<!-- doc-class: historical -->
# Task Report: 四家竞品对比的「人话版」+ 生成器同步（Rule 5）


**任务**：把 `docs/archive/demo/final/competitive.md`（四家竞品 file:line 对比）重做成两层文件 ——
第一层给没耐心的读者，第二层一个字节不删。生成器 `docs/archive/demo/final/build.sh` 同步改，两个文件都由它产出。

## Completed

1. **新文件 `docs/archive/demo/final/competitive-human.md`（两层，生成器产出）**
   - 第一层「一分钟版」：7 个部分 —— 我们是什么 / 一眼看懂对比 / 数字对照（旧→新）/ 逐格对照（18 行）/
     我们强在哪 / 我们弱在哪 / 为什么会这样 / 「查不到（不是没有）」是什么意思 / 诚实差距 6 条 / 第二层从哪开始。
   - 第二层 = `competitive.md` **逐字节原样**（生成器 `cmp` 断言，实测空）。
2. **口径不再读反（核心缺陷 1）**
   - 格子主文本换成「他们有，我们没有 / 我们有，他们没有 / 两边都有 / 查不到（不是没有）」；
     旧词 **赢/输/平/未判** 只在括注与对照表里带含义出现。
   - 第一层给出**旧→新对照表**（4 行，含旧词原意与本次出现格数）+ **逐格对照表**（18 行）。
   - 数字不是手写：`N_CELLS/N_WIN/N_LOSE/N_TIE/N_UNK` 由生成器从同一份 `CELLS` 变量数出。
3. **第一屏回答问题（核心缺陷 2）**：`我们是什么`（4 句）→ `一眼看懂对比`（9 句）→ 数字对照 →
   逐格对照 → `我们强在哪`（7 句）/ `我们弱在哪`（8 句）→ `诚实差距（六条）`。首屏不再是表格与 file:line。
4. **`competitive.md` 仅增不删**：新增 §0「人话版」入口（指向新文件 + 四条旧词→新说法 + `cmp` 声明），
   §「口径」段加一条：这组词反直觉、已换口径、逐格对照看新文件。原文 132 行一行未删（逐条核对见下）。
5. **`build.sh` 生成器**：新增 3 个数据块（`CELLS` / `LEGEND` / 派生的计数）与 human face 段的运行期自检
   —— 该段印 **7 行结论，其中 6 行是断言**（第 1 行是字节数，不是断言），另有只在不通过时发声的守卫
   （逐条打印结论，任一条不满足即 `exit 1`）；
   `index.html` 的 `§1` 口径提示与 `§6` 边界项加十字（未判 ≠ 没有 + 指路人话版）。全部零依赖（bash + awk + od + cmp）。
6. **`claude` 那一面讲成人话且不弱化**：第一层专列一节解释「查不到（不是没有）」——
   本机只有打包程序、没有源码、所以查不到、不猜、也不等于对方没有。

## Not Done

- **没有改 `competitive.md` 的任何一格判定**：口径缺陷是「读法」问题，不是「判定」问题；
  表格里的 赢/输/平/未判 一个都没有重新裁定（那是另一次测量的事，需要重跑源码比对）。
- **没有为公开发布净化素材图**：第一层正文不含任何路径/函数名（生成器逐条扫描并打印结论），
  但第二层与 `index.html` 引用的素材图内仍含沙盒路径等内部标识 —— 这条本来就写在 §6「未做」里，未变更。
- **`index.html` 的 §1 表格没有换成新口径**：本任务的验收面是 `competitive-human.md` 与 `competitive.md`；
  页面只加了指路与「未判 ≠ 没有」一句。整页换口径属于页面改版，不在本次范围（已在此明说，避免误读为遗漏）。
- **没有改 gopi 的 Go 代码**（约束 7）：生成器不触发 devdoc 分类，DOC-INDEX 行由 `gopi doccheck --fix` 走既有写入器落盘。

