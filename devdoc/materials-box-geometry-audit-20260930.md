<!-- doc-class: historical -->
# 素材框几何审计：D 面配方是否外溢（159 条逐条像素测量）

**日期**：2026-09-30　**任务**：`01a0eede-4972757984b11c441929149a`
**本页的全项目计数（159 条素材 / 144 条有病 / 15 条干净）的生成器与测量点**：
生成器 `scripts/materials/box_audit.py`（复跑：`python3 scripts/materials/box_audit.py
docs/archive/demo/materials <out.json>`）；测量于 commit `bba716f6`（= 本分支基线 master 的
merge point），`docs/archive/demo/materials/` 在该 commit 下的工作树为干净。
**结论一句话**：D 面修掉的**两个坏配方有一个真的外溢了**——共享渲染器
`scripts/materials/render.py` 把框的上下边按**标定行距外推**、并用**比例**判框对错；
盘上 159 条成对素材里 **144 条**的标记线压在字形芯上（切片量 7–252 像素）。
本轮把 D 面配方（行槽按像素量 + 框边由像素定 + 三命题结构判据）搬进共享渲染器，
并留下红→绿回归。

## 1. 两个坏配方在代码里的位置

| 配方 | 位置 | 实测 |
|---|---|---|
| ① 行槽/框坐标**按标定 pitch 外推** | `scripts/materials/render.py`（旧）：`y = int(round(y0 + pitch*idx)) - PAD_ABOVE_MARK`；`bh = line_ink_h + BOX_H_PAD` | pitch 是**取整过的整数**（实测 30），真实行距带小数 ⇒ 每行漂零点几像素；且框高比本行墨迹高 6px |
| ② 用**比例**判框对错 | 旧判据 = `ink_in_box.ink_under_mark`（数字只用于"有墨迹"），实测 2940–3160 | 比例看不见"框线切了下一行的字" |
| ①′ 同一套标定的**另一份实现** | `scripts/materials/render-entry.py`：`box_y = MARGIN + mark_line*LH`，无 pitch 外推 | 该面 17 条实测**全干净**（over128 = 0） |

D 面（`materials-render-D.py`）早已改成三命题结构判据 + 像素定量；共享渲染器直到本轮
才同步——这就是"外溢"的全部内容。

## 2. 测量口径（为什么可信）

三条规矩，都是为了**不从生成器的世界里取任何值**：

1. **框矩形由颜色身份量出**，不由差分形状推断。`gopi annotate` 的标记墨是
   RGB(220,30,30)（`internal/text2img/annotate.go` 的 markInkR/G/B），底图是灰阶
   （0/64/127/191/255），两者不重叠 ⇒ 逐像素相等即可判"这是框线"。
   用差分形状会漏：某条素材的框边在差分里只有 4 像素宽的行，长游程门槛一卡就整条漏掉
   （实测踩过，见 §5）。
2. **标签芯片不是被审计的框**：芯片把底图**涂白**再写标签字，因此"底图内部墨迹 = 0"
   的就是芯片。不用几何猜、不用位置猜。
3. **两条独立判据**，互相不依赖：
   - 框**边所在行**（top / bottom）在框的列范围内切到多少原始墨迹像素（`<128` 算 glyph 芯）；
   - 框**内部**有几段墨迹（段 = 连续有墨行，间隔 ≥ 4 行才算断开）。

口径写死在 `evidence/box_audit.py`（脚本随报告留在 session 目录，盘上 159 条可直接复跑）。

## 3. 逐面测量结果

| 面 | 生成器 | 条目 | 干净 | 有病 |
|---|---|---|---|---|
| flags | `gen-flagF.sh` / `gen-flagF-214.sh` | 31 | 0 | 31 |
| top | `gen-topB.sh` | 26 | 0 | 26 |
| task | `gen-taskA.sh` | 17 | 0 | 17 |
| （—） | `gen-task17-CJK.sh` + `render-entry.py` | 17 | **17** | 0 |
| memory | `gen-memoryC.sh` | 14 | 0 | 14 |
| top | `gen-topA.sh` | 14 | 0 | 14 |
| env | `gen-env-E1.sh` | 14 | 1 | 13 |
| env | `gen-envB.sh` | 8 | 0 | 8 |
| sandbox | `gen-taskD.sh` | 7 | 0 | 7 |
| env | `materials-gen-env-notify.sh` | 6 | 0 | 6 |
| node | （census） | 4 | 1 | 3 |
| env | `env-census.sh` / `render.py` | 1 | 0 | 1 |
| **合计** | | **159** | **15** | **144** |

