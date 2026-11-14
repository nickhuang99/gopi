<!-- doc-class: normative -->
# Handoff — 2026-09-22 重启前状态

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> 撰写时刻：2026-09-22（周二）
> 重启原因：session 过长，浪费缓存（nick 指示）
> 本文档 = 重启后第一份要读的东西

---

## 0. 一句话现状

**正在做 DeepSeek 账单分析**（nick 的财务分析小品），已算出核心数字，
**卡在一个待 nick 确认的对比口径上**。其余是 GoPi 后台在跑的常态。

---

## 1. ⭐ 当前任务：DeepSeek 账单分析

### 1.1 素材（全在 `~/work/claude/nick/`）

| 文件 | 内容 | 状态 |
|---|---|---|
| `amount-2026-09-21_2026-09-21.csv` | 84 行 × 21 小时，token 明细 + **自带 price 列** | ✅ 已读 |
| `cost-2026-09-21_2026-09-21.csv` | 21 行，逐小时实收 | ✅ 已读 |
| `Agent账单的研究_fixed.txt` | nick 写的周六小品文（73 行） | ✅ 已读 |
| `deepseek_analysis_bill.txt` | 命中率敏感性推演（51 行，四家价格表） | ✅ 已读 |
| `deepseek_new_model_new_price.txt` | 价格表（含智谱/OpenAI/Anthropic） | ✅ 已读 |
| `~/docs/deepseek-api/quick_start/pricing.md` | 官方定价页（归档） | ✅ 已读 |

### 1.2 昨天（2026-09-21 周一）真实用量

```
缓存命中输入    1,156,607,857   11.57 亿
缓存未命中输入     21,077,655    0.21 亿
总输入          1,177,685,512   11.78 亿
总输出              9,483,108    0.09 亿
总计            1,187,168,620   11.87 亿
请求数                  13,085
命中率                  98.21%
单次均输出               725 token
实收账单                ¥126.32
```

### 1.3 与文章周六数据对比（nick 上轮问的「比例是否一致」）

| 项 | 周六（文章 §8） | 周一（昨天） | 比例 |
|---|---:|---:|---:|
| 总输入 | 15.74 亿 | 11.78 亿 | 0.75 |
| 总输出 | 0.169 亿 | 0.09 亿 | **0.56** |
| 请求数 | 22,433 | 13,085 | **0.58** |
| 命中率 | 98.09% | 98.21% | ✅ 一致 |
| 单次输出 | 700+ | 725 | ✅ 一致 |

**答案 = 三处都不一致，且不成等比**：输入降 25%，但输出降 44%、请求降 42%。
**推论**：昨天请求少 42%，但单次上下文从 70 万 token（周六）涨到 90 万 token（昨天），**+28%**。

### 1.4 四家价格同用量对比（已算完）

用量基准 = 昨天真实负载。算法自证：**算出的 DS 工作日值 ¥126.32 与实测账单完全相等，误差 0.00%**。

| 平台（元/百万） | 周末·全谷时 | 工作日·峰谷 | 峰谷溢价 |
|---|---:|---:|---:|
| **DeepSeek V4.1 Flash** (0.02/1/4) | **¥82.14** | **¥126.32** | **+53.8%** |
| 智谱 GLM-5.3-Flash (0.23/0.8/2.8) | ¥309.43 | ¥309.43 | 0% |
| OpenAI GPT-5.6 Luna (0.144/1.44/8.64) | ¥278.84 | ¥278.84 | 0% |
| Anthropic Claude Haiku 4.5 (0.72/7.2/36) | ¥1,325.91 | ¥1,325.91 | 0% |

- **周末**：DS 优势 3.4x（OpenAI）/ 3.8x（智谱）/ 16.1x（Anthropic）
- **工作日**：优势缩至 2.2x / 2.4x / 10.5x（对手价格不动，DS 自己涨 53.8%）

### 1.5 峰谷实测（★ 方法论可复用）

逐小时用谷时价 0.02/1/4 复算，再与账单实收比 —— **比值恰好 1.000 或 2.000，21/21 全中**。
**推断：任意 DeepSeek 账单，比值 2.0 的小时 = 高峰，1.0 = 谷时。**

昨天 53.8% 的 token 落在高峰窗口（9-12 / 14-18），所以溢价才有 53.8%（几乎 1:1 对应）。

### 1.6 官方规则（权威原文，别再搞错日期）

> 北京时间周一至周五（不含中国法定节假日）**9:00 - 12:00、14:00 - 18:00** 为高峰时段；
> 其余时段，**包括周末及中国法定节假日全天均为空闲时段**。

- **2026-09-21 = Monday（周一，有峰谷）** ← 用 `date -d '2026-09-21' '+%A'` 查证
- 2026-09-19 = Saturday（周六，全谷时）
- 2026-09-22 = Tuesday（今天）
- `~/docs/deepseek-api/quick_start/pricing.md:35` 英文原文写 "01:00-04:00 and 06:00-10:00 UTC, Monday through Friday" = 同一窗口

### 1.7 ⚠️ 本轮犯过的错（不要重犯）

