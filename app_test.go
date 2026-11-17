package main

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParsePortsLinux(t *testing.T) {
	out := `@ss
LISTEN 0      4096       127.0.0.1:5432       0.0.0.0:*
LISTEN 0      511                *:3000             *:*    users:(("node",pid=4242,fd=20))
LISTEN 0      511             [::]:3000          [::]:*    users:(("node",pid=4242,fd=21))
LISTEN 0      4096   127.0.0.53%lo:53         0.0.0.0:*    users:(("systemd-resolve",pid=180,fd=14))
LISTEN 0      128        127.0.0.1:8080       0.0.0.0:*    users:(("my app",pid=77,fd=3))
@cwd
4242	/home/me/dev/projects/gilito
77	/
@docker
postgres|0.0.0.0:5432->5432/tcp, [::]:5432->5432/tcp
redis|6379/tcp
`
	got := parsePorts(out)
	if len(got) != 4 {
		t.Fatalf("want 4 ports, got %d: %+v", len(got), got)
	}
	if p := got[0]; p.Port != 53 || p.Addr != "127.0.0.53" || p.Process != "systemd-resolve" {
		t.Errorf("port 53: %+v", p)
	}
	if p := got[1]; p.Port != 3000 || p.Addr != "*" || p.PID != 4242 || p.Project != "gilito" {
		t.Errorf("port 3000: %+v", p)
	}
	if p := got[2]; p.Port != 5432 || p.Docker != "postgres" || p.PID != 0 {
		t.Errorf("port 5432: %+v", p)
	}
	if p := got[3]; p.Process != "my app" || p.Project != "" {
		t.Errorf("port 8080: %+v", p)
	}
}

func TestParsePortsMac(t *testing.T) {
	out := `@lsof
COMMAND   PID USER   FD   TYPE             DEVICE SIZE/OFF NODE NAME
node    91234   me   23u  IPv6 0x1234567890abcdef      0t0  TCP *:5173 (LISTEN)
ControlCe 512   me   10u  IPv4 0x1234567890abcdee      0t0  TCP 127.0.0.1:7000 (LISTEN)
@cwd
p91234
fcwd
n/Users/me/code/portfolio
p512
fcwd
n/
@docker
`
	got := parsePorts(out)
	if len(got) != 2 {
		t.Fatalf("want 2 ports, got %+v", got)
	}
	if p := got[0]; p.Port != 5173 || p.Process != "node" || p.Cwd != "/Users/me/code/portfolio" || p.Project != "portfolio" {
		t.Errorf("port 5173: %+v", p)
	}
	if p := got[1]; p.Port != 7000 || p.Addr != "127.0.0.1" || p.Project != "" {
		t.Errorf("port 7000: %+v", p)
	}
}

func TestParseNetstat(t *testing.T) {
	out := "\r\nConexiones activas\r\n\r\n  Proto  Dirección local   Dirección remota   Estado   PID\r\n" +
		"  TCP    0.0.0.0:135            0.0.0.0:0              LISTENING       1234\r\n" +
		"  TCP    127.0.0.1:5173         0.0.0.0:0              ESCUCHANDO      5678\r\n" +
		"  TCP    192.168.1.5:50123      140.82.112.26:443      ESTABLISHED     999\r\n" +
		"  TCP    [::]:135               [::]:0                 LISTENING       1234\r\n" +
		"  UDP    0.0.0.0:5353           *:*                                    4321\r\n"
	names := parseTasklist("\"svchost.exe\",\"1234\",\"Services\",\"0\",\"12.345 KB\"\r\n\"node.exe\",\"5678\",\"Console\",\"1\",\"80.000 KB\"\r\n")
	got := parseNetstat(out, names)
	if len(got) != 2 {
		t.Fatalf("want 2 listeners, got %+v", got)
	}
	if got[0].Port != 135 || got[0].Process != "svchost" || got[1].Port != 5173 || got[1].Process != "node" {
		t.Errorf("got %+v", got)
	}
}

