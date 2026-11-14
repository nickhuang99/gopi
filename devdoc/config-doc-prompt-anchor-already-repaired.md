<!-- doc-class: historical -->
# Task Report: CONFIG.md prompt 锚点 —— 红的复现、修复它的 commit，以及"陈旧 checkout 让绿的 master 看起来是红的"这个陷阱

task `01a1331f-1b81-71d1-84d5-b6b3efc90fd0`
worktree `/home/user/workspace/gopi-01a1331f1b8171d184d5b6b3efc90fd0`，
branch `task/01a1331f1b8171d184d5b6b3efc90fd0`（base `afbf9b572`）

## 判定（verdict）

**这条 bug 已经被修好了，修复 commit 是 `a4a2b64e0`；本分支不需要任何代码/文档改动。**

红线确实存在，但只存在于**一个已过期的 revision**上：`64b8a316a`（本机 main
checkout `/home/user/work/GoPi` 的 master，也正是本机 `origin/master` 远端跟踪引用
的旧值）。在那个 revision 上 `CONFIG.md:174` 锚 `cmd/agent/main.go:805`，而 805 行
是空行 —— 我用真实运行复现了它（下方 `## Verification`）。同一个 token 在同一台机器
的 repo 里**早已被修好**：`a4a2b64e0` 于 2026-10-13 11:33:43 把该锚改指
`main.go:1006`（真正读 `GOPI_PROMPT` 的那一行）。

**所以本 run 的交付物是"判定 + 证据"，不是"再指一次锚"** —— 盲改会把一个已经正确的
锚点改坏，或产出一个与 `a4a2b64e0` 重复的 commit（Rule 26：修复已落地时，用证据说明
它已落地，不要重复实现）。

## 根因：不是"锚点写错了"，是"行号漂移 + 本机 master 陈旧"

### (1) 锚点出生时是对的（Rule 31：逐 revision 实测，不是推理）

| revision | `main.go:805` 的内容 | 判定 |
|---|---|---|
| `8dd4ce793`（锚点出生，2026-09-27） | `if v := os.Getenv(config.PromptEnvVar); v != "" {` | ✅ 正是该行 |
| `844d1f3d2`（漂移诞生） | `<空行>` | ❌ BLANK |
| `64b8a316a`（reporter 的 base） | `<空行>` | ❌ BLANK（= 复现的红） |
| `afbf9b5`（本分支 tip）/ 对端 master | 改为 `main.go:1006` | ✅ GREEN |

`805` 行是**被一次无语义的位移弄空的** —— `844d1f3d2`（`chore(verb-retirement):
converge the branch onto master's implementation — the epic is already live`）把
`main.go` 里 805 行**之前**的代码搬走了，锚点没动，落点就空了。这是行号锚点的
结构性缺陷（锚点记的是数字，不是符号），也是空白落点门禁
（`b86ad6bf0`，`TestConfigDocRefsLandOnContent` 的 BLANK 谓词）存在的理由。

`1006` 这一行不是"就近挑一个非空行"，它**就是** `GOPI_PROMPT` 的读取点：
`1006` 是那行 `// GOPI_PROMPT env channel (P1-3) …` 注释，`1016` 是
`if v := os.Getenv(config.PromptEnvVar); v != "" {`。re-point 是**按符号**做的。

### (2) 陷阱：本机 master 陈旧，把已修复的历史当成"当前的红"

本 run 开始时，三处 `master` 指向**三个不同的 commit**：

```bash
$ git ls-remote origin refs/heads/master        # 直接问对端（只读）
afbf9b57265e1b47b8605f70aafdba329db366df        refs/heads/master
$ git rev-parse origin/master                   # 本机缓存的远端跟踪引用（陈旧）
2e5b4b942072352ea80e385cd51a6fe0c1a714f6
$ git rev-parse refs/heads/master               # 本机 main checkout（更陈旧）
64b8a316af16c3ab6333ec06d65e868000815652
$ git fetch --dry-run origin
   2e5b4b942..afbf9b572  master     -> origin/master
```

对端 `192.168.7.6:/home/user/work/GoPi` 的 master **已经推进到 `afbf9b572`** ——
也就是**本 run 自己的分支 base**。于是：

- 在 `64b8a316a` 上量 → **BLANK=1，红**（reporter 的观察，真实）
- 在 `afbf9b5`（对端 master 的当前值）上量 → **BLANK=0，绿**

两者都对，差的是 revision。**"在 master 上就红"这句话，只有当人同时说出
"哪个 commit 的 master"时才有意义** —— 本机 `refs/heads/master` 陈旧了
56 个 commit（见账本行 `01a13394-a0d9-7626-80c9-7b897cac2c70`：「本机
refs/heads/master 陈旧 56 个 commit：按 "master" 度量会把已修缺陷读成仍红」，
正是同一个陷阱的独立发现）。

**可复用的判据**：判一条"当前是否红"的断言，先把**对端的 ref 直接问出来**
（`git ls-remote origin refs/heads/master`），不要用本机缓存的 `origin/master`，
更不要用本机 checkout 的 `refs/heads/master`。

