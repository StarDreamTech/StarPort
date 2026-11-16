package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Exit is one chosen node and the local port it should own.
type Exit struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

// Store is the local rule list. It holds names and ports, never node secrets.
type Store struct {
	path string
}

func Open(dir string) *Store {
	return &Store{path: filepath.Join(dir, "exits.json")}
}

func (s *Store) List() ([]Exit, error) {
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Exits []Exit `json:"exits"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("出口列表读不了")
	}
	return file.Exits, nil
}

func (s *Store) Save(exits []Exit) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(struct {
		Exits []Exit `json:"exits"`
	}{Exits: exits}, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Add(name string, port int) ([]Exit, error) {
	exits, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, e := range exits {
		if e.Port == port {
			return nil, fmt.Errorf("端口 %d 已经在用", port)
		}
		if e.Name == name {
			return nil, fmt.Errorf("这个节点已经加过了")
		}
	}
	exits = append(exits, Exit{Name: name, Port: port})
	if err := s.Save(exits); err != nil {
		return nil, err
	}
	return exits, nil
}

func (s *Store) Remove(port int) ([]Exit, error) {
	exits, err := s.List()
	if err != nil {
		return nil, err
	}
	next := exits[:0]
	found := false
	for _, e := range exits {
		if e.Port == port {
			found = true
			continue
		}
		next = append(next, e)
	}
	if !found {
		return nil, fmt.Errorf("没有端口 %d", port)
	}
	if next == nil {
		next = []Exit{}
	}
	if err := s.Save(next); err != nil {
		return nil, err
	}
	return next, nil
}
