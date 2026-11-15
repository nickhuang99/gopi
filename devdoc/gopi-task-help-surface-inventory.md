<!-- doc-class: normative -->
# `gopi task` help-surface inventory (G1 of the help-surface hardening)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: 01a0c2d1-9b10-7290-b3bb-7cf17dbede99 (epic brief: `briefings/brief-help-surface-hardening.md`, G1).
**Branch**: `feat/g1-task-help-surface`.
**Question this document answers**: for every `gopi task` verb/flag the help
advertises, does a BASIC test exist that INVOKES it the way the help says to
invoke it (happy path + first refusal) — never `-h`?

**Why a test and not a reading.** The briefing's motivating defect is
`gopi task sync --push-only --task-id T`: implemented, compiled, unit-tested,
reported complete, and never typed (PROJECT.md Rule 31). A suite that enters
through a seam the author chose cannot see that class. So "has a basic test"
below means **the help's own argv was driven through the real binary** — the
same spelling an operator types.

## Re-derivation (the quoted command)

The surface list is DERIVED, never hand-typed. Two independent derivations,
which the build requires to agree:

```bash
# 1. the operator-facing derivation (this document's table)
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
$ scripts/g1-task-help-surface.sh ./bin/gopi
list	all,db,status,sweep	4
show	branch,db,worktree	3
status	branch,db,json,worktree	4
search	db	1
dedup-scan	db,live	2
improvements		0
submit	attachment,db,fixture,model,parent-task-id,payload,priority,repo,session-id,source,tags,task-id	12
update	clear-parent,db,force,parent-task-id,payload,reason,repo,result,status	9
archive	db	1
clone	confirm-cancelled,db,dry-run,reason,repo	5
sweep-worktrees	apply,backfill-anchors,db,dry-run,json,max,min-age-h	7

# 2. the same list, re-derived in the test package from the SAME usage text
#    (cmd/agent/cli_tasks.go tasksUsage), which is what the binary prints:
$ go test -count=1 -run TestTaskHelpSurfaceMatchesInventory -v ./cmd/agent/
=== RUN   TestTaskHelpSurfaceMatchesInventory
--- PASS: TestTaskHelpSurfaceMatchesInventory (0.00s)
```

