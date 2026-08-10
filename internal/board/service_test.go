package board

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"loom/internal/session"
	"loom/internal/store"
)

func openBoardDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "loom.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// fakeManager records session calls for assertion and injects failures.
type fakeManager struct {
	ensured   []store.Card
	attached  []store.Card
	killed    []store.Card
	statusN   int
	reconcile int
	ensureErr error
	attachErr error
	killErr   error
	statusRes map[string]session.SessionStatus
	statusErr error
}

func (f *fakeManager) Ensure(ctx context.Context, c store.Card) error {
	f.ensured = append(f.ensured, c)
	return f.ensureErr
}
func (f *fakeManager) Attach(ctx context.Context, c store.Card) error {
	f.attached = append(f.attached, c)
	return f.attachErr
}
func (f *fakeManager) Kill(ctx context.Context, c store.Card) error {
	f.killed = append(f.killed, c)
	return f.killErr
}
func (f *fakeManager) Status(ctx context.Context) (map[string]session.SessionStatus, error) {
	f.statusN++
	return f.statusRes, f.statusErr
}
func (f *fakeManager) ReconcileOnStartup(ctx context.Context) error {
	f.reconcile++
	return nil
}

func newService(t *testing.T, db *sql.DB, f *fakeManager) (*Service, *fakeManager) {
	t.Helper()
	return NewService(db, f), f
}

func seedWorkspaceBoard(t *testing.T, db *sql.DB, name string) (store.Workspace, store.Board, []store.Column) {
	t.Helper()
	ws, err := store.CreateWorkspace(db, name, t.TempDir())
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	b, err := store.CreateBoard(db, ws.ID, "Board")
	if err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}
	cols, err := store.ListColumns(db, b.ID)
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}
	return ws, b, cols
}

func columnWithStage(cols []store.Column, stage string) store.Column {
	for _, c := range cols {
		if c.Stage == stage {
			return c
		}
	}
	panic("no column with stage " + stage)
}

// bumpCreatedAt makes ws unambiguously the most recent: two inserts can share
// a millisecond, and created_at ties are harmless per §3.3 (the store test
// disambiguates the same way).
func bumpCreatedAt(t *testing.T, db *sql.DB, wsID string) {
	t.Helper()
	if _, err := db.Exec(
		"UPDATE workspaces SET created_at = '2099-01-01T00:00:00.000' WHERE id = ?", wsID,
	); err != nil {
		t.Fatalf("bump created_at: %v", err)
	}
}

func mustCard(t *testing.T, db *sql.DB, columnID, title string) store.Card {
	t.Helper()
	c, err := store.CreateCard(db, store.CardInput{ColumnID: columnID, Title: title})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	return c
}

func TestMoveCardDoneStageKills(t *testing.T) {
	db := openBoardDB(t)
	_, _, cols := seedWorkspaceBoard(t, db, "ws")
	card := mustCard(t, db, columnWithStage(cols, "todo").ID, "Card")
	doneCol := columnWithStage(cols, "done")
	svc, f := newService(t, db, &fakeManager{})

	moved, err := svc.MoveCard(context.Background(), card.ID, doneCol.ID, nil, nil)
	if err != nil {
		t.Fatalf("MoveCard: %v", err)
	}
	if moved.ColumnID != doneCol.ID {
		t.Fatalf("moved card column = %s, want %s", moved.ColumnID, doneCol.ID)
	}
	if len(f.killed) != 1 || f.killed[0].ID != card.ID {
		t.Fatalf("Kill calls = %v, want exactly [%s]", f.killed, card.ID)
	}
}

func TestMoveCardNonDoneNoKill(t *testing.T) {
	db := openBoardDB(t)
	_, _, cols := seedWorkspaceBoard(t, db, "ws")
	card := mustCard(t, db, columnWithStage(cols, "backlog").ID, "Card")
	svc, f := newService(t, db, &fakeManager{})

	toCol := columnWithStage(cols, "review")
	if _, err := svc.MoveCard(context.Background(), card.ID, toCol.ID, nil, nil); err != nil {
		t.Fatalf("MoveCard: %v", err)
	}
	if len(f.killed) != 0 {
		t.Fatalf("Kill calls = %v, want none for non-done move", f.killed)
	}
}

