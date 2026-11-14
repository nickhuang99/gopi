<!-- doc-class: historical -->
# GOPI.md 不再被 git 跟踪 — 它是客户端自己的配置文件

**日期**：2026-10-04 · **来源**：operator 的决定（"GOPI.md 是客户端自己的配置文件，
gopi 照旧读取，但绝不靠 clone 跨机传播"）。

## 决定

`GOPI.md` 从索引里移除，并在 `.gitignore` 里**保留为被忽略的根文档**（`/*.md` 本来
就忽略它），**不再有 `!/GOPI.md` 白名单**。

两半是同一个决定的正面和反面，缺一半就变成了另一个缺陷：

| 半边 | 事实 | 由谁钉住 |
|---|---|---|
| 不再被跟踪 | `git ls-files GOPI.md` 无输出；`git check-ignore -v GOPI.md` → `/*.md` | `TestRepoRootGopiDocIsUntrackedButReadable`（index 腿） |
| 仍被读取 | 文件在磁盘上，`gopi --gopi-prompt 1 version` 打印 `loaded GOPI.md (92283 bytes)` | 同上（磁盘腿：存在性 + 字节下限 + 首段锚句） |

**为什么去掉白名单而不是只 `git rm --cached`**：只从索引删掉、保留白名单，文件会在
下一次 `git add .`（或任何一个"把根目录的东西加进来"的路径）被重新跟踪 —— 白名单
正是那个把一台机器的本地策略传播到每台 checkout 的机制。反向也一样：白名单还在而
文件未被跟踪时，它会以 `??` 出现在 `git status` 里，逼每个 run 记住要不要处理它。
没有白名单，两种状态都不再出现。

## 改了什么（同一提交）

1. `git rm --cached GOPI.md`（文件留在磁盘，内容未变：sha256 与基线一致）
2. `.gitignore`：删 `!/GOPI.md`，改写解释段（说明为什么故意没有这条 negation）
3. `internal/standards/scratch_gate.go`：`rootDocAllowlist` 去掉 `GOPI.md`；
   allowlist 注释、`junkIgnoreAllowed` 注释、`ignoredPaths` 的 negation 示例同步
4. `internal/standards/scratch_gate_test.go`：red-proof 的 clean 列表去掉 `GOPI.md`；
   新增 `TestRepoRootGopiDocIsUntrackedButReadable`
5. `CONFIG.md` §4 的 GOPI.md 条目：从"根目录允许的两份 markdown 之一"改成
   "不被 git 跟踪、由各机 operator 放置、读取行为不变"
6. `internal/tools/chat_write_guard_test.go`：原来把 `repo + "/GOPI.md"` 当作
   "tracked 因而必须拒绝"的样例 —— 那断言的是**旧事实**。改为用 throwaway repo
   断言**判据本身**（按 index 判定，不按文件名），见下
7. `internal/envctx/envctx.go`：注释里 "GOPI.md is git-tracked" 的事实更正
8. `reports/README.md`：Enforcement 段同步（说明 allowlist 只剩三份、没有 `!/GOPI.md`）

## 为什么 chat write guard 的测试要重写（这条最容易踩）

`ChatWritePathRefusal` 用 `git ls-files --error-unmatch` 判定是否为被跟踪文件 ——
**判据是索引，不是文件名**。旧测试把 `/home/user/work/GoPi/GOPI.md` 放进"必须被
拒绝"的表里，靠的是"那个文件此刻被跟踪"。取消跟踪后这条断言变成假阴性：它不再是
跟踪文件，所以 guard 放行是**正确**行为（chat 可以写本 checkout 自己的规则文件），
而测试仍会红（或者更糟：为了让测试绿而去把文件名硬编码进 guard，那才是真正的缺陷）。

新测试 `TestChatWritePathRefusalKeysOnTheIndexNotAFilename` 在一个临时 repo 里造
两个根文档，一个 `git add`、一个不 add，断言"被跟踪的拒、未被跟踪的放行"。
它不再依赖本 checkout 当时的索引状态，因此跟踪决定再变一次也不会让它说谎。

## 没有动的

- **Rule 正文与编号引用**：`Makefile` / `scripts/*.sh` / `internal/standards/*.go` /
  注释里大量的 `(GOPI.md Rule N)` 是**规则编号引用**，不是"它被跟踪"的断言，全部不动。
- 各门禁**从磁盘读** GOPI.md 的那些（`branchForceInstructionRel`、
  `archiveMintInstructionRel`、`redproofRestoreNamedFiles`、`internal/agent` 的
  project-context walk）：它们走的是 filesystem，不是 index，所以取消跟踪对它们
  零影响 —— 这也是"读"这半边不需要任何代码改动的原因。
- `internal/standards/repo_hygiene_gate.go` 的 tracked-blob census：`GOPI.md` 是
  文本，本来就不在基线表里。

## 各 checkout 的收尾（本 run 不做）

本 run 只在自己的 worktree 落地。hosta / peerbox 各自的 checkout 需要各自执行一次
`git rm --cached GOPI.md`（或者直接 `git checkout .gitignore` 合并后让 `.gitignore`
把它变成未跟踪的 `??`，再 `rm --cached`）——**合并后它们的工作树里那份文件不会自动
消失**，这正是设计意图：文件是各机自己的。

## 检查

```bash
go test ./internal/standards/ -run 'RepoRoot|RootDoc|ConfigDoc|CommandsDoc'
go test ./internal/tools/   -run 'ChatWrite|ChatPath'
git ls-files GOPI.md          # 空
sha256sum GOPI.md             # 7465127d5255c4403f033dbbd3ad59536b9dc1d5b2a2cc58e8f1ac2b03da2145
gopi --gopi-prompt 1 version 2>&1 | grep 'loaded GOPI.md'
```
