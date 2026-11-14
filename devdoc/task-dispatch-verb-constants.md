<!-- doc-class: historical -->
# Task Report: `gopi task` dispatch compares internal/taskverbs constants (not bare verb literals)

Branch: `feat/task-dispatch-verb-constants` (fresh, off `master` d45d90e2).

## Context (why this task existed)

Found while fixing the `gopi task status` help-enumeration defect
(`devdoc/task-status-help-enum.md`, branch `fix/task-status-help-enum`): the
top-level `gopi -h` task row was a hand-typed verb list and had silently missed
`status`. That fix introduced `internal/taskverbs` — the leaf package that is
now the single source of truth for the family vocabulary — and derived the
help/completion surfaces from it.

Its "Not Done" section listed the remaining Rule 1b instance and warned that it
is **not** a mechanical rename:

> `cli_tasks.go:662 case "search", "show", "status", "update", "archive":` …
> Changing those labels to `taskverbs.*` is NOT a mechanical rename:
> `TestTaskCompletionNamesMatchDispatch` (cli_completion_test.go:68) discovers
> the dispatch verbs by scanning for QUOTED case labels, so constant labels
> would make that scanner blind (it would report every verb as missing).

That warning is exactly what happened when the conversion was attempted: the
regex `case\s+("(?:[a-z-]+)"…)` matches nothing once the labels are
identifiers, and the test then reports *every* verb as "missing dispatch verb"
— a red test for correct code, i.e. a guard that has to be reworked in the same
change or it becomes an obstacle.

Per Rule 23 the failed/archived branch was NOT continued: `internal/taskverbs`
was carried onto a fresh branch as a cherry-pick (commit `b243ce4a`, author
preserved) and the work proceeded from `master`.

## Completed

### 1. `internal/taskverbs` grows the two vocabularies the dispatch needs

`internal/taskverbs/verbs.go`:

- **new constants** for the spellings the dispatcher also compares and that had
  no constant yet: `New`, `Next` (both REMOVED M4.5 verbs — no dispatch case;
  the dispatcher recognizes them only to print the pointed removal message),
  `Serve` (the deprecated alias of `Merger`, which KEEPS a dispatch case).
- **`DispatchVerbs`** = `FamilyVerbs ∪ {Serve}`, DERIVED (not a second typed
  list): the dispatch surface, i.e. every spelling `runTasksSubcommand` must
  have a case for. `TestDispatchVerbsAreDerived` pins the derivation.
- `FamilyVerbs` remains the ADVERTISED surface (`DispatchVerbs` is not a
  synonym: the completion/help surface deliberately carries no `serve`).

### 2. `cmd/agent` references the constants at every family decision site

- `cli_tasks.go` — the dispatch switch (`runTasksSubcommand`), the parse switch
  and `taskFlagMember` (`parseTasksArgs`), the guard/deprecation comparisons
  (`verb == taskverbs.Merger || verb == taskverbs.Serve`, the M4.5 removed-verb
  arm), the run target comparison (`target == taskverbs.Next`), and the
  dispatch-error message, whose enumeration is now
  `strings.Join(taskverbs.DispatchVerbs, "|")` instead of a hand-typed
  `list|show|update|…` list (the same drift class the epic's parent fixed for
  the help row).
- `commands.go` — the `/task` slash dispatch (the same engine surface inside a
  conversation): `case taskverbs.List:` … plus the unknown-subcommand message
  derived from the same four constants.
- `cli_verbs.go` — `taskVerbRun = taskverbs.Run` (an ALIAS, not a second
  literal: the agent-spawn guard's word and the dispatch label are now one
  definition).
- `cli_chat.go` — the `gopi chat list|backfill` family got named constants
  (`chatSubcommandList` / `chatSubcommandBackfill`) rather than being folded
  onto `taskverbs`: `list` is the chat index's verb, not the task ledger's, and
  aliasing it to `taskverbs.List` would have coupled two unrelated families
  (the first attempt did exactly that and was reverted).

### 3. The guard that had to be reworked: the dispatch surface is resolved with the AST

`cmd/agent/cli_tasks_dispatch_gate_test.go` (new file) holds the shared scanner
and the gate:

- `taskverbConsts` parses `internal/taskverbs/verbs.go` and builds
  **name → spelling** by reading the const block. The gate therefore resolves
  labels against the definition site instead of a hand-maintained copy in the
  test (Rule 13: the spelling is written in exactly one place).
- `dispatchVerbLabels` walks `runTasksSubcommand`'s AST, finds the
  `switch verb` statement and resolves every case label through that map — the
  AST replacement for the old regex-over-quoted-labels scan, and what
  `TestTaskCompletionNamesMatchDispatch` now calls.
- `verbDecisionFindings` is the decision-site predicate: a case label of a
  switch **on a family discriminant** (`verb`, `sub`, `target`) or a string
  literal compared (`==`/`!=`) against one, that is not resolvable to a
  `taskverbs` constant.

`TestTaskCompletionNamesMatchDispatch` (cli_completion_test.go) was rewritten
around `dispatchVerbLabels`; its assertions are untouched in both directions
(every resolved dispatch verb must be completable — minus the `serve` alias,
which the completion surface deliberately omits — and every completion
candidate must have a dispatch case).

### 4. The exhaustive gate + its red-proof

- `TestNoBareTaskVerbLiteralAtDecisionSite` scans the whole `cli_tasks*.go`
  family plus the ONE slash-command function that mirrors it
  (`commands.go`'s `runSlashTask` — the file also hosts `/log` and `/memory`,
  whose `sub` switch is a different vocabulary; the scope is the function, not
  the file). A scan that inspects fewer than 30 decision sites **fails**: a
  green gate that read nothing is the failure mode the floor exists to prevent.