func TestMoveCardCrossBoardNoKill(t *testing.T) {
	db := openBoardDB(t)
	_, _, cols := seedWorkspaceBoard(t, db, "ws")
	card := mustCard(t, db, columnWithStage(cols, "todo").ID, "Card")

	_, board2, _ := seedWorkspaceBoard(t, db, "ws2")
	cols2, err := store.ListColumns(db, board2.ID)
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}
	done2 := columnWithStage(cols2, "done")
	svc, f := newService(t, db, &fakeManager{})

	_, err = svc.MoveCard(context.Background(), card.ID, done2.ID, nil, nil)
	if !errors.Is(err, store.ErrCrossBoardMove) {
		t.Fatalf("MoveCard err = %v, want ErrCrossBoardMove", err)
	}
	if len(f.killed) != 0 {
		t.Fatalf("Kill calls = %v, want none for rejected move", f.killed)
	}
	got, err := store.GetCard(db, card.ID)
	if err != nil {
		t.Fatalf("GetCard: %v", err)
	}
	if got.ColumnID != card.ColumnID {
		t.Fatalf("card moved despite rejection: column %s, want %s", got.ColumnID, card.ColumnID)
	}
}

func TestMoveCardPartialAnchorsNoKill(t *testing.T) {
	db := openBoardDB(t)
	_, _, cols := seedWorkspaceBoard(t, db, "ws")
	card := mustCard(t, db, columnWithStage(cols, "todo").ID, "Card")
	doneCol := columnWithStage(cols, "done")
	svc, f := newService(t, db, &fakeManager{})

	_, err := svc.MoveCard(context.Background(), card.ID, doneCol.ID, nil, &card.ID)
	if !errors.Is(err, store.ErrPartialAnchors) {
		t.Fatalf("MoveCard err = %v, want ErrPartialAnchors", err)
	}
	if len(f.killed) != 0 {
		t.Fatalf("Kill calls = %v, want none for rejected move", f.killed)
	}
}

func TestMoveCardKillErrorSurfacesAfterMove(t *testing.T) {
	db := openBoardDB(t)
	_, _, cols := seedWorkspaceBoard(t, db, "ws")
	card := mustCard(t, db, columnWithStage(cols, "todo").ID, "Card")
	doneCol := columnWithStage(cols, "done")
	svc, f := newService(t, db, &fakeManager{killErr: errors.New("forced: kill")})

	_, err := svc.MoveCard(context.Background(), card.ID, doneCol.ID, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "forced: kill") || !strings.HasPrefix(err.Error(), "board:") {
		t.Fatalf("MoveCard err = %v, want board-wrapped kill failure", err)
	}
	got, gerr := store.GetCard(db, card.ID)
	if gerr != nil {
		t.Fatalf("GetCard: %v", gerr)
	}
	if got.ColumnID != doneCol.ID {
		t.Fatalf("move rolled back after kill failure: column %s, want %s", got.ColumnID, doneCol.ID)
	}
	if len(f.killed) != 1 {
		t.Fatalf("Kill calls = %v, want 1", f.killed)
	}
}

func TestOpenCardAttaches(t *testing.T) {
	db := openBoardDB(t)
	_, _, cols := seedWorkspaceBoard(t, db, "ws")
	card := mustCard(t, db, columnWithStage(cols, "todo").ID, "Card")
	svc, f := newService(t, db, &fakeManager{})

	if err := svc.OpenCard(context.Background(), card.ID, false); err != nil {
		t.Fatalf("OpenCard: %v", err)
	}
	if len(f.ensured) != 1 || f.ensured[0].ID != card.ID {
		t.Fatalf("Ensure calls = %v, want [%s]", f.ensured, card.ID)
	}
	if len(f.attached) != 1 || f.attached[0].ID != card.ID {
		t.Fatalf("Attach calls = %v, want [%s]", f.attached, card.ID)
	}
}

func TestOpenCardDetach(t *testing.T) {
	db := openBoardDB(t)
	_, _, cols := seedWorkspaceBoard(t, db, "ws")
	card := mustCard(t, db, columnWithStage(cols, "todo").ID, "Card")
	svc, f := newService(t, db, &fakeManager{})

	if err := svc.OpenCard(context.Background(), card.ID, true); err != nil {
		t.Fatalf("OpenCard detach: %v", err)
	}
	if len(f.ensured) != 1 {
		t.Fatalf("Ensure calls = %v, want 1", f.ensured)
	}
	if len(f.attached) != 0 {
		t.Fatalf("Attach calls = %v, want none for --detach", f.attached)
	}
}

