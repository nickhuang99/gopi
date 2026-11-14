<!-- doc-class: historical -->
# Task Report: minclient 的 Rule 22 pin 缺口 —— 门禁点名形态不可行，改用 testdata 只读 fixture

**task**: `01a132f2d929702eafdf26606fea261e` · **baseline**: `0d2d8a7f2` (= `master` tip at
this run's start; `git merge-base --is-ancestor HEAD master` → true, `HEAD == master`)

## Completed

- **复测基线（如实）**：两条门禁在 pristine master 上**本就恒红**，与任何在飞分支无关 ——
  本 worktree 的 `HEAD` 就是 `master` tip `0d2d8a7f2`，`git diff master...HEAD` 为空，
  红是**测出来的**，不是推断的。
- **反证门禁原文给的 FIX 形态**：`scripts_gate_test.go:313` 的 FAIL 正文让执行者
  `add a TestMain calling testutil.RunSuiteMain(m, "minclient")`。**实测不可行**，
  两个独立理由（下有原始输出）：
  1. `doubao-minclient` 是 `devdoc/doubao-asr-eval/minclient/` 下的**独立 module**，
     `github.com/nick/gopi/internal/...` 对它根本不解析（`no required module provides package`）；
  2. 即使在 `go.mod` 里补上 `require` + `replace`，Go 的 `internal` 规则也**按导入路径**拒绝：
     `use of internal package github.com/nick/gopi/internal/testutil not allowed`
     （调用方路径是 `doubao-minclient`，不是 `github.com/nick/gopi/...` 前缀）。
  并且这条边会把该 module 从 `go list -m all` = 1 个模块的**零依赖**，拉成整棵 gopi 依赖树，
  同时凭空生成 `go.sum`（实测 1565B）—— 与该 module 自己 `go.mod` 里声明的
  "Zero-dependency module … depends on nothing outside the Go standard library"
  **和它承载的评估结论直接冲突**。
- **最小修法**：**把这个包的 scratch 使用整个消除**。两处 `t.TempDir()` 只用来**承载一个被读取的文件**
  （`resolveCredentials` 的凭证文件 / API key 文件），从来不写回、不累积状态 ⇒
  改成仓库内**只读** `testdata/` fixture，写操作随之消失。
  - `devdoc/doubao-asr-eval/minclient/auth_test.go`：`t.TempDir()` + `os.WriteFile` → `filepath.Join("testdata", …)`；`os` 导入随之删除。
  - `devdoc/doubao-asr-eval/minclient/testdata/v2_voice.env`（新增，183B）、`testdata/apikey.txt`（新增，37B）。
  - **未动**任何门禁谓词 / baseline / exemption；**未新增**第二个 `TestMain`（该包仍恰好 0 个）；
    **未改** `go.mod`（零依赖守住）。
- **附带收益**：凭证字节从"内联在测试函数体里运行期写出"变成"仓库里的文件"，**进了 review**。

## 为何"消除 scratch"比"钉 pin"更正确（不是绕门禁）

门禁的谓词是 `usesScratchTemp(src)` —— "这个包**创建** scratch"（`t.TempDir()` /
`os.MkdirTemp(` / `os.TempDir()` / `/tmp` 字面量）。修完这两处，谓词**返回 false 且这是真的**：
该包确实不再创建 scratch。门禁的意图（Rule 22：临时工作区不得落在 `/` 分区）**被满足得更彻底**——
不是"钉住了临时根"，而是"没有临时根可钉"。谓词没有被削弱，只是它匹配的事实消失了。

反面修法（**已否决**）：给嵌套 module 加 `require`/`replace` 去够 `internal/testutil`。
它违反 Rule 35（零依赖是一种执着理念），破坏该 module 自身的评估结论，还要维护一条
`../../..` 相对路径。为修一条门禁而制造一条依赖不划算。

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → **exit 0**
- Tests:
  - `go test ./internal/testutil/ -run 'TestEveryTempUsingSuiteIsPinned|TestPinCoverageTable' -count=1`
    → **ok github.com/nick/gopi/internal/testutil 4.793s**（修前 FAIL）
  - `go test -count=1 ./...`（module `doubao-minclient` 自身）→ **ok doubao-minclient 0.003s**
  - `go vet .`（module `doubao-minclient`）→ clean；`gofmt -l .` → 无输出
- Ran: 见下 `## Evidence (self-filed)`，每条都是本轮**原文复制**的命令与输出。
- Result: 门禁 A/B 由 FAIL → ok；覆盖表该行由 `yes yes - NO-PIN` 翻成 `- - - no-temp-use`，
  `scratch-using` 54 → 53，`pinned: 53`（= scratch-using，不变量重新成立）；
  module `doubao-minclient` 自身 `ok`。

### 反向控制（证明绿来自这一改动，不是门禁被放宽）

把两处改动整个撤回（`git checkout -- auth_test.go` + 移走 `testdata/` = **逐字回到修前状态**，
`git status --porcelain` 干净），**同一条命令** → **逐字复现**原红：
同 `pin_coverage_test.go:92`、同 `pin_coverage_test.go:95`（`yes yes - NO-PIN`）、
同 `scripts_gate_test.go:313`、同 `scratch-using: 54` / `pinned: 53`、`exit=1`。
放回 → **ok / exit=0**（`05-green-after-restore.txt`）。控制前后只差这一个改动 ⇒ 归因成立。

恢复验证按 Rule 5 的判据做**显式 revision 比较**（不是 index 相对 diff）：
`diff <(git show HEAD:devdoc/doubao-asr-eval/minclient/auth_test.go) <(工作区文件)` 显示修复在位。

### Rule 22 按**设备号**判（不是路径拼写）

```
device=2051 path=/          device=2051 path=/tmp      # 同分区 —— /tmp 正是被禁的拼写
device=28   path=/dev/shm
device=2052 path=/home/user/workspace/gopi-test-scratch
```

本修法是**不再创建**临时文件，所以严格说没有"临时根"需要判分区。记录该测量是为了说明：
若走"钉 pin"路线，pin 解析到的是 `gopi-test-scratch`（2052，另一个设备）或 `/dev/shm`（28）——
两者都**凭设备号**离开 `/`（2051）；而 `t.TempDir()` 未钉时落在 `/`（2051）。这正是门禁要防的差。

## Evidence (self-filed)

证据目录 `~/.gopi/sessions/01a132f2-d929-702e-afdf-26606fea261e/artifacts/`：

| 文件 | 内容 | 我看到的 |
|---|---|---|
| `01-red-baseline.txt` | 修前两门禁原文 | `pin_coverage_test.go:92/95 … yes yes - NO-PIN`；`scripts_gate_test.go:313 … minclient creates scratch directories …`；`packages: 76 scanned \| scratch-using: 54 \| pinned: 53`；`exit=1` |
| `02-green.txt` | 修后两门禁 | `pin_coverage_test.go:95: … minclient  -  -  -  no-temp-use`；`packages: 76 scanned \| scratch-using: 53 … \| pinned: 53`；`--- PASS: TestEveryTempUsingSuiteIsPinned`；`ok … 5.164s`；`exit=0` |
| `03-reverse-control-red.txt` | 撤回改动后重跑 | 与 `01` **逐字同行号**；`scratch-using: 54` / `pinned: 53`；`exit=1` |
| `05-green-after-restore.txt` | 放回后重跑 | `ok github.com/nick/gopi/internal/testutil 4.793s`；`exit=0` |
| `04-rule22-device.txt` | `stat -c %d` | `/`=2051、`/tmp`=2051、`/dev/shm`=28、scratch home=2052 |
| `06-nested-module-import-refused.txt` | 门禁点名形态的可行性实测 | `no required module provides package github.com/nick/gopi/internal/testutil`（vet/test 均 FAIL）；补 `require`+`replace` 后 `use of internal package … not allowed`；`go.sum` 从不存在 → 1565B；`go list -m all` 从 1 个模块 → bubbletea/lipgloss/… 整棵树 |
| `run-nested-import-experiment.sh` | 上面那次的脚本（自动还原 `go.mod`、删除探针文件） | `script exit=0`，事后 `git status --porcelain` 只剩本次交付的两项 |

## 复测基线（精确判据，不是推断）

- `master` ref 在本 run 期间**移动过**：`0d2d8a7f2`（我的 base）→ `b99fc60f2`（本轮末测到的 tip）。
  所以"master 上红不红"必须按**我的 base** 判，不能按移动中的 ref 判。
- **两条门禁的目标红（`minclient`）**：在 base 上，本 worktree 的 HEAD **就是** base 本身
  （`HEAD == 0d2d8a7f2`，`git diff <base>...HEAD` 为空），所以 `01-red-baseline.txt` 里的红
  就是 base 的红。**恒红确认。**
- **顺带测到的另外 3 条红，也是在 base 上（不是新 master 引入的）**：
  判据是 `git diff <base>..HEAD -- <那些文件>` 为空 ⇒ **我的 commit 没碰过它们，worktree 里这两个
  文件的字节就是 base 的字节** ⇒ 门禁读到的红就是 base 的红。**逐行直接核验过**：
  `git show 0d2d8a7f2:cmd/agent/main.go | sed -n '805p'` → **空行** —— 这正是
  `CONFIG.md:174` 那条 anchor 落空行的原因。
- **如实补充**：新 master `b99fc60f2`（worker-control selector 的 merge）**顺带改掉了
  `cmd/agent/main.go`**，同一位置现在已是内容（`// attached natively (vision model sees the
  pixels directly).`）—— 那条红**已被新 master 覆盖掉，很可能不再红**。本 run 未去复测新 master
  （我的分支只需要对 base 负责），只如实记录这次位移。

## Not Done

- **base 上另有 3 条与本任务无关的既有红，本 run 未触碰**（判据如上：`git diff <base>..HEAD` 为空 +
  逐行核验）：
  - `TestConfigDocRefClassCensus`（`internal/standards`）：`CONFIG.md:174 → cmd/agent/main.go:805` 落空行；
  - `doccheck` 的 anchor **NUMBER drift**（2 行）：`devdoc/doubaostream-session-test-coverage.md` 的
    `cmd/agent/chat_voice_live.go:249`（文本实际在 `:254`）、`devdoc/merge-resolve-peak-hour-guard.md`
    的 `cmd/agent/main.go:982`（文本实际在 `:80`）。
  已按 Rule 41 单独提行，不在本分支修。
- 没有为该修法新写 Go 测试：这是**删掉 scratch 使用**，不是修 bug 行为；
  `auth_test.go` 原有 6 个测试**本身就是回归守卫**（它们仍走真实文件读取路径），
  且改动后仍全绿。新增测试只会重复它们。
- `scripts_gate_test.go:313` 的 FAIL 正文**仍然**写着
  `FIX: add a TestMain calling testutil.RunSuiteMain(m, "minclient")` —— 对嵌套 module
  这是一个**办不到的**指引。**本任务没有改它**（改门禁文案属于门禁面，且"不要动门禁"
  是 briefing 的硬约束）。**这是一个真实的、原样保留的缺陷**：下一个遇到同类包的执行者
  会被同一句话误导。建议**单独派一条**修该文案（提示嵌套 module 的第二种修法：
  消除 scratch 使用，或让该 module 自带一个不依赖 `internal/` 的等价入口）。

## Rule 6d 交付声明

- **分支**：`task/01a132f2d929702eafdf26606fea261e`（**未改名** —— 分支名即账本地址）
- **worktree**：`/home/user/workspace/gopi-01a132f2d929702eafdf26606fea261e`（briefing 已给的，未另建）
- **取回**：`git -C /home/user/work/GoPi show task/01a132f2d929702eafdf26606fea261e:devdoc/doubao-asr-eval/minclient/auth_test.go`
  （linked worktree 共享对象库/refs，**无需 push**；本 run **未自行 merge**）
- **报告**：本文件 + `$GOPI_TASK_REPORT_PATH`（同一份内容）
