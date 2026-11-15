<!-- doc-class: normative -->
# man pages — the full verb set: the 13-page manifest and the help/man transition state

**Status**: step 2 of 3 of epic "man page 方案 A" (operator decision, 2026-10-11).
**Task**: `01a12b36-7637-72a9-bf5f-a830d6a25607`

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: `01a12b36-7637-72a9-bf5f-a830d6a25607`
**Branch**: `task/01a12b36763772a9bf5fa830d6a25607`
**Depends on**: step 1, `task/01a12b365af47e299a208b4f686bdcbb` (tip `eab9bf611`)

## What this step is

Step 1 built the pipeline and proved it on two pages. This step **moves and
completes**: it adds the remaining verb pages to the same pipeline, so the man
set covers the CLI's whole published verb surface. It changes no help text, no
usage constant and no budget constant — the 52 usage constants and the length
budgets are step 3's business.

## The page set, DERIVED (not typed)

The manifest comes from two independent authoritative surfaces, and they were
compared rather than one being trusted:

```bash
./bin/gopi config completion zsh | sed -n "/commands=(/,/^)/p" \
  | sed -n "s/^    '\(.*\)'$/\1/p" | sort -u > completion-sorted.txt
./bin/gopi -h | grep -oE '^  gopi [a-z_]+ ' | awk '{print $2}' | sort -u > index-sorted.txt
diff -u completion-sorted.txt index-sorted.txt     # exit 0
```

Both sides yield **12**, and `diff` exits 0 — byte-identical lists:

```
chat config memory merge notify session task tool ui_state version web worker
```

Raw output: `~/.gopi/sessions/01a12b36-7637-72a9-bf5f-a830d6a25607/artifacts/`
(`verbs-completion-zsh.txt`, `help-top.txt`, `verbs-completion-sorted.txt`,
`verbs-index-sorted.txt`).

### "12 页" is the verb count; the SET is 13 files

The epic says "12 页齐 (man gopi 总览 + 12 动词页)". Read literally that is 13
documents, and 13 is what exists: the epic's own parenthetical already counted
the overview separately. Stated plainly so no one hunts for a missing page:

| # | page | verb | step |
|---|---|---|---|
| 0 | `man gopi` | (top-level index) | 1 |
| 1 | `man gopi-chat` | `chat` | 1 |
| 2 | `man gopi-config` | `config` | 2 |
| 3 | `man gopi-memory` | `memory` | 2 |
| 4 | `man gopi-merge` | `merge` | 2 |
| 5 | `man gopi-notify` | `notify` | 2 |
| 6 | `man gopi-session` | `session` | 2 |
| 7 | `man gopi-task` | `task` | 2 |
| 8 | `man gopi-tool` | `tool` | 2 |
| 9 | `man gopi-ui_state` | `ui_state` | 2 |
| 10 | `man gopi-version` | `version` | 2 |
| 11 | `man gopi-web` | `web` | 2 |
| 12 | `man gopi-worker` | `worker` | 2 |

`ui_state`, `version` and `web` ARE verbs and get pages. `ui`, `cli` and
`merger` are dispatched by the binary but are deliberately NOT published as
verbs, so they are not pages.

## The one reader extension this step needed, and why it was right

> **Retired mechanism (merge verb removal, 2026-11-01).** the `gopi merge -h`
> invocation quoted below is a RECORD of what that page printed in 2026-10; the
> top-level verb was removed later (its implementation and flag parser deleted)
> and the word now answers with a refusal that points at `gopi task merge`. The
> reader extension this section describes is still in `mansource.UsageRows` and
> still serves `gopi worker -h`; what has changed is which surfaces it has to
> read.

`mansource.UsageRows` knew **two** help forms: the top-level one (`Usage:`
header, rows beneath) and the verb one (first shape ON the `Usage:` line). Two
verbs print **neither** — `gopi merge -h` and `gopi worker -h` open on a
one-line title and go straight into an indented block of invocations:

```
GoPi merge — one foreground merge run.

  gopi merge                          one run over every repo in the
                                      settings.json "repos" table
```

Without a third reader the generator writes `@@ NO USAGE BLOCK @@` into both
pages and the gate reports *"`gopi merge -h` printed no usage block — the
synopsis fact source is missing"*: a claim about the READER, dressed as a claim
about the page.

