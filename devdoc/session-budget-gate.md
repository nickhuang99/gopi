<!-- doc-class: historical -->
# Task Report: the session-budget gate — the 60-minute budget enforced at the ONE beat that exists

**Branch:** `feat/session-budget-gate` (worktree `/home/user/workspace/gopi-session-budget-gate`), base `master 2e33714e`
**Epic verdict:** DOABLE, and delivered as such — **option (A) and option (B) both implemented; option (C) deliberately NOT implemented.**

## Completed

### (A) The commit-beat gate — `internal/agent/loop.go`

`commitTaskItem` (the `task(action=commit)` body) now asks the budget BEFORE it
writes anything:

```go
if refusal, refused := a.sessionBudgetRefusal(); refused {
    return refusal
}
```

`sessionBudgetRefusal()` fires only when **all four** hold:

1. the budget is finite **and** a clock exists (`sessionBudgetStatus().ok`);
2. `elapsed >= sessionBudgetWarnPercent` of `sessionBudget()`;
3. `hasOpenTrackedItems()` — the model's tracked list still holds an item that is
   not completed, i.e. **this commit would ADD a further subtask**;
4. no `reframeVerdict` has been filed this run.

Otherwise the pre-existing behaviour is untouched. The refusal **records
nothing** (it returns before `tools.WriteTaskArea`), so there is no
half-committed state, and it names the two concrete exits — LAND THE WORK, or
`task(action=reframe, verdict=TOO_BIG)` + roadmap. Nothing is killed.

The gate is **narrow on purpose** (the operator's constraint, quoted in the
epic: *"我们目前不要过度强制… 强制不好"*): the escape hatches are exactly the two
the epic names, and condition 4 exists so that a run which HAS reframed keeps
committing — otherwise the gate would deadlock the very exit it prescribes.

A kill switch (`GOPI_SESSION_BUDGET_REFUSAL=0|false|no|off`) is read by exactly
one function, `sessionBudgetCommitRefusal()` (Rules 2/21). Anything that is not
an explicit off-spelling leaves the gate **on** — a typo must not silently
disarm the behaviour being measured (pinned by test).

### (B) The reframe TIME DIMENSION — `internal/agent/reframe.go`

- `reframeTimeRefusal(in)` refuses `verdict=DOABLE` past the mark **while tracked
  items are open**, and latches nothing: the run may re-issue with the honest
  verdict. A closed/absent tracked list is exempt (the "epic already finished"
  case — an honest DOABLE costs nothing there), as are `TOO_BIG`/`IMPOSSIBLE`
  (both already carry work).
- The criterion is **taught** on every surface that carries it: the `task` tool
  description (`sessionBudgetTimeDimension()`), the `roadmap` schema description
  (`sessionBudgetCeilingClause()`), `reframeDirective` **and**
  `reframeNudgeMessage` — the last two ride the EPIC message rather than the
  density-gated system prompt. All render from `sessionBudgetMinutesResolved()`,
  so `GOPI_SESSION_BUDGET` changes what the model reads.

### (3) The escalation is now actionable prose

`sessionBudgetLine()` past the mark states **elapsed, remaining, budget, percent
and the expected action** (it previously stated elapsed + percent only, and its
action clause was a `Split it NOW` line with no land-the-work exit). Below the
mark the line is unchanged. Three readers (`sessionBudgetLine`,
`sessionBudgetRefusal`, `reframeTimeRefusal`) share ONE measurement
(`sessionBudgetStatus`) — no second inline percentage.

### (4) Tests — `internal/agent/sessionbudgetgate_test.go`

Table-driven, at the real entry points (`commitTaskItem`, `handleTaskReframe`),
not on the predicates alone: the gate's three bands; the short-override band;
the no-list band; the unlimited band; the budget-0 / nil-clock
**byte-identity** case (three agents with wildly different clocks must return
the *same* string); the kill switch (nine spellings + one through the beat); the
post-reframe stand-aside; the reframe time dimension (six bands, both sides of
every precondition); and the teaching surfaces (the ceiling clause follows the
env override; the roadmap schema and both reframe guidance strings carry it).

### (5) This report + (6) the census

