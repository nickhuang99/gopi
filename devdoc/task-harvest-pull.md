<!-- doc-class: historical -->
# Task Report: task harvest --peer — HP→peerbox PULL sync (M4.12)

## Completed
- `gopi task harvest --peer ALIAS [--since UNIX] [--remote-db PATH] [--db PATH]` (cli_tasks.go + tools.TaskHarvest in memory_sync_task.go, the epic's file domain).
- ① SSH 到 peer（exec 直跑 ssh，无中间 shell；BatchMode+ConnectTimeout）远端 `sqlite3 -json` 执行 timestamp-after SELECT（`updated_at > 锚点`，锚点=sync_state 表 peer 行，无则 0=全量）②JSON 传输 ③本地单事务应用 ④事务内提交锚点 ⑤六计数（harvested/migrated/duped/conflict/skipped/failed）。
- 应用语义：新 id→INSERT（from-<peer> 强制 tag、terminal 行补 ended_at）；同 id 同戳→状态阶梯（terminal>live>pending——两侧同戳时"已完成结果"收编该行，这是同一会话双节点落种的收敛关键）；同戳同阶异文→conflict 不写；remote 较新→migrated；remote 较旧→skipped；**终态不回退**在 decide 与 UPDATE WHERE 双层强制；live 本地执行（claimed/in_progress）永不被 live 改写。
- 锚点回退：failed 行使锚点回退到最早失败戳 −1s，下次重拉，不丢行。
- 测试注入面：`HarvestOptions.Fetch` 替换 ssh 传输（14 个单测不碰网络）。

## E2E（真 HP，沙盒库副本）
- 全量首轮：154 行 → harvested=46 migrated=3 duped=11 conflict=73 skipped=21 failed=0（六计数和=154）。
- 幂等重跑：0 行传输，六计数全 0。
- 行数守恒：191→237（+46）；55 行带 from-hp。
- conflict 根因核查：86 个同 id 同戳对中 85 个同状态（=已收敛 duped），唯一 conflict = cancelled(HP) vs pending(peerbox)，本地行未降级——符合"终态不回退+异阶裁决留人工"设计。

## Not Done
- PUSH/双向协议（M4.13，本单范围外）。
- conflict 的人工解决工具（当前仅计数+报行，等 nick 裁决）。
- Rule 11 LLM 调用验证：N/A——本单是 CLI 运维动词（用户执行），未改 agent 工具面/prompt 路由。

## Verification
- Build: `go build ./...` OK；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- Tests: `go test ./internal/tools/ -run TestHarvest -count=1` → 14/14 PASS（新增 12 + 既有 harvest 面回归）
- E2E: 上述三组实测
- Commit: 717675a feat(harvest) + 695880e fix(同戳阶梯+对端 shell 引号)

## 交付位置（Rule 6d）
- 分支：`feat/harvest-hp-pull`（worktree ~/workspace/gopi-harvest-hp，共享对象库，主仓立即可见）
- 恢复：`git -C ~/work/GoPi show feat/harvest-hp-pull:devdoc/task-harvest-pull.md`
- 未 merge（Rule 6e：不自行 merge，留裁决）
