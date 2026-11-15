<!-- doc-class: normative -->

> **Retired mechanism (2026-11-05).** `gopi task push` — the verb this prescription measures and
> prescribes against — is DELETED with the cross-node sync family; it now dispatches to a removal
> sentence and exits 1, so the divergence it was to repair cannot occur through that name any more.
> The MEASUREMENT argument (a claim must be checked by running it) is untouched and still applies.

# The staleness alarm's THIRD sentence — a prescription must be executable

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a134cab94c7758824195abdfe811e6` (follow-up to `01a13440`). Branch
`task/01a134cab94c7758824195abdfe811e6`.

## The defect

`gitSyncIntegrationLag` measured ONE number — how far the local integration ref
(`refs/heads/master` here) sat behind the peer's mirrored tip
(`refs/remotes/gitPushcheck/master`) — and `GitPassReport.GitSyncStaleAlarm`
rendered ONE sentence for every non-zero value:

```
integration ref refs/heads/master is 3 commit(s) behind refs/remotes/gitPushcheck/master
  — `gopi task push` (this pass) advances it
```

On a **diverged** pair — the local ref carries commits the peer's mirrored tip
does not — that sentence is false, and not marginally so: it prescribes the very
pass whose client arm REFUSES to rebase/merge **by design**
(`gitSyncFFIntegrationBranch` merges `--ff-only`, so git answers "Not possible
to fast-forward, aborting."). Measured 2026-10-13 in task `01a13440`'s own run
and filed as this row: the operator was sent back to a command that can never
succeed, the ref stayed frozen, and `pull:skipped` repeated on every tick.

The number was never wrong. The **prescription** was.

## The classification

One extra `rev-list --count`, in the OPPOSITE direction, over the same two refs
the delivery lane already holds (no network, no second fetch — the mirror ref IS
the peer's tip as this repo last saw it):

| behind | ahead | shape | what the pass can do |
|---|---|---|---|
| `0` | `0` | at the peer's tip | nothing to do |
| `n>0` | `0` | **purely behind** | its OWN fast-forward closes it |
| `n>0` | `m>0` | **DIVERGED** | nothing — no fast-forward exists |

Both numbers ride the pass's ONE summary line, so the log line is
self-classifying without reading a step name:

```
[serve] [git-client] pull:skipped (Not possible to fast-forward, aborting.) integration_lag=3 integration_ahead=2
```

The second count is read **only where the first says there is a gap**: a host at
the peer's tip cannot be diverged with a non-zero lag, so on the aligned path the
extra git child would be paid on every tick for an answer that cannot differ —
the zero reported there is entailed by the measured lag, not a guess.

## The sentence, and why it says what it says

Diverged (one rendering, on both the resident and the manual face):

```
[worker] [git-client] WARNING: stale integration ref — integration ref refs/heads/master is 3 commit(s) behind
refs/remotes/gitPushcheck/master and carries 2 commit(s) the peer does not have — the two refs have DIVERGED,
so no fast-forward exists and this pass cannot advance it: reconcile the tips in the checkout that holds
refs/heads/master with `git merge refs/remotes/gitPushcheck/master`, after which this pass reports the ref aligned
```

Four properties, each of them a thing the old text got wrong or omitted:

1. **Both counts, named.** `3 behind` alone is the same reading for a pair one
   pass from aligned and for a pair that pass can never touch.
2. **The claim that it is un-closable.** "no fast-forward exists and this pass
   cannot advance it" — the pass says what it cannot do, instead of pretending.
3. **An executable next step.** `git merge refs/remotes/gitPushcheck/master` —
   git's OWN verb, against the same ref the count was measured against, in the
   checkout that HOLDS the ref. Not a new gopi verb, not a new tool, not a
   process (Rule 35): the remedy is a command the operator already has, and its
   working directory is derived from `IntegrationBranchForRepo(repo)` rather than
   typed. The client arm declines this merge because a client must not author a
   merge commit on the operator's behalf; naming it as the operator's own move is
   the honest version of that refusal.
4. **What it does NOT say.** `gopi task push` (this pass) is absent from the
   diverged text — that absence is the fix, and it is asserted.

Purely-behind keeps today's bytes EXACTLY (`gitSyncLagBehindFormat`), because
there the pass's fast-forward really is the remedy. The two texts are asserted
against their own constants, so the working case cannot be "fixed" by weakening
it: an alarm that never promises anything is as useless as one that lies.

`integration_ahead` is carried only when non-zero, so an aligned and a
purely-behind host print exactly the line they printed before this change
(same rule as `integration_lag`'s omission at the aligned value).

## A dead branch that had to go

`gitSyncIntegrationLag`'s doc claimed "a DIVERGED pair reports the mirror-side
count". The code never distinguished the shapes — it reported `rev-list
--count local..mirror`, which is the peer-side count for EVERY shape — and
the FILE's own consts comment admitted it: *"a same-named ref whose value is
NOT the peer's tip"*. The claim is now true in code (the branch exists and is
executed), and the comment is now the code's description rather than an
intention.

## Files

- `internal/tools/task_sync_git.go` — the classification (`gitSyncLagDivergedFormat`,
  `gitSyncLagManualMergeCommand`, `gitSyncLagPassPrescription`,
  `gitSyncLagUnknownFmt`, `gitSyncLagAheadSummaryField`), the second count
  (`gitSyncIntegrationAheadCount`), `GitPassReport.IntegrationAhead`, the summary
  line's second field.
- `internal/tools/task_sync_git_lag_test.go` — the diverged RED leg and its
  purely-behind CONTROL, paired.

## Verification

- **Red leg, real fixture, before/after on the SAME fixture:**
  `zz_redproof_diverged_lag_test.go` (a temporary probe, never committed; its
  output lives in the run's session dir as `red-leg.log` / `green-leg.log`).
  Unmodified production ⇒ the diverged alarm still read
  `` — `gopi task push` (this pass) advances it`` (RED, the defect present);
  with the fix ⇒ the diverged sentence above (GREEN).
- **Real surface (Rule 31):** `artifacts/e2e-diverged-alarm.sh` drives the BUILT
  binary's two real faces (`gopi worker` foregound round tick, and the manual
  `gopi task push`) against a real topology (a bare peer, a holder clone, a
  linked worktree), with the peer alias `peer1:~/work/GoPi` redirected into the
  fixture by `url.<path>.insteadOf` behind a safety gate that measures the
  redirection BEFORE any face runs. **20 checks, 0 failures** (log:
  `artifacts/e2e-diverged.log`), including: the purely-behind pass advanced the
  ref and stayed SILENT; the diverged pass moved nothing, named both counts,
  named the remedy and did NOT name the doomed pass; and the named remedy ran
  (`merge exit 0`), after which the next pass reported the ref aligned.
