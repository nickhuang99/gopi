<!-- doc-class: historical -->

# competitor-analysis 阶段 4 — 汇总：17 维 × 24 列的对比矩阵、差距判定与补齐 ROI

> **一句话**：在 **23 家清单**里，本次运行时**实有采集文档 18 家**、**5 家 0 份**；
> **阶段 3 的 23 份 `gap-*.md` 实测 0 份**（23 条子行全部 `pending`），
> 因此**本文的矩阵是从阶段 1/2 的采集文档直接推出的，不是读阶段 3 的差距表汇总的**。
> 每一格的出处是本文件自己的行号，不是 gap 文件（它不存在）。
> 四张差距表的**判定**部分标注为「未经阶段 3 独立复核」—— 阶段 3 没跑过，也就没有复核。

---

## 0. 前置条件实测（这是本文最先要说的事）

任务 briefing 写着「本任务必须在阶段 3 之后释放。开工前先读逐家对比表：
`devdoc/competitor/gap-<agent-id>.md`（阶段 3 产出）」。**实测：那个产物不存在。**

```
$ cd /home/user/workspace/01a1aa6d-5210-714f-bb61-c5b65cae01bc
$ ls devdoc/competitor/gap-*.md 2>/dev/null | wc -l
0

$ cd /home/user/work/gopi && ls devdoc/competitor/gap-*.md 2>/dev/null | wc -l
0

# 全主机搜索（workspace 与 work 两层下）
$ find /home/user/workspace /home/user/work -maxdepth 4 -name 'gap-*.md' 2>/dev/null | head -40
（无输出）

# 阶段 3 的 23 条子行状态
$ gopi task list --status pending | grep -c '01a1acc7'
23

# 阶段 3 父行本身：completed，但它只是一个"批量派发器"
$ gopi task show 01a1aa6d-3309-7734-a55f-725d3ee87138 | grep -E '^status:|^summary:'
status:         completed
summary:        competitor-analysis 阶段3：17 家 agent 逐个出对比表（谁强谁弱）

# 阶段 1C（5 家产品型/GUI/平台型）也仍未跑
$ gopi task show 01a1aa6c-c4c5-7bf4-8dcd-831c6a5bc904 | grep -E '^status:|^summary:'
status:         pending
summary:        competitor-analysis 阶段1C：采集 5 家产品型/GUI/平台型 agent 功能面（LobeHub/CherryStudio/Qoder/Copilot/WorkBuddy）

# 全部 refs 上从未出现 gap-*.md
$ git log --all --oneline --diff-filter=A -- 'devdoc/competitor/gap-*'
（无输出）
```

**结论（实测，不是推断）**：

| 事实 | 读数 |
|---|---|
| 阶段 3 的逐家差距表 | **0 / 23** |
| 阶段 3 的 23 条子行 | 23 条全部 `pending`（不是被父门挡住，是排队没轮到） |
| 阶段 1C 的 5 家采集 | 父行 `pending`，采集文档 **0 / 5** |
| 阶段 1A/1B 的 18 家采集 | **18 / 18 存在**，每份恰好 17 行判定 |
| 阶段 2A/2B/2C 的我方盘点 | **3 / 3 存在** |

**为什么父门没拦住本任务（这是口径事实，不是 bug 陈述）**：Rule 33 的父门判的是
**父行的 status**，而阶段 3 的父行是一个派发器 —— 它派完 23 条就 `completed` 了，
真正干活的是那 23 条子行。于是「父行 completed」与「前置工作做完」在这次恰好背离。
判前置是否满足，要看**产物的存在性**，不是父行状态：

```bash
ls devdoc/competitor/gap-*.md 2>/dev/null | wc -l    # 现在 0，应为 23
```

**本文因此选择的自曝口径**：不假装读过 gap 表；直接从阶段 1/2 的采集文档推矩阵；
所有「是真差距还是路线选择」的判定标注为**未经阶段 3 独立复核**。

---

## 1. 口径与可复算性

### 1.1 数据来源（哪些文件参与，哪些不参与）

| 来源 | 份数 | 在本文里的角色 |
|---|---|---|
| `devdoc/competitor/<name>.md`（阶段 1A/1B） | 18 | **矩阵里 18 列的全部格值 + 行号** |
| `devdoc/competitor/gopi-self-a.md`（阶段 2A） | 1 | 我方 D01/D02/D04/D05/D06/D08/D13/D14 的判定与出处 |
| `devdoc/competitor/gopi-self-b.md`（阶段 2B） | 1 | 我方 D16/D17 |
| `devdoc/competitor/gopi-self-c.md`（阶段 2C） | 1 | 我方 D03/D09/D10/D11/D12 |
| `devdoc/competitor-analysis-framework.md`（阶段 0） | 1 | 17 维定义、四级口径、证据通道、我方探针命令表 |
| `devdoc/competitor/gap-*.md`（阶段 3） | **0** | **不存在**（§0） |
| 本 run 的 E3 实跑存档 | 见 §8 | 我方 D15（三份盘点都没判过这一维）+ D03/D09/D11/D12 的独立复验 |

### 1.2 一个必须先换算的编号陷阱（2A 用的是旧 16 维编号）

阶段 0 的 `dims.txt` 只有 **16 行**（`wc -l` = 16）。框架后来扩到 **17 维**，
并把 `D07` 之后**整体重排**。两份编号**同名不同义**：

| 旧编号（`dims.txt`，2A 在用） | 旧名 | 框架编号（17 维） | 框架名 |
|---|---|---|---|
| D07 | audit | **D13** | 可审计痕迹 |
| D08 | mcp | **D07** | 外部能力扩展 |
| D09 | approval | **D08** | 命令审批门 |
| D10 | sandbox | **D09** | 执行隔离 |
| D11 | image | **D10** | 图像输入 |
| D12 | guiauto | **D11** | 屏幕驱动 |
| D13 | server | **D12** | 本地服务面 |
| D14 | extend | **D07**（与旧 D08 mcp 同族） | 外部能力扩展 |
| D15 | cost | **D14** | 计量面 |
| D16 | orchestrate | **D17** | 任务编排 |

2A 自己在 `gopi-self-a.md:285` 就写了这件事：

```
| D17 | **未定义** | 阶段 0 的 `dims.txt` 只有 D01–D16，共 16 行（`wc -l` 实测 16）。本片在 §2 只表 10 维，剩余 6 维按上表分流，**D17 无人认领** —— 这是阶段 0 的口径缺口，不是本片漏做 |
```

**本文 §2 的 gopi 列已按右列换算**，不是照抄 2A 的编号。**这是本阶段最容易出的一类错**：
把 2A 的 `D08 mcp = none` 直接抄进框架的 `D08 命令审批门`，会把一个 `full` 记成 `none`。

### 1.3 判定口径（框架 §2，四级）

- `full`(2)：本 run 亲自执行过一条命令，`exit=0` 且输出含该维「full 证据串」。
- `partial`(1)：无 E3，但有 ≥1 条 E1（文档命中）或 E2（`repo@sha:path:line`）证据。
- `none`(0)：E3 命令 `exit≠0` 且命中拒绝串，或 E1+E2 探针在覆盖该维的语料上**双零**。
- `unknown`：E1/E2/E3 三通道都没被执行过，且必须写三种合法原因之一（框架 §4.4）。

