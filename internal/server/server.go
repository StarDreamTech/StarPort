package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/StarDreamTech/starport/internal/proc"
	"github.com/StarDreamTech/starport/internal/profile"
	"github.com/StarDreamTech/starport/internal/store"
)

// Engine starts and stops the sidecar. Implementations must only stop the
// process they started.
type Engine interface {
	Use(config string)
	Start() (int, error)
	Stop() error
	Running() (int, bool)
}

// App is the local management page and the API an agent can call.
type App struct {
	mu        sync.Mutex
	Listen    string
	Profile   string
	DataDir   string
	MixedPort int
	DNSListen string
	Bin       string
	Rules     *store.Store
	Engine    Engine
	dial      func(port int) bool
}

func New(listen, profile, dataDir, bin, dns string, mixed int) *App {
	eng := &proc.Mihomo{Bin: bin, DataDir: dataDir, Config: filepath.Join(dataDir, "runtime.yaml")}
	return &App{
		Listen:    listen,
		Profile:   profile,
		DataDir:   dataDir,
		MixedPort: mixed,
		DNSListen: dns,
		Bin:       bin,
		Rules:     store.Open(dataDir),
		Engine:    eng,
		dial:      dialLocal,
	}
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.page)
	mux.HandleFunc("GET /api/status", a.status)
	mux.HandleFunc("GET /api/catalog", a.catalog)
	mux.HandleFunc("GET /api/exits", a.exits)
	mux.HandleFunc("POST /api/exits", a.addExit)
	mux.HandleFunc("DELETE /api/exits/{port}", a.deleteExit)
	mux.HandleFunc("POST /api/start", a.start)
	mux.HandleFunc("POST /api/stop", a.stop)
	mux.HandleFunc("GET /api/agents", a.agents)
	return mux
}

func (a *App) page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(indexHTML)
}

func (a *App) status(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	writeJSON(w, http.StatusOK, a.snapshot())
}

func (a *App) exits(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	exits, err := a.Rules.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exits": exitsOrEmpty(exits)})
}

func (a *App) catalog(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	nodes, display, err := a.nodes()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	type item struct {
		Name string `json:"name"`
	}
	list := []item{}
	hidden := 0
	for _, n := range nodes {
		if n.Notice {
			hidden++
			continue
		}
		list = append(list, item{Name: n.Name})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"profile":        display,
		"usable":         len(list),
		"hidden_notices": hidden,
		"nodes":          list,
	})
}

func (a *App) addExit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	nodes, _, err := a.nodes()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	node, err := profile.Match(nodes, body.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	exits, err := a.Rules.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	port := body.Port
	if port == 0 {
		port = nextPort(exits, a.reserved())
	}
	if err := checkPort(port, a.reserved()); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	exits, err = a.Rules.Add(node.Name, port)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"exits":   exits,
		"pending": a.pending(exits),
	})
}

func (a *App) deleteExit(w http.ResponseWriter, r *http.Request) {
	port, err := strconv.Atoi(r.PathValue("port"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("端口不对"))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	exits, err := a.Rules.Remove(port)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exits": exitsOrEmpty(exits), "pending": a.pending(exits)})
}

func (a *App) start(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	exits, err := a.Rules.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if len(exits) == 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("还没有节点，先添加再启动"))
		return
	}
	text, _, err := a.profileText()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	nodes, err := profile.Parse(text)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	selected := make([]profile.Node, 0, len(exits))
	for _, e := range exits {
		n, err := profile.Match(nodes, e.Name)
		if err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("%s：%w", e.Name, err))
			return
		}
		n.ListenPort = e.Port
		selected = append(selected, n)
	}
	rendered, err := profile.Render(text, selected, profile.Options{MixedPort: a.MixedPort, DNSListen: a.DNSListen})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := os.MkdirAll(a.DataDir, 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	linkGeo(a.DataDir, profile.ClashHome())
	cfgPath := filepath.Join(a.DataDir, "runtime.yaml")
	if err := os.WriteFile(cfgPath, []byte(rendered), 0o600); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	a.Engine.Use(cfgPath)
	if _, err := a.Engine.Start(); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	deadline := time.Now().Add(4 * time.Second)
	for _, e := range exits {
		for time.Now().Before(deadline) && !a.dial(e.Port) {
			time.Sleep(100 * time.Millisecond)
		}
		if !a.dial(e.Port) {
			_ = a.Engine.Stop()
			writeErr(w, http.StatusBadGateway, fmt.Errorf("端口 %d 没有打开，已停下。详情在 data/mihomo.log", e.Port))
			return
		}
	}
	if err := writeApplied(a.DataDir, exits); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, a.snapshot())
}

func (a *App) stop(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.Engine.Stop(); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	_ = os.Remove(filepath.Join(a.DataDir, "applied.json"))
	writeJSON(w, http.StatusOK, a.snapshot())
}

func (a *App) agents(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	exits, err := a.Rules.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	type row struct {
		Name    string `json:"name"`
		Port    int    `json:"port"`
		Socks5H string `json:"socks5h"`
		Open    bool   `json:"open"`
	}
	rows := make([]row, 0, len(exits))
	for _, e := range exits {
		open := a.dial(e.Port)
		rows = append(rows, row{
			Name: e.Name, Port: e.Port, Open: open,
			Socks5H: fmt.Sprintf("socks5h://127.0.0.1:%d", e.Port),
		})
	}
	pid, running := a.Engine.Running()
	writeJSON(w, http.StatusOK, map[string]any{
		"running": running,
		"pid":     pid,
		"proxies": rows,
	})
}

