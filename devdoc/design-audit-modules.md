<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# 设计文档 ↔ 源代码一致性审计：模块划分（元规则 · 只定义不执行）

本文件定义"审计谁、怎么切、切多少"，模板见 `devdoc/design-audit-template.md`；**本任务不审计任何模块、不移动任何文件、不改生产代码。**

## 1 划分依据：按职责边界，不按目录

一条职责边界 = **一次可命名的用户可见结果**（"任务被认领"/"分支被交付"/"场地被回收"），
不是 `internal/xxx/` 这个目录。理由是实测的，不是审美的：

- 收尾链（交付点同步 → 构建/vet → 释放 worktree → 出报告）横跨 `internal/tools/`、
  `internal/closeouthandoff/`、`internal/runend/`、`internal/taskreport/`、`cmd/agent/` 五个目录。
  按目录切会把它切成 5 块，**每条裁决都指向邻居包，谁都不拥有它** —— commit `3c396080` 正是这条链上的
  真实缺陷：本机 `settings.json` 的 `sync_peers: []` ⇒ `HasSyncPeers()` 假 ⇒
  `finishCleanupServerTarget() == ""` ⇒ 在**客户端臂之前**就分派到 `finishCleanupServerRecycle`，
  而那条臂当时只有 `git worktree remove` —— **交付点同步整段不可达**。**缺陷落在交叉处，故按职责切。**
- 同一目录会落进多个模块（`internal/tools/` 支撑 M01/M02/M03/M04/M05/M06/M09），
  这是预期结果，不是划分失败：文件归属由**它服务的职责**决定。

既有资产：`devdoc/requirements/` 系列（14 边界，`00-module-map.md` 575 行）已按同一原则划过一遍，
但其目的是"写需求文档"而非"审现有文档与代码是否一致"，且**缺收尾/交付、观测、执行环境、终端输入、感知**。
本清单**复用其口径、不重造**并补齐缺口；两者差集本身即审计线索。
**编号不同源**：本文件 M01–M17 ≠ 该系列 M1–M14。对应（本 → 该）：M01→M1、M02→无章、M03→M2、
M04→M10、M05→M3、M06/M07→M4、M08→M5、M09→M6、M10→M7、M11→无章、M12→M12/M14、M13→M9、
M14→M11、M15→M10、M16→M13、M17→M8。**按上表引用，不要按编号同名去猜。**

### 1.1 可复现测量（Rule 38：数字必须能重算）

```
find internal cmd -name '*.go' ! -name '*_test.go' | wc -l            # 618
find internal cmd -name '*.go' ! -name '*_test.go' | xargs wc -l      # 293630 ；HEAD aab63a1e
# 每模块 loc：对 §2.1 的 glob 文件列表求和（不含 _test.go）
# 文档面粗筛（可重复）：ls devdoc/*.md devdoc/*/*.md | xargs -n1 basename | grep -icE '<关键词>'
```

**口径**：只算生产 `.go`（`_test.go` 不计），loc 为物理行数。**行号是本表修订时的读值**，
引用前用 `sed -n '<n>p' <file>` 复核。**文档面一栏是粗筛**，精确归属由各审计子任务重跑并写死清单。

**已知缺口（诚实声明）**：文件数/loc 由一次性脚本按 §2.1 glob 逐个认领算出，
**仍有 44 个 `cmd/agent/cli_*.go` 未被任何模块认领** —— 多数是某模块的 CLI 表面
（`cli_archive.go` 属 M04、`cli_proxy.go` 属 M16），但也可能说明 M10 的职责面画小了。
**审计子任务必须重算自己那一面**，不要把本表数字当作自己的观测值（Rule 31/38）。

## 2 模块清单（17 个）

