<!-- doc-class: historical -->
# Ad-hoc Guard: 1214 错误降级阶梯（临时方案）

> **AD-HOC — 临时方案，根因查清后整体移除。**
> mark ad-hoc: root cause = compact 后 messages 形态，查清后移除
> （error_1214.go / error_1214_test.go / claude_client.go 内 GUARD 注释）

## 背景

2026-12-14 两例实锤（nick 授权）：1.5h+ 长会话首轮合法中途流式 1214。
嫌疑最大：compact/上下文折叠后的 messages 形态。静态 config 管不了运行时
形态变化 → 授权 ad-hoc guard code 让 gopi 先能用。

## 机制（internal/deepseek/error_1214.go）

检测 `HTTP 400` + body 含 `"1214"` → 走降级阶梯：

| 档位 | 动作 |
|------|------|
| 0 | 原请求发送（两条发送路径 ClaudeChat / ClaudeChatStream 都接了 guard） |
| 1 | strip optional 参数：thinking / reasoning_effort / cache_control / tools，留 model+messages+max_tokens+stream 最小核 |
| 2 | messages 本体降级：system 块合并为单 text 块 / tool_result null content 归一为 text 占位 / 超长 text·thinking 截断（100K runes） |
| 3 | 仍 1214 → fail-fast + **死前完整请求体落盘**（bug037 简版） |

落盘：`$GOPI_1214_DUMP_DIR`（默认 `<tmp>/gopi-1214-dumps/`）下
`<ts>.dump.json`，含 timestamp / api_error_body / original+stripped+degraded
三个请求变体 —— resume 可续、请求可回放。

## 关键实现决策

1. **retryDo 不动**（旧路径 429/5xx 重试逻辑保留）。guard 挂在两条发送路径的
   `retryDo` 错误出口：operation 内部截获最终 status+body（retryDo 会把
   400 合成 error 丢掉 body，guard 需要原始 body 分类 1214）。
2. **ladder roundtrip 不走 retryDo**：一次 raw send，HTTP 4xx/5xx 作为
   *数据*返回（status, body, nil-err）—— ladder 自己做分类与重发。
3. **baseline 复用**：guard 触发时首次 1214 已发生，ladder 的第 0 次 send
   直接复用已观测结果，不重发原请求。
4. **streaming 接受后重放**：stream 路径 ladder 接受变体后重新走
   ClaudeChatStream（roundtrip 的 200 body 丢弃，多花一次请求 —— ad-hoc
   成本可接受）；非流式路径直接解析 roundtrip 已读的 body，无多余发送。
5. 所有阈值命名常量（Rule 1）：`error1214Marker` / `guardDegradedMaxTextLen`
   / `errorDumpMaxTail`。

## 验证

- 单测 7 例全过（error_1214_test.go）：rung1 接受 / rung2 接受（system
  合并断言）/ 耗尽→dump+fail-fast（dump 结构断言）/ 非流式 e2e / 流式
  e2e（SSE 回放）/ 非 1214 的 400 不触发（sends==1）/ tool_result null
  content 归一。
- `go build ./...` + `go vet ./internal/deepseek/` 干净。
- deepseek / agent / config 包全部 PASS（其余包与本次改动无关）。

## 未做 / 后续

- **根因排查**（compact 后 messages 形态到底哪种形态触发 1214）——本单只
  做止血。查清后：移除 error_1214.go、error_1214_test.go、两条发送路径的
  GUARD 块，恢复 retryDo 原样。
- 1214 落盘目前只在 guard 耗尽时；正式 bug037（结构化 dump）另有安排。
- strip 档目前把 tools 一并剥掉（最小核口径）；若某任务必须带 tools 可
  考虑保留 tools 只剥 thinking —— 待真实案例再定。
