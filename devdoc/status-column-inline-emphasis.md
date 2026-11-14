<!-- doc-class: historical -->
# 状态行的"列内按值强调"通道 —— StatusColumn.Emphasis（实施记录）

任务 **01a0e8c8**（worker）。本文件记录**已落地**的部分与**明确未做**的部分。上位侦察/设计文档是
`devdoc/chat-status-cache-hitrate.md`（母任务 01a0e84b 的交接，含几何算术与既有测试清单）。

## 1. 缺口是什么（一句话）

`internal/lineedit` 的 `styledFields` 把 label 按 `StatusSep` 拆段，**按字段身份**（label / 工具活动）
整段套色 —— 段内饰品里没有"按值选色"的挂点。所以一个**测量值**（prompt cache 命中率）即便算出了
good/fair/bad 等级，也**无处可挂**：这正是"颜色随值变化"在改前完全不生效的机制（R15 静默不生效）。

## 2. 落地了什么

### 2.1 渲染层：`StatusColumn` 新增列内强调通道（`internal/lineedit/answer.go`）

```go
type StatusColumn struct {
    Text  string
    Width int
    EmphasisSpan string  // 本列文本中"是被测量出来的值"的那一段
    Emphasis     Style   // 该段穿的样式（由产出方给出，见 §2.2）
}
```

`styledStatusLine` 在**对齐路径**改走 `statusAlignedRow`：它用产出方自己的列清单重新组装一次行
（列边界只存在于那份清单里 —— 事后按 `StatusColumnSep` 反切等于"渲染层匹配字符串"，本项目明令禁止），
并且**先断言重组出的行与 fit 决定的 `plain` 逐格相同**，不同则拒绝走该分支（宁可退回旧路径，也不画
一条谁都没量过的行）。

强调的施加方式是**重述（re-statement）**，而不是把序列拼进已着色字段：

```
ESC[1mESC[38;2;218;165;32m  ← 字段样式开
  ESC[38;2;255;167;38m hit 69% ESC[0m   ← 强调段自带 Wrap（结尾是 reset）
  ESC[1mESC[38;2;218;165;32m           ← 字段样式"重述"
   ·↑896↓402
ESC[0m                                ← 字段样式关
```

**为什么必须重述**：段自身的 `Wrap` 以 reset 收尾，reset 会把**外层字段色一并结束**——若不在段后重开
字段样式，该列段后的每一个字符都会掉到终端默认色。这就是嵌套复位缺陷（nested-reset defect），
`TestStyledStatusLine_ColumnKeepsItsColourAfterTheSpan` 专门钉它。

**未对齐路径（窄终端）同样生效**：`styledFieldsEmphasis` 逐段套色后，对**恰好包含该段**的那一段走
同一个 `wrapStatusField`（单一实现，两条路径不可能对"字段长什么样"给出两个答案）。
`Status.LabelEmphasis`/`Emphasis` 作为该路径的产出方事实存在，改前只被未对齐的 `Text()` 读、被
`PrepareLabel` 用却**从不真正上屏**（R15 违规），现在由 `styledStatusLine` 真正消费。

**单色/零样式路径逐字节不变**：`MonoPalette` 在 `styledStatusLine` 入口直接返回 `statusLine`；
`Style{}` 零值经 `wrapStatusField` 也不产生任何字节。`TestStyledStatusLine_MonochromeIsByteIdenticalWithAColumnEmphasis`
对 7 种宽度断言 `styled == statusLine`。

### 2.2 产出方：命名阈值 + 色阶解析（`cmd/agent/chat_status_hitrate_emphasis.go`）

```go
statusHitRateGoodFloorPct = 80
statusHitRateFairFloorPct = 50
```

- 阈值是**产出方自己的判断**（这笔读数的经济学：cache 命中 vs 写缓存，后者在传输层贵约 30 倍），
  不来自任何价格表/契约/API 文档；渲染层只做**查表**（`StatusGood/StatusFair/StatusBad`），
  永远不知道被衡量的是什么（memory #5707 的分工）。
- `statusHitRateSpan(ops, pal, columnText)`：**唯一的列级入口**。它把"这一段是被测量出来的值"和
  "这个值落在哪一档"一起给出，并且**在该列自己的文本里找到这段**——找不到就返回空（诚实 no-op，
  而不是造一个没人能看见的强调）。**本文件不自行拼读数字**：读数由 `statusCacheHitRate` 产出、
  全形由 `statusCacheHitRateField` 决定，这里只回读。
- 三档切换用 `switch` 且**穷举 0..100 全范围**测试（`TestStatusHitRateEmphasis_TiersCoverTheWholeRange`），
  不是只测边界点：档位之间若有洞/重叠，屏幕上是看不出来的（一次只动一个百分点）。

### 2.3 接线（`cmd/agent/chat_stream.go` / `cli_chat.go`）

- `chatStatusSource` 新增 `pal` 字段 + `setPalette`：**色表由 REPL 解析一次**（`chatPal`）后交过来，
  绝不在 source 内二次解析（否则能力降级提示会向正在读它的终端打印两次）。零值是 `MonoPalette`，
  所以直接构造 source 的测试天然不穿档（通道默认惰性）。
