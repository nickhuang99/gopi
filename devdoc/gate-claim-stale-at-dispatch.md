<!-- doc-class: historical -->
> **HISTORY (added by the restore row).** `internal/tools/merge_config_anchor_gate.go` and its
> test file were deleted whole in commit `5a35f6104` (the `internal/docclass` teardown). Every
> occurrence below is a RECORD of that lane while it existed — **not** a command to run and not
> a check that runs today. That lane's live check **was** the census generator's `--check` leg
> plus the arms restored under the same documented name `TestMergeConfigAnchorGate`, which drove
> it rather than any merge-time lane. **BOTH WERE DELETED (2026-11-05)**: `78bdf05a`
> (operator ruling) removed the documentation-citation family — `config-anchor-census.go`, its
> `.tsv`, `citation-census.go`, `citation-selector-census.go` and the gates that drove them —
> and `b8cad761` removed `internal/tools/merge_config_anchor_gate_test.go` with the last census
> arm. Nothing judges CONFIG.md's anchors today: `go run` on the generator now prints
> `stat … no such file or directory`, and `-run TestMergeConfigAnchorGate` prints
> `[no tests to run]` and exits 0 (an EMPTY GREEN, not a pass). The anchor rules are prose
> authors obey; a reader must not run either command this note used to give.
# The gate-assertion clause: a RED claim that names no revision gets the ROW's own age

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> **STATUS (2026-10-17) — READ FIRST.** The LEAF **and** the dispatch-seam
> **WIRING both ship.** The wiring was withdrawn once (`1aa9b855b`, 2026-10-16
> 15:33) because it reddened a ground-announcement E2E by truncating the child
> prompt, and it was **restored** the same night (`1225d917c`, 23:58 — "restore
> the gate-assertion wiring, re-based onto master's path-set clause"), which is an
> ancestor of this file's base `942b3ac`. Measured on this tree, not inferred:
> `git merge-base --is-ancestor 1225d917c HEAD` → exit 0, and a live dispatch
> (this file's own row) received the clause. The truncation mechanism that forced
> the withdrawal is `devdoc/claim-truncation-at-dispatch.md`; it is no longer
> binding, and **the withdrawal paragraph below is history, not state.**
>
> **What was still open, and is now closed here.** The scanner read only the
> branch-NAMED spelling ("`<surface>` is RED on master"), i.e. a verdict bound to
> the integration branch standing AFTER it. Row `01a1432e-4441-71a6-bd4c-f94fce6cbe11`
> asserted the same defect branch-LESS, in this repo's CJK word order — "…在
> pristine 树上即红" — and was dispatched UNSTAMPED because of it (measured on that
> row's composed briefing text: `claims=0 misses=0`). The branch-less arm below
> closes that spelling without weakening the binding rule the file already
> removed a measured false positive with.
>
> **The withdrawal paragraph, kept as history** (it described `1aa9b855b`'s
> state, 2026-10-16 15:33 — superseded by `1225d917c` the same night): it reported
> binding the clause to the spawn seam reddening
> `cmd/agent/TestWorkerAnnouncesTheGroundToTheChildAndLeavesOneWorktree` **9/9**
> against **6/6 PASS** on base `3db1fd211`, the measured mechanism being a
> TRUNCATED CHILD PROMPT — and "a clause truncated away is a clause that does not
> exist". That bound has since been named and fixed; the clauses now compose
> through `tools.BriefingWithClause` (ONE append decision), which is what the
> restore re-based onto.


**Epic**: `01a14242-29e7-70ef-a9f3-346c89f33a3f` (the third seal at the dispatch
seam). **Measured on**: row `01a136be-e3b2-71b9-9219-0b4a2258feff`.

## The incident, measured

That row's briefing opened with:

```
`go test -run TestDocClassBannerMatchesManifest ./internal/standards/` is RED on master, in a file
this branch does not touch:
```

- The defect was **real when the row was authored**: `created_at` =
  `2026-10-14 03:30:42 +0800` (epoch `1791919842`), integration tip `82605ae57`,
  and the named file's `DOC-INDEX.tsv` row said `historical` while the file did
  not open with its banner.
- It was repaired **22 minutes later** (`f468c3101`, `04:49:50`), on the
  integration branch by `05:35:57`.
- The row was **dispatched two days later** (`2026-10-16 08:52:47`) with nothing
  on it saying so. The dispatched run's first moves were spent re-measuring a
  premise that had been dead since the day it was written, and it could only say
  so *after* paying for it.

## Why the two halves already on the seam could not see it

Measured on that row's own briefing text before the change (probe output
`claims=0 / misses=0` for both):

| instrument | fires on | why this sentence is invisible to it |
|---|---|---|
| `taskreport.BaseClaimsIn` (`baseclaim.go`) | a base **word** and a **commit-shaped token** on one line | the sentence declares no base and names no revision at all |
| `taskreport.UnlandedClaimsIn` (`unlandedclaim.go`) | a sentence asserting the branch **LACKS** something ("never landed") | this one asserts the branch **HAS** a red surface — the opposite direction |

**Positive control for that reading**: the same seam *does* stamp the sibling
spelling `base 2f697e5`. The gap is the **spelling**, not the seam.

## What was built

Four files, all additive, no new tool/script/resident artifact (Rule 35).

### 1. `internal/taskreport/gateassert.go` — the leaf scanner

`GateAssertionsIn(body, branch)` yields a `GateAssertion{Line, Surface, Branch}`
per assertion; `GateAssertionMissesIn` yields the lines it *saw* but could not
attribute to a surface. The predicate has two halves and both are measured:

1. **Binding, not co-residence.** A verdict word (`red`, `fails`, `failing`,
   `broken`) must stand as a standalone, un-negated word with the branch's name
   inside `gateAssertionBindingWindowRunes` **after** it.
   *Why the direction matters*: the real briefing carries a **second** line —

   ```
   Every `go test ./internal/standards/` on master reports this red beside whatever the branch
   ```

   — where the branch comes **first** and `red` is a *reference* to the assertion
   made one line earlier. The first implementation (co-residence) stamped it:
   `claims=2` on the real text, one of them a surface nobody asserted anything
   about. **A false positive on an honest briefing is worse than the silence this
   epic fixes**, so the binding rule is the predicate's load-bearing half and the
   false positive is pinned as a test fixture.
2. **Negation is the repair, not the assertion.** `not red`, `no longer red`,
   `never fails` must stay silent: those are the sentences that *report* the fix.

Surfaces, in the order this repo's briefings use them: a backtick fence, a
revision-shaped token, a Go test-function name, a repo-path-shaped token.

### 2. `internal/tools/task_briefing_gate_assert.go` — the anchor and the clause

The moment the assertion was made is recovered from the **ROW** — `created_at` —
resolved to a revision with

```
git rev-list --first-parent --max-count=1 --before=<epoch> <IntegrationClaimTargetRef>
```

and measured for distance against the ref every other clause on this seam uses
(`IntegrationClaimTargetRef` — one resolver, Rules 8/13/14).

The reading is a fact about the briefing's **AGE**, never about the defect, so
the clause says `RE-MEASURE THE SURFACE BEFORE REPAIRING` and denies the
completion reading in its own words. A stamp that claimed the defect was gone
would be this epic's own defect, one level up.

**The positive control is structural**: a revision at the tip has distance 0 and
stamps nothing — and the arm is *recorded*, not silently skipped.

### 3. `internal/tools/briefing_clause.go` — one append decision

Six clauses now ride this seam. Each call site used to re-derive "did it stamp?"
from the text (`if clause != ""`); the seam now asks once —
`BriefingWithClause(briefing, clause) (text, appended)` — so the marker line on
the run's facts cannot say "stamped" about a briefing the child received
unchanged.

### 4. `internal/tools.TaskCreatedAt` (`tasks.go`) — the row's own authoring moment

Read through the same read-only opener every other row lookup on this path uses
(`openTasksDBReadOnly`). Returns `(value, present, error)` — a row with no usable
moment is **absent**, not `0`, so the caller states the refusal instead of
measuring against an invented anchor (Rule 15a).

## The arms the seam must tell apart

Every one of these leaves a line in the run's record; none is a silent pass:

| arm | condition | behaviour |
|---|---|---|
| **stale** | authoring revision is behind the tip | clause stamped; record states the distance |
| **current** (positive control) | authoring revision **is** the tip | nothing stamped; reason recorded |
| **unreadable** | verdict + branch, no surface token | nothing measured; reason recorded (this is a **hole in the instrument**, not a briefing defect — the fixture line is real prose) |
| **unanchored** | `created_at` not read, or the ground is not a repo | nothing measured; reason recorded |
| **unresolvable** | no commit at or before that moment | nothing measured; reason recorded |
| **not an ancestor** | the authored revision is not on the integration line | nothing measured; reason recorded |
| **already stamped** | a second pass over a stamped briefing | appends nothing; says so |

The last row is not cosmetic: the clause **quotes** the briefing line it
contradicts, so a plain re-read of a stamped briefing would re-fire on its own
quote (a resume, a re-dispatch, an operator paste).

## Verification (what was actually run)

- `go build ./...` → OK.
- `go test ./internal/taskreport/ -run GateAssert -count=1` → ok.
- `go test ./internal/tools/ -run 'TestBriefingGateAssert|TestTaskCreatedAt' -count=1` → ok.
- Real run on the motivating row's own briefing text, against this ground,
  `created_at` epoch `1791919842`:

  ```
  briefing gate assertion → "`go test -run TestDocClassBannerMatchesManifest ./internal/standards/` is RED on master, in a file" as of 2026-10-14 03:30:42 (82605ae5) is 436 commit(s) BEHIND refs/remotes/gitPushcheck/master (exit=0) — STAMPED into the child's briefing (82605ae5)
  ```

  and the clause it produces names the surface, the epoch, the resolved revision
  `82605ae5745b9628fb879d4edf38bb4ee841b5e3`, the distance, the three re-runnable
  commands, the instruction and the denial. **The anchor is derived from a
  revision and a commit date, so it is stable and re-derivable** — the reading
  does not depend on when the probe ran.

## The branch-less arm (2026-10-17) — the spelling row 01a1432e was dispatched blind to

The wiring ships; what it could not SEE was a verdict bound to a tree state
instead of to the integration branch's name. That is the SECOND half of why row
`01a1432e-4441-71a6-bd4c-f94fce6cbe11` went out unstamped, and it is closed here.

**Measured, on that row's own composed briefing text** (the summary header plus
the sentence, i.e. exactly what `tools.taskRunBriefing` hands the seam), driven
through the real scanner against this host's ledger:

