<!-- doc-class: normative -->

# E3 证据可达性：口径、改写规则与残留（`devdoc/competitor/gap-*.md`）

本文件记录一次**只改引用、不动结论**的修复：把 23 份 `gap-*.md` 里写成相对仓库根的
`artifacts/<相对路径>` 证据引用，改写为指向**真实会话目录**的绝对路径；并把修不动的残留逐条列出，
不改、不猜。

## 1. 问题的形状（实测，不是推断）

`gap-*.md` 里的 E3 证据（「我方实际跑过」）在 worker 的 **session 目录**
（`/home/user/.gopi/sessions/<task-id>/artifacts/`）里产生，**从未进过任何 checkout**。
文档却把它写成相对仓库根的 `artifacts/<相对路径>`，于是在仓库根下 `test -e` 一律为假。

修复前的基线（本 ground HEAD `c96c437f`，在 ground 内执行）：

```bash
cd /home/user/workspace/01a1ae2c-6929-77cc-a0d6-cc000718b3ca
git rev-parse HEAD
# c96c437f53b7fdcbfabb4dedfd74957298e2c20f
grep -oh 'artifacts/[A-Za-z0-9_./-]*' devdoc/competitor/gap-*.md | sed 's/[.,;:)]*$//' | sort -u | wc -l
# 367
grep -oh 'artifacts/[A-Za-z0-9_./-]*' devdoc/competitor/gap-*.md | sed 's/[.,;:)]*$//' | sort -u \
  | while read -r p; do [ -e "$p" ] && echo y; done | wc -l
# 1
```

**367 条全树去重相对路径，仓库根下只有 1 条"存在"** —— 而它正是 `artifacts/` 目录自己
（`ls -d artifacts/` 成功，它下面被引用的文件一个都不在）。按 `(文档, 路径)` 计，
引用点是 **484** 个。

**出现次数有两个数，取决于是否加左边界，写清免得被当成矛盾：**

```bash
cd /home/user/workspace/01a1ae2c-6929-77cc-a0d6-cc000718b3ca
# 无左边界：已写成绝对路径的引用，它的 `artifacts/<tail>` 尾巴也被数一次
git grep -oh 'artifacts/[A-Za-z0-9_./-]*' c96c437f -- 'devdoc/competitor/gap-*.md' \
  | sed 's/[.,;:)]*$//' | sed '/^$/d' | wc -l
# 745
# 有左边界（§5 用的口径）：只数真正是相对引用的那些
python3 ~/.gopi/sessions/01a1ae2c-6929-77cc-a0d6-cc000718b3ca/readings.py | grep '^head'
# head           102     100        65      63        9      643       1       334        1
```

**745 − 643 = 102**，而 `102` 正是 HEAD 上**绝对引用**的出现次数 —— 差值就是那些尾巴。
两个数都对，问的问题不同：745 是"文本里 `artifacts/` 这个串出现几次"，643 是"真正按相对
路径解析的引用有几处"。本文件后面一律用 **643**（有左边界）。

（附带一条实测：`git grep -P` 在本机不可用 —— `fatal: cannot use Perl-compatible regexes
when not compiled with USE_LIBPCRE`。上面能跑的 `-oh` 是**没有** `-P` 的，GNU `grep -ohP`
在管道里才支持 PCRE。）

### 1.1 与主 checkout 上 `377` 的对账（给复核者）

同一组命令在主 checkout `/home/user/work/gopi`（HEAD `e1d47553c`）上得到
`loose distinct = 377`、`exists = 1`。**差的 10 条不是口径差异，是那个树更靠前：**

> **2026-11-06 rebase 之后这条差已经消失。** 上游 `9855534d` / `21bfc5fc` / `e1d47553`
> 三次 D17 复探向 `gap-copilot-cli.md` 添了 10 条新的 `artifacts/d17/…` 尾巴，本分支
> rebase 到 `d1e4aeff` 之后自己的尾巴集也是 **377**，且与主 checkout 逐字节相同：
>
> ```bash
> cd /home/user/workspace/01a1ae2c-6929-77cc-a0d6-cc000718b3ca
> git grep -oh 'artifacts/[A-Za-z0-9_./-]*' origin/master -- 'devdoc/competitor/gap-*.md' | sed 's/[.,;:)]*$//' | sort -u | sha256sum
> grep -oh 'artifacts/[A-Za-z0-9_./-]*' devdoc/competitor/gap-*.md | sed 's/[.,;:)]*$//' | sort -u | sha256sum
> ```
>
> ```
> e7956eca8ef5a9a23d55ca0ef3d9c691cdb3bc54d6f74eb6ab17152be85585f9  -
> e7956eca8ef5a9a23d55ca0ef3d9c691cdb3bc54d6f74eb6ab17152be85585f9  -
> ```
>
> 下面那段 367 vs 377 的对账保留原样，因为它是**这个 ground 在 rebase 之前**的读数记录；
> 现在的读数是 377 = 377（差值全在上游那 10 条 `artifacts/d17/` 上，`comm -23` 仍为空）。