## Verification

- Build:  `bash -n docs/archive/demo/final/build.sh` → syntax OK（Generator 是 bash，无编译步；`go build` 未被本任务触及）
- Tests:  `bash docs/archive/demo/final/build.sh` → exit 0，human face 段 6 条自检全绿（见下 `Result`）；
  `bash docs/archive/demo/final/build.sh --shot` → exit 0（含无头渲染自证，不碰真实桌面）
- Ran:
  ```
  bash docs/archive/demo/final/build.sh
  bash docs/archive/demo/final/build.sh --shot
  tail -c "$(stat -c%s docs/archive/demo/final/competitive.md)" docs/archive/demo/final/competitive-human.md > /tmp/t3 && cmp /tmp/t3 docs/archive/demo/final/competitive.md
  awk 'NR==FNR{new[++n]=$0; next} {ok=0; for(i=p+1;i<=n;i++) if(new[i]==$0){p=i; ok=1; break} if(!ok){print "MISSING "FNR": "$0; miss++}} END{printf "旧文件 %d 行 → 新文件按序全部找到；未找到 %d 行（0 = 一条没删）\n", FNR, miss+0}' docs/archive/demo/final/competitive.md /tmp/old-competitive.md
  md5sum docs/archive/demo/final/index.html docs/archive/demo/final/competitive.md docs/archive/demo/final/competitive-human.md > /tmp/m1 && bash docs/archive/demo/final/build.sh >/dev/null 2>&1 && md5sum -c /tmp/m1
  gopi doccheck   # → "the devdoc corpus is consistent — every file under devdoc/ has a well-formed row", exit 0
  ```
  **收尾复核的第二轮（本次运行实测）**：
  ```
  # 红证：把「表重复输出」注入 session 目录里的副本，跑生成器要看它红
  cp -r <worktree>/docs/archive/demo/final <session>/gate-redproof/docs/archive/demo/final   # 副本，不在任何 git 树内
  <注入后> bash docs/archive/demo/final/build.sh   # → "逐格对照表出现 36 行，应为 18 行"，exit 1
  bash docs/archive/demo/final/build.sh            # 修好的树 → exit 0，grid rows 18
  awk '/^## 逐格对照/{f=1} f{print} /^## 我们强在哪/{exit}' docs/archive/demo/final/competitive-human.md  # 表只出现一次
  awk '/^# 第二层/{exit} {print}' docs/archive/demo/final/competitive-human.md | grep -n '没有'  # 未判未被讲成「没有」
  ```
  分类行由**写入工具的 doc-classify 钩子**在 `write` 落盘时自动追加（Rule 36：write/edit 与
  close-out 的 `SyncBranch` 都会给新 devdoc 分类），随后 `gopi doccheck`（只读面）复验一致、exit 0；
  语料行数 2681 → 2682，`awk -F'\t' 'NF!=4'` 无畸形行。
