<!-- doc-class: normative -->
<!-- status: CURRENT as of 2026-09-22 — supersedes any pre-2026-09-19 note that
     put spawn-directory or worktree-removal decisions in prompt discipline. -->
# 设计原则：生命周期动作的确定性归属（谁有资格判"做完"）

> 一条贯穿 spawn / close-out / reclamation 的设计原则。
> 起草于 2026-09-22，源于一个真事故（worktree 被在活 run 名下删除）。
> **这条原则不是"某个 bug 的修法"，而是"这类 bug 以后不该再出现"的判据。**

## 一、事故（为什么要有这条原则）

2026-09-21 23:09:45，`~/workspace/gopi-jieba-searchkw` 被删除，而：

- 它对应的行 `01a0c574-cb48-76ef-9d76-ac93d756e833` 状态是 **`in_progress`**
- 持有者 pid **1191072 还活着**
- 分支 `fix/jieba-token-absorbs-search-kw` **14 ahead**（有未交付的工作）

`~/.gopi/logs/removal-audit.log` 的记录：

```json
{"ts":"2026-09-21T23:09:45","kind":"worktree-removal","arm":"task-self-reclaim",
 "pid":1191072,"target":"/home/user/workspace/gopi-jieba-searchkw",
 "branch":"fix/jieba-token-absorbs-search-kw",
 "task_id":"01a0c574-cb48-76ef-9d76-ac93d756e833",
 "evidence":"this run's own finished row 01a0c574-... disposition reclaimFreable"}
```

**根因不是某个判据写错，而是"谁有资格判`做完`"这件事没有归属规则**：
那条路的 `evidence` 读的是 **"the delivery chain's local sync landed"** ——
这是**一句关于 merge 的话**（这个分支现在包含集成分支了），
被当成了**关于 work 的话**（这个任务干完了）。

一个 run 完全可以"已经对齐"却**还在半路**。对齐是关于分支位置的陈述，
完成是关于工作量的陈述，**两者不能互相代替**。

## 二、原则

### P1. 只有确定的角色能执行生命周期动作

生命周期动作（建目录 / 删目录 / 建分支 / 删分支 / 决定"做完"）分两类：

| 类型 | 例子 | 谁执行 | 为什么 |
|---|---|---|---|
| **确定性的** | 建 worktree、checkout、设 cwd、dirty 检查、remove worktree、删已合并分支 | **harness（worker，零 LLM）** | 这些是**可判定的事实**：`git status` 空不空、路径存不存在、分支是不是祖先 —— 答案唯一，不需要判断力 |
| **需要判断的** | 冲突怎么解、代码该怎么写、这个需求值不值得做 | **LLM / 专门的机制（如 `gopi task merge`）** | 这些**没有唯一答案**，需要上下文与取舍 |

**推论（事故的直接教训）**：
**LLM 不能用自己的"我觉得做完了"来触发破坏性动作。**
LLM 的自我报告是**意图**，不是**事实**。破坏性动作的前置条件必须是**机器可查的事实**。

### P2. `cwd` / worktree 归属是 run 的属性，不是 LLM 的裁量

一个 run 在哪个目录里干活，**必须由 runner 在 spawn 时设定**（`cmd.Dir`），
不能"记录下来然后指望模型自己 `cd` 过去"。

事故的反面证据：三个 task run child 的 cwd **全部是主仓**，
因为 `deliverable.worktree` 只被记录、从不被强制（`taskClaimGroundStamp` 是 additive，
从未命名的行永远回落主仓 —— 值"合法"，所以没人报错）。

**这个缺陷的第二重危害**：回收守卫 `finishCleanupWorktreeInUse` 靠扫
`/proc/*/cwd` 判"这个 worktree 还有人在用吗"。child 不在自己的 worktree 里
→ 守卫看到"无人使用"→ **正确地**允许回收 → 删掉活 run 的目录。
**守卫是对的，生产者站错了位置。** 修 must 修生产者，不是放宽守卫。

### P3. 回收前必须过确定性清单（不是"LLM 说完了就删"）

任何会删掉一个 run 的 worktree 的路径，前置必须过同一份清单：

```
□ dirty         —— 工作区干净吗（不干净按现有 dirty 规则处置，不静默释放）
□ outside history —— 分支是从集成分支分出来的吗（无共同祖先＝不是正常交付物）
□ check-in      —— 活在 commit 里，不在工作树里站着
□ safe removal  —— 这个目录能安全移除吗
```

**复用现有的判据，不写第二份**（Rule 13/14）。

