<!-- doc-class: historical -->
# `task list`: the archived-row drop now discloses itself

**Task:** `01a0ed37-6e4d-7b32-8377-45d52d435a32` (epic `archive-visibility`, DOABLE).
**Branch:** `task/01a0ed376e4d7b32837745d52d435a32`, base: master @ `ae717042`.
**Finding:** `docs/archive/demo/materials/01-list/05-finding.md` F1.1 / F1.2.

## What the briefing asked, and what the base commit already answered

The briefing's premise — *"`gopi task -h` only describes the filter as acting on
`--status failed`"* — was **already false at my base commit**. The sibling task
`01a0ecea…` (`f4151a80`, "one predicate for archived-row visibility") had landed
the F1.1 half: it moved the drop into `tools.TaskListVisible`, gave the
conversational `/task list` the same `--all`, **and rewrote the help** so it now
reads *"hidden from EVERY list view, whatever `--status` says, and whether or not
a status was given at all"*. I verified this on my own branch rather than
trusting the source (Rule 38 discipline: re-derive the premise):

```
$ git merge-base --is-ancestor f4151a80 HEAD && git log -1 --format='%h %ad %s' f4151a80
f4151a80 IS ancestor
f4151a80 Tue Sep 29 20:05:00 2026 +0800 fix(task-list): one predicate for archived-row visibility (terminal + chat)
```

So option **(a) "make the behaviour match the doc"** was already in place for the
scope question, and it is the option the ledger keeps: the tombstone hygiene is
deliberate (a handled failure must not re-clutter a working view), and the help
now states the true rule. What was still open is F1.2 — **the drop was silent**,
so an all-hidden view and an empty ledger printed byte-identical text. That is
the defect this branch fixes, together with the source comments that still
carried the old, narrower claim.

## The two stale comments (behaviour/doc alignment, the part still missing)

`f4151a80` synced the operator-facing help and left two **in-code** statements of
the same false rule — the same class of defect one layer down:

| file:line (base) | old claim |
|---|---|
| `internal/tools/tasks.go:6179` | "…only the default `--status failed` view hides it" |
| `internal/tools/tasks.go:6449` | "the **failed-view** hygiene: … must not re-clutter the **default** view" |

Both now state the rule the code implements (every list view; `TaskListVisible`
is the one funnel; the view that hid rows discloses it). A future reader
trusting the old comment would have re-introduced exactly the bug F1.1 was.

## The fix: the drop discloses itself (F1.2, Rule 15)

`internal/tools/tasks.go`

```go
const TaskListAllFlag = "--all"                    // Rule 1b/13: the ONE spelling
const TaskListArchiveHiddenPrefix = "[hidden] "    // the marker the help names too
const taskListArchiveHiddenFormat = "%s%d archived row(s) hidden — tags contain %q; %s shows them"

func TaskListArchiveHiddenNote(n int) string { if n <= 0 { return "" }; … }
```

`FilterArchivedRows` appends `TaskListArchiveHiddenNote(dropped)` on **both** of
its exits — under the surviving rows, and under the empty text:

```go
if rows == 0 {
	return taskListEmptyResult + "\n" + TaskListArchiveHiddenNote(dropped)
}
return rewriteTaskListCount(out, rows) + TaskListArchiveHiddenNote(dropped)
```

Three properties are deliberate, and each is tested:

1. **Zero is empty.** It is a FUNCTION returning `""` for `n <= 0`, not a
   `const`/format string: a caller that appends it unconditionally can never
   print "0 rows were hidden". Rule 15 cuts both ways — a view that hid nothing
   must not claim it did.
2. **The all-hidden view is distinguishable from an empty ledger.** This is the
   whole point: `No tasks found.` still leads (the reader's eye keeps the line
   it knows) and the disclosure rides under it.
3. **It names the real flag.** `TaskListAllFlag` is the same value
   `taskFlagMember` admits for the verb, so the instruction cannot drift from
   the parser.

**Why disclosure and not "stop filtering"?** The filter is policy the operator
asked for (M4.5: `--status failed` shows only unhandled failures). Removing it
would trade a visibility defect for the clutter the policy exists to prevent.
What F1.2 actually asks for is that the read command *say what it did*, which is
what the note does — in numbers, on the surface.

**Help updated in the same commit** (`tasksUsage`): the disclosure is documented
where the rule it belongs to already is, including that the line is printed even
for an all-hidden view, and that a view which dropped nothing carries no line.

## `--sweep`: same filter, measured (the briefing's third ask)

Measured on a private sandbox profile, one archived `completed` row
(`/home/user/.gopi/sessions/01a0ed37-…/final-sweep-semantics.txt`):

| probe | output |
|---|---|
| `task list --status completed --sweep` (stamp fresh) | `No tasks found.` + the disclosure |
| the same command again, inside the throttle window | `[stale] already swept at … (window 10m0s) — nothing swept` **+ the disclosure** |
| `task list --status completed --sweep --all` | the tombstone, ` [resolved]`, no disclosure |
| `task list --status completed \| grep '^\[hidden\]'` **vs** the same with `--sweep` | **byte-identical** |
| stamp mtime before/after a plain (non-sweep) read | `1790688704` → `1790688704` (unchanged) |

**Conclusion:** `--sweep` is NOT a separate view. `runTaskList` calls
`maybeTaskListSweep()` first (its own throttle is the persisted stamp, and a
refusal is loud, never silent) and then returns
`tools.TaskListVisible(opts.status, opts.all)` — the identical call the plain
read makes. So the sweep path inherits the disclosure and `--all` exactly, and a
plain read stays a pure read (no stamp write) as the help promises.

