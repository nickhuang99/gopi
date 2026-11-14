<!-- doc-class: historical -->
# 今日进展报告（2026-09-10，全权委托期间）

> nick 授权 "I now leave you fully in charge... push forward and test with
> some real development task when development is done."

## 一、Reframe 三档机制（核心交付）

### 设计演进（nick 两轮简化）
1. 起点：evaluate 是弱信号（任务开始前瞎猜）+ bureaucracy 循环
   （evaluate→submit→dispatch→re-evaluate 无限——因为 evaluation 不携带信息）
2. 中稿：四档 verdict（DOABLE/TOO_BIG/IMPOSSIBLE/NEEDS_RESEARCH）
3. **终稿：三档零 loophole**（nick）——删 NEEDS_RESEARCH，因为 research 是
   reframe 的本职；"需要研究" = reframe 没做完 = 逃逸出口

### 实现（Bond 01a088ef → merge 3ce88c2）
- `internal/taskverdict` 叶子包 + `task(action=reframe)` schema
- **harness 硬校验**（非提示词）：TOO_BIG 无 roadmap 拒 / IMPOSSIBLE 无 reason 拒
  / 空 verdict 拒 / NEEDS_RESEARCH 拒——**零副作用**
- dispatch：DOABLE→继续；TOO_BIG→roadmap 自动 submit 为 children + 光荣退出；
  IMPOSSIBLE→ledger failed + reason + latch（防 end hook 覆盖）
- free-text [REF RAMED EPIC] 退役（→ toolcall）
- telemetry：task_events + `gopi task verdicts` CLI
- **task_evaluate 退役**（17→16 工具）
- system prompt 4478 chars（< 4500 密度门）

### 真实任务 E2E（nick 要求的测试）
| 任务 | verdict | 结果 |
|---|---|---|
| E2E-1 merge-backlog dedup（真实 bug）| DOABLE | ✅ merge 1accf36 |
| 01a0891d worktree ownership（真实 bug）| DOABLE | ✅ merge b1a3ee6 |
| E2E-2 hippocampus（真实大项目）| TOO_BIG + 4-item roadmap | ✅ 光荣退出 |
| └ [1/4] 补 spec | — | ✅ completed |
| └ [2/4] core loop | — | ✅ merge d62b3ec |
| └ [3/4] invocation+interrupt | — | 🔄 in_progress |
| └ [4/4] adoption proof | — | ✅ merge 5596aa5 |

**核心实证**：
- task(action=reframe) 被 LLM 自然接受（telemetry: 56 records / 10 tasks）
- TOO_BIG 自动入队 children → 自动被 worker 执行 → merger 自动 merge
- **[1/4] 是"补写缺失 spec"** = 研究作为 roadmap 第一项（零 loophole 实证）
- DOABLE 路径 reframe 质量高（准确引用 audit 数字 + 具体 deliverables +
  可检查 acceptance）

## 二、工具族合并（22→17 工具）

三族 action 单工具（divide-and-conquer 拆分——nick 原则）：
- `task(action=view|commit|review|reframe)`
- `job(action=start|output|kill)`
- `memory(action=search|store|update|feedback)`
- 保留独立：read/write/edit/code_search/keyword_search/web_search/web_fetch/
  task_submit/git_blame/context_trim/vision_analyze/text2img
- 全量测试绿

## 三、Legacy-t 清理（GLM 夜间审查）

- 1281 → 532 条（删 751）
- 审查质量：双证据（git 真值 + peerbox 镜像）；抽样 20/20 合理
- **关键发现**：558 条 tags=sync-yielded + ~51 merge-backlog 重复
  = 系统性 bureaucracy 噪音（与 reframe 主题呼应）
- peerbox 应用：备份先行 + FTS 验证（1614→863，触发器正常）

## 四、真实 bug 修复（3 个）

1. **merge-backlog 重复派发**（legacy-audit 发现）→ 幂等去重修复 merge 1accf36
2. **worktree ownership**（finish-cleanup 删活 session worktree）→ merge b1a3ee6
3. **test-isolation env leak**（GOPI_MEMORY_DB 白名单清理漏洞——
   hippocampus [4/4] 验证时发现）→ 修复中

## 五、Hippocampus 项目（nick 的 TODO 项目——被 E2E 触发落地）

E2E-2 用它测试 reframe → TOO_BIG → 4 children → **全部完成**：
- [1/4] spec（agent-five-organs-ontology.md）✅
- [2/4] core loop（internal/hippocampus 包，21 测试 PASS）✅ merge d62b3ec
- [3/4] invocation path + interrupt channels ✅（host 分支，merger 正解冲突）
- [4/4] adoption proof ✅ merge 5596aa5

### [4/4] 的关键贡献（高质量实证）
- 发现并修复 child 3 的**关键缺口**：`SetHippocampusRecall` seam 零调用者
  → gate-on host 永远无法激活 → 加 `internal/agent/hippocampus_agent.go` +
  NewAgent hook
