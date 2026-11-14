<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Task Report: 被 BLOCKED 的交付（align/pre-merge 卡住）的 re-delivery 路径 — 真实两节点 e2e 取证

**Epic**: `01a12de4-3c61-78da-a8ae-f01214a2c9e2` · **Branch**: `task/01a12de43c6178daa8aef01214a2c9e2`
**Worktree**: `/home/user/workspace/gopi-01a12de43c6178daa8aef01214a2c9e2`

## Completed

- **0. 悬挂对象回收（epic 的 step 0）**：实测确认实现**不在任何 ref**（既不在本分支、也不在
  master、也不在任何 branch/tag），只以悬挂 commit `9ff61c306` 存活，其 parent `1d6a50184`
  已是本分支祖先。本分支 HEAD `da2a97063` 就是它被重新落到本分支上的真 commit：
  `git diff 9ff61c306 da2a97063` 只打印 **46 个别的文件**（本分支此间前进的正常差异，与此改动无关），
  而 `git diff 4b410c93a da2a97063 --stat` 正是 **9 files / +1671 / -18** —— 与悬挂对象的两半
  （retry-delivery 路径 + `finishCleanupDeliveryEvidenced` settled 判据修复 + 该任务 report/devdoc）完全一致。
- **1. 真实两节点 e2e fixture（/dev/shm）**：client 主仓 + bare peer 仓 + ssh shim
  （把 `<alias>:~/work/GoPi` 的 `git-upload-pack` / `git-receive-pack` 翻译到本地 bare，
  即**生产寻址**，不是把远端写成路径）+ settings.json 声明 build（Rule 12）+ 真实
  `gopi task submit` / `task update` 建的行，deliverable = `{repo, branch, worktree:<已删除目录>}`。
  设备号实测：fixture root **27**，`/` = **66307** ⇒ 可证明 off `/`（Rule 22）。
- **2. 未固定行的三个状态，全部由真实二进制跑出**（证据 08-e2e.log）：
  - ARM1 `--dry-run`：`Settled=False WouldRun=True`，verdict 以
    `delivery owed: ` 起头并以 `the local branch IS still here — the delivery half has not
    run for this row, and the absent directory is not evidence that it did` 收尾 —— 判**未结清**，
    且明说欠交付。
  - ARM2 阻断期 `--apply`（主 checkout 有 tracked dirt）：`candidates=1 removed=0 settled=0
    refused=1`，peer **无此 ref**，verdict 写明 `the DELIVERY was withheld (the branch did
    NOT reach the peer)`。`retry-delivery` 同时把该行标成 `status=blocked` 并落 trail 事件
    `delivery-blocked`。
  - ARM3/ARM4 阻断清除后：dry-run 重新报 owed，`--apply` 交付成功，
    **peer 侧 `git --git-dir=<peer>.git rev-parse --verify refs/heads/task/e2e-stalled`
    打印出的 sha 与分支 tip 逐字节相等**（`76cd1fee…` == `76cd1fee…`）；shim 日志证明
    走的是 `e2e-peer git-receive-pack '~/work/GoPi'`。
  - ARM5 再跑 `retry-delivery` = already-delivered no-op；ledger-keyed（无 `--task`）形态
    打印 `no row owes a delivery (no row's newest event is delivery-blocked)`。
- **3. 回归对照（三条固定行仍结清，第四条如实标注）**：CONTROL 1 已交付行二次 `--dry-run` 仍
  `Settled=True WouldRun=False`，`--apply` 幂等且 **peer tip 前后不变**
  （`before='76cd1fee…' after='76cd1fee…'`）；CONTROL 2a/2b 单节点行（deliverable 只有
  `{repo, branch}`，无 worktree 记录、无 peer 声明）`Settled=True`、`settled=1`；**同一行一旦声明
  peer**（CONTROL 2c）即 `Settled=False WouldRun=True` —— 由**通道**决定，而不是由目录是否存在决定。
  **CONTROL 2d 如实记录一个不同的形状**：一条**记录了 worktree** 的行，在**单节点**（无通道）主机上
  读出 `Settled=False WouldRun=False`，verdict 是 `worktree unreadable: … chdir … no such file or
  directory` —— 这是**另一条守卫臂**（worktree 不可读）先于 gone-directory 臂给出判决，不是
  delivery-owed 臂；因为它 `WouldRun=False`，所以没有任何动作被触发，但它**确实不是 settled**。
  本报告「单节点仍结清」这一条**只对 CONTROL 2a/2b 成立**，不覆盖 2d。
