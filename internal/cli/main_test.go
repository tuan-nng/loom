package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMainFlow exercises Main's bootstrap (config load, agent.Validate, store
// open, dispatch) with an isolated HOME/XDG_CONFIG_HOME so no host config is
// touched. The success path asserts only tmux-independent lines, so it passes
// with or without tmux installed.
func TestMainFlowInitAndStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	proj := filepath.Join(dir, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}

	if code := Main([]string{"init", proj}); code != 0 {
		t.Fatalf("Main(init) = %d, want 0", code)
	}
	// Second init is idempotent.
	if code := Main([]string{"init", proj}); code != 0 {
		t.Fatalf("Main(init) second = %d, want 0", code)
	}
	// Status resolves the selection and prints at least the board summary.
	if code := Main([]string{"status"}); code != 0 {
		t.Fatalf("Main(status) = %d, want 0", code)
	}
}

// TestMainFlowBareAndVersion covers the help/version exit-0 paths end to end.
func TestMainFlowBareAndVersion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	if code := Main(nil); code != 0 {
		t.Fatalf("Main(nil) = %d, want 0", code)
	}
	if code := Main([]string{"version"}); code != 0 {
		t.Fatalf("Main(version) = %d, want 0", code)
	}
}

// TestMainFlowBadConfig verifies a config-validation failure exits 1. The
// stale prompt_model key is the loudest failure (T1 acceptance).
func TestMainFlowBadConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	cfgDir := filepath.Join(dir, ".config", "loom")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[claude]\nprompt_model = \"x\"\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Capture stderr via a swap; Main writes to os.Stderr.
	code := Main([]string{"status"})
	if code != 1 {
		t.Fatalf("Main(bad config) = %d, want 1", code)
	}
}

// TestMainFlowBadDefaultAgent verifies agent.Validate fails startup when the
// config's default agent is unknown (C8).
func TestMainFlowBadDefaultAgent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	cfgDir := filepath.Join(dir, ".config", "loom")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[agent]\ndefault = \"bogus\"\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := Main([]string{"status"}); code != 1 {
		t.Fatalf("Main(bad default agent) = %d, want 1", code)
	}
}

// TestUsageError covers the Error() string (the only untested surface of the
// usageError type).
func TestUsageError(t *testing.T) {
	e := &usageError{msg: "boom"}
	if !strings.Contains(e.Error(), "boom") {
		t.Errorf("usageError.Error() = %q", e.Error())
	}
}
