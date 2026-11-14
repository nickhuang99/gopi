<!-- doc-class: normative -->
# Task Report: HP inline worker launchers → one checked launcher

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic: `peer-inline-launchers` (2026-09-12). Worktree `~/workspace/gopi-hplaunch`,
branch `fix/peer-inline-launchers`. Host under test: HP (remote laptop, `ssh hp`).

## The defect

HP (the second worker host) bypassed the single-launcher contract in three ways:

1. **No installed launcher.** `~/bin/gopi-worker-up.sh` did not exist, and HP's
   checkout (`/home/user/work/GoPi` @ `0f66d17`, 2026-09-11) does not contain
   `scripts/gopi-worker-up.sh` — neither in its working tree nor in its git
   object store (`git cat-file -e master:scripts/gopi-worker-up.sh` → *not a
   valid object name*). HP therefore had **no path to the launcher source at
   all**: it had to be shipped, not assumed.
2. **Two unsupervised inline starters.** `~/bin/peer-worker-autostart.sh` (cron,
   `*/10 * * * *`) and `~/bin/peer-worker-nightsafe.sh` each ran
   `setsid nohup ./bin/gopi worker >> "$LOG" 2>&1 < /dev/null &` and then
   `echo "started pid $!"` — **never reading the child's status**. A launch
   refused by the agent-session guard (exit 3) was logged exactly like a
   success: a refusal wearing the face of a success. Each was also a *second*
   launcher (its own log contract, its own `pgrep -x` accounting, no seams).
3. **A third inline path the sweep found** (see §sweep, row 4).

## Completed

1. **Launcher shipped to HP through its own `--install` seam.**
   `scripts/gopi-worker-up.sh` → `--install` → `~/.local/bin/gopi-worker-up.sh`
   (16457 bytes, sha256 `4b2ad5a2…d128f`), with `~/bin/gopi-worker-up.sh` a
   symlink to it. **HP proved**: `~/bin/gopi-worker-up.sh --dry-run` prints real
   output (N, repo, output root, startup log, per-worker `--worker-log-root`
   argv) and exits 0; `--count` prints
   `live gopi workers: 0 (pids: none)`.
2. **Both starters rewritten to delegate** — repo copies live in
   `scripts/peer-worker/peer-launcher-{autostart,nightsafe}.sh` (they are the source of
   truth for the peer's `~/bin` copies), installed byte-identically via `scp` +
   `install -m 0755`:
   - `grep -vE '^\s*#' <script> | grep -c 'bin/gopi worker'` → **0** for both.
   - Zero `setsid`/`nohup`, zero per-worker loop in executable code; the count
     reaches the launcher in ONE call: `GOPI_WORKER_REPO="$REPO" "$LAUNCHER" "$N"`.
   - Liveness comes from the launcher's own `--count` seam (no second `pgrep`
     accounting), plus a pending-work gate.
   - The status is captured (`status=$?`) and compared; a non-zero status prints
     a `FAILED:` line that names the launcher, the status, the refusal knob
     (`$GOPI_WORKER_REFUSAL_EXIT_CODE=3`) and the sentence
     **"no worker was started"**, then exits with that status.
3. **A repo-side gate** (`internal/testutil/hp_worker_launchers_test.go`, 8
   tests): the shape rule (no inline start — reusing the *same* regexes as the
   GLM-watcher gate), the static loudness rule, the knob table (Rule 1b/13/14),
   the operator-ownership pointer, and an end-to-end loudness probe driven by a
   stub launcher (refusing / accepting / worker-already-alive), plus the
   `--dry-run` seam. Because the peer's scripts live outside the repo, an HP-side
   edit that re-inlines a start is caught by `go test`, not by reading a remote
   file.
4. **Third inline path fixed with a disposition** (§sweep row 4):
   `~/.gopi/hp-night-legacy-audit.sh` now reports a refused round loudly and
   stops the loop; gated by
   `TestHpNightLegacyAuditDrainerChecksEveryRoundStatus` and
   `TestHpNightLegacyAuditDrainerStopsLoudlyOnRefusal`.
5. **Sweep + dispositions applied on HP** (§sweep), reproducible through
   `scripts/peer-worker/{sweep-peer-launcher-paths,apply-peer-worker-sweep-dispositions,probe-peer-worker-loudness,peer-worker-checkout-staleness}.sh`.

## Verification

Build/test (branch `fix/peer-inline-launchers`, commit `4fade437`):

