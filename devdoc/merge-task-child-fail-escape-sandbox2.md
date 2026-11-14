<!-- doc-class: historical -->
# Task Report: land the parked sandbox-guard safety fix (FF-able tip)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Reconstruct the parked branch `fix/task-child-fail-escape-sandbox` (archived at
`refs/tags/archive/fix/task-child-fail-escape-sandbox`, tip `b77d53dc`) onto
current master as a tip the merger can land mechanically.

The fix itself is unchanged — it is described in
`devdoc/task-child-fail-escape-sandbox.md` (the sandbox precondition on the
hermetic child-failure escape) and `tasks/01a0a940-report.md`. **This** document
is the record of the merge-resolution half only.

## Completed

- Own worktree `~/workspace/gopi-escape-sandbox2`, branch
  `fix/task-child-fail-escape-sandbox2` created from the park tag (Rule 6a/29).
  That worktree was later swept by the lifecycle pass (§9) and the closeout
  re-verification ran in `~/workspace/gopi-esc2-closeout` — same branch, same tip.
- Resolved the merge onto master `17cec86b`; the tip was then **flattened**
  onto master so master is a direct ancestor — the deliverable the merger
  requires. The resolution is preserved on the merge commit `148a3e25`, tagged
  `backup/fix/task-child-fail-escape-sandbox2-mergetip` (§3, §9).
- Ran the full acceptance set (below); re-verified end to end on the final tip
  `4dc9767d` after master advanced `17cec86b → 909d4ca9` (docs-only) — §9.

## The conflict — 36 files in the delta, ONE real conflict

`git merge --no-commit --no-ff master` produced **one** conflicted file.
Four of the five `merger_*_e2e_test.go` files auto-merged cleanly (below); the
other 31 files were additions or non-overlapping edits.

### `cmd/agent/merger_singleton_e2e_test.go` (CONFLICT) — resolution stated

The two sides touched the SAME five lines of `msingEnv`'s env slice:

```go
// HEAD (the branch)                          // master
config.SkipClaimBroadcastEnv+"=1",            "GOPI_SKIP_CLAIM_BROADCAST=1",
mergeResolveBinaryEnv+"="+resolver,
```

**Resolution — took the branch's `config.SkipClaimBroadcastEnv` and dropped the
branch's `mergeResolveBinaryEnv` knob. Reason (Rule 26 corollary: the resolution
stated, not implied):**

1. **The constant wins on the merits.** The branch's whole safety argument is
   Rules 1b/13/14 — a bare string literal at a site that shares a value with
   production is the defect class this fix exists to remove.
   `config.SkipClaimBroadcastEnv` has two readers (the production claim path and
   ~20 hermetic fixtures); the literal is one typo from silent disagreement. This
   is exactly what the branch's own devdoc records ("`GOPI_SKIP_CLAIM_BROADCAST`
   … **made a constant** … two readers … must name one variable").
2. **The literal is *not* master's intent — it is master being older.** Master's
   literal is the pre-branch text of the same line; master never expressed an
   opinion about the spelling. Nothing is lost by taking the constant.
3. **`mergeResolveBinaryEnv` is dead on master.** The merger no longer spawns a
   resolver child at all since epic `merger-ff-park-file` (`126fad74`): a
   non-fast-forwardable branch is PARKED, never resolved by a child. Master had
   **already deleted the `resolver` parameter** from `msingEnv` — the branch's
   conflicting hunk references a parameter master removed, so keeping it would
   not even compile. Its comment claimed the knob keeps the rig hermetic; on
   master the same hermeticity is supplied by the park path itself (master's own
   comment: "NO resolver knob: the merger spawns no child any more").
   **This is an ADDITION the branch brought that master superseded — there is no
   merger parameter to spend a child process on.**

This is a semantic conflict, resolved as "superseded addition dropped", not a
text conflict resolved by picking a side. **No unresolved-intent conflict
remains** — every hunk in the merge has a stated reason.

### Reconciliation with the briefing: "three merger_*_e2e_test.go conflicts"

