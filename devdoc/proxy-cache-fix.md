<!-- doc-class: historical -->
# Task Report: GLM cached_tokens 透传修复（gopi proxy 缓存统计丢失）

## Completed
- `internal/proxy/types.go`: `OpenAIUsage` 新增嵌套 `PromptTokensDetails *OpenAIPromptTokensDetails` 结构（含 `cached_tokens`），解析 GLM 的 `usage.prompt_tokens_details.cached_tokens` 字段
- `internal/proxy/stream.go`: `AnthropicUsage` 新增 `CacheReadInputTokens` 字段；`Finish()` 将 `CachedTokens → CacheReadInputTokens` 透传
- `internal/proxy/response.go`: 非流式用法转换同样透传 `CachedTokens → CacheReadInputTokens`
- 测试：
  - `stream_test.go` `TestStream_UsagePassthrough`: 上游 chunk usage 含 `prompt_tokens_details.cached_tokens:20` → message_delta usage `cache_read_input_tokens:20`（值一致）
  - `response_test.go` 新增 `TestNonStream_CacheReadInputTokens`: 非流式 usage 透传验证
  - 回归：`TestStream_FinishWithoutUsage` 确认 usage=nil 时行为不变（cache 字段有 `omitempty`）

## Not Done
- 无。流式与非流式两条路径均已修复并验证

## Verification
- Build: `go build ./...` OK
- Vet: `go vet ./internal/proxy/` OK
- Tests: `go test ./...` 全绿（9 packages ok）

## E2E 实测（真实 GLM-5.3-Flash，通过 openai 上游代理）
系统 prompt ~4900 字符，max_tokens=1024，相同请求发两次：

**非流式**（JSON 响应）：
```
REQUEST 1 usage: {'input_tokens': 926, 'output_tokens': 32, 'cache_read_input_tokens': 896}
REQUEST 2 usage: {'input_tokens': 926, 'output_tokens': 32, 'cache_read_input_tokens': 896}
```

**流式**（message_delta SSE）：
```
REQUEST 1 "usage":{"input_tokens":926,"output_tokens":54,"cache_read_input_tokens":896}
REQUEST 2 "usage":{"input_tokens":926,"output_tokens":33,"cache_read_input_tokens":896}
```

cache_read_input_tokens = 896 / 926 input = **96.8% 前缀缓存命中**，透传正确。

## Commit
- `e676d90` fix(proxy): pass through GLM cached_tokens as cache_read_input_tokens

## 文件清单
- `internal/proxy/types.go`（+8 行）
- `internal/proxy/stream.go`（+6 行）
- `internal/proxy/response.go`（+8 行）
- `internal/proxy/stream_test.go`（修改）
- `internal/proxy/response_test.go`（新增测试）
- `devdoc/proxy-cache-fix.md`（本报告）
