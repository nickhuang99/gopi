<!-- doc-class: normative -->
# Independent Verification Report: task list display honesty

Ledger row under verification: `01a09435-9ffe-7998-a6c0-da485f7571dd`
Deliverable: branch `fix/task-list-state-honesty`, commit `70eece5a`, merged to
master by `aad25579` ("Merge branch 'fix/task-list-state-honesty'").
Verifier: independent session, read-only wrt the deliverable (no code fix, no
DB mutation of live rows, verdict recorded exactly once).
Date: 2026-09-12.

## Verdict: **failed**

The delivered fix does deliver the epic's headline capability — three states
that used to render one identical `[running Xh since HH:MM:SS]` label now
render distinct labels, and a real live-shaped run proves it on the operator's
own screen. But the state ladder is **wrong in two independent, reproducible
ways**, and both of them are exercised by *this machine's own* ledger rows —
i.e. they are exactly the rows the feature exists to interpret.

Per the standing verifier contract (a verifier never fixes), the defects are
reported here with reproductions, not patched.

## 1. Checks that PASSED

| Check | Command | Result |
|---|---|---|
| Fix reachable from master | `git merge-base --is-ancestor 70eece5a master` | OK (exit 0) |
| Merge identified | `git rev-list --parents -n1 aad25579` | `aad25579` tip, parents `01c8b838` (pre-fix master) + `70eece5a` |
| Machinery absent at the pre-fix parent | `git grep -lE 'taskStateSuffix\|taskPidIsRunner\|finished-but-unrecorded' aad25579^1` | empty (exit 1) |
| Build | `CGO_ENABLED=0 go build ./...` | clean (exit 0) |
| Build (binary) | `CGO_ENABLED=0 go build -o /tmp/gopi-verify-honest ./cmd/agent/` | clean |
| gofmt | `gofmt -l internal/tools/taskstate.go tasks.go taskstate_test.go taskstate_now_test.go tasklist_running_test.go` | empty |
| go vet | `go vet ./internal/tools/` | clean |
| Fix's own state tests, -count=1 | `go test -count=1 -run 'TaskState\|TaskListRunning\|Running' -v ./internal/tools/` | 13/13 PASS |
| Renderer is display-only | `grep -nE 'db\.\|Exec\(\|INSERT\|UPDATE\|sqlite\|WriteFile\|os\.Remove' internal/tools/taskstate.go` | no hit — no write path |
| No new status enum | labels are `const taskState*` display strings; the row stays `in_progress` in the DB | OK |
| Read-only on the live ledger | `sha256sum ~/.gopi/memory.db` before/after a full `task list` on the new build | identical (`43c2d058…3de9`) — though see the caveat in §4 |
| Rule 1b/13 | no bare `"sessions"`/`"in_progress"` literal in the new code — `config.SessionsDirName`, `taskStatusInProgress` referenced | OK |

## 2. Independent evidence: the operator's screen DID change (real data, strongest class)

`~/work/GoPi/bin/gopi` (mtime 19:29, master **before** the 19:47 fix commit;
`strings` confirms it carries none of the new labels) vs
`/tmp/gopi-verify-honest` (built from master `aad25579`). Same live ledger
(`~/.gopi/memory.db`, 1093 rows, 6 `in_progress`).

Live run, pid `1575642` — `/proc/1575642/comm` = `gopi`, state `S`, claim stamp
`peer1:1575642` = this host, `claimed_at` >= `in_progress_at`:

```
OLD  #01a09569  01a09569-1817-7be5-be31-299f055ffff6  [in_progress]  investigate task worker starting message  [running 8m33s since 19:44:28] [via worker]
NEW  #01a09569  01a09569-1817-7be5-be31-299f055ffff6  [in_progress]  investigate task worker starting message  [running 9m2s since 19:44:28] [via worker]
```

The healthy row keeps its pre-change wording byte for byte (modulo the clock)
— the compatibility half of the fix is real.

## 3. DEFECT 1 (hard): the reason text can be factually FALSE — reproduced on a live ledger row

`gopi task list` on the new build, live ledger:

```
#01a09573  01a09573-328f-756d-b895-0292fc7cf60e  [in_progress]  [auto] config dir  [unknown: claim stamp names another host, age 4m20s]
```

The ledger row at that moment (and still now):

```
status=in_progress  claimed_by=peer1:2881717  claimed_at=1789213750  in_progress_at=1789213750
hostname=peer1   /proc/2881717 -> absent (the pid is DEAD)
```

The stamp names **this host** (`peer1`); the label says **another host**. The
rendered reason contradicts the evidence it claims to report.

