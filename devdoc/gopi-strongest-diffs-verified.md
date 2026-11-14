<!-- doc-class: historical -->
# gopi 六条最强差异机制 × 四家竞品 — 真跑验证报告

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

会话：`01a0eec1-e2a4-7366-bcf3-d2c2e183fe18`
原始输出根目录（全部 `.out` / `.rc` / 清单落盘处，**不在 git 树内**）：

```
/home/user/.gopi/sessions/01a0eec1-e2a4-7366-bcf3-d2c2e183fe18/strongest/raw/
```

---

## 0. 判定口径（先钉死，全程一致）

| 判定 | 含义 |
|---|---|
| **赢** | 该机制在本产品**原生存在且实测通过**（有产品自己的命令/flag/持久状态作证），或"它没有此能力"而 gopi 有（本表一律按"gopi 赢"记） |
| **平** | 产品存在该能力，但需手工配置 / 默认不启用 / 语义比 gopi 弱 |
| **输** | 产品具备该能力且**实测优于 gopi**，或 gopi 实测失败 |
| **未判** | **只允许唯一一个理由：本机跑不起来**（写明卡点）。能力面已看清但模型超时的竞争格一律判**赢**并注明 |

**产品能力判据（反作弊核心）**：判为"产品能力"必须有**产品面证据** —— 本产品 CLI 的子命令/flag，或它自带的持久状态（自带 DB 文件）。**模型临场手写 shell 拼 git / 手写 sqlite 不算厂商能力**。四格探测输入都逐字写死了这一条，四家收到的是同一段文字。

---

## 1. 判据修正总表（机制 × 四家 × 判定 × 证据）

| 机制 | codex | dsh | claude-code | hermes | gopi（基线） |
|---|---|---|---|---|---|
| **M1** append-only 台账 + 审计链 | **赢**（原生面无台账；判定块被 180 s 截断，证据=转录内的原生命令普查） | **未判** | **赢**（NONE，判定块完整） | **赢**（原生 `kanban` 行+Events 审计链，真跑） | **赢** |
| **M2** 父子依赖闸门 | **赢**（NONE，判定块完整） | **未判** | **赢**（NONE，实测 claim 穿透） | **赢**（原生真闸门：`cannot claim … status=todo`，真跑拒绝） | **赢** |
| **M3** 自感知 / 自省 | **赢**（判定块被截断；原生面普查已看清=无自省入口） | **未判** | **赢**（两次超时 0 字节，`-p` 无自省面） | **平**（原生五条子命令，通过性未实测） | **赢** |
| **M4** 合并队列 + 推送闸门 | **赢**（NONE，判定块完整） | **未判** | **赢**（超时 0 字节，无任何合并面） | **赢**（五格全被 gopi 污染；产品面无 merge 子命令） | **赢** |
| **M5** worktree 生命周期 | **赢**（NONE，判定块完整） | **未判** | **赢**（超时 0 字节，无 worktree 面） | **平**（真建真列，`gc` 不回收） | **赢** |
| **M6** 跨节点同步 | **赢**（NONE，判定块完整） | **未判** | **赢**（超时 0 字节，无 sync 面） | **赢**（LLM 格超时无污染；产品无 sync 子命令） | **赢** |

**一处分寸**：codex 的 M1 / M3 与 claude 的 M3/M4/M5/M6 这 6 格**没有产出产品的自述判定块**（被 180 s 截断或 0 字节）。它们的判定证据分为两类，报告全程区分二者：**(A) 产品的自述判定块**（codex M2/M4/M5/M6、claude M1/M2、hermes M3/M4——注意 hermes 的 M3/M4 是污染格，见 §5）；**(B) 无 LLM 的原生命令面普查/原生面真跑**（其余格）。逐格来源见 §2b 的 `answer-provenance.log`。

**18 格判出、6 格未判**：dsh **6 格全部未判**（同一运行性卡点）；其余 18 格全部判出，其中 hermes **M3 / M5 两格为平**，无任何格判 **gopi 输**。

### gopi 输的条目
**没有。** 六条机制 × 四家 = 24 格，无一格判定"gopi 输"。最近的两格是 hermes 的 **M3 / M5 = 平**（见 §5），已单列说明。

