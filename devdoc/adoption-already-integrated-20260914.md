<!-- doc-class: historical -->
# Task Report: merger's misleading adoption/BUILD-gate messages

Branch: `fix/adoption-integrated-prune` (worktree `~/workspace/gopi-adopt-integrated`)
Trigger: operator screenshot of a foreground `gopi merger`
(`/home/user/.gopi/sessions/01a09faa…/attachments/tmp/merger_error.txt`).

## The evidence (attachment), and what each line is

```
[task-notify] dead-holder reclaim  (t-stale-sibling) skipped: repo directory /tmp/other-repo is gone — this row cannot be reconciled here
[serve] [adopt] scanned=11 adopted=0 ref-kept=0 dispatched=0 duped=0 skipped=9 failed=2
[serve] [adopt] fix/ledger-fixture-leak: commit: git commit --no-verify -m adoption: merge fix/ledger-fixture-leak (b9ae5cbc): exit status 1:
[serve] [adopt] fix/worker-ownership-pointer-master-red: commit: git commit --no-verify -m adoption: merge fix/worker-ownership-pointer-master-red (67445327): exit status 1:
[merger] [/home/user/Downloads/games/Maps] merged=0
[merger] [/home/user/work/GoPi] merged=0 protected=10
[task-notify] merger: BUILD gate ABSTAINS for fix/worker-gone-selfspawn (branch fails to build, but target master does not build either — FAIL) — not branch-introduced, merging unchanged
[merger] merge-verify: baseline sweep at 4c4be3741f9b: failing packages = github.com/nick/gopi/cmd/agent, github.com/nick/gopi/internal/stylegate, github.com/nick/gopi/internal/testutil
```

| Line | Verdict |
| --- | --- |
| dead-holder reclaim skip | **correct** (`reclaimDisposition`: a row whose repo directory is gone is unfixable here; the line states that in one honest sentence — `internal/tools/task_reclaim.go:268`). |
| `[adopt] … failed=2` with an **empty** `exit status 1: ` tail | **BUG (fixed)** — see D1. |
| `merged=0`, `protected=10` | correct counts (`downloading/games/Maps` is a configured repo). |
| BUILD gate ABSTAIN "…but target master does not build either — **FAIL**" | **BUG (misleading wording + avoidable builds, fixed)** — see D2. |
| `merge-verify: baseline sweep … failing packages = …` | **misleading wording (fixed)** — see D3. The failing set is the PRE-merge tip's own standing condition; the footer read like the merge's verdict. |

## D1 (root cause, proven) — a healthy, already-landed adoption reported as a failure, forever

`AdoptionSweep` ② probes with `git merge --no-commit --no-ff <branch>`
(`internal/tools/task_adoption.go`) and treats a **zero exit** as "a merge was
staged, so ③a commits it". But when the candidate's tip is **already contained
in master** git answers `Already up to date.` and stages **nothing** — while
still exiting 0. The ③a commit then has nothing to commit and fails.

Reproduced on the live repo (HEAD `fc6a9373`, 2026-09-14):

```
$ git merge-base --is-ancestor b9ae5cbc master && echo ancestor   → ancestor
$ git merge-base --is-ancestor 67445327 master && echo ancestor   → ancestor
$ git merge --no-commit --no-ff fix/ledger-fixture-leak
Already up to date.            (exit 0, no MERGE_HEAD)
$ git show --stat b9ae5cbc     → docs(briefings): … (committed with it)
```

Two failures compounded:

1. **The report's detail was empty.** `adoptionGit` built its error from
   **stderr only** (`cmd.Output()` keeps stdout, the error rendered
   `stderr.String()`), and for this shape git writes its explanation to
   **stdout** — `On branch master / nothing to commit, working tree clean`
   (verified in a scratch repo, and reproduced by the red-proof run). So the
   operator saw `exit status 1: ` and nothing else (Rule 15b: a channel that
   cannot say why is a channel nobody reads).
2. **The branch never converges.** Nothing prunes a leftover ref of an
   already-contained branch, so *every* serve pass re-printed `failed=2`
   for work that had already landed (the operator's screenshot shows the
   steady state, not a one-off).

