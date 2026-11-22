---
created: 2026-11-15
type: manual
summary: Comprehensive user manual and CLI/TUI operational guide for Kill Switch.
---

# Kill Switch Manual

This manual explains how to configure, operate, and troubleshoot Kill Switch (`killswitch`) for the Phase 1 Level 2 Flux freeze, the Phase 2 safety and recovery work, and the Phase 3 layered switches and workload controls. For architecture principles, see [project-overview](project-overview.md); for compilation and tests, see [build-deploy](build-deploy.md).

---

## 1. Quick Reference

| Command | Purpose | Key Flags |
|---|---|---|
| `killswitch status` | View switch and cluster state | `--switch ID`, `--json` |
| `killswitch <switch-id> on` | Restore originals / apply the ON state | `--dry-run`, `--yes`, `--force` |
| `killswitch <switch-id> off` | Capture originals / apply the OFF state | `--dry-run`, `--yes`, `--force` |
| `killswitch suspend` | Alias for `flux off` | `--dry-run`, `--yes`, `--force` |
| `killswitch restore` | Alias for `flux on` | `--dry-run`, `--yes`, `--force` |
| `killswitch validate` | Validate layered config without a cluster | — |
| `killswitch history` | Inspect past journaled operations | `--switch ID`, `--limit N`, `--json`, `--details` |
| `killswitch tui` | Interactive terminal UI | — |

Global flags for all commands:
- `--config <path>`: Config directory or switches YAML path (defaults to `./switches.yaml`, `./config.yaml`, or `~/.config/killswitch`).
- `--context <name>`: Target Kubernetes context (e.g. `talos`).
- `--kubeconfig <path>`: Path to kubeconfig (defaults to `~/.kube/config` or `$KUBECONFIG`).
- `--timeout <dur>`: API request timeout (default `60s`).
- `--state-dir <path>`: State directory (defaults to `$XDG_STATE_HOME/killswitch` or `~/.local/state/killswitch`).
- `--log-level <lvl>`: `debug`, `info`, `warn`, `error` (default `info`).

---

## 2. Configuration (`switches.yaml` + `workflows/`)

Configuration is layered: `switches.yaml` defines the user-visible controls, and each workflow file defines the ordered steps a switch runs.

```yaml
# switches.yaml
switches:
  - id: flux
    name: Flux Reconciliation
    description: Suspend or resume Flux reconciliation
    icon: ★
    workflow: flux
  - id: navidrome
    name: Navidrome
    description: Scale the Navidrome music server to zero (auto-suspends Flux)
    workflow: navidrome
  - id: cloudflared
    name: Cloudflared
    description: Scale Cloudflared to zero and close the tunnel (auto-suspends Flux)
    icon: 🌐
    workflow: cloudflared
  - id: cluster-shutdown
    name: Cluster Shutdown
    description: Power off 235 → 234 → 233 (one-way; no automatic restore)
    icon: ⏻
    workflow: cluster-shutdown
```

```yaml
# workflows/navidrome.yaml
id: navidrome
name: Navidrome
requires:
  flux: suspended
steps:
  - id: navidrome
    action: kubernetes.scale
    target: {kind: Deployment, namespace: navidrome, name: navidrome}
    states:
      off: {replicas: 0}
      on: {restore: previous, default: 1}
```

`icon` is optional; it renders in a fixed-width slot in the TUI, leaving blank space when unset.

Steps run in declaration order for ON and in reverse order for OFF. `requires.flux` accepts `suspended`, `active`, or `any` and creates a one-way Flux interlock (a dependent may suspend Flux before scaling down).

A workflow with `one_way: true` is a button: it has no ON side, always runs its steps in declaration order, and refuses ON transitions. One-way workflows cannot require Flux.

```yaml
# workflows/cluster-shutdown.yaml
id: cluster-shutdown
one_way: true
steps:
  - {id: 01-235, action: talos.shutdown, target: {kind: Node, name: "192.168.2.235"}}
  - {id: 02-234, action: talos.shutdown, target: {kind: Node, name: "192.168.2.234"}}
  - {id: 03-233, action: talos.shutdown, target: {kind: Node, name: "192.168.2.233"}}
```

`talos.shutdown` runs `talosctl shutdown --nodes <name> --endpoints <name>` and polls the Talos API port until the node is unreachable before the next step starts. It is idempotent and irreversible, requires `talosctl` on `PATH` with a valid `~/.talos/config`, and must run from outside the nodes being shut down. The CLI extends its default operation timeout for one-way workflows; an explicit `--timeout` must cover every node (roughly two minutes per node).

Supported actions:
- `flux.suspend` / `flux.resume` on `Kustomization` and `HelmRelease` (captures `spec.suspend`).
- `kubernetes.scale` on `Deployment` and `StatefulSet` (captures `spec.replicas`).
- `kubernetes.suspend` on `CronJob` (captures `spec.suspend`; optional).
- `talos.shutdown` on `Node` (captures reachability; irreversible).

