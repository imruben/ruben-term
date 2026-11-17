package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aymanbagabas/go-pty"
)

// Output the window has not drawn yet. Past the high mark the reader stops, so a
// program that prints without end (yes, a huge cat) slows down instead of filling memory.
const (
	highWater = 2 << 20
	lowWater  = 512 << 10
)

type session struct {
	id  string
	pty pty.Pty
	cmd *pty.Cmd

	mu      sync.Mutex
	cond    *sync.Cond
	pending int // bytes sent to the window and not acknowledged
	closed  bool
}

type sessions struct {
	mu   sync.Mutex
	byID map[string]*session
	next int
	// emit sends an event to the window
	emit func(event string, args ...any)
}

func newSessions(emit func(string, ...any)) *sessions {
	return &sessions{byID: map[string]*session{}, emit: emit}
}

func (m *sessions) start(spec spawnSpec, cols, rows int) (string, error) {
	if cols < 2 {
		cols = 80
	}
	if rows < 2 {
		rows = 24
	}
	p, err := pty.New()
	if err != nil {
		return "", err
	}
	if err := p.Resize(cols, rows); err != nil {
		p.Close()
		return "", err
	}
	cmd := p.Command(spec.path, spec.args...)
	cmd.Env = spec.env
	cmd.Dir = spec.dir
	if err := cmd.Start(); err != nil {
		p.Close()
		return "", fmt.Errorf("start %s: %w", spec.path, err)
	}

	m.mu.Lock()
	m.next++
	s := &session{id: fmt.Sprintf("s%d", m.next), pty: p, cmd: cmd}
	s.cond = sync.NewCond(&s.mu)
	m.byID[s.id] = s
	m.mu.Unlock()

	done := make(chan struct{})
	go func() { // output → window
		defer close(done)
		buf := make([]byte, 32<<10)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				s.mu.Lock()
				for s.pending > highWater && !s.closed {
					s.cond.Wait()
				}
				s.pending += n
				s.mu.Unlock()
				m.emit("pty:data", s.id, base64.StdEncoding.EncodeToString(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { // the program ended: let the last output through, then close
		code := 0
		if err := cmd.Wait(); err != nil {
			code = 1
			var ee interface{ ExitCode() int }
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			}
		}
		// A ConPTY (and a unix pty whose other end we still hold) never reports the end
		// by itself: closing it is what ends the reader.
		time.Sleep(120 * time.Millisecond)
		s.shut()
		<-done
		m.mu.Lock()
		delete(m.byID, s.id)
		m.mu.Unlock()
		m.emit("pty:exit", s.id, code)
	}()
	return s.id, nil
}

func (s *session) shut() {
	s.mu.Lock()
	already := s.closed
	s.closed = true
	s.cond.Broadcast()
	s.mu.Unlock()
	if !already {
		s.pty.Close()
	}
}

func (m *sessions) get(id string) (*session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.byID[id]
	if !ok {
		return nil, errors.New("no such session")
	}
	return s, nil
}

func (m *sessions) write(id string, data []byte) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	_, err = s.pty.Write(data)
	return err
}

func (m *sessions) resize(id string, cols, rows int) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	if cols < 2 || rows < 2 {
		return nil
	}
	return s.pty.Resize(cols, rows)
}

// ack: the window drew n more bytes.
func (m *sessions) ack(id string, n int) {
	s, err := m.get(id)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.pending -= n
	if s.pending < 0 {
		s.pending = 0
	}
	if s.pending < lowWater {
		s.cond.Broadcast()
	}
	s.mu.Unlock()
}

// close ends a tab: the program gets the hang-up a closed terminal gives.
func (m *sessions) close(id string) {
	s, err := m.get(id)
	if err != nil {
		return
	}
	s.shut()
	if s.cmd.Process != nil {
		proc := s.cmd.Process
		go func() { // whatever ignored the hang-up
			time.Sleep(2 * time.Second)
			_ = proc.Kill()
		}()
	}
}

func (m *sessions) closeAll() {
	m.mu.Lock()
	all := make([]*session, 0, len(m.byID))
	for _, s := range m.byID {
		all = append(all, s)
	}
	m.mu.Unlock()
	for _, s := range all {
		s.shut()
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	}
}
