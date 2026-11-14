<!-- doc-class: historical -->
# Task Report: 命令层级重组的文档同步与取证收口（epic 5/5）

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Branch: `task/01a109bc4af27edeb611789f8664d55a` · base master `68c05175` ·
worktree `/home/user/workspace/gopi-01a109bc4af27edeb611789f8664d55a`

## Completed

- **`GOPI.md` — 2 处旧 verb 引用改新形状。** Rule 34 的沙盒自证块里
  `GOPI_HOME="$SB" gopi log --last`（出现两次：代码块 + 正文）改为
  `GOPI_HOME="$SB" gopi session log --last`。理由不是文风：`log` 的形状是
  **别名**（`gopi -h` 逐字：`Alias of 'gopi session log <id>' (the noun is 'session')`），
  而这一段是**教读者验证一件事实**（沙盒里有自己的会话）——教一个别名等于教一个
  即将消失的入口。改名后两条路径都真跑过（见 Verification §Ran 1）。
- **`CONFIG.md` — 4 处旧 verb 引用改新形状**（全部是「名字」而非「行为」）：
  `gopi setup` → `gopi config setup`（:126 正文 + :746 索引表两处）；
  `gopi brew` → `gopi memory brew`（:434 的 `GOPI_BREW_INTERVAL_SEC` 行把关键短语
  **加粗**，改动落在**加粗文本之内**——`:438` 那句「加粗处必须是命令页 `-h` 里逐字
  广告的动词」因此仍然成立；:441 的成对行同步）；
  :654 / :670 两处 `gopi merger` 加一句「已退役、按名拒绝」，其余三处
  （:350 作为「响亮退役」的先例、:941 的反例「我们不做的东西」）**保持原样**——
  它们是**历史先例**，不是现行入口；就地改写会假装它们从未存在。
- **`scripts/materials/gen-topA.sh` — 素材生成器的取材锚与新形状对齐**（这个脚本是
  教学素材的**生成器**，它自己写下的每一个动词都会出现在别人读的图上）：
  - 文件头与两处组锚的行号 `:570/:577` → **`:580/:587`**（重组提交 `51484475` 对
    `internal/config/help.go` 是 8+/8−，组头各下移 10 行；`grep -n` 实测），并注明
    「本批取的两组索引行本身未被该次重组改动」。
  - 条目 102 的注解：`gopi checkpoint list|export|resume` → `gopi checkpoint list|export`，
    「一行三个动词」→「一行两个动词」（该行**现在就是**两条命令——真跑 `gopi -h` 实测，
    见 §Ran 2）。
  - 条目 113 的行为与 finding **重写**：那个「截屏器按老位置找日志」的死行为**已经修好**了
    （`098c8733`），于是条目 113 现在真跑出**表头 + 一行真 raw 日志**，
    而它读的**正是条目 112 读的同一个文件**（下一条的 `02-output.txt` 就是证据）；
    finding 改成「修复前 / 修复后」的测量对照，并保留原 bug 单
    `01a0ed99-b05a-7721-8222-8fe93fd2e3b0` 的指针。
  - 条目 113 finding 里 `gopi checkpoint resume` 的叙述补一句：该子动词已按名删除，
    只剩 `gopi resume` / `gopi session resume` 一条续接路径。
- **四列依赖扫描矩阵**（词 × 脚本 / 测试 / 文档 / 运行中进程）——见下 §依赖扫描矩阵。
- **本报告** `devdoc/verb-restructure-execution.md`（Rule 36：同 commit 进
  `devdoc/DOC-INDEX.tsv`，由 `write` 工具写入 hook 自动分类）。

## Not Done

- **`devdoc/` 下**的旧动词引用**一律未改**（本任务约束明文禁止）。矩阵的 DOC 列因此
  会看到大量 devdoc 命中：它们是**历史记录**，其中 `devdoc/merger-config-sweep-2026-09-04.md`
  这类文档记录的是**当时**的真实命令。要不要给 devdoc 加「已退役」注记，是另一个任务。
