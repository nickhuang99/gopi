<!-- doc-class: historical -->
# Task Report: the `tmpdir` settings VALUE — configured, measured, gated, and the residue accounted for

Epic: *settings.json carries NO `tmpdir` key, so the injection mechanism merged
for task 01a0987d (commit `ec4c860a`, present in master) is inert.*
Branch `feat/tmpdir-settings-value` (worktree `~/workspace/gopi-tmpdir-cfg`,
from master `eb802d64`). Rule 22 is the governing rule; Rule 25 (unprivileged)
and Rule 6 (never the main repo) bound the work.

## Completed

### 1. The configured value, with its device-id measurement

`~/.gopi/settings.json` (a symlink to `.ds_flash_settings.json`) gained exactly
one key (one occurrence, JSON re-parsed after the write, 13 top-level keys):

```json
{"tmpdir": "/dev/shm/gopi-test-scratch/tmp"}
```

**Measured, not spelled** (both identifiers reported, because they are different
kernel numbers for the same question — `stat` reports `st_dev`, the code path
reports `statfs` `Fsid`; the *verdict* is identical either way):

| path (resolved) | `stat -c %d` (st_dev) | `statfs` Fsid | same FS as `/`? |
| --- | --- | --- | --- |
| `/` | 2051 | `16146614864190929547` | — |
| `/tmp` | **2051** | `16146614864190929547` | **YES — the defect** |
| `/dev/shm/gopi-test-scratch/tmp` (= tmpdir) | **28** | `5229408204322450386` | **NO** |
| `/home`, `/home/user/.gopi` | 2052 | — | no |

The resolved target is `/dev/shm/gopi-test-scratch/tmp` (the scratch home is a
symlink onto the memory filesystem on this unprivileged host), i.e. inside the
tree the age GC already bounds — an un-GC-able `/dev/shm/<newdir>` would have
traded reservoir A for reservoir B, which is why the value is not a fresh
directory. Nothing outside gopi's own process tree was touched (no
`~/.bashrc`, no `/etc/environment`, no `/etc/tmpfiles.d` — those files' mtimes
are from 2026-09-13 / 2024-02-17, unmoved).

### 2. A REAL-CHILD transcript (kernel evidence, not a claim)

