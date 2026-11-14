<!-- doc-class: historical -->
# docclass anchor tier — the fork, decided (residual (i) already delivered; residual (ii) accepted as is)

Task: `01a12d5f-166a-7cf6-9c0c-f589096aeab4` (SYSTEM layer, Rule 28 — peerbox).
Tree measured: **611a3082d**. Epic whose numbers are re-measured here:
`devdoc/docclass-anchor-tier-determination-20261012.md` (measured at `3db654fb7`).
Host: peerbox. Branch: `task/01a12d5f166a7cf69c0cf589096aeab4`.

## 1. The briefing was partly STALE — residual (i) is already delivered

The epic presents residual (i) ("the line NUMBER is never validated", 153 rows) as an
open fork whose option (d) would land 153 red rows. **It is not open.** It was
implemented and merged by `03f2d1dfe feat(docclass): detect anchor NUMBER drift,
freeze the 153-row population`, an **ancestor of this tree's HEAD**:

```
$ git merge-base --is-ancestor 03f2d1dfe HEAD && echo ANCESTOR
ANCESTOR

$ git log --all --oneline -- internal/docclass/anchor_number.go
03f2d1dfe feat(docclass): detect anchor NUMBER drift, freeze the 153-row population
```

Three artifacts landed with it: `internal/docclass/anchor_number.go` (the detector,
`AnchorNumberFindings` / `AnchorNumberIncoherent`), `internal/docclass/testdata/
anchor-number-baseline.tsv` (170 lines — a 153-row permit table), and
`internal/docclass/anchor_number_gate_test.go` (the ratchet: unpermitted drift is red,
a stale permit is an error, with a read floor and a permit-ratio floor).

**Rule 31 — a hit is not a delivery, so it was RUN, not read.** The gate's own green
arm printed the population itself:

```
$ go test ./internal/docclass/ -run AnchorNumber -count=1 -v
anchor_number_gate_test.go:142: devdoc/DOC-INDEX.tsv: 232 anchor row(s) scanned,
    153 number-incoherent, all 153 permitted by the frozen baseline
--- PASS: TestAnchorNumberGateOnTheRealTree (0.11s)
... 16 tests, all PASS
ok  github.com/nick/gopi/internal/docclass  0.499s
```

So **residual (i) = already delivered**, and option (d) is not a future decision: it
landed in its only landable form. The epic's literal (d) — "fail when the cell's line
does not carry the text" — would have been 153 red rows with no prerequisite; what
landed instead answers the same question with the 153 rows **frozen as permitted input**
and every NEW drift red. The epic's stated prerequisite for (d) ("needs (b) first") was
therefore avoided, not satisfied.

**The live question is residual (ii) alone.**

## 2. Fresh census of residual (ii) at 611a3082d (Rule 38 — every figure re-run here)

Script: `anchor-ambiguity.py` and `anchor-option-costs.py`, both in this task's session
evidence directory (`~/.gopi/sessions/01a12d5f-…/`), with raw output beside them
(`anchor-ambiguity.txt`, `anchor-option-costs.txt`). Both are read-only over the repo and
print numbers rather than prose arithmetic.

```
$ python3 anchor-ambiguity.py --root .      # commit 611a3082d
anchor rows parsed:        232
  text present (gateable): 232
    pinned    (k == 1):    194
    UNDERDETERMINED (k>=2): 38      (16.4%)
L1b word-count concentration:
  underdetermined with a ONE-WORD recorded text: 26 of 38 (68%)
  pinned with a ONE-WORD recorded text:           1 of 194
```

