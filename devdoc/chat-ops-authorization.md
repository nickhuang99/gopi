<!-- doc-class: normative -->
# chat 操作授权判据：从「按动词名禁」→「按时间」→「按动作类别」

本文件记录**两代**判据的更替：第一代把「按动词名禁」换成「按时间 + 用户授权」（task
`01a115d1-67f8-74a4-a1ed-e1fe21b4dc8c`，operator 2026-10-07 裁定，§1–§7 保留原文），
第二代把「按时间」换成**按动作类别**（task `01a14731-e777-72c8-897b-b87857e74d1a8`，
operator 2026-10-11 裁定，见 §8）。**当前判据是 §8**；§1–§7 是它的来路，保留是因为
「为什么删掉档位会变严」这件事只有走过一遍的人才记得（§8.2）。

第一代代码：`internal/tools/chat_ops_authorization.go`、`internal/agent/chat_write_guard.go`、
`internal/agent/chat_write_boundary.go`、`internal/tools/chat_write_guard{,_readonly}.go`、
`internal/config/{config,task_sync_switch,settings}.go`、`CONFIG.md`。

---

## 1. 旧判据 vs 新判据

**旧判据**：`internal/tools/chat_write_guard.go` 问「这条命令行里跑的是哪个 git 子命令」，
再拿一张按**名字**列出的禁用表 (`ChatBannedGitSubcommands`) 判。之后加了只读表
(`chat_write_guard_readonly.go`)，把「只读的调用」从名字表里摘出来。

**它答对了什么、答错了什么。**

- 答对了：**写操作**确实必须挡住。chat 没有 branch、没有 worktree，一个 commit 从它手里出去
  就没有 owner、没有 rollback 点、并且改写 operator 正站在里面的那棵树。
- 答错了（这是 operator 指出的）：**「这个动作叫什么」和「它要花多久」是两回事。**
  用名字判，意味着
  - `go build ./...`、`make test`、合并（当时的 `gopi merge`；该动词已于 2026-11-01
    删除，现在是 `gopi task merge`）—— 不叫 git，于是**从来没被问过**，一个本来
    该跟人打招呼的几分钟操作，和一个 `ls` 走同一条路；
  - 反过来，只读的 git 调用被当成动词名字判 —— 见 §2。**判据问错了问题**。

**新判据（operator 原话的落地）**：判**时间**，再判**谁授权的**。

| 情形 | 旧 | 新 |
|---|---|---|
| `git status` | 放行 | 放行（只读，任何速度） |
| `git worktree list` | 拒（动词名 `worktree` 在禁用表） | 放行 |
| `go build ./...`（没声明时间） | 放行（不是 git） | **请示**（长类，未声明预算） |
| `make test` + operator 说「最多 5 分钟」 | 放行 | 放行（operator 授权覆盖） |
| `make test` + `timeout_sec: 60` | 放行 | 放行（≤ 阈值，工具真会 kill） |
| `make test` + `timeout_sec: 90`（operator 没说） | 放行 | **请示**（> 阈值且无授权） |
| 改源码 / 调代码 | 拒（若走了写路径） | **仍拒**，路由 `gopi task submit` |

### 判据落在哪些常量上

```go
const ChatOpQuickBudgetSec   = 30 // 理想带：行内直接放行
const ChatOpAskThresholdSec  = 60 // 请示阈值：>60s 必须有 operator 本回合的授权
```

`ChatOpAdmit(hasBudget, timeoutSec, command, operatorTurn) ChatOpAdmission` 是唯一判定入口，
顺序如下（每一步都对应 operator 裁定里的一句话）：

1. **没声明预算**，或声明成 `timeout_sec: 0`（bash 工具的「无限」拼写）：
   - operator 本回合原话里有预算 → 放行（人授权了，就不该跟人犟）；
   - 否则看**常驻姿态**（§3）：全问模式下，命令**命中长类**（build/test/install/merge…）→ 请示；
     命令不命中长类 → 放行（这是「普通控制台工作」，理想带内假定）。
2. **声明了 ≤ 60s** → 放行。这是裁定的两个直接带（理想 30s + 第一分钟内其余部分）。
3. **声明了 > 60s** → 只有当 operator 原话的预算覆盖它时放行。

## 2. 只读残留的根因

