<!-- doc-class: historical -->
# Task Report: m47-e2e-hp 残留分支核查（epic「nick 三问」c 项闭环）

日期: 2026-08-31 · 执行: gopi 会话 · 分支 `devdoc/m47-e2e-hp-check`（基于 master 5c6f5ca）

## 背景
Epic 诊断曾列出残留分支 `feat/m47-e2e-hp`（1 commit 未核交付物）。本任务逐项核实
m47 工作是否仍有未合入 master 的遗留，并顺带核查 `hp-unique-maxtokens-work`（2026-08-06 旧分支）。

## ① e4cd331 是否已包含在 master —— **已合入（定论）**

```
$ git merge-base --is-ancestor e4cd331 master && echo IS-ancestor
e4cd331 IS ancestor of master (已合入)
```

e4cd331 = `fix(m47): restore dispatch.go — also swallowed by .gitignore (add -f trap)`
（Qingzhe Huang, 2026-08-30 10:31），内容 cmd/agent/dispatch.go (220 行) +
dispatch_test.go (182 行)，共 402 insertions。

**结论: m47 工作无遗留。** feat/m47-e2e-hp 分支本体已在早前收尾中删除
（for-each-ref refs/heads 无该分支），其承载的 commit 已全部可达自 master。

## ② cherry-pick 评估 —— 不适用
① 判定 e4cd331 已在 master 历史，无需 cherry-pick，亦无内容可丢弃。

## ③ remotes/hp 与 remotes/peerbox 上的 m47 命名分支残留 —— 无

```
$ git for-each-ref refs/heads refs/remotes --format='%(refname)' | grep -i m47
（无输出）→ no m47-named refs anywhere
```

本地 + 所有 remote（含 hp/、peerbox/、peerboxx/）均无 m47 命名分支。此前的
`git log --all --grep="m47"` 两个命中（a4ba5cc、e4cd331）均已在 master
（a4ba5cc 见 devdoc/task-ledger-m46-47.md）。

## ④ hp-unique-maxtokens-work 核查 —— 有 5 个未合提交，但已被 master 取代，建议可清理（有 archive tag 兜底）

本地分支已不存在（epic 背景列出的是旧状态），现存引用：
- `refs/remotes/peerbox/hp-unique-maxtokens-work` = f9a319a
- `refs/remotes/peerboxx/hp-unique-maxtokens-work` = f9a319a（同 tip）
- `refs/tags/archive/hp-unique-maxtokens` = f9a319a（**已有归档 tag 兜底**）

```
$ git rev-list --count master..peerbox/hp-unique-maxtokens-work
5                      ← master 之外确有 5 个提交（NOT ancestor）
```

5 个提交（f9a319a ← 41b1c75 ← 2b69713 ← b75f9e4 ← 5c2d112，2026-08-06，
qingzhe huang）改动 10 文件 +356/−41，主题：max_tokens 可配置（GOPI_MAX_TOKENS）、
clamp thinking budget、最终"完全移除 budget"。

**价值评估: 已被 master 后续工作取代，不建议合入。**
master 现状（git grep 证实）：
- `internal/agent/loop.go:1297` — `MaxTokens` 仍可经 `GOPI_MAX_TOKENS` 覆盖
  （该分支的 5c2d112 同款能力，以更完整形态落地）；
- `internal/deepseek/claude_client.go:94` — `DefaultClaudeMaxTokens = 16384`，
  且有测试钉住（claude_client_test.go:901 "A4-converged 16384"）；
- 306c21c `feat(deepseek): A4 — converge max_tokens default 384K → 16384 (measured)`、
  c4f6f9b `feat(agent): enforce thinking budget in the harness` 等后续 commit
  已在 master 上从"移除限制"路线收敛为"A4 实测 16384 + harness 强制 thinking budget"路线。
- 分支 b75f9e4 的"完全移除 budget"方向与 master 现行设计相反（master 保留了
  可配置 + clamp + harness enforcement），合入只会回退设计。

**处置建议: 可清理**——本地分支已无；两处 remote 跟踪引用与命名易误导
（名字与现行 max_tokens 语义冲突）。删除安全：`archive/hp-unique-maxtokens`
tag 已永久指向 f9a319a，任何内容都可找回
（`git ls-remote peerbox 'refs/heads/hp-unique-maxtokens-work'` → 远端删除由 PA 执行）。

## ⑤ worktree 佐证
`git worktree list`：主仓 + gopi-branch-backlog / gopi-ftsync-rescue / gopi-testsuite
三个他人 worktree——均非 m47 相关，m47 的 worktree 早已清理。

## Completed
- ① e4cd331 归属定论: 已在 master（ancestor 判定）
- ② cherry-pick 评估: 不适用（已合入）
- ③ m47 命名分支残留: 本地+remote 全零
- ④ hp-unique-maxtokens-work: 5 未合提交逐一核实 → 判定被 master 取代，建议可清理（tag 兜底在）
- 本报告落盘并提交

## Not Done
- 未删除任何 remote 分支（按 epic 指示，merge/删除由 PA 执行）
- 未 merge 本分支（PA 负责）

## Verification
- git merge-base --is-ancestor e4cd331 master → exit 0（IS ancestor）
- git for-each-ref | grep -i m47 → 空
- git rev-list --count master..peerbox/hp-unique-maxtokens-work → 5（NOT ancestor）
- master grep: GOPI_MAX_TOKENS 覆盖 + DefaultClaudeMaxTokens=16384 均在 master
- Commit: 见 `git log devdoc/m47-e2e-hp-check -1`
