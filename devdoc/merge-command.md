<!-- doc-class: historical -->
# `gopi merge` — a foreground one-shot merge command

> **DISPOSITION (2026-11-01, merge-removal epic).** This document is kept as the
> RECORD of a command that no longer exists, and it is classified `historical`
> for that reason. The top-level `gopi merge` verb was removed (epic
> merge-verb removal): `./bin/gopi merge` now exits **1** with
> ``gopi: `merge` is RETIRED — use `gopi task merge` ``. Nothing here is an
> instruction a reader can carry out; every `gopi merge` below is what the
> command did while it existed. The engine those sections describe is still in
> the tree and still dispatches — under the family spelling `gopi task merge`,
> documented by the FILE `man/gopi-merge.md`. This is not a substitution: the
> sections are not rewritten, because rewriting a record would make it a
> fiction about a command no one can run.
>
> The class line above was `normative` until this date. That was the defect: the
> body's own AMENDED note (`:7-12`) already says these rows are "kept as the
> record of what this command was, not as its contract", so the page declared a
> standing claim its own text disclaims.
>
> **The deletion, measured on the tree that carries this note (2026-11-01, items
> 2–3) — every Go path the page names is ABSENT:**
>
> ```
> $ for f in cmd/agent/merge_run.go cmd/agent/merge_run_test.go cmd/agent/merger_takeover.go internal/tools/merge_ceiling_rework.go internal/tools/merge_handover_counter.go; do if [ -e "$f" ]; then printf 'PRESENT  %s\n' "$f"; else printf 'ABSENT   %s\n' "$f"; fi; done
> ABSENT   cmd/agent/merge_run.go
> ABSENT   cmd/agent/merge_run_test.go
> ABSENT   cmd/agent/merger_takeover.go
> ABSENT   internal/tools/merge_ceiling_rework.go
> ABSENT   internal/tools/merge_handover_counter.go
> ```
>
> §4.1 is the arm this change deleted. **Nothing here is a current contract.**

Task: merge-command-foreground (briefing `9ce8e159`). Branch
`feat/gopi-merge-command`, worktree `~/workspace/gopi-merge-cmd`.

> **AMENDED 2026-09-19 (epic merge-lock).** This document's "no lock" sections
> (§1 table, §1 prose, §6, §7, §8 item 8) describe the command as it shipped and
> are now **false**: `gopi merge` HOLDS the single-instance flock
> (`serve.lock` under the gopi data root) for the whole run. The change and its
> reasoning are in `devdoc/merge-flock.md`; the "no lock" rows and paragraphs
> below are kept as the record of what this command was, not as its contract.

## 1. What was asked, and what the command is

One top-level command, **one run, no residency**: walk every branch of every
repo in the settings.json `repos` table, land what git alone can land, hand the
diverged branches to an LLM **only within a measured, flag-overridable ceiling**,
and where the ceiling fires **STOP and file the branch as a rework task** instead
of grinding.

The identity difference from `gopi merger` is the whole point:

| | `gopi merger` | `gopi merge` |
|---|---|---|
| process shape | cron tick, or `--every D` self-driving loop | ONE foreground run |
| lock | takes `serve.lock`, holds it for the loop's life | **the same `serve.lock`, held for the one run** (epic merge-lock, 2026-09-19 — was: none) |
| daemon faces | `--daemon`/`--stop`/`--restart`, detach ladder | **none** |
| progress record | `gopi merger takeover-progress` | **none** |
| what it does with a conflict | LAUNCHES a take-over gopi and returns | measures it, then hands it over **in-process** or files it |
| state after the run | the take-over child is alive | nothing is alive, nothing is on disk |

The resident merger is **still untouched as a process**: `merger_takeover.go`,
the loop, the progress record and the detach ladder all stay, because a separate
residency task owns them. What this command now SHARES with it is the LOCK —
one guard, one funnel (`acquireServeLock`), taken for one foreground run
(epic merge-lock, 2026-09-19).

## 2. The algorithm (`cmd/agent/merge_run.go`)

Per repo, in this order:

1. **`git rev-parse --is-inside-work-tree`** — a non-repo is an error outcome
   (exit 1 for that repo), never a silent skip.