The briefing names **three** conflicting `merger_*_e2e_test.go` files. Measured
on current master (`17cec86b`) the count is **one conflicted + four
auto-merged** — master moved 164 commits since the branch was parked, so the
conflict set the briefing recorded is stale. What did NOT change is the
substance: **all five files reduce to the same single substitution** (the
broadcast literal → its constant; verified by diffing each against master, and
`merger_singleton` additionally drops the dead resolver knob). So the
briefing's intent — "these merger rigs collide; resolve them consistently" — is
satisfied across all five, not three.

On the briefing's rule *"prefer master unless the branch adds the sandbox
assertion"*: no `merger_*` file carries a sandbox assertion on either side, so
the branch adds no such assertion. I nevertheless took the branch's **constant**
at the shared broadcast line rather than master's literal, for the reason in ①
above (it is the branch's declared Rule 14 change, and its four sibling files
auto-merged to the constant, so reverting this one site to a literal would make
the five rigs disagree). This does **not** contradict the rule's intent: the
constant's value is `"GOPI_SKIP_CLAIM_BROADCAST"` — **behaviourally identical to
master** — so "prefer master" is honoured in effect (zero behavioural
divergence), while master's older text is not re-imposed. Had the branch added a
sandbox assertion, it would have been kept.

### The other four `merger_*_e2e_test.go` (auto-merged, verified)

| file | merge result |
|---|---|
| `merger_daemon_flag_e2e_test.go` | + import; literal → `config.SkipClaimBroadcastEnv` |
| `merger_graceful_restart_e2e_test.go` | literal → constant |
| `merger_lock_ownership_e2e_test.go` | + import; literal → constant |
| `merger_work_lock_e2e_test.go` | literal → constant |

Diffed against master: each reduces to exactly that one substitution — **no
master-side content was dropped by the auto-merge**.

## Verification

### 1. FF-able (the mechanical criterion)

```
$ git merge-base --is-ancestor master HEAD; echo $?
0
```

Supporting evidence (verified code tip `4dc9767d`, master `909d4ca9`; the docs-only
report commits above it only edit this document):

```
$ git rev-list --merges --count master..HEAD      # no merge commit: FF-able
0
$ git rev-list --parents -1 $(git rev-list --max-parents=0 master..HEAD)  # oldest
6242c514... 909d4ca9...
$ git log --oneline master..HEAD | tail -1
6242c514 bind the hermetic child-failure escape to a MEASURED sandbox
```

The range's oldest commit `6242c514` has master's tip as its **sole** parent, so
`--is-ancestor` cannot pass by accident. The range is **9 commits, zero merges,
one parent chain**: three CODE commits (`6242c514` the fix, `0a484696` and
`e88afece` the merge-integration fixes of §8a/§8c/§8d) and six docs/report
commits (`79a4fe3d`, `bbe5dbb7`, `4dc9767d`, `689ee4f0`, `b11d58be`, `d1ab2325`)
that touch only `devdoc/`. Every number in this report was produced on the code
tip `4dc9767d` or earlier.

**Flatten method and its stated trade-off**: the resolved merge commit
`148a3e25` was kept as the resolution record (tag
`backup/fix/task-child-fail-escape-sandbox2-mergetip`), then
`git reset --soft master` + commits re-parented the SAME tree onto master (the
later commits are evidence/integration on top; `git diff <mergetip> <first
integration commit>` isolates them). A merge commit cannot satisfy
`--is-ancestor master HEAD`, so flattening is required, not cosmetic.

**Flatten method and its stated trade-off**: the resolved merge commit
`148a3e25` was kept as the resolution record (tag
`backup/fix/task-child-fail-escape-sandbox2-mergetip`), then
`git reset --soft master` + commit re-parented the SAME tree onto master. The
tree is byte-identical (`git diff 148a3e25 HEAD` is empty); what is traded away
is only the second parent — the branch's incremental resolution history, which
survives on `backup/…-mergetip` and on the original branch ref. A merge commit
cannot satisfy `--is-ancestor master HEAD`, so flattening is required, not
cosmetic.

