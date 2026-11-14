<!-- doc-class: normative -->
# Task Report: worker-ownership pointer backfill (master-red `TestWorkerOwnershipPointerPresent`)

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

**Branch:** `fix/worker-ownership-pointer-backfill` (worktree `~/workspace/gopi-wngate`)
**Date:** 2026-09-13
**Epic:** BUG(master red): `internal/testutil` `TestWorkerOwnershipPointerPresent` — the
2026-09-13 backfill (pre-existing at `9f79d19f`, still red at master `fda4e364`)
**Verdict:** DOABLE — fixed; the diff is pure additions (0 deletions).

## The gate's question, and the per-file answer

`internal/testutil/worker_ownership_gate_test.go` fails any file under `devdoc/`,
`briefings/` and `scripts/` that MENTIONS a worker-launch / task-run shape without
either the ONE canonical pointer line

```
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
```

(`<!-- … -->` in markdown, `#` in shell, `//` in Go) or a **written reason** in
`workerOwnershipDismissed`. A bare dismissal is not a fix, and the table is also
bidirectional (a dismissed file that carries the marker fails — the table cannot
drift out of sync with reality).

Master reported **19 files** (not the 17 in the brief — master had moved since the
finding: `devdoc/daemon-context-isolation.md` and `devdoc/worker-daemon-singleton.md`
had joined the set; one of the 19 "lines" is the wrapped tail of a long path, so the
distinct file count is 19 including `devdoc/worker-ownership.md` itself, which is the
canonical target and was already dismissed). The gate truncates its report at
`workerOwnershipMaxReportedHits = 20`, so the list is exhaustive here.

The split is not cosmetic: the gate's own question is **does this file TEACH the
launch move, or merely NAME/CITE it?** Earlier "fixes" of this test chose dismiss for
files that plainly teach it (§ below), which is why this backfill re-read all of them.

### 9 files → pointer line (they teach the move)

The file tells a reader *how to start a worker / launcher* (a brief whose deliverable
is a launcher change, or a report whose finding is "the daemon must detach"):

| File | What it teaches |
|---|---|
| `briefings/brief-daemon-cgroup-detach.md` | the deliverable is "let `gopi worker --daemon` detach itself" |
| `briefings/brief-peer-inline-launchers.md` | the re-take whose subject IS the launcher (`~/bin/gopi-worker-up.sh` install seam) |
| `briefings/brief-worker-daemon-flag.md` | "make `gopi worker --daemon` self-background" with a run-the-daemon acceptance step |
| `briefings/brief-worker-log-session-attribution.md` | assigns launcher `logdir` work to the launcher's owner |
| `briefings/brief-worker-process-independence.md` | the correct-independent-launch research brief (`setsid nohup … worker`) |
| `briefings/brief-worker-up-miscounted-live-count.md` | "`gopi-worker-up.sh 3` printed …" — a count read by a human deciding whether to launch more |
| `devdoc/daemon-cgroup-detach-20260913.md` | the report of the daemon-detach epic: its whole subject is the launch path |
| `devdoc/worker-daemon-singleton.md` | "`gopi worker --daemon` could be started any number of times" — the singleton contract of the launcher |
| `devdoc/worker-self-release-verify-ea095c80.md` | evidence table whose rows are real `gopi worker --once` arms |

Placement follows the precedent of `briefings/brief-worker-idle-exit.md` and
`devdoc/worker-cohort-kill-20260912.md`: a blockquote line immediately after the H1
title (before the first section) in `briefings/`, immediately after the header block
in `devdoc/` — i.e. where a reader sees it *before* the instructions.

### 9 files → reasoned dismissal (they cite the move, they do not teach it)

| File | Reason (also carried in the table) |
|---|---|
| `briefings/brief-salvage-0412.md` | evidence: the six `gopi worker` casualties of the 04:12 kill are counted as collateral of `hermes` |
| `briefings/brief-worker-cohort-kill.md` | evidence: the forensic timeline of that cohort kill — the count IS the bug report |
| `briefings/brief-worktree-recycle-2.md` | evidence: quotes the operator's recollection of a past fix while reporting a recycling symptom |
| `briefings/brief-task-ledger-hygiene.md` | evidence: a ledger-hygiene inventory listing `gopi worker -h` usage-fix commits by subject line |
| `briefings/brief-pa-never-runs-tasks.md` | evidence + prohibition: the operator ruling that BANS the move (this gate's own source) — its quotes are what is being outlawed |
| `briefings/brief-merger-topverb-daemon.md` | design record: "merger is an independent top-level service **like `gopi worker` is a top-level command**" — the comparison IS the argument |
| `devdoc/daemon-context-isolation.md` | design/evidence record: names which process spawns which per task (a mechanism table), no recipe |
| `devdoc/signalrec-session-attribution.md` | design record: explains that `gopi task run` MINTS the child's run uuid at claim time — a mechanism note |
| `devdoc/merge-resolution-test-budget-single-source.md` | evidence: an aborted-run log line quoted as proof of a known full-suite hang class |

## Not done

- **`TestScriptsAreBoundedAndOffTmp` is still red in this package, and it is NOT this
  task's subject.** It is pre-existing master red (measured: 4 red tests on master,
  1 on this branch — see Verification), it belongs to the /tmp-ban workstream
  (task lineage `#01a092a8` cancelled / `#01a0987d` verified TMPDIR), and its demands
  (rewrite `scripts/verify-rehome.sh` to source `scripts/lib/tmp-ban.sh`; de-`/tmp`
  `scripts/voice-e2e-peer.sh` lines 126/131/etc.) are outside worker-ownership. Touching
  them here would smuggle an unrelated fix into a red-gate backfill. It is reported,
  not hidden.