2. **Master safety** — `tools.MergerForcedSafety(repo, master, false)`, the same
   inspection the resident merger runs, with `clean=false`: this command does not
   repair an unfinished merge, it **reports** it. Why not repair: the merger's
   repair is justified by the merger owning every write to master in that repo;
   a foreground command run by hand has no such claim, and `reset --hard` on a
   tree somebody else left dirty is exactly the destructive guess Rule 15
   forbids. A dirty tree / unfinished merge ⇒ `skipped`, and the repo is not
   written to at all.
3. **Candidate branches** — every local branch except: the integration branch,
   branches a worktree holds (`git worktree list --porcelain`), and
   `archive/<name>`-tagged branches (a DECIDED EXCLUSION, PROJECT.md Rule 9).
4. **Per branch, git decides first** (`mergeDecideBranch`):
   - `merge-base --is-ancestor <branch> master` ⇒ **already-merged**, `branch -d`.
   - `merge --ff-only <branch>` succeeds ⇒ **landed**, `branch -d`.
   - otherwise **diverged** ⇒ go to §3.
5. **Rebuild** — if this repo landed anything, `tools.RebuildAfterMerge(repo)`
   (the Rule 12 declared-build contract). No build entry ⇒ skipped silently; a
   failed build is reported, never a rollback.

## 3. The ceiling

The ceiling is the part the resident path does not have, and it is **measured,
not guessed**. `mergeMeasureConflict` materialises the merge in a throwaway
worktree **inside the scratch tree** (`scratch.MkdirTemp`, Rule 22 — never
`/tmp`), runs the probe the prompt itself prescribes (`merge --no-commit
--no-ff`), and reads two numbers off git:

- the **conflicted file count** (`git diff --name-only --diff-filter=U`);
- the **estimated token cost of the branch's own diff**, anchored at the merge
  base, through `agent.EstimateTokens` — the SAME estimator the agent loop uses
  to decide compaction, so the ceiling is denominated in the currency that
  actually runs out.

The probe worktree is removed by a `defer` **and** the merge is aborted, so a
crash between them cannot leave a branch pinned in a worktree (a pinned branch
would be skipped as "somebody's live work" forever — a silent permanent skip).

Two axes, each a named constant with its reason beside it:

