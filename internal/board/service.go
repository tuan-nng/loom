// Package board provides BoardService, the application-core orchestration
// seam both the CLI and TUI consume (DESIGN-002 §4.2). It composes store CRUD
// with the session manager, applies the done-stage auto-kill rule on move
// (ADR-001 §4.1 step 4), and owns the current workspace/board selection
// fallback chain (ADR-001 §6).
package board

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"loom/internal/session"
	"loom/internal/store"
)

// ErrNotInitialized is returned by selection resolution when no workspace (or
// no board) exists. The message is part of the contract: the CLI prints it as
// the actionable next step (ADR-001 §6).
var ErrNotInitialized = errors.New("run loom init")

// sessionManager is the session surface BoardService delegates to, satisfied
// by *session.Manager. The seam exists so the done-kill rule and open/close
// round-trip are unit-testable without a tmux server (mirroring the
// runRecorder seam in internal/session).
type sessionManager interface {
	Ensure(ctx context.Context, c store.Card) error
	Attach(ctx context.Context, c store.Card) error
	Kill(ctx context.Context, c store.Card) error
	Status(ctx context.Context) (map[string]session.SessionStatus, error)
	ReconcileOnStartup(ctx context.Context) error
}

// Service composes the store and the session manager into the operations the
// UI surfaces call. It carries no *config.Config: the manager resolves the
// card's agent and watch root internally (session.Manager.cardForAgent/
// watchRoot), so selection and launch defaults need no config here.
type Service struct {
	db   *sql.DB
	sess sessionManager
}

// NewService is the one constructor; production passes a *session.Manager,
// tests pass a fake sessionManager.
func NewService(db *sql.DB, sess sessionManager) *Service {
	return &Service{db: db, sess: sess}
}

// CreateCard creates a card in the given column, resolving board/workspace
// and position from the store (ADR-001 §3.4).
func (s *Service) CreateCard(in store.CardInput) (store.Card, error) {
	c, err := store.CreateCard(s.db, in)
	if err != nil {
		return store.Card{}, fmt.Errorf("board: %w", err)
	}
	return c, nil
}

// UpdateCard applies a CardUpdate. A non-nil pointer to "" clears a nullable
// field to NULL (how the CLI expresses `--agent=` reset, DESIGN-002 §13).
func (s *Service) UpdateCard(id string, u store.CardUpdate) (store.Card, error) {
	c, err := store.UpdateCard(s.db, id, u)
	if err != nil {
		return store.Card{}, fmt.Errorf("board: %w", err)
	}
	return c, nil
}

func (s *Service) GetCard(id string) (store.Card, error) {
	c, err := store.GetCard(s.db, id)
	if err != nil {
		return store.Card{}, fmt.Errorf("board: %w", err)
	}
	return c, nil
}

func (s *Service) ListCardsByBoard(boardID string) ([]store.Card, error) {
	cs, err := store.ListCardsByBoard(s.db, boardID)
	if err != nil {
		return nil, fmt.Errorf("board: %w", err)
	}
	return cs, nil
}

func (s *Service) ListCardsByColumn(columnID string) ([]store.Card, error) {
	cs, err := store.ListCardsByColumn(s.db, columnID)
	if err != nil {
		return nil, fmt.Errorf("board: %w", err)
	}
	return cs, nil
}

// DeleteCard removes the card. It does not kill a running session: trace rows
// cascade away with the card, and the tmux session itself keeps running until
// the agent exits (ADR-001 §8 orphan mitigation) — delete must work without
// a session manager.
func (s *Service) DeleteCard(id string) error {
	if err := store.DeleteCard(s.db, id); err != nil {
		return fmt.Errorf("board: %w", err)
	}
	return nil
}

// MoveCard repositions a card via store.MoveCard (passing (nil, nil) appends,
// ADR-001 §3.4), then applies the done-stage rule: when the target column's
// stage is "done", the card's session is killed and its run finalized
// (ADR-001 §4.1 step 4). The kill is conditional on the move committing, so a
// rejected move (cross-board, partial anchors) never touches the session; a
// kill failure is returned loudly because "mark done" and "stop the agent"
// must not silently diverge — the move is committed, so retrying the move
// (now a no-op) re-runs the idempotent kill.
func (s *Service) MoveCard(ctx context.Context, cardID, toColumnID string, beforeID, afterID *string) (store.Card, error) {
	card, err := store.GetCard(s.db, cardID)
	if err != nil {
		return store.Card{}, fmt.Errorf("board: %w", err)
	}
	if err := store.MoveCard(s.db, cardID, toColumnID, beforeID, afterID); err != nil {
		return store.Card{}, fmt.Errorf("board: %w", err)
	}
	col, err := store.GetColumn(s.db, toColumnID)
	if err != nil {
		return store.Card{}, fmt.Errorf("board: %w", err)
	}
	if col.Stage == "done" {
		if err := s.sess.Kill(ctx, card); err != nil {
			return store.Card{}, fmt.Errorf("board: move to done: %w", err)
		}
	}
	c, err := store.GetCard(s.db, cardID)
	if err != nil {
		return store.Card{}, fmt.Errorf("board: %w", err)
	}
	return c, nil
}

