package main

import (
	"encoding/base64"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// RT_REAL=1 go test -run TestRealShell -v
// Opens a real tab against your own home: ruben-term's zsh must come up without
// terminal-setup (no tmux, none of its variables) and still have your PATH.
func TestRealShell(t *testing.T) {
	if os.Getenv("RT_REAL") == "" {
		t.Skip("set RT_REAL=1")
	}
	e := &environment{Profile: Profile{ID: "shell", Kind: "posix"}}
	spec, err := e.spawn(os.Getenv("RT_CWD"), "echo RUN:$RUBEN_TERM:${TERMINAL_SETUP:-none}:${TMUX:-notmux}:$ZDOTDIR", "", true)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var out strings.Builder
	exit := make(chan int, 1)
	m := newSessions(func(event string, args ...any) {
		if event == "pty:data" {
			b, _ := base64.StdEncoding.DecodeString(args[1].(string))
			mu.Lock()
			out.Write(b)
			mu.Unlock()
		} else {
			exit <- args[1].(int)
		}
	})
	start := time.Now()
	id, err := m.start(spec, 120, 30, false)
	if err != nil {
		t.Fatal(err)
	}
	wait := func(s string) {
		for i := 0; i < 150; i++ {
			mu.Lock()
			ok := strings.Contains(out.String(), s)
			mu.Unlock()
			if ok {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Errorf("never saw %q", s)
	}
	wait("RUN:1:")
	t.Logf("first prompt + launcher after %v", time.Since(start).Round(time.Millisecond))
	m.write(id, []byte("print -r -- \"CHK:$(whence -p claude):$(whence -p opencode):$(whence -p go):${+functions[nvm]}:${+functions[update]}:${+aliases[makec]}:$HISTFILE:GIT=${+aliases[gl]}${+functions[gco]}${+functions[gpr]}:$(whence -w code):$(code --version 2>&1 | head -1)\"\r"))
	wait("CHK:/")
	m.write(id, []byte("exit\r"))
	select {
	case <-exit:
	case <-time.After(10 * time.Second):
		t.Error("exit did not end the session")
	}
	mu.Lock()
	defer mu.Unlock()
	t.Logf("\n%q", out.String())
}
