# Kill Switch (`killswitch`)

**Kill Switch** is a single-binary, reversible infrastructure operations controller for Kubernetes and homelab clusters. 

Phase 1 implements a **Level 2 Flux Freeze**: suspending and restoring configured Flux `Kustomization` and `HelmRelease` resources with strict dependency ordering, write-ahead JSON journaling, atomic crash recovery, and both CLI and interactive TUI interfaces.

---

## Table of Contents

- [Features](#features)
- [Installation & Build](#installation--build)
- [Configuration](#configuration)
- [Commands & Usage](#commands--usage)
  - [`killswitch status`](#killswitch-status)
  - [`killswitch suspend`](#killswitch-suspend)
  - [`killswitch restore`](#killswitch-restore)
  - [`killswitch history`](#killswitch-history)
  - [`killswitch tui`](#killswitch-tui)
- [Global Flags](#global-flags)
- [Exit Codes](#exit-codes)
- [Safety & Architecture](#safety--architecture)
  - [Dependency Ordering](#dependency-ordering)
  - [Write-Ahead Log (`journal.jsonl`)](#write-ahead-log-journaljsonl)
  - [Atomic State Snapshot (`state.json`)](#atomic-state-snapshot-statejson)
  - [Conflict Detection & `--force`](#conflict-detection---force)
  - [Interrupted Run Recovery](#interrupted-run-recovery)
- [RBAC & Permissions](#rbac--permissions)

---

## Features

- **Reversible by design**: Captures the exact original state before changing anything and restores only to that original baseline.
- **Strict Dependency Ordering**:
  - **Suspend**: Freezes Kustomizations first (reverse order), then HelmReleases (reverse order).
  - **Restore**: Unfreezes HelmReleases first (forward order), then Kustomizations (forward order), preventing health-check hangs on suspended Helm releases.
- **Crash Proof**: Every intent is recorded to a write-ahead log (`journal.jsonl`) and synced (`fsync`) before any Kubernetes API patch is executed.
- **Concurrency Protection**: Non-blocking `flock` prevents concurrent executions across separate terminals or scripts.
- **Conflict Aware**: Detects out-of-band cluster modifications and refuses to overwrite without `--force`.
- **Zero Flux Go Dependencies**: Built directly on `k8s.io/client-go/dynamic` and discovery APIs.

---

## Installation & Build

### Prerequisites

- Go `1.26+` (or distro package)
- Active `kubeconfig` pointing to your cluster

### Build Binary

```bash
# Build the single binary
go build -o killswitch ./cmd/killswitch

# Verify
./killswitch --help
```

---

## Configuration

Kill Switch searches for configuration in the following order:
1. `--config <path>` flag
2. `./config.yaml`
3. `./config.yml`
4. `./config.example.yaml`
5. `~/.config/killswitch/config.yaml`

### Example Configuration (`config.example.yaml`)

```yaml
flux:
  resources:
    # Kustomizations (dependency order)
    - { kind: Kustomization, namespace: flux-system, name: flux-system }
    - { kind: Kustomization, namespace: flux-system, name: infrastructure }
    - { kind: Kustomization, namespace: flux-system, name: cert-manager-config }
    - { kind: Kustomization, namespace: flux-system, name: apps }

    # HelmReleases (infrastructure first, then applications)
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

---

## Commands & Usage

### `killswitch status`

Displays the live reconciliation status of all configured resources and the current global state (`SUSPENDED`, `RESTORED`, or `PARTIAL`).

```bash
# Human-readable table
./killswitch status

# Machine-readable JSON output
./killswitch status --json
```

Output highlights:
- `STATUS`: `ACTIVE` (reconciling), `SUSPENDED` (frozen), or `MISSING`
- `ORIGINAL`: Recorded original state before the last suspend
- `GEN/OBS`: Resource generation vs observed generation (tagged with `(!)` if the controller is lagging)
- `PHASE`: Active in-progress phase during an operation or recovery

---

### `killswitch suspend`

Freezes all configured resources by setting `spec.suspend: true`.

```bash
# 1. Preview changes without modifying the cluster
./killswitch suspend --dry-run

# 2. Interactive execution (prompts for confirmation)
./killswitch suspend

# 3. Non-interactive execution (e.g. in CI or scripts)
./killswitch suspend --yes

# 4. Override external conflict warnings
./killswitch suspend --force
```

---

### `killswitch restore`

Restores all configured resources to their recorded original states.

```bash
# 1. Preview restore plan
./killswitch restore --dry-run

# 2. Execute restore with confirmation
./killswitch restore

# 3. Non-interactive restore
./killswitch restore --yes

# 4. Override external modifications
./killswitch restore --force
```

---

### `killswitch history`

Inspects past operations recorded in the write-ahead journal (`journal.jsonl`).

```bash
# Show last 10 operations
./killswitch history --limit 10

# Output full history as JSON
./killswitch history --json
```

---

### `killswitch tui`

Launches the full interactive terminal UI powered by Bubble Tea:

```bash
./killswitch tui
```

**Keybindings:**
- `s` — Plan and prompt to **Suspend** all resources
- `r` — Plan and prompt to **Restore** all resources
- `d` — Open **Dry-run** preview modal
- `u` — Refresh live cluster status
- `y` / `n` — Confirm or cancel actions in modal
- `q` / `Ctrl+C` — Exit TUI

---

## Global Flags

The following flags apply to all subcommands:

| Flag | Default | Description |
|---|---|---|
| `--config <path>` | `config.yaml` | Path to target resources configuration YAML |
| `--context <name>` | Current context | Target Kubernetes context |
| `--kubeconfig <path>` | `~/.kube/config` | Path to kubeconfig file |
| `--timeout <duration>` | `60s` | Operation timeout per API request |
| `--state-dir <path>` | `$XDG_STATE_HOME/killswitch` | Directory for journal and lock files |
| `--log-level <level>` | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |

---

## Exit Codes

Kill Switch follows strict exit code semantics for reliable automation:

| Code | Meaning | Cause |
|---|---|---|
| `0` | Success | Operation completed and verified |
| `1` | Partial failure | One or more resources failed patching or read-back verification |
| `2` | Config / Connection error | Invalid YAML, unreachable cluster, or missing target resource |
| `3` | Conflict | Resource was modified outside Kill Switch; requires `--force` |
| `4` | Unsafe state / Lock contention | Concurrent run active or incomplete state without recorded originals |

---

## Safety & Architecture

### Dependency Ordering

To prevent reconciliation deadlocks:
1. **Suspend Order**: Kustomizations (reverse config order) -> HelmReleases (reverse config order).
2. **Restore Order**: HelmReleases (forward config order) -> Kustomizations (forward config order).

Restoring HelmReleases *before* Kustomizations ensures that Kustomizations with `wait: true` or health checks do not time out waiting for dependent Helm charts that have not yet been unsuspended.

### Write-Ahead Log (`journal.jsonl`)

Located at `$XDG_STATE_HOME/killswitch/journal.jsonl` (fallback `~/.local/state/killswitch/journal.jsonl`):
- Mode `0600` on disk.
- Events: `op.start`, `resource.intent`, `resource.result`, `conflict`, `op.end`.
- An intent is **appended and fsynced before** the cluster patch is sent.
- Crashes mid-line produce torn writes, which the parser automatically detects and ignores.

### Atomic State Snapshot (`state.json`)

Derived view of the active operation and recorded original states:
- Written to a temporary file, fsynced, atomically renamed over `state.json`, followed by directory fsync.
- If `state.json` is missing or out of sync with `journal.jsonl`, it is automatically reconstructed by replaying the journal.

### Conflict Detection & `--force`

If a resource's suspension state on the cluster changes out-of-band (e.g. manually edited via `kubectl` or another operator):
- Kill Switch flags this as a `conflict`.
- Execution halts with **Exit Code 3**.
- Use `--force` to acknowledge and proceed.

### Interrupted Run Recovery

If the tool is terminated (e.g. `SIGKILL`, system reboot, power cut):
1. `killswitch status` flags the interrupted operation.
2. Running `killswitch suspend` resumes and completes remaining targets.
3. Running `killswitch restore` restores any touched targets back to their original baseline.

---

## RBAC & Permissions

For cluster environments requiring least-privilege service accounts, apply `rbac.example.yaml`:

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

Apply with:
```bash
kubectl apply -f rbac.example.yaml
```
