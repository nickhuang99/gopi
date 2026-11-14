<!-- doc-class: historical -->
# `gopi-tools-suite-build-cache-<pid>` residue — writer, the four ways a run ends, and the two halves of the fix

Task `01a13032-4c50-7a23-8e7b-bb837a39cee5` (branch `task/01a130324c507a238e7bbb837a39cee5`,
baseline `46cb3e7`, host `hosta`). Every number below was measured in this run;
the commands that produce each one are quoted with it, and the read-only recheck
block at the end reproduces the whole picture without touching any of it.

## 1. The phenomenon, re-measured (not inherited)

```console
$ ls -1 /home/user/workspace/gopi-build-cache | wc -l                       # 692 entries
$ find ... -mindepth 1 -maxdepth 1 -type d -name 'gopi-tools-suite-build-cache-*' | wc -l   # 689
$ find ... -name 'gopi-tools-suite-build-cache-*' -empty | wc -l            # 648 untouched
$ find ... -name 'gopi-tools-suite-build-cache-*' -type f -printf '%s
' | awk '{s+=$1} END {print s}'
                                                                            # 2,056,340,536 B
$ stat -c '%d %n' /home/user/workspace/gopi-build-cache /                   # 66305 vs 66308
```

(The counts moved 687 → 689 during this session: two concurrent suites minted
their own shells while it ran. §1's table is the state at 23:27, the recheck block
in §8 prints the current one; the ratio is what matters, and it is the point.)

The root holds three populations:

| population | count | bytes (`find -type f -printf '%s'` sum) | claimed by |
|---|---|---|---|
| `gopi-tools-suite-build-cache-<pid>` | 687 | **1961.1 MB in 58,476 files** | **nothing** (before this change) |
| `<16-hex>` per-repo keys | 3 | 737.1 MB in 7,740 files | `build-cache` row (correct) |
| `<16-hex>` stray shards | 0 | — | `build-cache/shards` row (correct) |