**两条结构性口径，直接决定了四张表的形状：**

1. **对方（18 家）的上限是 `partial`** —— 框架 §1 明确「不得为了取证去安装/运行任意第三方 agent」，
   所以对方**结构性拿不到 `full`**。实测印证：18 份采集文档共 306 格，**`full` = 0 格**。
2. **我方 17 维全部有 E3**（本 run 逐条实跑，见 §8）。于是框架 §2 要求的
   `score_gopivs`（只在同为 E3 的维度上比）对**每一家对手都是 0** —— 这个数不存在，
   不是因为对手弱，是因为**双方从未在同一证据通道上见过面**。
   **所以「等级」不能直接跨方比较**；本文的比较分两层写：跨对手比（同通道，可信）
   与 我方 E3 vs 对方静态证据（只能作方向提示）。

### 1.4 计数与可复算（每个数字的生成器）

```bash
# 分母：18 份采集文档 × 17 维
$ echo $((18*17))
306

# 18 家的文件清单（把本汇总文件和 2A/2B/2C 三份盘点都排除 —— 见下面的自污染陷阱）
$ CORPUS=$(ls devdoc/competitor/*.md | grep -v -e 'gopi-self' -e 'summary')
$ echo "$CORPUS" | wc -l
18

# 四档计数
$ for lv in full partial none unknown; do printf '%-8s ' "$lv"; \
    grep -hc "| $lv |" $CORPUS | awk '{s+=$1} END{printf "%d\n", s}'; done
full     0
partial  217
none     18
unknown  71

# 每份必须是恰好 17 行判定（框架 §7.2 完成判据）
$ for f in $CORPUS; do printf '%-16s %s\n' "$(basename $f .md)" "$(grep -cE '^\| D[0-9]{2} \|' $f)"; done
astrbot          17
claude-code      17
cline            17
codex            17
copilot-cli      17
crush            17
deep-code        17
deepseek-tui     17
hermes           17
kilo-code        17
langcli          17
nanobot          17
oh-my-pi         17
openclaw         17
opencode         17
pi               17
qwen-code        17
reasonix         17
```

**⚠️ 一个实测踩到的自污染陷阱（复算时必须先读这一条）**：本文一旦落盘，它自己就
落在 `devdoc/competitor/` 里，于是 `ls devdoc/competitor/*.md` 会把它也算进去。实测：

```
# 本文写之前
full 0   partial 217   none 18   unknown 71

# 本文落盘之后，同一条命令（只排除 gopi-self）：
full 1   partial 218   none 19   unknown 72        ← 四个数全都 +1
$ grep -cE '^\| D[0-9]{2} \|' devdoc/competitor/summary.md
27                                                 ← 本该"每份 17 行"的检查把它算成 27 行
```

两处污染源都在本文内部：① `§1.4` 与 `## Evidence` 里**逐字引用**了
`grep "| unknown |"` 这条命令，那行文字本身就带 `| unknown |` 字面量；
② `§1.2` 的编号对照表用了 `| D07 | audit | **D13** | 可审计痕迹 |` 这种形状，命中了「17 行判定」的正则。

⇒ **所以本文所有计数命令都必须排除自身**（`grep -v -e 'gopi-self' -e 'summary'`），
上面已改好。若复算时看到 `full=1 / unknown=72`，那不是数据变了，
是**你把本文也算成了一份采集文档**。
（同族教训：Rule 42 的门禁曾因 `git ls-files` 在自身文件 untracked 时扫不到自己而空转——
「A SECOND vacuum」；一个扫描器必须能看见自己。）

**unknown 比例（本文核心可信度数字）：71 / 306 = 23.20%**。
`unknown` 的原因分布（框架 §4.4 的三种合法取值，逐条原文在 18 份采集文档的第 5 列）：

```
# unknown 的原因分布（一条可同时命中多种；口径见框架 §4.4 三种合法原因）
未运行（成本/权限）: 71
语料无此话题: 47

# 逐家 unknown 计数
copilot-cli: 16
kilo-code: 11
claude-code: 9
opencode: 8
crush: 6
pi: 6
cline: 5
codex: 5
oh-my-pi: 3
qwen-code: 2

合计 unknown = 71  分母 = 18 x 17 = 306
```

（一条备注可同时命中多个原因，故两类之和 > 71。）

**矩阵生成器**（本文 §2 由它产出，可重跑）：`artifacts/gen-summary-matrix.py`
（本 run session 目录 `/home/user/.gopi/sessions/01a1aa6d-5210-714f-bb61-c5b65cae01bc/artifacts/`）。

```bash
$ python3 artifacts/gen-summary-matrix.py . > artifacts/summary-matrix.md
cells=408 none=19 partial=218 unknown=71 absent=85 full=15
contest cells (18 real x 17)=306; unknown=71; unknown_ratio=71/306=23.20%
```

注意 `cells=408` 与 `306` 的差：408 = **24 列 × 17 维**（含 gopi 一列与 5 家 absent）；
`none=19`/`partial=218`/`full=15` 是 408 格的口径（gopi 列贡献 15 个 full、1 个 partial、1 个 none），
`unknown=71` 恰好只有对手列贡献 —— 两种口径分开报，避免把 gopi 的 full 数进「对手有 full」。

---

## 2. 17 维 × 24 列全局矩阵

格值 = `<等级>(<出处>)`。出处四种：

- `L<n>` —— `devdoc/competitor/<列名>.md` 的第 n 行（该行即该维的证据与备注行）；
- `a:<n>` / `b:<n>` / `c:<n>` —— `devdoc/competitor/gopi-self-{a,b,c}.md` 的第 n 行；
- `fw:<n>` —— `devdoc/competitor-analysis-framework.md` 的第 n 行（该行是判定本身）；
- `run` —— 阶段 2A/2B/2C 都没判过这一维，本 run 自跑 E3，存档见 §8 的 `e3-D15-gopi.txt`；
- `absent` —— 该家**采集文档 0 份**（阶段 1C 未跑），**不是 `unknown`**：
  `unknown` 是「查了没查到」，`absent` 是「这一家根本没进入采集」，两者不可混。