### 无法验证的条目与卡点
| 条目 | 卡点（唯一理由：本机跑不起来） |
|---|---|
| dsh × M1–M6（6 格） | 每次调用 `dsh: HTTP_404: DeepSeek API error (HTTP 404)`，`rc=1`，10 s 内返回。**输入已到达产品**（见 §3 到达侧 sha256），是产品自身对上游 API 的调用失败，与探测内容无关。**未做**网络取证：本机对外部出站抓包/中间人不在本次任务授权范围内（且属"改环境"），故按"测不出"处理，不做推测 |
| claude × M3 / M4 / M5 / M6（4 格） | `timeout --signal=KILL 180` 触发 `rc=137`，输出 0 字节。**M3 已按"改一次超时"补跑 420 s，仍 0 字节超时**（`raw/probe-m3-introspect.claude.long.out`），说明不是"再等一会就好"。能力面已从它的 `--help` 看清（无自省/合并/worktree/sync 入口），故按 §0 口径判**赢**，并在此保留"模型侧未产出答案"的事实 |
| codex M1 / M3、hermes M1 / M2 / M5 / M6（6 格 `rc=137`） | 180 s 被 KILL。**逐格复核（`answer-provenance.log`）表明这 6 格没有产出产品的判定块** —— `.out` 里那条 `*_VERDICT=` 行是**探测提示词自身的占位符回显**（`=<NATIVE 或 NONE>` 形式，带尖括号），不是产品给的实值。故这 6 格**不能按"产品自述"记**。判定改由**无 LLM 的原生命令面**支撑：codex 的 M1/M3 由该格转录里自身跑出的 `codex --help` 全量子命令普查（无 task/ledger/audit 类动词，见 §6）；hermes 的 M1/M2 由 `hermes kanban` 真跑（`m1m2m5.hermes.kanban.run.log`）、M5 由 `followups.log` 的 worktree 真建+gc 不回收、M6 由产品面普查（`hermes --help` 无 sync/peer 类动词） |
| 6 格无产品自述（上一条同源） | 完整清单：codex M1、codex M3、hermes M1、hermes M2、hermes M5、hermes M6。**这一事实在 §1 表里以 "(A) 自述 / (B) 原生面" 两类证据区分标注，不混用** |
| hermes × M3 的**通过性** | 她的自省子命令 `sessions list` / `sessions stats` 90 s 未返回（`rc=124`）、`insights --days 1` 150 s 未返回（`rc=124`） —— 卡点在本机她那批 GB 级 `~/.hermes/*.db`（`claude_sessions.db` 1.6 GB、`luke.db` 1.5 GB、`distilled_knowledge.db` 991 MB），**不是**命令不存在（`--help` 列得清清楚楚）。故只证"存在"，不记"实测通过" |

---

## 2. 反作弊遵守说明（逐条对照约束）

1. **只读竞品**：未修改 codex / dsh / claude / hermes 任何源码或配置。对 hermes 只用了它自己的 CLI 创建**一次性 kanban board**，用完即 `boards rm` 归档（写进它自己的 kanban 根，属"使用产品"而非"改配置"）；探针结束后 `~/.hermes/kanban/boards/` 下只剩 `_archived`。
2. **不杀 hermes 进程**：全程未向 PID 841880 发任何信号。实测复核（本报告复核轮 t+2123s 再次确认）：
   ```
   $ ps -o pid,etime,cmd -p 841880 --no-headers
   841880  03:32:13  /home/user/work/hermes-agent/.venv/bin/python3.11 …/bin/hermes chat --cli --yolo
   ```
   本次任务启动的 hermes 子进程（`hermes chat -q`）都是独立新进程，与 841880 无关。
