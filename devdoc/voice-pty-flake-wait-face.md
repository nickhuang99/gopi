<!-- doc-class: historical -->
# Task Report: the two chat voice PTY flakers — a wait that reads a different face than its assertion

Task `01a12c12-9e84-70f1-ba78-e469893a396e` (Rule 41 row from `01a12b36`).

## Completed

- **Root cause found and fixed for both cases — and the shape is ONE shape.**
  Neither case is about voice behaviour: each one **waited on one face and then
  asserted about another**, so the assertion could be read in the gap between
  the two.
  - `TestChatVoice_TheRunOfFailuresEndsTheMode` waited for the **FAILED row**
    (a band row the READ LOOP paints on its own goroutine) and then read
    **`m.active()`** — a state bit that `fail()` flips *after* it has queued the
    row (`m.notice(...)` then `m.leave(...)` → `shutdown()` → `on = false`).
    Two instants, two goroutines: the queue is a hand-off, so the row can be on
    the grid while the state has not flipped yet. The wait now reads **both
    facts in one bound** (`waitForBound`), which is the same face the assertion
    below reads.
  - `TestChatVoiceLive_DraftsGrowInTheLineThenSettleOnTheFinishKey` (leg 4)
    waited for the settled sentence **anywhere on the grid**, but leg 5 counts
    it **on the editing row only**. The `[voice] transcript note`
    (`chatVoiceTranscriptNote`) carries the *same sentence* on another row, so
    a whole-grid match can be satisfied by the note while the editing row still
    shows the last DRAFT — exactly the failure artifact (task `01a12b36`):
    `you> 温 度 是`, counted 0. The wait now reads the **editing row** and
    requires the **draft segment to be cleared** (`h.in.editor.Draft() == ""`),
    which is the state the count is about (`internal/lineedit/draft.go`: the
    segment is cleared in the same pass that moves the text into the buffer).
- **Both failures reproduced on this host, deterministically, without load**
  (Rule 27 — no synthetic pressure was generated). The lever is corpus size, not
  the scheduler: under `-count=40` the in-process goroutine sweep (`internal/testutil`)
  flags 2 goroutines parked in `lineedit.(*Editor).waitBandWrite` reached from
  `voiceChatMode.leave → notice`, and `-cpu=1` made the state-bit gap recur.
  1 of 40 runs failed pre-fix; the post-fix runs are 20/20 and 40/40 green.
- Two test files changed, **no product code**: `cmd/agent/chat_voice_failure_run_test.go`,
  `cmd/agent/chat_voice_live_pty_test.go`.

## Not Done (honest)

- **The exact scheduler interleaving of the original full-package failure was
  not observed.** This host did not reproduce it in a real
  `go test ./cmd/agent/` run (the two tests passed there: see `## Verification`),
  so what is established is (a) the **code-level ordering** that makes the wait
  satisfiable before the assertion's fact — read from the source, quoted in the
  commits' comments — and (b) a **reproduced failure of the same family** under
  a longer corpus, from the pty's own in-process sweep. The claim is therefore
  "the wait can no longer return before the state it asserts", not "the original
  interleaving was replayed".
- **A second, separate defect was found and is NOT part of this fix.**
  `leave()` calls `notice()` from the loop's own goroutine; a shutdown that races
  the editor's read loop can park a band write for the full
  `bandWaitBudget` (4 s) and print
  `[gopi] editor: a band write was not drawn within 4s …`. The in-process sweep
  reports it as **parked repository goroutines: 2** (pre-existing; the reference
  full run from `01a12b36` ends with `parked repository goroutines: 0`). Filed
  as its own row, not fixed here: it is product-code threading, out of this
  task's scope (Rule 41 — the finder is not required to fix it).
- The two pre-existing failures in the full-package run
  (`TestChatCookedFallback_NonTTY`, `TestRunPathPlainPromptIsVerbatim`) are also
  NOT this task's: both are environmental (an operator session id / a real
  `gopi` run injecting the `[system] [t+0s]` framing), unchanged by this diff and
  unrelated to `cmd/agent`'s voice surface.

## Full-package confirmation (the original symptom, replayed through the suite entry point)

The reported symptom was *"these two tests fail under a full cmd/agent run, pass
3x in isolation"*. That exact shape was replayed through this repo's declared
test entry point — `scripts/test-budget.sh ./cmd/agent`, with 4 sibling
`go test ./cmd/agent` processes running on the same host — and **both cases
PASS in-package**:

```
PASS TestChatVoice_TheRunOfFailuresEndsTheMode Elapsed=2.21s
PASS TestChatVoiceLive_DraftsGrowInTheLineThenSettleOnTheFinishKey Elapsed=0.49s
```