```bash
cd /home/user/workspace/01a1ae2c-6929-77cc-a0d6-cc000718b3ca
git grep -oh 'artifacts/[A-Za-z0-9_./-]*' c96c437f -- 'devdoc/competitor/gap-*.md' | sed 's/[.,;:)]*$//' | sort -u > /tmp/ground_refs.txt
wc -l < /tmp/ground_refs.txt       # 367
cd /home/user/work/gopi
grep -oh 'artifacts/[A-Za-z0-9_./-]*' devdoc/competitor/gap-*.md | sed 's/[.,;:)]*$//' | sort -u > /tmp/main_refs.txt
comm -13 /tmp/ground_refs.txt /tmp/main_refs.txt   # 只在主 checkout 里的
comm -23 /tmp/ground_refs.txt /tmp/main_refs.txt   # 只在 ground 里的 → 空
```

`comm -13` 的输出（10 条，全部来自 `artifacts/d17/` 这一族）：

```
artifacts/d17/
artifacts/d17/A1-submit.txt
artifacts/d17/A2-pid.txt
artifacts/d17/A2-readback.txt
artifacts/d17/A3-negative.txt
artifacts/d17/A4-lifecycle.txt
artifacts/d17/A5-real-ledger-lifecycle.txt
artifacts/d17/A6-isolation.txt
artifacts/d17/A7-list.txt
artifacts/d17/A8-why-sandbox.txt
```

`comm -23` **为空**，所以两个数都对：**本 ground 367，主 checkout 377**，差的就是
`c96c437f` 之后落进主 checkout 的那三个提交（`9855534d9` / `21bfc5fc9` / `e1d47553c`，
`git log --oneline c96c437f..e1d47553c -- devdoc/competitor/` 可查）。

**顺带一个可复用的口径事实**：`sed 's/[.,;:)]*$//'` 在本树里**不改变**去重结果
（剥前 367、剥后还是 367），因为带尾标点的写法（`artifacts/cherry/e1-guide-hits.`）
本来就是同名路径的另一形态。

## 2. 文件还在不在？在。它们留在会话目录里

```bash
ls -d /home/user/.gopi/sessions/*/           | wc -l   # 4102
ls -d /home/user/.gopi/sessions/*/artifacts  2>/dev/null | wc -l   # 549
# 以每个会话目录为 base 建索引（相对路径 → 所在会话），供匹配用
for d in /home/user/.gopi/sessions/*/artifacts; do
  s=${d%/artifacts}; ( cd "$s" && find artifacts -type f 2>/dev/null | sed "s|^|$s\t|" )
done > /tmp/session_index_tuple.txt
wc -l < /tmp/session_index_tuple.txt   # 200015
```

被引用的 367 条里，**335 条在某个会话目录下按同名相对路径命中**，按「同一条路径出现在
几个会话里」分布：

| 命中会话数 | 路径条数 |
|---|---|
| 1 | 258 |
| 2 | 34 |
| 3 | 36 |
| 4 | 6 |
| 7 | 1 |
| 0（按文件匹配不到） | 32 |

所以「文件随 ground 回收、只剩降级这条路」的推断**不成立**：**ground 被回收了，session
目录没有。** `/home/user/workspace/<task-id>/` 会被清理，
`/home/user/.gopi/sessions/<task-id>/` 留着（它正是 briefing 指定的证据法定位置）。
缺的只是文档里少写了一层路径。

## 3. 改写规则（每条都可复核）

对每个 `(文档, 路径)` 引用点，按顺序判定：

1. **唯一解（unique）** —— 该相对路径在整台机器的会话索引里只命中**一个**会话，
   且 `<会话>/<相对路径>` 真实存在。→ 改写。
2. **按文档归因（ambiguous）** —— 命中多个会话，但该文档已由别的唯一解锁定出一个
   **主会话**（该文档唯一解里出现次数最多的那个会话），且主会话下该路径存在。→ 改写为主会话。
3. **目录/通配（area）** —— 路径是目录，或带通配（`artifacts/gap/e3-*.txt`、
   `artifacts/gap/e3/`、被截断成 `artifacts/gap/e3-` 的前缀）。判据不是"文件存在"，而是
   **所指目录存在**：取该路径去掉 `*` 后最长的前缀当目录，主会话下该目录存在即改写。
4. **无法恢复（unresolved）** —— 都不成立。→ **不改**，原样保留，在 §5 逐条列出。

已带会话前缀的引用**不做第二次加前缀**，只把错形态规范化成
`/home/user/.gopi/sessions/<完整-id>/`。实测到的三类坏形态：

