<!-- doc-class: historical -->
# The fixture leak: a test's close-out chain pushed into the REAL repo

Record of a fix (2026-09-22, branch `fix/fixture-leak-temp-root`, epic
`fix-fixture-leak-temp-root`). Historical: it describes one run's measurement
and the change it produced, not a standing contract.

## Symptom

`refs/heads/feat/sync-conflict-completed` kept appearing in the REAL repository
(`/home/user/work/GoPi`) — an orphan history (`a.txt` = `hello` / "init",
`b.txt` = `deliverable` / "deliverable work", author `t <t@t>`) — and it came
back with a **new root commit every run**, which is the tell: not a stale ref
being re-pushed, but a test re-creating it. It had been deleted by hand four
times, each time tagged (`orphan/sync-conflict-completed`,
`orphan/sync-conflict-completed-v2`, `archive/feat-sync-conflict-completed-20260921`).

## The leaking path (located, not inferred)

`internal/tools/task_finish_cleanup_test.go` — `newFinishCleanupFixture` builds
the fixture under `t.TempDir()` (correct), and `finishCleanupFixture.runChain`
injects `f.serverDir` (correct). The leak is the OTHER arm of
`TestFinishCleanupGoneWorktreeNamesTheUndeclaredPeer`:

- `internal/tools/task_finish_cleanup_test.go:2290` — the arm's
  `t.Setenv(config.SyncPeersEnv, "peerbox")` (master's line numbers; the arm
  begins "The note is owed only when the undeclared peer IS the condition")
- `internal/tools/task_finish_cleanup_test.go:2294` —
  `taskFinishCleanupChain(ctx, rep, deliverable, false)` (the target is the
  production resolver's, never injected)

`taskFinishCleanupChain` (production, `internal/tools/task_finish_cleanup.go:1559`,
resolving the target at `:1607` from `finishCleanupServerTarget()` at `:1652`)
calls `finishCleanupServerTarget()`, which renders
`<first declared peer>:<finishCleanupPeerRepo>` — the peer is `peerbox`, and
`finishCleanupPeerRepo = "/home/user/work/GoPi"`. The chain then executed:

```
git push 'peerbox:/home/user/work/GoPi' feat/sync-conflict-completed:feat/sync-conflict-completed
```

**Why the resolved directory is the real repo, on this host (peerbox):**

1. `peerbox` is the host's OWN ssh alias — `/home/user/.ssh/config` `Host peerbox` →
   `HostName 10.0.0.6`, i.e. loopback-adjacent. `hostname` is `peerbox`.
2. `~` in `/home/user/work/GoPi` is expanded by the receiving `git-receive-pack` on
   `peerbox`, i.e. to `/home/user/work/GoPi` — the real repo.
3. The fixture's temp client repo is the SENDER: it has the branch object, so
   the push is accepted and `refs/heads/feat/sync-conflict-completed` is created
   in the real repo with whatever object that run's fixture made — a new root
   each run.

Evidence: `GIT_TRACE` over `go test ./internal/tools/ -count=1` contains
`git push 'peerbox:/home/user/work/GoPi' …` followed by
`ssh peerbox 'git-receive-pack '~'/work/GoPi''`, at the same second the real
repo's ref file appeared (`07:05:09.6`), with reflog entry
`0000…→e06f787b … push` and author the COMMITTER of the running process
(`Qingzhe Huang`, not the fixture's `t <t@t>` — the fixture's objects were
received, its identity never wrote the ref).

The same suite was RUNNING CONCURRENTLY in a second worktree
(`/home/user/workspace/gopi-stash-pred`, a worker of task `01a0c54a`), which is why the
first reproduction looked intermittent: it was mine, but the residue often
predated my run. The trace settles it — the leak path is in the code, and it
reproduces in a single package run.

## The fix

One gate at the chain's push point, `runFinishCleanupChainTarget`:
`finishCleanupTargetLeavesTestSpace(target)`.

- **Are we a test?** `testing.Testing()`, plus the marker
  `GOPI_FINISH_CLEANUP_TEST_PUSH` for RE-EXEC'd test children (real
  subprocesses report `Testing()==false`).
- **Is the recipient outside test space?** A PATH property: the LOCAL side of
  the target (after the `alias:` split, tilde-expanded against this process's
  home) must resolve under a temp/scratch root in force
  (`GOPI_TMPDIR` / `TMPDIR`). `/home/user/work/GoPi` does not; the fixture's own bare
  repo does. No alias spelling is special-cased.

The marker is the documented opt-out, used only by arms that push to their own
temp root (`allowTestPushTarget` + `localTestPeerTarget` in the test file). The
leaking arm now declares the same peer (its case is "a peer IS declared") but
INJECTS a target inside its own fixture: the same `runFinishCleanupChainTarget`
seam the push-contract tests already use. Plan mode (`planOnly`) is out of
scope: it writes nothing.

## RED proof

The regression test was run on unmodified master's production code (pristine
detached worktree with only the new test file copied in), with an `ssh` shim
first on `PATH` so the red run could not itself leak into the real repo:

```
--- FAIL: TestFinishCleanupInProcessTestCannotPushOutsideTestSpace
    task_finish_cleanup_test.go:2477: ① an in-process test push toward a real node
      (peerbox:/home/user/work/GoPi) must be REFUSED by the fence, got skipped=false reason=""
shim log:
  SHIM-SSH -o SendEnv=GIT_PROTOCOL peerbox git-upload-pack '/home/user/work/GoPi'
  SHIM-SSH peerbox git-receive-pack '/home/user/work/GoPi'
```

Against the fixed code the same test passes, and its positive-control arm still
delivers to the fixture's own bare repo.

## Not done / residual

- The residue ref `refs/heads/feat/sync-conflict-completed` (and
  `refs/tags/orphan/*`, `refs/tags/archive/*`) is NOT touched by this change:
  the ticket forbids a post-hoc janitor and forbids touching those tags. The
  regression test therefore pins "the ref must not MOVE across a run"
  (`before == after`) and REPORTS a pre-existing residue instead of asserting
  absence. After the operator deletes the residue, the same test reads the
  acceptance form (absent before → absent after).
- Sibling fixtures were scanned and classified — the table is committed beside
  this record as `devdoc/fixture-leak-sibling-scan.tsv`. One confirmed near-miss
  family (`GitSyncPushLocalBranches` with a path-shaped
  target under `/tmp`) is out of this change's reach: `TMPDIR` is often set to a
  path beside the repo, so a valid fixture is a legitimate REFUSAL there. The
  clean repair is a proper temp root for that fixture (a sibling ticket).
- General fence (refuse an outside-history ref at the commit/push path) and the
  `gopi merge` unrelated-history janitor are separate tickets by design.
