<!-- doc-class: normative -->
# Resident image self-check — the decider refuses code it is not running

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a133e70a5e77098fc2ce73b4782198` (branch `task/01a133e70a5e77098fc2ce73b4782198`).

## The symptom (measured, 2026-10-13, host peerbox)

```
$ ps -o pid,lstart,etime,cmd -p 3869823
3869823 Tue Oct 13 05:18:08 2026  08:32:13 gopi merge --every 2m

$ readlink -f /proc/3869823/exe
/home/user/work/GoPi/bin/gopi (deleted)

$ strings /proc/3869823/exe | grep -c "devdoc-anchor family may have drifted"      -> 0
$ strings /home/user/work/GoPi/bin/gopi | grep -c "devdoc-anchor family may have drifted" -> 1
```

Four gopi processes were in the same state. The resident `gopi merge --every 2m`
is the process that DECIDES landings, and it was running an image whose file had
been replaced under it: every gate edge added to the source after that build
existed in the working tree, in `bin/gopi`, and in every grep a reader ran —
and in NO part of the decider. Nothing said so; the loop kept printing ordinary
per-pass reports.

## Root cause

`go build -o bin/gopi …` writes a **new inode** and renames it over the old
path. A process that is already running keeps the inode it exec'd from
(`/proc/<pid>/exe` → `… (deleted)`), so "the binary on disk" and "the binary
deciding merges" were two different files. The kernel reports the divergence in
one place — the `(deleted)` marker on `/proc/<pid>/exe` — and nothing in the
resident face ever looked at it.

The second, subtler shape: a rebuild that writes the artifact **in place** (same
inode, new bytes) leaves no `(deleted)` marker at all, and only the two build
stamps tell the two images apart.

## The fix — in the resident face itself, not wrapped around it (Rule 35)

`internal/tools/image_selfcheck.go` answers one question about THIS process:
*is the image I am running still the image the machine would start from the path
I was started at?* Two arms, both positive evidence:

| arm | evidence | catches |
|---|---|---|
| ① REPLACED | `/proc/self/exe` resolves to a target carrying `procDeletedSuffix` (`" (deleted)"`) — the kernel's own marker, reusing the evidence `merger_status.go`'s orphan-lock probe already relies on | unlink + rename over (what actually happened on peerbox) |
| ② STALE STAMP | the running image's `vcs.revision` (this process's own `debug.ReadBuildInfo`) differs from the `vcs.revision` `debug/buildinfo` reads off the artifact at that path | a rebuild written IN PLACE |

Adopted by the resident faces (`cmd/agent/image_selfcheck.go`):

> **Retired mechanism (merge verb removal, 2026-11-01).** The resident face named
> in the bullet below, `gopi merge --every D`, no longer exists: the top-level
> verb and its `--every` loop were removed, and `./bin/gopi merge` answers exit
> **1**. What survives and is what this page is about: `internal/tools/image_selfcheck.go`
> itself, still adopted by `gopi worker`'s claim loop (the bullet further down).
> The merge-only reasoning in the first bullet is kept as the record of why a
> stale image must REFUSE rather than warn; the surviving merge face
> (`gopi task merge`) is a one-shot and has no loop to re-check.

- **`gopi merge --every D`** — the prologue checks once and **REFUSES** (exit 1)
  if stale; the loop **re-checks every tick**, because a loop that runs for hours
  can have its artifact replaced long after it started. This face is merge-only:
  a stale image there is a decider making permanent decisions with a gate set
  nobody can see, so refusing is the correct degraded mode.
- **`gopi worker` claim loop** — reports the staleness once, loudly, and keeps
  ticking. Deliberately different: this loop also carries the housekeeping ticks
  (lifecycle, scratch GC, review sweeps) that a stale image performs correctly,
  so exiting would convert a stale image into a NEW outage. The report is owed
  at the claim — the decision it is about.
- Both faces print ONE line when the check comes back clean, so "checked and
  current" is distinguishable from "the check is not in this build at all".

Rule 15 in both directions: an UNMEASURABLE check (no procfs, an unstamped
image) is reported as such (`ArmingNote`, "NOT ARMED" / "REPLACEMENT only") and
is never rendered as a clean bill of health; a stale check prints the alarm
naming the pid, both paths, both revisions and the remedy (restart the process).
A stale image also deliberately does **not** borrow `--advisory`-style escape
hatches: there is no override flag, because the point is that the decision
should not be made.

**No new dependency (Rule 35's four questions, answered).** (1) What breaks
without it: measured above — a decider running pre-edge code for 8.5 hours
without a word. (2) Why not in-process: it IS in-process; the check reads facts
the process already owns. (3) Ownership/lifecycle: nothing is created — no state
file, no timer, no unit, no daemon, no script. (4) What the next reader sees: the
alarm line explains itself and names the remedy; a clean run says the check ran.

## Verification

- **Build**: `go build ./...` → exit 0. `go vet ./internal/tools/ ./cmd/agent/` → exit 0.
- **Tests** (red proofs below): `go test ./internal/tools/ -run TestSelfImageCheck`
  → ok; `go test ./cmd/agent/ -run 'TestMergeEveryRefusesAStaleImage|TestSelfImageCheckArmedOnACleanImage'`
  → ok.
- **Ran** (Rule 31 — a real replacement of a real binary, twice):
  1. **In the gate** (`TestMergeEveryRefusesAStaleImage`): the package's built
     binary is copied into the suite scratch root, started as
     `gopi merge --every 5m --fast`, that file is unlinked and a fresh copy
     written at the same path, and the assertions read exit status 1, the alarm,
     and the absence of any pass line before the refusal.
  2. **By hand, on a live loop**
     (`artifacts/image-selfcheck-realrun.{sh,txt}` in this run's session dir;
     the loop ran with `--every 1s --fast` against a sandbox profile so the pass
     was milliseconds and touched no ref):
     ```
     $ readlink -f /proc/1900063/exe
     /home/user/workspace/gopi-imgselfcheck/gopi (deleted)
     gopi merge: REFUSING to decide with a stale image (pid 1900063) — the kernel
     reports this process's image as "…/gopi (deleted)" — the file at …/gopi was
     replaced (or unlinked) while this process kept running, so the code it is
     deciding with is NOT the code at that path; running image no VCS stamp
     (…/gopi (deleted)), artifact at that path no VCS stamp (…/gopi); restart this
     process … — the decisions it would make without the newest code are the
     defect this refusal prevents
     VERDICT: the loop exited on its own after the replacement
     exit status: 1
     ```
     The replacement itself is the shape `go build -o` produces: `mv -f` of a
     freshly built artifact ONTO the running path (rename(2) over an existing
     name replaces the dentry). An earlier attempt that `mv`'d the artifact AWAY
     first renamed the dentry and left the process with a live path — the check
     correctly did NOT refuse, which is why the scenario now reproduces the real
     shape. The two "no VCS stamp" fields are honest rather than a bug: that
     binary was built in a linked worktree, which the toolchain does not stamp
     (`ArmingNote` reports exactly this as "REPLACEMENT only").
  **Docs the change touches** were re-run too: `gopi merge -h` printed a new
  "Image self-check." paragraph in its OWN words — the carrier of the contract is
  `man gopi-merge`, not that page, and saying the page printed
  `mergeHelpImageSelfCheckNote` (or "the same sentence") was the false carrier
  claim corrected on 2026-10-13 (task 01a1350c) — and the flag table's `--every`
  row names the refusal.
- **Red proof ①** (`TestSelfImageCheckReplacedImageIsRefused`): with the
  `case c.Replaced:` arm disabled → FAIL "an unlinked image read as fresh:
  replaced=true stale=false"; restored, verified by `sha256sum -c` (OK).
- **Red proof ②** (`TestSelfImageCheckArmedOnACleanImage`, the control): with the
  tick check forced to fire → FAIL "a loop whose artifact was never replaced
  refused to run" and "run over — was never written"; restored, `sha256sum -c` OK.

## Not done (honest)

- The `(deleted)` state of the **already-running** processes on peerbox (pids
  3869823, 556550, 2441159, 1423501) is not cleared by this change: this run
  delivers the mechanism that makes the state loud and refuses to keep deciding
  on it, and a resident started BEFORE the fix has no check to run. Those
  processes must be restarted by the operator.
- Arm ② cannot fire for a build the toolchain did not stamp (a linked-worktree
  build carries no `vcs.*` settings) — reported by name rather than guessed at.
- The check is about THIS process's image. It deliberately does not enumerate or
  judge its peers (that would need a roster, a policy for strangers and a reason
  to be trusted).