- `/home/user/.gopi/sessions/<session-id>/<artifact-path>`（`~` 在 `test -e` 里不展开）；
- `<任意前缀>/sessions/<session-id>//home/user/.gopi/sessions/<session-id>/artifacts/<artifact-path>`（前缀叠加两次）；
- `.//home/user/.gopi/sessions/<id>/artifacts/cherry/repo`（`.//` 前缀）、
  `/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec` 被写成截断形式 `/home/user/.gopi/sessions/01a1ad3b-/artifacts/<artifact-path>`（**id 被截断，永不成立**；完整 id 见 §3）。

改写只做一件事：**在路径前补会话目录**（或把错前缀换成对的）。捕获文件本体**不复制进
仓库**（Rule 39：受控树不放 blob；它们是证据，不是交付物）。

## 4. 结果：每份文档的对照表

`HEAD 相对点` = 该文档在 `c96c437f` 上按 `(文档,路径)` 去重的相对引用点数；
`现绝对点` = 现在同一文档里的绝对会话引用点数；`残留` = 仍是相对、未改的点数。

| 文档 | HEAD 相对点 | 现绝对点 | 残留 |
|---|---|---|---|
| `gap-astrbot.md` | 11 | 11 | 0 |
| `gap-cherry-studio.md` | 18 | 19 | 0 |
| `gap-claude-code.md` | 24 | 25 | 0 |
| `gap-cline.md` | 22 | 22 | 0 |
| `gap-codex.md` | 25 | 29 | 0 |
| `gap-copilot-cli.md` | 23 | 42 | 0 |
| `gap-crush.md` | 18 | 19 | 0 |
| `gap-deep-code.md` | 21 | 22 | 0 |
| `gap-deepseek-tui.md` | 20 | 21 | 0 |
| `gap-github-copilot.md` | 28 | 32 | 0 |
| `gap-hermes.md` | 19 | 20 | 0 |
| `gap-kilo-code.md` | 38 | 38 | **1** |
| `gap-langcli.md` | 20 | 22 | 0 |
| `gap-lobehub.md` | 14 | 16 | 0 |
| `gap-nanobot.md` | 20 | 22 | 0 |
| `gap-oh-my-pi.md` | 22 | 25 | 0 |
| `gap-openclaw.md` | 17 | 18 | 0 |
| `gap-opencode.md` | 18 | 20 | 0 |
| `gap-pi.md` | 10 | 10 | **1** |
| `gap-qoder.md` | 2 | 8 | **1** |
| `gap-qwen-code.md` | 17 | 30 | **1** |
| `gap-reasonix.md` | 20 | 21 | 0 |
| `gap-workbuddy.md` | 4 | 8 | 0 |
| **合计** | **431** | **500** | **4** |

**这张表在第一个版本里写错过，错因值得留在这里。** 最初填的是「合計 484 / 483」，用的是
`artifacts/[A-Za-z0-9_./-]*` 这样一个**没有左边界**的正则：它会把一条已经写成
`/home/user/.gopi/sessions/<id>/artifacts/gap/e3-d01.txt` 的引用里那个 `artifacts/gap/e3-d01.txt`
**尾巴**再算成一个相对引用点。于是同一处证据被数两次 —— 一次算绝对、一次算相对 ——
总数虚高，而且虚高的部分恰好全是"已经改好的"那些。加上 `(?<![/A-Za-z0-9_.-])` 之后，
`head` 列从 484 落到 **431**，`现绝对` 从 483 升到 **500**（多了 9，正是那 9 条
`~/.gopi/sessions/<session-id>/` 被规范化成绝对形式的结果，见 §3）。

`gap-qoder.md` 的 `2 → 8` 是同一件事的另一面：它 HEAD 上的引用本来就写成
`~/.gopi/sessions/<session-id>/`，被左边界排除，所以既不计相对也不计绝对；规范化后进入绝对计数。
**`残留` 列（4 条）在两种口径下都是 4，这是这张表里唯一没有被口径影响的一列。**

（`现绝对点` 有时比 `HEAD 相对点` 多：那几份文档在 HEAD 上本来就有若干已经写成
`~/.gopi/sessions/<session-id>/` 的引用，被 §3 的规范化收进同一个计数；少 1 的地方就是那 1 个残留点。）

按**解决方式**统计（`resolution.tsv` 的第四列，一行一个引用点）：唯一解 **258**、
按文档归因 **168**、未解决 **58**，合计 484。

**"58 未解决"与"最终残留 4"之间那 54 条的去向，必须说清，否则两个数看起来互相矛盾。**
`resolution.tsv` 是在**第 3 级（目录/通配）规则跑之前**写下的，所以当时落进 unresolved 的
58 条里，有 **54 条**随后被那一级解掉 —— 它们不是文件、而是目录或通配前缀：

```bash
cd /home/user/workspace/01a1ae2c-6929-77cc-a0d6-cc000718b3ca
# 58 条 unresolved 里，现在仍是相对引用的有几条
python3 ~/.gopi/sessions/01a1ae2c-6929-77cc-a0d6-cc000718b3ca/unresolved-recheck.py
# unresolved in tsv: 58   still relative now: 4   fixed after the tsv was written: 54
```

