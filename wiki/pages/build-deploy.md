---
created: 2026-11-08
type: build-deploy
summary: Build, test, lint, and run commands for the Kill Switch Go CLI and TUI.
---

# Build & Deploy

Kill Switch is implemented in Go as a single-binary CLI and TUI.

## Build

```sh
go build ./...
go build -o killswitch ./cmd/killswitch
```

## Test

```sh
go test ./...
go vet ./...
```

## Lint / format

```sh
gofmt -l .
go vet ./...
```

## Run

```sh
./killswitch status                         # queries live Flux resources from cluster
./killswitch suspend --dry-run              # previews suspend plan without modifying cluster
./killswitch suspend                        # freezes all configured Flux resources
./killswitch restore                        # restores all resources to recorded original states
./killswitch history                        # displays operation history from journal.jsonl
./killswitch tui                            # launches the Bubble Tea interactive TUI
```

Requires a valid kubeconfig with access to the configured Flux resources. Config is loaded from `config.yaml` or `config.example.yaml`.

## Wiki tooling

Node is used only for the wiki helper:

```sh
node scripts/wiki.mjs index
node scripts/wiki.mjs lint
node scripts/wiki.mjs log <action> "<description>"
node scripts/wiki.mjs decision "<decision> — because <reason>"
```

## Release / distribution

Phase 7 scope: standalone Linux binaries, shell completions, dry-run mode, installation docs, tests, and release automation. Not yet implemented.
