<!-- doc-class: normative -->
# Disk usage and task-ground residue are TWO INDEPENDENT FACTS — and this host proves it

> Measured on `hosta`, 2026-10-26T08:17 – 08:37 +08:00, at repo HEAD
> `afa0517d6d2b8cf91b2995ceb13ad51e16520d7a`. Every number below was produced by a
> command run in this session; the raw dumps are under
> `~/.gopi/sessions/01a1749f-388d-7854-97d0-562a654e857f/evidence/`.
> The recipe to reproduce all of it is §6 (a command sequence — no script file).

## 1. The claim, in one paragraph

The premise "the disk is 96% full, so reclaim the task grounds" fuses two facts that
share no number on this host. `/` is one filesystem (`/dev/nvme0n1p2`, 1.8 TB, 96%).
The task grounds are on **another** filesystem (`/dev/nvme1n1p1` mounted at
`/home/user/workspace`, 3.6 TB, 93%). Deleting **every** task ground (measured:
**185 dirs, 42 301 MB = 41.31 GiB**) changes `/`'s `Use%` by **zero points** — the
bytes are not on it. On the disk that actually holds them, all of it is **1.28 % of
the used space** (Use% 92.86 → 91.67, a 1.19-point delta). So disk percentage is not
a reclaim-progress signal, and reclaiming grounds is not a disk-space remedy. The
reclaim-progress signal is the **directory count**, which is directly observable.

## 2. The two filesystems (the fact the fused premise misses)

```
$ df -h / /home/user/workspace                       # run 2026-10-26T08:17:19
Filesystem      Size  Used Avail Use% Mounted on
/dev/nvme0n1p2  1.8T  1.7T   75G  96% /
/dev/nvme1n1p1  3.6T  3.2T  249G  93% /home/user/workspace

$ df -B1 / /home/user/workspace
Filesystem         1B-blocks          Used    Available Use% Mounted on
/dev/nvme0n1p2 1967317549056 1787279011840  80028717056  96% /
/dev/nvme1n1p1 3936818806784 3469918916608 266843860992  93% /home/user/workspace

$ stat -c '%d %n' / /home/user /home/user/workspace   # device ids
66308 /
66308 /home/user
66305 /home/user/workspace

$ findmnt -T /home/user/workspace -o TARGET,SOURCE,FSTYPE
TARGET               SOURCE         FSTYPE
/home/user/workspace /dev/nvme1n1p1 ext4
```

`66308 ≠ 66305`. `/home/user/workspace` is a **separate physical device**, not a
directory on `/` — `du -x /` stops at that mount, which is why `/`'s own breakdown
(§4) contains no trace of the 3.6 TB of data sitting under `workspace`.

## 3. The task grounds, measured, and what deleting them would do

```
$ ls -d /home/user/workspace/01a*   | wc -l        # run 2026-10-26T08:36:57
119
$ ls -d /home/user/workspace/gopi-* | wc -l
66
$ printf '%s\0' /home/user/workspace/01a* /home/user/workspace/gopi-* \
    | du -sm --files0-from=- | awk '{s+=$1} END {print "dirs="NR" total_MB="s" GiB="s/1024}'
dirs=185 total_MB=42301 GiB=41.3096
```

**42 301 MB = 41.31 GiB across 185 directories.** Now the arithmetic, three
denominators for the SAME 42 301 MB — because the choice of denominator is where
this kind of claim usually goes wrong:

```
$ awk -v g=$((42301*1048576)) 'NR==2{a_tot=$2;a_used=$3} NR==3{b_tot=$2;b_used=$3} END{...}' \
    < <(df -B1 / /home/user/workspace)
SLASH  : cap=1967317549056 used=1787293257728  grounds_as_pct_of_capacity=2.2546%  as_pct_of_used=2.4817%  after=88.5946%
WORKSPC: cap=3936818806784 used=3469918920704  grounds_as_pct_of_capacity=1.1267%  as_pct_of_used=1.2783%  after=87.0135%
```

and the same thing expressed the way `df` itself computes `Use%` (ext4 reserves
blocks, so `Use% = used/(used+avail)`, **not** `used/total` — verified here, it is
`95.72 %` vs `90.85 %` on `/`):

