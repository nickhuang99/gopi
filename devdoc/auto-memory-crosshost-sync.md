<!-- doc-class: normative -->
# 跨机记忆同步的语义正确性：provenance 随行走 + 显式「细节仅在本机 <host>，不可达」

> **PARTLY REVERSED 2026-11-06 — the command this page names is BACK, minus one flag.**
> `gopi memory sync` was restored the day after its deletion, as a MANUAL door:
> one invocation = one session, exit. `--peer` / `--push` / `--pull` / `--dry-run` /
> `--serve-assets` / `--timeout` all run again; the runnable shape and its measured
> two-way session are in `devdoc/memory-sync-manual-door-restored.md`. **What is
> still gone is the `--every <dur>` cadence flag**, whose reader and loop went in
> batch 5 — so a line below that carries `--every` is a RECORD, not a command to
> paste (it exits 1 with `unknown flag`). The one-way copy `gopi memory backup`
> is the other manual cross-host door and its guarantees differ: it REPLACES the
> peer ledger instead of merging it.

**任务**：`01a11df4-b0a7-72ac-b801-aa251a9a0cc0`（epic `自动记忆` 的第 5/5 项）
**分支**：`task/01a11df4b0a772acb801aa251a9a0cc0`
**日期**：2026-10-09
**前提裁定**：第 1/5 项（`devdoc/auto-memory-candidate.md`）采纳**方案 C**——`[IMPORTANT]`
捕获的记忆行是**自包含正文**（4 KiB 上限、排除 thinking）。

---

## 0. 一句话结论

按方案 C，「指针悬空」**不作为内容故障**存在——正文与行同生共死，对端 `memory search`
能直接检索到完整正文（本文 §4 有逐字取证）。但第 2/5 项新增的 provenance
（`session_id` / `round_index` / `source_host`）此前**不上线**：`SyncRow` 线型只有
topic/question/answer/keywords/stored_unix 五个键，三列在 transport 上被静默丢弃——
正是「行加了列、transport 没加」这一类缺陷（memory #8459 的实测教训）。本项补齐
**投影 + 扫描 + 写入**三处，走**兼容通道**；并让对端对**不属于本机**的指针渲染出
**显式**句子「detail only on its own host "peerbox" … unreachable from here」，而不是留一个
静默悬空的指针（Rule 15）。

---

## 1. 缺陷：三列在线上，但没有上 transport

| 位置 | 改造前 | 后果 |
|---|---|---|
| 投影（export SELECT） | `topic, question, answer, search_kw, distilled_at` | 三列从不进 patch |
| 扫描（rows.Scan） | 五个目标 | 同上 |
| 写入（import → `memoryStoreRow`） | 传 `MemoryOrigin{}` | 对端行读回「未记录」 |
| 线型（`SyncRow`） | 五个 JSON 键 | **根因**：结构体没有该字段 |

**为什么「方案 C 已经消灭悬空」之后还要补**：C 解决的是**内容**可达性，不解决
**可寻址性**。「这一条是从哪儿来的？」是关于**产生它的那台机器**的问题；行到达对端时
若丢掉指针，对端的行与「当初没有任何人捕获过」的行**在数据上不可区分**——这正是
Rule 15 禁止的静默降级。所以：**正文随行走（C），指针也随行走（本项）**。

**RED 证据**（把投影临时改回改造前状态后真跑，见 §5 Evidence）：

```
--- FAIL: TestSyncOriginTravelsWithTheRow
    RED: the exported row has no origin — the three columns did not reach the wire (projection gap)
--- FAIL: TestSyncOriginSurvivesThePatchFile
    RED: the row's JSON carries no "origin" key:
    {"topic":"wire topic","question":"wire question for the peer", ... ,"stored_unix":1791517081}
--- FAIL: TestImportedOriginRendersLocalOnlyOnThePeer
    the render does not carry the "detail_local_only" marker — the unreachable state is silent
```

第三条 RED 的输出正是本项要消灭的那个形状——一个**悬空且无声**的指针：

```
origin=sess=sess-peer-1 round=4 host=some-other-node      ← 只有地址，没有任何可达性结论
```

---

## 2. 改法（三处 + 兼容通道）

### 2.1 线型：一个可选对象，不是一个新键的散列

```go
type SyncRow struct {
    Topic      string   `json:"topic"`
    Question   string   `json:"question"`
    Answer     string   `json:"answer"`
    Keywords   []string `json:"keywords,omitempty"`
    StoredUnix int64    `json:"stored_unix,omitempty"`
    Origin     *SyncRowOrigin `json:"origin,omitempty"`   // ← 本项新增
}
```

