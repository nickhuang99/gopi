<!-- doc-class: normative -->
# devdoc: notify.sh 拉对端事件（peer pull）— 为什么不进 gopi、游标放哪、ssh 怎么防拖垮

状态：实现完成，脚本 + 本文档同一 commit。
范围：**只改 `scripts/notify/notify.sh`，不改任何 gopi Go 代码**（operator 决议：非普遍性需求不进 gopi）。

---

## 1. 需求

host-b 的 `notify.cmd`（= `bash /home/user/.gopi/notify/notify.sh`）被 gopi 在每次任务生命周期事件时 exec 一次，
payload 从 stdin 进来。它原本只做两件事：投给本机所有 chat slot，然后把 raw payload 追加到
`<profile>/notify/task-events.jsonl`（anchor）。

要多做一件：**顺手把对端（默认 peer1）的新事件也拉到本机 chat 里**，用 ssh 读对端同一文件，
按「上次取到的时间」增量筛，投递后更新游标。

## 2. 为什么放在脚本里，而不是 gopi

gopi 里已经有一整套跨节点机制：`internal/sync`（claim 广播、终态推送、serve-tick delta）、
`sync_peers` 白名单、`GOPI_SYNC_PEERS` 覆盖、`newSSHCommand`（BatchMode + ConnectTimeout）。
把通知拉取做进 gopi 意味着：新增一条常驻/serve-tick 通道、新增一个配置键、新增一处故障面，
而**收益只在一台机器上**（operator 的 chat 想同时看到对端事件）。

gopi 的设计已经把这件事划清了边界：`internal/tools/tasknotify.go` 明确「gopi 不硬编码任何通知机制，
它读配置里的 shell 命令并 exec，事件交给命令」。**通知的投递形态是配置面的责任，不是 gopi 的内核责任** ——
所以这一层逻辑天然属于 `notify.cmd` 指向的那个脚本。放进 gopi 反而是把「某台机器的通知偏好」
升级成所有部署都要带的普遍性机制（Rule 35 的第二问：「能不能由已经跑着的代码解决」—— 能，就是这段脚本）。

**代价要说清**：脚本形态意味着这段逻辑没有 Go 单元测试网。它的回归门是
`cmd/agent/chat_session_slots_script_test.go`（钉住 `SLOT_LIMIT`/`SLOT_PREFIX`/`SLOT_SUFFIX` 与脚本可执行位）
加上本 run 的真跑证据（本文档 §6）。这是有意识的取舍，不是疏忽。

## 3. 结构：三段，一段循环

```
stdin payload ──► field()/build_message() ──┐
                                            ├──► deliver_all()  ← 唯一的投递循环
对端新事件 ─────► build_message(前缀) ───────┘
                                            └──► anchor 追加（只追加本机 payload）
```

- `field(payload, key)` —— 从**任意** payload 抽字段（原来是从闭包里的全局 `payload` 抽，现在带参），
  本机 payload 与对端每一行共用同一个抽取器。
- `build_message(payload, prefix)` —— 拼一行 `task event: …`。本机调用传空前缀，
  **本机消息文本与改前逐字一致**（§6 G1 实测）；对端传 `[peer <alias>] `。
- `deliver_all(message)` —— 逐 slot 投递那 10 格。**本机事件和对端事件都走这一个循环**，
  被调用两次而不是复制两份：复制一份投递规则就有两处会各自漂移。
- 本机事件**先投**，对端事件**后投**（顺序是安全属性：ssh 再慢也不占在本机消息前面）。

anchor（第 3 段）**只追加本机 payload**：对端事件在它自己的 anchor 里已经存在，
本机这个文件是「本机做过什么」的锚点，塞进对端行会让 `gopi notify events` 的读者看到不一样的东西。
对端事件的去重**只靠游标**，不靠 anchor 内容比对。

## 4. 游标：位置、字段、比较