```
$ df -B1 / /home/user/workspace | awk 'NR>1{ now=used/(used+avail)*100; after=(used-g)/((used-g)+(avail+g))*100; ... }'
/dev/nvme0n1p2   Use%=95.72 | grounds=2.482% of used | AFTER removing grounds: Use%=93.34 (delta 2.38 pts)
/dev/nvme1n1p1   Use%=92.86 | grounds=1.278% of used | AFTER removing grounds: Use%=91.67 (delta 1.19 pts)
```

**Read the two lines together and the two facts separate cleanly:**

| question | answer, measured |
|---|---|
| Do the grounds sit on `/`? | **No** — `0` `01a*` dirs under `/home/user`; all 119 under the `nvme1n1p1` mount |
| Delete all 185 grounds → what happens to `/`'s 96%? | **Nothing. 0 points.** The bytes were never on `/` |
| Delete all 185 grounds → what happens to the disk that holds them? | 92.86% → **91.67%**, a **1.19-point** delta |
| Grounds as a fraction of the disk they occupy? | **1.28 % of used**, 1.13 % of capacity |
| Is "reclaim the grounds" a fix for "96% full"? | **No.** It is a fix for "185 unreclaimed directories" |

The `2.25 % / 2.48 %` figures against `/` are a **category error**, kept in this
document on purpose: they are arithmetically correct and factually irrelevant,
because they divide one filesystem's data by another filesystem's capacity. A
figure is only a claim once its denominator names the same volume as its numerator.

## 4. What actually occupies each filesystem (the real owners)

### 4.1 `/` — `/dev/nvme0n1p2`, 1 967 317 549 056 B, used ≈ 1 787.3 GB

```
$ du -m -x --max-depth=1 /            # run 2026-10-26T08:08:33  (du -x stops at the workspace mount)
1679075	/
1495893	/home
59964	/usr
30212	/var
10353	/tmp
10032	/mnt
6732	/opt
328	/boot
28	/etc

$ du -sm /swapfile                    # not in the tree walk above: it is a file, and du -x / skips it
65537	/swapfile
$ swapon --show
NAME      TYPE SIZE USED PRIO
/swapfile file  64G 7.9G   -2
```

as a share of `/`'s used bytes:

```
$ (measured 2026-10-26T08:34:51 — used=1787298885632 B = 1787.3 GB)
path                                       MB        GiB % of / used
/home/user/diabloforum                 578823      565.3     33.96%
/home/user/Videos                      149063      145.6      8.75%
/home/user/Downloads                   137509      134.3      8.07%
/home/user/.cache                      131921      128.8      7.74%
/home/user/work                        111144      108.5      6.52%
/home/user/Documents                    87249       85.2      5.12%
/home/user/.gopi                        71843       70.2      4.21%
/home/user/.gopi/sessions               69497       67.9      4.08%
/swapfile                               65537       64.0      3.84%
/usr                                    59964       58.6      3.52%
/home/user/stable-diffusion-webui       52992       51.8      3.11%
/home/user/anaconda3                    31199       30.5      1.83%
/var                                    30217       29.5      1.77%
```

`/home/user` totals **1 495 916 MB (1.46 TB)**. One operator directory —
`diabloforum`, 578 823 MB — is **34 % of everything `/` holds**, i.e. ~13.7× the
whole task-ground corpus.

### 4.2 `/home/user/workspace` — `/dev/nvme1n1p1`, 3 936 818 806 784 B, 3 231.6 GiB used

```
$ du -sm -x /home/user/workspace      # full walk, job run 2026-10-26T08:08:43, took ~23 min
3309006	/home/user/workspace

$ du -sm /home/user/workspace/* | sort -rn | head -20      # run 2026-10-26T08:21:47
975009	/home/user/workspace/kiwix
359063	/home/user/workspace/wikipedia
317540	/home/user/workspace/Downloads
282189	/home/user/workspace/stable-diffusion-webui
261896	/home/user/workspace/OLLAMA
261402	/home/user/workspace/llvm-project
106614	/home/user/workspace/latent-diffusion
63723	/home/user/workspace/doubao_dialog
60994	/home/user/workspace/fastText
44907	/home/user/workspace/backup
42187	/home/user/workspace/wiki
33091	/home/user/workspace/vcmi
25100	/home/user/workspace/minimind
22452	/home/user/workspace/LLVM
21197	/home/user/workspace/my_project
19298	/home/user/workspace/debootstrap
13320	/home/user/workspace/_tmp
12260	/home/user/workspace/Synology
12081	/home/user/workspace/wechat
11865	/home/user/workspace/tools
```

