<!-- doc-class: historical -->
# The notify fixture family at base `0b743cbac`: one red, one line, one mechanism

**Task**: `01a12cbb-350b-7922-a120-ec3eee2e2c65` · **Base rev**: `0b743cbac`
(detached worktree `/home/user/workspace/gopi-base-01a12cbb`, `dirty=0`) · **Host**:
hostb-Laptop · **Run worktree**: `.../gopi-01a12cbb350b7922a120ec3eee2e2c65`
(`task/01a12cbb350b7922a120ec3eee2e2c65`)

This is the record of one measurement run. Measured and inferred are labelled
separately (Rule 41).

## What was asked, and what the measurement says

The addendum filed against row `01a12ca8` argues from two object-store greps that the
family is not uniform at `0b743cbac`, and concludes that the row's "4/4 RED" on
`TestNotifyDeliverStatesTheMessageOnceInTheSession` therefore *cannot* be the re-filter
shape. This run reproduced the rev and ran each fixture individually.

**The premise's first half is right and its second half is wrong.** Measured
(`base-0b743cbac-fixture-shape.out`, from the pristine base tree):

```
cmd/agent/cli_notify_deliver_test.go:328:     for _, c := range sess.HarnessUserContents() {   ← fixed (9867ed461)
cmd/agent/cli_notify_deliver_idle_test.go:199: for _, c := range sess.HarnessUserContents() {  ← fixed (9867ed461)
cmd/agent/cli_notify_duplicate_test.go:159:    for _, c := range sess.UserContents() {         ← NOT fixed (4cec86abb missing)
```

and the same dump names the owner of each read line:

```
cli_notify_deliver_test.go:232:       func TestNotifyDeliverReachesARunningChatSession
cli_notify_deliver_idle_test.go:73:   func TestNotifyDeliverWakesAnIdleChatSessionWithNoInput
cli_notify_duplicate_test.go:67:      func TestNotifyDeliverStatesTheMessageOnceInTheSession   ← the ANCHOR
```

**The anchor test is defined in the DUPLICATE file, at every rev measured**
(`git grep -l` at `0b743cbac`, `a3a31da42`, `master` — all answer
`cmd/agent/cli_notify_duplicate_test.go`; `git log --all -S 'func
TestNotifyDeliverStatesTheMessageOnceInTheSession' -- cmd/agent/cli_notify_deliver_test.go`
returns **0** commits, i.e. the name was never in the DELIVER file). So
`cli_notify_deliver_test.go:328` — which the addendum reads as "the anchor's read line,
already fixed" — belongs to a **different test**. The family is not uniform, but for the
**opposite** reason: the two DELIVER fixtures are repaired, and the anchor is red
*because its own repair is missing*. The anchor's red **is** the re-filter shape.

## The two measurements that decide it (not the greps)

**PROBE A — the fixture's own read path, instrumented in place** (all three views plus
the raw record, one run, ~33 s):

```
PROBE read-path: AllUserContents rows=2 carrying=1 | UserContents rows=0 carrying=0 | HarnessUserContents rows=2 carrying=1
PROBE row[0] carrying=true  "[system] An external process sent this session a message: NOTIFY-DUP-9f31: one delivery, one message"
PROBE row[1] carrying=false "[system] [t+2s] — first request of this session."
```

The record **holds** the delivered row; the view the fixture ranges returns zero of
them; the union view and the harness view both carry it. This is the read-path verdict
in one print-out: the delivered sentence is minted by the harness
(`chat_job_notice_turn.go` `sessionMessageFact` prefixes `sessionevent.HarnessMessagePrefix`)
and `testutil.Session.UserContents()` has returned the operator-only view since
`ea1357381` — the exact complement of what the assertion needs.

**PROBE B — the one-line flip.** With only `:159` changed
(`UserContents()` → `HarnessUserContents()`, `git diff` shows that single line):

```
--- PASS: TestNotifyDeliverStatesTheMessageOnceInTheSession (33.57s)
ok  	github.com/nick/gopi/cmd/agent	39.102s
```

Red ↔ green is arrested by that one line, so the red is the **read-path shape** and
nothing else. `4cec86abb` is exactly that one line, and it is **not** an ancestor of
`0b743cbac` — hence red there. **Not** a product defect (the writer put the row in the
record, measured above), **not** a host/timing artifact (4/4 red, identical line and
identical count text; and a timing artifact does not turn green on a view change).

The other two fixtures **PASS** at this rev — `TestNotifyDeliverReachesARunningChatSession`
(`ok … 33.375s`) and `TestNotifyDeliverWakesAnIdleChatSessionWithNoInput`
(`ok … 36.943s`). Neither is red at `0b743cbac`, which is also why "the DELIVER fixture
is green" cannot be used as evidence about the anchor.