operator 的观察是：「只读表已经有了，但 `git worktree list` 一类纯查询还是被拦，payload/heredoc
里含只读 git 文本时整条被拒」。实测确认了**三个互不相同的根因**，都在
`internal/tools/chat_write_guard*.go`：

**(a) wrapper 的载荷根本没被读。**
`whitelist` 里 `bash`/`sh`/`sudo`/`ssh`/`xargs`/`find` 是「wrapper clause」——reader 不展开它，
直接生成一条 assertion，拒绝语就是实测会话里那句
`its payload is a string this boundary does not expand`。
于是 `sh -c 'git worktree list'` 被整条拒，而**同样几个字直接写**就放行。
→ 修复：shell wrapper（`bash`/`sh`/`zsh`/`dash`）的 `-c` 载荷被**重新分词后递归**进同一个 reader
(`chatWrapperPayloadWords` + `chatGitInvocationsForWords`)。载荷里的 mutation 照样被抓；
载荷是变量（`bash -c "$CMD"`）时明确回到不可读路径，**不当作「里面什么也没有」**。

**(b) reserved verb 走到的是动词组规则，不是只读表。**
`worktree`/`branch`/`tag`/`stash` 在只读表里有细则，但**裸 git 词的路径**（`git worktree --help`）
先撞上「这个动词是保留动词 → 它是写操作」，连表都不问；拒绝语还不点名，读起来像整个动词组被禁。
→ 修复：该路径现在调同一个 `chatGitReservedVerbAssertion`（shell 写法 `stash --bogus` 走的就是它），
  两种写法一个答案，且拒绝时点名**输掉的那个词**（`remove` / `-D` / `--bogus`）。

**(c) shell 重定向被当成参数判。**
`git worktree list 2>/dev/null`、`git log -3 2>&1 | head -3`、`git commit -m x 2>&1` 里的 `2>`/`2`
被当作「动词的参数」甚至「pathspec」，于是整条被拒（实测拒绝语把 `2>` 报成"会碰到的路径"）。
真实 shell 在 git 被 exec 之前就吃掉了重定向，git 永远看不到它。
→ 修复：`chatShellRedirectionWord` + `chatGitStripRedirections`，在**每一个**判定 git 参数的谓词前剥离
重定向与其目标词；拒绝语也不再点名 `2>`。

**这三条的共同形状**：判定用的是「命令行里出现的字」，而不是「exec 之后进程收到的东西」。
修复的方向因此是同一个——**让 reader 读得更多、判得更接近实际行为**，而不是加白名单。

### 回归测试（每条都钉住上面一个根因）

`internal/tools/chat_write_guard_readonly_test.go`：

- `TestChatBashWriteRefusalAllowsReadOnlySubcommands` 增补 wrapper 与重定向的**放行**用例
  （`bash -c 'git worktree list'`、`sh -c "git status"`、`git worktree list 2>/dev/null`、
  `git log --oneline -3 > /tmp/log.txt` …）；
- `TestChatBashWriteRefusalStillRefusesTheMutationsBesideTheReads` 增补**同一条路上的拒绝**用例
  （`bash -c 'git commit -a -m x'`、`sh -c "git add -A"`、`git commit -m x 2>&1` …）；
- 新增 `TestChatBashWriteRefusalJudgesAReservedVerbByTheReadOnlyTable`：同一调用两种写法
  （`git stash --bogus` 与 `stash --bogus`）必须**为同一个理由**输，且点名同一个词。

**未修复（诚实）**：`ssh <peer> git log -3`、`xargs git push` 这类 wrapper 的载荷是**别的进程的参数**，
不是 shell 字符串，依旧走不可读路径 → 拒绝。这是刻意的：读它需要猜别的进程的参数文法，
而猜错的代价比多问一次高。`find . | xargs grep` 不受影响（不含 git 词）。

## 3. 时间判据：能做什么、不能做什么

### 做不到的（必须明说）

**「这条命令要跑多久」在它跑之前是不可知的。** gopi 里没有任何组件给命令计时，没有历史表；
想「知道」就只能新增长期存储（历史库/缓存）——那是一条新的持久依赖，Rule 35 明确要求先论证四条
（什么坏了、为什么不能在进程内修、谁负责回收、下一个人会发现什么）才允许存在。为回答一个
**声明就能回答得更好**的问题去造一个数据库，是不划算的。

