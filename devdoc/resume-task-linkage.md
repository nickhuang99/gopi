<!-- doc-class: historical -->
# Task Report: resume --task 台账关联 + 主 help resume/checkpoint 条目

Epic: nick 指出 resume 应暴露在 cmd line/help/log；实测补齐 ②主 help 条目与 ④task 台账关联。
分支: `feat/chat-remote-dispatch`（worktree ~/workspace/gopi-resume-task，基于 a364352）

## Completed
- **主 help 补条目**（internal/config/help.go FormatUsage）: Subcommands 段新增
  `gopi checkpoint list` / `gopi checkpoint export` / `gopi resume ... [--task TASK_ID]`
  三段简介（此前主 help 仅 GOPI_NO_ENFORCE_MEMORY 注释提过一句 checkpoints）。
- **`gopi resume --task <task_id>`**（cmd/agent/cli_resume.go）:
  - `parseResumeArgs`: --task VALUE / --task=VALUE；continuation 文件 positional
    兼容（修复了初版 --task 与 args[1] positional 冲突的回归——实测抓到）；未知
    -旗标拒绝（typo 防护）；空值报错；解析 pre-flight（坏参不浪费 checkpoint 加载）。
  - 新 agent 建立后、ag.Resume 前调 `tools.TaskTagResumed(taskID, ag.SessionLog.SessionID())`:
    新 session_id 以 `resumed:<session-id>` tag 追加进 task 行。**非致命**: 台账故障
    只 warn 不阻断恢复（恢复中断会话优先）。stderr 打印关联结果。
- ~~**`gopi task tag-resumed <task_id> <session_id>`**（cli_tasks.go + tasks.go）:
  补链/审计入口；--db 透传；usage 更新。~~ **该 CLI 拼写已于 2026-10-16 删除**
  （动词面退役：它是内部 resume 钩子，0 次执行）。**能力保留**：resume 在
  `cmd/agent/cli_resume.go` 里**直接进程内调用** `tools.TaskTagResumed`，日志
  仍是 stderr 的 `tagged resumed:<sid>`；回归断言为
  `TestResumeStampsTheFreshSessionOntoTheRow`。
- **internal/tools/tasks.go**: `taskTagResumedPrefix="resumed:"` + `taskAddTag`
  （幂等 tag 追加，TaskArchive 重构复用之，消重）+ `TaskTagResumed`（幂等，错误时
  报 unknown task / empty args）+ `TaskResumeCount`/`taskResumedTagCount`。
- **展示**: `task show` tags 行下新增 `resumed: ×N`（仅 N>0 时）；`task list` 行尾
  `[resumed×N]` 标记。
- **测试** (cmd/agent/cli_resume_task_test.go): TestParseResumeArgs（11 case）、
  TestTaskTagResumedRoundTrip（幂等/多 session 累积/unknown task/空参/show 断言）、
  TestTaskListShowsResumedMarker（标记恰好一次）。

## Not Done
- live API 的 failed→resume→再失败→重派 全闭环（M4.7 retry 策略本身）——本次只交付
  台账关联基建（重派策略读 `resumed:` tags 即可判断 resume 次数）。
- task submit 的 --tags 与 resumed: tag 的命名冲突未做防护（用户可自造 resumed:x 伪
  tag——与 resolved tag 同等信任级别，台账本为审计层）。

## Verification
- Build: `go build ./...` OK；`go vet ./cmd/agent ./internal/tools ./internal/config` clean
- Tests: `go test ./... -count=1` 全绿（tools 包 Task* 套件 4.0s OK；cmd/agent resume 套件 OK）
- 实测（真实二进制 bin/gopi）:
  - `gopi -h` 出现 checkpoint list/export + resume [--task TASK_ID] 条目 ✅
  - `gopi resume -h` 含 --task 示例 ✅
  - `task tag-resumed` ×3（含重复幂等）→ show `tags: resumed:a,resumed:b` + `resumed: ×2`
    + list 行尾 `[resumed×2]` ✅
  - 端到端: `gopi resume /tmp/gopi-raw-smoke.log --task t-smoke-1 [/tmp/cont.md]` 真实跑起
    ReAct loop，新 session 72c0a755/766be3f8 自动打 tag，show `resumed: ×4` ✅
  - JSONL 无损: resume 新 session 2 行（session_start + user continuation）✅
- Commit: 见分支 feat/chat-remote-dispatch（本次提交 hash 见 git log）

## Notes / 设计决策
- tag 而非新列: append-only 台账铁律下 tags 是既有可变字段（TaskArchive 先例），
  FTS5 tasks_fts/tri 触发器自动索引 tags，`task search resumed:xxx` 免费可用。
- 幂等 key = session_id: 同一 session 重放（crash 后重跑 resume）不重复计数。
- TaskArchive 重构复用 taskAddTag 后增加一次 re-read 保证确认串显示变更后 tags。
