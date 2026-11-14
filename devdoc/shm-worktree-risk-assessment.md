<!-- doc-class: historical -->
# /dev/shm Worktrees — Risk Assessment (measured on this host, 2026-09-13)

**Branch:** `feat/shm-worktree-assessment` · **Worktree:** `~/workspace/gopi-shm-risk`
**Type:** investigation + risk analysis (no production code changed; no system change made)

## 0. Answers to the operator's questions (each with its measured basis)

| Question | Answer |
|---|---|
| Is `/dev/shm` shared memory? | It is a **tmpfs directory** (a RAM-backed filesystem), *not* the POSIX/SysV shared-memory namespace. On this host: `tmpfs on /dev/shm type tmpfs (rw,nosuid,nodev)`, size = 7.8 G = half of RAM. |
| What is the purpose of a git worktree there? | Never GoPi's documented default (`GOPI.md` says `~/workspace/gopi-<task>`). Two real sources exist: (a) the **test/verify harness** — `GOPI_TEST_SCRATCH_HOME` is symlinked into `/dev/shm` (`scripts/setup-scratch-home.sh:49`), so fixtures create worktrees there for tmpfs speed; (b) **ad-hoc agent worktrees** hand-created with `git worktree add /dev/shm/...` for control/scratch copies. |
| Does it consume system resources? | Yes — RAM, not disk. Measured: each worktree 37–62 MB / 1878 files; the 19 of 63 registered worktrees under `/dev/shm` total **1 015 MB**, and the test scratch home is **2 988 MB** → the two together account for essentially all of `/dev/shm`'s 3.9 G/7.8 G (`Shmem: 2.08 GB` of 15.5 GB RAM). History is **not** duplicated (shared object store). |
| Does gopi clean up? | For **task-attached** worktrees: yes, three code paths (`task_reclaim.go:751`, `task_finish_cleanup.go`, the serve merged-cleanup pass). For **ad-hoc / fixture** worktrees: **no** — measured 11 stale fixture worktrees still registered, and 3.0 GB of test scratch persisting. |
| Is it a normal worktree? | Yes. `cat /dev/shm/gopi-hf-control/.git` → `gitdir: /home/user/work/GoPi/.git/worktrees/gopi-hf-control`. Identical git semantics, different filesystem; `git worktree remove <path>` works the same and `scripts/safe-merge.sh` guard [c] treats it identically. |
| Does it break other processes' shared-memory IPC? | **No, not by namespace.** Measured: **zero** POSIX shm objects in `/dev/shm` (no `.name`/`sem.name` entries) and X11 using filesystem sockets under `/tmp/.X11-unix`. Nuance measured later in the same session: Xvfb *does* map one **SysV** segment (`/SYSV00000000`) — a different namespace, see §5. The interference channel is **RAM (resource)**, not name collision or corruption. |
| Is there danger? | Yes — one real one: **RAM/OOM blast radius**. `MemFree` measured at 265 MB, tmpfs pages cannot be reclaimed to disk, and an OOM-kill is host-wide (it can take the orchestrator, a sibling agent, or Xorg). Plus volatility (data loss on reboot), a shared unquota'd cap (ENOSPC hits everyone), and world-writable, predictable names. |
| Should we add whitelist/blacklist guards? | Yes for **coarse, cheap** ones (declared worktree root, path/command deny rules, RLIMIT/cgroup for child processes). **No** to seccomp-as-the-answer: it is the wrong layer for agent mistakes (high effort, fragile against the Go runtime) — it contains exploits, not slip-ups. Ranked list in §7. |

## 1. What `/dev/shm` is on this host

```
$ mount | grep shm
tmpfs on /dev/shm type tmpfs (rw,nosuid,nodev)
$ df -h /dev/shm
tmpfs  7.8G  3.9G  4.0G  50% /dev/shm          # no size= option → default = 50% of RAM
$ grep -E 'MemTotal|MemFree|MemAvailable|Shmem:|SwapFree' /proc/meminfo
MemTotal: 16213808 kB   MemFree: 265512 kB   MemAvailable: 11505036 kB
Shmem:     2078904 kB   SwapFree: 1100216 kB
$ ls -ld /dev/shm
drwxrwxrwt 76 root root 2480 Sep 13 04:44 /dev/shm
```