Root cause (`internal/tools/taskstate.go`,
`taskClaimUnverifiableReason`): the final fallback of the ladder asserts a
foreign host whenever `claimed_by` is non-empty and the stamp is not stale —
but the ladder is reached with that same input in two *different* situations
that the fix conflates:

1. the claim stamp really names another machine (pid unknowable here), and
2. the stamp names **this** machine, `claimHolder` returned `ok=true`, the pid
   probe **did** vote — and said **dead** (`aliveChecked=true, alive=false`).

Case 2 is a perfectly interpretable row (a killed/crashed run on this host), and
it is the realistic shape of a worker row that dies. It is reported as an
unknowable foreign host — and it stays that way even when this host's session
evidence is fully readable:

```
C same host + FRESH stamp + the pid probe actually voted DEAD + readable silent session
  -> "unknown: claim stamp names another host"
```

The engine knows the holder is not alive and still refuses to name that fact.

Minimal reproduction, **no stubs, via the production entry point**
`taskInProgressStateNow` — a row carrying row `01a09573`'s own stamp:

```
PRODUCTION PATH (claim="peer1:2881717", pid dead, stamp fresh, this host) -> "unknown: claim stamp names another host"
DEFECT REPRO: label claims a FOREIGN host while the stamp names THIS host (peer1)
```

(`claimHolder("peer1:2881717") -> host="peer1" pid=2881717 ok=true` — the
same-host branch is taken; `hostnameOrEmpty()="peer1"`.)

### Knock-on: the `died` verdict is effectively unreachable for real rows

Labelling the epics's own three situations:

| Epic situation | Realistic worker row shape | Rendered |
|---|---|---|
| (a) live run | stamp `peer1:<live gopi pid>` | `running` ✔ |
| (b) finished-but-unrecorded | session_end + worktree gone | `finished-but-unrecorded` ✔ |
| (c) **died / killed / crashed** | stamp `peer1:<dead pid>`, session silent | **NOT `died`** — `unknown: claim stamp names another host` ✘ |

Proof: `claimStampFreshness` returns `claimStampStale` only when
`ClaimedAt < InProgressAt - 5s` — a *previous* claimant. A current claimant whose
process died keeps a **fresh** stamp (the kill does not rewrite
`in_progress_at`), so an ordinary crash never takes the stale/empty/stale-vote
escape hatches. The `died` branch is therefore reachable only for a row with
`claimed_by` empty or malformed — a shape worker-claimed rows do not have.
That is visible in the fix's own fixture: its "died" test constructs
`&Task{Status: taskStatusInProgress, SessionID: "sess-crash", …}` with **no**
`ClaimedBy` and no `HasClaim` — a shape no claimed worker row has, so the test
never exercises the shape the state exists for.

Classification impact: both a foreign-history row and a crashed run report the
same string. A foreign reaper that has not yet noticed the dead pid will produce
a **mixed-liveness** screen (some `running`, some `unknown: … another host`)
that reads exactly like the "are these dead?" confusion the epic was filed to
remove.

*(Severity note, stated honestly: this is a false-reason / operator-confusion
defect, not a data-loss one — the renderer is read-only and does not close
rows.)*

## 4. DEFECT 2 (hard): a row on ANOTHER host renders `died`