## `a4a2b64e0` 在哪些 ref 里、不在哪些 ref 里（Rule 31 的 ref 表）

| ref | 含 `a4a2b64e0`？ | 说明 |
|---|---|---|
| 本分支 `task/01a1331f…`（tip `afbf9b5`） | ✅ | 交付所在的 ref |
| `task/01a1331ad0777617898e4cbd90a1f19c` | ✅ | 同一次修复的另一条分支（`doccheck` anchor-number 那条） |
| 对端 master（`ls-remote` 实测 = `afbf9b572`） | ✅（即本分支 tip 本身） | 对端是权威 |
| 本机 `origin/master`（缓存 = `2e5b4b942`） | ❌ | 陈旧缓存 |
| 本机 `refs/heads/master`（main checkout = `64b8a316a`） | ❌ | 陈旧 checkout（reporter 度量用的就是它） |

`git branch -a --contains a4a2b64e0` 只列出**本机可见**的两条 task 分支 ——
因为对端 master 的推进尚未 fetch 到本机，本机的 ref 图里看不到它。**"哪些 ref 含
这个 commit"这个问题，只有先把对端的 ref 问回来才答得完整**；否则会得出
"修复只存在于我自己的分支上"这个错误结论。

## 本 run 做了什么、没做什么

**做了**：把红与绿各**真跑一次**并留档（Rule 31）；用 `git log -S` 命名修复
commit（Rule 38：figure 要能被重新导出）；用 `git ls-remote` 把"master 到底指哪儿"
定死；把复现放在 `/dev/shm`（dev 27）的**临时 detached worktree** 里跑，
main checkout 全程只读（Rule 6/29）。

**没做**：没有修改 `CONFIG.md`（锚点已是 `main.go:1006`，`a4a2b64e0` 已修）；
没有改任何 Go 代码；没有写入 `/home/user/work/GoPi`；没有为已修复的锚点再造一个
重复 commit（Rule 26）。

## 顺带实测到的一条**预先存在的红**（不是本 run 造成的）

`make test-budget PKGS='./internal/standards ./internal/docclass'` 在本分支上
**FAIL**。这不是本 run 的产物，本 run 的 diff **一个 Go 文件都没碰**
（`git diff afbf9b5 -- '*.go'` 为空）：

```
--- FAIL: TestStandardGates/TestGate3ElseAfterReturnRedProof
    gate_test.go:103: Rule 18 (no else after terminating if): 3 total hits, 0 baselined, 2 escaped, 1 new (unannotated)
    gate_test.go:115: NEW violation (checklist 4.3) at cmd/agent/main.go:228  [else after a terminating if-body — drop the else and de-indent (early return)]
```

**A/B 判据（Rule 31：真跑，不推理）**：在 `/dev/shm` 的**纯净 `afbf9b572`
worktree**（本分支 base，无任何本地改动）上跑同一条命令 —— **同样 FAIL、同一行号**。
所以它在**我的 base 上就已经红了**，与本次判定无关。

**已有人在跟**：账本里已有两行，且有更正 ——
`#01a13367`（原始报告）与 `#01a1336c`（更正：违规字节来自 `749d35a1c`，
经 hand-over merge `c5306a23e` 进入 ground，**master 本身没有这段字节**）。
按 Rule 26/41，这里**引用而不重复提行**；也不由本 run 修（本 run 的范围是
CONFIG.md 锚点的判定，改 `cmd/agent/main.go` 会把一份 docs-only 交付变成代码改动）。

### 一条顺带测出来的**门禁读数陷阱**（值得记）

`go test -run 'TestGate3ElseAfterReturnRedProof' ./internal/standards/`
在**同一棵树**上打印 **`ok`（PASS）**，而父测试
`go test -run 'TestStandardGates' ./internal/standards/` 打印 **FAIL**，
点名同一行 `cmd/agent/main.go:228`。

实测：`TestGate3ElseAfterReturnRedProof` **是**顶层测试（`gate_test.go:327`，
`-v` 确认 `=== RUN`），它**真的跑了并 PASS** —— 因为它验的是门禁对**合成夹具**
该不该红（red proof），而**真实树的扫描由父测试 `TestStandardGates` 的表格驱动**
（`gate_test.go:45` 的 `probe:` 表项）。**`*RedProof` 的独立 PASS 不是"这棵树是绿的"
的判据** —— 拿它当树级结论，就会把一个红的包读成绿的。本 run 的判据因此一律取
**父测试**的输出。

## Not Done (honest)

- **没有修真正的结构性缺陷**：行号锚点会在无视觉语义的位移下静默漂空。
  本 run 只判定并留档，没有提出"锚点跟符号走"的机制改造 —— 那是另一个任务。
- **本机那条陈旧的 `refs/heads/master` 没有被我 fast-forward**：改它属于
  main checkout 的写操作，Rule 6/29 禁止本 run 触碰。留作 operator / 对端同步的活。
