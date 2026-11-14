<!-- doc-class: normative -->
# max_tokens 收敛：384K → 16K（A4 实证）

> 2026-08-22，deepseek-v4-flash @ api.deepseek.com/anthropic。三方对比 #4 建议
> "maxTokens 384K → 8-16K 收敛"，本 devdoc 记录真实 API 验证过程与结论。

## 结论

**DefaultClaudeMaxTokens: 384000 → 16384**。8192 实测够用（607 行单次 write 零截断）
但余量仅 ~8%，千行级草稿会截断；16384 保持 2 倍余量，同时把每响应上限降低 23 倍。

## 实证 1：端点硬强制 max_tokens（8192 验证）

直接 API 请求 `max_tokens: 8192` + "输出尽可能多的数字"：

```
stop_reason: max_tokens
output_tokens: 8192          ← 恰好在 8192 处截断
text: 14212 chars            ← 约 1.73 chars/token（密集数字流）
```

→ 端点对 max_tokens 是**硬上限**，超出的输出被截断（stop_reason=max_tokens → gopi
的 "length" 分支 → 重发循环——正是 maxtokens 历史 bug 的失败模式）。

## 实证 2：长 write 不截断（8192 下的 41 轮真实任务）

任务：单次 write 调用写 ~350-600 行 memdb 库 + 编译验证。GOPI_MAX_TOKENS=8192、
无全局超时。结果（session c7833cb1，41 轮，exit=0）：

- **0 次截断警告**；5 次整文件 write 全部落地；最终产物 547 行 memdb.go +
  76 行测试，编译/测试通过。
- 最大 write 草稿：18,685 转义字符 ≈ 5,968 completion tokens（≈3.1 字符/token —
  转义膨胀远低于担心的 2-4x，Go 代码引号密度低）。
- 最重单轮（iter=26）：6,147 字符 thinking + 5,968 completion ≈ **7,500 / 8,192
  tokens（91.5% 利用率）**，未截断。
- 每轮等待中位 17.6s；cache 95.6% 命中。

## 为什么不是 8192

- 8192 下最重单轮已达 91.5% 利用率，余量 ~700 tokens。8192 内估算：
  1,000 行草稿 ≈ 30K 转义字符 ≈ 9.6K tokens → **8192 会截断**（devdoc/
  maxtokens-truncation.md 反复修复的正是这个场景：4096 → 32768 → 384K）。
- 16384 = 实测最重单轮（7,500 tokens）的 2 倍余量，模型生成完即停，上限提高
  无实际成本（本轮 41 轮 36,619 completion tokens，全部低于 16,384）。
- 需要更紧预算时 GOPI_MAX_TOKENS 仍可覆盖（规则 2：单点 env 读取）。

## 未做

- **write 分块写**（超长文件自动分多次 write）：仍是 future work（与
  maxtokens-truncation.md 的 Not Done 一致）——16384 已让 ~1,000 行单次写入可行。
- 千行级草稿的实测（生成速率 ~16 chars/s，千行 ≈ 30 分钟一轮，成本高；
  用 607 行实测 + 转义膨胀率外推）。

## Commit

`feat(deepseek): A4 — converge max_tokens default 384K → 16384 (measured)`
