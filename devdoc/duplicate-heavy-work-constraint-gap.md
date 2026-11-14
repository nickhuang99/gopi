<!-- doc-class: historical -->
# 为什么"禁止重复重活"的单对它自己的关账者毫无约束力 — 约束层诊断

Task `01a0f511-3d18-73a7-bf50-503394a44771` · branch `task/01a0f5113d1873a7bf50503394a44771`
基线 master `35fdec12`。**诊断报告：零产品代码改动**（本文件 + `devdoc/DOC-INDEX.tsv` 一行）。

一句话结论：**01a0f3f7 留下的全部东西都是"文本"，而文本只在"它覆盖到的那条命令"上起作用；
它覆盖的命令是「测试套件」，重复的那条命令是「扫描脚本」——两者不相交，所以约束为零。**
更硬的一层：**留下这些文本的 worker（pid 2704384）就是当天 08:5x 起重复扫描的那个 worker**——
同一 pid，同一台机，同一天，前后相隔约 2.5 小时。

本报告是**约束层/机制层上位**，不含糊：`01a0f510`（`devdoc/duplicate-full-scan-diagnosis.md`）
回答"那一次为什么起了两遍"（现象 + 该次的直接机制），本报告回答"为什么**已经写下**的约束
没有作用在**任何一次**上"——见 §5。

---

## 1. 现场复核（先证据，后结论）

### 1.1 同一 pid 三重身份（本报告的核心事实）

| 出处（逐字） | 读数 |
|---|---|
| `~/.gopi/sessions/01a0f3f7-2a01-73e4-a445-48a3a8accee2/output.log:1` | `# gopi task output — task 01a0f3f7-2a01-73e4-a445-48a3a8accee2 pid 2704384 …` |
| 同一 session JSONL `…/a3db235a-…jsonl` **行 244**（thinking 原文） | `So D2 (04:13:59) is in a DIFFERENT child session (the dead-holder resume, session 231291f0, pid 2704384).` |
| `~/.gopi/sessions/01a0f450-da2b-7cf5-b712-12566380ed56/output.log:1` | `# gopi task output — task 01a0f450-da2b-7cf5-b712-12566380ed56 pid 2704384 (find it again: …)` |

三行合起来：**2704384 既是（a）04:13:59 那个"第二组重叠 go test"的启动会话的 pid，
也是（b）跑完并关掉 01a0f3f7 这张单的 worker，还是（c）07:29 起 01a0f450 那张单、
在 07:36–07:39 起两份 `g7-verb-census.sh` 的那个 worker。**

时间线（全部取自上面两个 session 的 `timestamp` 字段）：

```
04:13:59.99  2704384 的 child session（231291f0）起第二组 go test        ← 01a0f3f7 诊断的对象
05:07       2704384 关掉 01a0f3f7（devdoc 56f6dd4d 落在 05:01:02）
05:10       2704384 写完 task-report.md（该 session 目录的 mtime）
07:28:22    5e3f5c63 落地（"把声明入口写进 bash/job 描述"）
07:29:54    2704384 起 01a0f450 的 child（census 那条）
07:36:53.08 该 child `bash` 起 `bash scripts/g7-verb-census.sh ./bin/gopi | head -60`
07:38:59.88 前者被 120s 空闲网掐掉（`Bash error: command killed: no output for 2m0s`）
07:39:03.25 同一 child 用 `job(action=start)` 起**同一条**脚本        ← 重复发生
07:44:48.21 它自己的 `ps` 抓到两条链（3242275 / 3245125 各自一个 `bash scripts/g7-verb-census.sh`，
            3245128 / 3245835 两个 `rg … --glob session.jsonl ~/.gopi/sessions`）
07:45:14.44 thinking 原文：`Two census runs are running concurrently`
```

证据命令：

```bash
head -1 ~/.gopi/sessions/01a0f3f7-2a01-73e4-a445-48a3a8accee2/output.log
head -1 ~/.gopi/sessions/01a0f450-da2b-7cf5-b712-12566380ed56/output.log
python3 - <<'P'   # 逐行取 JSONL 指定行的 tool_use / thinking
import json
p='/home/user/.gopi/sessions/01a0f450-da2b-7cf5-b712-12566380ed56/1543a6cb-be0c-46f4-9959-1c6cb0f86fee.jsonl'
for i,l in enumerate(open(p),1):
    if i in (108,111,115,118,202,203,205): print(i, json.loads(l)['timestamp'])
P
```

