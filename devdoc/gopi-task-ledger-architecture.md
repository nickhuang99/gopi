<!-- doc-class: normative -->
# The `gopi task` ledger — architecture the help page no longer carries

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Status:** landed (epic `01a10bb7` item 4/5, the CLI help-hierarchy restructure).
**Surface:** `cmd/agent/cli_tasks.go` (`tasksUsage`, the `gopi task -h` page).
**Page size:** 453 lines → 147 lines (`./bin/gopi task -h | wc -l`), with the
advertised surface provably unchanged (17 synopses, 59 advertised verb-flag
pairs — identical before and after; see *Verification* below). Of the 147, 79
are the per-verb description lines and 68 are the shared blocks (title, the
append-only rule, the verb list's own heads, the "Not ledger verbs" pointer,
`--db`, the status values, the tail pointer).
**Guards that keep it honest:** `cmd/agent/cli_tasks_help_surface_basic_test.go`
(the G1 surface gate: derivation == pinned expectation == covered set == the
inventory table), `scripts/g1-task-help-surface.sh` +
`devdoc/gopi-task-help-surface-inventory.md` (the regenerated inventory),
`cmd/agent/cli_help_hierarchy_reverse_guard_test.go` (every dispatched
sub-verb must be named by its owning page), `cmd/agent/cli_verb_census_test.go`,
`cmd/agent/cli_tasks_advert_gate_test.go` (no service/sync verb re-advertised
here).

## Why the page was cut

`gopi task -h` printed ~20 KB of prose — the biggest page in the CLI. The
operator's rule for this epic is one sentence per second-level verb plus the
address of the page that carries the detail; a verb you must already know in
order to find is the defect the restructure exists to remove.

**One honest property of THIS page, measured, because it shapes the pointer
wording**: unlike `gopi session` (where `gopi session log -h` really does print
a verb-specific page) or `gopi memory` / `gopi task sync` / `gopi merge -h`, the
17 ledger verbs have **no per-verb page**. *(The third example no longer applies:
`gopi merge` was removed by the merge-removal epic, 2026-11-01, so it has no help
page to be an example OF. The property this sentence measures — the ledger verbs
have no per-verb page — is unchanged and still measured below.)* Measured on the
built binary:

```bash
$ H=$(./bin/gopi task -h | md5sum); for s in list show for-branch status \
      tag-resumed archive update verdicts sweep-worktrees finish-cleanup \
      sweep-zombies search improvements dedup-scan submit verify reopen; do
    h=$(./bin/gopi task $s -h 2>&1 | md5sum); [ "$h" = "$H" ] && echo "$s SAME" || echo "$s OWN"; done
list SAME   show SAME   for-branch SAME   status SAME   tag-resumed SAME
archive SAME   update SAME   verdicts SAME   sweep-worktrees SAME
finish-cleanup SAME   sweep-zombies SAME   search SAME   improvements SAME
dedup-scan SAME   submit SAME   verify SAME   reopen SAME
$ ./bin/gopi task sync -h | wc -l    # the sibling that DOES have its own page

# MEASURED WHEN THIS TRANSCRIPT WAS TAKEN; `tag-resumed`, `verdicts` and
# `sweep-zombies` (§F2 of the inventory) and now `for-branch` are retired
# spellings — each still prints the family page (they are in RemovedVerbs and
# answer `-h` with the same SAME result), they are simply advertised nowhere.
# `reopen` is the one row this note does NOT cover: master 545b5b2 deleted the
# verb outright while this branch was being delivered, so `gopi task reopen -h`
# no longer prints the family page — it answers the generic unknown-subcommand
# dump. Re-take the loop with the CURRENT advertised list when the count
# matters.
137
```

**The page's Usage line therefore stopped claiming the circular pointer.** Until
this second pass it read `— every verb below answers 'gopi task <sub> -h'`,
which the all-SAME measurement above shows is false for all 17 verbs: running
that command prints this same page. The line is now the bare address, and the
pointer sentence names the document that really carries the reasoning.

