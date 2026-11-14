<!-- doc-class: normative -->
# 夜间推进报告 — 2026-09-11（the operator 休息期间，PA 自主推进）

**交接**：07:25 ｜ **上次更新**：08:45 ｜ **状态**：进行中
**基线**：master `0bc42bd`（独立验证 EXIT=0 / 零 FAIL / 20 包全绿）

---

## 一、对表结果（the operator 要求）

### A. TODO 陈旧状态已修正（6 项已完成未标记）

| TODO 项 | 实际 | 证据 |
|---|---|---|
| Computer Use 长期目标 | ✅ 闭环已验证 | `screen_capture`+`act` E2E，commit 902bab4 |
| 语音输入 | ✅ 评估已交付 | whisper.cpp 评估 + 跨机通道设计 |
| chat/web 双模式对齐 | ✅ P0/P1/P2+testsuite 全 merge | master 2b5a562 起 |
| 大海马体记忆项目 | ✅ 已落地 | `internal/hippocampus/`，收敛门 1.00→0.17 |
| Reframe task | ✅ 已实现+E2E | task(action=reframe) 三档，merged 3ce88c2 |
| Tasklist 治理 | ✅ 全部完成 | 27d9d2b |

### B. TODO 有、台账无单 → 已补派

- **01a08da2-6c3d**：distilled.db 剩余 1426 条 evidence 来源治理（Rule 16 可寻址性）

### C. 今日产出已写入 TODO（5 项长期项）

Rule 16 全流程链条 / 能动性第一步 / cgroup 资源路线 / 测试基础设施 / GOPI.md 上枷锁

### D. 顺手清理

- `feat/webcam-capture-evaluation`（已合并）→ worktree + 分支删
- `feat/ban-tmp-scratch` → 被 worker 自动收编进 master

---

## 二、今日成果（master 上 06:00 后 = **46 commits，其中 6 次自动 merge**）

### 已收编的交付

| 交付 | 内容 | 验收 |
|---|---|---|
| `/tmp` 禁令全套（`7c96ecf`） | scratch 落 `~/workspace` + **硬 device-id 断言**（非 warning）+ 12 个脚本迁移 + 生产者修复 | PA 实测 device 2052≠2051 ✅ |
| Computer-Use 闭环（`902bab4`） | `screen_capture`（眼）+ `act`（手，**纯 Go X11，零 CGO**） | cap sha 变化证明闭环 ✅ |
| **测试套件严密性审查**（`0bc42bd`） | the operator 指出的漏洞：报告 `devdoc/test-code-discipline-audit.md`（311 行）+ `/tmp` 检查扩展到 Go 测试源码 | **独立全量 EXIT=0** ✅ |

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

| worker 死亡根治 | `~/bin/gopi-worker-up.sh`（setsid，PPID=1 验证） | 3 worker 全 PPID=1 ✅ |

### 我（PA）的交付

| 交付 | 内容 |
|---|---|
| **Rule 16 全流程链条**（`94612ea`） | 12 环节审计（7 断链点）+ "存储链条完整"判据 |
| **三条腿闭环**（`f615b48`） | 记得住→找得到→**想得起来要用**（=能动性本质） |
| **GOLDEN-RULES.md** | 10 条金句，每条必须带"来源事故"+"指导行为" |
| DS 文档归档（`bb76825`） | `~/docs/deepseek-api/` 72 页，幂等可重跑 |
| 余额探针 | 成本从估算→实测；**发现 pricing 表 bug**（`01a08daf`） |

### 修复的三个真问题（验证抓出来的）

1. **我的脚本**：归档器报告带时间戳破坏幂等（重跑非逐字节相同）+ 探针 None 解引用
2. **POC 目录 4 处 `/tmp` 漏网**（`6581b2d`）——新装的 `/tmp` 检查立刻抓到
3. **文档锚点漂移 2 处**（`16dfe7e`）——`internal/agent/loop.go:1904` 指向空行（该行号已属陈旧记录；同页 `:1903` 的 `CompactTriggerRatio` 才是当时所指）

---

## 三、正在跑（3 worker，全部 PPID=1）

| 单 | 内容 | 已跑 |
|---|---|---|
| `01a08d78` | 机械风格清扫（263 处 `%v`→`%w` / 吞错 / 测试卫生） | 1h02m |
| `01a08da4` | scratch GC 挂进已有常驻（**零新守护进程**）+ 检查"env 常量必须有人读" | 12m |
| `01a08daf` | pricing 表修 `deepseek-flash`（+新旧价不同，只加别名会高估 2.5 倍） | 2m |

## 四、队列（12 pending）

**LLM 自投（审查自然产出，非我派）** —— 这批最能说明机制在运转：

```
01a08dcd-0401  os.Setenv 泄漏进程状态（49 处）—— deferred restore 不等于 unset
01a08dcd-00d8  测试代码往开发者【真实 $HOME】写 scratch（read_test.go）  ← 比 /tmp 更隐蔽
01a08dcd-1ed0  gopi 二进制构建缓存 3 处重复（last-writer-wins）
01a08dcc-fef2  cmd/agent 测试二进制写固定 /tmp 路径且从不清理
01a08dd0-8b9a  8 个测试 unchecked os.Chdir（级联污染后续测试）
01a08dd0-aa50  等迁移完用带基线的 gate 强制 Rule 17（t.Setenv）
```

**研究/机制（我派的长期项）**：链条断链验证 / "先查"机制化 / 能动性第一步 / cgroup v2 研究

---

## 五、PA 的执行纪律（本夜遵守）

- 每条命令带 `timeout`（今天的事故教训）
- 不阻塞等待：长任务走 background + notify
- **验收看证据不看自报**（三次通知都是旧日志，逐次核对）
- 不按名杀进程（用 PID / cgroup / 命名空间）
- 记录持续更新（本文件）

## 六、时间线

- `07:25` 交接完成，3 worker + nightwatch 探针启动
- `07:38` DS 文档归档完成（72/72 零失败）
- `07:45` 发现 pricing 表不认 `deepseek-flash`（唯一成本工具失效）→ 派单
- `08:10` 测试套件审查交付，**自投 5 个新 bug 单**
- `08:27` Rule 16 + 三条腿 + GOLDEN-RULES 入库
- `08:32` 独立全量验证 `0bc42bd` = EXIT=0 / 零 FAIL
- `08:45` 对表 + 本文档更新

---

## 七、待 the operator 决策

1. **`make test` 的环境依赖**：`cmd/agent` 的 brew 测试需 `ANTHROPIC_AUTH_TOKEN`，无凭证时 FAIL。
   是否要让它在无凭证时 skip（而不是 FAIL）？
2. **Rule 16 是否写进 GOPI.md**：目前是独立 devdoc（避免与 `01a08d70` 改 GOPI.md 冲突）。
3. **Files API 用于截图任务**：vision 现走 base64 内联且历史每轮重发，
   改 `file_id` 可能显著降低 HOMM3/GUI 任务的上下文成本——要不要立项评估？
