<!-- doc-class: historical -->
# Task Report: GOPI.md 指向已退役动词的拼写修正（doccheck / sandbox / scratch-gc → 已发布动词面）

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a12b98-83fd-7498-9e4d-2f4f4a5283c8` (branch `task/01a12b9883fd74989e4d2f4f4a5283c8`, host hosta)
Origin: Rule 41 行 —— 读 GOPI.md Rule 36 正文照抄 `gopi doccheck` 撞墙（exit 1，动词已退役）。

## Completed

- **清点了 GOPI.md 中全部指向已退役顶层动词的拼写，并在真二进制上逐个核验。** 命中的动词有
  三个，全部已被退休拒绝（exit 1）：

  | 退役拼写 | 真二进制判定 | 已发布动词面 |
  |---|---|---|
  | `gopi doccheck` | `is RETIRED — use \`gopi tool doccheck\``, exit 1 | `gopi tool doccheck` |
  | `gopi sandbox …` | `is RETIRED — use \`gopi tool sandbox\``, exit 1 | `gopi tool sandbox …` |
  | `gopi scratch-gc …` | `is RETIRED — use \`gopi tool scratch-gc\``, exit 1 | `gopi tool scratch-gc …` |

  对照组（证明清点不是"看到 gopi 就当退役"）：`gopi ui_state`、`gopi merge`、`gopi worker`、
  `gopi task`、`gopi chat`、`gopi session`、`gopi tool` 全部 `-h` exit 0，**未改**。

- **修正了 GOPI.md 中全部 13 处退役拼写**（`git diff --stat`：13 insertions / 13 deletions，
  单文件）：
  - Rule 36：bash 块 2 行（`gopi doccheck` / `gopi doccheck --fix`）+ 正文 1 行（`` `gopi doccheck` is a **thin face** ``）= **本行必改的 3 处**。
  - Rule 34：9 处（bash 块 4 行 + 正文/反引号引用 5 处）—— 同一文件同类残留。
  - Rule 35：1 处（`` `gopi scratch-gc deps` ``）。
- **清点了同文件是否还有同类残留**：修正后 `grep -nE 'gopi (doccheck|scratch-gc|sandbox)\b' GOPI.md`
  零命中（exit 1 = 无匹配）。
- **按修正后的拼写逐字真跑取证**（Rule 31，见 `## Verification` 的 `Ran:`）：
  `gopi tool doccheck` / `--fix` / `gopi tool sandbox show` / `gopi tool scratch-gc sandboxes --dry-run` /
  `gopi tool scratch-gc deps` 全部 exit 0。
- 报告本体（本文件）。

## Not Done

- **CONFIG.md 中同族残留未改，已按 Rule 41 当场单独提行**：`gopi task submit … --tags bug`
  → **task `01a12ba6-927a-7158-973a-7eeeec044242`**（briefing 含实测命令与输出，证据文件
  `~/.gopi/sessions/01a12b9883fd74989e4d2f4f4a5283c8/artifacts/bug-config-md-retired-verbs.md`）。
  实测的 CONFIG.md 命中 5 处（`gopi sandbox` ×3 行、`gopi scratch-gc families --tree` ×3 行），
  照抄 `gopi scratch-gc families --tree dumps` 得 `is RETIRED — use \`gopi tool scratch-gc\`` + exit 1。
  它带自己的门（`internal/standards/config_doc_gate_test.go`），改它属另一行工作量，本行不夹带。
- **COMMANDS.md 的 3 行未改，且本行不对其下结论**：`COMMANDS.md:121/134/135` 是索引表的
  **retirement 行**（`| \`gopi doccheck\` | routed only \`gopi tool doccheck\` | … |`），
  故意印出退役拼写以记录路由关系，属历史/索引记录形态；是否算"残留"须按
  `devdoc/DOC-INDEX.tsv` 的 truth class 判定，本行不自行判 class。实测同族命中计数：
  `GOPI.md 0 / CONFIG.md 5 / COMMANDS.md 3（退休记录行）/ CHANGELOG.md 0`。
