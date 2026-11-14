<!-- doc-class: normative -->
# Chat Table 设计单：chats 表 + 钩子分流 + 存量迁移（feat/chat-table）

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> 结论先行：**Epic ①②④⑤ 已由 M4.6（feat/chat-remote-dispatch, a4ba5cc，merged 99e5893）落地**；
> 本单的实证增量 = ③ 迁移真实执行（110/110 行迁入真库）+ ⑥ 回滚方案成文
> + 一处**交付缺陷修复**（backfill CLI verb 分发 bug——不修则 ③ 永远无法执行）。
> Schema 与钩子分流的设计裁决已在 devdoc/task-ledger-m46-47.md 记录，本文引用并补
> 与 M4.5 的并窗口论证。

## 1. 背景与本体论证（Schema 对比）

台账 116 行中 chat auto 行占 79%（92 行当时，现存 [auto] 行 125 条）且队列列全部恒空。
chat 会话与 execution task 是**不同本体**：

| 本体 | 生命周期 | 生命周期由谁推进 | 需要的列 |
|---|---|---|---|
| execution task | pending→claimed→in_progress→completed/failed | 外部 agent（claim/CAS/verify 链） | 13 列队列契约 |
| chat session | 生而 running，止而一次性 ended（幂等） | 会话自身（单进程） | 纯事实列 |

真共通仅 `session_id / model / ended_at` 3 列——共享一张表等于让 chat 行背着
12 列永假队列契约。chats 表因此**全事实列、无状态机**（`status` 列不存在，
`(mode, started_at)` 索引即检索面；会话"进行中"= `ended_at IS NULL`）。

### 1.1 Epic 逐条裁决（现状）

| Epic 点 | 裁决 | 证据 |
|---|---|---|
| ① chat 表 schema（session_id PK / mode / model / first_msg / turn_count / input_tokens / output_tokens / cache_tokens / workdir / log_path / started_at / last_msg_at / ended_at + 预留 brewed_at） | **已落**（M4.6） | `internal/tools/chat.go` chatsTable()：18 列逐列对上 Epic 清单，另增 title / parent_session_id / git_branch / git_repo_root / end_reason（hermes 40 列对比逐列裁决，见 task-ledger-m46-47.md）。brewed_at 预留列在列 |
| ② taskregister 钩子分流 | **已落**（M4.6） | `internal/agent/chatregister.go`：RegisterChatSession / RecordChatTurn / CloseChatRow；taskregister.go 的 runEngine 路径零改动（run/serve child 本就该写 tasks）；chat CLI/web 入口已改调 chat 侧钩子 |
| ③ 迁移 92→110 条存量 auto 行 | **本单完成** | `gopi chat backfill`（tools 层 ChatBackfill 早已实现，CLI 门修复后可执行），110/110 迁入真库 ~/.gopi/memory.db |
| ④ 检索面 | **已落**（M4.6） | task search 不含 chat（TestChatSearchIsolationFromTasks 锁死）；`gopi chat list [query] [--model] [--limit]` 已可用；FTS5 对 first_msg 已建伴生索引（chats_fts/chats_tri），检索留待后续 |
| ⑤ M4.5 并窗口 | **已消化** | M4.5 先 merge（master c8423b6，tasks 表存活列已落真库 schema）；本单在同一窗口只碰 cli_chat.go 分发点，未触碰 taskregister.go/tasks schema |
| ⑥ devdoc | **本文** | schema 论证 + 迁移步骤实录 + 回滚方案 §4 |

### 1.2 log_path 指针裁决

JSONL 物理日志（~/.gopi/sessions/）仍是**权威全文**，DB 只存指针 + 索引面。
hermes messages_fts 撑出 4.3GB 大库的教训：**存储时预处理 > 查询时实时理解，
但全文不复制**。first_msg 只存首条 user 消息作为检索锚点，全文检索留给 JSONL。

## 2. 钩子分流（②的现状）

```
gopi run / gopi task run / gopi serve ──runEngine──► tasks 表（[auto] 行，队列契约）
gopi chat / gopi web ────────────────Chat 路径──► chats 表（事实列，无状态机）
```

- START: `RegisterChatSession(mode, firstMsg, firstMsgSource)` —— 首个成功 turn 落行
  （session_id PK）。`first_msg` 只由**操作员本人的首句**占据（face 取自
  `internal/inputsource`，判定在 `internal/tools/chat_first_msg.go`）；注入的 notify /
  job-notice 文本不写标题列，落显式占位符 `(no operator line yet)`，之后
  `OfferChatFirstMsg` 让操作员一开口就把占位符换成真句。`--repair-first-msg`
  （`gopi chat backfill`）从权威 session JSONL 重推历史行。
- 每turn: `RecordChatTurn(prompt, completion, cache)` —— token **MAX 折叠单调**
  （重复/乱序调用不会缩水）；turn_count +1；last_msg_at 翻动
- END: `CloseChatRow(exitCode)` —— ended_at + end_reason（0→normal，非零→interrupt），幂等
- `GOPI_CHAT_DB`：chat 索引的 DB 覆盖（默认同库 memory.db，Rule 2 单点读取）
- 开关：GOPI_AUTO_TASK_REGISTER 只管 tasks 侧；chat 侧无独立开关（web server
  与 chat CLI 都在 agent 构造后防御式初始化 Cost，注册条件 runErr==nil && Cost!=nil）

分流信号裁决（m46 devdoc）：按 **CLI 入口面**分流——chat 进程写 chats，
runEngine 路径写 tasks。`task run` 的 child 是 runEngine 进程，写 tasks 表正确
（execution 本体）；无需 run-child 抑制信号。

## 3. 迁移实录（③，2026-08-30 于真库执行）

