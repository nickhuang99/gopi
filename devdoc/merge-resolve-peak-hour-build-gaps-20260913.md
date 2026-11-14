<!-- doc-class: historical -->
# Task Report: the reverted merge of `fix/peak-hour-build-gaps` — reproduce the red, fix the merged state, leave it landable

> **Revision 4 (2026-09-13, task `01a09957`, HANDOFF).** Master is still
> `cec425cc`; the delivered tip is **the commit carrying this revision**
> (parent `21f9c67d` = the 6th forward-merge `6c370aa9` + `cec425cc`) — its
> exact SHA and tree are declared in this task's handoff message, and its two
> immediate ancestors are `21f9c67d` (merge) and the rev-3 docs commit
> `f067b5ef`. Handoff facts, all re-measured on this exact tree in the handoff
> worktree `/home/user/workspace/gopi-peak4-merged` (removed immediately after
> this commit, per gate [c]):
>
> - `git merge-base --is-ancestor master fix/peak-hour-build-gaps` → **rc 0**
>   (ff-mergeable against current `cec425cc`).
> - `git diff --name-only master HEAD` → **`devdoc/merge-resolve-peak-hour-build-gaps-20260913.md`
>   only** (0 non-`.md` paths); non-`.md` `git ls-tree -r` digest
>   `b02406875ceb647a33d870769209f258404c8032` is **identical** to master's.
> - `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **rc 0**;
>   `CGO_ENABLED=0 go build ./...` → **rc 0**.
> - The delta-zero differential sweep (§7, run5) was measured on this exact
>   code tree: control `db1ac4aa` vs merged `6c370aa9`, FAIL-set diff **EMPTY**.
> - No force-push, no rebase: the original fix commits `fda4e364` / `8af9c6f2` /
>   `dc4c1753` / `88f2c454` / `e51b554c` / `900f0276` are all still reachable
>   from `master` (verified `git merge-base --is-ancestor <c> master` → rc 0 for
>   each), i.e. the pre-existing history was never rewritten.
> - `bash scripts/safe-merge.sh fix/peak-hour-build-gaps` → **✅ exit 0** with
>   the worktree removed (gate [c] passes only when no worktree holds the
>   branch).
>
> **Revision 3 (2026-09-13, this session, task `01a09957`).** §1–§4 stand
> unchanged. Master advanced twice more behind the branch — `db1ac4aa` →
> `828ba83a` (docs-only) → **`cec425cc`** (`Merge branch 'feat/worker-once-purpose'`;
> `git diff --name-only db1ac4aa cec425cc` = 2 `devdoc/` files, **0** non-`.md`
> paths) — so the branch was forward-merged a **sixth** time and its tip is now
> **`21f9c67d`** (tree `bf41b814`, parents `6c370aa9` + `cec425cc`). The
> docs-only advances do not invalidate the differential sweep: the non-`.md`
> `git ls-tree -r` digest is **identical** (`f84993139da5b750…`) at every ref
> involved — `db1ac4aa`, `828ba83a`, `cec425cc`, `6c370aa9`, `21f9c67d` — so the
> Go tree under test is byte-for-byte the code tree the sweep measured, and no
> branch-attributable test outcome is possible. The sweep **completed** this tick
> with a **DELTA-ZERO** verdict (7 identical failures, both trees — §7), the
> standards gate was re-probed live in both directions on the delivered tip, and
> the build was re-verified at `21f9c67d` (§5).
>
> **Revision 2 (2026-09-13, earlier tick, task `01a09957`).** §1–§4 (the
> reproduction, the root cause and the "no new code commit is owed" argument)
> stand as written and unchanged. What changed again is everything that names a
> *commit*: master moved `791d81cc` → **`db1ac4aa`** (`6c2ad527` +
> `bfd9c32b` + `db1ac4aa`, the worker-state-attestation line), so this branch
> was forward-merged a **fifth** time and its tip is now **`6c370aa9`** (tree
> `a787d8db`, parents `af653147` + `db1ac4aa`). The old tip `af653147` was
> **not** ff-mergeable against the new master (`git merge-base --is-ancestor
> master af653147` → rc 1), which is why the refresh was owed. §3, §5, §6 and §7
> carry the refreshed numbers, and the differential sweep was re-run from
> scratch on the new pair of trees (pristine-master control `db1ac4aa` vs the
> merged tip) — driver and raw logs under `/home/user/workspace/peak4-logs/`.
> The single-file content delta over master is unchanged (this report), and the
> non-`.md` tree digest of the two trees is **byte-identical**
> (`sha256` of `git ls-tree -r HEAD | grep -v '\.md$'` →
> `f8499313…11423` on both), so the code tree under test is still master's tree
> byte-for-byte.
>
> **Revision 1 (2026-09-13, earlier session).** §1–§4 stand. Master moved
> `30d1a6f7` → `791d81cc` (`a106b98f` + `791d81cc`, the
> `feat/version-tag-every-revision` line), so the branch was forward-merged a
> **fourth** time and its tip became `9252ed69` instead of `215dc0aa`; the
> earlier revision's numbers are preserved below where they are still
> informative.

## Summary

The merger's post-merge verify judged the merged tip `fda4e364` RED
(`cmd/agent/cli_tasks_worker.go:58:2: "sync/atomic" imported and not used`),
the round was auto-reverted (`master` → `900f0276`), and the work was left
unshipped. This report reproduces that red first-hand, derives its root cause,
states what remediation the merged state actually owed, and records the handoff
state of the branch.

**Bottom line up front:** the red is a **content incoherence introduced by the
revert/repair, not a bad branch tip**: *both* of `fda4e364`'s parents build
green on their own, yet the merge of the two yields an import with no consumer.
The branch's intent — *close the pre-merge undefined-symbol build gaps* — is
already satisfied on current master, so the merged state builds green with **no
new code commit**. The branch has been reshaped onto the **real fix line**
(`fda4e364`, the reverted merge commit) and forward-merged with current master
(`db1ac4aa`, the 5th refresh); its merged tree (`a787d8db`) differs from
master's tree (`bf14c503`) only by this report (0 non-`.md` paths; the non-`.md`
`ls-tree` digests are equal), and both build exit 0. The landing is therefore a
**content no-op** apart from this document — stated plainly in §6, because a
merger must not expect a code change from it.

## 1. The red, reproduced (this session, on the branch's own commits)

Reproduction on a pristine detached worktree at the reverted merge tip, with a
module cache and `GOPROXY=off` (no network dependence):

```console
$ git -C /home/user/workspace/gopi-peakgaps-red rev-parse HEAD
fda4e364c17c9cf6e99f13f11137686ebc076ee2
$ CGO_ENABLED=0 go build ./cmd/agent/ ; echo rc=$?
# github.com/nick/gopi/cmd/agent
cmd/agent/cli_tasks_worker.go:58:2: "sync/atomic" imported and not used
rc=1
$ CGO_ENABLED=0 go build ./... ; echo rc=$?
cmd/agent/cli_tasks_worker.go:58:2: "sync/atomic" imported and not used
rc=1
$ grep -c 'atomic\.' cmd/agent/cli_tasks_worker.go     # consumers of the import
0
$ ls cmd/agent/peak_guard.go
ls: cannot access 'cmd/agent/peak_guard.go': No such file or directory
```

The whole delta `fda4e364` introduced over its **master parent** is that one
import line:

```console
$ git diff --stat 900f0276 fda4e364          # 900f0276 = fda4e364's 1st parent
 cmd/agent/cli_tasks_worker.go      |  1 +
 cmd/agent/cli_tasks_worker_stop.go |  6 ++-
 devdoc/fix-peak-hour-build-gaps.md | 90 ++++++++++++++++++++++++++++++++++++++
 3 files changed, 96 insertions(+), 1 deletion(-)