- Result:
  - `build.sh` exit 0（`--shot` 亦 exit 0）；逐行打印的自检结论（**本次收尾复核后的最新一次重跑**，verbatim）：
    ```
    build.sh: human face (competitive-human.md)  : 17670 bytes
    build.sh:   technical face byte-for-byte     : IDENTICAL (cmp vs competitive.md, 12239 bytes)
    build.sh:   plain-sentence length (≤25 chars) : longest 23 chars ("「查不到（不是没有）」：我们没查到，不敢下结论"), over-limit 0
    build.sh:   legend ↔ cells (derived, 1 source) : 18 行 / 19 个判定 = 赢11/输4/平3/未判1; plain+legend grid rows 18; 判定表 3 张 / 表内数据行 18
    build.sh:   plain face (first screen)         : 47 bullet lines, no path/filename, no bare legacy 赢/输/平
    build.sh:   cell dimensions present in record : all present (no face contradicts the record)
    build.sh:   zero external dependency          : clean (no http(s) src/href in competitive-human.md)
    ```
    （本块是**第 5 个缺陷修好之后**的重跑；第 5 个缺陷之前的同一行印的是 `17619 bytes` / `46 bullet lines`
    —— 那两个值曾留在本块里，与本节末尾的字节账自相矛盾，已一并更正。第 3 个缺陷「表重复输出」红证时的那份日志里，同一行印的是 `18689 bytes` / `grid rows 36` /
    `MISSING DIMENSION` —— 那是**红态**的引用，保留在此作对照，不是当前状态。）
  - 第二层逐字节：`cmp` **无输出**（= 相同）。独立复核命令（不依赖生成器）见 `Ran`。
  - 逐条核对：旧 132 行 → 新文件按序全部找到，**未找到 0 行**；无序集合复核 `comm -23` 输出 **0 行**；
    grep 代码块 107 行未变（新增的 12 行全部属于新增 §0）。
  - 章节级计数（旧 → 新）：§1 表格 9→9、§2 8→8、§3 7→7、§4 列表 3→3、§5 列表 11→11、§6 列表 4→4。
  - 重跑逐字节相同：`md5sum -c` 三个文件全 **OK**（生成器是纯函数：无时间戳、无 HEAD、无自身渲染数字）。
  - `git status --porcelain`：见 `Commit`（提交后应为空）。
- Commit: `565e6dc7` — 「docs(demo): 四家竞品对比加「人话版」两层文件，口径不再读反」
  （交付提交；本报告自身的 hash 补记随其后的一个小提交）
  `git status --porcelain` 提交后**无输出**（Rule 30）。

> 本报告里出现的一处自证文字（`build.sh: … : IDENTICAL` 等）是**构建日志的逐字引用**；
> 生成器每次跑都会重新打印，`Ran` 段给了可复跑的命令。

## 收尾复核发现的第 3 个真实缺陷（已修 + 已红证）

**症状**：第一层的「逐格对照」18 行表被**输出两遍** —— 一遍无表头，紧接一遍有表头，
占了半屏，`我们强在哪` 被推到 100 行开外。这与「第一屏回答是什么/强在哪/弱在哪」直接冲突。

**根因（5-why）**：`build.sh` 的两段写法叠加 —— 命令替换里用 `awk END{}` 打完表并按 `r>0` 追加小注，
替换之后**又**手写了一份表头 + 同一份 `awk` 表体。`r>0` 永远为真，所以 `END{}` 那句必然打印，
于是表体出现两次；而当时的自检只统计「表行数是否 ≥ 1」，重复输出不可见。

**修法**：合并为一个命令替换（表头 + 表体 + 小注一次打完，小注改用 bash 内联比较），
并把自检从「有没有」升级为「**恰好 N_CELLS 行**」——`grep -cE '^\| [^|]* \| [^|]* \| \*\*[^*]*\*\* \|$'` 必须等于 18，不等即 `exit 1`。

**红证（Rule 5 回归守卫，红→绿）**：在 session 目录复刻一份树，把「重复输出」这一缺陷重新注入
（在命令替换后追加同一份无表头表体），跑生成器：

```
build.sh: 逐格对照表出现 36 行，应为 18 行 —— 表被重复输出或漏输出   ← 拦下
build.sh:   legend ↔ cells (derived, 1 source) : 18 格 = 赢11/输4/平3/未判1; plain+legend grid rows 36
build.sh:   cell dimensions present in record : MISSING DIMENSION
RED exit=1
```
修好的树：`plain+legend grid rows 18`、`all present`、`green exit=0`。
副作用：`competitive-human.md` 18689 → 17573 字节（少掉的正是重复的那 18 行）。
（红证在 session 目录的**副本**里做，不进任何 git 树 —— 未在仓库里落任何测试夹具。）

## 收尾复核发现的第 4 个真实缺陷（已修 + 已红证）

