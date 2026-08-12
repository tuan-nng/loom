// Command loom is the loom CLI: a multi-agent kanban board. Bare `loom`
// launches the interactive TUI; every state mutation is also scriptable via
// subcommands (ADR-001 §6).
package main

import (
	"os"

	"loom/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
