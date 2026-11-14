<!-- doc-class: normative -->
# Procedural Memory (工作流记忆) — M5 实现文档

**分支**: `feat/memory-procedural`（基 master 5c6f5ca）· **域**: `internal/tools/memory_procedural.go`, `memory_sync_procedural.go`, `memory_brew_{llm,run}.go`, `internal/agent/loop.go`, `cmd/agent/cli_{memory,memsync}.go`

## 1. 结论先行

gopi 五层记忆补齐 Procedural 层：**新表 `procedural`（name UNIQUE）+ 双 FTS5 索引 + brew 工作流分流 + memory_store type=procedural + memory_search 引擎级并入 + memsync procedural 通道**。epic 四验收全部有自动化测试：①brew 含工作流 session → procedural 落库（`TestBrewWorkflowRoutingE2E`）②memory_search 命中（同测试 ③ 段）③同名/相似去重（`TestProceduralDedup`）④memsync 往返幂等 + node provenance（`TestProceduralSyncRoundtrip`）。tools 包预存失败 `TestEnsureTagsRegeneratesForNewerTS`（symbol index 相关，master 已存在，与本分支无关——stash 后仍 FAIL）。

## 2. Schema（Bond 草案 → 实测修正）

草案 `procedural(id, name UNIQUE, trigger, steps, preconditions, tools_used, success_rate, use_count, version, tier, confidence, sources, created_at, updated_at)` 实现时修正三处：

| 草案 | 实测修正 | 根因 |
|---|---|---|
| `trigger TEXT` | 保留，但 `trigger` 是 SQLite 关键字上下文敏感——所有 JOIN 查询列必须 `p.` 限定 | `SELECT id, name, trigger ... FROM procedural_fts JOIN procedural` 报 *ambiguous column name: name*（FTS 虚拟表列与基础表列同名），触发器 DDL 内 `new.trigger` 同理 |
| steps 自由 TEXT | `steps` 存 JSON 数组 `[{"action","detail"}]`，`proceduralStepMax=8192` 封顶 | 结构化步骤是 epic 明文要求；freeform 兜底=单步 `{action:"do", detail:answer}` |
| jieba_token 列 | **不加** | distillated_qa 的 jieba_token 是 2-char gram 近似列，procedural 的 trigram 索引已覆盖 CJK 子串召回，第二列纯冗余 |

幂等建表 `ensureProceduralSchema`（照抄 ensureSchema 模式）：基础表 + `procedural_fts`（unicode61, external-content, 列 name/trigger/steps）+ `procedural_tri`（trigram, 列 name/trigger）+ 每索引 ai/au/ad 三触发器（external-content 'delete' 模式）。**search 连接只读（query_only）不建表**：ProceduralSearch 探测表不存在即返回空提示（MemorySearch 同语义）。

## 3. 去重：name UNIQUE + trigram（阈值实测修正）

- **exact**: `WHERE name=?` → 命中即 merge（version+1, updated_at 触碰, sources 并集），UNIQUE 约束只是兜底。
- **相似**: `proceduralFindSimilar` = trigram FTS 预筛 → name-only Jaccard 确认。**阈值 0.3 而非 QA 通道的 0.5**：实测 `jaccard("重启网络服务","网络服务重启")=0.333`——工作流名字短且常语序重排，0.5 漏合并；真不同流程（"清理构建缓存"）gram 几乎零共享，远低于 0.3。**Jaccard 只比 name，不比 name+trigger**：两流程共享 trigger（"需要重启时"）但做不同事（nginx vs postgres）必须共存——首版比 `name+" "+trigger` 导致同 trigger 异名流程全被吞（TestProceduralDedup 抓住）。
- **probe 必须 gram 拆分**：整段 CJK run 当一个 phrase 查 trigram 索引只命中**逐字相等**的存储值（"网络服务重启" 查不到 "重启网络服务"，实测 Debug21）——必须拆成重叠 3-char gram OR 查询。注意：**MemorySearch 的 trigram 路由存在同样缺陷（整 run phrase 查询），本分支不动既有 QA 语义，仅记录**。
- **merge 不覆写 steps**：存储侧流程保持权威（brew 重跑幂等），与 MemoryStore merge-not-overwrite 同哲学。

## 4. 写入路径

### brew 蒸馏分流（①）
judge prompt v2 加 `kind` 字段（workflow/lesson，缺省 lesson——旧 judge 输出零破坏）。`kind=workflow` → `distillWorkflow`（新 prompt：name/trigger/preconditions/steps/tools_used/keywords）→ `storeBrewWorkflow` → `ProceduralStore(w, BrewSourceTag)`，落库 `sources="procedural,brew"`（双通道 export 闸门都可见）。stats 复用 Stored/Deduped。

