<!-- doc-class: normative -->
# Task Report: why does `gopi task run <id>` report "previous 14 crashes"?

Diagnosis of the startup line

```
[gopi] crash-dump: backfilled 14 orphan session dump(s) from previous abnormal exits
```

and of the repo debris (`w.txt`, `y.txt`, `.leakprobe*`, `tmp-e2e/`) reported alongside it.

## TL;DR — answers to the three questions

1. **What is that work?** It is the **orphan-crash-dump sweep** (crash-dump epic ②),
   called unconditionally from `main()`. It is **not** worker-mode logic and **not**
   task-run logic — it runs on *every* `gopi` invocation (chat, `task run`,
   `task list`, any subcommand). So: not a mode confusion, but a **global startup
   side effect that a task-run path has no business paying for**.
2. **Why 14?** Because **the sweep has no way to tell a *live* sibling session from
   a crashed one**. Its only exclusion is the *current process's own* session id
   (`BackfillOrphanCrashDumps(sessionIDOf(ag))`, `cmd/agent/main.go:277`). Any other
   gopi process that is alive *right now* — or any session that ended between the
   dump and today — is written up as a crash. These are **false positives**, and
   they are permanent: `BackfillOrphanCrashDumps` never removes a dump whose session
   later ended cleanly.
3. **Is it a sign of bad engineering?** The sweep itself is defensible; the
   *misfire rate*, the *unconditional per-startup full-tree scan* (measured: **~0.6 s
   wall + ~431 MB allocation churn** per startup, even when writing 0 dumps), and the
   tracked repo junk are real debt. Details below.

## Evidence

Reproduction ran live on this host at 2026-09-12 03:17 (two runs, 8 minutes apart).

| Claim | Command / data |
|---|---|
| 14 dumps were written in ONE clock minute, matching the reported number | `ls -l --time-style=+%m-%d_%H:%M:%S ~/.gopi/sessions/*/*.dump.json` → **14** dumps at 03:09, 2 at 03:17 |
| A dump is written for a session that is **alive** | my own task session `775a06ef-3c1c-43ec-a26d-3f1dc6426b3c` (dir `01a091e7-…`) has **no** `session_end` in its JSONL and its file was growing while the task ran → it is a live session with zero dumps, but a *sibling* live session had one: `8cc49594-…jsonl` was dumped at `03:17:53` and kept growing until `03:19:19` with still no `session_end` |
| The detector *ignores any `.jsonl` without a `session_end` tail* | `internal/agent/session.go` `DetectOrphanSessions` / `HasSessionEnd`, `sessionEndScanBackMax = 4096` |
| Dumps are **never reconciled** — 117 of 473 are for sessions that ended cleanly *after* the dump | for each `*.dump.json`, read `session_log` and `grep -c '"type":"session_end"'` → **117/473** now carry a clean end; `473` dumps total (473 `crash` + 5 `unknown` + 2 `vendor`) |
| The scan is **unconditional and full-tree** | `cmd/agent/main.go:277` (no mode/tty/subcommand gate) + `DetectOrphanSessions` (ReadDir of `~/.gopi/sessions`, then *every* session dir) → **28,549** entries, **405 MB** of JSONL |
| **Measured startup cost** (real code, zero dumps to write) | `BackfillOrphanCrashDumps` = **668 ms**, `DetectOrphanSessions` = **648 ms** / 357 orphans, alloc churn **430.9 MB** (`runtime.MemStats.TotalAlloc` delta), `VmHWM` 36.7 MB — probe in the worktree, `go run ./tmpbench` |
| Cost root cause | `HasSessionEnd` does `os.ReadFile(path)` → the **whole** JSONL (up to 31 MB; 405 MB across the tree) to look at a 4 KB window; Go's GC keeps live heap small (0.4 MB) but every file is allocated and discarded |
| Sessions tree size | `du -sh ~/.gopi/sessions` → **8.6 GB** |

## Root cause (5-why)

