<!-- doc-class: historical -->
# Task Report: memsync 双向同步 epic 收尾 — partition sessions + content-conflict gate (M4.13 补全)

> **Corrected 2026-11-05 (classification + content).** A 2026-09 task report is the
> record of ONE run, so it is `historical`, not `normative`. Two mechanisms it names
> are DELETED: `gopi task sync --peer` (the whole cross-node sync surface went
> 2026-11-05) and `GOPI_SYNC_INTERVAL`, which the catalog and `gopi --help-env`
> advertised while **no production path read it**. The audit below stands as the
> measurement of the tree on that date.

分支 `feat/memsync-bidirectional`（基 master e820c2e，worktree ~/workspace/gopi-memsync-bidir）。

## 背景：epic 到手时的真实状态（审计结论）

Epic 假设 M4.8 单向版"正在实现中"，实际仓库已远超该假设：
- M4.8 单向 task sync 已落地（memsync --export-tasks/--import-tasks，feat/memsync-tasks）
- M4.12 harvest --peer PULL 已落地（六计数、终态不回退、锚点回退）
- M4.13 `gopi task sync --peer` 双向 delta session **已在 master**（e820c2e）：push+pull 双向、
  newer-updated_at-wins、终态不回退、live 本地执行保护、equal-anchor 冲突 tag、
  anchor 只进不退、防回流 echo 识别、serve tick 自动同步（GOPI_SYNC_INTERVAL）。

**真 gap（本单补全的）**：①epic 四条实测未在 E2E 层验证（尤其内容冲突与全链幂等往返）；
②`--tags` 模块分区选段导出缺失（epic ③）；③equal-anchor **内容**冲突（同 id 不同
payload）会被静默吸收成 tags-union —— 违反"冲突不静默"；④devdoc/memsync-bidirectional.md 缺失。

## Completed

- **SyncTaskDeltaOptions.Tags**（internal/tools/memory_sync_delta.go）：分区导出。
  M4.8 的 comma-bounded LIKE 门（`','||tags||','` LIKE `%,tag,%`）verbatim 复用——精确 tag，
  不匹配子串（module:x 不会命中 module:xx）。与锚点窗口 AND 组合。
- **SyncTaskPatch.TagsFilter**（memory_sync_task.go）：分区随 v2 patch 走线。serve-remote 读它
  并对称过滤自己的 push —— `--tags` session 双向对称，ssh 腿无需新协议状态。import 忽略它。
- **`gopi task sync --tags TAG`**（cmd/agent/cli_tasks.go + cli_tasks_sync.go）：CLI 旗标接通；
  --serve-remote / --status 带 --tags 直接拒绝（peer 侧/查询侧无导出可过滤）。
  `SyncPeerSessionTags` 导出面（serve tick 自动同步恒传 ""——整窗，防分区静默搁浅）。
- **内容冲突门**（memory_sync_delta.go syncDeltaMergeRow）：equal-anchor 同 status 但
  summary/payload 有异 → 不再当 tags-union 静默吸收：RowsConflict 计数 + 'conflict' tag +
  已知冲突 re-echo 识别（no-op）。never-overwrite 完好——内容永不 UPDATE，status 不动。
- **internal/tools/memory_sync_bidir_e2e_test.go**（4 个双库 E2E，+13 单测断言面）：
  - Epic①：A 自投 2 行 + 完成 1 行 → push → B +2 新行（from-hp provenance）+1 前向迁移，
    行数守恒、内容逐字节一致、B→A 回程全 echo（零重复）。
  - Epic②：双侧同 id 推进到不同终态（completed vs failed，锚点齐平）→ RowsConflict=1
    显式报告、双侧 status 均不动、'conflict' tag、re-echo no-op。
  - Epic③：--tags 分区导出——只出命中行、子串不漏（module:x ≠ module:xx）、空过滤=整窗、
    TagsFilter 随 patch 对称约束 peer 的 push。
  - Epic④：export→mutate→import→re-export 全链——第三轮双向纯 echo，行数 1/1 不漂移。
  - 附：内容 hash 不变量测试（session 前后 A 侧行 hash 恒等）。
- devdoc/memsync-bidirectional.md（本文档）。

## Not Done

- brew 通道（M4 已有，epic 明示零改动——未触碰）。
- git 分支 push-back 通道（epic b 项：标准 git 语义，PA/gopi 处理——无代码改动需求）。
- conflict 的人工裁决工具（沿用 M4.12 的立场：计数+报行+tag，裁决留 nick）。
- Rule 11 LLM 调用验证：N/A——`task sync` 是 CLI 运维动词（人/PA 执行），不在 agent 工具面。

## Verification

- Build: `go build ./...` OK；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK。
- 单测: `go test ./internal/tools/ -run 'TestTaskSync|TestHarvest|TestSync|TestBidir' -count=1`
  → ok（含 4 个新 E2E + 既有 sync/harvest/task-sync 全绿，零回归）。
