package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"loom/internal/session"
	"loom/internal/store"
)

// TestFormKeysOpenOverlays: n/N open immediately; m/e require a focused card
// and set the right overlay kind; m/e on an empty column degrade to a notice.
func TestFormKeysOpenOverlays(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{{ID: "k1", ColumnID: "c-blog", Title: "alpha"}}
	m := readyModel(t, svc)

	m, _ = press(t, m, 'n')
	if m.form == nil || m.form.kind != formNewCard {
		t.Fatalf("n did not open new-card form, form=%v", m.form)
	}
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.form != nil {
		t.Fatal("esc did not close the form")
	}

	m, _ = press(t, m, 'N')
	if m.form == nil || m.form.kind != formNewColumn {
		t.Fatalf("N did not open new-column form, form=%v", m.form)
	}
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})

	m, _ = press(t, m, 'm')
	if m.form == nil || m.form.kind != formMoveCard {
		t.Fatalf("m did not open move form, form=%v", m.form)
	}
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})

	m, _ = press(t, m, 'e')
	if m.form == nil || m.form.kind != formEditCard {
		t.Fatalf("e did not open edit form, form=%v", m.form)
	}
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})

	// Empty board: m/e must not open a form.
	svc2 := newBoardService()
	m2 := readyModel(t, svc2)
	m2, _ = press(t, m2, 'm')
	if m2.form != nil || !strings.Contains(m2.note, "no card to move") {
		t.Errorf("m on empty board: form=%v note=%q", m2.form, m2.note)
	}
	m2, _ = press(t, m2, 'e')
	if m2.form != nil || !strings.Contains(m2.note, "no card to edit") {
		t.Errorf("e on empty board: form=%v note=%q", m2.form, m2.note)
	}
}

// TestNewCardFormSubmits walks the n form end to end: type a title and
// description, cycle priority to high, submit, and assert the marshalled
// CardInput plus the post-fetch refocus onto the new card.
func TestNewCardFormSubmits(t *testing.T) {
	svc := newBoardService()
	m := readyModel(t, svc)

	m, _ = press(t, m, 'n')
	m, _ = typeText(t, m, "Buy milk")

	// tab to description, type it; tab to column, cycle right (c-blog →
	// c-todo); tab to priority, right (high)
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	m, _ = typeText(t, m, "semi-skimmed")
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyRight})

	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	msg := execMsg(t, cmd).(cardCreatedMsg)
	if msg.err != nil {
		t.Fatalf("create err = %v", msg.err)
	}
	if len(svc.createInput.Title) == 0 {
		t.Fatal("title not marshalled")
	}
	if svc.createInput.Title != "Buy milk" {
		t.Errorf("title = %q, want Buy milk", svc.createInput.Title)
	}
	if svc.createInput.Description == nil || *svc.createInput.Description != "semi-skimmed" {
		t.Errorf("Description = %v, want &semi-skimmed", svc.createInput.Description)
	}
	if svc.createInput.ColumnID != "c-todo" {
		t.Errorf("ColumnID = %q, want c-todo", svc.createInput.ColumnID)
	}
	if svc.createInput.Priority != "high" {
		t.Errorf("Priority = %q, want high", svc.createInput.Priority)
	}
	if svc.createInput.Agent != nil {
		t.Errorf("Agent = %v, want nil (empty picker entry)", *svc.createInput.Agent)
	}

	// Drive the full loop: append the new card to the fake, fold the result
	// msg, run the follow-up fetch, and assert the cursor lands on it.
	svc.cards = append(svc.cards, msg.card)
	m, cmd = update(t, m, msg)
	if cmd == nil {
		t.Fatal("afterCardCreated returned nil cmd, want fetch")
	}
	fm := execMsg(t, cmd).(fetchMsg)
	if fm.err != nil {
		t.Fatalf("fetch err = %v", fm.err)
	}
	m, _ = update(t, m, fm)
	if m.focus != 1 {
		t.Errorf("focus = %d, want 1 (c-todo)", m.focus)
	}
	if got := m.lists[m.focus].Index(); got != 0 {
		t.Errorf("cursor index = %d, want 0 (new card first)", got)
	}
}

