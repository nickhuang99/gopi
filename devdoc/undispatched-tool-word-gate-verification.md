<!-- doc-class: historical -->
# Task Report: 收口 `TestUndispatchedToolWordsNameADispatchingCanonical` — 断言归属与取证

Task `01a11461-db68-7ad5-a13c-b9fdf5150ca4`（sibling：`01a1144e-3f6e-7b05-b42b-0ca941f0f78b`，已 status=completed）。
本任务**只做判定与取证，不改产品代码**：被测断言在 master 上已经是绿的，本报告要回答的是
「它是不是退役表/拼写表的正确契约，以及它属于哪一行」。

基线：`HEAD = 3255973cc`（== `master` 的 tip，`git rev-list --left-right --count master...HEAD` → `0 0`），
测量主机 `hostb-Laptop`，worktree `/home/user/workspace/gopi-01a11461db687ad5a13cb9fdf5150ca4`。

## Completed — 判定（4 条，每条都有真跑证据）

### ① 这条测试现在测的不是「规范拼写必须派发」，而是「拒绝里印出的地址必须真能跑」

`cmd/agent/cli_undispatched_tool_words_test.go:42` 的断言体是：

- 每行 `word/canonical/reason` 非空（反空洞）；
- `e.refusalAddress() != programAddress(e.word)` —— 拒绝不得指向被拒词自己；
- `refusalAddressDispatches(t, e.refusalAddress())` —— 拒绝印出的那条命令**本二进制真的跑得到**（真跑二进制，Rule 31）；
- `!dispatched[e.word]` —— 被拒词不得同时是派发词。

换成「规范拼写必须派发」的那一版**已经不存在**了：`git show 6f20bdd87^:cmd/agent/cli_undispatched_tool_words_test.go` 显示旧断言是
`dashTwin(e.canonical) == dashTwin(e.word)`（word-vs-word 的拼写关系比较），它在 `844d1f3d2` 把 `canonical` 从「命令词」改成「地址片段」之后
**恒真**（每个 row 都读成 "refuses itself"）→ master `b7b39a57f` 上 8 行全红。`6f20bdd87`（作者 gopi，2026-10-07 09:49）把它换成上面的地址式断言。
**本分支不是这个改动的作者**：`6f20bdd87` 已是 `master` 的祖先（`git merge-base --is-ancestor 6f20bdd87 master` → 真），而本 run 的 worktree 恰好停在它之上（HEAD == master tip）。

### ② 与 `devdoc/cli-naming-convention.md` rule1/rule2 一致，且是 rule1/rule2 在「拒绝」这一半的机器化形式

该文档的两半：**rule1**（工具派生词保留下划线，字节等同 `internal/toolnames`）、**rule2**（gopi 自己的 CLI 动词走短横）。
文档自身给出了「未派发拼写被指名拒绝、rc=1、无 session、无 ledger row、提示指向规范拼写」的契约。
测试的 `refusalAddressDispatches` **正是这一句的可执行形式**：它不比较字符串，而是把地址真跑一次
（`resolve` 成 `gopi tool code-search` / `gopi tool web search`，见下）。所以「提示指向一个能派发的地址」不是对文档的偏离，而是文档里
「a hint pointing at nothing would be worse than the run it replaces」的 gate 化。rule1/rule2 决定**拼写**，这条 gate 决定**拼写的出路可执行**，二者不冲突。

### ③ 归属：三分，不是二分

| 部分 | 归属 | 证据 |
|---|---|---|
| 地址式断言本身（`refusalAddress()` / `refusalAddressDispatches` / `programAddress(e.word)`） | **master 的 `6f20bdd87`**（属于「gate 自证恒真」修复线，不是 01a1144e 那行） | 本分支的测试文件与 sibling 分支 `6685f7f83` 的副本 **逐字节相同**（`git diff HEAD 6685f7f83 -- cmd/agent/cli_undispatched_tool_words_test.go` → 空输出） |
| 拒绝的**地址内容**（每行指向哪条活门） | **兄弟行 `01a1144e-3f6e` 的修复** | sibling 把 `canonical` 从命令词改成地址片段、给每行加 `homeProg`；commit `6685f7f83`（qingzhe huang，2026-10-07 11:27，晚于 `6f20bdd87`） |
| 测试文件本身 | **兄弟分支**（`git log 6685f7f83 -- cmd/agent/cli_undispatched_tool_words_test.go` → `6f20bdd87`、`17e7ed8b2`、`844d1f3d2`…）—— 该文件是那行的产物，`6f20bdd87` 只是这行历史上的一个点 | 同上 |

