<!-- doc-class: historical -->
# The residual red in TestChatInject_EscapeByNameReachesTheTurnSeam: an ORDERING race at the fixture's injection seam

**Status:** attribution record + pre-existing-red evidence. **No production change in
this branch** — the mechanism below is off the product's own decision points, and the
measurement says it reds without the fix too. The fix direction is named at the end and
dispatched as its own task.

Commit of record: `4d42acb1` (the park-window EDGE fix, `internal/lineedit/editor.go`,
which this record measures). Parent measured for the pre-existing comparison:
`e07b32a7`.

`4d42acb1` proved the park-window EDGE loss *sufficient* for the red and shipped a
deterministic guard for it (`internal/lineedit/injectpark_test.go`). It did **not** claim
the mechanism was the *only* one: after the commit, one `-count=3` run printed
`FAIL github.com/nick/gopi/cmd/agent 11.394s` with the failure message **never
captured** — so "the fix is incomplete" and "the red is the fixture's noise" were both
open. This record settles which, with the message captured.

## 1. The residual red, captured verbatim

Invocation, verbatim:

```bash
go test ./cmd/agent/ -run 'TestChatInject_EscapeByNameReachesTheTurnSeam' -count=5 -v -timeout 300s
```

Verdict, both trees, same message, same shape:

```
    chat_inject_pty_test.go:338: the injected key NAME "escape" never reached the turn seam within 3s — the tool's own spelling is broken while the raw byte works
--- FAIL: TestChatInject_EscapeByNameReachesTheTurnSeam (6.35s)
```

Every red iteration takes **6.3–6.6 s**; every green one takes **3.27–3.29 s** (the 3 s
claim plus a step). The failure is always the *last* claim of the case — the
`escape`/interrupt half — never the earlier `d` half.

Census, `-count=5` per round, load from `uptime` beside each round:

| tree | rounds | iterations | red (`never reached`) | per-iteration rate | load observed |
|---|---|---|---|---|---|
| **WITH `4d42acb1`** | 6 | 26 | 6 | 23 % | 9.5 – 15.7 (4 cores) |
| **PRE-`4d42acb1` (`e07b32a7`)** | 8 | 41 | 10 | 24 % | 7.2 – 10.0 (4 cores) |

**The red is pre-existing, not introduced and not removed by the park-window fix.** Its
rate is indistinguishable between the two trees at these loads. The same case at low
load is green: `-count=1` × 92 single-process iterations under 9.45 produced **zero**
`never reached` (the only non-zero exits there were the suite's own
`parked repository goroutines` sweep, §4), and `-count=20` at load 2.1–4.0 was 20/20.

The pre-fix binary was built from a `git archive e07b32a7` extracted OUTSIDE the
worktree (Rule 29 — no foreign checkout was touched), and
`cmd/agent/chat_inject_pty_test.go` is byte-identical between the two revs
(`git diff 4d42acb1 -- cmd/agent/chat_inject_pty_test.go` is empty), so this is a clean
A/B.

## 2. The mechanism, from the trace

Temporary instrumentation (a `ztrace` helper + call sites in `editor.go`, `inject.go`,
`raw.go`; **all reverted before the commit** — the worktree is clean and this branch
carries no probe). One red iteration, microseconds since process start:

```
ZT 18021446 01164 q0xc00086fa00.put "\x1b" -> len=1          <- the test's injection lands
ZT 18021470 01165 inj0xc0008574a0.wake: notify
ZT 18021504 01166 term0xc0002f4f00.poll ret n=1 stdinRev=0x0 <- the loop's poll takes the wake
ZT 18021512 01167 term0xc0002f4f00.wake DRAIN
ZT 18021521 01168 inj.ReadEvent: inner -> kind=3 bytes=0 q=0  <- EventWake, nothing read
ZT 18021538 01170 inj0xc000c2e900.InputPending: q=false      <- the DECIDER asks
ZT 18021551 01172 ed0xc0009c4708.loop-wait tout=-1 …         <- and commits to poll(-1)
ZT 18021556 01173 inj0xc000c2e900.ReadEvent(-1) ENTER q=0
ZT 18021561 01174 q0xc0009ad880.take EMPTY
ZT 18021566 01175 term0xc0002f4f00.poll enter timeoutMS=-1   <- parked; nothing will wake it
ZT 18366599 01176 inj0xc0008574a0.injectUnit "escape" accepted q=1 readByEditor=false
```

**The ordering is the finding, and it is read off the trace, not inferred:** the wait is
DECIDED (`loop-wait tout=-1`, the decider reported `pending=false`) and the read is
ENTERED against an empty queue (`q…take EMPTY`), and only *then* — 3.34 s at 01176 — does
the injector report the key **still queued** (`q=1`) and `readByEditor=false`. The key was
never lost; it arrived after the loop had already committed itself to an unbounded
`poll(-1)` with nothing left to interrupt it.

