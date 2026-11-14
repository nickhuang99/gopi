<!-- doc-class: historical -->
# Task Report: forward rework of `fix/launcher-install-refresh`

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

**Branch:** `fix/launcher-install-refresh` (recovered, then forward-fixed)
**Worktree:** `/home/user/workspace/gopi-launcher-refresh-rework` (Rule 6 linked worktree)
**Ledger row:** `01a0959e-9199-7c84-8eb0-89ae7f2f6a76`
**Recover the report:** `git show fix/launcher-install-refresh:devdoc/launcher-install-refresh-rework-20260913.md`

---

## 1. Branch recovery (the ref was ABSENT)

`git rev-parse -q --verify fix/launcher-install-refresh` → exit 1; `git branch -a --list '*launcher*'`
→ empty; `git reflog --all | grep -i launcher` → no surviving ref. Recovered from the merge
commit's **second parent** (Rule 6d — the ref, not the merge, is the deliverable):

```bash
# the merge the verifier judged, and its two parents
git cat-file -p bdde6e3c0842bbd08d11135d8a0031bcd9c05faa
#   tree     b5b1f64a…
#   parent 1 dc823e502f603ab257c94f06fd54c1cac774d853   (master at merge time)
#   parent 2 6a42b82d47a572d7d457c028aa1fab5cbf861955   (fix/launcher-install-refresh tip)
git branch fix/launcher-install-refresh 6a42b82d47a572d7d457c028aa1fab5cbf861955
git log --oneline -2 fix/launcher-install-refresh
#   6a42b82d fix(worker-up): audit the INSTALLED launcher copy (--check) + drift regression gates
#   0244f3d1 Merge branch 'feat/hp-refresh-master-20260912'
```

| what | value |
|---|---|
| merge commit | `bdde6e3c0842bbd08d11135d8a0031bcd9c05faa` "Merge branch 'fix/launcher-install-refresh'" |
| merge 1st parent (master) | `dc823e502f603ab257c94f06fd54c1cac774d853` |
| merge **2nd parent** (branch tip → the recovered ref) | `6a42b82d47a572d7d457c028aa1fab5cbf861955` |
| revert commit | `ecd2d272807f1390e29092db66a29d9c2767e06e` — `Revert "Merge branch 'fix/launcher-install-refresh'"` |
| reverting diff | 3 files, 1 insertion / 504 deletions (`devdoc/worker-up-installed-copy-drift-20260912.md` −128, `internal/testutil/worker_up_install_test.go` −319, `scripts/gopi-worker-up.sh` −58) |

Cardinality check — the **four** non-crown files the original fix touched are present in the
reverted diff, so recovery is faithful: `scripts/gopi-worker-up.sh`, the two fix-owned files, and
the report.

### Re-creating the merged state (re-apply, don't re-merge)

`git merge master` on the recovered branch is a pure fast-forward (`6a42b82d` is an ancestor of
master — `git merge-base --is-ancestor 6a42b82d master` exit 0), and yet the fix's content was
**absent** from master: the revert commit deleted it. So the fix was re-applied by reverting the
revert, not by re-merging:

```bash
git merge master --no-edit          # pure ff: the revert still stands in content
git revert -n ecd2d272807f1390e29092db66a29d9c2767e06e   # re-applies exactly the reverted diff
git commit -m "revert-reapply: restore the --check seam + install-drift gates on top of master"
```

The re-applied content is intact: `sha256 scripts/gopi-worker-up.sh = 98e3ead4d8b6c2b3…` (19138 B)
— identical to the verifier's measured repo-side digest (AC1's `98e3ead4d8b6c2b35102031bed73cd0a2bff5558d3a616d2d1cf47f30f481df7`).

---

## 2. The reproduced failure on the merged state

Reproduced on an **isolated HOME** (the host's real `~/.local/bin` is never touched by this
session), in the incident's own shape: installed copy = the **pre-fix** blob
(`8e852829:scripts/gopi-worker-up.sh`, sha256 `4b2ad5a2…`) while the repo copy carries the merged
fix (`98e3ead4…`).

