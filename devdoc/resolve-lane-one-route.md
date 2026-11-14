<!-- doc-class: normative -->
# devdoc: one route rule for the resolve lanes, one terminal-row identity, one retired dispatcher

Findings F4/F5/F6 of `devdoc/merge-resolve-effort-audit.md`. Branch
`fix/resolve-lane-policy-v2` (fresh branch off `fix/resolve-lane-policy` @
`3f4c7919`; that branch is a pure fast-forward from master `9ce8e159` —
`git rev-parse master == c00ca037^`).

## The problem this closes

Two merge-conflict routes coexisted with **no rule choosing between them**, and
the expensive one dominated: 86 resolve rows (70 branches) against 4 take-over
rework rows. An unstated default *is* a default — the resolver lane, because it
is the one the machinery takes with nobody deciding. The ledger's shape was
therefore produced by accident, not by policy.

Two further defects ride on the same lane:

- **Re-mint (F6).** Resolve-row dedup asked only "is a row LIVE?". A row that
  reached a terminal status stopped blocking, so the next tick minted a fresh
  one for the same branch — 8 branches carried >1 row, and one branch took **7
  rows in 3 hours**, each hand-authoring a resolve commit (median ~93 lines)
  before being closed.
- **A dead automatic dispatcher (F4).** `serveMergeBacklogAudit` (lane A's only
  automatic trigger) had no production caller since `d11af9a6` (2026-09-04); it
  was kept green by its own tests, so it *looked* live.

## The three decisions, and how the code states them

### 1. The default route is the RESOLVER lane (`87d68b13`)

`cmd/agent/merger_takeover_prompt.md` §3.6.0 rule 3 used to offer *both*
outcomes to the filer's judgement ("the conflict goes to the resolver lane …
**or** to the author as a rework, your judgement") — the epic's own complaint,
one level down. It now names one default:

> **The DEFAULT is the resolver lane.** A conflict nobody owns goes to a
> resolver … **The rework route (§3.6 case ④) is the EXCEPTION, and you file it
> deliberately** — only when you can state, in the row, why the author's lane is
> cheaper.

The clause is pinned by phrase in `mtpRequiredBehaviours`
(`TestMergerTakeoverPromptCoverage`), so a later edit cannot soften the default
back into a choice. The rework route is not deleted — it is the *exception*,
which is what keeps §3.6.1 case ④ (diff archived → rework submitted → branch
deleted) valid.

### 2. One terminal-row identity reader, both writers (`c00ca037`, re-verified)

`resolveConflictRowIdentity(ctx, branch)` in `internal/tools/task_merge.go:131`
is the **one** reader. It returns a tri-state — `live` (a resolver holds it:
duplicate, wait) vs `terminal` (the decision is made: **do not re-mint**) —
mutually exclusive by construction, because the two status lists partition the
schema's CHECK-constrained status set.

Both writers consult it before minting:

| writer | site |
|---|---|
| adoption sweep (lane B) | `internal/tools/task_adoption.go:355` |
| manual merge dispatch (lane A) | `internal/tools/task_merge.go:814` |

Two design points worth keeping:

- **Two row shapes are the same fact.** Lane A stamps the branch into the
  *summary* (`taskMergeSummaryVerb`); lane B stamps the same summary and also a
  payload carrying `taskMergePayloadBranchLine`. The query matches either, so a
  branch concluded in one shape is concluded in both.
- **The terminal set is rendered, never typed** (`taskTerminalStatusSQLList`,
  `internal/tools/tasks.go:243`) from `taskStatusCompleted` / `Failed` /
  `Cancelled` / `Verified` — and `taskLiveStatusSQLList` is its declared
  complement from the same constants (Rules 1b/13/14). A status added to one set
  must be added to the other, and the compiler-visible constants are the only
  spelling of either.
- The re-mint that is *legitimate* — a resolve attempt FAILED and the author has
  since pushed more work — is not silenced: it reaches the ledger through the
  **branch-rework** lane (`resolveRowReworkSummaryPrefix`), which is the lane
  that owns "the resolver could not land it". That is why the terminal gate must
  not also block rework rows.

### 3. The dead automatic dispatcher is retired, not wired (`c00ca037`)