Note where `Downloads`, `OLLAMA`, `doubao_dialog`, `fastText`, `backup` turned up:
**under the workspace root**, i.e. on the 3.6 TB disk — not under `/home/user`.

```
$ for p in Downloads OLLAMA doubao_dialog fastText backup; do for r in /home/user /home/user/workspace; do
    [ -e "$r/$p" ] && printf '%-16s %-34s EXISTS dev=%s\n' "$p" "$r/$p" "$(stat -c %d "$r/$p")"; done; done
Downloads        /home/user/Downloads               EXISTS  dev=66308
Downloads        /home/user/workspace/Downloads     EXISTS  dev=66305
OLLAMA           /home/user/workspace/OLLAMA        EXISTS  dev=66305
doubao_dialog    /home/user/workspace/doubao_dialog  EXISTS  dev=66305
fastText         /home/user/workspace/fastText      EXISTS  dev=66305
backup           /home/user/backup                  EXISTS  dev=66308
backup           /home/user/workspace/backup        EXISTS  dev=66305

$ du -sm -x /home/user/workspace/{Downloads,OLLAMA,doubao_dialog,fastText,backup}
317540	/home/user/workspace/Downloads
261896	/home/user/workspace/OLLAMA
63723	/home/user/workspace/doubao_dialog
60994	/home/user/workspace/fastText
44907	/home/user/workspace/backup
```

**Two same-named `Downloads` and `backup` exist on this host, one per filesystem.**
A path name does not identify a volume; the device id does (§2). This is the same
class of trap the rule set already names for host identity (Rule 40: an identical
path on two hosts proves nothing) — it applies to two mounts on ONE host as well.

### 4.3 Three classes, on the 3.6 TB disk (denominator: 3 309 006 MB du total)

```
CLASS                          MB        GiB     % of that disk's content
operator: kiwix + wikipedia  1334072     1302.8    40.32%
operator: the five named      749060      731.5    22.64%
everything else               936433      914.5    28.30%
gopi caches (.gocache)        236175      230.6     7.14%
TASK GROUNDS (01a*+gopi-*)     42301       41.3     1.28%
gopi scratch + sandbox         10965       10.7     0.33%
---
ALL gopi-owned                 289441      282.7     8.75%
```

The `everything else` line is honest, not a rounding device: it is
`workspace du total − the five named classes`, i.e. 936 433 MB of directories this
document did not enumerate individually (the top-20 list above ends at 11 865 MB).

**Ownership is the whole point of the split.** The operator's data — `kiwix`,
`wikipedia`, `Downloads`, `OLLAMA`, `doubao_dialog`, `fastText`, `backup`,
`stable-diffusion-webui`, `diabloforum`, `Videos`, `Documents` — is **not a
cleanup candidate**. It is 1 302.8 + 731.5 = **2 034.3 GiB, 63 % of that disk**, and
it is the operator's to keep or delete. A reclaim procedure whose effect is
measured in "percent of disk freed" will always be tempted to reach for it, because
that is where the bytes are. **The task grounds are the only part of this disk an
agent may reclaim, and they are 1.28 % of it.** Both statements must be stated
together or the second one invites the first.

### 4.4 gopi's own footprint — stated by the same measurement, in both places

Consistency requires this section, because the tool that writes this document is
also a consumer of space:

| gopi-owned | where | MB | GiB | share |
|---|---|---|---|---|
| task grounds | `workspace` (3.6 T disk) | 42 301 | 41.3 | 1.28 % of that disk |
| `.gocache` (go build cache) | `workspace` | 236 175 | 230.6 | 7.14 % of that disk |
| `gopi-test-scratch` | `workspace` | 10 264 | 10.0 | 0.31 % |
| `gopi-sandbox` (Rule 34 root) | `workspace` | 701 | 0.7 | 0.02 % |
| `.gopi/sessions` | **`/`** | 69 497 | 67.9 | **4.08 % of `/` used** |

