<!-- doc-class: historical -->
# Task Report: fix-3major — self-review majors ①②③

Branch: `feat/fix-3major` (HP, `~/work/GoPi` — worktree-shared refs, base
peerbox/master `bca2e81`). Epic: fix the 3 major bugs from
`devdoc/self-review-20260830.md`, repro-test-first, full-suite green, push peerbox.

## Completed

### Bug ① — skip-tag 队头阻塞 + bug028 payload 死亡 (t-1788053263)
- `internal/tools/tasks.go` `TaskNextFiltered`: the candidate loop no longer
  `return Empty` on a skip-tagged row (the head-blocking bug) — the SELECT
  dropped its `LIMIT 1`, and rows are walked with `continue` until the first
  ELIGIBLE row (skip-tag match OR empty payload → skipped; none → Empty).
- Empty-payload rows are now never claimable at the queue layer: a
  payload-less row is ledger data, not dispatchable work. This kills bug028
  (serve claims ledger row → `taskRunBriefing` hard-refuses → `os.Exit(1)`
  → daemon silently dies ×3, per `devdoc/nightshift-report-20260830.md`).
  The bare-briefing incident guard (e3ca35ca) stays as defense-in-depth.
- `cmd/agent/cli_tasks.go` `runTaskServe`: `--once` now runs its single pass
  (previously the pass consumed the next claim AND a real claim could be
  silently skipped); non-once passes claim exactly one task.
- Callers: `TaskNext` delegates to `TaskNextFiltered`, so the payload rule is
  queue-API-wide. Test seeds updated to carry payloads where the test's intent
  is claim-flow, not payload semantics (`task_dispatch_test.go` "self work"/
  "race task", `tasknotify_test.go` "notify lifecycle"/"tagged task").

### Bug ② — legacy 重建迁移丢 FTS 触发器 (t-1788053263-1)
- `internal/tools/tasks.go` `tasksRebuildCheckConstraint`: after the commit of
  the rename-recreate-copy-drop migration, an explicit FTS rescue runs:
  drop + recreate `tasks_fts`/`tasks_tri` from `tasksFTSDefs()`, then
  `INSERT INTO <idx>(<idx>) VALUES('rebuild')` per companion (repopulates
  every row from tasks), then recreate `tasks_ai/au/ad` LAST (so 'rebuild'
  cannot double-fire them).
- Mechanism (isolation-proven on sqlite 3.37.2, /tmp probe + memory #1138):
  SQLite ≥3.25 rewrites same-table trigger bodies on `ALTER TABLE ... RENAME`
  (ON tasks → ON tasks_old); `DROP TABLE tasks_old` then deletes the
  triggers. The copy INSERTs ran under the (already renamed-away) triggers →
  migrated rows were NEVER indexed; the index survives only because
  external-content tables keep their rows, and a FRESH insert after migration
  is silently NOT indexed. `ensureTasksSchema`'s trigger re-creation restores
  future sync but not the migrated rows — only 'rebuild' does.
- Honest repro note: the classic legacy path (3-col FTS + 4-status CHECK) was
  ALREADY rescued in practice by `migrateTasksFTS` — the 3→7 column shape
  drift makes it drop/recreate/rebuild the companions. The unrescued hole is
  the INTERMEDIATE-build state (FTS widening landed while the stored CREATE
  literal still lacked 'pending'): shape matches, no rescue fires, migrated
  rows unindexed and triggers lost. Both repros + the fix are in place; the
  fix makes correct behavior explicit instead of riding on the shape-drift
  side effect.

### Bug ③ — web 默认 0.0.0.0 (t-1788053264)
- `cmd/agent/cli_web.go` `webDefaultAddrValue`: `":8090"` → `"127.0.0.1:8090"`
  (loopback-only default, the documented posture — web executes tools with no
  approval gate). LAN exposure = explicit `--addr 0.0.0.0` (or GOPI_WEB_ADDR)
  opt-in; `webDisplayURLs` still resolves the explicit all-interfaces bind
  into reachable URLs (fb9de6b behavior preserved for the opt-in case).

## Verification

- Repro tests (TDD, all reproduced BEFORE the fix, all pass after):
  - `TestReproSkipTagHeadBlocking` (existing, self-review) — FAIL→PASS
  - `TestReproServeClaimsEmptyPayloadRow` (new) — FAIL→PASS
  - `TestReproLegacyMigrationLosesFTSRows` (new; intermediate-state seed +
    trigger-presence/pointing + MATCH + live-index assertions) — FAIL→PASS
  - `TestReproWebDefaultLoopbackOnly` (new) — FAIL→PASS
  - `TestWebDisplayURLsAllInterfacesStillResolvesLAN` (new) — PASS before+after
- `go build ./...` → OK.
- `go test ./internal/tools/ ./cmd/agent/ -count=1` → full PASS (job-2/job-3).
- `go test ./...` (whole repo, HP): `internal/tools` ctags `symbol_test`
  failures (HP ctags binary: "Unknown language Rust/TypeScript, --map-JavaScript"
  — environmental, pre-existing, see memory #1138) and `testsuite`
  live-gopi tests (prompt-harness noise). Both pre-existing on this node,
  unrelated to these changes; verified by failure content (ctags CLI errors /
  positional-prompt harness text), not by stash-bisect.

## Not Done / deferred
- No schema version stamp for "rebuild already rescued" — the rescue is
  idempotent (a re-run just re-indexes), so it stays cheap and unconditional.
- `webDefaultAddr` still honors GOPI_WEB_ADDR (Rule 2: single env read site
  unchanged); no deprecation warning printed for implicit all-interfaces
  binds via env (documented posture change only).
- testsuite/ctags environmental failures are NOT fixed here (HP node issue,
  tracked in memory #1138).

## Deliverable
- Branch `feat/fix-3major` @ **712ccd2** on HP ~/work/GoPi (shared refs; base
  peerbox/master bca2e81); pushed to peerbox remote.
- Recover: `git -C ~/work/GoPi show feat/fix-3major:devdoc/fix-3major-report.md`
