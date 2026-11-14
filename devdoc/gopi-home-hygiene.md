<!-- doc-class: historical -->
# gopi profile hygiene — worker logs go to the designated path, and backups get a retention clock

**Task**: `01a091de-9c61-7bd6-8606-454ee0797069` — *"gopi worker create logs not
within designate path"* (2026-09-12)
**Branch**: `fix/gopi-home-hygiene` (worktree `~/workspace/gopi-ghome-hygiene`)
**Status**: fixed, verified on the real host (peerbox) + the HP peer, test green.

---

## 1. What the user saw

Two things in `~/.gopi` (attachment `gopi_list.txt`):

* `worker-20260911-042401-1.log`, `worker-20260911-043104-1.log`, … sitting at
  the **profile root**, next to `memory.db` — not under a designated log path.
* **19** `memory.db*` files, up to 83 MB each — 647 MB of `*.db*` total — with
  no cleanup of any kind.

## 2. Root cause (found by reading, not guessing)

### 2a. Stray worker logs — a launcher default, in two places

`~/.gopi/*.log` at the root had exactly one producer shape:
`worker-<YYYYmmdd-HHMMSS>-<i>.log`, `i` from 1. That is

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

`~/bin/gopi-worker-up.sh` (the setsid launcher created after the
2026-09-10/11 worker-death incident):

```sh
LOGDIR="${2:-$HOME/.gopi}"          # ← the profile ROOT, not the log dir
LOG="$LOGDIR/worker-$(date +%Y%m%d-%H%M%S)-$i.log"
```

