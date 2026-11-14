<!-- doc-class: historical -->
# 状态行：ctx 段 → cache 命中率显示 —— 侦察结论 + 交接设计（未实施）

任务 01a0e84b（chat 会话）。本文件是**侦察与设计**，不是已落地的改动：本轮在预算内判定
该 epic 超出单会话，已按 TOO_BIG 分解下派。子任务请以此为起点，不必重做侦察。

## 1. 操作者的诉求（原话语义）

Linux `gopi chat` 信息条的 `ctx 1M/1M≡`（实测见 §3）**零信息**：分子/分母是 gopi 自己的
常量预算，分母虽已有 `≡` 限定符仍不可读。要求替换为 **DeepSeek prompt-cache 命中显示**：
命中率百分比 + 绝对 token 数，**颜色随命中率变化**。

## 2. 前置结论（已完成，可复用）

数据链路**已经通**，无需新增解析：

- chat 走 `/v1/messages`（`internal/deepseek/claude_client.go`）：
  `ClaudeUsage.CacheReadInputTokens` = hit，`InputTokens + CacheCreationInputTokens` = miss；
  `AddClaudeUsage` 累加进 `CostTracker.TotalCacheHitTokens / TotalCacheMissTokens`
  （`internal/deepseek/claude_client.go:303-306`）。
- 状态行已把这**真实计数**喂进 `statusOps`：`cmd/agent/cli_chat.go:2149-2150`
  （`ops.cacheRead = ag.Cost.TotalCacheHitTokens`，`ops.cacheWrite = ...MissTokens`）。
- 本轮真实会话 `75e835ef` 的 usage：turn1 hit=896 miss=414（68.4%），turn2 hit=2048 miss=582
  （77.9%）——**每轮都变**，足以驱动命中率显示。

⇒ 命中率 = cacheRead/(cacheRead+cacheWrite)，是**传输层直接可测**的量，不是派生估计，
符合操作者的「只显示确定可测的量」判据（memory #5912）。

## 3. 基线屏幕真值（改前，真 pty 100x26）

harness：`/home/user/.gopi/sessions/01a0e84b-3a17-7d59-b9be-545a6420a3f7/ptychat.py`
（python `pty.fork` 驱动真实 `./bin/gopi chat`；**写文本与写 `\r` 必须分两次**，
否则被当成 paste 不提交）。

row025（100 格）：

```
deepseek-flash ·  iter 2  ·  tools off                 ·  ctx 1k/1M≡ ·↑2k↓582          24s · ready
```

attrs：`0:14` gold bold(model) `17:8`(iter) `28:26`(tool) `57:30` gold bold(metrics)
`70..87` = `↑2k↓582` **未着色** `87:11` bold+italic+gold(live)。

要点：**metrics 组整体一个样式**——`↑2k↓582` 与 `ctx 1k/1M≡` 同色，且段内没有按值选色的机制。
所以「颜色随命中率变化」在现有渲染路径上**无处可挂**（见 §5）。

## 4. 几何：改动必须动 100 格预算的算术（这是本轮判定 TOO_BIG 的根因）

全部在 `cmd/agent/chat_stream.go`（`internal/lineedit` 提供 cell 算术与 status 行组装）：

- 行 = model 14 + iter 7 + tool 16 + metrics 组 27 + 3 个列分隔(4 格) + gap 2 = **100**，
  `statusPinSlackCells == 0`（槽位为 0，被测试钉死：`chat_status_cache_bound_test.go:504`）。
- metrics 组 `statusMetricsCol = statusCtxCol(17) + statusCacheCol(8) + 2*statusMetricsSepCells`。
- ctx 子单元 `statusCtxCol = label(statusCtxLabel "ctx") 3 + gap 1 + pair 13`；
  pair 13 = `2*statusCtxPairSideCellsWidest(6) + statusCtxPairSepCells(1)`。
- cache 子单元 8 格，内容 `↑<read>↓<write>`（`statusCacheField`），格式化器**按构造有界**
  （`formatCountBounded`，`cmd/agent/chat_status_cache_bound.go`，一次 1.66M token 会话
  曾把 `↓` 半截挤掉——`statusCtxPairSideCellsPinned=6` 的代价就在这里）。
- 编译期守卫：`const _ = uint(statusLayoutBudgetCols - statusColumnsAtCols)`——**一格超支即编译失败**。
  任何重分配都要让这条等式重新成立。

被钉死的测试（改内容/宽度必须同步，约 10 个文件）：
`chat_status_metrics_test.go`（40 处引用）、`chat_status_ctx_label_test.go`、
`chat_status_ctx_provenance_test.go`、`chat_status_degradation_test.go`、
`chat_status_ladder_test.go`、`chat_status_cache_bound_test.go`、`chat_status_width_test.go`、
`status_row_render_test.go`、`chat_status_row_unit_rule_test.go`、
`chat_status_symbols_legend_test.go`；另有 `cmd/agent/cli_chat.go` 帮助文本与 legend/符号可见性门。

## 5. 配色机制：需要新增一条「按值选色」通道

现状：`internal/lineedit/palette.go:639 styledFields(label, pal)` 把 label 按 `StatusSep`
拆分，逐段套 `pal.StatusLabel` 或 `ToolRow`——**样式由字段身份决定，不由字段的值决定**。
组内分隔符是 `StatusSepGroup`（`·`），组内不换色。

