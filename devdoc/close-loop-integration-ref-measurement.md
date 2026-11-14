<!-- doc-class: historical -->
# The close loop's current-ness question: measured against the ref the grounds are cut from

**Class:** design
**Task:** 01a1385d-bd99-7bd0-8594-a7e5ff9bf307 (bug row filed by 01a137cb-b958-7a03-b8f0-7bad6acf267c)
**Files:** `internal/tools/rebuild.go`, `internal/tools/rebuild_integration_ref.go`,
`internal/tools/integration_claim_target.go`, `cmd/agent/cli_tasks_rebuild.go`,
`cmd/agent/merge_run.go`

## The defect

`RebuildAfterMerge` — the merge round's last step, the one that makes the machine
run the code master holds — decided whether a rebuild was needed with:

```go
tip, err := gitTip(repo)   // git -C repo rev-parse --short HEAD   ← the LOCAL HEAD
if current, currentRev, _ := binaryVersion(repo); versionMatches(currentRev, tip) {
    res.OK = true          // "already current" → SKIP
    return res
}
```

`gitTip` resolved the **checkout's own HEAD**, never the ref the task grounds are
cut from. So a host whose local master trails its own upstream mirror read
"already current" and skipped, while the artifact was ~97 commits behind the
integration branch every dispatched grounding uses (measured 2026-10-14 on
hostb-Laptop: the artifact attested `c3a3f4eae74c` = `refs/heads/master`, and
`refs/remotes/gitPushcheck/master` read `7d0f7848b9e8`, 97 commits ahead).

The important part is what that means for repair: **the close loop could not fix
its own blind spot.** By its own definition the artifact *was* current, so the
mechanism that already ticks (`mergerPassCloseLoop` → `serveRebuildAfterMerge` →
`RebuildAfterMerge`, plus the `RebuildPendingRepos()` drain) reported success
every pass while the machine ran stale code.

## The fix — a better question, not a new mechanism

The ref that must answer "what should this repo's artifact carry?" is the ref the
grounds follow. That ref is **already resolved** in this package, for the
ancestry question: `integrationClaimTargetRef` (`integration_claim_target.go`,
task 01a12d30) decides — from the object graph alone — that the delivery lane's
mirror (`gitPushbackCheckRef` = `refs/remotes/gitPushcheck/master`) is the better
answer *iff* the local integration branch is an ancestor-or-equal of it **and**
the mirror carries at least one commit the local branch does not.

The fix makes the version question read the same answer. `integrationClaimTargetRef`
now delegates to `integrationClaimTargetMeasured`, which returns the ref **and**
the reason the mirror did not win (one measurement, read twice — Rule 8/13; a
second probe could disagree with the first). `rebuildTipForRepo`
(`rebuild_integration_ref.go`) then reads that ref's `--short` tip and hands the
close loop a `ExpectedVersion`, an `ExpectedRef` and a `MeasureNote`:

```
mirror wins                → Ref = refs/remotes/gitPushcheck/master, Note = ""
no mirror (or diverged)    → Ref = refs/heads/master,               Note = why
neither (peerless / test)  → Ref = HEAD,                            Note = why
```

**Nothing beyond the repository is read, and nothing is refreshed.** No fetch, no
timer, no state file, no new ref, no hook (Rule 35) — the mirror ref either
exists in this object store or it does not.

### The fallback is the shipped behaviour, by construction

A checkout with neither a mirror nor a local integration branch falls back to
`HEAD` — **the pre-fix measurement** — so peerless single-host and test
deployments behave exactly as before. What is new is that the fallback is
**named**, never silently substituted (Rule 15): `MeasureNote` is printed by the
close loop, so "measured against the ref the grounds follow" and "measured
against this checkout's own HEAD" can no longer read identically. That is the
whole reason the incident survived a month of green close-loop logs.

### Why the local integration branch is still preferred over HEAD

A repo with `refs/heads/master` but no mirror is exactly today's behaviour (the
branch tip *is* HEAD on a normal checkout), so preferring the branch name changes
nothing observable while making the resolution order uniform: the ref the grounds
are cut from, then HEAD. It also means a detached or off-branch checkout is still
measured against the integration branch rather than against whatever is checked
out.

## What the close loop now prints

```
task merge: rebuild ok — <repo> already current at <rev> [measured against refs/remotes/gitPushcheck/master]
task merge: rebuild ok — <repo> built in 12s (<rev>) [measured against refs/heads/master] note: … did not offer a fresher tip …
[merger] [rebuild] FAILED: <repo> cmd="…" exit=0 err=version mismatch: … [measured against refs/heads/master]
```

`rebuildMeasureSuffix` (cmd/agent) renders it; `RebuildResult.ExpectedRef` /
`MeasureNote` carry it, so every face (`task merge`, `task merge-execute`, the
merger pass, the `gopi merge` run report) prints the same fact.

## The honest edge — it cannot reverse a chosen ref

`integration_claim_target.go` documents that a checkout whose mirror is
*diverged* (the mirror is ahead, but the local branch holds commits it lacks)
abstains to the local ref. The close loop inherits that decision: on a diverged
checkout it measures — and logs — `refs/heads/master` with the reason. Staleness
is then still possible (the mirror may be ahead on one line while the local
branch holds another), and the operator's door is to reconcile the two refs; the
close loop does not pick a winner between them, which would be a guess about
which history the machine should run.

## Red proof

`internal/tools/rebuild_integration_ref.go`: the mutated line is

```go
ref, _, note := integrationClaimTargetMeasured(repo)
+ ref = taskGitHeadToken   // ← the pre-fix predicate, re-introduced
+ note = ""
```

with all five new tests red (2 of them failing on the arm the incident was made
of: the shortcut fires with `expected=<local tip>` while the mirror is ahead, and
a correct artifact fails the post-build assertion). Restored, and the restore
verified by **explicit revision**:

```
$ git diff HEAD -- internal/tools/rebuild_integration_ref.go      # prints nothing
$ test "$(git hash-object internal/tools/rebuild_integration_ref.go)" \
      = "$(git rev-parse HEAD:internal/tools/rebuild_integration_ref.go)"   # → true
```

## Layering (Rule 28), stated rather than assumed

Rebuild / close-loop machinery is the **system layer**, which Rule 28 puts on
peerbox. This row was dispatched here by the operator, so the change was made here
and the fact is recorded in the task report.

## What did NOT change (deliberately)

- No fetch, no pull, no ref advance, no scheduling, no daemon — the resolution is
  a read of refs that already exist.
- No new tool, verb, flag, event or config key.
- The post-build assertion (`verifyBuiltVersion`) and the `-dirty` reject keep
  their contracts; only the **tip they compare against** moved to the
  integration ref.
- `gitTip` is deleted rather than kept beside the new measurement — a second
  HEAD-tip helper is the defect waiting to be re-adopted.
