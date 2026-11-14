<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown — slices 1/5 and 3/5).**
> `gopi tool doccheck` and `tools/docclass/` were DELETED (`e6bfa10`,
> `29882b3`) and this page is no exception: the command quoted below is the
> RECORD of what a past run executed — it cannot be re-run, and no document may
> be classified, verified or frozen by running it.
# /home partition exhaustion on hostb-Laptop — a host-level constraint

**Status:** measured, documented; **NO cleanup performed** (deliberately — see §7).
**Filed by:** task `01a12fbe-1fee-7e6d-b5a6-58b9298c100c` (Rule 41: an evidence-only
row — the finder does not fix).
**Host:** `hostb-Laptop`. Every number below is MEASURED on this host at the
timestamp shown; anything inferred is labelled **inferred** and is not used as a
premise for a verdict. Raw output: §Evidence index.

---

## 1. The failure mode as filed

A worker ran a chained command in its own worktree:

```bash
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ && ./bin/gopi tool doccheck ; echo exit=$?
```

It read `exit=1` and concluded **the doccheck tool was red**. The actual sequence
was: `go build` died with

```
link: mapping output file failed: no space left on device
```

so `bin/gopi` was never produced, `&&` short-circuited, `./bin/gopi` was a missing
file, and the shell reported the status of the **failed `&&` chain** — 1 — which is
byte-identical to what a genuinely red `doccheck` reports. The tool never ran; the
report said it failed.

**Why it is a defect and not merely a misread.** `A && B ; echo $?` collapses two
different worlds into one integer, and **both worlds are real on this host**
(measured §5):

| what happened | `$?` the chain reports | what a reader concludes |
|---|---|---|
| build OK, doccheck found gaps | 1 | "doccheck says there are gaps" |
| build FAILED (ENOSPC), doccheck never ran | 1 | "doccheck says there are gaps" |

`bin/gopi` is **gitignored**, so a fresh linked worktree has no binary by
construction and a missing-file failure is the *normal* starting state, not an
anomaly. The one fact that separates the worlds — did the build link of the chain
succeed — is not carried by `$?`. See §8 for the one-line fix.

## 2. Measured state of the partition

`df -h /home` (measured, this run 2026-10-12T19:49):

```
Filesystem      Size  Used Avail Use% Mounted on
/dev/nvme0n1p7  458G  432G  2.1G 100% /home
```

- `df -B1 /home` — 490 999 562 240 total, 463 751 606 272 used, **2 231 291 904 available**.
- `df -i /home` — 4 966 359 of 30 523 392 inodes (**17%**): **inode exhaustion is
  excluded**; this is byte-space exhaustion.
- `findmnt` — `/dev/nvme0n1p7 ext4 457.3G 432G 2G`.

**The constraint is INVERTED relative to Rule 22's usual direction** — the two
partitions, measured:

| mount | device | size | avail | used |
|---|---|---|---|---|
| `/` (`/dev/nvme0n1p5`) | **66307** | 183G | **131G** | 25% |
| `/home` (`/dev/nvme0n1p7`) | **66309** | 458G | **2.1G** | 100% |

The *root* partition is nearly empty while the partition everything of size lives
on is full. Consequences measured, all `stat -c %d` = **66309**:

| temp root | resolves to | device | Rule 22 verdict |
|---|---|---|---|
| `TMPDIR` | `/home/user/workspace/_tmp` | 66309 | passes the rule, sits on the full partition |
| `GOPI_TEST_HOME` scratch | `/home/user/workspace/gopi-test-scratch` | 66309 | same |
| merger build gate cache | `/home/user/workspace/gopi-build-cache` | 66309 | same |

Rule 22's criterion ("provably off the `/` partition, by device id") is satisfied
**by every temp root here** — and the host still ran out. The rule's *intent*
("not on the partition that fills up") is what this host violates. That is a
property of the layout, not a bug in the rule: on a host whose `/` and `/home` are
separate physical partitions, "off `/`" and "off the full one" are different
questions.

**The partition is under LIVE pressure, not merely full.** Four `df` samples inside
this run (all `/dev/nvme0n1p7`, available bytes):

| sample (time) | available bytes |
|---|---|
| t+117s | 2 171 174 912 |
| t+246s (after one build) | 2 091 094 016 |
| 19:49:06 | 2 168 078 336 |
| 19:49:31 (after one build) | 2 164 838 400 |

