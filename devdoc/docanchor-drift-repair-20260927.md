<!-- doc-class: normative -->
# Task Report: doc-anchor gate repair — 40 new anchors re-keyed, 10 stale permits deleted

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

## Completed

`TestDocAnchorsResolveInNormativeDocs` (`internal/agent/docanchor_test.go`) was RED
at master (`c183aab6`) with **40 new** drifted anchors and **10 stale** baseline
permits. Both counts are now **0**, with no gate code, no regex and no
`docAnchorMinChecked` floor touched.

1. **Every one of the 40 new anchors was re-keyed to its symbol's CURRENT line** —
   by looking the symbol up in today's file, never by arithmetic shift. The
   emitters disagreed row by row (some claims had moved 1500+ lines, some symbols
   had been deleted outright), so a "shift by delta" pass would have produced 40
   confidently wrong anchors.
2. **Anchors whose mechanism no longer exists were moved with their claim, not
   faked onto a live line**: e.g. `fd-finalizer-leak` cites
   `cmd/agent/merger_takeover_lock.go`, DELETED (the doc's own STATUS banner says
   so) — the anchor now says that instead of pointing at a stranger's line.
3. **The 10 stale permits were deleted, not re-pointed.** Each excused a row that
   master has since FIXED (the line resolves again), so the drift they excused no
   longer exists. Serving them a new key was considered and rejected: a permit
   excuses drift, and there was none left to excuse. The manifest/docs were edited
   ONLY where the row still stood on a blank line.
4. **The 5 sites the deleted permits exposed were repaired too**, not left to the
   next run: `daemon-e2e-leak` (×2), `stylegate-errorf-verb-green` (the `%w` site
   moved into signalrec's destination helper), `04-sync-and-nodes` (the two
   whitelist declarations, `:616`/`:633` → `:322`/`:344`), `worker-once-vs-task-run-next`
   (`workerPeakWait` was DELETED with the peak guard by `bb0c4c1a` — the row's claim
   is now keyed to the `idleStreak` wait that replaced it), and
   `docanchor-gate-bare-lineno` (its before/after table's "after" cell now reads
   `runEngine`'s current line, `:3296`, instead of a 2026-09 line).
5. Record added to `internal/agent/docanchor-baseline.tsv`: every remaining drift
   entry predates this branch, proven by the base-worktree emitter run (below).

## Not Done (honest)

- **Only anchors the gate flags (blank line / past EOF) were repaired.** Line
  numbers inside the same rows that still land on a NON-blank line but no longer on
  the named symbol (e.g. `tasks-auto-register-feasibility-ds.md` row 159 cites
  `loop.go:1965` for `maybeEnforceCompletionStore`, which now lives at `:4500`) are
  **not** audited — that is a strictly larger set than the 2625 checked anchors and
  it is not what made the gate red.
- The **224 permitted** entries were not re-verified as a whole; only those the
  deleted permits touched were examined.
- `workspace/tmp/*` scratch (emitter dumps, apply scripts) was left OUT of the
  worktree on purpose (Rule 30) — the throwaway emitter test
  (`internal/agent/zzz_docanchor_emit_test.go`) was deleted, not committed.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK (exit 0)
- Tests:  branch-scoped, on the committed tree —
  `go test -count=1 -timeout 25m ./internal/agent/ ./internal/docclass/`
  → `internal/docclass` **ok**; `internal/agent` **FAIL**, with exactly one
  `--- FAIL` line, `TestTaskPromptCarriesDirectiveAtHead`
  (`output_language_prompt_test.go:127: the directive must PREPEND the prompt, never
  replace it`) — and that same test fails the same way at the merge-base
  (`cd /home/user/workspace/tmp/docanchor-base-01a0de15 && go test ./internal/agent/
  -run TestTaskPromptCarriesDirectiveAtHead -count=1` → FAIL, `c183aab6`), so it is
  master's red, not this branch's. Every doc-anchor test in the package passes
  (`TestDocAnchorsResolveInNormativeDocs`, `TestDocAnchorBareLineRules`,
  `TestDocAnchorScopeComesFromTheManifest`).
- Ran:    the gate verb itself, before and after, plus the emitter on the base worktree:
  `cd <worktree> && go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1`
- Result: gate summary line, before → after:

```
before: doc anchors verified: 2627 across 552 normative docs (… 234 pre-existing drift entries permitted, 40 new, 10 stale permits)
after : doc anchors verified: 2625 across 552 normative docs (2171 explicit + 454 bare continuations; 460 bare out of row/table/heading scope; 224 pre-existing drift entries permitted, 0 new, 0 stale permits)
```

- Commit: `259619e2` fix(docanchors): re-key the 40 drifted anchors and delete the
  10 stale permits (docs + `testdata/docanchor-baseline.tsv` only; this report rides
  the same branch)

## Evidence (self-filed)

- RED at master, on this branch's merge-base: `go test ./internal/agent/ -run
  TestDocAnchorsResolveInNormativeDocs -count=1` → `exit=1`, summary
  `234 pre-existing drift entries permitted, 40 new, 10 stale permits`.
- **Pre-existing, not introduced here**: the emitter run in the base worktree
  (`/home/user/workspace/tmp/docanchor-base-01a0de15`, created at merge-base
  `c183aab6`) produced an **identical key set** to the working tree — 40 new / 10
  stale / 248 permitted (dumps: `~/workspace/tmp/keys-base.txt`,
  `keys-mine.txt`). No drift belongs to this branch.
- GREEN after the repair: same command → `exit=0`, summary
  `224 pre-existing drift entries permitted, 0 new, 0 stale permits`, and
  `grep -c "docanchor_test.go:190:"` = 0 (no anchor complaint lines remain).
- No gate logic changed: `git diff --stat -- internal/agent/docanchor_test.go`
  prints nothing; the diff is docs + `testdata/docanchor-baseline.tsv` only.
- `git status --porcelain` in this worktree: prints nothing (Rule 30).
