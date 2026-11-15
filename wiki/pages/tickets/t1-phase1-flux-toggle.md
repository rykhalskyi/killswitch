---
created: 2026-11-08
type: ticket
status: implemented
summary: Phase 1 plan for a Go CLI/TUI that suspends and restores all configured Flux Kustomizations and HelmReleases with JSON journaling and crash recovery.
---

# T1 — Phase 1: Level 2 Flux freeze

Implements the first milestone from the [project overview](../project-overview.md). Build and test commands will land in [build-deploy](../build-deploy.md) once the Go module exists.

## Spec

Build a single-binary Go tool (CLI-first, thin Bubble Tea TUI) that performs a **Level 2 Flux freeze**: suspend/restore all configured Flux `Kustomization` and `HelmRelease` resources, capture original states, journal every operation, verify results, and never report a clean global state unless every configured resource matches.

Decisions: [D-03](../../decisions.md) JSON journal, [D-04](../../decisions.md) Level 2 scope, [D-05](../../decisions.md) dynamic client, [D-06](../../decisions.md) CLI-first, [D-07](../../decisions.md) Go 1.26, [D-08](../../decisions.md) no GitRepository.

### Locked decisions

| Area | Decision |
|---|---|
| Scope | Level 2: 4 Kustomizations + 11 HelmReleases |
| Language | Go 1.26.10 (install from Fedora) |
| Kubernetes access | `client-go/dynamic` + `unstructured` (no Flux Go modules) |
| Interface | CLI first; thin Bubble Tea TUI over the same engine |
| Persistence | `journal.jsonl` = source of truth; `state.json` = rebuildable snapshot |
| Database | No SQLite |
| Sources | No `GitRepository` handling in v1 |
| Config | Explicit YAML resource list |

### Prerequisite

Go is not installed. Fedora 44 provides `golang 1.26.10`:

```sh
sudo dnf install -y golang
go version
```

Requires sudo. Module path: `github.com/rykhalskyi/killswitch`.

### Non-goals for Phase 1

- Scaling workloads, SSH/WoL, network isolation, workflow orchestration.
- Stopping Kubernetes controllers/operators (Kill Switch freezes Flux, not Kubernetes).
- Helm uninstall/install.
- Multi-cluster support or concurrent operators.
- Waiting for full workload readiness after restore (readiness is displayed, not required).

### Default target set

Config order is dependency order; the engine uses it for restore and reverses it for suspend.

**Kustomizations** (`flux-system`): `flux-system`, `infrastructure`, `cert-manager-config`, `apps`.

**HelmReleases** (infrastructure first, then apps):

| Namespace | Name |
|---|---|
| `local-path-storage` | `local-path-provisioner` |
| `traefik` | `traefik` |
| `monitoring` | `kube-prometheus-stack` |
| `cnpg-system` | `cloudnative-pg` |
| `metrics-server` | `metrics-server` |
| `keda` | `keda` |
| `cert-manager` | `cert-manager` |
| `nextcloud` | `nextcloud` |
| `homepage` | `homepage` |
| `transcributed` | `transcributed` |
| `langfuse` | `langfuse` |

```yaml
# config.example.yaml
flux:
  resources:
    - { kind: Kustomization, namespace: flux-system, name: flux-system }
    - { kind: Kustomization, namespace: flux-system, name: infrastructure }
    - { kind: Kustomization, namespace: flux-system, name: cert-manager-config }
    - { kind: Kustomization, namespace: flux-system, name: apps }
    - { kind: HelmRelease, namespace: local-path-storage, name: local-path-provisioner }
    - { kind: HelmRelease, namespace: traefik, name: traefik }
    - { kind: HelmRelease, namespace: monitoring, name: kube-prometheus-stack }
    - { kind: HelmRelease, namespace: cnpg-system, name: cloudnative-pg }
    - { kind: HelmRelease, namespace: metrics-server, name: metrics-server }
    - { kind: HelmRelease, namespace: keda, name: keda }
    - { kind: HelmRelease, namespace: cert-manager, name: cert-manager }
    - { kind: HelmRelease, namespace: nextcloud, name: nextcloud }
    - { kind: HelmRelease, namespace: homepage, name: homepage }
    - { kind: HelmRelease, namespace: transcributed, name: transcributed }
    - { kind: HelmRelease, namespace: langfuse, name: langfuse }
```

Validation: known kinds only, non-empty name/namespace, no duplicates, at least one resource, GVR discoverable, target existence reported by `status` (missing targets are a hard error at execution time).

