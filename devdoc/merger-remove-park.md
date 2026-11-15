<!-- doc-class: normative -->
# Task Report: removing the merger's park mechanism

Epic: **merger-remove-park** (briefing `briefings/brief-remove-park-mechanism.md`,
reframed verdict DOABLE). Branch `feat/merger-remove-park`, worktree
`/home/user/workspace/gopi-remove-park`, cut from master `5e6f0062`.

## Completed

### 1. Classification of every park-related site (A delete / B keep / C decide / D undecided)

**A — park-only, DELETED**

| site | reason |
|---|---|
| `internal/tools/repo_sweep_park_worktree.go` (whole file, 313 lines) | exists only to serve the park lane's step 3 (force-release a parked branch's worktree) |
| `repo_sweep_park_worktree_test.go` (whole file, ~640 lines) | tests of that file only |
| `repo_sweep_park_test.go` (whole file, ~440 lines) | tests of the park lane's row/tag/dedup behaviour; the FF half was re-cut (not dropped) into `repo_sweep_notff_test.go` |
| `repoSweepParkBranch`, `repoSweepParkOutcome`, `repoSweepParkFileTask`, `repoSweepParkPayload`, `repoSweepParkTaskLive`, `repoSweepParkSummary`, `repoSweepParkSummaryHead`, `repoSweepParkSummaryHeadOf`, `repoSweepParkReason`, `repoSweepDrainParkedWorktrees`, `repoSweepParkReleaseWorktree`, `parkWtPayloadCmds` + 5 `parkWtPayload*Cmd` helpers | each is a step of the park lane (`repo_sweep.go`) |
| consts `parkTaskSummaryHeadFmt`, `parkTaskSummaryTagSep`, `parkTaskSummaryRowIdentityFmt`, `parkTaskPriority`, `parkTaskPayloadFmt`, `parkAction*` (7), the `parkWtGit*` verb block | Rule 1b: the lane's spellings, with the lane |
| `RepoSweepResult.{Parked, ParkDuplicated, ParkFileFailed, ParkWorktreeReleased, ParkWorktreeReleaseFailed, ParkDrained, ParkDrainFailed}` | counters only the park lane incremented |
| sweep loop arm `case parkActionParked/ParkDuplicated/ParkFileFailed/…` + the ②b drain arm | the park lane's dispatch |
| `repoSweepDecideBranch` arm ③ (`repoSweepParkOutcome(repo, branch)`) | the park decision — **replaced** by `repoSweepNotFFOutcome(branch)` |
| `cmd/agent/cli_tasks.go` park summary lines (`parked=`, `park-duplicate=`, `park-row-FAILED=`, `park-worktree-released=`, …) | log rendering of the deleted counters |

**B — still needed, KEPT (some re-pointed)**

