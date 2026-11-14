<!-- doc-class: historical -->
# Independent Verification Report: HP inline worker launchers → single operator launcher

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

**Ledger row**: `01a09436-7a57-74c9-829b-654465a6b0d0` (re-submit of failed `01a092db-a698`)
**Deliverable**: merge `e1e8cf21` (`Merge branch 'fix/hp-inline-launchers'`), fix commits
`4fade437` + `0ffc6296`, branch `fix/hp-inline-launchers`
**Verifier**: independent, read-only (no edits to the deliverable, no fixing, no signal to any
running worker/merger, no `pkill`).
**Verified at**: master tip == `e1e8cf21` (the merge IS the tip).
**Verdict**: **PASS** — recorded once in the ledger.

## 0. Deliverable located + ancestry

```
$ git log -1 --format='%H %P' e1e8cf21
e1e8cf21164da738688c8edced4a69ef051ade97  f7a20e95…  0ffc6296…      # 2nd parent = branch tip
$ git log --oneline e1e8cf21^1..e1e8cf21^2
0ffc6296 fix(hp-launcher): the sweep's third inline path stops loudly; ship the report
4fade437 fix(hp-launcher): both HP starters delegate to the single launcher, refusal is loud
$ git merge-base --is-ancestor <c> master   → ANCESTOR for BOTH fix commits
$ git rev-parse master e1e8cf21             → both e1e8cf21…  (merge is the tip)
$ git diff --numstat e1e8cf21^1 e1e8cf21   → 9 files, 1343 insertions, 0 deletions
```

Pre-fix tree = `e1e8cf21^1` (`f7a20e95`). `git ls-tree e1e8cf21^1 scripts/hp` → **empty**:
`scripts/hp/` does not exist before the fix (all 7 scripts are additions).

Delivered files: `devdoc/hp-inline-launchers-20260912.md`, `internal/testutil/hp_worker_launchers_test.go`
(569 lines, 9 tests), `scripts/hp/{hp-worker-autostart,hp-worker-nightsafe,hp-night-legacy-audit,
sweep-hp-worker-paths,apply-hp-sweep-dispositions,probe-hp-loudness,hp-checkout-staleness}.sh`.

## 1. Per-criterion table

| # | Criterion (epic "完成的定义") | Verdict | Evidence (verifier-reproduced) |
|---|---|---|---|
| 1 | Deliverable located: merge + fix commits + files | **PASS** | §0 — `e1e8cf21` is master's tip; both fix commits are ancestors; 9 files/1343 insertions |
| 2 | `go build ./...` clean; touched shell scripts `bash -n` | **PASS** | `CGO_ENABLED=0 go build ./...` in a detached worktree at `e1e8cf21` → exit 0. `bash -n scripts/hp/*.sh scripts/gopi-worker-up.sh` → OK (7 files) |
| 3 | The fix's own tests run green | **PASS** | `go test ./internal/testutil/ -run 'TestHpWorker\|TestHpNight' -count=1` → **ok** 0.498s: 9/9 tests PASS (delegation, loud refusal, knob table, ownership pointer, end-to-end loudness probe, 2× drainer, dry-run seam). `go test ./internal/standards/` → **ok** 5.647s |
| 4 | Red-proof: gate RED pre-fix / GREEN post-fix | **PASS** | §2 — two independent red-proofs, both directions reproduced |
| 5a | Launcher installed on HP, `--dry-run` works | **PASS** | HP: `~/bin/gopi-worker-up.sh` → `~/.local/bin/gopi-worker-up.sh` (16457 B, sha256 `4b2ad5a2…`); `--count` → `live gopi workers: 0 (pids: none)`, rc=0; `--dry-run` → N=2 repo=/home/user/work/GoPi + per-worker `--worker-log-root` argv, rc=0 |
| 5b | No inline `gopi worker` in the two scripts (grep proof) | **PASS** | `grep -vE '^\s*#' <f> \| grep -c 'bin/gopi worker'` → **0** repo-side (both) AND **0** on HP `~/bin/` (sha256 `9586448d…` / `03adc083…` == repo copies). No `setsid`/`nohup` in executable code |
| 5c | Loud failure on refusal (constructed failure proof) | **PASS** | §3 — verifier-authored stub-launcher probe, run BOTH locally and **on HP**: rc=3 + `FAILED: launcher exited 3 (refusal $GOPI_WORKER_REFUSAL_EXIT_CODE=3) — no worker was started`; missing launcher → rc=127, same shape; success path noisy-free (rc=0, 0 FAILED lines) |
| 5d | Scan of other auto-start paths, each with a disposition (devdoc) | **PASS (with gaps — §4)** | Re-run read-only on HP: `crontab -l` → **one** worker path (`*/10 … hp-worker-autostart.sh`, now delegating); all 6 user systemd units contain **no** `gopi worker`; `~/.config/autostart`/`.bashrc`/`.profile` **no gopi ref**; the `.bak-*` copies are `-rw-------` (de-executed); the drainer `~/.gopi/hp-night-legacy-audit.sh` (sha256 `3c53f64e…` == repo copy) now has `status=$?` + a FAILED-then-stop path. **Gap**: root crontab unreadable without
  **[RETIRED 2026-09-26 — the schedule is gone and the script now REFUSES an automatic caller:** operator ruling 「这个 cronjob 必须终止，我们不能依赖这种系统来做，它掩盖了问题。」— the cron net MASKED a worker-death loop (1,214 delegations); `scripts/hp/hp-worker-autostart.sh` + `hp-worker-nightsafe.sh` refuse any scheduler context. **Do not re-install it: no replacement auto-launcher, no escape hatch.** See devdoc/hp-worker-autostart-retired-20260926.md.]** sudo |
