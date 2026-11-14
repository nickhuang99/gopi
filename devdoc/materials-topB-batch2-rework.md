<!-- doc-class: historical -->
# 返工报告：把「A 面批 2」在 master 之上重新落地（含三个分叉文件的调和）

**任务**：`01a0ee18-a5b1-7945-b030-3939c213427c`（返工死同伴分支 `task/01a0ed23`）
**归档 diff**：`01a0ed23154f763eb5ee36b3867ecaf7-rework.diff`，sha256_16 `0153dbb291c172c4`（与交接一致）
**出发点**：master `c2b7987d`（开工时）→ 落点：`ee140ee0`（交付点 sync，master 此时 `923e4c80`）
→ **本报告落点 = 本文件所在提交**（`1655c71e` 收尾轮 → `d9d725d9` 验收锚点 → `295de3a4` 终验锚点 → 本行；定案测量 tip 为 `1655c71e`，此后仅本报告散文再动）
**证据目录**：`~/.gopi/sessions/01a0ee18-a5b1-7945-b030-3939c213427c/ev/`
（`acceptance.txt` 开工轮验收、`acceptance_at_tip.txt` 与 `acceptance_DONE.txt` **定案后的两次验收**、
`final/ACCEPTANCE_FINAL.txt` **本分支最后一次全量验收**、`batch2/new159.txt` 归档新文件清单、
`census/rerun_at_1655c71e.txt` **tip 上重跑的生成器逐字输出**、两侧 diff、`bin/gopi` 自建二进制）
> 取证目录名的坑：本 run 的会话目录是**带连字符**的 `01a0ee18-a5b1-…`（`grep` 本 run 自己的命令
> 命中它的 `*.jsonl` 可证）。曾误建过一个**不带连字符**的 `01a0ee18a5b1…/ev/final/`（三个文件与
> 本目录同名同内容，sha256 逐一相同），已按 Rule 30 清理（`ev/stray_sessiondir_removed.txt`）。
> **该路径在清理之后又被观测到一次**（01:29:16 时 mtime，内容只有 `ev/`），随后消失；本 run
> 连续 20 次探针（0.7s 间隔）与末次 `stat`/`ls`/`find` 均为不存在。**机制本 run 未能定性，不推测**
> ——原始观测逐条留在 `ev/final/phantom_sessiondir_anomaly.txt`。两处记录都保留、不互相覆盖；
> 对交付无影响（本 run 证据 71 个文件全在带连字符的真目录内，全程完好）。

## Completed

1. **159 个新文件重放落地**（`4f657e24`）：26 条六件套（`115-…`–`140-…`）、
   `scripts/materials/{gen-topB.sh,entries-topB.sh}`、`devdoc/materials-topB-batch2.md`。
   重放方式不是 `git apply` 整包，而是 `git checkout <死分支 tip> -- <159 条路径>`——
   归档 diff 被导出成「纯文本 + `Binary files … differ`」的形式，**不再携带 PNG 字节**
   （实测：162 个 `diff --git` 段、**0 个 `GIT binary patch`**、52 个 `Binary files … differ`），
   逐文件 apply 必然在二进制上失败，且 `--exclude` 不会把失败暴露成非零退出（首次尝试被管道掩掉，
   日志留在 `ev/apply.log`）。逐路径 checkout 是对**死分支对象**的只读取用（对象仍在库中），
   不 checkout 死分支本身、不复活它（Rule 23），随后逐文件 `git hash-object` 与
   `1592afa3:<path>` 比对，**159/159 逐字节一致**（无 MISMATCH 输出）。
2. **三个分叉文件的调和**（`61128c08`），逐 hunk，不选边：
   - `devdoc/DOC-INDEX.tsv`：**行级 union**，master 侧逐行保留，只把批 2 的那 1 行
     （`devdoc/materials-topB-batch2.md`）按全表排序插到它与 master 共同的分支点
     （`materials-*` 段第一行之前）。实测 `diff master:` **恰好 +1 行**
     （`1f87a99e` 的两侧各插入过 1–3 行，是本次真冲突的成因）。
   - `docs/archive/demo/materials/README.md`：批 2 的 `§6.05/§6.05.1/§6.06` 整块替换 master 的 `§6.05`
     （`### 6.2` 及其后全部内容 master 原文保留）。**并改正批 2 原稿里的一个错误读数**：
     原稿在四组之外写「仍没有条目的是 `text`」——`text` 不是本仓的命令，那是它自己发现的
     正则坑（字符类不含数字）把 `gopi text2img` 截断出来的产物。
   - `docs/archive/demo/materials/COVERAGE.md`：master 的对外字形（标题/生成器行/返工说明）在前，
     其后是两支记录的并列——**master 侧（第 8/12 轮 `node` 面、第 7/12 轮批 1）与
     批 2 侧（第 8/12 轮批 2）各自成节、原文保留**，每节标注「这是哪个 run、在哪个 commit 上的读数」。