func TestParseSSHConfig(t *testing.T) {
	got := parseSSHConfig(`
# my servers
Host *
  ServerAliveInterval 30

Host vps  backup
    HostName 203.0.113.7
    User deploy
    Port 2222

Host github.com
	HostName=github.com
	User git
Host *.internal !bastion
  User ops
`)
	if len(got) != 3 {
		t.Fatalf("want 3 hosts, got %+v", got)
	}
	if got[0].Name != "vps" || got[0].HostName != "203.0.113.7" || got[0].User != "deploy" || got[0].Port != "2222" {
		t.Errorf("vps: %+v", got[0])
	}
	if got[1].Name != "backup" || got[1].User != "deploy" {
		t.Errorf("backup: %+v", got[1])
	}
	if got[2].Name != "github.com" || got[2].HostName != "github.com" || got[2].User != "git" {
		t.Errorf("github: %+v", got[2])
	}
}

func TestParseDistros(t *testing.T) {
	utf16 := []byte("U\x00b\x00u\x00n\x00t\x00u\x00-\x002\x004\x00.\x000\x004\x00\r\x00\n\x00d\x00o\x00c\x00k\x00e\x00r\x00-\x00d\x00e\x00s\x00k\x00t\x00o\x00p\x00\r\x00\n\x00")
	for _, raw := range [][]byte{utf16, []byte("Ubuntu-24.04\r\ndocker-desktop\r\n")} {
		got := parseDistros(raw)
		if len(got) != 1 || got[0] != "Ubuntu-24.04" {
			t.Errorf("got %q", got)
		}
	}
}

func TestShQuote(t *testing.T) {
	for in, want := range map[string]string{
		"~":           `"$HOME"`,
		"~/dev":       `"$HOME"'/dev'`,
		"/srv/it's":   `'/srv/it'\''s'`,
		"/a b/$(rm)c": `'/a b/$(rm)c'`,
	} {
		if got := shQuote(in); got != want {
			t.Errorf("shQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestSetEnv(t *testing.T) {
	env := setEnv([]string{"A=1", "B=2"}, "A", "3")
	if strings.Join(env, ",") != "B=2,A=3" {
		t.Errorf("got %v", env)
	}
	if env = setEnv(env, "B", ""); strings.Join(env, ",") != "A=3" {
		t.Errorf("got %v", env)
	}
}

// The real thing on this machine: scripts run, the panels answer.
func TestPosixEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix only")
	}
	e := &environment{Profile: Profile{ID: "shell", Kind: "posix"}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := e.run(ctx, "printf '%s' \"it's $((40+2))\"\n")
	if err != nil || out != "it's 42" {
		t.Fatalf("run: %q %v", out, err)
	}
	if _, err := e.ports(ctx); err != nil {
		t.Errorf("ports: %v", err)
	}
	if _, err := e.sshHosts(ctx); err != nil {
		t.Errorf("ssh hosts: %v", err)
	}
	dir := t.TempDir()
	if _, err := e.run(ctx, "mkdir -p '"+dir+"/one/.git' '"+dir+"/two words'\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run(ctx, "mkdir -p '"+dir+"/two words/inner/.git'\n"); err != nil {
		t.Fatal(err)
	}
	// the second root is a folder of the first: it is a group, not a project
	projects, err := e.projects(ctx, []string{dir, dir + "/missing", dir + "/two words"})
	if err != nil || len(projects) != 2 || projects[0].Name != "one" || !projects[0].Git ||
		projects[0].Root != filepath.Base(dir) || projects[1].Name != "inner" || projects[1].Root != "two words" {
		t.Errorf("projects: %+v %v", projects, err)
	}
}

// A session: output arrives, input is read, the size is right, the end is reported.
func TestSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix only")
	}
	var mu sync.Mutex
	var out strings.Builder
	exit := make(chan int, 1)
	m := newSessions(func(event string, args ...any) {
		switch event {
		case "pty:data":
			b, _ := base64.StdEncoding.DecodeString(args[1].(string))
			mu.Lock()
			out.Write(b)
			mu.Unlock()
		case "pty:exit":
			exit <- args[1].(int)
		}
	})
	id, err := m.start(spawnSpec{path: "/bin/sh", args: []string{"-c", "stty size; read x; echo \"got:$x\"; exit 3"}}, 100, 30, false)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := m.write(id, []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-exit:
		if code != 3 {
			t.Errorf("exit code %d, want 3", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not end")
	}
	mu.Lock()
	got := out.String()
	mu.Unlock()
	if !strings.Contains(got, "30 100") || !strings.Contains(got, "got:hello") {
		t.Errorf("output: %q", got)
	}
	if _, err := m.get(id); err == nil {
		t.Error("the ended session is still listed")
	}
}

// Closing a tab ends its program.
func TestSessionClose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix only")
	}
	exit := make(chan int, 1)
	m := newSessions(func(event string, args ...any) {
		if event == "pty:exit" {
			exit <- args[1].(int)
		}
	})
	id, err := m.start(spawnSpec{path: "/bin/sh", args: []string{"-c", "sleep 60"}}, 80, 24, false)
	if err != nil {
		t.Fatal(err)
	}
	m.close(id)
	select {
	case <-exit:
	case <-time.After(10 * time.Second):
		t.Fatal("closing did not end the program")
	}
}