切片量分布（`over128`，即标记线压 glyph 芯的像素数）：干净 = 0（15 条）；
有病的最小 7，最大 **197**（`006-sandbox-run`，6 个框）。最重的五条：

| 条目 | over128 | 框数 |
|---|---|---|
| `006-sandbox-run` | 197 | 6 |
| `211-flag-output-language-refusal` | 188 | 3 |
| `002-checkpoint-list` | 162 | 4 |
| `217-flag-position-not-verb-first` | 137 | 3 |
| `env-01-output-language-fatal` | 120 | 2 |

干净的 15 条：`01-list 02-show 03-status 06-verify 07-archive 08-search 09-for-branch
10-sweep-worktrees 11-sweep-zombies 12-finish-cleanup 13-improvements 14-dedup-scan
15-verdicts 16-tag-resumed 035-env-compute-nodes`。前 14 条全在 `render-entry.py` 面，
第 15 条是 census 面——**"哪一面干净"与 §1 的代码定位完全一致**，这是本审计最强的
内部一致性证据。

## 4. 修法（照搬 D 面配方）

`scripts/materials/render.py` 新增/替换三件，逐框过检、量不出就 FATAL：

- `measure_line_band(png, y0, pitch, idx, x0, x1)`：量出全图墨迹段，每段按
  `round((top - y0)/pitch)` 认领行号，**认领必须一一对应**（两段认领同一行 → 诊断）；
  找不到目标行、或该段高度 ≥ 一行行距（跨了两行）→ 诊断。标定值**只当搜索提示**。
- `place_box(png, band, w, idx)`：上下边放进墨迹段**之外**的留白（`BOX_PAD` 起，
  `FRAME_TRIES` 次），让不出干净边 → 返回诊断，**绝不 clamp 到别的行**。
- `check_box(png, box, band, idx, label)`：三命题结构判据——① 上下边所在行在框列
  范围内无墨迹（`EDGE_LUMA=128`）；② 框内恰好一段墨迹；③ 那一段落在**本行行槽**里
  （拦"整行漂一格"）。verdict 与逐框明细随 JSON 一起产出（`box_verdicts`），
  旧的"有墨迹就算过"整条删掉。

新常量（Rule 1，全部具名）：`INK_LUMA=200`（判"这行有墨"，含抗锯齿）、`EDGE_LUMA=128`
（判据阈值，框线落在这一档）、`BOX_PAD=3`、`FRAME_TRIES=4`、`SPAN_GAP=4`。
阈值取值与 D 面同一口径，**没有为新缺陷放宽任何一条**。

## 5. 红→绿回归与真跑对照

`scripts/test_materials_render.py`，6 项断言，全部通过：

1. **RED**：盘上**真的发出去的那个框**（`002-task-submit/04-annotated.png` 里量出的
   `(5,343)-(1064,364)`）被判 MISALIGNED，且**理由是下边切到 18 个墨迹像素**——
   不是碰巧别的命题。用真样本当红，不是编一个。
2. **非空**：同一个框，旧的**比例**判据确实放行过它（框内墨迹 > 0）。红是实的。
3. **机理**：红样本的形状是"顶边 0 墨迹、底边 18 墨迹"——正是旧公式的不对称
   （只有顶部 `PAD_ABOVE_MARK=3`，框底贴着本行行槽的底边，而该行末尾是下伸部）。
4. **GREEN**：新算法在同一行的像素上量出的框，三命题全过，且与旧框**不同**。
5. **DRIFT**：标定值偏 2px —— 外推值跟着漂（`≈2px`），而 `measure_line_band` 量出的
   行槽**不动**（咬住像素）。
