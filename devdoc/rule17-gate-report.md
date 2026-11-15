<!-- doc-class: normative -->
# Task Report: Rule 17 (test env isolation) gate — the agent-suite pin is an escape, not baseline debt

**Epic**: make master's Rule 17 gate honestly green; record the ownership finding.
**Branch**: `docs/rule17-gate-report` (worktree `~/gopi-rule17-report`, off master `8447c58`).
**Verdict on the briefing's premise**: the *red* was already fixed in master; the *dishonest accounting* was not.

---

## 1. Root cause (ownership: **master**, pre-existing — no branch involved)

The briefing described "4 NEW violations in `internal/agent/testmain_isolation_test.go:76-79`".
That description is right about the symptom and wrong about the cause:

| step | commit | what it did |
|---|---|---|
| 1 | `6093c76` (master) | rewrote **comments only** in `internal/agent/testmain_isolation_test.go` (+24 lines). The four statements (`os.Setenv(memoryDBEnv, …)`, `os.Setenv(memoryDBEnvLegacy, "")`, `os.Unsetenv(tools.TaskWrapperEnv)`, `os.Unsetenv(tools.TaskChildSessionEnv)`) inside `pinSuiteLedgerEnv` moved `:62-65` → `:76-79`. |
| 2 | same commit | **never touched `internal/standards/baseline.go`**, whose entries are line-keyed. Result: 4 **STALE** entries (`:62-65`, matching nothing) + 4 "**NEW**" hits (`:76-79`, the *same four statements*). |
| 3 | `dca15ff` (master, merged via `feat/style-sweep`) | re-pointed the 4 entries `:62-65` → `:76-79`. Master `8447c58` therefore measures **Rule 17 = 112 hits / 105 baselined / 7 escaped / NEW=0** — green. (105, not 100: the baseline list holds 100 *entries* but covers 105 *hits*, because 5 of the baselined lines carry two env calls each — see the measurement note in §5.) |

Identity proof (not an inference): `git show 6093c76^:internal/agent/testmain_isolation_test.go`
carries the same four statements at `:62-65`, inside the same function, with the same
argument expressions that now sit at `:76-79`. The diff of `6093c76` is comments and blank
lines only. Nothing about a branch or a fork is involved — the gate went red **inside master's
own commit**.

So the epic's deliverable 1 (re-point the entries) was **already satisfied by `dca15ff`**, and
re-doing it would have been a no-op commit. What remained was:

- the entries were restored in their **weakest possible form** — a line-keyed permit that
  breaks again on the next comment edit, and that reads as *legacy debt* even though the
  sites are consciously-correct code; and
- **no report existed** anywhere for the incident (this is the first one; the only earlier
  mention is `devdoc/style-sweep-20260912.md` §10.5 noting master's Rule 17 was still red at
  `805b313`).

## 2. Why a mere re-point is the wrong end state, and why the escape is the right one

Rule 17's gate has **two** accounting channels, and they are deliberately different
(`internal/standards/gate.go`, the `gateClass` doc):

- **baseline** = "the linter was told to be quiet about this line" — an exact `file:line`
  permit, line-keyed, and a stale permit is itself a gate error;
- **escape** = "this line is correct and here is why" — `// env-leak-ok: <reason>` on the
  offending line (or in the contiguous comment block above it), and escaped hits stay
  **visible** in `TestStandardGateDataReport`.

The four sites are the *textbook* escape case:

1. **`t.Setenv` is structurally unavailable.** `pinSuiteLedgerEnv` is called from `TestMain`
   before `m.Run()`; there is no `*testing.T` in scope, so the prescribed remedy cannot be
   written at all. (`internal/tools/testmain_test.go:107` is the same class of site.)
2. **The value must outlive the test.** The pin exists so the ledger path and the dropped
   wrapper-child identity reach the **child processes** the suite spawns, which read
   `GOPI_MEMORY_DB` at *exec* time; `t.Setenv`'s restore-at-test-end semantics cannot express
   that. This is the very reason the gate's own baseline header gives for keeping
   child-process `os.Setenv` sites.
3. **The escape channel is genuinely applicable and genuinely in use.** Seven sites already
   live this way (`cmd/agent/dispatch_test.go:240,242,256,259,263`,
   `internal/tools/memory_sync_bidir2_e2e_test.go:346,348`), so this is not a novel or
   invented mechanism.

The escape also beats the baseline on every axis the project's own gate docs care about:
it **shrinks** the allow-list (100 → 96 entries, the ratchet only ever goes down), it is
**line-shift-proof** (a comment edit above `pinSuiteLedgerEnv` cannot turn it red — exactly
the failure mode that produced this incident), and it keeps the sites **visible as reviewed
true positives** rather than hidden as debt.

**No gate weakening**: the predicate is untouched, `maxBaselineEntries` is untouched, no file
glob replaced an entry, and every escape carries its own reason.

## 3. What changed

### 3a. `internal/agent/testmain_isolation_test.go` (test-only)

