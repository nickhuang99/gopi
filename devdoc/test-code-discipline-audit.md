<!-- doc-class: normative -->
# Task Report: test-code discipline audit (C) — resource, isolation, effectiveness

**Epic**: Test-suite rigor audit, part (C): review the TEST code for resource
discipline, isolation discipline and effectiveness, and file structured bug
tickets for what is not fixed here.
**Branch**: `feat/scratch-residue-audit` (worktree `~/workspace/gopi-scratch-audit`).
**Parent report**: `devdoc/scratch-residue-audit.md` (A + B + the pin table).

---

## 0. Scope, measured (not quoted)

| | lines |
|---|---|
| test code (`*_test.go` under `internal/`, `cmd/`, `testsuite/`) | **89,801** |
| production code (`*.go`, same trees, no `_test.go`) | **74,494** |

The test tree is ~1.2× the production tree. Test files counted: **324**.
The briefing's 86,737 / 72,473 is stale (the tree grew); the ratio did not move.

Method: mechanical scans (grep/python, listed per row below), then hand reading
of every hit that the scan alone cannot judge (a `t.Skip` with a condition, a
`/tmp` string that is data rather than a write). Nothing was deleted and no test
was declared vacuous — see §3.

---

## 1. Resource discipline

| measure | count | judgement |
|---|---|---|
| `t.TempDir()` | 965 | the isolation mechanism; up from 940 at the briefing's snapshot |
| `time.Sleep` | 55 | **1** is ≥ 1s: `internal/tools/memory_sync_delta_test.go:1349` sleeps 2s because the rank guard adopts an equal-tier patch only when `updated_at` is **strictly later** at second granularity — a real requirement, documented in place, and the file already routes the other waits through `syncTestSleepToNextSecond()`. Not a flake-hider. |
| `exec.Command` | 152 | **0** files that call `.Start()` without a `Kill`/`Signal`/`Wait` in the same file — the process-leak class the epic asked about is empty |
| test-built binaries left behind | 2 → **0** | see §1.1 |
| unbounded loops in test load generators | 0 | already audited by the merged ban's scan; re-checked |
| `make([]byte, 10^6+)` buffers | 0 | none |

### 1.1 FIXED — probe binaries were built into fixed `/tmp` paths

`cmd/agent/cli_version_test.go` and `cmd/agent/home_settings_test.go` compiled the
gopi binary into fixed, world-writable, **executable** paths
(`/tmp/gopi-version-test`, `/tmp/gopi-home-settings-test`). Measured leftovers on
this host: `19918061` bytes (`Sep 11 07:35`) and `19918061` bytes (`Sep 8 22:19`),
on the 87%-full `/` partition — and nothing age-GCs them (the scratch GC only
sweeps its own namespace under the scratch home).

Two problems, not one:

1. **Leak** — 20MB per test binary per build, never reclaimed.
2. **Predictability** — the test *executes* whatever sits at that fixed path. A
   stale binary from another checkout, or a planted symlink, decides what runs.
   That is a supply-chain-shaped hazard in CI, and it is invisible to a
   functional test that passes.

Both now build into the suite's pinned scratch root
(`filepath.Join(testutil.SuiteTempRoot(), …)`), which is off `/` (device 28 vs
2051) and removed with the run root. `settings_nilpanic_test.go`'s
`os.MkdirTemp("", …)`-with-no-cleanup is the same class and is ticketed
(`01a08dcd-1ed0…`), together with the 3× duplication of the `gopiBinOnce` cache
that made three different destinations possible in the first place.