### 1.2 第二次重复的命令对（`01a0f450` 的 run，逐行）

| 行 | ts (+08:00) | 工具 | 命令 |
|---|---|---|---|
| 108 | 07:36:53.078 | `bash` (`timeout_sec:300`) | `cd …/gopi-01a0f450… && bash scripts/g7-verb-census.sh ./bin/gopi 2>&1 \| head -60` |
| 111 | 07:38:59.880 | result | `Bash error: command killed: no output for 2m0s` / `exit=-1` |
| 115 | 07:39:03.251 | **`job`** action=start | `…; bash scripts/g7-verb-census.sh ./bin/gopi > $EV/census.tsv 2> $EV/census.err; echo "rc=$?" >> $EV/census.err` |
| 118 | 07:39:03.766 | result | `job started: job-3` + 该命令逐字回显 |
| 202 | 07:44:48.218 | result（`ps`） | 3242272/3242275（job-3 侧）+ **3245125**（第二条 `bash scripts/g7-verb-census.sh`）+ 两个 `rg` |
| 203 | 07:45:14.440 | thinking | `Two census runs are running concurrently` |
| 205 | 07:45:14.440 | `bash` | `--- kill rogue census ---` / `kill 3245125`（**正向 PID 杀**，无信号送达证据） |

代价读数（同一 run 自己记的）：两条 `rg` 分别已跑 309s / 93s；`census.tsv` 与 `census.err`
在 07:39:03 建出后**一直为 0 字节**（`ls -l` 实测），即 job-3 这一份全程无产出；
最终被采用的结果写在 `census-authoritative.tsv`（07:46:45）。

---

## 2. 交付物审计表：01a0f3f7 留下了什么，它能拦住什么（逐条 file:line / 或"没有"）

| # | 它留下的东西 | 位置（可复核） | 它能拦住的 | 它**没能**拦住的 |
|---|---|---|---|---|
| D1 | 诊断文档 | `devdoc/overlapping-test-runs-diagnosis.md`（commit `56f6dd4d`，经 `beae180d` 进 master；`git branch --contains 56f6dd4d` → `master`） | 谁**主动去读**它 | 它自己的分类就是"不是权威"：`devdoc/DOC-INDEX.tsv:1771` = `devdoc/overlapping-test-runs-diagnosis.md  historical  historical (0 standing / 5 record): … no current artifact cites it as authority`。**没有任何当前产物引用它**，所以它对"起命令的那一刻"没有发言权。 |
| D2 | 提示词里的一句注记 | `internal/agent/loop.go:1190`（`bashJobTestEntryPointNote`），注入点 `:1317`（`bash` 描述）、`:1586`（`job` 描述）；commit `5e3f5c63`，`git merge-base --is-ancestor 31edde69 master` → 0 | 删掉这句话（`internal/agent/test_entry_point_description_gate_test.go` 会红）；以及**裸 `go test`** 这一条路径 | ① **它的作用域只有"测试套件"**——原文结尾是 `… Two \`go test\` sets in one run is the observed failure: overlap the scope, not the run`；对 `bash scripts/g7-verb-census.sh` 一个字都没有。② 它**根本没进过两次事故 run 的上下文**：`grep -c "Run the repo's declared test entry point"` 在 `01a0f3f7` 的 raw.log = **0**、在 `01a0f450` 的 raw.log = **0**（那个 run 在 07:36:48 的 HEAD 是 `a5f745c9`，到 07:51 才 merge master）——所以"这句注记没用"在本单里既不能证实也不能证伪，**能证实的是它的覆盖面与本案不相交**。 |
| D3 | 决策行 | `gopi task show 01a0f41f-6f68-70b8-b9bc-e200134ea575` → `status: completed`；其报告 `…/task-report.md` 明确写下：**route 2（范围/入口纪律）被选，route 1（在 test-budget.sh 里加并发上限）因"命令压根没进那个脚本"被否** | 记录了一个决定 | 决定的是"**只加文本**"。它把唯一可能的**机械**约束（并发上限）判为无效并放弃——这个判断在 **`go test`** 这条路径上是对的（两条命令都是裸 `go test`），但**它同时把一个"本来就该在别处"的问题一并关掉了**：本案的重复不是 `go test`，而是脚本全量扫描。决策行从此 `completed`，没有再派发后继。 |
| D4 | 记忆 | `01a0f3f7` session 行 352、461 的 `memory(action=store)`（"诊断重叠 go test 的取证套路"） | 下一次**主动 search** 时给线索 | 它是**描述性配方**，不是约束；且 `memory(action=search)` 是**可选**动作，不是起命令前的必经步骤——本次重复的 run 有没有搜过它，没有任何机制要求。 |
| D5 | 机械上限 | **没有** | — | `internal/tools/job.go` 全文无墙钟上限：`grep -n 'timeout\|Timeout\|Deadline\|Timer\|AfterFunc' internal/tools/job.go` → **0 条**（`job.go:127` 只有 `exec.Command`，`:138` 只有 `SysProcAttr{Setsid:true}`）。前台 `bash` 只有"120s 无输出"（`internal/tools/bash.go:46`），它掐的是启动器不是计算。 |
| D6 | 跨进程"谁在做同一件事"的答案 | **没有** | — | `internal/tools/job.go:98,103`：注册表是进程内单例（`newJobManager()` / `var theJobs = newJobManager()`），无跨进程枚举。第二个 gopi 进程无法知道第一个在扫同一棵树。 |

