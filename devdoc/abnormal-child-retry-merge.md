<!-- doc-class: historical -->
# Task Report: landing the parked branch `feat/abnormal-child-retry`

## Completed

- **Merge**: worktree `/home/user/workspace/gopi-abnormal-child-retry` created from
  the park tag `archive/feat/abnormal-child-retry` (tip `ca8eecea`); `git merge
  --no-commit --no-ff master` (`ee125fe3`) auto-resolved with **ZERO textual
  conflicts** (`git diff --name-only --diff-filter=U` empty). The conflict
  resolution therefore needed no side-picking.
- **Additivity proof** (measurement, not eyeballing): the merged tree vs master
  is exactly the branch's own 7 files, `+1141/-3`. Nothing master-side was
  dropped and nothing branch-side was lost. Master had not touched the failure
  arm of `cmd/agent/cli_tasks.go` (line 1830) nor
  `internal/tools/task_holder_resume.go`, so the parked merge was cleanly
  additive.
- **Integration audit**: every symbol the feature calls still exists on master
  (`TaskRowTerminalAt`, `TaskRunRetryCounts`, `TaskRunRetryNote`,
  `TaskRunRetryCap`, `TaskRunChildResumable`, `SpawnTaskResume`,
  `TaskUpdateReason`, `TaskSessionID`, the three format consts). Master carries
  no competing abnormal-child-retry logic (`MergeVerifyRetryCap` in
  `verify_gate.go` is a different lane). `go build ./...` and `go vet` on both
  touched packages: clean.
- **Flattened to an FF-able tip**: a merge commit cannot satisfy
  `git merge-base --is-ancestor master HEAD`. The merge tip
  `3286633425ca` was tagged `backup/feat/abnormal-child-retry-mergetip` (the
  two-parent resolution record) and flattened with `git reset --soft master` +
  recommit — the resolution is now a **one-parent child of master** with a
  byte-identical tree.
- **gofmt**: `cmd/agent/cli_tasks_child_retry_wiring_test.go` was not
  gofmt-clean; formatted it (5 lines). The branch tree's `gofmt -l .` set is now
  a **subset-equal** of master's (8 = 8, delta empty).
- **Park tag dropped**: acceptance allows it once the branch has landed as an
  FF-able tip. Safe because `ca8eecea` is a **parent** of
  `backup/feat/abnormal-child-retry-mergetip`, so the archived tip's history is
  still reachable (verified: `git merge-base --is-ancestor <park tag>
  <mergetip>` exit 0). `bash scripts/safe-merge.sh feat/abnormal-child-retry`
  now exits **0** (all four checks green).

## Resolution record / deliverable refs

| ref | what |
|---|---|
| `feat/abnormal-child-retry` | the FF-able deliverable tip (`079b7d6e`) — the merger's deliverable-ref target |
| `feat/abnormal-child-retry-v2` | the worktree's own branch, same tip (identical sha) |
| `backup/feat/abnormal-child-retry-mergetip` | the two-parent resolution record (`3286633425ca`) |
| park tag `archive/feat/abnormal-child-retry` | DROPPED (branch landed as an FF-able tip; archived tip kept reachable via the mergetip tag) |

The branch has NOT been merged into master — that is the merger's step
(pure git, FF), not this task's. No force-push was performed anywhere.

## Not Done (honest)

- **Master was not merged by this task** — deliberately: the merger lands
  fast-forwardable branches, and `master` was only read. The branch is left
  FF-able and safe-merge-clean for it.
- **Two pre-existing `cmd/agent` test failures were NOT fixed** (out of scope,
  identical on master — see the A/B below):
  `TestVerifyGateLandingCarriesTheDurableCloseLoop` (reads a
  `merger_verify_child.go` that master does not have) and
  `TestCloseLoopWiringIsComplete` (`func mergerRunPass(` renamed on master).
  They are master reds, not merge defects.
- **Pre-existing `internal/stylegate` red NOT fixed** (master-identical —
  ceilings diff is empty; the branch's files contribute **zero** hits).
- A pre-existing `<<<<<<<`/`>>>>>>>` pair in `devdoc/repo-context-gate-reverify.md`
  exists on master too and was left untouched (not introduced here).

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → exit 0 (at the deliverable
  commit); `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- **Vet**: `go vet ./cmd/agent/ ./internal/tools/` → exit 0.
- **FF-ability**: `git rev-list --count master..HEAD` → **1**;
  `git merge-base --is-ancestor master HEAD` → exit **0**;
  `git diff --stat <mergetip> HEAD` → **empty** (tree identical).
- **Branch feature tests** (at the deliverable commit `079b7d6e`):
  `go test -count=1 -run 'Retry|AbnormalChild' ./cmd/agent/ ./internal/tools/`
  → `ok cmd/agent 4.312s`, `ok internal/tools 2.682s`.
- **Same-commit A/B** (`-json ./internal/tools/ ./cmd/agent/`, branch vs a
  detached master worktree at `ee125fe3`, both with `bin/gopi` freshly built):
  - package verdicts **identical** on both sides:
    `internal/tools` PASS; `cmd/agent` FAIL.
  - failing test set **identical**: {`TestVerifyGateLandingCarriesTheDurableCloseLoop`,
    `TestCloseLoopWiringIsComplete`} — same messages both sides →
    **zero regressions introduced by the merge**.
  - `internal/standards` → PASS. `internal/stylegate` ceilings: branch list
    byte-identical to master's (7 ceilings, e.g. `errorf-verb` 9>8,
    `ignored-error` 201>182) → pre-existing.
- **gofmt**: branch `gofmt -l .` (8 files) set-diff vs master's (8 files) →
  **empty**.
- **Conflict markers introduced**: 0 (branch tree matches master's single
  pre-existing occurrence).
- **`scripts/safe-merge.sh feat/abnormal-child-retry`** → exit 0, all four
  checks ✅.

## Merge commit

`079b7d6e` — `feat(task-run): bounded automatic retry of an abnormally-exited
executor child` (one parent: master `ee125fe3`; tree = master + the feature).
