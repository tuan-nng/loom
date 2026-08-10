package cli

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"loom/internal/agent"
	"loom/internal/store"
)

// validPriorities is the closed priority set enforced by the cards.priority
// CHECK (ADR-001 §3.3). The CLI validates up front for a friendly message;
// the schema remains the enforcement point.
var validPriorities = map[string]bool{"low": true, "medium": true, "high": true}

// runCardAdd creates a card in the resolved (or --board/--column-scoped)
// column. --priority left empty picks up the store's "medium" default
// (createCard). A non-empty --agent is validated against agent.Known() up
// front (DESIGN-002 §13, C8); empty leaves the card's agent NULL (follow the
// config default at launch, resolved by Card.AgentOrDefault).
func runCardAdd(a *App, args []string) error {
	fs := flag.NewFlagSet("card add", flag.ContinueOnError)
	description := fs.String("description", "", "card description")
	objective := fs.String("objective", "", "card objective")
	acceptanceCriteria := fs.String("acceptance-criteria", "", "card acceptance criteria")
	priority := fs.String("priority", "", "card priority (low|medium|high)")
	labels := fs.String("labels", "", "comma-separated labels")
	codebasePath := fs.String("codebase", "", "codebase path")
	boardName := fs.String("board", "", "board name")
	columnName := fs.String("column", "", "column name")
	agentName := fs.String("agent", "", "agent (claude|opencode); empty follows the config default")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 1, 1); err != nil {
		return err
	}
	if *priority != "" && !validPriorities[*priority] {
		return fmt.Errorf("invalid priority %q (accepted: low, medium, high)", *priority)
	}
	if *agentName != "" && !agent.IsKnown(*agentName) {
		return fmt.Errorf("invalid agent %q (accepted: %s)", *agentName, acceptedAgents())
	}

	ws, b, err := a.boardOf(fs, *boardName)
	if err != nil {
		return err
	}
	col, err := a.columnOf(b.ID, *columnName)
	if err != nil {
		return err
	}

	in := store.CardInput{ColumnID: col.ID, Title: fs.Args()[0], Priority: *priority}
	if *description != "" {
		in.Description = description
	}
	if *objective != "" {
		in.Objective = objective
	}
	if *acceptanceCriteria != "" {
		in.AcceptanceCriteria = acceptanceCriteria
	}
	if *labels != "" {
		in.Labels = labels
	}
	if *agentName != "" {
		in.Agent = agentName
	}
	if *codebasePath != "" {
		cb, err := a.findCodebase(ws.ID, *codebasePath)
		if err != nil {
			return err
		}
		in.CodebaseID = &cb.ID
	}

	c, err := a.svc.CreateCard(in)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "created card %s (%s)\n", c.Title, c.ID)
	return nil
}

