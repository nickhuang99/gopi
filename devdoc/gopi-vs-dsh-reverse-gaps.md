<!-- doc-class: normative -->
# 反向竞品研究④：gopi vs dsh — 5 条「从未被查」机制的定向补轮

**任务**：`01a0eef1-87a7-74b8-8c63-c31947430749`
**被测对象**：`/home/user/work/dsh-claude` @ `141eb6fef83422698aef7a981029e843e8161534`（本 run 只读，未改、未提交）
**前序缺口来源**：`devdoc/gopi-vs-codex-reverse-gaps.md` §4.4 —— dsh 列的**合并队列 / worktree 生命周期 / scratch GC / 跨节点同步 / 证据封口** 5 格写着 `待汇总`，理由原文是「那 5 条**从未被查**——不是判了"无"，是**没查**」。本 run 就是那一轮**定向补轮**。

---

## 0. 摘要（TL;DR）

5 条全部给出 dsh 侧结论，**零 `未判`**（前作 §4.4 的 5 个 `待汇总` 格子全部被填满）：

| # | 机制 | dsh 侧结论（一条话） | 判定 |
|---|---|---|---|
| 1 | 合并队列 + 推送闸门 | 有**本地推送检查点**（lefthook `pre-push` = typecheck、`pre-merge-commit` = 两条 staged 校验），但**没有合并队列**，推送闸门也**不校验"是否合到 master"** | **赢**（闸门语义面）；部分对应物须说清 |
| 2 | worktree 生命周期 | 生产代码里 `worktree` **只出现 1 处**，是发布脚本的**单例** `DetachedWorktree`（用完 dispose）；`agent({ isolation: 'worktree' })` 是**显式判拒**的 deferred 选项 | **赢（无对应物）** |
| 3 | scratch GC | 有三处**各自为政**的临时目录清理，**无统一谓词、无年龄闸门、无持有者活体否决、无 off-`/` 判据**；其中两处**在源码注释里明说"不清理，交给 OS"** | **赢（谓词统一 + 活体否决）** |
| 4 | 跨节点同步 | 跨节点语义词**全树零命中**；最近的邻居（loopback 信任栅栏 / `$DSH_HOME` 单机根 / 匿名 ID）全是**单机**语义 | **赢（本仓零对应物）** |
| 5 | 证据固化 | 有**运行时**的 append-only 事件账本（比 gopi 不输），**没有"完成声明 → 被裁决的记录"**这一环：`seal` / `gate claim` / `Ran` 校验全树零命中 | **赢（交付面的证据固化无对应物）** |

**与前作 §4 同形的 dsh 列填充**见 §3。**未判 0 条**（本 run 唯一的不确定性是 dsh 的 CI 侧行为，见 §4 末尾"边界说明"）。

---

## 1. 判定口径（写给复核者）

1. **只认代码，不认文档。** dsh 的 `docs/`、`AGENTS.md`、`.agents/notes/` 在本报告中**不作为判定依据**——除非那份文档本身就是被测机制（如 `packages/session/session-persistence/src/revision.ts` 的类型注释是代码契约的一部分）。这条对本报告很关键：dsh 的 `.agents/notes/` 有 **2198** 个受版本控制的文件，绝大多数是设计文档；把文档当实现会系统性高估 dsh。
2. **检索面**：`packages/**/src/**`（生产代码）、`apps/**/src/**`、`scripts/*.ts`、`native/**`、`.github/**`。**测试文件（`tests/`、`*.spec.ts`）单独标注**——测试里出现某词不等于产品实现了它（本报告第 2 条的关键区分点）。
3. **"未判"的门槛**：本 run **没有**发出任何 `未判`。查不到就是查不到——但每个"查不到"都必须带**检索词原文**，复核者可逐条复跑（§5 Ran 段是 verbatim）。**"我没查到"与"它没有"之间的桥，只有一条：检索词与检索面写出来。**
4. **判定 ≠ 全面碾压。** 每条判定后面必须写清"dsh 有没有最接近的东西"，避免把"不同"读成"没有"。

---

## 2. 五条机制逐条查证

### 机制 1 — 合并队列 + 推送闸门

**gopi 侧**：`internal/tools/task_sync_git_pushback.go:333` `gitPushbackSharesHistoryWithMaster`
（`:304` 注释：判断 `branch` 与对等端 master 是否共享历史 → 不共享即**拒绝推送**）、
`:591` `gitPushbackPlan`、`:628` 的调用点；合并队列侧 `scripts/safe-merge.sh`（竞争收尾守卫，
拦截 `archive/*` / `loser/*` / 仍被 worktree 占用的分支）+ `cmd/agent/merge_run.go`
（**已于 2026-11-01 随顶层 merge 动词删除**，`ls cmd/agent/merge_run.go` → No such file；
同一个引擎现在经 `gopi task merge` 到达，闸门本身不变）。
即：**"推送"是一个有闸门的动作，闸门会检查祖先关系；"合并"是一个排队动作，有前置守卫。**

**dsh 侧查的检索词（原文）**：

```
git grep -niE "merge_queue|mergeQueue|merge queue|pre-?push|prePush|merge-base|mergeBase" -- 'packages/*/src/**' 'apps/*/src/**'
git grep -lniE "merge" -- 'packages/*/src/**'
grep -nE "^[a-z-]+:" lefthook.yml
grep -nE "merge_group|on:|pull_request|push:|branches" .github/workflows/ci.yml
grep -rniE "merge_group|merge queue" .github/
git grep -niE "\bland(ing)?\b|integrat(e|ion) branch|cherry-pick|rebase" -- 'scripts/*.ts' 'scripts/*.mjs' 'packages/**/src/**'
```

**查出的东西（file:line）**：

