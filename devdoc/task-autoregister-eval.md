<!-- doc-class: normative -->
# Tasks 自动登记：三模型实现评估与选型（DS / GLM / hy4）

日期：2026-08-28 ｜ 裁判：Go 评审 agent（本会话） ｜ 基线：master @ bcef08a

三份实现（同一需求，三个模型独立完成，各自全量测试 9/9 PASS，E2E 均已人工验证过）：

| 分支 | 模型 | 分支头 | diff 规模 |
|---|---|---|---|
| `feat/task-autoregister-ds` | deepseek-v4-flash | 956abdf | 10 文件 +666/-4 |
| `feat/task-autoregister-glm` | glm-5.3-flash | 8cdd2dd | 7 文件 +697/-3 |
| `feat/task-autoregister-hy4` | hy4-preview | eddb8b9 | 8 文件 +1002/-2 |

## 1. 五维评分表

评分 1–5，加权：功能 30% / 代码质量 25% / 测试质量 25% / 约束符合 10% / 独有亮点 10%。

| 维度 | DS | GLM | hy4 |
|---|---|---|---|
| 功能完整性 | **4** | **5** | **4** |
| 代码质量 | **4.5** | **4.5** | **3.5** |
| 测试质量 | **3.5** | **4.5** | **5** |
| 约束符合（无 delete / Rule 3 / best-effort） | **5** | **5** | **5** |
| 独有亮点 / 隐藏缺陷 | **3.5** | **4.5** | **3.5** |
| **加权总分** | **4.05** | **4.70** | **4.18** |

### 一句话点评

- **DS（4.05）**：最精简、注释最老练，但 `TaskSearch` 无 `ORDER BY`——宣称 BM25 排名实际是表扫描序（devdoc 自认），且超长 briefing 不截断全量入库、2 字 CJK 不可搜。
- **GLM（4.70）**：功能最全（`[auto]` 前缀、三 tier 召回含 2 字 CJK LIKE 兜底、真 BM25 排序 + cap、多值开关、latch 防双写），无一处硬伤，工程严谨度最高。
- **hy4（4.18）**：测试最厚（20 个，行为全覆盖），`TaskStatus*` 导出别名最符合 Rule 2；但 resume 无检查会注册续聊行（devdoc 声称"续聊不另开行"与实现不符——跨进程 resume 是新 UUID，实际会开新行）、147 行文件一半是注释、`taskRegisteredAt()` 死代码、输出带负值 rank 噪音。

## 2. 关键差异与实测验证

| 差异点 | DS | GLM | hy4 | 判定 |
|---|---|---|---|---|
| 自动行标记 | 无 | `[auto] ` 前缀（可 `search auto` 区分） | 无 | GLM 胜 |
| 开关取值 | 仅 `0` | `0/off/no/false/disabled` | `0/false/no/off` + 数值 0 | 等同 |
| summary 截断 | 无（全文入库） | 200 runes 折叠+截断 | 500 runes 截断 | GLM/hy4 胜 |
| 空 briefing | TaskNew 报错→stderr | 显式跳过 | 注册 `(session xxx)` 占位 | GLM 最干净 |
| resume 处理 | 跳过（else 分支） | 跳过（`if !resume`） | **无条件注册续聊行** | GLM/DS 胜 |
| end hook 防双写 | 无 latch（start 失败→end 刷错） | latch（测试覆盖 double no-op） | 靠记录存在性（幂等） | GLM/hy4 胜 |
| 搜索排序 | **无 ORDER BY** | `bm25()` + created_at | `f.rank ASC` | GLM/hy4 胜 |
| 2 字 CJK（"自动"） | 不可搜 | LIKE tier 兜底 **命中** | 不可搜（明确不做） | GLM 胜 |
| 查询转义 | escapeQuery + CJK 分段 | ftsQuote 全 phrase | escapeQuery | 三者均防注入 |
| 输出 cap | 无 | 20 + `(capped at 20)` | 20（limit 参数） | GLM 输出最友好 |
| `_ad` FTS 触发器 | 无（死代码论） | 有（防御性，有理由） | 无 | 可辩，无碍 |
| 状态常量 | agent 侧复制字符串 | agent 侧复制字符串 | tools 导出别名（Rule 2） | hy4 胜 |
| 测试数量/质量 | 5 个，核心覆盖 | 10 个，含注入/reopen/双写 | 20 个，行为最全 | hy4 胜 |

实测（GLM 分支真机）：`task search "自动"`（2 字 CJK）命中、连字符 UUID 查询命中（ftsQuote 字面化）、注入探针 `") OR 1=1 --` 干净 miss——三 tier 与防注入均为真实行为而非纸面设计。

