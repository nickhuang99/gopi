<!-- doc-class: historical -->
# Task Report: the internal/tools suite hang that SURVIVED the scratch-root fix (t-1788361069 round 3)

**Branch:** `fix/tools-suite-hang-residual` (worktree `~/workspace/gopi-suitehang`, base `9cc034bc`)
**Date:** 2026-09-13 · **Host:** 4 cores, load average 12–46 (3–4 concurrent `tools.test` suites), `/tmp` on `/dev/sda3` (95% full)

## Summary

The 2026-09-10 fix (`pinSuiteTempRoot` → tmpfs scratch root) fixed the **write** side of the
suite's isolation but not the **read** side. A test that pins only its DB path
(`t.Setenv("GOPI_MEMORY_DB", …)`) still leaves `config.GopiDir()` resolving the **developer's
real profile**, and `task show`'s session resolution walks *every* entry of
`~/.gopi/sessions` looking for one id. That cost is **linear in the ambient tree size**
(54,847 entries on this host) and was ~240s of the 438.9s suite — enough, at 600s wall-clock
per package, to panic the suite on a loaded box. Fix: pin an **empty per-run gopi profile**
in `TestMain`, the same "empty sandbox" recipe the notify pin already uses.

## Completed

- **Reproduced and named the hang on current master** — the suite did **not** deadlock on this
  run: it passed in **438.894s** (999 PASS / 0 FAIL, load 15–25), with only 161s of headroom
  under the 600s package timeout. The failure mode is therefore *wall-clock starvation*, not a
  lock — and the four tests carrying it are named below.
- **Raw SIGQUIT dump captured and saved** (`devdoc/evidence/tools-suite-hang-residual-dump.txt`):
  the test goroutine is **`[runnable]`** in `os.ReadDir`, i.e. burning CPU in a directory scan —
  not blocked in fsync, not waiting on a mutex. `database/sql.(*DB).connectionOpener` sits idle
  in `select` beside it (victim, never the leak).
- **Blocking frame named**: `taskFindSessionJSONL` ← `taskSessionLogPath` ← `taskShowWithDB` ←
  `TaskShow` ← `TestTaskStampRun` (`internal/tools/tasks.go:1512 / :1604 / :1882 / :1830`).
- **CPU profile attribution**: `runtime.gcDrain` 26.97s cum (**45%** of samples) and
  `runtime.scanobject` 34.07s flat — a GC-dominated profile, because the per-directory strings
  and `json.Unmarshal` scratch of 54,178 directory scans are transient heap churn at ~180% CPU.
- **Scaling law measured** (same binary, one test):
  | ambient `~/.gopi/sessions` entries | `TestTaskStampRun` wall |
  |---|---|
  | 0 | 18.75s |
  | 4,000 | 84.34s |
  | 16,000 | 272.93s |
  | 54,847 (real host) | 77.41s at load ~20 (≈900s at load 1, per the law) |
- **Fix, test-side (no timeout raised)**:
  - `internal/testutil/suiteprofile.go` — `PinSuiteGopiProfile` sets `GOPI_HOME` to an empty
    per-run `<scratch root>/suite-gopi-home` (so `GopiDir()` = that + `/.gopi`), plus
    `AssertSuiteGopiProfileIsPinned` as the runtime guard. Named constants
    (`SuiteGopiProfileDirName`, `suiteGopiProfileMaxEntries`); no bare literals (Rule 1/13/14).
  - `internal/tools/testmain_test.go` — one line in `TestMain` + guard test
    `TestSuiteGopiProfileIsSandboxed`.
  - **8 fixtures that named a fake root via `HOME` only** now name `GOPI_HOME` too —
    the pin *wins* over `HOME`, and these fixtures' intent was always "relocate the gopi data
    root" (`tasks_test.go` ×6 fixtures, `memory_test.go`, `task_deliverable_branch_test.go` ×2).
  - **Production inconsistency fixed** (exposed by the pin, not caused by it):
    `taskBriefingFromSession` resolved `os.UserHomeDir()/.gopi/sessions` while its three
    siblings (`taskSessionLogPath`, `taskModelFromSession`, `taskRawLogPathFromSession`) resolve
    `config.GopiDir()`. A `GOPI_HOME`-sandboxed run (the documented relocation knob) showed a
    briefing-less `task show`. Now both axes agree (Rule 14). This also removes the pin's one
    real escape hatch (`GOPI_HOME` could not redirect that lookup).
