<!-- doc-class: historical -->
# 夜间交接 — 2026-09-10（最终版）

**时间**：2026-09-10 23:10
**委托**：make good use of night time + 早上看报告

---

## 🚨 第一件事：GLM 账户余额耗尽（需要你处理）

### 现象
HP 上的 gopi 任务全部失败于：
```
429 {"code":"1113","message":"[1113][余额不足或无可用资源包,请充值。]"}
```

### 我的验证（两个时间点）
| 时间 | 测试 | 结果 |
|---|---|---|
| 22:54（窗口外） | GLM API curl 直测 | ❌ 429/1113 |
| **23:00:10（窗口内）** | **GLM API curl 直测** | ❌ **仍然 429/1113** |
| 22:55 | DeepSeek curl 直测 | ✅ HTTP 200 |

**结论：免费窗口不能恢复余额不足**（账户级阻塞 vs 时段优惠是两回事）

### 处置
- HP 的 5 个任务**全部迁回 peer1**（DeepSeek）
- 移除 HP 夜间 cron + 终止 peer1 等待器（都会撞 429）
- HP 清理干净：无 worker / 无任务 / 无 cron

### 需要你
**给 GLM 充值**，或检查账户后台是否有未激活的免费资源包。

---

## 📊 当前运行状态

### peer1（唯一执行平台，DeepSeek 可用）
| 项 | 值 |
|---|---|
| workers | **3 个** |
| in_progress | 3（CH9329 / webcam / Tab-order） |
| pending | 13 |
| 常驻 | task merger（每 5m）+ gopi chat |

### HP
离线待充值。队列空、无进程、无 cron。

---

## ✅ 已完成并验收

### 语音任务 1：whisper.cpp 评估（51.5KB，3 commit 进 master）
| 问题 | 结论 |
|---|---|
| **静态单二进制** | ✅ 可行（7MB 零依赖）← **保住 gopi 哲学** |
| **中文质量** | ⚠️ tiny/base **会听错**（"语音识别"→"云时别"）；**small 才对** |
| **速度** | tiny 2.4s / base 4-5.5s / **small 15-42s（比实时慢）** |
| 流式 | ❌ 无真流式 |
| **VAD** | ✅ 真的（Silero，比实时快） |
| server | ✅ 仅 5 路由，无 SSE/WebSocket |
| **host-b .13** | ⚠️ 拒绝免密 SSH（measured blocker） |

10 条开放问题，其中：**Volcengine 价格未验证**（标注"不要当事实引用"）、**量化模型中文质量未测**（标为"最高价值后续实验"）。

### 语音任务 2：跨机设计（39.4KB，已 merge）
- **最小设备契约 = 一行命令**（设备零安装）
- **推荐注入点 = `POST /api/chat`**（唯一能注入运行中 agent + 有 SSE 返回通道）
- **发现真隐患**：以 `/` 开头的转写会被路由到 slash command **而非模型**（`cli_web.go:569-571`）

### 覆盖率基础设施
- 工具 + 9 断言测试（**含 `go doc` 交叉验证**）已提交
- **基线：473 函数 / 72.5% / 缺口 130**
- **抓到真 bug**：正则漏 `func (Type) Method()` → 少报 5 个 → 靠交叉验证发现
- **`cmd/agent` 仅 48.2%**（最大隐患：CLI 层，最近 bug 全在这）

---

## 📋 已派任务（peer1 13 pending）

| 类型 | 任务 |
|---|---|
| **覆盖率**（8） | 试点 taskverdict / tools-sync 族 16 缺口 / tools-生命周期族 14 缺口 / gopilog 5 / hippocampus 6 / shot 1 / deepseek 9 / config 6 |
| **Computer Use 研究**（5） | CH9329 / webcam 闭环 / Tab-order / 全景切口 / 游戏 agent |
| **实现**（2） | 屏幕捕获+键鼠工具化 / 编码规范收集 |
| **新 bug**（1） | LLM 账户级错误不该重试（今晚发现） |

---

## 🔍 今晚发现的 2 个设计问题（已转工单）

从 HP worker 死亡日志（`worker-night-1.log`）挖出：

```
Error: ... 429 {1113 余额不足} after 3 retries, fallback: ... 429 {1113} after 3 retries
```

