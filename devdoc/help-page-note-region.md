<!-- doc-class: normative -->
# devdoc: the `-h` page note REGION — a pointer must resolve, never a phrase match

**Class**: a published `-h` page promises a note it does not carry.
**Decision** (2026-10-13, task `01a13508-448b-78cc-ba4b-7ffe96f4d23e`): a page that points
FORWARD at a note must carry that note inside a **marked, published REGION**; the guard is a
RESOLUTION against that region, never a match on the sentence.

## The defect, and why the first instrument did not see it

`TestAtomPageCarriesNoDanglingNotePointer` (`cmd/agent/cli_tasks_merger_help_atoms_test.go`)
states the property in its own doc-comment — *"a pointer into the page must land on something"* —
and implements one phrase:

```go
const pointer = "note below"
if strings.Contains(page, pointer) && !strings.Contains(page, "THE IMAGE SELF-CHECK") { … }
```

Measured (task `01a13496`, reviewed at `6ef428c16`): re-spell the same promise as
*"see the self-check note just under this row"* and the guard stays **GREEN**. The guard covers
one SPELLING of the defect, so the class walks around it — and a live instance was already
standing outside its reach: `gopi ui_state -h` line 63 said *"see the note below"* while the
160-line page contained the word `note` **exactly once** — the pointer itself.

This is Rule 15's shape at the level of a guard: the instrument CLAIMED a property and
delivered a phrase, which is a silent failure in the instrument.

## The decision, in code

A `-h` page that points forward at a note carries it inside a marked region on the PUBLISHED page:

```
<!-- BEGIN <slug> NOTE -->
…the note…
<!-- END <slug> NOTE -->
```

The shape is not invented here — `man/gopi-merge.md` already carries
`<!-- BEGIN/END IMAGE SELF-CHECK -->` for the merge face's own note, for the same reason: a mark
is evidence a reader, a grep or a reviewer can check, and the region is addressable by name.

`cmd/agent/cli_help_note_region.go` holds the ONE vocabulary (`helpNoteBeginPrefix/Suffix`,
`helpNoteEndPrefix/Suffix`, `helpNoteSlugPattern`, `helpNotePointerBinders`,
`helpNotePointerBinderProximity`) and the predicate `helpNotePageOffenses` — a **pure function of the
page text**, so the gate and its red proof run the same code and not a re-implementation.

The predicate refuses exactly three things, each in its own words:

1. **a locational note pointer on a page with no marked region** — the class itself;
2. **an EMPTY region** — marks delimiting nothing: the defect wearing the fix's clothes (and
   the same refusal the merge face's own gate makes at its *"an empty region would make this
   gate pass on nothing"* arm);
3. **a MALFORMED mark** — `<!-- BEGIN NOTE -->`, a region with no name, addressable by nobody —
   plus a page that printed nothing (a guard that cannot see the page must not pass).

## What is deliberately NOT the fix