```
go build ./...                                        → OK
go test ./internal/testutil/ -run 'TestHpWorker|TestHpNight' -count=1
  --- PASS: TestHpWorkerLaunchersDelegateToTheSingleLauncher
  --- PASS: TestHpWorkerLaunchersReportRefusalLoudly
  --- PASS: TestHpWorkerLauncherKnobsAreDeclared
  --- PASS: TestHpWorkerLaunchersCarryTheWorkerOwnershipPointer
  --- PASS: TestHpWorkerLaunchersRefusedLaunchIsLoudNonZeroAndSilentOnSuccess
  --- PASS: TestHpNightLegacyAuditDrainerChecksEveryRoundStatus
  --- PASS: TestHpNightLegacyAuditDrainerStopsLoudlyOnRefusal
  --- PASS: TestHpWorkerLaunchersDryRunStartsNothing
  ok  github.com/the operator/gopi/internal/testutil
bash -n  scripts/peer-worker/*.sh scripts/gopi-worker-up.sh    → all OK (incl. on HP)
```

**Red-proof** (the gate fails on the pre-fix shape): restoring
`setsid nohup ./bin/gopi worker …` into the repo copy of the autostart script and
deleting the drainer's `status=$?` block turns the gate red —

```
--- FAIL: TestHpWorkerLaunchersDelegateToTheSingleLauncher
    … starts workers ITSELF ("./bin/gopi worker") … still detaches a process itself (setsid/nohup …) …
--- FAIL: TestHpNightLegacyAuditDrainerChecksEveryRoundStatus
    … does not capture the round's exit status (`status=$?` right after the round) …
```

then both are restored and the suite is green again.

**HP acceptance probes** (`scripts/peer-worker/probe-peer-worker-loudness.sh`, run on HP):

| probe | construction | result |
|---|---|---|
| 1 | launcher renamed away | autostart **rc=127**, nightsafe **rc=127**, both `FAILED: launcher exited 127 … — no worker was started`, failure recorded in the log |
| 2 | refusing stub launcher (guard exit 3) | both **rc=3**, `FAILED: launcher exited 3 (refusal $GOPI_WORKER_REFUSAL_EXIT_CODE=3) — no worker was started` |
| 3 | accepting stub launcher | both **rc=0**, `FAILED` line count **0** (the success path stays quiet) |
| 4 | drainer + refusing stub binary | **rc=3**, exactly **1** round started, `FAILED: round 1 — … exited 3 (refusal $GOPI_WORKER_REFUSAL_EXIT_CODE=3); no further round was started` |

The launcher was restored after each construction
(`~/bin/gopi-worker-up.sh → ~/.local/bin/gopi-worker-up.sh`, sha unchanged), and
every probe exited with **no live `gopi` process** and **zero task rows touched**
(`SELECT … WHERE updated_at > <probe start>` → empty).

## Sweep — every worker-start path on HP, with its disposition

Reproduce: `ssh hp 'bash -s' < scripts/peer-worker/sweep-peer-launcher-paths.sh`.

| # | path | disposition |
|---|---|---|
| 1 | `crontab -l` → `*/10 * * * * /home/user/bin/peer-worker-autostart.sh` | **THE only scheduled worker path.** Fixed: it now delegates (§Completed 2). Re-run verified end to end. |
  **[RETIRED 2026-09-26 — the schedule is gone and the script now REFUSES an automatic caller:** operator ruling 「这个 cronjob 必须终止，我们不能依赖这种系统来做，它掩盖了问题。」— the cron net MASKED a worker-death loop (1,214 delegations); `scripts/peer-worker/peer-worker-autostart.sh` + `peer-worker-nightsafe.sh` refuse any scheduler context. **Do not re-install it: no replacement auto-launcher, no escape hatch.** See devdoc/peer-worker-autostart-retired-20260926.md.]**
