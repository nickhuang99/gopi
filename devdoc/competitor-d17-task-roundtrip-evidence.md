<!-- doc-class: normative -->
# D17 的任务行提交→按 id 读回往返（E3 亲跑证据，自足形态）

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

本文是 `devdoc/competitor/gap-*.md` 主表 **D17（任务/子 agent 生命周期与状态保存）**
「我方」一格的 E3 证据本体。它刻意把**命令与原始输出直接写在正文里**，而不是指向
以 `artifacts/` 开头的这类相对路径（共 374 条，逐条列在 §5 所述的提行里）——因为那些路径在本机任何 checkout 里都取不回来
（见 §5，实测 374 条 missing）。读者不需要跳转到任何文件就能复核本文的每一条断言。

测量时间：2026-11-06 08:03–08:05 (+08:00)；run `01a1ae0c-962a-7cd8-bd83-7e583bebf4b0`；
二进制 `bin/gopi`（`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`，13.3s 内建好）。

## 1. 用了哪个 profile（这一条决定证据是否污染活跃账本）

探针行**不写进操作员的活跃账本**。用的是一条私有 profile（PROJECT.md Rule 34 的
标准沙箱，它有自己的 `memory.db`），由 gopi 自己的初始化器创建：

```bash
$ ./bin/gopi tool sandbox create --label d17 --task-id 01a1ae0c-962a-7cd8-bd83-7e583bebf4b0
# → /home/user/workspace/gopi-sandbox/sandbox-d17-61605742
```

沙箱的 profile 根与它的账本是两个**真实文件**，与操作员的账本不同一个 inode：

```bash
$ stat -c '%n dev=%d inode=%i size=%s mode=%a' \
    /home/user/workspace/gopi-sandbox/sandbox-d17-61605742/.gopi/memory.db \
    /home/user/.gopi/memory.db
/home/user/workspace/gopi-sandbox/sandbox-d17-61605742/.gopi/memory.db dev=66305 inode=152974884 size=131072 mode=600
/home/user/.gopi/memory.db dev=66308 inode=2112960 size=795996160 mode=644
```

下面四条命令都以
`GOPI_HOME=/home/user/workspace/gopi-sandbox/sandbox-d17-61605742` 与
`GOPI_MEMORY_DB=/home/user/workspace/gopi-sandbox/sandbox-d17-61605742/.gopi/memory.db`
为前缀，工作目录是 clone ground `/home/user/workspace/01a1ae0c-962a-7cd8-bd83-7e583bebf4b0`。

## 2. 提交，拿到它打印的 id

```bash
$ GOPI_HOME=$SB GOPI_MEMORY_DB=$SB/.gopi/memory.db ./bin/gopi task submit \
    'D17-E3-probe: read-back round-trip, sandbox-only row' --tags d17-probe
task_submit: repo "/home/user/workspace/01a1ae0c-962a-7cd8-bd83-7e583bebf4b0" is the clone channel's workspace for one run (under the workspace root "/home/user/workspace") — stamping the source repository /home/user/work/gopi it was cloned from, so the row outlives this run
gopi task submit: no --payload given — the positional summary is mirrored into the payload (a payload-less row is never claimed by worker/serve)
Task queued: 01a1ae2b-19a7-742d-bc7c-58df6b60c7d5 (status=pending, source=agent, tags=d17-probe)
exit=0
```

（每一条命令在真实运行里前面还有两行关于 `task_pull` 已退役的 stderr 通告，与本题无关，
此处不重复；它们不影响任何一行的退出码。）

## 3. 按**它打印的那个 id**读回

