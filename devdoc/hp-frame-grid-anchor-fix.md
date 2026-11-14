<!-- doc-class: historical -->
# 任务报告：HP 帧采集网格上边界的定锚 —— 缺口①修复，缺口②的验收口径被证伪

任务 `01a0f7b3-5b08-7ae8-9590-f66644783138`（本单）／前单 `01a0f763`。
主机 peerbox，master `2ff133e7`，执行日 2026-10-01。

> **引用说明（2026-10-03 起）**：本文件引用并逐字复制的 `docs/archive/demo/replay/` 三套参照帧
> （`img/` `img-new-vte/` `img-old-xterm/`）**已随该目录的全部图像产物移除**（Rule 39；
> epic `01a0fd2e` scope 3）。下面的命令与输出是**当时的现场记录**，不是可逐字重放的现在：
> 帧本身由 `bash docs/archive/demo/replay/shoot-replay.sh` 重跑生成，判据
> `docs/archive/demo/replay/verify-frames.py`（与它的最小复现测试 `test-verify-frames.py`）仍在树上，
> 那次返工的推理在 `devdoc/demo-replay-three-reworkings.md`。

---

## 一句话结论

本单要修的**两个**缺口里，**缺口①（`crop-grid.py` 的锚点与裁切框）已经修好并实测生效**；
**缺口②的验收口径本身是坏的**——用 master 现在的 `verify-frames.py` 原样去跑，
**连仓库自己 committed 的、README 声称 14/14 的参照帧都过不了（实测 0/7）**。
在"判据不得放宽、必须用 master 的 verify 原样跑"这两条硬约束同时成立时，
"out/clean 跑出 7/7" 不可达——不是图像做不到，是判据的定位模型与目标几何不一致。

---

## Completed

### 1. `crop-grid.py` 的锚点与裁切框（缺口①，已修复）

新脚本在 `hp@<session>/hp/crop-grid.py`（本单 session 目录，Rule 29：HP 仓库一字节未动），
相对前单的两处改动：

- **锚点**：不再取"整屏最长墨迹行"，而是取**窗口内部第 0 行的那条整宽细分隔线**——
  跨度 ≥ 文字区宽 90%、跨度内连通覆盖 ≥ 85%、**带高 ≤ 半个行高**（这一条把 gnome-terminal
  自带的菜单栏实心块排掉：菜单栏块实测 2470px 宽、比正文分隔线更靠上，正是前单选错的那条）。
- **裁切框**：先按性质去掉滚动条列（整列墨迹占比 ≥ `SCROLLBAR_COL_FRAC`，文字列永远达不到），
  再用**两条同形 `─` 分隔线的行心差**算真实行距（第 0 行 ↔ 第 23 行 ⇒ 23 × 行距），
  而不是 H/ROWS 除出来——实测 1094/26 = 42.08 会把光标块切成两半而落到 23 槽，真实 42.0 才把它放回 24 槽。

**成品判据也从"sha 稳定 + 色数 > 3"换掉了**（`inner2.sh`）：改为脚本的出口码——
锚点找得到 **且** 26 个行槽里 ≥ 16 行有墨迹。旧判据在空窗口上也成立（菜单栏抗锯齿文字 = 179 色，实测）。

**实测产物**（HP 私有 Xvfb `:89`，`metacity`，`dbus-run-session`，`DCONF_PROFILE` 私有字号 26pt；
绝不读/写操作者 DISPLAY / dbus / dconf，清理只按 pid）：

```json
// hp/logs/clean-01.crop.json（逐帧落盘，原样）
{
 "left": 264, "top": 94, "grid_w": 2462, "grid_h": 1092,
 "text_w": 2462, "text_h": 1094,
 "anchor_y": [113, 116],
 "second_rule_y": 1079,
 "px_per_char": 21.043,
 "row_pitch": 42.0,
 "pitch_route": "分隔线间距（第0行↔第23行）",
 "ink_rows": 24,
 "bg": [48, 10, 36]
}
```