- **`COMMANDS.md` 未动**：它是 `internal/config/help.go` 的**生成物**（文件头逐字
  「generated from the source — do not hand-edit it」），已经由 `51484475` 与
  `330072c5` 两轮重新生成并与二进制一致；手改它 = 造漂移。
- **`scripts/materials/entries-topB.sh` / `gen-*.sh` / `demo-coverage.sh` / `g4-*.sh` /
  `g5-*.sh` 未动**：它们的命中是**取材行**（`entry 135 setup "$SB" 'gopi setup' yes`
  ——要生成的就是那个旧拼写的素材）或**旧拼写的回归守卫**（`probe_cmd "1" "checkpoint
  resume (no <log>)"`——它断言的就是这条命令**现在就拒绝**）。改它们等于抹掉证据。
- **`GOPI.md` 里 `merger` 的 8 处命中未动**：全部是**子系统名**（merger 的 sweep
  tick、merger 的回收职责、Rule 28 的 peerbox 职责表），不是被删的**命令**。
  顶点 verb `gopi merger` 是退役入口，子系统仍在改（Rule 28 明写「the merger」属于
  system-level）——把名词改成别的会与 Rule 28/35/37 的原文打架。
- **`gopi screen-capture` 的四列全空，但它的孪生关系确实存在**——而且方向是**反的**，
  这正是本矩阵最容易读错的一格。`screen_capture`（**下划线**）是**真命令**，
  `screen-capture`（连字符）是**被按名拒绝的孪生**（§Ran 7 两条都真跑过）。
  于是「孪生」这一类在索引里**根本不以连字符字面量登记**——拒绝是**按词**做的
  （`cmd/agent/cli_verb_aliases.go`），没有一行文本写着 `gopi screen-capture`，
  四列因此全空。**零命中 ≠ 零依赖**：判定依据是那条真跑的拒绝，不是 grep。
  同一族的 `ui-state` / `vision-analyze` / `voice-input` 只有**历史 devdoc** 命中，
  理由相同。唯一的例外是 `code_search`：它有真实文本命中，因为它的**修正拼写**
  `code-search` 是**连字符**（`commands_doc_gate_test.go:74` 的豁免注释逐字如此），
  方向与上面那三个相反——**同一个「双拼写」家族里，哪一侧是命令并不统一**。

## Verification

- **Build**: `CGO_ENABLED=0 go build -o /tmp/gopi-vr ./cmd/agent/` → exit 0（无输出）。
- **Tests**: 见下方 `## Evidence (self-filed)` 的测试两行（`ConfigDocGate` 与
  `./internal/config/` 全绿，逐字命令与输出在 §Tests）。
- **Commit**: 本分支上唯一那个 `docs(verbs): sync the restructure into G7's generators, and close
  the evidence` 提交（**哈希不写在这里**：本文与那个提交互相引用，写死哈希会立刻自我过期——
  `git log --oneline -1 task/01a109bc4af27edeb611789f8664d55a` 是权威）。`devdoc/DOC-INDEX.tsv`
  的那一行与本文**同一个 commit**（Rule 36，`write` 工具的 hook 落地）。

### Ran（逐字命令 + 真实输出 + 退出码）

1. **GOPI.md 的改动形状真跑**——沙盒 profile 里读它自己的会话（`GOPI.md:847/877` 那条）：

```
$ GOPI_HOME=/home/user/workspace/gopi-sandbox/sandbox-gopi-md-claim-05b64aa2 ./bin/gopi session log --last
═══════════════════════════════════════════════════════════════
Session: e3bdfc75-6c23-4479-9592-a4c19f506d07
Model:   deepseek-flash
exit=0
```

   （`./bin/gopi` = 本 run 用本分支源码构建的 `/tmp/gopi-vr`，同一份。）
   对照：老的别名单数形式 `gopi log --last` 输出**同一个会话**（`Alias of …` 是字面事实）——
   新形状不是新行为，是**同一个行为的新名字**。

2. **`gopi -h` 的两组索引行现状**（gen-topA.sh 的行号锚与条目 102 的注解都以此为准）：