| 维度 | gopi | astrbot | claude-code | cline | codex | copilot-cli | crush | deep-code | deepseek-tui | hermes | kilo-code | langcli | nanobot | oh-my-pi | openclaw | opencode | pi | qwen-code | reasonix | cherry-studio | github-copilot | lobehub | qoder | workbuddy | full | partial | none | unknown/absent |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| **D01** | none(a:136) | partial(L20) | unknown(L21) | none(L20) | partial(L21) | unknown(L21) | unknown(L21) | partial(L18) | partial(L18) | partial(L18) | unknown(L20) | partial(L17) | partial(L18) | none(L21) | partial(L19) | none(L21) | unknown(L21) | partial(L21) | partial(L19) | absent | absent | absent | absent | absent | 0 | 10 | 4 | 10 |
| **D02** | full(a:137) | partial(L21) | partial(L22) | partial(L21) | partial(L22) | unknown(L22) | partial(L22) | partial(L19) | partial(L19) | partial(L19) | partial(L21) | partial(L18) | partial(L19) | partial(L22) | partial(L20) | partial(L22) | partial(L22) | partial(L22) | partial(L20) | absent | absent | absent | absent | absent | 1 | 17 | 0 | 6 |
| **D03** | full(c:305) | partial(L22) | unknown(L23) | partial(L22) | partial(L23) | unknown(L23) | partial(L23) | partial(L20) | partial(L20) | partial(L20) | partial(L22) | partial(L19) | partial(L20) | partial(L23) | partial(L21) | partial(L23) | partial(L23) | partial(L23) | partial(L21) | absent | absent | absent | absent | absent | 1 | 16 | 0 | 7 |
| **D04** | full(a:138) | partial(L23) | partial(L24) | partial(L23) | partial(L24) | unknown(L24) | partial(L24) | partial(L21) | partial(L21) | partial(L21) | partial(L23) | partial(L20) | partial(L21) | partial(L24) | partial(L22) | partial(L24) | partial(L24) | partial(L24) | partial(L22) | absent | absent | absent | absent | absent | 1 | 17 | 0 | 6 |
| **D05** | full(a:139) | partial(L24) | unknown(L25) | partial(L24) | partial(L25) | unknown(L25) | partial(L25) | partial(L22) | partial(L22) | partial(L22) | unknown(L24) | partial(L21) | partial(L22) | partial(L25) | partial(L23) | unknown(L25) | unknown(L25) | partial(L25) | partial(L23) | absent | absent | absent | absent | absent | 1 | 13 | 0 | 10 |
| **D06** | full(a:140) | partial(L25) | unknown(L26) | unknown(L25) | unknown(L26) | unknown(L26) | none(L26) | partial(L23) | partial(L23) | partial(L23) | unknown(L25) | partial(L22) | partial(L23) | partial(L26) | partial(L24) | none(L26) | none(L26) | partial(L26) | partial(L24) | absent | absent | absent | absent | absent | 1 | 10 | 3 | 10 |
| **D07** | partial(fw:408) | partial(L26) | partial(L27) | partial(L26) | partial(L27) | unknown(L27) | partial(L27) | partial(L24) | partial(L24) | partial(L24) | partial(L26) | partial(L23) | partial(L24) | partial(L27) | partial(L25) | partial(L27) | partial(L27) | partial(L27) | partial(L25) | absent | absent | absent | absent | absent | 0 | 18 | 0 | 6 |
| **D08** | full(a:143) | partial(L27) | unknown(L28) | unknown(L27) | partial(L28) | unknown(L28) | unknown(L28) | none(L25) | partial(L25) | partial(L25) | unknown(L27) | partial(L24) | partial(L25) | partial(L28) | partial(L26) | unknown(L28) | unknown(L28) | partial(L28) | partial(L26) | absent | absent | absent | absent | absent | 1 | 10 | 1 | 12 |
| **D09** | full(c:724) | partial(L28) | partial(L29) | unknown(L28) | partial(L29) | partial(L29) | unknown(L29) | none(L26) | partial(L26) | partial(L26) | unknown(L28) | partial(L25) | partial(L26) | unknown(L29) | partial(L27) | unknown(L29) | unknown(L29) | partial(L29) | partial(L27) | absent | absent | absent | absent | absent | 1 | 11 | 1 | 11 |
| **D10** | full(c:570) | partial(L29) | partial(L30) | partial(L29) | partial(L30) | unknown(L30) | unknown(L30) | partial(L27) | partial(L27) | partial(L27) | partial(L29) | partial(L26) | partial(L27) | unknown(L30) | partial(L28) | partial(L30) | partial(L30) | partial(L30) | partial(L28) | absent | absent | absent | absent | absent | 1 | 15 | 0 | 8 |
| **D11** | full(c:487) | partial(L30) | partial(L31) | partial(L30) | unknown(L31) | unknown(L31) | unknown(L31) | none(L28) | none(L28) | none(L28) | unknown(L30) | partial(L27) | none(L28) | partial(L31) | partial(L29) | unknown(L31) | partial(L31) | partial(L31) | none(L29) | absent | absent | absent | absent | absent | 1 | 8 | 5 | 10 |
| **D12** | full(c:602) | partial(L31) | unknown(L32) | unknown(L31) | partial(L32) | unknown(L32) | partial(L32) | none(L29) | partial(L29) | partial(L29) | unknown(L31) | none(L28) | partial(L29) | partial(L32) | partial(L30) | partial(L32) | partial(L32) | partial(L32) | partial(L30) | absent | absent | absent | absent | absent | 1 | 12 | 2 | 9 |
| **D13** | full(a:141) | partial(L32) | partial(L33) | unknown(L32) | partial(L33) | unknown(L33) | partial(L33) | partial(L30) | partial(L30) | partial(L30) | unknown(L32) | partial(L29) | partial(L30) | partial(L33) | partial(L31) | unknown(L33) | partial(L33) | unknown(L33) | partial(L31) | absent | absent | absent | absent | absent | 1 | 13 | 0 | 10 |
| **D14** | full(a:145) | partial(L33) | unknown(L34) | partial(L33) | unknown(L34) | unknown(L34) | partial(L34) | partial(L31) | partial(L31) | partial(L31) | unknown(L33) | partial(L30) | partial(L31) | unknown(L34) | partial(L32) | unknown(L34) | partial(L34) | unknown(L34) | partial(L32) | absent | absent | absent | absent | absent | 1 | 11 | 0 | 12 |
| **D15** | full(run) | partial(L34) | unknown(L35) | partial(L34) | unknown(L35) | unknown(L35) | partial(L35) | partial(L32) | partial(L32) | partial(L32) | unknown(L34) | partial(L31) | partial(L32) | partial(L35) | partial(L33) | unknown(L35) | unknown(L35) | partial(L35) | none(L33) | absent | absent | absent | absent | absent | 1 | 11 | 1 | 11 |
| **D16** | full(b:856) | partial(L35) | unknown(L36) | partial(L35) | unknown(L36) | unknown(L36) | unknown(L36) | none(L33) | partial(L33) | partial(L33) | unknown(L35) | partial(L32) | partial(L33) | partial(L36) | partial(L34) | unknown(L36) | unknown(L36) | partial(L36) | partial(L34) | absent | absent | absent | absent | absent | 1 | 10 | 1 | 12 |
| **D17** | full(b:857) | partial(L36) | partial(L37) | partial(L36) | partial(L37) | unknown(L37) | partial(L37) | none(L34) | partial(L34) | partial(L34) | partial(L36) | partial(L33) | partial(L34) | partial(L37) | partial(L35) | partial(L37) | partial(L37) | partial(L37) | partial(L35) | absent | absent | absent | absent | absent | 1 | 16 | 1 | 6 |

**列数核对**：1（维度）+ 24（gopi + 18 实采 + 5 absent）+ 4（full/partial/none/unknown/absent 合计列）
= **29 列**，**行数 17**。

### 2.1 每家对手的 score_all / n（`unknown` 不计分，框架 A3）

`score_all` = `full=2 / partial=1 / none=0` 求和；`n` = 参与计分的维数（= 17 − unknown）。
**`score_gopivs` 全部为 0**（§1.3 第 2 条）：只在我方也有 E3 的维度上比，
而对方**没有任何一维有 E3**。

（`score_all/n`，按 `score_all` 降序；`n` = 17 − unknown）