```bash
$ GOPI_HOME=$SB GOPI_MEMORY_DB=$SB/.gopi/memory.db ./bin/gopi task show 01a1ae2b-19a7-742d-bc7c-58df6b60c7d5
task_id:        01a1ae2b-19a7-742d-bc7c-58df6b60c7d5
status:         pending
session:        (no session id)  run directory holds no run log and no session id is recorded for this row
summary:        D17-E3-probe: read-back round-trip, sandbox-only row
run_dir:        /home/user/workspace/gopi-sandbox/sandbox-d17-61605742/.gopi/sessions/01a1ae2b-19a7-742d-bc7c-58df6b60c7d5
runs:           none (no run log in the directory)
session_id:
model:          (unknown)
parent_task_id: —
tags:           d17-probe
priority:       5
source:         agent
submitted_by:   LLM session — (task_submit)
deliverable.repo: /home/user/work/gopi
briefing:
    D17-E3-probe: read-back round-trip, sandbox-only row
task_report:    no report recorded
created_at:     2026-11-06 08:03:42
updated_at:     2026-11-06 08:03:42  (state/sync anchor — not a heartbeat)
ended_at:       (not ended)
exit=0
```

读回的每一格都是提交写进去的：id 相同、`status=pending`、`tags=d17-probe`、
`source=agent`、`priority=5`、briefing 就是那条 summary、`created_at` 有值、
`ended_at` 未结束、`run_dir` 落在**私有 profile 的**会话根下（不是操作员的）。

## 4. 写一次状态，再读一次 —— 读反映写

```bash
$ GOPI_HOME=$SB GOPI_MEMORY_DB=$SB/.gopi/memory.db ./bin/gopi task update \
    01a1ae2b-19a7-742d-bc7c-58df6b60c7d5 --status cancelled \
    --reason 'D17 probe: private-sandbox row, cancelled after read-back so it can never be dispatched'
Task updated: 01a1ae2b-19a7-742d-bc7c-58df6b60c7d5 (status=cancelled, reason=D17 probe: private-sandbox row, cancelled after read-back so it can never be dispatched)
exit=0
```

```bash
$ GOPI_HOME=$SB GOPI_MEMORY_DB=$SB/.gopi/memory.db ./bin/gopi task show 01a1ae2b-19a7-742d-bc7c-58df6b60c7d5
task_id:        01a1ae2b-19a7-742d-bc7c-58df6b60c7d5
status:         cancelled
session:        (no session id)  run directory holds no run log and no session id is recorded for this row
summary:        D17-E3-probe: read-back round-trip, sandbox-only row
run_dir:        /home/user/workspace/gopi-sandbox/sandbox-d17-61605742/.gopi/sessions/01a1ae2b-19a7-742d-bc7c-58df6b60c7d5
runs:           none (no run log in the directory)
session_id:
model:          (unknown)
parent_task_id: —
tags:           d17-probe
priority:       5
source:         agent
submitted_by:   LLM session — (task_submit)
reason:         D17 probe: private-sandbox row, cancelled after read-back so it can never be dispatched
deliverable.repo: /home/user/work/gopi
briefing:
    D17-E3-probe: read-back round-trip, sandbox-only row
result:         (cancelled)
transition:     who=run:01a1ae0c-962a-7cd8-bd83-7e583bebf4b0 pending → cancelled why=D17 probe: private-sandbox row, cancelled after read-back so it can never be dispatched
trans.event:    cancelled
task_report:    no report recorded
created_at:     2026-11-06 08:03:42
updated_at:     2026-11-06 08:03:57  (state/sync anchor — not a heartbeat)
ended_at:       2026-11-06 08:03:57
exit=0
```

**读确实反映了写**：`status` pending→cancelled，`reason` 出现，`result: (cancelled)`，
新增 `transition:` / `trans.event:` 两行审计（谁在什么时候把它从什么改成了什么），
`ended_at` 由 `(not ended)` 变成 `2026-11-06 08:03:57`，`updated_at` 同步推进。

## 5. 往返是**按 id**，不是「读最新一行」—— 两条对照

**(a) 错 id 按名失败，不会退化成"给最新的那行"**

```bash
$ GOPI_HOME=$SB GOPI_MEMORY_DB=$SB/.gopi/memory.db ./bin/gopi task show 01a1ae2b-dead-beef-0000-000000000000
gopi task show error: task: no task with task_id="01a1ae2b-dead-beef-0000-000000000000"
exit=1
```

