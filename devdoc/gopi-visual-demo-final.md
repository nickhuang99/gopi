<!-- doc-class: historical -->
# docs/archive/demo/final —— 给操作者拿去看的 gopi 可视化 demo


**这一页是什么。** `docs/archive/demo/final/index.html` 是一个**离线可开的单文件 HTML**：
它在一页里回答「gopi 是什么、有什么、比别家好在哪、哪里还差」。旁边三份机器可核的伴随文件：
`build.sh`（唯一的生成器）、`competitive.md`（四家一手源码对比）、
`coverage-summary.tsv` + `coverage.tsv`（覆盖率仪器的产出，带测量 commit）、
`materials.tsv`（每个功能点 → 素材条目 → 截图路径 + 字节数 + sha256 前 16 位）。
`render-proof.png` 是本页**自己**被 `gopi shot` 渲染出来的整页图 —— 像素级证明它是「可开且图文并茂」的。

## 1. 为什么是 HTML 单页，不是 GIF（形态论证）

操作者接受两种形态（图文并茂 HTML / 带指示的 GIF）。选 HTML 的理由，按权重：

1. **零依赖可交付（Rule 35）。** HTML + 相对路径引图 = 一个目录，拷走就能看。
   GIF 要自己写编码器（Go 标准库无 GIF 动画写入口的现成链路）或引 `image/gif` 之外的依赖，
   还要为每一帧生成图片 —— 为一个「给人看的东西」新增持久化工具链，正是 Rule 35 要拒绝的方向。
2. **可核性。** HTML 里的图是**素材库自己的文件**（相对路径引用，不复制）——
   读者能把 `index.html` 的每一个 `<img>` 对着素材库点开，逐条核对。
   GIF 把图**重新编码**过，核对要回到源头，页面上看到的已经不是原件。
3. **可重跑。** `bash docs/archive/demo/final/build.sh` 一条命令重建全部产物（含像素自证）。
   GIF 的每一帧都是手工排布，改一个字要重排。
4. **文本可被工具读。** 覆盖率、竞品判定、file:line 都是文本，能 grep、能 diff、能被别的任务复用。

GIF 的**唯一**优势是「不需要打开浏览器」——而操作者的场景是「拿去给人看」，
打开一个本地 HTML 比播放一个 GIF 更常见，且 GIF 会丢掉可核性（第 2 条）。

## 2. 素材的引用方式：不复制（一个刻意的决定）

页面用**相对路径**引用 `docs/archive/demo/materials/<entry>/04-annotated.png`，**不复制字节**。
理由：素材库的价值是「这条记录的字节就是那次运行的原始 bytes」（`00-entry.md` 记着
`measured_commit` 与生成器）。复制会在 `final/` 下造出第二份字节，
一旦有人"顺手优化"了其中一份，两份的 sha256 就不再是同一个东西 ——
`docs/archive/demo/showcase/materials-cut/selection.tsv` 用三个 sha256 锚住素材，正是为了防这件事。

代价（写清楚，不藏）：`final/` 目录**单独拷走图会断**。所以：
- 页面顶部写明「把 `docs/archive/demo/` 整个拷走就能看」；
- `build.sh` 每次运行前**逐张检查**被引用的截图存在，缺一张就带路径中止（不是跳过）。

## 3. 选材：避开 D 面 7 条与 CJK 乱码 17 条（逐条核对，不是记忆）

| 组 | 状态（本次核对） | 处置 |
|---|---|---|
| **D 面 7 条**（001-sandbox-create / 002-checkpoint-list / 003-checkpoint-export / 004-checkpoint-resume / 005-sandbox-show / 006-sandbox-run / 007-sandbox-unknown） | **未修**。`git log -1 -- …/001-sandbox-create/04-annotated.png` → `955706a1`（2026-09-29 的 D 面产出提交），**没有任何后续修复提交**；生成器 `scripts/materials/gen-taskD.sh` 里 `grep -c text2img` = **0**，即它仍走旧的 xterm 截图管路 —— 与 203e8a40 修 CJK 时指出的缺陷同源（该提交只动了那 17 条）。 | **全部排除**：12 条选中素材里，这 7 条**一条都没有**（`build.sh` 每次运行都断言，命中即退出非零）。 |
| **CJK 乱码 17 条**（01-list … 17-reopen） | **已修**（任务 `01a0ee8b` completed，提交 `203e8a40` "fix(materials): 素材截图走 text2img —— 中文不再是 Latin-1 乱码"）。核对输出：`01-list/04-annotated.png` 当前 `134175` 字节，而 `203e8a40` 的 stat 行写着 `43037 -> 134175` —— **当前字节 == 修复提交写入的字节**，说明工作区里的就是修复后的图；生成器 `scripts/materials/gen-task17-CJK.sh` 里 `gopi text2img` 出现 1 次（旧路 `gopi capture` 也已不在该脚本的截图路径上）。 | 即便已修，**本次也不选**（口径：修好了仍然不碰，避免把"刚改过的图"当作稳定证据）。 |

