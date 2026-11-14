<!-- doc-class: normative -->
# M4.5 台账治理包 — 七单连做 devdoc (feat/task-ledger-m45)

> **2026-08-30 退役注**：①号单（t-1788036628，task ingest 跨节点镜像）
> 已由 EPIC #185 退役——db-sync 架构转向后联邦镜像前提消失，由 memsync
> task ledger 通道替代。见 devdoc/task-ingest-retirement.md。

> 单号：t-1788038361 ④ / t-1788039140 ⑤ / t-1788039618 ⑥ / t-1788037374 ③ /
> t-1788036918 ② / t-1788036628 ① / t-1788040730 ⑦
> 分支：feat/task-ledger-m45（worktree ~/workspace/gopi-ledger-m45，基 master af7cafb）
> 提交：c84d9f0 (chunk1 ④⑤⑥③②) → 8a2800c (chunk2 ①⑥⑦) → 57c25ee (E2E fix)

## Completed（按执行顺序）

### ④ task new 整体删除 (t-1788038361)
- `cmd/agent/cli_tasks.go`：删除 new 分支 + runTaskNew 调用点（原本就是直接
  switch case，一并摘除 taskFlagMember 的 new 旗标组与 tasksOptions.model）。
- 删除后的 `gopi task new` 给出指路式报错（exit 1）：
  `removed (M4.5) — use "gopi task submit <summary>"`（taskRemovedAdvice）。
- `internal/config/help.go` + tasksUsage：无 task new 字样。
- `cmd/agent/commands.go`：/task new slash 分支删除（同语义 /task next 一起
  给出 removed 报错）；taskCommandUsage 重写。
- **Go 路径保留**：taskregister.go 的 tools.TaskNew 原样（harness 自动登记
  不经 shell 层）——实测起真实 gopi 会话仍生成 [auto] 行。
- 测试：cli_tasks_test.go new 用例改写为删除断言（unknown subcommand +
  advice 指路）。

### ⑤ task update 收窄 status-only (t-1788039140)
- `tools.TaskUpdate(taskID, status)` 双参签名（原三参 summary 摘除）。
  summary 非空即报错的要求以**签名级删除**实现——比运行时校验更硬：CLI
  根本无法把 summary 传进来（--summary 旗标已摘，taskFlagMember 拒绝）。
- Harness 内容路径单开：`TaskUpdateWithResult(taskID, status, newSummary)`——
  仅 task run 回写与 taskregister 结束钩子使用（运行统计追加），CLI/slash
  不可达。错误文案含 "task content is immutable — cancel and re-submit"。
- FTS 触发器不动（tasks_au 自动同步）；update status 后 summary 词条仍在
  索引——内容没变，正确语义。
- 测试：TestTaskUpdateSummaryOnly 语义翻转（原"可改"断言改为"拒绝"断言）。

### ⑥ task next 收缩 + claimed 瞬时化 (t-1788039618)
- CLI next 分支删除（正门 = `task run next`：TaskNext CAS → TaskStart →
  执行 → 回写一气呵成）；removed 报错指路 run next / serve。
- claimed 降级为瞬时内部态：usage 文案明示"只在 CAS 与 TaskStart 之间毫秒
  级存在，唯一持久化正当理由 = 崩溃恢复"。
- **崩溃恢复**：`tools.TaskResumeClaimed`（claimed 行重打 claim trail）+
  `task run <id>` 显式目标自动尝试恢复（recovered claimed task 提示），
  随后 TaskStart → 正常回写。兜底 `update --status pending` 保留（迁移表
  claimed→pending 本就合法）。
- --as 旗标在 run/serve 保留；claimed_by 列保留（CAS 竞争者记录）。
- help 命令清单收敛：list|show|update|search|submit|run|serve|archive|ingest。
- 测试：TestCrashRecoveryResumeClaimed（人造 claimed 行→恢复→跑完→列表无
  claimed 残留）；TestNoClaimedLingersAfterRunNext（正常 run 后零 claimed）。

### ③ serve 守护化 (t-1788037374)
- flock 单实例：`acquireServeLock()` 对 `~/.gopi/serve.lock` 取
  LOCK_EX|LOCK_NB；文件写入 holder pid；第二个 serve 拒绝并报
  `another gopi task serve is already running (lock … held by pid N)`，
  exit 1。进程退出（含被杀）内核释放锁。实测：双 serve 并行 → 第二个拒绝。
- `--skip-tag TAGS`（逗号分隔）：`tools.TaskNextFiltered` 让携带任一 skip
  tag 的 pending 行不被该 claimer 领取（不误吞 pa-queue 行）。run/serve
  都走 TaskNextFiltered（run 无 --skip-tag 旗标时等价 TaskNext）。
