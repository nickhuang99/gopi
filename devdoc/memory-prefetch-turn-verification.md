<!-- doc-class: normative -->
# 轮边界记忆检索 — 独立复核报告（本会话）

**任务**（上一次运行的续行）：`01a10edf-5df5-7f3e-af0d-b771d0b99c6f`
**分支**：`task/01a10edf5df57f3eaf0db771d0b99c6f`，尖端 `84a4816b`
**本会话新增**：本报告（唯一新交付物；代码在上一会话已提交，本会话未改一行源码）

## 交付声明

- **分支**：`task/01a10edf5df57f3eaf0db771d0b99c6f`
- **尖端**：`3f48a0fd`（`ee234dd7` 本复核报告 + DOC-INDEX 行，其上再叠一次交付点同步
  `merge master`）。**merge 到 master 未发生**，且按 Rule 6e 不由本会话执行。
- **取回命令**：
  `git show task/01a10edf5df57f3eaf0db771d0b99c6f:devdoc/memory-prefetch-turn-verification.md`
  以及上一会话的 `…:devdoc/memory-prefetch-turn-boundary.md`。
- **worktree**：`/home/user/workspace/gopi-01a10edf5df57f3eaf0db771d0b99c6f` ——
  本会话结束时 `git status --porcelain` 为空（Rule 30，本会话实测）。
- **commit 在主仓可见**：worktree 与主仓 `/home/user/work/GoPi` 共享 object store 与
  refs，无需 push；本会话从主仓直接 `git show` 读到了分支上的报告。

## Completed

- **交付面被独立测量，而不是照抄上一次运行的散文。** 上一会话的会话笔记里写着
  "已合入 master"，本会话对它做了两次核对，**结论是那句不成立**：

  ```
  git merge-base --is-ancestor 84a4816b master   → 非零（84a4816b 不在 master 里）
  git log --oneline master ^84a4816b | wc -l     → 0（master 没超出我的基点，是我没被合入）
  git log --oneline 84a4816b ^master | wc -l     → 4（我的四个提交仍悬在分支上）
  git cat-file -e master:devdoc/memory-prefetch-turn-boundary.md → fatal: Not a valid object name
  git grep -l TurnInput master -- internal/memoryprefetch         → 空（代码也不在 master）
  ```
  读法：**交付物是一次都没被 merge 的分支交付**，不是 master 上的既成事实。
  据 Rule 6e，merge 由 PA 在验收后处理，我不自行 merge —— 但"已合入"这句话没有实测支撑，
  不能作为本任务闭合的凭据，故在此如实更正。

- **交付物确实落在主仓可见的 ref 上（Rule 6d 的实质要求成立）。** 分支尖端可从主仓
  `/home/user/work/GoPi` 直接读到：`git show task/01a10edf…:devdoc/memory-prefetch-turn-boundary.md`
  取回 13331 字节的报告；worktree 与主仓共享 object store 与 refs，不需要 push。

- **收尾状态（Rule 30）被测量为空**，本会话前后各测一次：worktree
  `/home/user/workspace/gopi-01a10edf5df57f3eaf0db771d0b99c6f` 里
  `git status --porcelain` 与 `--untracked-files=all` 都**不打印任何行**；本报告落盘后再测一次。

- **Rule 36 的分类行在位**：`devdoc/DOC-INDEX.tsv` 第 955 行是
  `devdoc/memory-prefetch-turn-boundary.md  historical  historical (3 standing / 2 record): …`，
  与文件真实存在一致（上一会话用 `doccheck --fix` 补的）。

- **改动面的测试在本会话重跑，绿**（不是复述上一会话的数字，见 `## Verification`）。
  `make test-budget` 那条流在上一会话跑到会话结束仍未结束 —— 本会话改用**分支范围内、
  按 `-run` 过滤的 smoke**（Rule 27 的 merge/verify 路径做法），它跑完了。

## Files

- `devdoc/memory-prefetch-turn-verification.md`（本文件，本次唯一新增）
- `devdoc/DOC-INDEX.tsv`（本文件的分类行，Rule 36，同一个提交）
- 只读引用（未修改）：`devdoc/memory-prefetch-turn-boundary.md`、
  `cmd/agent/chat_prefetch_turn.go`、`internal/memoryprefetch/turn.go`、
  `internal/lineedit/pasteref.go`

## Verification

- **Build:** `CGO_ENABLED=0 go build ./...`（worktree 内）→ **exit 0，`BUILD OK`**
  （本会话观察到）。