## Plan

### Layout

```
cmd/killswitch/main.go
internal/cli/         # commands, flags, exit codes, output
internal/config/      # YAML load + validation
internal/flux/        # dynamic client, GVR discovery, get/patch/read-back
internal/engine/      # plan, ordering, suspend/restore, partial results
internal/journal/     # JSONL append/fsync/replay, torn-line handling
internal/state/       # atomic state.json, lock, rebuild from journal
internal/tui/         # Bubble Tea view over engine
config.example.yaml
```

Dependencies: `k8s.io/client-go`, `k8s.io/apimachinery`, `gopkg.in/yaml.v3`, `charmbracelet/bubbletea`, `charmbracelet/lipgloss`. Standard library for JSON, `slog`, and `flock`.

### Journal and state

Location: `$XDG_STATE_HOME/killswitch/` (fallback `~/.local/state/killswitch/`), mode `0700`; files `0600`.

`journal.jsonl` is an append-only write-ahead log, one event per line, newline as commit marker:

```json
{"v":1,"seq":1,"ts":"2026-11-08T18:00:00Z","op":"01J...","event":"op.start","action":"suspend","context":"talos","targets":15}
{"v":1,"seq":2,"ts":"...","op":"01J...","event":"resource.intent","kind":"Kustomization","namespace":"flux-system","name":"apps","from":false,"to":true}
{"v":1,"seq":3,"ts":"...","op":"01J...","event":"resource.result","kind":"Kustomization","namespace":"flux-system","name":"apps","status":"ok","observed":true,"durationMs":42}
{"v":1,"seq":4,"ts":"...","op":"01J...","event":"conflict","kind":"HelmRelease","namespace":"monitoring","name":"kube-prometheus-stack","expected":true,"observed":false}
{"v":1,"seq":5,"ts":"...","op":"01J...","event":"op.end","status":"partial","ok":14,"failed":1}
```

- `resource.intent` is appended and `fsync`ed **before** patching the cluster.
- On read, ignore a trailing line without a newline (torn write).
- No rotation in Phase 1; `history` filters in memory.

`state.json` is a derived snapshot, atomically written (temp → fsync → rename → fsync dir):

- active operation and per-resource phase;
- recorded originals (`originalSuspended`);
- `lastSeq` and last completed operation.

On startup: lock, load `state.json`, compare `lastSeq` with the journal; replay the journal if missing/corrupt/mismatched. An `intent` without `result` is an incomplete operation. `status` shows it; `suspend` finishes it, `restore` undoes it.

**Original-state rule**

- `suspend` records current `spec.suspend` as the original.
- If already suspended, keep an existing original; if none exists (suspended outside the tool), record `true`.
- `restore` sets each resource to its recorded original, so externally suspended resources stay suspended.
- A resource changed outside Kill Switch is a `conflict`; require `--force`.

**Locking:** `flock` on `lock`; released automatically on exit. Concurrent invocation fails with exit code 4.

### Operation flow

**Suspend**

1. Lock; load config; discover GVRs; recover state.
2. Read current `spec.suspend` for all targets.
3. Build plan: originals and targets; print it.
4. `--dry-run` stops here.
5. Confirm unless `--yes`.
6. Append `op.start`.
7. Suspend Kustomizations in reverse config order: `apps`, `cert-manager-config`, `infrastructure`, `flux-system`.
8. Suspend HelmReleases in reverse config order.
9. For each: append `intent` + fsync → patch `spec.suspend: true` → read back → append `result` → rewrite `state.json`.
10. Append `op.end`; derive global state from actual read-backs.

**Restore** (reverse order to avoid `wait: true` hanging on suspended HelmReleases)

1–6 as above.
7. Restore HelmReleases in forward config order (infrastructure, then apps).
8. Restore Kustomizations in forward config order: `flux-system`, `infrastructure`, `cert-manager-config`, `apps`.
9. Same per-resource intent/patch/read-back/result/snapshot sequence.
10. Append `op.end`.

**Verification**

- Success criterion: read-back `spec.suspend` equals the target.
- Also record `metadata.generation`, `status.observedGeneration`, and Ready condition. Lagging controller observation is a warning, not a failure.
- Global state: `suspended` only if all targets are `true`; `restored` only if all match their originals; otherwise `partial`. Never show a clean state otherwise.

**Partial failure**

