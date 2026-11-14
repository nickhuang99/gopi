<!-- doc-class: normative -->
# Task Report: memsync task 通道防自引用 export 门 — 预见性审查（brew 语义对齐）

分支 `feat/task-id-collision`（HP 主仓，基 3c80e40）。修复面：
`internal/tools/memory_sync_delta.go`（v2 delta export）+ `memory_sync_task.go`（v1 one-shot export）。

## Epic 判定与审计结论

Epic 问题："task 行扩展（M4.8）的 export 过滤是否同样防回流——brew 行防自引用语义
（export 只出 sources 含 brew 标记的行）task 通道有没有？"

**审计结论（修前）**：task 通道的两个 export 都**没有**防自引用门——
- `SyncTaskExportDelta`（v2 session push/pull 的唯一 SELECT）：只有锚点窗口 + Tags LIKE 门。
- `SyncTaskExport`（v1 --export-tasks）：`WHERE 1=1` + status/tags/since。

## 死循环路径（Epic 预见成立，但机制与 epic 叙述的略有出入）

epic 担心的"payload 变了 → peerbox INSERT 重复行"实际被两层已有机制兜住：
①payload 内容永不 rewrite（syncDeltaMergeRow 的 never-overwrite + content-conflict
tag），同 id 回流只会 newer-wins/echo/conflict，不会 INSERT 第二行；
②同 id 回流撞 task_id 分支，不产生新行。

**真实的回流面**是状态/标签镜像回灌：HP 收到 from-peerbox 行后，本地任何一次合法写
（tag edit 走 taskAddTag，bump updated_at）都会推进 HP 的本地锚点 → HP 的 re-export
窗口重新打开 → 该行再次出门 → peerbox 的 merge 看到 updated_at 更大（HP 的写入晚于
peerbox 自己的原始写）→ newer-wins 用 **HP 侧的 stale 镜像** 覆盖 peerbox 更新的本地
tags/status。锚点回退余量（syncAnchorMarginBackdate）会周期性重发，故这不是一次性
污染而是每次 sync 都可能重演。实测修复前：echo push 返回 RowsTagged=1（HP 的
hp-touched 标签打回了 peerbox 的行）。

## 修复：export 端 NOT LIKE 门（对齐 brew 双闸门）

两个 exporter 都加同一句：

```sql
AND NOT (',' || COALESCE(tags,'') || ',') LIKE '%,from-<self>,%'
```

- `syncSelfTag()`（新 helper）：`from-` + (GOPI_SYNC_NODE 覆盖 || hostname) ——
  与 export 的 node-identity 规则同源，同 host 双节点 sim（GOPI_SYNC_NODE）照常工作。
- 语义：带 from-<本节点> 的行是**通道带来的行**，它的真相由对端自己的写入传播，
  本节点的镜像副本永不二次出门（brew：导入行 node:* 永不二次导出，同构）。
- 排除只挡 from-<self>，不挡 from-<peer>：转发对端状态推进的行（relay legitimacy）
  与本节点自投单（想要的方向）都保留。

## 测试（memory_sync_reflow_test.go，3 个）

1. `TestReflowExportExcludesFromSelfRows` — v2 export：from-<self> 行被排除，
   本地自投行 + 外来 from-peerbox 行照常出门。
2. `TestReflowExportGateV1Export` — v1 --export-tasks 同门。
3. `TestReflowEchoStaleMirrorNoReturn` — epic 端到端：peerbox 自投 → hp import →
   hp taskAddTag（锚点推进）→ hp 回推 → 修前 RowsTagged=1（stale 镜像打回），
   修后 echo no-op（already=1），peerbox 行内容/标签零污染。

## 全量对照归因（Rule: environmental 失败必须 base 对照）

- 修后 sync-family（TestBidir|TestTaskSync|TestSync|TestHarvest|TestReflow|TestTask）
  全绿；`TestSyncDeltaTwoNodeHandoff` 需补 `GOPI_SYNC_NODE=b`（模拟节点身份必须与
  A 侧 hostname 区分，否则 B 的 push 会自我过滤——门生效的直接证据）。
- tools 包全量剩余 2 个失败（TestMemorySearchNonBlocking tmpdir race +
  TestEnsureTagsRegeneratesForNewerTS ctags JS/TS env）：base（stash 本改动）同样
  失败，预存环境问题，与本改动无关。cmd/agent + internal/config 全绿。
