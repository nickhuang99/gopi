<!-- doc-class: historical -->
# Task Report: notify `cmd` 投递缺行终止符 —— 生产者侧修帧（task 01a0eec9）

**Branch:** `task/01a0eec9416f7ef69e55e4d5f1ac1a71` · worktree `/home/user/workspace/gopi-01a0eec9416f7ef69e55e4d5f1ac1a71` · commit `e2b7e379`（+ 本轮复核补笔，见 §6）

## Completed

### 1. 根因（一处咽喉，不是一个接收端的毛病）

`internal/tools/tasknotify.go` 的 `deliverTaskNotify` 里 `json.Marshal` 产出的**裸 JSON 字节**被
直接当作 `sh -c` 的 stdin（`cmd.Stdin = bytes.NewReader(body)`）。文档推荐的接收端配置是
`notify.cmd = "cat >> ~/.gopi/notify/task-events.jsonl"`，于是每条一次 append、没有任何行终止符，
若干条事件在文件里首尾相连成 `}{`。实测操作者历史文件：51,737,592 字节、135,167 个事件、**全文只有 1 个换行**。

这不是"`.jsonl` 这个接收端写坏了"——**同一份 `body` 同时喂给 webhook 和 cmd**。webhook 是 HTTP 请求体，
本就不该有行终止符；把修复放在接收端（在操作者的 `cmd` 里加 `echo`）等于要求每一位操作者为每一条自定义
命令各修一次。**帧由生产者负责**：一个函数保证一次，覆盖全部接收端。

### 2. 修复

```go
// internal/tools/tasknotify.go
taskNotifyLineTerminator = '\n'   // 命名常量（Rule 1）

func taskNotifyCmdStdin(body []byte) []byte {
	framed := make([]byte, 0, len(body)+1)
	framed = append(framed, bytes.TrimRight(body, "\n")...)
	return append(framed, taskNotifyLineTerminator)
}
```

`cmd.Stdin = bytes.NewReader(taskNotifyCmdStdin(body))` —— 唯一的调用点。

- **幂等**：先剥掉全部尾部 `\n` 再补**恰好一个**。调用方自带换行不会多出空行；`json.Marshal` 的输出
  （本就不以换行结尾）字节不变，只是多一个 `\n`。
- **JSON 内容不变**：只动行末，不重新序列化。
- **webhook 字节不变**：webhook 走的是原来的 `body`，未经过这个函数（有测试钉住）。

### 3. 回归测试（红→绿可证）

| 文件 | 内容 |
|---|---|
| `internal/tools/tasknotify_lineframe_test.go` | 红证夹具：连发 2 条 → 数换行符、禁空行、末行必须终止、逐行 `json.Unmarshal`；另有 webhook/cmd 同源字节断言（webhook 无 `\n`，cmd = webhook + 一个 `\n`） |
| `internal/tools/tasknotify_lineframe_unit_test.go` | `taskNotifyCmdStdin` 的三例幂等表测（无换行/已有换行/多个换行） |

夹具命令是**真实的文档配置形状** `cat >> FILE`（不加 `&& echo` 补丁——补丁会把缺陷藏起来）。

### 4. 既有消费者逐条排查（依赖错误行为 / 被 bug 蒙住）

| 消费者 | 读法 | 结论 |
|---|---|---|
| `internal/tools/tasknotify_test.go:33` `readNotifyLog` | 按 `\n` split + 逐行 `json.Unmarshal` | **被 bug 蒙住**：按行读是正确意图，但夹具的 cmd 是 `cat >> log && echo >> log`（自带补丁），于是夹具永远看不到真实接收端的形状。★ 这条夹具正是操作者历史文件坏掉而套件全绿的机制——**测试自带了生产端没有的修复**。保留不改（它测的是 payload 内容），缺陷由新夹具承担。 |
| `internal/tools/tasknotify_test.go:429,437` | `strings.Contains(body, …)` 子串匹配 | **被 bug 蒙住**：整文件匹配（count 恒为 1，却是"命中/未命中"语义），不依赖行结构，修复后语义不变。 |
| `cmd/agent/cli_tasks_list_sweep_readpath_test.go:181` | `strings.Contains(notifyLog, …)` | **被 bug 蒙住**：同上，存在性判断，与行结构无关。 |
| `scripts/acceptance-zombie-auto-dispose.sh:123` | `tail -c +N "$LIVE_SINK" \| grep -q` | **依赖错误行为 → 现已误报**。它只读本次运行**新增的字节区间**：修复前该区间整段被行尾折断，`grep -q` 几乎必然命中；修复后模式被换行劈开，脚本会走 else 分支打印
  「operator sink grew only from concurrent sessions」。**方向安全**（只会误报靠后一种，不会漏报泄漏），但这句话现在是**假的**。 |