| ID | 模块 | 职责一句话 | 生产 .go | loc |
|---|---|---|---|---|
| M01 | 台账与调度 | 一行任务从 submit 到 claim/lease/terminal 的状态机与调度漏斗 | 60 | 37889 |
| M02 | 收尾与交付 | 一次运行结束时的交付点同步、构建门、场地释放与报告契约 | 18 | 11039 |
| M03 | 合并与分支 | 分支从候选到落 master 的选择、试合并、回滚与分支名守卫 | 46 | 20065 |
| M04 | 场地生命周期 | worktree 的创建、清洁判据、残留判定与回收 | 23 | 15089 |
| M05 | 节点同步 | 两主机之间的台账/记忆/git 推送与节点拓扑 | 23 | 12146 |
| M06 | 记忆库与蒸馏 | 记忆记录的生命周期、检索、蒸馏与程序化工作流 | 15 | 10739 |
| M07 | 上下文装配 | 一次模型调用的上下文与边界文本是怎么拼出来的（含 chat 边界） | 10 | 2208 |
| M08 | agent 循环与工具路由 | 一次对话的循环、工具调用与路由说服机制 | 36 | 20243 |
| M09 | 工具体系实现面 | 17 个工具各自的实现、参数契约与守卫 | 43 | 13849 |
| M10 | CLI 骨架与宿主进程面 | 动词表、进程入口、子进程守卫与宿主生命周期 | 13 | 6317 |
| M11 | 终端输入与渲染 | 行编辑、输入解析、SGR/PTY 渲染 | 43 | 22817 |
| M12 | 配置与构建 | 每一个可配旋钮的解析优先级与声明式构建 | 34 | 10226 |
| M13 | 门禁与文档真值 | 能让构建变红的机器检查，与文档分类/索引真值 | 29 | 12230 |
| M14 | 观测与生命周期记录 | 日志、会话记录、检查点、通知与信号留痕 | 39 | 19037 |
| M15 | 执行环境适配 | 沙箱、临时根、cgroup/进程身份等"跑在哪"的适配 | 54 | 20499 |
| M16 | 感知外围 | 截图/渲染/视觉/语音/外网等进出本机的通道 | 51 | 20198 |
| M17 | chat/UI 与客户端 | 屏幕、UI 树与客户端选择：回答"我正跑在哪张屏上" | 37 | 26137 |
| — | 合计 | — | 618 | 293630 |

### 2.1 每模块的边界与代码面

**M01 台账与调度** — 负责：行状态机、认领谓词（含 `parent_task_id` 门）、租约/心跳、僵尸。**不负责**：行被交付（M02）、分支被合并（M03）、场地被回收（M04）。
代码面：`internal/{tasksource,tasktags,taskverbs,taskverdict,fullid,ledgerwindow,workerownership}/`、`internal/tools/{task,tasks,worker_,taskheartbeat,tasknotify,taskproc,taskreissue,taskrunguard,taskrunlog,taskstate,tasksummary,taskzombie,task_submit,task_adoption,task_staleness,task_holder,task_for_,task_payload,task_attachment,task_session,task_resume,task_repo,task_signal,taskledger}*.go`、`cmd/agent/{cli_tasks,cli_task_,worker,dispatch}*.go`
文档面：`requirements/01-task-ledger.md`（粗筛专题 244 篇）。

**M02 收尾与交付** — 负责：交付点同步、预合并构建/vet、场地释放、报告契约。**不负责**：合并决策（M03）、场地回收判据（M04）、门禁实现（M13）。
代码面：`internal/{closeouthandoff,runend,taskreport}/`、`internal/tools/{task_finish,task_close,task_report,verify_gate,run_closeout}*.go`、`cmd/agent/{cli_finish,finish_,cli_report,report_contract_child,run_closeout_reap_scope}*.go`
文档面：**无设计文档**（`requirements/` 无收尾/交付章）—— 这正是 `3c396080` 的文档面证据。粗筛 34 篇。

