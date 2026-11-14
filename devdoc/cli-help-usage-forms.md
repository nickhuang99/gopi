<!-- doc-class: normative -->
# CLI help: the three usage forms, and why `merge`/`worker` keep theirs

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> Task `01a12b7e-4388-7e36-8264-b59888dd8cde` (tags=bug, filed by the man-page
> episode's own Rule 41 sweep). Branch `task/01a12b7e43887e368264b59888dd8cde`.
> Evidence: `~/.gopi/sessions/01a12b7e-4388-7e36-8264-b59888dd8cde/artifacts/`.

## The reported defect, reproduced

Twelve published top-level verbs, and the `-h` pages use **three** shapes:

```
$ for v in chat config memory merge notify session task tool ui_state version web worker; do
    printf '%-12s %s\n' "$v" "$(./bin/gopi $v -h 2>&1 | grep -c '^Usage:')"
  done
chat         1
config       1
memory       1
merge        0          ← no standalone `Usage:` header
notify       1
session      1
task         1
tool         1
ui_state     1
version      1
web          1
worker       0          ← no standalone `Usage:` header
```

The consequence was measured, not theorized: `internal/mansource.UsageRows`
anchored on the `^Usage:` line, so it returned **zero rows** for `merge` and
`worker` — and the man generator then wrote `@@ NO USAGE BLOCK @@` into both
pages while the drift gate reported

> **Retired mechanism (merge verb removal, 2026-11-01).** The `gopi merge -h`
> invocations quoted in this document (here and at §"the reader was too narrow")
> are the RECORD of a gated failure line from 2026-10. The top-level verb was
> removed later, so that page no longer exists; what survives is the READER this
> page is about (`mansource.UsageRows`), still exercised by `gopi worker -h`.

```
`gopi merge -h` printed no usage block — the synopsis fact source is missing
```

which is a claim about the **page** that was really a claim about the
**reader**. Stating that precisely is the whole point of this document: the two
diagnoses imply opposite repairs.

## The three forms (the census, measured on the real binary)

| form | shape | pages |
|---|---|---|
| `header` | a bare `Usage:` line, rows beneath | config, memory, notify, session, task, tool, ui_state, web |
| `header-line` | the first shape sits ON the `Usage:` line (`Usage: gopi chat …`) | chat, version |
| `headerless` | no `Usage:` at all — an indented invocation block under the title | **merge, worker** |

The census is produced by `TestUsageFormCensusMatchesThePublishedPages`
(`cmd/agent/help_usage_form_census_test.go`), which spawns the built binary
through the contract suite's own `helpPage` helper — the same fact source the
man gate reads. The FORM is logged, never asserted: pinning "exactly two pages
are headerless" would freeze today's count and turn a legitimate change of shape
into a red build.

## Provenance: the headerless shape is DELIBERATE, not legacy

This was the question the row was filed to answer, and the answer decides the
repair. Both headerless pages were **written that way on purpose**, by an
operator-commissioned epic, and the shape is **gated**:

- **`workerUsageHead`** — `cmd/agent/main.go`, introduced by `9e24d228fd`
  (*"fix(cmd/agent): service help is capability-first, not a devdoc
  (worker/merger -h)"*, Qingzhe Huang, **2026-09-14 20:09:51 +0800**). Its
  message states the required shape verbatim: *"Both usage heads now follow one
  shape: title one-liner → invocations → Options → Environment → Notes. What it
  CAN do comes first; the why/how detail is collected in the trailing Notes
  section."* The constant's own doc comment repeats it and names the gate.
- **`taskMergeUsageHead`** — `cmd/agent/cli_tasks_merger_help.go`, the surviving
  merge surface's page, written to the capability-first shape by construction.
  Its predecessor `mergeUsageHead` lived in the now-deleted
  `cmd/agent/merge_run.go` and was introduced by `06438272b6`
  (*"feat(merge): add the foreground one-shot `gopi merge` command"* — the
  commit that ADDED a verb since removed, quoted by its own title,
  **2026-09-18 13:58:57 +0800**); that page and its constant left the gate's set
  on 2026-11-01.
- **The gate**: `cmd/agent/help_capability_first_test.go` —
  `hcfCapabilityFirstOffenses` requires an invocation line within the page's first
  `hcfSectionMaxLine+1` lines, with prefix `hcfInvocationPrefix` (`"  gopi "`),
  over the live `workerUsageHead` constant (the merge half was removed with the
  verb — see the arm's own comment, `help_capability_first_test.go:437`).

**Therefore the obvious normalization is a revert — but NOT for the reason a
first reading suggests, and the difference matters.** The claim "adding a
`Usage:` header turns the capability-first gate red" is **FALSE, measured
2026-10-11** (probe: `artifacts/hcf-probe.txt`): inserting a standalone `Usage:`
line above the invocation block leaves `hcfCapabilityFirstOffenses` at **0
offenses** for both pages. `hcfSectionMaxLine = 3` and the 0-based scan admits
`i <= 3`, so the invocation lands on line 3 (index 2→3) and is still inside the
bound. An earlier revision of this section asserted the red build; the
measurement above replaced the assertion (Rule 15 — a claim about a fact the
gate would decide is the gate's to state, and it stated the opposite).

**The reason the normalization is still wrong is the shape the two forms
actually are**, and the same probe measured it:

| page form | clause ① (invocation before prose) |
|---|---|
| `headerless` — merge, worker (2 pages) | **satisfied** |
| `header` (8 pages) | **satisfied — all 8**, once the three pages whose prose preceded their block were re-ordered (2026-10-12, task 01a12bb2) |
| `header-line` — chat, version (2 pages) | **satisfied — both**, once clause ① reads the header-line spelling as an invocation (the old reader did not, which is why they used to read as breaches) |

**The table above is HISTORICAL for the old clause, which is kept here because
the reversal it records is what the new one rests on.** The old clause was a
POSITION window (`first `"  gopi "` line at index <= hcfSectionMaxLine = 3`), and
its verdicts — `header` violated on 3 of 8, `header-line` violated on both —
were partly layout accidents and partly a reader that could not see the second
invocation spelling (a synopsis sitting ON the `Usage:` line). Clause ① is now an
ORDER relation (the first invocation, in EITHER spelling, must precede the first
prose paragraph), it is evaluated over all 12 published pages, and all 12 satisfy
it: `devdoc/help-clause1-order-relation.md` carries the measurement, the two red
proofs and the three page re-orderings.

So a `Usage:` header and the capability-first shape are **not two spellings of
one thing**: the header *separates the title from the invocations*, which is
exactly what the epic was created to abolish on these two pages. Giving
merge/worker a header would move them onto the form whose own gate refuses it in
3 of 8 cases (and in 2 of 2 for the header-line variant) — a revert of the
operator's decision whether or not a build goes red. Two facts keep that from
being an accident of the other ten pages: the ten are legacy, and **step 3 of
the man epic (`01a12b36-96c3-7e28-9121-dd28b209540d`) owns the help text and is
the row where that style question can be decided once**. A help-surface rewrite
here would also collide with that live sibling row.

So this row resolves as **"acceptable difference — harden the reader and freeze
the contract"**, and the difference is now *named* rather than *discovered*.

## What was landed

### 1. A real reader bug the hardening uncovered (not the reported one)

The headerless fallback shipped in step 2 (`cd0cf3a8f`) scanned the **whole
page** for the first indented `gopi <verb> ` row. Measured on a probe page
(2026-10-11) — a page that opens on its invocations and carries an indented
prose line in its Notes tail:

```
Notes:
  gopi thing --fast is the only spelling that skips the probe.
```

the reader returned that **sentence** as the usage block:

```
PROBE bodyRow rows=[]string{"  gopi thing --fast is the only spelling that skips the probe."}
```

The man SYNOPSIS of such a page would quote a paragraph instead of a synopsis —
the same class of defect the row is about, one layer in.

**Fix**: the headerless scan is now **bounded by the page's first section
heading** (`isSectionHeading`: unindented, ending in `:`; `Usage:` itself is the
block's own starting line and is not a bound). The invocation block is always
above the sections, because that is the shape the capability-first gate
enforces — so the bound is derived from an existing contract, not a new
convention. `internal/mansource/mansource.go` (`usageBlockStart`,
`isSectionHeading`).

Blank-line bound was **rejected**: the real `worker` page prints its next block
(`Options:`) with a single blank line after the invocations, so a blank-line
bound would cut the worker block short.

### 2. The reader's `Usage:` spelling is now exported

`usageHeader` → **`mansource.UsageHeader`**. The form census and the reader must
agree about what "this page has a usage block" means, and they live in two
packages; a second spelling of `Usage:` is exactly how the two would silently
disagree (Rule 13/14 — one definition, referenced everywhere).

### 3. The contract is frozen as a test

`cmd/agent/help_usage_form_census_test.go`:

- `hucUsageFormOffenses(help, verb)` — **the** predicate, a pure function so the
  red proof runs the same code the gate runs. Three clauses: the page yields
  usage rows; those rows name **this** verb; the block sits above the first
  non-`Usage:` section heading (the reader's own bound, asserted on the pages it
  reads).
- `TestPublishedHelpPagesShareTheUsageContract` — the gate, over the 12
  published pages via the exported `CliVerb*` constants (no bare verb spellings).
- `TestUsageFormCensusMatchesThePublishedPages` — measures and logs which form
  each page uses; fails only on `none`, and fails if the walk did not classify
  every page (a scan that found nothing must be distinguishable from a scan that
  never ran).
- `TestUsageFormPredicateIsNotBlind` — the red proof inside the suite: control
  (the real merge page reports nothing) plus three poisons — a page with no
  block, a page whose block sits below `Notes:`, and a block documenting another
  verb.
- `internal/mansource/mansource_test.go` gains
  `TestUsageRowsDoesNotReadATailProseRow` and
  `TestUsageRowsFindsNothingBelowTheSections` — the measured probe shape and its
  negative.

## Reproduction and red→green (all observed in this run)

**Reproduce the original defect** (reader's third form disabled — `return i` →
`_ = i` in `usageBlockStart`):

```
--- FAIL: TestPublishedHelpPagesShareTheUsageContract/merge  (0.01s)
    `gopi merge -h` yields no usage rows … the synopsis fact source is missing
--- FAIL: TestPublishedHelpPagesShareTheUsageContract/worker (0.01s)
    `gopi worker -h` yields no usage rows … the synopsis fact source is missing
```

— **exactly** the two verbs the row names, nothing else.

**And user-visibly, one layer up** (`make man-check` with the fallback off):

```
mancensus: man/gopi-merge.md  — STALE (regenerate with `make man`)
mancensus: man/gopi-worker.md — STALE (regenerate with `make man`)
```

**Green after restore**: all four suites below pass, `make man-check` exits 0,
and `mancensus` reports 13/13 pages "up to date" — i.e. the hardened reader
reproduces the shipped generated regions **byte for byte**, which is why no man
source changed in this commit.

## Consumer inventory — everything anchored on the `^Usage:` line

| consumer | anchor | affected by the headerless form? |
|---|---|---|
| `internal/mansource` `UsageRows` / `usageBlockStart` | `UsageHeader` + headerless fallback | **yes — the reader this row is about**; hardened here |
| `internal/standards/testdata/mancensus.go` (`synopsisBody`) | `UsageRows` | yes — inherits the fix; writes `@@ NO USAGE BLOCK @@` only on a page that truly has none |
| `cmd/agent/man_source_gate_test.go` | `UsageRows` | yes — inherits the fix |
| `scripts/replay-check.sh` `c6_usage` | `grep -m1 '^Usage: gopi chat \['` | no — `chat` is the `header-line` form |
| `cmd/agent/commands_test.go` | `strings.Contains(…, "Usage:")` on **slash** commands | no — chat slash commands, not CLI verbs |
| `cmd/agent/cli_verb_hint_guard_test.go` | `strings.Contains(out, "Usage:")` on the **top-level** page | no — top-level page prints a header |
| `internal/config/help.go` | **prints** `Usage:` (`renderSection("Usage:", …)`) | producer, not consumer |
| `cmd/agent/*_usage*.go` (52 usage constants) | publish the shapes | producers |

No consumer outside `internal/mansource` and its two dependents parses the usage
block by the `^Usage:` anchor. Nothing else needed changing.

## What would make this difference disappear (if it ever should)

Give a headerless page a `Usage:` header **and** move its first invocation to
line ≤ `hcfSectionMaxLine`: that WAS a change to the operator's capability-first
shape and belonged to the row that owns the help text (step 3,
`01a12b36-96c3-7e28-9121-dd28b209540d`), with the capability-first gate updated
in the same commit. This document is the record that the swap is a decision, not
a cleanup — the next reader should meet a reason, not a gap.

**RESOLVED 2026-10-12 (task 01a12bb2-3772-72b7-bb2a-a5a3e8fecf13).** The
`hcfSectionMaxLine` window that this section reasoned about is RETIRED, so the
question above no longer decides anything: clause ① is an ORDER relation (first
invocation, in either spelling, before the first prose paragraph), evaluated over
all 12 published pages, and every one of them satisfies it. The window survives
only as `hcfLegacyInvocationInWindow`, a test fixture that keeps the retirement
measurable (`TestClauseOnePositionIsOneLineWide`). See
`devdoc/help-clause1-order-relation.md`; the row this section's last paragraph
names (`01a12b36`, "help 瘦身") has since landed and is `completed`, so no live
sibling owns that call any more.

## Not done here (honest)

- **The help surface is not normalized** — deliberate, per the provenance above.
- **`make man-check` needs no regeneration** — verified: the hardened reader
  reads all 13 pages (including `merge`/`worker`) to the byte the files already
  hold (`mancensus: … — up to date`, 13/13).
- The census covers the **12 published top-level verbs**; group sub-pages
  (`gopi task <sub>` etc.) are out of scope by the oral manifest the man set
  itself uses.