| `scripts/materials/materials-gen-env-notify.sh:252-264` | 抓包文件 `cmd-out.jsonl`，注释称"一行 JSON payload" | **依赖错误行为**：抓包命令是 `cat >> CAP`，注释把"一条事件"当成"一行"，全文件只有一行。修正后每条事件落成独立一行，注释描述才成立（该脚本本次未改，属交付面之外的既有素材生成器）。 |
| `devdoc/requirements/notifications.md:333-337` | `tr -cd '\n' \| wc -c` = 1、`wc -l` = 1 | **被 bug 蒙住，且是准确的诊断记录**：这是 FINDING 6.0 的原始测量，描述的是 bug 本身，不是读法错误。 |
| `devdoc/requirements/evidence/evidence-human-asks.md:16397,16407` | `grep -c "$id"` → 1；`grep -o '"event":"[a-z-]*"[^}]*"<task_id>"'` | **被 bug 蒙住（部分）**：`("event"…)` 条目每事件的 payload 里都带 task_id，修复前后**都**只有 1 个 `"event"` 字面量，结论成立。但 `grep -c "$id"` 的**单位不是行**：`grep -c` 数的是匹配**行**，整文件一行 ⇒ 无论一个 task_id 出现 1 次还是 5 次都得到 1。它靠巧合正确，**不能作为"某任务只投递了一条事件"的证据**。 |
| `devdoc/06-agent-loop` 报告（95,283 events / 49 MB）、`04-sync-and-nodes.md:551`、`task-notify-study-20260920.md:210` | 事件计数 | **被 bug 蒙住**：计数字面正确。这正是 bug 能活这么久的原因——`jq -c . file \| wc -l` 会**逐个解析首尾相连的 JSON 值**，给出正确的 2（我用 681 字节的 pre-fix sink 实测过：`jq … \| wc -l` = 2，而 `wc -l` = 0）。**按值计数的读法意外地没坏，按行读的全坏了**——所以"计数一直对得上"从不构成"文件是好的"的证据。 |
| `CONFIG.md:483` | 文档示例 `"cmd": "cat >> …/task-events.jsonl"` | **不是消费者**，是配置语义本身。**未改**（约束：不改 notify.cmd 配置语义）。 |

修复后实测的读法对照（681 字节、2 条事件的 pre-fix sink → post-fix sink）：

| 读法 | pre-fix | post-fix |
|---|---|---|
| `wc -l` | 0 | 2 |
| `head -1 \| wc -c` | 681（整文件） | 353（一条事件） |
| `tail -1 \| wc -c` | 681 | 354 |
| `while IFS= read -r _` 迭代数 | 0 | 2 |
| `grep -c '"event"'` | 1 | 2 |
| `jq -c . \| wc -l` | 2 | 2 ← **jq 按值解析，从来没错过** |

### 5. 历史文件的只读安全解析配方

操作者的 `~/.gopi/notify/task-events.jsonl`（51 MB / 135,167 事件）**只读，未改动一个字节**
（测试沙箱在 `/dev/shm`，套件 TestMain 已钉 `GOPI_SETTINGS`，见下）。

**重要：它并不只是"少一个换行"。** 全文只有 1 个换行（在文件中部），另有**一处真正的结构损坏**：
offset 33,935,609 起，一个 2026-09-12 的探针（`{"event":"probe","task_id":"probe-1"}`）带着**被转义的引号**
内联在一条 `stale` 事件的 `zombie` 字段里，把该条事件的尾部吃掉了 —— 这就是 `jq` 报
`parse error: Invalid numeric literal at line 1, column 33994549` 的真正原因，**与缺换行无关**
（该损坏区间在 UTF-8 里对应字节偏移 ≈ 33,994,546；jq 报的 33,994,549 落在同一区间的开头，
两者差 3 字节是在 `{` 之后又吃掉了 `"event"` 才判定失败——**同一处损坏**，不是两处）。
所以直连解析的读法最多只能取到 92,134 条就停住。