6. **拒绝**：让不出干净上下边时返回诊断，而不是悄悄给一个坏框。

真跑对照（Rule 31）——**同一份 01/02 证据字节**，`git show HEAD:scripts/materials/render.py`
当"旧"，工作树当"新"：

```
old rc/sha03/sha04: 0 24be387ef3620b9f 42c3315b3c36cfa1
new rc/sha03/sha04: 0 24be387ef3620b9f 165f2fef34536d61
03-raw IDENTICAL: True    04-annotated CHANGED: True
old: mark=5080 over200=65 over128=46 boxes=[(5,365,158,398), (5,403,1072,424)]
new: mark=5096 over200=0  over128=0  boxes=[(5,365,158,398), (5,403,1072,428)]
```

`03-raw.png` 逐字节相同 ⇒ 两次跑的是**同一张图**，差别只在框；旧框压 glyph 芯 46 像素，
新框 **0**。

## 6. 重画：机制做出来了，已修 2 条（后续条目按同一配方铺开）

**根因（第二轮查出来的，纠正了 §6 早先的判断）**：老素材的 03-raw **不是**当前
`render.py` 版式画的——老图**没有**标签净空插入、**没有**文末补行，高度由行数直接决定
（444 = 14 行）；现在的默认版式会插净空并把图撑到 564。所以"重建不出来"不是缺输入，
而是**版式不同**。

**修法**：`render.py` 增加 `--legacy-layout`——按**盘上那张图自己**的版式重画
（不插净空、不补尾行、按原图高度定行数；标签带覆盖只写进 JSON 不拒绝，因为老素材
本来就压着上一行）。

**重画入口**：`scripts/materials/rerender_entry.py`，输入全部从**已在仓库里**的东西重建：
- 图顶解说 = `00-entry.md` 的「## 标注图在指什么」正文；
- 三段分隔行 = 生成器脚本里的 printf 字面量（各面逐字相同，从 `gen-*.sh` 取回）；
- 正文 = `01-input.txt` + `02-output.txt`（证据，只读）；
- `--mark-match` 规格 = 生成器 `entry …` 调用的第 8 参数（字面量形与 `$MARKS_TABLE` 形都支持）。

**守门（faithful reconstruction）**：重画的 `03-raw.png` 必须与盘上**逐字节相同**
才允许写盘；不同 = 重建没做对 ⇒ SKIP 退出非零、一个字节都不写。再加一条尺寸守卫。

**实测结果**（盒级 `over128`，标签芯片不计入）：

| 条目 | 旧框压字形芯 | 新框 | 03-raw |
|---|---|---|---|
| `002-task-submit` | 46 | **0** | sha `2c4bcdb8…` 未动（逐字节相同） |
| `003-task-submit-bug` | 57 | **0** | sha `a89a9f65…` 未动 |

两条的 `01/02` 在 diff 里不出现。

**还没铺开的原因（诚实说）**：其余面各有各的取回路径要先补——
`gen-taskD.sh` 的 render 输入不走那三个 printf（是另一套写法），
`gen-task17-CJK.sh` 走的是 `render-entry.py`（实测无病，不需要重画），
`gen-memoryC.sh` / `materials-gen-env-notify.sh` 的 marks 规格没落在 `entry` 的
第 8 参数位置上（`no mark line landed inside the image`）。
每条都要单独确认取回路径——这是**按面铺开的工作量**，不是机制缺口。

## 7. 测量脚本踩过的两个坑（后来者别再踩）

1. **差分形状猜框会漏**：annotate 的框线在差分图里只有 ~4 像素宽的行，长游程门槛
   （≥24）一卡就整条漏掉，表现为"no-box"（实测 96 条被判 unmeasured，全是假阴性）。
   改成**按标记色身份取像素**后立刻变得可测。
2. **纯 Python 逐像素扫全图太慢**：159 张 × 全图 twice ⇒ 单次跑超 2 分钟、被 bash 超时
   杀掉两次。改成"只扫差分 bbox + flat 列表"后仍在 100–140 秒量级，最终靠后台 job 完成。
   下一次直接写成 `numpy`/`array` 或只扫标注色像素列表。
