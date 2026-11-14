<!-- doc-class: normative -->
# 自动记忆的捕获判据：内容形态闸门，以及它与 hermes 闸门的关系

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

> **溯源。** `TestDocAnchorsResolveInNormativeDocs` 已于 2026-10-22 随 `internal/docclass` 在 `5a35f61` 中删除：本页里的 `file.go:NNN` 引用是落地时的记录，没有任何在跑的门禁解析它。行号对人依然有用，但它不再被测试守护。下文引用的原句都按当时的写法保留 —— 若某句读起来像现在时（"FAIL"、"resolve"、"门禁"），那是那一次运行的状态，不是本树上还活着的门禁。

**任务**：`01a11df4-ae3b-70e3-b823-38e632816f25`（epic 自动记忆 的第 3/5 项）
**日期**：2026-10-09
**范围**：判据 + 捕获接线（`internal/agent/selfnotecapture.go`、`selfnotecapture_test.go`、
`selfrecord.go` 的一个字段、`loop.go` 响应结束路径的一行调用）。
**上游裁定**：item 1/5（`devdoc/auto-memory-candidate.md`，方案 C 自包含全文）与
item 2/5（`devdoc/auto-memory-candidate-schema.md`，session/round/host 指针）。

---

## 0. 一句话

**捕获是一道「内容形态」闸门：一条 note 只有在是拓扑图 / 表格 / 带推理的长结论时才转成
`distilled_qa` 行；空壳状态报告一律拒绝。** 判据是纯函数 `classifySelfNote`，全部阈值是具名
常量，零新依赖、零 LLM 调用、零新工具 —— 它挂在循环里已有的「响应结束」那一行上。

---

## 1. 判据的六个分支（`classifySelfNote`，纯函数）

顺序即成本与确定性顺序，先拒绝污染，再判长度，最后三条形态臂（强在前）：

| 序 | 分支 | 常量 | 值 | 裁定 |
|---|---|---|---|---|
| 1 | 占位符 | `selfRecordPlaceholderShape` | `<round summary>` | 拒绝 `placeholder` |
| 2 | 标记字面量 | `selfNoteMarkerOpen/Close` | `[IMPORTANT]` / `[/IMPORTANT]` | 拒绝 `marker-literal` |
| 3 | 长度下限 | `selfNoteMinRunes` | 150 runes | 拒绝 `too-short` |
| 4 | 拓扑图 | `selfNoteTopologyGlyphs` | `├└┌┐┘│─┴┬┼┃━┏┓┗┛→←↑↓↔⇄⇒⇐` | 捕获 `topology` |
| 5 | 表格 | `selfNoteTablePipeMin` | `|` 计数 ≥ 3 | 捕获 `table` |
| 6 | 长结论 | `selfNoteConclusionRunes` + `selfNoteSummaryMarkers` | ≥300 runes **且**含推理标记词 | 捕获 `conclusion` |
| — | 其余 | — | — | 拒绝 `no-structure` |

**为什么第 6 条需要两个条件**：长度单独一个条件无法把「长结论」和「长状态报告」分开 ——
这是实测出来的，不是设计洁癖。在操作者自己的记录语料（67 个会话、113 条 note，
`~/.gopi/sessions/*/self-record.md` 全量，2026-10-09）里，**39 条 ≥300 runes 的 note 中只有 16 条
是带推理的长结论，另外 23 条是「全都跑了、全绿」式的状态报告**；只有长度臂的版本会把那 23 条
一起收进库里。加上标记词后两类被正确分开（16 / 23）。

**标记词表是从语料里数出来的，不是想出来的**：判定 21、证据 10、实测 9、根因 8、证明 7、
因此 3、决定 3、结论 2、修法 1 …… （中英双语各取，见 `selfNoteSummaryMarkers`）。

**拒绝是**有**理由的**：`selfNoteVerdict.Reason` 是六个具名值之一，调用方把它写进 run 日志
（`🧠 auto-memory: note not captured (too-short)`）—— 一次没转成记忆的 note，理由是可读的，
不是静默的（Rule 15）。

---

## 2. 与 hermes `_has_structure()` / `MIN_RESPONSE_LENGTH` 的关系：**扩展，不是包含**

hermes 的闸门（`~/.hermes/plugins/auto_distill/__init__.py:786-790`）：

```python
MIN_RESPONSE_LENGTH = 200                       # 合并后正文的字符数
_has_structure(text) = 有表格行 ^\|.+\|.+\|$ / ## 标题 / ``` 代码块 / **粗体** /
                        ^[-*] 列表 / ^\d+. 有序表 / ^> 引用
