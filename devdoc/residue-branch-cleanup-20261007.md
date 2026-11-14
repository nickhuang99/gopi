<!-- doc-class: historical -->
# Task Report: 两条残留 task/* 分支的清理 —— 一条归档，一条证伪（它被活着使用）

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a112e3-9e65-7543-8a63-54eb7f247112`
Worktree: `/home/user/workspace/gopi-01a112e39e6575438a6354eb7f247112` (branch `task/01a112e39e6575438a6354eb7f247112`)

## Completed

### 0. briefings 的两个前提被实测推翻（这是本次最重要的产出）

briefing 说分支 A「0 个独有提交、worktree 无未提交工作（`git status` 的 223 条是拿 master 当基准
产生的假象）」。**前半句对，后半句错**：

| 主张 | 实测 | 判据 |
|---|---|---|
| A 的 ref 是 master 祖先 | **真** | `git merge-base --is-ancestor <A tip> master` → 0；`git rev-list --count master..A` = 0 |
| A 的 worktree 无未提交工作 | **假** | 该 worktree `git status --porcelain` = **35 个 tracked 改动**（`git diff` 182 KB），untracked = 0 |
| A 的 worktree 里没有活进程 | **假** | pid 2779817 站在里面，是 `gopi session resume`，正在跑 `go test ./cmd/agent/` 与 `./internal/standards/` |

「223 条是假象」这个结论本身对了一半：拿 master 当基准去数**未提交文件**确实会得到一串无关数字，
但那不能推出 worktree 干净 —— 干净的判据是 `git status --porcelain` 本身（它拿 **HEAD** 当基准），
而不是任何拿 master 当基准的 diff。两者被混为一谈了。

### 1. 分支 B 的 22 文件逐文件核对：**两边互为残缺，而非重复**

原判断是「B 与 master 的 `ad0f32714` 是同一件清扫工作做了两遍」。核对结论是**更糟也更值得记**：
master 的 `ad0f32714` 扫得比 B 深（B 在它之前 fork，`git merge-base --is-ancestor ad0f32714 <B base>`
= 假），但 B 也补了 master **今天仍然漏着**的真调用位。

按「真调用位（command position 的 argv）vs 散文（注释/生成的文档字符串）」逐条分类，
判据 = 该行在运行时是否作为 argv 被执行。

| 文件 | 差异性质 | master 是否已覆盖 |
|---|---|---|
| `scripts/materials/gen-taskD.sh` | **真调用位 5 处**（B 修，master 未修） | ❌ 未覆盖 |
| `scripts/materials/materials-render-D.py` | **真调用位 1 处 + docstring 2 行**（B 修，master 未修） | ❌ 未覆盖 |
| `scripts/materials/gen-envB.sh` | **真调用位 3 处**（B 修，master 已用另一写法修好同一处） | ✅ 已覆盖 |
| `scripts/materials/gen-topA.sh`、`entries-topB.sh`、`gen-taskA.sh`、`gen-memoryC.sh`、`gen-topB.sh`、`gen-flagF.sh`、`gen-env-E1.sh`、`gen-interactive-real.sh`、`gen-taskD.sh`(散文部分)、`scripts/human-ui/*` | **散文**（注释、入口卡文本、commit 文案） | ✅ 已覆盖；且 master 的散文比 B **更准确** |
| `docs/archive/demo/materials/001-notify-cmd-default-off/06-gif-raw.txt` | **录制证据**：写死沙盒路径与 task id、时间戳 | ✅ 与拼写无关，B 只是重录了一次 |
| `compare-terminals.sh`、`web-only.sh`、`materials-gen-env-notify.sh`、`materials-gen-gif.sh`、`render-entry.py`、`render.py`、`chat_replay_xtest.sh`、`chat-ui-observe.sh` | 与 master **逐字节相同** | ✅ |

**两个方向都值得记的细节：**

- **master 漏掉的真调用位**（`master` tip `e036ea95b` 的当前行号）：
  - `gen-taskD.sh:437`、`:440` — `'$BIN' checkpoint export …` → 应 `session checkpoint export`
  - `gen-taskD.sh:489` — `'$BIN' checkpoint resume …` → 应 `session checkpoint resume`
  - `gen-taskD.sh:559`、`:564` — `'$BIN' sandbox run …` → 应 `tool sandbox run`
  - `materials-render-D.py:567` — `sh([bin_path, "capture", …])` → 应 `["tool","capture"]`（**两个 argv 元素**）
    外加 `:8/:9` docstring 的 `gopi capture` / `gopi annotate` → `gopi tool capture` / `gopi tool annotate`
- **B 把散文改错了**：B 在 `gen-taskD.sh:391`、`:419` 把 master 的 `gopi session checkpoint list`
  写成 `gopi checkpoint list`，`:447` 正文里的 `checkpoint export` 同理。**这些裸形式是已退役拼写**，
  实跑取证：
  ```
  $ bin/gopi checkpoint list
  gopi: `checkpoint` is RETIRED — use `gopi session checkpoint` …
  $ bin/gopi sandbox show
  gopi: `sandbox` is RETIRED — use `gopi tool sandbox` …
  ```
  所以「以 master 为准」在散文侧是对的 —— 这条分支**部分**把一个已修好的东西改坏了。

**真调用位的实跑判据（Rule 31）**：`kubectl`… 不适用；这里用的是真二进制
`/home/user/work/GoPi/bin/gopi` 的拒绝臂 —— 上面两行即其输出，退出码 0（拒绝走 stderr）。
6 处 argv 一旦执行就拿到「is RETIRED」，即演示脚本会**跑不通**，不是文档不准确。

**已另提 bug 行**：`01a112ec-ebbf-713f-a64f-2c43e53714e7`（tags: bug, verb-retirement），
payload 里附了 master 行号、归档分支里那 6 处的现成修法、以及「只取调用位、不整文件搬 B」的边界。

### 2. 两条 worktree 的活进程检查（Rule 34 / cgroup-cwd 判据）

判据一律是 `/proc/<pid>/cwd` readlink 落在该目录里（不是 `pgrep -f`，它会自匹配）。

**B 的 worktree** —— 目录已释放，检查结果是「有孤儿，已清理」：
- 4 个 `Xvfb`（pid 2786510 `:97`、2788667 `:98`、2811627 `:96`、2816432 `:80`），
  `cwd` 全指向 B 的 worktree，`PPID=1`（父 shell 早退），各 0 个 X client
  （`xlsclients` 空、socket 只 LISTEN 无连接），CPU 时间 0。
  它们是已取消行 `01a10efc` 那个 run 留下的孤儿。
- 处置：只对这 4 个 pid 发 SIGTERM（不是 `pkill Xvfb` —— 本机还有 X0/X1/X62/X63/X64/X73/X200/X240
  等其它套件可能正在用的显示），3 秒后 4 个全消失，`pgrep -ax Xvfb` 空。
- 复检：B 目录下 0 个活 cwd。

**A 的 worktree** —— **不动**，理由见下节。

### 3. B 的归档（Rule 9，精确拼写）+ 实跑证据

```bash
git tag archive/task/01a10efc760d79e2aaddcf5681406c37 task/01a10efc760d79e2aaddcf5681406c37
# -> bc921bef0997d87bc1f33ce46970daed944badf5  == 分支 tip（tag 与 tip 同 hash，已核）
git worktree remove /home/user/workspace/gopi-01a10efc760d79e2aaddcf5681406c37   # 不加 --force，成功
git branch -D task/01a10efc760d79e2aaddcf5681406c37
# -> Deleted branch task/01a10efc760d79e2aaddcf5681406c37 (was bc921bef0).
```

**归档生效的证明（记忆 #1962：date 后缀拼写等于没归档）—— safe-merge 从 worktree-occupied 变 archive-tag：**

```
归档前: ❌ safe-merge: 存在拦截项 —— 禁止 merge (拒绝种类: worktree-occupied)      exit=1
归档后: ❌ safe-merge: 存在拦截项 —— 禁止 merge (拒绝种类: archive-tag missing-ref) exit=1
        stderr: ❌ 拒绝: 分支 '…' 已有存档 tag 'archive/task/01a10efc…' —— 已归档(败者)，禁止 merge
```

两次都是 exit 1，**但拒绝的种类变了** —— 这正是「tag 拼对了」的机器可读证据（拼错时 safe-merge 会
在 [a] 打印「无 archive/tag —— 未存档」并可能放行）。
另：`git rev-parse --verify --quiet refs/heads/task/01a10efc…` → exit 1（分支已从 `refs/heads` 消失），
`git tag -l 'archive/task/01a10efc*'` 非空，`git worktree list` 不再列出该目录。

### 4. 分支 A：**不归档**，并派发后续行（Rule 26 / Rule 30）

A 的 worktree 里有 35 个 tracked 改动 **且有一个活着的 resume 进程正在里面跑测试**：

- pid 2779805 `gopi worker --resume --branch task/01a10e23d5d1778a82c519cfeab5feb6`（cwd = 主检出）
- pid 2779817 `gopi session resume 01a10e23-d5d1-778a-82c5-19cfeab5feb6 --task 01a10e23-…`（cwd = A 的 worktree）
- 快照时刻它正在跑 `go test -count=1 -timeout 2400s ./cmd/agent/` 与 `go test -count=1 ./internal/standards/`；
  会话 jsonl 持续追加。该行 status=failed、`reason: task failed`，tags 含 `run-retry:1`、`resumed:<session>` ——
  这是一次**重算**，不是交接。

三条「不能做」的理由（逐条落在规则上，不是谨慎）：
1. `git worktree remove` 会删掉那 35 个文件的改动 = 正在被使用的未提交工作（Rule 30：dirty 是工作，不是垃圾）。
2. 归档 tag 一旦落下，merger sweep 与 safe-merge 永久排除该分支；**若那次 resume 之后要提交，分支已在 merge 路径之外**
   （记忆 #1972 的同类事故）。
3. 活进程的 cwd 钉住 inode，`finish-cleanup` 的 liveness 判据本来就该拒绝释放（Rule 34 / `guestRowOwnCheckoutRefusal`）——
   这是设计，不是缺陷。

**证据留档**（写进本 run 的 session 目录，绝不落进任何 checkout —— Rule 34/37）：
`/home/user/.gopi/sessions/01a112e3-9e65-7543-8a63-54eb7f247112/groundA-snapshot/`
`status-porcelain.txt`、`worktree.diff`（182 KB 全量）、`diff-stat.txt`、`untracked.txt`。

**已派发**：`01a112f1-8067-7a43-a982-0c81b1d2c0ed`（tags: cleanup, worktree-residue），
要求「先判活死、再决定归档还是先救回改动」，两条路各自的实测判据与命令都写在 payload 里，
并明确禁止 `--force` 绕过前置检查。

### 5. `gopi worker --resume` 的 ground 推断陷阱：落地 **A 侧（点名推断出来的目录）**

**陷阱的机制（读代码 + 实测）**：`gopi worker --resume` 有两条通向「不在 ground 里跑」的路，
两条都不是这个面自己选的目录：

1. `internal/tools/task_resume_ground.go` 的 `--branch` 拼写解析不通过时，`taskResumeGroundNoRowRefGoneFmt`
   渲染成 `… and <repo> has no such branch (git has no such ref)`。但 `--branch` **不接受 repo 参数**，
   那个 `<repo>` 是从 ledger 行推出来的候选（`TaskGitProvenanceFor`）。句子读起来像**对 branch 的判决**，
   实际是**对某个推断目录的判决** —— 读者会去追错的对象。这个陷阱在本次任务里**当场发生**：
   A 的 worktree 里那条活进程用的是 `--branch <name>`，而 `--branch` 的正确拼写是 `task/01a10e23d5d1778a82c519cfeab5feb6`
   （完整 ref），**没有** `task/` 前缀的写法会解析失败 —— 于是操作者拿到一句「你的分支不存在」，
   而分支就在他眼前。
2. `cmd/agent/cli_resume.go` 的 `resumeChdirToWorktree`：ground 的 chdir 失败时 fail-soft 回落到
   **本进程的 cwd**，而这个 cwd 由**调用者**决定，不是 resume 选的。实测：本次那条活 worker 是从
   **主检出** `/home/user/work/GoPi` 启动的（`readlink /proc/2779805/cwd`），所以它一旦回落，落点就是共享树 ——
   在那里提交没有 owner、没有分支、没有回滚点（Rule 37）。

**论证：为什么选 A 而不是 B（「必须显式给出」）**

- **B（强制 `--worktree`/`--repo`，取消推断）面太大且会打断正在工作的路径**：`--branch` 是
  `devdoc/worker-resume-ground.md` 那条 epic 明确给出的三种拼写之一，操作者手里有分支名时它就是正确入口；
  把它改成必填目录，等于把「我知道分支名」的人赶去先查 `git worktree list`。
- **本任务自己的约束（Rule 6a/29）要求对别人的分支只读**，把一条他人 epic 的 CLI 语义翻过来
  （拒绝一条今天能用的拼写）属于系统层的语义改动，风险与收益不成比例。
- **A 直接命中根因**：陷阱不是「有推断」，而是「**推断出来的东西被当成事实渲染**」。点名它即可：
  操作者手里仍有 branch，同时知道该去看哪个目录。
- A 是**纯加法**：不改任何解析结果、不改任何拒绝/接受的决定，只改一处常量文本 + 一处 fail-soft 警告的措辞。
  回归测试能钉住它（去掉限定词即变红），而不会有「本来能跑的拼写现在跑不了」的副作用。

**落地内容**
- `internal/tools/task_resume_ground.go`：新增 `taskResumeGroundRepoInferredFmt`
  = `"%s (inferred — no --repo was given, so this is the repo the ledger named)"`，
  在 `taskResumeGroundNoRowRefusal` 的 `!p.RefExists` 分支用它包住 repo 轴。
- `cmd/agent/cli_resume.go`：新增 `resumeFallbackNotice` + `resumeFallbackNoticeFmt` + `cwdOrUnknown` +
  `resumeCwdUnknownMarker`；失败回落的警告现在点名**它将实际运行的目录**（`CALLER's cwd <path>`），
  而不是含糊的 "current dir"。空 cwd 渲染显式未知标记（Rule 15，不留空白）。
- 测试：`internal/tools/task_resume_ground_test.go::TestResumeGroundNoRowNamesAMissingRef` 增加
  「必须含 inferred」断言；`cmd/agent/cli_resume_worktree_test.go::TestResumeFallbackNoticeNamesTheCallersCwd`
  新增（钉子目录 + CALLER 字样）。

## Not Done (honest)

- **分支 A 未归档、未释放。** 它不是「本轮没做完」，而是「按规则本轮**不能**做」：worktree 里有活进程 +
  35 个未提交 tracked 文件。已派发 `01a112f1-8067-7a43-a982-0c81b1d2c0ed` 承接，交付条件写在 payload 里。
  **所以验收条件「两条 linked worktree 已释放」本轮只达成了 1/2。**
- **master 上 6 个真调用位未修。** 属别人（verb-retirement）的范围，按 Rule 26 派发
  `01a112ec-ebbf-713f-a64f-2c43e53714e7`，本轮不代改 —— 本任务的约束是「只在自己的 worktree 内改与本任务相关的代码」，
  而修 scripts 的退役拼写是另一条 epic 的活。
- **`gopi worker --resume` 的 B 侧（强制显式 ground）未做**，理由见上（论证已给出）。

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → exit 0；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BIN BUILT`
- Vet: `go vet ./internal/tools/ ./cmd/agent/` → exit 0
- Tests:
  - `go test ./internal/tools/ -count=1 -timeout 600s -run 'TestResumeGround'` → `ok github.com/nick/gopi/internal/tools 2.742s`
  - `go test ./cmd/agent/ -count=1 -timeout 900s -run 'TestResumeChdir|TestResumeFallback|TestResumeGround'` → `ok github.com/nick/gopi/cmd/agent 1.039s`
  - 两包各自 `-run` 过滤后的目标子集；未跑整包（SMOKE、branch-scoped，Rule 27）。
- Ran（归档，verbatim）:
  ```
  git tag archive/task/01a10efc760d79e2aaddcf5681406c37 task/01a10efc760d79e2aaddcf5681406c37
  git worktree remove /home/user/workspace/gopi-01a10efc760d79e2aaddcf5681406c37
  git branch -D task/01a10efc760d79e2aaddcf5681406c37
  bash scripts/safe-merge.sh task/01a10efc760d79e2aaddcf5681406c37
  ```
- Result: tag → `bc921bef0…`（= 分支 tip）；worktree 删除成功（**无 `--force`**）；
  `Deleted branch task/01a10efc760d79e2aaddcf5681406c37 (was bc921bef0).`；
  safe-merge 的拒绝种类由 `worktree-occupied` → `archive-tag missing-ref`（见 §3 转录）；
  `git worktree list` 已不含该目录；`refs/heads/task/01a10efc…` rev-parse exit 1。
- Ran（活进程判据，verbatim）:
  ```
  readlink /proc/2779817/cwd   # -> /home/user/workspace/gopi-01a10e23d5d1778a82c519cfeab5feb6
  readlink /proc/2779805/cwd   # -> /home/user/work/GoPi
  ps -o pid,ppid,etime,stat -p 2779805,2779817
  DISPLAY=:80 timeout 5 xlsclients   # 空
  ss -xlp | grep X11-unix            # X80/X96/X97/X98 全 LISTEN 无连接
  ```
- Result: A = 活的（`go test ./cmd/agent/` 在跑）；B 的 4 个 Xvfb 是孤儿，
  SIGTERM 后 `pgrep -ax Xvfb` 空。
- Ran（退役拼写拒绝臂，verbatim）: `bin/gopi checkpoint list` / `bin/gopi sandbox show`
  → `gopi: \`checkpoint\` is RETIRED — use \`gopi session checkpoint\`` / `gopi: \`sandbox\` is RETIRED — use \`gopi tool sandbox\``
- Commit: （见文末交付声明）

## Evidence (self-filed)

- `git merge-base --is-ancestor task/01a10e23d5d1778a82c519cfeab5feb6 master` → exit 0（A 的 ref 是 master 祖先）
- `git -C <A worktree> status --porcelain | wc -l` → **35**（briefing 的「无未提交工作」被推翻）
- `git -C <A worktree> diff | wc -c` → 182262
- `bash scripts/safe-merge.sh <B>` 归档前 → `拒绝种类: worktree-occupied`；归档后 → `拒绝种类: archive-tag missing-ref`
- `git rev-parse --verify --quiet refs/heads/task/01a10efc760d79e2aaddcf5681406c37` → exit 1
- `git rev-parse archive/task/01a10efc760d79e2aaddcf5681406c37` → `bc921bef0997d87bc1f33ce46970daed944badf5`
- 本 run worktree 收尾 `git status --porcelain` 空（Rule 30）—— 见文末