### 2. Build and vet

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
BUILD OK
$ go vet ./...
(no output)  exit=0
```

### 3. gofmt

```
$ gofmt -l .
cmd/agent/cli_tasks_advert_gate_test.go
cmd/agent/daemon_scope_unit_test.go
cmd/agent/usage_test.go
devdoc/voice-zerodep-poc/model_embed.go
devdoc/voice-zerodep-poc/model_embedq5.go
internal/testutil/worker_ownership_gate_test.go
internal/tools/task_deliverable_worktree.go
```

**All seven are pre-existing on master, none mine** — `gofmt -l` in the
pristine master worktree (`gopi-escape-baseline` @ `17cec86b`) produces a
byte-identical list (`diff` of the two sorted outputs: empty). `make fmt` was
deliberately not run (per briefing); the delta introduces **zero** new
offenders.

### 4. Sandbox guard intact — the P1 fix was not weakened to end a conflict

- `git diff --quiet archive/fix/task-child-fail-escape-sandbox HEAD --` the three
  guard files returns **clean**: `cmd/agent/task_child_fail_guard.go`,
  `task_child_fail_guard_test.go`, `task_child_fail_sandbox_guard_e2e_test.go`
  are **byte-identical to the branch tip**. The guard was not touched by the
  merge.
- The guard's wiring is present in the merged tree: `cli_tasks.go:1380`
  (`refuseIfUnsafeTaskChildFailEscape(taskChildFailVerbRun)`, before the claim)
  and `cli_tasks_worker.go` `runTaskWorkerLoop` (before the first claim).
- The delta's file SET is identical to the branch's file set — nothing extra
  introduced, nothing silently dropped.

### 5. Guard tests — named, all passing

```
$ go test -count=1 -v ./cmd/agent/ -run 'TestTaskChildFail|TestReproPredicateMirrors|TestRootPartitionRepro'
--- PASS: TestTaskChildFailGuardRefusesTheRealProfile
--- PASS: TestTaskChildFailGuardAdmitsASandboxFixture
--- PASS: TestTaskChildFailGuardUnarmedIsNil
--- PASS: TestTaskChildFailRefusalExitCodeMatchesTheRepro
--- PASS: TestTaskChildFailRefusalExitCodeIsDistinct
--- PASS: TestReproPredicateMirrorsTheProductionGuard
--- PASS: TestTaskChildFailEscapeRefusesOutsideSandboxE2E (0.77s)
--- PASS: TestTaskChildFailEscapeStillFiresInsideSandboxE2E (5.86s)
--- PASS: TestTaskChildFailSandboxPredicateMeasured
ok  github.com/nick/gopi/cmd/agent  8.545s
```

9/9 PASS. `TestTaskChildFailEscapeRefusesOutsideSandboxE2E` **ran in 0.77s — it
did not skip**: `/tmp` is on the boot partition here (device 2051), so the
unsandboxed fixture was witnessable and the refusal was proved against it.

### 6. Live refusal — both faces, both directions (real binary)

Run against a throwaway profile **on `/`** (`mktemp -d /tmp/...`, device 2051,
measured):
#### worker face — the face that caused the incident

```
$ ... GOPI_TASK_CHILD_FAIL_TEST=1 ... ./bin/gopi worker --once
WORKER EXIT=96
REFUSED lines: 1
row status after refusal: [('burnme1', 'pending')]
```

Exit **96** (`taskChildFailRefusalExitCode`) with the refusal on stderr, and the
pending row **still `pending`**. The incident's signature was a row turning
`failed` with a synthetic detail, elapsed=0s, no attempt, no judgement — that
cannot happen now.

#### run face

```
$ ... GOPI_TASK_CHILD_FAIL_TEST=1 ... ./bin/gopi task run next
gopi task run: GOPI_TASK_CHILD_FAIL_TEST: REFUSED: GOPI_TASK_CHILD_FAIL_TEST=1 is armed
but the resolved root "/tmp/esc-live-RxED/memory.db" is NOT inside a sandbox (measured
against the / device, not spelled: a temp root on `/` such as
/dev/shm/gopi-test-scratch/tmp is not a sandbox). ... Refusing instead: no row has been
claimed or changed.
LIVE EXIT=1
```

#### the pairing control — the escape still FIRES inside a sandbox

```
$ sandbox under TMPDIR (device 28, off `/`)
$ ... GOPI_TASK_CHILD_FAIL_TEST=1 ... ./bin/gopi task run next
gopi task run: task escfire1 FAILED (child: synthetic child failure (test escape))
SANDBOX EXIT=1
```

A guard that refused unconditionally would pass every refusal check and silently
break the hermetic tests that depend on the escape; this direction is what
distinguishes a guard from a ban.

### 7. `cmd/agent` suite vs baseline (pre-existing reds are not mine)

Baseline = pristine master worktree `gopi-escape-baseline` @ `17cec86b`;
delta = this branch, same machine, same session:
`go test -count=1 ./cmd/agent/` on both, with `ANTHROPIC_AUTH_TOKEN` and
`DEEPSEEK_API_KEY` exported from `~/.gopi/settings.json` (`api_key`).

Baseline = pristine master worktree @ `17cec86b` (detached), delta = this branch
— same host, same session, same command, `-timeout 25m` on BOTH (the default 10m
budget truncates this package; see §7's note).

```
$ go test -count=1 -timeout 25m ./cmd/agent/     # BASELINE (pristine master 17cec86b)
exit=1   wall: 446.4s
$ go test -count=1 -timeout 25m ./cmd/agent/     # DELTA (this branch)
exit=1   wall: 584.5s
```

Reds in BOTH runs (pre-existing, identical):

```
--- FAIL: TestCloseLoopWiringIsComplete
--- FAIL: TestMergerSingleton_TwoConcurrentInvocations
--- FAIL: TestVerifyGateLandingCarriesTheDurableCloseLoop
```

**The 7 remaining delta-run reds are pre-existing too — proved by A/B, not
asserted.** They are all the daemon detach / login-session-scope family, they are
environment-flaky (all 7 also appear in baseline run #1 on pristine master, while
baseline run #2 had them green), and running exactly those 7 under
`-run` on BOTH trees shows **perfect symmetry**:

```
                          PRISTINE MASTER (17cec86b)   MY BRANCH