**The epic's residual-(ii) figures reproduce exactly at the current commit**: 38
underdetermined rows, 26 of them one-word, 1 of 194 pinned rows one-word. 194 + 38 = 232
(the epic had 193 + 38 = 231; the corpus grew by one row and it landed on the pinned
side, which is why the epic's "26 of 231 cells" in the (c′) row is now 26 of 232).

What "underdetermined" costs, concretely — the recorded text is satisfied by up to 1150
distinct lines:

```
k=1150  devdoc/merge-backlog-terminal-guard.md   internal/tools/tasks.go:2825 at '}'
k=1067  devdoc/shm-worktree-risk-assessment.md   internal/tools/task_reclaim.go:751 at '//'
k=545   devdoc/gopi-web-lan-access.md            cmd/agent/cli_voice_client.go:811 at '//'  (x5 rows)
```

And the degenerate texts are not scattered — they are two values:

```
'//'  10 rows   satisfying-lines k: [65,133,208,545,564,1067]
'}'   10 rows   satisfying-lines k: [29,75,110,297,522,800,1110,1150]
```

## 3. The decision

**Residual (i): already delivered** by `03f2d1dfe` (ancestor of HEAD), evidenced by
`TestAnchorNumberGateOnTheRealTree`'s own run above. No work owed; re-implementing it
would have been the stale-briefing trap.

**Residual (ii): option (a) — accept as is**, and record the hole rather than argue it
away. Reason: for every tightening on offer, the measured benefit is small or negative
and the measured cost lands on the DATA manifest (Rule 36) and on the merge/verify gate.

**Measured cost of each option not taken** (`anchor-option-costs.py`, commit 611a3082d):

| option | measured cost | measured benefit |
|---|---|---|
| **(a) accept as is** ← **CHOSEN** | 0 | 0; but the hole is now named in `evidence.go` and here |
| **(b) whole-corpus re-derivation** | rewrites the 2303-data-line manifest (Rule 36 DATA); the epic measured 666 changed lines over 3db654fb7; it re-couples today and drifts again | **0 on either residual** — the generator can never repair a number (its source is the doc's own prose) and it does not tighten the text rung |
| **(c) start-of-line prefix** | makes **1** of the 38 underdetermined rows **unresolvable** (its text is never at line start, so the row's claim is DELETED rather than tightened) | uniquely pins **1** of 38 — reproduces the epic's "tightens 1 of 38" |
| **(c′) minimum normalised word count ≥3** | makes **41 of 232 (18%)** cells unresolvable as written — **27 one-word, but also 14 two-word**; repairing them means re-deriving DATA; and it needs a second, lower-bound generator change at `tools/docclass/classify.py:537` (`lines[n-1].strip()[:70]` caps the text at 70 chars but sets **no floor at all** — that is the hinge the epic calls "the generator change, i.e. (b)'s machinery") | removes the degenerate class (20 of the 27 one-word rows are `}`/`//`) |
| **(d) gate the number** | **moot as stated — already landed** by `03f2d1dfe` in its only landable form; the epic's own prerequisite ((b)) would have bought nothing | closes residual (i) honestly — and it is closed |

**The (c′) finding this run adds, and it is the reason (c′) is refused:** a word-count
floor does not separate "degenerate" from "identifying" — it separates short from long.
Among the cells it would kill are texts that DO discriminate:

```
'mergePrelandGate'        1 row    satisfied by 12 lines
'os.Exit(1)'              1 row    satisfied by 9 lines
'nextIncompletePointer'   1 row    satisfied by 5 lines
'p.recordLine(prefetchRecord{'  1 row   satisfied by 3 lines
```

`mergePrelandGate` at k=12 is 12× ambiguous, but it is *the* identifying token of a
guard; a floor of 3 words deletes its claim outright while leaving every 3-word text
untouched — including any 3-word text that is itself generic. **The floor is aimed at
the wrong variable**: the measured weakness is *generic text*, not *short text*, and the
script shows the two overlap but are not the same set.

## 4. What was changed in this tree (D3)

`internal/docclass/evidence.go`, the `anchoredTextLine` doc comment ONLY:

- The measurement was attributed: "all 185 anchor rows" → the 185 figure is named as the
  population **when the rule was introduced**, explicitly readable as history, because the
  corpus is **232** at 611a3082d.
- The sentence "the looseness costs no discrimination the tier needs" was **false for a
  measured 38** and was replaced by the measurement: 194 pinned / 38 underdetermined, 26 of
  them one-word (`}` ten times, `//` ten times), up to 1150 satisfying lines, with the
  consequence spelled out (such a row survives the deletion of the line it recorded).
- The decision, and the measured cost of each tightening refused, are cited from the comment
  to this report, so the rung reads as *chosen* rather than as *unexamined*.

**No executable line changed** — the edit is inside a comment block and was made so as not
to shift any line number (the frozen 153-row baseline and `TestMergeUnionRowCheckOnTheRealManifest`
both depend on line numbers in the corpus). No anchor row records a line inside
`internal/docclass/evidence.go` (the only anchor into this package is
`internal/docclass/docclass.go:47`), which is why this file was the safe place to correct.

## 5. Verification

```
Build:  go build ./...                                        -> exit 0
Tests:  go test ./internal/docclass/ -count=1                 -> ok  1.736s
        go test ./internal/docclass/ -run AnchorNumber -count=1 -> ok  1.361s (16 tests PASS)
        go test ./cmd/agent/ -run TestMergeUnionRowCheckOnTheRealManifest -count=1 -> ok 0.699s
Ran:    python3 anchor-ambiguity.py --root .        (commit 611a3082d) -> 232 rows / 194 pinned / 38 underdetermined
        python3 anchor-option-costs.py --root .     (commit 611a3082d) -> (c) 1 of 38 pinned, 1 unresolvable;
                                                                          (c') 41 of 232 cells unresolvable
Result: the comment correction shifted no line number — the anchor-number gate still reports
        "232 anchor row(s) scanned, 153 number-incoherent, all 153 permitted", and the
        merge-union row check stays green.
Commit: <see the Commit section of the task close-out>
```

## 6. Findings about the epic itself (recorded, not acted on)

1. **§6 "Filed as `01a12d4e-…`" is a dangling reference.** `gopi task show 01a12d4e`
   answers `no task with task_id="01a12d4e"`. The decision it was filed for was in fact
   delivered by `01a12d1e` → `01a12d21` → `03f2d1dfe`, all `completed`. A reader following
   the epic's own pointer reaches nothing.
2. **The epic's population figures are stale** (231 → 232 anchor rows; 185 in the code
   comment → 232). Both are now named with the commit they were measured at.
3. **The epic's residual (i) premise is stale** by one delivered commit — the reason this
   run did not re-implement it.

## 7. Not Done (honest)

- **Residual (ii) is NOT fixed — it is accepted, and that is the decision.** The 38
  underdetermined rows remain underdetermined; a future reader now finds the hole named in
  `anchoredTextLine`'s comment with a re-runnable census behind it, instead of a comment
  asserting it does not exist.
- **(b) was not executed.** This run did NOT run `tools/docclass/classify.py .` — running it
  *is* option (b), and taking that decision was not this run's. Its cost is quoted from the
  epic's measurement at 3db654fb7 and its blast radius (2303 data lines) measured here; it was
  not re-derived at 611a3082d.
- **(c′) was not implemented**, in either half — neither the word-count floor in
  `anchorTextKey`/`anchoredTextLine` nor the generator's `[:70]` lower bound. Reason in §3.
- **The 27 one-word cells were not re-derived** (that is (b)/(c′)'s machinery, refused).
- **No gate behaviour changed.** The number gate, the text gate, the hold-out gate and the
  merge-union gate are exactly as they were; this run added no check and removed none.
- **Residual (i)'s 153 rows still record wrong line numbers as navigation.** The baseline
  freezes them as *permitted*, it does not repair them. Repairing them is (b), which the
  epic measured as 0 gateable gain and which this run refused.