```

so the merged tree carried an import whose only consumer did not exist in that
tree: `cmd/agent/peak_guard.go` is **absent** from `fda4e364`'s tree, and the
`peakWarned atomic.Bool` declaration the file was supposed to consume is absent
too (`grep -c peakWarned` on `900f0276:cmd/agent/cli_tasks_worker.go` → `0`).

## 2. Root cause (5-why) — a revert-induced incoherence, not a bad branch

1. **Symptom** — `"sync/atomic" imported and not used` at the merged tip.
2. **Mechanism** — the merge pairs a **branch side that re-adds the import**
   with a **master side from which the consumer was removed**. Measured, at
   each ref, with `peak_guard.go` presence / `peakWarned` count / `sync/atomic`
   import count in `cli_tasks_worker.go`:

   | ref | role | peak_guard.go | peakWarned | sync/atomic |
   |---|---|---|---|---|
   | `bd42e827` | merge base (`feat/peak-hour-guard` tip) | YES | 3 | **0** ← gap 1 |
   | `8af9c6f2` | **branch side** (`dc4c1753` + report) | YES | 3 | 1 |
   | `900f0276` | **master side** (post-revert) | **no** | **0** | **0** |
   | `fda4e364` | the merge (`900f0276` + `8af9c6f2`) | no | 0 | **1** ← orphan |
   | `master` today | repaired by its own line | YES | 3 | 1 |

3. **Why the sides diverge on the consumer** — the base `bd42e827` carried the
   peak-hour feature *already merged*. Between the base and `900f0276`, master
   **reverted** that merge (`9cbeb9e5 Revert "Merge branch 'feat/peak-hour-guard'"`),
   deleting `peak_guard.go` and the `peakWarned` site. The branch side
   (`dc4c1753`) was authored *against the base*, so it "fixed" the base's gap 1
   (missing import) and gap 2 (`remaining` out of scope) while assuming the
   base's consumer still existed.
4. **Why a merge cannot reconcile this** — git merges **text**, not semantics.
   The master-side deletion of `peak_guard.go` is not a conflict against the
   branch side (which never touched that path), and the branch side's one-line
   import addition is not a text conflict against master's import block
   (master's block simply has no `"sync/atomic"` line to collide with). Both
   hunks apply cleanly and the result is incoherent — the classic **semantic
   conflict** that a textual merge reports as "clean".
5. **Norm broken** — the revert and the branch's fix encode **mutually
   contradictory premises** about the same feature's presence ("peak-hour is
   merged" vs "peak-hour is not merged"). Nothing forced the two premises to be
   reconciled before the merge; the build did, one commit later. *Both parents
   build green on their own* (`900f0276` rc=0, `8af9c6f2` rc=0, measured),
   which is exactly why a parent-only build gate cannot prevent this class — the
   break exists only **in the merged state**, so only a pre-merge *merged-state*
   build catches it. (Everything in the table is an ancestor of master today:
   `fda4e364`, `900f0276`, `8af9c6f2`, `dc4c1753`, `bd42e827`, `9cbeb9e5` all
   report `--is-ancestor master` = YES.)

## 3. The remediation: reshape the branch onto the fix line, forward-merge current master

The branch ref had been deleted by the merger's contained-merge sweep after the
content reached master by another path; the rework recreated it **on the
reverted merge tip itself** — the line that carries the fix commits — rather
than on a master tip (an earlier attempt put the ref on `e51b554c`, a *master*
commit, which produced a content-free tip; that attempt is superseded).

In a linked worktree (Rule 6), no rebase, no force-push — forward merge only.
Master moved during the task (`b84eb68d` → `e8e94ca0` → `8f4042b3` →
`30d1a6f7` in the first session, then `a106b98f` → `791d81cc` in revision 1,
then `6c2ad527` → `bfd9c32b` → `db1ac4aa` in revision 2), so the branch was
forward-merged **five** times; each master advance was re-baselined onto the
*then-current* master, and the last one is the handoff tip:

```console
$ git checkout -B fix/peak-hour-build-gaps fda4e364
$ git merge --no-ff master    # (1st) master b84eb68d -> fb694642   CONFLICT-FREE
$ git merge --no-ff master    # (2nd) master 8f4042b3 -> 0c923b18   CONFLICT-FREE
$ git merge --no-ff master    # (3rd) master 30d1a6f7 -> 215dc0aa   CONFLICT-FREE
$ git merge --no-ff master    # (4th) master 791d81cc -> 9252ed69   CONFLICT-FREE
$ git merge --no-ff master    # (5th) master db1ac4aa -> 6c370aa9   CONFLICT-FREE
$ git merge --no-ff master    # (6th) master cec425cc -> 21f9c67d   CONFLICT-FREE
$ git log -1 --format='%H %P'
21f9c67d88453b1dd7e25cbc8eecc1099b6f0f22 6c370aa9dbad6007dcf03db736594a756fadb7ae cec425cc...
```

The 6th merge is conflict-free for the same structural reason as the 5th: the
incoming master commits (`db1ac4aa..cec425cc`) are **docs-only** (`git diff
--name-only db1ac4aa cec425cc` → `devdoc/guard-test-daemon-env-isolation-rework-2026-09-13.md`,
`devdoc/worker-once-vs-task-run-next.md`; **0** non-`.md` paths), and the branch
contributes no code hunk, so nothing is arbitrated. Measured on the refreshed
tip: `git diff --name-only master HEAD` prints exactly one path, this report;
and since master is an ancestor of the tip, `git merge-base master HEAD` *is*
master, so the branch's own delta over its merge base is that same single `.md`
file.

The 5th merge is conflict-free for a structural reason that is worth stating
precisely: **the branch contributes no code hunk at all**, so git had nothing to
arbitrate. Measured on the refreshed tip — `git diff --name-only master HEAD`
prints exactly one path, this report; and since master is already an ancestor of
the tip, `git merge-base master HEAD` *is* master, so the branch's own delta over
its merge base is that same single `.md` file. The incoming master commits
(`791d81cc..db1ac4aa`) touch `cmd/agent/cli_tasks_worker.go`,
`cmd/agent/cli_worker_status.go`, `cmd/agent/cli_worker_status_test.go`,
`cmd/agent/worker_status_residue_e2e_test.go` and two `devdoc/` files — the
worker-status-attestation line — and every one of those master edits simply
arrives unchanged, because nothing on the branch side competes for those lines.

Every tip's first parent reaches back to `fda4e364` — so the commit the
merger's verify judged red stays reachable: nothing was rewritten, and no
published history moved (reflog: `Created from e51b554c` → reset to `fda4e364`
→ merge → merge; no `push -f`, no rebase).

**Why the merged state is now green, mechanically:** master's current tree
carries **both halves** of the intended fix, and the forward merge adopts that
tree:

```console
$ git show master:cmd/agent/cli_tasks_worker.go | sed -n '58p'
    "sync/atomic"                                    # the import (dc4c1753's hunk 1)
