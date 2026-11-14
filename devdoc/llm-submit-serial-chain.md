<!-- doc-class: historical -->
# LLM 提交顺序 = 依赖顺序：reframe roadmap 与 task_submit 的串行链

Task: `01a10a24-…` (epic) · 日期 2026-10-05 · 分支 `task/01a10a247a8e7a8bb420b90f814ca089`

## Completed

**operator 裁决（2026-10-05）**：「LLM 提交任务的顺序 = 逻辑依赖顺序……不要考虑并行的损失，
默认顺序就是依赖」。模型表达不了依赖图，它能给出的唯一依赖信号就是**提交顺序**。本改动把
这个顺序变成账本的真值，两条 LLM 路径都改：

1. **`internal/tools/tasks.go`** — 新增 `TaskSubmitReturningID(opts) (string, string, error)`：
   同一条 `insertTaskRow` 漏斗，多返回刚写入的行 id。`TaskSubmit` 变成它的**薄包装**
   （`out, _, err := TaskSubmitReturningID(opts); return out, err`），确认串逐字不变。
   mirror leg（`taskSubmitRemote`，转发 `SyncTaskRow`）返回空 id —— 该 wire 不带 parent 列，
   空 id 是诚实答案，调用方据此**保留**上一环而不是编造一个链接。
2. **`internal/agent/reframe.go`** — `dispatchReframeRoadmap` 的 `parentID` 随链推进：
   首项 `= rowID`，其后 `= 上一项成功返回的 child id`。**失败不断链**：某项提交失败时
   `parentID` 不动，下一项挂到「最后一个真的落地」的行上，代价是这一项而不是后面全部。
   同一函数里加 `reframeSubmitFn` 注入缝（package var），唯一目的是让「中途提交失败」这个
   真实状态造不出的分支可被测试钉住（Rule 10.5）。
3. **`internal/agent/loop.go`** — `task_submit` 工具描述改写：删掉"全挂同一 parent + `gopi task serve`"
   的旧引导，改为「按期望执行顺序逐条提交，harness 自动把每项挂到上一项」；`parent_task_id`
   参数描述同步（省略 = 自动成链；显式给出 = 逐字尊重）。executor 里新增 `lastSubmitRowID`
   自链：省略 parent 时挂到本 run 上一次提交的行，首次则挂到本 run 自己的账本行
   （`autoTaskRowID`），两者皆空则**不链接并如实报告**。措辞常量集中在
   `internal/agent/task_submit_chain.go`（Rule 1/13）。
4. **`internal/tools/reframe.go`** — `TaskChildren` 的文档改正（链式后它只返回链头，
   其余要逐环走）；`TaskShowRow` 加了又删：`TaskChildren` 已足以表达"谁挂在我下面"，链式
   检查不需要第二个 reader（Rule 14）。

**未改动**：Rule 33 一字未动 —— gate、释放集 `{completed, verified}`、以及无
override/cascade/timeout，都是**同一个谓词**（`taskParentGateDenied`）；本次只决定子行
**指向谁**。命令行 `gopi task submit` 未动（人类路径，`--parent-task-id` 照旧）。不实现
reparent（另有任务 `01a10a18`）。

## Not Done

- **Rule 11（LLM 真的按新描述提交）的完整证明**：sandbox 上取得了一次真实会话证据
  （3 次 task_submit，模型主动逐条提交、自动成链，见下 `Result`），但那是**被 briefing
  明确指示**"逐条提交、不要传 parent_task_id"的运行 —— 工具**描述**在没有外部指令时是否
  足以单独说服模型，本会话未取得独立证据。这条留作已知缺口。
- 失败的**注入**路径（`reframeSubmitFn` case 2）只有单测覆盖，没有 sandbox 真跑。
- 上游 `TaskSubmit` 的其它调用点未逐一复审（契约未变，仅多一条出口）。

## Verification

