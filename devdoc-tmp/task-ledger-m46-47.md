<!-- doc-class: normative -->
# Task Report: M4.6 chats 表 + M4.7 远程调度（feat/chat-remote-dispatch）

重派单（原 t-1788047065 因 GLM 端点故障 28m 无产出）。前次设计裁决直接采用：
【分流信号裁决】按 CLI 入口面分流——chat 进程（gopi chat / gopi web）写 **chats 表**；
runEngine 路径（plain one-shot runs，`echo prompt | gopi`）保持写 **tasks 表**。

## Completed

### M4.6 chats 表（t-1788041668 + t-1788041900）
- **新表 `chats`**（internal/tools/chat.go，与 tasks 同库不同表）：session_id PK /
  mode(chat|web) / model / title / first_msg / turn_count / input_tokens /
  output_tokens / cache_tokens / workdir / log_path(指针，JSONL 仍是权威全文) /
  parent_session_id / git_branch / git_repo_root / end_reason(normal|interrupt|crash)
  / started_at / last_msg_at / ended_at / **brewed_at（M3 蒸馏登记预留列）**。
  全事实列、无状态机——会话生而 running，止而一次性 ended（幂等）。
- **钩子分流**（internal/agent/chatregister.go，前次实例已留接线，本单补齐 tools 层实现）：
  `RegisterChatSession(mode, firstMsg, firstMsgSource)`（首个成功 turn；firstMsg 只在
  被判定为**操作员本人首句**时落进 first_msg —— 见 `internal/tools/chat_first_msg.go`，
  注入的 notify/job-notice 文本落显式占位符，`OfferChatFirstMsg` 让之后开口的操作员补上）/
  `RecordChatTurn(prompt,
  completion, cache)`（每 turn 翻动，token **MAX 折叠单调**——重复/乱序调用不会缩水）/
  `CloseChatRow(exitCode)`（ended_at+end_reason，exit 0→normal 非零→interrupt）。
  taskregister.go 的 runEngine 路径零改动——execution 行照旧进 tasks。
- **task_id == session_id 特意保留**：chat 行的键就是它的会话；chat 不是排队工作项。
- **backfill**：`gopi chat backfill [--dry-run] [--verbose]`。从 tasks 捞
  session_id 非空 + [auto] 前缀 + 未标记行；first_msg/turns/token 从 session JSONL
  重推（tools 不能 import gopilog——gopilog→agent 成环——自写 raw-line 扫描，
  同字段同语义）；JSONL 丢失的行裸导入（summary 推 first_msg）。源行 summary 追加
  " — chat: backfilled"（append-only，不删）；重跑自动跳过。
- **检索分工**：`gopi chat list [query] [--model M] [--limit N] [--db PATH]` =
  chat 侧检索面（LIKE first_msg/title/session_id + model 过滤）；`task search`
  永不见 chat 行（物种隔离，单元测试 TestChatSearchIsolationFromTasks 锁死）。
  chats_fts/chats_tri（FTS5 external-content + 触发器，照抄 tasks 模式）已随表建立，
  ChatSearch 面留待后续 FTS 化（设计单 ④ 的"可后续加"）。
- **GOPI_CHAT_DB**：chat 索引的 --db 覆盖（Rule 2 单点读取）。
- **Agent.Chat/ChatOps 入口防御式初始化 Cost**：手工构造的 Agent（测试/嵌入式载体）
  Cost 为 nil 会让注册条件（`runErr==nil && ag.Cost!=nil`）永假——根修是入口补
  `a.Cost = &deepseek.CostTracker{}`，而非在调用点再包一层 nil 检查。

### hermes sessions 表 40 列对比（t-1788041900 ⑦ 逐列裁决）

**采纳（本次落入 chats 表）**：
| hermes 列 | gopi 落点 | 理由 |
|---|---|---|
| id/uuid | session_id PK | 一行=一会话，天然主键 |
| mode (chat/web) | mode | 入口面即载体，检索维度 |
| model | model | 成本/复现维度 |
| title | title（列已建，生成器后续） | LLM 标题优于 first_msg 摘要；hermes 3322 sessions 实证 |
| first message | first_msg | 时间近因检索 |
| turn_count | turn_count | 活性事实 |
| input/output/cache tokens | input_tokens/output_tokens/cache_tokens | 成本五件套的 gopi 等价物（GLM cache 93-98% 对账前提） |
| created/updated/ended | started_at/last_msg_at/ended_at | 会话三时点 |
| end_reason | end_reason | 语义化退出（hermes 有用列） |
| workdir | workdir | 哪个项目的会话 |
| parent_session_id | parent_session_id（预留） | clone/fork 血统（hermes 232 条 clone 可检索的基础） |
| git_branch/git_repo_root | git_branch/git_repo_root | 开发会话检索：改的哪个 repo |

