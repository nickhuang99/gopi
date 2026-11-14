<!-- doc-class: historical -->
# Task Report: resolve merge feat/merger-verify-gate into master

## Completed
- Reproduced the merge in the assigned linked worktree:
  `git merge master --no-commit --no-ff` → 2 conflicted files (UU):
  - `internal/standards/baseline.go` (2 hunks)
  - `internal/tools/repo_sweep.go` (1 hunk)
- Resolved each hunk as a UNION — neither side's intent dropped:

### 1. `internal/standards/baseline.go` — errorWrapBaseline (task_merge.go line-key)
HEAD keyed `internal/tools/task_merge.go:260`, master `:277` — the SAME single
`%v` site at different line numbers (the merged tree has exactly one such site,
at line 287). Resolution: one re-keyed entry `internal/tools/task_merge.go:287`
plus a `merge-note:` comment. Dropping a side would have been a *stale-baseline*
failure or a *new-violation* failure — the line-keys must name the MERGED line.

### 2. `internal/standards/baseline.go` — elseAfterReturnBaseline
HEAD `task_merge.go:230` (one entry), master `:250` + `:264` (two). The second
master entry was its `} else { mergeErr = err }` branch, which HEAD's ③ rewrote
into a guard (`if mergeErr == nil { ... }`); the merged tree has exactly ONE
else-after-return (`-d` chain, line 257). Resolution: one entry `:257` + note.

### 3. `internal/tools/repo_sweep.go` — `RepoSweepResult` struct tail
Both sides appended counters to the same struct tail:
HEAD added `MergeVerify []MergeVerifyResult` + `MergeVerified`/`MergeReverted`
(epic merger-verify-gate), master added `ContractRefused` (merge-time CLI
contract gate). Both are referenced in the merged tree — `task_merge.go`
writes `ContractRefused++`, `cmd/agent/cli_tasks.go` writes `res.MergeVerify`
/`MergeVerified`/`MergeReverted` — so the union keeps BOTH blocks (master's
`ContractRefused` first, then HEAD's three), single `Err` field, and the
`MergeVerifyResult` type declaration that HEAD's hunk carried.
gofmt-normalized `Err string` alignment.

## Not Done
- Full `go test ./...` — deliberately waived by the epic (known pre-existing
  full-suite hang) and never run.
- `go test ./internal/tools/` (even narrowed to `TestMergerRepoSweep|…`) hangs
  with no output in this repo — killed at the 2-minute no-output net, per the
  epic ("a hanging check never blocks your exit"). Compensated with
  `go vet ./internal/tools/` + `go build ./...` (both exit 0), which fully
  compile-checks the touched struct/fields.

## Verification
- Build: `go build ./...` → exit 0
- Tests: `go test ./internal/standards/ -run 'TestStandardGates|TestStandardGateDataReport' -count=1`
  → `ok github.com/nick/gopi/internal/standards 17.789s` (the gates whose
  baselines I re-keyed; green proves my entries match real hits and no
  entry is stale)
- Vet: `go vet ./internal/tools/ ./internal/standards/ ./cmd/agent/` → exit 0
- Markers: `grep -c -E '^(<<<<<<<|=======|>>>>>>>)'` → 0 in both files
- Post-commit state: `git rev-parse -q --verify MERGE_HEAD` → none; `git status`
  clean
- Commit: `60399588 resolve merge feat/merger-verify-gate into master`
  (branch `feat/merger-verify-gate`, worktree
  `/dev/shm/gopi-test-scratch/queue/queue-runtime-merge-resolve-615179/gopi-merge-resolve-831573684/wt`)

## Recovery
`git -C /home/user/work/GoPi show feat/merger-verify-gate:devdoc/merge-resolve-merger-verify-gate.md`
(worktree shares the main repo's refs — no push needed.)
