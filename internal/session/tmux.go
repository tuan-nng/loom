// Package session wraps tmux: one detached session per card, `loom-<id>`
// naming, attach/detach handoff (ADR-001 §4.4, DESIGN-002 §10.1). When loom
// itself runs inside an enclosing tmux client (`$TMUX` set), sessions are
// managed on that enclosing server so a card opens as a plain linked window
// (tab) rather than a nested tmux client; standalone, loom keeps its own
// dedicated `-L <server>` socket.
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
	// Prefix is the key binding the dedicated loom server uses to reach its
	// own sessions (ADR-001 §4.4). It is applied to the `-L` server so a
	// standalone attach never collides with a host's prefix; empty means the
	// C-a default. It is not applied to an enclosing server (see Enclosing).
	Prefix string
	// enclosing is the socket path of the enclosing tmux server when loom
	// runs inside a tmux client. When non-empty, sessions are created and
	// managed on that server (`-S`) so they can be opened as plain linked
	// windows — never nested clients. Empty means use the dedicated `-L
	// Server` socket.
	enclosing string
	// pane is `$TMUX_PANE`, the pane loom itself runs in. It is how the
	// enclosing session is identified: tmux resolves a command's "current
	// session" from the calling client, not from `$TMUX`, so any command
	// that must act on the user's session targets it explicitly via this
	// pane rather than relying on an implicit default.
	pane string
	bin  string
}

// EnclosingSocket reports the socket path of the enclosing tmux server when
// the loom process itself runs inside a tmux client (`$TMUX` set) — the first
// comma-separated field of `$TMUX`. It returns "" when loom runs standalone.
func EnclosingSocket() string {
	s := os.Getenv("TMUX")
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, ','); i >= 0 {
		return s[:i]
	}
	return s
}

func New(server string) (Tmux, error) {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		return Tmux{}, fmt.Errorf("session: tmux not found in PATH (install it: 'apt install tmux' or 'brew install tmux')")
	}
	t := Tmux{
		Server:    server,
		Prefix:    "C-a",
		enclosing: EnclosingSocket(),
		pane:      os.Getenv("TMUX_PANE"),
		bin:       bin,
	}
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
	// `-n` names the session's first window, so an enclosing-server link
	// surfaces as a tab named `loom-<id>`.
	_, err := t.run("new-session", "-d", "-s", name, "-n", name, "-c", cwd, command)
	if err == nil {
		t.configureServer()
		return nil
	}
	if ok, _ := t.HasSession(name); ok {
		t.configureServer()
		return nil
	}
	// The first new-session on a cold server (which, with exit-empty on, is
	// every first open after the last session ended) can transiently fail
	// with "server exited unexpectedly" — a startup race, not a real error.
	// The failed attempt leaves no session behind, so retry once; a session
	// that exists after a reported failure counts as success (the attempt
	// committed before the client errored).
	time.Sleep(100 * time.Millisecond)
	if _, err = t.run("new-session", "-d", "-s", name, "-n", name, "-c", cwd, command); err != nil {
		if ok, _ := t.HasSession(name); ok {
			t.configureServer()
			return nil
		}
	}
	return err
}

