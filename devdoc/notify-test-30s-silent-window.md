<!-- doc-class: historical -->
# devdoc: the notify family's flat ~30s silent window — a one-shot channel read twice

Task `01a13233-c687-7275-b85f-e77ea051693b`, 2026-10-13, host hosta.
baseline `0674b07`, branch `task/01a13233c6877275b85fe77ea051693b`.

Evidence artifacts (traces, dumps, raw runs): `~/.gopi/sessions/01a13233-c687-7275-b85f-e77ea051693b/`
— deliberately **outside** the git worktree (Rule 30).

## 1. Symptom, as it was reported

Four `cmd/agent` cases each emitted their last structured event ~2s after
`=== RUN` and then sat **SILENT for a flat ~30.0s** before `--- PASS`:

| case | file |
|---|---|
| `TestNotifyDeliverWakesAnIdleChatSessionWithNoInput` | `cli_notify_deliver_idle_test.go:73` |
| `TestNotifyDeliverStatesTheMessageOnceInTheSession` | `cli_notify_duplicate_test.go:67` |
| `TestNotifyPeerCLIWakesTheFarSessionWithNoInput` | `cli_notify_peer_test.go:257` |
| `TestNotifyDeliverReachesARunningChatSession` | `cli_notify_deliver_test.go:232` |