网格 `(264,94) 2462x1092`、行距 `42.000px`（由两条分隔线实测，不是除出来的）、
每字符 `21.04px`（判据 ≥16，过）、**24/26 行槽有墨迹**。前单的"带菜单栏的空窗口"已消失：
帧 01 现在真的含正文 24 行 + 输入区光标块 + 状态栏（下节像素证据）。

### 2. 缺口②的验收口径被证伪（本单最重要的发现）

`verify-frames.py` 的「输入区」一问取 **全图最长、且落在 `[0.6, 1.6] × 字宽` 内的同色游程**，
把该行当光标（`verify-frames.py:76-88`）；选择规则是 `if cursor is None or best > cursor[1]`
——**取最长**。问题是这个窗口的上界 `1.6 × cell` 同时包住了**正文的字形游程**。

逐像素测量（脚本 `evidence/cursor-rule-measure.tsv`，本单 session 目录）：

| 集合 | 图 | cell | 光标窗口 | 真光标块（绿） | 正文最大同色游程 | 比 | verify 选中 |
|---|---|---|---|---|---|---|---|
| xterm 参照集（committed，通过） | 2227×706 | 19 | 11.4–30.4 | run=18 @y655 槽24 | 19 @y656 槽24 | **1.06** | 光标（对，侥幸） |
| VTE 参照集（committed，不通过） | 2225×1432 | 19 | 11.4–30.4 | run=19 @y1321 槽23 | **30** @y1412 槽25 | **1.58** | **正文（错）** |
| HP 本单产出 | 2462×1092 | 21 | 12.6–33.6 | run=21 @y1008 槽24 | **33** @y942 槽22 | **1.57** | **正文（错）** |

读法：真光标块的宽度**恰好**是 1.00 × 字宽（21px @cell21、19px @cell19）；
而正文里最长的同色游程随字号升高会超过 1.6 × 字宽（VTE 30px @cell19 = 1.58，
HP 33px @cell21 = 1.57）。**凡每字符 ≥ 20px 的渲染都必然输给正文行，`q2` 恒为 ✗**；
xterm 参照集之所以过，是因为它的 cell=19 使正文游程与光标游程**并列**（19 vs 19，
1.06 倍），而选择规则在并列时先遇到光标行。这正是"7/7 曾经成立、现在不成立"的机理。

决定性对照——**同一脚本、同一命令、对 master 自己 committed 的三套图**：

```console
$ python3 docs/archive/demo/replay/verify-frames.py --dir docs/archive/demo/replay/img/clean --rows 26 --cols 117 --min-px-per-char 16
  01-01-see-the-boundary.png         ✗✗✓✓  框线行=12(顶有/输入上方无) 光标=30px@槽25 状态栏槽25 占用槽=24/26 色团=25 19.00px/字符 → NO
  ... (02–07 同为 ✗✗✓✓ → NO)
四问判据: 0/7 张通过
# ↑ README 第 3 节对这套图写的是「14/14 通过」

$ python3 docs/archive/demo/replay/verify-frames.py --dir docs/archive/demo/replay/img-new-vte/clean --rows 26 --cols 117 --min-px-per-char 16
四问判据: 0/5 张通过

$ python3 docs/archive/demo/replay/verify-frames.py --dir docs/archive/demo/replay/img-old-xterm/clean --rows 26 --cols 117 --min-px-per-char 16
  01-01-see-the-boundary.png         ✓✓✓✓  框线行= 4(顶有/输入上方有) 光标=19px@槽24 状态栏槽25 占用槽=24/26 色团=13 19.01px/字符 → yes
  ... (02–07 同为 ✓✓✓✓ → yes)
四问判据: 7/7 张通过

$ python3 docs/archive/demo/replay/verify-frames.py --dir <本单 HP 产出>/out/clean --rows 26 --cols 117 --min-px-per-char 16
  01-clean.png                       ✗✗✓✓  框线行=16(顶有/输入上方无) 光标=33px@槽22 状态栏槽25 占用槽=24/26 色团=28 21.00px/字符 → NO
四问判据: 0/1 张通过
```

