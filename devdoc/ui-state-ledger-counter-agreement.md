<!-- doc-class: historical -->
# The audit ledger's counters and its own entry list are now the same facts read twice

**Task:** 01a0e4f5 · **Branch:** `task/01a0e4f5f9cc787a9823040b8500c215` · **Date:** 2026-09-28

## The reported defect

`gopi ui_state --target chat --all --json`, on the real dump of session
`01a0e49e-6730-755a-a6ed-11dc0dfa5d96`:

```
audited=7  matched=6  disagreed=1  rejoined=1  repainted=0  lost_text=1
lines[0]  index=1  verdict.status="mismatch"  rejoined=true
          reason: "… WRAPPED across rows [10 11 12] … the text arrived …"
lines[1]  index=4  verdict.status="mismatch"  damage="wrap_artifact_or_loss"
```

Three contradictions in one ledger, and they are three faces of ONE structural
cause:

1. **two mismatch entries beside `disagreed: 1`** — the count did not describe the
   list;
2. **`rejoined: 1` naming no entry** — a counter with nothing behind it;
3. **`status="mismatch"` beside "the text arrived"** — one line with two verdicts,
   because the *status* and the *counting rule* were decided at different places.

## Root cause

`AuditAnswer` incremented `Matched` / `Disagreed` / `Rejoined` / `Repainted` at five
separate points of the auditing loop, and built the `AuditLine` entry describing the
same line at six. The rejoin branch incremented `led.Rejoined++` and
`led.Matched++` while appending an entry whose `Verdict.Status` was still
`CompareMismatch` (the status `CompareTranscriptLine` returned before the wrap was
joined; only the ledger's own special case knew the line had arrived).

Nothing in the loop could notice, because **no code ever asked the numbers and the
list to agree**. The counters were a second, hand-maintained copy of facts the list
already implied — and they had drifted.

## The fix

The counters are **derived from the entry list** in exactly one place,
`(*AuditLedger).auditCountsFromEntries`:

| counter | rule |
|---|---|
| `Repainted` | entries whose damage is the ROW's (`repaint_tail` / `damage_to_row`) |
| `Rejoined` | entries whose verdict status is `CompareRejoined` |
| `Disagreed` / `LostText` | entries whose class says the text is NOT on the grid (`Damage.DamageArrived()`) |
| `Matched` | `Audited − Disagreed` — closes by construction, never by two increments that happen to agree |

So a counter can no longer disagree with the list: they are the same facts.

**One line, one status, one entry.** Two supporting changes make that literal:

- **`CompareRejoined` is a real status** (`compare.go`), not a flavour of
  `mismatch`. `CompareTranscriptLine` asks the grid's own wrap instrument
  (`compareSequence`) and, when it reassembles the line, sets
  `Status=CompareRejoined`, `Damage=DamageTextWrapped`, `InGrid=true` and the rows
  it joined. The **status IS the counting rule**, on both faces at once — the JSON
  ledger and `--compare` — because they read the same verdict.
- **the rejoin is NAMED**: `CompareVerdict.RejoinRows` (and `AuditLine.RejoinRows`)
  carry the rows, and the sentence is generated from them (`rejoinReason`), so
  `rejoined: 1` is checkable rather than a number to trust.

**A third defect surfaced while doing it, and it was the same shape one level down.**
`AuditAnswer` passed `CompareTranscriptLine` the *raw* transcript lines as the
authority side, while comparing against the *drawn* text (markers removed) — i.e.
it asked a different question from the one the ledger was counting, so the two could
name one line two ways. It now passes the drawn lines. (`--all` is unchanged for
every other case, verified: the branch's audit of the report's real dump prints the
same `audited/matched/lost_text/outcome/exit` as master's.)

**A fourth: `Damage.DamageArrived()` was missing two of its own classes**
(`damage_to_row`, `emphasis_unpainted`). The ledger's `repainted` count treated
`damage_to_row` as an arrival while the ONE predicate that says what a class MEANS
called it a loss — invisible until the counters were derived from the classes, and
fixed at the predicate rather than worked around at the call site.

**The width proxy is gone from the arrival decision.** `wrapOnlyFits` (a width test
used as the arrival answer) became `wrapWouldFit` (a gate for *asking* the
instrument). `text_wrapped` is now decided by the grid's own join — the class whose
whole claim is "the grid reassembled it" can no longer be decided by measuring the
line instead of the grid.

## Not Done (honest)

- **The `--compare` text face prints `rejoin_rows=[10 11 12]` without a word
  around it** (the ledger's JSON is self-describing; the one-line text rendering
  reads as a raw Go slice). Cosmetic, and deliberately not papered over here: the
  number is the fact and the reason sentence beside it says "WRAPPED across rows".
- **`lost_text` equals `disagreed` again on every input exercised** (every
  disagreement class today is a loss). It stays a separate counter because a
  disagreement can also be a line whose arrival claim the audit could not confirm;
  that state is now unreachable through `CompareTranscriptLine` (an unconfirmed
  wrap is `mismatch` + a loss class), so the two counters are equal *for now* — the
  separation is kept for the case that motivated it, not because an input shows it.
- **Two pre-existing failures are untouched and unrelated**: `internal/agent`'s
  `TestDocAnchorsResolveInNormativeDocs` (doc-anchor drift in
  `devdoc/chat-answer-gold-is-not-a-regression.md` and `devdoc/cov-agent-cr.md`,
  files this change never touches) fails identically on a clean checkout of this
  branch's base — measured in a scratch worktree at the same commit.

## Landed onto master (merge `01a0e54c`, 2026-09-28)

This branch sat **1 ahead / 24 behind** master, and master had landed the same wrap
family **independently** (`842e5f34 fix(ui_state): --compare asks the wrap join, and
the prefix is the LINE's`) with a different spelling. Three files conflicted
(`internal/uiclient/{audit,compare,damage}.go`); the merge takes **master's wrap
structure + this branch's counter-derivation**, so read the two lists below before
reading the paragraphs above — several of them describe the pre-merge branch only.

**Unchanged, and this is what the merge was for — master did NOT have it:**

1. **The derivation.** `(*AuditLedger).auditCountsFromEntries` — the recipe table
   above — is the landed code, and it is the actual fix for the reported defect.
   Master still incremented `Matched`/`Rejoined`/`Repainted`/`Disagreed`/`LostText`
   at five points of the auditing loop; those increments are **gone**, replaced by
   one pass over `AuditLine`s read after the loop closes.
2. **`Damage.DamageArrived()`** names `DamageToRow` and `DamageEmphasisUnpainted` as
   arrivals. **This was the branch's alone** — master (verified:
   `git show 17c300a7:internal/uiclient/damage.go`) still read
   `return d == DamageNone || d == DamageTextWrapped || d == DamageRepaintTail`, the
   two-class omission this branch fixed. The merge takes the branch's spelling, and
   that is load-bearing for the derivation: with master's three-case predicate the
   `default` arm of `auditCountsFromEntries` would charge a `damage_to_row` entry as
   a disagreement, breaking `repainted`/`disagreed` on the very class the ledger has
   a test for (`internal/uiclient/audit_repaint_test.go`).
3. **The drawn-lines authority** (`AuditAnswer` compares against `auditDrawnLines`,
   not the raw transcript lines) — present on both sides.
4. **The branch's own regression test and the real-dump result** — see below.

**Different in the merge, and why (each is a MERGE NOTE in the source):**

- **`CompareRejoined` is declared but assigned by nothing.** Master names a wrap
  `CompareMatch` + `CompareVerdict.Rejoined`; the branch named it its own status.
  Two vocabularies for one arrival is the drift this whole change removes, so master's
  is kept for the one-line face. The constant survives because **the ledger's entry
  uses it**: `AuditAnswer` relabels the wrap entry's `Verdict.Status` to
  `CompareRejoined`, which is what makes the `rejoined` count read off the entry list
  and what the branch's test inspects (`verdict.status=="rejoined"`). Under the
  no-relabel design the branch's test fails — "rejoined=1 but 0 entries rejoined".
- **The rows are `WrapRows` / `wrap_rows`, not `RejoinRows` / `rejoin_rows`.** The
  branch field would have been set by *nothing* after the merge (master fills
  `WrapRows` in the one place the join is asked), so it was deleted rather than left
  as a dead JSON key that printed `rows []`.
- **`wrapWouldFit` is deleted (the width gate).** The branch gated the join on the
  line being at least as wide as the grid and asked it *inside* that gate; master
  asks `compareSequence` unconditionally and keeps the width test only inside
  `classifyLostLine`, where it names `cjk_wrap` / `wrap_artifact_or_loss` when the
  join did not reproduce. Master's is strictly wider in what it **asks** and identical
  in what it **accepts**, so the gate could only ever have suppressed an arrival —
  the defect being fixed. The branch's second, gated probe below master's
  unconditional call was unreachable and was dropped with it.
- **The sentence renderer is `wrapArrivalReason`, not `rejoinReason`** (one renderer
  for both faces; the branch's copy was byte-identical in intent and became dead).
- **The branch's `cmd/agent/cli_uistate.go` and `cli_uistate_compare.go` edits are
  reverted to master.** `--compare`'s `rejoin_rows=%v` would have printed `rows []`
  once the field was deleted, and `uiStateCompareOutcomeOf`'s `CompareRejoined` arm
  was dead. Consequence, stated honestly: **the `--compare` text face does not print
  the rejoin rows** (its JSON does, via `wrap_rows`), so the `## Not Done` note above
  about a bare `rejoin_rows=[…]` no longer describes the landed code — the CLI
  surface now says only what master's does.

**The result on the real dump is unchanged from the table above** (re-measured on the
merged tree, same session and command):

```
audited=7 matched=6 disagreed=1 rejoined=1 repainted=0 lost_text=1 outcome=disagreed  exit=3
  line 1  status=rejoined  damage=          wrap_rows=[10 11 12]  (was: status=mismatch, no rows)
  line 4  status=mismatch  damage=wrap_artifact_or_loss
```

`--compare` on that same wrapped line now reports `outcome=agreed`, `match=true`,
`prefix=102` of the line's 102 runes — the wrap is an arrival on **both** faces, which
is the single claim the three contradictions above reduce to.

## Verification

- **Build:** `go build ./...` → OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- **Tests:**
  - `go test ./internal/uiclient/` → ok (the audit/compare/damage packages, including
    every pre-existing repaint/wrap/loss control).
  - `cmd/agent`: `-run 'TestUIStateAudit_LedgerCountersAgreeWithTheEntryList|TestUIStateAudit_TextFacePrintsTheSameCountersAsTheList'` → PASS.
  - Branch-scoped run of `./cmd/agent/ ./internal/uiclient/ ./internal/agent/ ./internal/lineedit/ ./internal/standards/`.
- **Ran (the surface, verbatim — Rule 31):**

  ```
  bin/gopi ui_state --target chat --all --json \
    --session /home/user/.gopi/sessions/01a0e49e-6730-755a-a6ed-11dc0dfa5d96/session.jsonl
  ```

  The canned-dump regression case drives the same verb with a wrapped line painted
  through the production band on a real 117×26 pty (`auditFixture`).

- **Result — the report's own run, master vs branch (both exit 3):**

  | | counters | `len(lines)` | entry 1 |
  |---|---|---|---|
  | master | `audited=7 matched=6 disagreed=1 rejoined=1 lost_text=1` | 2 | `status="mismatch"`, `damage=""`, no rows, sentence says the text arrived |
  | branch | `audited=7 matched=6 disagreed=1 rejoined=1 lost_text=1` | 2 | `status="rejoined"`, `damage="text_wrapped"`, `rejoin_rows=[10,11,12]`, sentence says the text arrived |

  `disagreed: 1` now equals the list's one loss entry (index 4), the arrival at
  index 1 is no longer counted as a disagreement, and `rejoined: 1` names a line and
  its rows. The exit code is unchanged (3) because a line really was lost — **the
  loss rule was not widened**.

- **RED→GREEN (the regression case on the base revision).** The same test file, run
  against a clean detached worktree of this branch's base commit, FAILS:

  ```
  --- FAIL: TestUIStateAudit_LedgerCountersAgreeWithTheEntryList
      cli_uistate_ledger_agreement_test.go: "1 entr(y/ies) still carry
      verdict.status=\"mismatch\" while their class says the text arrived"
      … ledger: {"audited":1,"matched":1,"disagreed":0,"rejoined":1,
                 "lines":[{"index":0,"verdict":{"status":"mismatch", …
                 "damage":"text_wrapped",
                 "reason":"… WRAPPED across rows [21 22] … the text arrived …"}]}
  ```

  On the branch the same case passes. The fixture asserts it really produced a wrap
  (`rejoined != 0`) and a non-empty audit before asserting anything else, so it can
  never pass by not exercising the case.
