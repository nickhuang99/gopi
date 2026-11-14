<!-- doc-class: historical -->
# Task Report: M4.13 harvest git push-back + anchor skew-rewind + live-twin semantics

## Completed
- **Git push-back phase (⑤)** — `internal/tools/memory_sync_task.go`:
  - `GitPushbackReport{branches_pushed, push_failed}` joins the harvest output as the `git` JSON domain (nil = disabled / `--no-push`).
  - `gitPushbackDefault(peer)`: walks `git for-each-ref refs/heads`, skips master/main, pushes every local branch (cap `harvestGitMaxBranches=20`) to `<peer>:~/work/GoPi` via `git push <peer>:<repo> <name>:<name>`. Idempotent — git's up-to-date detection makes re-runs no-op successes. Peer token validated (no whitespace/`;`). Each invocation capped by `harvestGitTimeout=60s` (Rule 1: all literals in one const block).
  - Ordering: git push-back runs AFTER the committed db sync (asserted by `TestHarvestGitPushbackRunsAfterCommit` — the fake pusher records whether the tx rows are visible). A git failure returns the counts plus a `git push-back` error; the db outcome is never poisoned (independent idempotent domains).
  - Fail-soft per branch: a failed push lands in `push_failed` with the first stderr line (`harvestGitErrLine`), never aborts the remaining pushes.
  - CLI: `gopi task harvest --peer ALIAS [--no-push]`; `--no-push` leaves `GitPushback` nil → zero report. Usage/help updated (`cmd/agent/cli_tasks.go`).
  - Tests: `internal/tools/memory_sync_task_gitpushback_test.go` — 6 tests (ordering, report joining, fail-soft, `--no-push` nil, invalid peer guard, non-repo listing failure). Tunables (`harvestGitTimeout/MaxBranches/PeerRepo/ErrLine`) injectable.
- **Anchor skew-rewind (bug038 class)** — the anchor is never parked at the newest stamp: `harvestAnchorRewind=5.0s` rewind on every advancing harvest (`newAnchor-anchor < 5 → keep old anchor; else −5`). A row committed in the same wall-clock second (or truncated by the `%.6f` SELECT format) is re-fetched next run instead of skipped forever; idempotent apply makes the duplicates free. `harvestTsEpsilon` 1e-9 → 1e-6 (one ULP of an epoch REAL at ~1.8e9 is 2.4e-7 s). Tests: `TestHarvestAnchorRewindSameSecondRecovers` (truncated-stamp row adopted on run 2), updated anchor expectations in rerun/failure tests.
- **Live-twin harvest semantics (E2E bug038 signature)** — remote `claimed/in_progress` rows carry a `remoteLive` marker (computed BEFORE status coercion to pending): at equal stamps a live local twin + remoteLive twin are the SAME rank — identical content absorbs (duped), diverging conflicts, and a remote TERMINAL row still wins (`migrated`). `harvestSameContent` compares a live local row against the coerced pending twin as equal rank. Removed the old blanket "remote older → skipped" fall-through (a newer stamp now reaches the terminal-wins path). Tests: `TestHarvestSameStampLiveTwinAbsorbed`, `TestHarvestLiveLocalSkipped` extended with the terminal-result-migrates-live path.
- Comment drift fixed: three sites claimed "git FIRST, then db" while the code (and order test) runs git AFTER the committed pull — all aligned.

## Not Done
- No real-machine HP↔peerbox push-back E2E in this session (needs the peer online); the fake-pusher tests cover ordering/semantics and git's own up-to-date detection carries idempotency.
- `harvestPeerRepo` (`~/work/GoPi`) is a constant, not per-peer configurable — fine while both nodes mirror the same checkout path.

## Verification
- Build: `go build ./...` OK; `go vet` (tools/config/cmd/agent) clean.
- Tests: targeted — `TestHarvest*` 42s OK, `GitPushback` 11s OK, `TestTaskIngestConcurrentClaimVsIngest` 11s OK (isolation), `internal/config` + `cmd/agent` (63s) packages OK. Full `internal/tools` package hit the 10m panic-timeout twice under heavy concurrent load from other agents' live sessions (two DIFFERENT victims — TestTaskIngestConcurrentClaimVsIngest, then TestLivenessColumns, both task-pool areas untouched by this diff; both pass in isolation). Verdict: box-load flake, not a regression.
- Commit: see `git log --oneline -2` (feat(memsync) push-back + docs).
- Mid-flight coordination note: the `@prefix payload` work (main.go/help.go/main_payload_test.go) was committed separately by another session (0bea9c0 + b000ca8) while this task was in flight; only the memsync files remained for this commit.