// TestNewCardAgentNilVsPinned: the picker's empty-default entry yields a NULL
// agent; cycling to a named agent pins it.
func TestNewCardAgentNilVsPinned(t *testing.T) {
	svc := newBoardService()
	m := readyModel(t, svc)

	m, _ = press(t, m, 'n')
	m, _ = typeText(t, m, "x")
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if a := execMsg(t, cmd).(cardCreatedMsg); a.err != nil || svc.createInput.Agent != nil {
		t.Errorf("empty picker entry: err=%v agent=%v, want nil", a.err, svc.createInput.Agent)
	}

	svc2 := newBoardService()
	m2 := readyModel(t, svc2)
	m2, _ = press(t, m2, 'n')
	m2, _ = typeText(t, m2, "x")
	// tab ×4 to agent (title → description → column → priority → agent),
	// right ×2 ("" → claude → opencode)
	for i := 0; i < 4; i++ {
		m2, _ = pressKey(t, m2, tea.KeyPressMsg{Code: tea.KeyTab})
	}
	m2, _ = pressKey(t, m2, tea.KeyPressMsg{Code: tea.KeyRight})
	m2, _ = pressKey(t, m2, tea.KeyPressMsg{Code: tea.KeyRight})
	m2, cmd2 := pressKey(t, m2, tea.KeyPressMsg{Code: tea.KeyEnter})
	if a := execMsg(t, cmd2).(cardCreatedMsg); a.err != nil {
		t.Fatalf("create err = %v", a.err)
	}
	if svc2.createInput.Agent == nil || *svc2.createInput.Agent != "opencode" {
		t.Errorf("Agent = %v, want &opencode", svc2.createInput.Agent)
	}
}

// TestNewCardValidation: an empty title keeps the form open with a form-local
// error line; typing clears it.
func TestNewCardValidation(t *testing.T) {
	svc := newBoardService()
	m := readyModel(t, svc)

	m, _ = press(t, m, 'n')
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("empty title should not submit")
	}
	if m.form == nil {
		t.Fatal("form closed on validation error")
	}
	if !strings.Contains(m.form.err, "title is required") {
		t.Errorf("form.err = %q, want title is required", m.form.err)
	}
	if got := plain(m.View().Content); !strings.Contains(got, "title is required") {
		t.Errorf("error not rendered in view:\n%s", got)
	}

	m, _ = typeText(t, m, "x")
	if m.form.err != "" {
		t.Errorf("err not cleared on keystroke: %q", m.form.err)
	}
}

// TestFormCancel: esc closes the form without any service write.
func TestFormCancel(t *testing.T) {
	svc := newBoardService()
	m := readyModel(t, svc)

	m, _ = press(t, m, 'n')
	m, _ = typeText(t, m, "cancel me")
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd != nil {
		t.Error("esc returned a cmd, want nil")
	}
	if m.form != nil {
		t.Fatal("form still open after esc")
	}
	if svc.createInput.Title != "" {
		t.Error("esc should not have called CreateCard")
	}
}

// TestFormNavigation: tab advances and wraps, shift+tab goes back, and
// left/right cycle a cycle field in place but move the cursor in a text field.
func TestFormNavigation(t *testing.T) {
	svc := newBoardService()
	m := readyModel(t, svc)

	m, _ = press(t, m, 'n')
	// tab: title(0) → description(1) → column(2) → priority(3) → agent(4) →
	// wraps to title(0)
	for _, want := range []int{1, 2, 3, 4, 0} {
		m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
		if m.form.focus != want {
			t.Fatalf("after tab focus = %d, want %d", m.form.focus, want)
		}
	}
	// shift+tab from title(0) wraps to agent(4)
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.form.focus != 4 {
		t.Fatalf("after shift+tab focus = %d, want 4", m.form.focus)
	}

	// right cycles the agent selection in place
	before := m.form.fields[4].index
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	if got := m.form.fields[4].index; got != (before+1)%len(m.form.fields[4].options) {
		t.Errorf("agent index = %d, want %d", got, (before+1)%len(m.form.fields[4].options))
	}
	if !m.form.fields[4].touched {
		t.Error("cycling did not mark the field touched")
	}

	// left/right on a text field must not cycle; they reach the input instead
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyTab}) // agent(4) → title(0)
	if m.form.focus != 0 {
		t.Fatalf("focus = %d, want 0 (title)", m.form.focus)
	}
	m, _ = typeText(t, m, "ab")
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if got := m.form.fields[0].input.Position(); got != 1 {
		t.Errorf("text cursor position = %d, want 1", got)
	}
}