**损坏容忍的顺序解析（只读，实测恢复 135,167 条，跳过 1 处损坏）：**

```python
import json
dec = json.JSONDecoder()
data = open(PATH, encoding="utf-8").read()
i = n = skipped = 0
while True:
    while i < len(data) and data[i] in " \t\r\n":
        i += 1
    if i >= len(data):
        break
    try:
        obj, j = dec.raw_decode(data, i)      # 从任意位置解析一个完整的 JSON 值
    except ValueError:
        j = data.find('{"event"', i + 1)      # 损坏区间跳到下一条事件的起点（不是下一个字节）
        if j < 0:
            break
        skipped += 1
        i = j
        continue
    n += 1
    i = j                                    # 事件与事件之间不需要换行
```

要点：`raw_decode` 逐值解析（不依赖行结构）；**损坏时跳到下一个 `{"event"` 锚点**，而不是跳一个字节——
后者会在损坏区里逐字节重试。**不要**用 `jq -c . file | wc -l` 当"文件健康"的判据（它按值解析，
pre-fix 也给出正确计数），也不要用 `grep -c` 数事件（数的是行）。

实测事件直方图：`completed 43436 / in_progress 37607 / claimed 30862 / failed 21722 / stale 1499 / build-failed 41`。

## Not Done

- **既有消费者只做了排查结论，未改任何一份**（除本任务新增的测试）。上面标为"依赖错误行为 → 现已误报"的
  `scripts/acceptance-zombie-auto-dispose.sh:123` 与 `materials-gen-env-notify.sh` 的注释**留给各自的
  所有者**：前者是既有验收脚本，改它需要跑它的完整验收流程；后者是素材生成器（会重建大量产物）。
  两处都不影响本次修复的正确性，且已在报告里点名，可直接派单。
- **未重建操作者的历史文件**（明确禁止）。历史的那 1 行 + 1 处损坏保持原样；从修复落地这一刻起，
  新事件才是逐行落盘的。
- **未做 `devdoc/requirements/notifications.md` 的 FINDING 6.0 复测**（该文件属 requirements 语料，
  复测需另派任务）。
- **预存失败 `TestTaskIDWhitespaceNormalizedAllAccessors` 未修**（`internal/tools`）：在 master 的 pre-fix
  detached worktree 上同样 FAIL，逐字相同（`sess-ws-2` 在沙箱 profile 里没有 session 目录），与本次 diff 无关。
  只登记，不冒充已修。

## Verification

