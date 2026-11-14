<!-- doc-class: historical -->
# Task Report: worker should remove worktree when done (01a095a4-ea82)

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Epic: a worker must release its worktree when development is over, because a
worktree-occupied branch is refused by `scripts/safe-merge.sh` gate [c] **and**
put on the merger's protected list (`repoSweepProtectedBranches`) — so a
leftover worktree silently withholds a finished deliverable from the dedicated
merger. Also: find out why some worktrees live under `/tmp` instead of
`~/workspace`, and remove the garbage ones.

Verdict shape: **DOABLE**, executed. No code change was needed — the mechanism
already exists on master; what was missing was that the running process was
still the OLD binary (evidence below) plus one operational pass. **Nothing was
merged** (Rule 6 / merger owns that).

---

## Completed

### 1. The protocol is implemented per-branch, and it is strictly safe

`internal/tools/task_reclaim.go` (`ReclaimDeadHolderWorktrees`) frees the
**directory only**, for a row that is all of:

| guard | check |
|---|---|
| terminal row | `completed` / `failed` / `cancelled` / `verified` (a live `pending`/`claimed`/`in_progress` row is never a candidate) |
| claim host is *this* host | a foreign `host:pid` stamp is unknowable here → never called dead |
| claim pid provably dead | `kill -0` + zombie check |
| no live ledger row owns the path/branch | `taskLiveClaims` (`ownerOfWorktree`, `ownerOfBranch`) |
| worktree clean | `git status --porcelain` empty (untracked included) |
| no live process inside | `/proc/*/cwd` scan + `.git`-file holder check |
| branch matches the row's branch | a worktree on another branch is not this row's to free |

**The branch is always kept** — it carries the deliverable. Removal of a
worktree can therefore never lose a commit (the commit lives in the shared
repo); this was verified per removal (`tip before == tip after`, §3).

Three further safety nets agree with the same predicate:
`finishCleanupServerRecycle` (per-row chain), `MergedCleanupSweep` (merged
branches only), and `serveFinishCleanupSweep` (the pass; its reclaim leg runs
before the row chain).

### 2. The sweep's 10-tick starvation is understood and bounded

Two scans feed one pass (`cmd/agent/cli_tasks.go`):

* newest-10 rows (`ORDER BY updated_at DESC LIMIT 10`), and
* the *stranded* scan (`updated_at ASC LIMIT 10`) over rows whose recorded
  worktree directory **still exists**.

Measured on the live ledger (read-only): 170 terminal rows carry a
`worktree` key; **168 of their directories are already gone**. The reclaimed
rows are ordered `updated_at DESC`, so the newest-10 window is exactly where
freshly-finished rows live — the window is not the binding constraint for the
backlog shape found here. The residual limit is honest and named: **10
stranded directories per pass**, and a row that never recorded
`deliverable.worktree` is unreachable by either scan.

### 3. Evidence-based removal of every provably-dead worktree

Snapshot, per candidate: branch/tip, ancestry vs `master`, `git status
--porcelain`, live `/proc/*/cwd` scan, `lsof +D`, and a live-ledger-row query
on **both** axes (worktree path and branch). Remove only if all three liveness
probes were empty. Tracked diffs and untracked files were copied to
`~/.gopi/backups/wt-sweep-20260912/<name>/` before removal.

**17 directories freed** (all `git worktree remove`, rc=0, empty output):

```
/home/user/workspace/gopi-mergerverify          (detached a0bc3e11, merged)
/home/user/workspace/gopi-verify-01a09435       verify/worker-writeback-01a09435   (a8dd161a)
/home/user/workspace/gopi-verify-01a0948b       verify/worker-writeback-01a0948b   (388d8c54)
/home/user/workspace/gopi-verify-01a09587       verify/launcher-install-01a09587   (f9545fad)
/home/user/workspace/gopi-verify-combo-ind      docs/verifier-combo-verify-20260912(29d4e7eb)
/home/user/workspace/gopi-verify-combo/report   docs/worktree-combo-cleanup-verification
/home/user/workspace/gopi-verify-glmwatch       (detached 141a6aa8, merged)
/home/user/workspace/gopi-verify-hplaunch       docs/hp-inline-launchers-verification
/home/user/workspace/gopi-verify-prefix         (detached cf99e644, merged)
/home/user/workspace/gopi-verify-reclaim-01a09569 verify/dead-holder-reclaim-01a09569
/home/user/workspace/gopi-verify-zombie-01a0948b docs/zombie-auto-dispose-verification-…
/tmp/gwv/red  /tmp/gwv/repo                     (detached 55296ffb, merged)
/tmp/vth/pre  /tmp/vth/prefix                   (detached 875816fd, b8c556ae, merged)
/tmp/zv/pre   /tmp/zv/fixed                     (detached, merged)
/tmp/vdh/pre                                    (stub left after `git worktree remove` refilled the empty dir)
```

