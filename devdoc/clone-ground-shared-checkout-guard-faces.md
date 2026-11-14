<!-- doc-class: normative -->
# 共享 checkout 守卫的三个面：一个判据，四处接线，两份清单

本文记录 01a1957a 事故的**剩余缺口**如何被补上，以及为什么判据只有一份。
事故本身（reframe 丢掉 harness clauses + bash 允许在共享 checkout 建分支）已经在
commit `11d5513d` 修掉；本文讲的是那次修复**没有覆盖到**的两半，和一个悬空的清单。

## 1 缺口是什么

`11d5513d` 交付的守卫只覆盖 bash 工具的 `git checkout -b|-B` / `git switch -c|-C`。
事故的另外两半完全没被机器看见：

| 缺口 | 事故里的形状 | 修前行为 |
|---|---|---|
| 写路径 | run 在 MAIN checkout 里 **编辑** `cmd/agent/cli_scratch_gc.go` | `write`/`edit` 照常落字节 |
| `worktree add` 拼写 | 建分支的第三种写法 | bash 守卫看不见，直接放行 |
| reframe 清单 | `reframePreservedClauseLabels` 是手维护的 | 明天新增的 clause **静默**被丢 |

第三条是「缺陷的下一层」：`reframePreservedClauseLabels` 本身就是为了修
「reframe 丢 clause」，但一张没人检查的清单等于一个希望 —— 新增 clause 的作者
忘了加，第一次 reframe 就丢掉它，和 01a1957a 一模一样。

## 2 判据只有一份（Rule 13/14）

```go
// internal/tools/clone_ground_branch_create_guard.go
func cloneGroundSharedCheckoutVerdict(dir, ground string) (string, bool)
```

它回答唯一的那个问题：**这个目录是本机声明的共享 checkout，且在 run 自己的 ground
之外吗**。「声明」= settings.json 的 `repos` 表，通过既有的
`config.ResolveMergerRepos` 读取（merger 每条 tick 都扫这张表，Rule 12），
**没有新增配置键**（Rule 35）。

三个面各自渲染自己的句子，但**不各自做判断**：

- `git checkout -b|-B` / `git switch -c|-C` → 判 `cloneGroundGitRepoDir(clause)`
- `git worktree add -b|-B` → 判**两个**目录：命令所在的 checkout（在这里建分支
  是同一个缺陷）和 git 要创建的 target（Rule 29 的subject：它写在 checkout 之外）
- write/edit → 判 `filepath.Dir(path)`，接在 `atomicWriteFile` 与 `editImpl` 上

## 2b 目录从哪儿读：shell 的 `cd`，还是 git 自己的 `-C`（实测踩到的洞）

**目录有两个来源，而最初的守卫只读了 shell 那一个** —— 这是我在本任务里
自己踩到、实测复现的洞，而且它在**既有面**（`checkout -b`）上同样存在，
不是新面引入的：

```
$ git -C /home/user/work/gopi checkout -b task/probe-dashc
Switched to a new branch 'task/probe-dashc'          EXIT=0
$ git -C /home/user/work/gopi worktree add -b task/probe-r2 /home/user/workspace/_probe-r2
Preparing worktree (new branch 'task/probe-r2')      EXIT=0
```

两次都带着 `GOPI_RUN_GROUND`、共享 checkout 已声明、守卫已接线 —— 分支**真的**
建在共享 checkout 里，HEAD 真的离开了 integration 分支。发现它的方式很直接：
一条**本应被拒**的探针返回了 0，而那条探针本身就用 `git -C` 写（夹具也是这么写的）。

一个「加一个 `-C` 就绕过去」的守卫不算守卫。所以目录改由 `cloneGroundGitRepoDir`
回答，优先级是：

```
git 全局 -C <dir>  >  shell 的 cd  >  bash 工具的 workdir
```

`-C` 的 operand 用**包里已有的那个** option scanner 读
（`refGuardScanOptions` + `refGuardDashCShort`/`gitDashCDirOption`），而且只读
**子命令之前**的词 —— 和 `refGuardResolve` 用同一个规则，原因也一样（实测）：
扫全句会把 `git switch -C <name>`（子命令自己的 force-create）读成 `git -C <dir>`。

四条回归臂钉住它（`git -C` 建分支 / `-B` 强制 / `-C` 压过 cd / `-C` + worktree add），
外加两条**不许收紧**的边界（`-C` 指向自己 ground 放行、`-C` 只读放行）。
去掉 `cloneGroundGitRepoDir` 后这四条全部创建了分支（`RED_EXIT=1`，实测）。

## 3 四个「不判」的边界（都是刻意的）

1. **没有 `GOPI_RUN_GROUND` 的进程** —— 操作员自己的 shell、`gopi chat`、无 repo 的行。
   它们的 cwd 不是任何人做出的声明，拒绝操作员自己的 `git checkout -b` 等于守卫
   自己发明一个 owner。**这是硬约束，不是宽松。**
2. **run 自己 ground 里的路径/分支** —— ground 的日常用法。
3. **未被声明为 `repos` 条目的目录** —— 守卫读声明，不发明共享树。
4. **读操作** —— `git checkout <已有分支>`、`git status`、`read` 一律不动。

逃生口复用既有的 **`GOPI_ALLOW_DANGEROUS=1`**，和 bash 面同一个 ——
一个判断两个逃生口，就会让操作员的同一个决定开始有两种含义。