- Build: `go build ./...` → OK（无输出）
- Tests: `go test ./internal/agent/ -run 'TestReframe|TestHandleTaskReframe|TestParseReframe|TestEpicFraming' -count=1` → `ok github.com/nick/gopi/internal/agent 3.259s`
  四个新场景：`TestReframeRoadmapChainIsSerial`（3 项 → 链 + 恰好 1 行可 claim）、
  `TestReframeRoadmapSingleItemStaysDirect`（单项边界）、`TestReframeRoadmapFailureKeepsChainLinkable`
  （失败不断链）；空 roadmap 由既有 `validateReframe` + `TestHandleTaskReframeInvalidHasNoSideEffects` 覆盖。
  既有两处"兄弟"断言（`reframe_test.go` 的 `TooBigEnqueuesChildren` / `RepeatRecordedNotRedispatched`）
  已同步改为链式断言 —— 它们数"直系子节点"，链式后正确结果就是 1。
  包级：`make test-budget PKGS='./internal/agent ./internal/tools'` → 见下 Evidence。

- Ran: **真二进制 + 真模型 + sandbox 账本**（Rule 31/34），两条腿：

  ```
  # sandbox（Rule 34：私有 profile，未污染真实账本）
  SB=$(/tmp/gopi-chain-484813 sandbox create --label chain-e2e --task-id 01a10a247a8e7a8bb420b90f814ca089 | tail -1)
  # → /home/user/workspace/gopi-sandbox/sandbox-chain-e2e-986a1912

  # 腿 1：reframe roadmap（父行 01a10a69-81dc-…，wrapper 身份 ⇒ 走 [EPIC GOAL] framing）
  GOPI_HOME="$SB" GOPI_MEMORY_DB="$SB/.gopi/memory.db" \
    GOPI_TASK_WRAPPER_ID=01a10a69-81dc-7574-afff-55b4d30e3905 \
    /tmp/gopi-chain-484813 < /tmp/chain-brief.md

  # 腿 2：task_submit 自动成链（父行 01a10a6b-ef08-…，模型 3 次独立 task_submit，均省略 parent）
  bash /tmp/chain-run2.sh   # 同一 GOPI_HOME/GOPI_MEMORY_DB，cwd = 本 run 的 worktree（git 树）

  # 取证
  gopi task list --status pending --db "$SB/.gopi/memory.db"
  python3 -c "sqlite3 read-only: SELECT task_id,parent_task_id,status,summary FROM tasks ORDER BY created_at"
  ```

- Result:

  **腿 1 — reframe roadmap 链**（release epic 行后，`gopi task list --status pending`）：
  ```
  #01a10a6a-1564-…  [pending]  [3/3] Step three: verify …  [queue-skip] parent 01a10a6a-14bd-… not released (status=pending; releases on completed|verified)
  #01a10a6a-14bd-…  [pending]  [2/3] Step two: apply  …    [queue-skip] parent 01a10a6a-141e-… not released (status=pending; releases on completed|verified)
  #01a10a6a-141e-…  [pending]  [1/3] Step one: prepare …   [next]
  ```
  恰好 1 行无 `[queue-skip]`（链头带 `[next]`），另 2 行带 gate 原文并指名前驱 —— 正是验收条件。

  **真账本 SQL（两条腿合看，`ORDER BY created_at`）**：
  ```
  01a10a69-81dc-… | (none)                               | completed | e2e: serial-chain evidence run (TOO_BIG reframe)
  01a10a6a-141e-… | 01a10a69-81dc-…                      | pending   | [1/3] Step one: prepare …
  01a10a6a-14bd-… | 01a10a6a-141e-…                      | pending   | [2/3] Step two: apply …
  01a10a6a-1564-… | 01a10a6a-14bd-…                      | pending   | [3/3] Step three: verify …
  01a10a6b-ef08-… | (none)                               | completed | e2e: task_submit auto-chain evidence run
  01a10a6d-8c48-… | 01a10a6b-ef08-…                      | pending   | e2e chain submit A
  01a10a6d-952d-… | 01a10a6d-8c48-…                      | pending   | e2e chain submit B
  01a10a6d-a15e-… | 01a10a6d-952d-…                      | pending   | e2e chain submit C
  ```
  每项 parent = 前一项，首项 = reframe/wrapper 行；三条 `e2e chain submit` 行是模型**三次独立
  `task_submit`**（briefing 明令省略 `parent_task_id`）产生的，harness 自动成链。

  **pending 视图（腿 2，`A` 无 `[queue-skip]`，`B`/`C` 带 gate 原文指名前驱）**：
  ```
  #01a10a6d-a15e-…  [pending]  e2e chain submit C  [queue-skip] parent 01a10a6d-952d-… not released (status=pending; releases on completed|verified)
  #01a10a6d-952d-…  [pending]  e2e chain submit B  [queue-skip] parent 01a10a6d-8c48-… not released (status=pending; releases on completed|verified)
  #01a10a6d-8c48-…  [pending]  e2e chain submit A
  ```
  模型侧返回体逐条带着链注（会话 JSONL 内 `chained: this row's parent_task_id is …`），
  即「harness 替你链接了」这句话真的回给了模型。