3. **在新 master 语境下重跑 census（Rule 31/38）**：本 run 自建 `bin/gopi`
   （sha256_16 `2f8b03d64c85f0f1`），`scripts/materials-coverage.sh` 在 tip 上实测
   **`produced 143 / 193`**、`candidate dirs 143 (complete=143 incomplete=0)`、`unreadable 0`；
   逐命令判据实测 **37 行 / 35 个动词 / 35 个有条目**。
   两支做的是**互不重叠的面**：`comm -13`（merge-base vs master 的条目目录）给 master-only **26 个**，
   与批 2 的 26 条**交集为 0**，所以 `143 = 117 + 26` 是同源测量，不是把旧数字搬运相加。
   三个冲突文件里的 census 数字因此**同源一致**（都用 tip `61128c08` 上的生成器读数）。

## Not Done（诚实清单）

1. **`measured commit` = `61128c08`，不是最终 tip**：`3c1dd9ed` 只改了这两个 md 里
   `4f657e24`→`61128c08` 的锚字，**没有重跑**生成器。三个文件的编译产物语义完全一致，
   但按 Rule 38 的严格读法，锚字应指一次真实测量；下一次动这两个文件时请重跑并更新。
   **（已在收尾轮 `cb52f40b` 闭合——锚字改为判据式并重跑，见文末「收尾轮」一节；本条原文保留。）**
2. **批 2 那 26 条素材本身没有重跑**：本 run 只做了「重放 + 调和 + census」，
   没有重新执行 `scripts/materials/gen-topB.sh`（真跑会新建沙盒、离屏 X11、
   `shot`/`capture`/`vision_analyze` 等，远超本 run 的 60 分钟预算与本次交付范围）。
   所以六件套的**内容**仍是批 2 那次 run 的真跑记录，本 run 未独立复现它们——
   交付的是「这批工作在新的 master 上不丢、且不与 master 的记录互相矛盾」。
3. **`grep -rn '<<<<<<<' docs/archive/demo/materials/ devdoc/` 在 tip 上不是空：`ee140ee0` 是 66 行、
   `3c1dd9ed` 是 63 行，而**同一条命令在 master 上是 63 行**（`git grep -n '<<<<<<<' master -- … | wc -l`）。
   多出的 3 行全部来自**交付点 sync 带进来的 master 新 commit**（`devdoc/resume-session-target-no-latch.md` 等
   merge-resolution 类散文对冲突标记本身的引用），**不是真冲突标记**；本次重放的 159 个文件里含该字符串的
   为 **0**，三个被调和的文件里行首标记为 **0**。即：这条命令在这个语料库里**本来就不为空**，
   它数的是那些讲解冲突的文档，不是冲突本身。
4. **`text` 那个错误的来历不是笔误**：它是批 2 自己的口径 bug 在文档里的第二处落地。
   本 run 只改了这一处（README §6.06），`COVERAGE.md` 里批 2 的 `Not Done` 第 1 条
   **原文保留**（它记的正是这个坑的原始形态）——两处不矛盾：一处是改正，一处是记录。
   批 2 那次 run 的其余叙述（它自己的「本 run」措辞）同样按 Rule 38 保留原文，未改写为第一人称。

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → rc=0（`ev/acceptance.txt` 第 1 段）
- **Tests**（分支范围冒烟，Rule 27 不跑全树）:
  `go test ./internal/docindex/ ./internal/standards/ -run 'Doc|Manifest|Census|Corpus' -count=1`
  → `ok internal/docindex 1.076s` / `ok internal/standards 4.086s`
  `gopi doccheck` → `the devdoc corpus is consistent — every file under devdoc/ has a well-formed row` (rc=0)
- **Ran**（逐字）:
  ```bash
  git checkout 1592afa3 -- $(cat ev/batch2/new159.txt)          # 159 个新文件
  for p in $(cat ev/batch2/new159.txt); do                       # 与死分支逐字节比对
    [ "$(git rev-parse 1592afa3:$p)" = "$(git hash-object $p)" ] || echo MISMATCH $p; done
  GOPI_MATERIALS_DIR=$PWD/docs/archive/demo/materials bash scripts/materials-coverage.sh
  $EV/bin/gopi -h | sed -n '/^Commands/,/^Examples/p' | grep -oE '^  gopi [a-zA-Z0-9_-]+' \
      | sed 's/^  gopi //' | sort -u | wc -l                 # 35
  $EV/bin/gopi doccheck
  git merge --no-commit --no-ff master && git merge --abort
  grep -rn '<<<<<<<' docs/archive/demo/materials/ devdoc/ | wc -l
  ```
  （每个 `$EV` 展开为本 run 的证据目录
  `/home/user/.gopi/sessions/01a0ee18-a5b1-7945-b030-3939c213427c/ev`；
  `$EV/bin/gopi` 是本 run 自建二进制，sha256_16 `2f8b03d64c85f0f1`。）