**M03 合并与分支** — 负责：候选选择、试合并/共建、回滚、master 认领、分支名与安全合并守卫。**不负责**：交付点同步（M02）、分支被创建/回收（M04）。
代码面：`internal/branchform/`、`internal/tools/{merge,merger,branch_,safe_merge,task_merge,master_claim,main_checkout,ledger_agreement,shared_stash,manifest_union,repo_sweep_ref}*.go`、`cmd/agent/{merge,cli_tasks_merge,cli_tasks_merger_help}*.go`
文档面：`requirements/02-merge-and-branches.md`、`merge-branch-lifecycle.md`（粗筛 239 篇）。

**M04 场地生命周期** — 负责：worktree 创建/新鲜度、清洁判据（Rule 30）、残留归属、回收与 repo sweep。**不负责**：合并（M03）、沙箱（M15）、交付同步（M02）。
代码面：`internal/{worktreeroots,worktreerelease}/`、`internal/tools/{worktree_,task_reclaim,reclaim_,run_ground,repo_sweep,archive_,task_deliverable_,task_ground_anchor,task_resume_ground,dirty_holder,task_main_checkout}*.go`
文档面：`requirements/03-worktree-and-cwd.md`（粗筛 96 篇）。

**M05 节点同步** — 负责：任务/记忆/git 的跨主机同步与推送、节点拓扑与别名。**不负责**：本机台账语义（M01）、合并（M03）。
代码面：`internal/tools/{task_sync,memory_sync,script_install_sync,firstopen_sync,peer_}*.go`、`cmd/agent/{cli_tasks_sync,cli_memory_sync,cli_memsync,memory_sync_tick}*.go`
文档面：`requirements/04-sync-and-nodes.md`、`notifications.md`（粗筛 67 篇）。

**M06 记忆库与蒸馏** — 负责：记忆记录生命周期/检索排序/反馈/程序化工作流与蒸馏。**不负责**：上下文拼装（M07）、跨主机同步（M05）。
代码面：`internal/hippocampus/`、`internal/tools/memory*.go`、`cmd/agent/{cli_memory,cli_mem}*.go`
文档面：`requirements/05-context-and-memory.md`（粗筛 59 篇）。

**M07 上下文装配** — 负责：一次调用的上下文文本、chat 写边界文本、会话事件注入。**不负责**：循环本身（M08）、记忆检索算法（M06）。
代码面：`internal/memoryprefetch/`、`internal/sessionevent/`、`internal/agent/chat_{context,write_boundary}*.go`
文档面：**无独立设计文档**（散见 `requirements/05`、`06` 与专题；粗筛 40 篇）。

**M08 agent 循环与工具路由** — 负责：循环、工具调用与结果回填、routing 说服（Rule 11）。**不负责**：工具实现（M09）、上下文来源（M07）。
代码面：`internal/agent/*.go` **减 M07 已认领的** `chat_context*.go`/`chat_write_boundary*.go`
文档面：`requirements/06-agent-loop.md`（粗筛 18 篇）。

**M09 工具体系实现面** — 负责：17 个工具的实现、参数契约、调用守卫、"工具被真的用了吗"。**不负责**：路由说服（M08）、门禁（M13）。
代码面：`internal/tools/*.go` **减 M01–M06 已认领的文件**（残余定义，见 §1.1）+ `internal/toolnames/`
文档面：`requirements/07-tools-and-routing.md`（粗筛 66 篇）。

**M10 CLI 骨架与宿主进程面** — 负责：动词表、进程入口、子进程守卫、宿主/服务生命周期。**不负责**：各动词的业务（M01–M06/M12）、渲染（M11）。
代码面：`internal/{clicontract,cliverbs}/`、`cmd/agent/{commands.go,main.go,agent_spawn_guard,child_,compute_nodes_unwired,cli_host,cli_daemon,daemon_,host_lifecycle}*.go`
文档面：`requirements/cli.md`（粗筛 99 篇）。

