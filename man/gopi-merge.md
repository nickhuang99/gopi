---
title: GOPI-MERGE
section: 1
header: GoPi Manual
---

# NAME

gopi-merge - the merge engine's contract: how a branch is decided, what the caps are, and what the exit status means

# SYNOPSIS

<!-- BEGIN SYNOPSIS (hand-written for a page with no `-h` source: the word this page is named after has no help page — see the retirement note in DESCRIPTION) -->

```
  gopi task merge <branch>...     decide ONLY the named branches, then stop
  gopi task merge --all           sweep every local branch except the
                                  integration branch
  gopi task merge --repo PATH     act on this repo instead of the cwd
  gopi task merge --submit        route conflicts into the ledger for an
                                  executor agent (default: report only)
  gopi task merge --db PATH       override the memory db path
```

**A BARE `gopi task merge` is REFUSED** (exit 1, `missing required parameter: …`)
as of 2026-11-06 (task 01a1ac9e). It used to sweep every repo in the
`settings.json` `"repos"` table — the arm still exists and is what `--repo PATH`
drives, and the repos table remains the merger tick's own sweep list — but the
CLI no longer runs a decision nobody named. Measured on this host, the bare
spelling took the merge lock and started a build gate on the operator's live
working checkout while printing nothing about which repo it was deciding.

<!-- END SYNOPSIS -->

# DESCRIPTION

> **Retired mechanism (merge verb removal, 2026-11-01).** This page was named
> after the top-level `gopi merge` command, and that command is GONE: its
> implementation and its flag parser were deleted, the name now answers with a
> refusal and exit 1:
>
> ```
> gopi: `merge` is RETIRED — use `gopi task merge` (`gopi task merge -h` for its help).
> To send the word as a prompt instead, say so explicitly: `--prompt merge`.
> ```
>
> The surviving trigger is the family spelling `gopi task merge`. What follows is
> the ENGINE's contract, which is unchanged and still worth reading — but every
> spelling below is `gopi task merge`, and the flags this page used to document
> (`--fast`, `--no-verify`, `--every`, `--stop`, `--status`, `--already-landed`,
> `--max-context-tokens`, `--max-conflicted-files`, `--handover-budget`) were
> deleted WITH that parser and are not accepted by any surface.

**`gopi task merge`** walks every repository in the `settings.json` `"repos"`
table and decides every branch with `git` where `git` CAN decide. A branch
already in the integration branch is pruned; a fast-forwardable branch is landed
and pruned; a branch that does not fast-forward is a conflict, and a conflict is
a LEDGER ROW — `conflict` when nothing is submitted, `dispatched` when
`--submit` files a `merge-conflict` task for an executor — never a child process
spawned in-process: the engine runs exactly one `exec.Command`, and it is `git`
(`git grep -c 'exec.Command' internal/tools/task_merge.go` → `1`).

Naming branches on the command line narrows the run to exactly those names — in
every repo that holds one. A repo holding none is skipped whole, and a name that
matches nothing anywhere is refused rather than silently ignored.

**The walk, in full.** Every branch is decided with `git` where `git` can decide
alone: a branch already in the integration branch is pruned; a fast-forwardable
branch is landed and pruned; a branch that does not fast-forward is a conflict.
A conflict on a DECLARED append-only manifest (`devdoc/DOC-INDEX.tsv`) is refused
by the manifest-duplicate gate (⓪d) before it can land — measured: a branch whose
union would carry one key twice is refused as `MANIFEST-DUP gate`, with the
coordinates the standards gate prints (`<file>:<line>: duplicate row for <path>`).
There is NO union-RESOLUTION lane: `union-resolved` is retired vocabulary from the
removed one-shot merge command, and it appears in no Go source on this tree —
measured (`git grep -n 'union-resolved' -- '*.go'` → no hits). A reader who meets
the word in an old report is reading a promise this engine no longer makes: a
conflict it cannot resolve by `git` alone becomes a ledger row, never a silent
union.