1. **账户级错误不该重试**——"余额不足"重试永远不会成功，纯浪费 3 次时间
2. **fallback 在单 provider 下是空转**——HP 只有 GLM，fallback 还是 GLM

→ 工单 `01a08bda-f706`（tags=bug，优先级已自动提升）

---

## 🧪 PA 的失误（诚实记录）

1. **误杀运行中的 worker**（清理测试进程时）

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

2. **`pgrep -f "gopi worker"` 自我误匹配**（脚本 bug；已改为 `pgrep -x gopi` 并**用实验验证**）
3. **在免费窗口前启动 HP worker**（22:43 vs 23:00）——白撞了一次余额不足

---

## 📚 记忆与文档

- `devdoc/night-report-20260910.md`（主报告，含证据链附录）
- `devdoc/handoff-20260910-2250.md`（交接）
- 蒸馏入库 4 条（8624-8627），**闭眼自测 6/7 命中**（并修复了自动生成 search_kw 的质量问题）

---

## 🌅 醒来看这些

```bash
# 覆盖率推进
cd ~/work/GoPi && python3 scripts/func_coverage.py

# 主报告
less devdoc/night-report-20260910.md

# 语音两报告（§0 TL;DR + §H 开放问题最值得看）
less devdoc/voice-input-whispercpp-evaluation.md
less devdoc/voice-input-design-space.md

# 队列
./bin/gopi task list --status in_progress
```

## 待你决策
1. **GLM 充值**（HP 才能复用）
2. **中文 STT 选型**：small 准但慢 / tiny+base 快但会听错
3. **testutil（29 缺口）与 devdoc/archive（5）是否纳入覆盖率 campaign**（建议：testutil 中优先、archive 排除）
4. **cmd/agent 48.2%** 是否单独派任务（无函数级口径，是盲区）

---

## 🔄 更新（00:57）：HP 已切换到 DeepSeek，算力恢复

**你的指令**："if not (free), then there is no point to use it, change HP config to deepseek-flash to run test cases"

### 执行
1. **备份 GLM 配置**：`~/.gopi/settings.json.bak-20260910-glm` + `.glm_settings.json.bak-20260910-nogLM`
2. **创建 DeepSeek 配置**：从 peer1 复制 `.ds_flash_settings.json` → HP `~/.gopi/.ds_hp_settings.json`
3. **API 直测验证**：HP → DeepSeek `HTTP 200`（真实调用成功）
4. **切换 symlink**：`settings.json → .ds_hp_settings.json`
5. **冒烟测试**：真实 agent turn 通过（`HP-DS-SMOKE-OK`，18.5s，cache 97%）
6. **同步源码**：HP 到 `b7b8000` + bin 重建
7. **重派任务**：5 个覆盖率任务重新提交（gopilog/hippocampus/shot/deepseek/config）
8. **启动 worker**：2 个（pid 1787516 / 1787552）
9. **恢复 cron 兜底**：`~/bin/hp-worker-autostart.sh`（每 10 分钟，`pgrep -x gopi` 正确检测）
  **[RETIRED 2026-09-26 — the schedule is gone and the script now REFUSES an automatic caller:** operator ruling 「这个 cronjob 必须终止，我们不能依赖这种系统来做，它掩盖了问题。」— the cron net MASKED a worker-death loop (1,214 delegations); `scripts/hp/hp-worker-autostart.sh` + `hp-worker-nightsafe.sh` refuse any scheduler context. **Do not re-install it: no replacement auto-launcher, no escape hatch.** See devdoc/hp-worker-autostart-retired-20260926.md.]**

### 验证
```
HP worker: 2 个在跑
队列: in_progress=2, pending=4
LLM 错误: 无 ✓
```

**HP 现在是 DeepSeek 驱动**（peer1 同款配置），不再依赖 GLM。

**代价认知**：DeepSeek 没有免费窗口，所以 HP 现在**跑任务是要花钱的**——但按你的判断，GLM 不能用时，花点钱换可用算力是值得的（DeepSeek 空闲价 0.02/1/4 元每百万 token，实际有 97% cache 命中，成本可控）。

---

## 📦 夜间新增交付（2 小时内 13 个任务完成）

除了已提到的，还有：

### 编码规范清单（`devdoc/coding-standards-checklist.md`）
你要求的"可机械检查的规则"——含来源、检测方法、我们的违规历史。

