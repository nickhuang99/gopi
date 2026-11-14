<!-- doc-class: normative -->
# devdoc: a payload that CLAIMS an attachment, on a row that has none

Task: `01a0b53b-a1f4-7966-86b5-954f086b1f76` (branch `feat/attachment-claim-marker`).

## The defect

The attachments feature works end to end: `task_submit(attachments=[ABS_PATH…])`
copies each file under `<gopi-dir>/task-sessions/<task_id>/attachments/…` and
records a manifest in the row's deliverable JSON; `task show` renders an
`[Attachments]` block with one `[attachmentN: <replica>]` line per entry (and a
`from:` line naming the source path + sha256 + size) — and renders **nothing at
all** when the manifest is empty (`internal/tools/task_session.go:517` reader,
`cmd/agent/cli_tasks.go:1354` composition).

So the two situations are textually indistinguishable:

| row | `task show` before |
|---|---|
| payload says "screenshot attached", no `attachments` passed | *(no attachments section)* |
| payload mentions `internal/tools/tasks.go:2706` in passing | *(no attachments section)* |

The first is a **false evidence claim**: the reader (operator, or the dispatched
child that reads its own briefing) goes looking for evidence that was never
archived. Silent — the failure mode Rule 15 forbids.

## The fix (two faces, one predicate)

`internal/tools/task_attachment_claim.go`:

- `TaskAttachmentClaimScan(summary, payload) (phrase string, ok bool)` — the one
  predicate. Case-insensitive, word-boundary aware, longest-phrase-first.
- **READ face** (`TaskShow`): when the deliverable carries no manifest **and**
  the prose matches, print
  `[Attachments] NONE — but the payload references an attachment ("<phrase>"). No manifest, no replica files: the referenced evidence is NOT archived with this task.`
- **WRITE face** (`TaskSubmit`): the same predicate appends
  `warning: the payload asserts an attachment ("<phrase>") but no attachments were submitted — nothing was archived with this task; pass the file's absolute path via --attachment/attachments, or drop the claim`
  to the return value, which the CLI *and* the `task_submit` tool both surface.

### The decision: warn, do not refuse

A false prose claim is a **diagnosability** defect, not a reason to block work.
Refusing the submit would let a wording nit stop a dispatch, and a claim-carrying
row is still better than no row (the dispatcher is the product; the wording is
not). It also keeps the row legal and dispatchable, as the epic requires:
attachment-less rows are unchanged in status, claimability and payload handling.

### Why only asserting PHRASES fire (and no bare paths)

A payload that quotes `"/home/user/workspace/build.log"` is ordinary prose — the
run writes it itself, cites it as a source, or names a scratch path. Flagging a
path would fire on most real payloads, the marker would be tuned out, and the
loud-instead-of-silent fix would be worse than the silence it replaced.

Same reasoning removes the **bare article**: a payload *discussing the feature*
("the attachments area is created when …") is ordinary prose, and an article is
not a claim. Every phrase in the table is claim-SHAPED — *something attached* /
*see the attachment* / *included as attachment* / *attached to this task*.

This is also why the `sha256`/`from:` renderings were **not** touched: with a
manifest present the `[attachmentN:]` block already names what IS archived, so a
marker there would be a lie. The marker is scoped to the no-manifest case.

## Rule anchoring

- **Rule 14** — one definition site. The 45 phrases are named constants; the
  matching table is *derived from them at init* (longest-first), so the constant
  list and the scanned list cannot drift. The constants stay in `internal/tools`
  (not a leaf package) because they have exactly ONE consumer: this package's
  `TaskShow` + `TaskSubmit`. `cmd/agent` prints the marker string it is handed —
  it never spells it — so a leaf package would be a second definition site, i.e.
  exactly the mirroring Rule 14 bans.
- **Rule 1b/13** — no inline literal decides anything: the marker label, its
  tail sentence, the warning template and every phrase are constants.
  `TestAttachmentClaimGate_LiteralsLiveOnlyAtTheirDefinition` scans the dir's
  production `.go` files and fails any that carries the marker literal, with a
  read-floor (the scan must have seen files) and a presence half (the definition
  file must still carry it) so it cannot pass vacuously.
- **Rule 15** — this *is* the no-silent-failure fix: the false claim becomes an
  explicit line instead of an absence.

## Verification

- `go build ./...` → OK (`CGO_ENABLED=0 go build ./...`).
- `go test ./internal/tools -run 'TestAttachmentClaim|AttachIndex|Attachment'` →
  `ok github.com/nick/gopi/internal/tools 34.8s`;
  `go test ./cmd/agent -run Attach` → `ok 3.1s` (the byte-identity guard).
- **Red-proof**: with both wirings disabled (`if false &&`), the three positive
  tests FAIL and every negative test stays green; restored afterwards.
- **CLI diff, master vs branch, same ledger rows** — the sharpest form of both
  acceptance criteria at once:

```
=== R1 asserting-payload, no manifest: master vs new ===
11a12
> [Attachments] NONE — but the payload references an attachment ("screenshot attached"). No manifest, no replica files: the referenced evidence is NOT archived with this task.
=== R2 manifest row: master vs new ===  IDENTICAL
=== R3 clean prose: master vs new ===   IDENTICAL
```

`R2` carries a real attachment (`attachment1:` + `from: … sha256 …`): zero diff
against master's binary. `R3` mentions `internal/tools/tasks.go near line 2706`:
zero diff. Only the red-proof row gains a line.