全文（未删减）在 session 目录 `evidence/verify-runs.txt`。
**并且 committed 的 `evidence/four-questions.tsv` 内容本身就是全 NO** ——
README 第 3 节的"14/14"表格与仓库里实际测量到的结果互相矛盾，不是本单引入的。

结论：**7/7 不可达**，且不可达的原因在 verify 一侧。修 `verify-frames.py`
已按 Rule 26 派给姊妹任务 **`01a0f7d9-db7b-7404-b876-07222c476898`**（独立成单，因为它在
`docs/archive/demo/replay/` 的判据与文档、不在 HP 侧；本单的约束禁止改动它的判据）。

---

## Not Done（诚实列出）

- **`--dir out/clean ... 7/7` 未达成**，且在现行约束下不可达（上节给出定量证据）。
  本单没有为了让绿变亮而放宽 `MIN_PX_PER_CHAR` / `min-colours` —— 那是被明令禁止的。
- **只产出了 1 帧合格网格，不是 7 帧**。原因逐条列出，不粉饰：
  1. 帧 01 在轮询第 4 次成功；帧 02–07 的运行里，窗口在 `xdotool` 眼里一度只剩
     `3000x1250`（即整屏尺寸）——窗口被 WM 取消映射/重映射期间 `getwindowgeometry` 返回的是
     屏幕尺寸，锚点自然找不到，30 轮全部 FATAL（原始日志见下）。
  2. 另一条 FATAL 是**成品判据按设计生效**：`分隔线已找到（y=113..116），但 26 个行槽里只有
     10 行有墨迹（判据 ≥16）—— 窗口是个空壳，正文还没打印出来；不交半成品`。
     这一条是本单想要的**正确**行为（前单正是少了它才交了空窗口），但要它稳定通过，
     需要让场景里的正文在"窗口拿到真尺寸"之后完整画出——那是前单 `gen-scenes.py` 的
     "等真尺寸再打印"时序问题，本单未修完。**已按 Rule 26 单独立单**：
     `01a0f7e5-82e5-788b-95f6-56526b7dd91e`（几何判据改成性质：不等于屏幕尺寸 + 容得下 26×117，
     而不是"连续若干轮尺寸相同"）。
  3. 每帧重建 display + `dbus-run-session` 的开销约 45–60s，7 帧串起来远超一次 60 分钟会话里
     能反复试错的量；本单的剩余预算用在了把根因钉死（上面的定量表），而不是反复重跑。
- **`crop-grid.py` 只对 VTE(gnome-terminal) 单样本验证过**：xterm 与它字形不同，
  "两条同形 `─` 分隔线算行距"这条路径未在 xterm 上跑过。
- **帧 02 及以后的 `shots/full2-*.png` 已被后续重跑覆盖**，只有帧 01 的全屏原图与裁出图留档
  （`hp/keep/`）。逐帧 JSON 与日志仍在 `hp/logs/`。
- README 第 3 节"14/14"与 `evidence/four-questions.tsv` 全 NO 的矛盾**未在本单修正**——
  它属于姊妹任务的文档范围（该任务的验收里已写明要更正）。

---

## Verification

- **Build**: 不适用（本单只改 Python/bash 中间件，Go 仓库零改动：`git status --porcelain` 在本单 worktree 里除本报告外为空）。
- **Tests**: 不适用；本单的"测试"是逐像素测量脚本 `probe-cursor.py`（本单新写，落在 session 目录）
  与 `crop-grid.py` 自己的出口码（成品判据）。