```

**判据 = 同一语料上逐条实测**（113 条 note，脚本见 §5）：

| 集合 | 条数 |
|---|---|
| hermes 闸门（`_has_structure` **且** ≥200 字符） | 6 |
| 本判据闸门 | 38 |
| 两者交集 | 4 |
| **只有 hermes 放行** | **2** |
| **只有本判据放行** | **34** |

**裁定：是「扩展」+ 两处有意的差异，不是严格的包含关系。**

1. **长度臂是包含**：hermes 的 200 字符（Python `len` = 字符数）⇒ 必然 ≥150 runes，所以凡过
   hermes 长度门的，都过本判据的下限；本判据下探到 150，比 hermes **更宽**。
   单位也不同：hermes 数的是「合并后的整轮回复」，本判据数的是**一条 note**（≤500 runes，契约硬顶）。
2. **形态臂是互有出入，不是超集**：
   - 本判据有、hermes 没有：**拓扑图**（框线/箭头字符集）与**长结论**（长度+推理标记）。
     这正是操作者指定的两个形态（含图表 / 含拓扑图 / 大段总结）—— hermes 一个都不覆盖：
     实测里 34 条 note 被本判据收下而 hermes 全部漏掉。
   - hermes 有、本判据没有：`**粗体**`、``` 代码块 ```、`##` 标题、`-`/`1.` 列表、`>` 引用。
     实测里 2 条 note 因为含 `**粗体**` 过 hermes 而被本判据拒绝。
     **这是有意的**：操作者给的判据是「含图表/含拓扑图/大段总结」，粗体不是这三种形态之一；
     把 hermes 的装饰类形态一并收进来 = 把闸门放宽到操作者没有裁定的范围。
     若日后要收，加一条具名臂即可（见 §4 的"扩展点"）。
3. **闸的对象不同**：hermes 是 **post-hook 按 section 切分后逐段判**（`ds_response_splitter`），
   本判据判的是**模型自己写下的一条 note**——注记（self-recording 第一期）本身就是模型的提炼结果，
   所以闸门只需要管形态，不需要再切段。

---

## 3. Rule 35 四问（零依赖，逐条正面回答）

1. **不做会坏什么。** 不新增任何持久依赖，所以这一问的前提不成立 —— 但要正面说清它换来了什么：
   没有这道闸门，`[IMPORTANT]` note 全量直灌 `distilled_qa`，而实测语料里 21% 的 note 短于
   150 runes（24/113），另有 23 条长篇空壳；这些都是"运行状态"而不是"经验"，进库即噪音。
   闸门是**在已有的写入路径上加一次纯函数判定**，不是新增机制。
2. **为何不能就地解决 / 为什么不许要新机制。** 因为捕获点本身就在**已有的响应结束路径**上：
   `loop.go` 里 `recordSelfNotes` 那一行（第一期挂点），判定是同一个调用栈里的一次函数调用。
   写入走**已有的** `tools.MemoryStoreWithOrigin`（item 2/5 的入口），落进**已有的** `distilled_qa`
   与其 FTS/trigram 触发器。**没有任何新的 tick、timer、worker、socket、lock file、config key。**
3. **生命周期归谁。** 数据落在**已有的** `memory.db`，由保留策略治理（当时的答案是“已有的 `gopi memory sync`”，但该动词已于 `5677cc6e` 删除；今天在库与库之间搬数据的是单向拷贝 `gopi memory backup`）；
   run 内的**去重表**（`selfRecord.noteStored`，一个 map）随 run 结束而消失，不需要回收。
   **没有新工件 ⇒ 没有新的 owner 问题。**
4. **后人如何辨识。** 日志行以 `🧠 auto-memory:` 开头，写明是捕获 / 跳过 / 拒因 / 库返回的
   `Memory stored (id=N)`；判据的每一条臂和每个阈值都有具名常量与注释说明它为什么是这个值。

---

## 4. 接线，以及它**不做**的三件事

接在 `loop.go` 响应结束路径，与第一期抽取**同一站点**，且是**行数中性**的一行改：

```go
a.captureSelfNotes(a.recordSelfNotes(content, res.thinking))
```

**不做**的三件事，每一件都是 epic 的显式约束或实测教训：

- **不置位 `a.memoryStored`**。task-end 的记忆要求是**模型的**义务（完成检查点就是为它存在的），
  让 harness 自己的捕获替它满足，等于悄悄退役那个检查点。测试
  `TestCaptureSelfNotesNeverSatisfiesTheMemoryRequirement` 先证明"确实发生了一次真实捕获"，
  再断言标志仍为 false。
- **不改变 `self-record.md` 的语义**。侧信道文件仍然**全量**记录每一条 note（那是证据）；
  过滤只作用于"转记忆"这一步。
- **不写没有指针的行**。指针来自**同一个** `memoryStoreOrigin()`（item 2/5）：没有 session 的 run
  桩不出 session，就**不写匿名行**，只记一行日志（`no session to stamp the pointer with`）。