func TestOpenCardEnsureErrorStopsAttach(t *testing.T) {
	db := openBoardDB(t)
	_, _, cols := seedWorkspaceBoard(t, db, "ws")
	card := mustCard(t, db, columnWithStage(cols, "todo").ID, "Card")
	svc, f := newService(t, db, &fakeManager{ensureErr: errors.New("forced: ensure")})

	err := svc.OpenCard(context.Background(), card.ID, false)
	if err == nil || !strings.HasPrefix(err.Error(), "board:") || !strings.Contains(err.Error(), "forced: ensure") {
		t.Fatalf("OpenCard err = %v, want board-wrapped ensure failure", err)
	}
	if len(f.attached) != 0 {
		t.Fatalf("Attach calls = %v, want none when Ensure fails", f.attached)
	}
}

func TestCloseCardKills(t *testing.T) {
	db := openBoardDB(t)
	_, _, cols := seedWorkspaceBoard(t, db, "ws")
	card := mustCard(t, db, columnWithStage(cols, "todo").ID, "Card")
	svc, f := newService(t, db, &fakeManager{})

	if err := svc.CloseCard(context.Background(), card.ID); err != nil {
		t.Fatalf("CloseCard: %v", err)
	}
	if len(f.killed) != 1 || f.killed[0].ID != card.ID {
		t.Fatalf("Kill calls = %v, want [%s]", f.killed, card.ID)
	}
}

func TestCloseCardKillErrorWrapped(t *testing.T) {
	db := openBoardDB(t)
	_, _, cols := seedWorkspaceBoard(t, db, "ws")
	card := mustCard(t, db, columnWithStage(cols, "todo").ID, "Card")
	svc, _ := newService(t, db, &fakeManager{killErr: errors.New("forced: kill")})

	err := svc.CloseCard(context.Background(), card.ID)
	if err == nil || !strings.HasPrefix(err.Error(), "board:") || !strings.Contains(err.Error(), "forced: kill") {
		t.Fatalf("CloseCard err = %v, want board-wrapped kill failure", err)
	}
}

func TestSessionStatusPassthrough(t *testing.T) {
	db := openBoardDB(t)
	want := map[string]session.SessionStatus{"card1": {Running: true, Attached: false}}
	svc, f := newService(t, db, &fakeManager{statusRes: want})

	got, err := svc.SessionStatus(context.Background())
	if err != nil {
		t.Fatalf("SessionStatus: %v", err)
	}
	if f.statusN != 1 {
		t.Fatalf("Status calls = %d, want 1", f.statusN)
	}
	if got["card1"] != want["card1"] {
		t.Fatalf("SessionStatus = %v, want %v", got, want)
	}
}

func TestReconcilePassthrough(t *testing.T) {
	db := openBoardDB(t)
	svc, f := newService(t, db, &fakeManager{})

	if err := svc.ReconcileOnStartup(context.Background()); err != nil {
		t.Fatalf("ReconcileOnStartup: %v", err)
	}
	if f.reconcile != 1 {
		t.Fatalf("ReconcileOnStartup calls = %d, want 1", f.reconcile)
	}
}