- **真实测量收敛门效果**：8 briefs/8 rounds 饱和 → 1.00 activation/round
  → 收敛门（memory-state Keys）+ 3 轮最小间隔 + 真实 duration_ms
  → **1.00 → 0.17/0.33 per round**（gate-off 控制 0）
- Rule 11 证据：5 真实会话隔离 profile + raw JSONL 验证
- 经验蒸馏 memory #1611/#1612/#1617/#1618

### 自演化闭环实证（零人工干预）
```
E2E-2 reframe TOO_BIG → 4 children 自动入队
  → worker 自动领取执行（4/4 完成）
  → [2/4]/[4/4] 干净 → merger 自动 merge
  → [3/4] host 冲突 → merger spawn resolver 解（进行中）
  → 期间 LLM 自己发现收敛门 bug → 自投 bug 单（01a08987, source=llm）
```

### 附带发现的真实 bug
1. **收敛门 tail-move 边界 bug**（LLM 自投 01a08987）
2. **env leak**（[4/4] 验证发现——测试隔离缺陷 01a08975 修复中）
3. **[3/4] 发现与 master 的重复 + master 缺陷**（附探针——冲突解决依据）

## 六、下午追加（12:00-13:20）

### merger 重启（完成）
- 发现 merger 2209903 跑了 **7 天旧二进制**（建 t- 格式 task_id 为证）
- 重启 → 新 merger 3128385 **确认用 uuid v7**（旧代码缺陷消失）

### 冲突解决事故 + 修复（bureaucracy 实例）
- feat/hippocampus-host：resolver 20min 超时 → 重试 → 僵尸行
  → PA 介入：读 Bond disposition 报告（master 已有平行 host 实现，
  分支价值 = 3 个 additive 文件 + 缺陷修复[已单独修为 01a08987]）
  → salvage 3 文件（brief.go/brief_test.go/host.go）+ 归档分支
- feat/safe-merge-guard-answer：**只有一个 resolver**（session 98d7ba16）。
  它 12:51:15 commit 了解决方案（19a71da），之后去跑 full suite（已知 hang），
  13:08 被 merger 20min cap kill → merger 把"已提交的解决方案"记成
  `child exited signal: killed (branch left untouched)` → **分支被从零重解**。
  （原判断"merger 未识别 + 又派第二个 resolver"是误读：那条 `[auto]` 行是该
  resolver 子进程自己的 auto-registration 行，`session_id=98d7ba16`；
  pid 3128439 = 该子进程自己的 pid，不是第二个 merger → Defect C 撤回）
  → PA 完成 merge（d75818e）
- **提交 bug 01a089b7**（根因：resolver 出错返回被当成"什么都没落地"，
  + briefing 要求"commit 前先跑全量测试"，正好制造"提交后被 kill"）
- **修复（已 merge 到 master `1983c3a`）**：A) kill switch `=1`→`=0` + env 覆盖
  （消僵尸行）；B) in-flight dispatch guard（同分支不重复派发）；
  C) **landed-resolve 仲裁**：出错的 resolver 若分支 tip 前移 ⇒ 视为已解决
  （row 记 landed reason，下一 tick 直接 merge，绝不重解）；D) briefing 改为
  **COMMIT FIRST** + 有界验证（全量 suite 明确不要求）

### PA 自身教训
- **ps greps 关键词误判僵尸事故**：gopi 子进程命令行是 `/home/user/work/GoPi/bin/gopi`
  （无参数）→ `grep 'gopi-merge-resolve'` 永不匹配 → 误判 3 个活行/活 resolver 为僵尸
  → cancel 活 resolver 行 + 删其 worktree。**正确做法**：查 claimed_by PID 存活
  或 pgrep -P <父 pid>，绝不用 grep 关键词。
- **同一误判的第二次代价（补充）**：把 resolver 子进程自己的 auto-registration 行
  当成"第二个 resolver / 第二个 merger"（pid 3128439 实为子进程自身 pid）
  → 据此提交了 bug 01a089ce（两个 merger）——该 bug **撤回/应关闭**。
  **教训**：`claimed_by` 的 pid 只证明"谁写的这一行"，必须再对 `session_id`
  才能判定"谁在跑"；先核对 session_id，再下并发结论。

### 真实 bug 修复（全部完成/进行中）
1. merge-backlog dedup ✅ merge 1accf36
2. worktree ownership ✅ merge b1a3ee6
3. **收敛门 tail-move bug**（LLM 自投 01a08987）✅ completed
4. **env-leak 测试隔离**（01a08987 姊妹）✅ merge 23369f2
5. **data race**（LLM 自投 01a0898e）🔄 in_progress
6. **merger 重复派发**（新发现）→ bug 01a089b7 pending

### master 状态
d75818e（含 reframe + hippocampus + 全部 bug 修复 merge）

## 七、待办/未决
- data race 修复验收
- merger 重复派发 bug 修复（01a089b7）
- IMPOSSIBLE 档真实触发（未出现——合理）
- hippocampus 全链完成后：真实使用观察（收敛门效果已测：1.00→0.17/0.33 per round）