Validation rules:
- Switch ids are unique and reference existing workflows.
- Workflow ids are unique; step ids are unique within a workflow.
- Action targets match the supported kinds. Non-Node targets need a non-empty namespace and name; Node targets use the address in `name` and have no namespace.
- `requires.flux` is `suspended`, `active`, or `any`; the Flux switch cannot require itself, and one-way workflows cannot require Flux.
- Legacy single-file `flux.resources` configs are rejected. Run `killswitch validate` to check a config without a cluster.

Originals are captured only on ON→OFF, never overwritten, and typed per action. An ON transition without a recorded original is refused unless `states.on.default` is set.

---

## 3. Workflows

### 3.0 Switch Transitions (`on` / `off`)

Any configured switch can be toggled by id:

```bash
killswitch navidrome off --dry-run
killswitch navidrome off --yes
killswitch navidrome on --yes
```

OFF captures the original state for every step (replicas or suspend flags) before applying the non-default state. ON restores those originals, or applies a fixed/default value when no original exists. If a workflow requires Flux suspended, turning it OFF first suspends Flux in the same operation; turning it back ON leaves Flux as-is. Toggling Flux never modifies dependent switches; Flux reconciliation is the source of truth for workload state.

A one-way switch (`one_way: true`) is a button, not a toggle: `ctrl+t` in the TUI or `killswitch <switch-id> off` runs it once in declaration order, and `on` is refused. Its `STATE` cell shows `▶ SHUTDOWN` when its targets are up, `○ DOWN` when they are all off, or `◐ PARTIAL` when mixed. A switch whose live probe fails shows `⚠ ERROR` (state `UNKNOWN`) while the other switches still render; the per-step error is available in the properties view.

`killswitch suspend` and `killswitch restore` remain aliases for the Flux switch (`flux off` / `flux on`).

### 3.1 Inspecting Cluster Status

Run `status` to inspect every switch, then the Flux resources:

```sh
killswitch status
killswitch status --switch navidrome
```

Output fields:
- `STATUS`: `ACTIVE` (reconciling), `SUSPENDED` (frozen), or `MISSING`.
- `ORIGINAL`: State recorded before the last freeze (`SUSPENDED` or `ACTIVE`).
- `READY`: Ready condition status and reason.
- `GEN/OBS`: `metadata.generation` vs `status.observedGeneration`. Flagged with `(!)` if controller reconciliation is lagging.
- `PHASE`: Active in-progress phase during operations or recovery (`in_progress`, `ok`, `error`).
- `Global State`:
  - `SUSPENDED`: All configured resources verified suspended.
  - `RESTORED`: All configured resources match recorded original baselines.
  - `PARTIAL`: Mixed state; requires intervention.

A switch whose live probe fails is reported as `UNKNOWN` with a `Warning:`/`Error:` line instead of failing the entire status query.

For programmatic consumption:
```sh
killswitch status --json
```

---

### 3.2 Freezing Flux (`suspend`)

Freezing sets `spec.suspend: true` across all configured targets.

1. **Always dry-run first**:
   ```sh
   killswitch suspend --dry-run
   ```
2. **Execute with confirmation prompt**:
   ```sh
   killswitch suspend
   ```
3. **Execute non-interactively**:
   ```sh
   killswitch suspend --yes
   ```

**Execution Order**:
To prevent dangling reconciliations, suspend runs in reverse dependency order:
1. Kustomizations in **reverse** config order (`apps` → `cert-manager-config` → `infrastructure` → `flux-system`).
2. HelmReleases in **reverse** config order (`langfuse` → ... → `local-path-provisioner`).

---

### 3.3 Restoring Flux (`restore`)

Restoring unfreezes resources back to their recorded original states.

1. **Dry-run**:
   ```sh
   killswitch restore --dry-run
   ```
2. **Execute**:
   ```sh
   killswitch restore
   ```

**Execution Order**:
To avoid Kustomizations with `wait: true` or health checks hanging while waiting for suspended Helm releases:
1. HelmReleases in **forward** config order (`local-path-provisioner` → ... → `langfuse`).
2. Kustomizations in **forward** config order (`flux-system` → `infrastructure` → `cert-manager-config` → `apps`).

---

### 3.4 Terminal User Interface (`tui`)

Launch the Bubble Tea interface:

```sh
killswitch tui
```

The switch list is loaded from `switches.yaml`. The panel title is a breadcrumb showing where you are, for example `Flux Controls › Flux Reconciliation › Properties` or `Flux Controls › Flux Reconciliation › Operation History`. The properties view lists workflow steps with their observed state, original value, and action.

The confirmation dialog is composited over the dimmed switch table, so the underlying view stays visible while the pending change is reviewed.

