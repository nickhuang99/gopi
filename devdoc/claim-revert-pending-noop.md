<!-- doc-class: historical -->
# The claim-remote revert leg's two 0-row causes (false "terminal" on a pending row)

Task `01a0c2ce-e9dd-7796-bfbb-afc4fb56567f` (sender-side message defect, 2026-09-21).
Scope: `internal/tools/memory_sync_claim.go`, the CLAIM CHANNEL's peer leg.

## Symptom

`gopi task sync --push-only --task-id T` on a row that is `pending` answered

```
push <peer>: REFUSED: <task>: no longer reclaimable (terminal — left as-is)
```

for a row that is neither terminal nor raced — the sender-side face reported a
state the ledger never held. Same string on a bare `--push-only` (whole window).

## Root cause (5-why)

1. Why a refusal? `SyncClaimRemote` → `syncClaimRemoteRow` → verb `pending` →
   the revert leg `syncClaimRemoteRevert` returned a non-empty reason.
2. Why a reason at all? Its CAS is
   `UPDATE tasks SET status='pending', updated_at=? WHERE task_id=? AND status IN ('claimed','in_progress')`
   — an ALREADY-pending row is not a legal source, so `RowsAffected == 0`.
3. Why did 0 rows become a refusal? The `n == 0` branch had ONE message —
   `"no longer reclaimable (terminal — left as-is)"` — written for the race it
   was introduced for (the writeback that beats the rollback, 终态不回退), and it
   inferred "terminal" from the row count alone.
4. Why is that wrong? 0 rows has TWO causes with OPPOSITE meanings: *already at
   the goal state* (nothing to do — success) and *landed terminal* (refuse). The
   count cannot tell them apart; only the row's ACTUAL status can.
5. Why does it matter? A push is a STATE VERB: re-pushing a pending row restates
   the state the peer's row should have, so the no-op IS the success (the CLI
   help promises exactly this: "the claim broadcast's manual face — idempotent").
   The defect turned every idempotent re-push into an accusation, and the
   operator has no way to tell a benign no-op from a real terminal race.

## The fix

The leg now reads the row's REAL status **before** the write — the same
`taskByID` read the raced re-read already used — and splits the two causes:

| row status (read from the ledger) | answer | reason |
|---|---|---|
| `pending` | **accepted** (idempotent no-op) | `claimRevertNoopReason` = `"already pending — nothing to revert"` |
| terminal (`completed`/`failed`/`cancelled`/`verified`) | refused, wording unchanged | `claimRevertTerminalReason` = `"no longer reclaimable (terminal — left as-is)"` (+ `" (row is now …)"` on the raced path) |
| `claimed` / `in_progress` | the CAS does its real work | `""` |
| other / unknown | the CAS decides; a 0-row race re-reads and names the state that won | `"revert raced (row is now …)"` |

Acceptance in `SyncClaimRemote`: the leg answers with the composed line
(`<id>: already pending — nothing to revert`), and the handler classifies it by
the marker `claimRevertNoopMarker` (`strings.Contains`) as an
**accepted no-op** — the precedent is the existing `claimLockRowNotFound`
marker, which rides `ClaimPushResponse.Reason` as an accepted verdict's audit
reason. So `resp.Accepted` stays `true`, and the reason names the real state.

Rules: the four message/keyword strings are named constants (Rule 1b/13) — the
no-op string is COMPARED in the handler, and a bare literal compared elsewhere is
exactly the drift Rule 13 describes.

## Regression tests (`internal/tools/memory_sync_claim_revert_noop_test.go`)

At the `SyncClaimRemote` level (the peer's real handler), plus one at the push
surface:

- `TestSyncClaimRemotePendingRowAnswersIdempotentNoop` — pending row in →
  `accepted=true`, reason names the real state, and the string `"terminal"` (and
  `"reclaimable"`) is ABSENT.
- `TestSyncClaimRemoteTerminalRowKeepsRefusalWording` — a genuinely terminal row
  keeps today's exact wording (`<id>: no longer reclaimable (terminal — left as-is)`).
- `TestSyncClaimRemoteClaimedRowStillReverts` — the leg's real work survives the
  new gate (claimed → pending, `claimed_by` kept, accepted, no reason).
- `TestSyncClaimRemoteRevertReportsTheRacedStatus` — the OTHER 0-row cause: a
  writeback between the read and the CAS is reported as the state that won,
  never as "terminal".
- `TestSyncTaskPushOnlyAlreadyPendingRowIsAccepted` — the push surface
  (`SyncTaskPushOnly`, what the CLI verb calls) ships the row's verbatim pending
  state and prints the accepted line.

## Evidence (two nodes, one host)

`scripts/evidence-pushonly-pending-noop.sh <scratch-root> <gopi-bin>` — node A
(`GOPI_HOME=$root/a`) pushes over a PATH-level `ssh` shim, node B runs the REAL
`gopi task sync --claim-remote` handler. The shim resolves the remote's `gopi`
through PATH, so the harness MUST get a `gopi` symlink to the binary under test
(otherwise the installed binary answers with the pre-fix wording).

```
pre-fix binary  (branch base 96325d30)   RESULT: 2/3 PASS   (ACCEPTANCE 1 FAIL:
    "push …: REFUSED: t-proof-pending: no longer reclaimable (terminal — left as-is)")
fixed binary                             RESULT: 3/3 PASS   (ACCEPTANCE 1:
    "push …: accepted (2 row(s))"; ACCEPTANCE 2 keeps the refusal wording;
    ACCEPTANCE 3: the peer's 1..8 cap still refuses a 14-row window)
```

The same red→green at the unit level (a temporary probe re-installing the old
inference in the new structure fails the terminal-wording test, green when
restored) is recorded in the task report.

## Not in this branch

The `--task-id` ROW FILTER (a manual push ships its whole window because
`SyncTaskExportDelta` has no single-row option) is a separate defect with its own
branch `fix/push-only-task-id-row-filter` — read for context, deliberately not
folded here. Consequence for this harness: with the unfixed row filter the sender
ships its whole window, so the fixture must stay under the peer's 1..8 cap until
that branch lands; ACCEPTANCE 3 grows the ledger on purpose to show the cap is
untouched.