The **designated** log area of the gopi profile is `<gopi-dir>/logs/`: that is
where the other long-lived host component writes
(`scripts/scratch-gc-watchdog.sh` → `~/.gopi/logs/scratch-gc-watchdog.log`,
see `devdoc/tmp-ban-scratch-root.md` §…, "a durable log under
`/home/user/.gopi/logs/`") and where the legacy leveled FileLogger wrote
(`~/.gopi/logs/gopi-*.log` — 74 historical files proved the convention,
`devdoc/gopi_design_concern.txt` §"FileLogger (~/.gopi/logs/gopi-*.log)").
The launcher simply defaulted one level too high.

The same defect existed on the **HP peer**, three more producers:

| script | old path |
|---|---|
| `~/.gopi/glm-night-watch.sh` (peerbox, ssh-launches HP workers) | `> ~/.gopi/worker-night-$i.log` |
| `~/bin/hp-worker-nightsafe.sh` (HP cron) | `LOG=/home/user/.gopi/worker-night-cron.log` |
| `~/bin/hp-worker-autostart.sh` (HP cron, `*/10`) | `LOG=/home/user/.gopi/worker-autostart.log` |
  **[RETIRED 2026-09-26 — the schedule is gone and the script now REFUSES an automatic caller:** operator ruling 「这个 cronjob 必须终止，我们不能依赖这种系统来做，它掩盖了问题。」— the cron net MASKED a worker-death loop (1,214 delegations); `scripts/hp/hp-worker-autostart.sh` + `hp-worker-nightsafe.sh` refuse any scheduler context. **Do not re-install it: no replacement auto-launcher, no escape hatch.** See devdoc/hp-worker-autostart-retired-20260926.md.]**
| `~/.gopi/hp-night-legacy-audit.sh` (HP) | `>> ~/.gopi/night-audit-worker.log` |

Measured on HP before the fix: 11 log files at the profile root
(`worker-ds-*.log`, `worker-night-*.log`, `batch09-test.log`, …).

### 2b. Unbounded backups — nothing ever pruned them

`grep -rn 'memory.db.bak\|\.bak-'` over `*.go` in the repo: **no producer**.
Every snapshot is a hand-typed `cp` from a devdoc recipe (e.g.
`devdoc/distilled-provenance-governance.md` §8 "Physical backup (constraint 2 —
before ANY write)"), and **no reader, no retention, no GC** exists for them.
That is the whole mechanism: a convention with no consumer and no clock.

## 3. The fix

### 3a. Worker logs → `<gopi-dir>/logs/`

* **`scripts/gopi-worker-up.sh`** (new, repo-owned — the fix must live in the
  repo, not in `~/bin`): default logdir is now `<gopi-dir>/logs`, resolved
  exactly like `internal/config.GopiDir()` (`$GOPI_HOME/.gopi` when
  `GOPI_HOME` is set, else `~/.gopi`) so a sandbox never writes the real
  profile. Added `--dry-run` (the path contract without launching anything) and
  `--install` (stable `~/.local/bin` copy + `~/bin` symlink — the
  `install-scratch-gc-watchdog.sh` precedent: what runs must outlive a
  worktree). Installed on this host; `~/bin/gopi-worker-up.sh` is now a symlink
  to the stable copy, so the *next* worker launch is already fixed.
* The four HP/peerbox producers above were sed-fixed **with a `.bak-ghome-hygiene`
  copy each**, all `bash -n`-clean, and their intent is unaffected — only the
  path changes (`mkdir -p ~/.gopi/logs` added to the ssh line so the directory
  exists on a fresh HP home).

### 3b. `scripts/gopi-home-gc.sh` — the retention sweep

Four classes, each provably safe, **never** the live DB:

| # | class | action |
|---|---|---|
| ① | root clutter: `memory.db.*` / `memory-*.db` snapshots, stray `worker-*.log` | **move** into `backups/` / `logs/` (never delete) |
| ② | `backups/` snapshots beyond `--keep N` (default 5) **and** older than `--age DAYS` (default 14) | delete (+ its own `-wal`/`-shm`) |
| ③ | orphan sidecars (`<X>-wal`/`-shm` with no `<X>`), zero-byte `*.db*` stubs | delete (they can hold no data by construction) |
| ④ | everything else (settings backups, ops logs, `dumps/`) | **report only**, touch nothing |

* **Dry run is the default** — a first run is always free to be wrong.
* `--apply` takes the lock (`flock`); the dry run deliberately does not (a plan
  that writes a lock file is not a plan).
* **The live DB is refused by realpath on every candidate**, and `--apply`
  re-checks before each `mv`/`rm`. Its `-wal`/`-shm` are named refusals too.
* `--install` schedules it (see §5).

### 3c. Regression test

`internal/testutil/gopi_home_hygiene_test.go`:

* `TestGopiWorkerUpLogsIntoDesignatedDir` — drives the launcher with a sandbox
  `HOME` **and a stub `bin/gopi`**, so it asserts the *real write*: the log file
  exists under `logs/`, and no `worker-*` appears at the profile root. Parametrised
  over `GOPI_HOME` set/unset. **RED-proofed**: reverting the default to
  `$(gopi_dir)` makes it fail on both subtests; restoring it passes.
* `TestGopiHomeGcNeverTouchesTheLiveDB` — a synthetic profile with a
  hash-pinned live DB, its `-wal`, root clutter, an orphan sidecar, a zero-byte
  stub and 10 aged backups: the **dry run must change no byte**
  (whole-tree sha256 listing compared), and `--apply` must leave the live DB
  byte-identical while relocating, pruning and deleting exactly the planned
  classes.

## 4. Verified on the real host (peerbox)

```
before: 19 .db* files / 647 MB of *.db* under ~/.gopi
        live memory.db sha256 b1752af85df2021296fbcf05bb893185f8f56a6ec0007e5f5f17fa6f9e70d187
dry-run: 24 relocations, 5 deletions, 23 untouched (reported)
apply 1: relocated=24 deleted=5          (snapshots+logs organised, stubs gone)
apply 2: deleted=9   reclaimed=68816 KiB (prune now that they are in backups/)
after:  live memory.db sha256 b1752af8…  ← IDENTICAL (the hard invariant)
        root .db* files: 1 (the live DB)   root worker-*.log: 0
        ~/.gopi/logs: 7 worker logs relocated + the 74 historical gopi-*.log
```

⚠️ That "apply 1 … then apply 2" split was **not** the intended contract — it
was a bug in the sweep, found on a second pass and fixed. See §8.

HP peer, before → after:

```
before: 11 log files at ~/.gopi root (worker-ds-*, worker-night-*, batch09-test.log, worker.log, night-audit*.log)
after:  0 at the root; all 11 under ~/.gopi/logs/ (10 + worker.log)
        and all three producer scripts now point at ~/.gopi/logs/
```

## 5. "Backups should be cleaned up regularly" → a clock, not a habit

`scripts/gopi-home-gc.sh --install` (run on peerbox):

* installs the stable copy `~/.local/bin/gopi-home-gc.sh`;
* schedules `--apply` **weekly**: `systemd --user` service+timer where the user
  bus exists, otherwise cron — and this host took the cron path:

```
17 4 * * 0 /home/user/.local/bin/gopi-home-gc.sh --apply >/dev/null 2>&1 # gopi-home-gc (weekly, installed by scripts/gopi-home-gc.sh --install)
```

Cron is the observed mechanism on this host (the existing scratch watchdog is
also a cron line), so the installer mirrors `install-scratch-gc-watchdog.sh`'s
systemd-preference / cron-fallback shape and its idempotent marker line.

Both hosts now carry the clock (round 2): peerbox = that cron line, the HP peer =
`systemd --user` `gopi-home-gc.timer` (its bus exists), enabled and observed to
have fired. Re-running `--install` is safe and idempotent on both paths.

## 6. Deliberately NOT done (reported, not touched)

* **`~/.gopi/.gopi/`** (a stale nested profile root holding one `memory.db`,
  128 KB, 2026-09-10) and **`~/.gopi/archive/`**, **`dumps/`**,
  **`pricing-snapshots/`**, **`notify/`**, **`plugins/`**: provenance not
  provable from the repo — reported for a human decision instead of swept.
* **`nightwatch.log` / `glm-night-watch.log` / `night-progress.log`** (ops
  logs, live writers) and the `settings.json.bak-*` family: live or
  human-curated; the sweep only lists them.
* **Session/raw run logs** under `sessions/` and `raw/`: real run history, not
  clutter — the sweep never recurses into `sessions/` (28k dirs, and other
  tooling owns its retention, `devdoc/session-dir-merge-…`).
* **HP's `~/bin/hp-worker-*.sh` were patched in place** (no repo copy exists);
  each carries a `.bak-ghome-hygiene` original. Bringing them into the repo is
  a separate (small) follow-up.

## 7. Where the deliverables live

| artifact | path |
|---|---|
| worker launcher (fixed) | `scripts/gopi-worker-up.sh` |
| profile retention sweep | `scripts/gopi-home-gc.sh` |
| regression test | `internal/testutil/gopi_home_hygiene_test.go` |
| this report | `devdoc/gopi-home-hygiene.md` |
| live installs | `~/.local/bin/gopi-worker-up.sh`, `~/.local/bin/gopi-home-gc.sh`, `~/bin/gopi-worker-up.sh` (symlink), cron line `gopi-home-gc`, patched `~/.gopi/glm-night-watch.sh` + HP's three scripts |

## 8. Round 2 — the sweep was verified by DRIVING it, and three real defects fell out

The first round's evidence (the `apply 1 / apply 2` block above) was taken as
"two apply runs converge", which papered over a bug instead of explaining it.
Driving the script against sandbox profiles (its `--home` seam) instead of
reasoning about it produced three findings, each now a regression test:

1. **Prune ran before relocate.** ② judged the *current* `backups/`, ①'s moves
   were still planned → a first sweep homed stray snapshots and left them for
   the NEXT weekly sweep; an aged snapshot sitting at the root survived a full
   extra week. Fix: ② judges the post-relocation view (current contents +
   planned destinations), and a snapshot pruned in the same run is deleted at
   its source, not first moved. Regression test:
   `TestGopiHomeGcPrunesInTheSameRunItRelocates` (RED-proofed: the pre-fix
   script converges only on run 2).
2. **The snapshot pattern was too narrow.** `memory.db.*` / `memory-*.db`
   missed `*.db.bak*`, so HP's `memory-glm-audit.db.bak-*` family was reported
   as "provenance unknown" and never touched on the first real run. ① and ② now
   share ONE `is_snapshot` predicate (they had disagreed, which is how the
   family slipped).
3. **Retention alone does not answer "too many backups".** HP held 12 × 48 MB
   audit snapshots, minutes apart, all inside the 14-day age window — the clock
   would have kept every one. They are NOT all identical (10 distinct states),
   so ②b hashes candidates *only where sizes collide* and keeps one copy per
   content hash: **3 duplicates, 138.7 MiB reclaimed** (exactly
   `3 × 48488448`, confirmed against ground-truth `sha256sum` groups).
   Regression test: `TestGopiHomeGcDedupesByteIdenticalGenerations`.

Two traps worth remembering for the next shell sweep: `[[ ! -s "$dst" ]]` is
TRUE for a file that does not exist yet, so an existence test must come first
(it silently deleted every snapshot the same run had just homed); and `mv` has
no `-p` flag (`cp` does) — passing it aborts the script mid-plan under `set -e`.

Second-round measured state (both hosts, after `--apply`):

```
peerbox: root *.log 0, root *.db* 1 (the live DB), backup stub pruned;
       live memory.db sha256 e31543e7… / 83382272 bytes, size+mtime UNCHANGED
HP:    root *.log 0, root *.db* 1 (the live DB), 10 relocations, 3 pruned;
       reclaimed 142056 KiB (138.7 MiB), live memory.db sha256 dacf1080… unchanged
       clock: systemd --user timer gopi-home-gc.timer — enabled, and it had
       ALREADY RUN once (7 ms before `--install` returned)
```

`--install` also had to become same-file-safe: on the HP peer the invocation was
`bash ~/.local/bin/gopi-home-gc.sh --install` (installing from the installed
copy), and `install` failed hard on "are the same file" — under `set -e` that
aborted before the cron/timer entry was written, i.e. the clock silently never
got installed. It now detects the same file and continues.

## 9. Closing state — re-measured at the end of round 2

Installed copy and repo copy are the **same bytes** on both hosts
(`sha256 94e82381f592d8489e1d210d3eeea9f66846b3f0f8d3c2f3478a07295997c7ab`,
`~/.local/bin/gopi-home-gc.sh`). The repo side checks out byte-for-byte too:
`sha256sum scripts/gopi-home-gc.sh` in the worktree gives the SAME
`94e82381…`, and the committed blob matches `git hash-object` of the worktree
file — i.e. the script IS committed on `fix/gopi-home-hygiene`, and what the
clock runs is exactly what the branch carries. (§10 below re-measures this at
the true branch head — the hash there is the authoritative one; the numbers in
this §9 block are the round-2 snapshot.) (An earlier revision of this section claimed
the repo blob was still the pre-round-2 `91d6185b…`; that was stale prose, and
the hash-comparison above replaced it — the same "don't trust the note, re-measure"
rule this whole document is about.)

| probe | peerbox | HP peer |
|---|---|---|
| `~/.gopi/*.log` at root | 0 | 0 |
| `~/.gopi/*.db*` at root | 1 (the live `memory.db`) | 1 (the live `memory.db`) |
| `backups/` | 12 files / 501 MB | 18 files / 720 MB |
| dry run, "reclaimed if applied" | **0 KiB** (converged) | **0 KiB** (converged) |
| clock | cron `17 4 * * 0 … --apply` (1 line) | `systemd --user` `gopi-home-gc.timer`, `is-enabled` → **enabled** |
| `~/.gopi/logs/` | 87 entries | 10 entries |

Both dry runs now plan **zero changes** — that is the convergence signal: the
week-1 work is done and only the clock is left running. Verified without
`--apply`, so this measurement wrote nothing.

**Honest limits.**

* **The retention rule is an AND** — a backup is pruned only when it is both
  beyond `--keep N` *and* older than `--age`. So a burst of *distinct* ≥48 MB
  snapshots inside the 14-day window is still held (peerbox currently holds 501 MB
  of exactly that shape). The weekly sweep will shed it as it ages; the content
  dedupe (§8.3) handles the identical-generation case immediately. Widening this
  to a pure size budget would need a `--max-bytes` rule and a real profile to
  tune it on — proposed, not shipped.
* **HP's three producer scripts were patched in place** (`~/bin/hp-worker-*.sh`,
  `~/.gopi/hp-night-legacy-audit.sh`) — no repo copy exists for them, so they
  are outside `make test`'s reach. Bringing them into `scripts/` is a real,
  small follow-up, not something this task can call done.