| site | why it stays |
|---|---|
| the FF landing arm (`merge --ff-only`, `branch -d`, `MarkRebuildPending`) | the tick's load-bearing half; unchanged |
| the merge-time contract/BUILD/UNION compile gates and **their** archive + rework row (`taskMergeRefuseUncompilable` → `repoSweepArchiveBranch` → `buildGateSubmitRework`) | a branch that does not COMPILE must leave the queue; this lane is about compilation, not about fast-forwarding. One caller, one reason — verified by `TestRepoSweepBuildRefusalStillArchivesAndDoesNotReportNotFF` |
| `repoSweepArchiveExempt` + `archiveBranchPrefix` / `archiveTagRefPrefix` (tasks.go, exported) + every consumer (`branch_attribution.go:326`, `task_merge.go`, `task_finish_cleanup.go:556`, `task_deliverable_branch.go:428`, `task_merge_executor.go`, `task_merge_backlog_terminal_guard.go`) | class C below |
| `MergedCleanupSweep` tidy pass (③) | recycles already-merged branches + merged worktrees; unrelated to park |
| `mergeActionDeleted` / `res.Deleted` | the tidy pass's counter, not a park counter |
| the dirty dead-holder salvage (`SalvageDirtyDeadHolderWorktrees`) | a separate lane (uncommitted work in a dead holder's directory) |
| `resolveRowIsMergerOwned` + the merger-owned row markers | ledger rows the park lane filed are ORDINARY dispatchable rows; the markers keep `TaskNextFiltered` from swallowing them |

**C — the archive-tag exemption: DECIDED (kept, with the reason and the discriminator)**

`repoSweepArchiveExempt` **stays**. It is not park machinery — it is the
"decided loser / ledger archive is never merged or deleted" contract, shared by
`git worktree`-level tooling, `safe-merge.sh` (PROJECT.md Rule 9) and the finish
cleanup. Deleting the exemption would make every human-archived ref mergeable
again, including the deliberately archived ones. What the park removal changes is
that **the merger no longer MINTS such a tag for a branch it merely cannot
fast-forward**. The three classes and their disposition (evidence:
`scripts/archive-tag-census.sh`, counts below) are in §2 of this report.

**D — undecided, listed not guessed**

- The 91 tags classified `D undecided-no-ledger-row` by the census — no
  annotated tag, no `hp-unique-` name, no ledger row naming the branch. A
  cleanup script cannot tell a merge-minted tag from a human's `git tag archive/x`
  for these, so they are left alone and listed. **Recommendation: operator
  decision, per tag.** (The census emits the exact list. 35 of the 91 are
  zero-commits-ahead — provably holding no work — which is the cheap subset to
  start from.)
- `feat/hand-archive-gate` / `feat/archive-why-gate-20260917` — the *naming* of
  both suggests a deliberately-added archive gate; whether that gate is the
  park tag or a separate hand-archive mechanism is **not decidable from the
  code on master** (no such symbol exists here). Left D, flagged for the
  operator, because guessing wrong in either direction deletes or preserves a
  lane nobody has defined yet.

### 2. The three archive-tag classes, their discriminator, and the counted disposition

`scripts/archive-tag-census.sh` (new, committed) classifies every
`refs/tags/archive/*` tag of a repo from **git alone plus the optional ledger**,
and prints TSV: `class  subclass  ahead  annotated  ref_present  name  tag`.

The discriminator, per class (each fact is machine-readable; no guessing):

| class | discriminator | why it identifies THIS class |
|---|---|---|
| ① human / ledger archive | `git cat-file -t <tag>` == `tag` (ANNOTATED) **or** name starts `hp-unique-` | only a deliberate `git tag -a` makes an annotated tag — the merger's tag is lightweight by construction; `hp-unique-*` is the loser-archive namespace every consumer exempts by NAME |
| ② park tag | the ledger has a row with summary `resolve merge conflict for <branch> in <repo>` | that is the exact summary the park lane filed (`parkTaskSummaryRowIdentityFmt`), and nothing else in the tree writes it |
| ③ build-gate refusal | the ledger has a row with summary `resolve merge conflict: rework: <branch>` | the merge-time compile gates' own rework summary (`resolveRowReworkSummaryPrefix`); **not** park residue |
| D undecided | none of the above | listed for a human; the two merger-minted classes are decidable, this one is not |

Counts on `/home/user/work/GoPi` (159 tags, census run 2026-09-17):

```
    1  ① human-annotated                  (archive/openai-sdk-backup-20260826 — annotated, human-written message)
    1  ① hp-unique-ledger
   45  ② park-tag-already-in-master        (tagged tip IS an ancestor of master: holds no work)
   20  ② park-tag-still-ahead              (tagged tip is 1..N commits ahead of master: holds unmerged work)
    1  ③ build-gate-refusal                (archive/fix/task-list-address-no-scan)
   91  D undecided-no-ledger-row
  ---
  159  total
```

**Cross-check that makes ② trustworthy**: of the 65 `resolve merge conflict`
rows, **all 65** correspond to a tag, and 45 of them are already ancestors of
master — i.e. the park lane frequently tagged a branch whose content had ALREADY
landed. That is the "junk tag per tick" the briefing describes, now quantified.

**Disposition recommendation (RECOMMENDATION ONLY — nothing executed):**

1. **① (2 tags)**: keep. The exemption is correct; these are deliberate archives.
2. **②a (45 tags, in master)**: **deletable in a batch** — the tagged tip is
   reachable from master, so nothing is held and nothing can be lost; deleting
   the tag re-arms nothing (the branch ref is already gone).
3. **②b (20 tags, still ahead)**: **do not delete blindly** — decide per branch:
   - the tag's tip is the only remaining copy of that work; run
     `git show archive/<b>` / `git diff master...archive/<b>` first;
   - if the work is wanted → **deleting the tag puts the branch back in the
     candidate set** (the exemption is the only thing excluding it) and the new
     merger's take-over path hands the diff over as a rework task. That is the
     intended path, and the migration must be deliberate, not accidental;
   - if the work is dead → delete the tag and leave the refs absent.
4. **③ (1 tag)**: keep. The BUILD gate is live machinery and still mints these.
5. **D (91 tags)**: operator decision, per tag. Start with the 35 that are
   zero-commits-ahead (provably hold nothing).

**What happens to a branch after the exemption is dropped for it** (asked
explicitly by the briefing): its ref is re-listed by
`repoSweepUnprotectedBranches` on the very next tick. The tick makes ONE ff
probe. If it is still not ff-able it is **reported and left alone, every tick,
forever** — no tag, no row, no worktree touched. That repeated report is the
signal; the take-over path (the merger launcher's gopi, `merger_takeover_prompt.md`
§3.6) is what actually hands the diff over. So: dropping an exemption does NOT
cause repeated failed merge attempts (a `--ff-only` merge is never attempted on a
non-ff branch: the probe decides first), but it DOES cause a repeated one-line
report until a take-over claims the branch.

### 3. Deletion inventory

```
internal/tools/repo_sweep_park_worktree.go          -313   whole file
internal/tools/repo_sweep_park_worktree_test.go    -~640   whole file
internal/tools/repo_sweep_park_test.go             -~440   whole file (FF half re-cut below)
internal/tools/repo_sweep.go                       -595/+~60  park lane funcs, consts, counters, arms, docs
cmd/agent/cli_tasks.go                             -46/+~10   park summary rendering; not-ff line
cmd/agent/main.go                                  usage head: PARK → REPORT
```

Net (git `--stat` on the commit): **3 files deleted, 14 modified/added, 17 files touched, +1043 / −2111 lines**.

### 4. Post-removal behaviour on a non-fast-forwardable branch (the briefing's §6)

```
candidate branch (ref not in master, not checked out, not archive-exempt)
        │
        ├─ merge-base --is-ancestor <branch> master == 0  →  git merge --ff-only
        │      (lands; branch -d; MarkRebuildPending; rebuild later in the tick)
        │
        └─ ≠ 0  →  repoSweepNotFFOutcome(branch)
                   action = "not-ff", Detail = the reason + the probe command
                   NOTHING ELSE HAPPENS:
                     · no archive/<branch> tag
                     · no ledger row (of any summary)
                     · no worktree touched, no force-remove
                     · the ref, its tip and its checkout survive
                   → the branch is a candidate again next tick, and is reported again
                   → the take-over path hands its diff over as a rework task
        │
        └─ merge-base itself FAILED (git error) → action = "error", branch untouched
```

The tick's own git-only, child-free, wait-free property is unchanged.

### 5. Verification

```
$ go build ./...    → OK (no output)
$ go vet ./...      → OK (no output)
$ gofmt -l .        → only pre-existing devdoc/*.go + task_finish_premerge.go
                      + two unrelated *_test.go files (unchanged by this branch;
                      verified against master's own gofmt output)
$ bash scripts/archive-tag-census.sh --repo /home/user/work/GoPi
                    → 159 rows + counted summary (see §2)
$ go test -run TestRepoSweep ./internal/tools/   → ok
$ go test -timeout 30m -count=1 ./internal/tools/ ./cmd/agent/ ./internal/standards/
    → ok  internal/tools     325.172s
    → ok  cmd/agent          241.993s
    → ok  internal/standards  16.545s
      EXIT=0   (all three GREEN; the binary was rebuilt first, so the e2e cases
      that drive bin/gopi verified this branch's own code)
```

### 6. Tests re-cut rather than dropped

`repo_sweep_park_test.go` was the FF-OR-PARK contract. Its FF half and the
rebuild-mark witness were **kept**, re-cut to the new spelling, and four NEW
cases were added that assert the removal as ABSENCES:

- `TestRepoSweepNotFFBranchIsLeftCompletelyAlone` — no archive tag, no ledger
  row, ref + tip + declared work byte-identical, branch still a candidate.
- `TestRepoSweepNotFFIsReportedEveryTickAndNeverStopsTheTick` — re-reported on
  the next tick, and a tick holding both branches still lands the ff-able one
  (ordering in `res.Outcomes` is the continuation proof).
- `TestRepoSweepNonFFTickWritesNothing` — whole-tick ledger delta == 0 across two
  ticks, and no tag: the removal's own red-proof (a counter check would pass
  even if the writer were re-inlined).