**为什么是一个对象而不是三个平铺键**：五个原键**逐字节不变**，所以**旧接收方**的
`encoding/json` 把不认识的 `origin` 键丢掉即可——**丢的是 provenance，从来不是行**
（这是**兼容通道**的核心：老对端不会因为新字段而整条链路失败）。平铺三键对老读者
同样是静默的，但它把三个新字符串放进每一行的形状里，而一个半实现的发送方可以
产出**半条指针**；一个对象只有两种含义：在（有指针走了）或不在（没有）。

### 2.2 投影 + 扫描：COALESCE 且**列存在性门控**

`syncOriginColumnsPresent`（`internal/tools/memory_sync_origin.go`）探测本库
`distilled_qa` 到底声明了哪几列，投影由探测结果拼装：

```go
selectSQL := `
    SELECT COALESCE(topic,''), ..., COALESCE(distilled_at,0)` +
    syncOriginSelectSep + syncOriginColumnExpr(presentOriginCols) + `
    FROM distilled_qa WHERE ...`
```

- 三列**都在** → 追加 `COALESCE(session_id,''), COALESCE(round_index,0), COALESCE(source_host,'')`；
- **一列都没有**（旧库 / 外来 schema）→ 追加三个**字面常量**
  （`syncAbsentOriginSelectExpr`），**扫描形状恒定**，SQL 不会死于 `no such column`。

这是 memory #8459 那条教训的直接复用：**「凡是行的结构体加列，必须把该列在每一条
transport 的投影 + 扫描 + 写入三处都补，缺一处就是静默丢列」，且跨机 schema 演进必须
走兼容通道（COALESCE / 存在性门控），不要硬引用新列。**

### 2.3 写入：指针随行落库，但**半条指针按行拒绝**

```go
if ptr := r.Origin.memoryOrigin(); !ptr.Complete() {
    stats.RowsRefused++                      // 文件通道
    stats.Errors = append(stats.Errors, fmt.Sprintf("row %d: %s", i, ptr.Refusal()))
    continue
}
res, err := memoryStoreRow(ctx, tx, r.Topic, r.Question, r.Answer, source, r.Keywords,
                           r.Origin.memoryOrigin())
```

两条通道（`SyncImport` 文件通道 / `syncAssetsImportRows` ssh 资产通道）都加了这层守卫。
**为什么是「按行拒绝」而不是让 store 的错误中止整个 patch**（那是改造前实测到的行为）：
一条来自某个构造错误的发送方的坏指针，会**把两个节点之间的整条同步链路打死**，
直到有人在远端修好为止——这正是「一行把整条通道拖死」的失败模式，也是本通道存在的
理由。拒绝仍然**有声且有据**：计数 + `stats.Errors` 里点名行号与原因。
（顺带把这条边界写成了测试：坏行两侧各放一条好行，断言 `RowsRefused=1` 且
`RowsImported=2`——邻居照常落地。）

### 2.4 不可达状态：一个**判据**，不是一次文件系统探测

```go
func originLocallyReachable(o *SyncRowOrigin, thisHost string) originReachability
func originLocalOnlyNote(o *SyncRowOrigin, thisHost string) string
func originLocalOnlyRender(o *SyncRowOrigin, thisHost string) string
```

三种判定：

| 判定 | 条件 | 渲染 |
|---|---|---|
| `originReachLocal` | 指针的 host **就是**本机 | 不渲染（`origin=` 段已经带着地址） |
| `originReachRemote` | host 是**别的**机器 | **显式句子**，点名那台机器 |
| `originReachUnknownHost` | 指针没记 host | 报为**未知**，不假装是地址也不判成远程 |

**关键是判据本身**：可达性是**节点身份**的函数，**不做任何文件系统探测**。
理由有二，且都成立：① 方案 C 下没有读者会去打开 session log，所以唯一**可知**、
也是 epic 唯一要求的，就是「细节属于哪台机器」；② 去 stat 一个路径就是引入
一个**新依赖**（Rule 35 禁止）。

渲染出的句子（本机 hp，行来自 peerbox）：

```
detail_local_only=detail only on its own host "peerbox" (recorded by another node) — session_id/round_index/source_host sess-aaa111, unreachable from here
```

