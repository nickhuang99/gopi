<!-- doc-class: normative -->
# Task Report: /tmp ban — scratch root moved off `/`, bounded GC, script audit

**Epic**: Ban /tmp as a test/dev workspace in GoPi, mechanically.
**Branch**: `feat/ban-tmp-scratch` (worktree `~/workspace/gopi-tmp-ban`)
**Incident**: `devdoc/peerbox-resource-exhaustion-20260911.md` (2026-09-11: `/` 19G → 1.5G free)

---

## 0. Why this is mechanical now

Three separate entry points wrote throwaway data into `/tmp`, which on this host
lives on the 147G `/` partition (87% full, `/dev/sda3`, device id **2051**):

1. **Go suites** — `internal/testutil/scratchroot.go`'s `PinSuiteTempRoot`
   defaulted to `/dev/shm`, falling back to the ambient temp root, i.e. `/tmp`.
   Every `t.TempDir()` in 14 test packages followed it.
2. **Shell/Python scripts** — `mktemp -d` / `tempfile.mkdtemp()` default to
   `/tmp`, so every e2e/acceptance harness wrote there.
3. **Production code paths** — job output files, web-search merge dirs, memsync
   staged envelopes, deepseek crash dumps, `text2img` staging, merge-resolve
   worktrees, and Chrome profile dirs (`os.TempDir()` / `os.MkdirTemp("")`).

An incident is a symptom; the *mechanism* was that nothing **asserted** the
location and nothing **bounded** the tree. Both are now code.

---

## 1. Completed

### (A) Locate + assert — `internal/testutil/scratchroot.go`

- **Default scratch root** is `<home>/workspace/gopi-test-scratch/<suite>/<run>`
  (`PinSuiteTempRoot`), **never** `os.TempDir()`. `SuiteTempRootDir()` = the
  workspace scratch home; `/dev/shm` is no longer a default (it stays referenced
  only as the memory backing the setup script links to).
- **Hard assertion** `AssertSuiteScratchRootSafe(suite)` / `FatalAssertSuiteScratchRootSafe`:
  compares the scratch root's **device id** with `/`'s; on a match it fails the
  suite with the device ids, the incident, and the **fix**
  (`mount -t tmpfs …` / `scripts/setup-scratch-home.sh`), exiting with the
  documented `SuiteScratchUnsafeExitCode = 97` **before a single test runs**.
  All numbers/paths are named constants (Rule 1a/1b/13).
- **Path canonicalisation** (`scratch.CanonicalPath`, one definition, Rule 14):
  on an unprivileged host the scratch home is a **symlink** onto a memory
  filesystem (`scripts/setup-scratch-home.sh`:
  `~/workspace/gopi-test-scratch → /dev/shm/gopi-test-scratch`). `getcwd(2)`
  always reports the *resolved* path, so a symlinked spelling made every test
  comparing a `chdir`'d cwd against its own scratch path red
  (`cmd/agent TestResumeChdirToWorktree`). Found by running the suite, not by
  reading it — it is the one non-obvious change in this deliverable.

### (B) Bound — age + holder GC

- `GCStaleScratchRoot()` = `GCStaleScratchRootOlderThan(ScratchStaleAge)` with
  `ScratchStaleAge = 6 * time.Hour`, run **inline on every pin** (so the tree is
  swept whenever anyone runs a test) and callable standalone by a watchdog
  (`GOPI_TEST_SCRATCH_GC_MIN` is the one env reader, Rule 2).
- **HARD RULE — never delete a directory with a live owner.** Before any
  `RemoveAll` the sweep consults both holder channels:
  1. `scratchDirHolder` — scans `/proc/<pid>/cwd` + `/proc/<pid>/fd` for a live
     process inside the directory, comparing **resolved** link targets per
     component prefix (a process in `dir/sub/x` holds `dir`).
  2. `ScratchHolderAlive` — the permission-free channel: `PinSuiteTempRoot`
     writes a `.holder` file (`pid` + kernel **start time**, so pid reuse is
     detectable) and the sweep reads `/proc/<pid>/stat`.
- **Honest limitation, documented in the code**: on this shared host with
  `ptrace_scope=1`, `/proc/<pid>/cwd|fd` is **not readable for same-user
  siblings** (179 unreadable entries measured). So an unreadable entry is
  skipped, never treated as a holder (treating it as one froze the GC
  entirely); the hard rule holds for every *observable* holder, and the
  `.holder` registration is what protects a long-lived suite exactly.
- Advisory `.gc.lock` in the scratch home (stale after `gcLockStaleAge`) so
  concurrent suites never race the same directory; an unmeasurable entry
  (vanished / permissions / dangling symlink) is **kept**, never guessed away
  (Rule 15). Scan bounded by `scratchScanLimit`.
- Suite self-clean: `FinishSuiteForTestMain` / `CleanupSuiteTempRoot` remove the
  per-run root; `TestScratchDir(t, sub)` is the bounded per-test directory.

### (C) Scripts audit + static gate

Two bugs were found by *running* the helpers rather than reading them — both are
now regression-tested:

1. **Subshell registry leak** — the natural caller form
   `WORK="$(gopi_scratch_mkdir sub)"` runs in a **subshell**, so a cleanup
   registry held in a shell *variable* never reached the parent: every caller
   leaked its scratch directory until the age GC caught it. The registry is now
   a **file** (`gopi_scratch_registry`), and
   `TestScratchHelperCleanupSurvivesSubshell` fails if it regresses to a
   variable.
2. **`m1_acceptance.py` had no `--dry-run`** — the epic requires every touched
   script to be dry-run verifiable; it was added (and asserts the scratch queue
   off `/` as part of the plan).
