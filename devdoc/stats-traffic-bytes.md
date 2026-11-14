<!-- doc-class: normative -->
# stats 维度扩展第二批：--bytes 体级流量视图

## 背景

gopi stats 首版（8f0d419）按 FINAL-USAGE-PROPOSAL.md 的 bits-by-bits 原则覆盖了
rounds/token min-max-avg/cache/completion/tools/trim cliffs；定稿文档第 89-90 行明确
"requests/response 体级/tool 参数等后续加"。本批次落地 **requests/response 体级** 维度：
`gopi stats <id> --bytes`。

## 新增视图

`--bytes` = per-round body traffic（raw source only）：

```
  RND        REQ     MSGS       RESP     TEXT    THINK KIND
    1    36.0 KB        1     9.1 KB       65      211 stream
    2    44.4 KB        3    16.2 KB       64      340 stream
    3    45.3 KB        5    25.3 KB      486      263 stream
    4    36.5 KB        3    23.9 KB      489      156 stream
    5    36.6 KB        3    21.6 KB      536       63 stream
  TOT   198.9 KB       15    96.2 KB    1,640    1,033 1 kinds (stream)
```

列定义（usage 已在 stats 主文档，此处不复述 token 语义）：
- **REQ** = 请求体 wire 字节（`len(RequestRaw)`，发送时的确切 JSON 字节——请求大小，
  不是 token 数）。上下文增长的另一把尺子：round 2 体 44.4 KB / msgs 3 = tool 结果回填。
- **MSGS** = 该请求体携带的 user/tool 消息条数（`len(Request.Messages)`）——token 数背后
  的上下文结构信号（工具调用轮会 1→3→5 增长）。
- **RESP** = 响应侧 wire 字节（chunks/body 累计，含 marker 开销；复用既有的
  `RoundResponse.Bytes`，与 `raw --rounds` 的 BYTES 同源同语义）。
- **TEXT / THINK** = 响应重组后的 text/thinking **字符数**（SSE delta 累计），输出量代理，
  **不是 token 数**。字符≠字节，故不用 humanBytes 而用千分位原样显示（humanChars）。
- **KIND** = 响应侧形状 stream / response / http-error / none。
- **TOT** = 末行汇总：所有 round 的请求/响应字节、字符、消息数与响应种类数。probe 与
  no-usage rounds 一并计入——体流量发生与否与 provider 是否报 usage 无关。

## 实现要点

- 数据在 **BuildStatsFromRounds 捕获、释放前复制**：RequestRaw/Request 在 digest 后即
  被置 nil（多百 MB raw log 内存纪律），故新字段在释放前的同一循环里拷入 RoundStat。
- 新字段全带 `omitempty`，jsonl 源 round 全为 0 → `stats-json.json` golden 只做纯增量。
- 聚合在**渲染时**由 RoundStats 现算（buildTraffic），不在 SessionStats 上加冗余字段——
  原结构零改动，现有字段/JSON 键不动，符合"不推翻现有结构只加维度"。
- 语义决策：
  - 总量/计数 fold **含 probe 与 no-usage rounds**——体流量发生与否与 provider 是否报
    usage 无关，行不能从流量和里静默消失（与 token 聚合含 probe 的政策不同，已在注释标明）。
  - `RespKind` 空串归一为 "none"（请求后无响应侧的 round）。
  - jsonl 源无 wire body → `--bytes` 打一行 "(no raw log — request/response body sizes
    unavailable)"，绝不打印空表。
  - `--all --bytes` 被 renderStats 拒绝报错（aggregate 无 per-round rows），不静默。
- 输出风格与既有 --rounds/--tools 一致：headerSep + 标题 + 表格，宽度常量列，humanBytes
  渲染字节。表头注明 numbering basis（raw ordinal 1-based）。

## Golden 护航

- 新快照 `tests/fixtures/log-stats-raw/golden/stats-traffic.txt`（gopilog 层 + CLI 层同
  一文件断言，CLI 层缺失即失败——沿用 log-content-golden 模式）。
- `stats-json.json` 重录：每 round 增量 6 键（req_bytes/messages/resp_bytes/resp_kind/
  text_chars/thinking_chars），diff 审查为纯增量，数值与真实 fixture 逐一核对
  （round 1: req 36893 / msgs 1 / resp 9330 —— 与 raw log 原字节一致）。
- overview/rounds/tools/jsonl 快照**零意外变化**（rounds/tools/overview 未动）。

## 测试

- gopilog：TestGoldenStatsTraffic（新 golden）
- cmd/agent：TestStatsCLIOverFixture 增 --bytes 段（同 golden）、
  TestStatsBytesFallbackSaysNoRawLog（jsonl 源提示）、TestStatsBytesRejectedOnAll
  （--all --bytes 拒绝）、parse 接受 --bytes。
- go build ./... && go vet ./... 过；go test ./internal/gopilog/ ./cmd/agent/ 全绿。

## 未做 / 后续

- tool 参数规模统计（tool_use input 大小）：定稿同句列出但本批未做——tool 参数量可从
  REQUEST body 的 tool_use 块或 input_json_delta 累计，属第三批候选（需要先想清楚
  "参数规模"的度量单位是字节还是块数）。
- HTTP-ERROR / probe rounds 在此 fixture 无真实样本，KIND 列只测到 stream（none 路径靠
  代码保证，无伪造样本——沿用 fixture 缺口政策）。
