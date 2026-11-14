<!-- doc-class: historical -->
# Backfill: the worker-ownership pointer on master's remaining trigger files

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Task row `01a0a0a5` (superseded — its list was 5 files and stale) /
epic "master red: `internal/testutil` TestWorkerOwnershipPointerPresent".
Branch `fix/worker-ownership-9files`, worktree `~/workspace/gopi-wo-9files`,
base master `7b5d427a`.

## 1. The list is TEN files today, not nine

The gate's own output is the authority (the briefing's nine was measured on an
older master). Measured on `7b5d427a`:

```
briefings/brief-cwd-and-lifetime-complete.md
briefings/brief-cwd-release-coverage.md
briefings/brief-daemon-e2e-leak.md
briefings/brief-proc-triple-starttime.md
devdoc/daemon-cwd-escape.md
devdoc/daemon-e2e-leak.md
devdoc/rule18-voice-device-else.md
devdoc/task-list-inprogress-scan-20260914.md
devdoc/worker-help-capability-first.md
devdoc/worker-status-lock-assert.md
```

Four of the ten are files that landed on master **during** the 2026-09-14
daemon-leak/cwd work (the fix branches themselves, merged as `10f96bc0` /
`7b5d427a`), which is exactly how a "nine" briefing goes stale between
dispatch and execution. All TEN are annotated here — the epic's acceptance
("report names all nine files") is satisfied with the tenth named honestly as
a superset, see §3.

## 2. Disposition — every file ANNOTATED, zero dismissals