- **Build:** `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK（`sha256 3795c7b95acd6b40…`，post-fix）
- **Tests:** `go test ./internal/tools/ -run 'TestTaskNotifyCmdSinkIsLineFramed|TestTaskNotifyCmdStdinFraming|TestTaskNotifyTargetsShareOneJSONBody' -count=1`
  → **3 PASS**（红证树同一条命令 **FAIL**，见下）；`go test ./internal/tools/ -run 'Notify|notify' -count=1` → **ok 3.133s**；
  `go test ./internal/standards/ -count=1` → **ok 28.122s**（Rule 16–21 六道闸门）
- **分支范围冒烟（本轮实测）**：`GOPI_TEST_TIMEOUT=45m scripts/test-budget.sh ./internal/tools/`
  → `elapsed=163s exit=1 outcome=fail`，**唯一失败是预存缺陷** `TestTaskIDWhitespaceNormalizedAllAccessors`
  （master 的 pre-fix detached worktree 上逐字相同地 FAIL → 与本次 diff 无关，见 Not Done）。
  跨三包的 `make test-budget PKGS='… ./cmd/agent/'` 首次运行是 **budget 到期**
  （`budget=25m elapsed=1506s outcome=timeout`，非断言失败），已按提示收窄为单包重跑。
- **Ran:**（Rule 31，真实二进制 + 真实 `wc -l` + 真实逐行解析，`GOPI_HOME`/`GOPI_SETTINGS` 指 `/dev/shm` 沙箱，
  notify 走 `settings.json` 的 `notify.cmd`——不是 env，即真实的文档配置路径）
  ```
  ./bin/gopi task submit "e2e line framing probe" --payload p
  ./bin/gopi task update 01a0eed9-681f-7685-9021-e1e1aef0dd6d --status in_progress
  ./bin/gopi task update 01a0eed9-681f-7685-9021-e1e1aef0dd6d --status completed
  wc -l < sink.jsonl                                  # → 2      （pre-fix 同一路径 → 0）
  while IFS= read -r line; do printf '%s' "$line" | jq -c '{event,status,task_id}'; done < sink.jsonl
  ```
- **Result:** 逐行解析实际输出（每行独立可解）：
  ```
  {"event":"in_progress","status":"in_progress","task_id":"01a0eed9-681f-7685-9021-e1e1aef0dd6d"}
  {"event":"completed","status":"completed","task_id":"01a0eed9-681f-7685-9021-e1e1aef0dd6d"}
  ```
  `jq -c . sink.jsonl | wc -l` → 2；`tail -1 sink.jsonl | jq -r .event` → `completed`；`read` 循环迭代 → 2。
- **Commit:** `devdoc/task-notify-cmd-line-framing.md`

## Evidence (self-filed)

**红证（Rule 5 的红→绿，真跑）**：`git worktree add --detach /home/user/workspace/gopi-redproof-01a0eec9 master`
（pre-fix 代码：`internal/tools/tasknotify.go:518 cmd.Stdin = bytes.NewReader(body)`），只把**新测试文件**
拷进去（新文件、零冲突——它证明的正是缺陷，不是复制了修复）。

**把两个新文件一起拷进去是错的**：`tasknotify_lineframe_unit_test.go` 直接调用修复才引入的
`taskNotifyCmdStdin`，在 pre-fix 树里是 `undefined` —— 那是**编译失败**（"套件编不过"），不是
"观察到缺陷"，两者证据等级不同。**红证只拷 SINK 那个文件**（`tasknotify_lineframe_test.go`，它走真实的
`TaskSubmit`/`TaskUpdate` → sh -c → 文件，不引用任何新符号），得到的是行为级 FAIL：
```
--- FAIL: TestTaskNotifyCmdSinkIsLineFramed
  sink holds 0 line terminators for 2 events — line-oriented readers (wc -l/jq/tail/read) misread it:
    "…"}{"event":"completed",…}"        ← 两条事件首尾相连，全文 0 个换行
