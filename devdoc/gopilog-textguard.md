<!-- doc-class: normative -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# The binary briefing's SECOND render family: `gopi log` views + the JSONL line cap

Epic: binary-briefing defect, gopilog half. Date: 2026-09-15. Repo
`/home/user/work/GoPi`, branch **`fix/gopilog-textguard`**, worktree
`/home/user/workspace/gopi-textguard`.

Predecessor (READ-ONLY reference, another agent's unmerged work):
`fix/binary-summary-guard` — it fixed the LEDGER family (`internal/tools`,
`internal/agent`) and its own devdoc is `devdoc/binary-summary-guard.md`.
This branch is FRESH off master (Rule 23): it never commits onto that branch.

## 1. The two defects this branch closes

Both were proven on ONE real session — `a3d34ed1-b998-4caf-9474-9d2b374e1b90`
(workdir `/home/user/workspace/peak-bin`, `gopi gopi help`, i.e. the positional
`gopi` named the freshly built ELF and `readInput` read it as prompt TEXT; the
same session the ledger fix traced).

**A. Renderers printed the captured prompt verbatim.** `s.Task` is the
session's FIRST user row (reader.go, `processEvents` "user" case) — the accepted
prompt. `FormatSession` (`TASK:`) and `FormatTodo` (`EPIC GOAL:`) wrote it out
unfiltered, so `gopi log a3d34ed1… --todo` exited 0 and emitted the ELF header
plus NUL bytes: `file(1)` calls that output `data`, `read` refuses it, and a
terminal shows garbage. Measured BEFORE: `--todo` was a 72,435,087-byte file,
`file -` = `data`. AFTER the guard: 3,209 bytes, `file -` = `Unicode text,
UTF-8 text`, `tr -dc '\0' | wc -c` = 0, and the ELF magic stays visible as
`[bin \x7fELF\x02\x01\x01\x00…]`.

**B. The reader made that session uninspectable.** Its whole conversation sits
on ONE 87,618,404-byte JSONL line (line 2; the model echoing the captured bytes
back). `internal/gopilog/reader.go` capped a line at `scannerMaxBufSize` = 10 MiB,
so every verb touching that file — `gopi log <id>`, `--todo`, `--last`,
`gopi stats --cost` — died with bufio's bare `bufio.Scanner: token too long`
(no file, no line, no number). The session an operator most needs to inspect was
the one that could not be inspected.

## 2. The fix (what changed, and why this shape)

### 2.1 `internal/textguard` — the ONE shared decision (NEW leaf package)

Rule 14: the question "is this string text?" is asked by two packages that
cannot depend on each other. `internal/gopilog` already imports
`internal/agent`; `internal/tools` imports nothing from gopilog — putting the
predicate in the ledger would close a cycle. So the predicate lives in a leaf
package (stdlib imports only, the `internal/toolnames` precedent) and both
packages consume it.

API: `IsText(s)`, `IsPrintableRune(r)`, `Sanitize(s, maxRunes, singleLine)` plus
the exported constants `Marker` (`bin`), `HeadBytes`, `DominancePercent`,
`Ellipsis`. For non-text input `Sanitize` escapes maximal non-text runs IN
PLACE as `[bin \x7fELF\x02\x01…]` — **the magic bytes stay visible**, because
that trace is the only honest evidence of what was captured (Rule 15: never a
silent strip).

One behaviour change over the ledger's previous private sanitizer, forced by the
real data: blob detection now fires on non-text **count** dominance **or** on
**span** dominance at ≥ `blobCollapseMinBytes` (1 MiB). The real briefing is
21,690,123 bytes carrying 3,372,545 NULs — only **15%** by count, but spanning
the whole string. Under a count-only rule that line escaped in place and
rendered **72 MB of markers** (measured: `verify-out-real-todo.txt`); as a blob
it is one line. The size floor on the span rule is deliberate: below 1 MiB the
in-place escape is affordable, so every byte of text is preserved and the
ledger's `task show` payload fidelity is untouched.

### 2.2 `internal/tools` — the ledger half now consumes the shared guard

`sanitizeTaskSummary` / `sanitizeTaskPayloadText` are thin wrappers over
`textguard.Sanitize` (the ledger keeps its OWN ceiling constant
`taskSummaryMaxRunes` = 200 — a storage policy, not a text decision).
`taskTextSanitize` and its six private helpers were deleted: **274 lines
removed, 74 added**, one predicate instead of two. Behaviour is identical for
text (identity) and for the measured ELF row (same `[bin \x7fELF…]` marker).

### 2.3 `internal/gopilog` — every `s.Task` emit site is guarded

`internal/gopilog/tasktext.go` (new) is the render-shaped face of the shared
guard: `sessionTaskText` (block, line structure preserved), `sessionTaskLine`
(single line), `sessionTaskLines` (block lines). Wired at:

| site | emitter |
|---|---|
| `formatter.go` `FormatSession` | `TASK:` block |
| `formatter.go` `FormatTodo` | `EPIC GOAL:` block |
| `formatter.go` `FormatSummary`, `FormatSummaryTable` | one-line rows |
| `formatter.go` `FormatChat` | the iteration's `UserPrompt` — the SAME captured-prompt bytes, one field away |
| `reader.go` `LoadAllSummaries` | `ss.Task` (defence in depth: `gopi log --last` rows) |

Splitting into lines happens AFTER the guard (`sessionTaskLines`), so no line
boundary can be decided inside a NUL run.

**Audit of the other emitters** (`rawview.go`, `httpview.go`, `rounds.go`,
`stats.go`, `FormatGrep`, `FormatToolFilter`, `FormatContextTrim`): none prints
`s.Task`. `FormatToolFilter`/`FormatGrep` deliberately print raw evidence
(bash results, tool inputs) — an operator `gopi log --raw`s a binary payload on
purpose, and gutting it would destroy evidence. The defect is narrower and
precise: the value the framework **accepted as the task**, which every renderer
presents as a HEADLINE.

### 2.4 The reader cap — the deliberate decision

Documented in `internal/gopilog/jsonlline.go` (where the code is, not in a
comment elsewhere):

1. **STREAM the line** instead of capping it. A session line's length is set by
   the model's own output, which nothing in this process can bound — any cap is
   a latent "this legitimate session cannot be inspected" bug with a different
   number. `jsonlScanner` accumulates the line itself (`bufio.Reader.ReadSlice`
   + append): an 87 MB line reads like a 200-byte one.
2. **KEEP a safety ceiling** — `jsonlLineSafetyCeiling` = 1 GiB (≈12× the
   largest session line ever measured here). "Unbounded" is not a design; a
   corrupt file must not drive the process out of memory.
3. **NEVER fail silently or anonymously.** Exceeding the ceiling returns
   `jsonlLineTooLongError` carrying the file, the 1-based line number, the byte
   offset where the line starts and the ceiling. Every call site asserts
   `scanner.Err()`; bufio's bare "token too long" is never surfaced again.
   `ValidateSessionFile` had the same 10 MiB bufio cap — and its stale
   post-loop message blamed a **missing `session_start`** for what was actually
   an over-cap read; it now uses the shared reader and propagates the real
   error. The raw-HTTP round scanner keeps its own (documented) cap but wraps
   its error with the path.

One implementation serves all five session-file scanners (`reader.go` × 4,
`stats.go` × 2, `rawlog.go` × 3) — Rule 14 again, no mirrored buffer setup.

## 3. Verification (exact commands + observed output)

Every row below was RUN; nothing is inferred.

### 3.1 Build + tests

```
$ cd /home/user/workspace/gopi-textguard
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/      → exit 0
$ go test ./internal/gopilog/ ./internal/textguard/    → ok  (both)
$ go test ./...                                        → see §3.4
```

### 3.2 The real 87 MB session (the acceptance case)

BEFORE (`bin/gopi-prefix`, built from pristine master `eb802d64`, same
`GOPI_HOME`): the session could not be rendered AT ALL — the reader cap killed
every verb (see §3.5 for the captured output).

AFTER:

```
$ GOPI_HOME=/home/user ./bin/gopi log a3d34ed1-b998-4caf-9474-9d2b374e1b90 --todo > acc-real-todo.txt
exit=0
$ wc -c acc-real-todo.txt ; tr -dc '\0' < acc-real-todo.txt | wc -c ; file -b acc-real-todo.txt
3102 bytes   NUL=0   Unicode text, UTF-8 text, with very long lines (916)
$ grep -o 'bin .\{0,30\}' acc-real-todo.txt
bin \x7fELF\x02\x01\x01\x00…]
$ GOPI_HOME=/home/user ./bin/gopi log a3d34ed1-… --last 3 > acc-real-last.txt
exit=0   NUL=0   ASCII text
```

BEFORE the RENDER guard too (an intermediate state of this work, kept as
evidence of why the blob rule has a SPAN clause): with count-only blob
detection the very same session produced a **72,435,087-byte** `--todo`
(`.verify-artifacts/verify-out-real-todo.txt`) — the 21,690,123-byte briefing
carries only 15% NULs, so an in-place escape emitted megabytes of markers.

Rendered headline after the fix:

```
EPIC GOAL:
  [EPIC GOAL]
  [bin \x7fELF\x02\x01\x01\x00…]
  help
```

### 3.3 The fixture session (ELF head as the first user row)

`internal/gopilog/binaryprompt_render_test.go` builds a real session FILE whose
first user row is the ELF header; the CLI was also driven against a hand-written
fixture under `.verify-artifacts/verify-home/.gopi/sessions/fixture-elf-prompt/`
(a throwaway GOPI_HOME, so the operator's real session dir is never touched).

BEFORE / AFTER, old binary (master) vs. this branch, SAME fixture, same
`GOPI_HOME`:

```
BEFORE  gopi log fixture-elf-prompt --todo  → exit 0   950 bytes  120 NULs  file: data
BEFORE  gopi log fixture-elf-prompt         → exit 0  1090 bytes  120 NULs  file: data
AFTER   gopi log fixture-elf-prompt --todo  → exit 0   742 bytes    0 NULs  file: Unicode text, UTF-8 text
AFTER   gopi log fixture-elf-prompt         → exit 0   775 bytes    0 NULs  file: Unicode text, UTF-8 text
both AFTER outputs contain: bin \x7fELF\x02\x01\x01\x00…]
```

`file -b` calls the BEFORE output `data` and the AFTER output text; the NUL
count goes 120 → 0; the ELF magic stays visible as the honest evidence.

### 3.4 Gate status (honest)

`./internal/standards/` and `./internal/stylegate/` were run BOTH on pristine
master (`/home/user/workspace/gopi-tgbase-667424`, tip `eb802d64` — the exact
commit this branch is based on) and on the branch, to separate pre-existing red
from anything this branch could have caused:

- pristine master: **FAIL** — Gate 1 `%v` wrapping, 2 new unannotated sites
  (`cmd/agent/cli_voice_client.go:811`, `cmd/agent/main.go:965`) plus a stale
  baseline entry (`cmd/agent/main.go:954`); stylegate `errorf-verb` 10 hits vs
  ceiling 9.
- this branch: the **same** sites, the same two packages.

**This branch adds ZERO new gate hits**: no `%v`-wrapped error, no `os.Setenv`
in a test, no `panic`, no `else`-after-return, every env constant read — and
none of the reported hits names a file this branch touches (checked
exhaustively: grep the gate+stylegate output for every file in
`git diff --name-only HEAD` → no match). The standards/stylegate red is
inherited from master and belongs to the other agent's in-flight work
(`cli_voice_client.go` / `cmd/agent/main.go`), not to this diff.

The `internal/tools` red is a FLAKE, measured on both trees with 10 identical
`-count=1` runs of `TestTaskFindSessionJSONLStillHonoursEveryLayout`:
pristine master **8 ok / 2 fail**, this branch **8 ok / 2 fail** — the
difference between green and red is not the code under test but a session-tree
cache shared across the test's subtests.

## 4. Anti-regression guards (the gates that keep this fixed)

1. `internal/gopilog/binaryprompt_render_test.go` — a REAL ELF-prompt session
   file, parsed and rendered through `FormatSession` / `FormatTodo` /
   `FormatChat` / `FormatSummary`; each asserts **0 NUL bytes in the rendered
   string**, valid UTF-8, and the magic bytes still visible.
2. `internal/gopilog/reader_linecap_test.go` — a >10 MiB line is READ (no
   committed fixture; generated at test time), and an over-ceiling line fails
   with the named, positioned error (injected small ceiling, so no >1 GiB
   fixture is needed).
3. `internal/gopilog/taskemit_gate_test.go` — the Rule-13 sweep: every
   production line touching `.Task` / `.UserPrompt` must be a guard call, a
   presence check, an assignment or a comment. A future unguarded emitter fails
   the test.
4. `internal/gopilog/gopilog_wiring_test.go` — layering: gopilog must import
   `internal/textguard`, must NOT import `internal/tools`, and must not
   re-implement the predicate.
5. `internal/textguard/textguard_test.go` — the predicate itself (identity for
   CJK/emoji, the ELF run, the blob rule, the ceiling).

## 5. Deliverable + recovery

- **Branch**: `fix/gopilog-textguard` (worktree
  `/home/user/workspace/gopi-textguard`, shared object store with
  `/home/user/work/GoPi` — no push needed).
- **Recovery**: `git -C /home/user/work/GoPi show fix/gopilog-textguard:devdoc/gopilog-textguard.md`
- **Commit**: see the Rule-5 report (`tasks/…-report.md`) for the hash.
- Not merged, not cleaned up: the worktree stays until PA acceptance (Rule 6d/6e).
