<!-- doc-class: historical -->
# 夜班完整报告（2026-08-29 20:00 — 08-30 09:30）— draft（待两 Bond 收尾后定稿）

## 一、BIG-GOAL 记忆工程：四期全部闭环 ✅
| 期 | 内容 | master | 核心数字 |
|---|---|---|---|
| M1 | 检索质量地基（exam_questions+5W1H） | cfa8d5c | top1 36%→50%, top3 43%→64%, 100% 填充 |
| M2 | 生命周期（tier+惰性衰减+feedback 闭环） | 621a60e | 12 项投票零回归, Rule 11 三会话 |
| M3 | brew 自产知识 | 12eca48 | 4 lessons 落库, 检索 3/3, gate 拒 3 |
| M4 | 跨节点同步（memsync PR 语义） | af7cafb | export 闸门+import 幂等+provenance |
| 附 | exact-dedup 引擎 bug 修复（>200 rune 行） | — | E2E 抓到 217-rune 复制 |

## 二、M4.5 台账治理七件套 ✅（master 8e1342a）
new 删除 / update 收窄 status-only / next 收缩+claimed 瞬时化 / serve flock+--skip-tag / 墓碑归档（6 failed→resolved）/ ingest :8923 / 存活追踪列+stale 检测。bug027 同族预存红顺手消除。生产 serve 常驻（every 10m+skip pa-queue+notify→task-events.jsonl）。

## 三、架构四砍定案（devdoc/centralized-architecture-20260830.md）
estimate 系数→原始时间戳 / HTTP ingest→ssh CLI / tag 路由→负载均衡计数 / nodes.yaml→settings 白名单。
终稿：peerbox 唯一队列+无状态计算位（二进制+settings+ssh 免密）+CLI 回报+git push back+binary scp 分发。
教训固化：设计评审必答题"这个层删掉会怎样？"

## 四、双节点并行首战
HP 通道全通（GLM key/源码同步/重建/memory.db rsync/smoke HP-GLM-OK/push-back 闭环 HP-CHANNEL-OK 实证）。
负载失衡（6 vs 1）诊断=僵尸冒充（#79/#78 10h 无写入）+占位误领 → 清理+手动 LB（SELF-REVIEW 复制到 HP）→ 真实并行：peerbox 写代码+HP 读代码。

## 五、事故与修复
1. GLM SSE/DNS 抖动 ×2 → M4.6+M4.7 首战死 28m（无代码损失，设计成果从 thinking 抢救进重派 briefing）→ resume 续命（byte-exact 211KB 恢复实测）
2. distilled.db 头损坏（magic 24B 被覆写）→ 从 bak-20260730 借头修复 → 7137 行无损，VACUUM+post-fix backup
3. bug028（PA 发现）：serve 领到无 payload 行→refuse→os.Exit→守护进程静默死亡 ×3 → 工单（TaskNextFiltered 加 payload 非空过滤）
4. skip-tag 失配（tags 反复被打空）→ 数据修复+payload 过滤方案进 bug028 单

## 六、价格修正（HP 截图+vision 三重互证）
0.8/2.8/0.23 是原价；五折实付 0.4/1.4/0.115（算术自洽）。9/9 后策略翻转未触发——GLM 双窗口期主力。

## 七、SPA 截图+vision 范式 ✅（pa-q-spa-screenshot-paradigm 首战）
HP playwright chromium-1228 → 裁条带+DOM 互证 → devdoc/assets/ 证据。gopi shot 工单（t-1788043085）排队。

## 八、工单矩阵（当前）
- 跑中：M4.6+M4.7 重派（370b2c57 resume 续命）/ SELF-REVIEW（HP e724d04e）
- pending 高优：bug028 serve Exit / resume --task 关联 / model stamp（执行时回填）/ stale sweep 位置
- pending 特性：gopi shot / M4.7 尾巴（三源+LB）/ verify-loop 验收（等 schema 基线，已就位）
- 已闭环墓碑：CNY 截图 / spa 范式 / bug027 / 断言过时×2

## 九、PA 行为复盘（按 nick 四纠正自查）
- ✅ 全部执行走台账/background+notify，无阻塞等待
- ✅ 四次被砍层后不再加层（stale sweep 挂现成 tick、回报走 ssh CLI）
- ✅ 验收=读证据链+抽查（未逐断言手验）
- ⚠️ 仍有改进空间：发现 bug028 前生产 serve 死了三次才对账出来——pull 盲区在订阅端事件流里补齐后消失