The park-window fix is **sufficient but its window is a subset of this race**. The fix
bounds the wait only when the level is already `true` at the park point, and the
decisive run shows the bounded wait being *consumed by the other key*: the loop drains
the `d` with `q=TRUE` (the fix's `tout=5`), then re-evaluates at `pending=false` and
parks unbounded — the `escape` landing a moment later is the same lost-edge shape, just
outside the branch the fix covers.

The injection point proves it is not the product's own seam:
`newChatInputWithSource` builds the injector **and** `NewChatEditor(injector)` in the
same test goroutine, and the injector call in the test (`q0xc00086fa00.put`, `q=1`) is
**not the object the read loop asks** (`q0xc0009ad880.take EMPTY`, the loop's own
injector). That is why the case's own diagnostic prints a queued key and
`readByEditor=false` in the same breath: it is reporting on the object it injected into,
while the loop parks against the object it reads from. The end-to-end claim ("the KEY
NAME reaches the turn seam") is therefore decided by a race between the injection and
the read loop's *next decision*, and the read loop's decision can be the one already
taken.

**Verdict: (2) of the epic's two shapes, with the second mechanism identified —** the
residual is a load-correlated ordering race at the pty fixture's injection seam,
**pre-existing** (red on `e07b32a7` at the same rate), NOT a product defect that
`4d42acb1` failed to close.

## 3. What a green guard would require, and why this branch does not ship one

The epic asked for either shape (1) — a second mechanism with its own **red-into-green**
guard — or shape (2), the measurement above. The mechanism is proven (shape 1's first
half) but its **green half does not exist for this case**, and that is a fact about the
race, not an omission:

- The race is between two goroutines with no shared decision point: the injection lands
  at `[decision, read]`, and nothing on the product side can un-decide it without making
  every wait bounded (a latency/CPU cost on the normal path — the same trade the
  park-window fix deliberately restricted to the `inputPending()` case).
- A guard written in this package can construct the window (that is exactly what
  `injectpark_test.go` does) but a guard of *this* race in the pty fixture is a guard on
  a test's own nondeterminism: it reddens on the unfixed tree and, by construction, on
  any tree — which is a reproduction harness, not a regression guard. Committing a test
  that fails ~23 % of the time would convert a load-correlated flake into a permanent
  red on the merge path, which is worse than the flake.
- The honest exit is therefore the measurement (§1, §2) plus a **fix direction** the
  next task can execute: make the case assert the hand-off it can actually own, or make
  the fixture's second injection wait for the read loop to be observably idle (the
  fixture already has the seam it needs — `SetMinTurnActive` / the geometry observer —
  and a signal the case owns rather than a 3 s wall). Whichever arm is taken, the
  acceptance is "N consecutive `-count=5` rounds under load with no `never reached`",
  not "the suite is green once".

## 4. Two other reds seen on the way, and why they are not this one

`combined -count=5` and `-count=1` runs exit non-zero sometimes with **no failure
message at all** — the suite's own close-out sweep, not an assertion:

```
suite goroutine sweep: parked repository goroutines: 1
  github.com/nick/gopi/internal/lineedit.(*Terminal).ReadEvent(...)  raw.go:480
  …(*Injector).ReadEvent(...)                                        inject.go:665
  …(*Editor).Run(...)                                                editor.go:1362
  …cmd/agent.newInjectPtyGrid.func1()               chat_inject_pty_test.go:94
```

That is the case's own read loop still in `poll(-1)` at the moment the sweep runs — the
same unbounded park, seen at teardown instead of through the 3 s claim. It is a
**second face of the same resident goroutine**, not a second mechanism: the sweep counts
it while the fixture's `t.Cleanup` is still within `injectPtyWait`. Counts observed
0/1/2/5, uncorrelated with the claim's red, and present on the pre-fix tree as well.

## 5. Reproducing this record

```bash
# the red + its verbatim verdict (expect ~1 round in 3 to red under load ≥ 7 on 4 cores)
go test ./cmd/agent/ -run 'TestChatInject_EscapeByNameReachesTheTurnSeam' -count=5 -v -timeout 300s

# the pre-existing comparison, WITHOUT touching a foreign worktree (Rule 29)
SC=/tmp/gopi-prefix-$(date +%s); mkdir -p $SC
git archive e07b32a7 | tar -x -C $SC && cd $SC
go test -c -o /tmp/prefix.test ./cmd/agent/
/tmp/prefix.test -test.run 'TestChatInject_EscapeByNameReachesTheTurnSeam' -test.count=5 -test.v
```

Evidence for every figure above (raw logs, the A/B binaries, the trace dumps) lives in
this run's session directory, outside the worktree (Rule 30):
`/home/user/.gopi/sessions/01a0f8aa-631d-78e9-b743-8dd8e7e42e92/evidence/` —
`m5-*.log` (post-fix rounds), `prefix-*.log` + `prefix-census.log` (pre-fix),
`id-3.log` / `qtrace-*.log` (the traced reds), `single-*.log` (the `-count=1` negative).