$ git show master:cmd/agent/cli_tasks_worker.go | grep -n 'peakWarned'
705:// peakWarned latches the one-shot report of an unusable peak-hours
708:var peakWarned atomic.Bool                          # the consumer
713:    return peakWarned.CompareAndSwap(false, true)
$ git show master:cmd/agent/cli_tasks_worker_stop.go | sed -n '415,421p'
        step := workerStopPollInterval
        // remaining is declared in the LOOP-BODY scope (not inside the if):
        // the peak-hour guard below clamps its wait against the same
        // time-to-deadline, so the value must outlive the clamp statement.
        remaining := time.Until(deadline)
        if remaining < step {                        # dc4c1753's hunk 2, byte-identical
```

Import + consumer coexist, and the hoisted `remaining` is present — the red's
mechanism is structurally gone, not merely unobserved.

## 4. Why no new code commit is owed

The epic's constraint is to keep the original fix's *intent*: close the
pre-merge undefined-symbol gaps. That intent is discharged — the original fix
closed **3 errors in 2 files** (its own words), and all three lines are on
master:

- gap 1 (`atomic` undefined → missing `sync/atomic` import) — present at
  `master:cmd/agent/cli_tasks_worker.go:58`, with its consumer at `:708`/`:713`;
- gap 2/3 (`remaining` undefined ×2 at the peak clamp) — the loop-body hoist is
  present at `master:cmd/agent/cli_tasks_worker_stop.go:415-421`.

Both hunks are `dc4c1753`'s, on master via the feature's own re-landed line.
Adding a *new* code commit that re-applies already-landed lines would be
duplicate work, and a code change invented merely to give the branch a diff
would be worse than a no-op — it would be an unjustified edit to a green tree.

## 5. Verification

Both worktrees are linked worktrees of the main repo; `bin/gopi` is gitignored,
so it was built fresh in each (a launcher-driving test must not read a stale
binary). `TMPDIR`, `GOCACHE` and the suite home for every run are under
`/home/user/scratch/peakgaps-run3/` (`stat -c %d` → `2052`) versus `/` (`2051`)
— a different device id, i.e. off the `/` partition (Rule 22, measured not
spelled).

Build, on the refreshed merged tree (tip `6c370aa9`, tree `a787d8db`,
worktree `/home/user/workspace/gopi-peak4-merged`):

| check | merged tip `21f9c67d` / `6c370aa9` | control (pristine master `cec425cc` / `db1ac4aa`) |
|---|---|---|
| `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | exit 0 | exit 0 |
| `CGO_ENABLED=0 go build ./...` | exit 0 | exit 0 |
| `CGO_ENABLED=0 go build ./cmd/agent/` at `fda4e364` (the red) | **exit 1** — `"sync/atomic" imported and not used` | n/a |
| `go build ./...` on each parent in isolation | `900f0276` exit 0 · `8af9c6f2` exit 0 (both green) | — |

Content / gates:
- `git diff --name-only master 21f9c67d` → **1 file**, this report: the merged tip
  introduces **no code delta** over pristine master. Stronger than a path list:
  the non-`.md` `git ls-tree -r HEAD` digests of the two trees
  hash **identically** (`f84993139da5b750e4d8b2407ca64ff45624156a923c40c554e7011bfca11423`)
  — every tracked non-markdown path has the same mode, blob and path, so the Go
  tree under test is master's Go tree byte-for-byte, and no branch-attributable
  test outcome is *possible* (the sweep below is corroboration, not the argument).
  Measured at each ref: `db1ac4aa` = `828ba83a` = `cec425cc` = `6c370aa9` =
  `21f9c67d` → same digest (`f84993139da5b750…`).
- Rule 16 baseline permit: the re-keyed permit
  `internal/standards/baseline.go:173` → `"cmd/agent/main.go:982"` still names a
  real site on the **current** master (`db1ac4aa`): `grep -n 'escape a literal
  prompt' cmd/agent/main.go` → exactly one line, 982, in **both** worktrees
  (master's `791d81cc..db1ac4aa` delta touches no `cmd/agent/main.go`), and the
  permit file itself is byte-identical in both trees. `go test -count=1
  ./internal/standards/` on the refreshed merged tree `6c370aa9` → **ok**
  (21.155s) and again on the delivered tip `21f9c67d` → **ok** (5.875s) — the
  Rule 16 gate is live, not green-blind. (Earlier tips, same
  verdict: control `7.09s` @ `8f4042b3` vs merged `6.31s` @ `0c923b18`, and
  merged `8.175s` @ `215dc0aa`.) Re-keyed, never relaxed — the documented
  455 → 961 → 982 chain is kept in place. **Both gate directions fail on the
  delivered tip** (probed in-place on `21f9c67d`: a probe file written, the test
  run, the probe removed; `git status --porcelain` clean afterwards apart from
  this report):
  - *green baseline*: `go test -count=1 -run TestStandardGates ./internal/standards/`
    → `ok` (3.49s);
  - *new hit*: a fresh `fmt.Errorf("probe: %v", err)` inside the scanned package
    `internal/gopilog` → `FAIL TestStandardGates/TestGate1ErrorWrapRedProof`:
    `Rule 16 … 9 total hits, 8 baselined, 0 escaped, 1 new (unannotated)` +
    `NEW violation (checklist 2.2) at internal/gopilog/zz_probe_hold.go:10`;
  - *stale permit*: `982` → `981` → `FAIL`: `STALE baseline entry
    cmd/agent/main.go:981 matches no violation` **plus** `NEW violation … at
    cmd/agent/main.go:982`;
  - restored → `ok` (5.260s). So the permit is live in both directions, not
    green-blind.
- Differential whole-suite `go test -count=1 ./...` (re-run from scratch on the
  current pair): control worktree `/home/user/workspace/peak5-ctl` (pristine
  master `db1ac4aa`, detached, tree `bf14c503`) versus merged worktree
  `/home/user/workspace/peak5-mrg` (merged tip `6c370aa9`, detached, tree
  `a787d8db`), **identical driver and env** — a fresh shared `GOCACHE` under
  `/home/user/scratch/peak5/` (off the `/` partition: device `2052` vs `/`'s
  `2051`, measured with `stat -c %d` per Rule 22), `GOPROXY=off`, `CGO_ENABLED=0`,
  `GOPATH`/`GOMODCACHE` pinned, and per-limb `HOME`/`TMPDIR` under the same
  scratch root. Compared as a **FAIL-set diff**. Driver:
  `/home/user/workspace/peak4-logs/sweep2.sh` (one limb per invocation, so the
  two limbs are literally the same code path); raw logs `ctl.log` / `mrg.log`,
  fail-sets `ctl-fails.txt` / `mrg-fails.txt`, pkg tallies `ctl-pkgs.txt` /
  `mrg-pkgs.txt` in the same directory. §7 records the verdict verbatim.
  Note again that the trees are **identical in code** (equal non-`.md` tree
  digest, above), so the FAIL-set is **empty by construction** — the empirical
  sweep is corroboration, not the argument.

## 6. Handoff state (Rule 6d — the deliverable's persistent location)

- **Branch**: `fix/peak-hour-build-gaps`. Its **merge commit is
  `21f9c67d88453b1dd7e25cbc8eecc1099b6f0f22`** (parents the previous tip
  `6c370aa9dbad6007dcf03db736594a756fadb7ae` + master `cec425cc`), with this
  report edit committed on top. The tip is therefore *HEAD of the branch* —
  `git -C /home/user/work/GoPi rev-parse fix/peak-hour-build-gaps` — not a
  literal SHA in this file: a commit that names its own SHA cannot exist, and the
  report commit touches only `devdoc/`. The merge commit's tree is
  `bf41b8145294bc8111894f4701d894f462f136c4` and its non-`.md` digest is already
  identical to master's, so the code tree the merger will verify is master's.
- **Mergeability**: `git merge-base --is-ancestor master fix/peak-hour-build-gaps`
  → exit 0 (master is an ancestor: **ff-mergeable**, no rebase owed). Re-checked
  at handoff against master `cec425cc`.
- **Landing is a content NO-OP** apart from this report: the tip's tree differs
  from master's tree only by this document, so merging it changes no code. This
  is deliberate and is the epic's acceptance condition ("branch-only delta
  zero"); it also means the branch **does not re-land the peak-hours feature** —
  master already carries it. If a merger wants a branch that *changes* master,
  this is not it.
- **No force-push / no rebase**: the branch reflog holds only `merge` entries
  (six of them), `Reset to fda4e364` and `Created from e51b554c`; every earlier
  commit (`fda4e364`, `dc4c1753`, `8af9c6f2`, `bd42e827`, `e51b554c`,
  `215dc0aa`, `c190a762`, `9252ed69`, `af653147`, `6c370aa9`) still reports
  `--is-ancestor fix/peak-hour-build-gaps` = YES — nothing was rewritten, so the
  commit the merger's verify judged red (`fda4e364`) stays reachable.
- **Recovery**: `git -C /home/user/work/GoPi show fix/peak-hour-build-gaps:devdoc/merge-resolve-peak-hour-build-gaps-20260913.md`.
- **`safe-merge.sh`**: at handoff the branch's *own* worktree is removed (gate
  **[c]** — worktree occupancy — is the only gate that can block it), leaving
  ✅ exit 0.

## 7. Differential suite result (control vs merged tip)

Control = `/home/user/workspace/peak5-ctl` (pristine master `db1ac4aa`,
detached, tree `bf14c503`) · merged = `/home/user/workspace/peak5-mrg`
(merged tip `6c370aa9`, detached, tree `a787d8db`). Both ran the same driver
`/home/user/workspace/peak4-logs/sweep2.sh` (one limb per invocation, so the two
limbs are literally the same code path) with the same env (shared fresh
`GOCACHE` under `/home/user/scratch/peak5/` — off the `/` partition, device
`2052` vs `/`'s `2051`, measured with `stat -c %d` (Rule 22) — `GOPROXY=off`,
`CGO_ENABLED=0`, `GOPATH`/`GOMODCACHE` pinned, per-limb `HOME`/`TMPDIR` under
the same scratch root). Both limbs run **concurrently** on the same host, so
they see the same load; the price is a slower wall-clock, the gain is that a
load-sensitive test flakes in both trees rather than in one.

```console
$ sed -E 's/ \([0-9.]+s\)$//' /home/user/workspace/peak4-logs/ctl-fails.txt | sort -u > ctl-fails-norm.txt
$ sed -E 's/ \([0-9.]+s\)$//' /home/user/workspace/peak4-logs/mrg-fails.txt | sort -u > mrg-fails-norm.txt
$ diff ctl-fails-norm.txt mrg-fails-norm.txt
$ echo rc=$?
rc=0
```

**VERDICT: DELTA ZERO.** After removing the wall-clock suffix (which carries no
information), the two FAIL sets are **identical** — 7 tests, the same 7 in both
trees (`TestAgencyReplay_LiveLedgerIdempotent`, `TestMemoryFeedbackSessionDedup`,
`TestMemoryFeedbackSuggestCold`, `TestStyleGate`, `TestSuiteTempRootIsPinned`,
`TestTask_BarePrompt_SIGTERM_WritesSessionEnd`,
`TestWorkerOwnershipPointerPresent` — the host's known pre-existing reds, present
in pristine master). Both limbs: `agent_build_rc=0`, `build_all_rc=0`,
`test_rc=1` (the same 7). Package tallies agree line-for-line except timings.

**Raw FAIL lines** (`--- FAIL:` verbatim, timings as measured):

```console
$ grep -E '^\s*--- FAIL' /home/user/workspace/peak4-logs/ctl.log | sed 's/^[[:space:]]*//'
--- FAIL: TestAgencyReplay_LiveLedgerIdempotent (0.00s)
--- FAIL: TestMemoryFeedbackSessionDedup (8.81s)
--- FAIL: TestMemoryFeedbackSuggestCold (34.99s)
--- FAIL: TestStyleGate (20.07s)
--- FAIL: TestSuiteTempRootIsPinned (0.00s)
--- FAIL: TestTask_BarePrompt_SIGTERM_WritesSessionEnd (5.91s)
--- FAIL: TestWorkerOwnershipPointerPresent (0.06s)

$ grep -E '^\s*--- FAIL' /home/user/workspace/peak4-logs/mrg.log | sed 's/^[[:space:]]*//'
--- FAIL: TestAgencyReplay_LiveLedgerIdempotent (0.00s)
--- FAIL: TestMemoryFeedbackSessionDedup (8.36s)
--- FAIL: TestMemoryFeedbackSuggestCold (34.20s)
--- FAIL: TestStyleGate (19.99s)
--- FAIL: TestSuiteTempRootIsPinned (0.00s)
--- FAIL: TestTask_BarePrompt_SIGTERM_WritesSessionEnd (6.32s)
--- FAIL: TestWorkerOwnershipPointerPresent (0.06s)
```

Every difference between the two lists is the parenthesised wall-clock (e.g.
`8.81s` vs `8.36s`) and the per-package seconds in the tally — i.e. the failure
*set* is empty-diff, and the residual is host timing only. The **branch-only
delta is ZERO**, as the identical non-`.md` tree digest already guaranteed; the
sweep is the empirical corroboration, and it is the epic's acceptance check
("FAIL-set diff EMPTY"). The 7 reds are master's own and are deliberately **not**
fixed here (§Not Done).

## Not Done

- **No re-land of the peak-hours feature.** Master already carries it; a
  revert-of-revert was explicitly out of scope for the original fix branch and
  remains so. What is attested here is the *fix* path, not a feature landing.
- **No new code commit** — see §4.
- **Master's own pre-existing reds are untouched** (delta, not global green):
  the environment-injected `cmd/agent` / `internal/agent` / `internal/testutil`
  failures are expected in **both** trees and therefore contribute nothing to
  the delta. They are *not* fixed here.
- **The earlier cycle's original report is not on master**:
  `devdoc/fix-peak-hour-build-gaps.md` (blob of `8af9c6f2`) was deleted by the
  revert `9cbeb9e5` and never returned; this report supersedes it and is the
  standing record.
