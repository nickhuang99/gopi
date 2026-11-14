<!-- doc-class: normative -->
# The chat bound's numbered notice: announced once per period, counted once per period

Task: `01a0f86c-cb50-7679-9ef9-26c72685eeaf`
Branch: `task/01a0f86ccb5076799ef926c72685eeaf`
Sibling (the branch that introduced the failing assertion, archived — do NOT continue
on it, Rule 23): `task/01a0f8491fea7b60baa1c22970a90274`

## The failure, verbatim

`make test-budget PKGS='./cmd/agent'`, whole package:

```
--- FAIL: TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator (0.69s)
chat_surface_bound_e2e_test.go:446: the console announced the self-driven turn 1/8 2 time(s),
want exactly 1 — the ACCEPTED count is the loop's own, read where the loop numbers it (bound 8)
```

The assertion counts, in the DURABLE painted stream, how many times each numbered
notice appears:

```go
stream := s.paint.streamANSIFree()
for n := 1; n <= bound; n++ {
    want := flattened(fmt.Sprintf(chatSelfDrivenTurnNotice, n, bound))
    if got := strings.Count(stream, want); got != 1 { t.Fatalf(...) }
}
```

## The mechanism, measured

**The notice's text is the same in both periods, and a complete run announces it
twice — by design.** `chatSelfDrivenTurnNotice` is
`"[gopi] self-driven turn %d/%d of this period (typed by the chat_surface tool) — this
loop stops at its bound"`, and the renewal notice (written by the OPERATOR's own
submitted line, `chatSelfDrivenRenewalNotice`) reopens the budget. Period 2 then numbers
**its** first turn `1 of the bound` again.

So the words `turn 1/8 of this period` are on the record TWICE in any run that completes
both periods. The count was taken at `chat_surface_bound_e2e_test.go:446`, i.e. **after
PHASE 1 and before PHASE 2's renewal** — so it could only pass while period 2 had not
started yet. It depends on a RACE between the suite's read and the mock's self-driven
loop, which is why it never reproduced in 60 isolated concurrent runs of the case and
why the whole-package concurrency (slower, pty-coupled neighbours) exposed it.

MEASURED (the artifact, kept at
`~/.gopi/sessions/01a0f86c-cb50-7679-9ef9-26c72685eeaf/attachments/wholepackage-red-before-fix.json`,
the `go test -json` stream of the red whole-package run; extract the case's `output`
events and read the `--- durable stream tail ---` the failure printed):

```
...[gopi]self-driventurn8/8ofthisperiod... [gopi]thisisthelastself-driventurnofthisperiod(bound8):...
───turn18·self-driven-17──...
...[gopi]self-driventurn1/8ofthisperiod(typedbythechat_surfacetool)—thisloopstopsatitsbound...
```

That is period 1's closing notice, then **the renewed period's own `1/8`, announced
after it**. The words matched twice because the console announced them twice, once per
period — the console was correct and the claim was not.

**A second, independent defect lives in the same assertion**, which is why the fix does
not merely move the read: **the painted byte stream is a log of WRITES, not of
announcements.** A notice is written to the band by `WriteNotice → Editor.WriteLines`,
and the editor also redraws the rows it tracks (`repaint`) as they change; a passing
run's raw stream therefore shows small partial-row re-issues (`you>you>you>`,
`…·ready`, the rule row, the mode word). Counting substrings there answers a question
about the paint — Rule 10.2's boundary: *a render claim is a pixel claim; the bytes are
a transport, the cells are the truth*. A duplicate announcement and a repaint are
indistinguishable from the bytes alone.

## The fix — two halves, one claim

The claim is unchanged and is still exact: **the loop announces each numbered turn
exactly once per period, and a genuine duplicate is a failure.** What changed is (a)
where the announcement is counted and (b) over what window.

1. **The count is read at the ANNOUNCEMENT, not at the paint.** `chatInput` now carries
   an optional observer (`announce io.Writer`, `cmd/agent/lineedit_wire.go`) that
   `WriteNotice` TEEs the notice into, in addition to the console write it already
   made. `chatInput.WriteNotice` is the ONE place a REPL notice is announced (the
   editor's repaints never touch it), so a count taken there counts events, and the
   console's own bytes are unchanged (asserted:
   `TestBoundNoticeLedgerCountsAnnouncementsNotRepaints` checks the notice arrives whole
   and identical with an observer installed). The e2e case installs a `countingWriter`
   ledger before the run starts.

