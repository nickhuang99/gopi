<!-- doc-class: historical -->
# The ground-kill walk captured a process identity inside the argv publication window

**Status:** historical record of one diagnosis (2026-10-17), plus the standing
rule it produced. Task `01a14b65-42a7-7ece-a4a1-a31b51ccd071`.

## Symptom

`TestGroundKillSignalsOnlyTheRecordedIncarnation` (`cmd/agent/ground_kill_identity_test.go`)
went red on roughly a third of its runs, always at the SAME line — arm ①'s
"the refusal is not a refusal" assertion — with a sentence that contradicts
itself:

```
ground_kill_identity_test.go:82: the guarded signal reached the live pid anyway
(pid 2833450, pid 2833450 was REUSED: now pid=2833450 starttime=7876343
cmdline="sleep 300", was pid=2833450 starttime=7876343 cmdline="") —
the refusal is not a refusal
```

Both halves carry the **same pid and the same starttime**; only `cmdline`
differs, and the *stored* one is empty. `starttime` is fixed at process
creation and an `exec` preserves it, so same-starttime means ONE incarnation —
a "REUSED" verdict here is not a pid reuse.

## Root cause — a CAPTURE treated as a RE-CHECK

`/proc/<pid>/cmdline` of a just-spawned child is EMPTY for a short window after
`exec.Cmd.Start()` returns: the argv block is published after the kernel's task
entry exists (measured 194/200 spawns, `ArgvUnpublished` in
`internal/testutil/procident.go`; counter evidence 1/120 spawns when the reader
reads a few other `/proc` entries first — which is exactly why it is flaky and
not deterministic).

The test did:

1. `StartMockProcess` → the child exists, its argv may not be published yet;
2. `testutil.ReadProcIdentity(pid)` → a **capture**, which can land INSIDE the
   window and store `Cmdline: ""`;
3. `id.Alive()` → a **re-check**, which reads the now-published `"sleep 300"`
   and compares it to the stored `""` → reports REUSED → the assertion fails.

The guard under test (`KillIfIdentical`, which compares the **incarnation**
pid+starttime only) was never broken: arm ①'s refusal was correct, and the
failure message was produced by the fixture's own bookkeeping.

## The fix — the capture site, never the shared reader

`internal/testutil` already ships the two entry points, and the distinction is
the whole point:

- `ReadProcIdentity` — observe NOW. A re-check (`Alive`, `KillIfIdentical`)
  must never wait; making it wait turns a liveness verdict into a guess with a
  delay.
- `AwaitProcIdentity` — the CAPTURE entry point for "this child I just
  spawned": polls while `ArgvUnpublished()` and REFUSES (naming pid + budget)
  rather than returning a half-filled identity.

The change is two call sites in `cmd/agent/ground_kill_identity_test.go` — the
walk's fixture capture and `TestGroundKillGoesThroughTheIdentityPath`'s live
fixture capture — both switched to `AwaitProcIdentity`. No assertion was
relaxed, nothing was skipped, and `ReadProcIdentity`/`Alive`/`KillIfIdentical`
are untouched.

## Measured

`go test ./cmd/agent/ -run 'TestGroundKill' -count=1`, repeated. The pre-fix
figure comes from a test BINARY compiled from the pre-fix source
(`go test -c ./cmd/agent/`) and run in a loop, so a rebuild mid-measurement
cannot contaminate it — a mistake made once in this run and discarded. The
post-fix figure is the same loop on the fixed tree plus the ≥20 consecutive
runs required by the acceptance criterion. Both logs live in this run's session
dir (`~/.gopi/sessions/01a14b65-…/artifacts/flake-before.txt` /
`flake-after.txt`).

## What a future reader should take away

- **"pid N was REUSED" with equal starttimes is a CONTRADICTION, not a reuse.**
  Read `cmdline=""` in the failure text as the argv publication window before
  believing any diagnosis that blames the mechanism under test.
- **Capture waits; re-check does not.** If you store an identity and later ask
  `Alive()` about it, the capture must go through `AwaitProcIdentity`.
- The same race was previously mis-diagnosed as "the watchdog fires too early"
  in `daemonlifetime_test.go` (see `internal/testutil/procargvpublish_test.go`).