| | before | after |
|---|---|---|
| row `01a1432e`'s assertion | `claims=0 misses=0` | `claims=1`, surface `TestDocClassBannerMatchesManifest`, anchor `树上` |
| the real clause function over that text | no clause | 1819 bytes, `288 commit(s) PAST` the authoring revision |
| whole corpus (1515 ledger rows) | `claims=51 misses=16` | `claims=107 misses=46` |

The corpus column is **not** a quality score, and it is not ONE arm either —
measured on that same 1515-row sweep, the 56 newly-claiming rows split in two:

| anchor of the newly-read claim | rows | what it asserts |
|---|---|---|
| a tree state (`树上` / `pristine`) | 23 | red at an unmodified checkout — row 01a1432e's class |
| the branch name, CJK word order (`在 master 上恒红`) | 32 | red on master, written anchor-FIRST |
| a Latin `pristine`/branch spelling that one line already carried | 1 | (the same row re-read line by line) |

The second row is the honest surprise of this change: this repo's briefings bind
a verdict to `master` in **CJK word order** far more often than to a tree state,
and the Latin arm could never read that order — the same blindness, a larger
population. It is still a *binding* rule (anchor before verdict, negation
honoured, no anchor-after reading for a CJK verdict), so it adds no co-residence
false positive; `TestGateAssertions_TheCJKAnchorMustPrecedeTheVerdict` is the
guard.