- **Tests（分支范围 smoke）:**
  - `go test ./internal/memoryprefetch/ -run 'Turn|Defaults' -count=1` →
    `ok github.com/nick/gopi/internal/memoryprefetch 0.027s`
  - `go test ./internal/lineedit/ -run 'PasteRef|StripPaste' -count=1` →
    `ok github.com/nick/gopi/internal/lineedit 1.486s`
  - `go test ./cmd/agent/ -run 'Prefetch|TurnAdoption|TurnInput|TurnPlan|TurnSeal' -count=1` →
    `ok github.com/nick/gopi/cmd/agent 6.084s`，**EXIT=0**
  - 交付点同步（`3f48a0fd`）**之后**重跑：
    `go test ./cmd/agent/ -run 'TurnAdoption|TurnInput|TurnPlan|TurnSeal' -count=1` →
    `ok github.com/nick/gopi/cmd/agent 2.526s`（后台 job-6，报 exited code=0）；
    同一条命令在同步前（含 `Prefetch` 项）也报 `ok … 6.084s`。
  - `make test-budget PKGS='./internal/memoryprefetch ./internal/lineedit ./cmd/agent'`
    本会话**未跑完**：`./cmd/agent` 整包耗时远超本次会话预算，被本会话主动结束
    （`job kill`）。**如实记为未完成，不当成绿**。
- **Ran:** 本会话**没有新增可运行的界面** —— 我一行源码都没改，交付物是复核报告本身，
  所以没有"我改了但没跑"的表面。上一会话的 Rule 31 证据（真 pty + 真沙箱 + 真二进制，
  notify 面 stdin 零输入）留在
  `/home/user/.gopi/sessions/01a10edf-5df5-7f3e-af0d-b771d0b99c6f/artifacts/`，
  其记录 JSON 是上一会话亲手 `cat` 出来的。本会话**没有重跑那次 pty 驱动器**。
- **Result:** 我对交付物能说的话是：分支尖端在主仓可见、四个提交齐全、worktree 干净、
  改动面的三个包在本会话按 `-run` 过滤跑绿。**我不能说"它已合入 master"——实测为否。**
- **Commit:** `ee234dd7`（本复核报告 + DOC-INDEX 行）；其后 `3f48a0fd` 为交付点同步
  （把 master 合入本分支）。校验：
  `CGO_ENABLED=0 go build ./...` → `BUILD OK`（同步后重跑）；
  `git merge-base --is-ancestor master HEAD` → 0（同步已落地）。

## Not Done

- **整包 `./cmd/agent` 的预算门本会话没跑完**（会话预算内不可能跑完；不是回归，是规模）。
  上一会话已做基线对照（`61acd58a` detached worktree，同一批用例 25 个具名失败，
  与本分支流的失败集合差集为空）—— 那条对照的证据在上一会话的 artifacts 与报告里，
  **本会话没有重跑它**，因此本报告不把它当作本会话的观察。
- **本会话没有重跑真 pty 端到端**（notify / paste 两面）：上一会话的证据在案，重跑需要
  重新建沙箱 + 挂 pty，且不会改变本会话要回答的问题（"交付是否落地"）。
- **`/queue` 那一半仍未真跑覆盖**（上一会话已具名，属父行 01a10e22 的统一输入通路）。
- **merge 未发生。** 我没有 merge 自己的分支（Rule 6e）。若 PA 认为该分支应落地，
  需要走 merge 路径；`git merge-tree` 预演可用（本会话跑通）。

## Evidence (self-filed)

| 声明 | 命令 | 我看到的 |
|---|---|---|
| 分支尖端不在 master | `git merge-base --is-ancestor 84a4816b master` | 非零退出（NO） |
| master 未超出我的基点 | `git log --oneline master ^84a4816b \| wc -l` | `0` |
| 我的四个提交仍悬空 | `git log --oneline 84a4816b ^master \| wc -l` | `4` |
| 报告不在 master | `git cat-file -e master:devdoc/memory-prefetch-turn-boundary.md` | `fatal: Not a valid object name` |
| 代码不在 master | `git grep -l TurnInput master -- internal/memoryprefetch` | 空输出 |
| worktree 干净 | `git status --porcelain`（worktree 内） | 空 |
| 分类行在位 | `grep -n memory-prefetch-turn-boundary devdoc/DOC-INDEX.tsv` | `955:devdoc/…md  historical  …` |
| 构建 | `CGO_ENABLED=0 go build ./...` | `BUILD OK`（exit 0） |
| 改动面 three packages green | 见 `## Verification` 的三条 `go test` | 三行 `ok …`，cmd/agent 那条 EXIT=0 |
