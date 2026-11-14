<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Task Report: why `tmpdir` is `/dev/shm/gopi-test-scratch/tmp` (not `~/workspace`) — and the default that removes the /tmp fallback

**Epic:** operator question, verbatim — *"I observe `[gopi] tmpdir: TMPDIR=/dev/shm/gopi-test-scratch/tmp (from settings key tmpdir; every child inherits it)` … I thought we agree that tmpdir should avoid /tmp … I thought we decide using ~/workspace as tmpdir? is this decison changed some moment? why? can we setup this tmpdir or TMPDIR by default to avoid /tmp?"*

**Branch:** `fix/tmpdir-default` — worktree `~/workspace/gopi-tmpdir-default`, created from master `51edd8d9` (Rule 6a).
**Date:** 2026-09-16. **Governing rules:** 22 (criterion, not value), 25 (no privileged remedy), 1/13/14 (constants), 15 (no silent failure), 6/8 (worktree, additive commits).

---

## 1. Answering the question first: no decision was reversed

**The original agreement WAS `~/workspace` — and it was refined on 2026-09-11 by measurement, not by preference.** The operator's shell-level layer is still in force today (measured in this session): `~/tmp` exists, is actively written to (newest entry 2026-09-15 23:18, 266M) — that is the *"从 shell 起的东西"* half of the epic. The gopi-internal half (this repo's settings key) landed **after** a measured finding that `~/workspace` is the wrong *home for temp data*, even though it is off `/`.

### The measured evidence (all re-confirmed in this session)

| path | resolved target | device (`stat -c %d` / `statfs` Fsid) | same FS as `/`? | filesystem |
| --- | --- | --- | --- | --- |
| `/` and `/tmp` | — | `2051` / `16146614864190929547` | — | `/dev/sda3`, 147G, **87% used** |
| `/dev/shm` | — | `28` / `5229408204322450386` | **NO** | tmpfs (RAM) |
| `/home`, `/home/user/workspace` | — | `2052` | NO | `/dev/sda4`, 766G, 59% used (ext4) |
| `tmpdir` = `/dev/shm/gopi-test-scratch/tmp` | `/dev/shm/gopi-test-scratch` (the scratch home is a **symlink** onto tmpfs) | `28` | **NO** | tmpfs |

**Why `~/workspace` was rejected as the temp root — the decisive measurement** (`devdoc/tmp-ban-scratch-root.md`, 2026-09-11, peerbox, `GOPI_TEST_TMPDIR=<root> go test ./internal/tools/ -run TestFreshDBOpenCloseIsFast`):

| temp root | fresh ledger DB open+INSERT+close (median) | ratio vs in-memory control | verdict |
| --- | --- | --- | --- |
| `/dev/shm` (tmpfs) | **16.8ms** | 1.41x | PASS |
| `~/workspace/...` (ext4 `/dev/sda4`) | **411.9ms** | **29.97x** (limit 12.0x) | **FAIL** |

Measured suite-level consequence: `internal/tools` took **601s (FAIL — the 10-minute default go-test timeout) on the ext4 workspace vs 237s (PASS) on tmpfs**. Off-`/` is necessary but **not sufficient**: the tree holds SQLite scratch, whose close-time checkpoint fsync pays a block-device round trip on *any* block filesystem. That is why the answer is "off `/` **and** memory-backed", and why `~/workspace` alone cannot be it.

### So where did `~/workspace` land instead? (all three, deliberately)

1. **The temp root's PATH** — `<home>/workspace/gopi-test-scratch` *is* under `~/workspace`; on this unprivileged host that directory is a symlink onto `/dev/shm` (`scripts/setup-scratch-home.sh`), so the path requirement and the fsync requirement are satisfied at once. On a privileged host the same script mounts a tmpfs at that path — the operator's deliberate switch, never a password gopi asks for (Rule 25).
2. **Worktrees / repo bulk output** — default root `~/workspace` (`internal/worktreeroots`).
3. **Go build caches** — `GOTMPDIR`/go-build under the workspace (`.gotmp`, `gopi-modcache-*`).

**Nothing was silently changed.** The setting's value is recorded, gated and reproducible: `cmd/agent/tmpdir_settings_value_test.go` (branch `feat/tmpdir-settings-value`, `devdoc/tmpdir-settings-value.md`) asserts the *operator's own* settings file carries a `tmpdir` whose device id differs from `/`'s.

---

## 2. Completed — the default the operator asked for

`"can we setup this tmpdir or TMPDIR by default to avoid /tmp?"` → **yes, implemented.**