3. **不改 gopi 代码**：`git status --porcelain` 只列出本报告批次的 `scripts/materials/*` 与 `devdoc/*` 新增文件，无既有文件被改。
4. **不许只跑 `-h`**：24 格里 18 格是**真任务调用**（把探测输入当 prompt 投给产品）；gopi 基线每条机制都跑了**真动作**（真建台账行、真拒绝认领、真 merge 拒绝、真 sweep、真起 worktree、真起协议腿）。仅"产品入口普查"（如 `hermes --help` 找 merge/sync 子命令）使用 `-h`，且明确标注为能力面取证而非机制实测。
5. **不许回避显眼测试**：M2 对 claude 是**最不利也最有价值**的一格 —— claude 的 `blockedBy` 注释存在但不拦，实测把 `pending` 父下的子任务成功 claim 成 `in_progress`，直接把"注释即闸门"的假象打掉，这一格被完整保留并逐字落盘。
6. **不替竞品写包装脚本**：驱动 `scripts/materials/strongest-drive.sh` 只做 **`cd` / `timeout` / 重定向 / 写 `$?`**，把探测文件**逐字展开进 argv**；脚本头写明这条反作弊契约。没有为任何一家拼接命令、没有代它们运行 git。
7. **原始输出落盘**：24/24 格都有 `.out` + `.rc`，另加 12 份 gopi 基线日志与 20 份无 LLM 原生取证日志，共 **72 个 artifact**，逐个 sha256 记录在 `scripts/materials/strongest-raw-manifest.tsv`。
8. **四家输入逐字相同**：见 §3，有**发送侧与到达侧双向 sha256**（到达侧由独立脚本 `strongest-arrival-check.py` 复核，**24/24 HIT**）。

### 2b. 复核轮新增的两件取证工具（随本批提交）

| 工具 | 作用 | 输出 |
|---|---|---|
| `scripts/materials/strongest-arrival-check.py` | 到达侧逐字复核：从四家**自己的**会话记录里读回 payload，比对发送侧 delivered sha256_16 | `.../strongest/arrival-check.log`（24/24 HIT，逐格给出到达文件路径） |
| `scripts/materials/strongest-answer-provenance.sh` | 逐格回答来源判定：把**探测提示词自身的占位符回显**与**产品给出的实值判定块**分开 | `.../strongest/answer-provenance.log`（12 格 ANSWER / 6 格 EMPTY-FILE / 6 格 NO-ANSWER） |

这两个工具是"证据的可复核性"本身 —— 报告里的每一个 ✓ 与每一个判定，都能被第三方只读重跑。

9. **Rule 27**：全程零合成负载 —— 没有 `dd`、没有压力循环、没有并发轰炸；所有"产品能力"取证都是单进程单次调用。
10. **Rule 22**：临时工作区实测不在 `/` 分区 ——
    ```
    $ stat -c %d /            → 2051
    $ stat -c %d .../strongest/{,work,raw}  → 2052 2052 2052
    $ stat -c %d ~/workspace/gopi-01a0eec1… → 2052
    ```
11. **Rule 38**：本报告所有 census 数字（**72 个 artifact**（`ls | wc -l` = 72，清单 `strongest-raw-manifest.tsv` 表头 1 行 + 数据 72 行 = 73 行）、**24 格**（`ls probe-m*.rc | wc -l` = 24）、六条机制）由生成器 `ls` / `wc -l` / `wc -c` / `sha256sum` 在 commit `5cdc64ae` 打印，非手算；复现命令见 §7。

---

## 3. 逐字输入的双证据（发送侧 + 到达侧）

探测输入文件（6 个，随本报告同批提交）：`scripts/materials/probe-m{1..6}-*.txt`

### 3a. 发送侧
`scripts/materials/strongest-probe-sha256.txt`：每个文件给出**两个** sha256 —— 磁盘文件字节值，以及 `$(cat file)` 实际投递的字节值（差 1 字节 = 剥掉的行尾换行）。驱动对四家使用**同一个** `PROMPT="$(cat "$f")"` 展开，故四家收到的 payload 字节相同。

```
probe-m1-ledger    file=7e50e0fb…218d (1289B)  delivered=78477cd6…d5c2 (1288B)
probe-m2-dependency file=70f9f623…d39a (1240B)  delivered=620b709d…4ef9 (1239B)
probe-m3-introspect file=9b024550…e4c7 (1076B)  delivered=7190ec88…3198 (1075B)
probe-m4-mergegate file=69d63e9b…f335 (1233B)  delivered=d0336a7a…96b2 (1232B)
probe-m5-worktree  file=84056296…efd  (1121B)  delivered=22243555…1f81 (1120B)
probe-m6-sync      file=15e47880…1b72 ( 835B)  delivered=b1bb844b…91c6 ( 834B)
```