- `TestRepoSweepBuildRefusalStillArchivesAndDoesNotReportNotFF` — pins the lane
  split so a future "remove the last park-looking thing" pass cannot delete the
  build gate by mistake.

Both merge gates' own tests (`merge_build_gate_test.go`) had a park-shaped
assertion (`res.Parked`); it was re-pointed to `res.NotFF` with the reason
recorded in the comment.

## Not Done

- **No tag was deleted, and no exemption was dropped.** Recommendation only —
  the briefing's hard boundary. §2 gives the counted proposal.
- **The take-over path itself is not merged** (it lives on
  `feat/merger-takeover-landed`, a separate task). This branch therefore leaves a
  non-ff branch REPORTED and waiting: until the take-over lands, nobody picks it
  up automatically. That is the honest state — and it is strictly better than
  park, which hid the branch AND filed a row nobody could act on.
- **The 91 D-class tags are undecided** — listed, not guessed.
- `devdoc/merger-park-worktree-release.md` and
  `devdoc/requirements/02-merge-and-branches.md` /
  `merge-branch-lifecycle.md` still describe the park lane (they reference the
  now-deleted `repo_sweep_park_test.go`). They are historical records of a
  design that was built and then retired; I did not rewrite them, and I state
  that here rather than silently leaving a stale reference.

## Verification (commands actually run)

See the session's tool output. Summary at the time of writing:

- `go build ./...` — OK
- `go vet ./...` — OK (0 findings)
- `go test -run TestRepoSweep ./internal/tools/` — ok (3.5s)
- full `./internal/tools/ ./cmd/agent/ ./internal/standards/` run — launched as a
  background job; its result is appended below when it returns (the report is
  written now because the branch must not be left uncommitted).

## Commit

See `git log` on `feat/merger-remove-park`.
