<!-- doc-class: historical -->
# Task Report: master RED — internal/testutil's three gates (worker-ownership / test budget / scratch pin)

Task: `01a0a941` (resubmission of burned row `01a0a92d-21a5-76d4-acdb-404ffebbdca7`).
Branch: `fix/testutil-triple-20260916`, worktree `~/workspace/gopi-testutil-triple`, based on master **0dac2ae7**,
later replayed onto **52d649cc** (see the re-verification section).

## Completed

Measured on a FRESH worktree off master `0dac2ae7` (not the shared main checkout), before any fix:

```
--- FAIL: TestWorkerOwnershipPointerPresent            worker_ownership_gate_test.go:365: 7 uncovered file(s)
--- FAIL: TestTestBudgetHasASingleSourceAndNamesATimeout  testbudget_gate_test.go:103: scripts/accept-daemon-scope.sh:105 runs `go test` with no `-timeout`
--- FAIL: TestPinCoverageTable                          pin_coverage_test.go:95: internal/taskreport  yes yes -  NO-PIN
```

**Correction to the briefing's count (measured, not assumed):** the gate lists **7** tracked files, not 8.
The 8th named file, `devdoc/verify-01a0a72c-merger-stop-immediate.md`, is **untracked session residue in the
shared main checkout** (`git status ??`, absent from `git log --all -- <path>`, a no-op line 2×). A clean
worktree — and CI — sees 7. Confirmed by the same gate run before and after the fix.

### 1. TestWorkerOwnershipPointerPresent — 7 files annotated (additive only)

