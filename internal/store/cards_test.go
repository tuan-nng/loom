package store

import (
	"database/sql"
	"errors"
	"testing"
)

// newCardTree opens a store and creates a workspace with a board and two
// columns: dev (stage dev) and done (stage done).
func newCardTree(t *testing.T) (*sql.DB, Workspace, Board, Column, Column) {
	t.Helper()
	db := openTest(t)
	w, err := CreateWorkspace(db, "w", "/tmp/w")
	if err != nil {
		t.Fatal(err)
	}
	b, err := CreateBoard(db, w.ID, "Board")
	if err != nil {
		t.Fatal(err)
	}
	cols, err := ListColumns(db, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	return db, w, b, cols[1], cols[4]
}

// mustCard binds t into a closure so a two-value CreateCard call can be
// spread across the final two parameters (Go only spreads a multi-valued call
// when it supplies every remaining argument).
func mustCard(t *testing.T) func(Card, error) Card {
	t.Helper()
	return func(c Card, err error) Card {
		if err != nil {
			t.Fatalf("card op: %v", err)
		}
		return c
	}
}

func strp(s string) *string { return &s }

func TestCreateCardAppendsPosition(t *testing.T) {
	db, w, b, dev, _ := newCardTree(t)

	first, err := CreateCard(db, CardInput{ColumnID: dev.ID, Title: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateCard(db, CardInput{ColumnID: dev.ID, Title: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Position != 1000 || second.Position != 2000 {
		t.Errorf("positions = %d, %d; want 1000, 2000", first.Position, second.Position)
	}

	var codebase, desc sql.NullString
	if err := db.QueryRow(
		"SELECT codebase_id, description FROM cards WHERE id = ?", first.ID,
	).Scan(&codebase, &desc); err != nil {
		t.Fatal(err)
	}
	if codebase.Valid || desc.Valid {
		t.Errorf("default codebase_id/description = %#v/%#v, want NULL", codebase, desc)
	}
	var priority string
	if err := db.QueryRow("SELECT priority FROM cards WHERE id = ?", first.ID).Scan(&priority); err != nil {
		t.Fatal(err)
	}
	if priority != "medium" {
		t.Errorf("default priority = %q, want 'medium'", priority)
	}
	if first.BoardID != b.ID {
		t.Errorf("BoardID = %q, want %q", first.BoardID, b.ID)
	}
	if first.WorkspaceID != w.ID {
		t.Errorf("WorkspaceID = %q, want %q", first.WorkspaceID, w.ID)
	}
}

func TestCreateCardAgentNullAndExplicitRoundTrip(t *testing.T) {
	db, _, _, dev, _ := newCardTree(t)

	def, err := CreateCard(db, CardInput{ColumnID: dev.ID, Title: "def"})
	if err != nil {
		t.Fatal(err)
	}
	if def.Agent != nil {
		t.Errorf("Agent = %#v, want nil", def.Agent)
	}
	if def.AgentOrDefault("claude") != "claude" {
		t.Errorf("AgentOrDefault(NULL) = %q, want 'claude'", def.AgentOrDefault("claude"))
	}

	exp := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "exp", Agent: strp("opencode")}))
	if exp.Agent == nil || *exp.Agent != "opencode" {
		t.Errorf("Agent = %#v, want ptr 'opencode'", exp.Agent)
	}
	if exp.AgentOrDefault("claude") != "opencode" {
		t.Errorf("AgentOrDefault(explicit) = %q, want 'opencode'", exp.AgentOrDefault("claude"))
	}
}

func TestCreateCardRejectsBogusAgentPriority(t *testing.T) {
	db, _, _, dev, _ := newCardTree(t)

	if _, err := CreateCard(db, CardInput{ColumnID: dev.ID, Title: "bad", Agent: strp("bogus")}); err == nil {
		t.Error("agent='bogus': create succeeded, want CHECK error")
	}
	if _, err := CreateCard(db, CardInput{ColumnID: dev.ID, Title: "bad", Priority: "urgent"}); err == nil {
		t.Error("priority='urgent': create succeeded, want CHECK error")
	}
}

func TestUpdateCardPartialSetAndClear(t *testing.T) {
	db, _, _, dev, _ := newCardTree(t)
	c := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "t"}))

	u, err := UpdateCard(db, c.ID, CardUpdate{Title: strp("renamed")})
	if err != nil {
		t.Fatal(err)
	}
	if u.Title != "renamed" {
		t.Errorf("Title = %q, want 'renamed'", u.Title)
	}

	u, err = UpdateCard(db, c.ID, CardUpdate{Agent: strp("claude")})
	if err != nil {
		t.Fatal(err)
	}
	if u.Agent == nil || *u.Agent != "claude" {
		t.Errorf("Agent = %#v, want 'claude'", u.Agent)
	}

	// Partial update must not disturb untargeted columns.
	u, err = UpdateCard(db, c.ID, CardUpdate{Agent: strp("")})
	if err != nil {
		t.Fatal(err)
	}
	if u.Agent != nil {
		t.Errorf("Agent after clear = %#v, want nil", u.Agent)
	}
	if u.Title != "renamed" {
		t.Errorf("Title after clear = %q, want 'renamed' untouched", u.Title)
	}
}