- **位置**：`<profile>/notify/peer-<peer>-last-seen.txt`，每 peer 一个单行文件。
  放 `<profile>/notify/` 的理由：它就在 anchor 旁边，同属「通知面」的状态；已经在 `<profile>` 下的
  `notify` 目录是脚本自己建的（`mkdir -p "$SINK_DIR"`），不新增目录层级。
- **字段**：ISO 字符串 `timestamp`（如 `2026-10-12T08:21:29+08:00`），**字符串比较**，不做日期解析。
  依据：两端 timestamp 都带 `+08:00`，同格式下字典序 = 时间序；实测 300 行窗口内只有 1 处逆序，
  且那行是含 `null` 的历史行。相比之下 `created_at`/`updated_at` 在 300 行里有 30/31 处逆序 ——
  它们是**状态被改写的时间**，不是事件发生的时间，所以不用。
- **推进规则**：游标记「这次在对端**见到**的最新 timestamp」，包括本批已投递的。这样一批全被去重也不会
  反复重读。本批没见到任何新行 → 游标原样不动。
  **（2026-10-16 修正：推进是「每批一次」，不是「每次运行一次」——见 §10。原文写的是「这次」，
  而当时的实现把那次写放在了 O(n) 投递循环**之后**，正是停摆缺陷本身。）**
- **首跑没有游标**：**不投任何东西**，只把游标建立成对端最新事件的 timestamp。
  理由是实测的：第一次跑（游标不存在）时若把对端 anchor 全量当新事件，本机 chat 一次收到 **447 条**
  历史通知（§6 事故记录）。「没有游标」诚实的含义是「从此刻开始」，不是「自从开天辟地」。
- **写失败出声**（Rule 15）：写不进去就打一行 stderr，并说明后果（下轮会重复读到同一批）。

## 5. ssh 与超时

- `BatchMode=yes` —— 绝不提示密码；没有人会回答提示，提示 = 挂住投递。
- `ConnectTimeout=10` —— **10 的依据**：本调用在健康链路上的实测成本是 **0.36–0.82s**
  （握手 + 远端全量扫描 53MB/4030 行），10s 是「明显高于实测成本、又不至于拖垮投递」的取值，
  约 12 倍余量；同时与本仓库既有的 `internal/sync: syncSSHConnectTimeout = 10` 一致。
- `-T` —— 不分配 pty。
- **失败必须出声**：非零退出打一行 stderr，带 peer 名、ssh 退出码、ssh 自己的 stderr 原文；
  然后 `return 0`，本机投递照常。坏别名实测 **0.07s** 就返回（`Could not resolve hostname …`），
  本机 10 格投递一次不落（§6 G4）。
- **每轮最多读 `PEER_LIMIT=2` 个 peer**：exec 本脚本的子进程有 30s 硬超时
  （`internal/tools/tasknotify.go` 的 `taskNotifyCmdTimeout`，且杀整个进程组），
  而一个黑洞地址的 peer 会吃掉整个 `ConnectTimeout`。2 × 10s 给本机投递留 10s，而本机投递实测 0.13s。
  超限的 peer **点名打 stderr 并留给下一轮**，不是静默截断。
- **对端程序走 stdin，不走 argv**：`ssh` 会把它收到的命令参数拼成**一个字符串**再交给远端 login shell 解析，
  于是作为参数传进去的带引号程序会丢引号、自身引号不再配对（实测：远端把文件读成不可读并 `exit 3`）。
  现在用 `sh -s`，程序走 stdin，**游标作为唯一的 argv 操作数**。
- **游标上远端之前先白名单**：字符集限定 `[0-9A-Za-z:+.-]`（ISO 时间戳只需要这些）。
  越界就**跳过该 peer 并出声**，绝不当作空游标（那会重投整个历史）。这条挡住了「手改过的游标文件」
  和「对端 timestamp 字段里夹了引号」两种注入到远端解析器的路径（§6 G5：`evil"); touch /tmp/…` 未被执行）。

## 6. Rule 31 真跑证据