- **Regression guard, mutation-proven**: removing the `PinSuiteGopiProfile` line from `TestMain`
  makes the guard RED —
  `GOPI_HOME is unset — the suite fell back to the developer's real gopi profile (/home/user/.gopi), whose audit lookups walk its whole sessions tree.`

## Not Done / deliberately out of scope

- **`taskFindSessionJSONL` still scans an unbounded ambient tree.** Attack A (bound the scan
  itself — e.g. look for the exact `sessions/<id>/session.jsonl` and `sessions/<id>.jsonl` paths
  first, index the rest) was *not* implemented: it is a real but much larger production change
  (it would turn "resolved / not resolved" into "may not have found it", since the id match is a
  *prefix* match over file contents), it is not needed for acceptance, and it is a **separate,
  independently verifiable** workstream. The suite is now immune to the tree size; production
  `gopi task show` on a 54k-entry profile still pays the scan. **Recommend a follow-up task.**
- **Pre-existing red suites, untouched (identical on master, not caused by this branch)**:
  `internal/standards` fails `Rule 20` (annotated escape at `internal/shot/x11.go:513`), and
  `internal/testutil` fails `TestWorkerOwnershipPointerPresent`. Verified identical on master
  `9cc034bc` before/after — **zero sites added by this branch**.
- The suite is still load-sensitive in the residual sense that 3–4 concurrent `tools.test`
  binaries on 4 cores triples the wall clock. That is host capacity, not a leak; no timeout was
  changed to accommodate it (acceptance #3).

## Root cause — 5-why

1. **Why does `go test ./internal/tools/ -count=1` panic at 600s?** Because the package needs
   ≥438.9s of wall clock on this host and host load multiplies it past the per-package timeout.
2. **Why 438.9s?** Because four tests alone spend 188s, and 14 tests spend ~300s — all of them
   `task show` / deliverable-backfill tests.
3. **Why do they spend it?** `task show` resolves the task's session ~4× per render
   (`taskSessionLogPath` → `taskFindSessionJSONL`, `taskSessionStartID`, `taskModelFromSession`,
   `taskRawLogPathFromSession`, `taskBriefingFromSession`), and `taskFindSessionJSONL` does
   `os.ReadDir(sessions)` **plus** `os.ReadDir(sub)` + first-line read + `json.Unmarshal` for
   **every one of the 54,178 subdirectories** in search of one short id.
4. **Why does the test scan the developer's 54k-entry tree at all?** Because the suite pins only
   `GOPI_MEMORY_DB` (a scratch DB path); `config.GopiDir()` keeps resolving `$HOME/.gopi`
   — the suite has **no profile isolation**. The tmpfs pin fixed scratch **writes**; nothing
   pinned scratch **reads**.
5. **Why did the previous fix miss it?** It was diagnosed from a goroutine dump showing an
   `fsync` at WAL close, so the fix targeted the filesystem (`TMPDIR`). The fsync was real (it
   was the *other* blocker), but the dump also contained the profile-scan goroutine — and this
   round shows the scan alone suffices: **the moving-target victim is the signature of a
   resource stall**, and here the "resource" is the developer's profile size.

**Real leak/deadlock vs loaded-box artifact: both were present, and they are distinguishable.**
This round's 438.9s pass at load ~20 with a *stable* slow-test set (identical names across runs)
is the load-sensitive wall-clock artifact; the profile scan is the deterministic multiplier that
sets the floor. There is **no** deadlock: the dump's test goroutine is `[runnable]` (consuming
CPU), and the profile-vs-time law is exactly linear — a lock or a leak would not scale linearly
with a directory's entry count.

## Verification

