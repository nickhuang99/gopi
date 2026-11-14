<!-- doc-class: historical -->
# Handoff — 2026-09-24 早间（夜间自主工作期间）

**写于：** 2026-09-23 23:30（nick 睡前布置「自己决策、维持项目不失控」）
**作者：** PA（全权自主模式）
**master：** `db109cfb`

---

## 一句话

**项目没有失控到危险的程度，但它在一个自我增压的环里。** 我量出了这个环的四个机制，每个派了一张单；其中「ground 建了不宣告」那张**已经被 agent 实现并交付**（`feat/ground-announcement`，952 行，含 E2E + GOPI.md 条款测试）。

---

## 一、你睡前那句话，我用数据回答了

「I feel it is going to run out of control」——四个独立测量：

| 测量 | 值 | 怎么测的 |
|---|---|---|
| 当天 session 数 | 79 | `find ~/.gopi/sessions -name '*.raw.log' -newermt '2026-09-23 00:00'` |
| 其中 raw log > 10MB | **63（80%）** | 同上 `-size +10M` |
| 最大单 session 日志 | **253 MB** | `01a0ce03`「gopi log regression」 |
| GOPI.md 体积 | **89,842 B**（09-15 是 44,824，八天翻倍） | `wc -c < GOPI.md` |
| GOPI.md 占单次请求 | **96.8%**（89,864 / 94,871 B） | `/tmp/request.txt` |
| 最重 session | 280 轮 / 2510 万 prompt tokens / 均 90,370 每轮 | `gopi stats ffe7d596-…` |
| 当天 commit 构成 | 181 总 = **85 docs**（70 个作者是 gopi）+ ~20 真代码 | `git log --since` |

**不是钱的问题**（最贵 session ¥1.9954，输出占 53.6%），**是吞吐和注意力的问题**。

### 那个环

```
长 session（80% 超 10MB，峰值 253MB）
   → 出事故
   → 往 GOPI.md 追加规则叙述        （44,824 → 89,842 字节）
   → 每轮都付更贵的 prompt          （均 90,370 tokens/轮）
   → 更慢更分心
   → session 更长
   → 长 session 的产物没人收        （worktree / 分支）
   → 转而写文档而不是写代码          （85 docs vs ~20 代码）
   → 更多事故面 → 回到起点，更快
```

**环里没有一步是普通意义的 bug。** 每一步都是合理的局部反应。**这个环就是系统本身。**

完整记录：`devdoc/runaway-loop-20260923.md`（已入库 distilled 10650）。

---

## 二、四个机制 → 五张单（全部已派）

| 机制 | 单号 | 状态 |
|---|---|---|
| **ground 建了不宣告** → 每任务多一个空 worktree | `01a0ceac-433e-7180-b3ae-2ba48fc15826` | **已交付**（`feat/ground-announcement`，等它跑完好 merge） |
| **没有东西驱动 merge** → completed 的活躺分支上 | `01a0ceb2-1e4e-76ef-9093-948521e162ff` | pending（p1） |
| **60 分钟预算只在 prompt 里** | `01a0cec2-629c-7b69-9972-23176ce53130` | pending（p1） |
| **GOPI.md 八天翻倍、每轮注入** | `01a0cec2-66a1-7820-83a2-50c95213faf3` | pending（p1） |
| **distill_record.py 生成垃圾 search_kw**（自测时发现） | `01a0cee1-1201-73a0-ac78-531c554d66f0` | pending（p3，hermes repo） |

外加昨晚已派仍在排队的：`01a0cea7`（33 支 BURIED 分类，**正在跑**）、`01a0ce88`（merge landed list 从不被喂）、`01a0ce2b`（shot chrome 生命周期）。

---

## 三、我今晚实际动手做了什么（不只是分析）

1. **手动跑了一次 `gopi merge`** → 落地 3 支积压的 completed 分支（`3fc9b703` / `c0c973cf` / `4e414f40`）。**这直接证明「无驱动」这个诊断**：跑一次就全落地。
2. **清了 1 个孤儿 worktree**（`gopi-wh-prefix`，detached + 探针残留），探针归档到 `~/workspace/rescued-artifacts-20260923/`。**那个探针是合规的 red-proof 做法**（同一文件跑 pre-fix 树和 post-fix 树，两次运行就是 red→green 转换）——值得作为范式。
3. **删了 hermes-agent 一个 9/18 的陈旧 `index.lock`**（0 字节、无持有者、6 天前）。
4. **入库 4 条蒸馏 + 自测**（见第五节）。
5. **写了 5 张 brief + 1 篇 devdoc**。