// runCardUpdate applies only the flags the user passed: stdlib flag cannot
// tell "absent" from "present/empty" on its own, so fs.Visit tracks presence
// and only visited fields populate the CardUpdate (nil = untouched). For a
// nullable field (description/objective/acceptance-criteria/labels/codebase/
// agent), an explicitly empty value clears it to NULL — how `--agent=` resets
// a card to the config default (DESIGN-002 §13). column_id is intentionally
// not settable here: store.CardUpdate excludes it because MoveCard is the
// only writer that keeps board_id/workspace_id in sync — use `card move`.
func runCardUpdate(a *App, args []string) error {
	fs := flag.NewFlagSet("card update", flag.ContinueOnError)
	title := fs.String("title", "", "card title")
	description := fs.String("description", "", "card description")
	objective := fs.String("objective", "", "card objective")
	acceptanceCriteria := fs.String("acceptance-criteria", "", "card acceptance criteria")
	priority := fs.String("priority", "", "card priority (low|medium|high)")
	labels := fs.String("labels", "", "comma-separated labels")
	codebasePath := fs.String("codebase", "", "codebase path; empty clears it")
	agentName := fs.String("agent", "", "agent (claude|opencode); empty resets to the config default")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 1, 1); err != nil {
		return err
	}
	id := fs.Args()[0]

	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })

	if visited["title"] && *title == "" {
		return fmt.Errorf("--title must not be empty")
	}
	if visited["priority"] {
		if *priority == "" {
			return fmt.Errorf("--priority must not be empty")
		}
		if !validPriorities[*priority] {
			return fmt.Errorf("invalid priority %q (accepted: low, medium, high)", *priority)
		}
	}
	if visited["agent"] && *agentName != "" && !agent.IsKnown(*agentName) {
		return fmt.Errorf("invalid agent %q (accepted: %s)", *agentName, acceptedAgents())
	}

	var u store.CardUpdate
	if visited["title"] {
		u.Title = title
	}
	if visited["description"] {
		u.Description = description
	}
	if visited["objective"] {
		u.Objective = objective
	}
	if visited["acceptance-criteria"] {
		u.AcceptanceCriteria = acceptanceCriteria
	}
	if visited["priority"] {
		u.Priority = priority
	}
	if visited["labels"] {
		u.Labels = labels
	}
	if visited["agent"] {
		u.Agent = agentName
	}
	if visited["codebase"] {
		if *codebasePath == "" {
			cleared := ""
			u.CodebaseID = &cleared
		} else {
			card, err := a.svc.GetCard(id)
			if err != nil {
				return err
			}
			cb, err := a.findCodebase(card.WorkspaceID, *codebasePath)
			if err != nil {
				return err
			}
			u.CodebaseID = &cb.ID
		}
	}

	c, err := a.svc.UpdateCard(id, u)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "updated card %s\n", c.Title)
	return nil
}

// runCardList prints one line per card as `id  column  [badge]  title
// (priority)`, scoped by --board/--column (defaulting to the resolved
// selection/whole board) and optionally filtered by --search (case-
// insensitive substring over title/description — the store has no query
// primitive for this, so filtering happens client-side).
func runCardList(a *App, args []string) error {
	fs := flag.NewFlagSet("card list", flag.ContinueOnError)
	boardName := fs.String("board", "", "board name")
	columnName := fs.String("column", "", "column name")
	search := fs.String("search", "", "filter by title/description substring")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 0, 0); err != nil {
		return err
	}

	_, b, err := a.boardOf(fs, *boardName)
	if err != nil {
		return err
	}

	var cards []store.Card
	var lerr error
	if *columnName != "" {
		col, ferr := a.findColumn(b.ID, *columnName)
		if ferr != nil {
			return ferr
		}
		cards, lerr = a.svc.ListCardsByColumn(col.ID)
	} else {
		cards, lerr = a.svc.ListCardsByBoard(b.ID)
	}
	if lerr != nil {
		return lerr
	}
	if *search != "" {
		cards = filterCards(cards, *search)
	}

	cols, err := store.ListColumns(a.db, b.ID)
	if err != nil {
		return err
	}
	colNames := make(map[string]string, len(cols))
	for _, c := range cols {
		colNames[c.ID] = c.Name
	}

	for _, c := range cards {
		badge := agentBadge(c.AgentOrDefault(a.cfg.Agent.Default))
		fmt.Fprintf(a.out, "%s\t%s\t[%s]\t%s (%s)\n", c.ID, colNames[c.ColumnID], badge, c.Title, c.Priority)
	}
	return nil
}