| Check | Command | Result |
|---|---|---|
| Build | `CGO_ENABLED=0 go build ./...` | exit 0 |
| Vet | `go vet ./internal/testutil/ ./internal/tools/` | exit 0 |
| **Master baseline** | `go test ./internal/tools/ -count=1 -timeout 600s` (master `9cc034bc`) | `ok … 438.894s` — 999 PASS / 0 FAIL, loads 15.6→25.3, 3–4 concurrent `tools.test` |
| **RED proof (unfixed)** | `env GOPI_HOME=<16k-entry profile> go test ./internal/tools/ -run '^TestTaskStampRun$' -count=1 -timeout 120s` | `panic: test timed out after 2m0s` / `running tests: TestTaskStampRun (2m0s)` |
| **RED proof (unfixed, real profile)** | `env GOPI_HOME=<54,847-entry profile> … -run '^TestTaskStampRun$' -timeout 120s` | same panic — `TestTaskStampRun` |
| **GREEN (fixed branch)** | `go test ./internal/tools/ -count=1 -timeout 600s` | **`ok … 201.319s`** (wall 238.89s), **1000 PASS / 0 FAIL**, loads 12.1→23.2 |
| The four giants, before → after | same tests | 73.61s→**0.14s**, 49.45s→**0.01s**, 38.07s→**0.04s**, 27.20s→**0.05s** |
| Guard mutation proof | drop `PinSuiteGopiProfile` from `TestMain` | `--- FAIL: TestSuiteGopiProfileIsSandboxed` (names `/home/user/.gopi`) |
| Guards green | `-run 'TestSuiteGopiProfileIsSandboxed\|TestSuiteNotifyEnvIsHermetic\|TestSuiteScratchRootIsPinned'` | all PASS |
| Pre-existing reds | `go test ./internal/standards/`, `./internal/testutil/` (master **and** branch) | identical failures on both — 0 added sites |

**No timeout value was raised** — the only timeout in play is the harness's own `-timeout 600s`
(and `-timeout 120s` for the red proof). The suite got faster by removing work, not by extending
a budget (acceptance #3).

## Evidence — raw dump excerpt

Full file: `devdoc/evidence/tools-suite-hang-residual-dump.txt`.

```
goroutine 7 gp=0xc000150540 m=nil [runnable]:
internal/poll.(*FD).ReadDirent(0x124f6c0?, {0xc000388000, 0x2000, 0x2000})
	/usr/local/go/src/internal/poll/fd_unixjs.go:68
os.(*File).readdir(...)  /usr/local/go/src/os/dir_unix.go:78
os.ReadDir({0xc000316d00?, 0x3e?})  /usr/local/go/src/os/dir.go:125
github.com/nick/gopi/internal/tools.taskFindSessionJSONL({0xc00001e640, 0x19}, {0xc000291e36, 0x6})
	/home/user/work/GoPi/internal/tools/tasks.go:1512
github.com/nick/gopi/internal/tools.taskSessionLogPath(...)
	/home/user/work/GoPi/internal/tools/tasks.go:1604
github.com/nick/gopi/internal/tools.taskShowWithDB(...)
	/home/user/work/GoPi/internal/tools/tasks.go:1882
github.com/nick/gopi/internal/tools.TaskShow({0xc60952?, 0x2d1?})
	/home/user/work/GoPi/internal/tools/tasks.go:1830
github.com/nick/gopi/internal/tools.TestTaskStampRun(0xc000150380)
```

and the red-proof form of the same complaint:

```
panic: test timed out after 2m0s
	running tests:
		TestTaskStampRun (2m0s)
```

## Deliverable location (Rule 6d)

- **Branch:** `fix/tools-suite-hang-residual` — a linked worktree at
  `~/workspace/gopi-suitehang` of the main repo, so the commit is already in the **main repo's**
  refs (no push needed).
- **Recovery command:** `git log --oneline fix/tools-suite-hang-residual` (in `~/work/GoPi`) or
  `git show fix/tools-suite-hang-residual:devdoc/tools-suite-hang-residual.md`.
- Merge is left to the reviewer; this branch is not self-merged.
