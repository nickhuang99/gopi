<!-- doc-class: normative -->
# Task Report: test-suite rigor audit — scratch isolation + test-code discipline

**Epic**: Test-suite rigor audit: root-cause the historical `pN_dump_*`/`*_verify`
residue (A), complete scratch isolation across the Go test packages (B), and
review the test code itself for resource/isolation/effectiveness discipline (C).
**Branch**: `feat/scratch-residue-audit` (worktree `~/workspace/gopi-scratch-audit`)
**Baseline**: master `955bb91`; master advanced to `16dfe7e` mid-task, which is
merged into this branch. The briefing's "before" state is STALE: the
`/tmp` ban (`feat/ban-tmp-scratch`, `8ef6f48`) is already merged
(`git merge-base --is-ancestor 8ef6f48 HEAD` → true), so this report RE-MEASURES
rather than re-implements, and reports only the delta this task added.

---

## 1. Answer to (A): what produced `pN_dump_*` / `*_verify`, and does it recur?

**The residual directories on this host are NOT Go test scratch.**
Measured (`/tmp/p3_atl_dump/00000.json`, all `2026-09-06`, reproducible with
`ls -1d /tmp/p3_* | wc -l` → **130** entries, of which `ls -1d /tmp/p3_*/` → 111
directories; the `>6` `_verify` variants follow the same naming rule):

```
{ "file": "/tmp/atl.h3m", "mapname": "atl", "ok": true,
  "plain": { "event_names": [ "Motto1", "all0-afterthestorm0", ... ] } }
```

plus a sibling `paths.txt` naming the input map. That is a **HoMM3 `.h3m` map
dump**, i.e. the output of a separate VCMI/map-tooling session — the producer is
`~/workspace/h3m-deliverable` (its `build.sh` builds `h3mplayable` against
`~/workspace/h3m-tools-review/homm3tools/h3mlib`), *not* this repo: neither
`p3_*` nor `paths.txt` nor `00000.json` exists anywhere in GoPi's sources or in
its git history (`git log --all -S` → nothing). GoPi's own test path was
excluded by measurement, not by assumption: `TestEveryTempUsingSuiteIsPinned`
and `grep -rn '"/tmp'` over GoPi's test tree show no Go-test code that creates a
`pN_dump_*` directory, and the pin has moved every suite's scratch off `/`.

**The 13,044-directory incident itself** (`devdoc/peer1-resource-exhaustion-20260911.md`)
had ONE mechanism, and it was GoPi's: a suite's scratch root followed
`os.TempDir()` = `/tmp` = the boot device (device 2051), with **no cleanup hook**
— the pre-pin `pinSuiteTempRoot` created one root per run and only `TestMain`'s
success path removed it. The peer1 table's attribution of the `pN_dump_*`
*name* to "Go 测试的 scratch root 产物" is therefore imprecise: the *count and the
mechanism* are GoPi's, the *names on disk today* are the map tooling's. Both
statements are now in the record — see §6, where the peer1 table carries a
one-line correction so a future agent does not re-chase the wrong producer.

**Recurrence after the merge: bounded, with two measured exceptions.**

| channel | state | evidence |
|---|---|---|
| Go suite scratch (`t.TempDir()`) | **cannot** land on `/tmp` | 14/14 scratch-using packages pinned; `PinSuiteForTestMain` refuses pre-test if the pin did not engage (§3) |
| ambient fallback (`os.TempDir()` with no pin) | **refused**, exit 97 | `TestSuitePinFallbackToBootDeviceFails` (§3) |
| Go tool's own build work dir | still `/tmp/go-build*` | documented Not Done: `GOTMPDIR` is a machine-level lever, not a repo change (pre-existing) |
| legacy `gopi-*-suite-tmp-*` roots in the memory base | **swept** now | §2: 580 → 75 `suite-tmp-*` names in `/dev/shm` after the first sweep; **0 aged** left |
| **`gopi-job-*.out/.err` in `/tmp`** | still appearing | measured `2026-09-11 08:12`, while the scratch-home fix is already in `internal/scratch` — see the note below |

The last row is the honest residual. `JobStart` streams to
`scratch.CreateTemp(RuntimeClassJob, …)` → `<scratch>/queue/queue-runtime-job-<pid>/`.
This session's own jobs land there as designed (measured: two consecutive jobs
reported `stdout: /dev/shm/gopi-test-scratch/queue/queue-runtime-job-2907468/…`),
so the `/tmp` files belong to a process whose **binary predates the fix**: the
`gopi worker` processes carry a start time of `07:24`, while `bin/gopi` was
rebuilt at `07:41` — a running process keeps the old image, which is exactly the
recurring stale-bin mechanism Rule 12 describes. Not a code defect; it is the
reason the merge step rebuilds.