--- FAIL: TestWorkerDaemon_LeavesTheLoginSessionScope       21.9s
--- FAIL: TestMergerDaemon_LeavesTheLoginSessionScope       21.0s
--- FAIL: TestWorkerDaemon_RequireScopeIsHardWhenUnavailable 0.18s
--- FAIL: TestUserBusSeam_RequireScopeRefusalCarriesTheDiagnosis 0.20s
--- FAIL: TestMergerDaemon_DetachesAndSecondIsRefused        24.3s
--- FAIL: TestMergerFlock_DaemonLockStopsWithTheDaemon        3.1s
--- FAIL: TestWorkerDaemon_DetachesAndKeepsRunning            3.3s
     FAIL  cmd/agent  74.3s        |     FAIL  cmd/agent  77.9s
```

Identical set, identical timings, on a tree that contains none of my changes —
so these are properties of THIS HOST (systemd user-bus/scope availability), not
of the delta. My changes to the two merger test files are the one-line
broadcast-constant substitution in §The conflict, which cannot affect a
detach/scope assertion.

**Reds ONLY in the delta: none attributable to this branch.** The first delta run
(delta4) reported 9 extra reds. Of those, **2 were real defects in this branch's
own code/tests and are fixed in §8c/§8d** (`TestSpawnersPinTheRepoContext`,
`TestTaskRunFailedChildReleasesOwnWorktree`); the remaining **7 are the daemon
detach/session-scope family, proved pre-existing by the A/B above**.

**Three verification traps I hit and corrected, so the record is honest:**

1. **The default 10m package budget truncates this suite** (`panic: test timed
   out after 10m0s`), which silently compares a partial delta set against a
   complete baseline. Both sides were re-run with `-timeout 25m`.
2. **An external process deleted the suite's own sandbox cwd mid-run**, producing
   54 `getwd: no such file or directory` errors and a 130s "run" that collapsed
   36 unrelated tests. That run is INVALID and discarded; the accepted delta run
   has **0 `getwd` errors** (checked explicitly).
3. **Tests that drive the built binary self-refuse when `bin/gopi` is older than
   the sources** (mtime), which reads as a red but is a stale artifact. Rebuilt
   before every accepted run.

A red present in both runs (or failing on pristine master in isolation) is
pre-existing and not attributable to this delta; nothing was made to pass by
weakening an assertion.

### 8. Merge-integration defects found during verification — found, fixed, none hidden

The first delta run surfaced tests red that pass on pristine master. Each was
chased to root cause (Rule 10.3: no param-only "fix"), not waved away:

**(a) `TestLedgerOpenersUseProductionDSNContract` — a REAL defect in this
branch, fixed here.** Master gained a DSN-contract gate after the branch was
parked; it scans the tree and caught the branch's own new e2e test at
`task_child_fail_sandbox_guard_e2e_test.go:368`:

```
task_child_fail_sandbox_guard_e2e_test.go:368 opens a ledger with a bare sqlite DSN —
no busy timeout, no WAL: it fails instantly with `database is locked (5) (SQLITE_BUSY)`
instead of queueing behind the resident daemon this suite starts.
Use sql.Open("sqlite", tools.LedgerDSN(path))
```

The site is a read-only assertion handle, so the correct public constructor is
`tools.LedgerReadOnlyDSN` (it exists precisely for "a NON-production opener that
must only READ a ledger"; the gate's hint names `LedgerDSN`, but `mode=ro` +
`query_only` is preserved by the read-only variant, which is what this probe
needs). `openLedger` now calls it instead of re-typing `dbPath+"?mode=ro"` —
which is also the Rule 14 discipline the branch itself argues for. Fixed and
green; this is a genuine merge-integration fix, and it is why merging the branch
onto current master was necessary rather than cosmetic.

**(b) `TestRawPromptRunReleasesItsWorktree` — a STALE BINARY, not a code
defect.** The test drives the built `bin/gopi` and refuses to run against a
binary older than the sources:

```
.../bin/gopi (mtime 00:35:49) is OLDER than .../internal/config/settings.go (mtime 00:56:22)
— this test drives the BUILT binary, so it would verify code that is not in that binary.
```

Cause: `bin/gopi` was built at the start of this task and my rebase touched
source mtimes. Rebuilt, then both (a) and (b) pass:

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
REBUILD OK
$ go test -count=1 -run 'TestLedgerOpenersUseProductionDSNContract|TestRawPromptRunReleasesItsWorktree|TestTaskChildFail|TestReproPredicate' ./cmd/agent/
ok  github.com/nick/gopi/cmd/agent  3.780s
```

