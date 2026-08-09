package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ErrCrossBoardMove rejects moving a card to a column whose board differs
// from the column it currently lives in. v0.1 has no cross-board move
// (ADR-001 §3.3).
var ErrCrossBoardMove = errors.New("store: card move across boards")

// Card mirrors the cards row. Agent is *string because NULL carries meaning:
// "follow [agent] default at launch time", resolved by AgentOrDefault rather
// than at write time (DESIGN-002 §6).
type Card struct {
	ID                 string
	ColumnID           string
	BoardID            string
	WorkspaceID        string
	CodebaseID         *string
	Title              string
	Description        *string
	Objective          *string
	AcceptanceCriteria *string
	Priority           string
	Labels             *string
	Agent              *string
	Position           int
	CreatedAt          string
	UpdatedAt          string
}

// AgentOrDefault resolves the launch agent: an explicit card value wins,
// otherwise the config default is used (DESIGN-002 §6).
func (c Card) AgentOrDefault(def string) string {
	if c.Agent != nil && *c.Agent != "" {
		return *c.Agent
	}
	return def
}

const cardColumns = "id, column_id, board_id, workspace_id, codebase_id, title, description, objective, acceptance_criteria, priority, labels, agent, position, created_at, updated_at"

// CardInput carries the mutable fields for a new card.
type CardInput struct {
	ColumnID           string
	Title              string
	Description        *string
	Objective          *string
	AcceptanceCriteria *string
	Priority           string
	Labels             *string
	CodebaseID         *string
	Agent              *string
}

// CreateCard inserts a card at max(position)+1000 within its column
// (ADR-001 §3.4), denormalizing board_id/workspace_id off the column's
// board. One transaction so the position pickup and insert are atomic.
// Priority defaults to "medium" when empty, matching the schema default.
func CreateCard(db *sql.DB, in CardInput) (Card, error) {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return Card{}, err
	}
	defer tx.Rollback()

	card, err := createCard(tx, in)
	if err != nil {
		return Card{}, err
	}
	if err := tx.Commit(); err != nil {
		return Card{}, err
	}
	return GetCard(db, card.ID)
}

func createCard(e execer, in CardInput) (Card, error) {
	var boardID, workspaceID string
	if err := e.QueryRowContext(context.Background(),
		"SELECT b.id, w.id FROM columns c JOIN boards b ON b.id = c.board_id JOIN workspaces w ON w.id = b.workspace_id WHERE c.id = ?",
		in.ColumnID,
	).Scan(&boardID, &workspaceID); err != nil {
		return Card{}, err
	}

	var pos int
	if err := e.QueryRowContext(context.Background(),
		"SELECT COALESCE(MAX(position), 0) FROM cards WHERE column_id = ?", in.ColumnID,
	).Scan(&pos); err != nil {
		return Card{}, err
	}

	priority := in.Priority
	if priority == "" {
		priority = "medium"
	}

	c := Card{
		ID:                 NewID(),
		ColumnID:           in.ColumnID,
		BoardID:            boardID,
		WorkspaceID:        workspaceID,
		CodebaseID:         in.CodebaseID,
		Title:              in.Title,
		Description:        in.Description,
		Objective:          in.Objective,
		AcceptanceCriteria: in.AcceptanceCriteria,
		Priority:           priority,
		Labels:             in.Labels,
		Agent:              in.Agent,
		Position:           pos + 1000,
	}
	_, err := e.ExecContext(context.Background(),
		"INSERT INTO cards (id, column_id, board_id, workspace_id, codebase_id, title, description, objective, acceptance_criteria, priority, labels, agent, position) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		c.ID, c.ColumnID, c.BoardID, c.WorkspaceID, ptrToNull(c.CodebaseID), c.Title,
		ptrToNull(c.Description), ptrToNull(c.Objective), ptrToNull(c.AcceptanceCriteria),
		c.Priority, ptrToNull(c.Labels), ptrToNull(c.Agent), c.Position,
	)
	return c, err
}

