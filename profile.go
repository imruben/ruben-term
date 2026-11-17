package main

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed shell
var shellFiles embed.FS

// where each embedded shell file goes, under ~/.config/ruben-term
var shellInstall = [][2]string{
	{"shell/launch.sh", "launch.sh"},
	{"shell/zshenv", "zsh/.zshenv"},
	{"shell/zprofile", "zsh/.zprofile"},
	{"shell/zshrc", "zsh/.zshrc"},
	{"shell/git.zsh", "zsh/git.zsh"},
	{"shell/bashrc", "bash/bashrc"},
}

// Profile is a kind of tab: a shell, and the machine the side panel looks at.
type Profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind is "posix" (macOS, Linux, a WSL distro) or "windows" (PowerShell, cmd).
	Kind   string `json:"kind"`
	Distro string `json:"distro,omitempty"`
}

type environment struct {
	Profile
	once    sync.Once
	home    string // the home folder inside the environment
	prepErr error
}

// detectProfiles lists what this machine can open. On Windows the WSL distros come
// first (the default one on top), then PowerShell and cmd.
func detectProfiles() []Profile {
	if runtime.GOOS != "windows" {
		name := filepath.Base(os.Getenv("SHELL"))
		if name == "" || name == "." {
			name = "shell"
		}
		return []Profile{{ID: "shell", Name: name, Kind: "posix"}}
	}
	var out []Profile
	for _, d := range wslDistros() {
		out = append(out, Profile{ID: "wsl:" + d, Name: d, Kind: "posix", Distro: d})
	}
	out = append(out,
		Profile{ID: "powershell", Name: "PowerShell", Kind: "windows"},
		Profile{ID: "cmd", Name: "Command Prompt", Kind: "windows"},
	)
	return out
}

func wslDistros() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "wsl.exe", "-l", "-q")
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	hideWindow(cmd)
	raw, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseDistros(raw)
}

// parseDistros reads `wsl -l -q`. Older builds ignore WSL_UTF8 and answer in UTF-16:
// dropping the NUL bytes is enough for distro names.
func parseDistros(raw []byte) []string {
	text := strings.ReplaceAll(string(bytes.ReplaceAll(raw, []byte{0}, nil)), string(rune(0xFEFF)), "")
	var out []string
	for _, line := range strings.Split(text, "\n") {
		name := strings.TrimSpace(strings.Trim(line, "\xff\xfe"))
		if name == "" || strings.HasPrefix(name, "docker-desktop") {
			continue
		}
		out = append(out, name)
	}
	return out
}

func (e *environment) isWSL() bool { return e.Distro != "" }

// run feeds a POSIX script to sh inside the environment and returns what it prints.
// The script goes through stdin, so nothing has to survive Windows' quoting.
func (e *environment) run(ctx context.Context, script string) (string, error) {
	if e.Kind != "posix" {
		return "", errors.New("not a posix environment")
	}
	var cmd *exec.Cmd
	if e.isWSL() {
		cmd = exec.CommandContext(ctx, "wsl.exe", "-d", e.Distro, "--cd", "~", "-e", "sh", "-s")
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-s")
	}
	hideWindow(cmd)
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(strings.ReplaceAll(stderr.String(), "\x00", ""))
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), errors.New(msg)
	}
	return stdout.String(), nil
}

// prepare installs ruben-term's shell files in the environment (once per app run) and
// learns its home folder.
func (e *environment) prepare() error {
	e.once.Do(func() { e.prepErr = e.install() })
	return e.prepErr
}

