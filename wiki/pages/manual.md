---
created: 2026-11-15
type: manual
summary: Comprehensive user manual and CLI/TUI operational guide for Kill Switch.
---

# Kill Switch Manual

This manual explains how to configure, operate, and troubleshoot Kill Switch (`killswitch`) for Phase 1 (Level 2 Flux freeze). For architecture principles, see [project-overview](project-overview.md); for compilation and tests, see [build-deploy](build-deploy.md).

---

## 1. Quick Reference

| Command | Purpose | Key Flags |
|---|---|---|
| `killswitch status` | View live cluster reconciliation state | `--json` |
| `killswitch suspend` | Freeze configured resources (`spec.suspend: true`) | `--dry-run`, `--yes`, `--force` |
| `killswitch restore` | Restore resources to recorded original states | `--dry-run`, `--yes`, `--force` |
| `killswitch history` | Inspect past journaled operations | `--limit N`, `--json` |
| `killswitch tui` | Interactive terminal UI | — |

Global flags for all commands:
- `--config <path>`: Config file path (defaults to `./config.yaml`, `./config.example.yaml`).
- `--context <name>`: Target Kubernetes context (e.g. `talos`).
- `--kubeconfig <path>`: Path to kubeconfig (defaults to `~/.kube/config` or `$KUBECONFIG`).
- `--timeout <dur>`: API request timeout (default `60s`).
- `--state-dir <path>`: State directory (defaults to `$XDG_STATE_HOME/killswitch` or `~/.local/state/killswitch`).
- `--log-level <lvl>`: `debug`, `info`, `warn`, `error` (default `info`).

---

## 2. Configuration (`config.yaml`)

Kill Switch targets resources explicitly declared in YAML. Order in configuration represents dependency order.

```yaml
flux:
  resources:
    # Kustomizations in dependency order
    - { kind: Kustomization, namespace: flux-system, name: flux-system }
    - { kind: Kustomization, namespace: flux-system, name: infrastructure }
    - { kind: Kustomization, namespace: flux-system, name: cert-manager-config }
    - { kind: Kustomization, namespace: flux-system, name: apps }

    # HelmReleases: infrastructure first, then workloads
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

Validation rules:
- Supported kinds: `Kustomization` and `HelmRelease`.
- Non-empty `namespace` and `name`.
- No duplicates.
- At least one resource required.

---

## 3. Workflows

### 3.1 Inspecting Cluster Status

Run `status` to inspect all configured resources:

```sh
killswitch status
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

Keybindings:
- `s` — Plan and prompt to **Suspend**
- `r` — Plan and prompt to **Restore**
- `d` — Open **Dry-run** modal
- `u` — Refresh status
- `y` / `n` — Confirm or cancel modal prompts
- `q` / `Ctrl+C` — Exit

---

### 3.5 Operation History

Query the write-ahead journal (`journal.jsonl`):

```sh
killswitch history --limit 10
killswitch history --json
```

Displays timestamp, operation ID, action, outcome status, targets count, OK count, failed count, and duration in milliseconds.

---

## 4. Safety & Recovery Guarantees

### 4.1 Write-Ahead Logging (`journal.jsonl`)
Located at `$XDG_STATE_HOME/killswitch/journal.jsonl` (mode `0600`):
- Every operation emits `op.start`.
- Prior to patching any resource, a `resource.intent` record is written and **fsynced** to disk.
- After patching and read-back verification, `resource.result` is appended.
- `op.end` records the final status (`success`, `partial`, or `failed`).
- Torn lines from mid-write process crashes are automatically detected and discarded on read.

### 4.2 State Snapshots (`state.json`)
- Derived atomic snapshot of current progress, recorded originals (`originalSuspended`), and sequence IDs.
- Written via temporary file, fsync, atomic rename, and directory fsync.
- Automatically reconstructed from `journal.jsonl` if missing, corrupted, or mismatched.

### 4.3 Lock Protection (`flock`)
- Kill Switch acquires an exclusive, non-blocking lock (`lock` file in state directory).
- Concurrent invocations immediately abort with exit code `4`.

### 4.4 Conflict Detection
- If a target resource's suspension state on the cluster changes out-of-band, Kill Switch detects a conflict.
- Execution halts with exit code `3`.
- Override intentional out-of-band changes by providing `--force`:
  ```sh
  killswitch restore --force
  ```

### 4.5 Interrupted Run Recovery
- If terminated mid-run, `status` surfaces the interrupted operation and affected resources.
- `killswitch suspend` resumes and completes remaining targets.
- `killswitch restore` reverts touched targets back to their original recorded baselines.

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