Headroom is not a state, it is a residue of whatever concurrent agents last
finished. Nothing is holding a stable 2G.

## 3. Measured consumption

| path | measured | note |
|---|---|---|
| `/home/user/.cache/go-build` (**the Go build cache**) | **119G** = 127 531 872 256 B | the dominant consumer — previously *inferred*, now MEASURED |
| `/home/user/.gopi/sessions` | 44G | session dirs / evidence artifacts |
| `/home/user/workspace/gopi-test-scratch` | 26G | suite scratch root |
| `/home/user/work` | 20G | checkouts (`glow-moon` 8.4G, `claude` 3.7G, `hermes-test` 2.2G, …) |
| `/home/user/workspace/_tmp` | 18G | the ambient `TMPDIR`; 337 children |
| `/home/user/workspace/gopi-build-cache` | 12G | the merger build gate's disk-backed cache (memory #3457's relocation) |
| `/home/user/.hermes` | 6.7G | |
| `/home/user/.gopi` (whole) | 46G | incl. `peerbox-mirror` 2.1G, `memory.db` 235M |
| `/home/user/.cache` (whole) | 121G | i.e. **go-build is 98% of the cache tree** |
| `/home/user/.cache` minus go-build | ~2.6G | `ms-playwright` 677M, `wine` 421M, `tracker` 165M, `bazelisk` 90M, … |
| `~/.cache/gopi-tools-suite-build-cache-*` | **174 dirs / 93M** | one throwaway cache per test PID — the residue class memory #3457 predicted; each dir 7.8M |

Structure of the cache, measured: 256 entries at depth 1; `trim.txt` is **10 bytes**,
mtime `2026-10-12 05:44:54` — i.e. the toolchain's own automatic trim had not run
for ~14h before this run and did not run during it.

**Measured vs inferred — the separation Rule 41 requires.**

* **MEASURED**: every size in the table (`du`), every `df` figure, the device ids,
  the inode counts, the build exit status and elapsed time (§4), every exit-status
  collision in §5, the `+~80M` delta a single build applies (§4).
* **INFERRED (and not a premise for anything here)**: that go-build's 119G is
  *reclaimable*. `go clean -cache` was **NOT** run, so how much the toolchain would
  actually drop is a hypothesis. The bytes exist; their reclaimability is untested,
  and §7 says who may test it.
* **INFERRED**: that go-build is what *filled* the disk. It is 119G **now**, and it
  is the largest single consumer by an order of magnitude; when those bytes landed
  was not measured.

## 4. Reproduction in THIS run's own worktree

Worktree `/home/user/workspace/gopi-01a12fbe1fee7e6db5a658b9298c100c`, branch
`task/01a12fbe1fee7e6db5a658b9298c100c`, HEAD `8922d775a`. Verbatim transcript
(`repro-build.txt`):

```
### STEP 1: remove the binary so the chain's first link must produce it
ls: 无法访问 'bin/gopi': 没有那个文件或目录
### df -B1 /home  BEFORE
/dev/nvme0n1p7 490999562240 463751606272 2231291904 100% /home

### STEP 2: the exact chained caller
--- CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ ---
--- go build exit=0  elapsed=2s ---
--- ls -l bin/gopi ---
-rwxrwxr-x 1 nick nick 65997732 Oct 12 19:49 bin/gopi
--- ./bin/gopi tool doccheck ---
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
--- doccheck exit=0 ---
### CHAIN OBSERVED: build_exit=0 doccheck_exit=0

### STEP 3: df / du AFTER
### df -B1 /home  AFTER
/dev/nvme0n1p7 490999562240 463818059776 2164838400 100% /home
### du -s -B1 GOCACHE AFTER
127531872256
```

**Result: GREEN.** `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` exits **0**,
producing a 65 997 732-byte binary — 2s warm (this transcript), 16s cold (the first
run of the session, when the worktree had no `bin/`). So the ENOSPC threshold is
**not** reached at 2.1–2.2G free; the filed failure happened at 45M free. The gap
(45M … 2.1G) was **not** bisected: bisecting it means filling the partition on
purpose, which Rule 27 bans.

**A build is a WRITER to the resource it needs** — measured: one `go build` grew
the cache by ~71–80M (127 460 339 712 → 127 531 872 256 B across two runs) and took
~80M of the partition (2 171 174 912 → 2 091 094 016 available). This is why the
failure is *progressive*: every attempt lowers the headroom the next attempt needs,
so a sequence of red builds marches the host to 0 free rather than plateauing.