失败一律只记日志（Rule 15b）：store 报错、库不可写、指针缺失，都只写日志、run 照常继续。

**扩展点（写给后来者）**：要加形态臂（例如 hermes 的粗体/代码块），在 `classifySelfNote` 加分支 +
给常量 + 在表驱动测试里给正反例；**不要**把阈值塞进调用点。

---

## 5. 证据（Rule 30 / 31）

**Build**

```
$ go build ./...                      # 无输出
$ CGO_ENABLED=0 go build -o /tmp/gopi-cap ./cmd/agent/    # 无输出
```

**Tests**

```
$ go test ./internal/agent/ -run 'TestClassifySelfNote|TestCaptureSelfNotes' -count=1
ok  	github.com/nick/gopi/internal/agent	1.320s
```

**真跑（Rule 31：在独立 sandbox profile 里跑一个真实 task run，不是本会话）**

```bash
SB=$(/tmp/gopi-cap tool sandbox create --label caprun --task-id 01a11df4ae3b70e3b82338e632816f25 | tail -1)
# → /home/user/workspace/gopi-sandbox/sandbox-caprun-7c0a25a0   (设备号 2052 ≠ / 的 2051，Rule 22)

GOPI_HOME=$SB /tmp/gopi-cap task submit "AUTO-MEMORY CAPTURE PROBE 2 (fixture…)" \
    --repo /home/user/workspace/gopi-caprun-repo --payload @/tmp/caprun-brief.md --source agent
# → Task queued: 01a11e7f-e8dc-7802-a915-5b6ea5e77462

cd /home/user/workspace/gopi-caprun-repo
GOPI_ALLOW_AGENT_SPAWN=1 GOPI_HOME=$SB /tmp/gopi-cap worker 01a11e7f-e8dc-7802-a915-5b6ea5e77462
```

`Result`（worker 日志逐字）：

```
[gopi] 📝 self-record: 1 note(s) appended (1 held for replay)
[gopi] 🧠 auto-memory: captured a conclusion note — Memory stored (id=1).
```

**读回那一行**（同一个 sandbox profile，`gopi memory search`）：

```
#1 self-record tier=warm conf=0.85 lesson=failure when=2026-10-09 02:31:07
   sources=self-record(auto) tokens=2
   origin=sess=ec7ab84d-a97c-498e-8e59-74dfb8052327 round=1 host=peer1
```

⇒ **行 id = 1**；`session_id=ec7ab84d-a97c-498e-8e59-74dfb8052327`、`round_index=1`、
`source_host=peer1`，与 epic 要求的 session/round 指针一致。

**第一轮探针的反例同样跑出来了**（同一 sandbox，行前的 note 只有 130 runes）：

```
[gopi] 🧠 auto-memory: note not captured (too-short)
```

—— 闸门在真 run 里既放行了应该放行的，也拒绝了应该拒绝的。

**判据在真实语料上的分布**（113 条 note，2026-10-09；脚本见下）：

| 结果 | 条数 | 占比 |
|---|---|---|
| 捕获（拓扑 22 / 结论 16） | 38 | 34% |
| 拒绝 `too-short` | 24 | 21% |
| 拒绝 `no-structure` | 46 | 41% |
| 拒绝 `marker-literal` | 5 | 4% |
| 拒绝 `placeholder` | 0 | 0% |

```python
# 复现判据分布：读 ~/.gopi/sessions/*/self-record.md 的 note 行
#   （2026-11-10 起新行是 "r<round> <note>"，历史行是 "- <note>"，
#    两种形状都要算 —— 统一走 internal/agent/selfRecordNoteFromLine）
```

**不在本报告内的诚实边界**

- `TestDocAnchorsResolveInNormativeDocs` 在本 worktree 上 **FAIL**，但它**不是本改动的回归**：
  在本分支 base（`master b9b0cb217`）的干净副本（`git archive master` 解到 scratch，非 /
  分区）上跑同一个测试，**同样的 3088 anchors / 39 new / 12 stale permits**、同样的 30 个
  漂移站点、同样的 12 条 stale permit（逐行 diff 只有 worktree 路径前缀与提交顺序不同）。
  它是 master 自带的既有红，不是本改动的产物 —— 报告里不把它算作"绿"。
- 本改动**没有**改动 system prompt / kernel 预算（契约与判据都不进 kernel，沿用第一期结论：kernel
  4488/4500 无余量）。prompt cache 因此不受影响；本 run 的 session `request` 事件里
  cache_hit/cache_miss 可直接复核（第一期配方）。
- 判据的阈值（150/300 runes、3 个竖线、字符集）**是可调的具名常量**：它们来自本文 §1 的语料实测，
  换语料应重新数，而不是照抄。

**Commit**：见下方 commit 行（分支 `task/01a11df4ae3b70e3b82338e632816f25`）。