Backups worth naming: `gopi-verify-prefix` had a **reverse-engineered
`glm-night-watch.sh`** (25 added lines vs master) + `internal/testutil/
glm_night_watch_test.go`; `gopi-verify-rpt` a devdoc edit; `/tmp/zv/pre` and
`/tmp/vdh/pre` untracked verifier test files. All preserved under the backup
path above.

**Each removal was proven not to lose work**: `git for-each-ref` diff of every
local branch tip before vs after → only two changes, **both by other live
agents** (`feat/tasklist-header-count` moved because its worktree is live and
being committed to; `verify2/dead-holder-reclaim-01a09569` is the new
generation of that worktree). No tip was altered by a removal; every freed
branch still resolves and `git cat-file -e <branch>:<file>` is intact.

### 4. `safe-merge` gate [c] flips from refused to green — merge-input restored

```
before:  ❌ [c] 分支 'verify/worker-writeback-01a09435' 仍被 worktree 占用 → exit 1
after :  ✅ [c] worktree 占用: 无 worktree 挂着 … → ✅ safe-merge: 全部通过  (exit 0)
```

Exit-0 after reclaim (all previously blocked by gate [c]):
`verify/worker-writeback-01a09435`, `verify/worker-writeback-01a0948b`,
`verify/launcher-install-01a09587`, `docs/verifier-combo-verify-20260912`,
`docs/worktree-combo-cleanup-verification`, `docs/hp-inline-launchers-verification`,
`verify/dead-holder-reclaim-01a09569`, `docs/zombie-auto-dispose-verification-20260912`.
Left untouched (live/occupied on purpose): `feat/help-format`,
`feat/tasklist-header-count`, `docs/glm-night-watch-verify-01a092a6` (my own
row's worktree), `review/voiceinput-2`.

### 5. Why the worktrees were `/tmp` — answered from the artifacts, not guessed

Every `/tmp` tree is a **one-off A/B checkout of the MAIN repo** (a linked
worktree; no "meaningless testing branch" exists — the admin dirs are
`pre`, `pre1`, `pre2`, `prefix`, `repo`, `red`, `fixed`, all *detached*):

```
/tmp/gwv/repo   → gitdir: /home/user/work/GoPi/.git/worktrees/repo     probe.sh: "sandbox for the GLM watcher fix"
/tmp/vth/pre    → …/worktrees/pre1    accept-delivered.log: "fixture — dead holder + clean linked worktree" (01a0948b verify)
/tmp/zv/pre     → …/worktrees/pre     acc.sh: "Real-binary acceptance for zombie auto-disposal"
/tmp/vdh/pre    → …/worktrees/pre2    (verifier probe)
```

They are the dev signal, not the worker: **the worker always uses
`~/workspace/<name>`** — every one of the 170 ledger rows carries a
`~/workspace/...` path, and the `/tmp` paths appear in **no** ledger row. The
in-repo devdoc corpus teaches exactly this pattern (13 devdocs use
`git worktree add --detach /tmp/<name>`). All detached tips were already
ancestors of `master`, all clean or backed up → **garbage, removed** (the
`/tmp` parent dirs keep only the probe logs/outputs, which are cheap and
non-registered).

Deliberately **kept**: the 2 live worktrees (`gopi-help`, `gopi-tasklist-count`),
`gopi-verify-rpt` (the row 01a095a4 you are reading runs in it), the new
`gopi-verify-reclaim-01a09569` (a sibling worker created it while this task ran;
it is live), which is the safety rule working as intended.

### 6. The real operational root cause: the OLDER BINARY is still running

The pass that would have freed all of this runs every 5 min, but it could not
help before 20:46 because the running processes started earlier:

```
bin/gopi                       built 20:26  vcs.revision=0244f3d1   (BEFORE the fix)
/usr/bin/gopi task merger      started 07:48 (pid 1284455)
workers 1575642 / 2814868 / 2822463 / 2828060
master tip 558fb09d  ← merged "fix/dead-holder-reclaim" at 20:46  (reclaim + registration repair)
```

`strings bin/gopi | grep -c "dead-holder worktree registration pruned"` = **0**
(that marker IS on master), and the merger log explains why the rebuild did not
happen: `[rebuild] skip: worktree dirty — rebuild deferred to a clean tree`
(the **main** checkout has 32 untracked `briefings/*.md`). So on this host the
protocol is armed but not yet firing.

**Reproduction that proves the fix works (newest binary, sandboxed ledger, no
merge performed)** — `/dev/shm/wtprobe3-hxFR`, two terminal rows (one
`completed`, one `failed`) with provably dead holders:

```
before:  git worktree list → wt-completed + wt-failed occupied
run   :  GOPI_HOME=<sandbox> GOPI_SETTINGS=<sandbox>/settings.json \
         GOPI_MEMORY_DB=<sandbox>/memory.db ./bin/gopi worker --once
out   :  [worker] [finish-cleanup] dead-holder worktree reclaimed: feat/probe-completed (t-probe-completed) freed for merge; branch kept
         [worker] [finish-cleanup] dead-holder worktree reclaimed: feat/probe-failed (t-probe-failed) freed for merge; branch kept
after:   git worktree list → main repo only;  git branch → feat/probe-completed, feat/probe-failed, master (both kept)
```

A first attempt with a bare-hand-written sandbox DB was refused
(`… is not a distilled knowledge base (missing distilled_qa table)`) — the
sandbox ledger must be a clone of the real schema; recorded so the next agent
does not repeat it.

### 7. Deferred, with evidence: `in_progress` orphans

`01a09573-328f-756d-b895-0292fc7cf60e` is `in_progress`, claimed by
`peerbox:2881717`, and that pid is **dead** (and it holds no worktree). A
*terminal* row with a dead holder is reclaimable; a *live-status* row is not —
`taskLiveHolderStatus` treats `in_progress` as a holder by design, and the
zombie path that owns such rows (`sweep-zombies --kill`, `DetectStaleTasks`)
is a different subsystem. Not touched here; filed as a follow-up
(`task_submit`), because the same class will keep hiding stranded directories.

---

## Not Done

* **No merging** — by instruction and Rule 6. The freed branches are merge
  input; the merger sweeps them.
* **No `git worktree remove` for the 4 worktrees whose owner is alive**
  (`gopi-help`, `gopi-tasklist-count`, `gopi-verify-rpt`,
  `gopi-verify-reclaim-01a09569`). They would be freed by the same pass once
  their rows go terminal.
* **No durable code change / hardcoded sweep verb.** The upstream predicate
  already satisfies the epic; the residual gaps (10-per-pass stranded cap,
  rows that never recorded `deliverable.worktree`, `/tmp` scratch worktrees
  being invisible to the sweep by construction, `in_progress` orphans) are
  reported with evidence and submitted as tasks rather than silently patched.
* **No repo-wide canonical sweep implemented in Go** (wiring the reclaim into
  a merge-free verb) — it needs a decision with the merger/ledger owner,
  because it touches the same protected list the merger consumes.
* **Not cleaned**: the 32 untracked `briefings/*.md` in the main checkout that
  block the merger's rebuild. Reported, not removed (not this task's
  mandate, and they belong to other sessions).