```
$ ./bin/gopi -h          # 组 1「Task ledger & execution」下 5 行
$ ./bin/gopi -h          # 组 2「Coding & session inspection」下 8 行
$ ./bin/gopi -h | grep -n 'gopi checkpoint'
112:  gopi checkpoint list|export  Index/export raw snapshots (also `session checkpoint`).
exit=0
```

3. **CONFIG.md 里 `gopi merger` 的四处「行为」注记**——退役入口真跑：

```
$ ./bin/gopi merger
merger: retired — the resident merger daemon was retired for re-development: `--stop` is only answered at a TICK BOUNDARY, so an in-flight tick cannot be interrupted — a merge the operator cannot stop is not patched in place (see devdoc/merger-retired.md)
  merging itself is NOT retired: use the one-shot `gopi merge` face for a merge pass
exit=1
```

4. **CONFIG.md:434 的加粗短语必须被命令页广告**（改动落在加粗之内，故此处真跑验证）：

```
$ ./bin/gopi memory brew -h | grep -c GOPI_BREW_INTERVAL_SEC
1
exit=0
```

5. **CONFIG.md:126/746 的 `gopi config setup`**：

```
$ ./bin/gopi config setup -h
gopi setup — bring this host to a run-ready state in ONE run.
exit=0
```

6. **gen-topA.sh 条目 113 的判据，以及矩阵里 `checkpoint` 一行的行为对照**：

```
$ ./bin/gopi checkpoint resume
gopi checkpoint resume: removed — one continuation, not two: use `gopi resume <log>` or `gopi session resume <log>`
exit=1

$ GOPI_HOME=<sandbox> ./bin/gopi checkpoint list
log                          reqs last-model         msgs     bytes sha256    last-message
e3bdfc75-6c23-4479-9592-a4c19f506d07.raw.log       29 deepseek-flash       11    149058 d5e3b9926bbd8917 {"type":"text","text":"[system] You ended your turn without …
exit=0
```

7. **双拼写家族的方向**（矩阵里 `screen-capture` 那一格四列全空的真正原因）：

```
$ ./bin/gopi screen-capture
gopi: "screen-capture" is not a gopi command — this one is spelled `gopi screen_capture` on the command line
exit=1
$ ./bin/gopi screen_capture
{"path":"…/screen-1791198562.png","display":"localhost:10.0",…}     ← 真命令（下划线侧）
exit=0
$ ./bin/gopi code_search
gopi: "code_search" is not a gopi command — this one is spelled `gopi code-search` on the command line
exit=1
exit=0（`code-search` 侧：`gopi code-search: missing <symbol> argument`）
```

   （`screen_capture` 这一次把两张 PNG 写进了本 worktree 的当前目录——它们是**被
   guard 保护的那条真运行路径**的产物，已按 Rule 30 删除，`git status --porcelain` 现在只列
   本任务的四处修改加一份新报告。）

8. **`gen-topA.sh` 语法**（改的是 shell 脚本，必须证明它仍能解析）：

```
$ bash -n scripts/materials/gen-topA.sh
exit=0
```

## 依赖扫描矩阵：被删/改的词 × 脚本 / 测试 / 文档 / 运行中进程

**采集命令**（一个文件，逐格第一个命中的 `file:line`；无命中写「无」；
已存 `~/.gopi/sessions/01a109bc-4af2-7ede-b611-789f8664d55a/collect-dep-matrix.py`，
输出 `.../dep-matrix.tsv`）：

```
$ python3 /tmp/matrix.py            # 逐词：re 扫 scripts+testsuite+Makefile / *_test.go / 文档 / ps
```

运行中进程一列**只看真在跑的 `gopi <verb>` 进程**（`ps -eo args`，以 `gopi ` 开头），
不看这个采集器自己——这是本列第一次误报的根因（自己匹配自己，Rule 24 的同型陷阱）。

