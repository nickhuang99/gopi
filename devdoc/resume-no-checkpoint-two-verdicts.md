<!-- doc-class: normative -->
# 两种「没有 checkpoint」不是一件事 —— resume 拒绝的实测判据

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Row:** 01a1a885 · **Scope:** `gopi worker --resume <task-id>` / `gopi session resume <task-id>` 的无 checkpoint 拒绝路径（`cmd/agent/cli_resume_no_checkpoint.go`）。

## 问题：一句措辞，两种相反的情形

`gopi worker --resume <task-id>` 对下面两种行给出**同一条**拒绝：

```
resume target %q is task %s, which has no session checkpoint to resume: its run
directory (%s) holds no run whose raw log still exists, and no run of it
recorded one. …
```

| 情形 | 语义 | 正确出路 |
|---|---|---|
| (a) **从未启动** —— 行被提交（甚至被 claim、被派发），但没有任何一次运行开过会话 | 没有可续的东西 | **新 UUID 新行**（裁决 #27273） |
| (b) **跑过，checkpoint 丢了** —— 行真的跑过，run log 记录了一个 raw log 路径，文件已被清理或 profile 搬走了 | resume 是**正确动词**，只是证据没了 | 同一份工作重做一次 |

把 (a) 渲染成 (b) 的措辞，读者一半的时间被送去错的补救路径 —— 这是 Rule 15 意义上的缺陷：两种状态由一次**测量**区分，拒绝文本就有义务把这次测量做出来并写出来。

## 判据：run log，而不是 output.log

一次运行在**发出任何请求之前**就打开自己的会话日志（`*.jsonl`），所以：

```
no run log AND no *.raw.log  →  这条行没有任何一次运行开始过     (a)
a run log exists             →  行跑过；缺的是 checkpoint        (b)
```

本机实测（`~/.gopi/sessions` 下**没有 `*.raw.log`** 的目录，问它有没有 `*.jsonl`）：

```
NO_RAW_LOG: dirs_with_jsonl(run began)=975  dirs_without_jsonl(nothing started)=1582
```

两个都是大样本，不是从一次事故里抠出来的边角。

### brief 建议的第二半判据：实测后**不采用**

brief 建议用「run 目录里没有任何子进程启动后的记录」作为佐证，具体形态是 output log 里没有 `executor cwd →` 行。**两半都实测失败**：

```
$ grep -c 'executor cwd →' …/output.log      # 这些目录都没有 run log
01a19075-80b5-7a85-b0df-02a55f1c8b16 → 1     # 而这一行「从未运行」
01a1a60e-b419-76d3-ae2c-0a368bdc09f3 → 1     # 子进程环境准备好之后什么也没有
   54 个带 output.log 的无 run log 目录里, 52 个没有这一行
```

`01a19075` 的「从未运行」有独立证据（记忆 #22282）：目录里只有 `attachments/ metadata.json output.log`，没有 `*.jsonl`、没有 `*.raw.log`，ground `ahead=0 / behind=61`，工作树干净，零产出。

**为什么这条线不该用**：`taskCwdResolvedFmt` 是 **worker wrapper** 写的，它在地面解析之后、spawn 子进程之前落盘（`cmd/agent/cli_tasks.go:3307`，常量在 `task_child_env.go:72`）。所以它记录的是「wrapper 走到了这一步」，**不是**「一次运行开始了」。用它，会把「已启动但立刻死掉」的行判成跑过 —— 正是要在 (a) 与 (b) 之间分辨的那件事。run log 由**运行侧**写，是诚实的判别器，所以只用它。

## 实现

`cmd/agent/cli_resume_no_checkpoint.go`：

- `resumeRowEvidence` —— 一次 `os.ReadDir` 量出来的目录事实（`Exists` / `Entries` / `RunLogs` / `RawLogs`），判定与措辞读**同一份**测量，不可能各自看到不同的目录。
- `(e resumeRowEvidence) neverStarted()` —— 唯一的谓词：no run log AND no raw log。
- `(e resumeRowEvidence) verdict()` —— `never-started` / `ran-no-checkpoint`，两个字面量各一个具名常量（Rule 1b），拒绝文本把它印出来，测试断言它。
- 拒绝文本两个具名常量：`resumeNoCheckpointNeverStartedFmt` / `resumeNoCheckpointRanFmt`。目录清单**计数并封顶**（`resumeNoCheckpointEntryCap`），超出部分说「还有 N 个，用 `ls <dir>` 取回」，不做静默截断。