候选界定：`summary LIKE '[auto] %' AND session_id != ''`（110 行）。
真库 238 tasks 行拆解：125 [auto] 行 = 110 带 session（→chats）+ 12 completed 无
session（无证可迁，留台账）+ 2 pending + 1 failed（同无 session 留台账）；
另有 3 行已打 backfill 标记但 **session_id=''**（历史无值遗留，无可恢复事实）。

步骤（真库执行前先全流程演练于副本）：

1. **副本验证**（最小样本原则）：`cp ~/.gopi/memory.db /tmp/memdb-chatcopy.db`
   → `GOPI_MEMORY_DB=/tmp/memdb-chatcopy.db ./bin/gopi chat backfill --dry-run`
   （110 候选）→ apply → `PRAGMA integrity_check`=ok → 重跑 =0 候选（幂等）→
   抽查一行：chats 行的 model/turns/tokens/first_msg 与 session JSONL
   （sessions/<sid>.jsonl 首行 session_start 事件）逐字段对上。
2. **备份**：`cp ~/.gopi/memory.db ~/.gopi/memory.db.bak-chat-backfill`（27,246,592 B）
3. **dry-run 真库**：`migrated=110 skipped(unreadable-log)=0 dryrun=true`
4. **apply 真库**：`migrated=110 skipped=0` → integrity ok → chats=110 →
   tasks backfill 标记 113（110+历史 3）
5. **幂等复跑**：0 候选
6. **检索面实测**：`gopi chat list --limit 3` 正常渲染（mode/model/turns/tokens/时间/首句）

facts 重推来源（权威=JSONL）：first user message（first_msg）、`request` 事件
prompt_tokens/cache_hit+cache_miss/completion_tokens 求和（token 三件套）、
user 消息计数（turn_count）。JSONL 丢失的行以 summary 推 first_msg 裸导入。

## 4. 回滚方案（⑥）

分两层，独立可执行：

**层 1：撤销迁移数据**（不动代码）
```bash
cp ~/.gopi/memory.db.bak-chat-backfill ~/.gopi/memory.db
# 或 surgical（保留迁移后新产生的行）：
sqlite3 ~/.gopi/memory.db "DELETE FROM chats; UPDATE tasks SET summary = REPLACE(summary, ' — chat: backfilled', '') WHERE summary LIKE '%backfilled';"
```
（备份位于 ~/.gopi/memory.db.bak-chat-backfill，27MB；DELETE chats 幂等安全——
chats 是纯事实表，无外部引用；REPLACE 撤标记让行重新可迁。）

**层 2：退役 chats 路径**（代码回退，恢复单表时代）
```bash
git revert a4ba5cc   # M4.6 chats 表 + 分流 hook 的引入 commit（勿 revert 本单的 bug fix）
```
回滚顺序：先停 chat/web 前端进程 → revert → 重建二进制。tasks 表不受影响
（chat 行 ② 分流后不再进 tasks；回滚后新 chat 会回到 [auto] 行形态）。

**边界**：chats 表本身 `CREATE TABLE IF NOT EXISTS` 幂等，层 1 后若再跑新版
gopi chat 会重新落表——层 1 是数据层撤销，层 2 才是功能退役。

## 5. 交付缺陷修复：backfill CLI verb 分发 bug

**现象**：`gopi chat backfill` 任何情况下只打印 "No chats found."（list 分支
输出），迁移永远不执行——tools 层 ChatBackfill 是死代码。

**根因**（5-why：现象→机制）：isChatSubcommand 派发 `runChatList(args[2:])`
把 verb 砍掉；而 runChatList **自己用 rest[0] 分支 list|backfill**——无 verb 的
尾巴落进 `verb==""` → list 分支。写 tools 层与写 CLI 层的人对"谁消费 verb"的
契约理解相反，一层裁一层不裁。

**修复**：新增 `chatIndexArgs(args) (tail, ok)`——isChatSubcommand 派发时
**保留 verb**（传 args[1:]），runChatList 继续自消费 rest[0]。
回归测试 `TestChatIndexArgs`（6 case：backfill/list 保 verb、REPL 形态、
未知 verb、空 argv）锁死契约。

**验证**：副本 dry-run 110 → apply 110/110 → 幂等 → 真库迁移（§3）。

## 6. 未完成 / 后续

- **FTS 检索面**（④ 的"可后续加"）：chats_fts/chats_tri 已随表建立（external
  content + 触发器），`gopi chat list` 现走 LIKE；MATCH 化照抄 tasks_fts 三层
  召回即可，非首期。
- **title 生成器**：列已建，生成器后续。
- **无 session 的 [auto] 行**（12 completed + 2 pending + 1 failed，共 15 行）：
  无证可迁，留台账原样（append-only 原则，不删）。
- **brewed_at 消费**：M3 brew 蒸馏登记列已预留，等 brew 管道写它
  （M3 输入源从扫目录升级为读表时启用）。

## 7. Verification

- Build: `CGO_ENABLED=0 go build ./...` OK（二进制 bin/gopi 重建后 backfill 可达）
- Tests: `go test ./cmd/agent/ -run 'TestChatIndexArgs|TestIsChatSubcommand' -count=1` PASS
- 迁移: 真库 110/110, `PRAGMA integrity_check`=ok, 幂等复跑 0 候选, chat list 渲染正常
- Commit: (见交付声明)

## 8. 检索面包屑

chat table chats 表 schema 会话台账；backfill 迁移 auto 行 chats 表；verb 分发
bug runChatList args[2:]；No chats found 假象；钩子分流 chatregister
RegisterChatSession RecordChatTurn CloseChatRow；GOPI_CHAT_DB；回滚 memory.db
backup 撤销迁移；M4.5 M4.6 并窗口 taskregister.go。
