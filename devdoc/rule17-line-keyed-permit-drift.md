<!-- doc-class: historical -->
# Rule 17 line-keyed permits: one defect, two faces (drift forensics + the ratchet-down that closed it)

**Status**: fix landed on branch `task/01a12e150c6078469446a301eed0376c`, red→green
measured on two trees. **Not yet on local master `d7fb65344`** — see the last section.
**Touches**: `internal/standards/baseline.go` (4 permits deleted),
`internal/tools/taskid_whitespace_normalize_residual_test.go` (4 statements deleted).

## The defect, in one line

Four `os.Unsetenv` sites in `internal/tools/taskid_whitespace_normalize_residual_test.go`
sat at **546/547/653/654** while `testEnvBaseline` still pinned **543/544/650/651** —
so the Rule 17 gate reported the SAME four statements twice: 4 NEW violations
(`checklist 5.2`) and 4 STALE permits. A STALE entry is an error too, so the two
tallies cancelled numerically and a genuinely new Rule 17 violation would have been
dissolved into the noise.

## Forensics — which commit pushed the addresses down

Measured by walking the file's history and reading the statement's line number in
**each commit's tree** (never the worktree, which is always the tip):

| commit | `os.Unsetenv("GOPI_TASK_NOTIFY_CMD")` at | file lines | subject |
|---|---|---|---|
| `9155fa111` | :536 | 654 | fix(tools): one task_id normalization seam for every ledger entry point |
| `8c432c044` | :543 | 665 | feat(task-sync): one switch for AUTOMATIC task-row exchange, default OFF |
| `e30bb60a9` | :546 | 668 | fix(sync): the manual push's receipt names the LOCK, denies the ledger write |
| `186935607` | *(deleted)* | 668 | fix(standards): delete the redundant notify restore and the 4 stale Rule 17 permits |

**The +7 shift (`9155fa111` → `8c432c044`) is when the pins were last correct**: that
commit inserted a `t.Setenv(config.TaskSyncEnv, …)` block at ~`:68` and a 4-line
comment at ~`:603`, moving 536/537/639/640 to the 543/544/650/651 the baseline named.

**The +3 shift (`e30bb60a9`), and it is the cause of the red**: the
`fix(sync): the manual push's receipt …` commit rewrote the assertion block of
`TestSyncTaskPushOnlyPaddedIDPushesTheSameRow` at ~`:509` — **above** the 4 sites —
growing two statements into two assertions, and changed **no file under
`internal/standards/`** (`git show --stat e30bb60a9 -- internal/standards/baseline.go`
is empty). Sites 543/544/650/651 → 546/547/653/654; the permits were never touched.

`git blame` on the inserted lines agrees independently (`:509-513` → `e30bb60a9`), and
`git merge-base --is-ancestor e30bb60a9 d7fb65344` = **YES** — the shift is on master's
own history, not a branch artefact.

> The lesson generalises and is already a rule elsewhere: a **line-keyed** permit rots
> on ANY edit above it, including a comment-only rewrite. That is why the fix below
> does not re-key.

## The fix: ratchet DOWN, not re-point

The four statements were `t.Cleanup(func(){ os.Unsetenv(K) })` pairs sitting directly
after `t.Setenv(K, …)`. They are **redundant**: stdlib `testing.go:1326`
(`common.Setenv`) reads `LookupEnv` per key and its own test-scoped cleanup calls
`os.Unsetenv` when the variable started ABSENT and `os.Setenv(prev)` when it did not —
so `t.Setenv` already restores the variable *and its absence*. Deleting the statements
is therefore strictly behaviour-preserving, and it lets the permits go with them:

- 4 `os.Unsetenv` statements deleted from the test file;
- 4 line-keyed permits deleted from `testEnvBaseline` (never re-pointed, never
  replaced with an inline escape).

**Why not re-key 543→546?** Because the statements are redundant, re-keying would pin a
defect that need not exist and would leave the next edit above them to re-open the same
drift. **Why not add 4 new entries next to the old 4?** The ratchet only moves down; an
entry matching no violation is a silent permission, and 4 STALE entries would stay red.

## Measured: red tree and green tree

Both runs use the SAME command, `-count=1`, one process:

```
go test ./internal/standards/ -run 'TestStandardGates/TestGate2TestEnvRedProof|TestStandardGateDataReport' -count=1
```

| tree | Rule 17 line | STALE lines | verdict |
|---|---|---|---|
| `d4bbf6954` (the fix's parent, detached worktree) | `hits=111 baselined=62 escaped=45 NEW=4` | 4 (`:543 :544 :650 :651`) | **FAIL** |
| `186935607` (branch tip) | `hits=107 baselined=62 escaped=45 NEW=0` | 0 | **ok** |

Reproduce the red tree without a stash (this repo's 9 linked worktrees share one stash
stack, and `git stash` is refused here anyway):

```bash
cd <your repo>
git worktree add --detach <declared-worktree-root>/gopi-redproof-01a12e15 <base-commit>
cd <that worktree> && go test ./internal/standards/ -run 'TestStandardGates' -count=1
git worktree remove <that worktree> --force
```

Caveat measured here: the worktree root must be a **declared** root
(`<home>/workspace`, the scratch home, or a system temp root) — `/dev/shm/...` is
refused by the guard, with the declared roots named in the refusal.

## Evidence

- `~/.gopi/sessions/01a12e15-0c60-7846-9446-a301eed0376c/artifacts/rule17-line-drift-forensics.txt`
  — the per-commit line table, the `8c432c044→e30bb60a9` diff hunk, the
  `9155fa111→8c432c044` hunk, and the permit deletion.
- `…/artifacts/rule17-red-tree-d4bbf6954.txt` — the full red run (4 NEW + 4 STALE, exit 1).

## What is still open

The fix is **not on local master `d7fb65344`** —
`git merge-base --is-ancestor 186935607 d7fb65344` = NO, and `git show master:…baseline.go`
still carries the 4 permits while `git show master:…residual_test.go` still carries the
4 statements at 546/547/653/654. The two trees are byte-identical to each other for both
files, so master's Rule 17 gate is red for exactly the reason measured above until this
branch merges, or until a worker is dispatched against master's own copy.