**症状**：第一层「逐格对照」表下面写「对着下面的 **7 张表** 逐格看」，而记录里只有
**3 张**判定表（§1 codex / §2 dsh / §3 hermes 各一张，§4 claude 是列表、§5 §6 不是判定表）。
「7」是 `grep -c '^## [0-9]'` 数出来的**章节**数 —— 它数的是标题，不是判定表，两个数字根本不是一回事。

**根因**：句子里的数字是**手写进 heredoc 的**，没有任何东西从记录里导出它；
而当时的 gate 只检查「表行数恰好 18」，不检查首屏印出来的**任何数字**与记录是否一致 ——
数字错了而所有灯都绿。这与本次主诉（读者被文件里的数字误导）是同一类毛病，只是这次错的是我们自己的字。

**修法**：数字改为**派生**（`N_CELLS_ROWS=$(grep -c '^| 维度 |' competitive.md)`、
`N_GRID_ROWS=` 数表内数据行），句子改成「共 18 行，落在下面 3 张判定表里」；
并加一条 gate：**从已落盘的人话文件里把印出来的数字抠出来**，与**从已落盘的记录里数出来的**表数比较，
不等即 `exit 1`；同时要求表内数据行合计 = 18（网格一格不落地落在表里）。

**关键点（第一版 gate 是假的）**：第一版 gate 比较的是 `PUB_TABLES != $N_CELLS_ROWS` ——
即「印出来的数字 vs 供 heredoc 的那个变量」。把 `N_CELLS_ROWS=7` 硬写回去测试时它**仍然全绿**，
因为两个数字来自同一个变量。gate 必须**绕过变量、直接从落盘文件读**才成立。这一版才拦住。

**红证（红→绿，两次）**：在 session 目录复刻整棵树（含 `docs/archive/demo/materials/`、`scripts/`，
因为生成器从 `ROOT` 解析路径），注入两个形态的缺陷：

```
# 形态 1：把派生改成硬写的 7（旧 bug 的变量形态）
$ sed -i '552s|.*|N_CELLS_ROWS=7|' docs/archive/demo/final/build.sh && bash build.sh
build.sh: 首屏写「7张判定表」，记录里是 3 张 —— 首屏数字与记录不符   ← 拦下
build.sh:   cell dimensions present in record : MISSING DIMENSION
RED exit=1

# 形态 2：把句子退回原始手写形态「对着下面的 7 张表逐格看」
build.sh: 首屏写「无张判定表」，记录里是 3 张 —— 首屏数字与记录不符   ← 拦下（句子不再派生 ⇒ 抠不到数字）
RED exit=1

# 修好的树（当时印出的那一行；该行的**形态**随后被第 6 个缺陷改掉，见下节 —— 现在印
# 「18 行 / 19 个判定 = 赢11/输4/平3/未判1」）
build.sh:   legend ↔ cells (derived, 1 source) : 18 格 = 赢11/输4/平3/未判1; plain+legend grid rows 18; 判定表 3 张 / 表内数据行 18
build.sh:   cell dimensions present in record : all present
GREEN exit=0
```

**顺带修的数字**（全部是手写计数这一类毛病，改完都以生成器**重跑印出的那一行为准**）：
- `devdoc` 里「5 项运行期自检」是错的 → 改为「该段印 7 行结论，其中 6 行是断言」。
- 第一层不重复的 bullet 行数由 **46** → **47**（第 5 个缺陷把顶部说明块改写成 4 句短行、并新增覆盖面守卫的说明行）。

**第 3｜4｜5 个缺陷的字节账**：`competitive-human.md` 18689（表重复）→ 17573（修掉重复）
→ 17619（第 4 个把一行长句拆成 6 行短句）→ **17670**（第 5 个把四行 45 字的说明块换成四行 ≤12 字，
另加覆盖面守卫；第 6 个只改自检打印行与守卫，**人话文件字节数不变**：仍是 17670）。`competitive.md` 全程 **12239 字节不变**：新加的技术说明块落在**人话文件的存档层前言**
（`# 第二层` 标题之下、`cmp` 断言段之后），**不进 `competitive.md`** —— 所以「一条没删」与「逐字节等同」
两条同时成立。当前值以生成器每次重跑印出的 `human face (competitive-human.md) : 17670 bytes` 为准，不要引用任何中间值。