**Enumerating spellings.** "just under" → "beneath" → "following" is the same defect one
vocabulary rotation later, and it false-positives on legitimate prose ("the four flags listed
below"). The page-level decision is the page's **structure** — a marked region exists, is closed,
is non-empty — and a page that carries a region is decided without looking at the sentence at all.

**Honest correction (measured 2026-10-16, task `01a13722-bb4e-74c8-8da8-b6607da803ce`).** The
sentence above used to end "…never about the wording of the sentence pointing at it — so EVERY
locational spelling is caught". It was **false**, and measuring it is what this section records.
Two different halves live in this file and only one of them is structure:

| half | decided by | wording-independent? |
|---|---|---|
| a page that **carries** a marked region | the marks (exists / closed / non-empty) | **yes** — no wording can dodge it |
| a page with **no** region, scanned for a pointer to refuse | binder FAMILIES + proximity | **no** — a family it omits is missed |

Measured on the live predicate before the repair: `"see the self-check note after this row."` on a
region-free page returned **0 offenses** while `"just under"`/`"below"` wording on the SAME page
returned **1**. Both guards (`TestAtomPageCarriesNoDanglingNotePointer`,
`TestPublishedHelpPagesResolveTheirNotePointers`) stayed **GREEN** on that spelling. So the
closed seven-word list was a silent miss wearing a property's comment — Rule 15 at the guard
level, one layer below where the file's own header warns about it.

## The widened net, and the limit it still has

`helpNotePointerBinders` is now a family list — the locative adverbs the first draft had
(`below`, `beneath`, `underneath`, `under`, `following`, `further down`, `past this`), plus the
FORWARD-REFERENCE prose families the draft omitted (`after`, `later`, `further on`, `down the
page`, `at the end`, `next page`, `next screen`). Widening was decided by MEASUREMENT, not taste:

- the corpus was probed page by page (`publishedVerbHelpPages` + `taskverbs.MergerVerbs` + the
  top-level page), reporting each page's region count and every line mentioning the note referent.
  Public locators are the seven + the "after …" family — so "after …" is a real family a page can
  use, not one spelling quoted from a report;
- the BROADEST honest rule — "a page that mentions a note without carrying a region" — was
  measured and **rejected**: it false-positives on `gopi session -h` ("gopi session notes <query>
  … SEARCH the round-level [IMPORTANT] note") and on `gopi -h` ("notes.txt  A positional …"),
  which name a note as CONTENT and point nowhere. A guard that reds a page for printing the word
  `note` is a guard authors route around, which is how a guard dies.

**The limit, stated rather than hidden** (this is the part the corrected comment now says):

- a promise built on a family the list omits — `"see the note ahead"`, `"see the note in a
  moment"` — is still MISSED. No rule assembled from English binders covers every spelling, and
  this file no longer claims otherwise;
- the trade's direction is deliberate (Rule 15): a bounded over-match is printed in words the
  author can act on (carry the note in a region, or name the carrier), while a silent miss is what
  let the defect live;
- the spelling-INDEPENDENT guarantee is unchanged and is the page-level one: put the note in a
  marked region and no wording matters at all.

**A pointer that NAMES its carrier is not this class** ("see `man gopi-ui_state`"). The `-h` page
ROUTES; the man page carries the contract (the same sentence the usage-budget gate's refusal
prints). Only a promise of a note *at a location on this page* needs a region.

## The visited false negative, kept because it is the lesson

The first version of the predicate split its vocabulary: pages carrying **no** mark were scanned
strictly (`below`/`beneath`/`underneath`/`further down`/`past this`), pages carrying a mark
loosely (those plus `under`/`following`). The reasoning was that `under` has an ordinary meaning
("the dumps under ~/.gopi"). The red proof's `respelled-under` arm — *"see the self-check note
under it"* on a page with no region — came back **GREEN**, i.e. the same false negative the guard
exists to refuse, reproduced inside the guard. The vocabulary is now ONE list including `under`,
bounded by `helpNotePointerBinderProximity` (60 chars) and by the sentence (`[^.\n]`), and the arm of
that name is in the red proof so the split cannot come back. A bounded, explained over-match
beats a silent miss (Rule 15).

## The repair

`gopi ui_state -h` had the note already standing on the page, unmarked: the *"AN ALL-BLANK GRID
IS QUALIFIED, NOT ANSWERED"* paragraph plus the *"Exit codes:"* block. What it did NOT carry was
the distinction the pointer promised — **when exit 1 means "no authoritative side" and when it
means "could not run"**. The page now carries that paragraph inside
`<!-- BEGIN self-observation NOTE --> … <!-- END self-observation NOTE -->`, whose marks are
rendered from the SAME vocabulary constants the guard recognizes (Rule 14: one definition).

The page measured 160 lines against `uiStateUsageBudget = 172`; with the region it measures 169,
inside the budget and inside `usageBudgetNewFlagMargin = 12` of the cap — line 63's promise now
resolves into a region on its own page.

## Honest limits

- The gate walks `publishedVerbHelpPages` — the twelve published verb pages — **and the
  sub-atom pages**: `taskverbs.MergerVerbs` (the list the dispatcher reads) is walked in the
  same test (`t.Run("task-"+verb, …)`), so `gopi task merge -h` and its siblings are scanned
  through the parser's own table and a new atom is scanned the day it exists. The atom page
  carries no note pointer today (verified: 0 occurrences of the word `note`, 0 region marks).
  The top-level `gopi -h` IS walked too (the test's own `top-level` subtest); its one `note`
  occurrence is a file-name column, not a promise.
- `TestAtomPageCarriesNoDanglingNotePointer` (`cli_tasks_merger_help_atoms_test.go`) no
  longer holds a vocabulary of its own: after the consolidation (task
  `01a135b3-d727-7bb1-9e61-be4628f9b941`) its body consumes `helpNotePageOffenses` — the ONE
  predicate — keeps its separate legitimate assertion (the `stale decider` fact the page must
  not lose), and carries a RED ARM that feeds the SAME predicate the measured re-spellings
  ("see the self-check note just under this row", and the out-of-vocabulary "see the self-check
  note after this row") on a region-free page, so a second, narrower instrument cannot grow back
  on the atom path. Narrowing the predicate's binder families back to the old seven-word list
  makes that arm go RED — the mechanical demonstration is the arm's reason to exist.
- The proximity bound (`helpNotePointerBinderProximity`, 60 chars) is a bound: a promise whose
  note word stands more than 60 characters from its binder, in the same sentence, is not caught.
  Widening it trades against welding two unrelated clauses together.
- A page may carry a region nobody points at — that is not this defect (an unreferenced region is
  redundancy, not a dangling promise) and the predicate stays silent about it by design.