// TestEditFormSeedsAndSubmits: the e form seeds from the card, clears the
// description to NULL, and marshals a three-way CardUpdate (nil untouched /
// &"" clear / &v set).
func TestEditFormSeedsAndSubmits(t *testing.T) {
	desc := "d"
	svc := newBoardService()
	svc.cards = []store.Card{
		{ID: "k1", ColumnID: "c-blog", BoardID: "b1", Title: "alpha", Description: &desc, Priority: "low", Agent: strp("opencode")},
	}
	m := readyModel(t, svc)

	m, _ = press(t, m, 'e')
	f := m.form
	if f == nil {
		t.Fatal("e did not open edit form")
	}
	if got := f.fields[efTitle].input.Value(); got != "alpha" {
		t.Errorf("seeded title = %q, want alpha", got)
	}
	if got := f.fields[efAgent].selectedLabel(); got != "opencode" {
		t.Errorf("seeded agent = %q, want opencode", got)
	}

	// change title, clear description (empty = &"" clears to NULL), leave the
	// rest untouched
	m, _ = typeText(t, m, "2") // title alpha → alpha2
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	msg := execMsg(t, cmd).(cardUpdatedMsg)
	if msg.err != nil {
		t.Fatalf("update err = %v", msg.err)
	}
	if svc.updateID != "k1" {
		t.Errorf("updateID = %q, want k1", svc.updateID)
	}
	u := svc.updateInput
	if u.Title == nil || *u.Title != "alpha2" {
		t.Errorf("Title = %v, want &alpha2", u.Title)
	}
	if u.Description == nil || *u.Description != "" {
		t.Errorf("Description = %v, want &\"\" (clear to NULL)", u.Description)
	}
	if u.Objective != nil || u.AcceptanceCriteria != nil || u.Labels != nil {
		t.Errorf("untouched fields should stay nil: %+v", u)
	}
	if u.Priority != nil {
		t.Errorf("untouched Priority = %v, want nil", u.Priority)
	}
	if u.Agent != nil {
		t.Errorf("untouched Agent = %v, want nil (keeps opencode)", u.Agent)
	}
}

// TestEditAgentResetToNull: cycling the agent picker to the empty-default
// entry on an edit clears the pinned agent to NULL (&""), the DESIGN-002 §13
// reset.
func TestEditAgentResetToNull(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{{ID: "k1", ColumnID: "c-blog", BoardID: "b1", Title: "alpha", Agent: strp("opencode")}}
	m := readyModel(t, svc)

	m, _ = press(t, m, 'e')
	// title field first; tab to agent (7 fields: title,desc,obj,ac,prio,labels,agent)
	for i := 0; i < 6; i++ {
		m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	}
	if got := m.form.fields[efAgent].selectedLabel(); got != "opencode" {
		t.Fatalf("seeded agent = %q, want opencode", got)
	}
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyLeft})
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if got := m.form.fields[efAgent].selectedValue(); got != "" {
		t.Fatalf("cycled agent value = %q, want empty-default", got)
	}
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if a := execMsg(t, cmd).(cardUpdatedMsg); a.err != nil {
		t.Fatalf("update err = %v", a.err)
	}
	if svc.updateInput.Agent == nil || *svc.updateInput.Agent != "" {
		t.Errorf("Agent = %v, want &\"\" (reset to NULL)", svc.updateInput.Agent)
	}
}

// TestNewColumnForm: N types a name, cycles the stage, and marshals
// CreateColumn against the current board.
func TestNewColumnForm(t *testing.T) {
	svc := newBoardService()
	m := readyModel(t, svc)

	m, _ = press(t, m, 'N')
	m, _ = typeText(t, m, "Someday")
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyRight}) // todo → dev
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	msg := execMsg(t, cmd).(columnCreatedMsg)
	if msg.err != nil {
		t.Fatalf("column err = %v", msg.err)
	}
	if len(svc.createCol) != 1 {
		t.Fatalf("CreateColumn calls = %d, want 1", len(svc.createCol))
	}
	c := svc.createCol[0]
	if c.boardID != "b1" || c.name != "Someday" || c.stage != "dev" {
		t.Errorf("CreateColumn = %+v, want b1/Someday/dev", c)
	}
}

// TestNewColumnValidation rejects an empty name.
func TestNewColumnValidation(t *testing.T) {
	svc := newBoardService()
	m := readyModel(t, svc)

	m, _ = press(t, m, 'N')
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("empty name should not submit")
	}
	if m.form == nil || !strings.Contains(m.form.err, "name is required") {
		t.Fatalf("form = %v, err = %q", m.form, m.form.err)
	}
}