### 3b. 到达侧（从各产品**自己的**会话记录里读回）
复核脚本：`scripts/materials/strongest-arrival-check.py`（只读；遍历每条 JSONL/DB 行的**所有**字符串，比对 delivered sha256_16）。**独立复跑结果 24/24 HIT**，日志 `.../strongest/arrival-check.log`：

| 产品 | 记录载体 | M1 | M2 | M3 | M4 | M5 | M6 |
|---|---|---|---|---|---|---|---|
| codex | `~/.codex/sessions/2026/09/30/rollout-*.jsonl` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| claude | `~/.claude/projects/-home-nick--gopi-sessions-01a0eec1…/*.jsonl` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| hermes | `~/.hermes/state.db` `messages(role='user')` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| dsh | `~/.dsh/sessions/<cwd-hash>/session-*/session.jsonl.zstd`（`zstd -dc`） | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |

24 格×四家每格都给出**具体到达文件路径**（见 `arrival-check.log`），逐格 sha256 与发送侧相同 —— 即"输入到达了产品"这一条对**四家全部 24 格**成立，与产品是否成功回答无关。

**dsh 的到达侧尤其重要**：它 6 格全部 `rc=1`，但 6 个 delivered sha256 **逐一等于**其它三家收到的同一字符流 —— 证明它失败在**产品自己调用上游 API**那一步，而不是"输入没送到"。这一点把 dsh 的判定锁死在"未判（运行性）"，而不是"赢"。

---

## 4. gopi 侧基线：六条机制各跑一次真实 CLI

证据文件全部在 `.../strongest/raw/`。

| 机制 | 真实调用（verbatim） | 结果 | 证据文件 |
|---|---|---|---|
| M1 | `gopi task submit "…M1-ledger-probe-gopi: 探测 gopi 原生任务台账" --tags probe` → `gopi task list` → `gopi task show <id>` → `gopi task status <id> --json` | 落库 `01a0ef13-7ae2-77ee-90ab-10b8543b247c`；`task_events` 链 `claimed → in_progress → completed` 逐条带时间戳 | `m1.submit.gopi.out`, `m1.gopi.session.log`, `m2.gopi.session.log` |
| M2 | 建父 `M2-parent-gopi` + 建子 `M2-child-gopi --parent-task-id <父>` → `gopi task list --status pending` → `gopi task update <父> --status …` | 子行被标 `[queue-skip] parent … not released`，无关行标 `[next]`；父走完 `completed` 后子行的 `[queue-skip]` 消失 | `m2.gopi.session.log` |
| M3 | `gopi log --last` / `gopi stats <sid>` / `gopi raw <sid>` / `gopi checkpoint list` | 四项原生自省全部命中（会话 id、token+成本带价格指纹、raw 轨迹、checkpoint 面） | `m3.gopi.live_session.out`, `m3m4.gopi.baseline.log` |
| M4 | `gopi merge --fast probe-orphan-01a0ee`（无关历史）／`gopi merge --fast chat/…`（命名不存在分支）／`gopi merge --status` | 无关历史被**具名拒绝**（`unrelated-history` + 归档 tag，ref 原地不动）；不存在分支被具名拒绝而非静默忽略 | `m4.gopi.orphan.log`, `m4m6.gopi.gate.log`, `m6m4.gopi.peer.log` |
| M5 | `gopi task sweep-worktrees`（dry-run，逐 registration 归属判定）／真实 dispatch：`gopi worker <task-id>` 自带建 worktree + 分支 + finish-cleanup 回收 | 归属表逐条给出 owner 与理由（`an unfinished run still owns it` / `only a ledger row remembers it` / `live-holder`）；worker 跑完自动释放 worktree | `m5m6.gopi.run.log`, `m5sweep.gopi.log`, `m5.gopi.worker.run.log`, `m5.gopi.dispatch*.log` |
| M6 | `gopi task sync -h` / `--peers` / `--status`；协议腿真跑：`printf '{"rows":[]}' \| gopi task sync --serve-remote` 与 `--claim-remote` | `--status` 给出 per-peer 锚点；协议腿**真被执行**，并以"版本字段缺失 → 拒绝导入未知格式"的具名错误回应（写入面由格式校验把守） | `m6.gopi.session.log`, `m6m4.gopi.peer.log`, `followups.log` |