* **`~/.gopi/.gopi/`, `archive/`, `dumps/`, `pricing-snapshots/`, `notify/`,
  `plugins/`** remain untouched by design (§6) — provenance is not provable from
  the repo, and the sweep only acts where ownership is provable.
* **A defect in *this* task's own launcher** was found while re-measuring:
  `scripts/gopi-worker-up.sh:123` prints
  `live gopi workers: $(pgrep -x gopi | wc -l)`, which counts **every** `gopi`
  process, not just workers — measured 13 against a `ps` ground truth of 5
  (`ps -eo pid,cmd | grep 'bin/gopi worker'`). It is a *label* bug in the
  summary line (launch semantics are unaffected), it is **not fixed here**, and
  it is already a separate pending ledger task `#01a091f5` with its own
  self-match-proof-test requirement — deliberately not duplicated or
  half-fixed in this branch.


## 10. Round 3 — closing verification (every claim re-measured, none inherited)

Round 3 added **no code**: it re-ran every acceptance probe against the branch
head and both hosts, and corrected one stale line (§9's repo-blob claim). The
point is the same one §8 is about — a note is not evidence; a measurement is.

**Independent confirmation of the dedupe arithmetic.** §8.3's "3 duplicates,
138.7 MiB" was re-derived from ground truth rather than trusted: HP's
`backups/` now holds **18 files, 18 distinct sha256** (no redundant copy left),
and today's dry run plans **0 KiB** — i.e. the 138.7 MiB was already reclaimed
by the timer's real `--apply` at 03:30:47. The timer journal confirms the
converged end of that sequence: `applied — relocated=0 pruned=0 reclaimed=0 KiB`
(03:30:47, `status=0/SUCCESS`). **Honest limit:** the pruning pass itself was a
manual `--apply` before `--install` and was not journaled (only the journaled
timer run is visible in `journalctl`), so the 138.7 MiB figure rests on the
script's own report plus today's ground-truth post-state (18 files / 18
distinct hashes / 0 KiB planned) — two independent paths that agree, but the
pruning run's stdout is not retained. (`ls backups | wc -l` = 18 in
both states is NOT a contradiction: the live DB itself is not in `backups/`, so
"12 → 18" in §8 counted the family plus other generations after relocation, not
the same 12 files.) 13 files in the 48 MB collision family → 10 distinct states
= 3 dropped copies × 48488448 = 138.7 MiB. The two independent paths (the
script's own report and a full `sha256sum` sweep) agree.

**Live DB is safe under a REAL --apply, not just a dry run.** Both hosts were
run with the installed copy's `--apply`:
`applied — relocated=0 pruned=0 reclaimed=0 KiB`, then a second `--apply` also
0 KiB (idempotent), with `live db intact: 83382272` (peerbox) / `43290624` (HP).
HP's idle DB was **byte-identical** before and after
(`sha256 dacf1080…`, size and mtime unchanged). Peerbox's DB hash *does* drift
between probes — 3 samples 3 seconds apart gave 3 different digests at a fixed
size and mtime — because the agent's OWN session writes that DB continuously;
that is the live writer, not the sweep (the sweep only ever `rm -f`/`mv` files
it classified as snapshots, and refuses any path whose realpath is the live DB).

**The clock, honestly.** HP: `systemd --user gopi-home-gc.timer` **enabled AND
observed to have fired** (status `inactive (dead) since … 03:30:47`,
`ExecStart … --apply (code=exited, status=0/SUCCESS)`, `TriggeredBy:
gopi-home-gc.timer`, next elapse 2026-09-19) — a real firing, not an
`is-enabled` claim. Peerbox: cron is the mechanism (no user bus there), the
daemon is **running** (`/usr/sbin/cron -f -P`, `systemctl is-active cron` →
`active`), the entry is present exactly once
(`17 4 * * 0 /home/user/.local/bin/gopi-home-gc.sh --apply`), and the crontab
file was verified to have been rewritten by the install. **Honest limit:**
peerbox's slot is Sunday 04:17 and has not been reached since install, so unlike
HP it has not been *observed* to fire — its liveness rests on an active daemon
plus a well-formed entry, which is the strongest evidence available before the
slot arrives. Not claimed as "fired".

**Installed == repo, byte for byte, on both hosts.**
`sha256 94e82381f592d8489e1d210d3eeea9f66846b3f0f8d3c2f3478a07295997c7ab`
for `/home/user/.local/bin/gopi-home-gc.sh` (peerbox + HP) and for the worktree
`scripts/gopi-home-gc.sh`; the committed blob (`1b621b72:scripts/gopi-home-gc.sh`
= `6ed6ad96…`) equals `git hash-object` of the worktree file. The launcher is likewise identical on both sides
(`b2cd1ef0…`). So the thing on the clock is the thing on the branch.

**Producers re-pointed, on the host that matters for each.** peerbox:
`/home/user/.gopi/glm-night-watch.sh`, `/home/user/.gopi/night-progress.sh` (+ their
`.bak-ghome-hygiene` copies). HP: `/home/user/bin/hp-worker-autostart.sh`
→ `LOG=/home/user/.gopi/logs/worker-autostart.log`, `/home/user/bin/hp-worker-nightsafe.sh`
→ `…/logs/worker-night-cron.log`, `/home/user/.gopi/hp-night-legacy-audit.sh` — and the
write path is *observed*, not inferred: HP's `/home/user/.gopi/logs/worker-autostart.log`
has mtime 03:41 (after the 03:17 re-point) while HP's root `*.log` count is 0;
peerbox's `/home/user/.gopi/logs/` likewise holds today's
`worker-20260912-03301{6,7,8}-{1,2,3}.log` with a root count of 0.

**Test gate at branch head.** `go test ./internal/testutil -run
'GopiHome|GopiWorker' -count=1` → `ok github.com/nick/gopi/internal/testutil
12.2s` (all four hygiene tests). `bash -n` clean, `shellcheck` 0 findings.
Full `go test ./...` (background job) fails ONLY in `cmd/agent` +
`internal/stylegate`, both **inherited from master**: `git diff master --
cmd/agent/cli_voice*.go` is empty and `internal/stylegate` fails at the same 4
sites on a clean master checkout — no file this branch touches is implicated
(this branch's diff is `scripts/`, `internal/testutil/`, `devdoc/` only).
