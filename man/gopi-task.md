---
title: GOPI-TASK
section: 1
header: GoPi Manual
---

# NAME

gopi-task - the task-as-memory ledger (cross-session traceability and audit)

# SYNOPSIS

<!-- BEGIN SYNOPSIS (copied verbatim from `gopi <verb> -h` by internal/standards/mancensus) -->

```
  gopi task <sub> [args]
```

<!-- END SYNOPSIS -->

# DESCRIPTION

**gopi task** is the ledger of work items. The ledger lives in the SAME
`memory.db` as `distilled_qa` and `memory_feedback`, so a task record survives
its session — and a backup, a sync or a restore of the knowledge base carries
the task history with it.

It is an **audit layer, not runtime control**: the ledger does not touch the
agent loop's own task list. It is **append-only** — there is no delete, the
summary is immutable, and a briefing revision is itself an audit record
(`payload_revised`, carrying the old and new sha256).

`gopi task` **manages** rows; it does not run them. The only command that runs a
row is `gopi worker` (`man gopi-worker`).

`gopi task list` marks the row a dispatcher would take next with `[next]`, and
an unclaimable one with `[queue-skip]` — claim order is priority-first, not the
render order, so the two markers can disagree with the order on screen and are
right to. A live row ends with its **ground**: the branch and the worktree
directory its run works in, read off the row's recorded deliverable, so
task ↔ branch ↔ worktree are comparable in one read. The branch name IS the
task id, which is what makes `gopi task show --branch` a lookup rather than a
guess: the row is addressed by the deliverable it records, through the same
resolver, so the id read off the ref and the id read off the ledger are one
value.

Moving a row between machines WAS a family of its own (`send`, `receive`, `pull`,
`sync`, `harvest`, `push`) and is **RETIRED** (2026-11-05): no verb in this binary
opens a session against another node's ledger. Work that must run on another
host is FILED there — `gopi task submit "<briefing>"` on that host, which stamps
that host's own repo and ledger — and work crosses hosts through the GIT ref a
row's deliverable publishes to. The eight spellings still dispatch, each to a
removal sentence; see RETIRED: MOVING A ROW BETWEEN MACHINES below.

> **Retired mechanism (merge verb removal, 2026-11-01).** the top-level
> `gopi merge` command is gone; the word still routes, answering with that
> refusal, and the surviving trigger is `gopi task merge`.

The git-infrastructure atoms (merge, merged-cleanup, adopt, audit-merges) and
the merge machinery are documented by `man gopi-merge`; the executing service by
`gopi worker -h`. The resident merger daemon is **retired** — `gopi merger`,
`gopi task merger` and `gopi task serve` refuse and exit 1. Merging itself is
not retired: a merge pass is `gopi task merge`, and the top-level `gopi merge`
spelling was removed (epic merge-verb removal) — the word still answers with
that refusal rather than "unknown command".