- Commit: 见本分支末尾 commit（`feat(task): LLM submit order IS the dependency order`）。

## Evidence (self-filed)

- `go build ./...` → exit 0，无输出。
- `go test ./internal/agent/ -run 'TestReframe|TestHandleTaskReframe|TestParseReframe|TestEpicFraming' -count=1` → `ok … 3.259s`（首次红在 `internal/agent/reframe_test.go` 的 `TestReframeRoadmapChainIsSerial`：`release the epic row: task: illegal status transition pending → completed` —— 状态梯子建成三跳后转绿）。
- `make test-budget PKGS='./internal/agent ./internal/tools'` → 见下方 job 输出（本 run 记录：首次 436s，`FAILED: internal/agent`，原因是**编译错**——我 M5 编辑里 `first` 重名（`internal/agent/reframe_test.go` 的 `no new variables on left side of :=`）；修正后靶向绿）。
- sandbox 真跑（两条腿）的原文与 SQL 见上 `Ran:` / `Result:`，账本文件为
  `/home/user/workspace/gopi-sandbox/sandbox-chain-e2e-986a1912/.gopi/memory.db`（真实账本
  `/home/user/.gopi/memory.db` 未被本次验收写入）。
- 未做：Rule 11 的无指令独立证据（见 `## Not Done`）。

## Rework onto master 99e65d50 (task 01a10ab4, 2026-10-05)

本文档随**重做合并**落地：原分支 `task/01a10a247a8e7a8bb420b90f814ca089` 的 ref 被并合运行
删除，tip `0b19bb27` 仍在对象库中（`git cat-file -t` = commit），现由
`rescue/llm-submit-serial-chain-0b19bb27` 标签钉住。重做方式为**真合并**（`git merge --no-ff 0b19bb27`
到 master 99e65d50），不是 cherry-pick —— 孤儿提交自身的谱系被保留。

**唯一冲突**：`internal/tools/tasks.go`，按**并集**手工解决（无 `-X ours/theirs`、无 rebase）。
master 侧在该文件前移了 325 行并引入具名常量 `taskParentMissingFormat`（Rule 1/13），
分支侧把同一条 return 拓宽成三值（`TaskSubmitReturningID` 需要）。两侧都保留，并在原地留
`MERGE NOTE` 注释。相对分支 tip，HEAD 在该文件上**只有一行删除** —— 正是那条被并集取代的
重复字面量：

```
-			return "", "", fmt.Errorf("task_submit: parent_task_id %q does not exist", opts.ParentTaskID)
+			return "", "", fmt.Errorf("task_submit: "+taskParentMissingFormat, opts.ParentTaskID)
```

**锚点重键（Rule 38 意义上的重新测量，不是复制过去的数字）**：master 未推进
`internal/agent/loop.go`（`git diff --stat f991bf6d..master -- internal/agent/loop.go` 为空），
所以上文 ③ 段在 loop.go 上引入的 38/4 行位移在合并树上**依旧成立**，两处定点随之成立：

