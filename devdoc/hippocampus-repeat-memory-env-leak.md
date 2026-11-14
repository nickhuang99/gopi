<!-- doc-class: normative -->
# Task Report: 把「hippocampus 备忘每轮重复投递 → 收敛门」的排障教训写进记忆（补：测试环境泄漏根因）

Epic: 把这次排除故障的教训写进记忆：gopi 会话里 hippocampus 备忘每轮重复投递，
靠收敛门（相同记忆状态不再投递）修复。写完就结束。

本轮**只写记忆**（memory store/update），不改生产代码（Rule 3；reframe 的 constraints）。
上一轮已产出 `devdoc/hippocampus-repeat-memory.md`（记忆 #1611/#1612 的重写）；本轮的
新增价值是**复现验证 + 一个新根因**（测试隔离被 env 泄漏击穿），已入记忆 #1618 并回写 #1612。

## Completed

1. **复现并确认收敛门的既有修复仍有效**（不是口头转述，是跑出来的）：
   `cd ~/workspace/gopi-hippo-adopt && env -u GOPI_MEMORY_DB GOPI_HIPPOCAMPUS= go test ./internal/agent/ -run TestHippocampus_ -count=1` → **16/16 PASS**，其中关键三例
   `TestHippocampus_ConvergenceGateSuppressesRepeatedState`（round1 首报→Lv-1；round5 同 key→Lv-0；追加新 key→Lv-1）、
   `TestHippocampus_MinInjectIntervalSpacing`、`TestHippocampus_DefaultGateIsOffAtLv1`。

2. **新的排障发现（本轮真正的增量）：一次"看似无关"的假失败，根因是 env 泄漏，而非被测代码。**
   - 症状：`TestHippocampus_DefaultOffLeavesBehaviourUnchanged` FAIL，报
     `gate-on session differs from gate-off beyond the hippocampus artifacts`；
     `want`（gate off）里有 `[system] Hot-tier resident candidates …` 那条 user message，
     `got`（gate on）里没有；同时日志出现 `🧠 hot-resident digest injected (256 bytes)`。
   - 5-why：digest 非空 → `MemoryHotResident()` 查到 `tier='hot'` 行 →
     它读 `memoryDBPath()`，优先级 `GOPI_MEMORY_DB > GOPI_HIPPO_DB > GopiDir()/memory.db` →
     本 shell（先前 e2e 遗留）export 了
     `GOPI_MEMORY_DB=$worktree/tmp-e2e/home/.gopi/memory.db`（1560 行真实库副本，含热层行）→
     `newTestAgentEnv`（`internal/agent/loop_test.go:858`）是**白名单式**清理：只 Setenv 了
     HOME/GOPI_HOME/GOPI_SETTINGS/DEEPSEEK_API_KEY/GOPI_RAW_LOG/GOPI_THINKING*/GOPI_MAX_TOKENS，
     **没有清 `GOPI_MEMORY_DB` / `GOPI_HIPPO_DB` / `GOPI_HIPPOCAMPUS`**。
     另一个预置的坑：`env | grep -i hippocampus` 在本 shell 里是 `GOPI_HIPPOCAMPUS=1`——
     只清它会**掩盖**真正的泄漏（该用例仍然红），所以必须连 DB 路径一起清。
   - 判定/规避：`env -u GOPI_MEMORY_DB GOPI_HIPPOCAMPUS= go test …` → PASS（对照：只清
     `GOPI_HIPPOCAMPUS` 仍 FAIL）。
   - 判别方法（可复用）：**"测试在别的 shell 绿、在这里红" 时，先 `env | grep -i gopi` 列出
     全部 override，再与 helper 的 `t.Setenv` 清单做差集 —— 差集里的键就是泄漏源。**
     gopi 测试隔离的最小必备集合：`HOME + GOPI_HOME + GOPI_SETTINGS + GOPI_MEMORY_DB +
     GOPI_HIPPO_DB + GOPI_HIPPOCAMPUS`。