## The repository's own classifiers, run at this rev

**Correction to this document's first version** (the gates are TWO files, not one; the
first version transplanted the first and read its `0 sites` as a boundary of the class.
Measured afterwards, that inference was wrong — see the second gate below.)

**Gate 1 — `internal/testutil/session_view_filter_gate_test.go`** (landed at
`b9295e780`, **not** an ancestor of the base), transplanted verbatim from master:

```
session_view_filter_gate_test.go:200: scanned 1650 test files, 24 view call sites, 0 re-filter site(s)
--- PASS: TestSessionViewIsNeverFilteredASecondTime
--- PASS: TestSessionViewGateHasTeeth (6/6 arms)
```

Its class is arm 1/arm 2 — "range a filtered view **and re-test the element with the
harness predicate**". The base rev contains no such site, which is a true statement about
that class and nothing more.

**Gate 2 — `internal/standards/session_view_gate_test.go`** (landed at `04f066306`,
also **not** an ancestor of the base; sha256 `24be6bfc…`, transplanted verbatim):

```
=== RUN   TestSessionRecordViewsAreFilteredOnce
    session_view_gate_test.go:491: 1 session-record read path(s) re-partition a view the fixture already partitioned (Rule 13/14):
        cmd/agent/cli_notify_duplicate_test.go:159: for _, c := range sess.UserContents() {
            the loop ranges over UserContents (the view that EXCLUDES harness rows) while searching for the text this file delivers with `notifyMessageFlag`: the product records a delivered sentence on a HARNESS row (internal/agent/chat_job_notice_turn.go sessionMessageFact), so the search is false on a correct record. …
    session_view_gate_test.go:494: session-record read path: 1650 test file(s) scanned, 3 view(s) classified from internal/testutil/session.go, 1 violation(s)
--- FAIL: TestSessionRecordViewsAreFilteredOnce (2.26s)
FAIL	github.com/nick/gopi/internal/standards	6.437s      (exit 1)
```

**This gate classifies exactly the red shape** (its class 2: range a view that excludes
harness rows while `strings.Contains`-searching for the text the file delivers), and at
this rev it names **one** site — the anchor fixture's own line. The independent
red-then-green proof, with only `:159` flipped and nothing else touched in the file
(sha256 `72eaa34a…` → `5535694c…`, restored afterwards):

```
--- PASS: TestSessionRecordViewsAreFilteredOnce (1.73s)
ok  	github.com/nick/gopi/internal/standards	6.775s      (exit 0)
```

So the class that decides this defect **is** machine-classified, by a gate that landed
after the base rev — the anchor's red is not a gap in the gate's coverage, it is an
instance the gate catches (and would have caught, had it been an ancestor). No new row
filed: the shape has a check, at master, proven red at the base rev and green on the
one-line fix (`base-0b743cbac-class2-gate.out`).

## The fake-green control (Rule 31's own trap, measured here)

`go test ./cmd/agent/ -run 'TestThisNameDoesNotExistZZZ' -count=1 -v` prints
`testing: warning: no tests to run` and exits **0**. Every archive above therefore
carries `--- PASS`/`--- FAIL` **and** the package's `ok`/`FAIL` line, and the exit code
alone was never treated as the result.

## Measured vs inferred

- **Measured**: the read line per fixture and its owning test at `0b743cbac`; the
  ancestry verdict of each fix commit against that rev; the FAIL/PASS of each fixture
  with `-count=1` and `-count=3`; the three-view counts and the raw record contents; the
  one-line flip's PASS; **both** transplanted gates' verdicts at this rev (gate 1: 0
  sites; gate 2: RED, naming `:159`, and GREEN on the single-line flip); the control at
  `a3a31da42` (PASS, `ok … 54.991s`); every file restored to its base sha256 with
  `git status --porcelain` empty.
- **Inferred (labelled)**: that `a3a31da42`'s green is *explained* by its `UserContents`
  still being the unfiltered raw census (its body was read, no predicate call — read, not
  executed). The inference is not load-bearing: the mechanism is decided by PROBE A/B.
- **Not measured**: whether any *further* rev in the window carries a second shape; this
  run measured `0b743cbac` (its subject) and used `a3a31da42` and `master` only as
  ancestry/control anchors.

## Files

- `devdoc/notify-fixture-family-base-shape-20261012.md` — this document.
- Raw archives and the two instruments: session artifacts of
  `01a12cbb-350b-7922-a120-ec3eee2e2c65` (listed in the task report's `## Evidence`).
