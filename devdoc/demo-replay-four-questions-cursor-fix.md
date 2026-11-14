<!-- doc-class: historical -->
# 任务报告：docs/archive/demo/replay/verify-frames.py 四问判据的定位模型修复

任务 id `01a0f7d9-db7b-7404-b876-07222c476898`；分支 `task/01a0f7d9db7b7404b87607222c476898`
（从 master `2082feef` 起）。姊妹任务：`01a0f7b3`（HP 帧网格锚点，契约见第 5 节）。

## Completed

1. **矛盾复现（原样输出见第 2 节）**：旧脚本对 `img/clean` 输出 **0/7**、对
   `img-new-vte/clean` 输出 **0/5**，而 committed 的 `evidence/four-questions.tsv`
   那一列**本身全 NO** —— 这张表与 README 第 3 节声称的"14/14 通过"矛盾了很久。
   只有 `img-old-xterm/clean` 是 7/7。
2. **像素级判定**：矛盾在 **verify 的定位模型**，不在参照帧几何 —— 12 张 committed 帧
   （vte 2225×1432、xterm 2227×706）每一张的界面机械特征都齐备（第 3 节）。
3. **最小修复**：`verify-frames.py` 的光标块定位改成"**实心矩形**"（宽度仍 0.6–1.6
   字格；另加竖直连续性 0.5–1.2 个行槽）+ 行槽归属按**覆盖的行槽集合**判。
   `--min-colours 4` 与 `--min-px-per-char 16` **一个字未改**。
4. **最小复现测试** `test-verify-frames.py`（Rule 10.5）：旧脚本上**红**（参照帧 `✗✗✓✓`）、
   修好后**绿**，且 5 张真坏图仍被拒（空帧 / 擦掉光标 / 只剩菜单栏横线 / 擦掉状态栏 /
   光标加宽到 2.1 字格）。
5. **文档更正**：README 第 3 节判据表重写、新增 3.1 节如实记录这处矛盾；顺带修掉三处
   陈旧几何（`2227×706` / `19.01px/字符` / `xterm + gopi capture`）。
6. **证据重生成**：`evidence/four-questions.tsv`、`-raw.tsv`、`raw-clean-pxdiff.tsv`
   用修好的判据重跑（逐张 ✓✓✓✓，与 README 表格一致）。

## Not Done

- **没有重跑 `shoot-replay.sh`**（不起新 Xvfb、不重拍帧）。本次不动任何帧图像 —— 任务
  要求"判据要能测出真坏的图，不是把图修绿"，重拍只会掩盖问题。`evidence/pixel-measure.tsv`
  与 `ui-questions.tsv` 由 `shoot-frames.py` 在拍摄时写出，**未**重新生成（它们的数值不依赖
  被判据修好的那部分：宽度/跨度/每字符像素/色团数与光标定位无关）。
- `evidence-new-vte/` 与 `evidence-old-xterm/` 下的四问 TSV 未刷新；它们在本次修复前就是
  各自那一代几何的产物，且不在验收面内（验收面 = `img/clean` 与 `img-new-vte/clean`）。

## Verification

