package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const selfTestServer = "loomselftest"

func tmuxBin(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatalf("session: tmux not found in PATH (install it: 'apt install tmux' or 'brew install tmux')")
	}
	return bin
}

// tmuxTest builds a wrapper bound to the isolated self-test server. Tests fail
// hard when tmux is absent, mirroring the git helpers in internal/trace.
func tmuxTest(t *testing.T) Tmux {
	t.Helper()
	return Tmux{Server: selfTestServer, bin: tmuxBin(t)}
}

func uniqueName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("loomtest-%d-%d", os.Getpid(), time.Now().UnixNano())
}

// bootServer starts the isolated self-test server via a throwaway session so
// that absent-session checks below are deterministic (server exists but the
// target session does not), rather than hitting the missing-server error.
// NewSession retries the cold-server startup race itself.
func bootServer(t *testing.T, tm Tmux) string {
	t.Helper()
	boot := uniqueName(t)
	if err := tm.NewSession(boot, t.TempDir(), "cat"); err != nil {
		t.Fatalf("NewSession(boot): %v", err)
	}
	t.Cleanup(func() { tm.KillSession(boot) })
	return boot
}

func waitForPane(t *testing.T, tm Tmux, name, want string) {
	t.Helper()
	pane := tm.CapturePane(name)
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(pane, want) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for pane to contain %q; got %q", want, pane)
		}
		time.Sleep(50 * time.Millisecond)
		pane = tm.CapturePane(name)
	}
}

func waitForSession(t *testing.T, tm Tmux, name string, want bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		ok, err := tm.HasSession(name)
		if err != nil {
			t.Fatalf("HasSession(%q): %v", name, err)
		}
		if ok == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for HasSession(%q) == %v", name, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestSessionName(t *testing.T) {
	if got := SessionName("0123456789abcdef0123456789abcdef"); got != "loom-0123456789abcdef0123456789abcdef" {
		t.Fatalf("SessionName = %q, want %q", got, "loom-0123456789abcdef0123456789abcdef")
	}
	for _, id := range []string{"a:b", ":ab", "ab:", "a:b:c"} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("SessionName(%q) did not panic", id)
				}
			}()
			SessionName(id)
		}()
	}
}

