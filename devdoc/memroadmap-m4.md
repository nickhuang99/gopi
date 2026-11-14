<!-- doc-class: normative -->
# M4 — 跨节点知识库同步（HP brew 行回灌 peerbox）

分支: `feat/memroadmap-m4`（HP 节点 `~/work/GoPi` 直接开发，基线 merge 了
peerbox/feat/memroadmap-m3 的 621a60e..8b21c03..3568457）

## 设计题与三方案对比

问题：HP 上 brew 蒸馏出的 lesson 行（`distilled_qa`，sources=brew）如何
回灌 peerbox 主库——两库同源（同一 hermes distilled.db 血统，各 ~27MB），
但 id 已各自漂移，双向都会继续增长。

### 方案① rsync 整库替换 —— 否决

- 整库文件级覆盖：HP 库拷到 peerbox 后，peerbox 在同步窗口期间的本地写入
  （memory_store / memory_feedback / tier 更新）被整体抹掉——**数据丢失**，
  不是"风险"是必然。
- id 空间冲突：两库 AUTOINCREMENT 各自前进，同一 id 已是不同内容；
  覆盖后 peerbox 本地引用（feedback 事件按 memory-id 审计）全部错位。
- 无合并语义：rsync 是"替换"不是"合并"，与 memory 库 append-mostly 但
  双写（store + feedback）的现实根本不匹配。
- 唯一优点（简单）在 ATTACH/patch 方案面前不成立——两者实现量相当。

### 方案② SQLite ATTACH 行级合并 —— 否决（作为通道；保留为运维工具）

- 技术上可行：`ATTACH remote.db; INSERT OR IGNORE SELECT ... WHERE
  question NOT IN (SELECT question FROM main.distilled_qa)`。
- 三个硬伤：
  1. **ssh + sqlite3 远程执行**——ATTACH 必须在同一 sqlite 进程里同时
     打开两库，跨节点就得先把远端库拉过来（又回到整库传输），或者
     在 peerbox 上跑 import 脚本（通道逻辑跑到 SQL 里，无法做防自指/
     冲突裁决这类应用层判断）。
  2. **id 碰撞**:INSERT 时 id 冲突需要 OR IGNORE/重排，而 FTS5
     external-content 表 rowid 对齐 id，重排 id 必须同步重建 FTS 行
     ——在 SQL 里做等于把 MemoryStore 的写路径复制一份裸 SQL 实现。
  3. **绕过了应用层语义**：garbage gate（isGarbage）、dedup（exact +
     trigram-Jaccard）、sources merge、5W1H 列填充……全是 MemoryStore
     的职责。SQL 通道要么重复实现（漂移），要么放弃（脏数据）。
- 结论：ATTACH 适合**一次性对账/repair**（PA 运维），不适合日常同步
  通道。

### 方案③ git-PR 语义 patch（实现并推荐）✅

- HP `gopi memsync --export --out patch.json` → patch 只含**本节点的
  brew 行**（tag 闸门），不含 id。
- patch 文件人工/自动传到 peerbox（scp/共享盘/git 都行——通道与传输
  解耦，正是 PR 的精神：patch 即 PR diff）。
- peerbox `gopi memsync --import patch.json` → 每行走 **MemoryStore 原
  路径**：dedup（exact + 相似度）→ merge（sources 集合并）→ garbage
  gate → FTS 同步。重复 import = no-op（幂等）。
- 为什么与 push-back 通道同构：HP 本来就 `git push peerbox
  feat/memroadmap-m4` 交付代码；memsync 用同样的"本地产出 → 远端
  审阅合并"模型交付知识行。一个心智模型，两条管道。

### 关键裁决（实现中确定）

1. **sources 不进 patch**：provenance 在 import 侧强制打
   `node:<exporter-hostname>`（patch.Node 字段），不信任文件自带
   tags——防伪造/防漂移。
2. **export 双闸门**：`sources LIKE %,<tag>,%` 且 `NOT LIKE %,node:%`
   ——brew 行才出门，导入行（node:*）永不二次导出（无 ping-pong）。
3. **人工行永远赢**：import 前查 question 是否已被**非同步行**
   （无 brew/node 标签）持有——命中则 REFUSE 该 patch 行（计入
   refused，不是 deduped）。MemoryStore 的 (question, topic) 键在
   topic 不同时会 INSERT 影子行，这一层 import 侧守卫补上了缺口。
4. **发现并修复 MemoryStore dedup 真 bug**（见下）。

## Completed

- `internal/tools/memory_sync.go`：SyncExport / SyncImport / patch
  格式（versioned, v1）/ SetMemoryDBPath。
- `cmd/agent/cli_memsync.go`：`gopi memsync --export|--import` 子命令
  （cli_brew.go 模式注册进 main.go 分发链）。