### P4. 清单之外的事，交给专职机制 —— 特别是：不碰冲突

> **worker 只做它能确定保证的。有冲突也没办法，反正 merge 也会看到。**
> **不解决、不报告、不转达。**

| worker 做（确定、可保证） | worker 不做（留给 `gopi task merge`） |
|---|---|
| worktree 脏了怎么办 | 分支是否冲突 |
| 没有 outside history | 冲突是什么 |
| check-in 完整 | 任何关于冲突的裁决 |
| worktree 能安全 remove | 把冲突结果告诉 resume 的 child |

**为什么这条要写死**：冲突是合并那一趟会自己检测并打回的。
worker 提前检测 = **重复劳动**（resume 出去的 run 还得重跑一遍 merge，
看到同一个冲突，走到同一个地方）。所以：**不解决、不报告、不转达。**

**推论（连带的机制简化）**：**resume 的唯一正当理由 = 活没干完**
（有未提交改动 / 任务清单未完成）—— 这是 child 自己比任何外部观察者都清楚的事。
既然 worker 不产出冲突裁决，就**没有"merge 失败结果"要传给 resume 的 child**，
**也就不需要 attachment 机制**。这个问题被 P4 直接关掉了。

### P5. 清理 ≠ 解决。失败清单项的响应是「留着」

一个 run 没过清单时，**保留它的 worktree 和分支原样**，让它能被 resume
或交给合并那一趟（`gopi task merge`）。**移除绝不是对"检查没过"的响应。**

事故里就是这条被违反了：判据失败（其实是判据读错了）的响应是删目录。

## 三、为什么"确定性"是关键词（成本论证）

把生命周期动作交给 LLM 判断，代价有三层：

1. **不可靠**：LLM 的自我报告是意图不是事实（本次事故的直接成因）
2. **不可审计**：说"我做完了"没有机器可核的证据链
3. **要花钱**：机械动作走 LLM = 花 token 买确定性，而且买到的是"不确定"

反过来，交给 harness 的判断是**免费的、可断言的、可回归测试的**
（`git status --porcelain` 空不空，写个测试就能钉住）。

**判据**：**一个动作能不能被写成 `if <机器可查的事实> then <动作>`？**
能 → harness 做。不能 → 交给有判断力的机制，且**明确它的边界**。

## 四、已派工单（这条原则的落地）

| 端 | 工单 | 做什么 |
|---|---|---|
| **创建端** | `01a0c650-0358-7301-94b2-461d47b59bca` | worker 在 spawn 前建 worktree + checkout branch + 设 `cmd.Dir`；解析不出就拒启动，**绝不回落主仓** |
| **回收端** | `01a0c664-9bad-7ad1-bf48-f6e6a94018dc` | `task-self-reclaim` 不许自判完成；回收前置确定性清单（P3）；**零冲突处理**（P4） |
| **判据修正** | `01a0c642-bdfa-752f-8426-c84a0d303707` | reclaim 不许回收活 run 的 worktree；`--status` 不许按分支名匹配就假报 completed |
| **第三条保护线** | `01a0c63d-8d7e-705b-add6-36f89c6efe50` | 合并候选集查台账 live 行（防止未完成分支被合进来） |
| **规则入 G0PI.md** | Rule 30a（commit `55017971`） | "对齐 ≠ 完成"，delivery-point sync 是最后一步 |

## 五、边界（这条原则**不**主张什么）

- **不主张"harness 能判一切"**。冲突、设计取舍、需求判断都超出它 —— P4 就是划这条界。
- **不主张"LLM 不该自报完成"**。LLM 报告是**意图输入**，有用；但它**不能作为破坏性动作的授权**（P1）。
- **不主张"清理要激进"**。P5 相反：清单没过的响应是留着，不是删。
- **不主张加新机制**。全部复用了现有件（`taskFinishCleanupChain` / `finishCleanupWorktreeDirtyReason` / `finishCleanupRemovalReason` / `ReclaimDeadHolderWorktrees` / `removalaudit`）。

## 六、这条原则会不会过时（针对"库会积攒过时政策"的反作用）

**会过时的部分**：具体的判据、具体的函数名、具体哪个 arm 叫什么。

**不会过时的部分**：**"破坏性动作的授权必须是机器可查的事实"**（P1）
和**"确定性的给 harness，需要判断的划清边界"**（P1/P4 的划分方式）。

所以本文的 normative 部分写成**判据形式**而不是**实现描述** ——
实现变了判据还在；判据本身变了，说明这条原则被推翻了，应当显式修订而不是静默腐烂。
