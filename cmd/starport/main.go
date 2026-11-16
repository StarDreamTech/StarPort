package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/StarDreamTech/starport/internal/server"
)

type config struct {
	Listen    string
	MihomoBin string
	Profile   string
	DataDir   string
	MixedPort int
	DNSListen string
}

func main() {
	path := flag.String("config", "config.yaml", "本机配置，缺省时用内置默认值")
	flag.Parse()
	cfg := loadConfig(*path)
	if err := loopback(cfg.Listen); err != nil {
		log.Fatal(err)
	}
	app := server.New(cfg.Listen, cfg.Profile, cfg.DataDir, cfg.MihomoBin, cfg.DNSListen, cfg.MixedPort)
	log.Printf("星口听在 http://%s", cfg.Listen)
	log.Fatal(http.ListenAndServe(cfg.Listen, app.Handler()))
}

func loadConfig(path string) config {
	cfg := config{
		Listen:    "127.0.0.1:18765",
		DataDir:   "data",
		MixedPort: 17890,
		DNSListen: "127.0.0.1:17874",
	}
	b, err := os.ReadFile(path)
	if err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, val, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			val = strings.TrimSpace(val)
			if i := strings.Index(val, " #"); i >= 0 {
				val = strings.TrimSpace(val[:i])
			}
			val = strings.Trim(val, `"'`)
			switch strings.TrimSpace(key) {
			case "listen":
				if val != "" {
					cfg.Listen = val
				}
			case "mihomo_bin":
				cfg.MihomoBin = val
			case "profile":
				cfg.Profile = val
			case "data_dir":
				if val != "" {
					cfg.DataDir = val
				}
			case "mixed_port":
				if n, err := strconv.Atoi(val); err == nil {
					cfg.MixedPort = n
				}
			case "dns_listen":
				if val != "" {
					cfg.DNSListen = val
				}
			}
		}
	}
	if cfg.DataDir != "" && !filepath.IsAbs(cfg.DataDir) {
		cwd, err := os.Getwd()
		if err == nil {
			cfg.DataDir = filepath.Join(cwd, cfg.DataDir)
		}
	}
	if cfg.Profile != "" && !filepath.IsAbs(cfg.Profile) {
		cwd, err := os.Getwd()
		if err == nil {
			cfg.Profile = filepath.Join(cwd, cfg.Profile)
		}
	}
	if cfg.MihomoBin == "" {
		const verge = "/Applications/Clash Verge.app/Contents/MacOS/verge-mihomo"
		if _, err := os.Stat(verge); err == nil {
			cfg.MihomoBin = verge
		} else if found, err := exec.LookPath("mihomo"); err == nil {
			cfg.MihomoBin = found
		}
	}
	return cfg
}

func loopback(listen string) error {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		log.Fatal("管理页只能听 127.0.0.1")
	}
	return nil
}