所以本判据**不预测**，只读两种**声明**（declaration）：

1. **call 自己的 `timeout_sec`**。这不是预测，也**不是自我声明**：bash 工具会按这个数字 **kill 整个
   进程组**（`internal/tools/bash.go`），所以「这条 ≤ 1 分钟」是构造上成立的，不是承诺。
   `timeout_sec: 0` 是「无界」的拼写，也是唯一不被当作预算的声明。
   注意：bash 工具**默认 120s** 的帽子**不算声明** —— 那是工具的上限，不是命令的时长估计；
   把它当估计会让判据变成空话（每条命令都"≤120s，直到被杀"，而一个被杀的 build 留下一条
   operator 从没批准过的超时调用）。
2. **operator 本回合原话里的预算**（`最多 5 分钟` / `give it 10 minutes`）。只从消息里读、不落库，
   所以闸门不可能"忘了关"（与 `chat_cleanup_authorization.go` 同一形状、同一理由）。取**最小值**：
   一句既给又限的话（"跑 1 分钟，但别超 30 秒"）授权的是那个限制。

### 剩下的不确定：用**类**兜底，且只在什么都没声明时

`long` 行 是 operator 裁定里点名的那几类（build / test / 包安装 / merge / worker），
词表来源是**控制台已经在用的路由语**（bash 工具的描述说 `make test` 走项目声明的入口，job 工具说长命令
是 job），不是新造的一张表。它**只在没有任何声明时**参与判定。

这个兜底会**漏判**（一条很慢但没命中任何词的命令会被当作普通工作直接跑）。方向是安全的：
最多是"本该问一次却直接跑了"，而不是"擅自跑了很久"；而且 operator 的话一出现，判据立刻改用授权，
不再看词表。**假阳性**方向也测了：`ls`、`git status`、`git worktree list` 都不命中。

### 朴素读取的边界（也明说）

从 operator 原话里读预算是一个粗糙的扫描（数字 + 单位词，英/中），词表外的说法会漏读 → 落到"请示"。
refusal 里**印出能开闸的确切拼写**，所以误判的代价是 operator 一句话。

## 4. 「改代码」禁区的可操作判据

唯一硬禁区，**与时间无关**：**改源码 / 调代码（debug fix）一律是 task，不是 edit**，
无论它十秒还是十分钟。它不走 ops-budget 这条通道（那条通道管的是"能不能现在跑"），
而是走写边界本身：写进任何**别的 git-tracked 路径**被拒，并路由到 `gopi task submit`。

为什么 debug fix 要单独点名：**最诱人的绕过正是它**——"就先改一行让它不报错"。那一行的产权
和一次重构的产权完全一样（没有 branch、没有 owner、没有 rollback 点），而它正因为**快**而不触发
任何时间判据。所以判据必须按**动作的性质**判它，而不是按耗时。

可操作的判据（写边界第 (3)(5) 条，prompt 与守卫同源）：
- 写进 `devdoc/` 以外任何 git-tracked 路径 → 拒；
- 唯一允许：`devdoc/` 下的文档，`git add <那一个文件>` + `git commit -m "..."`；
- 其它要留下的产物 → 本 run 自己的 session 目录（Rule 37）；
- 要改代码 → `gopi task submit`。

## 5. 授权通道：一个开关，三个标签

```
GOPI_CHAT_OP_AUTO (env)  >  settings.json "chat_op_auto"  >  ASK (默认)
```

- **默认 ASK**，且这个默认是刻意的：一个没人声明过成本的操做被控制台自己开跑，正是 operator
  裁定点名的缺陷；"我没法给它定界"不能变成"所以我跑了"。
- 通道形状照抄 `config.TaskSyncEnabled`（Rule 2/13/14：一个 knob 一个读者）：
  - 拼错的值**不能**被悄悄读成默认 —— 报 stderr，交给 settings 通道（Rule 15）；
  - settings 文件读不了 → 落到 ASK，并**报出来**；
  - `tools.ChatOpChannel()` 是**唯一**解析点，settings key/env 名在 `internal/config/task_sync_switch.go`
    定义一次、tools 侧别名引用（不重新拼写）；
  - catalog 行（`internal/config/settings.go`）+ `CONFIG.md` 行同步（`config_doc_gate_test` 会红）。

