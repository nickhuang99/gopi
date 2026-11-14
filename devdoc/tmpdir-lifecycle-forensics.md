<!-- doc-class: historical -->
# `_tmp` 生命周期取证：一个声明过的 tmpdir，与它缺失的回收腿

测量日期 2026-11-01/02，主机 `hosta`。分支 `task/01a19555e1f7-7565-9ca2-411ec8551d93`。
全部原始输出在 `/home/user/.gopi/sessions/01a19555-e1f7-7565-9ca2-411ec8551d93/evidence/`。

## 0. 前提修正（先读这一条，否则会照着错的前提重造一套）

`/home/user/workspace/_tmp` **不是野目录，不是别人留下的垃圾**。它是 gopi 自己声明的
默认 tmpdir：

- `config.ResolveTmpDirConfigured`（`internal/config/tmpdir.go:79`）的默认值落到
  `scratch.DefaultTmpDir()` → `filepath.Join(WorkspaceUnderHome(), DefaultTmpDirLeaf)`
  （`internal/scratch/tmproot.go:746`），`DefaultTmpDirLeaf = "_tmp"`
  （`internal/scratch/scratch.go:65`）；
- gopi 把这个值**发布进 TMPDIR** 给每一个子进程（`cmd/agent/tmpdir_inject.go` 的
  `publishTmpDir`），所以任何子进程里的 `os.MkdirTemp("", …)`、任何 `mktemp -d`
  都落在它下面；
- 文档里有它：`CONFIG.md` 的 tmpdir 行。

实测（`evidence/06-gotmpdir-coverage.txt`）：

```
### 我的进程 TMPDIR: ---
TMPDIR=/home/user/workspace/_tmp
```

所以缺陷的准确形状不是"没人认识它"，而是**认识它、只在其中一条子命名空间上回收**。
`internal/scratch/declared_trees.go` 已经有一整套 declared-trees 机制，其中
`go-tmp` 这一行就 root 在同一个目录上，但它的谓词是 `go-build*`。

## 1. 规模与构成（实测）

`evidence/00-baseline.txt`、`evidence/22-name-families.txt`、`evidence/30-tmpdir-census.txt`：

| 家族 | 条目数 | 占用 | 归属 |
|---|---|---|---|
| `go-build*` | 106 | 7 466 MB | `go-tmp` 行（declared） |
| `gopi-*` | 21 | 1 102 MB | **无行**（base 谓词部分认） |
| `probe-*` | 27 | 393 MB | **无行**（base 谓词部分认） |
| `tmp.*` | 63 | 110 MB | **无行**（`os.MkdirTemp` / `mktemp -d`） |
| run 自选名（`sigpipe-*`、`walrep`、`loadbearing`、`ab-*`、`lb-*`、`pyA/B` …） | 224 | 11 815 MB | **无行** |
| 合计 | 528 顶层项 | 20 886 MB | |

`memory.db` 共 37 个 / 7.4 GB；其中 6 个是 675 942 400 字节（645 MB）的整份账本副本。

## 2. 现有回收腿到底覆盖什么（实测 `--dry-run`）

`evidence/31-tmpdir-unset.txt`、`evidence/32-go-tmp-holding.txt`：

```
scratch-gc role=cli-trees ... tree=go-tmp present=true entries=106 bytes=11270405 holding=7639498630 holding_n=106 dry-run=true
would-remove /home/user/workspace/_tmp/go-build3659052550 tree=go-tmp age=24h1m45s bytes=11270405
tree-summary tree=go-tmp examined=106 not_ours=339 collectable=1 kept=105 truncated=false
```

读法：**339 个顶层项不在 `go-tmp` 的视野里**（`not_ours=339` 正是 528−106−根 之外
其余的名字），106 个 `go-build*` 里只有 1 个过了 24h 窗口，105 个 "younger than the
stale window"。也就是说这条腿正当工作，只是**只覆盖 7.4 GB 中的一小部分，而且回收速度
被 24h 窗口限制**——`go-build*` 自己会自限（最老的即 2026-10-31 12:40），它不是 20 GB 的病因。

## 3. 已交付的修复：`tmpdir-residue` 这一行

`internal/scratch/declared_tmpdir_residue.go`（新文件）+ `declared_trees.go` 里一行 append。

**它取什么**：`IsBaseResidueName` —— base sweep 已经信任的残余命名空间
（`gopi-<token>` / `gocache-<token>` / `probe-<token>`）。**谓词是借的，不是抄的**
（Rule 14）：第二份"哪些名字是 gopi 生成的"就是这包自己注释里警告的漂移。

