<!-- doc-class: historical -->
# COMMANDS.md — the CLI manual, and the two machines that keep it honest

**Status:** landed on `task/01a0fff0caab72c6aed79ea9e4e841c2` (master base
`b191a55d`). This page is the design narrative; the reference itself is
`COMMANDS.md` at the repo root.

## The problem

gopi's CLI had no manual. The top-level `gopi -h` is deliberately an *index*
(`internal/config/help.go:509` renders rows that point at each verb's own page),
each verb's `-h` is authoritative for its own flags, and CONFIG.md covers every
*knob* — but nothing covered the *verb surface as a whole*: what the 43 command
words are, which one answers a given wish, and why several spellings are
deliberately odd. That information existed only in the comments of
`cmd/agent/cli_verbs.go:46`, where a user never looks.

## The design, in three decisions

### 1. The machine truth is two files, and neither is importable

The verbs are decided in exactly two places:

- the **words**: `CliVerbXxx` constants, `cmd/agent/cli_verbs.go:46` (with
  `CliVerbAct` beside itself at `cmd/agent/cli_act.go:38`, and three aliases of
  leaf-package constants per Rule 14 — `internal/tools/ledger_backup.go:88`,
  `internal/cliverbs/cliverbs.go:56`, `internal/cliverbs/cliverbs.go:90`);
- the **routing**: `dispatcherOfToken`, `cmd/agent/cli_contracts_test.go:187`,
  keyed by the very constants above so a rename cannot drift it.

One is `package main` and the other is a `_test.go` file, so neither can be
imported by a gate or by a generator. **The shared reader is therefore a new
leaf package, `internal/cliindex`** (`internal/cliindex/cliindex.go:1`): it
reads both files as TEXT, resolves a qualified constant
(`tools.CliVerbMemoryGroup`) to the leaf package that owns it, joins the two
halves on the word, and fails loudly with a read-floor (fewer than 30 verbs ⇒
refuse) rather than reporting a short list. Both consumers call it, so the
document and the gate can never disagree about what the CLI offers.

The join is the interesting part: `CliVerbXxx` is the *primary* index (a
constant with no routing is still a verb, and the gate reports it), and
`dispatcherOfToken` adds the tool-wire mirrors (`gopi screen_capture`,
`gopi web_search`, …) whose words live in `internal/toolnames`.

### 2. The overview is GENERATED, not transcribed

The verb table inside COMMANDS.md sits between two markers —
`<!-- BEGIN GENERATED: verb-index -->` … `<!-- END GENERATED: verb-index -->` —
and is written by a generator under `internal/standards/testdata/`:

```bash
go run internal/standards/testdata/commands-census.go . COMMANDS.md
go run internal/standards/testdata/commands-census.go . COMMANDS.md --check   # exit 1 on drift
```

It lives under `testdata/` for the same reason
`repo-hygiene-census.go` does: the Go tool ignores that directory, so the file
is a MAIN package that `go build ./...` never compiles, while a reader can still
run it verbatim (Rule 35 — no new binary, no script, no daemon). The generator
is a thin wrapper: it calls `cliindex.ReplaceBlock` and reports whether the
document changed.

Each generated row carries two anchors — the predicate that routes the word and
the constant that owns its spelling — so the table is not just a list but a
*live* list: a verb whose routing moves re-renders to the new line.

### 3. The gate asserts four properties, and can be shown red

`internal/standards/commands_doc_gate_test.go` is the anti-drift half, shaped
exactly like its sibling `config_doc_gate_test.go`:

1. **coverage** — every verb `cliindex.Load` returns must be mentioned by the
   document (the generated block counts, and so does prose);
2. **no invention** — every `` `gopi <word>` `` claim in the document must name a
   dispatched verb, with a two-entry reviewed exemption map for the two words
   the document deliberately quotes (`gopi code_search` — the underscore
   spelling the dispatch *refuses by name*; `gopi replay` — the retired verb
   whose capability is `gopi chat --replay` now);
3. **the generated block matches a fresh render**, byte for byte, with the
   first differing line printed;
4. **anchors resolve** — every `file.go:NNN` span must point at an existing,
   **non-blank** line, with a read-floor (`commandsDocMinAnchors = 40`) so a
   page whose anchors were stripped fails instead of passing.

Three red proofs drive the *same* predicates over poisoned fixtures: an
invented verb, a dropped verb, a stale/blank/too-few anchor, and a
hand-edited generated block.

## What was measured while writing it (not assumed)

- **43 verb words**, from a source that holds **35 `CliVerbXxx` constants** —
  the difference is 8 tool-wire mirrors (`screen_capture`, `web_search`,
  `web_fetch`, `keyword_search`, `vision_analyze`, `text2img`, `voice_input`, …)
  plus `act`, whose constant lives in its own file. Anyone who count-matches
  "35 constants" against "rows in the table" will read a defect that is not
  there; the join is by *word*, not by constant.
- **102 distinct `file.go:NNN` anchors**, none on a blank line, all resolving.
- `git grep '^| \`gopi '` over the committed block plus a prose-mention check
  showed every verb is *explained*, not merely listed (0 verbs present only as a
  table row).

## The root-doc exception, and why it was reviewed

A tracked markdown file at the repo ROOT is refused by the parking-lot gate
unless `rootDocAllowlist` names it (`internal/standards/scratch_gate.go:129`) —
the entry exists so a report can never be parked at the root again. COMMANDS.md
took a reviewed entry beside CONFIG.md, on the same ground CONFIG.md's own
comment states: it is a **document of record**, not a report — an operator is
*sent to the root* for it. The gate that would otherwise only warn about a
missing allowlist entry is now asserted from both sides
(`TestCommandsDocRootAllowlisted`).

## Related work, deliberately NOT waited for

- `01a0fff0` (worker resource lifecycle) may later add or re-shape a verb's
  classification section. This page was written against the **current master**
  (`b191a55d`) rather than waiting: the generated block is the part a re-shape
  would touch, and it is regenerated by one command, so the cost of the
  eventual re-run is one invocation, not a rewrite.
- `01a0ffb4` (`gopi chat --replay` / `--film`) is **already merged** into master
  at `b191a55d`, and the page reflects the landed state: `replay` is NOT a verb,
  it is a flag — which is exactly why `replay` sits in the exemption map.

## Not done (honest)

- The second level of the GROUP verbs (`gopi task list`, `gopi memory store`,
  `gopi session show`, `gopi web search`) is described in prose with anchors,
  but it is **not** machine-generated: those vocabularies live per-family
  (`taskverbs`, `memorySubcommandNames`, …) with their own drift guards, and
  folding them into this page's generator is a follow-up, not a gap in the
  top-level claim. The gate's coverage assertion is about the TOP-LEVEL verbs
  (`cmd/agent/cli_verbs.go:46`), which is the stated scope.
- The page does not enumerate every FLAG of every verb (that is each verb's own
  `-h`, and `cmd/agent/cli_contracts_test.go:68` pins those pages). The
  "I want to …" index names the verb and its anchor; the flags belong to the
  verb's own page.

## Verification (this run)

- Gate: `go test ./internal/standards/ -run CommandsDoc` — the four assertions
  plus the four red proofs.
- Generator round trip: `--check` after a write reports "in step with the
  source (43 verbs)".
- Anchors: the 102 distinct refs were resolved one by one against the tree
  (0 missing, 0 out of range, 0 blank).
- The surfaces were RUN, not described: every verb page was invoked as
  `./bin/gopi <verb> -h` and the intent index was spot-checked with real
  invocations (`gopi log --last`, `gopi stats --last`, `gopi config show`,
  `gopi task list --status in_progress`, `gopi session list --limit 2`,
  `gopi sandbox show`, `gopi scratch-gc --dry-run`, `gopi doccheck`,
  `gopi code-search CliVerbTask`). See the task report for the verbatim output.
