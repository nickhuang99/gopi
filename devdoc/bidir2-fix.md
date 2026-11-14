<!-- doc-class: historical -->
# Task Report: TestBidir2\* 修复（红名单第 6 轮收尾）

## Completed
- **根因判定**（devdoc/tools-redlist-round6.md，11 个探针测试）：
  - 主因：anti-reflow 导出门（56fd84a）× 同主机模拟——模拟节点名 peerbox/hp 与宿主
    hostname 相同，syncSelfTag() 与导入行的 from-tag 撞名，导出在 SQL 层静默滤光
    （Scanned=0 → pushTo 全零统计）。
  - 次因：236fb03 使 config role 优先于 83ed846 的 peers-only pin，server/client
    判定失去确定性。
  - 定性：**测试装置未适配生产新 gate（test-asserts-old-semantics），实现无 bug**。
- **修复（test-only，commit 3f82854）**：
  - `newBidirNode`/`pushTo`/`tickSync` 统一 `t.Setenv("GOPI_SYNC_NODE", n.name)`
    ——节点身份跟随模拟名（生产文档语义，memory_sync_delta.go:134）。
  - `pushTo` 显式 pin `GOPI_NODE_ROLE`（dst=peerbox→server，否则 client）+
    `config.ResetNodeIdentityForTest()`（TestSyncDeltaConflictEqualAnchor 先例）。
  - `TestBidir2DivergentTerminalsConverge`：explicit-role fixture 下 s1 的 server
    裁决推进 peerbox 锚点，s2 走 strictly-newer 路径以 `RowsAlready` 显式回报；
    原断言里依赖 hostname 的 equal-anchor 'conflict' 标签检查改为
    「必须显式回报（conflict/resolved/already=1）」——语义收紧为机器无关。
- **TestMemorySearchNonBlocking**：环境性 SQLite 并发 flake（全量下偶发），
  单跑 3 次全绿（3.69s / 1.84s / 单包内绿），非回归，无需修改。

## Not Done
- 无（全量 tools 套件已于 2026-09-01 05:20 复核绿：ok 545.963s，-count=1 串行；TestMemorySearch
  NonBlocking 全量下未再触发——此前 3s 失败均为包级并发 SQLITE_BUSY 环境性 flake）。

## Verification
- Build: `go build ./...` OK；`go vet ./internal/tools` clean；gofmt clean
- 红名单 3 测试：**绿 × 2 次连续**（job-3 28.6s、job-4 38.2s，各含 4 个 TestBidir2*）
- 回归：TestBidirEpic1-4（实测①-④）+ Reflow + TestSyncRemote 套件 ok（18.5s / 32.5s）
- **全量 `./internal/tools`：ok 545.963s（-count=1，串行，job-5）——第 6 轮红名单清零**
- Commit: `3f82854 fix(test): bidir e2e node identity + explicit role pins ...`
  （交付在 master；recovery: `git show 3f82854`）