- **4. red→green 对照（本轮新增，这是 epic 明确要求的那一项）**：
  - **RED（编译级）**：`git archive 4b410c93a` 取改动前的树（不借任何 worktree，Rule 29），
    把本分支的测试文件放进去 → `finishCleanupDeliveryEvidenced` 出现 **0** 次，
    新测试 **build failed**（`undefined: finishCleanupDeliveryOwedSkipMarker` 等）。
  - **RED（判定级，更强）**：取**当前树**，只把 classifier 里 gone-directory 那一臂还原成
    改动前的 `return true`（"目录不在 = 已交付"）——包**照常编译**，而
    `TestFinishCleanupStrandedGoneWorktreeIsNotSettled` **FAIL**（日志 11-red-behavior.log）。
  - **RED（e2e 级，控制二进制）**：同一两节点 fixture 上，改动前的控制二进制 dry-run 输出
    `Settled=True WouldRun=False`——**正是缺陷本身**（13-red-attribution.log）。
  - **GREEN**：本分支上同批测试全 PASS（12-green.log）：retry 4 项 + chain 3 项。
- **5. Rule 36 收口**：回收进来的 devdoc 原先**没有 DOC-INDEX 行**——`gopi tool doccheck`
  报出该 gap，`--fix` 补行；同时本改动把 `cli_tasks.go` 下移 25 行，令
  `devdoc/archive-mint-outside-writer.md` 的 anchor 行号失配（anchor gate RED），
  按「regenerate, never hand-edit」用 `python3 tools/docclass/classify.py . --only
  devdoc/archive-mint-outside-writer.md` 重生成该行。现 `gopi tool doccheck` exit 0、
  `classify.py --check` 报 `0 gap(s)`、`go test ./internal/docclass/ -run Anchor` ok。

## Files

- `internal/tools/task_finish_cleanup.go` — settled 判据的问询化（`finishCleanupDeliveryEvidenced`、
  `finishCleanupGoneWorktreeDeliveryOwed*`、owed marker）（回收）
- `internal/tools/task_delivery_retry.go` / `task_delivery_retry_test.go` — ledger-keyed
  re-delivery 路径（回收）
- `cmd/agent/cli_tasks_retry_delivery.go`、`cmd/agent/cli_tasks.go`、`internal/taskverbs/verbs.go`
  — `gopi task retry-delivery` 动词面（回收）
- `devdoc/finish-cleanup-settled-not-inferred-20261012.md`、
  `tasks/01a12dad-3110-7edb-881b-c87ddeedf45a-report.md` （回收）
- `devdoc/DOC-INDEX.tsv` — 补 `finish-cleanup-settled-not-inferred` 行 + 重生成
  `archive-mint-outside-writer` 的 anchor 行（Rule 36）
- `cmd/agent/cli_tasks_help_surface_basic_test.go`、`devdoc/gopi-task-help-surface-inventory.md`
  — 回收对象未带的缺口：`cmd/agent` 的 help 面库存与门槛不认识新动词（本分支补上，见 Verification）
- `devdoc/blocked-delivery-redelivery-e2e-proof.md` — 本报告
- **不在 worktree 内**（Rule 37）：取证脚本与原始输出全部在
  `/home/user/.gopi/sessions/01a12de4-3c61-78da-a8ae-f01214a2c9e2/evidence/`（见下）

## Verification

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK（本分支；控制二进制
  `CGO_ENABLED=0 go build -o /dev/shm/gopi-prefix-01a12de4/gopi-prefix ./cmd/agent/` 亦 OK）