`astrbot` 17/17  ·  `openclaw` 17/17  ·  `deepseek-tui` 16/17  ·  `hermes` 16/17  ·  `langcli` 16/17  ·  `nanobot` 16/17  ·  `qwen-code` 15/15  ·  `reasonix` 15/17  ·  `oh-my-pi` 13/14  ·  `codex` 12/12  ·  `cline` 11/12  ·  `deep-code` 11/17  ·  `crush` 10/11  ·  `pi` 10/11  ·  `claude-code` 8/8  ·  `opencode` 7/9  ·  `kilo-code` 6/6  ·  `copilot-cli` 1/1

**这张分数表只能横向读「对手之间」，不能纵向读「对手 vs gopi」**：
gopi 的分数里绝大多数是靠 E3 挣来的，而 E3 这条路对手被口径禁止走。
把它当排名用，等于拿满档对半档 —— 框架 §2 的原话就是「否则比较是拿满分我方对上限为 partial 的对方」。

---

## 3. 按维度的我方档位 + 档内最强 / 最弱 3 家

**「档内」的定义**：只在该维等级**已达该维最高档**的对手里排序；档内次序 = 证据强度
（`E1+E2` > `E2` > `E1`），平手按名称字母序 —— **这是可复算的排序键，不是判读**。
**因为对方全部封顶 `partial`，除 D01（我方 `none`）外的 16 维「档内最强 3」都是
partial 档内的最强，不代表他们比我们的 `full` 更强** —— 这一列读的是
「谁的静态证据最扎实」，用于判断**该盯谁**，不用于判断谁赢。

**D01**（对方非零 10/18，none 3，unknown 5）
- 档内最强 3：`astrbot`(partial, E1+E2, L20)、`codex`(partial, E1+E2, L21)、`hermes`(partial, E1+E2, L18)
- 档内最弱 3：`deepseek-tui`(partial, E1, L18)、`openclaw`(partial, E1, L19)、`reasonix`(partial, E1, L19)

**D02**（对方非零 17/18，none 0，unknown 1）
- 档内最强 3：`astrbot`(partial, E1+E2, L21)、`cline`(partial, E1+E2, L21)、`codex`(partial, E1+E2, L22)
- 档内最弱 3：`langcli`(partial, E1, L18)、`openclaw`(partial, E1, L20)、`reasonix`(partial, E1, L20)

**D03**（对方非零 16/18，none 0，unknown 2）
- 档内最强 3：`astrbot`(partial, E1+E2, L22)、`cline`(partial, E1+E2, L22)、`crush`(partial, E1+E2, L23)
- 档内最弱 3：`langcli`(partial, E2, L19)、`codex`(partial, E1, L23)、`deepseek-tui`(partial, E1, L20)

**D04**（对方非零 17/18，none 0，unknown 1）
- 档内最强 3：`claude-code`(partial, E1+E2, L24)、`codex`(partial, E1+E2, L24)、`crush`(partial, E1+E2, L24)
- 档内最弱 3：`opencode`(partial, E2, L24)、`qwen-code`(partial, E2, L24)、`reasonix`(partial, E2, L22)

**D05**（对方非零 13/18，none 0，unknown 5）
- 档内最强 3：`codex`(partial, E1+E2, L25)、`hermes`(partial, E1+E2, L22)、`nanobot`(partial, E1+E2, L22)
- 档内最弱 3：`qwen-code`(partial, E2, L25)、`reasonix`(partial, E2, L23)、`deepseek-tui`(partial, E1, L22)

**D06**（对方非零 10/18，none 3，unknown 5）
- 档内最强 3：`deep-code`(partial, E1+E2, L23)、`deepseek-tui`(partial, E1+E2, L23)、`astrbot`(partial, E2, L25)
- 档内最弱 3：`openclaw`(partial, E2, L24)、`qwen-code`(partial, E2, L26)、`reasonix`(partial, E2, L24)

**D07**（对方非零 17/18，none 0，unknown 1）
- 档内最强 3：`claude-code`(partial, E1+E2, L27)、`cline`(partial, E1+E2, L26)、`codex`(partial, E1+E2, L27)
- 档内最弱 3：`opencode`(partial, E2, L27)、`qwen-code`(partial, E2, L27)、`reasonix`(partial, E2, L25)

**D08**（对方非零 10/18，none 1，unknown 7）
- 档内最强 3：`deepseek-tui`(partial, E1+E2, L25)、`astrbot`(partial, E2, L27)、`codex`(partial, E2, L28)
- 档内最弱 3：`openclaw`(partial, E2, L26)、`qwen-code`(partial, E2, L28)、`reasonix`(partial, E2, L26)

**D09**（对方非零 11/18，none 1，unknown 6）
- 档内最强 3：`deepseek-tui`(partial, E1+E2, L26)、`astrbot`(partial, E2, L28)、`claude-code`(partial, E2, L29)
- 档内最弱 3：`openclaw`(partial, E2, L27)、`qwen-code`(partial, E2, L29)、`reasonix`(partial, E2, L27)

**D10**（对方非零 15/18，none 0，unknown 3）
- 档内最强 3：`deep-code`(partial, E1+E2, L27)、`kilo-code`(partial, E1+E2, L29)、`astrbot`(partial, E2, L29)
- 档内最弱 3：`claude-code`(partial, E1, L30)、`cline`(partial, E1, L29)、`codex`(partial, E1, L30)

**D11**（对方非零 8/18，none 5，unknown 5）
- 档内最强 3：`astrbot`(partial, E2, L30)、`langcli`(partial, E2, L27)、`oh-my-pi`(partial, E2, L31)
- 档内最弱 3：`qwen-code`(partial, E2, L31)、`claude-code`(partial, E1, L31)、`cline`(partial, E1, L30)

**D12**（对方非零 12/18，none 2，unknown 4）
- 档内最强 3：`codex`(partial, E1+E2, L32)、`crush`(partial, E1+E2, L32)、`deepseek-tui`(partial, E1+E2, L29)
- 档内最弱 3：`qwen-code`(partial, E2, L32)、`reasonix`(partial, E2, L30)、`oh-my-pi`(partial, E1, L32)

**D13**（对方非零 13/18，none 0，unknown 5）
- 档内最强 3：`astrbot`(partial, E2, L32)、`claude-code`(partial, E2, L33)、`codex`(partial, E2, L33)
- 档内最弱 3：`pi`(partial, E2, L33)、`reasonix`(partial, E2, L31)、`oh-my-pi`(partial, E1, L33)

**D14**（对方非零 11/18，none 0，unknown 7）
- 档内最强 3：`pi`(partial, E1+E2, L34)、`astrbot`(partial, E2, L33)、`cline`(partial, E2, L33)
- 档内最弱 3：`nanobot`(partial, E2, L31)、`openclaw`(partial, E2, L32)、`reasonix`(partial, E2, L32)

**D15**（对方非零 11/18，none 1，unknown 6）
- 档内最强 3：`crush`(partial, E1+E2, L35)、`astrbot`(partial, E2, L34)、`cline`(partial, E2, L34)
- 档内最弱 3：`oh-my-pi`(partial, E2, L35)、`openclaw`(partial, E2, L33)、`qwen-code`(partial, E2, L35)

**D16**（对方非零 10/18，none 1，unknown 7）
- 档内最强 3：`hermes`(partial, E1+E2, L33)、`reasonix`(partial, E1+E2, L34)、`astrbot`(partial, E2, L35)
- 档内最弱 3：`oh-my-pi`(partial, E2, L36)、`openclaw`(partial, E2, L34)、`qwen-code`(partial, E2, L36)

