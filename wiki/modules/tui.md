---
title: tui (internal/tui)
description: The BubbleTea v2 board TUI — model/update/view loop, canonical keymap, five-column layout, card badges + live session markers, Enter-to-open and K-to-kill, and the T18 forms overlays (n new card, e edit, N new column, m move). Landed as real Go source in T16 (shell) + T17 (session markers) + T18 (forms).
type: module
tags: [ wiki, module, tui, bubbletea ]
---
## Summary

`internal/tui` is the BubbleTea v2 terminal UI: the board (its real surface) plus the four T18 form overlays, with the canonical §3.5 keymap, five-column layout, per-card agent badges and live session markers, and live Enter/K session control. It renders via LipGloss (`lipgloss.JoinHorizontal` column layout, kancli pattern) with each column a Bubbles v2 `list` and a custom card delegate. Forms (`n`/`e`/`N`/`m`) are real since T18 — centered bordered overlays with text and in-place cycle fields. Still bound-but-stubbed to a status-bar notice naming its task: the detail view (`d`), search, board/workspace switch and help (`/` `s` `w` `?`) (T19–T20). Spec: ADR-001 §3.1, §3.5; DESIGN-002 §14.

## Responsibilities

- **Board shell (T16):** 5-column default (Backlog/To Do/In Progress/Review/Done) via `lipgloss.JoinHorizontal`; each column a `bubbles/list` with the custom card delegate. The lists keep only cursor + paging keys — the board owns h/l, `/`, q, `?` (each list's own bindings are disabled in `columnKeyMap`).
- **Navigation:** `j/k` moves the cursor within the focused column, `h/l` moves column focus, pgup/pgdown/home/end scroll. Quit: `q` confirms when sessions are live, `Q` force-quits (sessions keep running detached).
- **Session markers + badges (T17):** each card renders `▸ [cl] Title ●` — the §14 agent badge (`cl`/`oc` from `card.AgentOrDefault(cfg default)`), a `●` running / `◉` attached marker, bold title. Markers refresh every 2s without a keystroke.
- **Enter → open+attach (T17):** ensures the card's session via `svc.OpenCard(ctx, id, detach=true)` (the ensure's probe window never blocks Update), then hands the terminal to `tmux -L <server> attach-session -t loom-<id>` through `tea.ExecProcess` so BubbleTea owns the TTY and restores the board on detach.
- **K → kill+finalize (T17):** `svc.CloseCard` kills the session and finalizes its run. The kill suppresses the poll's completion toast so a killed card isn't re-toasted as "session ended"; the suppression clears when the card is reopened.
- **Session poll (T17):** a 2s `tea.Every` tick reads `SessionStatus` and updates markers; a previously-live card going absent raises the "session ended" toast (detached completion).
- **Form overlays (T18):** `n` new card (title, column, priority, agent), `e` edit card (title/description/objective/acceptance criteria/priority/labels/agent), `N` new column (name + stage), `m` move card — one concrete `form` struct with text (`textinput`) and in-place cycle fields (left/right), tab/shift+tab field cycling, enter submit from any field, esc cancel, and a form-local error line. Rendered as a centered bordered box (`lipgloss.Place`) that owns every key while open. Marshalling: create keeps empty optionals nil, edit diffs against the card (unchanged → untouched; changed-to-`""` clears a nullable col to NULL), and the agent picker's empty-default entry yields NULL on create / `&""` reset on edit via a `touched` flag (DESIGN-002 §14). After a successful submit the board re-fetches and **refocuses** the affected card (via `pendingFocus`/`applyPendingFocus`).

## Public API / entry points

