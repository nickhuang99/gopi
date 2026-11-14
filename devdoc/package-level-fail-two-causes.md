<!-- doc-class: historical -->
# The package-level fail in `cmd/agent` — TWO causes, and a reachability claim corrected

**Task:** 01a133ab-50bf-7b51-b6d8-90050f20820a · **Ground tip:** `7f17e115e`
**Task this reports on:** the acceptance run of the nil-`*sql.DB` fix (`b10ded051`)

A run stream carrying `Action=…"fail"` with **no `Test` field** is a
**package-level verdict** — `go test` prints one such line when the test binary
never produced a per-test verdict for the package. This document exists because
that shape has **more than one cause**, and a briefing for this run named the
wrong one (both the cause and the reachability of its fix).

## Cause 1 — nil `*sql.DB` → SIGSEGV (FIXED, and its fix IS reachable)

The note leg's second retrieval leg asked a warm handle *"is the store I want the
one you already hold?"* and answered with **path equality alone**, while
`(*prefetchHandle).close()` nulls only `db` and keeps `path`. A released handle
therefore looked reusable, and `db.usableDB()` — `nil` — reached
`tools.ChatNoteSearchOn`, whose first statement calls `(*sql.DB).QueryContext`:
a nil dereference inside `database/sql`, i.e. a SIGSEGV that kills the test
binary before it can report which test crashed it. Full write-up:
`devdoc/prefetch-notes-nil-handle.md`.

**Measured in this run, on the real tip `7f17e115e`:**

```
$ go test -count=1 -timeout 300s -run '<the two regression tests>' ./cmd/agent/
ok  	github.com/nick/gopi/cmd/agent	4.737s
```
and in the full-package stream of this run all three of the defect's tests carry
`"Action":"pass"` (`TestPrefetchSealDoesNotWaitForAnInFlightLookup`,
`TestPrefetchNotesLegDoesNotReuseAReleasedHandle`,
`TestPrefetchLookupDrivesAReleasedSharedHandle`), with `grep -c SIGSEGV <stream>` = **0**.

### The red proof (Rule 5's restore criterion, explicit revision)

With the three production files overwritten by their pre-fix blobs
(`b10ded051^`):

```
--- FAIL: TestPrefetchNotesLegDoesNotReuseAReleasedHandle (0.03s)
    chat_prefetch_notes_released_handle_test.go:101: the note leg reused a RELEASED handle: …
--- FAIL: TestPrefetchLookupDrivesAReleasedSharedHandle (0.07s)
panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x1 addr=0x20 pc=0x6250d4]
database/sql.(*DB).conn(0x0, …) /usr/local/go/src/database/sql/sql.go:1317
    github.com/nick/gopi/internal/tools.chatNoteQuery(…)
    	…/internal/tools/chat_notes.go:574
    github.com/nick/gopi/internal/tools.ChatNoteSearchOn(…, 0x0, …)   ← the nil handle
    	…/internal/tools/chat_notes.go:455
    github.com/nick/gopi/cmd/agent.prefetchNotesSearch(…)
    	…/cmd/agent/chat_prefetch_notes.go:302
FAIL	github.com/nick/gopi/cmd/agent	5.244s
```

Restored, and **verified by explicit revision** (never an index-relative diff):

```
$ test "$(git hash-object <p>)" = "$(git rev-parse HEAD:<p>)"
MATCH     internal/tools/chat_notes.go             a36b4e3f1f18e794e5451b3996920003cb16ce9e
MATCH     cmd/agent/chat_prefetch.go               207abb593edc02a3dc26e8b128dea1d7473dfd30
MATCH     cmd/agent/chat_prefetch_notes.go         92dbd416ff3a99b4e04992a3948b33dc2024aed4
MATCH=3 MISMATCH=0
```

## Cause 2 — a leaked watcher goroutine (LIVE, and this is what failed this run)

The acceptance run of this task **still ends `exit=1`**, with exactly **one**
`Action=="fail"` line and no `Test` field — but `grep -c SIGSEGV` = **0**. The
cause is not a crash:

```
suite goroutine sweep: parked repository goroutines: 6
suite goroutine sweep: goroutine NNNNN [chan receive]:
  github.com/nick/gopi/cmd/agent.workerDaemonCtx.installFileWatcher.func1()
  	cmd/agent/cli_worker_control.go:2164
  created by github.com/nick/gopi/cmd/agent.workerDaemonCtx.installFileWatcher in goroutine NNNNN
  	cmd/agent/cli_worker_control.go:2161
```

