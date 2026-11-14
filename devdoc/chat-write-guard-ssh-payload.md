<!-- doc-class: historical -->
# Task Report: `ssh host 'CMD'` 的只读 payload 走既有递归读路径（chat write guard）

> **这份报告描述的整条路径已不存在（2026-10-17，epic `01a1510e` 第 1–5 项）。**
> `chatWrapperPayloadWords` / `chatGitInvocationsForWords` / `chatWrapperPayload` /
> `chatShellExpansionChars` 这一族 payload 展开器随动词判定表一起被删除，取而代之的是
> 操作员自己写的 ```chat-policy 围栏 + 启动 posture（**`devdoc/chat-policy.md`**）。
> 因此本文件里那条判据**不再适用**：新实现里**没有**"载荷读不出来就拒"这条路径 ——
> `ssh -o … host git push` 既不会被"展开"也不会被"断言拒绝"，它只是若干**词**，由词前缀规则
> 或 posture 回答。实测对照（同一组 13 条命令，改动前后各跑一次）见
> `/home/user/.gopi/sessions/01a1510e-9d42-733e-b1e0-6084810d79db/task-report.md`：
> 基线对 `ssh -o StrictHostKeyChecking=no host git push` 拒绝的理由是
> `its payload is a string this boundary does not expand`，新实现在 `on-request` 下问、在
> `yolo` 下放行、在 `unless-trusted` 下拒 —— 三种答案都不再经过"载荷可读性"这个判据。
> 本文件作为历史记录保留（它解释过的那两个缺陷是真的）。

Task `01a12b2ece2e75d096d89b53478309f6` · branch `task/01a12b2ece2e75d096d89b53478309f6`

## Completed

- **`ssh` 与本机 shell wrapper 走同一条递归读 payload 路径。**
  `internal/tools/chat_write_guard.go` 的 `chatWrapperPayloadWords` 原来是
  `!chatGitShellWrappers[clause[0]]` 就返回 `(nil,false)` —— 只有 POSIX shell 的
  `-c '<cmd>'` 载荷会被重新分词并递归进 `chatGitInvocationsForWords`，`ssh host 'CMD'`
  则落进 `chatGitPathAssertionWrapper` 的「一律拒绝」集合。现在入口先把 wrapper 的
  载荷 `STRING` 取出来（新函数 `chatGitStringWrapperPayload`），**再**做原来的
  `chatShellExpansionChars` 检查与 `chatShellTokens` 分词，递归那一跳一个字没改。
- **两个可读形态共用这一个门，理由是同一条**：载荷是**本机可见的一个字符串**，
  不需要任何展开就能读。shell 的是 `-c` 的操作数（`chatWrapperPayload`，原样保留）；
  ssh 的是目的地操作数**之后**的词（新函数 `chatSSHPayloadWords`）——ssh 会把
  目的地之后的词拼成远端 shell 执行的字符串，所以 `ssh host git status` 与
  `ssh host 'git status'` 是同一条远端命令，重新 `Join` 回一个字符串后两者
  进入同一个 tokenizer，落到同一个判定。
- **判定表一个字没动**：只读判定仍是唯一那张 `chatGitReadOnlyVerdict` +
  `ChatBannedGitSubcommands`（`chat_write_guard_readonly.go`），没有新解析器、没有
  新白名单、没有改工具面。这次改动**只增加「能读到什么」**，不改变「读到之后怎么判」。
- **不懂就拒的原则保持（并且是本次实现的一等约束）**：ssh 只读 **无选项形态**
  （`ssh <host> <cmd…>`）。带选项的形态（`ssh -p 2222 host CMD`、`ssh -i KEY host CMD`、
  `ssh -o … host CMD`）里目的地可能是某个选项的操作数，**载荷边界不可证明** ——
  猜边界会读得比载荷**更少**，而边界之后的一句 `git push` 会被放行，这是唯一
  真正会丢东西的方向。所以这些形态**原样保留断言（拒绝）**：拒绝得比需要的更窄，
  代价只是"多拒一条只读命令"，而不是"漏一条变更命令"。

## Not Done

- **ssh 的选项形态仍然是拒的**（`ssh -p 2222 peerbox 'git worktree list'` → 断言拒绝）。
  要做到放行它，需要复刻 ssh 的整个选项文法来判断哪个词是操作数、哪个词是目的地；
  那是本任务明确排除的「新写解析器」。当前方向的失误是**保守**的（多拒不少拒），
  已在测试里作为**有意行为**钉住（`TestChatSSHUnreadableFormsKeepTheirAssertion`）。
- **没有在真实 chat 会话里让模型选择 ssh 拼写**（Rule 11 的"模型真调用"腿）。
  本任务改的是**判定函数**而非工具面/描述/prompt 路由，`ssh` 这条路由本来就在
  prompt 里（远端会话是已存在的能力），所以没有「prompt 说服」这一半要验证；
  但如实说：真实 chat 会话那一腿没有取证，见 `## Verification` 的 `Ran:` —— 证据是
  **守卫自己的入口**被真实调用，不是模型在会话里发出这条命令。