- Build: `python3 -m py_compile docs/archive/demo/replay/verify-frames.py docs/archive/demo/replay/test-verify-frames.py` → OK（本任务是文档流水线的 Python，不是 Go 构建面）
- Tests: `python3 docs/archive/demo/replay/test-verify-frames.py` → **9 tests, OK**（参照帧 19 张全 ✓✓✓✓ + 5 张负控全拒 + 阈值常量/CLI 默认值/判定点一致性）
- Tests(红): `GOPI_VERIFY_FRAMES=<旧脚本> python3 docs/archive/demo/replay/test-verify-frames.py` → **FAILED**（参照帧 `✗✗✓✓`、几何常量缺失、`KeyError: 'rule_slots'`）
- Tests(N4 红→绿，第二处缺陷的复现): `python3 -m unittest test-verify-frames.NegativeControls.test_N4_status_bar_missing_rejected` 在**未修 q3 的那一版**上 `AssertionError: '✓' != '✗'`；改 `q3 = status_on_last_row and blank_row_before_status` 后 `ok`
- Ran:
  1. `python3 docs/archive/demo/replay/verify-frames.py --dir img/clean` → **7/7 张通过**（修复前 0/7）
  2. `python3 docs/archive/demo/replay/verify-frames.py --dir img-new-vte/clean` → **5/5 张通过**（修复前 0/5）
  3. `python3 docs/archive/demo/replay/verify-frames.py --dir img/raw` → 7/7；`--dir img-old-xterm/clean` → 7/7（无回归）
  4. `python3 docs/archive/demo/replay/verify-frames.py --dir img/raw --out evidence/four-questions-raw.tsv`、`--dir img/clean --out evidence/four-questions.tsv`、`--dir img/clean --out /dev/null --twin-raw img/raw --twin-out evidence/raw-clean-pxdiff.tsv`（重生成证据）
- Result: 逐张 `四问=✓✓✓✓ … → yes`；TSV 与 README 第 3 节的表格一致；负控 5 张全部 `NO`；`git status --porcelain` 无输出。

## Evidence (self-filed)

- 旧脚本对 `img/clean`：`四问判据: 0/7 张通过`，exit=1，逐张 `✗✗✓✓ 光标=30px@槽25/11/19/3/7`（README 的 7 张表声称 ✓✓✓✓）
- 旧脚本对 `img-new-vte/clean`：`四问判据: 0/5 张通过`
- 新脚本对上述两套：`7/7 张通过` / `5/5 张通过`，exit=0
- 像素量（`img/clean`，逐张一致）：光标块 `x 2205..2223 (w=19) y 1321..1375 (h=55)`、颜色 `(0,255,0)`、`slot_h=55.077`、`cell=19`、`y=1292..1294` 为整宽分隔线（跨度 2223 ≥ 98%×2225）
- `img-old-xterm/clean`：光标块 `19×18px y=655..672`、`slot_h=27.15`、分隔线 `y=636`（跨度 2224 / 2227）
- 负控（`test-verify-frames.py`）5 例全部 `pass=NO`；`CURSOR_MIN_W_FRAC=0.6 / CURSOR_MAX_W_FRAC=1.6` 未被改动，`--min-colours 4` / `--min-px-per-char 16` 仍在 argparse 里逐字存在（`test_cli_defaults_unchanged` 直接读源码断言）
- 第二处缺陷的红证据（原样）：`python3 -m unittest test-verify-frames.NegativeControls.test_N4_status_bar_missing_rejected -v` 在 `58589fc4` 那一版上 → `AssertionError: '✓' != '✗' : 状态栏擦掉了却仍判 ✓`；修 `q3` 后同一命令 `ok`
- 红/绿全量（最终版，`evidence/red-green-final.log`）：旧脚本两次 `Ran 9 tests` → `FAILED (failures=2, skipped=1)`，第一条断言即 `AssertionError: '✗✗✓✓' != '✓✓✓✓'`（**无 error**，红是断言判的）；修好的脚本 `Ran 9 tests in 161.725s` → `OK`
- README 表格 ⇄ 脚本输出：`python3 verify-frames.py --dir img/clean` 的 7 行与 README 第 3 节代码块**逐字节相同**（把注释里那 7 行替换成脚本原样输出后比对 → `逐字节一致: True`）；`--out` 重生成 `four-questions.tsv` / `-raw.tsv` / `raw-clean-pxdiff.tsv` 后 `git status --porcelain` 里**没有** evidence 文件变动（读数幂等）
- `gopi doccheck` → `the devdoc corpus is consistent`（exit 0）

## 3. 像素证据：矛盾在判据，不在帧

