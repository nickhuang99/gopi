<!-- doc-class: normative -->
# Task Report: resolve merge conflict — feat/peak-hour-guard into master

> **RETIRED (2026-09-16).** The guard whose flags the tables below exercise was removed outright
> by `fix/remove-peak-guard` (commit `bb0c4c1a`, devdoc/peak-guard-removal.md): `--ignore-peak-hours`
> is now an unknown flag and `GOPI_IGNORE_PEAK_HOURS` is read by nothing. The measurements below
> record what the tree did in September 2026; do not replay them as commands.

## Completed

### 1. Diagnosis: why the branch would not merge

The briefing said "no conflicted files reported by git (the probe failed for
another reason)". The real reason:

- master **had already merged this branch once** (`bd42e827 Merge branch
  'feat/peak-hour-guard'`) and then **reverted it twice** (`9cbeb9e5`, and
  `69f91ccc` for the follow-up `fix/peak-hour-build-gaps`).
- So master carries a **revert commit** of the branch's content while the
  branch tip still carries the content. A plain `git merge af9575ae` re-applies
  the branch diff on top of a tree that already "knows" the content was
  reverted → conflicts / a re-revert, not a clean fast-forward.
- Recovery: the branch's own content is intact on the branch. What matters is
  `ce7e127a..af9575ae` (13 files, 871 insertions) — the branch diff from its own
  base, which is what the merge must land.
- The branch tip `af9575ae` message says it outright: *"wip(peak-hour):
  peak-hours cost guard — **does NOT build yet** (2 small gaps)"*. Those two gaps
  were fixed on the (now reverted) follow-up branch `fix/peak-hour-build-gaps`
  at `dc4c1753`; their fixes were **ported into this resolution**.

### 2. Fresh worktree, fresh merge — no work on the failed path (Rule 23)

`git worktree add /home/user/workspace/gopi-peak-hour -b feat/peak-hour-guard`
from master `13acf700`, then merged `af9575ae` into it. First merge commit
**`88f2c454`** (parents `13acf700` = master tip, `af9575ae` = branch tip).

**Master then advanced** (docs + fixes), so the branch was extended by merging
the current master tip **`2c01d9ec`** (`f0e213d8`), and the standards permit
re-keyed (`e9532d3b`). Final commits: `4257de5f` (report sync) and `e7748b2a`
(probe matrix + filed-bug note) — the **last code commit is `e7748b2a`**; the
commits after it are docs-only edits of this report, so the branch tip may sit
one or two doc commits higher at adoption time. The tree below was verified on
the code head `e7748b2a`.

An intermediate master commit `c0cd21ab` was briefly merged in and then
**deliberately dropped** by resetting the branch: master's own reflog proves
`c0cd21ab` was merged and then *reset away* (`master@{1}` = merge,
`master@{0}` = reset), so re-landing it inside this branch would silently
resurrect work master chose to discard. `feat/peak-hour-guard` is branched from
`2c01d9ec` only — a merge-clean branch from the tip is the whole deliverable.

### 3. Three conflicts, and how each was resolved

| File | Conflict | Resolution |
|---|---|---|
| `cmd/agent/main.go` | master's new tmpdir bootstrap + `signalrec.Install` vs the branch's top-of-main peak gate | kept BOTH; **ordering decided deliberately**: the peak guard runs at line ~565, *before* `signalrec.Install` (~578) — `signalrec.Install` rebinds `dispatchArgs`, so a guard reading the pre-install argv is the only place where the verb it refuses to run is the verb the operator actually typed |
| `cmd/agent/cli_tasks_worker.go` | master's drain/stop guard vs the branch's peak claim deferral | drain guard FIRST (an operator-requested stop must exit without first sleeping out a peak window), then the peak claim deferral for the claim path |
| `cmd/agent/cli_verbs.go` | master's `taskVerbRun = taskverbs.Run` alias vs the branch's help-word constants | kept **both** — the alias for dispatch, the constants as the single source of truth for the help vocabulary |

