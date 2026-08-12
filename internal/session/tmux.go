// Package session wraps tmux for the dedicated `-L <server>` model: one
// detached session per card, `loom-<id>` naming, attach/detach handoff
// (ADR-001 §4.4, DESIGN-002 §10.1).
package session

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Tmux struct {
	Server string
	bin    string
}

func New(server string) (Tmux, error) {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		return Tmux{}, fmt.Errorf("session: tmux not found in PATH (install it: 'apt install tmux' or 'brew install tmux')")
	}
	t := Tmux{Server: server, bin: bin}
	out, err := t.run("-V")
	if err != nil {
		return Tmux{}, fmt.Errorf("session: tmux version check: %v", err)
	}
	if !tmuxGE3(out) {
		return Tmux{}, fmt.Errorf("session: tmux %s is too old; need tmux >= 3.x (install/upgrade: 'apt install tmux' or 'brew install tmux')", strings.TrimSpace(out))
	}
	return t, nil
}

func (t Tmux) NewSession(name, cwd, command string) error {
	_, err := t.run("new-session", "-d", "-s", name, "-c", cwd, command)
	if err == nil {
		return nil
	}
	if ok, _ := t.HasSession(name); ok {
		return nil
	}
	// The first new-session on a cold server (which, with exit-empty on, is
	// every first open after the last session ended) can transiently fail
	// with "server exited unexpectedly" — a startup race, not a real error.
	// The failed attempt leaves no session behind, so retry once; a session
	// that exists after a reported failure counts as success (the attempt
	// committed before the client errored).
	time.Sleep(100 * time.Millisecond)
	if _, err = t.run("new-session", "-d", "-s", name, "-c", cwd, command); err != nil {
		if ok, _ := t.HasSession(name); ok {
			return nil
		}
	}
	return err
}

func (t Tmux) HasSession(name string) (bool, error) {
	_, err := t.run("has-session", "-t", name)
	if err == nil {
		return true, nil
	}
	if MissingServer(err) {
		return false, err
	}
	if te, ok := err.(*tmuxError); ok && te.code == 1 {
		return false, nil
	}
	return false, err
}

func (t Tmux) CapturePane(name string) string {
	out, err := t.run("capture-pane", "-p", "-t", name)
	if err != nil {
		return ""
	}
	return out
}

func (t Tmux) SendKeys(name, keys string) error {
	_, err := t.run("send-keys", "-t", name, keys)
	return err
}

func (t Tmux) KillSession(name string) error {
	_, err := t.run("kill-session", "-t", name)
	return err
}

func (t Tmux) ListSessions() ([]string, error) {
	out, err := t.run("list-sessions", "-F", "#{session_name}")
	if err != nil {
		if MissingServer(err) {
			return nil, nil
		}
		if te, ok := err.(*tmuxError); ok && te.code == 1 {
			return nil, nil
		}
		return nil, err
	}
	return parseSessionNames(out), nil
}

func SessionName(id string) string {
	if strings.ContainsRune(id, ':') {
		panic("session: colon forbidden in tmux session names (ADR-001 §4.4): " + id)
	}
	return "loom-" + id
}

// AttachCommand builds the tmux invocation that hands the terminal to
// session name on t's `-L` server. When the caller is itself already
// running inside an enclosing tmux client (`$TMUX` set), attaching directly
// would nest a second tmux client inside the current pane; instead this
// opens the session as a NEW WINDOW of that outer session — `tmux
// new-window` reads `$TMUX` to target the enclosing server/session with no
// `-L`/`-t` of its own — so "open" always surfaces as a tab, never a nested
// attach. Outside tmux (no `$TMUX`), there is no outer session to add a tab
// to, so it falls back to a direct attach.
func (t Tmux) AttachCommand(name string) *exec.Cmd {
	if os.Getenv("TMUX") != "" {
		return exec.Command(t.bin, "new-window", "-n", name, "--", t.bin, "-L", t.Server, "attach-session", "-t", name)
	}
	return exec.Command(t.bin, "-L", t.Server, "attach-session", "-t", name)
}

// SessionState reports one live session's name and whether a client is
// currently attached, the `◉` marker input for the board's status view
// (ADR-001 §4.1 step 3).
type SessionState struct {
	Name     string
	Attached bool
}

// Sessions lists every live session with its attached flag via a single
// `-F '#{session_name}\t#{session_attached}'` call. A missing server or an
// empty session list both return (nil, nil), mirroring ListSessions.
func (t Tmux) Sessions() ([]SessionState, error) {
	out, err := t.run("list-sessions", "-F", "#{session_name}\t#{session_attached}")
	if err != nil {
		if MissingServer(err) {
			return nil, nil
		}
		if te, ok := err.(*tmuxError); ok && te.code == 1 {
			return nil, nil
		}
		return nil, err
	}
	return parseSessionStates(out), nil
}

// tmuxError carries tmux's exit code and stderr so callers can classify a
// missing-server failure without string-matching a generic exec error.
type tmuxError struct {
	code   int
	stderr string
}

func (e *tmuxError) Error() string {
	return fmt.Sprintf("session: tmux exited %d: %s", e.code, strings.TrimSpace(e.stderr))
}

func MissingServer(err error) bool {
	te, ok := err.(*tmuxError)
	if !ok {
		return false
	}
	return strings.Contains(te.stderr, "no server running") ||
		strings.Contains(te.stderr, "error connecting to")
}

func (t Tmux) run(args ...string) (string, error) {
	cmd := exec.Command(t.bin, append([]string{"-L", t.Server}, args...)...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", &tmuxError{code: ee.ExitCode(), stderr: stderr.String()}
		}
		return "", fmt.Errorf("session: tmux %v: %v", args, err)
	}
	return stdout.String(), nil
}

func parseSessionNames(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func parseSessionStates(out string) []SessionState {
	var states []SessionState
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		states = append(states, SessionState{Name: fields[0], Attached: len(fields) > 1 && fields[1] == "1"})
	}
	return states
}

func tmuxGE3(v string) bool {
	start := -1
	for i := 0; i < len(v); i++ {
		if v[i] >= '0' && v[i] <= '9' {
			start = i
			break
		}
	}
	if start < 0 {
		return false
	}
	end := start
	for end < len(v) && v[end] >= '0' && v[end] <= '9' {
		end++
	}
	major, err := strconv.Atoi(v[start:end])
	if err != nil {
		return false
	}
	return major >= 3
}