- 回归: `go test ./cmd/agent/ ./internal/config/ -count=1` → ok。
- CLI 实测: `task sync --serve-remote --tags x` → 正确拒绝（exit 1 + 报错行）；
  `task sync --status` 输出 per-peer 锚点行。
- Commit: de9abf4 feat(memsync): bidirectional epic gaps

## 设计决策（给未来 agent）

- **TagsFilter 放 patch 里而非协议外**：分区 session 的对称性靠数据自带——serve-remote 处理
  stdin patch 时顺手读到过滤条件，ssh 腿、envelope 形状、sync_state schema 全部零改动。
- **内容冲突门放在 same-status 分支最前**：原实现 same-status + tags-union 先行，内容碰撞被
  RowsTagged 吞掉（E2E 抓到的真 bug）。冲突判定必须先于一切"无害合并"。
- **测试的 env 隔离坑**：t.Setenv 在同一测试内互相覆盖——双库 sim 的每个 helper 必须先
  `n.use(t)` 重指 GOPI_MEMORY_DB，否则 submit/查询静默落错库（probe 测试抓到的）。
- **updated_at 1s 分辨率**：同秒两次 writeback = equal-anchor 冲突（合并语义如此）。测试里
  间隔 1.1s 写两步状态；真实节点同秒双写本来就是冲突面（与 epic ② 语义一致）。

## 交付位置（Rule 6d）

- 分支：`feat/memsync-bidirectional`（worktree ~/workspace/gopi-memsync-bidir，共享对象库，
  主仓立即可见；HEAD de9abf4）
- 恢复：`git -C ~/work/GoPi show feat/memsync-bidirectional:devdoc/memsync-bidirectional.md`
- 未 merge（Rule 6e：不自行 merge，留裁决）

---

## 增补（peerbox 收尾轮，2026-08-31）：真机双向 E2E + ssh 通道修复 + 台账收敛

> 接续轮在 peerbox（主仓 /home/user/work/GoPi, master 0e2d727）执行。分支 tip `0bc02c0`，
> worktree /home/user/workspace/gopi-memsync-bidir（peerbox 侧）。

### Completed

- **真机 ssh 通道修复（peerbox→hp 反向）**：peerbox 首次 `task sync --peer hp` 发现
  peerbox 的 ssh config 没有 hp 条目（只有 brew patch 时代的完整 argv 转发假设）。
  补 `Host hp` 段（10.0.0.5 + 专属 key）。HP→peerbox 方向 HP 侧 config 用
  `/home/user/.ssh/id_ed25519`（user 热邮箱 key）即可认证 peerbox。
- **双向 session 实测（真账 267 行）**：`task sync --peer hp` pushed=31 → 二轮
  pushed=2/pull already=N → 三轮 pushed=2 → 幂等 echo 稳定。`--status` 两 peer
  锚点齐平推进。
- **模块分区真机验证**：HP 自投 `module:x` / `module:y` 探针行 →
  `task sync --peer hp --tags module:x` 只拉回 module:x 行（module:y 不进入
  peerbox）；换 `--tags module:y` 对称验证。分区过滤对 push/pull 双向生效
  （TagsFilter 随 patch 对称约束 serve-remote 的 push）。
- **台账真账收敛**：两节点 272 行 task_id 集合与 status 全部一致
  （`diff` 空）。修复过程全部走显式 CLI `task update`（前向状态机），无一处
  直接 UPDATE——内容零覆写。zombie in_progress 行（97ddb03f 等 9 行）收敛到
  HP 终态；pa-q claimed 行通过镜像语义收敛（mirrored=3）。
- **sync session 报告行补 resolved= 计数**（0bc02c0）：server 裁决计数此前
  只在 stats 结构里，CLI 输出缺失——冲突面显式报告闭环。
- **devdoc 恢复**：原 e2a2501（docs: memsync bidirectional epic report）
  commit 在 HP worktree 被删后 dangling；从 HP 对象库 verbatim 恢复
  （5b4e438）。教训：worktree 清理前必须确认 branch ref 指向 tip commit。

### Verification（真机）

- peerbox→hp `task sync --peer hp`：双向 session 连续 5 轮，锚点只进不退。
- `--tags module:x` 分区拉取 imported=1（module:y 行未入 peerbox）。
- 台账守恒：两侧 272 行、status 集合 diff=0、无重复行。
- 幂等：重复 sync pushed=0/pull already=N、行数不漂移。
- 测试：tools 包 sync-family 全绿（TestTaskSync|TestHarvest|TestSync|TestBidir
  109s）、cmd/agent 包全绿、E2E shim 脚本 4/4（merge master 后重跑含
  ssh-argv 修复）。

### 恢复命令

```
git -C /home/user/work/GoPi show feat/memsync-bidirectional:devdoc/memsync-bidirectional.md
git -C /home/user/work/GoPi log --oneline -3 feat/memsync-bidirectional
```
（HP 主仓分支 ref 已由 peerbox fetch+update-ref 同步到 0bc02c0；HP/peerbox 双侧
对象库均有该分支。未 merge——按 Rule 6e 留裁决。）