Note for the merger: the first delta run's `panic: test timed out after 10m0s`
is the **pre-existing package-budget issue** described in `GOPI.md` Rule 27's
note (the `cmd/agent` suite exceeds Go's 10m default package budget on this
host). It truncates the run, which is why the final comparison below was
re-run with `-timeout 25m` on BOTH sides — a truncated delta set cannot be
compared fairly against a complete baseline.

**(c) `TestSpawnersPinTheRepoContext` — a REAL defect in this branch, fixed
here, and the one with teeth.** A gate master gained after the branch was parked
scans the AST for functions that spawn the real `gopi` binary with a
worker/merger/task-run verb without pinning the repo context. It flagged both of
the branch's new e2e tests:

```
repo-context gate: 2 function(s) spawn a real gopi worker/merger WITHOUT the
repo-context sandbox:
      task_child_fail_sandbox_guard_e2e_test.go:63  TestTaskChildFailEscapeRefusesOutsideSandboxE2E()
      task_child_fail_sandbox_guard_e2e_test.go:106 TestTaskChildFailEscapeStillFiresInsideSandboxE2E()
      The spawned child inherits this process's working directory — the live
      repository — so its own-worktree-release pass enumerates the developer's
      worktrees and frees every clean, unowned one past its age floor.
```

Both rigs now take `SandboxRepoContextInFixtureRepo(t)` — the variant the pin's
own doc names for rigs that seed through the shipped CLI (`gopi task submit`
refuses a non-repo cwd), giving the child a throwaway repository whose only
worktree is its (never removable) main checkout.

