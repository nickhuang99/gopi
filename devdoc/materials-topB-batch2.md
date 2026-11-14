<!-- doc-class: historical -->
# 素材库 A 面批 2：顶层命令四组的 26 条六件套（epic 第 8/12 轮）

**任务**：`01a0ed23154f763eb5ee36b3867ecaf7`　**分支**：`task/01a0ed23`
**测量 commit**：`1f87a99e`（binary 自建 `5b4d724341cffb53`，未改源码）

## Completed

- `internal/config/help.go` 里**此前没有条目的四组**（`:581` Chat & web / `:589` Tools /
  `:602` Memory & knowledge / `:607` Configuration & maintenance）逐个真跑，
  新增 **`115-…`–`140-…` 共 26 条**六件套（`00-entry.md` / `01-input.txt` / `02-output.txt` /
  `03-raw.png` / `04-annotated.png` / `05-finding.md`），全部 `annotation_required: yes`
  且标注图齐备（census `unreadable=0`）。
- **生成器拆成两件**（Rule 14）：`scripts/materials/gen-topB.sh` 只负责采集/渲染/断言
  （逐字复用批 1 的实现，机制只有一份），`scripts/materials/entries-topB.sh` 是本轮
  "采集了哪些条目"。换一批条目只改后者。
- **4 条真缝**（真跑挖到，非编造）：`140-pixel-evidence`（视觉模型在全黑画布上编出不存在的
  乐器）、`130-shot`（同一 argv 同一运行，一次成功一次失败——间歇性；且"空输出+124"
  是取证被截早）、`137-doccheck`（同一个坑的两种活法：拒绝里给出路 vs 只说"没找到"）、
  `118-ui_state`（"开过会话" ≠ "有 dump 可读"）。其余 22 条走诚实空。
- **两个口径坑**写进 `docs/archive/demo/materials/README.md §6.05.1`：
  (1) 从批 1 抄来的正则 `[a-zA-Z_-]+` 不含数字，把 `gopi text2img` 截成 `text`，
  **凭空造出一个"缺条目"的假缺口**；(2) 行数 37 ≠ 去重动词数 35。
  修口径后本面 **35/35 无缺口**。
- `docs/archive/demo/materials/COVERAGE.md` 与 `README.md §6.05/§6.05.1/§6.06` 按本轮现场读数更新。

## Files

- `scripts/materials/gen-topB.sh` — `finding()` 加护栏（标签必须对应真 `entry`，否则 FATAL+exit 1）；
  模板的真缝清单三条→四条。
- `scripts/materials/entries-topB.sh` — `finding 142-pixel-evidence` 改为 `finding 140-pixel-evidence`
  （正文一字未动）；两处交叉引用 `142 号 finding` → `140 号 finding`。
- `docs/archive/demo/materials/COVERAGE.md` — census 分子 118→117（含构成表与勘误说明）；142→140。
- `docs/archive/demo/materials/README.md` — §6.05 分子 118→117；142→140。
- `docs/archive/demo/materials/1[0-4][0-9]-*/00-entry.md`、`…/05-finding.md` — 指向 `142` 的引用改为 `140`；
  22 条默认 finding 的"真缝清单"由三条补为四条（补 `118-ui_state`）。
- `docs/archive/demo/materials/140-pixel-evidence/05-finding.md` — 由诚实空改为那段真缝正文（原本要写进 142 的内容）。
- `devdoc/materials-topB-batch2.md` — `## Not Done` 增第 0 条（本次自审推翻的三个说法）、
  `## Evidence` 增红/绿实测；census 数字更正。

## Not Done

0. **本 run 的独立复核推翻了自己上一轮的三个说法**（诚实登记，都是文档数字/标签，不涉及素材本身）：
   (a) `142-pixel-evidence` 这个标签**从来没有对应的 `entry`**（本批条目是 `115-140`，26 条），
   所以那段"vision 幻觉"真缝正文**从未落盘**——而 `COVERAGE.md` / `README.md` / 本报告都已把它
   当条目引用；本 run 把该正文并回它真正属于的 `140-pixel-evidence`（像素真值那条），
   **对外引用（`COVERAGE.md` / `README.md` / 全部条目正文与 finding）里 `142` 清零**；
   只有本报告与生成器注释保留 `142`，作为这次丢失的记录本身（记录不能被"清干净"）。
   (b) 根因是生成器的 `finding()` 用 `cat > "$OUTROOT/$1/05-finding.md"`：目录不存在时
   **只往 stderr 报一句、整批照常跑完并 rc=0**，正文进空气（Rule 15 静默失败）。已改成
   **标签必须对应一条真 `entry`，否则 FATAL + exit 1**；被 `GOPI_MATERIALS_ONLY` 过滤掉
   的合法标签则明确打印"跳过"（不是静默）。
   (c) `COVERAGE.md` 早先印的 census 分子 `118`（及为凑数写的「其它既有条目 1」）**是错的**：
   五个分组现场相加 `17+51+9+14+26 = 117`；`ae19dacb` 只增目录（`--diff-filter=D` 空），
   父提交 `1f87a99e` 是 91 个目录，`91+26 = 117`；生成器现在打印 117。已改文档
   （Rule 38：复不出的数字改文档，不改树——树里本来就没有第 118 个目录）。

1. `116-web`（124 截断）/`117-ui`（无 TTY 拒绝）记的是这两条命令的**非交互面**，
   不是"人在终端里用起来什么样"——那需要一个人坐在终端前。
2. `123-vision_analyze` 的输入是离屏 X11（`:77`）的截图，不是操作者桌面；
   **没有**测"真桌面上这条命令读得怎么样"。