**Executed proof that the leak is closed** (the offender is not merely "written
somewhere else" — it is no longer written at all):

```
$ stat -c %Y /tmp/gopi-version-test /tmp/gopi-home-settings-test
1789083317  1788877178
$ go test -count=1 -run 'TestVersion|TestHomeSettings' ./cmd/agent/
ok  github.com/nick/gopi/cmd/agent  1.323s
$ stat -c %Y /tmp/gopi-version-test /tmp/gopi-home-settings-test
1789083317  1788877178        # IDENTICAL — the fixed /tmp paths were not touched
```

Both tests pass (so they still exercise the binary they build), and the two
pre-existing leftovers — `19918061` bytes and `20319565` bytes on the `/`
partition — are now exactly what they are: dead files an operator deletes once
(`rm -f /tmp/gopi-version-test /tmp/gopi-home-settings-test`).

### 1.2 NEW GATE — the `/tmp` ban now covers Go test sources

The merged ban's `TestScriptsAreBoundedAndOffTmp` audited `scripts/`, `devdoc/`
and `testdata/` — **not** the Go test sources, which is exactly where the `/tmp`
writes had survived. `assertTestSourcesCreateNoTmpPath` closes that:

- scans all **324** test files;
- flags a `/tmp` path **only when a creating call consumes it on the same line**
  (`os.MkdirTemp`, `os.CreateTemp`, `os.Mkdir(All)`, `os.WriteFile`, `os.Create`,
  `os.OpenFile`, `os.Remove(All)`, `os.Chdir`, `os.Symlink`, `go build -o`);
- three reviewed exemptions, each asserted to still exist (a stale exemption is
  an error, so it cannot quietly widen into a hole):
  the gate's own rule tables, the detector's synthetic samples, and
  `internal/shot/cov_shot_x11_test.go` (it *removes* the X server's own
  protocol-mandated `/tmp/.X<n>-lock` in the cleanup of a private Xvfb it started
  — a path the X11 protocol decides, not a repo choice).

**Why the predicate is narrow — measured, not assumed:** **125** test lines carry
an *inert* `/tmp` string (a path handed to an argument parser, a
`{"path": "/tmp/x"}` inside a mocked tool call, an expectation compared against a
parsed value, a comment). A gate that reddened on those would be red on the day
it landed and disabled by the next contributor — the failure mode the
`internal/standards` gates explicitly call out in their "deliberately rejected"
sections. Fixed vs inert is decided by *who consumes the literal*, and that is
what the predicate encodes.

Evidence:

```
$ go test -count=1 -run TestScriptsAreBoundedAndOffTmp -v ./internal/testutil/
    scripts_gate_test.go:158: Go /tmp gate: 324 test files scanned, 3 exempted by name (3/3)
--- PASS: TestScriptsAreBoundedAndOffTmp
$ go test -count=1 -run TestTmpPathCreationPredicate -v ./internal/testutil/
--- PASS: TestTmpPathCreationPredicate      # 10 rows: build-into-tmp/mkdirtemp/removeall/chdir → true;
                                            # inert-arg/inert-compare/inert-map/comment → false
```

### 1.3 TICKETED — scratch written into the developer's real `$HOME`

`internal/tools/read_test.go:88-102` (`TestRead_TildeExpansion`) writes
`~/gopi-read-test-<suffix>.txt` — the real home, the one tree on this host that is
not scratch, on a machine whose entire scratch contract is "a test never touches
the developer's real state". It is safe only because of a `defer os.Remove`; any
path that skips the defer (a panic in another test in the same binary, `os.Exit`,
a SIGKILL of the test binary) leaves the file behind. The suffix is not random
either: `randomSuffix()` returns `filepath.Base(os.TempDir())`, the same constant
string for every call in a run.
Ticket: `01a08dcd-00d8…`.

---

## 2. Isolation discipline

| measure | count | judgement |
|---|---|---|
| `os.Setenv` in tests | **49** | Rule 17 mandates `t.Setenv` → ticketed |
| `t.Setenv` in tests | 1,058 | the established pattern; the migration is the remaining work |
| files with `os.Setenv` and **no** restore at all | 2 (6 sites) | worst shape, see below |
| `os.Chdir` | 15 | 8 with an unchecked restore → ticketed (two sibling sites are the correct model) |
| `t.Parallel` | **0** | no cross-test parallelism exists today |
| `go test -shuffle` in CI | not found | ticketed as the follow-up |

### 2.1 The `os.Setenv` shape that is actually wrong

Not every `os.Setenv` leaks — most files pair it with a restore. The real defect
is the **deferred restore that cannot unset** (6 sites, 2 files:
`internal/agent/taskstartsearch_test.go:92-94,124-126`,
`internal/agent/taskendstore_test.go:82-84`):

```go
old := os.Getenv("GOPI_MEMORY_DB")          // "" when it was previously UNSET
os.Setenv("GOPI_MEMORY_DB", dbPath)
defer os.Setenv("GOPI_MEMORY_DB", old)      // leaves it SET TO THE EMPTY STRING
```

`os.Getenv` cannot tell "empty" from "unset"; `os.LookupEnv` can, and a sibling
test that asserts absence (or a consumer that branches on `LookupEnv`) sees a
polluted process environment. The restored value is also a path inside a
`t.TempDir()` the framework has already removed. This is precisely why Rule 17
exists, and it is the reason the migration is worth doing rather than cosmetic.

### 2.2 The `os.Chdir` shape that is actually wrong

`internal/tools/search_test.go:26-27` restores with an **unchecked**
`os.Chdir(oldwd)` (8 such sites in all: also `internal/agent/loop_test.go:519`,
`cmd/agent/cli_tasks_merged_cleanup_test.go:134`, and 5 in
`cmd/agent/cli_resume_worktree_test.go`). If the restore fails, every later test
in the binary runs with a working directory inside a removed `t.TempDir()` — a
cascade of confusing relative-path failures whose reported cause is never the
real one. Two sibling sites do it correctly and are the model
(`internal/tools/read_test.go:41-44` discards the error *visibly*,
`memory_sync_task_gitpushback_cov_test.go:73-76` checks it inside `t.Cleanup`).
Go 1.24 is in use, so `t.Chdir` (which registers its own restore and reports
failure) is the smallest correct change for all 8 sites.
Ticket: `01a08dce-7277…`.

### 2.3 What is NOT a problem

- **No `t.Parallel`** anywhere: there is no cross-test concurrency for an
  env/cwd leak to race today, so the two defects above are order-dependence
  hazards (and `go test -shuffle` exposure), not live flakes.
- **Global state in the test binaries**: the `gopiBinOnce` package var is the one
  lru-ish global, and it is guarded by `sync.Once`-style lazy init — the defect
  there is duplication (§1.1 ticket), not a data race.

---

## 3. Effectiveness

### 3.1 `t.Skip`: 193 sites — inspected, no unjustified skip found

80 condition-less `t.Skip(...)` + 107 `t.Skipf(...)`. A mechanical scan looked
for the only form that hides a failure — a skip whose preceding 12 lines contain
no `if`/`switch` (i.e. one that is not guarded by a capability probe) — and found
**0 candidates**. Every condition-less skip carries its own reason in the
argument and probes a real absence:

```
"ctags not installed"                 (8 sites — the ctags-backed tools skip without it)
"Xvfb is not installed — live actuation cannot be exercised"
"no git binary in test env"
"running as root — permission checks are bypassed"
"already inside the re-exec child"     (the re-exec helpers)
"cannot determine home directory"
```

Two design notes worth recording, because a future reviewer will otherwise
re-flag them: (a) the four `internal/testutil` suites use `t.Skipf` when the
host's memory filesystem or `/tmp` geometry makes an assertion *undemonstrable* —
"skip, never fail" is correct there, since the assertion is about the host, and
each skip prints why; (b) `internal/tools/read_test.go:96` skips when `$HOME`
cannot be resolved — legitimate, and unrelated to §1.3's unrelated defect.

### 3.2 Vacuous assertions: none found, and no test was deleted

The epic allows deleting a provably vacuous test if it is listed with evidence
separately. The scan produced **no candidate**, so **no test was deleted**:

- tests asserting nothing: a scan for a test body whose only statements are
  `t.Log`/`t.Skip` found only the *deliberate re-exec children*
  (`TestScratchReexecTrivial`, `TestHelperWorktreeCwdHolder`) — each is a probe
  whose value is that the parent observes it running (or not), and each says so
  in its comment. Deleting either would silently remove the evidence the
  corresponding parent test asserts against.
- `if err != nil { t.Fatal } `-only tests: none found by the scan.
- The two "would-be double coverage" candidates hand-checked:
  `TestAssertScratchRootOnTmpFails` (unit: the error value) and
  `TestSuiteRefusesToRunOnTmp` (end-to-end: the exit code and the wiring) assert
  different halves; the same holds for the new
  `TestGCLegacyScratchResidue*` unit tests vs the host post-condition test.

### 3.3 Bug tickets filed (structured, `tags=[bug]`, severity in the payload)

| task id | severity | headline | directory |
|---|---|---|---|
| `01a08dcc-fef2-7a22-b47b-b48d7031a00b` | high | test-built gopi binaries written to fixed `/tmp` paths, never cleaned | `cmd/agent` |
| `01a08dcd-00d8-7aa2-9186-823f3fed46f1` | high | test writes scratch into the developer's real `$HOME` | `internal/tools` |
| `01a08dcd-0401-7cd9-9c69-969876a43276` | medium | `os.Setenv` leaks process state (49 sites) | `internal/agent` +4 |
| `01a08dcd-1ed0-7797-94fb-7ae55a1853a6` | medium | the gopi-binary build cache is duplicated 3× | `cmd/agent` |
| `01a08dd0-8b9a-71dd-bc3c-74e22ab26fdf` | low | 8 unchecked `os.Chdir` restores | `internal/tools`, `internal/agent`, `cmd/agent` |

**Ticket-hygiene actions taken after filing** (both verified in the ledger):
`01a08dd3-dc9b-77c3-99b4-22afc661ce0a` was **cancelled — STALE**: it reported
`TestHippocampusDocAnchorsResolve` failing, but that was a stale-base artifact
(master `16dfe7e` had already landed the re-anchor `1814->1826`); merging master
into this branch makes the test pass, so the ticket was withdrawn rather than
left for an agent to re-discover nothing was broken.
`01a08dce-7277-7415-acc4-c730c070e312` was **cancelled — DUPLICATE** of the
8-site `os.Chdir` ticket above (the 1-site one was filed 3 minutes earlier and
is strictly narrower); two overlapping tickets would let two agents collide on
the same files. Net: **5 live tickets** from this audit, not 7.

Each carries: the repro command, the site list with line numbers, why it matters,
a fix direction that stays inside test code, and an acceptance check. They are
deliberately **not** fixed here: §1.1's two sites were fixed because the new gate
could not adopt (a red gate gets disabled), while these five need their own
`go test ./cmd/agent/` (≈42s) and `./internal/tools/` (≈155s) acceptance cycles —
delegating them is the epic's own instruction ("the rest delegated as subtasks").

---

## 4. Not Done

- The five tickets above (delegated, each self-contained).
- **`go test -shuffle=on`** full-tree campaign: `t.Parallel` is 0, so shuffle is
  the only way to expose the residual env/cwd coupling — a multi-minute run,
  filed as the follow-up to the `os.Setenv`/`os.Chdir` tickets.
- **Production `/tmp` writers are out of scope by the epic's own constraint**
  (do not modify product code). For the record, the ones found while
  cross-checking the test side, all already inside the merged ban's justification
  table: `internal/shot/x11.go`'s `x11/tmp/.X11-unix` (protocol-mandated read
  path), `internal/tools/write.go`'s `CreateTemp(dir, …)` (atomic-rename temp in
  the target's own directory — correct), and `plugins/websearch/remotebox/*`
  (a deployed remote service).
