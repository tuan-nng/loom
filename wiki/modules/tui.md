---
title: tui (internal/tui)
description: The BubbleTea board UI — models, components, canonical keybindings, card badges, forms, and the card-detail view.
type: module
tags: [wiki, module, tui, bubbletea]
---

## Summary

`internal/tui` is the BubbleTea v2 terminal UI: the Kanban board, card-detail view, forms (new/edit card + agent picker, new column, move), help overlay, and session markers. It renders via LipGloss (`lipgloss.JoinHorizontal` column layout, kancli pattern), Bubbles widgets (`list`, `textinput`), and Glamour markdown in the detail view. Spec: ADR-001 §3.1, §3.5; DESIGN-002 §14.

## Responsibilities

- Board layout: 5-column default (Backlog/To Do/In Progress/Review/Done) via `lipgloss.JoinHorizontal`; each column a `bubbles/list` with a custom card delegate.
- Canonical keybindings (ADR-001 §3.5): `j/k` cards, `h/l` columns, `Enter` open/attach, `m` move (done-stage auto-kills session), `n` new card, `N` new column, `K` kill session, `d` detail, `e` edit, `/` search, `s` board, `w` workspace, `?` help, `q`/`Q`/`Ctrl+c` quit.
- Session markers per card: running / attached; no marker when idle.
- Agent badge (`cl`/`oc`) next to priority/labels, resolved from `card.AgentOrDefault(cfg.Agent.Default)`; agent picker in `n`/`e`; "Agent: … (default)" field in detail.
- Forms via Bubbles; markdown card-detail via Glamour; trace history (Files Changed list) in detail.

## Public API / entry points

```go
type Model struct { ... }
func New(...) *Model
func (m Model) Init() tea.Cmd
func (m Model) Update(tea.Msg) (tea.Model, tea.Cmd)
func (m Model) View() string
```

## Key files

- `internal/tui/app.go` — program wiring, model state
- `internal/tui/board.go` — board + card list + session markers
- `internal/tui/card_detail.go` — detail view (description, AC, trace history, agent field)
- `internal/tui/forms.go` — new/edit card, agent picker, new column, move
- `internal/tui/keymap.go` — canonical keybindings

## Dependencies

- Everything below (`config`, `agent`, `store`, `session`, `trace`, `board`). External: BubbleTea v2, Bubbles, LipGloss, Glamour.

## Participates in

- Enter = launch the card's agent via [SessionManager.ensure](../modules/session.md); `K`/`m` kill/finalize; poll updates markers; reconcile-on-startup runs at TUI startup.

## Related

- Architecture: [Architecture Overview](../architecture/overview.md) · [Session Model](../architecture/session-model.md)
- Concepts: [session](../concepts/session.md) · [stage](../concepts/stage.md)
- Flows: [Card open → completion](../flows/card-open-complete.md) · [Attach/detach](../flows/attach-detach.md)