完整原始输出：`~/.gopi/sessions/01a12d32-7f51-79c2-9e6b-76990d9f5f67/artifacts/GATES-FINAL.txt`
（跑它的脚本同目录 `artifacts/gates.sh`）。要点：

| 门 | 结论 |
|---|---|
| G1 本机消息文本 | 改前/改后同一 payload、同一沙箱，投进 chat inbox 的记录除 `id`/`created_at`（每次投递各自生成）外 **IDENTICAL** |
| G2 真拉对端 | `notify.sh: peer peer1: 2 new event(s), 2 delivered`，chat 侧收到 `[peer peer1] task event: event=completed …`，游标推进到 `2026-10-12T08:21:29+08:00`，整轮 0.92s |
| G3 连跑第二次 | `peer peer1: 0 new event(s), 0 delivered`，chat 只收到本机那条（对端 0 条） |
| G4 坏 peer | stderr：`peer zz-no-such-peer-zz: cannot read its event stream (ssh exit 255): ssh: Could not resolve hostname …`；本机投递仍 delivered=1；整轮 **0.07s** |
| G5 配置/游标边界 | `sync_peers` 非数组 / settings 非 JSON / 空数组 / 无该键 / 无 settings 文件 —— 各自点名出声并跳过；坏游标被拒且 `/tmp/pwned-notify` 未创建 |
| G7 首跑无游标 | 只建立游标、**0 delivered**；下一轮只投此后的新事件 |
| G8 gopi 真实 exec 形态 | 以 `sh -c "bash <script>"`（`internal/tools/tasknotify.go` 的启动方式）+ 继承 `GOPI_HOME`/`GOPI_SETTINGS` 跑：`peer peer1: 2 new event(s), 2 delivered`，chat 侧本机一条 + 两条 `[peer peer1]` |

沙箱必须**同时**钉 `GOPI_HOME` 和 `GOPI_SETTINGS`：`GOPI_SETTINGS` 在本会话环境里是导出的，
不钉就会读 operator 的真 settings —— 实测发生过，见下。

**本 run 的事故（诚实记录）**：G6 段（在真实 profile 上直跑、`GOPI_SETTINGS` 被 `env -u` 拿掉）
暴露了上面的首跑缺陷，当时游标不存在，脚本把对端 anchor 全量投给了 operator 的活跃 chat
（会话 `5af45c3e-…`，447 条 `[peer peer1]` 消息进入其 `messages/inbox.jsonl`）。
修法是 §4 的「首跑只建立游标」。**这 447 条已投出的消息没有撤回** —— 撤回要改 operator 活跃 chat 的 inbox，
那不是我该动的文件；这里如实记下，由 operator 决定是否清理。

## 7. Rule 35 四问

1. **没有它什么会坏**：operator 的 chat 只看得到本机事件；对端（peer1）的任务完成/失败没有任何推送，
   要靠人工 `ssh` 或切窗口看。这是**能力缺失**，不是「会变脆」。它不修任何既有故障 —— 所以下面三问更要紧。
2. **为什么不能在进程内解决**：gopi 进程内**已经有**这条通道吗？没有；它有的是 task-ledger 同步，
   与「通知进 chat」是两回事。能放进 gopi 的 Go 代码吗？能，但那就把一台机器的通知偏好变成所有部署的普遍机制
   （§2）。已有的、每轮都在跑的代码里加？`serve-tick` 是**服务端常驻**面，而本机是 client、
   notify 是被 exec 的一次性进程 —— 两者不是同一条生命周期。
3. **谁负责它的生命周期，怎么回收**：生命周期 = **gopi 的 notify exec 本身**。
   没有新进程、没有定时器、没有常驻：不 notify 就不拉，脚本干完就 exit。
   状态只有两类，都在 `<profile>/notify/` 下：10 个既有 slot 文件 + 每 peer 一个单行游标文件。
   回收：游标文件是幂等的单行覆盖写，`PEER_LIMIT` 之外的 peer 下一轮自动接管；ssh 输出落在
   `$$` 命名的临时文件，`trap … EXIT` 清理，无残留。**本 run 实测无 crontab、无 systemd user timer**
   （`no crontab for nick`、`systemctl --user list-timers` 0 个）—— 没有第二处调度。
