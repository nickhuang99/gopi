<!-- doc-class: historical -->

> **RETIRED (2026-10-29, task `01a183d8-1ac2-7c54-b811-4f6c10f6ba17`) — the
> AUTOMATIC leg described below no longer exists.** The per-dispatch
> announcement (`announceLedgerAgreement`, wired at the claim gate in
> `cmd/agent/cli_tasks.go`) was DELETED, together with
> `cmd/agent/task_ledger_agreement.go`, `cmd/agent/task_ledger_agreement_cache.go`,
> their two test files, and the `ledger-divergence` notify event that only they
> fired. Why: with two peers declared it cost ~0.9s of ssh per worker round and
> its verdict was a CONSTANT — 235 status-mismatches that stayed mismatched for
> hours across 6+ alarms and 0 operator actions. Nothing could act on the
> announcement, so it was a cost with no consumer. **No switch was left behind**
> (Rule 35: deleting is deleting).
>
> **What was KEPT, deliberately** — the judgement was "the worker must not carry
> this", never "this capability must not exist":
>
> - `gopi task diff --peer <alias>` and its declared-peer form (`gopi task diff`
>   with no `--peer`), backed by `tools.LedgerAgreementReport` — the explicit,
>   always-fresh, read-only comparison.
> - `gopi task sync --full <peer>` — the explicit repair.
> - `tools.LedgerAgreement` + `Agreed()` / `Summary()` / `Detail()` /
>   `DivergentCount()`, and the peer-row seam (`SetLedgerAgreementPeerRowsForTest`)
>   the surviving tests drive.
>
> **What went with it**: `StructuralConflict` / `StructuralConflictLine` and
> `LedgerAgreementStructuralMarker` (the tighter per-dispatch judgement existed
> only to serve the automatic leg), `LedgerAgreementPeerCatalog` /
> `LedgerAgreementReportFor` (the peer-catalog cache's read half),
> `LedgerAgreementAlarm` / `LedgerAgreementUncheckedAlarm` /
> `fireLedgerDivergenceAlarm` / `SetLedgerDivergenceAlarmSinkForTest`, and the
> whole peer-catalog cache in `task_ledger_agreement_cache.go`.
>
> Every section below is the record of WHY that mechanism was built. It is kept
> for the reasoning, not as a description of the current tree.

# The Ledger-Agreement Precondition — "do my ledgers agree?" is now asked

Epic: the peerbox/HP ledgers diverged and **nothing in the ordinary operator path
surfaced it** before cross-node machinery (claim broadcast, the remote CAS's
"is this row already being worked elsewhere?") relied on the two agreeing.

`gopi task diff --peer X` (read-only, exit 1) and `gopi task sync --full X`
(explicit repair) already existed and are **reused verbatim — nothing was
rebuilt**. What was missing is the *precondition check*: the question was
answerable on demand and never asked.

## What landed

| piece | where |
|---|---|
| the check itself (read-only report over the diff's own set logic) | `internal/tools/ledger_agreement.go` |
| the alarm event (`ledger-divergence`), registered for the `all` expansion | `internal/tools/tasknotify.go` |
| the integration point: the dispatch claim gate | `cmd/agent/task_ledger_agreement.go` + the call in `runTaskRunOnce` (`cmd/agent/cli_tasks.go`) |
| the red proof (+ two companion state tests) | `cmd/agent/task_ledger_agreement_redproof_test.go` |

### 1. The check: three outcomes, never two

`tools.LedgerAgreementReport(peer)` reuses the diff's read path
(`reconLocalRowSets` + the peer catalog read + `reconCompare`) — so the
precondition and `gopi task diff --peer` can never drift into two notions of
"divergent". It has exactly **three** outcomes, because the defect being closed
is a check whose failure looks like success:

* **agreed** — quiet. Without `GOPI_DEBUG` an ordinary run prints nothing; a
  passing precondition is not narration, and noise is how operators learn to
  ignore a signal.
* **diverged** — counts per class, a bounded id sample (8 per class, truncation
  announced) and the two commands: `gopi task diff --peer P` (diagnose) and
  `gopi task sync --full P` (repair, explicit).
* **UNCHECKED** — ssh down, timeout, unparsable reply. Printed as
  `UNCHECKED` **and** alarmed. "I could not check" is a different operator
  action than "they agree"; reporting it as agreement is exactly the silent
  staleness this epic names.

### 2. Topology from the node's own declaration

The peer list is `config.ResolveSyncPeers(nil)` — settings.json `sync_peers`
(an explicit `GOPI_SYNC_PEERS` still wins, because that IS the documented
precedence; the point is that the operator need NOT supply it). The acceptance
invocation below empties that variable to prove the topology came from the
declaration alone. A peer-less node is single-node: no second ledger exists to
disagree with, and the check is skipped by the same `config.HasSyncPeers()` gate
the claim protocol itself uses.

### 3. Why the claim gate is the ONE justified integration point

Cross-node dispatch is *defined* by the remote CAS: Phase-2 asks the **peer**
for a `task_id` this node holds, so "is this row already being worked
elsewhere?" answers correctly only while both sides agree on that row. The gate
is:

* **where the assumption is used** — checking anywhere else checks a different
  fact;
* **a single choke point** — every dispatch funnels through `runTaskRunOnce`
  (manual `task run`, the worker round, auto-dispatch);
* **cheap** — ONE peer read per dispatch (measured `~0.45 s` for the ssh leg),
  not per row;
* **reachable without new surfaces** — no new tool (Rule 3), no new CLI verb,
  no daemon.

### 4. No auto-repair — the divergence IS the evidence

Nothing in this path writes, merges or deletes a row; the only two operations
are a local `SELECT` and one peer-side `SELECT`. A divergent pair is *data*
which shows what diverged and in which direction, and converging it
automatically would destroy precisely that. The gate is **advisory on purpose**:
it never blocks the claim either — it states the precondition failure and lets
the operator decide (repair, or dispatch anyway). The red proof asserts the
fixture's rows are untouched afterwards (the local-only rows still exist, the
mismatch is still a mismatch, the peer's row was never imported).

## Measured divergence (live, HP → peerbox, 2026-09-21)

`env -u GOPI_SYNC_PEERS ./bin/gopi task diff --peer peerbox` → **exit 1**; the
same run through the NEW no-`--peer` face
(`env -u GOPI_SYNC_PEERS ./bin/gopi task diff`) → **exit 1** with the same
classes, i.e. the declared topology answered the question the operator did not
have to supply:

```
diff peerbox: ledger agreement: DIVERGED from peerbox — 759 differing rows
  (missing-on-peer=748 missing-on-local=9 status-mismatch=2; local=2949 peer=2210 rows).
  ...
  missing-on-peer  (748): t-1788053263-1, … (+740 more)
  missing-on-local (9): 01a0c2cb-72cc-…, decoy-guest-probe-1 (+1 more)
  status-mismatch  (2): 01a0c2a5-…: in_progress≠completed, 01a0c2b4-…: in_progress≠completed
```

The per-class table (peer counts move by 2 rows between reads because peerbox is
live — `missing-on-local` was 7 and `status-mismatch` 2 at the first read,
9/2 at the second; **no repair was run by this work**):

| class | count | dominant shape | authoritative direction | severity |
|---|---|---|---|---|
| missing-on-peer | 748 | 670 peer-origin (`from-peerbox` tagged) + 78 origin; 686 cancelled, 62 completed | **None needed — informational.** Cancellation does not replicate, so cancelled rows are *expected* to exist only where they were cancelled. | **Low per row.** No live work at risk. But it contradicts the M4.24 "identical before the anchor" premise, so it is the noise floor any future check must not mistake for a defect. |
| missing-on-local | 7 | 6 `pending` + 1 `cancelled`, all peer-origin (peerbox's rework queue: incl. `t-stale-sibling`, `decoy-guest-probe-1`) | **Peer (peerbox) is authoritative** — these are rows **peerbox created**, or created from peerbox's task id, within the delta window, which our anchor has not pulled yet. | **MEDIUM-hard** — the concrete "is this row already being worked elsewhere?" failure: a peer-pending row unknown locally can be dispatched here, and for id-mirrored rows the local write path can refuse the insert (`task_id already exists`), which is a hard dispatch failure. |
| status-mismatch | 2 | both `01a0c2a5…`, `01a0c2b4…`: local `in_progress` ≠ peer `completed` | **Peer (peerbox) is authoritative** — both carry `worker:peerbox`, i.e. peerbox is where the work ran and where it finished. A completed verdict outranks a local in_progress copy (the rank-merge's terminal-wins ladder). | **HIGH and the actionable class** — the local copy is a phantom in-flight row: a node that offers it for dispatch can chase work that is already finished, and the local `in_progress` will block reclaim until reconciled. |

The asymmetry between the first two rows is the same rule seen from both sides:
**peerbox does not rewind cancellations, so a cancelled row stays local to
whoever cancelled it** (670 of the 748; and `t-stale-sibling` travelled to
peerbox as a cancel because it was cancelled *there*). The remaining classes are
the anchor window's latency plus one genuine rank disagreement.

**No repair was run as part of this work.** The divergence above is left
exactly as measured (evidence); `gopi task sync --full peerbox` is the operator's
call.

## Not Done (honest)

* **No automatic repair, by design** — argued above, not skipped.
* **The check is not wired into the serve/sync tick** (only the dispatch gate).
  A second integration point would have been a second cost without a second
  assumption; if the tick should also assert it, that is a follow-up.
* **BUG 1** (`sync --push-only --task-id` row filter) is untouched — separate
  ticket, different code paths, explicitly out of scope.
* **`internal/uiclient/*` / `cli_ui.go`** untouched (not in master) — the check
  is a CLI/wrapper-side surface.
* **The M4.24 normative doc does not name the no-`--peer` form** — the new face
  lives in `syncUsageHead` (asserted by `TestTasksSyncFamilyHelp`) and here; a
  doc follow-up, not a behaviour gap.

## The two surfaces the branch broke, and how they were found

Worth recording because both are the SHIPPED-help / gate-classification family,
not the feature's logic:

1. **`syncUsageHead` — replacing instead of extending.** Writing the no-`--peer`
   face into the family help, I overwrote the `gopi task diff --peer <alias>`
   synopsis line. `TestTasksSyncFamilyHelp` (the family's single-home guard) went
   red with `syncUsageHead is missing "gopi task diff --peer <alias>"`. The fix
   restores the original line verbatim and puts the new sentence in the prose
   below it — a help surface's obligation is to advertise BOTH faces. A branch
   that adds a face by deleting another is the failure mode this test exists for.
2. **`TestAssertionTotalityAuditGate` — an unclassified negative assertion.**
   The gate scans every test tree for "nothing ran / must not fire" assertions
   and fails any unclassified one (`auditTotalityAllow`). The new
   `TestLedgerAgreementAgreedIsQuiet` ("an agreeing pair fires no alarm") owed
   its classification; adding it was the whole fix. The reason recorded with it
   is the interesting half: the set is an **alarm sink**, not a step list, so
   there is no idempotent member a guard could wrongly exclude — and the positive
   half is pinned twice over (`TestTaskRunGateReportsLedgerDivergence` proves the
   alarm DOES fire on a diverged pair).

And two defects found by re-reading the diff rather than by any gate:
`syncUsageHead`'s new sentence originally carried **backticks inside the backtick
raw-string const** (terminating it — a build break, caught by `go build` only
because it was re-run after the edit), and a **trailing-space change** on the
`serveFinishCleanupQuery` const line (reverted). Rule for the next agent: after
editing a raw-string const, rebuild; and read your own diff for whitespace
before committing.