func (a *App) snapshot() map[string]any {
	exits, _ := a.Rules.List()
	pid, running := a.Engine.Running()
	type program struct {
		Name   string `json:"name"`
		PID    int    `json:"pid"`
		State  string `json:"state"`
		Listen string `json:"listen,omitempty"`
	}
	programs := []program{{
		Name: "starport", PID: os.Getpid(), State: "running", Listen: a.Listen,
	}}
	if running {
		programs = append(programs, program{Name: "mihomo", PID: pid, State: "running"})
	} else {
		programs = append(programs, program{Name: "mihomo", PID: 0, State: "stopped"})
	}
	open := 0
	type exitView struct {
		Name    string `json:"name"`
		Port    int    `json:"port"`
		Open    bool   `json:"open"`
		Socks5H string `json:"socks5h"`
	}
	views := make([]exitView, 0, len(exits))
	for _, e := range exits {
		up := running && a.dial(e.Port)
		if up {
			open++
		}
		views = append(views, exitView{
			Name: e.Name, Port: e.Port, Open: up,
			Socks5H: fmt.Sprintf("socks5h://127.0.0.1:%d", e.Port),
		})
	}
	_, display, _ := a.profileMeta()
	up := 1
	if running {
		up = 2
	}
	return map[string]any{
		"programs":        programs,
		"program_count":   up,
		"profile":         display,
		"exits":           views,
		"exit_count":      len(views),
		"listening_count": open,
		"pending":         a.pending(exits),
		"mixed_port":      a.MixedPort,
	}
}

func (a *App) nodes() ([]profile.Node, string, error) {
	text, _, err := a.profileText()
	if err != nil {
		return nil, "", err
	}
	nodes, err := profile.Parse(text)
	if err != nil {
		return nil, "", err
	}
	_, display, err := a.profileMeta()
	if err != nil {
		return nil, "", err
	}
	return nodes, display, nil
}

func (a *App) profileText() (string, string, error) {
	path, _, err := a.profileMeta()
	if err != nil {
		return "", "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("读不到本机 Clash 配置")
	}
	return string(b), path, nil
}

func (a *App) profileMeta() (string, string, error) {
	if a.Profile != "" {
		return a.Profile, filepath.Base(a.Profile), nil
	}
	return profile.CurrentProfile(profile.ClashHome())
}

func (a *App) pending(exits []store.Exit) bool {
	if _, ok := a.Engine.Running(); !ok {
		return false
	}
	applied, err := readApplied(a.DataDir)
	if err != nil {
		return true
	}
	return !sameExits(exits, applied)
}

func (a *App) reserved() map[int]bool {
	out := map[int]bool{7890: true, 7874: true, a.MixedPort: true}
	if _, port, err := net.SplitHostPort(a.DNSListen); err == nil {
		if n, err := strconv.Atoi(port); err == nil {
			out[n] = true
		}
	}
	if _, port, err := net.SplitHostPort(a.Listen); err == nil {
		if n, err := strconv.Atoi(port); err == nil {
			out[n] = true
		}
	}
	return out
}

func checkPort(port int, reserved map[int]bool) error {
	if port < 1024 || port > 65535 {
		return fmt.Errorf("端口要在 1024 和 65535 之间")
	}
	if reserved[port] {
		return fmt.Errorf("端口 %d 留给管理页或 Clash 本身", port)
	}
	return nil
}

func nextPort(exits []store.Exit, reserved map[int]bool) int {
	used := map[int]bool{}
	for _, e := range exits {
		used[e.Port] = true
	}
	for p := 18001; p <= 65535; p++ {
		if !used[p] && !reserved[p] {
			return p
		}
	}
	return 0
}

func sameExits(a, b []store.Exit) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, e := range a {
		seen[e.Name+"\x00"+strconv.Itoa(e.Port)]++
	}
	for _, e := range b {
		k := e.Name + "\x00" + strconv.Itoa(e.Port)
		seen[k]--
		if seen[k] < 0 {
			return false
		}
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

func writeApplied(dir string, exits []store.Exit) error {
	body, err := json.Marshal(struct {
		Exits []store.Exit `json:"exits"`
	}{Exits: exits})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "applied.json"), body, 0o600)
}

func readApplied(dir string) ([]store.Exit, error) {
	b, err := os.ReadFile(filepath.Join(dir, "applied.json"))
	if err != nil {
		return nil, err
	}
	var file struct {
		Exits []store.Exit `json:"exits"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, err
	}
	return file.Exits, nil
}

func linkGeo(dataDir, home string) {
	if home == "" {
		return
	}
	for _, name := range []string{"Country.mmdb", "geoip.dat", "geosite.dat", "ASN.mmdb"} {
		dst := filepath.Join(dataDir, name)
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		src := filepath.Join(home, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		_ = os.Symlink(src, dst)
	}
}

func dialLocal(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func exitsOrEmpty(exits []store.Exit) []store.Exit {
	if exits == nil {
		return []store.Exit{}
	}
	return exits
}

func readJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("读请求失败")
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return fmt.Errorf("请求是空的")
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("请求不是 JSON")
	}
	return nil
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
