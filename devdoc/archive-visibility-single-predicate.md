<!-- doc-class: historical -->
# Archive visibility: one predicate, every list view

**Task:** `01a0ecea-2c99-7172-a50c-faa5e44d2aa7` — epic `archive-visibility` (DOABLE).
**Branch:** `task/01a0ecea2c997172a50cfaa5e44d2aa7` (base `3f048cb6`).
**Measured at:** 2026-09-29, binary built from this branch (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`).

## The defect, measured (not inferred)

`gopi task archive <id>` stamps the `resolved` tag on a TERMINAL row — a
tombstone, never a delete. The visibility promise was *per caller*, not per
column, and the two operator-facing list views had drifted apart.

Pre-fix binary, private sandbox profile (`GOPI_HOME=<sandbox>`), one archived
`completed`/`cancelled` row present:

| view | `[resolved]` rows rendered |
|---|---|
| `gopi task list` (terminal, no `--status`) | 0 — filtered |
| `gopi task list --status completed` | 0 — filtered |
| `gopi task list --all` | the tombstone, with its marker |
| **`/task list` (chat, no `--status`)** | **1 — the tombstone RENDERED** |
| **`/task list --status completed`** | **1 — the tombstone RENDERED** |

Same ledger, same archived column, opposite verdicts depending on which face
rendered it. The conversational view was also the only one that could not be
excused as a deliberate choice: it had no `--all` to name its behaviour with.

**The table is reproducible, not narrated.** The pre-fix column was measured, at
close-out, by a binary built from the delivered branch's own BASE commit
(`3f048cb6`) — extracted with `git archive 3f048cb6 | tar -x` into an off-tree
directory (Rule 29: never a checkout of someone else's worktree) and built with
`CGO_ENABLED=0 go build -o /home/user/workspace/_tmp/gopi-prefix ./cmd/agent/`.
The same probe script then drove both binaries against their own private
sandbox profile, and the two differential lines read:

```
pre-fix  (/home/user/workspace/_tmp/archvis-evidence-prefix.txt)
  terminal: default=0  --status completed=0  --all=4
  chat:     default=4  --status completed=4  --all=4      VERDICT: FAIL — views disagree
post-fix (/home/user/workspace/_tmp/archvis-evidence-final.txt)
  terminal: default=0  --status completed=0  --all=3
  chat:     default=0  --status completed=0  --all=3      VERDICT: PASS
```

The `--all` totals differ by one because each run seeds a fresh pair of rows
(the sandbox ledger is cumulative: 2 rows per run).

## Root cause (5-why)

1. The archived row rendered in the chat view → the marker-bearing row reached
   the screen unfiltered.
2. Unfiltered → the drop was applied by the **caller**, not computed inside the
   render: `cmd/agent/cli_tasks.go` `runTaskList` did `if !opts.all { result =
   tools.FilterArchivedRows(result) }`.
3. Why that mattered → **there was a second caller of `tools.TaskList`**:
   `cmd/agent/commands.go` `runSlashTask` (`/task list`) called it bare.
4. Why a second caller could exist at all → the visibility predicate was not
   owned by the package that owns the datum. `FilterArchivedRows` was public and
   OUTSIDE the render path, so "did this view filter?" was a property of each
   call site — an invariant with as many spellings as there are callers.
5. Why it survived → every existing test pinned ONE face (`cli_tasks_test.go`
   for the verb, `tasks_archive_test.go` for the engine), so no test ever
   compared two views of the same row.

The failure mode is structural, not a typo: a rule that lives at the call site
is a rule the next call site will not know about.

## The fix: one funnel

`internal/tools/tasks.go`

```go
// TaskListVisible renders the ledger the way EVERY operator-facing view must …
func TaskListVisible(statusFilter string, includeArchived bool) (string, error) {
	rendered, err := TaskList(statusFilter)
	if err != nil {
		return "", err
	}
	if includeArchived {
		return rendered, nil
	}
	return FilterArchivedRows(rendered), nil
}
```

Both faces now come through it:

- `cmd/agent/cli_tasks.go` `runTaskList` → `tools.TaskListVisible(opts.status, opts.all)`
  (the `if !opts.all` at the call site is replaced by the argument).
- `cmd/agent/commands.go` `runSlashTask` → `tools.TaskListVisible(opts["status"], opts["all"] == slashTaskFlagSet)`.

`TaskList` stays the RAW render on purpose: its row-level `[resolved]` marker is
the datum `FilterArchivedRows` reads, and `task show` / `task search` keep the
full ledger (a tombstone is hidden from LIST views, not from the ledger).

### The slash view gains the shared flag, not a private one

`/task list --all` is parsed by `parseSlashTaskArgs` into `slashTaskAllFlag`
(`"--all"`, the same spelling `taskFlagMember` admits for the CLI verb) with
`slashTaskFlagSet` as the presence value — so the two parsers agree on the
flag's NAME and the one funnel agrees on its MEANING. `taskCommandUsage`
(`/help task`) documents it in the same commit.

## Help text synced (the promise was wrong on both sides)

- `tasksUsage` (`gopi task -h`): the old line scoped the behaviour to
  `--status failed` ("it only leaves the default --status failed view") — false
  already, since the CLI filter ran for every status. It now states the rule the
  code implements: hidden from every list view, `--all` the one spelling that
  returns the tombstone, predicate named as `tools.TaskListVisible`.
- `taskCommandUsage` (`/help task`): now lists `--all` and the archived-row rule.

## Not Done (honest)

- The epic's stated reading ("archived `completed` rows vanish under
  `--status completed` but reappear in the default view") does **not** reproduce
  on the pre-fix binary in this repo: `runTaskList` filtered for EVERY status,
  default included. The inconsistency that DOES reproduce, and was fixed, is
  **terminal verb vs `/task list`**; the consolidation the epic asked for
  ("let the default view also go through FilterArchivedRows") is reached in its
  stronger form — a single funnel both views call. Recorded here so a later
  reader does not chase a phantom on the CLI face.
- The two list-family comment blocks in `internal/tools/tasks.go` were
  re-read rather than churned: `taskHashDisplay`'s (`#` is the row marker
  "every render-side consumer keys on (FilterArchivedRows, …)") and
  `TaskListOrderNote`'s ("FilterArchivedRows rewrites only the header's leading
  integer") both name the FILTER as a consumer of the render — neither claims
  the CLI applies it, so neither is stale after this change and no edit was
  made. The authoritative statement of the funnel sits on `TaskListVisible`.

## Verification

See the task report (`task-report.md`, this session's directory) for the
build/test/run block and the self-filed evidence lines.