func TestWSLPath(t *testing.T) {
	cases := map[string]string{
		`\\wsl.localhost\Ubuntu-24.04\home\me\dev`: "/home/me/dev",
		`\\wsl$\Ubuntu\home\me\`:                   "/home/me",
		`\\wsl.localhost\Ubuntu`:                   "/",
		`C:\Users\me\code`:                         "/mnt/c/Users/me/code",
		`D:\`:                                      "/mnt/d",
		"/already/posix":                           "/already/posix",
	}
	for in, want := range cases {
		if got := wslPath(in); got != want {
			t.Errorf("wslPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShortenHome(t *testing.T) {
	if got := shortenHome("/home/me", "/home/me/dev/projects", false); got != "~/dev/projects" {
		t.Errorf("got %q", got)
	}
	if got := shortenHome("/home/me", "/home/me", false); got != "~" {
		t.Errorf("got %q", got)
	}
	if got := shortenHome("/home/me", "/home/meow", false); got != "/home/meow" {
		t.Errorf("got %q", got)
	}
	if got := shortenHome(`C:\Users\Me`, `c:\users\me\code\x`, true); got != "~/code/x" {
		t.Errorf("got %q", got)
	}
}

func TestNormalizeAndMergeHosts(t *testing.T) {
	cfg := defaultConfig()
	cfg.ProjectRoots = []string{" ~/dev ", "", "~/dev", "~/work"}
	cfg.Launchers = []Launcher{{Name: "", Command: "htop"}, {Name: "empty", Command: " "}}
	cfg.Servers = []Server{{Name: "", HostName: "203.0.113.7", User: "deploy"}, {Name: "nohost"}}
	cfg.Theme, cfg.FontSize = "pink", 99
	got := normalize(cfg)
	if len(got.ProjectRoots) != 2 || got.ProjectRoots[0] != "~/dev" || got.ProjectRoots[1] != "~/work" {
		t.Errorf("roots: %q", got.ProjectRoots)
	}
	if len(got.Launchers) != 1 || got.Launchers[0].Name != "htop" {
		t.Errorf("launchers: %+v", got.Launchers)
	}
	if len(got.Servers) != 1 || got.Servers[0].Name != "203.0.113.7" {
		t.Errorf("servers: %+v", got.Servers)
	}
	if got.Theme != "dark" || got.FontSize != 13 {
		t.Errorf("theme %q size %d", got.Theme, got.FontSize)
	}
	hosts := mergeHosts([]Server{{Name: "vps", HostName: "1.2.3.4"}}, []SSHHost{{Name: "vps"}, {Name: "gh"}})
	if len(hosts) != 2 || !hosts[0].Saved || hosts[0].HostName != "1.2.3.4" || hosts[1].Name != "gh" {
		t.Errorf("hosts: %+v", hosts)
	}
}

func TestFontFamilies(t *testing.T) {
	got := fontFamilies("Cascadia Code\r\nCascadia Code Light\nconsolas\nDejaVu Sans Mono,DejaVu Sans Mono Book\n.SF NS\n@MS Gothic\nCascadia Code\nUbuntu Mono Medium\nUbuntu Mono\nArial Black\n")
	want := []string{"Cascadia Code", "consolas", "DejaVu Sans Mono", "Ubuntu Mono"}
	// "Arial Black" stays: there is no "Arial" to fold it into
	want = append([]string{"Arial Black"}, want...)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}
