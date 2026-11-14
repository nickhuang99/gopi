<!-- doc-class: historical -->
# Task Report: 对外展示物 epic —— 本轮交付是「分解」本身（verdict=TOO_BIG）

Epic: `01a0ecdb-812d-773a-b936-0f17d1879a3d` —— 把 gopi 的真实能力做成
「同行技术人 A / 客户 B / 投资人 C」三种能看懂的对外展示物。

## Completed

- **现场实测切分依据**：`gopi -h` 实测 **37 个顶层命令**，help 自带 **6 个功能类**
  （Task ledger & execution / Coding & session inspection / Chat & web / Tools /
  Memory & knowledge / Configuration & maintenance），`gopi task -h` 实测 **31 个子命令**。
  该清单作为切分骨架，集中记录在子任务 1 的 briefing 里（不靠记忆推导）。
- **规模估算与判定**：6 类 × 37 命令 × 四动作（①真跑 ②人话用途 ③`file:line` 机制
  ④help/文档一致性核查）+ coverage.tsv + 竞品位置 + 三受众 A/B/C + 总装，
  **单个 60 分钟 session 装不下** → `task(action=reframe)` verdict=**TOO_BIG**。
- **decomposition（9 项 roadmap，已由 harness 自动入队为子任务）**：

  | # | 子任务 id | 目标 |
  |---|---|---|
  | 1/9 | `01a0ecdd-fec0-7305-a259-fbfd4de39ea0` | 样本+仪器：`task` 命令族 31 子命令四动作 + `scripts/demo-coverage.sh` → `coverage.tsv`（全链格式基准） |
  | 2/9 | `01a0ecde-00bd-7ed7-b46f-cee27c4d5d4c` | Class 1 后半：`worker`/`merger`/`merge`/`archive` + 可复现小演示 |
  | 3/9 | `01a0ecde-0236-72bc-a866-dae935bfe28a` | Class 2 Coding & session inspection |
  | 4/9 | `01a0ecde-03c6-724d-89bc-4bf7b17b1798` | Class 3 Chat & web |
  | 5/9 | `01a0ecde-0567-7c65-8916-be4b1fbbdc0f` | Class 4 Tools（眼睛与手） |
  | 6/9 | `01a0ecde-05dc-72dc-b7cb-8a46611df396` | Class 5 Memory & knowledge |
  | 7/9 | `01a0ecde-0616-7de0-82d9-dbf82a5b9b45` | Class 6 Configuration & maintenance |
  | 8/9 | `01a0ecde-0651-7e70-8a99-eede843e85fb` | 竞品真实位置（对外版，扩到 4 家） |
  | 9/9 | `01a0ecde-0695-75e7-b864-35c0d165b52a` | 三受众 A/B/C + 可放映总装 |

  每个子任务 briefing 自包含：目标 / 产出路径 / 真跑要求（Rule 31）/ 私有 profile
  要求 / 记录格式 / 验收；均以 `parent_task_id=01a0ecdb-…` 挂在本 epic 下，
  受 Rule 33 依赖门控（父未 release 不会被子任务顶掉队列）。

- **失败模式写入记忆**（跨 session 复用）：memory `#6763` 记录 capdemo9 的失败形态
  （49KB 内部审查 + 6 张「tool 拍自己」的 PNG，被判「幼儿园都不算」）与正确切分/判据；
  `#6761` 命中并投票 useful。

## Not Done（诚实）

- 六类类文档、`coverage.tsv`、竞品位置文档、A/B/C 三版、总装 —— **本轮一份都没有写**，
  它们全部是上面 1–9 号子任务的交付物。本轮不做这些，是本轮 verdict 的直接结论。
- `scripts/demo-coverage.sh` 未创建；`docs/archive/demo/showcase/` 目录未创建。
- 尚无 help/文档不一致 bug 单 —— 不一致只在「真跑 + 读代码」之后才可能出现，
  属于子任务 1–7 的产出，本轮的诚实状态是「一条都还没发现」。

## Verification

- Build: 未改任何 Go 代码（本轮只做分解，无源码改动），故未跑 build；`gopi -h`/`gopi task -h`
  用的是本 worktree 既有可执行路径，非新构建。
- Tests: 未改代码 → 无针对性测试可跑；未跑全树（Rule 27：merge/verify 路径上的正确形态是
  smoke、分支scoped）。
- Ran: `gopi -h`、`gopi task -h`（分类与子命令计数）；`gopi task list`（确认子任务已入队）；
  `gopi task show 01a0ecdd-fec0-7305-a259-fbfd4de39ea0`（确认 `parent_task_id`）；
  `task(action=reframe, verdict=TOO_BIG, roadmap=[9 项])`（入队动作）。
- Result: `gopi task list` 打出 2898 行任务台账，其中 9 条 `[pending] [1/9]…[9/9]`
  子任务 + 本 epic 行存在；`task show` 子任务 1 的 `parent_task_id` =
  `01a0ecdb-812d-773a-b936-0f17d1879a3d`（即本 epic），`tags: reframe-roadmap`。
- Commit: 见本条 devdoc 的提交哈希（本 run worktree 的分支上）。

## Evidence (self-filed)

- 子任务入队：`gopi task list | head -20` 输出含
  `#01a0ecdd-fec0-7305-a259-fbfd4de39ea0  [pending]  [1/9] …` 至
  `#01a0ecde-0695-75e7-b864-35c0d165b52a  [pending]  [9/9] …`，共 9 条。
- 归属核验：`gopi task show 01a0ecdd-fec0-7305-a259-fbfd4de39ea0` 输出
  `parent_task_id: 01a0ecdb-812d-773a-b936-0f17d1879a3d`、`tags: reframe-roadmap`。
- 工作区干净：`git status --porcelain` 在本 worktree 下于提交前后均按 Rule 30 检查。
- help 类与命令计数：`gopi -h` → 37 顶层命令 / 6 类分组；`gopi task -h` → 31 子命令。