The fix documents this case as unknowable in three places — the state table
("claim stamp names another host" → `unknown: …`), the ladder comment ("a
foreign pid is unknowable here"), and its own test comment ("FOREIGN HOST: a
remote claim's pid is unknowable here — unknown, **never `died`** … we cannot
see that process"). That promise is broken as soon as the row's session JSONL
is **readable on this host**, which is the normal case for a row executed on a
shared/stale host. A foreign stamp leaves `aliveChecked=false`, and the
session-only layer sit *above* the claim layer in the ladder:

```go
case !aliveChecked && st.SessionTailKnown && !st.SessionEnd:
    if st.Worktree != "" && !st.WorktreeGone { return unknown(<worktree held>) }
    return taskStateDied        // <- reached BEFORE any claim inspection
```

Reproduction (asserts the resolved state, and the fixture uses the
deliverable's OWN session-JSONL helper so the file really resolves — the first
attempt used a hand-rolled path that did not resolve and the probe silently
fell through to the claim layer, which is why the result is quoted with the
resolution proof):

```
session resolved="…/.gopi/sessions/sess-foreign/sess-foreign.jsonl" tailKnown=true end=false
B foreign + READABLE silent session, stale heartbeat -> "died"
```

So a row whose holder lives on another machine is asserted **dead** on the basis
of evidence gathered on *this* machine — the exact `died`-vs-unknowable
conflation the fix's own doc claims to have avoided. Which of DEFECT 1/2 an
operator sees is decided by an unrelated fact (whether the session file happens
to be readable here), so the same real situation gets two different labels:

| Claim stamp | Session readable here, no `session_end` | Rendered |
|---|---|---|
| `peer1:<dead pid>` (this host) | no | `unknown: claim stamp names another host` (FALSE reason — §3) |
| `other-host:<pid>` | yes | `died` (asserted without being able to see that process — §4) |
| `other-host:<pid>` | no | `unknown: claim stamp names another host` (correct) |

## 5. Gaps in the deliverable's own test suite (why the suite is green anyway)

The suite is green and the deliverable's §6 claims coverage of "pid reuse,
foreign host, missing session file". The two defects above survive it because
of **fixture choice**:

1. Every boundary fixture is a row with **no** `SessionID` or a session id that
   resolves to nothing (`taskProbeRowState` returns early), so the tests never
   reach the ordering that §4 concerns.
2. The "PID REUSE" case is built as `HasInProgressAt + InProgressAt = now-1m`
   with `ClaimedAt = now-2h` — which makes the stamp **stale**, so the pid layer
   is *skipped entirely* (`freshness != claimStampStale` is false => no probe
   vote at all). The test therefore never covers the real reuse shape (fresh
   stamp, live pid, foreign comm name), and `aliveChecked=true, alive=false`
   has **no test at all** — which is exactly the branch that produces DEFECT 1.
3. The "died" fixture omits `ClaimedBy`/`HasClaim` (see §3).

A verifier-authored probe in the deliverable tree (kept UNCOMMITTED, run in a
detached worktree at `aad25579`, recorded here so it is reproducible):

```go
func TestZZVerifierProductionPath(t *testing.T) {
    t.Setenv(config.GopiDirEnv, t.TempDir())
    now := time.Now()
    tk := &Task{
        Status: taskStatusInProgress,
        ClaimedBy: "peer1:2881717", HasClaim: true, ClaimedAt: 1789213750.0,
        HasInProgressAt: true, InProgressAt: 1789213750.0,
        HasToolcall: true, LastToolcallAt: float64(now.Add(-4 * time.Minute).Unix()),
        CreatedAt:   float64(now.Add(-4 * time.Minute).Unix()),
    }
    got := taskInProgressStateNow(tk, now)   // -> "unknown: claim stamp names another host"
    ...
}
```

## 6. Honestly listed gaps (not verified / not reproduced)

- **A trap I hit and am recording** (same class the deliverable's suite hit): my
  first §4 probe hand-rolled the session path, the file did **not** resolve,
  `taskProbeRowState` returned early and the case silently fell through to the
  claim layer — a false "pass". Re-run with the deliverable's own
  `taskStateTestSessionJSONL` helper (the resolution proof is quoted in §4) it
  reproduces. A verifier probe that does not assert the FIXTURE is as untrustworthy
  as a test that does not assert the state.
- The live ledger re-read during one probe advanced `memory.db`'s mtime
  (the list read runs the throttled stale sweep) although the byte checksum was
  unchanged, so the checksum result in §1 proves only "same bytes", not
  "no writer ran". No live row was mutated by this verifier; no row was
  projected or restored.
- `internal/tools` full-package `-count=1` was run as a background job subject
  to this session's time budget; the targeted selector run (13 tests) is the
  green evidence cited. If the full package is required for the verdict, it
  should be re-run to completion.
- Pre-fix parent A/B red-proof of the *fix's own tests* (copying
  `taskstate_test.go` into a worktree at `aad25579^1`) was **not** run: the file
  references `taskProbeRowState`, `taskInProgressState` and other
  fix-only symbols, so it cannot compile at the parent (the memory #2032 recipe's
  known limitation). The absence of the machinery at the parent was established
  by `git grep` instead (§1), which is the weaker but sufficient check for a
  NEW-file fix.

## 7. What should happen next (for a rework row, not for this verifier)

1. Make the claim layer authoritative for the `aliveChecked && !alive`
   situation (a this-host stamp whose pid is dead is a *decidable* row; it must
   not fall into the "unknowable" reason), or extend the reason vocabulary so it
   names *which* signal was unusable instead of reusing the foreign-host string.
2. Re-order the ladder so an unattributable (foreign / malformed) claim is
   settled before the session-only `died` verdict is asserted — or make `died`
   require a positive this-host dead-holder fact.
3. Add the missing fixtures: fresh-stamp + dead this-host pid (`aliveChecked`),
   foreign stamp + readable silent session, and a claimed "died" row.

Deliverable location of this report: branch `docs/task-list-state-honesty-verification-20260912`
(reachable from the shared repo).