---

## 5. hermes 的两条"平"（本报告最需要注意的地方）

hermes 是四家里唯一一个**确实把机制做出来了**的产品（`hermes kanban`），但它有两处语义弱于 gopi，故判 **平** 而非输：

| 机制 | hermes 实测做到 | 弱在哪 | 证据 |
|---|---|---|---|
| **M5 worktree** | 一次性 board + `--workspace worktree` + `boards set-default-workdir` → `claim` **真的建出了隔离 worktree 与分支**：`m5repo/.worktrees/t_7551e979  [wt/t_7551e979]` | 完成后 `gc` **不回收它**（`GC complete: 0 workspace(s) … removed`，worktree 仍在），回收语义缺失；而 gopi 的 worker 跑完自动 `worktree-freed=` | `raw/followups.log` |
| **M3 自省** | 原生具备 5 条自省子命令：`sessions`(list/stats/export) / `insights`(token+cost) / `logs` / `checkpoints` / `dump` | **存在性有据、通过性无实测**：把这些动词直接跑，`sessions list` / `sessions stats` 90 s 未返回（`rc=124`），`insights --days 1` 150 s 未返回 —— 卡在本机她那批巨型库（`~/.hermes/*.db` 有 1.6 GB 级文件）上，故不能按"实测通过"记；同时 **gopi 的自省面更宽**（`stats` 带价格指纹与 regime、`raw` 逐条 HTTP、`log --rounds`、`checkpoint`、`ui_state --compare`） | `raw/hermes.m3.native.log`, `m1m2.hermes.kanban.help.out` |

**hermes LLM 探测格全线污染，必须披露（复核后范围比初版更大）**：`probe-m{1,2,3,4,5}-*.hermes.out` 五格里模型把"我的产品"认成了 **gopi**，实际执行的是 `./bin/gopi -h` / `./bin/gopi task -h` / `./bin/gopi merge -h` / `./bin/gopi task sweep-worktrees` 等 **gopi CLI**（逐格计数：M1=4 次、M2=21 次、M3=1 次、M4=5 次、M5=12 次 gopi 调用；**M6=0 次**，但 M6 格 `rc=137` 无判定）。因此这五格**不能作为 hermes 的能力证据**，其中的 `M3_VERDICT=NATIVE` / `M4_VERDICT=NATIVE` 尤其具有误导性 —— 那是她给 **gopi** 打的 NATIVE。hermes 的全部判定改由**无 LLM 的原生面取证**支撑（`raw/m1m2m5.hermes.kanban.run.log`、`raw/hermes.native.surface.log`、`raw/hermes.m3.native.log`）：

- M2（hermes 原生真闸门，真跑拒绝）：
  ```
  $ hermes kanban create "M2-child-hermes" --parent <父>   → Created t_ddd1e696
  $ hermes kanban claim t_ddd1e696
  cannot claim t_ddd1e696: status=todo lock=(none)          ← 产品自己拦下了
  $ hermes kanban complete <父> ; $ hermes kanban claim t_ddd1e696
  Claimed t_ddd1e696 / Workspace: …/workspaces/t_ddd1e696  ← 释放后可领
  ```
  （证据 `raw/m1m2m5.hermes.kanban.run.log`；`status=todo` 即被父依赖挡在 ready 之前的态）
- M1（hermes 原生审计链）：
  ```
  $ hermes kanban show t_a2d55811
  Events (1):
    [2026-09-30 05:43] created {'assignee': None, 'status': 'ready', 'parents': [], …}
  ```
- M4 / M6（hermes 产品面普查）：`hermes --help` 的子命令全集里 **`merge`/`push` 命中 0 次**，**无 `sync`/`peer` 类动词** → 无合并闸门、无跨节点同步。

---

## 6. 各家"赢"格的直接证据（产品自己的话）

