<!-- doc-class: historical -->
# Handoff — 2026-09-28 — chat 渲染复检的未闭合项 + 自主测试基础设施

**上一份**：`devdoc/handoff-20260927-chat-render-and-infobar-defects.md`（10 项缺陷清单）
本次复检对该清单做了验证，**8 项 PASS、1 项被操作者推翻、1 项（T1/T2 家族）仍未端到端验证**。
本文档记录：**已确认的结论**、**仍未验证的项**、以及**本次新识别的功能缺口**。

**复检时的 HEAD**：`10dcd490`（远超上一份文档记录的 `6936c787`；T1 的实际修复
commit `b7d3f86a` 是 `6936c787` 的后代 —— 即修复落地于上一份文档写完之后）。

---

## 0. 复检方法（沿用上一份，并补充本次实测的局限）

- `gopi chat --dump-screen` **先 arm** 该 run，它才会写自己的 `chat-screen.dump`。
- `gopi ui_state --target chat` 把屏幕读成 **cell grid**（带属性，无需像素/vision）。
- `gopi ui_state --compare "<line>" --session <id> --target chat` 判定某行是 grid 丢的
  还是 transcript 丢的（**exit 0 = 两边都有 / 3 = 不一致 / 1 = 检查跑不了**、
  **绝不代表"两边一致"**）。
- **本次实测新增的局限（重要）**：
  - `--compare` 返回 `in_grid=false` 且 `grid_row=-1` 时，**通常是行已滚出 26 行窗口**，
    **不是**丢失 —— 上一份文档已警告，本次确认。
  - **`ui_state --all` 的 `matched=0` 在长 session 中无诊断价值**：它审计整个
    transcript，而 grid 只有 26 行，故大量 `in_grid=false` 是**必然而非缺陷**。
  - **`--dump-screen` 的 dump 只在屏幕变化时写**。不输入则 dump 停在旧帧；输入触发
    重绘则可能抓到"重绘进行中"的帧（会含转义碎片）。详见任务
    `01a0e433-19a7-7c24-bc01-cac9c135721c`。
  - 真 chat 的 dump 之所以**持续可读**，是因为工具调用等持续改变屏幕。**真 session
    的 dump 是可信的；pty 探针的流式采样不可信。**

---

## 1. 本次复检结论（已确认）

| 项 | 判定 | 证据 |
|---|---|---|
| **T3** slash 单候选 Arrow-Down | **PASS（暂缓优化）** | 设计为 documented no-op：单候选时画一行 note 说明"nothing to move to"。操作者手动验证 `/h` **正确过滤**出唯一项 `/help`，但 Down 无法选取 —— 操作者裁定：**这属于更高级功能（类似 tab 补全），优先级放一放**。但该场景被提升为自主测试的验收用例（见任务 `01a0e442-baf1`） |
| **T4** output-language 硬拒 | **PASS** | 实测 `gopi --output-language "not a lang!!" version` → **EXIT=2**，refusal 在 stderr、在 subcommand 分发之前。flag 与 env 均 fatal（settings.json 通道才 warn-and-continue，是 pinned decision）。回归测试 `cmd/agent/cli_output_language_validation_test.go` 的注释精确记录该缺陷 "measured on master 6936c787" |
| **T5** cache 列截断 | **PASS** | 屏幕真值（attrs）：状态行 `▌ deepseek-flash ·  iter 4  ·  tools ·  ⚙ bash … ·  100.1k/200.0k≡ ·↑4M↓77k`，**`↑` 与 `↓` 都在**；整行 **cell-width = 99 ≤ BudgetWidth 100** |
| **T6** info bar 金色 | **PASS** | 屏幕真值 attrs：状态行 `fg=colour-220 bold`（gold）。与上一份文档 addendum 一致 |
| **T7** ctx 分母身份 | **PASS** | 屏幕真值：`100.1k/200.0k≡` —— 分母带 `≡` 身份标识，非裸 `92.1k/200.0k!` |
| **T8** dump-screen 措辞 | **PASS** | help 现为 "ARM this run so its own screen is readable … **WHO READS IT: gopi itself.**"，不再要求操作者 "type"/"compare" |
| **C1/C2** 黄金色更正 | **~~not-a-bug~~ → 被操作者推翻** | `render.go:15-20` 声明 "THE PROMPT IS EMPHASISED, THE BUFFER IS NOT"，但操作者**两次**指出输入仍是 default color 而非 golden，认为是缺陷。见任务 `01a0e439-469c` —— **C1 的结论必须改为 REVERSED** |
| **T1/T2** streaming 期渲染 | **仍未端到端验证** | 见下节 §2 |

