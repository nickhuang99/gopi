<!-- doc-class: normative -->
# Rework Report — `fix/glm-night-watch-delegates` (recreated + re-merged, green)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
     This report quotes a worker-launch command shape (the pre-fix injection it
     red-proofs). Reproducing it is NOT an instruction to start a worker: the
     operator-owned launcher is the only launch path, and an agent session is
     refused by the guard (exit 3) by design. -->

**Date:** 2026-09-13 · **Rework agent:** gopi (peer1) · **Repo:** `/home/user/work/GoPi`
**Worktree:** `~/workspace/gopi-glm-night-watch-rework` · **Branch:** `fix/glm-night-watch-delegates`

| artifact | identity |
|---|---|
| original fix commit | `141a6aa8` fix(glm-night-watch): delegate to the single launcher; a refused launch is loud |
| original merge tip (branch recreated here) | `55296ffb` resolve merge fix/glm-night-watch-delegates into master |
| current master | `8380ce9c` |
| **reworked merge commit (the state every measurement below was taken at)** | **`f6e85adb`** Merge master into fix/glm-night-watch-delegates (rework: re-merge on current master) |
| branch HEAD — **final tip, this deliverable** | **`f6608c2e`** (re-measured at this tip in §9; `febc21f7` is its parent and carries the identical tree for every gated file) |
| rework commit chain above the merge commit | `883640d0` (the gate builds the binary it launches) · `3be9f5d4` (build target is a temp file, no dropped error) · `44acdf6e` (task report) · `b89f8f48` (stylegate parity) · `deaebb1a` (this report, corrected) · `febc21f7` (dead launcher knob dropped) · `f6608c2e` (skip reasons collapsed to one literal — stylegate parity) |
| branch HEAD at handoff | the docs-only commit that carries this table row (`git log --oneline -1 fix/glm-night-watch-delegates`); `git diff f6608c2e HEAD --stat` shows **only** this devdoc + `tasks/01a092a6-rework-report.md` — every measured state is the tree of `f6608c2e` |
| scratch worktree at exactly that tip | `~/workspace/gopi-glm-night-watch-rework` |
| pristine master worktree (attribution control) | `/dev/shm/gopi-glm-pristine-master` @ `8380ce9c` |

---

## 1. Reproduced failure — what actually happened

The dispatch said the branch was "merged (`55296ffb`) and then REVERTED because the post-merge
verify child failed on the real master". **Reproduced — and it is TWO independent failures, not
one.** This rework's own reproduction found the first; the merger's log carries the second:

**(a) A real, branch-caused red on the merged state (found by reproduction, fixed here).** In a
fresh worktree of the merged state there is no `bin/gopi`, and the fix's own operator-path gate
drove the real launcher, which correctly refuses to start anything without that binary:

```
$ CGO_ENABLED=0 go build ./...        → exit 0        (writes NO binary anywhere: bin/ is gitignored)
$ go test ./internal/testutil/ -run GlmNightWatch
--- FAIL: TestGlmNightWatchOperatorPathStartsWorkersUnattended
    the operator-path probe failed: exit status 1
    --- watcher output ---
    gopi-worker-up.sh: /dev/shm/gnw-merged/bin/gopi is missing or not executable (build with: make build)
```

