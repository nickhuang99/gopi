<!-- doc-class: historical -->
# Task Report: tasks-merge-executor-live — LLM merge executor for pending merge-backlog rows

Epic: 把 pending merge-backlog 台账行真正合入 master 并收尾。分支
`feat/merge-executor-live`。背景记忆 id=1177（repo=worktree 交付形态坑、
serve flock 单例、branch -d 语义）、id=1208（hook v5 并发坑：共享工作树
merge 踩踏、dirty 判定含 untracked）。

## 1. 问题

`gopi task audit-merges --submit`（foresee-review §2）会为每个 unmerged
分支入队一条 pending `merge <branch> into master` 行；post-receive hook
也会经 `audit-merges --submit` 入队。但这些行**没有执行器**：serve 的
dispatch 把它们当普通 LLM 任务 claim 给 agent 跑（昂贵、且 briefing 只让
agent "run scripts/safe-merge.sh then merge"——每次 merge 消耗一次 LLM
会话）。本任务交付**零 LLM 的 merge 执行器**（与 audit-merges/adopt/
finish-cleanup 同边界：Go 只 GUARD / MERGE / CLOSE，语义判断留人/agent）。

## 2. 实现

### 2a. 引擎 `internal/tools/task_merge_executor.go`（新）

`TaskMergeBacklogExecute(taskID)` 对**一行** pending merge-backlog 行执行
守卫链（fail-soft，顺序即安全论证）：

| 步骤 | 守卫 | 行为 |
|---|---|---|
| ① row gate | summary 必须解析为 `merge <branch> into master` | 非本行 → skipped（行留 pending） |
| ② dirty gate | 主 checkout `git status --porcelain` 必须空（含 untracked——hook v5 教训） | dirty → skipped，**零步骤**（行留 pending，下次 pass 重试） |
| ②b 目标守卫 | 主 checkout 必须在 master/main 上 | 否则 failed + 行 failed（绝不 merge 进错误 ref） |
| ③ safe-merge | Go 版 safe-merge.sh：archive tag / loser·archive 前缀 / worktree 占用 | 命中 → claimed+started+**failed**（阻断是 REPORT，不静默跳过） |
| ④ merge | `git merge --no-commit --no-ff`（adoption 链 probe 形状）；**已合并**（`merge-base --is-ancestor` 分支 tip 已是 HEAD 祖先）→ 直接 complete + branch -d（git merge 会 exit 0 "Already up to date" 但无 commit，故前置探测） | 冲突 → `merge --abort` + failed（master 不动、分支存活） |
| ⑤ close-out | — | commit（`merge-backlog: merge <branch>`）+ `branch -d` + **completed** writeback |

**repo==worktree 形态（验收④）**：deliverable.repo 可能指向 linked
worktree（t-1788157112 实况）。merge 必须落在**主 checkout 的 master**，
不能动 worktree 自己的分支。`mergeExecMainRepo` 用
`git rev-parse --path-format=absolute --git-common-dir` 解析主仓（83f73f1
同款修复）——main checkout 的 common-dir 是 `<main>/.git`，worktree 的是
`<main>/.git/worktrees/<name>`，取第一个 `.git` 组件之前的路径。

**状态机合法性**：消耗行的路径（safe-merge 阻断/目标不匹配/冲突）都先
claim（pending→claimed）再 start（claimed→in_progress）再 failed
writeback（in_progress→failed 合法）；dirty 守卫不消耗任何状态。成功路径
claim→start→completed。测试确认 `pending→failed` 非法被任务状态机拒绝，
所以必须先走 claimed→in_progress。**防偷抢**：claimed 但 claimed_by≠
merge-executor 的行必须 skip（LLM dispatch 可能正在跑同一行，双 claim=
双 merge）——只有自己 holder 的 claimed 行才可恢复。

### 2b. CLI `gopi task merge-execute <task-id> [--db PATH]`（cli_tasks.go）

- `parseTasksArgs` 新 verb case：恰好一个位置参数 + `--db`。
- `taskFlagMember` 新 verb 白名单。
- dispatch case → `runTaskMergeExecute`（打印 `status=... branch=... repo=...
  reason=...` + 每步 `[ok|FAIL] step detail`）。
- usage 段补充。

### 2c. serve pass 步骤（cli_tasks.go）

`serveMergeExecSweep()`：在 merge-backlog audit 之后、dispatch 之前跑，
throttle `GOPI_MERGE_EXECUTE_INTERVAL`（默认 900s，负值禁用，单源
`mergeExecIntervalFromEnv`），每次最多 `mergeExecScanMax=5` 行
（`tags LIKE '%merge-backlog%' AND status IN ('pending','claimed')`）。
每行 fail-soft；skipped 行不打印（常规守卫），completed/failed 打印报告行。

### 2d. 测试