3. `137-doccheck` 只证"命令在这两种 cwd 下各说什么"，不证"语料在别的 commit 上也有序"。
4. `133-brew` 的样本是 1 个会话 / 2 轮（`accepted=0 rejected=1` 是小样本产物），
   不构成"长会话下的蒸馏质量"。
5. `130-shot` 的间歇性**没有**去"修"（调 ulimit/chrome 参数会改变被记录的事实）；
   两边（130 失败 / 140 成功）都入档，由读者对照。
6. `124-text2img` 的字体链是**本机**这一串回落；换机器/换字体树，渲染内容会不同。

## Verification

- **Build**: `CGO_ENABLED=0 go build -o $EV/materials-topB/bin/gopi ./cmd/agent/`
  → exit 0，`v0.1.0 / go1.24.5 linux/amd64`，sha256 前 16 位 `5b4d724341cffb53`
  （未改源码，与 `1f87a99e` 树一致）。
- **Tests**: 六件套谓词由本 run 独立复算（非生成器自报）：
  `python3` 脚本逐目录检查「六件齐全且非空 / `02` 有 `^exit=[0-9]+` 行 / 两个 PNG 的
  magic 均为 `89504e47` / 两图 sha256 不同」→ **26/26 全绿，bad=0**。
  退出码分布（`grep -h '^exit=' docs/archive/demo/materials/1[1-4][0-9]-*/02-output.txt | sort | uniq -c`）：
  `20 exit=0 / 7 exit=1 / 1 exit=124`。
- **Ran**（逐字，本轮的两个入口）:
  - `bash scripts/materials/gen-topB.sh` → `GENRC=0`（整批；会话日志 `run6.log` + 单条补跑 `130-shot`）
  - `GOPI_MATERIALS_ONLY="116-web 123-vision_analyze 137-doccheck 140-pixel-evidence" bash scripts/materials/gen-topB.sh`
  - `bash scripts/materials-coverage.sh`
- **Result**:
  - census 现场读数：`produced 117 / 193`、`candidate dirs 117 (complete=117 incomplete=0)`、
    `unreadable 0`、`measured commit 1f87a99e+dirty`（`+dirty` 是本文件自身未提交，照实写）。
    **本 run 更正**：本报告与 `COVERAGE.md` 早先写的 `118`（以及 `COVERAGE.md` 里凑数用的
    「其它既有条目 1」）是**错的**。判据：`ae19dacb` 只增目录（`--diff-filter=D` 输出 0 行），
    父提交 `1f87a99e` 有 91 个目录，`91+26 = 117`；五个分组现场相加 `17+51+9+14+26 = 117`；
    生成器现在也打印 117。复不出来的数字按 Rule 38 改文档、不改树（也没有目录可删：
    118 从来就不存在，是当时把读数写大了）。
  - 逐命令读数：`-h` Commands 段 37 行 = 去重 35 个动词；**35/35 有條目，本面无缺口**；
    批 2 覆盖 29 个动词、其中 18 个此前无条目。
  - 单条抽查（`130-shot`）：`02-output.txt` 逐字为
    `chrome did not report a DevTools port within 15s …` / `gopi shot: shot: chrome exited
    without printing a DevTools port (stderr consumed)` / `exit=1`；
    同批 `140` 的 `p5.png` 为 1440x2800（PNG 头解析）——正反两例同在库里。
- **Commit**: 见本分支 HEAD（报告随素材同一次提交）。

## Evidence (self-filed)

- **本次修正的红/绿实测**（Rule 15 的护栏本身）：
  - 红（旧实现 `finding() { cat > "$OUTROOT/$1/05-finding.md"; }`，`set -uo pipefail` 无 `-e`）：
    `finding 142-pixel-evidence` → stderr `…/142-pixel-evidence/05-finding.md: No such file or
    directory`，**批次继续执行、最终 rc=0**，磁盘上没有该目录 → 即本批丢稿的机制。
  - 绿（现实现）：同一标签 → `FATAL: finding '142-pixel-evidence' 在 …entries-topB.sh 里没有
    对应的 entry —— 标签写错了。` + **exit 1**；合法标签 `140-pixel-evidence` → rc=0 且写入；
    合法但本轮被过滤的 `139-sandbox` → `note: 跳过（…GOPI_MATERIALS_ONLY 过滤…）` + rc=0，
    且**不**创建目录。
  - 幂等：用生成器模板重渲 `119-web-search/05-finding.md` 与落盘文件 `diff` → 无差异。
- 六件套复算：`python3` 独立脚本输出 `TOTAL dirs=26 ok=26 bad=0`（本轮）；全库
  `candidate dirs 117 (complete=117 incomplete=0)` 由 `scripts/materials-coverage.sh` 现场打印。
- `130-shot` 的失败与 `140` 的成功：`130/02-output.txt` 末行 `exit=1`；
  `140/02-output.txt` 第 5 行 `PNG …/p5.png 1440x2800`——两条都在本轮产物里，可逐字复核。
- 视觉幻觉那条：`140` 的 `05-finding.md` 记了两次 `vision_analyze` 的逐字回答
  （有墨迹的 `term.png` → 读出三行文字；全黑 `blank79.png` → 描述出一件不存在的乐器），
  两次 `exit=0`；两张输入的暗像素数（843 / 153600）由本 run 用 PIL 现量。
- 口径坑：修正前后的两条命令逐字并列写在 `README §6.05.1`；
  修正前输出为 `text`（截断），修正后为 `text2img`。
- 台账隔离：生成器开头逐字打印 `operator_ledger_sha256_16=`（跑前）；
  它**变化过**——宿主 chat 会话（本 run 自己）会写自己的 ledger 行，生成器只记录该事实
  并标注为非违规证据，不拿它当"没碰台账"的证明（Rule 15）。