What matters for both rows is that **no added claim is a REPAIR report** — the
negation arms are what keep that true, and the sweep was checked for it (grep for
`不红`/`未红`/`没红`/`别红` over the 77 newly-emitted claim lines: 0 hits).

**What the arm is.** A verdict word bound to an anchor standing *before* it:
`pristine` (Latin, standalone word) or `树上` (CJK, substring — CJK prose does not
space its words), plus the CJK verdict `红`. Two rules make it narrower than a
co-residence rule, and both are pinned by tests:

- **direction**: for a CJK verdict the anchor must stand BEFORE the word. The
  anchor-after reading was measured on this host's corpus firing on "「恒红」不是
  master 的缺陷" ("always-red is NOT a defect of master") — a sentence asserting
  the OPPOSITE of a red surface.
- **negation**: CJK particles (`不`, `未`, `没`, `别`) deny the verdict, the CJK
  counterpart of the Latin `not `/`no ` list. Without it, a row reporting the
  repair ("已经不红了") is stamped as if it asserted the defect.

**The Latin false positive stays dead.** The binding rule (verdict first, anchor
within 24 runes AFTER it) is untouched: `TestGateAssertions_TheLatinArmStillNeedsItsBranch`
still holds the "on master reports this red" line silent, and
`TestGateAssertions_TheBranchlessArmIsBoundNotCoResident` extends the same
discipline to the new anchors. The clause renders the branch-less anchor as what
it is — `the TREE STATE "树上" the assertion names (an unmodified checkout of
master)` — rather than claiming the author wrote "on master", which they did not.