## Evidence (self-filed)

- 「第二层逐字节等同」：`tail -c … | cmp` 无输出（exit 0）—— 上面 `Ran` 的第三条命令，本次运行亲测。
- 「一条没删」：awk 顺序包含检查打印 `未找到 0 行`；`comm -23` 打印 0。
- 「≤25 字」：生成器扫描第一层（**含顶部说明块与编号列表**）打印 `over-limit 0`，最长 23 字，
  且覆盖面守卫要求「扫到的行数 ≥ 可读行数」；**本轮之前**它其实只扫了 bullet 行，`longest 23` 是真话但非全覆盖。
- 「口径不会读反」：生成器把 `CELLS` 第 4 列与**从已落盘的 `competitive.md` 抽出的判定列**逐行比对
  （维度 + 判定 token 序列），打印 `all present`；本次比对前曾以 exit 1 拦下两次真实错误：
  手写表 12 行 vs 记录 18 行、以及 §3「定时 / 驻留作业」的双判定行。
- 「首屏数字与记录一致」：`PUB_TABLES`（从落盘的人话文件里抠出）== `REC_TABLES`（从落盘的记录里数出）
  == 3，且表内数据行合计 = 18；两次红证见上。
- 「零外部依赖」：`grep -cE '(src|href)="https?://'` 在 `competitive-human.md` 上为 0。
- 「自检行不自相矛盾」（第 6 个缺陷）：红证跑的是**真生成器的一份副本**（会话目录内，`ROOT` 钉回真仓库），
  只把打印退回旧形态 → 守卫打印 `自检行自相矛盾 —— 等号前写「18 格」（行数），等号后是判定 token（和为 19）`，
  **`exit 1`（取 `PIPESTATUS[0]`，不是 grep 的退出码）**；真仓库重跑 → `exit 0`。修好的树同行为
  `18 行 / 19 个判定 = 赢11/输4/平3/未判1`。守卫读的是**已拼好的打印字符串**，不是源文件文本 ——
  所以它校验的是读者看到的那一行。**这次红证里先出了两次假红**（副本 `ROOT` 未钉回真仓库导致生成器在
  `materials/` 检查处早退；`bash … | grep …; echo $?` 读到 grep 的 0），两处都已修正，见下节。
- 「确定性」：重跑后 `md5sum -c` 三文件 OK（本次运行亲测，非承诺）。

## 人话文案表（旧技术描述 → 新第一层文案）

第一层每句 ≤25 字（机械扫描，最长 23）；下表左列摘自 `competitive.md` 第二层，右列是第一层的对应句。

