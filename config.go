package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Launcher is a program that opens in a new tab, in the folder of the tab you are on.
type Launcher struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// Config is the user's config.json. Missing keys keep their default.
type Config struct {
	FontFamily     string            `json:"fontFamily"`
	FontSize       int               `json:"fontSize"`
	LineHeight     float64           `json:"lineHeight"`
	Theme          string            `json:"theme"` // dark | light | system
	Colors         map[string]string `json:"colors"`
	Scrollback     int               `json:"scrollback"`
	CursorBlink    bool              `json:"cursorBlink"`
	CopyOnSelect   bool              `json:"copyOnSelect"`
	DefaultProfile string            `json:"defaultProfile"`
	// LoadUserRc reads your own ~/.zshrc (never the terminal-setup block in it).
	LoadUserRc bool       `json:"loadUserRc"`
	Launchers  []Launcher `json:"launchers"`
	// ProjectRoots: each one is a group in the Projects panel, named after the folder.
	ProjectRoots []string `json:"projectRoots"`
	// Panel is the side panel to start with: ports, ssh, projects, or empty for none.
	Panel string `json:"panel"`
	// Servers are SSH hosts added in Settings, shown in the SSH panel before the hosts
	// of ~/.ssh/config.
	Servers []Server `json:"servers"`
	// RestoreTabs opens the tabs and panes of the last run, each in its folder.
	RestoreTabs bool `json:"restoreTabs"`
	// RestorePrograms is what happens to a program that was running in a restored pane:
	// "safe" reopens Claude, opencode and ssh and types the rest at the prompt, "all"
	// runs every one again, "none" types them all.
	RestorePrograms string `json:"restorePrograms"`
}

// Server is an SSH host kept in config.json, so it does not need ~/.ssh/config.
type Server struct {
	Name     string `json:"name"`
	HostName string `json:"hostName"`
	User     string `json:"user"`
	Port     string `json:"port"`
	Identity string `json:"identity"` // a key file, ~ allowed; empty: ssh's default
}

func defaultConfig() Config {
	return Config{
		FontFamily:   `"Cascadia Code", "JetBrainsMono Nerd Font", "JetBrainsMono NF", "SF Mono", Menlo, Consolas, monospace`,
		FontSize:     13,
		LineHeight:   1.2,
		Theme:        "dark",
		Colors:       map[string]string{},
		Scrollback:   10000,
		CursorBlink:  false,
		CopyOnSelect: false,
		LoadUserRc:   true,
		Launchers: []Launcher{
			{Name: "Claude", Command: "claude"},
			{Name: "opencode", Command: "opencode"},
		},
		ProjectRoots:    []string{},
		Panel:           "",
		Servers:         []Server{},
		RestoreTabs:     true,
		RestorePrograms: "safe",
	}
}

// configDir is ~/.config/ruben-term on macOS and Linux, %AppData%\ruben-term on Windows.
func configDir() string {
	if runtime.GOOS == "windows" {
		if d, err := os.UserConfigDir(); err == nil {
			return filepath.Join(d, "ruben-term")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ruben-term")
}

func configPath() string { return filepath.Join(configDir(), "config.json") }

// loadConfig reads config.json over the defaults; the first run writes the defaults so
// there is a file to edit. A file that does not parse is reported and left alone.
func loadConfig() (Config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(configPath())
	if os.IsNotExist(err) {
		if err := os.MkdirAll(configDir(), 0o755); err != nil {
			return cfg, err
		}
		out, _ := json.MarshalIndent(cfg, "", "  ")
		return cfg, os.WriteFile(configPath(), append(out, '\n'), 0o644)
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultConfig(), err
	}
	return normalize(cfg), nil
}

// normalize fixes what is out of range and drops empty entries.
func normalize(cfg Config) Config {
	def := defaultConfig()
	if cfg.FontSize < 6 || cfg.FontSize > 40 {
		cfg.FontSize = def.FontSize
	}
	if cfg.LineHeight < 1 || cfg.LineHeight > 2 {
		cfg.LineHeight = def.LineHeight
	}
	if cfg.Scrollback < 0 {
		cfg.Scrollback = def.Scrollback
	}
	if strings.TrimSpace(cfg.FontFamily) == "" {
		cfg.FontFamily = def.FontFamily
	}
	switch cfg.Theme {
	case "dark", "light", "system":
	default:
		cfg.Theme = def.Theme
	}
	switch cfg.RestorePrograms {
	case "safe", "all", "none":
	default:
		cfg.RestorePrograms = def.RestorePrograms
	}
	switch cfg.Panel {
	case "", "ports", "ssh", "projects":
	default:
		cfg.Panel = ""
	}
	if cfg.Colors == nil {
		cfg.Colors = map[string]string{}
	}
	roots := []string{}
	seen := map[string]bool{}
	for _, r := range cfg.ProjectRoots {
		r = strings.TrimSpace(r)
		if r != "" && !seen[r] {
			seen[r] = true
			roots = append(roots, r)
		}
	}
	cfg.ProjectRoots = roots
	launchers := []Launcher{}
	for _, l := range cfg.Launchers {
		l.Name, l.Command = strings.TrimSpace(l.Name), strings.TrimSpace(l.Command)
		if l.Command == "" {
			continue
		}
		if l.Name == "" {
			l.Name = l.Command
		}
		launchers = append(launchers, l)
	}
	cfg.Launchers = launchers
	servers := []Server{}
	for _, s := range cfg.Servers {
		s.Name, s.HostName = strings.TrimSpace(s.Name), strings.TrimSpace(s.HostName)
		s.User, s.Port, s.Identity = strings.TrimSpace(s.User), strings.TrimSpace(s.Port), strings.TrimSpace(s.Identity)
		if s.HostName == "" {
			continue
		}
		if s.Name == "" {
			s.Name = s.HostName
		}
		servers = append(servers, s)
	}
	cfg.Servers = servers
	return cfg
}

// saveConfig writes config.json (what Settings changed).
func saveConfig(cfg Config) error {
	if err := os.MkdirAll(configDir(), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := configPath() + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, configPath())
}

// windowState is the size the window had when it was last closed.
type windowState struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

func statePath() string { return filepath.Join(configDir(), "state.json") }

func loadState() windowState {
	st := windowState{Width: 1100, Height: 700}
	if data, err := os.ReadFile(statePath()); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	if st.Width < 480 || st.Width > 10000 {
		st.Width = 1100
	}
	if st.Height < 300 || st.Height > 10000 {
		st.Height = 700
	}
	return st
}

func saveState(st windowState) {
	if data, err := json.Marshal(st); err == nil {
		_ = os.MkdirAll(configDir(), 0o755)
		_ = os.WriteFile(statePath(), data, 0o644)
	}
}

// session.json: the tabs and panes of the last run (see SaveSession). screens.json:
// what each pane showed, by pane id (see SaveScreens).
func sessionPath() string { return filepath.Join(configDir(), "session.json") }
func screensPath() string { return filepath.Join(configDir(), "screens.json") }

func readJSONFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || !json.Valid(data) {
		return ""
	}
	return string(data)
}

// writeJSONFile replaces the file in one step, so a crash leaves the old one.
func writeJSONFile(path, data string) error {
	if !json.Valid([]byte(data)) {
		return errors.New(filepath.Base(path) + ": not JSON")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