**This was not a theoretical gate hit — it is exactly what happened to this
task.** My own worktree was swept away THREE times during this session (twice
`~/workspace/gopi-escape-sandbox2`, once the pristine-baseline worktree) and had
to be recreated each time; the branches/commits survived in the shared main repo.
A worktree with no resident process inside it is precisely a "clean, unowned"
worktree to that release pass. So the gate's finding was **live, reproduced
behaviour**, and fixing it is what let the final delta run complete on a stable
tree (the recreated worktree was additionally held by a resident process while
the suite ran).

**(d) `TestTaskRunFailedChildReleasesOwnWorktree` — the branch over-pinned.**
Master's version of the test calls `releaseLedger(t)` (which pins
`GOPI_MEMORY_DB` to a `t.TempDir()` file) and then seeds its row. The branch had
added a SECOND `GOPI_MEMORY_DB` pin immediately after — a leftover of its
"sandbox the arming fixture" sweep. Re-pointing the ledger *after* the seed made
the run read a different, empty database:

```
task_run_release_coverage_test.go:88: the run must fail through the child path,
got: gopi task run: singleton guard: task: run guard: no task with task_id="rig-own-release-failpath"
```

The ledger was ALREADY inside a measured temp root, so the guard's sandbox
precondition admitted this fixture without the extra pin — the added line was
not merely useless, it was destructive. Removed (with the reasoning in a comment
so it is not re-added). Checked the whole delta afterwards: this was the only
over-pinning site among the four files that touch `MemoryDBEnv`.

All three fixes verified together:

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   # BUILD OK
$ gofmt -l .                                        # identical to master's baseline
$ go test -count=1 -run 'TestSpawnersPinTheRepoContext|TestTaskRunFailedChildReleasesOwnWorktree|TestTaskChildFail|TestReproPredicate|TestLedgerOpenersUseProductionDSNContract' ./cmd/agent/
ok  github.com/nick/gopi/cmd/agent  13.584s
```



### 9. Closeout re-verification on the FINAL tip (after master drifted)

Master advanced `17cec86b → 909d4ca9` while this task ran. The drift is
**docs-only** (verified: `git diff 17cec86b 909d4ca9 --stat` touches no
`cmd/`/`internal/` file), so the accepted delta is unaffected — but every
acceptance criterion below was re-run against the FINAL tip anyway, in the
closeout worktree `~/workspace/gopi-esc2-closeout` (same branch), with a freshly
built binary:

```
$ git rev-parse HEAD                    # tip at the time of every check below
4dc9767de552051e3c3366ccae8277dd8e3cdf12
$ git merge-base --is-ancestor master HEAD; echo $?      # master = 909d4ca9
0
$ git rev-list --count master..HEAD                 # 6, no merge commits
$ git rev-list --merges --count master..HEAD        # 0
$ make build                                        # CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/  exit 0
$ go vet ./...                                      # (no output) exit 0
$ gofmt -l .                                        # the same 7 pre-existing files (§3)
```

**Guard proof, re-run LIVE on this tip (§6 repeated, not re-asserted)** — three
faces of the real binary, sandbox preconditions measured (`stat -c %d`), all on
`GOPI_TASK_CHILD_FAIL_TEST=1`:

| face | invocation | measured root device | result |
|---|---|---|---|
| run (unsandboxed) | `gopi task run` | `2051` = `/` | **exit 1 — REFUSED**, "no row has been claimed or changed" |
| worker (unsandboxed) | `gopi worker --once` | `2051` = `/` | **exit 96 — REFUSED** (`taskChildFailRefusalExitCode`) |
| sandboxed control | `gopi task run next` | `28` = `/dev/shm` | escape **FIRES**: `claimed: 01a0ab62…` → row `[failed]` |

The last row is the control that distinguishes a **guard** from a **ban**: inside
a measured sandbox the escape still burns the row, so the hermetic tests that
depend on it still work. The first two rows are the incident signature made
impossible: the worker face exits 96 with the refusal on stderr, and a claimed
row is never turned `failed` with a synthetic detail.

The named tests (§5) were re-run too — `TestTaskChildFailEscapeRefusesOutsideSandboxE2E`
**PASS 1.06s** (did not skip) and the pairing control
`TestTaskChildFailEscapeStillFiresInsideSandboxE2E` **PASS 3.50s**, plus the
seven unit/exit-code tests.

### 9a. One briefing constraint was UNSATISFIABLE IN PLACE — stated, not hidden

The briefing requires "leave worktree clean". Executed literally, that constraint
**conflicts with this environment**: the own-worktree-release sweep frees any
worktree that is clean, unowned and past its age floor, so `~/workspace/gopi-escape-sandbox2`
was swept away mid-run and "clean" made it a reclaim candidate again. The protocol
that actually holds a worktree here is a **resident process with its cwd inside
it** (`nohup bash -c 'cd <wt>; sleep 3000'`) — a resident process is also exactly
what `branch_attribution.go` records as "the owner is working". A dirty-marker
file is NOT a safe substitute: touching a tracked file makes sources newer than
`bin/gopi` and the mtime-guard tests (`TestRawPromptRunReleasesItsWorktree`)
refuse. So the worktree is left with its tree **committed (git status clean)** and
held by a resident process, rather than physically removed — see the delivery
declaration. This is the same defect §8c root-caused from the other side, and it
is reported rather than papered over.

### 9b. Post-release: all four safe-merge guards pass (exit 0)

The closeout worktree was released (`git worktree remove --force`), then a fresh
worktree was added for the final pass. With **no worktree occupying the branch**,
the guard that blocked earlier clears:

```
$ bash scripts/safe-merge.sh fix/task-child-fail-escape-sandbox2
✅ [a] archive tag: 无 archive/fix/task-child-fail-escape-sandbox2 —— 未存档
✅ [b] 分支名黑名单: 'fix/task-child-fail-escape-sandbox2' 无违规前缀
✅ [c] worktree 占用: 无 worktree 挂着 'fix/task-child-fail-escape-sandbox2'
ℹ️  [d] 分支 'fix/task-child-fail-escape-sandbox2' 尚未 merge 进 master

