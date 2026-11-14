<!-- doc-class: historical -->
# Verification of `devdoc/userbus-unlinked-inode-zero-diagnosis.md`

Commands run for that verdict, and what they printed. Task
`01a12cd9-cf2b-7174-9016-3edff10748b6` (2026-10-12). Verbatim output also lives in
the run's session dir (`evidence/`); this file carries the parts a reader of the
devdoc would otherwise have to take on trust.

## 1. The isolation attempt, and the exact errors that make it impossible

`attribution.txt` records the *shape*; these are the errors.

```
$ git worktree add --detach /home/user/workspace/gopi-isolate-01a12cd9 HEAD
$ cd /home/user/workspace/gopi-isolate-01a12cd9
$ git checkout 71543117d -- cmd/agent/daemon_userbus_probe.go
$ go test ./cmd/agent/ -count=5 -timeout 8m -run 'TestUserBusProbe_RealUnlinkedSocketIsDetected' -v
```

Blob check before the build (both sides as intended):

```
probe file blob now: 40ee1d3379529e22d4ccc82460f3a3f286cec7c0   ← 71543117d's blob
test  file blob now: 138d75b5fdf81af325512668a26b2c5537d3acb7   ← HEAD's blob (untouched)
```

Build result: **12 errors**, all of them "unknown field / wrong signature" — the
fix changed the struct (`found`/`bound`/`rows`) and the return type of
`userBusKernelBinding` (`(binding, bool)` → `(binding, error)`) in the same
commit, so the old probe body cannot compile against the new test (and vice
versa). Full log: `evidence/isolation-probe-reverted.log`.

**Consequence for the verdict**: the A/B (30 PASS vs 10 FAIL) is what carries
attribution; the finer "probe or test?" question is *measured* unanswerable by
single-file revert, and that is recorded rather than papered over. The one bound
that does exist is measured: `git log --oneline 71543117d..HEAD --
cmd/agent/daemon_userbus_probe.go` → `0f9840c68` alone.

## 2. Acceptance runs on this tree (the tree the report is written on)

```
$ go test ./cmd/agent/ -count=30 -timeout 8m -run 'TestUserBusProbe_RealUnlinkedSocketIsDetected' -v
ok   github.com/nick/gopi/cmd/agent  0.500s          30 × --- PASS, 30 × === RUN, 0 FAIL
$ go test ./cmd/agent/ -count=10 -run 'TestUserBusProbe_RealUnlinkedSocketIsDetected' -v
ok   github.com/nick/gopi/cmd/agent  0.462s          10 × --- PASS, 0 FAIL
$ go test ./cmd/agent/ -count=1 -timeout 8m -run 'UserBus|userBus' -v
ok   github.com/nick/gopi/cmd/agent  5.252s          18 × --- PASS, 0 FAIL
$ go vet ./cmd/agent/
VET_RC=0
```

Logs: `head-count30-verbose.log`, `acceptance-count10-final.log`,
`family-smoke-thisrun.log`.

## 3. Baseline side (the reporter's tree)

```
$ git worktree add --detach /home/user/workspace/gopi-prefixcheck-01a12cd9 71543117d
$ go test ./cmd/agent/ -count=10 -timeout 12m -run 'TestUserBusProbe_RealUnlinkedSocketIsDetected'
FAIL github.com/nick/gopi/cmd/agent 0.387s  RC=1    10 × --- FAIL, 0 PASS
```

Each failure, verbatim:

```
daemon_userbus_probe_test.go:458: the kernel must still hold the socket after the path was
  unlinked: {… bound:true listen:false inode:0 … connectErrno:2}
daemon_userbus_probe_test.go:461: the bound socket's inode must be unchanged by the unlink:
  before 18780084, after 0
```

The inode varies per run (`18780084`…`18780112` here, `18013307` in the reporter's
run) — it is an inode, not a constant, which is why a byte-for-byte comparison of
the two logs is impossible and the *shape* is the comparison. Both check-outs were
removed after use (`git worktree list | grep -E 'prefixcheck|isolate'` → none).

## 4. Disclosure: the closure row was NOT auto-enqueued, and that is by design

Closing the row printed:

```
[verify-closure] 211 bug rows are already awaiting a verdict (cap 100) — not enqueuing
verification for 01a12cd9-cf2b-7174-9016-3edff10748b6; drain the verify backlog first
```