| 5e | Devdoc report exists | **PASS** | `devdoc/hp-inline-launchers-20260912.md`, 233 lines, in the merge; contains the sweep table, the boundaries, the defects submitted |
| 6 | Verdict recorded once + confirmed | **PASS** | `gopi task update 01a09436-… --status verified --result "evidence: …"` then `gopi task show` |

## 2. Red-proof (criterion 4) — the decisive step

The deliverable's gate (`internal/testutil/hp_worker_launchers_test.go`) was **absent** on the
pre-fix parent (the whole `scripts/hp/` directory is new), so "the suite is green on master" is
not evidence. Two throwaway detached worktrees: **pre** = `e1e8cf21^1` (`/tmp/hpv/pre`),
**fixed** = `e1e8cf21` (`/tmp/hpv/fixed`).

**(a) Frozen-test red-proof** — the fix's own 569-line gate file was copied verbatim into the
pre-fix tree:

```
cd /tmp/hpv/pre && go test ./internal/testutil/ -run 'TestHpWorker|TestHpNight' -count=1
--- FAIL: TestHpWorkerLaunchersDelegateToTheSingleLauncher/{autostart,nightsafe}
--- FAIL: TestHpWorkerLaunchersReportRefusalLoudly/{autostart,nightsafe}
--- FAIL: TestHpWorkerLauncherKnobsAreDeclared/…  --- FAIL: …CarryTheWorkerOwnershipPointer/…
--- FAIL: TestHpWorkerLaunchersRefusedLaunchIsLoudNonZeroAndSilentOnSuccess
        (a) missing launcher: output does not contain "FAILED"
        (b) refusing launcher: output does not contain "FAILED" — the refusal must be loud and named
--- FAIL: TestHpNightLegacyAuditDrainerChecksEveryRoundStatus
FAIL github.com/nick/gopi/internal/testutil
```
Same file at the fixed tree → **9/9 PASS** (§1 row 3).

**(b) The incident's own shape, end to end** — the pre-fix scripts were reconstructed in the pre
tree from the shape the devdoc quotes, and the gate fires with the incident's own vocabulary:

```
--- FAIL: TestHpWorkerLaunchersDelegateToTheSingleLauncher/{autostart,nightsafe}
  hp_worker_launchers_test.go:147: … starts workers ITSELF ("./bin/gopi worker") — that is the
    second, unsupervised launcher this epic removes …
  hp_worker_launchers_test.go:147: … starts workers ITSELF ("/gopi worker") …
  hp_worker_launchers_test.go:153: … still detaches a process itself (setsid/nohup in executable
    code) — detaching is the launcher's job …
```

**(c) Post-fix mutation red-proof (green→red in the tree that has the fix)** — re-inserting the
inline `setsid nohup ./bin/gopi worker` start into the repo copy of `hp-worker-autostart.sh`
turned the gate red with the same `:147` / `:153` messages; `git checkout --` restored it
(`git status --porcelain` → 0 files). Removing the drainer's `status=$?` block:
`does not capture the round's exit status (`status=$?` right after the round)` +
`the refusal line does not contain "FAILED"` → red; restored → green. **The gate is not a
tautology: it fails on the incident's shape and on a re-inlined post-fix tree.**

## 3. Loudness / delegation — verifier-authored behaviour probe

Stub launcher (nothing is ever started); real repo copy on the local tree and the installed copy
on HP. Local (`/tmp/hpv/probe/probe-loudness.sh`) and HP (identical semantics):

| case | autostart | nightsafe | launcher argv | stderr |
|---|---|---|---|---|
| accepting stub | rc=0 | rc=0 | `--count` then `1` | 0 FAILED lines (success is quiet) |
| refusing stub (exit 3) | **rc=3** | **rc=3** | `--count` then `1` | `FAILED: launcher exited 3 (refusal $GOPI_WORKER_REFUSAL_EXIT_CODE=3) — no worker was started` |
| launcher missing | **rc=127** | **rc=127** | — | `FAILED: launcher exited 127 … — no worker was started` (no inline fallback) |
| `--dry-run` | rc=0 | — | **0 stub calls** | prints the launcher call it would make; starts nothing |

