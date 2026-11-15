<!-- doc-class: normative -->
# 夜间报告 — 2026-09-10 深夜（the operator 睡前全权委托）

**报告时间**：2026-09-10 23:05
**委托**："make good use of night time and I will check with your report tomorrow morning"
**结论先说**：⚠️ **HP 的 GLM 账户余额耗尽（错误码 1113），23:00 免费窗口并未恢复**。我做了实测确认并把 HP 的全部任务迁回 peer1（DeepSeek）。当前 peer1 2 个 worker 在跑，队列 13 个任务。

---

## 一、⚠️ 最重要的事：GLM 余额耗尽（阻塞）

### 现象
HP 上的 gopi 任务在 22:53 死于：
```
[1113][余额不足或无可用资源包,请充值。]
http_status: 429
```

### 我做过的验证（不是猜测）
| 时间 | 测试 | 结果 |
|---|---|---|
| 22:54（窗口外） | GLM API 直测 | ❌ 429 余额不足 |
| **23:00:10（窗口内）** | **GLM API 直测** | ❌ **仍然 429 余额不足** |
| 22:55 | DeepSeek API 直测（peer1） | ✅ HTTP 200 正常 |

**结论：免费窗口救不了——这是账户余额问题，不是窗口问题。**

### 我的处置
1. **HP 的 5 个任务全部迁回 peer1**（4 个 pending 重新提交 + 1 个被杀的重提）
2. **移除 HP 的夜间 cron 兜底**（启动无意义，只会撞 429）
3. **终止 peer1 等待器**（它仍在往 HP 派 worker）
4. **清理 HP**：无 worker、无活跃任务、无 cron

### 需要你决定
GLM 账户需要充值才能继续用。在充值之前，**HP 无法作为算力节点**——所有任务都在 peer1 用 DeepSeek 跑。

（如果 GLM 的免费窗口是"资源包"形式而非"余额"，可能需要检查账户后台是否有未激活的包。）

---

## 二、已完成并验收的任务

### ✅ 语音任务 1：whisper.cpp 评估（51.5KB，3 commit 已进 master）

**关键实测结论**：

| 问题 | 结论 |
|---|---|
| 编译 | ✅ 2 分钟，无需网络 |
| **静态单二进制** | ✅ **可以**（`BUILD_SHARED_LIBS=OFF` → 7MB 零依赖，实测可运行）——**保住 gopi 的哲学** |
| **中文质量** | ⚠️ **tiny/base 会听错**（"语音识别"→"云时别"/"与音时别"）；**small 才对** |
| **速度** | tiny ~2.4s / base 4-5.5s / **small 15-42s（比实时慢，RTF 4.1）** |
| 流式 | ❌ **无真流式**（`examples/stream` 自述"very quick-n-dirty"，是滑动窗口重跑） |
| **VAD** | ✅ 真的（Silero，实测 0.1-2.8s，比实时快） |
| server 模式 | ✅ 有，**但仅 5 路由**，**无 SSE/WebSocket**，单 mutex 串行化 |
| **host-b .13** | ⚠️ ping 通（0.84ms RTT）**但拒绝免密 SSH**（measured blocker） |

**它诚实列了 10 条开放问题**，其中两条最有价值：
- **Volcengine 价格未验证**（官方页 JS 渲染抓不到 0 字符）——明确标注"不要当事实引用"
- **量化模型（q5_0）中文质量未测** → 标为"**最高价值的后续实验**"

### ✅ 语音任务 2：跨机设计（39.4KB，已 merge）

**最小设备契约 = 一行命令**（设备零安装）：
```bash
ffmpeg -f alsa -i default -ac 1 -ar 16000 -f s16le - | curl -sS -T - http://192.0.2.6:8088/v1/audio
```

**推荐注入点 = `POST /api/chat`**（`cli_web.go:818`）——唯一能注入**运行中** agent 且有**返回通道**（SSE）的点，且无需改 gopi 源码。

**它发现了一个真实隐患**：
> 说一句 "slash task list" → 因为以 `/` 开头，会被路由到 **slash command 而不是模型**（`cli_web.go:569-571`）

---

## 三、覆盖率 campaign（你定案的方法论）

### 度量基础设施（已提交 `e6336f9`）
- `scripts/func_coverage.py` — 函数级覆盖率（保守：函数名须在同包 `*_test.go` 出现）
- `scripts/test_func_coverage.py` — 9 断言准确性测试（**含与 `go doc -all` 独立交叉验证**）

### 基线
```
473 导出函数 / 343 覆盖 = 72.5% / 缺口 130
```

⚠️ **验证时抓到真 bug**：原正则漏掉 `func (Type) Method()`（接收器仅类型名，合法 Go）→ 少报 5 个函数。**靠与 `go doc` 交叉验证抓到**。

### 语句级交叉印证
**`cmd/agent` 仅 48.2%** ← 最大隐患（CLI 层；最近 bug 全在这：worker -h 跑循环×3 / task submit payload 空 / memory 裸命令进 agent 循环）

