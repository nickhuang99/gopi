<!-- doc-class: normative -->
# Task Report: the scratch GC stale window is configurable (row 01a0a4c4-c26a-726f-a32f-72b171194741)

## Completed

- **The knob exists, in one place each.** `internal/scratch.StaleAge` (a bare
  `6 * time.Hour` in `gc.go` whose own comment claimed "longer than any healthy
  suite run") became `internal/scratch.DefaultStaleAge` — a documented DEFAULT —
  plus three names and ONE reader:
  - `scratch.GCStaleAgeEnvName` (`GOPI_TEST_SCRATCH_STALE_MIN`),
  - `scratch.GCStaleAgeSettingsKey` (`scratch_gc_stale_min`),
  - `config.ScratchGCStaleMinFlagName` (`--scratch-gc-stale-min`, catalog row
    carrying all three faces so `gopi --help-env` advertises them),
  - `scratch.ResolveStaleAge()` — the ONE reader (Rule 2), in the leaf package
    because `cmd/agent`, `internal/testutil` and the dry-run/base/suite faces all
    consume it. Every sweep face now calls it instead of the constant.
- **One publisher** for the declaring channels: `config.PublishScratchGCStaleMin`
  (called pre-subcommand-chain from `tmpDirBootstrapFromArgs`, the same moment
  tmpdir is published) applies the project's order — flag > caller's exported env
  > settings.json > default — and writes the knob's single variable. The flag
  channel additionally rides `config.BootstrapEnvFromArgs` (which already hoists
  catalog flags before the subcommand chain), so `gopi --scratch-gc-stale-min 90
  merger`, `gopi task worker` and the one-shot verb all see it.
- **A bad value is refused, never defaulted silently**, through ONE predicate
  (`config.ValidateScratchGCStaleMinEnv`, used by the agent path's flag parser and
  by the verb). `0`/negative/garbage is reported with the off switch named — a
  threshold that can never be exceeded would reap NOTHING, which is the defect
  this knob fixes; switching the sweep off stays the cadence knob's job.
- **Docs**: the `scratch-gc` usage text documents the flag, the env var and the
  settings key; `DefaultStaleAge`'s comment carries the measurements below.
- **Tests** (all new):
  - `internal/scratch/gc_stale_age_test.go`: env faces + the behavioural repro
    (a 90-minute-old entry survives the default window and is collected under a
    30-minute override) + the non-regression guard that a HELD entry is never
    collected however small the window (holder = a foreign live process, since
    the sweep deliberately exempts itself).
  - `internal/config/scratch_stale_min_test.go`: catalog row identity (env/key/
    flag agree), flag parsing, refusal of unusable values, and the publisher's
    precedence — including that a caller's own export is never clobbered and that
    the flag outranks it.
  - `cmd/agent/scratch_gc_stale_window_test.go`: the REAL binary end to end —
    flag, env face, the default control, and the refusal (non-zero exit, tree
    untouched).

## Not Done

- **The resident tick's cadence/off switch was NOT touched.** That knob
  (`GCIntervalEnvName`, the settings `scratch_gc`) already existed and is out of
  this row's scope; the new knob deliberately does not duplicate its off switch.
- **The measured window is still the DEFAULT of 6h.** The knob makes the policy
  changeable; choosing a different default is an operator decision, and this task
  did not take it. The measurements below are the argument for whoever takes it.
- **No `internal/testutil` suite-level test** for the alias change
  (`ScratchStaleAge` → the default, `GCStaleScratchRoot` → the resolved window):
  that layer is exercised by the resident tick's existing tests, and its callers
  are covered by the repro above. Flagged here rather than claimed.
- **`internal/config/env.go`'s settings face was deliberately left out**: see the
  note in the Evidence section — routing the settings value through `ApplyEnv`
  would have INVERTED the documented order (it would have become
  indistinguishable from an operator export). The publisher resolves settings
  itself instead.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → exit 0 (also `go build -o /dev/null ./cmd/agent/`).
- Tests: `go test ./internal/scratch/ ./internal/config/ ./internal/standards/ -count=1` → ok (all three packages).
  `go test ./cmd/agent/ -run TestScratchGCStaleWindow -count=1` → ok.
  `go test ./internal/scratch/ -run TestResolveStaleAge -count=1` → ok.
- `gofmt -l` on every touched package → empty. `go vet ./cmd/agent/` → clean.
- Commit: see the close-out message (branch `fix/scratch-gc-stale-window`).

## Evidence (self-filed)

**1. The pre-change behaviour, observed (red-proof).** The same regression test,
written against the pre-change API in a detached worktree of the unmodified tree
(`git worktree add --detach … HEAD`):

```
$ go test ./internal/scratch/ -run TestZRedProofStaleWindowOverride -count=1
--- FAIL: TestZRedProofStaleWindowOverride (0.00s)
    ztmp_redproof_test.go:31: RED PROOF: the configured 30m window did not reach
    the sweep (removed=0, hardcoded StaleAge=6h0m0s)
```
i.e. on master the knob is not merely unread — the pre-change tree does not even
have the names. The pre-change BINARY, given the same override, ignores it:

```
$ GOPI_TEST_SCRATCH_HOME=/…/redproof/home GOPI_TEST_SCRATCH_STALE_MIN=30 \
    ./gopi-pre-staleage scratch-gc
scratch-gc role=cli home=/…/redproof/home removed=0 stale=6h0m0s disabled=false
$ ls /…/redproof/home
suite-staleage-redproof-run-1          # 2h old: survived a 30m override
```

**2. The post-change behaviour, observed (real binary, three channels).** One
fixture per face: an in-namespace entry aged 2 hours (120m), i.e. between a
30-minute window and the 6h default.

```
default:                      scratch-gc … removed=0 stale=6h0m0s   -> SURVIVED (120m < 6h)
GOPI_TEST_SCRATCH_STALE_MIN=30: … removed=1 stale=30m0s             -> REMOVED (120m > 30m)
--scratch-gc-stale-min 30:      … removed=1 stale=30m0s             -> REMOVED (120m > 30m)
{"scratch_gc_stale_min":30}:    … removed=1 stale=30m0s             -> REMOVED (120m > 30m)
```
The verb's own `stale=` field naming the window, together with the entry's fate,
is the observation: the report and the effect agree.

**3. The measurements behind the mechanism decision** (the task offered
"parameterise the window" vs "replace it with a measured budget"; the choice is
parameterisation, argued from these, taken on the live scratch home
`/home/user/workspace/gopi-test-scratch`, 2026-09-18, with a temporary probe that
was deleted before close-out — same predicates as `SweepHomePlan`, i.e. holder
vetoes applied):

```
WINDOW 30m    reclaimable=175 held= 3 bytes=5667.7M   registered=172 unregistered=3
WINDOW 1h     reclaimable=151 held= 1 bytes=5342.1M   registered=149 unregistered=2
WINDOW 2h     reclaimable=128 held= 1 bytes=3741.5M   registered=126 unregistered=2
WINDOW 6h     reclaimable=  0 held= 0 bytes=   0.0M   (the code's own window)
QUEUE children=618 youngest=-7s
```

What this says, and why it settles the mechanism:

- At the code's own 6h window the sweep reclaims **0 entries / 0 bytes** while
  5.5 GB sits unheld and only 30-120 minutes old. The stale window — the POLICY —
  is what was leaving the tree full, not the mechanism, not the holder rule
  (only 1-3 entries per window are held).
- 172 of the 175 entries are `.holder`-registered, so the sweep's per-entry
  liveness registration is doing the veto work; a bytes/entries BUDGET would have
  to delete exactly those registered entries to free the space it was measured
  for, i.e. the budget mechanism buys nothing here and costs the safety arm.
- The distribution is a long tail of finished suites, and a queue whose children
  are refreshed by their live producers (`youngest=-7s`): the queue is already
  sized by its producers' lifetimes, while the *home* root is not. Both point at
  the same instrument: an operator-settable age, not a byte target.

**4. The counters that remained green throughout.** `go test ./internal/standards/`
passes with the change — in particular the Rule 21 env-reader gate
(`TestGate6EnvReaderFindsTheRealTree`) accepts `GOPI_TEST_SCRATCH_STALE_MIN` as
read, because its reader (`scratch.ResolveStaleAge`) is in the argument list
rather than in a doc comment.

**5. `git status --porcelain`** at close-out: empty (see the close-out message's
own output; the probe files and the two `/home/user/workspace/_tmp` scratch trees
lived outside this worktree).