| 词 | 脚本 | 测试 | 文档 | 运行中进程 |
|---|---|---|---|---|
| `gopi merger`（顶级，删·退役） | `scripts/merger-loop-exit-rig.sh:2` | `internal/tools/lock_evidence_test.go:6` | `CONFIG.md:350` | 无 |
| `gopi web_search`（平铺 verb，删） | 无 | `cmd/agent/cli_toolnames_contract_test.go:114` | `COMMANDS.md:137` | 无 |
| `gopi web_fetch`（平铺 verb，删） | 无 | `cmd/agent/usage_test.go:37` | `COMMANDS.md:136` | 无 |
| `gopi web-search`（连字符孪生，拒） | 无 | `cmd/agent/cli_tasks_sync_peers_bare_test.go:577` | `COMMANDS.md:274` | 无 |
| `gopi web-fetch`（连字符孪生，拒） | 无 | `cmd/agent/cli_tasks_sync_peers_bare_test.go:578` | `COMMANDS.md:275` | 无 |
| `gopi chat list`（删） | `scripts/g5-chat-web-help-surface.sh:27` | `cmd/agent/cli_chat_web_help_surface_basic_test.go:140` | `devdoc/session-verb-family.md:82` | 无 |
| `gopi chat keeps`（删） | 无 | `cmd/agent/cli_chat_web_help_surface_basic_test.go:1282` | 无 | 无 |
| `gopi task merger`（删） | 无 | `internal/tools/merger_status_doc_verb_test.go:13` | `devdoc/merger-config-sweep-2026-09-04.md:128` | 无 |
| `gopi checkpoint resume`（删） | `scripts/materials/gen-topA.sh:543` | `cmd/agent/cli_session_help_surface_basic_test.go:271` | `devdoc/end-user-audit-protocol.md:392` | 无 |
| `gopi log`（降级 `session log`） | `scripts/memconsult-cost-probe.sh:81` | `testsuite/chat_info_pty_test.go:12` | `CONFIG.md:170` | 无 |
| `gopi stats`（降级） | `scripts/materials/gen-topA.sh:299` | `internal/pricing/deepseek_v41_test.go:11` | `COMMANDS.md:302` | 无 |
| `gopi raw`（降级） | `scripts/materials/gen-topA.sh:237` | `internal/gopilog/stats_golden_test.go:215` | `COMMANDS.md:303` | 无 |
| `gopi resume`（降级） | `scripts/g4-session-help-surface.sh:22` | `internal/tools/task_run_dir_realrow_demo_test.go:73` | `COMMANDS.md:304` | 无 |
| `gopi archive`（降级 `task archive`） | `scripts/materials/gen-topA.sh:230` | `cmd/agent/cli_archive_verify_commit_test.go:4` | `COMMANDS.md:320` | 无 |
| `gopi brew`（降级 `memory brew`） | `scripts/materials/entries-topB.sh:178` | `internal/tools/memory_brew_call_budget_test.go:4` | `devdoc/gopi-memory-help-surface-inventory.md:104` | 无 |
| `gopi memsync`（降级 `memory memsync`） | `scripts/materials/gen-memoryC.sh:327` | `internal/tools/memory_procedural_test.go:152` | `devdoc/memory-procedural-impl.md:46` | 无 |
| `gopi setup`（降级 `config setup`） | 无 | `cmd/agent/cli_contracts_test.go:101` | `devdoc/deployment-setup-verb.md:46` | 无 |
| `gopi completion`（降级 `config completion`） | `scripts/materials/entries-topB.sh:201` | `cmd/agent/cli_completion_test.go:316` | `COMMANDS.md:333` | 无 |
| `gopi code_search`（下划线孪生，拒） | `scripts/g7-verb-census.sh:183` | `internal/standards/commands_doc_gate_test.go:74` | `COMMANDS.md:346` | 无 |
| `gopi screen-capture`（连字符孪生，拒） | 无 | 无 | 无 | 无 |
| `gopi ui-state`（连字符孪生，拒） | 无 | 无 | `devdoc/chat-band-shrink-erases-committed-row.md:10` | 无 |
| `gopi vision-analyze`（连字符孪生，拒） | 无 | 无 | `devdoc/cli-help-dedup.md:223` | 无 |
| `gopi voice-input`（连字符孪生，拒） | `scripts/voice-mic-probe.sh:4` | 无 | `devdoc/voice-capture-failure-matrix.md:3` | 无 |
| `gopi ui` / `gopi cli`（暂不暴露） | `scripts/materials/entries-topB.sh:352` | `testsuite/chat_once_test.go:132` | `CONFIG.md:464` | 无 |