- **A repo-wide `os.Setenv` ban gate** (the analogue of §1.2 for Rule 17):
  buildable, but premature while 49 sites are still baselined — the ticket is the
  adoption mechanism, the gate is the ratchet, exactly as `internal/standards`
  documents for its own five gates.

---

## 5. Verification

```
grep -c lines                       : test 89,801 | prod 74,494
grep -rn 'time.Sleep' *_test.go     : 55 (1 ≥ 1s, documented)
grep -rn 'exec.Command' *_test.go   : 152 (0 unpaired Start)
grep -rn 'os.Setenv(' *_test.go     : 49 → ticket
grep -rn 'os.Chdir(' *_test.go      : 15 (8 unchecked restores → ticket)
grep -rn 't.Parallel' *_test.go     : 0
grep -rn 't.Skip' *_test.go         : 193 (0 unguarded candidates)
go build ./...                      : OK
go test -count=1 ./internal/testutil/ : ok
go test -run TestScriptsAreBoundedAndOffTmp -v ./internal/testutil/ : PASS (324 scanned, 3/3 exempt)
go test -run TestTmpPathCreationPredicate -v ./internal/testutil/   : PASS
go test -count=1 ./cmd/agent/          : ok (1.3s targeted, 46.9s full)
/tmp probe-binary mtimes after a run   : UNCHANGED (the leak is closed)
```

## 6. Commits

Branch `feat/scratch-residue-audit` (worktree `~/workspace/gopi-scratch-audit`),
base `955bb91`; the two commits this report owns:

| commit | content |
|---|---|
| `d7b6d3b` | /tmp gate extended to Go test sources + probe binaries off `/tmp` (§1) |
| `5480687` | leak-closed proof + the full-tree run result (§1, §5) |

The five tickets in §3.3 are ledger rows, not commits — deliberately delegated
(the epic's own instruction). The artifact stays on the branch in the shared
repo, so no push is needed (Rule 6c).