**结论（本单问题 (1) 的答案）**：01a0f3f7 留下的可拦第二次的东西 = **一句话（D2）+ 一篇文章（D1）+ 一条已完成的决策记录（D3）+ 两条记忆（D4）**，
其中 D1 被自己的分类标成 historical、D3 明确放弃了机械约束、D2 的**作用域与本案不相交**。
→ **对"同一台机上一次全量脚本扫描被起两遍"这件事：可拦住的东西 = 没有。**

---

## 3. Bond 起重活前会读到的**全部文本源**清单（本单问题 (2)）

"Bond"= 一个 worker 起的 child run。它起一条重活前，上下文里**只有**下表这些文本。
逐条给出位置、**现在的原文片段**、以及"应有成本约束却缺席"的判定。

| # | 文本源 | 位置 | 现在的内容（逐字片段） | 该有成本约束？ |
|---|---|---|---|---|
| T1 | system prompt `[BASH EFFICIENCY RULES]` | `internal/agent/loop.go:801` | `1. Chain steps into ONE command (the project's own build+test gate); parallel batches are separate and fine.` | 缺席（只讲"合并成一条"，不讲这条有多贵） |
| T2 | system prompt `[JOB RULES]` | `internal/agent/loop.go:876` | `1. Outlives one bash call? job(action=start).` | **缺席，且方向相反——这行在推荐那条无上限的路径** |
| T3 | system prompt `[EFFICIENCY RULES]` | `internal/agent/loop.go:717` | `1. PARALLEL: all independent tool calls in ONE response; sequence on dependency.` | 缺席（"并行"是唯一被鼓励的维度） |
| T4 | system prompt `[ENGINEERING RULES]` | `internal/agent/loop.go:887` | `7. 依赖预算: ZERO-DEP default.` | 缺席（"预算"在这里只指持久依赖，不指 IO/墙钟） |
| T5 | `bash` 工具描述 | `internal/agent/loop.go:1316`（+ 注记 `:1317`） | 结尾注记：`Run the repo's declared test entry point — make test (whole tree) or make test-budget PKGS='…' …` | **部分**：只覆盖"测试套件" |
| T6 | `job` 工具描述 | `internal/agent/loop.go:1584`（+ 注记 `:1586`） | 正文只讲 `action=start/output/kill` 的生命周期与"detached in its own session — immune to bash timeouts" | **★ 该有而完全没有（点名项，见下）** |
| T7 | 项目上下文 `GOPI.md`（经 `LoadProjectContext` 进 prompt；raw.log 实测 106 次出现 `## Rule 6: Isolated Work Directory`） | `GOPI.md:523`–`GOPI.md:544`（Rule 26） | `A duplicate dispatch costs one agent's time; a stalled repair leaves the defect live.` / `Dispatching the same work twice is allowed and expected.` | 缺席，且**语义上许可重复**（Rule 26 管的是"派发"，本案是"同一 run 内起两遍"，但读 prompt 的模型只会读到"重复是被允许的"） |
| T8 | 本行的 briefing | 每行自带 | `01a0f510` 的 briefing 里问过成本上限（"问：worker 起 child 之前，有没有任何地方检查…"） | 局部（只在那一行；`01a0f450` 的 briefing 里**零**成本约束——见 §6 证据的 briefing 全文） |
| T9 | `memory(action=search)` 命中 | `01a0f3f7` 行 352/461 存的两条 | 取证套路（怎么证明重叠、怎么测字节落点） | 无（描述性，非规定性） |
| T10 | devdoc 语料 | `devdoc/overlapping-test-runs-diagnosis.md:1` = `<!-- doc-class: historical -->` | 见 D1 | 无（自标 historical，明确不当权威） |

