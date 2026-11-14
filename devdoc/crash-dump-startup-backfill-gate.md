<!-- doc-class: historical -->
# Task Report: the orphan-crash-dump sweep no longer costs a whole-tree read on every start

**Branch:** `fix/crash-dump-tail-read` (worktree `~/workspace/gopi-crash-tail`, base `79d6fe52`)
**Files:** `internal/agent/session.go` (HasSessionEnd), `internal/agent/crashdump_sweep_gate.go` (new),
`cmd/agent/main.go` (startup call), + 2 test files.

## Symptom (as briefed)

Every `gopi` invocation — chat, `task run`, `task list`, any subcommand — paid a
full-tree session scan before doing any work:

```
BackfillOrphanCrashDumps: written=0 elapsed=668.9ms alloc-churn=430.9 MB   (2026-09-12, 28,549 entries)
DetectOrphanSessions:                            elapsed=647.9ms
```

## Root cause (5-why)

1. the sweep walks every session under the data root → 2. per file it asks
`HasSessionEnd` "is there a `session_end` in the tail?" → 3. `HasSessionEnd` did
`os.ReadFile(path)` — **the whole file** (up to 31 MB observed) → 4. to inspect at
most `sessionEndScanBackMax` (4096) bytes at the end → 5. so the cost was
proportional to the *tree*, not to the *question asked*: 431 MB of allocation
churn per start on a tree that only grows. The GC kept live heap small, which is
why this was invisible to heap-based monitoring — `TotalAlloc` is where it shows.

## Completed

### (a) `HasSessionEnd` reads the TAIL, never the file

`internal/agent/session.go`: `os.ReadFile` → `os.Open` + `Stat` + one `ReadAt` of a
`sessionTailWindowBytes = sessionEndScanBackMax + 1` window at the file end. The
one extra leading byte is what keeps the first candidate line newline-bounded —
without it the partial head line would be spliced onto the first complete event
and every candidate would be unparseable. The backwards walk itself is unchanged
(same bound, same semantics: "a `session_end` within the last 4096 bytes of tail").

Two details that matter:

- **`sync.Pool` for the window buffer.** A 4 KB window per file is still ~120 MB
  of churn across a 29k-entry tree; the pool removes it. Measured: churn is now
  *independent of tree size* (22.4 MB for both probes, flat across rounds).
- **A test seam, not a production abstraction:** `openSessionTailFile` (var) +
  the `sessionTailFile` interface exist so the proof can count bytes off disk.

### (b) The sweep no longer runs on every start

New `internal/agent/crashdump_sweep_gate.go`:

| state | verdict |
|---|---|
| no stamp / unreadable stamp | sweep + stamp (fail-open — a broken marker must never suppress the sweep forever, Rule 15) |
| stamp from an earlier UTC day | sweep + stamp |
| stamp from today | **skip — nothing is walked at all** |
| `GOPI_CRASH_DUMP_SWEEP=force` | sweep now + stamp (the explicit trigger) |
| `GOPI_CRASH_DUMP_SWEEP=off` | skip, stamp untouched |

`cmd/agent/main.go` calls `agent.OrphanSweepDue(time.Now())` first and walks only
when due; the stamp is written **after** the sweep, so a sweep that panics mid-walk
is retried on the next start. The interval is a **calendar day key**, not
"last sweep + 24 h": a wall-clock latency would drift later on every start
(a 23:00 start pushes the next sweep to 23:00 tomorrow), while a UTC day key is
stable, legible in the file, and bounded by one day whatever the start pattern.

Why a stamp and not `gopi scratch-gc`: that verb's contract is the *suite scratch
home* — a different tree, a different clock, and a log line a watchdog greps;
widening it would make that line ambiguous exactly where it is machine-read. The
explicit trigger the brief asks for is the documented env value above (zero new
CLI surface, Rule 3).

### (c) Coordination with the sibling false-positive fix