### 4. Ported the branch's own build fixes (dc4c1753)

- `cmd/agent/cli_tasks_worker.go`: missing `sync/atomic` import (`atomic.Bool`
  for the one-shot peak warning latch).
- `cmd/agent/cli_tasks_worker_stop.go`: the loop-body `remaining` was declared
  inside the `if` scope but clamped by the peak deferral below it → hoisted to
  the loop-body scope.

### 5. A real defect found by runtime probe (not by the compiler)

The branch's `isUsageQuery` exemption listed only `HelpFlag / HelpFlagLong /
HelpWord` — while `main`'s help scan also answers `--help-env` / `help-env`.
Consequence: **inside a peak window, `gopi --help-env` was refused with exit 4**,
i.e. the very escape hatch the guard's own refusal message points at was
unreadable from inside the window. Fixed by referencing **main's own help
constants** (`HelpEnvFlagLong`, `HelpEnvWord`) instead of re-typing a subset of
the vocabulary (Rule 13: constants exist to be referenced).

### 6. Regression guards (Rule 10.5 — every fix ships a repro test)

- `cmd/agent/peak_guard_test.go` — `TestIsUsageQueryAnswersEveryHelpSpelling`
  (every constant main's scan answers on must be exempt) and
  `TestIsUsageQueryRejectsNonQueries` (scope: a single token only, `--help-env x`
  and a prompt containing `--help` are still guarded). The construction that
  matters is that predicate and scan read the SAME constants, so a future edit
  that re-types a spelling in either one fails here.
- `internal/peakhours/peakhours_test.go` — window parse/format round-trip,
  wrap-around, fail-open on an unparsable declaration.

### 7. Second merge round — master moved again (2c01d9ec → 32fb9da3)

Between the first resolution and adoption, master advanced two commits to
**`32fb9da3`** (`Merge branch 'feat/fix-master-red-standards-stylegate-signalrec'`),
touching `cmd/agent/tmpdir_inject.go`, `internal/signalrec/signalrec_test.go`,
`internal/testutil/tmproot_assert_test.go`, a new devdoc and
`internal/standards/baseline.go`. The branch was re-merged onto that tip
(commit **`aa0d14fa`**, parents `583138af` = previous branch head,
`32fb9da3` = master tip).

**One conflict, and it is the interesting kind: both parents re-keyed the SAME
Rule-16 permit.** master re-keyed `cmd/agent/main.go:955 → :961` (signalrec
wiring adds lines above the site); this branch re-keyed `:955 → :982` (the peak
guard adds lines above the site, and master's own `:955` was already stale —
the merged tree puts the call at `:982`). Textual merge therefore produced two
contradictory permit lines. The resolution **combines both shifts in one
reasoned entry** — neither parent's number is right alone, because both shifts
are real and additive:

- `cmd/agent/main.go:982` with a comment naming BOTH causes (signalrec.Install
  wiring + the peak-hour start guard) and the verification that the site is
  unchanged: `grep -n 'escape a literal prompt' cmd/agent/main.go` prints
  exactly one line, at `:982`.
- The gate was then run on the merged tree: `go test ./internal/standards/` →
  **ok** (Rule 16 NEW = 0, every other gate NEW = 0, no stale permit) — i.e. the
  merged tree is *cleaner* than pristine master, which still fails both ways at
  this site.
- Nothing else conflicted: the other four master files auto-merged. The new
  master commits are the *reason* this round exists, and none of them touches
  the feature's files.

A Rule-23 note: the merge was done by extending the **existing** resolution
branch (it is this task's own branch, not somebody else's failed path) rather
than opening a fresh one — the failed path being avoided is the reverted
`9cbeb9e5`/`69f91ccc` content on master, which this branch does not touch.

## Verification

- Second round (`aa0d14fa`, on top of master `32fb9da3`): `CGO_ENABLED=0 go
  build ./...` → **OK**; `gofmt -l cmd internal` → empty; no conflict markers
  left in any `.go` file; `go test ./internal/standards/ ./internal/peakhours/
  ./internal/config/ ./internal/pricing/` → **all ok**.
- Runtime exit-code matrix re-run on the **rebuilt binary of the merged tree**
  (`CGO_ENABLED=0 go build -o … ./cmd/agent/`, Beijing 19:00):
  `--peak-hours 19:00-20:00` + {`version`, `--help`, `help-env`} → **exit 4**;
  `--ignore-peak-hours version` → **0**; an out-of-hours window → **0**;
  an unparsable window (`19:00-23:59`) → **0 with a loud stderr warning**
  (fail open, never silent). The default windows at 19:00 Beijing are off-peak,
  which is why the unforced matrix is all-0.
- Build: `CGO_ENABLED=0 go build ./...` → **OK** (from the tree at `88f2c454`;
  `go vet ./cmd/agent/ ./internal/peakhours/` → clean, gofmt clean).
- Runtime probe on the built binary (`GOPI_PEAK_HOURS` set to a window covering
  "now"): exit **4** inside the window; exit **0** with `--ignore-peak-hours`;
  exit **0** with the env override; exit **0** for all five help spellings
  (`-h`, `--help`, `help`, `--help-env`, `help-env`); exit **0** outside the
  window; an unparsable window string → **fail open with a warning on stderr**
  (never a silent stand-down, never a wedged host — Rule 15).
- Tests: `go test ./...` on the frozen merge tree is **100% of the packages that
  can pass**. The three failing packages are **pre-existing master failures,
  proven byte-for-byte**, not merge regressions:

  | Package | master `2c01d9ec` | merge HEAD `e9532d3b` | Verdict |
  |---|---|---|---|
  | `cmd/agent` | 3 FAIL | 3 FAIL — **identical set** | pre-existing (host env: `GOPI_WORKER_DAEMON_CHILD=1` leaks into the guard test's exec'd child → "inherited worker lock fd 3 is not a usable file") |
  | `internal/stylegate` | FAIL (ignored-error / test-sleep / test-setenv ceilings) | FAIL — same findings; the one count difference is `undecidable sites 115 → 116`, i.e. exactly the ONE new file this merge adds that the gate cannot decide | pre-existing |
  | `internal/testutil` | `TestWorkerOwnershipPointerPresent` FAIL (10 files under `devdoc/worker-graceful-drain*` lack the pointer) | FAIL — **same 10 files** | pre-existing |

  Method (the comparison is the point — a green-looking claim would hide this):
  clean detached worktree of master `2c01d9ec` (`/home/user/workspace/gopi-peak-mastercheck`)
  vs the merge worktree, same commands, output diffed:
  `diff <(go test ./cmd/agent/ …) <(…)` → the only lines differing are the
  wall-clock durations; `diff` of the stylegate finding lines → the single
  115/116 count (new file). Neither `internal/stylegate`, `internal/testutil`
  nor the `cmd/agent` spawn-guard tests are touched by the merge diff
  (`git diff --name-only 13acf700..6f5a5024 | grep -E 'stylegate|testutil'` → empty).
- Runtime re-probe on the frozen tree's own binary, with a
  `GOPI_PEAK_HOURS` window covering "now": `version` → **exit 4**;
  `--help-env` → **exit 0**; `--ignore-peak-hours version` → **exit 0**;
  no window → **exit 0**.
- Commit: `88f2c454` (first merge), `6f5a5024` (help-vocabulary fix + regression
  tests), `f0e213d8` (merge master `2c01d9ec`), `e9532d3b` (**head** — standards
  permit re-key).
- Standards gates (Rules 16–21, `go test ./internal/standards/`) — **the one
  place where the merge itself needed a repository-wide fix, and it was needed
  on master too**: Rule 16's permit is keyed by `file:line`, and both the
  merge and master's own drift made it stale. On pristine master `2c01d9ec` the
  gate fails **both ways** — a NEW hit at `cmd/agent/main.go:961` *and* a STALE
  permit at `:955`; on the merged tree the same site is at `:982` (the peak
  guard adds lines above it in `main()`). Re-keyed at `e9532d3b` with a
  reasoned comment, never relaxed and never deleted: the site is the unchanged
  `%w`-impossible construction (its `%v` verbs format a *string* constant
  describing how to escape a payload), and the gate still fails on a new `%v`
  wrap anywhere else.

## Not Done

- **The revert pair on master was NOT touched.** Master still carries
  `9cbeb9e5` / `69f91ccc` (the reverts). The resolution makes the branch
  merge-clean *as a normal merge* — the merge commit lands the content; the
  revert commits remain in history and are not rewritten (rewriting master's
  history is out of scope and would be destructive).
- **No merge into master from this session** — per the briefing the server
  adopts the branch on a later serve tick; `scripts/safe-merge.sh` and the
  merge itself are the PA's step (Rule 6c: a worker does not merge its own
  branch).
- **The three pre-existing master failures were NOT fixed** (out of scope for a
  conflict resolution, and touching them would mix an unrelated repair into the
  merge): the `cmd/agent` spawn-guard trio (host env leak), the
  `internal/stylegate` ceilings, and `TestWorkerOwnershipPointerPresent`'s ten
  `devdoc/worker-graceful-drain*` files. They are documented above with the
  master-vs-merge proof so the PA can see they are not this branch's doing.
- **The one new stylegate "undecidable site" (115 → 116)** is
  `internal/peakhours/peakhours.go` — a new file the syntactic gate declines to
  judge. It is reported, not a violation; no ceiling was raised for it.
- `internal/pricing/pricing.go` now references `internal/peakhours` instead of
  its own hardcoded window pair; the pricing assertions were not re-derived
  beyond the full-suite run above (the package itself passes: `ok
  github.com/nick/gopi/internal/pricing`).
- **A real defect in the feature was found by the runtime probe and intentionally
  NOT fixed here** (it is a semantic change, not a conflict resolution, and it is
  deliberately pinned by the feature's own test). `internal/peakhours.NextOffPeak`
  walks off-peak candidates as whole-hour offsets from the **current moment**
  (18:56 → 19:56) instead of from the hour boundary (→ 19:00), so the refusal
  over-states the 2x window by up to 59 minutes:
  `GOPI_PEAK_HOURS="18:00-19:00" ./bin/gopi web` at 18:56:10 says *"billed at 2x
  until 19:56:10 (1h0m0s away)"* when the window ends at 19:00 (4m away). The
  same `Wait` paces the resident worker's claim deferral. The behaviour is
  **deliberate** — `internal/peakhours/peakhours_test.go:TestNextOffPeakWalksFromTheCurrentMoment`
  pins "the walk's ACTUAL semantics … the conservative walk" — so changing it
  inside a merge commit would be hiding a feature decision in a resolution.
  Filed as its own bug task: **`01a09a69-6643-7732-86e2-c4f98a80c5ef`** (repro,
  two consequences, suggested fix, acceptance criteria).
- The merge is against master **`32fb9da3`** (the tip at the second, final
  round — `aa0d14fa`; the first round was against `2c01d9ec`). If master moves
  again before adoption, re-run the same comparison; `git diff 32fb9da3..HEAD`
  contains **no** file outside the feature (13 branch files +
  `peak_guard_test.go` + `peakhours_test.go` + this report +
  `internal/standards/baseline.go` — the last only in the conflicting permit
  hunk), so a later merge is conflict-free unless master itself edits those same
  files.

## Post-merge rework round — the recorded revert's true cause (task `01a098b0`)

This section answers the rework epic's question (*why did the verify gate revert
this branch, and what must the merged state do to pass?*). It was written
**without a branch-attributable red to repair** — the diagnosis below is the
deliverable, and it is why no code change is warranted here.

### A. The branch cannot regress current master — the delta is three non-Go files

Against today's master the branch adds **no Go code at all**:

```
$ git diff --name-only master...feat/peak-hour-guard
devdoc/merge-resolve-peak-hour-guard.md
internal/standards/baseline.go
tasks/01a098a1-report.md
```

and every feature path is **byte-identical** to master's
(`git diff master e1cf22c1 -- cmd/agent/peak_guard.go internal/peakhours/
internal/config/workhours.go` → empty). Master independently re-landed this
feature after the revert (`88c2c454`, `6f5a5024`, `adf10911`), so the code the
rework was filed against is already on master; only the two documents and the
Rule-16 permit comment block still differ.

### B. The cycle-2 root cause: a stale `file:line` permit — Rule 16

The gate judged a **delta**, not an absolute state
(`internal/tools/verify_gate.go`, at the time of this incident): it swept the
PRE-MERGE tree, exported the result as `GOPI_MERGE_VERIFY_BASELINE_LOG`, and the
child's rule was *"PASS iff the merged
state adds no failing package the baseline did not already fail"*. So a
master that is already red is fine — but ONE package that was **green on the
baseline and red on the merged tree** fails the child and reverts.

> **RETIRED 2026-10-30.** The baseline-delta machinery described in this
> section is GONE: slice 2/4 of epic `01a18799` retired the `verify` verb, and
> `MergeVerifyBaselineRunEnv` / `MergeVerifyBaselineRunTimeout` /
> `MergeVerifyBaselineFailures` / `MergeVerifyFormatBaseline` were deleted with
> their only caller (`TaskVerify`, `internal/tools/tasks.go`). The audit below
> is still the accurate record of what the incident was; there is no live
> `GOPI_MERGE_VERIFY_BASELINE_LOG` wire to set.

That is exactly what happened, and `git` proves it in three lines:

```
$ git show 05d73d703e69:cmd/agent/main.go | grep -n 'escape a literal prompt'   # the baseline's site
860:  return "", fmt.Errorf("Error reading @ payload file %q: %w (escape a literal prompt …
$ git show bd42e827:internal/standards/baseline.go | grep -n 'cmd/agent/main.go:'  # the merged permit
106:  "cmd/agent/main.go:860",
$ git show bd42e827:cmd/agent/main.go | grep -n 'escape a literal prompt'         # the merged site
875:  return "", fmt.Errorf("… (escape a literal prompt …
```

The peak-guard's imports in `main()` shifted the Rule-16 site **860 → 875**
while the permit still named `:860` — a **stale permit**. The gate then fails
**both ways** (a NEW hit at `:875` *and* a STALE entry at `:860`), so
`internal/standards` flipped green → FAIL on the merged tree. The merger's own
log records the verdict's basis and the unwind:

```
[merger] merge-verify: baseline sweep at 05d73d703e69: failing packages = cmd/agent,
         internal/stylegate, internal/testutil, internal/tools, internal/version
[merger] merge-verify FAIL: branch feat/peak-hour-guard reverted (git revert -m 1 bd42e827…)
```

`internal/standards` is **not** in that listed set (it was the healthy baseline
package), which is precisely why its flip is the delta the child rejected. The
recorded event carries the templated reason only — the child's verdict text was
**not** written to the ledger (the rework briefing's `Verify evidence … event
"merge-verify":` field is empty), so this diagnosis had to be recovered from
`~/.gopi/logs/merger.log` + `git`.

### C. Master fixed the same defect independently — the merged state is green now

Master's own `86467b49` ("fix(standards): re-key the Rule 16 permit after the
peak-guard merge") re-keyed the permit; today **branch and master agree**, and
the key matches the real merged site:

```
$ git show master:internal/standards/baseline.go | grep -n 'cmd/agent/main.go:'   → "cmd/agent/main.go:982"
$ git show master:cmd/agent/main.go | grep -n 'escape a literal prompt'          → 982
```

So the exact interaction that reverted cycle 2 no longer exists on the merged
tree — and the branch's own §7 resolution had already reached the same key
independently (`:982`, both shifts combined). The two sides converged, and the
merge is now **conflict-free**: `git merge --no-commit --no-ff
feat/peak-hour-guard` on master `86467b49` → *"Automatic merge went well"*, 0
conflicted files, 0 conflict markers, staging exactly the three files in §A.

### D. Measured on the merged state (this round)

Worktree `/home/user/workspace/gopi-peak-mctl2`, master `86467b49` + branch
`e1cf22c1`, merge left staged (`MERGE_HEAD=e1cf22c1`):

- `CGO_ENABLED=0 go build ./...` → **exit 0** (clean); `gofmt -l cmd internal`
  → empty.
- `go test ./internal/standards/ ./internal/peakhours/ ./internal/config/`
  → **ok / ok / ok** (the packages a line shift puts at risk).
- **Functional probe on the merged tree's own binary** at 20:16 Beijing —
  the fix's intent is intact end-to-end:

  | probe | exit |
  |---|---|
  | `GOPI_PEAK_HOURS=20:00-23:00 gopi version` | **4** (refused, 2x warning on stderr) |
  | … `gopi --ignore-peak-hours version` | 0 |
  | … `gopi --help-env` | 0 (*the branch's own `isUsageQuery` repair*) |
  | `GOPI_PEAK_HOURS=01:00-02:00 gopi version` | 0 |
  | `GOPI_PEAK_HOURS=19:00-23:59 gopi version` | 0 + loud stderr (fail open) |

- **Differential vs pristine master** (`go test ./...` both sides, same host,
  same `TMPDIR`, sequential; artifacts in
  `/dev/shm/gopi-test-scratch/peak-rework/`): **zero differential**.
  - failing packages, merged = failing packages, pristine =
    `{cmd/agent, internal/agent, internal/testutil}` — `diff` empty;
  - failing **test names** identical (`TestAgentSpawnGuard*` ×3,
    `TestHippocampusDocAnchorsResolve`, `TestWorkerOwnershipPointerPresent`) —
    `diff` empty;
  - `CGO_ENABLED=0 go build ./...` → `build_rc=0` on both;
  - the four packages pristine's tree sweep truncated
    (`internal/tools`, `internal/version`, `internal/worktreeroots`,
    `testsuite`) were re-run individually: **ok on pristine**, and ok on merged
    — so the comparison covers the whole package universe.
  No new failing package and no new failing test, i.e. the child's rule is
  satisfied. The failures are unambiguously **not branch-attributable**: the
  branch adds no Go file (§A) and touches **none** of the files the failing doc
  gates name (`devdoc/hippocampus.md` anchor drift, the
  `devdoc/worker-graceful-drain*` set, `devdoc/detach-judgment-rule.md`); the
  `cmd/agent` trio is the host worker-env leak this session itself demonstrates
  (`GOPI_WORKER_DAEMON_CHILD=1` + `GOPI_DAEMON_READY_FD=3` inherited by the
  test's exec'd child → *"inherited worker lock fd 3 is not a usable file"*).

### E. Verdict and deliverable

**No branch code change is warranted or made.** The branch is not the
regression: it contributes zero Go delta and its already-relied-on permit
re-key is now identical to master's. Every remaining red on the merged tree is
present identically on pristine master — a baseline-covered, pre-existing
set (some of it a host-environment artifact), which the gate's delta rule
explicitly tolerates. The deliverable is therefore **this diagnosis plus the
measured evidence**, with the branch left merge-clean and untouched so the
merger lands it on a later tick and the gate judges the same state again.

Reusable lesson stored in the knowledge base: **memory id 2500** (the
baseline-DELTA rule + the `file:line` permit drift that trips it + the
`merger.log` forensics recipe for a revert whose reason was never recorded).
