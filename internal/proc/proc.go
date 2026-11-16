package proc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Mihomo runs one sidecar. It only stops a process whose command line contains
// the config path this manager wrote.
type Mihomo struct {
	Bin     string
	DataDir string
	Config  string
}

func (m *Mihomo) pidFile() string { return filepath.Join(m.DataDir, "mihomo.pid") }

func (m *Mihomo) Running() (int, bool) {
	b, err := os.ReadFile(m.pidFile())
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if !owns(pid, m.Config) {
		return 0, false
	}
	return pid, true
}

func (m *Mihomo) Use(config string) { m.Config = config }

func (m *Mihomo) Start() (int, error) {
	if m.Bin == "" {
		return 0, fmt.Errorf("没有 mihomo 可执行文件")
	}
	if _, err := os.Stat(m.Bin); err != nil {
		return 0, fmt.Errorf("找不到 mihomo：%s", m.Bin)
	}
	if _, ok := m.Running(); ok {
		if err := m.Stop(); err != nil {
			return 0, err
		}
	}
	if err := os.MkdirAll(m.DataDir, 0o755); err != nil {
		return 0, err
	}
	logf, err := os.OpenFile(filepath.Join(m.DataDir, "mihomo.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(m.Bin, "-d", m.DataDir, "-f", m.Config)
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		logf.Close()
		return 0, err
	}
	if err := os.WriteFile(m.pidFile(), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		logf.Close()
		return 0, err
	}
	go func() {
		_ = cmd.Wait()
		logf.Close()
	}()
	return cmd.Process.Pid, nil
}

func (m *Mihomo) Stop() error {
	pid, ok := m.Running()
	if !ok {
		_ = os.Remove(m.pidFile())
		return nil
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !owns(pid, m.Config) {
			_ = os.Remove(m.pidFile())
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	time.Sleep(200 * time.Millisecond)
	_ = os.Remove(m.pidFile())
	return nil
}

func owns(pid int, config string) bool {
	if pid <= 0 || config == "" {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), config)
}