## Verification

- Build: `go build ./...` → **OK**（`BUILD_OK`，无输出）
- Tests: `go test ./internal/tools/ -run 'TestChat' -count=1` → **ok
  github.com/nick/gopi/internal/tools 2.285s**
  · `go test ./internal/tools/ -run 'TestChat|TestPeerAlias' -count=1` → **ok**
  · 声明入口 `make test-budget PKGS='./internal/tools'`（smoke + branch-scoped）→
  见 `artifacts/test-budget-tools.txt`。**该次运行 exit=1，但失败与本改动无关**：
  唯一失败是 `TestTagsIndexProbeOnRealCtagsBytes`（`torn_index_test.go:49`，
  `the header probe rejected a COMPLETE index`）。已按 Rule 41 取证定性并提行，
  不在这里含糊：证据 `artifacts/BUG-ctags-index-probe-rejects-complete-index.txt`
  与 `artifacts/BUG-ctags-index-probe-BYTES.txt`。两条事实钉死它的归属：
  （a）本 run 的 diff 只碰 `chat_write_guard*.go` 与 `devdoc/`，**不含**
  `torn_index_test.go` / `symbol.go`；（b）把本 run 的改动还原到旧基线
  （`db24657f8` 的 `chat_write_guard.go`）后**同一条测试同样 FAIL**（exit 1）。
  根因：本机（hostb-Laptop）的 `ctags` 是 **Exuberant 5.9~svn20110310**，
  其 `-f` 输出**不含** `!_TAG_FILE_FORMAT` 头（实测首 64 字节是
  `20 20 20 20 20 20 47 4f 50 49 5f…`，即条目而非伪标签），而
  `tagsIndexValid`（`internal/tools/symbol.go:48`）要求该前缀 ⇒ 完整索引被判无效。
  peerbox 是 **Universal Ctags 5.9.0**（实测 `ssh peerbox 'ctags --version'`），故只在
  HP 出现。已作为 bug 提至 peerbox：**`01a12b49-8921-7684-8928-4c17e259417f`**
  （`--tags bug`，payload = 上述证据 + 5-why + 建议方向）。
- Ran: **Rule 31 真跑 —— 守卫自己的入口被真实调用**。
  守卫没有 CLI 面（`ChatBashWriteRefusal` 在 `internal/agent/chat_write_guard.go`
  的 `chatToolWriteGuard` 里被调用，没有任何 verb 暴露它），所以用一个**临时**
  `go run` harness 直接调用 `tools.ChatBashWriteRefusalAuthorized`（**就是
  `chatToolWriteGuard` 调的那一个函数、同一个参数表**），真命令、真输出，
  跑完删除（Rule 35：不作为交付物留在树里）：

  ```bash
  cd <worktree> && go run ./tmp-rule31-guard-harness \
      'ssh peerbox '"'"'git worktree list'"'"'' \
      'ssh peerbox '"'"'git push origin master'"'"'' \
      'sh -c '"'"'git worktree list'"'"'' \
      'ssh -p 2222 peerbox '"'"'git worktree list'"'"''
  ```

- Result: 原始输出（逐字，全文在
  `~/.gopi/sessions/01a12b2ece2e75d096d89b53478309f6/artifacts/rule31-guard-raw-output.txt`）：

  ```
  ADMITTED  ssh peerbox 'git worktree list'
  REFUSED   ssh peerbox 'git push origin master'
  Refused by the CHAT WRITE BOUNDARY (bash ran `git push`): this chat owns no branch and no worktree …
  ---
  ADMITTED  sh -c 'git worktree list'
  REFUSED   ssh -p 2222 peerbox 'git worktree list'
  Refused by the CHAT WRITE BOUNDARY (`git ssh`): its payload is a string this boundary does not expand, so the git invocation inside it cannot be read, so it is refused rather than guessed at.
  ---
  ```

  即：`ssh peerbox 'git worktree list'` 放行（修复前被拒），`ssh peerbox 'git push origin master'`
  以 **`git push`** 为名拒绝（不是以 `ssh` 为名——递归读到了载荷并点名了输掉的动词），
  `sh -c 'git worktree list'` 不回归，带选项的 ssh 保持断言拒绝。