**不采纳（附理由）**：
| hermes 列 | 裁决 | 理由 |
|---|---|---|
| messages / messages_fts（级存储） | **不建**（设计单 ⑥ 明示） | hermes messages_fts 撑出 4.3GB 大库的教训；JSONL 指针方案，DB 只存索引 |
| billing_provider / billing_mode | 暂缓 | gopi 尚无账单分叉（单 provider）；成本追踪以 token 三件套起步 |
| estimated_cost / actual_cost / cost_status | 暂缓 | 需先有单价表与 provider 对账层——token 列已可推算，列等真需求 |
| slack/discord 等渠道特有列 | 不采纳 | gopi 无对应载体（chat/web 之外无入口面） |
| hermes 的状态机/锁列 | 不采纳 | 会话无排队语义——状态机属 tasks（执行物），chat 是索引物 |

### M4.7 远程调度（t-1788046668 + t-1788046891）
- **三源合一**（internal/config/config.go `ResolveComputeNodes(cliNodes)`，签名照旧单）：
  CLI `--nodes JSON` > env `GOPI_COMPUTE_NODES`（JSON 数组字符串）> settings.json
  `"compute_nodes"`（**该旋钮已于 2026-10-22 随任务 `01a160a2` 整体删除**：解析器、三源与
  `cmd/agent/dispatch.go` 一并移除，见 `devdoc/compute-nodes-knob-deleted-20261022.md`；
  以下保留当时的实现记录）。校验合一（当时由 `ParseComputeNodesJSON` 一套 invariants 服务三源）：
  name 非空唯一、max_concurrent ∈ [1,64]。**取错即报错**：env/settings JSON 坏 =
  error，绝不静默降级本地（typo 白名单悄悄变本地调度才是事故）。EnvVars 目录 +
  help.go 分组新增 GOPI_COMPUTE_NODES（gopi --help-env 可见）。
- **compute_nodes 条目扩字段**（t-1788046891）：`{name, max_concurrent, report_to,
  repo}`——name=ssh 别名（拓扑）、max_concurrent=容量、report_to=回报 ssh 别名、
  repo=push 回程目标。每节点一行完整描述（拓扑+容量+回程）。**显式白名单**：
  绝不以 ssh config 为拓扑源。
- **负载均衡 dispatch**（cmd/agent/dispatch.go，serve tick 内）：领取任务 →
  对每节点 ssh 查 `gopi task list --status in_progress` 行数 → count<max_concurrent
  候选中**取最空者** → ssh 派发。全满/ssh 不可达 → 本地跑（--nodes-only 则释放
  claim 等下 tick）。节点失联降级 = 跳过该节点 + serve 日志一行，**循环不炸**。

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

- **派发命令 nohup 后台化**：`nohup gopi task run <id> --report-to X --repo Y
  >/dev/null 2>&1 &`——ssh 在 handoff 完成即返回，serve tick 不被 remote 全程
  run 挂住（5m tick 守护进程不能为一次 run 开几小时的 ssh）。
- **remote 回报**（t-1788046891）：remote 无状态，全部上下文由 argv 传递——
  `gopi task run <id> --report-to <alias> --repo <target>` 的 writeback 包装为
  ①本地台账 writeback（remote 自己的审计不撒谎）→ ②`ssh <report-to> gopi task
  update <id> --status X`（中心台账）→ ③`git push <repo> HEAD:<branch>`（分支回程）。
  ②③ 失败只 log 不 panic（任务已跑完，回报是 best-effort）。
- **状态机衔接**：派发成功后中心行翻 in_progress（合法转移 claimed→in_progress），
  remote 回报 completed/failed 从 in_progress 合法到达；若停在 claimed，remote 的
  回报会被 taskAllowedTransitions 拒绝——这是 E2E 暴露的坑，已修。
- **serve.lock GOPI_HOME 隔离修复**（E2E 发现）：acquireServeLock 原来
  os.UserHomeDir 硬拼 ~/.gopi/serve.lock，GOPI_HOME 沙箱里的 serve 会撞上真机
  daemon 的锁（本次 E2E 撞上 pid 4145349 的生产 serve）——改走 config.GopiDir()，
  锁随数据根走。
- **help 三处**：tasksUsage（run/serve 段落 + flags 白名单）、EnvVars 目录、
  settings.json 注释，全写明（实测标准 ⑤ 的 help 要求）。

## Not Done（诚实清单）
- **HP 真机端到端**（实测标准 b 的 ssh 真机部分）：peerbox↔HP 双机拓扑需第二台机器
  在线配合。本单以 fake-ssh 单机 E2E（.e2e/dispatch-e2e.sh：提交 3 任务 → serve
  --once --nodes → 断言 dispatch.log 收到 nohup gopi task run <id> --report-to
  --repo → 中心行翻 in_progress）覆盖 dispatch 决策/审计/状态机衔接；**真机 ssh
  复核留给 peerbox 上的部署步骤**（脚本即清单）。
- **title LLM 生成器**：列已建（schema 不 churn），生成推迟（brew 管道窗口）。
- **chat 检索的 FTS 面**：chats_fts/chats_tri 表+触发器已建，ChatSearch 查询面
  未接（list 的 LIKE 面已够当前量级）。
- **testsuite 两个既有失败**（TestTask_HappyPath_Stdin/Positional）：master 基线
  8e1342a 同样失败（stash 验证），system prompt 注入变化所致，与本单无关。

