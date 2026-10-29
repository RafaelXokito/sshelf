//go:build devcheck

// Command devcheck is a throwaway program for the T-000 manual exit check
// (ADR-007). It runs a Bubble Tea v2 program that hands the terminal to a stub
// tea.ExecCommand, which enters raw mode and prints every byte it receives as
// hex until it reads 'q'. Run it with:
//
//	go run -tags devcheck ./internal/session/devcheck
package main

import (
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

// stub is a tea.ExecCommand that echoes raw input bytes as hex.
type stub struct {
	stdin  io.Reader
	stdout io.Writer
}

func (s *stub) SetStdin(r io.Reader)  { s.stdin = r }
func (s *stub) SetStdout(w io.Writer) { s.stdout = w }
func (s *stub) SetStderr(io.Writer)   {}

func (s *stub) Run() error {
	in, out := s.stdin, s.stdout
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	f, ok := in.(interface{ Fd() uintptr })
	if !ok {
		f = os.Stdin
	}
	fd := int(f.Fd()) //nolint:gosec // a file descriptor fits in an int
	state, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("raw mode: %w", err)
	}
	defer func() { _ = term.Restore(fd, state) }()

	_, _ = fmt.Fprint(out, "stub session: raw mode on; type keys (Ctrl+C should print 03), q returns\r\n")
	buf := make([]byte, 64)
	for {
		n, err := in.Read(buf)
		for _, b := range buf[:n] {
			_, _ = fmt.Fprintf(out, "%02x ", b)
			if b == 'q' {
				_, _ = fmt.Fprint(out, "\r\n")
				return nil
			}
		}
		if err != nil {
			return err
		}
	}
}

type execDoneMsg struct{ err error }

type model struct {
	runs    int
	lastKey string
	lastErr error
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		m.lastKey = msg.String()
		switch msg.String() {
		case "e":
			return m, tea.Exec(&stub{}, func(err error) tea.Msg { return execDoneMsg{err} })
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	case execDoneMsg:
		m.runs++
		m.lastKey = ""
		m.lastErr = msg.err
	}
	return m, nil
}

func (m model) View() tea.View {
	s := fmt.Sprintf("sshelf devcheck (alt screen)\n\n"+
		"e  hand the terminal to the stub session\n"+
		"q  quit\n\n"+
		"stub runs: %d\nlast error: %v\nlast key since return: %q\n",
		m.runs, m.lastErr, m.lastKey)
	v := tea.NewView(s)
	v.AltScreen = true
	return v
}

func main() {
	if _, err := tea.NewProgram(model{}).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "devcheck:", err)
		os.Exit(1)
	}
}
