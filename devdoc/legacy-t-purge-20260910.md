<!-- doc-class: historical -->
# Legacy t-记录清理完成（GLM 夜间审查 → peerbox 应用）

日期 2026-09-10。执行者: GLM (HP night window) + PA (应用回生产库)。

## 流程（nick 风险隔离设计）
1. peerbox 快照 → HP `memory-glm-audit.db`（备份 DB——GLM 完全操作权）
2. peerbox 完整镜像到 HP（`~/.gopi/peerbox-mirror/`——db + sessions 2661 条目）
   ——GLM 查证"是否已被后继完成/有 git 证据"用镜像而非 HP 自身数据
3. 1281 条 t- 分 8 批（~160/批）+ batch-09 测试批
4. HP cron 23:00 起跑（GLM 免费窗口）——实际 03:37 修复脚本后启动，05:18 完成
5. PA 复核（抽样 20/20 合理）→ 应用回 peerbox

## 审查结果（8 批，GLM 决定）
| 批 | DELETE | KEEP | 其他 |
|---|---|---|---|
| 01 | 120（113 重复派发 + 7 过时）| 37 | UPDATE unsure 3 |
| 02 | 152（merge-backlog 8分支×20 重复）| 8（每分支留最新1条）| — |
| 03 | 160 | 0 | — |
| 04 | 160（128 重复派发 + 32 已闭环）| 0 | — |
| 05 | ~35（D1 簇）| — | — |
| 06 | 50 | 110（completed 可追溯）| — |
| 07 | 12 | 76 | UPDATE echo-mirror 72 |
| 08 | — | — | — |
| **合计** | **751** | | |

## 被删行画像（复核关键）
- 状态: **cancelled 687 (91.5%)** + completed 62 + claimed 2
- tags: **sync-yielded 558**（M4.18 时代"让渡"标记——单节点模式已废设计的噪音）
  + merge-backlog 类 ~51（serve branch-scan 循环派发的重复决议行）
- source: auto 667 + **human 66** + llm 18
- 抽样验证: 20/20 合理（测试探针行/writeback 覆盖摘要的僵尸行/
  重复派发行/被取代的旧单——如有 git 闭环证据的 completed bug 单）

## 关键发现：bureaucracy 的真实案例
**558 sync-yielded + ~51 merge-backlog 重复派发 = 系统自身的循环噪音**——
与 reframe/evaluate 讨论的"evaluate→submit→dispatch→re-evaluate"是
**同类问题**：机制设计导致的重复派发（serve branch-scan 对同批分支
反复 auto-submit merge/archive 决议）。清理掉的是机制产出的垃圾，
不是人的工作意图。

## peerbox 应用（2026-09-10 09:45）
- 备份: `~/.gopi/backups/memory.db.bak-pre-legacy-purge-20260910-094506`
- 删除 751 条（事务）: t- 行 1283 → **532**；tasks_fts 1614 → 863（触发器
  正常同步——无 legacy FTS 丢触发器问题）；零残留；抽样可检索 ✅
- 总行数 863（含 uuid 新行 331）

## 附
- 决定表: HP `~/.gopi/legacy-audit-decisions/decide-NN-delete.txt`
- 报告: HP `~/.gopi/legacy-audit-log-NN.md`（8 份，含双证据查证）
- HP 侧备份链: memory-glm-audit.db.bak-20260910-034528 起多份批前快照
