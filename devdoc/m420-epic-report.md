<!-- doc-class: historical -->
# Task Report: M4.20-E — Epic 级集成验收（worker loop 全链）

Branch: `feat/worker-loop-m420` @ A→B→C→D→E commits（见 Verification）

## Completed
- **Build**：`CGO_ENABLED=0 go build ./...` 全绿（A–D 全部改动面）。
- **E2E worker loop**（真二进制 sandbox）：submit → `gopi worker` claim → run →
  must-succeed push → `[worker-sync] peer=hp pushed=2 pulled=1 migrated=0` →
  `queue drained — exiting` exit 0；`worker:peer1` tag 落行。
- **双侧 rank 收敛 sim**（`scripts/e2e-m420-rank.sh`，双真节点同机 file-channel）：
  seed → A 完成（rank3）→ B 镜像 in_progress（旧 anchor）→ A 导出 delta →
  **B 收敛到 completed**（rank 前进）→ 回程 A echo 幂等 no-op。6/6 断言全过。
- **发现的 epic 级缺陷并修复**（v1 file-channel 三处，v2 delta 之外的第二裁决面）：
  1. `SyncTaskExport` 不导出 `updated_at/ended_at` → 接收端无从 rank 裁决。
     修复：SELECT 补两列（`COALESCE(ended_at,0)`），行携带 wire anchor。
  2. 反回流门吞掉终态判决：A 完成的行在 B 上带 from-A tag，A 再拉时被
     `NOT LIKE from-<self>` 排除 → 判决永远回不了起源节点。修复：**终态行豁免
     反流门**（verdict=history 不是 channel spam；import 侧幂等吸收）。
  3. `importTaskExisting` 对"terminal patch × live local"拒绝（旧 live-execution
     guard）→ 双侧永不收敛。修复：v1 通道并入 **rank ladder**（rank3 > rank≤2
     = 前进收敛），live guard 由 M4.18 起点原子性取代。
  4. `existing==nil && terminal` 被拒 → 未见过该行的节点永远学不到任务已完成。
     修复：`importTaskMirror` 终态镜像插入（never pending → 不会重跑）。

## Verification
- Build: `CGO_ENABLED=0 go build ./...` OK
- Tests: cmd/agent TestWorker* ok（8.5s）；internal/tools 导入/导出/delta/bidir
  套件 ok 188.0s（EXIT=0）；rank matrix 14 行 + bilateral/guard 单测全绿（M4.20-B）；
  verify/ingest/list 套件 ok 131.8s（M4.20-C）。
- E2E: `scripts/e2e-m420-rank.sh` 6/6 PASS；`/tmp/worker_e2e.sh` 全链 PASS。
- Epic commits（本分支依次）: `e02bb98` A worker loop · `27f7eec` docs ·
  `5eaebd3` B rank model · `b8e94d7` C reissue · `0b5d252` D observability ·
  E = 本次 export/ladder/mirror 修复 + 本报告。

## Not Done
- hp 节点跑旧版 gopi（`task sync` 无 `--claim-remote`）：跨节点实测仍 fail-soft；
  等 hp 升级后重跑 2-worker 跨机 E2E。
- internal/tools 全量套件 box-load 10m panic（memory #1183 既有 flake，非本 epic
  引入）；选层套件已覆盖全部改动面。