- serve 锁在 runTaskServe 进入循环前获取；--once 单飞也持锁。

### ② 墓碑归档 (t-1788036918)
- **tags resolved 方案**（少 schema 变更，payload 倾向采纳）：
  `tools.TaskArchive(taskID)` 把 "resolved" 追加进 tags（normalize 后）；
  幂等（已归档 → "already archived" no-op）；非终态拒绝（pending/claimed/
  in_progress 不能标 resolved——归档是墓碑不是逃队列捷径）。
- CLI：`gopi task archive <task_id>`；failed 视图默认只显示未处置：
  `TaskList` 对 resolved 行追加 " [resolved]" 标记，
  `tools.FilterArchivedRows` 在 CLI list（无 --all）侧滤除；全滤空时收敛
  为 "No tasks found."（E2E 发现的悬空计数行问题，57c25ee 修复）。
- 审计能力保留：archived 行 show/search 照常可达（实测 search 命中墓碑）。
- **首批 6 条历史 failed 墓碑已归档**（真实 ~/.gopi/memory.db）：
  #102 8ef3003b…、t-1788031086(#101)、b4b6b43c…、b1fddd7e…、7d0507eb…、
  459a4263… → 归档后 `task list --status failed` = "No tasks found."，
  --all 显示 6 条 [resolved]。

### ① ingest 接收端 (t-1788036628)
- 新文件 `internal/tools/taskingest.go`：
  - `TaskIngest(ingestPayload)`：接收 M3 通知钩子 payload 格式
    （event/task_id/status/summary/elapsed/tokens/… + remote_node）。
  - **防伪造**：本地不存在的 task_id 拒绝（HTTP 404 语义）——镜像只能
    标注本地已有行。
  - **前向迁移限定**：仅非终态本地行 → completed/failed/cancelled（跨
    节点语义比本地状态机宽：本地行停在 pending 时远端已跑完，允许终态
    跳变；非终态互转一律拒绝——那些是本地队列状态）。已终态行再收不同
    终态 → 409 forward-only。终端→同状态 → 幂等 "already"。
  - **幂等 upsert**：重复投递 no-op（200）；stats 摘要只追加一次
    （"(from <node>)" 守卫）；source 列不动，发送方记录为
    `remote:<node>` tag；ended_at 取远端 updated_at（可信时）。
  - `TaskIngestHTTPHandler`：POST /task-ingest，拒绝映射
    400/404/405/409；成功（含幂等重投）200。
- CLI：`gopi task ingest --listen ADDR`（默认 :8923，ReadHeaderTimeout
  10s 常量）。HP 侧只需配置
  `GOPI_TASK_NOTIFY_WEBHOOK=http://<peer1>:8923/task-ingest` +
  `GOPI_TASK_NOTIFY_EVENTS=completed,failed`——配置即用，零代码。
- E2E 实测（本机 18923）：known row 镜像成功（tags remote:hp-test、
  stats 追加一次）；unknown → 404；重投 → 200 already；
  failed→completed → 409。payload 里 status 缺失时回退 event 字段。

### ⑦ 存活追踪 (t-1788040730)
- 新列（ALTER TABLE 幂等迁移 + schema 字面量 + rebuild 拷贝链 +
  scanTask/scanTaskRow 17 列投影）：`tasklist_issued_at`、
  `last_toolcall_at`（均为 REAL NULL 原始事实）。
- 写入点（internal/agent/taskheartbeat.go，best-effort 永不阻塞循环）：
  - `todoState.update()`（tasklist 块到达）→ touchTasklistIssuedAt。
  - `executeToolBatch` / `executeToolBatchParallel` 入口 →
    touchLastToolcallAt（每次工具调用 = 活着信号；30s 节流——列只需
    分钟级新鲜度）。session id 在 runEngine 注册行后 latch。
- **stale 检测（查询侧，无新守护进程）**：
  `tools.DetectStaleTasks(thresholdSec)`——SELECT status=in_progress AND
  COALESCE(last_toolcall_at, created_at) < now−threshold；命中 → event=stale
  走现成 GOPI_TASK_NOTIFY 管道（需 GOPI_TASK_NOTIFY_EVENTS 含 stale 或
  all）。阈值默认 30min（常量），GOPI_TASK_STALE_SEC 秒级覆盖（Rule 2
  单 reader）。接点：`task run`/`serve` 每次派发前扫一遍（含空队列）。
- E2E 实测：真实 gopi 会话 last_toolcall_at 落列（tasklist_issued_at 需
  LLM 发 tasklist 块，纯 bash 任务未触发——机制单元测试覆盖）；
  kill -9 的孤儿行（in_progress，updated_at 停在创建时刻）被
  `task run` 前置扫描检出并投递 `{"event":"stale",...}` 到 notify 目标。
