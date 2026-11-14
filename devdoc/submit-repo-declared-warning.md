<!-- doc-class: normative -->
# The submit seam now says when a repo declares no build command

## The defect

Two checkouts of this project exist on this host under names that differ only in
case:

```
/home/user/work/GoPi    (uppercase) — history root d5736e9ac, tip 0592c9ae4 (2026-10-18)
/home/user/work/gopi    (lowercase) — history root 964b2404b (a fresh snapshot taken 2026-10-18)
```

They are **unrelated histories** (`git merge-base` exits 1) and the lowercase one
was created as a source snapshot of the uppercase tip
(`964b2404b initial: source snapshot at 0592c9ae4, no history`), so the uppercase
checkout is a frozen predecessor whose history simply is not carried forward.

The settings `repos` table declares a build command for the **lowercase** path
only:

```
repos     {"/home/user/work/gopi":{"build":"CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/","build_timeout_sec":600}}
dev_repo  "peer1:/home/user/work/gopi"
```

`task submit` without `--repo` stamps the **process CWD**
(`taskSubmitResolveDefaultRepo`, `internal/tools/tasks.go`), so a submit typed
while standing in the uppercase checkout records that path. It then passes the
**one** repo check the submit seam performs —
`git rev-parse --is-inside-work-tree` (`taskRepoVerifiable`) — because the path
really *is* a git work tree. The row queues, and dispatch refuses it later and
terminally:

```
clone channel: repo "/home/user/work/GoPi" declares no build command in
settings repos → /home/user/work/GoPi.build — refusing to push a workspace
that was never built
```

That is row `01a159a9-ecb8-7664-8beb-57438ac4023f`, and 863 more rows stamping
the same path (measured: see the audit below).

## Why the existing repair arms never fired

Both were checked, and both are correct for a *different* question:

| mechanism | question it answers | why it is silent here |
|---|---|---|
| `DurableRepoForSubmitWithNote` | is this path the run's ephemeral GROUND? | `/home/user/work/GoPi` is a main checkout, so the answer is identity — no error, no note |
| `RepairDurableRepoForSubmit` | is this a DEAD workspace path? | it runs only on the failure arm above, and it only touches paths under the workspace root |
| `taskRepoVerifiable` | is this a git work tree? | it is, so it passes |

The gap is that **"is a work tree" and "declares a build contract" are two
different questions**, and only the first was asked at the seam where the
submitter is still looking at their command.

## The change

`internal/tools/task_submit_repo_declared_warning.go` — a
`TaskHermesHostWarning`-shaped **warning** (never a refusal), appended to the
submit confirmation beside the host-ownership and base-staleness warnings
(`internal/tools/tasks.go`, the `TaskSubmitReturningID` confirmation block):

- it reads the table through `config.ResolveMergerRepos()` and the channel
  through `config.CloneChannelConfigured()` — the same two readers the refusing
  machinery uses, so the warning can never disagree with the refusal it predicts;
- it is **silent** when the settings file cannot be read ("I could not look" is
  not evidence of an undeclared repo);
- it states **which channel is in force**: with `dev_repo` set the row will be
  refused; with `dev_repo` empty the worktree channel asks no build contract and
  today's run would work. A warning that claims a refusal that cannot happen is
  a false alarm;
- it does not care whether the path exists — `internal/devworkspace` already
  answers that with its own sentence (`RepoRefusalDeadPathFmt`).

Consequence, measured with the real CLI on a sandboxed profile:

```
$ GOPI_HOME=$SB gopi task submit "probe" --repo /home/user/work/GoPi
Task queued: 01a16970-a045-74ea-9f93-484463af4939 (status=pending, source=agent, tags=—)
warning: repo "/home/user/work/GoPi" is not a key of the settings "repos" table, so no build command is declared for it — the git check above passed only because the path IS a work tree, which says nothing about its build contract. dev_repo is set, so this row's run takes the clone channel, which refuses it: clone channel: repo "/home/user/work/GoPi" declares no build command in settings repos → "/home/user/work/GoPi".build — refusing to push a workspace that was never built. Declared repos: /home/user/work/gopi. Declare this path's "repos" entry, or submit against a repo that is already declared (see CONFIG.md).
```

(The same warning fires on the **cwd-default** arm — the mechanism that actually
produced the 864 rows — because the default resolves into the same `repo`
variable the warning reads.)

## Auditing the ledger

`sqlite3` is not installed on this host (GOPI.md §3), and `gopi task list` does
not print `deliverable`, so the read-only audit is a small Go program against
the ledger. It lives in this run's session directory, not in the repo (Rule 35:
an operator one-shot, not a new tracked tool):

```
/home/user/.gopi/sessions/01a16519-55c6-7c8d-a23d-9b055cce4a87/q/repoaudit/main.go
```

Run it from a checkout of gopi so the `modernc.org/sqlite` module resolves:

```bash
go run ~/.gopi/sessions/<session>/q/repoaudit/main.go \
   -db ~/.gopi/memory.db -settings ~/.gopi/settings.json -status live
```

`-status live | terminal | all` selects the population; the summary line names
how many rows were read and judged, and an empty judgement is printed as a
warning rather than as a clean result (Rule 15 / Rule 36's shape).

Measured 2026-10-23 on this ledger (1805 rows):

```
read 1805 row(s); judged 1165 with a deliverable.repo; 640 no repo;
951 NOT in the "repos" table (950 of them already terminal)
```

with the offending paths grouping as: **864** × `/home/user/work/GoPi`,
**46** × a reclaimed `/home/user/workspace/<task-id>`, and the rest one-off
foreign paths (`/home/user/Downloads/games/Maps`, `.`, …). The **live** (non
terminal) population is exactly **1** row.

## What this does NOT do

- it does not repair any historical row (the ledger is append-only; the 73
  cancelled rows and the 864 completed ones stay as they are);
- it does not delete or move `/home/user/work/GoPi` — that is an operator
  decision, and the three options with their consequences are in the task
  report;
- it does not add a tool, a verb or a resident process (Rule 3 / Rule 35).