The integration branch, a branch held by a worktree and an `archive/<name>` tag
are never candidates, and are reported as skipped/untouched: the candidate set
comes from `MergerSweepPreview`, the SAME classifier the merger tick runs, so the
manual face and the tick cannot disagree about what is a candidate.

A branch sharing NO commit with the integration branch (an orphan root) cannot be
merged at all: `git` refuses it outright, so there is no conflict to measure and
nothing to file. The run reports it `error` with git's own sentence, exit **2**,
and leaves the ref as it found it — measured on an orphan-root fixture:
`merge orphanb  error  merge orphanb failed (no conflict state to abort): … exit
status 128: fatal: refusing to merge unrelated histories`, `failed=1`. It is NOT
reported `unrelated-history`: that word is the ARCHIVE and pushback vocabulary
(`ArchiveReasonUnrelatedHistory`), not an outcome of this face. Merging such a
history stays a deliberate manual step
(`git merge --allow-unrelated-histories`).

**Two ceilings, two subjects — RETIRED, recorded not promised.** This paragraph
describes the deleted top-level verb. The ceilings were named constants in that
verb's own implementation (measured: `--max-context-tokens`,
`--max-conflicted-files`, `--handover-budget`, all three REFUSED by
`gopi task merge` with exit 1), and the lane that filed a stopped branch
(`merge_ceiling_rework.go`) went in `c8452ee0`. `gopi task merge` does not
measure a diverged branch: it hands it to the conflict lane. The reasoning is
kept because it is the argument for any future ceiling — the token ceiling
bounds the RUN (one giant conflict must not consume the context every other
branch needs); the conflicted-file ceiling bounds the BRANCH (a conflict spread
over dozens of files is a rewrite wearing a conflict's clothes).

The run is **foreground and stateless**. It takes the same single-instance
`flock` the repository's other merge writers take, holds it for the duration of
the run, and releases it by process exit and by nothing else — so a crashed or
killed run leaves nothing to sweep out after it. There is no supervisor, no
progress record, no pid file and no stop flag, and the `--every` loop that once
published such a record went with the parser. The resident merger daemon is
retired: `gopi merger` and its aliases refuse at every spelling.

A repository whose `"repos"` entry declares a build is rebuilt after this run
lands anything in it; a repository that declares no build is a pure merge target
and is skipped silently — never a missing build, never a guess.

Post-pass verification — RETIRED with the verb, recorded here because this page
promised it. `gopi task merge` does NOT run a `go build`/`go vet` verification
pass over what it landed, and it does not withdraw a batch: the lane that did
(`internal/tools/merge_pass_verify.go`, with `CaptureMergePassScope`,
`VerifyMergePass`, `WithdrawMergePass` and the `merge-withdrawn` alarm) belonged
to the retired top-level `gopi merge` verb and was DELETED on 2026-11-01 — its
last production caller, `cmd/agent/merge_run.go`, had already gone. Measured on
this tree (`git grep -n 'CaptureMergePassScope\|VerifyMergePass(' -- '*.go'`):

```
cmd/agent/cli_tasks.go:4573:// built by CaptureMergePassScope, in merge_pass_verify.go, which never needed
```

One line, and it is a COMMENT. The file is gone, the functions are gone, and the
number of lines that CALL either function is zero. A landing that does not
survive is not reported by this engine; it is found by the suite of whatever
lands next. **`gopi task merge` does NOT verify the merged tree and does NOT
withdraw a landing**: a landing stands or a landing is refused, and there is no
post-landing measurement to turn a landed batch back. Restoring that guarantee is
unfinished work, not a documented behaviour — `devdoc/merge-verify-withdraw.md`
records the mechanism that used to provide it (historical) and
`devdoc/merge-verb-removal-inventory.md` records the deletion. The build step
that DOES still run is the per-repo rebuild below (Rule 12).

**What `BURIED` means, and which command decides it.** `gopi task audit-merges`
reports the merge backlog (measured: `merge-backlog audit (report-only):
scanned=N submitted=N duped=N skipped=N failed=N`); `gopi task archive --check`
classifies the ARCHIVED arm (measured: `archive backlog: <repo>` then
`N reason-less archive tag(s) over a completed/verified task row`). Both read
only: no lock, no run, no write.

**BURIED** = the branch is out of every merge path while its
owning ledger row says the work FINISHED: the work is landed or abandoned in the
ledger's view, yet the branch still exists and no merge path will ever pick it
up again. For the classification that names which of those are re-dispatchable —
reason × owning row × whether the work is on the integration ref at all — run
`gopi task archive --check`.

**The lock is taken per run, and a REFUSED lock is FATAL.** A held lock means the
run exits non-zero having decided nothing, so a hook that fires a merge pass gets
a status it can tell apart from a landing that went red. A crash or a SIGKILL
releases the lock too, so there is never a lock to sweep out after a dead run,
and the lock is not residency.

**Image self-check** — LIVE, but it is the WORKER face's mechanism, not this
engine's (this page carries it because the epic that built it started here; the
page that promises it is `man gopi-worker`). A resident face checks, before it
decides anything and again on every tick, whether the process is still running
the artifact at the path it was started from: the kernel's own "(deleted)" marker on `/proc/<pid>/exe` (the file
was replaced or unlinked under a running process) or a build revision that no
longer matches the artifact on disk (a rebuild written in place). The worker's
claim loop reports a stale image and keeps ticking (it also carries the
housekeeping ticks, which a stale image performs correctly). Nothing is created
by this check: no state file, no timer, no watcher. The remedy is always to
restart the process. Why the check exists, the two arms it stands on, and the
measurements behind it (the incident, the `(deleted)` marker, the
in-place-rebuild shape, the two real runs): `devdoc/merge-image-selfcheck.md`.