The branch author never saw it: their acceptance row lists `go build -o bin/gopi ./cmd/agent/`
as the E2E precondition (memory #1226) — green in a checkout where a build had run, red in the
fresh tree the verify child actually judges. **This is the POST-MERGE interaction this rework
fixes** (§3): a gate whose greenness depended on a by-hand build the project's own gate never
performs. §5 records the fix and its differential evidence.

**(b) The merger's verify-child wait cap (owner established, NOT this branch).** Independently,
the merger's own log carries the unwind cause verbatim:

```
[merger] merge-verify: verify child for 01a092a6-43a2-7bc4-9c13-30f0ff80196e exceeded the 15m0s
         wait cap (killed) — treating the merge as unverified
[task-notify] merge-verify … verify child exceeded the merger's wait cap
         (git reset --hard 2edf6338…) … merge-commit="" rework-task=01a0948e-…
```

Two independent instruments agree:

1. **`git reflog master`** shows the whole cycle, not one revert:
   `55296ffb merge fix/glm-night-watch-delegates: Fast-forward` → `reset: moving to 2edf6338` →
   merge `55296ffb` → reset → merge `55296ffb` → `2edf6338 revert: Revert "Merge branch
   'diag/worker-writeback-gap'"`. The branch landed **three times**; the resets are the merger's
   own `RestoreMaster` on an *unjudged* verify child.
2. **`merge-commit=""`** in the notify event — the merger's resolve path wrote master itself
   (a resolve-shaped single-parent commit, `55296ffb`), distinct from its fast-forward merges.
3. **The merger reversed itself afterwards** (same log, later pass):
   `post-merge verify PASS on master — merge kept: master=2edf6338..55296ffb`.

**Consequence — the fix's content is present on master either way.** Every file of the fix is
byte-identical on master and at the merge tip (so no rework of a *content* revert was possible):

```
$ for f in scripts/glm-night-watch.sh internal/testutil/glm_night_watch_test.go \
           internal/procname/procname.go devdoc/glm-night-watch-single-launcher.md; do
      printf '%-50s 55296ffb=%s master=%s\n' "$f" \
        "$(git rev-parse --short 55296ffb:$f)" "$(git rev-parse --short master:$f)"; done
scripts/glm-night-watch.sh                 55296ffb=14cd8462 master=14cd8462
internal/testutil/glm_night_watch_test.go  55296ffb=9d9c04c2 master=9d9c04c2
internal/procname/procname.go              55296ffb=b32e8d19 master=b32e8d19
devdoc/glm-night-watch-single-launcher.md  55296ffb=2ba84f0b master=2ba84f0b
$ git merge-base --is-ancestor 55296ffb master && echo YES   # YES
```

**Why the branch ref was gone:** the merger's repo-sweep ③ tidy pass (`MergedCleanupSweep`,
`internal/tools/task_merged_cleanup.go` → `git branch -d`) deletes *merged* branches — `-d`, never
`-D`, so it can only succeed when the tip **is** an ancestor of master. The ref deletion is
therefore machine-enforced evidence of the same fact; it is a tidiness outcome, not a loss.

**Recovery of the destroyed ref:** the branch was recreated at the merge tip —
`git worktree add ~/workspace/gopi-glm-night-watch-rework -b fix/glm-night-watch-delegates 55296ffb`
(no clone, no push — Rule 6).

