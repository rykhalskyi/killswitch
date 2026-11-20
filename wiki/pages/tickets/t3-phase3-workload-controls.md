---
created: 2026-11-17
type: ticket
status: implemented
summary: Phase 3 — adopt layered switch/workflow config and action handlers, then implement Kubernetes workload scaling.
---

# T3 — Phase 3: Kubernetes workload controls

Follow-up to [T2](t2-phase2-safety-recovery.md) and roadmap item 3 in the [project overview](../project-overview.md). Implements the Phase 3 slice of the [extension architecture spec](../specs/extension-architecture.md); decisions [D-11…D-17](../../decisions.md).

## Spec

Adopt the switches → workflows → action-handler layering now, with a deliberately small first handler set.

In scope:

1. **Layered configuration.** Add `switches.yaml` and `workflows/*.yaml` with cross-reference validation (`killswitch validate`). The layered format is required from day one; there are no existing user configs to preserve.
2. **Handler contract.** Introduce a handler registry and the `Validate → Observe → Capture → Plan → Apply → Verify → Restore` lifecycle. Refactor the current Flux engine behind `flux.suspend`/`flux.resume` without changing behavior.
3. **Typed original state.** Replace `OriginalSuspended map[string]bool` and the journal's boolean `from/to/observed` with journal v2 payloads (`action`, `workflow`, `step`, `original`, `desired`, `observed`, `phase`). Keep reading v1 journals so existing state dirs replay.
4. **Workload scaling.** Implement `kubernetes.scale` for `Deployment` and `StatefulSet` using the dynamic client: observe `spec.replicas`, capture it on ON→OFF, set the OFF target, restore it on ON.
5. **Switch state.** Derive each switch's display state from live observation of its steps (`ON`, `OFF`, `MIXED`, `DRIFT`, `MISSING`, `UNKNOWN`), using journaled originals for expected/restore values. Reuse the T2 conflict, recovery, lock, breadcrumb, history, and overlay behavior.
6. **Dependency interlock.** Evaluate `requires.flux` before the first step. Turning a dependent ON auto-suspends Flux when needed. Turning Flux ON first runs the OFF workflow of every currently-ON dependent (in `switches.yaml` order), then resumes reconciliation. The whole cascade is journaled as one operation (D-15, D-17).
7. **CLI/TUI.** `killswitch <switch-id> on|off`, `status --switch`, and `history --switch`; keep `suspend`/`restore` as aliases for the Flux switch. The TUI list is loaded from config and the detail view lists workflow steps and their observed state.

Out of scope: generic `depends_on` graphs, beyond-Flux ordering, rollback policy, node/network/script handlers, parallel steps, arbitrary manifest apply.

Acceptance criteria:

- A two-step media workflow scales Deployments to 0 and restores the captured replica counts, including after a restart.
- External replica changes between OFF and ON surface as conflicts and are journaled with real expected/observed values.
- An interrupted scale step resumes only unfinished steps and never recaptures originals.
- `requires.flux: suspended` auto-suspends Flux first, journals it under the same operation, and leaves the Flux switch OFF.
- Turning Flux ON while a dependent is ON runs that dependent's OFF workflow first, then resumes Flux; a failed dependent OFF does not block Flux and is surfaced as MIXED/DRIFT.
- Dry-run and the confirmation dialog list the primary transition and every cascaded transition.
- Journal v1 state dirs replay and continue under v2.
- `go test ./...`, `go vet ./...`, `gofmt -l .`, and `node scripts/wiki.mjs lint` pass.

Decisions are resolved in [D-12…D-17](../../decisions.md): capture only on ON→OFF, refuse ON without an original unless a default is set, stop on mid-workflow failure with opt-in rollback, and enforce the two-way Flux interlock.

## Plan

- `internal/config`: add switch/workflow types, multi-file loader, and reference validation; update `Validate`.
- `internal/journal`, `internal/state`: journal v2 event fields and typed originals; v1 read/replay compatibility; recovery keyed by workflow step.
- `internal/actions`: handler registry + interface; `flux` handler wrapping the current engine; `kubernetes.scale` handler.
- `internal/engine`: generalize plan/execute/verify/status/conflict/recovery around steps and typed state; add forward prerequisite transitions and reverse dependency cascades.
- `internal/tui`: load switches from config; render list/detail from steps; keep breadcrumb, overlay, history.
- `internal/cli`: on/off/status/history by switch; validate subcommand; Flux aliases.
- Tests: scaling, conflict, resume, forward prerequisite auto-toggle, reverse cascade ordering and non-blocking failure, v1 replay, config validation, TUI rendering.
- Docs: manual config/workflow sections, README, and this ticket's Outcome.

Risks: journal schema migration; config rewrite; engine/TUI refactor breadth. Mitigate with v1 replay tests and a config validation suite.

## Outcome

Implemented on `feature/phase3-workload-controls`.

- `internal/config`: layered `switches.yaml` + `workflows/*.yaml` loader, reference/action validation, legacy programmatic flux normalization, and `killswitch validate`.
- `internal/actions`: handler registry and lifecycle, `flux.suspend`/`flux.resume`, `kubernetes.scale` (Deployment/StatefulSet), optional `kubernetes.suspend` (CronJob), plus a dynamic-client implementation.
- `internal/journal`, `internal/state`: journal v2 typed `original`/`desired`/`observed` fields keyed by switch/workflow/step, typed `originalValues`, `StepStates` recovery, and v1 replay support.
- `internal/engine`: generalized plan/execute/status around workflow steps, live switch states, typed conflicts, stop-on-first-step-failure, forward prerequisite auto-toggle, and the reverse Flux-dependent cascade journaled as one operation.
- `internal/cli`, `internal/tui`: `<switch-id> on|off`, `status --switch`, `history --switch`, `validate`, Flux aliases, config-loaded switch list, and step-based properties view.
- Tests cover scaling, originals restore, conflicts, interrupted resume without recapture, forward/reverse interlock ordering, v1 replay, config validation, and Kubernetes fake-client scaling. `go test ./...`, `go vet ./...`, `gofmt -l .` pass. A live-cluster smoke test remains pending until the homelab cluster is online.