2. **The count is scoped to the PERIOD, not to "so far".** `countingWriter` keeps the
   announcements IN ORDER, so the record can be sliced by an observable boundary:
   - **period 1** = every entry **up to and including the closing notice**
     (`chatSelfDrivenClosingNotice`, the console's own statement that the ceiling
     arrived). The case waits for that closing notice first — a bounded wait like every
     other in the case — so the window is COMPLETE when it is read, not "as much as has
     happened yet".
   - **period 2** = every entry **after the renewal notice** (PHASE 2 waits for the
     renewed period's first numbering; PHASE 3 re-reads all `1..bound` over that window
     once period 2's own closing notice is in).

   `countBefore(marker, want)` and `turnNoticesAfter(marker, n, bound)` are those two
   reads. Nothing is loosened: a number announced twice INSIDE its period still fails,
   an announcement past the ceiling (`bound+1`) is a strict zero, and a period whose
   numbering overshoots is red.

The fix is therefore a STRONGER claim than the old one: the old assertion could be
satisfied by a period that had not finished producing its numbers (it was a statement
about how far the run had got), while both new counts are statements about a period's
own arithmetic, read over that period's complete record.

## Round 2 — my own fix carried the same defect, and the whole-package run caught it

The first version of the fix was reviewed by the same instrument it introduced, in the
whole-package run this task exists to make green:

```
chat_surface_bound_e2e_test.go:651: the console announced the closing turn's bound 2 time(s)
in period 1, want exactly 1 — the notice that says the ceiling arrived must be announced
once and only once per period
```

with the ledger's own ordered dump printing a **perfectly correct run**:
`1/8 … 8/8, closing, renewal, 1/8 … 8/8, closing`, each numbered turn once. That check was
`entriesContaining(closing) != 1` — a **whole-run** count of the closing notice, which
BOTH periods announce, taken while the loop was still running. It passed only while
period 2 had not closed yet: the very race this task removed from the old count, left in
one call site. It is the most useful thing this run measured: **fixing an instrument does
not fix its call sites**, and only the concurrent whole-package run exposed it (5×
`-count=5` isolated repetitions of the case stayed green with that line in place).

The corrected shape makes the window a **POSITION PAIR**, which removes the whole-run read
class rather than one instance of it:

- `entriesContaining(marker) []int` — the positions of a marker, not a tally.
- `countBetween(from, to, want)` (half-open) and `countInWindow(from, to, want)` (inclusive
  end, for a period whose record legitimately ends ON its own closing notice).
- **period 1** = `[0, renewal)` — the renewal is both the end of period 1 and the start of
  period 2, so it is the one boundary both windows share; waiting for it is waiting for
  period 1's record to be **settled**, which is what makes the count a statement about the
  period instead of about how far the run has got.
- **period 2** = `[renewal+1, itsOwnClosing]`, located by position (the first closing
  notice strictly after the renewal), with period 1's earlier identical notice explicitly
  excluded by name in the failure text.
- The PHASE 3 "the renewed period closes too" check is now **zero closings before its own
  closing notice** — the strict form, read over the same window.

## Round 3 — the run that judged the corrected shape found a literal that could never be satisfied

The position-pair rewrite was put in front of the same instrument
(`-count=5 -run 'TestChatSurfaceBound|TestBoundNotice'`), and it came back **5/5 RED** — and
the red was the run doing its job one level further in than the code:

```
chat_surface_bound_e2e_test.go:901: the renewed period announced the closing turn's bound
0 time(s) before its own closing notice, want exactly 0 — the close is the period's LAST
announcement, and a second close before it would be a ceiling that arrived twice
```

`0` is the CORRECT reading — that is what the assertion asks for — and the run still failed,
because the condition beside the message read `got != 1`. The window is
`[renewal+1, period2Closing)`, **half-open**, so the entry that closes period 2 is excluded
from it by construction: the count can only ever be `0` (right) or `≥1` (a ceiling that
arrived twice), never `1`. The `!= 1` was copied from the per-number check's shape, where
the window's inclusive end (`countInWindow`) makes `1` reachable — a literal carried across
a window whose end changed. **The message was right and the condition was wrong, in the same
statement**; only running it says which of the two the test believes.

That is why this section exists in the report rather than a silent fix: a green suite would
never have shown it, the assertion as written was RED BY CONSTRUCTION, and the failure text
printed the correct value while failing on it — the shape a reader is most likely to
misread as a product defect. Corrected to `got != 0` with the half-open distinction stated
in place, the same command is **5/5 GREEN** (`bound-count5-r4.log`: five
`--- PASS: TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator`,
`EXIT=0`).

## Files

- `cmd/agent/lineedit_wire.go` — the announcement seam (`chatInput.announce`, teed by
  `WriteNotice`, additive: byte-identical console output).
- `cmd/agent/chat_surface_bound_e2e_test.go` — `countingWriter` +
  `announcementLedger` + `sawNoticeOnce` + `streamTail`; PHASE 1's per-number count,
  PHASE 2's post-renewal read, PHASE 3's period-2 window count.
- `cmd/agent/chat_surface_help_menu_e2e_test.go` — `helpMenuSession.ledger` (nil when a
  case installs no observer).
- `cmd/agent/chat_surface_bound_notice_count_test.go` — NEW: the deterministic
  regression guard (Rule 10.5), no pty/no mock/no timing.

## Verification

- Build: `go build ./...` — OK (exit 0).
- Vet: `go vet ./cmd/agent/` — OK (exit 0).
- Tests (regression guard, deterministic):
  `go test ./cmd/agent/ -count=1 -run 'TestBoundNotice' -v`
  → `--- PASS: TestBoundNoticeCountIsReadPerPeriodNotPerPaint (0.00s)`,
  `--- PASS: TestBoundNoticeLedgerCountsAnnouncementsNotRepaints (0.00s)`,
  `ok github.com/nick/gopi/cmd/agent 0.624s`.
- Tests (the acceptance case):
  `go test ./cmd/agent/ -count=1 -run 'TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator' -v`
  → PASS, with the case's own printed accounting:
  `PHASE 1: the model asked for a self-driven turn 9 times; the tool refused at the
  bound and the console announced "[gopi] this is the last self-driven turn of this
  period (bound 8): the loop closes when it ends."`;
  `PHASE 2: the operator's line "carry on" renewed the budget …and exactly one
  self-driven turn was numbered 1 of 8 after it…`;
  `BOUND: 36 model round(s); the self-driven loop stopped at the absolute ceiling TWICE
  (once before the operator's line, once after it)…`.
- Ran: `CGO_ENABLED=0 go test ./cmd/agent/ -count=5 -run 'TestChatSurfaceBound|TestBoundNotice' -v`
  → 5/5 PASS (`bound-count5-r4.log`, EXIT=0); 30 runs across r3+r4.
- Ran: `make test-budget PKGS='./cmd/agent'` (the whole package, the surface that surfaced the flake)
  → **exit 1, and NOT on the count.** The residual failure is
  `chat_surface_bound_e2e_test.go:763: the loop accepted 16 self-driven turn(s) and refused 2, want 8
  accepted and exactly 1 refused (bound 8)` in 0.33 s — the MOCK's run-scoped `turns`/`refusals`
  counters, a different instrument from the ledger this change writes, reached only in the package
  environment (every isolated run takes ~20.6 s and stops at 9 asks). It is reported, not hidden, and
  filed as its own bug task; it is not a regression from this change (pre-fix the case died at line 651
  and never reached line 763).
## Acceptance run — the observed result

```
--- failures (assertion/build; the budget did NOT trip) ---
  --- FAIL: TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator (0.33s)
  --- FAIL: TestTaskShowRendersTheSameDigestForAllThreeAddresses (0.08s)
stamp: 2026-10-02T02:05:03+08:00 host=peer1 pkg=./cmd/agent budget=25m elapsed=585s exit=1 outcome=fail
```

The count assertion this change rewrites (`the console announced the closing turn's bound N time(s) …`)
appears in **no** run after the correction. The 0.33 s duration is the tell that line 763 is a different
animal: 18 asks = two periods, driven back to back, read off counters that are scoped to the whole run —
the same class of mistake, in the mock's instrument rather than in the ledger, which is why it survives
this fix and needs its own.
## RED PROOF — the seam is what the count rests on (revert, run, restore)

The regression guard must fail for exactly one reason, and the seam it reads must be the
thing that fails when it is removed. Measured, in this worktree, by deleting the tee
block from `chatInput.WriteNotice` (the `if in.announce != nil { … io.WriteString(in.announce,
text) … }` arm), running the guard, and restoring the file byte-identically
(`git diff --stat cmd/agent/lineedit_wire.go` → `30 ++++++` back in place after the run):

```
$ CGO_ENABLED=0 go test ./cmd/agent/ -count=1 -run 'TestBoundNotice'
--- FAIL: TestBoundNoticeLedgerCountsAnnouncementsNotRepaints (0.00s)
    chat_surface_bound_notice_count_test.go:121: after ONE announcement the ledger counted 0, want exactly 1
FAIL	github.com/nick/gopi/cmd/agent	3.460s
```

`announcements counted 0` — the ledger is silent because the announcement seam is gone;
the notice itself still reached the console (the editor path below the tee is untouched),
which is exactly the distinction the fix rests on. Restored, the same command is green.
A first attempt at the revert was REFUSED by the compiler —
`method chatInput.WriteNotice already declared` — because a naive splice dropped the
block's terminator along with its opening brace: the red proof has to be a real revert,
and the compiler said so rather than letting a broken tree be reported as a red run.

## `## Evidence (self-filed)`

- `go build ./...` → exit 0 (this worktree, after the fix).
- `go vet ./cmd/agent/` → exit 0.
- Red proof (seam removed): the failure line quoted above, verbatim.
- Red evidence BEFORE the fix: the whole-package run of the unmodified tree
  (`wholepackage-red-before-fix.json`, 10348 go-test-json events) with the exact
  failure line quoted above, and the pre-fix assertion's dependency on the race
  (it reads a run-scoped count before the renewal starts).

## Not Done (honest)

- The case still runs the mock to BOTH periods before asserting period 2's window, so
  PHASE 3's window count is taken after the run's own closing notice rather than at a
  mid-flight instant — deliberate, and the reason the count is race-free; a reader
  looking for "assert period 2 is half-spent" will not find it, because that claim
  would reintroduce exactly the race this task removed.
- `sawNoticeOnce`'s ambiguous arm (announcement written once, ledger zero) LOGS rather
  than fails: the alternative is an intermittent red that reports nothing. The arm is
  reachable only if an announcement reaches the screen by a path that is not
  `chatInput.WriteNotice` — if that ever happens the log line names it.
- Not run: the whole-TREE suite (Rule 27's merge/verify path is smoke, branch-scoped;
  the whole package is the branch-scoped acceptance here).