**D17**（对方非零 16/18，none 1，unknown 1）
- 档内最强 3：`oh-my-pi`(partial, E1+E2, L37)、`pi`(partial, E1+E2, L37)、`astrbot`(partial, E2, L36)
- 档内最弱 3：`opencode`(partial, E2, L37)、`qwen-code`(partial, E2, L37)、`reasonix`(partial, E2, L35)


---

## 4. 四张全局表

**共同声明（适用于 4.1–4.4 的每一行判定）**：
「判定」列是**本 run 从采集文档直接推出的**，**未经阶段 3 独立复核**
（阶段 3 的 23 份 gap 表实测 0 份，§0）。凡「推断」字样出现处，均为推断，不是实测。

### 4.1 敌有我无（对方有、我方 `none`）

```
D01: gopi=none; 等级严格高于我方 10 家 -> astrbot=partial(L20), codex=partial(L21), deep-code=partial(L18), deepseek-tui=partial(L18), hermes=partial(L18), langcli=partial(L17), nanobot=partial(L18), openclaw=partial(L19), qwen-code=partial(L21), reasonix=partial(L19)
```

判定：**只有 D01 一维，且这一维 10/18 对手都有**。
`gopi-self-a.md:136` 的判定依据是「105 份 `-h` 全文对 `npm i|pip install|go install|brew install|make build|download` **0 命中**」，
本 run 复验：`ls package.json pyproject.toml install.sh` 三者皆 `No such file or directory`，
`grep -c '^install:' Makefile` = 0（§8 `e3-D01-D02-D06-D07-gopi.txt`）。
**这是本次对比里唯一一处「我方完全没有、对手普遍有」的能力面。**

### 4.2 敌强我弱（对方等级严格高于我方）

**按等级口径，这一张表只有 D01 一行**（即 4.1 那一行），因为：

- 对方上限 `partial`，我方 15 维 `full`、1 维（D07）`partial`、1 维（D01）`none`；
- `partial > none` 只在 D01 成立；`partial` 不严格高于 `partial`。

**所以「敌强我弱」在本口径下几乎为空集，而这本身是一个口径产物、不是实力结论。**
若要问「有没有对手在我们 `full` 的维度上做得更深」，等级回答不了，只能逐维读语义
（如 D17 的「可认领 + 可回溯」形状、D16 的检索器 schema）。这类问题本文**不判**，
交给阶段 3 的逐家分析 —— 它没跑（§0）。

判定：**不做论断**。把 D01 之外的空集读成「我们处处更强」是这一维最典型的误读。

### 4.3 我有敌无（我方有、对方 `none`）

```
D01: gopi=none; 对方 none 3 家 -> cline(L20), oh-my-pi(L21), opencode(L21)
D06: gopi=full; 对方 none 3 家 -> crush(L26), opencode(L26), pi(L26)
D08: gopi=full; 对方 none 1 家 -> deep-code(L25)
D09: gopi=full; 对方 none 1 家 -> deep-code(L26)
D11: gopi=full; 对方 none 5 家 -> deep-code(L28), deepseek-tui(L28), hermes(L28), nanobot(L28), reasonix(L29)
D12: gopi=full; 对方 none 2 家 -> deep-code(L29), langcli(L28)
D15: gopi=full; 对方 none 1 家 -> reasonix(L33)
D16: gopi=full; 对方 none 1 家 -> deep-code(L33)
D17: gopi=full; 对方 none 1 家 -> deep-code(L34)
```

判定（未经阶段 3 复核）。最强的三处，且**同时是采集质量最好、误判风险最低的三处**：

| 维度 | 我方 | 对方 `none` 家数 | 为什么这条判定可信度高 |
|---|---|---|---|
| D11 屏幕驱动 | `full` | **5 / 18** | 5 家都是 E1+E2 双零型 `none`（不是 `unknown`），且我方有**本 run 实测两张全屏 sha256 不同**（`sha256_16=ee2a715139252ab0` → `sha256_16=9aa696ee5eb5f9d4`，§8） |
| D06 会话语义 | `full` | 3 / 18 | crush/opencode/pi 的 `none` 是双零；我方 `session list` 真跑出 50 条会话（§8） |
| D12 本地服务面 | `full` | 2 / 18 | 我方 `gopi web` 真起服务、`curl` 得 `http=200 bytes=69870`（§8）；`none` 两家（deep-code/langcli）是双零 |

（同一段输出的 D01 行属于 4.1 的反向行，保留只为让生成器输出完整。）

### 4.4 各有千秋（双方都有，但形状不同）

等级相同（都是 `partial`/`full`）**不等于**能力相同；这一表列的是
「双方都有非零证据、且我方能说清形状差异」的维度。统计基线（对方 `full|partial` 家数 / 18）：

```
D01: gopi=none; 对方 full|partial 10/18
D02: gopi=full; 对方 full|partial 17/18
D03: gopi=full; 对方 full|partial 16/18
D04: gopi=full; 对方 full|partial 17/18
D05: gopi=full; 对方 full|partial 13/18
D06: gopi=full; 对方 full|partial 10/18
D07: gopi=partial; 对方 full|partial 17/18
D08: gopi=full; 对方 full|partial 10/18
D09: gopi=full; 对方 full|partial 11/18
D10: gopi=full; 对方 full|partial 15/18
D11: gopi=full; 对方 full|partial 8/18
D12: gopi=full; 对方 full|partial 12/18
D13: gopi=full; 对方 full|partial 13/18
D14: gopi=full; 对方 full|partial 11/18
D15: gopi=full; 对方 full|partial 11/18
D16: gopi=full; 对方 full|partial 10/18
D17: gopi=full; 对方 full|partial 16/18
```

判定（未经阶段 3 复核，**以下每一条都是本 run 对语义的推断，不是实测**）：

| 维度 | 我们有 | 对手有 | 形状差异（推断） |
|---|---|---|---|
| D02 零配置首跑 | `config setup` 一条幂等命令把 host 带到 run-ready | 17/18 | 对手是「粘 key 即用」（Web UI 或环境变量）；我们是「一条 provisioning 命令 + 480 行环境变量表」（§8 `--help-env` 实测 480 行）。**对手的首跑更短，我们的首跑更可检查** |
| D04 配置可检查面 | `config show` 打印生效值（`exit=0`，实测见 §8）+ `--help-env` 480 行 | 17/18 | 对手多是「读一个 config 文件」；我们是「读文件 + 一条命令打印生效值 + 一张环境变量表」。**这条我方证据链更硬** |
| D13 可审计痕迹 | `session log/show/stats/raw` 四档 + 轮次/工具/字节/成本四条腿 | 13/18 | 对手多是 `--verbose` / jsonl 落盘；我们是**分层可读**（round 级 / 请求体级 / 原始报文级 / 成本级）。我方 `session log <id> last` 实测 `exit=0` 且打印 `Tools: bash`（§8） |
| D17 任务编排 | 任务账本 + worker + parent gate + `[queue-skip]` 标记 | **16/18** | 这维**对手普遍有**（16/18 非零），说明它已是行业标配而非差异点；我方差异在**父门可被输出观察**（`[queue-skip]` 带原因）。**各家深度本文未判**（阶段 3 未跑） |
| D16 跨会话记忆 | `memory` 多子命令（store/search/update/feedback）+ provenance 分类（`total rows: 27973 / addressable: 15576 / untraceable: 12397`） | 10/18 | 对手多是 `CLAUDE.md` / `AGENTS.md` 类**规则文件**（静态提示），我方是**可检索 + 可投票 + 可降级**的库。**这是"文件 vs 库"的形状差，不是等级差** |
| D07 外部能力扩展 | plugin 树（`websearch/scripts/` 8 个脚本）**有实现**，但 help 面无扩展点声明、无 MCP | **17/18** | 对手 17/18 都声明了扩展点（多为 MCP）。我方**有实现、无声明**，按框架 §8 判 `partial` 为上限。**这一维是我们最接近"敌强我弱"的地方，但等级口径显示不出来** |
| D10 图像输入 | `vision_analyze` 真跑读图（本 run 实测：输入 632 tok / 输出 1098 tok） | 15/18 | 对手 15/18 有非零证据，但**多数是「能 attach 图片」的声明**，不是「能读回屏幕并自报 token」。**这条我方是 E3，对方是 E1/E2** |