### 已派任务（peer1，13 pending）
| 任务 | 覆盖 |
|---|---|
| 覆盖率试点 | internal/taskverdict（2 函数，最小闭环） |
| **tools sync 族** | 16 缺口（claim/delta/reconcile/session/task） |
| **tools 生命周期族** | 14 缺口（session 目录/attachments/cleanup/worktree/vision） |
| **gopilog** | 5 缺口 |
| **hippocampus** | 6 缺口 |
| **shot** | 1 缺口（CDP 截图="眼睛"） |
| **deepseek** | 9 缺口（关键路径 wire/SSE/retry） |
| **config** | 6 缺口（EnvBool/ResolveSettingsPath 等，有 bug 历史） |
| 语音跨机（新） | 已合并 |
| CH9329 / webcam / Tab-order / 全景 / 游戏 / 编码规范 / 屏幕捕获 | 战略 + 实现 |

每个任务都要求：**逐行读 + code_search 查定义和所有调用方（引 file:line）+ 边界与错误路径测试 + memory_store 记录 + 工作流反馈**。

---

## 四、PA 的失误与教训（诚实记录）

### 失误 1：误杀运行中的 worker
清理测试进程时用了 `pkill -x gopi`，**杀掉了正在跑 gopilog 任务的 worker**。

**教训**：清理进程前必须确认没有正在执行的任务。

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

### 失误 2：`pgrep -f "gopi worker"` 自我误匹配
兜底脚本用 `pgrep -f "gopi worker"` 检测 worker，**会匹配到脚本自己**（命令行含该字符串）→ 明明没 worker 却报告有。

**修复**：改成 `pgrep -x gopi`（精确匹配进程名），并**用实验验证**（起一个真 worker 看哪种匹配对）。

### 失误 3：在免费窗口之前启动 worker
我 22:43 就启动了 HP worker（窗口 23:00 才开始）→ 撞上付费时段的余额不足。

**教训**：时间敏感的部署要严格核对窗口。

### 观察：状态机不允许 claimed → pending
清理陈旧 claim 时发现：`claimed` 只允许 → `in_progress|cancelled`。所以无持有者的残留只能用 cancel——这是设计选择（claimed = "我承诺跑"，要么跑要么取消）。

---

## 五、当前状态总览

| 平台 | 状态 |
|---|---|
| **peer1** | ✅ DeepSeek 可用；**2 个 worker** 在跑；13 pending |
| **HP** | ⚠️ **GLM 余额耗尽**；已清理（无 worker/无任务/无 cron）；等充值 |

**peer1 在跑的两个任务**：
1. CH9329 硬件 HID 评估
2. Webcam Capture（闭环伺服版）

**其他常驻**：`task merger`（每 5m）、`gopi chat`

---

## 六、醒来看这些

```bash
# 1. 覆盖率推进情况
cd ~/work/GoPi && python3 scripts/func_coverage.py

# 2. 语音报告（推荐先读 §0 TL;DR + §H 开放问题）
less devdoc/voice-input-whispercpp-evaluation.md

# 3. 跨机设计（§5 注入点是核心）
less devdoc/voice-input-design-space.md

# 4. 队列
./bin/gopi task list --status in_progress
```

## 七、待你决策

1. **GLM 账户充值**（否则 HP 无法使用）——\u4e5f\u8bf7\u68c0\u67e5\u8d26\u6237\u540e\u53f0\u662f\u5426\u6709\u672a\u6fc0\u6d3b\u7684\u514d\u8d39\u8d44\u6e90\u5305
2. **中文 STT 选型**：`small` 准但慢（15-42s）/ `tiny+base` 快但会听错
3. **`internal/testutil`（29 缺口）与 `devdoc/archive`（5 缺口）是否纳入 campaign**（我建议：testutil 中优先、archive 排除）
4. **`cmd/agent` 48.2% 语句覆盖**——是否需要单独派任务（它没有函数级统计口径，是盲区）

---

## 附录 A — 证据链：HP worker 的 23:00 死亡现场（原始日志）

等待器在 23:00:00 精确触发并启动了 2 个 HP worker（pid 1778185 / 1778837）。
它们立即失败，`~/.gopi/worker-night-1.log` 原文：

```
tasklist: ever_established=false n_lists=0 n_malformed=0 first_ratio=-1.0 (never established)
elapsed=14.6s
Error: LLM call failed (stream: deepseek: API error (status 429):
  {"type":"error","error":{"type":"rate_limit_error","code":"1113",
   "message":"[1113][余额不足或无可用资源包,请充值。]"}}
  after 3 retries, fallback: deepseek: API error (status 429): ... after 3 retries)
worker: run 01a08bc9-dd9c failed (continuing): child: exit status 1
worker: queue drained (1/3 idle rounds) — waiting 30s (new tasks picked up next tick)
```

### 两个额外发现

1. **重试是无效的**：`after 3 retries, fallback: ... after 3 retries` —— worker 对
   1113 做了 3 次重试**并**尝试了 fallback，但两者都打到同一个失效凭据。**账户级
   错误（余额/凭据）不应该重试**——这是纯粹的时间浪费。错误分类应该把 1113 这类
   "账户状态"错误标记为 non-retryable。

2. **fallback 在单 provider 配置下无意义**：HP 只配了 GLM，所以 fallback 还是 GLM。
   这印证了 gopi 的 provider fallback 设计在单节点单凭据场景下是空转。

3. **writeback 报了一个状态机错误**（无害）：
   `illegal status transition cancelled → failed (allowed from cancelled: )` ——
   因为我在 23:00 前已把该行 cancel（迁移到 peer1），worker 随后尝试写 failed。
   状态机正确拒绝了，但错误信息可读性一般（"allowed from cancelled: " 后面是空的）。
