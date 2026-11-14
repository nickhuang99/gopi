<!-- doc-class: historical -->
# 同一份全量扫描被起两遍 — 诊断（diagnose-first，2026-10-01）

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a0f510-e2bc-7603-a47f-f2ae4a972566`（本行自己的 id，见 `## 证据` 的
`GOPI_TASK_RUN_ID` 读数）。这是**诊断报告**，产品代码未改（除本文件）。

一句话结论：**不是"两个人碰巧同时干一件事"，而是同一 run 在同一个窗口里
把同一条命令起了两遍**——先 `bash`（被 120s 空闲网掐掉，pipeline 仍活着），
再 `job`（无上限），两者扫同一棵树；而 `bash` 的掐断**杀不到**被掐的
pipeline，`job` 又**根本没有上限**，于是两个 5 分钟的遍历同时压在
`/home` 这块盘上（IO full avg10 50.74% → 81.12%）。

---

## 1. 复现事实（三条，全部可复核）

| # | 事实 | 证据 |
|---|---|---|
| F1 | 同一 session、同一命令起了两遍 | session `01a0f450-da2b-7cf5-b712-12566380ed56`，run `1543a6cb-be0c-46f4-9959-1c6cb0f86fee`：**JSONL 行 108** `bash` 起 `bash scripts/g7-verb-census.sh ./bin/gopi 2>&1 \| head -60`（`timeout_sec:300`）；**JSONL 行 115** `job(action=start)` 起**同一条** `bash scripts/g7-verb-census.sh ./bin/gopi > $EV/census.tsv …`。行 111 是前者的结局：`Bash error: command killed: no output for 2m0s`。 |
| F2 | 该 run **自己**在行 202 的 `ps` 里抓到了两条链 | 同一 JSONL 行 202（`2026-10-01T07:44:2x`）：`3242272 344 bash -c ulimit … g7-verb-census.sh …` / `3242275 344 bash scripts/g7-verb-census.sh ./bin/gopi` / **`3245125 337 bash scripts/g7-verb-census.sh ./bin/gopi`** / `3245128 337 rg -o -N --no-filename '"command": *"[^"]{0,400}' --glob session.jsonl /home/user/.gopi/sessions` / `3245835 121 rg …（同样的 rg）`。**行 203 的 thinking 原文**：`Two census runs are running concurrently`。 |
| F3 | 操作员在 07:44 附近量的指标 | `/proc/pressure/io` full **avg10=50.74%**（第一个窗口）→ **avg10=81.12%**（第二个窗口）；同一时刻 `find … -size +1M`（pid 3246528）**D 状态卡死 43s**，两条 `rg` 分别已跑 **309s / 93s**。两进程共用同一条 `bash -c ulimit …` 父链形状（`bash.go:302`）。 |

**同形问题的第二次**：前一次是 `01a0f3f7`（一个 Bond 起两组重叠 `go test`），
已在 `devdoc/overlapping-test-runs-diagnosis.md` 里记录（本分支 `56f6dd4d`）。

---

## 2. 根因链（5-why，逐条带 file:line / 测量）

**① 为什么慢？** 不是字节量——**是 162 636 个目录的遍历**。

同一条 stage-4d 查询（``scripts/g7-verb-census.sh:361``），同一棵树、同一时刻，
只改一个参数：

```
$ cd /tmp   # 磁盘空闲时（两个肇事者已在 07:45 被杀）
$ rg -o -N --no-filename '"command": *"[^"]{0,400}' --glob session.jsonl ~/.gopi/sessions
rg_elapsed=3.40 s   maxrss=331500 KB                 # 3.4s / 0.43GB 命中
$ rg -j 1 … # 同上
j=1 elapsed=4.79 s          $ rg -j 4 …
j=4 elapsed=2.44 s                                   # 线程数不是变量
$ rg -o -N --no-filename '"command": *"[^"]{0,400}' ~/.gopi/sessions   # 去掉 --glob
glob_false_s=120.01（超时被杀）   vs   glob_true_s=2.71
```

- 树的形状（Rule 38：生成器 = `find`/`du`，测量于本报告写作时=分支 tip）：
  `find ~/.gopi/sessions -type d | wc -l` → **162636**；
  `find … -name session.jsonl` → **3414 个文件 / 0.43 GB**；
  `find … -name '*.raw.log'` → **1316 个文件 / 45.06 GB**；`du -sh` → **56G**。
