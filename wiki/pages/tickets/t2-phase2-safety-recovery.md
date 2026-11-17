---
created: 2026-11-17
type: ticket
status: implemented
summary: Phase 2 gap closure — TUI history, precise interrupted-run recovery, and per-resource conflict detection on top of the Phase 1 engine.
---

# T2 — Phase 2: Execution safety and recovery

Follow-up to [T1](t1-phase1-flux-toggle.md) and roadmap item 2 in the [project overview](../project-overview.md). Phase 1 already shipped most of the original Phase 2 brief; this ticket covers only what is still missing or imprecise.

## Spec

Already implemented in T1 (no work needed here):

- Append-only JSONL journal with pre-patch `fsync`, torn trailing line discard, and `history` aggregation.
- Atomic `state.json` snapshots with journal replay on missing/corrupt/sequence-mismatched state.
- Non-blocking `flock` lock and exit code 4 on contention.
- Live read-back verification and `partial` global state until every target matches.
- Confirmation prompts in CLI and TUI; conflict abort with exit code 3 and `--force`.

Remaining Phase 2 scope:

1. **History in the TUI.** Add an `h` key that opens a read-only history view backed by `Engine.History(limit)`, showing operation time, action, status, target/ok/failed counts, and duration. `esc` returns to the switch list; history refreshes after an operation completes. The TUI must not read the journal file directly.
2. **Precise interrupted-run recovery.** Use `ActiveOp.ResourceStates` to identify resources touched by an interrupted operation. `suspend` should complete only untouched targets; `restore` should revert only touched targets instead of reapplying originals to every configured resource. Keep the refusal to restore when an active operation has no recorded originals (exit code 4).
3. **Conflict detection by expected vs observed.** Current detection keys off the last successful suspend operation only (`internal/engine/engine.go`), so an external un-suspend after `restore` or an external suspend after `restore` is not flagged. Compare each target with the expected state derived from the plan and recorded originals, and journal the real `expected` value instead of a hardcoded `true`. TUI execution needs a visible conflict/force path equivalent to CLI `--force`.
4. **Documentation sync.** `wiki/pages/manual.md` keybindings and T1's claimed interrupted-run tests differ from the current code; update the manual and add the missing engine tests.

Out of scope: new action handlers, workloads, nodes, network controls, and workflow orchestration.

Acceptance criteria:

- `h` in the TUI shows journaled operations and returns with `esc`.
- Journal replay tests cover interrupted suspend (resume untouched targets) and interrupted restore (undo only touched targets).
- External un-suspend/suspend after a successful restore is detected as a conflict in both CLI and TUI.
- `go test ./...`, `go vet ./...`, `gofmt -l .`, and `node scripts/wiki.mjs lint` pass.

## Plan

- `internal/tui`: add a history view mode, `historyMsg`/`historyCursor`, `fetchHistoryCmd` calling `Engine.History(50)`, a renderer, `h`/`esc` key handling, and view tests.
- `internal/engine`: derive a touched-resource set from `ActiveOp.ResourceStates` during plan building; refactor conflict checks to compare expected vs observed per resource; extend `engine_test.go` for interrupted-run and post-restore drift cases.
- `internal/cli` / TUI execution: keep exit code 3 semantics; surface conflict reasons in the TUI with a force-confirmation path.
- `README.md` and `wiki/pages/manual.md`: document `h`, correct the stale keybinding table, and describe the recovery/conflict behavior actually shipped.
- Run the verification commands listed in the acceptance criteria before flipping `status:` to `implemented`.

## Outcome

Shipped.

- TUI history view: `h` opens `Engine.History(50)`; `↑`/`↓` scroll, `Esc` returns to the previous view, `r`/`u` refresh, and history is refreshed after every operation. The TUI never reads `journal.jsonl` directly.
- Precise recovery: `planResources` scopes an interrupted operation to `ActiveOp.ResourceStates`. Suspend resumes only targets not already verified; restore undoes only resources with a recorded intent. Resources outside the interrupted operation are untouched.
- `Status` no longer reports `restored` unless the last completed operation was a successful restore and every resource matches its recorded original.
- Conflict detection now compares per-resource expected vs observed state in both directions (external un-suspend after freeze and external suspend after restore). Journaled conflict events carry the real `expected`, `observed`, and reason.
- TUI confirmation shows conflict reasons and supports `f` for a forced execution; CLI keeps exit code 3/`--force` semantics.
- Tests added for interrupted-suspend resume, interrupted-restore undo, post-restore drift conflicts, TUI history navigation, and TUI force override. README and the user manual were updated.
- Verified with `gofmt -l .`, `go vet ./...`, `go test ./...`, and `node scripts/wiki.mjs lint`.