- Attempt every resource, collect results, report per-resource errors.
- `op.end`: `success` (all), `partial` (some), `failed` (none/aborted).
- Exit 1 on partial/failed; never silently treat partial success as complete.

**Interrupted run**

- `status` detects the active operation and shows per-resource phase.
- `suspend` can complete it; `restore` can undo touched resources.
- If originals are missing and the operation is incomplete, refuse with a clear message.

### CLI

```
killswitch status   [--json]
killswitch suspend  [--dry-run] [--yes] [--force]
killswitch restore  [--dry-run] [--yes] [--force]
killswitch history  [--limit N] [--json]
killswitch tui
```

Global flags: `--config`, `--context`, `--kubeconfig`, `--timeout`, `--state-dir`, `--log-level`.

Exit codes: `0` success, `1` partial failure, `2` config/connection error, `3` conflict/needs `--force`, `4` unsafe/incomplete state or lock contention.

Logging: `slog` to stderr; no secrets in logs or journal.

### TUI

Single screen over the engine: global status banner, resource table (kind, namespace, name, actual, original, result), and footer actions (`s` suspend, `r` restore, `d` dry-run, `q` quit). Confirmation modal shows the plan. Partial failures render a red banner; green only when every resource matches. No direct API calls from the TUI.

### Testing

- `journal`: torn trailing line, fsync/write order, replay.
- `state`: atomic write, missing/corrupt state, seq mismatch, rebuild.
- `engine` with a fake resource client: ordering, idempotency, partial failure, conflicts, already-suspended originals, incomplete-operation resume/undo, lock contention.
- `config`: validation cases.
- `flux`: read-back and GVR discovery (fake dynamic client).
- Manual smoke test on `talos`: `status` → `suspend --dry-run` → `suspend` → `status` → `restore` → `status`.
- Optional integration test against a kind cluster with Flux installed.

### Implementation milestones

0. Install Go; `go mod init github.com/rykhalskyi/killswitch`.
1. `internal/config` + `internal/journal` + `internal/state` with tests.
2. `internal/flux` dynamic client + GVR discovery.
3. `status` command.
4. `engine` plan + `suspend --dry-run`.
5. Suspend execution + verification + partial failure + exit codes.
6. Restore + conflict detection + interrupted-run recovery.
7. `history`.
8. TUI.
9. Tests, `config.example.yaml`, README, least-privilege RBAC example, release build.

### Acceptance criteria

- Connect using the current kubeconfig.
- Load configured Flux resources from YAML.
- Display actual reconciliation status.
- Suspend all configured resources.
- Persist original states in the JSON journal/state.
- Restore original states.
- Recover safely if the application exits midway.
- Report partial failures without treating them as complete.
- No clean global state until every configured resource is verified.
- Level 2 coverage: 4 Kustomizations + 11 HelmReleases.

### Risks / notes

- Go install needs sudo and network access.
- `infrastructure`/`apps` use `wait: true`; restore order avoids health-check hangs.
- `restore` verifies suspend flags, not full workload health; `status` surfaces readiness asynchronously.
- External changes are conflicts, never silently overwritten.
- RBAC needs `get/list/patch` on both Flux CRDs; document a ClusterRole, though daily use can rely on the current cluster-admin kubeconfig.

## Outcome

Implemented Phase 1 Level 2 Flux freeze.

Shipped:
- Go module `github.com/rykhalskyi/killswitch`
- `internal/config`: YAML loading and validation for target resources
- `internal/journal`: Append-only write-ahead log (`journal.jsonl`) with pre-patch `fsync` and torn trailing line discard
- `internal/state`: Atomic `state.json` snapshots and non-blocking `flock` lock file
- `internal/flux`: Dynamic client (`k8s.io/client-go/dynamic`) with automated GVR discovery, `spec.suspend` patching, and live read-back verification
- `internal/engine`: Dependency-ordered execution (reverse suspend, forward restore), verification, partial failure handling, conflict detection, history aggregation
- `internal/cli`: Full CLI subcommands (`status`, `suspend`, `restore`, `history`, `tui`) with dry-run, json output, confirmation prompts, and standard exit codes (0, 1, 2, 3, 4)
- `internal/tui`: Bubble Tea terminal interface with status banners and modal confirmations
- Target configuration in `config.example.yaml` (4 Kustomizations + 11 HelmReleases)
- Least-privilege RBAC definitions in `rbac.example.yaml`
- User guide in `README.md`
- Comprehensive unit tests across all packages (`config`, `journal`, `state`, `flux`, `engine`)
