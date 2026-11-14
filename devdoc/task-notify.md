<!-- doc-class: historical -->
# Task Report: task 状态变迁通知（feat/task-notify）

## Completed

### ① 状态变迁捕获（CLI 层 hook，非 SQLite 触发器）
选择 CLI 层而非 SQLite trigger 的原因：Go 层 hook 拿得到**迁移前状态**
（previous_status）、可复用状态机/配置解析/单测基建，且投递失败不影响
事务（trigger 里发 HTTP 会拖死写路径）。两个写入口全部挂上：
- `TaskUpdate`（internal/tools/tasks.go）：状态机门内记录 `prevStatus`
  （cur.Status），UPDATE 提交成功后重读行 → `notifyTaskTransition(prevStatus, updated)`。
  同状态 no-op 与 summary-only 编辑不通知。
- `TaskNext` CAS claim（pending→claimed）：CAS 赢家直接用内存中的
  candidate（已补 claimed_by/at）发通知，无需重读。

捕获覆盖的迁移 = taskAllowedTransitions 全表：pending→claimed→
in_progress→completed/failed + 恢复路径（pending 回退/重试/重开）。

### ② 通知目标可配置（env，每次触发热读取）
`~/work/tasks` subscribe 的 hot-reload 哲学：配置每次触发时重读，
改 env 立即生效、无需重启。

| env | 作用 | 默认 |
|---|---|---|
| `GOPI_TASK_NOTIFY_WEBHOOK` | webhook URL，POST JSON payload | 无（关） |
| `GOPI_TASK_NOTIFY_CMD` | shell 命令（sh -c），payload 走 stdin | 无（关） |
| `GOPI_TASK_NOTIFY_EVENTS` | 触发状态集（逗号分隔 / `all`） | claimed,in_progress,completed,failed |
| `GOPI_TASK_NOTIFY_PATTERN` | glob 筛选（见 ④） | 空=广播 |
| `GOPI_TASK_NOTIFY_ASYNC` | truthy=goroutine 异步投递 | 同步 |

两者可同时配置。投递失败只打 stderr（`[task-notify] …`），永不致命。
超时：webhook 10s / cmd 30s（进程组 kill，照抄 scheduler fireCmd）。

### ③ payload
```json
{"event":"completed","previous_status":"in_progress",
 "task_id":"t-123","status":"completed","summary":"…",
 "session_id":"…","model":"…","tags":"feat,web","source":"auto",
 "claimed_by":"host:pid","elapsed_seconds":1.0,"tokens":1234,
 "created_at":…,"updated_at":…,"timestamp":"RFC3339"}
```
- elapsed：ended_at−created_at（终态），updated_at 兜底（中间态）。
- tokens：从 summary 尾巴（auto-register 追加的 `tokens=N`）best-effort
  解析，取最后一次出现，缺失=0。

### ④ task_type_pattern 筛选（subscribe 机制对齐）
订阅存储未引入（GoPi 的通知走 env 单一来源，Rule 2）；pattern 语义与
`~/work/tasks/internal/scheduler/subscribe/notify.go` 的
`filepath.Match(sub.TaskTypePattern, tsk.Type)` 对齐，但匹配域适配 GoPi：
task.tags（整串 / 任一单个 tag）+ task_id。空 pattern = 广播；非法 glob
= 不匹配（不出错）。

### ⑤ 测试（internal/tools/tasknotify_test.go，全部 -run TestTaskNotify PASS）
- `TestTaskNotifyTransitionFires`：pending→claimed→in_progress→completed
  恰好 3 条通知，event/previous_status/task_id/summary/tokens=1234 全校验。
- `TestTaskNotifyNonSubscribedStatusSkipped`（负例）：EVENTS=completed 时
  claimed/in_progress/summary-only 零通知，completed 恰好 1 条。
- `TestTaskNotifyPatternFilter`：tags=feat,web 命中 `feat*`，chore 任务静默。
- `TestTaskNotifyDisabledByDefault`：无 env 时零通知。
- 单元：pattern 匹配矩阵 + tokens 解析矩阵。
- 测试注入方式：cmd 目标 = `cat >> $TMP/notify.log && echo >> …`，纯本地
  文件断言，无网络依赖。

## Not Done（有意defer）
- **settings 文件目标**：Epic 允许 env/settings 二选一配置；本实现取 env
  （GoPi 现有配置面就是 env，无 settings 文件基建；Rule 2 单 reader）。
  若未来需要文件配置，loadTaskNotifyConfig 是唯一改点。
- **订阅持久化（subscriptions.json + CLI 子命令）**：~/work/tasks 有
  Store/Add/Remove 全套；GoPi 侧通知消费者目前只有规则引擎一个，env 直配
  已覆盖，持久化订阅表待真实需求再引入。
- **hermes 目标类型**：scheduler 有第三种 target（chat 注入），GoPi 无此
  通道，未移植。
- **SQLite 触发器路线**：评估后放弃（见 ①），无 hooks 表/触发器残留。

## Verification
- Build: `go build ./...` OK；`go vet ./internal/tools/` clean
- Tests: `go test ./internal/tools/ -run TestTaskNotify -count=1` →
  ok 13.6s（7/7）；全量 tools+agent 回归见下方后台任务结果
- Commit: feat/task-notify 分支（worktree ~/workspace/gopi-task-notify，
  基于 master fa8368d）

## 设计要点备忘（未来 agent）
1. hook 必须在 UPDATE **提交后**触发（先 exec 后 notify）——投递失败不能
   回滚台账写入；prevStatus 从状态机门内的 cur 取，不是重读（重读已是新值）。
2. TaskNext 的 CAS 赢家通知用内存 cand 即可：WHERE status=pending 保证
   它就是提交后的行。
3. notifyTaskTransition 对 prevStatus==status 直接 return——状态机允许的
   幂等写回（completed→completed）不算迁移。
4. 测试 cmd 目标务必把换行写进同一文件（`cat >> f && echo >> f`），
   `; echo` 会把换行送进 stdout 而不是日志。