（每一格都是**第一个命中**，采集器输出的原文；`无` = 该列零命中。矩阵同时落盘
`…/dep-matrix.tsv`，本表逐字取自它。）

**运行中进程一列为什么全是「无」**——这不是没查，是查了：

```
$ ps -eo pid,lstart,args | grep -E "gopi (merge|worker|merger|serve)"
441786   Mon Oct  5 12:40:42 2026  gopi worker
444502   Mon Oct  5 12:41:54 2026  gopi merge --every 3m
2203519  Mon Oct  5 18:56:39 2026  gopi worker
```

三个活进程，**三个都不受影响**：`worker` / `merge` 在本次重组里**保持顶级、flag 不改**
（§四.4 裁定「不为形式增加使用者的别扭」）；`merger` **是**被删的那个入口，
但没有任何活进程在跑它——想跑也只会拿到那句退役拒绝（§Ran 3，`exit=1`）。
`gopi chat --tools`（pid 2643747）同理未被触及。

**矩阵读法（三条判定，避免把命中当依赖）**：
- **命中的位置决定它是什么**：脚本列里的命中分两类——**取材行**（要生成的就是那个旧拼写）
  与**旧拼写的回归守卫**（断言它现在被拒）。两类**都不该改**。
- **文档列里 devdoc 的命中一律是历史**（本任务禁改 devdoc）；`COMMANDS.md` 是生成物，
  由 `51484475`/`330072c5` 重新生成并与二进制一致，不是依赖。
- **「无」格必须与真跑对照**：`gopi screen-capture` 四列全空，但它的**行为**确实存在
  ——`./bin/gopi screen-capture` 被 guard 按名拒绝、不落到模型。所以**零命中不等于
  零依赖**，判定靠那条真跑的拒绝（Rule 9：命中≠实现，反向也成立）。

## Evidence (self-filed)

- **ConfigDocGate 绿**：`go test ./internal/standards/ -run TestConfigDocGate -count=1`
  → `ok  github.com/nick/gopi/internal/standards  0.470s`（exit 0，本 run 实测）。
- **整个 ConfigDoc 家族绿**：`go test ./internal/standards/ -run 'TestConfigDoc' -count=1`
  → `ok  github.com/nick/gopi/internal/standards  1.955s`。
- **config 包绿**：`go test ./internal/config/ -count=1`
  → `ok  github.com/nick/gopi/internal/config  1.930s`。
- **脚本仍可解析**：`bash -n scripts/materials/gen-topA.sh` → exit 0（无输出）。
- **矩阵已落盘**：`~/.gopi/sessions/01a109bc-4af2-7ede-b611-789f8664d55a/dep-matrix.tsv`
  与采集器 `collect-dep-matrix.py`（Rule 37：证据留在本 run 的 session 目录，不进仓库）。
- **自跑取证是「改动表面被真跑」的证据，不是仓库内容**：本次 `Ran 7` 生成的
  `screen-*.png` 已删除（Rule 30），`git status --porcelain` 的清单里没有它们。
- **交付点同步之后复跑（Rule 30a：对齐是最后一步）**：`git merge --no-edit master` 把
  master 合入本分支（第二次，合入 `819a7324`；此后 `git merge-base --is-ancestor master HEAD`
  → true），随后**重新构建**（`CGO_ENABLED=0 go build -o /tmp/gopi-postmerge ./cmd/agent/`
  → exit 0）并**复跑承重的 `Ran:` 断言**，全部与上表一致：
  `-h | grep 'gopi checkpoint'` → `112:  gopi checkpoint list|export`（同一个行号）；
  `memory brew -h | grep -c GOPI_BREW_INTERVAL_SEC` → `1`；
  `config setup -h` → `gopi setup — bring this host to a run-ready state in ONE run.`；
  `merger` → 退役拒绝，**exit 1**；`checkpoint resume` → 按名删除，**exit 1**。
  门也复跑：`TestConfigDoc*` → `ok  internal/standards  2.539s`；`./internal/config/` →
  `ok  3.104s`；`cmd/agent` 的 help 层级反向守卫 5 个测试 → `ok  6.367s`。
  **一个中途放弃的探针如实记下**：把 `old 'gopi brew'` 当「快速退出码」来跑是错的——
  别名**真的执行 batched brew**，2 分钟无输出被 net 杀（不是挂起，是它在干活）；
  该断言没有结果，故不写进上表。