`gopi shot --url <loopback>` spawns the browser binary, and that binary is
injectable (`internal/shot`'s `GOPI_SHOT_CHROME`), so a probe can be the child.
With **TMPDIR unset in the environment** (`env -u TMPDIR`) and the settings
value in force:

```
$ [gopi] tmpdir: TMPDIR="/dev/shm/gopi-test-scratch/tmp" (from settings key tmpdir; every child inherits it)

# read from an INDEPENDENT process, while the child was alive:
$ cat /proc/565998/environ | tr '\0' '\n' | grep -E '^(TMPDIR|GOPI_HOME)='
GOPI_HOME=/home/user/.gopi
TMPDIR=/dev/shm/gopi-test-scratch/tmp

# the child's own report:
pid=565998
proc_self_environ[TMPDIR=/dev/shm/gopi-test-scratch/tmp]
os.TempDir=/dev/shm/gopi-test-scratch/tmp
device_of_os_TempDir=1217566478 (resolved /dev/shm/gopi-test-scratch/tmp)
device_of_slash=-535540565 (resolved /)
MkdirTemp=/dev/shm/gopi-test-scratch/tmp/probe-child-3834818960 (err=<nil>)
```

`/proc/<pid>/environ` is the block `execve` HANDED the child — it cannot be true
unless inheritance really happened (a child echoing `$TMPDIR` would only be
reporting on itself). Three acceptance facts are in those lines: the child's
`os.TempDir()` resolves off `/` by device id, its own environ carries the
injected variable, and a real `os.MkdirTemp()` landed there.

### 3. The in-repo regression test (red-before / GREEN-after, both transcripts)

`cmd/agent/tmpdir_settings_value_test.go` (new). It reads the **operator's**
settings file — explicitly reversing the suite's `GOPI_SETTINGS`/`GOPI_HOME`
sandbox pins with `testutil.SnapshotEnv` restore, and that reversal is
*necessary*: with the pin in place the test silently read the suite's empty
sandbox (`/…/suite-gopi-home/.gopi/settings.json`) and the gate was blind. It
then asserts (a) the key exists and its target is off `/` **by device id**,
(b) a **real child gopi spawns** is handed the configured value in its own
`/proc/self/environ`, and (c) that child's `mktemp` lands off `/` with **no new
entry** appearing under the fallback temp root (the root's own listing is
compared before/after, so the leak check does not depend on the child's
self-report).

**RED (before) — two red states, transcript verbatim:**

```
# (A) key ABSENT — the host's state before this task
--- FAIL: TestTmpDirSettingsValueReachesTheSpawnedChild (0.00s)
  the operator's settings /home/user/.gopi/settings.json carries no "tmpdir" key
  — gopi and every child it spawns fall back to /tmp, which is on the `/`
  partition on this host.

# (B) key PRESENT but pointing at the partition the rule bans
--- FAIL: TestTmpDirSettingsValueReachesTheSpawnedChild (0.00s)
  tmpdir "/tmp" is on the SAME filesystem as / (device 16146614864190929547)
  — the exact configuration that filled the boot partition with residue
```

The settings file was restored from a byte-exact backup after both runs
(re-verified: JSON valid, `"tmpdir"` occurs once, value intact).

**GREEN (after):**

```
--- PASS: TestTmpDirSettingsValueReachesTheSpawnedChild (2.26s)
  settings /home/user/.gopi/settings.json: tmpdir="/dev/shm/gopi-test-scratch/tmp"
    device=5229408204322450386 vs / device=16146614864190929547 (MEASURED, not spelled)
  child (pid 636225) /proc/self/environ TMPDIR="/dev/shm/gopi-test-scratch/tmp";
    its mktemp landed at "/dev/shm/gopi-test-scratch/tmp/gopi-tmpdir-value-probe-6Y0TBo"
    device=5229408204322450386 (off /, device 16146614864190929547)
--- PASS: TestTmpDirSettingsValueAmbientFallbackLeaks (2.24s)
  RED PROOF: with the ambient fallback in force the child landed at
    "/tmp/gopi-tmpdir-value-probe-YxOPV9" (device 16146614864190929547 = /)
    and added [gopi-tmpdir-value-probe-YxOPV9] under /tmp
  reclaimed /tmp/gopi-tmpdir-value-probe-YxOPV9 (0 bytes) — the one path this
    negative case created
```

The second case is the in-tree **red proof**: the observation is *capable* of
failing (it is the same probe, with the ambient fallback in force), so the green
case is not a function that always returns nil. It also reclaims exactly the one
path it caused, by reported path, with the size printed (Rule 22 forbids a
recursive wipe of the temp root).

Two things this test taught, both recorded in it so they are not "fixed" back:
`dash`'s `tr '\0' '\n'` spelling over `/proc/self/environ` produced an **EMPTY**
capture on this host (the `sed -e 's/\x0/\n/g'` form — the project's own
precedent — is the one that works); and the suite's profile pin means any test
of the *operator's* configuration must say so out loud.

### 4. Build + touched packages

```
$ CGO_ENABLED=0 go build ./...            → OK (no output)
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ → OK
$ go vet ./cmd/agent/ ./internal/shot/    → clean;  gofmt -l → clean

$ go test ./internal/shot/                → ok   (50.7s)
$ go test ./internal/config/ -run 'Settings|TmpDir' → ok
$ go test ./internal/scratch/             → ok   (57.0s)
$ GOPI_BIN=… go test ./cmd/agent/ -run 'TmpDir|Tmpdir' → ok (13.1s)
```

Two gate suites are red **on master too, with the identical hit list** (measured
by running them in `~/work/GoPi` at the same moment, not asserted):

- `internal/standards` — the pre-existing Rule 13 escape in
  `internal/shot/x11.go` (`/tmp/.X11-unix`). My `chrome.go` edit did not add a
  baseline key or shift one.
- `internal/stylegate` — the `errorf-verb` ceiling with 10 production hits
  (`internal/tools/memory_*.go`, `worktree_sweep.go` line-shifted by a
  concurrent branch, …). **No hit names a file I touched**, so my commit adds
  none; the ratchet belongs to their owners (a different task, not this epic).

### 5. The residue: what was safely reclaimable, and what was reclaimed

`scripts/audit-temp-residue.py` is the audit + reclaim tool (new, committed).
Its criterion is **not** a path list (Rule 22): an entry is reclaimable only when
**all** hold — owned by the invoking user, at least **7 days** old, and held by
**no** live process (the same liveness criterion the scratch GC uses: no
`/proc/<pid>/cwd|root|fd/*` points at or under it). The root is resolved through
`tempfile.gettempdir()` (never a spelled literal) and the script prints the
**measured** device ids of the root and of `/`. Reclaim is per verified path,
`--apply` opt-in, every path's size printed, no recursive wipe.

Age histogram of the temp root, `entries / MB`:

| bucket | BEFORE | AFTER reclaim |
| --- | --- | --- |
| `<1d` | 153 / 538 | 167 / 406 |
| `1-7d` | 1333 / 4581 | 1337 / 3424 |
| `7-30d` | **543 / 765** | **4 / 9** |
| `>30d` | 4 / 4 | 4 / 0 |
| **total** | **2033 entries / 4143 MB** | **1512 entries / 3911 MB** |

**Reclaimed: 534 entries, 228 MB** (`# reclaimed 534 entries, 228 MB`), the
whole `≥7d` slice the criterion accepted. Kept, and why (honest list):

| kept | size | reason |
| --- | --- | --- |
| 7 entries of another user (+ root's `systemd-private-*`, `snap-private-tmp`) | unreadable | never ours to judge or delete |
| `/tmp/h3mgui` | 8.9 MB / 9.6d | **held** by a live process (`/proc/*/cwd`) — the liveness veto working |
| `1-7d` slice | ~3.4 GB | **not safely reclaimable by any static criterion**: liveness cannot be attributed from outside the creating process, and this host runs concurrent agent sessions. This is the honest boundary of the fix, not a rounding error. |

`*.db` / `*.bak` files under the temp root (the epic's "80 MB scratch-ledger DB
copies and `*.bak` source copies") still total **720 MB**, all of them inside the
`<7d` slice — I did **not** reclaim them, for the reason above.

`/` free space moved `20G → 21G` (`df -h /`: `147G 120G 20G 86%` → `147G 119G 21G 86%`),
which is a smaller delta than the 228 MB reclaimed — other writers on this shared
host (concurrent sessions) added entries during the same window, so the free-space
figure is not a clean function of this reclaim. The entry-count and the histogram
are the measurements that are attributable; the free-space delta is reported as
observed.

## Not Done

- **The `1-7d` slice (~3.4 GB) was not reclaimed.** It cannot be attributed
  safely from outside the creating process (see the table). Reclaiming it would
  require the *creators* to honour the configured root — which is exactly what
  this task's part 1 does from now on. A future task could add a bounded,
  age-based sweep with the same holder veto, but it would be a new mechanism and
  this epic explicitly asked for "the configured value plus its proof".
- **No host-level change.** No `~/.bashrc`, `/etc/tmpfiles.d`, `/etc/environment`
  edit, and no `sudo` (Rule 25) — measured, not asserted: those files' mtimes are
  unchanged.
- **`internal/standards` / `internal/stylegate` remain red on master** (the
  pre-existing hits above). Not fixed here: neither is caused by this change,
  and both belong to the packages that own the offending lines.
- **The suite-hermeticity interaction is documented, not solved**: a test that
  reads the operator's real settings must reverse the suite pins itself. A
  shared helper for that would be a reasonable follow-up (this epic's test does
  it inline with `testutil.SnapshotEnv`), and it is the one piece a future agent
  might reasonably refactor.

## Files

| file | role |
| --- | --- |
| `cmd/agent/tmpdir_settings_value_test.go` | NEW — the regression gate: operator's key measured off `/`, real spawned child (environ + landed mktemp + no new fallback entry), plus the in-tree red proof with its own one-path reclaim |
| `internal/shot/chrome.go` | `ChromeEnvName = chromeEnvVar` — the `x11.go DisplayEnvName` alias pattern, so the probe writes the production variable instead of mirroring the literal (Rule 13/14) |
| `scripts/audit-temp-residue.py` | NEW — criterion-driven residue audit + per-path reclaim (owner + age + holder veto; measured device ids; explicit path list with sizes) |
| `/home/user/.gopi/settings.json` | the configured `tmpdir` value (host configuration, not a repo file) |
| `devdoc/tmpdir-settings-value.md` | this report |

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK; `go vet`/`gofmt` clean.
- Tests: `internal/shot` ok, `internal/config` ok, `internal/scratch` ok,
  `./cmd/agent -run 'TmpDir|Tmpdir'` ok — including the new test's RED-before
  (two red transcripts) and GREEN-after.
- Gates: `internal/standards` + `internal/stylegate` red **identically on master**
  (hit lists compared side by side, none naming a file this change touched).
- Host boundary: `git status` in `~/work/GoPi` shows none of this change's files;
  `~/.bashrc` / `/etc/environment` / `/etc/tmpfiles.d` mtimes unchanged.
