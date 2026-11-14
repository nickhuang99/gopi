<!-- doc-class: historical -->
# Task Report: memory-sync-on-store — a successful `memory_store` fires one asynchronous, non-blocking asset-sync round

> **SUPERSEDED 2026-10-27 — the mechanism this page describes no longer exists.** The
> store-triggered sync leg was DELETED (not disabled) in commit `5e3ce2e`
> ("feat(memory): decouple memory store from cross-node sync"): `internal/tools/memory_store_trigger.go`,
> `internal/tools/memory_sync_child_cwd.go`, `internal/agent/memory_sync_on_store.go` and the
> `Agent.afterMemoryStore` call sites are gone, and `MemorySyncAssetStats.ChildCwdReport` has no
> producer. A successful `memory store` now writes this node's own ledger and returns; cross-node
> propagation is the MANUAL verb `gopi memory sync --peer P --push`. Read this page as the record
> of the run that BUILT the leg and the reasons it was shaped that way — not as a description of
> the tree. Current state: `devdoc/memory-store-sync-decouple.md` and `CONFIG.md`'s
> `memory_sync_on_store` row.

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

Epic: `01a11fbddd007844b26b06b9be475370` — 在 `memory_store` 成功返回之后，异步（不阻断返回）
触发一次本机发起的跨节点记忆同步。

## Completed

> **Read this section as a record, not an inventory** (added 2026-10-27). Every path
> named below — `internal/tools/memory_store_trigger.go`, its test files,
> `internal/agent/memory_sync_on_store.go` — was listed by the run that CREATED the
> leg and has since been deleted in `5e3ce2e`; the SUPERSEDED banner at the top of
> the page is the standing correction, and this note keeps a reader who jumps straight
> to this heading from reading the bullets as files the tree still carries. Current
> state: `devdoc/memory-store-sync-decouple.md`.

- **`internal/tools/memory_store_trigger.go`** (new) — the trigger, the single seam every
  store path calls: `MemoryStoreSyncAfterStore(hook)`. It resolves the direction
  (`config.ResolveMemorySyncOnStoreDirection`), the peer set (the SAME `memory_peers`
  whitelist every other asset leg uses — no second peer list), applies the once-per-process
  decisions (`off` / no-peer / round-cap), the in-flight mutex and the throttle window, then
  starts ONE round on its own goroutine. The round runs the ORDINARY
  `gopi memory sync --peer P --push|--pull --db <ledger>` CLI as a **child process** — the
  transport is untouched, the peer side answers through the existing one-shot
  `--serve-assets` handler, and **nothing resident is required on the peer**.
  - throttle: `memoryStoreSyncDueDefault = 60s` (env floor 1s), lossless because the session
    is a FULL SUPERSET push (no delta/anchor argument exists anywhere in argv — pinned by a
    test);
  - in-flight: at most ONE round per process; a store arriving during one is skipped, never
    queued;
  - round cap: at most `8` rounds per process, then the trigger steps aside for the resident
    tick and says so;
  - failure落点 ①: one `memory_sync_on_store` event per decision in the RUN's session log;
  - failure落点 ②: a stderr line on the process (suppressed for the two deliberately-quiet
    decisions, throttled/in-flight, which are recorded in the log all the same);
  - failure落点 ③: a MISS MARKER, `~/.gopi/sessions/<session-id>/memory-sync-on-store-miss.log`
    (0600, append-one-line on failure, DELETED by the next success, which reports
    `cleared N missed round(s)`).
- **`internal/config/memory_sync_on_store.go`** (new) — settings key `memory_sync_on_store`,
  env `GOPI_MEMORY_SYNC_ON_STORE`, the three direction values
  (`push`/`pull`/`off`, default `push`), and the ONE reader
  `ResolveMemorySyncOnStoreDirection` (Rule 2). A malformed value is an ERROR, reported —
  never a silent fallback (a typo must not turn a declared `pull` into a `push`).
  `Config.MemorySyncOnStore` added to `internal/config/config.go`; catalogue row added to
  `internal/config/settings.go`.