func TestUpdateCardNoopReturnsRow(t *testing.T) {
	db, _, _, dev, _ := newCardTree(t)
	c := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "t"}))
	u, err := UpdateCard(db, c.ID, CardUpdate{})
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != c.ID {
		t.Errorf("noop update returned %q, want %q", u.ID, c.ID)
	}
}

func TestUpdateCardNotFound(t *testing.T) {
	db := openTest(t)
	if _, err := UpdateCard(db, "missing", CardUpdate{}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("UpdateCard missing = %v, want sql.ErrNoRows", err)
	}
}

func TestListCardsByBoardAndColumn(t *testing.T) {
	db, _, b, dev, done := newCardTree(t)
	CreateCard(db, CardInput{ColumnID: dev.ID, Title: "dev1"})
	CreateCard(db, CardInput{ColumnID: dev.ID, Title: "dev2"})
	CreateCard(db, CardInput{ColumnID: done.ID, Title: "done1"})

	byBoard, err := ListCardsByBoard(db, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(byBoard) != 3 {
		t.Fatalf("ListCardsByBoard len = %d, want 3 (board-scoped)", len(byBoard))
	}

	byDev, err := ListCardsByColumn(db, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(byDev) != 2 || byDev[0].Title != "dev1" || byDev[1].Title != "dev2" {
		t.Errorf("ListCardsByColumn = %+v, want [dev1 dev2] ordered by position", byDev)
	}
}

func TestMoveCardAppendToColumnEnd(t *testing.T) {
	db, _, _, dev, done := newCardTree(t)
	c := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "t"}))

	if err := MoveCard(db, c.ID, done.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	got, err := GetCard(db, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ColumnID != done.ID {
		t.Errorf("ColumnID = %q, want %q", got.ColumnID, done.ID)
	}
	if got.BoardID != c.BoardID {
		t.Errorf("BoardID = %q, want %q (kept in sync)", got.BoardID, c.BoardID)
	}
	if got.Position != 1000 {
		t.Errorf("append Position = %d, want 1000", got.Position)
	}
}

func TestMoveCardMidpointBetweenAnchors(t *testing.T) {
	db, _, _, dev, _ := newCardTree(t)
	a := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "a"}))
	b := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "b"}))
	c := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "c"}))
	// a=1000, b=2000, c=3000 → move c between a and b lands at 1500.
	if err := MoveCard(db, c.ID, dev.ID, strp(a.ID), strp(b.ID)); err != nil {
		t.Fatal(err)
	}
	got := GetCardOrFatal(t, db, c.ID)
	if got.Position != 1500 {
		t.Errorf("midpoint Position = %d, want 1500", got.Position)
	}
	if got.ColumnID != dev.ID {
		t.Errorf("ColumnID = %q, want %q", got.ColumnID, dev.ID)
	}
}