So `UsageRows` gained the headerless fallback (indent + a WORD after `gopi `,
which is what keeps the sentence "gopi version is the only spelling …" out of a
SYNOPSIS block). This is a **reader extension inside the existing leaf package**,
not a second pipeline: same `man/` sources, same `mancensus` generator, same
`pandoc` step, same gate, same markers.

Two guards ship with it, in the file whose convention is "the shapes that
actually went wrong, not invented examples":

- `TestUsageRowsReadsTheHeaderlessForm` — the third form yields its 3 rows.
- `TestUsageRowsDoesNotOpenOnProse` — the `Usage:` header wins over prose that
  mentions `gopi`, and paragraph text never leaks into a SYNOPSIS.

**Red proof (observed, not asserted).** The fallback was disabled in isolation
(`return i` → `_ = i` in `usageBlockStart`) and the gate run:

```
--- FAIL: TestManSourcesMatchTheirHelpPage/gopi-merge.md
    `gopi merge -h` printed no usage block — the synopsis fact source is missing
--- FAIL: TestManSourcesMatchTheirHelpPage/gopi-worker.md
    `gopi worker -h` printed no usage block — the synopsis fact source is missing
```

The source was restored immediately afterwards (`grep -c '_ = i'` → 0).

## The drift gate found real drift in my own prose

`man/gopi-memory.md` wrote a prose row `` `--tier cold` ``. `--tier` appears on
`gopi memory -h` inside a **usage line**, not as a row head, so the gate refused
it by name:

```
man/gopi-memory.md: names --tier, which neither `gopi memory -h` nor `gopi -h`
prints — a man page may not invent a flag
```

The gate was right and the prose was wrong: a flag presented as a row head is a
claim to document a spelling, and this page was using it as shorthand for a
*tier value*. Rewritten as prose. This is the gate working on its author, which
is the only evidence that it works at all.

## help ↔ man: the transition state (FOR STEP 3)

Step 3 (help slimming) inherits this arrangement:

- **`-h` stays the contract surface.** One-line synopsis, every flag spelling,
  exit codes — the parser reads them and the tests assert them. Nothing moved
  off it in this step: `go test ./cmd/agent/ -run
  'UsageBudget|HelpWidth|HelpSurface|ManSource'` is green and the 52 usage
  constants are byte-unchanged.
- **`man gopi-<verb>` is where the detail now HAS a home.** The prose pages
  carry the contracts, the refusals, the exit codes and the "why", so step 3 can
  cut a `-h` page with somewhere for the cut text to go.
- **`man gopi` carries the generated verb index**, derived from `gopi -h`'s own
  index rows — so a verb added to the CLI either reaches the man page or the
  build goes red.
- **The detail is IN the man page, not pointed at.** Man pages are deployable
  and `devdoc/` is not, so a man page that said "see devdoc/x.md" would point at
  nothing on a user's machine — the trap the operator named.

### One finding filed, not fixed here (Rule 41) — step 3 should read it first

Reading the 12 `-h` pages one by one turned up a surface inconsistency, and it
is **filed as its own row, not patched in this branch**:

> **`01a12b7e-4388-7e36-8264-b59888dd8cde`** (tags=bug, pending) — 10 of the 12
> published verbs open `-h` on a standalone `Usage:` line; **`merge` and
> `worker` print none**, going straight from a one-line title into the indented
> block. Measured, not inferred: `./bin/gopi <verb> -h | grep -c '^Usage:'`
> gives 1 for ten verbs and **0 for merge and worker**.

Why it is not cosmetic, measured in this same step: any consumer that anchors on
the `Usage:` line gets **zero rows** for those two pages *while their usage block
is right there*. `UsageRows` did exactly that, and the gate then reported
`gopi merge -h printed no usage block` — a claim about the PAGE that was really
a claim about the READER. The fix here was to teach the reader the third form

**Step 3 is where this becomes a decision, not here.** Step 3 owns the 52 usage
constants and the length budgets; this step may not touch them, so normalizing
`merge`/`worker` is out of scope by construction. If both rows edit help text at
once they collide — read `01a12b7e`'s briefing before starting step 3. Raw
evidence: `~/.gopi/sessions/01a12b36-7637-72a9-bf5f-a830d6a25607/artifacts/evidence-usage-header-inconsistency.txt`.

## The three drift directions, re-measured at this step's scale

