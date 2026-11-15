<!-- doc-class: normative -->
# Task Report: tools 全量红名单根因判定（第 6 轮，master 7532a3d）

红名单：/tmp/tools-fails3.txt — TestBidir2ImportSymmetry / TestBidir2LiveConflictNeverClobbered /
TestBidir2DivergentTerminalsConverge / TestMemorySearchNonBlocking。

## Completed

### ① TestMemorySearchNonBlocking — 环境性 flake（非回归），单跑绿
- 单跑实测（master@2405299，`go test -run ...TestMemorySearchNonBlocking -count=1`）：**PASS 3.69s**。
- 3s 失败形态与全量并发一致：SQLite 迁移并发竞争（SQLITE_BUSY / duplicate column），与
  memory #882、#1014（两次独立任务均记录 pre-existing flaky，重跑即过）相互印证。
- 本轮全量 5 个包并行跑相互锁库（job-3~7 出现 5 个 `<-timeout` SIGKILL 相互踩踏），
  全部判无效并杀掉；有效数据以单跑/单包为准。

### ② TestBidir2\* — 三个候选根因中两个被证伪，主根因被实锤（全零统计形态）
**实测复现（关键证据，job-1）**：三个测试失败形态完全一致——每条 pushTo 腿返回
**全零 SyncTaskImportStats**（imported=0 already=0 migrated=0 mirrored=0 conflict=0 resolved=0
refused=0 Errors=[]），且数据并未到达对端。例：
```
TestBidir2ImportSymmetry: imported=2 already=0, want 2/2（第二轮 pushTo 全零）
```

**证伪过程（不猜，逐个实测）**：

| 候选根因 | 实验探针 | 结论 |
|---|---|---|
| A. IsServerNode 语义变化（236fb03：config role 优先 + fail-safe client） | 探针 TestRoleCheckAmbient：包内默认 role="server"（来自宿主 ~/.gopi/settings.json），IsServerNode=true；手动 GOPI_NODE_ROLE=client + ResetNodeIdentityForTest 后 false，role pin 机制工作正常 | **非主根因**：role pin 显式生效时合并仍全零 |
| B. 导出锚窗口漂移（AfterSource/时间） | 探针 TestExportWindowCheck：AfterSource=0 与 =now 均能导出刚提交的行 | **证伪** |
| C. 83ed846 pushTo 方向 pin 失效 | 探针 TestIsClientAfterHostnamePin / TestIsServerForeignPeerPin：pin 均按设计翻转/生效；但 236fb03 之后 config role 压过 pin（测试没 pin role），server/client 侧判定确实错位 | **次级因素**（影响计数器归类，不产生全零） |
| **D. anti-reflow 导出门（56fd84a）× 同主机模拟** | 探针 TestExportDivergeTrace：b 节点 DB 里确有 t-div 行（tags=from-peer1），但 `SyncTaskExportDelta` 返回 rows=0，Scanned=0 —— 行在 SQL 阶段被过滤；探针 TestGateHypothesis：把 GOPI_SYNC_NODE 改成不匹配的 alias 后同样的 2 行立刻导出成功 | **实锤主根因**：模拟节点名叫 peer1、宿主机名也是 peer1 → syncSelfTag()=from-peer1 与行上 from-peer1 标签互撞，导出门把“本节点自己导入过的行”全部静默滤掉 → 下游合并零输入 → 全零统计 |

**根因链（5-why）**：测试失败 → 合并收到 0 行 → 导出返回 0 行 → 导出门
`NOT LIKE '%from-<self>%'` → 模拟节点名与宿主机名相同 → bidir e2e 的同机双节点模拟
依赖 hostname 作节点身份（688b7aa 写测试时即如此），56fd84a（8-31 13:42，是 688b7aa
8-31 15:29 的祖先）之后任何 from-tag 行都带 self 撞名风险；236fb03/1833a4c（9-1）又把
role 判定改成 config 优先，测试未同步 pin role。
**定性：测试装置旧语义（fixture 未适配生产新增的两个 gate），非实现 bug。**
跨节点真实部署不受影响（真实节点 hostname≠对端别名，from-tag 不等于 self-tag）。

修复方向（最小 patch，留待修复单）：fixture 与宿主解耦——newBidirNode/pushTo/tickSync
统一 `t.Setenv("GOPI_SYNC_NODE", n.name)`（节点身份跟随模拟名，与 memory_sync_delta.go:134
的既有语义一致），导入侧补 `GOPI_NODE_ROLE` 显式 pin + `config.ResetNodeIdentityForTest()`
（TestSyncDeltaConflictEqualAnchor 先例 78fe5fa 同款）。断言语义（镜像计数器、live 保护、
completed-wins）全部保留。

### ③ 时间线证据（merge 相关性）
- 688b7aa（8-31 15:29）写测试时 56fd84a（8-31 13:42）已在库内——当时测试绿是因为 fixture
  行不带 from-tag（首轮导出的是本地 submit 行）；第二轮起依赖 re-import 行再导出，被新门滤掉。
- 9-1 03:39–04:06 的 merge 批（83ed846/236fb03/1833a4c）与红的时间相关性 = 最后压垮项；
  devdoc/claim-broadcast.md 的“post-merge tree inherits the pin — no action needed”结论与
  master@7532a3d 实测仍红矛盾（该 pin 只设 peers，未解 from-tag 撞名与 role 优先）。

## Not Done
- TestBidir2\* 的修复 patch 本身：本会话以逐候选根因证伪为主（11 个探针测试，均即建即删，
  未污染树），修复作为 bug 任务 t-1788210015 入队（tags=bug，payload 带全部证据+约束+验收标准）。
- 全量 tools 套件绿：等修复单落地 + 单包串行跑（并行跑在本机必然互锁）。

## Verification
- Build: `go build ./...` OK（多次隐式验证，探针编译均过）
- Tests: 4 红单跑 → TestMemorySearchNonBlocking PASS(3.69s)；TestBidir2ImportSymmetry FAIL
  形态=全零统计（复现 3x，探针定位到导出门 SQL 过滤）
- 提交: 本文件（devdoc）；修复任务 t-1788210015（bug 流水 55 号）
