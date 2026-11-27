// Package ops is n.ops: the install under one root, its two accounts and their user
// services, backups, replay and updates.
package ops

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Root is Plebian's one directory. Nothing outside it is written except the two accounts
// and their linger flags.
type Root string

func (r Root) P(parts ...string) string {
	return filepath.Join(append([]string{string(r)}, parts...)...)
}

func (r Root) Bin() string        { return r.P("bin", "plebian") }
func (r Root) Store() string      { return r.P("data", "store.db") }
func (r Root) Blobs() string      { return r.P("data", "blobs") }
func (r Root) Secrets() string    { return r.P("secrets", "secrets.json") }
func (r Root) Run() string        { return r.P("run") }
func (r Root) APIFile() string    { return r.P("run", "api.json") }
func (r Root) ServerHome() string { return r.P("home", "server") }
func (r Root) ToolsHome() string  { return r.P("home", "tools") }
func (r Root) Workspace() string  { return r.P("workspace") }
func (r Root) Outside() string    { return r.P("workspace", "outside") }
func (r Root) Config() string     { return r.P("config.json") }
func (r Root) Tailnet() string    { return r.P("tailscale") }
func (r Root) Browser() string    { return r.P("browser") }
func (r Root) Speech() string     { return r.P("speech") }
func (r Root) RunnerSock() string { return r.P("run", "runner", "runner.sock") }

// Account names. Both are system accounts; their homes are inside the root.
const (
	ServerUser = "plebian"
	ToolsUser  = "plebian-tools"
)

// Config is what install fixes for the server: the owner's time zone and the sources it
// reads time and texts from (a fixture endpoint in tests, the host clock and libgm otherwise).
type Config struct {
	TZ          string `json:"tz"`
	ClockSource string `json:"clock_source,omitempty"`
	TextsSource string `json:"texts_source,omitempty"`
	Tailnet     bool   `json:"tailnet"`
	AuthKeyFile string `json:"auth_key_file,omitempty"`
}

func (r Root) LoadConfig() (Config, error) {
	var c Config
	b, err := os.ReadFile(r.Config())
	if errors.Is(err, os.ErrNotExist) {
		return Config{TZ: "UTC"}, nil
	}
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(b, &c)
}

// FromExecutable finds the root from the running program: <root>/bin/plebian.
func FromExecutable() (Root, error) {
	if v := os.Getenv("PLEBIAN_ROOT"); v != "" {
		return Root(v), nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	return Root(filepath.Dir(filepath.Dir(exe))), nil
}