**M11 终端输入与渲染** — 负责：行编辑、输入文本/路径解析、SGR 与终端链接。**不负责**：UI 树（M17）、CLI 动词（M10）。
代码面：`internal/lineedit/`、`internal/sgr/`、`cmd/agent/{inputtext,inputpath,lineedit,term,cli_term}*.go`
文档面：**无设计文档**（粗筛 46 篇）。

**M12 配置与构建** — 负责：`CONFIG.md` 目录里每个旋钮的优先级、声明式构建与重建。**不负责**：门禁实现（M13）、沙箱根解析（M15）。
代码面：`internal/{config,envctx,version,pricing}/`、`internal/tools/{rebuild,config_,path_policy}*.go`、`cmd/agent/cli_{config,tmpdir,settings,env}*.go`
文档面：`requirements/11-build-and-repos.md`、**`CONFIG.md`**（本身即真值文档）（粗筛 57 篇）。

**M13 门禁与文档真值** — 负责：让构建变红的机器检查、文档分类/索引/DOC-INDEX 真值。**不负责**：被检查的行为本身（各模块）。
代码面：`internal/{standards,stylegate,docindex,docclass,smokegate,smoke,textguard,flaketest,heavytest}/`、`cmd/agent/cli_doccheck*.go`
文档面：`requirements/12-verification-and-gates.md`、`doc-truth-classification-report.md`（粗筛 82 篇）。

**M14 观测与生命周期记录** — 负责：日志、会话记录布局、检查点/回放、通知与信号留痕、删除审计。**不负责**：门禁（M13）、聊天表面（M17）。
代码面：`internal/{gopilog,sessionlayout,procstat,checkpoint,replay,daemonlifetime,signalrec,removalaudit,notifyfeed}/`、`cmd/agent/cli_{log,session,signal,notify}*.go`
文档面：**无设计文档**（粗筛 160 篇）。

**M15 执行环境适配** — 负责：沙箱、临时根（off-`/`）、cgroup/进程身份、测试脚手架。**不负责**：场地/worktree（M04）、配置优先级（M12）。
代码面：`internal/{sandbox,scratch,rundir,runcmd,procname,cgroupscope,borrowedfd,daemoncwd,testutil}/`、`internal/tools/{tilde,timeout,withtimeout,childenv,lock_owner,repo_resolve,signal_record}*.go`
文档面：**无设计文档**（散见 `requirements/test-infrastructure.md`；粗筛 52 篇）。

**M16 感知外围** — 负责：截图/渲染/视觉/语音/外网检索等进出本机的通道。**不负责**：UI 客户端选择（M17）、工具路由（M08）。
代码面：`internal/{text2img,shot,gifgen,doubaoasr,proxy,deepseek,thinkmode,boolvocab,agencyscan}/`、`internal/tools/{text2img,vision,screencapture,websearch,search,symbol,service_control,agency}*.go`
文档面：**无设计文档**（含 `text2img.md` 等专题；粗筛 37 篇）。

**M17 chat/UI 与客户端** — 负责：屏幕/UI 树读取、客户端选择与自我身份（Rule 3 的 surface identity）。**不负责**：chat 循环（M08）、写边界文本（M07）。
代码面：`internal/uiclient/`、`cmd/agent/{chat,cli_chat,ui,web,cli_ui,cli_web,ui_state_self,web_identity}*.go`
文档面：`requirements/09-chat-ui.md`、`09-chat-ui-verification-report.md`（粗筛 232 篇）。

## 3 "活的文档"判据（机器可查优先）与归档动作

### 3.1 判据（L1–L3 机器可查；L4 需裁决，但每条可指 file:line）

- **L1 有分类**：文件在 `devdoc/DOC-INDEX.tsv` 有一行且四列合形 —— `gopi doccheck` exit 0；
  `go test ./internal/standards/ -run 'TestDocManifestCoversEveryDevdocFile|TestDocManifestRowsAreWellFormed'`。
- **L2 有证据锚**：`normative` 行的 evidence 首字段是 `sym`/`anchor`/`path`/`cite` 之一，且该锚在**当前 master** 解析得到
  （`internal/docclass` 的证据分级；锚点烂掉 = 文档死了却还热着）。