## 3. 选型：GLM 为 merge 基准

加权总分 4.70 居首，且无隐藏硬伤。决定性理由：

1. **功能完整性唯一满分**：真 BM25 排序、三 tier 召回（含 2 字 CJK）、结果 cap、`[auto]` 标记——四功能全部"正确实现"而非"实现了个影子"（对照 DS 无排序、hy4 resume 偏差）。
2. **工程严谨度**：latch 防双写（单行 per session 的铁保证）、常量集中 + 单点读开关（Rule 2）、日志走 agent Logger 统一 `[gopi]` 前缀。
3. **测试质量高**：注入安全、reopen 幂等、double end-hook no-op、no-delete grep 回归——测的是行为契约。
4. **行数适中**：697 行实现同等功能，远轻于 hy4 的 1002 行（其中测试占大头且存在死代码）。
5. **resume 语义正确**：与"一次任务一行、summary=briefing"规格一致。

## 4. 从落选分支吸收的亮点（已并入 master）

| # | 吸收项 | 来源 | 落点 |
|---|---|---|---|
| 1 | `TaskStatus{InProgress,Completed,Failed,Cancelled}` 导出别名——字符串镜像 SQL CHECK 约束保持单一来源（Rule 2），agent 侧 hook 引用 `tools.TaskStatus{Completed,Failed}` 而非复制字面量 | hy4 | `internal/tools/tasks.go` + `internal/agent/taskregister.go` |
| 2 | no-delete 测试强化：API 面守卫（`func TaskDelete/TaskRemove/TaskClear/TaskPurge/TaskDrop` 不得出现），互补 agent 侧的 DELETE 语句 grep | hy4 | `internal/tools/tasks_fts_test.go` `TestTaskNoDeleteFuncRegression` |

未吸收（有意为之）：hy4 的 `TaskShowRaw` 读-改-写 end hook（GLM 内存 latch 更简单且少一次 DB 往返）、`limit` 函数参数（CLI 无需）、rank 显示（负值噪音）、空 briefing 占位行（GLM 跳过更干净）、`_ad` 触发器取舍保留 GLM 原判（防御性声明零成本）。

## 5. Merge 与验证结果

- **merge commit**：`4b53f25`（`--no-ff`，GLM 分支）；吸收提交 `5c26ecb`。
- **全量测试**（master 合并后）：`go test ./...` → **9/9 包 PASS**：

```
ok  github.com/nick/gopi/cmd/agent
ok  github.com/nick/gopi/internal/agent
ok  github.com/nick/gopi/internal/checkpoint
ok  github.com/nick/gopi/internal/config
ok  github.com/nick/gopi/internal/deepseek
ok  github.com/nick/gopi/internal/gopilog
ok  github.com/nick/gopi/internal/proxy
ok  github.com/nick/gopi/internal/text2img
ok  github.com/nick/gopi/internal/tools
```

- **E2E（隔离 HOME + 临时 DB，真库零污染）**：
  - 真实任务 `reply with the single word: hello` → 自动登记 `[auto] reply with the single word: hello — elapsed=10.1s iter=6 tokens=63383 [completed]`（session_id 作 task_id）✓
  - `task search hello` / 全 UUID（连字符）/ `自动登记`（trigram）/ `自动`（LIKE tier）全部命中 ✓
  - `GOPI_AUTO_TASK_REGISTER=0` 再跑任务 → 台账无新行 ✓；`off/false/disabled` 变体均生效 ✓
  - 真库 `~/.gopi/memory.db` 保持 2 行 cancelled 演示数据原样 ✓

## 6. 清理结果

- 删除分支：`feat/task-autoregister-ds`（was 956abdf）、`feat/task-autoregister-hy4`（was eddb8b9）
- 删除 worktree：`/home/user/workspace/task-autoregister-ds`、`/home/user/workspace/task-autoregister-hy4`
- 保留：`feat/task-autoregister-glm` 分支 + worktree（被 merge 分支，确认后清理）

## 7. 硬约束复核

- ✅ 无 delete 路径：合并后 `DELETE FROM tasks` 不存在；双测试守卫（语句 grep + 函数名 API 面）
- ✅ 不新增 LLM 工具（Rule 3）：纯 harness 行为 + CLI verb
- ✅ best-effort：两个 hook 均只 log 不阻塞（devdoc 实现报告已验证）
- ✅ 未触碰其他未跟踪文件（bugs/、devdoc/ 未跟踪、memory.db 等原样）

## 8. 遗留说明

- 被中断（kill -9 / 断电）的运行会留下 `in_progress` 悬挂行——三份实现一致接受（与 session_end JSONL 的既有写入语义对齐），CLI `task update` 可手工补终态。