---

## 2. (B) Bounded sweep of the legacy `gopi-*-suite-tmp-*` residue

**The hole.** The pin that predates the scratch home (`8286e98`, `5f64422`)
created its per-run root *directly inside the shared memory base* —
`os.MkdirTemp(memoryBase, "gopi-suite-tmp-")`, and `internal/tools`' original
implementation spelled its own suite name in (`"gopi-tools-suite-tmp-"`). The
only cleanup hook was `TestMain`'s success path, so a suite killed by a timeout,
`^C` or the OOM killer left its root behind **forever**. When the pin moved into
`<home>/workspace/gopi-test-scratch/<suite>/<run>`, the age GC moved with it and
those older roots became invisible to every collector. Measured before the fix:
**43 + 537** orphans in `/dev/shm`.

**The implementation** (`internal/testutil/legacy_residue.go`, new):
`GCLegacyScratchResidue()` / `…OlderThan()`, run **inline by every pin** (so it
bounds the residue without a watchdog). It is namespace-limited **by
construction**, which is the property that keeps it from ever becoming a global
delete:

1. it walks only the bases `MemoryBase()` resolves (`GOPI_SCRATCH_MEMORY_BASE`,
   read in exactly ONE place — Rule 2), and skips a base on the `/` filesystem;
2. it collects only names in **this system's** legacy namespace: a `gopi-`
   prefix **and** a `suite-tmp-` infix *with* a suffix
   (`IsLegacyScratchResidue`) — `gopi-acc-*`, `gopi-diag-*`,
   `gopi-gate-probe-*`, `gopi-test-scratch`, another program's
   `someone-else-suite-tmp-*` and a bare `suite-tmp-*` are all excluded;
