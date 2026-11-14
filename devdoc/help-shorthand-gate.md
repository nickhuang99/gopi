<!-- doc-class: normative -->
# The help-shorthand gate: classifying an advertised invocation, not just deriving it

**Task** `01a10ff501b772959fb85095b937aedd` · gate
`cmd/agent/cli_help_shorthand_gate_test.go`

## The class, and why four groups could not see it

Every group in the help-surface family (G1 task, G3 memory, G5 chat/web, G6
tools/maintenance) derives its advertised surface from the page constants and
refuses a form it cannot account for. All four read a synopsis line for its
**verb** and its **`--flags`**. A page can put a choice inside what looks like
argv — `bash|zsh|fish`, `--useful|--useless|--unused`,
`patch.json | --import patch.json`, `"why it failed"|@FILE` — and the shorthand
tokens are not flags, so they never became a row. **A row that does not exist
cannot fail.** That is the whole mechanism of the gap: the derivations were not
weak, they were reading a different question.

G4 (session) closed the variant of this class on its OWN page by deriving rows
and then EXECUTING each one; the other groups derive only. This gate is the
cheap cross-group half: derive the same rows and refuse the shapes that could
never be executed — one fixed cost, in-process, from the constants.

## The five classes and the four arms

`typeable` | `metavariable` | `bracket-optional` | `alternative-branch` |
`shell-pipe`.

1. `|` at bracket depth ≥ 1 — an optional group `[--mode chat|web]`. ALLOWED.
2. depth 0, **glued** (`a|b`) — one literal token no dispatcher accepts. REFUSED.
3. depth 0, **spaced**: a program-word tail (`| jq .prompt_tokens`) is a worked
   example; a flag-shaped tail (`| --import patch.json`) is a branch wearing a
   shell example's spacing. The second is REFUSED.
4. The help spellings `-h | --help` / `-h|--help` are exempt — an explicit
   two-word allow-list, not a heuristic.

An unclosed bracket group is refused too: the same defect one step earlier.

## The extractor lessons (each cost a round)

- **A blank line does not end a scope.** Releasing the scope there silently
  dropped the memory and G6 groups — 128 of 359 rows. The floors exist because
  of this.
- **Join a wrapped continuation** (indent ≥ 18) onto its row, or a group broken
  over two lines reads as an unclosed bracket — but only for the line DIRECTLY
  under the row. Released by indent alone, it concatenated the whole
  `voice_input` flag table's prose onto one synopsis.
- **`<id>` is a metavariable, not a group.** Counting angle brackets as group
  brackets classified the family's own `--json | jq` control as an optional
  choice.
- **A row's own value block is not a surface** (`--mode chat|web` in a flag's
  body is a value choice). Exclude it with a scope STACK, not a pattern.
- **Tokenize with quote awareness**, or the offending token reported is a
  fragment (`failed"|@FILE`) and the message does not name the page's own text.

## The non-vacuity discipline

A classifier gate that refuses everything passes the refusal test, and one that
admits everything fails it — so the control set must contain BOTH: seven GREEN
controls (the family's existing shapes: `| jq`, `-h | --help`, `[--mode chat|web]`,
`[--tier hot|warm|cold]`, `[--shape|--backfill|--reclassify]`) and six RED ones.
Plus a floor on the scanned line count and on each class count — measured 359
rows (typeable 160 / metavariable 98 / bracket-optional 100) against floors of
120/60/30/8.

## The red proof

`git worktree add --detach <path> HEAD` + copy the new test in + run it **there**
against the pre-fix pages: RED, 15 offenders, controls PASS. Do not use
`git stash` in the shared repo. The gate then goes GREEN after the fixes.

That proof is what found **three offenders the briefing did not name**
(`session checkpoint list|export`, `gopi tool shot --url URL | --self` written
twice) — one of them on a page G4 already claims to execute end to end, because
G4's runner reads the same argv fragments and never reassembled them.

## The fix recipe (keeps the advertised verb/flag set)

Wrap the choice in a bracket group: `--reason "why"|@FILE` →
`[--reason "why"|@FILE]`; `--url URL | --self` → `[--url URL|--self]`;
`checkpoint list|export` → `checkpoint [list|export]`. For a top-level index row,
name ONE spelling and describe the others in the description column
(`gopi config completion <shell>` + "bash, zsh, fish").

**The glued form also keeps the column narrow**: `[--url URL | --self]` (with
spaces) widened the index column, pushed every row past the render width and
broke `TestTopLevelUsageShowsVoiceInput` — measured, not foreseen.

## What this does NOT do

It refuses a shorthand; it does not execute a row. Four of five groups still have
no typed case for their advertised surfaces, and the three named classes
(metavariable / bracket-optional / shell-pipe) are unexecuted there. That sweep
is its own task — this gate is the admission half, one fixed cost rather than
four more runners.
