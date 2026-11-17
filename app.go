package main

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	wails "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is what the window can call (window.go.main.App.*).
type App struct {
	ctx      context.Context
	mu       sync.Mutex
	cfg      Config
	cfgErr   string
	envs     map[string]*environment
	profiles []Profile
	sessions *sessions
}

func NewApp() *App {
	a := &App{envs: map[string]*environment{}}
	cfg, err := loadConfig()
	a.cfg = cfg
	if err != nil {
		a.cfgErr = err.Error()
	}
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.sessions = newSessions(func(event string, args ...any) {
		wails.EventsEmit(ctx, event, args...)
	})
}

func (a *App) beforeClose(ctx context.Context) bool {
	if !wails.WindowIsMaximised(ctx) && !wails.WindowIsFullscreen(ctx) && !wails.WindowIsMinimised(ctx) {
		w, h := wails.WindowGetSize(ctx)
		saveState(windowState{Width: w, Height: h})
	}
	return false
}

func (a *App) shutdown(context.Context) {
	if a.sessions != nil {
		a.sessions.closeAll()
	}
}

// Boot is everything the window needs to draw itself.
type Boot struct {
	Platform       string    `json:"platform"` // windows | darwin | linux
	WindowsBuild   int       `json:"windowsBuild"`
	Config         Config    `json:"config"`
	ConfigPath     string    `json:"configPath"`
	ConfigError    string    `json:"configError"`
	Profiles       []Profile `json:"profiles"`
	DefaultProfile string    `json:"defaultProfile"`
	// Autorun drives the window from RUBEN_TERM_AUTORUN (see the README, "Testing").
	Autorun string `json:"autorun"`
}

func (a *App) Bootstrap() Boot {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.profiles == nil {
		a.profiles = detectProfiles()
		for _, p := range a.profiles {
			a.envs[p.ID] = &environment{Profile: p}
		}
	}
	return Boot{
		Platform:       runtime.GOOS,
		WindowsBuild:   windowsBuild(),
		Config:         a.cfg,
		ConfigPath:     configPath(),
		ConfigError:    a.cfgErr,
		Profiles:       a.profiles,
		DefaultProfile: a.defaultProfile(),
		Autorun:        os.Getenv("RUBEN_TERM_AUTORUN"),
	}
}

func (a *App) defaultProfile() string {
	for _, p := range a.profiles {
		if p.ID == a.cfg.DefaultProfile || p.Name == a.cfg.DefaultProfile {
			return p.ID
		}
	}
	if len(a.profiles) > 0 {
		return a.profiles[0].ID
	}
	return ""
}

// ReloadConfig reads config.json again.
func (a *App) ReloadConfig() Boot {
	cfg, err := loadConfig()
	a.mu.Lock()
	a.cfg = cfg
	a.cfgErr = ""
	if err != nil {
		a.cfgErr = err.Error()
	}
	a.mu.Unlock()
	return a.Bootstrap()
}

// SaveConfig writes what the Settings screen changed to config.json and uses it.
func (a *App) SaveConfig(cfg Config) (Boot, error) {
	cfg = normalize(cfg)
	if err := saveConfig(cfg); err != nil {
		return a.Bootstrap(), err
	}
	a.mu.Lock()
	a.cfg = cfg
	a.cfgErr = ""
	a.mu.Unlock()
	return a.Bootstrap(), nil
}

// DefaultConfig is what a new install starts with (Settings → Reset).
func (a *App) DefaultConfig() Config { return defaultConfig() }

// PickFolder asks for a folder with the system's dialog and returns it as the shell of
// that profile sees it: a WSL tab gets /home/… or /mnt/c/…, and the home folder is ~.
// Empty when the dialog was cancelled.
func (a *App) PickFolder(profileID string) (string, error) {
	return a.pick(profileID, true)
}

// PickFile is PickFolder for a file (an SSH key).
func (a *App) PickFile(profileID string) (string, error) {
	return a.pick(profileID, false)
}

func (a *App) pick(profileID string, folder bool) (string, error) {
	e, err := a.env(profileID)
	if err != nil {
		return "", err
	}
	_ = e.prepare() // learns the home folder; without it the path is just not shortened
	opts := wails.OpenDialogOptions{Title: "Choose a folder", ShowHiddenFiles: !folder}
	if !folder {
		opts.Title = "Choose a file"
	}
	if start := e.dialogDir(); start != "" {
		if st, err := os.Stat(start); err == nil && st.IsDir() {
			opts.DefaultDirectory = start
		}
	}
	var p string
	if folder {
		p, err = wails.OpenDirectoryDialog(a.ctx, opts)
	} else {
		p, err = wails.OpenFileDialog(a.ctx, opts)
	}
	if err != nil || p == "" {
		return "", err
	}
	return e.fromHostPath(p), nil
}

// OpenConfig opens config.json in the system's editor for .json files.
func (a *App) OpenConfig() error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("notepad.exe", configPath())
	case "darwin":
		cmd = exec.Command("open", "-t", configPath())
	default:
		cmd = exec.Command("xdg-open", configPath())
	}
	return cmd.Start()
}

func (a *App) env(profileID string) (*environment, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e, ok := a.envs[profileID]; ok {
		return e, nil
	}
	return nil, errors.New("unknown profile: " + profileID)
}

// NewSession opens a tab's shell and returns its id. cwd is a folder of that profile's
// machine; run is a command to start in it (a launcher), empty for a plain shell.
func (a *App) NewSession(profileID, cwd, run string, cols, rows int) (string, error) {
	e, err := a.env(profileID)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	loadRc := a.cfg.LoadUserRc
	a.mu.Unlock()
	spec, err := e.spawn(cwd, run, loadRc)
	if err != nil {
		return "", err
	}
	return a.sessions.start(spec, cols, rows)
}

// Write sends what you typed (text).
func (a *App) Write(id, data string) error { return a.sessions.write(id, []byte(data)) }

// WriteBinary sends raw bytes (old-style mouse reports), base64-encoded.
func (a *App) WriteBinary(id, b64 string) error {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return err
	}
	return a.sessions.write(id, data)
}

func (a *App) Resize(id string, cols, rows int) error { return a.sessions.resize(id, cols, rows) }
func (a *App) Ack(id string, n int)                   { a.sessions.ack(id, n) }
func (a *App) CloseSession(id string)                 { a.sessions.close(id) }

func (a *App) Ports(profileID string) ([]Port, error) {
	e, err := a.env(profileID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	ports, err := e.ports(ctx)
	if ports == nil {
		ports = []Port{}
	}
	return ports, err
}

func (a *App) KillProcess(profileID string, pid int, force bool) error {
	e, err := a.env(profileID)
	if err != nil {
		return err
	}
	return e.kill(pid, force)
}

func (a *App) SSHHosts(profileID string) ([]SSHHost, error) {
	e, err := a.env(profileID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	hosts, err := e.sshHosts(ctx)
	a.mu.Lock()
	servers := append([]Server(nil), a.cfg.Servers...)
	a.mu.Unlock()
	return mergeHosts(servers, hosts), err
}

func (a *App) Projects(profileID string) ([]Project, error) {
	e, err := a.env(profileID)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	roots := append([]string(nil), a.cfg.ProjectRoots...)
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	projects, err := e.projects(ctx, roots)
	if projects == nil {
		projects = []Project{}
	}
	return projects, err
}

// OpenURL opens a link in the browser. Only web links: a terminal prints whatever a
// program sends, and that must not be able to start other things.
func (a *App) OpenURL(url string) error {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return errors.New("not a web link")
	}
	wails.BrowserOpenURL(a.ctx, url)
	return nil
}