| 帧（117×26 整屏） | 图尺寸 | 字格 cell | 行槽 slot_h | 真光标块 | 分隔线（整宽连通） | 状态栏 |
|---|---|---|---|---|---|---|
| `img/clean/*` 7 张（vte） | 2225×1432 | 19px | 55.077px | **19×55px**，槽 {23,24}，色 `(0,255,0)` | `y=1292..1294` 跨度 2223（≥98%×2225）、墨迹覆盖率 100% | 末槽 25 有墨迹，其上 `y=1258..1291` 空行 |
| `img-new-vte/clean/*` 5 张 | 2225×1432 | 19px | 55.077px | 同上 | 同上 | 同上 |
| `img-old-xterm/clean/*` 7 张 | 2227×706 | 19px | 27.15px | **19×18px**，槽 {24}，色 `(11,22,11)` | `y=636` 跨度 2224 | 末槽 25 |

**行槽归属**：`slot_h = H/rows`；底部三槽固定 = `rows-3` 分隔线 / `rows-2` 输入行 + 光标 /
`rows-1` 状态栏。真光标**整行高**（vte 55px = 1.00 slot_h；xterm 18px = 0.66 slot_h），
宽度恒为 **1.00 cell**。`you>` 行与分隔线、状态栏各差一行 —— 实测 vte 的
`y=1292..1294`（分隔线）、`y=1321..1375`（光标块）、`y=1388..1429`（状态栏）。
`vision_analyze` 读同一张图的底部条带，确认"横贯整宽的横线 → `you>` 行 + 右端绿色实心方块
→ 最后一行状态栏"（原话在会话目录 `probe/`）。

## 4. 根因（5-why）

1. **症状**：判据对 `img/clean` 报 0/7，而 README 说 14/14。
2. **机制**：输入区那一问要求"光标块在倒数第二槽"，而 `cursor_is_second_last_row=False`。
3. **怎么得的**：光标块由"**在宽度窗口 [0.6,1.6] 字格内取横向游程最长的那一行**"选出。
4. **为什么错**：正文字形的横向游程最大 **30px = 1.58 字格**，落在窗口内且比真光标
   （**19px = 1.00 字格**）更长 ⇒ "取最长"**永远**选中文字，光标位置随机化（槽 25/11/19/3/7），
   连带"输入区上方那条框线"（要求 = 光标槽 − 1）也翻掉 → 四项里两项恒 ✗。
5. **为什么老 xterm 臂是绿的**：它行距 27.15px、字形横向游程恰好 = 19px = 字格宽，
   "取最长"没得可选 ⇒ **偶然正确**。换任一现代 VTE 终端（21px 行距族）即恒 ✗ ——
   这正是 `img/clean` 与 `img-new-vte/clean` 的处境，README 的两张表却仍写着 xterm 时代的 7/7。

**结论**：判据的**定位模型**错（游程 ≠ 实心块；且把"高度"这个**行**的量用 cell 量会得出
"55px 的真光标太高"这种单位错误），参照帧几何达标。差一点踩的第二个坑：光标块的宽度若按
"块最宽行"取，会把光标左邻的字形并进来（实测 36px > 1.6 cell）——所以块宽取**该行自身的
最长游程**，而不是块内最大值。

6. **同一族的第二处缺陷（负控 N4 抓出）**：判据 ③ 的实现写的是"`last_row_has_ink`（图里有
   墨迹）**且**其上有空行带"，而 README 的契约写的是"**最后一个行槽**里有墨迹，且其上有
   一个空行槽" —— 少掉的那半截正好是**行槽归属**，也就是这次修复的主题。后果：一张
   **末槽被整块擦掉**的图（状态栏没了，上一行还有墨迹并隔着空行带）在四问那一列照样得到
   `✓✓✓✓`，只靠 `pass` 里的 `status_on_last_row` 兜底 —— 报告表格那一列是人读的，它说 ✓
   就得是 ✓。修法：`q3 = status_on_last_row and blank_row_before_status`。**为什么它现在
   才被发现**：旧版本里 q2 恒 ✗，四问从不出现 `✓✓✓✓`，这条兜底看不出来；修好定位模型后
   负控才有机会把它顶出来 —— 一条被另一条缺陷遮住的缺陷。