**ON 到底买到了什么**：让控制台能自己开一个**没有声明边界**的操做（包括长类命令）；
它**不**买 `timeout_sec: 0`（operator 授权的是"没给边界"，不是"永不结束"），
也**不**买一个声明了 >60s 却没有 operator 原话的调用。

## 6. prompt 与守卫同源（Rule 13/14）

`internal/agent/chat_write_boundary.go` 的第 (6) 条 ops-budget 规则**渲染自守卫自己的常量**：

```go
quick := strconv.Itoa(tools.ChatOpQuickBudgetSec)
ask   := strconv.Itoa(tools.ChatOpAskThresholdSec)
... tools.ChatBashTimeoutArg ...
```

于是"prompt 教一个 band、guard 执行另一个"这件事**不可表达**。
`internal/agent/chat_ops_budget_prompt_test.go` 从两端钉：

1. prompt 里的两个数字 == 守卫常量；且 prompt 不得把 bash 工具的 **120s 默认帽子**当预算教；
2. prompt 里的参数名 == bash 工具 schema 自己声明的 `timeout_sec`；
3. 同一批真实命令行上，prompt 的声明（长类要预算）与守的裁决**必须一致**
   （`make test`/`go build`/`gopi task merge`/`npm install` 全拒；`ls`/`git status`/`git worktree list` 全放）。

第 (1)(2)(3) 条拒绝语与 prompt 都从同一批常量/函数渲染（`ChatReadOnlyGitInvocationsRendered`、
`chatOpTaskSubmitRoute`、`ChatBashTimeoutArg`），所以任何一侧改名都会让另一侧的测试红。

## 7. 还剩什么（诚实）

- **没有真跑一次交互式 `gopi chat` 会话**记改前/改后裁决（需要 TTY + 一次模型往返，本 run 不可用）。
  等价的取证是：用**真实入口** `ChatBashWriteRefusalIn` / `ChatOpAdmit` /
  `ChatOpAdmissionRefusal(Unbounded)` 在改前/改后各驱动一遍（见 task report 的 `## Evidence`）。
  Rule 11 要求的"LLM 真的被 prompt 说服"验收面还没做：需要在真实会话里看模型是否在
  >60s 或未声明时**先问**，这属于另一次带会话的 run。
- **长类词表会漏判**（§3），方向安全。
- **`ssh <peer> git ...` / `xargs git ...` 的载荷仍旧不可读**（§2），刻意保守。
- **一个既存缺口，本 task 未修，已在测试里断言为已知事实而不是被迁就**：reader 的入口有一道
  快速短路 —— 一行里没有 `git` 这个**字面词**就整个跳过（`chatGitWordPattern`）。于是
  **shell 写法**的 reserved verb（`stash --bogus`、`worktree add p`，全程无 `git` 词）在**基线**和
  现在都是 ALLOWED。修它要动 reader 的入口条件（把「有 git 词」换成「有 git 词**或**首词是 reserved
  verb」），影响面超出本判据的范围，属另一个 work item。
  `TestChatBashWriteRefusalJudgesAReservedVerbByTheReadOnlyTable` 的注释里写明了这一点，
  用例只用 git 写法 —— **不把期望改宽去迁就缺陷**。

---

## Verification

- **Build**: `go build ./...` → exit 0
- **Vet**: `go vet ./internal/tools/ ./internal/agent/ ./internal/config/` → clean
- **Tests**: `go test -count=1 ./internal/tools/ ./internal/agent/` → `ok internal/tools 3.572s` / `ok internal/agent 0.940s`
- **RED proof**（基线 `HEAD~1`，`git show` 还原改动文件后重跑同一批用例）→ 7 条用例红，
  逐条拿到拒绝语原文（`payload is a string this boundary does not expand`、
  `` `2>/dev/null` is not a read-only operation ``、去掉 verb 路径的仅打印选项被拒 …）
- **Ran / Result**（真实入口，改前 `HEAD~1` vs 改后 `HEAD` 各驱动一遍）：

  | 命令 | 改前 | 改后 |
  |---|---|---|
  | `git worktree list` | ALLOWED | ALLOWED |
  | `git worktree list --porcelain` | 拒（table 无此词） | ALLOWED |
  | `git -C . log --oneline -3 2>/dev/null` | 拒（重定向当 pathspec） | ALLOWED |
  | `sh -c 'git worktree list'` | 拒（payload 不展开） | ALLOWED |
  | `sh -c 'git worktree remove x'` | 拒 | **仍拒**（载荷内 mutation 照拒） |
  | `git worktree remove x` | 拒 | 拒，并**点名** `remove` |
  | 未声明 `go test ./...` | ALLOWED（旧判据不看时间） | 拒，路由 `task_submit` 之外的「先问」 |
  | `timeout_sec=30 git worktree list` | ALLOWED | ALLOWED（理想带） |