| 旧（技术描述，第二层原样） | 新（第一层人话） |
|---|---|
| 单二进制、`CGO_ENABLED=0`、零运行时依赖 | 它只是一个文件，拷走就能跑。 |
| cargo workspace（~130 crates）+ 独立沙箱助手进程 | 别人装它要一堆东西。 |
| 记忆质量裁判：投票 / 纠正 / 反馈动词直接改检索排名 | 记忆能投票，用过的说有用。 |
| codex 的自动管线更自动，但没有这个用户级降级通道 | 错的那条，我们能让它降级。 |
| 像素级证据链：自研渲染 + 标注动词 + 无头截图 | 截图能证明，尺寸从图里读。 |
| 临时根 off-`/` 的机械断言（`AssertTempRootOffRootFS`） | 临时目录不在根盘，它会喊。 |
| 任务台账的父行闸门（父行没放行孩子不派发） | 任务有父子，父没完子不发。 |
| 无子 agent 委派（只有后台 shell job） | 我们没有子 agent。 |
| 对大型任务是**量级级**吞吐差，这是最硬的一条 | 一个任务只能一路干到底。 / 别人能同时开好几路。 / 这是最硬的一条。 |
| 无沙箱分级与审批分层（只有危险命令黑名单） | 沙箱没有分级，只有黑名单。 |
| 工具是枚举的，不是可扩展的（扁平表 + switch） | 工具数量写死，加工具要改三处。 |
| 记忆无自动整合，靠每任务主动 store | 记忆不会自己整理，要我催。 |
| 无内建定时/常驻调度（hermes 有 cron 子系统） | 没有定时任务，这是故意的。 |
| Rule 35 零依赖：不靠系统层的 cron/脚本掩盖问题 | 我们不想靠系统脚本活着。 / 脚本会盖住问题。 |
| 无人值守的周期作业要操作者自己起 systemd | 代价是无人值守要自己起服务。 |
| claude 本机无可读源码（245 MB 打包 ELF，非源码树） | 对手是 Claude，本机只有打包好的程序。 |
| 未判 = 在本次可读到的材料里查不到实现 | 查不到就写查不到，不猜。 |

## 判定口径对照表（旧 → 新）

| 旧词 | 旧词原意（反直觉） | 新口径（格子主文本） | 本次出现 |
|---|---|---|---|
| **赢** | 竞品该维度比 gopi 强 | **他们有，我们没有** | 11 格 |
| **输** | gopi 更强 | **我们有，他们没有** | 4 格 |
| **平** | 双方都有等价机制 | **两边都有** | 3 格 |
| **未判** | 本次能读到的材料里查不到对应实现 | **查不到（不是没有）** | 1 格 |
| — | 共 18 行 / 19 个判定 | | 19 |

> 数字由生成器从 `CELLS` 数出（`legacy_count` 数 token，不是数行：§3「定时 / 驻留作业」一行含两个判定
> —— 能力面「赢」、口径「输」）。旧文件的 12 个 **赢** 是读者最容易读反的部分。

## 第二层「一条没删」逐条核对

核对对象：`git show HEAD:docs/archive/demo/final/competitive.md`（旧，132 行）→ 新的 `docs/archive/demo/final/competitive.md`（135 行）。

| 旧文件的块 | 旧计数 | 新计数 | 核对方式 | 结论 |
|---|---|---|---|---|
| 首部：标题 / `doc-class` / 口径 3 条 | 10 行 | 10 行 + 新增口径 1 条 | 逐行顺序包含 | 未删（口径段 +1 条） |
| 测量对象与 rev 表 | 6 行 | 6 行 | awk 表行计数 | 未删（4 家一行不少） |
| 「怎么测的」grep 代码块 | 107 行 | 107 行 | 取块去围栏后 `wc -l` + `diff` | 未删（diff 只多 §0） |
| §1 codex 表格 | 9 行 | 9 行 | 逐行 | 未删 |
| §2 dsh 表格 | 8 行 | 8 行 | 逐行 | 未删 |
| §3 hermes 表格 | 7 行 | 7 行 | 逐行 | 未删 |
| §4 claude 未判说明 | 3 项 | 3 项 | 逐行 | 未删 |
| §5 双向差距（我们更强 5 / 更弱 6） | 11 项 | 11 项 | 逐行 | 未删 |
| §6 方法与边界 4 条 | 4 项 | 4 项 | 逐行 | 未删 |
| **合计** | **132 行** | 132 行（+§0 12 行 新增） | 顺序包含检查 + 无序集合检查 | **未找到 0 行** |

