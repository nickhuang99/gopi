<!-- doc-class: normative -->
# 调查报告：memory_feedback 的"立即反馈"是否回归丢失

> 日期：2026-08-27 · 判定：**不是代码回归 —— 是 LLM 偏好（prompt 说服力问题）**
> 调查方法：git log -S 全查（memory_feedback / 相关字符串 × 全分支）+ 主分支代码现状逐行核对 + 1360 个历史 session 时序扫描

---

## 1. 结论先行

**"任务开始 memory_search 后立即 memory_feedback"的代码路径 100% 完整未破坏。**
8e3ac0c（怀疑对象）**只动了 task-end memory_store 的强制注入点，零行 feedback 生产代码**。
但**没有任何真实 session 在 search 后 1-2 轮内投过票**——最近 60 个 session 中仅 4 个投了 feedback，
全部在 search 之后 **5~92 轮**（任务中后期/任务结束），且全部 `rating=useful`（不带 tool_use_id，
成对删除永不触发）。

**判定：代码 bug ❌ → 不是。LLM 偏好（采纳率/即时性双低）✅ → 是。**
根因与 FEEDBACK-AUDIT-REPORT（097c677）一致，本报告补充了"立即性"维度的铁证 + 8e3ac0c 无罪的 git 证据。

---

## 2. Git 历史调查（git log -S "memory_feedback" 全查，不遗漏）

涉及 memory_feedback 的 commit（`git log --all -S "memory_feedback"`，28 个命中去重后关键项）：

| commit | 时间 | 动作 | 对"立即反馈"的影响 |
|---|---|---|---|
| 69bdc1d / cf63513 / 682fadc | 早 | memory_feedback P0 实现（投票通道） | 无（前置） |
| 82cb805 | 08-26 03:44 | **feat(search-remove)：立即上下文瘦身**——引入 ①search 描述引导"见结果后立即评估"②memory_feedback 加可选 tool_use_id ③harness 成对删除（`Context slimming` 块 + RemoveToolPair） | **立即反馈逻辑的唯一源头** |
| 892a60a | 08-26 04:32 | fix(loop)：tool-result 载荷重复（tick+stamp 装配顺序） | 修复而非破坏——stamp 头正是 LLM 逐字复制 tool_use_id 的机制；payload 从双份变单份 |
| 8e3ac0c | 08-26 08:41 | fix(loop)：task-end memory_store 不再被简短完成答案绕过 | **只改 maybeEnforceCompletionStore 的调用条件**（`len(content)>=40` 门槛移到 if 内），R3 分支重构；diff 中 feedback 相关行 13 处**全部在 commit message/测试**，生产代码 0 行 |
| 0ba3d0b | 08-26 18:16 | feat(prompt)：rule d 双向化——"judge every search hit: used → useful; contradicts → useless then update" | 增强而非破坏——prompt 首次覆盖 useful 方向 |
| e6b7aad (HEAD) | 最新 | 13 功能验证矩阵文档 | 无 |

**关键证据**：
- `git show 8e3ac0c -- internal/agent/loop.go | grep -c slimm/RemoveToolPair/rating-useless` = **0**
- 82cb805..8e3ac0c 之间动 loop.go 的 4 个 commit（d734963 thinking-trim、eee6daa context-trim 泛化、892a60a 载荷去重、8e3ac0c 本身）——逐一核对均未移动/注释 feedback 成对删除块
- 成对删除块 `Context slimming` 自 82cb805 引入后位置未变：`internal/agent/loop.go:6417`

## 3. 代码现状（文件:行号，全部在主分支 HEAD e6b7aad 实测）

| 组件 | 位置 | 状态 |
|---|---|---|
| memory_search 工具描述"立即评估"引导 | `internal/agent/loop.go:547` | ✅ 完整："After seeing results, immediately assess each hit… If a hit is useless… memory_feedback with rating=useless and the originating tool_use_id — the harness will drop it from context right away" |
| toolRoutingRules 记忆 if-then | `internal/agent/loop.go:696-698` | ✅ 0ba3d0b 版 rule d 在位："judge every search hit: used it → memory_feedback(useful); contradicts your results → memory_feedback(useless) then memory_update. 不投票=知识库失去质量信号." |
| memory_feedback schema tool_use_id | `internal/agent/loop.go` claudeToolDefs（描述含 call_… 消歧"NOT record #N"） | ✅ 可选参数在位 |
| harness 成对删除（search-remove） | `internal/agent/loop.go:6417`：`tc.Name=="memory_feedback" && rating=="useless" && tid!="" → Ctx.RemoveToolPair(tid)` | ✅ 未被移走/注释；`context_trim` 同用 RemoveToolPair（`internal/agent/loop.go:6417`） |
| 结果 stamp 头（verbatim-copy 机制） | `internal/agent/loop.go:1665-1582`：`[t+NNNs] [tool_use id: X]\n<payload>`（892a60a 修复后单份） | ✅ 在位 |
| taskStartSearchMessage | `internal/agent/loop.go:118-120` | ✅ 在位（注意：**只要求 search，不提投票**——见 §5） |

