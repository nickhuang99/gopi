<!-- doc-class: normative -->
# man pages — the markdown single source, the pipeline, and the drift gate

**Status**: step 1 of 3 of epic "man page 方案 A" (operator decision, 2026-10-11).
**Task**: `01a12b36-5af4-7e29-9a20-8b4f686bdcbb`
**Siblings (already queued)**: step 2 = the remaining 10 pages
(`01a12b36-7637-72a9-bf5f-a830d6a25607`); step 3 = help slimming + the 52 usage
constants (`01a12b36-96c3-7e28-9121-dd28b209540d`).

---

## 1. Why this exists (the measured argument, not a preference)

The help pages are at their line budgets **to the line**:

| page | measured | budget | constant |
|---|---|---|---|
| `gopi -h` | 173 lines | **177** | `topLevelUsageMaxLines`, `cmd/agent/usage_test.go` |
| `gopi chat -h` | 451 lines | **462** | `chatUsageBudget`, `cmd/agent/cli_usage_budget_test.go` |

(The `gopi chat -h` row was 576/577 under `chatUsageMaxLines` in
`cmd/agent/cli_chat_usage_budget_test.go` until help-slimming step 3 moved the
page's contract detail into `man/gopi-chat.md` and replaced that single-page
constant with the table that now caps **every** published page —
`subcommandUsageBudget` in `cmd/agent/cli_usage_budget_test.go`, whose
bidirectional arm also makes more than `usageBudgetNewFlagMargin` of slack red.
The measured-argument below is unchanged: it is the budget history that
motivated the move.)

Both budgets were raised to get where they are, twice each, and the recorded
reasons are the same sentence in both files: *"the paragraph IS the flag's
contract, and its detail has no other page to live on"*. A page whose length has
no legitimate destination can only grow, and each growth is another raise — the
subcommand budget's own header says a verb's page's LENGTH **"was protected by
nothing … which is exactly how a verb's page becomes a manual"**.

So the fix is not another compression pass. It is **giving the detail a page**:
a real man page, which is where a command's detail is supposed to live, and
which costs the help index nothing. Step 3 (help slimming) is deliberately NOT
this step — a page may only shed its detail once the destination exists.

## 2. The pipeline — source, command, output

**Source** — `man/<page>.md`, one markdown file per verb page:

```
man/gopi.md         man gopi        (the CLI index; also carries the verb list)
man/gopi-chat.md    man gopi-chat   (the chat verb)
```

**The ONE command** (Rule 38: the product names the program that re-derives it,
and here the generator IS the derivation):

```bash
make man
```

which is exactly these three steps, run from the repo root:

```bash
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/                       # the fact source
go run internal/standards/testdata/mancensus.go . bin/gopi            # rewrite the generated regions
pandoc -s -t man man/<page>.md -o bin/man/man1/<page>.1               # markdown -> roff
```

`make man` then prints how to read the result:

```bash
MANPATH=$PWD/bin/man man gopi
MANPATH=$PWD/bin/man man gopi-chat
```

**A second target refuses staleness** — the same predicate the Go gate asserts,
available from a shell. Measured exit statuses, precisely (the two numbers are
different and both are on purpose — `make` reports 2 for a failed target recipe,
while the generator itself exits 1):

```bash
make man-check                    # clean tree   -> exit 0, both pages "up to date"
make man-check                    # stale source -> generator exit 1, make exit 2,
                                  #   naming the page: "man/gopi.md — STALE (regenerate with `make man`)"
```

