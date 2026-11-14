<!-- doc-class: historical -->
# Task Report: the orphan-crash-dump sweep gates on tree CHANGE, not a calendar day

Branch `feat/crashdump-sweep-incremental` · worktree `~/workspace/gopi-sweepinc` ·
commit `f59d72f5` · task row `01a091ff-9bc9-7b3c-aa0b-79269a4d5379`.

Predecessors, both on this branch's base: the IO half
(`devdoc/crashdump-scan-cost.md`, bounded tail read) and the day-key gate
(`fcbb4742`, `devdoc/crash-dump-startup-backfill-gate.md`).

## Completed

- **The day-key trust decision is gone.** `OrphanSweepDue` / `MarkOrphanSweepDone`
  now key on a **change-sensitive marker** (`crash-dump-sweep.marker`) instead
  of `crash-dump-sweep.stamp`. The gate has three independent layers, in
  decision order:
  1. **floor** — `crashDumpSweepFullWalkInterval` (1 h): a full walk is DUE
     whenever the last completed walk is older than the floor, whatever the
     marker claims. This is the self-heal for every stale-marker path.
  2. **budget** — `crashDumpSweepBudgetSec` (60 s): a start inside the window of
     a completed walk trusts that walk instead of paying for its own; N starts
     inside the window cost one walk.
  3. **change probe** — `treeChangeProbe`: a bare `stat` of the sessions root
     refuses the skip as soon as the root's name set changed, so a session
     directory (or flat JSONL) appearing after the marker forces the next walk.
- **The headline acceptance is satisfied and tested end to end**:
  `TestStartupGate_SkipsOnlyWhenTheMarkerProvesNoChange` plants a session
  orphaned *after* the marker (no re-stamp, no env override, no timer) and
  asserts the NEXT start dumps it. A day-key stamp could not do this — it
  skipped the whole day.
- **Every uncertainty fails OPEN to sweeping** (Rule 15): absent / unreadable /
  truncated / unparseable / future-dated / foreign marker, an unbuildable
  probe, and an unstattable root all report DUE with a specific reason. A
  marker that cannot be written returns its error (the startup path already
  reports it); writes are atomic (temp + fsync + chmod + rename), so "crash
  before the marker write" leaves the OLD marker rather than a truncated one.
- **The classification predicate is untouched** — verified, not asserted:
  `git diff HEAD~1 HEAD -- internal/agent/session.go
  internal/agent/orphanclassify.go internal/agent/crashdump.go` is empty. The
  commit touches only `crashdump_sweep_gate.go` and its test.

## Design decision (with measurements, and the rejected candidates)

Measured on the reference tree the same day, same host, interleaved rounds
(47,893 root entries / 45,889 task dirs / 1,521 session dirs / 95,669
sub-entries / 1,673 JSONL / 827 dumps):

| probe | wall | churn |
|---|---|---|
| `DetectOrphanSessions` (the walk) | 1194–1765 ms | 41.7 MB |
| 2× ReadDir + `Info()` per JSONL | 1121–1655 ms | 33.3 MB |
| newest-JSONL-mtime scan | 1025–1738 ms | 34.7 MB |
| `ReadDir(root)` alone | 98 ms | 9.7 MB |
| root + 1 level ReadDir | 678 ms | 32.3 MB |
| `stat(root)` | ~0 ms | — |

1. **CHOSEN — change-sensitive marker + bounded-cadence floor.** The probe is a
   `stat` (~0 ms), not a scan. Relying on a *scan-strong* probe is the trap:
   rows 2 and 3 show that a fingerprint strong enough to notice a write inside
   an existing session dir **costs what it was meant to avoid** — it needs the
   same ReadDir pass as the walk. A busy host makes it worse: the newest JSONL
   write was 1–2 s old, so an "unchanged" verdict is almost never available.
2. **REJECTED — fingerprint every JSONL's mtime.** Measured above: no saving,
   plus a second way to be wrong. The budget ledger is the honest answer to a
   busy host (one walk per window), not a bigger probe.
3. **REJECTED — folding into the resident merger tick.** It is the one component
   that runs when no user start happens, which is why it was considered. But
   the sweep's exclusion ("not the CURRENT process's session") and its
   reporting line both belong to the STARTING process, and its product is worth
   as much written a minute later. Moving it re-homes the trigger without
   making it change-proportional, and it has no production entry point for this
   work: `retireStaleOrphanDumps` (crashdump.go:416, added by `b33dda6e`) has
   **no caller anywhere** in `internal/` or `cmd/` — `devdoc/crash-dump-classify-liveness.md:108`
   claims it "runs"; it does not. The tick would have to grow a second, unowned
   walk.
4. **REJECTED — readdir-name pruning / chunked root read.** Aliasing the two
   root levels into one pass (one `ReadDir(root)` + one per session dir) cuts
   the root round-trips but still walks every subdir: it removes ~98 ms of a
   ~1 200 ms walk while adding a second tree-shape contract that must not drift
   from `DetectOrphanSessions`. It optimises the walk, and the walk is not what
   a warm start should be paying at all.