- **`--glob` 没有让 walker 跳过 45 GB**（去掉它立刻 120s 超时，加上 2.71s）：
  两次都要把 162 636 个目录全部 `getdents` 一遍；区别只在**要不要把命中
  的文件读进来**——`session.jsonl` 共 0.43 GB（3s 级），`*.raw.log` 共 45 GB
  （分钟级）。所以真正的时间常数是 **"每个目录一次随机读" × 162636**：
  盘 IO full 时单次 `getdents` 1–3 ms ⇒ **2.7 分钟 ~ 8 分钟**，
  正是 F3 里 309s / 93s / D 状态 43s 的量级。
- **盘是共享资源，两个 walker 互相饿死**（本报告的实测前提：`rsync`/`rg` 类
  walk 是延迟受限负载）：F3 的 avg10 50.74→81.12 就是这个叠加。

**② 为什么"上限"没有拦住它？** 计算有上限，**IO 没有**。

- 前台 `bash`：`internal/tools/bash.go:46` `const bashNoOutputTimeoutSec = 120`
  —— 空闲输出安全网。它**掐的是 launcher，不是计算**，且掐断后
  pipeline 存活（见 ④）。
- `job(action=start)`：`internal/tools/job.go:138`
  `cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}`，全文**没有** ctx /
  timeout / deadline（`grep -n 'timeout\|Timeout\|ctx\|Deadline' internal/tools/job.go`
  → 0 条实质命中）。**而这正是系统提示推荐长任务的路径**
  （job 工具描述："Outlives one bash call? job(action=start)"）。
  **上限的缺失恰好落在被推荐的那条路上。**

**③ 为什么没有"重复检测"？** job 注册表是**进程内单例**：
`internal/tools/job.go:103` `var theJobs = newJobManager()`，
`job.go:98` `newJobManager()` 只 `make(map[string]*Job)`；没有任何跨进程
枚举（`grep -rn 'pgrep\|/proc/\[' internal/tools/job.go internal/tools/bash.go`
→ 0 命中）。同一台机器上第二个 gopi 进程无法知道第一个正在扫同一棵树。

**④ 为什么"掐断"没有真的掐断？** 负 PID 杀的是**外层包装器的进程组**，
而 pipeline 的三个进程不在那个组里。逐字引用 JSONL 行 205（本 run 自己发的）：

```
--- kill rogue census ---
kill 3245125 2>/dev/null
```

`3245125` 是 `bash scripts/g7-verb-census.sh`，但它不是 `bash -c '… | head -60'`
那个组的组长。桌面上用**同形状**的命令验证（本报告实测）：

```
$ setsid bash -c 'ulimit -v 12582912 -u 4096 …; sleep 300 2>&1 | head -60' &
$ BP=$(pgrep -f 'sleep 300' | head -1);  # 104007
  104007  104007 bash      ← 组 = 104007
  104008  104007 bash      ← 104007 的**子** bash，才是管道的左侧
$ kill -9 -104007         # bash.go 的空闲/超时杀法
$ ps -eo pid,pgid | awk '$2==104007'   → 空（无人生还，但组外孤儿仍在）
$ ps -eo pid,pgid | awk '$4=="sleep"'  → 69928(1375) 88685(88684) 88717(1136914)  ← 全是「换了进程组、被 init 收养」的孤儿
```

同形状在本 run 内的第二次复现（`gopi-job-3683551203.out`）：

```
recorded pipeline bash pid=105652
105652 S 105650 105650 bash     ← PGID=105650(the wrapper)，**不是 105652**
$ kill -9 -105652 →  bash: kill: (-105652) - No such process
```

`bash.go` 的 `$!` 记录的是**外层 `bash -c` 的 PID**，而管道左端是它 fork 出来的
**另一个** bash（`bash -c 'A | B'` 的内部优化，见 `internal/tools/taskproc.go:318`
注释：`/bin/bash -c "sleep 30"` 会退化成 `/bin/sleep 30`）。负号杀组时，
组长 ≠ 记录的 PID ⇒ `kill` 报 ESRCH；而真正在扫盘的 `rg` 换了组、被 init 收养后
**继续跑**。**这就是为什么 07:39:11 起的 `rg 3245128` 在 07:44 还活着。**
（`internal/tools/job.go:277` 用同样的负 PID 杀法，所以 job 的 kill 同病。）

