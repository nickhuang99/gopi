<!-- doc-class: normative -->
# 文档里的 rev 引用必须在本仓库可解析 —— 一枚悬挂对象与一个被吞掉的 exit 128

Task: 01a1355d-d0d3-7a1c-8c1b-ebb4325e74b7 · ground `task/01a1355dd0d37a1c8c1bebb4325e74b7`
· 基线 `e63a80a`（不是 master）· 主机 host-b（client）

## 结论（一句话）

`devdoc/scratch-gc-flight-join.md` 的"观察到的第二处"把 `installFileWatcher` 的 join 修复
记成 commit `b2366a62a` —— 那个 rev **在本仓库根本不解**，而它旁边那行
`git merge-base --is-ancestor … && echo yes || echo no` → `no` 把 **"解不开（exit 128）"**
伪装成了 **"不是祖先（exit 1）"**；真正的落地点是 master 上的 **`ba7592966`**
（"both stops JOIN"，本文档本身就是那一枚写下的）。

## 两条判据（可复用）

### 1. `&& echo yes || echo no` 会把 exit 128 洗成 no —— 判据要看 `$?`

两种完全不同的结局印出同一个 `no`：

| 结局 | git exit | 典型输出 | 含义 |
|---|---|---|---|
| 不是祖先 | **1** | （无输出） | 真查过，答案是"不" |
| rev 不存在 | **128** | `fatal: Not a valid object name` | **没查成** |

实测（本仓库）：

```
$ git merge-base --is-ancestor b2366a62a e7f3bf2 && echo yes || echo no
fatal: Not a valid object name b2366a62a
no
compound-exit=0                      ← 复合命令把 128 吞成了成功
$ git merge-base --is-ancestor b2366a62a e7f3bf2 2>/dev/null ; echo "merge-base-exit=$?"
merge-base-exit=128                  ← 真实的判据在这里
```

**配方**：在文档里记一条"祖先关系"结论时，记 `git merge-base --is-ancestor <rev> <base> ; echo $?`
的**退出码**，别记 `&& … || …` 的**文字**。前者区分"不"与"没查成"，后者把两者压成同一枚 `no`。
（同族：Rule 24 的"用 `/proc/<pid>/cgroup` 判脱离、别用 PPID"，都是"判据必须能分辨两种结局"。）

### 2. 一枚 rev 在别的机器上"是 commit"不等于读者能复核它

`b2366a62a` 的真实身份（实测，2026-10-14）：

| 探测 | host-b（读者所在机） | peer1（对端） |
|---|---|---|
| `git cat-file -t b2366a62a` | `fatal` + **exit 128** | `commit` |
| `git rev-list --all \| grep -c '^b2366a62a'` | **0** | **0** |
| `git for-each-ref --contains b2366a62a \| wc -l` | — | **0** |
| `git cat-file --batch-all-objects … \| grep -c '^b2366a62a'` | **0** | **1** |

即：**对象只存在于 peer1 的对端对象库，而且没有任何 ref 指向它**（悬挂对象）。
"无 ref 可达 ⇒ 不随 fetch/push 过桥 ⇒ 别的克隆永远看不到"这一句是**推断**；
实测到的只有上表四个计数。

**判据**：文档给读者的每一条 rev 引用，都要能在**读者手上那个仓库**里
`git cat-file -t <rev>` 解成 `commit`。只在一台机器上成立的引用 = 不可复核的引用。
复核命令（本仓库、零依赖）：

```
$ for r in $(grep -oE '\b[0-9a-f]{7,40}\b' devdoc/<doc>.md | sort -u); do \
    printf '%-42s %s\n' "$r" "$(git cat-file -t "$r" 2>/dev/null || echo UNRESOLVABLE)"; done
```

**注意它会把 task id 片段（`01a1343d…`）和 `git patch-id` 前缀一并卷进来** —— 那些不是
rev 引用，读输出时按"被当作 rev 引用写出来的那一串"筛。

## 本例的完整事实（避免后来者重查）

- **原先引的 rev**：`b2366a62a9351ce28241b637ab4fd34a3e8dc49f`
  `fix(worker): the file watcher gets an exit channel, and its installer joins it`
  （Tue Oct 13 15:32:20 2026 +0800，作者 qingzhe huang，父提交 `3544b690c6e86b5bc2647ca4c2a86b4ce94434b9`）。
  peer1 上对象完好（`cat-file -t` → `commit`），但 `rev-list --all` 命中 0、`for-each-ref --contains` 0 个 ref。
- **真正落地的**：`ba759296639d12f4544ee197721dfeb34eab5f6c`
  `fix(cmd/agent): both stops JOIN — the watcher's uninstall and the resident sweep's flight`
  （Tue Oct 13 16:41:20 2026 +0800）。本仓库 `cat-file -t` → `commit`；
  `merge-base --is-ancestor ba7592966 refs/remotes/origin/master` → **yes**；
  `… e7f3bf2` → **no**（它比 `e7f3bf2` 晚，走的是 `e7f3bf2` → `b888461f2` → `ba7592966`）。
- **它与 `b2366a62a` 不是同一个改动**：同一文件 diff 的稳定
  `git patch-id --stable` 不同（`a2362019…` vs `72c49a7d…`）。同形，不同一枚。
- **同段的"已提行/附件"在本机也不可复核**：task 行
  `01a1343d-d9c2-793c-a5e2-29538e340c09` 在 host-b 账本里没有
  （`gopi task show …` → `no task with task_id=…`）；附件
  `~/.gopi/sessions/01a13431-968c-7602-b48f-d2daf1aa6cc2/artifacts/bug-installfilewatcher-no-join.md`
  在 host-b 不存在，在 peer1 也**不存在**。该行与附件产生于哪个 ground 未确认 —— 只记实测。

## 哪些门禁抓不到它（诚实边界）

- **`gopi tool doccheck` 在本例的基线上 exit 0**：它的 anchor 面按"记录文本 + 行号"
  冻结（`devdoc/scratch-gc-flight-join.md` 在 `internal/docclass/testdata/anchor-number-baseline.tsv`
  里是 `number-drifted … first-frozen at 0b3c1de29`），**没有任何一面检查"引用的 rev 能不能解"**。
- 所以本文件记的两条判据目前**只有人读**，**没有机器门禁** —— 谁要把它变成门禁，
  那是新工作（Rule 35：先问"没有它会坏什么"，并接受"多一个门禁就是一个新依赖"的成本）。

## 取证

- 本仓库侧：`~/.gopi/sessions/01a1355d-d0d3-7a1c-8c1b-ebb4325e74b7/artifacts/evidence-b2366a62a-unresolvable.md`
- peer1 侧（ssh 只读探测）：同目录 `evidence-peer-peer1-b2366a62a.md`
- 改动：`devdoc/scratch-gc-flight-join.md`（Rule 4，surgical，未改"观察到的第二处"的结论段）
