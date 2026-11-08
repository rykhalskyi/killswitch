## Kill Switch

**Kill Switch is a YAML-driven Linux TUI application for orchestrating operational changes in a homelab.**

Its purpose is to execute workflows safely, remember what changed, and restore the previous state when needed.

Core principles:
- Declarative configuration: Workflows are defined in YAML, not hardcoded in the application.
- Reversibility: Capture original state and support undo wherever possible.
- State awareness: Know what changed, what succeeded and what failed.
- Extensibility: Support different infrastructure technologies through action handlers.
- Transparency: Preview operations, show execution progress and maintain a journal.
- No infrastructure assumptions: Users define their own services, nodes and scripts.
The application is not a general-purpose task runner. It is a specialized infrastructure operations controller.

## Phase 1 — Flux reconciliation toggle

### 1. TUI

A minimal interface:

```
┌─────────────── KILL SWITCH ───────────────┐
│                                           │
│  Flux reconciliation                     │
│                                           │
│  Status: ACTIVE                           │
│                                           │
│  [ Suspend reconciliation ]               │
│                                           │
│  Resources: 3 configured                  │
│                                           │
└───────────────────────────────────────────┘
```

The button changes according to the current state:

- ACTIVE → Suspend reconciliation
- SUSPENDED → Restore reconciliation

### 2. Configuration

`config.yaml`:

```
flux:
  enabled: true
  resources:
    - kind: Kustomization
      namespace: flux-system
      name: apps

    - kind: Kustomization
      namespace: flux-system
      name: infrastructure

    - kind: HelmRelease
      namespace: monitoring
      name: prometheus
```

### 3. Core behaviour

Suspend

1. Read the current suspension state of every configured resource.
2. Save the original state in SQLite.
3. Set `spec.suspend: true`.
4. Verify each resource.
5. Report success or partial failure.

Restore

1. Load the saved original states.
2. Restore each resource to its original state.
3. Verify each resource.
4. Mark the operation as restored.

Never assume every resource was originally active.

### 4. Technology

| Component      | Choice     |
| -------------- | ---------- |
| Language       | Go         |
| TUI            | Bubble Tea |
| Kubernetes API | client-go  |
| Configuration  | YAML       |
| Journal        | SQLite     |
| Logging        | Go `slog`  |

### 5. Suggested project structure

```
kill-switch/
├── cmd/
│   └── kill-switch/
│       └── main.go
├── internal/
│   ├── tui/
│   ├── flux/
│   │   ├── client.go
│   │   ├── reconcile.go
│   │   └── state.go
│   ├── journal/
│   │   └── sqlite.go
│   └── config/
│       └── config.go
├── config/
│   └── config.yaml
└── go.mod
```

### 6. Phase-one completion criteria

- Connect to Kubernetes using the current kubeconfig.
- Load configured Flux resources from YAML.
- Display actual reconciliation status.
- Suspend all configured resources.
- Save original states in SQLite.
- Restore original states after suspension.
- Recover safely if the application exits midway.
- Report errors without silently treating partial success as complete.

One important safety rule: If suspending or restoring only some resources succeeds, show a partial-failure state. Never display the global toggle as fully suspended or active until all configured resources have been verified.

This is the complete scope of phase one. Keep the implementation small and avoid building abstractions for future switches until they're actually needed.


----------------


I'd structure Kill Switch into six phases, keeping each phase independently useful. The important thing is to build a reliable foundation before adding more infrastructure controls.

## Proposed development roadmap

### Phase 1 — Global Flux reconciliation toggle

Agreed scope

- Connect to Kubernetes using kubeconfig.
- Configure Flux resources in YAML.
- Suspend reconciliation for configured resources.
- Save original states in SQLite.
- Restore original states safely.
- Display actual status in the TUI.
- Handle partial failures and application restarts.

Result: A reliable global GitOps pause/resume control.

### Phase 2 — Execution safety and recovery

Before adding more operations, strengthen the engine.

- Execution history and detailed logs.
- Recovery after application crashes.
- Detect incomplete operations.
- Verify actual infrastructure state.
- Prevent overwriting changes made outside Kill Switch.
- Add confirmation prompts for potentially disruptive operations.

Result: Kill Switch can safely manage operations even when something goes wrong.

### Phase 3 — Kubernetes workload controls

Add the ability to turn applications on and off.

- Scale Deployments to zero.
- Restore their previous replica counts.
- Support StatefulSets.
- Configure workloads through YAML.
- Verify that workloads reach the requested state.

Result: Manage application availability without manually running `kubectl`.

### Phase 4 — Node management

Add physical and virtual machine controls.

- SSH-based shutdown.
- Wake-on-LAN.
- Check node availability.
- Configure credentials and targets.
- Handle unreachable nodes and timeouts.

Result: Control homelab machines from one interface.

### Phase 5 — Network controls

Add temporary network isolation.

- Block outbound internet access.
- Isolate selected services.
- Support Kubernetes NetworkPolicy or Cilium.
- Optionally integrate with a firewall or router.
- Capture and restore original network rules.

Result: Control network access without manually editing firewall rules.

### Phase 6 — Workflow orchestration

Bring the individual capabilities together.

- Define multi-step operations in YAML.
- Define prerequisites and dependencies.
- Execute steps in order.
- Roll back completed steps after failures.
- Support reusable workflows.
- Provide an execution plan before applying changes.

Result: One action can safely coordinate multiple parts of your homelab.

### Phase 7 — Polish and distribution

- Build standalone Linux binaries.
- Add shell completions.
- Add configuration validation and helpful error messages.
- Support dry-run mode.
- Add installation and usage documentation.
- Add tests and release automation.

Result: A usable, maintainable tool that is easy to install and operate.

## Recommended priorities

| Phase                      | Priority  | Dependency |
| -------------------------- | --------- | ---------- |
| 1. Flux toggle             | Essential | None       |
| 2. Safety and recovery     | Essential | Phase 1    |
| 3. Kubernetes workloads    | High      | Phase 2    |
| 4. Node management         | Medium    | Phase 2    |
| 5. Network controls        | Medium    | Phase 2    |
| 6. Workflows               | High      | Phases 3–5 |
| 7. Polish and distribution | High      | Ongoing    |

## A few architectural decisions

I'd keep these principles throughout development:

1. Configuration-driven: Define resources and operations in YAML, not in Go source code.
2. State-aware: Capture the original state before changing anything.
3. Reversible: Every operation should have a defined restoration strategy.
4. Observable: Show actual infrastructure state, not just what Kill Switch thinks it did.
5. Safe by default: Detect conflicts and partial failures instead of blindly continuing.

## My recommendation

Don't implement all six operational capabilities before building workflows. First make each individual capability reliable, then combine them.

The first meaningful milestone after Phase 1 should be Phase 2: safety and recovery. That is what turns Kill Switch from a convenient Flux toggle into a dependable operations tool.

I can also help turn Phase 1 into a Go implementation backlog, with packages, interfaces, and individual development tasks.

If you'd prefer a scheduled check-in:

🛠️Weekly Kill Switch planning check-inTap to set up