**注**：sibling 的 `6685f7f83` **尚不在本机任何 master**（`remotes/peerbox/master` 也不含它），只在 `remotes/gitPushcheck/master` 上；
本分支的测试通过**不依赖**它。

### ④ 拒绝表实跑与文档表格已经对不上（既有缺口，**不属于本项**）

`devdoc/cli-naming-convention.md` 的拒绝表（master 版本，`4a7e905bc` 最后一次改动）写的是
`code_search → gopi code-search`，而二进制现在拒绝 `code-search`：
`gopi code-search -h` → rc=1 `RETIRED — use gopi tool code-search`。表格右侧「Points at」那一列在 master 上是**过期**的。
sibling 的 `6685f7f83` 里改了这张表（`| `code_search` | `gopi tool code-search` |` 并把表头改成 `Points at (the LIVE door)`，新增 web dash twin 两行），
但那个 commit 未落 master。**结论**：文档缺口属兄弟行的补丁范围，本项只报证不修（本 run 不动产品代码、不动文档）。

## Not Done

- 未修 `devdoc/cli-naming-convention.md` 的拒绝表地址列（见 ④）：属兄弟行补丁范围，本项禁止重复修。
- 未把 sibling `6685f7f83` 带到本分支（无需求，且会构成第二次实现）。
- `ui_state` 的双形式归属（顶层词 vs tool 面）已有单一出处（`cli_undispatched_tool_words.go` 的 `ui_state` 行：`homeProg` 为空 = 词本身是门），
  本项只做**取证确认**（`gopi ui_state -h` rc=0 / `gopi ui-state -h` rc=1 且指回 `ui_state`），未改代码。

## Verification

- Build: `CGO_ENABLED=0 go build -o /home/user/.gopi/sessions/01a11461db687ad5a13cb9fdf5150ca4/gopi-verify ./cmd/agent/` → rc=0，产物 31013045 字节（二进制建在 **session 目录**，不落 worktree）。
- Tests: `go test ./cmd/agent/ -count=1 -run TestUndispatchedToolWordsNameADispatchingCanonical -v` → `--- PASS: TestUndispatchedToolWordsNameADispatchingCanonical (5.00s)` / `ok  github.com/nick/gopi/cmd/agent  9.156s`，rc=0。
- Ran:
  1. 七个规范拼写逐个真跑（隔离 profile，provider 端点被指到 `127.0.0.1:9`）：
     `env -i PATH=/usr/bin:/bin HOME=<sess>/probe-home GOPI_HOME=<sess>/probe-home ANTHROPIC_BASE_URL=http://127.0.0.1:9 DEEPSEEK_BASE_URL=http://127.0.0.1:9 timeout 25 <sess>/gopi-verify <word> -h`
  2. 门（地址）逐个真跑：同形，参数换成 `tool code-search -h` / `tool keyword_search -h` / `tool web search -h` / `ui_state -h` …
  3. 文档承诺的地址真跑：`<word>` ∈ {`code-search`,`vision_analyze`,`screen_capture`,`voice_input`,`web_search`}。
  4. 非空洞（mutation）真跑：把 `code_search` 行的地址改成被拒的 `gopi code-search`（**只写 session 目录的副本 + `go test -overlay=`**，worktree 内文件一行未改）。
- Result:
  - 规范拼写（rc / 首行，原文见 `Evidence`）：`keyword_search` rc=1 RETIRED；`code_search` rc=1 "not a gopi command … spelled `gopi tool code-search`"；**`ui_state` rc=0**（真页面）；**`web_search` rc=0** 且首行就是 `gopi web_search: retired — use \`gopi tool web search\``；`vision_analyze` / `screen_capture` / `voice_input` 均 rc=1 RETIRED。
  - 门：`tool code-search` rc=0、`tool keyword_search` rc=0、`tool vision_analyze` rc=0、`tool screen_capture` rc=0、`tool voice_input` rc=0、`tool web search` rc=0、`tool web fetch` rc=0、`ui_state` rc=0 —— **每一个拒绝印出的地址都真的跑得到**（这正是断言③断的性质）。
  - mutation：`--- FAIL: … cli_undispatched_tool_words_test.go:81: "code_search" prints the address "gopi code-search", which this binary does not run — the hint leads nowhere`，rc=1 → 断言**非空洞**。