- **L3 有类横幅**：`historical` 文档首行是 `<!-- doc-class: historical -->` ——
  `-run TestDocClassBannerMatchesManifest`。
- **L4 声称与代码一致**：文档的每一条"某行为存在/不存在"的断言，能在当前源码指到 `file:line`
  （或明确写出"无法定位 ⇒ 判为无设计/文档该改"）。**L4 是审计裁决的对象，不是机器门禁。**

判定口径：L1 缺 → 文档**未登记**；L1/L2/L3 齐但 L4 假 → 文档**已坏**（该改/该归档）；
L4 无从谈起且代码面存在 → **无设计文档**（该新建）。

### 3.2 归档动作（**本任务只定义，不执行**）

1. **先查引用**：`git grep -l '<旧路径>'` + 全库 `devdoc/` 路径引用扫描（实测：`report-class-disposition-proposal.md`
   记 396 篇文档引用 devdoc 路径、551 个被引路径）—— 被引用的文档归档前必须改引用，或选择不归档。
2. `git mv devdoc/<x>.md devdoc/archive/<x>.md`（该目录已存在，且在 `tools/docclass/classify.py` 的 `HIST_DIRS` 无写者路径表内）。
3. **DOC-INDEX.tsv 随动 = 两步**：① **手工删**旧路径那一行（否则留 orphan row —— worktree 里已无此路径）；
   ② 给新路径补行（需先有一条**带 class 的行**，`--fix` 才能接管 banner）。**`--fix` 的边界（源码为准）**：
   它写**缺失的行**与**该类需要的 banner**（`internal/docindex/docindex.go:470-473`：`ClassBannered` 且首行不是
   该 banner 时前插），且只在**同一个 corpus 缺口**上工作；它**不删**陈旧/孤儿行（只在 report 里列出）。
   故归档 = `git mv` + 手工删旧行 + `doccheck --fix`，**同一个 commit**（Rule 36）。单路径 banner 也可用
   `python3 tools/docclass/banner.py <repo> --only <path>`（`internal/docindex/docindex.go:109` 的常量所指）。
4. **gate 影响**（会红的就是这三条，顺序即因果）：缺行 → `CoversEveryDevdocFile`；行不合形 → `RowsAreWellFormed`；
   `historical` 缺 banner → `BannerMatchesManifest`。收工前验：`gopi doccheck && go test ./internal/standards/ -run Doc`。
5. **新建文档**按 L1–L3 反向走：`write` 工具落盘（write 钩子自动分类），否则 `gopi doccheck --fix` 后补 banner。

## 4 模块级子任务清单（18 项：17 个模块 + 1 个跨模块收口）

每项都是 self-contained briefing：**目标 / 验收 / 输入（设计文档 + 代码面 glob）/ 时限 ≤ 60 分钟**。
**> 20k loc 的模块（M01/M03/M11/M15/M16/M17）必须在 briefing 里再点名 1–2 条职责子链**
（同名模块章见 `devdoc/requirements/01-per-module-briefings.md`；**两套编号不同源**，对应表在 §1），
否则一个 session 审不完。**批量派发时，每份 briefing 必须自包含 —— 不要把"见 §2.1"当 briefing。**

**批次与顺序（先做有真实缺陷线索的，再做纯文档缺失的）**