**(b) 同一个 id 在操作员的活跃账本里查不到 —— 证明探针行没有落进活跃队列**

```bash
$ ./bin/gopi task show 01a1ae2b-19a7-742d-bc7c-58df6b60c7d5      # 无沙箱环境变量，默认 profile
gopi task show error: task: no task with task_id="01a1ae2b-19a7-742d-bc7c-58df6b60c7d5"
exit=1
```

```bash
$ ./bin/gopi task list --all --db /home/user/.gopi/memory.db | grep -c 01a1ae2b-
0
grep_exit=1
```

沙箱账本自己的列表里它只有一行：

```bash
$ GOPI_HOME=$SB GOPI_MEMORY_DB=$SB/.gopi/memory.db ./bin/gopi task list --all
1 task(s):
#01a1ae2b-19a7-742d-bc7c-58df6b60c7d5  [cancelled]  D17-E3-probe: read-back round-trip, sandbox-only row  (model (unknown), source agent, created 2026-11-06 08:03:42)
exit=0
```

⇒ 提交与读回走的是**同一个 store**（沙箱 profile 解析出的那一个 `memory.db`），
且该 store **不是**操作员的账本。探针行因此既不会变成活跃队列里的可派发垃圾，
也不需要靠事后清理来保证这一点（它另外已被 `cancelled`，双重保险）。

## 6. session / runs / 落盘路径三组字段：在本 run 自己的行上读一次

探针行没有跑过，所以它的 `runs:` 是 `none`。要让同一张表上的「会话 / 轮次 / 落盘路径」
三组字段出现，读本 run 自己的行即可（活跃账本，只读）：

```bash
$ ./bin/gopi task show 01a1ae0c-962a-7cd8-bd83-7e583bebf4b0
task_id:        01a1ae0c-962a-7cd8-bd83-7e583bebf4b0
status:         in_progress
session:        01a1ae0c-962a-7cd8-bd83-7e583bebf4b0  state=running  crash=none  turns=28  requests=30
summary:        gap-doc D17 probe: submit then read back by id
session_log:    /home/user/.gopi/sessions/01a1ae0c-962a-7cd8-bd83-7e583bebf4b0/01a1ae0c-962a-7cd8-bd83-7e583bebf4b0.jsonl
raw_log:        /home/user/.gopi/sessions/01a1ae0c-962a-7cd8-bd83-7e583bebf4b0/01a1ae0c-962a-7cd8-bd83-7e583bebf4b0.raw.log
task_output:    /home/user/.gopi/sessions/01a1ae0c-962a-7cd8-bd83-7e583bebf4b0/output.log
run_dir:        /home/user/.gopi/sessions/01a1ae0c-962a-7cd8-bd83-7e583bebf4b0
run_log:        /home/user/.gopi/sessions/01a1ae0c-962a-7cd8-bd83-7e583bebf4b0/01a1ae0c-962a-7cd8-bd83-7e583bebf4b0.jsonl
runs:           1 run(s) — 0 completed, 1 with a raw-log checkpoint
run:            01a1ae0c-962a-7cd8-bd83-7e583bebf4b0  2026-11-06 08:02:19  unfinished  28 loop passes  ← resume target
session_id:     01a1ae0c-962a-7cd8-bd83-7e583bebf4b0
model:          deepseek-flash
parent_task_id: —
tags:           gap-probe
priority:       5
source:         agent
submitted_by:   LLM session — (task_submit)
run_method:     worker
run_cmd:        gopi worker
reason:         work started
deliverable.repo: /home/user/work/gopi
ground (resolved now): repo=/home/user/work/gopi worktree=(none) branch=(none) — the row's session "01a1ae0c-962a-7cd8-bd83-7e583bebf4b0" names no worktree-add and no in-place branch creation; the row's clone ground /home/user/workspace/01a1ae0c-962a-7cd8-bd83-7e583bebf4b0 (model clone) holds no "task/01a1ae0c962a7cd8bd837e583bebf4b0" ref
resume this row: `gopi session resume <id>` (no live ground here, so nothing has to be released)
briefing:
    gap-doc D17 probe: submit then read back by id
result:         verdict=DOABLE items=0 origin=01a1ae0c-962a-7cd8-bd83-7e583bebf4b0/3591068
transition:     who=operator claimed → in_progress why=work started
trans.event:    in_progress
verdict:        verdict=DOABLE items=0 origin=01a1ae0c-962a-7cd8-bd83-7e583bebf4b0/3591068
verdict.event:  reframe
task_report:    no report recorded
claimed_by:     hosta:937150 (at 2026-11-06 08:02:19)
liveness:       last_toolcall_at=08:04:10 (age 13.31s — fresh) tasklist_issued_at=(none — unknown)  (clock 08:04:23)
process:        pid=3591068 start=2026-11-06T08:02:19+08:00 cmd="/home/user/work/gopi/bin/gopi"  (live: alive)
created_at:     2026-11-06 07:30:22
updated_at:     2026-11-06 08:02:22  (state/sync anchor — not a heartbeat)
ended_at:       (not ended)
exit=0
```

