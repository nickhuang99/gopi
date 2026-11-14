<!-- doc-class: historical -->
# The named worker face had no stop chain (`gopi worker <task-id>` ignored SIGINT)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

<!-- doc-class: historical -->

**Measured 2026-10-27, fixed in the same cycle.** `gopi worker <task-id>`
(`runTaskWorkerExplicit`, `cmd/agent/cli_tasks_worker.go`) installed the graceful-stop
HANDLER but never started the goroutine that reads that handler's wake-up. The loop
face (`runTaskWorkerLoop`) starts it, which is why only the *named* face was affected.

## The chain, and the missing link

```
installWorkerStopSignals()          ← BOTH faces had this
  signal → workerRequestStopWithSignal(...)   (records the request, writes the signal record)
  signal → notifyWorkerStopNudge()             (a one-slot non-blocking send)
              ↓
startWorkerStopForwarder()          ← THE LOOP FACE ONLY
  goroutine: <-workerStopNudge → forwardWorkerStopDuringRound()
              → forwardWorkerStopToChild(false) → SIGTERM to the in-flight run child
```

The handler's own comment says the decision belongs to the forwarder goroutine
("the forwarding decision belongs to the waiter … which can tell a DRAIN from a bare
signal"), so on the named face **nothing read `workerStopNudge` at all**: the nudge
sat in the one-slot buffer forever. A run child does not return by itself — the round
is blocked in `cmd.Wait()` on it — so the stop never landed.

## What was observed (real binaries, real ledger, `sleep`-free hang)

Both arms: a real `gopi task submit` row in a throwaway repo, a real
`gopi worker <uuid>` in a sandbox profile whose model endpoint is a local server that
**accepts and never answers** (so the round is long-running for a reason, not by luck).

| arm | binary | `kill -INT <worker>` | result |
|---|---|---|---|
| RED | pre-fix (`git archive HEAD` build) | at t+25s | worker **still `Sl` 16s later**, run child still alive, **no stop line in its log at all** |
| GREEN | fixed build | at t+25s | child **gone at t+3s**, worker **gone at t+4s**, log carries `gopi] terminated received — finalizing session…`, row written back `failed` (the interrupt is recorded, not swallowed), ground collected |

The red arm's silence is the defect's signature: a stop that is only *recorded* looks
identical to a stop that never arrived, because the only reader of the wake-up was
never started.

## The fix (three lines of contract, one of them a judgement call)

1. `stopForwarder := startWorkerStopForwarder()` + `defer stopForwarder()` in
   `runTaskWorkerExplicit`, beside `installWorkerStopSignals` — the two halves of the
   stop chain now sit together on every runner face (Rule 14: one runner). The DRAIN
   guarantee is untouched: `forwardWorkerStopDuringRound` forwards NOTHING when the
   durable request file stands, so `--stop`/`--restart` still let the in-flight round
   finish (pinned by `TestWorkerDrainStillLetsTheRoundFinish`).
2. After `workerRoundNamed` returns, a pending **bare** stop runs the loop's boundary
   wait (`awaitWorkerStopChild(false)`: forward, wait up to `workerStopGraceDefault`,
   then terminate what is left) so the process's exit is the end of the work it
   started — a tool's grandchild or a `bash` job cannot outlive the round. A durable
   drain is skipped there on purpose: that request means "let it finish", and this
   face's round already reached its own close-out.
3. `cmd/agent/worker_explicit_stop_chain_test.go` pins the FACE, not the chain: the
   chain itself is already proven by `worker_stop_forward_regression_test.go`, so what
   needs a regression guard is that the named face is *on* it (call membership +
   deferred teardown + parity with the loop face). Red on the pre-fix body, green
   after — both directions measured in the same cycle.

## Why a structural arm was the honest choice

`runTaskWorkerExplicit` ends in `os.Exit`, so no in-process test can observe its
epilogue, and the behavioural half (a bare signal reaches an in-flight child) is
already covered. The failure that shipped was not a broken forwarder — it was a face
that never started one. A test that asserts the call is present is the smallest thing
that fails when this specific defect returns.
