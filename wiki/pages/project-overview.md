---
created: 2026-11-08
type: overview
summary: Kill Switch is a YAML-driven Go TUI for reversible homelab operations, starting with a Flux reconciliation toggle.
---

# Project Overview

**Kill Switch is a YAML-driven Linux TUI application for orchestrating operational changes in a homelab.** It executes workflows safely, records what changed, and restores the previous state when needed.

It is a specialized infrastructure operations controller, not a general-purpose task runner.

## Core principles

- **Declarative configuration** — workflows and targets live in YAML, not Go source.
- **Reversibility** — capture original state before changing anything; support undo.
- **State awareness** — know what changed, what succeeded, and what failed.
- **Extensibility** — add infrastructure technologies through action handlers.
- **Transparency** — preview operations, show progress, maintain a journal.
- **No infrastructure assumptions** — users define their own services, nodes, scripts.

## Technology stack

| Component      | Choice                              |
| -------------- | ----------------------------------- |
| Language       | Go                                  |
| TUI            | Bubble Tea + lipgloss               |
| Kubernetes API | client-go (`dynamic` + `unstructured`) |
| Configuration  | YAML                                |
| Journal        | `journal.jsonl` + atomic `state.json` (D-03) |
| Logging        | Go `slog`                           |

## Project structure

```
kill-switch/
├── cmd/killswitch/main.go
├── internal/
│   ├── cli/           # command parsing and output
│   ├── config/        # YAML loading and validation
│   ├── engine/        # planning, execution, recovery, conflicts
│   ├── flux/          # dynamic client for Flux CRDs
│   ├── journal/       # append-only JSONL journal
│   ├── state/         # state.json snapshots and flock
│   ├── tui/           # Bubble Tea views and overlay compositor
│   └── version/
├── config.example.yaml
├── rbac.example.yaml
└── go.mod
```

State lives under `$XDG_STATE_HOME/killswitch/` (`journal.jsonl`, `state.json`), never in the repo.

## Roadmap

1. **Phase 1 — Global Flux reconciliation toggle** (essential, no dependency)
2. **Phase 2 — Execution safety and recovery** (essential, needs Phase 1)
3. **Phase 3 — Kubernetes workload controls** (high, needs Phase 2)
4. **Phase 4 — Node management** (medium, needs Phase 2)
5. **Phase 5 — Network controls** (medium, needs Phase 2)
6. **Phase 6 — Workflow orchestration** (high, needs Phases 3–5)
7. **Phase 7 — Polish and distribution** (high, ongoing)

Phases 1–3 are implemented; see [T2](tickets/t2-phase2-safety-recovery.md) and [T3](tickets/t3-phase3-workload-controls.md). The TUI also ships a breadcrumb path and a confirm dialog composited over the dimmed table. Phase 3 adopts the switches → workflows → action-handler layering from the [extension architecture spec](specs/extension-architecture.md), with `flux.suspend`/`flux.resume`, `kubernetes.scale`, and the optional `kubernetes.suspend` handler. Phase 4 (node management) is next.

## Key concepts

- **Switch** — the user-visible control in the TUI. A switch is an operational state (ON/OFF), not a command; it must not reference Kubernetes, Flux, SSH, or scripts.
- **Workflow** — the ordered steps a switch runs in each direction, with declared prerequisites.
- **Action handler** — the Go capability that validates, captures, executes, verifies, and restores one step.
- **Resource** — a configured target (Flux `Kustomization` / `HelmRelease`, later workloads, nodes, network rules) identified by kind, namespace, and name.
- **Operation** — a switch transition run over its workflow steps, journaled in `journal.jsonl`.
- **Original state** — the per-target state captured before an operation and used to restore it; typed per action, not a single boolean.
- **Partial failure** — some steps succeeded and others did not; a switch must never show a clean ON/OFF state until every configured target is verified.

See [build-deploy](build-deploy.md) for commands.