func TestPassthroughErrorPaths(t *testing.T) {
	db := openBoardDB(t)
	svc, _ := newService(t, db, &fakeManager{})

	getters := []struct {
		name string
		got  func() error
	}{
		{"GetCard", func() error { _, err := svc.GetCard("nope"); return err }},
		{"GetWorkspace", func() error { _, err := svc.GetWorkspace("nope"); return err }},
		{"GetBoard", func() error { _, err := svc.GetBoard("nope"); return err }},
		{"GetColumn", func() error { _, err := svc.GetColumn("nope"); return err }},
		{"GetCodebase", func() error { _, err := svc.GetCodebase("nope"); return err }},
		{"UpdateCard", func() error { _, err := svc.UpdateCard("nope", store.CardUpdate{Title: strPtr("x")}); return err }},
		{"MoveCard", func() error { _, err := svc.MoveCard(context.Background(), "nope", "nope", nil, nil); return err }},
		{"OpenCard", func() error { return svc.OpenCard(context.Background(), "nope", false) }},
		{"CloseCard", func() error { return svc.CloseCard(context.Background(), "nope") }},
	}
	for _, tt := range getters {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.got()
			if !store.IsNotFound(err) {
				t.Fatalf("%s err = %v, want not-found, wrapped board:", tt.name, err)
			}
			if !strings.HasPrefix(err.Error(), "board:") {
				t.Fatalf("%s err %q, want board: prefix", tt.name, err)
			}
		})
	}

	// Delete a real card through the service (DeleteCard has no not-found
	// path: store delete is idempotent), and confirm subsequent reads see it
	// gone — DeleteCard is the one passthrough the CRUD test only exercised
	// via cascade.
	_, _, cols := seedWorkspaceBoard(t, db, "ws")
	card := mustCard(t, db, columnWithStage(cols, "todo").ID, "Card")
	if err := svc.DeleteCard(card.ID); err != nil {
		t.Fatalf("DeleteCard: %v", err)
	}
	if _, err := svc.GetCard(card.ID); !store.IsNotFound(err) {
		t.Fatalf("GetCard after DeleteCard err = %v, want not-found", err)
	}

	// DeleteWorkspace on a nonexistent id still succeeds (store delete is
	// idempotent), covering its error path is N/A; assert no panic.
	if err := svc.DeleteWorkspace("nope"); err != nil {
		t.Fatalf("DeleteWorkspace(nonexistent) err = %v, want nil", err)
	}
}

func strPtr(s string) *string { return &s }

func TestResolveSelectionFromUIState(t *testing.T) {
	db := openBoardDB(t)
	ws, b, _ := seedWorkspaceBoard(t, db, "ws")
	if err := store.SetUIState(db, &ws.ID, &b.ID); err != nil {
		t.Fatalf("SetUIState: %v", err)
	}
	svc, _ := newService(t, db, &fakeManager{})

	gotWS, gotB, err := svc.ResolveSelection()
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}
	if gotWS.ID != ws.ID || gotB.ID != b.ID {
		t.Fatalf("ResolveSelection = (%s, %s), want (%s, %s)", gotWS.ID, gotB.ID, ws.ID, b.ID)
	}
}

func TestResolveSelectionFallbackMostRecent(t *testing.T) {
	db := openBoardDB(t)
	ws1, b1, _ := seedWorkspaceBoard(t, db, "ws1")
	ws2, b2, _ := seedWorkspaceBoard(t, db, "ws2")
	bumpCreatedAt(t, db, ws2.ID)
	_ = ws1
	_ = b1
	svc, _ := newService(t, db, &fakeManager{})

	gotWS, gotB, err := svc.ResolveSelection()
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}
	if gotWS.ID != ws2.ID || gotB.ID != b2.ID {
		t.Fatalf("ResolveSelection = (%s, %s), want most-recent (%s, %s)", gotWS.ID, gotB.ID, ws2.ID, b2.ID)
	}
	_ = ws1
	_ = b1
}

func TestResolveSelectionDeletedSelectionFallsBack(t *testing.T) {
	db := openBoardDB(t)
	ws, _, _ := seedWorkspaceBoard(t, db, "ws")
	if err := store.SetUIState(db, &ws.ID, nil); err != nil {
		t.Fatalf("SetUIState: %v", err)
	}
	if err := store.DeleteWorkspace(db, ws.ID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	ws2, b2, _ := seedWorkspaceBoard(t, db, "ws2")
	svc, _ := newService(t, db, &fakeManager{})

	gotWS, gotB, err := svc.ResolveSelection()
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}
	if gotWS.ID != ws2.ID || gotB.ID != b2.ID {
		t.Fatalf("ResolveSelection = (%s, %s), want (%s, %s)", gotWS.ID, gotB.ID, ws2.ID, b2.ID)
	}
}

func TestResolveSelectionNoWorkspaces(t *testing.T) {
	db := openBoardDB(t)
	svc, _ := newService(t, db, &fakeManager{})

	_, _, err := svc.ResolveSelection()
	if !errors.Is(err, ErrNotInitialized) || !strings.Contains(err.Error(), "run loom init") {
		t.Fatalf("ResolveSelection err = %v, want ErrNotInitialized ('run loom init')", err)
	}
}

