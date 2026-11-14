<!-- doc-class: historical -->
# Task Report: independent verification — ledger 01a092fc (post-merge rebuild untracked guard)

Verifier, read-only. No code changed; the main repo is byte-preserved (tip `eda42737`, untracked=30, tracked_mods=0) and all throwaway worktrees were removed.

## Located deliverable

| item | value |
|---|---|
| fix commit | `eda42737f51e1a968f70cd74cdd877f214bc642b` — "feat(rebuild): the post-merge dirty-tree guard judges TRACKED changes only" |
| ancestry | `git merge-base --is-ancestor eda42737 master` → **OK** (also == master tip) |
| merge | `git reflog master` → `eda42737 master@{0}: merge feat/rebuild-guard-tracked-only: Fast-forward` (an earlier identical fast-forward at `master@{2}` was reverted by `git reset --hard 49ff4082` at `master@{1}` when the post-merge verify FAILed) |
| files | `cmd/agent/cli_tasks_rebuild.go`, `cmd/agent/cli_tasks_rebuild_guard_test.go` (new), `cmd/agent/cli_tasks_rebuild_test.go`, `internal/tools/rebuild.go`, `internal/tools/rebuild_tracked_guard_test.go` (new), `internal/tools/task_adoption.go`, `internal/standards/baseline.go`, `devdoc/rebuild-guard-tracked-only.md` |

The fix is two halves: (a) the rebuild guard switches to the new `tools.AdoptionGitStatusTracked` (`git status --porcelain --untracked-files=no`) while adoption keeps the strict untracked-inclusive `AdoptionGitStatus`; (b) `runBuild` injects `status.showUntrackedFiles=no` via `buildVCSStatusEnv` so the go toolchain's own `vcs.modified` stamp does not go `-dirty` on untracked-only scratch.

## Per-criterion table

| # | criterion | verdict | evidence |
|---|---|---|---|
| 1 | fix is an ancestor of master | **PASS** | `merge-base --is-ancestor eda42737 master` → ok; reflog shows the fast-forward merge |
| 2 | `CGO_ENABLED=0 go build ./...` green | **PASS** | exit 0 |
| 3 | fix's Go files gofmt-clean | **PASS** | `gofmt -l` on all 7 → empty |
| 4 | regression test(s) cover untracked-allowed state, RUN+PASS on fixed tree | **PASS** | `TestServeRebuildUntrackedOnlyRunsBuild`, `TestRebuildE2EUntrackedOnlyBinFollowsMasterAfterMerge`, `TestAdoptionGitStatusTrackedIgnoresUntracked`, `TestRunBuildStampsCleanUnderUntrackedOnly` |
| 5 | regression test(s) cover tracked-defers state, RUN+PASS on fixed tree | **PASS** | `TestServeRebuildTrackedDirtStillSkips`, `TestServeRebuildTrackedAndUntrackedSkips`, `TestRunBuildStampsDirtyUnderTrackedDirt`, `TestAdoptionGitStatusTrackedReportsTrackedDirt` |
| 6 | untracked-allowed leg demonstrably RED on pre-fix parent | **PASS (measured)** | pre-fix worktree `49ff4082` + the fix's own guard test file grafted on → `TestServeRebuildUntrackedOnlyRunsBuild` FAILS: `[merger] [rebuild] skip: worktree dirty — rebuild deferred to a clean tree`; other 3 PASS |
| 7 | acceptance ① ≥1 untracked + 0 tracked ⇒ rebuild RUNS, bin follows tip | **PASS** | test 4 + live: master tip `eda42737f51e`; build with `buildVCSStatusEnv` shape ⇒ `eda42737f51e` == tip; plain build ⇒ `eda42737f51e-dirty` |
| 8 | acceptance ② ≥1 tracked modification ⇒ still DEFERS with the existing log line | **PASS** | tests in row 5; log line `worktree dirty — rebuild deferred to a clean tree (bin stays on the last clean master)` (`cmd/agent/cli_tasks_rebuild.go:41,86`) |
| 9 | **whole project's own gate green on the merged state** | **FAIL** | `go test ./internal/stylegate` → `ignored-error: 183 test hit(s), ceiling 182`; pre-fix clean worktree `49ff4082` → `ok (182 ≤ 182)` |

## A/B red-proof — method and raw output

