// Package profile reads a local Clash config and writes a small sidecar config.
// The sidecar lists only the nodes the user picked. Subscription URLs and node
// secrets stay in the source file; this package never returns them.
package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Node is one proxy the page can show. Raw is the original YAML item and is
// only used to build the local runtime config.
type Node struct {
	Name       string
	RawName    string
	Raw        string
	Notice     bool
	ListenPort int
}

// ClashHome is the Clash Verge Rev settings directory on this computer.
func ClashHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "io.github.clash-verge-rev.clash-verge-rev")
}

// CurrentProfile resolves Clash Verge's selected profile file and its display name.
// profiles.yaml can contain subscription URLs; only the current id and the name
// next to it are read.
func CurrentProfile(home string) (path, display string, err error) {
	if home == "" {
		return "", "", fmt.Errorf("没有找到 Clash Verge 的配置目录")
	}
	text, err := os.ReadFile(filepath.Join(home, "profiles.yaml"))
	if err != nil {
		return "", "", fmt.Errorf("读取 Clash 配置列表失败")
	}
	id := ""
	for _, line := range strings.Split(string(text), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "current:") {
			id = strings.TrimSpace(strings.TrimPrefix(trim, "current:"))
			id = strings.Trim(id, `"'`)
			break
		}
	}
	if id == "" {
		return "", "", fmt.Errorf("Clash 配置列表里没有 current")
	}
	path = filepath.Join(home, "profiles", id+".yaml")
	if _, err := os.Stat(path); err != nil {
		return "", "", fmt.Errorf("当前配置文件不存在")
	}
	display = id
	lines := strings.Split(string(text), "\n")
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim != "- uid: "+id && trim != "uid: "+id {
			continue
		}
		for _, next := range lines[i+1 : min(i+8, len(lines))] {
			n := strings.TrimSpace(next)
			if strings.HasPrefix(n, "- uid:") || (len(next) > 0 && next[0] != ' ' && next[0] != '\t') {
				break
			}
			if strings.HasPrefix(n, "name:") {
				name := strings.TrimSpace(strings.TrimPrefix(n, "name:"))
				name = strings.Trim(name, `"'`)
				if name != "" && name != "null" {
					display = name
				}
				break
			}
		}
		break
	}
	return path, display, nil
}

// Parse lists proxy items. Notice nodes are subscription placeholders, not exits.
func Parse(text string) ([]Node, error) {
	sections := splitTop(text)
	body, ok := sections["proxies"]
	if !ok {
		return nil, fmt.Errorf("配置里没有 proxies")
	}
	lines := strings.Split(body, "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("配置里没有 proxies")
	}
	items := splitItems(lines[1:])
	if len(items) == 0 {
		return nil, fmt.Errorf("配置里没有节点")
	}
	out := make([]Node, 0, len(items))
	for _, item := range items {
		rawName, name, err := itemName(item)
		if err != nil {
			return nil, err
		}
		raw := strings.Join(item, "\n")
		if !strings.HasSuffix(raw, "\n") {
			raw += "\n"
		}
		out = append(out, Node{
			Name:    name,
			RawName: rawName,
			Raw:     raw,
			Notice:  isNotice(name),
		})
	}
	return out, nil
}

// Match finds one usable node by exact name, or by a unique substring.
func Match(nodes []Node, query string) (Node, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Node{}, fmt.Errorf("要写节点名字")
	}
	var exact []Node
	var partial []Node
	for _, n := range nodes {
		if n.Notice {
			continue
		}
		if n.Name == query {
			exact = append(exact, n)
		} else if strings.Contains(n.Name, query) {
			partial = append(partial, n)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return Node{}, fmt.Errorf("有 %d 个同名节点", len(exact))
	}
	if len(partial) == 1 {
		return partial[0], nil
	}
	if len(partial) == 0 {
		return Node{}, fmt.Errorf("没有这个节点")
	}
	names := make([]string, 0, min(5, len(partial)))
	for i := 0; i < len(partial) && i < 5; i++ {
		names = append(names, partial[i].Name)
	}
	return Node{}, fmt.Errorf("匹配到 %d 个节点，请写全名：%s", len(partial), strings.Join(names, "、"))
}

// Options controls the sidecar ports that are not themselves exits.
type Options struct {
	MixedPort int
	DNSListen string
}

