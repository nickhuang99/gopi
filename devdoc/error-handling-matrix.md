<!-- doc-class: normative -->
# Error Handling Matrix — provider-neutral LLM client errors

> Epic t-1788121946 重设计。取代 v1 的 '1214 特案 strip reasoning_effort'（vendor
> tunnel-vision，已删 `error_1214.go`）。原则：**按 HTTP 语义分类，禁止 vendor
> 分支**。

## 设计原则（provider-neutral）

1. **HTTP 语义分类，非 vendor 错误码**。4xx 就是确定性错误，无论错误体里写的是
   `1214`（GLM 形态）、`1210`（GLM always-thinking）、还是 DeepSeek 的
   empty-thinking message——分类器只看 status code。vendor 错误码不再出现在
   任何 Go 逻辑里。
2. **最小核 = 所有 LLM API 的最大公约数**。`model + messages + max_tokens +
   stream`。一次实现覆盖 DeepSeek 400 空 thinking 家族 / Anthropic 400 / GLM
   1214——同一形状三家都收。
3. **vendor 差异进配置数据不进 if 逻辑**。GLM always-thinking（1210 → omit
   thinking 字段）这类 per-provider 值域差异留在请求构造层的 config 数据 /
   注释里（`claude_client.go` BuildClaudeRequest）；错误处理路径零 vendor 分支。
4. **死前落盘 = 通用观测**。任何非 200：raw log 记 HTTP status + 错误响应体；
   确定性死亡再 dump 完整可重放请求体。provider-agnostic 诊断原料。
5. **session 级 vs call 级边界**：call 级 = 一次最小核降级；session 级 = 降级后
   仍 4xx → fail session，台账留 resume 指引（failed→resume 是标准恢复路径，
   session JSONL + raw log 都在盘上）。

## 分类矩阵

| 类别 | 判定 | 处置 |
|---|---|---|
| 账户级 account | HTTP 402，或厂商账户码（如 GLM `1113` 余额不足）藏在 429/4xx 信封里 | **不重试、不降级、不 fallback 同 provider** —— 直接 fail-fast，dump/session_end/台账带 `remedy`（充值或换 provider） |
| 确定性 deterministic | HTTP 4xx（除 429） | 剥到最小核重发**一次** → 仍 4xx → fail-fast + 请求体落盘 |
| 瞬时 transient | HTTP 429 / 5xx | retry × MaxRetries(默认3) 指数 backoff，尊重 Retry-After |
| 网络 network | timeout/refused/reset/DNS | retry × MaxRetries 指数 backoff → 仍失败即 fail |
| 未知 unknown | 其余（200 正常返回不进此路径） | 原样上抛 |

> account 行只在该表下行排序上优先：**429 里带账户码 = 账户级，不是限流**（否则
> resumer 会一直重试一个死账户 —— GLM 1113 的实战证据见
> `devdoc/night-report-20260910.md`）。判定复用 `internal/deepseek/error_account.go`
> 的**同一个** `isAccountLevelStatus` 谓词，崩溃 dump 侧不复制厂商码知识。

实现：`internal/deepseek/error_matrix.go`（`classifyHTTPError` /
`isDeterministicHTTPError` / `retryableStatus` / `isNetworkError`）。

## 降级路径（minimal core）

`internal/deepseek/minimal_core.go`：

- `MinimalCore(req)`：**白名单式**重建 `&ClaudeRequest{Model, MaxTokens,
  Messages, Stream}`——未来新增 optional 字段**构造性**地不会泄进最小核。
- `runMinimalCoreDegrade`：第一次 send 命中 4xx → 最小核重发一次 → 仍 4xx →
  `dumpFailedRequest`（时间戳 dump.json：original_request + http_status +
  api_error_body）→ fail-fast。
- 原请求永不 mutate；dump 保存的是**原始未剥离**请求（可重放/可 resume）。

### 流式路径特例（为什么 stream 4xx 不在 client 内降级）

agent 层拥有 stream→非流式 fallback（`loop.go chatOnceAttempt`：
ClaudeChatStream 失败 → ClaudeChat 重发）。若 client 对 streaming 4xx 也做
client 内重发，会双发并抢先消耗服务端计数、破坏 agent 层 fallback 契约。
所以：**streaming 4xx → fail-fast + 落盘 + 上抛**，恢复由 agent 层 fallback
完成；fallback 的非流式请求若仍 4xx，再走 `ClaudeChat` 内的最小核降级——
降级重发全程只发生一次。

## 关键修复：sentinel error（历史 bug）

`retry.go` 的 `sentinelAPIError{status, body}`：操作闭包对非 200 不再返回
拼好的 `"API error (status 500): ..."` 文本（该文本不含 timeout/refused
关键词 → `isNetworkError` 判 false → **5xx/429 从未被重试过**——2026-08-31
最小复现确认 pre-existing），改为结构化哨兵 error；retryDo 用
`classifyHTTPError` 按 status 分类。修复后 429/5xx 指数 backoff 重试真正生效
（`TestTransientRetry_429And5xx` 红→绿）。

## 台账 resume 指引

`internal/agent/taskregister.go`：failed 台账行 summary 追加
`| FAILED — recoverable: gopi resume <session-id> replays session context;
check the session raw log for the fatal request/response`。

## 测试矩阵（error_matrix_test.go）

- `TestClassifyHTTPError_Matrix`：16 行全分类断言（含 1214/1210 形态的 400
  只是 deterministic——无 vendor 特判）。
- `TestMinimalCore_StripsAllOptional`：wire JSON 无
  thinking/reasoning_effort/tools/system/cache_control/temperature/top_p；
  原请求不被 mutate。
- `TestDegradePath_DeepSeek400EmptyThinking / _GLM1214 / _Anthropic400`：
  三个历史案各走一遍 → 全 rescued by 最小核，恰好 2 次发送。
- `TestDegradePath_Exhausted_DumpAndFail`：仍 4xx → 恰好 2 发 + dump 文件
  （original_request 完整 + http_status + api_error_body）。
- `TestTransientRetry_429And5xx`：429/500 各重试一次后成功。
- `TestNetworkError_RetriedThenFailedWithDump`：网络断 retry×3 后 fail。
- `TestNon200RawLog_CarriesStatusAndBody`：raw log 含 HTTP status + 错误体。
- `TestStreamPath4xx_NoClientResend`：streaming 4xx 恰好 1 发（agent fallback
  拥有恢复）。

环境变量：`GOPI_ERROR_DUMP_DIR`（默认 `<tmp>/gopi-dumps`）。
