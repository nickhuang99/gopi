<!-- doc-class: normative -->
# 谁来决定「这个 run 正在被执行」—— 内核锁，不是 pid 存活

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**这条文档回答一个问题**：`gopi session resume` / `gopi worker --resume` 判断
「这一行还能不能接着跑」时，凭的是什么证据。判据在 2026-11-05 换了一代，本文记录
为什么换、换成了什么、以及**缝隙还在哪里**。

## 1. 缺陷：闸问的不是那个问题

**先读一句指路**：`devdoc/task-process-identity.md` 是这个闸**换代之前**的记录
（`<!-- doc-class: historical -->`，2026-09-13），它里面
「`gopi resume` refusing — task … has a verified-LIVE holder process; resuming
would run it twice」那段输出是当时**真实发生过**的答案。**它是历史，不是现行判据** ——
本文第 2 节起是今天的。

`decideResumeByTaskID` 读 `tools.TaskProcessStatusFor`，后者走**进程身份阶梯**
（pid + starttime + cmdline，`internal/tools/taskproc.go`）再加上 **cwd 行梯级**
（`taskproc_cwd_row.go`）。它回答的是：

> 「账本记录的那个进程**还存在**吗？」

而 resume 需要回答的是：

> 「这个 run 目录**正在被谁执行**吗？」

两个问题在大多数时候答案相同，所以这个替代长期看起来是对的。它们在下面这一对
实测里分叉，而副作用是**逐字相同的拒绝文本**：

| 同一个宿主、同一个晚上 | 结论 |
|---|---|
| holder 5h18m，CPU 24s，36 线程全在 futex，`session.jsonl` 停在 16:57:18 —— **零进展** | 拒绝是**错的** |
| holder 已跑 2m25s，正在一轮之内 | 拒绝是**对的** |

旧闸无法区分这两种，因为它只有一个自由度。

## 2. 新一代判据：run-scope 内核 flock

`3f342f570` 引入了 **run-scope 排他锁**：一个 run 目录（
`sessions/<row-id>/`）下的 `run.lock`，由**执行那个 run 的进程**独占持有。

它的性质正好是这个闸需要的：

- **一个进程拿不到别人正拿着的锁** → 「我拿到了」≡「没有人在执行这个目录」；
- **进程一死锁即释放**，`SIGKILL` 也不例外，**不需要任何 sweep 去相信**；
- 它是**内核**的事实，不是某个 pid 的推断。

所以判据变成：

```
拿到锁   ⇒ 没有人在执行 ⇒ 账本里「LIVE holder」只是「那个 pid 还活着」，可以覆盖
拿不到锁 ⇒ 有人正在执行 ⇒ 拒绝（这就是 criterion ②，并发仍被挡住）
```

## 3. 落地的形状（代码在哪儿）

| 位置 | 作用 |
|---|---|
| `cmd/agent/run_lock_resume_seam.go` | `resumeTakeRunLock(progName, rawLogPath) (release func(), arbitrated bool, err error)` |
| `cmd/agent/cli_resume.go` | `resumeDecideWithLock(taskID, arbitrated)` —— **两件事相遇的唯一一处** |
| 同上 | `resumeReportHolderOverride` —— 放行时**播报**被覆盖的 pid 与证据 |

**顺序就是修复本身。** 直到 2026-11-05，闸在**拿锁之前**回答，于是较弱的那次
测量先拒绝，锁根本没机会说话。现在锁先拿，闸读**已经仲裁过的**结论。

`arbitrated` 这个第二返回值是承重的（Rule 15）：

- `true` —— 内核把这个目录判给了本进程，LIVE 声明被覆盖；
- `false` —— 检查点不在本 profile 的任何 run 目录里（一份导出的 raw log），
  **内核什么都没说**，LIVE 声明保持拒绝。**「没有测量」不是「可以放行」。**

## 4. 没有被改动的两个闸

- **UNKNOWN / foreign holder**：外主机的 run 目录**不是本进程能锁的**，锁对它
  没有任何话说 → 照旧拒绝。
- **FINISHED 状态闸**（`completed` / `verified` / `superseded` → `gopi task clone`）：
  锁回答「有没有在执行」，从不回答「活是不是干完了」→ 照旧拒绝。

## 5. criterion ② 为什么没有被削弱

放行的**唯一**路径是「本进程此刻正持有这个目录的 flock」。第二个并发调用
**根本到不了 `resumeDecideWithLock`** —— `resumeTakeRunLock` 在 `flock` 那一步
就被 `runlock.Lock` 以 `ErrHeld` 拒绝，并把锁文件路径和持有者印出来。

两条实测（本 run，sandbox 私有 profile，从不碰操作员账本）：

```
# ① 放行：LIVE holder（pid 3785079）但目录没在执行
$ GOPI_HOME=$SB /tmp/gopi-probe session resume 01a1ac12-...-0deadbeef003 --task ...-0deadbeef003
EXIT=1   # 失败在"checkpoint 无消息"，不是被闸挡
stderr:
  gopi session resume: run lock held: .../sessions/01a1ac12-...-0deadbeef003/run.lock (pid 3804765)
  gopi session resume: task 01a1ac12-...-0deadbeef003 records a LIVE holder (pid 3785079) and resuming
    anyway — this process now holds the run-scope lock on its run directory, ...

# ② 拒绝：另一个进程真持有那把锁
$ flock $LOCK -c 'echo HOLDER-HAS-LOCK pid=$$; sleep 20' &
$ GOPI_HOME=$SB /tmp/gopi-probe session resume ...-0deadbeef003 --task ...-0deadbeef003
EXIT=1
stderr:
  gopi session resume: refusing to resume 01a1ac12-...-0deadbeef003.raw.log: run directory is already
    locked by another process: "...run.lock" (held by pid 3804765, startticks 179560858, ...)
```

## 6. 缝隙：仍然没有被测到的东西（诚实清单）

1. **refusal 文本里的 pid 来自 lock 文件的旧标记，不是真实持有者。** 上面 ② 的
   输出里 `held by pid 3804765` 是**上一次** resume 写下的 stamp（那次 resume 早已
   退出），而真正持锁的是 `3809448`。也就是说：**「谁在持锁」这个事实是对的
   （flock 不会骗人），「持锁的是哪个 pid」这个播报是陈旧的。**
   它是**另一行的职责** —— 并行行 `01a1ac0f-3875-7838-a677-c32038e5b7f8` 正在修
   `run.lock` 的 pid 与任务行 holder 身份不一致、死 pid 上报这一层。
   **本行修的是判据换代（闸读谁），没有修播报的新鲜度。**
2. **外来 holder 的锁永远是「没仲裁」。** 另一台机器上的 run 目录本进程锁不到，
   所以那里的 LIVE 声明只能保持拒绝 —— 这是对的，但也意味着**跨机的 stale holder
   仍然无法被这条路径救回来**，仍需人工清 stamp。
3. **锁的粒度是 run 目录，不是提交范围。** 两个进程争同一个 row 必然互斥；但两个
   进程在同一 repo 的不同 row 上并行仍然可能撞文件 —— 那本来就是 merge 阶段的
   问题，不是这把锁的事。
4. **`arbitrated=false` 的形状仍是一条「继续而不锁」的路径**（导出 raw log 的
   resume）。它一直是那样，本行没有收窄它，只是让它对 LIVE 声明保持拒绝。