// TestMoveCardRebalanceBeforeWrite drives the §3.4 rebalance through normal
// use: each iteration pins a new card just below b, halving the a→b gap.
// After ~10 halvings next-prev reaches 1 and the pre-write renumber ratio
// fires — without it the very next midpoint would collide. Every intermediate
// write must leave the column strictly ordered and collision-free, which is
// the observable form of "rebalance before the write" (§10).
func TestMoveCardRebalanceBeforeWrite(t *testing.T) {
	db, _, _, dev, _ := newCardTree(t)
	a := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "a"}))
	b := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "b"}))

	// 40 halvings far exceed the ~10 needed to exhaust a 1000-step gap. The
	// pinned card below b forces the renumber path repeatedly.
	pinnedID := a.ID
	for i := 0; i < 40; i++ {
		m := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "m"}))
		if err := MoveCard(db, m.ID, dev.ID, strp(pinnedID), strp(b.ID)); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		assertColumnPositions(t, db, dev.ID)
		pinnedID = m.ID
	}

	assertNoDuplicatePositions(t, db, dev.ID)
	// 40 halvings with no renumber would collide by ~10 (midpoint equals the
	// pinned neighbour) and fail assertColumnPositions above; reaching 40
	// with strictly ordered, collision-free positions is the observable form
	// of the pre-write renumber.
	cols := ListByColumnOrFatal(t, db, dev.ID)
	if len(cols) != 42 {
		t.Fatalf("column has %d cards, want 42", len(cols))
	}
	if cols[0].ID != a.ID || cols[len(cols)-1].ID != b.ID {
		t.Errorf("display order not preserved: first=%s last=%s, want a…b", cols[0].ID, cols[len(cols)-1].ID)
	}
}

// TestMoveCardRenumberDeterministic asserts the exact post-renumber state,
// exercised on a pre-built exhausted gap: with the two anchor cards pinned to
// next-prev == 1, the pending move renumbers the column to 0,1000,2000,… and
// lands the mover at the fresh midpoint.
func TestMoveCardRenumberDeterministic(t *testing.T) {
	db, _, _, dev, _ := newCardTree(t)
	a := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "a"}))
	b := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "b"}))
	m := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "m"}))
	// Pin the exhausted gap directly: a at 0, b at 1 (next-prev == 1 burns the
	// final position; the midpoint (0+1)/2 == 0 would collide with a).
	if _, err := db.Exec("UPDATE cards SET position = 0 WHERE id = ?", a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE cards SET position = 1 WHERE id = ?", b.ID); err != nil {
		t.Fatal(err)
	}

	if err := MoveCard(db, m.ID, dev.ID, strp(a.ID), strp(b.ID)); err != nil {
		t.Fatal(err)
	}

	reordered := ListByColumnOrFatal(t, db, dev.ID)
	// Display order preserved (a, b, m by current position): renumber assigns
	// a=0, b=1000, m=2000, then the pending move lands m at (0+1000)/2 == 500.
	for _, c := range reordered {
		switch c.ID {
		case a.ID:
			if c.Position != 0 {
				t.Errorf("a.Position = %d, want 0", c.Position)
			}
		case b.ID:
			if c.Position != 1000 {
				t.Errorf("b.Position = %d, want 1000", c.Position)
			}
		case m.ID:
			if c.Position != 500 {
				t.Errorf("m.Position = %d, want 500", c.Position)
			}
		}
	}
	if len(reordered) != 3 {
		t.Fatalf("column has %d cards, want 3", len(reordered))
	}
	assertNoDuplicatePositions(t, db, dev.ID)
}

func assertNoDuplicatePositions(t *testing.T, db *sql.DB, columnID string) {
	t.Helper()
	var n int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM (SELECT position FROM cards WHERE column_id = ? GROUP BY position HAVING COUNT(*) > 1)",
		columnID,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("duplicate positions in column %q", columnID)
	}
}

func assertColumnPositions(t *testing.T, db *sql.DB, columnID string) {
	t.Helper()
	rows, err := db.Query(
		"SELECT id, position FROM cards WHERE column_id = ? ORDER BY position", columnID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	last := -1 << 30
	for rows.Next() {
		var id string
		var pos int
		if err := rows.Scan(&id, &pos); err != nil {
			t.Fatal(err)
		}
		if pos <= last {
			t.Errorf("positions not strictly increasing (saw %d after %d)", pos, last)
		}
		last = pos
	}
}

func TestMoveCardCrossBoardRejected(t *testing.T) {
	db, w, _, dev, _ := newCardTree(t)
	other, err := CreateBoard(db, w.ID, "Other")
	if err != nil {
		t.Fatal(err)
	}
	otherCols, err := ListColumns(db, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	c := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "t"}))

	if err := MoveCard(db, c.ID, otherCols[0].ID, nil, nil); !errors.Is(err, ErrCrossBoardMove) {
		t.Fatalf("cross-board move = %v, want ErrCrossBoardMove", err)
	}
	got := GetCardOrFatal(t, db, c.ID)
	if got.ColumnID != dev.ID {
		t.Errorf("card moved after rejection: ColumnID = %q, want %q", got.ColumnID, dev.ID)
	}
}

