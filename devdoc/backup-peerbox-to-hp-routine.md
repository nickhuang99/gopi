<!-- doc-class: historical -->
# peerbox→HP 知识备份 routine（decisive-side=peerbox）

Date: 2026-09-09 · Epic: backup-peerbox-knowledge-to-hp · Branch: feat/export-gate-wide

## 目的

把 peerbox memory.db 的全部知识资产**内容级**同步/备份到 HP，使 HP gopi 的
`memory search` 能检索到 peerbox 的知识；**不覆盖 HP 自己的记录**；peerbox 为
decisive-side（冲突时 peerbox 权威——但本 routine 的同步语义=纯补缺，不做
覆盖动作，见下）。

## 两类动作

### A. live 知识行同步（distilled_qa → HP live memory.db）

    # 在 peerbox 上（decisive-side）：
    gopi memory sync --peer hp

语义 = unique-only superset（`gopi memory sync` 既有通道，2026-09-02）：
identity = exact question。peerbox 独有的 question → INSERT 进 HP（provenance
node:peerbox）；双侧同 question → 不动（HP 版本保留）；真冲突 → 不覆盖
（unique-only 通道本就不写已有行）。**export gate**（2026-09-09 扩展，
feat/export-gate-wide）：默认导出所有非 node:* 本地行——brew、hermes
导入、空 source 早期开发蒸馏全走；node:*（从别处导入的行）永不回流。
旧 gate 只导 brew（漏 ~1095 行共享知识，即本次 epic 修的缺陷）。

前置条件：
- 双侧二进制 ≥ feat/export-gate-wide（HP 侧只需 ≥ memory-assets-sync，
  import 路径未变；peerbox 侧必须有宽 gate 才能把非 brew 行推出去）。
- settings.json `memory_peers` 声明 peer（不是 sync_peers——
  单节点 task 模式 sync_peers=[] 不影响资产同步）。
- **先做 pre-live 快照**（回滚点，见 B 节的 HP 侧命令）。

验收（执行后）：
- HP distilled_qa 行数增长 ≈ peerbox 独有 question 数（双侧 question 集合
  差集；2026-09-09 实测 +404）。
- HP `gopi memory search "<peerbox 某条知识关键词>"` 能检索到。
- HP 原有行 0 lost / 0 content-changed（拿 pre-live 快照对 diff）。
- 无 node:* 回流（HP 上没有 peerbox 从 HP 导入的行的副本——peerbox 导入的行
  带 node:peerbox，不会再次导出）。

### B. 整库单向快照（tasks/chats/procedural/conclusions/蒸馏全表 → HP backups/）

task ledger **只备份不同步**（nick：HP 有自己的任务队列，单向即可）。用
SQLite online backup API（并发安全、无需停 gopi）：

    # HP 侧（对端收文件）：
    ssh hp 'cat ~/.gopi/memory.db' > /tmp/hp_mem.db        # 拉对端 live（可选，核对）
    scp ~/.gopi/memory.db hp:/tmp/zb_mem.db                # 推 peerbox 全库到 HP /tmp
    ssh hp 'python3 - <<EOF
    import sqlite3, os, time
    os.makedirs(os.path.expanduser("~/.gopi/backups"), exist_ok=True)
    stamp = time.strftime("%Y%m%d-%H%M%S")
    src = sqlite3.connect("/tmp/zb_mem.db")
    dst_path = os.path.expanduser(f"~/.gopi/backups/memory-peerbox-full-{stamp}.db")
    dst = sqlite3.connect(dst_path)
    with dst:
        src.backup(dst)
    dst.close(); src.close()
    print("SNAPSHOT", dst_path)
    EOF'

验收：对快照逐表 `SELECT COUNT(*)` 与源机核对
（distilled_qa/tasks/chats/procedural/session_conclusions）。
2026-09-09 实测：1488/1586/82/2/310 两侧一致。

回滚/恢复：任何同步前先 `python3` online-backup 一份 HP live DB 到
`~/.gopi/backups/memory-hp-prelive-<stamp>.db`（2026-09-09 已验证）。若
同步出问题，把该快照拷回 `~/.gopi/memory.db` 即可。

## 常见坑

- 别用 `memsync --export`/`gopi memory sync` 期待它搬 node:* 行——按设计
  永不导出（防回流环）。真备份被设计排除的行 = 整库快照（B 节）。
- HP hostname ≠ alias：同步用 ssh alias `hp`；若某命令在 HP 上要自称 hp，
  `export GOPI_SYNC_NODE=hp`（见 ~/.config/environment.d/ 或 ~/.profile）。
- `gopi memory_search` 已废弃——用 `gopi memory search`。
- 行数核对以 question 集合为准（同 question 双侧都有 = 不搬，
  +404 vs peerbox 独有 405 差 1 通常是 garbage gate 拒绝的测试行）。
