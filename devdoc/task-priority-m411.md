<!-- doc-class: historical -->
# Task Report: M4.11 任务队列 priority — tags 推导默认值 + ORDER BY + 存量回填 (t-1788087991)

分支：`feat/task-priority-tags`（worktree `~/workspace/gopi-task-priority`，基于 master 90a6315）
Commit：`66e9d24`

## Completed

- **priority 列**：`tasksSchemaTable` + `tasksP2Columns` 幂等 ALTER（`ALTER TABLE tasks ADD COLUMN priority INTEGER`）。
  **NULL = legacy 行**是刻意的语义设计：submit 路径永远显式写值（tags 推导或 --priority 覆盖），
  legacy 判定与显式-5 不冲突（首个实现用 DEFAULT 5 曾把"显式 5"和"legacy"混为一谈，被回归测试抓住后改 NULL 哨兵）。
- **taskTagPriority（一处定义）**：bug 前缀族→1（`bug`、`bug033`、`bug037`…真实台账的 bug-flow 命名约定就是 bug 前缀，
  精确匹配会漏掉 3 条存量单——实测发现后改为 HasPrefix("bug")）；`docs`/`doc-only`→8；其余→5。
  bug > docs 优先级（既是 bug 又有 docs tag = bug）。
- **TaskSubmit 缺省填充**：`opts.Priority <= 0` → `taskTagPriority(opts.Tags)`；显式值永远覆盖（规则④）。
- **TaskNextFiltered ORDER BY**：`ORDER BY COALESCE(priority, 5) ASC, created_at ASC, id ASC`。
  bug 单跳到 feature 单前面；无 LIMIT 1 的 walk 语义保持（skip-tag/payload 过滤行 continue）。
- **存量回填**：ensureSchema 对 `priority IS NULL` 的行逐行重跑 taskTagPriority（复用同一映射，零 SQL/Go parity 风险），
  只写 NULL 行——显式 priority 永不被改写。幂等：一遍后无 NULL bug 行。
- **TaskShow** 显示 `priority:` 行（NULL → 显示默认 5，避免"0 看起来最高"的误读）。
- **CLI** `task submit --priority N`（非正整数报错）；**task_submit 工具**新增 `priority` integer 参数
  （LLM 自投单 source=llm tags=bug 同样吃 bug=highest——规则⑤）。
- **sync 三路 import 全部带上 tags 推导**：importTaskFresh / delta insert / harvestInsert——bug 单跨节点同步不降级。

## Not Done

- TaskList 列表视图未加 priority 列（show 有，list 无）——低价值，未做。
- 3 个预存环境失败与本次无关（master 基线复跑同败）：
  `TestAgent_Run_ContextTrimReplaceSlimming`、`TestAgent_Run_ToolResultPayloadOnce`（agent 包），
  `TestEnsureTagsRegeneratesForNewerTS`（ctags JS/TS 环境问题，#1138/#1164 已知）。

## Verification

- Build: `CGO_ENABLED=0 go build ./...` OK；`go vet` 三包干净
- Tests: 新增 6 个回归测试全 PASS
  （TestTaskTagPriorityMapping / TestTaskSubmitDerivesPriorityFromTags /
  TestTaskSubmitExplicitPriorityOverridesTags / TestTaskNextOrdersByPriority /
  TestTaskPriorityBackfillLegacyBugRows / TestTaskShowPriorityLine）；
  tools+agent+cmd 定向套件 PASS（上述 3 个 master 同败的除外）
- **实测（真实 db 副本 + 新二进制）**：
  ① 无 priority 的 bug 单（tags=bug）priority=1，排在 feature(5)/docs(8) 前
  ② `--priority 5` + tags=bug → priority=5（显式覆盖 tags）
  ③ 真实 memory.db 副本回填：bug033/bug036 v2/bug037/CHECK重建 4 条存量 pending bug 单全部 →1，
     NULL 归零；next-claim 顺序 = 4 条 bug 单(按 created_at) → 15 条 feature 单
- Commit: 66e9d24（git add 逐文件，Rule 8）

## 设计决策（值得记）

1. **NULL 哨兵而非 DEFAULT 5**：legacy 行判定必须与"submitter 显式选了 5"可区分，否则规则④被回填击穿。
2. **bug 前缀族匹配**：真实台账 bug-flow 单的 tag 是 bug033/bug037 这类带编号形式；"tags 含 bug"的 Epic 语义 = bug 族前缀。
   反向保护：token 中段含 bug（如 debugbug）不算——必须以 bug 开头。
3. **回填走 Go 循环而非 SQL 表达式**：先试过一条 json_each SQL，复杂且与 Go 映射有漂移风险；
   逐行 SELECT NULL → taskTagPriority → UPDATE，单一事实源。

## 恢复命令

```
git -C ~/work/GoPi show feat/task-priority-tags:devdoc/task-priority-m411.md
git -C ~/work/GoPi log feat/task-priority-tags --oneline -3
```