- **MemoryStore exact-dedup 修复**（`internal/tools/memory.go`）：
  键宽是 question[:200]+topic，但存储的是全文——dedup 查询原来拿
  200-rune 探针比 217-rune 全文列，**>200 rune 的行永远无法 exact
  去重**（m4 验收实测：217-rune 的 brew 行 re-import 时复制成两行）。
  修复：查询改为 `substr(question,1,200)=<探针>`；相似度预筛同样先
  截到 200 rune（与 exact 键同一探针，行为确定）。
  回归测试：`TestMemoryStoreLongQuestionDedup`。
- `listSessionJSONLs` 容忍缺失的 sessions 目录（全新 GOPI_HOME 是
  "nothing to brew" 而非 error——hermetic 测试暴露的第二个坑）。
- `TestIsBrewSubcommand` 加 `t.Setenv("GOPI_HOME", t.TempDir())`：
  原测试无隔离，在 HP 上 isBrewSubcommand 真执行 → 扫真实 sessions →
  打真 API → 5 分钟超时 panic（m3 测试设计缺陷，HP 环境暴露）。
- 测试 7+3：export tag 闸门/幂等双导/node 标记/冲突裁决/garbage
  拒收/版本守卫/长问题 dedup + CLI 匹配/usage 文档化。

## Not Done（诚实清单）

- 自动传输（scp/cron 定时 push patch）未做：当前 patch 文件由人/PA
  携带（scp 一行命令）。自动调度属运维动作，接口已就绪。
- peerbox 侧 import 无 CLI 审阅 UI（`--dry-run` for import 未做）：
  refused/deduped/stored 全量回显已足够人工裁决；dry-run 留待需要时。
- 反向通道（peerbox→HP 补行）未做：epic 范围就是 HP brew 行回灌 peerbox；
  反向复用同一 patch 格式即可（--source 过滤任意 tag）。
- tier/confidence 沿袭策略未做：import 行走 MemoryStore 默认
  warm/0.85，与 brew 本地行为一致；跨节点信任加权留 M5。

## Verification

- Build: `go build ./...` OK；`CGO_ENABLED=0 go build -o bin/gopi
  ./cmd/agent/` OK；`go vet` OK（除 HP 缺 rg 的环境性用例）。
- Tests（本任务相关全部通过）:
  - `go test ./internal/tools/ -run 'TestSync|TestMemoryStoreLongQuestionDedup'` → 8/8 PASS
  - `go test ./internal/tools/ -run 'TestMemoryStore|TestMemorySearch|TestStoreBrew|TestRunBrew|TestLoadSession|TestSlice|TestJudge|TestDistill'` → PASS
  - `go test ./cmd/agent/ -run 'TestIsBrew|TestBrewUsage|TestIsMemsync|TestMemsyncUsage'` → 5/5 PASS
  - 全套件 `go test ./...`：internal/tools、cmd/agent（除下述 1 项）全 ok；
    5 个失败全部为 **HP 环境既有问题**，与 m4 无关——已用父提交 cfe590a
    的独立 worktree 复现确认：
    - `TestCodeSearch*` ×5（cmd/agent）：HP 缺 rg + ctags 选项不兼容；
    - `TestAgent_Run_ContextTrimReplaceSlimming` /
      `TestAutoTaskRegisterFailedStatus` /
      `TestChatTaskRegistration_WrapperFailedStatus` /
      `TestAgent_Run_ToolResultPayloadOnce`（internal/agent）：父提交
      上同样失败。
- **端到端实测（epic 验收线）**：
  1. HP 真库造 3 条 brew smoke 行（id 1130-1132）。
  2. `./bin/gopi memsync --export --out /tmp/m4-hp-brew-patch.json`
     → 6 行（3 smoke + 3 条 m3 蒸馏的真 lesson），node=hostb-Laptop。
  3. scp 拉取 peerbox 真库副本（1078 行，0 行带 node 标签）。
  4. import → `stored=6 deduped=1 refused=0`（deduped=1 是与 m3 时期
     同 question 行的合法合并）；修复后净库重放 3 次：
     `stored=6 → deduped=6 → deduped=6`，行数恰 6——**幂等成立**。
  5. `memory_search "m4 sync smoke"` 在 peerbox 副本上命中 #1136/#1137
     （tokens=3，sources=node:hostb-Laptop）；中文问题跨节点检索
     同样命中。检索确认 ✅
- 修复前后的差异证据：第一次 import 曾 `stored=5 deduped=1`（长问题
  行 1132 漏 dedup 成新行）——引擎修复后同 patch 重放全 deduped。

## 已知坑（给未来 agent）

1. `.gitignore` 裸模式 `agent` 吞 `cmd/agent/` 新文件——`git add -f`
   （m3 已记录，m4 再次踩到：cli_memsync.go 对 git 隐身）。
2. SQLite REAL 列（distilled_at）scan 必须进 float64 再转 int64。
3. 跨库同步的幂等性依赖**引擎 dedup 的键宽一致性**：存储全文、
   dedup 截断的组合必须显式对齐（substr），否则 >键宽 的行永远
   重复。任何"存储值 ≠ 匹配键"的字段都要查一遍。
4. CLI 子命令测试必须 t.Setenv(GOPI_HOME, TempDir)——子命令是真执行
   的（会碰真库/真 API/真网络）。
