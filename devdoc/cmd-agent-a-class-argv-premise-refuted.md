<!-- doc-class: historical -->
# Task Report: cmd/agent 的 A 类失败 —— 前提不成立；实际落点是「裸字面量」残留

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a10e03-990b-73f0-8684-d6519f235194` (epic 的 [1/4] 切片)
Branch: `task/01a10e03990b73f08684d6519f235194`
Tree at start: `ba3120f0` (master), worktree clean.

Truth class: **normative** for the measurement below; the numbers are a CENSUS
figure and name their generator and commit (Rule 38).

## 结论先说（最重要的发现）

**本任务的题目前提，在这棵树上不成立。**

Briefing 声称：`checkpoint/archive/brew/memsync/proxy/code-search/screen_capture/
vision_analyze/voice_input/text2img/scratch-gc/annotate/capture/shot/ledger-window`
等顶层拼写「已退役」，一批 `*_test.go` 仍以它们作 argv[0] 驱动真二进制，因而落到
拒绝路径，断言失败，要改写成家族地址。

实测（真二进制，argv[0] 探针）：

```
checkpoint       exit=0    archive       exit=0    brew        exit=0
memsync          exit=0    proxy         exit=0    code-search exit=0
screen_capture   exit=0    vision_analyze exit=0   voice_input exit=0
text2img         exit=0    scratch-gc    exit=0    annotate    exit=0
capture          exit=0    shot          exit=0    ledger-window exit=0
```

这 **15 个词在本树里全部仍被顶层派发**，各自打印自己的 usage 页，`-h` 退出 0。
真正被退役的只有 **phase-1 的四个**：`log` / `stats` / `raw` / `resume`（exit=1，
拒绝文案指向 `gopi session <verb>`）。

**根因**：退役的「生产半」`d3492e0a`（"feat(cli): retire the top-level verb
spellings behind family doors"）**不在这棵树里**：

```
$ git merge-base --is-ancestor d3492e0a HEAD  →  1 (NOT ancestor)
$ git for-each-ref --contains d3492e0a
    refs/heads/task/01a10dc306ad78b7b65313efd18879bc   ← 另一个 worktree 的分支
```

它出现在 `git log --all` 里，所以 briefing 的作者看得到它；而它从未进入 master。
本树 master 只包含 phase-1（`324577e8`/`98bf2c71`/`4971f060`），**不含**完整退役。
（本 run 期间 master 还从 `ba3120f0` 前进到 `31109ca1`，仍然不含 `d3492e0a`。）

**后果**：把 `d3492e0a` 的测试面「移植」过来，不是修复而是**制造回归**——那些测试改用的
`verbRetireAddress` / `retiredArgv` / `runRetiredWordProbe` / `verbRetireWords` 都是
随被拒的拼写一起引用的**生产符号**，本树没有，`go vet` 直接报 `undefined`。更危险的是大多数
文件**能通过编译**：它们只是把一个「还在工作」的探测改成了一个「被拒绝」的探测。

### 红/绿对照（同一文件，同一命令）

```console
--- GREEN: master(HEAD) 的 cli_resume_status_e2e_test.go ---
179: testutil.RunGopi(t, []string{CliVerbSession, CliVerbResume, resumeStatusTaskID}, …)
$ go test ./cmd/agent/ -run TestResumeWritesTerminalStatus
ok      github.com/nick/gopi/cmd/agent   9.350s

--- 换上 d3492e0a 的同名文件 ---
179: testutil.RunGopi(t, []string{"resume", resumeStatusTaskID}, …)
$ go test ./cmd/agent/ -run TestResumeWritesTerminalStatus
    cli_resume_status_e2e_test.go:181: resume must succeed: code=1
        gopi: "resume" is not a gopi command — this one is spelled `gopi session resume` …
