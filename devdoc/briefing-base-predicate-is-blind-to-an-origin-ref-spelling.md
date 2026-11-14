<!-- doc-class: historical -->
# The base-revision predicate reads a WORD, so `origin/master a802b47` declares nothing — measured on row `01a17dc1`, 2026-10-28

**Scope:** the base half of the briefing premise seam — `internal/taskreport.BaseClaimsIn`
(`internal/taskreport/baseclaim.go:77`, `baseClaimWords = []string{"base", "基线"}`),
read by `TaskBriefingPremiseBaseRefresh` (dispatch,
`internal/tools/task_briefing_premise.go:196`, called at `:206`) and by
`internal/tools/task_briefing_base_submit.go:117` (submit).

**Verdict of the measurement:** the predicate is correct about what it reads, and
**structurally silent about the spelling this repo's briefings actually use for the
same declaration**. A briefing that pins its evidence at
`origin/master a802b47` declares a base revision and gets **nothing measured** —
not a stale stamp, not a refused-token record, not even a "declares no base"
line. `claims=0, misses=0`. The two sibling documents
(`devdoc/briefing-base-revision-staleness-at-dispatch.md`,
`devdoc/briefing-base-revision-staleness-at-submit.md`) both assume the author
wrote the word `base`; this document is about the author who did not.

---

## What the predicate requires, and what it therefore cannot see

A claim exists only where a **base word** and a commit-shaped token stand together
(`baseclaim.go:77` declares the two words; the doc comment at `:40-52` states the
narrowness is deliberate). Ten shapes, probed through the real predicate
(`BaseClaimsIn` / `BaseClaimMissesIn`, no reimplementation):

| line | claims | misses |
|---|---|---|
| `base a802b47` | 1 | 0 |
| `base commit a802b47` | 1 | 0 |
| `base origin/master a802b47` | 1 | 0 |
| `基线 a802b47` | 1 | 0 |
| **`origin/master a802b47`** | **0** | **0** |
| **`on master a802b47`** | **0** | **0** |
| **`git checkout --detach a802b47`** | **0** | **0** |
| **`master `a802b47` 跑同一个测试`** | **0** | **0** |
| **`detach 到 origin/master `a802b47` 跑同一个测试`** | **0** | **0** |

The last five are declarations a reader makes without effort, and every one is
invisible. **`misses=0` is the sharper half of the finding**: the refused-token
record (`BaseClaimMissesIn`, `baseclaim.go:202`) exists precisely so that a
declaration the predicate cannot read is distinguishable from no declaration at
all (Rule 15 — that is the reason `BaseClaimMiss` exists). It does not fire here,
because nothing about `origin/master a802b47` looks like a refused *shape*: the
line carries no base word at all, so the scanner never starts.

## The measured case (row `01a17dc1-4fe2-7c63-9b1d-f1add338eda8`)

That row's briefing pins its evidence at `a802b47` in exactly this spelling:

```
未改任何生产 `.go`）。取证方式：detach 到 origin/master `a802b47` 跑同一个测试。
### 1) 在 origin/master a802b47（detached）上跑那个测试
```

Measured facts (all commands and raw output below):

- Its dispatch produced **zero** base record lines:
  `grep -c "briefing base →" /home/user/.gopi/sessions/01a17dc1-4fe2-7c63-9b1d-f1add338eda8/output.log` → **0** (exit 1).
  A control row dispatched through the same seam carries two:
  `grep -c "briefing base →" /home/user/.gopi/sessions/01a1365f-aaa6-7b34-a0c1-02d531e6ceb1/output.log` → **2**.
- `a802b47` had **already been superseded** when the row was written: the row was
  created `2026-10-27T15:39:25Z` = **23:39:25 +0800**, while `a802b47` is
  `C=2026-10-27T22:08:24+08:00` and the fix for the very surface it called red,
  `a11a33f` (`fix(chat): a usage query does not launder a valued flag stated
  twice`), is `C=2026-10-27T22:55:26+08:00` — **44 minutes before the row was
  created**, with `git rev-list --count a802b47..6c752f6` = **21** commits of
  master movement in that window.
- So the row reported "RED on master" from a revision master had left behind
  half an hour earlier, and the seam that exists to catch exactly that had
  nothing to measure.

**This is not a claim that the row was wrong to file.** The defect it described
was real at `a802b47` (the sibling run reproduced it there in an extracted tree:
`rc=0` + the 84-line page + `--- FAIL`). What went stale was the *revision it
pinned*, and the instrument that would have said so was blind to the spelling.

## Corpus census — how often the blind spelling appears