**为什么另起一行而不是塞进 `origin=` 段**：`origin=` 段是紧凑的 key=value 地址表，
其余渲染器按字段解析它；把散文塞进去会让一个字段列表里出现句子。
新增标记常量 `originRenderLocalOnlyMarker`，让读者（和测试）不必解析英语就能找到它。

---

## 3. 红-绿测试（`internal/tools/memory_sync_origin_test.go`）

| 测试 | 钉住的事实 |
|---|---|
| `TestSyncOriginTravelsWithTheRow` | **RED 主证明**：三列到达线上（投影） |
| `TestSyncOriginSurvivesThePatchFile` | 经**真实 patch 文件** export→import：JSON 契约（`origin` 键存在）与对端落库 |
| `TestOriginLocalOnlyNote` | 三种判定 + 两种**不该**出句子（同机 / 无指针）；大小写不敏感 |
| `TestOriginLocalOnlyRenderCarriesAMarker` | 渲染带可检索标记；**拥有者本机不渲染**该句子 |
| `TestImportedOriginRendersLocalOnlyOnThePeer` | **端到端**：真实 `MemorySearch` 渲染出显式不可达句，且**自包含正文完整可检索** |
| `TestLocalOriginRendersNoLocalOnlyNote` | 本机行**不会**被误标（句子是指针的事实，不是无条件附加） |
| `TestExportSucceedsOnAPreOriginSchema` | 旧 schema（真实 schema 砍掉三列）导出**不失败**，行照常出去 |
| `TestImportOfAPreOriginPatchStoresNoPointer` | 旧发送方的 patch（无 `origin` 键）导入为零指针，**不报错也不编造** |
| `TestImportRefusesAHalfPointerFromAPeer` | 半条指针**按行**拒绝且被点名，邻居照常落地 |
| `TestOriginWireRoundTripsThroughJSON` | 转换对：nil 与零值都必须读作「未记录」 |

`-count=1` 全绿（见 §5）。

---

## 4. 真实双库往返（Rule 31 真跑取证）

全部临时库都在**私有 profile（sandbox）**里，操作者的 `~/.gopi/memory.db`
**一个字节都没被写**（Rule 32）。跨机那一段走**真实 ssh**，对端用**私有的 sandbox
profile + 本分支新建的二进制**，因此 hp 的真实库同样未被触碰。

### 4.1 发送端：peerbox 捕获一条带 provenance 的记忆

```
$ GOPI_HOME=$SB_A gopi memory store \
    --topic "跨机 provenance 取证" \
    --question "how does provenance survive cross-host memory sync?" \
    --answer "SELF-CONTAINED BODY: the [IMPORTANT] note travels verbatim under scheme C (4KiB cap), so the peer can search it directly." \
    --keywords "sync,provenance,crosshost" \
    --session-id "sess-aaa111" --round-index 9 --db $SB_A/.gopi/memory.db
Memory stored (id=1).
```

### 4.2 导出：三列在线上

```
$ GOPI_HOME=$SB_A gopi memory memsync --export --out /tmp/origin-patch.json --db $SB_A/.gopi/memory.db
exported 1 rows (1 scanned, 0 skipped by --since) → /tmp/origin-patch.json (node peerbox)
```

patch 的 row（逐字，注意 `origin` 对象）：

```json
{"topic":"跨机 provenance 取证",
 "question":"how does provenance survive cross-host memory sync?",
 "answer":"SELF-CONTAINED BODY: the [IMPORTANT] note travels verbatim under scheme C (4KiB cap), so the peer can search it directly.\n\nkeywords: sync, provenance, crosshost",
 "keywords":["sync","provenance","crosshost"],
 "stored_unix":1791517159,
 "origin":{"session_id":"sess-aaa111","round_index":9,"source_host":"peerbox"}}
```

### 4.3 真实 ssh：peerbox 的 patch → hp 的 `--serve-assets`

```
$ ssh -o BatchMode=yes hp 'GOPI_HOME=/home/user/workspace/gopi-sandbox/sandbox-originpeer-0fc160e4 \
      GOPI_SYNC_NODE=hp /tmp/gopi-origin-hp memory sync --serve-assets' < /tmp/origin-patch.json
exit=0
{"version":1,"node":"hp","exported_at":1791517211,"generator":"dev",
 "import_stats":{"imported":1,"existing":0,"deduped":0,"refused":0},"rows":[]}
```

### 4.4 对端 `memory search`：正文完整 + 显式不可达

