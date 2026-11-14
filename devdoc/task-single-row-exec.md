<!-- doc-class: historical -->
# Task Report: task-single-row-exec — eliminate task run dual-row redundancy

## Epic goal
auto 镜像行只保留给"无工单执行"（裸 prompt/chat）；task run 执行信息全部写回 human 行，
消除 human + auto dup-of 双行冗余（关账×2 / 显示×2 / 同步×2 / stale×2）。

## Completed
1. **taskregister.go maybeRegisterSessionTask wrapper-child gate**（主修复）:
   - `GOPI_TASK_WRAPPER_ID` 存在（我是 task run child，执行已有 human 工单）→
     不再登记 auto 镜像行，直接返回；`autoTaskRowID` 锁到 wrapper 行的 task_id
     （liveness heartbeat 打到唯一 human 行）。
   - 无 wrapper env（裸 prompt/chat/web）→ 照常登记 auto 行 —— auto 行保留其唯一
     正当用途：给无工单执行一个审计载体（E2）。
   - 删除了 wrapper tag（TaskTagWrapper / wrapper:<id>）与 mirror run provenance
     镜像逻辑（TaskRunProvenance 调用点）。
2. **移除双行渲染**:
   - `task list` [dup-of <wrapper>] 标记删除。
   - `task show` linked_to（auto 侧）与 executor（human 侧反查）行删除。
   - 删除死代码 helper：taskWrapperTagValue、taskExecutorTaskID、TaskTagWrapper。
3. **TaskReconcileMirrorCloseout 保留**（历史审计 + 防御）:只对仍 in_progress 的
   存量 legacy auto 镜像行生效（新执行不再产生镜像，reconcile 变 no-op）；函数文档
   改为 legacy-mirror seam 语义。taskTagWrapperPrefix 常量保留（历史行解析需要）。
4. **存量双行数据不动**（E6）:completed 的 dup-of 对保持原样（append-only 审计）；
   only in_progress / 未来执行不再产生新对。
5. **其他读双行的地方逐一核查**（设计任务 4）:
   - finish-cleanup（cli_tasks.go:1237 runTaskRunOnce 内 + serveFinishCleanupSweep）
     的查询条件是 `deliverable LIKE '%"repo"%'` —— auto 镜像行从不携带
     deliverable.repo（worktree backfill 经 TaskWrapperEnv 只写 human 行），
     天然只命中 human 行，无需改。
   - TaskDeliverableWorktree / TaskDeliverableWorktreeBash 读 TaskWrapperEnv 找
     human 行 → 保持（就是本设计的执行行）。只更新了注释（不再提 mirror 配对）。
   - resume --task 用 resumed:<session-id> tag，与 wrapper tag 无关 → 无影响。
   - main.go SIGPIPE ignore 的 TaskWrapperEnv gate → 保持（child 身份判定不变）。
   - heartbeat（loop.go:2452 → taskheartbeat.go touch）在 wrapper child 下
     autoTaskRowID = human wrapper id，WHERE task_id=t-xxx AND in_progress 命中
     human 行 —— 执行中 liveness 落在唯一执行行（E3 增强而非回归）。
6. **回归测试全部改写为 single-row 断言**:
   - internal/agent/taskregister_test.go:
     TestAutoTaskRegisterWrapperChildNoAutoRow（child 不登记 auto 行 + autoTaskRowID
     = wrapper id + END hook no-op）；TestAutoTaskRegisterPlainSessionKeepsProvenanceFree。
   - internal/tools/tasks_test.go: TestTaskLinkageSingleRowNoTwoRowRender /
     TestTaskLinkageNoFalseLinkage / TestTaskLinkageListNoDupOfMarker；reconcile
     closeout 测试保留（legacy 语义）；seedWrapperAndMirror 改走测试本地
     TaskTagLegacyWrapperForTest（生产 TaskTagWrapper 已删）。
   - cmd/agent/cli_tasks_test.go: TestTaskRunSingleRowE2E（真 binary + mock LLM:
     task run 后 list 只有 human 行、无 [auto]、show 带 session_id/run_method/
     completed）。
   - cmd/agent/cli_tasks_waitpid_test.go: SIGKILL / non-zero / normal 三场景
     改写为 single-row 断言（wrapper 行终态 + 0 auto 行）。

## Not Done
- chat 表分流（M4.6）不在此任务范围 —— 只做执行行合并；web chat 已走 CHATS 表，
  与 tasks 两行问题无关（cli_web.go 确认）。
- 存量 in_progress dup-of 对的自动清理不做（epic 明确:历史审计不破坏；仅停发新的）。

## Verification
- Build: `go build ./...` OK；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK。
- go vet ./internal/tools/ ./internal/agent/ ./cmd/agent/: PASS。
- Tests:
  - `go test ./internal/agent/` : **52.4s ok**（含改写后的 register 测试）
  - `go test ./internal/tools/ -run 'TestTaskLinkage|TestReconcileMirrorCloseout|TestRunProvenance|TestAutoTask'` : ok
  - cmd/agent E2E（真 binary）:
    - TestTaskRunSingleRowE2E PASS —— E1（run→1 行，无 auto dup）
    - TestTaskRunSurvivesClosedStdoutPipe PASS
    - TestTaskRunChildSIGKILLClosesWrapperRowE2E PASS —— E4（关账正常）
    - TestTaskRunChildNonZeroExitClosesWrapperRowE2E PASS —— E4
    - TestTaskRunNormalExitSingleRowE2E PASS —— E3（human 行带 session_id）
  - E2（裸 prompt 仍登记 auto 行）: internal/agent register 测试覆盖（plain session
    无 wrapper env 照常登记）。
  - E6（存量 completed 双行不被破坏）: TaskReconcileMirrorCloseout legacy 测试
    保留（历史 in_progress 镜像仍可 reconcile；completed 不被双写）。
- Commit: (见 git log)

## Evidence of acceptance E1-E6
- E1: TestTaskRunSingleRowE2E —— list 无 [auto] 行、无 dup-of；show 只有 human 行。
- E2: TestAutoTaskRegisterPlainSessionKeepsProvenanceFree / TestAutoTaskRegisterOnAndEndHook
  —— 无 wrapper env 时 auto 行照常登记+关闭。
- E3: E2E human show 断言 session_id / run_method / status completed；heartbeat
  锁 autoTaskRowID=wrapper id（loop.go）。
- E4: SIGKILL/non-zero E2E —— wrapper 行 failed；normal E2E —— completed。
- E5: internal/agent 52s 全绿 + cmd/agent E2E 全 PASS。
- E6: reconcile closeout 测试（legacy 镜像仍可关闭；completed 不双写）。

## Post-merge re-verification (master, fresh bin/gopi)
Merge 后全量回归在 master 主 repo + 重建的 bin/gopi 下重跑（E2E 用 module root
bin/gopi —— GopiBin 逻辑）:
- `go test ./cmd/agent/` : **217s ok**（全套件，含 TestParseTasksArgs 系——此前 worktree
  reap 的 getwd 假失败在 master 下不存在）
- `go test ./internal/agent/ ./internal/tools/` : ok（53s + 551s）
- `go test ./internal/...`（其余包）: 全 ok，零 FAIL
- **stale-bin 教训**: merge 后若不同步 rebuild bin/gopi，E2E 会拿旧二进制跑新断言而
  失败（TestTaskRunSingleRowE2E 首次即此）——重跑前必须
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`（Rule 12 close loop）。
- 残留脚手架分支 feat/verify-single-row / feat/task-single-row-exec-r2 已归档
  （archive/single-row-verify-20260905 / archive/single-row-r2-20260905）并删除。
