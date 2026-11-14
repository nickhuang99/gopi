<!-- doc-class: historical -->
# Task Report: Computer Use 全景 + 5 切口分析（分析文档，无实现）

**Branch**: `feat/cu-landscape-analysis` · **Worktree**: `~/workspace/gopi-cu-wt`（Rule 6 链式 worktree，与主仓库共享 refs → 无需 push）
**Deliverable**: `devdoc/computer-use-landscape-and-niches.md`

## Completed
- 8 阶段管线解剖（intent→capture→state→decision→actuate→wait→verify→recover），逐段标 `成熟 / 新变量 / 混合`，附反证（§1.1、§1.2）。
- 玩家版图表 7 层 + 空格位（本地优先 / 非云 / 非标设备 / 长循环只读看护），空格位显式标为**推断**（§2）。
- 5 切口 × 5 维度 = 25 格矩阵，逐格带 `[S#]`/`[L#]` 来源编号或显式 `未验证`（§3）。
- 不做清单 10 条，每条带理由与证据（§4）。
- gopi 编码 vs CU 4 原语同构分析（capture/actuate/wait/verify），含本地量化差异（60× 观测代价、verify 零 LLM 调用）（§5）。
- 诚实不确定清单 12 条：抓取失败清单 + 自身方法学弱点（§6）。
- 单一推荐切口（niche A）+ 可证伪 72h 实验 + 最小成本（0 新硬件 / ≤2 人日 / ≤¥10 token）+ 预设判决规则（§7）。
- 来源清单：17 个外部 URL（其中 15 个经 web 工具实际抓取正文）+ 5 组本地实测证据路径（§8）。

## Not Done
- 未取得 Anthropic / OpenAI / AWS / Google 的一手文档与定价（抓取超时 / 403 / 404）→ **未做任何 ROI 计算**，并在 §6 第 1–3 条标注。
- 未穷尽枚举"空位"→ 空格位只作假设，不作结论（§6 第 6 条）。
- 未实测 Windows UIA / macOS AX 通道；CH9329 硬件未接入（其延迟为协议推算）→ 标 `未验证`（§6 第 8–9 条）。
- 未运行 §7 推荐实验（本任务范围是分析；该实验是推荐交付，不是本次交付）。
- 未提交他人未跟踪产物（`devdoc/tab-order-poc/`、`devdoc/tab-order-actuate-poc.md`、`devdoc/webcam-capture-evidence/`）：仅只读引用为本地证据，**未纳入提交**。

## Verification
- 产物存在：`devdoc/computer-use-landscape-and-niches.md`（252 行 / ~31 KB，本分支 HEAD 中）。
- 模糊词闸门：`grep -n "大概\|可能\|应该\|相信\|业内普遍\|必将\|一定会"` → 唯一命中在"阅读前提"的**否定句**（声明不使用该说法），矩阵单元格与正文分析句零命中。
- 矩阵覆盖：5 切口 × 5 维度共 25 格逐一填写，每格以 `[S#]`/`[L#]` 或显式 `未验证` 收尾（全文 `未验证` 出现 26 次）。
- 外部来源：17 个 URL，其中 ≥3（实际为 9 个文档正文 + 6 个 README/站点 + 1 个 GitHub API）经 web 工具**实际抓取**并在文中 inline 引用；抓取失败的来源在 §6 明列。
- 构建/测试：纯文档任务，无代码改动 → 无 build/test 步骤。
- 提交隔离：下方 `git show --stat` 只含本任务两个文件；主仓库 `git status` 不含我的文件。

## Commit
- 分支 `feat/cu-landscape-analysis` 的 HEAD（哈希见最终回复；报告与主交付物同批入库，故不内嵌哈希以免 amend 后过期）。
- 逐文件 `git add`（Rule 8，未用 `git add -A`），该提交只含：
  - `devdoc/computer-use-landscape-analysis-report.md`
  - `devdoc/computer-use-landscape-and-niches.md`
- `git show --stat` 输出：

```
 docs(cu): 冷静解剖 Computer Use 管线/版图/5 切口 — 成熟件 vs LLM 新变量 + 不做清单 + 最小首步
 devdoc/computer-use-landscape-analysis-report.md |  31 +++
 devdoc/computer-use-landscape-and-niches.md      | 252 +++++++++++++++++++++++
 2 files changed, 283 insertions(+)
```

- 交付物持久位置声明（Rule 6d）：分支 `feat/cu-landscape-analysis`；恢复命令
  `git show feat/cu-landscape-analysis:devdoc/computer-use-landscape-and-niches.md`。
