<!-- doc-class: normative -->
# 给正在运行的 worker 任务补发权威资料 — `gopi notify --session`

**面**：`gopi chat`（PA 面） · **日期**：2026-10-13 · **来源 session**：`953a917d-9de5-4f7f-bd78-d0248ed4346d`

**这份文档回答什么。** 一个任务已经被人认领、正在写代码，此时你（PA）拿到了一份**权威资料**
或者发现它的 briefing 里有一处**错误前提**。你要在**不打断它、不另提一条任务返工**的前提下
把资料送到它手上。这条路存在，只有一条命令，而它有一个会让消息**静默消失**的坑 ——
本文给方法、给坑、给判据。

---

## 0. 一条命令

```bash
gopi notify --session <SESSION-目录名> --message "<全文>"
```

投递是**持久**的：消息被 append 到 `<session-dir>/messages/inbox.jsonl`，**即使那个 session
当时没在跑也留着**；worker 在**下一个 round 边界**取走（同一个取用点，没有第二条队列）。
**正在飞的 round 不会被中断。** 消息可以很长 —— 实测 3.7 KB 全文落盘正常。

**验证它收到没有**：`tail` 那个任务的 `output.log`，看 `[thinking]` 有没有引用消息内容。

---

## 1. `--session` 传什么 — **这一步是坑**

**必须传 session 目录名。在 worker 任务里，那个目录名就是 task id。**

| 传什么 | 结果 |
|---|---|
| `<task-id>`（= session 目录名） | ✅ 落 `<root>/<task-id>/messages/inbox.jsonl`，回报结尾 `(resolved as its session directory name)` |
| `<run-uuid>`（`gopi task show` 的 `session_log` 文件名） | ❌ **静默失效**，见 §2 |

**`gopi task show` 的 `session_log:` 那一行给的是 run id**（`<task-id>/<run-uuid>.jsonl`），
**它不是 session 目录名** —— 而这是最容易照抄的那个值。踩中一次 = 通知静默消失。

---

## 2. 踩中的后果：静默失败（Rule 15）

```
$ gopi notify --session d1495e9e-c14b-4ab0-9b54-549e9000e623 --message "…"
queued: message daf1ae8e100af2a8 for session d1495e9e-c14b-4ab0-9b54-549e9000e623
inbox:  /home/user/.gopi/sessions/messages/inbox.jsonl              ← 无人读
session dir: /home/user/.gopi/sessions (resolved as a session record with that id)
EXIT=0                                                              ← 报成功
```

- 退出码 **0**，打印 `queued`
- 落进 `<root>/messages/inbox.jsonl` —— **sessions 根目录，不是任何 session 的目录**
- 读取路径是 `<session-dir>/messages/inbox.jsonl`
  （`internal/tools/session_message.go` `sessionInboxPath`），所以**没有任何 session 会读它**
- 回报的 `resolved as a session record with that id` **读起来像成功了**

### 根因（`cmd/agent/cli_notify.go` `resolveNotifySessionDir`）

`gopilog.FindSession` 按 run id 命中 `<root>/<task-id>/<run-uuid>.jsonl` 之后，
`dir = filepath.Dir(path)` **已经就是容器目录** `<root>/<task-id>`，但紧接着：

```go
if filepath.Base(path) != sessionlayout.RecordName {   // "….jsonl" != "session.jsonl"
    dir = filepath.Dir(dir)                            // → <root>   ✗ 多剥一层
}
```

**这个判据无法区分扁平布局与 task-run 布局** —— 两者 basename 都不等于 `session.jsonl`。
扁平布局（`<root>/<id>.jsonl`）下多剥一层恰好得到 `root`，**碰巧是对的**，bug 就藏在这个巧合里。

**已提 bug**：`01a1330e-dfe3-7130-8ddf-f0aa73f5a274`（tags=bug）。

### 判据 — **别拿回执当判据**

1. 看回报 `session dir:` 之后是不是 `<sessions>/<task-id>`，结尾是不是
   `resolved as its session directory name`；
2. 用**文件存在性**复核：

```bash
ls -la ~/.gopi/sessions/<task-id>/messages/inbox.jsonl
```

---

## 3. 消息怎么写 — 四段式

1. **纠正了什么** —— briefing 里若有错，第一句就说清，别让执行者踩着错前提往下走。
2. **关键事实/参数** —— 表格化，带出处（文档页码 / 资源 id / 实测 logid）。
3. **要求它在报告里明确回答的问题**（哪怕选择不做）—— 5 个左右，逼出**决策**而不是沉默。
4. **重申不变的验收要求** —— `Ran:` 必须真跑、包级测试行不许用 `-run` 过滤充数、
   `git status --porcelain` 带自己的观测、补 `## Files`。

**第 3 段是把"我告诉了它"变成"它必须表态"的那一步。**

---

## 4. 时机：为什么这次值

worker 的推演顺序是 **读文件 → 定接缝设计 → 写代码**。消息在它**动手写代码之前**到达
（`output.log` 的 `[thinking]` 明证）：

> "The PA sent an authoritative doc. Key additions: `enable_nonstream`,
> `end_window_size`, `result_type`, `force_to_speech_time`. I must decide on these
> and document them in constants + report."

**送达点是 round 边界** —— 只要它还没写完代码就是及时；写完了则变成一次返工指令，
仍可接受，但不如前者。

---

## 5. 与"另提一条任务"的取舍

| | `notify` 补发 | 另提一条任务 |
|---|---|---|
| 打断执行者 | 不打断，round 边界取走 | 要它自己发现并放弃当前方向 |
| 成本 | 一条命令 | 一个 worktree + 一次模型运行 + 一次合并 |
| 适用 | **给在跑的那条补资料 / 纠正前提** | **工作确实需要独立的一次执行** |

**与 Rule 26 不冲突**：`notify` 是"给在跑的这条补料"，Rule 26 是"这条修不完，另派一条补"。
**先用 notify，notify 不够再派。**

---

## 6. 附带机制（同一现场发现）

- **`gopi task show` 里有两个 id**：`task_id`（= session 目录名）与 `session_log`
  的文件名（**run uuid**）—— **不是同一个东西**，用错的地方不止 `notify`。
- **`task_submit` 自动串联**：一个会话里第 2 次及以后的提交会挂在前一次提交之下。
  一条**独立**的 bug 夹在中间提交时会被 **park**（等父行终态），需解挂：

  ```bash
  gopi task update <task-id> --clear-parent --reason "…"
  ```

- **同会话 `memory(action=search)` 会命中自己刚 `store` 的记录**，系统以
  `degradation: note-self-echo` 拒绝回灌 —— 同会话 search 自己的记忆**不构成检索**。

---

## Verification

- **Ran（真跑，非 `-h`）**：两次 `gopi notify`，一次传 run id（错）、一次传 task id（对），
  输出与落点见 §1/§2；对照 `ls -la ~/.gopi/sessions/<task-id>/messages/`（3960 B inbox 存在）
  与 `ls -la ~/.gopi/sessions/messages/`（错投的那条）。
- **接收确认**：`grep -n "PA sent" <task>/output.log` → 命中 worker 的 `[thinking]` 原文（§4 引用）。
- **反例**：传 run id 时 exit 0 且打印 `queued`，落点无人读 → 这是本页存在的理由。
- **bug 行**：`01a1330e-dfe3-7130-8ddf-f0aa73f5a274`。
- 取证原件：`~/.gopi/sessions/953a917d-9de5-4f7f-bd78-d0248ed4346d/artifacts/notify-session-runid-silent-miss.md`
