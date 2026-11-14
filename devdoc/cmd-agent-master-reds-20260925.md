<!-- doc-class: normative -->
# Two master reds measured 2026-09-25: the screen-assertion gate's 5 hits, and `./cmd/agent`'s 10-minute package budget

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task row `01a0d530-ed6d-7e42-b093-3baf6336bf00` (worker), branch
`task/01a0d530ed6d7e42b0933baf6336bf00`, worktree
`/home/user/workspace/gopi-01a0d530ed6d7e42b0933baf6336bf00`.

**Base of measurement: pristine master `04e96589`** — a detached worktree
(`/home/user/workspace/gopi-mred-baseline-01a0d530`) created for this task only,
nothing checked out of anybody else's ground (Rule 29).

Both signals were reported by an earlier run as PRE-EXISTING reds on master and
found while branch-scoped verification for `01a0d513` (chat silent exit) ran. The
briefing asked for exactly one thing: confirm each signal is master's own (not the
sibling branch's), and for the screen-assertion one, reach green **without
weakening the gate**.

---

## 1. `internal/standards` — the screen-assertion gate flagged 5 lines in `internal/uiclient/dump_test.go`

### What the gate is for

`TestScreenAssertionDoesNotSyncOnProducerCounters` (`internal/standards/screen_assertion_gate_test.go`)
exists to catch one shape: a test that **waits on a producer's counter**
(`Writes()`, `Frames()`, …) to decide that a screen is finished. Such a wait
asserts a screen no producer promised to finish — the counter tracks what the
writer DID, not what the terminal shows — so the assertion passes for the wrong
reason and the test is a proxy measurement that can never fail when the render
breaks.

The gate's own words, in the violation it printed:

> `[Writes() is a PRODUCER counter — a test that waits on it asserts a screen no`
> `producer has promised to finish; wait on the assertion's own subject (the`
> `rendered row), or (for a direct measurement) mark the line //screen-ok: <what it measures>]`

So the gate declares **two legitimate exits**: assert the subject, or mark the
line as a direct measurement with `//screen-ok: <what it measures>`. It is a
DESIGNED escape, not a mute: the marker must state what the line measures.

### Why the 5 lines are the second shape, not the first