这一格里的 `session:`（含 `state` / `crash` / `turns` / `requests`）、`runs:`（含断点计数
与 `← resume target` 标记）、以及 `session_log:` / `raw_log:` / `task_output:` / `run_dir:`
/ `run_log:` 五条落盘路径，全部由**同一次 `task show`** 从账本与磁盘现状一起渲染出来。

## 7. 一处需要读者注意的差异：`run_dir` 跟着**读的 profile** 变，不跟着行变

同一行、同一个 `--db`，只要换掉 `GOPI_HOME`，`run_dir` 就换根：

```bash
$ ./bin/gopi task show 01a1ae2b-19a7-742d-bc7c-58df6b60c7d5 --db $SB/.gopi/memory.db
run_dir:        /home/user/.gopi/sessions/01a1ae2b-19a7-742d-bc7c-58df6b60c7d5  MISSING — run directory not created — no session directory is recorded for this row
runs:           none (run directory not created)
exit=0
```

这条命令读的是沙箱账本（`--db`），却把 `run_dir` 解析到了**操作员的**
`/home/user/.gopi/sessions/01a1ae2b-19a7-742d-bc7c-58df6b60c7d5`，并如实报告它不存在
（`MISSING — run directory not created`）。所以「落盘路径」一栏是**账本文件 + 当前 profile** 两件事共同渲染的结果，
读的人要知道自己用的是哪个 profile。这是**实测到的行为**，本行不判断它是不是缺陷；
若判断为缺陷，按 PROJECT.md Rule 41 应单独提行（本 run 已就 §5 所述的另一件事提行
`01a1ae2c-6929-77cc-a0d6-cc000718b3ca`）。

## 8. 结论（D17「我方」一格的判据）

| 项 | 实测 |
|---|---|
| 提交打印 id | `Task queued: 01a1ae2b-19a7-742d-bc7c-58df6b60c7d5`，exit=0 |
| 按该 id 读回 | 同一 id 整行，exit=0 |
| 是否按 id | 错 id → `task: no task with task_id="01a1ae2b-dead-beef-0000-000000000000"` exit=1（不退化成"最新一行"） |
| 读是否反映写 | 一次 `--status cancelled` 写之后，`status` / `reason` / `result` / `transition` / `ended_at` 全部随之改变 |
| 是否同一 store | 同一 profile 内 submit 与 show 互见；换 profile 立即 `no task with task_id` |
| 行级字段 | `status` / `session`（state·crash·turns·requests）/ `runs`（断点与 resume 目标）/ 五条落盘路径 |
| 副作用 | 探针行在私有 profile + 已 `cancelled`；操作员账本 grep 计数为 0；`git status --porcelain` 为空 |

⇒ 我方 D17 = `full`：任务行**提交后可按 id 读回整行**，且读回的会话/轮次/落盘路径
与写入是同一个 store。