Measured against all 13 pages, with the raw output in the session artifacts:

1. **generated region hand-edited → gate red → `make man` restores it from
   `-h`.** The generator copies `UsageRows` verbatim; the block is a copy, and
   the copy is reconstructible.
2. **prose naming an unknown flag → gate red, and `make man` does NOT fix the
   prose** (by design — prose is a human's). This is what caught `--tier`.
3. **prose edited → the rendered man page changes.** `man/gopi-notify.md` gained
   a marker line, `make man` re-rendered, `man gopi-notify | grep` went 0 → 1
   hits, then the marker was removed and it returned to 0. A source change
   reaches the page a reader actually reads; the pipeline is not decorative.

## Files

New (11 sources):

```
man/gopi-config.md   man/gopi-memory.md  man/gopi-merge.md    man/gopi-notify.md
man/gopi-session.md  man/gopi-task.md    man/gopi-tool.md     man/gopi-ui_state.md
man/gopi-version.md  man/gopi-web.md     man/gopi-worker.md
```

Modified (4):

```
Makefile                                  MAN_PAGE_NAMES: 2 -> 13
internal/standards/testdata/mancensus.go  pages: 2 -> 13
cmd/agent/man_source_gate_test.go         manPages: 2 -> 13
internal/mansource/mansource.go           UsageRows: the headerless fallback
internal/mansource/mansource_test.go      the two guards above
```

The rendered `.1` pages land in `bin/man/man1/` and are **not tracked** — `bin/`
is ignored, and the recipe is the deliverable (Rule 39).

## Not Done (honest)

- **No system install.** `/usr/local/share/man/man1` is not writable by this
  user, so the pages are read through `MANPATH=$PWD/bin/man`. Installing into a
  system man directory is a host-level action and belongs to that directory's
  owner — not something to obtain by asking for a privilege (Rule 25/35).
- **No `man gopi <verb>` sub-page reach.** `man gopi-task` is the spelling; the
  git-style `man gopi task` form needs configuration this step did not add.
- **Per-sub-verb pages** (`man gopi-task-submit`, …) are not in scope: the
  derivation is the published VERB list, and sub-verbs are not published verbs.
- **The `-h` pages are unchanged**, deliberately — including `merge` and
  `worker` still printing no `Usage:` header. That inconsistency is real and
  visible in the man sources' SYNOPSIS blocks; whether to normalize it is a
  help-text decision, i.e. step 3 or its own task, not a silent edit here.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK; `go build ./...` — OK.
- Tests: `go test ./cmd/agent/ -run 'UsageBudget|HelpWidth|HelpSurface|ManSource'` — `ok 20.775s`.
  `go test ./internal/mansource/` — `ok`.
- Ran:
  - `make man` — 13 pages: `mancensus` regenerated the 11 new sources, `pandoc` rendered 13 `.1` files.
  - `make man-check` — exit 0 (all 13 up to date); after a poisoned source, exit 1/2.
  - `MANPATH=$PWD/bin/man man -w <13 pages>` — 13 paths, one per page, exit 0.
  - `MANPATH=$PWD/bin/man man <page> | col -b | wc -l` — 36–116 lines per page; every page has a SYNOPSIS section.
  - `./bin/gopi tool doccheck` — "the devdoc corpus is consistent", exit 0.
  - `git status --porcelain` — clean after commit (Rule 30).
- Result: 13 pages resolve and render; `make man` is idempotent (a second run
  reports 13 "up to date"); the drift proof moves a marker 0 → 1 → 0 through the
  rendered man output.

## Evidence (self-filed)

- **Manifest derivation** — `diff -u completion-sorted.txt index-sorted.txt`
  printed nothing and exited 0; both files hold 12 lines (verified by `wc -l`).
- **Headerless reader red proof** — the two FAIL lines quoted above, observed
  with the fallback disabled and nothing else changed; `grep -c '_ = i'` → 0
  after restore.
- **Gate caught my prose** — the `--tier` refusal quoted above was printed by
  `go test ./cmd/agent/ -run ManSource`, red, before the fix; green after.
- **Render truth** — `man -w` returned 13 existing paths; `col -b | wc -l`
  gave 36–116 rendered lines per page; all had a SYNOPSIS heading. Not OCR, not
  a filename check.
- **Drift proof** — the marker grep returned 0 before the edit, 1 after
  `make man`, 0 after restore.