3. **`scripts/setup-scratch-home.sh` linked a directory to its own ancestor** —
   its symlink route had no guard for a scratch home that resolves at or
   **inside** the memory-backed tree: `GOPI_TEST_SCRATCH_HOME=/dev/shm/gopi-test-scratch`
   (or a subdirectory of it) was replaced by a link to the directory that
   *contains* it. The host carried the artifact of exactly that —
   `/dev/shm/gopi-test-scratch/gopi-test-scratch → /dev/shm/gopi-test-scratch`
   (Sep 11 06:30) — a loop every directory walk chases, and the migration route
   would have `cp -a`'d the tree onto itself and then deleted it. Reproduced
   under a private `GOPI_SCRATCH_MEMORY_BASE`, fixed with a resolved-path guard,
   the stray removed, and regression-tested
   (`TestSetupScratchHomeRefusesToLinkADirectoryToItsOwnAncestor`, three routes:
   inside-the-tree refused, tree-itself a no-op, healthy route idempotent).

- **New shared helpers** (one contract, three languages):
  `scripts/lib/tmp-ban.sh` (199 lines), `scripts/lib/tmp_ban.py` (211 lines) —
  scratch home/queue resolution, **device-id assertion**, trap-time child kill +
  scratch removal, `gopi_bounded_loop` (iteration cap **and** `timeout`),
  `bounded_loop`/`run_bounded` for Python, dependency preflight, `--dry-run`
  reporting. Sourcing the shell helper deliberately does **not** set shell
  options.
