---
created: 2026-11-17
type: spec
status: proposed
summary: Extension architecture for Phases 3–6 — switches, workflows, and state-aware Go action handlers, with gap resolutions and tradeoffs.
---

# Extension architecture — switches, workflows, action handlers

Cross-links: [T3](../tickets/t3-phase3-workload-controls.md) · [project overview](../project-overview.md) · decisions [D-11](../../decisions.md).

## Context

Today Kill Switch is a single-purpose Flux freeze tool: one hardcoded switch (`switchDefs` in `internal/tui/model.go`), a Flux-only engine, and a config with only `flux.resources` of `Kustomization`/`HelmRelease`. Phases 3–6 add workloads, nodes, network, and orchestration. This spec defines the layering and the state/safety model those phases build on.

Goals:

- Switches are operational states, not commands; the UI never references infrastructure.
- Workflows compose typed actions; new infrastructure is added as a Go handler, not UI or engine branches.
- Every action is reversible via a captured original state, or explicitly declared irreversible.
- Phases 1–2 guarantees carry over unchanged: pre-apply journal+fsync, read-back verification, expected-vs-observed conflicts, scoped resume/undo, single-writer lock.

Non-goals: parallel step execution, multi-cluster, arbitrary manifest apply, GUI.

## Layering

| Layer | File | Responsibility |
|---|---|---|
| Switches | `switches.yaml` | TUI labels, descriptions, workflow references |
| Workflows | `workflows/*.yaml` | Ordered steps, prerequisites, per-position desired states |
| Actions | Go handlers | Validate, observe, capture, apply, verify, restore |
| Scripts | referenced files | Custom operations outside native handlers |
| Journal/state | `journal.jsonl`, `state.json` | Originals, history, recovery |
| UI | `internal/tui` | Render switches/steps, trigger transitions, show progress |

### Switches

```yaml
# switches.yaml
switches:
  - id: flux
    name: Flux Reconciliation
    description: Suspend or resume Flux reconciliation
    workflow: flux
  - id: media
    name: Media services
    description: Control media applications
    workflow: media
```

### Workflows (canonical schema)

One definition per target, with a desired state per switch position. This replaces the older duplicated `on:`/`off:` blocks.

```yaml
# workflows/media.yaml
id: media
name: Media services
requires:
  flux: suspended        # suspended | active | any

steps:
  - id: jellyfin
    action: kubernetes.scale
    target: {kind: Deployment, namespace: media, name: jellyfin}
    states:
      off: {replicas: 0}
      on:  {restore: previous}

  - id: sonarr
    action: kubernetes.scale
    target: {kind: Deployment, namespace: media, name: sonarr}
    states:
      off: {replicas: 0}
      on:  {restore: previous}
```

- `steps` are executed in order for ON and in reverse order for OFF.
- `states.on.restore: previous` restores the original captured on the last ON→OFF transition.
- `states.on.replicas: N` / `states.off.replicas: N` set a fixed desired state instead of restoring.
- Switch ON is the operational default; OFF is the non-default state where capture happens.

## Handler contract

Actions are Go capabilities referenced by name. They are not configured in YAML beyond their parameters.

```
Validate(step) error
Observe(ctx, step) (Observed, error)              // live state for display + conflict checks
Capture(ctx, step) (Original, error)              // only on ON→OFF (or first OFF)
Plan(ctx, step, observed, original) (Item, error) // dry-run, desired state, conflicts
Apply(ctx, item) (Observed, error)
Verify(ctx, item) error                           // optional; ErrUnverifiable allowed
Restore(ctx, item, original) (Observed, error)    // for ON
```

Every step result is journaled as intent (before apply) and result (after read-back), keyed by workflow, step, and target. `Verify` failing leaves the switch non-clean.

### Action catalog

| Action | Handler | Captured original | Reversible | Phase |
|---|---|---|---|---|
| `flux.suspend` / `flux.resume` | Flux (port current engine) | `spec.suspend` bool | yes | 3 |
| `kubernetes.scale` | Kubernetes dynamic | `spec.replicas` | yes | 3 |
| `kubernetes.suspend` (`CronJob`) | Kubernetes dynamic | `spec.suspend` bool | yes | 3 optional |
| `node.cordon` / `uncordon` / `drain` | Kubernetes node API | schedulable + taints | yes | 4 |
| `node.shutdown` / `reboot` | SSH | desired power / reachability | partial | 4 |
| `node.wake` | Wake-on-LAN | none | best-effort | 4 |
| `network.policy` | Network provider | policy/ruleset | provider-dependent | 5 |
| `script.run` | Shell | none; requires `undo` | only with `undo` | 5–6 |
| `kubernetes.patch` (generic) | Kubernetes dynamic | arbitrary field | yes | defer |

## State model

- **Switch state** is derived from live observation of every step, not from the last journal entry: `ON`, `OFF`, `MIXED`, `DRIFT`, `MISSING`, `UNKNOWN`. This preserves the T2 lesson that journal-only state cannot detect out-of-band change.
- **Original state** comes from the journal and is typed per action (`bool`, `int`, node mode, script marker), replacing `OriginalSuspended map[string]bool`.
- **Expected state** for conflict detection is derived from the last successful transition plus recorded originals, as in Phase 2, generalized per step.
- **Operation** = one switch transition. Recovery resumes the unfinished steps of that transition in the same direction; restoring the opposite direction undoes only touched steps.