```
$ ssh hp 'GOPI_HOME=$SBHP GOPI_SYNC_NODE=hp /tmp/gopi-origin-hp memory search \
      "cross-host memory sync provenance" --limit 2 --db $SBHP/.gopi/memory.db'

#1 跨机 provenance 取证 tier=warm conf=0.85 lesson=recipe when=2026-10-09 03:40:10
   sources=node:peerbox tokens=4 origin=sess=sess-aaa111 round=9 host=peerbox
Q: how does provenance survive cross-host memory sync?
A: SELF-CONTAINED BODY: the [IMPORTANT] note travels verbatim under scheme C (4KiB cap), so the peer can search it directly.

keywords: sync, provenance, crosshost
detail_local_only=detail only on its own host "peerbox" (recorded by another node) — session_id/round_index/source_host sess-aaa111, unreachable from here
```

**三件事在同一段输出里被同时证明**：① 正文完整可检索（方案 C）；② 指针三列在
`origin=` 段里完好（本项）；③ 显式不可达句子出现（deliverable (b)）。

### 4.5 反方向（pull）：hp 捕获 → peerbox 导入

```
$ ssh hp '... memory store --topic "hp 本机教训" \
      --question "what did hp learn about sync pull?" \
      --answer "PULL LEG BODY: captured on hp itself, self-contained." \
      --session-id "sess-hp-777" --round-index 3 --db $SBHP/.gopi/memory.db'
Memory stored (id=2).

$ ssh hp '... memory memsync --export --out /tmp/hp-patch.json --db $SBHP/.gopi/memory.db'
exported 1 rows (1 scanned, 0 skipped by --since) → /tmp/hp-patch.json (node hostb-Laptop)

$ GOPI_HOME=$SB_A gopi memory memsync --import /tmp/hp-patch.json --db $SB_A/.gopi/memory.db
imported: stored=1 deduped=0 refused=0

$ GOPI_HOME=$SB_A gopi memory search "sync pull" --limit 3 --db $SB_A/.gopi/memory.db
keyword…
detail_local_only=detail only on its own host "hp" (recorded by another node) — session_id/round_index/source_host sess-hp-777, unreachable from here
---
#1 跨机 provenance 取证 … origin=sess=sess-aaa111 round=9 host=peerbox      ← 本机行：无不可达句
```

**同一段渲染里，来自 hp 的行报不可达、本机的行不报**——这正是「句子是指针的事实，
不是无条件附加」在真实输出上的样子。

### 4.6 旧对端 / 旧库的兼容真跑

```
$ GOPI_HOME=$SB_B GOPI_SYNC_NODE=hp gopi memory sync --serve-assets < /tmp/origin-patch.json
exit=0
{"version":1,"node":"hp","exported_at":1791517189,"generator":"dev",
 "import_stats":{"imported":0,"existing":1,"deduped":0,"refused":0},"rows":[]}
```

（对端回 `existing:1` 而不是 `imported:1`，因为同一个 session 里先前已导入过——通道
的可重入语义照旧。）

---

## 5. Evidence (self-filed) / 印证与复现

- **Build**：`go build ./...` → 无输出（成功）；`gofmt -w` 后 `internal/tools` 我改的两个文件无格式问题。
- **RED 证明**（把投影改回改造前状态后真跑，两条主测试按预期失败）：
  `go test ./internal/tools/ -run 'TestSyncOriginTravelsWithTheRow|TestSyncOriginSurvivesThePatchFile'`
  → `FAIL`，并打印上面 §1 引用的两条 RED 行；恢复后同命令 `ok`。
- **GREEN 证明（本项全部测试）**：
  `go test ./internal/tools/ -run 'TestSyncOrigin|TestOriginLocalOnly|TestImportedOrigin|TestLocalOrigin|TestExportSucceedsOnAPreOriginSchema|TestImportOfAPreOrigin|TestImportRefusesAHalfPointer|TestOriginWire' -count=1`
  → `ok  github.com/nick/gopi/internal/tools  1.474s`。
- **本包全量**：`make test-budget PKGS='./internal/tools'`（结果见 §6）。
- **Ran**：§4 的每条命令都是逐字真跑；被测二进制是**本分支新建**的
  `/tmp/gopi-origin`（本机）与 `/tmp/gopi-origin-hp`（对端，为 hp 交叉编译后 scp）。
- **设备检查（Rule 22）**：沙盒与 session 目录在 `%d=2052`，`/` 是 `2051`。

---

## 6. Not Done（诚实边界）

