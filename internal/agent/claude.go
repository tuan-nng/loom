package agent

import (
	"os/exec"

	"loom/internal/config"
)

type claudeDriver struct{}

func init() {
	drivers["claude"] = claudeDriver{}
}

func (claudeDriver) Name() string { return "claude" }

func (claudeDriver) LaunchMode() LaunchMode { return LaunchModeInteractive }

func (claudeDriver) Resolve(cfg *config.Config) (string, error) {
	return exec.LookPath(cfg.Agent.Claude.Binary)
}

func (claudeDriver) Launch(exe string, card Card, cfg *config.Config) (SessionSpec, error) {
	argv := []string{exe, BuildPrompt(card)}
	if m := cfg.Agent.Claude.Model; m != "" {
		argv = append(argv, "--model", m)
	}
	return SessionSpec{Argv: argv, SendKeys: ""}, nil
}
