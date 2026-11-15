<!-- doc-class: normative -->
# M4.8: task submit --repo → deliverable.repo (t-1788089584)

信息闭环的补全：task 行最重要的被忽视参数 = **git repo path**。submit 带上 repo →
task show 展示 → 后续 verify/merge（M4.9 执行方回填 branch/worktree）**零记忆**干活。
本单自足（原依赖单未物化，已并入本单交付）。

## Completed

1. **`--repo` flag（CLI submit）** — `cmd/agent/cli_tasks.go`
   - `parseTasksArgs("submit", ...)` 接受 `--repo PATH`（`--repo X` 与 `--repo=X` 皆可，
     memoryFlagSet 惯例）；**缺省 = submitter 的当前目录**（parse 时 `os.Getwd`；
     server 模式 = workspace cwd）。cwd 解析失败 → 参数错误，submit 不发生。
   - `taskFlagMember` 白名单加 `repo`（仅 submit；`list --repo` 等仍被拒）。
   - `runTaskSubmit` 把 `opts.repo` 传入 `TaskSubmitOptions.Repo`。
   - tasksUsage 面更新（`gopi task -h`）；`internal/config/help.go` 同步。

2. **必要验证 + deliverable 落库（engine）** — `internal/tools/tasks.go`
   - `TaskSubmitOptions.Repo` 新字段；**非空时 `git -C <dir> rev-parse
     --is-inside-work-tree` 必须成功**（`taskRepoVerifiable`，10s 超时
     `taskDeliverableReposTimeout`）。失败 → **abort 整个 submit**（错误在 INSERT
     之前抛出，队列零残留——submitter 忘带/带错 repo = 操作非法，不是警告）。
     exit-0 但输出非 true（bare repo）同样拒绝：执行方必须能在里面开分支。
   - 验证通过 → `deliverable = {"repo":"<path>"}`（`taskDeliverableRepoJSON`）
     随 INSERT 落 `deliverable` 列。**空 Repo 不验证不落块**——引擎不自行解析
     cwd（cwd 语义归调用方：CLI 在 parse、tool 在 handler），引擎保持可测试。
   - schema：`deliverable TEXT DEFAULT ''` 进 CREATE literal + `tasksP2Columns`
     幂等 ALTER（存量库自动迁移，旧行保持 ''，**不回填**——历史行已闭环）
     + 状态重检 rebuild 的列位置拷贝（colExpr fallback `''`）。
   - `taskSelectColumns` / `scanTask` / `scanTaskRow` / `Task.Deliverable` 扩展
     （投影单点，所有读路径自动带上）。

3. **task show 展示** — `TaskShow` 在 submitted_by 之后渲染
   `deliverable.repo: <path>`（解析 JSON 失败/空 repo → 原样渲染 blob，
   向前兼容 M4.9 加 branch/worktree 字段）。无 deliverable 的行零新增行
   （与 #1108 列宽教训一致：不加 `—` 占位行，旧断言不受扰动）。

4. **LLM task_submit 工具同步** — `internal/agent/loop.go`
   - schema 加 `repo` 属性：描述写明**缺省 = 当前目录（正常情况，勿杜撰路径）
     + 验证失败整单 abort + 落 deliverable.repo**（首句路由决策 + 缺省语义）。
   - handler：`repo` 缺省时 `os.Getwd()`（与 CLI 同语义；Getwd 失败 → 错误返回），
     显式传参原样透传。
   - `validateToolArgs` 不变（仍只 summary 必填）。

## Not Done（M4.9 边界，本单不做）

- branch/worktree 回填：执行方结束后 `UPDATE tasks SET deliverable=...` —— M4.9 双向 sync 消费。
- memsync `SyncTaskExport` 的 SELECT 列不含 deliverable（列在含 send-data 判定的
  显式清单中，M4.9 扩展时加列即可；本次不动，保持 diff 最小）。
- `/task` slash 面无 submit 子命令（M4.5 起 enqueue 只走 `gopi task submit` /
  task_submit 工具），无需改动。

## Verification

- Build: `go build ./...` + `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- Vet: `go vet ./cmd/agent ./internal/tools ./internal/agent ./internal/config` OK
- 单测（新增 `internal/tools/tasks_deliverable_test.go` 5 例 +
  `internal/agent/task_submit_repo_test.go` 3 例 + `TestParseTasksArgsSubmitRepoFlag`）:
  - TestTaskSubmitRepoStampDeliverable（deliverable JSON 落库 + show 渲染）PASS
  - TestTaskSubmitRepoNonRepoAborts（abort + **0 行落库**）PASS
  - TestTaskSubmitRepoBareRepoAborts PASS
  - TestTaskSubmitNoRepoNoDeliverable（无 repo → 无块，旧行为不变）PASS
  - TestTaskSubmitRepoExplicitOverridesCwd PASS
  - TestTaskSubmitToolRepoDefaultCwd / RepoExplicitArg / RepoNonRepoAborts
    （工具 handler 面 cwd 缺省/显式/abort）PASS
  - TestParseTasksArgsSubmitRepoFlag（--repo 解析 + cwd 缺省 + 跨动词拒绝）PASS
- E2E（真二进制 + 沙箱 GOPI_MEMORY_DB，`/tmp/e2e-task-repo-param.sh`）：4/4 PASS
  ① ~/work/GoPi 内无 --repo submit → `deliverable.repo: /home/user/work/GoPi`
  ② /tmp（非 git）submit → 非零退出 + "not a git work tree" + 行不存在
  ③ --repo 显式覆盖 cwd → show 显示显式 repo
  ④ 无 deliverable 的行不渲染块
- 全量 `go test ./... -count=1`：见 tasks/<task-id>-report.md 收尾记录。

## 关键设计决策

- **验证放引擎、缺省放调用方**：`TaskSubmitOptions.Repo` 为空 = 引擎不碰文件系统
  （测试无 cwd 依赖）；cwd 缺省在 CLI parse 与 tool handler 两处实现（同一语义，
  注释互相指认）。后果：engine 层"漏传 repo"不落块（fail-open 到旧路径）而非
  abort——abort 语义只针对"显式给了坏 repo"。CLI/工具路径保证真实用户永远走到验证。
- **前置验证（INSERT 之前）**：失败时队列零残留；后半场（M4.9）若改为回填
  branch/worktree 也要走同样的"先验证后写"纪律。
- **abort 错误消息**带原始 git stderr，诊断不二跳。