## 5. The mechanism, measured — the exit-status collision is real and common

From `repro-misread.txt` (the red proof; runnable at any time, needs no ENOSPC,
generates no load — Rule 27):

```
########## CASE A — build SUCCEEDS, tool runs ##########
### chain status  $? = 0
### bin/gopi exists? bin/gopi

########## CASE B — build FAILS, tool NEVER RUNS (the filed case) ##########
### chain status  $? = 1
### bin/gopi exists? NO
```

And the values that chain-status 1 collides with, measured on the real binary:

| command | exit | meaning |
|---|---|---|
| `./bin/gopi tool doccheck` (clean corpus) | **0** | green |
| `./bin/gopi tool doccheck --bogus` | **1** | usage error |
| `./bin/gopi tool nosuchverb` | **1** | unknown verb |
| `./bin/gopi task show 00000000-…-000000000000` | **1** | not found |
| `build && ./bin/gopi …` with a failing build | **1** | **the tool never ran** |

So `exit=1` is not merely ambiguous in theory: it is the status of four ordinary
outcomes, one of which means "nothing happened". The distinguishers exist and are
cheap — the build's own status, the binary's existence, and the build's stderr —
and the chain's `$?` carries none of them.

Three properties compose the defect:

1. **`&&` collapses two worlds into one number** (the table above).
2. **`bin/gopi` is gitignored**, so a fresh worktree has no binary by
   construction (memory #3388); `./bin/gopi …` then yields a missing-file error
   whose status is also 1.
3. **The ENOSPC line is on the *build's* stderr**, and the observed form discarded
   it (`>/dev/null 2>&1`), so the one line naming the real cause never reached the
   reader.

## 6. The failure mode is not confined to the caller — the fix belongs to the surface

A finding worth recording because it is a *second instance of the same mistake*,
produced by this very investigation: the first draft of the detection recipe below
was written as

```bash
if ! CGO_ENABLED=0 go build … 2>"$log"; then
    echo "BUILD FAILED (exit $?) — the tool below NEVER RAN"
```

and it printed **`BUILD FAILED (exit 0)`**. The reason: `$?` inside the `then` body
of `if ! CMD` is the status of the **`!`-negated compound**, i.e. 0, not the build's
status. The author of the recipe — a reader who is *specifically looking for this
class of bug* — reintroduced it in the same minute. The lesson is not "be careful":
it is that **the status of a command is not reliable from where you are standing**,
so a surface that reports on another command's outcome must capture the status
*at the command*, into a variable, immediately:

```bash
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ 2>"$log"; rc=$?     # ← captured AT the command
[ "$rc" -ne 0 ] && { echo "BUILD FAILED (exit $rc) — the tool below never ran"; … }
```

This is why §9 files the reporting half as a system-layer row rather than trusting
the prose here: a recipe can be mis-transcribed; a caller that captures the status
itself cannot.

## 7. Remedy options — the OPERATOR's decision, with trade-offs

**None of these was executed.** Deleting a shared build cache or changing host
policy is the operator's decision, not a task's (Rule 41). Raw output for every
option's inputs is in §3.

| # | option | what it buys | cost / risk |
|---|---|---|---|
| 1 | **`go clean -cache`** (the direct 119G) | the largest single lever on this host by an order of magnitude; the build only needs ~2G, measured §4 | throws away warmth every concurrent agent shares. Measured elsewhere in this repo (memory #3457): cold 84.4s vs warm 3.7s — a **22.6×** build-time regression for everyone until the cache re-warms |
| 2 | **Bound `GOCACHE`** (a size cap) | limits growth without losing warmth | **the Go toolchain has no stock size cap.** `trim.txt` (measured: 10 bytes, 14h stale) drives an internal policy that did not bound this cache across this run's builds. A bound means machinery that does not exist yet — a Rule 35 dependency decision, and a system-layer one (§9) |
| 3 | **Move `GOCACHE` off `/home`** (`GOCACHE=/…`, e.g. on `/` with 131G free) | removes 119G from the full partition | **worst failure mode available**: `/` is the ROOT filesystem. An unbounded 119G cache relocated there can fill `/`, which takes the OS down rather than just builds. It also invalidates every existing warm entry. Do not take this without a bound, and note that a naive "off `/`" check (Rule 22's own criterion) would bless it |
| 4 | **Per-repo `GOCACHE` under the worktree root** | the merger build gate already does exactly this and it is measured good (memory #3457: disk-backed, per-repo key, 512 MiB bound, cold 84.4s vs warm 3.7s) | does not reduce total bytes, only relocates them; a cache under a *worktree* is re-COLD from the second worktree onward unless it is keyed per repo — the exact trap that recipe documents |
| 5 | **Retire the `gopi-tools-suite-build-cache-<pid>` residue** (174 dirs, 93M) | small; stops a class that grows one dir per test PID | needs a reaper in machinery that already ticks (Rule 35) — a system-layer change (§9) |
| 6 | **Bound the second tier** (`sessions` 44G, `gopi-test-scratch` 26G, `_tmp` 18G = 88G) | the unattended growers | needs a retention policy; `sessions/` holds evidence that reports cite, so deleting it can orphan a report's evidence trail |
| 7 | **Enlarge `/home`** (LVM / mount / new disk) | removes the constraint entirely | operator-only, needs offline action, out of scope for any task |
| 8 | **Do nothing to the host; make the misread impossible** (§8) | zero bytes, zero risk; converts a silent wrong conclusion into a named failure | does not restore headroom — the next build at 0 free still fails, but *loudly and correctly*, and the report will say "build failed, ENOSPC" instead of "the tool is red" |

**Recommendation, stated as one:** take **8** unconditionally — it costs nothing and
it is the actual defect (a WRONG REPORT was produced). Then, for headroom, choose
between **1** (immediate, at the cost of warmth) and **6** (structural, at the cost
of a retention policy). **3** should be considered only together with **2**, and
never on its own.

## 8. Detection recipe — so a run cannot misread ENOSPC as a tool failure

```bash
log="${TMPDIR:-/tmp}/build-stderr.log"        # or the run's own session dir
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ 2>"$log"; rc=$?     # capture AT the command (§6)
if [ "$rc" -ne 0 ]; then
    echo "BUILD FAILED (exit $rc) — the tool below NEVER RAN"
    grep -qE "no space left on device|ENOSPC|cannot allocate" "$log" \
        && echo "  ^ cause: ENOSPC — an ENVIRONMENT failure, NOT a code defect"
    tail -20 "$log"
    exit "$rc"
fi
./bin/gopi tool doccheck; echo "doccheck exit=$?"
```

Three rules of thumb follow, each cheap:

* **Never read `$?` as evidence about the second command of a compound chain.**
  Capture the status *at* the command that produced it (§6 shows how easy it is to
  get this wrong even while looking for it).
* **Before believing a red tool in a fresh worktree, assert the binary exists and
  the build exited 0** — `bin/gopi` is gitignored, so its presence is implied by
  nothing except a successful build.
* **Classify the failure before reporting it**: `grep -c "no space left on device"`
  on the build's stderr, and `df -h /home` **and** `df -h /` — both, because on this
  host the constraint is on the *second* one, and `/dev/shm` (the older trap,
  memory #3382/#3362) is at 56% and is not the resource in play.

## 9. Out of scope for this row — filed separately

The system-layer halves belong to **peerbox** (Rule 28), not to this client host.
Filed as follow-up rows rather than fixed here (Rule 41: the finder does not fix):

| row id | subject |
|---|---|
| `01a12ff2-a6a8-7af8-a664-c1c51dc7676c` | Bound or relocate the shared GOCACHE — the measured 119G on `/home`, and the "moving it to `/` without a bound can fill ROOT" trap |
| `01a12ff2-a6e0-7f33-bc60-722ee50ecfb8` | A red tool must not be indistinguishable from a tool that never ran — the chained-build ENOSPC misread (§5, §6) |

---

### Evidence index (all raw output is under this run's session directory — Rule 37)

| artifact | path |
|---|---|
| consumption measurement (raw, 91 lines) | `~/.gopi/sessions/01a12fbe-1fee-7e6d-b5a6-58b9298c100c/artifacts/measure-home.txt` |
| `df` / `findmnt` / inode snapshots (raw) | `…/artifacts/df-home.txt` |
| build reproduction transcript (raw, 41 lines, both exit codes) | `…/artifacts/repro-build.txt` |
| the misread red proof (raw: CASE A / CASE B) | `…/artifacts/repro-misread.txt` |
| the scripts that produced them | `…/artifacts/{measure-home.sh,repro-build.sh,repro-misread.sh}` |
| the build's stderr captured by the fixed form | `…/artifacts/build-stderr.log` |
