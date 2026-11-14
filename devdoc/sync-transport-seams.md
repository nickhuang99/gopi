<!-- doc-class: historical -->
# Task Report: peer-sync transport seams — anchor guard + claim-lock wait

Branch: `feat/sync-transport-seams` (worktree `~/workspace/gopi-sync-seams`)

## Completed

Two peer-sync transport legs that each carry a protocol invariant were
unassertable offline, so the invariants were unguarded. Both now use the
package's established seam pattern, and the real paths are asserted
in-process.

1. **Anchor-guard remote-half seam** — `var reconRemotePendingSetHook
   func(peer string) (int, error)` in `internal/tools/memory_sync_reconcile.go`
   (nil → the production ssh exec; same doc/pattern as
   `syncSSHExchangeHook` in `memory_sync_session.go`). `reconRemotePendingSet`
   consults it first.
2. **`claimLockWait` const → var** — `internal/tools/memory_sync_claim.go`:
   the round-bound math moved out of the `const` block into a documented
   package `var` with the **production default unchanged**
   (`syncSSHTimeout + syncSSHConnectTimeoutSeconds + claimLockRoundSlack`),
   exactly the sibling `claimLockTimeout` precedent.
3. **Real anchor-guard assertions** (`memory_sync_reconcile_cov_test.go`),
   via the seam — no `GOPI_ANCHOR_GUARD=0` needed:
   - `TestSyncAnchorGuardEqualPendingSetPasses` — local pending (2, read from
     the pinned ledger; a `completed` row correctly excluded) == peer 2 →
     the anchor advances; the guard asks the resolved peer exactly once.
   - `TestSyncAnchorGuardRefusesOnMismatchWithRepairHint` — local 2 vs peer 3
     → refuses, and the error names both sides **and** the two-step repair
     (`gopi task diff --peer X` then `gopi task sync --full X`).
   - `TestSyncAnchorGuardWrapsUnreadablePeerTransportError` — a failing peer
     read → fail closed, "anchor guard" + the transport detail wrapped, and
     **no** repair hint (a diff cannot run against an unreachable peer).
   - `TestSyncAnchorGuardFailClosedOnUncomparablePeer` (existing) now also
     asserts the hook defaults to nil and keeps the live ssh leg.
4. **RETRY-leg assertion** (`memory_sync_claim_cov_test.go`):
   `TestWaitClaimLockVerdictTimesOutToRetry` shortens `claimLockWait` to 30ms
   and asserts expiry → `ClaimLockRetry` with reason
   "lock round wait exceeded", no round/lines on the synthesized verdict, and
   `elapsed < 2s` (measured ~0.03s). It also asserts the production default
   value and restores it. `TestWaitClaimLockVerdictShortenedWaitStillReadsARound`
   is the boundary: a shortened wait still returns an arriving verdict
   verbatim, so the retry test cannot pass by breaking the read.
5. **Rule 1b cleanup on the touched path** — the `GOPI_ANCHOR_GUARD` literal
   was typed at the guard's use site and in 4 test files; it now comes from
   the named const `gopiAnchorGuardEnv` (defined once, referenced 6×).
6. The two devdocs that filed this testability gap were corrected in place:
   `devdoc/cov-tools-sync-reconcile.md` gap 2 and `devdoc/cov-tools-sync-claim.md`
   gap 1 are marked CLOSED with the fix + test names.

## Not Done

- `reconRemoteRowSets` (the `TaskDiffPeer` row-catalog transport) still has  no seam — out of scope for this epic (the anchor guard needed only the
  pending-count leg). `TaskDiffPeer`'s happy path stays asserted by folding
  the real local half against an injected peer catalog
  (`devdoc/cov-tools-sync-reconcile.md` gap 1).
- `reconRemotePendingSet`'s own JSON parsing (empty output → 0 rows,
  unparsable → error) is still ssh-only: the hook short-circuits before the
  parse, so that branch remains unreachable in-process (`gap 3`).
- The ALOHA backoff sleep path (`RunClaimLockProtocol` round > 1) remains
  undriven for real wall-clock reasons (pre-existing gap 2 in the claim devdoc).

## Verification

- Build: `go build ./...` → OK (`BUILD_OK`), `go vet ./internal/tools/` clean,
  `gofmt -l internal/tools/` → empty.
- Targeted tests:
  `go test ./internal/tools/ -run 'TestSyncAnchorGuard|TestWaitClaimLockVerdict|TestTaskDiffPeer|TestSyncTickShouldRunFull|TestMustSyncFullEvery' -count=1 -v`
  → 16/16 PASS, `ok github.com/nick/gopi/internal/tools 0.510s`; the RETRY
  test itself 0.03s (no wall-clock wait).
