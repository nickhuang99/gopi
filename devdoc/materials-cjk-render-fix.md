<!-- doc-class: normative -->
# 素材截图的中文乱码：根因与修法（demo materials 17 条）

**日期**: 2026-09-30　**分支**: `task/01a0ee8bb7ba75dfa8b06adc4a3f6449`　**范围**: `docs/archive/demo/materials/{01-list…17-reopen}`

## 症状与根因（5-why，不停在"看起来对"）

| 现象 | 谁在错 |
|---|---|
| `03-raw.png` 里中文全是 Latin-1 乱码 | 渲染侧 |
| `04-annotated.png` 同一张图，标注**中文是好的** | 标注侧没问题 |
| `02-output.txt` 是完好 UTF-8 | 数据侧没问题 |
| 字体已安装（`wqy-zenhei` / `DroidSansFallbackFull`） | 字体侧没问题 |

**一问**：为什么只有截图侧坏？—— 两个侧走的是**两条渲染路径**：截图用 `xterm -display :77` + `gopi capture`，
标注用 `gopi annotate`（= `internal/text2img`，自研 SFNT/TrueType + 字体链）。
**二问**：为什么 xterm 会把 UTF-8 画坏？—— 旧生成脚本里**没有 `LANG`/`LC_ALL`**。
没有 locale，xterm 不按 UTF-8 解码，一个 3 字节的汉字被当成 3 个 Latin-1 字符逐字节画。
**三问**：为什么"没设 locale"是根因而不是"字体没装"？—— 因为同一台机器、同一份 `02-output.txt`、
同一个文件里的标注中文是好的；差别只在渲染器。**四问**：为什么修法不能只是"加一行 LANG"？——
见下节。**五问**：为什么会留下这个洞？—— 旧脚本把截图的正确性外包给了一个**外部程序的运行环境**
（xterm + locale + X display + 窗口尺寸），而这三样都不在仓库里、也不在复跑命令里。

## 三条路，选了 C（理由，不是偏好）

| 路 | 内容 | 判 |
|---|---|---|
| **A** | 给旧脚本加 `LANG=zh_CN.UTF-8`/`LC_ALL` | 否：等于把正确性继续押在**环境变量**上——同一份脚本换台机器、换次 login 就再坏一次；而且它没解决"一图两制"，标注与截图仍是两条路径 |
| **B** | 起 xterm 时加 `-u8` 等开关 | 否：仍是外部 X 程序 + 仍然依赖它自己的解码策略；且要拉一头 Xvfb 才能复跑（复跑成本 ↑） |
| **C** | 截图也走 `gopi text2img`（`internal/text2img`） | **选**：零 CGO、零外部程序、**全程没有 locale 这个概念**——没有 locale 就不能误读字节；一图一制，截图与标注同源同字体链（Rule 35：先问"能不能在已有进程里解决"，答案是可以，且这就是已有能力） |

选 C 之后，**新增的常驻依赖为 0**：没有新脚本常驻、没有 cron/unit、没有新配置键。
新增的两个文件是**生成器本体**（`scripts/materials/`，与既有 `gen-taskA.sh`/`render.py` 同居一处，
属"脚本即为交付物"的先例），不是运行时依赖——复跑的输入是 `01-input.txt`，不是这两个脚本。

## 顺带修掉的第二个缺陷：标注在框整行

Rule 2「渲染真值」用得上的地方：**标注的判据是"去掉这张图读者还看不看得懂"**，
而"框住整行"恰恰是**没有说明力**的——一行本来就一行，框住它等于没框。
旧图即如此（`grep` 旧 `00-entry.md`：「框住的是 pending 视图的正文」= 整段）。

新的定位法：`needle`（词/字段）+ 字符偏移。
用 `PIL.ImageFont.getlength` 读出与 `text2img` **同源**的 TrueType advance（都是 `hmtx`），
算出该词在渲染行内的 x 起止，只框那个词。**17 条的框宽占图宽 5%–48%，没有一条接近整行**
（整行会被 `render-entry.py` 直接拒绝：`box_w >= 0.6*W` → exit 1）。

## 自证：三项像素检查，任一不过就不产出这张图

`gopi annotate` 的标签芯片画在框**上方** ~39px 内，如果标注行上方是空白就不挡；
如果上方 30px 处正好是上一行正文（长输出里每行都紧挨着），标签就会**盖住证据**——
一张"证明"图盖住它要证明的东西，是标注最恶的失败形态。`render-entry.py` 因此：

1. `ink_under_box > 0` —— 框里原本有墨迹（否则框是装饰，框错行了）；
2. `changed_pixels > 0` —— `annotate` 真的改了像素（不是空跑）；
3. `label_covered_ink == 0` —— **标签带内"原本有墨迹"且"被 annotate 改动"的像素必须为 0**。
   对策是折行后在标注行上方**插 3 个空行**（90px > 标签带），这条是实测逼出来的：
   早期版本用 `subprocess` 默认管道捕获 `02-output.txt`，`textwrap` 因此退化成按 70 列折行，
   标注行上方只剩 1 行空白 → vision 实测「红框压住文字尾巴」。**捕获 subprocess 输出会改变
   `textwrap` 的默认宽度**——这是个安静的行为耦合，值得记一笔。