3. it collects only a **directory** (`Lstat` — a same-named file or symlink is
   left alone, and the sweep never follows a link into somebody else's data)
   older than `ScratchStaleAge` (the SAME 6h threshold as the home GC) **and
   held by no live process** — the `.holder` registration plus a single batched
   `/proc/<pid>/cwd|fd` pass;
4. it never recurses, is bounded by `scratchScanLimit`, and reports every
   removal failure instead of swallowing it (Rule 15b).

**Executed evidence** (the acceptance's "7-hour-old orphan collected while a
foreign entry survives"):

```
$ go test -count=1 ./internal/testutil/           # before
585 residue names in /dev/shm
testutil: legacy suite-scratch residue GC removed 7 stale entries in /dev/shm older than 6h0m0s
…  (across the runs while developing the sweep)
$ ls -1 /dev/shm | grep -c 'suite-tmp-'
75                                                 # 580 -> 75, aged residue: 0
$ ls -1 /dev/shm | grep -c 'gopi-acc\|gopi-diag\|gopi-gate-probe\|gopi-verify'
4                                                  # all four foreign-by-namespace entries intact
$ ls -ld /dev/shm/gopi-test-scratch                # the scratch home itself: untouched
```

The residue count is not zero because every run of every *currently executing*
suite creates young entries — by design collectable only after `ScratchStaleAge`.
The oldest survivor on the host is `Sep 11 02:12`, i.e. **exactly at the 6h
boundary**: nothing older than the threshold survives, and the next pin crosses
it. `TestHostLegacyResidueIsSweptByTheSuitePin` states the post-condition on the
real host and reports the split (`aged` must be 0; `young` is logged).

**Tests** (`legacy_residue_test.go`, all run against a PRIVATE memory base, never
the host's real one):

| test | property |
|---|---|
| `TestIsLegacyScratchResidueNamespace` | the 17-row accept/reject table — the sweep's whole safety argument |
| `TestGCLegacyScratchResidueCollectsAgedOrphanAndKeepsForeignEntry` | **the acceptance case**: two 7h-old self-created roots collected; a foreign program's entry, a `gopi-`-prefixed NON-scratch entry, and a 5h-old root all survive |
| `TestGCLegacyScratchResidueNeverDeletesAHeldRoot` | hard rule: a live `sleep` holding the root blocks the sweep, and the root IS collected once the holder exits (so the survivor was the holder check, not an inert sweep) |
| `TestGCLegacyScratchResidueSkipsABaseOnRootFS` | an aged, correctly-named root in a `/`-filesystem base survives — the base is refused, not swept |
| `TestGCLegacyScratchResidueHonoursTheScratchHomeOverride` | an operator pin (`GOPI_TEST_TMPDIR`) stands the sweep down entirely |
| `TestGCLegacyScratchResidueLeavesFilesAndSymlinksAlone` | a same-named file and symlink survive, and the symlink's target is never deleted through the link |
| `TestGCLegacyScratchResidueAgeBoundary` | the explicit-threshold form and its ±1 minute boundary |
| `TestMemoryBaseNameMatchesTheSetupScript` | drift gate: the sweep and `scripts/setup-scratch-home.sh` read ONE variable name; `MemoryBaseDefault == ShmDir` |

---

## 3. (B) The implicit boot-device fallback now FAILS

**The hole.** With no pin engaged, `SuiteTempRoot()` is `os.TempDir()` — `/tmp`,
the boot device — and `PinSuiteForTestMain` ran the suite there. Nothing refused:
the only guard, `AssertSuiteScratchRootSafe`, fails on `/tmp` *by device id*, and
was never reached "by accident" as a designed behaviour, only as a message.

**The change** (`internal/testutil/scratchroot.go`): `PinSuiteForTestMain` now
refuses an **unengaged** pin **before any test runs**, exits
`SuiteScratchUnsafeExitCode` (97), and prints a message naming the AMBIENT
fallback, the measured state, and the fix — deliberately *distinct* from the
"operator pinned a bad root" message, because the two need different fixes.
`GOTESTSUITE_NO_PIN=1` is the documented opt-out for a host that genuinely cannot
hold a scratch home (a read-only container); the default is to refuse.

**Executed evidence** — `TestSuitePinFallbackToBootDeviceFails` re-execs *this
package's own `TestMain`* (via `GOPI_TESTUTIL_REEXEC`/`GOPI_TESTUTIL_NO_PIN`) with
the scratch home made uncreatable (`GOPI_TEST_HOME` pointed at a regular file, so
every `MkdirAll` under it fails with ENOTDIR) and `TMPDIR` on a fixture inside
`/tmp`:

```
=== RUN   TestSuitePinFallbackToBootDeviceFails
--- PASS: TestSuitePinFallbackToBootDeviceFails (0.01s)
```

The test asserts, in order: the child's exit status is **non-zero and equal to
97**; the output carries `did NOT engage`, `AMBIENT temp root`,
`SAME filesystem as /` and `FIX:`; and `--- PASS: TestScratchReexecTrivial` is
**absent** — i.e. no test body ran, so the refusal is a pre-test refusal and not
a mid-suite failure a CI wrapper could misread as a partial pass.

---

## 4. (B) Pin coverage: BEFORE / AFTER table

Produced by `TestPinCoverageTable` (which prints the table and fails on any
scratch-using package without a pin). "Detected" is the static gate's view —
**BEFORE** = the marker set the gate shipped with
(`t.TempDir()`, `os.MkdirTemp("`) and **AFTER** = the widened set this task
added (`os.TempDir()`, `os.MkdirTemp("/tmp", …)`, `os.CreateTemp("/tmp", …)`).

| package | BEFORE detected | AFTER detected | pin |
|---|---|---|---|
| `cmd/agent` | yes | yes | **PIN** |
| `internal/agent` | yes | yes | **PIN** |
| `internal/checkpoint` | yes | yes | **PIN** |
| `internal/config` | yes | yes | **PIN** |
| `internal/deepseek` | yes | yes | **PIN** |
| `internal/gopilog` | yes | yes | **PIN** |
| `internal/hippocampus` | yes | yes | **PIN** |
| `internal/pricing` | yes | yes | **PIN** |
| `internal/sessionevent` | yes | yes | **PIN** |
| `internal/shot` | yes | yes | **PIN** |
| `internal/testutil` | yes | yes | **PIN** |
| `internal/text2img` | yes | yes | **PIN** |
| `internal/tools` | yes | yes | **PIN** |
| `testsuite` | yes | yes | **PIN** |
| `internal/proxy` | – | – | no temp use (gate confirms) |
| `internal/standards` | – | – | no temp use |
| `internal/taskverdict` | – | – | no temp use |

`17 packages scanned | scratch-using: 14 by the ORIGINAL markers, 14 by the
widened markers (+0 newly detected) | pinned: 14`.

**The +0 is the finding, not a non-result.** Every temp-using package was already
pinned — *because the ban already shipped*. What the widening adds is the guard
for the **next** package: it now recognises the incident's actual signature
(scratch reached through `os.TempDir()` or an explicit `"/tmp"` argument), which
the original two markers could not see. Test history confirms that is exactly the
shape the leak had:

| state | temp-using | pinned | unpinned |
|---|---|---|---|
| pre-ban (master @ `5f64422^`) | 3 (`cmd/agent`, `internal/agent`, `internal/tools`) | 3 | **14 packages had NO `TestMain` at all** |
| post-ban (this worktree) | 14 | 14 | 0 |

(Pre-ban column measured with `git grep -l 'func TestMain' 5f64422^ -- '<pkg>/*_test.go'`:
`checkpoint`, `config`, `deepseek`, `gopilog`, `hippocampus`, `pricing`,
`sessionevent`, `shot`, `testutil`, `text2img`, `testsuite` … → 0 TestMains.)

**Minimal sample for the new detector**: `TestUsesScratchTempDetectsAmbientChannels`
(9 rows: `os.TempDir()`, `os.MkdirTemp("/tmp", …)` → true; an inert `--db /tmp/x.db`
argument and a comment → false) and `TestTmpPathCreationPredicate` for the Go
source gate's predicate.

---

## 5. (C) Test-code discipline review

Scope measured in this worktree: **89,801 lines of `_test.go` vs 74,494 lines of
production `.go`** (the briefing's 86,737/72,473 has grown). The three axes, with
what was measured and what was done:

### 5.1 Scratch/resource discipline — **fixed + gated**

| site | finding | action |
|---|---|---|
| `cmd/agent/cli_version_test.go` | built its probe binary to the **fixed** path `/tmp/gopi-version-test`; `19918061` bytes still on the boot device (`Sep 11 07:35`) | **fixed** — builds into `testutil.SuiteTempRoot()` (off `/`, removed with the run root) |
| `cmd/agent/home_settings_test.go` | same, `/tmp/gopi-home-settings-test` (`Sep 8`) | **fixed** — same |
| `cmd/agent/settings_nilpanic_test.go` | `os.MkdirTemp("", …)`, **no cleanup** | ticket (same class) |
| the tests themselves | the `/tmp` gate audited `scripts/`, `devdoc/`, `testdata/` but **not** Go test sources | **fixed** — `assertTestSourcesCreateNoTmpPath` now scans all 324 test files |

A fixed, **executable** path in a world-writable directory is more than a leak:
the test executes whatever is at that path, so a stale or planted file decides
what runs (a supply-chain-shaped hazard in CI). The new gate flags a `/tmp` path
**only when a creating call consumes it on the same line** (`os.MkdirTemp`,
`os.CreateTemp`, `os.WriteFile`, `os.RemoveAll`, `os.Chdir`, `go build -o`, …),
with three reviewed exemptions. The narrow predicate is deliberate and measured:
**125** test lines carry an *inert* `/tmp` string (a path handed to an argument
parser, `{"path": "/tmp/x"}` inside a mocked tool call, a doc comment) and a gate
that reddened on those would be disabled by the next contributor — the same
reasoning recorded for the `internal/standards` gates.

Resource hygiene measured, for the record: **55** `time.Sleep` sites, of which
**1** is ≥1s (`internal/tools/memory_sync_delta_test.go:1349`, a documented
strictly-later-second requirement); **152** `exec.Command` sites, and **0** files
that `Start()` a process without a `Kill`/`Wait` in the same file; `t.TempDir()`
usage up from 940 to **965**.

### 5.2 Isolation discipline — **ticketed**

- **`os.Setenv` in tests: 49 sites** (`internal/agent` 27, `internal/tools` 7,
  `cmd/agent` 7, `internal/deepseek` 2, `internal/config` 2,
  `internal/standards` 1) — Rule 17 already mandates `t.Setenv`. The worst shape
  (6 sites, 2 files) is
  `old := os.Getenv(X); defer os.Setenv(X, old)`: when `X` was previously
  **unset**, `old` is `""` and the restore leaves the variable **set to the empty
  string** — indistinguishable to `os.Getenv`, visible to `os.LookupEnv`, and
  pointing at a `t.TempDir()` the framework has already removed.
- **`os.Chdir`: 16 sites**; `internal/tools/read_test.go:26` restores with an
  **unchecked** `os.Chdir(oldwd)` — a failed restore silently relocates every
  later test in the binary.
- **`t.Parallel`: 0** — so there is no cross-test parallelism to be
  order-dependent today; the risk is confined to the env/cwd leaks above and a
  `go test -shuffle` campaign (ticketed below).

### 5.3 Effectiveness — **measured, nothing to delete**

- **`t.Skip`: 193 sites (80 condition-less `t.Skip`, 107 `t.Skipf`).** Each was
  inspected mechanically for an unguarded form (a skip whose preceding 12 lines
  contain no `if`/`switch`) → **0 unguarded candidates**. The condition-less
  skips are all dependency- or re-exec-guards with the reason in the argument
  (`"ctags not installed"`, `"Xvfb is not installed"`, `"already inside the
  re-exec child"`, `"running as root — permission checks are bypassed"`), i.e.
  the skip is honest about the environment rather than hiding a failure.
- **No test was deleted or marked vacuous.** The epic allows deleting a provably
  vacuous test if listed with evidence separately; the scan produced no such
  candidate, and the two retired-double-coverage suspects were checked by hand
  and are live (e.g. `internal/testutil/scratchroot_entrypoint_test.go` is
  exercised by the re-exec test above).

### 5.4 Bug tickets filed (`tags=[bug]`, payload = repro + sites + acceptance)

| id | severity | headline |
|---|---|---|
| `01a08dcc-fef2…` | high | `cmd/agent` writes test-built gopi binaries to fixed `/tmp` paths and never cleans them (39MB measured on the host) |
| `01a08dcd-00d8…` | high | test code writes scratch into the developer's **real `$HOME`** (`internal/tools/read_test.go`, `~/gopi-read-test-<suffix>.txt`) |
| `01a08dcd-0401…` | medium | `os.Setenv` in tests leaks process state (49 sites; deferred restore is not unset) |
| `01a08dcd-1ed0…` | medium | the gopi-binary build cache is duplicated 3× in `cmd/agent` (last-writer-wins on package vars) |
| `01a08dce-7277…` | low | a test restores its working directory with an unchecked `os.Chdir` |

---

## 6. Not Done (honest backlog)

- **`cmd/agent` binary-build consolidation and the `$HOME` write are ticketed,
  not fixed** — both are test-code changes in packages this task did not need to
  touch to close (B), and each deserves its own `go test ./cmd/agent/` (≈42s) and
  `./internal/tools/` (≈155s) cycle.
- **`GOTMPDIR`**: `go test` still uses `/tmp` for its own build work dir
  (`/tmp/go-build*`, ≈100MB resident on this host). Unchanged from the merged ban;
  the lever is machine-level (`GOTMPDIR`/a wrapper), not a repo change.
- **`gopi-job-*` files in `/tmp`** are still produced by processes running a
  pre-`8ef6f48` binary image (stale-bin, §1); the code path already writes to the
  scratch queue. No repo change needed — the merge-time rebuild is the fix, and
  it has already happened for new processes.
- **`go test -shuffle=on` campaign** for order dependence: the measurement shows
  0 `t.Parallel`, so a shuffle run is the only way to expose the residual
  env/cwd coupling; not run here (it is a multi-minute full-tree run) and filed
  as the natural follow-up to the `os.Setenv`/`os.Chdir` tickets.
- **A systemd/watchdog timer** for the standalone GC (`GOPI_TEST_SCRATCH_GC_MIN`,
  `GCStaleScratchResidue`): the pin-inline sweep already bounds both trees
  whenever anyone runs a test, so this remains an operator action.
- **`plugins/websearch/remotebox/*`** stays out of scope (a deployed remote
  service, not a dev/test scratch workspace), as recorded in the merged ban.
- **One nested legacy residue directory is still NOT collected on purpose**:
  `~/workspace/gopi-test-scratch/tools-ext4probe/` holds legacy
  `gopi-suite-tmp-*` dirs from a manual ext4 probe by a previous session. The GC
  matches scratch names at the HOME level and never recurses into a home entry
  it does not recognise (recursing would mean deleting children of a directory
  this system did not create, which the hard rule forbids). Operator action:
  `rm -rf ~/workspace/gopi-test-scratch/tools-ext4probe` once confirmed unwanted.

---

## 7. Verification

```
CGO_ENABLED=0 go build ./...                    : OK
go vet ./internal/testutil/ ./cmd/agent/        : clean
gofmt -l internal/testutil cmd/agent            : clean (touched files only; no `make fmt`)
go test -count=1 ./internal/testutil/           : ok
go test -count=1 ./...                          : see §8 (full-tree run)
go test -run TestScriptsAreBoundedAndOffTmp -v  : PASS — 324 test files scanned, 3 exempted (3/3)
go test -run TestTmpPathCreationPredicate -v    : PASS (10-row predicate sample)
go test -run TestSuitePinFallbackToBootDeviceFails -v : PASS (child exit 97, no test body ran)
go test -run TestGCLegacyScratchResidue -v      : PASS (7h orphan collected, foreign entry survives)
go test -count=1 ./cmd/agent/                   : ok (46.9s; /tmp probe-binary mtimes UNCHANGED after the run)
go test -count=1 ./internal/testutil/           : ok (all 9 acceptance tests PASS on a fresh independent run)
go test -count=1 ./... (full tree)              : GREEN — 17/17 test packages ok, 0 FAIL
                                                  (20 pkgs listed = 17 ok + 3 "no test files";
                                                   cmd/agent 43.8s, internal/tools 164.3s)
                                                  (An earlier run on this branch showed
                                                  TestHippocampusDocAnchorsResolve failing; that was a
                                                  STALE-BASE artifact, not a defect: the branch was based at
                                                  955bb91 while master had already advanced to 16dfe7e
                                                  "docs: re-anchor the two loop.go references that drifted
                                                  (1814->1826, 1993->2019)" — exactly that fix. Merged 16dfe7e
                                                  and the test passes. The ticket filed for it
                                                  (01a08dd3-dc9b-77c3-99b4-22afc661ce0a) was CANCELLED with that
                                                  reason so the queue does not re-issue it.)
                                                  Re-verified independently afterwards (spot checks, fresh
                                                  processes): TestPinCoverageTable / …IsNotVacuous /
                                                  TestEveryTempUsingSuiteIsPinned / TestScriptsAreBoundedAndOffTmp
                                                  / TestHostLegacyResidueIsSweptByTheSuitePin -> all PASS.
/dev/shm 'suite-tmp-' residue                   : 580 -> 63 (aged residue 0); 75 right after the first
                                                  sweep, 63 after a later test run — the pin-inline sweep
                                                  keeps ratcheting down, it is not a one-time cleanup
/dev/shm foreign entries (acc/diag/gate/verify) : 4/4 intact
/                          : device 2051 | scratch home (resolved) : /dev/shm, device 28
```

## 8. Coverage (rule: `scripts/func_coverage.py` must not drop)

Both runs are the SAME command in the SAME worktree, so the comparison is
like-for-like:

```
$ python3 scripts/func_coverage.py --json
BEFORE (baseline, when the worktree was pristine):
  {"total_funcs": 531, "covered": 446, "pct": 84.0}
AFTER  (this branch):
  {"total_funcs": 531, "covered": 448, "pct": 84.4}
```

`total_funcs` is unchanged (531) and `covered` **rose by 2** (446 → 448, 84.0% →
84.4%). The only package whose numbers moved is the one this task edited:

```
internal/testutil   68 funcs   34 covered -> 68 funcs   36 covered
```

So the new exported sweep (`GCLegacyScratchResidue`,
`…OlderThan`, `MemoryBase`, `LegacyScratchBases`, `IsLegacyScratchResidue`) and
the widened detector all enter covered, and nothing dropped.

## 9. Commits

Branch `feat/scratch-residue-audit`, base `955bb91` (5 commits):

| commit | content |
|---|---|
| `404cbd5` | bounded legacy-residue sweep + implicit boot-device fallback refusal (B) |
| `d7b6d3b` | /tmp gate extended to Go test sources + probe binaries off `/tmp` (C) |
| `783d707` | audit report + the peer1 producer correction (§1) |
| `bc3a9a5` | exact coverage numbers (531 funcs, 446→448 covered, 84.0→84.4%) |
| `5480687` | leak-closed proof + the full-tree run result |
| `4c18b9c` | exact branch/commit table in both reports (recovery path, Rule 6d) |
| `94777b4` | merge master `16dfe7e` — clears the stale-base anchor failure |
| `fb42136` | ticket-ledger correction (1 cancelled STALE, 1 cancelled DUPLICATE) |
| `05a86bf` | reproducible (A) producer measurement |
| `final` | full-tree GREEN evidence + residue re-measurement |

Every commit carries its own `go test` evidence in its message, per Rule 5/10.5.
The branch is in a worktree, so these commits are already on the branch in the
shared repo (`git show feat/scratch-residue-audit:devdoc/scratch-residue-audit.md`)
— no push required (Rule 6c).
