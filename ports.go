package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Port is something listening on a TCP port.
type Port struct {
	Port    int    `json:"port"`
	Addr    string `json:"addr"` // "*" when it listens on every interface
	PID     int    `json:"pid"`
	Process string `json:"process"`
	Cwd     string `json:"cwd"`     // the folder the program runs in
	Project string `json:"project"` // its last part: usually the project
	Docker  string `json:"docker"`  // the container that publishes the port
}

// One script, one answer: the listeners, the folder of each program, and the ports
// Docker publishes. ss on Linux, lsof on macOS. Docker gets two seconds, no more.
const portsScript = `
if command -v ss >/dev/null 2>&1; then
  echo "@ss"
  out=$(ss -H -ltnp 2>/dev/null)
  printf '%s\n' "$out"
  echo "@cwd"
  printf '%s\n' "$out" | grep -o 'pid=[0-9]*' | cut -d= -f2 | sort -u | while read -r p; do
    c=$(readlink "/proc/$p/cwd" 2>/dev/null) && printf '%s\t%s\n' "$p" "$c"
  done
else
  echo "@lsof"
  out=$(lsof -nP -iTCP -sTCP:LISTEN 2>/dev/null)
  printf '%s\n' "$out"
  echo "@cwd"
  pids=$(printf '%s\n' "$out" | awk 'NR>1 {print $2}' | sort -u | paste -sd, -)
  [ -n "$pids" ] && lsof -a -d cwd -Fpn -p "$pids" 2>/dev/null
fi
echo "@docker"
if command -v docker >/dev/null 2>&1; then
  docker ps --format '{{.Names}}|{{.Ports}}' 2>/dev/null &
  dp=$!
  ( sleep 2; kill "$dp" ) >/dev/null 2>&1 &
  kp=$!
  wait "$dp" 2>/dev/null
  kill "$kp" >/dev/null 2>&1
fi
exit 0
`

var ssUsers = regexp.MustCompile(`\(\("([^"]*)",pid=(\d+)`)

// parsePorts reads the output of portsScript.
func parsePorts(out string) []Port {
	type key struct{ port, pid int }
	seen := map[key]*Port{}
	var order []key
	cwds := map[int]string{}
	docker := map[int]string{}

	add := func(addr string, port, pid int, proc string) {
		k := key{port, pid}
		if p, ok := seen[k]; ok { // the same program on IPv4 and IPv6
			if addr == "*" {
				p.Addr = "*"
			}
			return
		}
		seen[k] = &Port{Port: port, Addr: addr, PID: pid, Process: proc}
		order = append(order, k)
	}

	section, lsofPID := "", 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "@") {
			section = line
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		switch section {
		case "@ss": // LISTEN 0 511 127.0.0.1:3000 0.0.0.0:* users:(("node",pid=12,fd=20))
			f := strings.Fields(line)
			if len(f) < 4 {
				continue
			}
			addr, port, ok := splitAddr(f[3])
			if !ok {
				continue
			}
			proc, pid := "", 0
			if m := ssUsers.FindStringSubmatch(line); m != nil {
				proc = m[1]
				pid, _ = strconv.Atoi(m[2])
			}
			add(addr, port, pid, proc)
		case "@lsof": // node 12 me 23u IPv4 0x… 0t0 TCP *:3000 (LISTEN)
			f := strings.Fields(line)
			if len(f) < 9 || f[0] == "COMMAND" {
				continue
			}
			pid, err := strconv.Atoi(f[1])
			if err != nil {
				continue
			}
			addr, port, ok := splitAddr(f[len(f)-2])
			if !ok {
				continue
			}
			add(addr, port, pid, strings.ReplaceAll(f[0], `\x20`, " "))
		case "@cwd":
			if pid, dir, ok := strings.Cut(line, "\t"); ok { // Linux: pid<TAB>folder
				if n, err := strconv.Atoi(pid); err == nil {
					cwds[n] = dir
				}
				continue
			}
			switch line[0] { // lsof -F: p<pid>, then n<folder>
			case 'p':
				lsofPID, _ = strconv.Atoi(line[1:])
			case 'n':
				if lsofPID != 0 {
					cwds[lsofPID] = line[1:]
				}
			}
		case "@docker": // name|0.0.0.0:5432->5432/tcp, [::]:5432->5432/tcp
			name, ports, ok := strings.Cut(line, "|")
			if !ok {
				continue
			}
			for _, p := range strings.Split(ports, ",") {
				host, _, ok := strings.Cut(strings.TrimSpace(p), "->")
				if !ok {
					continue
				}
				if _, port, ok := splitAddr(host); ok {
					docker[port] = name
				}
			}
		}
	}

	res := make([]Port, 0, len(order))
	for _, k := range order {
		p := *seen[k]
		p.Cwd = cwds[p.PID]
		if p.Cwd != "" && p.Cwd != "/" {
			p.Project = path.Base(p.Cwd)
		}
		p.Docker = docker[p.Port]
		res = append(res, p)
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].Port < res[j].Port })
	return res
}

