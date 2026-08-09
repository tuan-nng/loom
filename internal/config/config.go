package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Agent    AgentConfig    `toml:"agent"`
	Session  SessionConfig  `toml:"session"`
	Database DatabaseConfig `toml:"database"`
}

type AgentConfig struct {
	Default  string         `toml:"default"`
	Claude   ClaudeConfig   `toml:"claude"`
	Opencode OpencodeConfig `toml:"opencode"`
}

type ClaudeConfig struct {
	Binary string `toml:"binary"`
	Model  string `toml:"model"`
}

type OpencodeConfig struct {
	Binary        string `toml:"binary"`
	Model         string `toml:"model"`
	OpencodeAgent string `toml:"opencode_agent"`
	Interface     string `toml:"interface"`
	AutoApprove   bool   `toml:"auto_approve"`
}

type SessionConfig struct {
	TmuxServer string `toml:"tmux_server"`
	Prefix     string `toml:"prefix"`
}

type DatabaseConfig struct {
	Path string `toml:"path"`
}

func Default() *Config {
	return &Config{
		Agent: AgentConfig{
			Default: "claude",
			Claude:  ClaudeConfig{Binary: "claude"},
			Opencode: OpencodeConfig{
				Binary:    "opencode",
				Interface: "mini",
			},
		},
		Session: SessionConfig{
			TmuxServer: "loom",
			Prefix:     "C-a",
		},
		Database: DatabaseConfig{Path: "~/.config/loom/loom.db"},
	}
}

func Load() (*Config, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	cfg := Default()
	path := filepath.Join(dir, "loom", "config.toml")
	md, err := toml.DecodeFile(path, cfg)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if err := cfg.expandPath(); err != nil {
				return nil, err
			}
			return cfg, nil
		}
		return nil, err
	}
	if k := stalePromptModel(md); len(k) > 0 {
		return nil, fmt.Errorf("config: %s is obsolete; rename prompt_model to model", k)
	}
	if err := cfg.expandPath(); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	switch c.Agent.Opencode.Interface {
	case "mini", "full":
	default:
		return fmt.Errorf("config: interface %q not supported (accepted: \"mini\", \"full\")", c.Agent.Opencode.Interface)
	}
	return nil
}

func (c *Config) expandPath() error {
	p := c.Database.Path
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if p == "~" {
		c.Database.Path = home
		return nil
	}
	c.Database.Path = filepath.Join(home, strings.TrimPrefix(p, "~/"))
	return nil
}

func stalePromptModel(md toml.MetaData) toml.Key {
	for _, k := range md.Undecoded() {
		switch {
		case len(k) == 2 && k[0] == "claude" && k[1] == "prompt_model":
			return k
		case len(k) == 3 && k[0] == "agent" && k[1] == "claude" && k[2] == "prompt_model":
			return k
		}
	}
	return nil
}
