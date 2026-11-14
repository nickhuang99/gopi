<!-- doc-class: historical -->
# Routine: peerbox → HP 完整备份（代码 + 运行 DB + SQLite 快照）

> 作者: PA (MoneyPenny)。用途: 标准例行流程——未来 gopi/PA 直接照此执行，
> **不再花 10 分钟探索自己的代码**（2026-09-09 教训：上次 Bond 探索 10 分钟、
> 实际执行 5 秒）。
> 最后更新: 2026-09-09。实测耗时: 41MB 全量快照+传输 **5.3 秒**。

## 三层备份策略（完整覆盖）

| 层 | 资产 | 方法 | 频率建议 |
|---|---|---|---|
| **L1 代码** | git repo（GoPi/Maps 等）| HP git remote 已配 + push-back 自动（bug038）| 每次 commit（自动）|
| **L2 运行 DB 内容** | tasks 台账 / distilled_qa 知识（brew+hermes 导入+空 source）| `gopi task sync --peer hp` + `gopi memory sync --peer hp`（gate 已扩非 node:*）| 每任务完成/每日 |
| **L3 灾难恢复快照** | memory.db 全量（含所有表）| **SQLite backup API 快照 → scp HP**（本文档核心）| 重构/迁移/大改前必做 + 可 cron 每日 |

## L3: SQLite backup API 快照（为什么不用 scp 裸文件）

**背景**：memory.db 是 WAL 模式（Write-Ahead Logging）——数据分布在
`memory.db` 主文件 + `memory.db-wal`（最近未 checkpoint 写入）。gopi 进程
正在并发写库。

**❌ 错误做法**：`cp memory.db hp:` 或直接 scp 裸文件——漏掉 -wal 的最新
写入 = 不一致/损坏副本（实测 `database image is malformed`）。

**✅ 正确做法**：SQLite **官方在线备份 API**（`sqlite3_backup`——Python
`sqlite3.Connection.backup()` 封装）：
1. 从 SQLite 内部**逐页复制**（含 -wal 全部数据）
2. **处理并发**（复制中 gopi 继续写——读到一致逻辑状态，-wal 增量并入）
3. 产出 = **自包含一致快照文件**（某一时刻数据库的完整状态）
4. **无需停 gopi**（在线热备）

## 标准命令（直接复制执行）

```bash
# 1. 快照（本机，秒级）——改 TS 变量区分版本
TS=$(date +%Y%m%d-%H%M%S)
SNAP=~/.gopi/memory.db.peerbox-backup-$TS
python3 -c "
import sqlite3, sys
src = sqlite3.connect('/home/user/.gopi/memory.db')
dst = sqlite3.connect('$SNAP')
src.backup(dst)   # ← SQLite backup API（在线一致快照）
dst.close(); src.close()
print('snapshot ok: $SNAP')
"

# 2. 传输到 HP（局域网，秒级）
scp -q "$SNAP" hp:~/.gopi/backups/

# 3. HP 侧验证（表数一致——抽查）
ssh hp "sqlite3 ~/.gopi/backups/$(basename $SNAP) 'SELECT COUNT(*) FROM distilled_qa'"

# 4. 本地清理（可选——保留最近 N 份）
rm -f "$SNAP"   # 已传 HP——本地可不留
```

## 实测耗时（2026-09-09, memory.db 41MB）

| 阶段 | 耗时 |
|---|---|
| backup API 快照 | ~3 秒 |
| scp 到 HP（局域网）| ~1-2 秒 |
| HP 验证 | 即时 |
| **总计** | **~5.3 秒** |

**推论**：备份本身极快——**可高频做**（重构前必做 + cron 每日，5 秒成本
几乎免费）。DB 增长到 1GB 估计 <2 分钟仍可接受。真正的成本是"决定备份
什么"的认知开销——本文档消除之。

## L1 + L2 快速参考

```bash
# L1 代码：HP 已有 peerbox remote——Bond push-back 自动（bug038 机制）
# 手动：git push peerbox <branch>（从 HP 侧）

# L2 内容同步（单节点拓扑需显式 peer env——settings 声明单节点）
GOPI_SYNC_PEERS=hp ./bin/gopi task sync --peer hp        # tasks 台账（双向）
./bin/gopi memory sync --peer hp                         # 知识行（gate: 非 node:*）
```

## 恢复流程（灾难时）

```bash
# 从 HP 快照恢复 peerbox：
scp hp:~/.gopi/backups/memory.db.peerbox-backup-<TS> ~/.gopi/memory.db.restored
# 停 gopi → 用 restored 替换 memory.db（或 --db 指向验证后再换）
sqlite3 ~/.gopi/memory.db.restored "PRAGMA integrity_check"   # 先验证
```

## 验证清单（备份后确认）

- [ ] HP backups/ 有新快照文件（带日期 TS）
- [ ] HP 快照表数与 peerbox live 一致（distilled_qa/tasks/chats/procedural 抽查）
- [ ] L2 sync 输出无 error（pushed/imported 计数合理）
- [ ] L1 git: HP master 与 peerbox 对齐（`ssh hp git -C ~/work/GoPi log -1`）

## 历史教训（为什么有本文档）

- 2026-09-09: 备份 Bond 探索 10 分钟（101 工具调用查 gopi CLI 有无 backup
  verb）→ 实际执行 5 秒。**结论：routine 操作必须有文档**——PA (MoneyPenny)
  负责维护。Bond 上次探索的结论：gopi CLI **无整库 backup verb**（设计如此
  ——备份是运维操作非 CLI 功能）；sync 通道（task/memory）是内容级非文件级。
