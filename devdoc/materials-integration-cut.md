<!-- doc-class: historical -->
# materials-integration-cut — 素材库的脚本化挑选与放映产物

**一句话。** `scripts/materials/pick.sh` 一条命令从 `docs/archive/demo/materials/` 的
95 条素材里选出集成集（95 命中 / 0 未命中），并生成可放映产物
`docs/archive/demo/showcase/materials-cut/`（一页一条：标注图 + verbatim 命令 + 一行断言 +
回溯到素材条目的相对路径与 sha256）。没有人工翻条目：命中列表是脚本打印的，
放映页是脚本写的。

**实测时点**：产物自报 `measured_commit: 9e882a15`（生成器提交；`dirty_inputs: none`）。
复核口令：`git show 9e882a15:scripts/materials/pick.sh`——产物把生成器的 commit 写在
自己头部，所以「这一页是哪一版脚本产出的」不必靠人记。
素材树内容清单一整轮未变（见 §5 的只读证据）。产物 sha256：
`index.html a6a3e0cf…` / `selection.tsv 316e6ba4…`。

---

## 1. 判据（= `scripts/materials/pick.sh` 头部注释的实体，冲突以脚本为准）

一条素材被选中，当且仅当五条全真。前四条是素材库自己的机器谓词
（`docs/archive/demo/materials/README.md` §5），后两条是本产物加的出货条件：

| # | 判据 | 机器判据 | 出处 |
|---|---|---|---|
| 1 | **六件齐全** | 六个固定名文件存在且非空；`02-output.txt` 含 `^exit=[0-9]+`；两张 PNG 有 PNG magic | README §2/§5 |
| 2 | **finding 非空** | `05-finding.md` 去掉空行与标题行后仍有正文 | README §2 第 3 条 |
| 3 | **标注真的落笔** | `sha256(04-annotated.png) != sha256(03-raw.png)` | 本仓既有的"sha 不同 = 标注真发生"口径 |
| 4 | **出货面净得出** | 被嵌入放映页的三段（`01-input.txt` 全文、exit 行、finding 首句）经净化后不含 `/home/`、`/Users/`、`/mnt/`、`/media/`、`/opt/`、`/srv/`、`sessions/`、裸 UUID | 本 epic 的出货约束 |
| 5 | **分组** | `face` 取 `00-entry.md` 的 `- face:` 字段；库里最早一批无该字段的条目回落到卡片自己的 `**group**`，记 `face=legacy` | README §4（面以字段为准，不靠目录位置猜） |

**关于判据 3（一条当前不淘汰任何人的门槛，为什么还留着）。** 实测库里
**95/95** 条的 `04-annotated.png` 与 `03-raw.png` 的 sha256 不同，所以这条门槛
今天一条都不淘汰。它不是装饰：它拦的是"把 raw 复制成 annotated"这一类退化——
那种退化**看起来**六件齐全，而放映页会因此印出一张什么都没标注的图。
`--selftest` 用一个合成的"两份图逐字节相同"的条目证明了它真的会拒绝。

**关于 `annotation_required`（记录但不判门槛）。** epic 的字面判据是"有
04-annotated.png"，而 README §1 的人审判据是"拿掉标注图就读不懂"。二者**不等价**：
库里 17 条最早一批的卡片根本没有 `annotation_required` 字段，却确实有标注图。
本脚本按 epic 的字面判据取图（`six` 已含"图在、非空、是 PNG"），把
`ann_decl` 作为**报告字段**逐行如实印出——不做比 epic 更严的静默加码，也不假装
字段不存在。

---

## 2. 命令（verbatim）

```bash
bash scripts/materials/pick.sh                       # 挑选 + 生成放映页（默认两处落点）
bash scripts/materials/pick.sh --selftest            # 判定器的红证：6/6
bash scripts/materials/pick.sh --only env            # 只看某个 face
bash scripts/materials/pick.sh --no-html             # 只挑选（stdout + TSV）
```

默认落点：

- 选择记录 → `$GOPI_EVIDENCE_DIR`，否则 `~/.gopi/sessions/${GOPI_TASK_ID:-pick}/materials-cut/selection.tsv`
  （**证据不进分支**，Rule 30/37）；本 run 的字面调用带 `--evidence` 指向本次会话目录，
  `scripts/materials/pick.sh` 的产物那份也复制到 `--out` 目录。
- 放映页 → `docs/archive/demo/showcase/materials-cut/index.html`（可用 `--out` 覆盖）。

**本 run 实际跑的命令**（逐字）：

