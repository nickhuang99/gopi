<!-- doc-class: normative -->
# The holder-gone marker needed TWO records to agree, because the ledger keeps two

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task row: `01a1bb5b-0fd3-7897-849a-bd7c399b29e7` (cut point `ceb1436`).
Scope: `internal/tools/taskholder_gone.go` (`taskHolderGoneProof`), plus
`internal/tools/taskholder_gone_agreement_test.go`.

## The defect, measured

`gopi task show` printed, in ONE output, two liveness judgements that contradict
each other — captured on the real ledger before any change
(evidence: the run's session directory, `evidence/realrun-unfixed-01a1bb2d.txt`):

```
status:         in_progress + holder dead (pid 4122750 gone)
claimed_by:     host-b:3177317 (at 2026-11-08 20:47:42)
process:        pid=4122750 start=2026-11-08T20:47:42+08:00 cmd="/home/user/work/gopi/bin/gopi"  (live: gone)
```

and the two pids, measured directly:

```
$ for p in 4122750 3177317; do [ -d /proc/$p ] && echo "$p ALIVE cwd=$(readlink /proc/$p/cwd)" || echo "$p GONE"; done
4122750 GONE
3177317 ALIVE cwd=/home/user/.gopi/sessions/01a1bb2d-e7bc-7d28-a79f-da6b445a94ab
```

So the row was marked dead while the process its own claim stamp names was
running, with its working directory standing in that row's session directory —
a healthy run reported as a corpse, on the surface an operator scans.

## Root cause: the two records are stamped by two different processes

The ledger records a run's life TWICE, and the two records come from different
processes with different lifetimes:

| record | stamped by | lifetime | renewed when |
|---|---|---|---|
| `proc_pid` / `proc_start_ticks` / `proc_cmd` | the **executor incarnation** — the child the worker spawns per attempt | one attempt | the execution body is replaced (checkpoint resume, a tool restarted in a fresh process) |
| `claimed_by` (`host:pid`) | the **claimant** — the long-lived `gopi worker` that took the row | the whole run | at claim time, and it does not move when the executor child is replaced |

`taskHolderGoneProof`'s identity arm consulted the first record and nothing else
whenever `HasProcIdentity && ProcPid > 0`, and its own comment asserted the two
were "the same incarnation by construction". **That assertion is the bug.** They
are different pids naming different processes *by construction*, so the moment
the executor child is replaced — which is the normal shape of a long run — the
identity record names a number that no longer exists while the run continues.
`Dead` on record ① is a fact about one ATTEMPT; only record ② is a fact about
the RUN.

Reproducing the divergence needs no fault at all:

```
before      ① Dead (executor child gone) + ② alive (claimant running)  → marker emitted  ✗
after       ① Dead + ② alive                                          → no marker      ✓
```

## The fix

`taskHolderGoneProof` now makes the claim only when every record this host can
**read** agrees the run is over:

- **① (identity layer) must measure a death** — `Dead` or `Recycled`, unchanged
  from before; an `Unknown`/`NotMeasured`/`Running` answer still yields nothing;
- **② (claim stamp) is a VETO** — `taskHolderLiveClaimPid` resolves the stamp
  through the package's ONE parser (`claimHolder`: empty / unparsable / foreign
  host / a stamp that predates the run are all "not readable here"), and a pid
  this host measures `pidAlive` suppresses the marker entirely;
- **the exemption is a measurement, not a convenience** — when the stamp names
  the *same number* as the recorded incarnation, there is only ONE record, not
  two: that "alive" reading is the stranger the ladder already answered
  `Recycled` about, and it carries no new evidence. `taskstate.go` settled this
  shape the other way ("(its number was reused, or it is gone). That is a decidable row"), so letting it veto would silence every recycled row. The veto
  therefore requires **two different numbers**.

Every unmeasurable reading keeps the direction it already had: "I could not
measure it" removes a veto and never manufactures a death claim (Rule 15). The
no-identity path is unchanged.

Restructured for Rule 14 (one implementation per question): ① became
`taskHolderIdentityProof`, ② became `taskHolderLiveClaimPid`, and both emitters
(the list marker and the `task show` status suffix) reach the proof through the
same two functions, so "no claim stamp" and "the claimant is gone" cannot be
spelled two ways.