```
=== [1] --check against a drifted installed copy (the STALE state) ===
STALE: ~/.local/bin/gopi-worker-up.sh is NOT this script's bytes
  installed sha256 4b2ad5a2…  (16457 B)
  repo      sha256 98e3ead4…  (19138 B)
exit=1
```

After the repair the same seam:

```
=== [3] --check again ===
identical: ~/.local/bin/gopi-worker-up.sh = 98e3ead4d8b6c2b3… (repo copy 98e3ead4d8b6c2b3…)
exit=0
```

This matches the verifier's AC1 measurement exactly — same two digests, same 16457/19138 byte
counts, same exit-1 STALE on the live host. AC2/AC3/AC4 passed at verification and were **not**
re-fixed here (their evidence stands in `tasks/01a09587-verify-report.md`; that task is TERMINAL
and was never re-run).

### Root cause (5-why, not "the install wasn't run")

1. The installed copy was the pre-fix bytes → the host had never run `--install` against master.
2. Why not? The install is a **manual operator step** the original fix's own "Not Done" section
   ordered ("After the merge, run `… --install`").
3. Why was it left manual? The fix's deliverable was an **audit** seam (`--check`) plus
   byte-identity gates — it made the drift **audible** but never **drove** the repair.
4. Why did no gate catch that? Every gate of the fix drives the **repo** script or an **isolated
   HOME**; none looks at the copy that actually starts workers
   (`~/.local/bin/<name>`, reached via the `~/bin/<name>` symlink).
5. **Root cause:** a merge changes the repo's script and *nothing in the merge path re-runs its
   `--install`*. The seam was measurable but undriven — so it could only ever report the very
   drift it existed to prevent.

**A git hook was considered and rejected as the vehicle.** A `post-merge` hook *does* fire on the
merger's dominant fast-forward path (verified: git 2.34.1, `git merge --ff-only` runs
`.git/hooks/post-merge` with `$1=0`), but hooks are per-clone `.git/hooks` files — untracked,
unversioned, requiring their own install. The live `.git/hooks/post-receive` was **4 months stale**
versus its tracked `scripts/git-hooks/post-receive`, i.e. exactly the untracked-host-artifact class
that produced this incident. The vehicle must be code on master.

---

## 3. The fix (original intent preserved)

The Rule-12 merge-round close loop — `serveRebuildAfterMerge`, the one hook that demonstrably runs
after every merge round — already rebuilt the binary. It gains the **second half**: it now also
re-installs every drifted installed script copy.

| file | change |
|---|---|
| `internal/tools/script_install_sync.go` (new) | `SyncInstalledScripts(repo)` — drift detection + repair |
| `internal/tools/script_install_sync_test.go` (new) | 7 engine gates |
| `cmd/agent/cli_tasks_rebuild.go` | close loop calls the refresh after the rebuild; `serveRebuild` split out to report a revert |
| `cmd/agent/cli_tasks_script_sync_test.go` (new) | 2 wiring gates |
| `cmd/agent/merger_verify_child.go` | refresh **before** the verify child judges (see §4) |
| `cmd/agent/merger_verify_script_sync_test.go` (new) | 2 ordering gates |

**Intent preserved — the seam is still the script's, this only decides WHEN.** The refresh invokes
each script's **own** `--install`; the script keeps every install semantic (the already-installed
guard, the `~/bin` symlink, glm-night-watch's peer note). Nothing is duplicated, nothing reimplemented.

**Scope, deliberately narrow and non-destructive** (a merge must not make operator decisions):

- refresh **only a script that already has an installed copy** — never install something the host
  chose not to install;
- invoke **only a script that declares `--install`** (Rule 21: calling a non-declaring script with
  it would be an unknown-flag error, i.e. a fabricated failure);
- a repo with no `scripts/` dir is a quiet no-op (docs-only / foreign repo) and no directory is created;
- failure is **logged, never fatal, never a rollback** — the merge is done and good;
- **trust nothing:** the child's exit 0 is re-verified by re-reading the installed bytes and
  asserting byte identity (an install that silently no-ops is the same silent-degradation class);
