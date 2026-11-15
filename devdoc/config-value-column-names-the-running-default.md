<!-- doc-class: normative -->
# `/config`: the value column names the default that runs

## The defect (measured 2026-11-13, bug row `01a1d15d-9b5f`)

`/config` printed a knob's row from two facts that were each true and jointly
useless:

```
max_tokens      (unset)  [built-in default]
context_window  (unset)  [built-in default]
thinking        (unset)  [built-in default]
timeout         (unset)  [built-in default]
…
agent.max_tokens      16384  [agent]
agent.context_window  200000  [agent]
```

A reader could not answer "will 16384 be sent, or is nothing set?" — the value
column said nothing and the channel column named a default it did not print —
and the resolved `agent.*` rows below looked like a contradiction rather than
the same fact stated twice. The table's own title says `configuration in force`,
which sharpened the confusion: `(unset)` was in force and still ran 16384.

Root cause: `renderRuntimeConfigSnapshot` printed `f.InForce` (what the PROCESS
holds) and, when that was empty, the literal `(unset)`; the compiled-in default
was known to the builder (`runtimeConfigBuiltinDefaultOf`) but only ever reached
the CHANNEL column. The two columns were designed to be separate — the old code
comment said so explicitly — and the separation was the bug.

## The fix

`runtimeConfigField` carries a `Default`, filled by
`runtimeConfigDefaultForValueColumn(key, inForce, document)`: non-empty only
when the process holds nothing AND the document states nothing AND the code has
a default. The renderer then prints both facts in one cell, from ONE format
constant (`runtimeConfigUnsetDefaultFormat`):

```
max_tokens      (unset), 16384 runs  [built-in default]
timeout         (unset), 0 runs  [built-in default]
keep_thinking   (unset)  [(unset)]
```

Three deliberate boundaries:

- **The default is never a second copy.** Every value comes from
  `runtimeConfigBuiltinDefaultOf`, which returns the real constants
  (`deepseek.DefaultClaudeMaxTokens`, `agent.DefaultContextWindow`,
  `deepseek.DefaultClaudeThinkingBudget`, `thinkmode.Disabled`,
  `config.ResolveReasoningEffort("", nil)`) — Rule 13.
- **A knob with no default keeps `(unset)` in both columns.** Labelling
  `keep_thinking` / `hide_thinking` / `effort_values` `[built-in default]`
  would promise a fallback the code does not have; the regression test asserts
  the absence.
- **A `NOT APPLIED` row does not name a default in the cell.** Its `!` note
  already states the default AND the far more important fact that the document's
  value is dead; putting a default in the cell beside a dead value would blur
  that distinction.

`(unset), 0 runs` for `timeout` is deliberately literal: `0` is how the code
spells "no ceiling" (`internal/config/config.go`: `TimeoutSec int // overall
agent timeout in seconds (0 = unlimited)`), so the report prints the code's own
spelling rather than inventing a phrase the constant does not use. The `/help
config` block says so, beside the channel it documents.

## The guard

`TestRuntimeConfigSnapshotNamesTheRunningDefaultInTheValueCell` pins three
claims, the third being the one a naive fix drops: what the unresolved row says
will run IS the value the resolved `agent.*` row prints — two rows, one fact.
`TestRuntimeConfigSnapshotMarksNotApplied`'s `max_tokens` assertion was
rewritten to the new shape (`(unset)`, `[built-in default]` AND the default's
value), not relaxed to match the old output.

Both defaults are read through `runtimeConfigBuiltinDefaultOf` in the test too,
so a constant that moves cannot leave a stale literal behind.
