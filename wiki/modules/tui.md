---
title: tui (internal/tui)
description: The BubbleTea v2 board TUI — model/update/view loop, canonical keymap, five-column layout, card badges + live session markers, Enter-to-open and K-to-kill, the T18 forms overlays (n new card, e edit, N new column, m move), the T19 card detail pane (d), and the T20 extras — search (/), board switch (s), workspace switch (w), help overlay (?). Landed as real Go source in T16 (shell) + T17 (session markers) + T18 (forms) + T19 (detail) + T20 (search/switch/help).
type: module
tags: [ wiki, module, tui, bubbletea ]
---
## Summary

`internal/tui` is the BubbleTea v2 terminal UI: the board (its real surface) plus the four T18 form overlays, with the canonical §3.5 keymap, five-column layout, per-card agent badges and live session markers, and live Enter/K session control. It renders via LipGloss (`lipgloss.JoinHorizontal` column layout, kancli pattern) with each column a Bubbles v2 `list` and a custom card delegate. Forms (`n`/`e`/`N`/`m`) are real since T18 — centered bordered overlays with text and in-place cycle fields. The **card detail pane (`d`) is real since T19** — a centered overlay showing the focused card's metadata, agent (with the `(default)` marker when the card's own agent is empty), codebase path, and its per-run history (start/end times, duration, changed-file count). The **T20 extras are real since T20**: `/` opens a search form applying a client-side filter (case-insensitive title/description substring mirroring `loom card list --search`), `s`/`w` open single-field cycle pickers seeded at the current board/workspace that submit through `ShowBoard`/`SwitchWorkspace` (persisting the selection to `ui_state`) then re-fetch, and `?` opens a stateless keymap overlay derived from the keymap table itself (esc/q close, every key swallowed). Spec: ADR-001 §3.1, §3.5; DESIGN-002 §14.

## Responsibilities

- **Card detail pane (T19):** `d` opens a centered overlay (`cardDetail`) for the focused card — title, description, objective, acceptance criteria, labels, priority/stage badge, the resolved agent (`AgentOrDefault`, `(default)` suffix when the config default applied) with its codebase path (`GetCodebase`), and the run history (`RunsForCard`): each run as `MM-DD HH:MM → [dur]` with a `●`/`◉` live/open marker, `end`-timestamp or `open` for un-ended runs, and `n files changed` (or `no changes`). Owns every key while open: `esc`/`q` closes; all other keys are swallowed (the focus-preserving `detailUpdate` guard, mirroring the form overlay). The history is fetched per-open (never polled); a fetch error renders inline and `esc`/`q` still close. Rendered with a tolerant plain-text renderer (not full Glamour yet).

- **Search (`/`, T20):** `openSearch` opens a single-field `textinput` form seeded with the active filter; enter commits `searchMsg` and `afterSearch` applies it client-side — `m.search` holds the filter and `visibleCards()` narrows `buildLists`/`listCount` (case-insensitive title/description substring, the same semantics as `loom card list --search`, so a TUI action stays scriptable). An empty submit clears the filter, `esc` cancels preserving the prior filter, and the snapshot is never re-fetched — but every later re-fetch preserves the filter.
- **Board switch (`s`, T20):** `openBoardPicker` reads `ListBoards(m.ws.ID)` synchronously (the same DB-only call the CLI makes); a failure or empty board set degrades to a status-bar notice without opening an overlay. The picker is a single cycle field seeded at the current board (`indexOf`); enter submits to `ShowBoard` (persisting `{workspace, board}` to `ui_state`), folded by `afterBoardSwitched` into a re-fetch + toast. `applyFetch`'s board-transition branch resets kill-suppression and `pendingFocus` (both board-scoped, ADR-001 §6).
- **Workspace switch (`w`, T20):** `openWorkspacePicker` reads `ListWorkspaces()` synchronously with the same degrade-on-error/empty semantics; enter submits to `SwitchWorkspace` (which clears the board selection per ADR-001 §6), folded by `afterWorkspaceSwitched` into a re-fetch that resolves the new workspace's first board.
- **Help overlay (`?`, T20):** `openHelp` opens a stateless `helpOverlay` rendering `helpRows` derived from `defaultKeyMap()` — the canonical §3.5 keybinding table, so the overlay can never drift from the bindings. `esc`/`q` close it and every key is swallowed while it is up, exactly like the card-detail pane.

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
    GetCodebase(id string) (store.Codebase, error)                       // T19 detail pane
    RunsForCard(cardID string) ([]store.CardRun, error)                  // T19 detail pane
    ListBoards(workspaceID string) ([]store.Board, error)                // T20 `s` switch
    ShowBoard(boardID string) (store.Board, error)                       // T20 `s` switch
    ListWorkspaces() ([]store.Workspace, error)                          // T20 `w` switch
    SwitchWorkspace(workspaceID string) (store.Workspace, error)         // T20 `w` switch
}