---

## 2. 仍未验证的项（**下次必须做**）

### 2.1 T1/T2 家族的真 pty 渲染级验证 —— 任务 `01a0e44e-9cb5-71af-a67d-af702090f3e6`

上一份文档里最严重的一项，**至今没有任何真 pty 证据**。

- **已确认的**：包内守卫 `internal/lineedit/answer_wrap_scroll_dup_test.go` 全绿
  （delta 1..40 + 64、16366 帧全判定）；修复 commit `b7d3f86a` 在 HEAD 中。
- **未确认的**：真 pty + 真 dump 上的 (a) 无字面 marker、(b) 无重复/粘连片段、
  (c) 跨行样式连续；且 **fixture 结构上看不见"不换行行"的路径**
  （`dupTokensPerLine = 18` 使每行都宽于 band 行）。
- **必须走渲染级**，文档原话："绿不代表一切"。

### 2.2 已知的同族 bug（真 pty 上实测复现）

任务 `01a0e433-fe9f-7389-82a2-69cf38ffff11`（+ 证据追加 `01a0e439-5a9b-7551-9e77-17b5c862907a`）

**两个实例**，形态相同（整句 + 截断前缀），且**与行宽无关**：

| | 实例 1 | 实例 2（操作者 verbatim 拷贝） |
|---|---|---|
| 位置 | scrollback row 082（窗口外） | **可见 band** lines 000-002 |
| 行宽 | 41 cell，**不换行** | 换行（被折成 3 行） |
| 屏幕出现次数 | 2 | 2 |
| transcript 次数 | 1 | 1（`session.jsonl` line 307） |

**共同点：都出现在一轮答话的最后一行**（紧邻 turn 分隔线）。
→ 修复**不能只在 wrap 路径上做**，必须覆盖 band 的 **turn-结束提交边界**。

---

## 3. 本次新识别的功能缺口（操作者提出）

按操作者的优先级排序。**操作者的判断：`#1` 是"自主开发的最重要的基础设施，
优先完成，其他都迎刃而解"。**

### `#1` 按键级注入 + 不提交的文本注入 —— 任务 `01a0e438-fee6` / `01a0e442-baf1`

**目标**：gopi 能自己输入 `/h`（**不提交**）看到过滤菜单、按 `\x1b[B` 导航 ——
即**不需要操作者协助就能测试交互路径**。

**实测的现状**：
- `chat_surface action=type` 是**整轮提交**语义：`type("/h")` 会**执行 `/h` 命令**，
  而不是"打进输入行不回车、于是菜单打开"。
- 代码中**不存在**任何 key-injection 入口
  （`grep -rn "Inject\|FeedKey\|injectKey" internal/lineedit/ cmd/agent/`（非测试）无匹配）。

**需要**：(i) `action=key` 注入原始字节序列，走 raw-mode editor 的**真实按键解码路径**；
(ii) 文本注入的**不自动提交**变体。

### `#2` 自驱上界语义改为「每次用户输入之间」 —— 任务 `01a0e44a-46d2`

**操作者裁决原话**：*"这个上界 max turn 可以改掉为每一次互动……一旦我作为用户给你新的
指示，就等于上界清零，又开始计数从零开始达到 4 再停。我们的目的是不要无限跑，每一轮
都有用户监督。"*

**实测现状**：`chatSurface.typed`（`internal/agent/chat_surface.go`）**只增不减**，
一次 loop 用尽后整个 session 的自驱能力被关死，实测 notice：
`self-driven loop CLOSED (0/4 used) … start a new session for a fresh bound`。

**精确改动点（已定位）**：
- 计数器：`chatSurface.typed`（`internal/agent/chat_surface.go`）
- 消费点：`cmd/agent/cli_chat.go:1011-1033`
- **归零点：`cmd/agent/cli_chat.go:1045` 的 `line := lr.line`**（该分支 `lr` 来自
  `lineCh`，即用户真实输入）→ 在此加 `ag.ChatSurfaceResetLoop()`

**代码里已有为设计留的口子**：`ChatSurfaceTakePending` 的注释自述
"*a session whose loop was never started must stay startable*" ——
即当前"关死"行为是 `typed` 只增不减的实现后果，**非设计意图**。