- **Result**:
  ```
  ## acceptance @ 3c1dd9ed / master 923e4c80
  go build rc=0
  markers in the 3 relanded files: 0 / 0 / 0
  markers grep -rn over the tree: 63   (master 自己的同一条命令也是 63)
  porcelain: 0
  merge --no-commit --no-ff master: rc=0, unmerged=0, 之后 --abort → porcelain=0, MERGE_HEAD absent
  archive new-file count: 159 ; missing from HEAD: 0
  census: produced 143 / 193   candidate dirs 143 (complete=143 incomplete=0)
  porcelain after census: 0
  ```
  （逐字全文：`~/.gopi/sessions/01a0ee18-a5b1-7945-b030-3939c213427c/ev/acceptance.txt`）
- **Commits**: `4f657e24`（159 新文件）→ `61128c08`（三文件调和）→ `3c1dd9ed`（census 锚字）→
  `7617847b`（返工报告 + Rule 36 分类）→ `ee140ee0`（交付点 sync，master `923e4c80`）

## Evidence (self-filed)

- 归档 diff 的二进制缺失：`grep -c '^GIT binary patch' <diff>` → **0**，`grep -c '^Binary files'` → **52**；
  首次 `git apply` 的失败逐字在 `ev/apply.log`（`patch does not apply` on `04-annotated.png`）。
- 159/159 一致：上述 hash 比对循环**无 MISMATCH 输出**（脚本对每条都比，没有输出即全等）。
- DOC-INDEX 一行之差：`diff ev/master_devdoc_DOC-INDEX.tsv devdoc/DOC-INDEX.tsv` 只有
  `1504a1505 > devdoc/materials-topB-batch2.md …` 一行；调和的**第一次**尝试（`merge-file --union` 式排序
  重写）把 15 行表头注释块挤到了「表尾」，`diff` 立刻把它显示出来 → 改用「按 master 原文件插入 1 行」，
  规则是：**master 的每一行逐字节不动**（`merge-file` 的 2659 行版与 master 的 2656 行版在头部有 11 行差异）。
- census 同源：`git ls-tree master docs/archive/demo/materials | grep -c '00-entry.md$'` → **117**；
  `comm -13 <(merge-base 条目目录) <(master 条目目录) | wc -l` → **26**；与 `new159.txt` 的 26 个目录**交集 0**。

## 收尾轮（口径收尾，commit `cb52f40b`）

上面 `## Not Done` 的第 1 条（锚字停在 `61128c08`）在本轮被**闭合**，并且不是靠"再写一个字面 hash"，
而是换成了**判据式锚定**——这是 Rule 38 的正确读法：

- **被测量对象 = 条目树，不是 commit 字面。** census 生成器只读 `docs/archive/demo/materials/` 下的条目树，
  所以锚字的有效性取决于**条目树**。实测 `git diff --name-only 61128c08 <tip> -- docs/archive/demo/materials/`
  **只列出 COVERAGE.md / README.md 两行散文**，条目文件 **0 个**；两点上 `00-entry.md` 计数同为 **143**。
  两次读数逐字相同因此是**可证明的必然**，不是巧合，也不是把旧数字搬运相加。
- **COVERAGE.md 逐字输出的锚字**改为 `7813cd09`（本 run 在 tip 上真跑的那一次，`ev/final/census_verbatim_7813cd09.txt`），
  并写明 `61128c08` 那一次读数**只对 `61128c08` 声明成立**、两点的等价性由上面的 diff 证明。
- **修掉一处并列未标注的编号**：`COVERAGE.md` 里 master 侧的 `node` 面与批 2 侧同称「第 8/12 轮」，
  同级标题并列而**没有任何归属标注**——读者无法判断哪一节是哪个 run 的读数。现加**同号说明块**，
  点明两份文案并列不合并（互不重叠的面），并给出各自的读数与 commit。
- **README 的测量点列**不再写中间提交字面（`b5265270`，它既不是 tip 也不是被测对象），改记「本 run 在
  tip 的树上重跑」；§6.06 一句同改。