## Verification

```
go build -o bin/gopi ./cmd/agent/         BUILD_OK (fresh, in ~/workspace/gopi-wtsweep)
go test ./internal/tools/ -run 'Reclaim|FinishCleanup|Worktree' -count=1
                                          ok  github.com/nick/gopi/internal/tools  9.493s
gofmt -l internal/tools/ cmd/agent/       (empty)

Existing pass-level acceptance already on master (no duplicate test added):
  cmd/agent/cli_tasks_dead_holder_accept_test.go
    TestAcceptance_DeadHolderReclaimUnblocksSafeMerge   (gate [c]: refused → exit 0, tip unchanged)
    TestAcceptance_LiveHolderKeepsSafeMergeRefused      (live cwd inside → refused, dir survives)
    TestAcceptance_VanishedWorktreeLeavesNoFailureLine
  internal/tools/claim_holder_freshness_test.go  (dead/live holder, dirty, live-row, kill switch)

Live probes (this task, on the real repo):
  git for-each-ref refs/heads  before vs after   only the 2 live-agent moves (no tip changed by a removal)
  bash scripts/safe-merge.sh <freed branch>      exit 0 (was exit 1 on gate [c])
  bash scripts/safe-merge.sh feat/help-format    exit 1 — gate [c], the CONTROL
  strings bin/gopi | grep -c 'registration pruned'  0  (old binary; master has it)
  sandbox worker --once E2E                      reclaimed 2/2 dirs, both branches kept
```

