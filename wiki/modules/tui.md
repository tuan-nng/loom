---
title: tui (internal/tui)
description: The BubbleTea v2 board TUI — model/update/view loop, canonical keymap, five-column layout, card badges + live session markers, Enter-to-open and K-to-kill. Landed as real Go source in T16 (shell) + T17 (session markers).
type: module
tags: [ wiki, module, tui, bubbletea ]
---
## Summary

`internal/tui` is the BubbleTea v2 terminal UI: the board (its only real surface so far), with the canonical §3.5 keymap, five-column layout, per-card agent badges and live session markers, and live Enter/K session control. It renders via LipGloss (`lipgloss.JoinHorizontal` column layout, kancli pattern) with each column a Bubbles v2 `list` and a custom card delegate. Everything else — forms (`n`/`e`/`N`/`m`), the detail view (`d`), search, board/workspace switch and help (`/` `s` `w` `?`) — is bound but stubbed to a status-bar notice naming its task (T18–T20). Spec: ADR-001 §3.1, §3.5; DESIGN-002 §14.

## Responsibilities

- **Board shell (T16):** 5-column default (Backlog/To Do/In Progress/Review/Done) via `lipgloss.JoinHorizontal`; each column a `bubbles/list` with the custom card delegate. The lists keep only cursor + paging keys — the board owns h/l, `/`, q, `?` (each list's own bindings are disabled in `columnKeyMap`).
- **Navigation:** `j/k` moves the cursor within the focused column, `h/l` moves column focus, pgup/pgdown/home/end scroll. Quit: `q` confirms when sessions are live, `Q` force-quits (sessions keep running detached).
- **Session markers + badges (T17):** each card renders `▸ [cl] Title ●` — the §14 agent badge (`cl`/`oc` from `card.AgentOrDefault(cfg default)`), a `●` running / `◉` attached marker, bold title. Markers refresh every 2s without a keystroke.
- **Enter → open+attach (T17):** ensures the card's session via `svc.OpenCard(ctx, id, detach=true)` (the ensure's probe window never blocks Update), then hands the terminal to `tmux -L <server> attach-session -t loom-<id>` through `tea.ExecProcess` so BubbleTea owns the TTY and restores the board on detach.
- **K → kill+finalize (T17):** `svc.CloseCard` kills the session and finalizes its run. The kill suppresses the poll's completion toast so a killed card isn't re-toasted as "session ended"; the suppression clears when the card is reopened.
- **Session poll (T17):** a 2s `tea.Every` tick reads `SessionStatus` and updates markers; a previously-live card going absent raises the "session ended" toast (detached completion).

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
- `internal/tui/app_test.go` — `fakeService` seam + BubbleTea-driven tests (navigation, layout, quit confirm/force, badge/markers, completion toast, open/kill, attach handoff argv, poll re-arm)

## Dependencies

- [board](../modules/board.md) (selection, cards, columns, sessions — the whole Service), and transitively `store`, `session`, `config`, `agent`. External: BubbleTea v2 (`charm.land/bubbletea/v2` v2.0.7), Bubbles v2 (`charm.land/bubbles/v2` v2.1.1), LipGloss v2 (`charm.land/lipgloss/v2` v2.0.5). Glamour is still future (card detail, T19).

## Participates in

- Enter = ensure + attach through [BoardService.OpenCard](../modules/board.md) + the tmux attach handoff; `K` kill/finalizes; the 2s poll drives markers and completion toasts — the [card open → completion](../flows/card-open-complete.md) flow.

## Related

- Architecture: [Architecture Overview](../architecture/overview.md) · [Session Model](../architecture/session-model.md)
- Concepts: [session](../concepts/session.md) · [stage](../concepts/stage.md) · [agent-driver](../concepts/agent-driver.md)
- Flows: [Card open → completion](../flows/card-open-complete.md) · [Attach/detach](../flows/attach-detach.md)
- CLI twin: [cli](../modules/cli.md)