<!-- doc-class: historical -->
# `gopi merge --fast` — the deterministic-only merge round

Task: `01a0b742-3c64-7dd2-ac2e-8912c6ad304a` (branch `feat/merge-fast`).

## The problem the flag answers

`gopi merge` (epic merge-command-foreground, 2026-09-18) already decides every
branch **git alone can decide** — a spent ref is pruned, a fast-forwardable
branch is landed and deleted — but it does that and then **keeps going**: every
branch that does NOT fast-forward is measured in a throwaway worktree and handed
to a short-lived merge gopi (an LLM run). For an operator whose mergeable set is
mostly fast-forwards, there was no way to say *"just do the free part"*.

The epic's ask: `gopi merge --fast` — git commands only, land the
fast-forwardable branches, delete the landed refs, leave the conflicting ones
for a later full `gopi merge`, no LLM burden.

## The design (and the one decision that mattered)

`--fast` is a new arm of the EXISTING command (`cmd/agent/merge_run.go`), not a
new command and not a new engine: the same repo table, the same single-instance
lock, the same safety checks, the same branch candidates, the same declared
rebuild, the same report. Everything it *removes* is exactly the part of a merge
round that needs judgement.

`mergeDecideBranch` already had the three-way decision, and the third arm is
where the flag lives:

| arm | condition | full run | `--fast` |
|---|---|---|---|
| ① spent ref | `merge-base --is-ancestor <b> master` | `git branch -d` | same |
| ② fast-forward | `git merge --ff-only <b>` | land + `git branch -d` | same |
| ③ diverged | everything else | measure probe + hand over to the merge gopi | report `diverged`, touch nothing |

So the fast arm is a **gate on the hand-over**, not a replacement of the
landing path — which is why the fast run is not merely "faster", it is also
free of the two costs that existed only to serve the LLM: the throwaway
conflict-probe worktree and the branch-rework filing (the ceiling machinery).
Both are *skipped by construction* in the fast arm (no branch is ever measured),
and both are asserted as skipped by the tests.

**A subtlety worth recording** (it bit the first version of the fixture): a
branch created BEFORE master moved is an *ancestor* of master, and `git merge
--ff-only` on it exits 0 without moving master — so arm ② does not fire, arm ①
does (the ref is spent). Only a branch created ON TOP of current master is a
true fast-forward candidate. The test fixture now builds the diverged branch
first and the FF branch on top of the advanced master for exactly this reason.

### What `--fast` deliberately does NOT do

- **No hand-over**: `mergeHandOver` is gated on `!opts.fast` (and there can be
  no hand-over to gate, since nothing is measured). No gopi child is started, so
  no model call is spent.
- **No probe**: `mergeMeasureConflict` (a linked worktree under the scratch
  tree) is never reached.
- **No filing**: `mergeSubmitRework` (the ceiling's ledger row + attached diff)
  is never reached — a branch the fast arm cannot land is simply left in
  `refs/heads`.
- **No archive tag**: an archive tag is a *decided exclusion* (out of the merge
  queue forever); the whole point of the fast arm is that the branch stays a
  candidate for the next full run. Nothing in the fast arm writes one.
- **No deletion of a diverged branch**: the ref, its tip and its worktree state
  are untouched. Deleting it would be the silent work-loss the command's own
  ceiling path explicitly guards against.

### Flag semantics

- `--fast` is parsed by the same tiny parser (`parseMergeArgs`) and its spelling
  is the named constant `mergeFastFlag`; the usage page advertises the arm with
  the same constant (one spelling — Rules 1b/13), and a test binds page ↔
  parser in both directions.
- **The ceilings are refused alongside `--fast`.** `--max-context-tokens` and
  `--max-conflicted-files` bound the *hand-over* a diverged branch would get,
  and the fast arm has no hand-over. Silently ignoring them is the Rule 15
  failure (an operator would believe a ceiling was in force), so
  `--fast --max-conflicted-files 0` is a usage error naming both flags.
- The arm announces itself on stdout (`mergeFastArmLineFormat`), so a captured
  run log needs no inference about which path ran.
- The lock, the safety checks, the candidate rules (integration branch, archived
  branches, worktree-held branches) and the declared rebuild (Rule 12) are all
  unchanged — the fast arm is a merge writer and takes the same guard.
- Exit status: unchanged contract — 0 when every branch reached a terminal
  decision. A `diverged` verdict IS a decision, so a fast run over a repo whose
  branches are all diverged still exits 0.

## Evidence (measured, this branch)

Live run of the real binary (`/dev/shm/gopi-fast` built from this worktree,
`CGO_ENABLED=0 go build ./cmd/agent/`), against a scratch repo
(`/home/user/workspace/_tmp/merge-fast-e2e2-*/repo`) holding one diverged branch
and one true fast-forward:

```
--- before (ff tip 66374e6) ---
feat/diverged 04b7916
feat/ff 66374e6
master bf11d7f
gopi merge: --fast — deterministic-only run: land and prune what a fast-forward carries, hand nothing to an LLM, file nothing
  .../repo feat/diverged  diverged  does not fast-forward — needs judgement; left untouched for a full `gopi merge`
  .../repo feat/ff        landed    fast-forwarded into master, ref pruned
gopi merge: run over — 2 branch decision(s), lock released with the run, no child left behind
exit=0
--- after ---
feat/diverged 04b7916      (untouched, same tip)
master 66374e6             (== the landed branch's tip)
porcelain:[]
```

No gopi child was started (no hand-over line, no session), and the diverged
branch's ref and tip are byte-identical before and after.

Red proof of the new gate (Rule 10.4): with the `if opts.fast { return
diverged }` arm deleted from `mergeDecideBranch`, the new tests FAIL on exactly
the properties the arm provides —
`TestMergeFastLandsFastForwardableBranchAndPrunesIt` reports
`the fast run materialised a conflict-probe worktree (1 teardown(s))` and
`TestMergeFastLeavesDivergedBranchForTheFullRun` reports the missing detail plus
`a fast run reported a hand-over or a rework filing`. Restored byte-identical
(`cp` back; `go build` OK) and re-run green.

## Not done (honest)

- **The fast arm does not attempt a NON-fast-forward merge.** git can also
  sometimes merge diverged branches cleanly (`git merge` with no conflict, a
  full merge commit). The epic said "or simply merge without conflict"; it was
  deliberately NOT implemented: this command's landing path is fast-forward-only
  by design (a merge commit on master would land a branch the ceiling machinery
  never measured, and the divergence between "clean merge" and "needs
  judgement" is exactly the judgement the full run hands to the LLM). A
  follow-up could add it as its own opt-in arm — it is a decision, not an
  oversight.
- **No bulk/parallel speedup**: the fast arm removes the LLM and the probe, not
  the per-branch subprocess count. Measured cost is a handful of git calls per
  branch, which was never the bottleneck.
- **`gopi task merge --all` (the hook face, `internal/tools/task_merge.go`) has
  no `--fast`.** That engine already deletes what it lands and needs no LLM
  unless it is asked to submit; a fast flag there is a separate change.
- The `gopi merge -h` "WHAT IT DOES" paragraph still describes the full run
  only; the `--fast` contract lives in the invocation list and the `Notes` tail
  (which is where the capability-first gate requires detail to live).