- `status()` 里 `full[statusColMetrics]` 组装完成后，用**这一列自己的文本**求 span/tier 并写回列。
- `pal` 与 `ops` **同锁读取**：档位由"这张表 + 这次读数"共同决定，跨重绘边界读一对会让上一次测量穿
  下一次的表。

## 3. 验证（真机、逐格）

真 pty 100x26 驱动 `bin/gopi chat`（harness：本会话目录 `ptychat.py`，写文本与写 `\r` 分两次）。

row025（真屏幕，100 格）与 attr 区间：

```
deepseek-flash ·  iter 1  ·  tools off                 ·  hit 69% ·↑896↓402            4s · ready
0:14  fg=rgb:daa520 bold      model
14:4  default                sep
18:7  fg=rgb:daa520 bold      iter
25:4  default
29:25 fg=rgb:daa520 bold      tool
54:4  default
58:7  fg=rgb:ffa726 bold      "hit 69%"  ← 命中率：fair 档（琥珀），与整列的金色 ffa726≠daa520
65:20 fg=rgb:daa520 bold      " ·↑896↓402"  ← 同一列、段之后的字符：**仍是字段色**（未被 reset 打掉）
85:2  default
87:10 fg=rgb:ffd700 bold italic  live
```

这一条同时证明三件事：强调段的样式**与相邻字符不同**、该列**剩余字符仍有色**、行文本**没有变化**
（`stripSGR` 后与 `statusLine` 逐字节相同）。

`NO_COLOR=1` 同尺寸真跑：row025 **版式逐格相同**（同样 100 格、同样分段），attrs 全行 `default`
（`[{col:0,cells:100,style:'default'}]`，零 SGR）。**注意**：两帧的**数字本身就不同**
（mono `↑896↓401` / 彩色 `↑896↓402`）—— 那是实时读数在变，不是强调的差异；
本对照证明的是"强调不改变文本版式"与"关色即零转义"，不是"两帧逐字相同"。

## 4. 诚实未做 / 边界

1. **档位随值变化只在 UT 里证明，真机只抓到 fair 档。** 真机抓到 69%（fair）。要抓 good 档需要把
   会话累积命中率推到 ≥80%，而 8 轮真跑（下条）在预算内没跑完；good/bad 两档的"上屏"目前由
   `TestStatusHitRateEmphasis_TiersCoverTheWholeRange`（0..100 穷举，含 80/100 与 0/49）
   与列级 span 测试覆盖，不是伪证，但**没有真机截图**。工具链（`ptychat.py` + 抓 attr runs 的脚本）
   已在会话目录，重抓只差一次长会话。
2. 列内强调目前只有 **metrics 列**在用（这是本任务的目标列）。通道是通用的：任何列写
   `EmphasisSpan`/`Emphasis` 即生效。
3. **度量中只有第一次出现被强调**：同一列文本重复出现该段（"hit 68% … hit 68%"）时，只强调第一处——
   一处测量就是一处。第二个重复是同样的字符表达的不同事实，全强调等于装饰没量过的格。
4. **未对齐路径的 span 目前通常为空**：窄终端的 label 尾巴是 context pair（母任务 01a0dcd1 的
   re-partition），不是命中率，所以那里 "找不到该段 → 不强调" 是正确结果而不是缺口；该路径的
   通道由 `TestStyledStatusLine_UnalignedPathHonoursTheLabelEmphasis` 单独钉住（label 带命中率时确实着色）。
5. **几何零改动**：`statusLayoutBudgetCols`、`statusPinSlackCells == 0`、编译期守卫
   `const _ = uint(statusLayoutBudgetCols - statusColumnsAtCols)` 均未触碰，未新增/改动任何几何常量。

## 5. 与姊妹任务 01a0e8c7 的合并

`dbd50b71`（01a0e8c7：把 metrics 首个子单元从 context pair 换成命中率读数）**已合入本分支**（merge
commit `a5841f41`）。合并要点：

- 语义冲突已解：我的第一版在 `statusCtxField` 里**也**做了命中率替换（与姊妹重复）。合并后**只保留
  姊妹的实现**（`statusCacheHitRateField`，含 label 在窄格让步的规则），我的产出方文件改成
  **只回读、不自行合成**（`statusHitRateSpan` 从列文本里找该段）。
- 常量冲突已解：`statusHitRateLabel`/`statusHitRatePctSep` 姊妹已定义（Rule 14：一个定义，不镜像），
  故删除我这份重复定义。
- 我这份唯一的补充常量是两档阈值（`statusHitRateGoodFloorPct`/`statusHitRateFairFloorPct`）——
  姊妹的 `statusCacheHitRate` 只算百分比，不做分档。

## 6. 一处**先存**的失败测试（非本任务引入）

`cmd/agent` 的 `TestChatStatus_StaysOneScreenRowWithALongToolSummary`（`lineedit_height_test.go:270`）
**在 master 的干净树上也间歇失败**（`/home/user/work/GoPi` 连跑 5 次：4 次 FAIL；本 worktree 连跑：
0/0/2 次 FAIL）。已单独取证，与本改动无关，见报告 `## Not Done`。
