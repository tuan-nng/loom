package agent

import (
	"os/exec"

	"loom/internal/config"
)

type opencodeDriver struct{}

func init() {
	drivers["opencode"] = opencodeDriver{}
}

func (opencodeDriver) Name() string { return "opencode" }

func (opencodeDriver) LaunchMode() LaunchMode { return LaunchModeInteractive }

func (opencodeDriver) Resolve(cfg *config.Config) (string, error) {
	return exec.LookPath(cfg.Agent.Opencode.Binary)
}

func (opencodeDriver) Launch(exe string, card Card, cfg *config.Config) (SessionSpec, error) {
	o := cfg.Agent.Opencode
	argv := []string{exe}
	switch o.Interface {
	case "full":
		argv = append(argv, "--prompt", BuildPrompt(card))
	default:
		argv = append(argv, "--mini", "--prompt", BuildPrompt(card))
	}
	if m := o.Model; m != "" {
		argv = append(argv, "--model", m)
	}
	if a := o.OpencodeAgent; a != "" {
		argv = append(argv, "--agent", a)
	}
	if o.AutoApprove {
		argv = append(argv, "--auto")
	}
	return SessionSpec{Argv: argv, SendKeys: ""}, nil
}
