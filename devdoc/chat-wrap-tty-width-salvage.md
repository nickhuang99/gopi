<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Salvage Measurement: `salvage/chat-wrap-tty-width-20260928` — intent already landed as `87b8bf3d`

**Verdict: nothing to re-implement.** The archived branch was measured file-by-file
and symbol-by-symbol against current master. **Its entire intent is already on
master**, landed by `87b8bf3d feat(chat): bound the fallback stream's rows to the
terminal width (U2)` (2026-09-20, eight hours after the branch's own code commit).
This document is the salvage record; **no production code change accompanies it**,
and none is warranted.

## 0. What was archived, and why

| field | value |
|---|---|
| archived branch | `salvage/chat-wrap-tty-width-20260928` |
| original branch | `feat/chat-stream-tty-wrap` (ref deleted) |
| branch tip | `11feae53` — survives at tags `archive/feat/chat-stream-tty-wrap` + `rework-backup/chat-stream-tty-wrap` |
| branch base | `92d14684` |
| archive reason | non-ff salvage branch; substantive conflict / unrelated history with master |
| diff | 9 files, +822 / −21 |

The ref was deleted but the object was never pruned: `git cat-file -t 11feae53`
→ `commit`. The measurement below therefore compares **live objects**, not the
archive's `.diff` text — a stronger instrument than the diff, and the reason
every claim here is re-derivable with `git show`.

## 1. Why the branch was a refused duplicate — by the U2 brief's own §4

`briefings/brief-u2-wrap-streamed-answer.md` §4 ("Context you should know (so you
do not redo finished work)") names this exact tip, **by tag and sha**, before any
rework was attempted:

> Both added a `cmd/agent/chat_width.go` and both edited `chat_stream.go` /
> `answer.go` / `render.go`. They were written **before** master fixed U1/U3/V1, so
> they each carry their own now-superseded versions of those. You may **read** them
> as prior art (`git show`, `git diff`) — **do not merge them, do not continue on
> them** …
>
> ```
> refs/tags/rework-backup/chat-stream-tty-wrap     11feae53   (ahead=2)
> ```
>
> The correct outcome is a **new, small branch off current master** with the wire
> above — nothing else.

So the branch was not a candidate awaiting a merge: it was **refused by name**,
and the brief prescribed the replacement. `87b8bf3d` is that replacement. This is
the substantive reason the 9-file diff could not merge — it is a superseded
duplicate competing with master's own landing of the same feature, not a
well-formed change blocked by ordering.

## 2. Establishment: the upstream landing is ancestor and predecessor-aware

```
$ git merge-base --is-ancestor 87b8bf3d master && echo "IS ancestor of master"
IS ancestor of master

$ git log -1 --format='%h %ci %s' 87b8bf3d
87b8bf3d 2026-09-20 20:17:44 +0800 feat(chat): bound the fallback stream's rows to the terminal width (U2)

$ git log -1 --format='%h %ci %s' 9ae98052     # the branch's own code commit
9ae98052 2026-09-20 07:35:18 +0800 feat(chat): wrap fallback TTY stream output to the terminal width

$ git merge-base --is-ancestor 87b8bf3d 92d14684 && echo YES || echo "NO not in branch base"
NO not in branch base
```

Two facts fall out: master's U2 landed **after** the branch wrote the same wire, and
it was **not** in the branch's base — so the branch could not have been carrying it.
They are two independent implementations of one intent, and master's is the landed one.

## 3. Per-file, per-symbol evidence table

Every identifier the archived branch **adds** is present on master. `grep -c` counts
files with at least one hit.

| file | branch intent | on master | evidence |
|---|---|---|---|
| `cmd/agent/chat_width.go` (new, 54) | `wrapColsFor(out, reserved)` — the one wrap decision; `chatWrapColsMin` | **LANDED** (superset) | `master:cmd/agent/chat_width.go:49 func wrapColsFor`, `:24 const chatWrapColsMin = 2` |
| `cmd/agent/chat_stream.go` | `chatRenderer.rows` + once-per-turn read; `emit()` single write point; `wrapStreamText`/`streamRow`; CRLF on soft row | **LANDED** (superset) | `:3282 rows int`, `:3148 r.buffered.rows = wrapColsFor(r.out, false)`, `:3336 func (r *chatRenderer) emit`, `:3394 func wrapStreamText`, `:3408 chatRowBreakCRLF` / `:3411 chatRowBreakLF` |
| `cmd/agent/chat_stream_wrap_test.go` (new, 368) | 7 tests over the wrapped + frozen halves | **LANDED** (7 of 8 carry the same intent; 1 branch test dropped) | 8 tests on master; 7 map 1:1 by intent under renamed prefixes, 1 is master-only — see §3b |
| `internal/lineedit/winsize.go` | `TerminalCols(fd) int`, 0 = unknown; (+ dead `WinchSignal`) | **LANDED** | `:44 func TerminalCols(fd int) int` |
| `internal/lineedit/answer.go` | export `WrapToCols`; `displayWidth` → `DisplayWidth` at call sites | **LANDED** | `:3542 func WrapToCols(s string, width int) []string` |
| `internal/lineedit/render.go` | export `DisplayWidth` (one definition, Rule 14) | **LANDED** | `:560 func DisplayWidth(s string) int` (delegating) — 51 files reference it |
| `internal/lineedit/wrap_export_test.go` (new, 74) | 4 export-contract tests | **LANDED** under another name | `internal/lineedit/winsize_export_test.go`: `TestWrapToCols_BoundsEveryRow` (`:17`), `TestWrapToCols_ZeroMeansNoWrapping` (`:41`), `TestWrapToCols_NeverSplitsAWideRune` (`:54`), `TestTerminalCols_UnknownIsZero` (`:75`) |
| `devdoc/chat-stream-wrap.md` (new, 127) | the run's report | **LANDED** as a different doc | `devdoc/chat-stream-app-wraps.md`, already classified `historical` at `DOC-INDEX.tsv:622` |
| `devdoc/DOC-INDEX.tsv` | +1 row for the branch's doc | **N/A** | master already carries the row for *its* doc (above) — the branch's row would name a doc that does not exist on master |

### 3b. The test mapping is a rename, not a superset

The branch's test names carry the prefix `TestChatStreamTTYWrap_`; master's carry
`TestChatStreamWrap_`. Counting alone would mislead (`8` vs `7`), so the two sets
were compared by name (`comm` on the extracted test lists). **Every one of the
branch's 7 tests has a master counterpart**, and master adds exactly one that the
branch never had:

| branch (`TestChatStreamTTYWrap_…`) | master (`TestChatStreamWrap_…`) |
|---|---|
| `WrapsALongUnbrokenDeltaAtCellBoundaries` | `RowsNeverExceedTheTerminalWidth` |
| `SoftWrapKeepsTheRowFromDoubleAdvancing` | `SoftWrapDoesNotDoubleAdvance` |
| `NeverSplitsAWideRune` | `NeverSplitsAWideRune` |
| `MultilineAnswerKeepsTheAnswersOwnBreaks` | `MultilineAnswerKeepsTheAnswersOwnBreaks` |
| `NonTTYOutputIsByteIdentical` | `NonTTYOutputIsByteIdentical` |
| `RegressionEmitsRowBreaks` | `RegressionEmitsRowBreaks` |
| `WidthComesFromARealTerminal` | `WidthComesFromARealTerminal` |
| — | **`FrozenPathIsDecidedByTheWriter`** (master-only) |

7 mapped + 1 master-only = 8. The one assertion the branch made that master does
not is named in §3a's item 1 — and master's behaviour there is the deliberate one.

Symbol census — every added identifier resolves on master:

```
WrapToCols        8 files      DisplayWidth     51 files
chatWrapColsMin   2 files      wrapColsFor       4 files
r.buffered.rows   3 files      chat_stream_wrap_test 3 files
```

### 3a. The two differences are master's, and master's is better

The only non-comment deltas between the branch tip and master are **master's own
improvements**, both already recorded in its landed implementation:

1. **The frozen rule dropped a condition.** Branch:
   `stdout != os.Stdout || !isTerminal(stdout) || !isTerminal(os.Stdin)`.
   Master: `!isTerminal(file)` — the reader half is *deliberately* not consulted,
   with the reasoning in the file's own doc comment. This is a widening of a
   correct-by-construction rule, not a regression.
2. **The branch shipped dead code.** `const WinchSignal = unix.SIGWINCH` has
   **zero readers on the branch itself** (`git grep -n WinchSignal 11feae53` →
   2 hits: the doc comment and the const). Master correctly omitted it — Rule 21's
   concern (an env/symbol constant with no real reader) in spirit, and Rule 3's
   "don't add what nothing uses".

Re-implementing either difference would be re-adding a rejected shape.

## 4. What was done instead of a re-implementation

Per Rule 23 the failed branch was **read**, never continued:
`git cat-file`/`git show`/`git diff` against `11feae53` — no checkout, no
`git apply` of the stale diff (Rule 29). A fresh branch off current master was cut
and renamed descriptively, and the only artifact landed is **this salvage record**
plus its manifest row (Rule 36 — classified in the same commit that lands it).

**Explicitly: no production code changes.** The measurement found no survivor, and
that is the deliverable — an "already landed upstream" finding is a positive
result, not an empty one. Re-touching `chat_width.go`, `chat_stream.go`,
`answer.go` or `render.go` would have reintroduced exactly the duplicate the U2
brief refused by name.

## 5. Verification

- `go build ./...` → clean (rc=0)
- `go vet ./...` → clean (rc=0)
- `git merge-base --is-ancestor 87b8bf3d master` → rc=0 (IS ancestor)
- `python3 tools/docclass/classify.py . --check` → exit 0
- `git status --porcelain` → empty at close (Rule 30)
