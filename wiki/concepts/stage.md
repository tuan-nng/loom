---
title: stage
description: A column's workflow stage (backlog|todo|dev|review|done); stage carries real behavior — done auto-kills the session.
type: concept
tags: [wiki, concept, kanban]
---

## Definition

A column's **stage** is its workflow position: `backlog | todo | dev | review | done` (default `dev`). It is *not* just a label — it carries real behavior: moving a card into a column whose stage is `done` **auto-kills the card's session and finalizes its trace** (ADR-001 §3.3, §4.1). There is no separate `cards.status` column — workflow state is expressed entirely by the card's column.

## Why it matters

- **"Mark done" and "stop the agent" cannot diverge** — an agent left running in Done is the one case a board user won't notice. The move triggers the kill automatically.
- Extra columns may **reuse a stage**, so the trigger is 'target column's stage == done', never a column id.
- The CHECK constraint (`stage IN (...)`), the default 5-column board (`Backlog/To Do/In Progress/Review/Done` at positions 0,1000,2000,3000,4000), and `loom init`/`board create` seeding all come from ADR-001 §3.3, §6.

## Where it lives

- `columns.stage` column (ADR-001 §3.3).
- Enforced by `store.MoveCard` / [BoardService](../modules/board.md) (`MoveCard` auto-kills via SessionManager).
- Triggered by TUI `m` key and `loom card move <id> <column>`.

## Related

- Concepts: [session](./session.md) · [run](./run.md)
- Architecture: [Data Model](../architecture/data-model.md)
- Modules: [board](../modules/board.md) · [store](../modules/store.md)