## Red → green

The red proof is the pair reverted, tests kept (the source files restored with
`git show HEAD:<path> > <path>` — no `git stash`, see the note below):

| | internal/tools | cmd/agent |
|---|---|---|
| RED (`FilterArchivedRows` un-wired) | `--- FAIL: TestTaskListVisibleHidesArchivedInEveryStatusView` (and the two `TestFilterArchivedRows…` note assertions) | `--- FAIL: TestTasksCLIListDisclosesHiddenArchivedRows` (4 assertions, incl. "byte-identical to an empty ledger") |
| GREEN | `ok github.com/nick/gopi/internal/tools 0.641s` | `ok github.com/nick/gopi/cmd/agent 13.213s` |

Full logs: `red-final-internal.txt`, `red-final-cmd-agent.txt`,
`green-final-internal.txt`, `green-final-cmd-agent.txt`, `red-to-green.txt`
(session directory above).

**`git stash` was refused by the harness, and it was right to** — in this
repo `refs/stash` is shared by every linked worktree, so a stash/pop pair can
land another session's WIP on top of a branch it does not own. The red-pair
reversal used `git show HEAD:<path> > <path>` and a session-directory backup
instead. Worth repeating for the next agent: **do not use `git stash` here.**

## Not Done (honest)

- **The F1.1 scope fix is not mine.** It landed in `f4151a80` before my base
  (sibling task `01a0ecea…`); I verified it in my tree and only aligned the two
  stale in-code comments. A reader of the finding doc should not credit this
  branch with the help-text rewrite.
- **The finding doc itself (`docs/archive/demo/materials/01-list/05-finding.md`) was not
  edited.** It is a demo-materials record of a measurement, and Rule 38 keeps a
  narrative measurement as the record it is; the material fixes for F1.1/F1.2
  belong to the demo round that owns that corpus.
- **`--sweep` was measured through the CLI's rendering path, not by forcing a
  real stale row to be swept.** The stamp was fresh in both sandboxes, so the
  sweep itself refused (`[stale] …`); what is proven is that the sweep path
  shares the filter, the render and the disclosure — not the sweep's own
  ledger effect (that behaviour is pre-existing and has its own tests:
  `TestTaskListSweepFlagStillFiresStaleEvent`, `TestTaskListReadPathSweepsNothing`).

## Verification

- **Build:** `go build ./...` → `BUILD_OK` (exit 0); binary built with
  `CGO_ENABLED=0 go build -o bin/final-gopi ./cmd/agent/`.
- **Tests:** `go test ./internal/tools/ -count=1` → `ok … 0.641s`;
  `go test ./cmd/agent/ -count=1` → `ok … 13.213s` (scoped run over the list
  surface: `TestTasksCLIListDisclosesHiddenArchivedRows`,
  `TestTasksCLIListHeaderCountMatchesRows`, `TestSlashTaskListHidesArchivedRows`,
  `TestTaskListReadPath…`). Red proof quoted above.
- **Ran:** on a private sandbox (`gopi sandbox create --label final2 --task-id …`),
  real binary, real ledger —
  `GOPI_HOME=$SB ./bin/final-gopi task list --status completed`;
  `… task list --status completed --all`; `… task list`; `… task show fin-a`;
  `… task list --status completed --sweep` (twice, plus `--all`); and the control
  `… task list --status completed` against a **fresh empty** sandbox.
- **Result:** the archived ledger prints `No tasks found.` **plus**
  `[hidden] 2 archived row(s) hidden — tags contain "resolved"; --all shows them`;
  the empty-ledger control prints `No tasks found.` **alone** (exit 0, no
  disclosure) — the two are no longer byte-identical; `--all` renders both
  tombstones with ` [resolved]`; `task show` still reaches the archived row; the
  sweep probes matched the plain read byte for byte.
- **Commit:** `e31c945b` — `fix(task-list): the archived-row drop discloses the count it hid`

## Evidence (self-filed)

- `git merge-base --is-ancestor f4151a80 HEAD` → exit 0 (`f4151a80 IS ancestor`),
  printed by the command above — the F1.1 premise re-derived, not assumed.
- RED: `go test ./internal/tools/ -run 'TestFilterArchivedRows|…HiddenNote…|…Visible…'`
  → `FAIL github.com/nick/gopi/internal/tools 1.289s`, quoting
  `tasks_archive_test.go:483: --status completed must disclose the row it hid`.
- RED: `go test ./cmd/agent/ -run TestTasksCLIListDisclosesHiddenArchivedRows`
  → `FAIL … 6.914s`, quoting `cli_tasks_test.go:1283: an all-archived view is
  byte-identical to an empty ledger`.
- GREEN: same two packages → `ok … 0.641s` / `ok … 13.213s` (logs listed above).
- Disclosure bytes on a real ledger: `final-cli-evidence.txt` (commanded runs
  (i)–(iv)); empty-ledger control in `final-cli-evidence.txt`'s sibling run,
  `No tasks found.` with no `[hidden]` line.
- `--sweep` parity: `final-sweep-semantics.txt` section F — the `grep '^\[hidden\]'`
  of the plain read and of the sweep read are the same line.
- Purity side of the sweep claim: stamp mtime `1790688704` before and after a
  plain `task list` (section E of the same file).
- `git status --porcelain` in the worktree before close-out: see the run's own
  close-out record (the framework measures it; not restated here).
