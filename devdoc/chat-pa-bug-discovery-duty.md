<!-- doc-class: historical -->
# 发现异常即是职责：chat PA 职责条款新增 (4b)「立即报 bug」

**任务**：`01a12b22-4b6f-74ce-80ab-cefef743bd90` · **分支**
`task/01a12b224b6f74ce80abcefef743bd90` · **出具**：2026-10-11 · **主机** hosta

---

## 1. 缺口是什么（根因，不是症状）

`internal/agent/chat_pa_duty.go` 的 `[CHAT PA DUTY …]` 段落把工作分成三类：

| 类别 | 条款 | 判据 |
|---|---|---|
| OP（PA 自己做） | (1) / (1b) | operator 本来会亲手做的一切，**除** coding/debug |
| TASK（派单） | (2) | **"a change to the REPOSITORY"** |
| OPERATOR-ONLY | (3) | 不可逆 / 需要 operator 的判断 |

**"我发现了一个缺陷"落不进任何一类**：它不是对仓库的改动（不是 (2)），也不是 (1) 里
operator 会亲手做的活（operator 并不知道这个缺陷存在，所以它不可能是"本来会亲手做"的）。
条款缺位的结果是可预测的：PA 退化到**问 operator**——而那正是 (1) 存在的意义所要消灭的动作。

条款 (4) 已经说 PA「可以 READ、可以 MEASURE、并把发现连同复现写一行」，但它**没有说这一行
写到哪里、什么时候写**——所以"写一行"被读成了"写进本次回答/留到收尾"，
**发现随上下文一起死掉**。

(4b) 命名的是第三种动作：**立案**。它不是 (2) 的放宽（PA 不修仓库），也不是 (1) 的延伸
（operator 没在做这件事），而是一条独立的**职责**。

## 2. 改了什么

| 文件 | 改动 |
|---|---|
| `internal/agent/chat_pa_duty.go` | 新增 clause **(4b)**（`chatPADutyBugDiscoveryRule(toolsOn bool)`）+ marker 常量 `chatPADutyBugDiscoveryMarker` + 两条 limb 常量 + 先例常量 `chatPADutyOverAssertionPrecedents`；文件头注释补该条款的来源与理由 |
| `internal/agent/chat_pa_duty_bug_discovery_test.go` | **新文件**，7 个测试（见 §4） |
| `internal/cliverbs/cliverbs.go` | 新增 `TaskTagsWord = "tags"` / `TaskTagsFlag = "--" + TaskTagsWord`（叶常量包，Rule 14） |
| `cmd/agent/cli_tasks.go` | `taskTagsFlagName = cliverbs.TaskTagsWord`（alias，非字面量）；解析点与 admission 表改引用它 |
| `internal/cliverbs/cliverbs_test.go` | `cliverbsAliasTargets` 增两条；`TestSenseFlagsMatchCmdAgentDispatch` 的 want 表增 `TaskTagsFlag` → `taskTagsFlagName` |

### 2.1 位置：为什么 (4b) 渲染在 (4) 与 (5) 之间

`chat_pa_duty_test.go`（**本次未修改**，约束要求它继续通过）里有一条**位置敏感的**断言：

```go
for _, clause := range []string{
    "(1) YOU DO IT YOURSELF,",
    chatPADutyNoPermissionMarker,
    "(2) ONLY a change to the REPOSITORY is a task",
    "(3) OPERATOR-ONLY",
} {
    if n := strings.Count(block, clause); n != 1 { ... }
}
```

其中 `(2) ONLY a change to the REPOSITORY is a task` 是 (1b) 的**紧邻后缀**：
`chatPADutyNoPermissionRule(...)` 的返回串末尾接的就是 (2)。把新条款插在 (1b) 与 (2) 之间，
会把这个断言变成 `Count == 0` —— 也就是说，"只新增"这条约束在**那个位置**上做不到。
插在 (4) 与 (5) 之间则既满足语义顺序（(4) 说"可以 READ/MEASURE 并立案" → (4b) 说"这就是职责、
现在立" → (5) 说"用结果自查"），也**不触碰任何既有子串**。