顺带：`bash.go` 的空闲网还会**误杀正常长扫描**——stage 4d 的 pipeline 左侧
未结束时右侧 `head` 不 flush，stdout 恒定为空：

```
$ timeout 5 bash -c 'sleep 300 2>&1 | head -60' | wc -c → 0   （5s 被杀，一个字都没吐）
```

所以"5 分钟静默被当成挂死"是**必然**，不是偶发。

**⑤ 为什么没人看见？** 全仓库**没有任何一处读 `/proc/pressure/io`**：
只有两行**注释**提到该指标（`internal/tools/firstopen_sync_decision.go:11`、
`internal/tools/ledger_dsn.go:177`）——没有任何代码路径消费它。

---

## 3. 缺口清单（三条，各自现状 + file:line）

| 缺口 | 现状 | 位置 |
|---|---|---|
| **成本上限** | 前台唯一上限是"空闲 120s 无输出"（**不是"总共跑多久"**）；`job` 路径**完全无界**。 | `internal/tools/bash.go:46`（有界，掐 launcher）；`internal/tools/job.go:138`（无界） |
| **重复检测** | 无。注册表是进程内单例，无跨进程枚举。 | `internal/tools/job.go:98,103` |
| **全局可见性** | 无。`/proc/pressure/io` 只出现在注释里；没有"peer 是否在扫同一棵树"的任何查询面。 | `firstopen_sync_decision.go:11`、`ledger_dsn.go:177`（注释） |

**与 `01a0f3f7` 的关系判定：同形，不同载体。**
同一份"全量扫描"工作被并发起两遍（机制相同：第二个启动点**看不见**第一个），
但载体不同：`01a0f3f7` 是**一个 Bond 里 `go test` 两组重叠**
（`devdoc/overlapping-test-runs-diagnosis.md`），本次是**同一 run 的
`bash` + `job` 两条链**，扫的是 `~/.gopi/sessions` 而不是 repo 构建。
共同的上游缺陷是同一个：**启动一个全量重活之前，没有"这台机器上还有谁在做
同一件事"的答案，也没有"我最多能花多少 IO"的上限**。

---

## 4. 成本归因：41GB 扫描是否必要？有没有更便宜的等价查询？

**问题（stage 4d，`scripts/g7-verb-census.sh:361`）**：`gopi <verb>` 有没有被
**真实**执行过（`--help` 不算，Rule 31）。它用一条 `rg` 扫全部
`session.jsonl` 来回答。

**测量结论：查询本身不贵，贵的是"fan-out × 竞争"。**

- 空闲盘上，同一条 `rg` **2.71–3.40 s**、**0.43 GB** 命中、maxrss 331 MB。
  换句话说：**41 GB 里只有 0.43 GB 是它要读的**，其余 45 GB（`*.raw.log`）
  它靠 `--glob` 在读入阶段就跳过了 —— 但**遍历阶段一个目录都没省**，
  而那 162 636 次 `getdents` 才是 3 秒 / 5 分钟的分水岭（§2 ①）。
- **更便宜的等价查询确实是存在的，而且是现成的索引**：账本 `chats` 表带
  `log_path`（`internal/tools/memory.go` 的 `chats` schema）：
  ```
  chats rows = 216 | with log_path = 207 | log_path existing = 129
  ```
  ⇒ 一条 SQL + 129 个文件，可回答"最近 216 个会话里有没有跑过这个 verb"。
  **但它是部分覆盖（129/3414）**，替代不了 census 宣称的"全语料"，
  只能作为**第一遍便宜扫描**。
- **索引能不能彻底替代？不能，原因具体**：`session_conclusions` 只存
  distilled 结论（`select count(*) … conclusion like '%gopi %'` → 3350 行），
  **不存命令串**；`task_events.detail` 是事件文本；`chats` 只登记会话级元数据。
  真正带 `"command"` 字段的是各 `session.jsonl` 里的 tool_use 行——
  **没有哪张表存它**（这也是 census 必须扫磁盘的原因）。
  ⇒ 要把 cost 真正降下来，要么**给会话树建索引**（新持久依赖，Rule 35 四问
  先行），要么**把 `--glob` 语义做实**（见 §5 建议 1）。

---

## 5. 修法建议（不新增常驻物）

按"改动面 / 收益"排序。全部**不引入 daemon / cron / 状态文件**（Rule 35）。