- Full package + repo suite: `go test ./internal/tools/ -count=1` and
  `go test ./...`, run detached (`job action=start`) — output read once:
  see the "Full suite" section below.
- Coverage metric: `python3 scripts/func_coverage.py --pkg internal/tools`
  → 181 funcs, 148 covered, **81.8%** (unchanged headline number — the epic
  converts previously-unreachable BRANCHES into asserted ones);
  `syncAnchorGuard`, `WaitClaimLockVerdict` and `reconRemotePendingSet` are
  all NOT in the `--uncovered` list, i.e. the new tests name/execute them.
  Statement coverage of the two legs under the targeted run
  (`go tool cover -func`): `WaitClaimLockVerdict` **100.0%**,
  `syncAnchorGuard` **92.3%** (the residual is the
  `reconLocalPendingSet` error leg, which needs an unreadable local ledger),
  `reconRemotePendingSet` 47.1% — the hook short-circuits before its ssh
  exec and JSON parse, which is the documented remaining gap (a seam can
  only replace the transport, not the parse the ssh leg would feed).
- Hermeticity of the new tests (they must not read production sync_peers):
  `GOPI_SYNC_PEERS="ghost@no-such-host.invalid" GOPI_SETTINGS=/nonexistent/settings.json
  go test ./internal/tools/ -run 'TestSyncAnchorGuard(Equal…|Refuses…|Wraps…)'
  -count=1` → 3/3 PASS: the seam, not the topology, supplies the peer.
- Race/determinism: `go test ./internal/tools/ -run
  'TestSyncAnchorGuard|TestWaitClaimLockVerdict' -count=3 -race`
  → `ok … 22.118s` (no data race on the package-level var/hook).
- Commit: `c4e0f4a` on `feat/sync-transport-seams`.

### Full suite (detached jobs, output read once)

Serial run on this branch, at `c4e0f4a`:

```
$ go test ./... -count=1        # (task-notify/memsync noise filtered)
ok  github.com/nick/gopi/cmd/agent               255.506s
ok  github.com/nick/gopi/internal/agent          273.134s
ok  github.com/nick/gopi/internal/checkpoint       1.420s
ok  github.com/nick/gopi/internal/config           0.357s
ok  github.com/nick/gopi/internal/deepseek         2.067s
ok  github.com/nick/gopi/internal/gopilog          3.041s
ok  github.com/nick/gopi/internal/hippocampus     27.277s
ok  github.com/nick/gopi/internal/pricing          0.009s
ok  github.com/nick/gopi/internal/proxy            0.021s
ok  github.com/nick/gopi/internal/shot             1.014s
ok  github.com/nick/gopi/internal/taskverdict      0.010s
ok  github.com/nick/gopi/internal/testutil         0.009s
ok  github.com/nick/gopi/internal/text2img         0.576s
ok  github.com/nick/gopi/testsuite                 1.255s
--- FAIL: TestMemorySearchNonBlocking (0.02s)     <- pre-existing flake, see below
FAIL github.com/nick/gopi/internal/tools         264.723s
FAIL

$ go test ./internal/tools/ -count=1               # serial re-run, same commit
ok  github.com/nick/gopi/internal/tools          176.541s
```

Multi-package `./...` runs its packages in parallel, and this host was under
load 10–15 at the time (Heroes3 81% CPU, `unattended-upgrade` 100% CPU, and
two peer agents' concurrent `go test ./internal/tools` loops). The two reds
that appear in that window are **pre-existing, load-sensitive flakes on
master too**, not caused by this change:

| Test | Nature | Evidence |
|---|---|---|
| `TestMemorySearchNonBlocking` | load-sensitive first-open race, filed as memory #1152 (`memInitMu` fix lives on another branch) | `-count=10` → `ok` on **this branch** and on **master** (0.51s / 0.36s); recorded as known flake in `devdoc/preexisting-test-reds-fix.md`, `devdoc/e2e-live-merge-report.md`, `devdoc/bidir2-fix.md` |
| `TestFreshDBOpenCloseIsFast` | `TestMain` tmpfs timing guard, budget 25ms×12 DBs (500ms under -race) — a pure wall-clock assertion about filesystem fsync cost | red only while load ≈ 12; `-count=5` → `ok` on this branch and master; direct measurement of 12 fresh DBs in `/dev/shm` = **6ms** at rest. Already filed as pending bug row `01a08c4f` ("wall-clock budget false-reds under host CPU contention — conflates fsync stall with scheduling starvation"); no duplicate filed here |

The serial `internal/tools` run (the one that matters for this epic's gate)
is green: **176.541s**, both new test families included. This epic's own
tests are wall-clock-free; they were re-run 3× under `-race` at load ~12
with no flake.
