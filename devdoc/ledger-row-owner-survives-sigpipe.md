<!-- doc-class: normative -->
# A ledger-row owner must survive its launcher — SIGPIPE, and the row it left open

**Bug row:** `01a1950b-8be5-7d73-89f4-c5de45e8ea6d`
**Fixed at:** `internal/agent/brokenpipe_guard.go` (+ one call in
`internal/agent/taskregister.go`), measured 2026-11-01 on this host.

## The symptom, as reported

One `[auto]` row sat at `in_progress` for 1h51m with `ended_at` empty:

```
$ gopi task show 01a194e1-5e79-7ad5-a8b3-bde0a9236b0e
status:         in_progress
summary:        [auto] chats
run_method:     auto
run_cmd:        gopi chats
claimed_by:     hosta:3430882 (at 2026-11-01 10:12:40)
process:        pid=3430882 … (live: gone)
ended_at:       (not ended)
```

Its session directory held four artifacts and none of them carried a
`session_end`; a backfill dump 19 minutes later read
`{"category": "orphan-no-session-end", "exit_marker": "absent"}`. A `stale`
event had been emitted. Nothing moved the row — and by design, not by omission:
automatic disposal was removed by the operator's 2026-10-26 ruling
(`devdoc/zombie-sweep-removed.md`, "gopi reports a stuck row, it never rewrites
one"). So the producer is the only place this class can be repaired.

## Why it died — the measured mechanism, and the two probes that lied

The session log's last event is a `request` row emitted at the SAME instant as
the death, and the parent shell's line for that instant is
`/usr/local/bin/gopi chats 2>&1 | head -12`. A write to fd 1/2 whose reader has
closed raises SIGPIPE, and SIGPIPE's default disposition KILLS the process: no
`session_end`, no end hook, an `in_progress` row nobody will ever close.

Two probes were built before the right one, and BOTH passed on the pre-fix tree
— which is exactly why they are recorded here:

| probe | reading on the PRE-FIX tree | verdict |
|---|---|---|
| `syscall.Kill(os.Getpid(), SIGPIPE)` | survived | **measures nothing** |
| write to a self-made pipe whose read end is closed | `EPIPE`, survived | **measures nothing** |

Go's runtime honours SIGPIPE **only for fd 1 and fd 2**; a write to any other
pipe just returns `EPIPE`. When the child's **stdout IS** the pipe with no
reader, the default disposition kills it:

```
$ ./probe          # fd 1 is a pipe whose reader closed
mode=default err=signal: broken pipe        ← KILLED (exit 141)
mode=ignored err=<nil>                      ← signal.Ignore(syscall.SIGPIPE)
```

Corollary worth keeping: `go run .` SWALLOWS a child's exit status — a probe
that needs an exit code must `go build -o bin . && ./bin …`.

## Why the existing guard did not cover it

`cmd/agent/main.go` already ignores SIGPIPE, but only when
`GOPI_TASK_WRAPPER_ID` is set — a `task run` CHILD. Its own comment drew the
boundary: "Top-level `gopi | head` semantics are untouched — a plain run keeps
the default SIGPIPE disposition." That was true when a top-level run owned
nothing. Since run-identity-unify it MINTS ITS OWN ROW
(`maybeRegisterSessionTask` → `TaskNewOID` + the self-claim stamp), so it carries
exactly the close-out obligation the wrapper child's guard was written to
protect. **The guard was keyed on the process's LINEAGE when the fact that
matters is its DEBT.**

## The fix

`internal/agent/brokenpipe_guard.go`:

```go
func armBrokenPipeSurvivalForRowOwner() {
	signal.Ignore(syscall.SIGPIPE)
}
```

called from `maybeRegisterSessionTask` immediately after the row INSERT
succeeds. The window between the successful INSERT and this call holds no output
write, which is why arming there cannot itself lose the row.

**Scope, stated so it is not read as "gopi ignores SIGPIPE everywhere":** the
only caller is the minting arm. A `task run` child never reaches it (it latches
the wrapper row and returns before minting), and every command that owns no row
keeps the default disposition — measured on the built binary:

```
$ ./bin/gopi config show | head -2 ; echo exit=$?
gopi configuration in force
exit=141                                  ← still dies like a filter
$ ./bin/gopi chats 2>&1 | head -12        ← the launcher that produced the row
… survives; pre-fix this was exit 141 in 1.93 s …
```

**No config knob.** A switch that can re-arm "run dies silently and the row
never closes" would be a switch that can recreate the defect (Rule 35).

## The regression arm

`internal/agent/brokenpipe_guard_test.go` re-execs this test binary as a child,
hands it a stdout pipe, closes the reader, and lets the child drive the REAL
production path: `maybeRegisterSessionTask`, then a write to its own fd 1, then
the ordinary end hook. Two assertions: the child exits 0 with its survival
marker, and the row it minted reads `completed` from ITS OWN ledger (the
database comes from the shared fixture, Rule 32; the child publishes its verdict
through a file so only the side that resolved the path ever reads it).

Red proof — the fix removed, the call in `taskregister.go` stashed:

```
--- FAIL: TestRowOwnerSurvivesAClosedStdoutAndClosesItsRow (3.75s)
    brokenpipe_guard_test.go:124: the row-owning child did not survive a vanished
    launcher (exit: signal: broken pipe) — a run that owns an in_progress row must
    outlive its reader, or that row is never closed
```

Restored: `--- PASS`. The two files and the one call are the whole change.

## What this does NOT fix

A run killed by SIGKILL, an `_exit`, or a death inside a syscall still loses its
close-out, and this rule deliberately leaves that alone: those deaths are the
evidence of a defective close-out path, and the 2026-10-26 ruling is that the
ledger keeps showing them. The bug fixed here was different in kind — a NORMAL
launcher gesture (`| head`, a closed pty, a vanished wrapper) destroyed the run
by DEFAULT DISPOSITION, i.e. the process never had a chance to write anything.
That is a producer defect, and it is now closed.