**它拒绝什么**：run 自选名一律拒。这是 224 项 / 11.8 GB 的主体，也是这条修复
**不**去碰的部分——名字里没有 token 的目录没有可判定的主人，收它就是 Rule 42 里
"拿别人没交给你的东西"的同形。

**窗口**：`TmpDirResidueStaleAge = 7 * 24h`，比 base sweep 的 6h 长得多，因为这是
**操作员的工作区**不是 scratch 的内存根：`gopi-base-*`、`probe-home-*` 这类目录是
人手动打开来调查的东西，实测人群里 228h–474h 的 fixture 正是报告按名字引用的。

**实测选中集**（`evidence/41-final-verify.txt`）：

```
tree=tmpdir-residue present=true entries=36 bytes=292966807 holding=361744687 holding_n=36 dry-run=true
would-remove /home/user/workspace/_tmp/gopi-base-cc846ee0 tree=tmpdir-residue age=350h55m59s bytes=146283737
would-remove /home/user/workspace/_tmp/gopi-base-verify2 tree=tmpdir-residue age=350h18m22s bytes=146283836
...
tree-summary tree=tmpdir-residue examined=36 not_ours=411 collectable=33 kept=3 truncated=false
```

33 项 / 293 MB 可收，3 项因未过窗口而留。**run 自选名命中数 0**：

```
### 对照：grep 确认 run 自选名一律不出现
0
```

## 4. sigpipe 那 6 个 645 MB 账本：根因

`evidence/11-seed-evidence.txt`、`evidence/20-copy-proof.txt`、`evidence/29-copy-proof-by-row.txt`：

```
675942400 /home/user/.gopi/memory.db
675942400 /home/user/workspace/_tmp/sigpipe-repro-1793506307/memory.db
... （共 6 个，与主账本字节数完全相同）
```

三个证据把它们钉死为**整份拷贝**而不是各自生长：

1. 字节数与主账本**完全相同**（675 942 400）；
2. 两个副本里的**同一条任务行都在**（`gopi task show 01a19527-… --db <副本>` 在两个
   副本里都返回同一条 `in_flight` 行）；
3. `settings.json` 与真实 profile 的**逐字节一致**（`cmp -s` 通过）。

但 `evidence/20-copy-proof.txt` 同时说明它**不是**由 `sandbox.Create` 做的：
前 64 KB 与主账本 `DIFF-64K`（6 个全 diff），而 Rule 34 的 sandbox 副本是**逐字节 copy**。
所以这 6 个目录是**一个 run 手工搭的临时 profile**——不是 `gopi tool sandbox create`
的产物：它们没有 `.sandbox-owner.json`，没有 `<dir>/.gopi/` 这一层，只有
`memory.db`/`-shm`/`-wal`（`evidence/13-sandbox-shape-compare.txt`）。

对照真正的 sandbox 形状（`gopi tool sandbox show`，同在证据文件里）：`root=/home/user/workspace/gopi-sandbox`，
子目录带 `.sandbox-owner.json`。

**判定**：

- **能不能共用一个**：能，而且应当。6 个副本的差别只在前 64 KB 的 sqlite 头/WAL 状态，
  实验要验证的是"进程被 SIGPIPE 打死时账本是否损坏"，一个副本 + 每轮前从主账本重新拉一次
  就够了，不需要 6 份并存的快照。
- **该不该用 testutil 的 writer scaffold（Rule 32）**：不该。`testutil.NewTestDBPath(t)`
  给的是**空**的 per-test 库，而这个实验要的恰恰是"真实的、有 645 MB 行的库"。
  正确通道是**读**那一半：`testutil.Sample(t)` / `SampleDBPath(t)`（只读、由一次
  文档化的 seed 定义构建）。实验若要可写，用 `sandbox.Create` 的 `GOPI_SANDBOX_LEDGER_SEED`
  拷一份进 sandbox——那会得到 owner marker 和回收腿，而不是 6 个无主目录。

**这是推断，不是实测**：我没有重跑那个 sigpipe 实验，所以"共用一个够用"是按实验形状
（同一进程、同一份输入、只在退出方式上分叉）推断的。

## 5. 本行误执行的一次删除（如实记录）

`evidence/32-go-tmp-holding.txt` 里那次 `--explain` **没有**带 `--dry-run`，
所以它是**一次真实 pass**，首行 `removed=1`：删掉了
`/home/user/workspace/_tmp/go-build3659052550`（11 270 405 字节，age 24h1m45s）。

- 它在 `--dry-run` 之前已列为 `would-remove`，所以删的正是计划里的那一个；
- 它是 Go 工具链的构建缓存目录，工具链会自行重建；
- 但它违反了本行"不执行任何删除"的准则，所以在这里点名，而不是隐去。

