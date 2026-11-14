<!-- doc-class: normative -->
# The lifecycle litter fixture named no writer, and the only symptom was `disposed=0`

**Task**: `01a12b7924cb7eee995d3125dd372de4` (branch
`task/01a12b7924cb7eee995d3125dd372de4`)
**Date**: 2026-10-11  **Host**: host-b  **Base**: master `a3a31da42`
**Epic**: `task-ledger-auto-row-disposal`
**Predecessor doc**: `devdoc/lifecycle-litter-fixture-drift.md` (same incident,
diagnosis only — the repair it describes does not reach master; see "What this
run adds")

## The anomaly, as reported

`TestTaskLifecycleTick_DisposesTheNeverStartedAutoRow` (`cmd/agent`) was reported
FAIL on the peer master that contains `71577959e` ("reach the auto registrations
the disposer could not see") and PASS on the reporting host's local master.
The briefing's hypothesis was that the fixture seeded a row the narrowed
predicate no longer qualifies.

## What was measured on this host

**The premise is refuted: local master is RED too, and the peer master was
never the problem.** `71577959e` is an ancestor of local master `a3a31da42`;
`29af4042a` (the fixture repair) is **not**. Reproduced on master's own tree,
checked out detached (`git worktree add --detach <path> master`, then the test
run inside it — the tree is master's, unmodified):

```
$ git -C /home/user/workspace/gopi-master-repro-01a12b79 log --oneline -1
a3a31da42 Merge branch 'task/01a12b498921768489284c17e259417f'

$ go test -count=1 -run '^TestTaskLifecycleTick_DisposesTheNeverStartedAutoRow$' ./cmd/agent
--- FAIL: TestTaskLifecycleTick_DisposesTheNeverStartedAutoRow (0.81s)
    task_lifecycle_litter_test.go:95: the never-started auto row is still "in_progress" after the tick — this is the reported defect:
        [gopi] [lifecycle] resident task-lifecycle sweep (stale report + confirmed-zombie disposal + finish cleanup) active on this process; every 10m0s
        [worker] [lifecycle] stale reported=2 disposed=0 sink=cat >> /dev/shm/suite-cmd-agent-run-…/notify.log
FAIL
FAIL	github.com/the operator/gopi/cmd/agent	4.475s
```

`stale reported=2 disposed=0` is the peer's symptom verbatim: the stale-report
leg counts both seeded rows, the disposal leg reaches neither. `disposed=0` is
the whole observable distance between the cause and the operator — the cause is
in a fixture, and the symptom appears in the middle of a worker-round tick.

## The root cause, proven by varying ONE thing

Same tree, same production code, only the fixture's summary spelling changed —
`Summary: "lifecycle fixture " + id` (master) versus `Summary: "[auto] lifecycle
fixture " + id`. Nothing else touched; the tree restored with `git checkout -- .`
afterwards.

```
# master's tree untouched            → FAIL   (the run above)
# master's tree, prefix flipped only → ok     github.com/the operator/gopi/cmd/agent	3.549s
```

The mechanism, line-verified:

* `71577959e` moved the extinction pass's identifying question off
  `run_method = 'auto'` and onto the harness REGISTRATION —
  `autoRunLitterCandidateWhere()` returns
  `status = ? AND run_method IN (?, ?) AND summary LIKE ?`, with the LIKE
  pattern built from `autoRunLitterSummaryPrefix + "%"`
  (`internal/tools/taskzombie_extinct.go`).
* The fixture seeded a row summarised `"lifecycle fixture t-litter-never-started"`
  — **no marker** — so the row the pass was meant to dispose was never a
  candidate. It passed only while the predicate asked the method column.
* `71577959e`'s own argument is that this is CORRECT behaviour, not a
  regression: a row that is not verifiably the harness's registration must not
  be collected, because the correct negative (an imported row) is
  indistinguishable from a false negative. The predicate did not narrow into
  wrong; the fixture had stopped describing the harness.

So the defect is **entirely in the fixture**. `internal/tools`' production
predicate is right as it stands, and no production predicate is touched here.

## The peer's master is already fixed — the "must it land?" question

```
$ git fetch peer1 master && git log --oneline -1 peer1/master
ec7342d72 Merge branch 'task/01a12b865e8b768ba37482de4422a489'
$ git merge-base --is-ancestor 71577959e peer1/master && echo YES
YES
$ git merge-base --is-ancestor 29af4042a peer1/master && echo YES
YES
$ git show peer1/master:cmd/agent/task_lifecycle_litter_test.go | grep -n 'Summary:'
71:  Summary: lifecycleLitterSummary(LifecycleLitterBriefing + " " + LifecycleLitterFixtureID),
79:  Summary: lifecycleLitterSummary(LifecycleLitterBriefing + " " + LifecycleLitterRunDirFixtureID),
```

The peer moved past the red while this task was being written: its master now
carries the fixture repair. **The peer does not need another commit, and this
branch does not need to be its carrier.** What was still missing, and what this
branch supplies, is the reason a fix of this shape will not hold — see below.

## What this run adds: the repair was invisible at its own layer

`29af4042a` fixed the fixture and added a precondition — but the precondition it
added is one string assertion inside the ONE end-to-end test:
`strings.HasPrefix(got, agent.AutoTaskSummaryPrefix)`. That catches the *next*
time the summary is edited in that test. It does not catch the case that actually
happened, and that the surrounding evidence proves is live:

* `internal/tools/taskzombie_extinct_test.go`'s
  `TestAutoRunLitterSummaryPrefixMatchesTheAgentRegistrationHook` reads the
  `[auto] ` constant out of `internal/agent/taskregister.go` and compares it to
  **the same package's own copy**. It reads the mirror against the mirror: the
  implementation of `autoRunLitterRegistrationSummary` lives in that package too,
  so the pair holds no matter how far BOTH drift from what the pass will actually
  accept. A drift-affirming test — a false green in its purest form.
* `db:memory` (the reader scaffold, md5 `ec1c93ce8e2e0285c07f4ac05cdab35a`)
  carries `TestAutoRunLitterSummaryPrefixMatchesTheAgentRegistrationHook` and **not**
  `TestAutoRunLitterCandidateQueryAgreesWithTheClassifier`, the one test that
  relates the SQL half to the Go half. So the agreement property the commit
  message leans on is *not asserted in the artifact the peer's tree ships*.

The repair therefore rests on a test that cannot fail for the reason it exists.
This branch replaces that with a pin that CAN:

1. **`tools.AutoRunLitterCandidateShape(summary, runMethod)` is exported**
   (`internal/tools/taskzombie_extinct.go`) — the candidate predicate as a pure
   function of the row's two signature columns, the Go half of the query, and
   deliberately the classifier's own body rather than a re-derivation.
2. **`cmd/agent` asks it about the row it just minted.** In
   `TestTaskLifecycleLitterFixtureIsReachableByTheDisposer`, the fixture's own
   summary comes from `agent.AutoTaskSummaryPrefix` (cmd/agent CAN import agent —
   only `internal/tools` cannot, since agent imports tools), and the pass's
   predicate is asked through the exported function. This is the independent
   second leg the `internal/tools` test structurally cannot supply.
3. **The fixture asserts its own reachability at SEED TIME.** `seedLifecycleLitterRow`
   reads the row back from the LEDGER and refuses to let the test proceed if
   `AutoRunLitterCandidateShape` says the pass cannot see it — so the next
   occurrence fails **at the fixture, with the offending summary quoted**, instead
   of surfacing as `disposed=0` in a tick's stdout three layers away.
4. **The pin is non-vacuous in both directions**: direction 2 feeds it master's
   drifted summary verbatim and requires a refusal, so the check is observably
   able to say NO.

## The red proof (the pin catches the exact span that broke)

Fixture reverted to master's marker-less spelling, everything else fixed:

```
--- FAIL: TestTaskLifecycleTick_DisposesTheNeverStartedAutoRow (0.04s)
    task_lifecycle_litter_test.go:71: fixture precondition: the extinction pass CANNOT SEE the row
        t-litter-never-started just seeded (summary "lifecycle fixture t-litter-never-started",
        run_method "auto") — it is not a harness registration …
--- FAIL: TestTaskLifecycleLitterFixtureIsReachableByTheDisposer (0.00s)
    task_lifecycle_litter_test.go:161: the fixture's own registration "lifecycle fixture
        t-litter-never-started" is not reachable by the extinction pass …
```

Same one variable as the mechanism proof above, opposite direction: the drift is
now a failure of the FIXTURE, named as such, at the point of seeding.

## Files

* `cmd/agent/task_lifecycle_litter_test.go` — the seeding helper + the
  reachability pin + the two-direction negative control.
* `internal/tools/taskzombie_extinct.go` — exports
  `AutoRunLitterCandidateShape`; the mirror's comment now records that cmd/agent
  needs no mirror.
* `internal/tools/taskzombie_extinct_test.go` — the mirror test states what it
  genuinely pins (the constant's value), and adds the non-vacuity guard.

## Not done (honest)

* **No production predicate was changed, deliberately** — the measured verdict
  is "the fixture was stale, not the predicate", and widening the pass to reach
  marker-less rows is the "guess wearing a name" `71577959e` refuses by design.
* **`29af4042a` is not on this branch's ancestry from master** — this branch
  carries it (via the `14a828d1c` merge), so the acceptance test is green here,
  but the *repair to the fixture* as such is the peer's to keep. What this branch
  contributes on top is the pin; if the two are merged, the pin is the only
  conflict surface (both edit the same test file).
* `db:memory` was inspected read-only; no claim is made about why that artifact
  carries one test and not the other — only about what it contains.

## Verification

### Build

```
$ go build ./...
BUILD_OK
$ gofmt -l cmd/agent/task_lifecycle_litter_test.go internal/tools/taskzombie_extinct.go internal/tools/taskzombie_extinct_test.go
(no output)
```

### Tests

```
$ go test -count=1 -run '^TestTaskLifecycleTick_DisposesTheNeverStartedAutoRow$' ./cmd/agent   # ×3
ok  	github.com/the operator/gopi/cmd/agent	4.211s
ok  	github.com/the operator/gopi/cmd/agent	4.007s
ok  	github.com/the operator/gopi/cmd/agent	3.732s
```

(3/3, the acceptance criterion.)

The touched packages, whole, at the committed tip `bbf9649a3`:

```
$ make test-budget PKGS='./cmd/agent ./internal/tools'
TIP=bbf9649a3346083d4dc0f4c659d2b67f23d7ec04
```

* `internal/tools`: **pass** (elapsed 380.1s) — the whole package, including the
  three tests this diff touches and `TestAutoRunLitterCandidateQueryAgreesWithTheClassifier`.
* `cmd/agent`: the run reports three failures, and **all three are pre-existing
  master red, not this diff's**: `TestChatVoice_TheRunOfFailuresEndsTheMode`,
  `TestChatCookedFallback_NonTTY`, `TestRunPathPlainPromptIsVerbatim`. Measured
  on master's OWN tree (unmodified, `HEAD a3a31da42`) the last two fail
  identically:

  ```
  $ cd <detached master worktree> && git status --porcelain   # (no output)
  $ go test -count=1 -run 'TestChatVoice_TheRunOfFailuresEndsTheMode|TestChatCookedFallback_NonTTY|TestRunPathPlainPromptIsVerbatim' ./cmd/agent
  --- FAIL: TestChatCookedFallback_NonTTY (15.44s)
  --- FAIL: TestRunPathPlainPromptIsVerbatim (0.31s)
  FAIL	github.com/the operator/gopi/cmd/agent	19.931s
  ```

  None of the three names a file this diff touches, and the acceptance test —
  the subject of this task — is green 3/3 in the same tree.

### Ran

```
# 1. the peer's failure reproduced against master's OWN tree (detached worktree)
$ git worktree add --detach /home/user/workspace/gopi-master-repro-01a12b79 master
$ cd /home/user/workspace/gopi-master-repro-01a12b79
$ go test -count=1 -run '^TestTaskLifecycleTick_DisposesTheNeverStartedAutoRow$' ./cmd/agent

# 2. the mechanism, ONE variable: master's tree + the fixture's summary prefixed
$ python3 -c '…replace Summary: "lifecycle fixture " + → "[auto] lifecycle fixture " +…'
$ go test -count=1 -run '^TestTaskLifecycleTick_DisposesTheNeverStartedAutoRow$' ./cmd/agent
$ git checkout -- .    # tree restored

# 3. the peer's master state
$ git fetch peer1 master && git merge-base --is-ancestor 29af4042a peer1/master

# 4. the pin's red proof (fixture reverted to master's spelling, rest fixed)
$ go test -count=1 -run 'TaskLifecycleLitter|TaskLifecycleTick_DisposesTheNeverStartedAutoRow' ./cmd/agent
```

### Result

1. `--- FAIL … disposed=0` — master is red, the premise refuted (raw output above).
2. `ok github.com/the operator/gopi/cmd/agent 3.549s` — one variable flips the colour,
   so the mechanism is the fixture's summary, not the predicate.
3. `YES` for `71577959e`, `YES` for `29af4042a` — the peer's master is already
   repaired; nothing more needs to land there.
4. Both new pins FAIL with the offending summary quoted in the message.

### Commit

The tip of `task/01a12b7924cb7eee995d3125dd372de4` — `git log -1 --format='%H %s'`
at hand-over names it. The hash is written this way on purpose: **this report
rides in that same commit**, so a hash typed into it here would name the commit
before the report was added and be stale by construction. The commit subject is
`fix(cmd/agent): the litter fixture must prove the disposer can SEE it, not
mirror the disposer's own constant`, and the measured tip hash is recorded in
this run's own session evidence
(`~/.gopi/sessions/01a12b79-24cb-7eee-995d-3125dd372de4/`).

## Evidence (self-filed)

* master red, raw: `go test -count=1 -run '^TestTaskLifecycleTick_DisposesTheNeverStartedAutoRow$' ./cmd/agent`
  in `/home/user/workspace/gopi-master-repro-01a12b79` (detached at `a3a31da42`)
  → `--- FAIL … task_lifecycle_litter_test.go:95 … stale reported=2 disposed=0`, exit 1.
* one-variable flip → `ok github.com/the operator/gopi/cmd/agent 3.549s`, exit 0; tree
  restored, `git status --porcelain` empty in that worktree afterwards.
* ancestry: `git merge-base --is-ancestor 29af4042a master` → exit 1 (absent);
  same against HEAD → exit 0 (present). `peer1/master` at `ec7342d72`, both
  commits present (exit 0 each).
* prefix const: `internal/agent/taskregister.go:101` `AutoTaskSummaryPrefix = "[auto] "`;
  `internal/tools/taskzombie_extinct.go` `autoRunLitterSummaryPrefix = "[auto] "`.
* predicate: `autoRunLitterCandidateWhere()` → `status = ? AND run_method IN (?, ?) AND summary LIKE ?`
  with `autoRunLitterCandidateArgs()` → `{taskStatusInProgress, "", runcmd.MethodAuto, prefix + "%"}`.
* red proof: the two FAIL blocks quoted verbatim in "The red proof" above.
* `db:memory` reader, md5 `ec1c93ce8e2e0285c07f4ac05cdab35a`: contains
  `TestAutoRunLitterSummaryPrefixMatchesTheAgentRegistrationHook`; does not
  contain `TestAutoRunLitterCandidateQueryAgreesWithTheClassifier`.
* this worktree at close-out: `git status --porcelain` → empty (Rule 30).
* touched-package suite at tip `bbf9649a3`: go test JSON stream
  `…/test-budget/run-1791735065-4061818.json` — `internal/tools` `pass`
  (elapsed 380.119); `cmd/agent` failures were `TestChatVoice_TheRunOfFailuresEndsTheMode`,
  `TestChatCookedFallback_NonTTY`, `TestRunPathPlainPromptIsVerbatim`, none of
  which names a file this diff touches. The last two reproduced on master's own
  tree (unmodified) → `FAIL github.com/the operator/gopi/cmd/agent 19.931s`.
* `git show --stat HEAD` — 5 files: the fixture, the exported predicate, the
  mirror test, the report, the index row.
