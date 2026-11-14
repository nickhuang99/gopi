<!-- doc-class: historical -->
# Devdoc: the merger's verify child now leaves a durable signal-provenance record

Task: epic `signalrec-verify-attribution` (reframe 2026-09-16, verdict DOABLE).
Branch: `fix/signalrec-verify-attribution`.

## The defect

A `gopi task verify` child spawned by the merger died leaving **nothing on disk**.
`internal/signalrec` files a victim's record in the OWNING session's directory and
**never** in a public file (`no-public-fallback`, by design — the legacy sink
`~/.gopi/logs/signal-provenance.log` is deliberately not used): when no handle
resolves, it prints

```
[gopi] signal provenance NOT FILED (no public-dir fallback): …
```

and returns an error. The verify child was the one spawn in the tree where
**every** handle was absent at once:

| handle | why it was absent |
|---|---|
| `GOPI_SIGNALREC_SESSION_DIR` | nothing handed the child one |
| ledger id (`GOPI_TASK_RUN_ID`) | the gate **strips** the daemon context on purpose (an independent verification must not inherit the resolving run's handle), and the child mints its own session uuid |
| resident process-keyed layout | `task verify` is a mode that **owns its signal handlers**, so the process-keyed table cannot serve it |

The dominant way a verify child ends is the gate's **wait cap — a SIGTERM** — so
the dominant ending left no evidence at all ("the verify child vanished").

## The fix

The launcher hands the child the destination **at spawn time**
(`tools.SignalRecordEnv(rec.TaskID)` → `mergeVerifyProvenanceEnv`), in
`cmd/agent/merger_verify_child.go`:

```
child.Env = append(child.Env, mergeVerifyProvenanceEnv(rec.TaskID)...)
```

The entry carries the judged row's session container (`tools.TaskSessionDir` — the
same container every other artifact of the row lives in), the row id, and an
opt-in marker `GOPI_SIGNALREC_AGENT_CHILD` that says *this pair is a MEANT handoff,
not the over-attribution an inherited id would produce*. Fail-open with a report:
an unresolvable destination is stated on stderr and the child's resolution stays in
charge — never a silent miss.

`signalrec.handedDest` classifies a marked handoff **by the dir it names** and
requires only **identity** — the dir exists and its key IS the declared ledger row
— because a marked handoff's dir is legitimately **empty at spawn time** (the child
has not written its session log yet, and the cap kill may arrive before it ever
does). Requiring the usual marker/JSONL was the first cut of this fix and it
refused the handoff at exactly the moment the epic exists for. Measured, not
theorized: `go test -run TestMergeVerifyChildSignalRecordIsFiledWithTheHandoff`.

## The second defect this rig uncovered (a HANG, not a missing record)

The gate's wait-cap branch drained a **single-value** channel twice:

```go
done := make(chan error, 1)
go func() { done <- child.Wait() }()
select {
case werr := <-done:                        // the one consumer
case <-time.After(cap):
    gone := make(chan struct{})
    go func() { <-done; close(gone) }()
    stopChildGracefully(..., waitChildExitGone(gone))
    <-done                                  // ← BLOCKS FOREVER (the value is gone)
```

`merge_resolve_child.go` had the same shape. So on every verify/resolve child that
exceeded its cap the **merger hung**: the intent line the killer had just written
proved it had run, while the caller never read the verdict, never rewrote the row,
and the operator saw "the merger went quiet after the verify child vanished".

**How it was found**: the E2E rig below hung for its full budget with a victim that
had exited long before — measured symptom, from the rig's own diagnostic:
`victim not reaped within 15s — pid …: /proc/…/stat: no such file or directory
(already gone)`. Fix: join on the **CLOSED** channel (`<-exited`), one consumer per
value. Regression guard (source-shape, exact, red-proven):
`TestSpawnedChildWaiterIsReceivedExactlyOnce`
(`cmd/agent/merger_verify_child_stop_guard_test.go`) — reverting either seam to
`<-done` fails it, restoring passes.

## Verification (the E2E rig)

`cmd/agent/merger_verify_signal_attribution_e2e_test.go` — all of it PRODUCTION
code, none of it re-implemented: `mergeVerifyChildArgv` (the argv the gate spawns,
from which the record's role is derived), `tools.SignalRecordEnv` (the handoff),
`stopChildGracefully` + `waitChildExitGone` (the gate's OWN kill path), and
`signalrec.Install()` **inside the victim** — the test binary re-executes itself in
victim mode (`GOPI_MVS_VERIFY_VICTIM_CHILD`, dispatched from `TestMain` before any
pin), so the process that dies is the process that installed the handler.

Two arms, both real SIGTERMs delivered by the production killer:

* **control** (handoff withheld, parent identity cleared) → reproduces the defect:
  the refusal line on the victim's stderr and **no** `signal-provenance.jsonl` in
  the row's session dir.
* **fix** (handoff in place) → one parseable record in
  `<GOPI_HOME>/.gopi/sessions/<row>/signal-provenance.jsonl`:

```json
{"ts":"…","kind":"signal","signal":"SIGTERM","pid":4112002,"pgid":4112002,
 "cgroup":"0::/user.slice/user-1000.slice/session-18205.scope","role":"task verify",
 "argv":"…/agent.test task verify 01a0a72c-…","task_run":"01a0a72c-…",
 "session_dir":"…/.gopi/sessions/01a0a72c-…","layout":"task-run",…}
```

A third case, `TestMergeVerifyChildWiresTheProvenanceHandoff`, keeps the
**mechanism** and the **wiring** from drifting apart — it reads the spawn seam's
own source and fails if the append is removed (a working mechanism nobody invokes
is exactly this epic's shape).

```
go build ./...                                  BUILD_OK
go vet ./internal/signalrec/ ./cmd/agent/       clean
go test ./internal/signalrec/                   ok  5.4s
go test ./cmd/agent/ -run 'TestMergeVerifyChildSignalRecord|TestMergeVerifyChildWiresTheProvenanceHandoff'
                                                ok  1.275s  (3/3 PASS)
go test ./cmd/agent/ -run TestSpawnedChildWaiterIsReceivedExactlyOnce
                                                ok  2.5s
```

## Witnesses (documentation only — NOT to be fixed)

The verify child runs inside the **merging host's login session scope**:
`cgroup 0::/user.slice/user-1000.slice/session-18205.scope`, `sid` = the test/merger
process's session, `pgid == pid`. That is the known cgroup-escape limitation of a
non-daemon spawn (Rule 24: independence is judged by cgroup, and this one has not
left its session). It is recorded here so the next reader does not mistake it for a
provenance bug: the record above is filed correctly regardless.

## Not done (honest)

* The INTENT line of the killer itself is still refused when the killer's own
  process carries no ledger identity (the rig clears `GOPI_TASK_RUN_ID` on
  purpose). That is the pre-existing contract — the refusal is *reported*, never
  silent — and it is not this epic's subject (the victim's own record is).
* The cgroup/session witness above is documented, not repaired (Rule 24).