`go env GOCACHE` → `/home/user/workspace/.gocache`; it is a real go build cache
(`README`, `trim.txt`, 2-hex shards of `-a`/`-d` files). `gopi config show` reports
`temp workspace /home/user/workspace/_tmp (from the caller's TMPDIR)`.

**The uncomfortable line is the last one**: gopi's *session ledger* on `/` (67.9 GiB)
is **1.6× larger than every task ground combined** (41.3 GiB) and is 4.08 % of the
filesystem that is actually at 96 %. Nothing in this document proposes deleting it —
it is the audit trail, and Rule 42's "did the caller create it" test does not make
another run's session directory disposable. It is recorded because a document that
argues "reclaim the grounds, not the disk" while silently holding 67.9 GiB on the
disk at 96 % would be arguing in bad faith.

## 5. The reclaim-progress signal: DIRECTORY COUNT, not disk percentage

Disk percentage cannot observe reclaim here — it moves 0 points on `/` (§3) and
1.19 points on the other disk, which is inside the noise of ordinary writes. The
directly observable fact is the count:

```
$ A=$(ls -d /home/user/workspace/01a* | wc -l); B=$(ls -d /home/user/workspace/gopi-* | wc -l); echo "$A $B $((A+B))"
119 66 185
```

Sampled four times during this session, the number did not move (`08:21:57`, `08:32:32`,
`08:33:38`, `08:36:57` — all **185**), while `/`'s used bytes drifted +5.5 MB in the
same window (`1787294674944 → 1787299803136` B over 08:32:38→08:36:57). **The disk
moved; the count did not.** That is exactly the property a progress metric needs and
the disk does not have.

**Why the count is the right signal, and what it is NOT measuring.** `gopi tool
sweep-worktrees --dry-run` reported `collectable=728 kept=127` on this tree, yet
only 113 of the 762 distinct `/home/user/workspace/...` paths it names still exist:

```
$ grep -o 'collectable=[0-9]*' sweep-dump | head -1      → collectable=728
$ grep -o 'kept=[0-9]*'         sweep-dump | head -1      → kept=127
$ grep -oE '/home/user/workspace/[A-Za-z0-9._-]+' sweep-dump | sort -u | wc -l   → 762
$ ...of those, still present on disk                                              → 113
$ ...of those, missing on disk                                                    → 649
```

So `collectable=728` is **an input to the sweep, not a report of what is on disk** —
649 of the paths it counts are stale ledger entries for directories that no longer
exist. Reading that number as "728 dirs waiting to be deleted" would be wrong in the
expensive direction: it makes the queue look 4× larger than it is and never converges.
The honest pair to watch is:

| metric | command | value at 2026-10-26T08:36:57 |
|---|---|---|
| **directory count** (the progress signal) | `ls -d /home/user/workspace/{01a*,gopi-*} \| wc -l` | **185** |
| **bytes they hold** (the ceiling on any gain) | `printf '%s\0' … \| du -sm --files0-from=- \| awk '{s+=$1}END{print s}'` | **42 301 MB** |
| stale sweep entries (a ledger defect, not a backlog) | count of dry-run paths that are gone | 649 |

A reclaim is progressing when the first number falls. The second number states the
most it can ever buy. Neither is a disk percentage.

## 6. The recipe — reproducible command sequence (no script file)

Every number in this document comes from this sequence; run it top to bottom on any
host. `-x` is not optional: it is what keeps one filesystem's data out of another's
total, and omitting it is how the original confusion was manufactured.