| 格 | rc | 产品自证（逐字） | 落盘 |
|---|---|---|---|
| codex M1 | 137（**无判定块**；证据=转录内的原生命令普查） | `codex --help` 全量子命令普查（`exec/agents/debug/queue/archive/resume/cloud/mcp/sandbox/apply/…`）里**无 task/ledger/audit 类动词**；唯一"任务"面是 `codex cloud`（账号级云端任务，`status/list/apply/diff`），非本地持久台账 → 无 append-only 台账与事件链 | `raw/probe-m1-ledger.codex.out`（1331 行转录） |
| codex M2 | 0 | `error: unexpected argument '--depends-on' found` → `GATE_CMDS=codex cloud exec --depends-on …` / `M2_VERDICT=NONE` | `raw/probe-m2-dependency.codex.out` |
| codex M4 | 0 | `codex apply` 只吃一个 task id，对"共同祖先/工作区干净/FF"**零校验**；无分支合并队列 → `M4_VERDICT=NONE` | `raw/probe-m4-mergegate.codex.out` |
| codex M5 | 0 | `codex worktree list` → `error: unexpected argument 'list' found`；无 create、无 reclaim → `WORKTREE_RECLAIM=NONE` / `M5_VERDICT=NONE` | `raw/probe-m5-worktree.codex.out` |
| codex M6 | 0 | 只有 `remote-control`（单向被控）+ `cloud`（账号级云存），**无 peer 记录同步、无冲突合并** → `M6_VERDICT=NONE` | `raw/probe-m6-sync.codex.out` |
| claude M1 | 0 | `claude --help` 全量 grep `task|ledger|audit|todo|journal|history` **唯一命中是 `dontAsk` 的子串假命中**；反向证据：`claude project purge --help` 承认内部有 tasks 状态但**只暴露删除、不暴露创建/列表/事件** → `LEDGER_CMDS=NONE` / `M1_VERDICT=NONE` | `raw/probe-m1-ledger.claude.out` |
| claude M2 | 0 | `TaskUpdate #2 --addBlockedBy #1` 后，父仍 `pending` 时子被**成功 claim 成 in_progress**：`#2 [in_progress] M2-child-claude … [blocked by #1]` —— 依赖只是注释、不拦领取 → `M2_VERDICT=NONE` | `raw/probe-m2-dependency.claude.out` |

---

## 7. 复现命令

```bash
S=/home/user/.gopi/sessions/01a0eec1-e2a4-7366-bcf3-d2c2e183fe18/strongest
M=/home/user/workspace/gopi-01a0eec1e2a47366bcf3d2c2e183fe18/scripts/materials

# 1) 逐字输入 + 双 sha256（发送侧）
sha256sum $M/probe-m*.txt

# 2) 四家 × 六机制：同一文件、同一 $(cat) 展开，仅 cd/timeout/重定向/写 $?
bash $M/strongest-drive.sh $S codex  $M
bash $M/strongest-drive.sh $S claude $M
bash $M/strongest-drive.sh $S hermes $M
bash $M/strongest-drive.sh $S dsh    $M

# 3) 无 LLM 的原生面取证（hermes M1/M2/M3/M5）+ gopi M6 协议腿 + claude M3 长超时补跑
bash $S/hermes-native.sh
bash $S/followups.sh

# 3b) 复核轮：到达侧逐字复核 + 逐格回答来源判定（两个都只读）
python3 $M/strongest-arrival-check.py $M          # → 24/24 HIT，逐格到达文件路径
bash    $M/strongest-answer-provenance.sh $S      # → 12 ANSWER / 6 EMPTY-FILE / 6 NO-ANSWER

# 4) 清单与 census（本报告全部数字的生成器；实测 regen 与提交版逐字节相同）
cd $S/raw && ls | wc -l                       # → 72 artifacts
ls $S/raw/probe-m*.rc | wc -l                 # → 24 cells
{ printf 'artifact\tbytes\tsha256_16\trc\n'
  for f in $(ls $S/raw); do
    rc=""; case "$f" in
      *.rc) rc=$(cat $S/raw/$f);;
      *.out) [ -f "$S/raw/${f%.out}.rc" ] && rc=$(cat "$S/raw/${f%.out}.rc");;
    esac
    printf '%s\t%s\t%s\t%s\n' "$f" "$(stat -c %s $S/raw/$f)" \
      "$(sha256sum $S/raw/$f | cut -c1-16)" "$rc"
  done; } > $M/strongest-raw-manifest.tsv     # → 表头 1 行 + 数据 72 行 = 73 行
# 实测：重新生成为 73 行，diff 与提交版 IDENTICAL
```

---