- **Doccheck**: `gopi tool doccheck` → corpus consistent
- **Rule 30**: `git status --porcelain` → 空
- **Commit**: `f349f54decb4ee4bba3b297326482f536fa28821`（分支 `task/01a115d167f874a4a1ede1fe21b4dc8c`，15 文件 +1677 行）

---

## 8. 第二代判据：按「动作类别」（task 01a14731，当前判据）

operator 2026-10-11 原话（要点）：**「判据不该是耗时档位。ops/DevOps/网络/脚本/文档/查询/诊断/提
bug 这一类我本来就要亲自动手做的事，chat 直接做。写仓库（改源码、跑门禁验自己的改动、commit）仍然是
硬禁区，走 task_submit。只有『这个动作本身属于长类』（整树测试、完整构建、包安装、merge）才必须先问。」**

### 8.1 三类，一个谓词

`internal/tools/chat_ops_authorization.go` 里只有一个分类函数 `ChatOpClassify(command) ChatOpClass`
与一个判定函数 `ChatOpAdmit(command, operatorTurn) ChatOpAdmission`：

| 类 | 值 | 判据 | 裁决 |
|---|---|---|---|
| OPERATOR WORK | `ChatOpClassOperatorWork` | 类别表未命中的行（**默认**） | **直接跑**，任何 posture 都不问 |
| REPOSITORY WRITE | `ChatOpClassRepoWrite` | ```chat-op-category 围栏里的 `repo-write` 行 | **拒**，路由 `gopi task submit`；**任何 posture 都不放** |
| LONG OPERATION | `ChatOpClassLong` | ```chat-op-category 围栏里的 `long` 行 | 未授权 → **问**；operator 本轮原话有预算 或 posture=auto → 跑 |

**顺序即语义**：repo-write 先判。`make test-budget PKGS='./internal/tools'`、`go test ./internal/...`
同时命中两张表，而裁定的答案是「跑门禁验自己改动 = 写仓库那一半」，所以 repo-write 胜。
分界线是**范围**而不是时长：**改动自己那几个 package 的验证**（`make test-budget`、
`go test ./internal/...`、`golangci-lint run`）属于 gate 运行 → 写仓库；**整树**（`make test`、
`go test ./...`、`go build ./...`）属于长类 → 问。

词表**不再是 Go 字面量**：它们住在 `internal/chatpolicy/category.go` 的**第二围栏**
（```chat-op-category）里，由一个与执行表**不同**的解析器与类型（`ParseCategories` / `CategoryTable`）
读入，随执行表一起在启动时冻结（`tools.SetChatOpCategories`，回落到内置默认文本
`chatpolicy.DefaultCategories`）。`ChatOpClassify` 只消费这张表：命令行先过 **tools 的唯一分词器**
（`ChatShellTokens` + `chatShellClauses`），每个子句按**词前缀**匹配（最长优先、等长按文件顺序），
整行取**最严类**。这是刻意的语义变化——旧的 `strings.Contains` 换成词前缀，`xmake test` 不再算
`make test`，源树写成目录词 `> internal/` 而不是十四条路径清单。

两张词表（`ChatOpRepoWriteVocabulary` / `ChatOpLongOperationVocabulary`）仍**导出**，因为 prompt 渲染的
是同一份表（Rule 13/14）；`ChatOpRenderedClassList` 渲染时丢掉尾 `*`。

**类别表不得污染执行表**：另一个围栏、另一个类型、另一个冻结 holder，
`chatBashPolicyMatch` 一行未读它。可执行证明见
`internal/agent/chat_ops_category_test.go` 的 `TestChatOpCategoryTableCannotDecideWhatRuns`
——把类别表换成空表，执行臂对 `git push origin master` 的裁定逐字节不变。

### 8.6 执行表本身换成了「操作员规则 + 启动 posture」（2026-10-17，epic `01a1510e`）

§1–§8.5 谈的是**类别判据**（这个动作是什么），本节记的是它旁边那张**执行表**（这一行能不能跑）的
更替 —— 两张表是**两个问题**，一直在两个地方回答（`internal/chatpolicy/category.go` 的文件头把
这条分界写成第一段）。

- **执行表不再编码在任何地方**：`internal/tools/chat_write_guard.go` 原来那张十四条 git 动词表、
  部分动词的 pathspec 谓词、只读子表，以及**"读不出来就拒"**（`ssh -o …`、`$FILES`、`~`、glob、
  重定向）全部删除；判定现在来自项目 `GOPI.md` 里的 ```chat-policy 围栏
  （`internal/chatpolicy`，纯函数：围栏文本 → `Policy`；词 → `Match`），围栏没有意见的行交给
  **启动 posture**（`--policy` / `GOPI_CHAT_POLICY` / settings.json `chat_policy`，启动时解析一次后
  **冻结**，见 `internal/agent/chat_policy_posture.go`）。