Before: with no `tmpdir` key in settings.json and no caller `TMPDIR`, gopi published *nothing* and every child fell back to `os.TempDir()` → `/tmp` on the `/` partition. That fallback was the documented defect (2026-09-15 audit: `/tmp` = 4.0G, 1,310 entries aged 1–7d, largest ones 80MB ledger-DB copies — **nothing reaps them**: the scratch GC is namespace-limited to the scratch tree by construction).

After:

- **`scratch.DefaultTmpDir()`** (`internal/scratch/tmproot.go`) returns `<scratch home>/tmp` — the same directory this host's settings key already pins, resolved canonically.
- **The check comes BEFORE the write**: `scratch.AssertPathOffRootFS(path)` measures the filesystem of the path's **nearest existing ancestor** and refuses a temp root on `/`. A host whose scratch home sits on `/` gets a refusal and an **untouched filesystem**, never a directory created in the wrong place and then condemned. (Design consequence: the function creates nothing on `/` even in the failure case.)
- **`injectSettingsTmpDir`** (`cmd/agent/tmpdir_inject.go`) now resolves in this order:
  1. caller's `TMPDIR` (always wins — unchanged, the epic's hard constraint),
  2. settings.json `tmpdir`,
  3. **the measured default** (new) — and a host where the default cannot be provided is reported LOUDLY on stderr with the reason, then keeps the ambient root (never silent, Rule 15).
- Every report line now names **which** source is in force: `from settings key tmpdir` vs `from the built-in default (no tmpdir key, no caller value)`.
- `scratch.TmpDirName` / `RuntimeClass*` / `TmpDirSettingsKey` constants only — no bare literals (Rules 1b/13/14).

### Why the default is the *shared, stable* `tmp` child and not a per-process GC-bound dir

The first draft made the default a per-pid runtime queue entry so the age GC would collect it. That is wrong, and the reason is the sweep's own hard rule: **a child may outlive the gopi that published `TMPDIR`** (detached worker, `nohup` handoff, a daemon's descendants). A per-process entry is `queueChildCollectable` once unheld+stale — the GC would delete a live child's temp root from under it = data loss, while a leaked directory is only disk usage. So the default is the ONE shared child (`<scratch home>/tmp`), which is not in the sweep's namespace at all. `TestDefaultTmpDirIsTheOneSharedChild` pins this as a design decision, not an accident.

---

## 3. Verification

```
$ go build ./...                                          # OK
$ go vet ./internal/scratch ./cmd/agent ./internal/config  # OK
$ go test -count=1 ./internal/scratch ./internal/config ./internal/testutil ./internal/standards
ok  github.com/nick/gopi/internal/scratch      (9.6s / 16.3s across runs)
ok  github.com/nick/gopi/internal/config
FAIL github.com/nick/gopi/internal/testutil   59.1s   <-- PRE-EXISTING master red, see below
ok  github.com/nick/gopi/internal/standards   9.3s
$ go test ./cmd/agent/ -run TmpDir -count=1               # 3 named cases, all PASS
```

**The two reds observed are BOTH pre-existing on unmodified master** (verified by running them in a throwaway detached worktree at `master` = `51edd8d9`, created for the measurement and removed after; neither red touches a file this branch modifies — `git diff --name-only master` lists 9 files, none of them the ones below):

| red | evidence it is not this branch |
| --- | --- |
| `internal/testutil TestWorkerOwnershipPointerPresent` | reproduces on unmodified master with the identical 4 files (`briefings/brief-daemon-scope-unit-name-per-profile.md`, `briefings/brief-worker-child-stdout-to-session-dir.md`, `devdoc/nightshift-cgroup-worktree-resume-20260915.md`, `devdoc/worktree-residue-closeout.md`) — the already-documented "regrowth of an already-fixed class" (`devdoc/tmpdir-merge-resolution.md` §2). This branch adds no devdoc/briefing that teaches a launch shape. |
| `cmd/agent TestWorkerDaemon_{ForegroundWorkerIgnoresTheLock,StopEndsTheDaemon,RestartReplacesTheDaemon}` | reproduce on unmodified master in 5.2s (`daemon start failed: daemon exited during startup without reporting (see the log): EOF`) — an environment/host red around `gopi worker --daemon`'s `systemd-run` scope escape, not a tmpdir assertion. |

