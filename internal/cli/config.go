package cli

import (
	"flag"
	"fmt"

	"github.com/BurntSushi/toml"
)

// runConfig prints the effective loaded config as TOML (DESIGN-002 §11, T13):
// defaults plus file overrides with the ~-expanded database path. No
// omitempty — zero-valued fields are meaningful (empty model = agent default)
// and the output round-trips as a config the user could copy.
func runConfig(a *App, args []string) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 0, 0); err != nil {
		return err
	}
	if err := toml.NewEncoder(a.out).Encode(a.cfg); err != nil {
		return fmt.Errorf("cli: %w", err)
	}
	return nil
}
