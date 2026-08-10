package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"loom/internal/store"
)

// seedCardBoard inits a workspace/board in a's db and returns columnID by
// stage, mirroring seedStatusDB (status_test.go).
func seedCardBoard(t *testing.T, a *App) map[string]string {
	t.Helper()
	if err := a.run([]string{"init", t.TempDir()}); err != nil {
		t.Fatalf("init: %v", err)
	}
	_, b, err := a.svc.ResolveSelection()
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}
	cols, err := store.ListColumns(a.db, b.ID)
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}
	byStage := make(map[string]string, len(cols))
	for _, c := range cols {
		byStage[c.Stage] = c.ID
	}
	return byStage
}

func onlyCard(t *testing.T, a *App) store.Card {
	t.Helper()
	_, b, err := a.svc.ResolveSelection()
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}
	cards, err := a.svc.ListCardsByBoard(b.ID)
	if err != nil {
		t.Fatalf("ListCardsByBoard: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("cards = %v, want exactly 1", cards)
	}
	return cards[0]
}

func TestRunCardAddDefaults(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	seedCardBoard(t, a)
	if err := a.run([]string{"card", "add", "Alpha"}); err != nil {
		t.Fatalf("card add: %v", err)
	}
	if !strings.Contains(out.String(), "created card Alpha (") {
		t.Errorf("add output = %q", out.String())
	}
	c := onlyCard(t, a)
	if c.Priority != "medium" {
		t.Errorf("priority = %q, want medium (store default)", c.Priority)
	}
	if c.Agent != nil {
		t.Errorf("agent = %v, want nil", c.Agent)
	}
	_, b, _ := a.svc.ResolveSelection()
	cols, _ := store.ListColumns(a.db, b.ID)
	if c.ColumnID != cols[0].ID {
		t.Errorf("card landed in column %s, want first column %s", c.ColumnID, cols[0].ID)
	}
}

func TestRunCardAddAllFlags(t *testing.T) {
	a, _, _ := newTestApp(t, &stubSess{})
	seedCardBoard(t, a)
	ws, err := a.svc.ResolveWorkspace()
	if err != nil {
		t.Fatalf("ResolveWorkspace: %v", err)
	}
	cbDir := t.TempDir()
	if _, err := store.CreateCodebase(a.db, ws.ID, cbDir); err != nil {
		t.Fatalf("CreateCodebase: %v", err)
	}

	if err := a.run([]string{"card", "add", "Beta",
		"--description", "desc", "--objective", "obj",
		"--acceptance-criteria", "ac", "--priority", "high",
		"--labels", "a,b", "--codebase", cbDir, "--agent", "opencode",
	}); err != nil {
		t.Fatalf("card add: %v", err)
	}

	c := onlyCard(t, a)
	if c.Priority != "high" {
		t.Errorf("priority = %q, want high", c.Priority)
	}
	if c.Agent == nil || *c.Agent != "opencode" {
		t.Errorf("agent = %v, want opencode", c.Agent)
	}
	if c.Description == nil || *c.Description != "desc" {
		t.Errorf("description = %v", c.Description)
	}
	if c.Objective == nil || *c.Objective != "obj" {
		t.Errorf("objective = %v", c.Objective)
	}
	if c.AcceptanceCriteria == nil || *c.AcceptanceCriteria != "ac" {
		t.Errorf("acceptance criteria = %v", c.AcceptanceCriteria)
	}
	if c.Labels == nil || *c.Labels != "a,b" {
		t.Errorf("labels = %v", c.Labels)
	}
	if c.CodebaseID == nil {
		t.Fatalf("codebase id not set")
	}
	abs, _ := filepath.Abs(cbDir)
	cb, err := store.GetCodebase(a.db, *c.CodebaseID)
	if err != nil || cb.Path != abs {
		t.Errorf("codebase = %+v (err %v), want path %s", cb, err, abs)
	}
}

func TestRunCardAddExplicitColumn(t *testing.T) {
	a, _, _ := newTestApp(t, &stubSess{})
	cols := seedCardBoard(t, a)
	if err := a.run([]string{"card", "add", "X", "--column", "Review"}); err != nil {
		t.Fatalf("card add: %v", err)
	}
	c := onlyCard(t, a)
	if c.ColumnID != cols["review"] {
		t.Errorf("column = %s, want review column %s", c.ColumnID, cols["review"])
	}
}

func TestRunCardAddInvalidPriority(t *testing.T) {
	a, _, _ := newTestApp(t, &stubSess{})
	seedCardBoard(t, a)
	err := a.run([]string{"card", "add", "X", "--priority", "urgent"})
	if err == nil || !strings.Contains(err.Error(), "invalid priority") {
		t.Fatalf("err = %v, want invalid priority", err)
	}
}