### memory_store type=procedural
`internal/agent/loop.go` 执行器：`type=procedural` 分流读 name/trigger/steps/tools_used/preconditions；优雅降级 topic→name、question→trigger、answer→单步（惰性调用也落可检索流程而非空行）；validate 仅在非 procedural 时强制三件套。tool def 新增 type/name/trigger/preconditions/steps/tools_used 六属性。

## 5. 检索并入（②）

**引擎级**：`MemorySearch` 尾部并入 `ProceduralSearch`（QA 空提示 + procedural 命中 = 非"空结果"，`[procedural workflows]` 段；有 QA 命中则尾接）——放引擎而非 loop.go/CLI，一个合并点服务所有调用方。ProceduralSearch 三路由同 QA 模式：FTS5 exact-token（name/trigger/steps）→ trigram CJK → LIKE 2-char。排序 = token coverage×2.0 + conf + tier 权重 + ≤0.5 recency 微推（updated_at 30 天内）。渲染 meta 行 `#id name vN tier conf tools= sources=`，步骤渲染 `N. action: detail`。**根因陷阱（已修）**：检索结果行内再查 use_count（单连接 MaxOpenConns(1)）→ 嵌套查询死锁——删除行内查询。

## 6. memsync procedural 通道（④）

`memory_sync_procedural.go`，与 QA 通道同构：
- **patch**: `{version:1, node, rows:[{name,trigger,steps,preconditions,tools_used,version,updated_unix}]}`，独立版本计数 `syncProceduralPatchVersion=1`（与 QA patch 版本无关演进）。
- **export 闸门**: `sources LIKE %,procedural,% AND NOT LIKE %,node:%`（无 ping-pong）；**sources 不进 patch**，import 侧强制 `node:<exporter>`（provenance import 时定，不信任文件——同 QA 规则）。
- **import**: 单 BEGIN IMMEDIATE 事务 all-or-nothing；name exact → merge（**version/updated_at 取 max(local,patch) 再 +1**——两节点并发演进互不回退，`TestProceduralSyncVersionMax`）；相似 → merge；否则 INSERT。steps 永不覆写。空 name/trigger/steps 记 RowsRefused 不炸 patch。
- **CLI**: `gopi memsync --export-proc --out p.json` / `--import-proc p.json`（usage 已更新）。

## 7. 实测记录（epic 验收 ↔ 测试）

| 验收 | 测试 | 证据 |
|---|---|---|
| ① brew 工作流 session → procedural 落库 | TestBrewWorkflowRoutingE2E | stub judge kind=workflow → procedural 表 1 行 sources=procedural,brew；distilled_qa 1 行（分流不串） |
| ② memory_search 检索命中 | 同上 | QA 空时 `[procedural workflows]` 段含 `测试失败修复流程` |
| ③ 同名/相似去重 | TestProceduralDedup | 同名 → version bump 不插行；语序重排 → 相似合并；COUNT=1；异名插入 COUNT=2 |
| ④ memsync 往返 | TestProceduralSyncRoundtrip + VersionMax | export→fresh 库 import=1 行带 node: provenance → re-import no-op(dedup=1) → 再 export scanned=0（无 ping-pong） |

单测 8 个（Store/Search 三路由/Dedup×2/SourceTag/SyncRoundtrip/SyncVersionMax/Empty）+ E2E 1 个全 PASS。**编译器盲区教训**：`trigger` 列名合法但 JOIN 未限定报 ambiguous（SQL 层非编译层）；`use_count` 行内嵌套查询=单连接死锁（测试挂 60s 而非 panic 行号指示处——goroutine dump 里才见 `useCountColumn`）。

## 8. 要点清单

1. `trigger`/`name` 列名在 FTS JOIN 中必须 `p.` 限定——ambiguous 报错来得毫无提示。
2. 只读连接（query_only）上 ensure*Schema 必失败：search 路径探测表缺失→空结果，绝不建表。
3. trigram probe 必须 3-char gram 拆分；整 run phrase 只匹配逐字相等（QA 通道同缺陷待修）。
4. 短名重排相似度：阈值 0.3（0.5 漏合并）+ name-only Jaccard（+trigger 吞异名流程）。
5. 单连接（MaxOpenConns(1)）引擎内禁止行内嵌套查询——必死锁。
6. brew judge 新字段必须 `omitempty` + 缺省语义（旧 LLM 输出零破坏路由）。
7. stub 测试回放按调用序：judge+distill 同 chunk 成对，多 lesson 一 judge 多 distill。
8. memsync provenance：export 不带 sources、import 强制 node:<exporter>、version 取 max——三件套缺一不可。
