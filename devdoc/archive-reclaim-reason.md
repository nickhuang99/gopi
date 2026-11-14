<!-- doc-class: historical -->
# The archive-reclaim arm now records the WHY before it removes the head

**Epic**: archive-reclaim-reason (task 01a14a8c-a9f0-7715-be5f-626ea5bc6f71).
**Files**: `internal/tools/archive_ref_reclaim.go`, `internal/tools/archive_reason.go`,
`internal/tools/archive_ref_reclaim_reason_test.go`.

## The defect, measured

`archiveReclaimDelete` removed a decided branch's head ref and recorded nothing in
`refs/gopi/archive-reason/<branch>`. The archive TAG was (correctly) kept, so the
decision survived — but a tag alone is a WHY-less decision. Measured on peerbox
2026-10-17: 11 `refs/heads` removed by this arm, 10 of them with no reason record;
the operator-facing count `gopi task archive --check` reported `239 reason-less
archive(s)` and fell to `229` only after the ten records were written by hand.
The arm was manufacturing the very debt the reason lane exists to eliminate.

Reproduced end-to-end with the PRE-FIX binary (`git archive 2c56271` → build) on a
fixture repo: two archived heads removed, `0` reason refs afterwards, and the
classification report still reading `2 reason-less` (see the task report's
`## Evidence`).

## The fix — record, then re-check, then remove

`archiveReclaimOne` was split into `archiveReclaimOneWith(repo, branch, tag,
record archiveReclaimReasonRecord)`; the production entry point passes
`archiveReclaimReasonRecorder`, which calls the project's ONE archive writer
(`ArchiveTagReasoned`). The sequence between guard ② and the removal is now:

1. re-verify tag tip == head tip (guard ②, unchanged);
2. **write/ensure the reason record** (new);
3. `archiveReclaimDelete` re-checks the tip again and removes the head.

A record that cannot be written returns `ArchiveReclaimSkipped` with the writer's
error and **keeps the head ref** (Rule 15): deleting first would recreate exactly
the reason-less state the epic is about.

## Why the shared writer, and why a new code

The writer is `ArchiveTagReasoned`, so this arm cannot mint a second reason
namespace or a second record shape, and Rule 9's durable half is preserved
mechanically: that function only CREATEs a tag that does not exist, so an existing
archive ref is byte-identical afterwards (pinned by object id AND object type in
the test).

Idempotence is *by reason*, which is what protects the other lanes' WHY: a branch
already carrying `not-ff-able-park`, `compile-failure`, … keeps that record
verbatim through a reclaim — the reclaim must never restamp it with its own code,
because that record is the only copy of why the branch lost.

A new code was needed for the legacy reason-less tags (the 21:04 batch and
everything written before the reason lane): the arm's honest claim is not "why the
branch lost" but "the decision was already recorded as a tag, and the head ref was
reclaimed against that very commit". Hence `ArchiveReasonReclaimCarried`
(`reclaim-carried`), added in all three places the closed vocabulary lives — the
const block, the ordered `ArchiveReasons` list (last in the automatic-lane group,
since a lane writes it and no operator chooses it), and `ArchiveReasonDoc` — so it
appears in `gopi task archive -h`, the refusal messages and the completion message
without any further edit.

The `detail:` line names the arm (`removalaudit.ArmArchiveReclaimBranch`, the same
spelling the removal-audit record carries) and the tag tip, so "was this really
reclaimed against the commit the tag names?" is answerable from the record alone.

## Two seams, both test-only

- `archiveReclaimReasonRecord` — the function type. It exists so a test can drive
  the ORDER (a recorder that reads the head ref at call time and finds it still
  there) and the FAILURE half (a recorder that errors) without having to make git
  itself unwritable.
- The ledger owner lookup seam already existed; unchanged.

Neither forks the arm: the production entry point is one call away.

## Tests

`archive_ref_reclaim_reason_test.go`:

| test | what it pins |
|---|---|
| `TestArchiveReclaimRecordsTheReasonBeforeRemovingTheHead` | reason ref exists, IS a blob, parses to `reclaim-carried`, first line is `reason: …`, `detail:` names the arm + the tip, tag object id/type/commit unchanged |
| `TestArchiveReclaimNeverRewritesAnExistingReason` | a park-lane record survives byte-for-byte; a second pass writes nothing |
| `TestArchiveReclaimKeepsTheHeadWhenTheReasonCannotBeWritten` | the head ref still resolves at record time (order), and a failing record keeps the ref with the error reported |
| `TestArchiveReclaimStopsTheReasonlessBacklogGrowing` | fixture ledger row (`completed`, scaffold DB) — backlog 1 → 0 through the shipped arm; RED half drives the pre-fix path (no seam) and asserts the backlog still counts the branch |

The RED halves use the arm's own entry points with the reason seam removed, the
same shape `archive_ref_reclaim_audit_test.go` uses: an assertion that cannot go
red has stopped proving anything.