`## Census` below carries the before/after numbers.

## Not Done (honest)

- **(C) prompt-injection slimming is NOT implemented** — deliberately, per the
  epic. It is a separate ticket.
- **No GOPI.md rule was added** — deliberately, per the epic (the file's growth
  is a symptom, not the cure).
- **No session is killed, no context is truncated, no raw log is stopped,
  `GOPI_SESSION_MAX_WALLCLOCK` is untouched, `--fast` and the merge driver are
  untouched, and there are no scripts/cron/daemons** — all as constrained.
- **The doc-anchor gate is NOT green on this branch — and it was ALREADY RED on
  clean master `2e33714e`.** Measured identically on both: `2494 anchors,
  233 pre-existing drift entries permitted, 11 stale permits`, with the `new`
  count moving **75 (master) → 71 (branch)**. **That `−4` is NOT a re-key by
  this change — no doc was edited** (`git diff --name-only master...HEAD` lists
  only `DOC-INDEX.tsv` and four new docs; nothing pre-existing). It is drift
  coincidence: this diff shifts `internal/agent/loop.go` by ~168 lines, so
  OTHER docs' bare `internal/agent/loop.go:<lineno>` anchors land on different lines, and the
  blank-line hits they produce happen to net four fewer (measured per-file:
  20 → 16 `loop.go … BLANK line` lines, 10 new / 14 gone, as a SET). An earlier
  commit of this branch did carry a re-key sentence in this report, and it was
  wrong; it is corrected here. The pre-existing
  `TestDocClassBannerMatchesManifest` failure
  (`handoff-20260924-night-autonomous.md`, `runaway-loop-20260923.md`,
  `full-id-no-prefix.md` classified `historical` without the banner) is also
  identical on master and branch and is **not mine**. "Green doc-anchor gate" is
  therefore unattainable on this base, and the honest report is an unchanged
  pre-existing count plus a drift-coincident net −4 that is NOT mine to claim.
- **`devdoc/DOC-INDEX.tsv` DOES carry the new row, and the evidence tier had to
  be measured rather than guessed.** A class-driven corpus means a new devdoc is
  invisible until classified, so the row is part of the deliverable. Two tiers
  were tried on the branch tip, and the difference is real:
  - `anchor internal/agent/loop.go:2233 at func (a *Agent) sessionBudgetRefusal() (string, bool) {`
    — the docclass gates pass, but the **anchor gate rejects the row**: the
    cited line is a `func` declaration, which the anchor tier's own
    line-must-not-be-blank/unattributable check treats as unresolvable, so the
    doc is reported as `has no resolvable anchor`.
  - `path internal/agent/loop.go` — **both** gates pass: the docclass gates stay
    green and the anchor census keeps the doc readable — **zero** error lines
    name this doc at the final tip (`2494 anchors across 514 normative docs …
    71 new, 11 stale`; the 455 out-of-scope bare continuations and the 233
    permitted drift entries are unchanged, and every one of the 71 `new` lines
    belongs to another file). Tier `path` is
    what a newly-landed doc can honestly supply: the file it names resolves, and
    the standing-claim spot-check is the doc's own symbol reference rather than a
    line number that this very change would shift.

## Verification

- Build:  `go build ./...` → RC=0; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (26284136 bytes). `git diff fbfeb27b..HEAD -- internal/ cmd/` is EMPTY — the binary that produced both acceptance refusals is this branch's code (the later commits are devdoc-only).
- Standards: `go test ./internal/standards/ -count=1` → 1 FAIL, `TestDocClassBannerMatchesManifest`, from THREE docs this branch does not touch (0 hits in `git diff --name-only master...HEAD`); pre-existing, reported not chased.
- Vet:    `go vet ./...` → RC=0
- Worktree CLEAN at hand-over (`git status --porcelain` prints nothing), `gofmt -l internal/agent/` → empty. (The branch head moves with every devdoc commit, so this report names no sha as "the tip": read it with `git log -1 feat/session-budget-gate`. SHA-in-doc self-reference is a moving target — noted because an earlier commit of this report got it wrong twice.)
- Tests:  `go test ./internal/agent/ -run 'TestSessionBudget|TestReframeTimeDimension|TestSystemPromptDensity|TestGOPIRule' -count=1` → **ok** (38.3s); **full `go test ./internal/agent/` → 3 FAIL, ALL THREE reproduced identically on clean master `2e33714e`** (see Evidence — `TestDocAnchorsResolveInNormativeDocs`, `TestAgent_WallclockCap_TruncatedRunIsNotCompleted`, `TestAgent_StuckLoop_TruncatedRunIsNotCompleted`); `gofmt -l internal/agent/` → empty
- Ran (A): a real 1-minute-budget run in a standard sandbox, quoted verbatim in Evidence
- Ran (B): a real 5097-char oversized brief reframed at `verdict=DOABLE` in a 1-minute-budget run, quoted in Evidence
- Result: both refusals observed **in the session JSONL**, not only on stdout

