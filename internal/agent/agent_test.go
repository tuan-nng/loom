package agent

import (
	"reflect"
	"strings"
	"testing"

	"loom/internal/config"
)

type stubDriver struct {
	name string
	mode LaunchMode
}

func (d stubDriver) Name() string { return d.name }

func (d stubDriver) Resolve(*config.Config) (string, error) {
	return "/usr/bin/" + d.name, nil
}

func (d stubDriver) LaunchMode() LaunchMode { return d.mode }

func (d stubDriver) Launch(exe string, card Card, cfg *config.Config) (SessionSpec, error) {
	return SessionSpec{Argv: []string{exe}, SendKeys: "Enter"}, nil
}

func seedDrivers(t *testing.T, d map[string]Driver) {
	t.Helper()
	orig := drivers
	drivers = d
	t.Cleanup(func() { drivers = orig })
}

func TestBuildPrompt(t *testing.T) {
	tests := []struct {
		name string
		card Card
		want string
	}{
		{"title only", Card{Title: "Fix bug"}, "Fix bug"},
		{
			"title and description",
			Card{Title: "Fix bug", Description: "steps"},
			"Fix bug\n\n## Description\nsteps",
		},
		{
			"all sections",
			Card{Title: "T", Description: "D", Objective: "O", AcceptanceCriteria: "A"},
			"T\n\n## Description\nD\n\n## Objective\nO\n\n## Acceptance Criteria\nA",
		},
		{"empty sections omitted", Card{Title: "T"}, "T"},
		{
			"whitespace-only sections omitted",
			Card{Title: "T", Description: "  \n\t ", Objective: "\n", AcceptanceCriteria: "   "},
			"T",
		},
		{
			"interior content verbatim",
			Card{Title: "T", Description: "a\nb  c"},
			"T\n\n## Description\na\nb  c",
		},
		{
			"title untrimmed",
			Card{Title: "  T  ", Description: "D"},
			"  T  \n\n## Description\nD",
		},
		{
			"empty title still written",
			Card{Description: "D"},
			"\n\n## Description\nD",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildPrompt(tt.card); got != tt.want {
				t.Errorf("BuildPrompt() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPosixEscape(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "plain", "'plain'"},
		{"single quote", "it's", "'it'\\''s'"},
		{"newline preserved", "a\nb", "'a\nb'"},
		{"empty", "", "''"},
		{"quote and newline", "a'b\nc", "'a'\\''b\nc'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PosixEscape(tt.in); got != tt.want {
				t.Errorf("PosixEscape(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCommandLine(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{"nil", nil, ""},
		{"empty", []string{}, ""},
		{"every element quoted", []string{"a b", "c'd"}, "'a b' 'c'\\''d'"},
		{"empty element", []string{"", "x"}, "'' 'x'"},
		{"prompt is one quoted element", []string{"-p", "long prompt with 'quote'"}, "'-p' 'long prompt with '\\''quote'\\'''"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CommandLine(tt.argv); got != tt.want {
				t.Errorf("CommandLine(%q) = %q, want %q", tt.argv, got, tt.want)
			}
		})
	}
}

func TestAgentOrDefault(t *testing.T) {
	tests := []struct {
		name string
		card Card
		def  string
		want string
	}{
		{"NULL -> default", Card{Agent: ""}, "claude", "claude"},
		{"explicit -> card value", Card{Agent: "opencode"}, "claude", "opencode"},
		{"empty default", Card{Agent: ""}, "", ""},
		{"whitespace is explicit", Card{Agent: " "}, "claude", " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.card.AgentOrDefault(tt.def); got != tt.want {
				t.Errorf("AgentOrDefault() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	t.Run("interface accepted", func(t *testing.T) {
		seedDrivers(t, map[string]Driver{
			"stub-a": stubDriver{name: "stub-a"},
			"stub-b": stubDriver{name: "stub-b"},
		})
		for _, iface := range []string{"mini", "full"} {
			cfg := &config.Config{Agent: config.AgentConfig{Default: "stub-a", Opencode: config.OpencodeConfig{Interface: iface}}}
			if err := Validate(cfg); err != nil {
				t.Errorf("Validate() interface %q = %v, want nil", iface, err)
			}
		}
	})

	t.Run("interface rejected with accepted values", func(t *testing.T) {
		for _, iface := range []string{"", "tui"} {
			cfg := &config.Config{Agent: config.AgentConfig{Default: "stub-a", Opencode: config.OpencodeConfig{Interface: iface}}}
			err := Validate(cfg)
			if err == nil {
				t.Fatalf("Validate() interface %q = nil, want error", iface)
			}
			for _, sub := range []string{"mini", "full"} {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("Validate() error %q missing accepted value %q", err, sub)
				}
			}
		}
	})

	t.Run("default accepted", func(t *testing.T) {
		seedDrivers(t, map[string]Driver{
			"stub-a": stubDriver{name: "stub-a"},
			"stub-b": stubDriver{name: "stub-b"},
		})
		for _, def := range []string{"stub-a", "stub-b"} {
			cfg := &config.Config{Agent: config.AgentConfig{Default: def, Opencode: config.OpencodeConfig{Interface: "mini"}}}
			if err := Validate(cfg); err != nil {
				t.Errorf("Validate() default %q = %v, want nil", def, err)
			}
		}
	})

	t.Run("default rejected with accepted values", func(t *testing.T) {
		seedDrivers(t, map[string]Driver{
			"stub-a": stubDriver{name: "stub-a"},
			"stub-b": stubDriver{name: "stub-b"},
		})
		cfg := &config.Config{Agent: config.AgentConfig{Default: "claude", Opencode: config.OpencodeConfig{Interface: "mini"}}}
		err := Validate(cfg)
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		for _, sub := range []string{"accepted", "stub-a", "stub-b"} {
			if !strings.Contains(err.Error(), sub) {
				t.Errorf("Validate() error %q missing %q", err, sub)
			}
		}
	})

	t.Run("empty registry renders none", func(t *testing.T) {
		seedDrivers(t, map[string]Driver{})
		cfg := &config.Config{Agent: config.AgentConfig{Default: "claude", Opencode: config.OpencodeConfig{Interface: "mini"}}}
		err := Validate(cfg)
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		if !strings.Contains(err.Error(), "none") {
			t.Errorf("Validate() error %q missing %q", err, "none")
		}
	})
}

func TestRegistry(t *testing.T) {
	seedDrivers(t, map[string]Driver{
		"zebra": stubDriver{name: "zebra"},
		"alpha": stubDriver{name: "alpha"},
	})

	want := []string{"alpha", "zebra"}
	if got := Known(); !reflect.DeepEqual(got, want) {
		t.Errorf("Known() = %v, want %v", got, want)
	}

	if !IsKnown("alpha") {
		t.Error("IsKnown(\"alpha\") = false, want true")
	}
	if IsKnown("claude") {
		t.Error("IsKnown(\"claude\") = true for unseeded name, want false")
	}

	d, err := Get("zebra")
	if err != nil {
		t.Fatalf("Get(\"zebra\") = error %v, want nil", err)
	}
	if d.Name() != "zebra" {
		t.Errorf("Get(\"zebra\").Name() = %q, want %q", d.Name(), "zebra")
	}

	_, err = Get("claude")
	if err == nil {
		t.Fatal("Get(\"claude\") = nil error, want error")
	}
	for _, sub := range []string{"unknown driver", "claude"} {
		if !strings.Contains(err.Error(), sub) {
			t.Errorf("Get() error %q missing %q", err, sub)
		}
	}
}

func TestRegistryNilValue(t *testing.T) {
	seedDrivers(t, map[string]Driver{"broken": nil})
	if IsKnown("broken") {
		t.Error("IsKnown(\"broken\") = true for nil driver, want false")
	}
	if _, err := Get("broken"); err == nil {
		t.Error("Get(\"broken\") = nil error, want error")
	}
}