1. **心算日期**：我算出"2026-09-21 是周日"并据此推出"周日也收峰时费"的发现 —— **全错**。
   nick 原话：「你个白痴，这个是周一啊！你为什么不能简单的date函数查到呢？」
   **铁律：任何日期/星期判断必须跑 `date`，绝不心算。**
2. **纠缠分布问题**：nick 说"周末全天都是波谷价"，我却在讨论"负载分布口径"（均匀 vs 实测）。
   nick 原话：「你再说什么？周末全天都是波谷价格，你理解吗？」
   **教训：他给出定价规则时，不要再把它复杂化成分布问题；直接算。**

---

## 2. 待办 / 下一步

| # | 事项 | 状态 |
|---|---|---|
| 1 | 账单分析是否要归档成 devdoc | **待 nick 确认** |
| 2 | 「53.8% token 撞高峰」是否要优化（查昨天哪些任务跑在 14-18 点） | **待 nick 确认** |
| 3 | 是否要把四家对比算进文章修订 | **待 nick 确认** |

---

## 3. GoPi 后台常态（重启会杀掉这些）

### 3.1 重启会杀死的进程

| 进程 | 说明 |
|---|---|
| `gopi resume 01a0c35f-c516-...` | nick 自己起的，在 `~/workspace/gopi-tasksync-switch` 里跑，**重启会杀** |
| `gopi worker`（pid 935164 等） | Hermes 会话子孙，**重启会杀** |
| 各种测试沙盒 daemon | 无主，可不管 |

**重启后如需继续后台干活，用**：
```bash
cd ~/work/GoPi && setsid nohup ./bin/gopi worker > /tmp/gopi-worker.log 2>&1 < /dev/null &
```
（`~/bin/gopi-worker-up.sh N` 是 OPERATOR-ONLY，PA 禁用）

### 3.2 在跑的任务 / 分支

| 分支 | 状态 | 说明 |
|---|---|---|
| `feat/task-sync-switch-off` | ahead 5 / behind 40+ | `01a0c35f-c516-78a1-b385-99a234f14626`，**nick 已 resume 且正在跑**，worktree = `~/workspace/gopi-tasksync-switch` |
| `fix/merge-llm-resolves-conflicts` | ahead 15 | `01a0c37d-ebdd-7b4e-9934-6174ad509e0f`，可 resume |

### 3.3 本会话已派的单（全部已 commit brief）

**name/path freshness 三张**（brief commit `c91a117f`）：

```
01a0c53c-b770-7b66-a5d4-39d9e4e7cfc4   worktree path must be fresh
01a0c53c-bbcc-7cbf-8bf9-d78a3f03e6d4   pushback name collision unguarded
01a0c53c-c029-7d0b-9f98-71fc9069e632   branch creation needs a precheck
```

**此前派出（全在队列）**：

```
01a0c3db-f16d-73ae-9c35-0a489f981a3a   假账本：run 被截断记成 completed（p1）
01a0c3ce-a331-74cf-b218-ecd12508d3de   code_search EOF panic（p1）
01a0c36d-6bbd-7653-8bf6-62d009bc1f9f   merge 必须 mint version tag（p1）
01a0c52b-23ba-7776-be52-cd404c2afb01   gopi resume 从不关闭自己的行（p1）
01a0c3db-f42c-7284-adae-c270b20d16b8   TestAssertionTotalityAuditGate RED（p1）
01a0c3c8-1daa-7285-a2cb-7c019c571af9   统一沙盒（p5）
```

### 3.4 查台账的正确命令

```bash
# 全 id（绝不缩写！nick 铁律）
sqlite3 ~/.gopi/memory.db "SELECT task_id||' | '||summary||' | '||status FROM tasks WHERE status='pending' ORDER BY priority;"

# 查单
sqlite3 ~/.gopi/memory.db "SELECT task_id,summary,status,claimed_by FROM tasks WHERE task_id LIKE '01a0c53c%';"
```

---

## 4. 本会话的重要发现（已在 memory，但重启后要记得）

1. **`failed → cancelled` 非法**（`tasks.go:3093` 只允许 `{verified, completed}`）。payload=0 的 auto 盖章行被 `TaskNextFiltered` 过滤，永不领取 → 留着无害。
2. **合并那趟的 worktree 保护 = dead holder 陷阱**（当年 `merge_run.go:496`；该顶层动词已于 2026-11-01 删除，引擎在 `gopi task merge`）：分支被 worktree 占用就跳过，**不管里面有没有活进程**。判活唯一可靠 = 扫 `/proc/<pid>/cwd`。
3. **被 kill 的任务 = 中断待 resume，不是完成待 merge**（nick 已追责两次）。
4. **worktree 空目录会被静默复用**（实测：非空→`fatal: already exists`；空→成功）。
5. **账单自带 price 列**，能直接读出峰谷两档价，不需要外部价格表。

---

## 5. 重启后的第一件事

1. 跑 `date` 确认今天日期
2. 读本文件
3. 问 nick：账单分析是否要归档 / 是否要优化避峰调度
4. 如需后台干活，起 worker（setsid 命令见 §3.1）

---

**devdoc 路径**：`~/work/GoPi/devdoc/handoff-20260922-bill-analysis.md`
**待 commit**