FAIL
```

master 那一版**本来就已经是家族地址**；`d3492e0a` 那一版把它**还原成退役拼写**。
移植 = 让绿变红。这就是我没有移植的原因。

## Completed（实际交付）

1. **前提取证与否定**（本任务的主要产出）：用真二进制逐词实测 + `merge-base
   --is-ancestor` 判定生产半的归属，证明 A 类失败在本树不存在，并给出红/绿对照。
   证据：`a-class-evidence.txt`、`regression-proof.txt`、`premise-evidence.txt`（会话目录）。

2. **A 类验收 grep 已达成**（这就是 briefing 的第 2 条验收标准）：

   - `exec.Command(bin, "<退役词>", …)`：**0 处**。
   - `[]string{"<退役词>"}` 作为 argv 头驱动真二进制：**0 处**。
   - 唯一残余的裸字面量是 `cli_vision_test.go:93` 的 `isVisionSubcommand([]string{"log"})`
     —— 它**不是**驱动真二进制，而是「这个谓词不得吞掉兄弟词」的**负向断言**。即便如此，
     按其本意它就是一个退役词，所以我把它改成常量引用。

3. **裸字面量收敛为常量**（Rule 1/13/14 在测试面的执行，3 个文件 4 处）：

   | 文件 | 改前 | 改后 |
   |---|---|---|
   | `cli_vision_test.go:93` | `[]string{"log"}` | `[]string{CliVerbLog}` |
   | `cli_keyword_search_test.go:83` | `"log", "task"` | `CliVerbLog, CliVerbTask` |
   | `cli_code_search_test.go:81` | `"web_search","memory_search","log","text2img","vision_analyze"` | `toolnames.ToolWebSearch, CliVerbMemory, CliVerbLog, toolnames.ToolText2img, toolnames.ToolVisionAnalyze` |

   改后 `go vet ./cmd/agent/` 干净。

## Not Done（诚实缺口）

- **briefing 要求的「A 类 argv 改写」没有做，因为它会制造回归**。理由与红/绿证据见上。
  这不是回避：改写的对象在本树不存在，照做等于把 15 个仍可用的探测改成被拒绝的探测。
- **`memory_search` 的常量不存在**：我用了 `CliVerbMemory`（组名词）替代。原字面量是
  工具线的旧名；`toolnames` 里今天没有对应的 `ToolMemorySearch`（`internal/toolnames/names.go:116`
  注释说明那批独立 memory 工具已并入 `memory(action=…)` 组）。该负向断言只要求「不是
  code-search 的兄弟」，组名词满足语义；但严格说这不是「原字面量自己的常量」。
- **本切片与兄弟任务的重叠**：`01a10e23-d44b7e72`（B/C 类）与 `01a10e23-d2ef`（重新
  合并 `feat/verb-retirement-complete` 解冲突）**都还没跑**。`d2ef` 是解冲突、把完整退役
  合进 master 的那一步；**它落地之后，本切片的 A 类才真正存在**，届时需要按家族地址重写
  ——本报告的前提取证（哪些词在哪个 commit 退役）正是给那一步用的输入。
- **`grep` 判据的诚实边界**：我给出的是「argv[0] 是裸退役拼写、且驱动真二进制」的判据。
  一个把退役词放进**变量再传给** `exec.Command` 的**间接**形态不会被这个 grep 抓到
  （Rule 32 对同类 gate 的诚实缺口注记同样适用）。我用 `RunGopi` 全量清单人工核过一遍
  （见下 Evidence 第 3 条），未发现该类形态。

## Verification

- Build: `go vet ./cmd/agent/` → 干净（无输出，exit 0）；`CGO_ENABLED=0 go build -o
  $S/gopi ./cmd/agent/` → OK。
- Tests（**全包真跑，Rule 31**）：`go test -c -o $S/cmdagent.test ./cmd/agent/` 后
  `cd cmd/agent && $S/cmdagent.test -test.timeout 30m -test.v` 于 clean 树（`ba3120f0`,
  `git status --porcelain` 空）：
  `EXIT=0`，`--- PASS` **2214**，`--- FAIL` **0**，`--- SKIP` **36**。
  （改用 `go test ./cmd/agent/` 直接跑时，因逐包缓冲在 3 分钟内无输出会被 idle 网掐死，
  所以用 `-c` 编译 + 直接执行 + `-test.v` 落盘；日志 `agent-before-v.log` 808 KB 全量。）
- Tests（**改动之后**，`f5ecb6f9` 的全部 5 个改动一起）：`go test -c -o $S/cmdagent-after.test
  ./cmd/agent/` 后 `cd cmd/agent && $S/cmdagent-after.test -test.timeout 30m -test.v` →
  `EXIT=0`，`--- PASS` **2214**，`--- FAIL` **0**，`--- SKIP` **36**。
  **与改动前逐项相同**（2214/0/36）——这正是本次改动的性质：把 4 处裸字面量收敛为常量，
  不改行为。
- Ran: 见 Evidence 第 1 条（逐词 argv[0] 探针，真二进制 + 真 `GOPI_HOME`）。
- Result: 15 个「应已退役」的词全部 exit=0；4 个 phase-1 词 exit=1 并打印家族地址。
  全包 2214 PASS / 0 FAIL（改动前后一致）；`go vet ./cmd/agent/` 干净。
- Commit: `f5ecb6f9` on `task/01a10e03990b73f08684d6519f235194`

## Evidence (self-filed)

1. **逐词 argv[0] 探针**（真二进制 `$S/gopi-clean`，真 `GOPI_HOME=$(mktemp -d)`）：
   ```console
   $ for w in checkpoint archive brew memsync proxy code-search screen_capture \
              vision_analyze voice_input text2img scratch-gc annotate capture shot ledger-window; do
       $B $w -h >/dev/null 2>&1; printf '%-16s exit=%s\n' "$w" "$?"; done
   checkpoint exit=0   archive exit=0   brew exit=0   memsync exit=0
   proxy exit=0   code-search exit=0   screen_capture exit=0   vision_analyze exit=0
   voice_input exit=0   text2img exit=0   scratch-gc exit=0   annotate exit=0
   capture exit=0   shot exit=0   ledger-window exit=0
   ```
   同样是这四个 phase-1 词的反面：
   ```console
   $ $B log -h    → exit=1  gopi: "log" is not a gopi command — this one is spelled `gopi session log` …
   $ $B resume -h → exit=1  … spelled `gopi session resume` …
   ```
2. **改动后的全包真跑**：`cd cmd/agent && $S/cmdagent-after.test -test.timeout 30m -test.v`
   → `EXIT=0`（日志 `agent-after-v.log`；PASS/FAIL 计数以该文件为准）。
3. **`RunGopi` 全量人工核**：`grep -rn 'RunGopi(t, \[\]string{' --include='*_test.go' cmd/agent/`
   全量输出逐条读过 —— 出现的 argv[0] 全是 `CliVerbSession` / `task` / `worker` / `front`
   等**活**地址，无一是裸退役拼写。
4. **A 类验收 grep**（clean 树，`git status --porcelain` 空）：
   ```console
   $ grep -rEn 'exec\.Command(Context)?\([^,]+, *"(log|stats|raw|resume)"' --include='*_test.go' cmd/agent/
     NO MATCH
   $ grep -rEn '\[\]string\{\s*"(log|stats|raw|resume)"\s*[,}]' --include='*_test.go' cmd/agent/
     cmd/agent/cli_vision_test.go:93   ← 负向断言，非真二进制驱动；本 run 已改为 CliVerbLog
   ```
5. **Registry/ancestry 判定**：
   ```console
   $ git merge-base --is-ancestor d3492e0a HEAD; echo $?   → 1
   $ git for-each-ref --contains d3492e0a --format='%(refname)'
       refs/heads/task/01a10dc306ad78b7b65313efd18879bc
   $ git merge-base --is-ancestor 324577e8 HEAD; echo $?   → 0   (phase-1 在 master)
   ```

## 给后续切片（`01a10e23-d2ef` / `-d44b`）的可复用结论

- **哪些词退役、在哪一次提交退役**：phase-1 的 `log`/`stats`/`raw`/`resume` 在 master
  （`324577e8` 起）；其余 15 个词 + `archive`/`ui`/`cli` 的退役在 `d3492e0a`，**只存在于
  `task/01a10dc306ad78b7b65313efd18879bc`**。
- **顺序不可颠倒**：`d2ef`（解冲突、把完整退役合进 master）**必须先于** `d44b`（cmd/agent
  的 A/B/C 改写）。否则 A 类的对象还不存在，改写出来的测试会驱动一个仍在派发的词。
- **移植测试面时不要用 `git checkout <sha> -- 'cmd/agent/*_test.go'` 一把梭**：它会带回
  依赖目标树**不存在**的生产符号的文件（本 run 实测：`go vet` 报 `undefined:
  verbRetireAddress` / `retiredArgv` / `runRetiredWordProbe` / `verbRetireWords`，
  且 **86 个文件里只有 4–6 个**报错，其余照常编译 —— 表面成功）。正确做法：移植后跑
  vet，逐个回退报错文件，**并先验证生产半的 ancestry**。
