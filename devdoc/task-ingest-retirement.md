<!-- doc-class: historical -->
# task ingest 退役决策 (EPIC #185, 2026-08-30)

> nick 终判：task ingest（跨节点状态镜像接收端）toyish/useless ——
> 架构转向 db-sync 后，"联邦镜像"这一设计前提整体消失，由 memsync
> 替代（task ledger 通道 --export-tasks/--import-tasks）。

## 决策

**退役而非修 bug031。** bug031（#159，ingest toggle 非原子 check-then-write
竞态）不再修复——修好一个即将废弃的入口是浪费；工单已 cancelled（不修将
废件）。代码留档不删（联邦历史遗产，git 历史即文档）。

## 处置记录（三件，全部完成）

1. **HP 侧拔线**：`~/.bashrc` L135 `export GOPI_TASK_NOTIFY_WEBHOOK=
   http://192.168.7.6:8923/task-ingest` 已删除（防 HP 侧 task-notify 向
   已死端口打 400 误报）。备份存 HP `~/.bashrc.bak-ingest-retire`。
   排查 `.profile`/`.bash_profile`/`.bashrc.d` 无其他引用。
2. **peerbox 停 ingest 进程**：`./bin/gopi task ingest --listen :8923`
   （pid 4138416，2026-08-30 07:18 起 nohup 常驻）已 kill；端口 8923
   释放。启动脚本/Makefile/crontab/systemd 无引用残留（唯一启动方式
   为手工 nohup，无看门狗，kill 即净）。devdoc/task-ledger-m45.md 等
   历史文档中的 8923 字样为历史记录，保留。
3. **bug031 工单取消**：#159 (t-1788077827) → `cancelled`（show 确认
   持久化）。人类 epic #161 (t-1788078626) 为本退役单的上游记录。

## memsync 单通道确认（语义单一化完成）

`gopi memsync` task ledger 通道实测回环：

```
export : --export-tasks --out /tmp/ms-test.json --limit 3
         → exported 3 tasks (4 scanned) → node peerbox
import : --import-tasks /tmp/ms-test.json --db <memory.db 副本>
         → queued=0 duped=0 already=3 migrated=0 tagged=0 refused=0
再导入 : 同参数二次导入 → already=3 refused=0（幂等：already 语义）
```

注意：--import-tasks 的目标库是 **memory 库**（internal/tools/
memory_sync.go memoryDBPath()，task 行内嵌于 memory.db 的 tasks 表），
不是 tasks.db——tasks.db 副本会被拒（"not a distilled knowledge base"）。

## Verification

- HP `grep GOPI_TASK_NOTIFY ~/.bashrc` → 空（拔线完成）
- peerbox `pgrep -af "task ingest"` → 空；`ss -tlnp | grep 8923` → 空
- `gopi task show t-1788077827` → status: cancelled
- memsync export→import→re-import 回环 → already=3 refused=0 幂等

## Not Done

- 代码本体（cmd/agent/cli_tasks.go ingest 块、internal/tools 接收端）
  保留不删——联邦历史遗产，未来若 db-sync 需要接收端参考可考古。
- HP/其他节点的 memsync 周期化（cron/systemd timer）属 M4.9 双向
  sync 范畴（#182），不在本单。