那 54 条是 `artifacts/gap/`、`artifacts/gap/e3/`、`artifacts/cherry/repo`、`artifacts/gap/e3-`
（被截断的通配前缀）、`artifacts/gap/sb/sandbox-gapcodex-abc04aae`（沙箱目录）这一类 ——
**它们是"所指目录存在"的问题，不是"所指文件存在"的问题**，第 3 级判定它们的方式是
「去掉 `*` 后取最长前缀当目录，主会话下该目录存在即改写」。剩下 **4** 条就是 §6 那四条。

## 5. 复核：改写后重跑同一循环

> **本节读数在 2026-11-06 的 rebase 之后重新测过，`+12 出现 / +10 去重` 全部来自上游。**
> 本分支 rebase 到已发布的 `d1e4aeff` 时，上游三次 D17 复探（`9855534d` / `21bfc5fc` /
> `e1d47553`）往 `gap-copilot-cli.md` 里加了 10 条新的 `artifacts/d17/…` 尾巴、共 12 处引用，
> 并留下 **55 条相对引用**。冲突的并集解法是：**取上游全文，再对它重跑本文件的改写规则**
> （`resolve_union.py`，映射优先用 `final_map.json`，其次用「磁盘上只有一个会话目录有它」，
> 两者都不命中的一律**保持相对**并报出来）。因此下面所有数字都比 rebase 之前多 12 / 10，
> 而 §5 里那些**引号内的历史读数**（482 / 489 / 741 / 750）是 rebase 前那次运行的叙述性记录，
> 按 Rule 38 它们不可重算，只可引用，故保留原样。
>
> 并集的正确性有一条逐字节判据，不靠读：
>
> ```bash
> cd /home/user/workspace/01a1ae2c-6929-77cc-a0d6-cc000718b3ca
> # 把两个版本的绝对前缀都剥掉，剩下的必须是同一份字节
> git show origin/master:devdoc/competitor/gap-copilot-cli.md | sed -E 's#/home/user/\.gopi/sessions/[A-Za-z0-9_.:-]+/##g' | sha256sum
> sed -E 's#/home/user/\.gopi/sessions/[A-Za-z0-9_.:-]+/##g' devdoc/competitor/gap-copilot-cli.md | sha256sum
> ```
>
> ```
> f4a1ee2fe90d3ec92a569bd2d6a7b408c7acd3b38506e5f80c3c5a589e4b14f9  -
> f4a1ee2fe90d3ec92a569bd2d6a7b408c7acd3b38506e5f80c3c5a589e4b14f9  -
> ```
>
> 相同 ⇒ 冲突解决**只换了前缀**，上游正文一个字都没被改写（`resolve_union.py` 报
> `rewrites: {'map': 45, 'disk-unique': 10, 'left': 0}`：45 处走 `final_map.json`、
> 10 处按「磁盘唯一」解出、0 处被迫保持相对）。

`bash ~/.gopi/sessions/01a1ae2c-6929-77cc-a0d6-cc000718b3ca/census.sh`（从仓库根执行；
它是 `census.py` 的一行 wrapper，只有一个实现）：

```
absolute_refs occurrences=762 reachable=762 unreachable=0 | distinct=499 reachable=499 unreachable=0
tilde_refs    occurrences=0   reachable=0   unreachable=0 | distinct=0   reachable=0   unreachable=0
relative_refs occurrences=4   reachable=0   unreachable=4 | distinct=4   reachable=0   unreachable=4
ALL occurrences=766 reachable=762 unreachable=4 | distinct=503 reachable=499
UNREACHABLE [relative] gap-kilo-code.md :: artifacts/e3/D02-freshhome-ls.txt
UNREACHABLE [relative] gap-pi.md :: artifacts/conversations/devices/envs/files/locations/login/logs/mcp/models/ports/proxy-marker
UNREACHABLE [relative] gap-qoder.md :: artifacts/qoder/e3-Dxx.txt
UNREACHABLE [relative] gap-qwen-code.md :: artifacts/gap/e2/x-docs_design_2026-08-23-approval-mode-contract.md
```

**先前的 `census.sh` 有三个独立的缺陷，全部已修，留在这里因为每一个都会让读数寂静地错：**

1. **tilde 与 absolute 用了同一个正则。** 于是 9 条 `~/.gopi/sessions/<session-id>/` 被当成绝对引用，
   `test -e` 拿到一个含 `~` 的字符串（shell 不展开它）—— 全部判不可达。
2. **`test -e` 不做 `~` 展开。** 任何以 `~` 开头的引用都必须先展开再判，否则"存在"与
   "不存在"无从区分。
3. **只报次数，不报去重**，而两种口径在不同段落里被当成同一件事引用。

修好之后一个事实浮出来：**那 9 条 `~/.gopi/sessions/<session-id>/` 从来没有被规范化过** ——
它们以原文形式被 `git add` 进了暂存区（`til_occ=9`），本次修复才把它们变成绝对形式
（`482 → 489`）。

