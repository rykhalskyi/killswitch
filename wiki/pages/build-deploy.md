---
created: 2026-11-08
type: build-deploy
summary: Build/test/lint/release commands for Kill Switch (Go); tooling will be added with Phase 1.
---

# Build & Deploy

The repository is currently a Go project scaffold (`.gitignore` for Go, no `go.mod` yet). Commands below are the expected conventions; update this page when the toolchain lands.

## Build

```sh
go build ./...
go build -o kill-switch ./cmd/kill-switch
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
./kill-switch            # reads config/config.yaml and the current kubeconfig
```

Requires a valid kubeconfig with access to the configured Flux resources.

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
