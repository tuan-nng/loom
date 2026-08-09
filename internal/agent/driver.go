package agent

import (
	"fmt"
	"slices"
	"strings"

	"loom/internal/config"
)

type LaunchMode string

const (
	LaunchModeInteractive LaunchMode = "interactive"
	LaunchModeRun         LaunchMode = "run"
)

type SessionSpec struct {
	Argv     []string
	SendKeys string
}

type Driver interface {
	Name() string
	Resolve(cfg *config.Config) (string, error)
	LaunchMode() LaunchMode
	Launch(exe string, card Card, cfg *config.Config) (SessionSpec, error)
}

var drivers = map[string]Driver{}

func Get(name string) (Driver, error) {
	d, ok := drivers[name]
	if !ok || d == nil {
		return nil, fmt.Errorf("agent: unknown driver %q", name)
	}
	return d, nil
}

func Known() []string {
	names := make([]string, 0, len(drivers))
	for name := range drivers {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func IsKnown(name string) bool {
	d, ok := drivers[name]
	return ok && d != nil
}

func Validate(cfg *config.Config) error {
	switch cfg.Agent.Opencode.Interface {
	case "mini", "full":
	default:
		return fmt.Errorf("agent: interface %q not supported (accepted: \"mini\", \"full\")", cfg.Agent.Opencode.Interface)
	}
	if !IsKnown(cfg.Agent.Default) {
		return fmt.Errorf("agent: default %q not supported (accepted: %s)", cfg.Agent.Default, acceptedNames())
	}
	return nil
}

func acceptedNames() string {
	names := Known()
	if len(names) == 0 {
		return "none"
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(quoted, ", ")
}