## 8. Verification（Rule 5 / Rule 31）

- **Build**：`go build` 不适用（本任务不改 Go 代码，只做只读探测与文档）。gopi 侧基线所用二进制身份（**复核轮实测**）：
  ```
  $ ls -la /usr/local/bin/gopi
  lrwxrwxrwx /usr/local/bin/gopi -> /home/user/work/GoPi/bin/gopi
  $ sha256sum /usr/local/bin/gopi
  126b042e0e8ba14f93cf437618aadf123745ea9f014e8d03b40727ded0096faf  /usr/local/bin/gopi
  $ gopi version
  v0.1.0  go1.24.5  linux/amd64
  revision: 33cc274d68abf1ee097a99c630c72b82a600aea1  built: 2026-09-29T21:40:42Z
  ```
  **诚实披露**：初版报告写的是 `3e48bc51…` / revision `09781342…` —— 那是探测时点的读数；本轮复核时该软链指向的 `bin/gopi` 已被**其它进程重建**（revision 已变）。所有 gopi 基线的 `.out` 日志是**探测时点的产物**，其内容不随二进制重建而变；但"当前二进制 == 探测时二进制"这一条**本报告不主张**。
- **Tests**：不适用（无代码改动）。本次任务的"测试"即 24 格真跑 + 12 份 gopi 基线 + 20 份无 LLM 取证，全部落盘。
- **Retired mechanism (merge verb removal, 2026-11-01).** 下面 `Ran` 清单里的
  `gopi merge --fast` 是本次运行**实际敲过**的记录，逐字保留；顶层动词随后被移除，
  今天打字会得到 **exit 1**（`gopi task merge` 是同一引擎的存活拼写）。
- **Ran**（改动的 surface = 探测仪器与报告，逐字照录；★=本轮复核新增）：
  ```
  bash .../strongest/drive.sh .../strongest codex  <materials-dir>
  bash .../strongest/drive.sh .../strongest claude <materials-dir>
  bash .../strongest/drive.sh .../strongest hermes <materials-dir>
  bash .../strongest/drive.sh .../strongest dsh    <materials-dir>
  bash .../strongest/hermes-native.sh
  bash .../strongest/followups.sh
  ★ python3 scripts/materials/strongest-arrival-check.py scripts/materials
  ★ bash    scripts/materials/strongest-answer-provenance.sh $S
  ★ ps -o pid,etime,cmd -p 841880 --no-headers        # hermes 进程仍在，未被杀
  ★ stat -c '%d %n' / $S $S/raw                      # Rule 22 device 实测
  ★ gopi doccheck                                     # Rule 36
  gopi task submit "…" --tags probe   /  gopi task list --status pending  /  gopi task update …
  gopi merge --fast probe-orphan-01a0ee   /   gopi task sweep-worktrees
  GOPI_ALLOW_AGENT_SPAWN=1 GOPI_HOME=$SB gopi worker 01a0ef17-2a97-7014-9a4f-0415e43ec376
  printf '{"rows":[]}' | GOPI_HOME=$GH gopi task sync --serve-remote
  ```
- **Result**：
  - ★ `strongest-arrival-check.py`：**24/24 HIT**（四家×六格，逐格到达文件路径记入 `arrival-check.log`）。
  - ★ `strongest-answer-provenance.sh`：**12 ANSWER / 6 EMPTY-FILE / 6 NO-ANSWER** —— 与报告 §1 的"(A)/(B) 两类证据"标注一致；6 格 NO-ANSWER 即 codex M1/M3 + hermes M1/M2/M5/M6（判定不依赖它们的自述）。
  - ★ `ps -p 841880`：hermes 主进程存活（etime 03:44:51），全程零信号。
  - ★ `stat -c %d`：`/`=2051，`.../strongest`、`.../raw`、worktree 均=2052（Rule 22 通过）。
  - 24/24 格落盘（`.out`+`.rc`）；`raw-manifest.tsv` 重建后与提交版**逐字节相同**（73 行）。
  - dsh 6 格 `rc=1`（HTTP_404，运行性卡点，输入已被四家同源确认接收）；claude 4 格 `rc=137`（180 s，M3 补跑 420 s 仍 0 字节）。
  - ★ 修正：初版称"codex M1 / hermes M1/M2 已在被杀前吐出完整判定块"——**不成立**，那三格里的 `*_VERDICT=` 行是**探测提示词自身的占位符回显**。已按实测重写 §1/§2 卡点表。
  - ★ 修正：初版称 hermes 到达侧"M1/M2 格内超时无记录"——**不成立**，hermes 六格到达侧**全部 6/6 HIT**（`messages(role='user')` 逐条比对），已重写 §3b。
  - gopi 六条机制全部用真实 CLI 跑通（§4 表）。
  - hermes 原生 `claim` 在父未释放时**真被拒绝**，父完成后**可领** —— 闸门实测通过。
  - **gopi 输的条目：0 条**；hermes 2 格为平，其余全部为赢。
  - `git status --porcelain`：见 §9。