### 3.1 点名：`internal/agent/loop.go:1584`（`job` 工具描述）——**该有成本约束，现在一个字都没有**

现文（逐字，行 1584 起）：

```
Name: toolnames.ToolJob,
Description: "Manage a background shell job — the action parameter selects the operation (start | output | kill).
action=start runs a command in the BACKGROUND, detached in its own session — immune to bash timeouts (bash kills
its whole process group on timeout, which would take a shared background process down with it). Returns a job id +
stdout/stderr file paths. Use for long-running commands (builds, tests, servers, scripts). Do NOT poll: …"
```

**为什么是它**：`job(action=start)` 是本案**两次重复**里第二次的载体（07:39:03 那一行），也是
01a0f3f7 里第一组 `go test` 的载体（04:10:27 的 `job`）。它有三重叠加的缺陷：
① 描述**主动**说 `Use for long-running commands (… scripts)`，把脚本拉进来；
② 描述说它 `immune to bash timeouts`——**把"没有上限"当卖点讲**；
③ 它后面挂的那句注记（`bashJobTestEntryPointNote`）**只谈测试套件**，于是"我这条是扫描脚本"
正好落在两句话之间的缝里。加上 D5（`job.go` 无墙钟上限）与 D6（无跨进程可见性），
"起一条 5 分钟级全量扫描"这个动作在**全部文本源里没有任何一处提醒过代价**。

**为什么"再写一句成本上限"不够（briefing 的硬边界）**：因为已经证明这行的**覆盖面**决定一切——
D2 那句话写得够重（`is the observed failure`），但它写在"测试套件"这个作用域里，
对本案的命令**根本没接上**；且它落地（07:28）时两次事故 run 的 prompt 里出现次数是 **0**。
所以本报告把"文本"列为**必要不充分**，把机械/可见性项（§4）列为主项。

---

## 4. 可见性缺口现状 + Rule 35 合规修法（本单问题 (3)）

### 4.1 现状：本机"正在做几件重活"**没有任何查询面**（逐条实测）

| 候选面 | 现状 | 证据 |
|---|---|---|
| `gopi scratch-gc deps`（Rule 35 的只读常驻面） | 报了常驻进程，但**按 origin 过滤**，只看 gopi 形状的程序 | `cmd/agent/cli_scratch_gc_deps.go:416` `if origin != depsOriginGopi { continue }`；今日实跑 = `… summary scheduled=0 units_gopi=0 units_foreign=33 resident=6 gopi=6 foreign=33 total=39`，而同一时刻 `pgrep -x rg | wc -l` = 0、真正在扫盘的 `rg`/`find` **一个都看不到**（它们不是 gopi 形状） |
| `/proc/pressure/io` | **没有任何代码路径读它**，只有两行注释 | `internal/tools/firstopen_sync_decision.go:11`、`internal/tools/ledger_dsn.go:177`（`grep -rn 'pressure/io' internal/ cmd/` → 仅这两条注释） |
| `job(action=start)` 的回执 | 只报**自己的** id/命令/两个文件路径，不报主机状态 | `internal/tools/job.go:213` `job started: %s\ncommand: %s\nstdout: %s\nstderr: %s` |
| 模型自己的 `ps` | **这就是唯一的仪器** | 01a0f450 行 202 的 `ps` 与行 203 的 thinking `Two census runs are running concurrently`——**是模型手工发现的，不是系统告诉它的** |
| 跨进程注册表 | 无（D6） | `internal/tools/job.go:98,103` |

### 4.2 修法建议（Rule 35 合规：不新增常驻 / 不新增状态文件 / 不做成闸门）

**R1（建议采纳，主项）— 把"本机重活读数"挂进 `job` 启动回执（既有通道，零新工件）。**

形状：在 `internal/tools/job.go:213` 的 `job started:` 文本里，追加**一行**现场读数：

```
host: io full avg10=NN.N%  runnable=N  heavy-walkers=N (names…)
```