`gopi task clone` is the **clean-room** copy. The ladder has TWO rungs, and a
failed row's two honest exits are exactly those two. Re-entering the SAME
recorded ground with the SAME session is the first (`gopi session resume
<id>`, `gopi worker --resume`, `gopi task status <id>`); **clone** is the
second, for when that ground is no longer trustworthy — a NEW id, a NEW
session, a NEW directory. (A third verb that used to sit between them was
retired: it minted this verb's copy — new id, new session, new directory —
under a name and a `--reason` flag shaped as if it CONTINUED the failure, so
operators reached for it to continue work and got a fresh ground with the
context abandoned. Its mechanics are this verb's; the intent it advertised is
`session resume`'s.) The successor is `<id>-cN`, parented to the source, and
it carries the source's briefing **verbatim** behind a provenance header, its
tags, its `deliverable.repo` (inherited, or `--repo PATH`) and its attachments
re-materialised in the successor's own session directory. It carries **nothing
run-allocated** — `worktree`, `branch`, `grounds` — so its ground is derived
from its OWN id and is never the source's directory: that is the one claim the
verb makes, and the line it prints states the ground it promises.

The successor also carries a `repo`, because a row with no `deliverable.repo`
cannot resolve a **ground** and a worker would refuse it — the copy would
inherit the failure instead of the ability to run. The repo is inherited from
the source; `--repo PATH` overrides it (resolved to its durable main checkout,
so a linked worktree argument lands on the repository). `worktree` and `branch`
are **never** inherited: they are allocated by the run that owned them, and a
successor claiming one would block that ground's real owner. When neither an
override nor a source repo exists the successor is still minted and the command
says `carries no deliverable.repo`, with the remedy — an unstartable successor
is stated, never silent.

The source row's **`attachments` manifest is carried too**
(`reopen-carries-evidence`): each replica is re-materialised inside the
SUCCESSOR's own session directory and its digest re-measured from the copy that
landed, so `gopi task show <successor>` renders real files rather than the
false-claim marker. What the command prints says which way it went —
`attachments=` names the count carried, and a row that had none prints the
"none" marker. A payload that says "see attachment" and a manifest that is empty
is not a rerun: it is a pointer back at the source row.

The **status gate is the design**. A row that may be RUNNING (`in_progress`,
`claimed`) is refused — its ground has an owner, and a clone would put a second
run beside the first. A row that has never run (`pending`) is refused — nothing
can be distrusted, the ground is UNUSED rather than dirty, and the remedy is to
run the row. `completed`, `failed` and `verified` are cloned: their run is over,
which is exactly the state "the room may now be dirty" describes. `cancelled`
is refused unless `--confirm-cancelled` says the abandonment is being reversed,
because minting work from an abandoned row silently would resurrect a decision
somebody already made. A source that already has a **live** clone is refused and
the live row is named: that row already carries the work, so a second one would
dispatch it twice. `--dry-run` prints the whole plan — the id it would mint, the
repo, the ground, the payload size — and writes NOTHING.

Its `--reason` is **optional**: a clean-room copy repeats the same work, so
there is no failure it must carry. When given, it rides the payload as a
provenance note.

# AUDIT VERBS AND THEIR EXIT CODES

The read-only audits answer different questions and their codes are
deliberately distinguishable, because a caller has to tell "the answer is no"
from "I could not look".

`gopi task dedup-scan` reports rows that look like one another — the twins are
recognised by payload hash, canonical hash and trigram similarity — and `--live`
narrows the report to claimable rows. It is report-only and exits **2** when
twins were found, which is the CI-visible half of its contract.

## The four retired audits (2026-10-30, task 01a18799)

`for-branch`, `branch-refs`, `orphans` and `stranded` are **retired spellings**.
They are not deleted: each still DISPATCHES and answers with one sentence naming
what it did and where its reader goes instead (they live in
`taskverbs.RemovedVerbs`, the same group `run` and `sweep-zombies` are in), and
each exits non-zero — nothing was measured, so a script that gated on one of
them must not read a clean status. They are advertised nowhere, carry no entry
on `gopi task -h`, and own no implementation. What they were:

- `for-branch <branch>` — the branch→row lookup. It was a facet of the retired
  worktree+branch delivery semantics; its "every owner" answer was reconstructed
  from rows the current model no longer produces, because a branch IS a row's
  ledger address. **The question survives as a flag on the read**:
  `gopi task show --branch <branch>` (or `--worktree <path>`) names the row that
  records that deliverable, through the SAME resolver (`tools.TaskForBranch`),
  and `gopi task status --branch` answers the state instead of the digest. The
  code that answer uses — **4**, "the ledger was read and no row records it" —
  is still `tools.TaskForBranchExitNoOwner`, still told apart from **1**
  ("the ledger could not be read at all"), because "I could not look" must never
  render as "nobody owns it".
- `branch-refs --peer ALIAS` — the peer-ref audit. It asked the peer's own ref
  advertisement for every terminal row's deliverable branch, i.e. it made a
  claim about a delivery channel this repository no longer runs. The ref facts
  are still readable from their owners: `git ls-remote --heads <peer>` (what a
  peer advertises), `git for-each-ref refs/heads` (what this host holds),
  `gopi task sweep-worktrees --apply` (the ownership-independent worktree
  reap) and `gopi task merge` (the merge sweep).
- `orphans` — the orphaned-deliverable readout over the same channel. A
  terminal row's record still names its branch and repo (`gopi task show <id>`),
  and the ref itself is answered by git: `git -C <repo> for-each-ref
  refs/heads/<branch>` and `git -C <repo> log --all --grep <id>`.
- `stranded` — the contradiction readout over LIVE rows (the row's own record
  says the run is over while the ledger calls it live). Both halves are still on
  the read: `gopi task show <id>` prints the ledger status AND the advisory
  ground line for the same row, and a run log is a file —
  `/home/user/.gopi/sessions/<task-id>/output.log`. `gopi task list --status pending`
  marks an unclaimable row `[queue-skip]` with the parent gate's own reason.

The one thing that did NOT die with those files is the shared instrument they
happened to contain: the reachable-history reader ("does any commit reachable
from a ref NAME this task id?") now lives in
`internal/tools/task_ref_history.go`, because its readers are the completion
witness and the deliverable-tip probe — questions about rows that have nothing
to do with the retired readouts.

# FLAG CONTRACTS

`gopi task submit --source` names the row's ROLE. Omitted, it is derived from
this process's own evidence; an explicit `--source human` that the
`GOPI_HUMAN_TERMINAL` gate denies is **refused**, never stored as something
else.

`gopi task submit --fixture` marks the row **queue decoration** — a rig's proof
ledger, a demo, a screenshot. It carries the reserved `fixture` tag, which the
shared claimability predicate refuses BY CONSTRUCTION, so the row shows as
`[queue-skip]` and no dispatcher will ever spend a run on it: a claim cannot be
recalled by whoever seeded the row, which is why the refusal is structural
rather than a convention.

`gopi task submit --parent-task-id independent` — the SAME word means the SAME
thing on both faces, and this page is where the two are reconciled. On the
`task_submit` TOOL the value is a **sentinel**: the harness chains a run's
submissions onto one another, and this spelling suppresses that link for the one
row that carries it. The CLI never chains, so on this face the sentinel has no
chain to suppress — and it resolves to the outcome it names anyway: the row goes
out with **no parent**, exactly as omitting the flag does, and one line on stderr
says so. Mixing the spelling with a real id is **refused**: "no parent" and
"these parents" are contradictory instructions about one column, and the two are
never resolved silently. Before 2026-11-07 the value was passed to the engine as
if it were a task id and the submit died on
`parent_task_id "independent" does not exist` — a sentence that sends its reader
looking for a mistyped id, never at the other face.

`gopi task update --force` is the operator's **delivery-proof escape**, and it is
the CLI face of `GOPI_TASK_DELIVERY_PROOF_ALLOW` — the same decision, typed. It is
NOT a master switch and it is not "skip the check":

- the measurement still runs. The arm reads the row's tip (its recorded ground's
  HEAD) and its declared destination, and when the tip is in neither the ancestry
  nor the patch set of that destination the verdict is **undelivered**;
- the escape turns the REFUSAL into a RECORD. The write proceeds, one line goes
  to stderr naming the source that asked (`--force`, the variable, or both), and
  the row's trail carries `delivery-proof-unverified`, which `gopi task show`
  renders as its `verdict:` line. A **proven** close writes no such record at
  all, so "proven" and "let through by decision" are distinguishable on every
  surface that renders the row;
- it waives nothing else. The dirty-close-out, residue, tip and merge-backlog
  gates do not see it, and it cannot reach any write path but this one: the
  harness writeback, the merger and the lifecycle tick call other entry points,
  so no run inherits a release nobody typed.

Use it to close a row whose delivery is known to be out of band (an operator
move, a manual push, a hand-verified artifact). Leaving the row OPEN is the other
honest answer, and it keeps the row on the delivery queue.

Each ledger verb's synopsis line in `gopi task -h` carries that verb's FULL
flag list, so the page stays a complete syntax index and the reasoning lives
HERE. The reasoning behind the flags is a third document, cited by both,
`devdoc/gopi-task-ledger-architecture.md` (with
`devdoc/task-parent-release-statuses.md` for `superseded` and Rule 33's
dependency gate).

# EXIT STATUS

Exit codes are per sub-verb, because they answer different questions and the
difference is deliberate. The deliverable selector that `gopi task show` and
`gopi task status` both carry (`--branch`, `--worktree`) distinguishes **4**
(the ledger was read and no row records this deliverable — the named "no owner"
answer, the one code
`tools.TaskForBranchExitNoOwner` carries so a script can tell this ANSWER from a
failure) from **1** (the ledger could not be read at all): "I could not look"
must never render as "nobody owns it". That code pair was published by the
retired `for-branch` and is unchanged by its retirement — the same resolver
answers it.

**The retired verbs answer 1 and nothing else.** `verify`, `retry-delivery`,
`reconcile` and the four read-only audits (`for-branch`, `branch-refs`,
`orphans`, `stranded`) still DISPATCH (they are in `taskverbs.RemovedVerbs`),
and each prints ONE sentence naming what it did and where that question is
answered now, then exits **1** — nothing measured, nothing claimed. The
per-verb codes those spellings used to publish (**4** a verification cap that
expired, **5** a child that stopped without a verdict, **2 / 3 / 6 / 7** the
peer-facing audit answers) went with them: they described a delivery channel
this repository no longer runs, and a spelling that answered them would be
making a claim its reader could not act on.

# RETIRED: MOVING A ROW BETWEEN MACHINES

**The surface is DELETED (2026-11-05).** Eight spellings used to form a
cross-node family: `send`, `receive`, `pull`, `sync`, `sync-pass`, `diff`,
`harvest` and `push`. Every one of them reached ANOTHER node's ledger — over ssh,
through a declared `sync_peers` topology — and this binary no longer does that.
They are in `taskverbs.RemovedVerbs`: each still DISPATCHES so an old script, a
task row or a shell history hears what happened, prints one sentence naming
where the capability went, and exits **1**.

**Where the capability went, by INTENT rather than by spelling.**

- *Put this row on the other machine* — there is no second ledger to put it in.
  A row that must be WORKED on another host is FILED there: `gopi task submit
  "<briefing>"` on that host, which stamps that host's own repo and ledger
  (Rule 28 / Rule 40). Nothing on this host reads, waits for, or merges the row
  you meant to send, so filing it where the work belongs is not a workaround —
  it is the only shape that survives.
- *Get the other machine's finished work* — that crosses as a GIT ref: a run's
  own close-out publishes its clone's `master` (the clone channel's delivery
  leg), and the peer's `master` is what you fetch. No verb is involved.