Deleting, not wiring, is deliberate: wiring it would re-add an automatic
conflict dispatcher to the tick — the state F1 (repo sweep is report-only, zero
`Submit`) exists to prevent. Gone with it: `serveMergeExecSweep`,
`serveBranchScan`/`agentBranchScan`, their cadence constants and env machinery.
`TestDeadServeMergePipelineIsRetired` keeps them gone (code occurrences only —
the retirement note in `cli_tasks_sync.go` legitimately names two of them).

### 4. The retirement's ORPHANED KNOB — a live Rule 15/21 defect (`30b421fc`)

The inherited retirement left `--no-branch-scan` in a half-dead state:

- still admitted by `taskFlagMember`,
- still advertised on `gopi merger -h` (`cmd/agent/main.go`) and in the shipped
  help page (`internal/config/help.go`),
- still carried through `mergerRestartArgv`,

while `opts.noBranchScan` was **read by nobody** — its only reader was the
deleted `serveBranchScan`. That is Rule 15's exact shape ("an option is either
accepted and obeyed or refused by name, never accepted and ignored") and Rule
21's ("a declared knob no code reads"), one level up from the dead pipeline it
was a sibling of. Removed together: the options field, its parse assignment, the
`taskFlagMember` entry, the `mergerNoBranchScanFlag` const, the argv append, the
`cmd/agent/main.go` help line, and the two test callers that set it.

`testdata/merger_help_prefix.txt` deliberately **keeps** the pre-fix line: it is
the byte-exact page `TestCapabilityFirstPredicateIsNotBlind` requires, and a
golden file of the *old* text is supposed to be old text.

New guard `TestRetiredFlagIsNotAdvertised` (`cmd/agent/cli_tasks_audit_test.go:149`)
asserts three things about a retired flag — **not admitted** (`parseTasksArgs`
must refuse it), **not advertised** (comment-stripped scan of the three help
pages), **not declared** (the struct field) — plus a self-check that its scanner
flags the pre-fix line, so the gate cannot pass by matching nothing.

## What the run did NOT do

- **`repo_sweep.go`'s report-only non-ff behaviour is untouched** (hard
  constraint): the file is absent from `git diff --name-only master...HEAD`.
  Zero `Submit` there stays zero.
- **No Rule 30 guard was weakened**: `reclaimSkipDirtyReason` and the reclaim
  refusals are not in the diff.
- **No "> N authored lines ⇒ rework" threshold was invented** (F7). The median
  93 authored lines per resolve commit is cited as the *cost* that makes the
  route decision matter, not as a rule — a threshold needs a measurement this
  diff does not have.
- **No live-ledger backfill**: the 86 historical resolve rows are history; the
  rule is preventive.
- **No `--no-branch-scan` replacement**: nothing in the merger tick skips a
  branch-inventory pass any more (`MergerRepoSweep` is the discovery half);
  re-wiring a producer would re-create the bureaucracy F4 deleted.

## How it is verified

- **Re-mint (the 7-row case), red-before/green-after** — `internal/tools/task_merge_resolve_identity_test.go`:
  `TestTaskMerge_NoReMintAfterTerminalResolveRow`,
  `TestTaskMerge_DefersToLiveReworkRow`, `TestTaskMerge_IdentityBothRowShapes`,
  `TestAdoption_NoReDispatchAfterTerminalResolveRow`. Neutering
  `taskTerminalStatusSQLList()` to `""` (the pre-fix live-only dedup) makes the
  first and last FAIL — the second pass/sweep dispatches for a branch whose
  resolve row is already terminal.
- **Route rule** — `cmd/agent/merger_takeover_prompt_gate_test.go:110` pins the
  clause by phrase.
- **Retirement** — `TestDeadServeMergePipelineIsRetired`, and the acceptance
  grep reports intent: `grep -rn "serveMergeBacklogAudit" --include=*.go cmd
  internal | grep -v _test.go` → one hit, the retirement comment naming the
  commit that orphaned it.
- **Orphaned knob** — `TestRetiredFlagIsNotAdvertised`, plus the CLI-level proof
  `gopi merger -h` no longer advertises it and `gopi merger --no-branch-scan`
  reports `unknown flag`.