## Safety invariants (carried from T2)

- Journal intent + `fsync` before any apply.
- Read-back verification; the switch is not clean until every step matches its desired/restored state.
- Conflicts compared in both directions and journaled with real expected/observed values.
- Interrupted operations resume only touched steps; never recapture originals mid-recovery.
- One global lock (single writer) per state dir.

## Gap resolutions

Resolved decisions: [D-12…D-17](../../decisions.md).

| Gap | Decision | Tradeoff |
|---|---|---|
| Config layering | Separate `switches.yaml` + `workflows/*.yaml`, loaded from a config dir; cross-reference validation | more files and validation |
| Config compatibility | Require the layered format; no legacy `flux.resources` synthesis (no user configs exist yet) | a config rewrite when adopting T3 |
| Switch state | Hybrid: live observation for display, journaled originals for expected/restore | handlers must implement `Observe` |
| Capture timing | Capture only on ON→OFF; never overwrite an existing capture; `--recapture` to reset intentionally | stale original if infra changes while OFF |
| Missing original on ON | Refuse with exit 4 unless `states.on.default` is configured | first-run restores may need a default |
| Resume/partial failure | Resume unfinished steps; never recapture | manual follow-up on partial failure |
| Mid-workflow failure | Stop by default; per-workflow `on_error: rollback` opt-in | rollback must itself be journaled and resumable |
| Prerequisites | Two-way interlock: turning a dependent OFF auto-satisfies the prerequisite; moving the prerequisite out of that state runs dependent OFF workflows first | cascades change other switches and can be destructive; always previewed |
| Cascade failure | Continue the prerequisite transition; Flux reconciles dependents; journal the failure and surface MIXED/DRIFT | assumes the dependent's target state is represented in Git/Flux |
| Irreversible actions | Require `reversible: false`; `script.run` also needs `undo` to be restorable | some actions can never be restored |
| Verification | Optional `Verify`; unverifiable steps render `UNVERIFIED` | not all infrastructure is observable |
| Typed state | Journal v2 with generic `original`/`desired`/`observed` JSON; keep v1 replay | migration and replay work |
| Persistence | Keep `journal.jsonl` + `state.json` (D-03) | no SQL queries; fine at homelab scale |
| Locking | Keep one global lock for now | long workflows block other switches |
| Secrets | Environment or external credential helper only | extra setup for SSH/network handlers |

### Dependency interlock semantics (D-15, D-17)

`requires` is declared on the workflow (`requires: {flux: suspended|active|any}`) and creates a switch-level interlock.

Forward direction — turning a dependent OFF:

- A dependent switch may be OFF (non-default state) only while the prerequisite switch is in the required state (`flux: suspended` means the Flux switch is OFF).
- Turning the dependent OFF first transitions the prerequisite if needed, journaling it in the same operation, then runs the dependent's steps.
- Prerequisites are not auto-reverted: turning the dependent back ON leaves Flux as-is.

Reverse direction — moving the prerequisite:

- Turning the prerequisite out of the required state (Flux OFF → ON) first runs the OFF workflow of every currently-ON dependent that requires the state being left (e.g. `flux: suspended` when Flux turns ON), in `switches.yaml` order, then resumes Flux.
- This makes the interlock two-way: reconciliation cannot resume while a dependent is still in its manual state.
- If a dependent's OFF workflow fails, the Flux transition still proceeds. The failure is journaled and the switch shows MIXED/DRIFT; Flux reconciliation is expected to converge the dependent. This assumes the dependent's target state is represented in Git/Flux; otherwise it can remain inconsistent.
- If the prerequisite transition (Flux ON) itself fails, the operation aborts.

Shared rules:

- The whole cascade is one operation with per-switch and per-step phases. Recovery resumes unfinished cascade steps in the same direction and never recaptures originals.
- Dry-run and the confirmation modal list the primary transition and every cascaded transition before execution.
- An out-of-band prerequisite change cannot trigger a cascade; `status` reports the dependency violation and the user can run the repair.
- Cycles are rejected at config validation.
- Phase 3 implements the Flux special case only (one level); generic `depends_on` graphs land in Phase 6.

## Phased delivery

- **Phase 3** — adopt the layered config and canonical states schema; introduce the handler registry and typed originals/journal v2; implement `kubernetes.scale`; port Flux to `flux.suspend`/`flux.resume`; implement the Flux dependency interlock in both directions (dependent OFF suspends Flux; Flux ON runs dependent OFF workflows first); stop on the first failed primary step. Multi-step workflows allowed sequentially; the `on_error: rollback` policy, generic `depends_on`, and multi-switch ordering land in Phase 6.
- **Phase 4** — node handlers (cordon/drain/reboot) and SSH/WoL transport.
- **Phase 5** — `network.policy` and `script.run` with explicit `undo` and a script trust model.
- **Phase 6** — orchestration: `depends_on`, multi-switch prerequisite ordering, rollback policies, richer resume.
- **Phase 7** — polish and distribution.