### 2.2 四项语义（deliverable 1）

| # | 语义 | 渲染里的话（节选） |
|---|---|---|
| a | 发现异常是**职责**、不是请示 | `that is a DUTY of this surface, not a permission question` / `Do NOT wait for the end of the run and do NOT ask first` |
| b | 门槛刻意设低**是 operator 的决定** + Rule 26 允许重复提交 | `deliberately LOW, and that is the OPERATOR'S decision` / `dispatching the same work twice is allowed and EXPECTED` |
| c | 唯一约束是**证据必须真**（实测 vs 推测）+ 三次撤回先例 | `EVIDENCE MUST BE TRUE` / `a code comment, a document's wording or your own expectation is NOT evidence` / `3e699e3d8 / 2d90810e6 / b84e84ad8` |
| d | 与 (2)(3) 不冲突 | `contradicts neither (2) nor (3)` / `stays a TASK` / `OPERATOR-ONLY` |

**三次过度断言撤回先例**（真跑 `git log --all --grep=retract` 取，逐条可 `git show`）：

| commit | 被撤回的是什么 |
|---|---|
| `3e699e3d8` | A/B 对照组的 control 因 ENOSPC 损坏，"两向差分皆空"这个结论从未可采信 |
| `2d90810e6` | 两条因果论断的唯一来源是**代码注释**（且时间戳被倒着念） |
| `b84e84ad8` | 报告里"末回合尾巴未动"被同一次探针推翻——它正是本次修复的**另一个症状** |

### 2.3 双 limb 的诚实性（Rule 15）

```
tools  limb: "file it in THIS turn with `task_submit`"
no-tools limb: "this path carries NO tools, so state the finding and its evidence plainly
                and hand the operator the exact command to file it"
```

纯 chat 路径**没有工具上线**（实测：该请求 `tools` 数组为空），所以对它说"现在就立案"是一条
**无法执行的指令**——本文件头部把这种失败模式称为最坏的一种，"because it is followed"。
两条 limb 由参数选择，各自只说对自己为真的话。且 `no-tools` 里**不含** tools limb 的句子、
tools 里也**不含** no-tools 的句子（两个 limb 的 prompt 上双向断言，见 §4）。

### 2.4 词汇一律来自叶常量（Rule 1/13/14）

| 渲染出的东西 | 来源 |
|---|---|
| `gopi task submit "<…>"` | `chatTaskSubmitRoute()` —— **写边界段落渲染的同一个 helper**，所以两段不会教出两条命令 |
| `--tags` | **新增** `cliverbs.TaskTagsFlag`（`= "--" + TaskTagsWord`），cmd/agent 侧 `taskTagsFlagName = cliverbs.TaskTagsWord` 是 alias |
| `bug` | `tasktags.BugTag`（叶包，唯一真源；ledger 按逗号 token 精确匹配这个值） |
| `task_submit` | `toolnames.ToolTaskSubmit` |

**为什么 `--tags` 值得新增一个叶常量**：prompt 教模型**立即**跑这条命令，而 `--tags` 此前
在 `internal/` 下**没有**任何可导入的常量——在 prompt 里写 `"--tags"` 就是 Rule 1/13 禁止的
裸字面量，出现在一个"模型被要求马上照做"的位置上。叶包加常量 + cmd/agent 加 alias +
`TestSenseFlagsMatchCmdAgentDispatch` 的 want 映射，三处同时锁死，改名必红。

## 3. 与既有条款的关系（没有放宽任何东西）

- **(2)**：修缺陷仍然是"改仓库"，仍然是 TASK。(4b) 只授权**报**，不授权**修**。
- **(2b)**：PA 不 commit。(4b) 不碰这一条。
- **(3)**：会重写已发布 ref / 删未证实工作的修复，仍是 OPERATOR-ONLY。
- **(4)**：原语义一字未动；(4b) 是 (4) 那句"把发现连同复现写一行"的**落地点**（写到哪、何时写）。
- **写边界**：未被放宽。既有 `TestChatPADutyAgreesWithTheWriteBoundaryOnWhatIsATask`、
  `TestChatPADutyRendersNoDestructiveBranchForm`、`chat_write_boundary_test.go` 全绿。