| dsh 的对应物 | 位置 | 实际是什么 |
|---|---|---|
| `pre-push` 钩子 | `lefthook.yml:52-54` | **只有一个 job**：`name: typecheck` / `run: pnpm run typecheck` |
| `pre-merge-commit` 钩子 | `lefthook.yml:40-49` | 两个 job：`verify-translation-pairing.ts --cached`、`verify-archived-agent-notes.ts` |
| 钩子的定位声明 | `lefthook.yml:1-2` | "Keep these local checkpoints fast; **CI owns the full repository-wide gate matrix**" |
| CI 触发面 | `.github/workflows/ci.yml:3-6` | `on: push: branches: [master]` + `on: pull_request:` —— **没有 `merge_group`** |
| 全仓 `merge_group` / `merge queue` | `.github/` 全目录 | **零命中** |
| 合并语义（`land`/`integrate`） | 4 处命中，逐个读过 | 全是 UI/文案语（`CommandNodeView.tsx:26` "One integrated `/compact`…"、`tmux-context/src/index.ts:11,87` "integrated terminal"）+ `packages/core/session/src/types.ts` 无关命中 → **无一条是合并入队逻辑** |

**结论**：
- **推送闸门——有话可说但语义不同。** dsh **确实在推送时跑检查**（`pre-push` → `pnpm run typecheck`），这是一条**真实的推送闸门**，本报告不否认。但它检查的是**类型正确性**，**不检查"这个分支是不是从 master 长出来的"**——没有 `merge-base`、没有祖先关系判定、没有 reject 理由（对比 gopi 的 `gitPushbackSharesHistoryWithMaster`：`merge-base --all` 为空 → **按名拒绝**）。
- **合并队列——无。** `merge_queue` / `mergeQueue` / `merge_group` **全树零命中**；CI 只有 `push:[master]` 与 `pull_request` 两种触发，即"PR + 人点合并"的常规 GitHub 流，**没有排队语义、没有逐条出队、没有入队前置守卫**。
- 证据等级：`.github/` 目录是**仓库内**的事实，不是猜测；dsh 的 CI 是否存在**仓库外**的合并编排（如内部 bot、GitLab mirror）本 run 无法证否——这条写在 §4 边界说明里。

**判定**：**赢（推送闸门语义面 + 合并队列无对应物）**，**附诚实边界**：dsh 并非"推前不查"，它查类型；它没有的是**祖先关系闸门**与**队列**。

---

### 机制 2 — worktree 生命周期（每任务一块地 + 自动回收）

**gopi 侧**：`internal/tools/worktree_sweep.go:395` `WorktreeSweepRun` / `:426` `worktreeSweepOptionsFrom`
（带 `minAge` 年龄闸门 + `max` 上限）、`:2115` `worktreeSweepRun`（真实实现）、
`internal/tools/worktree_sweep_chat.go`（聊天地块的另一条谓词）、
`internal/tools/task_deliverable_worktree.go`（台账行的 `deliverable.worktree` 归属）、
`internal/tools/worktree_fresh_path.go`（路径必须新鲜）。即：**每任务一块地，地的归属写在台账行上，
回收有年龄 + 持有者谓词，脏/惰性分开处置。**

**dsh 侧查的检索词（原文）**：

```
git grep -niE "worktree|work-tree" -- '*.ts'
git grep -lniE "worktree" -- 'packages/**/src/**' 'apps/**/src/**' 'scripts/**'
git grep -nE "['\"]worktree['\"]" -- 'packages/**/src/**' 'apps/**/src/**' 'scripts/**' 'native/**'
git grep -nE "worktree['\"]?,\s*['\"]add|'worktree', 'add'" -- '*.ts' '*.mjs' '*.js'
git grep -nE "isolation:\s*'" -- '*.ts'
```

**查出的东西（file:line）**：

| dsh 的对应物 | 位置 | 实际是什么 |
|---|---|---|
| `class DetachedWorktree` | `scripts/publish-npm-baseline.ts:203` | "Owns a **temporary detached worktree** and removes it after staging" |
| 地块创建 | `scripts/publish-npm-baseline.ts:212,215` | `mkdtempSync(join(tmpdir(), 'dsh-npm-baseline-'))` + `git worktree add --detach <path> <commit>` |
| 地块回收 | `scripts/publish-npm-baseline.ts:223-236` | `dispose()`：`git worktree remove --force` + `rmSync(temporaryRoot)`；调用点 `:607` |
| 地块的持有者 | 任何地方 | **无**——`DetachedWorktree` 是发布脚本内的**一个局部对象**，不写任何台账、不落任何归属记录 |
| `isolation: 'worktree'` 选项 | `packages/workflow/workflow-worker-thread/src/runtime.ts:41` | `const DEFERRED_AGENT_OPTIONS = new Set(['effort', 'isolation', 'agentType'])` |
| 该选项的行为 | `…/src/runtime.ts:370` + 测试 `tests/workflow-worker-thread.spec.ts:330` | 传入即**报错**：`expect(result.error).toContain('"isolation" is deferred')` —— **"worktree 隔离"在 dsh 里是被显式判拒的未实现项**，不是能力 |
| 其余 `worktree` 命中 | `scripts/change-scope.ts:197,230`（读当前 worktree 的 diff 范围）、`scripts/install-lefthook.mjs:600,657,724,…`（把 hook 装到 worktree 作用域）、`scripts/test-fixture-cleanup.ts`（测试夹具清理）、`scripts/translation-pairing.ts:237,277`（`input: 'worktree'` = 翻译比对的输入源选择） | 全是**工具脚本对 git worktree 的读取/配置**，与"为一次执行分配一块地并回收"无关 |

**结论**：
- 生产代码（`packages/**/src/**` + `apps/**/src/**`）中 `worktree` 命中数 = **1**，就是
  `packages/sandbox/sandbox-windows-acl/src/token.ts:163` 的一句注释（指一份 POC 测试夹具）。
  **没有 worktree 的分配器、没有归属记录、没有回收谓词、没有持有者扫描、没有脏/惰性分类。**