**必须说清的一件事：那个「482 绝对 / 9 tilde」的中间态，现在从任何 ref 都取不回来了。**
它只存在于**提交前的 index** 里，`git add` 之后、`git commit` 之前；本提交一落地，index
就等于 worktree，`git show :<path>` 返回的已经是修好的版本。所以：

```bash
cd /home/user/workspace/01a1ae2c-6929-77cc-a0d6-cc000718b3ca
# `[~]` 不是洁癖：写成 '~/.gopi/sessions' 的 grep，那个 `~` 会在命令到达
# shell、甚至到达 heredoc 之前就被展开成 /home/user，于是它匹配的是【绝对】路径，
# 报出一个假的大数（实测在 gap-deep-code.md 上报 20，而真实 tilde 引用是 2）。
git show :devdoc/competitor/gap-deep-code.md    | grep -c '[~]/.gopi/sessions'   # 0（已修好）
git show HEAD:devdoc/competitor/gap-deep-code.md  | grep -c '[~]/.gopi/sessions' # 0
git show HEAD^:devdoc/competitor/gap-deep-code.md | grep -c '[~]/.gopi/sessions' # 2
```

**`HEAD^` 有 9 条 tilde（分布在 7 个文件），`HEAD` 有 0 条**，但**这救不回中间态**：
`HEAD^` 是**修复前的基线**，它同时带着 **643 条相对引用**；中间态是 **482 绝对 + 4 相对 +
9 tilde** 这个组合。"绝对改完了、相对只剩 4、tilde 还剩 9"三件事同时成立的那个状态，只
活过一次 `git add` 到 `git commit` 之间的 index，**没有任何 ref 记录它**。所以 §5 那张表
`index` 行的**每一列**都能从别处单独看到，**同一行同时成立却不能** —— 它是叙述性读数，
引用时必须连同"它只被观察到一次"一起说。 下面那个 `index` 行是**当时那一刻的读数**，
按 Rule 38 的口径它属于叙述性数据（一次运行的观察），不是可重算的普查数字 —— 引用它时
必须连同"它已不可复现"一起说。重跑 `three-way.py` 现在得到的是 `index = worktree`
（都是 762/499/0），这是**正确的**，不是脚本坏了：

```bash
python3 ~/.gopi/sessions/01a1ae2c-6929-77cc-a0d6-cc000718b3ca/three-way.py
# point      abs_occ  abs_ok  abs_dist abs_dok  til_occ  rel_occ  rel_dist
# head           102     100        65      63        9      643       334
# index          762     762       499     499        0        4         4   <- 提交后 = worktree
# worktree       762     762       499     499        0        4         4
```

**当时（提交前）那次运行读到的 `index` 行是：**

```
# index          741     741       482     482        9        4         4
```

`741 − 750 = 9`、`482 − 489 = 7`、`til_occ 9 → 0`：差值正是那 9 条被规范化的 tilde 引用
（7 条不同的路径，其中 2 条出现两次）。**这一行是推算不出来的，只能引用**——它是一个
已经消失的中间态的读数。

脚本正文可以原样粘贴复跑：

```bash
shopt -s nullglob
ABS=$(grep -ohP '(?<![A-Za-z0-9_./-])/home/user/\.gopi/sessions/[A-Za-z0-9_./*-]*' devdoc/competitor/gap-*.md | sed 's/[.,;:)]*$//' | sort -u)
REL=$(grep -ohP '(?<![/A-Za-z0-9_.-])artifacts/[A-Za-z0-9_./*-]*' devdoc/competitor/gap-*.md | sed 's/[.,;:)]*$//' | sort -u)
reach(){ local p=$1 a; if [[ $p == *'*'* ]]; then a=( $p ); [ ${#a[@]} -gt 0 ]; else [ -e "$p" ]; fi; }
for p in $ABS; do reach "$p" || echo "UNREACHABLE $p"; done
for p in $REL; do reach "$p" || echo "UNREACHABLE $p"; done
```

**三个口径，别混**：

| 口径 | 数 | 读法 |
|---|---|---|
| 去重绝对 token | **499**（492 文件/目录 + 7 通配） | 499/499 可达；7/7 通配展开非空 |
| 绝对引用出现次数 | **762** 次，可达 **762**，不可达 **0** | 同一路径出现几次算几次 |
| `(文档, 路径)` 引用点 | 484 点（`resolution.tsv`，改写 480，残留 4） | §4 那张表 |

第三条的 484 与第一条的 499 差 15：**同一个文件被两份文档引用会被点计数两次**（`(文档, 路径)`
是点，路径是去重），而 499 里还含 7 条通配。它们回答不同问题，不是同一件事的两种写法。
（`484` 这一行来自 `resolution.tsv`，它是**rebase 之前**那次解析的结果 —— 上游 D17 那 10 条
尾巴不在它的 484 里。所以第三条与第一、二条之间的差值里含着两个原因：点/去重的口径差，
加上 rebase 前/后的时间差；要一个当下的点数，用 `resolution.tsv` 的同一套规则在现在的树上重跑。）

