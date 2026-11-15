<!-- doc-class: normative -->
# 一条 task 行只有一个 ID（task-run 身份统一）

## 症状（实测，本机 2026-10-27）

一次 `task run` 的四个"身份"互相不等：

| 值 | 改之前 | 改之后 |
|---|---|---|
| run 目录名 | `<task-id>` | `<task-id>` |
| 记录文件名 | `<minted-uuid>.jsonl` | `<task-id>.jsonl` |
| 记录里的 `session_start.sessionId` | `<minted-uuid>` | `<task-id>` |
| 账本 `session_id` 列 | `<minted-uuid>` | `<task-id>` |

证据（本 run 自己的目录，未改动前就在盘上）：

```
$ ls ~/.gopi/sessions/01a17de9-e772-7470-80c1-f954f07c6b59/
df8d3009-33a8-45b1-bd99-a7212fbe2b7a.jsonl
df8d3009-33a8-45b1-bd99-a7212fbe2b7a.raw.log
metadata.json
output.log
attachments/
```

目录说 task-id，文件说 uuid。后果是操作员手上**唯一**的定位符（task id）
`gopi session log <task-id>` / `--resume <task-id>` 找不到文件——只有知道那个
从来没被人打印过的 uuid 才行。

## 根因

`internal/agent/loop.go` 的 `NewAgentResuming` 无条件 `newUUIDv4()` 铸 run id，
而 run 目录名另有来源（`sessionDirForID`，取 `GOPI_TASK_WRAPPER_ID`）。两个
来源各自正确、合起来就是一次运行两个身份。`9d71ad8` 修的是"续写既有记录"
（不再另建一个），没有统一布局。

## 改法（四点，见提交 `eea016d`）

1. `internal/agent/session.go` — `mintRunID(taskID)`：**唯一**决定"这次运行写
   在哪个 id 下"的地方。task run 子进程（`GOPI_TASK_WRAPPER_ID` 是 admissible
   的 id）→ 用 task id；裸 prompt / chat（无 wrapper）→ 仍铸 uuid。
   不可接受的 id **不改编**，交给 `sessionDirForID` 按名拒绝。
2. `internal/agent/rawlog.go` — `newRawLoggerInTaskRunDir`：task run 的 raw log
   改 append-or-create。原因不是风格：id 统一后，同一行的**第二次运行必然**
   落在同一个文件名上，`O_EXCL` 会让它静默失去 wire log。判据**不是字节数**
   （bug `01a190ba`）：`fi.Size() == 0` 只会告诉你"文件里有字节"，不会告诉你
   "本行的先前一次运行开过它"，而把所有非空文件都判成续写，就把
   `resumedFrom=<本 run 自己的 session id>` 写了进去 —— 一次运行声称续写了它
   自己。现在的见证是 `rawLogPriorRunOf` → `rawLogHeaderNamesSession`：文件里
   已有**指名本 session 的 header 行**＝先前运行开过它（写 resume 头，只声明
   "续写"，不编造续写来源）；没有任何可识别 header 的非空文件是"来历不明"，
   同样不编造来源；只有空文件才写 created 头。`rawLogResumeSource` 是"续写来源
   永不得等于正在写的 session"的唯一判据，两个写入者共用。
3. `internal/sessionlayout` — `NewestRecordInDir(dir)`：**唯一**回答"这个 run
   目录代表哪条记录"。`internal/tools` 与 `internal/gopilog` 过去各写一份循环
   （Rule 13/14）。
4. `internal/gopilog/reader.go` — `exactSessionPath` 在 id 匹配失败后回退到
   **操作数自己的 run 目录**（`runDirRecordForID`）。判据是**目录**，不是文件名，
   所以旧布局（`<uuid>.jsonl`，文件名永远不等于操作数）与新布局（`<task-id>.jsonl`）
   一条规则同时覆盖，且不需要走全树扫描。空目录仍是 miss。

**账本列不用改写入逻辑**：`taskUpdate` 的延迟修复腿经由
`taskRunSessionFromRowDir` 读该目录的最新记录，布局统一后它自动得到 task id。

## 兼容性

- 盘上已有的 `<task-id>/<uuid>.jsonl` 全部继续可读，`taskRunSessionFromRowDir`
  对它们仍回旧 uuid（列的含义不因这次改动漂移）。
- `gopi chat` 与裸 prompt 完全不受影响：无 wrapper env → uuid，目录
  `sessions/<uuid>/session.jsonl`（有 pin 测试）。

## 验证（真实执行，见 task-report 的 `Ran:`/`Result:`）

- fresh `task run` 子进程 → 四值相同，`session log <task-id>` exit 0。
- 旧布局目录（手工种一条 uuid 记录）→ `session log <task-id>` exit 0 并显示
  该 uuid。
- 目录被移走后 → `--session <task-id>` exit 1，拒绝消息**按名指名**操作数与
  查找根，且不新建任何 session 目录。
- 同一行第二次运行 → 同一文件续写（记录里两段 `session_start`，raw log 出现
  resume 头），sessions 根下仍只有一个目录。