- 唯一的**真实生命周期**在 `scripts/publish-npm-baseline.ts`——它是**发布流水线的单例资源**：
  一次发布建一个 detached worktree，用完 `dispose()`。**生命周期与"任务"无关，与"发布这一步"有关**。
- 最尖锐的一点：dsh **知道** `isolation: 'worktree'` 这个概念（它照抄了 Claude Code 的选项名，
  见 `runtime.ts:39` 的注释 "Deferred Claude Code options we name explicitly in the rejection message"），
  并且**选择了显式判拒**。这比"没听说过"更能说明：worktree 地块在当前 dsh 里**不是能力，是待办**。

**判定**：**赢（无对应物）**。诚实补充：dsh 的发布脚本证明它会用 `git worktree`，只是**不把它当执行地块用**。

---

### 机制 3 — scratch GC（有界临时树，年龄 + 持有者谓词）

**gopi 侧**：文件头就是机制说明 `internal/scratch/gc.go:1-16`（把 sweep 从"测试 pin 内联"移进生产 tick，
理由是"一个停了套件的宿主会攒下所有被杀死套件遗留的根"）；谓词统一在 `internal/scratch/`：
`gc.go:173` `ResolveStaleAge`、`:191` `ScratchEntryStale`、`:490` `Collectable`、`:508` `EntryVerdictFor`
（"未知 = 保留"）、`:616` `EntryVerdictsForBatch`、`:705` `unheldBatch`（持有者活体否决）、
`sandbox_gc.go:64` `SandboxStaleAge = 72 * time.Hour` + `:307` `SweepSandboxes`；
外加 Rule 22 的 off-`/` 判据 `AssertTempRootOffRootFS`。

**dsh 侧查的检索词（原文）**：

```
git grep -lniE "reclaim|sweep|garbage|\bgc\b|evict|prune|stale" -- 'packages/*/src/**' 'apps/*/src/**' 'native/**' 'python/**'
git grep -nE "\bsweep" ... / git grep -nE "reclaim" ... / git grep -nE "retention" ... / git grep -nE "orphan" ...
git grep -niE "maxAgeMs|STALE_AFTER|staleAfter|olderThan|older than" -- 'packages/**/src/**' 'scripts/*.ts' 'apps/**/src/**'
git grep -lniE "mkdtemp|tmpdir|scratchRoot|scratch root|scratchDir|tmp_root" -- 'packages/**/src/**' 'apps/**/src/**' 'scripts/**' 'native/**'
git grep -nE "rm\(|rmdir|cleanup|dispose" -- 'packages/spill/**/src/**'
git grep -rn "privateRoot" -- 'packages/**/*.ts'
```

**查出的东西（file:line）**：

| dsh 的清理机制 | 位置 | 谓词 | 缺口 |
|---|---|---|---|
| 构建产物清理（**手动命令**） | `scripts/clean.ts:7` `knownOrphanEntries = new Set(['node_modules','lib','.typecheck'])`；`:94` "有 package.json 即为活包"；`:101` `unknown = entries.filter(…)`；`:103,112` 未知条目 → **拒绝删除并列出** | **存在性 / 名单**（无时间维度） | 由 `package.json:26` `"clean": "tsx scripts/clean.ts"` **人工触发**，没有 tick、没有年龄 |
| spill（工具输出溢出）根 | `packages/spill/spill-local/src/store.ts:27-33` `privateRoot()` → `mkdtempSync(join(tmpdir(), 'dsh-spill-'))` | **无** | 文件头（`:16-17`）说明用途是"可预测路径会让别的本地用户读到溢出内容或预置符号链接"——**安全动机，不是回收动机**；`:29` 的 `defaultRoot ??=` 意味着进程内**只建一次、不重扫** |
| spill 孤儿 | `packages/spill/spill-policy/src/index.ts:182` | — | 源码注释原文：**"The spill file already written is a harmless orphan; cleanup is deferred."** |
| Windows 沙箱临时目录 | `packages/sandbox/sandbox-local/src/index.ts:452` | **进程 dispose** | 源码注释原文：**"A crash skips all of it… OS temp hygiene (or manual removal) eventually reclaims it."** |
| 测试夹具清理 | `scripts/test-fixture-cleanup.ts:1-8,43` `removeFixtureSafely` | Windows 句柄重试（50×200ms） | **测试基础设施**，不是产品侧的临时树治理 |
| 附件内容寻址孤儿 | `packages/attachment/attachment/src/index.ts:53` | — | 注释原文："may stay unreachable until **a future retention policy** collects them" —— **未来的**，即现在没有 |

**关键零命中**：
- **年龄谓词**：`maxAgeMs|STALE_AFTER|staleAfter|olderThan` 在 `packages/**/src/**` + `apps/**/src/**` + `scripts/*.ts` 里
  **没有一条**是"文件/目录太老就删"。查出的命中全是**会话格式版本**（`session-persistence/src/coordinator.ts:80`
  "older than the supported v…"）、**事务序**（`workspaces/manager.ts:321` "reject a snapshot strictly older than the installed projection"）、
  **压缩保留比**（`compaction-basic/src/config.ts` 的 `retention` = 上下文预算，不是磁盘回收）。
- **统一临时树**：`mkdtemp|tmpdir|scratchRoot|scratchDir|tmp_root` 的命中分散在 fs-local / sandbox / spill /
  subprocess / session-persistence-jsonl / worker-thread —— **每个子系统各自的临时位**，没有任何一个"树"的概念，因此也无从 sweep。
- **持有者活体否决**：无（gopi 的对应物是 `/proc/<pid>/cwd` 站在里面就否决删除）。
- **设备/分区判据**：无（gopi Rule 22 的 off-`/`）。

**结论**：dsh 的回收是**分散的、进程生命周期的、或干脆交给操作系统**的。它**不输在"有没有清理"**——
它有三个清理点、一个有安全动机的私有根、一个拒绝误删未知目录的保守谓词（`clean.ts:103` 那段"unknown entries remain"是
**好的工程实践**，本报告如实记下）。它输在**没有一条统一谓词**：没有年龄、没有持有者活体否决、没有设备判据、
没有"未知=保留但会被重扫"的默认。

