<!-- doc-class: historical -->
# M4.20-A: Worker Loop (`gopi worker`) — Implementation Report

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Branch: `feat/worker-loop-m420` (worktree `~/workspace/gopi-m420`, base master f391a4a)

## Completed

- **`gopi worker` top-level verb** (`cmd/agent/main.go` `isWorkerSubcommand`):
  a TEMPORARY PROCESS — no daemon, no flock (unlike `task serve`). Parallelism
  = number of worker processes; death is stateless (all durable facts live in
  the ledger + sync channel).
- **Worker loop** (`cmd/agent/cli_tasks_worker.go`):
  1. **Claim** — `tools.TaskNextFiltered` CAS (M4.20 起点: exactly one
     concurrent claimer wins; a lost race loops internally to the next
     eligible row; empty queue → drained).
  2. **Execute** — reuses `runTaskRun` (single-runner semantics, repo
     guardrails, M4.18 claim broadcast, terminal writeback chain). The
     already-claimed row is passed through with `TaskResumeClaimed` re-stamp
     (a second claim would steal the NEXT pending row).
  3. **Terminal must-succeed push** — inherited: the writeback's M4.19 ②
     `PushTerminalTaskSync` runs inside the child/close-out path unchanged.
  4. **顺势 db-sync** — `workerSyncPeers`: one `SyncPeerSession` per peer,
     PULL-then-PUSH (git pull-then-push semantics: SyncPeerSession pushes
     our delta AND merges the peer's returned envelope — pull-first order in
     one round trip). Fail-soft per peer: a sync failure prints and
     continues — the must-succeed push already delivered the terminal state.
  5. **Next / exit** — drained queue (re-checked once after
     `workerIdleRecheckMS`) → exit 0. `GOPI_WORKER_MAX_ROUNDS` caps rounds
     (0 = unlimited; Rule 2: one env read site).
- **Worker provenance** (`internal/tools/memory_sync_delta.go`): rows a
  worker executed carry `worker:<hostname>` in tags.
  `TagWorkerClaim` (idempotent union) + `TaskRunWritebackTagged`; the
  worker's writeback routes through it via the `workerWriteback` latch — a
  plain `task run` writeback stays tag-free.
- **Anti-misclaim**: the worker never claims `pa-queue`-tagged rows (serve's
  `--skip-tag` contract, fixed list `workerExcludedTags`).

## Not Done

- `--once`/`--max-rounds` CLI flags: covered by `GOPI_WORKER_MAX_ROUNDS` env
  (the epic's contract is queue-drain; a round cap is the escape hatch).
- No worker-side `GOPI_SKIP_CLAIM_BROADCAST` override: the claim broadcast is
  part of the start-point atomicity the worker relies on — it always runs
  (sims should keep using `task run` with the skip env).

## Verification

- Build: `CGO_ENABLED=0 go build ./...` — OK
- Tests:
  - `go test ./cmd/agent/ ./internal/agent/ ./internal/config/` — ok
    (83s / 102s / 0.07s), including the new TestWorker* trio.
  - `internal/tools/` full suite hit the 10m box-load panic-timeout
    (victim: TestBashIdleTimeout_KillsStalledCommand — passes isolated in
    1.0s; the documented box-load flake pattern, memory #1183/#1147).
    Selective ledger+sync+worker suite
    (`TestWorker|TestTask|TestSync|TestTagWorker|TestBidir`) — ok 547.8s,
    EXIT=0: no regression from the worker changes.
  - New tests: `TestWorkerWritebackTagUnionsProvenance`,
    `TestWorkerMaxRoundsEnv`, `TestWorkerExcludedTagsConstant`.
- E2E smoke (real binary, sandbox GOPI_HOME): submit 1 task → `gopi worker`
  → claimed, resumed, child ran, writeback `completed`, tags
  `worker:peer1`, db-sync to 2 peers ok (pushed=2 pulled=1), then
  "queue drained — exiting", exit=0. Idempotent re-run: exits immediately.
- 2-worker parallel E2E (same sandbox, 2 queued tasks, two concurrent
  `gopi worker` processes): w1 claimed `-1` task, w2 the other — each
  executed exactly once (CAS split the queue), both exit=0, both rows
  terminal, tags carry `worker:peer1`. No double-run observed.

## M4.20-D: provenance + observability（追加）

- `[worker-sync] peer=<name> pushed=N pulled=N migrated=N`（成功）/ `[worker-sync] peer=<name> FAILED: <err>`（fail-soft 继续）— 每轮 db-sync 的一行观测契约（grep 友好）。
- 排空退出语义：**始终 exit 0**（排空=成功）；消息区分 `queue drained — exiting`（本机全闲）与 `queue drained (other holder active: N rows)`（他者持行，`tools.LiveRowsOwnedByOthers()` 按 claimed_by 前缀计数）。
- 新增 `tools.OpenWorkerProbeDB` 测试 seam；测试：TestWorkerSyncObservabilityLine（行形状钉死）、TestWorkerDrainedExitSemantics（0/1 行两态）。
- E2E 复验：单 worker 提交→完成→`[worker-sync] peer=hp pushed=2 pulled=1`→排空 exit 0。