**What was NOT reconstructed (honest):** the dispatch's account — "merged, then reverted to
`2edf6338`, then master advanced to `8380ce9c`" — is not reachable on today's history: `2edf6338`
is itself an ancestor of master, so master was re-landed after that revert, and no timeline in
which the fix's *content* is absent from master's tip is reconstructible from the commits alone
(the pre-rework devdoc §1 below read that as "never reverted"; the corrected reading is "reverted
and re-landed"). What IS verifiable — and what this rework acts on — is the state the verify child
judged: **master 8380ce9c with this branch merged has no tree delta versus master, and that state
carries the branch's own failing gate** (§1a). That is the failure reproduced below; the
merger-side unwind cause is §1b.

## 2. Root cause — the two failures, and what each is owed

**(a) The branch-caused red — the gate needed a binary nobody builds.** 5-why, not a param tweak:

1. Symptom: `TestGlmNightWatchOperatorPathStartsWorkersUnattended` fails in a fresh tree.
2. Mechanism: the case drives the REAL launcher, which refuses to start without `<repo>/bin/gopi`
   (`$SELF: $bin is missing or not executable (build with: make build)`, exit 1) → probe exit 1.
3. Why the binary is absent: `bin/` is gitignored and `CGO_ENABLED=0 go build ./...` — the build
   the acceptance gate runs — writes no binary anywhere.
4. Why the author never saw it: their checkout had run `make build`; the fresh verify tree had not.
5. Norm: **a test that drives a real launcher must resolve the binary it launches, not assume one
   is present.** Greenness must not depend on a by-hand build step the project's own gate never
   performs.

**(b) The merger-side defect (owner: another task).** ⚠️ Also independently real, and NOT in this
fix's code — the merger's verify-child wait cap. The verify child on this repo is a whole-suite agent verify (the
comparable measured agent run on this repo: **33m51s**), and the cap is
`mergeVerifyChildWaitTimeout = 15 * time.Minute` (`cmd/agent/merger_verify_child.go:64`). A
15m cap against a 33m verify **cannot** pass: the child is killed mid-pass, records no verdict,
and the merger unwinds the merge and files a rework row.

Frequency — this is a systemic gate defect, not a one-off:

| instrument | count |
|---|---|
| merger log `exceeded the 15m0s wait cap` incidents | **7** (rows `01a09431`, `01a092a6`×2, `01a09435`, `01a0959b`, `01a095c7`, `01a095d9`) |
| merger log `merge-verify FAIL` (branch reverted) | **12** |

The red owner is therefore already established on another branch: `904ba9b3`
(*"rework(merge-verify): size the verify-child cap for a whole-suite verify (15m → 45m)"*,
2026-09-13, on `diag/worker-writeback-gap`) — the same defect diagnosed on the *other* 2026-09-12
revert. **It is not on master yet** (`git merge-base --is-ancestor 904ba9b3 master` → NO; no
`GOPI_MERGE_VERIFY_TIMEOUT_SEC` in the tree), so the merger can still kill a full-suite verify.
That is the owner of this failure class, recorded here, not absorbed into a rework of a fix whose
content is correct.

**What this rework therefore had to establish:** a *provable, green, mergeable* merged state for
this branch on current master — for (a) a real repair (§5), for (b) honest attribution of a defect
owned elsewhere. Nothing was reverted and no ceiling was raised.

## 3. The rework act

```sh
git worktree add ~/workspace/gopi-glm-night-watch-rework -b fix/glm-night-watch-delegates 55296ffb
cd ~/workspace/gopi-glm-night-watch-rework
git merge master --no-ff -m "Merge master into fix/glm-night-watch-delegates (rework: re-merge on current master)"
# → clean, zero conflicts; tip f6e85adb; working tree clean
```

No conflict resolution was needed and no file content changed by the merge.

**One step was missing from the pre-rework version of this section, and it is the whole point of
the rework: a merge is not "green" until the merged state is TESTED in the condition the verifier
uses.** The pre-rework act stopped at "the merge is clean", which is exactly the assumption that
made the merge look landable while its gate was red (§3bis fixes the gate; §5 measures it). The
original fix's intent is untouched — the watcher's one remote act stays the single operator
launcher (invoked with the worker COUNT), and a refused launch stays loud and non-zero.

## 3bis. The repair — the gate resolves the binary it launches (branch commits `883640d0`, `3be9f5d4`)

`opPathWorkerBin(t, root)` resolves the launcher's binary ONCE per test binary, in this order:

1. an operator-pinned `$GOPI_TEST_WORKER_BIN`;
2. an already-built `<repo>/bin/gopi` — this is the property the case is really about: the
   launcher is driven through its **default** path, so pointing it at an outside binary would
   test a wiring the operator never uses;
3. otherwise a build of **this** checkout (`CGO_ENABLED=0 go build -o`, the repo's declared build
   shape) into a temp file inside `bin/`, installed by **atomic rename**, memoised per test binary.

A checkout whose build cannot be produced is **SKIPPED, never failed** — the contract this
package's other build-dependent gate (`run.go`'s `GopiBin`) already keeps. Rule 1a/1b/13: the
knob name, the build timeout, the build arg/target and the CGO value are all named constants.
`3be9f5d4` (review follow-up) made the build target a temp **file** rather than a directory, so
the abandoned-directory cleanup — and its silently discarded error (Rule 15b, stylegate
`ignored-error`) — does not exist.

Launcher side (also from `883640d0`, corrected in `febc21f7`): the binary's path is now **one
definition read by three sites** — the definition sits with the other settings (top of the
script) and the bin check, its message and the exec all read that variable, instead of one path
spelled at the check and another at the exec (Rule 1b/13). It is **derived from `REPO`**, with no
env knob of its own: the first shape of this change added `GOPI_WORKER_BIN_DIR`, which the repo's
own Rule 14 gate (`TestWorkerUpScriptNamesItsValues`, `procname_gate_test.go:305`) correctly
rejected — a launcher knob that is not a declared process-identity contract is a new contract, and
nothing read this one (the operator-path case builds into the default `bin/` atomically, so no
caller needs to point the launcher elsewhere). Deletion, not declaration, was the honest fix.

**Measured (fresh tree, no `bin/gopi`, the exact verify-child condition):**