**Fix**
- `adoptionGit` now falls back to **stdout text** when git's stderr is empty —
  the cause can no longer be dropped at the seam.
- The probe's ground truth is now `MERGE_HEAD` (`adoptionMergeStaged`), not its
  exit status: exit 0 **without** MERGE_HEAD = "already integrated". That case
  is **pruned in-pass** (`git branch -D`, reported through `RefKept` if git
  refuses — a linked worktree) and reported on its **own labelled line**,
  never through `failed=`:

```
[serve] [adopt] already-integrated: fix/ledger-fixture-leak: already integrated in master — nothing to merge; prune the leftover ref with: git branch -D fix/ledger-fixture-leak
```

## D2 — an ABSTAIN line that asserted a failure it had just disproved

```
merger: BUILD gate ABSTAINS for fix/worker-gone-selfspawn
  (branch fails to build, but target master does not build either — FAIL) — not branch-introduced
```

The parenthesised clause claimed a **red master** the gate had never measured:
the branch's tip was already **contained in master** (its work had landed and
the ref was left behind), so the branch *adds nothing* master does not have and
no failure can be branch-introduced. The wording (and the `— FAIL`) sent the
reader hunting for a master regression.

**Fix** — one ancestry read now precedes the master build and states the fact
(`git merge-base --is-ancestor <branch> <master>` = "master already contains
the branch"), skipping a whole build that could only ever reproduce the
branch's own outcome:

```
merger: BUILD gate ABSTAINS for fix/already-landed (master already contains fix/already-landed — the branch adds nothing, so there is no branch-introduced failure to find) — merging unchanged
```

**Direction matters, and the first attempt got it backwards**: the initial fix
asked "does the branch contain master?" — true for **every** branch ahead of
master, i.e. the ordinary merge case, so it abstained on exactly the
non-compiling branch this gate exists to refuse. The gate's own incident
fixture caught it (`TestMergeBuildGate_RefusesTheIncidentShape` flipped from
refusal to abstention when the full suite ran). The shipped criterion is the
reverse relation, pinned from BOTH sides by
`TestMergeBuildGate_MasterContainsBranchAbstainsOnAncestry` (contained ⇒
abstain) and `TestMergeBuildGate_AncestryDirectionKeepsTheRefusal` (ahead +
broken ⇒ still refused).

The comparative arm's wording was also corrected: a target that did not PASS
is reported as `did not PASS either — <status>`, never as "does not build".
Behaviour is unchanged in both arms (Abstain: merge proceeds).

## D3 — the merge-verify baseline footer read like the merge's verdict

`merge-verify: baseline sweep at <tip>: failing packages = …` is a **record of
the PRE-merge tip's own condition**, which on this repo is a standing one
(master's known-red packages). Read next to a merge, it looks like the merge's
result. The line now names both the tip and the role:

```
merge-verify: pre-merge baseline at 4c4be374 — the branch is judged as a DELTA against these packages, not by them: failing packages = … (none — the pre-merge state was green for the swept command)
```

## Completed
- D1: stdout fallback in `adoptionGit`; `MERGE_HEAD`-based already-integrated
  detection; in-pass ref prune + dedicated report line (both CLI faces);
  bounded one-line error detail (`adoptionErrDetail`, `adoptionErrDetailBudget`).
- D2: ancestry-based ABSTAIN with honest wording + one avoidable build removed.
- D3: baseline footer reworded (tip + role).
- Tests (red-proofed, see Verification): 3 new (`task_adoption_integrated_test.go`,
  `merge_build_gate_contained_test.go`) + baseline re-key.
- `devdoc/adoption-already-integrated-20260914.md` (this file).

## Not Done
- `dead-holder reclaim` and `merged=0 / protected=10` needed **no** change
  (classified correct above; changing them would be scope creep).
- The two leftover refs (`fix/ledger-fixture-leak`,
  `fix/worker-ownership-pointer-master-red`) are still in the operator's repo:
  the fix now PRINTS the one-line remedy instead of running it in a pass that
  could not reach them (the real repo's serve face is the operator's own
  merger). Verified remedy: `git branch -D <branch>` (both are ancestors of
  master; merged-cleanup's pair lane skips them — guard ①b has no reflog
  movement to authorise the delete).
- `fix/worker-gone-selfspawn` (the D2 subject) was already merged (`16f871c3`).

## Verification
- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (rebuilt after
  every source edit: `cmd/agent` e2e tests carry an `assertGopiBinIsFresh`
  guard and correctly failed with a stale binary during development).
- Tests (red-proof FIRST — each fix reverted with an `if false { }` guard):
  - BEFORE (pre-fix paths, new tests):
    - `TestAdoptionAlreadyIntegratedIsNotAFailure` FAIL —
      `Failed = [fix/already-landed: commit: git commit --no-verify -m adoption:
      merge fix/already-landed (04d6eb5): exit status 1: On branch master nothing
      to commit, working tree clean]` (the empty-tail defect, with the stdout
      cause visible only because this run already carried the seam fix);
    - `TestAdoptionAlreadyIntegratedKeepsTheRefWhenGitRefuses` FAIL (same);
    - `TestMergeBuildGate_MasterContainsBranchAbstainsOnAncestry` FAIL with the
      operator's exact wording — `(branch fails to build, but target master did
      not PASS either — FAIL)`.
  - AFTER (fix in place): all PASS —
    `go test ./internal/tools -run 'TestAdoptionAlreadyIntegrated|TestAdoptionGitErrorCarriesGitCause'`
    → ok; `go test ./internal/tools -run TestMergeBuildGate` → ok.
  - `go test ./internal/tools/... ./internal/standards/... ./internal/stylegate/... ./cmd/agent/...`
    → see `/dev/shm/testrun3.summary`.
  - Gates: `go test ./internal/standards/` → ok (1 baseline re-key,
    `internal/tools/task_adoption.go:265 → 346`, same site/reason, ratchet
    unchanged).
  - Pre-existing reds on master (NOT introduced here, reproduced on master with a
    rebuilt binary): `TestStyleGate` (`internal/doubaoasr/doubaoasr_test.go:458`
    time.Sleep), `TestWorkerDaemon_StatusReportsTheLiveDaemon`
    (`cmd/agent/worker_daemon_singleton_e2e_test.go:144`) — the latter's failure
    is the status line naming `…/001/.gopi/worker.lock` while the test asserts it
    must not name a path below the test home's `001` state dir. Both are in
    master's known-red set; master's own merger reports `failing packages =
    cmd/agent, internal/stylegate, internal/testutil` (the screenshotted line).
- Real-binary before/after on the SAME fixture (private `GOPI_HOME` +
  `settings.json` with `tmpdir`, a seeded completed row with
  `deliverable.branch=fix/leftover`, `fix/leftover` an ancestor of master):
  - master binary `gopi task adopt`:
    `failed=1` / `failed: fix/leftover: commit: git commit --no-verify -m
    adoption: merge fix/leftover (2bbee26): exit status 1: ` ← **empty tail**;
  - fixed binary: `scanned=1 … failed=0` +
    `already-integrated: fix/leftover: already integrated in master — nothing to
    merge; prune the leftover ref with: git branch -D fix/leftover`, and the
    second run is `scanned=0` (the reported condition is cleared, so the line
    does not repeat).
- Commit: subject `fix(adoption): an already-integrated branch is not a failed
  adoption` on branch `fix/adoption-integrated-prune` (tip of that branch, read
  it with `git log -1 fix/adoption-integrated-prune`); this file lands in it, so
  the hash is deliberately not self-recorded.

## Reusable lessons
- An exit status is not a state: `git merge --no-commit` returning 0 does not
  mean a merge exists (`MERGE_HEAD` does).
- `exec.Command(...).Output()` keeps stdout and renders only stderr in the
  error — a stderr-only rendering can produce a cause-less error message.
- A report line that is re-emitted identically on every pass is a convergence
  bug, not a cosmetic one: the fix must clear the condition it reports.