**基线对照**（HEAD 内容从 `git archive` 取，在同一个仓库根下跑同一段循环）：

```bash
cd /home/user/workspace/01a1ae2c-6929-77cc-a0d6-cc000718b3ca
B=~/.gopi/sessions/01a1ae2c-6929-77cc-a0d6-cc000718b3ca/baseline
mkdir -p "$B" && git archive c96c437f devdoc/competitor | tar -x -C "$B"
python3 ~/.gopi/sessions/01a1ae2c-6929-77cc-a0d6-cc000718b3ca/readings.py
```

```
point      abs_occ  abs_ok  abs_dist abs_dok  til_occ  rel_occ  rel_ok  rel_dist  rel_dok
head           102     100        65      63        9      643       1       334        1
index          741     741       482     482        9        4       0         4        0
worktree       762     762       499     499        0        4       0         4        0
```

（`index` = 本轮修复**提交之前**的暂存态读数。**它已不可从任何 ref 取回**：那个状态只
活在提交前的 index 里，本提交落地后 `git show :<path>` 与 `HEAD^` 都返回修好的版本，两个
都是 0 条 tilde 引用（见 §5 前面的说明）。这一行只能引用，不能推算。`rel_ok` 在仓库根是 1 而
在隔离副本里是 0 —— **仓库根确实有一个受控的 `artifacts/` 目录**，那是唯一一条可达的相对
引用，也正是 §1 说的"只有 1 条存在，而它正是 `artifacts/` 自己"。从别的 cwd 解析时它会变成 0，
所以引用这个数必须连同 cwd 一起说。）

| | 改写前（HEAD `c96c437f`） | 改写后（本 worktree） | 变化 |
|---|---|---|---|
| 绝对引用 token（去重） | 65（可达 63） | **499**（可达 499） | +434 |
| 绝对引用出现次数 | 102（可达 100） | **762**（可达 **762**） | 不可达 2 → **0** |
| `~/.gopi/sessions/<session-id>/` 出现次数 | 9 | **0** | −9（规范化成绝对形式） |
| 相对引用 token（去重） | 334 | **4** | −330 |
| 相对引用出现次数 | 643（可达 1） | **4**（可达 0） | −639 |

即：**643 次相对出现里的 639 次被迁成绝对路径，剩下 4 次就是 §6 那四条**。
`index` 那一行是这条改写的**中途快照**：绝对 482 条已可达，但 9 条 `~` 形式还留在文里 ——
所以"可达 0 个绝对引用不可达"在当时成立，"没有未规范化的引用"**并不成立**。

## 6. 无法恢复的 4 条（原样保留，不改）

| # | 文档 | 原引用（未改） | 为什么不能改 |
|---|---|---|---|
| 1 | `gap-kilo-code.md` | `artifacts/e3/D02-freshhome-ls.txt` | **文件不存在**。`/home/user/.gopi/sessions/01a1acc7-7cb8-73f0-9dab-d47e0063a5f3/artifacts/e3/` 下 36 个文件逐条列过，没有它（同目录只有 `D02-setup.txt`，内容就是那条 `4 path(s) provisioned` 读数）。原文档说的 `ls -A <freshhome>/.gopi` 没有单独落盘 —— 补前缀会造出一个假引用。 |
| 2 | `gap-pi.md` | `artifacts/conversations/devices/envs/files/locations/login/logs/mcp/models/ports/proxy-marker` | **不是路径，是一串被 grep 出来的目录名**。原文语境是 `/home/user/.gopi/sessions/01a1acc7-7eda-77b0-88e3-5cbf79246a52/artifacts/pi/paths.txt` 里的命中串（`conversations` `devices` `envs` `files` `locations` `login` `logs` `mcp` `models` `ports` `proxy-marker` 共 11 个目录名并列），文档把词表当路径引用了。补前缀等于发明一个不存在的文件。 |
| 3 | `gap-qoder.md` | `artifacts/qoder/e3-Dxx.txt` | **占位名**。原文是注释「十七维全部有 E3（本 run 亲手跑、exit=0、输出在 `artifacts/qoder/e3-Dxx.txt`）」，`Dxx` 是通配写法；真实文件是 `e3-D01.txt` 到 `e3-D17.txt` 共 17 个，同一段已给 `ls /home/user/.gopi/sessions/01a1acc7-7f43-78e6-82e3-f4f13a1480ca/artifacts/qoder/e3-D*.txt` 的取回命令（该通配已改写，展开 20 个文件）。 |
| 4 | `gap-qwen-code.md` | `artifacts/gap/e2/x-docs_design_2026-08-23-approval-mode-contract.md` | **抓取失败的产物**。原文第 93 行「方法论教训」段自己写明那次 `curl -sS -o <此路径> -w '%{http_code}'` 拿到的不是 200，文件从未写出。补前缀会让它看起来像存在 —— 这条恰恰是文档自己在记录的失败。 |