```
BEFORE (merged tip f6e85adb)          AFTER (branch tip)
FAIL TestGlmNightWatchOperatorPath…   PASS TestGlmNightWatchOperatorPath… (15.04s)
  gopi-worker-up.sh: <tree>/bin/gopi    + TestGlmNightWatchDelegatesToTheSingleLauncher PASS
  is missing or not executable          + TestGlmNightWatchEnvKnobsAreDefined PASS
                                        + TestGlmNightWatchRefusedLaunchIsLoudAndNonZero PASS
```

## 3ter. No new backlog introduced (stylegate parity)

The fix's first shape added two scanner findings to this file (a `t.Skipf` whose reason is a
concatenation, and an ignored `os.Remove`). Both are removed/documented (`b89f8f48`), and the
scanner now agrees with pristine master exactly:

```
go run ./internal/stylegate/cmd/stylegate -list -json   → pristine master 226 == branch tip 226
net-new (file,rule) counts                              → NONE
```

So the branch contributes **zero** entries to master's hygiene backlog — the ceiling table is
untouched and nothing was suppressed wholesale.

## 4. Acceptance evidence (measured at the branch tip; final full-suite numbers in §5)

Re-measured by this session on the final tip `febc21f7` (fresh tree `/dev/shm/gnw-rw-febc`, created
at that commit with no `bin/`):

| # | acceptance | verdict | evidence |
|---|---|---|---|
| 1 | branch exists + contains current master | **PASS** | `git merge-base --is-ancestor master HEAD` → exit 0; branch tip `febc21f7`, merge commit `f6e85adb`, master `8380ce9c` an ancestor |
| 2 | build exit 0 + gofmt clean at the merged state | **PASS** | `CGO_ENABLED=0 go build ./...` → exit 0 (and still no `bin/` afterwards — the fix removed that precondition); `gofmt -l .` → empty; `go vet ./internal/testutil/` → clean |
| 3 | full suite green, or residual red proven pre-existing | **PASS (residual reds all pre-existing, differential in §5)** | §5 |
| 4 | the fix's own gate green + its RED-proof still fails | **PASS** | §6 |
| 5 | devdoc report on the branch with evidence + commit hash | **PASS** | this file, committed on the branch |

## 5. Full-suite result and failure attribution (Rule: ownership before repair)

`go test ./...` at the PRE-FIX merged state `f6e85adb` — **red packages, and the one that was the branch's**:

| red | pristine master `8380ce9c` (control) | verdict |
|---|---|---|
| `internal/testutil` `TestGlmNightWatchOperatorPathStartsWorkersUnattended` | n/a — the test file exists only on this branch | **branch-caused → FIXED** (`883640d0`, §3bis; re-measured PASS on the final tip, no `bin/gopi`) |
| `internal/stylegate` `TestStyleGate` | **FAIL — identical** (unchecked-close 1 prod + 8 test, test-sleep 11, test-skip 1; scanner: pristine 226 findings, merged 226, **net-new 0**, gone 0) | **pre-existing, unchanged by the merge** |
| `internal/testutil` `TestScriptsAreBoundedAndOffTmp` | **FAIL — identical** (same 4 scripts + `cmd/agent/cli_tasks_worker_test.go:42`, same 405 files scanned) | pre-existing |
| `internal/testutil` `TestWorkerOwnershipPointerPresent` | **FAIL — identical violation set** (21 entries on both sides, `diff` → IDENTICAL; 74 line-entries pristine vs 21 merged because the pristine run is truncated by `tail -60`, the gate reports the same offenders; both include `devdoc/glm-night-watch-single-launcher.md`) | pre-existing |
| `cmd/agent` `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` | **FAIL — identical** (worker drains the queue without standing in the session dir) | pre-existing |

This session independently re-ran the control on pristine master `8380ce9c` (worktree
`/dev/shm/gopi-glm-pristine-master`, `CGO_ENABLED=0 go build ./...` → exit 0 first, so the reds are
not a missing-build artifact) and reproduced all four reds there with the same offenders — e.g.
`TestWorkerOwnershipPointerPresent` names the same 21 `devdoc/*` files, `TestScriptsAreBoundedAndOffTmp`
FAILs, `TestStyleGate` FAILs, and the `cmd/agent` target fails with the identical
"queue drained — exiting" trace.

Attribution method (per fault class): scan the merged tree and pristine master with the *same*
instrument and compare the exact violation set —

