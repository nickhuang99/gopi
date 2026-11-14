<!-- doc-class: historical -->
# The devdoc flat-spelling sweep — what the G5 re-derivation exposed

**Task**: 01a10f17-d0dc-7d45-b5d3-991a739f86d6 (G5 inventory re-derivation),
re-landed as task 01a10f84 after the merge run pruned the original branch as
"already merged" on an ancestry test while the content never reached master.
**Instrument**: `artifacts/scan-devdoc-flat-spellings.py` in this task's session
directory (`/home/user/.gopi/sessions/01a10f17-d0dc-7d45-b5d3-991a739f86d6/artifacts/`)
— session evidence, deliberately NOT committed: it is a measurement, and the
repo's precedent for it (`scan-old-spellings.sh`, session `01a10efc…`) is the
same.

## Why a sweep at all

`devdoc/gopi-chat-web-help-surface-inventory.md` was RED on master: its row 8
and its per-row test table still spelled the RETIRED flat verb `gopi proxy`
while `TestG5InventoryMatchesTheHelp` derives the surface set from the usage
constants, which now yield `gopi tool proxy`. The doc's own generator
(`scripts/g5-chat-web-help-surface.sh`) still probed `gopi proxy` — a word the
binary refuses by name since verb-retirement phase 1, so the script printed a
flagless row at exit 0: silently wrong (Rule 15). Both were re-derived onto the
family address; the Totals census (8 surfaces / 44 flags / 8 driven) did not
move, because the surface was renamed, not added.

The obvious follow-up question — *how many other devdocs still show a retired
verb as a command?* — is what this sweep answers.

## The instrument, and the false green it produced first

The vocabulary is the BINARY'S OWN VERDICTS (Rule 31): candidate words are
harvested from the docs, handed to the real binary inside an isolated profile,
and only a word whose `-h` prints the retirement notice counts. 540 candidates
→ **25 retired words**. The class comes from `devdoc/DOC-INDEX.tsv`:
`normative` (a standing claim about master — a command it shows must run) vs
`historical` (the doc's title declares a past run's record — citing the retired
spelling is what it is FOR).

**The first cut reported the flagship defect as a "narrative mention" and the
control tree went green.** It asked whether the WORD was wrapped in backticks
(`` `proxy` ``); a doc that presents a command wraps the WHOLE PHRASE
(`` `gopi proxy` ``), so the word is never adjacent to a backtick. Fix: count
delimiter parity before the match column, not adjacency. The control tree then
reported the subject doc's 3 hits and the worktree 0 — the only shape in which
a scan's verdict means anything.

The second correction was structural: one `grep` per (doc, word) pair is 45k
process spawns and blew past the 2-minute idle net; a single tree walk with the
class looked up from the path prefix is fast (re-measured on the re-landed tree:
`wall=7.39s` for 1816 docs).

## What it found

> **Re-measured for the re-land (task 01a10f84, 2026-10-06).** Every number in this
> section was re-taken with the same instrument, on the tree this branch hands
> over — master `01fc16dc` merged in at the delivery-point sync, HEAD `8390c892`
> — and on `--ref master` as the control. The tree moved under the measurement
> three times while this task ran (`402961f2` → `208da693` → `aef82d93` →
> `01fc16dc`, two of them merged in as they arrived); the figures below are the
> LAST run, taken after the final merge, and re-running the instrument at that
> tip reproduces them. The pre-prune figures survive nowhere; what follows
> replaces them.

| tree | correctness hits | subject doc | docs with a hit |
|---|---|---|---|
| `master` (control, read from the object store) | **2008** | 3 | 426 |
| the re-landed tree | **2008** | 0 | 426 |

1815 docs by `--ref`, 1816 in the worktree (the doc you are reading is the
difference). The three displaced hits are the subject doc's own rows (`:58`,
`:93`, `:109`, word `proxy`), and they reappear as three hits in this doc, which
is the intended trade. 240 further sites are `narrative-mention` (prose, outside a
code span) and 2 are `identifier` (a tool's underscore spelling in a non-shell
fence) — reported, not counted as defects.

**Both sibling inventories have since been re-derived on master, so the "three
inventories, one re-derived" shape this section first reported is gone** — and
that is what the first run of this sweep was for, so the count is the proof it
worked: `devdoc/gopi-session-help-surface-inventory.md` (G4) and
`devdoc/gopi-tools-maintenance-help-surface-inventory.md` (G6) both read **0**
correctness hits at the close-out tree, and G6's gate
(`TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation`) passes. Master's own
`ce507a8f fix(g4): probe the session family address` did the G4 half.

G4's gate is still red at the close-out tree, for an UNRELATED reason, and it is
attributed rather than guessed: all three of its files
(`cli_session_help_surface_basic_test.go`,
`devdoc/gopi-session-help-surface-inventory.md`,
`scripts/g4-session-help-surface.sh`) are **byte-identical to master**, and the
failure is its own expectation table — `cli_session_help_surface_basic_test.go:393`
pins the fixture's raw-log path as `…9cd0/7f2c9d11-….raw.log` while the help now
prints `…9cd0/….raw.log`. A master-owned staleness, not this change's.

The remaining hits are spread thin (426 docs, median 2–6 each) and are mostly the
same shape: a doc written while a verb was flat, whose code spans and fences still
show the flat form.

## Not done (honest)

- The ~2000 remaining hits are **not** repaired here. Several are legitimate
  citations of a RETIRED spelling and belong to history; separating them
  doc-by-doc is a large sweep, and `--normative-only` (the flag-subset already
  in the instrument) is the next cut. What this task owes is the measurement,
  and it is above with its control.
- The **G4** inventory re-derivation is done on master, but its gate's stale
  expectation table (above) is a separate row with its own gate (Rule 28/26):
  dispatched or fixed there, not here.
- `internal/standards` has its own red set on master, unrelated to this change.

## Reuse

`python3 artifacts/scan-devdoc-flat-spellings.py <repo-root> [--ref <rev>]` —
exit 0 = no correctness hit, 1 = at least one, 2 = the scan could not measure
(no binary, empty vocabulary). `--ref` reads a control tree out of the object
store, which is the only way to show the instrument can still see the defect.