`build.sh` 把这两条核对**印在每次运行的输出里**（含 CJK 修复提交的 hash 与标题），
所以「避开了哪 7+17 条」是一个每次构建都重新测过的结论，不是本文件里的一句承诺。

### 3.1 一处漏网，与补上的那道闸（本文档第一版写错了）

第一版的本节写的是「全部排除」，**而那是错的**：被排除的只是**截图表**（`materials.tsv` 的
entry 列），页面 §3 的**出处行**另走一路 —— 缝 4 当时引的是
`docs/archive/demo/materials/007-sandbox-unknown/05-finding.md`，即 **D 面 7 条之一**。
第一版的检查只读了截图列，于是「12 张图里没有 D 面」被写成了「页面不涉及 D 面」——
**这两句话不是同一句**，而差值恰好是当时唯一没测过的那条路径。

- **怎么发现的**：不是靠回忆，是靠把已发出的页面按目录名逐条 `grep` 一遍
  （`for d in $D7; do grep -c "materials/$d" index.html; done` → `007-sandbox-unknown` = 1，别处 0）。
- **怎么补的**：① 缝 4 换成同一批的 `130-shot/05-finding.md`（`timeout` 截断把观测者的动作
  混进被观测者的读数——与缝 3 同族，且该素材本就在引用列表里）；② `build.sh` 新增
  `assert_no_banned_in_html()`，在页面**落盘之后**对**整份 `index.html`**（不只截图列）断言
  D7+J17 一个都不出现，命中即 `exit 1`；③ 每次构建把它当独立一行打印
  （`banned-material check (page-wide) : clean`），使这条结论**每次重建都被重测**。
- **残留的诚实边界**：闸门只覆盖 `index.html`。`competitive.md` 等三份伴随文件已人工逐条核对
  （`banned-dir hits=0`），但那是**一次核对**，不是每次构建的断言 —— 谁往 `competitive.md`
  里添一条 D 面引用，`build.sh` 不会拦。要补齐得把同一断言扩到那几份文件上。

## 4. 覆盖率：测量本身要能重跑

覆盖率来自 `scripts/demo-coverage.sh`（仓库既有的仪器，本任务只**重跑**它，不修改它），
产出落在 `docs/archive/demo/final/coverage.tsv`，首部带 `# generator:` 与 `# measured_commit:`
（Rule 38 的两个要素由脚本自己写，不是人手抄）：

```
# generator: scripts/demo-coverage.sh
# measured_commit: 203e8a40
# measured_at: 2026-09-30 03:42:35 +0800
# top_level_advertised_verb_lines: 37
rows: top=37 sub=41 total=78   ran=42 refused=10 not-run=26
```

**对外页面引的是 `coverage-summary.tsv`**，不是原始 `coverage.tsv`：原始文件里每一行都带
探针的**完整 argv**（内含本次运行的私有 profile 绝对路径），那是给我们留的证据，
不是给外人看的路径。摘要保留**判定**（`ran|refused|not-run` + 真实 exit code），去掉 argv ——
两个文件都在同一个目录里，取摘要的行数/状态分布与原始逐行一致。

**一个诚实的读数**：仪器自己的输出里，`ran` 含两个 `127`（`worker` / `merge` 两个探针在
某些环境下 argv 组装成了 `worker: command not found`）与 4 处 `UnicodeDecodeError`
（探针输出里有一段非 UTF-8 字节让 awk/python 解析器报错）。这些是**仪器**的噪声，
不是 gopi 的失败；本任务按 Rule 35「不动既有仪器」的口径没有去改它，如实记录在此 ——
要修是另一个任务（`scripts/demo-coverage.sh` 的探针 argv 组装与解码容错）。

## 5. 竞品对比：一手到什么程度，以及 claude 为什么是「未判」

- **codex / dsh / hermes**：本机有可读源码树，每一格来自直接读取（`grep -n` / `head`），
  行号与该仓库的 rev 一并给出，`competitive.md` 里附了全部 grep 命令（可复跑）。判定口径
  `{赢,输,平,未判}`：**赢 = 竞品更强**，**输 = gopi 更强**，**未判 = 本次材料里查不到**。
