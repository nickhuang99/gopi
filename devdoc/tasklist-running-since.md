<!-- doc-class: historical -->
# Task Report: task list in_progress 行 running 起始时间+时长显示（M4.12 显示层）

## Completed
- `internal/tools/tasks.go`：`taskRunningSuffix(t *Task, now time.Time)` —— 仅 `in_progress` 状态渲染
  `  [running <dur> since <HH:MM:SS>]` 后缀，锚点 = claimed_at（M4.5 liveness 写入）；
  claimed_at 缺失的 pre-M4.5 行回退 updated_at 且时钟带 `~` 标记（近似值诚实显示）。
- 时长格式化 `taskFormatDuration`：`2h13m5s` / `38.2s` / `700ms`（≥1min 截秒，sub-second 截 10ms，
  常量 `taskDurationTruncMilli`，Rule 1）。
- TaskList 行渲染接入：每次 list 读一次时钟（`now := time.Now()`），行内时长可比；负时长（时钟偏移）钳 0。
- 回归测试 `tasklist_running_test.go` 4 个：in_progress 行含 `[running 2h13m0s since <claimed 时钟>]`；
  pending（无 claimed_at）与 completed 行无 `[running` 字段；格式化表驱动；7 个不适用状态零渲染。
- 与 completed 行的 ended_at/elapsed 显示对称：终态行 elapsed 已定格，不重复渲染 running。

## Not Done
- `gopi task show` 未加 running 字段（show 已有 claimed_by (at …)，需求域仅限 list 渲染）。
- master 既有 gofmt 点名（taskSelectColumns 续行缩进）未顺手修——非本任务域，避免噪音 diff。

## Verification
- Build: `go build -o /tmp/gopi-check ./cmd/agent/` OK
- Tests: `go test ./internal/tools/ -run 'TestTaskListRunning|TestTaskListNoRunning|TestTaskFormatDuration|TestTaskRunningSuffixInapplicable'` 4/4 PASS
- 实测（真二进制读真实台账）：in_progress 行渲染 `[running 11m54s since ~19:29:19]`（auto 行 claimed_at
  NULL → ~ 回退路径实证），pending/completed 行无该字段。
- 全量回归 job 后台跑（internal/tools + cmd/agent），结果见 commit 前检查。

## Root-cause note（为什么第一版测试红）
- 首次实现把 `anchor = t.ClaimedAt` 写在了重构后的分支外（丢失赋值），claimed_at 存储正确但渲染永远回退
  updated_at——用一个最小探针测试（DB 真值 vs 渲染输出对照）定位，符合"根因优先/最小样本"。

## 设计决策
- 字段只对 in_progress 适用 = `taskRunningSuffix` 单点状态判断，pending（无 claimed_at）与终态（elapsed
  已定格）天然不适用。
- 回退路径显式化：pre-M4.5 老 in_progress 行无 claimed_at，直接不显示会漏掉"卡死 12h"这类巡查目标
  （本次 epic 的动因），`since ~HH:MM:SS` 保信息量且不撒谎。