```bash
bash scripts/materials/pick.sh --evidence /home/user/.gopi/sessions/01a0ed23-1f78-7a7f-b8ca-e28dcfb41534/materials-cut
bash scripts/materials/pick.sh --selftest
```

---

## 3. 命中列表（脚本打印，不是人工抄）

`pick.sh` 在 stdout 按 face 分组打印全部命中条目。本时点的结果：

```
candidate_dirs: 95
selected: 95
excluded: 0
selected_annotated_bytes: 23083593   (22542 KiB —— 现场测量，随快照变)

## 未命中原因分类
（空）

## 命中条目（按 face 分组）
[env] 29 条        （17 条 legacy 的 A/C 面条目按卡片自己的 group 落在此面）
[task] 17 条
[top_level] 14 条
[memory] 14 条
[node] 4 条
```

**`excluded: 0` 是有含义的 0，不是一句空话。** 一个只会说 yes 的判定器不是判定器，
所以 `pick.sh --selftest` 用**合成的坏条目**（不碰素材树，Rule 29/37）驱动同一条
判定路径，逐条断言它被拒且原因码正确：

```
selftest good         reason=ok                                     expected=ok                   ok
selftest bad-six      reason=six-incomplete:04-annotated.png,png-04-annotated.png expected=six-incomplete:*     ok
selftest bad-finding  reason=finding-empty                          expected=finding-empty        ok
selftest bad-ann      reason=six-incomplete:png-04-annotated.png    expected=six-incomplete:*     ok
selftest bad-applied  reason=annotated-identical-to-raw             expected=annotated-identical-to-raw ok
selftest bad-clean    reason=unclean:shipped-surface(1)             expected=unclean:*            ok
SELFTEST PASS: 6/6（1 条好条目通过 + 5 条各破坏一个判据被拒，原因码正确）
```

选择记录 `selection.tsv` 里**命中与未命中都在**（本时点 95 行明细，全部
`reason=ok`），所以"为什么没选"永远是机器可查的，而不是靠人回忆。

---

## 4. 覆盖率数字（Rule 38：带生成器 + measured_commit）

| 数字 | 值 | 生成器 | measured_commit |
|---|---|---|---|
| 库已产出条目 | **95 / 193** | `scripts/materials-coverage.sh` | `a6d62208` |
| 候选条目目录 | 95（complete=95 / incomplete=0 / unreadable=0） | 同上 | 同上 |
| 本集成集命中 | **95（= 全部候选）** | `scripts/materials/pick.sh` | `9e882a15` |
| 本集成集未命中 | 0 | 同上 | 同上 |
| 放映页嵌入图片总字节 | 23083593（22542 KiB，现场测量） | 同上 | 同上 |
| 放映页文章数 / 嵌入图片数 | 95 / 95 | 同上 | 同上 |
| 集成产物 sha256 | `index.html a6a3e0cf…` / `selection.tsv 316e6ba4…` | 同上 | 同上 |

**口径说明**（避免把两个分子混成一个）：`95 / 193` 的分子是"六件齐全的条目数"，
分母是 README §6 的**库总容量**基线（36+17+16+4+3+27+90），不是"应该有 193 条素材"
的承诺。本集成集的 95 是「95 个候选目录里被四条判据选中的个数」——两者在本时点
数值相同是巧合，不是同一个量：如果库里出现一条六件齐全但出货面净不掉的条目，
`materials-coverage` 仍会计 96，而 `pick.sh` 会把它排除。

---

## 5. 产物形态与出货条件

```
docs/archive/demo/showcase/materials-cut/
├── index.html      一页一条：标注图 + verbatim 命令 + 一行断言 + 回溯路径与 sha256
└── selection.tsv   95 行逐条明细（含未命中的原因码）+ 头部自报 generator/commit/baseline
```

`index.html` 自身的出品事实（都是现场查的，见 §6）：95 条 `<article>`、95 张
`<img>`（路径全部可解析，无断链）、95 条含 `素材条目：docs/archive/demo/materials/<slug>/`
与 `sha256:`、95 条含 `本次真跑 <code>exit=`。

**三条出货条件，机器查不靠人看**：产物内 `grep` **零命中** `/home/user`、`sessions/`、
`GOPI_HOME=/`。净化把本机路径抽象成 `${HOME}` / `${RUN}` / `${SB}` / `${EV}`、
把会话 id 抽象成 `{session-id}`；`GOPI_HOME=${SB}` 这种**品牌化的变量名**是刻意保留的
——读者需要看到"这条素材是在一个私有 profile 里跑的"，而它指向的路径不泄露任何命名空间。