Facts that matter:

* **It is a filesystem, not a "memory object"**: a `tmpfs` mount instance. Files are pages in the page cache; they are *not* reclaimable to disk (unlike a file's clean cache pages) — they can only be freed by the owner or pushed to swap.
* **It is not the POSIX/SysV shm namespace.** `shm_open("x")` creates `/.x` here and `sem_open` creates `/.sem.name`; SysV `shmget` lives in a *separate* namespace (`/proc/sys/kernel/shmall|shmmax|shmni`). Occupying the *directory* therefore consumes neither namespace — it consumes RAM.
* **Cap = 50 % of RAM by default** (7.8 G on a 15.5 G box) — no per-directory quota inside it.
* **Security-relevant mount flags**: `nosuid` + `nodev` are the correct hardening (complements the sticky 1777). There is **no `noexec`**: exec-capable, and the suite relies on that (see H5).
* **Volatile by construction**: an ordinary reboot empties it.

## 2. Why a worktree ends up there (root cause, not guessing)

The documented default is **not** `/dev/shm`:

```
GOPI.md:159: git worktree add ~/workspace/gopi-<task> -b feat/<name>
```
`grep -rn "dev/shm" internal/agent/ internal/tools/*.go` → *no production hit*: no prompt steers worktrees to tmpfs.

Two mechanisms produce them anyway — both measured:

1. **Test / verify harness fixtures.** `scripts/setup-scratch-home.sh` symlinks the scratch home to `/dev/shm` (`MEMORY_BASE="${GOPI_SCRATCH_MEMORY_BASE:-/dev/shm}"`); `internal/testutil/scratchroot.go` pins suite temp roots (`ShmDir = "/dev/shm"`). Suites then create *linked worktrees* inside their scratch root — visible in the registry as
   `/dev/shm/gopi-test-scratch/wbr-f2/master`, `wbr-f2/merged`, `wbr-final/master`, … and in session payloads for the merge/verify suites. This is a **deliberate** choice: tmpfs avoids boot-device fsync stalls (measured elsewhere in this repo: cold DB open 14–16 ms on `/dev/shm` vs seconds under I/O load on the boot device — `devdoc/freshdb-contention-guard.md`).
2. **Ad-hoc agent worktrees.** Names like `/dev/shm/gopi-hf-control`, `/dev/shm/gopi-stop-A`, `/dev/shm/listhonest-control`, `/dev/shm/gopi-glm-pristine-master`, `/dev/shm/gopi-ver-01a095c7-{pre,post,mut}` are hand-created control/scratch copies. An agent picked `/dev/shm` because nothing told it which root is *execution* scratch vs *permanent* work.

Measured share (snapshot — the registry is live, see §4):
```
$ git worktree list --porcelain | awk '/^worktree /{print $2}' | grep -c '^/dev/shm'   → 19
  total 64:  19 /dev/shm | 20 ~/workspace | 24 /tmp | 1 main repo
  /dev/shm breakdown: 11 test fixtures (wbr-*, wbr-m*) + 8 ad-hoc (gopi-*, listhonest-control)
```

## 3. Resource cost (measured)

* **History is shared, the working tree is not.** `/dev/shm/gopi-hf-control/.git` is a one-line pointer file to the main repo's object store (`/home/user/work/GoPi/.git/worktrees/...`; the whole `.git` is 157 MB for 63 worktrees). So a worktree costs *working-tree bytes only*.
* **Per worktree:** 37–62 MB, 1 878 files (uniform across the ones measured — checkout + build artifacts).
* **Total:** the 19 registered `/dev/shm` worktrees = **1 015 MB** (26 % of the 3.9 G used, up to 62 MB each — measured across all 19, not only the `gopi-*` names); the scratch home `gopi-test-scratch` = **2 988 MB** (74 %). Both tenants are sizeable: the worktrees are not negligible.
* **RAM effect:** `Shmem` 2.08 GB = 13 % of RAM; `/dev/shm` 3.9 G used of a 7.8 G cap; `MemFree` 265 MB (volatile — `MemAvailable` 11.5 GB shows most of that is reclaimable page cache, which is why the box is *not* currently thrashing).
* **Root filesystem:** unaffected — that is precisely the benefit when the alternative is the boot device.

## 4. Does gopi clean up? (code + measurement)

Cleanup **exists and is real** for the task lifecycle:

* `internal/tools/task_reclaim.go:751` — `reclaimGitRemove` runs the ordinary `git worktree remove` (never `--force`) for a reclaimable row's deliverable worktree.
* `internal/tools/task_finish_cleanup.go:19` — finish-cleanup step ③ `git worktree remove <wt> --force`, only after the branch was pushed.
* the serve "merged-cleanup" pass removes the branch + worktree of a merged branch (verified E2E in an earlier task, memory #1265).

Gaps, measured on this host:

* Every path above is **task-attached**. Ad-hoc and fixture worktrees are never in a task row → **no sweeper sees them**. Evidence: the three `gopi-ver-01a095c7-*` worktrees created by/for task `01a095c7` are still registered, as are the `wbr-*` fixtures.
* Test scratch has its **own** age-GC with a live-process holder check (`internal/testutil/legacy_residue.go`, `ScratchStaleAge`) — deliberately *not* extended to worktrees.
* The git side is healthy — **no dead registrations** (`git worktree list` × `[ -d ]` → 0 dead), so the residue is *live registrations that nobody will finish*, not ghosts. One of them still holds uncommitted work: `git -C /dev/shm/gopi-ver-01a095c7-pre status --porcelain` → `?? cmd/agent/zz_verifier_redproof_test.go`, for a row that is now **terminal (`[verified]`)** — the volatile-filesystem risk (H2) with a real instance.
* Leftovers from earlier experiments sit in the shared dir itself (`b.rules`, `c.rules`, `cntprobe`, `commprobe`, `execprobe`, `glr-gopi-testbin` (21 MB), `orphan-e2e.*`, `*.log`, `*.list`) — a sign that names there are claimed opportunistically.

**Dry-run reclaim inventory (the shape §7-C's sweeper would produce, run against real state):** all 19 `/dev/shm` registrations, with `DIR_EXISTS`, `git status --porcelain` line count, live `/proc/*/cwd` holder count, size, HEAD state:

```
PATH                                            EXISTS  DIRTY  HOLDERS  MB  HEAD
/dev/shm/gopi-glm-pristine-master                 yes      0      0      58  DETACHED
/dev/shm/gopi-hf-control                          yes      0      0      61  DETACHED
/dev/shm/gopi-stop-A                              yes      0      0      37  DETACHED
/dev/shm/gopi-test-scratch/wbr-f2/master          yes      0      0      58  DETACHED
/dev/shm/gopi-test-scratch/wbr-f2/merged          yes      0      0      58  DETACHED
/dev/shm/gopi-test-scratch/wbr-f3/master          yes      0      0      62  DETACHED
/dev/shm/gopi-test-scratch/wbr-f3/merged          yes      0      0      62  DETACHED
/dev/shm/gopi-test-scratch/wbr-final/master       yes      0      0      62  DETACHED
/dev/shm/gopi-test-scratch/wbr-final/merged       yes      0      0      62  DETACHED
/dev/shm/gopi-test-scratch/wbr-time               yes      1      0      62  DETACHED   ← KEEP (dirty)
/dev/shm/gopi-test-scratch/wbr-tip                yes      0      0      62  DETACHED
/dev/shm/gopi-ver-01a095c7-mut                    yes      1      0      37  DETACHED   ← KEEP (dirty)
/dev/shm/gopi-ver-01a095c7-post                   yes      0      0      57  DETACHED
/dev/shm/gopi-ver-01a095c7-pre                    yes      1      0      37  DETACHED   ← KEEP (dirty; row terminal [verified])
/dev/shm/gopi-verify-wt                           yes      0      0      37  DETACHED
/dev/shm/gopi-vwt-teDW                            yes      0      0      37  DETACHED
/dev/shm/listhonest-control                       yes      0      0      42  DETACHED
/dev/shm/wbr-m                                    yes      0      0      62  DETACHED
/dev/shm/wbr-m2                                   yes      0      0      62  DETACHED
```

19/19 directories exist (no ghosts), 0 live holders, **3 dirty → never auto-remove** (report only), 16 clean candidates ≈ 940 MB. Note the inventory **changed within this session** (fixture registrations `wbr-f2/f3` disappeared; `gopi-verify-wt`, `gopi-vwt-teDW`, `wbr-m`, `wbr-m2` appeared) — the count is a snapshot taken by a *live* harness, so any sweeper must re-read `git worktree list --porcelain` per run and must never assume a worktree is idle because it was idle a minute ago.

**"Is this just a normal worktree like any other path?"** Yes, confirmed: same gitdir pointer, same `git worktree remove`, same `safe-merge.sh` guard, same rule 6a semantics. The only differences are the underlying filesystem (RAM, volatile) and that the *sweeper* does not know about non-task paths.

## 5. Interference with other processes' IPC — verdict

* **POSIX shm namespace: 0 objects.** `ls -a /dev/shm | grep -E '^\.|^sem\.'` → none (122 non-dot entries, all ordinary dirs/files). Filling the directory cannot collide with a `shm_open`/`sem_open` name.
* **SysV shm: separate namespace** — untouched by `/dev/shm` occupancy.
* **X11 on this host (corrected 2026-09-13 — the earlier reading was imprecise):** Xvfb `:99` **does** map shared memory, but a **SysV** segment, not a `/dev/shm` entry:
  ```
  $ grep -E '/SYSV|/dev/shm|memfd' /proc/$(pgrep Xvfb)/maps
  7f62d98fb000-7f62d997b000 rw-s 00000000 00:01 2260994   /SYSV00000000 (deleted)
  $ ipcs -m        # SysV namespace: 15 segments, 2 attachments each, 71.0 MB total (lightdm holds one 64 MB)
  ```
  Zero `/dev/shm`-file mappings in Xvfb; its sockets are *filesystem* sockets (`/tmp/.X11-unix/X0|X240`, protocol-mandated). The earlier `awk '/shm|memfd/…'` count of 0 was a **pattern** miss (it does not match `/SYSV`), not evidence of no shared memory — an honest correction to §5 as first written.
* **Who actually maps files out of `/dev/shm` (host-wide scan, `grep -rl '/dev/shm' /proc/[0-9]*/maps`):** exactly 2 processes — a suite test binary mapping sqlite `m2.db-shm` files, and `gopi-merger merger --every 1h` **executing a test binary from `/dev/shm/gopi-test-scratch/.../merger-daemon-bin/gopi-merger-daemon`**. That is the real coupling: a long-lived daemon's executable lives in the tmpfs the suites GC; nothing to do with IPC namespaces, everything to do with a shared RAM-backed mount.
* **Verdict:** a worktree living in `/dev/shm` does **not** corrupt, block, or delay another process's IPC. The interference channel is purely **resource**: the same 7.8 G cap, and RAM that cannot be written back.

## 6. Hazards, ranked (MEASURED vs THEORETICAL)

| # | Hazard | Class | Detail |
|---|---|---|---|
| **H1** | **RAM pressure → OOM kill** | measured | tmpfs pages are not reclaimable to disk. The 50 %-of-RAM default (7.8 G of 15.5 G) is **not** an explicit policy on this host — `/dev/shm` appears in neither `/etc/fstab` nor a `dev-shm.mount` systemd unit, so nobody has ever chosen a size. The swap cushion is largely **spent**: `swap.img` 4 G with only **1.11 G free** (`SwapFree`), `SwapCached` 108 MB, `swappiness` 60, `overcommit_memory` 0 — so a tmpfs overrun does not land on a fresh swap; it goes to the OOM killer, which is host-wide and picks its own victim (orchestrator, sibling agent, or Xorg). This is the only "disaster" class that is real today. |
| **H2** | **Volatility → data loss** | structural | Reboot/crash empties `/dev/shm`: uncommitted work, untracked files, `.gopi-tags` state vanish. Committed objects survive (shared object store). Afterwards the directories exist but are empty → `git worktree list` shows a *pristine* worktree, and a later `git worktree prune` cannot help (the directory is present). |
| **H3** | **Shared, unquota'd cap → ENOSPC for everyone** | measured | One runaway tenant (the test scratch home is already 3.0 GB) can consume the whole cap; then *any* process writing there — browser, DB, another suite — fails with ENOSPC. No per-worktree limit exists. |
| **H4** | **World-writable dir + predictable names** | measured | mode 1777, 122 entries, names like `gopi-ver-<task>-pre` are guessable; any local process can pre-create a path or a symlink before a later `worktree add`/write follows it. `nosuid`/`nodev` mitigate privilege escalation but not path hijack. |
| **H5** | **exec portability** | measured | `/dev/shm` here is *without* `noexec` and the suite relies on that (test binaries were run from `/dev/shm/gopi-test-scratch/buildprobe/*.test`, `devdoc/agent-test-cpu-investigation.md:67`). On a hardened host mounting tmpfs `noexec`, every suite pinned to it breaks — a silent portability trap. Conversely, a world-writable *executable* mount is a mild local-security smell. |
| **H6** | **No-signal cleanup** | measured | Cleanup keys off the task ledger; worktrees created outside it (ad-hoc/fixture) accumulate with no owner and no age-GC — measured 11 such registrations. |
| — | *corruption of another process's data* | **theoretical** | tmpfs is a real filesystem with normal POSIX semantics; a worktree inside it cannot corrupt a *different* file by existing. Do not plan guards against this. |

## 7. Recommendation — and the sandbox whitelist/blacklist question

**A. Path model (cheap, deterministic).**

1. Keep `/dev/shm` for what it demonstrably wins at — **ephemeral test/verify scratch** (fresh-DB work: 14–16 ms vs seconds on the boot device under load).
2. Declare a **worktree root** per repo instead of free-form absolute paths in the matrix: `settings.json → "repos": {"<path>": {"build": …, "worktree_root": "~/workspace"}}`, default `~/workspace`. `/dev/shm` is then allowed **only** for the scratch home (which already has its own resolver, `internal/testutil/scratchroot.go`) — a production `git worktree add /dev/shm/...` becomes a policy violation rather than a judgement call.
3. If a fast transient copy is genuinely needed: (a) it must live under the scratch home (so residue GC owns it), and (b) it must be torn down by the creating step or by the age-GC — never left registered.

**B. Bound the tmpfs (defense against H1/H3).**

Mount `/dev/shm` with an explicit `size=` (e.g. 2–4 G) instead of the 50 %-of-RAM default: a runaway then fails early, locally, with ENOSPC, instead of OOM-killing an unrelated process at the worst moment. **Measured gap:** the current 7.8 G is nobody's decision — there is no `/etc/fstab` entry and no `dev-shm.mount` systemd unit on this host, so the kernel default applies and nothing documents it. `size=4G` must be chosen with §3's measured tenants in mind (test scratch alone is 3.0 GB); a useful intermediate is to make the loader script set the cap and to keep the scratch home under the *same* resolver that already owns it.

**B2. The clamp that actually stops a host-wide OOM: a systemd slice for gopi children.**

A mount option bounds the *shared* pool; it does not bound *who eats it*. The measured danger is a gopi child (suite, daemon, agent) allocating without limit: `swap.img` is 4 G with only ~1.1 G free, so the overrun lands on the OOM killer, which is host-wide and picks its own victim. Put the gopi process tree in its own slice with `MemoryMax` / `MemoryHigh` / `TasksMax` (e.g. `systemd-run --slice=gopi.slice -p MemoryMax=6G --scope …`), so one runaway is killed **inside its own cgroup** and the orchestrator, a sibling agent and Xorg survive. This is the guard that answers "one mistake causing disaster" for real; §7-D.1 (RLIMIT) is its zero-dependency cousin when cgroup delegation is unavailable.

**C. Extend the existing sweeper to worktrees (H2/H6).**

`internal/testutil/legacy_residue.go` already implements the right shape for scratch dirs: name-namespaced, holder-aware (skip dirs held by a live `/proc/*/cwd|fd`), age-based, dry-run first, "unmeasurable ⇒ KEEP". Reuse that machinery for worktree **registrations** under a declared scratch base, and pair it with `git worktree list --porcelain` (never per-row `prune` loops — the attribution pitfall in memory #2127).

**Holder detection must also cover `/proc/*/maps` — measured, not theoretical.** §5 found `gopi-merger merger --every 1h` (pid 293168) **executing** `/dev/shm/gopi-test-scratch/suite-cmd-agent-run-69512576/merger-daemon-bin/gopi-merger-daemon`, and a suite test binary mapping sqlite `m2.db-shm` files. Neither process has that path as its `cwd`, so a `cwd/fd`-only holder check would classify both as garbage and reclaim the tmpfs out from under a running daemon. The scratch GC's holder pass must therefore scan `maps` (and `exe`/`fd`) as well; a scratch dir whose binary is mapped by a live process is a KEEP regardless of age — and, longer term, *nothing should be executed out of the scratch home*, because the home is designed to be GC'd.


**D. Blast-radius guards — the operator's whitelist/blacklist question.**

Ranked by value/cost for *agent mistakes* (this is the real danger: a fork bomb or a `rm -rf` slip is far more destructive than a worktree path):

1. **Resource limits first (highest value).** `RLIMIT_AS` / `RLIMIT_NPROC` (or a cgroup) on the agent's child processes: this is the guard that actually prevents "one mistake causing disaster" — a runaway test or fork bomb kills only itself. Nothing else on this list comes close.
2. **Command deny rules (coarse blacklist).** A dangerous-command guard already exists (it refuses `rm -rf` outside the workdir — memory #1265). Extend *by shape*, not by enumeration: refuse recursive delete / `chown` / `mount` / `dd of=/dev/*` targeting anything outside the row's declared roots (`worktree_root`, scratch home, `/tmp` fixtures); refuse `git worktree remove` on a path outside the row's roots; require the path to be a *declared* root rather than comparing strings.
3. **Path policy (whitelist for writes).** Writes/worktree creation allowed only under declared roots; `/dev/shm` writes only under the scratch home. Implement as a small predicate + a static gate (Rule 16–21 style) so no production code can hardcode `/dev/shm/<name>` again — the constant/escape pattern already used by `internal/standards`.
4. **Do NOT reach for seccomp as the answer here.** Syscall filtering targets *exploit* containment at the syscall layer; it is high-effort, Go-runtime-fragile, and says nothing about the actual failure modes above (RAM, path, recursion). A whitelist of *system calls* buys almost nothing against "an agent ran the wrong command", while a whitelist of *paths* + an rlimit buys most of it.

**E. Cheap immediates (no design needed).** Sweep the stale fixture registrations and the empty ad-hoc worktrees under `/dev/shm` — the §4 dry-run inventory says **16 of 19 are clean, 0 have a live `/proc/*/cwd` holder, and 3 must be kept** (`wbr-time`, `gopi-ver-01a095c7-mut`, `gopi-ver-01a095c7-pre` — each carries `git status --porcelain` output; `-pre` still holds `cmd/agent/zz_verifier_redproof_test.go` for a row that is already terminal `[verified]`, so the file is almost certainly the leftover of a finished verification and needs a human glance, not a `--force`). The same pass should remove the experiment leftovers in the shared directory itself (`cntprobe`, `execprobe*`, `commprobe*`, `*.rules`, `orphan-e2e.*`, `glr-gopi-testbin` 21 MB). Order matters: **check dirty → report → remove clean**, never the reverse, and never with `--force`.

## 8. Not done / limitations (honest)

* **Read-only investigation.** No system change was made: nothing was cleaned, no mount options altered, no guard implemented. §7 is a recommendation, not a shipped change.
* **Guards are follow-up work**, submitted as a separate task row (see §10) — implementing them needs its own design, tests and LLM-adoption proof (Rule 11), which is out of scope for an investigation.
* **Single host, single moment.** `MemFree` is volatile; the `/dev/shm` 50 %-of-RAM default is a kernel default (systemd mounts it that way by default on many distros) — `df` on another host may differ.
* **The X11 measurement is one process snapshot**; a client can switch to shared pixmaps dynamically. The structural argument (§5: separate namespaces) is what makes the verdict robust, not the single reading.
* **No build/test run** for this change: the deliverable is a markdown document, so `go build`/`go test` are not applicable (stated rather than skipped silently).

## 9. Verification (commands whose output backs §1–§6)

```
mount | grep shm ; df -h /dev/shm ; grep -E 'MemTotal|MemFree|MemAvailable|Shmem:|SwapFree' /proc/meminfo
findmnt -no TARGET,FSTYPE,OPTIONS /dev/shm ; grep -i shm /etc/fstab ; ls /usr/lib/systemd/system/dev-shm.mount
cat /proc/sys/vm/swappiness /proc/sys/vm/overcommit_memory ; swapon --show
ls -ld /dev/shm ; ls -a /dev/shm | grep -cE '^(\.|sem\.)'          # POSIX shm objects → 0
git worktree list --porcelain | awk '/^worktree /{print $2}'       # 64 registrations, 19 under /dev/shm
for d in $(…shm worktrees…); do du -sm "$d"; done                  # 37–62 MB each
du -sm /dev/shm/gopi-test-scratch                                  # ~3.0 GB, 586 children
cat /dev/shm/gopi-hf-control/.git                                  # gitdir: …/GoPi/.git/worktrees/gopi-hf-control
grep -E '/SYSV|/dev/shm|memfd' /proc/$(pgrep Xvfb)/maps            # 1 SysV segment — NOT 0 (§5 correction)
ipcs -m                                                            # SysV namespace, ~15 segs / 71 MB
grep -rl '/dev/shm' /proc/[0-9]*/maps                              # exactly 2 procs (sqlite -shm, merger daemon exec)
while read p; do [ -d "$p" ] || echo DEAD $p; done < <(worktree list)   # 0 dead registrations
for w in $(…shm worktrees…); do echo "$w $(git -C $w status --porcelain | wc -l)"; done   # 3 dirty → keep
grep -rn "dev/shm" internal/agent/ internal/tools/*.go             # no production steering hit
```

## 9b. Amendment log (this document was corrected, not rewritten)

| Amendment | Section | What changed |
|---|---|---|
| X11/SysV correction | §0, §5 | The first version reported "0 shm mappings in Xvfb". Re-measured with a pattern that includes `/SYSV`: Xvfb maps one SysV segment. The **namespace verdict is unchanged**; the *evidence* was wrong. Recorded rather than quietly edited (a wrong reading left in place would mislead the follow-up task). |
| `/dev/shm` has no author | §6 H1, §7 B | Measured that no `/etc/fstab` entry and no `dev-shm.mount` unit exist — the 7.8 G cap is an unowned kernel default, not a policy. |
| swap cushion largely spent | §6 H1, §7 B2 | `swap.img` 4 G, only ~1.1 G free, `SwapCached` 108 MB → the OOM path is closer than the first version implied; added the systemd-slice (`MemoryMax`/`TasksMax`) clamp as the guard that finally bounds a runaway. |
| dry-run reclaim inventory | §4 | Added the per-registration holder/dirty/size table the §7-C sweeper would emit, and the observation that the inventory **changed within a single session**. |

## 10. Follow-ups submitted

* Task **`01a09765-6b0e-7918-86b2-80f36d0c77a3`** (`sandbox-hardening`, priority 5, repo `/home/user/work/GoPi`) — implement the §7 guard set: declared `worktree_root` in the per-repo settings table, shape-based path/command policy, RLIMIT_AS/RLIMIT_NPROC (or cgroup) on agent children, a static literal gate with a red-proof, and reuse of the age-GC + holder-check machinery to sweep stale worktree **registrations** (dry-run first, never remove a worktree with `git status --porcelain` output). The operator's whitelist/blacklist question is answered there in code, not prose — this report deliberately ships no guard implementation (see §8).

## 11. Deliverable / recovery

* Branch **`feat/shm-worktree-assessment`** (worktree `~/workspace/gopi-shm-risk`) — the deliverable is the tip of that branch, not a frozen hash (memory #1952: a hardcoded hash is a hash-chase trap).
* This report exists **only on that branch** — it is a standalone assessment document and was intentionally NOT merged into master by the authoring agent (Rule 6d/6a; merge is the merger's call).
  `git show feat/shm-worktree-assessment:devdoc/shm-worktree-risk-assessment.md`
