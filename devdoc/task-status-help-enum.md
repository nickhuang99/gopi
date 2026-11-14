<!-- doc-class: historical -->
# Task Report: `gopi task status` missing from the top-level help enumeration (2 pre-existing red tests)

**Branch:** `fix/task-status-help-enum` (worktree `/home/user/workspace/gopi-task-status-help`, based on master `571c3d30`)
**Recovery:** `git show fix/task-status-help-enum:devdoc/task-status-help-enum.md`

## Completed

- **Root cause (git-verified, not guessed).** `d7474fb4` ("feat(task): persist a task's
  process identity …") introduced `gopi task status` and updated **two** of the three
  surfaces that must agree: `taskCompletionNames` (cmd/agent/cli_completion.go, the
  `gopi task <TAB>` set) and the family help `tasksUsage` (cmd/agent/cli_tasks.go). It did
  **not** touch `internal/config/help.go`, whose top-level `gopi -h` command index kept a
  **hand-typed** enumeration of the family verbs — so `status` was absent there and both
  pinned tests stayed red on master:
  `TestTopLevelUsageTaskVerbEnumMatchesCompletion` (usage_test.go:367) and
  `TestTopLevelUsageShowsTaskLedger` (usage_test.go:317).

- **Fix — the enumeration is now DERIVED, not re-typed.** New leaf package
  `internal/taskverbs` (Rule 14 shape, sibling of `internal/tasktags`/`internal/taskverdict`:
  constants only, zero project imports, so `internal/config` can import it without a cycle):

  | constant | value |  | constant | value |
  |---|---|---|---|---|
  | `Adopt` | adopt | | `Reopen` | reopen |
  | `Archive` | archive | | `Run` | run |
  | `AuditMerges` | audit-merges | | `Search` | search |
  | `Diff` | diff | | `Show` | show |
  | `Harvest` | harvest | | `Status` | **status** |
  | `Improvements` | improvements | | `Submit` | submit |
  | `List` | list | | `SweepZombies` | sweep-zombies |
  | `Merge` | merge | | `Sync` | sync |
  | `MergeExecute` | merge-execute | | `TagResumed` | tag-resumed |
  | `MergedCleanup` | merged-cleanup | | `Update` | update |
  | `Merger` | merger | | `Verdicts` | verdicts |
  | `Push` | push | | `Verify` | verify |

  plus `FamilyVerbs []string` — the one **advertised surface** in render order
  (24 verbs; the deprecated `serve` alias is deliberately absent).

- **Both consumers now reference it (Rule 13 — no bypass, no mirror):**
  - `internal/config/help.go`: the row is built with
    `"The task-as-memory ledger: " + strings.Join(taskverbs.FamilyVerbs, ", ") + " — append-only, never deleted."`
  - `cmd/agent/cli_completion.go`: `var taskCompletionNames = taskverbs.FamilyVerbs`.

- **Anti-regression guards (Rule 10.4/13), all red-proofed:**
  - `TestNoProductionFileRestatesTheTaskVerbEnumeration` (internal/config) — walks
    `internal/**` + `cmd/**` production files and fails any that restate the verb
    sequence as a literal (the definition site is the ONE allowed holder). It also fails
    if fewer than 50 files were scanned, so it cannot pass vacuously.
  - `TestTopLevelTaskRowDerivesTheEnumeration` — the index row must equal the
    constant-built string.
  - `TestTaskCompletionNamesAreTheSharedFamilySurface` (cmd/agent) — the completion set
    must BE `taskverbs.FamilyVerbs`, element for element, and must contain `Status`.
  - `internal/taskverbs/verbs_test.go` — pins the dispatched spellings (literals asserted
    in exactly one place), `FamilyVerbs` ↔ constants agreement, sortedness/uniqueness, and
    the deliberate `serve` exclusion.

- **Assertions untouched.** No test was relaxed; `usage_test.go`, `cli_completion_test.go`
  and `cli_tasks.go`'s `tasksUsage` are unmodified.

## Not Done

> **FOLLOW-UP LANDED** (branch `feat/task-dispatch-verb-constants`,
> devdoc/task-dispatch-verb-constants.md): the dispatch-switch instance below was
> converted — the labels are `taskverbs` constants, the `unknown subcommand
> (want …)` enumeration is derived from `taskverbs.DispatchVerbs`, and
> `TestTaskCompletionNamesMatchDispatch` was reworked to resolve the labels
> through the AST instead of by regex-scanning quoted literals. The paragraph
> is kept verbatim as the record of what was known at the time.

- **No change to `cmd/agent/cli_tasks.go` / `tasksUsage`** — the family help already
  documents `gopi task status` (d7474fb4), and `TestTaskCompletionNamesMatchFamilyHelp`
  derives `taskCompletionNames` from `tasksUsage` by source scan; it passes unchanged.
- **`serve` alias** deliberately left dispatching (scripts) and unadvertised, as before.
- **Not migrated:** the other hand-typed verb lists in the tree (e.g. the dispatch-error
  message `unknown subcommand %q (want list|show|…)` at cli_tasks.go:1020, the first-level
  `topLevelCommandNames`) — out of scope for this fix; the new gate covers the *task
  family enumeration* class only.
- **Pre-existing Rule 1b instance left in place (submitted as its own work item):** the
  dispatch switch itself still compares bare literals —
  `cli_tasks.go:662 case "search", "show", "status", "update", "archive":` and
  `cli_tasks.go:3868 case "status":`. Changing those labels to `taskverbs.*` is NOT a
  mechanical rename: `TestTaskCompletionNamesMatchDispatch` (cli_completion_test.go:68)
  discovers the dispatch verbs by scanning for QUOTED case labels, so constant labels
  would make that scanner blind (it would report every verb as missing). The conversion
  and the scanner rework belong together, in a task of their own.

## Verification

- **Build:** `go build ./...` → OK (job-2, `BUILD_OK`).
- **Targeted (the two pinned tests):**
  `go test -count=1 -run 'TestTopLevelUsageTaskVerbEnumMatchesCompletion|TestTopLevelUsageShowsTaskLedger' ./cmd/agent/`
  → **ok** (they FAIL on master `571c3d30` before the change — reproduced first).
- **Packages:** `./cmd/agent/ ./internal/config/ ./internal/taskverbs/ ./internal/standards/
  ./internal/stylegate/ ./internal/testutil/` (job-2 output in the transcript;
  `internal/standards` is **unchanged vs master** — `ok` on both).
- **RED PROOF (Rule 10.4 — a guard you have not seen fail is vacuous):**
  1. master's `help.go` (literal row, no `status`) restored → `TestTopLevelTaskRowDerivesTheEnumeration`
     fails with the exact `have`/`want` diff and both pinned tests fail → then restored green.
  2. a hand-typed re-inline planted in the tree (`cmd/agent/zz_probe_literal.go` quoted list
     **and** `internal/config/zz_probe_row.go` a joined literal string) → the gate flags BOTH
     by path+offset; removing the probes returns green. The first probe attempt exposed a
     **bug in the gate itself** (a quote-blind separator let the quoted Go list through) —
     fixed and re-proved, noted here so the gate's precision is auditable.
- **Real binary (render truth, Rule 10.2):** `CGO_ENABLED=0 go build -o … ./cmd/agent/`;
  `gopi -h` renders the row with `… show, status, submit, …` (continuation lines included);
  `gopi task -h` documents `gopi task status <task_id> [--json] [--db PATH]`;
  `gopi completion bash` emits `… show status submit …` for the `task)` case;
  `gopi task status` (no id) → `missing required parameter: task_id (positional)`.
- **Rule 1b/13 compliance:** the verb literal `"status"` appears in the production tree only
  at its `internal/taskverbs` definition site (`grep -rn '"status"'` in cmd/agent hits only
  the unrelated `--status` FLAG parsing); the enumeration literal exists in exactly one file,
  enforced by the new gate.

## Files

- `internal/taskverbs/verbs.go` (new, constants + `FamilyVerbs`)
- `internal/taskverbs/verbs_test.go` (new)
- `internal/config/help.go` (row derives from `taskverbs.FamilyVerbs`)
- `internal/config/task_enum_gate_test.go` (new, anti-restatement gate + derivation test)
- `cmd/agent/cli_completion.go` (`taskCompletionNames` = `taskverbs.FamilyVerbs`)
- `cmd/agent/cli_completion_test.go` (derivation guard)