// Render builds a mihomo config that contains only the selected nodes.
// Each node gets one socks listener. Rules and groups from the source are left out.
func Render(text string, selected []Node, opt Options) (string, error) {
	if len(selected) == 0 {
		return "", fmt.Errorf("还没有选择节点")
	}
	if opt.MixedPort <= 0 || opt.MixedPort > 65535 {
		return "", fmt.Errorf("混用端口不合法")
	}
	if opt.DNSListen == "" {
		return "", fmt.Errorf("要指定 DNS 监听地址")
	}
	seen := map[int]bool{}
	for _, n := range selected {
		if n.Notice || n.Raw == "" || n.RawName == "" {
			return "", fmt.Errorf("节点 %q 不能写入运行配置", n.Name)
		}
		if n.PortOK() == false {
			return "", fmt.Errorf("端口不合法")
		}
		if seen[n.ListenPort] {
			return "", fmt.Errorf("端口 %d 重复", n.ListenPort)
		}
		seen[n.ListenPort] = true
	}
	sections := splitTop(text)
	var b strings.Builder
	fmt.Fprintf(&b, "allow-lan: false\nbind-address: 127.0.0.1\nipv6: false\nlog-level: info\nmixed-port: %d\nmode: rule\n", opt.MixedPort)
	if dns, ok := sections["dns"]; ok {
		b.WriteString(patchListen(dns, opt.DNSListen))
		if !strings.HasSuffix(dns, "\n") {
			b.WriteByte('\n')
		}
	}
	if hosts, ok := sections["hosts"]; ok {
		b.WriteString(hosts)
		if !strings.HasSuffix(hosts, "\n") {
			b.WriteByte('\n')
		}
	}
	b.WriteString("proxies:\n")
	for _, n := range selected {
		b.WriteString(n.Raw)
		if !strings.HasSuffix(n.Raw, "\n") {
			b.WriteByte('\n')
		}
	}
	b.WriteString("listeners:\n")
	for _, n := range selected {
		fmt.Fprintf(&b, "  - name: sp-%d\n    type: socks\n    port: %d\n    listen: 127.0.0.1\n    udp: false\n    proxy: %s\n", n.ListenPort, n.ListenPort, n.RawName)
	}
	b.WriteString("rules:\n  - MATCH,DIRECT\n")
	return b.String(), nil
}

// ListenPort is set by the caller before Render. It is not parsed from Clash.
func (n Node) PortOK() bool { return n.ListenPort >= 1024 && n.ListenPort <= 65535 }

func splitTop(text string) map[string]string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	out := map[string]string{}
	var key string
	var buf []string
	flush := func() {
		if key == "" {
			return
		}
		out[key] = strings.Join(buf, "\n")
		if !strings.HasSuffix(out[key], "\n") {
			out[key] += "\n"
		}
	}
	for _, line := range lines {
		if isTop(line) {
			flush()
			key = strings.TrimSpace(strings.SplitN(line, ":", 2)[0])
			buf = []string{line}
			continue
		}
		if key != "" {
			buf = append(buf, line)
		}
	}
	flush()
	return out
}

func isTop(line string) bool {
	if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
		return false
	}
	return strings.Contains(line, ":")
}

func splitItems(lines []string) [][]string {
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " "), "- ") && strings.TrimSpace(line) != "" {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	indent := len(lines[start]) - len(strings.TrimLeft(lines[start], " "))
	var items [][]string
	var cur []string
	for _, line := range lines[start:] {
		if line == "" && cur == nil {
			continue
		}
		this := len(line) - len(strings.TrimLeft(line, " "))
		if strings.HasPrefix(strings.TrimLeft(line, " "), "- ") && this == indent {
			if cur != nil {
				items = append(items, cur)
			}
			cur = []string{line}
			continue
		}
		if cur != nil {
			cur = append(cur, line)
		}
	}
	if cur != nil {
		items = append(items, cur)
	}
	return items
}

func itemName(item []string) (raw, decoded string, err error) {
	for _, line := range item {
		trim := strings.TrimSpace(line)
		trim = strings.TrimPrefix(trim, "- ")
		if !strings.HasPrefix(trim, "name:") {
			continue
		}
		raw = strings.TrimSpace(strings.TrimPrefix(trim, "name:"))
		if i := strings.Index(raw, " #"); i >= 0 && !strings.HasPrefix(raw, `"`) && !strings.HasPrefix(raw, `'`) {
			raw = strings.TrimSpace(raw[:i])
		}
		decoded, err = decodeScalar(raw)
		return raw, decoded, err
	}
	return "", "", fmt.Errorf("有一个节点没有名字")
}

func decodeScalar(raw string) (string, error) {
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		return unescapeDouble(raw)
	}
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return strings.ReplaceAll(raw[1:len(raw)-1], "''", "'"), nil
	}
	return raw, nil
}

func unescapeDouble(quoted string) (string, error) {
	inner := quoted[1 : len(quoted)-1]
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] != '\\' {
			b.WriteByte(inner[i])
			continue
		}
		if i+1 >= len(inner) {
			return "", fmt.Errorf("节点名字转义不完整")
		}
		switch inner[i+1] {
		case 'n':
			b.WriteByte('\n')
			i++
		case 't':
			b.WriteByte('\t')
			i++
		case '\\', '"', '/':
			b.WriteByte(inner[i+1])
			i++
		case 'u':
			if i+6 > len(inner) {
				return "", fmt.Errorf("节点名字转义不完整")
			}
			n, err := strconv.ParseInt(inner[i+2:i+6], 16, 32)
			if err != nil {
				return "", fmt.Errorf("节点名字转义不完整")
			}
			b.WriteRune(rune(n))
			i += 5
		case 'U':
			if i+10 > len(inner) {
				return "", fmt.Errorf("节点名字转义不完整")
			}
			n, err := strconv.ParseInt(inner[i+2:i+10], 16, 32)
			if err != nil {
				return "", fmt.Errorf("节点名字转义不完整")
			}
			b.WriteRune(rune(n))
			i += 9
		default:
			b.WriteByte(inner[i+1])
			i++
		}
	}
	return b.String(), nil
}

func patchListen(section, listen string) string {
	lines := strings.Split(section, "\n")
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "listen:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
			lines[i] = indent + "listen: " + listen
		}
	}
	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

func isNotice(name string) bool {
	for _, s := range []string{"更新订阅", "激活订阅", "禁止使用"} {
		if strings.Contains(name, s) {
			return true
		}
	}
	return false
}
