package profile

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalProfileRendersOneNode(t *testing.T) {
	bin := "/Applications/Clash Verge.app/Contents/MacOS/verge-mihomo"
	if _, err := os.Stat(bin); err != nil {
		t.Skip("no local mihomo")
	}
	path, _, err := CurrentProfile(ClashHome())
	if err != nil {
		t.Skip(err.Error())
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := Parse(string(text))
	if err != nil {
		t.Fatal(err)
	}
	var chosen Node
	usable := 0
	for _, n := range nodes {
		if n.Notice {
			continue
		}
		usable++
		if chosen.Name == "" {
			chosen = n
		}
	}
	if usable == 0 {
		t.Fatal("no usable nodes")
	}
	chosen.ListenPort = 18008
	out, err := Render(string(text), []Node{chosen}, Options{MixedPort: 17890, DNSListen: "127.0.0.1:17874"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "proxy-groups:") || strings.Contains(out, "0.0.0.0") {
		t.Fatal("runtime config copied groups or a public bind")
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-t", "-d", dir, "-f", cfg)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		for _, line := range strings.Split(string(raw), "\n") {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "uuid") || strings.Contains(lower, "server") || strings.Contains(lower, "password") {
				continue
			}
			if strings.TrimSpace(line) != "" {
				t.Log(line)
			}
		}
		t.Fatalf("mihomo rejected the rendered config: %v", err)
	}
}