- `io full avg10` ← `/proc/pressure/io`（内核共享设备级，`some` 会恒高，用 `full`）。
- `heavy-walkers` ← 一次 `/proc` 扫描里形状匹配的进程数（本案的形状：
  `rg|find|grep` 且 argv 里含 `~/.gopi`、`-r` 递归、或 `go test|go build`）。
- **为什么合规**：没有 cron/daemon/状态文件/锁；进程一退什么都不剩；它**只告知，不拒绝**——
  一旦做成"超阈值就拒绝启动"，就是一个隐式调度器（Rule 35 禁止、01a0f510 §5.4 也已划出这条线）。
- **代价（实测）**：一次全 `/proc` 扫描（254 个 pid，含读 `cmdline`）= **0.173 s**
  （`for p in /proc/[0-9]*; do cat $p/cmdline; done` → `real 0m0.173s`）；纯 `comm` 同样 0.170 s。
  开发面：一个函数 + 一行字符串 + 一个 gate 测试（形状照抄
  `internal/agent/test_entry_point_description_gate_test.go` 的做法：钉住这行必须渲染出来）。
  **不做**的事：不改 `parallelSafeTools`（`loop.go:5535`，只含只读工具）、不抬任何 timeout、不加锁。
- **下一任读者如何发现它**：这行**每次 `job(action=start)` 都印**在模型的工具回执里——
  与本案"模型自己 `ps` 才发现"相比，发现点从"事后手工"提前到"起命令的那一刻"；
  另有 gate 测试钉住它的存在，`gopi job -h`（工具描述）里补一句指向它。
- **已知代价/风险（诚实列出）**：每个 job 回执多一行（噪声）；`heavy-walkers` 的判据是一张
  形状表，会漏（漏报=静默，必须写成"至少 N 个已知形状"而不是"共 N 个"）；
  它**不能阻止**重复，只能让重复在**发生的同一秒**可见——这正是本报告主张的上限
  （拒绝式闸门已被 `01a0f41f` 与 Rule 35 双双否掉）。

**R2（配套，弱项）— 把 D2 那句注记的作用域从"测试套件"扩到"任何全树命令"。**
现文（`internal/agent/loop.go:1195`）逐字：`Two \`go test\` sets in one run is the observed failure: overlap the scope, not the run`。
把它改成对"同类全树命令（`go test ./...`、`rg … ~/.gopi/sessions`、任何 `find`/`du` 全树遍历）"同样成立。
**代价**：一句话 + gate 测试改一行。**但它单独不够**：见 §3.1 末段（作用域与落地时机都曾错过），
所以它只能作为 R1 的配套，不能作为本单的答案。

**明确不做（并说明为什么）**：不做锁 / flock / pid 文件（新持久依赖，且对 `bash -c` 起的
子进程本来就看不见）；不做"只在 `scripts/test-budget.sh` 里的并发上限"（`01a0f41f` 已判：
命令压根没进那个脚本）；不新增 cron/daemon/systemd 单元；不削弱 `parallelSafeTools`；
不抬 `bashDefaultTimeoutSec`/`bashNoOutputTimeoutSec`。

---

## 5. 本单 vs `01a0f510`：**上位（约束层/机制层），不是同层**

| | `01a0f510`（`devdoc/duplicate-full-scan-diagnosis.md`，commit `7c8054f9`，master `35fdec12`） | **本单 `01a0f511`** |
|---|---|---|
| 提问 | "同一条命令为什么被起了两遍"——**那一次** | "`01a0f3f7` 这张已经完成的单，为什么对**任何一次**都没有约束力"——**约束语料为何失效** |
| 层级 | 现象层 + 该次的直接机制（`bash` 空闲网掐不住 pipeline、`job` 无上限、负 PID 杀不到孤儿） | 约束层：交付物审计（§2）、文本源清单（§3）、可见性缺口（§4）、上位/同层判定（本节）、拍板点（§7） |
| 证据源 | 该 run 的 JSONL 行 108/111/115/202/203/205 + 现场测量 | **相同**（本报告重引，逐行给出）+ **01a0f3f7 的交付物与分类、DOC-INDEX 行、`job.go` 的零上限、`cli_scratch_gc_deps.go:416` 的过滤** |
| 产物 | 三条缺口清单 + 4 条修法建议 | **01a0f3f7 能拦住什么/拦不住什么（逐条 file:line 或"没有"）**、**Bond 起命令前的全部文本源**、**点名 `loop.go:1584` 这块该有而没有**、**同一 pid 三重身份**、**是否拍板** |
| 结论的作用面 | 改那条脚本 / 修那个杀法 | 改**约束的落点**（文本放哪、读数放哪），并回答"要不要拍板" |