| constant | default | why this value |
|---|---|---|
| `mergeMaxContextTokensDefault` | 120000 | deliberately far above one healthy hand-over (a real conflict's diff is tens of KB ≈ a few thousand tokens) and below the model's window, so the default fires only on a genuinely oversized branch |
| `mergeMaxConflictedFilesDefault` | 25 | a conflict spread over dozens of files is a rewrite wearing a conflict's clothes — no short-lived run resolves it well, and the honest carrier is the rework queue |

Both are overridable (`--max-context-tokens N`, `--max-conflicted-files N`), so
an operator who knows better than the default is not blocked by it.

## 4. The four outcomes

Exit code **0 means every branch reached a terminal decision**; a filed branch is
a decision, not a failure.

| outcome | meaning |
|---|---|
| `landed` | fast-forwarded into master, ref pruned |
| `already-merged` | tip already in master, ref pruned |
| `handed-over` | diverged and within the ceiling — handed to the merge gopi in this run |
| `handover-failed` | the hand-over child ended with an error — the failure is STAMPED into the ledger and the next run retries (below the cap) |
| `rework-filed` | this command's OWN hand-over reached the cap — the branch entered the branch-rework lane |
| `cap-exceeded` | diverged and OVER a ceiling — **filed** as a branch-rework task with the diff attached, branch left alone |
| `skipped` | a worktree holds it, the repo is dirty/mid-merge, or a LIVE branch-rework row owns it (the hand-over cap was reached earlier) |
| `error` | measurement or submit failed (exit 1) — the branch is always KEPT |

`cap-exceeded` is the headline: the command STOPs on that branch rather than
grinding, and the work survives because the same call that stops it files it.

## 4.1. The hand-over failure arm — a counter, so the loop can end

> **RETIRED 2026-11-01 — this whole section describes machinery that is GONE.**
> Read it as the record of what the arm was, not as its contract.
>
> * The command it belonged to, the top-level `gopi merge`, was deleted (epic
>   merge-verb-removal, item 3: `refactor(merge): delete the one-shot gopi merge
>   implementation`). `gopi merge` now answers with a retirement sentence and
>   exit 1; the surviving spelling is `gopi task merge`, whose engine
>   (`tools.TaskMerge`) never had this arm.
> * The arm itself was deleted next: `merge_handover_escalate.go` in
>   `c8452ee0` (the escalation half), and
>   `internal/tools/merge_handover_counter.go` — every symbol in the table below
>   — in the change that carries this note (the counter half). Neither file
>   exists in the tree any more.
> * What the counter fired, the `merge-handover-failed` alarm, left
>   `taskNotifyAlarmEvents` in the same change: an alarm with no writer promises
>   `GOPI_TASK_NOTIFY_EVENTS=all` an event nothing emits.
>
> The section is kept because the REASONING is still worth reading — a failure
> visible only on one foreground command's stderr is re-decided identically
> forever — and because the `merge-verify` counter it is contrasted with is
> live. Nothing below is a runnable claim.

The ceiling is a **deterministic measurement** (conflicted files / tokens), and
for a long time it was the only thing on this path that filed rework. The
hand-over arm had no such instrument: when the child merge gopi came back with
an error, `mergeHandOver` printed it and returned. A branch whose resolution
child failed on **every** run was therefore re-decided identically forever, its
failure visible only on the stderr of a foreground command nobody re-reads — the
live analogue of the concern the archived tag
`archive/merge-resolve-escalate-early-cb9f72f` carried (whose own seam was
deleted wholesale and is NOT resurrected here).

**Everything from here to the end of §4.1 is the RECORD.**

| piece | what it is |
|---|---|
| `MergeHandoverRecordFailure(repo, branch, detail)` | mints ONE ledger row per failed hand-over: status `failed` (terminal, so it can never be claimed), tags `MergeHandoverStampTagPrefix`, deliverable `{"repo","branch"}` |
| `MergeHandoverFailCount(repo, branch)` | counts those rows — the deliverable JSON is the authority, the tag `LIKE` is only a narrowing prefilter (a branch name is a prefix of another branch name) |
| `MergeHandoverFailCounts(repo)` | the same count for EVERY branch of a repo in ONE query — the per-repo read the sweep makes once, because a per-branch query would be one ledger scan per branch per run |
| `MergeHandoverFailCap = 3` | the anti-loop cap, the same shape as `MergeVerifyFailCap` and the resolve lane's cap |

**It cannot collide with the merge-verify counter.** The stamp carries
`MergeHandoverEvent` (`merge-handover-failed`), a tag no other lane writes, and a
test (`TestMergeHandoverEventLiteralMirrorGate`, itself gone with the counter)
failed if that literal was ever inlined in a second file — so the two counters
could not silently begin to sum. They measure different facts: a branch whose
merged state failed *verification* vs. a branch whose merge child could not *do
the work at all*.

**Escalation reuses the lane, it does not mint one.** At the cap the branch is
filed with the SAME identity every other refusal lane uses — summary
`resolveRowReworkSummaryPrefix + branch`, tags `resolveRowReworkTags`, deduped by
`reworkSummaryLive` — so a branch the ceiling stopped, the build gate refused, or
a failed hand-over exhausted is the SAME row in the ledger. Filing a second row
family for one branch is what master's live-only one-probe design forbids.

**The gate runs BEFORE the decision, not after it.** Each repo's failure counts
and the branch's rework liveness are read ONCE, up front; a branch at the cap
whose branch-rework row is still LIVE never reaches `mergeDecideBranch` at all —
it is `skipped`, with the reason stated. That is what makes the loop *stop*
rather than merely *record*: the identical re-decision is not performed a fourth
time (pinned by counting the seam's invocations in
`TestMergeHandOverEscalationHaltsTheIdenticalRedecision`). The dedup is
LIVE-ONLY, exactly as in the rest of the lane: once that row goes terminal the
branch is the merge queue's business again, so the escalation is a gate and not a
dead end.

Both readings degrade **soft** (`0` / empty map on an unreadable ledger) and say
so on stderr: a ledger hiccup must degrade to today's behaviour — hand the branch
over — never silently freeze the merge queue. A stamp write failure is reported
(Rule 15) and the escalation is NOT taken on a count that was never recorded.

And a child that never started is the SAME failure as a child that errored
(`newAgent()` failure stamps too, with its own detail spelling): from the
branch's point of view the resolution did not happen either way.

## 5. The filing lane (`internal/tools/merge_ceiling_rework.go`)

`MergeCeilingFileRework` reuses the **existing** branch-rework identity, not a
new one: summary `resolve merge conflict: rework: <branch>` (the lane's own
prefix), tags `merge-conflict,branch-rework`, dedup through `reworkSummaryLive`.
So a branch the ceiling stopped is the SAME row as a branch the build gate or
the contract gate refused — **one branch, one row, however it was refused**.

The diff is written into the scratch tree and attached (capped at 2 MiB, with an
explicit truncation marker — never a silent cut). A submit failure returns an
error that is **reported and the branch kept**: `git branch -D` after a failed
submit is precisely the silent work-loss this command must never have (Rule 15).

## 6. The prompt: forked, not rewritten

`cmd/agent/merger_takeover_prompt.md` → **`cmd/agent/merge_prompt.md`**, embedded
by this command (`//go:embed merge_prompt.md`, `var mergePrompt`).

**A fork, said plainly.** The resident merger's prompt is a SEPARATE FILE that
this branch does not touch: `merger_takeover.go` still embeds
`merger_takeover_prompt.md` for the take-over child it launches, and its gate
test (`merger_takeover_prompt_gate_test.go`, `mtp*` symbols) still governs it.
Two files, two commands, two identities — not a rename that leaves an orphan.
The judgement text is shared; the runtime premise is not, and the copy is how
that is expressed. **The drift risk is real and is recorded in `## Not Done`.**

**Rename table** (symbol by symbol):

| old | new |
|---|---|
| `cmd/agent/merger_takeover_prompt.md` | `cmd/agent/merge_prompt.md` |
| `Var mergerTakeoverPrompt` (embed in `merger_takeover.go`) | `var mergePrompt` (embed in `merge_run.go`) |
| `merger_takeover_prompt_gate_test.go` | `merge_prompt_gate_test.go` (**NOT** repointed: the resident file keeps `mtp*` and still gates `merger_takeover_prompt.md`; the merge gate is a **new** file whose `mtp*` symbols were renamed to `mrg*` to avoid the redeclaration — it owns `mrgPromptPath`) |
| `mtpPromptPath = "merger_takeover_prompt.md"` | `mrgPromptPath = "merge_prompt.md"` (new const; the resident `mtpPromptPath` is untouched) |
| `<!-- takeover-section: §X -->` | `<!-- merge-section: §X -->` |
| `TestMergerTakeoverPrompt*` | `TestMergePrompt*` |
| §3.9 `serial-lock` | §3.9 `no-lock` |
| §3.10.1 `progress-record` | §3.10.1 `budget` |
| report heading `MERGER TAKE-OVER REPORT` | `MERGE RUN REPORT` |

`merger_takeover.go` still embeds the OLD file for the RESIDENT take-over child
— that path is a separate task's, and this command does not touch it. The
launcher test (`merger_takeover_launcher_test.go`) is correspondingly
**untouched**: `git diff --name-only 9ce8e159..HEAD | grep launcher` prints
nothing, and its `TestMergerTakeoverBriefingCarriesPromptAndFrame` still asserts
the RESIDENT artifact's spellings (`MERGER TAKE-OVER REPORT`,
`<!-- takeover-section: §3.5 peer-dead -->`) against `mergerTakeoverPrompt` —
correct, because the take-over briefing is still built from that variable. An
earlier revision of this branch had repointed those assertions to the merge
prompt's spellings; that edit was WRONG (it asserted the resident briefing
carries a prompt it does not carry) and was reverted before the commit.

