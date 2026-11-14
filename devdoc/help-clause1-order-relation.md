<!-- doc-class: normative -->
# clause ①: from a POSITION window to an ORDER relation

Task `01a12bb2-3772-72b7-bb2a-a5a3e8fecf13`, converging the follow-up row filed
by `01a12b9f-9736-778a-811a-52ed659b5dcd` (whose record is
`devdoc/help-clause1-position-boundary.md`, class `historical`).

## The claim that was there before

`hcfCapabilityFirstOffenses` clause ① asked: *is there a line carrying
`hcfInvocationPrefix` (`"  gopi "`) among the first `hcfSectionMaxLine+1` lines?*
`hcfSectionMaxLine = 3`, so the window was `index <= 3`. It was evaluated on two
constants (`workerUsageHead`, `mergeUsageHead`).

## What was measured (real binary, 2026-10-12, base `85de27d4e`)

`artifacts/pages-12-baseline.txt` (12 pages, verbatim), summarised in the table
below. Two independent defects, each fatal on its own:

**(a) A blank line decided the verdict.** Four pages that satisfied the window —
`config`, `memory`, `task`, `web` — put their first invocation on 0-based line
**3** = `hcfSectionMaxLine` **exactly**: zero headroom. `merge`, `notify`,
`worker` had one line. `TestClauseOnePositionIsOneLineWide` pins it: the same
page, one blank line inserted after the title, flips.

**(b) The window did not read the second invocation spelling.** `chat` and
`version` print their synopsis ON the `Usage:` header (`Usage: gopi chat …`), so
`"  gopi "` never matched and the predicate reported "no invocation at all" for
pages that print one on line 1. Measured, that is why the old census could not
tell "the page breaches" from "the reader did not know the page's form".

| verb | form | first invocation (0-based) | old window verdict | order verdict |
|---|---|---|---|---|
| chat | header-line | 0 | BREACH (reader missed it) | OK |
| config | header | 3 | OK @ boundary, zero headroom | OK |
| memory | header | 3 | OK @ boundary, zero headroom | OK |
| merge | headerless | 2 | OK | OK |
| notify | header | 2 | OK | OK |
| session | header | 6 | BREACH | OK |
| task | header | 3 | OK @ boundary, zero headroom | OK |
| tool | header | 7 | BREACH | OK |
| ui_state | header | 11 | BREACH | OK |
| version | header-line | 0 | BREACH (reader missed it) | OK |
| web | header | 3 | OK @ boundary, zero headroom | OK |
| worker | headerless | 2 | OK | OK |

**The old census's own log line, before this change**: `satisfied=7 breached=5 of
12 (breaching: chat session tool ui_state version; 4 satisfy it with ZERO lines
of headroom, i.e. exactly on the hcfSectionMaxLine bound)`.

## The re-specified predicate

Clause ① now tests the relation it names, over the whole page, with no window:

```
first invocation line (either spelling)  MUST BE BEFORE  first prose paragraph
```

Three functions, one definition each, shared by the gate and the census:

| function | what it decides |
|---|---|
| `hcfFirstInvocationLine` | the page's first line carrying `"  gopi "` (a row) **or** `"Usage: gopi "` (a header-line synopsis, derived from `mansource.UsageHeader` + `gopiProgramWord`, never re-typed) |
| `hcfFirstProseLine` | the page's first PROSE paragraph, or −1 |
| `hcfLegacyInvocationInWindow` | the RETIRED window form, kept as a fixture so the retirement is measured rather than asserted |

**The narrow definition of "prose line" is the load-bearing decision.** One line
qualifies only when it is: non-blank, unindented, not an invocation in either
spelling, not a bare section label (`Usage:`, `Flags:`, …), and terminated like a
sentence (`.`, `:`, `—`, `→`, `…`); and it is not the page's first non-blank line
(the title). The trap this avoids is measured: a WIDE definition ("the first
non-empty line that is not an invocation") flags `merge` and `worker`, both of
which are already capability-first — the flag is an artefact of a missing blank
line between their invocation block and a trailing note, not of their shape. A
rule that refused those two would be measuring the presence of a blank line,
which is the defect being repaired.

## Where the clause is now enforced

| surface | what it does |
|---|---|
| `TestClauseOneServesEveryPublishedPage` (`help_capability_first_test.go`) | **the gate**: walks `publishedVerbHelpPages` through `helpPage()` (the real binary, Rule 31) and fails per verb |
| `TestUsageFormCensusCarriesClauseOne` (`help_usage_form_census_test.go`) | **the census**: the same judgement, logged per page, plus the set-integrity and non-vacuity floors |
| `TestClauseOneIsAnOrderRelation` | **the red proof**, both directions and both boundaries |
| `TestClauseOnePositionIsOneLineWide` | **the red proof for the retirement**: the legacy window flips on a blank line, the shipped order predicate does not |

**One assertion was replaced, not silently dropped.** The old census carried
`if breaching == 0 { t.Error(...) }`. That floor was correct for a clause the
shipped pages mostly did not satisfy; under a clause that is now a GATE over the
whole set it would assert that the gate FAILS — the one thing a green build may
never require. The pin it carried is kept and moved: the census asserts the walk
is whole (≥ 12 pages), that every page it counts as breaching is really refused
by `hcfCapabilityFirstOffenses`, and reports how many pages the ORDER clause
accepts while the RETIRED window would refuse them (2 today) — a statement about
the two tests rather than about the pages, so it keeps its teeth after any
re-layout.

## The page edits that made the gate green

The gate is only useful if the pages satisfy it, so three pages moved (no text
deleted, the same words in the other order):

| page | change |
|---|---|
| `cli_session.go` | the opening "A session is one run of this binary …" paragraph moved DOWN into the closing prose block (it is a definition of the noun, not the first thing a reader needs) |
| `cli_uistate.go` | the `Usage:` block moved above the "One call, zero pixels, zero vision" paragraph; the WHAT IT IS / WHAT IT IS NOT lines follow the block |
| `cli_tool.go` | the `Usage:` block moved above the "Every command below is reached ONLY as …" one-door paragraph |

Line counts are unchanged on `session` (71) and `tool` (43); `ui_state` went
160 → 160 (`wc -l` reports the same page length; the two constants were
re-ordered, not re-written). No usage budget was raised — see the budget table in
`cmd/agent/cli_usage_budget_test.go` for the caps these three pages sit under.

## Evidence

- `artifacts/pages-12-baseline.txt` — all 12 `-h` pages verbatim from the real
  binary (the table above is derived from it, not from prose).
- `artifacts/clause1-order-verdict.txt` — the ORDER judgement re-derived page by
  page from the real binary, with the invocation line, the prose line and the
  verdict. It agrees with the suite's census output.
- `artifacts/order-predicate-probe.txt` — the probe that fixed the narrow prose
  definition (the wide form's false positives on `merge`/`worker`).

## Not done here (honest)

- **The three page edits are re-orderings, not slimming.** Where the prose went
  is where the page already kept its other prose; no paragraph was deleted, and
  no budget was lowered. Turning `help 瘦身`'s freed lines into a further
  reduction is the owning row's business.
- **Group sub-pages (`gopi task <sub> -h`, `gopi tool <command> -h`) are out of
  scope**, exactly as the usage-form census already declares: the clause is
  evaluated on the 12 published top-level pages the man set covers. A sub-page
  added to that set is measured the day it appears; a sub-page deliberately
  outside it is not silently "compliant".
- **The clause says nothing about how far down the invocation block sits**, only
  that it precedes prose. That is deliberate: a page that leads with its
  invocations and then runs long is the length question, owned by the usage
  budgets, not by this clause.