// SwitchWorkspace persists the selection {last_workspace_id: id,
// last_board_id: nil}. The board selection is reset, not resolved: the old
// board belongs to another workspace and persisting a board the user never
// picked would be a hidden write — resolution lazily falls back to FirstBoard
// (ADR-001 §6).
func (s *Service) SwitchWorkspace(workspaceID string) (store.Workspace, error) {
	ws, err := store.GetWorkspace(s.db, workspaceID)
	if err != nil {
		return store.Workspace{}, fmt.Errorf("board: %w", err)
	}
	if err := store.SetUIState(s.db, &ws.ID, nil); err != nil {
		return store.Workspace{}, fmt.Errorf("board: %w", err)
	}
	return ws, nil
}

// ShowBoard persists the selection {workspace_id: b.WorkspaceID,
// board_id: b.ID} — "board show" records both (ADR-001 §6). The board's own
// workspace id is authoritative.
func (s *Service) ShowBoard(boardID string) (store.Board, error) {
	b, err := store.GetBoard(s.db, boardID)
	if err != nil {
		return store.Board{}, fmt.Errorf("board: %w", err)
	}
	if err := store.SetUIState(s.db, &b.WorkspaceID, &b.ID); err != nil {
		return store.Board{}, fmt.Errorf("board: %w", err)
	}
	return b, nil
}

// CreateBoard creates a board (seeding its five default columns, ADR-001 §6)
// and persists the selection {workspace_id, new board id}.
func (s *Service) CreateBoard(workspaceID, name string) (store.Board, error) {
	b, err := store.CreateBoard(s.db, workspaceID, name)
	if err != nil {
		return store.Board{}, fmt.Errorf("board: %w", err)
	}
	if err := store.SetUIState(s.db, &workspaceID, &b.ID); err != nil {
		return store.Board{}, fmt.Errorf("board: %w", err)
	}
	return b, nil
}

func (s *Service) ListWorkspaces() ([]store.Workspace, error) {
	ws, err := store.ListWorkspaces(s.db)
	if err != nil {
		return nil, fmt.Errorf("board: %w", err)
	}
	return ws, nil
}

func (s *Service) CreateWorkspace(name, rootPath string) (store.Workspace, error) {
	w, err := store.CreateWorkspace(s.db, name, rootPath)
	if err != nil {
		return store.Workspace{}, fmt.Errorf("board: %w", err)
	}
	return w, nil
}

func (s *Service) GetWorkspace(id string) (store.Workspace, error) {
	w, err := store.GetWorkspace(s.db, id)
	if err != nil {
		return store.Workspace{}, fmt.Errorf("board: %w", err)
	}
	return w, nil
}

// DeleteWorkspace removes the workspace; the ui_state selection is cleared by
// ON DELETE SET NULL, which re-arms the fallback chain on next resolution.
func (s *Service) DeleteWorkspace(id string) error {
	if err := store.DeleteWorkspace(s.db, id); err != nil {
		return fmt.Errorf("board: %w", err)
	}
	return nil
}

func (s *Service) ListBoards(workspaceID string) ([]store.Board, error) {
	bs, err := store.ListBoards(s.db, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("board: %w", err)
	}
	return bs, nil
}

func (s *Service) GetBoard(id string) (store.Board, error) {
	b, err := store.GetBoard(s.db, id)
	if err != nil {
		return store.Board{}, fmt.Errorf("board: %w", err)
	}
	return b, nil
}

func (s *Service) DeleteBoard(id string) error {
	if err := store.DeleteBoard(s.db, id); err != nil {
		return fmt.Errorf("board: %w", err)
	}
	return nil
}

func (s *Service) CreateColumn(boardID, name, stage string) (store.Column, error) {
	c, err := store.CreateColumn(s.db, boardID, name, stage)
	if err != nil {
		return store.Column{}, fmt.Errorf("board: %w", err)
	}
	return c, nil
}

func (s *Service) ListColumns(boardID string) ([]store.Column, error) {
	cs, err := store.ListColumns(s.db, boardID)
	if err != nil {
		return nil, fmt.Errorf("board: %w", err)
	}
	return cs, nil
}

func (s *Service) GetColumn(id string) (store.Column, error) {
	c, err := store.GetColumn(s.db, id)
	if err != nil {
		return store.Column{}, fmt.Errorf("board: %w", err)
	}
	return c, nil
}

func (s *Service) DeleteColumn(id string) error {
	if err := store.DeleteColumn(s.db, id); err != nil {
		return fmt.Errorf("board: %w", err)
	}
	return nil
}

func (s *Service) CreateCodebase(workspaceID, path string) (store.Codebase, error) {
	cb, err := store.CreateCodebase(s.db, workspaceID, path)
	if err != nil {
		return store.Codebase{}, fmt.Errorf("board: %w", err)
	}
	return cb, nil
}

