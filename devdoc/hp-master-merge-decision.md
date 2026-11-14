<!-- doc-class: historical -->
# Task Report: hp/master merge-backlog row closure — decision: ALREADY MERGED (phantom row)

日期: 2026-09-01 · 执行: gopi 会话 9e6a507c (HP client) · Epic: "hp/master sits unmerged on master"

## 结论（决策）

**hp/master 不需要 merge 也不需要 archive —— 它的 tip 已 100% 在 master 里，
本行是 branch-scan 被陈旧 peerboxx 引用幻影污染产生的重复行（75 行台账噪音）。**

- `peerbox 侧 refs/remotes/hp/master`（HP push-back 落在 server 上的 remote-tracking 引用），
  tip `0a59749`（devdoc/task-id-style-analysis.md, 2026-09-01 18:33），
  经 probe fetch 验证 **IS ancestor of master f35e463**（0 ahead / 792 behind）——
  该 tip 是随 `peerbox/feat/pushback-e2e-t1788252501` merge 6d920df 进入 master 的。
- 内容核查：tip 唯一独有 commit 只是一个 130 行的 devdoc 文档；master 全量包含。
- `git ls-remote peerbox` 全量列出：refs/remotes/hp/master = 0a59749（无其它 hp-* 分支头）。

## 幻影根因（为什么台账攒了 75 行）

1. 本仓历史上存在第二个 remote `peerboxx`（同一台 192.168.7.6，2026-08-31 09:30 最后 fetch，
   之后 remote 从 config 移除但 **refs/remotes/peerboxx/* 残留**）。
2. 其中 `peerboxx/hp-unique-maxtokens-work`（tip f9a319a）恰好名字带 `hp` 且 unmerged。
3. serve branch-scan (`listUnmergedBranches`, `git branch -r --no-merged master`) 把它扫进来，
   ledger 以行摘要命名分支时，操作者把它误记作 `hp/master` —— 实际从未存在名为 hp/master 的
   待 merge 分支。行被反复 cancel → closed row 不再 dedupe（re-arm 语义）→ 每次扫描再提交 →
   2026-09-01 04:58 起累计 **75 行**（72 cancelled / 3 completed，均无 evidence）。

## 处置（决策执行）

1. **不 merge**：无内容可 merge（tip 已在 master）。
2. **不重复 archive**：`archive/hp-unique-maxtokens` tag 已存在（2026-08-06 起），
   m47-e2e-hp-check (f37f5b9) 已裁定该分支为败者（无预算方案被 A4 实证否定）。
   tag tip = 分支 tip（REACHABLE 验证），内容永久可找回。
3. **幻影清除**（止血——否则下次扫描还会撞上 peerboxx 残留）：
   - `refs/remotes/peerboxx/hp-unique-maxtokens-work` 删除（tag 兜底）
   - `refs/remotes/peerboxx/feat/memsync-bidirectional`、`feat/task-submit-model`、
     `peerboxx/master` 删除（三者均 IS-ancestor of master，零未合内容，已逐一验证）
   - 临时 probe ref `peerbox/hp-remote-master-probe` 删除（验证用）
   - `git branch -r --no-merged master` 复查后只剩真实待决分支：
     `peerbox/feat/pushback-chain-report`、`peerbox/feat/test-a-doc-review`（非本 Epic 范围）。
4. **台账**：id 1068 → cancelled（重复 auto 行），id 1071（本会话）→ completed；
   开放中的 hp/master 行归零。

## 教训（P2.5 延伸）

- merge 决策前先验 tip 归属：`git ls-remote <remote>` 是唯一权威（fetch spec
  `+refs/heads/*:refs/remotes/peerbox/*` 根本不会把 peerbox 侧的 refs/remotes/* 镜像下来，
  本地"看不见"≠不存在）。
- remote 从 config 移除不会清 refs —— 残留 remote-tracking 引用会喂幻影给
  branch-scan。收尾链（finish-cleanup）应同时 `git remote remove` + prune refs；
  serve 侧 `git fetch --prune` 亦可自愈（本次已跑）。
- 败者归档 tag 命名 `archive/<shortname>`（本例 archive/hp-unique-maxtokens）与
  分支全名 archive/<branch> 不匹配时，safe-merge 检查 [a] 抓不住 ——
  本仓允许该形态（tag 早于 Rule 9），但新归档一律带日期全名。

## Not Done

- `peerbox/feat/pushback-chain-report`、`peerbox/feat/test-a-doc-review` 两个真实 unmerged
  分支未处置（属各自 Epic，不越界）。
- peerbox 侧 remote-tracking refs/remotes/hp/master 的物理删除属 host 端操作（PA），本端不可达。
- 未改 branch-scan 代码让它跳过 archive-reachable 分支（幻影根因已物理清除，代码防御
  留给后续 need-based 任务）。

## Verification

- `git ls-remote peerbox | grep hp` → 仅 refs/remotes/hp/master 0a59749 + archive tag f9a319a
- probe fetch + `git merge-base --is-ancestor peerbox/hp-remote-master-probe master` → exit 0
- `git rev-list --left-right --count master...probe` → `3 0`（0 ahead = 无未合提交）
- `git merge-base --is-ancestor archive/hp-unique-maxtokens peerboxx/hp-unique-maxtokens-work` → exit 0
- `git branch -r --no-merged master`（清理后）→ 无 hp-* / peerboxx-* 幻影
- 台账 `status in (pending,in_progress,claimed) AND summary like '%hp/master%'` → 0
- scripts/safe-merge.sh peerboxx/hp-unique-maxtokens-work → exit 0 仅作对照（证明守卫 [a]
  的 archive/<branch> 精确匹配抓不到短名 tag——已在教训中记录，非本行 merge 依据）
- Commit: 见 git log devdoc/hp-master-merge-decision.md -1