| 2 | `crontab -l` → `@reboot … doubao_browser_daemon.py`; `*/2 … evennia-health-probe.py`; commented-out smart_backup / glow-moon scheduler | **Not a worker path** — no gopi involvement. Left untouched (out of scope). |
| 3 | `~/.config/systemd/user/*` — `gopi-home-gc.{service,timer}` (ExecStart `~/.local/bin/gopi-home-gc.sh --apply`), `hermes-scheduler.service`, 3 evolution units | **Not a worker path.** `gopi-home-gc.sh` is the profile-hygiene sweeper (its own docstring: it *relocates/prunes* `worker-*.log`); `hermes-scheduler.service` is a symlink to `/dev/null` (inert). Left untouched. **No systemd unit starts a worker.** |
| 4 | `~/.gopi/hp-night-legacy-audit.sh` — ran `/usr/local/bin/gopi worker --once` in an 8-round loop, **no status check** | **Third inline path; FIXED.** Nothing schedules it (no cron entry, no unit, no shell-startup reference — it is a hand-run campaign tool), so the defect was not "unsupervised autostart" but the silent-success hole: a refused round logged like a success. Its `--once` round is a deliberate bounded drain, *not* launcher-shaped (no setsid/nohup, no second log contract, no accounting), so it keeps the round and gains the checked status. |
| 5 | `~/bin/*.bak-*` — 4 pre-fix copies of the two starters (`-ghome-hygiene`, `-peer-inline-launchers-20260912`), **each still executable in a PATH directory** | **De-executed, bytes kept as evidence** (`chmod a-x`): a loose executable copy of the deleted defect in `~/bin` is still a launcher-shaped path. |
| 6 | `~/.gopi/hp-night-legacy-audit.sh.bak-ghome-hygiene` (executable pre-fix copy, same status hole) | **De-executed**, same reasoning. |
| 7 | `~/.gopi-ship/gopi-worker-up.sh` — the launcher copy left by the ship step | **De-executed, bytes kept** as the ship evidence; the *installed* `~/.local/bin/gopi-worker-up.sh` is authoritative (same sha256 `4b2ad5a2…`). |
| 8 | `~/.gopi-ship/probe/*` — the epic's own stub launchers | **Removed** (regenerable test scaffolding; never a worker path). |
| 9 | `~/work/GoPi/scripts/*.sh`, `testdata/smoke.sh`, `devdoc/*.sh` (grep hits) | **No real start**: no uncommented `gopi worker` invocation in any of them (verified by the sweep's executable-line grep). Documentation/test fixtures only. |
| 10 | `~/.gopi/legacy-audit-batches/tmp/{decide.sh,mirrorlookup.sh}` (grep hits) | **No worker start** — `sqlite3` data tools for the audit campaign. |
| 11 | `~/.config/autostart/*.desktop`; `~/.bashrc`/`~/.profile`; `/etc/cron.{d,daily}`; `/etc/rc*.d`; `/etc/init.d`; `/etc/profile.d`; `atq`; system systemd units | **No gopi reference** in any of them (grep per source). |
| 12 | root crontab (`/var/spool/cron/crontabs`) | **UNVERIFIED — permission denied** (no sudo in this session). See §boundary. |

## HP checkout staleness — how the launcher got there

HP's checkout is **stale, and cannot be refreshed into usefulness here**:

```
/home/user/work/GoPi  0f66d17  2026-09-11  docs(night): overnight update …
/usr/local/bin/gopi -> /home/user/work/GoPi/bin/gopi   (20235296 bytes, Sep 11 04:15)
git cat-file -e master:scripts/gopi-worker-up.sh  →  NOT in the object store
.git/FETCH_HEAD                                   →  Sep 11 04:35
```

`internal/tools/worker_output.go` (2026-09-12) and
`internal/config/agentguard.go` (2026-09-12) are **not** in HP's HEAD, and
`strings bin/gopi | grep -c` returns **0** for `refusing to start a worker`,
`worker-log-root` and `GOPI_AGENT_SESSION`. So the launcher was shipped as a
**file through its own `--install` seam** (the seam exists exactly for this),
never assumed present, and never pushed into the stale checkout. The two starters
were shipped the same way (`scp` + `install`), and the drainer likewise.
Evidence recipe: `scripts/peer-worker/peer-worker-checkout-staleness.sh`.

## Not Done / honest boundary

- **The refusal path these scripts report cannot currently fire on HP.** HP's
  deployed binary predates the agent-session guard (`strings` → 0 for
  `refusing to start a worker`), so on HP `gopi worker` has no exit-3 refusal to
  report. The rewritten starters are nonetheless correct and *observable*: they
  report and propagate **whatever** status the launcher returns (proved with a
  refusing stub, probes 1–2), and the same scripts on a guarded binary report the
  real guard refusal. Closing the binary gap is a **deployment** task
  (rebuild/fetch on HP), not a script task; submitted as a bug (§defects).
- **The launcher's output-attribution flag is silently unused on HP.** The
  launcher passes `--worker-log-root <root>`, which HP's stale binary cannot
  parse. It does not fail — `gopi worker --totally-bogus-flag-xyz` was observed
  to start the loop anyway (rc=124, loop running) — so on HP a worker's output is
  **not** attributed to the task's session dir, the very defect class the
  launcher contract removes. The launcher's own contract holds; the stale binary
  silently ignores the flag. Submitted as a bug (§defects), with the
  evidence that unknown `worker` flags are accepted (no argv validation).
- **Root crontab unverified.** `/var/spool/cron/crontabs` is not readable without
  sudo and this session has none. A root-armed worker start on HP is therefore
  **not excluded** — only the user crontab and the system cron dirs are proven
  clean. If a root entry existed it would be outside `operator`-launcher control;
  the fix would be the same delegation.
- **What can still auto-start a worker on HP** (after this change): (a) the
  `*/10` cron entry → `peer-worker-autostart.sh` → the single launcher (checked,
  loud, operator-owned) — the intended path; (b) a hand-run of
  `peer-worker-nightsafe.sh` (nothing schedules it today) or of the legacy-audit
  drainer, both now status-checked; (c) any **new** file that a future agent or
  operator drops in `~/bin` or `~/.config/systemd/user/` — outside the repo, no
  gate can see it, so the repo-side gate only covers the copies it ships;
  (d) a **root**-armed cron/unit (unverifiable, §boundary above); (e) the
  explicit operator escapes (`GOPI_HUMAN_TERMINAL=1`, `GOPI_ALLOW_AGENT_SPAWN=1`)
  and the guarded `gopi worker` verb itself on a *guarded* binary.
- **Pre-existing reds, not mine.** `TestScriptsAreBoundedAndOffTmp` and
  `TestWorkerOwnershipPointerPresent` fail identically **on master in the main
  repo** (`cd ~/work/GoPi && go test ./internal/testutil/ -run '…' ` → same 3
  files / same doc list). They are outside this epic's diff
  (`git diff --stat master...HEAD` → only `scripts/peer-worker/*` + the new gate).
- **Probe incident (disclosed).** While writing probe 4 my `python3` replacement
  of the drainer's invocation line silently did **not** match (the file already
  contained the `$GOPI_WORKER_BIN` line at a different offset), so the probe ran
  the **real** `/usr/local/bin/gopi worker --once` once. Verified harmless: the
  run reported `queue drained — exiting`, **zero** task rows were written
  (`updated_at` window empty), no session dir was created, and no `gopi` process
  was left behind. The script was then fixed (`grep`-verified) and probe 4 re-run
  against a stub, rc=3, 1 round. Lesson: verify the edit landed
  (`grep -n`) before treating a probe as sandboxed.
