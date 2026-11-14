<!-- doc-class: normative -->

# The bash tool's unbounded rung: the pipe wait AFTER the kill

Task `01a1911e`. A send-verify polling loop was reported to have "pinned a bash
tool call for 101 minutes with no timeout mechanism". This page records what was
MEASURED, what was not, and the one rung that really had no bound.

## 1. The epic's two premises, checked against the ledger

**"101 minutes" does not reproduce.** Every session under `~/.gopi/sessions`
(3454 directories, 191351 completed bash calls) was measured end-to-end from the
`tool_use` timestamp to the `tool_result` timestamp
(`evidence/scan_polling.py`, output in `evidence/scan_polling.out`):

```
total bash calls with a result: 191351
wall=  NONE 2026-10-10T08:59:01  01a1234b   (the ONLY bash tool_use with no result)
wall=  632s 2026-10-12T03:43:50  01a12c5b   (ledger-wide MAXIMUM = 10.5 min)

wall=580s (9.7 min) timeout_sec=0   while [ ! -f "$EVID/run2.rc" ]; do sleep 20; echo "wait run2 t=$(date -Is) polls=$(wc -l < $EVID/run2.poll.txt) log=$(stat -c %s $EVID/run2.test.log)"; done; echo "DONE run2"; cat $EVID/run2.rc; tail -2 $EVID/run2.result.txt
wall=300s (5.0 min) timeout_sec=0   while [ ! -f "$EVID/run3.rc" ]; do sleep 25; echo "wait run3 t=$(date -Is) polls=$(wc -l < $EVID/run3.poll.txt) log=$(stat -c %s $EVID/run3.test.log)"; done; echo "DONE run3 rc=$(cat $EVID/run3.rc)"; cat $EVID/run3.result.txt
```

The longest bash call in the entire ledger is 632 s. The longest POLLING loop is
580 s, and both polling loops **terminated because the marker appeared**: run2
after 46 polls, run3 after 33 polls.

**"The loop body never ran (poll.txt had 1 line)" is false.** `poll.txt` is
written BY the poller, and it grew:

```
$ wc -l evidence/ep4/run3.poll.txt        -> 33
$ tail -1 evidence/ep4/run3.poll.txt      -> 2026-10-31T17:08:54+08:00 head=ad264e2362e22c9bc127d120e2a34df92b6fcc46 porcelain=0 prod_diff=0 threads=1423 gopi_procs=5 loadavg=2.05 3.24 2.61 2/2078 4006160
```

So the observed incident was: a caller passed `timeout_sec=0` (unlimited
absolute time, a deliberate choice), the poller echoed every 20–25 s which
refreshed the idle net on every byte read from either pipe, and the loop ended
when the `go test` it waited on finally wrote its marker. That is a long wait by
configuration — not a hang, and not a missing mechanism.

## 2. Candidate (A): the nets DO exist — and the poller resets one of them

`internal/tools/timeout.go` declares `DefaultBashTimeout = 120s` and
`DefaultBashIdleTimeout = 120s`; `bashBudget` (`internal/tools/bash.go`) maps the
caller's `timeout_sec`:

| `timeout_sec` | absolute | idle net |
|---|---|---|
| `< 0` (invalid) | `BashTimeout()` | `BashIdleTimeout()` |
| `0` | **none** | `BashIdleTimeout()` |
| `> 0` | `timeout_sec` | `min(BashIdleTimeout(), timeout_sec)` |

`timeout_sec=0` therefore does NOT mean "no nets" — the 120 s idle net still
applies. But it is refreshed by every byte the read goroutines consume, so a loop
that echoes every 25 s can never trip it. **That half of the epic is correct**,
and it explains a long-but-bounded wait; it does not explain a hang.

## 3. The rung that WAS unbounded — and the minimal proof

Step 9 of `bashInDirCtx` waited for both pipe readers with no deadline:

```go
<-readDone
<-readDone
```

EOF on the read ends arrives only when **every holder of the write end** has
closed it. A descendant that LEAVES the process tree — `setsid CMD &` reparents it
to init, so `bashDescendantPIDs(pid)` cannot enumerate it and `killChildTree()`
has nothing to signal — inherits the write end and holds it open forever. By then
the absolute budget and the idle net have BOTH already fired (a kill is what
killed the child), so nothing is left to fire and the call sits there with **no
deadline at all**. That is the real "no timeout mechanism", and it is one rung
below the nets.

The minimal sample is the regression test's own command:

```
setsid bash -c 'echo $$ > <pidfile>; exec sleep 300' & echo launched
```

with `abs = 2s`, `idle = 2s`. Measured against unmodified code, and again against
the fix with step 9 mutated back to the unbounded form:

```
--- FAIL: TestBashKillReturnsWhenADescendantHoldsThePipe (20.02s)
    bash_pipe_retention_test.go:67: bashInDirCtx never returned: the tool stayed blocked
    past its absolute budget (2s) because a descendant left the process tree and kept
    the output pipe open (watchdog 20s)
```

With the fix in place:

```
--- PASS: TestBashKillReturnsWhenADescendantHoldsThePipe (4.00s)
ok  	github.com/nick/gopi/internal/tools	5.494s
```

## 4. The fix — one bound, at the rung that failed

Step 9 now bounds the pipe wait, and **only after a kill has been delivered**
(`cmdCtx.Done()`, which carries both the expired budget and the operator's
cancel). While the command is alive and quiet the wait stays unbounded, which
keeps the old semantics for a slow-but-live reader — the bound cannot cut off a
command that is still producing.

On expiry the tool closes the pipe **read** ends it opened. It signals nothing:
the surviving descendant is a process this caller no longer holds, so taking it
down is the operator's call (Rule 42). Because the stream now ends early, the
release is STATED in the result (Rule 15) rather than left to be inferred from a
missing tail.

```go
const bashPipeGraceAfterKill = 2 * time.Second
const bashPipeRetainedNoticeFragment = "output pipe still held"
const bashPipeRetainedNoticeFmt = "\n[%s after the command was killed: a descendant left the process tree and kept the output pipe open; the tool released the pipe after %s]"
```

Live invocation against a binary built from this tree (`make build`), the same
`setsid` sample with a 2 s budget:

```
$ go run ./.tmpexercise
elapsed=4.002s
--- err=command timed out after 2s
--- output:
exit=-1
launched

[command timed out after 2s]
[output pipe still held after the command was killed: a descendant left the process tree and kept the output pipe open; the tool released the pipe after 2s]
```

`elapsed = 4.0 s` = the 2 s budget plus the 2 s grace. Before the change the same
call never returned.

## 5. What this does NOT claim

- It does not make `timeout_sec=0` bounded. That stays the caller's deliberate
  choice; the idle net is the only safety net there, and a caller that emits
  output on a schedule resets it. A caller that wants a ceiling passes one.
- It does not kill the escaped descendant. Freeing the tool and taking down
  somebody else's process are different decisions, and only the first is ours.
- It adds no watchdog, daemon or script (Rule 35): the bound lives inside the
  call that was already running.
