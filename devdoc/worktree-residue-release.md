<!-- doc-class: normative -->
# Task Report: why are there so many worktrees? — classification, root cause, released

Epic row: `01a09c25-3212-7d9d-9ad3-ee952e1c862b` ("why are so many worktree?")
Branch: `docs/worktree-residue-release` (worktree `/home/user/workspace/gopi-wt-release`)
Measured: 2026-09-14 03:0x–03:4x on peer1, repo `/home/user/work/GoPi`.

## Completed

### 1. Inventory + classification (53 registrations → 46 released → 7 kept)

Full input: `git worktree list` (53 entries incl. the main checkout). Every entry was
classified by: branch vs detached HEAD, unmerged-commit count `master..<tip>`, dirtiness
(`git status --porcelain`), a live-holder probe over `/proc/*/cwd` + `/proc/*/maps`, and a
ledger lookup (`tasks.deliverable` naming that path).

| class | count | examples | disposition |
|---|---|---|---|
| merge-build-gate scratch checkouts | 12 | `…/queue/queue-runtime-merge-build-<pid>/merge-build-gate-*/wt-*` | removed |
| suite / probe / baseline / verify fixtures (detached) | 15 | `gopi-baseline-399ea474`, `master-baseline-wt`, `peak-*`, `verify-01a09957/wt` | removed |
| orphaned worker deliverable worktrees | 9 | `gopi-accept-epilogue`, `gopi-fwd-br`, `gopi-daemon-ctx`, `gopi-probe-priv`, `gopi-doubao-asr`, `gopi-tmpdir-report`, … | removed |
| ad-hoc A/B control trees (detached) | 6 | `gopi-rawlog-control`, `gopi-fwd-ctl`, `gopi-verify-outtxt-pre` | removed |
| held by a live process | 3 | `gopi-conflict-flags`, `gopi-merger-stop-pid`, `gopi-rawlog-rework` | KEPT |
| git-`locked` scratch | 2 | `…/merge-build-gate-1034928654/wt-master` | KEPT |
| mid-write detached tree | 1 | `gopi-fwd-revert` (4 staged deletions) | KEPT |
| main checkout | 1 | `/home/user/work/GoPi` | KEPT |

Only **6 of 53** registrations had any ledger row naming them.

### 2. Root cause (four independent mechanisms, all still live)

**RC1 — the merge-time BUILD gate's scratch checkouts are never reclaimed when the
merger process dies mid-call.** `internal/tools/merge_build_gate.go` materialises each
revision under test as `git worktree add --detach <scratch>/<rev> <rev>` (queue class
`scratch.RuntimeClassMergeBuild`, i.e. `<scratch-home>/queue/queue-runtime-merge-build-<mergerpid>/…`)
and cleans up twice: a deferred `git worktree remove --force` (build-gate fn) and
`os.RemoveAll(workRoot)` (`mergeBuildGateScratch`). Two failure shapes survive both:

- SIGKILL of the merger between `add` and the deferred `remove` → **directory and
  registration both persist**;
- `os.RemoveAll` deletes the directory *behind git's back* — a removed-behind-git
  registration is only retired by `git worktree prune -v`, and the gate never prunes.

Evidence: 12 registrations named `queue-runtime-merge-build-<pid>`; **all 12 producer
pids are dead** (`kill -0` fails); 2 of them (`…3424324/…1034928654/wt-master`,
`…3424467/…3281513403/wt-master`) are *already directory-less* (`MISSING-DIR`) while their
parent queue dirs still hold `gocache/` + `gomodcache/`. Nine concurrent `gopi-merger merger`
processes (etimes 8380–8700 s, plus `--every 1h`) are running this path continuously — one
per branch per tick.

**RC2 — the `/dev/shm` location is the test harness's design, not a stray path.**
`scripts/setup-scratch-home.sh:49` `MEMORY_BASE="${GOPI_SCRATCH_MEMORY_BASE:-/dev/shm}"`
plus the measured symlink `~/workspace/gopi-test-scratch -> /dev/shm/gopi-test-scratch`
and `internal/scratch/scratch.go RootDirName = "gopi-test-scratch"`. So *every* suite /
probe / verify worktree that goes through the scratch home lands in tmpfs by construction.
Measured cost: `/dev/shm` = 7.8 G with 4.6 G used (59 %) before the release — meanwhile the
boot device `/` stands at 87 % (19 G free). Legal under Rule 22 (a different filesystem
from `/`), but RAM-only residue.