1. **把 `--glob` 的语义做实（首选，零新依赖，纯脚本改动）。**
   今天 stage 4d 交给 `rg` 的是**整个 `sessions` 根**，161 222 个无关目录
   被白白走一遍。改成**只把文件清单喂进去**：
   ```bash
   find "$SESSIONS" -name session.jsonl -print0 | xargs -0 -r rg -o … 
   ```
   本报告实测：`find … -name session.jsonl | wc -l` **2.73 s**（列出 3155 个）。
   把 walk 变成一次 `find` 之后，两条链不再各走一遍树；而且
   `find` 的输出是**同一份可复用的清单**（census 内其它 stage 也能吃）。
   附带好处：不再需要 `--glob` 这个只有 rg 认识的开关。
2. **`job` 加"总时长"上限，而不是只靠空闲网。** 现在 `job` 无界
   （`internal/tools/job.go:138`）。最小改动是给 `job(action=start)` 一个
   默认墙钟上限（常量 + 既有 env-knob 模式，Rule 2），超时走**已有的**
   `Kill` 路径。**注意**：必须先修 3，否则这个上限同样杀不到孤儿。
   ⚠️ 涉及承重结构（长任务全都走它），**需要用户拍板**：默认值取多少、
   是否允许显式 `--no-timeout`。
3. **把负 PID 杀法改对（bug fix，不是策略）。** 现在 `$!` / job 的
   `cmd.Process.Pid` 是**外层包装器**，而管道左侧在**另一个**进程组里
   （§2 ④ 两次实测）。两条候选：
   (a) 起命令时 `SysProcAttr{Setpgid:true}` 并记录**真正的组长**；
   (b) 杀之前用 `pgrep -P <pid>`-形状的**进程树**枚举（`/proc` 走一遍），
       逐层杀。建议 (a)+(b) 一起：组内负 PID 杀 + 组外后代补杀。
   **必须带回归测试**（Rule 10.5）：起 `bash -c 'sleep 300 | head -60'`，
   断言 kill 之后**没有任何** `sleep` 后代存活。
4. **启动一个"全量重活"前的一行自检（可选，成本最低的可见性）。**
   不建常驻物，只在 `job(action=start)` / `bash` 起命令时，若命令里出现
   已知重活的标记（如 `g7-verb-census.sh`、`go test ./...`、`rg … ~/.gopi`），
   读一次 `/proc/pressure/io` **full avg10**，超阈值就在结果里带一句
   "当前 IO full avg10=X%，另有 N 个 gopi worker 在跑"。
   **注意**：这是**告知**，不是闸门——不要顺手做成拒绝启动（会变成
   新的隐式调度器）。且它需要"哪些命令算重活"的判据，属于设计决策。

**明确不做的事**：不放宽任何 timeout；不把 census 标记成 heavy 了事
（`internal/testutil/heavy_inventory_gate_test.go` 是分类工具，不是修复）；
不加 cron/daemon；不削弱 `internal/testutil` 的 scratch-root 拒绝。

---

## 6. 证据（self-filed）

本报告每条结论的测量命令与读数；所有 `rg -o … sessions` 之外的写操作都在
本 worktree 或本 run 的 session 目录内。