- 顺序包含检查：`miss=0`（awk，见 `Ran`）。
- 无序集合检查：`comm -23 <(旧行排序) <(新行排序)` → 0 行。
- 唯一变化是**新增**：`competitive.md` 的 §0（16 行含空行）、口径段的 1 条说明；以及 `index.html` 的两处十字。
- 新增 block 的位置与生成顺序：**§0 必须在记录重写之后 append** ——
  实测踩过：先 append 会被后面整块重写的 `competitive.md` 覆盖（而 `cmp` 仍然通过，因为第二层取的是上一版同样内容的文件），
  §0 会静默消失。现顺序：记录 → §0 → 人话版 → 自检。

## 收尾复核发现的第 5 个真实缺陷（已修 + 已红证）

**症状**：文件**顶部说明块**（第 5–8 行，在第一层之内）最长一句 **45 字、且带两个路径**
（`docs/archive/demo/final/build.sh`、`competitive.md`）—— 直接违反约束 6，却是全绿交付。

**5-why**：为何漏 → 长度/无路径扫描的起点是 `^# 第一层`，顶部说明块在它**之上**；
为何起点如此 → 写 gate 时把「第一层」当成一个标题，而它其实是**文件开头到存档标题之间的全部散文**；
为何没人发现 → gate 打印的 `longest 23 chars` 是**真话但只覆盖了一部分**，而报告里写成了全覆盖结论；
为何扫描会漏掉行 → 编号列表（`1. …6.`）也不在 `case -*` 分支里，六条诚实差距**从来没被量过**；
根因 → **一个报告「已检查」的 gate，其扫描范围是手写的、可以比约束窄，且没有任何东西量过这个范围**。

**修法（三处，都在生成器）**：
1. 顶部说明块改写成 4 句 ≤12 字的人话，**路径与生成器事实移入第二层的技术说明块**（存档层允许路径，且更有用）。
2. 扫描起点改为**文件第 1 行**，扫描分支增加 `>` 引用行与 `1.` 编号列表。
3. 新增**覆盖面守卫**：扫描到的行数必须 ≥ 第一层「非空、非标题、非表格、非引用、非分页线」的可读行数，不等即 `exit 1`。

**红证（三次，各自不同形态）**：

```
# 形态 1：顶部说明块退回旧的 4 行长句（含路径）
build.sh: plain face carries '.md' x1 (rule 6: no path/filename)
build.sh:   plain-sentence length (≤25 chars) : longest 47 chars ("两层同一个源头：`docs/archive/demo/final/build.sh`…"), over-limit 4
RED-1 exit=1

# 形态 2：只把扫描范围退窄回「从 # 第一层 起」（句子本身仍是修好的短句）
build.sh: 人话层长度扫描覆盖 57 行，第一层可读行有 62 行 —— 有句子没被扫到
RED-2 exit=1

# 形态 3：把第 1 条诚实差距从编号列表改成一条长 bullet
build.sh:   plain-sentence length (≤25 chars) : longest 31 chars ("没有子 agent，大任务的吞吐差一个量级，这一条最疼，我们认"), over-limit 1
RED-4 exit=1

# 修好的树
build.sh:   plain-sentence length (≤25 chars) : longest 23 chars ("「查不到（不是没有）」：我们没查到，不敢下结论"), over-limit 0
build.sh:   plain face (first screen)         : 47 bullet lines, no path/filename, no bare legacy 赢/输/平
GREEN exit=0
```

**第二次踩到的静默 no-op（值得记住）**：形态 3 的第一版注入用 `t.replace(old,new)` 且 **不检查替换是否发生** ——
锚点字符串多写了一个前导空格，替换成了 no-op，生成器跑的仍是**未注入**的树，于是打印「over-limit 0」`exit 0`，
看起来像「红证失败」。修法：注入后 `assert new in open(p).read()`，并把退出码一并打印。
**教训与 gate 同源：一个「没生效的注入」和一个「没扫到的行」是同一种错 —— 检查必须能证明它检查了东西。**

## 收尾复核发现的第 6 个真实缺陷（已修 + 已红证）