- **`scripts/setup-scratch-home.sh`** (new, `--dry-run`): mounts a
  tmpfs at the scratch home when privileged, else symlinks it to
  `/dev/shm/gopi-test-scratch` (the memory base is read once from
  `GOPI_SCRATCH_MEMORY_BASE`, default `/dev/shm`). It is idempotent **by
  construction**: it refuses the one destructive input — a scratch home that
  resolves at or inside the memory tree — instead of linking a directory to its
  own ancestor (see bug 3 above; the host's stray such link has been removed).
  **Run on this host** — the scratch home is now device **28 ≠ 2051**.
- **Migrated** (all now `bash -n` clean, all gained `--dry-run` + a
  `gopi_scratch_cleanup EXIT INT TERM` trap): `e2e-m418-yield.sh`,
  `e2e-m420-rank.sh`, `e2e-m421-claim-lock.sh` (**since DELETED** — it drove
  the cross-node claim lock, removed in batch 4), `e2e-merge-backlog.sh`,
  `e2e-task-merge.sh` (both `mktemp -d` uses dropped), `e2e_smoke.sh`,
  `migrate-sessions.sh`, `m1_acceptance.py`, `m2_acceptance.py`,
  `test_ds_pricing_snapshot.py`, `test_func_coverage.py`, and
  `devdoc/memsync-e2e-sim.sh` (the last `/tmp` `mktemp -d` dev harness).
- **Static gate** `TestScriptsAreBoundedAndOffTmp` (`internal/testutil/scripts_gate_test.go`):
  audits `scripts/`, `devdoc/`, `testdata/` and fails on (1) a `bash -n` parse
  error, (2) an unbounded `while :` / `while true` / `until false` / Python
  `while True:` whose body carries no `timeout`/deadline/counter, (3) a `/tmp`
  literal outside the ban's own helper files, (4) a scratch-creating script that
  does not source/import the shared helper. Vacuous-pass guarded.
- **`TestEveryTempUsingSuiteIsPinned`** — the Go-side blast-radius gate: any test
  package that calls `t.TempDir()`/`os.MkdirTemp("")` must have a `TestMain`
  routing the pin through `internal/testutil`. This is what stops a new package
  from silently re-introducing the bug.
- **In-test load generators audited**: every `for {` loop in the repo's
  `_test.go` files carries a deadline/break/return bound (checked mechanically
  with a scan of all 40-line windows; zero unbounded). `plugins/websearch/remotebox/*`
  is a deployed remote service, not a dev/test scratch workspace — out of scope,
  recorded below.

### TestMain wiring (the epic's entry-point list)

| Suite | Wiring |
|---|---|
| `internal/tools`, `internal/agent`, `cmd/agent` | existing TestMains, re-pointed to the shared implementation (`PinSuiteForTestMain` / `FatalAssertSuiteScratchRootSafe`) |
| `internal/testutil` | `PinSuiteTempRoot` + `FatalAssert…` (dogfoods the ban) |
| `internal/checkpoint`, `internal/config`, `internal/deepseek`, `internal/gopilog`, `internal/hippocampus`, `internal/pricing`, `internal/sessionevent`, `internal/shot`, `internal/text2img` | **new** `TestMain` → `RunSuiteMain` |
| `testsuite` | **new** `TestMain` → `RunSuiteMain` (26 `t.TempDir()` calls) |
| `internal/proxy`, `internal/taskverdict` | no temp use — the gate confirms neither needs a pin |

### New tests (the epic's list)

| Acceptance | Test |
|---|---|
| scratch root on `/tmp` → **hard fail** | `TestSuiteRefusesToRunOnTmp` (re-execs the test binary with `GOPI_TEST_TMPDIR` on `/`, asserts exit **97**, asserts the message names "SAME filesystem as /" + "FIX:" + the env var, asserts **no test body ran**); `TestAssertScratchRootOnTmpFails` |
| 7h-old orphan → GC'd, fresh → kept | `TestGCStaleScratchRemovesOldOrphanAndKeepsYoung`, `TestGCStaleSweepBoundedAgeBoundary` |
| in-use → untouched | `TestGCKeepsScratchHeldByALiveProcess` (real `sleep` child with the dir as cwd), `TestGCKeepsScratchHeldByOpenFileDescriptor` |
| concurrent GC safe | `TestGCConcurrentSweepsAreSafe` (6 parallel sweeps, exactly-once removal) |
| unreadable dir → no panic | `TestGCUnreadableScratchEntryDoesNotPanic` (permission-locked + dangling symlink, both kept) |
| misplaced/missing home | `TestGCMissingHomeIsANoOp`, `TestScratchHomeIsNeverTheAmbientTempRoot`, `TestSuiteScratchRootIsPinnedUnderTheScratchHome`, `TestTestScratchDirLandsInTheQueueAndIsSelfCleaning`, `TestScratchRootSharesRootFSAnswersFromStatfs`, `TestPathWithinDoesNotMatchSiblingPrefixes`, `TestScratchHomeMountHintNamesTheFix` |
| scripts gate | `TestScriptsAreBoundedAndOffTmp`, `TestEveryTempUsingSuiteIsPinned`, `TestScratchHelperCleanupSurvivesSubshell` |
| setup script cannot loop a scratch dir onto its own ancestor | `TestSetupScratchHomeRefusesToLinkADirectoryToItsOwnAncestor` (runs the real script against a private memory base, three routes) |

---

## 2. Temp-root inventory (audited, every `/tmp` writer found)

### Migrated to the scratch tree

| Site | Was | Now |
|---|---|---|
| `internal/testutil` suite pin (14 packages) | `os.TempDir()` / `/dev/shm` | `<home>/workspace/gopi-test-scratch/<suite>/<run>` |
| `internal/tools/job.go` | `os.CreateTemp("")` | `scratch.CreateTemp(RuntimeClassJob, …)` |
| `internal/tools/websearch.go` | `os.MkdirTemp("")` | `scratch.MkdirTemp(RuntimeClassWebSearch, …)` |
| `internal/tools/memory_sync_session.go`, `memory_sync_reconcile.go` | `os.CreateTemp("")` | `scratch.CreateTemp(RuntimeClassMemsync, …)` |
| `internal/shot/shot.go` | `os.MkdirTemp("")` (Chrome profile) | `scratch.MkdirTemp(RuntimeClassShot, …)` |
| `internal/deepseek/error_1214.go` | `<temp>/gopi-1214-dumps` | `scratch.DumpDir("gopi-1214-dumps")` |
| `internal/deepseek/minimal_core.go` | `<temp>/gopi-dumps` | `scratch.DumpDir("gopi-dumps")` |
| `cmd/agent/merge_resolve_child.go` | `os.MkdirTemp("")` | `scratch.MkdirTemp(RuntimeClassMergeResolve, …)` |
| `cmd/agent/commands.go` (`/text2img`) | `os.TempDir()/gopi-slash-*.png` + staging temp | `scratch.DumpDir(RuntimeClassText2Img)` |
| `cmd/text2img-verify/main.go` | `/tmp/smoke-render/*` | `scratch.DumpDir(RuntimeClassText2Img)`, `GOPI_T2I_VERIFY_DIR` override |
| `scripts/*.sh`, `scripts/*.py`, `devdoc/memsync-e2e-sim.sh` | `mktemp -d` / `mkdtemp()` | `gopi_scratch_mkdir` / `scratch_dir()` / `make_scratch_dir()` |
| `cmd/agent/cli_*.go`, `internal/config/help.go` usage text | `--out /tmp/…` examples | `--out ~/workspace/out/…` |

### Remaining `/tmp` writers — with justification

| Site | Why it stays |
|---|---|
| `internal/shot/x11.go` `x11UnixSocketDir = "/tmp/.X11-unix"` | **Protocol-mandated**: the X11 protocol defines the abstract/unix socket path; it is a *read/connect* path for an already-running server, not scratch we create. Commented in place. |
| `plugins/websearch/remotebox/*` (`google-cdp-watchdog.sh` `while true`, `/tmp/chromeprofile`, `/tmp/*.log`, `google_cdp_server.py` `while True`) | A **deployed remote service** (systemd unit on the search-bot host, `Restart=always`), not a GoPi dev/test scratch workspace: its `while true` *is* the watchdog's job and its `/tmp` is that host's. Auditing it from this repo would be a false positive; the gate therefore scopes to `scripts/`, `devdoc/`, `testdata/`. Named here so the exclusion is explicit, not accidental. |
| Test files passing `"/tmp/x"` as **inert string data** (`internal/proxy`, `cmd/agent/cli_*_test.go` `--db /tmp/x.db`) | The path is parsed/compared, never created or read — a `/tmp`-shaped *input*, not a write. |
| `internal/tools/write.go` `os.CreateTemp(dir, …)` | Creates the temp file **in the target file's own directory** for an atomic rename — the correct behaviour, unrelated to `/tmp`. |
| Documentation/comments naming `/tmp` (help text, usage strings, incident docs) | Text, not paths that get created. |

---

## 3. Not Done

- **A nested legacy residue directory is deliberately NOT collected**: on this
  host `/dev/shm/gopi-test-scratch/tools-ext4probe/` (364K, `Sep 10 23:59`)
  holds legacy `gopi-suite-tmp-*` directories from a *manual* ext4 probe run by
  a previous session. The GC matches scratch names at the **home level** and
  never deletes a home entry it does not recognise — recursing into an
  unrecognised directory to delete its children would mean deleting data this
  system did not create, which the hard rule forbids. **Operator action**: `rm -rf ~/workspace/gopi-test-scratch/tools-ext4probe`
  once confirmed it is not wanted. Everything the *system* creates
  (per-run roots, the queue, runtime scratch) is now swept.
- **The scratch home's upkeep on a scheduler-less host**: the watchdog's CLOCK
  is installed (cron, `*/30`) — see §6. A host with a working systemd --user
  bus uses the shipped units instead (`scripts/systemd/`, installed by
  `scripts/install-scratch-gc-watchdog.sh`); **that path is UNVERIFIED on this
  host**, which has no user bus at all (no `$XDG_RUNTIME_DIR/bus` socket), so
  the units here are shipped-and-reviewable, not shipped-and-proven.
- **A host that never runs `install-scratch-gc-watchdog.sh`** is still exposed:
  the sweep needs either a scheduler entry or a resident worker/merger. Repo
  state cannot install a user's timer (a machine-level action), which is why
  the installer is a script with an idempotent crontab marker rather than
  something a merge could do.
- **`go test` work dirs are redirected but not audited for other repos**: the
  redirect lives in the machine-wide go env file (`~/.config/go/env`, now
  `GOTMPDIR=$HOME/workspace/gopi-go-tmp`), so it applies to every Go repo on
  this host by construction. A repo whose own env sets `GOTMPDIR`/`TMPDIR`
  explicitly overrides it — nothing in GoPi does.
- **`GOTMPDIR` is a redirect, NOT a reaper**: a *killed* `go` command leaves its
  `go-build*` work dir behind (that is how the 35 pre-existing `/tmp/go-build*`
  dirs got there). After this change those dirs land under
  `~/workspace/gopi-go-tmp`, which no sweep collects yet — the tree there is
  bounded by the go tool's own cache semantics, not by this system. Redirecting
  was the epic's option 2; collecting that tree is a separate follow-up.
- **`internal/testutil` GC does not descend into a *stale per-run root*** beyond
  the age rule: an abandoned root's children are collected with the root. This
  is intentional (the root's mtime does not move when children come and go) but
  means a root kept alive by a single fresh child survives — the `.holder` +
  age rule is the bound.
- The `symlink` form of the scratch home is host state, not repo state: a
  fresh host must run `scripts/setup-scratch-home.sh` once. It is idempotent and
  `--dry-run`-able, and `AssertSuiteScratchRootSafe` is what catches a host that
  has not run it.

---

## 4. Verification

```
go build ./...                              : OK
go vet ./internal/testutil/ ./internal/scratch/ ./internal/tools/ ./internal/agent/ : clean
gofmt -l internal/testutil internal/scratch : clean
go test ./internal/testutil/                : ok   (all ban + GC + gate tests PASS)
go test ./internal/tools/                   : ok   153.5s
go test ./cmd/agent/                        : ok   41.2s
go test ./internal/agent/                   : ok
go test ./testsuite/ ./internal/proxy/ ./internal/taskverdict/ : ok
bash -n (12 touched scripts)                : OK
python3 -c ast.parse (4 touched .py)        : OK
scripts/*.sh --dry-run, m1/m2 --dry-run     : scratch root reported off `/`, "nothing was executed"
GOPI_TEST_HOME=/tmp python3 scripts/m1_acceptance.py --dry-run
                                            : HARD FAIL, prints device 2051, the incident, and the FIX
stat -c %d ~/workspace/gopi-test-scratch /  : 28 vs 2051  (off `/`)
/`/tmp` df before/after a full run          : see §5
```

### The /tmp-growth acceptance (df before/after a full test run)

`/tmp` usage and entry count are sampled immediately before and after a full
`go test` of the pinned suites; the numbers are in §5 below. The scratch tree
itself lives on tmpfs (`/dev/shm`, 7.8G), so a run's scratch is reclaimed by the
kernel at worst and by the age GC at latest — and never on `/`.

---

## 5. Commit + measured evidence

- Branch: `feat/ban-tmp-scratch`, worktree `~/workspace/gopi-tmp-ban`.
- Verified: `git status` in `~/work/GoPi` (the main repo) lists **none** of the
  files edited here.

### Measured: a full pinned-suite run adds nothing to `/tmp`

Entry-list diff around a real run (`checkpoint`, `pricing`, `sessionevent`):

```
$ ls -1 /tmp | sort > before
$ go test -count=1 ./internal/checkpoint/ ./internal/pricing/ ./internal/sessionevent/
ok  checkpoint 0.458s | ok pricing 0.005s | ok sessionevent 0.193s
$ ls -1 /tmp | sort > after
$ comm -13 before after          # NEW /tmp entries
(empty — zero new entries)
```

Under a **full** acceptance run (`./internal/tools/ ./cmd/agent/` plus ten more
suites, ~220s), `/tmp` moved `123415M → 124275M` used and `2682 → 2688` entries.
Every one of those new entries is the **go tool's own build work dir**
(`/tmp/go-build*`, measured **129M** resident) — the `go test` driver creates it
before any test binary exists, so no repo-level change can move it. It is not
GoPi scratch, it is not created by the code under test, and it is removed by the
go tool itself; `GOTMPDIR` is the only lever and is documented as Not Done (§3).

The distinction that matters for the incident is proven by the same numbers: the
2026-09-11 exhaustion came from **13,044 abandoned `*/tmp` scratch dirs** plus an
unbounded `while :; do dd …; done`. After this change a run leaves **0** scratch
entries in `/tmp` and the scratch tree is 52M on tmpfs (`/dev/shm`, device 28),
with `/tmp` growth attributable solely to the compiler's build cache — bounded by
the go tool, not by a loop with no end.

Device check (the ban's core claim):

```
$ stat -c '%d %n' ~/workspace/gopi-test-scratch /
28 /dev/shm/gopi-test-scratch        # the symlink home RESOLVES here (testutil resolves before statfs)
2052 /home/user/workspace/gopi-test-scratch   # the link itself sits on /home — irrelevant, the resolved target is what the suite writes to
2051 /
```

### Final acceptance run (after the setup-script hardening)

```
CGO_ENABLED=0 go build ./...                        : build OK
go test -count=1 ./internal/testutil/ ./internal/tools/ ./cmd/agent/
    ok internal/testutil 0.932s | ok internal/tools 157.232s | ok cmd/agent 42.714s
df /tmp before → after                              : 127265620K → 127265632K  (delta 12K)
/tmp entry count delta                              : 0 new entries
go-build dirs at the end                            : 4 (the go tool's own, 5 mid-run)
gate re-run (bounded + pinned + subshell)           : ok 0.282s
device ~/workspace/gopi-test-scratch → /dev/shm …    : 28 vs / 2051  (off `/`)
```

Mid-run sample (while the trio was executing): `/tmp` held **5** `go-build*`
directories and **zero** GoPi scratch entries, while the run's own scratch roots
(9 live) sat under `~/workspace/gopi-test-scratch` — i.e. the run's scratch is
off `/` even at its peak, and the only `/tmp` bytes are the compiler's.

### Closing run (HEAD 2702233) — every new `/tmp` entry attributed

A later re-run of the same trio (`internal/testutil` 0.670s, `internal/tools`
133.115s, `cmd/agent` 38.692s, all `ok`) moved `/tmp` `127265744K → 127265944K`
and `2697 → 2700` entries. All three new entries were identified by `find
-newermt` inside the run window and **none is test scratch**:

| New `/tmp` entry | Created by |
|---|---|
| `gopi-job-*.out` / `gopi-job-*.err` (3 files, ≤1.2K) | The **live harness process** — `~/work/GoPi/bin/gopi` built `Sep 11 04:11`, i.e. **before** this branch existed, so it still runs the old `job.go` that wrote job output to `/tmp`. This branch's `job.go` calls `scratch.CreateTemp(scratch.RuntimeClassJob, …)`; the behaviour changes the moment master + this branch are rebuilt (Rule 12's close loop). Not a defect in the branch — a stale binary, and proof that the migration is the thing that fixes it. |
| `/tmp/mt*.out` (54B / 207K) | My own ad-hoc `git merge-tree` diagnostics in the shell, not the suite. |
| `go-build*` (0 new dirs; 4 pre-existing, newest `07:05` = before the run) | Nothing new — the compiler's cache dirs all predate the run. |

So the classification is exact: **0** entries from the test binaries, all three
from the harness/diagnostics layer. The acceptance claim ("after a full test run,
`/tmp` does not grow from scratch") holds; the residue is a stale binary plus my
own shell scratch, both outside the deliverable.

---

## 6. The host-level upkeep path (2026-09-11, follow-up to §3)

§3's last two entries — "no systemd/watchdog unit for the standalone GC" and
"No `/tmp`-ban for `go test` cache / build work dirs (`GOCACHE`, `GOTMPDIR`)" —
are **closed here, with the measurements below**. The mechanism that made them
survivable is exactly what the 2026-09-11 exhaustion was: *nothing bounded the
tree* on a host that stops running suites.

### 6.1 The gap, precisely stated

`internal/testutil`'s sweep is **inline in the test pin**, and the resident half
(`cmd/agent/scratch_gc_tick.go`, committed ahead of this work) rides the
**worker/merger tick**. Both need something to be running. A host that runs no
suites and keeps no resident gopi — a build box, a workstation between tasks —
is bounded by neither: an orphaned per-run root from a suite killed mid-run
sits there until the next test run. `GOPI_TEST_SCRATCH_GC_MIN` had a reader
(Rule 21 gate 6 closed that), but a *cadence* is not a *trigger*. Nothing in the
repo could fire the sweep; only the OS scheduler can.

### 6.2 What was added (no new tool — Rule 3)

| Piece | What it is |
|---|---|
| `gopi scratch-gc` (`cmd/agent/cli_scratch_gc.go`) | The **one-shot** sweep: synchronous, prints one logfmt line, exits. A CLI verb under `cmd/agent` — explicitly allowed by the epic, because the LLM tool surface is frozen and a resident process is the thing this face exists to avoid. Thin: it calls `internal/scratch.SweepHome`, the same function the tick calls, so the hard rule lives in ONE place. |
| `--dry-run` (`internal/scratch.SweepHomePlan` + `scratch.Collectable`) | The vetting face. The collectability predicate was **extracted** and is now shared by the real sweep and the plan, so "would-remove" can never promise something the sweep would refuse. |
| `scripts/scratch-gc-watchdog.sh` | The scheduler's entry point: explicit PATH (a scheduler does not source your profile), an flock (one run at a time), a durable log under `/home/user/.gopi/logs/`, and a **stale-binary probe** — it runs `scratch-gc -h` and refuses to invoke a binary whose usage head is not this verb's, because an *unrecognised first argv word is a prompt to the agent loop* (a stale watchdog would otherwise quietly burn an LLM session every 30 minutes instead of sweeping). |
| `scripts/systemd/gopi-scratch-gc-watchdog.{service,timer}` + `scripts/install-scratch-gc-watchdog.sh` | The 30-minute clock: systemd --user when a user bus exists, **cron** otherwise (`*/30`, idempotent via a crontab marker). |

Cadence rationale (30 min): an order of magnitude finer than `scratch.StaleAge`
(6h), so abandoned scratch is collected within a fraction of its own staleness
window; the resident tick's default is 60 min. On a host with neither face the
30-minute one is the *only* one — that is why it is the finer of the two.

### 6.3 Acceptance (a) — the sweep runs with NO test running

The clock is installed in the user's crontab (this host has **no systemd --user
bus** — no `$XDG_RUNTIME_DIR/bus` socket — so cron is the mechanism here):

```
$ crontab -l | grep gopi-scratch-gc-watchdog
*/30 * * * * /home/user/.local/bin/gopi-scratch-gc-watchdog.sh >/dev/null 2>&1 # gopi-scratch-gc-watchdog (30min, installed by …)
```

One **scheduler-triggered** execution (a temporary `* * * * *` entry, cron
fired it, the entry was then removed), no test process involved:

```
2026-09-11T10:38:01+08:00 scratch-gc-watchdog scratch-gc role=cli home=/dev/shm/gopi-test-scratch removed=1 stale=6h0m0s disabled=false
2026-09-11T10:39:01+08:00 scratch-gc-watchdog scratch-gc role=cli home=/dev/shm/gopi-test-scratch removed=0 stale=6h0m0s disabled=false
```

(2 lines = 2 ticks; the second found nothing left, which is the residue-free
steady state.) Durable copy of these lines: `/home/user/.gopi/logs/scratch-gc-watchdog.log`.

### 6.4 Acceptance (b) — a 7h-old orphan is gone with no test running

A per-run root planted exactly as a killed suite leaves one, backdated 7h
(older than the 6h `StaleAge`):

```
$ ORPHAN=/dev/shm/gopi-test-scratch/suite-cmd-agent-run-20260911999
$ mkdir -p "$ORPHAN" && touch -d '7 hours ago' "$ORPHAN"
$ ls -ld "$ORPHAN"        → drwxrwxr-x … Sep 11 03:37   (age 7h)
… cron tick (no test running) …
$ ls -ld "$ORPHAN"        → GONE          # 10:38:01 log line above: removed=1
```

Re-verified a second time, independently, on the **developer's real** scratch home
(the symlinked one, `~/workspace/gopi-test-scratch` → `/dev/shm/gopi-test-scratch`),
with no test process alive — a held-free 7h orphan planted there was gone after one
watchdog run and the log line names it by its symlink-resolved target:

```
$ mkdir -p ~/workspace/gopi-test-scratch/suite-cmd-agent-run-20260911zzz
$ touch -d '7 hours ago' ~/workspace/gopi-test-scratch/suite-cmd-agent-run-20260911zzz
2026-09-11T10:43:27+08:00 scratch-gc-watchdog scratch-gc role=cli home=/dev/shm/gopi-test-scratch removed=1 stale=6h0m0s disabled=false
```

The hard rule held in the same run: the developer's real scratch home holds
hundreds of **fresh, live** roots (they survived — `removed=0` on the next
tick), and the unit/E2E tests pin the veto directly
(`TestScratchGCCLI_NeverRemovesHeldRoot`: a stale root registered via
`RegisterHolder` to a LIVE pid survived while its unheld neighbour was
collected).

### 6.5 Acceptance (c) — no `/tmp` growth from a `go build ./...`

`GOTMPDIR` is now set in the machine-wide go env file, and `GOCACHE` was
confirmed already off `/tmp`:

```
$ cat /home/user/.config/go/env
GOTMPDIR=/home/user/workspace/gopi-go-tmp
$ go env GOCACHE
/home/user/.cache/go-build
```

Where the go tool's work dir lands — the lever, A/B on the same command
(`GOENV=off` = no env file, i.e. the default):

```
GOENV=off (no env file → the DEFAULT) → WORK=/tmp/go-build1708391629
env file (GOTMPDIR redirected)        → WORK=/home/user/workspace/gopi-go-tmp/go-build533193578
go test -work (test binaries)         → WORK=/home/user/workspace/gopi-go-tmp/go-build1870900445
```

Full-tree build with a **cold** cache (`go clean -cache` first, 43.7s):

```
before: /tmp/go-build dirs=35 bytes=1040754099
after : /tmp/go-build dirs=35 (Δ0) bytes=1040754099 (Δ0)
```

`Δ0` new directories and `Δ0` bytes: the compiler's work dirs now land under
`/home/user/workspace/gopi-go-tmp` (this session left 2 live `go-build*` there while the
suite ran, plus the abandoned ones the probes produced — the mechanism that
created the 35 `/tmp/go-build*` dirs in the first place, since a KILLED go
command leaves its work dir behind and nothing else but this redirect would ever
collect it).

**Honesty note on this host's layout**: `/tmp`, `/home/user/workspace` and `/`
all resolve to `/dev/sda3` (device 2051) under `findmnt`; the separate-device
mapping (`/dev/shm` = 28) is the one §4/§5 measured. So on *this* host the
`GOTMPDIR` redirect is **hygiene and attribution, not space**: it stops the go
tool depositing unbounded `go-build*` piles in the shared `/tmp` that nothing
sweeps, and it concentrates them in a directory this system owns. On a host
where `/` and `/tmp` differ, it is a space fix as well. (The `df -B1` sample
taken during the cold-cache build measured sda3 avail −4096 bytes — noise, not
the build: `/tmp/go-build*` was byte-identical before and after.)

### 6.6 Verification (this change)

```
go build ./...                      : OK
go vet ./cmd/agent/ ./internal/scratch/ : clean
gofmt -l                            : clean
go test ./internal/scratch/         : ok
go test ./cmd/agent/ -run 'TestScratchGC|TestCLIContract|TestCLIUserVerbTokens|TestCompletion'
                                    : ok  (5 new E2E cases + the CLI contract suite)
```

New tests pin every claim above: stale-orphan removed / fresh + foreign kept
(`TestScratchGCCLI_RemovesStaleOrphanE2E`), the hard rule
(`TestScratchGCCLI_NeverRemovesHeldRoot`), `--dry-run` deletes nothing
(`TestScratchGCCLI_DryRunDeletesNothing`), the off switch
(`TestScratchGCCLI_OffSwitchHonoured`) and zero-side-effect `-h`
(`TestScratchGCCLI_UsageIsSideEffectFree`).
### 6.7 Independent re-verification of (a) and (b) — 2026-09-11, after the writes

§6.3/§6.4 are the numbers captured while the feature was being built. The whole
acceptance was then re-run from a **cold start** against the artifacts as
committed, on the developer's real (symlinked) scratch home, and (a) was
additionally re-proved on the **persistent** `*/30` entry rather than the
temporary per-minute one:

```
$ crontab -l | grep gopi-scratch-gc-watchdog
*/30 * * * * /home/user/.local/bin/gopi-scratch-gc-watchdog.sh >/dev/null 2>&1 # gopi-scratch-gc-watchdog (30min, installed by …)
$ pgrep -ax cron
1524 /usr/sbin/cron -f -P                     # the clock's driver is running
```

(a)+(b), no test process started anywhere in this window, timed to the installed
`*/30` boundary:

```
10:56:xx  planted  /dev/shm/gopi-test-scratch/suite-watchdog-verify-1100   (mtime backdated 7h)
11:00:01  cron fired the installed entry
          log line: scratch-gc role=cli home=/dev/shm/gopi-test-scratch removed=1 stale=6h0m0s disabled=false
          ⇒ the planted 7h orphan is GONE
```

The stale root survived only until the *scheduler's own* tick: nothing in the
window ran `gopi scratch-gc` by hand and nothing ran `go test`.

The vetoes were observed alive in the same pass — the developer's real scratch
home holds hundreds of fresh per-run roots plus live `queue-*` entries, and none
of them were collected (the tick's `removed` count is 1, exactly the planted
root). An unrelated `go test` **did** run in another agent session during the
window: it did not matter, because the sweep is driven by the clock, not by any
process's activity — which is the whole point of the change.

### 6.8 Acceptance (c), re-measured — and the two honesty corrections

The A/B lever is one command, with and without the machine-wide env file:

```
$ GOENV=off go test -work -run TestNothingZZZ ./internal/scratch/
WORK=/tmp/go-build3807726172                     # control: GOENV=off ⇒ /tmp, as before
$ go test -work -run TestNothingZZZ ./internal/scratch/
WORK=/home/user/workspace/gopi-go-tmp/go-build4063928829   # env file ⇒ under ~/workspace
```

`go env GOTMPDIR GOCACHE` → `/home/user/workspace/gopi-go-tmp` and
`/home/user/.cache/go-build` (`GOCACHE` was already off `/tmp` — it is the one
Go cache that never needed the change).

Corrections to §6.5, in the interest of not overselling a number:

1. **`/tmp/go-build*` is NOT frozen at 35 dirs / 1,040,753,099 bytes.** After the
   change it still held 35 → 37 directories and 1,045,993,147 bytes, because the
   `GOENV=off` **control runs** themselves wrote into `/tmp` — that is what a
   control is for. The honest formulation is the A/B above: *a go command with
   the env file does not touch `/tmp`*. The delta proof is only valid when no
   `GOENV=off` control (or an unrelated `go clean -cache`) runs in the measured
   window.
2. **`go clean -cache` is not a neutral step in that window.** It rewrites
   `~/.cache/go-build`, and the following cold rebuild moved ~1.5 GB through
   `/` — the same device as `/tmp` here, so a whole-filesystem `df` before/after
   flaps for reasons that have nothing to do with the scratch tree. Measure
   `/tmp` (or `/tmp/go-build*`), not `/` — and read the A/B, not the flapping
   total.
3. **The redirect is hygiene on THIS host, not a space win.** `/`, `/tmp` and
   `~/workspace` are all `/dev/sda3` here, so moving a work dir between them
   frees nothing. The value is (i) attribution — the tree's growth is a
   workspace concern, not a `/tmp` one, (ii) the 2026-09-11 exhaustion mechanism
   cannot recur on a host where `/tmp` IS a small tmpfs — which is the general
   case, not this host's.

### 6.9 Independent re-verification of (b)'s *shape* — foreign entries are never touched

§6.4 proves a held root survives. This pass additionally proves an entry that is
**not in this system's namespace** survives a real (non-dry) sweep, which is the
stronger claim — the sweep is namespace-limited, not age-limited:

```
11:0x:xx  planted  /dev/shm/gopi-test-scratch/orphan-verify-3603305   (7h old, name matches NO marker)
…  a manual watchdog run (removed=2 in that pass: the two suite-*/queue-* probes) …
$ ls -d /dev/shm/gopi-test-scratch/orphan-verify-3603305
drwxrwxr-x 2 nick nick 40 Sep 11 03:46                     # SURVIVED
```

`orphan-verify-3603305` did **not** match `SuiteRunMarker` / `LegacyScratchMarker`
/ `QueueMarker`, so the sweep never even considered it — the same protection that
leaves the legacy `tools-ext4probe/` residue in §3 alone. (The probe was removed
by hand afterwards.)

The `--dry-run` face was exercised in the same pass and its shared predicate held
up: it named exactly the two collectable entries (`queue-verify-*`,
`suite-verify-stale-*`, both `age=7h0m0s`), deleted nothing, and the subsequent
real sweep removed exactly those two and kept the fresh neighbour:

```
$ gopi scratch-gc --dry-run
scratch-gc role=cli home=/dev/shm/gopi-test-scratch removed=2 stale=6h0m0s disabled=false dry-run=true
would-remove /dev/shm/gopi-test-scratch/queue-verify-3603361 age=7h0m0s
would-remove /dev/shm/gopi-test-scratch/suite-verify-stale-3603361 age=7h0m0s
$ ls -d …/suite-verify-stale-*      → still there (the plan deletes nothing)
```

### 6.10 Second independent re-run (2026-09-11 10:47–10:55) — acceptance with clean attribution

A second reviewer re-ran the acceptance end to end without trusting §6.3–§6.9's
numbers. Two measurement defects in the earlier evidence were found and are
corrected here; the acceptance itself holds.

**(a) the installed entry, driven by the scheduler, no test running.**

```
$ crontab -l | grep gopi-scratch-gc-watchdog
*/30 * * * * /home/user/.local/bin/gopi-scratch-gc-watchdog.sh >/dev/null 2>&1 # gopi-scratch-gc-watchdog (30min, installed by …)
$ pgrep -ax cron
1524 /usr/sbin/cron -f -P
$ journalctl --since 10:51 --no-pager | grep CMD
10:52:01 peerbox CRON[3613078]: (nick) CMD (GOPI_TEST_SCRATCH_HOME=/dev/shm/gopi-scratch-watchdog-proof \
          /home/user/.local/bin/gopi-scratch-gc-watchdog.sh >/dev/null 2>&1 # gopi-scratch-gc-watchdog-TEMP-PROOF)
$ grep gopi-scratch-watchdog-proof /home/user/.gopi/logs/scratch-gc-watchdog.log
2026-09-11T10:52:01+08:00 scratch-gc-watchdog scratch-gc role=cli \
  home=/dev/shm/gopi-scratch-watchdog-proof removed=1 stale=6h0m0s disabled=false
```

**(b) a 7h orphan gone, with nothing but the clock involved — and with clean
attribution this time.** The earlier passes planted into the developer's REAL
scratch home, which any suite (including ones other sessions were running
concurrently) also sweeps — an alternative explanation. This pass planted into a
scratch home **no test ever uses** (`GOPI_TEST_SCRATCH_HOME=/dev/shm/gopi-scratch-watchdog-proof`,
a *temporary* crontab entry carrying the env), so the ONLY possible collector was
the scheduled watchdog:

```
10:51:41  planted /dev/shm/gopi-scratch-watchdog-proof/suite-proof-run-1100  (mtime backdated 7h)
10:51:41  live `go test ./...` processes on the host: 1 … 10:51:41 they exit; from 10:51:41 on: 0
10:52:01  cron fires the entry above
10:52:01  log: home=/dev/shm/gopi-scratch-watchdog-proof removed=1   ← the 7h orphan is gone
$ ls -d /dev/shm/gopi-scratch-watchdog-proof
ls: cannot access …: No such file or directory
```

The temporary entry and the proof home were removed afterwards; the installed
`*/30` entry is the only survivor (`crontab -l | grep scratch` → one line).
**Caveat stated plainly**: the proof window overlapped a full `go test ./...`
that another session had started, so "no test process alive at all" is not
claimed for the first 40 s of the wait — what is claimed, and is stronger, is
that the *tree swept does not belong to the suite layer*, so no test could have
swept it. The sweep ran when the scheduler said so, not when a test ran.

**(c) `/tmp` untouched by a build+test — with the harness's own pollution removed.**
The earlier A/B counted `/tmp/go-build*` with a glob that also matched the
reviewer's own `/tmp/go-build.before` / `.after` snapshot files (946 bytes each),
which is exactly the apparent `+946` in §6.5's re-run. Measured on **directories
only**:

```
$ find /tmp -maxdepth 1 -type d -name 'go-build*' | wc -l ; du -sb <those dirs>
BEFORE: dirs=39 bytes=1046001339
  go build ./...                 : OK
  go test -count=1 ./internal/scratch/ : OK
AFTER : dirs=39 bytes=1046001339
NEW /tmp/go-build* dirs created by build+test: (none)
$ GOENV=off go build -work ./internal/scratch/  → WORK=/tmp/go-build227522949          (control)
$ go build -work ./internal/scratch/            → WORK=/home/user/workspace/gopi-go-tmp/go-build408543360
```

So acceptance (c) is confirmed **bit-for-bit**: with the machine-wide go env file
in force, a full `go build ./...` plus a test run create no `/tmp/go-build*` entry
at all, while the `GOENV=off` control still writes to `/tmp` (the two are the
same command; only the env differs). §6.8's corrections stand and are the
reason this was re-measured rather than restated.

**Contract wiring re-checked on master's tip, not just the branch**: this
worktree's `cmd/agent/cli_scratch_gc.go` and the deployed `/home/user/work/GoPi/bin/gopi`
(built 10:37 today) carry the verb — `bin/gopi scratch-gc -h` prints the usage
head the watchdog probes for, and `bin/gopi scratch-gc` prints the logfmt line.
Until this branch merges and Rule 12's post-merge rebuild runs, `bin/gopi` is a
pre-merge build that ALREADY carries the verb (otherwise the watchdog would log
its `FAIL … does not carry the scratch-gc verb` line and refuse to sweep).