**判定**：**赢（谓词统一 + 年龄闸门 + 持有者活体否决）**，**附诚实**：`clean.ts` 的"未知条目拒绝删除"与 gopi 的"未知=保留"同向，这一格**不是碾压**。

---

### 机制 4 — 跨节点同步（两主机台账/知识库互推）

**gopi 侧**：`internal/tools/memory_sync.go:1-17`（节点间知识库同步，git-PR 语义：
`gopi memsync --export --since <unix>` 出 JSON patch、每行带 `sources=node:<host>`；
import 走 `MemoryStore` **同一条** dedup + merge 路径；幂等 = 再导入是 no-op）、
`memory_sync_claim*.go` / `memory_sync_reconcile*.go` / `memory_sync_delta.go`（认领 / 对账 / 增量），
任务侧 `task_sync_git.go` + `task_sync_git_pushback.go`（Rule 28 的两主机分工就建在这上面）。

**dsh 侧查的检索词（原文）**：

```
git grep -lniE "peer|another machine|cross-?node|remote host|hostname|multi-?host" -- 'packages/**/src/**' 'apps/**/src/**'
git grep -lniE "cross-node|multi-node|node sync|replicate|replication|peer-to-peer|cluster" -- 'packages/**/src/**' 'apps/**/src/**' 'docs/**'
git grep -lniE "exportState|importState|stateFile.*(import|export)|transferState" -- 'packages/**/src/**'
git grep -rnE "git',\s*\['push'|\['push'\]|remote'\]|git remote" -- 'packages/**/src/**' 'apps/**/src/**'
# 硬零命中复核（逐条数文件数）：
for q in "cross-node|cross node|node-to-node" "another machine|other machine" \
         "peer host|remote peer" "sync the ledger|ledger sync|sync task|task sync" \
         "machine hostname|host label"; do git grep -liE "$q" -- 'packages/**/src/**' 'apps/**/src/**' 'scripts/*.ts'; done
```

**硬零命中的实测数字**：

| 检索词原文 | 命中文件数 |
|---|---|
| `cross-node\|cross node\|node-to-node` | **0** |
| `another machine\|other machine` | **0** |
| `peer host\|remote peer` | **0** |
| `machine hostname\|host label` | **0** |
| `sync the ledger\|ledger sync\|sync task\|task sync` | **1** —— 逐条读过：`packages/typert/loader/src/index.ts:367` "Reconcile one entry name against the live loader entries; **a mount returns its async task**"（模块加载器的异步任务，与台账无关） |

**最近的邻居们（都查了，都不是跨节点同步）**：

| dsh 的东西 | 位置 | 为什么不是 |
|---|---|---|
| `$DSH_HOME` 单一根 | `apps/cli/src/args.ts:131`（`--profile` = `$DSH_HOME/profiles` 下的 profile）、`packages/attachment/attachment-local/src/index.ts:32,63`（"Explicit harness home; omitted follows `$DSH_HOME`, then `~/.dsh`"）、`packages/boot/app-boot/src/profile.ts:5` | 这是**一台机器上的一个 home**，是 profile 隔离，**不是两节点互推**（对应 gopi 的 `GOPI_HOME` / 沙箱，不是 `memsync`） |
| loopback 信任栅栏 | `packages/client/connection/src/loopback-hostname.ts:12` `isLoopbackHostname` + `src/api-request-trust.ts:108`（`if (!isLoopbackHostname(hostUrl.hostname) && !isTrustedAuthority(...)) return false`） | 这是**反跨节点**的：它把 `localhost / [::1] / 127/8` 之外的 Host 挡在外面 |
| 匿名用户 ID | `packages/identity/anonymous-user-id/src/index.ts:6` | 注释原文：identity "**never derived from** the hostname, network address, **git remote**, or any…" —— dsh 有意让身份**不带机器标识** |
| 远端（`remote` 注入项） | `packages/api/remotes/src/client/index.ts:106`、`packages/client/runtime/src/client/sessions/remotes.ts:12` | 这是**浏览器客户端 ↔ 本地 host 的 RPC 命名空间**（`Typert Remote dispatch`），进程/页面间通道，不是主机间同步 |
| `replicate` 3 处命中 | 全在注释里描述"同一段 UI 逻辑在另一处复刻"（如 `ui-tool/…/bash-sample.module.css:80` "replicated per the registrant"） | 与状态复制无关 |

**结论**：跨节点同步在 dsh 本体**零对应物**——不只是"没找到同类实现"，而是**多个设计选择主动指向单机**：
loopback 栅栏把非本机 Host 挡掉、匿名 ID 明确拒绝从 `git remote` 派生、所有持久化根都挂在单一 `$DSH_HOME` 下。
**判定**：**赢（本仓零对应物）**。**附一条边界**：dsh 是企业产品，**服务端/内部编排**是否存在跨设备状态同步，
本 run 无法从本仓证明——这一条写进 §4，但本报告**不把它标 `未判`**，因为判定的对象是"dsh 源码有没有这套机制"，答案是**确定的"没有"**。

---

### 机制 5 — 证据固化（报告封口 + gate-claim / Ran 记录）

**gopi 侧**：`internal/tools/task_report.go:33` 起（seal **永不覆盖**模型自写内容、只做有界截断与缺失小节标注，
注释原文 "The seal NEVER clobbers model-authored content"）、`:60` `TaskShowReportClaimLabel = "task_report_claim: "`、
`:65`（Ran 侧的 `TaskShowReportRanLabel`）、`:172` `taskReportGateClaimRecord`（**裁决**报告里的闸门声明：
"a sentence asserting a gate result with nothing observed behind it"）、
`:189` `taskReportRanCheckRecord`（裁决 Rule 31 的 `Ran:` 证据）。
即：**报告被写下那一刻，它的"我跑过了"会被逐条记录，并能在 `task show` 读到。**

