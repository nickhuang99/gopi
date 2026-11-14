<!-- doc-class: historical -->
# Task Report: one launcher owner — the GLM watcher delegates, a refused launch is loud

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

## Completed

- **Repo source of truth `scripts/glm-night-watch.sh`** (new): the GLM-window
  watcher is now a SCHEDULER that delegates. Its only remote act is
  `ssh <peer> "bin/gopi-worker-up.sh N"` — the single launcher, called once
  with the worker COUNT. No inline start remains anywhere.
  - `--dry-run` prints the delegation (peer, launcher, N) and launches nothing.
  - Child exit status is CHECKED. Non-zero is a FAILED launch: logged loudly,
    named, and the script's own exit status is non-zero. The refusal status is a
    named knob (`GOPI_GLM_REFUSAL_EXIT_CODE`, default 3) whose message names the
    agent-session guard, so "exit 3" and "the guard refused" cannot drift apart.
  - `--install` writes `<gopi-dir>/glm-night-watch.sh` byte-identical (verified
    with `cmp`) and probes the peer for the launcher, printing the exact install
    command when it is absent.
  - The window wait is bounded by `GOPI_GLM_WINDOW_MAX_SEC`; a target beyond the
    bound is abandoned and the delegation happens now (honest in production, and
    the seam that makes the delegation branch observable without sleeping).
  - **No agent-signature override is invented.** The guard's signals are
    env/process-chain based and do not traverse `ssh` (a remote sshd session
    carries no signature), so the operator path is intact without any cheat.
- **Installed copy updated** — `~/.gopi/glm-night-watch.sh` is the defect site;
  the fixed script is installed there and verified byte-identical to the repo.
  Backup of the pre-fix installed copy: `/home/user/workspace/.glm-prefix-backup.sh`
  (and `./.prefix-glm-night-watch.sh` in the worktree).
- **Regression checks** (`internal/testutil/glm_night_watch_test.go`, 5 tests):
  1. delegates to the single launcher; no inline start; no per-worker loop; has `--install`
  2. every env knob is declared and pinned to its Go-side value
  3. end-to-end: a refusing transport (exit 3) → FAILED line naming the refusal
     AND non-zero exit; an accepted launch (exit 0) is not reported as FAILED;
     `--dry-run` shows the delegation and launches nothing
  4. the operator path: N workers really start from an empty-environment shell
  5. the startup-log name is pinned to the launcher's own spelling
- **Constants** (Rule 1b/13/14): `internal/procname` gained `WorkerLoopStartLine`,
  `WorkerStartupLogName`, and the env names the watcher/launcher share;
  `cmd/agent/cli_tasks_worker.go` now prints `procname.WorkerLoopStartLine`
  instead of typing the literal.

## Not Done

- **HP has no launcher installed.** `~/bin/gopi-worker-up.sh` is absent on the
  peer, and HP's `~/work/GoPi` is stale/divergent (no `scripts/gopi-worker-up.sh`).
  Until someone installs it, every window ends in a FAILED delegation — now
  loudly, which is the point. HP also carries its own inline starters
  (`hp-worker-autostart.sh`, `hp-worker-nightsafe.sh`) that are the same defect
  class on the peer; they are out of this epic's scope (this epic fixes the
  watcher, not HP's other scripts) and were NOT touched.

  **[RETIRED 2026-09-26: both starters now REFUSE an automatic caller and the cron
  line is gone — do not re-install it, and add no replacement auto-launcher or
  escape hatch. See `devdoc/hp-worker-autostart-retired-20260926.md`.]**

- The `--install` probe reports the missing peer launcher but still exits 0
  (the local install did happen); the per-run FAILED line is the loud signal.

## Incident (my fault — recorded, not hidden)

While developing test 4, my first probe scanned `ps` for the argv shape
`bin/gopi worker` and SIGTERMed **every** match instead of only the workers it
started. It killed **5 live workers** on peerbox (pids 380024, 380057, 463445,
463556, 463612). No supervisor replaced them; `gopi-worker-up.sh --count`
reported 0 workers afterwards.

- **Class**: exactly the forbidden pattern-match kill (the 04:12 incident class).
- **Fix in the test**: the probe now touches NO process table. The launch is
  proven by the workers' OWN artifact — the launcher's startup log, which
  carries one `worker: loop start` banner per worker — read under the sandbox
  gopi dir. A second containment guard was added: the sandbox pins
  `GOPI_HOME` and `GOPI_WORKER_LOG_ROOT` so it can only ever see its own workers.
- **Recovery**: workers are restarted with `~/bin/gopi-worker-up.sh 2` (an
  operator action; I did not run it unattended).

## Verification

- Build: `go build ./...` → **OK**
- Tests: `go test ./internal/testutil/ -count=1` → **ok** (5 new gates; full
  package green, 36s)
- `bash scripts/glm-night-watch.sh --dry-run` → prints
  `remote command -> ssh hp 'bin/gopi-worker-up.sh 2'`, **exit 0, launches nothing**
- Refusal, fixed script (stub transport exit 3):
  `FAILED: the launcher REFUSED this launch on hp (exit 3) …` and **exit 3**
- Refusal, pre-fix script (same stub): logs `workers launched`, **exit 0** —
  the silent failure this epic closes
- **RED against the pre-fix script**: with `~/.gopi/glm-night-watch.sh` copied
  into `scripts/`, the gates fail on
  `starts workers ITSELF ("./bin/gopi worker")`, `has no --install seam`, and
  the missing knobs.
- Operator path (test 4): 2 workers announced their loop from an
  empty-environment shell — the operator path still starts N workers
- Guard untouched: `git status --short -- internal/config/` is **empty**
- Installed copy: `cmp` against the repo file → **byte-identical**

## Deliverable location

- Branch: `fix/glm-night-watch-delegates` (worktree `~/workspace/gopi-glmwatch`)
- Recovery: `git show fix/glm-night-watch-delegates:devdoc/glm-night-watch-single-launcher.md`
- The commit is shared through the worktree (same object store as the main repo).