1. `task run` prints "backfilled 14 orphan dumps" → 2. the sweep wrote a dump for each
   JSONL lacking `session_end` → 3. it only excludes *this process's* session, so
   concurrently-running sessions look identical to killed ones → 4. **the
   "no session_end" predicate is not a crash signal until the process is proven
   dead**, and the sweep runs *at startup of a new process* — the moment when the most
   other sessions are alive → 5. the design (`devdoc/crash-dump-design.md` §60, "排除当前
   session 后" ✅) only ever considered the single-session case, so a multi-session host
   was never modelled. Consequence: the dump is a **permanent, un-reconciled claim**
   ("this died") that the later `session_end` never retracts.

The 03:09 batch of 14 is the same phenomenon: at 03:09 fourteen gopi sessions were
mid-flight (nightly/probe runs); the next gopi start swept them as corpses. The one
`8cc49594-…` dump at 03:17:53 is the *proof*: it was written for a session whose
JSONL was still being appended 90 seconds later.

## Repo debris (`w.txt`, `y.txt`, `.leakprobe*`, `tmp-e2e/`)

- `w.txt` (2 B, contains `x`) and `y.txt` (2 B, contains `y`) are **git-tracked**,
  committed by author `t` on 2026-09-01 with the commit messages `w` and `y`
  (`b9204a02`, `d6b47671`) — ad-hoc scratch files, not fixtures, not referenced by
  code or tests.
- Also tracked in the repo root: `.leakprobe*` (a 19 MB **test binary**
  `.leakprobe-agent.test`, `.leakprobe-full.out`, and a copy of a **real
  `~/.gopi/memory.db`**, `.leakprobe/home/.gopi/sessions/*.jsonl`), `.leakprobe2/`
  (a full `go-build` cache, thousands of files), `tmp-e2e/` logs, `.recycle-epic-marker`.
- These are unrelated to the crash line (they are not read by the sweep), but they are
  genuine hygiene debt: committed scratch output, a committed profile DB, and a
  committed build artifact in a repo whose `.gitignore` already covers `bin/`.

## Not Done (honest)

- **No production fix landed in this task.** The two defects below are behaviour
  changes that need their own verification, so they were filed as bug tasks instead of
  patched on the diagnostic branch.
- The exact 14-count is per-run; it depends on how many gopi processes were alive at
  the previous start. The *mechanism* (not the number) is what is established here.
- One observation I could not fully explain, recorded for the follow-up: this runner
  session had **no** `GOPI_TASK_WRAPPER_ID` (so no "wrapper child" line — that line is
  therefore *correct* when present: a `task run` child must not mint a second ledger
  row, `internal/agent/taskregister.go`), yet `GOPI_EXECUTOR_SESSION_ID=775a06ef-…`
  named a session whose dir is `gopi-sessions/01a091e7-…` (the task id). A naming/ID
  smell, not a crash cause.

## Recommended fixes (filed as bug tasks)

**A — false positives (`[gopi] crash-dump: backfilled N`)**
1. Only dump a session whose JSONL mtime is older than a live-window (e.g. a few
   minutes): a file being appended right now is not a corpse.
2. Reconcile instead of asserting forever: if the dump exists but the JSONL later
   carries `session_end`, mark it resolved (or delete it) — a crash claim must be
   retractable.
3. Reword the startup line: it currently reads as "you had 14 crashes" when it means
   "14 sessions have no recorded end".

**B — startup cost**
1. `HasSessionEnd` must read the **tail** (`os.Open` + `ReadAt` on the last
   `sessionEndScanBackMax` bytes), not `os.ReadFile` of the whole file — that removes
   the 431 MB churn and most of the 0.6 s.
2. Stop paying it on every `gopi` start: the tree is 28.5 k dirs / 8.6 GB. Fold the
   orphan sweep into the existing one-shot sweep face (`gopi scratch-gc`, `d306bcdc`)
   or gate it behind an explicit trigger / a staleness marker.

## Verification

- Build (worktree `~/workspace/gopi-crashdiag`, branch `diag/crash-dump-startup`):
  `CGO_ENABLED=0 go build ./...` → exit 0, no output.
- Tests: `go test ./internal/agent/ -run 'Crash|Orphan|SessionEnd' -count=1` →
  `ok github.com/the operator/gopi/internal/agent 0.023s`.
- Repo hygiene: nothing in the main repo was modified; the only edits are on branch
  `diag/crash-dump-startup` in worktree `~/workspace/gopi-crashdiag`
  (`git status --short` in `/home/user/work/GoPi` lists no file from this diagnosis).
- Commit: this doc on `diag/crash-dump-startup` (see the branch's log).

## Follow-up tasks filed (the fixes are NOT in this branch)

| Task | Scope |
|---|---|
| `01a091ea-f519-7404-a22e-6da85f579f99` (pending, bug) | crash-dump backfill reports LIVE/cleanly-ended sessions as crashes — fix A1/A2/A3 (mtime live-window, reconciliation, rewording) |
| `01a091ea-f649-79b5-8d64-a18eca4e7ca0` (pending, bug) | the sweep's ~0.6 s + 431 MB alloc churn on EVERY startup — fix B1/B2 (tail read instead of whole-file `ReadFile`; stop paying it per start) |
| `01a091eb-f575-7c4f-88a3-5fcec5d55d75` (pending, hygiene+bug) | remove the 434 git-tracked scratch/junk paths in the repo root (`w.txt`, `y.txt`, `.leakprobe*`, `tmp-e2e/`) + the regression guard |

## Verification recipe (reuse — do not re-derive)

- Confirm the false positive in one shot: pick a `*.dump.json`, stat its mtime, then
  `stat -c %y ~/.gopi/sessions/<task>/*/<session>.jsonl` — a JSONL whose mtime is
  LATER than the dump (or which is still growing) proves the session was alive.
- Confirm the sweep cost without touching production: build a tiny `main` in a
  worktree that calls `agent.BackfillOrphanCrashDumps("bench-no-such-session")` and
  `agent.DetectOrphanSessions(...)` around `runtime.ReadMemStats` (probe in the
  Appendix; `VmHWM` from `/proc/self/status` is the honest high-water mark,
  `TotalAlloc` delta is the churn).

## Appendix — the cost probe (reproducible)

Run from a worktree of this repo (`go run ./tmpbench`, then delete the dir):

```go
package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/the operator/gopi/internal/agent"
	"github.com/the operator/gopi/internal/config"
)

func rssKB() int { // VmHWM is the true high-water mark; TotalAlloc is churn
	b, _ := os.ReadFile("/proc/self/status")
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VmHWM:") {
			var kb int
			fmt.Sscanf(strings.TrimPrefix(l, "VmHWM:"), "%d", &kb)
			return kb
		}
	}
	return -1
}

func main() {
	fmt.Println("gopi dir:", must(config.GopiDir()))
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	t0 := time.Now()
	n := agent.BackfillOrphanCrashDumps("bench-no-such-session")
	runtime.ReadMemStats(&after)
	fmt.Printf("BackfillOrphanCrashDumps: written=%d elapsed=%s alloc-churn=%.1f MB VmHWM=%.1f MB\n",
		n, time.Since(t0), float64(after.TotalAlloc-before.TotalAlloc)/1048576, float64(rssKB())/1024)
	t1 := time.Now()
	o := agent.DetectOrphanSessions("bench-no-such-session")
	fmt.Printf("DetectOrphanSessions: n=%d elapsed=%s\n", len(o), time.Since(t1))
}
```

Result on this host (28,549 session entries, 405 MB JSONL, 0 dumps to write):

```
BackfillOrphanCrashDumps: written=0 elapsed=668.9ms alloc-churn=430.9 MB VmHWM=36.7 MB
DetectOrphanSessions: n=357 elapsed=647.9ms
```