## Evidence (self-filed)

**(A) The commit-beat refusal, from the session JSONL** —
`$SB/.gopi/sessions/40c91ffc-c885-4bdb-8bb6-ea3bf4d35f60/session.jsonl`,
read with `python3 -c` over the JSONL and printed block-by-block (the run's own
`tool_result` record, byte-for-byte):

```
REFUSED — past 75% of the session budget with tracked items still open: 1m9s elapsed, 0s remaining of 1m0s (114% used). This commit would add a further subtask, and a task that cannot finish inside the budget is TOO BIG by definition. Two exits, both available now: (1) LAND THE WORK — finish and commit the items still open, then commit with the list fully completed; or (2) SPLIT — file task(action=reframe, verdict=TOO_BIG) with a roadmap, and the harness enqueues the children. The commit was NOT recorded. Nothing else is required: no session is being killed.
```

(and the second attempt, `1m10s elapsed … 117% used`, same text — the tracked
list still held item 2 as `progressing`, which is exactly the "would ADD a
further subtask" condition. Run summary: `71.8s | 5 iterations | OK`,
`tool_calls=3: bash 1, task 2`.)

**(B) The reframe time-dimension refusal, from the session JSONL** —
`sandbox-reframeprobe3-8d09fb2a/.gopi/sessions/e01db8a9-2bca-4329-af7d-8558c0ea1c17/session.jsonl`,
printed block-by-block with `python3 -c` over the JSONL (the run's own
`tool_result` record, byte-for-byte):

```
task action=reframe: verdict=DOABLE REFUSED — past 75% of the session budget (1m49s elapsed, 0s remaining of 1m0s, 181% used) with tracked items still open: an epic still unexecuted at the escalation mark is not executable here — it is TOO_BIG with a roadmap (each item completable inside ONE session), or IMPOSSIBLE with a reason. Nothing was recorded and nothing is blocked: re-issue this same call with the honest verdict.
```

The probe's `[TASK_LIST]` (item 2 `progressing`, never completed) is what made
the epic "still unexecuted", and the run stopped there without implementing any
of the 21 subsystems — the refusal redirected it, which is the whole point.

**The gate's own tests, green:**

```
--- PASS: TestSessionBudgetGateBands (11.22s)                       6 bands
--- PASS: TestSessionBudgetGateUnlimitedAndNilClockByteIdentical (7.63s)
--- PASS: TestSessionBudgetGateKillSwitch (1.89s)                   9 spellings + 1 through the beat
--- PASS: TestSessionBudgetGateStandsAsideAfterReframe (1.93s)
--- PASS: TestReframeTimeDimension (0.55s)                          6 bands
--- PASS: TestReframeTimeDimensionTeachesTheCeiling (0.00s)
ok  	github.com/nick/gopi/internal/agent	28.832s
```

**Doc-anchor census, measured identically on both refs** (`go test
./internal/agent/ -run TestDocAnchor -count=1`):

| ref | line |
|---|---|
| master `2e33714e` (main checkout, clean) | `doc anchors verified: 2494 across 513 normative docs … 233 pre-existing drift entries permitted, 75 new, 11 stale permits` |
| branch tip (`4a582911`, doc + manifest row present) | `doc anchors verified: 2494 across 514 normative docs … 233 pre-existing drift entries permitted, 71 new, 11 stale permits` |