- **未新增门禁**：本 run 的产出是判定，不是机制（Rule 35：不轻易新增依赖/机制）。

## Verification

- **Build**: 无代码改动，未构建（本 run 无源码编辑）。
- **Tests**:
  - 本分支 tip（`afbf9b572`）：`go test -count=1 -run ConfigDoc ./internal/standards/`
    → **exit 0, ok**；census 行
    `CONFIG.md reference census over 195 anchor(s): CODE=118 COMMENT=69 BRACE=8 BLANK=0 UNRESOLVED=0`
  - `64b8a316a`：同上命令 → **exit 1, FAIL**（下方 `## Evidence`）
- **Ran**（逐字，两次都是真跑，不是 `-h`）:

```bash
# ① 红：在 /dev/shm 的临时 detached worktree 里跑（main checkout 全程只读）
git worktree add --detach /dev/shm/gopi-01a1331f-probe 64b8a316a
cd /dev/shm/gopi-01a1331f-probe && go test -count=1 -run ConfigDoc ./internal/standards/

# ② 绿：本分支自己的 worktree
cd /home/user/workspace/gopi-01a1331f1b8171d184d5b6b3efc90fd0
go test -count=1 -run ConfigDoc ./internal/standards/

# ③ 修复 commit 的命名（Rule 38：figure 带生成器）
git log --oneline --all -S'main.go:805' -- CONFIG.md
git log --oneline --all -S'main.go:1006' -- CONFIG.md

# ④ master 到底指哪儿（只读问对端）
git ls-remote origin refs/heads/master
```

- **Result**: 见下方 `## Evidence (self-filed)`。
- **Commit**: 见本分支日志（报告自身随本 commit 落地）。

## Evidence (self-filed)

**① 红，在 `64b8a316a` 原样复现**（原始输出留在本 run session dir
`…/artifacts/red-64b8a316a.txt`）。复现用的 worktree 在 `/dev/shm`
（`stat -c %d` → **27**），`/` 的 dev 是 **66308** —— 不在 `/` 分区（Rule 22）：

```
### cmd: go test -count=1 -run ConfigDoc ./internal/standards/
### cwd=/dev/shm/gopi-01a1331f-probe HEAD=64b8a316af16c3ab6333ec06d65e868000815652
--- FAIL: TestConfigDocRefsLandOnContent (0.01s)
    config_doc_gate_test.go:255: CONFIG.md carries 1 reference(s) landing on a BLANK line:
          cmd/agent/main.go:805 (CONFIG.md:174 points at a blank line)
--- FAIL: TestConfigDocRefClassCensus (0.02s)
    config_doc_gate_test.go:299: CONFIG.md reference census over 195 anchor(s): CODE=118 COMMENT=68 BRACE=8 BLANK=1 UNRESOLVED=0 | enforced=1 permitted=0 unenforced(comment+brace)=76
FAIL	github.com/nick/gopi/internal/standards	5.395s
### exit=1
```

**② 绿，在本分支 tip `afbf9b572`**：

```
### exit=0
    config_doc_gate_test.go:299: CONFIG.md reference census over 195 anchor(s): CODE=118 COMMENT=69 BRACE=8 BLANK=0 UNRESOLVED=0 | enforced=0 permitted=0 unenforced(comment+brace)=77
--- PASS: TestConfigDocRefClassCensus (0.02s)
ok  	github.com/nick/gopi/internal/standards	2.921s
```

对照的**唯一差异是 revision**：同一台机器、同一个 repo 对象库、同一个命令 ——
CLI 谓词 `-run ConfigDoc` 两次都执行了，命中集合不同（一次 1 处 BLANK，
一次 0 处），所以不可能是"测试没跑到"造成的假绿。

**③ 命名修复 commit**：

```
$ git log --oneline --all -S'main.go:1006' -- CONFIG.md
a4a2b64e0 fix(config): re-point CONFIG.md's prompt read-at anchor at the line that reads GOPI_PROMPT
$ git log -1 --format='%H %an <%ae> %ad' a4a2b64e0
a4a2b64e08c14e4a7dbe7a839caacf511aea42c3 qingzhe huang <user@gmail.com> Tue Oct 13 11:33:43 2026 +0800
```

**④ 逐 revision 的 805 行内容**（证明锚点出生时正确、被位移弄空）：

```
--- at 8dd4ce793 (anchor introduced, 2026-09-27):
  805:[		if v := os.Getenv(config.PromptEnvVar); v != "" {]
--- at 64b8a316a (reported red):
  805:[]
--- at HEAD:
  1006 is the GOPI_PROMPT read comment; 1016 is the read itself
```

**⑤ 三处 `master` 指向三个 commit**：见上文 `## 根因 (2)` 的 `ls-remote`/`rev-parse`
输出（`afbf9b572` 对端 / `2e5b4b942` 本机缓存 / `64b8a316a` 本机 checkout）。

**⑥ `git status --porcelain` 在本 worktree 打印为空**（Rule 30）：见本 run 的
close-out 记录。
