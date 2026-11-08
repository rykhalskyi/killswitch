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

| Component      | Choice      |
| -------------- | ----------- |
| Language       | Go          |
| TUI            | Bubble Tea  |
| Kubernetes API | client-go   |
| Configuration  | YAML        |
| Journal        | SQLite      |
| Logging        | Go `slog`   |

## Planned project structure

```
kill-switch/
├── cmd/kill-switch/main.go
├── internal/
│   ├── tui/
│   ├── flux/          # client.go, reconcile.go, state.go
│   ├── journal/       # sqlite.go
│   └── config/        # config.go
├── config/config.yaml
└── go.mod
```

## Roadmap

1. **Phase 1 — Global Flux reconciliation toggle** (essential, no dependency)
2. **Phase 2 — Execution safety and recovery** (essential, needs Phase 1)
3. **Phase 3 — Kubernetes workload controls** (high, needs Phase 2)
4. **Phase 4 — Node management** (medium, needs Phase 2)
5. **Phase 5 — Network controls** (medium, needs Phase 2)
6. **Phase 6 — Workflow orchestration** (high, needs Phases 3–5)
7. **Phase 7 — Polish and distribution** (high, ongoing)

Phase 1 is the current scope. Keep it small; avoid abstractions for future switches until needed.

## Key concepts

- **Resource** — a configured target (Flux `Kustomization` / `HelmRelease`, later workloads, nodes, network rules) identified by kind, namespace, and name.
- **Operation** — a suspend or restore run over all configured resources, journaled in SQLite.
- **Original state** — the suspension/scale/replica state captured before an operation and used to restore.
- **Partial failure** — some resources succeeded and others did not; the global state must never be shown as fully suspended or active until every configured resource is verified.

See [build-deploy](build-deploy.md) for commands.
