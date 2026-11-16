package profile

import (
	"strings"
	"testing"
)

const fixture = `
allow-lan: true
bind-address: 0.0.0.0
dns:
    enable: true
    listen: 127.0.0.1:7874
hosts:
    example.test: 127.0.0.1
mixed-port: 7890
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
      port: "1"
      uuid: 00000000-0000-0000-0000-000000000001
      alterId: 0
      cipher: auto
    - name: 美国-测试
      type: vmess
      server: 10.1.2.3
      port: 1
      uuid: 00000000-0000-0000-0000-000000000002
      alterId: 0
      cipher: auto
proxy-groups:
    - name: G
      type: select
      proxies:
          - DIRECT
rules:
    - MATCH,DIRECT
`

func TestParseAndRenderKeepsOnlyTheChosenNode(t *testing.T) {
	nodes, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("nodes %d", len(nodes))
	}
	usable := 0
	for _, n := range nodes {
		if !n.Notice {
			usable++
		}
	}
	if usable != 2 || !nodes[0].Notice {
		t.Fatalf("notice filter usable=%d first=%v", usable, nodes[0].Notice)
	}
	got, err := Match(nodes, "香港-测试")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "🇭🇰 香港-测试" {
		t.Fatalf("name %q", got.Name)
	}
	if !strings.Contains(got.RawName, `\U0001F1ED`) {
		t.Fatalf("raw name was rewritten: %s", got.RawName)
	}
	got.ListenPort = 18008
	out, err := Render(fixture, []Node{got}, Options{MixedPort: 17890, DNSListen: "127.0.0.1:17874"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "proxy: "+got.RawName) {
		t.Fatal("listener did not keep the original name scalar")
	}
	if strings.Contains(out, "00000000-0000-0000-0000-000000000002") || strings.Contains(out, "10.1.2.3") {
		t.Fatal("unselected node leaked into the runtime config")
	}
	if strings.Contains(out, "0.0.0.0") || strings.Contains(out, "proxy-groups") {
		t.Fatal("source bind address or groups were copied")
	}
	if !strings.Contains(out, "listen: 127.0.0.1:17874") || !strings.Contains(out, "example.test") {
		t.Fatal("dns listen or hosts missing")
	}
	if strings.Contains(out, "127.0.0.1:7874") {
		t.Fatal("original dns listen was kept")
	}
}

func TestMatchIsExactBeforeSubstring(t *testing.T) {
	nodes := []Node{{Name: "日本-广东专线 BGP"}, {Name: "日本-广东专线 BGP 2"}}
	got, err := Match(nodes, "日本-广东专线 BGP")
	if err != nil || got.Name != "日本-广东专线 BGP" {
		t.Fatalf("got %q err %v", got.Name, err)
	}
	if _, err := Match(nodes, "广东"); err == nil {
		t.Fatal("ambiguous substring should fail")
	}
}