## 4. 测试（deliverable 2）

新文件 `internal/agent/chat_pa_duty_bug_discovery_test.go`，7 个测试：

| 测试 | 断言什么 |
|---|---|
| `TestChatPADutyBugDiscoveryRendersOnBothLimbs` | 两 limb 的 `chatPADutyRules()` **以及两个组装后的 chat prompt** 都含新 marker；并先做**非空性**检查（marker 不能配一段空话） |
| `TestChatPADutyBugDiscoveryKeepsBothLimbsHonest` | tools limb 含 tools 句、no-tools limb 含 no-tools 句；**no-tools 里不出现 tools 句**（假指令） |
| `TestChatPADutyBugDiscoveryNamesTheReservedTag` | 渲染出**完整调用串** `gopi task submit "…" --tags bug`（由常量拼出）；路由与写边界**同一拼法**；flag 是**带破折号**的那个 |
| `TestChatPADutyBugDiscoveryIsRenderedExactlyOnce` | **Count == 1**：新 marker + 7 个既有 clause/marker，两 limb 各查一遍（记忆 #8844 的实测教训：`Contains` 分不出 1 份还是 2 份） |
| `TestChatPADutyBugDiscoveryLeavesTheExistingOrderAlone` | 8 个 marker 在**同一渲染串里**的相对顺序不变，新条款落在 (4) 之后、(5) 之前 |
| `TestChatPADutyBugDiscoveryCarriesThePrecedentsAndTheBound` | 四项语义逐条在场 |
| `TestChatPADutyOverAssertionPrecedentsAreRealCommits` | 先例是**可 `git show` 的 commit 缩写**，不是一句口号；< 3 条即失败（一条撤回是轶事，不是判据） |

`chat_pa_duty_test.go` **未修改**（实测 `git diff --name-only -- internal/agent/chat_pa_duty_test.go`
输出为空），其全部测试继续通过。

## 5. 被否决的替代方案

- **把 (4b) 并在 (2) 里**（"repository change 或 discovered defect 都算 task"）：会改写 (2) 的语义，
  并让 `chat_pa_duty_test.go` 的那条子串断言失效——而"只新增不改写"是本次的硬约束。
- **不动 prompt，只加 guard**：没有任何可执行的判据——"发现异常"是模型的判断，不是一条命令，
  没有 dispatch 点可以拦截（Rule 3/35：不新增工具、不新增脚本、不新增配置开关）。
- **把 `--tags` 直接写成字面量**：Rule 1/13 禁止，且这是模型被要求**立即执行**的命令——
  正是最不能写死的位置。
- **先例只写"三次撤回"不点名**：Rule 38 的纪律——一个读者要据此行动的数字必须可复算，
  否则就是"带数字的传闻"。故点名三个 commit，并由测试守住它们仍是 commit 缩写。

## 6. 没有做的事（诚实）

- **没有跑真实模型的端到端会话**验证"PA 真的会因此立案"。Rule 11 的完整验证环需要真实
  模型轮次与经费；本次交付的是 **prompt 渲染真值 + 渲染串断言**（Rule 31），
  以及**请求体层面的实测**（§7）。LLM 行为面的取证留给下一次真实 chat 会话。
- **没有改 `promptSections`**：(4b) 随 `chatPADutyRules` 走，只进 chat 两条路径，
  后台 task run 的 prompt 不受影响（既有断言保证 TASK prompt 不含 duty 标记）。

## 7. 取证（Rule 31）

完整命令、输出片段与判定见 `task-report.md` 的 `## Verification`。
证据文件（session 目录 `~/.gopi/sessions/01a12b22-4b6f-74ce-80ab-cefef743bd90/`）：

```
req-tools.json.1 / req-tools.json.1.system.txt   --tools 真实请求体与拼出的 system 全文
req-plain.json.1 / req-plain.json.1.system.txt   纯 chat 真实请求体与 system 全文
r31-render.sh                                    复跑脚本（幂等，零 token）
stub/r31_stub.go                                 本地捕获 stub（拒绝每次调用，不花钱）
```