Method (no dependency on the devdoc's claim): a clean detached worktree at the pre-fix parent with the fix's **own** regression test file copied in, so both trees run the identical test code against the differing production code.

```
$ git worktree add --detach /tmp/v-prefix 49ff4082 && cp cmd/agent/cli_tasks_rebuild_guard_test.go /tmp/v-prefix/cmd/agent/
$ (cd /tmp/v-prefix && CGO_ENABLED=0 go test -count=1 -v -run 'TestServeRebuildUntrackedOnlyRunsBuild|TestServeRebuildTrackedDirtStillSkips|TestServeRebuildTrackedAndUntrackedSkips|TestAdoptionCleanTreeStaysStrict' ./cmd/agent/)
=== RUN   TestServeRebuildUntrackedOnlyRunsBuild
    cli_tasks_rebuild_guard_test.go:49: untracked-only tree must NOT defer the rebuild (pre-fix predicate):
        [merger] [rebuild] skip: worktree dirty — rebuild deferred to a clean tree (bin stays on the last clean master)
--- FAIL: TestServeRebuildUntrackedOnlyRunsBuild (0.01s)
--- PASS: TestServeRebuildTrackedDirtStillSkips (0.01s)
--- PASS: TestServeRebuildTrackedAndUntrackedSkips (0.01s)
--- PASS: TestAdoptionCleanTreeStaysStrict (0.01s)
FAIL	github.com/nick/gopi/cmd/agent	0.081s
```

Fixed tree (same run, no graft needed):

```
--- PASS: TestServeRebuildUntrackedOnlyRunsBuild (0.03s)
--- PASS: TestServeRebuildTrackedDirtStillSkips (0.01s)
--- PASS: TestServeRebuildTrackedAndUntrackedSkips (0.06s)
--- PASS: TestAdoptionCleanTreeStaysStrict (0.02s)
ok  	github.com/nick/gopi/cmd/agent	0.241s
--- PASS: TestAdoptionGitStatusTrackedIgnoresUntracked (0.02s)
--- PASS: TestAdoptionGitStatusTrackedReportsTrackedDirt (0.02s)
--- PASS: TestBuildVCSStatusEnvDropsCallerConfig (0.00s)
--- PASS: TestRunBuildStampsCleanUnderUntrackedOnly (0.34s)
--- PASS: TestRunBuildStampsDirtyUnderTrackedDirt (0.34s)
--- PASS: TestRebuildE2EUntrackedOnlyBinFollowsMasterAfterMerge (0.36s)
ok  	github.com/nick/gopi/internal/tools	1.177s
```

Live second-half leg (master `eda42737f51e`, 30 untracked / 0 tracked):

```
$ CGO_ENABLED=0 go build -o /tmp/v-bin-plain ./cmd/agent/ && /tmp/v-bin-plain --version
eda42737f51e-dirty                      # untracked-only tree + NO env ⇒ -dirty (the reason half (b) is required)
$ env GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=status.showUntrackedFiles GIT_CONFIG_VALUE_0=no CGO_ENABLED=0 go build -o /tmp/v-bin-fix ./cmd/agent/ && /tmp/v-bin-fix --version
eda42737f51e                            # == master tip: versionMatches would pass
$ ./bin/gopi --version
49ff408258b6-dirty                      # deployed bin is STILL pre-fix master (the reported symptom)
```

Full-suite context (`CGO_ENABLED=0 go test -count=1 ./...`, master `eda42737`): red packages are `internal/agent` and `internal/stylegate`.

- `internal/agent` (9 tests: `TestAutoTaskRegister*`, `TestChatTaskRegistration_*`, `TestFailedLedgerHint_*`) — **pre-existing**: the identical 9 fail in a clean `49ff4082` worktree (`go test ./internal/agent/` there → same FAIL list). Unrelated to this fix (env/ledger-DB dependent).
- `internal/stylegate` — **attributable to this commit**, see below.

## The correctness leg that is TRUE, and the one that is FALSE

The bug itself is genuinely fixed and I reproduced both acceptance behaviours independently (rows 4–8). What is NOT delivered is acceptance 9 — the project's own machine gate is red on the merged state, and the red is a net-new violation introduced by the fix.

Gate count, measured with a scanner probe injected into both throwaway worktrees (the gate prints sites only when it fails, so I measured the counts directly):

```
pre-fix  /tmp/v-prefix-clean @49ff4082: ignored-error testScope=true got=182 ceiling=182  → ok
post-fix /tmp/v-postfix      @eda42737: ignored-error testScope=true got=183 ceiling=182  → FAIL
```

Site-level diff of the two `ignored-error` test-site lists: 4 lines net-new, 3 net-gone — the 3 gone are `cmd/agent/cli_tasks_rebuild_test.go:164/165/167` (pre-existing sites renumbered by the fix's own `+10`-line edit to that file, with `:169/171/172` net-new), and the +1 is a release-blocking new site:

```
cmd/agent/cli_tasks_rebuild_guard_test.go:119   (ignored error-typed value (adoptionCleanTree(repo), result 2))
    `if clean, _ := adoptionCleanTree(repo); clean {`
```

It is in the fix's own **new** file, so the `182 → 183` delta is fully attributable to this commit; no other file's site count moved. There is no `//stylegate:ignore` annotation at that site (grepped: zero occurrences in either of the fix's new test files), while the sibling convention is an explicit per-site reason (`cmd/agent/cli_web_align_test.go:69,70,129`). No annotation fix commit exists anywhere (`git log --all --oneline -- cmd/agent/cli_tasks_rebuild_guard_test.go` → only `eda42737`), and the rework branch ref `feat/rebuild-guard-tracked-only` is gone (`git branch -a` → no match). Rework task `01a09453-fc4e-7e1a-8068-b9bc91ed5723` is still **pending** (filed by the earlier revert; its own gate note is the same finding). So the rework is NOT delivered — the ceiling cannot be raised without weakening the sweep's ratchet.

## Honest gaps

- I did not exercise the notify / auto-revert path (`build-failed` delivery, `RestoreMaster`) end-to-end; only the stamp precondition for it (`-dirty`) was proven.
- I did not restart the merger daemon to observe a live `[merger] [rebuild] ok:` line post-fix (that would have touched live services). The live evidence is the build/stamp leg plus the pre-existing skip line in `~/.gopi/logs/merger.log:2944,2969`.
- `internal/agent`'s 9 reds were not root-caused (only shown to be pre-existing and untouched by this commit).
- One grafted test file existed inside the throwaway pre-fix worktree; it never touched the main repo and the worktree was removed.

## Verdict

**failed** — the fix is functionally correct and both acceptance behaviours reproduce (10/10 targeted tests PASS; A/B red-proof confirmed), but the merged state (master `eda42737`, clean checkout) fails the project's own `internal/stylegate` gate: `ignored-error` 183 test hits > ceiling 182, with the single net-new site `cmd/agent/cli_tasks_rebuild_guard_test.go:119` in this commit's own new test file. Rework (one annotation line, or handle the error) is filed but not delivered.
