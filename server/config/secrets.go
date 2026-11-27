// Package config holds the secrets file: readable only by the server user (0600), each
// secret referred to by name. Tools receive values in their environment; readings are
// scrubbed of them (n.tools). The model never sees a secret.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
)

type Secrets struct {
	Path string
	mu   sync.Mutex
}

func (s *Secrets) load() (map[string]string, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Secrets) Get(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.load()
	return m[name]
}

// Values returns every secret value, longest first (for scrubbing).
func (s *Secrets) Values() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.load()
	var out []string
	for _, v := range m {
		if v != "" {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

func (s *Secrets) Set(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	if value == "" {
		delete(m, name)
	} else {
		m[name] = value
	}
	b, _ := json.MarshalIndent(m, "", " ")
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	tmp := s.Path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmp, s.Path)
}