- **未新增任何门禁**（Rule 35：不新增常驻物/脚本）。实测结论是：prose 面（`.md` 里的命令拼写）
  在本仓**没有门**覆盖 —— 已有的 census/help-surface/contracts 门全绿也不代表这些过期拼写
  不存在。这正是本行存在的原因，也是"手动 grep prose"这一步不能被门族绿灯替代的证据。

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD_OK`（见 Evidence 04）
- Tests:
  - `go test ./internal/standards/ -run 'TestDocClass|TestDocManifest|TestDocIndex|TestDocArchive|TestDevdoc|TestDocClassBanner'` → `ok 0.674s`
  - `go test ./cmd/agent/ -run 'TestDocCheck'` → `ok 1.332s`
  - `go test ./internal/agent/ -run 'TestGOPIRule3_MatchesClaudeToolDefs'` → `ok 0.491s`（唯一读 GOPI.md 的规则面门禁）
  - `go test ./internal/standards/ -run 'TestStandardGates|TestGate7'` → `ok 6.116s`
  - `go test ./internal/standards/ -run Doc -v` → 含 `TestCommandsDocTeachesNoRefusedSpelling`、`TestConfigDoc*` 全 PASS
- Ran:（逐字）
  ```
  gopi tool doccheck
  gopi tool doccheck --fix
  gopi tool sandbox show
  gopi tool scratch-gc sandboxes --dry-run
  gopi tool scratch-gc deps
  ```
- Result: 全部 exit 0（`gopi tool scratch-gc deps` 因 crontab 不可读被工具自己按 Rule 15 报 INCOMPLETE 并 exit 1 ——那是工具如实报告测量不完整，不是拼写问题）；对照组三条退役拼写 `gopi doccheck` / `gopi sandbox show` / `gopi scratch-gc sandboxes` 全部 `is RETIRED — use …` + exit 1。

### 一条 pre-existing 的红（非本行引入，已提行）

`make test-budget PKGS='./internal/standards/'` 有 1 个失败：
`TestRemovalAuditGateEveryRemovalSiteEmits`（`internal/tools/task_finish_cleanup.go:4310 / :4339` UNRECORDED）。
**证明与本行无关**：base commit `699a5caad` 的纯净 `git archive` 副本（不施加我的改动）
复跑同一测试 → 同样 FAIL。已按 Rule 41 单独提行：
`gopi task submit` → **task `01a12bac-c4e5-78c2-98a4-d7dea1a2dc89`**
（根因：门禁判据是字面 `BranchDelete`，而 `9b88c4b5b` 已改用同语义的 `BranchDeleteAt`）。

## Evidence (self-filed)

原始输出落在本 run 的 session 目录（Rule 37）：
`~/.gopi/sessions/01a12b9883fd74989e4d2f4f4a5283c8/artifacts/`

1. `01-gopi-md-inventory.txt` —— 修正前 GOPI.md 的命中行（`grep -n`）与每个动词在真二进制上的判定。
2. `02-post-fix-commands.txt` —— 修正后逐字真跑的五条命令 + 原始输出 + exit code。
3. `03-post-fix-residual.txt` —— 修正后残留检查（零命中）。

关键几行原文（避免二次转述）：

```
$ gopi doccheck
gopi: `doccheck` is RETIRED — use `gopi tool doccheck` (`gopi tool doccheck -h` for its help). …
rc=1

$ gopi tool doccheck --repo .
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
rc=0

$ gopi tool sandbox show
sandbox root=/home/user/workspace/gopi-sandbox source=from the built-in default prefix=sandbox- stale=72h0m0s
rc=0

$ gopi tool scratch-gc sandboxes --dry-run
gopi scratch-gc sandboxes: root=/home/user/workspace/gopi-sandbox source=from the built-in default stale=72h0m0s
scratch-gc role=cli-sandboxes home=/home/user/workspace/gopi-sandbox removed=0 stale=72h0m0s disabled=false bytes=0 dry-run=true
keep /home/user/workspace/gopi-sandbox/sandbox-paduty2-9b669b6b reason="not aged"
rc=0

$ gopi tool scratch-gc deps
deps kind=resident name="/home/user/work/GoPi/bin/gopi" where="pid=1536559" origin=gopi detached=yes cgroup="…/vte-spawn-…scope"
deps kind=unit name="apache2.service" where="/etc/systemd/system/apache2.service" origin=foreign
rc=0
```

- 残留检查：`grep -nE 'gopi (doccheck|scratch-gc|sandbox)\b' GOPI.md` → 无输出，exit 1（无匹配）。
- `git status --porcelain` 于本 worktree：见提交后复查（Rule 30）。
- `git diff --stat`：`GOPI.md | 26 +++++++++++++-------------`，1 file changed, 13 insertions(+), 13 deletions(-)。

## Commit

见本 run 分支 `task/01a12b9883fd74989e4d2f4f4a5283c8` 的 tip commit。