- **三值而不是二值**：`allow` / `prompt` / `forbidden`。`prompt` 在这个面上不等于放行也不等于拒绝——
  命令**没跑**，模型必须当轮问操作员（`[NEEDS OPERATOR APPROVAL — command NOT run]`）。
  `unless-trusted` 是最严拼写：**表里沉默的行直接拒**，因为"没人读的审批请求等于没放行"。
- **唯一执行点**：`internal/tools/chat_policy_gate.go` 的 `chatBashPolicyMatch` → 分词（**只有
  `tools.ChatShellTokens` 一个分词器**）→ 切子句 → 每子句 `Posture.Resolve` → 整行取**最严**。
- **判据文档**：这张表的当前陈述是 **`devdoc/chat-policy.md`**（三 posture 真跑矩阵、前后差异表、
  规则语法、零值为什么是 on-request 都在那里）。本节只记它与类别判据的**关系**：
  `chatBashPolicyMatch` 不读类别表，`ChatOpClassify` 不读执行表 —— 两条独立证据链。

### 8.2 为什么「直接删掉 30/60」会变严（本任务的第一约束，机理）

旧判据的兜底顺序是：没声明预算 → 看 posture → 全问。而 **posture 的默认就是 all-ask**。
所以旧系统里「没声明预算的普通命令」实际落在**最后**那条 `chatOpIdealBandAssumedReason`
（"不命中长类 → 当作普通工作放行"）。一旦把 30/60 那两个常量删掉、而不补一个显式的类别分类器，
"不命中长类"这条**隐式**的放行分支也一起消失，未声明调用就全部掉进 `posture=ASK` 兜底 ——
**比改造前更严**，而且严得没人预期。

因此新判据把这个**隐式**约定写成**显式默认**：`ChatOpClassify` 的末行 `return ChatOpClassOperatorWork`
就是旧系统里那条理想带假定，现在是唯一、可读、可测的规则。回归测试
`TestChatOpCategoryOperatorWorkRuns` 用**真实命令行 + 空 posture + 什么都不声明**钉住它
（如果谁再把档位改回来，这条测试立刻红）。

### 8.3 `timeout_sec` 不再是判据（但常量仍在，因为文案要念它）

`timeout_sec` 是 bash 工具的**执行上限**（到点 kill 整个进程组），**不是类别声明**：一个 3 秒的
`sed -i` 仍然是写仓库，一个 3 分钟的只读探查仍然是 operator 自己的工作。所以
`chatBashTimeoutDeclared` 从 `internal/agent/chat_write_guard.go` **删除**了，
`tools.ChatBashTimeoutArg` 只作为**被否定的名词**保留（prompt 与拒绝语要说"它不是类别声明"）。
`TestChatOpCriterionReadsNoStopwatch` 钉住 `ChatOpQuickBudgetSec` / `ChatOpAskThresholdSec` 不再存在。

### 8.4 授权来源（长类唯一可问的那一类）

1. **operator 本回合原话**里的时长（`最多 5 分钟` / `give it 10 minutes` / `跑吧` + 任意带单位数字），
   取**最小**一个；只从消息读、不落库（与 `chat_cleanup_authorization.go` 同形）。
2. **常驻姿态** `GOPI_CHAT_OP_AUTO` > settings.json `chat_op_auto` > all-ask
   （`tools.ChatOpChannel`，唯一读者）。它**只**管长类：operator 说"合理的事你自己做"，
   ≠ 允许写仓库。