- red→green（旧基线取证）：把改动文件还原到 HEAD
  （`git show HEAD:internal/tools/chat_write_guard.go > internal/tools/chat_write_guard.go`）
  后跑同一批新测试 → **FAIL，14 条 `FAIL:`**，原始输出
  `~/.gopi/sessions/01a12b2ece2e75d096d89b53478309f6/artifacts/red-baseline.txt`。
  **独立复测（更宽的载荷集）**：同一旧基线再跑一次，载荷集扩到 5 条只读 + 5 条变更
  → **24 条 `FAIL:`**（5 顶层 + 19 子测试），原始输出
  `artifacts/rule31-prefix-baseline-RED.txt`。两次的数字不同只是**覆盖面不同**（同一基线、
  同一测试选择器），不是观测漂移：两条都留档，谁引用都指向自己跑的那一份（Rule 38）。
  旧基线的两条具体拒绝语：

  ```
  the same payload is judged differently by the two wrappers — shell="" ssh="Refused by the CHAT WRITE BOUNDARY (`git ssh`): its payload is a string this boundary does not expand, so the git invocation inside it cannot be read, so it is refused rather than guessed at. …"

  ChatBashWriteRefusal("ssh peerbox 'git worktree list'") refused a read-only remote command:
      Refused by the CHAT WRITE BOUNDARY (`git ssh`): its payload is a string this boundary does not expand, …
  ```

  还原修复版本后同一批测试 → **ok**。
- Commit: 见本任务分支 `task/01a12b2ece2e75d096d89b53478309f6`。

## Evidence (self-filed)

- `go build ./...` → 无输出 + `BUILD_OK`（观察到的字符串）。
- `go test ./internal/tools/ -run 'TestChat' -count=1` → `ok github.com/nick/gopi/internal/tools 2.285s`。
- 新测试 4 个：`TestChatSSHPayloadIsJudgedExactlyLikeAShellPayload`（对称性：同一载荷在
  `sh -c` 与 `ssh <host>` 下**判定相同**，且方向被单独断言，所以"两边都拒"不能冒充通过）、
  `TestChatSSHQuotingVariantsReadTheSamePayload`（引号/非引号/`user@host`/多子句载荷边界）、
  `TestChatSSHUnreadableFormsKeepTheirAssertion`（不懂就拒）、
  `TestChatWrapperPayloadWordsReadsTheTwoStringShapes`（门本身的单元钉：可读集合不能被一次
  编辑悄悄收窄回 shell，也不能放宽到载荷不是命令行的那几个 wrapper）。
- 旧基线 RED：`artifacts/red-baseline.txt`，`grep -c "FAIL:"` = **14**，退出码 1。
- Rule 31 原始输出：`artifacts/rule31-guard-raw-output.txt`。
- `git status --porcelain`（worktree 内）：本次改动的 5 个文件，见提交；收尾后为空。
- 无关失败的归属判定（Rule 41）：`go test ./internal/tools/ -run
  '^TestTagsIndexProbeOnRealCtagsBytes$' -count=1` 在**本 run 修复树**与
  **旧基线（`db24657f8` 的 `chat_write_guard.go`）**上**都是** `--- FAIL` /
  `torn_index_test.go:49: the header probe rejected a COMPLETE index` ⇒ 与本 diff
  无关（本 diff 不含该测试或 `symbol.go`）。本机 `ctags --version` =
  `Exuberant Ctags 5.9~svn20110310`，真实索引用 `head -4 … | cat -A` 看**首行即条目**、
  首 64 字节 `head -c 64 | od -An -tx1` 无 `!_TAG_FILE` ⇒ 探针的 header 前提在本机
  不成立。已提 bug：peerbox `01a12b49-8921-7684-8928-4c17e259417f`（回读确认
  `Task queued: 01a12b49-8921-7684-8928-4c17e259417f`）。

## 本 run 实测澄清（补充，不改动上面的既有观测）

两条「看着不对」的形态被真跑定性，证据都在 `artifacts/`：