## Verification
- Build: `go build ./...` OK；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- Vet: `go vet ./cmd/... ./internal/...` clean
- Tests: `GOPI_ALLOW_DANGEROUS=1 go test ./... -count=1`（270s）：
  cmd/agent ok / internal/agent ok / internal/tools ok / 其余全 ok；
  testsuite 2 FAIL 为 master 既有（stash 对照验证）。
- 新增测试：internal/tools/chat_test.go ×6（ensure/turn/close 循环、missing-row、
  re-ensure 不清 first_msg、过滤、backfill 迁移+标记+幂等、dry-run 零写入、检索
  隔离）；cmd/agent/dispatch_test.go ×6（ssh 参数硬化、计数解析、最空节点选择、
  全满/失联降级、remote 命令上下文+nohup、--nodes 校验、三源优先级）；
  cmd/agent/cli_web_test.go 的 web 端 chats 表契约测试（TestWebChat_ChatTable-
  ReflectsSession：首 turn ensure→turn_count 累加→finalize end_reason=normal）；
  Bug003 回归测试改断言 chats 表契约。
- E2E（单机模拟）：
  - dispatch：`bash .e2e/dispatch-e2e.sh` → `[dispatch] t-… → fakenode
    (remote-run accepted)`，dispatch.log 记录
    `nohup gopi task run t-… --report-to fakenode --repo fakenode:~/work/GoPi
    >/dev/null 2>&1 &`，台账行 [in_progress]。
  - remote 回报：`gopi task run $T --report-to fakenode`（假端点让 child 失败）
    → 本地行 [failed] + fake-ssh 收到 `gopi task update $T --status failed`。
  - chat：mock SSE → `printf "hello there\n/quit\n" | ./bin/gopi chat` →
    `gopi chat list`：`<uuid> [chat] hello there (normal, model …, turns 1,
    tok in=10 out=7 cache=10, started …, ended …)`；task search 零泄漏。
  - 首turn失败：无 chat 行（不产生孤儿 running 行），符合设计。

## Deliverable（Rule 6d 声明）
- **分支**：`feat/chat-remote-dispatch`（worktree ~/workspace/gopi-chat-remote，
  与主仓共享对象库——commit 即在主仓分支上，无需 push）
- **恢复命令**：`git -C ~/work/GoPi show feat/chat-remote-dispatch:devdoc/task-ledger-m46-47.md`
- commit：见分支 tip（feat(tools): M4.6 chats 表 + M4.7 dispatch）

## 附录：见证会话补充验收（3644f54d — 与 worker 并行的原会话，让位后转只读+验证）

### 修复 2 个 commit（4721052 + 56adcfb）
- **4721052 fix(m46)**: ChatBackfill --verbose 对 JSONL 不可读行解引用 nil facts → panic。
  真 DB 实测发现（82 行 backfill，9 行日志已清理）。修复：summary-only 行报告日志缺失。
  真 DB backfill 落库验证：82 行全部标记 — chat: backfilled，chats 表 18+64 行；
  8 行抽样 jsonl 直算 vs DB 列 全 OK（turns/tokens 逐行吻合）。
- **56adcfb fix(testsuite)**: TestTask_HappyPath_* 2 FAIL 为**既有失败**（08-30 03:14
  621a60e 把 task-start checkpoint 注入作为 user 事件写进 session JSONL，之后无人跑过
  testsuite）。根因修复：断言 prompt CONTENT 而非 user 事件计数（注入事件是 harness
  政策，agent 包已断言）。

### HP 真机 E2E（fake-ssh 之外的真实双机链路）
- hp→peerbox 反向 ssh 可达；HP worktree e2e-wt 构建 56adcfb 二进制
- HP 跑 t-1788059681（自建行）：**任务本体成功**（hello-e2e.txt 写入，
  DONE 回复，35s）+ **git push back 成功**（e2e/hp-verify-run → peerbox，peerbox 可查）
- **发现缺口（已开 bug 单 t-1788060307）**：dispatch 只发 nohup task run <id>，
  remote 本地无行 → TaskStart 失败秒退。fake-ssh E2E 因共库掩盖。修复方向
  （remote --briefing 模式）留给补丁单
- report-to 回报路径验证：ssh peerbox 'gopi task update <id>' 可达，但 update 的
  目标 id 在中心台账须存在——HP 自建行 id 中心不认（反伪造 by design），
  完整闭环需补丁单的 briefing 模式
- GOPI_TASK_NOTIFY_WEBHOOK 镜像：HP →POST peerbox:8923/task-ingest → 中心行
  迁移 + tags 加 node:hp,remote:<node>——**验证通过**（t-1788054323）
- task-ingest 反伪造：404 未知行 / 400 非终态 / 409 forward-only——逐条验证

### 最终测试
go test ./... 全绿 11/11 包（含 testsuite；GOPI_ALLOW_DANGEROUS=1，
ANTHROPIC_AUTH_TOKEN=x 供 brew 测试），EXIT=0。e2e fake-ssh dispatch 脚本复跑通过。