// CardUpdate carries fields to change. A nil pointer leaves the column
// untouched; a non-nil pointer sets it. For nullable columns, a non-nil
// pointer to "" clears the column to NULL — how the CLI expresses `--agent=`
// reset (DESIGN-002 §13). column_id is intentionally absent: MoveCard is the
// only writer of column_id and keeps board_id/workspace_id in sync, so a
// column change must go through MoveCard (ADR-001 §3.3).
type CardUpdate struct {
	CodebaseID         *string
	Title              *string
	Description        *string
	Objective          *string
	AcceptanceCriteria *string
	Priority           *string
	Labels             *string
	Agent              *string
}

// UpdateCard applies the non-nil CardUpdate fields in one statement and
// returns the refreshed row.
func UpdateCard(db *sql.DB, id string, u CardUpdate) (Card, error) {
	var sets []string
	var args []any
	add := func(col string, v *string, nullable bool) {
		if v == nil {
			return
		}
		sets = append(sets, col+" = ?")
		if nullable && *v == "" {
			args = append(args, nil)
		} else {
			args = append(args, *v)
		}
	}
	add("codebase_id", u.CodebaseID, true)
	add("title", u.Title, false)
	add("description", u.Description, true)
	add("objective", u.Objective, true)
	add("acceptance_criteria", u.AcceptanceCriteria, true)
	add("priority", u.Priority, false)
	add("labels", u.Labels, true)
	add("agent", u.Agent, true)

	if len(sets) == 0 {
		return GetCard(db, id)
	}
	sets = append(sets, "updated_at = strftime('%Y-%m-%dT%H:%M:%f','now')")
	args = append(args, id)

	res, err := db.Exec("UPDATE cards SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return Card{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return Card{}, err
	} else if n == 0 {
		return Card{}, sql.ErrNoRows
	}
	return GetCard(db, id)
}

func GetCard(db *sql.DB, id string) (Card, error) {
	row := db.QueryRow("SELECT "+cardColumns+" FROM cards WHERE id = ?", id)
	c, err := scanCard(row)
	if err != nil {
		return Card{}, err
	}
	return c, nil
}

func ListCardsByBoard(db *sql.DB, boardID string) ([]Card, error) {
	rows, err := db.Query("SELECT "+cardColumns+" FROM cards WHERE board_id = ? ORDER BY position", boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cs []Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		cs = append(cs, c)
	}
	return cs, rows.Err()
}

func ListCardsByColumn(db *sql.DB, columnID string) ([]Card, error) {
	rows, err := db.Query("SELECT "+cardColumns+" FROM cards WHERE column_id = ? ORDER BY position", columnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cs []Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		cs = append(cs, c)
	}
	return cs, rows.Err()
}

func DeleteCard(db *sql.DB, id string) error {
	_, err := db.Exec("DELETE FROM cards WHERE id = ?", id)
	return err
}