func (s *Service) ListCodebases(workspaceID string) ([]store.Codebase, error) {
	cbs, err := store.ListCodebases(s.db, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("board: %w", err)
	}
	return cbs, nil
}

func (s *Service) GetCodebase(id string) (store.Codebase, error) {
	cb, err := store.GetCodebase(s.db, id)
	if err != nil {
		return store.Codebase{}, fmt.Errorf("board: %w", err)
	}
	return cb, nil
}

func (s *Service) DeleteCodebase(id string) error {
	if err := store.DeleteCodebase(s.db, id); err != nil {
		return fmt.Errorf("board: %w", err)
	}
	return nil
}

// OpenCard creates-or-reuses the card's session (sess.Ensure, which records
// trace_start only after the startup probe) and, unless detach is set, hands
// the terminal to it (sess.Attach). No config here: the manager resolves the
// agent and watch root internally (DESIGN-002 §10.2).
func (s *Service) OpenCard(ctx context.Context, cardID string, detach bool) error {
	card, err := store.GetCard(s.db, cardID)
	if err != nil {
		return fmt.Errorf("board: %w", err)
	}
	if err := s.sess.Ensure(ctx, card); err != nil {
		return fmt.Errorf("board: %w", err)
	}
	if !detach {
		if err := s.sess.Attach(ctx, card); err != nil {
			return fmt.Errorf("board: %w", err)
		}
	}
	return nil
}

// CloseCard kills the card's session and finalizes its run's trace
// (ADR-001 §4.1 step 4, non-interactive).
func (s *Service) CloseCard(ctx context.Context, cardID string) error {
	card, err := store.GetCard(s.db, cardID)
	if err != nil {
		return fmt.Errorf("board: %w", err)
	}
	if err := s.sess.Kill(ctx, card); err != nil {
		return fmt.Errorf("board: %w", err)
	}
	return nil
}

// SessionStatus returns the per-card session state (● running / ◉ attached);
// one synchronous tick, the caller owns the cadence (TUI 2s poll, one-shot
// CLI `loom sessions`).
func (s *Service) SessionStatus(ctx context.Context) (map[string]session.SessionStatus, error) {
	return s.sess.Status(ctx)
}

// ReconcileOnStartup finalizes open runs whose session is absent — the
// startup backstop for runs that ended while no loom process was watching
// (ADR-001 §4.1 step 5).
func (s *Service) ReconcileOnStartup(ctx context.Context) error {
	return s.sess.ReconcileOnStartup(ctx)
}

// ResolveSelection returns the current workspace and board via the ADR-001 §6
// fallback chain: ui_state selections when valid (a deleted selection degrades
// via ON DELETE SET NULL), else the most-recently-created workspace and its
// first board, and ErrNotInitialized ("run loom init") when none exists. Full
// structs are returned — every caller needs RootPath/names and they are
// already fetched.
func (s *Service) ResolveSelection() (store.Workspace, store.Board, error) {
	st, err := store.GetUIState(s.db)
	if err != nil {
		return store.Workspace{}, store.Board{}, fmt.Errorf("board: %w", err)
	}
	ws, err := s.resolveWorkspace(st)
	if err != nil {
		return store.Workspace{}, store.Board{}, err
	}
	b, err := s.resolveBoard(st, ws)
	if err != nil {
		return store.Workspace{}, store.Board{}, err
	}
	return ws, b, nil
}

func (s *Service) resolveWorkspace(st store.UIState) (store.Workspace, error) {
	if st.LastWorkspaceID != nil {
		ws, err := store.GetWorkspace(s.db, *st.LastWorkspaceID)
		if err == nil {
			return ws, nil
		}
		if !store.IsNotFound(err) {
			return store.Workspace{}, fmt.Errorf("board: %w", err)
		}
	}
	ws, err := store.MostRecentWorkspace(s.db)
	if store.IsNotFound(err) {
		return store.Workspace{}, ErrNotInitialized
	}
	if err != nil {
		return store.Workspace{}, fmt.Errorf("board: %w", err)
	}
	return ws, nil
}

// resolveBoard prefers the ui_state board when it still exists and belongs to
// the resolved workspace (guards hand-edited state; SwitchWorkspace already
// clears it), else falls back to the workspace's first board.
func (s *Service) resolveBoard(st store.UIState, ws store.Workspace) (store.Board, error) {
	if st.LastBoardID != nil {
		b, err := store.GetBoard(s.db, *st.LastBoardID)
		if err == nil && b.WorkspaceID == ws.ID {
			return b, nil
		}
		if err != nil && !store.IsNotFound(err) {
			return store.Board{}, fmt.Errorf("board: %w", err)
		}
	}
	b, err := store.FirstBoard(s.db, ws.ID)
	if store.IsNotFound(err) {
		return store.Board{}, ErrNotInitialized
	}
	if err != nil {
		return store.Board{}, fmt.Errorf("board: %w", err)
	}
	return b, nil
}