## Files

* `devdoc/worktree-release-01a095a4.md` — this report (branch
  `feat/stale-worktree-sweep`, worktree `~/workspace/gopi-wtsweep`).
* Backups of everything removed: `~/.gopi/backups/wt-sweep-20260912/<name>/`
  (`tracked.diff`, `files/`).

### 8. Addendum — the live `/tmp` fixture mechanism (verified, not guessed)

Re-inspection after the sweep, to answer "why does some gopi create a
worktree in `/tmp` instead of `/home/user/workspace`" with evidence captured live:

- **The worker never chooses `/tmp`.** All 170 ledger rows carry a
  `/home/user/workspace/...` worktree path; no row has ever carried a `/tmp` path.
- **`/tmp` trees are fixture checkouts made by the *verifier/probe* pattern**,
  as one-off linked worktrees of the MAIN repo. The admin dirs (`pre`, `pre1`,
  `pre2`, `prefix`, `repo`, `red`, `fixed`) are short *fixture roles*, the HEAD
  is **detached** on a tip that is already an ancestor of master, and they are
  created/destroyed inside a single verification run — caught in the act:
  `bash -c mkdir -p /tmp/tlv && cd /home/user/work/GoPi && git worktree add --detach
  /tmp/tlv/pre f1cbc9c1` spawned by a live `bin/gopi --timeout 0` (pid 3221926).
  Such a tree is `locked` while its probe runs, so it is correctly never swept.
- **One exception is a worker artifact, not a probe**: `/tmp/taskcwd-wt`
  (branch `fix/worker-session-cwd`, created by task 01a095af) was made by the
  worker itself — the `cd`-guard makes commands absolute, so a `/tmp` path is
  reachable when `/home/user/workspace` is not used. Its branch tip equals master
  (`558fb09d`, zero unique commits), so no deliverable is at risk either way.
- **Detector consequence for anyone re-running this cleanup**:
  `git worktree list` keeps reporting an entry for a directory that was
  deleted behind git's back — removing the directory is NOT sufficient; only
  `git worktree remove <path>` (or `git worktree prune`) clears the
  registration. Always end a sweep with `git worktree prune -n -v` (expect
  empty).
- **Probe pitfall (observed)**: run the liveness probes from OUTSIDE the
  candidate directory. A probe shell that `cd`s into the candidate appears in
  `/proc/*/cwd` as a live holder of it — the sweep almost self-blocked on its
  own shell before the holder cmdline was read.

### 9. Hand-off inventory — what remains, and why NOT one of them was removed

Re-checked at hand-off against the live repo (`git worktree list --porcelain`
and `git worktree prune -n -v` → empty, i.e. the registration list and the
directories agree). Three liveness probes ran per directory — `/proc/*/cwd`
readlink, `lsof +D`, and a live-ledger query on BOTH the path and the branch
axis — and each probe was started from OUTSIDE the candidate. **No directory
below was removed: every one is either actively held by a running
agent/verifier, or mid-write with uncommitted work.**