| 批次 | 顺序 | 任务 | 模块 | 目标（一句话） | 验收 | 输入（文档 / 代码面） |
|---|---|---|---|---|---|---|
| B1 有缺陷线索 | 1 | A-M02 | 收尾与交付 | 审交付点同步与场地释放是否与设计一致 | 每条裁决指 file:line；产出 U 编号清单 | 无设计文档 / M02 glob |
| | 2 | A-M04 | 场地生命周期 | 审清洁判据与回收谓词 | 同上 | `03-worktree-and-cwd.md` / M04 glob |
| | 3 | A-M03 | 合并与分支 | 审候选选择/试合并/回滚与 master 认领 | 同上（限 1–2 子链） | `02-merge-and-branches.md` / M03 glob |
| | 4 | A-M01 | 台账与调度 | 审认领谓词与 `parent_task_id` 门一致性 | 同上（限漏斗+谓词） | `01-task-ledger.md` / M01 glob |
| B2 系统主干 | 5 | A-M05 | 节点同步 | 审同步腿与节点拓扑 | 同上 | `04-sync-and-nodes.md` / M05 glob |
| | 6 | A-M06 | 记忆库与蒸馏 | 审记忆生命周期与检索排序 | 同上 | `05-context-and-memory.md` / M06 glob |
| | 7 | A-M08 | agent 循环 | 审循环与 routing 说服 | 同上 | `06-agent-loop.md` / M08 glob |
| | 8 | A-M09 | 工具体系 | 审 17 工具体契约与守卫 | 同上（按工具分组采样，非全量） | `07-tools-and-routing.md` / M09 glob |
| | 9 | A-M07 | 上下文装配 | 建新文档：上下文与写边界从哪来 | 产出"该新建"条目 | 无设计文档 / M07 glob（2.2k loc，最小） |
| B3 表面与真值 | 10 | A-M10 | CLI 骨架 | 审动词表与进程入口 | 同上 | `cli.md` / M10 glob |
| | 11 | A-M12 | 配置与构建 | 审旋钮优先级与 `CONFIG.md` 一致 | 同上 | `11-build-and-repos.md`、`CONFIG.md` / M12 glob |
| | 12 | A-M13 | 门禁与文档真值 | 审门禁清单与文档分类真值 | 同上 | `12-verification-and-gates.md` / M13 glob |
| | 13 | A-M11 | 终端输入与渲染 | 建新文档：输入/渲染的契约 | 同上（限 1–2 子链） | 无设计文档 / M11 glob |
| | 14 | A-M14 | 观测与记录 | 建新文档：日志/会话/检查点布局 | 同上 | 无设计文档 / M14 glob |
| B4 外围与文献面 | 15 | A-M15 | 执行环境适配 | 建新文档：沙箱/临时根/cgroup 契约 | 同上（限 1–2 子链） | 无设计文档 / M15 glob |
| | 16 | A-M16 | 感知外围 | 建新文档：感知通道与降级 | 同上 | 无设计文档 / M16 glob |
| | 17 | A-M17 | chat/UI 客户端 | 审客户端选择与自我身份 | 同上 | `09-chat-ui.md` / M17 glob |
| | 18 | A-X01 | 跨模块收口 | 汇总 U 编号、判定"该归档"清单、产出下一批任务 | 汇总表 + 可直接 dispatch 的任务文本 | 上面 17 份报告 |

**A-M02 完整 briefing 示例（其余任务按同一形状写）**：

> 目标：审计 M02 收尾与交付的设计-代码一致性。范围：交付点同步、预合并构建/vet、场地释放、报告契约。
> 起点线索：commit `3c396080`（交付点同步在 `sync_peers: []` 时够不着）——核对**当前 master** 是否仍有未覆盖的臂
> （本机 `settings.json` 的 `sync_peers` 为空即该条件，复现路径直接可用）。
> 输入：代码面见本文件 §2.1 M02 的 glob；**该模块无设计文档**，须按模板第 1 节写"无设计文档"并列出散落断言来源
> （`GOPI.md` Rule 30/12/31、`devdoc/requirements/03-worktree-and-cwd.md`）。
> 验收：按 `design-audit-template.md` 六节产出 `devdoc/design-audit/M02-close-out.md`（≤ 一屏两屏），每条裁决指到
> `file:line` 与文档行号，不一致项编号 U-M02-01…；`gopi doccheck` exit 0；**每条`一致`附一个确认动作**。
> 时限：60 分钟；**不改生产代码、不移动任何文件**。