---

## 5. 真差距 vs 路线选择

**判据（本文自定，写在前面以便被反驳）**：

- **真差距** = 对手有、我方无（`none`），且**这一维不依赖我方已有的东西**，
  补齐的成本是**一条命令 / 一个文件级改动**；
- **路线选择** = 我方主动不做，或做了但选了另一种机制（如「无 MCP 但有 plugin 树」），
  补齐会**违反既有约束**（Rule 3 工具最小化 / Rule 35 零依赖 / 单二进制）；
- **待裁决** = 事实清楚但方向需要操作员定，本文只摆事实与代价。

| # | 维度 | 事实（实测） | 分类 | 理由 |
|---|---|---|---|---|
| 1 | **D01 安装分发** | 我方 `none`；10/18 对手有具名安装命令；我方 `package.json`/`pyproject.toml`/`install.sh` 皆无，`Makefile` 无 `install:` 目标 | **真差距** | 缺的是「从零到可执行的一条命令」。它不违反任何约束（不需要新依赖、不需要新进程、不需要新配置），成本是**一个 Makefile 目标 + 一段文档**。而且它是 10/18 对手的**入口门槛**：对手是 `curl \| bash` / `npm i -g` 一条命令，我们目前是「有 `dev_repo` 才能 clone 然后 `make build`」 |
| 2 | **D07 外部能力扩展** | 我方 `partial`（有 plugin 树、无 MCP）；17/18 对手声明了扩展点 | **待裁决** | 补 MCP = 新增一条**对外协议 + 常驻连接**，与 Rule 3（17 个工具不加）与 Rule 35（零依赖）相抵。**不是"补不上"，是"补了要付什么"没定**。事实层面唯一确定的是：我们现在**有实现、没声明**，连 `partial` 的声明面都没占 |
| 3 | D11 屏幕驱动 | 我方 `full`，5/18 对手 `none` | **不该补** | 方向反过来：这是我有敌无 |
| 4 | D17 任务编排 | 我方 `full`，16/18 对手非零 | **不该补** | 已是行业标配，且我方在「父门可观察」上有形状差；等级上无可补 |
| 5 | D02/D04 配置可检查面 | 我方 `full` + E3；17/18 对手非零 | **不该补** | 我方证据链更硬（`config show` 打印生效值 + 480 行 env 表） |
| 6 | 对方「拿到 `full`」 | 对方 306 格 `full` = **0** | **不该追** | 这是**口径**决定的（框架 §1 禁止为取证安装第三方 agent），不是能力事实。追它等于追一个自己定的上限 |
| 7 | 阶段 3 的 23 份 gap 表 | **0 / 23** 存在 | **真差距（流程面）** | 本任务的前置产物缺失。这不是功能差距，是**流水线差距**：派发器的 `completed` 被当成了工作的完成（§0）。本文不提交修复任务（交付物约定），只登记事实 |
| 8 | 采集覆盖 | 5/23 家 `absent`（阶段 1C 未跑） | **真差距（数据面）** | 23 家清单里 5 家从未进入采集；任何「23 家」的结论都不能用本文的 18 家冒充 |

---

## 6. ROI 排序补齐清单（**不提交修复任务**，作为决策依据）

**ROI = 收益（多少对手有 / 是否是门槛）× 确定性（成本已知、无新依赖） ÷ 成本**。
排序是**本 run 的推断**，理由列在每一条里，供反驳。

| 序 | 补齐项 | ROI 理由 | 成本估计 | 违反约束？ |
|---|---|---|---|---|
| **1** | **D01：给 gopi 一条具名安装命令** | 收益最高：10/18 对手有，且它是**从零到可执行的门槛**（没有它，取用必须依赖 `dev_repo` clone）。确定性最高：改动落在 `Makefile` 一个目标 + 文档；不新增依赖、不新增常驻进程 | 小（`install:` 目标 + 一段 README） | 不违反。**注意**：`go install` 与「单二进制零依赖」同向；`curl \| sh` 会引入一个**托管脚本**（Rule 35 要四问），所以更小的形状是 `make install` / `go install` |
| **2** | **把 18 家的 unknown（71 格）降下来** | 收益：`unknown` 占 23.20%，其中 **copilot-cli 16/17、kilo-code 11/17** 两家几乎整行空白 —— 这两家的任何排名都不可信。降 unknown 比新增一家采集更便宜（证据通道已建好，只是 E2 取样没取到内容） | 小-中（重采样 2 家） | 不违反 |
| **3** | **跑阶段 1C 的 5 家采集** | 收益：把 `absent` 的 85 格（5×17）变成有值，让「23 家」名副其实。成本是 5 次采集（每份 17 维） | 中 | 不违反 |
| **4** | **跑阶段 3 的 23 份 gap 表** | 收益：本文四张表的「判定」目前是**未经复核**的；它是唯一能让判定被独立复核的机制 | 中-大（23 个 run） | 不违反 |
| **5** | **D07 的扩展点声明**（MCP 或等价） | 收益：17/18 对手在这一维有声明，我们连声明面都没占。**但**：MCP 是常驻协议入口，Rule 3/35 都指向「不加」 | 大（新协议 + 连接 + 生命周期） | **可能违反 Rule 3/35 → 待裁决** |
| — | 对方拿 `full` | **不该做**（口径产物，§4.2） | — | — |

---

## 7. 数据可信度声明

### 7.1 unknown 计数与比例

| 口径 | 值 |
|---|---|
| 分母（18 家实采 × 17 维） | **306** |
| `unknown` | **71** |
| **比例** | **23.20%**（71/306） |
| `full`（对手） | **0** —— 口径决定，见 §1.3 |
| `partial` | 217 |
| `none` | 18 |
| `absent`（5 家 × 17） | **85**（不计入 306，也不计入 71） |

计数命令见 §1.4（`CORPUS=$(ls devdoc/competitor/*.md | grep -v -e 'gopi-self' -e 'summary')`，
再 `grep -hc "| unknown |" $CORPUS` 求和 = 71）。**这是 `grep` 计数，不是本 run 的推断。**

`unknown` 集中在 10 家（另 8 家为 0）：

