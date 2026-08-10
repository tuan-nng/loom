package cli

import (
	"strings"
	"testing"

	"loom/internal/store"
)

func runWith(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	a, out, errw := newTestApp(t, &stubSess{})
	code := a.finish(a.run(args))
	return code, out.String(), errw.String()
}

func TestRunBarePrintsHelp(t *testing.T) {
	code, out, _ := runWith(t)
	if code != 0 {
		t.Fatalf("bare loom exit = %d, want 0", code)
	}
	if !strings.Contains(out, "usage: loom <command> [args]") {
		t.Errorf("bare loom help missing usage line:\n%s", out)
	}
	if !strings.Contains(out, "loom workspace switch <name>") {
		t.Errorf("help missing workspace switch:\n%s", out)
	}
}

func TestRunVersion(t *testing.T) {
	code, out, _ := runWith(t, "version")
	if code != 0 {
		t.Fatalf("version exit = %d, want 0", code)
	}
	if strings.TrimSpace(out) != "loom "+Version {
		t.Errorf("version = %q, want %q", out, "loom "+Version)
	}
}

func TestRunHelp(t *testing.T) {
	code, out, _ := runWith(t, "help")
	if code != 0 {
		t.Fatalf("help exit = %d, want 0", code)
	}
	if !strings.Contains(out, "loom column add <name>") {
		t.Errorf("help missing column add:\n%s", out)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	code, _, errw := runWith(t, "frobnicate")
	if code != 1 {
		t.Fatalf("unknown command exit = %d, want 1", code)
	}
	if !strings.Contains(errw, "unknown command \"frobnicate\"") {
		t.Errorf("stderr = %q, want unknown-command message", errw)
	}
	if !strings.Contains(errw, "run 'loom help'") {
		t.Errorf("stderr missing help hint: %q", errw)
	}
}

func TestRunInitIdempotent(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	dir := t.TempDir()
	if err := a.run([]string{"init", dir}); err != nil {
		t.Fatalf("init: %v", err)
	}
	first := out.String()
	out.Reset()
	if err := a.run([]string{"init", dir}); err != nil {
		t.Fatalf("second init: %v", err)
	}
	if out.String() != first {
		t.Errorf("init not idempotent: second output %q != first %q", out.String(), first)
	}
}

func TestRunInitCreatesWorkspaceBoardColumns(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	dir := t.TempDir()
	if err := a.run([]string{"init", dir}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if !strings.Contains(out.String(), "initialized") {
		t.Errorf("init output missing 'initialized': %q", out.String())
	}
	ws, err := store.WorkspaceByRootPath(a.db, dir)
	if err != nil {
		t.Fatalf("workspace not created: %v", err)
	}
	boards, err := store.ListBoards(a.db, ws.ID)
	if err != nil || len(boards) != 1 {
		t.Fatalf("boards = %v (err %v), want 1", boards, err)
	}
	cols, err := store.ListColumns(a.db, boards[0].ID)
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}
	if len(cols) != 5 {
		t.Errorf("default columns = %d, want 5", len(cols))
	}
}

func TestRunWorkspaceCreateUsesCwd(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	if err := a.run([]string{"workspace", "create", "demo"}); err != nil {
		t.Fatalf("workspace create: %v", err)
	}
	if !strings.Contains(out.String(), "created workspace demo") {
		t.Errorf("output = %q", out.String())
	}
	ws, err := store.ListWorkspaces(a.db)
	if err != nil || len(ws) != 1 {
		t.Fatalf("workspaces = %v (err %v), want 1", ws, err)
	}
}

func TestRunWorkspaceList(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	if err := a.run([]string{"workspace", "create", "alpha"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := a.run([]string{"workspace", "create", "beta"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := a.run([]string{"workspace", "list"}); err != nil {
		t.Fatalf("list: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "alpha\t") || !strings.Contains(got, "beta\t") {
		t.Errorf("workspace list missing entries:\n%s", got)
	}
}

func TestRunWorkspaceSwitchPersistsSelection(t *testing.T) {
	a, _, _ := newTestApp(t, &stubSess{})
	if err := a.run([]string{"workspace", "create", "one"}); err != nil {
		t.Fatalf("create one: %v", err)
	}
	if err := a.run([]string{"workspace", "create", "two"}); err != nil {
		t.Fatalf("create two: %v", err)
	}
	if err := a.run([]string{"workspace", "switch", "one"}); err != nil {
		t.Fatalf("switch: %v", err)
	}
	st, err := store.GetUIState(a.db)
	if err != nil {
		t.Fatalf("GetUIState: %v", err)
	}
	if st.LastWorkspaceID == nil {
		t.Fatal("selection not persisted")
	}
	ws, err := store.GetWorkspace(a.db, *st.LastWorkspaceID)
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if ws.Name != "one" {
		t.Errorf("selected workspace = %q, want one", ws.Name)
	}
}

func TestRunBoardCreateShowPersistSelection(t *testing.T) {
	a, _, _ := newTestApp(t, &stubSess{})
	if err := a.run([]string{"init", t.TempDir()}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := a.run([]string{"board", "create", "Second"}); err != nil {
		t.Fatalf("board create: %v", err)
	}
	if err := a.run([]string{"board", "show", "Second"}); err != nil {
		t.Fatalf("board show: %v", err)
	}
	st, err := store.GetUIState(a.db)
	if err != nil {
		t.Fatalf("GetUIState: %v", err)
	}
	if st.LastBoardID == nil {
		t.Fatal("board selection not persisted")
	}
	b, err := store.GetBoard(a.db, *st.LastBoardID)
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if b.Name != "Second" {
		t.Errorf("selected board = %q, want Second", b.Name)
	}
}

func TestRunColumnAddStage(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	if err := a.run([]string{"init", t.TempDir()}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := a.run([]string{"column", "add", "QA", "--stage", "review"}); err != nil {
		t.Fatalf("column add: %v", err)
	}
	if !strings.Contains(out.String(), "added column QA (stage review)") {
		t.Errorf("output = %q", out.String())
	}
	// default stage
	if err := a.run([]string{"column", "add", "Defaults"}); err != nil {
		t.Fatalf("column add default stage: %v", err)
	}
	if !strings.Contains(out.String(), "Defaults (stage todo)") {
		t.Errorf("default-stage output missing: %q", out.String())
	}
	// invalid stage
	if err := a.run([]string{"column", "add", "Bad", "--stage", "bogus"}); err == nil {
		t.Error("invalid stage accepted, want error")
	} else if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("invalid-stage error = %q, want mention of bogus", err.Error())
	}
}

func TestRunColumnListDelete(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	if err := a.run([]string{"init", t.TempDir()}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := a.run([]string{"column", "add", "QA", "--stage", "review"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	out.Reset()
	if err := a.run([]string{"column", "list"}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out.String(), "QA\treview") {
		t.Errorf("column list missing QA:\n%s", out.String())
	}
	if err := a.run([]string{"column", "delete", "QA"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	out.Reset()
	if err := a.run([]string{"column", "list"}); err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if strings.Contains(out.String(), "QA") {
		t.Errorf("column list still shows QA after delete:\n%s", out.String())
	}
}

func TestRunBoardListDelete(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	if err := a.run([]string{"init", t.TempDir()}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := a.run([]string{"board", "create", "Second"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := a.run([]string{"board", "list"}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out.String(), "Board\n") || !strings.Contains(out.String(), "Second\n") {
		t.Errorf("board list missing entries:\n%s", out.String())
	}
	if err := a.run([]string{"board", "delete", "Second"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestRunWorkspaceCodebase(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	if err := a.run([]string{"init", t.TempDir()}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := a.run([]string{"workspace", "codebase", "add", t.TempDir()}); err != nil {
		t.Fatalf("codebase add: %v", err)
	}
	if err := a.run([]string{"workspace", "codebase", "list"}); err != nil {
		t.Fatalf("codebase list: %v", err)
	}
	if !strings.Contains(out.String(), "added codebase") {
		t.Errorf("output = %q", out.String())
	}
}

// TestRunColumnBoardFlag covers the --board flag's workspace-search path and
// the codebase list output through the resolved workspace.
func TestRunColumnBoardFlag(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	if err := a.run([]string{"init", t.TempDir()}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := a.run([]string{"board", "create", "Sprint2"}); err != nil {
		t.Fatalf("board create: %v", err)
	}
	if err := a.run([]string{"column", "add", "QA", "--board", "Sprint2", "--stage", "review"}); err != nil {
		t.Fatalf("column add --board: %v", err)
	}
	if !strings.Contains(out.String(), "to board Sprint2") {
		t.Errorf("output = %q", out.String())
	}
	out.Reset()
	if err := a.run([]string{"column", "list", "--board", "Sprint2"}); err != nil {
		t.Fatalf("column list --board: %v", err)
	}
	if !strings.Contains(out.String(), "QA\treview") {
		t.Errorf("column list --board missing QA:\n%s", out.String())
	}
	// unknown board name fails loudly
	if err := a.run([]string{"column", "list", "--board", "Nope"}); err == nil {
		t.Error("column list --board Nope succeeded, want error")
	}
}

func TestRunStubNotBuilt(t *testing.T) {
	code, _, errw := runWith(t, "card", "list")
	if code != 1 {
		t.Fatalf("card stub exit = %d, want 1", code)
	}
	if !strings.Contains(errw, "not implemented") {
		t.Errorf("card stub stderr = %q, want 'not implemented'", errw)
	}
}

func TestRunUsageErrorForBadArgs(t *testing.T) {
	code, _, errw := runWith(t, "workspace", "create")
	if code != 1 {
		t.Fatalf("bad args exit = %d, want 1", code)
	}
	if !strings.Contains(errw, "run 'loom help'") {
		t.Errorf("stderr missing help hint: %q", errw)
	}
}

func TestRunGroupWithoutSubcommand(t *testing.T) {
	code, _, errw := runWith(t, "workspace")
	if code != 1 {
		t.Fatalf("bare group exit = %d, want 1", code)
	}
	if !strings.Contains(errw, "run 'loom help'") {
		t.Errorf("bare group stderr missing help hint: %q", errw)
	}
}

func TestRunExtraArgs(t *testing.T) {
	code, _, errw := runWith(t, "version", "extra")
	if code != 1 {
		t.Fatalf("version extra exit = %d, want 1", code)
	}
	if !strings.Contains(errw, "expected 0 argument(s), got 1") {
		t.Errorf("stderr missing arity message: %q", errw)
	}
}

func TestRunWorkspaceSwitchMissing(t *testing.T) {
	a, _, _ := newTestApp(t, &stubSess{})
	if err := a.run([]string{"workspace", "switch", "nope"}); err == nil {
		t.Fatal("switch to missing workspace succeeded, want error")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Errorf("switch error = %q, want 'not found'", err.Error())
	}
}

// TestRunUninitializedErrors covers the state-dependent commands failing
// through the fallback chain with the "run loom init" hint before any
// workspace exists.
func TestRunUninitializedErrors(t *testing.T) {
	for _, args := range [][]string{
		{"board", "list"},
		{"board", "create", "X"},
		{"column", "list"},
		{"workspace", "codebase", "list"},
		{"workspace", "codebase", "add", "/tmp"},
	} {
		a, _, _ := newTestApp(t, &stubSess{})
		err := a.run(args)
		if err == nil {
			t.Errorf("%v succeeded on empty db, want error", args)
			continue
		}
		if !strings.Contains(err.Error(), "run loom init") {
			t.Errorf("%v error = %q, want 'run loom init'", args, err.Error())
		}
	}
}

func TestRunBoardShowDeleteMissing(t *testing.T) {
	a, _, _ := newTestApp(t, &stubSess{})
	if err := a.run([]string{"init", t.TempDir()}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := a.run([]string{"board", "show", "Nope"}); err == nil {
		t.Error("board show missing succeeded, want error")
	}
	if err := a.run([]string{"board", "delete", "Nope"}); err == nil {
		t.Error("board delete missing succeeded, want error")
	}
	if err := a.run([]string{"column", "delete", "Nope"}); err == nil {
		t.Error("column delete missing succeeded, want error")
	}
}

// TestRunBoardCreateAfterWorkspaceCreate covers the boardless-workspace flow:
// `workspace create` makes a bare workspace, and `board create` must succeed
// (resolving the workspace only), seeding its five columns.
func TestRunBoardCreateAfterWorkspaceCreate(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	if err := a.run([]string{"workspace", "create", "demo"}); err != nil {
		t.Fatalf("workspace create: %v", err)
	}
	if err := a.run([]string{"board", "create", "Board"}); err != nil {
		t.Fatalf("board create in bare workspace: %v", err)
	}
	if !strings.Contains(out.String(), "created board Board") {
		t.Errorf("output = %q", out.String())
	}
	ws, err := a.svc.ResolveWorkspace()
	if err != nil {
		t.Fatalf("ResolveWorkspace: %v", err)
	}
	bs, err := store.ListBoards(a.db, ws.ID)
	if err != nil || len(bs) != 1 {
		t.Fatalf("boards = %v (err %v), want 1", bs, err)
	}
	cols, err := store.ListColumns(a.db, bs[0].ID)
	if err != nil || len(cols) != 5 {
		t.Fatalf("default columns = %v (err %v), want 5", cols, err)
	}
}

// TestRunHelpFlag covers --help/-h/help routing: all three exit 0 with help on
// stdout, and a leaf --help prints its usage line.
func TestRunHelpFlag(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"column", "add", "--help"}} {
		a, out, errw := newTestApp(t, &stubSess{})
		code := a.finish(a.run(args))
		if code != 0 {
			t.Errorf("%v exit = %d, want 0 (stderr: %q)", args, code, errw.String())
		}
		if len(args) > 1 && !strings.Contains(out.String(), "usage: column add") {
			t.Errorf("leaf --help output = %q, want column add usage", out.String())
		}
		if len(args) == 1 && !strings.Contains(out.String(), "usage: loom <command>") {
			t.Errorf("help output missing usage line: %q", out.String())
		}
	}
}