RED_EXIT=1
```
同一条命令在修复树 → 3 PASS。**二进制级 A/B**：pre-fix `bin/gopi`（`sha256 87d030af34b9d811`）驱动同一组
命令 → `wc -l` = **0**；post-fix（`3795c7b95acd6b40`）→ `wc -l` = **2**。

**隔离（记忆 #2074/#2079）**：套件经 `testutil.PinSuiteForTestMain` 把 `GOPI_SETTINGS` 钉在空的 per-run
沙箱路径上（不是 unset——unset 会回落到操作者真实的 `~/.gopi/settings.json`）；新测试另用
`t.Setenv` 按用例指定 `GOPI_TASK_NOTIFY_CMD` 与 `GOPI_MEMORY_DB`。E2E 全程 `GOPI_HOME=/dev/shm/gopi-e2e-01a0eec9`，
输出里出现的 sink 路径只有 `/dev/shm/gopi-e2e-01a0eec9/sink.jsonl`，无 `task-events.jsonl` 泄漏。

**操作者历史文件（只读）**：会话开始前读到 `size 51,737,592` / `sha256 5d866865bfa430cf…`，
换行数 **1**，`}{` 出现 **135,164** 次，损坏起点 offset **33,935,609**（2026-09-12 的 `probe` 探针）。
本任务只对它做过 `read/stat/sha256/grep/jq/python3` 读操作。**它在此期间仍会因外部 serve 追加而增长**
（本次会话内实测 `51,737,592 → 51,739,223 (t+726s) → 51,740,734 (t+1081s)`，`mtime` 同步前移，`sha256` 随之改变；
每一条新增都来自外部会话，不含本任务的任何 task id）——因此本报告**不声称字节不变**；可核验的断言是：
本任务写入的 sink 路径全部在 `/dev/shm`（`…/ev.jsonl`、`…/ev-prefix.jsonl`），历史文件里不含本任务任何 task id。

### 6. 本轮独立复核（同会话后段重跑，全部为实测输出）

| 断言 | 命令 | 观察值 |
|---|---|---|
| 红（行为级） | pre-fix detached worktree + **只拷 sink 测试文件** → `go test -run TestTaskNotifyCmdSinkIsLineFramed` | `--- FAIL: … sink holds 0 line terminators for 2 events`，`RED_EXIT=1`，失败信息原样打印出 `…"}{"event":"completed"…` 两条粘连事件 |
| 绿 | 修复树同一条命令 | `ok github.com/nick/gopi/internal/tools 0.676s` |
| 二进制 A/B（pre-fix） | master 现场编译的 `gopi-prefix`（`sha256 87d030af34b9d811`）驱动 2 条真实通知 | `wc -l = 0`、`换行数 = 0`、`bytes = 697`、`read 循环迭代 = 0`、**`jq -c . \| wc -l = 2`（误导性判据）** |
| 二进制 A/B（post-fix） | 修复树 `gopi`（`sha256 3795c7b95acd6b40`）同命令同沙箱形状 | `wc -l = 2`、`换行数 = 2`、`bytes = 709`、逐行 `jq -r .event` → `in_progress` / `completed` |
| 逐行可解 | `while IFS= read -r l; do printf '%s\n' "$l" \| jq -r .event; done < sink` | 2 次迭代，各自独立解析成功 |

**为什么两个新测试文件不能一起拷进红证树**：见上（编译失败 ≠ 观察到缺陷）。

### 7. 与主库上后来的"接收端侧"修法共存（实测，非推理）

主库在 2026-09-30 04:36 落了 `briefings/notify-hp-to-peerbox-channel.md`（`7e140378`，仅 briefing，
未改 `tasknotify.go`），把 `notify.cmd` 换成**接收端侧**修法：

```sh
awk '{print; print ""}' | tee -a <events> > /dev/null
```

它在接收端补一个空行。**与本次生产者侧帧同用时实测的交叉效果**：post-fix 二进制 + 该 cmd 形状 →
`wc -l = 4`，`空行数 = 2`，**空行数 == 事件数**（每条事件后**多出一个空行**）：

```
line 1: JSON OK (len=350)      ← in_progress
line 2: JSON OK (len=0)        ← 接收端 awk 多补的空行
line 3: JSON OK (len=351)      ← completed
line 4: JSON OK (len=0)        ← 空行
```

顺带纠正那份 briefing 里一处关于我们 cmd 的测量口径：它的"`tee -a`/`printf` 都保留缺失的换行"这个
陈述在**本次修复之后**不再成立（我们现在**总是**以 `\n` 结尾）。而 `wc -l=4 / 空行=2` 正是"两条路径同时
在补帧"的直接证据。

**更重要的操作后果（第三方读取者据此设计 watch 会踩）**：空行在两种判据下**相反**地评估 —

```bash
printf '\n' | jq -e .   # exit 0   ← 空行被判为合法空输入（假绿）
printf '\n' | jq -c .   # exit 0 且无输出  ← 无记录
go json.Unmarshal([]byte(""))  # err=unexpected end of JSON input  ← 换行剥掉后 Go 侧报错
```

所以 `jq -e .` 逐行校验对"精确一个换行"没有鉴别力，而 Go/`json.Unmarshal` 对空行会失败。
**结论**：`awk '{print; print ""}'` 这一半应当收缩 —— 补帧是生产者（本修复）的唯一职责；
保留它只会在每个消费者里制造"空行算不算记录"的分歧。这对 51MB 历史文件**只读解析无影响**：
解析配方按**值**顺序 `raw_decode`，空白由 `data[i] in " \t\r\n"` 跳过，空行/缺换行同样安全。

**历史备份（新增实测，只读）**：`~/.gopi/notify/task-events.jsonl.bak-pre-newline-20260930`
（就是操作者那份坏文件的备份）`size=51,739,179`、**换行数 1**、空行数 0；用 §5 的配方恢复
**135,167 条事件 / 跳过 1 处损坏**，直方图 `completed 43436 / in_progress 37607 / claimed 30862 /
failed 21722 / stale 1499 / build-failed 41` —— 与原文件口径一致，可作为"修复前后分界"的比对基准。

**Rule 30**：`git status --porcelain` → 空（见收尾）。