单元测试：`go build ./...` OK；`go test ./internal/agent/ -run 'TestRemoveToolPair|TestSystemPromptDensity|TestMemoryFeedback'` PASS（RemoveToolPair 9 个用例 + density + schema 全绿）。

## 4. 运行现状（session 时序——亲手读，非二手）

**扫描方法**：解析 ~/.gopi/sessions/*.jsonl 的 tool_use 事件，按"含 tool_use 的 assistant 消息"计轮次（round），
量 search→feedback 的**轮次间隔**（非行距）。

### 4a. 最近 60 个 session（8/26 18:00 之后，含 8e3ac0c+0ba3d0b 时代）
- 26 个有 memory_search，**仅 4 个有 memory_feedback**（15%）
- 时序（search_round → fb_round → gap）：
  | session | search | feedback | gap | rating | tool_use_id |
  |---|---|---|---|---|---|
  | 11b3012b (08-26 21:57) | r1 | r32 | **31 轮** | useful | 无 |
  | aa62488d (08-26 21:13) | r1 | r6 | **5 轮** | useful | 无 |
  | 741a5fe4 (08-26 18:18) | r1 | r93 | **92 轮** | useful | 无 |
  | 56bf5b51 (feature 验证任务) | r1 | r23 | **22 轮** | useful | **有**（call_00_QgeKBeat…）|

**最接近"立即"的 aa62488d 也是 5 轮后**；741a5fe4 的 r92=memory_store、r93=feedback——**任务结束才投**。

### 4b. 56bf5b51 的 tool_use_id 之谜（唯一带 id 的真实 session）
feedback 带了 `tool_use_id=call_00_QgeKBeatOmPZ22q77Imv9723`（=该 session 的 memory_search 调用 id，正确！），
但 **rating=useful**——harness 成对删除只在 `useless` 时触发（`internal/agent/loop.go:1694`），
且 DB 侧该 id 的 search 结果仍在（无 `context slim` 日志）。**证明：LLM 会正确复制 id，但把"立即无用反馈"
写成了"迟到的 useful 反馈"**——成对删除路径从未在实战触发。

### 4c. useless+tool_use_id 的唯二出现 = 82cb805 的 smoke test
2c8e30cd / a67d4e1d / f15da5e3（08-26 03:42-03:44，workdir=/home/user/work/gopi-search-remove，
user="Short verification task. Do EXACTLY this sequence…"）——gap=1（**真·立即**）但为脚本化验证，非有机任务。
FEATURE-VERIFY-MATRIX #3 ⚠️（"useless+tool_use_id 仅 smoke 触发"）得到本调查复证。

### 4d. 回归窗口对照（8/26 03:00-13:00，82cb805 与 8e3ac0c 之间）
96f2889c（08:43，8e3ac0c 后 2 分钟）gap=2/3 useful——与 smoke 的"立即 useless"不同；
fa8ffc05（07:59）gap=132 useful+tid。**8e3ac0c 前后无任何"立即反馈"行为变化**——若它是回归点，其前（03:44-08:41）与后（08:41+）的即时投票率应出现断层，实际断层不存在（前后都是 0 个有机立即投票）。

## 5. 判定与根因

**不是代码回归。** 立即反馈的引导存在三层（search 描述、rule d、stamp 头），harness 删除逻辑在，测试绿。
**是 LLM 偏好 + prompt 说服力不足**，两个子原因：

1. **采纳率低（已有结论，097c677）**：task-start search 有 harness 强制注入（taskStartSearchMessage），
   feedback 纯靠软提示——LLM 对非强制钩子遵守率 ~9-15%。
2. **立即性缺失（本次新证据）**：taskStartSearchMessage（`internal/agent/loop.go:118-120`）**只命令 search，一个字没提"投完票再干活"**——
   LLM 读到命中后进入任务正文，投票被推迟到任务中后期甚至 completion checkpoint 之后（741a5fe4 r92-93）。
   即"立即反馈"从未被放入强 checkpoint 消息，只在工具描述的细节里。

## 6. 修复建议（供任务 2 / prompt 强化参考，未实施）

1. **taskStartSearchMessage 加投票指令**（与 store 对称）："…call memory_search once… then for each hit returned, vote immediately via memory_feedback (useful / useless+tool_use_id) before doing anything else."
2. **可选 harness 兜底**（对称 maybeEnforceCompletionStore）：search 有命中且本任务从未投票 → 注入一次性投票 checkpoint。会改 R3 完成出口 API call 计数测试。
3. FEATURE-VERIFY-MATRIX #3 的"未实战"状态维持——成对删除缺实战验证，可在实施 1/2 后补一次真机 useless 冒烟。

## 7. Verification
- Build: `go build ./...` OK
- Tests: `go test ./internal/agent/ -run 'TestRemoveToolPair|TestSystemPromptDensity|TestMemoryFeedback'` PASS
- Git: 本调查零代码改动（纯调查任务）；HEAD e6b7aad
