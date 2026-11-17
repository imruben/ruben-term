package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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
}

func defaultConfig() Config {
	return Config{
		FontFamily:   `"JetBrainsMono Nerd Font", "JetBrainsMono NF", "Cascadia Code", "SF Mono", Menlo, Consolas, monospace`,
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
		ProjectRoots: []string{"~/dev/projects", "~/mytheresa"},
		Panel:        "",
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
	if cfg.FontSize < 6 || cfg.FontSize > 40 {
		cfg.FontSize = 13
	}
	if cfg.LineHeight < 1 || cfg.LineHeight > 2 {
		cfg.LineHeight = 1.2
	}
	if cfg.Scrollback < 0 {
		cfg.Scrollback = 10000
	}
	if cfg.Colors == nil {
		cfg.Colors = map[string]string{}
	}
	return cfg, nil
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