17/17 实测：`ink 200–2218`、`changed 1201–3114`、`covered 0`。

## 尺寸：由内容决定，不裁不缩不设上限

- 只框**证据区**（`02-output.txt` 分隔行之后），图顶的解说句与命令原文不参与取景；
- 折行 96 列（runes），`text2img --cols 400` 只是"别再折"，`--lines 400` 远大于本批最长 26 行，
  **不会触发分页改名**（分页会产出 `-1.png` 后缀，静默换名 = 静默换交付物）；
- 实测图高 414–2604px，与输出行数同向（`10-sweep-worktrees` 26 行 → 2604px）。
  **没有设"图太大就报错"的自检**：图高与输出行数正相关，那种自检只会逼人删证据。

## 顺带把旧生成器 `scripts/materials/render.py` 的两个真缺陷修了

这两个缺陷是**量出来的**，不是看出来的。度量口径：`03-raw.png` 与 `04-annotated.png` 逐像素比，
数「原本是墨迹（`<128`）且被改动超过 30 灰度」的像素 = **被标注盖掉的证据**。

| | BEFORE（旧 xterm+capture 出图，`render.py` 旧行为） | AFTER（`text2img` + 修后的 `render.py`） |
|---|---|---|
| 17 条合计被盖掉的墨迹 | **5871 px** | **13 px**（且 13 个**全部落在框线±3px 内**＝框压字缘） |
| 单条最差 | `14-dedup-scan` 1617px、`10-sweep-worktrees` 1217px、`06-verify` 1047px | 最差 6px（`04-submit`） |
| 图宽 | 一律 1300（固定 `--area`，被裁） | 993–1069（内容决定） |

三处根因，逐个：

1. **框宽写死**：旧 `box_w = max(60, min(w - 20, 1080))`，`x=6` —— 不管标什么，都框满整行。
   现在 `box_w = w - 2*FULLROW_MARGIN`（`render.py` 框的本来就是**行**）与
   `render-entry.py` 的**词框**（5%–48% 宽）各司其职。
2. **静默 clamp 把框挪到别的行上**：旧 `y = max(0, min(…, h - (line_ink_h+6)))`，
   末几行越界时框被挤上去、压住上一行——**框对不上行 = 标注在指别处**。
   现在越界直接 `REFUSED` 并退出非零（实测复现：`末行是退出码` 那条曾被挤到别的行）。
3. **标签带没有净空**：标签芯片画在框**上方** ~39px 内，长输出每行紧挨着，
   于是标签必然压住上一行。现在：折行后标注行上方**补足 3 个空行**（90px），
   文末补 2 行（否则末行的框没有框高可用 → 走 2 的越界路径）。

新增闸（`render.py` 与 `render-entry.py` 都做，任一不过即 exit 1、不产出这张图）：
`ink_under_mark>0`（框里有墨迹）、`changed_pixels>0`（annotate 真改了像素）、
**`label_covered_ink==0`**（标签带内「原本有墨迹 ∩ 被改动」必须为 0）。

**另一处口径修正**：`--mark-line <N>` 的 `N` 旧口径是**图内绝对下标**，
输出行数一变就框错行；现在 `N` = **`02-output.txt` 内的第几行（1 起）**，
与条目卡上的行号一致（`picks = [(floor + N - 1, …)]`）。`--mark-match` 本来按内容找行，不变。

**影响（诚实写清）**：`gen-taskA.sh` 及其它 `render.py` 调用方**本次未重跑**——
它们产出的素材图不在本任务范围内（本任务只动 01–17 这 17 条，见 §范围）。
重跑它们会**更慢但更对**：新口径下若某条标注行上方没有空白，`render.py` 会**拒绝产出**
而不是默默盖住证据；拒绝是响的（Rule 15），但确实意味着重跑别批可能需要调 `--mark-match` 的目标。
另外 `TAIL_BLANK_LINES` 让行数 +2：正好卡在 `LINES_PER_PAGE`=200 边界上的别批渲染会多出一页
（`03-raw-1.png`），调用方的 `rm -f 03-raw-*.png` 已经会清掉它。

## 这 17 条到底跑了什么（不是纸面声明）

```
$ bash scripts/materials/gen-task17-CJK.sh --bin …/bin/gopi \
      --font /usr/share/fonts/truetype/wqy/wqy-zenhei.ttc --scratch <session>/mat
── gen-task17-CJK: 17/17 产出，failed: none
$ python3 <session>/mat/update-entries.py
ok: 17/17 条目卡已更新（生成器 + 截图表 sha + caption）
```

`02-output.txt` **未改动**（它本来就是好的，是证据不是产物）；旧图通过 git 历史保留，
没有删除文件；修前/修后对照见会话目录 `before/01-list-before-after.png`。

## 交付物声明

- 分支：`task/01a0ee8bb7ba75dfa8b06adc4a3f6449`（worktree `~/workspace/gopi-01a0ee8bb7ba75dfa8b06adc4a3f6449`）
- 生成器：`scripts/materials/gen-task17-CJK.sh`（编排 + 17 条的 needle/label/caption 表）
  + `scripts/materials/render-entry.py`（单条渲染 + 框词定位 + 三项自证）
- 取回：`git show task/01a0ee8bb7ba75dfa8b06adc4a3f6449:devdoc/materials-cjk-render-fix.md`