```
copilot-cli: 16   kilo-code: 11   claude-code: 9   opencode: 8   crush: 6
pi: 6   cline: 5   codex: 5   oh-my-pi: 3   qwen-code: 2
```

⇒ **`copilot-cli` 与 `kilo-code` 的任何「档位/排名」结论都不成立**（前者 1/17 有值、
后者 6/17）。本文 §2 保留它们的行（矩阵要不漏列），但 §2.1 的 `score_all/n`
对这两家是 `1/1` 与 `6/6` —— **分母小到没有比较意义**。

### 7.2 低可信来源（逐条，附原文出处）

| 来源 | 问题（原文） | 对本文的影响 |
|---|---|---|
| **copilot-cli** | `copilot-cli.md:14` `source_note: 仓库只有 24 个 blob（README.md/changelog.md/install.sh/container/.github/），无实现源码树` | E2 几乎不可能命中 ⇒ 16/17 `unknown`。**该家整行不可用于排名** |
| **claude-code** | `claude-code.md:14` 全文：`source_note: 仓库不含实现源码：`src/` 目录不存在，1473 个 blob 分布在 `mods/`(1383)、`examples/`(41)、`.github/`(22)、`scripts/`(8)；`LICENSE.md` 首行为 `© Anthropic PBC. All rights reserved.`` | 9/17 `unknown`；E2 命中只能落在 `mods/`、`examples/`、`.github/`、`scripts/` 四处 |
| **kilo-code** | 与其 11/17 `unknown` 一致（采集文档未附 source_note，**这一点本身是采集缺口**） | 6/17 有值，可比较性低 |
| **codex / pi** | `codex.md:14` guide 内唯一 URL 指向第三方 `ZhiYi-R/moon-bridge`；`pi.md:14` guide 写的 `badlogic/pi-mono` 已更名为 `earendil-works/pi` | 仓库身份由阶段 0 §6 核对得出，不是 guide 直给 ⇒ **「对方仓库正确」这一步依赖阶段 0 的核对，不在本文的取证范围** |
| **opencode** | `opencode.md:14` 默认分支是 `dev`（不是 `main`），sha 取自 `refs/heads/dev` | 可复算（sha 固定），但读者按 `main` 复现会不一致 |
| **5 家 `absent`** | 阶段 1C 未跑（§0） | 85 格无值。**不得把 `absent` 当 `unknown`，更不得当 `none`** |
| **本 run 的我方列（D15）** | 阶段 2A/2B/2C 都没判过 D15；本 run 自跑 `tool code-search` 两次 `exit=0`（§8） | D15 的 `full` 出处是 `run`，**不是三份盘点文档** —— 本文唯一一处「我方判定由本 run 新增」 |
| **2A 的编号** | 2A 用旧 16 维编号（§1.2），且它自己也登记了这个缺口（`gopi-self-a.md:285`） | 若读者直接引用 2A 的 `D0x`，会与框架编号错位 |

### 7.3 证据强度分布（对手 306 格）

```
E1+E2: 83   E2: 133   E1: 29   —（unknown）: 61
```

（该行由 `python3` 逐行解析 18 份采集文档的「证据等级」列得到，与 §7.1 的 71 有差，
是因为此处按「证据等级」列计数、且部分 `unknown` 行的备注里仍写了 `E1`（记「试过的 URL」）；
两种计数口径不同，**两者都如实列出**。）

⇒ **E2 单独占 133 格、E1 单独 29 格**：对手的绝大多数非零判定来自**静态证据**，
没有任何一格有 E3。这是 §1.3 第 2 条的来源。

---

## 8. 本 run 的我方 E3 实测（存档在 session artifacts，不入库）

下表每条都在 `~/.gopi/sessions/01a1aa6d-5210-714f-bb61-c5b65cae01bc/artifacts/` 有落盘文件。
**退出码一律先落盘再读，不走管道**（管道会把 `$?` 换成 `grep`/`head` 的）。

| 维 | 命令（逐字） | 结果 |
|---|---|---|
| D01 | `./bin/gopi --version` | `exit=0`，输出 `v0.4.0` + `revision: cd7a63d79ede91ff52b249ae48821e1d5e01e9b4`；**但无任何安装命令**：`ls package.json pyproject.toml install.sh` 三者皆 `No such file or directory`，`grep -c '^install:' Makefile` = **0** ⇒ `none` |
| D03 | `ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic/ env -u GOPI_AGENT_SESSION -u GOPI_EXECUTOR_SESSION_ID timeout 90 ./bin/gopi --output-language zh-CN "reply with exactly: OK"` | `exit=0`，输出 `OK` ⇒ 改 base_url 后真实请求成功 |
| D04 | `./bin/gopi config show > cfg 2>&1; echo $?` → `0`；`./bin/gopi --help-env > env 2>&1; echo $?` → `0`，`wc -l env` = **480** | 键与值两列可读（`base_url` / `model` / `vision_model` / `api_key <set: 35 chars>`） |
| D05 | `env -u GOPI_AGENT_SESSION -u GOPI_EXECUTOR_SESSION_ID timeout 90 ./bin/gopi --output-language zh-CN "reply with exactly: OK"` | `exit=0`，`elapsed=14.3s`，输出 `我将按请求仅回复"OK"。`。**第一次裸跑 `exit=3`**（agent spawn 守卫拒绝，原文：`Exit code: 3 — this refusal started nothing`），去掉会话环境变量后成功 |
| D06 | `./bin/gopi session list` | `exit=0`，首行 `50 session(s):` |
| D07 | `./bin/gopi --help-env 2>&1 \| grep -in plugin` → 1 行（散文，非扩展点声明）；`ls /home/user/.gopi/plugins/websearch/scripts/` → 8 个脚本 | 有实现、无扩展点声明 ⇒ `partial` 上限（框架 §8 同结论） |
| D08 | `./bin/gopi chat -h` 的 `--policy POSTURE` 段 | 列出 `allow\|yolo\|auto\|on-request\|ask\|prompt\|forbidden\|deny\|never\|unless-trusted` ⇒ `full` |
| D09 | `./bin/gopi tool sandbox show` → `sandbox root=/home/user/workspace/gopi-sandbox source=from the built-in default prefix=sandbox- stale=72h0m0s`，`exit=0`；`tool sandbox create --label probe-stage4 --task-id 01a1aa6d-5210` → `sandbox /home/user/workspace/gopi-sandbox/sandbox-probe-stage4-563936c0 created=true`，`exit=0`；`ls -la <该路径>/.gopi/` 列出 `memory.db`(0 字节, 0600) / `plugins -> /home/user/.gopi/plugins` / `settings.json`(-r--r--r--) / `sessions/` / `raw/` / `logs/` | 私有 profile 真被创建 ⇒ `full` |
| D10 | `./bin/gopi tool vision_analyze <d11-before.png> --prompt "一句话：这是什么窗口"` | `exit=0`，输出首行全文 `【vision 分析结果】(模型 deepseek-flash, 输入 632 tokens / 输出 1098 tokens, 图片 186850 bytes)`，其下是对图的描述文字 ⇒ `full` |
| D11 | `capture --screen --out s1` → `sha256_16=ee2a715139252ab0`；`act move 100 100` → `pointer=100,100`；`capture --screen --out s2` → `sha256_16=9aa696ee5eb5f9d4` | **两张截图 sha256 不同**（屏幕确实变了）⇒ capture→act 闭环 `full`。**附一条实测边界**：`capture --window` 前后两次 sha256 相同（`e0e91c5f0e81b366`），因为指针移到了活动窗口之外 —— 判据要用**全屏**捕获，不是窗口捕获 |
| D12 | `./bin/gopi web --addr 127.0.0.1:18094 &` 后 `curl -s -o /dev/null -w 'http=%{http_code} bytes=%{size_download}' http://127.0.0.1:18094/` → `http=200 bytes=69870`，响应体含 `<title>gopi web chat</title>` | 真起服务、真 200 ⇒ `full` |
| D13 | `./bin/gopi session log 01a1aa6d-5210-714f-bb61-c5b65cae01bc last` → `exit=0`，打印 `ROUND 54 of 54`、`Usage: prompt 140,242 tok (cache hit 139,392 / miss 850)`、**`Tools: bash`**；`session stats <id> --tools` → `exit=0` + 一张指标表 | 读回含带工具名的记录 ⇒ `full`。**附实测更正**：`session log --last --tools` → `exit=1` `unknown flag --tools` —— `--tools` 属于 `session stats`，不属于 `session log` |
| D14 | `./bin/gopi tool tokens --file go.mod` → `go.mod: 715 tokens (1865 bytes, 46 lines, deepseek-v4.1-bpe)`，`exit=0` | 同一输入打印确定整数 ⇒ `full` |
| D15 | `./bin/gopi tool code-search CliVerbTokens` → `CliVerbTokens  cmd/agent/cli_verbs.go:158  const`，`exit=0`；`tool code-search ResolveSandboxRoot` → `internal/config/sandbox.go:228  func`，`exit=0` | 符号级定位返回 `file:line` ⇒ `full`（**本 run 新增判定**，2A/2B/2C 都没判过这维） |
| D16 | `./bin/gopi memory search fuzz --limit 1` → `#4965 gopi version discards a reachable borrowed name when master advances: intent (b) reachability + containment, and an RP premise that measured false tier=warm conf=0.90 lesson=recipe when=2026-10-03 20:07:00 eff_conf=1.00 sources=node:peer1 tokens=1 fb=0.67(1)`（该行逐字取自 artifacts/e3-probes-gopi.txt:39，未做任何省略），`exit=0` | 检索命中 ⇒ `full`（2B 另有 store→search 往返自证） |
| D17 | `./bin/gopi task list --status pending` → `36 task(s) with status=pending` + 逐行 `[pending]`，`exit=0` | 账本可按状态读回 ⇒ `full`（2B 另有 submit→show 往返自证） |