Read at the source before calling it anything: this is the documented **backlog
guard** — `internal/tools/task_verify_closure.go` (`taskClosureMaxAutoDispatchedCompleted = 100`,
line 128) prints it when `awaitingVerifyCount` is at or above the cap, and the file's
own comment above it says the guard "counts completed bug rows awaiting a verdict".
So it is intended behaviour, not a defect, and **no bug row was filed for it** (Rule 41
would want evidence of a *malfunction*; this is a switch doing its job). Recorded here
because a later reader of this row will see the completed status with no closure row
and should know why. Whether that backlog should be drained, and by whom, is the
operator's call — out of this run's scope.

## 5. The hash boundary — why "the file is byte-identical to `origin/master`" cannot clear this case

Measured for this run (full output in `evidence/hash-boundary.txt`):

```
$ for r in 71543117d 0f9840c68 master; do printf "%s  " $r; \
    git show $r:cmd/agent/daemon_userbus_probe_test.go | sha256sum; done
71543117d  e95f3180530a8f9f4753ac58cc77d5ee9b00fdf81b2d174e1b1f1aff27124236  -
0f9840c68  4acff90b04003cbc4f549007d2a10d6fc0079b893b9b5efb55159f4156de35d0  -
master     4acff90b04003cbc4f549007d2a10d6fc0079b893b9b5efb55159f4156de35d0  -

$ for r in 71543117d 0f9840c68 master; do printf "%s  " $r; \
    git show $r:cmd/agent/daemon_userbus_probe.go | sha256sum; done
71543117d  c0535aac2f9fd23968cc759992ffd127bd620cd74a36c73a7f201b0b0c8081d0  -
0f9840c68  43c7b9a580adcb3203d99426af35af20243355f54ee8a6ed98be5b6e0026e5a0  -
master     43c7b9a580adcb3203d99426af35af20243355f54ee8a6ed98be5b6e0026e5a0  -

$ git show --stat --format='%h %ci' 0f9840c68
0f9840c68 2026-10-12 05:10:57 +0800
 cmd/agent/cli_tasks_sync_peers_bare_test.go |  12 ++
 cmd/agent/daemon_userbus_probe.go           | 174 +++++++++++++++++++--
 cmd/agent/daemon_userbus_probe_test.go      | 224 ++++++++++++++++++++++++++--
 devdoc/DOC-INDEX.tsv                        |   1 +
 devdoc/userbus-probe-unlinked-row-fold.md   | 124 +++++++++++++++
 5 files changed, 510 insertions(+), 25 deletions(-)
```

**Read off it:** the fix moved *both* files (`e95f3180… → 4acff90b…` for the test,
`c0535aac… → 43c7b9a5…` for the probe). So `e95f3180…` is the pre-fix test file, and it
is the hash the sibling row `01a12cca-…` recorded from **its own** tree as
`sha256sum …daemon_userbus_probe_test.go` *and* from `git show origin/master:…` — i.e.
its `origin/master`, whatever commit that ref pointed at, carried the pre-fix blob.
That row therefore ran the *same* pre-fix code this run reproduced as 10/10 FAIL, and
its "byte-identical to `origin/master`" line certifies sameness with a tree that also
lacked the repair: true as written ("these reds are not from this row's changes"),
misleading as read ("this red is not real on master").

*(Rule 41 boundary, revised: the two hashes and the diffstat above are measured; the step
"therefore `01a12cca`'s `origin/master` carried `e95f3180…`" was an inference from the
briefing's recorded `git show origin/master:… | sha256sum` — and it has since been
**replaced by a direct measurement** of that row's own baseline, which survives in the
object store at its recorded HEAD `b7e5eddd6` (`evidence/sibling-identity-rerun-today.txt`):

```
$ git show b7e5eddd6:cmd/agent/daemon_userbus_probe_test.go | sha256sum
e95f3180530a8f9f4753ac58cc77d5ee9b00fdf81b2d174e1b1f1aff27124236  -
$ git show b7e5eddd6:cmd/agent/daemon_userbus_probe.go | grep -c userBusFoldUnixRows
0
$ git merge-base --is-ancestor 0f9840c68 b7e5eddd6 ; echo exit=$?
exit=1
```

That row's test file hashes to the **pre-fix** blob, its probe carries **zero**
occurrences of the symbol the fix introduces, and the fix is not its ancestor. The
inference is now the measurement — and it changes nothing about the verdict: the verdict
never rested on it.)*
