<!-- doc-class: normative -->
# The scratch tree's resident sweep (Rule 21, no new daemon)

## The defect

The scratch home (`$HOME/workspace/gopi-test-scratch`) was bounded by a sweep
that ran **inline in the test pin** (`internal/testutil.PinSuiteTempRoot`). The
one hook meant to cover a host that stops running suites,
`GOPI_TEST_SCRATCH_GC_MIN`, was **declared and read by nothing** — not in
production, not in a test. A hook with no consumer is worse than a missing
feature: the help text, the devdoc and the constant all claim the sweep is
running, so an operator who sets it believes the tree is bounded.

Two consequences, both measured elsewhere in the repo's devdocs:

- the tree grows exactly as fast as suites are KILLED (the same unbounded-growth
  mechanism as the `/tmp` 13,044-directory incident);
- an unread knob is invisible to every existing gate — Rules 1–20 have nothing
  to say about a constant whose value never reaches the environment layer.

## Design

**Zero new resident processes.** The sweep rides the ticks of the two components
that are ALREADY resident:

| component | call site | face |
|---|---|---|
| `gopi task worker` | `cmd/agent/cli_tasks_worker.go:160` | long-running face only (`--once` never sweeps) |
| `gopi task merger` | `cmd/agent/cli_tasks.go:1925` | pass tick (the resident half that survives a client-only deployment) |

The wrapper is `cmd/agent/scratch_gc_tick.go`. `--once` faces deliberately do
not sweep: they are the unit/E2E seam and a test binary must never sweep the
developer's real scratch tree (Rule 6).

**Cadence.** `GOPI_TEST_SCRATCH_GC_MIN` (minutes), semantics reused verbatim
from the env that already documented "every N minutes". Default
`scratchGCDefaultIntervalMin = 60`, chosen against the two clocks that matter:

- `scratch.StaleAge` is 6h, so an hourly cadence is an order of magnitude finer
  — steady-state scratch is bounded by ~1h of abandonment, not ~6h;
- the ticks themselves already run at 5min (merger `--every`) / 10min
  (`workerLifecycleThrottleMin`), so hourly is the *coarsest useful* cadence:
  one `ReadDir` of a directory holding tens of entries, next to a tick that
  already merges branches or drains tasks.

**Off switch, two faces, one knob.** `GOPI_TEST_SCRATCH_GC_MIN=0` (or a negative
value), or settings.json/CLI `"scratch_gc": false` / `--scratch-gc=off`.
`config.ScratchGC` is the durable switch; `config.ApplyEnv` publishes OFF as
that same env, so there is exactly ONE reader of the knob (Rule 2). The switch
lives in `cmd/agent`, not in the shared config package: `internal/config` must
stay importing leaf packages only, and the knob's reader is the tick.

**Fail-open.** Every failure path (no home, unreadable entry, held lock,
unreadable `/proc`) is logged and the tick continues — a disk-hygiene sweep must
never be able to fail the host task. Worst acceptable outcome: stale scratch
survived one more tick.

**Placement.** The sweep core moved from `internal/testutil` to
`internal/scratch` because `internal/testutil` imports `"testing"`: a production
import of it drags the test framework into the shipped binary. `internal/scratch`
is the leaf package (Rule 14) both layers can import. `internal/testutil` keeps
its suite-facing API by delegating, so every existing caller and test is
unchanged.

**Namespace-limited by construction** (it can never become a global delete): it
walks only the scratch home; collects only names in this system's namespace
(`suite-…`, the legacy `gopi-suite-tmp-…`, `queue-…`); requires age **and** no
live holder; never recurses.

## Verification

`go build ./...` OK; `go test -count=1 ./...` green except the pre-existing
`internal/tools` load flake diagnosed below (A/B measured, not assumed); `go
test ./internal/scratch/ ./internal/testutil/
./internal/standards/ ./internal/config/ ./cmd/agent/` green.

### Live fixtures (before/after counts)

| fixture | before | removed | after |
|---|---|---|---|
| stale, unheld root (`suite-ghost-run-1`) | 1 | 1 | 0 |
| fresh root (1min old) | 1 | 0 | 1 |
| LIVE-held root (real `sleep` process, cwd inside) | 1 | 0 | 1 |
| … same root, after the holder exits | 1 | 1 | 0 |
| foreign namespace (`someone-elses-data`, `gopi-notes`, `tmp-build`, `suite`, `queue`) | 5 | 0 | 5 |

The held-root fixture is the load-bearing one: the root is collected only after
the holder exits, which proves the earlier pass was protected by the **holder**,
not by an age or name accident.

### Fail-open

- non-directory home → error returned, **0 removed**, no panic;
- missing home → no-op, no error (the expected non-event on a host that never
  ran a suite);
- a forced GC error is reported through `fmt.Fprintf(os.Stderr, …)` and the
  tick continues (host task unaffected).

### Gate 6 (Rule 21) — negative-tested