Keybindings:
- `↑` / `↓` (or `k` / `j`) — Select a switch, or scroll properties, the YAML view, history, and history details
- `Enter` — Open the selected switch's properties, or the selected history operation's step details
- `y` (in properties) — Show the selected switch's effective YAML; scroll with `↑`/`↓`, `g`/`G`, or `PgUp`/`PgDn`, `Esc` to go back
- `Esc` — Return from properties, YAML, history details, or history to the previous screen
- `Ctrl+T` (or `t` / `Space`) — Toggle the selected switch; a "building switch plan" dialog appears immediately, then the confirmation modal once the plan is ready
- `h` — Open the operation history view (`Enter` there opens step details)
- `l` (in history or step details) — Show the raw `journal.jsonl` records for the selected operation; scroll with `↑`/`↓`, `g`/`G`, or `PgUp`/`PgDn
- `y` / `Enter` (in the confirmation dialog) — Confirm the pending change
- `f` — Confirm with force override when the plan shows conflicts
- `n` / `Esc` — Cancel the pending change
- `r` / `u` — Refresh live status (and history)
- `q` / `Ctrl+C` — Exit

---

### 3.5 Operation History

Query the write-ahead journal (`journal.jsonl`) from the CLI or TUI:

```sh
killswitch history --limit 10
killswitch history --switch navidrome
killswitch history --details
killswitch history --json
```

Each row shows the time, switch name, direction (`ON`, `OFF`, or `ONE-WAY`), result status, completed/target steps, and duration. `--details` expands every operation into its workflow steps with the action, target, result, original/desired/observed values, and errors. `--json` includes the same per-step records.

In the TUI, press `h` to open the same history view; `↑`/`↓` selects an operation, `Enter` opens its step details, `l` opens the raw journal records for the selected operation (scrollable with `↑`/`↓`, `g`/`G`, `PgUp`/`PgDn`), and `Esc` returns. Both read `Engine.History`, so the CLI and TUI always agree.

---

## 4. Safety & Recovery Guarantees

### 4.1 Write-Ahead Logging (`journal.jsonl`)
Located at `$XDG_STATE_HOME/killswitch/journal.jsonl` (mode `0600`):
- Every operation emits `op.start`.
- Prior to patching any resource, a `resource.intent` record is written and **fsynced** to disk.
- After patching and read-back verification, `resource.result` is appended.
- `op.end` records the final status (`success`, `partial`, or `failed`).
- Journal v2 events carry `switch`, `workflow`, `step`, `direction`, `original`, `desired`, `observed`, and `phase`. v1 journals still replay and continue under v2.
- Torn lines from mid-write process crashes are automatically detected and discarded on read.

### 4.2 State Snapshots (`state.json`)
- Derived atomic snapshot of current progress, recorded originals (`originalSuspended` for Flux and typed `originalValues` per workflow step), and sequence IDs.
- Written via temporary file, fsync, atomic rename, and directory fsync.
- Automatically reconstructed from `journal.jsonl` if missing, corrupted, or mismatched.

### 4.3 Lock Protection (`flock`)
- Kill Switch acquires an exclusive, non-blocking lock (`lock` file in state directory).
- Concurrent invocations immediately abort with exit code `4`.

### 4.4 Conflict Detection
- Kill Switch compares each step's observed value (`spec.suspend` or `spec.replicas`) with the state it expects from the last successful operation and the recorded originals.
- Drift is detected in both directions: external un-suspend after a freeze and external suspend after a restore.
- Conflicts are journaled with the real `expected` and `observed` values.
- Execution halts with exit code `3`.
- Override intentional out-of-band changes by providing `--force`, or by pressing `f` in the TUI confirmation modal:
  ```sh
  killswitch restore --force
  ```

### 4.5 Interrupted Run Recovery
- If terminated mid-run, `status` surfaces the interrupted operation and its per-resource phases.
- Recovery is scoped to the steps recorded in `ActiveOp.StepStates` (with legacy `ResourceStates` replay for v1 journals). Originals are never recaptured during recovery.
- `killswitch suspend` completes only the targets the interrupted suspend had not verified as suspended.
- `killswitch restore` reverts only the targets the interrupted operation actually touched, using their recorded originals.
- Resources outside the interrupted operation are left untouched.

---

## 5. Exit Codes

| Code | Meaning |
|---|---|
| `0` | Success: all targets reconciled and verified. |
| `1` | Partial failure: one or more resources failed patching or read-back. |
| `2` | Config or connection error: invalid YAML or unreachable cluster. |
| `3` | Conflict: out-of-band change detected; requires `--force`. |
| `4` | Unsafe state or lock contention: concurrent process or missing originals. |

---

## 6. RBAC Configuration

When running under a restricted ServiceAccount instead of `cluster-admin`, apply `rbac.example.yaml`:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: killswitch-flux-freeze
rules:
  - apiGroups: ["kustomize.toolkit.fluxcd.io"]
    resources: ["kustomizations"]
    verbs: ["get", "list", "patch"]
  - apiGroups: ["helm.toolkit.fluxcd.io"]
    resources: ["helmreleases"]
    verbs: ["get", "list", "patch"]
```