Generator: the harness at
`/home/user/.gopi/sessions/01a17e04-c358-7460-a5e6-00e972f7085e/evidence/r2-scanharness/main.go`
(it calls the shipped `taskreport.BaseClaimsIn` / `BaseClaimMissesIn` — no
reimplementation of the predicate; the corpus is every `payload` field under
`/home/user/.gopi/sessions/*/metadata.json`). Measured 2026-10-28 at ground HEAD
`84beb309ce3c90875e85142eb12769564778ea45`:

| dimension | value |
|---|---|
| payloads scanned | **1959** |
| carry the `origin/master <sha>` spelling | **7** |
| …of those, `claims=0` (fully silent, no base record) | **4** |
| …of those, claims come from a DIFFERENT line of the same payload | **3** |
| payloads that declare a base in a shape the predicate DOES read | **141** |
| payloads carrying any `origin/<ref> <sha>` spelling (the broader parent) | 7 (0.36 %) |

The 4 silent ones, by row: `01a12e64-23c3`, `01a13394-a0d9`,
**`01a17dc1-4fe2`**, `01a1811f-3f9e`. The three that "claim" do so from an
unrelated line — their pin is still unmeasured, which is why the count of
*silent pins* is 7, not 4.

## Why the obvious widening is not obviously right

The cheap fix is "treat `origin/<ref> <sha>` as a base word". It is **0.36 %** of
the corpus, so the false-positive exposure looks small — but the precision cost
is not zero and it is not measured by that number: `refs/remotes/origin/master`
appears in briefings as a **subject of measurement** ("I measured AT it") and as a
**comparison operand** ("`merge-base --is-ancestor f6031ead9 origin/master` →
rc=0"), and only the first is a base declaration. `baseClaimRevisionVerbs`
(`baseclaim.go:131`) already refuses the command shapes for the same reason. Any
widening has to answer which of those two a given line is, and that is a
judgement with its own false-positive budget — not a keyword to add.

**This document does not make that change.** It measures the hole and files it
(Rule 41: find it, take the evidence, put it in the queue; the repair is its own
row). Widening the predicate is a decision for whoever owns the seam.

## The same predicate's OTHER failure, measured by filing this row

Filing the bug row from this page (`gopi task submit`, 2026-10-28) produced a
stamp **about this page's own probe table** — the submit seam read a quoted
shape as a declaration:

```
[Base revision — a declared base was re-measured at submit]
warning: 1 declared base revision(s) in this payload are already behind the integration branch
- base a802b473 is 64 commit(s) behind 84beb309 …; payload line: "base a802b47                                         claims=1 misses=0"
```

That `payload line` is a **row of the ten-shape table above**, whose entire
purpose is to report what the predicate answers for the text `base a802b47`.
The payload declares no base revision; it measures the predicate. The stamp
therefore sent a reader to re-measure `a802b47` — the wrong object — on a page
exactly about the instrument being unable to tell which object it is looking at.

**And it compounds.** The warning text itself contains `base a802b47`, so the
next submit that quotes the warning (as the second row filed from this session
does) is read as declaring that base **again**:

```
warning: 2 declared base revision(s) in this payload are already behind …
- … payload line: "- base a802b473 is 64 commit(s) behind 84beb309 …
- … payload line: "| base a802b47 | claims=1 misses=0 |"
```

A machine whose warning about quoted text is itself quoted text has no floor on
this: each report about the stamper becomes a fresh stamp. This is the
**false-positive** direction of the same predicate, and it is filed separately
(row `01a18232-3537`) from the blind-spelling one above (`01a18231-d8bb`); the
two are the same instrument and must be repaired against each other's controls,
because widening one sharpens the other.

## What this record is NOT

- Not a duplicate of the two sibling documents. Those measure a **stale
  declaration the seam could read** and the stamps it renders; this one measures a
  **declaration the seam cannot read**, in the spelling the sibling's own
  motivating row would have used if it had not written the word `base`.
- Not a claim that `01a17dc1` was fabricated. The row's observation is
  reproducible at the revision it names; the row's **premise** had expired, and
  nothing told it so.
- Not a change to any predicate, test or stamp. Zero production lines move in
  this commit; the artifact is this page plus the row filed from it.

## Commands (verbatim, re-runnable)

```bash
cd /home/user/workspace/01a17e04-c358-7460-a5e6-00e972f7085e
grep -c "briefing base →" /home/user/.gopi/sessions/01a17dc1-4fe2-7c63-9b1d-f1add338eda8/output.log; echo "exit=$?"
grep -c "briefing base →" /home/user/.gopi/sessions/01a1365f-aaa6-7b34-a0c1-02d531e6ceb1/output.log; echo "exit=$?"
git log --format='%h A=%aI C=%cI %s' -1 a802b47
git log --format='%h C=%cI %s' -1 a11a33f
git rev-list --count a802b47..6c752f6
grep -n "origin/master" internal/taskreport/baseclaim_test.go; echo "grep_exit=$?"   # → 0 matches: no shipped test pins this spelling
```