// configureServer applies the loom-owned server settings (ADR-001 §4.4) as
// globals on the dedicated `-L` server. They make a standalone attach render
// as a plain pane instead of a second tmux client: the prefix is remapped
// (Prefix, default C-a) so a nested attach never collides with a host's
// prefix, the status line is turned off, and `detach-on-destroy off` returns
// a client to its terminal when its session is destroyed. It is a no-op on
// an enclosing server: loom must never rewrite the user's own tmux globals.
// Best-effort: a server that died between session creation and here is left
// alone.
func (t Tmux) configureServer() {
	if t.enclosing != "" {
		return
	}
	prefix := t.Prefix
	if prefix == "" {
		prefix = "C-a"
	}
	for _, args := range [][]string{
		{"set-option", "-g", "prefix", prefix},
		{"set-option", "-g", "status", "off"},
		{"set-option", "-g", "detach-on-destroy", "off"},
	} {
		_, _ = t.run(args...)
	}
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

// KillSession terminates a card's session and the agent running in it. On an
// enclosing server the session's window is normally also linked into the
// user's own session (see AttachCommand), and a linked window outlives
// `kill-session` — the session goes away while the agent keeps running in an
// orphaned tab. Killing the window instead destroys the pane (and the agent)
// across every link, which empties the session and destroys it too; a
// `kill-session` fallback covers a session whose window was already gone.
func (t Tmux) KillSession(name string) error {
	if t.enclosing != "" {
		if _, err := t.run("kill-window", "-t", name); err == nil {
			if ok, _ := t.HasSession(name); !ok {
				return nil
			}
		}
	}
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
// session name. On an enclosing server (`enclosing` set, i.e. loom runs
// inside a tmux client), the session's window is linked into the enclosing
// session — `tmux link-window` targets the enclosing server/session via
// `-S`/`$TMUX` — so the card opens as a normal tab (a plain pane, no nested
// tmux client) and the handoff returns immediately. Re-opening a card whose
// window is already linked selects that tab instead of linking a second time,
// which would show the same window twice. Standalone (no enclosing server),
// it falls back to a direct attach on the dedicated `-L` server.
func (t Tmux) AttachCommand(name string) *exec.Cmd {
	if t.enclosing == "" {
		return exec.Command(t.bin, "-L", t.Server, "attach-session", "-t", name)
	}
	sess := t.enclosingSession()
	if sess == "" {
		// Pane unknown (no $TMUX_PANE): fall back to an unqualified link and
		// let tmux pick the destination it would default to.
		return exec.Command(t.bin, "-S", t.enclosing, "link-window", "-s", name)
	}
	if win := t.linkedWindow(sess, name); win != "" {
		return exec.Command(t.bin, "-S", t.enclosing, "select-window", "-t", sess+":"+win)
	}
	return exec.Command(t.bin, "-S", t.enclosing, "link-window", "-s", name, "-t", sess)
}

// enclosingSession returns the id of the session holding the pane loom runs
// in, or "" when it cannot be determined. tmux derives a command's current
// session from the calling client, so a loom process that is not itself a
// client (the common case for these bookkeeping calls) has no reliable
// implicit session; resolving it from $TMUX_PANE makes the target explicit
// and keeps loom off whichever session tmux would otherwise have guessed.
func (t Tmux) enclosingSession() string {
	if t.pane == "" {
		return ""
	}
	out, err := t.run("display-message", "-p", "-t", t.pane, "#{session_id}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// linkedWindow reports the id of session name's current window — the one
// `link-window -s name` would link — when it is already linked into session
// sess, else "". Windows are matched by id because ids are unique per server
// and immune to renaming, and the id is what the caller targets: it is stable
// across the window renumbering that closing a tab can trigger, whereas an
// index resolved here could name a different tab by the time it is used.
func (t Tmux) linkedWindow(sess, name string) string {
	out, err := t.run("display-message", "-p", "-t", name, "#{window_id}")
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(out)
	if id == "" {
		return ""
	}
	out, err = t.run("list-windows", "-t", sess, "-F", "#{window_id}")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == id {
			return id
		}
	}
	return ""
}

// AttachCommandFor builds the attach handoff for card session name, resolving
// the tmux binary and the enclosing server itself. It exists for callers that
// hold only the configured server name (the TUI) and would otherwise have to
// duplicate AttachCommand's enclosing-server logic.
func AttachCommandFor(server, name string) (*exec.Cmd, error) {
	t, err := New(server)
	if err != nil {
		return nil, err
	}
	return t.AttachCommand(name), nil
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
	cmd := exec.Command(t.bin, append(t.targetArgs(), args...)...)
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

// targetArgs selects the server every tmux subcommand runs against: the
// enclosing server (via `-S <socket>`) when loom runs inside a tmux client,
// otherwise the dedicated `-L <Server>` socket.
func (t Tmux) targetArgs() []string {
	if t.enclosing != "" {
		return []string{"-S", t.enclosing}
	}
	return []string{"-L", t.Server}
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
