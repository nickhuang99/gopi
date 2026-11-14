<!-- doc-class: historical -->
# Task Report: HasSessionEnd tail walk — newline-bounded candidates (orphan misclassification fix)

Branch: `fix/hassessionend-moving-end` (worktree `~/workspace/gopi-sessionend-tail`, base master `0f66d17`)

## Problem

`HasSessionEnd` (internal/agent/session.go:593) decides whether a session JSONL
closed gracefully — no `session_end` tail ⇒ the process died mid-run, and
`DetectOrphanSessions` / `BackfillOrphanCrashDumps` then crash-dump the file.

The tail walk sliced every candidate line as `data[i+1 : len(data)-1]`: the
slice **END was pinned to the file end**, not to the newline that terminates
the line. So a half-written trailing line (SIGKILL mid-write — the exact case
the scan exists for) was glued onto every earlier candidate:

```
session_start\n session_end{...}\n  {"type":"assistant_text","text":"half-writ
                                   ^ newline at i                ^ file end = slice end
candidate at i = `session_end{...}\n{"type":"assistant_text","text":"half-writ`  → Unmarshal fails
```

`json.Unmarshal` therefore failed for *every* candidate, the
"keep scanning backwards a bounded way" branch was unreachable, and a
cleanly-ended run was reported as an orphan. Two secondary shapes were dead
too: a file whose last line has **no trailing newline** (`i == len(data)-1`
was skipped by the loop guard), and the single-line fallback indexed
`data[len(data)-1]` — a latent `data[-1]` panic had the `len(data) > 0` guard
ever been dropped/changed.

## Completed

- **Fix (internal/agent/session.go)** — `HasSessionEnd` rewritten as an
  explicit moving-end-pointer walk:
  - `lineEnd` starts at the file end (minus one trailing `\n`, which is a
    terminator, not content, so a file ending without a newline is handled);
    the candidate is `data[lineStart:lineEnd]` with
    `lineStart = LastIndexByte(data[:lineEnd], '\n') + 1` — **newline-bounded**,
    so a torn trailing line is dropped instead of splicing onto the previous
    line.
  - The bounded scan-back is preserved via a **named constant**
    `sessionEndScanBackMax = 4096` (Rule 1a: the old bare `4096` literal is
    gone; measured from the file end, so the reach is unchanged: a
    `session_end` further back than the bound is still not found).
  - Empty file → `false` before any indexing (`len(data) == 0` guard); the
    separate single-line fallback block is gone entirely — the walk subsumes
    it (a single line is just `data[0:lineEnd]`), which removes the
    `data[len(data)-1]` panic shape structurally rather than by guard.
  - Extracted `sessionEventTypeOf(line []byte) string` (one JSON decode path).
  - `bytes` added to the imports; `go vet` clean; `gofmt` clean.
- **Regression tests (internal/agent/sessionend_tail_test.go, new file)**
  - `TestHasSessionEndGarbageTailAfterSessionEnd` — the minimal deterministic
    repro: `session_start\n session_end\n <torn line>` ⇒ **true**.
  - `TestHasSessionEndCases` — 11-case truth table: clean ± trailing newline,
    torn tail, long (1400 B) torn tail, blank lines after `session_end`,
    single-line `session_end`/`session_start` without newline, orphan, orphan
    with torn tail, **empty file**, and `session_end` beyond the scan-back
    bound (still `false` — the bound is honoured, no full-file scan).
  - `TestHasSessionEndMissingFile` — missing path ⇒ false.
  - `TestDetectOrphanSessionsTornTail` — end-to-end guard on the user-visible
    symptom: a torn-tail clean run is **not** listed as an orphan while a real
    orphan still is. (Complements the pre-existing
    `TestDetectOrphanSessions`, which is retained and still passes.)

## Not Done

- The **pre-existing** red `TestHippocampusDocAnchorsResolve`
  (devdoc/agent-five-organs-ontology.md anchors) is left untouched — it fails
  identically on untouched master (evidence below), is unrelated to session
  logs, and fixing a docs anchor is out of this task's scope
  ("internal/agent/session.go product code only").
- No change to `DetectOrphanSessions`/`BackfillOrphanCrashDumps` — the fix is
  entirely in the predicate they call.

## Verification

Build:
```
$ CGO_ENABLED=0 go build -o /tmp/gopi-se-build ./cmd/agent/   → BUILD OK
$ go vet ./internal/agent/                                     → clean (no output)
$ gofmt -l internal/agent/session.go internal/agent/sessionend_tail_test.go → clean
```

Targeted suite (post-fix):
```
$ go test ./internal/agent/ -run 'TestHasSessionEnd|TestDetectOrphanSessions' -count=1
ok  github.com/nick/gopi/internal/agent  0.026s
  TestDetectOrphanSessions                     PASS   <- retained pre-existing test
  TestHasSessionEndGarbageTailAfterSessionEnd  PASS   <- new repro
  TestHasSessionEndCases (11 subtests)         PASS
  TestHasSessionEndMissingFile                 PASS
  TestDetectOrphanSessionsTornTail             PASS
```

Pre-fix proof (new tests kept, **only** `internal/agent/session.go` reverted to
master via `git stash push internal/agent/session.go`):
```
--- FAIL: TestHasSessionEndGarbageTailAfterSessionEnd
--- FAIL: TestHasSessionEndCases
    --- FAIL: .../garbage_tail_after_session_end_(repro)
    --- FAIL: .../long_garbage_tail_after_session_end
    --- FAIL: .../clean_without_trailing_newline
--- FAIL: TestDetectOrphanSessionsTornTail
```
⇒ the regression test genuinely fails on the pre-fix code and passes after.

Full package suite, fix vs untouched master:
```
worktree (fix):  --- FAIL: TestHippocampusDocAnchorsResolve   FAIL 41.6s
master (0f66d17):--- FAIL: TestHippocampusDocAnchorsResolve   FAIL 40.4s
```
Same single red on both ⇒ **the only failure is pre-existing and unchanged by
this work**; no new red introduced.

Commit: `HEAD` of branch `fix/hassessionend-moving-end`
(`git -C ~/work/GoPi log -1 --format=%h fix/hassessionend-moving-end` — the
exact short hash is quoted in this task's final message, since amending this
report changes it); touches `internal/agent/session.go`,
`internal/agent/sessionend_tail_test.go`,
`devdoc/sessionend-tail-walk-fix.md`.

## Deliverable location

- Branch: `fix/hassessionend-moving-end` (in the shared repo — the worktree
  shares master's object store, so the commit is already visible from
  `~/work/GoPi`; no push needed).
- Recovery: `git -C ~/work/GoPi show fix/hassessionend-moving-end:internal/agent/session.go`
  and `... show fix/hassessionend-moving-end:devdoc/sessionend-tail-walk-fix.md`.
- Worktree: `~/workspace/gopi-sessionend-tail` (not removed — merge/review is
  the PA's step).