- 附带修复达成：payload ④ 的现役 in_progress 僵尸行自然可检出。

## 冲突决策（epic 内裁决记录）
1. **⑤ 签名级收窄 vs 运行时校验**：payload 允许"保留签名但 summary 非空
   报错"。选了双参签名 + TaskUpdateWithResult 单独通道——CLI 物理上无法
   传 summary，比报错更硬；同时 harness 统计路径不受影响（它的 summary
   写入是合法生命周期动作，不是内容编辑）。
2. **⑥ next 降级为 --debug vs 删除**：payload 给了两案。选删除 + 指路
   报错（与 ④ 一致的 removed 语义；--debug 隐藏面会留下第二套心智）。
   tools.TaskNext(Filtered) Go 函数保留（run/serve 的引擎）。
3. **② 过滤位置**：TaskList 引擎侧 vs CLI 侧过滤。选 CLI 侧
   （FilterArchivedRows）——引擎渲染保持全量真值，--all 与 show/search
   零成本可达；[resolved] 尾标是渲染层标记不是数据。
4. **① 远端连接 vs HTTP 接收**：--db 跨节点连接不做（SQLite 文件锁语义
   不允许），HTTP ingest 是"PR-style patch"：远端提议、本地按自己的规则
   （防伪造/前向/幂等）裁决落账。
5. **⑦ stale 触发面**：不加守护进程（payload 明确"查询侧策略"），扫
   描挂在 run/serve 派发前（serve 的 tick 天然周期化）。

## Verification
- Build: `go build ./...` OK；`go vet ./...` OK（CGO_ENABLED=0）。
- Tests: 全量 `go test ./... -count=1` 首轮 4 红（全部本分支引入/暴露，
  已修+复跑）：
  1. TestAgent_Run_ToolResultCarriesTick panic——runEngine 新加的
     heartbeat latch 未防 nil SessionLog（最小 test Agent 无 logger）；
     修复：SessionLog nil 守卫（heartbeat 本就是 best-effort）。
  2-4. TestSlashTaskRoundTrip / TestWebChat_R4_ChatREPLSlashRouting /
     TestTopLevelUsageShowsTaskLedger——断言仍期待 /task new 与
     "gopi task new" 字样；按 ④ 语义翻转为删除断言/submit 引擎路径。
  复跑 cmd/agent + internal/agent 全绿（job-2，见下方补录）；
  其余包首轮已 ok（internal/tools 232s ok 全量）。
- bug027 顺手修复：taskregister_test.go 两处 5-space 断言改为
  `regexp: status:\s+failed` 宽松断言（bug 报告首选方案 a），master 现存
  2 个预存红消除。
- 关键单测：TestTaskArchiveResolved / TestFailedViewHidesArchived /
  TestFilterArchivedRowsEmptyResult / TestAcquireServeLock /
  TestServeSkipTagFilter / TestCrashRecoveryResumeClaimed /
  TestNoClaimedLingersAfterRunNext / TestTaskIngestMirror /
  TestTaskIngestHTTPHandler / TestTouchLivenessColumns /
  TestDetectStaleTasks / TestStaleNotifyDelivery / TestTasksUsageIsAppendOnly
  （M4.5 新增+改写 13 个）。
- CLI 实测（/tmp/m45-e2e 隔离库 + 真库归档）：
  - `task new`/`task next` → removed 报错 exit 1；`task update --summary`
    → unknown flag；status-only 迁移正常。
  - archive：6 条历史 failed 全归档；failed 视图 "No tasks found."；
    --all 6 条 [resolved]；search 仍命中；非终态拒绝。
  - serve：双进程 flock 拒绝（pid 报告）；--once 正常退出。
  - ingest：curl 四连——镜像/404/200幂等/409 全过。
  - stale：kill -9 孤儿 → task run 前置扫描 → event=stale 落盘。
- Commit: c84d9f0 → 8a2800c → 57c25ee（git add -f 逐个文件，
  .gitignore 裸模式陷阱规避）。

## Not Done / 交由后续
- tasklist_issued_at 的真实会话 E2E 依赖 LLM 真的发出 [TASK_LIST] 块
  （本轮验证 bash 任务未发）；列与 touch 机制有单测，真值落库待正常
  使用观测。
- stale 阈值 30min 默认值按 payload 要求"实测后数据驱动调整"——现未调。
- HP 端（192.0.2.6）跨机实测未做（本机回环已全过）；HP 侧只是环境
  变量配置，风险低。