func TestResolveSelectionWorkspaceWithoutBoards(t *testing.T) {
	db := openBoardDB(t)
	ws, err := store.CreateWorkspace(db, "ws", t.TempDir())
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	bs, err := store.ListBoards(db, ws.ID)
	if err != nil {
		t.Fatalf("ListBoards: %v", err)
	}
	for _, b := range bs {
		if err := store.DeleteBoard(db, b.ID); err != nil {
			t.Fatalf("DeleteBoard: %v", err)
		}
	}
	svc, _ := newService(t, db, &fakeManager{})

	_, _, err = svc.ResolveSelection()
	if !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("ResolveSelection err = %v, want ErrNotInitialized", err)
	}
}

func TestSwitchWorkspacePersists(t *testing.T) {
	db := openBoardDB(t)
	ws1, _, _ := seedWorkspaceBoard(t, db, "ws1")
	ws2, _, _ := seedWorkspaceBoard(t, db, "ws2")
	svc, _ := newService(t, db, &fakeManager{})

	got, err := svc.SwitchWorkspace(ws2.ID)
	if err != nil {
		t.Fatalf("SwitchWorkspace: %v", err)
	}
	if got.ID != ws2.ID {
		t.Fatalf("SwitchWorkspace returned %s, want %s", got.ID, ws2.ID)
	}
	st, err := store.GetUIState(db)
	if err != nil {
		t.Fatal(err)
	}
	if st.LastWorkspaceID == nil || *st.LastWorkspaceID != ws2.ID {
		t.Fatalf("LastWorkspaceID = %v, want %s", st.LastWorkspaceID, ws2.ID)
	}
	if st.LastBoardID != nil {
		t.Fatalf("LastBoardID = %v, want nil after workspace switch", st.LastBoardID)
	}
	_ = ws1
}

func TestSwitchWorkspaceNotFound(t *testing.T) {
	db := openBoardDB(t)
	svc, _ := newService(t, db, &fakeManager{})

	_, err := svc.SwitchWorkspace("nonexistent")
	if err == nil || !store.IsNotFound(err) {
		t.Fatalf("SwitchWorkspace err = %v, want not-found", err)
	}
}

func TestShowBoardPersists(t *testing.T) {
	db := openBoardDB(t)
	_, b, _ := seedWorkspaceBoard(t, db, "ws")
	svc, _ := newService(t, db, &fakeManager{})

	got, err := svc.ShowBoard(b.ID)
	if err != nil {
		t.Fatalf("ShowBoard: %v", err)
	}
	if got.ID != b.ID {
		t.Fatalf("ShowBoard returned %s, want %s", got.ID, b.ID)
	}
	st, err := store.GetUIState(db)
	if err != nil {
		t.Fatal(err)
	}
	if st.LastWorkspaceID == nil || *st.LastWorkspaceID != b.WorkspaceID {
		t.Fatalf("LastWorkspaceID = %v, want %s", st.LastWorkspaceID, b.WorkspaceID)
	}
	if st.LastBoardID == nil || *st.LastBoardID != b.ID {
		t.Fatalf("LastBoardID = %v, want %s", st.LastBoardID, b.ID)
	}
}

func TestCreateBoardPersists(t *testing.T) {
	db := openBoardDB(t)
	ws, _, _ := seedWorkspaceBoard(t, db, "ws")
	svc, _ := newService(t, db, &fakeManager{})

	b, err := svc.CreateBoard(ws.ID, "Second")
	if err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}
	st, err := store.GetUIState(db)
	if err != nil {
		t.Fatal(err)
	}
	if st.LastWorkspaceID == nil || *st.LastWorkspaceID != ws.ID {
		t.Fatalf("LastWorkspaceID = %v, want %s", st.LastWorkspaceID, ws.ID)
	}
	if st.LastBoardID == nil || *st.LastBoardID != b.ID {
		t.Fatalf("LastBoardID = %v, want %s", st.LastBoardID, b.ID)
	}
}

func TestCreateWorkspaceDoesNotPersist(t *testing.T) {
	db := openBoardDB(t)
	ws, _, _ := seedWorkspaceBoard(t, db, "ws1")
	svc, _ := newService(t, db, &fakeManager{})

	if _, err := svc.CreateWorkspace("ws2", t.TempDir()); err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	st, err := store.GetUIState(db)
	if err != nil {
		t.Fatal(err)
	}
	if st.LastWorkspaceID != nil {
		t.Fatalf("LastWorkspaceID = %v, want nil (create does not persist)", st.LastWorkspaceID)
	}
	_ = ws
}