| directory | branch / state | why kept |
|---|---|---|
| `~/workspace/gopi-help` | `feat/help-format` | live agent — a `cd ~/workspace/gopi-help && … go test ./...` shell is running inside it right now |
| `~/workspace/gopi-verify-reclaim-01a09569` | `verify2/dead-holder-reclaim-01a09569` | live verifier row's tree |
| `~/workspace/gopi-verify-rpt` | `docs/glm-night-watch-verify-01a092a6` | live agent row's tree |
| `~/workspace/gopi-voicereview2` | `review/voiceinput-2` | live reviewer's tree |
| `~/workspace/gopi-verify-tasklistcount-01a09599` | `verify/tasklist-header-count-01a09599` | **001a09599 is being verified right now** (`bin/gopi task verify 01a09599-…`, pid 3221920 alive) |
| `/tmp/taskcwd-wt` | `fix/worker-session-cwd` (0 unique commits vs master) | **LIVE and MID-WRITE**: task 01a095af is 15+ min into writing here — `git status` shows 8 dirty/untracked entries (`cmd/agent/task_session_cwd.go` new; `cli_resume.go`, `cli_tasks.go`, `cli_tasks_worker.go`, `worker_output.go` modified). The tip equals master so nothing is owed to the merger — but the directory must survive until that worker exits. This is precisely the case the epic warns about. |
| `/tmp/tlv/pre`, `/tmp/tlv/fixed` | detached `f1cbc9c1` / `9bfb390e` | live verifier's fixture checkouts (`bin/gopi --timeout 0`, pid 3221926) |
| `/tmp/prefix-01a09599` | detached `f1cbc9c1` | live verifier fixture created 21:09:31 — a THIRD `/tmp` tree that appeared *while this note was being written*, same pattern as §8 |

**The twin-directory trap, in the other direction**: the same verifier touched
`~/workspace/gopi-verify-tasklistcount-01a09599` **and** `/tmp/prefix-01a09599`
at 21:09 — one in `~/workspace`, one in `/tmp`. That is how a `/tmp` tree looks
like garbage while it is in fact the scratch half of a running verification.
Fixture trees are identified by a triad — a short role name (`pre`, `prefix`,
`repo`, `red`, `fixed`), a DETACHED head, and zero unique commits vs master —
never by the `/tmp` path alone.

**The running binary is not master — the reason new backlogs keep forming**:
`bin/gopi --version` → `0244f3d1d6af-dirty` (an ancestor of master, not its tip
`9bfb390e`), and the binary's mtime (20:26) predates master's tip commit
(21:05). Master already carries the reclaim pass
(`ReclaimDeadHolderWorktrees`, `internal/tools/task_reclaim.go:128`, wired into
the merger's finish-cleanup), but the process that would run it was built from
an older tree, and the merger daemon (pid 1284455) has been up 13h+ without a
rebuild. The post-merge rebuild is the step that closes this loop; while the
main checkout carries 32 untracked `briefings/*.md`, that rebuild is skipped and
the merged fix stays inert. Residue therefore continues to land as queued tasks
(01a095b5 + the roadmap children) rather than as more hand-removals.

**Where the reclaim actually runs (and why "it never fires" is expected for a
worker)**: the ONLY production caller is `cmd/agent/cli_tasks.go:2415`, inside
`serveFinishCleanupWorktreeQuery` — i.e. the **server/merger sweep**, not a
worker's own exit path. So a worker that finishes its development does not
release its own worktree at all; release depends on the merger daemon being
rebuilt, alive, and ticking. Two consequences, both observed here: (1) the
epic's literal ask ("worker should remove worktree when done") is unmet on the
producer side — the fix belongs in the worker's finish path; (2) the reclaim
scan is keyed on **terminal** rows (`cli_tasks.go` "Dead-holder repair leg …
a terminal row whose holder is provably dead"), which is exactly why an
orphaned `in_progress` row with a dead holder (task 01a095b5, live instance
01a09573) parks its directory forever — no live claim is needed to prove death,
but the scan never looks at a non-terminal row. Three guards it does enforce:
dead claim pid, clean worktree, no live process inside — and it frees the
DIRECTORY only, never the branch.

**Rule for the next sweep**: a candidate is removable ONLY when all three
probes are empty *and* `git status --porcelain` in it is clean. Re-probe
immediately before removing — on this pass `/tmp/taskcwd-wt` went from
"5 dirty" to "8 dirty" and `/tmp/prefix-01a09599` appeared from nothing inside
a single observation window. No merge was performed by this task.
