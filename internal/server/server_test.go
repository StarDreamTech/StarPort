package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixture = `
dns:
    listen: 127.0.0.1:7874
hosts:
    example.test: 127.0.0.1
proxies:
    - name: 更新订阅
      type: vmess
      server: 127.0.0.1
      port: 1
      uuid: 00000000-0000-0000-0000-000000000099
      alterId: 0
      cipher: auto
    - name: "\U0001F1ED\U0001F1F0 香港-测试"
      type: vmess
      server: 127.0.0.1
      port: 1
      uuid: 00000000-0000-0000-0000-000000000001
      alterId: 0
      cipher: auto
    - name: 美国-测试
      type: vmess
      server: 10.9.8.7
      port: 1
      uuid: 00000000-0000-0000-0000-000000000002
      alterId: 0
      cipher: auto
`

type fakeEngine struct {
	config  string
	running bool
}

func (f *fakeEngine) Use(config string)   { f.config = config }
func (f *fakeEngine) Start() (int, error) { f.running = true; return 42, nil }
func (f *fakeEngine) Stop() error         { f.running = false; return nil }
func (f *fakeEngine) Running() (int, bool) {
	if f.running {
		return 42, true
	}
	return 0, false
}

func TestAddStartAndHideSecrets(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "source.yaml")
	if err := os.WriteFile(profilePath, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	app := New("127.0.0.1:18765", profilePath, filepath.Join(dir, "data"), "/bin/echo", "127.0.0.1:17874", 17890)
	fake := &fakeEngine{}
	app.Engine = fake
	app.dial = func(int) bool { return true }
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	res := postJSON(t, ts.URL+"/api/exits", `{"name":"香港-测试","port":18008}`)
	if res.StatusCode != 200 {
		t.Fatalf("add %d %s", res.StatusCode, readBody(t, res))
	}
	res = postJSON(t, ts.URL+"/api/start", `{}`)
	if res.StatusCode != 200 {
		t.Fatalf("start %d %s", res.StatusCode, readBody(t, res))
	}
	runtime, err := os.ReadFile(filepath.Join(dir, "data", "runtime.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(runtime)
	if strings.Contains(text, "10.9.8.7") || strings.Contains(text, "00000000-0000-0000-0000-000000000002") {
		t.Fatal("unselected node was written")
	}
	if !strings.Contains(text, "port: 18008") || !strings.Contains(text, `\U0001F1ED`) {
		t.Fatal("chosen listener was not written with the original name")
	}
	res, err = http.Get(ts.URL + "/api/agents")
	if err != nil {
		t.Fatal(err)
	}
	var agents struct {
		Running bool `json:"running"`
		Proxies []struct {
			Port    int    `json:"port"`
			Socks5H string `json:"socks5h"`
		} `json:"proxies"`
	}
	if err := json.NewDecoder(res.Body).Decode(&agents); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if !agents.Running || len(agents.Proxies) != 1 || agents.Proxies[0].Socks5H != "socks5h://127.0.0.1:18008" {
		t.Fatalf("agents %+v", agents)
	}
	res = postJSON(t, ts.URL+"/api/exits", `{"name":"测试","port":18009}`)
	if res.StatusCode != 400 {
		t.Fatalf("ambiguous add should fail, got %d %s", res.StatusCode, readBody(t, res))
	}
}

func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	res, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b)
}
