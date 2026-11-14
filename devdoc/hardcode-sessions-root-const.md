<!-- doc-class: historical -->
# Task Report: sessions 目录字面量 → config.SessionsDirName 单源收敛

## Completed

hardcoding-vs-constant 全库审查（epic t-1788904463）子发现落地：`"sessions"` 目录名
字面量 11 处（briefing 明列 10 + 审查中补 1）全部收敛到单一导出常量
`config.SessionsDirName`（internal/config/gopihome.go），替代原 task_session.go:67
的 unexported `taskSessionsRootName` 与各文件裸写。

**单源归属决策**：epic 建议导出 `tools.TaskSessionsRootName`，但审查实证
internal/gopilog/reader.go:19 有第二份 `const sessionsDir = "sessions"`（#1549
双常量反模式同型），而 gopilog 不 import internal/tools（import 方向禁止）。四处
消费包（tools/agent/gopilog/cmd/agent）共同且唯一的公共底座是 internal/config →
常量放 config，gopilog 的本地常量改为 `= config.SessionsDirName` 别名（保留本地
名，改动最小）。task_session.go 的 taskSessionsRootName 同样降级为
`= config.SessionsDirName` 别名（保留任务域语义命名，零行为差）。

### 改动清单（全部 `filepath.Join(..., "sessions")` → `config.SessionsDirName`）
- internal/config/gopihome.go：新增 `SessionsDirName = "sessions"`（含 doc 注释）
- internal/tools/task_session.go：67 常量 `= config.SessionsDirName`（127/149 引用不动）
- internal/tools/tasks.go：1501/1537（taskSessionLogPath/taskSessionStartEvent，gd 根）、
  1682（taskBriefingFromSession，`home/.gopi` audit 读面）
- internal/tools/taskzombie.go：120
- internal/tools/task_deliverable_branch.go：74/124
- internal/tools/chat.go：750（chatSessionsDir）
- internal/tools/memory_brew_run.go：28（brewSessionsDir）
- internal/agent/session.go：65/626（sessionDirForID / DetectOrphanSessions）
- internal/gopilog/reader.go：19 本地常量别名（消除第二份字面量）
- cmd/agent/cli_tasks.go：1389（newestSessionJSONLAfter）
- internal/testutil/session.go：56（审查中补的裸写点 —— 非 _test 测试库也属生产面）

### 穷举守卫测试
internal/config/sessions_dir_name_test.go：
`TestSessionsDirNameNoHardcodedLiterals` — WalkDir 扫 internal/ + cmd/ 全部非 _test
.go，凡含 `"sessions"` 的行必须命中豁免白名单（常量自定义、`= config.SessionsDirName`
别名、`json:"sessions"` wire tag、`case "sessions":` CLI 动词），否则 FAIL。改名
/迁移（DB-cleanup uuid t-1788905574 协作面）时漏 1 处即红。

## Not Done
- 未动 tasks.go:1682 的 `os.UserHomeDir() + "/.gopi"` 路径构建法本身（GOPI_HOME 不感知
  的 audit 读面，另一缺陷域，非本 ticket 目录名字面量范围；字面量已收敛，路径法另报）。
- cmd/agent/commands.go:338 `case "sessions"` 为 /log 子命令动词（非目录），豁免登记，
  语义与 toolnames 动词模式一致，未改。
- 测试文件（*_test.go）内 sandbox 夹具路径豁免（本应写真实沙箱路径），未改 ——
  守卫测试明示豁免依据。

## Verification
- Build: `go build ./...` OK（worktree，master 基线之上）
- Guard: `go test ./internal/config/ -run TestSessionsDirNameNoHardcodedLiterals` PASS；
  落地前该测试红（捕获 4 处：gopihome 自定义/两处真漏/两处假阳性），落地后绿
- Prod join-literal grep: `Join(...,"sessions")` 非 test 源码 0 命中
- 残留 `"sessions"` 仅豁免 4 处：gopihome.go:25/29（doc+def）、stats.go:117（json tag）、
  commands.go:338（CLI 动词）
- Tests: `go test -p 1 ./internal/tools/ ./internal/agent/ ./internal/gopilog/
  ./internal/config/` — tools 322.197s ok / agent 35.396s ok / gopilog 1.027s ok /
  config 0.107s ok（含 task_session 创建/读取/清理全套 —— 行为不变实证）
- gofmt：本次改动文件全 clean；chat.go/session.go/cli_tasks.go 的 gofmt 违例为
  master 基线既有（stash 验证 pristine 亦点名），非本次引入，未顺手修
- Commit: `1ad2b34` refactor(sessions): 'sessions' dir literals → config.SessionsDirName single source
