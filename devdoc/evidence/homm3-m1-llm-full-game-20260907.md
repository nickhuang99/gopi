<!-- doc-class: historical -->
# M1 真局实验 — LLM 自主打 HOMM3（GLM 夜间免费窗口）

日期 2026-09-07。HP 节点执行（GLM-5.3-flash，夜间免费 23:00-09:00），
任务 t-1788720718，8h 墙钟 cap 内 graceful finish（2880 步/2342 轮/
464 vision_analyze/2370 工具调用）。

## 背景
M0（t-1788646730，2026-09-06）验证 LLM 能自主"启动游戏→进单人场景→
选中英雄→游戏内操作"（vision 证据 /tmp/homm3-cur.png：英雄+对话框）。
**但"开始游戏"≠"会玩游戏"**——nick 指示 M1 验证能否打完含真实战斗的
完整对局（2026-09-06）。同基座 claude 研究先例：Sibyl 2024（LLM 对战
工具化 70% 胜率）、AI 游戏 agent 业界公认卡点="开始后不会玩"（战斗/
多回合策略）。

## 环境
- HP（GOG HoMM 3 Complete，wine 9.0，Xvfb :96，xdotool，gopi GLM）
- 场景：Wolves of Winter（内置 S 图），对战内置 AI
- vision：gopi vision_analyze（GLM）+ PIL 图像处理辅助
- 截图：~/workspace/homm3-m1/（本 devdoc evidence/ 存关键里程碑）

## 结果（progress.md 原始记录）

### 第一局（Black Finn/Tower）
| 里程碑 | 时间 | 状态 |
|---|---|---|
| 启动+主菜单+退出对话框处理 | 18:57Z | ✅ |
| GAME STARTED（Wolves of Winter, Black Finn, Day1）| 19:13Z | ✅ |
| 回合循环确认（End Turn→AI→Day2 事件对话框关闭）| 19:46Z | ✅ |
| 英雄移动+访问 Witch Hut（Diplomacy）+拾木材/金（31000→32000）| 19:50Z | ✅ |
| **Day3 输局**：AI 趁英雄外出占领城镇→失败画面 | 20:14Z | ⚠️ |

### 第二局（Yog，重开——8h 大部分时间）
- 深陷"英雄视觉定位"：PIL 图像处理/minimap 分析/红蓝 sprite 签名
  反复尝试定位英雄——vision 无法可靠从 1280×800 全图上识别小英雄标记
- 游戏推进到 Day3（Yog 被 AI 行动），未触发战斗

## 结论（回答 nick"会不会玩"）
1. **"会玩"骨架证实**：回合循环/资源收集/地图探索/访问建筑全走通——
   不是"能进游戏就卡死"。M0→M1 关键进展：Day1→Day3 多日推进+真实
   经济操作+End Turn 循环。
2. **战斗未触发**：第一局 AI 先占城（等不到战斗）；第二局卡英雄定位。
3. **真瓶颈=视觉定位非游戏逻辑**：464 次 vision 中大量是"找英雄在哪"。
   英雄 sprite 在全图太小/地图色噪声干扰 → vision 不可靠。

## 技术教训（LLM 玩 GUI 游戏路线）
- vision 定位大场景小目标 = 核心难点（英雄 vs 全图）。比规则理解难。
- 方向：①minimap 坐标像素级读取（模板匹配非 vision）②局部放大再
  vision ③程序化 sprite 签名（红蓝匹配）辅助定位。
- 同 M0 死因（400 消息配对）+ M1 新瓶颈（视觉定位）都非"游戏逻辑
  不会玩"——harness/视觉管线是主战场。

## 资产
- maps repo devdoc/evidence/homm3-m1/：progress.md（里程碑原文）+
  g001-after-newgame.png（真实游戏画面 917KB）+ b2.png
- HP ~/workspace/homm3-m1/ 完整截图链（boot*/g*/e*/align-check 等）

## 下一步候选（M2）
- 英雄定位像素级辅助（模板匹配/minimap 读取）
- 真战斗触发（选更简单图/玩家先手/低难度 AI）
- 长会话稳定性（本 run 8h cap 无 400 崩溃——较 M0 进步）