`internal/uiclient/dump_test.go` (added by master `ce0bb4eb`, "gopi ui exports its
own screen as text") reads `d.Writes()` at lines 165, 174, 186, 200, 307. Each
read is a measurement of **the dumper's own install throttle**, taken
synchronously after a `Write` returned — never a wait on a producer:

| line | what the read measures |
|---|---|
| 165 | the install COUNT after construction (no screen is being produced, no row awaited) |
| 174 | that the throttle SUPPRESSED an install (the unchanged frame is byte-identical on disk, so the counter is the only observable) |
| 186 | the install the interval EXPIRY earned |
| 200 | the unchanged-rendering SUPPRESSION (an identical body is not rewritten) |
| 307 | that the off-file mode installs NOTHING (no path ⇒ no file to assert on) |

Three of the five have no file-level observable at all: an unchanged frame is
byte-identical on disk, so `Writes()` is not a proxy for the assertion's subject —
it **is** the subject. The fix is therefore the gate's own documented per-line
escape, each naming what it measures, and NOT a baseline entry: a baseline entry
would claim these are legacy sites the ratchet is waiting to remove, which is
false, and would teach the next reader that the gate's 5 hits are debt.

### Red → green, and the gate left intact

```
# pristine master 04e96589, gate alone
$ go test -count=1 -run 'TestScreenAssertionDoesNotSyncOnProducerCounters' ./internal/standards/
--- FAIL: TestScreenAssertionDoesNotSyncOnProducerCounters (1.03s)
    screen_assertion_gate_test.go:173: NEW violation … at internal/uiclient/dump_test.go:165
    … :174   … :186   … :200   … :307        (5 hits)

# the branch carrying only the 5 escapes
$ go test ./internal/standards/ -count=1
ok  	github.com/the operator/gopi/internal/standards	33.646s        (exit 0)
$ go test -count=1 -run 'TestScreenAssertionCountersRedProof' ./internal/standards/
ok  	github.com/the operator/gopi/internal/standards	0.194s         ← the gate still bites (its probe is untouched)
$ git diff master -- internal/standards/ | wc -l
0                                                           ← the gate package is byte-identical to master
```

Untouched, on purpose: `screenAssertionMinTestCallSites = 4` (the read floor — a
gate must still find call sites to judge), the `//screen-ok:` escape constant, the
probe, and `internal/standards/baseline.go`. The diff is 6 insertions / 5 deletions
in `dump_test.go` — comments only; the test bodies are unchanged and
`go test ./internal/uiclient/ -count=1` → `ok … 1.002s` (all 20 dumper/dump tests
PASS).

### A third master red met on the way (already fixed where this run stands)

On PRISTINE master the same package ALSO fails
`TestDocClassBannerMatchesManifest`:

```
docclass_gate_test.go:205: devdoc/handoff-20260924-preflight.md: class "historical" but the
    file does not open with its banner — first line must be "<!-- doc-class: historical -->"
docclass_gate_test.go:205: devdoc/worker-resume-ground.md: class "historical" but …
```

That is NOT this signal, and it is not fixed here: the sibling task `01a0d513`'s
branch (`eb03e5fc`, still open, row not yet merged) already repairs exactly those
two files, and this run's worktree was created ON that branch. Measured, not
assumed: `git show eb03e5fc:devdoc/handoff-20260924-preflight.md | head -1` →
`<!-- doc-class: historical -->`. So the recorded base of this worktree is master
+ that pending repair, which is why `go test ./internal/standards/ -count=1` is
green there and red on a bare master worktree for a second, unrelated reason.

---

## 2. `./cmd/agent` — whole-tree `go test` hits Go's 10-minute package budget. VERDICT: master-red, and the class is load × first-open ledger cost, not a hang in one test

### The measurement (pristine master, nothing from any branch)

```
$ cd /home/user/workspace/gopi-mred-baseline-01a0d530 && git rev-parse HEAD
04e9658938260a1f3c3e8c8484e24b0193b7ac56
$ echo "GOPI_BIN=${GOPI_BIN:-<unset>}" ; ls bin 2>&1
GOPI_BIN=<unset>
ls: cannot access 'bin': No such file or directory
$ go test -count=1 -v ./cmd/agent/          # NO -timeout: Go's own 10m default
…
=== RUN   TestClaimGroundStampNoRepoIsANoop
panic: test timed out after 10m0s
	running tests:
		TestClaimGroundStampNoRepoIsANoop (0s)

real	10m2.866s   user 2m4.678s   sys 0m27.441s      (exit 1)
```

**So (2) is master-red**: the same command, on a pristine master checkout, with
the sibling branch nowhere in the tree, dies the same way. It is NOT an artifact of
the `01a0d513` diff (that diff touches `cmd/agent/cli_chat.go`,
`internal/agent/chat_ops.go`, two chat tests and two devdocs — nothing on this
path).

### What the stack actually shows (the part nobody had read before)

The panic's `running tests:` line names `TestClaimGroundStampNoRepoIsANoop`, but
that test had run 0s and the goroutine dump says where the binary really is —
**inside `initKnowledgeBase`**, committing to a device-backed SQLite ledger:

```
goroutine 17177 [syscall]:
  modernc.org/libc.Xfsync → sqlite _unixSync → _walFrames → _pagerWalFrames
  → _sqlite3PagerCommitPhaseOne → _vdbeCommit → … → database/sql.(*Tx).Commit
  github.com/the operator/gopi/internal/tools.execSchemaBatchOnce   internal/tools/memory.go:795
  github.com/the operator/gopi/internal/tools.initKnowledgeBase     internal/tools/memory.go:1067
  github.com/the operator/gopi/internal/tools.TaskNew               internal/tools/tasks.go:1484
  github.com/the operator/gopi/cmd/agent.TestClaimGroundStampNoRepoIsANoop
      cmd/agent/task_claim_ground_test.go:257
```

and it names three leaked, NON-test live things (the shape
`cmd/agent/suite_stall_watchdog_test.go` was written to expose) — the suite's own
watchdog printed its measured sentence first, at 8m0s:

```
suite stall: this test binary has been running 8m0s (watchdog budget 8m0s) …
live child processes (1) — a test is waiting on one of these:
  pid=439856 state=S cmd="…/suite-cmd-agent-run-134886198/worker-daemon-bin/gopi worker"
goroutines of this repository still running (9) — the blocked work is here:
  goroutine 299/431/438/466 [select, 7 minutes]: cmd/agent.(*g5Probe).runLive.func1
      cli_chat_web_help_surface_basic_test.go:588   ← FOUR leaked g5 probes
  goroutine 9495 [IO wait, 1 minutes]: cmd/agent.voiceServeHTTP
      cli_voice_server.go:46 → TestVoiceServeHTTPHandlerContract.func1
      cli_voice_review_test.go:1696                 ← a leaked voice HTTP server
  goroutine 10728 [sleep]: TestWorkerStartedOnAPtyWritesItsLifecycleToTheDeclaredFile
      host_lifecycle_log_test.go:328                 ← still on its wall-clock waits
```

So the 10-minute trip is the sum of two costs: (a) every `cmd/agent` fixture's
FIRST ledger open paying this host's device journal commit (`testutil: … the ledger
sandbox … is on block-backed filesystem type 0xef53 (device-backed)` — printed on
every suite entry), and (b) a handful of leaked non-test goroutines/children that
keep the binary alive after the tests that started them have already failed.

### The scale, measured under the declared budget (the same package, same host)

`scripts/test-budget.sh` (the single-source instrument, Rule 1a/2), run in THIS
worktree — i.e. master + the pending `01a0d513` repair, whose diff adds no test and
no flag: **no timeout marker**, 953s wall, one package, 33 failing tests,
`exit=1` — i.e. the SAME package needs >600s even to reach its failures. Its
pristine-master twin (job-4) is measured in the next block, at a different load,
with the same verdict.

```
$ scripts/test-budget.sh ./cmd/agent/         # budget 25m per package
--- summary (953s wall, load 6.85->3.98, cores 4) ---
packages: 1 finished, 1 with a failure
  FAILED: github.com/the operator/gopi/cmd/agent
```

Read off that run's own `-json` stream: **the tests' summed wall clock is 973.5 s**
(30 tests ≥5s, the heavy floor `scripts/lib/heavy-threshold.sh` names) — the
package cannot fit inside Go's 600 s default on this host, whatever the load.

**The pristine-master twin of the same run** (job-4, in
`/home/user/workspace/gopi-mred-baseline-01a0d530`, WITH `bin/gopi` built so the
CLI E2E guards do their real work instead of failing fast), a quieter host:

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/        # BIN BUILT 26695891
$ GOPI_TEST_TIMEOUT=25m scripts/test-budget.sh ./cmd/agent/
--- summary (745s wall, load 3.98 5.09 7.07 -> 2.67 3.07 4.85, cores 4) ---
packages: 1 finished, 1 with a failure
  FAILED: github.com/the operator/gopi/cmd/agent
stamp: 2026-09-25T05:13:35… pkg=./cmd/agent/ budget=25m elapsed=745s … exit=1 outcome=fail
        stream=run-1790284415-451451.json
# from that stream: summed test wall 762.7 s; 25 tests ≥5s; NO timeout marker.
```

Two independent master trees, two load levels, 953 s and 745 s wall for one
package under a 25m budget — both well past Go's 600 s default, neither a hang.

### The verdict, and why "heavy-tag `cmd/agent`" is not the fix

Stated plainly, because the briefing asked for one verdict:

- **master-red, pre-existing, NOT this family of changes** — reproduced on a
  pristine master worktree with no branch content at all.
- **The whole-tree default-budget run is a LOAD detector here, not a hang
  detector** — the same conclusion `scripts/test-budget.sh`'s header and Rule 27
  already record (measured elsewhere: `./cmd/agent` → `FAIL … 601.063s` with a
  goroutine dump of idle `connectionOpener` goroutines, quoted verbatim in
  `tasks/01a0ab11-recovery-report.md`; `devdoc/
  merger-status-three-states.md:383` reaches the same root cause independently).
- **Heavy-classifying it is not available**: `internal/standards/
  heavy_inventory_gate_test.go` classifies TESTS, and gate/inventory packages may
  not be classified heavy — a package is not a row in `testdata/heavy-tests.tsv`.
- **The correct dispositions**, in order: (i) the merge/verify path uses the
  branch-scoped smoke under the declared budget (Rule 27, `internal/smokegate`),
  never the whole tree; (ii) the two genuinely actionable findings in the dump get
  their own row — the FOUR leaked `g5Probe.runLive` readers and the leaked
  `voiceServeHTTP` server are resource leaks in tests that have already reported
  their verdict, and they are what keeps a finished suite's binary alive. That is
  a defect with a name and a file:line, unlike the budget trip itself.

---

## Not done (honest)

- **No fix for the leaked goroutines of (2).** Locating and repairing
  `g5Probe.runLive`'s unclosed pipes and `voiceServeHTTP`'s server would be a
  second task's worth of work with its own red proofs; this row's ask was a stated
  verdict on (2), which §2 gives with the file:line evidence.
- **No `bin/gopi` prerequisite in the 10m reproduction.** The pristine-master
  worktree had no built binary (`GOPI_BIN` unset, `bin/` absent), so the CLI E2E
  guards failed fast there (13 `TestTaskRun*`-family failures at ~0.01s) while
  `[leak]`/render tests still ran. That makes the reproduction a LOWER bound on the
  budget pressure, not an upper one — it is not a reason to doubt it.
