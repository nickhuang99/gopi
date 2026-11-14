<!-- doc-class: normative -->
# memsync patch Version 兼容闸 + generator 头 (4c3575a)

## Epic
三节点二进制版本不一致是常态 (peerbox 新 / HP 旧)。新字段的 patch 被旧二进制读:
Go json.Unmarshal 默认丢弃未知字段 → 静默丢字段入库, 数据不完整且无报错。
SyncPatch.Version 字段存在, 但 import 闸是严格等值 (`!=`), 无版本语义、无
升级提示、行级 schema 漂移没有第二道闸。

## Bond 核查结论 (epic 问题 ①)
分支 feat/memsync-bidirectional 上 import **已有** Version 校验 (master 无
memory_sync.go, 该文件只在此分支): brew v1 (memory_sync.go)、task v1
(memory_sync_task.go)、delta v2 (memory_sync_delta.go + session.go) 四处
`!=` 严格等值拒绝。缺口不是"没有校验", 而是校验语义与可排障性:
1. 未来 v3 出现时, v2 代码对未知字段依旧静默 — Version 数字是唯一防线,
   拒绝时必须说清"为什么拒、怎么修"。
2. patch 无 generator 版本头 — 拒绝后无法定位是哪个节点的哪个 build 产的。
3. 版本缺失 (v0/手写 patch) 走 `!=` 只报 "unsupported patch version 0",
   语义含混。

## 实现 (共享闸 syncCheckPatchVersion, Rule 1 集中一处)
| got vs want | 行为 |
|---|---|
| ==  | nil (当前格式, 完整支持) |
| >   | 拒绝: "v%d is NEWER than this binary supports (v%d) — refusing import to avoid silent field loss; upgrade this node's gopi binary" |
| ==0 | 拒绝: "missing its version field … silently dropped fields" (永不当作当前格式) |
| <0  | 拒绝: "invalid patch version" |
| <   | 拒绝: 旧格式 → 提示升级导出节点 (防御, 正常不应出现) |

落地点 (全部 5 处 import 闸统一):
- memory_sync.go SyncImport (brew v1)
- memory_sync_task.go SyncTaskImport (task v1)
- memory_sync_delta.go SyncTaskImportDelta (delta v2)
- memory_sync_delta.go SyncDeltaEnvelopeFromPatch (v2, nil-safe via
  syncVersionOrZero — nil patch 走 got==0 分支)
- memory_sync_session.go SyncRemoteServe (ssh 对端 stdin)

### Generator 头 (新增字段, 信息性)
SyncPatch/SyncTaskPatch 增加 `generator` (omitempty): syncGeneratorVersion()
从 debug.ReadBuildInfo 取 vcs.revision (+-dirty), 兜底 "dev"。export 时写入;
import 永不据此拒绝 (向前兼容: 旧 patch 无此字段照常读)。用途: 拒绝信息 +
`strings` 排障时直接看 patch 是哪个 build 产的。

### 为什么不用 DisallowUnknownFields
行级未知字段检测 (json.Decoder.DisallowUnknownFields) 对"旧二进制读新
patch"无效: 旧二进制不认识新字段名, 反而把合法的未来 patch 全拒。
版本数字递增 + 兼容语义是唯一正确机制; 必填字段完整性已有 (topic/
question/answer / summary 空检查)。

## bidir E2E epic-② 修复 (顺带, 环境性)
TestBidirEpic2StateConflictExplicitReport 在本机 (hostname=hostb-Laptop,
settings.json sync_peers=[hp,peerbox]) 稳定 FAIL: syncIsServerNode() 读环境
配置 → 合并侧 b 变成 SERVER → 走 syncDeltaServerResolve (RowsResolved) 而非
legacy RowsConflict 分支。修法 (388be0a 同 pattern): t.Setenv pin
GOPI_SYNC_PEERS=peer-under-test; 断言改为双语义 — 显式报告 (RowsConflict+
RowsResolved==1) + 按 ruling 分支各自验证 (server: completed+sync-resolved
tag, 幂等 re-echo no-op; client: never-overwrite + conflict tag)。

## 验证
- 单测: TestSyncPatchVersionCompatSemantics (newer/-1/0/99 拒, v1 过),
  TestSyncImportNewerFormatPatchRefused (v2+future_field patch → 显式拒,
  DB 0 行), TestSyncImportVersionlessPatchRefused, TestSyncExportStampsGenerator。
  go test ./internal/tools/ -run "TestSync|TestBidir|TestHarvest|TestTaskSync" 全绿。
- 真二进制 E2E (/tmp/gopi-v2, v=128579f467aa-dirty):
  v2 patch → exit 1 "NEWER … upgrade this node's gopi binary";
  无 version patch → exit 1 "missing its version field";
  task v9 → exit 1; 三个拒绝场景 DB 文件根本未创建 (零写入);
  合法 v1 patch (带 generator: "128579f467aa-dirty" 头) → exit 0 正常导入。
- 环境预存失败 (与本任务无关, 干净 HEAD 同样 FAIL):
  TestEnsureTagsRegeneratesForNewerTS (Exuberant Ctags 5.9 无 TS/TSX 支持),
  TestMemorySearchNonBlocking (openStoreDB TOCTOU, 偶发), cmd/agent
  TestIsBrewSubcommand (brew 读真实 /home/user/.gopi/settings.json)。

## 语义备忘 (将来 bump 版本时)
bump syncPatchVersion → 旧二进制自动拒新 patch 并提示升级 (本闸);
新二进制读旧 patch: 拒绝 (got<want) — 如需读旧格式, 在对应 channel 显式
加迁移分支再放宽 got<want。generator 头不参与闸, 只做溯源。