# EXIT STATUS

Measured 2026-11-01 against the built binary, pipeline-free (`cmd >/tmp/o 2>&1;
echo $?` — through a pipe `$?` is the pipe's):

`0`
: every branch reached a terminal decision — a branch filed for rework included.

`1`
: the run could not be carried out at all, or something was left UNDECIDED. The
live arms, each measured (`cmd >/tmp/o 2>&1; echo $?` — never through a pipe, or
`$?` is the pipe's):

- a flag the atom does not admit — `gopi task merge --bogus` → **1**, `unknown flag --bogus`;
- a `--repo` that is not in the `repos` table **when no branches are named**
  (`gopi task merge --repo /tmp/somewhere`) → **1**, and the message names the
  settings file and the configured count. With branches named this arm is not
  reached — the per-branch arm below answers first;
- the single-instance lock refused (the refusal names the holder; the branches
  stay put for the next pass);
- a conflict (report-only or dispatched), and every refusal arm — contract-refused,
  build-refused, **head-off-integration** (measured: an explicit branch from a
  checkout on another branch → **1**, `head-off-integration=1 failed=0`).

A refusal is a non-zero exit for one reason: a script or a post-receive hook
reading this face must not be told "decided everything" while the work is still
in the queue. (Until 2026-11-01 one more arm fired here — a post-pass
verification that went red. That verification was deleted with the retired
top-level `gopi merge` verb and `gopi task merge` never had it; see the
DESCRIPTION note, and note that no exit code is left for it.)

`2`
: a per-branch run ERROR — the engine's `failed` tally is non-zero. Measured:
`gopi task merge no-such-branch` → **2** with `failed=1`, the line saying the ref
does not exist and no ledger row names it. It outranks the `1` arms in the
source's own switch (`cmd/agent/cli_tasks_merge.go`), so a run carrying both
reports `2`. This is an error, not a decision: the branch was neither merged nor
filed.

# SEE ALSO

`man gopi-task` (the ledger this engine files its conflicts into),
`man gopi-worker` (the only command that runs a row), `gopi task merge -h` (the
surviving trigger's own page), `gopi -h`.
