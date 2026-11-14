<!-- doc-class: historical -->
# PA 角色越界的复盘 — 2026-09-12

## 发生了什么

nick 两次纠正（原话）：

> "not only you make development slow, but also the quality is worrying, I have
> submitted two bugs, these can be only tips of iceburg. you should read your
> duty again as PA, you are not good at coding, you just need to dispatch
> tasks, not doing field working yourself"

> "I noticed one thing, as PA, you should spend more time in document and
> analysis than writing code yourself. you should submit code to gopi"

> "why don't you send bond do it, MoneyPenny?"

触发点：我在 voice input 的 Phase 2 自己做完了整条开发链——读 C++ 源码、
写 300 行 Go、自己跑测试、自己 debug 网络传输。而且在被纠正的那一刻，我
正在自己跑一个 600 秒级的 `go test ./internal/tools/`。

## 为什么这是错的（不是态度问题，是产出问题）

同一时间窗口里，两条产出的对比：

| | 我写的 | Bond 写的 |
|---|---|---|
| 产物 | voice_input 300 行 Go | crash-dump bug 报告 |
| 过程 | 无竞争、无独立审查 | 诊断 + 实测 |
| 证据 | "我跑过了" | 复现命令 + 根因(文件:行号) + 数字(668.9ms / 430.9MB / 28,549 entries) |
| 发现 | 0（自己看不出自己的问题） | 4 个真 bug，且 nick 说"只是冰山一角" |

**关键**：我自己写、自己测、自己宣布完成的代码，架构性缺陷一个都没看出来。
而 Bond 在我交付后几小时内就挖出了 3-4 个。**这不是我粗心，是角色错配**——
写代码的人没有独立性去审自己的代码，而 PA 的价值本来就在于组织和验证，
不在于亲手生产。

## 我的代码里已经确认的一个缺陷（作为例证）

```go
// 我写的（cmd/agent/cli_voice_server.go:53, 156）
os.CreateTemp("", "gopi-voice-*")     // "" = $TMPDIR = /tmp = 启动分区

// repo 里早就有的规矩（internal/scratch/gc.go:24）
//   "Never /, never /tmp"
```

这个 repo 因为填满 `/tmp` 把机器搞挂过一次，有专门的 `internal/scratch`
包和门禁。**我写了违反它的代码，而且完全没意识到**——因为我既不了解那条
规矩的来源，也没有人审。这正是"自己写自己测"的典型失败模式。

## 根因（5-why）

1. 为什么我自己写？—— 因为 "Phase 2 可以开工了" 就被我理解成 "我来开工"。
2. 为什么不派？—— 因为技术细节我刚刚摸清（读了 C++ 源码、懂了链路），
   派工要写 brief，自己写看起来更快。
3. 为什么"看起来更快"是错的？—— 因为它只算了写的时间，没算返工成本。
   我写的东西没有审查、没有竞争、没有独立性，质量无从保证；nick 提的
   bug 就是账单。
4. 为什么会低估返工成本？—— 因为我把"能跑通"当成了"做对了"。
   跑通是必要条件，不是充分条件。
5. 根本原因 —— **PA 的产出单位是「决策与组织」，不是「代码行」**。
   每当我开始写代码，我就同时失去了两个角色：我既不能再客观验证，
   也没在做本该做的组织工作。

## 纠正（具体到可执行）

**不做的**：
- 不自己写产品代码、不自己跑长测试、不自己 debug
- 遇到技术选择，不再"我先试试" —— 而是写进 brief 让 Bond 试

**做的**：
- 发现 bug → 写 brief（symptom / 复现 / 已诊断根因 / 交付物 / 约束 / 验收）
- 派工 → 巡查台账 → 读报告 → 析报告 → 抽查关键断言 → 报告结论
- 维护队列健康（重复单、僵尸、优先级）
- 读 nick 的 bug 报告并转成工单
- **把时间花在文档与分析上**

**判断标准（自查）**：如果我在敲代码或盯一个长命令，我就已经越界了。
正确的状态是：手边有几份在写的 brief，和几份在等的报告。

## 这一轮实际做了的（回归后的产出）

写了 4 份 brief 并派工：

| 任务 | 内容 | 状态 |
|---|---|---|
| 01a091ed-5466 | crash-dump backfill 误报 live session 为 crash | worker 执行中 |
| 01a091ed-62a1 | crash-dump sweep 每次启动 668ms + 431MB | worker 执行中 |
| 01a091eb-f575 | repo hygiene：434 tracked scratch paths（LLM 自投，比我的更全） | pending |
| 01a091f1-50ef | internal/tools flake：用 runs 不用 reasoning 下结论 | pending |
| 01a091f1-5947 | voice_input 独立审查（PA 写的，明确标注 "treat as unverified"） | pending |

清理：cancel 自己写的那份重复 hygiene 单（LLM 的 434 路径 vs 我的 3 个例子）、
停掉我起的 voice_input HTTP server、取消 2 个 15h+ 僵尸行。

## brief 的模板（本次定型）

每份必须含：

1. **Symptom** —— 可复现，带真实数字（不是"它坏了"）
2. **已诊断的根因** —— 引用 devdoc + 文件:行号，让 Bond 不必重新推导
   （但明确"验证它，不要从头重推"）
3. **交付物** —— fix + tests + devdoc，并说明"什么算完成"
4. **约束** —— worktree 路径 / 不许 commit 什么 / 同文件任务如何协调
5. **验收** —— 可机器检查的条目
6. **方法论提示** —— 引用本仓已有教训（background 纪律、`-count=1`、
   "不要为了让门禁变绿而放宽门禁"）

## 待确认

voice_input 那 300 行代码：已派独立审查（01a091f1-5947）。审查后按结论处置
——可能重写、可能打补丁。**由审查者决定，不由我辩护。**