```go
type Service interface {
    ResolveSelection() (store.Workspace, store.Board, error)
    ListColumns(boardID string) ([]store.Column, error)
    ListCardsByBoard(boardID string) ([]store.Card, error)
    SessionStatus(ctx context.Context) (map[string]session.SessionStatus, error)
    OpenCard(ctx context.Context, cardID string, detach bool) error
    CloseCard(ctx context.Context, cardID string) error
    TmuxAttach(cardID string) (*exec.Cmd, error)
    DefaultAgent() string
    CreateCard(in store.CardInput) (store.Card, error)                    // T18 forms
    UpdateCard(id string, u store.CardUpdate) (store.Card, error)        // T18 forms
    GetCard(id string) (store.Card, error)                               // T18 forms
    CreateColumn(boardID, name, stage string) (store.Column, error)      // T18 forms
    MoveCard(ctx context.Context, cardID, toColumnID string, beforeID, afterID *string) (store.Card, error) // T18 forms
}

func New(svc Service) Model
func Run(svc Service) error            // blocks until the board quits
func (m Model) Init() tea.Cmd
func (m Model) Update(tea.Msg) (tea.Model, tea.Cmd)
func (m Model) View() tea.View
```

The `Service` seam is the whole board surface: in production it is a [BoardService](../modules/board.md) wrapped with the CLI's config (the `tuiService` adapter supplies `DefaultAgent` and the tmux attach argv — see [cli](../modules/cli.md)); tests inject a `fakeService`. The 2s poll is a one-shot `tea.Every` that the `pollMsg` handler re-arms, so the board never stops ticking.

## Key files

- `internal/tui/app.go` — `Model`, Init/Update/View loop, fetch + session messages (`fetchMsg`/`pollMsg`/`statusMsg`/`openMsg`/`killMsg`/`attachDoneMsg`), `keyPress` + `quitOverlay`, `startPoll`/`statusTickCmd`/`openSessionCmd`/`killCmd`/`attachCmd` and their `after*` handlers
- `internal/tui/keymap.go` — canonical §3.5 `KeyMap`, `defaultKeyMap`, per-column `columnKeyMap` (disables the list's own h/l, filter, quit, help hijack)
- `internal/tui/board.go` — `cardItem`/`cardDelegate` render, `buildLists`, cursor-preserving `refreshMarkers`, `relayout`/`layout`/`statusBar`, `agentBadge`
- `internal/tui/forms.go` — the T18 overlay: `form`/`field` (text + cycle), `formKeyMap` + per-input keymap (tab/up/down freed for the overlay), `openNewCardForm`/`openEditCardForm`/`openNewColumnForm`/`openMoveCardForm`, `cardInput`/`cardUpdate`/`columnInput` marshalling (nil vs `&""` vs value), `view` (`lipgloss.Place` centered box), the four submit msgs + `after*` handlers, `formUpdate` (the keyPress guard), and the styles
- `internal/tui/app_test.go` — `fakeService` seam (incl. the five T18 write methods) + BubbleTea-driven tests (navigation, layout, quit confirm/force, badge/markers, completion toast, open/kill, attach handoff argv, poll re-arm)
- `internal/tui/forms_test.go` — T18 acceptance: overlay opening, new-card submit + marshalling + refocus, agent nil-vs-pinned, validation, cancel, tab/shift+tab/left/right navigation, edit seeding + three-way update marshalling, edit agent reset to NULL, new-column, move picker routing + follow-the-card refocus, key capture (q types, j swallowed), poll-under-form, move-to-done routing, empty-column degradation

## Dependencies

- [board](../modules/board.md) (selection, cards, columns, sessions — the whole Service), and transitively `store`, `session`, `config`, `agent`. External: BubbleTea v2 (`charm.land/bubbletea/v2` v2.0.7), Bubbles v2 (`charm.land/bubbles/v2` v2.1.1), LipGloss v2 (`charm.land/lipgloss/v2` v2.0.5) — `textinput` drives the form fields. Glamour is still future (card detail, T19).

## Participates in

- Enter = ensure + attach through [BoardService.OpenCard](../modules/board.md) + the tmux attach handoff; `K` kill/finalizes; the 2s poll drives markers and completion toasts — the [card open → completion](../flows/card-open-complete.md) flow.

## Related

- Architecture: [Architecture Overview](../architecture/overview.md) · [Session Model](../architecture/session-model.md)
- Concepts: [session](../concepts/session.md) · [stage](../concepts/stage.md) · [agent-driver](../concepts/agent-driver.md)
- Flows: [Card open → completion](../flows/card-open-complete.md) · [Attach/detach](../flows/attach-detach.md)
- CLI twin: [cli](../modules/cli.md)