// runCardShow prints every card field as `key: value` lines, mirroring the
// TUI detail view's field set (DESIGN-002 §14): agent gains a "(default)"
// suffix when the card's agent column is NULL.
func runCardShow(a *App, args []string) error {
	fs := flag.NewFlagSet("card show", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 1, 1); err != nil {
		return err
	}
	c, err := a.svc.GetCard(fs.Args()[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "id: %s\n", c.ID)
	fmt.Fprintf(a.out, "title: %s\n", c.Title)
	fmt.Fprintf(a.out, "description: %s\n", derefStr(c.Description))
	fmt.Fprintf(a.out, "objective: %s\n", derefStr(c.Objective))
	fmt.Fprintf(a.out, "acceptance criteria: %s\n", derefStr(c.AcceptanceCriteria))
	fmt.Fprintf(a.out, "priority: %s\n", c.Priority)
	fmt.Fprintf(a.out, "labels: %s\n", derefStr(c.Labels))
	resolved := c.AgentOrDefault(a.cfg.Agent.Default)
	if c.Agent == nil || *c.Agent == "" {
		fmt.Fprintf(a.out, "agent: %s (default)\n", resolved)
	} else {
		fmt.Fprintf(a.out, "agent: %s\n", resolved)
	}
	if c.CodebaseID != nil {
		if cb, err := store.GetCodebase(a.db, *c.CodebaseID); err == nil {
			fmt.Fprintf(a.out, "codebase: %s\n", cb.Path)
		}
	}
	return nil
}

// runCardMove resolves <column> against the card's own board (a move never
// changes board — ErrCrossBoardMove guards that at the store layer) and
// appends the card to the end via BoardService.MoveCard, which applies the
// done-stage auto-kill rule (ADR-001 §4.1 step 4, T12).
func runCardMove(a *App, args []string) error {
	fs := flag.NewFlagSet("card move", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 2, 2); err != nil {
		return err
	}
	id, columnName := fs.Args()[0], fs.Args()[1]

	card, err := a.svc.GetCard(id)
	if err != nil {
		return err
	}
	col, err := a.findColumn(card.BoardID, columnName)
	if err != nil {
		return err
	}
	moved, err := a.svc.MoveCard(context.Background(), id, col.ID, nil, nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "moved card %s to %s\n", moved.Title, col.Name)
	return nil
}

// runCardDelete removes the card. It does not touch any running session
// (BoardService.DeleteCard, T12): the tmux session keeps running until the
// agent exits.
func runCardDelete(a *App, args []string) error {
	fs := flag.NewFlagSet("card delete", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 1, 1); err != nil {
		return err
	}
	id := fs.Args()[0]
	c, err := a.svc.GetCard(id)
	if err != nil {
		return err
	}
	if err := a.svc.DeleteCard(id); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "deleted card %s\n", c.Title)
	return nil
}

// columnOf resolves the target column for card add: an explicit --column
// matches by name within the board, otherwise the board's first column
// (lowest position) is used.
func (a *App) columnOf(boardID, columnName string) (store.Column, error) {
	if columnName != "" {
		return a.findColumn(boardID, columnName)
	}
	cs, err := store.ListColumns(a.db, boardID)
	if err != nil {
		return store.Column{}, err
	}
	if len(cs) == 0 {
		return store.Column{}, fmt.Errorf("board has no columns")
	}
	return cs[0], nil
}

// findCodebase resolves a --codebase path to its registered Codebase row.
// Codebases are always stored as absolute paths (runCodebaseAdd), so the
// lookup path is normalized the same way before matching.
func (a *App) findCodebase(workspaceID, path string) (store.Codebase, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return store.Codebase{}, err
	}
	cbs, err := store.ListCodebases(a.db, workspaceID)
	if err != nil {
		return store.Codebase{}, err
	}
	for _, cb := range cbs {
		if cb.Path == abs {
			return cb, nil
		}
	}
	return store.Codebase{}, fmt.Errorf("codebase %q not found", path)
}

// filterCards keeps cards whose title or description contains q
// (case-insensitive); the store has no search primitive, so `card list
// --search` filters client-side over an already-scoped card set.
func filterCards(cards []store.Card, q string) []store.Card {
	q = strings.ToLower(q)
	out := make([]store.Card, 0, len(cards))
	for _, c := range cards {
		if strings.Contains(strings.ToLower(c.Title), q) {
			out = append(out, c)
			continue
		}
		if c.Description != nil && strings.Contains(strings.ToLower(*c.Description), q) {
			out = append(out, c)
		}
	}
	return out
}

// agentBadge is the short tag DESIGN-002 §14 defines for the TUI card badge,
// reused here for `card list`'s badge column: "cl" for claude, "oc" for
// opencode, the resolved name itself for any future driver.
func agentBadge(name string) string {
	switch name {
	case "claude":
		return "cl"
	case "opencode":
		return "oc"
	default:
		return name
	}
}

// acceptedAgents renders agent.Known() as a quoted, comma-joined list for
// error messages (mirrors validStages' invalid-stage message style).
func acceptedAgents() string {
	names := agent.Known()
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(quoted, ", ")
}

// derefStr renders a nullable string field as "" when unset, for card show's
// `key: value` lines.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