**dsh 侧查的检索词（原文）**：

```
git grep -lniE "seal|attest|verifiable|provenance|audit" -- 'packages/**/src/**' 'apps/**/src/**'
git grep -lniE "\bverdict\b|acceptance|sign-?off|attestation" -- 'packages/**/src/**' 'apps/**/src/**'
git grep -niE "taskReport|task report|completion report|handoff (note|report)" -- 'packages/**/src/**' 'apps/**/src/**'
git grep -lniE "ledger|journal|append-?only|writeAhead|WAL" -- 'packages/**/src/**' 'apps/**/src/**' 'python/**'
```

**查出的东西（file:line）**：

| dsh 的对应物 | 位置 | 语义 |
|---|---|---|
| append-only 会话账本 | `packages/session/session-persistence-jsonl/src/index.ts:3`（"stores a header and contiguous events in **one append-only file per session**"）、`:451`（"a stat-derived identity for each append-only log") | **运行时事件账本** —— 这一面 dsh **不输**（见下） |
| 每次持久化修订的标识 | `packages/session/session-persistence/src/revision.ts:9` `export type SessionPersistenceRevision = Branded<…>`、`:16` | 后端自有的**不透明修订令牌**，用于"轻量持久化观察" —— 它证明"log 变了"，**不证明"声明被验证过"** |
| `verdict` 的唯一实义命中 | `packages/client/modules/src/index.ts:17` | 注释原文：negative "not a client package" verdict —— **包分类缓存**，不是裁决 |
| `acceptance` 的大量命中 | `packages/client/runtime/src/client/sessions/session.ts:44,250`、`contract/session.ts:39` 等 | 全都是**用户输入被接受**（prompt acceptance = 用户消息进了 host），与"完成声明被验收"**同名不同义** |
| `seal` / `gate claim` / `Ran:` 校验 | 非记忆管线的全树 | **零命中** |
| `packages/runtime-diagnostics/`、`packages/session-query/` | 包存在（`runtime-diagnostics/src/index.ts`、`session-query-sqlite/src/query.ts`、`session-log-export/`） | **诊断与查询**——没有"报告"这个数据模型 |

**结论与诚实边界**：
- **运行时账本：dsh 不输。** `session-persistence-jsonl` 的 per-session append-only 日志
  （`session-checkpoint-policy`、`session-projection`、`consumed-work.ts` 的 "turn 消耗了多少工作"记账）
  是**扎实的运行时记录**，gopi 在这一面没有同等材料。这和前作对 codex 的判断同形（codex 的 trace 更强）。
- **交付面：dsh 无对应物。** dsh 记录的是**发生了什么**（事件流）；gopi 的 seal 记录的是
  **报告声称了什么、这句声称有没有观察支撑**，并把结果打成 `task show` 上可读的一行。
  dsh 的模型侧"完成报告"（`packages/workflow/tool-ralph/src/index.ts:270` "Ralph workflow returned an
  invalid completion report"）是**机器可校验的结构**，不是**被裁决的证据声明**。

**判定**：**赢（交付面的证据固化无对应物）**，**附一条明确的对照**：运行时事件账本这一面 dsh 强，
gopi 的 `task_report` 不做运行时 trace。

---

## 3. 与前作 §4 同形的 dsh 列填充

`devdoc/gopi-vs-codex-reverse-gaps.md` §4.1/§4.2 的 dsh 列里，被标 `待汇总` 的格子如下（**本节只填 dsh 列，
不改那两份报告**——原文可由 `git show HEAD:devdoc/gopi-vs-codex-reverse-gaps.md` 复核）：

### 3.1 技术架构（§4.1 第 2 行「执行治理（本报告轴）」的 dsh 格）

| 子面 | 前作 §4.4 的状态 | 本报告填充（dsh 侧） |
|---|---|---|
| 跨会话台账 | 已判：**无**（`goal` = 同会话） | 维持**无**；本 run 另证：持久化根是单一 `$DSH_HOME`，身份明确不派生自机器 |
| 合并队列 | **待汇总** | **无** —— `merge_queue\|mergeQueue\|merge_group` 零命中；CI 仅 `push:[master]` + `pull_request`（`.github/workflows/ci.yml:3-6`） |
| 推送闸门 | **待汇总** | **有闸门，不是同一把** —— `lefthook.yml:52-54` `pre-push` → `pnpm run typecheck`（检查类型，**不检查祖先关系**）；`lefthook.yml:40-49` `pre-merge-commit` 两条 staged 校验 |
| worktree 生命周期 | **待汇总** | **无（生产代码 1 处注释）** —— 唯一真实生命周期在 `scripts/publish-npm-baseline.ts:203-236`（发布单例）；`isolation:'worktree'` 在 `workflow-worker-thread/src/runtime.ts:41` 是**显式判拒的 deferred 选项** |
| scratch GC | **待汇总** | **有分散清理，无统一谓词** —— `scripts/clean.ts:7,94,101,103`（手动命令、名单谓词）、`spill-local/src/store.ts:27`（进程内 `??=`，无回收）、`sandbox-local/src/index.ts:452-453`（崩溃即交给 OS）；**年龄谓词零命中** |
| 跨节点同步 | **待汇总** | **无** —— 跨节点语义词全树零命中（§2 机制 4 的硬零命中表）；loopback 栅栏（`loopback-hostname.ts:12`）方向相反 |
| 证据封口 | **待汇总** | **运行时账本有，交付面裁决无** —— append-only jsonl（`session-persistence-jsonl/src/index.ts:3`）+ revision 令牌（`…/revision.ts:9,16`）；`seal`/gate-claim/`Ran` 校验零命中 |

### 3.2 产品价值（§4.2 的三格）