Raw stream: `/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1791743684-3064908.json`
— transcribed into this run's `artifacts/full-package-evidence.txt`.

**Honest caveat, and it is not this task's**: that same full-package run has ONE
failure, `TestChatCookedFallback_NonTTY` (`cli_lineedit_test.go:50/53/60`: the
session carries `memory-prefetch.jsonl`, and 4 user events where 2 are wanted —
a real `gopi` run injected the `[system] [t+0s] …` framing into that fixture
session). Environmental, unchanged by this diff, unrelated to the voice
surface; not fixed here.

## Verification

- Build: **OK** — `go vet ./cmd/agent/` (compiles and vets the whole package the
  diff touches): no output.
- Tests: `go test ./cmd/agent/ -run 'TestChatVoice_TheRunOfFailuresEndsTheMode|TestChatVoiceLive_DraftsGrowInTheLineThenSettleOnTheFinishKey' -count=20 -v`
  → **20/20 PASS each** (`--- PASS` ×20 per case, package `ok 55.042s`).
  `-cpu=1 -count=40` → **40/40 PASS** (pre-fix: 1 FAIL in 40).
  A real full-package run of the same package (background job):
  `go test ./cmd/agent/ -count=1 -v` → the two tests **PASS**
  (`--- PASS: TestChatVoice_TheRunOfFailuresEndsTheMode (2.21s)`,
  `--- PASS: TestChatVoiceLive_DraftsGrowInTheLineThenSettleOnTheFinishKey (0.49s)`,
  raw: session `full-cmdagent-1.txt` lines 1965-1968 / 1982-1983).
- Ran: the surfaces this change touches ARE tests, so the invocation IS the run:
  ```
  go test ./cmd/agent/ -run 'TestChatVoice_TheRunOfFailuresEndsTheMode|TestChatVoiceLive_DraftsGrowInTheLineThenSettleOnTheFinishKey' -count=20 -v
  go test ./cmd/agent/ -run 'TestChatVoice_TheRunOfFailuresEndsTheMode|TestChatVoiceLive_DraftsGrowInTheLineThenSettleOnTheFinishKey' -cpu=1 -count=40 -v
  go test ./cmd/agent/ -count=1 -v          # the full package, background job
  ```
- Result: see `## Evidence (self-filed)` — exit codes and the observed lines.
- Commit: see the branch's tip commit (this report rides it).

## Evidence (self-filed)

- `-count=20` on the two cases: `ok github.com/nick/gopi/cmd/agent 55.042s`;
  per-case tallies `20 --- PASS: TestChatVoiceLive_…`, `20 --- PASS: TestChatVoice_TheRunOfFailuresEndsTheMode`.
- `-cpu=1 -count=40` (post-fix): `40 --- PASS` for each case, `PASS` then
  `FAIL github.com/nick/gopi/cmd/agent 112.079s` — the package-level FAIL is the
  **goroutine sweep** (`suite goroutine sweep: parked repository goroutines: 2`,
  stack quoted in the report's *Not Done*), not a test failure; both target cases
  passed 40/40. Evidence file: session `cpu1-40.txt` lines 215-257.
- **Pre-fix red, same invocation** (the reproduction): `-cpu=1 -count=40` →
  `--- FAIL: TestChatVoice_TheRunOfFailuresEndsTheMode (2.23s)` /
  `chat_voice_failure_run_test.go:154: the mode is still ON after the run of
  failures reached its limit`. Kept in `pre-fix-cpu1-40.txt`.
- Full package `go test ./cmd/agent/ -count=1 -v`: raw `full-cmdagent-1.txt`,
  lines 1967 and 1983 carry the two `--- PASS` lines above.
- `git status --porcelain` in the worktree: empty after the commit.
- **Scope of this branch, measured (not inferred).** `git rev-list --count master..HEAD`
  prints `135`, yet only **2** are this run's commits: `git diff --name-only HEAD~2..HEAD`
  lists exactly the four paths under `## Files`. The other 133 ride the base the worktree
  was created from (`2dfe480fe`, a merge commit that is **not** on master; three sibling
  worktrees carry the same base). `git merge-base master HEAD` = `a3a31da42` = master's own
  tip, so master is fully contained and no foreign commit is rewritten — but a reviewer
  reading `master..HEAD` as this run's diff would see 160 files where the change is 4.

## Files

- `cmd/agent/chat_voice_failure_run_test.go` — the wait now reads the row AND the state bit.
- `cmd/agent/chat_voice_live_pty_test.go` — the wait now reads the editing row, draft cleared.
- `devdoc/voice-pty-flake-wait-face.md` — this report.