- *Audit this ledger for twin rows* — the one capability of that family that was
  never a peer protocol. It is a LEDGER verb now: `gopi task dedup-scan`
  (its own `-h` page carries its flags; `--db <PATH>` chooses the ledger).

**What the family's contracts were**, kept here because this page is the record
a reader consults after meeting a removal sentence — `send` put a row on a
peer's ledger, `receive` was its pull half, `pull` was `receive` over a
comma-list with one verdict line per id, `sync` was one bidirectional delta
session with a whitelisted peer (`--peers` / `--status` widened it to every
declared peer), `sync-pass` ran one whole cross-node pass and exited, `diff` was
the read-only reconciliation self-check, `harvest` collected the peer's rows and
`push` pushed this node's branch refs. The peer topology was **declared, never
discovered** — `settings.json` `"sync_peers"` / `GOPI_SYNC_PEERS` — so an
unknown alias was refused **by name** rather than producing an empty successful
run; `task_sync` (`GOPI_TASK_SYNC`) gated the automatic crossings a
`sync-pass` tick made on its own, and a verb an operator typed ran with the
switch off.

**The settings keys and the switch survive; the verbs do not.** `sync_peers` is
still read (the adoption sweep and the ref-lag measurement consult the declared
topology), so a settings.json carrying it is not an error — nothing in this
binary opens a session with it. There is **no interval variable for the git/ref
leg**: this page used to say one still set that cadence, and it was wrong — the
variable it named was advertised by the catalog and `gopi -h --env` while no
production path read it, and it was removed on 2026-11-05 together with the
serve-tick auto-sync interval. A pass that had no owner, and no cadence knob,
is not a pass.

# SEE ALSO

`gopi task <sub> -h` (every sub-verb answers its own page), `gopi -h`,
`man gopi`, `man gopi-worker` (runs a row), `man gopi-merge` (files conflicted
branches as rows), `man gopi-memory` (the same database).