func TestParseSessionNames(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"one", "loom-123\n", []string{"loom-123"}},
		{"many with blanks", "a\nb\n\nc\n", []string{"a", "b", "c"}},
		{"crlf", "a\r\nb\r\n", []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseSessionNames(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseSessionNames(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestTmuxGE3(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"3.6", "tmux 3.6", true},
		{"3.0", "tmux 3.0", true},
		{"3.5a", "tmux 3.5a", true},
		{"next-3.5", "tmux next-3.5", true},
		{"2.9", "tmux 2.9", false},
		{"2.9a", "tmux 2.9a", false},
		{"1.8", "tmux 1.8", false},
		{"empty", "", false},
		{"no digits", "tmux unknown", false},
		{"overflow", "tmux 99999999999999999999", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tmuxGE3(tt.in); got != tt.want {
				t.Fatalf("tmuxGE3(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestTmuxError(t *testing.T) {
	e := &tmuxError{code: 1, stderr: "can't find session: x"}
	if got, want := e.Error(), "session: tmux exited 1: can't find session: x"; got != want {
		t.Fatalf("tmuxError.Error() = %q, want %q", got, want)
	}
}

func TestNewNotInPath(t *testing.T) {
	t.Setenv("PATH", "")
	if _, err := New(selfTestServer); err == nil {
		t.Fatal("New with empty PATH: no error")
	} else if !strings.Contains(err.Error(), "not found in PATH") ||
		!strings.Contains(err.Error(), "apt install tmux") {
		t.Fatalf("New with empty PATH = %v, want install hint", err)
	}
}

func TestMissingServerPlainError(t *testing.T) {
	if MissingServer(fmt.Errorf("some plain error")) {
		t.Fatal("MissingServer(plain error) = true, want false")
	}
}

func TestCapturePaneAbsent(t *testing.T) {
	tm := tmuxTest(t)
	bootServer(t, tm)
	if got := tm.CapturePane(uniqueName(t)); got != "" {
		t.Fatalf("CapturePane(missing) = %q, want empty sentinel", got)
	}
}

func TestNew(t *testing.T) {
	tm, err := New(selfTestServer)
	if err != nil {
		t.Fatalf("New(%q): %v", selfTestServer, err)
	}
	if tm.Server != selfTestServer {
		t.Fatalf("Server = %q, want %q", tm.Server, selfTestServer)
	}
}

func TestTmuxRoundTrip(t *testing.T) {
	tm := tmuxTest(t)
	boot := bootServer(t, tm)
	name := uniqueName(t)
	t.Cleanup(func() { tm.KillSession(name) })
	_ = boot

	if ok, err := tm.HasSession(name); err != nil || ok {
		t.Fatalf("HasSession before create: ok=%v err=%v, want (false, nil)", ok, err)
	}
	if err := tm.NewSession(name, t.TempDir(), "cat"); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if ok, err := tm.HasSession(name); err != nil || !ok {
		t.Fatalf("HasSession after create: ok=%v err=%v, want (true, nil)", ok, err)
	}
	if err := tm.SendKeys(name, "LOOMKEYS"); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	waitForPane(t, tm, name, "LOOMKEYS")
	names, err := tm.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	found := false
	for _, n := range names {
		if n == name {
			found = true
		}
	}
	if !found {
		t.Fatalf("ListSessions = %v, want it to include %q", names, name)
	}
	if err := tm.KillSession(name); err != nil {
		t.Fatalf("KillSession: %v", err)
	}
	waitForSession(t, tm, name, false)
}

// TestServerConfigured verifies NewSession registers the loom-owned server
// settings on the isolated server (ADR-001 §4.4): prefix remapped to the
// default C-a, status line off, and detach-on-destroy off, so an attach
// nested inside a host tmux renders as a plain pane and its prefix keys
// never collide with the host tmux's.
func TestServerConfigured(t *testing.T) {
	tm := tmuxTest(t)
	bootServer(t, tm)
	opt := func(name string) string {
		out, err := tm.run("show-options", "-g", name)
		if err != nil {
			t.Fatalf("show-options -g %s: %v", name, err)
		}
		if i := strings.Index(out, name); i >= 0 {
			return strings.TrimSpace(out[i+len(name):])
		}
		return strings.TrimSpace(out)
	}
	if got := opt("prefix"); got != "C-a" {
		t.Errorf("server prefix = %q, want C-a", got)
	}
	if got := opt("status"); got != "off" {
		t.Errorf("server status = %q, want off", got)
	}
	if got := opt("detach-on-destroy"); got != "off" {
		t.Errorf("server detach-on-destroy = %q, want off", got)
	}
}

func TestHasSessionAbsent(t *testing.T) {
	tm := tmuxTest(t)
	bootServer(t, tm)
	ok, err := tm.HasSession(uniqueName(t))
	if err != nil {
		t.Fatalf("HasSession(absent): %v", err)
	}
	if ok {
		t.Fatal("HasSession(absent) = true, want false")
	}
}

func TestMissingServer(t *testing.T) {
	server := fmt.Sprintf("loomabsent-%d", os.Getpid())
	tm := Tmux{Server: server, bin: tmuxBin(t)}
	ok, err := tm.HasSession("nope")
	if ok {
		t.Fatal("HasSession on absent server = true")
	}
	if err == nil {
		t.Fatal("HasSession on absent server: no error")
	}
	if !MissingServer(err) {
		t.Fatalf("MissingServer(%v) = false, want true", err)
	}
	if names, err := tm.ListSessions(); err != nil || names != nil {
		t.Fatalf("ListSessions on absent server = %v, %v; want (nil, nil)", names, err)
	}
}

func TestEnclosingSocket(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want string
	}{
		{"unset", "", ""},
		{"full", "/tmp/tmux-1000/default,7931,0", "/tmp/tmux-1000/default"},
		{"socket only", "/tmp/tmux-1000/default", "/tmp/tmux-1000/default"},
		{"path with dashes", "/private/tmp/tmux-501/foo-bar,12,3", "/private/tmp/tmux-501/foo-bar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TMUX", tt.env)
			if got := EnclosingSocket(); got != tt.want {
				t.Fatalf("EnclosingSocket() with TMUX=%q = %q, want %q", tt.env, got, tt.want)
			}
		})
	}
}

// enclosingFixture boots an isolated server on its own socket standing in for
// the user's tmux, with a `host` session standing in for the session loom was
// launched from, and returns a Tmux bound to it as if loom were running in
// host's pane. A decoy session created first ensures tests fail if the code
// falls back to tmux's implicit "current session" instead of targeting host.
func enclosingFixture(t *testing.T) (Tmux, string) {
	t.Helper()
	bin := tmuxBin(t)
	sock := filepath.Join(t.TempDir(), "sock")
	kill := func() {
		_ = exec.Command(bin, "-S", sock, "kill-server").Run()
	}
	kill()
	for _, s := range []string{"decoy", "host"} {
		if out, err := exec.Command(bin, "-S", sock, "new-session", "-d", "-s", s, "cat").CombinedOutput(); err != nil {
			t.Fatalf("new-session %s: %v: %s", s, err, out)
		}
	}
	t.Cleanup(kill)
	out, err := exec.Command(bin, "-S", sock, "list-panes", "-t", "host", "-F", "#{pane_id}").Output()
	if err != nil {
		t.Fatalf("list-panes: %v", err)
	}
	pane := strings.TrimSpace(string(out))
	if pane == "" {
		t.Fatal("list-panes returned no pane for host")
	}
	return Tmux{Server: selfTestServer, enclosing: sock, pane: pane, bin: bin}, sock
}

// hostWindows lists the host session's windows as "id name" lines.
func hostWindows(t *testing.T, tm Tmux) []string {
	t.Helper()
	out, err := tm.run("list-windows", "-t", "host", "-F", "#{window_id} #{window_name}")
	if err != nil {
		t.Fatalf("list-windows -t host: %v", err)
	}
	return parseSessionNames(out)
}

// TestAttachCommandEnclosingLinksAsTab covers the enclosing-server handoff:
// opening a card links its window into the session loom runs in, so it
// surfaces as a plain tab rather than a nested tmux client, and re-opening
// selects that same tab instead of linking a duplicate.
func TestAttachCommandEnclosingLinksAsTab(t *testing.T) {
	tm, sock := enclosingFixture(t)
	name := uniqueName(t)
	if err := tm.NewSession(name, t.TempDir(), "cat"); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() { tm.KillSession(name) })

	before := hostWindows(t, tm)
	cmd := tm.AttachCommand(name)
	if want := []string{tm.bin, "-S", sock, "link-window", "-s", name, "-t"}; !reflect.DeepEqual(cmd.Args[:len(want)], want) {
		t.Fatalf("AttachCommand args = %v, want prefix %v", cmd.Args, want)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("link handoff: %v: %s", err, out)
	}
	after := hostWindows(t, tm)
	if len(after) != len(before)+1 {
		t.Fatalf("host windows = %v, want one more than %v", after, before)
	}
	if !slicesContainSuffix(after, name) {
		t.Fatalf("host windows = %v, want one named %q", after, name)
	}

	// Re-open: the window is already linked, so the handoff must select the
	// existing tab, leaving the window count unchanged.
	reopen := tm.AttachCommand(name)
	if got := reopen.Args[3]; got != "select-window" {
		t.Fatalf("re-open command = %v, want select-window", reopen.Args)
	}
	if out, err := reopen.CombinedOutput(); err != nil {
		t.Fatalf("select handoff: %v: %s", err, out)
	}
	if got := hostWindows(t, tm); len(got) != len(after) {
		t.Fatalf("host windows after re-open = %v, want unchanged %v", got, after)
	}
}

// TestKillSessionEnclosingKillsLinkedWindow guards the hazard that a linked
// window outlives `kill-session`: killing a card must remove the tab from the
// user's session and take the agent process with it, not orphan it.
func TestKillSessionEnclosingKillsLinkedWindow(t *testing.T) {
	tm, _ := enclosingFixture(t)
	name := uniqueName(t)
	if err := tm.NewSession(name, t.TempDir(), "cat"); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if out, err := tm.AttachCommand(name).CombinedOutput(); err != nil {
		t.Fatalf("link handoff: %v: %s", err, out)
	}
	linked := hostWindows(t, tm)
	if !slicesContainSuffix(linked, name) {
		t.Fatalf("host windows = %v, want one named %q before kill", linked, name)
	}
	if err := tm.KillSession(name); err != nil {
		t.Fatalf("KillSession: %v", err)
	}
	waitForSession(t, tm, name, false)
	if got := hostWindows(t, tm); slicesContainSuffix(got, name) {
		t.Fatalf("host windows = %v, want %q gone after KillSession", got, name)
	}
}

// TestConfigureServerSkippedWhenEnclosing pins that loom never rewrites the
// user's own tmux globals: status/prefix/detach-on-destroy belong to the
// dedicated `-L` server only.
func TestConfigureServerSkippedWhenEnclosing(t *testing.T) {
	tm, _ := enclosingFixture(t)
	before, err := tm.run("show-options", "-g", "status")
	if err != nil {
		t.Fatalf("show-options -g status: %v", err)
	}
	name := uniqueName(t)
	if err := tm.NewSession(name, t.TempDir(), "cat"); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() { tm.KillSession(name) })
	after, err := tm.run("show-options", "-g", "status")
	if err != nil {
		t.Fatalf("show-options -g status: %v", err)
	}
	if before != after {
		t.Fatalf("enclosing server status changed from %q to %q; loom must not touch the user's globals", before, after)
	}
}

func slicesContainSuffix(lines []string, suffix string) bool {
	for _, l := range lines {
		if strings.HasSuffix(l, " "+suffix) {
			return true
		}
	}
	return false
}
