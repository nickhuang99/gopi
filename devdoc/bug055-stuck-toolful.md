<!-- doc-class: historical -->
# Task Report: bug055 — toolStreak 第二计数器（同形 tool_use 监控循环兜底）

Date: 2026-09-02 · Branch: `feat/bug055-stuck-toolful` · Files:
`internal/agent/loop.go`, `internal/agent/loop_stuck_test.go`

## 背景：stuck-net 的盲区（10.7h 烧钱复发）

第一代 stuck-net（commit 0ab164d，`stuckState`，incident t-1788173679 12h
兜底）只抓**连续 N 轮无 tool_use 且文本近似**。bug055 实锤其盲区：

- **Bond 718319**（session 2b38bfa2，2026-09-02，M4.23 任务 12:34 启动）：
  任务交付后被收编，进程未退出，进入监控循环——`bash "sleep 115; ssh hp
  'sqlite3 … SELECT task_id,status…'"` **连续重复 272 次**（每轮约 120s），
  总烧 **10h15m**。
- 监控循环**每轮都有 tool_use**（bash 查询）→ `stuckState` 的
  tool_use-reset 臂每轮清零 streak → **永不触发**。
- 同型事故 t-1788173679（12h，4276 iter）此前已发生——两个事故共享
  "真工作做完后的 monitor loop" DNA。
- bug050（toolStreak 第二计数器）曾标 completed 且 tags 含 sync-resolved，
  但 master grep toolStreak **零命中**——实现从未存在，虚假关账（bug053）。

## 实现（本分支 = bug050 的真实落地）

### A. `toolStreakState` 第二计数器（loop.go）

- 常量（Rule 1，无魔法数）：
  - `maxStuckToolfulRounds = 10`（N=10：sleep 115 → 10 轮 ≈ 20 分钟 = 合理
    "等待上限"；事故是 272 轮/10h，N=10 会在 ~20min 截断）
  - `notifyEventLoopStuckToolful = "loop-stuck-toolful"`
  - `stuckToolfulRoundsEnv = "GOPI_STUCK_TOOLFUL_ROUNDS"`
- `toolFingerprint(toolCalls, text) (name, params, textPref)`：一轮的
  tool_use 身份 = (工具名串联, 参数 hash, 文本前缀)。参数规范化：
  **json.Marshal(Arguments) → sha256**（map 序列化按键排序——
  devdoc/resume-feasibility.md:152——参数顺序变化不掩盖身份，只有**值**变化
  才改 hash）；文本前缀复用 stuckTextPrefixChars=50 rune 窗口。多 tool_use
  轮按序拼接（一轮=一个指纹，整轮相同才算同）。
- `observe`：有 tool_use 且指纹与上一轮相同 → streak++；**指纹不同
  （参数变化=真工作）或 no-tool 轮 → 不累计**。no-tool 轮既不计数也不重置
  （与 stuckState 空文本语义一致——混合 monitor loop 仍累计）。
- `stuckToolfulRounds()`：`GOPI_STUCK_TOOLFUL_ROUNDS` **唯一读取点**
  （Rule 2）：unset→10，0→禁用，正数→覆盖，非法→默认+stderr 警告。
- `notifyLoopStuckToolful`：同 notifyLoopStuck 管线，event=loop-stuck-toolful。

### B. 接入点（与 stuckState 同一位置）

`stuck.observe` 之后立即 `toolStreak.observe(toolCalls, content)` ——
finish_reason switch **之前**的单一喂入点，任何 continue 路径（truncated/
end-turn nudge/completion checkpoint）无法绕过。Tripped → stderr ⛔ 日志 +
`notifyLoopStuckToolful` + error 返回（错误消息引用 loop-stuck-toolful）。

### C. 重置条件

参数变化（真工作每轮参数不同——`sleep 115` + 固定 ssh 命令 = 每轮完全相同；
真进度轮询参数会变）；tool 名变化；文本前缀分歧（同参数但文本不同 = 不同轮）。

## 测试（loop_stuck_test.go，全部 PASS）

| 测试 | 验证 |
|---|---|
| `TestToolStreak_StateUnit` | 指纹/累计/重置语义：同 tool+同参+同文累计；参数变→1；no-tool 不干扰；tool 名变→1；文本分歧→1 |
| `TestToolStreak_EnvOverride` | unset→10/0→禁用/正数→覆盖/bogus→默认+警告 |
| `TestAgent_StuckToolfulLoop_TerminatesAndNotifies` | mock 连续同 bash tool_use → ≤11 轮 terminate + loop-stuck-toolful notify 落盘 + **loop-stuck（第一计数器）不误发**（`"event":"loop-stuck"` 精确匹配，避免子串误报） |
| `TestAgent_StuckToolfulLoop_ParamChangeNoTrip` | 同 tool+同文本但命令每轮变 → 20 轮不触发（真工作不误伤） |
| `TestAgent_StuckToolfulLoop_Disabled` | GOPI_STUCK_TOOLFUL_ROUNDS=0 → 禁用，跑到迭代上限 |
| 既有 6 测试 | 全部保持 PASS（`TestAgent_WallclockCap_GracefulFinish` 的 mock 改为每轮参数变化的 tool_use——原来"endless identical todo"正是新检测器要抓的形状，测试意图是 wallclock 而非 stuck） |

**测试陷阱**：mock 的 tool_use 会真实执行——脚本里 `bash sleep 115` 会真的
挂 115s（首个测试运行直接超时）。`newStuckTestAgent` 必须 stub
`executeToolFn` 返回良性结果（检测器只消费轮次 tool_use 形状，不消费输出）。

## Verification

- Build: `go build ./internal/agent/` OK
- Tests: `go test ./internal/agent/ -run 'TestToolStreak|TestAgent_StuckToolful|…'`
  **ok**（10/10）；全量 `go test ./internal/agent/` 结果见下方（job 输出）
- Commit: 见 git log

## Not done / deferred

- 无自动 resume：loop-stuck-toolful 终止的 run 按失败记账，操作者重派。
- 参数规范化仅覆盖 `Arguments` map（JSON 序列化 hash）；`RawArgs` 原始串
  与解析后 map 等价（值相同 hash 相同）——已通过 json.Marshal 统一。

## Ops notes

- 订阅：`GOPI_TASK_NOTIFY_EVENTS=loop-stuck,loop-stuck-toolful,session-wallclock-exceeded,stale`
- 调参：`GOPI_STUCK_TOOLFUL_ROUNDS`（默认 10；0=禁用）
- loop-stuck-toolful 告警 = LLM 连续 10 轮重复同一 tool_use —— 检查 session
  JSONL 尾部再重派（10h 烧钱事故的机械兜底）。
