<!-- doc-class: historical -->
# One run, two session directories — the shape gate's missing operand

**Status:** landed (branch `task/01a14596573f7b59b8cec2645a2c80f6`, 2026-10-17).
**Class:** historical (this run's record; the STANDING statement of the rule lives
in `internal/sessionid`'s package comment and doc comments, which are the code
itself, plus the two earlier case studies
`devdoc/session-task-id-shape-gate.md` and
`devdoc/session-task-id-shape-gate-widened.md`).
**Row:** `01a14596-573f-7b59-b8ce-c2645a2c80f6` — the briefing's own claim was
"`$GOPI_TASK_REPORT_PATH` uses a different uuid spelling than the run's own
session dir".

## What was measured (this host, 2026-10-17)

`internal/sessionid` is the ONE place that decides whether an id may become a path
element; `taskSessionDir` / `TaskReportPath` both go through it. Its rule was a
blacklist of **uuid re-groupings**: an operand whose characters under the dashes
number **exactly 32** and which is not the canonical 8/13/18/23 spelling is
refused.

The measured operands that walked through it are of a DIFFERENT shape:

```
row id  (canonical, 32 hex)   01a1452c-928d-7e0f-9ef3-52c1482cfbc3
the sibling container         01a1452c-928d-7e0f9-9ef3-52c1482cfbc3   33 hex, 37 chars
second sample, same shape     01a14212e-e0d5-7aa5-8db3-0eca2b4e0969   33 hex, 37 chars
                               (row 01a14212-e0d5-7aa5-8db3-0eca2b4e0969)
```

Both are a uuid's 16 bytes with **one hexadecimal character inserted**; in both
samples the inserted character is the FIRST character of the group that follows,
i.e. the dash moved one place to the right. The ledger answers "no task with
task_id" for either sibling — **it names no row at all** — yet the seam joined it,
`MkdirAll`'d it, and the run then read its own evidence back under the canonical
name and reported artifacts that had "vanished from a live session directory".

The cost is on disk, twice:

| spelling | what is in it |
|---|---|
| `01a1452c-928d-7e0f9-9ef3-52c1482cfbc3` (33 hex) | `task-report.md`, `artifacts/`, `evidence/` — the run's whole deliverable |
| `01a1452c-928d-7e0f-9ef3-52c1482cfbc3` (canonical) | a second `evidence/` tree |
| `01a14212e-e0d5-…` (33 hex, 09:30) | `evidence/` only — its canonical twin was minted 57 minutes LATER (10:27) |

A census over `~/.gopi/sessions` (8872 entries, 8573 of them all-hex+dash names)
gives the two arms of the class their sizes: **35** directory names are a 32-hex
non-canonical grouping beside a canonical twin, and **2** are this 33-hex shape.
The class is real and recurring, not a one-off.

## What was NOT attributable, and is not guessed here

**Which producer mints the extra character is not derivable from the evidence of
this run**, and the report says so rather than naming a suspect. The shapes were
taken from the filesystem and the ledger; the transcript that would show the
insertion is the `jsonl` of the *other* run, and the only transcript this run
could read belongs to a different row. Recording a guess here would poison every
later reader (Rule 41's own lesson: a claim built on a comment rather than a
measurement was withdrawn in row `01a12b06-1875`). The residual question is
filed as its own row.

## The change

`internal/sessionid`:

- `overlongCandidateLen = undashedCandidateLen + 1` — the second arm of the class
  is named as its own constant, derived from the first, so the refusal and the
  length test cannot drift (Rule 1).
- `undashedHexRun` accepts EITHER length as a candidate (a mangled uuid is not
  one length).
- `isOverlongHexRun` is the refusal's second arm. It refuses **by shape**.
- `IsAdmissible` refuses both arms; `Refuse` phrases them separately — because the
  repairs differ: a **wrong grouping** is repaired by re-grouping (the operand
  determines the canonical row, and it IS printed), while the **extra digit** is
  NOT recoverable from the operand and therefore prints **no** "did you mean".

### Why the extra-digit arm cannot hand out a repair (the measured reason)

With 33 hex characters, "which single deletion leaves a uuid's 16 bytes" has no
unique answer: deleting the wrong one still yields **32 parseable hex
characters** — a uuid that names no row. Measured on the live sample, the scan
answered `1a1452c9-28d7-e0f9-9ef3-52c1482cfbc3`. That is a fabricated repair, and a
fabricated repair is more expensive than a bare refusal (Rule 15), so the first cut
of this change — which printed it — was corrected before landing. The refusal now
names the SHAPE and stops.

### The cost of the conservative arm, stated

An **anonymous** 33-hex token (not any row's bytes ± one digit) is refused too,
because by shape it is indistinguishable from the near miss above. That is the
deliberate trade: the rule stays a blacklist of what it can prove is a mangled
uuid, and the one shape it can no longer admit is named here rather than hidden.
`TestTaskSessionDirKeepsTheIdsItsBlacklistAdmits` pins BOTH halves — the ids the
tree really carries still resolve (`01a11548-tracked`, `t-1788146419`,
`capture-round-probe`, a canonical uuid), and the anonymous 33-hex operand is
refused.

The launcher seam (`cmd/agent/cli_tasks.go`) already reported the refusal on
stderr; nothing there needed a new branch — the session-dir resolution simply
fails, the child falls back to its own ledger id, and the operator gets the reason
instead of a silently populated second container. The `else` branch's message was
made honest about it (a REFUSED target is not a best-effort gap).

## Check

```bash
go test ./internal/sessionid/ ./internal/tools/ \
    -run 'OneRunOneSessionDirectory|RefusesAnExtraHexDigit|BlacklistAdmits|ShapeRule'
```