| 用户场景 | 前作 §4.2 的 dsh 值 | 本报告可补的事实 |
|---|---|---|
| 多任务/多会话长期工程 | `未判`（`goal` 同会话） | **维持"跨会话账本无"**，但补一层：dsh 的**单会话内**事件账本很强（append-only + 投影 + 修订令牌），所以"长期工程"这一格的输点是**跨会话**，不是**会话内** |
| "能不能信它"（可审计） | **`待汇总`** | **运行时强 / 交付面弱** —— trace 级事件账本扎实；但"完成声明 → 被裁决"这一环没有（§2 机制 5），所以可审计性的**对象**是"发生了什么"，不是"它声称做到了什么" |
| 单次运行成本可见性 | `待汇总` | 本 run **未测**该格（不属 5 条机制之一）：本报告的检索面没有覆盖 dsh 的成本/token 记账路径，**如实留白**，不猜 |

> **本节的诚实声明**：§3.2 最后一格（成本可见性）**不在本 run 的 5 条机制内**，本报告**不填**。
> 前作 §4.4 列的两轮补轮中，本报告只承担 **dsh 侧的那一轮**；claude 侧那一轮是另一个工作项。

---

## 4. 边界说明（本 run 的不确定性，写在这里而不是藏在结论里）

1. **dsh 的仓库外编排不可见。** `.github/` 是仓库内事实（15 个 workflow 文件，全部读过触发面），
   但一个企业仓库的**内部合并 bot / mirror / 私服 CI**可能存在于本仓之外。**本报告对"合并队列"的判定是
   对本仓的判定**，措辞已按此收窄（§2 机制 1 结论段）。
2. **`未判` 数为 0，但这不是"全知"。** 5 条机制每条都给出了检索面 + 检索词原文（§5 Ran），
   复核者可逐条复跑。**"零命中"是可复现的事实；"不存在"是从检索面推出的结论，两者在本报告里分列。**
3. **本 run 未运行任何 dsh 代码。** 判定全部基于**源码静态检索**（`git grep`）。
   dsh 是一个需要 pnpm + Node 的大型 monorepo，本 run 的 60 分钟预算不含安装与运行；
   **动态行为面（例如"它跑起来会不会自己建 worktree"）本报告没有断言。**

---

## Verification

- **Build**: N/A —— 本 run 的交付物是文档，未改任何 Go 代码（`git diff --name-only` 只有新增的
  `devdoc/gopi-vs-dsh-reverse-gaps.md`，见 `## Evidence`）。
- **Tests**: N/A（同上）。本 run 的"测试"是**可复跑的检索**：§5 的 `Ran` 段逐条 verbatim。
- **Ran**（verbatim，见 §5）：全部 `git grep` / `git ls-files` 命令在
  `/home/user/work/dsh-claude` @ `141eb6f` 上执行；census 生成器 `scripts/gopi-stats.sh` 在
  本 worktree 上执行。
- **Result**: 5/5 条机制给出 dsh 侧结论（4 条 file:line + 1 条硬零命中表带数字），`未判` = 0；
  dsh 仓 `git status --porcelain` = **0 行**（只读，未改）；本 worktree `git status --porcelain` 见 §6。
- **Commit**: 见 §6（本报告所在 commit）。

---

## 5. Ran（verbatim）