## The change probe's reach (measured, not assumed)

Probed on this host (ext4, 5.15) with ≥1.2 s separation per step:

- `mkdir <root>/child` → the root's ctime/mtime DOES change, but ext4 publishes
  it **lazily**: a stat taken immediately after can still read the old pair
  (visible within ~100 ms). Hence `crashDumpSweepProbeSettle` (150 ms) on the
  writing side; the reader needs no wait, since a mismatch only means "walk".
- create / append / remove **inside** an existing session dir
  (`<root>/<id>/session.jsonl`) → moves the JSONL's own timestamps and (for
  create/remove) the session dir's, but **does not reach the root at all**.
  So a session merely continuing to write in its own directory is invisible to
  the probe. That case is bounded by the floor, and it is rarely a crash: a
  still-writing session classifies `OrphanLive`, and a session whose writer died
  has stopped writing.
- The root's **parent** is deliberately NOT probed: the marker file lives in
  the data root, i.e. in that parent, so renaming the marker temp into place
  moves the parent's ctime — probing it would make every marker write
  invalidate its own probe and no start would ever be trusted.
- `stat(root)` is ~0 ms even at 47,893 entries, so the fast path is genuinely
  free.

## Fail-closed analysis (every stale-marker path)

| path | mechanism | degradation |
|---|---|---|
| crash before the marker write | atomic temp+fsync+rename ⇒ the OLD marker survives | old `swept_at`; the change probe sees the sweep's own writes; **floor** bounds it to 1 h |
| crash mid-write | ditto — a truncated marker is never installed | same as above |
| marker truncated by hand / partial write | reader errors (`missing probe`, `probe field` parse) | **DUE** (walk) |
| unreadable marker | `os.Open` error (incl. a directory at the path) | **DUE** |
| marker of a future/unknown version | version compare | **DUE** |
| clock skew (marker dated in the future) | `SweptAt.After(now)` | **DUE** — never lets a bad clock disable the sweep |
| clock skew (host clock jumps back) | `now.Sub(SweptAt)` becomes small | skip for at most the floor window, then DUE |
| out-of-band removal of a session dir at the root | root ctime/mtime moves | **DUE** (walk) |
| out-of-band rename at the root | root ctime/mtime moves (measured) | **DUE** |
| write inside an existing session dir | NOT visible to the probe | bounded by the **floor** (≤1 h), the case the probe cannot see |
| wrong marker (copied from another data root) | `RootPath` compare | **DUE** (Foreign) |
| marker with a mismatched probe shape | length compare in `sameTreeChangeProbe` | **DUE** |
| marker write fails (read-only root, perms) | error returned to the caller | reported on stderr; next start walks |
| marker dir missing | `MkdirAll` on write, DUE on read | **DUE** |

No path yields a silent skip: the only skip is a marker that was read, parsed,
trusted, path-matched, in the budget window, and whose root evidence still
matches — and even that is capped by the floor.

## Not Done (honest)

- **The subdir-write case is bounded, not instant.** A session that dies after
  writing inside an existing session dir is not noticed by the change probe; it
  is found by the next floor-expired walk (≤1 h). Detecting it instantly would
  require the ReadDir-per-JSONL fingerprint that the measurements above reject
  as a no-op — the honest trade is bounded latency, and the report says so
  rather than implying an instant detection the design does not have.
- **`crashDumpSweepProbeSettle` is a wall-clock sleep on the startup path.**
  It is 150 ms, paid once per FLOOR-expired walk (not per start), so it does not
  affect the warm-start path the epic is about — but it is a real sleep, and it
  exists only because ext4 publishes parent timestamps lazily. A filesystem
  with immediate publication would need 0 ms.
- **`retireStaleOrphanDumps` remains dead code** (no caller). I did NOT delete
  it — it is outside this task's scope (dump reconciliation belongs to the
  false-positive sibling) and removing it would change the file the sibling
  owns. It is recorded here so the next reader does not believe the devdoc line
  that claims it runs.
- **`reconcileStaleOrphanDumps` still walks the whole tree on every executed
  sweep** (it is the retraction half of `BackfillOrphanCrashDumps`). That is
  unchanged: when the gate says DUE, the sweep pays two ReadDir passes, not
  one. Making retraction incremental is a separate change.
- **`make test`'s outcome is recorded below**; the two end-to-end startup tests
  skip themselves when `bin/gopi` is absent (their documented precondition),
  which is why the binary is built as part of the verification.
- One measurement accident, reported for the record: an early probe APPENDED a
  synthetic line into the real host tree (`~/.gopi/sessions/f417a45d-.../session.jsonl`)
  while testing that an append moves ctime; that file was deleted immediately
  afterwards (its directory, holding only `metadata.json`, remains). All later
  probes ran on `os.MkdirTemp` sandboxes. It is a probe-hygiene error, not a
  product defect.

## Verification