`TestGate6EnvReaderRedProof` runs the **registered gate** (not a copy) against
synthetic sources: an unread hook is reported; `os.Getenv(constant)` is silent;
a helper funnel (`boolEnvTrue(constant)`) is silent; a following statement's
identifiers are **not** readers; the `// env-reader-exempt:` marker above a hit
classifies as escaped. `TestGate6EnvReaderFindsTheRealTree` runs the predicate
over the whole tree and fails if the reader index is empty (a green-but-blind
scan), then requires zero remaining hits — it is red on the unread hook and
green after the wiring.

The gate found two real defects while being written, both fixed in this branch:

- `internal/agent/taskheartbeat.go` — a second spelling of
  `GOPI_TASK_STALE_SEC` with no reader (its single reader,
  `config.ResolveTaskGracePeriods`, owns the name constants); the duplicate
  constant is gone.
- `internal/hippocampus/config.go` — `hippocampusEnvPrefix` is a namespace, not
  a knob; marked `// env-reader-exempt:` with the reason.

### Live end-to-end (real binary, real worker loop)

Built with `CGO_ENABLED=0 go build -o /tmp/gopi-scratchgc ./cmd/agent/` and run
as `GOPI_TEST_SCRATCH_HOME=<sandbox>/scratch GOPI_WORKER_MAX_ROUNDS=1 … gopi
worker` against a sandbox home holding three fixtures:

```
BEFORE:  suite-ghost-run-77   (8h, unheld, our namespace)
         suite-live-run-88    (8h, cwd held by a real `sleep 300`)
         someone-elses-data   (8h, foreign namespace)

run 1 (holder alive):
  [worker] [scratch-gc] removed 1 stale scratch entry older than 6h0m0s from …
  AFTER: suite-live-run-88, someone-elses-data      ← held root KEPT, foreign KEPT

run 2 (holder exited):
  [worker] [scratch-gc] removed 1 stale scratch entry …
  AFTER: someone-elses-data                         ← the same root now collected
```

The off switches were exercised the same way: `GOPI_TEST_SCRATCH_GC_MIN=0` →
zero sweep log lines, fixture survived; `{"scratch_gc": false}` in the sandbox
settings (`GOPI_SETTINGS=<sandbox>`) → zero sweep log lines, fixture survived;
the same file with `"scratch_gc": true` → the sweep ran and collected it. The
settings face is read through `config.ResolveSettingsPath()`, so it follows
`--settings`/`GOPI_SETTINGS`.

### Suite pin

`internal/scratch` now has a `TestMain` (`testmain_scratchroot_test.go`) routing
its scratch through `testutil.RunSuiteMain`, as the repo's pin gate
(`TestEveryTempUsingSuiteIsPinned` / `TestPinCoverageTable`) requires of any
package whose tests create scratch directories — without it the fixtures would
land on the ambient temp root, the exact defect the /tmp ban exists for.

### One pre-existing flake, measured A/B rather than assumed

On this host a full-suite run went red once in `TestMemorySearchNonBlocking`
(`internal/tools/memory_test.go` — "concurrent store: … vtable constructor
failed: distilled_qa_fts" / "concurrent entries not searchable"). That package
is **untouched by this branch** (`git diff --stat master...HEAD -- internal/tools/`
is empty), but "untouched" is not a measurement, so the flake was measured
against master directly — both sides built `go test -c`, both binaries run
alternately 60× under the same synthetic `dd conv=fsync` load:

| binary | iterations | failures |
|---|---|---|
| `master` (`0cd8a90`) | 60 | 4 |
| this branch | 60 | 4 |

Two distinct error texts (FTS vtable-constructor failure under a concurrent
store; a store that then does not become searchable) on BOTH sides ⇒ a
pre-existing load-dependent flake in that test, not a regression from this
branch. The load loops were killed and their `/tmp` files removed afterwards
(disk back to its prior 85% / 22G free).

## Not done

- The sweep is not exposed as a CLI verb (`gopi scratch gc`); the pin and the
  two resident ticks are the triggers. A manual trigger is a separate, smaller
  change and was not required by the acceptance criteria.
- `GOPI_TEST_SCRATCH_GC_MIN`'s historical "watchdog" helper in
  `scripts/setup-scratch-home.sh` was not re-plumbed: with the resident tick
  reading the same env, a watchdog timer is no longer needed (and a systemd
  timer would violate the no-new-daemon constraint).

## Docs updated alongside (rule-file parity)

A new gate that exists only in `internal/standards/` is invisible to the agents
the rule is addressed to, so this branch carries the rule text too:

- `GOPI.md` — the section heading is now **Rules 16–21**, "run all five" →
  "run all six", and a **Rule 21** subsection landed in the same shape as
  Rules 16–20 (BAD/GOOD snippet, "How to verify" with both test names, EMPTY
  baseline, and the deliberately rejected predicate shape).
- `devdoc/standards-gate.md` — the landed-gates table gained the Rule 21 row,
  the addendum records this gate's numbers and the two real defects it found,
  and the count references were updated to six.
- `devdoc/coding-standards-checklist.md` §16.1 — a note that gate 6 comes from
  GoPi's own rule 1b, not from the Google corpus, so the A/B status column of
  that table stays honest.

## Resident processes added: **0**