**判定的判据（可复核）**：`01a0f510` 的结论是"这次为什么重复"（§2 根因链 5-why，
主语是 `bash`/`job`/`kill` 三个机制）；本报告的结论是"**为什么写下来的约束没接上**"
（主语是交付物、文本源、分类与可见性）。前者在任何一次重复里都要重算；后者一旦成立，
对**所有**重复形态成立。**因此是上位。**

（不重不漏地说明：`01a0f510` §2 ③ 的"跨进程无枚举"与本报告 §4.1 的"跨进程注册表单例"
是**同一事实的两个引用**——本报告不重新证明它，只把它接到"可见性缺口"这一层上；
若要"同层"，那只可能是在说这一条，而它占本报告 1/5 的篇幅。）

---

## 6. 是否需要用户拍板（本单问题 (5)）——**需要，三点**

1. **R1 是否采纳**：它改变每一次 `job(action=start)` 的工具回执文本（所有 run 都能看到，
   0.17 s 代价，零新工件）。**建议：采纳**——它是"让重复在同一秒可见"的最低成本落点。
2. **是否要一个真正的"拒绝式闸门"**（例如本机同时只允许一件全量重活）。
   **建议：不要**——`01a0f41f` 已判 route 1 无效（命令没进那个脚本），Rule 35 也禁止
   以新持久依赖（锁/状态文件）实现；且它会把 gopi 变成一个隐式调度器。
   **但这条只能由用户拍板**，因为它关掉的是"更强的保证"。
3. **GOPI.md 是否要加一条成本规则**（Rule 27 的邻居：Rule 27 禁"为了测试制造设备负载"，
   本案是"为了取证制造全树遍历"）。规则是**常设约束**，落地即意味着有人要维护它的门禁。
   **建议：先不加**——先用 R1 让事实可见；若 R1 之后重复仍然发生，再谈规则。

其余无需拍板：本单零代码改动、零新工件、不影响任何既有门禁。

---

## 7. 证据（self-filed，逐条可复算）

```bash
# §1.1 同一 pid 三重身份
head -1 ~/.gopi/sessions/01a0f3f7-2a01-73e4-a445-48a3a8accee2/output.log
  → # gopi task output — task 01a0f3f7-… pid 2704384 …
head -1 ~/.gopi/sessions/01a0f450-da2b-7cf5-b712-12566380ed56/output.log
  → # gopi task output — task 01a0f450-… pid 2704384 …
# 01a0f3f7 的 session JSONL 行 244（thinking）逐字含 "session 231291f0, pid 2704384"

# §1.2 第二次重复的命令对（行号即引用）
S=~/.gopi/sessions/01a0f450-da2b-7cf5-b712-12566380ed56/1543a6cb-be0c-46f4-9959-1c6cb0f86fee.jsonl
# 行 108 bash / 111 killed / 115 job start / 118 job started / 202 ps / 203 thinking / 205 kill
ls -l ~/.gopi/sessions/01a0f450-…/census.tsv ~/.gopi/sessions/01a0f450-…/census.err
  → 两个文件均 0 字节，mtime 2026-10-01 07:39:03
ls -l ~/.gopi/sessions/01a0f450-…/census-authoritative.tsv
  → 1331 字节，mtime 07:46:45

# §2 D1/D2/D3/D5/D6
git show --stat 56f6dd4d | tail -3            → devdoc/overlapping-test-runs-diagnosis.md, 341 行
grep -n 'overlapping-test-runs-diagnosis' devdoc/DOC-INDEX.tsv
  → 1771: … historical  historical (0 standing / 5 record): … no current artifact cites it as authority
git branch --contains 56f6dd4d | head -1      → + master
git log --oneline -1 5e3f5c63 --format='%ad' --date=iso
  → 2026-10-01 07:28:22 +0800
git merge-base --is-ancestor 31edde69 master && echo yes   → yes   （07:36:53 进 master）
grep -n 'timeout\|Timeout\|Deadline\|Timer\|AfterFunc' internal/tools/job.go   → 无命中
gopi task show 01a0f41f-6f68-70b8-b9bc-e200134ea575 | grep status:   → status: completed

# §3 文本源清单（行号在 master 35fdec12 上读）
internal/agent/loop.go:717, 801, 876, 887, 1190, 1316/1317, 1584/1586
GOPI.md:523–544（Rule 26）；grep -c 'Rule 26' <census raw.log> → 106（每轮一次）

# §4.1 可见性缺口
gopi scratch-gc deps | tail -1
  → deps summary scheduled=0 units_gopi=0 units_foreign=33 resident=6 gopi=6 foreign=33 total=39
grep -rn 'pressure/io' --include='*.go' internal/ cmd/
  → 仅 internal/tools/firstopen_sync_decision.go:11 与 internal/tools/ledger_dsn.go:177（注释）
sed -n '416p' cmd/agent/cli_scratch_gc_deps.go   → if origin != depsOriginGopi {
sed -n '213p' internal/tools/job.go
  → return fmt.Sprintf("job started: %s\ncommand: %s\nstdout: %s\nstderr: %s",

# §4.2 R1 的代价（直接测量，非推测，无设备负载）
ls -d /proc/[0-9]* | wc -l                   → 254
time (for p in /proc/[0-9]*; do cat $p/cmdline 2>/dev/null >/dev/null; done)
  → real 0m0.173s   user 0m0.139s   sys 0m0.043s

# 事故 run 的 prompt 里那句注记出现次数（§2 D2）
grep -c "Run the repo's declared test entry point" <01a0f3f7 raw.log>  → 0
grep -c "Run the repo's declared test entry point" <01a0f450 raw.log>  → 0
grep -c "Run the repo's declared test entry point" <本 run raw.log>     → 32

# 构建 + 工作区
go build ./...                               → BUILD_ALL_OK (exit 0)
git status --porcelain                       → （close-out 时为空）
```