```bash
# --- 基线 ---
cd /home/user/work/dsh-claude && git rev-parse HEAD
#   → 141eb6fef83422698aef7a981029e843e8161534
cd /home/user/work/dsh-claude && git status --porcelain | wc -l
#   → 0            （只读 dsh：本 run 未修改、未提交它）

# --- 机制 1：合并队列 + 推送闸门 ---
cd /home/user/work/dsh-claude && git grep -niE "merge_queue|mergeQueue|merge queue|pre-?push|prePush|merge-base|mergeBase" -- 'packages/*/src/**' 'apps/*/src/**'
#   → packages/client/runtime/src/client/sessions/provide.ts:88（一句注释 "pre-push roster"），其余零命中
grep -nE "^(pre-\-?\w+|[a-z-]+):" lefthook.yml        # → pre-commit:5 / pre-merge-commit:40 / pre-push:52
sed -n '52,54p' lefthook.yml                           # → "pre-push:" / "  jobs:" / "    - name: typecheck"
sed -n '40,49p' lefthook.yml                           # → 两个 job：verify-translation-pairing / verify-archived-agent-notes
grep -nE "merge_group|on:|pull_request|push:|branches" .github/workflows/ci.yml
#   → 3:on: / 4:  push: / 5:    branches: [master] / 6:  pull_request:      ← 无 merge_group
grep -rniE "merge_group|merge queue" .github/          # → 零命中
git grep -niE "\bland(ing)?\b|integrat(e|ion) branch|cherry-pick|rebase" -- 'scripts/*.ts' 'scripts/*.mjs' 'packages/**/src/**'
#   → 4 处，全为 UI/文案语（已逐条读过，见 §2 机制 1 表）

# --- 机制 2：worktree 生命周期 ---
cd /home/user/work/dsh-claude && git grep -niE "worktree|work-tree" -- '*.ts' | wc -l
git grep -lniE "worktree" -- 'packages/**/src/**' 'apps/**/src/**' 'scripts/**'
#   → packages/sandbox/sandbox-windows-acl/src/token.ts（1 句注释）+ scripts/change-scope.ts|.spec.ts
#     + scripts/install-lefthook.mjs|.spec.ts + scripts/publish-npm-baseline.ts + scripts/translation-pairing*.ts
#     + scripts/test-fixture-cleanup.ts
git grep -nE "worktree" -- 'packages/**/src/**' 'apps/**/src/**' | wc -l
#   → 1                （生产代码里唯一一处，是注释）
git grep -nE "'worktree', 'add'|worktree', 'add'" -- '*.ts' '*.mjs' '*.js'
#   → scripts/publish-npm-baseline.ts:215  runner.run('git', ['worktree', 'add', '--detach', path, commit], repositoryRoot)
sed -n '203,236p' scripts/publish-npm-baseline.ts
#   → class DetachedWorktree / static create(...mkdtempSync(join(tmpdir(),'dsh-npm-baseline-'))...) / dispose(): worktree remove --force + rmSync
git grep -nE "isolation:\s*'" -- '*.ts'
#   → packages/workflow/workflow-worker-thread/tests/workflow-worker-thread.spec.ts:330  agent('x', { isolation: 'worktree' })
sed -n '35,45p' packages/workflow/workflow-worker-thread/src/runtime.ts
#   → const DEFERRED_AGENT_OPTIONS = new Set(['effort', 'isolation', 'agentType'])
sed -n '320,335p' packages/workflow/workflow-worker-thread/tests/workflow-worker-thread.spec.ts
#   → expect(result.error).toContain('"isolation" is deferred')

# --- 机制 3：scratch GC ---
cd /home/user/work/dsh-claude && git grep -lniE "reclaim|sweep|garbage|\bgc\b|evict|prune|stale" -- 'packages/*/src/**' 'apps/*/src/**' 'native/**' 'python/**'
git grep -nE "\bsweep" -- 'packages/**/src/**' 'apps/**/src/**' 'scripts/*.ts' | grep -viE "\.css"
#   → 全是 UI 行扫（running sweep）与 client 会话队列扫描，无文件系统回收
git grep -nE "reclaim" -- 'packages/**/src/**' 'apps/**/src/**' 'scripts/*.ts'
#   → core/scope/src/store.ts:157、preset/agent-presets/src/index.ts:249,502、
#     sandbox/sandbox-local/src/index.ts:452、session-persistence/src/coordinator.ts:224,1229
sed -n '438,462p' packages/sandbox/sandbox-local/src/index.ts
#   → "A crash skips all of it… OS temp hygiene (or manual removal) eventually reclaims it."
git grep -niE "maxAgeMs|STALE_AFTER|staleAfter|olderThan|older than" -- 'packages/**/src/**' 'scripts/*.ts' 'apps/**/src/**'
#   → 无一条是"文件太老就删"（全是会话格式版本/事务序/压缩保留比）
sed -n '1,33p' packages/spill/spill-local/src/store.ts        # → privateRoot(): mkdtempSync(join(tmpdir(),'dsh-spill-'))，defaultRoot ??=
git grep -nE "rm\(|rmdir|cleanup|dispose" -- 'packages/spill/**/src/**'
#   → 只有 invariant.ts 的 disposer 注释
sed -n '170,190p' packages/spill/spill-policy/src/index.ts
#   → "The spill file already written is a harmless orphan; cleanup is deferred."
grep -nE "knownOrphanEntries|package.json'\)|unsafeOrphans.push|addIfPresent" scripts/clean.ts
#   → 7:knownOrphanEntries / 94 / 101 / 103 / 112 / 167
grep -n "\"clean\"" package.json        # → 26:  "clean": "tsx scripts/clean.ts"   （人工命令）

# --- 机制 4：跨节点同步 ---
cd /home/user/work/dsh-claude && for q in "cross-node|cross node|node-to-node" "another machine|other machine" "peer host|remote peer" "machine hostname|host label"; do printf '%-42s -> %s files\n' "$q" "$(git grep -liE "$q" -- 'packages/**/src/**' 'apps/**/src/**' 'scripts/*.ts' | wc -l)"; done
#   → 全部 0 files
git grep -niE "sync the ledger|ledger sync|sync task|task sync" -- 'packages/**/src/**' 'apps/**/src/**' 'scripts/*.ts'
#   → packages/typert/loader/src/index.ts:367（"a mount returns its async task"——模块加载，非台账）
git grep -lniE "exportState|importState|stateFile.*(import|export)|transferState" -- 'packages/**/src/**'
#   → 零命中
sed -n '12,22p' packages/client/connection/src/loopback-hostname.ts        # → isLoopbackHostname: localhost/[::1]/127.0.0.0-8 之外为 false
sed -n '1,20p' packages/identity/anonymous-user-id/src/index.ts            # → "never derived from the hostname, network address, git remote"
sed -n '30,66p' packages/attachment/attachment-local/src/index.ts          # → root = $DSH_HOME/attachments/v1（单机根）

# --- 机制 5：证据固化 ---
cd /home/user/work/dsh-claude && sed -n '1,10p' packages/session/session-persistence-jsonl/src/index.ts
#   → "append-only file per session"
sed -n '1,17p' packages/session/session-persistence/src/revision.ts
#   → 9: export type SessionPersistenceRevision = Branded<...>  / 16: export function SessionPersistenceRevision(value)
git grep -niE "taskReport|task report|completion report|handoff (note|report)" -- 'packages/**/src/**' 'apps/**/src/**'
#   → packages/jobs/jobs/src/types.ts:122（完成报告器）+ workflow/tool-ralph/src/index.ts:270（结构校验）
git grep -niE "\bverdict\b|acceptance|sign-?off|attestation" -- 'packages/**/src/**' 'apps/**/src/**' | head -20
#   → verdict 仅 modules/src/index.ts:17（包分类缓存）；acceptance 全是"用户输入被接受"
ls packages/runtime-diagnostics/*/src/ packages/session-query/*/src/          # → 诊断/查询，无报告数据模型

# --- census（生成器 + commit） ---
cd /home/user/workspace/gopi-01a0eef187a774b88c63c31947430749 && bash scripts/gopi-stats.sh > /home/user/.gopi/sessions/01a0eef1-87a7-74b8-8c63-c31947430749/census-gopi.txt
head -12 /home/user/.gopi/sessions/01a0eef1-87a7-74b8-8c63-c31947430749/census-gopi.txt
#   → repo / branch task/01a0eef187a774b88c63c31947430749 / base commit fe6a99d3470253fbf30441b5398b3e2754094cf6 / dirty files: 0

# --- dsh 侧 census（生成器：git ls-files，在对象库/工作树上只读） ---
cd /home/user/work/dsh-claude && C=$(git rev-parse HEAD)
printf 'commit=%s\ndsh.tracked_files=%s\ndsh.ts_tsx_files=%s\ndsh.ts_tsx_loc=%s\ndsh.pkg_json=%s\ndsh.workspace_globs=%s\ndsh.gh_workflows=%s\ndsh.agents_notes=%s\n' \
  "$C" "$(git ls-files|wc -l)" "$(git ls-files '*.ts' '*.tsx'|wc -l)" \
  "$(git ls-files -z '*.ts' '*.tsx'|xargs -0 cat|wc -l)" "$(git ls-files 'packages/*/*/package.json'|wc -l)" \
  "$(grep -cE '^  - ' pnpm-workspace.yaml)" "$(ls .github/workflows/*.yml|wc -l)" "$(git ls-files '.agents/**'|wc -l)"
#   → commit=141eb6fef83422698aef7a981029e843e8161534
#     dsh.tracked_files=7807   dsh.ts_tsx_files=2700   dsh.ts_tsx_loc=601563
#     dsh.pkg_json=233         dsh.workspace_globs=18   dsh.gh_workflows=15   dsh.agents_notes=2198

# --- gopi 侧锚点（本 worktree） ---
grep -n "gitPushbackSharesHistoryWithMaster\|func gitPushbackPlan\|unrelated-history" internal/tools/task_sync_git_pushback.go
#   → 304 / 333（func）/ 591（func gitPushbackPlan）/ 628
grep -n "func WorktreeSweepRun\|worktreeSweepOptionsFrom" internal/tools/worktree_sweep.go     # → 395 / 426
grep -n "SandboxStaleAge\|func SweepSandboxes" internal/scratch/sandbox_gc.go                   # → 64（72*time.Hour）/ 307
sed -n '1,17p' internal/tools/memory_sync.go                                                    # → memsync --export/--import + node:<host>
sed -n '55,70p' internal/tools/task_report.go                                                   # → TaskShowReportClaimLabel / RanLabel
grep -n "taskReportGateClaimRecord\|taskReportRanCheckRecord" internal/tools/task_report.go      # → 172 / 189
```