判定原则一句话：**只补真实存在的那一层前缀；补不出真实路径的，宁可留相对、留证据。**
把一条不存在的路径写成绝对形式，比留一条相对路径更坏 —— 后者读者能看出"这是按仓库根写的"，
前者看起来像已经验证过。

## 6.5 最强的一条"没改坏"证据：路径名集合逐字节相同

改写只换前缀，**不应动任何路径名**。可测：把 `artifacts/<tail>` 尾巴去重排序后取 sha256，
HEAD 与改写后应当完全一致。

```bash
cd /home/user/workspace/01a1ae2c-6929-77cc-a0d6-cc000718b3ca
B=~/.gopi/sessions/01a1ae2c-6929-77cc-a0d6-cc000718b3ca
git grep -oh 'artifacts/[A-Za-z0-9_./-]*' c96c437f -- 'devdoc/competitor/gap-*.md' \
  | sed 's/[.,;:)]*$//' | sort -u > "$B/tails-head.txt"
grep -oh 'artifacts/[A-Za-z0-9_./-]*' devdoc/competitor/gap-*.md \
  | sed 's/[.,;:)]*$//' | sort -u > "$B/tails-now.txt"
diff "$B/tails-head.txt" "$B/tails-now.txt" && echo IDENTICAL
sha256sum "$B/tails-head.txt" "$B/tails-now.txt"
```

实测（本 run，rebase **之前** 的树）：

```
head tails: 367   now tails: 367
IDENTICAL
83d56746ffc58e7328cb82bb416afe71ca3647aeb72f596f5c086be1438781fb  tails-head.txt
83d56746ffc58e7328cb82bb416afe71ca3647aeb72f596f5c086be1438781fb  tails-now.txt
```

**rebase 到 `d1e4aeff` 之后这组读数变了，变的原因只有上游那 10 条尾巴，不是改写规则变了：**

```bash
cd /home/user/workspace/01a1ae2c-6929-77cc-a0d6-cc000718b3ca
git grep -oh 'artifacts/[A-Za-z0-9_./-]*' c96c437f -- 'devdoc/competitor/gap-*.md' | sed 's/[.,;:)]*$//' | sort -u > /tmp/t367.txt
grep -oh 'artifacts/[A-Za-z0-9_./-]*' devdoc/competitor/gap-*.md | sed 's/[.,;:)]*$//' | sort -u > /tmp/t377.txt
wc -l < /tmp/t367.txt < /tmp/t377.txt          # 367 / 377
comm -13 /tmp/t367.txt /tmp/t377.txt           # 只在现在的树里（= 上游 D17 那 10 条）
comm -23 /tmp/t367.txt /tmp/t377.txt           # 只在基线里 —— 空
sha256sum /tmp/t377.txt
git grep -oh 'artifacts/[A-Za-z0-9_./-]*' origin/master -- 'devdoc/competitor/gap-*.md' | sed 's/[.,;:)]*$//' | sort -u | sha256sum
```

```
367
377
artifacts/d17/
artifacts/d17/A1-submit.txt
artifacts/d17/A2-pid.txt
artifacts/d17/A2-readback.txt
artifacts/d17/A3-negative.txt
artifacts/d17/A4-lifecycle.txt
artifacts/d17/A5-real-ledger-lifecycle.txt
artifacts/d17/A6-isolation.txt
artifacts/d17/A7-list.txt
artifacts/d17/A8-why-sandbox.txt
e7956eca8ef5a9a23d55ca0ef3d9c691cdb3bc54d6f74eb6ab17152be85585f9  /tmp/t377.txt
e7956eca8ef5a9a23d55ca0ef3d9c691cdb3bc54d6f74eb6ab17152be85585f9  -
```

判据从"与基线 sha 相同"升级成**两条更强的**：

1. `comm -23` 为空 ⇒ 改写与 rebase 都没有**删掉或改写**任何一条基线尾巴；
2. 现在的尾巴集与 **`origin/master` 上同一组文件**的尾巴集 sha256 **完全相同** ⇒ 本分支
   在尾巴层面与上游逐字节一致，唯一的差别就是前缀。

上面那段 `367 = 367` 的读数保留原样：它是 rebase 前那次运行的叙述性记录（Rule 38），
不可重算，但正确。

**`367 = 367` 且 sha256 相同**，所以：改写没有新增、删除或改写任何一条路径名，只把前缀
从"相对仓库根"换成"绝对会话目录"（外加撤销 3 处误加的绝对化、展开 1 处截断的会话 id）。
§1 里那条 `367 / 1` 的命令因此在改写后仍然原样成立 —— 它数的是尾巴，而尾巴没动。

## 7. 验收：每份文档的 D17 行按引用 `test -e`