**What was edited structurally** (three sections, each because the runtime
premise changed, not for style):

- **opening + §0** — "a cron launcher that exited immediately" became "a
  foreground command that is still running, holding no lock"; the "next cron
  tick starts a fresh one" paragraph became the ceiling paragraph.
- **§1** — the `gopi merger takeover-progress init` first-call instruction is
  gone (there is no record and no `--status` surface to feed). In its place:
  "you hold no lock and you take none", and "the command already did your first
  pass for you" (the FF pass landed and deleted those refs — the child has no
  ref left to triage).
- **§3.4.0 (new)** — the ceiling, in the child's terms: the command measures and
  the command files; the model must not file a second row, must not estimate
  tokens by feel, and must decide from the machine facts it already has.
- **§3.9** — the whole lock section was replaced: the command takes none, a
  resident `--every` loop is a PEER not a blocker, and why a lock would be
  "residency by another name" in a command whose contract is one run.
  **(INVERTED 2026-09-19 by epic merge-lock: the command now TAKES the lock, the
  section is titled `§3.9 lock`, and a resident `--every` loop that started
  first is the HOLDER this run would have been refused by. See
  `devdoc/merge-flock.md`.)**

## 7. What is NOT here

- **No daemon, no detach, no `--stop`/`--restart`.** The flags do not parse:
  an unknown flag is a non-zero exit naming it (Rule 15).
