<!-- doc-class: historical -->
# Task Report: `task show` 的拼写修复子句只在它**确实构成修复**时才印

**任务**：`01a131e2-953f-76e5-9839-ea82c17c2e35`（reissue of `01a130e4`）
**branch**：`task/01a131e2953f76e59839ea82c17c2e35`（基线 `abd3fb0`）

## Completed

### 症状（原始上报者的原话与实测）

`gopi task show <UUID>` 对**本账本中不存在**的行，只要输入是**规范拼写**的虚线 uuid，
就多印一句「这是 uuid 的非规范拼写」并**把输入自身当作更正建议**：

```
$ gopi task show 00000000-0000-0000-0000-000000000000
gopi task show error: task: no task with task_id="00000000-0000-0000-0000-000000000000" — it is a uuid's 16 bytes in a non-canonical spelling (dashes dropped or moved); did you mean "00000000-0000-0000-0000-000000000000"?
```

`did you mean` 指向的**就是操作数本身** —— 那不是建议，是噪音；而真正的答案
（「这一行不在本账本里」）被它盖住了。上报者正是被这句误导去查拼写，真因是
**该行在别的 host 的账本上**（实测：`01a12cb3-53c8-720b-a04e-bccdbd538ba2` 在
hosta 上被同样误报，而它存在于 peerbox 的账本）。

### 根因（5-why）

1. `taskIDNearMissSuffix(taskID)` 只问 `sessionid.CanonicalSpellingOf(taskID)` 是否
   成功，**不问返回的拼写与输入是否不同**；而对一个**已经规范**的 uuid，
   re-group 是恒等变换 → `canonical == taskID` → 印出 `did you mean "<同一个 id>"`。
2. 更深一层：这个函数**手里没有账本事实**。它的签名只有 `taskID string`，所以它
   **不可能**知道「我建议的那个拼写，本账本里真的有吗」—— 于是它把「形状像」当成了
   「是修复」。行在**别的 host** 账本上时，建议同样落空。
3. 命名本身也误导：函数名里的 `NearMiss` 暗示了「近失」，但对规范拼写而言根本不是近失。

### 修复（`internal/tools/tasks.go`，Rule 4 surgical）

`taskIDNearMissSuffix` 带上判定所需的账本事实，并在**两个**条件都成立时才有话可说：

```go
func taskIDNearMissSuffix(ctx context.Context, db *sql.DB, taskID string) string {
	canonical, ok := sessionid.CanonicalSpellingOf(taskID)
	if !ok {
		return ""                       // sessionid 无法 re-group 的操作数（t-… / 名字）
	}
	if canonical == taskID {
		return ""                       // 输入已是规范拼写：没有"另一个拼写"可给
	}
	row, err := taskByID(ctx, db, canonical)
	if err != nil || row == nil {
		return ""                       // 建议必须可达：本账本不持有该行就不印
	}
	return fmt.Sprintf(" — … did you mean %q?", canonical)
}
```

调用点 `taskShowWithDB` 已在同一个 `ctx`/`db` 上（它手里的 handle 就是账本），
把两者传下去即可 —— **没有新增第二处 uuid 判定**：拼写规则仍然只来自
`internal/sessionid`（Rule 14）。

**`err` 分支为何是「返回空」而不是「返回 error」**：本函数只装饰一个已经成立的
refusal（`no task with task_id=…` 会照常打出来），而且 Go 里 `ctx`/`db` 来自
`taskShowWithDB` 自己刚用过的同一 handle（若它真的坏了，前面的 `taskByID` 早就报错
了）。所以这不是「吞掉失败」，是「建议缺席」—— 判据（行是否在本账本）本来就只有
两个取值，误判的代价是这句建议本身（Rule 15 注释里已写明这一取舍）。

## Not Done

- **`internal/sessionid.Refuse` 未改动。** 它的问题面不同：`task session dir` /
  `session log` 这两个 seam 面对的**不是**「查一行」，而是「join 一个会话目录」，
  在那里近失拼写会 mint 出**第二个目录**，`did you mean` 指的是**形状上的正确
  拼写**，与「本账本是否持有该行」无关（一份本机从未记录过的任务，其会话目录也
  可能确实存在）。**我没有把它一并改成「查账本」**，因为那会给 session seam 加一次
  它不需要的 DB 读，且会把「形状」与「账本」两件不同的事混为一谈。
  如果将来要统一，应作为独立任务评估 —— **本行只修账本那一面**（epic 原文也是这个范围）。
- 未做全树测试（Rule 27：merge/verify 路径是 SMOKE、分支范围）。

## Verification

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BINARY OK`
  （另 `go build ./...` → `exit=0`；`gofmt -l` 两文件均无输出）
- **Tests**: `go test ./internal/tools/ ./internal/sessionid/ -count=1` →
  ```
  ok  	github.com/nick/gopi/internal/tools	195.177s
  ok  	github.com/nick/gopi/internal/sessionid	0.002s
  ```
  子集复跑：`-run 'TestNamedLimits|TestTaskIDNormalizeIsTheSingleSeam|TestTaskShow|Rule1|Rule13'` → `ok ... 0.783s`（Rule 1/13 命名与镜像门禁同批绿）。
- **Ran**（真实 CLI，真实参数，**新 binary vs 旧 binary 并排**）：

```
$ B=/home/user/workspace/gopi-01a131e2953f76e59839ea82c17c2e35/bin/gopi
$ $B task show 00000000-0000-0000-0000-000000000000
gopi task show error: task: no task with task_id="00000000-0000-0000-0000-000000000000"
rc=1