// TestMovePickerFollowsCard: m opens a column cycle over the card's board,
// enter routes MoveCard with (nil,nil) anchors, and the refocus lands the
// cursor in the destination column.
func TestMovePickerFollowsCard(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{{ID: "k1", ColumnID: "c-todo", BoardID: "b1", Title: "alpha"}}
	m := readyModel(t, svc)

	// focus the card: c-todo is focus 1
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	m, _ = press(t, m, 'm')
	if m.form == nil || m.form.kind != formMoveCard {
		t.Fatalf("m did not open move form, form=%v", m.form)
	}
	if got := m.form.fields[mfColumn].selectedLabel(); got != "To Do" {
		t.Errorf("seeded column = %q, want To Do", got)
	}
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyRight}) // To Do → In Progress
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	msg := execMsg(t, cmd).(cardMovedMsg)
	if msg.err != nil {
		t.Fatalf("move err = %v", msg.err)
	}
	if len(svc.moveCalls) != 1 {
		t.Fatalf("MoveCard calls = %d, want 1", len(svc.moveCalls))
	}
	mc := svc.moveCalls[0]
	if mc.cardID != "k1" || mc.toColumnID != "c-dev" {
		t.Errorf("MoveCard = %+v, want k1 → c-dev", mc)
	}
	if mc.beforeID != nil || mc.afterID != nil {
		t.Errorf("MoveCard anchors = %v/%v, want nil/nil (append)", mc.beforeID, mc.afterID)
	}

	// fold result + refetch: cursor should land on the card in c-dev (focus 2)
	m, cmd = update(t, m, msg)
	fm := execMsg(t, cmd).(fetchMsg)
	m, _ = update(t, m, fm)
	if m.focus != 2 {
		t.Errorf("focus = %d, want 2 (In Progress)", m.focus)
	}
	if got := m.lists[m.focus].Index(); got != 0 {
		t.Errorf("cursor index = %d, want 0", got)
	}
}

// TestKeyCapture: while a form is open, q types instead of quitting and j/k
// do not move the board — the form owns every key.
func TestKeyCapture(t *testing.T) {
	svc := newBoardService()
	m := readyModel(t, svc)

	m, _ = press(t, m, 'n')
	m, _ = typeText(t, m, "qu")
	m, _ = typeText(t, m, "q")
	if m.confirmQuit {
		t.Fatal("q with a form open must not open the quit overlay")
	}
	if m.form == nil {
		t.Fatal("form closed by board key")
	}
	if got := m.form.fields[0].input.Value(); got != "quq" {
		t.Errorf("typed value = %q, want quq", got)
	}

	m, _ = typeText(t, m, "j")
	if m.form == nil {
		t.Fatal("form closed by board key")
	}
	if got := m.form.fields[0].input.Value(); got != "quqj" {
		t.Errorf("j should type into the field (not move the board), value = %q, want quqj", got)
	}
}

// TestPollUnderForm: the 2s session poll keeps firing under a form; markers
// update but the form view is unaffected and the form stays open.
func TestPollUnderForm(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{{ID: "k1", ColumnID: "c-blog", Title: "alpha"}}
	svc.status = map[string]session.SessionStatus{"k1": {Running: true}}
	m := readyModel(t, svc)

	m, _ = press(t, m, 'n')
	viewBefore := plain(m.View().Content)
	m, _ = update(t, m, statusMsg{status: map[string]session.SessionStatus{}})
	if m.form == nil {
		t.Fatal("status tick closed the form")
	}
	if viewBefore != plain(m.View().Content) {
		t.Errorf("form view changed under the poll:\n--- before ---\n%s\n--- after ---\n%s", viewBefore, plain(m.View().Content))
	}
}

// TestNewCardNoColumnsDegrades is the column-delete-stripped-board edge: the
// n form opens without panicking on the empty column picker and validation
// rejects the submit with a clear message.
func TestNewCardNoColumnsDegrades(t *testing.T) {
	svc := newBoardService()
	svc.cols = nil
	m := readyModel(t, svc)

	m, _ = press(t, m, 'n')
	if m.form == nil {
		t.Fatal("n did not open the new-card form")
	}
	if got := plain(m.View().Content); !strings.Contains(got, "(none available)") {
		t.Errorf("empty column picker not degraded in view:\n%s", got)
	}
	m, _ = typeText(t, m, "x")
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("submit with no columns should not produce a cmd")
	}
	if m.form == nil || !strings.Contains(m.form.err, "no columns") {
		t.Fatalf("form = %v, err = %q, want board has no columns", m.form, m.form.err)
	}
}

// TestMoveToDoneRoutesThroughService is a smoke check that the m form routes
// to MoveCard even for a done-stage target — the auto-kill is BoardService's
// job (service.go:125), not the TUI's.
func TestMoveToDoneRoutesThroughService(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{{ID: "k1", ColumnID: "c-blog", BoardID: "b1", Title: "alpha"}}
	m := readyModel(t, svc)

	m, _ = press(t, m, 'm')
	// c-blog(0) → ... → Done(4)
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if a := execMsg(t, cmd).(cardMovedMsg); a.err != nil {
		t.Fatalf("move err = %v", a.err)
	}
	if len(svc.moveCalls) != 1 || svc.moveCalls[0].toColumnID != "c-done" {
		t.Errorf("move calls = %+v, want k1 → c-done", svc.moveCalls)
	}
}