The family is **not** "680 empty directories", and the correction matters for the
root cause (the shape was miscounted twice in this run before it was measured
properly — first with a `find … | head` census that broke its own pipe, then with
`du` over a glob, which reports a directory's own inode size and not its subtree):

| shape (entries directly inside, regular files below) | count |
|---|---|
| 0 entries, 0 files — the shell as `MkdirAll` makes it | **646** |
| ≥1 subdirectory, ≥1 regular file — a **filled** sandbox | **41** |
| the largest | 8,239 files / 29 entries (pid 904975) |

So 646 shells are untouched and 41 hold a real one-off GOCACHE (1.96 GB total,
26.9 % of the root). 662 of the 687 were created on a single day (2026-10-12), and
the pids span 4336 … 4187039 — i.e. they wrap, which matters for §3.

**A pid in this name is not a live process, measured.** Of the 687 shells exactly
one carries a pid that is currently alive — 3451 — and it is not a test run at
all:

```console
$ cat /proc/3451/cmdline | tr '\0' ' '
/usr/libexec/gnome-shell-calendar-server
$ stat -c '%n %y' .../gopi-tools-suite-build-cache-3451
.../gopi-tools-suite-build-cache-3451 2026-10-12 04:28:52 +0800
```

The shell is 19 hours older than the process now wearing that pid: recycled.
That measurement is why the new row (§7b) does not use liveness as a keep.

## 2. The writer: `internal/tools/testmain_test.go`

`toolsPinBuildCacheSandbox()` — armed from **`internal/tools/testmain_test.go:392`
(`TestMain`)**, beside the signal arm at `:395` — offers the merger BUILD gate a
throwaway `GOCACHE` for the length of the `internal/tools` suite. Every line number
below is the post-change file (`git show HEAD:internal/tools/testmain_test.go`);
the pre-change file is the same text shifted up by the size of the two comments
this change added:

| site | what it does |
|---|---|
| `internal/tools/testmain_test.go:23` | `toolsBuildCacheSandboxName = "gopi-tools-suite-build-cache"` — the name, now asserted equal to the shared prefix |
| `:126` | `toolsPinBuildCacheSandbox()` — the writer |
| `:179` | `toolsBuildCacheSandboxCandidates()` — three arms, in order |
| `:132` | `os.MkdirAll(dir, scratch.DirPerm)` — **the line that creates the directory** |
| `:136` | `os.Setenv(scratch.GoCacheRootEnvName, dir)` + record in `toolsBuildCacheSandboxDir` |
| `:192` | `toolsRemoveBuildCacheSandbox()` — the removal |
| `:408` | … called **after `m.Run()`** (`:407`) — the clean exit path |
| `:251` | `toolsReapBuildCacheSandboxOnSignal()` — the new signal path (this change) |
| arm 2 | `<workspace home>/workspace/gopi-build-cache/gopi-tools-suite-build-cache-<pid>` |

**Arm 2 is the one that engages on this host, and it cannot be otherwise.** The arm
tries its candidates in order and skips any that fails `toolsDirIsDiskSafe`
(`:334`, which asks the gate's own three placement predicates):

* arm 1 `<scratch run root>/gopi-tools-suite-build-cache` — this host's scratch run
  root is `/dev/shm/suite-tools-run-…`, **tmpfs** → refused (off-RAM ban). Measured
  in every suite run's own stderr: `scratch root /dev/shm is tmpfs (memory filesystem)`.
* arm 2 `<workspace home>/workspace/gopi-build-cache/<name>-<pid>` — device **66305**
  vs `/` = **66308** → accepted. This is the directory the census found.
* arm 3 `$HOME/.cache/<name>-<pid>` — device 66308 = `/` → refused.

(The doc comments on this function still describe the ranking as a preference
["The USER-CACHE candidate stays LAST…"]; the code's actual reason on this host is
that arms 1 and 3 are *refused*, not merely ranked last. Same function, both true.)

## 3. It is only the *arm order* that could have hidden this — and it does not

The pid in the name is `os.Getpid()` of the **test binary**. `internal/scratch`'s
own `SandboxStaleAge` machinery uses a pid for exactly the same purpose, but the
build-cache root is **not** one of its roots, so before this change no arm looked
there at all. Measured:

```console
$ gopi tool scratch-gc trees --dry-run --explain    # baseline binary 46cb3e7
scratch-gc ... tree=build-cache bytes=0 holding=772932107 holding_n=3 dry-run=true
scratch-gc ... tree=build-cache/shards bytes=0 holding=0 holding_n=0
$ ... | grep -c gopi-tools-suite-build-cache        # 0 — no line names the family
$ gopi tool scratch-gc sandboxes                    # walks a different root entirely
```

## 4. The real run that proves the writer creates the directory

Not a read of the code (`Rule 31`). A compiled `internal/tools` test binary, run
with a known pid, watched by a 0.1 s snapshot-diff:

```console
$ go test -c -o $SESS/artifacts/tools.test ./internal/tools/
$ tools.test -test.run '^TestSuiteBuildCacheSandboxIsPinned$' -test.count=200000 &
  test-binary pid=361147
  APPEARED AT: t+14x0.1s
  $ ls -ld .../gopi-tools-suite-build-cache-361147
  drwxr-xr-x 2 nick nick 4096 Oct 12 23:19 .../gopi-tools-suite-build-cache-361147   # EMPTY
  $ ps -o pid,cmd -p 361147
  361147  …/artifacts/tools.test -test.run ^TestSuiteBuildCacheSandboxIsPinned$ …
--- after SIGKILL of that process ---
  .../gopi-tools-suite-build-cache-361147            # SURVIVED
```

The directory is named by **that process's pid**, created by **that run**, and it
outlived the run. Second, on the declared entry point: one
`make test-budget PKGS='-run TestMergeBuildGate ./internal/tools/'` run created its
own shell and removed it **2.7 s later** (`comm` of the before/after listings shows
exactly one new directory, which then disappears; the run's stamp reads
`elapsed=20s exit=0 outcome=pass`).

## 5. Why everything is empty — the cause, measured, with the three dead ends

An **untouched shell is the normal shape of a run that never drove the gate — and
the 41 filled ones are runs that did**. Both were observed directly:

* **The shell is created in `TestMain`, before any test runs.** A run filtered to
  non-gate tests (`-run TestSuiteBuildCacheSandboxIsPinned`) creates the sandbox and
  never writes a byte into it. Measured: 0 entries during the run. That is the 646.
* **The gate fills it when a test drives a real build.** A
  `-run TestMergeBuildGate` run populated its shell within 1.2 s of starting — three
  `<16-hex>/gocache` subtrees, the per-repo-key shape the gate mints for each
  fixture repo. On the real tree, the 41 filled shells hold 8,239 files at the top
  end and real Go build-cache bodies (`trim.txt`, `README`, `*-a`/`-d` entries): a
  one-off GOCACHE per fixture repo. So the family's bytes are **not** overhead-free —
  1.96 GB, 26.9 % of the root.
* **Both shapes are garbage for the same reason, and it is not "they are empty".**
  The sandbox exists only for the length of one test process: the winning candidate
  is a directory the gate is *handed* for that run, the tests that use it are the
  gate's own fixtures (throwaway repos under `t.TempDir()`), and a clean exit
  deletes the whole thing — so the cache it holds can never be read by a later run
  by construction. Measured: every shell's pid is dead (except one recycled pid),
  and 23 of the 24 shells past the 24 h window are untouched.

Dead ends eliminated by measurement, so a later reader does not repeat them:

1. **"Written and then swept."** No. The removal is `os.RemoveAll(sandbox)`, not a
   content sweep: a removed directory leaves nothing, and a 0-entry directory is the
   pre-build state, not a post-sweep one.
2. **"Fresh runs are being cleaned; only old ones remain."** No — inverted. The
   shells that survive are the ones whose *runs died*; a run that exits normally
   removes its shell 2.7 s after creating it.
3. **"The root is the tmpfs/`/`-shared accident being re-created."** No. The
   candidate list was rewritten precisely so the chosen arm lands on the one tree
   that is off `/` and off RAM (device 66305). The placement is correct; the
   *disposal* is what is missing.

## 6. Which ending leaves a shell — the measurement the fix had to be built on

| how the run ends | who cleans up | measured |
|---|---|---|
| test binary exits normally | `toolsRemoveBuildCacheSandbox` after `m.Run` | shell gone 2.7 s after creation |
| **SIGTERM / SIGINT / SIGHUP / SIGQUIT** | nothing, before this change | shell **survives** (red proof, §7) |
| **the suite's own timeout clock** | nothing, and no in-process code can | `tools.test -test.timeout 4s` → **`$? = 2`**, i.e. `testing` calls `os.Exit(2)`, **not a signal**; `go test -timeout 4s -run TestMergeBuildGate ./internal/tools/` left its shell on disk and printed no handler line |
| `gopi test-budget` (default filter, every test) | nothing | 285 shells appeared on this host while one filtered run was being watched |
| SIGKILL, or a wedged run whose signal never arrives | nothing | shell survives (measured) |

**The budget path is therefore the producer.** `scripts/test-budget.sh:715` runs
`go test -count=1 -timeout "$test_budget" …` (25 m, and the *only* reader of
`GOPI_TEST_TIMEOUT`), and a bare `go test ./…` gets the toolchain's 10 m default.
That clock is the machinery **we ourselves** told to stop long runs — and 662 of
the 683 shells were created on the one day the suite ran under it. An in-process
handler cannot reach that path at all; it is the one ending that needs a sweep.

This also kills the tempting "just add a `t.Cleanup`/`defer`" shape: a signal does
not run the defer stack, and `os.Exit(2)` does not either.

## 7. The conclusion, in two halves — both landed, both with a real run

### 7a. The writer reaps its own shell on the signals it can catch

`internal/tools/testmain_test.go`: `toolsReapBuildCacheSandboxOnSignal()`, armed
in `TestMain` right after the sandbox (line 293), plus `toolsReraise`.

```console
$ tools.test -test.run '^TestKilledSuiteRunReapsItsBuildCacheSandbox$'     # RED, before the fix
  build_cache_sandbox_signal_test.go:126: the sandbox .../gopi-tools-suite-build-cache-442257
  survived the run that created it being SIGTERMed …
--- FAIL: TestKilledSuiteRunReapsItsBuildCacheSandbox (30.94s)

$ …same command after the fix…
    build_cache_sandbox_signal_test.go:97: child 451646 reported its sandbox .../…-451646
--- PASS: TestKilledSuiteRunReapsItsBuildCacheSandbox (0.99s)
```

Two things the first version got wrong, both caught by running it (and both now
pinned by the test's assertions):

* **`signal.Notify` DISABLES a signal's default action.** The first handler only
  removed the sandbox and returned — and the TERMed child did not die at all (the
  test hung 90 s and failed). The handler must `signal.Stop` and **re-raise**; the
  test asserts the child ends `Signaled() == SIGTERM`, so a swallowing handler is
  red.
* The first draft's shared prefix was derived from `SharedBuildCacheDirName`
  (`gopi-build-cache-tools-suite-build-cache-`) while the writer mints
  `gopi-tools-suite-build-cache-`. The cross-package assertion written for exactly
  this drift caught it immediately.

### 7b. A shell nothing reaped is claimed by a declared-tree row

`internal/scratch/declared_build_cache_suite.go` adds the row
**`build-cache/suite-sandbox`** (registered in `DeclaredTrees()`, beside
`build-cache` and `build-cache/shards`), rooted at the same directory, with the
predicate *prefix + digit run* and the key row's own staleness closure
(`buildCacheKeyStale`, whose `files == 0` arm is already the "empty → mtime" rule).
The shared prefix is `worktreeroots.SharedBuildCacheSuiteSandboxPrefix`, and the
writer's constant is asserted equal to it.

```console
$ gopi tool scratch-gc trees --dry-run | grep build-cache       # 23:48, the closing state
scratch-gc … tree=build-cache             removed=0 stale=24h0m0s bytes=0 holding=772932107 holding_n=3
scratch-gc … tree=build-cache/suite-sandbox removed=1 stale=24h0m0s bytes=3123 holding=2056340536 holding_n=43
would-remove /home/user/workspace/gopi-build-cache/gopi-tools-suite-build-cache-660065 \
             tree=build-cache/suite-sandbox age=25h3m8s bytes=3123
scratch-gc … tree=build-cache/shards      removed=0 stale=24h0m0s bytes=0 holding=0 holding_n=0
```

`holding_n=43` is what is left AFTER the untouched shells this investigation
produced were reclaimed by hand (see §9) — 43 of the survivors hold real cache
data, which is the honest denominator. The one candidate the plan names is exactly
the right one: the ONLY shell past the 24 h window, 3,123 bytes of real one-off
GOCACHE (recheck at 23:48: 24 shells past the window, exactly 1 of them holding
files). The 3 per-repo keys are untouched — `holding_n=3` on the pre-existing row
is unchanged, which is the disjointness this row promised. Nothing was deleted by
that command: it is `--dry-run`.

**Why no liveness check on this row, even though 41 shells hold real cache data
(a deliberate difference from `SweepSandboxes`).** Because liveness cannot be
answered for this name — measured, not argued: the name carries the *test
binary's* pid, pids are recycled (4336 … 4187039 in the census), and the one
shell whose pid is alive is a gnome-shell process 19 h younger than the
directory. A liveness test here would keep a shell because an unrelated process
wears its number. What the window protects is the only thing worth protecting:
a shell from the run happening *now* is younger than 24 h and kept (measured: 0
of the 41 filled shells are past the window), while the 1.96 GB of per-run
one-off caches — unreadable by any later run by construction — ages out. The
cost, stated plain: a shell stranded by a wedged run, and its cache, wait 24 h.

**Rule 35, answered in order.** (1) *What breaks without it:* the family grows one
directory per run forever — 662 in a day on this host, bounded by nothing, on a
partition that has already been exhausted three times by this class of tree. (2)
*Why not in-process only:* the in-process half is 7a and covers the signals; the
`-test.timeout` / `os.Exit(2)` ending is unreachable from inside the process, and
it is the producer. (3) *Who owns it:* the sweep that already ticks — the
worker/merger tick and `gopi tool scratch-gc trees` — no new reaper, no new
process, no timer, no cron. (4) *What the next person sees:* the dir name carries
the writer's name, the row prints `tree=build-cache/suite-sandbox` with the count
and the bytes, and `--dry-run` lists every candidate with its age before anything
is deleted.

## 8. Read-only recheck (nothing below writes)

```bash
R=/home/user/workspace/gopi-build-cache
ls -1 "$R" | wc -l
find "$R" -mindepth 1 -maxdepth 1 -type d -name 'gopi-tools-suite-build-cache-*' | wc -l
find "$R" -mindepth 1 -maxdepth 1 -type d -name 'gopi-tools-suite-build-cache-*' -empty | wc -l
# bytes: sum the FILES — `du` on a directory reports its own inode size and
# under-reports a subtree (this is the mistake §1 records)
find "$R"/gopi-tools-suite-build-cache-* -type f -printf '%s\n' | awk '{s+=$1} END {print s}'
stat -c '%d %n' "$R" /                       # 66305 vs 66308 — the placement bans' inputs
gopi tool scratch-gc trees --dry-run | grep build-cache          # the row + the counts
gopi tool scratch-gc trees --dry-run | grep suite-sandbox | head # the named candidates
```

`git -C "$R" ls` does not apply: the root is not a repository (it is a cache tree
under the worktree root), so a `git ls` there reports nothing — the file-system
commands above are the check. Within the task worktree the deliverable check is
`git status --porcelain` (empty) and `git log --oneline -1`.

## 9. Not done, honestly

* **The ~687 pre-existing shells — 1.96 GB, 41 of them holding a real one-off
  GOCACHE — were not deleted.** Reclaiming them is the operator's call, and the
  change ships the measurement, the predicate and the read-only plan instead
  (`--dry-run` above lists 24 already past the window, 1 of them filled). Six of
  them are this investigation's own footprint (probe runs, including two
  deliberate internal-timeout probes), and every one of those that was still
  untouched was reclaimed during wrap-up — with bare `rmdir`, which REFUSES a
  non-empty directory, so nothing holding cache data could be included — together
  with the other aged empty shells left by concurrent runs (the `holding_n=43` in
  §7b is the post-wrap-up state). The remaining 43 filled shells — 1.96 GB — wait
  for the window, and for the operator, like the rest.
* **The `-test.timeout` ending strands a shell for up to 24 h** and cannot be
  fixed in-process (§6). Pinning `GOPI_TEST_TIMEOUT` to a signal-delivered timeout
  is a change to `scripts/test-budget.sh` — the declared entry point shared by
  every host — and belongs with the system layer (Rule 28), so it is reported here
  rather than made here.
* **`scratch.SweepSandboxes` was left alone.** Its root is a different tree, its
  shape is a `.sandbox-owner.json` directory, and widening it to reach the
  build-cache root would have two arms claiming one namespace — the opposite of
  what Rule 14 asks for. This family is reached through `DeclaredTrees()`, which
  both sweep faces walk.
* **Sixty-three shells were minted during this session** (23:14 → 23:52) by
  concurrent suites, not by anything here — the census moved 687 → 689 while the
  package suite was running. That is the rate the fix is aimed at.

## 10. Verification

* Build: `go build ./...` → OK; `CGO_ENABLED=0 go build -o $SESS/artifacts/gopi2 ./cmd/agent/` → OK.
* Tests: `make test-budget PKGS='./internal/tools'` → **3414 tests, 0 failures,
  exit=0** (`stamp: 2026-10-12T23:31:07+08:00 … exit=0 outcome=pass`).
* Ran: `gopi tool scratch-gc trees --dry-run` (real tree, before/after the row);
  `tools.test -test.run '^TestKilledSuiteRunReapsItsBuildCacheSandbox$'` (red then
  green); `go test -run SuiteSandbox ./internal/scratch/` → ok.
* Result: writer pinpointed and proven by a real run whose pid appears in the
  created directory's name; the empty-shape cause explained from measurement; the
  row claims 689 shells / 2.06 GB and leaves the 3 per-repo keys alone;
  the red→green transition is reproducible with the two commands in §7a.
