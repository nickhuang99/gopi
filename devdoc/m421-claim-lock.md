<!-- doc-class: normative -->
# M4.21 跨节点任务领取 = 数据库级两阶段锁（feat/claim-lock-m421）

> Epic 交付（nick 定稿）：`task run` 的跨节点领取从"广播+让渡"改为**两阶段分布式锁**，
> 失败=事务回滚（永不 cancel），碰撞=ALOHA 退避重试。
> Branch: `feat/claim-lock-m421` · Commit: `db22503` · Worktree: `~/workspace/gopi-m421`

## 1. 动机：旧语义的两个致命面

M4.18 的 claim broadcast（领取即所有权：cancel + sync-yielded 永久退出）有两个问题：

1. **双弃死锁**：我的 cancel 镜像到对端 → 两边都不执行（最致命的失败态）。
   nick 否决：失败必须回滚到 pending，把行还给对端**和**自己。
2. **信息传递裂缝**：广播只是"通知"，不构成锁——对端看到 pending 仍可领取。
   两阶段锁把对端变成协议平等方：每边都在自己的 ledger 上跑同一把 CAS 阶梯。

## 2. 协议：三阶段 + 冲突不变式

```
Phase 1  本地锁     UPDATE tasks SET status='claimed' …
                    WHERE task_id=? AND status='pending'      ← 恰好一个赢家
Phase 2  远程锁     对端跑同一把 CAS（pending→claimed），row
                    已被本地 live-claimed → REFUSE
Phase 3  推进(双锁齐) 两边都 claimed → in_progress；然后才执行 briefing
```

**验收不变式**：claim 成功后 local row == peer row == in_progress，
不存在"一边执行、另一边还是 pending"的窗口态。

**碰撞不变式**（持有本地锁、尚未双锁齐时，对端行只有两种态）：
- 对端 claimed/in_progress（协议中）→ 冲突 → 退避重试
- 对端 in_progress（它赢了两阶段）→ 回滚 + 让渡
- 每个观测都映射到 {retry, yield}——没有未定义态。

**时序图**（正常全链 / ③对端已跑 / ④回滚）：

```
Node A                    Node B
  │ Phase1 CAS claimed      │
  │──── acquire patch ─────▶│ Phase2: pending→claimed (CAS)
  │                         │ Phase3: claimed→in_progress
  │◀───── accepted ─────────│
  │ Phase3: claimed→in_progress
  │ ▼ 双锁齐 → run briefing  │ ▼ in_progress (mirror)
  │
  │ (对端已跑 ③)             │
  │ Phase1 CAS claimed      │ in_progress (owner, 双锁齐已毕)
  │──── acquire patch ─────▶│ REFUSE: claim state conflict
  │◀───── refused ──────────│
  │ rollback: claimed→pending (claimed_by 保留 + sync-claim-revert tag)
  │ ▼ yield（不执行）        │ ▼ 继续执行
  │
  │ (Phase2 失败 ④：对端不可达×N 轮)
  │ retry: backoff 0.5-2s → ×2 … cap 30s, N=3
  │ rounds exhausted → rollback to pending（行活着，双方可再领）
```

## 3. 关键实现（internal/tools/memory_sync_claim.go 重写）

| 符号 | 作用 |
|---|---|
| `RunClaimLockProtocol(taskID, owner)` | runner 驱动：逐轮远程锁 → proceed/retry/yield |
| `StartClaimLockBroadcast` / `WaitClaimLockVerdict` | 一轮的并行对端腿 + 有界等待（`claimLockWait`） |
| `classifyClaimLockRound` | 碰撞不变式三分类（accept=proceed / refuse=yield / unreachable=retry / row-not-found=uncontested） |
| `SyncClaimRemote`（对端 `task sync --claim-remote`） | acquire 动词（pending→claimed→in_progress 条件写）/ revert 动词（claimed/in_progress→pending）/ 终态拒绝 |
| `RollbackLocalClaim` | claimed→pending，claimed_by **保留**（审计），fresh updated_at（回滚锚随合并传播），`sync-claim-revert` tag |
| `ClaimLockBackoff(round)` | ALOHA: [0.5s,2s) 抖动起步，×2 指数，cap 30s（Rule 1：全部常量集中在 tunables 块） |
| `claimLockMaxRounds=3` | 轮数上限，耗尽=概率性让渡（仍是 revert） |

配套改动：
- `tasks.go`：`TaskStart` 移除 pending 直通——**所有**领取必须过 Phase-1 CAS；
  新增 `TaskClaimExplicit`（`task run <id>` 的显式领取：pending=CAS 领取，claimed=崩溃恢复重领）。
- `cli_tasks.go`：runTaskRun 接 `RunClaimLockProtocol`；yield 时 exit 1（不执行）；
  `GOPI_SKIP_CLAIM_BROADCAST=1` 保留单节点逃生门。
- M4.19 ② terminal must-succeed push（`PushTerminalTaskSync` 1s/2s/4s 指数重试 +
  `notifyTerminalPushFailed` 报警事件）随本次重写一并保留。

## 4. 修复中的事故（诚实记录）

- **环境慢导致 go test 默认 10m timeout panic**（本机 load ~3，全量 tools 包 >10m）。
  与本次改动无关（master 同样超时，baseline 复现在同一个测试上挂起——sqlite WAL
  大量 TempDir 文件在慢盘上的 I/O）。后续全量验证用 `-timeout 30m` 后台跑。
- **工作树事故**：`git stash` + 多文件 mv 的恢复操作把未提交的 5 个工作文件弄丢了
  （write 工具的产物不在 git 对象里，stash pop 后只剩 untracked）。**全部从
  `git fsck --unreachable` 的 dangling blob 恢复**（write 调用恰好落过 hash-object）。
  教训：**分阶段 commit，永远不要把交付押在工作树/暂存区上**。
- 首版 e2e 断言 `task next` 已在 M4.5 移除 → 改用 `task run next` 的 Phase-1 打印验证死锁防护。

## 5. 验证

| 项 | 结果 |
|---|---|
| `go build ./...` | OK |
| `go vet`（tools+agent） | clean |
| claim-lock 单测矩阵（rollback/advance/revert/classify/backoff/yield/push/notify） | ok（46.7s） |
| E2E 五组 `scripts/e2e-m421-claim-lock.sh` | **19/19 PASS**（①正常全链 ②碰撞恰一赢家 ③对端已跑让渡 ④回滚无残留 ⑤退避序列+让渡+死锁防护） |
| 全量 `go test ./internal/tools/ ./cmd/agent/ ./internal/agent/ -timeout 30m` | 见 tasks 报告（后台 job-3） |
| Commit | `db22503`（6 files, +988/−413） |

## 6. 未做 / 后续

- 真实 ssh 双机 E2E（当前 sim 用 in-proc 桥接探针驱动同 host 双 GOPI_HOME；
  ssh 腿由 `TestClaimSSHCommandShape`（argv 形状）+ `TestClaimSSHExchangeUnreachableFailsSoft` 覆盖）。
- serve tick 内的自动收尾（yield 后 next-pass 重领）依赖现有 dispatcher 轮询，未加新逻辑。