`gopi task <ledger-verb> -h` prints this same family page, so a bare
`gopi task <sub> -h` pointer would send the reader in a circle. The page
therefore keeps each verb's **complete synopsis line** (the flags it takes) and
cuts only the *descriptive prose* beneath it — the pointer sentence says where
the semantics live (`devdoc/`), not "run this again".

## What moved off the page, and where each paragraph now lives

Nothing was deleted. Every paragraph the page used to carry is either:

1. **Still on the page** — because a gate or a behaviour depends on it, or
   because more than one verb reads it:
   - each verb's synopsis line (flags, unchanged, byte for byte);
   - the append-only rule, named in the page's own rule text (`NO delete`,
     `immutable`, `append-only`);
   - the two render orders and both queue markers (`newest first`,
     `priority-first`, `[next]`, `[queue-skip]`) and the `[hidden] ` disclosure
     marker — pinned by `TestTaskListHelpNamesBothOrders` and
     `TestTasksUsageIsAppendOnly`;
   - `--db PATH` (the family's common flag);
   - the status vocabulary (`pending → claimed → in_progress → terminal` plus
     `verified`) — the second pass folded the seven-row table into one line and
     moved the claim-lock rung's rationale HERE (§ *`claimed` — the
     crash-recovery window*), which is the paragraph that used to sit under it;
   - ~~the `for-branch` EXIT CODES table~~ — **GONE with its verb** (2026-10-30,
     task 01a18799: `for-branch`, `branch-refs`, `orphans` and `stranded` are
     retired spellings, `taskverbs.RemovedVerbs`). The four exit codes it
     promised did not disappear — `0` / `4` (no owner) / `1` (usage) / `1`
     (unreadable ledger) are still what the surviving `show --branch` and
     `status --branch` answer, and `TestTaskVerbStatusRunsAsAdvertised` asserts
     the `4` arm by name. What left the page is the ENTRY, not the algebra;
   - the verify default and its override channel (`1800`,
     `GOPI_TASK_VERIFY_TIMEOUT_SEC`) — the verify test reads the verb's OWN
     block out of `tasksUsage` and demands both spellings, which is why they
     stay on the flag line. The settings-key spelling
     (`internal/config.TaskVerifyTimeoutSettingsKey`) is off the page because
     the second pass dropped it: `CONFIG.md` §2c is where the three-channel
     precedence is documented, and no gate requires the page to restate it;
   - the update contract's three pinned strings (`--reason WHY`,
     `status-specific default`, `payload_revised`), each in the update entry's
     block (the block is delimited by the next blank line);
   - `improvements`' one-line promise ("run twice → identical"), its paragraph
     trim leaving that clause in place;
   - the "Not ledger verbs" pointer section (the advert gate requires the
     mention to survive).

2. **Recorded here** — the argument and the mechanics, which no gate needs on
   the page:

### The dispatcher's order is not the ledger's order

The ledger is one append-only table (`tasks`) in the same `memory.db` as
`distilled_qa` / `memory_feedback`. Two ORDERINGS read it and they are
deliberately different:

- **The ledger order** — `created_at DESC`, newest first. Every `gopi task list`
  view uses it, filtered or not; `--status` selects a population, never a
  reordering. That is why the dispatcher's next row is NOT the top line: it is
  marked `[next]` in the pending view.
- **The dispatcher order** — priority-first (bug before feature before docs,
  oldest first inside a tier). `gopi worker` / `gopi task serve` claim by it.

