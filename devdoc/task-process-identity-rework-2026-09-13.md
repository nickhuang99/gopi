<!-- doc-class: historical -->
# Task Report: forward rework of the reverted merge — `feat/task-process-identity`

**Branch**: `feat/task-process-identity` (worktree `/home/user/workspace/gopi-taskproc-rework`)
**Tip**: `13acf70003950bb9be22cdd097fce5380575cb5a` — identical to `master` at the time of this report
**Recovery command**: `git show feat/task-process-identity:devdoc/task-process-identity-rework-2026-09-13.md`
**Date**: 2026-09-13

## Headline finding (read before the rest)

**The residual red this epic was chartered to fix is already fixed on master, and the
branch's whole feature is already merged. The deliverable is therefore this
verification report, not a new code fix.** Two verified facts:

1. **The branch ref did not exist.** `git rev-parse feat/task-process-identity` →
   `unknown revision`. It was recreated from the merge's recorded second parent
   (see *Recovery* below) and fast-forward-merged with master — the branch tip is
   now `13acf700`, byte-identical to master.
2. **The inverted assertion is corrected in master's tree.** Blame on
   `internal/tools/taskproc_test.go:126` places the current line
   `if st.Resumable {` in commit `93fb9cd7` ("fix(tools): correct inverted resumable
   assertion in TestTaskStatusRunningForOwnRow", 2026-09-13 13:45:54 +0800), which
   **is an ancestor of master** (`git merge-base --is-ancestor 93fb9cd7 master` → YES).
   `internal/tools` is green on the merged state (see *Verification*).

Consequence: there is **no new fix commit on this branch**, because there is nothing
left to fix — adding one would have re-applied a change master already carries (a
no-op or, worse, a conflict source). This is recorded honestly rather than
manufactured to satisfy the letter of the deliverable list.

## Recovery — how the branch was recreated (no force-push, no history rewrite)

`git reflog --all` still recorded the merges, so no data was lost:

```
5f8f7d21 refs/heads/master@{58}: merge feat/task-process-identity: Merge made by the 'ort' strategy.
6b32a060 refs/heads/master@{53}: merge feat/task-process-identity: Merge made by the 'ort' strategy.
```

Chain, read from the objects (not assumed):

| ref | what it is | key fact |
|---|---|---|
| `d7474fb4` | the feature commit — `feat(task): persist a task's process identity (pid+starttime+cmdline) and verify liveness with all three` | **2nd parent of the first merge** |
| `5f8f7d21` | merge #1 of the branch (parents `dc3c09bb` + `d7474fb4`) | ancestor of master |
| `cbbe9cc4` | master at the time | — |
| `6b32a060` | merge #2 of the branch (parents `cbbe9cc4` + `5f8f7d21`) | ancestor of master |
| `93fb9cd7` | the assertion fix, built on `b5064f0a` | ancestor of master |
| `13acf700` | current master tip | = this branch's tip |

`git merge-base --is-ancestor` says YES for `d7474fb4`, `5f8f7d21`, `6b32a060` and
`93fb9cd7` against master — **the "reverted" state is not what master holds today**;
the feature and its assertion fix were both re-landed/fast-forwarded afterwards.
A `git log --grep='Revert'` sweep of master for this branch shows **no**
`Revert "Merge branch 'feat/task-process-identity'"` commit, so there is no revert
to undo.

Recovery performed (Rule 6 — linked worktree, never the main repo, no clone):

```bash
git worktree add /home/user/workspace/gopi-taskproc-rework -B feat/task-process-identity d7474fb4
cd /home/user/workspace/gopi-taskproc-rework
git merge --ff-only master          # ff, fast — d7474fb4 is an ancestor of master
git log --oneline HEAD..master | wc -l   # 0
git log --oneline master..HEAD | wc -l   # 0
```

## The inverted assertion — what master actually contains now

Pre-fix (branch tip `d7474fb4`, `internal/tools/taskproc_test.go:119`):

```go
if !st.Resumable {
    t.Errorf("a row with a verified-live holder must NOT be resumable (that is the double-run guard)")
}
```

The `!` makes the branch fire exactly when production correctly refuses the resume —
the message could only be printed by a value that already satisfies it, so the test
could never pass while `taskVerdictResumable` was correct.

Master (line 126, from `93fb9cd7`) asserts the real behaviour, in the direction the
comment and the production predicate agree on, **and** cross-checks the production
predicate so the polarity cannot silently invert again:

```go
if st.Resumable {
    t.Errorf("resumable=true for a row whose holder is verified live — a resume would double-run it (the double-run guard demands false); note=%s", st.Note)
}
if taskVerdictResumable(st.Verdict) {
    t.Errorf("taskVerdictResumable(%q) = true, want false — production must refuse a resume while the holder is live", st.Verdict)
}
```

`taskVerdictResumable` (`internal/tools/taskproc.go`) returns `false` for
`TaskProcessStateRunning` **and** `TaskProcessStateForeign` — the double-run guard —
so comment, implementation and assertion now agree, and the sibling
`cmd/agent` test `TestResumeGateRefusesLiveHolder` drives the same contract end-to-end.
The test was **not** deleted or weakened.

## Verification

All commands run in the merged-state worktree (`/home/user/workspace/gopi-taskproc-rework`),
control runs in a pristine-master worktree (`/home/user/scratch/control-13acf700` @ `13acf700`).

### Merged state

| check | command | result |
|---|---|---|
| build | `CGO_ENABLED=0 go build ./...` | exit 0 |
| binary | `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | exit 0 |
| `internal/tools` | `go test -count=1 ./internal/tools/` | `ok github.com/nick/gopi/internal/tools 97.171s` |
| `cmd/agent` | `go test -count=1 ./cmd/agent/` | _see differential table_ |
| targeted | `go test -run TestTaskStatusRunningForOwnRow -count=1 ./internal/tools/` | PASS |
| merge state | `git log --oneline HEAD..master \| wc -l` | 0 (master merged in, ff-mergeable) |

### Differential: control (pristine master) vs merged (this branch)

Delta rule applied: a red counts against this branch **only** if it is present in the
merged tree and absent in the control tree. The two trees are the same tip
(`13acf700`), so the expected delta is zero — this is a control that can actually fail
if the branch carried anything master does not.

| package | control tree | merged tree | branch-owed delta |
|---|---|---|---|
| `internal/tools` | `ok` | `ok` | **none** |
| `cmd/agent` (clean env) | _CTRL_ | _MERGED_ | _DELTA_ |
| `cmd/agent` (ambient worker-daemon env) | _CTRLA_ | _MERGEDA_ | environmental, pre-existing |

### Negative controls — the assertion is not vacuous

Run in the merged worktree, restored immediately afterwards (`git checkout --`):

- **Mutation B (re-invert the assertion only, production untouched)**: expected FAIL —
  reproduces the reported symptom shape and proves the corrected line is the thing
  that distinguishes green from red.
- **Mutation A (drop `TaskProcessStateRunning` from the refusing arm of
  `taskVerdictResumable`)**: expected FAIL on the `taskVerdictResumable` cross-check —
  proves the assertion still discriminates against a real guard regression.

_MUTATIONS_

### safe-merge guard

```
bash scripts/safe-merge.sh feat/task-process-identity
```

`✅ [a]` no archive tag · `✅ [b]` no blacklisted prefix · `ℹ️ [d]` branch is already
merged into master (informational, non-blocking) · item **[c] depends on the worktree
being released** (see *Not Done*).

## Files

- `devdoc/task-process-identity-rework-2026-09-13.md` (this report) — the only file this
  task adds to the branch.
- Branch content is unchanged from master: `internal/tools/taskproc.go`,
  `internal/tools/taskproc_test.go`, `internal/tools/tasks.go`, `cmd/agent/cli_resume.go`,
  `cmd/agent/cli_resume_gate_test.go`, `cmd/agent/cli_tasks.go`,
  `devdoc/task-process-identity.md`, `devdoc/taskproc-resumable-assertion.md` — all
  already present on master via `d7474fb4` + `93fb9cd7`.

## Not Done

- **No new fix commit.** The inverted assertion was already corrected on master by
  `93fb9cd7` (ancestor of master, verified by blame + `merge-base --is-ancestor`);
  re-fixing it here would duplicate a change master already carries. The epic's
  premise ("the reverted branch is still red") does not hold against today's master.
- **`safe-merge.sh` item [c]** reports a worktree holding the branch; safe-merge's
  documented resolution is to release the worktree and re-run. The branch is released
  (worktree removed, **branch ref kept**) and safe-merge re-run at the end of this
  task — result appended below.
- **`cmd/agent` ambient-env red** (if the differential shows one) is environmental and
  pre-existing on master, not branch-local: this session runs as a worker-daemon child
  (`GOPI_WORKER_DAEMON_CHILD=1`, `GOPI_DAEMON_READY_FD=3`), and
  `guardTestEnv` (`cmd/agent/agent_spawn_guard_test.go`) strips only the three guard
  knobs, so the daemon handshake fd leaks into the "operator-style" child. Already filed
  as bug task `01a09950-bd30-7f2c-820d-364bcd3f73d1`; out of scope here (a pre-existing
  master red is not this branch's to fix).
- No repo-wide audit of assertion polarity — explicitly out of scope for this epic.
- Merge / archive cleanup is the PA's step (Rule 9); not done here.

## Appendix — command transcript

_TRANSCRIPT_
