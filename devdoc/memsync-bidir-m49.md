<!-- doc-class: historical -->
# Task Report: M4.9 增补 — db sync 双向语义定稿的验收测试（import 对称 / 双端冲突 / serve tick 环）

> **Corrected 2026-11-05 (classification + content).** This is a 2026-09 task report:
> a record of one run, so it is `historical`, not `normative` — it made sense as
> `normative` while the engine it verified was live. That engine is gone: the
> cross-node sync surface (`task sync`/`diff`/`harvest`/`push`/`send`/`receive`/`pull`,
> `runTaskSyncTick`, `SyncPeerSession`) was deleted 2026-11-05, and with it
> `GOPI_SYNC_INTERVAL` — a knob the catalog and `gopi --help-env` advertised while
> nothing read it. The row below that describes the serve tick auto-sync describes a
> mechanism this repository no longer has.

分支 `feat/memsync-bidir-m49`（worktree ~/workspace/gopi-bidir-m49，基 master 4e22a64）。
Epic：t-1788075984 增补（nick：db sync 从一开始就是双向——双端冲突都要处理，this is
price we have to pay；并入 t-1788075984 实现范围）。

## 背景：epic 到手时的真实状态（审计结论）

增补要求的三通道双向语义**在 master 已全部落地**，本单是它的验收测试闭环：