- Commit: 见本文件所在 commit（本 run 分支 `task/01a11461db687ad5a13cb9fdf5150ca4`）。

## Evidence (self-filed)

1. **规则拼写逐个真跑**（`<sess>/evidence/canonical-probe.txt`，1410 字节，已回读非空）：
   ```
   keyword_search     rc=1   | gopi: `keyword_search` is RETIRED — use `gopi tool keyword_search` (`gopi tool keyword_search -h` for its help). To send the word as a prompt instead, say so explicitly: `--prompt keyword_search`.
   code_search        rc=1   | gopi: "code_search" is not a gopi command — this one is spelled `gopi tool code-search` on the command line (`gopi tool code-search -h` for its help). To send the word as a prompt instead, say so explicitly: `--prompt code_search` (or the GOPI_PROMPT env var).
   ui_state           rc=0   | gopi ui_state — read a live `gopi ui` screen state as an interpreted cell grid
   web_search         rc=0   | gopi web_search: retired — use `gopi tool web search` (the same tool, one spelling)
   vision_analyze     rc=1   | gopi: `vision_analyze` is RETIRED — use `gopi tool vision_analyze` (`gopi tool vision_analyze -h` for its help). …
   screen_capture     rc=1   | gopi: `screen_capture` is RETIRED — use `gopi tool screen_capture` (`gopi tool screen_capture -h` for its help). …
   voice_input        rc=1   | gopi: `voice_input` is RETIRED — use `gopi tool voice_input` (`gopi tool voice_input -h` for its help). …
   ```
   —— 注意 `web_search` 的 rc=0 **不是**「规范拼写被派发到工具」：它走的是 `--help` 臂（`gopi web_search` / `gopi web_search foo` 都是 rc=1，见 `evidence/door-probe.txt` 末段），首行就是拒绝文本。
2. **门真跑**（`<sess>/evidence/door-probe.txt`，2293 字节）：`tool code-search` rc=0（`GoPi code-search subcommand — expose the code_search tool as a CLI command.`）、`tool keyword_search` rc=0、`tool vision_analyze` rc=0、`tool screen_capture` rc=0、`tool voice_input` rc=0、`tool web search` rc=0、`tool web fetch` rc=0、`ui_state` rc=0；反证 `tool code_search` rc=1、`tool keyword-search` rc=1、`ui-state` rc=1（指向 `gopi ui_state`）。
3. **文档承诺地址真跑**（`<sess>/evidence/doc-address-probe.txt`，1063 字节）：`code-search` rc=1（RETIRED 指向 `gopi tool code-search`）、`vision_analyze` rc=1、`screen_capture` rc=1、`voice_input` rc=1、`web_search` rc=0（拒绝页）→ 文档表格「Points at」列在 master 上过期（判定 ④）。
4. **非空洞 mutation**：`go test -overlay=<sess>/mut/overlay.json ./cmd/agent/ -count=1 -run TestUndispatchedToolWordsNameADispatchingCanonical` → FAIL，`cli_undispatched_tool_words_test.go:81` 指名 `"code_search" prints the address "gopi code-search", which this binary does not run`。
5. **断言归属的对象证据**：`git diff HEAD 6685f7f83 -- cmd/agent/cli_undispatched_tool_words_test.go` → **空**；`git diff --stat HEAD 6685f7f83 -- cmd/agent/cli_undispatched_tool_words.go` → `59 insertions(+), 110 deletions(-)`；`git merge-base --is-ancestor 6f20bdd87 master` → 真、`6685f7f83` → 假。
6. **worktree 干净**：`git status --porcelain` 在交付时为空（证据与临时产物全部写在 `<sess>/evidence`、`<sess>/mut`、`<sess>/gopi-verify`）。
7. **文档门（Rule 36）**：`gopi tool doccheck` → `doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row`，rc=0（本报告 + `DOC-INDEX.tsv` 一行已同 commit 落地）。
   `go test ./internal/standards/ -run TestDocClassBannerMatchesManifest` 在工作树上 **FAIL**，但**不是本报告造成的**：把本报告与 `DOC-INDEX.tsv` 那一行都撤掉的**干净树**上同样 FAIL，
   且报错只点 `devdoc/cli-help-user-audit.md`（`carries the "historical" banner but ... classes it "normative"`；doc 计数 `1167` vs 干净树 `1166`，唯一差异是本报告且未出现在报错里）。该红是 master 上的既有状态，不在本项范围。