4. **下一个人发现它时看到什么**：脚本头部三段式注释写清了它做什么、为什么这么分段、
   哪些值是别的包定义的（`ChatSessionSlotLimit`、`SyncPeersSettingsKey`、`TaskNotifyCmdTimeout` 的 30s）；
   每个失败分支都在 stderr 上点名（谁的错、哪个文件、什么后果）。游标文件在 `notify/` 下、
   名字自带 peer 与用途，`cat` 一下就知道进度。

## 8. 临时安排的边界（明确说清，避免它悄悄变成契约）

- **peer 名单** 来自 `settings.json` 的 `sync_peers`（`internal/config` 的
  `SyncPeersSettingsKey`），没有第二份硬编码名单。脚本自己解析该文件（`python3 -c` json 解析，
  无 python3 就点名跳过，不用脆弱的正则猜），**不经过 gopi 的 `ResolveSyncPeers`** ——
  因此 `GOPI_SYNC_PEERS` / CLI 覆盖这两级**不生效**。这是有意的：脚本只读声明面的**一半**，
  而它读的那一半正是持久化的那份（env 覆盖是单次进程的，不该改一个「长期拉谁」的决定）。
- **远端路径**写死 `~/.gopi/notify/task-events.jsonl`（对端默认 profile）。
  对端用 `GOPI_HOME` 自定义 profile 时，本脚本读不到 —— 已知边界，未实现远端 profile 发现。
- **对端事件的 chat 文本**是 `[peer <alias>] ` + 与本机同格式的一行；
  不做「对端事件写进本机 anchor」。
- 这四点是**当前取舍**，若将来要变（比如接 `ResolveSyncPeers` 的两级覆盖），
  应当作为新任务重做，而不是在这里慢慢长。

## 9. 验证命令

```bash
bash -n scripts/notify/notify.sh                       # 语法
shellcheck -S warning scripts/notify/notify.sh         # 无告警（信息级豁免见下）
go test ./cmd/agent/ -run 'Slot|NotifyScript'           # 既有的脚本契约门
```

`shellcheck -S style` 有 1 条**预先存在**于原脚本的 info 级 `SC2016`
（单引号里的 `` `gopi` `` 不展开 —— 故意的），新脚本未新增同类；`ssh` 调用点带
`# shellcheck disable=SC2029`（游标**必须**在客户端展开，这是设计）。

---

## 10. 停摆缺陷与修复（2026-10-16，任务 01a14194）

### 缺陷（实测，非推断）

cursor 停在 `2026-10-14T06:11:22`两天不动，而 peer 锚点在涨；`notify/` 下 33 个
`.peer-pull.*.raw` 残留（EXIT trap 不执行 = 进程被 SIGKILL）；同一批事件跨会话重复到达 chat。

根因是**结构**，不是偶发故障：投递循环**每行**调一次 `deliver_all`，而 `deliver_all`
**每个 slot 起一个 `gopi notify` 进程** —— 402 行 × 10 个活跃 slot = **1517 次进程启动**（实测），
远超 `taskNotifyCmdTimeout`（30s）能跑完的量；而**写 cursor 的代码在循环之外、之后**。
SIGKILL 不给 trap 机会 ⇒ 循环中断 ⇒ cursor 永不推进 ⇒ 每轮从同一旧 cursor 重拉同一批。

按实测算：单次真实投递 ~9ms，402 行 × 10 slot ≈ 36s > 30s —— **通知流的规模本身保证了它必然发生。**

### 修复（三件事，`scripts/notify/notify.sh`）

