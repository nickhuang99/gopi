<!-- doc-class: historical -->
# Forward rework of the reverted merge `fix/verify-no-verdict-keeps-merge`

Task: `verify-no-verdict-keeps-merge-forward-rework`. Worktree
`/home/user/workspace/gopi-vfwd`, branch `fix/verify-no-verdict-keeps-merge`.

This devdoc records what the previous attempt got wrong, what made the merged
state flaky, and the measurements that justify each change. The FEATURE devdoc
(`devdoc/verify-no-verdict-keeps-merge.md`, restored by this branch) describes
the two fixes themselves; this one describes the re-land.

## 1. Why the merge was reverted, and what "flaky" actually was

`79738cb0` merged the branch; the post-merge verify judged FAIL and `8f0836cb`
reverted it. The reported cause was that the new `internal/daemoncwd` package is
flaky under concurrent runs. Measured, it was TWO independent defects in the
package's own test FIXTURES — never in the feature's predicate:

1. **Host-global fixture geometry.** `testWorktreeDir` rooted its fixtures at
   `Dir(scratch.HomeDir())/daemoncwd-holder-fixtures` — a directory every
   concurrent run of the package shares — and all five cases used the same leaf
   `worktree-shaped-cwd`. Two runs built the SAME path, so the first
   `t.Cleanup` `RemoveAll` deleted the directory a sibling's live child was
   standing in; `/proc/<pid>/cwd` then answers `("", false)` and the case reports
   *"the measurement itself is broken"* / *"never reported cwd"*.
2. **A `/proc` publication-order race in the readiness check (found here, not in
   the original report).** Even with unique roots, the concurrency probe caught
   one failure, on a POSITIVE case, with a child whose cwd, environ and argv0
   were all correct. `waitForProcCwd` returned on the `/proc/<pid>/cwd` match,
   but the kernel does not publish the three records at one moment: **`cwd` is
   already the child's new directory while `cmdline` and `environ` still report
   the LAUNCHER's records**, so the predicate reads the launcher's argv[0] (not
   daemon-shaped) and the launcher's environment (no scratch variable) and a
   positive case is spuriously red.

   **The mechanism is MEASURED, not assumed** (a 40-line probe,
   `/dev/shm/dwc-probe/procrace`, 200 spawns of a child given an explicit cwd and
   an argv[0] marker; cwd read BEFORE cmdline in every iteration, so a
   `cwd=new, cmdline=launcher` pair is the strong direction):

   ```
   iterations=200  both_observed=200
   cwd_new_seen_while_cmdline_still_launcher=50      (the window — 25% of spawns)
   cmdline_marker_seen_while_cwd_still_old=0         (the reverse never happens)
   ```

   A first version of this probe reported 0 windows and was WRONG: it read `cwd`
   with `os.ReadFile`, which fails `EISDIR` on a symlink-to-directory, so the
   "window" counter could never fire. The corrected read (`os.Readlink`) is what
   produced the 50/200. Both numbers are in this devdoc because a negative
   measurement from a broken instrument looks exactly like evidence.

   A narrow window — but one in four spawns is not narrow, and a loaded host
   samples it, which is exactly what a 4-way probe is for.

Neither defect weakens the fix's intent: the predicate, the assertions, the
verdict-less keep + dedup + retry cap and the reclaim arm are untouched.

## 2. A defect in the re-land itself — the 2-way `git revert`

The first re-land (`7e58a40e`) restored the revert's file list with
`git checkout <branch tip> -- <paths>`. **That silently dropped master content
that landed AFTER the branch**: 98 lines of the peak-hour verify-child exemption
(merge `9487cd75` / `69145a20`) in `cmd/agent/merger_verify_child.go` — the
clock-proof spawn, its argv contract and its record-keeping.

`git revert 8f0836cb` has the SAME defect (measured, not reasoned): it is a
2-way patch, so it deletes those lines too. The result of git's own revert was
diffed against a 3-way result and differs by exactly that block (plus a
duplicated `IsDaemonProcess` doc comment).

**The re-apply is therefore a 3-way** (`git merge-file`):

| side | revision | role |
|---|---|---|
| ours | `master:cmd/agent/merger_verify_child.go` | post-revert, with the peak feature |
| base | `8f0836cb:…` | master as the revert left it |
| theirs | `8f0836cb^:…` | the reverted branch content |