The count reaches the launcher in **one** call (`GOPI_WORKER_REPO="$REPO" "$LAUNCHER" "$N"`),
liveness is the launcher's own `--count` seam (no second `pgrep` accounting), and the status is
captured (`status=$?`) then compared — the silent-success hole is closed.

**No side effects from the verifier**: HP live `gopi` processes = **0 before and after** every
probe; no worker was launched; the real `~/bin/gopi-worker-up.sh` was never renamed or invoked
with an intent to spawn (probes used a stub); no `pkill` was issued anywhere.

## 4. Honest gaps (NOT passes)

1. **Root crontab unverified.** `/var/spool/cron/crontabs/root` → *Permission denied* (no sudo).
   A root-armed worker start on HP is **not excluded**; only the user crontab and the system cron
   dirs are proven clean (`/etc/cron.d`, `/etc/cron.daily`, rc dirs, `/etc/init.d`,
   `/etc/profile.d`, `atq` per the delivered sweep).
2. **The refusal these scripts report cannot currently fire on HP.** HP's deployed binary is
   stale (pre-agent-session-guard), so `gopi worker` there has no exit-3 refusal; the loudness
   path was proven with a refusing **stub**, which is exactly the contract the scripts must
   honour. Closing the binary gap is a deployment task, not a script task (devdoc §Not Done).
3. **Unknown-flag acceptance on HP's stale binary** (`--worker-log-root` silently ignored) is a
   *reported* defect, not verified fixed here — it is out of this epic's diff.
4. **Host-level ownership of `~/bin` is ungated.** The repo-side gate audits the copies the epic
   ships; a *new* file dropped into HP `~/bin` or `~/.config/systemd/user/` is outside any repo
   gate (devdoc §boundary (c)). Verified true of the current state; not prevented by the fix.
5. **Peer-side state is host-observed, not repo-checked.** HP facts above come from live `ssh hp`
   reads: repo↔HP byte identity is sha256-confirmed for all three installed scripts, but a later
   HP-side edit would be caught by `go test` only for the shipped copies' content, not for the
   peer's file (devdoc acknowledges this design).
6. **Probe-4 incident disclosed by the worker** (a python substitution that silently did not
   match, so one real `gopi worker --once` round ran) — the worker reported it honestly; I did not
   reproduce it, and my own probes isolate the launcher with a stub.

## 5. Non-interference

- Deliverable untouched: `/tmp/hpv/fixed` `git status --porcelain` → clean after every mutation
  was reverted; the report worktree `/home/user/workspace/gopi-verify-hplaunch` (branch
  `docs/hp-inline-launchers-verification`) has only this file.
- No `pkill -f gopi` / `pkill -x gopi`, no signal to any worker/merger, no task row mutated other
  than the verdict (recorded exactly once).
- The old failed task `01a092db-a698` was not rerun; the peerbox watcher fix `141a6aa8` untouched.
- Pre-existing reds (not the fix's): in the **clean** pre-fix tree at `e1e8cf21^1` —
  `TestGlmNightWatchOperatorPathStartsWorkersUnattended`, `TestScriptsAreBoundedAndOffTmp`,
  `TestWorkerOwnershipPointerPresent` all FAIL. On master only the latter two are red;
  the GLM watcher one is **binary-dependent** (`bin/gopi` missing → probe exits 1) and turns
  **green once `bin/gopi` is built** (`go test … -run TestGlmNightWatchOperatorPathStartsWorkersUnattended`
  → ok 5.238s), so it is a de-skipped e2e, not a regression.

## 6. Commands (reproduce)

```bash
git worktree add --detach /tmp/hpv/pre   e1e8cf21^1     # pre-fix parent
git worktree add --detach /tmp/hpv/fixed e1e8cf21       # the fix
cd /tmp/hpv/fixed && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
bash -n scripts/hp/*.sh scripts/gopi-worker-up.sh
go test ./internal/testutil/ -run 'TestHpWorker|TestHpNight' -count=1 -v
cp /tmp/hpv/fixed/internal/testutil/hp_worker_launchers_test.go /tmp/hpv/pre/internal/testutil/ \
  && cd /tmp/hpv/pre && go test ./internal/testutil/ -run 'TestHpWorker|TestHpNight' -count=1   # RED
ssh hp 'bash -s' < /tmp/hpv/hp-probe.sh                                                          # HP read-only
```

## Commit

verification report only (branch `docs/hp-inline-launchers-verification`); the deliverable
`fix/hp-inline-launchers` @ `e1e8cf21` is unchanged.