- **Tests**: `make test-budget PKGS='./internal/tools ./cmd/agent'` → 见 `## Result`
- **Ran**（逐字，真实动词 + 真实参数 + 真实 ledger）：
  - `gopi task finish-cleanup --dry-run --json --task 01ae2e00-0000-4000-8000-000000000021 --db /dev/shm/gopi-e2e-01a12de4-.../ledger/memory.db`
  - `gopi task finish-cleanup --apply --task 01ae2e00-0000-4000-8000-000000000021 --db <同上>`
  - `gopi task retry-delivery --task <同上> --db <同上>` 与 ledger-keyed `gopi task retry-delivery --db <同上>`
  - `git --git-dir=<peer>.git rev-parse --verify refs/heads/task/e2e-stalled`（交付前/后）
  - 控制二进制：`/dev/shm/gopi-prefix-01a12de4/gopi-prefix task finish-cleanup --dry-run --json --task <同上> --db <red fixture>`
- **Result**:
  - 交付前 peer 为空 ⇒ ARM2 后仍为空（withheld，`refused=1`）⇒ ARM3/ARM4 后
    `76cd1fee7149f7b56be9136e6da160185563584f` == 分支 tip；控制二进制 dry-run 为
    `Settled=True WouldRun=False`（RED），本分支为 `Settled=False WouldRun=True`（GREEN）。
  - 对照：已交付行 `Settled=True WouldRun=False` 且 peer tip 不变；单节点行 `Settled=True`，
    但同一行声明 peer 后 `Settled=False WouldRun=True`。
  - `make test-budget PKGS='./internal/tools ./cmd/agent'`：**本轮整轮 RED，归属已查明非本改动引入**
    （`internal/tools` 挂起在 `TestMergerSweepPreview_MatchesSweepCandidatePredicate`，
    `cmd/agent` 红在 master 既有红 `TestNotifyScriptAgreesWithTheRegistryConstants`）；
    本改动触碰的测试面定向重跑 **全绿**（见 Not Done 的三证与 `15-scoped-reverify.log`）。
- **Commit**: `da2a97063`（回收：9 files / +1671，即交付物本身）+
  `69e320250`（本报告与 Rule 36 收口：`devdoc/blocked-delivery-redelivery-e2e-proof.md`
  + `devdoc/DOC-INDEX.tsv`）

## Evidence (self-filed)

全部原始输出在 `/home/user/.gopi/sessions/01a12de4-3c61-78da-a8ae-f01214a2c9e2/evidence/`：

| 文件 | 内容 / 我看到的 |
|---|---|
| `08-e2e.log` | 全套 arm 的真实输出：ARM1 `Settled=False WouldRun=True`；ARM2 `settled=0 refused=1` + `peer holds … = ''`；ARM4 后 `peer sha = '76cd1fee…' want = 76cd1fee…`；CONTROL 1/2 行 |
| `04-e2e.log`、`06-e2e-final.log`、`07-e2e.log` | 同套 arm 的重复运行（含 `device: fixture root=27 / =66307`） |
| `arm1.json`、`arm2-blocked.txt`、`arm4-cleared.txt` | 未固定行三态的 JSON/文本快照 |
| `09-control-build.log` | 控制二进制构建：`retry-delivery` 在改前树中 **0** 命中；`finishCleanupDeliveryEvidenced` **0** 次 |
| `10-red-unit.log` | 编译级 RED：`undefined: DeliveryRetryTaskID / deliveryRetryOwedMarker / …`，`FAIL [build failed]` |
| `11-red-behavior.log` | 判定级 RED：包 `build OK`，`--- FAIL: TestFinishCleanupStrandedGoneWorktreeIsNotSettled` |
| `12-green.log` | GREEN：retry 4 项 + chain 3 项全 `--- PASS` |
| `13-red-attribution.log` | 控制二进制 e2e RED：`Settled=True WouldRun=False`；并纠正了我第一次 e2e 脚本里一句自相矛盾的 echo |
| `fixture.sh`、`e2e.sh`、`runner.sh`、`probe.sh` | fixture 与 arm 脚本（一次性、手工、非调度，Rule 35） |
| `build-control.sh`、`red-unit.sh`、`red-unit-behavior.sh`、`red-behavior.sh`、`red-attrib.sh`、`re-extract.sh` | 各驱动脚本 |