---

## Census（数字必带生成器与 commit — Rule 38）

**生成器**：`scripts/gopi-stats.sh`（本仓，只读；自报 commit 与 dirty 状态）与
`git ls-files` / `git rev-parse`（dsh 侧，只读工作树）。

| 量 | 值 | 生成器 | commit |
|---|---|---|---|
| `gopi.production_go_files` | 582 | `scripts/gopi-stats.sh` | `fe6a99d3`（base commit，`dirty files: 0`） |
| `gopi.production_go_loc` | 273757 | 同上 | 同上 |
| `gopi.test_go_loc` | 387725 | 同上 | 同上 |
| `gopi.packages` | 67（cmd=3 internal=63） | 同上 | 同上 |
| `gopi.llm_tools` / `gopi.cli_verbs` / `gopi.env_vars` | 29 / 36 / 93 | 同上 | 同上 |
| `gopi.devdoc_files` | 1589 | 同上 | 同上 |
| `dsh.tracked_files` | 7807 | `git ls-files \| wc -l` | `141eb6fe` |
| `dsh.ts_tsx_files` / `dsh.ts_tsx_loc` | 2700 / 601563 | 同上（`git ls-files -z \| xargs -0 cat`） | 同上 |
| `dsh.pkg_json`（`packages/*/*/package.json`） | 233 | 同上 | 同上 |
| `dsh.workspace_globs`（`pnpm-workspace.yaml`） | 18 | `grep -cE '^  - '` | 同上 |
| `dsh.gh_workflows` | 15 | `ls .github/workflows/*.yml` | 同上 |
| `dsh.agents_notes`（受版本控制的设计文档） | 2198 | `git ls-files '.agents/**'` | 同上 |

> 前作 `competitor-dsh-analysis.md` 记的是 `18088` 文件——那是**含 `node_modules`/未跟踪面**的目录计数；
> 本报告用的是 **`git ls-files` 受版本控制面 = 7807**。两个数字口径不同，**不是矛盾**，此处标注以免复核者误判。

---

## Evidence (self-filed)

- `cd /home/user/work/dsh-claude && git status --porcelain | wc -l` → `0`（dsh 只读，未改未提交）。
- `cd /home/user/work/dsh-claude && git rev-parse HEAD` → `141eb6fef83422698aef7a981029e843e8161534`（与 briefing 指定 commit 一致）。
- `git grep -nE "worktree" -- 'packages/**/src/**' 'apps/**/src/**' | wc -l` → `1`（机制 2 的核心度量）。
- 机制 4 硬零命中四项，逐条 `| wc -l` → 全部 `0`（检索词与输出见 §5）。
- `bash scripts/gopi-stats.sh` 输出头 12 行含 `dirty files: 0`（census 对应的树是干净的）。
- 本 run 未运行 dsh 代码（§4 边界 3），因此**没有任何"dsh 跑起来如何"的声明**。
- 本报告为**新文件**；`devdoc/competitor-dsh-analysis.md` 与 `devdoc/gopi-vs-codex-reverse-gaps.md`
  在本 run 的 diff 中**不出现**（见 §6 的 `git status --porcelain` 输出）。

---

## Not Done（如实写明）

1. **§3.2 的"单次运行成本可见性"格未填** —— 它不在本 run 的 5 条机制内，本报告不猜（前作把它标
   `待汇总`，那是 dsh 报告该覆盖的维度，不是本轮的 5 条之一）。
2. **未动态验证** —— 本 run 没有安装/运行 dsh（§4 边界 3）。所有判定是静态源码检索的结论。
3. **未覆盖 dsh 的 claude 侧对应轮** —— 前作 §4.4 列的是**两轮**补轮（dsh 侧 + claude 侧）；
   本报告只承担 dsh 侧那一轮。