**图片按相对路径直接引用素材条目自己的文件**（`../../materials/<slug>/04-annotated.png`），
**不复制**：复制会造出第二份 bytes，与素材库那个"原始记录"的 sha256 就不再是同一个东西，
而放映页印的 sha256 正是用来锚定那份原始记录的。要连图一起看，在仓库根起静态服务：
`python3 -m http.server 8080`，打开 `/docs/archive/demo/showcase/materials-cut/`。

**只读契约（README §8）**：`pick.sh` 不改素材树任何一个字节。实测证据——跑前跑后对整棵树
取内容清单（`find docs/archive/demo/materials -type f | sort | xargs sha256sum | sha256sum`），
两次都是 `528db4d084912639778cd23cf123a70f8fe08c9bc8caad029750f17032ba4271`；
产物写在树外（`--out` 与证据目录）。

---

## 6. Verification

- **Build**: 不适用（本产物是 bash 生成器 + 两个静态产物，无 Go 改动）；
  `bash -n scripts/materials/pick.sh` → 语法通过（每次改动后都跑）。
- **Tests**: `bash scripts/materials/pick.sh --selftest` →
  `SELFTEST PASS: 6/6`，exit 0（见 §3 逐行输出）。
- **Ran**:
  ```bash
  bash scripts/materials/pick.sh --evidence /home/user/.gopi/sessions/01a0ed23-1f78-7a7f-b8ca-e28dcfb41534/materials-cut
  ```
- **Result**:
  - stdout：`candidate_dirs: 95 / selected: 95 / excluded: 0 / selected_annotated_bytes: 23083593`，
    按 face 分组打印 29+17+14+14+4 条命中条目 → `$EV/pick-run7.log`。
  - 落盘：`docs/archive/demo/showcase/materials-cut/{index.html,selection.tsv}`
    （sha256 `a6a3e0cf83d4dbd25e14e3433874bb1bdd24df99ad3a0f42b61bc185d593673b` /
    `316e6ba4c341141b2eb4acd70e069d1a2c87bba40bcdbc495f9243dadc3c1060`）。
  - 幂等：连跑两次，去掉 `measured_at` 行后 stdout 与 TSV **逐字节相同**。
  - 对账：`selection.tsv` 的命中 slug 集与 `index.html` 的 `<article>` slug 集
    `diff` **完全相同**（95/95）。
  - 出货 grep：`/home/user` / `sessions/` / `GOPI_HOME=/` 在
    `index.html` 与 `selection.tsv` 里各 **0 命中**。
  - 只读：跑前跑后 `docs/archive/demo/materials` 内容清单 sha256
    `528db4d0…ba4271` 不变；`git status --porcelain docs/archive/demo/materials` 空。
  - 覆盖率：`bash scripts/materials-coverage.sh` → `produced 95 / 193`，
    `measured commit a6d62208`。
  - `gopi doccheck` → `the devdoc corpus is consistent …`，exit 0。
  - `git status --porcelain` → **空**（Rule 30）。
- **Commit**: `b7b31acb`（生成器）→ `7369a0ae`（修 provenance 链接）→
  `65b619b3`（报告）→ `d73a1a19`（在干净 HEAD 上重生成两件产物）→
  `2d7af41b`（selftest 的 scratch 目录改走 Rule 22 契约）→
  `9e882a15`（脏标记只描述本次测量读过的字节）→ 最后一轮重生成（产物即上面那两个 sha）。
  **sha 会随重生成变化**：产物头部嵌着自己的 `measured_commit`，所以每多一次提交再重生成，
  sha 就换一次。报告引的这两个 sha 对应 `measured_commit: 9e882a15` 那一版脚本。

### Evidence (self-filed)