- **Ran**（逐字，原样）:

  ```bash
  # 1) 对 master committed 的三套图 + 本单产出，用 master 的 verify 原样跑
  python3 docs/archive/demo/replay/verify-frames.py --dir docs/archive/demo/replay/img/clean        --rows 26 --cols 117 --min-px-per-char 16
  python3 docs/archive/demo/replay/verify-frames.py --dir docs/archive/demo/replay/img-new-vte/clean --rows 26 --cols 117 --min-px-per-char 16
  python3 docs/archive/demo/replay/verify-frames.py --dir docs/archive/demo/replay/img-old-xterm/clean --rows 26 --cols 117 --min-px-per-char 16

  ```

- **Result**: 见上两节——`0/7`（img/clean）、`0/5`（img-new-vte）、`7/7`（img-old-xterm）、`0/1`（本单产出）；
  HP 侧帧 01 产出 `grid_w=2462 grid_h=1092 row_pitch=42.0 px_per_char=21.043 ink_rows=24`。
- **Commit**: `9ae5aa83`（原始交付 commit，分支 `task/01a0f7b35b087ae89590f66644783138`）；
  交付点同步为 `21a5237c`（`git merge master`，唯一冲突 `devdoc/DOC-INDEX.tsv` —— 两侧各加一行
  自己的 devdoc，**保留双方**）。`git merge-base --is-ancestor master HEAD` → 0（YES）。

## Evidence (self-filed)

本单自己跑出来、并留在 session 目录（`~/.gopi/sessions/01a0f7b3-5b08-7ae8-9590-f66644783138/hp/`）的证据：

| 主张 | 命令 | 观察到的值 |
|---|---|---|
| 缺口①已修好 | `ssh hp "bash run2.sh"`（帧 01） | `logs/clean-01.crop.json`：`grid_w=2462 grid_h=1092 anchor_y=[113,116] second_rule_y=1079 row_pitch=42.0 pitch_route=分隔线间距（第0行↔第23行） ink_rows=24 px_per_char=21.043` |
| 帧 01 真有正文+光标+状态栏 | `python3 probe-cursor.py hp/keep/clean-01.png 26 117` | 绿块 `(0,255,0)` 自 y1008 连续至 y1049（**42 行 = 1 个字格高**，宽 21px = 1.00 cell），槽 24；状态栏黄字 `(255,215,0)` 自 y1055 起，槽 25 |
| 缺口②的判据错在"取最长" | `evidence/cursor-rule-measure.tsv` | 三集比值 **1.06 / 1.58 / 1.57**：≥20px/字符的渲染里正文游程必然超过真光标 |
| verify 的 7/7 只对 xterm 成立 | `evidence/verify-runs.txt` | `img-old-xterm/clean → 7/7`；`img/clean → 0/7`；`img-new-vte/clean → 0/5`；本单产出 `→ 0/1` |
| 成品判据在空壳窗口上生效 | `job-3` 输出原文 | `FATAL: 分隔线已找到（y=113..116），但 26 个行槽里只有 10 行有墨迹（判据 ≥16）—— 窗口是个空壳，正文还没打印出来；不交半成品` ×5 |
| 帧 02–07 失败的直接原因 | `job-3` 输出原文 | `真 toplevel: id=2098797 几何 2523x1208` → 之后 `轮询 9/30` 起变成 `窗口 3000x1250、文字区 3000x1250`（窗口重映射期 `getwindowgeometry` 返回屏幕尺寸），`轮询 30/30` 仍找不到锚点 → `FATAL: 180 秒内没有产出合格网格` |
| 分支相对 master 只动 `devdoc/` | `git diff --stat master...HEAD` | `devdoc/DOC-INDEX.tsv 1 +`、`devdoc/hp-frame-grid-anchor-fix.md 168 +`，2 files changed, 169 insertions(+) |
| `internal/standards` 的红不是本单引入 | `make test-budget PKGS='./internal/standards ./internal/docindex'`；`git diff --stat master...HEAD -- internal/scratch/profile_root.go` | 唯一失败 `TestStandardGates/TestGate4PanicRedProof`（Rule 19，`internal/scratch/profile_root.go:206`）；该文件在本单 diff 里的 stat 为**空**（0 行） |