- skipped when the round **auto-reverted** — a revert restores the pre-sweep tip, so refreshing
  there would faithfully re-install the OLD bytes and invert the drift.

---

## 4. The ordering trap (a defect found in this fix's own first cut)

The close loop runs at the **end** of a merger pass, but a **gated** bugfixing branch is judged by
the verify child *inside* `mergerVerifyGate` (`runMergeVerifyChild`) — **before** that close loop.
Wired only into the close loop, the refresh arrives **after the judge**, so a re-land of exactly
this branch would fail identically. Caught by writing an ordering test, not by inspection.

The refresh is part of **landing** (master is merged and the working tree carries the new scripts),
so it now also runs immediately before the child is spawned. A FAILED verdict unwinds master to
`beforeTip`, and the post-gate `serveRebuildAfterMerge` then re-syncs from that restored tree — so
the revert path cannot leave an installed copy pointing at a merge that no longer exists.

**The ordering test asserts on WHAT THE CHILD SAW**, not on a post-hoc state check: the stub child
observes the installed file at spawn time and records it. A late refresh would satisfy any
"it matches now" assertion and must not pass.

---

## 5. Verification

### Build / static

| check | result |
|---|---|
| `CGO_ENABLED=0 go build ./...` | **exit 0** |
| `gofmt -l cmd internal` | **empty** |
| `go vet ./internal/tools/ ./cmd/agent/` | **clean** |

### The original fix's own targeted tests (acceptance criterion)

`go test ./internal/testutil/ -run TestWorkerUp -count=1 -v` → **PASS, 6/6**:

`TestWorkerUpCountsArgvShapeNotName`, `TestWorkerUpScriptNamesItsValues`,
`TestWorkerUpScriptHasNoPgrepOrPkill`, `TestWorkerUpInstallIsByteIdenticalToTheRepoScript`,
`TestWorkerUpCarriesNoRetiredLauncherForms`, `TestWorkerUpCheckReportsStaleInstalledCopy`
(`ok … 1.459s`).

The launcher install/refresh path is therefore green in its own right, exactly as the verifier found
(`ok … 0.245s`).

### New gates

- `go test ./internal/tools/ -run TestSyncInstalledScripts -count=1` → **7/7 PASS**
- `go test ./cmd/agent/ -run 'TestServeRebuildRefreshesDriftedInstalledScript|TestServeRebuildSkipsScriptSyncOnAutoRevert'` → **2/2 PASS**
- `go test ./cmd/agent/ -run 'TestMergeVerifyGateRefresh…'` → ordering gates **PASS**
  (`TestMergeVerifyGateRefreshesInstalledScriptBeforeTheJudge` **0.42s** with a prebuilt
  `GOPI_TEST_BIN`; it SKIPs — never fails — when the load-sensitive `go build` of the test's own
  binary exceeds its budget, so run it with `GOPI_TEST_BIN` set on a loaded host).
- `go test ./internal/testutil/ -run TestWorkerUp -count=1` → **6/6 PASS** (`ok … 1.522s`)
- `go test ./internal/standards/ -count=1` → **ok 23.9s**

The ordering gate's own output is the ordering evidence — the refresh line precedes the child's verdict:

```
[merger] [scripts] refreshed: gopi-worker-up.sh (installed copy re-installed from the repo)
Task updated: 01a09740-… (status=verified, reason=verification passed)
[merger] […] fix/sync: post-merge verify PASS: branch fix/sync kept on master (d63eafb1e83c → 344150ee2c66)
--- PASS: TestMergeVerifyGateRefreshesInstalledScriptBeforeTheJudge
```

**RED-proof (Rule 10.5).** Removing the close-loop wiring
(`serveScriptSync(repo)` → `_ = repo`) turns the drift gate **red**:

```
--- FAIL: TestServeRebuildRefreshesDriftedInstalledScript
    the merge round must refresh the drifted installed copy and log it, got:
        [merger] [rebuild] ok: …/repo built in 71.3ms (62862db)
```