## 4 两个开关，一个都不能漏

bash 面的条件是 `RejectDangerous && !escapeDangerous`。写路径最初只检查了
`escapeDangerous`，**漏掉 `RejectDangerous`** 就出现了两个后果：

- 语义上，同一个判断的两个面回答两个不同的开关；
- 实际上，`internal/tools/deadcode_behavior_test.go` 会把 `RejectDangerous` 置 false，
  于是新测试是**顺序依赖**的 —— 它测的是邻居，不是守卫。

（Rule 15：一个没有观察到它声称内容的 pass，比一个红更糟。）

## 5 reframe 清单的门禁（`internal/agent/reframe_dropped_clause_labels_test.go`）

判据不是「名字以 Label 结尾」（那样会捞进 `TaskShowDeliverableRepoLabel` 这类
`task show` 字段、`TaskGroundChannelLabel` 这类 format 串），而是**两条同时成立**：

```
导出 + 名字以 "Label" 结尾  ∧  解析后的值以 "[" 开头
```

值是 `go/ast` **常量不动点解析**出来的（`A + "\n" + …`、引用另一个常量都能解得开），
`fmt.Sprintf` 形状取第一个格式动词之前的**标记头**。

**这个判据被数据验证过**：在本 rev 上跑出 **15** 条，而两份手写清单正好是
5 保留 + 10 丢弃 —— 精确相等，没有多余也没有遗漏。

三个断言，缺一不可：

1. **census 的每一条**必须在保留清单或丢弃清单里 → 新增 clause 忘分类 = 红，
   报错**点名那个常量**。
2. **反方向**：清单里的一条如果解析不出任何 census 成员（改名/删除/不再以 `[` 开头）
   也是红 —— 那是一条关于「已经不存在的东西」的决定。
3. **读地板**（`reframeClauseLabelReadFloor = 12`）：扫到的条数低于地板就红。
   一次「什么也没读到」的扫描必须能和「干净」区分开（Rule 15）。

丢弃清单里每一条都必须带**理由**，空理由是静默决定，和不存在等价。

### 它测不到什么（如实）

判据是「导出 + `Label` 后缀 + `[` 开头」这个**约定**。一条不是常量的 clause
（运行时拼出来的）、或者不叫 `…Label` 的、或者不以 `[` 开头的，抓不到。
所以 `reframe_ground_clauses.go` 的注释要求新 clause 遵守这个约定 —— 约定由人去守，
**约定之内由门禁去守**。

## 6 红证明（两半都实测）

| 红证 | 手法 | 结果 |
|---|---|---|
| 三个面摘接线 | `go test -overlay`（工作树全程干净） | `RED_EXIT=1`，4 个 `--- FAIL`（写、编辑、worktree×2） |
| 接回 | 不带 overlay | `EXIT=0`，`ok` |
| 门禁：植入明天的 clause | 真在 `internal/tools` 加一个常量，清单不动 | `GATE_RED_EXIT=1`，点名 `TaskBriefingFutureClauseLabel` |
| 门禁：从保留清单摘一条 | overlay | `PRESERVED_RED_EXIT=1`，两个断言红 |

overlay 的价值：**工作树不脏**。若用「改文件→跑→改回」，
`git status` 会有一段时间是脏的，而脏的工作树不被任何 merge 路径拾取（Rule 30）。

## 7 真实入口证据

不是单测，是**本 run 自己的 bash / write 工具**（真进程带 `GOPI_RUN_GROUND`）：

```
cd /home/user/work/gopi && git worktree add -b task/guard-probe /home/user/workspace/_guard-probe
→ Bash error: dangerous command rejected: cannot create branch "task/guard-probe" in
  /home/user/work/gopi: that directory is the declared shared checkout …

write /home/user/work/gopi/.guard-probe.txt
→ write: … resolves to /home/user/work/gopi, inside the declared shared checkout …
  while this run's own ground is /home/user/workspace/01a19602-… Nothing was written.
```

事后测量（共享 checkout 未被污染）：

```
ls /home/user/work/gopi/.guard-probe.txt        → No such file or directory
ls -d /home/user/workspace/_guard-probe         → No such file or directory
git branch --list 'task/guard-probe' | wc -l    → 0
git status --porcelain | wc -l                  → 0
```

同一个守卫**也挡了我自己**：本 run 早先用 `git worktree add -b` 在 ground 外建的
探针 —— 那正是「没有 `GOPI_RUN_GROUND` 的进程照旧放行」这条约束的证据（那条调用
来自一个没有 ground 的父进程）。探针已 `worktree remove` + `branch -d` 清理干净。

## 8 一个 gate 之间的小瑕疵（如实记录）

`disallowedCombination`-类的「接回即绿」证据里，`TestReframe_SwapKeepsHarnessClausesVerbatim`
在 overlay 摘掉 `TaskBriefingAuthorityLabel` 时**仍然 PASS**。原因是该 fixture 的
authority clause 正文里包含 announcement clause 的正文（`THIS RUN ALREADY HAS ITS
WORKTREE`），所以移掉 authority 标签后，它作为 announcement 段的一部分被保留了下来，
断言仍然满足。**接住这个变化的是同一批里的门禁测试**（它点名了缺的那条），
所以缺陷不会被漏掉；但这条 fixture 本身对该标签的重叠是有盲区的，值得记在这里。
