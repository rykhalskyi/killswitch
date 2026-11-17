# Decisions

Append-only decision record. The "why" lives here; the full change history lives in git.

- [2026-11-08] D-01 — Go + Bubble Tea + client-go + YAML + SQLite + slog — because the brief fixes this stack for Phase 1
- [2026-11-08] D-02 — One page per work item; spec/plan pages only for large epics — because most work is small and token budget matters
- [2026-11-08] D-03 — Replace the SQLite journal from D-01 with journal.jsonl as the write-ahead source of truth plus an atomic state.json snapshot — because volume is tiny, single-writer, and SQLite adds no recovery benefit here
- [2026-11-08] D-04 — Phase 1 scope is a Level 2 Flux freeze: 4 Kustomizations plus 11 HelmReleases — because suspending Kustomizations alone does not stop HelmRelease reconciliation
- [2026-11-08] D-05 — Use client-go/dynamic with unstructured for Flux CRDs — because it avoids Flux Go modules and code generation while keeping get/patch/read-back generic
- [2026-11-08] D-06 — Build CLI-first with a thin Bubble Tea TUI over a shared engine — because the safety logic must be testable and scriptable
- [2026-11-08] D-07 — Pin Go 1.26.10 from Fedora 44 — because Go was not installed and the distro package is current
- [2026-11-08] D-08 — Exclude GitRepository from Phase 1 — because suspending it does not prevent reconciliation and would add a third verification path
- [2026-11-17] D-09 — Phase 2 recovery scopes plans to ActiveOp.ResourceStates — because replaying untouched resources could overwrite external state
- [2026-11-17] D-10 — Conflict detection compares per-resource expected vs observed in both directions and journals real expected values — because last-op-only checks missed post-restore drift