| 通道 | 增补要求 | master 现状 | 本单动作 |
|---|---|---|---|
| ① task 行 | 双向 memsync，两端 import 同一套 rules | M4.8/M4.13 delta（a395f5a、5c68437）对称已实现：hash 去重/前向迁移/never overwrite/from-<node> provenance | **⓪对称验收测试补上**（此前只有 peerbox→HP 方向的 E2E） |
| ② git | HP fetch peerbox master + push feat/* back | push-back（harvest git push）+ fetch 均已有，零新代码 | 无代码，纪律项（devdoc 记录） |
| ③ brew | M4 memsync 本来双向（node: provenance） | 双向已实现；import 端两侧对称可跑 | 审计确认，零改动 |

serve tick 自动 sync（GOPI_SYNC_INTERVAL>0 + sync_peers）也已在 master（runTaskSyncTick，
cli_tasks_sync.go:160）——增补要求的"各自节点 tick 检测本地新终态→export→对端 import"
就是每个 serve pass 后的 SyncPeerSession（锚点窗口内的新行双向走）。

**真 gap（本单补的）**：增补明确的双端对称语义没有对应的验收测试面——
`memory_sync_bidir_e2e_test.go`（基础 epic）只测了 peerbox→HP 主方向 + 分区/内容冲突；
HP 端作为 import 方的对称性、HP 本地执行与 peerbox 终态裁决的碰撞、serve tick 双端环
都只被间接覆盖。

## Completed

- **internal/tools/memory_sync_bidir2_e2e_test.go**（4 个双库 E2E，全部走真实
  export/merge/session 原语）：
  - **TestBidir2ImportSymmetry**（⓪ import 对称）：peerbox→HP 与 HP→peerbox 驱动同一个
    merge（SyncTaskImportDelta），接收端报告镜像的计数类（imported=2 首触 / already
    回声），两个台账收敛到逐 id 逐 status 相等的状态集；后续双向 pass 全 echo。
  - **TestBidir2LiveConflictNeverClobbered**（⑤a 本地执行 vs 远端终态）：HP claim 并
    in_progress（claimed_by 有值）时 peerbox 完成自己的 pending 副本并 push 终态——HP 的
    执行真相**不被覆写**（already=1，M4.8 硬规则在 delta 通道的实现）；HP 走完后收敛；
    第三轮 pass 静默。
  - **TestBidir2DivergentTerminalsConverge**（⑤b 终态分歧显式报告）：同一锚点上
    peerbox=completed vs HP=cancelled → 双侧**显式 conflict tag**（绝不静默），本地
    仲裁者按 completed-wins 收敛（cancelled→pending→in_progress→completed 走状态机），
    分歧痕迹留在 tags（task show 可审计）。
  - **TestBidir2ServeTickAutoSync**（⓿ serve tick 双端环）：peerbox tick 推终态行 +
    拉 HP 新行，HP tick 推本地新行；第二轮 tick 全 echo（锚点吸收）。session 走真实
    SyncPeerSession 全路径，ssh 传输腿用**注入 hook** 替换（见下）。
- **syncSSHExchangeHook**（internal/tools/memory_sync_session.go，+10 行）：ssh 传输
  可注入 seam（HarvestOptions.Fetch 同款模式）。nil（生产）= 原 exec ssh 路径，行为
  零改动；非 nil = in-process 调对端真实 SyncRemoteServe。serve-tick 测试因此结构性
  无 ssh 可达。

## Not Done

- git 通道纪律（"每次 peerbox merge 后 HP 尽快 fetch"）：无代码需求，属运维纪律。
- brew 通道：M4 已双向（node: provenance），增补明示零改动——审计确认未触碰。
- 真机（ssh hp/peerbox）复测一轮双向：本单之前 peerbox 收尾轮已在真账 272 行上跑通
  （devdoc/memsync-bidirectional.md 增补节）；本单新增测试面是单机双库——真机通道
  的回归由既有 shim E2E（devdoc/memsync-e2e-sim.sh，重跑通过）+ 单测覆盖。

## 事故记录（真实泄漏，已修复+结构化防复发）

第一版 serve-tick 测试直接调 SyncPeerSession + 真实 sync_peers 白名单——测试内的
tick **ssh 到了生产节点**：t-tick-* 两行写进 peerbox/HP 真实台账、HP 的 sync anchor
被推进。处置：两侧 DELETE t-tick-* 行 + peerbox 端 hp anchor 手工回拨（1788143199→
1788159686，与对端 peerbox 锚点对齐——HP 无新增本地行期间推进锚点不丢数据：窗口内
无行，后续会话重拉 5s margin 内重复行为幂等）。防复发：测试改注入 hook，任何别名
不进沙箱 map 即报错——ssh 在测试进程内结构性不可达。教训已记 memory。

## Verification

- 单测（新增）: `go test ./internal/tools/ -run 'TestBidir2' -count=1` → 4/4 PASS（29s）
- 回归（sync 家族）: `-run 'TestTaskSync|TestHarvest|TestSync'` → ok（109s，含既有
  bidir E2E/终态裁决/harvest 全绿）
- 回归（daemon/config）: `go test ./cmd/agent/ ./internal/config/ -count=1` → ok
- Build: `go build ./...` OK；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- Shim E2E: `bash devdoc/memsync-e2e-sim.sh bin/gopi` → 4 步全过（双向 handoff、
  幂等 re-run 锚点吸收）
- Commit: 688b7aa test(memsync): M4.9 addendum bidirectional acceptance

## 设计决策（给未来 agent）

- **验收测试断"契约"不断"时钟"**：同秒双写 = equal-anchor（1s 分辨率的合并语义），
  严格新 = migrated。两侧都可能发生 → 断言 `migrated+already=1`（吸收必显式），不赌
  秒边界——和既有 syncTestClockBackstep/syncBidirTestSleep 手法一致。
- **ssh shim re-exec 测试二进制的死路**：helper 子进程会被 testing 框架接管 stdout
  （PASS banner 污染 ssh 管道）/ stdin 争用挂死（2min timeout 教训）。正确 seam 是
  传输层注入（syncSSHExchangeHook），别和 testing 框架抢进程控制权。
- **测试访问生产通道 = 数据泄漏**：任何测试只要 resolve 到真 sync_peers/真 GOPI_HOME
  就可能 ssh 出去。沙箱必须"结构化"（无可达路径），不是"纪律上别跑真网"。

## 交付位置（Rule 6d）

- 分支：`feat/memsync-bidir-m49`（worktree ~/workspace/gopi-bidir-m49，共享对象库，
  主仓立即可见；HEAD 688b7aa）
- 恢复：`git -C ~/work/GoPi show feat/memsync-bidir-m49:devdoc/memsync-bidir-m49.md`
- 未 merge（Rule 6e：不自行 merge，留裁决）