// MoveCard repositions a card. Passed (nil, nil) it appends at
// max(position)+1000; with beforeID/afterID it lands the card between two
// neighbours at (prev+next)/2 (ADR-001 §3.4). When the target gap is
// exhausted (next-prev <= 1) the whole column is renumbered to 0,1000,2000,…
// in display order before the move is applied — one transaction, so no reader
// sees the intermediate renumbering. column_id/board_id/workspace_id are all
// written (the store is the only writer of column_id and keeps the
// denormalized ids in sync). Moving to a column on a different board returns
// ErrCrossBoardMove.
func MoveCard(db *sql.DB, cardID, toColumnID string, beforeID, afterID *string) error {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	card, err := getCard(tx, cardID)
	if err != nil {
		return err
	}

	var boardID, workspaceID string
	if err := tx.QueryRowContext(context.Background(),
		"SELECT b.id, w.id FROM columns c JOIN boards b ON b.id = c.board_id JOIN workspaces w ON w.id = b.workspace_id WHERE c.id = ?",
		toColumnID,
	).Scan(&boardID, &workspaceID); err != nil {
		return err
	}
	if card.BoardID != boardID {
		return ErrCrossBoardMove
	}

	pos, err := movePosition(tx, toColumnID, beforeID, afterID)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(context.Background(),
		"UPDATE cards SET column_id = ?, board_id = ?, workspace_id = ?, position = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%f','now') WHERE id = ?",
		toColumnID, boardID, workspaceID, pos, cardID,
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ErrPartialAnchors rejects a move with exactly one anchor: the midpoint is
// only well-defined between two neighbours, and an unbounded side can't be
// expressed as a position. Callers use (nil, nil) to append (§3.4).
var ErrPartialAnchors = errors.New("store: card move needs both anchors or none")

// movePosition computes the destination position within a column. Append mode
// (both offsets nil) uses max+1000. Anchored mode reads the neighbour
// positions and, when the gap is exhausted, renumbers the whole column before
// recomputing the midpoint.
func movePosition(e execer, toColumnID string, beforeID, afterID *string) (int, error) {
	if beforeID == nil && afterID == nil {
		var max int
		if err := e.QueryRowContext(context.Background(),
			"SELECT COALESCE(MAX(position), 0) FROM cards WHERE column_id = ?", toColumnID,
		).Scan(&max); err != nil {
			return 0, err
		}
		return max + 1000, nil
	}
	if beforeID == nil || afterID == nil {
		return 0, ErrPartialAnchors
	}

	prev, next, err := anchorPositions(e, toColumnID, beforeID, afterID)
	if err != nil {
		return 0, err
	}
	if next-prev <= 1 {
		if err := renumberColumn(e, toColumnID); err != nil {
			return 0, err
		}
		prev, next, err = anchorPositions(e, toColumnID, beforeID, afterID)
		if err != nil {
			return 0, err
		}
	}
	return (prev + next) / 2, nil
}

// anchorPositions reads the positions of the two neighbour cards, scoped to
// the target column so an anchor from another column (or a stale id) is an
// error rather than a silent misplaced midpoint.
func anchorPositions(e execer, toColumnID string, beforeID, afterID *string) (int, int, error) {
	prev, err := anchorPosition(e, toColumnID, beforeID)
	if err != nil {
		return 0, 0, err
	}
	next, err := anchorPosition(e, toColumnID, afterID)
	if err != nil {
		return 0, 0, err
	}
	return prev, next, nil
}

func anchorPosition(e execer, toColumnID string, id *string) (int, error) {
	var pos int
	if err := e.QueryRowContext(context.Background(),
		"SELECT position FROM cards WHERE id = ? AND column_id = ?", *id, toColumnID,
	).Scan(&pos); err != nil {
		return 0, err
	}
	return pos, nil
}

// renumberColumn rewrites the whole column to 0,1000,2000,… in display order.
// It is only reached after the gap check, so every card sits on a distinct
// position afterwards and the pending midpoint is guaranteed available.
func renumberColumn(e execer, columnID string) error {
	rows, err := e.QueryContext(context.Background(),
		"SELECT id FROM cards WHERE column_id = ? ORDER BY position, created_at",
		columnID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := e.ExecContext(context.Background(),
			"UPDATE cards SET position = ? WHERE id = ?", i*1000, id,
		); err != nil {
			return err
		}
	}
	return nil
}

func getCard(e execer, id string) (Card, error) {
	return scanCard(e.QueryRowContext(context.Background(),
		"SELECT "+cardColumns+" FROM cards WHERE id = ?", id))
}

func scanCard(row rowScanner) (Card, error) {
	var c Card
	var codebaseID, desc, obj, ac, labels, agent sql.NullString
	err := row.Scan(
		&c.ID, &c.ColumnID, &c.BoardID, &c.WorkspaceID, &codebaseID, &c.Title,
		&desc, &obj, &ac, &c.Priority, &labels, &agent, &c.Position,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return Card{}, err
	}
	c.CodebaseID = nullToPtr(codebaseID)
	c.Description = nullToPtr(desc)
	c.Objective = nullToPtr(obj)
	c.AcceptanceCriteria = nullToPtr(ac)
	c.Labels = nullToPtr(labels)
	c.Agent = nullToPtr(agent)
	return c, nil
}