1. **批量投递，`gopi notify --deliver-multi`**（新增，`internal/tools.NotifyDeliverMultiFlag`）：
   stdin 收一个 JSON 数组（消息串），**一次进程**把每条投给**每个**已注册 slot，
   走的是与单条面**同一个** `tools.SessionInboxDeliver`。投递成本从 O(行×slot) 降为
   **O(批×1)** —— 实测同一 402 行 backlog：1517 次启动 → **14 次**。
   `gopi notify --deliver-multi -h` 探测该 reader 是否存在，没有就退回逐行（旧行为，只是慢），并出声。
2. **cursor 在循环内、每批之后推进** —— 进度记录永远不超过「已投递」。中断的代价因此
   被限制在**一个批**（`PEER_DELIVER_BATCH_LINES=40`），而不是整条流。
3. **本次运行自己守住 deadline**（`NOTIFY_RUN_BUDGET_SEC` = 父进程的 30s 减去 5s 余量），
   ssh 腿用 `timeout` 夹住（`ConnectTimeout` 只管连接阶段，管不住「连上后远端卡住」），
   并且在批边界**优雅停止**：父进程的 SIGKILL 是杀，本脚本的停止是**已经写好 cursor 的停止**。
   另外按**年龄**（`NOTIFY_SPOOL_STALE_SEC=300`，绝不按「看起来像废弃」）回收被 SIGKILL 留下的
   spool，回收数量出声。

### 为什么 `--deliver-multi` 收「数组」而不是「换行分隔的多行消息」

因为它不是「一条多行消息」。批量路由必须让**每条消息仍是独立的一条**（各自一个 inbox 记录、
各自一个 id），否则一条 402 行的消息会变成单个 turn —— 语义完全不同。而单条消息里**可以含换行**
（实测：`--message "$(printf 'A\nB\nC')"` 作为一条记录的原样保存），所以「换行分隔」在协议上
不可逆；以**一批一条子进程**的方式让每条消息都成为自己的调用，那一批就只有一个进程，
每行一次进程启动的 O(n) 从根上消失。

### 实测证据（Rule 31，全部真跑）

- session 目录 `artifacts/E2E-FINAL.txt`：真 `gopi` 二进制 + 真 10 个 slot + **真的那份 402 行
  backlog spool**（`/home/user/.gopi/notify/.peer-pull.1051035.raw`）+ 显式 `PATH` 上的 stub 只
  替换 **ssh 传输**（远端 awk 程序本身是真跑的）、`timeout -s KILL` 扮演父进程的杀。
- 回归测试 `scripts/notify/notify_peer_resume_test.sh`（4 例，含**拿修复前形状的脚本跑同一次
  中断**的 RED 臂）。修复前的那份 sink **有三个来源**，脚本每次运行都自报走了哪一条
  （`RED arm source — override|history|synthesis`）：`NOTIFY_PRE_FIX_REV` 显式覆盖 >
  `notify.sh` 自身历史里**不含新特征生产点**的最新修订 > 在待测 sink 上做三处**锚定替换**
  按构造重建（批量读取既不被调用也不被探测、cursor 写回被移出批次循环、循环欠下的那次
  写回被放到循环之后）。重建**被检查而非被信任**（`bash -n` / 与待测 sink 不同 / 无探测行 /
  **无循环内 cursor 写**——最后一条正是 case 4 测量的性质，缺了它这条 RED 臂就是在测修复
  本身），任一不过 = FATAL。锚点消失才是具名跳过：两条 RED 断言各打一行
  `SKIP (named, Rule 27)`，汇总写 SKIPPED 计数而不是 passed。
  **为什么必须有后两条来源**：squashed source snapshot 的 clone 里 `notify.sh` 只剩一枚
  修订且已带新特征，只走 git 的那版在这类 ground 上打印一行 FATAL 并 exit 2 —— 一次都没
  跑过，而「没跑」与「绿」无法区分（Rule 36）。
  `cmd/agent/notify_deliver_multi_test.go`（批量面的行为 + 跨语言 flag pin）。
- 两个数字并排：**修复前 1517 次启动 / cursor 不动 / 下一轮再投 1517 条** vs **修复后 14 次启动 /
  cursor 推进到已投批次的最后一行 / 下一轮只投剩余**。