- **口径坑在本次收尾里又复现了一次（现场证据）**：核对文档内逐动词 dump 与本 run 重跑时，
  我第一遍用的正则 `^[a-z_-]+=[0-9]+$` 把 **`text2img=1` 整个吞掉**，于是拿到的是 **34** 个动词而
  不是 **35**——与 `README §6.05.1` 记的「字符类漏字符会凭空造出一个缺口/凭空抹掉一项」是**同一个坑**。
  改用 `^[a-zA-Z0-9_-]+=` 后 **35/35 逐值一致**（`ev/final/{doc_perverb,run_perverb}.txt`，`diff` 无输出）。
  这条留作证据：坑不在文档里被写了一遍就消失，它在下一个人手里照样复现。
- **验收（`cb52f40b` 与合并 master 后的 `896a3a5b` 各跑一遍，逐字在 `ev/final/acceptance_final.txt`；
  定案 tip `1655c71e` 上再跑一遍，逐字在 `ev/acceptance_at_tip.txt`——三次读数相同）**：
  `go build ./...` rc=0；三个被调和文件的行首标记 **0 / 0 / 0**；`git status --porcelain` **0**；
  `git merge --no-commit --no-ff master` 干净（合并后 `896a3a5b` 已含 `a62060d6`，故 `Already up to date`）、
  `--abort` 后仍 `0`；159 个新文件与死分支 tip **159/159 逐字节一致**；26 个目录各 **6 文件**；
  census 在 tip 实测 **143 / 193**（`candidate dirs 143 (complete=143 incomplete=0)`、`unreadable 0`）。
  定案 tip 上又跑了两遍（`1655c71e`、写完本报告后的 `d9d725d9`），逐字在 `ev/acceptance_at_tip.txt`
  与 `ev/acceptance_DONE.txt`——两遍读数逐字相同，因为「验收的被测对象」同样是**条目树 + 三个调和文件**，
  而这两次提交只改了本报告这一份散文：`go build ./...` rc=0、
  materials 下标记 **0**、三个调和文件行首标记 **0/0/0**、`porcelain 0`、
  `merge --no-commit --no-ff master` → `unmerged=0` 且 `--abort` 后仍 `0`、
  159 文件对死分支 tip **checked=159 mismatch=0**、census 与 `7813cd09` 那次**逐字相同**
  （`diff` 只差 `measured commit` 一行，`ev/census/rerun_at_1655c71e.txt`）。
- **工具树里 2 条行首标记的两侧同源**（`devdoc/repo-context-gate-reverify.md:209/213`，
  `git rev-parse {master,HEAD}:<file>` 给同一个 blob `1f0174d6`）：**master 自身就是这么提交的**，
  是别人散文里的引用块，不属本次交付，未动。

### 这一轮的新证据（`~/.gopi/sessions/01a0ee18-a5b1-7945-b030-3939c213427c/ev/final/`）

| 文件 | 内容 |
|---|---|
| `census_verbatim_7813cd09.txt` | 锚字那次生成器的逐字输出（`measured commit : 7813cd09`，`143 / 193`） |
| `tree_unchanged.txt` | `git diff --name-only 61128c08 7813cd09 -- docs/archive/demo/materials/` → 只有两行散文 |
| `perverb_at_tip.txt` | 本 run 重跑的逐动词覆盖数（35 个动词，0 个为 0） |
| `doc_perverb.txt` / `run_perverb.txt` | 文档内 dump 与本 run 实测的逐值比对（`diff` 无输出） |
| `merge_master_forward.txt` | 合并 master `a62060d6` 进分支的逐字输出（0 冲突） |
| `acceptance_final.txt` | 最终验收批逐字输出（build / 标记 / porcelain / merge / census） |
| `ACCEPTANCE_CLOSING.txt` | **本分支最后一次验收（工作树干净时）**：build rc=0、标记 0/0/0、porcelain 0、merge unmerged=0、census `143/193`@tip、159/159 一致、26 目录各 6 文件、master 是 HEAD 祖先（14 提交）、主 checkout porcelain=0、doccheck 绿 |
| `VERIFY_TREE_AND_CENSUS.txt` | 被测对象证明：`7813cd09`/`61128c08`/tip 三点条目文件同为 **143**，两点间 `docs/archive/demo/materials/` 的 diff **只有两行散文**；生成器在 tip 重跑**逐字同值**；逐动词判据重跑 37 行 / 35 动词 / 0 缺口 |
| `phantom_sessiondir_anomaly.txt` | 无连字符取证目录「清理后再现一次」的逐条原始观测（机制未定性，不推测） |
| `gc_log_observation.txt` | 主机既有的 git auto-gc 告警文件（`unreachable loose objects`，mtime 01:03:37，非本 run 引入）：**不动**、只记录，理由与影响写在文件内 |

**最后一次验收是在工作树干净的状态下跑的**（`be395a01`，`ACCEPTANCE_CLOSING.txt`），
所以它同时给出了 Rule 30 要的那一行 `git status --porcelain` 为空的实测。