- Build: `CGO_ENABLED=0 go build ./internal/agent/` → OK; full binary
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (20,974,513 bytes).
- `gofmt -l` on both touched files → clean; `go vet ./internal/agent/` → clean.
- Pure gate tests: `go test ./internal/agent/ -run
  'TestOrphanSweepDue|TestMarkOrphanSweepDone|TestGateConstants' -count=1` →
  ok. Covers the verdict table (no-marker / fresh / changed / floor / force /
  off), all seven broken-marker subtests, the unstattable root, the
  unwritable-marker error and the Rule-13 literal scan.
- **End-to-end (the headline acceptance)**:
  `go test ./internal/agent/ -run 'TestStartupGate' -count=1 -v` →
  `--- PASS: TestStartupGate_SkipsOnlyWhenTheMarkerProvesNoChange (0.70s)` and
  `--- PASS: TestStartupGate_SkipsTheWalkWhileMarkerFresh (0.66s)`.
  The first is the epic's acceptance: a session orphaned after the marker is
  dumped by the next start; the second proves a warm start writes no dump at
  all and that a floor-expired marker re-stamps.
- **Predicate untouched**: `git diff HEAD~1 HEAD -- internal/agent/session.go
  internal/agent/orphanclassify.go internal/agent/crashdump.go` → empty.
- **Before/after on the reference tree** (`/home/user/.gopi/sessions`), same
  method as `devdoc/crashdump-scan-cost.md` (rchar from `/proc/self/io`,
  `runtime.MemStats` TotalAlloc delta, wall-clock + spread):

  | | wall | churn | rchar |
  |---|---|---|---|
  | BEFORE — full walk per start (and the day-key gate's once-a-day walk) | 470.3–472.4 ms | 41.7 MB | 5.5 MB |
  | AFTER — the gate's verdict on a warm start (5 runs) | 0.013–0.027 ms | 0.003 MB | 0.000 MB |

  Verdicts on the real tree: **103 orphans** on every pass (identical across
  runs; the tree grows between sessions, which is why the count is quoted with
  the run rather than asserted as a constant in a test).
- **`make test`**: exit 1 — and the failures are NOT from this change. Two
  gates in `internal/stylegate` flagged THIS change on the first `make test`
  run and are now fixed:
  - `unchecked-close` `crashdump_sweep_gate.go:446,451` — the two error-path
    `tmp.Close()` calls discarded their error. Existing behaviour, but a
    regression under the project's own swept rule, so both now record the
    close failure on stderr (the primary write/sync error is still the one
    returned) and the rule's hit count for the file is 0.
  - `test-sleep` `crashdump_sweep_gate_test.go:162` — a fixed
    `time.Sleep(2 * crashDumpSweepProbeSettle)` waiting for ext4's lazy
    parent-timestamp publication. Replaced by `awaitTreeChangeVisible`, a
    bounded poll (5 s deadline) that asserts the PROPERTY ("a namespace change
    becomes visible to the probe") instead of the latency of one filesystem;
    its remaining 5 ms inter-iteration wait carries the rule's documented
    `//stylegate:allow-sleep` reason.
  After those fixes, `grep -c crashdump_sweep_gate` over the stylegate output is
  **0**. The residual stylegate failures (test-sleep 6, test-skip 1,
  ignored-error 190, ceiling 182) reproduce IDENTICALLY on an unmodified
  `master` checkout, i.e. they belong to other agents' in-flight work, not to
  this branch. `internal/testutil`'s `TestWorkerOwnershipPointerPresent` and
  `TestScriptsAreBoundedAndOffTmp` likewise fail on files this change never
  touched (`devdoc/zombie-auto-dispose-2026-09-12.md`,
  `scripts/acceptance-zombie-auto-dispose.sh`, the merger devdocs).
- Commit: `f59d72f5` on `feat/crashdump-sweep-incremental`, two files
  (`internal/agent/crashdump_sweep_gate.go`,
  `internal/agent/crashdump_sweep_gate_test.go`), probe dirs removed and
  confirmed absent from the commit.

## Verification recipe (reuse — do not re-derive)

- Probe binaries live in an untracked `./.probe*/` dir inside the worktree and
  are deleted before commit (`git -C <wt> clean -fd -- .probe6`). Because the
  sandbox pins the shell's working directory, drive git in the worktree with
  `git -C /home/user/workspace/gopi-sweepinc …` from the main repo.
- To measure the STARTUP decision, call `agent.OrphanSweepDue(time.Now())`
  directly and `runtime.MemStats` around it; to measure the walk, call
  `agent.DetectOrphanSessions()`. Take `rchar` from `/proc/self/io` — it is the
  kernel's truth and is unaffected by in-process seams.
- **Filesystem-timestamp claims need ≥1 s separation between the operation and
  the stat.** Two of my earliest probes concluded "ctime never moves" because
  the op and the stat landed in the same timestamp tick, and one concluded
  "mkdir does not move the parent" because of ext4's ~100 ms lazy publication.
  Separate the steps, and assert the settle window explicitly.
- The end-to-end startup tests need `bin/gopi` present
  (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`); without it they SKIP,
  which looks like a pass — build the binary before trusting a green run.