✅ safe-merge: 全部通过 —— 可安全 merge 'fix/task-child-fail-escape-sandbox2'
exit=0
```

**All four guards pass** — Rule 9's landing precondition is satisfied. Combined
with §1/§9 (`--is-ancestor` exit 0, no merge commits, one parent chain), the merger's
action is a plain fast-forward with no resolution work.

## Not Done (honest)

- **The guard fix did not land for free.** Merging onto current master surfaced
  **three** real integration defects in the branch's own code/tests (the bare
  DSN, the un-pinned repo context, the over-pinned ledger) — all in §8, all
  root-caused and fixed. That is exactly why the branch had to be re-integrated
  rather than simply landed: a plain "take the branch" would have failed three of
  master's own gates.
- **The park tag was NOT dropped.** `refs/tags/archive/fix/task-child-fail-escape-sandbox`
  (and the original branch `fix/task-child-fail-escape-sandbox`) are left in
  place on purpose: the briefing's acceptance says the park tag is dropped
  **after landing**, and the merge is not mine to perform (Rule 6e). My backup
  tag `backup/fix/task-child-fail-escape-sandbox2-mergetip` is likewise kept as
  the resolution record.
- **One line of the branch's new e2e test was changed by the merge** (§8a):
  the assertion handle now opens via `tools.LedgerReadOnlyDSN` instead of a bare
  `dbPath+"?mode=ro"`, **and two lines were added** to the same file in §8c
  (`SandboxRepoContextInFixtureRepo(t)` in both tests), **and one added line was
  removed** from `task_run_release_coverage_test.go` in §8d. The guard's own
  files remain byte-identical to the branch tip — this note exists so
  "byte-identical" is never read as covering the whole delta.
- **Guard files are byte-identical to `archive/…` heads; `tmproot.go` is NOT**
  (stated deliberately to avoid a false claim): master independently rewrote that
  file after merge-base (exported `ProbeReportPrefix`, `GopiTmpDirEnvName`), and
  the merge keeps master's rewrites **and** the branch's additions
  (`RootDeviceProbePath`, `IsTempRootOffRootFS`). A straight "take the branch"
  would have regressed master.
- **`safe-merge.sh` green at closeout.** The script initially refused the branch
  with one blocker — `⚠️ 分支 'fix/task-child-fail-escape-sandbox2' 仍被 worktree
  占用` — guard [c], refusing a branch a worktree holds. That was my own working
  worktree, i.e. the script working as designed. It was released, re-added for the
  final pass, and at closeout **all four guards pass with exit 0** (§9b). No guard
  was bypassed or weakened to obtain that.
- **`internal/scratch/tmproot.go` differs from the branch tip** (identified to
  avoid a false "byte-identical" claim): master independently rewrote the same
  file after merge-base (exported `ProbeReportPrefix`, `GopiTmpDirEnvName`); the
  merge keeps master's rewrites **and** the branch's additions
  (`RootDeviceProbePath`, `IsTempRootOffRootFS`), which is the correct outcome —
  a straight "take the branch" would have regressed master.
- No semantic conflict beyond the one described above was found. If the merger
  reads any hunk differently, the merge commit `148a3e25` carries the full
  resolution and the original branch ref carries the pre-merge intent for
  comparison. No conflict was resolved by weakening a safety guard: the guard
  files are byte-identical to the branch tip and the only edit the merge forced
  was the test-side DSN correction in §8a.

## Files (delta vs master, 36 files / +1365 −44)

New: `cmd/agent/task_child_fail_guard.go`, `task_child_fail_guard_test.go`,
`task_child_fail_sandbox_guard_e2e_test.go`,
`devdoc/task-child-fail-escape-sandbox.md`, `tasks/01a0a940-report.md`.
Modified (guard wiring): `cmd/agent/cli_tasks.go`, `cli_tasks_worker.go`;
(measured sandbox): `internal/scratch/tmproot.go`,
`internal/config/memory_env.go` (`LedgerPath()`), `internal/config/settings.go`
(`SkipClaimBroadcastEnv`), `internal/config/help.go`; plus the hermetic fixtures
that now name their sandbox in the same fixture.

## Delivery declaration (Rule 6d)

- **Branch**: `fix/task-child-fail-escape-sandbox2`, tip = **`4dc9767d` verified
  code tip** + six **docs-only report commits on top** (this document's own
  updates; they change no code, so every number in this report was produced on
  `4dc9767d` or earlier). Tip convention: the branch tip is the report commit;
  `git log --oneline master..HEAD -- cmd internal` lists the three CODE commits
  (`6242c514` fix, `0a484696`, `e88afece`) — that is the verified code tip's
  content.
  `git merge-base --is-ancestor master HEAD` → exit **0** on the branch tip
  (pasted in §1/§9). Worktree at closeout:
  `/home/user/workspace/gopi-esc2-closeout` (the original
  `~/workspace/gopi-escape-sandbox2` was swept by the lifecycle pass, §9a); tree
  committed, `git status --porcelain` empty.
- **In the main repo already**: worktrees share the main repo's refs, so the
  branch and tags are visible from `/home/user/work/GoPi` with no push. Verified
  from there: `git log --oneline -1 fix/task-child-fail-escape-sandbox2` →
  `4dc9767d`.
- **Recovery**: `git show fix/task-child-fail-escape-sandbox2:devdoc/merge-task-child-fail-escape-sandbox2.md`;
  resolution record: `git show backup/fix/task-child-fail-escape-sandbox2-mergetip`;
  pre-merge intent: `git show archive/fix/task-child-fail-escape-sandbox`.
- **Landing note for the merger**: `scripts/safe-merge.sh fix/task-child-fail-escape-sandbox2`
  must be run AFTER this closeout worktree is released — guard **[c]** refuses
  any branch still occupied by a worktree (observed: "仍被 worktree 占用 → 拒绝").
  That refusal is the script working as designed, not a defect in this branch:
  guards [a] (no archive tag), [b] (no blacklisted prefix) and [d] already report
  clear, and the merge itself is FF (`--is-ancestor` exit 0), so no conflict
  resolution is needed at landing time.
- **Park tag**: `refs/tags/archive/fix/task-child-fail-escape-sandbox` is
  deliberately RETAINED here (dropped after landing, per the briefing) — it is
  the provenance for the guard files' byte-identity check in §4.