- **`--every` 的常驻轮询腿未单独真跑**：本项取证走的是 `memory sync --serve-assets`
  这一条**真实的** ssh 服务端腿（对端真机、真二进制、真 profile）与文件通道的双向
  export/import。周期腿对同一组函数只是重复调用，因此没有另起一个常驻进程去验它。
- **`memory sync --peer hp` 从本机发起的完整 ssh 会话未跑**：它会走 `ssh hp gopi …`，
  即**对端 PATH 上已安装的 gopi**（不是本分支的二进制），所以那次真跑只能证明
  **master 版本**的兼容性、不能证明本分支。真正证本分支的跨机证据是 §4.3——把本分支
  的二进制放到对端、用对端自己的 profile 跑 `--serve-assets`。这一条是**有意选择**，
  不是遗漏。
- **`round_index` 的「本机可达」分支未做端到端**：`originReachLocal` 只在测试里
  钉住（`TestLocalOriginRendersNoLocalOnlyNote`），没有真去读一个 session log——
  因为方案 C 下**没有任何代码路径会读它**（本项刻意不引入该依赖）。这是设计边界。
- 未评估该项对 `memory search` 延迟的影响：本项没有给行**新增**任何索引，只多导
  三列，§1/5 已量化过体积与带宽量级。
- **目标包套件里跑出一个与本改动无关的间歇红**：`internal/tools` 的
  `TestBashWithTimeout_PositiveKills`。差分已证与本次改动无关
  （`git diff master HEAD -- internal/tools/bash.go internal/tools/timeout.go
  internal/tools/bash_test.go` 为空；隔离 `-count=20` 复现 1/20），机理是
  `timeout_sec=1` 时 idle 上限被 abs 预算夹到同一秒、两个 watcher 同刻到期。
  已按 Rule 26 另开 bug 行 `01a11edc-cab6-7872-9c80-588192bf10f7`，本项不动它。

---

## 8. 一条顺带测出的做法（给下一个改 `internal/tools` 的人）

**新增文件必须先 commit，再跑 `make test-budget`。** `cmd/agent` 有一族测试
（`cli_version_stamp_test.go` 的 `newVersionFixture`）把仓库 `git clone` 到
`/dev/shm` 再编译，而它的 `overlayWorkingTree` 只把**已跟踪文件的工作区改动**
搬进副本——它自己的注释写着「a file added to the source tree is not carried over
(it is untracked…)」。于是**未跟踪的新文件在副本里根本不存在**，影子构建以
`undefined: <你刚写的符号>` 失败：

```
cli_version_stamp_test.go:352: go build -o /dev/shm/suite-cmd-agent-run-*/.../gopi-main ./cmd/agent: exit status 1
    # github.com/nick/gopi/internal/tools
    internal/tools/memory_sync.go:80:10: undefined: SyncRowOrigin
```

**识别特征**：错误路径是 `/dev/shm/suite-…` 而不是你的 worktree，且报的是
「你明明写了的符号 undefined」。**这不是 flake，不要提 bug 行**——`git add` +
commit 后重跑即绿（本项实测：commit 后 `cmd/agent` 全绿）。
它与另两类「scoped smoke 假红」要分开：① 重叠套件/负载下的并发干扰
（隔离 `-count=N` 判断）；② `bin/gopi` 过期（先重建）。

---

## 7. 判据（可推广）

1. **「方案 C 消灭了悬空」不等于「指针可以不传」**：C 解决**内容**可达性，
   不解决**可寻址性**。丢掉指针不会让正文消失，但会让到达的行与「没被捕获过」的行
   **在数据上不可区分**——那本身就是一种静默降级。
2. **补列的判据是「这个判断在哪台机器上执行」**（memory #8459）：可达性判断在
   **接收方**执行，所以指针必须到接收方。反过来，`claimed_by` 这类发送方持有的事实
   明确留在本地。
3. **一条坏行不能拖死整条通道**：跨机通道里，任何**按行可判定**的坏输入都应当是
   **按行计数 + 点名**的拒绝，而不是整批中止——否则远端的一个构造错误会变成两个
   节点之间的长期断链。这与「文件通道单事务、all-or-nothing」并不矛盾：事务边界管的是
   **DB 级失败**，语义拒绝是**决定**，不是失败。
4. **不可达状态必须是判据而不是探测**：能用「它属于哪台机器」回答的，就不要去
   stat 一个路径——后者是新增依赖，而且答案更弱（本机没有文件 ≠ 细节不在本机）。