func TestRunCardAddInvalidAgent(t *testing.T) {
	a, _, _ := newTestApp(t, &stubSess{})
	seedCardBoard(t, a)
	err := a.run([]string{"card", "add", "X", "--agent", "bogus"})
	if err == nil || !strings.Contains(err.Error(), `invalid agent "bogus"`) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), `"claude"`) || !strings.Contains(err.Error(), `"opencode"`) {
		t.Errorf("err missing accepted values: %v", err)
	}
}

func TestRunCardUpdateFields(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	seedCardBoard(t, a)
	if err := a.run([]string{"card", "add", "Gamma"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	id := onlyCard(t, a).ID

	if err := a.run([]string{"card", "update", id, "--title", "Gamma2", "--priority", "low"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !strings.Contains(out.String(), "updated card Gamma2") {
		t.Errorf("update output = %q", out.String())
	}
	c, err := a.svc.GetCard(id)
	if err != nil {
		t.Fatalf("GetCard: %v", err)
	}
	if c.Title != "Gamma2" || c.Priority != "low" {
		t.Errorf("card = %+v", c)
	}
}

func TestRunCardUpdateEmptyTitleRejected(t *testing.T) {
	a, _, _ := newTestApp(t, &stubSess{})
	seedCardBoard(t, a)
	if err := a.run([]string{"card", "add", "Delta"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	id := onlyCard(t, a).ID
	err := a.run([]string{"card", "update", id, "--title="})
	if err == nil || !strings.Contains(err.Error(), "--title must not be empty") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunCardUpdateAgentResetToDefault covers the acceptance case: --agent=
// clears to NULL and a later config change re-defaults the card
// (AgentOrDefault re-resolves at read time, not write time).
func TestRunCardUpdateAgentResetToDefault(t *testing.T) {
	a, _, _ := newTestApp(t, &stubSess{})
	seedCardBoard(t, a)
	if err := a.run([]string{"card", "add", "Epsilon", "--agent", "claude"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	id := onlyCard(t, a).ID

	if c, err := a.svc.GetCard(id); err != nil || c.Agent == nil || *c.Agent != "claude" {
		t.Fatalf("precondition: agent = %v (err %v)", c.Agent, err)
	}

	if err := a.run([]string{"card", "update", id, "--agent="}); err != nil {
		t.Fatalf("update --agent=: %v", err)
	}
	c, err := a.svc.GetCard(id)
	if err != nil {
		t.Fatalf("GetCard: %v", err)
	}
	if c.Agent != nil {
		t.Fatalf("agent = %v, want nil after reset", c.Agent)
	}
	if got := c.AgentOrDefault(a.cfg.Agent.Default); got != "claude" {
		t.Errorf("AgentOrDefault = %q, want config default %q", got, a.cfg.Agent.Default)
	}

	a.cfg.Agent.Default = "opencode"
	if got := c.AgentOrDefault(a.cfg.Agent.Default); got != "opencode" {
		t.Errorf("AgentOrDefault after config change = %q, want opencode", got)
	}
}

func TestRunCardUpdateInvalidAgent(t *testing.T) {
	a, _, _ := newTestApp(t, &stubSess{})
	seedCardBoard(t, a)
	if err := a.run([]string{"card", "add", "Zeta"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	id := onlyCard(t, a).ID
	err := a.run([]string{"card", "update", id, "--agent", "bogus"})
	if err == nil || !strings.Contains(err.Error(), "invalid agent") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunCardUpdateCodebaseClear(t *testing.T) {
	a, _, _ := newTestApp(t, &stubSess{})
	seedCardBoard(t, a)
	ws, err := a.svc.ResolveWorkspace()
	if err != nil {
		t.Fatalf("ResolveWorkspace: %v", err)
	}
	cbDir := t.TempDir()
	if _, err := store.CreateCodebase(a.db, ws.ID, cbDir); err != nil {
		t.Fatalf("CreateCodebase: %v", err)
	}
	if err := a.run([]string{"card", "add", "Eta", "--codebase", cbDir}); err != nil {
		t.Fatalf("add: %v", err)
	}
	id := onlyCard(t, a).ID
	if c, err := a.svc.GetCard(id); err != nil || c.CodebaseID == nil {
		t.Fatalf("precondition: codebase not set (err %v)", err)
	}

	if err := a.run([]string{"card", "update", id, "--codebase="}); err != nil {
		t.Fatalf("update: %v", err)
	}
	c, err := a.svc.GetCard(id)
	if err != nil {
		t.Fatalf("GetCard: %v", err)
	}
	if c.CodebaseID != nil {
		t.Errorf("codebase = %v, want nil after clear", c.CodebaseID)
	}
}

func TestRunCardListBadgeAndSearch(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	seedCardBoard(t, a)
	if err := a.run([]string{"card", "add", "Login flow", "--agent", "claude"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := a.run([]string{"card", "add", "Signup flow", "--agent", "opencode"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := a.run([]string{"card", "add", "Other task"}); err != nil { // NULL agent -> default "claude"
		t.Fatalf("add: %v", err)
	}

	out.Reset()
	if err := a.run([]string{"card", "list"}); err != nil {
		t.Fatalf("list: %v", err)
	}
	got := out.String()
	for _, want := range []string{"[cl]", "[oc]", "Login flow", "Signup flow", "Other task"} {
		if !strings.Contains(got, want) {
			t.Errorf("list output missing %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "[cl]"); n != 2 {
		t.Errorf("expected 2 [cl] badges (explicit claude + NULL-default), got %d:\n%s", n, got)
	}

	out.Reset()
	if err := a.run([]string{"card", "list", "--search", "flow"}); err != nil {
		t.Fatalf("list --search: %v", err)
	}
	got = out.String()
	if !strings.Contains(got, "Login flow") || !strings.Contains(got, "Signup flow") {
		t.Errorf("search missing matches:\n%s", got)
	}
	if strings.Contains(got, "Other task") {
		t.Errorf("search should exclude non-matching card:\n%s", got)
	}
}

func TestRunCardListByColumn(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	seedCardBoard(t, a)
	if err := a.run([]string{"card", "add", "InBacklog"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := a.run([]string{"card", "add", "InReview", "--column", "Review"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	out.Reset()
	if err := a.run([]string{"card", "list", "--column", "Review"}); err != nil {
		t.Fatalf("list: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "InReview") {
		t.Errorf("column filter missing InReview:\n%s", got)
	}
	if strings.Contains(got, "InBacklog") {
		t.Errorf("column filter should exclude InBacklog:\n%s", got)
	}
}

func TestRunCardShow(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	seedCardBoard(t, a)
	if err := a.run([]string{"card", "add", "Theta", "--description", "d", "--priority", "high"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	id := onlyCard(t, a).ID

	out.Reset()
	if err := a.run([]string{"card", "show", id}); err != nil {
		t.Fatalf("show: %v", err)
	}
	got := out.String()
	for _, want := range []string{"id: " + id, "title: Theta", "description: d", "priority: high", "agent: claude (default)"} {
		if !strings.Contains(got, want) {
			t.Errorf("show output missing %q:\n%s", want, got)
		}
	}
}

func TestRunCardShowExplicitAgentNoDefaultSuffix(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	seedCardBoard(t, a)
	if err := a.run([]string{"card", "add", "Iota", "--agent", "opencode"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	id := onlyCard(t, a).ID

	out.Reset()
	if err := a.run([]string{"card", "show", id}); err != nil {
		t.Fatalf("show: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "agent: opencode\n") {
		t.Errorf("show output = %q", got)
	}
	if strings.Contains(got, "(default)") {
		t.Errorf("explicit agent should not show (default):\n%s", got)
	}
}

func TestRunCardMove(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	cols := seedCardBoard(t, a)
	if err := a.run([]string{"card", "add", "Kappa"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	id := onlyCard(t, a).ID

	out.Reset()
	if err := a.run([]string{"card", "move", id, "Review"}); err != nil {
		t.Fatalf("move: %v", err)
	}
	if !strings.Contains(out.String(), "moved card Kappa to Review") {
		t.Errorf("move output = %q", out.String())
	}
	c, err := a.svc.GetCard(id)
	if err != nil {
		t.Fatalf("GetCard: %v", err)
	}
	if c.ColumnID != cols["review"] {
		t.Errorf("column = %s, want %s", c.ColumnID, cols["review"])
	}
}

func TestRunCardMoveToDoneKillsSession(t *testing.T) {
	stub := &stubSess{}
	a, _, _ := newTestApp(t, stub)
	seedCardBoard(t, a)
	if err := a.run([]string{"card", "add", "Lambda"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	id := onlyCard(t, a).ID

	if err := a.run([]string{"card", "move", id, "Done"}); err != nil {
		t.Fatalf("move: %v", err)
	}
	if stub.killCalls != 1 {
		t.Errorf("kill calls = %d, want 1", stub.killCalls)
	}
}

func TestRunCardMoveUnknownColumn(t *testing.T) {
	a, _, _ := newTestApp(t, &stubSess{})
	seedCardBoard(t, a)
	if err := a.run([]string{"card", "add", "Mu"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	id := onlyCard(t, a).ID
	err := a.run([]string{"card", "move", id, "Nonexistent"})
	if err == nil || !strings.Contains(err.Error(), `column "Nonexistent" not found`) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunCardDelete(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	seedCardBoard(t, a)
	if err := a.run([]string{"card", "add", "Nu"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	id := onlyCard(t, a).ID

	out.Reset()
	if err := a.run([]string{"card", "delete", id}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(out.String(), "deleted card Nu") {
		t.Errorf("delete output = %q", out.String())
	}
	if _, err := a.svc.GetCard(id); err == nil {
		t.Errorf("card still exists after delete")
	}
}