- **No second lock.** The one it holds is the EXISTING `serve.lock`, taken
  through the existing seam — never a private substitute (epic merge-lock).
- **No progress record.** Nothing to observe after the process exits.
- **No residency removal.** `gopi merger`'s loop, progress record and detach
  ladder are untouched — that is a separate task's subject.

## 8. Verification

Branch-scoped, and precisely selected: the loose prefix `-run TestMerge` ALSO
matches three resident-merger control-face tests (`TestMergerStopEndsALiveLoop`,
`TestMergerRestartReplacesTheLoopWithoutOverlap`,
`TestMergerRestartDoesNotTouchTheTakeoverInFlight`) that are **red on the BASE
commit `9ce8e159` on this host** (verified by running them in a detached worktree
at that commit: same three names, same assertions, `FAIL … 274.682s`). Use:

```
go test -count=1 -v -run 'TestMergeUsage|TestMergeLands|TestMergeRecognises|TestMergeCeiling|TestMergeSmall|TestMergeSecond|TestMergeTakesNoLock|TestMergeDoesNotPark|TestMergeProbe|TestMergeRunHeader|TestMergePrompt' ./cmd/agent/
```

→ 18 tests, all PASS, `ok … 12.082s`. Build/vet/gofmt and the full evidence
(self-filed, per observation) are in this branch's task report; the devdoc's job
is the design, the report's is the measurement.

> **THIS COMMAND IS A RECORD, NOT A VERIFICATION (measured 2026-11-01).** The 18
> tests it counted lived in `cmd/agent/merge_run_test.go`, which no longer exists:
> the merge route's 95 test files (28883 lines) were retired by commit `5a9af79b`.
> Run it today and **every one of those eleven selectors matches no test function**,
> so `go test -run` prints `[no tests to run]` and **exits 0** — a permanent green
> that measures nothing, which is the exact vacuum the citation gate exists to
> count. The `citation-selector` baseline used to carry a row freezing this line as
> a known vacuum; that row is now **gone**, because this page was reclassified
> `historical` (`0facc9a8`) and a historical page's citations are exempt from
> judging (`internal/standards/citation_selector_gate.go`, `selectorHistDocClass` — that gate and its predicate were themselves DELETED in `1e3b0976`, after `78bdf05a` had already taken their tests, generators and tables; measured on this tree, `git cat-file -e HEAD:internal/standards/citation_selector_gate.go` fails and `git grep -w selectorHistDocClass HEAD -- '*.go'` matches nothing. So the exemption this sentence describes no longer exists either)
> — so nothing counts this line any more and this note is the only warning a
> reader gets. The selectors are left written above rather than edited out: the
> block is the measurement that was taken, and rewriting it would make this
> document a fiction about a run nobody can reproduce.

### Tests (`cmd/agent/merge_run_test.go`)

Seven scenarios, all against **real git repositories built by the test** (the
only seam is the LLM call):

1. `TestMergeUsageExitsZeroWithNoSideEffects` — `-h` exits 0, prints the page,
   and a settings file pointing at a nonexistent repo is never resolved;
2. `TestMergeLandsFastForwardableBranch` — the branch lands and its ref is pruned;
3. `TestMergeRecognisesAlreadyMergedBranch` — a spent ref is reported and pruned;
4. `TestMergeCeilingFiresAndFilesReworkTask` — **headline**: the ceiling fires on
   a conflicting branch, the row is filed, and the branch is NOT deleted;
5. `TestMergeCeilingIsIdempotentAcrossRuns` — two runs mint ONE row;
6. `TestMergeSmallConflictStaysUnderTheCeiling` — a one-file conflict is handed
   over with its measurement, not refused;
7. `TestMergeSecondRunWritesNothing` — a settled repo is a no-op (same tip, same
   refs, clean tree);