- Each of the four statements gained a reasoned `// env-leak-ok:` escape stating the
  TestMain/no-`*testing.T`/child-inherits-it reason (the two `os.Setenv` lines keep their
  pre-existing `//stylegate:ignore` reason as well; the two `os.Unsetenv` lines had none).
- The `pinSuiteLedgerEnv` doc comment gained a paragraph explaining why these four are an
  escape and not a baseline entry — the reason lives with the code, which is the point of the
  escape channel.
- The two `os.Unsetenv` sites previously carried **no** justification at all; they now do.

### 3b. `internal/standards/baseline.go`

- Deleted the four `internal/agent/testmain_isolation_test.go:76-79` entries (100 → **96**,
  list stays sorted, no widening).
- Recorded the shrink and the incident in the `testEnvBaseline` header.
- **Corrected a false claim the header had been making.** It said "Each site carries a
  `//stylegate:ignore` reason". Measured across the remaining 96 entries the count depends on
  the search window, so no single "annotated" figure is defensible: **31 carry the reason on the
  offending line itself, 33 within ±1 line, 43 within ±3, 47 within ±6** — i.e. **53 entries have
  no reason even 3 lines away**, and the figure only grows with the window (an honest statement is
  the whole curve, which is what the header now carries). The unannotated bulk is mostly a bare
  `os.Unsetenv(...)` — which leaks
  exactly as far as `os.Setenv`: the test is restored by the *next* test's `t.Setenv`, never by
  its own. They are unreviewed, not deliberately-kept. The header now says so, and also notes
  that the *rendered* per-gate reason (`gates()` in `gate_test.go`) describes the list as
  "hand-rolls the save/restore that t.Setenv does", which does not fit the child-process pins
  either — the two reasons disagree, and both read green.
  This is the same failure mode the epic was opened to fix (a green gate whose paperwork lies)
  found one level deeper, in a string this task had to touch anyway. Ownership: the false claim
  was introduced by master commit `d6af8fd` ("fix(standards): prune the baselines the style sweep
  paid off", 2026-09-11) — again master, again a commit that edited the paperwork without
  re-measuring the list, i.e. the same defect class as `6093c76`.

### 3c. Deliverable 2 status: **accepted and implemented** (was "rejected" in my first pass)

My initial reading was that the escape was inapplicable because I assumed the marker *had* to
sit in a comment block above the line. `linesAbove()` also accepts the marker on the offending
line itself, which is how the seven existing escapes work — so the channel applies here, and
the epic's condition ("if and only if the gate's escape channel genuinely applies and shrinks
the baseline") is met on both counts. Recorded here because the rejection was wrong and the
reasoning matters more than the flip.

## 4. Not Done

- **No product code touched** — no change outside the two writable surfaces (constraint honoured).
- The other 96 `testEnvBaseline` entries were **not** re-reviewed. The measurements in §3b show
  work is owed here (53 carry no reason even 3 lines away, and the rendered reason does not fit
  them), but each needs
  an individual read of its surroundings and its own escape-or-convert decision. Deliberately
  left as a follow-up rather than swept blind — a 53-site reclassification on an argument this
  report has only just established would be exactly the unexplained churn the constraints
  forbid. The natural next task: re-review the entry list against the two classes named in the
  header (child-process pin vs. genuinely hand-rolled save/restore) and convert the ones that are
  suite-scoped.
- `make fmt` was not run (forbidden); `gofmt -l` on the two touched files reports clean.

## 5. Verification

Commands run in the worktree `~/gopi-rule17-report` (master `8447c58` + this change):

```
$ gofmt -l internal/standards/baseline.go internal/agent/testmain_isolation_test.go
(no output — clean)

$ go test ./internal/standards/ -run TestStandardGates -count=1 -v
    Rule 17 (test env isolation): 112 total hits, 96 in baseline, 16 new/gated
--- PASS: TestStandardGates (4.17s)
    (0 "NEW violation" and 0 "STALE baseline entry" messages — the gate's two failure texts.
     Both are emitted by enforce(); STALE is computed independently, by iterating the baseline,
     so 0 STALE is a real measurement, not an artefact of the NEW count.)

$ go test ./internal/standards/ -count=1
ok  github.com/the operator/gopi/internal/standards  7.796s
    (whole package: TestStandardGates, TestStandardGateDataReport,
     TestBaselineReasonsAreCarried, all red-proofs — PASS)

$ go test ./internal/standards/ -run TestStandardGateDataReport -count=1 -v
    Rule 17 (checklist 5.2): hits=112 baselined=101 escaped=11 NEW=0
      escape: cmd/agent/dispatch_test.go:240, :242, :256, :259, :263
      escape: internal/agent/testmain_isolation_test.go:86, :87, :88, :89   <- the four converted sites
      escape: internal/tools/memory_sync_bidir2_e2e_test.go:346, :348
    Rule 16/18/19/20/21: NEW=0
--- PASS: TestStandardGateDataReport (2.30s)
--- PASS: TestBaselineReasonsAreCarried (0.00s)

$ go test ./internal/agent/ -count=1
ok  github.com/the operator/gopi/internal/agent  98.981s     (TestMain pin still live:
                                                      TestSuiteLedgerPin_IsActive PASS)
```

**The line shift the conversion absorbed — this task reproduced it.** The change *inserts* a
10-line doc comment above `pinSuiteLedgerEnv`, which moves the four statements from `:76-79`
(the lines master's `dca15ff` had just re-pointed the baseline to) to `:86-89`. Had they stayed
as line-keyed permits, this very commit would have produced 4 STALE + 4 NEW entries — a
one-commit replay of the `6093c76` incident that opened this epic, with my own commit as the
author. Because they are escapes, the shift is absorbed: the hits moved and the accounting did
not change shape. That is the empirical case for the escape over the re-point, demonstrated in
the same change rather than argued.

Before/after for Rule 17 — **entry** counts (the baseline list) and **hit** counts (what the
gate classifies) are different numbers here, so both are stated:

| | baseline entries | live-baselined hits | escaped hits | total hits | NEW | STALE |
|---|---|---|---|---|---|---|
| master `8447c58` | 100 | 105 | 7 | 112 | 0 | 0 |
| this branch | 96 | 101 | 11 | 112 | 0 | 0 |

The entry/hit gap is 5 in both rows — **not an accounting bug**: 5 baselined lines carry two
env calls each, so one `file:line` entry covers two hits. Verified by listing the baselined
lines that mention `os.Setenv`/`os.Unsetenv` twice — exactly 5, all of the shape
`t.Cleanup(func() { os.Unsetenv("GOPI_TASK_NOTIFY_CMD"); os.Unsetenv("GOPI_TASK_NOTIFY_EVENTS") })`
in `internal/tools/memory_sync_{claim,delta}_test.go`. `enforce()` compares entry keys against
hit keys, so a duplicate line is legal and stays green. **Measurement trap for the next agent:
the data report's `baselined=` figure counts hits, while `len(g.baseline)` counts entries —
comparing the two produces a mysterious 5-entry "gap" that looks like a bug and is not one.**

The four sites are now accounted for as escapes at `:86-89` (moved from `:76-79` by the
doc-comment insertion — which is exactly what an escape is immune to, and what this change
demonstrates on itself).

**A false alarm to be aware of when reporting these numbers (reported, not worked around).**
`TestStandardGates` logs "112 total hits, 96 in baseline, 16 new/gated"
(`gate_test.go:88-89`), where the third figure is *derived* as `countHits(hits) -
len(g.baseline)` — it is arithmetic, not a measurement of violations, and it mixes the two units
the paragraph above separates (112 hits − 96 entries → "16", where the real split is 101
baselined hits + 11 escaped + 0 unannotated). It therefore prints
"16 new/gated" on a run where `enforce()` raises zero errors, and it did so *before* this change
too. On this gate the number cannot reach 0 without emptying the baseline entirely, so its "new"
reading is meaningless here; the authoritative figures are `enforce()`'s error count (NEW/STALE)
and `TestStandardGateDataReport`'s `classifyAll` split (baselined/escaped/unannot). Fixing that
log line is a code change to `internal/standards/gate_test.go`, outside this epic's writable
surfaces — flagged for master instead of patched.

### `make test` (the epic's third gate) — green, run in the worktree on the committed change

```
$ make test          # go test ./...
ok  github.com/the operator/gopi/cmd/agent          94.157s
ok  github.com/the operator/gopi/internal/agent    116.966s
ok  github.com/the operator/gopi/internal/config     0.164s
ok  github.com/the operator/gopi/internal/standards 14.093s
ok  github.com/the operator/gopi/internal/stylegate  8.404s
ok  github.com/the operator/gopi/internal/tools    266.334s
ok  github.com/the operator/gopi/testsuite           2.113s
... (22 packages total, all ok; 3 packages report [no test files])
MAKE_TEST_EXIT=0
```

Constraints re-checked on the commit range (`git diff --name-only master...docs/rule17-gate-report`):
`devdoc/rule17-gate-report.md`, `internal/agent/testmain_isolation_test.go`,
`internal/standards/baseline.go` — the two writable surfaces plus the report, **no product code**.

## 6. Operational note (affects how to re-run this)

The worktree was **swept twice mid-task** by environment cleanup
(`~/workspace/gopi-rule17-report` and its branch simply disappeared between tool calls, before
any commit existed — nothing was lost). It now lives at `~/gopi-rule17-report`. If a fresh
worktree vanishes here, that is this environment's GC, not a git error: re-create it and note
that uncommitted work is the only thing at risk.

## 7. Recovery

- Branch: **`docs/rule17-gate-report`** (worktree `~/gopi-rule17-report`, shared object store —
  the commit is already visible from the main repo).
- `git show docs/rule17-gate-report:devdoc/rule17-gate-report.md`
- `git log --oneline -3 docs/rule17-gate-report`