## 十、SELF-REVIEW 首战战果（HP Bond）✅
6 个 bug 单（3 major / 3 minor），全部结构化 tags=bug+severity：
- **major ①skip-tag 队头阻塞饿死**（与 PA 独立发现的 bug028 呼应——双发现交叉验证）
- **major ②legacy 重建迁移丢 FTS 同步触发器**（旧行从 tasks_fts 永久消失——SQLite RENAME 语义坑）
- **major ③web 默认绑 0.0.0.0**（无审批门禁执行面暴露 LAN，违背 loopback-only 设计意图）
- minor ×3：exit-check 每轮告警刷屏 / isNonZeroExit 漏 4 个退出码 / retry.go debugLog off 判真
基线对比：前 4 条（LLM 首批自投，五要素齐）vs 今 6 条（+severity 分级，major 有复现）——**bug 自投通道规模化质量达标**。
跨节点回流：6 单已迁移 peerbox 台账（t-1788053514×6，from-hp tag，payload 带原 HP id）——手动通道实证，M4.7 后自动化。
code_search 采纳率：Bond 自报 grep 计数内含 fallback 使用（worktree 内 keyword_search 损坏）——精确数据待 JSONL 核实（PA 下轮）。

## 十一、M4.7 派 HP 执行（手动 LB 第二次）
HP self-review 完成后接续：peerbox submit 记账（t-1788053642）→ payload scp → HP cat|gopi 执行（feat/remote-dispatch-hp，session bcbf1eed）。
中心化过渡期派工模式实证完成——M4.7 落地后此循环自动化（serve tick 计数选节点→ssh 派发→remote 回账）。
双节点三任务：peerbox M4.6（resume 续命）+ HP M4.7 实现 + HP self-review ✅。

## 十二、SELF-REVIEW 验收 merge ✅（master bca2e81）
HP Bond 28m45s 完成 ~7800 LOC 审查（4 文件域），产出：6 bug 单（3 major/3 minor，全 5 要素+文件行号锚点+repro test）+ 3 项 investigated-and-cleared（负发现记录）。
亮点：major ① skip-tag 队头阻塞与 PA 独立发现的 bug028 交叉验证；cleared 列表把"像 bug 但不是"的脆弱性也记录（brewGuard drift 被 similarity screen 兜住）。
HP 侧 worktree/分支已清；peerbox bin/gopi 已重建。

## 十三、M4.6+M4.7 验收 merge + fix-3major ✅（master c16fe41）
**resume 续命成功案例**：M4.6+M4.7 首战死于 GLM SSE（28m 纯设计）→ resume byte-exact 恢复（78 messages/211KB）→ 完整交付（a4ba5cc）——resume 价值的活验收。
- **M4.6**：chats 表（chat/web 会话索引，hermes 40 列对比表在 devdoc/task-ledger-m46-47.md）+ taskregister 钩子分流
- **M4.7**：ComputeNode 白名单三源（CLI>env>settings.json）+ 负载均衡计数 dispatch + report_to/repo 回程参数
- **fix-3major**（HP 并行交付 b6f22e3）：skip-tag 队头阻塞+FTS 触发器 rebuild+web 0.0.0.0→127.0.0.1
- 意外 1：merge 后主仓 build FAIL——chatregister.go/dispatch.go 被 .gitignore 吞（agent 裸模式第 3 次复发），从 worktree 恢复+add -f（4f91e39/e4cd331）。根治建议=.gitignore 改精确路径或 CI 检查。
- 意外 2：HP fix-3major 与 M4.6+M4.7 同时 push→cli_tasks.go 冲突 2 处→保留 HEAD 超集（c16fe41）
- 最终全绿（tools 372s / agent 65s / cmd agent env ok），HP 已同步 c16fe41

## 十四、夜班收官面板
- master c16fe41：M1-M4.5 + SELF-REVIEW merge + fix-3major + M4.6/M4.7 全部落地
- 三节点：peerbox（大脑+serve+ingest）/ HP（fix 批完成）/ RPi5（arm64 就绪待轻任务）
- 待办尾清：minor×3 修复批 / gopi shot / verify-loop 验收 / 派工自动化（M4.7 已落地待端到端双机实测）

## 十五、RPI5 第三计算位接入 + 首单交付 ✅
arm64 交叉编译 17MB → scp（binary scp 模式首战，节点无需源码仓）；网络发现=wg3 VPN 劫持默认路由阻断 GLM，IPv6 直连绕过（AAAA 走 eth0）——VPN 研究身份与计算位共存零冲突。
首单=架构文档独立 review：5 条结构化建议（位置引用+改进方向），已吸收进架构文档（commit 系列）。结论"无阻塞性问题"。
三节点全部有产出：peerbox M4.6+M4.7 实现 / HP fix-3major+self-review / RPi5 架构 review。