**Dependencies: none added** (Rule 35). `man` is the read half of the pipeline;
the RENDER half needs `pandoc`, an **optional dependency** of the `man` target —
the render leg is guarded by `command -v` and **SKIPS LOUDLY** — **exit 0**,
naming what it skipped and pointing at `make man-check` — when the renderer is
missing (Rule 15: a missing renderer must not read as "the whole man pipeline is
broken"). Measured 2026-10-14 (peer1, task `01a139cb-0650-75a3-9186-da7c0ad2d8bb`
/ commit `66588b39f`): `make man` died with **exit 127** at the render leg
— `/bin/sh: 1: pandoc: not found` — after the source-generation step above it had
already succeeded, so only the untracked, regenerable `.1` artifacts under `bin/`
(Rule 39) go missing, while the `man/*.md` sources are regenerated either way
(Makefile comment, commit `66588b39f`). The recipe introduces **no** new script,
service, daemon, state file, lock or config key. The generator is a
Go `main` under `testdata/` — the same shape as its siblings
`commands-census.go` and `repo-hygiene-census.go`: the Go tool ignores
`testdata/`, so it is never compiled by `go build ./...`, and the tree keeps
exactly one built binary (Makefile's "ONE BINARY" note). The rendered `.1` files
land under `bin/` and are **not tracked** (Rule 39): a regenerable artifact's
recipe is the deliverable, and `bin/` is already ignored.

## 3. What "same source" means here — and what is NOT generated

A man source carries **two generated regions**, each between its own marker pair,
and those markers are the whole contract:

```
<!-- BEGIN SYNOPSIS (copied verbatim from `gopi <verb> -h` by internal/standards/mancensus) -->
<!-- END SYNOPSIS -->
<!-- BEGIN VERB INDEX (generated — internal/standards/mancensus; do not edit by hand) -->
<!-- END VERB INDEX -->
```

- **SYNOPSIS** — the verb's own usage rows, copied **byte for byte** as
  `gopi <verb> -h` prints them, wrapping included. Not a summary, not a rewrite.
- **VERB INDEX** (the top-level page only) — the authoritative verb list, read
  from the help page's own index rows.

**Everything else in a man source is a human's prose** and is never touched by
the generator: the DESCRIPTION, OPTIONS paragraphs, FILES and SEE ALSO are
written and reviewed like any other document. That split is deliberate — the
spellings that CAN drift are generated; the explanations that need a writer's
judgement are not.

**The fact source is the BINARY, never a source file.** The synopsis comes from
`gopi <verb> -h` — the surface an operator sees (Rule 31) — not from the usage
constant in `cmd/agent`, which is a different thing that merely ought to agree.

**Who may change what.**

| what | who | how |
|---|---|---|
| the verb list, a synopsis line, a flag spelling | nobody — it comes from the code | change the code, then `make man` |
| a paragraph of a man page | any contributor | edit `man/<page>.md`, then `make man` |
| adding a page | any contributor | add the file, then add it to `pages` in `internal/standards/testdata/mancensus.go` **and** to `manPages` in `cmd/agent/man_source_gate_test.go` (and to `MAN_PAGE_NAMES` in the Makefile) |

The gate fails with the path named when a page is in one list and not the other,
so "I forgot" is not a silent outcome (Rule 15).

## 4. The drift gate — three comparisons, one function

`cmd/agent/man_source_gate_test.go`. The whole gate is ONE pure function,
`manSourceDisagreements(doc, help, verb, index)`, so the red proofs run **the
same code** the gate runs.

1. **SYNOPSIS — row equality.** The block between the markers must equal
   `gopi <verb> -h`'s usage rows.
2. **VERB INDEX — equality** with the verb list `gopi -h` prints.
3. **FLAG NAMES — one-directional.** Every flag the man source names (measured
   over the WHOLE document, prose included) must exist on the help page. The
   reverse is deliberately NOT checked: the help page may print flags the man
   page does not yet document — it is the index, and the man page is where
   detail goes. The direction that IS checked is the one that catches the man
   page inventing a command line.

**Red proof** — `TestManSourceGateRefusesDrift` runs the gate's own predicate on
three poisoned copies of the shipped sources (an invented flag, a stale synopsis
row, a dropped verb-index row) **plus an untouched control**: the real sources
must report NOTHING, or the poisoned cases would be indistinguishable from a
predicate that always refuses.

**Two parsers, one home.** `internal/mansource` owns `UsageRows`, `HelpVerbs`,
`FlagTokens`, `CodeLines` and the markers. The generator **writes** through it
and the gate **reads** through it, so writer and refuser cannot disagree about
where a region is or what a row means (Rule 13/14). It is a leaf package of pure
string functions — no filesystem, no process — so both consumers stay honest
about where their input came from.

## 5. The 12-page list — where it came from

The list is **exported**, never typed. Two published surfaces were read, and they
agree exactly (this is the command actually run, with the binary it was run
against — the generator is `internal/standards/testdata/mancensus.go`):

```bash
# surface 1 — the generated zsh completion (`gopi config completion zsh`).
# The words are the quoted rows of the `commands=( … )` block, one per line.
./bin/gopi config completion zsh | sed -n '/^commands=(/,/^)/p' \
  | grep -oP "'\K[a-z_]+" | sort -u > /tmp/s1.txt

# surface 2 — the top-level help's own index rows
./bin/gopi -h | grep -oP '^  gopi \K[a-z_][a-z_-]*' | sort -u > /tmp/s2.txt

diff -u /tmp/s1.txt /tmp/s2.txt    # exit 0, no output
#   both: chat config memory merge notify session task tool ui_state version web worker
#   wc -l: 12 / 12
```

(An earlier form of surface 1 used `compgen -W "…"`, which is the BASH
completion's shape — `--bash` prints `compgen -W "chat config … " -- "$cur"` and
the same 12 words; `--zsh` prints the block quoted above. Both spellings yield
the same 12, and the raw outputs of both are archived under the run's session
directory. The `commands=(` form is quoted here because it is the one whose rows
are stable to parse.)

`diff` of the two sorted lists: **no differences** (12 = 12). Both are rendered
from `cmd/agent/cli_completion.go`'s `topLevelCommandNames` **minus**
`completionSuppressedVerbs` (`ui`, `cli`, `merger`) — the suppression is what
makes 15 dispatch words publish as 12, and it shows up identically on both
surfaces. Raw outputs are archived under the run's session directory
(`artifacts/completion-zsh.txt`, `artifacts/help-index-verbs.txt`).

**Divergence from the plan-A table, honestly recorded.** The plan named 12 pages
but did not enumerate them; the enumeration above is the authority, and it
differs from a naive guess in two places worth stating: **`ui_state`, `version`
and `web` have no "obvious" page** (they are verbs like any other and are in the
12), while **`ui`, `cli` and `merger` are dispatched but deliberately
unpublished** and are therefore NOT pages — `merger` refuses at every spelling,
and `ui`/`cli` are withheld by an operator ruling (暂不暴露, task 01a10dba…).
A page for either would document a door the CLI does not offer at argv[0].

## 6. Same-source is MEASURED, not asserted

The acceptance criterion was "change one thing in the source → regenerate → the
`man` output follows". Three experiments were run against the built pages, and
each one pins a DIFFERENT half of the contract. (Raw outputs:
`~/.gopi/sessions/01a12b36-…/artifacts/drift-*.txt`.)

**(A) A generated region is a COPY — hand-editing it is undone by `make man`,
and the gate goes red first.**

```
edit man/gopi-chat.md's SYNOPSIS row  →  go test ./cmd/agent/ -run TestManSourcesMatchTheirHelpPage
   --- FAIL: …/gopi-chat.md
     SYNOPSIS block is not what `gopi chat -h` prints (regenerate with `make man`)
make man  →  "mancensus: man/gopi-chat.md — regenerated"
diff man/gopi-chat.md /tmp/gc.orig  →  IDENTICAL to the pre-edit file
go test …  →  ok
```

The source file came back **byte-identical to what it was before the edit**,
because the region's content does not come from the file — it comes from
`gopi chat -h` and the file merely holds the last copy of it.

**(B) The prose is NOT regenerated — and a flag invented there is refused.**

```
append a `--invented-flag` paragraph to man/gopi-chat.md
go test …  →  FAIL: "names --invented-flag, which neither `gopi chat -h` nor
                     `gopi -h` prints — a man page may not invent a flag"
make man   →  "up to date" (the generator does not touch prose, by design)
delete the paragraph by hand  →  ok
```

This is the half that keeps the man page honest in the direction the generator
cannot: a human writes the prose, so a human can write a lie into it, and the
gate — not the generator — is what refuses it.

**(C) Prose that a human writes DOES reach the rendered page** (the trivial
direction, included because "same source" is worthless if a source edit has no
effect at all):

```
append "A paragraph added by hand to prove that prose is the source."
make man  →  MANPATH=$PWD/bin/man man gopi-chat | grep -c "prose is the source"  →  1  (line 116)
restore the file, make man  →  same grep  →  0
```

**And the staleness target is a real predicate, not a message**: with one
generated row poisoned, `make man-check` prints `man/gopi.md — STALE
(regenerate with `make man`)` and exits non-zero (generator 1 / `make` 2); with
the tree restored it prints `up to date` for both pages and exits 0.

**The rendered pages themselves.** Both are produced by the pipeline above and
read through `MANPATH` (nothing is installed — see §7):

```
$ MANPATH=$PWD/bin/man man -w gopi gopi-chat
/home/user/workspace/gopi-01a12b36…/bin/man/man1/gopi.1
/home/user/workspace/gopi-01a12b36…/bin/man/man1/gopi-chat.1

$ MANPATH=$PWD/bin/man man gopi      # first screen
GOPI(1)              GoPi Manual              GOPI(1)
NAME
       gopi - a tool-using AI coding agent
SYNOPSIS
       …                        ← the 8 `gopi -h` usage rows, verbatim
DESCRIPTION
       gopi is a tool-using AI coding agent. …
COMMANDS
       …                        ← the 12 verbs, generated
```

`man gopi-chat` renders the same way, its SYNOPSIS carrying the two wrapped
`gopi chat -h` usage rows byte-for-byte.

## 7. Not done in this step (honest)

- 10 of the 12 pages are not written — that is step 2, and the machinery above
  already accommodates them (add a file, a `pages` row, a `manPages` row, a name
  in `MAN_PAGE_NAMES`).
- **No help content moved.** Not one byte of any usage constant changed, and no
  line budget constant was touched — the existing budgets are the measurement
  that justifies this epic, not something to spend here.
- Nothing is installed into a system man directory: `/usr/local/share/man/man1`
  exists but is **not writable** by this user (measured: `NOT_WRITABLE`), so the
  pages are read through `MANPATH`. An install step is a host-level act with its
  own owner and is deliberately not taken here (Rule 35).
- `man gopi-session`, referenced from `gopi-chat(1)`'s SEE ALSO, does not exist
  yet — it is one of step 2's ten, and the cross-reference is written now
  because the page's own SEE ALSO is where a reader looks.