3. **记忆写入**（本 epic 的正式交付物，去重合并而非重复投递）：
   - `memory(action=store)` → **#1618**（新记录）：gopi 测试隔离被 env 泄漏击穿：
     hot-resident digest 假失败的根因 + 清 env 配方 + 最小隔离集。8 条中英混合检索面包屑
     （GOPI_MEMORY_DB / 假失败 / hot-resident digest / newTestAgentEnv 白名单 / `env -u` 清法 …）。
   - `memory(action=feedback, id=1612, rating=useful)`（tool_use id `call_00_CnWLniKtcyNNObfZyTvi6448`）：
     命中内容与本轮验证一致，直接复用。
   - `memory(action=update, id=1612)`：回写 answer —— 补全两道门的实现点
     （`converged/noteState/mayInject`、`hippoBriefKeys`、`hippocampusMinInjectIntervalRounds=3`、
     注入通道 `roundBoundaryInjections`、默认关逐字节不变）、**实测投递率 8/8=1.00 → 3/18=0.17**、
     正确的验证命令（带 `env -u GOPI_MEMORY_DB`）、三个坑（含新 env 泄漏坑，指向 #1618），
     并把 keywords 归一为 8 条中英混合面包屑。

## Not Done / 如实声明

- **未改生产代码**：`newTestAgentEnv` 的白名单清理缺口（应补 `GOPI_MEMORY_DB`/`GOPI_HIPPO_DB`
  置空或钉死临时 DB 路径）是**真实的测试基础设施缺陷**，但超出本 epic（只写记忆）范围，
  故以 bug 任务形式提交而非擅自改动。
- **#1613/#1614/#1615 三条同主题薄重复记录未逐一清理**：它们内容为 #1612 的严格子集
  （无实现位置/验证命令/根因）。仅将本轮命中的 #1615 降级 cold；重复 update 同义条目
  收益低且属另一个会话的产物，留待后续检索反馈自然降级。
- 未 merge / 未删 worktree（Rule 6e：merge 由 PA 验收后处理）。

## Verification

- Build: 未构建 —— 本任务零代码变更（无代码可构建，如实声明）。
- Tests（只读复现，证明回归面绿且定位假失败）：
  - `env -u GOPI_MEMORY_DB GOPI_HIPPOCAMPUS= go test ./internal/agent/ -run TestHippocampus_ -count=1`
    → `ok github.com/nick/gopi/internal/agent`，**16/16 PASS**。
  - 反证（未清 DB 路径）：`GOPI_HIPPOCAMPUS= go test … -run TestHippocampus_DefaultOffLeavesBehaviourUnchanged`
    → FAIL（`hippocampus_test.go:785` want/got 差异）——泄漏可复现。
- 记忆验证：`memory(action=search, query="hippocampus 备忘每轮重复投递 收敛门…")` → 命中
  #1611/#1612（含根因 + 实现位置 + 验证命令 + 三坑）+ #1618（env 泄漏）；#1612 已重写为完整版。
- Commit: 本报告单文件提交于分支 `feat/hippocampus-adoption`（worktree
  `~/workspace/gopi-hippo-adopt`），只 add 该文件（Rule 8，不 `git add -A`）。

## 交付声明（Rule 6d）

- **分支**：`feat/hippocampus-adoption`（worktree 共享主仓库 refs，提交即达主仓库该分支）。
- **恢复命令**：`git show feat/hippocampus-adoption:devdoc/hippocampus-repeat-memory-env-leak.md`
  ；记忆记录：`memory(action=search, query="gopi 测试隔离 env 泄漏 GOPI_MEMORY_DB hot-resident digest 假失败")`
  与 `memory(action=search, query="hippocampus 备忘 每轮重复 收敛门")`。
- worktree 未删除（未 merge，清理属 PA 验收后动作）。