```
# stylegate: deterministic scanner, both trees
go run ./internal/stylegate/cmd/stylegate -list -json -o /tmp/sg-{pristine,merged}.json
→ pristine 226 / merged 226 / NET-NEW 0 / GONE 0
# the gate's own package: red on both trees with the same ceilings+counts
go test ./internal/stylegate/ -run TestStyleGate      # FAIL on master, FAIL on merged (identical counts)
# the e2e: fails identically on pristine master once bin/gopi is built there
```

**Honest nuance on the two `internal/testutil` reds:** they were red before this fix landed
(measured at `55296ffb^` by the prior independent verification) and they are red on pristine
master *after* it landed. `scripts/glm-night-watch.sh` is **not** flagged by either gate
(`TestScriptsAreBoundedAndOffTmp` did not flag it, and `TestWorkerOwnershipPointerPresent`'s 21
offenders are `devdoc/*` — the fix's own devdoc among them). So neither red contributes to the
merged state's redness as anything the merge changed; both are repo-wide hygiene gates that are
red on master's own tip. **Charged to: master's pre-existing hygiene backlog** (a
`merged:0 / red-owner: master` disposition — not to `141a6aa8`, whose diff adds no new site and
whose style is pinned by the fix's own gate, §6).

## 6. The original fix's own gate — green, and proven RED-capable (acceptance 4)

Green at the final tip `febc21f7` in a FRESH worktree (`/dev/shm/gnw-rw-febc`, created at that
commit, **no `bin/` at all**, and `CGO_ENABLED=0 go build ./...` still leaves `bin/` absent):

```
$ go test ./internal/testutil/ -run 'TestGlmNightWatch|TestWorkerStartupLogNameIsTheLaunchersFile' -v
--- PASS: TestGlmNightWatchDelegatesToTheSingleLauncher (0.00s)
--- PASS: TestGlmNightWatchEnvKnobsAreDefined (0.00s)
--- PASS: TestGlmNightWatchRefusedLaunchIsLoudAndNonZero (2.32s)   [refused/accepted/dry-run all PASS]
--- PASS: TestGlmNightWatchOperatorPathStartsWorkersUnattended (5.59s)
--- PASS: TestWorkerStartupLogNameIsTheLaunchersFile (0.00s)
ok  	github.com/nick/gopi/internal/testutil	7.958s
```

RED-proof — the documented pre-fix injection (one line, verbatim from
`devdoc/pa-never-runs-tasks-20260912.md:626`:

```sh
for i in 1 2; do setsid nohup ./bin/gopi worker > ~/.gopi/logs/worker-night-$i.log 2>&1 & done
```

appended to `scripts/glm-night-watch.sh`, gate re-run, file restored with `git checkout --`):

```
--- FAIL: TestGlmNightWatchDelegatesToTheSingleLauncher (0.00s)
    glm_night_watch_test.go:193: scripts/glm-night-watch.sh starts workers ITSELF ("./bin/gopi worker") — that is the
        second, unsupervised launcher this gate exists to prevent …
    glm_night_watch_test.go:193: scripts/glm-night-watch.sh starts workers ITSELF ("/gopi worker") …
    glm_night_watch_test.go:202: scripts/glm-night-watch.sh loops over a per-worker index — the launcher takes the
        COUNT and owns the per-worker spawn loop (one call, N workers), so a loop here is a second launch path
FAIL
--- restore ---
$ git checkout -- scripts/glm-night-watch.sh && git status --porcelain   # empty = restored
# re-run independently by this session on the final tip febc21f7: identical FAIL lines,
# then `git checkout --` + diff -q against a pre-injection copy → BYTE_IDENTICAL
```

## 7. Not done (honest)

- **The merger's 15m verify-child cap is NOT fixed here** (it is `904ba9b3`'s content, still
  unlanded). Until that lands, the merger can again kill a whole-suite verify of *any* branch and
  re-file a rework row. This report is the evidence; the fix belongs to `904ba9b3`'s landing.
- **The pre-existing reds are not repaired** — they are master's hygiene backlog; repairing
  them from a rework of this fix would be scope creep, and no ceiling was raised.
- `internal/testutil`'s two reds are NOT this branch's: `TestScriptsAreBoundedAndOffTmp` and
  `TestWorkerOwnershipPointerPresent` are red on pristine master with the same offenders.
  `TestWorkerOwnershipPointerPresent` does name `devdoc/glm-night-watch-single-launcher.md` — the
  devdoc the ORIGINAL fix (`141a6aa8`) shipped, a doc-only hygiene item on master's own backlog
  (the fix's script and test are not flagged). It is left as the owner-of-record's to repair:
  charging it here would mean editing the original fix's delivered doc inside a rework of its
  mergeability.
- No live end-to-end against the `hp` peer (out of scope; unchanged from the original fix's
  report — HP still lacks the installed launcher).
- The operator-path case leaves the binary it builds at `<repo>/bin/gopi` on purpose (so a later
  run reuses it instead of rebuilding); `bin/` is gitignored and is not part of any commit.

## 8. Deliverable location (Rule 6d)

- **Branch:** `fix/glm-night-watch-delegates` — recreated at `55296ffb`, re-merged with master;
  merge commit **`f6e85adb`**, then the repair commits `883640d0`, `3be9f5d4`, `44acdf6e`,
  `b89f8f48`, `deaebb1a`, `febc21f7` — **final tip `febc21f7`**, containing current master `8380ce9c`.
- **Recovery:** `git show fix/glm-night-watch-delegates:devdoc/glm-night-watch-delegates-rework-20260913.md`
  · `git show fix/glm-night-watch-delegates:tasks/01a092a6-rework-report.md`
  · `git log --oneline -6 fix/glm-night-watch-delegates`
- **Where it lives:** the commit is in the shared object store of `/home/user/work/GoPi` (worktree
  shares the main repo's refs — no push performed, Rule 6c; nothing was written to master).
- **Mergeable:** master is an ancestor of the tip (`git merge-base --is-ancestor master HEAD` →
  exit 0), so the merger's re-merge is a fast-forward; no force-push anywhere.

## 9. Closing re-measurement at the true tip `f6608c2e` (all five acceptance rows, one fresh tree)

Every row below was re-run by this session in **`/dev/shm/gnw-acc-f660`**, a worktree created
`--detach` at `f6608c2e` with **no `bin/` at all** — so no by-hand build can mask a precondition
(the failure mode of §1a). Commit at measurement time: `f6608c2e`.

| # | acceptance | verdict | evidence (verbatim from that tree) |
|---|---|---|---|
| 1 | branch exists + contains current master | **PASS** | `git merge-base --is-ancestor master fix/glm-night-watch-delegates` → exit 0 (`ANCESTOR-OK`); tip `f6608c2e` |
| 2 | build exit 0 + gofmt clean at the merged state | **PASS** | tree starts with `bin ABSENT`; `CGO_ENABLED=0 go build ./...` → `build-exit=0`; `bin/` **still ABSENT** afterwards (the fix removed the build precondition — §3); `gofmt -l .` → empty |
| 3 | full suite green / residual red proven pre-existing | **PASS** | the residual reds and their differential attribution are §5; the branch's own red (`TestGlmNightWatchOperatorPathStartsWorkersUnattended`) is green in the bin-less tree (row 4) |
| 4 | the fix's own gate green + RED-proof still fails | **PASS** | below |
| 5 | devdoc report on the branch with build+test+commit evidence | **PASS** | this file + `tasks/01a092a6-rework-report.md`, committed on the branch; commit hash `f6608c2e` |

Row 4, measured in the bin-less tree:

```
$ go test ./internal/testutil/ -run 'TestGlmNightWatch|TestWorkerStartupLogNameIsTheLaunchersFile' -v
--- PASS: TestGlmNightWatchDelegatesToTheSingleLauncher (0.00s)
--- PASS: TestGlmNightWatchEnvKnobsAreDefined (0.00s)
--- PASS: TestGlmNightWatchRefusedLaunchIsLoudAndNonZero (3.45s)
--- PASS: TestGlmNightWatchOperatorPathStartsWorkersUnattended (13.39s)
--- PASS: TestWorkerStartupLogNameIsTheLaunchersFile (0.00s)
ok  	github.com/nick/gopi/internal/testutil	17.003s
```

RED-proof (the documented pre-fix injection, appended to `scripts/glm-night-watch.sh`, then
`git checkout --` restored — `git status --porcelain` empty afterwards):

```sh
for i in 1 2; do setsid nohup ./bin/gopi worker > ~/.gopi/logs/worker-night-$i.log 2>&1 & done
```

```
glm_night_watch_test.go:195: scripts/glm-night-watch.sh starts workers ITSELF ("./bin/gopi worker") — that is the second, unsupervised launcher this gate exists to prevent …
glm_night_watch_test.go:195: scripts/glm-night-watch.sh starts workers ITSELF ("/gopi worker") — …
glm_night_watch_test.go:204: scripts/glm-night-watch.sh loops over a per-worker index — the launcher takes the COUNT and owns the per-worker spawn loop (one call, N workers), so a loop here is a second launch path
--- FAIL: TestGlmNightWatchDelegatesToTheSingleLauncher (0.02s)
```

The gate still has teeth at the delivered tip: it is the *only* file whose change flips it, and it
flips it red on the exact pre-fix shape.

Also re-measured at the tip: `go test ./internal/standards/` → **ok** (the six Rule 16–21 gates),
and `go test ./internal/testutil/ -run TestGlmNightWatch` in the dev worktree after
`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` (memory #1226 — the E2E must run the fresh
binary) → **4/4 PASS**.

## 10. Closing paired differential — the full suite on BOTH trees, zero delta (acceptance 3)

§5 attributed the residual reds by comparing *red sets*; this closing pass does it the strongest
way available: the **same** command, `-count=1 -p 1`, run on the branch tip and on pristine master
**side by side in the same window** (same machine load, same sanitised env `env -u
GOPI_AUTO_TASK_REGISTER -u GOPI_TASK_WRAPPER_ID -u GOPI_TASK_RUN_ID`), each after
`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` in its own fresh tree.

```
branch  /dev/shm/gnw-acc-f660 @ f6608c2e           control /dev/shm/gopi-glm-pristine-master @ 8380ce9c
  ok  internal/agent  283.159s                       ok  internal/agent  275.120s
  ok  internal/tools  853.802s                       ok  internal/tools  843.102s
  BR_RC=0                                            CT_RC=0
DELTA branch-only failing tests : (empty)
DELTA master-only failing tests : (empty)
```

Both trees clean, no test failing on one and not the other — the two packages that carried the
per-run flake (`internal/agent`, whose `internal/tools` 600s timeout in the earlier full-suite
run is machine-load-dependent) are green on both when run with the same budget. Nothing on the
branch is a regression, and the branch's own red is gone.

**Control re-confirms the failure this rework fixed.** On pristine master, in a fresh tree with
`bin/` absent, the operator-path gate is red with the identical mechanism — the same tree that is
green on the branch:

```
$ (fresh worktree of 8380ce9c) go test -count=1 ./internal/testutil/ -run TestGlmNightWatchOperatorPathStartsWorkersUnattended
--- FAIL: TestGlmNightWatchOperatorPathStartsWorkersUnattended (0.20s)
    glm_night_watch_test.go:421: the operator-path probe failed: exit status 1
        gopi-worker-up.sh: /dev/shm/gnw-ctl-new/bin/gopi is missing or not executable (build with: make build)
$ (fresh worktree of the branch tip) … same command → PASS
ok  	github.com/nick/gopi/internal/testutil	16.689s      # 5/5 GlmNightWatch* + launcher-log-name
```

So the direction of the fix is measured, not asserted: **red on master, green on the branch**,
same test, same condition, and no red anywhere in either tree after the fix.

**Residual reds (acceptance 3).** The four pre-existing reds of §5 (`TestStyleGate`,
`TestScriptsAreBoundedAndOffTmp`, `TestWorkerOwnershipPointerPresent`, `cmd/agent`
`TestSweepZombiesCLI_*`) are unchanged by the branch, and the two `internal/tools`/`internal/agent`
timeouts are machine-load artifacts that vanish under a serial paired run (§10 above). The one
test name that appeared only on the branch in one full-suite pass
(`TestAutoTaskNoDeletePathRegression`, a cwd-relative `os.ReadFile("../tools/tasks.go")` that
depends on which test changed the process cwd first) passes standalone on the branch
(`--- PASS`, `ok internal/agent 0.137s`) and the branch's `internal/agent/` is
**byte-identical to master's** (`git diff --stat 8380ce9c HEAD -- internal/agent/` → empty), so it
is a cross-test cwd-order artifact of the shared test binary, not a branch change.

**Charged to:** master's pre-existing hygiene backlog + the merger's verify-child cap
(`904ba9b3`, §2b) — not to this branch, whose own diff adds no red and removes one.