- `TestDispatchLabelsResolveToTaskverbsConstants` is the exactness half: the
  resolved label set must EQUAL `taskverbs.DispatchVerbs` (missing → the new
  verb has no dispatch case; extra → a dispatched spelling nobody declared).
- `TestVerbDecisionGateRedProof` runs the predicate over the pre-fix source
  shapes. Four probes, two positives and two negatives:
  - master's quoted multi-label dispatch line → 5 findings,
  - master's `target == "next"` comparison → 1 finding,
  - a flag-name literal appended to the verb (not a decision site) → 0,
  - a same-spelled literal with the reviewed escape → 0.

**Inline escape**: a literal that is not the family verb but happens to be
spelled the same (git's `push` subcommand inside the family file) is allowed
with `// task-verb-lit-ok: <reason>` on its line — the same
reasoned-exception discipline as `internal/standards`' gates. The gate's scope
makes this rare: it only looks at decision sites, so flag names (`--status`)
and prose are not its business.

## Not Done

- **`tasksUsage` / the `-h` heads still carry verb spellings inside raw help
  text.** The family help is a raw string literal; its verbs are prose handed to
  the user, and it is already guarded from the other side
  (`TestTaskCompletionNamesMatchFamilyHelp` parses the usage body and fails if a
  documented `gopi task X` line and the completion set disagree). Rewriting the
  raw string to be built from constants would trade a guarded literal for an
  unreadable concatenation; left as-is, deliberately.
- **`cmd/agent/commands.go` still holds the other slash families**
  (`/log`, `/memory`) with their own `sub` switches over bare literals
  (`last|sessions|summary|read|search`, `search|store|get|list`) — those are
  NOT the task family vocabulary and belong to their own families' gates; the
  new gate deliberately scopes to `runSlashTask`.
- **The `gopi task serve` / `gopi task merger` deprecated-alias tests** are
  unchanged: the alias keeps dispatching and stays unadvertised.
- **`internal/standards/baseline.go`** — no permit touches the changed lines
  (verified: the baseline's only mention of `cli_tasks.go` is a comment about a
  historical entry at line 1171); no re-pin was needed.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK
- `gofmt -l cmd/agent internal/taskverbs` → empty
- `go vet ./cmd/agent/` → clean
- Guards green: `go test -run
  'TestTaskCompletionNamesMatchDispatch|TestTaskCompletionNamesMatchFamilyHelp|TestCompletionCandidateSets|TestNoBareTaskVerbLiteralAtDecisionSite|TestVerbDecisionGateRedProof|TestDispatchLabelsResolveToTaskverbsConstants'
  ./cmd/agent/` → ok
- **Red-proof (planted probes on the real tree, each reverted after):**
  1. new verb `zz-probe` added to `FamilyVerbs` with no dispatch case →
     `TestTaskCompletionNamesMatchDispatch`: `taskCompletionNames has "zz-probe"
     but runTasksSubcommand has no case for it`; `TestDispatchLabelsResolveToTaskverbsConstants`:
     `taskverbs.DispatchVerbs declares "zz-probe" but runTasksSubcommand has no
     case for it` (`TestTaskCompletionNamesMatchFamilyHelp` correctly stayed
     silent — a help-parsing failure is not a dispatch-completeness failure).
  2. `case taskverbs.Status:` re-inlined as `case "status":` →
     `cli_tasks.go:3869: case label uses the bare literal "status"` +
     `dispatch case label "\"status\"" is not a taskverbs constant`.
  3. `target == taskverbs.Next` re-inlined as `target == "next"` →
     `cli_tasks.go:1295: comparison against the family discriminant uses the
     bare literal "next"`.
- Full `go test ./cmd/agent/` and `./internal/{taskverbs,config,standards,stylegate}` → see the run captured below.

## Pre-existing failures (measured, NOT regressions)

`go test ./cmd/agent/` is **not** green on this tree, and it is not green on
master either. Measured both sides, same machine, same run shape:

| failure | this branch | clean base worktree @ master d45d90e2 |
| --- | --- | --- |
| `TestWebToolsSignalShutdownWritesSessionEnd` | FAIL | FAIL (reproduced with master's own freshly built `bin/gopi` in a `git worktree add --detach` checkout — no change of mine applied anywhere) |

The failure is `session: 2 session files … (session.jsonl, signal-provenance.jsonl)`:
the signal-provenance ledger (epic signal-provenance) lands under the
single-session root the test asserts holds exactly one file. Nothing in this
task's diff touches `internal/signalrec`, the web loader or that test.

- `./internal/stylegate` is FAIL **on master** as well (test-sleep / test-setenv
  ceilings, sites in `internal/testutil/tmproot_assert_test.go`) — verified by
  running master's copy, per the "diff a failing gate against master by RUNNING
  master's copy" lesson.
- `./internal/taskverbs`, `./internal/config`, `./internal/standards` → all ok
  on this branch (the standards baseline needed no re-pin: no permit covers the
  changed lines).
- `internal/agent`'s `TestAgentSpawnGuard*` three failures seen in a
  pre-rebase master checkout do **not** occur on `d45d90e2` (that checkout was
  `b5064f0a`, a mid-history probe worktree); the merged spawn-guard fix
  (`feat/guard-test-daemon-env-isolation`, in `d45d90e2`) is included here.
