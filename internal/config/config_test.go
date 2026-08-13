package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func setConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

func writeConfig(t *testing.T, body string) {
	t.Helper()
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "loom", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func expandedDefault(t *testing.T) *Config {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.Database.Path = filepath.Join(home, ".config", "loom", "loom.db")
	return cfg
}

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Agent.Default != "claude" {
		t.Errorf("Default: Agent.Default = %q, want %q", cfg.Agent.Default, "claude")
	}
	if cfg.Agent.Claude.Binary != "claude" {
		t.Errorf("Default: Claude.Binary = %q, want %q", cfg.Agent.Claude.Binary, "claude")
	}
	if cfg.Agent.Opencode.Binary != "opencode" {
		t.Errorf("Default: Opencode.Binary = %q, want %q", cfg.Agent.Opencode.Binary, "opencode")
	}
	if cfg.Agent.Opencode.Interface != "full" {
		t.Errorf("Default: Opencode.Interface = %q, want %q", cfg.Agent.Opencode.Interface, "full")
	}
	if cfg.Session.TmuxServer != "loom" {
		t.Errorf("Default: TmuxServer = %q, want %q", cfg.Session.TmuxServer, "loom")
	}
	if cfg.Session.Prefix != "C-a" {
		t.Errorf("Default: Prefix = %q, want %q", cfg.Session.Prefix, "C-a")
	}
	if cfg.Database.Path != "~/.config/loom/loom.db" {
		t.Errorf("Default: Database.Path = %q, want %q", cfg.Database.Path, "~/.config/loom/loom.db")
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Default().Validate() = %v, want nil", err)
	}
}

func TestExpandTilde(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", home)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"leading tilde slash", "~/x", filepath.Join(home, "x")},
		{"bare tilde", "~", home},
		{"tilde user left alone", "~user/x", "~user/x"},
		{"absolute left alone", "/var/tmp/loom.db", "/var/tmp/loom.db"},
		{"empty left alone", "", ""},
		{"mid-string tilde left alone", "a~/b", "a~/b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Database: DatabaseConfig{Path: tt.in}}
			if err := cfg.expandPath(); err != nil {
				t.Fatalf("expandPath() = %v, want nil", err)
			}
			if cfg.Database.Path != tt.want {
				t.Errorf("expandPath() path = %q, want %q", cfg.Database.Path, tt.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"mini accepted", Config{Agent: AgentConfig{Opencode: OpencodeConfig{Interface: "mini"}}}, false},
		{"full accepted", Config{Agent: AgentConfig{Opencode: OpencodeConfig{Interface: "full"}}}, false},
		{"empty interface rejected", Config{Agent: AgentConfig{Opencode: OpencodeConfig{Interface: ""}}}, true},
		{"unknown interface rejected", Config{Agent: AgentConfig{Opencode: OpencodeConfig{Interface: "tui"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("Validate() = nil, want error")
				}
				for _, sub := range []string{"mini", "full"} {
					if !strings.Contains(err.Error(), sub) {
						t.Errorf("Validate() error %q missing accepted value %q", err, sub)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	fullBody := `[agent]
default = "opencode"

[agent.claude]
binary = "claude-path"
model = "claude-3-5-sonnet"

[agent.opencode]
binary = "opencode-path"
model = "anthropic/claude-4"
opencode_agent = "build"
interface = "full"
auto_approve = true

[session]
tmux_server = "loom-test"
prefix = "C-b"

[database]
path = "~/db.sqlite"
`
	tests := []struct {
		name    string
		setup   func(t *testing.T)
		want    func(t *testing.T) *Config
		wantErr []string
	}{
		{
			name: "missing file -> defaults",
			setup: func(t *testing.T) {
				setConfigDir(t)
			},
			want: func(t *testing.T) *Config {
				return expandedDefault(t)
			},
		},
		{
			name: "present file parsed",
			setup: func(t *testing.T) {
				setConfigDir(t)
				writeConfig(t, fullBody)
			},
			want: func(t *testing.T) *Config {
				home, err := os.UserHomeDir()
				if err != nil {
					t.Fatal(err)
				}
				return &Config{
					Agent: AgentConfig{
						Default: "opencode",
						Claude:  ClaudeConfig{Binary: "claude-path", Model: "claude-3-5-sonnet"},
						Opencode: OpencodeConfig{
							Binary:        "opencode-path",
							Model:         "anthropic/claude-4",
							OpencodeAgent: "build",
							Interface:     "full",
							AutoApprove:   true,
						},
					},
					Session: SessionConfig{
						TmuxServer: "loom-test",
						Prefix:     "C-b",
					},
					Database: DatabaseConfig{Path: filepath.Join(home, "db.sqlite")},
				}
			},
		},
		{
			name: "superseded top-level claude prompt_model",
			setup: func(t *testing.T) {
				setConfigDir(t)
				writeConfig(t, "[claude]\nprompt_model = \"claude-3-5\"\n")
			},
			wantErr: []string{"model"},
		},
		{
			name: "stale agent.claude prompt_model",
			setup: func(t *testing.T) {
				setConfigDir(t)
				writeConfig(t, "[agent.claude]\nprompt_model = \"x\"\n")
			},
			wantErr: []string{"model"},
		},
		{
			name: "both stale keys",
			setup: func(t *testing.T) {
				setConfigDir(t)
				writeConfig(t, "[claude]\nprompt_model = \"a\"\n\n[agent.claude]\nprompt_model = \"b\"\n")
			},
			wantErr: []string{"model"},
		},
		{
			name: "unknown non-prompt_model key tolerated",
			setup: func(t *testing.T) {
				setConfigDir(t)
				writeConfig(t, "[bogus]\nx = 1\n")
			},
			want: func(t *testing.T) *Config {
				return expandedDefault(t)
			},
		},
		{
			name: "interface outside set",
			setup: func(t *testing.T) {
				setConfigDir(t)
				writeConfig(t, "[agent.opencode]\ninterface = \"tui\"\n")
			},
			wantErr: []string{"mini", "full"},
		},
		{
			name: "absolute db path unchanged",
			setup: func(t *testing.T) {
				setConfigDir(t)
				writeConfig(t, "[database]\npath = \"/var/tmp/loom.db\"\n")
			},
			want: func(t *testing.T) *Config {
				cfg := Default()
				cfg.Database.Path = "/var/tmp/loom.db"
				return cfg
			},
		},
		{
			name: "xdg unset falls back to home .config",
			setup: func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("XDG_CONFIG_HOME", "")
				path := filepath.Join(home, ".config", "loom", "config.toml")
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("[session]\nprefix = \"C-x\"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: func(t *testing.T) *Config {
				cfg := expandedDefault(t)
				cfg.Session.Prefix = "C-x"
				return cfg
			},
		},
		{
			name: "invalid toml",
			setup: func(t *testing.T) {
				setConfigDir(t)
				writeConfig(t, "[agent")
			},
			wantErr: []string{"toml"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			got, err := Load()
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("Load() = nil error, want error containing %v", tt.wantErr)
				}
				for _, sub := range tt.wantErr {
					if !strings.Contains(err.Error(), sub) {
						t.Errorf("Load() error %q missing %q", err, sub)
					}
				}
				return
			}
		if err != nil {
			t.Fatalf("Load() = %v, want nil error", err)
		}
		want := tt.want(t)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Load() mismatch:\n got: %+v\nwant: %+v", got, want)
		}
		})
	}
}