A row no dispatcher can claim carries `[queue-skip]`: no payload, merger-owned,
or a parent that has not been released (Rule 33's gate). **Both markers come from
ONE predicate** (`taskRowClaimable`, the claim funnel's own), so the view's
`[next]` names the row that will really be claimed next rather than a plausible
guess.

### Archived rows and the disclosure line

`gopi task archive` stamps `resolved` into the row's tags. An archived row is
hidden from EVERY list view — with or without `--status`, filtered or not — and
`--all` is the ONE spelling that brings the tombstones back. The predicate is one
function (`tools.TaskListVisible`), shared by the verb and by the conversational
`/task list`, so two views of the same column cannot disagree.

**A view that hides rows says so**: one line under the header opening with
`[hidden] `, reporting how many archived rows this render dropped and which flag
shows them — printed even when the filter leaves NOTHING, so an all-hidden view
is never byte-identical to an empty ledger. A view that dropped nothing carries
no such line.

### `claimed` — the crash-recovery window, and why it exists

The state machine is `pending → claimed → in_progress → terminal`. The first
rung is owned by the two-phase claim lock: `claimed` is the window between the
Phase-1 local compare-and-swap and the `双锁齐` (both locks held) confirmation.
Its sole durable justification is **crash recovery** — a process that dies
inside that window leaves the row recoverable rather than silently
half-claimed: `gopi task status <task-id>` reads it `resumable: yes` (exit 0) and
`gopi worker --resume <task-id>` continues the SAME row.

**`claimed` and `in_progress` answer two different questions, and the second
one is about a PROCESS** (epic `01a1a87b`, operator ruling 2026-11-05). Read the
two rungs as:

| status | what it asserts | who may write it |
|---|---|---|
| `claimed` | ownership is decided: exactly one runner won the claim CAS. Whether a child process exists is NOT asserted. | any claim leg, local or mirrored from a peer |
| `in_progress` | **a child process was exec'd successfully HERE, and its pid + start ticks are on the row** (the `process:` line of `task show`) | only the launcher, only after `cmd.Start()` returned no error |

Before that ruling the lock protocol's second half was recorded by advancing the
row to `in_progress` — which meant a peer's mirror, a merge executor and a
staleness closer all asserted "a child exists" about machines that had no child
at all, and `in_progress` stopped being evidence of anything. The lock half now
has its **own column**, `lock_phase` (`local` after Phase-1, `both` after 双锁齐,
empty when the protocol never ran — a single-node ledger writes nothing at all,
and `task show` prints no lock line), and `in_progress` is reserved for the one
fact it names.

The consequences, all deliberate:

- **A run that fails before exec stays `claimed`** with the cause recorded as a
  `start-failed` event and `result:` on the row — it does NOT advance to a status
  whose whole meaning is "a child is running". The state machine therefore grew
  the `claimed → terminal` edge (see the transition table below), because a
  verdict has to be reachable without a process.
- **The exec seam is asserted, not inferred**: `TaskRequireClaimed` runs before
  the spawn and is what stops two racing runners — the property the old
  `claimed → in_progress` CAS used to carry by accident.
- **A `claimed` row carries no heartbeat** (`task_heartbeat` stamps only
  `in_progress` rows), so a run that dies before exec is reclaimed by the stale
  sweep rather than kept alive by a stamp nobody answers for.

**`--status pending` is NOT the door, and this paragraph used to say it was.**
The transition table (`taskAllowedTransitions`, `internal/tools/tasks.go`) gives
`claimed` the exits `in_progress` plus the five terminal statuses
(`completed`/`failed`/`verified`/`cancelled`/`superseded` — the
exec-never-happened edge) — and NOT `pending`, so re-queueing a claimed row is
REFUSED by the state machine. Measured 2026-10-30 against a private sandbox
ledger (the refusal sentence is regenerated from the table, so it names exactly
the exits that exist):

```
$ gopi task update <claimed-id> --status pending --reason probe
gopi task update error: task: illegal status transition claimed → pending (allowed from claimed: in_progress|completed|failed|verified|cancelled|superseded)
exit=1
$ gopi task status <claimed-id>
status:      claimed
resumable:   yes
```

The same shape holds from `failed`: `--status pending` is refused
(`allowed from failed: in_progress|verified|completed`), the resume edge is
`failed → in_progress`, and the two documented exits are `gopi worker --resume`
(level 1, same row) and `gopi task clone` (level 2, a clean-room `<id>-c1`).
The refusal texts are pinned by `TestClaimedAndFailedCannotBeRequeuedByStatus`.

`in_progress` means a child process was exec'd here and its identity is on the
row (the two lock halves are `lock_phase`'s business, above). A terminal status
(`completed`/`failed`/`cancelled`) stamps `ended_at`; moving back to
`in_progress` clears it. `reopened` is **retired** as a value: a FAIL verify
verdict leaves the judged row terminal and REISSUES the rework as a NEW pending
row, which is how the ledger stays append-only.

### What the ledger is NOT

- **It is not runtime control.** It does not touch the agent loop's in-session
  task list; the harness's own list is a different, in-memory thing.
- **It has no delete.** Records are permanent for cross-session tracing and
  audit: the CLI may create rows, revise summaries, move statuses — never erase.
  A briefing revision is itself an audit record (`payload_revised`, old and new
  with sha256s), so nothing an operator types can erase a briefing once in
  force.
- **It is one ledger per profile**, and the reads that make a claim about the
  machine NAME the ledger they read (see the deliverable selector's scoped
  answer — `gopi task show --branch`, which resolves through the retired
  `for-branch`'s own seam and prints the row count it read: an unscoped
  "no row names it" would be a claim about the host).

### The deliverable selectors (`--branch` / `--worktree`)

The row can be addressed by its task id OR by its DELIVERABLE — the two facts an
operator standing in a worktree actually holds (`git branch --show-current` and
`pwd`). One seam resolves it (`resolveDeliverableSelector`), shared by
`show` and `status`; when several rows name the same deliverable (the in-flight
run plus the completed row that stamped it) the live owner is answered and the
others are named on stderr, so the choice is visible rather than silent.

A selector that resolves no row is an ANSWER with its own exit code
(`tools.TaskForBranchExitNoOwner` = 4), never exit 1: on `task status` exit 1
already means "running; do NOT resume", so answering 1 for "nothing names this"
would state a verdict about a row that was not found. Because a row records its
deliverable only when it is dispatched, hand-made work has no row at all — the
error is followed by a labelled GIT PROVENANCE block carrying what git itself
holds (exists, tip, where checked out, dirty), explicitly labelled as read from
git rather than recorded in the ledger, so the two channels are never confused.

## Verification (this change)

The pre-fix binary used for the red proof is the one this run built from the
SAME tree with `tasksUsage` restored (`/tmp` copy of the 172-line intermediate
version, `git show` equivalent); the measured page sizes below are
`./bin/gopi task -h | wc -l` on each binary, rebuilt before each measurement
(Rule 31).

```bash
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     # OK
$ ./bin/gopi task -h | wc -l
147        # 453 at the epic's baseline; 172 after the first pass; 147 here
$ scripts/g1-task-help-surface.sh <pre-fix binary> > before.tsv
$ scripts/g1-task-help-surface.sh ./bin/gopi       > after.tsv
$ diff before.tsv after.tsv && echo IDENTICAL
IDENTICAL        # 17 verbs, 59 advertised verb-flag pairs, flag sets byte-equal

# the page's own claim stopped being circular (all 17 verbs print THIS page)
$ H=$(./bin/gopi task -h | md5sum)
$ ./bin/gopi task list -h | md5sum                       # → the same hash
$ ./bin/gopi task sync -h | wc -l                        # 137 — a real own page
```

Also measured this pass: `gopi task worker -h` prints the 147-line family page
(it has no page of its own either), while `gopi task sync -h` (137) and
`gopi task merge -h` (59) really do.

The pre-fix binary was built from the SAME tree with `tasksUsage` restored from
a backup (`git show HEAD:cmd/agent/cli_tasks.go` is equivalent; the backup was
used so the red proof and the fix share one ledger-free, dependency-free
recipe).