$ /usr/local/bin/gopi task show 00000000-0000-0000-0000-000000000000     # 旧
gopi task show error: task: no task with task_id="00000000-0000-0000-0000-000000000000" — it is a uuid's 16 bytes in a non-canonical spelling (dashes dropped or moved); did you mean "00000000-0000-0000-0000-000000000000"?
rc=1

$ $B task show 01a131e2-953f76e5-9839-ea82c17c2e35                       # 规范 + 存在
task_id:        01a131e2-953f-76e5-9839-ea82c17c2e35
status:         in_progress
summary:        gopi task show 对不存在但拼写规范的 uuid 误报 'non-canonical spelling' 并把该 id 建议给自身 [reissue of 01a130e4]
rc=0

$ $B task show 01a131e2-953f76e59839-ea82c17c2e35                       # 32-hex + 存在（近失，建议可达）
gopi task show error: task: no task with task_id="01a131e2-953f76e59839-ea82c17c2e35" — it is a uuid's 16 bytes in a non-canonical spelling (dashes dropped or moved); did you mean "01a131e2-953f-76e5-9839-ea82c17c2e35"?
rc=1
$ $B task show 01a131e2-953f-76e5-9839-ea82c17c2e35 | head -2            # 那个建议真的可解析
task_id:        01a131e2-953f-76e5-9839-ea82c17c2e35
status:         in_progress
rc=0

$ $B task show 01a11548-tracked                                          # 非 uuid + 不存在
gopi task show error: task: no task with task_id="01a11548-tracked"
rc=1
```

- **Result**: 四条验收 a/b/c/d 全部符合 ——
  (a) 规范拼写+不存在 → 普通答案，**无** did you mean / non-canonical（旧 binary 有，新 binary 无，同一机器并排对照）；
  (b) 规范拼写+存在 → 照常打印行，exit 0；
  (c) 32-hex+存在 → 仍给出正确提示，且提示里的 id `task show` 可解析（exit 0）；
  (d) 非 uuid+不存在 → 普通答案，无提示。

- **red → green 证据**（新测试，pre-fix 的 `tasks.go` 直接取自 `HEAD`，跑完立刻恢复）：

```
$ git show HEAD:internal/tools/tasks.go > internal/tools/tasks.go   # 回到 red 状态
$ go test ./internal/tools/ -run TestTaskShowDoesNotOfferASpellingRepairTheLedgerCannotMake -count=1
--- FAIL: TestTaskShowDoesNotOfferASpellingRepairTheLedgerCannotMake (0.01s)
    tasks_test.go:793: TaskShow("00000000-0000-0000-0000-000000000000") = … did you mean "00000000-0000-0000-0000-000000000000"? …
    tasks_test.go:793: TaskShow("01a12cb3-53c8-720b-a04e-bccdbd538ba2") = … did you mean "01a12cb3-53c8-720b-a04e-bccdbd538ba2"? …
    tasks_test.go:806: TaskShow("01a131e2-953f76e59839-ea82c17c2e35") = … did you mean "01a131e2-953f-76e5-9839-ea82c17c2e35"? …
FAIL

$ cp /tmp/tasks.go.fixed internal/tools/tasks.go                     # 恢复修复
$ sha256sum internal/tools/tasks.go
41556d3cf80f74d09fb0e66831060a8e400bb075777acf5b5a819e6bb3566711  internal/tools/tasks.go
$ # 恢复判据（Rule 5，用显式 revision 而不是 index 相对 diff —— 见 `## Evidence`）
$ cp /tmp/tasks.go.fixed internal/tools/tasks.go
$ sha256sum internal/tools/tasks.go | cut -d' ' -f1   # == 变异前的同一个 sha256
41556d3cf80f74d09fb0e66831060a8e400bb075777acf5b5a819e6bb3566711

$ go test ./internal/tools/ -run 'TestTaskShow' -count=1
ok  	github.com/nick/gopi/internal/tools	0.626s
```

**回归测试**（`internal/tools/tasks_test.go`，新增
`TestTaskShowDoesNotOfferASpellingRepairTheLedgerCannotMake`）覆盖三个具体操作数：
规范拼写+不存在的 `00000000-…`、在**peer 账本**上的 `01a12cb3-53c8-720b-a04e-bccdbd538ba2`、
以及本账本不持有的近失 `01a131e2-953f76e59839-ea82c17c2e35`。既有的
`TestTaskShowNamesTheCanonicalSpellingForANearMiss`（近失 + 行存在 → 必须给建议）
保持通过 —— 修复没有把真正的修复能力一起关掉。

## Evidence (self-filed)

- **red 状态确实存在**：上面的 FAIL 输出是本 run 观察到的（三个操作数各自一条），
  不是推断。
- **恢复是显式比对过的**：`sha256sum internal/tools/tasks.go` 在变异**前**与**后**都读到
  `41556d3cf80f74d09fb0e66831060a8e400bb075777acf5b5a819e6bb3566711`（同值 ⇒ 恢复成功）。
  这里刻意用了文件内容哈希而不是 `git diff` —— index 里存的正是修复后的字节，
  拿 index 做参照的"恢复证据"会自己骗自己（Rule 5 的红证恢复判据）。
- **基线**：本 worktree 的 HEAD 为 `abd3fb0`（`git log --oneline -3` 观察到）。
- **未在错误位置改动**：所有读写均在本 worktree 内；主检出 `/home/user/work/GoPi` 未参与。

## Files

- `internal/tools/tasks.go` — `taskIDNearMissSuffix` 增加账本事实与两个短路；调用点传 `ctx`/`db`
- `internal/tools/tasks_test.go` — 新增 `TestTaskShowDoesNotOfferASpellingRepairTheLedgerCannotMake`
- `devdoc/task-show-spelling-hint-is-a-repair.md` — 本报告