- `devdoc/agent-five-organs-ontology.md`：`Agent.NoEnforceMemory` `:2056`（HEAD 实测字段行 `:2187`），
  `GOPI_NO_ENFORCE_MEMORY` `:2054`（HEAD 实测 `boolEnvTrue` 行 `:3455`）。原分支记的 `:2043/:2041`
  是它当时基线 `f991bf6d` 的值（实测 `:2174`）。
- `devdoc/hippocampus.md`：`interruptBase`/`interruptCancel` `:2315`/`:2316`（HEAD 实测字段行
  `:2315`/`:2316`）；`Chat` `:3260`（HEAD 实测定义行 `:4853`）。

本节的数字在合并后的树上**当场重测**；本 doc 不携带 `file.go:NNN` 锚点，docanchor gate 覆盖它
靠的是 DOC-INDEX 的 `path` 证据（`internal/agent/loop.go`），与重做无关。

**合并位移打到的既有 normative doc**：`internal/tools/tasks.go` 因并集多了 36 行（
`TaskSubmitReturningID` 的引入），所有指向该文件的行号锚点整体下移。实测被 gate 判红、且
**对照树上不红**的只有一处 —— `devdoc/task-dependency-order.md` 的 Rule 33 表（同一次运行
的 control tree 归因见下），已按 HEAD 重测重键（`6435→6471`、`6462→6498`、`6580→6616`、
`6494–6496→6530–6532`、`6609→6645`；该节正文的 `6429–6434→6465–6470`；同文件另一表的
`tasks.go:5473`（旧基线的 `TaskSubmitOptions.ParentTaskID`）→ `:5359`）。其余指向该文件的行号锚点
（散落在 audit / branch-task-linkage / merge-backlog-terminal-guard / task-ledger-hygiene /
taskid-vs-sessionid-verdict / verify-responsibility-spec 等文档里）在 master 上**本来就是**
blank-line drift 并被 baseline 收录，合并后同一行仍 blank，因此没有新增。

**control tree 归因（Rule 10.5 / 记忆 #5063 的做法）**：`git archive master | tar -x` 到一个
scratch 目录，跑同一个 `-run TestDocAnchorsResolveInNormativeDocs`，把两边按「doc + anchor」
归一后取差集 —— 分支独有 = 上述 1 处（已修），控制树独有 = 0。

## Delivery-point sync (task 01a10ab4, 2026-10-05 — commit `5846de9e`)