正确做法（沿用本项目既有原则，memory #5707「渲染层从不学格式，决策来自产出方」）：
**由产出方给出强调等级，渲染层只查表**——即像 `Status{Thinking bool}` 那样，在 Status/字段上
带一个由命中率算出的等级（如 `hitRateClass`），`lineedit` 用一个**命名颜色常量表**把它映到样式；
**禁止在渲染层匹配字符串**。单色（`MonoPalette` / `NO_COLOR`）路径必须仍可读——
`TestPaletteCarriesNoInformationAlone` 要求信息不能只靠颜色。

## 6. 建议的验收（Rule 31：必须真跑改动面）

1. 真 armed `gopi chat`（pty 驱动，装 §3 的 harness）+ 至少两轮对话，证明命中率**真的变化**；
   抓 row025 文本与 attrs（改前/改后对照）。
2. 宽度对账：全宽 `BudgetWidth`(100) + 各降级宽度，无字段被挤掉/截断（现有 ladder 测试的形态）。
3. `go test ./internal/agent/ ./cmd/agent/ ./internal/lineedit/ ...` 绿。
4. 报告写 `Ran:` / `Result:`（屏幕真值）/ 诚实的 `Not Done:`。

## 7. 与 T7/T5 的关系

T7 的缺陷（`ctx 1M/1M≡` 无信息）**由本改动直接解决**：该段被替换成带分母可解释的命中率
（概率本身即「命中/(命中+未命中)」）。T5（`↑1662k↓1662k` 溢出 7 格）已在 01a0e2e9 用有界
格式化器修掉；本次**不得回退**该有界性——新的命中率字段同样必须按构造有界（宽度来自
named 常量，不来自样本字符串）。

## 8. 本轮已落地的一半（commit 4421a575）与它的确切缺口

**已落地**（`internal/lineedit`，全测试绿，`go test ./internal/lineedit/` ok）：命名色阶
`StatusGood/StatusFair/StatusBad` 进入**全部 5 张 palette 表**（Mono / Basic 8-16 /
Palette256 / HermesDark / HermesLight）并加入 `fields()`（现有 palette 覆盖门与
「信息不只靠颜色」门都能看见它们）；`Status.LabelEmphasis` + `Status.Emphasis`；
`Status.PrepareLabel(pal)`。

**确切缺口（子任务必须知道）**：`PrepareLabel` 目前只被 **`Status.Text()`（未对齐路径）**
调用。真实 `gopi chat` 走的是**对齐路径**：`Status.Line(cols)` → `statusLine` →
`statusLineColumns` 用 `s.Columns` 逐列拼文本，随后 `styledStatusLine` 用
`styledFields(label, pal)` 按**字段身份**整列套色。因此：

- 在**对齐路径**上，`LabelEmphasis` / `Emphasis` **完全无效**（静默不生效）；
- 原因：命中率骑在 metrics 列**内部**，而列是被整列套色的；要在列内强调一个 span，
  必须让**列自己**携带强调（给 `StatusColumn` 加 `Emphasis` 字段，由
  `statusLineColumns`/`styledStatusLine` 在列内定位 span），否则就会出现
  「先套整列色、再在内部插一层 SGR」的嵌套复位问题（内层 `\e[0m` 会提前结束整列色，
  该列剩余字符掉色）——这正是**不能**用 `ReplaceAll` 事后补救的原因。
- 所以子任务的最小正确形是：**强调随列走**（`StatusColumn.Emphasis`），
  `PrepareLabel` 则退化为未对齐路径的同义实现；两条路径共用「span 文本来自产出方」
  的一条规则。

**生产者侧阈值**（命中率 → 等级）必须在 `cmd/agent` 命名为常量并按测量判定；
渲染层**只查表**，不得匹配字符串（记忆 #5707）。

## 9. 【修正 §4】命中率**不必动几何** —— 最小正确形（本会话末测得的算术）

§4 说「必须重算 100 格」是**过强**的结论。实测常量：

- `statusCtxCol` = 17 = label(3) + gap(1) + pair(13)；`statusCacheCol` = 8；
  `statusMetricsCol` = 17 + 1×2 + 8 = 27。
- 有界格式化器一侧上限 `statusCountSideTextWidest` = 3（"999" 或 "12k"），
  `formatCountBounded` 已被测试走遍所有档位与 int 极值证明有界。

**`ctx` 子单元的 17 格足以直接承载命中率**：

```
hit 68% 2k/1k      = label 3 + gap 1 + pct 4 + gap 1 + pair 7 = 16 ≤ 17
```

其中 pct 上限正是既有 `statusCtxPctCellsWidest = 4`（"100%"），
pair 用既有有界格式化器（3+1+3=7）。**⇒ 100 格等式、`statusPinSlackCells == 0`、
编译期守卫、以及几何类被钉死的测试全部不动**；只有**内容级**断言
（`ctx 92.1k/200.0k` 这类形式断言）需要改。

**降级形（宽度不足）**：丢掉绝对对 → `hit 68%`（7 格）；再窄 → `68%`（4 格）。
**不变量**：任何形式的宽度必须来自命名常量（`statusCtxPctCellsWidest` /
`statusCountSideTextWidest`），不得取自样本字符串（T5 的教训）。

**duplication 的取舍**：cache 子单元（8 格）已写着 hit/miss 的**绝对量**（`↑2k↓582`）。
若命中率对里的绝对量与之重复，可整段省掉 → `hit 68%` 只花 8 格；剩余格留给
（未来）绝对量或留空。**这一步由下派子任务按屏幕可读性决定**，不要凭想象定形。

**仍未解决的**：让颜色真的随命中率上屏（对齐路径的列内强调，见 §8 缺口）——
它与本节的文本改动**可以分开落地**：先上文本（零几何风险），再上颜色。