| 主张 | 我看到的观测 |
|---|---|
| 判定器能说"不" | `--selftest` 打印 6 行，5 行 `reason=` 是各不相同的拒绝码，末行 `SELFTEST PASS: 6/6`，exit 0 |
| 只读素材树 | `find docs/archive/demo/materials -type f \| sort \| xargs sha256sum \| sha256sum` 跑前跑后同为 `528db4d0…ba4271` |
| 产物无泄漏 | `grep -cF '/home/user'`（对两个产物文件求和）= 0；`sessions/` = 0；`GOPI_HOME=/` = 0 |
| 图不断链 | 95 个 `<img src>`，用 `os.path.isfile` 逐个解析 = 0 断链 |
| 幂等 | 两次运行的 stdout 与 TSV 在剔除 `measured_at` 行后 `diff` 为空 |
| 抓到一个静默 bug | TSV 95 行 vs 放映页 94 条 → 根因 `HEADER_LINES` 把 `#` 开头的列名行也数了一遍又 +1，每个列表都吃掉第一条 → 改为按 `#` 行数取门槛 → 对账 `diff` 为空 |
| 判定器的判据非空洞 | `--selftest` 抓到两条**空洞判据**：`bad-ann`（坏 PNG）被 six 拦下 ⇒ `annotated-missing` 是死分支，已删；`bad-clean` 的 `/home/` 与净化前的锚点不匹配 ⇒ 判据从未真正触发，已改 |
| Rule 22 合规 | `--selftest` 的 scratch 目录改走 `gopi_scratch_mkdir` 后跑完，`ls -d …/queue-pick-selftest-*` 无对象（trap 回收生效） |

---

## 7. Not Done（诚实）

1. **`excluded: 0` 没有真实样本**。判据 1–4 今天一条都不淘汰：库里 95 条全部六件齐全、
   finding 都非空、标注都真落笔、出货面都净得出。所以**四条判据的拒绝能力只有
   `--selftest` 的合成样本作为证据**，没有"真素材被拒"的样本。这不是缺陷（素材库质量
   高是好事），但读这个 0 的人应该知道它的证据来自哪里。
2. **`face=legacy` 的回落分组是启发式**。库里 17 条最早一批的卡片没有 `- face:` 字段，
   本脚本用卡片自己的 `**group**` 把它们落到 `legacy` 面，再由人读时并入对应面。
   README §4 说"面以字段为准"——对这些条目**没有字段可依**，所以这是一个**显式的
   回落**，不是静默猜测。要让它们进正规的面，需要改那些卡片（本任务不许改素材库）。
3. **`annotation_required` 未作为门槛**（理由见 §1）。如果 reviewer 认为该字段应当
   参与判据，`pick.sh` 里已有 `ann_decl` 列，加一行 `elif` 即可——但那会与 epic 的
   字面判据（"有 04-annotated.png"）冲突，需要 epic 先改口径。
4. **放映页是静态服务形态，不是自包含单文件**。图片用相对路径引用素材库原件、
   不复制，所以单独把 `index.html` 发给别人图片会断（这正是为了 sha256 锚定原始记录
   而接受的代价）。要一个能直接发出去的包，得另做一个"拷贝图 + 内联"的变体——
   那会产生第二份 bytes，与本页印的 sha256 不再对应，本任务刻意没有做。
5. **未做视觉回归**（没让视觉模型读图核对"标注框是否指向了正确的像素"）。
   本页只锚定"标注确实落笔"（sha256 不同），不评价标注质量——README §1 明说
   画质与标注是否指向正确像素是复核者（人）的事，机器不冒充裁判。
6. **两个既有的 stylegate/standards 失败不是本轮引入的**（如实记录归因）。
   `go test ./internal/stylegate/` 报三条超 ceiling：`cmd/agent/chat_prefetch.go:204`
   （ignored error）、`internal/tools/worktree_sweep_chat_test.go:80`（unchecked close）、
   `cmd/agent/chat_prefetch_test.go:46`（test sleep）。三者都不在本次 diff 的文件集里
   （`git diff --name-only master...HEAD` 只有 5 个材料相关文件），且 `master` 上
   `chat_prefetch.go:204` 本身就是 `_ = p.record.Close()`。**我没有改它们**：本任务是
   素材集成挑选，改这三个点属于另一件事，按 Rule 26 应由新任务覆盖。
   `internal/standards` 同样 FAIL（48s），原因未逐条定位，与本次 diff 无关。
   **按 Rule 26 已另派任务**：`01a0ee00-ade8-78b1-ba5b-b94085f76ec1`
   （tags=bug,stylegate,gate-red），payload 里带了上面这三条 file:line、归因证据
   （`git show master:cmd/agent/chat_prefetch.go` 第 204 行本来就是 `_ = p.record.Close()`）
   与验收条件。本分支**刻意不改**这三个文件——顺手扩大 diff 会让这次集成挑选的
   复审对象变得不可读。
7. **素材树在被并行任务追加**。本轮 [8/12] 与 [10/12] 两个兄弟任务仍在写素材树，
   所以 95 是 `measured_commit` 时点的快照；重跑 `pick.sh` 会得到新数字。