func TestCRUDPassthroughs(t *testing.T) {
	db := openBoardDB(t)
	svc, _ := newService(t, db, &fakeManager{})

	ws, err := svc.CreateWorkspace("ws", t.TempDir())
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if got, err := svc.GetWorkspace(ws.ID); err != nil || got.ID != ws.ID {
		t.Fatalf("GetWorkspace = (%v, %v), want (%s, nil)", got, err, ws.ID)
	}
	if wsList, err := svc.ListWorkspaces(); err != nil || len(wsList) != 1 {
		t.Fatalf("ListWorkspaces = (%v, %v), want 1", wsList, err)
	}

	b, err := svc.CreateBoard(ws.ID, "Board")
	if err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}
	if got, err := svc.GetBoard(b.ID); err != nil || got.ID != b.ID {
		t.Fatalf("GetBoard = (%v, %v), want (%s, nil)", got, err, b.ID)
	}
	if bs, err := svc.ListBoards(ws.ID); err != nil || len(bs) != 1 {
		t.Fatalf("ListBoards = (%v, %v), want 1", bs, err)
	}

	col, err := svc.CreateColumn(b.ID, "In Progress", "dev")
	if err != nil {
		t.Fatalf("CreateColumn: %v", err)
	}
	if got, err := svc.GetColumn(col.ID); err != nil || got.ID != col.ID {
		t.Fatalf("GetColumn = (%v, %v), want (%s, nil)", got, err, col.ID)
	}
	if cols, err := svc.ListColumns(b.ID); err != nil || len(cols) != 6 {
		t.Fatalf("ListColumns = (%v, %v), want 6", cols, err)
	}

	card, err := svc.CreateCard(store.CardInput{ColumnID: col.ID, Title: "Card"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if got, err := svc.GetCard(card.ID); err != nil || got.ID != card.ID {
		t.Fatalf("GetCard = (%v, %v), want (%s, nil)", got, err, card.ID)
	}
	title := "Renamed"
	updated, err := svc.UpdateCard(card.ID, store.CardUpdate{Title: &title})
	if err != nil || updated.Title != "Renamed" {
		t.Fatalf("UpdateCard = (%v, %v), want renamed", updated, err)
	}
	if cards, err := svc.ListCardsByColumn(col.ID); err != nil || len(cards) != 1 {
		t.Fatalf("ListCardsByColumn = (%v, %v), want 1", cards, err)
	}
	if cards, err := svc.ListCardsByBoard(b.ID); err != nil || len(cards) != 1 {
		t.Fatalf("ListCardsByBoard = (%v, %v), want 1", cards, err)
	}

	cb, err := svc.CreateCodebase(ws.ID, t.TempDir())
	if err != nil {
		t.Fatalf("CreateCodebase: %v", err)
	}
	if got, err := svc.GetCodebase(cb.ID); err != nil || got.ID != cb.ID {
		t.Fatalf("GetCodebase = (%v, %v), want (%s, nil)", got, err, cb.ID)
	}
	if cbs, err := svc.ListCodebases(ws.ID); err != nil || len(cbs) != 1 {
		t.Fatalf("ListCodebases = (%v, %v), want 1", cbs, err)
	}

	if err := svc.DeleteCodebase(cb.ID); err != nil {
		t.Fatalf("DeleteCodebase: %v", err)
	}
	if _, err := svc.GetCodebase(cb.ID); !store.IsNotFound(err) {
		t.Fatalf("GetCodebase after delete err = %v, want not-found", err)
	}
	if err := svc.DeleteColumn(col.ID); err != nil {
		t.Fatalf("DeleteColumn: %v", err)
	}
	if err := svc.DeleteBoard(b.ID); err != nil {
		t.Fatalf("DeleteBoard: %v", err)
	}
	if err := svc.DeleteWorkspace(ws.ID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	if _, err := svc.GetCard(card.ID); !store.IsNotFound(err) {
		t.Fatalf("GetCard after cascade err = %v, want not-found", err)
	}
}