Totals: **15 verbs, 59 advertised verb-flag pairs** — re-derived 2026-10-30 by
the quoted script against the built binary, at THIS tree (task 01a18799, on the
rebased tip; the number is the script's own output, never a carried-over count).
The surface has two independent retirements behind that figure and neither side's
measurement is the tree's:

- **20 verbs / 76 pairs** before the four read-only audits
  (`for-branch`, `branch-refs`, `orphans`, `stranded`) left — this task;
- **16 verbs / 62 pairs** with only those four gone — the reading this page
  carried before it landed, i.e. on the pre-rebase branch;

and on `master` the sibling retirement landed: `gopi task reopen` was deleted
outright (`545b5b2`, its mechanism moved to `session resume` / `task clone`), so
the rebased tree advertises one verb fewer than either side alone. `clone` — the
clean-room copy — joins the surface with its five flags; `update --force` and
`sweep-worktrees --backfill-anchors` were already advertised and are corrected
here from a re-derivation, not added. The verb count equals
`taskverbs.FamilyVerbs` exactly — asserted, not assumed
(`TestTaskHelpSurfaceMatchesInventory` compares the derived set against both
the pinned expectation and the covered set, so a verb added to the help with no
row and no test fails the build).

**THIS BLOCK WAS RE-RUN at 2026-10-30** (task `01a18799`): the derivation prints
**15 verbs**, and the five spellings missing from the 20 are two retirements —
the four read-only audits this task retired (`for-branch`, `branch-refs`,
`orphans`, `stranded`) and `reopen`, which `master` had already deleted while
this branch was being delivered. None is deleted from the dispatcher: the four
live in `taskverbs.RemovedVerbs` and still dispatch ONE removal sentence each
(§F6 records what each one was and where its readers go), and `reopen` — deleted
outright, its mechanism moved to `gopi session resume` / `gopi task clone` — now
answers through the generic unknown-subcommand dump, which is `master`'s
decision and is out of this task's scope. Nothing advertises any of the five, so
the derivation and the table above must not carry them either. The command and
the script are unchanged; the count moved because the surface did.

## The inventory

Legend — **Coverage**:
- `basic (G1)` — a test in `cmd/agent/cli_tasks_help_surface_basic_test.go`
  invokes the verb through the real binary on the help's own argv, happy path +
  first refusal. **All 15 rows are this value: G1 closed every gap** (the count
  is the table's own row count, re-derived with the surface on 2026-10-30 —
  `grep -cE '^\| [0-9]+ \| `' devdoc/gopi-task-help-surface-inventory.md` after
  the four read-only audits and master's `reopen` both left).
- `pre-existing` — the column records what existed BEFORE G1, so the delta is
  re-derivable rather than asserted. "argv" = a real invocation at the CLI
  entry (`{"task", taskverbs.X, …}` or the literal subcommand word); "parser" =
  `parseTasksArgs` in process only; "string match" / "prompt-TEXT" = the verb
  named in an expected-output string, never run; "engine" = the tools-layer
  function called directly with no CLI argv. Measured by
  `grep -lE '"task", *"<verb>"' $(ls *_test.go | grep -v help_surface_basic)`
  plus a `taskverbs.<Verb>` sweep in `cmd/agent`.
- **Not covered** would be an honest gap with a reason. There are none left.

| # | verb | advertised flags (help) | pre-G1 coverage | G1 test | happy path asserted | first refusal asserted |
|---|---|---|---|---|---|---|
| 1 | `list` | `--status --all --sweep --db` | argv (`cli_tasks_list_sweep_test.go`, `cli_tasks_test.go`, `peak_guard_removal_test.go`) | `TestTaskVerbListRunsAsAdvertised` | row shown; `--status` filter; `--all` | `--status bogus` → "invalid status" |
| 2 | `show` | `--branch --worktree --db` | argv (10+ files, e.g. `cli_tasks_test.go`, `cli_tasks_submit_payload_test.go`) | `TestTaskVerbShowRunsAsAdvertised`, `TestTaskShowRendersTheSameDigestForAllThreeAddresses` | id + summary + model rendered; **the digest is the SAME reached by `--branch` or `--worktree`** (the deliverable an operator standing in a worktree holds) | missing address → "missing required parameter" naming `--branch`/`--worktree`; both selectors at once → refused by the resolver |
| 5 | `status` | `--branch --worktree --json --db` | string match (`cli_completion_test.go`, `cli_tasks_singleton_guard_test.go`) — never invoked | `TestTaskVerbStatusRunsAsAdvertised`, `TestTaskStatusAcceptsBranchAndWorktree` | row + state; `--json` object; **the VERDICT is the same reached by `--branch` or `--worktree` as by the row id** (the zombie question asked from a worktree) | unknown row must not exit 0; a selector no row records → exit 4 (the "no owner" code the retired `for-branch` published, NEVER 1 — 1 already means "running" here) |
| 6 | `archive` | `--db` | — (no reference of any kind) | `TestTaskVerbArchiveRunsAsAdvertised` | `resolved` tag stamped, verdict preserved | non-terminal row → "not terminal" |
| 7 | `update` | `--status --payload --reason --result --repo --parent-task-id --clear-parent --db` | engine + parser + argv, no CLI argv for the tree-edge arm (`cmd/agent/cli_tasks_reparent_cli_test.go` drives it through the real binary) | `TestTaskVerbUpdateRunsAsAdvertised`, `TestTaskVerbUpdatePayloadRevisesThroughTheRealBinary`, `TestTaskReparentCommandLine` | status + reason readable afterwards; **`--payload` revises the briefing and `task show` reads the new value back** (epic 01a0fea1 item [2/5]); **`--parent-task-id` re-points the row's dependency edge and `task show` reads the new `parent_task_id` back, `--clear-parent` removes it** (Rule 33's edge becomes movable) | self-reported `verified` w/o evidence → "requires an evidence digest"; `--result` carrier accepted; **`--payload ""` → "refusing to blank the payload column"; `--payload` without `--reason` → "must carry --reason"**; `--parent-task-id` + `--clear-parent` together → "mutually exclusive"; a ghost parent id → "parent_task_id \"X\" does not exist"; self-reference and a cycle → refused by name, zero writes |
| 8 | `sweep-worktrees` | `--dry-run --apply --min-age-h --max --json` | argv — `taskverbs.SweepWorktrees` at `merger_work_lock_e2e_test.go:195`, invoked under a held lock | `TestTaskVerbSweepWorktreesRunsAsAdvertised` | plan; **`--json` (LIVE DEFECT, fixed)**; documented flag combo | `--min-age-h 0` → "invalid --min-age-h" |
| 9 | `search` | `--db` | engine (tools-layer query), no CLI argv | `TestTaskVerbSearchRunsAsAdvertised` | hit by summary text; miss named | keyword absent → "missing required parameter" |
| 10 | `improvements` | *(none advertised)* | — (no reference of any kind) | `TestTaskVerbImprovementsRunsAsAdvertised` | report rendered; **run twice → byte-identical** (the synopsis's own promise) | stray positional; see flag asymmetry below |
| 11 | `dedup-scan` | `--live --db` | `-h` fingerprint only (`cli_contracts_test.go`) | `TestTaskVerbDedupScanRunsAsAdvertised` | clean ledger exit 0; `--live`; **twins → exit 2** | unknown flag refused |
| 12 | `submit` | 11 flags (see TSV) | argv (14 files, e.g. `cli_tasks_test.go`, `cli_contracts_test.go`) | `TestTaskVerbSubmitRunsAsAdvertised` | minimal spelling + payload-mirror warning; **all 11 flags on one invocation, then the STAMPED row asserted** | missing summary; non-git `--repo` aborts |
| 20 | `clone` | `--reason --repo --confirm-cancelled --dry-run --db` | argv (the real binary, `cli_tasks_help_surface_basic_test.go`'s `TestTaskVerbCloneRunsAsAdvertised`; the engine's own suite is `internal/tools/task_clone_test.go`) | `TestTaskVerbCloneRunsAsAdvertised` | a NEW `-c1` pending child with `parent=`, the source's briefing verbatim behind a provenance header, a stated GROUND that differs from the source's, and `--dry-run` proving nothing was written (the row is still absent afterwards) | the FIRST refusal is the verb's design: an `in_progress` source is refused with "may be RUNNING" (its ground has an owner); `pending` → "has not run yet"; `cancelled` → names `--confirm-cancelled`; a LIVE clone → names it |

### Retired verbs — never a row, because the help advertises them nowhere

The three spellings below were advertised verbs when this table was written and are
now dispatch-only removals: each one answers with ONE sentence naming what replaced
it and exits 1 (``TestRetiredTaskVerbsDispatchARemoval``), and none of them appears in
``gopi task -h``. They carry **no row** in the table above on purpose — the table's
contract is one row per ADVERTISED verb, and a row for a verb the help does not
advertise is the stale claim this document exists to prevent. The retirement itself
is recorded in ``devdoc/cwd-release-delivery-retry-arms.md`` and in
``internal/taskverbs.RemovedVerbs``.

| retired verb | read this instead |
|---|---|
| `verify` | record the verdict yourself: `gopi task update <id> --status verified --result "evidence: <the test command> + its output tail"` |
| `retry-delivery` | `git ls-remote --heads <peer>` (what a peer advertises) and `git for-each-ref refs/heads` (what this host holds) — a clone-channel run publishes at its own close-out, so there is no blocked delivery to retry; a row is closed by `gopi task update <id> --status completed\|failed` |
| `reconcile` | `gopi task list` (the stale read-out: a row's holder is resolved per row and reported, never acted on — the `--sweep` trigger this row used to name was deleted end to end 2026-11-05, commit e4521fad), `gopi task sweep-worktrees --apply`, `gopi task list --status failed` |


## Findings

### F1 — LIVE defect: `sweep-worktrees --json` advertised but refused (FIXED)

**Shape**: the synopsis advertises `[--json]`, the runner reads it
(`runTaskSweepWorktrees` → `flags.jsonOut` → `WorktreeSweepJSON`), and
`taskFlagMember`'s `SweepWorktrees` case did not list it — so the help's own
spelling was refused. This is the `audit-merges` defect class
(`cli_tasks_advert_audit_flags_test.go`) one verb over.

```bash
$ ./bin/gopi task sweep-worktrees --json --db <tmp>/g.db      # AFTER fix
{ "findings": [ ... ]      exit=0
$ <pre-fix binary> task sweep-worktrees --json --db <tmp>/g.db
gopi task sweep-worktrees: unknown flag --json      exit=1
```
Red→green transcript: session evidence `g1-redproof-green.txt`.
**Fix**: `"json"` added to the verb's flag table (`cmd/agent/cli_tasks.go`).
**Guard**: `TestTaskHelpSurfaceAdvertisedFlagsAreAdmitted` sweeps every
advertised verb-flag pair through the parser (72 at 2026-10-16 — re-derive with
`scripts/g1-task-help-surface.sh`), so this class is closed for the WHOLE
family, not for one verb.

### F2 — RETIRED: `verdicts` and `tag-resumed` are no longer verbs (2026-10-16)

**Both rows are gone from the table above, and both SPELLINGS are gone from the
binary.** The operator's ruling was TOTAL deletion: no `RemovedVerbs` registry
entry, no replacement spelling, no "removed, use X" message — `gopi task
verdicts` and `gopi task tag-resumed` are now the generic

```bash
$ ./bin/gopi task verdicts            # exit 1
gopi task: unknown subcommand "verdicts" (want list|show|…)
$ ./bin/gopi task tag-resumed a b     # exit 1
gopi task: unknown subcommand "tag-resumed" (want list|show|…)
```

**`verdicts`** was the reframe-verdict telemetry report — a verb advertised in
NO group and documented by no row here, kept alive only by its dispatch case and
its enum entry. Its CAPABILITY is untouched: `tools.TaskReframeStats` still
exists, is still exercised by its own engine tests, and `task(action=reframe)`
still records the trail it reads.

**`tag-resumed`** was an INTERNAL resume hook exposed as a CLI verb (measured: 0
executions of the spelling). Its CAPABILITY is the resume path's lineage stamp:
`gopi session resume <raw> --task <id>` still writes `resumed:<session-id>` into
the row through a DIRECT in-process call to `tools.TaskTagResumed`
(`cmd/agent/cli_resume.go`), and the regression that pins it is
`TestResumeStampsTheFreshSessionOntoTheRow`
(`cmd/agent/cli_resume_stamps_resumed_tag_test.go`), which reads the id off the
sandbox's own session artifact rather than guessing it.

**What this cost the inventory's own history**: the F2 finding below (a
`verdicts` positional accepted and ignored) is fixed BY DELETION — the verb it
described no longer exists, so the guard is the surface itself. The transcript is
kept for the record, marked retired.

```bash
# the retired F2 transcript (kept: it is why the case was not simply dropped)
$ <pre-fix binary> task verdicts stray --db <tmp>/g.db
no reframe records yet …                                       exit=0
```

### F3 — FINDING (not fixed): `improvements` advertises no `--db`, and the verb cannot take one

`gopi task -h` prints `gopi task improvements` with **no flags at all** while
every sibling ledger verb carries `[--db PATH]`, and the "Common flags" section
at the foot of the same help says `--db PATH   override the memory db path`.
Measured:

```bash
$ ./bin/gopi task improvements --db /tmp/x.db
gopi task improvements: unknown flag --db         # refused, exit 1
```

This is a **help/help disagreement**, and per the briefing it is recorded as a
finding rather than silently patched either way: rewriting the help to print
`--db` would advertise a spelling the parser refuses (and admitting the flag
would need a reader — the runner takes the ledger from the environment, so
"admit it" is a design decision, not a one-liner). The test therefore asserts
the CURRENT contract (refusal) and this row is the record.

**Filed as a bug task**: `01a0cb36-1bb4-708b-a962-39a4955b9d65` (payload
carries the measured invocation, both candidate resolutions and the acceptance
— the choice between them is a help/behaviour contract decision, not a
drive-by edit).

### F4 — G1's own gate had a blind spot (FIXED)

Writing this table surfaced a defect in the G1 test's own scanner: it read only
a synopsis's FIRST line, and `submit` wraps its eleven flags over four lines —
so 8 advertised `submit` spellings were never swept while the flag gate
reported a green scan (`scanned < 40` could not see it). Widened to the wrap
geometry (indent ≥18 + leading `[`) and the floor raised to 50, so a regression
to head-lines-only (43 pairs) now fails instead of passing quietly.

### F6 — RETIRED: `for-branch`, `branch-refs`, `orphans`, `stranded` (2026-10-30, task 01a18799)

Four read-only audits left the FAMILY — not by being deleted (their `taskverbs`
constants survive in `RemovedVerbs`, so the spelling still dispatches one
removal sentence each) but by leaving the advertised surface entirely. They
shared one reason: they were facets of the **worktree + branch delivery
semantics** this repository no longer runs. Each asked the ledger a question
about a branch/ref world that stopped existing when a row's branch became the
row id itself, and two of them (`branch-refs`, `orphans`) went further and made
**peer-ref claims** about a delivery channel with no sender — a readout whose
answer the reader could not act on is worse than no readout (Rule 15: silence is
the defect, and so is a confident sentence about a mechanism that is gone).

Row 5's `for-branch` mention above is kept deliberately: it records WHAT the
"no owner" exit code meant, which is still the code `show --branch` /
`status --branch` return, and renaming the past would lose that.

Where each capability's readers go now (the replacement sentence each retired
spelling prints is asserted by `TestRetiredTaskVerbsDispatchARemoval`):

| retired verb | replacement |
|---|---|
| `for-branch <branch>` | `gopi task show --branch <branch>` — ONE row addressed by its deliverable, resolved through the SAME seam (`tools.TaskForBranch`). The plural "every owner" answer was reconstructed from rows the current model no longer produces (a branch IS a row's id), so the surviving face answers the question an operator still asks. `gopi task status --branch` answers the state instead of the digest. |
| `branch-refs --peer A` | `git ls-remote --heads <peer>` (what a peer advertises), `git for-each-ref refs/heads` (what this host holds), `gopi task sweep-worktrees --apply` (the ownership-independent worktree reap), `gopi task merge` (this row read `gopi merge` until the top-level verb was removed, 2026-11-01). |
| `orphans` | `gopi task show <id>` names the row's `deliverable.branch` / `deliverable.repo`; `git -C <repo> for-each-ref refs/heads/<branch>` and `git -C <repo> log --all --grep <id>` answer the ref. |
| `stranded` | `gopi task show <id>` prints the ledger status AND the advisory ground line for the same row (the two halves the readout joined), and the run log is a file: `/home/user/.gopi/sessions/<task-id>/output.log`. `gopi task list --status pending` marks an unclaimable row `[queue-skip]` with the parent gate's own reason. |

WHAT WAS DELETED (not retired): the implementations
(`internal/tools/task_branch_refs.go`, `task_ref_reachability.go`,
`task_stranded_rows.go`, `branch_disposition.go`), their CLI faces
(`cmd/agent/cli_tasks_branch_refs.go`, `cli_tasks_orphans.go`,
`cli_tasks_stranded.go`) and their dedicated tests. Two shared instruments moved
out FIRST, in the same commit (Rule 43 — a symbol whose readers live outside the
retired mechanism is not the mechanism's to delete):

- `internal/tools/task_ref_history.go` — the reachable-history reader
  (`taskRefNamedTaskIDs`, `taskRefIDsInSubject`, `taskRefHistoryNames`,
  `taskRefLooksLikeDashedID`, `taskRefGit`, and the git-flag constants). Its
  readers are `task_completion_witness.go` and `task_deliverable_tip.go`, which
  ask "did this row's work land?" about rows that have nothing to do with the
  orphans readout.
- `groundTestGitT` — the test-fatal git helper, whose readers are ground-fixture
  tests (`task_ground_shape_clause_test.go`).

### F5 — Scope boundary recorded, not a gap

`sync` / `diff` / `harvest` / `push` and the merger/worker services keep
dispatching under `gopi task` but are advertised elsewhere; the help POINTS at
them and carries no synopsis. Siblings G2–G6 own those pages. Their flags are
therefore not rows here — deliberately, per the briefing's constraint, and
enforced by `TestNoServiceOrSyncVerbInLedgerGroup`.

## What would be needed for a surface NOT covered here

There is none: all 13 advertised verbs have a G1 test. The honest limits
INSIDE those tests, so no reader over-reads the table:

- `verify`, `retry-delivery` and `reconcile` were RETIRED on 2026-10-30 (task
  01a18799, slice 2/4). Their G1 rows stay in the table as RETIRED rather than
  disappearing, because the row numbers are cross-referenced from the G1
  briefing and other devdocs; what changed is that their dispatch contract is
  now pinned by `TestRetiredTaskVerbsDispatchARemoval` (exit 1 + one sentence
  naming the replacement) instead of an invocation of a verb the help no longer
  advertises.
- `sweep-worktrees --apply` MOVES git worktrees; the G1 tests exercise the
  plan/`--json` faces and the argv errors, and the destructive half keeps its
  existing home (`merger_work_lock_e2e_test.go`). The verb is RUN at the
  surface; the destructive effect is not re-run here.
- **`finish-cleanup` came BACK, then went FOR GOOD — read BOTH halves before
  acting on this note.** The paragraph below records the 2026-10-26 restoration,
  which was correct on its premise. That premise was itself later removed: the
  close-out CHAIN (align → gate → push → worktree remove → branch drop) was
  retired with the worktree+branch delivery semantics it served, in `58b5618b`
  (*refactor(task): retire the finish-cleanup mechanism (slice 3/4)*), and the
  verb joined `internal/taskverbs.RemovedVerbs`. **The current state is: retired,
  refuses with exit 1, and advertised nowhere** — its row left this table on
  2026-10-31, its synopsis block left `gopi task -h` (the page measures 174
  lines; `taskUsageBudget` is 174 + the 12-line margin = 186), and
  `TestTaskVerbRetiredFinishCleanupExitsNonZero` pins the refusal. The
  restoration history below is kept because it explains WHAT the pass was, not
  because the verb is live.

  `fcbd0b7`
  (worktree-retirement phase 2) retired this verb FACE while leaving the
  implementation (`task_finish_cleanup.go`) alive, and that was the right call
  on its own premise: the close-out pass then rode the WORKER's claim loop
  (`serveFinishCleanupSweep`, every round), so an operator entry point was
  redundant — a resident carrier already ran it. **The premise is gone**
  (operator ruling, 2026-10-26: a worker dispatches, it does not reconcile the
  ledger or clean up grounds). With the loop no longer carrying it, the duty
  would have had NO carrier at all — which Rule 15 forbids — so the face is
  restored as the ONLY way to run the pass: `gopi task finish-cleanup
  [--task T] [--apply]`. It is the same body the resident cadence called
  (`finishCleanupSweepOnce`), so restoring the face cannot make the pass sweep
  a different set of rows than the tick used to.
  Renumbering the table is deliberately NOT done — the row numbers are
  cross-referenced from the G1 briefing and other devdocs, and a renumber would
  silently re-point those references.
