<!-- doc-class: normative -->
# Thinking-Trim 实测验证报告 — 最近 1-2 个 raw log

> 日期: 2026-08-26 | 验证对象: gopi P2 thinking-trim（wire 剥离 assistant reasoning）
> 数据源: `~/.gopi/raw/gopi-raw-*.log`（逐请求体字节级） + `~/.gopi/sessions/*.jsonl`（原始 thinking 真值）

## 结论

**功能真实工作**。最近两个 raw log（上一个已完成任务 + 当前会话）中，wire 上发送的
thinking 全部是 1 字符占位块（`gopi-empty-thinking` 签名）；若不做剥离（keep 臂），
同一批请求需累计重放 75K-296K chars 的历史 thinking。实测剥离率 **99.8%-99.94%**，
对应估计 token 节省 **≈18.8K - 74K tokens/会话**（chars/4 启发式）。

## 分析对象

| 会话 | raw log | 时段 | API 请求数 | 状态 |
|---|---|---|---|---|
| ceeb7248 | gopi-raw-20260826-215027.log | 21:50:27→21:53:19 | 18 | 上一个已完成任务 |
| 11b3012b | gopi-raw-20260826-215428.log | 21:54:28→(进行中) | 19（分析时） | 当前会话（本验证任务） |

映射方式：session_start 事件中的 `rawLogPath` 字段精确配对（非时间戳猜测）。

## 机制（代码确认）

- `internal/agent/context.go:366` `Context.WireMessages(keepThinking)` —— 默认
  （`GOPI_KEEP_THINKING` 未设）把所有 assistant 消息的 `ReasoningContent` /
  `ThinkingSignature` 置空；内存上下文与 session JSONL **保留**原始 thinking。
- `internal/deepseek/claude_client.go:583` —— 剥离后若该 assistant 消息带 tool_use
  且当前请求 thinking 开启，注入 1 字符占位块 `{"thinking":" ","signature":"gopi-empty-thinking"}`
  （API 硬性要求，实测去掉会 400）。
- 设计依据：LLM 每轮独立，thinking 不跨轮——重放历史 reasoning 对模型无益，仅烧 token。

## Wire 实测（raw log 逐请求体解析，18+19 请求全部解析成功）

| 指标 | ceeb7248（18 req） | 11b3012b（19 req） |
|---|---|---|
| wire 上 thinking 块总数 | 152 | 171 |
| wire 上 thinking chars 总量 | 152（=152×1-char 占位） | 190（≈171 占位+杂项） |
| 非占位 thinking 块（带真实文本） | **0** | **0** |
| wire 上累计消息数 | 324 | 289 |

wire 上不存在任何真实 thinking 文本——剥离彻底，仅剩合规占位符。

## 节省计算（原始 thinking 来自 session JSONL，逐请求对齐）

keep 臂等价量 = 每个请求累计重放其之前所有轮次的原始 thinking（LLM 每轮产生的
thinking 文本在 JSONL `assistant` 事件 `content[].thinking` 中逐字节保留）：

| 指标 | ceeb7248 | 11b3012b |
|---|---|---|
| 原始 thinking 总产生量 | 6,511 chars | 36,299+ chars（会话仍进行中） |
| keep 臂累计重放量 | 75,493 chars | 296,075 chars |
| 剥离后实际发送 | 152 chars | 190 chars |
| **移除 chars** | **75,341** | **295,885** |
| **移除比例** | **99.80%** | **99.94%** |
| **估计 token 节省（chars/4）** | **≈18,835** | **≈73,971** |

说明：keep 臂累计量远大于单轮产生量，因为 thinking 随轮次累积——第 N 个请求会重放
前 N-1 轮的全部 thinking（这正是 A/B 报告实测 B 臂 2.30M chars 的机制）。节省率随会话
变长单调上升（两个会话均 >99.7%）。

## API 计费层面的观察（request 事件 usage）

| 会话 | prompt_tokens | cache_hit | cache_miss |
|---|---|---|---|
| ceeb7248 | 314,446 | 296,064 (94.2%) | 18,382 |
| 11b3012b | 386,119 | 336,256 (87.1%) | 49,863 |

剥离的 thinking 属于**前缀缓存的稳定部分**（auto-cache 内容寻址），实际计费影响低于
名义 token 数：DeepSeek cache-hit 0.025 / cache-miss 0.25 每百万 token（memory #877 实测价）。
即使如此，thinking 剥离把每请求 prompt 里唯一会无限增长的部分消除，是缓存友好度的
前提（A/B 报告：B 臂每请求平均 45 chars vs A 臂 45K chars）。

## 复现方法

```bash
# 1. 确认 wire 上只有占位符
awk '$0=="→ REQUEST"{n++} END{print n}' ~/.gopi/raw/gopi-raw-*.log | tail -2
grep -c 'gopi-empty-thinking' ~/.gopi/raw/gopi-raw-<最新>.log

# 2. 原始 thinking（真值）在 session JSONL
python3 - << 'EOF'
import json
sess=[json.loads(l) for l in open('~/.gopi/sessions/<id>.jsonl')]
for e in sess:
    if e.get('type')=='assistant':
        for b in e['message']['content']:
            if b.get('type')=='thinking': print(len(b['thinking']))
EOF
```

## 局限

- token 节省为 chars/4 启发式估计，非 API 精确 token 数（thinking 多为英文/代码，
  chars/4 偏保守低估）。
- 本验证为单侧（trim 臂）；keep 臂对照数据见 reports/archive/2026-08/THINKING-TRIM-REPORT.md §2-3
  （A/B：2.30M chars keep vs 4.2K chars trim，−99.8%，≈573K tokens，51 vs 93 请求）。
- 11b3012b 为当前活动会话，数字随会话继续增长（趋势不变：>99.9%）。