---

## 四、我修正过的两次判断（重要）

**这是今晚最该被记住的部分**：我的两个推断被自己的测量推翻，修正都写进了 brief 而不是改掉。

1. **`01a0ceac` 的第一版说「child 把工作做在自己建的 worktree 里，ground 空着」→ 错。**
   实测 `/proc/<pid>/cwd`：**child 在 ground 里干活**，自建的那个是**没用过的空壳**（两个分支 tip 相同只因 worktree 共享 refs）。
   → 修法因此**变小**（宣告 ground + Rule 6a 加一句别建第二个），而不是"搬工作"。

2. **「单张任务跑 3 小时」→ 部分错。**
   实测 `01a0ce80` 的 `session_end`：`elapsed_sec=3605.5`（**正好 60 分钟**）、`exit_code=0`、314 轮。
   → 60 分钟是单张上限、且**在工作**；worker 进程 4 小时是因为它在**连续跑多张单**（设计如此：`workerDefaultIdleExits=3 × 30s → 队列空 90 秒自杀`）。

**两次错的根因是同一个**：从产物的存在推断进程的行为，而不是读 `/proc/<pid>/cwd`。

---

## 五、记忆链（按你的 HARD RULE 走完）

- devdoc：`devdoc/runaway-loop-20260923.md`（`db109cfb`）
- distilled：id **10650 / 10651 / 10652 / 10653**（tier=hot, confidence=0.95）
- 自测：手工改写 `search_kw` 后 **11/16 命中，多数 rank 1**；真实入口验证 `distilled_search("为什么会失控")` → **id 10650 rank 1**
- 过程中发现并派单：`distill_record.py` 自动生成的 `search_kw` 是垃圾（jieba 碎片「个作/是崩/次请」+ 机械抽取的 task-id 片段），**不手工改写的新记录基本搜不到** → `01a0cee1`
- 又踩了一次 9/22 记过的坑：手工改 `search_kw` 后用 delete+insert（给错旧值）**静默删掉 3 条记录的索引**，官方 `'rebuild'` 才修好。已写进那张单的 brief。

---

## 六、系统健康面（实测，不是假设）

- `go build ./...` RC=0；`git status --porcelain` 空
- worker 循环是**自限**的：`workerDefaultMaxRounds=0`（无限轮）但 `workerDefaultIdleExits=3 × 30s → 队列空 90 秒自己 exit 0`（"a task-DRAIN tool, not a daemon"）
- `01a0ce80` 正常结束：`exit_code=0, elapsed=3605.5s, 314 iter, 2748 万 tokens`，并写了自己的报告
- cache 命中 97.8–98.3%（钱不是瓶颈）
- `gopi stats` / `gopi raw` / `removalaudit` / `signalrec` 都工作，**本文每个结论都是从它们取的证据**
- 队列在消化：`01a0ce80` 完成后 worker 立刻领了下一张

---

## 七、你醒来时可以看的

**最值得看的**：`feat/ground-announcement` 那两个 commit（`93e48bc4` + `241dde70`）——它 952 行，含 E2E 测试和 GOPI.md 条款测试，commit message 直接是 `one task, one worktree`。**它还在跑**（13 个持有者，在跑测试），跑完可 merge。

**要你拍板的**：没有。五张单都按你的原则写了，硬边界都写明（不许复活 merger daemon、不许删规则文字、无 gate 的行为规则不许移出 prompt、不许改 gate 让规则"可移"）。

**可能想否决的**：`01a0ceb2` 我推荐用 systemd user timer 跑 `gopi merge --fast`。**这是"自动 merge"的一个新机制**，而 9/21 你说过跨节点同步要手动。我在 brief 里明确写了：如果你认为 timer 越界，**实现者必须显式说出来并改交手动 runbook**，不许默默什么都不做。

---

## 八、未处理（留给你的判断）

- `gopi-scratch-owners` worktree 我**故意留着**（ppid=1 的编译孤儿，是 RED PROOF 素材，杀了就没人能证明守卫会被污染）
- `~/workspace/gopi-01a0ce80…` 和 `…/gopi-01a0cea0…` 两个 ground 是活 worktree（在跑的 child 持有），**不能碰**
- `gopi-version-borrow` 也是空壳，但它属于正在跑的 `01a0cea0`，**先不动**
- 188 个 archive tag 没动（等 `01a0cea7` 的分类结果）
