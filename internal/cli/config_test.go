package cli

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"loom/internal/config"
)

func TestRunConfigPrintsEffectiveTOML(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	if err := a.run([]string{"config"}); err != nil {
		t.Fatalf("config: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"[agent]",
		"default = \"claude\"",
		"[agent.claude]",
		"binary = \"claude\"",
		"[agent.opencode]",
		"interface = \"mini\"",
		"[session]",
		"tmux_server = \"loom\"",
		"[database]",
		"path = \"" + a.cfg.Database.Path + "\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config output missing %q:\n%s", want, got)
		}
	}
}

func TestRunConfigRoundTrips(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	if err := a.run([]string{"config"}); err != nil {
		t.Fatalf("config: %v", err)
	}
	var decoded config.Config
	if _, err := toml.Decode(out.String(), &decoded); err != nil {
		t.Fatalf("decoding config output: %v", err)
	}
	if decoded.Agent.Default != a.cfg.Agent.Default ||
		decoded.Session.TmuxServer != a.cfg.Session.TmuxServer ||
		decoded.Database.Path != a.cfg.Database.Path {
		t.Errorf("round-trip mismatch: decoded %+v != %+v", decoded, *a.cfg)
	}
}

func TestRunConfigReflectsOverrides(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.Default = "opencode"
	cfg.Agent.Opencode.Interface = "full"
	db := openCLIDB(t)
	a := newApp(cfg, db, &stubSess{}, &strings.Builder{}, &strings.Builder{})
	out := &strings.Builder{}
	a.out = out
	if err := a.run([]string{"config"}); err != nil {
		t.Fatalf("config: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "default = \"opencode\"") {
		t.Errorf("config output missing override default:\n%s", got)
	}
	if !strings.Contains(got, "interface = \"full\"") {
		t.Errorf("config output missing interface override:\n%s", got)
	}
}