The briefing's hypothesis — INFERENCE from reading `notifyDeliverWait = 30 *
time.Second` (`cli_notify_deliver_test.go:43`) consumed by `waitForDescribe:131`
and the record poll at `:268` — was that a **30s poll bound was being spent**.

**The hypothesis was REFUTED.** The polls are not involved. The 30s is
`notifyTestTimeout` (`cli_notify_test.go:36`), spent in the **deferred cleanup**.

## 2. The measurement (not the constant)

Instrument: `run_trace_fine.py` (this session dir) starts the prebuilt test
binary, samples `/proc/<pid>/stat` at 1Hz (state + utime+stime jiffies), lists
children from `/proc/<pid>/task/*/children`, and **SIGQUITs the pid whose own
`-test.run` argv was verified** — never `pgrep -f` (Rule 24).

```
$ python3 $SD/run_trace_fine.py "$PWD" $SD/agent.test $SD/trace-reaches-running.txt 20 \
    -test.run='^TestNotifyDeliverReachesARunningChatSession$' -test.v -test.timeout=300s
```

Observed (verbatim, `trace-reaches-running.txt`; `dump-reaches-running.txt` is
the dump split out of it):

```
   1.623 [test] === RUN   TestNotifyDeliverReachesARunningChatSession
  13.977 [test] testutil: tested binary pinned to /dev/shm/…/gopi
  14.099 [test]     cli_notify_deliver_test.go:321: session: 1de636bf-… carries side file(s) […]
  33.650 SIGQUIT: quit
  …
goroutine 4 gp=0xc000103880 m=nil [select]:
github.com/nick/gopi/cmd/agent.TestNotifyDeliverReachesARunningChatSession.func2()
	cmd/agent/cli_notify_deliver_test.go:257 +0x85
github.com/nick/gopi/cmd/agent.TestNotifyDeliverReachesARunningChatSession(0xc0001036c0)
	cmd/agent/cli_notify_deliver_test.go:336 +0xed7
testing.tRunner(0xc0001036c0, 0x37aedd0)
```

**The blocked frame is the DEFERRED cleanup, not a poll loop.** Line 257 is the
`select` inside the `defer func(){…}()` at `:255`; `:336` is the function's
closing brace — i.e. the test body was **over** and the deferred cleanup owned
the wait.

CPU is flat across the window: `cpu_jiffies=289` unchanged from t=5.0s to t=33.0s
(`trace-clean-reaches-running.txt`) — a Go-level timer wait, not I/O
(memory `#10301`'s classifier). And `children=[]` throughout: the child was
already reaped, so **no child was ever still running**: the 30s was pure
bookkeeping.

## 3. The mechanism

```go
done := make(chan error, 1)
go func() { done <- cmd.Wait() }()   // the ONLY value this channel will ever hold
…
select { case <-done: … }            // the BODY's wait drains it
defer func() {
    select {
    case <-done:                     // empty forever → this arm is never ready
    case <-time.After(notifyTestTimeout):   // ← the whole 30s, per case
        _ = cmd.Process.Kill()
    }
}()
```

One buffered value, **two readers**. The body's select consumes the child's exit
first; the deferred cleanup then waits on a channel that can never become ready
again, so its only remaining arm is `time.After(notifyTestTimeout)`. The bound is
spent to relearn a fact already known — that the child exited.

This is why the window was **flat**, not jittery: it is a timer, not a race.

## 4. The fix — the shape, not the bound

`cmd/agent/cli_notify_child_exit_test.go` (new):

```go
type notifyChildExit struct {
	cmd  *exec.Cmd
	once sync.Once
	wait chan struct{}       // CLOSED on exit — always ready, any number of readers
	mu   sync.Mutex
	err  error               // retained status
}
func armNotifyChildExit(cmd *exec.Cmd) *notifyChildExit
func (e *notifyChildExit) waitFor(bound time.Duration) bool   // idempotent
func (e *notifyChildExit) exited() bool
func (e *notifyChildExit) status() error
```

A **closed channel plus a retained status** is replayable: every await answers
immediately. All four cases now use it at both read sites (the body's
`/quit`-then-wait, and the deferred cleanup), so the two readers are independent
instead of competing for one value.

**Lowering `notifyTestTimeout` was deliberately NOT the fix.** A shorter bound
would have hidden the defect behind a shorter wait — the flake registry's own
rule: a *duration* is not a *mechanism* (`testdata/flakes.tsv` header).

## 5. Red / green, on the real surface (Rule 31)

Not a unit test — the built test binary and the real case.

**RED** — the helper mutated back to the pre-fix shape (goroutine `e.sent <- err`
into a capacity-1 channel; `waitFor` reading from it):

```
$ $SD/agent-red.test -test.run='^TestNotifyChildExitIsAwaitableMoreThanOnce$' -test.v
=== RUN   TestNotifyChildExitIsAwaitableMoreThanOnce
    cli_notify_child_exit_test.go:142: await #2 did not observe the child's exit within 2s —
        the exit event was consumed by an earlier await instead of being replayed
--- FAIL: TestNotifyChildExitIsAwaitableMoreThanOnce (2.00s)
FAIL   (exit 1)
```

The same mutation, on the **real case**, reproduces the reported symptom — the
silent window is back:

```
$ python3 $SD/run_trace_fine.py "$PWD" $SD/agent-red.test $SD/trace-red-reaches-running.txt 99999 \
    -test.run='^TestNotifyDeliverReachesARunningChatSession$' -test.v
   5.627 [test] === RUN   TestNotifyDeliverReachesARunningChatSession
   7.308 [test]     cli_notify_deliver_test.go:316: session: 5e1f3e60-… side file(s) […]
  37.317 [test] suite goroutine sweep: parked repository goroutines: 0
# exit=0 elapsed=37.357
                             ⇒ the case cost 31.7s, all of it after 7.3s
```

**GREEN** — restored (see §6), same binary rebuilt:

```
$ $SD/agent-fixed.test -test.run='^TestNotify(DeliverWakesAnIdle…|…|ChildExit…)$' -test.v
--- PASS: TestNotifyChildExitIsAwaitableMoreThanOnce (0.00s)
--- PASS: TestNotifyDeliverWakesAnIdleChatSessionWithNoInput (3.37s)
--- PASS: TestNotifyDeliverReachesARunningChatSession (0.12s)     ← was 31.7s red
--- PASS: TestNotifyDeliverStatesTheMessageOnceInTheSession (2.08s)
--- PASS: TestNotifyPeerCLIWakesTheFarSessionWithNoInput (2.06s)
PASS   exit=0
```

| same case | red (pre-fix shape) | green (fixed) |
|---|---|---|
| `...ReachesARunningChatSession` | 31.7s | **0.12s** |
| four cases + guard, wall clock | ~128s (4 × ~32s) | **7.6s** |

## 6. Restore criterion (Rule 5)

The mutation was undone with `git checkout --` and verified against an **explicit
revision**, never an index-relative diff:

```
$ git diff HEAD -- cmd/agent/cli_notify_child_exit_test.go   # EMPTY
$ test "$(git hash-object cmd/agent/cli_notify_child_exit_test.go)" \
     = "$(git rev-parse HEAD:cmd/agent/cli_notify_child_exit_test.go)" && echo OK
blob identity: OK (worktree == HEAD)
$ git status --porcelain    # EMPTY
```

## 7. Not done / honest gaps

- Only `TestNotifyDeliverReachesARunningChatSession` was traced frame-by-frame
  with a SIGQUIT. The other three were confirmed by their **own** timings before
  and after (each ~32s → ~2-3s) and by their **identical** source shape (all four
  had the same two-reader channel). A dump inside each of the four was not taken.
- The child's exit status is retained by the helper but no case asserts it; the
  cases assert delivery, and a non-zero child exit is not their subject.
- `runNotifyDeliverArgv` (`cli_notify_deliver_test.go:80`) has a *related* but
  **different** shape: one goroutine writes the value, the body's select reads it
  once, and the timeout arm kills the child — there is no second reader, so it
  does not spend the bound. It was left untouched (no defect to fix).
- Two candidates were ruled out by measurement and are recorded so nobody
  re-chases them: (a) a **descendant holding the child's stdout write end** making
  `cmd.Wait()` block on the copy goroutine — refuted, `children=[]` throughout the
  window and `cmd.Wait()` had already returned (the child was reaped); (b) the
  console's **detached daemon** (`internal/daemonlifetime`, fd 200) keeping a pipe
  open — refuted by the same scan, and by `suite child sweep: live child
  processes: 0` at close-out.

## 8. Reusable recipe

**"A flat, integral wall-clock window with flat CPU in a Go test = a timer wait,
not I/O. Dump it before reading any constant."**

1. Build once: `go test -c -o $SD/agent.test ./cmd/agent/`.
2. Trace it with a script that (a) prints each output line stamped with seconds
   since start, (b) samples `/proc/<pid>/stat` state + jiffies, (c) lists children
   from `/proc/<pid>/task/*/children` — kernel truth, never `pgrep -f`.
3. SIGQUIT the pid **whose own argv carries your `-test.run`** and split the dump
   out of the same stream: the frame that is parked names the mechanism.
4. A **flat** window (not jittery) means a timer, so look for a bound whose arm
   became the only ready one — not for a race.
5. The classic shape this case produced: **one buffered value read by two
   observers.** A lifecycle event with more than one reader must be a *closed
   channel + retained status*, never a value a reader can consume.