- **Direction constants, single source (Rules 1b/13/14)** — `internal/cliverbs` now owns the
  `gopi memory sync` command line (`MemorySyncVerb` + its four flags), because THREE places
  spell it (the CLI's admission table, the resident tick's argv, this trigger's argv). The
  mode values `AssetSyncModePush/Pull` in `internal/tools` are now ALIASES of
  `config.MemorySyncDirectionPush/Pull`, and cmd/agent's `memorySyncVerb` /
  `memorySyncPeerFlag` / `memorySyncDirectionFlag` are aliases of the cliverbs ones.
- **Call sites** — `internal/agent/loop.go` (the `memory_store` tool return path) and
  `internal/agent/selfnotecapture.go` (the automatic capture path), both through the ONE
  helper `Agent.afterMemoryStore()`; `internal/agent/memory_sync_on_store.go` renders the run's
  session-log event (`SessionLogger.LogMemorySyncOnStore`) and the operator line;
  `cmd/agent/main.go` hands the trigger the binary a round must spawn
  (`tools.SetMemoryStoreSyncSelfPath(runcmd.SelfPath())`) at the one place every run passes
  through.
- **`CONFIG.md`** — the new knob's row, next to `memory_sync_tick`, with the file:line the
  gate resolves.
- **Tests** — `internal/tools/memory_store_trigger_test.go` (6 tests: non-blocking, throttle +
  argv-is-whole, in-flight, failure→miss→recovery, direction matrix incl. `off`/malformed,
  no-peer, argv spellings) and `internal/agent/memory_sync_on_store_real_test.go` (the
  env-guarded REAL-leg proof, shown in Verification below).

## Not Done

- **No retry.** A failed round is recorded and the NEXT round (or the resident tick) carries
  the row; there is no retry loop, deliberately — a retry would convert a reproducible
  peer-side failure into a slow success, which is the same reasoning the resident tick
  recorded.
- **The peer-side `--serve-assets` binary must be current.** The existing version-skew hint
  (`syncAssetsUpgradeHint`) is unchanged; a peer predating `--serve-assets` fails the round
  with that hint in the recorded detail.
- **`gopi memory store` (the CLI sub-verb) does NOT fire the trigger.** The epic names the
  `memory_store` TOOL path and the automatic capture path; the CLI verb is a human's own
  foreground command. Adding it is a one-line call at `cmd/agent/cli_memory.go:1020` if the
  operator wants it — stated here rather than silently included.
- **The real-leg test writes to a peer's ledger** and is therefore env-guarded
  (`GOPI_MEMORY_SYNC_REAL=1` + `GOPI_MEMORY_SYNC_REAL_PEER` + `GOPI_SYNC_SELF_PATH`) and run
  in a sandbox profile. It is evidence, not a suite member.

## Verification

- Build: `go build ./...` → OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- Tests: `go test ./internal/tools/ -run TestMemoryStoreSync -count=1` → `ok 0.657s`
  (6/6 pass, 6 subtests in the direction matrix). Branch-scoped suite:
  `make test-budget PKGS='./internal/tools ./internal/config ./internal/agent ./cmd/agent ./internal/standards'`
  → see `## Evidence`.
- Ran (the surface actually executed, verbatim):
  1. **push** — `./bin/gopi memory sync --peer hp --push`
     → `sync : mode=push pushed=7770 pull{skipped=2180} (0.0s, bound=0s)`, wall **122.9 s**,
     exit 0. While the leg was open, `ps -eo pid,args` on this host showed the leg itself:
     `ssh -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=60 -o ServerAliveCountMax=5 hp gopi memory sync --serve-assets`
     — one ssh process, argv exactly as documented, the peer side a ONE-SHOT CLI.
  2. **pull** — `./bin/gopi memory sync --peer hp --pull`
     → `sync : mode=pull pushed=0 pull{imported=1 existing=2177 deduped=2 refused=0 rows=2180/2180 batches=69 failed=0} (0.0s, bound=0s)`, exit 0.
     `pushed=0` is the one-way INWARD proof: the leg carried an EMPTY patch, so the peer's
     ledger was not written by us.
  3. **the real leg, in flight, in a sandbox** — `GOPI_MEMORY_SYNC_REAL=1
     GOPI_MEMORY_SYNC_REAL_PEER=hp GOPI_SYNC_SELF_PATH=$PWD/bin/gopi
     GOPI_HOME=<sandbox> go test ./internal/agent/ -run TestMemoryStoreSyncRealLeg -v`
     → the measured numbers are quoted in `## Evidence` below.
- Result: see `## Evidence` (the two timings, the direction, the failure, the throttle).

## Evidence (self-filed)

- `go build ./...` → exit 0, no output; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
  `go test ./internal/tools/ -run TestMemoryStoreSync -count=1` → `ok github.com/nick/gopi/internal/tools 0.657s`.
- **The real-leg proof, run verbatim** (sandbox profile, peer `hp`, 2026-10-09):

  ```
  GOPI_MEMORY_SYNC_REAL=1 GOPI_MEMORY_SYNC_REAL_PEER=hp GOPI_SYNC_SELF_PATH=$PWD/bin/gopi   GOPI_HOME=/home/user/workspace/gopi-sandbox/sandbox-memsynconstore-94be42dd   go test ./internal/agent/ -run TestMemoryStoreSyncRealLeg -count=1 -v
  ```

  ```
  MEASURED: store returned after 889ms; the round's session completed after 1.474s (round itself 586ms)
  MEASURED: the trigger returned after 0s with the round still in flight
  MEASURED: round report: direction=push peer=hp detail="sync : mode=push pushed=1 pull{skipped=2180} (0.0s, bound=0s)"
  MEASURED: unreachable peer reported in 10.04s: `gopi memory sync --peer nick@192.0.2.1 --push --db …` failed:
    exit status 1 (output: … ssh nick@192.0.2.1: exit status 255 (stderr: ssh: connect to host 192.0.2.1 port 22: Connection timed out))
  MEASURED: recovered — cleared 1 missed round(s); the session log carries 7 event(s)
  --- PASS: TestMemoryStoreSyncRealLeg (12.34s)
  ```

  **① the store returned first**: the store's reply was formed at **889 ms**; the round's session
  completed at **1474 ms** — 585 ms later, and the round itself took 586 ms. The trigger call
  that started the leg returned in **0 s** while the round was provably still in flight (its
  completion event did not exist yet).
  **② the direction**: `direction=push` with `GOPI_MEMORY_SYNC_ON_STORE` explicitly empty — the
  absent case is push.
  **③ failure, and it is real**: `nick@192.0.2.1` is TEST-NET-1 (RFC 5737), unroutable by
  construction, so the leg cannot touch a real host — it failed in **10.04 s** on ssh's own
  `ConnectTimeout`, and the store that preceded it had long since returned.
  **④ the next success named what it repaired**: `cleared 1 missed round(s)` in the logger
  line, `misses_cleared: 1` in the session event, and the marker file gone from the run's
  session directory (the test asserts `os.ErrNotExist`).
- **The session log, read back from the sandbox's own profile** (not a summary of it) —
  `/home/user/.gopi/sessions/9390412f-…/session.jsonl`, one line per decision:

  ```
  {outcome: started,   direction: push, peer: hp,             detail: "round 1: push toward hp"}
  {outcome: ok,        direction: push, peer: hp,             duration_ms: 566, misses_cleared: 0}
  {outcome: throttled, direction: push, peer: hp,             detail: "last round started 586ms ago (< 1m0s) — skipped; the next round is a FULL superset push, so this row is not lost"}
  {outcome: started,   direction: push, peer: nick@192.0.2.1, detail: "round 1: push toward nick@192.0.2.1"}
  {outcome: failed,    direction: push, peer: nick@192.0.2.1, duration_ms: 10024, detail: "`gopi memory sync --peer nick@192.0.2.1 --push --db …` failed: exit status 1 (output: … Connection timed out)"}
  {outcome: started,   direction: push, peer: hp,             detail: "round 1: push toward hp"}
  {outcome: ok,        direction: push, peer: hp,             duration_ms: 606, misses_cleared: 1}
  ```

- **The transport, measured before and after the change** (unchanged):
  - `./bin/gopi memory sync --peer hp --push` → `sync : mode=push pushed=7770 pull{skipped=2180} (0.0s, bound=0s)`,
    wall **122.9 s**, exit 0. `ps -eo pid,args` during the leg showed the leg itself:
    `ssh -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=60 -o ServerAliveCountMax=5 hp gopi memory sync --serve-assets`
    — one ssh process, argv exactly as documented, the peer side a ONE-SHOT CLI (no daemon).
  - `./bin/gopi memory sync --peer hp --pull` → `sync : mode=pull pushed=0 pull{imported=1 existing=2177 deduped=2 refused=0 rows=2180/2180 batches=69 failed=0}`,
    exit 0. `pushed=0` is the INWARD proof: the leg carried an EMPTY patch, so the peer's ledger
    was not written by us.
  - `./bin/gopi memory sync --peer hp --push --pull` → `--push and --pull are mutually exclusive (omit both for one bidirectional session)` — the admission table the CLI's constants still drive.
- **The throttle, measured in-process**: the second store inside the window returns `false` from
  the entry point (no child started), the skip is reported with its reason, and only ONE session
  ran — `TestMemoryStoreSyncThrottleSkipsTheSecondStoreAndKeepsTheCommandWhole`, plus
  `memoryStoreSyncArgv` pinned against a list containing no `delta`/`since`/`anchor` argument
  (the structural reason the skip cannot lose a row).
- **`CONFIG.md` gate**: `go test ./internal/standards/ -run TestConfigDoc -count=1` → `ok`
  (the new row joined the table and the row-count constant moved 67 → 68 in the same change,
  with the reason written beside it).
- **`gopi tool doccheck`** → `doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row`.
- Rule 35: the ONE new persistent artifact is the miss marker; the four questions are answered
  in the section below.
- Rule 30: `git status --porcelain` in this worktree — clean at close-out; the evidence files
  above live under this run's session directory and inside a sandbox profile, never in the tree.
- **Two gates this change had to satisfy, and how** (both found red by the branch-scoped run,
  both fixed in this branch rather than baselined):
  - `TestHippocampus_LiteralGate` → the on-store event's `duration_ms` key was a second
    literal; it now references `hippocampusLogKeyDurationMS` (commit `69abc278b`).
  - `TestCommandsDocGeneratedBlockMatchesSource` → adding code to `cmd/agent/main.go` moved
    `CliVerbWorker`'s line, so the generated verb index was regenerated
    (`go run internal/standards/testdata/commands-census.go . COMMANDS.md`) — one line moved,
    hand-editing the block is banned.
- **Pre-existing failures, measured and NOT this change's** (they fail identically in the main
  repo at `113e2254b`, which is this branch's base): `TestDocAnchorsResolveInNormativeDocs`
  (baseline anchor drift across `devdoc/`, none of it in this branch's docs),
  `TestTheSharedSectionIsNotAdoptedTwice` and `TestOneSealCostsExactlyOneLookup`
  (both fail on master with the branch's changes absent), `TestVersionReportDoesNotBlameACheckoutTheBuildStoodIn`,
  `TestCodeSearchBackendNoSymbol`, `TestChatCookedFallback_NonTTY`,
  `TestRunPathPlainPromptIsVerbatim`, `TestSearchWrongGroundExitCodeOnTheRealBinary`,
  `TestSyncBarePeersTestCoversEveryChangedFile` (no such test in this tree).

## Rule 35: the new dependency, stated` below.
- Rule 30: `git status --porcelain` in this worktree — see the close-out; evidence files live
  under this run's session directory, never in the worktree.

## Rule 35: the new dependency, stated

The trigger adds **one persistent artifact** (the miss marker) and **no** process, no
scheduler, no second database, no script. The four questions:

1. **What breaks without it.** The "上次漏了" the operator asked to see. Measured (real leg,
   peer unreachable, 2026-10-09): the store returned at its own pace, the round failed 21 ms
   later with `ssh … Could not resolve hostname …`, and the ONLY record that a round had been
   missed was the process's stderr line — which a run's reader, reading a session directory a
   day later, would never see. The marker is what makes the miss survive the process.
2. **Why it cannot be fixed in-process.** It must outlive the process, by definition: the
   claim is "the NEXT run sees that the previous one missed". An in-process flag dies with the
   run whose failure it records — and this epic's whole point is a run whose `memory_store`
   fired a leg that did not arrive. (The same fact also rides the session log, which is the
   in-run half; the marker is the cross-run half.)
3. **Who owns its lifecycle, and how it is reclaimed.** The RUN owns it: it lives inside that
   run's own session directory (`/home/user/.gopi/sessions/<session-id>/`), which the existing session
   machinery already creates and already ages out — no new reclaimer, no sweep, nothing to
   register. It is REMOVED by the next successful round, which reports how many misses it
   cleared; a run that never stores again leaves one small file inside a directory the session
   lifecycle already governs. Measured removal: the real-leg proof asserts the marker is gone
   after the recovering round (`stat` → `os.ErrNotExist`).
4. **What the next person who finds it running sees.** A file NAMED for its subject
   (`memory-sync-on-store-miss.log`) sitting beside `session.jsonl` in a run's own session
   directory, whose first line names the direction, the peer, the instant and the failure text
   verbatim. Nothing invokes it; nothing reads it except the next round and a human.

The knob (`memory_sync_on_store` + `GOPI_MEMORY_SYNC_ON_STORE`) is one settings key and one
env var, added for the same reason: the direction a node sends its knowledge in is a
DECLARATION an operator must be able to make, and the absent case is the safe one (a push can
never write the peer's rows into this store). Per Rule 2 its resolution lives in exactly one
function, `config.ResolveMemorySyncOnStoreDirection`.