---

## 8. 与 01a0f510 / 01a0f3f7 的边界（本单未触碰）

- 未改、未取消、未重写 `01a0f3f7`（`gopi task show` 仍为 `completed`；其分支
  `task/01a0f3f72a0173e4a44548a3a8accee2` 已按合并流程清掉，其内容在 master 上：`56f6dd4d`）。
- 未改任何产品代码；本分支只新增本文件与它的 `DOC-INDEX.tsv` 行。
- 未新增 scheduler / daemon / cron / 状态文件 / 锁（Rule 35）；§4.2 的建议全部是"文本 + 一次 `/proc` 读"。

## 关于本文件自己的分类（诚实标注）

`gopi doccheck --fix` 把本文件写进 `devdoc/DOC-INDEX.tsv:1006` 为 `historical`
（`historical (2 standing / 5 record): the doc's own title/name declares a past run's record, and no current artifact cites it as authority`），
与两份同类诊断文档（`overlapping-test-runs-diagnosis.md`、`duplicate-full-scan-diagnosis.md`）
一致。**这不是本报告的漏洞，而是 §3 论点的一个实例**：本文件属"某次运行的记录"，
按语料自己的规则**不具权威效力**——所以它 §2/§3/§4 里那些**关于当前代码的断言**
（`internal/tools/job.go` 无墙钟上限、`internal/agent/loop.go:1584` 的现文、
`cmd/agent/cli_scratch_gc_deps.go:416` 的 origin 过滤）都**自带 file:line 与命令**，
下一任读者**直接复核代码**即可，不需要"引用本文件"。这也是本报告把 R1 落在
**工具回执**（每次起命令都会印）而不是**落在文档**上的原因。

## Not Done (honest)

- **§4.2 的 R1/R2 只写形状与代价，未实现**——briefing 是 diagnose-first（不改产品代码），
  且 R1 是否采纳是 §6 的拍板点。
- **没能复算 08:5x 那一刻的 `/proc/pressure/io` 现场值**：那时段已过，事后采样是主机基线
  （01a0f510 §3 已证同一指标在无重叠时反而更高）。本报告因此不引用任何 PSI 数字作为重叠代价。
- **`heavy-walkers` 的判据没写成完整形状表**（只给了本案涉及的 `rg`/`find`/`go test`/`go build` 四类），
  这是 R1 落地时要定的一张表，本报告不做设计冻结。
- **未在事故 run 之外的其它历史 run 上做同形普查**（本次只复核了 `01a0f3f7`、`01a0f450`
  与本 run 三个 session 的 raw/jsonl）。
