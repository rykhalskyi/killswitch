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
- [2026-11-17] D-11 — Adopt three-layer configuration — switches.yaml, workflow files, and Go action handlers — because the switch UI must stay infrastructure-agnostic and actions must be reusable across switches
- [2026-11-17] D-12 — Capture original state only when a switch enters the non-default (OFF) state, never overwrite it, and expose --recapture for an intentional reset — because repeated captures would record the already-changed state as the original
- [2026-11-17] D-13 — Refuse an ON transition with no recorded original unless states.on.default is configured — because silently guessing a restore value can start workloads at the wrong size
- [2026-11-17] D-14 — Stop a workflow on the first failed step by default, with per-workflow on_error: rollback opt-in — because implicit rollback can fail and obscure the partial state
- [2026-11-17] D-15 — Evaluate requires before the first step and auto-toggle the referenced switch (starting with Flux), journaling it in the same operation and leaving it in the required state — because manual prerequisites add friction and hidden side effects are worse than visible ones
- [2026-11-17] D-16 — Require the layered switches.yaml plus workflows config format and drop legacy flux.resources compatibility — because no user configs exist yet
- [2026-11-17] D-17 — Treat switch dependencies as a two-way interlock: enabling a dependent auto-satisfies its prerequisite, and moving the prerequisite out of the required state runs dependent OFF workflows first; a failed dependent OFF does not block the prerequisite because Flux reconciliation converges it — because reconciliation is the source of truth and blocking adds no safety
- [2026-11-21] D-18 — Enforce requires.flux on the dependent's OFF transition (D-18) — because scaling a Flux-managed workload down while reconciliation is active is immediately reverted; restoring the dependent leaves Flux as-is
