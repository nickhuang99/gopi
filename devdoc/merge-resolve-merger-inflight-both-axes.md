<!-- doc-class: historical -->
# Task Report: resolve merge `feat/merger-inflight-both-axes` into `master`

Branch: `feat/merger-inflight-both-axes` (tip `9d49153a`, park tag
`archive/feat/merger-inflight-both-axes`), merge-base `69a4fed4`, master tip
`ea6373ab`. Resolution worktree: `/home/user/workspace/gopi-inflight-both-axes`
on `merge/merger-inflight-both-axes` (own worktree off master, Rule 6a/29).

## Completed

- Reproduced the merge (`git merge --no-commit --no-ff 9d49153a`) — exactly ONE
  conflicted file, `internal/tools/task_holder_resume.go`, 2 hunks; the branch's
  devdoc and its new `task_holder_resume_both_axes_test.go` merge clean.
- **Hunk 1** (`ResumeDeadHolderRuns` loop) — both sides replaced the same loop.
  Took the branch's `deadHolderResumeRowWorktree(r, byBranch)` structure (one
  decision, spelled once — the shared piece both sides want).
- **Hunk 2** (`deadHolderResumeRows` doc + query filter) — the two comments
  describe the same fix from two sides; BOTH kept (the loader evidence-set
  paragraph from the branch, the live-holder-status scoping paragraph from
  master). The query filter is identical on both sides
  (`COALESCE(deliverable,'') <> ''`) — takes master's form.
- **The real disagreement** was inside `deadHolderResumeRowWorktree`, which only
  exists on the branch: for a row that records directory A while git registers
  the row's branch at directory B, the branch dropped the candidate; master's
  landed fix (`30402cd0` + master's `TestDeadHolderResumeArmJudgesTheCheckoutGitRegisters`)
  judges it on **git's** checkout. The two are mutually exclusive on that input,
  and each side carried a test for its own reading. **Ruling: git's semantics
  win** — git is the occupancy source of truth, never the ledger; the ledger's
  recorded path is measured wrong on the live host (row `01a0a9b5` records
  `gopi-next-tick` while git registers `gopi-merger-next-tick`, memory
  #3231/#3234), and that is the very defect this epic's split 4/5 fixes. The
  branch's *structure* is kept, its *mismatch rule* is corrected; both sides'
  tests now pass (see Not Done for the honest scope note).
- The branch's `TestResumeArmDoesNotWeakenTheOccupancyVeto` asserted the
  discarded reading (row with another directory ⇒ no outcome). Re-pointed at the
  guarantee that survives BOTH intents: the recorded foreign directory is never
  adopted as the ground — the row is judged on git's registered checkout. The
  branch's other three tests (branch-axis load, live-holder protection,
  prefix-trap exactness) pass unmodified.
- **Two real defects found while running the branch's tests, both fixed here:**
  1. `git checkout --theirs <file>` as a way to take a side WIPES the
     non-conflicting changes master made to that file (it replaced the whole
     working-tree file from the merge stage, dropping master's added
     `SpawnTaskResume` → `undefined: tools.SpawnTaskResume`). Resolved by
     stripping the conflict markers per hunk instead, keeping master's
     unconflicted additions.
  2. `TestResumeArmJudgesABranchNamedRow` claimed "the gopi binary is stubbed by
     the fixture's GOPI_BINARY" but nothing set it, so the ARM's real spawn
     resolved through `runcmd.Launcher` → `os.Executable()` = **the test binary
     itself**, detached (PPID 1) into `tools.test resume t-branch-dead` — a
     package-suite re-run orphaned on every dead-holder case (5 such orphans were
     measured and killed; one had been running 6 minutes). The fixture now sets
     `GOPI_BINARY=testutil.MockDaemonBinary` as its own header always claimed.

## Not Done (honest)

- The mismatch ruling is a **judgement call between two landed fixes**, decided
  by evidence (memory #3231/#3234 + master's own new test) rather than by
  mechanically keeping both readings — the two cannot both hold for one input
  (one drops the row, the other judges it). The branch's reading is preserved in
  the tree as a comment naming the merge and the ruling, so a reviewer who wants
  it restored sees exactly what to flip (`deadHolderResumeRowWorktree` has one
  `return "", false` line there).
- Full `go test ./...` was not run — only the touched package
  (`./internal/tools/`) plus `./internal/standards/` (this repo's known-slow
  packages; the branch's own report also excluded `./...`). `./cmd/agent/` is
  not touched by this merge but IS built (`go build ./...`).
- The branch's devdoc (`devdoc/merger-inflight-both-axes.md`) was merged as-is;
  it still reports the pre-resolution state (it predates master's fix landing).
  Left unedited on purpose: it is the branch's own record.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` — OK
- Vet: `CGO_ENABLED=0 go vet ./internal/tools/` — clean
- Conflict markers: `grep -n '^<<<<<<<\|^>>>>>>>' internal/tools/task_holder_resume.go` → 0
- Targeted: `go test ./internal/tools/ -run 'TestResumeArm|TestDeadHolderResume' -count=1` — PASS
  (all four branch tests + master's `TestDeadHolderResumeArmJudgesTheCheckoutGitRegisters`
  now green together)
- Orphan check: `pgrep -af '<go-build>/tools\.test (resume|run) '` → empty after the fix
- Package + gates: `go test ./internal/tools/ ./internal/standards/ -count=1 -timeout 3600s`
  — result recorded below by the landing run.
- Commit: (this resolution commit — `resolve merge feat/merger-inflight-both-axes into master`)