func TestMoveCardAnchorFromAnotherColumn(t *testing.T) {
	db, _, _, dev, done := newCardTree(t)
	a := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "a"}))
	b := mustCard(t)(CreateCard(db, CardInput{ColumnID: done.ID, Title: "b"}))
	m := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "m"}))

	// Anchors are scoped to the target column: b lives in `done`, so the
	// midpoint can't be computed from it — an error, not a silent placement.
	if err := MoveCard(db, m.ID, dev.ID, strp(a.ID), strp(b.ID)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("anchor from other column = %v, want sql.ErrNoRows", err)
	}
	got := GetCardOrFatal(t, db, m.ID)
	if got.ColumnID != dev.ID {
		t.Errorf("card repositioned after bad anchors: ColumnID = %q, want %q", got.ColumnID, dev.ID)
	}
}

func TestMoveCardSingleAnchorRejected(t *testing.T) {
	db, _, _, dev, done := newCardTree(t)
	a := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "a"}))
	m := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "m"}))

	// Exactly one anchor is an error: the midpoint to an unbounded side (top
	// or bottom of the column) can't be expressed as a position without
	// colliding with the anchor card.
	for _, test := range []struct {
		name          string
		before, after *string
	}{
		{"before-only", strp(a.ID), nil},
		{"after-only", nil, strp(a.ID)},
	} {
		if err := MoveCard(db, m.ID, dev.ID, test.before, test.after); !errors.Is(err, ErrPartialAnchors) {
			t.Errorf("%s: move = %v, want ErrPartialAnchors", test.name, err)
		}
	}
	got := GetCardOrFatal(t, db, m.ID)
	if got.ColumnID != dev.ID {
		t.Errorf("card moved after partial-anchor rejection: ColumnID = %q", got.ColumnID)
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM cards WHERE column_id = ?", done.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("cards in done = %d, want 0", n)
	}
}

func TestMoveCardMissingTargetColumn(t *testing.T) {
	db, _, _, dev, _ := newCardTree(t)
	c := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "t"}))
	if err := MoveCard(db, c.ID, "nope", nil, nil); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("MoveCard to missing column = %v, want sql.ErrNoRows", err)
	}
	if got := GetCardOrFatal(t, db, c.ID); got.ColumnID != dev.ID {
		t.Errorf("card moved to missing column: ColumnID = %q", got.ColumnID)
	}
}

func TestDeleteCardAndCascade(t *testing.T) {
	db, _, _, dev, _ := newCardTree(t)
	c := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "t"}))

	if err := DeleteCard(db, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetCard(db, c.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetCard after delete = %v, want sql.ErrNoRows", err)
	}

	mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "t1"}))
	mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "t2"}))
	if err := DeleteColumn(db, dev.ID); err != nil {
		t.Fatal(err)
	}
	n := ListByColumnOrFatal(t, db, dev.ID)
	if len(n) != 0 {
		t.Errorf("cards after column delete = %d, want 0 (cascade)", len(n))
	}
}

func GetCardOrFatal(t *testing.T, db *sql.DB, id string) Card {
	t.Helper()
	c, err := GetCard(db, id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func ListByColumnOrFatal(t *testing.T, db *sql.DB, columnID string) []Card {
	t.Helper()
	cs, err := ListCardsByColumn(db, columnID)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

// TestValidPrioritiesOrder pins the priority set and display order used by
// the TUI picker and CLI validation to the schema CHECK (ADR-001 §3.3).
func TestValidPrioritiesOrder(t *testing.T) {
	want := []string{"low", "medium", "high"}
	if len(ValidPriorities) != len(want) {
		t.Fatalf("len(ValidPriorities) = %d, want %d", len(ValidPriorities), len(want))
	}
	for i, p := range ValidPriorities {
		if p != want[i] {
			t.Errorf("ValidPriorities[%d] = %q, want %q", i, p, want[i])
		}
	}
}