1. **`ssh peerbox "$CMD"` 与 `sh -c "$CMD"` 都是 ADMITTED —— 而且修复前就是。**
   实测（`artifacts/rule31-expansion-symmetry-raw.txt` 与
   `artifacts/rule31-expansion-symmetry-PREFIX-baseline.txt`，同一组 8 条命令、
   修复后与旧基线各跑一次）：

   ```
   修复后:                               旧基线(db24657f8):
   ADMITTED  ssh peerbox "$CMD"            ADMITTED  ssh peerbox "$CMD"
   ADMITTED  sh -c "$CMD"                ADMITTED  sh -c "$CMD"
   ADMITTED  bash -c "$CMD"              ADMITTED  bash -c "$CMD"
   ```

   机理：`"$CMD"` 是一个**引号词**，含 `$` → `chatWrapperPayloadWords` 返回
   `ok=false`，clause 落到 `chatGitPathAssertionWrapper` 分支 → 返回**一个断言**；
   而 `chatGitInvocations` 的入口早退条件是「**字面文本里没有 `git` 单词**」——
   `ssh peerbox "$CMD"` 全句没有字面 `git`，于是**根本没进 tokenizer**，断言那条路
   从未被走到。`ssh peerbox "$CMD" && git worktree list`（含字面 `git`）则**以
   `git ssh` 为名拒绝** —— 两种情况有据可查、互不矛盾。这是**既有**行为，与本次
   改动无关（fix 前 fix 后逐字相同），且 ssh 与 `sh -c` 在这条形态上**是对称的**。
   不是回归，本 run 不修（Rule 41：现象已取证、边界已定位，改动范围在既有递归路径）。
2. **`ssh peerbox git commit -m x` 由「断言拒绝」变为「与明文拼写同判」。**
   旧基线 `REFUSED (\`git git commit\`)`（断言，退化的主题串）→ 修复后
   `REFUSED (bash ran \`git commit\`)`（**点名真正的动词**）。方向未削弱：仍是 REFUSED，
   只是现在读得到载荷、拒得更准（`artifacts/rule31-expansion-symmetry-*.txt`）。
3. **`git reset --hard HEAD~1` 两侧都走断言**：`~` 在 `chatShellExpansionChars`
   （`"$*?[]{}~"`）里，`HEAD~1` 使载荷被判为不可读 → 断言拒绝，与
   `ssh` 的选项形态同一条路。两侧**都是 REFUSED**，只是断言主题串一边是 `git sh`、
   一边是 `git ssh` —— **方向一致、不损失**，对称性测试因此把断言形态与放行/变更
   分别断言，不让「两边都拒」冒充通过。

## 设计记录（为什么是这个形状）

1. **为什么复用 `chatWrapperPayloadWords` 而不是在 ssh 分支加一条并列路径**：
   递归那一跳（`chatGitInvocationsForWords(inner)`）才是"载荷按同样规则判"的**唯一**
   保证。并列一条路 = 第二张判定语义 = 下一次修 wrapper A 忘了 wrapper B。
   所以 ssh 的接入点放在**取载荷字符串**这一步，之后的 `chatShellExpansionChars`
   检查与 `chatShellTokens` 分词两半是 shell 与 ssh **共用**的。
2. **为什么 ssh 不进 `chatGitShellWrappers`**：那个集合的语义是"载荷是 `-c` 的
   **标志操作数**"，而 ssh 的载荷是**目的地之后的词**。混进一个集合会让
   `chatWrapperPayload`（找 `-c`）对 ssh 返回 `("",false)` —— 看起来是读，实际是
   **把 ssh 变成静默空载荷**，那比拒绝危险得多。两个形态由
   `chatGitStringWrapperPayload` 分开识别，**合流在递归处**。
3. **目的地不做 git 词特判**：`ssh git worktree list` 里 `git` 是**目的地**，所以它
   之后的 `worktree list` 是载荷。特意不为"目的地看起来像 git"开特例：特例化会
   制造一个"猜边界"的分支，而边界猜错的代价是漏一条变更命令（
   `ssh git worktree remove /tmp/wt` 会变成放行）。现在它在同样的递归里以
   `remove` 输掉。
4. **为什么临时的 Rule 31 harness 必须删**：它是 Rule 35 意义上的"新持久依赖"的
   反面样本——一个只为取证活一分钟的 `main.go`。留着它会让 `./...` 里多一个包，
   并给下一个人一个"这是入口"的错觉。删除动作本身写在 `## Verification` 里，
   以便复核者知道它曾经存在（Rule 15：不静默）。