The sibling task (`feat/fix-crashdump-false-positive`) owns *which* sessions are
orphans (an `orphanLiveWindow` mtime quiet-period + dump retraction) — it changes
`DetectOrphanSessions` / `orphanCheckJSONL` / `BackfillOrphanCrashDumps`'s return
shape. This task owns *when* the sweep runs and *how a file is read*; it adds no
predicate over session semantics. The combination was verified, not assumed — see
§Combination.

## Not Done (honest)

- **No new CLI verb** for the sweep (e.g. `gopi crash-dump-sweep`). The brief
  allowed "folding into an existing one-shot face **or** an explicit trigger /
  staleness marker"; the env trigger was chosen (Rule 3: no new tool/verb surface
  without proof). If an operator later wants a verb, it is a 5-line addition on
  top of `OrphanSweepDue` + `MarkOrphanSweepDone`.
- **The walk itself is still O(tree)** on the day it runs (29k `ReadDir`+`Stat`).
  Removing *that* needs the reconciler to keep incremental state (e.g. a
  directory listing snapshot), which is a design change, not this fix; the daily
  gate bounds its frequency to once a day and the tail read bounds its price.
- The probe (`tmpbench/`, `measure.sh/measure.out`) is deliberately **not
  committed** (the brief bans committing the probe; `/tmp` is banned as a work
  location, so it lives untracked inside the worktree).

## Verification

**① Measured before/after — same probe, interleaved OLD/NEW rounds on the live host**
(`go run ./tmpbench`; OLD = base `79d6fe52` in a detached worktree, NEW = this branch;
29,390–29,405 entries / 8.8 GB tree; `BackfillOrphanCrashDumps` called with every
detected orphan excluded, so it scans fully and writes 0 dumps):

| probe | OLD (whole-file read) | NEW (bounded tail read) |
|---|---|---|
| `DetectOrphanSessions` | 1.437 / 1.970 / 2.262 s — median **1.970 s** | 0.855 / 0.984 / 1.519 s — median **0.984 s** |
| ↳ alloc churn | 456.2 / 456.2 / 456.4 MB → **456 MB** | 22.4 / 22.4 / 22.4 MB → **22.4 MB (20× less, flat)** |
| `BackfillOrphanCrashDumps` | 1.733 / 2.204 / 1.455 s — median **1.733 s** | 1.007 / 0.951 / 1.623 s — median **1.007 s** |
| ↳ alloc churn | 328.8 / 328.8 / 328.7 MB → **328.8 MB** | 19.6 / 19.7 / 19.7 MB → **19.7 MB (17× less)** |
| ↳ `VmHWM` | 55–65 MB | 16.9–17.5 MB |

The residual ~1 s is the inescapable walk (29k `ReadDir`+`Stat`), which is why (b)
exists: **on a stamped day the walk does not happen at all**, so a host pays this
at most once per UTC day instead of once per `gopi` invocation.

**② Bounded read proven as a number** — `internal/agent/sessionend_bounded_read_test.go`
substitutes the opener with a counting `ReaderAt`:

- `TestHasSessionEndReadsTailOnly_MultiMBFile`: 8 MB file → **≤ 4097 bytes read**
  (the whole-file shape read 8,389,493 — verified by temporarily restoring
  `os.ReadFile`: all three proofs failed with the old numbers, then went green
  again after restore — the required RED-proof).
- `..._OrphanFile`: same bound on the negative (orphan) verdict.
- `TestHasSessionEndAllocChurnIsBounded`: 200 probes over a 4 MB file →
  **0.08 MB** churn (OLD: 840.6 MB).
- Window-boundary tests: a `session_end` behind a mid-line window cut is still
  found; a garbage-only window still reports an orphan (never a false "ended").

**③ The gate proven end-to-end, on the real binary** —
`crashdump_sweep_gate_test.go` runs the actual `gopi` binary against a sandbox
home holding a seeded orphan session whose dump does not exist yet:

- today's stamp → exit 0 and **0 dumps written** (the walk did not happen; with
  `due` forced to `true` the same phase fails with "2 dump(s) written" — RED-proof);
- stale stamp → the sweep runs and the orphan's dump IS written.
- Plus the full verdict table (no/stale/fresh/unreadable stamp, force, off) and a
  stamp round-trip across UTC days.

