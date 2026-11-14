<!-- doc-class: historical -->
# Help Hierarchy Reverse Guard — every registered sub-command must be ADVERTISED by its owning page

**Slice 4/5 of the help-hierarchy reorg.** This slice ships the regression
instrument (a recursive reverse guard) plus the one product change the instrument
needed to be expressible. It deliberately does **not** rewrite the pages — that
is the follow-up row (see `## Not Done`).

## The defect the guard exists to catch

The help hierarchy has a forward direction that is already gated: a page may not
advertise a word the CLI does not dispatch (the eight `*_help_surface_*` tests,
`cmd/agent/cli_*_help_surface_*_test.go`). The **reverse** direction was ungated:
a sub-command can be registered in the dispatcher and named on **no** help page,
so `gopi <verb> -h` never tells the user it exists. Nothing failed — the word
simply became undiscoverable.

That is not hypothetical in this tree: the third-level `gopi session checkpoint
list|export` words are dispatched (`runCheckpointSubcommand`'s switch) while the
`session` page's checkpoint pointer mentioned `restore`/`status` only. A user
reading `gopi session -h` had no route to the two live third-level words.

## What ships

| file | what |
|---|---|
| `cmd/agent/cli_help_hierarchy_reverse_guard_test.go` | the guard: 5 tests, reads the **real binary's** help output |
| `cmd/agent/cli_checkpoint.go` | `checkpointSubcmdList` / `checkpointSubcmdExport` constants; the dispatcher `switch` now references them instead of re-typing the literals (Rule 1b/13) |

### The guard derives its registries — it does not hold a list

A guard that hand-maintains the words it checks is a second source of truth that
drifts silently; it would pass forever while the CLI grew. Every registry the
guard checks is therefore **derived from the code that already owns it**:

- family verbs → `config.FamilyVerbs`
- second-level words → `sessionSubcommandNames`, `memorySubcommandNames`,
  `configSubcommandNames`, `notifySubcommandNames`, `toolSubcommandWords`
- third-level sandbox words → the `internal/sandbox` constants
- third-level checkpoint words → `checkpointSubcmdList` / `checkpointSubcmdExport`

A new sub-command added to any of those registries enters the guard's scope with
no test edit — which is the point.

### Three matching forms, because pages have three shapes

The first draft of the guard assumed one syntax and went **red on a green tree** —
a false red is an instrument defect, not a finding, and it is recorded here so the
next author does not re-derive it. A page advertises a word in whichever of three
shapes its own layout uses:

1. **command line** — `gopi memory search …` / `gopi task submit …`
2. **tool row** — the `tool` page's `name — description` rows
3. **alternation** — `<a|b|c>` inside a synopsis, e.g. the sandbox page's
   `gopi tool sandbox <create|show|run|…>`

The guard tries all three per word; a word matching none of them on its owning
page is red.

## Verification

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- **Tests**: `make test-budget PKGS='./cmd/agent ./internal/config'` →
  `packages: 2 finished, 0 with a failure` (exit 0, 505 s wall, stream
  `run-1791196838-2147065.json`). The three pre-existing `cmd/agent` failures the
  baseline below names did not reproduce in this run — the branch is fully green
  under the declared budget entry point.
- **Red→green, twice, on the real binary** (a guard nobody has seen fail is not
  an instrument):
  - *registry red*: inject a fake word into a registry → guard **red**;
    restore → **green**.
  - *page red (level 3)*: rename `show` → `inspect` on the sandbox page →
    guard **red**; restore → **green**.
- **A stale binary makes the red fake.** The guard reads `bin/gopi`, so a red
  proof is only valid after rebuilding — the first red attempt reused an old
  binary and "went red" for the wrong reason. Rebuild, then re-run.

## Not Done (honest)

- **The pages are not rewritten in this slice.** The `gopi -h` Commands index is
  still the long form and the pages still advertise unevenly; the guard makes
  that debt *visible and testable* rather than fixing it. The rewrite (index →
  one prose line per verb; each page listing its own second-level words plus
  `gopi <verb> <sub> -h` pointers) is dispatched as its own row — see
  `## Follow-up`.
- **The guard covers advertisement, not phrasing.** It answers "is the word
  reachable from its owning page's output", not "is the sentence about it
  accurate" — an accurate-worded wrong sentence still passes.

## Follow-up

One row, per Rule 26 (an incomplete fix is repaired by dispatching again, never
by waiting): the index collapse + page layering + the census/advertisement gate
rewrite, measured as TOO_BIG for a single session budget —
`01a10ba8-2714-77cd-9969-33d64e664b10`, chained after this row's task.

## Baseline (pre-change, for the next reader)

`make test-budget PKGS='./internal/config ./cmd/agent'` before this branch's
changes: `internal/config` green; `cmd/agent` carried three pre-existing
failures unrelated to help —
`TestChatSurfaceKey_DrivesTheRealREPLEditorOnARealPty`,
`TestWorkerForegroundPrintsTheTaskOnItsConsole`,
`TestEditToolAnnotatesTeachingDocsInAuditedTrees`.
