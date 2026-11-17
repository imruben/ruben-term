package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SSHHost is a Host of ~/.ssh/config, or a server added in Settings (Saved).
type SSHHost struct {
	Name     string `json:"name"`
	HostName string `json:"hostName"`
	User     string `json:"user"`
	Port     string `json:"port"`
	Identity string `json:"identity,omitempty"`
	Saved    bool   `json:"saved,omitempty"`
}

// mergeHosts puts the servers of Settings first; a host of ~/.ssh/config with the same
// name is left out.
func mergeHosts(servers []Server, fromFile []SSHHost) []SSHHost {
	out := make([]SSHHost, 0, len(servers)+len(fromFile))
	seen := map[string]bool{}
	for _, s := range servers {
		seen[s.Name] = true
		out = append(out, SSHHost{Name: s.Name, HostName: s.HostName, User: s.User, Port: s.Port, Identity: s.Identity, Saved: true})
	}
	for _, h := range fromFile {
		if !seen[h.Name] {
			out = append(out, h)
		}
	}
	return out
}

// parseSSHConfig lists the hosts you can name: patterns (*, ?, !) are rules, not hosts.
func parseSSHConfig(text string) []SSHHost {
	var hosts []SSHHost
	var current []int // the hosts the lines below belong to
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, _ := strings.Cut(line, " ")
		if k, v, ok := strings.Cut(line, "="); ok && !strings.Contains(k, " ") {
			key, val = k, v
		} else if k, v, ok := strings.Cut(line, "\t"); ok && !strings.Contains(k, " ") {
			key, val = k, v
		}
		val = strings.Trim(strings.TrimSpace(val), `"`)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "host":
			current = current[:0]
			for _, name := range strings.Fields(val) {
				if strings.ContainsAny(name, "*?!") || seen[name] {
					continue
				}
				seen[name] = true
				hosts = append(hosts, SSHHost{Name: name})
				current = append(current, len(hosts)-1)
			}
		case "match":
			current = current[:0]
		case "hostname":
			for _, i := range current {
				if hosts[i].HostName == "" {
					hosts[i].HostName = val
				}
			}
		case "user":
			for _, i := range current {
				if hosts[i].User == "" {
					hosts[i].User = val
				}
			}
		case "port":
			for _, i := range current {
				if hosts[i].Port == "" {
					hosts[i].Port = val
				}
			}
		}
	}
	return hosts
}

func (e *environment) sshHosts(ctx context.Context) ([]SSHHost, error) {
	if e.Kind == "posix" {
		out, err := e.run(ctx, "cat \"$HOME/.ssh/config\" 2>/dev/null\nexit 0\n")
		if err != nil {
			return nil, err
		}
		return parseSSHConfig(out), nil
	}
	home, _ := os.UserHomeDir()
	data, _ := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	return parseSSHConfig(string(data)), nil
}

// Project is a folder inside one of the configured roots.
type Project struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Git  bool   `json:"git"`
	Root string `json:"root"` // the group: the last part of the root folder
}

// shQuote quotes a path for sh; a leading ~ becomes $HOME.
func shQuote(p string) string {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	if p == "~" {
		return `"$HOME"`
	}
	if strings.HasPrefix(p, "~/") {
		return `"$HOME"` + q(p[1:])
	}
	return q(p)
}

// parseProjects reads lines "R<TAB>root" (a root that exists, then its folders) and
// "0|1<TAB>folder" (1: a git repo). Groups keep the order of the config; inside each,
// repos first, then by name. A root listed inside another root is not a project.
func parseProjects(out string) []Project {
	lines := strings.Split(out, "\n")
	roots := map[string]bool{}
	for _, line := range lines {
		if f := strings.SplitN(strings.TrimRight(line, "\r"), "\t", 2); len(f) == 2 && f[0] == "R" {
			roots[f[1]] = true
		}
	}
	base := func(p string) string { return p[strings.LastIndexAny(p, `/\`)+1:] }
	var res []Project
	seen := map[string]bool{}
	root, start := "", 0
	sortGroup := func() {
		group := res[start:]
		sort.SliceStable(group, func(i, j int) bool {
			if group[i].Git != group[j].Git {
				return group[i].Git
			}
			return strings.ToLower(group[i].Name) < strings.ToLower(group[j].Name)
		})
	}
	for _, line := range lines {
		f := strings.SplitN(strings.TrimRight(line, "\r"), "\t", 2)
		if len(f) < 2 || f[1] == "" {
			continue
		}
		if f[0] == "R" {
			sortGroup()
			root, start = base(f[1]), len(res)
			continue
		}
		if seen[f[1]] || roots[f[1]] {
			continue
		}
		seen[f[1]] = true
		res = append(res, Project{Name: base(f[1]), Path: f[1], Git: f[0] == "1", Root: root})
	}
	sortGroup()
	return res
}

func (e *environment) projects(ctx context.Context, roots []string) ([]Project, error) {
	if e.Kind == "posix" {
		var sb strings.Builder
		for _, r := range roots {
			if strings.TrimSpace(r) == "" {
				continue
			}
			q := shQuote(strings.TrimRight(r, "/"))
			sb.WriteString("[ -d " + q + " ] && printf 'R\\t%s\\n' " + q + "\n" +
				"for d in " + q + "/*/; do\n" +
				"  [ -d \"$d\" ] || continue\n  d=${d%/}\n" +
				"  if [ -e \"$d/.git\" ]; then g=1; else g=0; fi\n" +
				"  printf '%s\\t%s\\n' \"$g\" \"$d\"\ndone\n")
		}
		sb.WriteString("exit 0\n")
		out, err := e.run(ctx, sb.String())
		if err != nil {
			return nil, err
		}
		return parseProjects(out), nil
	}
	home, _ := os.UserHomeDir()
	var sb strings.Builder
	for _, r := range roots {
		if r == "~" || strings.HasPrefix(r, "~/") {
			r = filepath.Join(home, filepath.FromSlash(r[1:]))
		}
		entries, err := os.ReadDir(r)
		if err != nil {
			continue
		}
		sb.WriteString("R\t" + filepath.Clean(r) + "\n")
		for _, en := range entries {
			if !en.IsDir() || strings.HasPrefix(en.Name(), ".") {
				continue
			}
			full := filepath.Join(r, en.Name())
			g := "0"
			if _, err := os.Stat(filepath.Join(full, ".git")); err == nil {
				g = "1"
			}
			sb.WriteString(g + "\t" + full + "\n")
		}
	}
	return parseProjects(sb.String()), nil
}