- **Not touched, per constraints:** no `pkill`, no signal to any running worker
  or merger (only PIDs this probe created, none of them surviving); the peer1
  watcher fix `141a6aa8` untouched; failed task `01a092db-a698` not rerun. The
  stale `in_progress` row `01a08c4d` (claimed by HP PID 1818553, dead since
  2026-09-11) is a **pre-existing** claim-holder-freshness issue
  (`devdoc/claim-holder-freshness-and-dead-worktree-reclaim.md`) — reported, not
  "fixed" here.

## Defects submitted (tags `bug`, priority 1)

- **HP's deployed binary predates the agent-session guard and the worker
  output-attribution flag** — on HP the exit-3 refusal cannot occur and the
  launcher's `--worker-log-root` is silently ignored (unattributed worker
  output). Repro: `scripts/peer-worker/peer-worker-checkout-staleness.sh` + the probe in §Verification.
- **`gopi worker` accepts unknown flags and starts the loop anyway** — a launcher
  that passes an option the running binary does not know is *silently* degraded
  instead of refused (Rule 15). Repro: `gopi worker --totally-bogus-flag-xyz`
  (observed loop start, rc=124 on timeout; HP binary, queue drained).

## Commit

```
4fade437 fix(hp-launcher): both HP starters delegate to the single launcher, refusal is loud
```
(+ this report and the drainer fix in the follow-up commit on
`fix/peer-inline-launchers`.)

## Deliverable declaration (Rule 6d)

- **Branch**: `fix/peer-inline-launchers` (worktree `~/workspace/gopi-hplaunch`,
  shared with the main repo's object store — the commit is already in
  `~/work/GoPi`; no push needed).
- **Recovery**: `git show fix/peer-inline-launchers:devdoc/peer-inline-launchers-20260912.md`
  (scripts: `git show fix/peer-inline-launchers:scripts/peer-worker/peer-worker-autostart.sh`).
- **Peer-side truth**: HP `~/bin/peer-launcher-{autostart,nightsafe}.sh` +
  `~/.gopi/hp-night-legacy-audit.sh` + `~/.local/bin/gopi-worker-up.sh`
  (sha256 `4b2ad5a2…d128f`, symlinked from `~/bin/gopi-worker-up.sh`).
