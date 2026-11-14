<!-- doc-class: historical -->
# Devdoc: `gopi raw` — why it was slow, why a session left no trace, and what changed

Task `01a0d51b-f72d-78d4-a89f-9da7c97b1bd8`. Operator report, in the briefing's
own words: *"a gopi session at least interacts with LLM for something… without
basic raw log to honestly record everything timely, we lost ability to
reconstruct the issue."* Three separate defects sat behind that one report, and
each was reproduced before it was fixed.

## The evidence as reported

```
nick@peerbox:~/work/GoPi$ gopi log fb29b78b-8b72-4da7-93d1-706b9696b5f6 --chat
  Iterations: 0     Prompt: 0 tokens     Completion: 0 tokens
nick@peerbox:~/work/GoPi$ gopi raw fb29b78b-8b72-4da7-93d1-706b9696b5f6
nick@peerbox:~/work/GoPi$          ← nothing, after a multi-second wait
```

The operator's question was exactly right on both counts: *"isn't it only a
fixed path file open and return?"* and *"how can a session have no trace at
all?"*

## Defect 1 — the slow read (`gopi raw` / `gopi log`, 3.2s)

**Symptom.** `gopi raw <id>` took ~3.2s on this host and printed nothing.

**Root cause.** `FindSession(dir, id)` (internal/gopilog/reader.go) resolved the
operand with `collectSessionFiles(dir)` — a full walk of the sessions tree plus
a head read of **every session record** — and only then compared ids. The walk
answers "which of these records is the one?"; a FULL UUID operand needs no such
question, because every layout **names** its file after the run's uuid.

Measured on the live tree (76,207 entries, 3,665 records), `strace -c` on the
pre-fix binary:

| syscall | count |
|---|---|
| `openat` | 173,589 |
| `getdents64` | 333,583 |
| `.jsonl` head reads | 7,332 |

~2.4s of raw syscall time for an answer that is one `stat`.

**Fix.** `exactSessionPath` composes the candidate names from the layouts the
shared `internal/sessionlayout` package defines, stats them, and confirms the
hit with the SAME identity rule the walk applied (`FileToID` plus the side-file
and bare-record exclusions) — so a shortcut never becomes a different answer.
`sessionsIDCandidates` lists the root's child directories (one level, no session
file read) because a task-run record is named for the RUN uuid, not the task.
`SessionsRecordingRawLog` is narrowed the same way: under the co-located layout
the log's own directory is the whole search, while the legacy flat layout (no
directory links a log to its session) keeps the walk.

**Result.** `gopi raw fb29b78b…`: 3.196s → 0.289s. A miss on a non-existent id:
2.266s → 0.505s.

**Guard.** `TestFindSession_ExactIdDoesNotReadEveryRecord` builds 2000 decoy
records and fails when the name path costs more than a tenth of the walk.
Red-proof: re-disabling the fast path fails it (`36.6ms` vs `44.5ms` walk).

## Defect 2 — the raw log that never had a first byte

**Symptom.** `/home/user/.gopi/sessions/fb29b78b-…/fb29b78b-….raw.log` is
**0 bytes**, while the session itself is intact (`session.jsonl`, 464 bytes).

**Root cause.** `newRawLoggerInDir` creates the file at session start, but the
first byte lands at the first HTTP round. A session that never reached a round
— an unreachable endpoint, an aborted setup, a crash before the first send —
left a 0-byte file. Reproduced end-to-end with the pre-fix binary against an
unreachable endpoint: `1 iteration`, `FAIL`, `0 tokens`, raw log **0 bytes**.

This is the operator's "how can a session have no trace at all?" — it did not.
It had a file with no content, which answers nothing: was raw logging on? did the
session start? which run owned this file?

**Fix.** `writeRawLogHeader` writes a self-describing line at the moment the
file is created — the earliest point in the session's life, which is where the
briefing says raw capture belongs:

```
# gopi raw HTTP log — created 2026-09-24T20:50:05Z pid=372492 session=7c042751-… log=7c042751-….raw.log
```

Two deliberate choices: it goes **straight to the file**, not through the bufio
writer (a buffered header would be lost with the very process it explains —
`Close` never runs on a crash or a kill); and it is NOT in the round vocabulary,
whose markers are matched by whole-line equality, so it can never be miscounted
as a round (`TestParseHTTPRounds_IgnoresCreationHeader` pins that — a header
counted as a round would shift every round number the operator reads).

**Result.** Same scenario, new binary: header written, log 158 bytes.

## Defect 3 — nothing explained

**Symptom.** An empty log printed **nothing** (indistinguishable from a broken
verb), and a session whose recorded log was collected by the scratch GC got
"no raw logs found in `<gopi dir>/raw`" — naming a directory that session never
used.

**Fix.** Both shapes now say what happened:

- 0-byte log → stderr names the file, says no HTTP byte was ever captured, and
  states this is not a read failure.
- recorded-but-missing log (`SessionRecordedRawLog` + `missingRawLogNotice`) →
  names the missing path AND the intact session record, so the operator knows
  the difference between "no log was ever recorded" and "the log is gone".

**Result** (sandbox session whose recorded log was removed):

```
Error: session aaaa1111-… recorded raw log …/sessions/gone-aaaa1111-….raw.log, but that file does not exist.
The session record is intact (…/sessions/aaaa1111-….jsonl) — only the raw HTTP log is gone, most often
collected by the scratch GC once the run was old.
```
vs. the pre-fix binary, which reported `cannot list raw logs: open …/.gopi/raw:
no such file or directory` — a message about a directory nobody asked about.

## Honest residual

- **26 sessions on this host have a recorded raw log that is MISSING**; every
  one is a test-model session (`GOPI_RAW_LOG=OFF` under the test harness), and
  no real session is among them. The message now tells the truth about them
  instead of blaming the wrong directory.
- The **~0.29s remainder** is the root's `ReadDir` plus process start — the
  task-run layout is reachable only by listing the root's children (no name can
  be composed from the session id). Not the per-record read that was the defect.
- `missingRawLogNotice` resolves only the session's own recorded path; it does
  not try to guess a replacement log elsewhere.

## Verification recipe (reusable)

- Speed: `strace -c -f` on `gopi raw <id>` before/after, counting `openat`.
- Empty log: run the binary against an unreachable `GOPI_BASE_URL`
  (`http://127.0.0.1:9/v1`) inside a sandbox and `stat -c%s` the raw log.
- Isolation: `SB=$(gopi sandbox create --label … | tail -1)`, then
  `GOPI_HOME=$SB` (note: the profile root is `$GOPI_HOME/.gopi`).