func (e *environment) install() error {
	if e.Kind != "posix" {
		home, err := os.UserHomeDir()
		e.home = home
		return err
	}
	if !e.isWSL() {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		e.home = home
		base := filepath.Join(home, ".config", "ruben-term")
		for _, f := range shellInstall {
			dst := filepath.Join(base, filepath.FromSlash(f[1]))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(dst, shellFile(f[0]), 0o644); err != nil {
				return err
			}
		}
		return nil
	}
	// WSL: write the files from inside, with here-documents, and print $HOME
	var sb strings.Builder
	sb.WriteString("set -e\nd=\"$HOME/.config/ruben-term\"\nmkdir -p \"$d/zsh\" \"$d/bash\"\n")
	for _, f := range shellInstall {
		fmt.Fprintf(&sb, "cat > \"$d/%s\" <<'RUBEN_TERM_EOF'\n%sRUBEN_TERM_EOF\n", f[1], shellFile(f[0]))
	}
	sb.WriteString("printf '%s' \"$HOME\"\n")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) // a stopped distro has to boot
	defer cancel()
	out, err := e.run(ctx, sb.String())
	if err != nil {
		return fmt.Errorf("%s: %w", e.Name, err)
	}
	e.home = strings.TrimSpace(out)
	if !strings.HasPrefix(e.home, "/") {
		return fmt.Errorf("%s: could not find the home folder", e.Name)
	}
	return nil
}

// shellFile is an embedded shell file with unix line ends and a final newline.
func shellFile(name string) []byte {
	data, _ := shellFiles.ReadFile(name)
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	return data
}

type spawnSpec struct {
	path string
	args []string
	env  []string
	dir  string
}

// spawn says how to start a tab: in folder cwd (of the environment), running `run`
// first when a launcher asked for it.
func (e *environment) spawn(cwd, run string, loadUserRc bool) (spawnSpec, error) {
	if err := e.prepare(); err != nil {
		return spawnSpec{}, err
	}
	env := os.Environ()
	set := func(k, v string) { env = setEnv(env, k, v) }

	if e.Kind == "windows" {
		spec := spawnSpec{dir: e.home}
		if st, err := os.Stat(cwd); cwd != "" && err == nil && st.IsDir() {
			spec.dir = cwd
		}
		set("TERM_PROGRAM", "ruben-term")
		spec.env = env
		if e.ID == "cmd" {
			spec.path = lookPath("cmd.exe")
			if run != "" {
				spec.args = []string{"/K", run}
			}
			return spec, nil
		}
		spec.path = lookPath("pwsh.exe")
		if spec.path == "" {
			spec.path = lookPath("powershell.exe")
		}
		spec.args = []string{"-NoLogo"}
		if run != "" {
			spec.args = append(spec.args, "-NoExit", "-Command", run)
		}
		if spec.path == "" {
			return spec, errors.New("PowerShell not found")
		}
		return spec, nil
	}

	passed := []string{"RUBEN_TERM_CWD", "RUBEN_TERM_RUN", "RUBEN_TERM_INHERIT"}
	set("RUBEN_TERM_CWD", cwd)
	set("RUBEN_TERM_RUN", run)
	if loadUserRc {
		set("RUBEN_TERM_INHERIT", "1")
	} else {
		set("RUBEN_TERM_INHERIT", "")
	}
	launch := e.home + "/.config/ruben-term/launch.sh"

	if e.isWSL() {
		// WSLENV names the Windows variables that cross into the distro
		wslenv := os.Getenv("WSLENV")
		for _, k := range passed {
			if wslenv != "" {
				wslenv += ":"
			}
			wslenv += k + "/u"
		}
		set("WSLENV", wslenv)
		path := lookPath("wsl.exe")
		if path == "" {
			return spawnSpec{}, errors.New("wsl.exe not found")
		}
		return spawnSpec{
			path: path,
			args: []string{"-d", e.Distro, "--cd", "~", "-e", "sh", launch},
			env:  env,
		}, nil
	}

	set("TERM", "xterm-256color")
	if os.Getenv("LANG") == "" && os.Getenv("LC_ALL") == "" {
		set("LANG", "en_US.UTF-8") // an app started from Finder has no locale
	}
	return spawnSpec{path: "/bin/sh", args: []string{launch}, env: env, dir: e.home}, nil
}

func lookPath(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// setEnv sets k=v in an environment list (an empty v removes it).
func setEnv(env []string, k, v string) []string {
	out := env[:0:0]
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		same := name == k
		if runtime.GOOS == "windows" {
			same = strings.EqualFold(name, k)
		}
		if !same {
			out = append(out, kv)
		}
	}
	if v != "" {
		out = append(out, k+"="+v)
	}
	return out
}