// splitAddr splits "127.0.0.1:3000", "[::]:3000", "*:3000" or "127.0.0.53%lo:53".
func splitAddr(s string) (addr string, port int, ok bool) {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "", 0, false
	}
	port, err := strconv.Atoi(s[i+1:])
	if err != nil {
		return "", 0, false
	}
	addr = strings.Trim(s[:i], "[]")
	if j := strings.Index(addr, "%"); j >= 0 {
		addr = addr[:j]
	}
	switch addr {
	case "0.0.0.0", "::", "*", "":
		addr = "*"
	}
	return addr, port, true
}

// parseNetstat reads Windows' `netstat -ano`. A listener is a TCP line whose remote
// end is port 0: the word for LISTENING changes with the language of Windows.
func parseNetstat(out string, names map[int]string) []Port {
	type key struct{ port, pid int }
	seen := map[key]bool{}
	var res []Port
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "TCP" {
			continue
		}
		if _, rport, ok := splitAddr(f[2]); !ok || rport != 0 {
			continue
		}
		addr, port, ok := splitAddr(f[1])
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(f[len(f)-1])
		if err != nil || seen[key{port, pid}] {
			continue
		}
		seen[key{port, pid}] = true
		res = append(res, Port{Port: port, Addr: addr, PID: pid, Process: strings.TrimSuffix(names[pid], ".exe")})
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].Port < res[j].Port })
	return res
}

// parseTasklist reads `tasklist /FO CSV /NH`: "name.exe","pid",…
func parseTasklist(out string) map[int]string {
	names := map[int]string{}
	r := csv.NewReader(strings.NewReader(out))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, _ := r.ReadAll()
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		if pid, err := strconv.Atoi(row[1]); err == nil {
			names[pid] = row[0]
		}
	}
	return names
}

func (e *environment) ports(ctx context.Context) ([]Port, error) {
	if e.Kind == "posix" {
		out, err := e.run(ctx, portsScript)
		if err != nil && out == "" {
			return nil, err
		}
		return parsePorts(out), nil
	}
	ns := exec.CommandContext(ctx, "netstat", "-ano")
	hideWindow(ns)
	out, err := ns.Output()
	if err != nil {
		return nil, err
	}
	tl := exec.CommandContext(ctx, "tasklist", "/FO", "CSV", "/NH")
	hideWindow(tl)
	names, _ := tl.Output()
	return parseNetstat(string(out), parseTasklist(string(names))), nil
}

// kill stops a program by pid: politely, or for good with force.
func (e *environment) kill(pid int, force bool) error {
	if pid <= 1 {
		return errors.New("no pid: the program belongs to another user")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if e.Kind == "posix" {
		sig := "-TERM"
		if force {
			sig = "-KILL"
		}
		_, err := e.run(ctx, fmt.Sprintf("kill %s %d\n", sig, pid))
		return err
	}
	args := []string{"/PID", strconv.Itoa(pid), "/T"}
	if force {
		args = append(args, "/F")
	}
	cmd := exec.CommandContext(ctx, "taskkill", args...)
	hideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}
