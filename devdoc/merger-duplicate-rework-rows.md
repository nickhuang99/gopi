<!-- doc-class: historical -->
# The merger re-issued rework rows for ONE branch — a live-only dedup probe cannot see a MIGRATED row

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a115be-12ad-710d-ba37-cbac506ee63b` (branch `task/01a115be12ad710dba37cbac506ee63b`).
Fix commit: see §7. Every claim below carries a `file:line`, an excerpt of a real ledger, or
pasted command output from a run that was actually executed.

## 1. The symptom, and where it was measured

The merger files ONE branch-rework row per branch, keyed on a deterministic summary:

```
internal/tools/task_merge.go  resolveRowReworkSummaryPrefix + branch
  → "resolve merge conflict: rework: <branch>"
```

Measured on the two live ledgers (2026-10-07), for ONE branch
`task/01a113e467767eb9a4c33cb1df1cdfbb`:

```
peerbox$ sqlite3 -readonly ~/.gopi/memory.db \
  "SELECT status,COUNT(*) FROM tasks WHERE summary='resolve merge conflict: rework: task/01a113e467767eb9a4c33cb1df1cdfbb' GROUP BY status;"
cancelled|3
pending|1

hp$ …same query…
completed|1
pending|1
```

Four rows on peerbox, two on hp — for one branch. The rows, in filing order:

| id | filed by | then | why |
|---|---|---|---|
| `01a114aa` | peerbox merger | `cancelled` — *sent to peer hp* | migrated |
| `01a11564` | peerbox merger (16:04) | `cancelled` — *sent to peer hp* | migrated |
| `01a1156f` | peerbox merger (16:16) | `cancelled` by hand, reason names the duplication | the duplicate this task was filed on |
| `01a115bb` | peerbox merger | `pending` (still) | the third re-issue |

`gopi task show 01a1156f`'s own reason column says it: *"merger 重复派单：同一分支 … 的 rework
已由 01a11564 派发并已迁往 HP 执行。本行是 merger 在 12 分钟后（16:16 vs 16:04）对同一分支的
第二次出单，deliverable.branch 逐字节相同。"*

## 2. Root cause (5-why)

1. **Why a second row?** The dedup probe answered "no row of this summary exists".
2. **Why did it answer that?** It asked only about LIVE rows:

   ```go
   // internal/tools/task_merge.go (before this change)
   SELECT COUNT(*) FROM tasks WHERE summary = ?
     AND status IN ('pending','claimed','in_progress')
   ```

3. **Why was the row not live?** Because it had been MIGRATED to the peer — and the
   migration's own cancel leg sets it to `cancelled` here:

   ```
   gopi task send --cancel-source --peer hp
     → "cancel-source: 01a11564… cancelled here (peer hp holds it: stored)"
   ```

4. **Why is that a defect rather than correct?** Because for this lane `cancelled`
   carries TWO OPPOSITE meanings — "abandoned, nobody is working this branch" and
   "somebody else is working this branch now" — and the probe could only answer the
   first. A hand-cancel is a decision that the branch is NOT being worked; a migrated
   cancel is a decision that it IS, elsewhere.
5. **Why was the second row harmful?** Two ledgers held a `pending` row for one branch
   (peerbox `01a1156f`/`01a115bb`, hp `01a11564`), so two workers could claim the same
   branch, and a rework whose whole premise is "one branch, one reworker" had two.

## 3. The design decision: the MARKER, not the status

The naive fix — add `cancelled` to the live set — is forbidden by the same reasoning
that made the lane live-only on purpose. A branch whose rework the operator ABANDONED
must be able to enter the lane again; folding `cancelled` into "live" would make one
deliberate cancellation (or the very `01a1156f` row above, cancelled by hand *because*
it was the duplicate) permanently un-reworkable. Measured as RED by
`TestReworkDedupManualCancelStillRefiles` on that candidate fix.

Considered and rejected, with the measurement each one failed on:

| candidate | verdict |
|---|---|
| `cancelled` ∈ live set | breaks the abandonment arm (a branch can never be reworked again) |
| read the auto-written **reason** | it is prose an operator may rewrite (`gopi task update … --reason WHY` is the documented undo), and matching a sentence is not a fact |
| switch the migration's status to **`superseded`** | semantically right (`superseded` = "the work is elsewhere", minted 2026-10-07), but a hand-cancel can reach it too — a status still cannot carry the fact — AND it would change a surface two existing suites pin (`cmd/agent/cli_tasks_send_cancel_test.go`, `memory_sync_row_gate_test.go`) for no gain in precision |
| **a TAG on the row** | ✅ taken: the family's own vocabulary for a row fact no status expresses (`from-<node>` already means "this row travelled"), a TOKEN rather than a sentence, and it survives a rewritten reason |

So: `handed-off`, written by the ONE writer of the migration decision and read by the
lane's ONE gate.

## 4. The fix

**Writer** — `internal/tools/memory_sync_row_cancel.go`, `SyncRowCancelSource` (the
`--cancel-source` leg, after the status write it belongs to):

```go
note := fmt.Sprintf(syncRowCancelLineFormat, taskID, peer, effect)
if terr := syncRowStampHandoff(taskID, opts.DBPath); terr != nil {
    note += fmt.Sprintf(syncRowHandoffStampFailSuffix, terr)   // reported, never swallowed
}
```

`syncRowStampHandoff` appends the tag through `taskAddTag` (the family's own idempotent
writer) inside the same per-ledger pin the cancel just ran under — a marker written into
a DIFFERENT ledger than the row is exactly the namesake-row defect
`syncRowWithLedger`'s header records. A failed stamp does **not** roll the cancel back:
the row really is cancelled here and the peer really does hold it, so undoing the cancel
over a tag write would manufacture the double-dispatch the cancel exists to prevent
(Rule 15 — the failure is REPORTED, with its consequence, in the confirmation line).

**Reader** — `internal/tools/rework_handoff.go`, `reworkSummaryBlocked(summary)`, the
lane's ONE dedup entry with two arms:

1. **LIVE** (unchanged): a row of this summary is `pending`/`claimed`/`in_progress`.
2. **HANDED OFF** (new): a row of this summary is TERMINAL **and carries
   `handed-off`** — the work is on a peer, so a second row here would put one branch in
   two workers' hands.

Ordered live-first (so a long history cannot bury a running row) and capped at 50 rows
(the identity is deterministic, so a summary normally owns one or two). A plain
cancellation, a `failed` row and a `completed` row deliberately do **not** block.

> **Retired mechanism (merge verb removal, 2026-11-01).** The `$ gopi merge
> --max-context-tokens 1` line below is the RECORD of what this run executed, kept
> verbatim; the top-level verb was removed afterwards and answers **exit 1** today
> (`gopi task merge` runs the same engine). The ceiling lane it exercises is
> unchanged.

**Every consumer now reads the ONE entry.** `reworkSummaryLive` (the boolean projection)
is a thin wrapper over it, and the refusal lanes that RENDER a reason to an operator —
contract gate, build gate, manifest gate, merge ceiling, hand-over escalation — take the
two-value form so their line names the fact that stopped them instead of always claiming
a live row:

```
$ gopi merge --max-context-tokens 1          # PASS 2, same branch, after the migration
[task-notify] merge: ceiling stop for feat/rh-probe — branch-rework task 01a115e1-… was
  HANDED OFF to a peer by `gopi task send --cancel-source` and is cancelled HERE by that
  move (the branch-rework lane's work continues on the peer, whose own row carries it) —
  one branch, ONE rework row, so nothing is filed here
```

## 5. Proof — the same probe, both binaries, real surfaces

`gopi merge` needs this repo's `repos` table, and an isolated profile needs a repo whose
branch genuinely conflicts, so the probe built a fixture repo (both sides touch `f.txt`)
and drove the REAL verbs against a REAL peer ledger over ssh
(`gopi task send --cancel-source --remote-home /tmp/rh-peerhome`).

| | master's binary | this branch |
|---|---|---|
| pass 1 | files 1 row (`pending`) | files 1 row (`pending`) |
| migrate to the peer | source row → `status=cancelled`, tags `merge-conflict,branch-rework` | source row → `status=cancelled`, tags `merge-conflict,branch-rework,handed-off` |
| pass 2 | **files a SECOND row → 2 rows, one branch** | **refuses: "…HANDED OFF… one branch, ONE rework row" → 1 row** |

Verbatim tail of the master run:

```
[master] rows after pass 2 = 2  (1 = deduped, 2 = THE BUG)
#01a115dd-6d43-70b9-832a-1ea3a1887c59  [pending]    resolve merge conflict: rework: feat/rh-probe
#01a115dd-61bd-7352-9ebe-2efc243ffc99  [cancelled]  resolve merge conflict: rework: feat/rh-probe
```

and of this branch's:

```
[fixed2] rows after pass 2 = 1  (1 = deduped, 2 = THE BUG)
#01a115e1-eedd-77b3-babd-118caee332fc  [cancelled]  resolve merge conflict: rework: feat/rh-probe
```

The RED→GREEN transition is also pinned as a unit test:
`internal/tools/rework_handoff_test.go` → `TestReworkDedupHandedOffReworkIsNotRefiled`
fails on master's probe with *"the lane is FREE for a handed-off branch"* and passes with
the fix.

## 6. What this does NOT do (honest limits)

- **It does not make `gopi task send` retract the row on the peer.** Both ledgers keep
  their copy; that is by design (the peer must have a row to run). The gate is about not
  filing a THIRD one here.
- **The rows already in the ledgers are not touched.** peerbox still holds `01a1156f`
  (hand-cancelled, correctly) and `01a115bb` (`pending`). `01a115bb` is the row the
  branch's rework would legitimately run as; re-running the merge on this branch will
  now refuse it because `01a114aa`/`01a11564` carry no marker (they predate this
  change) — that is the conservative direction. Marking those two rows by hand is an
  operator decision, not a code change: the *next* migration stamps the marker on its
  own.
- **A marker written by an older binary is absent.** The gate reads the marker, so a
  migration performed before this commit does not block; the fix closes the loop from
  this commit forward.
- **The probe is per-ledger.** A row migrated by *this* host is cancelled and marked
  here; the peer's copy is `pending` there and is the one that runs. That asymmetry is
  the whole point, and it is what the refusal sentence tells the reader.

## 7. Files, commit

| file | what |
|---|---|
| `internal/tools/rework_handoff.go` | NEW — the lane's one dedup entry + the marker constant + the two refusal sentences |
| `internal/tools/rework_handoff_test.go` | NEW — the red→green regression, the abandonment arm, the identity/token arms, the writer's positive+negative arms |
| `internal/tools/task_merge.go` | `reworkSummaryLive` is now a projection of the one entry; the contract/manifest/build refusal lanes print the entry's own reason |
| `internal/tools/memory_sync_row_cancel.go` | the cancel leg stamps `handed-off` on a persisted migration; a failed stamp is reported |
| `internal/tools/memory_sync_row.go` | the stamp-failure suffix constant |
| `internal/tools/merge_ceiling_rework.go`, `internal/tools/merge_handover_escalate.go` | their operator-facing line names the fact that stopped them |