- **Rule 22 实测**：`stat -c %d /` = **66307**，`stat -c %d /dev/shm` = **27**，
  `stat -c %d /dev/shm/gopi-prefix-01a12de4` = **27** ⇒ fixture 与控制树都可证明 off `/`。
- **Rule 31 实测**：本 epic 改动的面就是 `finish-cleanup` 与 `retry-delivery` 两个真实动词，
  上表 `08-e2e.log` 是它们的真实调用与观察结果；单元测试被当作**补充**，不是替代。
- **Rule 36**：`gopi tool doccheck` → `the devdoc corpus is consistent — every file under
  devdoc/ has a well-formed row`，exit 0。
- **Rule 30**：见 `## Not Done` 与最终声明。

## Not Done

- **一次自我更正，如实记下**：第一版 RED e2e 脚本（`10-red-e2e.log`）在 apply 臂后打印了
  `(empty => the pre-fix binary delivered NOTHING)`，而**同一段输出里** peer sha 其实**非空**——
  那句话是我的脚本 echo 的措辞，与它自己的实测**互相矛盾**。我重跑了一次带逐臂状态戳的
  attribution 运行（`13-red-attribution.log`）来定位归属，结论是：**dry-run 是本次 epic 缺陷的
  真 RED**（`Settled=True WouldRun=False`，且此时 shim transports = 0，证明判定发生在任何传输之前），
  而 apply 臂那一次**确实推送了**（一次 `git-receive-pack`）。**RED 判据以 dry-run 为准**；
  旧脚本里那句 echo 是错的，不要引用它。
- **未单独复现**「11:26 那批三行 `settled=1`」的线上原始现场：本轮是在 fixture 上重放该判定，
  用控制二进制证明了同一判定在改前会产生 `Settled=True WouldRun=False`。
- **`make test-budget PKGS='./internal/tools ./cmd/agent'` 本轮为 RED，已定位归属、非本改动引入**，
  如实记下：整轮跑在 host load ≈ 17–20（8 核）下，`internal/tools` 挂在
  `TestMergerSweepPreview_MatchesSweepCandidatePredicate`（SIGQUIT 栈显示它停在 channel 等待），
  跑满 421s 后整包 FAIL；`cmd/agent` 则红在 `TestNotifyScriptAgreesWithTheRegistryConstants`
  （master 上同样 RED，已按 Rule 41 另提 bug 行 `01a12e54-461e`）。**归属三证**：
  ① 两个测试所在文件（`internal/tools/merger_status_test.go`、`cmd/agent/chat_session_slots_script_test.go`）
  与本分支改动的交集为空，`git diff master HEAD -- <file>` 对二者均为空（= 与 master 逐字节相同）；
  ② 在**本分支 tip** 上用 `-timeout 50s` 单独跑挂起的那一个测试 =
  `ok github.com/nick/gopi/internal/tools 1.704s`（1.7s 通过，不是挂起）；
  ③ 定向重跑本改动真正触碰的面：
  `go test ./internal/tools/ -run 'DeliveryRetry|FinishCleanup' -count=1` → **ok 66.560s**；
  `go test ./cmd/agent/ -run 'TaskVerb|HelpSurface|RetryDelivery' -count=1` → **ok 77.489s**；
  `CGO_ENABLED=0 go build ./cmd/agent/` → OK（日志 `15-scoped-reverify.log`）。
  即：**本改动触碰的测试全绿**；整轮红的两处都在与 master 相同的文件里，且是负载相关的挂起/master 既有红。
  这条挂在负载下是否稳定复现，留给下一轮（未做负载实验，Rule 27）。
- 本报告只覆盖 recovery + e2e 取证 + red/green 对照 + Rule 36 收口；实现本身的设计理由是那个
  回收进来的 devdoc 与 commit message 的职责，此处不重复。