The merge landed on `99e65d50`; master then moved twice while this run was still
closing out (`22540759 feat(cli): classify every top-level verb by concept
dimension, and gate it` + its own delivery-point sync commit `5de18bb6`). That
made the epic's own acceptance sentence — `git merge-base --is-ancestor master
HEAD` rc=0 — **red (rc=1)** through no fault of the branch: it is Rule 30a's
point stated as a measurement, an alignment performed earlier is stale by
construction.

Handling: **sync, never rewrite history.** The two upstream commits touch
`cmd/agent/cli_verb_classify_test.go`, `devdoc/gopi-verb-census.md` and
`internal/config/help.go`; the intersection with this branch's changed-file set
is **empty**, and `git merge --no-ff master` took the `ort` strategy with zero
conflicts (3 files changed, 330 insertions(+), 4 deletions(-)). No `-X ours`,
no `-X theirs`, no rebase, no `-B`/`-f` — the same constraint the union
resolution of `internal/tools/tasks.go` was held to.

Zero master rollback re-measured after the sync: `git diff master HEAD --numstat
-- internal/ cmd/` shows every deletion landing on a line this branch rewrote on
purpose (`internal/agent/loop.go` 38+/4-, `reframe.go` 81+/8-, `reframe_test.go` 259+/11-,
`reframe.go` 14+/5-, `tasks.go` 53+/17-), and the three newly arrived upstream
files carry **zero** deletions here. `git rev-list --count HEAD..master` → 0.

Measured after the sync commit: `go build ./...` rc=0, `go vet ./...` rc=0,
`gopi doccheck` rc=0 (corpus consistent), no `<<<<<<<`/`>>>>>>>` in any file this
branch touched, `git status --porcelain` empty. The branch-scoped smoke
(`make test-budget PKGS='./internal/agent ./internal/tools'`, the declared entry
point) was re-run at load 10.73/8.84/8.16 on 4 cores and streamed to this run's
session directory; its two FAILs are the ones already attributed by control-tree
diff to **pristine master failing identically** (the docanchor gate's 16
pre-existing new-drift entries, and the load-sensitive `run_closeout` flake that
passes `-count=5` under light load) — neither file is in this branch's diff.

### Second sync (2026-10-05, commits `2765cea7` + `78eb8a1d`) — and the docanchor attribution that proves the branch adds no drift

master moved again while this run was closing out (`99e65d50` → `f4798ea0`, ~120 files:
the verb-classification work, the doc-archive relocation, the flake-registry and
docanchor-drift-repair branches). Two of those files are **data files this branch
also edits**, so git handed back one real conflict:

- `internal/agent/testdata/docanchor-baseline.tsv` — conflict. Resolved by
  **union, with the superseded row dropped**: the branch's re-keyed row
  (`devdoc/test-env-isolation.md  blank  loop.go:2966`) is kept; master's comment
  block and its 9 newly measured permits are kept verbatim; master's *twin* row
  (`internal/agent/loop.go:2953`) is deleted, because the re-key in the very same change
  supersedes it — anything else is a redundant permit and a self-inflicted stale
  entry (the gate reports stale permits as errors).
- `devdoc/DOC-INDEX.tsv` — auto-merged. Verified as a union by comparing both
  sides: 0 master-only rows lost; the 77 "missing" branch-side rows all name
  files master relocated into `devdoc/archive/` (`os.path.exists` = false for
  every one), and `gopi doccheck` — Rule 36's authority, run on the merged tree —
  reports the corpus consistent.

**The attribution that matters.** `TestDocAnchorsResolveInNormativeDocs` runs red
on the merged tree (1 new drift, 2 stale permits). Run on a control tree built
from `git archive f4798ea0`, it is **red with the identical counts**
(242 pre-existing permits / 1 new / 2 stale — the same two `devdoc/requirements/cli.md`
stale rows, and the same single new drift — a citation inside master's own
`test-infrastructure.md` row about its flake-registry gate, which the merged tree
blanked; this branch neither introduced it nor touches that file). Branch-unique
drift = 0; merging this feature adds nothing to the gate. The stale permits are
master's own, introduced by its own live branches.

Measured on the final tree (`78eb8a1d`): `go build ./...` rc=0, `go vet ./...` rc=0,
`go test -run 'Reframe|Chain|TaskSubmit' ./internal/agent ./internal/tools` — both
packages `ok`, `gopi doccheck` rc=0, `git status --porcelain` empty,
`git merge-base --is-ancestor master HEAD` rc=0, `git rev-list --count HEAD..master`
= 0.

### Third sync (2026-10-05, commit `d2dd39ea`) — and the drift this branch had put there itself

master advanced once more before hand-over (`f4798ea0` → `1fed197f`, 18 commits).
The merge was **clean** (3 files, no conflict):

- `COMMANDS.md`, `internal/docclass/docclass.go`, `internal/standards/baseline.go` —
  all three were files master had just fixed; the branch's side of them was already
  master's, so the merge kept master's text and took nothing from the branch.

The re-run of `TestDocAnchorsResolveInNormativeDocs` then exposed one attribution
worth recording, because it was **this branch's own prose** and not master's: the
second-sync paragraph above quoted a file-and-line anchor inside a normative doc,
and the docanchor gate counts a mid-sentence `x.go:NNN` as an anchor. That quoted
line had gone blank under f4798ea0, so the sentence that *described* master's drift
manufactured a second copy of it. Fixed by dropping the number from the prose — the
sentence names the gate and the row, never a line — after which the merged tree reports **exactly the control
tree's single new drift** (`devdoc/requirements/test-infrastructure.md`, master's own
row) and the same 2 stale permits: **branch-unique drift = 0**, measured, not
asserted.