## Verification (2026-10-17, this tree)

- `go build ./...` → OK.
- `go test ./internal/taskreport/` (whole package) → ok; `go test ./internal/tools/
  -run 'Briefing|GateAssert|gateAssert'` → ok; the three `cmd/agent` E2Es that drive
  the briefing clause end to end (`TestBriefingLastClauseReachesTheChildsFirstRequest`,
  `TestBriefingPathSetClauseReachesTheChildsFirstRequest`,
  `TestWorkerAnnouncesTheGroundToTheChildAndLeavesOneWorktree`) → ok.
- `make test-budget PKGS='./internal/taskreport ./internal/tools ./cmd/agent'` is **not
  clean on this base**, and the red is classified rather than assumed: on a detached
  worktree at `942b3ac` with NO edits, the same selection fails the SAME five
  `internal/tools` gates (`TestAssertionTotalityAuditGate` + three
  `TestMergeConfigAnchorGate*`, whose report names 8 rotted citations in
  `cmd/agent/{cli_voice.go,main.go,merge_run.go}` — files this change does not touch);
  and the `cmd/agent` worker/claim/take-over tests are `ok` in isolation on BOTH trees
  (`2.648s` base, `2.397s` here) while red inside the parallel bath — the interference
  class this repo's ledger already records. The measurement is
  `evidence/02-budget-run-classification.txt` in the run's session directory.
- The real scanner over row 01a1432e's composed briefing, before → after quoted
  in the table above; the probe sources were temporary and are **not** committed
  (their output is).

## Not done (honest)

- No `cmd/agent` package-level test of the wiring itself beyond the build and the
  existing suite (the seam is a sequence of best-effort clauses; the clause
  function is covered directly at the `internal/tools` level, and the wiring is
  four lines). A full end-to-end walk (a real `gopi worker --once` on a fixture
  row whose `created_at` is old and whose briefing carries the assertion) is the
  obvious next test and is **not** in this branch.
- The scanner reads only the shapes listed above. A briefing asserting a red
  surface in a form none of the four surfaces matches is reported as an
  **unreadable miss**, not silently ignored — but it is still not stamped.