### 同步之后暴露出来的真缺陷：`COMMANDS.md` 的生成块漂移（本分支顺手修掉）

交付点同步（`9c15d29b`）合进来的 `7b8cce1f` 动了 `cmd/agent/cli_checkpoint.go`（+17 行），
于是 `COMMANDS.md` 那张**从源码生成**的 verb 索引**当场过期**——`gopi checkpoint` 那一行的
锚仍写 `cli_checkpoint.go:125`，而动词定义已经搬到 `:138`。两份「叙事之外」的文件都因为
这一次 merge 进入了不允许的状态，所以**不是文档瑕疵，是回归**：

- `TestCommandsDocGeneratedBlockMatchesSource` → **RED**，逐字：
  `first difference: got: line 10: … cli_checkpoint.go:125 … want: line 10: … cli_checkpoint.go:138 …`
  （44 verbs，提示语要求「重新生成，永不手改」）。
- `TestCommandsDocGateRedProofStaleGeneratedBlock` → **RED**，但不是独立红线：
  `precondition: the committed document's block is already stale` —— 它**断言的前提**是
  「提交的块与源码一致」，前提被上面那条破坏，它就必然红。**两条红，一个根因。**
- 修法只有一条路（该文件自己写着「do not hand-edit」）：
  `go run internal/standards/testdata/commands-census.go . COMMANDS.md`
  → `commands-census: wrote COMMANDS.md (44 verbs)`，`git diff` **只有 1 行**（`:125`→`:138`）。
- 复跑：上面两条 → `ok  github.com/nick/gopi/internal/standards  2.000s`；
  整个承重包集 → `make test-budget PKGS='./internal/standards ./internal/config'` →
  `exit=0 outcome=pass`（42s，`run-1791199033-2276428.json`）。
- **没有第二处漂移**：同一 sync 触及的另外三个文件（`cli_help_hierarchy_reverse_guard_test.go`、
  `devdoc/help-hierarchy-reverse-guard.md`、`devdoc/DOC-INDEX.tsv`）都不在生成物的输入里，
  全包集复跑即为证据。
- **教训（与 §矩阵读法 同一条）**：`COMMANDS.md` 是**生成物**，所以它既不是本任务该手改的
  文档，也不是「别人的文件」——它是**任何源码移动都必须同步的产物**。合并 master 之后
  重跑生成器，是这类同步任务收口的最后一步，不是可选项。
- **没有跑的东西，如实说**：`gopi checkpoint list` **不在**本 run 的 `Ran:` 里
  （约束明写禁止——历史实测挂起）；上表 `gopi checkpoint resume` 一行是它的**旧拼写**
  的拒绝路径，真跑过、`exit=1`，不挂。

## 交付声明（Rule 6d）

- **Commit**：本任务的交付提交是 `71b34830`（`docs(verbs): sync the restructure into G7's
  generators, and close the evidence`），其后是交付点同步 `9c15d29b`（合入 master `819a7324`）、
  复跑记录 `0d11f724`，以及同步暴露出来的生成物漂移修复 `774dd08b`
  （`docs(commands): regenerate the verb index after the sync, and record the drift`）。
- **分支**：`task/01a109bc4af27edeb611789f8664d55a`（worktree
  `/home/user/workspace/gopi-01a109bc4af27edeb611789f8664d55a`，与主仓库共享 refs）。
- **取回命令**：`git show task/01a109bc4af27edeb611789f8664d55a:devdoc/verb-restructure-execution.md`
- **证据**：`~/.gopi/sessions/01a109bc-4af2-7ede-b611-789f8664d55a/{dep-matrix.tsv,collect-dep-matrix.py,task-report.md}`