Result: `ours` + the branch's 63 lines — a diff of **63 insertions, 0
deletions**. Every one of the 10 reverted paths was audited for the same loss;
only `merger_verify_child.go` had master-only lines (the other nine: 0
deletions). The lesson is general: **a revert restores a state, not a delta —
re-land a reverted merge the same way the revert is meant to be consumed, and
verify with a 0-deletion diff against master.**

## 3. Master moved during the task

Master advanced past the reverted sha while this was in flight
(`8605d4f2` → `9487cd75`, the peak-child-exemption merge). The epic requires
master to be an ancestor, so master was merged INTO the branch, and the merge is
also the check that the re-apply did not clobber master's newer content: both
features edit the same regions of `cmd/agent/merger_verify_child.go`.

## 4. Changes in this rework (minimal diffs)

| file | change |
|---|---|
| `cmd/agent/merger_verify_child.go` | 3-way re-apply (63 insertions, 0 deletions vs master) |
| `internal/daemoncwd/daemoncwd_holder_test.go` | unique per-case root via `os.MkdirTemp` + `testFixtureRootPrefix`; readiness now waits for the child's `exec` (`envHolderHelperFlag` on cmdline); regression guard `TestWorktreeFixtureRootIsUniquePerCase`; dropped a duplicated doc comment |
| `internal/daemoncwd/daemoncwd_env_holder_test.go` | the five spawn sites and the guard reference `daemonEnvHolderWorktree` instead of retyping the leaf literal (Rule 1b/13) |
| `internal/daemoncwd/daemoncwd.go` | the duplicated `IsDaemonProcess` comment removed (diff vs master is now deletions only for that one comment — see §2) |

Constants, not literals (Rule 1b/13/14): `testFixtureRootPrefix`,
`envHolderHelperFlag`, `procCmdlineName`, `procCwdWaitAttempts`,
`procCwdPollInterval`. Rule 22: the fixture root's parent is the scratch home's
own parent — already certified off `/` at TestMain time — and the guard
MEASURES it (`testutil.ScratchRootSharesRootFS`), never spells it. Measured on
this host: `/dev/shm` dev=28 vs `/` dev=2051.

## 5. Measurements (before / after)

**Concurrency probe** — `run-probe.sh <worktree> <label> <rounds> [test-binary]`:
4 concurrent runs of `./internal/daemoncwd/` per round; a round's run FAILS when
the package reports any failure.

| state | runs | failures | signature |
|---|---|---|---|
| tip `0230f6c7` (the reverted branch, control) | 48 | **3 (6.25%)** | `never reported cwd …/daemoncwd-holder-fixtures/worktree-shaped-cwd` ×2, `measurement is unavailable` ×2 |
| `8902a116` (geometry fix only) | 48 | **1** | env-channel positive case, one failure |
| `b9526d74` (geometry + readiness) | **96** | **0** | — |
| merged state `9d4e6924` | 96 | **0** | — |
| merged state `ccadcbab`+`master` (`53489667`) — acceptance run | 24 | **0** | — |
| RED PROOF: `b9526d74` with only the readiness fix reverted | 48 | **2** | the same env-channel test → the readiness race is real |

The red proof is the useful half: it isolates the second defect (a naive
`go test -count=100` sequential run is green in every state, so repetition alone
would never have found it).

## 6. Recovery / re-land commands (the whole path, reproducible)

```bash
# 1. the branch ref survived the revert (it is an ANCESTOR of master, so a second
#    merge would fast-forward and re-apply nothing)
git -C /home/user/work/GoPi rev-parse fix/verify-no-verdict-keeps-merge   # 0230f6c7 = the tip at merge time
git -C /home/user/work/GoPi show --stat 8f0836cb                          # the 10 reverted paths

# 2. worktree (Rule 6)
git -C /home/user/work/GoPi worktree add ~/workspace/gopi-vfwd fix/verify-no-verdict-keeps-merge

# 3. re-apply the revert-of-the-revert as a 3-WAY, per file with master-only lines
git show master:$F > ours.go
git show 8f0836cb:$F > base.go        # master AS THE REVERT LEFT IT
git show 8f0836cb^:$F > theirs.go     # the reverted branch content
cp ours.go merged.go && git merge-file -L master -L base -L branch-content \
    merged.go base.go theirs.go       # exit 0 = clean
# verify the re-apply LOST NOTHING: the diff against master must show 0 deletions
git diff master --shortstat

# 4. merge the (moved) master in, then run the gates
git merge master
bash scripts/safe-merge.sh fix/verify-no-verdict-keeps-merge
```

The rig is now IN THE TREE, because the acceptance run has to reach it:

```bash
bash scripts/daemoncwd-fixture-concurrency-probe.sh ~/workspace/gopi-vfwd 6 4   # 6 rounds × 4 concurrent = 24 runs
```

`scripts/daemoncwd-fixture-concurrency-probe.sh <worktree> [rounds] [concurrency]`
builds the package's test binary once, runs N copies of it concurrently, and
fails the probe if any run fails. It takes its scratch from
`scripts/lib/tmp-ban.sh` (`gopi_scratch_mkdir`, Rule 22). It is a script and not
a `go test` case **because the defect is invisible to repetition** — the failing
geometry needs a concurrent sibling, which a single test process cannot be. The
session-local copies of the same loop are
`/dev/shm/dwc-probe/{run-probe.sh,single.sh,differential.sh}`.

## 7. What this rework did NOT do

- It did not touch the feature's production code paths (the verdict-less keep,
  the dedup, the retry cap, the reclaim arm) — the re-listed files are
  byte-identical to the recovered tip except for the two comments removed and
  the fixture/readiness changes above.
- It did not re-land the branch as a merge commit: the deliverable is forward
  commits on top of the recovered tip, because the tip is an ancestor of master.

## 8. Final acceptance at the delivered tip

The report at `tasks/verify-no-verdict-keeps-merge-forward-rework-report.md` is
written on the branch. Final branch tip `a5feedc3` (the commit that carries this
section's numbers); the verified CODE tip is
`6086c89f` — every commit after it is docs only (proved by
`git diff --stat 6086c89f a5feedc3 -- . ':(exclude)devdoc' ':(exclude)tasks'` →
empty), so the gates below run
against the same code. Latest independent re-run, on a fresh detached worktree of
`a5feedc3` (master `7b5d427a` at that moment, 2026-09-15 01:20): `ANCESTOR_OK`,
`BUILD_OK`, `go test -count=1 ./internal/daemoncwd/` ok 4.385s, and the
in-tree probe `runs=24 failures=0` (`PROBE_EXIT=0`), intent tests `ok`:

| gate | result |
|---|---|
| `CGO_ENABLED=0 go build ./...` | exit 0 (tip `a5feedc3`) |
| `go test -count=1 ./internal/daemoncwd/` | `ok` 4.385s (1.3s / 6.4–8.2s under load) |
| `scripts/daemoncwd-fixture-concurrency-probe.sh <wt> 6 4` | `runs=24 failures=0` ✅ |
| `go test -count=1 -run TestMergeVerifyNoVerdict ./cmd/agent/` | `ok` 7.520s |
| `go test -count=1 -run TestFinishCleanupWorktreeInUse ./internal/tools/` | `ok` 7.760s |
| `git merge-base --is-ancestor master HEAD` | exit 0 |

Earlier gates, at the code tip `6086c89f` (master `7b5d427a` merged in):
`go build ./...` exit 0; `git merge-base --is-ancestor master HEAD` exit 0;
`bash scripts/safe-merge.sh fix/verify-no-verdict-keeps-merge` ✅ 全部通过, exit 0
(re-run after the worktrees were released: all four items pass, [a] no archive tag,
[b] no blacklisted prefix, [c] no occupying worktree, [d] not yet merged);
intent tests (`TestMergeVerifyNoVerdict*`, `TestFinishCleanupWorktreeInUse*`) PASS.
The doc-only commits do not move those results — they add no code.

Cumulative AFTER: 0 failures / 412 concurrent runs across every run of the probe
(24 + 96 + 28 + 96 + 48 + 24 + 24 + 24 + 24 + 24), against 3/48 (6.25%) pre-fix.

The whole-suite differential was re-measured at the delivered tip (`5e15ec8b`, i.e.
master `7b5d427a` merged in) against a pristine-master worktree at the SAME sha, run
sequentially: the **branch-only FAIL set is empty** at both package and test level.
`cmd/agent`, `internal/testutil` and `internal/tools` are red identically on BOTH
trees (master's own debt and master's own red-proof test). The one test-level
difference was `TestBoundDaemonStaysAliveWhileItsSpawnerLives`, green ×3 on each side
in isolation — a load-sensitive daemon-timing flake in a file the branch does not
touch (0 files in `git diff master...HEAD`).

The branch is `master` + its forward commits (the re-land, the daemoncwd fixes,
the probe script, the docs) and `master` is its ancestor, so landing it is
a plain fast-forward — no history rewrite, and the dev worktree was removed only
after the deliverable declaration in the report (§ Deliverable declaration).
