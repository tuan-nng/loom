// Command loom is the loom CLI: a multi-agent kanban board. It routes to the
// interactive TUI eventually (T16); today bare `loom` prints help. All state
// mutations are scriptable via subcommands (ADR-001 §6).
package main

import (
	"os"

	"loom/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