Both FAIL — the gate does not pass on this base at all, in either ref: the 71
`new` and 11 `stale` lines name other files (measured: **zero** error lines name
`devdoc/session-budget-gate.md`). The branch's net `−4 new` is NOT a re-key —
see the `## Not Done (honest)` note: no pre-existing doc was edited by this
change, so the difference is drift coincidence on other docs' bare line anchors.

**Re-keyed anchors: NONE — and that is a deliberate call, corrected from an
earlier draft of this report.** The diff shifts `internal/agent/loop.go` (the
`sessionBudgetLine` family moved down ~119 lines), so OTHER docs
naming a bare `internal/agent/loop.go:<lineno>` may now point at a blank line (the measured
set is in the evidence below). Those docs are **landed records**
(`agent-five-organs-ontology.md`, `hippocampus.md`, `cov-agent-cr.md`,
`cli-as-eyes-and-hands-feasibility.md`, …), and the anchor gate's own baseline
comment says re-keying a record falsifies it — the gate permits that drift for
exactly this reason. They are REPORTED here so the next reader can re-key **by
symbol** deliberately rather than by accident. No file outside this change's
own four was touched, so this seam cannot be the source of a new failure.

**Pre-existing full-suite failures, measured identically on both refs**
(`go test ./internal/agent/ -run
'TestDocAnchorsResolveInNormativeDocs|TestAgent_WallclockCap_TruncatedRunIsNotCompleted|TestAgent_StuckLoop_TruncatedRunIsNotCompleted'
-count=1`, clean detached worktree at master `2e33714e` vs the branch tip):

```
master 2e33714e : --- FAIL: TestDocAnchorsResolveInNormativeDocs (0.81s)
                  --- FAIL: TestAgent_WallclockCap_TruncatedRunIsNotCompleted (3.64s)
                  --- FAIL: TestAgent_StuckLoop_TruncatedRunIsNotCompleted (2.44s)
branch tip      : --- FAIL: TestDocAnchorsResolveInNormativeDocs (0.87s)
                  --- FAIL: TestAgent_WallclockCap_TruncatedRunIsNotCompleted (3.88s)
                  --- FAIL: TestAgent_StuckLoop_TruncatedRunIsNotCompleted (4.24s)
```

The two truncation tests fail on master with `status: in_progress` where they
expect `failed` — the engine-forced-stop close-out, untouched by this change
(this diff adds no `runend` call and no close-out path). The anchor failure is
the census above. `go test ./internal/agent/ -run
'TestSessionBudget|TestReframeTimeDimension|TestSystemPromptDensity' -count=1`
→ **ok 25.364s** on the branch.

**Prompt-density gate:** `go test ./internal/agent/ -run TestSystemPromptDensity -count=1` → **ok** (1.057s). It caps `assembleSystemPrompt()` at 4500 chars; every option-B teaching string was therefore placed in the tool-schema prose and the EPIC message, which the gate does not measure.

**Census commands (verbatim):**

```
find ~/.gopi/sessions -maxdepth 2 -name '*.raw.log' | wc -l                                   → 492
find ~/.gopi/sessions -maxdepth 2 -name '*.raw.log' -size +10M | wc -l                        → 231
find ~/.gopi/sessions -maxdepth 2 -name '*.raw.log' -newermt '-7 days' | wc -l                → 387
find ~/.gopi/sessions -maxdepth 2 -name '*.raw.log' -newermt '-7 days' -size +10M | wc -l     → 190
find ~/.gopi/sessions -maxdepth 2 -name '*.raw.log' -printf '%s\n' | awk …                    → >50M:117  10-50M:119  1-10M:130  <1M:126
```

**Interpretation.** 492 raw logs, 231 of them over 10 MB — **47% of all sessions
cross the 10 MB mark**, and 113 are over 50 MiB (largest 289,933,977 B = 276.5 MiB). In the last 7
days it is worse, not better: 190 of 387 (49%). This is the population the epic
describes: the 60-minute budget was prompt-only, so nothing refused a session
that kept going. The two acceptance sessions above are 1.06 MB and 0.14 MB —
they were *stopped* at the mark by the refusal rather than allowed to run to
hundreds of megabytes. The census is a **before** snapshot in the sense that it
measures sessions produced by code *without* the gate; a genuine after-population
needs days of runs with the gate live, which one session cannot manufacture
(Rule 27 — no synthetic load). Reported as it falls: the number is high, the
mechanism that was missing is now present at the beat where it was missing, and
the population-level effect is a future measurement, not a claim of this report.