```bash
bash ~/.gopi/sessions/01a1ae2c-6929-77cc-a0d6-cc000718b3ca/verify_d17.sh
```

```
D17 gap-astrbot.md: NO-PATH-REF-IN-ROW
D17 gap-cherry-studio.md: ok (1 refs test -e = true)
D17 gap-claude-code.md: ok (1 refs test -e = true)
D17 gap-cline.md: ok (1 refs test -e = true)
D17 gap-codex.md: ok (1 refs test -e = true)
D17 gap-copilot-cli.md: ok (13 refs test -e = true)
D17 gap-crush.md: ok (1 refs test -e = true)
D17 gap-deep-code.md: ok (2 refs test -e = true)
D17 gap-deepseek-tui.md: ok (3 refs test -e = true)
D17 gap-github-copilot.md: ok (1 refs test -e = true)
D17 gap-hermes.md: ok (2 refs test -e = true)
D17 gap-kilo-code.md: ok (3 refs test -e = true)
D17 gap-langcli.md: ok (1 refs test -e = true)
D17 gap-lobehub.md: ok (1 refs test -e = true)
D17 gap-nanobot.md: ok (1 refs test -e = true)
D17 gap-oh-my-pi.md: ok (2 refs test -e = true)
D17 gap-openclaw.md: ok (2 refs test -e = true)
D17 gap-opencode.md: ok (1 refs test -e = true)
D17 gap-pi.md: ok (1 refs test -e = true)
D17 gap-qoder.md: ok (1 refs test -e = true)
D17 gap-qwen-code.md: ok (1 refs test -e = true)
D17 gap-reasonix.md: ok (1 refs test -e = true)
D17 gap-workbuddy.md: ok (1 refs test -e = true)
docs-with-a-failing-or-missing-D17-ref: 1
```

**22 份 ok，1 份 `NO-PATH-REF-IN-ROW`。** `gap-astrbot.md` 的 D17 行本来就没有路径引用
（它的证据写成 `gopi-self-b.md:330`，指向仓库内文档，不是会话捕获），所以没有可 `test -e`
的对象 —— 是原样，不是本 run 改坏的；计数行 `docs-with-a-failing-or-missing-D17-ref: 1`
数的就是它。

单点核对（`gap-qwen-code.md` 的 D17 行，证据是 `e3-d01-d17.txt`）：

```bash
grep -m1 '^| D17 ' devdoc/competitor/gap-qwen-code.md | grep -oP '/home/user/\.gopi/sessions/[A-Za-z0-9_./-]*'
# /home/user/.gopi/sessions/01a1acc7-7f8e-7274-a7a7-2294f220ce45/artifacts/gap/e3/e3-d01-d17.txt
test -e /home/user/.gopi/sessions/01a1acc7-7f8e-7274-a7a7-2294f220ce45/artifacts/gap/e3/e3-d01-d17.txt; echo $?
# 0
```

## 8. 这次没有做的事（诚实边界）

1. **没有把捕获复制进仓库。** Rule 39 管受控树里的 blob，而这些是几百字节到几 MB 的
   txt/png/gif；路径可达之后 `test -e` / `cat` 就是复核手段，复制只有成本没有增益。
2. **没有动任何 E3 判定。** 只改引用；`partial`/`full` 一个都没碰。纯文本 diff
   **23 files changed, 453 insertions(+), 453 deletions(-)** —— 增删行数相等，每一行都是
   同一行的路径前缀变化，没有内容改写。
3. **没有解决"下次还会不会犯"。** 病根是「证据落在 session 目录、文档却按仓库根写相对
   路径」。本文件把已有 480 点修到可判定；要防复发，得让写 gap 文档的那一步**直接输出
   绝对会话路径**（另一条任务，不在本 run 范围）。
4. **归因是启发式，不是权威。** `ambiguous` 那 168 点按「该文档唯一解最多的会话」归因。
   对同一份文档等价（一个 run 的捕获都在一个会话里），但若某份文档的捕获确实横跨两个 run，
   可能指错。判据在 §3 第 2 级，复核者可逐份推翻 —— 只改路径、不改结论，推翻的代价是一行。
5. **`gap-copilot-cli.md` 是引用最密的一份**（**42 个绝对点 / 94 次出现**，rebase 到
   `d1e4aeff` 之后重测；rebase 前是 32 / 82，多的 10 点 12 次是上游 D17 那批）。它的证据大多是
   `/home/user/.gopi/sessions/01a1acc7-7c1d-75c0-841f-3012ec049ff0/artifacts/gap/scratch-prefix`
   下的自装二进制与临时 profile，已全部指到 `01a1acc7-7c1d-75c0-841f-3012ec049ff0` 会话；
   那些会话目录仍在，可逐条复核。该前缀下的完整清单用
   `ls /home/user/.gopi/sessions/01a1acc7-7c1d-75c0-841f-3012ec049ff0/artifacts/gap/scratch-prefix`
   取回。