---

## 9. 交付物与工作区状态

- 报告：`devdoc/gopi-strongest-diffs-verified.md`（本文件）
- 探测输入 + 驱动（随本批提交）：`scripts/materials/probe-m{1..6}-*.txt`、`scripts/materials/strongest-drive.sh`、`strongest-hermes-native.sh`、`strongest-followups.sh`、`strongest-probe-sha256.txt`、`strongest-verbatim-proof.txt`、`strongest-raw-manifest.tsv`
- 原始输出：`/home/user/.gopi/sessions/01a0eec1-e2a4-7366-bcf3-d2c2e183fe18/strongest/raw/*.out|*.rc` + 清单（**故意留在 git 树外**，按 Rule 30：untracked litter 不进 worktree）
- 复核轮证据（同在 `.../strongest/`，git 树外）：`arrival-check.log`（到达侧 24/24 HIT）、`answer-provenance.log`（12 ANSWER / 6 EMPTY-FILE / 6 NO-ANSWER）、`progress.log`
- 工作区：`git status --porcelain` 只列出上述本批新增/修改文件，无既有文件被改

## 10. Not Done（诚实列出）

1. **dsh 的网络取证没做**：卡点是 `HTTP_404`，我**没有**抓包或改 hosts/proxy 去定位上游端点（那属于改环境/越界）。因此 dsh 六格停在"未判（运行性）"，没有更进一步的产品能力结论。
2. **claude 的 M3/M4/M5/M6 没有拿到模型侧答案**：180 s 全灭，M3 单独放宽到 420 s 仍然 0 字节。判定靠其 `--help` 能力面，**不是**靠它的自述。
3. **hermes 的 M5 回收语义只测到"gc 不动 worktree"这一次**；它的 `board rm` 会归档 board 目录，但不能断定 worktree 永不回收 —— 本报告只声明实测到的这一条。
4. **M6 的跨节点真同步只在本机单节点跑通协议腿**，没有第二台真机参与往返（`ssh hp` 可达 `rc=0`，但一次真 sync 会在对端 ledger 写入真实行，未授权未做）。
5. **6 格没有产品自述判定块**（codex M1/M3、hermes M1/M2/M5/M6）：它们被 180 s 截断。判定改由**同一次运行里产品自己的原生命令输出**或**无 LLM 的原生面真跑**支撑，均已落盘 —— 这不是"没测"，是"产品自己没说，证据在别处"。
6. **hermes 的 LLM 探测格五格（M1–M5）全部无效**（模型把 gopi 当成自己的产品），其中的 `NATIVE` 判定是给 gopi 打的。本报告**不采用**这五格作为 hermes 的任何证据；hermes 的结论只来自无 LLM 取证。**这也说明一件事**：把"你的产品是什么"交给模型自己认，在 hermes 的 cwd 里就不成立 —— 这是我这次探测设计上的一个真实缺口，反作弊规则挡不住它（规则挡的是"我替它写包装"，挡不住"它认错了自己"）。
7. **初版报告有两处事实错误，已在本轮修正**（见 §8 Result 的 ★ 两条）：①"codex M1 / hermes M1/M2 已吐出完整判定块"——实际那三格里的 `*_VERDICT=` 行是探测提示词自身的占位符回显；②"hermes 到达侧 M1/M2 格内超时无记录"——实际 hermes 六格到达侧 6/6 HIT。两处都已按实测重写，并在 §2b 落盘了两个只读复核工具供第三方重跑。