## 5. 改后的契约（供姊妹任务 `01a0f7b3` 对齐）

帧 = 某一帧的 **117×26 整屏**，`rows/cols` 由 CLI 给出（默认 26/117）：

- `slot_h = H / rows`（行槽高，像素）；`cell = 最长整宽分隔线跨度 / cols`（字格宽，像素）。
- **底部三槽固定**：`rows-3` 分隔线、`rows-2` 输入行（`you>` + 光标块）、`rows-1` 状态栏。
- **框线**：某行墨迹跨度 ≥ 98% W **且**该跨度内墨迹覆盖率 ≥ 85%；需同时存在
  槽 0 的一条与槽 `rows-3` 的一条。
- **光标块**：一个**实心矩形** —— 宽 ∈ [0.6, 1.6] × **cell**，同一 x 区间（±1px 抗锯齿）+
  同一颜色上连续 高 ∈ [0.5, 1.2] × **slot_h**，且它覆盖的行槽集合包含 `rows-2`。
  **不要**用"宽度窗口内最长游程"选块（会选中字形，1.58 cell > 1.00 cell）。
  **不要**用 cell 量高度（单位错误；高度是行的性质）。
- **状态栏**：槽 `rows-1` 有墨迹，**且**其上是空行带。两项缺一不可 —— 只写"有墨迹"就等于
  "图里有墨迹"，一张末槽被擦掉的图会漏过去（这是 N4 的复现；判据 ③ 的语义在 commit
  `58589fc4` 里写错过一次，本分支第二个 commit 修）。
- **颜色**：非背景墨迹量化（32 级）后 ≥ min-colours（默认 4）个 ≥200 像素的色团。
- **清晰度**：`px_per_char = 分隔线跨度 / cols ≥ min-px-per-char`（默认 16）。
- **通过** = 四问全 ✓ **且** 清晰度达标（状态栏在末槽这一条已并入 ③，不再单独当兜底项）。

判据的**唯一判定点**是 `verify-frames.py: judge()`；`verify()` 只做像素测量。测试与 CLI
都走 `judge()`，保证"脚本的判定"与"文档写的契约"不会再次分叉。

### 5.1 相对 `58589fc4` 的契约变更点（姊妹任务 `01a0f7b3` 若已对齐过，请按这三点复核）

| 项 | `58589fc4` 版 | 本分支最终版 |
|---|---|---|
| ③ 状态栏 | `last_row_has_ink and blank_row_before_status`（**漏**行槽归属），另在 `pass` 里靠 `status_on_last_row` 兜底 | `status_on_last_row and blank_row_before_status`（行槽归属回到判据里），`pass` 不再有独立兜底项 |
| 四问那一列 | 末槽被擦的图仍可能 `✓✓✓✓` | 末槽被擦 ⇒ ③ = ✗ |
| ① ② ④ 与全部阈值 | — | **未变**（`MIN_PX_PER_CHAR=16` / `min-colours=4` / 光标 0.6–1.6 cell × 0.5–1.2 slot_h） |

## 6. 交付物位置

- 分支：`task/01a0f7d9db7b7404b87607222c476898`（worktree
  `/home/user/workspace/gopi-01a0f7d9db7b7404b87607222c476898`）
- 取回：`git show task/01a0f7d9db7b7404b87607222c476898:devdoc/demo-replay-four-questions-cursor-fix.md`
  （或 `:docs/archive/demo/replay/verify-frames.py`、`:docs/archive/demo/replay/test-verify-frames.py`）
- commit 在共享仓的该分支上（worktree 与主仓共 refs，无需 push）。