func New(svc Service) Model
func Run(svc Service) error            // blocks until the board quits
func (m Model) Init() tea.Cmd
func (m Model) Update(tea.Msg) (tea.Model, tea.Cmd)
func (m Model) View() tea.View
```

The `Service` seam is the whole board surface: in production it is a [BoardService](../modules/board.md) wrapped with the CLI's config (the `tuiService` adapter supplies `DefaultAgent` and the tmux attach argv — see [cli](../modules/cli.md)); tests inject a `fakeService`. The T20 switch/list methods are auto-satisfied by the embedded `*board.Service`. The 2s poll is a one-shot `tea.Every` that the `pollMsg` handler re-arms, so the board never stops ticking.

## Key files

- `internal/tui/app.go` — `Model`, Init/Update/View loop, fetch + session messages (`fetchMsg`/`pollMsg`/`statusMsg`/`openMsg`/`killMsg`/`attachDoneMsg`), `keyPress` + `quitOverlay`, `startPoll`/`statusTickCmd`/`openSessionCmd`/`killCmd`/`attachCmd` and their `after*` handlers, the `detail` field + the `d` route in `keyPress` (checked after the form guard); T20: the `search`/`help` model fields, the `searchMsg`/`boardSwitchedMsg`/`workspaceSwitchedMsg` msgs + `after*` handlers, the `applyFetch` board-transition reset (kill-suppression + `pendingFocus`), and the `/` `s` `w` `?` keyPress routes (guard order: quit-confirm → form → detail → help → board)
- `internal/tui/keymap.go` — canonical §3.5 `KeyMap`, `defaultKeyMap`, per-column `columnKeyMap` (disables the list's own h/l, filter, quit, help hijack)
- `internal/tui/board.go` — `cardItem`/`cardDelegate` render, `buildLists`, cursor-preserving `refreshMarkers`, `relayout`/`layout`/`statusBar`, `agentBadge`; T20: `visibleCards` feeds `buildLists`/`listCount` so the filter narrows counts too
- `internal/tui/forms.go` — the T18 overlay: `form`/`field` (text + cycle), `formKeyMap` + per-input keymap (tab/up/down freed for the overlay), `openNewCardForm`/`openEditCardForm`/`openNewColumnForm`/`openMoveCardForm`, `cardInput`/`cardUpdate`/`columnInput` marshalling (nil vs `&""` vs value), `view` (`lipgloss.Place` centered box), the four submit msgs + `after*` handlers, `formUpdate` (the keyPress guard), and the styles; T20: the `formSearch`/`formBoardSwitch`/`formWorkspaceSwitch` kinds and their submit routing
- `internal/tui/card_detail.go` — the T19 overlay: `cardDetail` (card + agent/codebase + runs + `status`/`err`), `openCardDetail`, `detailUpdate` (the keyPress guard: esc/q close, else swallow), `detailView` (`lipgloss.Place` centered box), `renderMarkdown` (tolerant plain-text renderer), `detailRunLine`
- `internal/tui/search.go` — the T20 `/` overlay: `searchMsg`, `filterCards`/`visibleCards` (case-insensitive title/description substring), `openSearch` (seeded with the active filter), `afterSearch`
- `internal/tui/switch.go` — the T20 `s`/`w` pickers: `boardSwitchedMsg`/`workspaceSwitchedMsg`, `switchForm` (single cycle field seeded via `indexOf`), `openBoardPicker`/`openWorkspacePicker` (synchronous list, degrade-on-error/empty), `afterBoardSwitched`/`afterWorkspaceSwitched`
- `internal/tui/help.go` — the T20 `?` overlay: `helpOverlay`, `helpUpdate` (esc/q close, swallow the rest), `helpView`, `helpRows`/`keyNames` (derived from `defaultKeyMap()` so it can never drift), and the styles
- `internal/tui/card_detail_test.go` — T19 acceptance: `d` opens the detail pane, `esc`/`q` close it (and only it — the underlying cursor/session state survives), fields render (metadata, agent `(default)` suffix, codebase path, run history with duration + file counts, open-run marker, error inline)
- `internal/tui/app_test.go` — `fakeService` seam (incl. the five T18 write methods, the T19 `GetCodebase`/`RunsForCard` mirror, and the T20 `boards`/`workspaces`/`boardsErr`/`workspacesErr`/`showBoardID`/`switchWsID` fields + `ListBoards`/`ShowBoard`/`ListWorkspaces`/`SwitchWorkspace`) + BubbleTea-driven tests (navigation, layout, quit confirm/force, badge/markers, completion toast, open/kill, attach handoff argv, poll re-arm)
- `internal/tui/forms_test.go` — T18 acceptance: overlay opening, new-card submit + marshalling + refocus, agent nil-vs-pinned, validation, cancel, tab/shift+tab/left/right navigation, edit seeding + three-way update marshalling, edit agent reset to NULL, new-column, move picker routing + follow-the-card refocus, key capture (q types, j swallowed), poll-under-form, move-to-done routing, empty-column degradation
- `internal/tui/search_test.go` — T20 acceptance: `/` narrows the board (matching cards stay, header counts narrow), title/description + case-insensitive matching, esc restores the prior filter, empty submit clears, the filter survives re-fetches
- `internal/tui/switch_test.go` — T20 acceptance: `s`/`w` open the picker seeded at the current board/workspace, left/right cycle, enter submits through `ShowBoard`/`SwitchWorkspace` + re-fetch, esc cancels without a service write, list-error/empty-list degrade to a notice, submit-error toasts, board-scoped kill-suppression cleared on transition
- `internal/tui/help_test.go` — T20 acceptance: `?` renders the canonical §3.5 rows, esc/q close it, and board keys are swallowed while it is up

## Dependencies

- [board](../modules/board.md) (selection, cards, columns, sessions — the whole Service), and transitively `store`, `session`, `config`, `agent`. External: BubbleTea v2 (`charm.land/bubbletea/v2` v2.0.7), Bubbles v2 (`charm.land/bubbles/v2` v2.1.1), LipGloss v2 (`charm.land/lipgloss/v2` v2.0.5) — `textinput` drives the form fields. Glamour was added as a dependency at T19 but the detail pane currently renders markdown with a tolerant plain-text renderer — the full Glamour path is a later enhancement.

## Participates in

- Enter = ensure + attach through [BoardService.OpenCard](../modules/board.md) + the tmux attach handoff; `K` kill/finalizes; the 2s poll drives markers and completion toasts — the [card open → completion](../flows/card-open-complete.md) flow.

## Related

- Architecture: [Architecture Overview](../architecture/overview.md) · [Session Model](../architecture/session-model.md)
- Concepts: [session](../concepts/session.md) · [stage](../concepts/stage.md) · [agent-driver](../concepts/agent-driver.md)
- Flows: [Card open → completion](../flows/card-open-complete.md) · [Attach/detach](../flows/attach-detach.md)
- CLI twin: [cli](../modules/cli.md)