```
# F1/F2/F3 —— 事故 run 的原始 JSONL（行号即本报告引用）
sessions/01a0f450-da2b-7cf5-b712-12566380ed56/1543a6cb-be0c-46f4-9959-1c6cb0f86fee.jsonl
  行 108  bash  g7-verb-census.sh ./bin/gopi 2>&1 | head -60      timeout_sec=300
  行 111  RESULT  Bash error: command killed: no output for 2m0s   exit=-1
  行 115  job   job-3 = bash g7-verb-census.sh ./bin/gopi > $EV/census.tsv
  行 202  ps: 3242272/3242275/3245125(bash g7-verb-census) 3245128/3245835(rg) 3246528(find, D 43s)
  行 203  thinking: "Two census runs are running concurrently"
  行 205  "--- kill rogue census ---" ; kill 3245125
→ 已复制留档：~/.gopi/sessions/01a0f510-e2bc-7603-a47f-f2ae4a972566/evidence-01a0f450-run.jsonl

# 树的形状（生成器 = find/du，测量于本次 run）
$ find ~/.gopi/sessions -type d | wc -l                        → 162636
$ find ~/.gopi/sessions -name session.jsonl -type f | wc -l     → 3414
$ find … -name session.jsonl -printf '%s\n' | awk …             → files=3414 bytes=464949424 (0.43 GB)
$ timeout 100 find ~/.gopi/sessions -maxdepth 1 -name '*.raw.log' …
$ find … -maxdepth 3 -name '*.raw.log' -printf '%s\n' | awk …   → raw.log files=1316 bytes=45.06 GB
$ du -sh ~/.gopi/sessions                                       → 56G
$ du -s --block-size=1G ~/.gopi/sessions                        → 56
$ stat -c '%d %n' / /home                                        → 2051 / ; 2052 /home   （Rule 22：不同设备）
$ lsblk -no ROTA /dev/sda                                       → 0（非旋转盘）

# §4 成本归因（同一条 stage-4d 查询，只改一个参数）
$ /usr/bin/time rg -o -N --no-filename '"command": *"[^"]{0,400}' --glob session.jsonl ~/.gopi/sessions
  rg_elapsed=3.40 s  maxrss=331500 KB
$ /usr/bin/time rg -j 1 …   → j=1 elapsed=4.79 s maxrss=149940 KB
$ /usr/bin/time rg -j 4 …   → j=4 elapsed=2.44 s maxrss=201860 KB
$ /usr/bin/time rg -o … ~/.gopi/sessions            （无 --glob） → glob_false_s=120.01（超时）
$ /usr/bin/time rg -o … --glob session.jsonl …                  → glob_true_s=2.71
$ time find ~/.gopi/sessions -maxdepth 3 -name session.jsonl -type f -print | wc -l
  → 3155 / real 0m2.731s
$ python3: chats rows=216 with log_path=207 log_path existing=129
  （read-only 打开 file:/home/user/.gopi/memory.db?mode=ro）

# §2 ④ 负 PID 杀法（两次实测）
$ setsid bash -c 'ulimit -v 12582912 -u 4096 2>/dev/null; … sleep 300 2>&1 | head -60' &
  组 104007 成员：104007(bash) 104008(bash) 104021(ps) 104022(awk)
$ kill -9 -104007 ; 2s 后组内无人生还，但 ps 仍见 69928/88685/88717 三个换组孤儿
$ 本 run 内复现（job-2）：105652 PGID=105650≠105652 ⇒ kill -9 -105652 → "No such process"
$ timeout 5 bash -c 'sleep 300 2>&1 | head -60' | wc -c          → 0（空闲网必然误杀）

# 缺口位置的 file:line（分支 tip 读取）
internal/tools/bash.go:46      const bashNoOutputTimeoutSec = 120
internal/tools/bash.go:302     exec.CommandContext(ctx, "bash", "-c", scratch.ChildLimitPreamble()+command)
internal/tools/job.go:98,103   newJobManager() / var theJobs = newJobManager()   （进程内单例）
internal/tools/job.go:127,138  exec.Command("bash", "-c", …) / SysProcAttr{Setsid:true}
internal/tools/job.go:277      "// Negative pid = the process group (Setsid made the job its leader)."
internal/tools/taskproc.go:318 "…/bin/bash -c \"sleep 30\" becomes /bin/sleep 30…"
scripts/g7-verb-census.sh:361  rg -o -N --no-filename '"command": *"[^"]{0,400}' --glob session.jsonl "$SESSIONS"
internal/tools/firstopen_sync_decision.go:11 , internal/tools/ledger_dsn.go:177    （仅注释提及 /proc/pressure/io）
$ grep -n 'timeout\|Timeout\|ctx\|Deadline' internal/tools/job.go  → 无实质命中（job 路径无上限）

# 与 01a0f3f7 的同类性
$ git log --oneline -3 -- devdoc/overlapping-test-runs-diagnosis.md → 56f6dd4d docs(diagnosis): one Bond, two overlapping go test sets …
$ gopi task list --all | grep 01a0f3f7 → 01a0f3f7-2a01-73e4-a445-48a3a8accee2 [completed] BUG: one Bond ran two overlapping 'go test' sets at once
$ gopi task list --all | grep 01a0f511 → 01a0f511-3d18-73a7-bf50-503394a44771 [pending] bug: a brief forbidding duplicate heavy work did not bind its own closer — 01a0f3f7 repeated by peerbox:2704384

# 回归门在本分支为绿（git add 本报告之前的读数）
$ go test ./cmd/agent/ -run 'TestVerbCensusMatchesTheDispatch' -count=1
  → ok  github.com/nick/gopi/cmd/agent  14.005s      GATE_RC=0
```
