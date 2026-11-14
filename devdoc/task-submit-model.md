<!-- doc-class: normative -->
# Task Report: task submit --model 旗标 + 执行端 model 回填（task-submit-model）

## 背景（nick 发现的问题）
`gopi task list` 里 human source 行显示 `model (unknown)`。根因：`task submit` CLI
没有 `--model` 参数，human 行的 model 列恒空；auto 行有值是因为 taskregister
在启动时从进程带 `a.Model` 写入（TaskNew）。

## Completed
1. **① `task submit --model` 旗标**（cmd/agent/cli_tasks.go）
   - `tasksOptions.model` + `--model` 解析（`=` 语法同 memoryFlagSet 约定）
   - `taskFlagMember("submit")` 白名单加 `model`（跨动词拒绝保持：list --model 报 unknown flag）
   - usage 文档更新（默认策略：不填留空，执行后回填）
   - `runTaskSubmit` → `tools.TaskSubmitOptions.Model` 透传（engine 的
     TaskSubmitOptions.Model 字段与 INSERT 已存在——root cause 纯 CLI 面缺失）
2. **② 回填 stamp**（internal/tools/tasks.go + cmd/agent/{main,cli_tasks}.go）
   - `TaskUpdateWithResult`（harness-only 写回通道）写入前调用
     `taskExecutorSessionModel()` → `taskBackfillModel`：仅当 model 列为空
     时单条 UPDATE 回填（绝不覆盖 submit 时 --model 值/auto 行创建戳）
   - 执行端 session 解析双通道：
     - `GOPI_TASK_CHILD_SESSION`（TaskChildSessionEnv）：`task run` 父进程在子
       gopi 退出后，从 sessions 目录取 run 窗口内最新 .jsonl（newestSessionJSONLIn）
       解析 child session id 并 export——写回发生在父进程，子进程 env 不可见
     - `GOPI_EXECUTOR_SESSION_ID`（TaskExecutorSessionEnv）：`gopi` 启动时
       printLogPaths export 自身 session id——覆盖在执行会话内触发的写回
       （如 auto-register end hook）
   - session → model 解析复用 `taskModelFromSession`（session_start 首行，
     权威源；与 show 的 legacy fallback 同源）
3. **③ 存量回填脚本**（scripts/backfill-task-model.sh）
   - 机械映射：model='' 且有 session 指针（session_id 列 或 task_id 本身是
     session id）的行 → session JSONL session_start 提 model
   - 默认 dry-run；--apply 需 memory.db.bak-task-backfill 备份存在；
     单事务 + model='' 守卫 + integrity_check；幂等（重跑 nothing to apply）
   - **实测结论：本库 91 条空 model 行全部无 session 指针（human submit 行
     session_id 恒空），114 条带 session 的行已全部有 model** → 按 Epic ④
     诚实留 (unknown)。曾尝试 raw-log 映射（88/88 UNRESOLVED——raw log 记
     LLM 流量不含 task_id，无关联键），放弃，不做无据猜测回填
4. **④ 显示保持现状**：list 渲染 persisted model only；空 → (unknown)。
   数据修好后自然显示，未改显示逻辑
5. **单测**（回归守卫）
   - internal/tools: TestTaskSubmitModelFlag（--model 写入/缺省留空）、
     TestTaskUpdateWithResultBackfillsModel（①空行+child session→回填
     ②submit 值不被覆盖 ③无 session 文件写回不失败）
   - cmd/agent: TestParseTasksArgsSubmitModelFlag（解析/缺省/=语法/跨动词拒绝）

## E2E 实测（bin/gopi + /tmp/e2e-home 隔离 HOME）
- `submit --model deepseek-e2e` → list: `(model deepseek-e2e, source human)` ✅
- `submit`（无 --model）→ run 后 child 执行完 → 写回回填
  `model backfilled to "glm-5.3-flash" at writeback` → list 显示
  `(model glm-5.3-flash, source human)` ✅
- 存量行：可解析的（无——见 ③ 结论）… (unknown) 如实保留 ✅

## Not Done
- raw-log 反查回填：无关联键（task_id 不出现在 raw log），已判不可行——
  不做猜测性回填
- `gopi task verify` 的 verifier-child 写回路径未接 child-session 解析
  （verify child 用 TaskVerify 自建，其 model 回填走 TaskExecutorSessionEnv
  兜底通道，行为 best-effort 一致）

## Rule 11 适配说明
本改动是 CLI 旗标 + 引擎回填（task ledger 审计层），`task_submit` 工具的
description/参数未动（Model 由 harness 注入 `a.Model`，LLM 不可传）——
Rule 11 的"LLM 真实调用该工具"验收不适用于工具面未变的改动。等价验收 =
三场景 E2E（命令真实执行 + 行为可观测，见上）。工具面佐证：近期活跃会话
（c2311c60…）的 session JSONL 里有真实 `"name":"task_submit"` tool_use
记录——task_submit 路由面完好。若未来改的是 task_submit/task_evaluate 的
LLM 路由面，则必须走 Rule 11 原路径（tool_use 记录验证 + PA 复核）。

## Verification
- Build: `go build ./...` OK（CGO_ENABLED=0 bin/gopi OK）
- Tests: cmd/agent + internal/tools + internal/agent 全量 go test — PASS
  （internal/tools 唯一红 TestBashDaemonSpawn_DoesNotLeakProcesses 为
  master 既有 flaky：单跑 3x 均绿，与本改动无关）
- Vet: go vet clean；gofmt clean（改动文件）
- E2E: 三场景实测通过（见上）
- Commit: 027846e feat(tasks): task submit --model flag + executor-model
  backfill at writeback (branch feat/task-submit-model, worktree-shared
  with ~/work/GoPi — no push needed)