The canonical pointer line inserted at line 2 of each offender; no gate edit, no `workerOwnershipDismissed`
entry added (the gate's dismissal table is unchanged):

| file | line | form |
|---|---|---|
| devdoc/daemon-scope-per-profile.md | 2 | `<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->` |
| devdoc/nested-report-path-clobber.md | 2 | idem |
| devdoc/why-worktree-residue.md | 2 | idem |
| devdoc/worker-idle-drained-stop.md | 2 | idem |
| devdoc/worktree-reap-reachable.md | 2 | idem |
| devdoc/worktree-refusal-arms.md | 2 | idem |
| scripts/accept-daemon-scope.sh | 2 | `# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md` |

### 2. TestTestBudgetHasASingleSourceAndNamesATimeout — declared budget, flag on the invoking line

`scripts/accept-daemon-scope.sh`: the budget is declared ONCE at line 37 with one env reader
(`acc_go_timeout="${GOPI_TEST_TIMEOUT:-25m}"`, reusing `scripts/test-budget.sh`'s `test_budget` value), and
`-timeout "$acc_go_timeout"` is placed on the **same physical line** as the `go test` invocation (line 121) —
required, because the gate's predicate (`budgetGoTestRe`) is line-anchored and a continuation-line flag is
invisible to it. No exemption entry was added (`budgetExemptScripts` untouched).

### 3. TestPinCoverageTable — internal/taskreport scratch pin (REAL DEFECT, not a permit)

`internal/taskreport/own_test.go` builds fixtures with `t.TempDir()` at two sites and had no `TestMain`, so its
scratch landed on the ambient temp root — the boot partition `/` (Rule 22). Fixed by adding
`internal/taskreport/testmain_scratchroot_test.go` routing through `testutil.RunSuiteMain(m, "taskreport")`.
The coverage row moves `internal/taskreport  yes yes - NO-PIN` → `PIN PIN`; pinned count 27 → 28.

### 4. (Same family, found by the full-package run) TestScriptsAreBoundedAndOffTmp

`devdoc/loop-proof-merge-rebuild.sh` created scratch with a hand-chosen `mktemp -d "${GOPI_LOOP_PROOF_ROOT:-/dev/shm}/loop-proof-XXXXXX"`.
Fixed by sourcing `scripts/lib/tmp-ban.sh` and taking the directory from `gopi_scratch_mkdir loop-proof` +
`trap gopi_scratch_cleanup` (the helper is what measures the path against `/`), with a hard refusal if the
helper is missing. The briefing's three gates do not reach this one; it is listed here because a green
`internal/testutil` package requires it, and the sibling branch `fix/loop-proof-script-scratch` (f478baed)
touches the same file and was probed first.

## Branch provenance (briefing asked which candidates were relied on)

| branch | state | what was taken / left |
|---|---|---|
| `fix/testutil-gates-green` (fc1013bd) | ahead 1, unmerged | **Relied on as the base content** for all four fixes, then re-verified on the current tip. Its work covered gates 1–4 verbatim; no part was missing except the 8th-file count, which is an artifact of the main checkout, not of the branch. |
| `fix/worker-ownership-pointer-backfill-3` (58499911) | ahead 2, unmerged | Read (Rule 23). Redundant for this task: it annotates 3 files we do not need and buys its 7th file via a `workerOwnershipDismissed` entry + a 38-line gate edit — the weaker remedy; not adopted. |
| `fix/standards-rekey-2026-09-16` (7d92616e) | ahead 2, unmerged | Out of scope by the briefing and left untouched. NOT merged into master at the time of writing, so this branch's tip is 0dac2ae7 and no line-number interaction exists. |
| `fix/loop-proof-script-scratch` (f478baed) | unmerged | Its `tmp-ban.sh` adoption for `devdoc/loop-proof-merge-rebuild.sh` is byte-identical to what landed here, so the two merge as a no-op. |

## Red proofs (per gate: minimal violation planted → gate RED naming the site → violation removed → gate GREEN)

Each proof is the real gate run against a planted violation at the fix site; recovery verified with
`git status`/`grep` afterwards.

**Gate 1** — removed the pointer line from `devdoc/why-worktree-residue.md`:

```
--- FAIL: TestWorkerOwnershipPointerPresent (0.07s)
    worker_ownership_gate_test.go:365: 1 uncovered file(s) … devdoc/why-worktree-residue.md
```
restored → `ok github.com/nick/gopi/internal/testutil 0.877s`

**Gate 2** — stripped ` -timeout "$acc_go_timeout"` from the invoking line:

```
--- FAIL: TestTestBudgetHasASingleSourceAndNamesATimeout (0.02s)
    testbudget_gate_test.go:103: scripts/accept-daemon-scope.sh:121 runs `go test` with no `-timeout` …
```
restored → `ok … 1.080s`

**Gate 3** — removed `internal/taskreport/testmain_scratchroot_test.go`:

```
--- FAIL: TestPinCoverageTable (0.14s)
    pin_coverage_test.go:95: internal/taskreport   yes  yes  -  NO-PIN
```
restored → `ok … 1.418s`

**Gate 4** — restored master's hand-chosen `mktemp` script over the fixed one:

```
--- FAIL: TestScriptsAreBoundedAndOffTmp (3.71s)
    scripts_gate_test.go:218: devdoc/loop-proof-merge-rebuild.sh creates scratch data but does not source/import scripts/lib/tmp-ban.sh …
```
restored → `ok … 3.543s`

## Files changed

```
 devdoc/daemon-scope-per-profile.md               |  1 +
 devdoc/loop-proof-merge-rebuild.sh               | 17 ++++++++++++----
 devdoc/nested-report-path-clobber.md             |  1 +
 devdoc/testutil-triple-green-20260916.md         | (this report)
 devdoc/why-worktree-residue.md                   |  1 +
 devdoc/worker-idle-drained-stop.md               |  1 +
 devdoc/worktree-reap-reachable.md                |  1 +
 devdoc/worktree-refusal-arms.md                  |  1 +
 internal/taskreport/testmain_scratchroot_test.go | 21 ++++++++++++++++++++
 scripts/accept-daemon-scope.sh                   | 25 ++++++++++++++++++++++--
```
No file under `internal/testutil/` is modified — every gate is byte-unchanged, so nothing was weakened.

## Not Done (honest)

- The **8th file** (`devdoc/verify-01a0a72c-merger-stop-immediate.md`) was deliberately NOT committed: it is
  untracked session residue in the shared main checkout, not a repo file. If it is ever committed, the gate
  will flag it again — that is correct behaviour, not a gap in this fix.
- `internal/standards` (Rules 16/17/18) and the `Rule 17` sites in `internal/agent/journalreport_test.go`
  remain RED on master and are owned by `fix/standards-rekey-2026-09-16`; not touched here.
- The full tree (`go test ./...`) was NOT run to completion — it exceeds the per-package budget on a loaded
  host; verification is scoped to `internal/testutil` + `internal/taskreport` + `go vet` + `go build`, which
  is what this task's gates require.

## Verification

- `go vet ./...` — exit 0 (no output)
- `CGO_ENABLED=0 go build ./...` — exit 0
- `go test ./internal/testutil/ -count=1` — see the run recorded at the bottom of this report
- `go test ./internal/taskreport/ -count=1` — ok
- Targeted: `go test ./internal/testutil/ -count=1 -run 'TestWorkerOwnershipPointerPresent|TestTestBudgetHasASingleSourceAndNamesATimeout|TestPinCoverageTable|TestScriptsAreBoundedAndOffTmp|TestEveryTempUsingSuiteIsPinned'` → `ok github.com/nick/gopi/internal/testutil 3.040s`
- Commit: see the branch tip of `fix/testutil-triple-20260916`

## Re-verification after master advanced (acceptance: "on the tip you work from")

Master moved during the work: `0dac2ae7` → `52d649cc` (three docs-only gui-commits:
`d54c9822`, `bd2fbd1e`, `52d649cc`, touching `devdoc/gui-chat-roadmap.md` only). Branch
replayed onto the new tip (see `git log -1` for the hash; `git rebase master`, clean, no conflicts;
`git diff --stat 0dac2ae7..master` over this task's paths is EMPTY, so no interaction existed).

The misleading diffstat seen before the rebase (`devdoc/gui-chat-roadmap.md | 510 ---`) was
master's OWN new file absent from the older merge-base — an artifact of comparing against a
stale master, not a deletion by this branch. Confirmed with `git ls-tree master|HEAD`.

Re-run on the rebased tip (`git log -1 --format=%h` in the branch; master `52d649cc`):

```
build : CGO_ENABLED=0 go build ./...                     exit 0
vet   : CGO_ENABLED=0 go vet ./...                       exit 0 (no output)
gate1 : --- PASS: TestWorkerOwnershipPointerPresent
gate2 : --- PASS: TestTestBudgetHasASingleSourceAndNamesATimeout
gate3 : --- PASS: TestPinCoverageTable (+ TestPinCoverageTableIsNotVacuous)
        ok github.com/nick/gopi/internal/testutil 2.001s
        ok github.com/nick/gopi/internal/taskreport 2.617s
```

Red-proofs re-executed on the rebased tip (plant → RED at the site → restore → GREEN, worktree
clean between and after; `git status --porcelain` empty, tip unchanged at `9814bca2`):

```
gate1  drop line 2 of devdoc/why-worktree-residue.md
       → FAIL worker_ownership_gate_test.go:365: 1 uncovered file(s)…   restore → ok
gate2  strip ` -timeout "$acc_go_timeout"` off scripts/accept-daemon-scope.sh:121
       → FAIL testbudget_gate_test.go:103: scripts/accept-daemon-scope.sh:121 runs `go test` with no `-timeout`   restore → ok
gate3  remove internal/taskreport/testmain_scratchroot_test.go
       → FAIL pin_coverage_test.go:92: internal/taskreport creates scratch (t.TempDir()) but no TestMain…
         pin_coverage_test.go:95: internal/taskreport … -  NO-PIN                restore → ok
```

`fix/standards-rekey-2026-09-16` (the sibling row's Rule 16/17/18 work) is still UNMERGED at
`52d649cc`, so per the briefing's conditional it stays out of scope — no line-number
interaction with `internal/agent/journalreport_test.go` exists yet. If it merges later, this
branch touches no file it touches.

## Replay onto master `b63e223b` (master advanced twice WHILE this task ran)

master moved `0dac2ae7` → `52d649cc` → `6d2ce5cf` → **`b63e223b`** during this task. Each replay was
re-measured in a fresh detached worktree before touching anything:

- `95d6221d` (merge of `52d649cc`): the three gates were GREEN on that tip for ~13 minutes.
- master `6d2ce5cf` (`Merge branch 'feat/child-stream-task-output'`) **re-broke gate 1**: its new
  `devdoc/child-stream-task-output.md` teaches a worker-launch command and carries no pointer line —
  so the gate reported **8 uncovered files again, with a genuinely TRACKED 8th** (the earlier 8th,
  `devdoc/verify-01a0a72c-merger-stop-immediate.md`, is untracked main-checkout residue and is NOT the one).
- tip now: `95942a19` (merge of `b63e223b`) + `89f9913f` = **`89f9913f`** — the pointer line added to
  `devdoc/child-stream-task-output.md`, and nothing else.
- `fix/loop-proof-script-scratch` (f478baed) — the sibling branch this branch previously probed — is now
  MERGED, so section 4's `scripts/gopi-worker-up.sh` / `loop-proof` interaction is resolved in master.

Re-verified acceptance on the FINAL tip `89f9913f` (full output in the job log, §Verification below):

```
the three named gates : --- PASS ×3   ok internal/testutil 3.980s
full internal/testutil : ok   (PKG_EXIT=0)
internal/taskreport    : ok
build                  : CGO_ENABLED=0 go build ./cmd/agent/  BUILD_OK
vet                    : go vet ./...  VET_EXIT=0 (no output)
```

Red-proofs on `89f9913f` (plant → RED naming the exact site → restore → GREEN, `git status --porcelain` empty):

```
gate1  drop the pointer line from devdoc/child-stream-task-output.md
       → FAIL worker_ownership_gate_test.go:365: 1 uncovered file(s)…              restore → ok
gate2  strip ` -timeout "$acc_go_timeout"` off scripts/accept-daemon-scope.sh:121
       → FAIL testbudget_gate_test.go:103: scripts/accept-daemon-scope.sh:121 runs `go test` with no `-timeout`   restore → ok
gate3  remove internal/taskreport/testmain_scratchroot_test.go
       → FAIL pin_coverage_test.go:92: internal/taskreport creates scratch (t.TempDir()) but no TestMain…
         pin_coverage_test.go:95: internal/taskreport  yes yes -  NO-PIN         restore → ok
```

**Process lesson (recorder trap, cost one red-proof cycle):** while `git checkout -- <file>` is a fine
restore for a COMMITTED file, it silently discards an *uncommitted* edit — it reverted the very pointer
line the red-proof had just restored. Plant the violation in a file whose state you do not also need to
keep, or commit before the restore.

## Replay onto master `a9fee0d1` (a fourth advance, after the previous section was written)

master moved again: `b63e223b` → … → `a068d0d9` → **`a9fee0d1`** (merges `fix/repo-context-gate-reverify`,
`fix/rule17-journalreport-env-escape`, `fix/signalrec-verify-attribution`, `fix/stale-merge-audit-20260916`).
Measured on `a068d0d9` in a fresh detached worktree BEFORE replaying: gate 1 reported **11 uncovered files**
— the four new merges each landed an operator-facing devdoc that teaches a worker-launch / task-run shape
without the pointer. This is the same mechanism as the `6d2ce5cf` replay above, and it is the reason the
measure-before-replay step is not optional: **a green triple is not durable while master moves**, and each
merge round can add offenders faster than a branch can be merged.

Replay `ce2a416c` (merge of `a9fee0d1`) adds the pointer line, and nothing else, to the four files that
survived from that round once this branch's earlier pointer lines are merged:

| file | line |
|---|---|
| devdoc/cmdagent-repo-context-sandbox.md | 3 |
| devdoc/repo-context-gate-reverify-report.md | 3 |
| devdoc/repo-context-gate-reverify.md | 3 |
| devdoc/stale-merge-audit-20260916.md | 3 |

(On `a068d0d9` the raw count was 11 because this branch's earlier pointer lines are not merged yet; the
replay commit's own scope is the 4 files named above.)

Acceptance re-verified on `ce2a416c`:

```
the three named gates : --- PASS ×3   ok internal/testutil 3.603s
internal/taskreport    : ok
vet                    : go vet ./...   exit 0 (no output)
build                  : exit 0
```

Red-proofs re-executed on `ce2a416c` (plant → RED naming the exact site → restore → GREEN):

```
gate1  drop the pointer line from devdoc/stale-merge-audit-20260916.md
       → FAIL worker_ownership_gate_test.go:365: 1 uncovered file(s)… devdoc/stale-merge-audit-20260916.md   restore → ok
gate2  strip ` -timeout "$acc_go_timeout"` off scripts/accept-daemon-scope.sh:121
       → FAIL testbudget_gate_test.go:103: scripts/accept-daemon-scope.sh:121 runs `go test` with no `-timeout`   restore → ok
gate3  remove internal/taskreport/testmain_scratchroot_test.go
       → FAIL pin_coverage_test.go:92 + :95: internal/taskreport … -  NO-PIN                                restore → ok
```

`fix/standards-rekey-2026-09-16` remains UNMERGED, so Rules 16/17/18 stay out of scope, as briefed.

## Replay #4 onto master `b857a321` (measured on the raw master tip, then merged)

master advanced once more to **`b857a321`** = `Merge branch 'fix/testutil-gates-green'` — i.e. *another worker's*
attempt at this same triple. Measured **on the raw master tip first**, in a detached worktree at
`~/workspace/tt-check-b857`, before merging anything:

```
FAIL TestWorkerOwnershipPointerPresent (0.42s)
  worker_ownership_gate_test.go:365: 5 uncovered file(s) teach a worker-launch / task-run command without the operator-only pointer; listing all 5:
        devdoc/child-stream-task-output.md
        devdoc/cmdagent-repo-context-sandbox.md
        devdoc/repo-context-gate-reverify-report.md
        devdoc/repo-context-gate-reverify.md
        devdoc/stale-merge-audit-20260916.md
```

The sibling branch landed the same class of fix for the files it saw, but `devdoc/child-stream-task-output.md`
(the `b63e223b` offender) still reddened — because that file's pointer line lives only on THIS branch
(`89f9913f`), and this branch is not merged. Master's gate 1 therefore re-reddens after every merge round that
lands an operator-facing devdoc: measured offender counts across this task are **7 → 8 → 11 → 5**.

Merged `b857a321` into the branch (`a4e266f3`); all five files carry exactly one pointer line, so the merged
tip is green with no new annotation needed:

```
$ git grep -c 'WORKER-OWNERSHIP: OPERATOR-ONLY' -- <each of the five>   # → 1 each
```

Acceptance on `a4e266f3` (this run):

```
three named gates + TestPinCoverageTableIsNotVacuous : ok  internal/testutil  3.677s
full internal/testutil package                       : job-8 (background, -count=1)
internal/taskreport                                  : ok
build                                                : CGO_ENABLED=0 go build ./cmd/agent/  → BUILD_OK
vet                                                  : go vet ./...  → exit 0, no output
```

Red-proofs re-executed on `a4e266f3` (plant → RED naming the exact site → restore → GREEN; the worktree is
`git status` clean afterwards):

```
gate1  drop the pointer line from devdoc/stale-merge-audit-20260916.md
       → FAIL worker_ownership_gate_test.go:365: 1 uncovered file(s)…   restore → ok
gate2  strip ` -timeout "$acc_go_timeout"` off scripts/accept-daemon-scope.sh:121
       → FAIL testbudget_gate_test.go:103: scripts/accept-daemon-scope.sh:121 runs `go test` with no `-timeout`…   restore → ok
gate3  remove internal/taskreport/testmain_scratchroot_test.go
       → FAIL pin_coverage_test.go:92: internal/taskreport creates scratch (t.TempDir()) but no TestMain routes the pin…
         pin_coverage_test.go:95: internal/taskreport  yes yes -  NO-PIN      restore → ok
```

### The durability finding (the actionable one)

**A green triple is not durable while master moves.** Four replays in one task produced the same pattern: each
merge round adds tracked devdocs that teach a worker-launch / task-run shape, and gate 1's offender set is
whatever the current tip contains. The fix belongs on master, not on a branch: until
`fix/testutil-triple-20260916` is merged, the next merge round re-reddens master. The pointer annotations are
additive and conflict-free (one line at the top of each file), so the merge is safe; the red-proofs above
show the gates are unweakened (`internal/testutil/` is byte-unchanged on this branch — `git diff master..HEAD --
internal/testutil/` is empty).

`fix/standards-rekey-2026-09-16` remains UNMERGED (verified on `b857a321`: `fix/rule17-journalreport-escape`
did merge, `fix/standards-rekey-2026-09-16` at `7d92616e` did not), so Rules 16/17/18 stay out of scope, as
briefed.

## Replay #5 onto master `09360524` — a FOURTH gate red (scripts), and a merge that tried to revert this fix

Measured on the RAW master tip first (detached worktree, `TMPDIR=/dev/shm/gopi-test-scratch`):

```
master 09360524 (resolve merge fix/rule17-journalreport-escape into master)
  TestWorkerOwnershipPointerPresent           FAIL  worker_ownership_gate_test.go:365: 5 uncovered file(s)
  TestTestBudgetHasASingleSourceAndNamesATimeout  PASS
  TestPinCoverageTable                        PASS
  TestScriptsAreBoundedAndOffTmp              FAIL  scripts_gate_test.go:214 × 3:
        scripts/attribute-victim.sh:12  unbounded loop "while :; do"
        scripts/killer-watch.sh:10      unbounded loop "while :; do"
        scripts/tripwire-victim.sh:17   unbounded loop "while :; do"
```

**The fourth red is a real, tracked, nobody's-branch defect** (briefing scope was "the triple"; the full
package run is what surfaced it — the same way item 4 surfaced in the original round). The three scripts came
from `fd5a9d6b docs(cmd/agent): report the repo-context sandbox; add the attribution scripts` and are
byte-identical to master. No branch owns them: `git diff --name-only master...<branch>` over all branches
returns zero hits.

**Why the gate is right and the loop must be bounded.** These are forensic watchers (`while :; do … sleep
0.02; done`) started in the background to timestamp a victim's disappearance. Their spin condition is
"real work", so a static scan cannot prove they end — and the 2026-09-11 exhaustion was exactly one such
loop. A watcher that is never killed (an interrupted rig, a lost `trap`) spins forever at 50 polls/s.

**Fix** (`scripts/attribute-victim.sh`, `scripts/killer-watch.sh`, `scripts/tripwire-victim.sh`) — each
gains a declared wall-clock cap plus a named poll interval:

```bash
WATCH_CAP_SEC=1800            # bash's own SECONDS counter bounds the loop
WATCH_POLL_SEC=0.02
while :; do
  [ "$SECONDS" -ge "$WATCH_CAP_SEC" ] && break
  …work…
  sleep "$WATCH_POLL_SEC"
done
```

The cap sits on the FIRST line of the body deliberately: the gate's `loopBodyIsBounded` scans from the
header with a 40-line lookahead and returns false at the loop's closing `done`, so a bound placed after a
>40-line body would still be reported. `bash -n` clean on all three.

**A reading I first got wrong, recorded so the next reader does not repeat it.** `git diff --name-status
HEAD..master` on this branch printed `D devdoc/testutil-triple-green-20260916.md` and three `M` entries on the
scripts, which reads as "master deleted this task's report and reverted the fix". It does not. `HEAD..master`
diffs *my branch → master*, so `D` means "the file is not in master" and `M` means "master's version differs
from mine" — both true, and neither is an edit master made. The three questions that settle it:

```
git log --oneline master -- devdoc/testutil-triple-green-20260916.md      -> empty (never on master)
git diff --stat b857a321..master -- scripts/{attribute,killer-watch,tripwire-victim}.sh -> empty (untouched)
git diff --name-status db0b231a^1 db0b231a                                -> A briefings/…, M devdoc/rule17…
```

The merge's real content is the last line: master's own two files. The report was never on master (this branch
is the only holder), and master never touched the three scripts — so nothing was reverted and nothing was
deleted. The durability hazard of §Replay #4 is unchanged and needs no second instance: an unmerged branch
green is still not master green.

**Acceptance on the merged tip `db0b231a`** (master `09360524` + this branch's fixes):

```
the five gates (3 named + IsNotVacuous + scripts) : PASS ×5, ok internal/testutil 4.678s
build                                             : BUILD_OK
vet                                               : go vet ./... exit 0
```

**Four red-proofs on `db0b231a`** (plant → FAIL naming the exact site → restore → green, worktree clean):

```
gate1  drop the pointer line from devdoc/child-stream-task-output.md
       → FAIL worker_ownership_gate_test.go:365: 1 uncovered file(s)…                        restore → ok
gate2  strip ` -timeout "$acc_go_timeout"` off scripts/accept-daemon-scope.sh:121
       → FAIL testbudget_gate_test.go:103: scripts/accept-daemon-scope.sh:121 runs `go test` with no `-timeout`  restore → ok
gate3  remove internal/taskreport/testmain_scratchroot_test.go
       → FAIL pin_coverage_test.go:92/:95: internal/taskreport … NO-PIN                       restore → ok
gate4  delete the `[ "$SECONDS" -ge "$WATCH_CAP_SEC" ] && break` line from scripts/killer-watch.sh
       → FAIL scripts_gate_test.go:214: scripts/killer-watch.sh:14 unbounded loop "while :; do"  restore → ok
```

### Process lesson #2: a plant that silently fails to apply is a FALSE GREEN

Gate 4's red-proof was run twice and was INVALID both times: the plant used
`grep -c 'WATCH_CAP_SEC" -ge'` / `sed '/WATCH_CAP_SEC" -ge/d'`, and the real line reads
`[ "$SECONDS" -ge "$WATCH_CAP_SEC" ]`. The anchor never matched → the plant was a no-op → the gate stayed
green → the "ok" was line 214's ABSENCE, not the gate passing. The effective proof must (a) assert the
plant changed the file (`before=1 after=0`) and (b) keep the planted file parsable (`bash -n`), so the
failure can only come from the rule the plant violated. Re-run that way: FAIL at
`scripts_gate_test.go:214` naming `killer-watch.sh:14`, then restore → `ok`.

## Replay #6 onto master `1817dd20` — master fixed gate 4 ITSELF; this branch defers to master on those three scripts

While this task ran, a **different** worker landed the same gate-4 fix on master
(`1817dd20 fix(scripts): bound the three worktree-killer monitors by wall clock`). Their bound is the better
one — env-overridable (`WATCH_MAX_SECS="${WATCH_MAX_SECS:-3600}"` compared against `$SECONDS`) — so the merge
**conflicted by design** and was resolved by taking MASTER's three scripts over mine:

```
git merge master            -> CONFLICT (content): scripts/killer-watch.sh
git checkout master -- scripts/{killer-watch,tripwire-victim,attribute-victim}.sh
git commit                  -> 5fe6bcc9 Merge branch 'master' into fix/testutil-triple-20260916
```

The two bounds are equivalent in effect and were written independently (mine caps on bash's global
`SECONDS`; theirs stores a start offset). What remains OURS on this branch is the part master still lacks:

```
P1  pointer lines for the current gate-1 offender set   (master 1817dd20: 5 uncovered files)
P2  the declared test budget on scripts/accept-daemon-scope.sh:121
P3  internal/taskreport/testmain_scratchroot_test.go (the Rule 22 scratch pin)
    + this report
```

### Process lesson #3: a claim about who did what is worth only the command that measures it

The first draft of this section said "master deleted my report and reverted the scripts", read off
`git diff --name-status HEAD..master`. That diff reports *differences*, not *edits*: `D devdoc/…md` meant
only "the file is not on master" (it never was — this branch is its sole holder), and the `M` rows meant
"master's copy differs from mine". Settle provenance with commands that cannot be read backwards:

```
git log --oneline master -- devdoc/testutil-triple-green-20260916.md      -> empty (never on master)
git diff --stat b857a321..master -- scripts/{attribute,killer-watch,tripwire-victim}.sh -> empty (untouched)
git diff --name-status db0b231a^1 db0b231a                                -> A briefings/…, M devdoc/rule17…  (its real content)
```

### What this branch uniquely adds on master `1817dd20` (measured)

Master has moved under this task so often that three of the four fixes are now ON master already — two of
them from the pointer backfills the briefing predicted (`fix/worker-ownership-pointer-backfill-3`,
`fix/testutil-gates-green`) and gate 4 from an independent worker (`1817dd20`). Verified per gate:

```
gate 2  git show master:scripts/accept-daemon-scope.sh | grep acc_go_timeout
        -> line 37 acc_go_timeout="${GOPI_TEST_TIMEOUT:-25m}"   and line 121 go test … -timeout "$acc_go_timeout"   ON MASTER
gate 3  git cat-file -e master:internal/taskreport/testmain_scratchroot_test.go -> present                        ON MASTER
gate 4  git show master:scripts/killer-watch.sh | grep WATCH_MAX_SECS -> bound present                            ON MASTER (1817dd20)
gate 1  go test -run TestWorkerOwnershipPointerPresent on a detached master worktree
        -> FAIL worker_ownership_gate_test.go:365: 5 uncovered file(s)                                            STILL RED
```

So the branch's whole remaining delta is five pointer lines plus this report:

```
git diff --stat master...HEAD
 devdoc/child-stream-task-output.md           |   2 +
 devdoc/cmdagent-repo-context-sandbox.md      |   2 +
 devdoc/repo-context-gate-reverify-report.md  |   2 +
 devdoc/repo-context-gate-reverify.md         |   2 +
 devdoc/stale-merge-audit-20260916.md         |   2 +
 devdoc/testutil-triple-green-20260916.md     | 482 ++++++++++++++++++++++++++++
 6 files changed, 492 insertions(+)
```

**Gate 1 is the one that cannot be fixed once-and-for-all from a branch** — every merge round adds devdocs
that teach a worker-launch / task-run shape, so the offender set is re-measured at every tip (7 → 8 → 11 →
5 → 5 across this task). Merging this branch closes today's set; the next doc merge re-opens it. A durable
fix would make the gate's rule part of whatever writes those docs (or the docs' own template), which is a
separate task — recorded here as the honest follow-up, not silently assumed.

### Red proofs on the FINAL tip `fa20dedf` (all four valid)

```
P1  drop the pointer line from devdoc/child-stream-task-output.md
    → FAIL worker_ownership_gate_test.go:365: 1 uncovered file(s)…                       restore → ok
P2  strip ` -timeout "$acc_go_timeout"` off scripts/accept-daemon-scope.sh:121
    → FAIL testbudget_gate_test.go:103: scripts/accept-daemon-scope.sh:121 runs `go test` with no `-timeout`   restore → ok
P3  remove internal/taskreport/testmain_scratchroot_test.go
    → FAIL pin_coverage_test.go:92/:95: internal/taskreport … NO-PIN                     restore → ok
P4  delete `if [ $((SECONDS - watch_started)) -ge "$WATCH_MAX_SECS" ]; then break; fi`
    from scripts/killer-watch.sh   (plant asserted: bound lines 1 → 0, `bash -n` still clean)
    → FAIL scripts_gate_test.go:214: scripts/killer-watch.sh:13 unbounded loop "while :; do"   restore → ok
```

Acceptance on `5fe6bcc9` (master's tip `1817dd20` merged in, so the tip CONTAINS the master the gates run
against): the four named gates + `TestPinCoverageTableIsNotVacuous` all PASS,
`ok internal/testutil` (full package 217.165s on the equivalent `b67ff68a` gate set), `internal/taskreport`
ok, `CGO_ENABLED=0 go build ./...` BUILD_OK, `go vet ./...` exit 0, `git status --porcelain` clean after
every plant/restore pair.


## Replay #7 onto master `d5b7686d` — the SEVENTH advance; gate 1's offender set measured on the raw tip first

Method (unchanged from replays #4–#6): **measure the raw master tip in a throwaway detached worktree
BEFORE merging**, so the claim "this is what master really fails on" is a measurement, not a memory.

```
worktree /home/user/workspace/tt-reframe-master @ d5b7686d (raw master, detached)
  --- FAIL: TestWorkerOwnershipPointerPresent (0.47s)
      worker_ownership_gate_test.go:365: 6 uncovered file(s) teach a worker-launch / task-run command
      without the operator-only pointer; listing all 6:
          devdoc/child-stream-task-output.md
          devdoc/cmdagent-repo-context-sandbox.md
          devdoc/repo-context-gate-reverify-report.md
          devdoc/repo-context-gate-reverify.md
          devdoc/stale-merge-audit-20260916.md
          devdoc/worker-provenance-tag.md          <-- NEW this round
  TestTestBudgetHasASingleSourceAndNamesATimeout  ok   (master carries it: scripts/accept-daemon-scope.sh:37/:121)
  TestPinCoverageTable                            ok   (master carries the pin: internal/taskreport/testmain_scratchroot_test.go)
```

Five of the six already carry this branch's pointer line, so after `git merge master` (→ `a639a073`) the
gate reported exactly **1** offender — `devdoc/worker-provenance-tag.md`, which landed on master inside
`d5b7686d` (merge of `fix/worker-provenance-tag`, an independent worker's branch). Pointer line added at
its site (commit `1ec76683`), giving final tip **`1ec76683`**.

### A real hazard hit while annotating this file: `edit`-tool writes to it were reverted by a live process

Two `edit` calls on `devdoc/worker-provenance-tag.md` reported success, the following read showed the file
INTACT and the pointer line ABSENT, and `git diff --stat` eventually showed the file's first lines
REPLACED BY AN EMPTY LINE (`1 insertion(+), 3 deletions(-)` — the header line was gone). Sequence measured:

| attempt | write | observed after |
|---|---|---|
| `edit` (insert after the `Task:` line) | success | pointer line absent, file restored |
| `edit` (insert after the header) | success | pointer line absent, header line *deleted* |
| `cp` backup + `printf >>` append | — | **persisted** (`grep -c` = 1 over repeated polls) |

Mitigation that worked: back the file up, append the pointer line with `printf >>`, keep the working copy
in the backup, and commit immediately. Root cause not proven (a concurrent writer — the running merger
daemon / another session — is the leading hypothesis; this main checkout is shared). Recorded because the
consequence is silent: an `edit` that reports success while the bytes do not survive is a lost fix, and
only `git diff` after the write reveals it. The session report carries the same note.

### Red proofs re-run on the merged tip `1ec76683` (each plant ASSERTED effective before the gate is read)

```
P1 drop the pointer line from devdoc/worker-provenance-tag.md
   plant: pointer lines 1 -> 0 (grep -c asserted)
   → FAIL worker_ownership_gate_test.go:365: 1 uncovered file(s)… devdoc/worker-provenance-tag.md
   restore: git checkout -- devdoc/worker-provenance-tag.md → 1, status clean → gate ok
P2 strip ` -timeout "$acc_go_timeout"` off scripts/accept-daemon-scope.sh:121
   plant: line 121 `go test ./cmd/agent -count=1 -timeout "$acc_go_timeout" \` -> `go test ./cmd/agent -count=1 \`
          (asserted by sed -n 121p + grep -c 1 -> 0, `bash -n` clean so the failure can only be the rule)
   → FAIL testbudget_gate_test.go:103: scripts/accept-daemon-scope.sh:121 runs `go test` with no `-timeout`
   restore: git checkout → grep -c = 1, status clean → gate ok
P3 remove internal/taskreport/testmain_scratchroot_test.go
   plant: file moved aside; `ls` asserted the absence
   → FAIL pin_coverage_test.go:95: internal/taskreport … NO-PIN
   restore: file moved back; status clean → TestPinCoverageTable + IsNotVacuous ok
P4 delete the `WATCH_MAX_SECS` bound line from scripts/killer-watch.sh:14
   plant: bound lines 2 -> 1 (grep -c asserted), `bash -n` clean
   → FAIL scripts_gate_test.go:214: scripts/killer-watch.sh:13 unbounded loop "while :; do"
   restore: git checkout → status clean → ok
```

### Acceptance on the final tip `1ec76683` (contains master `d5b7686d`)

```
gates (3 named + PinCoverageTableIsNotVacuous)  : PASS ×4   ok internal/testutil 3.511s
build                                           : CGO_ENABLED=0 go build ./... → BUILD_OK
vet                                             : go vet ./... → exit 0
internal/testutil vs master (diff --stat)       : EMPTY — gates byte-unchanged, nothing weakened
worktree                                        : git status --porcelain empty after every plant/restore
```

### The durability finding, now measured across SEVEN master advances

Gate-1 offender counts measured on the raw tip of each advance: **7 → 8 → 11 → 5 → 5 → 6 → 6**. Every
merge round that lands a devdoc teaching a worker-launch / task-run shape adds an offender, and this
branch — unmerged — cannot prevent it. Gates 2/3/4 are now carried by master itself (three of the four
fixes were reproduced independently by other workers), so this branch's entire remaining delta on
`d5b7686d` is **the pointer lines for five gate-1 files** plus this report. The durable fix is either
(a) land this additive, conflict-free branch on master, or (b) make the *writers* of those devdocs carry
the pointer/refuse the launch shape by default — (b) is the follow-up recorded in the session report.

## Replay #8 (closing) — master reached `aeaf9ba0` (Rule 28 commit); final tip `3bc4761e`

Master advanced to `9b069869` (the standards-rekey merge) and then `aeaf9ba0` while this report was being
written. Re-measured on the raw tip of each: gate 1 still `6 uncovered file(s)` — the SAME six names as
replay #7 (`worker-provenance-tag.md` included), so this branch's pointer lines cover the whole set again
with zero new annotation. Gates 2/3 PASS on master.

Final tip **`3bc4761e`** (merge of master `aeaf9ba0`; `git merge-base --is-ancestor master HEAD` → true).

### Acceptance on `3bc4761e`

```
gates (3 named + TestPinCoverageTableIsNotVacuous) : PASS ×4, ok internal/testutil 2.925s
FULL internal/testutil package                     : ok internal/testutil 204.855s, ZERO --- FAIL (job-18)
                                                      (all 10 worker-ownership/pointer/pin/budget tests PASS)
build                                              : CGO_ENABLED=0 go build ./... → BUILD_OK
vet                                                : go vet ./... → exit 0
internal/testutil vs master (git diff --stat)      : EMPTY for internal/testutil/ — gates byte-unchanged
branch delta vs master                             : 6 pointer lines (2 lines each) + this report = 7 files
worktree                                           : git status --porcelain clean
```

### One full-package run was a FALSE RED — and it is a pre-existing scratch-GC flake, not this delta

job-17 (`go test ./internal/testutil/ -count=1`) exited FAIL 78.5s with exactly one line:

```
scratch: GC could not remove /dev/shm/gopi-test-scratch/suite-internal-testutil-run-1014047812/
  TestGCUnreadableScratchEntryDoesNotPanic…/workspace/gopi-test-scratch/suite-locked: permission denied
```

Classification (measured, not assumed): running that ONE test alone → `--- PASS` (the GC warning is an
expected log line from a deliberately unreadable fixture, `internal/testutil/scratchroot_test.go:498`), and
the immediately following full-package run (job-18, same tip, `-v`) is `ok … 204.855s` with zero FAIL. So the
first run's FAIL came from a scratch-GC race against a peer's concurrent suite (the scratch tree is shared
and `/dev/shm/gopi-test-scratch` held ~489 entries at the time, `.gc.lock` stamped 17:19), NOT from any file
this branch touches — this branch changes **no** `.go` file at all (the delta is 6 comment lines in `.md`
docs plus this report). Recorded here because a single full-package FAIL next to a green targeted run is
exactly the shape that gets misattributed to the change under test; the discriminator is a re-run plus the
one-test isolation, both of which are in this transcript.

---

**Closing tip:** the acceptance above was measured on `3bc4761e` and re-confirmed after every later
report commit — the branch head at hand-off is whatever `git log --oneline -1 fix/testutil-triple-20260916`
reports (each commit after `3bc4761e` is docs-only, inside this file). Re-verify in one command:
`CGO_ENABLED=0 go test ./internal/testutil/ -run 'TestWorkerOwnershipPointerPresent|TestTestBudgetHasASingleSourceAndNamesATimeout|TestPinCoverageTable' -count=1`