`cmd/agent`'s `TestMain` turns that count into a non-zero exit
(`cmd/agent/testmain_test.go:91`: `if parked := reportSuiteGoroutinesAtCloseOut(); parked > 0 && code == 0 { code = 1 }`),
so a leak is reported as a **package-level** fail — the same shape as Cause 1,
from a completely different mechanism.

**Why it leaks.** `installFileWatcher` has no exit path at all:

```go
func (c workerDaemonCtx) installFileWatcher() {
	if !c.consumesRequests() { return }
	go func() {
		t := time.NewTicker(workerFileWatchInterval)
		defer t.Stop()
		for range t.C {          // only the ticker — no ctx / done / stop channel
			c.consumeDrainRequests()
		}
	}()
}
```

Its own comment says *"The watcher stops with the process"*. For a **resident**
worker that is true. For a caller that **returns** — an in-process test — it is
not: nothing ends the goroutine, so every install parks one forever. Install
sites: `cmd/agent/cli_tasks_worker.go:328` (`runTaskWorkerExplicit`) and
`:413` (`runTaskWorkerLoop`).

**Localized to three tests** (per-test `-run '^<Test>$'`, same tree tip):

| test | parked | verdict |
|---|---|---|
| `TestWorkerSelfExitsAfterIdleRounds` | **2** | FAIL |
| `TestWorkerLoopDoesNotWaitOnTheResidentSweep` | **1** | FAIL |
| `TestWorkerBusyRoundResetsIdleStreak` | **1** | FAIL |
| six other worker-facing tests | (no line) | ok |

One-command repro (~15s):
`go test -count=1 -timeout 200s -run 'TestWorkerSelfExitsAfterIdleRounds' ./cmd/agent/` → `parked repository goroutines: 2`, FAIL.

**It is not the prefetch change.** `b10ded051`'s diff touches neither
`cli_worker_control.go` nor `cli_tasks_worker.go`, and all three of its tests pass
in this run's stream. Filed as bug rows `01a133e3-24e2-73f0-a514-bd0f0700bb23`
(diagnosis) and `01a133e5-6013-7b39-97a7-dbe3a9b7a9eb` (fix, carrying the
localization). **Rule 15 applies to the fix as well**: the honest repair is an
exit path on the watcher, never a widened sweep threshold — that sweep is the only
instrument that can see this leak.

## The reachability claim the briefing made, corrected

The briefing asserted `b10ded051` had **no ref that could reach it**. Measured:

```
$ for r in HEAD master remotes/gitPushcheck/master; do git merge-base --is-ancestor b10ded051 $r; done
HEAD                           REACHABLE
master                         not reachable        ← local master is 119 commits behind
remotes/gitPushcheck/master    REACHABLE            ← where the answer actually lives

$ git branch --contains b10ded051        # local refs only
* task/01a133ab50bf7b51b6d890050f20820a
$ git branch -a --contains b10ded051     # remote-tracking included
* task/01a133ab50bf7b51b6d890050f20820a
  remotes/gitPushcheck/master
```

The same sha gets **three different answers** from three refs, and the fix landed
through `919ed77a0` ("Merge branch 'task/01a13329388970dca5514257a19cdb0d'") on
the peer master, which is this worktree's cut point. The guidance that survived,
and the reason this is written down:

1. **Ask the content question, not only the ref question.** `git grep -n '<symbol>' -- <path>`
   is the verdict that a fix is in the tree; a reachability query is a *proxy*, and
   `git branch` without `-a` reads only local refs.
2. **Before reporting "no ref reaches it", name the refs you checked** — including
   `remotes/*` — with each one's exit code. A conclusion from an unstated,
   incomplete ref set is not a fact, it is a missing instrument.
3. **A local `master` that trails the peer master by N commits is not a baseline.**
   Verify against the ground's own tip and say how far behind local `master` is.
4. **A content miss must be split before it becomes a conclusion**:真阴性 (the fix
   is genuinely absent) vs 假阴性 (wrong ref / wrong package / wrong case). Here it
   was a false negative produced by the third reason.

census-evidence-ok: the figures in this document count one run's own stream (a
narrative record) and name it; no project-wide census figure is stated.