**这是两件事，不该混成一件。**

**(一) 是我的调用错误**：`gopi tool scratch-gc -h` 第 10 行就写着
`--dry-run  # report what WOULD be removed; deletes nothing`，我在验证 `--explain` 时
没有带上它，而 `--explain` 从来没有承诺过只读。删掉的东西在计划内，可重建。

**(二) 但帮助文本确实读起来像只读**，这一半我按 Rule 41 提了 bug 行
`01a1957a-0e54-7ba8-adfe-39a726228075`（原 01a19579 因 summary 措辞重提、已 cancelled）：

```
  gopi tool scratch-gc --dry-run  # report what WOULD be removed; deletes nothing
  gopi tool scratch-gc --explain --dry-run
                             # the VETTING face: the plan AND a verdict line per
                             # candidate — why each may be collected, or why it is
                             # kept —
```

`--explain` **只以与 `--dry-run` 的组合出现**，措辞是 "VETTING face"；同族的删除面都用
`--yes`（`profile --yes` / `roots --yes` / `stubs --yes`）明示"我也回收"，只有它绕过这条
既定语汇。代码上它没进 dryRun 分支，只是在真扫前多打一份判决
（`cmd/agent/cli_scratch_gc.go:1160-1170`）。

取证与原始输出：`evidence/50-explain-deletes.txt`。**要不要改是复核的裁决**，
本条只负责把"我以为"和"文本实际怎么写"分开记下来。

**裁决已落地（2026-11-01，同一行 `01a1957a-0e54-7ba8-adfe-39a726228075`，提交
`4ca68ba1`）。** 上面那段落的是当天的状态，不改写；这一段是结果，供后来的读者对齐：

- `--explain` **现在蕴含 `--dry-run`**，判定在解析器一处
  （`cmd/agent/cli_scratch_gc.go:scratchGCExplainImpliesDryRun`，在 argv 循环之后、
  任何 arm 读 `dryRun` 之前），所以 home / base / trees / sandboxes / tick 和三个
  `--yes` arm 一起继承这条承诺，而不是在九处各写一遍。
- **唯一例外是「本 arm 自己的同意词」**（按 arm 键的表
  `scratchGCConsentFlagsByArm`：`--yes` 只在 profile / stubs / roots 生效）。
  刻意不做 verb-wide —— home arm 不读 `--yes`，把它当同意就是把同一个缺陷换个方向
  装回去。**上面那段说的「只有它绕过既定语汇」因此成了现在的不变量**：
  `--yes` 就是这条族里"我也回收"的那句话，而其它 face 根本没有这句话。
- 本文档上面引用的行号（`:1160-1170`）是当天那次运行的读数，之后就漂了；现在
  `reportScratchGCVerdicts` 的调用在 `:1274` / `:1867`，函数体在 `:1886`。
  **这是历史记录，不追行号**，只在这里点名一次。
- 实测对照（同一个种子目录，改前/改后二进制）：改前 `--explain` 单独跑
  `removed=1` 且目录消失；改后同一条命令 `dry-run=true` + `would-remove` +
  verdict 行，目录**存活**；裸扫 `scratch-gc` 仍然照删，行为未变。

## 6. 不回收的部分：交给操作员

`genuinely foreign`（303 项 / 18.6 GB，含 224 个 run 自选名、16 个 218 MB 的
`ab-*`/`wal*`/`lb-*` 账本副本、63 个 `tmp.*`）本行**不动**。理由写在谓词里：
`EntryMatch` 就是为此存在的。候选命令（**仅供操作员裁决，本行未执行**）：

```bash
# 先看清楚哪些是 7 天没动过的（233 项 / 8 332 MB）
find /home/user/workspace/_tmp -maxdepth 1 -mtime +6 | wc -l
find /home/user/workspace/_tmp -maxdepth 1 -mtime +6 -exec du -sm {} + | tail -1
```

## 7. 结论

1. `_tmp` 是 gopi 的声明 tmpdir，不是野目录；缺陷是"声明了根、只回收了其中一条子命名空间"。
2. 25 GB 里 7.4 GB 是 `go-build*`（已有行，自限）、7.4 GB 是 `memory.db` 副本、
   18.6 GB 是 run 自选名。**没有一行曾被要求管过第 2、3 类**。
3. 本行补了 `tmpdir-residue`：借 base 谓词，覆盖 36 项 / 293 MB，窗口 7 天，
   走既有 `trees` 面与 resident tick，**不新建任何机制**（Rule 35）。
4. 剩下的 18.6 GB 是**判断**而不是**机制**问题：那些目录的主人不可从名字判定，
   按 Rule 42 不该由一条自动扫描去收。可执行清单在 §6。