- **单测（引擎，真 git fixture）** `internal/tools/task_merge_executor_test.go`：
  12 个用例——parse 表、clean merge、**repo==worktree（验收④）**、dirty
  skip、conflict abort、archive-tag 阻断、worktree 占用阻断、非 merge 行
  skip、终态行幂等 skip、目标不匹配、**已合并（already-merged）complete**、
  **claimed-by-other 不偷抢**。
- **CLI 测试** `cmd/agent/cli_tasks_merge_exec_test.go`：env 解析（6 例）、
  parse（拒绝零/两个位置参数、跨 verb flag）、flag 成员、serve sweep
  throttle（沙箱 GOPI_HOME，绝不碰生产台账）。
- **临时 live E2E**（`zz_merge_exec_live_e2e_test.go`，GOPI_E2E_MERGE_LIVE=1，
  真二进制 + 沙箱 GOPI_HOME 台账 + 真 git fixture，**跑完已删**）：4 用例全过
  —— ①dirty→skip 行留 pending ②clean merge→completed+branch -d+master
  前移 ③真冲突→failed+master 不动+分支存活+abort 后树干净 ④repo==worktree
  →merge 落在 master（worktree 自己分支不动）。

## 3. Not Done

- **生产台账不消费**：`t-1788275389`（人工提交的 "消化 21 行 hp/* pending"
  行，summary 非 merge 形状）经真二进制 merge-execute 验证正确 skipped，
  保持 pending。serve 常驻时 sweep 会在 15min 节拍下处理**未来** audit 入队
  的 merge 行——本任务不主动动 hp 的 21 行真实 pending（epic 约束：测试用
  沙箱台账）。
- 未做 merge 后 push-back（本任务范围是本地 master 收编；远端 push 由
  serve git sync 通道负责）。
- 未给 executor 加 LLM 工具（Rule 3：基础设施，非 agent 工具）。

## 4. Verification

- Build: `go build ./...` OK；`go vet ./internal/tools/ ./cmd/agent/` OK；
  `gofmt -l` 我的文件全干净。
- Engine 单测：`go test ./internal/tools/ -run TestMergeExec` 12/12 PASS
  （含 already-merged complete、claimed-by-other 不偷抢）。
- CLI 单测：`go test ./cmd/agent/ -run 'TestMergeExec|...MergeExecute...'`
  全 PASS。
- Live E2E：`GOPI_E2E_MERGE_LIVE=1 go test ./cmd/agent/ -run
  TestMergeExecLive_AllCases -v` → 4/4 PASS（含 repo==worktree）。
- 全量：`go test ./... -timeout 30m`（job_start 后台跑）**全绿**——
  internal/tools 772s、cmd/agent 146s、testsuite 33s。注意 internal/tools
  包本身就要 ~13min（大量 t.TempDir sqlite I/O）——默认 600s `-timeout`
  会 panic 超时（本机环境问题，master baseline 同样复现，见 memory #1198/#1182）；
  快速迭代只跑 `-run TestMergeExec`，判定回归必须 baseline 对照。
- Commit: `65efd1e` on `feat/merge-executor-live`（主仓分支，worktree-shared）。
  恢复：`git show feat/merge-executor-live:devdoc/tasks-merge-executor-live.md`
- 生产台账安全：`t-1788275389` 保持 pending（真二进制验证 skipped 路径）。

## 5. 坑（未来 agent 必读）

1. **`finishCleanupFirstLine` 的 "(no output)" 哨兵**：git 输出为空时它返回
   "(no output)"——porcelain（空=干净）、tag -l（空=无 tag）都必须显式把该
   哨兵当"空"，否则干净树/无 tag 会被误判 dirty/已归档。两处都加了
   `&& !strings.Contains(out, "no output")`。
2. **worktree list --porcelain 是多行输出**：`finishCleanupFirstLine` 只取
   第一行会把 branch 行截掉——占用守卫必须用 RAW 输出
   （`mergeExecGitRaw`）。
3. **merge 必须 --no-commit --no-ff**（adoption 形状）：普通 `git merge`
   自动 commit，冲突后 abort 语义不同；显式 probe 才能先验证再 commit。
4. **目标分支守卫**：主 checkout 不在 master 上时 merge 会进错误 ref——
   ②b 步骤 failed 掉行，绝不猜。
5. **临时 live E2E 用完即删**（hp finish-cleanup 模式）；单测保留。
6. **already-merged 前置探测**：`git merge --no-commit --no-ff` 对已合并
   分支 exit 0 且输出 "Already up to date." 但**没有 staging**——后面 commit
   会 "nothing to commit" 卡住。必须用 `git merge-base --is-ancestor`（exit
   code 是真相）前置判断：已合并 → 直接 complete + branch -d，不重 merge。

## 6. Deliverable

- 分支：`feat/merge-executor-live`
- 恢复：`git show feat/merge-executor-live:devdoc/tasks-merge-executor-live.md`
- 本报告在 master 合并后生效；提交在共享仓（worktree 交付形态）。