`cmd/agent/cli_resume.go` 的改动只有两处：

1. 原来那条内联 `fmt.Errorf` 换成 `resumeNoCheckpointError(target, st)`；
2. **记录在案但文件已不在**的 checkpoint（`tools.TaskResumeTarget` 故意返回一个已消失的路径，以便快速失败而不是全树搜索）现在也在这一层归类。不这么做，它会「解析成功」然后死在加载层，报的是另一句话，而且不说哪一行、哪个判定 —— 实测：这是本条改动的第一个红，arm (b) 直接报「a row whose checkpoint is gone must still be REFUSED」。

**被禁的 fall-through 一个字没动。** `resolveResumeTarget` 里「行存在但没有 checkpoint 就硬报错、绝不落到 session-id 解析」的那段注释与结构原样保留；本次只是把它的**措辞**变成一个测量。

## 为什么不实现「要求 A」（放宽 resume 让从未运行的行可以重跑）

三条理由，每条都能独立否决：

1. **操作员裁决（记忆 #27273，2026-11-05）**：「『从未运行』的行不属于 resume 的适用范围：它没有可续的东西，正确出路是新建一条（新 UUID、新 ground、干净重做）」。裁决还明确禁止给 id 加后缀（`-r1` / `-c1`）—— 本条因此把 never-started 的出路写成 `gopi task submit`（新 UUID），**而不指向 `gopi task clone`**：后者铸造 `<id>-c%d`（`tools.TaskCloneSuccessorIDFmt`），正好是裁决禁止的形状。clone 自己的 id 铸造是另一个缺陷，应当另提一行，不在本条里偷偷改名。
2. **2026-10-28 事故**：把那句拒绝放宽成「没有 checkpoint 就当全新开始」，曾经让一次 resume 接上了**提交者的 chat 会话**，那行最后由别人会话的统计写成了 completed（目录里始终没有自己的 run log）。`resolveResumeTarget` 与 `tools.TaskResumeTarget` 里现在这一整套阶梯，就是为了堵这条。
3. **brief 自己的 §硬要求**也禁止这么放宽。三条是同一个方向，不存在取舍。

## 诚实的后果：ground 占用与空目录

- **ground 占用**：`gopi worker --resume` 在跑到 checkpoint 判定**之前**先解析 ground。一条 ground 已死、或（本例）根本没记过 ground 的行，会先被 ground 那道门拒掉（`names no ground`），根本到不了这里的两种措辞。本次真跑取证时就是先撞上它：需要先用 `tools.TaskDeliverableWorktreeAdopt`（claim 缝的同一个 writer）把一条 live ground 记到行上，才到达 never-started 分支。**这不是本条的缺陷，但它是读者会遇到的顺序**，写在这里而不是留给下一个人踩。
- **空目录**：判据只看「有没有 run log / raw log」。一条被 claim 过、目录里有 `attachments/` 与 `metadata.json` 的行，与一条目录都还没有的行，都判为 never-started —— 对读者而言这两者的出路相同（新行），所以不细分为第三种判定；但拒绝文本会**印出目录里实际有什么**，让「真的空」与「有一次 claim 留下的残留」在文本里可辨。
- **没有覆盖的**：本判据不判断「这条行该不该重做」。它只回答「resume 有没有可续的东西」。

## 验证

- 红证明：把 `neverStarted()` 的判定短路成恒假（`if false && …`），arm (a) 立刻红，报的正是**错归类**：
  `the never-started refusal must name the new-UUID way out` / `must NOT recommend gopi task clone`，并给出被判成 ran-no-checkpoint 的那整句。还原后 arm 全绿；还原核验用 `grep -c 'if false && data.neverStarted()'` = 0。
- 回归：`TestResumeNoCheckpointVerdict`（a/b + 谓词四形态）、`TestReallyRanSessionStillResumes`（arm c：有 checkpoint 的行逐字不变，且真实 CLI 仍能续，且成功路径不印任何判定词）、`TestNeverStartedRowNeverStartsASession`（拒绝后 sessions 根不新增条目）。
- 真实 CLI：`gopi worker --resume <never-run row>` 在沙箱 profile 下打印 `verdict never-started` + 依据 + 新 UUID 出路，`EXIT=1`。