8. `TestMergeTakesNoLockAndLeavesNothingRunning` — no `serve.lock` appears.
   **(superseded 2026-09-19, epic merge-lock: the command now TAKES the lock;
   replaced by `TestMergeTakesTheSingleInstanceLock`,
   `TestMergeRefusesWhileAnotherFaceHoldsTheLock`,
   `TestMergeReleasesTheLockWhenTheRunEnds` + the real-binary
   `TestMergeLockRefusesASecondRunE2E` /
   `TestMergeLockIsReleasedWhenTheRunIsKilled` /
   `TestMergeHelpStillTakesNoLockE2E` in `merge_lock_e2e_test.go`.)**

## 9. Defects found during this run's own review (all fixed here)

The first implementation passed its own suite. Reviewing the shipped TEXT and
the child's briefing — the parts a green test does not read — found four
defects, three of them functional:

**(a) The safety probe parked EVERY repo it was written for.** The dirty-tree
guard read `tools.MergerForcedSafety(...).Dirty`, which is
untracked-INCLUSIVE. A live task field permanently holds untracked artifacts
(briefings, logs), so the command reported `skipped: working tree dirty` for
every repo an agent had ever touched — i.e. all of them — while looking
"handled". Fixed by reading `tools.AdoptionGitStatusTracked` (tracked-only) and
reporting through the new `mergeDetailTrackedDirty` constant. Regression test:
`TestMergeDoesNotParkRepoHoldingUntrackedScratch`.

**(b) The child's briefing lied about what the run did.** `mergeRunHeader`
hardcoded the landed section as `"(none — every branch the command could land,
it landed)"` and counted only repos that produced a *hand-over*. For a run that
landed a branch and then met a conflict, the briefing told the child nothing was
landed and named the wrong repo set — to the one reader who cannot check,
because the landed refs are gone by then. Fixed: the header is now rendered from
the run's own `outcomes` + the walked `repos` list, with `mergeNoLandingsLine`
as the honest negative. Regression tests:
`TestMergeRunHeaderReportsWhatTheRunDid`,
`TestMergeRunHeaderSaysNoLandingsWhenItLandedNothing`.

**(c) A PHANTOM VERB in the renamed prompt.** The rename left step 5 of §3.6.1
instructing `gopi merger merge-progress done …` — a command that exists nowhere
(the real family is `gopi merger takeover-progress`, and it belongs to the
RESIDENT merger's observable round, which this command does not have). A prompt
that sends the model to a nonexistent command spends its run on the refusal and
may conclude the record is merely broken rather than absent. Fixed: the step now
says the run report IS the record (§3.11). Gate: `merge-progress`,
`takeover-progress` and `progress-record` joined `mrgForbiddenContradictions` as
ABSENCES, and `"your report IS the record"` joined `mrgRequiredBehaviours` as
the positive half (absence alone would let a future edit delete the instruction
rather than replace it).

**(d) A resident-era phrase survived the rename.** §3.5 still read "skipping a
branch costs one **cron interval**" — vocabulary for a process this command does
not have. Fixed to "one run".

A fifth, sub-functional item: the cwd-release gate. The probe's throwaway
worktree lives in the scratch tree the next run reuses, so removing it owes a
release BEFORE the removal (a `chdir` out of an already-deleted directory still
pins the inode until the process exits). `mergeReleaseCwd` is wired through the
`mergeWorktreeRelease` seam and asserted by `TestMergeProbeReleasesCwdOnTeardown`.

**(e) A Rule 18 violation this run introduced.** The tracked-dirty guard's fix
(a) was written as `if err := …; err != nil { … } else if …`, and
`TestStandardGates/TestGate3ElseAfterReturnRedProof` failed it as a NEW
violation at `merge_run.go:343` — a gate on the merge path, so it is fixed
rather than baselined (de-indented to a separate `if`, the guard-clause shape
the rule asks for).

**(f) The handover table misdescribed the branch's own diff.** The rename table
in §6 listed `merger_takeover_prompt_gate_test.go` → `merge_prompt_gate_test.go`
and `mtpPromptPath` → the merge spelling, and claimed the launcher test's
artifact assertions had been "repointed". None of that is what the diff does: the
resident gate file and its `mtp*` symbols are untouched, `mrgPromptPath` is a NEW
constant, and `merger_takeover_launcher_test.go` is not in the diff at all (its
assertions still read `mergerTakeoverPrompt`). The table is the artifact whoever
retires the resident path will trust, so a wrong row sends that reader to a file
that never changed. Fixed in place, with the falsifying command written beside it
(`git diff --name-only 9ce8e159..HEAD | grep launcher` prints nothing).