## Combination with the sibling fix (the brief's explicit ask)

The sibling task (`feat/fix-crashdump-false-positive`, worktree
`~/workspace/gopi-fix-crashdump`) and this branch both edit the same statement in
`cmd/agent/main.go` (the startup sweep call), and both edit `internal/agent/session.go`.
A scratch worktree held the two merges to find the exact resolution — with
`git merge fix/crash-dump-tail-read` FIRST and the sibling second, the ONLY conflict
is that one block in `cmd/agent/main.go`; `internal/agent/session.go` auto-merges
(this branch touches `HasSessionEnd`, the sibling touches `DetectOrphanSessions` /
the constants block). Merging the sibling first instead produces a clean merge that
SILENTLY drops the gate, so the order matters — record it for the merger.

The resolution (keep the gate, take the sibling's two-value call inside it):

```go
if due, reason := agent.OrphanSweepDue(time.Now()); due {
    written, retracted := agent.BackfillOrphanCrashDumps(sessionIDOf(ag))
    switch {
    case written > 0 || retracted > 0:
        fmt.Fprintf(os.Stderr, "[gopi] crash-dump: backfilled %d session dump(s) with no recorded end, retracted %d stale dump(s)\n", written, retracted)
    case reason == agent.CrashDumpSweepReasonForced:
        fmt.Fprintf(os.Stderr, "[gopi] crash-dump: %s, no new orphan dumps\n", reason)
    }
    if err := agent.MarkOrphanSweepDone(time.Now()); err != nil { … }
}
```

One semantic consequence, deliberately taken: with the gate in place the
sibling's **retraction** also runs at most once per day rather than at every
start. That is the same reconciliation argument as for writing (a stale dump is a
ledger entry a human reads, not a hot-path product), and the explicit
`GOPI_CRASH_DUMP_SWEEP=force` trigger is the way to reconcile right now.

Verified on the combination (not assumed): `go build ./...` clean and the
affected tests + probe re-run on the merged tree.

**④ Combination with the sibling fix** — §Combination above (conflict map + the
exact resolution + the re-run on the merged tree).

## Overlap with a third worktree (found during coordination — read before merging)

`~/workspace/gopi-crashcost` (branch `fix/crashdump-scan-cost`, base `1c021f4c` = master)
holds an **uncommitted WIP** that solves the *same* half (a) of this task: it adds
`internal/agent/sessiontail.go` (`sessionTailRead`, same `sessionEndScanBackMax+1`
window) and splits the predicate out as `hasSessionEndBytes(data)` so a test can run
the old whole-file read and the new tail read over the same inputs.

That is an independent arrival at the same insight, with one deliberate difference
worth recording:

| | this branch | `fix/crashdump-scan-cost` (WIP, uncommitted) |
|---|---|---|
| window buffer | recycled from a `sync.Pool` | allocated per call |
| proof shape | counting `ReaderAt` seam → *bytes pulled off disk* is a number | old-vs-new read kept in the test binary → verdict **equivalence** input by input |
| gating | **yes** (the daily stamp + env trigger) | no |

They are alternatives, not complements: take ONE. Recommendation — this branch
(its proof pins the actual IO volume *and* it carries the gate that removes the
per-start walk); if the equivalence suite is preferred as the stronger semantic
proof, port `hasSessionEndBytes` + the equivalence test onto this branch — the
predicate split is orthogonal to the pool and the gate.

**⑤ Build + tests**

```
gofmt -l internal/agent cmd/agent internal/standards   → (no output)
go build ./...                                         → BUILD_OK
go test ./internal/agent/                              → ok  …/internal/agent  53.018s
go test ./internal/standards/                          → ok  …/internal/standards  9.579s
merged-tree run (this branch + the sibling fix):
  go build ./...                                       → COMBO_BUILD_OK
  go test -run 'TestStartupGate|TestOrphanSweepDue|TestMarkOrphanSweepDone|
                TestHasSessionEnd|TestDetectOrphanSessions|TestBackfillOrphan|
                TestOrphanLiveWindow|TestRecoverRunPanic' ./internal/agent/
                                                       → ok  …/internal/agent  0.912s
  go test -count=1 ./internal/agent/                   → ok  …/internal/agent  151.936s
```

The earlier whole-suite attempts did not complete (`internal/tools` alone ran past
the 900 s allowance on this host). It HAS now been run to completion on the merged
tree — `go test -count=1 ./...` → `FULLSUITE_EXIT=1`, with exactly four failing
packages, each cross-checked against master:

| failing package | test | on master? |
|---|---|---|
| `internal/stylegate` | `TestStyleGate` — `cmd/agent/cli_voice_server.go:162/166` bare `Close()` | **FAILS on master too** (the `voice` feature's own gap) |
| `cmd/agent` | `TestCLIContractInventoryMatchesDispatch` — 26 predicates, contract suite tracks 25 (the missing `voice` case) | **FAILS on master too** |
| `internal/tools` | `TestMemorySearchNonBlocking` — a FLAKE (1 failure in 10 runs on this branch AND 1 in 10 on master; not touched by this change: `git diff master -- internal/tools` is empty) | flaky on both |
| `internal/standards` | `TestStandardGates` / `TestStandardGateDataReport` — the Rule-16 permit line shifted with the code | **fixed in this branch** (`76593fba`); green on re-run |

Every other package is `ok`, `internal/agent` included (133 s).

**The standards gates caught this change (worth recording):** `go test
./internal/standards/` first reported
`Rule 16: STALE baseline entry cmd/agent/main.go:487` + a matching `NEW` hit.
The cause was mechanical — `errorWrapBaseline` pins a permit by LINE NUMBER and
the startup-gate edit above `readPayloadFile` shifted that call from `:487` to
`:506`. Re-baselined in `d7812506` with the reason recorded in the comment block;
the site itself is untouched. Also verified the sibling branch on its own base
(`79d6fe52` in a detached worktree) passes the same gates, so the red was this
change's line shift and not pre-existing breakage.

## Commit

- `132d2161` — `fix(crashdump): read only the session tail in HasSessionEnd`
- `fcbb4742` — `fix(crashdump): sweep orphan sessions at most once per UTC day`
- `4ada9a1c` — `docs(crashdump): startup-backfill cost report — tail read + daily gate, before/after measured`
- `d7812506` — `test(standards): re-baseline the shifted Rule-16 permit in cmd/agent/main.go`
- `f1a6f5c9` — `Merge master into fix/crash-dump-tail-read — gate keeps the sibling's (written, retracted) contract`
- `76593fba` — `test(standards): re-pin the Rule-16 permit to :509 after the gate-comment edit`
- (this report) `devdoc/crash-dump-startup-backfill-gate.md` — amended in `4ada9a1c`'s file

## Amendment (2026-09-12, after the sibling fix reached master)

Master now carries the sibling false-positive fix (`3b51fc00 Merge branch
'feat/fix-crashdump-false-positive'`), so the merge predicted in §Combination is
no longer a prediction — it is done, on this branch, as `f1a6f5c9`:

- The predicted SINGLE conflict materialised exactly (the startup call in
  `cmd/agent/main.go`); `internal/agent/session.go` auto-merged, as predicted.
- The resolution is the one recorded in §Combination, verbatim: keep the daily
  gate, call the sibling's `(written, retracted)` form inside it, keep the
  `reason == CrashDumpSweepReasonForced` arm.
- Merge order taken: **this branch first, master (sibling) second** — the order
  the §Combination note required, since the reverse order silently drops the
  gate.
- The Rule-16 baseline permit re-pinned to `cmd/agent/main.go:508` (the gate
  comment above `readPayloadFile` grew); `go test ./internal/standards/` green.
  The comment's line-shift story was corrected to match.
- A stale number in the gate comment ("~0.6 s") was replaced with the measured
  1.5–2 s pre-fix walk — the gate's own comment should not quote the shipped
  probe's first, cheapest reading as if it were the typical one.

Re-verified on the MERGED tree (not assumed):

```
go build ./...                                     → BUILD_OK
gofmt -l internal/agent cmd/agent internal/standards → (no output)
go test -count=1 -run 'TestStartupGate|TestOrphanSweepDue|TestMarkOrphanSweepDone|
      TestHasSessionEnd|TestDetectOrphanSessions|TestBackfillOrphan|
      TestOrphanLiveWindow|TestRetract|TestRecoverRunPanic|TestReconcile' ./internal/agent/
                                                   → ok  0.399s
go test -count=1 ./internal/agent/                 → ok  90.536s
go test -count=1 ./internal/standards/             → ok  4.562s
go run ./tmpbench   (merged tree, 30,113 entries)  → DetectOrphanSessions n=363
                                                     elapsed=687ms alloc-churn=23.8 MB
                                                     BackfillOrphanCrashDumps written=0 retracted=1
                                                     elapsed=1.958s alloc-churn=46.7 MB
                                                     heap=7.4 MB VmHWM=18,956 kB
```

(`retracted=1` on the merged tree is the sibling's reconciliation doing its job
on the live host — one dump for a session that has since ended. The gate does not
disturb it: `force` runs both halves now, a stamped day defers both, exactly the
semantic recorded in §Combination.)

**Known pre-existing red, not this branch's:** `TestCLIContractInventoryMatchesDispatch`
in `cmd/agent/` fails ("main.go has 26 … contract suite tracks 25") — the `voice`
subcommand landed without its contract case. Confirmed failing identically on
master (`~/work/GoPi`, 0.040s) with nothing from this branch applied. Same for
`TestStyleGate` (`cli_voice_server.go:162/166` bare `Close()`): it fails on
master, and `git diff master -- cmd/agent` shows this branch only touches
`cmd/agent/main.go`'s startup sweep call.

## End-to-end startup measurement (a real 16.4 GB tree, not a subset)

The probe above measures the sweep functions in isolation. To measure the thing
the bug report actually describes — a `gopi` START — a sandbox home was built by
HARD-LINKING the live tree (hard links share inodes, so 25,489 entries /
16.4 GB of JSONL cost ~309 MB of real disk; the sandbox was deleted afterwards):

```
elapsed (2 runs), sandbox = 25,489 JSONLs / 16.4 GB, LLM call unreachable by
design (the run ends in the same "LLM call failed" path for all three cases, so
the elapsed time IS the startup path):

  NEW binary, FRESH stamp  (walk skipped)      16.06 s / 15.42 s   maxrss 21 MB
  NEW binary, STALE stamp  (walk runs, tail)   17.38 s / 14.63 s   maxrss 26 MB
  OLD binary, base 79d6fe52 (walk + whole file) 36.48 s / 66.27 s  maxrss 111-152 MB

  same probes in isolation on that tree:
    OLD DetectOrphanSessions  40.155 s, 17,341 MB churn   ← the whole-file read
    NEW DetectOrphanSessions   1.208 s,     47.7 MB churn
    OLD BackfillOrphanCrash…  27.354 s, 16,858 MB churn, VmHWM 126,788 kB
    NEW BackfillOrphanCrash…   1.102 s,     40.5 MB churn, VmHWM  19,620 kB
```

Two things this pins down that the function-level probe cannot: (1) the win
scales with file SIZE — on a tree whose JSONLs are ~640 KB average (not the
4 KB-tail case) the whole-file read costs **17 GB of churn and 40 s**, and the
bounded read is flat at 1.2 s / 48 MB; (2) on a normal day the same start pays
**zero** of it, because the stamp is fresh and nothing is walked — which is why
the fresh-stamp and stale-stamp New-binary numbers are within noise of each
other while the old binary is 2–4× slower.

Absence-of-sweep at the binary level is proven by a separate, isolated number
rather than by these four starts: with the workspace probe home (uncached
metadata), the same binary goes 0.03 s with no stamp to 0.01 s with a fresh
stamp — the gate's own E2E test asserts the observable product (0 dumps on a
stamped day, a dump on a stale one) and is the durable guard.