- **claude**：**全部维度未判**，理由是**没有可读源码**，不是「查了没有」：
  `~/.local/bin/claude` → `~/.local/share/claude/versions/2.1.204`（245 MB 打包 ELF，无源码树）；
  `~/work/claude` 是「用 Claude 做研究」的产物仓库（顶层 tracked 是 `dialogs/`、`.ollama-runner/`、
  `memory/`，只 12 个 tracked `*.py`），**不是 Claude Code 的实现**；
  同机 `Downloads/claude-code-system-prompts`（690 份 prompt 文本）**是提示词不是实现**，
  因此**不作为判定依据**，只作为后续任务的线索写进页面。
  **口径的价值就在这一格**：把「查不到」写成「未判」而不是留空或猜一个平局，
  读者才知道哪一格是空的。

## 6. 已知边界（页面 §6 也写着，此处给实现侧的理由）

1. **素材图内含内部标识**（沙盒路径、任务 id 形状）。素材库只读，本任务**不裁切/遮挡**。
   本页**正文**不含绝对路径/会话 id —— `build.sh` 每次构建做 4 项字符串检查并打印结论
   （`/home/`、`sessions/`、`sandbox-materials`、`01a0e`；这项检查抓到过一处真实泄漏：
   自证段落里把本机绝对路径写进了页面，随后改为 `<repo>` 占位）。
2. **「有吸引力」是选材结果**：素材库 159 条有 finding 的条目，本页引 12 条；
   挑选判据与被排除原因在 `docs/archive/demo/showcase/materials-cut/selection.tsv`（既有产物，未改）。
3. **一行证据做了占位**：页面里每条功能点的「真实输出一行」把 id/路径写成 `<…>` ——
   形状不变、标识不复述（对外页面不含会话 id 的口径）。
4. **本页是脚本产物**：改内容改 `build.sh`。

## 6a. 页面里没有任何「构建时刻」——这条是修出来的，不是设计出来的

**症状（交付后复跑才看见，不是推理出来的）**：重跑 `bash docs/archive/demo/final/build.sh --shot` 之后，
页内写着 `PNG bytes 656527`、`sha256_16 623c8fb379654cd8`，而磁盘上的 `render-proof.png`
已经是 **654034 字节**。同一份交付里，数字与它描述的文件差一代。

**5-why**：① 数字不一致 ← ② 页面把它们**印进了自己** ← ③ 而它们度量的是「本页被渲染出来的产物」
← ④ 渲染发生在构建中、写在页面里之后 ← ⑤ **自指，没有不动点**：印进去就改了页面，
改了页面就改了下一张图。第二层症状同源：页面还印着构建时间戳 / 仓库 HEAD / 工作区状态，
于是**每次构建都重写 `index.html`**——「重跑后 `git status` 干净」「重跑后 diff 为空」这两件事
在那个形态下**永远不可能成立**，而它们正是 Rule 30 与 Rule 31 要的性质。

**修法（根因，不是调参）**：页面变成 **生成器 + 只读素材库的纯函数** ——
① 页内不再有任何「现在」：无时间戳、无 HEAD、无工作区状态、无自证图的字节/尺寸/sha；
② 自证段落只声明 **renderer** 与「见构建日志那一行」，把度量留在日志里（度量属于日志，不属于被度量的工件）；
③ 「这一页是哪次测的」由 `coverage.tsv` 自己的首部回答（`# measured_commit: 203e8a40`，
仪器写的，页面同源引用），不由页面自述；
④ 新增 `assert_page_is_timeless()`：页面落盘后断言 `仓库 HEAD / 工作区 clean|dirty / sha256_16 /
PNG bytes / PNG WxH` 六个模式**零出现**，命中 `exit 1`。承诺变成每次构建都跑的检查（Rule 13 的精神）。

**实测结果**：同生成器连续两次 `--shot`，三个产物 `index.html` / `materials.tsv` /
`render-proof.png` **逐字节相同**（`diff` 空、`cmp` 通过）；构建日志逐次打印
`timeless-page check ... : clean` 与 `render proof : render-proof.png 628956 bytes`。

**顺带修掉一个真实 off-by-one**：`materials.tsv` 表头从 6 行改成 5 行后，
旧脚本里按行号切数据的 `awk 'NR>6'` 会**吞掉一行**（`USED` 显示 11 而不是 12），
日志里那句 `material screenshots 11+1` 与页面 `13` 张图互相矛盾。两处都改成按列判据
（`$1!="#" && $1!="feature"`），不再依赖表头行数；`SECT` 这个数则直接不再打印
（它只是输入的行数，不是产物的度量——Rule 31：记在日志上的数字必须是**测出来的**）。

## 7. 复跑（一条命令）

```bash
# 覆盖率（仓库既有仪器，重建 coverage.tsv，写它自己算出的 measured commit）
GOPI_DEMO_OUT=$PWD/docs/archive/demo/final/coverage.tsv bash scripts/demo-coverage.sh
# 页面 + 三份伴随文件（+ --shot 时连像素自证图一起重建）
bash docs/archive/demo/final/build.sh --shot
```