- **RED-proof (mutation), `internal/scratch`:** injecting `return nil` at the top of `AssertPathOffRootFS` makes BOTH negative cases fail (`TestAssertPathOffRootFailsOnRootFS` — "accepted a temp root on the SAME filesystem as /"; `TestDefaultTmpDirRefusesAScratchHomeOnRootFS` — "returned … for a scratch home on the `/` filesystem"). Restored byte-identical afterwards. The assertion is therefore seen failing (Rule 13).
- **Measured, never spelled** — every verdict in the new tests is a `statfs` device-id comparison (`/` = `16146614864190929547`, the default = `5229408204322450386`).
- **Real-child e2e** (`cmd/agent`, against the real binary + a real shell through the bash tool): with NO settings key and NO caller `TMPDIR`, the spawned shell's own `/proc/self/environ` carries `TMPDIR="/dev/shm/gopi-test-scratch/tmp"` — device `5229408204322450386`, off `/`. The old expectation ("reports NO TMPDIR") was the defect and is now inverted, so the observation stays falsifiable.
- **Invariants kept:** configured key still reaches the child tree; a caller's `TMPDIR` still wins over both the key and the default.

## 4. Not Done (honest)

- **Nothing collects the files INSIDE `<scratch home>/tmp`** — by design (see §2's data-loss argument), not by oversight. Measured now: `/dev/shm/gopi-test-scratch/tmp` = **2.7M**, of which 28 abandoned `gopi-tmpdir-value-probe-*` directories (empty, one per past run of THIS branch's own probe — reclaimed by hand in this session, see below). The scratch **home** is age-swept, but this child is not in the sweep's namespace, so an operator's own temp data there is unbounded by gopi. A future task could give it a residency-bounded sweep (age + `.holder`) — deliberately not attempted here: the outliving-child case needs a liveness channel that survives its publisher, which the `.holder` pid+starttime format only provides for a *registered* owner.
- **The probe's own leak WAS fixed in this branch (found by that measurement).** `TestTmpDirSettingsValueReachesTheSpawnedChild` (the value probe) reclaimed nothing, so every GREEN run left one `gopi-tmpdir-value-probe-*` directory inside the very temp root the epic publishes; 28 had accumulated. The reclaim is now registered with `t.Cleanup` before the case's assertions (so it survives a failure in the assertions) and is shared with the RED case (`tmpDirValueReclaim`), and the residue is guarded by `TestTmpDirValueProbeLeavesNothingBehind` + `TestTmpDirValueProbeResidueGuardRedProof`. **The guard's SCOPE was corrected 2026-09-28** (worktree `task/01a0e5d54ac97b9cb5000179af6d9c7e`): the original shape audited the WHOLE shared temp root and flagged any probe-named entry older than 10 minutes, so it was red on a PRISTINE master checkout over a FOREIGN orphan while this run created and reclaimed nothing (measured before=1 after=1; the flagged entry was 58m55s old, from an earlier unrelated run) — a guard that fails on a stranger's litter cannot be read as evidence about this run's reclaim. It now asserts on the probe's OWN child-reported directory (the path `tmpDirValueReclaim` is registered on), and its red proof withholds that reclaim on the probe's real directory — the same spawn, the same path, the same predicate — so a leaked landing directory is still caught and the guard cannot become vacuous.
- **Residue OUTSIDE the scratch home is untouched.** Measured `/dev/shm` (33% used, 5.3G free; total 2.6G): 2.5G inside the scratch home, plus ~70M of ad-hoc directories no namespace owns (`gopi-ver-01a0a4f9`, `gopi-e2e-pass-rebuild-1012006`, `gopi-e2e-resolveorder-1012577`, `gopi-tb-verify`, `gopi-move-proof.*`, `loop.go.bak`). The levers are `gopi scratch-gc base` (the memory-base arm) and `GOPI_TEST_SCRATCH_GC_MIN` (resident cadence) — an operator action, not a code change in this branch.
- **`~/workspace` was NOT adopted as the value.** That is the deliberate resolution of this report (§1), not a deferral: adopting it would re-introduce a measured 25x slow path (601s FAIL) to move data that is *already* off `/`.
- The go-build cache that `go test`/`go build` create before any test binary runs is still the ambient `GOTMPDIR` (memory: "no repo change can move that" — only `GOTMPDIR` does).

## 5. Where the deliverable lives

- Branch **`fix/tmpdir-default`** (worktree `~/workspace/gopi-tmpdir-default`, shared with the main repo — no push needed).
- Recover any file with: `git show fix/tmpdir-default:<path>` (e.g. `git show fix/tmpdir-default:internal/scratch/tmproot.go`).
- Committed in the MAIN repo's object store (worktree-shared refs), cleaned up only after this declaration.