- **No `workerOwnershipDismissed` pruning.** The table still carries the pre-existing
  entries; the new entries are appended with the same discipline. A future pass could
  re-check the older entries against the gate's TEACH-vs-CITE question (the entries
  for `devdoc/cli-command-audit.md`-class files were not re-reviewed here).
- The 9 pointer files keep their own wording; no hand-copied policy paragraph was
  added anywhere (a pointer cannot drift, a copy can).

## Verification

```
# RED PROOF — with this branch's changes stashed (pristine tree, identical to master):
$ CGO_ENABLED=0 go test ./internal/testutil/ -run TestWorkerOwnershipPointerPresent
--- FAIL: TestWorkerOwnershipPointerPresent (0.05s)   # 19 files reported
FAIL

# GREEN — with the changes applied:
$ CGO_ENABLED=0 go test ./internal/testutil/ -run 'TestWorkerOwnership' -v
--- PASS: TestWorkerOwnershipPointerPresent (0.03s)
--- PASS: TestWorkerOwnershipCanonicalBlockIsReal (0.00s)
--- PASS: TestWorkerOwnershipGateScansTheTree (0.03s)
--- PASS: TestWorkerOwnershipDismissTableIsHonest (0.00s)
ok  	github.com/nick/gopi/internal/testutil

# The dismiss table is honest in the other direction too: TestWorkerOwnershipDismissTableIsHonest
# (a dismissed file that carries the marker fails) passes on all 9 new entries.

$ gofmt -l internal/testutil/ internal/version/      # (no output)
$ git diff --stat
 briefings/brief-daemon-cgroup-detach.md            |  2 ++   … (9 doc files, 2 lines each)
 internal/testutil/worker_ownership_gate_test.go    | 28 ++++++++++++++++++++++
 internal/version/testmain_scratchroot_test.go      | new file
 46 insertions(+), 0 deletions(-)          → additive only, no existing line changed
```

**Collateral fix (same package, same run):** master's `TestEveryTempUsingSuiteIsPinned`
and `TestPinCoverageTable` were also red, on exactly one cause —
`internal/version` creates scratch directories but had no `TestMain` pinning the
scratch root. Fixed with the standard one-line entry point
(`internal/version/testmain_scratchroot_test.go` → `testutil.RunSuiteMain(m, "version")`,
the pattern 8 sibling suites already use; Rule 22 / `devdoc/tmp-ban-scratch-root.md`).
Both tests went red → green; `internal/version` stays `ok`.

**Package-level before/after (measured, `-v`, failures sorted):**

| | master `fda4e364` | this branch |
|---|---|---|
| `TestWorkerOwnershipPointerPresent` | FAIL (19 files) | PASS |
| `TestEveryTempUsingSuiteIsPinned` | FAIL (`internal/version`) | PASS |
| `TestPinCoverageTable` | FAIL | PASS |
| `TestScriptsAreBoundedAndOffTmp` | FAIL | FAIL (pre-existing, unrelated: /tmp-ban) |

The two runs' `TestScriptsAreBoundedAndOffTmp` finding lists are byte-identical apart
from the scan counter (`438` → `439` test files, the file this branch adds) — proof
that the remaining red is master's, not introduced here.

**Build.** `CGO_ENABLED=0 go build ./internal/...` → OK. The honest caveat: a
**repo-wide** `go build ./...` is RED on master itself, unchanged by this branch —
`cmd/agent/cli_tasks_worker.go:58:2: "sync/atomic" imported and not used` (the same
error in a pristine `~/work/GoPi` checkout at `fda4e364`). That is a separate
master-red defect, reported here rather than papered over; nothing in this branch
touches `cmd/agent/`.

**Commits:** `c624a800` (code + docs, 46 insertions, 0 deletions) and the report
commit that follows it on `fix/worker-ownership-pointer-backfill`.
