<!-- doc-class: normative -->
# `gopi ui` / `gopi cli` — withheld from the published surfaces, still dispatchable

Truth class: **normative** — a standing claim about master, re-derivable at any
commit (registered as such in `devdoc/DOC-INDEX.tsv`).

Task `01a10dba-674f-769c-ba54-f8da1960f3a8`. Operator ruling: 这两个命令暂不暴露 —
the code stays, both verbs stay runnable, nothing advertises them.

## The change is a DISPLAY change, and nothing else

`cmd/agent/cli_ui.go`, `cli_term.go`, `cli_verbs.go` and every Go path that
implements or routes the two verbs are byte-for-byte untouched. What changed is
which surfaces list them:

| surface | before | after |
|---|---|---|
| top-level `gopi -h` command index (`internal/config/help.go`, `commandIndex`) | `gopi ui` row + `gopi cli` row | neither row |
| `gopi <TAB>` candidates (bash / zsh / fish, `cmd/agent/cli_completion.go`) | `ui`, `cli` offered | not offered |
| `gopi ui -h` / `gopi cli -h` | answers | **answers (unchanged)** |
| dispatch in `main()` | routes | **routes (unchanged)** |

Verified against the built binary, not the source:
`./bin/gopi -h | grep -E '^\s*gopi (ui|cli)\b'` → exit 1 (no match);
`./bin/gopi ui -h` and `./bin/gopi cli -h` → exit 0 with their own usage page;
all three `gopi completion <shell>` outputs carry no `ui`/`cli` token at the
first level.

## Why the suppression lives beside the completion table, not in `internal/config/help.go`

The two published surfaces must be suppressed **together**. A completion list
offering a verb the help index does not explain is exactly the drift that
`TestTopLevelHelpCoversEveryDispatchedSubcommand`'s reverse direction exists to
catch ("completion follows the help"). So the membership decision has ONE
definition — `completionSuppressedVerbs` (`cmd/agent/cli_completion.go`),
subtracted by ONE function `topLevelCompletionNames()`, which all three
renderers read. A later edit that re-adds a verb to one surface alone is red in
`cli_help_hidden_verbs_test.go`, not silently green.

`topLevelCommandNames` deliberately stays the FULL dispatch vocabulary: three
other guards key on it (the positional-verb guard's vocabulary, the
undispatched-tool-word refusal, the census's own drives). **What the chain
routes** and **what we advertise** are two different questions, and collapsing
them would make a suppression look like a de-dispatch — which is the exact
confusion the operator's ruling forbids.

## The `ui_state` description was a dangling pointer — Rule 15

The index row for `ui_state` used to read *"Read a live `gopi ui` screen as a
cell grid"*. Once `gopi ui` leaves the index, that sentence names a command the
reader cannot find on the front door: a pointer to nothing is its own defect.
The summary now reads *"Read a live agent surface's screen as a cell grid."* —
the detail it dropped (`--target`, which surface) is named by `ui_state`'s own
`-h`, which is where detail belongs.

`gopi ui_state` itself **stays in the index** — the operator kept it. Whether
`ui_state` should also be withheld is an open question, recorded as not decided
here (see the report's `## Not Done`): it is a *reader of* a hidden surface, and
a reader whose subject is unpublished may or may not be worth publishing itself.

`TestTopLevelHelpPointsAtNoCommandItDropped` guards the class rather than this
one sentence: a row whose summary or usage names a command the index does not
carry is red.

## The forward guards had to learn the difference

`TestTopLevelUsageAdvertisesEveryDispatchedVerb` ("a dispatched verb must not be
INVISIBLE by accident", memory #1252) and
`TestTopLevelHelpCoversEveryDispatchedSubcommand` are forward guards: they walk
the dispatch chain and demand a help entry per verb. A deliberate absence is not
their business, so each now skips a suppressed verb — reading
`completionSuppressesVerb`, never a hand-listed exception, so the suppression
list remains the single source of truth (Rule 13). The negative direction is not
left unguarded by that skip: it is pinned verb-by-verb in the new file.

## Files

- `internal/config/help.go` — two index rows removed, `ui_state` summary rewritten.
- `cmd/agent/cli_completion.go` — `completionSuppressedVerbs`, `topLevelCompletionNames()`, `completionSuppressesVerb()`; three renderers repointed.
- `cmd/agent/cli_help_hidden_verbs_test.go` — the reverse guard (new).
- `cmd/agent/usage_test.go` — the two forward guards read the suppression list.
- `COMMANDS.md`, `devdoc/gopi-verb-census.md` — regenerated / re-derived (Rule 38: a census figure is recomputed by its generator, never hand-edited). The census's `advertised` column is derived from the BUILT binary's own text, so it moved for exactly these two rows: `ui` and `cli` went `advertised 1 → 0` with `dispatched` still `1`.