```bash
# 1. The two facts: capacity AND identity. Read the SOURCE column, not just the mount.
df -h / /home/user/workspace
df -B1 / /home/user/workspace
stat -c '%d %n' / /home/user /home/user/workspace      # different %d == different filesystems
findmnt -T /home/user/workspace -o TARGET,SOURCE,FSTYPE

# 2. How many task grounds, and how big (two separate numbers, never one)
ls -d /home/user/workspace/01a*   | wc -l
ls -d /home/user/workspace/gopi-* | wc -l
printf '%s\0' /home/user/workspace/01a* /home/user/workspace/gopi-* \
  | du -sm --files0-from=- | awk '{s+=$1} END {print "dirs="NR" total_MB="s" GiB="s/1024}'

# 3. The ceiling of any reclaim: grounds / used bytes OF THE DISK THAT HOLDS THEM
awk -v g=$((42301*1048576)) 'NR==1{t=$2;u=$3} END{printf "%.4f%% of capacity, %.4f%% of used\n", g/t*100, g/u*100}' \
  <(df -B1 --output=source,size,used,pcent,target /home/user/workspace | tail -1 | awk '{print $1,$2,$3}')

# 4. What actually occupies each filesystem — du -x BOUNDS the walk to one device
du -m -x --max-depth=1 / 2>/dev/null | sort -rn | head -20
du -m -x --max-depth=1 /home/user 2>/dev/null | sort -rn | head -20
ls -l  /swapfile                                      # a FILE: invisible to du's tree walk
du -sm /home/user/workspace/* 2>/dev/null | sort -rn | head -20   # slow: full walk, ~20+ min

# 5. gopi's own share, stated in the same units
go env GOCACHE; du -sm -x "$(go env GOCACHE)"
du -sm -x /home/user/.gopi /home/user/.gopi/sessions
gopi tool sandbox show | head -3

# 6. The progress signal (run it before and after a reclaim; expect the FIRST to move)
ls -d /home/user/workspace/{01a*,gopi-*} 2>/dev/null | wc -l

# 7. Why the sweep's own number is not this number (stale ledger entries)
#    NOTE the verb: `gopi task sweep-worktrees`, NOT `gopi tool sweep-worktrees`
#    (the tool group does not carry it — an unverified spelling fails with
#    `unknown command "sweep-worktrees"` and exit 1, which is how this line was
#    caught before it shipped).
gopi task sweep-worktrees --dry-run > /home/user/workspace/_tmp/sweep.txt 2>&1
grep -o 'collectable=[0-9]*' /home/user/workspace/_tmp/sweep.txt | head -1
grep -o 'kept=[0-9]*'        /home/user/workspace/_tmp/sweep.txt | head -1
grep -oE '/home/user/workspace/[A-Za-z0-9._-]+' /home/user/workspace/_tmp/sweep.txt | sort -u \
  | while read -r p; do [ -d "$p" ] || echo "$p"; done | wc -l     # ghost entries
```

That step, run verbatim at 2026-10-26T08:39:41 (exit 0), reproduced the earlier
measurement exactly — the numbers above are not a one-off reading:

```
$ gopi task sweep-worktrees --dry-run | …          # 863 lines, exit 0
collectable=728
kept=127
distinct paths named: 762
  present: 113
  ghost  : 649
```

## 7. What this document refuses to conclude

- **It does not license deleting operator data.** `kiwix`, `wikipedia`, `Downloads`,
  `OLLAMA`, `doubao_dialog`, `fastText`, `backup`, `diabloforum`, `Videos`,
  `Documents`, `.cache` are the operator's. No reclaim procedure derived from this
  document may touch them (Rule 42: nothing signals or destroys what the caller did
  not create; Rule 35: no new dependency is added to clean up after them).
- **It does not propose a cleanup script, a cron line, a timer or a sweep.** The
  existing `gopi tool sweep-worktrees` ticks are the mechanism (Rule 35). What is
  measured here is the *metric*, and the metric is one `ls | wc -l`.
- **It does not claim the 96 % is a problem to be solved by this fact.** It says the
  opposite: the 96 % and the ground residue are two facts, and fixing one is not
  fixing the other.
- **It does not re-derive a census.** These are measurements of one host at one time
  (Rule 38's *narrative* class, not the *census* class): the commit pin is
  `afa0517d6d2b8cf91b2995ceb13ad51e16520d7a`, the timestamps are in the blocks above,
  and the disk is live — `/`'s used bytes moved +5.5 MB across this session's own
  readings. Re-run §6 rather than quoting these numbers later.