**两条方法论更正（本 run 亲自踩到，写下来防止复述）**：

1. `gopi --output-language zh-CN "<prompt>"` 在 **agent 会话环境里** `exit=3`（spawn 守卫），
   必须 `env -u GOPI_AGENT_SESSION -u GOPI_EXECUTOR_SESSION_ID` 才是被测对象本身的行为；
   把 `exit=3` 读成 D05 缺能力就错了。
2. `cmd | head` 得到的 `$?` 是 `head` 的（第一次 `config show` 因此读到 `exit=141`），
   **任何有语义的退出码都必须先重定向落盘再读**。

---

## Not Done（诚实）

- **未使用阶段 3 的产出，因为它不存在**（§0 实测：`gap-*.md` 0/23）。
  本文的四张表与全部差距判定因此**未经阶段 3 独立复核**，每一处都已在文内标注。
- **未采集阶段 1C 的 5 家**（cherry-studio / github-copilot / lobehub / qoder / workbuddy）：
  它们的 85 格是 `absent`，不是 `unknown`。**任何「23 家」的结论都不能引用本文的 18 家冒充。**
- **未补 71 格 `unknown`**：已用 `grep` 计数并定位到 10 家（§7.1），但重采样是独立工作项。
- **未对 18 家做 E3**（框架 §1 明文禁止为取证安装第三方 agent，Rule 27/35 同向），
  因此 `score_gopivs` 对每一家都是 0，**等级不能跨方比较**（§1.3）。
- **未判定「各家在 D17/D16 上的深度差异」**：那需要逐家读语义，属于阶段 3 的活。
  本文只给等级与统计基线（§4.4）。
- **未提交任何修复任务**（交付物约定）：§6 的补齐清单只是决策依据。
- **未验证 5 家 `absent` 的仓库存在性**：阶段 1C 未跑，连 URL 核对都没进这一步。

---

## Evidence（self-filed）

| 声明 | 我看到的（命令 + 输出） |
|---|---|
| `gap-*.md` 0/23 | `ls devdoc/competitor/gap-*.md 2>/dev/null \| wc -l` → `0`（ground 与主仓各一次）；`find /home/user/workspace /home/user/work -maxdepth 4 -name 'gap-*.md'` → 无输出 |
| 23 条子行 pending | `gopi task list --status pending \| grep -c '01a1acc7'` → `23`；抽样 `gopi task show <id>` → `status: pending`、`parent_task_id: 01a1aa6d-3309-7734-a55f-725d3ee87138` |
| 阶段 1C 未跑 | `gopi task show 01a1aa6c-c4c5-7bf4-8dcd-831c6a5bc904` → `status: pending` |
| 18 份采集各 17 行 | `CORPUS=$(ls devdoc/competitor/*.md \| grep -v -e 'gopi-self' -e 'summary'); for f in $CORPUS; do grep -cE '^\| D[0-9]{2} \|' $f; done \| sort -u` → `17`（单一取值，即 18 份全是 17） |
| 对手 `full` = 0 | `CORPUS=$(ls devdoc/competitor/*.md \| grep -v -e 'gopi-self' -e 'summary'); grep -hc "\| full \|" $CORPUS \| awk '{s+=$1} END{print s}'` → `0`；矩阵生成器同读数：`full=15` 全在 gopi 列 |
| unknown = 71 = 23.20% | 同上 `grep` 计数 → `71`；`echo $((18*17))` → `306`；生成器打印 `unknown_ratio=71/306=23.20%` |
| 禁用措辞 0 命中 | `LC_ALL=C.UTF-8 grep -nE '主[观]\|大[概]\|差[不]多\|看[情]况\|感[觉]\|较[好]\|较[强]' devdoc/competitor/*.md \| wc -l` → `0` |
| 2A 用旧 16 维编号 | `wc -l /home/user/.gopi/sessions/01a1aa6c-703d-7059-8e6a-4a6b5dced12c/artifacts/dims.txt` → `16`；`gopi-self-a.md:285` 原文「阶段 0 的 `dims.txt` 只有 D01–D16，共 16 行（`wc -l` 实测 16）。本片在 §2 只表 10 维，剩余 6 维按上表分流，**D17 无人认领** —— 这是阶段 0 的口径缺口，不是本片漏做」 |
| 我方 D11 capture→act 闭环 | 两次 `capture --screen` 的 `sha256_16` = `ee2a715139252ab0` vs `9aa696ee5eb5f9d4`（不同） |
| 我方 D12 本地服务 | `curl` → `http=200 bytes=69870`，响应体含 `<title>gopi web chat</title>` |

**本文的每个数字都由 §1.4 的命令或 §8 的落盘文件产生；
生成器与原始输出在 session artifacts 目录，不入库。**