**症状**：自检打印行写的是 `18 格 = 赢11/输4/平3/未判1` —— `11+4+3+1 = 19`，一行**绿**日志里
两个数并列却互相矛盾（等号前的 `18` 是**行数**，等号后的四项是**判定 token 数**）。
读者无法判断是记录错了还是自检错了。

**5-why**：为何并列 → 打印行写的是「格 = 明细」，把一个行数放在 token 明细前面；
为何不同源 → `§3 定时 / 驻留作业` 一行写两个判定（能力面赢 / 口径输），所以 18 行 = 19 个判定，
而 `N_TOK`（19）当时只用在首屏那句「共 19 格（18 行）」里，**没进自检**；
为何没人发现 → 「逐格对照 18 行」「四词之和 19」两个数各自都对，**错的是把它们并排写成等式**；
根因 → 与缺陷 4、5 同源：**两个没有派生关系的数字并排出现即矛盾**，而 gate 只校验了各自的来源。

**修法**：自检行改印两个真值（`N 行 / M 个判定 = 四词明细`），并新增守卫：
① `四词之和 == 行内声明的「个判定」`；② `行数 == N_CELLS`；③ 若退回旧的 `A 格 = 四词` 形态，
**直接按「等号前是行数、等号后是 token」这条语义矛盾判红**（旧形态没有可用锚点，故按语义判，
不靠占位符数量）。`N_TOK` 由 `CELLS` 派生（`N_WIN+N_LOSE+N_TIE+N_UNK`），非手写。

**红证**（亲测，命令与输出 verbatim）：

```
# 会话目录内一份生成器副本：只把打印形态退回旧写法，并断言注入真的发生了
S=~/.gopi/sessions/<session-id>/red6
cp <repo>/docs/archive/demo/final/build.sh "$S/build.sh"
sed -i 's|^ROOT="\$(cd .*|ROOT="<repo>"|' "$S/build.sh"     # 副本里 ROOT 必须指回真仓库，
                                                             # 否则 materials/ 找不到、会在自检之前 abort
python3 - "$S/build.sh"   # assert 两个锚点都在，再 replace；之后 assert 新形态已在文件里
bash "$S/build.sh" 2>&1 | grep -E '自相矛盾'
build.sh: 自检行自相矛盾 —— 等号前写「18 格」（行数），等号后是判定 token（和为 19）: 「赢11/输4/平3/未判1」—— 两个数不同源却并排
RED6 exit=1
```

修好的树：`18 行 / 19 个判定 = 赢11/输4/平3/未判1`，`build.sh exit=0`。

**两次踩到的「假红证」，一并记下来**（都是「检查没证明它检查了东西」的形状）：

1. **副本里 `ROOT` 由脚本自身路径推导** —— 副本不在仓库里，于是 `materials/` 解析到不存在的位置，
   生成器在跑到自检之前就 `abort`（`MISSING screenshot …`）。第一版把 `grep` 的退出码当成生成器的退出码，
   于是「红证通过」的结论其实来自一次**早退**。修法：副本内把 `ROOT` 钉回真仓库（上面那行 `sed`），
   并单独打印生成器的真实退出码。
2. **把 `grep` 放进管道后 `$?` 是 `grep` 的** —— `bash … | grep … ; echo exit=$?` 读到的永远是 0。
   修法：`bash … 2>&1 | grep …; echo "exit=${PIPESTATUS[0]}"`（或在 `grep` 之前单独跑一次拿退出码）。
   这与第 5 个缺陷末尾的「静默 no-op」是同一族：**一个读数读错了对象的门，和一个没扫到行的门一样绿。**

## DOC-INDEX 行（Rule 36）
```
devdoc/competitive-human-rewrite.md	historical	historical (2 standing / 0 record): the doc's own title/name declares a past run's record, and no current artifact cites it as authority	
```
（`gopi doccheck --fix` 写入，随后 `gopi doccheck` 复验 → `the devdoc corpus is consistent`，exit 0。
本文件是这次改写的记录，不是长期主张，故归 historical。）