**不可妥协的不变式**：**只有用户输入能开新窗口；模型不得通过任何工具调用自行归零**
（否则 = 无限自驱，失控防护失效）。必须有反例测试。

### `#3` Ctrl-C 打断 LLM session —— 任务 `01a0e439-0ef0-710b-a9cf-d9385b612172`

**操作者**：*"交互的过程中用户无法打断 LLM 的 session……这个是 Hermes 的一个重要功能，
因为有时候，用户发现 gopi 做的有问题需要打断。"*

与 `#2` 互补：**上界**限制"用户两次输入之间的自驱量"，**Ctrl-C** 让用户随时干预。
两者都在 → 上界可设宽松而不怕跑偏。

### `#4` `/queue` prompt 排队 —— 任务 `01a0e439-1e37-72cb-8ba5-19b45ebfd618`

**操作者**：*"hermes 里有一个 `/queue` 可以不用打断 gopi 而是把 prompt 排队，等到 gopi
上一轮完成再作为下一轮的 prompt 自动排队。"*

注意：`chat_surface action=type` 已有"把一行排为下一轮"的机制
（`cmd/agent/chat_surface_surface.go`），**应复用而非另起一套**。

### `#5` 输入行颜色 —— 任务 `01a0e439-469c-7d29-aa0b-c2f5d935e899`

**操作者**：*"我看到输入依然是 default color，不是系统的 golden color，至少这个问题
依然存在。"*（两轮连续指出）

屏幕真值：`row 022: 0:5=fg=colour-186 bold 5:112=default` —— prompt 有色，**输入文本 112
个 cell 是 default**。对照状态行 `fg=colour-220 bold`（gold）。

**必须先裁决意图**（整段染金 / 只有光标体现 / 其他），因为它**推翻了 C1 的 "by design"**。

---

## 4. 依赖关系（下次执行的顺序建议）

```
[基础] #1 按键注入 + 不提交文本注入（01a0e438 / 01a0e442）  priority 0
[基础] #2 BOUND 归零（01a0e44a）                            priority 0
   │  （#1 与 #2 是同一基础设施，建议一起做）
   ▼
[中断] #3 Ctrl-C（01a0e439-0ef0）
[排队] #4 /queue（01a0e439-1e37）
   ▼
[验证] T1/T2 真 pty 渲染级复检（01a0e44e）  ← 依赖上面全部
[修复] 同族重复 bug（01a0e433-fe9f）
[UX]   输入颜色（01a0e439-469c）
```

**为什么 #1/#2 是关键路径**：在它们落地前，上表下半部分的每一项**都无法被 gopi 自主
验证** —— 只能靠操作者手动测试。这正是操作者说的"自主开发的最重要的基础设施"。

---

## 5. 本次复检记录表（下一份文档请继续填）

| 项 | 状态 | 备注 |
|---|---|---|
| T3 | PASS（暂缓优化） | 过滤正常，Down 是 documented no-op；操作者裁定优先级放后 |
| T4 | PASS | exit 2；有回归测试 |
| T5 | PASS | `↑4M↓7k`，99 ≤ 100 cell |
| T6 | PASS | `fg=colour-220` |
| T7 | PASS | `200.0k≡` |
| T8 | PASS | help 改为 "WHO READS IT: gopi itself" |
| C1/C2 | **REVERSED（操作者裁决）** | 输入行颜色，待裁决意图后实现 |
| T1/T2 | **OPEN** | 从未真 pty 验证；见 `01a0e44e` |
| 同族重复 bug | **OPEN（已复现）** | `01a0e433-fe9f`，两个实例 |

---

## 6. 诚实边界

- **本 session 未能完成 T1/T2 的真 pty 验证**：pty 探针的 dump 采样不可信（§0），
  且本 session 的自驱轮已耗尽（这正是 `#2` 要解决的）。
- **未验证 ≠ 已修复**。T1/T2 的"守卫绿"只是单测真值，文档明确要求渲染级验证。
- **C1 被推翻**是基于操作者的直接观察，但**具体意图（染哪种颜色）尚未裁决** ——
  实现前必须先问清楚，不要猜。
- 所有本轮产生的临时探针文件已清理（`git status --porcelain` 为空）。
  **注意**：`/tmp/zz_dup_grid.txt` 等是**更早**任务留下的 T1 baseline 证据，
  **不是本轮产生的**，可能被系统清理 —— 若需要请尽快留档到仓库内。