Restoring it returns the tree green. The gate is not vacuous — it fails exactly when the wiring is gone.

### Full-suite differential vs un-merged master

Control worktree: `/dev/shm/glr-control` at master **`25c901d0`** (the tip the branch is merged
with). Both trees: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`, then
`go test -count=1 ./...` (~each on 4 cores under load average ≈25), compared by **failing-test
NAME** (`comm -13 control.failnames fix.failnames`), never by a single green/red number.

| | control (`25c901d0`, un-merged master) | fix branch (merged state) |
|---|---|---|
| failing **packages** | `cmd/agent`, `internal/stylegate`, `internal/testutil`, `internal/tools` | **the same four** |
| failing **test names** | 4 | 4 + 1 transient |
| `internal/standards` | `ok` 25.3s | **`ok` 22.7s** |

**FIX-ONLY failures (new regressions): NONE.** `comm -13 control.failnames.txt fix.failnames.txt`
returned exactly one name — `TestMergerRestart_RestartRunsTheSuccessorInOneCommand` — and it is
**master-owed, not a regression**: it reproduces *identically on the pure-master control tree*.

```
=== FIX tree ===
--- FAIL: TestMergerRestart_RestartRunsTheSuccessorInOneCommand (0.38s)
    --restart must succeed against a running merger, got 1:
      gopi merger: daemon start failed: another merge daemon is already running
      (lock …/.gopi/serve.lock held by pid 1218168)
=== CONTROL tree (pure master) ===
--- FAIL: TestMergerRestart_RestartRunsTheSuccessorInOneCommand (0.54s)
      gopi merger: daemon start failed: another merge daemon is already running
      (lock …/.gopi/serve.lock held by pid 1220214)