### 8.5 prompt 与判据同源

- `internal/agent/chat_write_boundary.go` 的规则 (6) 不再渲染两个数字，而是直接拼
  `tools.ChatOpOperatorWorkSentence()` / `ChatOpRepoWriteRefusalSentence()` /
  `ChatOpLongRefusalSentence(tools.ChatOpChannel())` —— **和拒绝语是同一批字符串函数**。
- `internal/agent/chat_pa_duty.go` 的 clause (1b) 同样渲染 `ChatOpOperatorWorkSentence()`，
  并保留 `GOPI_CHAT_OP_AUTO` / `chat_op_auto` / auto / all-ask 的指向。
- `internal/agent/chat_ops_category_test.go` 从两端钉：prompt 里三类标签 == `ChatOpClass.String()`；
  prompt 里的长类词表 == `ChatOpLongOperationVocabulary()` 全量；prompt 不能再教时间带。

### 8.6 活证据与已知边界

- 三条路径的真实验收：`TestChatOpCategoryOperatorWorkRuns` / `...RepoWriteRefused` /
  `...LongAsks`（tools 侧），`TestChatOpCategoryThreePathsAgree`（prompt×guard 同源）。
- **未做**：一次真实交互式 `gopi chat` 会话里观察模型被 prompt 说服（需要 TTY + 模型往返，
  且 Rule 37 禁止 chat 在本树 commit）。等价取证是驱动**真实入口**
  `tools.ChatOpRefusal` / `agent.chatToolWriteGuard`（见 task report 的 `## Evidence`）。

---

## 9. 这一代判据如何落到 master 上（task `01a149ed`，2026-10-17）

第二代判据最初开发于 task `01a14731` 的分支上，那份分支的 base **不含**第一代判据的落地提交
`f349f54de`（"judge an operation by its time and the operator's authorisation"）。因此它作为 diff
看起来像**把 f349f54de 整个回退掉**——这是交接时的真实风险，也是本任务存在的原因。

**核实过的机器事实（都在本次 run 里跑过）：**

- `git merge-base --is-ancestor f349f54de HEAD` → **exit 0**：第一代判据早已在 master 上，不需要
  也不能被"重新实现"。
- 分支三个提交（`4dd23321e` 类别判据 / `4bd95aa6f` 历史读数标注 / `faf64b315` 行中性注释）的
  patch-id **两两不同**，它们不是 f349f54de 的 cherry-pick，也没有别的分支承载它们
  （`git log --all --grep=01a14731` 只在这条线上命中）。
- master 在分支 fork 之后**没有动过**这 12 个文件里的任何一个，唯一例外是 `CONFIG.md` 的两行
  （`6bcd29d49` 的 merge-anchor ref 重指），cherry-pick 时自动合并、无冲突。

**结论（存活模型，一句话）**：**类别判据是唯一判据**——`tools.ChatOpClassify` / `ChatOpAdmit`
三类（OPERATOR WORK 跑 / REPOSITORY WRITE 拒并路由 task / LONG OPERATION 先问），
`timeout_sec` 只是 bash 执行上限、不再被读；第一代判据留下的 30/60 两个档位常量**不存在**于
HEAD，也不在本分支。落地方式是按序 cherry-pick 三个提交到当前 master 之上，**不是**
把分支 merge 回去（merge 会以两个 parent 的形式把"回退 f349f54de"写进历史，cherry-pick 只带
净改动，祖先关系保持不变）。

**同源核对（本次 run 追加的修正）**：cherry-pick 后 `internal/agent/chat_pa_duty.go` 的文件头
注释仍在描述**已废的时间档位**（"The time criterion for exactly that band already exists …
tools.ChatOpQuickBudgetSec / ChatOpAskThresholdSec"），而 clause (1b) 的函数体早已渲染类别句。
这正是 §8.5 说的"prompt 与判据同源"的**反面**：文档头教一套、代码跑另一套。本次一并改成类别
措辞；`devdoc/chat-pa-quick-ops-no-permission.md` 的 §1/§2.1/§2.2/§3 四处同样残留档位读数，
按"历史读数 + 现状指向 §8"处理，保留原文作为来路（那份文档本身是 2026-10-09 裁定的记录）。
