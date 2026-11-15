<!-- doc-class: normative -->
# devdoc: task payload cap — spill an oversized payload to an attachment

Branch: `feat/payload-spill` (worktree `~/workspace/gopi-payload-spill`)
Epic: *"task payload length must be caped"* — `task_payload_spill.go`,
`tasks.go` (TaskSubmit), `cli_tasks.go` (usage).

## The problem, precisely

A task's payload is injected **verbatim** into the dispatched child's first
user message (`cmd/agent/cli_tasks.go` `taskRunBriefing` → `[Briefing
payload]`). Two submit faces could therefore put an unbounded amount of text
into the child's context:

1. `--payload @file` — capped, but only *hard*: `config.maxFileRefBytes`
   (8 MiB) makes the submit **fail**. Failure is not a graceful cap.
2. `--payload "<inline text>"` and the **positional** form
   (`gopi task submit "$(cat huge.txt)"`) — **no cap at all**. The ledger
   measured 15 KB payloads in the wild; nothing bounded the next one.

And the briefing composes *both* columns, so an oversized **summary** is the
same hazard as an oversized payload.

## The fix: spill, never truncate

In the engine's `TaskSubmit` (not the CLI — so *every* face is covered,
including the internal auto-submit paths whose payloads are assembled from
other tasks' text), a column over `taskPayloadInlineMaxBytes` (16 KiB):

- is written **verbatim** to `<session-dir>/payload/spilled-payload.txt`
  (the byte-exact original the pointer's sha/size describe), and
- copied through the existing attachment machinery to its **replica**
  `<session-dir>/attachments/spilled-payload.txt`, recorded as **manifest
  entry 1**, so the existing `[attachmentN:]` briefing index resolves it with
  no new plumbing, and
- the ledger column becomes a ONE-LINE pointer:
  `[Briefing payload omitted: <n> bytes exceed the <limit>-byte inline limit —
  the full payload is preserved verbatim as attachment1 at <abs replica>
  (sha256 <hex>). Read that file with the read tool …] (payload begins: "…")`

Design decisions worth knowing:

- **16 KiB, not 8 MiB.** The payload's cost is *context*, not storage: it
  lands in the child's prompt. 16 KiB ≈ 4k tokens — above every legitimate
  one-shot briefing on the ledger (max ≈ 15 KB), far below a pasted log.
  Consequence: the ledger's historical max payload *is* the boundary. The
  constant is documented as the ONE knob; there is deliberately **no env
  override** (a size contract the dispatcher's context budget depends on must
  not be silently retunable).
- **Both columns share the budget.** An oversized *summary* is preserved too:
  the positional form puts the whole request in the summary column. If a
  distinct payload exists, summary + payload are preserved in the **same**
  attachment (nothing is dropped) and the summary column keeps a clipped
  first line (`taskSummaryHeadline`, ≤400 B) as the ledger headline; a
  summary-only row keeps the pointer in the summary column and stays
  payload-less (preserving the engine's registration-row shape).
- **Manifest slot 1.** The spill is PREPENDED to the submitter's
  `--attachment` list, so user attachments keep their order and are indexed
  from 2 on; the pointer renders "attachment1" **from the constant**
  `taskPayloadSpillAttachmentIndex`, so pointer number, `[attachmentN:]`
  index and `task show`'s per-entry lines can never diverge.
- **Two copies, on purpose.** The synthetic source is the honest "original"
  (the manifest's `from:` line names a file that *is* the payload), and the
  replica is what the index resolves. Both are written before the INSERT —
  a pointer whose file does not exist must never be committed (Rule 15), and
  a claimed task never re-materializes its briefing.
- **A short replica name.** A mirrored replica carries the source's absolute
  path structure (`attachments/tmp/log.txt`). The synthetic source's path is
  *inside* the session dir, so mirroring it would bury the replica under a
  deep self-referential chain — the spill names its replica directly
  (`attachments/spilled-payload.txt`) via a new
  `taskAttachmentCopyTo(taskID, src, rel)` that shares the ONE copy
  implementation with the mirrored face.
- **Retrieval breadcrumb.** A ≤200-byte preview of the payload's first line
  stays in the pointer because the ledger scans *payload substrings*
  (branch-scan merge-backlog dedup, agency fence counting,
  `task list --grep`) — a pointer-only row would silently drop out of those
  reports. (Contrast with `--reason @file`, whose whole point is that the
  text *never* reaches a child; it has no equivalent.)
- **Reported, not silent.** The submit confirmation (CLI stdout AND the
  `task_submit` tool's return) gains a line naming the size, the limit and
  the replica.
- **Under the limit = byte-identical to before** — same payload column, no
  manifest, the v1 repo-only deliverable shape (the Phase-1 regression
  contract).

## Verification

- `go build ./...` OK; `go test ./internal/tools/ -run TestPayloadSpill` (8
  tests) and `go test ./cmd/agent/ -run TestTaskRunBriefing` green.
- The boundary is pinned *exactly* (limit inline, limit+1 spilled).
- Byte-exactness is asserted through `cmp`-style comparisons, including
  CRLF/tab/non-ASCII payloads.
- Real-CLI smoke: `--payload @<220 KB file>` → pointer in `task show`, the
  replica readable at the named path, contents equal to the source (the
  `cmp` "EOF after byte N" is the input file's trailing newline, which the
  engine TrimSpaces — expected).

## Not done / deferred

- A TTL/GC for the `payload/` source directory (it lives in the session
  container, whose lifecycle is the finish-cleanup sweep's business).
- Cross-node sync of a spilled row: the replica is node-local, and a peer's
  import inserts `deliverable` via `deliverableForwardable`, which currently
  only authorizes repo-only(v1)/known-version blobs — worth a follow-up, but
  out of this epic's scope (single-node deployment is unaffected).
- `--reason` text (reopen) is already never embedded in a briefing.