Each of the ten **instructs** the launch move somewhere: they tell a reader
which process runs the worker loop, quote a runnable command line as the
thing to reproduce, or (the briefings) hand a worker the exact next actions.
Under the gate's own decision rule ("does the file TEACH the launch move?")
that is the pointer half, not the dismiss half — so the disposition is
uniform: insert the one canonical pointer line after the doc title (or, where
the title is preceded by context, in the file's opening block), in the
markdown blockquote form the audit anchors already use.

Six of the ten mention the move as an ADDITIONALLY-owned implementation
detail, which is worth stating per file so a future reader knows why the
pointer is honest rather than mechanical:

| # | file | mention site | why the pointer (not dismiss) |
|---|---|---|---|
| 1 | `briefings/brief-cwd-and-lifetime-complete.md` | 覆盖单: orders the cwd-release work on the worker loop / executor child / merger loop | a work order that names the launch, deliberately paired with `01a0a07e`/`01a0a094` — instruction |
| 2 | `briefings/brief-cwd-release-coverage.md` | the gap table ("`gopi worker` 循环 … ❌ 没有") + the 交付物 enumeration | tells the reader which process to add the release to — instruction |
| 3 | `briefings/brief-daemon-e2e-leak.md` | quotes `scripts/gopi-worker-up.sh:88-89` as the live source to extend; counts leaked daemons | quotes the launcher's counting contract as the thing to change — instruction |
| 4 | `briefings/brief-proc-triple-starttime.md` | quotes the same launcher lines; lists shell callers to update | a work order against the launcher — instruction |
| 5 | `devdoc/daemon-cwd-escape.md` | names `gopi worker --daemon` / `gopi merger --daemon` as the spawn sites that inherited no `Dir`; §7 asks for a read-only sweep | it names the spawn shape while explaining the producer fix — the pointer keeps the naming from reading as a recipe |
| 6 | `devdoc/daemon-e2e-leak.md` | the e2e cases "start REAL gopi daemons (`gopi worker --daemon`)" | the command is quoted as what the TEST does — the pointer forbids the reader from copying it |
| 7 | `devdoc/rule18-voice-device-else.md` | "with several `gopi worker` processes live on the host" + the `--status` repro | host-state evidence; the line the epic must not have read as a recipe |
| 8 | `devdoc/task-list-inprogress-scan-20260914.md` | the audit table's claim-pid column (`3445263 gopi worker` …) | ledger evidence rows — same shape as the already-dismissed `brief-task-ledger-hygiene`; annotated anyway, since the table also names the run command |
| 9 | `devdoc/worker-help-capability-first.md` | the help-shape block (`gopi worker` / `gopi worker --once`) + `-h` invocations | it prints the CLI's own invocation list — the file most likely to be read as a usage page |
| 10 | `devdoc/worker-status-lock-assert.md` | the deterministic repro block (`gopi worker --daemon` → `--status`) | a copy-pasteable launch command — the clearest instruction of the ten |

**Nothing dismissed.** Consequence: `workerOwnershipDismissed` and the gate
predicate `workerOwnershipVerbNeedles` are **untouched**. Adding dismiss
entries for files that instruct a launch is exactly what the epic forbids.

Provability, stated precisely (the earlier draft of this section overclaimed):
the worker-ownership gate file itself is byte-identical to master —
`git diff master fix/worker-ownership-9files -- internal/testutil/` is EMPTY.
After the pin repair of §5 the branch's diff is ten docs + this report +
ONE added test file (`internal/daemoncwd/testmain_scratchroot_test.go`, the
scratch-root pin — not a gate edit).

**Why not dismiss the evidence-shaped ones (3, 7, 8)?** Because the pointer is
true there too, and the pointer is the STRONGER state: it is visible at the
mention site (a dismissal is invisible to a reader of the doc) and it cannot
rot into a stale permit. The dismiss table stays what it is — the reviewed
exception list for files that merely cite the move — not a parking lot for
docs nobody wanted to touch.

## 3. Acceptance

- `CGO_ENABLED=0 go test -count=1 ./internal/testutil/` — see §4 for the raw run.
- `go build ./...` — see §4.
- Gate predicate: unchanged (`internal/testutil/worker_ownership_gate_test.go`
  is not in the diff).
- Nine-file acceptance: the epic's list of nine is a subset of the ten fixed
  here; the tenth (`devdoc/worker-status-lock-assert.md`, landed on master as
  part of the same daemon-leak family) is reported instead of silently
  skipped. Fixing only nine would leave the gate red.
- Whole-package green: required §5 (the pre-existing `internal/daemoncwd`
  scratch-root pin), since that suite was red on master independently.

## 4. Verification (raw — re-measured, whole PACKAGE, not `-run`)

The first draft of this section quoted a `-run` filter and a package result
that had never been observed in a whole-package run. Re-measured honestly:

```
$ cd ~/workspace/gopi-wo-9files && export TMPDIR=/dev/shm/gopi-test-scratch/wo9
$ CGO_ENABLED=0 go test -count=1 ./internal/testutil/          # ~310s
--- FAIL: TestEveryTempUsingSuiteIsPinned (0.70s)
    scripts_gate_test.go:185: internal/daemoncwd creates scratch directories
    (t.TempDir()/os.MkdirTemp) but no TestMain pins the scratch root …
FAIL	github.com/nick/gopi/internal/testutil	309.995s
```

`TestWorkerOwnershipPointerPresent` is **absent from that failure list** —
with all ten files annotated it is GREEN (it is red on master in the same
run, naming exactly the ten paths of §1). The package was still red on a
DIFFERENT gate, see §5.

FINAL state of the branch, every line the epic's acceptance names, one run:

```
$ cd ~/workspace/gopi-wo-9files && export TMPDIR=/dev/shm/gopi-test-scratch/wo9
$ CGO_ENABLED=0 go test -count=1 ./internal/testutil/     # whole package
ok  	github.com/nick/gopi/internal/testutil	277.035s
$ CGO_ENABLED=0 go test -count=1 ./internal/daemoncwd/
ok  	github.com/nick/gopi/internal/daemoncwd	3.782s
$ CGO_ENABLED=0 go build ./...
BUILD_OK
```

Red proof (pristine master, detached worktree `~/workspace/gopi-verify-01a0a096`):

```
--- FAIL: TestWorkerOwnershipPointerPresent (0.14s)
    worker_ownership_gate_test.go:365: files that teach a worker-launch / task-run
    command without the operator-only pointer:
          briefings/brief-cwd-and-lifetime-complete.md
          briefings/brief-cwd-release-coverage.md
          briefings/brief-daemon-e2e-leak.md
          briefings/brief-proc-triple-starttime.md
          devdoc/daemon-cwd-escape.md
          devdoc/daemon-e2e-leak.md
          devdoc/rule18-voice-device-else.md
          devdoc/task-list-inprogress-scan-20260914.md
          devdoc/worker-help-capability-first.md
          devdoc/worker-status-lock-assert.md
```

## 5. The second, PRE-EXISTING red in the same package

`TestEveryTempUsingSuiteIsPinned` (Rule 22 / the `/tmp` ban) fails on
**pristine master `7b5d427a` too** — same message, `internal/daemoncwd`. It is
not caused by this branch, but the epic's acceptance is "the WHOLE package is
green", so it had to be closed here.

Root cause: `internal/daemoncwd` is the one scratch-using suite of 25 without
a `TestMain` calling `testutil.RunSuiteMain(m, …)`, so its scratch data lands
on the ambient temp root; the gate's `pinEntryPointMarkers` never match it.
Fix: `internal/daemoncwd/testmain_scratchroot_test.go`, a verbatim copy of the
sibling pin (`internal/doubaoasr/testmain_scratchroot_test.go`) — same
comment, same call, no variation.

```
$ CGO_ENABLED=0 go test -count=1 -run TestEveryTempUsingSuiteIsPinned ./internal/testutil/
ok  	github.com/nick/gopi/internal/testutil	2.895s
```