## Evidence

- **Red proof on the unfixed tree** (`git show HEAD:internal/tools/taskholder_gone.go`
  restored byte-for-byte; `test "$(git hash-object internal/tools/taskholder_gone.go)" = "$(git rev-parse HEAD:internal/tools/taskholder_gone.go)"`
  verified before the run, so the restore was measured, not assumed):

  ```
  --- FAIL: TestHolderGoneMarkerSilentWhenTheClaimantIsAlive (0.01s)
      taskholder_gone_agreement_test.go:110: a row whose CLAIMANT is alive must claim nothing; got pid=1331150 proof="gone"
  --- FAIL: TestHolderGoneMarkerAgreementOnTheRealBinary (7.15s)
      a row whose claimant 1338326 is ALIVE carries a death claim:
          status:         in_progress + holder dead (pid 1338327 gone)
  ```

  (The third arm, `TestHolderGoneMarkerStillMarksTheTrulyDeadShapes`, is GREEN
  on the unfixed tree by design — it is the zero-false-negative half, and a test
  that goes red before the fix cannot measure that nothing was silenced.)

- **Real run, fixed binary** — same real ledger row, same moment sequence:

  ```
  $ ./bin/gopi task show 01a1bb2d-e7bc-7d28-a79f-da6b445a94ab
  status:         in_progress
  claimed_by:     host-b:3177317 (at 2026-11-08 20:47:42)
  process:        pid=4122750 start=2026-11-08T20:47:42+08:00 cmd="/home/user/work/gopi/bin/gopi"  (live: gone)

  $ ./bin/gopi task list --status in_progress | grep 01a1bb2d
  #01a1bb2d-e7bc-7d28-a79f-da6b445a94ab  [in_progress]  keyword_search 的 rg 输出用 CombinedOutput 全量进内存：128 MiB 常量只界一行，不界总量  (model deepseek-flash, source agent, created 2026-11-08 20:41:50)  [running 1h3m12s since 20:47:42] [via worker] [ground master @ /home/user/workspace/01a1bb2d-e7bc-7d28-a79f-da6b445a94ab]
  ```

  Both surfaces lost the false marker, and the `process:` line still reports the
  measured `gone` — the false *inference* went away, the raw measurement stayed.

- **The marker still fires on a genuinely dead run** — `TestHolderGoneMarkerStillMarksTheTrulyDeadShapes`
  covers identity-Dead + claimant-Dead, identity-Recycled + claimant-Dead (the
  wording follows the recorded incarnation), identity-Dead with no stamp, and
  identity-Dead with a STALE stamp (a previous claimant's live pid may not veto).
  `TestHolderGoneMarkerAgreementOnTheRealBinary` carries a second row driven
  through the real binary whose run really ended.

## Why not a status write

Unchanged from the file's own header: no process watches another one's row
(operator ruling 2026-11-05), so this is still a measurement rendered at render
time — no transition, no sweep, no timer. The fix changes only WHICH measurement
is allowed to speak.

## The residual direction, stated (never silent — Rule 15)

The veto can only ever SUPPRESS a marker, so the fix trades a false POSITIVE (a
working run called dead — the measured defect) against a false NEGATIVE (a run
that really ended, whose claim pid was recycled to an unrelated live process
before the render, is left unmarked for that window). Two things bound it and a
third states it:

- `claimStampFreshness` — a stamp older than the row's own `in_progress_at`
  names a PREVIOUS claimant and is not read at all, so the sibling-churn window
  cannot reach the veto;
- `claimHolder` — a foreign host or an unparsable stamp is "not readable here",
  which leaves the identity reading standing (the deny-by-default direction);
- and the row is not made unmarkable forever: `gopi task show`'s `process:` line
  still prints the raw `(live: gone)` measurement, so a reader who needs the
  identity fact keeps it. What the marker stops doing is the INFERENCE.

The direction is deliberate, and it is Rule 15's asymmetry: an unmeasured or
contradicted reading may weaken a verdict, never manufacture one. A marker that
fires on healthy runs teaches an operator to ignore the marker entirely, which
is strictly worse than a marker that lags by one recycled number.