**RC3 — ad-hoc probe/fixture trees have no owner record at all.** For
`gopi-r3-wt`, `helpwb-2b6586e7`, `peak-base-32fb9da3`, `master-baseline-wt`,
`peakhours-exact-wt` the session transcripts (`~/.gopi/sessions/*.jsonl`) contain **no
creating command** — they were made by interactive/probe shells, so no ledger row, no
`deliverable.worktree`, no owner. **47 of 53 registrations were ledger-invisible.**

**RC4 — the two reclaim paths cannot see RC1/RC3 residue, and the one path that could
never runs unattended.**
`ReclaimDeadHolderWorktrees` / `ReclaimOwnFinishedWorktrees` are keyed on a **ledger row**
claiming the worktree (terminal status + dead claim pid); `internal/tools/worktree_sweep.go`
(the stale-registration sweeper, ranked guard #5 of the 2026-09-13 `/dev/shm` assessment) is
keyed on **age (24 h) + a declared worktree root** and is reachable **only** from the
operator verb `cmd/agent/cli_tasks.go:3020` — it has no daemon/merger caller. Measured
dry-run: 42 × "younger than the threshold (… < 24h0m0s)", 5 × "outside every declared root —
not swept; declare the repo's worktree_root to bring it into policy", 2 × locked.
So an unattended repertoire never runs, and a registration with no directory lives forever
unless someone runs prune.

**Why it recurs** (the "this happened before" in the briefing): the producers are recurring
(one build-gate checkout per branch per merge tick × ~9 merger processes; every suite run in
the scratch home) and the reclaim surface is (a) ledger-keyed, (b) age-keyed + operator-invoked.
Nothing throttles, staggers or revisits the registration itself.

### 3. "Is it related to voice input development or testing?" — No (with the exact link)

- `fix/voice-mic-probe-privileged` (worktree `gopi-probe-priv`) and `feat/doubao-asr-eval`
  (`gopi-doubao-asr`) were 2 of the 53 — ordinary deliverable worktrees, both branches
  **already fully in master** (0 unmerged commits). Neither is a `/dev/shm` tree.
- The `/dev/shm` habit is the **test/scratch harness** choice (RC2), not voice.
- The only *measured* LLM-driven `/dev/shm` worktree creation is a **Rule-11 tool-adoption
  probe**, not voice work: ledger rows `01a0999e` (completed) and `01a09990` (failed) ran
  `git worktree add /dev/shm/gopi-rule11-a -b feat/rule11-a` and
  `/dev/shm/gopi-rule11-probe` verbatim; both directories are already gone.
- Verdict: voice/doubao-asr work **contributed 2 entries, no mechanism**. The mechanisms are
  the merge-build gate (RC1), the `gopi-test-scratch → /dev/shm` symlink (RC2), ownerless
  probe trees (RC3) and a reclaim surface that cannot see either (RC4).

### 4. Release (worktree directories only — no branch was merged or deleted)

Protocol per path: live-holder re-probe immediately before removal (`/proc/*/cwd` + `exe` +
`fd` + `maps`) → dirtiness check → **plain `git worktree remove` (never `--force`)** →
one `git worktree prune -v`. Release log:
`~/.gopi/backups/wt-release-20260914-0306/release.log` (64 lines; 46 × REMOVED).

Projection before the write: `git worktree list --porcelain` → `worktree-list-before.txt`;
`git for-each-ref --format='%(refname) %(objectname)'` → `refs-before.txt`.
Backups: every dirty/untracked file of every candidate tree copied under
`~/.gopi/backups/wt-release-20260914-0306/<path-mangled>/` (plus a `.status` per tree);
the 119 MB `gopi-doubao-asr/out/` measurement tree was **moved** there (not deleted);
a 194 MB `scratch-82b3b6f3/.gc` Go build cache was dropped as regenerable; a probe
duplicate `agent_spawn_guard_env_isolation_test.go` was removed **only after**
`git show feat/guard-test-daemon-env-isolation:<file> | diff -q` proved it byte-identical.

Result: **53 → 7** registrations. Branches deliberately kept:

| kept | why |
|---|---|
| `gopi-conflict-flags` (`fix/control-conflict-full-flags`) | 6 live pids with cwd inside (4 × `gopi-merger merger`, the daemon) |
| `gopi-merger-stop-pid` (`fix/merger-stop-verify-target`) | live cwd of `gopi-merger merger --every 1h` (pid 1409929) |
| `gopi-rawlog-rework` (`fix/rawlog-test-home-leak`) | live cwd of a probe test binary + `Xvfb :204` |
| `…/merge-build-gate-1034928654/wt-master`, `…/3281513403/wt-master` | git `locked` — an explicit do-not-touch |
| `gopi-fwd-revert` (detached) | dirty: 4 staged deletions (mid-write) |
| `/home/user/work/GoPi` | the main checkout |

Branches whose worktree was released (all now mergeable input for the merger — **not merged here**):
`fix/peakhours-exact-boundary`, `docs/worker-acceptance-epilogue`,
`feat/guard-test-daemon-env-isolation`, `docs/tmpdir-merge-resolution-report`,
`verify/zombie-zd-confirmed`, `docs/signalrec-stale-bin-correction`,
`fix/daemon-context-isolation`, `fix/voice-mic-probe-privileged`, `feat/doubao-asr-eval`.
Each was verified development-complete: clean (all local work committed — nothing was in the
working tree) and/or 0 unmerged commits. Deliberately **not** removed: `.git/` admin state of
other repos, any branch ref, any `archive/*` tag.

### 5. Recommendations (NOT implemented here — backlog material, no code changed)

1. **PRUNE AFTER EVERY GATE CALL (RC1, smallest fix).** `os.RemoveAll(workRoot)` orphans the
   registration; either `git worktree remove` before the `RemoveAll`, or one
   `git worktree prune` after the gate. Today only an operator can retire them.
2. **Make the registration sweeper unattended** (RC4): call `WorktreeSweepRun` from the merger
   tick or the worker daemon with a conservative age; today it is operator-verb-only.
3. **Reclaim by REGISTRATION, not only by ledger row** (RC3/RC4): 47/53 had no row, so
   ledger-keyed reclaim can never see them.
4. **Declare `"worktree_root"` for `/home/user/work/GoPi`** in settings.json — the sweeper
   itself printed that advice for 5 entries ("outside every declared root — not swept").
5. **Cap/stagger the build-gate scratch under tmpfs** (RC2): 4.6 G of a 7.8 G tmpfs is RAM;
   a crashed merger must not leave the gate's caches + checkouts resident.

## Not Done

- **No code change** was made: this epic's deliverable is the classification + root cause +
  release. The five recommendations above are backlog items, not implemented here.
- **No merge** was performed (explicitly out of scope per the briefing) and **no branch was
  deleted**.
- The 3 holder-held worktrees stay until their holders exit; the 2 `locked` gate trees stay
  until their (dead) producer's lock is cleared.
- `~/Downloads/games/Maps` shows up as a second repo in the sweeper's inventory
  (`repos=2`); its own worktree state was not touched.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (exit 0).
- Tests: `go test ./internal/tools/ -run 'WorktreeSweep|Reclaim|ClaimHolder|FinishCleanup' -count=1`
  → `ok github.com/the operator/gopi/internal/tools 6.104s`;
  `go test ./internal/tools/ -run MergeBuildGate -count=1` → `ok … 6.653s` (the gates this
  epic diagnoses: the stale-registration sweeper, both reclaim legs, the finish-cleanup chain
  and the merge-time build gate).
- **No ref moved**: `diff refs-before.txt refs-after.txt` → empty ("REFS UNCHANGED"); the
  release removed directories only.
- **Prune clean**: `git worktree prune -n -v` → no output (no stale registrations).
- **safe-merge gate (the merger's own guard) for every freed branch** →
  `bash scripts/safe-merge.sh <branch>` exit 0 for `feat/guard-test-daemon-env-isolation`,
  `fix/peakhours-exact-boundary`, `fix/daemon-context-isolation`, `fix/voice-mic-probe-privileged`,
  `feat/doubao-asr-eval`, `docs/tmpdir-merge-resolution-report`, `docs/signalrec-stale-bin-correction`,
  `verify/zombie-zd-confirmed`, `docs/worker-acceptance-epilogue` — i.e. gate **[c] worktree
  occupancy is now clear**, which is exactly what the merger needs. Exit 1 for the three
  still-holder-held branches (`fix/rawlog-test-home-leak`, `fix/control-conflict-full-flags`,
  `fix/merger-stop-verify-target`) — expected, unchanged by this task.
- Before/after list + per-path release log + backups:
  `~/.gopi/backups/wt-release-20260914-0306/`.

## Files

- `devdoc/worktree-residue-release.md` (this report)
- Scripts used (kept outside the repo, re-runnable): `/home/user/wt-classify.sh`,
  `/home/user/wt-release.sh`, `/home/user/wt-release2.sh`, `/home/user/wt-release4.sh`

## Deliverable declaration

Branch `docs/worktree-residue-release` in `/home/user/work/GoPi` (worktree
`/home/user/workspace/gopi-wt-release`). Recovery: `git show docs/worktree-residue-release:devdoc/worktree-residue-release.md`.
The branch was created in a linked worktree, so the commit is already in the shared repo —
no push needed. **Not merged** (merger's job).