### 游戏 agent 试验场设计（`devdoc/game-agent-testbed-design.md`）
**含你关心的变现分析，结论是诚实的负面**：

- **边际成本与售价同量级**（实测 1.43 元/游戏小时 ≈ $12/月 vs 订阅 $10-20/月）
- Steam 508K 评论显示：**披露生成式 AI 会降低推荐率**（营销负债）
- GPT-4 曾在 Civ VI **输给启发式 AI**（"玩得好"未解决）
- **结论**："this work is a **capability project, not a business**"

**它给出了可证伪的第一步**（`episode-1`：在一个 sgt-* 拼图游戏上做 10 轮闭环伺服，记录收敛数据）——**产出知识而非收入**，符合你的约束。

---

# 🌅 凌晨更新（04:30）— 两个重大事项

## 1. 🔥 Computer-Use 闭环验证成功（今晚最大突破）

**你要求的"眼睛和手"已经落地并端到端验证**：

```bash
DISPLAY=:99 ./bin/gopi screen_capture --out /tmp/b.png   # 眼睛
DISPLAY=:99 ./bin/gopi act move 400 300                  # 手
DISPLAY=:99 ./bin/gopi screen_capture --out /tmp/a.png   # 验证
```

**PA 亲测（不是我写的代码，我独立验证的）**：

| 步骤 | 结果 |
|---|---|
| `act info` | pointer 640,400；root 1280x800；只读探测不注入 |
| `screen_capture` | sha256_16 = `9adfd46934aa465c` |
| `act move 400 300` | pointer 移到 (400,300) |
| `screen_capture` | sha256_16 = `446fd15bb4aeebcc` ← **变了** |

**独立交叉验证**（不信工具自报）：`file` / ImageMagick `identify` / `sha256sum` 三方与工具输出**完全一致**（1280x800、25697 bytes、同一 hash）。

**技术特性**：
- **纯 Go 手写 X11 协议客户端**（`GetImage` 128px 分带抓取 + XTEST 输入注入）
- **零 CGO、零新依赖、不需要 xdotool**
- **已是 toolcall**（`screen_capture` 注册为工具，不只是 CLI）
- **LLM 采纳已实证**：真实会话中模型主动选 `screen_capture` 而非 `bash`+`import`，三次调用参数正确

**故意未做**（诚实记录）：窗口定向捕获 / Wayland / 完整键映射。

详见 `devdoc/COMPUTER-USE-CLOSURE-VERIFIED.md`。

## 2. 🔧 根治：worker 必须 setsid 启动

**今晚 3 次同因事故**：通过 Hermes background 会话启动的 worker，在会话被扰动时收到 SIGTERM 而死，留下 in_progress 僵尸行（holder pid DEAD）。共产生 4 个僵尸行。

**根因**：worker 是 Hermes 会话的子孙进程 → 会话拆卸连带杀掉。

**根治**：新建 `~/bin/gopi-worker-up.sh [N]`，用 `setsid nohup ... &` 启动，
**验证标准 = `ps -eo pid,ppid,cmd` 显示 PPID=1**（被 init 收养）。现已用此法启动 3 个 worker，全部 PPID=1。

## 3. 覆盖率继续推进

| 指标 | 数值 |
|---|---|
| 总计 | **85.5%**（414/484） |
| 100% 的包 | **6 个**（config/deepseek/gopilog/hippocampus/shot→（新 X11 代码待补）/taskverdict） |
| 剩余 | testutil 37（待决策）/ tools 19（已派 HP）/ agent 15（已派 peer1）/ shot 4（新 X11 层，已派）/ archive 5（建议排除）/ pricing 2 / proxy 1 |

**新派两个针对性任务**：
- `internal/shot` 的 4 个缺口 = **XTEST 层**（`Display`/`Focus`/`Click`/`TypeText`）——要求**钉住 X11 请求的字节布局**（协议级测试，不依赖显示器），这是"手"的核心
- `internal/tools` 剩余 19（含 `ScreenCapture`/`TaskVerify`/`TaskBriefing`）

## 4. 收编的分支（4 个，全部验证）

`screen-capture-actuation-tools` / `write-task-area-schema-timeout` / `task-verdicts-unknown-bucket` / `gopilog-httpview-removal`

验证：build ✅ vet ✅ tools 162s ✅ testutil ✅ shot 40.5s ✅