```

It is a merger singleton-`serve.lock` race in an e2e test — nondeterministic under a loaded 4-core
box, present with and without my change, and unrelated to scripts.

**CONTROL-ONLY failures: none** (`comm -23` empty) — nothing was masked.

Every one of the four failing packages fails on **both** trees, for reasons visible in the control
run itself:

- `internal/tools` — `panic: test timed out after 10m0s` on **both** trees (**600.176s** control /
  **600.150s** fix). A whole-package wall-clock timeout of the long suite under load, not a test
  failure: the `--- FAIL` lines inside it are teardown noise after the panic, and the fix's own
  `internal/tools` gates (below) pass standalone in **0.4s**.
- `internal/stylegate` — **global-count** gate, byte-identical on both trees: `197 test hit(s),
  ceiling 182` / `unchecked-close 1, ceiling 0` / `test-sleep 11, ceiling 0`. Proven by
  `go run ./internal/stylegate/cmd/stylegate -list` on each tree: **239 hits on both**, and
  `diff control.list fix.list` is **empty** — 100% master-owed, **zero** from this branch.
- `internal/testutil` — `TestScriptsAreBoundedAndOffTmp`, `TestWorkerOwnershipPointerPresent` fail on
  both trees (master-owed: the second is the worker-ownership pointer lane).
- `cmd/agent` — `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` fails on both trees.

**One pre-existing red was FIXED by this branch**: `internal/standards` passed here only after the
Rule-18 guard-clause rewrite in the last commit (`TestStandardGates` + `TestStandardGateDataReport`
were RED on the merged state with 2 stale permits and 3 new violations from the refactor; the rewrite
removed the violations and the now-dead permits, ratcheting the baseline **down**).

---

## 6. Mergeability (Rule 6d — declare before cleanup)

| check | command | result |
|---|---|---|
| master is an ancestor | `git merge-base --is-ancestor master fix/launcher-install-refresh` | **exit 0** (fast-forwardable, no force-push) |
| original tip preserved | `git merge-base --is-ancestor 6a42b82d fix/launcher-install-refresh` | **exit 0** (no history rewrite) |
| landing rehearsal | `git merge --no-ff fix/launcher-install-refresh` in a scratch worktree at `25c901d0` | rehearsed tree `478d96eb39a851b7df0fd400b6fab37c974f602e` == branch tip tree `478d96eb39a851b7df0fd400b6fab37c974f602e` — **deterministic, conflict-free** |

The rehearsal worktree was removed after the check; the branch was **not** merged by me.

### Branch history

```
fc1397f6 fix(testutil): reuse the package's ONE binDirName after merging master
c4d3ff91 Merge branch 'master' into fix/launcher-install-refresh
87293d20 docs(devdoc): the rework report — branch recovery, reproduced failure, root cause, fix, ordering trap, verification
c30c3ee5 style(standards): Rule 18 guard-clause rewrite + retire two now-stale baselines
97e3fd00 fix(merge-verify): refresh installed script copies BEFORE the verify child judges
e3b65966 Merge branch 'master' into fix/launcher-install-refresh
20cbc276 fix(merge-close-loop): refresh installed script copies after a merge round
3a8f6335 revert-reapply: restore the --check seam + install-drift gates on top of master
8380ce9c docs(devdoc): … (master at the time of the recovery)
```

The tip carrying **this report** is the commit that adds it; the commit before it is `97e3fd00`.
Naming a tip inside a report its own commit changes would be a self-referential off-by-one, so the
report is addressed by command instead: `git show fix/launcher-install-refresh:devdoc/launcher-install-refresh-rework-20260913.md`.

### The semantic merge conflict master introduced (a re-merge finding, not a rebase artifact)

Master kept moving while this branch was reworked (8380ce9c → 25c901d0 → **249d7860**), so the branch
was re-merged twice to stay fast-forwardable. The second re-merge produced a **file-clean merge that
did not compile**: master's glm-night-watch rework (`883640d0`, `3be9f5d4`) added

```go
// glm_night_watch_test.go
binDirName = "bin"   // the launcher's own <repo>/bin
```

while this branch's `worker_up_install_test.go` declared

```go
binDirName = "bin"   // the <HOME>/bin the install seam symlinks
```

Same package, **same value, different meaning** — git merged both declarations cleanly and the
package then failed to build (`binDirName redeclared in this block`), taking `TestWorkerUp` (the
acceptance tests) with it. Resolved per Rule 13/14 — master's landed definition wins, the branch's is
deleted and its comment records the reference. Both `bin` meanings are the same directory NAME, so
one constant serves both.

**This is the reason a re-merge must be followed by a build, not just by `git merge`'s exit code:**
`go vet ./internal/testutil/` caught it; the merge itself reported success.

---

## 7. Not Done (honest)

- **The live host's installed copy was NOT refreshed by this session.** Until this branch merges,
  `~/.local/bin/gopi-worker-up.sh` stays the pre-fix 16457-byte copy — deliberately. Installing an
  *unmerged* branch's launcher over the host's stable copy is the objection the original fix's own
  report and `devdoc/worker-count.md` both record. **After the merge** the close loop performs it
  automatically; to do it by hand now:
  `~/work/GoPi/scripts/gopi-worker-up.sh --install` then `~/bin/gopi-worker-up.sh --check` (must
  print `identical: …`). If the merger's close loop runs as designed, no manual step is needed at all
  — that is the point of this fix.
- **No real worker was launched.** The launch path was not exercised (the agent-spawn guard
  correctly refuses); `--check`/`--install`/`--dry-run` were used, as in the original verification.
- **The deeper live-host gap the original report filed is untouched**: no
  `~/.gopi/sessions/*/output.log` and no live worker carrying `GOPI_WORKER_LOG_ROOT` — a separate
  task, out of this scope.
- **`glm-night-watch.sh` and `gopi-home-gc.sh` also declare `--install`** and are therefore in the
  refresh's scope; `glm-night-watch.sh`'s install performs a peer **ssh** attempt (15 s connect
  timeout), which is why the per-child cap is 60 s. That is not exercised against a live peer here.
- The 20 legacy `worker-<ts>-<i>.log` files under `~/.gopi/` remain (untouched, as before).