## Files

- `internal/agent/loop.go` — `sessionBudgetStatus` (one measurement),
  `sessionBudgetReportFmt`/`EscalationFmt`/`RefusalFmt`,
  `sessionBudgetCeilingClause`, `sessionBudgetTimeDimension` (extended),
  `sessionBudgetCommitRefusal`, `budgetRemaining`, `sessionBudgetLine`
  (rewritten), `sessionBudgetRefusal`, `hasOpenTrackedItems`, the gate call in
  `commitTaskItem`, and `sessionBudgetRefusalEnv`.
- `internal/agent/reframe.go` — `reframeTimeRefusal`, `reframeTimeRefusalFmt`,
  the call in `handleTaskReframe`, and the ceiling clause in
  `reframeNudgeMessage`/`reframeDirective`.
- `internal/agent/sessionbudgetgate_test.go` — new, the deliverable-4 tests.
- `internal/agent/sessionbudget_test.go` — the pre-existing budget tests, still green.
- `devdoc/DOC-INDEX.tsv` — the manifest row for this doc (line 1692,
  `normative … path internal/agent/loop.go`), landed on this branch at
  `0b0e482e`. It is part of the deliverable: a class-driven corpus makes a new
  devdoc invisible until classified, and the tier choice was measured (see
  `## Not Done`).

## Acceptance (2) — the honest statement

The epic allows either shape here, and the answer is **both, in order**:

- **Acceptance run #2 (mis-sequenced, informative).** The first oversized-brief
  run called `task(action=reframe, verdict=DOABLE)` at `t+42s` (70% of the
  1-minute budget — **below** the 75% mark) and **before** any tracked list
  existed. The reframe was **accepted** (`VERDICT DOABLE: the epic is executable
  here`), and the model's own closing text says so: *"the call above was accepted
  instead, so that refusal did not occur"*. That is the gate behaving as
  designed on both preconditions — the mark was not crossed, and there was no
  tracked list to make the epic "still unexecuted" — but it is NOT evidence of
  the refusal, and it is reported as a miss, not as a pass.
- **Acceptance run #3 (preconditions made unskippable) — the refusal was
  OBSERVED.** The probe was rewritten to force the two preconditions (emit the
  tracked list first, then sleep past the mark, then reframe). The run's own
  session JSONL
  (`sandbox-reframeprobe3-8d09fb2a/.gopi/sessions/e01db8a9-2bca-4329-af7d-8558c0ea1c17/session.jsonl`)
  carries the refusal, quoted verbatim in `## Evidence` below: a DOABLE verdict
  for a 21-subsystem epic, refused at 181% of budget with the tracked list still
  open, naming both honest verdicts and latching nothing.

- **Two files on this branch were written by that probe run, not by this one**:
  `devdoc/reframe-probe-result.md` (`9e17e563`) and
  `devdoc/reframe-probe-session-report.md` (`789355b4`). They record what the run
  observed and are left in place (deleting another run's committed record is
  worse than explaining it), but their headline — "DOABLE accepted past the
  escalation mark" — is explained by the mis-sequencing above: the call landed
  at 70% with no tracked list, and its later `TOO_BIG` attempt was refused by the
  **pre-existing** duplicate-reframe guard ("already reframed this run"), which
  is not this gate. Measured from that run's JSONL: reframe calls at
  `t+42s` (DOABLE, no roadmap) and `t+10m06s` (TOO_BIG, 12-item roadmap); the
  `[TASK_LIST]` emission does not appear before the first call.

Either way, the **deterministic** proof of the time dimension is the unit-level
one (acceptance 3): `TestReframeTimeDimension` drives `handleTaskReframe` through
all six bands and asserts the refusal text, the no-latch property, and the
allowed cases. That test is green and is the load-bearing evidence; the live run
is the confirmation.
