---
name: add-switch
description: Use when the user wants to add, create, or scaffold a new Kill Switch switch in switches.yaml plus workflows/*.yaml — including "add a switch", "new switch", "scale these workloads", "add a workflow", or "make a new on/off control" for this repo.
---

# Add a Kill Switch switch

Add a user-visible switch to the layered Kill Switch config:

- `switches.yaml` (or this repo's `config.example.yaml`) declares the switch.
- `workflows/<id>.yaml` defines the ordered steps.
- Go action handlers execute each step. Phase 3 supports `flux.suspend`/`flux.resume`, `kubernetes.scale`, and optional `kubernetes.suspend`.

Do not add infrastructure logic to the TUI or engine. A switch is an operational state; a workflow composes existing actions.

## 1. Locate the config root

The root is the directory containing `switches.yaml` or the file passed to `--config`:

- In this repo: `config.example.yaml` plus `workflows/`.
- User installs: `switches.yaml` plus `workflows/` in the same directory (commonly `~/.config/killswitch/`).

Legacy single-file `flux.resources` configs are not supported. If the repo has no config root yet, create one using `config.example.yaml` as the template.

## 2. Add the switch entry

Append to the `switches:` list. `workflow` must match a workflow id.

```yaml
switches:
  - id: navidrome
    name: Navidrome
    description: Scale the Navidrome music server to zero (auto-suspends Flux)
    icon: ♪                # optional TUI marker
    workflow: navidrome
```

Rules:
- `id` is unique, lowercase, and is what the CLI accepts (`killswitch navidrome off`).
- `name` is the TUI label.
- `icon` is optional; it renders in a fixed-width slot, so rows stay aligned and unset icons leave blank space.
- `workflow` is the workflow file's `id`, normally the same slug.

## 3. Create `workflows/<id>.yaml`

Start from the closest example:
- `workflows/navidrome.yaml` — one Deployment with a Flux prerequisite.
- `workflows/pullini.yaml` / `workflows/cloudflared.yaml` — the same pattern for other Deployments.
- `workflows/flux.yaml` — the Flux reconciliation switch.

### Workload scaling template

```yaml
id: navidrome
name: Navidrome
requires:
  flux: suspended        # optional: suspended | active | any

steps:
  - id: navidrome
    action: kubernetes.scale
    target: {kind: Deployment, namespace: navidrome, name: navidrome}
    states:
      off: {replicas: 0}
      on: {restore: previous, default: 1}
```

Add more steps (for example a `StatefulSet`) by repeating the same block with a unique `id` and target.

- Steps run in declaration order for ON and in reverse order for OFF.
- OFF captures originals; ON restores them.
- `restore: previous` requires a captured original. Add `default:` so the first ON works.
- Use `on: {replicas: N}` instead of `restore: previous` for a fixed ON size.
- `requires.flux: suspended` makes turning the switch OFF suspend Flux first, so reconciliation cannot immediately undo the scale-down. Toggling Flux never touches this switch; reconciliation remains the source of truth. The prerequisite cascade is one operation.

### Flux template

```yaml
id: my-flux
name: My Flux group
requires:
  flux: any

steps:
  - id: apps
    action: flux.suspend
    target: {kind: Kustomization, namespace: flux-system, name: apps}
    states:
      off: {suspend: true}
      on: {restore: previous, default: false}
```

Only `Kustomization` and `HelmRelease` targets are valid for Flux actions.

### CronJob template

```yaml
steps:
  - id: backups
    action: kubernetes.suspend
    target: {kind: CronJob, namespace: navidrome, name: backups}
    states:
      off: {suspend: true}
      on: {restore: previous, default: false}
```

`kubernetes.suspend` is optional in Phase 3 and only supports `CronJob`.

## 4. Validate

Run without a cluster:

```sh
killswitch validate --config <config-root-or-switches-file>
# or in this repo
go run ./cmd/killswitch validate --config config.example.yaml
```

Validation catches duplicate ids, missing workflow references, unsupported actions, and invalid targets. Also check:
- Step ids are unique within a workflow.
- `requires.flux` is `suspended`, `active`, or `any`.
- Workflow ids match their file id; the Flux switch must not require Flux.

## 5. Exercise the switch

With a cluster available:

```sh
killswitch status --switch navidrome
killswitch navidrome off --dry-run
killswitch navidrome off --yes
killswitch status --switch navidrome
killswitch navidrome on --yes
```

Dry-run lists the primary transition and every cascaded step. Conflicts require `--force`. Missing targets are a hard error.

Without a cluster, add or update tests under `internal/engine/` using the fake Flux and scale clients in `phase3_test.go`, then run:

```sh
gofmt -l .
go vet ./...
go test ./...
```

## 6. Update docs when the switch is user-facing

- Add the example to `config.example.yaml` and `workflows/` when it belongs in the repo.
- Mention new actions or semantics in `README.md` and `wiki/pages/manual.md`.
- Update RBAC in `rbac.example.yaml` if the switch patches a new resource type.
- Wiki rules: do not hand-edit `wiki/index.md` or `wiki/log.md`; use `node scripts/wiki.mjs log|index|lint`.

## Invariants and common mistakes

- **Capturing on the wrong side**: originals are captured only on ON→OFF and are never overwritten. For scale, `off: {replicas: 0}` is the default.
- **First ON refused**: `restore: previous` without a recorded original fails unless `states.on.default` is set.
- **Wrong target kind**: `kubernetes.scale` only accepts `Deployment`/`StatefulSet`; Flux actions only accept `Kustomization`/`HelmRelease`; `kubernetes.suspend` only accepts `CronJob`.
- **Forgetting the switch entry**: a workflow file alone does nothing. A switch entry alone fails validation.
- **Editing generated files**: `wiki/index.md` is generated by `node scripts/wiki.mjs index`.
- **Expecting the dependent to run after a prerequisite failure**: the primary workflow stops on its first failed step, and a failed prerequisite (e.g. Flux suspend) aborts the operation before the dependent's steps run.

## Extension points

When adding new action types later:
1. Add the action constant and target validation in `internal/config/config.go`.
2. Implement the handler lifecycle in `internal/actions/` and register it in `internal/engine/engine.go`.
3. Add plan/execute tests with a fake client.
4. Update this skill's action list and templates.
