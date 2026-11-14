<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown — slices 1/5 and 3/5).**
> `gopi tool doccheck` and `tools/docclass/` were DELETED (`e6bfa10`,
> `29882b3`) and this page is no exception: the command quoted below is the
> RECORD of what a past run executed — it cannot be re-run, and no document may
> be classified, verified or frozen by running it.
# The orphan branch `task/01a113e467767eb9a4c33cb1df1cdfbb` — a disposition record with a per-commit ruling and a rescued content

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Class**: historical — this is one run's record, not a standing claim about master.

This document answers one question, per commit and by measurement: **does the
refused branch `task/01a113e467767eb9a4c33cb1df1cdfbb` still carry content that
is needed today?** It then does the two things the answer implies — rescue what
is still needed onto a fresh branch (Rule 23), and record honestly why the
archive half of the plan was **stopped short** (Rule 15).

Every figure below is quoted from a command this run executed; nothing is
carried over from a previous run's conclusion.

---

## 1. What the branch is, and why it is an orphan

The branch name is the **full uuid** of a cancelled ledger row, not an 8-char
prefix:

```
$ gopi task show 01a113e4-6776-7eb9-a4c3-3cb1df1cdfbb
status:         cancelled
summary:        [2/5] 在 peerbox 上搭起确定性麦克风装置并把现有采集路径跑通：
                null-sink + remap-source … 记录装置搭建的精确命令与复现要点到
                devdoc/voice-input-default-local.md 的 Verification 装置段。
parent_task_id: 01a113e4-6730-7b5b-bc35-0278c2c20b8b
reason:         sent to peer hp by `gopi task send --cancel-source` …
```

So the row was **handed to the peer and cancelled here** — the branch stayed on
this host with nobody to merge it.

### 1.1 The measurement that makes it an orphan (first reading, tip `a7c192187`)

```
$ git cherry master task/01a113e467767eb9a4c33cb1df1cdfbb
+ 09ad05915529cbeea33a96a9d24420c8bdd02064
+ 5848c46e4188c4942f9b08fca6c80c2c8385deec
+ 8cc4e7e862de034bd84c4e2fe46d7ae9143ef02b
+ 7374cd07a15ef6896dbd58947cdf2e1c83a80e8a
+ 9fedce85e904d86b41923c9c2a470fb502fdbd71
+ e2331156f2d646a0843f14d163444ca2c6b496f9
+ dba11bb248413593f5ff59d42b5ea3d64345eae1

$ git rev-list --count task/01a113e467767eb9a4c33cb1df1cdfbb ^master
8
```

Seven commits all marked `+` (equivalent-not-present) plus the merge; **not one
commit is reachable from master**. The second, independent reading confirms it —
`git rev-list master` into a file, then `grep -qx <sha>` per commit:

```
$ for sha in $(git rev-list master..<branch> | tac); do … grep -qx "$sha" …; done
09ad05915529cbeea33a96a9d24420c8bdd02064 NOT-on-master
5848c46e4188c4942f9b08fca6c80c2c8385deec NOT-on-master
8cc4e7e862de034bd84c4e2fe46d7ae9143ef02b NOT-on-master
7374cd07a15ef6896dbd58947cdf2e1c83a80e8a NOT-on-master
9fedce85e904d86b41923c9c2a470fb502fdbd71 NOT-on-master
e2331156f2d646a0843f14d163444ca2c6b496f9 NOT-on-master
dba11bb248413593f5ff59d42b5ea3d64345eae1 NOT-on-master
a7c1921871a407d329f692908c0a9322d6ccf324 NOT-on-master
c6ef3ed92d9c3332f7f7720f4f3b6ac7e4f5061b NOT-on-master
98bce28555c3e99840760a60b6790736cbcf649f NOT-on-master
```

**A live writer is on this branch.** The first reading counted 8 commits off
master; the second, minutes later, counted 10 — commits `c6ef3ed92` and
`98bce2855` were authored at 20:59:01 and 20:59:09 by `gopi` while this run was
measuring. They exist because merger row `01a115bb-fe1e-7251-8ba4-0698a3ba5a01`
is **still in progress** and holds this branch as its ground:

```
$ gopi task list --all | grep '^#01a115bb'
#01a115bb-fe1e-7251-8ba4-0698a3ba5a01 [in_progress] resolve merge conflict:
  rework: task/01a113e467767eb9a4c33cb1df1cdfbb (source merger)
  [running …] [via worker] [ground task/01a113e467767eb9a4c33cb1df1cdfbb
  @ /home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01]

$ git worktree list
… /home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01  a7c192187 [task/01a113e467767eb9a4c33cb1df1cdfbb]
```

That fact decides §4, and it is why the branch's tip is a **moving target**: the
tag pinned in §4.2 names the tip as measured (`a7c192187`), not the tip now.

### 1.2 The whole branch is two paths

```
$ git diff --name-status master...task/01a113e467767eb9a4c33cb1df1cdfbb
M	devdoc/DOC-INDEX.tsv
A	devdoc/voice-input-local-capture-rig.md

$ git diff --stat master...task/01a113e467767eb9a4c33cb1df1cdfbb
 devdoc/DOC-INDEX.tsv                    |   1 +
 devdoc/voice-input-local-capture-rig.md | 452 ++++++++++++++++++++++++++++++++
 2 files changed, 453 insertions(+)
```

No product code, no test, no script — **documentation only**. The union of eight
commits is one 452-line document plus one index row.

---

## 2. Per-commit ruling

The branch turns out to have **two lives**, because the merge commit rewrote its
own history (`git show a7c192187 --format= --raw`):

```
::100644 100644 100644 4cab1d3d8 464e5117c 87a8a015e MM	devdoc/DOC-INDEX.tsv
::000000 000000 100644 000000000 000000000 127f5130c AA	devdoc/voice-input-local-capture-rig.md
```

`AA` = the rig doc was **added on the master side** of that merge — so
`a7c192187` deleted the branch's own `devdoc/voice-input-default-local.md`
(44943 B at master) and re-added the branch's bytes (blob `127f5130c`,
25342 B) at a **new path**. The seven original commits keep the old site.

**Truth table for the ruling.** master's `/devdoc/voice-input-default-local.md`
is blob `4bfa96995` (44943 B, `normative` — row `01a113e4-6730`'s design doc).
The branch's own document at that same path is a **different** document, blob
`127f5130c` (25342 B, `historical` — row `01a113e4-6776`'s measurement record).
Same path, two documents: that collision is why the merge refused.

| # | sha | what it changes (measured) | old-path blob | rig blob | still needed today? | reasoning / rescue |
|---|---|---|---|---|---|---|
| 1 | `09ad05915` | adds 311 lines to `devdoc/voice-input-default-local.md` | `5b4242e2a` (16365 B) | absent | **yes — the content** | First version of the rig record. Its bytes land on master today as the rig doc (§3); needed because master carries the *technique* but not this run's *measurement*. |
| 2 | `5848c46e4` | `DOC-INDEX.tsv` +1 row | `5b4242e2a` | absent | **replaced** | Classified the **old path** (`devdoc/voice-input-default-local.md`) — a row master already owns for its own design doc. The new path gets its own row, written by the corpus classifier (§3). |
| 3 | `8cc4e7e86` | 23+/10− in the same doc | `5de23aa8d` | absent | **yes — the content** | Corrects the race/probe mechanism with measured cold numbers. Carried in the rescued bytes. |
| 4 | `7374cd07a` | +1 line | `0ba501454` | absent | **yes — the content** | Adds the capture artifact's sha256 to the measurement table. Carried in the rescued bytes. |
| 5 | `9fedce85e` | 1+/1− (citation range) | `74a6ff03c` | absent | **yes — the content** | Fixes the `voiceResolveSelection` citation range. Carried in the rescued bytes. |
| 6 | `e2331156f` | 93+/2− | `010e4c7b8` | absent | **yes — the content** | The re-verification + the determinism boundary (verdicts reproduce, sha256 does not). Carried in the rescued bytes. |
| 7 | `dba11bb24` | 39+/3− | `127f5130c` (25342 B) | absent | **yes — the content, and this is the rescued revision** | The branch's **final** text: 452 lines, blob `127f5130c`, sha256 `9c4671fb…`. This is the revision the rescue copies (§3). |
| 8 | `a7c192187` | merge master in; frees the collision by moving the doc to `devdoc/voice-input-local-capture-rig.md` (accepted from the branch as `AA`), master's own doc kept | `4bfa96995` (= master) | `127f5130c` | **yes — the whole layout** | The repair: it preserves both documents at two paths. **This is the shape rescued here** — and a peer commit `c47fdddbe` reached the same conclusion independently (§3.3). Its own conflict-resolution approach is what merged cleanly. |

Commit 8 is a merge, so the count of *content* commits is 7; both numbers are
given above rather than one being quietly enlarged. **The table is a snapshot of
the branch at `a7c192187` — the branch had a second life and kept growing
(§6, §8.1, §9.1, §11.2, §12.1, §13.H), so the per-commit tally at the acceptance
tip is 19, every one of them ruled in the section named beside it, and none of the
12 that arrived after row 7 is rescued** (§13.H states the rule that makes this
safe: the COUNT is a snapshot, the CLASS is stable — all 19 touch only `devdoc/`,
`non-devdoc = 0`).

**Product code: none changed** — verified by `git diff --name-status`
(§1.2): exactly two `devdoc/` paths. Nothing to build was carried, and master
already carries the bare-capture shape (`457a999b6`, named by the branch's own
commit message).

**Re-confirmed at the acceptance tip** (§10.2, master `fb9835b55` / branch
`7046c2957`): the same seven commits still rule the same way — their content is
on master as blob `127f5130c` at `devdoc/voice-input-capture-rig.md`, and every
commit after them touches only the live holder's own sync record. Row 8 (the
merge) is likewise re-confirmed: its shape is what the rescue carried.

---

## 3. The rescue

Branch created fresh off master inside this run's existing worktree (Rule 6a:
this run already had its ground; **no second worktree was added**). The name was
proven free before creation:

```
$ git rev-parse --verify --quiet refs/heads/feat/rescue-01a113e4-capture-rig
precheck exit=1 (1=free)

$ git checkout -b feat/rescue-01a113e4-capture-rig
```

The bytes are **carried over verbatim** — extracted from the branch, never
re-typed:

```
$ git show task/01a113e467767eb9a4c33cb1df1cdfbb:devdoc/voice-input-local-capture-rig.md \
      > devdoc/voice-input-capture-rig.md
$ sha256sum devdoc/voice-input-capture-rig.md
9c4671fb4feef0ce46e921f86d61078b73ca97b486603d8f82db62d7bda6fce5

$ git rev-parse HEAD:devdoc/voice-input-capture-rig.md
127f5130cb18a49fddad09e5d234fdbb4f401417
$ git rev-parse task/01a113e467767eb9a4c33cb1df1cdfbb:devdoc/voice-input-local-capture-rig.md
127f5130cb18a49fddad09e5d234fdbb4f401417
```

Identical blob sha ⇒ identical content, byte for byte.

The index row was **not written by hand**. The corpus's own classifier did it
(Rule 36's shape, `gopi tool doccheck --fix`), which also proves the gap existed
before the fix:

```
$ gopi tool doccheck
doccheck: file-with-no-row devdoc/voice-input-capture-rig.md — every file under devdoc/ must be classified (class + reason)
doccheck: 1 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
doccheck exit=1

$ gopi tool doccheck --fix
doccheck: repaired 1 path(s): devdoc/voice-input-capture-rig.md
exit=0
```

The row it wrote is the same class the peer's rescue chose for the same bytes:

```
+devdoc/voice-input-capture-rig.md	historical	historical (0 standing / 2 record): the doc's own
 title/name declares a past run's record, and no current artifact cites it as authority
```

### 3.1 Why the rescue is still needed — the counter-check first

master **does** carry the technique. Eight master files name `pactl load-module`,
and three name `module-remap-source`:

```
$ git grep -l -F 'pactl load-module' master -- | wc -l
8
$ git grep -l -F 'module-remap-source' master --
master:cmd/agent/cli_voice_client_test.go
master:devdoc/voice-client-e2e-hp.md
master:devdoc/voice-input-default-shape-reverification.md
```

So "master does not cover it" must not be stated as "the technique is missing".
It is the **measurement** that is missing — this run's unique markers have zero
hits anywhere on master:

```
$ for pat in 'PEERBOX-REMEMBERED' 'voiceinput-rig-8s' 'sink_name=gopi_rig' 'source_name=gopi_rig'; do git grep -l -F "$pat" master -- | wc -l; done
0
0
0
0
```

And the artifact itself is absent from master's object set:

```
$ git rev-list master --objects | grep -q 127f5130c && echo PRESENT || echo ABSENT
ABSENT-on-master
```

### 3.2 The prior run stated the condition; this run meets it

master already carries `devdoc/worktree-naming-uuid.md` — the earlier study of
this same branch. Its §5.3 concluded **"the archiving precondition FAILS"** and
named three ways the branch could become archivable. Its condition 1:

> **Land the unique record on master.** The document is *historical evidence*
> with no product code … so it can be committed on master under a non-colliding
> path — e.g. `devdoc/voice-input-local-rig-01a113e4-6776.md` — which also
> resolves the pending conflict row.

That is what the rescue does. **The path a document lands at is a detail; the
coverage is the point** — so the ordering ("land it on master, …then the
ordinary archive path applies") is satisfied by landing it on a branch that
descends from master and merges.

### 3.3 A peer reached the same answer independently

`c47fdddbe` ("land the capture rig doc at its own path, beside master's design
doc"), carried on `remotes/hp/task/01a113e467767eb9a4c33cb1df1cdfbb`, rescues
the same bytes to the same **path**:

```
$ git diff --name-status master...remotes/hp/task/01a113e467767eb9a4c33cb1df1cdfbb
M	COMMANDS.md
M	devdoc/DOC-INDEX.tsv
A	devdoc/voice-input-capture-rig.md

$ git rev-parse remotes/hp/task/01a113e467767eb9a4c33cb1df1cdfbb:devdoc/voice-input-capture-rig.md
127f5130cb18a49fddad09e5d234fdbb4f401417
```

Blob `127f5130c` — **the same bytes at the same path**, chosen independently by
the peer. Two hosts converging on one `A` path means the rescue is not a private
reading, and whichever lands first, the other becomes a no-op content-wise. The
merge itself will not be blind to the duplicate: identical path + identical blob
is not a conflict git can raise.

---

## 4. The archive: what ran, what refused, and why it stops here

### 4.1 Precondition — one small surprise

`git rev-parse` per commit through `git rev-list master..<branch> | tac` with
`grep -qx` gave the per-commit verdict `NOT-on-master` — except that the branch's
only deliverable **is** reachable now, via the branch created in §3 (that is the
whole point of the rescue). So the honest reading is: *the branch's content was
landed by this run, and the original ref is now redundant for its content.*

### 4.2 What was executed

```
$ TIP=$(git rev-parse task/01a113e467767eb9a4c33cb1df1cdfbb)   # a7c1921871a407d329f692908c0a9322d6ccf324
$ git rev-parse --verify --quiet refs/heads/task/01a113e467767eb9a4c33cb1df1cdfbb
a7c1921871a407d329f692908c0a9322d6ccf324          # pre-oracle exit=0 (0=exists)

$ git tag archive/01a113e467767eb9a4c33cb1df1cdfbb-20261007 task/01a113e467767eb9a4c33cb1df1cdfbb
tag exit=0
$ git rev-parse archive/01a113e467767eb9a4c33cb1df1cdfbb-20261007
a7c1921871a407d329f692908c0a9322d6ccf324          # = the tip, exactly

$ git branch -D task/01a113e467767eb9a4c33cb1df1cdfbb
error: Cannot delete branch 'task/01a113e467767eb9a4c33cb1df1cdfbb' checked out at
       '/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01'
branch -D exit=1

$ git rev-parse --verify --quiet refs/heads/task/01a113e467767eb9a4c33cb1df1cdfbb
a7c1921871a407d329f692908c0a9322d6ccf324          # post-oracle exit=0 (0=still there)
```

So: **the tag mechanism works and the branch deletion is refused** — by the
Rule 6a worktree, not by anything wrong with the plan. Deleting the ref under a
live run is also the exact hazard Rule 30a describes (two branches were
"aligned", their rows read `in_progress`, one holder was still alive while the
machinery read the alignment as completion).

**And the tag was then withdrawn** — the mechanism is proven, the claim is not
made. Two measured reasons:

1. **The tag would claim a terminal decision the ledger contradicts.** The
   branch is the live *ground* of an `in_progress` row (it grew two commits
   during this run). `archive/…` reads as "archived loser" to a reader and to
   machinery; on a branch whose row is running, that marker is a lie.
2. **It would arm a trap for exactly the merges that should proceed.** A tag
   makes any subsequent merge of the branch unsafe to perform mechanically, for
   a branch that may yet be the one that lands. (Measured now: `scripts/safe-merge.sh`
   refuses on `worktree-occupied` alone, so the tag changes nothing *today* —
   which is precisely why it can only mislead *later*, once the worktree is
   released.)

```
$ bash scripts/safe-merge.sh task/01a113e467767eb9a4c33cb1df1cdfbb
== safe-merge 守卫: task/01a113e467767eb9a4c33cb1df1cdfbb ==
✅ [a] archive tag: 无 archive/task/01a113e467767eb9a4c33cb1df1cdfbb —— 未存档
✅ [b] 分支名黑名单: 'task/01a113e467767eb9a4c33cb1df1cdfbb' 无违规前缀
⚠️  [c] 分支 'task/01a113e467767eb9a4c33cb1df1cdfbb' 仍被 worktree 占用（未清理）
❌ 拒绝: worktree 占用默认拒绝 —— 请先: git worktree remove <路径>
❌ safe-merge: 存在拦截项 —— 禁止 merge (拒绝种类: worktree-occupied)     exit=1
```

(Guard `[a]` matches `git tag -l "archive/$BRANCH"` = `archive/task/01a113e4…`;
this run's tag was spelled `archive/01a113e4…-<date>`, which `[a]` does **not**
match — noted so the check is not mistaken for more protection than it is.)

### 4.3 Disposition, and the work that remains

> **RESOLVED at the close of this record — §14 measures it.** The holder row
> terminated, its merge landed the branch, and this run then created the archive
> tag at the measured tip. The table and the shell below are kept as the record of
> what the gate looked like while it held and of the plan that was actually
> executed; §14 is the authoritative close-out state.
> **Final state**: `refs/heads/task/01a113e4…` ABSENT (`rev-parse --verify --quiet`
> exit=1); `archive/01a113e467767eb9a4c33cb1df1cdfbb-20261007` → `21e9563a2` (= the
> archived tip, `git rev-parse` equality checked); `master` = `21e9563a2` (REF-level
> `is-ancestor` exit=0, i.e. **covered at both levels now**); rescue content
> `127f5130c` / sha256 `9c4671fb…` on master and recoverable through the tag.

| ref | state after this run | why |
|---|---|---|
| `feat/rescue-01a113e4-capture-rig` | **kept** — the deliverable | carries the rig doc + its index row |
| `task/01a113e467767eb9a4c33cb1df1cdfbb` | **kept, un-archived** | live ground of `01a115bb` (in_progress); its content is now landed by the rescue, so the ref is redundant but not removable by this run |
| `archive/…-20261007` (by hand) and `archive/task/…` (by the verb) | **created twice, withdrawn twice** | §4.2 proved the ref mechanism; §7 asked the supported verb and found it decides a live ground's ref — both withdrawn, with the reason ref `refs/gopi/archive-reason/task/…` deleted too |

**The archive finishes the moment the merger's row terminates** — the step, and
the shell to run it, are:

```bash
BR=task/01a113e467767eb9a4c33cb1df1cdfbb
gopi task list --all | grep '^#01a115bb'        # must no longer read in_progress
gopi task show --branch $BR                     # WHO owns the branch NOW — the verb's gate reads this
git worktree list | grep gopi-01a115bb          # must be gone (the holder's own finish path removes it)
TIP=$(git rev-parse $BR)                        # record the measured tip (it is live; never quote a historic sha)
git merge --ff-only feat/rescue-01a113e4-capture-rig   # ONLY if the blob check below says master lacks it
git rev-list master --objects | grep -q 127f5130cb18a49fddad09e5d234fdbb4f401417 || echo "content still missing"
gopi task archive $BR --reason superseded       # the ONE writer: tag + reason ref + its own reclaim arm
git rev-parse --verify --quiet refs/heads/$BR; echo "exit=$? (1=gone)"
git tag -l "archive/$BR"                        # the tag the verb wrote, carrying the WHY
git show archive/$BR:devdoc/voice-input-local-capture-rig.md | sha256sum   # 9c4671fb… (content recoverable)
git branch; git worktree list                   # confirm no residue
```

**Amendments measured this round (§7) — the follow-up must not skip them:**

1. **Do not remove the worktree; and use `-d`, not `-D`.** By the time that step
   runs, `git worktree remove` has already happened as part of the holder row's
   own finish path (`internal/tools/task_finish_cleanup.go` runs
   `branch`+`-d`/`-D` — `finishCleanupBranchDeleteFlag = "-d"`, `…ForceDeleteFlag
   = "-D"`, lines 406–407). Removing a worktree by hand is the Rule 29 write onto
   a foreign tree, and `-D` throws away git's own merged-ness guard. The guard's
   own verdict on the current tip, measured:

   ```
   $ git merge-base --is-ancestor task/01a113e467767eb9a4c33cb1df1cdfbb master
   is-ancestor exit=1   (1 = NOT an ancestor ⇒ `git branch -d` refuses)
   ```

   So `-d` refuses **while the content is un-landed** — which is the honest
   outcome, and the one that tells the follow-up whether the rescue landed. Keep
   `-D` for the case where the tag is already in place and the check is done, and
   say *which* spelling was used in the record.
2. **The sweep will not do this for you.** The integration-side tidy
   (worktree remove + `branch -d` + archive tag) is **retired from the merger
   tick** — `internal/tools/repo_sweep.go:1361` ("The integration-side tidy
   (MergedCleanupSweep: worktree remove + branch -d + archive tag) is RETIRED
   from this tick — the sweep mutates no ref", epic `sweep-report-not-act`,
   2026-09-19). Nothing reclaims an un-merged ref on its own; the step below is
   the only path, which is why it exists as a row.

**And one step the follow-up must ADD: cover master with the rescued bytes if
nobody has.** The rescue only satisfies the archive precondition once its content
is *on master*, and whether that happened depends on a merge this run does not
perform (Rule 6e) and cannot observe. So the follow-up checks, and finishes the
job if the check is empty:

```bash
# on master, in a clean checkout (the follow-up's own worktree):
git rev-list master --objects | grep -q 127f5130cb18a49fddad09e5d234fdbb4f401417 \
  && echo "content already on master — archive only" \
  || echo "content NOT on master — land it first"
# if not landed: the rescue branch (below) fast-forwards, so the landing is free
git merge-base --is-ancestor master feat/rescue-01a113e4-capture-rig && \
  git merge --ff-only feat/rescue-01a113e4-capture-rig
```

The pointer the follow-up needs, in the form that survives an amended commit
(assert the content, never a frozen sha):

```bash
git rev-parse feat/rescue-01a113e4-capture-rig:devdoc/voice-input-capture-rig.md
# → 127f5130cb18a49fddad09e5d234fdbb4f401417  (the bytes, wherever the tip moved)
```

Whoever runs it should **not** re-open this question: the content-requirement is
already met (§3.2's condition 1), so what is left is a ref-hygiene step gated on
another row's lifecycle — not a decision.

**That step is filed, not left to memory** (Rule 26: an incomplete repair is
repaired by dispatching again, never by waiting). Row
`01a11676-c490-7ab3-a664-91cfa9b6408a` carries it with
`parent_task_id = 01a115bb-fe1e-7251-8ba4-0698a3ba5a01`, so the parent gate holds
it until the holder row terminates and releases it:

```
$ gopi task submit "归档孤儿分支 task/01a113e4…（内容已 rescue，只剩 ref 卫生）" \
      --parent-task-id 01a115bb-fe1e-7251-8ba4-0698a3ba5a01 --repo /home/user/work/GoPi
Task queued: 01a11676-c490-7ab3-a664-91cfa9b6408a (status=pending, source=agent)
```

Two consequences worth stating plainly. The chain does **not** fire if the holder
ends as `cancelled` rather than `completed|verified` — Rule 33 releases a child
only on the exact release set, so a cancelled holder parks it forever and that
becomes an operator decision (`gopi worker 01a11676-…` runs it explicitly). And
the tag at that point must name the tip **measured then**, not `a7c192187`: the
branch gained two commits (`c6ef3ed92`, `98bce2855`) *while this run was
documenting it*, so any sha frozen here is stale by construction.

---

## 5. Re-test: does master cover the branch? (the command to re-run)

The question is not "does master carry a *similarly named* file" — it does, and
that is the trap this branch is made of. These are the three commands whose
answers decide it, and they are cheap to repeat:

```bash
BR=task/01a113e467767eb9a4c33cb1df1cdfbb

# (1) ref-level: is any commit of the branch reachable from master?
git rev-list master > /tmp/onmaster.txt
for sha in $(git rev-list master..$BR | tac); do
  printf '%s ' "$sha"; grep -qx "$sha" /tmp/onmaster.txt && echo REACHABLE || echo NOT-on-master
done
# measured: every sha NOT-on-master (8 at the first reading, 10 later — the
# branch is live; see §1.1)

# (2) content-level: is the branch's unique blob anywhere on master?
git rev-list master --objects | grep -q 127f5130cb18a49fddad09e5d234fdbb4f401417 \
  && echo PRESENT || echo ABSENT
# measured: ABSENT-on-master   ← and it was ABSENT before the rescue, which is
# the reading that justified the rescue

# (3) after this branch lands: the same blob is on master at a NEW path
git rev-parse master:devdoc/voice-input-capture-rig.md   # currently ABSENT
```

Reading (1) or (2) alone is not enough: a name-level or path-level check gives
the wrong answer here (§5.2 of `worktree-naming-uuid.md` makes the same point),
and the rescued document is exactly the case where the **same blob at a
different path** is the correct outcome.

---

---

## 6. Close-out re-measurement — the ref grew twice while this record was being written

§1.1 already reported the branch as a moving target (8 commits off master at the
first reading, 10 minutes later). Re-measured once more before this run closed,
the movement continued, and the movement is the point: **a figure about this ref
is only true with the tip and the master tip named.** Both are named here.

```
$ git rev-parse master                                  # 7e084ba22675b8a612ee28ef2950ae5d1176a513
$ git rev-parse task/01a113e467767eb9a4c33cb1df1cdfbb   # 2fb334242d2c40ae331c0825c245bfffdb990c4d
$ git rev-list --count master..task/01a113e467767eb9a4c33cb1df1cdfbb   # 11

$ git diff --name-status master...task/01a113e467767eb9a4c33cb1df1cdfbb
M	devdoc/DOC-INDEX.tsv
A	devdoc/merge-resolution-voice-input-rig-conflict.md
A	devdoc/voice-input-local-capture-rig.md

$ git log --format='%h %p|%ci|%s' master..task/01a113e467767eb9a4c33cb1df1cdfbb
2fb334242 98bce2855 7e084ba22|2026-10-07 21:05:47 +0800|Merge branch 'master' into task/01a113e4…
98bce2855 c6ef3ed92|2026-10-07 20:59:09 +0800|docs(merge-resolution): name the commit hash and the session evidence paths
c6ef3ed92 a7c192187|2026-10-07 20:59:01 +0800|docs(merge-resolution): classify the rig-conflict report in the same commit
a7c192187 dba11bb24 e153b15c7|2026-10-07 20:52:16 +0800|Merge branch 'master' into task/01a113e4…
dba11bb24 e2331156f|2026-10-07 11:56:52 +0800|docs(voice): attribute the listener figure by pid and re-measure…
… (the seven original commits, §2)
```

So the branch now carries **9 content commits + 2 merges = 11** above master, at
three paths. §2's per-commit ruling remains valid for the seven commits it
covers (a commit's bytes do not change when the ref moves); what this section
adds is the ruling on the two commits that did not exist when §2 was written,
and the fact that the ref's tip is not `a7c192187` any more.

| # | sha | what it changes | ruled |
|---|---|---|---|
| 8 | `c6ef3ed92` | `A devdoc/merge-resolution-voice-input-rig-conflict.md` (+ `DOC-INDEX.tsv` row) | **not rescued** — see below |
| 9 | `98bce2855` | 7 lines added to that same document | **not rescued** — same reason |

**Why these two are not rescued, and why that is not an omission.** They are the
holder row's own merge-resolution record for the very collision §2 describes,
authored while this run was measuring (`20:59:01`, `20:59:09`). Two measured
reasons to leave them where they are: (1) they belong to a row that is **still
`in_progress` and still writing** (`2fb334242`, a fresh `git merge master`, landed
at `21:05:47`, i.e. after §2 was committed) — copying a document mid-author means
the rescue freezes a text its author has not finished; (2) the document is the
resolution of a conflict that is *already* resolved by path separation, so its
evidentiary value is the holder's own hand-over, not content master needs.

The reader is not left blind to it: the archive step in §4.3 tags the tip **as
measured then**, so if the ref is ever removed, that document is recoverable from
the tag (`git show archive/<name>:devdoc/merge-resolution-voice-input-rig-conflict.md`).
What must not be done is to copy it into this rescue — a second copy of a live
author's draft is worse than no copy.

**The duplicate-landing resolution, stated now so nobody has to re-derive it.** The
rig bytes (blob `127f5130c`) are in three places by design: this rescue at
`devdoc/voice-input-capture-rig.md`, the holder branch at
`devdoc/voice-input-local-capture-rig.md`, and the peer's `c47fdddbe` at the same
path as this rescue. Whichever side lands **first**, the later one should be
dropped in favour of master's copy rather than merged alongside it — identical
blob, so nothing is lost, and the alternative is one 452-line document at two
paths in a `devdoc/` corpus whose whole purpose is one row per file.

**Problem**: the follow-up row `01a11676-c490-7ab3-a664-91cfa9b6408a` is parked behind the holder and, as written, tells its reader to `git worktree remove` the holder's worktree — a Rule 29 write onto a foreign tree, and redundant (the holder's own finish path removes it). Add to §4.3: the worktree line is dropped; the branch delete uses `-d` (git refuses while the content is un-landed — the informative outcome), with `-D` reserved for after the tag is in place; and the follow-up also checks whether blob `127f5130c` ever reached master, landing the rescue branch if not (the sweep will not: `repo_sweep.go:1361`). An operator or PA should amend the row's briefing with this paragraph, or the reader must open this section first.

State of the archive's blocker at this same reading — unchanged, and still the
only thing in the way:

```
$ gopi task list --all | grep -E '^#01a115(b|c)'
#01a115bb-fe1e-7251-8ba4-0698a3ba5a01 [in_progress] resolve merge conflict: rework: … [running 22m28s] [ground task/01a113e4… @ /home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01]
$ git worktree list | grep 01a115bb
/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01  98bce2855 [task/01a113e467767eb9a4c33cb1df1cdfbb]
$ git branch -D task/01a113e467767eb9a4c33cb1df1cdfbb      # still refused, by design
$ git tag -l 'archive/01a113e4*'                            # still empty (withdrawn, §4.2)
```

---

## 7. The LIVING archive verb, measured against this ref — and withdrawn in the same round

§4.2 proved the tag mechanism with `git tag` by hand. This round asked the
**supported verb** (`gopi task archive`, the ONE archive writer — Rule 13/14)
what it does with *this* ref, and the answer is worth more than either: it
**does not refuse at all** (§7.1 — it decides the ref by an older cancelled row),
and the ref it writes is not a marker but a **switch** (§7.2).

### 7.1 The verb did NOT refuse — and the owner it names is not the live holder

```
$ ./bin/gopi task archive task/01a113e467767eb9a4c33cb1df1cdfbb --reason superseded
archive refused: task 01a114aa-eb19-7ce3-8333-d29df8e65e55 owns this branch and is "cancelled" …
```

This is the finding of the round, and it is the opposite of what this record
expected to measure. The verb **proceeded**:

```
$ ./bin/gopi task archive task/01a113e467767eb9a4c33cb1df1cdfbb --reason hand-mistake
archived task/01a113e467767eb9a4c33cb1df1cdfbb as archive/task/01a113e467767eb9a4c33cb1df1cdfbb
  reason: hand-mistake
  owner:  task 01a114aa-eb19-7ce3-8333-d29df8e65e55 (cancelled)
  read back: git cat-file blob refs/gopi/archive-reason/task/01a113e467767eb9a4c33cb1df1cdfbb
  skipped  … branch … is the integration branch or a task-run ground — never a reclaim candidate
verb exit=0
```

The gate that reads the row's status (`ArchiveDecide`, `internal/tools/archive_reason.go:890`)
resolved **`01a114aa-eb19-…`, a cancelled rework row** — *not* the live ground
holder `01a115bb-fe1e-7251-8ba4-0698a3ba5a01` (`in_progress`, whose worktree this
branch is checked out in, §1.1). A cancelled owner is `archiveOwnerArchivable`,
so the append-only ledger lets a ref whose head is a running run's ground be
**decided** by an older row about the same branch name.

The reason vocabulary is closed and listed by `-h` (`hand-mistake` = "a human
archived it by hand and the batch was a mistake"; `superseded`, `failed-verify`,
`not-ff-able-park`, `unowned`, … each name a real terminal condition). None of
them is true of this branch today — §3 rescues its content, §3.3 shows a peer
carrying the same bytes, and no row has decided anything — so the accepted reason
was deliberately the *wrong-but-accepted* one, and it was withdrawn in the same
round (§7.4). The acceptance's own §4.3 plan is unaffected in its order (content
first, then the decision), and its spelling is corrected: the tag must come from
the verb, never from `git tag` by hand, or the reason record is missing and
`gopi task archive --check` — which exits 1 on reason-less tags over terminal
rows, measured on this repo — reports it.

### 7.2 What the tag actually IS: a merge-exemption switch

`archive/<branch>` is read by `repoSweepArchiveExempt` (`internal/tools/repo_sweep.go:624`)
as a **tag-only** test — no `refs/heads` arm, no master/`task/` test in the
*reader*:

```go
func repoSweepArchiveExempt(repo, branch string) bool {
	out, err := repoSweepGit(repo, "tag", "-l", archiveBranchPrefix+branch)
	...
	return strings.TrimSpace(out) != ""
}
```

and its call sites include the merger's candidate scan (`repo_sweep.go:703`),
the branch-attribution read (`branch_attribution.go:369`), the row resolver
(`repo_resolve_row.go:127`) and the finish-cleanup chain
(`task_finish_cleanup.go:633`). So tagging a branch whose row is still live
**suppresses the merge lane** for a branch nobody has decided anything about —
the same conclusion §4.2 reached from `safe-merge.sh`, now with the production
reader named.

### 7.3 What the tag UNLOCKS: a head-deleting arm whose guards are elsewhere

`ArchiveReclaimDecidedRefs` (`internal/tools/archive_ref_reclaim.go`) deletes
`refs/heads/<branch>` with `git update-ref -d` for any branch that has an archive
tag naming the same commit (`archiveReclaimOne` → tip-equality guard →
`archiveReclaimDelete` re-verifies the tip immediately before the write). Its
protected set is **master/main + the `task/` prefix** — and that guard lives in
the DELETE arm, not the tag arm: measured above, the tag *was* written on a
`task/*` branch; what the reclaim arm then did was skip it **by name**:

```
  skipped  archive/task/01a113e467767eb9a4c33cb1df1cdfbb  branch task/01a113e467767eb9a4c33cb1df1cdfbb is the integration branch or a task-run ground — never a reclaim candidate
```

with the worktree guard as the second veto (`archiveReclaimHeldReason`: "branch
… is checked out at … — a live checkout is never touched (PROJECT.md Rule 29)").
This is the honest correction to §4.3's plan: **the tag and the delete are one
mechanism**, so "put a tag on it now and delete the branch later" arms a
deleter that is sitting one dead worktree away from removing a live run's ground.

### 7.4 Executed, then withdrawn — with the commands and their output

```
$ BR=task/01a113e467767eb9a4c33cb1df1cdfbb
$ ./bin/gopi task archive "$BR" --reason hand-mistake
archived task/01a113e467767eb9a4c33cb1df1cdfbb as archive/task/01a113e467767eb9a4c33cb1df1cdfbb
  reason: hand-mistake
  owner:  task 01a114aa-eb19-7ce3-8333-d29df8e65e55 (cancelled)
  read back: git cat-file blob refs/gopi/archive-reason/task/01a113e467767eb9a4c33cb1df1cdfbb
  skipped   archive/task/01a113e467767eb9a4c33cb1df1cdfbb  branch … is the integration branch or a task-run ground — never a reclaim candidate
verb exit=0

$ git tag -l 'archive/01a113e467767eb9a4c33cb1df1cdfbb'
archive/task/01a113e467767eb9a4c33cb1df1cdfbb
$ git for-each-ref --format='  %(refname) -> %(objectname)' refs/gopi/archive-reason/task/01a113e4…
  refs/gopi/archive-reason/task/01a113e467767eb9a4c33cb1df1cdfbb -> a5bf684c5239cfcf64043d27fb2a8a2d7f0bddff

$ git tag -d archive/task/01a113e467767eb9a4c33cb1df1cdfbb
Deleted tag 'archive/task/01a113e467767eb9a4c33cb1df1cdfbb' (was b26ede8e9)
tag -d exit=0
$ git update-ref -d refs/gopi/archive-reason/task/01a113e467767eb9a4c33cb1df1cdfbb
update-ref -d exit=0

$ echo "tags: [$(git tag -l 'archive/task/01a113e4*')]"      →  tags: []
$ git for-each-ref refs/gopi/archive-reason/task/01a113e4…   →  (nothing)
$ git rev-parse --verify --quiet "refs/heads/$BR"            →  b26ede8e9cd65247d6e072b7b9ff70846e568dbf
                                                               ref present exit=0
```

**The tip moved again under this measurement**: `b26ede8e9` where §6 measured
`2fb334242`, ten minutes earlier. The archive-ref tip and the tag both followed
it, which is exactly why no sha is frozen anywhere in this record.

**Withdrawn on three grounds, the first two new this round**:

1. **The tag is a switch, not a marker** (§7.2) — it suppresses the merge lane in
   four production readers while the merge lane is precisely what this branch is
   waiting for. Absence of the tag keeps every lane open for whichever side lands
   first.
2. **The tag arms a deleter** (§7.3) whose only remaining vetoes are the
   worktree's existence and the `task/` name — the same hazard §4.2 named from the
   safe-merge side, now measured from the production side.
3. **The decision would be recorded against the wrong row** (§7.1): the reason
   ref now names `01a114aa-eb19-…` (cancelled) as the owner of a ref whose head
   is `01a115bb-fe1e-7251-8ba4-0698a3ba5a01`'s ground. A record that points at the
   wrong row is worse than no record.

What survives from §4.2's withdrawal reasoning is unchanged: `archive/…` is the
decision lane's marker, and a decision that has not been made is not marked. §4.3
is amended accordingly — and the amendment is not cosmetic, because the *shape* of
the finishing step changes: the tag must come from `gopi task archive <branch>
--reason <code>` (the ONE writer, which also writes the reason ref and runs the
reclaim arm itself — `runArchiveReclaimAfterArchive`, `cmd/agent/cli_archive.go`),
never from `git tag` by hand, or the decision lands without a WHY and
`gopi task archive --check` reports it (measured on this repo this round: exit 1,
with the reason-less backlog listed). And whichever row is the branch's owner at
that moment decides whether the verb refuses: the follow-up must read
`gopi task show --branch <branch>`'s answer, not assume the live holder is the one
the gate will name.

**One hidden ref this round leaves behind, named rather than left to be
discovered**: the reason record lives at `refs/gopi/archive-reason/<branch>` — a
namespace no `git tag`/`git branch` listing shows. Both refs the verb wrote are
deleted above, so the repo is back to its pre-measurement ref set.

**Filed as a bug, because it is one** — the class is "the archive gate decides a
live ground's ref by an older cancelled row about the same branch name", and it is
dispatchable exactly as measured:

```
$ gopi task submit "bug(archive): a cancelled older row decides a ref whose head is a LIVE run's ground" \
      --tags bug --repo /home/user/work/GoPi
Task queued: 01a1167e-38c3-70e0-be1c-f6ed304fe851 (status=pending, source=agent, tags=bug,archive,ledger)
```

The payload carries the full transcript above, the four files that implement the
mechanism (`archive_reason.go:890` the gate, `repo_sweep.go:624` the reader,
`archive_ref_reclaim.go` the deleter, `cli_archive.go` the verb), and the
acceptance: a branch name with a cancelled **and** an `in_progress` owner row must
be REFUSED by `ArchiveDecide` (live owner wins), proven red-then-green by running
the real verb, not by a predicate test. Its parent is this run's row, so it is
parked until this row concludes — deliberate: the measurement it depends on is the
one recorded here.

---

## 8. Post-rescue re-measurement — the rescue LANDED, and the branch's last content decision defers to it

Written after the rescue reached master (`24c3c5537 Merge branch
'feat/rescue-01a113e4-capture-rig'`, 2026-10-07 21:10:35 +0800), and after the
branch merged master twice more. Master has advanced again, so §2/§6's counts are
superseded and **the figures below are the tip's own, taken together in one
reading**:

```
$ git rev-parse master                                      # 39c23372023b3c1f82479ca275eb8af28ba88e03
$ git rev-parse task/01a113e467767eb9a4c33cb1df1cdfbb       # 4d527593cbbf80e9a34a16b740d45401cabf80b0
$ git log -1 --format='%h %ci %s' <branch>
4d527593c 2026-10-07 21:17:48 +0800 Merge branch 'master' into task/01a113e4…

$ git diff --name-status master <branch>
M	devdoc/DOC-INDEX.tsv
A	devdoc/merge-resolution-voice-input-rig-conflict.md

$ git log --no-merges --oneline master..<branch> | wc -l    # 12 content commits
$ git log --merges    --oneline master..<branch> | wc -l    #  3 merges
$ git cherry master <branch> | grep -c '^+'                 # 12  (all +, none covered)
```

### 8.1 The three commits that did not exist when §6 was written — ruled

| # | sha | what it changes (measured) | still needed today? | reasoning / rescue |
|---|---|---|---|---|
| 10 | `f92a015ca` | `devdoc/merge-resolution-voice-input-rig-conflict.md` +121/−0 | **no — not rescued** | The holder row's own record of its second merge — same class as §6's ruling, same two reasons: authored by a row that is **still `in_progress` and still writing** (it committed again at 21:14, merged at 21:17), and the conflict it records is the one the rescue already resolved by path separation. |
| 11 | `b26ede8e9` | +27/−2 in that same document | **no — not rescued** | A further self-correction to the same live draft, ten minutes later; §6's reason (1) applies verbatim. (This one is **this** run's own commit, made on the orphan while measuring it — a correction, not a rescue candidate.) |
| 12 | `ad5bdbe5c` | `DOC-INDEX.tsv` −1, `…conflict.md` +92/−30, **`devdoc/voice-input-local-capture-rig.md` −452 (deleted)** | **no — and the DELETION is the reading that decides it** | This commit **deletes the branch's own copy of the rig document** because master now holds the identical bytes at `devdoc/voice-input-capture-rig.md` — measured: `git rev-parse <branch>:devdoc/voice-input-capture-rig.md` = `127f5130cb18a49fddad09e5d234fdbb4f401417` = `git rev-parse master:devdoc/voice-input-capture-rig.md`. It is a dedup **against this rescue**: the branch's latest content decision already defers to the rescued copy, so re-rescuing it would restore a document its own author just removed as redundant. |

The remaining three commits above master are merges (`a7c192187`, `2fb334242`,
`8741d03dc`); none introduces content of its own.

### 8.2 §5's three readings, re-run at this tip

```
(1) ref-level:   git merge-base --is-ancestor <branch> master   → exit 1   (NOT an ancestor)
(2) rig blob:    git rev-parse master:devdoc/voice-input-capture-rig.md
                   → 127f5130cb18a49fddad09e5d234fdbb4f401417
                 git rev-list master --objects | grep -c 127f5130c…   → 1   (PRESENT)
(3) the branch's own document:
                 git cat-file -e master:devdoc/merge-resolution-voice-input-rig-conflict.md
                   → fatal: Not a valid object name …          (ABSENT — branch-unique)
                 git rev-parse <branch>:devdoc/merge-resolution-voice-input-rig-conflict.md
                   → 9b65da41e7fc32c71ccac21eb73cb81b31f21168   (250 lines, 20506 B)
```

**The answer to §5's question changed exactly as the rescue predicted, and only
where it predicted it**: reading (2) flipped `ABSENT → PRESENT` on master, while
reading (1) stays "not an ancestor" — the branch is still not covered *as
history*, only its bytes are covered *as content* at a new path. What remains
branch-unique is one live author's merge-record, ruled **not** to be rescued in
§8.1.

### 8.3 What this means for the archive, and for the follow-up row

- The branch is **done producing rescuable content**: its one unique document is
  the live holder's own hand-over, and its rig bytes are on master. So the
  remaining work really is ref hygiene, which is what
  `01a11676-c490-7ab3-a664-91cfa9b6408a` was filed for (§4.3).
- The blocker is unchanged and re-measured at this tip: the holder row
  `01a115bb-fe1e-7251-8ba4-0698a3ba5a01` is `in_progress` **and its worktree is
  live** — the ground worktree is at the branch tip (`4d527593c`) with 13 live
  processes inside it (a `make test-budget PKGS='./cmd/agent …'` run), so

  ```
  $ git branch -D task/01a113e467767eb9a4c33cb1df1cdfbb
  error: Cannot delete branch 'task/01a113e467767eb9a4c33cb1df1cdfbb' checked out at
         '/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01'      exit 1
  $ bash scripts/safe-merge.sh task/01a113e467767eb9a4c33cb1df1cdfbb
  ❌ safe-merge: 存在拦截项 —— 禁止 merge … (拒绝种类: worktree-occupied)      exit 1
  ```

  Both are the **correct** outcome for a live ground, and neither is this run's to
  clear (Rule 29; the holder's own finish path removes it).
- When the holder concludes, the archive completes as: the tag written by the
  supported verb (`gopi task archive <branch> --reason <code>` — §7 measured that
  the verb decides the ref by its *older cancelled* owner `01a114aa-…` while the
  head is the live row's ground, so the follow-up must read
  `gopi task show --branch <branch>` first and, if the answer is still the
  cancelled row, treat the verb's output as the bug's evidence rather than as a
  licence), then `git branch -d` (informative while un-landed; `-D` only after the
  tag exists), then the `git branch` / `git worktree list` re-read.

  **Superseded in part by §9**: the follow-up's preconditions are two, and one of
  them is now satisfied — read §9.1 before running this block.

---

## 9. Fourth reading — the branch is STILL producing content, and it merged master twice more

Taken minutes after §8, in one pass, because §8's own figures had already gone
stale while it was being written. **This is the section a reader should quote;
§2/§6/§8's counts are superseded, not deleted — the per-commit rulings keep their
reasons, the figures do not.**

```
$ git rev-parse master ; git log -1 --format='%h %ci %s' master
39711d1464130df5bdff4df8e6f8df3d6a7a1528
39711d146 2026-10-07 21:20:13 +0800 Merge branch 'feat/rescue-01a113e4-capture-rig'

$ git rev-parse task/01a113e467767eb9a4c33cb1df1cdfbb
8f8325e64369456881e6404900f3d921b8679a37
$ git log -1 --format='%h %p %ci %s' <branch>   &&   git show --format='%H%n%P' --no-patch 4d527593c
8f8325e64 c287c58d2 39711d146 2026-10-07 21:20:32 +0800 Merge branch 'master' into task/01a113e4…
4d527593c… / parents ad5bdbe5c… 39c233720…

$ git log --no-merges --oneline master..<branch> | wc -l   → 13   (content commits; see §9.3 — stale within minutes)
$ git log --merges    --oneline master..<branch> | wc -l   →  5
$ git cherry master <branch> | grep -c '^+'                → 13   (all +, none covered)
$ git diff --name-status master <branch>
M	devdoc/DOC-INDEX.tsv
A	devdoc/merge-resolution-voice-input-rig-conflict.md
```

**Master moved twice under this record while it was being written**
(`24c3c5537` → `39c233720` → `39711d146`), and every one of those moves is this
run's own branch being merged — master's head is now literally the rescue merge
(`39711d146`'s second parent is `9b3ecf23d`, this record). The branch answered
each move with a delivery-point sync: `c287c58d2` at 21:19:12 and the merge
`8f8325e64` at 21:20:32.

### 9.1 The verdict, and why the archive does not happen in this run

- **The rescue is on master** — reading (2) of §5, re-measured at this newer
  master (same commit, re-run verbatim in the evidence file):

  ```
  $ git rev-parse --verify master:devdoc/voice-input-capture-rig.md
  127f5130cb18a49fddad09e5d234fdbb4f401417      exit=0     (PRESENT)
  $ git rev-list master --objects | grep -c 127f5130cb18a49fddad09e5d234fdbb4f401417
  1
  ```
- **Reading (1) is unchanged and must not be confused with (2)**:
  `git merge-base --is-ancestor task/01a113e4… master` → **exit 1**. Master holds
  the rescue's **bytes at a new path**; it does not hold the orphan's **history**.
  Both readings are in the evidence file, taken in the same pass.
- **`c287c58d2` was NOT ruled in §8.1** — §8.1 ruled the three commits that
  existed when *it* was written (`f92a015ca`, `b26ede8e9`, `ad5bdbe5c`); this one
  was committed at 21:19:12, after §8, and carries a further **+19/−4 rewrite of
  the holder's own merge-resolution record**. It is ruled here: **no — not
  rescued**, for §8.1's reason unchanged (it is a live row's self-recorded
  hand-over, whose diff is that record and its `DOC-INDEX` row, and whose subject
  matter — the rig conflict — the rescue already resolved by path separation).
- **Two more arrived while this section was being written** (`8191b4d0f` at
  21:21:07, +18; `fbe6ed9fa` at 21:21:53, +6/−3 — §9.3 measures the growth), both
  in the **same single document**, and they are ruled the same way: **no — not
  rescued**. At this point the ruling is no longer per-sha but **by class**, and
  the class is stated once: *every commit this branch still produces touches
  `devdoc/merge-resolution-voice-input-rig-conflict.md` (plus its `DOC-INDEX`
  row), a record of the holder's own syncs, authored by a run that is still
  executing.* The reasons are unchanged and they are the reasons to keep quoting —
  (a) a live author's working notes cannot be "rescued", because a rescue freezes
  a moving document into a second copy that immediately diverges; (b) the content
  they resolve (the rig-doc conflict) is already settled on master by path
  separation. **The class is decidable without enumerating shas, which is what
  makes it a ruling and not a snapshot.**
- **The blocker is `worktree`, and it is not this run's to clear.** Re-measured
  at this tip, by the real entry points rather than inferred:

  ```
  $ git branch -D task/01a113e467767eb9a4c33cb1df1cdfbb
  error: Cannot delete branch 'task/01a113e467767eb9a4c33cb1df1cdfbb' checked out at
         '/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01'          exit=1
  $ bash scripts/safe-merge.sh task/01a113e467767eb9a4c33cb1df1cdfbb
  ❌ safe-merge: 存在拦截项 —— 禁止 merge … (拒绝种类: worktree-occupied)         exit=1
  ```

  (`exit=1` measured on the script itself, not on a pipeline.) The holder row
  `01a115bb-fe1e-7251-8ba4-0698a3ba5a01` is still `in_progress` (`0 completed, 1
  with a raw-log checkpoint`, 181 turns), its worktree is at the branch tip
  `8f8325e64`, and a `/proc/*/cwd` scan found **8 live processes** inside that
  directory. **A ground that is still producing commits cannot be archived**: an
  `archive/` tag is a terminal loser marker and §7 measured that it *arms* a
  head-deleting arm (`ArchiveReclaimDecidedRefs`), so tagging a live ref is not
  bookkeeping — it is a delete request against another run's work. This run
  therefore withdraws nothing further and writes nothing: `git tag -l
  'archive/01a113e4*'` is **empty** (measured at this tip), and the branch ref is
  untouched.
- **What the follow-up row needs** (unchanged owner:
  `01a11676-c490-7ab3-a664-91cfa9b6408a`, `parent_task_id = 01a115bb-…`, so it
  releases only when the holder concludes): the preconditions are now **two**,
  both observable — (a) the rescue content is on master (✅ satisfied by
  measurement, §9.1), and (b) the holder's worktree is gone (`git worktree list`
  no longer lists `/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01`,
  and `git branch -D` no longer refuses). Only then: tag by the supported verb
  (§7's caveat applies — read `gopi task show --branch <branch>` first), then
  `git branch -D`, then the `git branch` / `git worktree list` re-read.

### 9.2 Rule 31 note — this round's build and smoke, at this tip

`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **exit 0**, and
`make test-budget PKGS='./internal/docindex ./internal/standards'` → **exit 0**,
`2 finished, 0 with a failure`, 94 s wall, stamp
`2026-10-07T21:18:06+08:00 host=peerbox … exit=0 outcome=pass
stream=run-1791379086-236075.json`. Re-run in this round rather than quoted from
§8's, because both branches moved between the two readings. `gopi tool doccheck`
→ **exit 0** (`the devdoc corpus is consistent`) after this section's edits, and
`git status --porcelain` in this run's worktree → empty (Rule 30).

Raw output of every command in §9, in one file:
`~/.gopi/sessions/01a115c3-fc80-7807-aaff-5c56272638ac/artifacts/orphan-01a113e4-section9-evidence.txt`
(96 lines) — the §8 file beside it (35 lines) is the earlier pass.

### 9.3 The count is a snapshot; the ruling is the class

Three readings of the same question inside this run, each correct when taken,
each stale within minutes:

```
$ git log --no-merges --oneline master..<branch> | wc -l
12      # §8   (master 39c233720 / branch 4d527593c)
13      # §9.1 (master 39711d146 / branch 8f8325e64)
15      # §9.3 (branch fbe6ed9fa, 21:21:53 — 8191b4d0f +18 and fbe6ed9fa +6/−3 arrived while §9 was being written)
$ git cherry master <branch> | grep -c '^+'     → 12 / 13 / 15   (never a single `-`: nothing is covered by patch-id)
```

The **holder is demonstrating the point of this section**: a run that is still
executing produces content faster than a report can enumerate it. That is why
§9.1 states the ruling **by class** — *anything this branch still produces is the
live holder's own sync record, and is not rescued* — and why the per-sha table in
§2/§8.1 keeps its measured content and reasons while its **counts** are marked
superseded. A reader after this run should take the class ruling from §9.1, and
may take the count from whatever single reading they run themselves; **no count
in this document should be quoted as the final one**, because there is no final
one while the branch is live:

```bash
git rev-parse task/01a113e467767eb9a4c33cb1df1cdfbb     # the tip it is at NOW
git cherry master task/01a113e467767eb9a4c33cb1df1cdfbb | grep -c '^+'
git log --no-merges --oneline master..task/01a113e467767eb9a4c33cb1df1cdfbb \
  | sed 's/^/  /'                                        # the class check: are they ALL in the one document?
```

### 9.4 The class, MEASURED — every `+` commit and the paths it touches

The class ruling is only as good as the measurement behind it, so here is the
whole set, taken in one pass at tip `7046c2957` (the `numstat` paths are the
commit's own; the full output is in the acceptance-sweep artifact):

```
$ for s in $(git cherry master <branch> | awk '$1=="+"{print $2}'); do
    echo "$(git rev-parse --short=7 $s)  [$(git show --numstat --format= $s | awk '{print $3}' | paste -sd, -)]"
  done

09ad059  [devdoc/voice-input-default-local.md]                                 ← the rig document
5848c46  [devdoc/DOC-INDEX.tsv]                                                ← its index row
8cc4e7e  [devdoc/voice-input-default-local.md]
7374cd0  [devdoc/voice-input-default-local.md]
9fedce8  [devdoc/voice-input-default-local.md]
e233115  [devdoc/voice-input-default-local.md]
dba11bb  [devdoc/voice-input-default-local.md]                                 ← 09ad059..dba11bb = the RIG DOCUMENT, all 7
c6ef3ed  [devdoc/DOC-INDEX.tsv, devdoc/merge-resolution-…conflict.md]          ← the holder's record begins
98bce28  [devdoc/merge-resolution-…conflict.md]
f92a015  [devdoc/merge-resolution-…conflict.md]
b26ede8  [devdoc/merge-resolution-…conflict.md]
ad5bdbe  [devdoc/DOC-INDEX.tsv, devdoc/merge-resolution-…conflict.md, devdoc/voice-input-local-capture-rig.md]
c287c58  [devdoc/merge-resolution-…conflict.md]
8191b4d  [devdoc/merge-resolution-…conflict.md]
fbe6ed9  [devdoc/merge-resolution-…conflict.md]
7046c29  [devdoc/merge-resolution-…conflict.md]                                ← c6ef3ed..7046c29 = the HOLDER'S LIVE RECORD, 9
```

**The split is exact and it is the whole ruling in one reading**: nine of the
sixteen — every commit from `c6ef3ed` onward, and every commit that arrived while
this document was being written — touch the holder's own
`merge-resolution-voice-input-rig-conflict.md` and, at most, the `DOC-INDEX.tsv`
row that classifies it (`ad5bdbe` also **deletes** the branch's rig copy, the
dedup §8.1 measured). The rig document itself is touched by commits `09ad059`…
`dba11bb` and by nothing after 11:56; its content is on master at the new path
(§9.1). So the class is not a heuristic: it is the measured fact that this
branch's **only** remaining output is a live run's note-taking on a conflict that
is already resolved — and the archive therefore waits on that run, not on
content.

---

## 10. Acceptance sweep — every reading re-taken at master `fb9835b55` / branch `7046c2957`

This section exists because both refs moved **again** while §9 was being
committed (§9.3's own warning, demonstrated once more). Every command below was
run in ONE pass in this run's worktree, and the whole transcript — command and
raw output, 114 lines — is filed at
`~/.gopi/sessions/01a115c3-fc80-7807-aaff-5c56272638ac/artifacts/orphan-01a113e4-acceptance-sweep.txt`.
Nothing in this section is quoted from an earlier one.

### 10.1 Both refs moved; the ruling did not

```
$ git rev-parse master
fb9835b55b4288a33eeebdaf64f39de3de8e496c
$ git log -1 --format='%h %ci %s' master
fb9835b55 2026-10-07 21:23:57 +0800 merge: union the append-only rows of
devdoc/DOC-INDEX.tsv (2134 row(s), mechanical, gopi merge union lane)  [record of a 2026 run; that verb was removed 2026-11-01]
$ git rev-parse task/01a113e467767eb9a4c33cb1df1cdfbb
7046c2957276c93f54902daf2bf8ea5dfd86ac08
$ git log -1 --format='%h %ci %s' task/01a113e4…
7046c2957 2026-10-07 21:22:24 +0800 docs(merge-resolution): record the fifth
sync as its own section, not an addendum
```

### 10.2 The class, re-measured at this tip — 16 `+`, the same split

One pass, `git cherry master <branch> | awk '$1=="+"{print $2}'` through
`git show --numstat --format=`; the paths are the commit's own:

```
09ad059  [devdoc/voice-input-default-local.md]
5848c46  [devdoc/DOC-INDEX.tsv]
8cc4e7e  [devdoc/voice-input-default-local.md]
7374cd0  [devdoc/voice-input-default-local.md]
9fedce8  [devdoc/voice-input-default-local.md]
e233115  [devdoc/voice-input-default-local.md]
dba11bb  [devdoc/voice-input-default-local.md]
c6ef3ed  [devdoc/DOC-INDEX.tsv,devdoc/merge-resolution-voice-input-rig-conflict.md]
98bce28  [devdoc/merge-resolution-voice-input-rig-conflict.md]
f92a015  [devdoc/merge-resolution-voice-input-rig-conflict.md]
b26ede8  [devdoc/merge-resolution-voice-input-rig-conflict.md]
ad5bdbe  [devdoc/DOC-INDEX.tsv,devdoc/merge-resolution-voice-input-rig-conflict.md,devdoc/voice-input-local-capture-rig.md]
c287c58  [devdoc/merge-resolution-voice-input-rig-conflict.md]
8191b4d  [devdoc/merge-resolution-voice-input-rig-conflict.md]
fbe6ed9  [devdoc/merge-resolution-voice-input-rig-conflict.md]
7046c29  [devdoc/merge-resolution-voice-input-rig-conflict.md]
```

Identical split to §9.4, with the two commits that arrived after §9 counted in
the holder's half: **7 + 1 index row = the rig document era (all on master by
content), 8 = the live holder's own sync record (not rescued, by class)**. The
count is 16, not 15; §9.3's table is superseded by this reading.

### 10.3 Two readings that MISLEAD, and the two that hold

A reader who reaches for the obvious two-dot diff gets a false picture, and this
round measured exactly how false:

```
$ git diff --name-status master task/01a113e467767eb9a4c33cb1df1cdfbb
D	cmd/agent/chat_voice.go
D	cmd/agent/chat_voice_pty_test.go
M	cmd/agent/cli_chat.go
M	cmd/agent/cli_chat_usage_budget_test.go
M	cmd/agent/commands.go
M	cmd/agent/lineedit_wire.go
M	devdoc/DOC-INDEX.tsv
D	devdoc/chat-voice-slash-command.md
A	devdoc/merge-resolution-voice-input-rig-conflict.md
M	internal/lineedit/editor.go
D	internal/lineedit/lineinsert.go
D	internal/lineedit/lineinsert_test.go
$ git diff --stat master <branch>
 … 12 files changed, 313 insertions(+), 2231 deletions(-)
```

**2231 deletions is not what this branch did.** The two-dot diff compares the two
tips as trees, and master has since added six commits this branch never merged
(`579634d25 feat(chat): /voice — continuous local voice input …`, `d8b02c4e2`
`internal/lineedit`, `8c014619e`, `e05e8fd7c`, `19c77cc8f`, `a078398f7`, then the
union-merge `fb9835b55`) — so `D cmd/agent/chat_voice.go` means "master has it,
the branch predates it", not "the branch deletes it". The same shape makes the
off-master commit count look like content:

```
$ git rev-list --count task/01a113e4… ^master          → 21
$ git cherry master task/01a113e4… | awk '{print $1}' | sort | uniq -c
     16 +
$ git merge-base master task/01a113e4…                 → 39711d146
```

**21 is not 21 content commits**: five of the off-master commits are merges, and
`git cherry` sees the 16 content commits only. The two readings that **do** hold
at this tip are the three-dot diff and cherry:

```
$ git diff --name-status master...task/01a113e4…       # what the BRANCH adds
M	devdoc/DOC-INDEX.tsv
A	devdoc/merge-resolution-voice-input-rig-conflict.md
$ git cherry master task/01a113e4…                     # 16 × '+', never a '-'
```

Both are in §5's re-test recipe; this round's contribution is the named trap:
**`git diff master <branch>` on a stale-baseline branch reports master's own
progress as the branch's deletions.**

### 10.4 The rescue is on master — and the branch itself still is not

```
$ git rev-parse master:devdoc/voice-input-capture-rig.md
127f5130cb18a49fddad09e5d234fdbb4f401417                      ← (2) PRESENT
$ git rev-list master --objects | awk '{print $1}' | grep -c '^127f5130…$'
1
$ git merge-base --is-ancestor task/01a113e4… master ; echo exit=$?
exit=1                                                        ← (1) not-covered
$ git branch --list feat/rescue-01a113e4-capture-rig
(empty)                                                       ← pruned after landing
$ git merge-base --is-ancestor d0032b8e8 HEAD ; echo exit=$?
exit=0                                                        ← the rescue commit
$ git rev-parse d0032b8e8:devdoc/voice-input-capture-rig.md
127f5130cb18a49fddad09e5d234fdbb4f401417                      ← same bytes, verbatim
```

So the rescue's bytes are on master (by content **and** by commit: the rescue
commit is an ancestor of this run's branch), while the orphan ref is still not
reachable from master — the two readings §5 keeps apart on purpose.

### 10.5 The archive block, re-run on its real entry points

```
$ git branch -D task/01a113e467767eb9a4c33cb1df1cdfbb
error: Cannot delete branch 'task/01a113e467767eb9a4c33cb1df1cdfbb' checked out at
       '/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01'
exit=1

$ bash scripts/safe-merge.sh task/01a113e467767eb9a4c33cb1df1cdfbb
✅ [a] archive tag: 无 archive/task/01a113e467767eb9a4c33cb1df1cdfbb —— 未存档
✅ [b] 分支名黑名单: 'task/01a113e467767eb9a4c33cb1df1cdfbb' 无违规前缀
⚠️  [c] 分支 'task/01a113e467767eb9a4c33cb1df1cdfbb' 仍被 worktree 占用（未清理）
❌ 拒绝: worktree 占用默认拒绝 —— 请先: git worktree remove <路径>
ℹ️  [d] 分支 'task/01a113e467767eb9a4c33cb1df1cdfbb' 尚未 merge 进 master
✅ [e] ref 存在性: refs/heads/task/01a113e467767eb9a4c33cb1df1cdfbb 可解析
✅ [f] 历史相关性: 'task/01a113e467767eb9a4c33cb1df1cdfbb' 与 master 有共同祖先

❌ safe-merge: 存在拦截项 —— 禁止 merge 'task/01a113e4…' (拒绝种类: worktree-occupied)
exit=1

$ git tag -l 'archive/01a113e4*'
(empty)
```

Guard `[d]` is the machine's own version of §10.4: the branch is **not** merged
as a ref, even though its unique content is on master. Nothing to add by hand.

### 10.6 The live ground, re-measured — 9 processes, and the holder is testing

```
$ for p in /proc/[0-9]*; do case "$(readlink $p/cwd)" in
      /home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01*) … esac; done
  pid=250348 make test-budget PKGS=./internal/docindex ./internal/standards ./cmd/agent
  pid=250541 go test -count=1 -timeout 25m -json ./internal/docindex ./internal/standards ./cmd/agent
  pid=251128 …/agent.test -test.paniconexit0 …
  … (9 live processes in total, incl. a `sleep 240` and the merger's own `bin/gopi`)
$ git -C /home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01 rev-parse HEAD
7046c2957276c93f54902daf2bf8ea5dfd86ac08          ← the holder is AT the branch tip
```

The holder row `01a115bb-fe1e-7251-8ba4-0698a3ba5a01` was still `in_progress`
(42 m running) at this reading, its worktree at the tip the branch is on, and a
`make test-budget` of its own in flight. That is the whole reason the archive is
not performed here, restated at this tip rather than inherited: **an `archive/`
tag is not bookkeeping — §7 measured that it arms a head-deleting arm — so it is
not written against a ref another run is standing on.**

### 10.7 What this round changes

Nothing in the ruling: §2's seven content commits keep their verdicts (the
content is on master, at
`devdoc/voice-input-capture-rig.md` = blob `127f5130c`), and §9.1's class ruling
covers the 8 later holder commits, now 8 of 16. What changes is the **reading a
reader should trust**, and the reading the follow-up row must re-run: at this
tip, `git cherry` says 16 `+`, `git diff master...<branch>` says *two devdoc
paths*, and the archive still refuses with a reason that is a live worktree.

---

## 11. Final acceptance sweep — the readings that define "done", taken in ONE pass

Written after §10's commit (`d19c6ac5d`). Every number below was read in a single
`bash` invocation at master `fb9835b55` / this branch `d19c6ac5d` / orphan
`7046c2957`, so no two of them describe different instants (the defect §10.3
diagnosed). Raw transcript:
`~/.gopi/sessions/01a115c3-…/artifacts/orphan-01a113e4-final-acceptance.txt`.

### 11.1 The orphan test — unchanged, at the acceptance tip

```
$ git rev-parse --verify --quiet refs/heads/task/01a113e467767eb9a4c33cb1df1cdfbb
7046c2957276c93f54902daf2bf8ea5dfd86ac08            exit=0   ← ref still present
$ git cherry master task/01a113e467767eb9a4c33cb1df1cdfbb | wc -l
16                                                           ← 16 lines
$ git cherry master task/01a113e467767eb9a4c33cb1df1cdfbb | grep -c '^+'
16                                                           ← ALL still `+`
$ git merge-base --is-ancestor task/01a113e467767eb9a4c33cb1df1cdfbb master
exit=1                                                       ← no commit reachable from master
$ git rev-parse master:devdoc/voice-input-capture-rig.md
127f5130cb18a49fddad09e5d234fdbb4f401417                     ← content on master
$ git rev-list master --objects | grep -c 127f5130cb18a49fddad09e5d234fdbb4f401417
1                                                            ← exactly one place
```

`(2)`+`(4)` say the rescue landed and is unique; `(1)`+`(3)` say the BRANCH was
never covered — which is the whole reason the two are recorded separately (§5).

### 11.2 The rescue, as delivered (Hand-over ①)

```
$ git rev-parse --verify --quiet refs/heads/feat/rescue-01a113e4-capture-rig
exit=1                                                       ← the ref is GONE (merger deleted it after landing)
$ git rev-parse d0032b8e8:devdoc/voice-input-capture-rig.md
127f5130cb18a49fddad09e5d234fdbb4f401417                     ← the rescue commit still holds the bytes
$ git rev-parse master:devdoc/voice-input-capture-rig.md
127f5130cb18a49fddad09e5d234fdbb4f401417                     ← identical → landed verbatim
```

An exit 1 here is the EXPECTED result, and §10.4's correction is now measured a
second time: the recovery spelling is the **blob** (`127f5130c`) or the rescue
commit `d0032b8e8`, **never** `feat/rescue-01a113e4-capture-rig` — the merger
retires a branch after it lands, so a hand-over that names the branch hands a
reader a dangling name. `git show 127f5130c > devdoc/voice-input-capture-rig.md`
restores the document from wherever it lives.

### 11.3 The archive — the refusal, re-run against the real entry point

```
$ git branch -D task/01a113e467767eb9a4c33cb1df1cdfbb
error: Cannot delete branch 'task/01a113e467767eb9a4c33cb1df1cdfbb' checked out at
       '/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01'      exit=1
$ bash scripts/safe-merge.sh task/01a113e467767eb9a4c33cb1df1cdfbb      exit=1
       (拒绝种类: worktree-occupied)
$ git tag -l 'archive/01a113e4*'                                          ← empty
$ git worktree list
/home/user/work/GoPi                                        fb9835b55 [master]
/dev/shm/gopi-masterchk                                     93848040e (detached HEAD)
/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01  7046c2957 [task/01a113e467767eb9a4c33cb1df1cdfbb]
/home/user/workspace/gopi-01a115c3fc807807aaff5c56272638ac  d19c6ac5d [task/01a115c3fc807807aaff5c56272638ac]
/home/user/workspace/gopi-01a1161e-fix                      5276068c1 (detached HEAD)
/home/user/workspace/gopi-01a1161e-verify                   5276068c1 (detached HEAD)
```

(The third line's sha is the branch's head **at the instant it was printed** — a
minute later the same line read `72a3746dc` (§11.5). What the line proves is the
[task/…] label and the path, not the number.)

The refusal is not a stale sentence in this document: it is the **same two
commands, the same two exit codes**, re-taken at the acceptance tip, and the
third line of `git worktree list` is the mechanism — a **linked worktree is
standing on that branch right now**. That is exactly the state in which
`git branch -D` is refused by design, and Rule 29/30a forbids taking it anyway:
the ground belongs to a live row.

### 11.4 The live ground — the number dropped, the meaning did not

```
$ for p in /proc/[0-9]*; do case "$(readlink $p/cwd)" in *gopi-01a115bbfe1e72518ba40698a3ba5a01*) …;; esac; done
holder_procs=1
$ gopi task show 01a115bb-fe1e-7251-8ba4-0698a3ba5a01 | grep -E '^(status|runs|deliverable\.worktree|ground)'
status:         in_progress
runs:           1 run(s) — 0 completed, 1 with a raw-log checkpoint
deliverable.worktree: /home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01
ground (resolved now): … branch=task/01a113e467767eb9a4c33cb1df1cdfbb (exists) — recorded ground — verified live
```

§10.6 measured **9** processes inside that worktree; this reading finds **1**. The
count is a snapshot and must not be quoted as a property — what holds across both
readings is the thing that decides the archive: the row is `in_progress` with an
**unfinished run** (0 completed / 1 with a raw-log checkpoint), the worktree is
still linked to the branch, and the resolver reports the ground **verified live**.
One process or nine, the branch is somebody's ground.

### 11.5 The tip moved once more — and that is what makes the diff reading clean

The sweep above (21:28:59) found the orphan at `7046c2957`; the next minute the
holder **aligned to the delivery point** and the ref moved again:

```
$ git rev-parse task/01a113e467767eb9a4c33cb1df1cdfbb
72a3746dc309fd450324da2ea553cf812faea692
$ git log -1 --format='%an %ad%n%s' task/01a113e4…
gopi gopi@localhost Wed Oct 7 21:29:00 2026 +0800
Merge branch 'master' into task/01a113e467767eb9a4c33cb1df1cdfbb
$ git log --oneline 7046c2957..task/01a113e4…
72a3746dc Merge branch 'master' into task/01a113e467767eb9a4c33cb1df1cdfbb
fb9835b55 merge: union the append-only rows of devdoc/DOC-INDEX.tsv (…, gopi merge union lane)  [quoted commit subject from 2026, unedited]
a078398f7 docs(voice): quote the observed clean close-out instead of promising it
19c77cc8f docs(voice): report the budget run as measured — 12 reds, 11 of them on the base
e05e8fd7c docs(voice): name the deliverable commits in the /voice report
8c014619e test(chat): raise the -h line budget for /voice's own contract paragraph
579634d25 feat(chat): /voice — continuous local voice input on the editing line
d8b02c4e2 feat(lineedit): cross-goroutine line insert + the flushed-Escape gate fix
$ git cherry master task/01a113e4… | wc -l ; … | grep -c '^+'
16
16
```

Two things this settles, both of which §10.3 flagged as readings that MISLEAD:

- **`git log 7046c2957..<branch>` looked like the branch was deleting 2231 lines
  of master's code.** It was not: those commits (`579634d25` `/voice`,
  `d8b02c4e2` lineedit, …) are **master's own**, arriving on the branch through
  this merge. Two-dot `A..B` and `git rev-list <branch>` count the master side
  too, so on a branch that has merged master twice they read as the branch's own
  work. Three-dot `git diff master...<branch>` and `git cherry` are the readings
  that hold.
- **The merge is the holder following Rule 30a, not the branch growing.** Its
  `git diff --name-status 7046c2957..72a3746dc` is exactly master's own six
  commits' files (`cmd/agent/chat_voice.go`, `internal/lineedit/lineinsert.go`, …)
  — the same file set `master` gained. The branch's OWN unique content is still
  what it was: nothing (blob `127f5130c` lives on master; §11.2).

### 11.6 This run's own gate — Rule 30, read honestly

Two readings, and the difference between them is the point:

```
$ git status --porcelain        （21:27, before §11 was written）
(no output)

$ git status --porcelain        （21:28:59, the sweep above)
 M devdoc/voice-input-rig-orphan-disposition-01a113e4.md
```

The second line is **§11 itself, uncommitted while it was being written** — the
sweep ran inside the round that edits this document, so of course it sees its own
edit. That is not a Rule 30 violation (untracked/tracked dirt from *another*
source) but it is also not a clean reading, and the honest statement is the one
Rule 30 asks for: the gate is asserted on the **committed** state, taken again
after the §11 commit, where porcelain prints nothing. A reader who wants the
proof runs the command, not this sentence.

```
$ ./bin/gopi tool doccheck
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row   exit=0
$ grep -n 'voice-input' devdoc/DOC-INDEX.tsv
1946:devdoc/voice-input-capture-rig.md	historical	historical (0 standing / 2 record): …
1957:devdoc/voice-input-rig-orphan-disposition-01a113e4.md	historical	historical (0 standing / 2 record): …
```

Both documents this run produced carry their classification row in the corpus
(Rule 36), written by the tool and not by hand.

### 11.7 The disposition, stated so the two rows cannot be confused

| question | answer | the reading that decides it |
|---|---|---|
| is the orphan's **content** still needed? | **no — it is on master** | `master:devdoc/voice-input-capture-rig.md` = `127f5130c`, `--objects` count 1 |
| is the orphan **branch** covered by master? | **no — never was** | `merge-base --is-ancestor` exit 1, `cherry` 16/16 `+` |
| what does the archive need? | the holder to release | `branch -D` exit 1 + `safe-merge.sh` exit 1 + worktree list + row `in_progress` |
| who does it? | row `01a11676-c490-7ab3-a664-91cfa9b6408a`, **parent = the holder row** | `gopi task show` on both rows (§4.3, §8.3) |

The third and fourth lines are why this run stops: the archive's blocker is a
**dependency**, and the row that owns the dependency is the parent of the row
that does the archive — so the gate itself guarantees the order, and no retry,
override or force is needed. §4.2's withdrawn tag stands as the evidence that the
manual path was tried and refused rather than assumed.

---

## 12. Delta sweep — what arrived after §11, and the path census that settles the ruling

§11 took its readings at orphan `7046c2957`. The ref kept producing commits while
this record was being written (that is §9.3's whole point: the author is still
alive), so this section re-reads the ref at its **newest** tip and answers the one
question a moving target keeps re-opening: *does anything new need rescuing?*
Everything below is from ONE `bash` invocation at master `fb9835b55` / orphan
`90b9355a0`; raw transcript (91 lines, every command with its output) at
`~/.gopi/sessions/01a115c3-fc80-7807-aaff-5c56272638ac/artifacts/orphan-01a113e4-section12-delta.txt`.

### 12.1 The two commits that arrived after §11 — ruled

```
$ git log --oneline 72a3746dc..task/01a113e467767eb9a4c33cb1df1cdfbb
90b9355a0 docs(merge-resolution): the report describes six syncs and its final artifact list
b26abfe0b docs(merge-resolution): sixth sync (master fb9835b55) and the 8-red baseline reading

$ git log --format='%h %s' --name-status 72a3746dc..task/01a113e4…
90b9355a0 docs(merge-resolution): the report describes six syncs and its final artifact list
M       devdoc/merge-resolution-voice-input-rig-conflict.md
b26abfe0b docs(merge-resolution): sixth sync (master fb9835b55) and the 8-red baseline reading
M       devdoc/merge-resolution-voice-input-rig-conflict.md
```

| sha | what it changes (measured) | still needed today? | reasoning / rescue |
|---|---|---|---|
| `b26abfe0b` | +lines in `devdoc/merge-resolution-voice-input-rig-conflict.md` only | **no — not rescued** | It is the **live holder's own sync record** (its §9.4 class), a running log of that run's merges with master. Freezing a live author's half-written log into a second copy would duplicate a document that is still being edited and whose owner is the row that owns this ref. |
| `90b9355a0` | same single path, same class | **no — not rescued** | Same reason. The path is absent from master (`master:devdoc/merge-resolution-voice-input-rig-conflict.md` → `fatal: … does not exist in 'master'`), and that absence is **correct**: it is not a deliverable, it is a run's diary, and its owner will land it through the holder row if it is meant to land. |

So the two new commits change **the count, not the ruling**: the ref grew from 16
to 18 `+` commits, and the class verdict (a live author's own record → *not
rescued*) covers both without a new decision. The branch's **content** decision —
made at §2/§3 and confirmed at §11.1 — is untouched.

### 12.2 The ruling's total, re-stated at the acceptance tip

The epic's acceptance asks for a per-commit ruling **totalling 7**. That number is
the branch's *content* commits, and §2 rules exactly them:

```
$ git cherry master task/01a113e467767eb9a4c33cb1df1cdfbb | head
+ 09ad05915529cbeea33a96a9d24420c8bdd02064
+ 5848c46e4188c4942f9b08fca6c80c2c8385deec
+ 8cc4e7e862de034bd84c4e2fe46d7ae9143ef02b
+ 7374cd07a15ef6896dbd58947cdf2e1c83a80e8a
+ 9fedce85e904d86b41923c9c2a470fb502fdbd71
+ e2331156f2d646a0843f14d163444ca2c6b496f9
+ dba11bb248413593f5ff59d42b5ea3d64345eae1
```

Names 1–7 of that list are §2's rows #1–#7 (the rig record, six revisions and its
index row); §2's row #8 is the merge `a7c192187`, which carries **no content of
its own** — it is the layout repair, and its shape is what the rescue copied.
**7 content + 1 merge = 8 sha ruled, and both numbers are stated** rather than one
being quietly enlarged to the other.

### 12.3 The path census — the strongest form of the class verdict

A per-commit ruling is a *reading*; a census over every `+` commit is a *class*.
Here is the class, measured at the acceptance tip:

```
$ for s in $(git cherry master task/01a113e4… | awk '{print $2}'); do
    git show $s --format= --name-only; done | grep -v '^$' | sort | uniq -c | sort -rn
     11 devdoc/merge-resolution-voice-input-rig-conflict.md
      6 devdoc/voice-input-default-local.md
      3 devdoc/DOC-INDEX.tsv
      1 devdoc/voice-input-local-capture-rig.md

$ for s in $(git cherry master task/01a113e4… | awk '{print $2}'); do
    git show $s --format= --name-only | grep -v '^$' | grep -v '^devdoc/'; done
(no output)
```

**All 18 `+` commits touch `devdoc/` paths and nothing else** — no Go, no
`cmd/`, no `scripts/`, no test. The four paths, and what each is today:

| path | branch blob | master blob | ruling |
|---|---|---|---|
| `devdoc/voice-input-default-local.md` | `4bfa96995` | `4bfa96995` | identical — the branch's later merge accepted master's document here (§2), so this path is a **collision marker**, not unique content |
| `devdoc/voice-input-local-capture-rig.md` | **ABSENT** | ABSENT | the branch's own rig copy, **deleted by its own author** (`ad5bdbe5c`, §8.1) once master held the same blob |
| `devdoc/merge-resolution-voice-input-rig-conflict.md` | `5d2e8e515` | ABSENT | the live holder's own sync record → **not rescued** (§12.1) |
| `devdoc/DOC-INDEX.tsv` | (rows) | (rows) | classifies the above; the rescue's own row was written by the tool (§3) |

The one path that is *unique content*, `merge-resolution…conflict.md`, is unique
**because it is live**, and that is exactly the case a rescue must not copy
(§9.4). The rig record — the branch's original purpose — already exists on master
as blob `127f5130c` (§12.4), byte-identical to the rescue.

### 12.4 Does master cover it now? — the re-test, with both readings kept apart

```
$ git merge-base --is-ancestor task/01a113e4… master ; echo exit=$?
exit=1                                   ← REF level: no commit of the branch is on master
$ git merge-base --is-ancestor master task/01a113e4… ; echo exit=$?
exit=0                                   ← master IS an ancestor of the branch (it merged master in)
$ git rev-list --count master..task/01a113e4…
24                                       ← commits reachable from the branch but not master
$ git rev-list --count task/01a113e4…..master
0                                        ← nothing on master is missing from the branch

$ git rev-parse master:devdoc/voice-input-capture-rig.md
127f5130cb18a49fddad09e5d234fdbb4f401417      ← CONTENT level: the rescued bytes are on master
$ git rev-list master --objects | grep -c 127f5130cb18a49fddad09e5d234fdbb4f401417
1
```

**The two levels, stated so neither can be quoted alone:**

- **REF level — master does NOT cover the branch, and never did**: `exit=1` on the
  ancestor test, `cherry` `18/18 +`. The branch is an orphan in the strict sense:
  it is not a mere un-merged tip, it is a line of history master has never
  contained.
- **CONTENT level — master DOES cover everything the branch *decided***: the rig
  document is on master as blob `127f5130c`, exactly once, at a non-colliding
  path. The remaining `+` commits' own paths are either identical to master's
  document (the collision) or the live author's diary (§12.3).

`24` is a **snapshot** — it was `21` at §10 and the ref is still moving — so a
reader re-running this must expect a different number and the same two verdicts.
That divergence (a count that drifts while the two readings hold) is §10.3's
warning, measured again: never quote the count as the ruling.

### 12.5 The archive — unchanged blocker, re-measured on the real entry points

```
$ git branch -D task/01a113e467767eb9a4c33cb1df1cdfbb ; echo exit=$?
error: Cannot delete branch 'task/01a113e467767eb9a4c33cb1df1cdfbb' checked out at
'/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01'
exit=1

$ bash scripts/safe-merge.sh task/01a113e467767eb9a4c33cb1df1cdfbb ; echo exit=$?
✅ [a] archive tag: 无 archive/task/01a113e467767eb9a4c33cb1df1cdfbb —— 未存档
✅ [b] 分支名黑名单: 'task/01a113e467767eb9a4c33cb1df1cdfbb' 无违规前缀
⚠️  [c] 分支 'task/01a113e467767eb9a4c33cb1df1cdfbb' 仍被 worktree 占用（未清理）
❌ 拒绝: worktree 占用默认拒绝 —— 请先: git worktree remove <路径>
ℹ️  [d] 分支 'task/01a113e467767eb9a4c33cb1df1cdfbb' 尚未 merge 进 master
✅ [e] ref 存在性: refs/heads/task/01a113e467767eb9a4c33cb1df1cdfbb 可解析
✅ [f] 历史相关性: 'task/01a113e467767eb9a4c33cb1df1cdfbb' 与 master 有共同祖先
❌ safe-merge: 存在拦截项 —— 禁止 merge 'task/01a113e467767eb9a4c33cb1df1cdfbb' (拒绝种类: worktree-occupied)
exit=1

$ git tag -l 'archive/01a113e4*'
(empty)
```

`exit=1` is asserted from the **script's own status**, not a pipeline's — §5's
transcript note about the pipe. The blocker is a **live ground**, not a missing
tag and not a missing merge: `[a]`/`[b]`/`[e]`/`[f]` pass, `[c]` is the refusal,
`[d]` explains why the branch is legitimately un-landed.

### 12.6 The live ground — the count is a snapshot, the ground is not

```
$ git worktree list
/home/user/work/GoPi                                        fb9835b55 [master]
/dev/shm/gopi-masterchk                                     93848040e (detached HEAD)
/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01  90b9355a0 [task/01a113e467767eb9a4c33cb1df1cdfbb]
/home/user/workspace/gopi-01a115c3fc807807aaff5c56272638ac  0864d9dda [task/01a115c3fc807807aaff5c56272638ac]
/home/user/workspace/gopi-01a1161e-fix                      5276068c1 (detached HEAD)
/home/user/workspace/gopi-01a1161e-verify                   5276068c1 (detached HEAD)

$ pgrep -c -f gopi-01a115bbfe1e72518ba40698a3ba5a01
5
$ gopi task list | grep 01a115bb
#01a115bb-fe1e-7251-8ba4-0698a3ba5a01  [in_progress]  resolve merge conflict: rework: …
```

The worktree is on the branch's **newest** tip `90b9355a0` — the holder works
there, so the branch moving is the holder moving. The process count reads 9 (§10.6),
1 (§11.4), 5 now: it is a snapshot of a live run's process table and **no reading of
it decides anything**. The invariant that does decide is the pair above: the row is
`in_progress` and the worktree is linked to this ref.

### 12.7 What this round changes

Nothing in the disposition — and that is the finding, not a lack of one. The
report is complete for its scope: 7 content commits + 1 merge ruled (§2,
re-stated §12.2), the class measured over all 18 `+` commits (§12.3), the rescue
landed verbatim (§12.4), the archive blocked by a live ground whose owning row is
the **parent** of the row that will do the archive (§12.5, §4.3). The one thing a
later reader must not do is quote `24`, `18` or `5` as a verdict: the ref is alive,
its owner holds the ground, and the ruling was made on the **class**, which those
numbers cannot change.

### 12.8 Close-out re-read, after this section was committed

The record's own gate, taken once the §12 commit (`08cb57979`) was in, in one
pass at master `fb9835b55` / orphan `90b9355a0` (transcript 32 lines:
`~/.gopi/sessions/01a115c3-…/artifacts/orphan-01a113e4-closeout-sweep.txt`):

```
[1] orphan ref: 90b9355a03e96fd611aedaf3a7a129eb09b42ef7   (cherry 18 lines / 18 '+')
[2] is-ancestor orphan->master: exit=1        ← still uncovered at the REF level
[3] master:devdoc/voice-input-capture-rig.md = 127f5130cb18a49fddad09e5d234fdbb4f401417
[4] git branch -D → exit=1 (checked out at …/gopi-01a115bbfe1e72518ba40698a3ba5a01)
[5] safe-merge.sh → exit=1 (拒绝种类: worktree-occupied)
[6] archive tags: []
[9] this run porcelain: (end)                 ← empty, Rule 30 satisfied
[10] doccheck: exit=0
[11] non-devdoc paths in '+' commits: (none)  ← §12.3's census re-confirmed
```

The two `exit=1`s are the SAME refusal §12.5 measured, on the same entry points,
with the same cause — the live ground — and `[9]` is this run's own gate closing
clean. Nothing in the disposition moved; the ref hygiene remains with row
`01a11676-c490-7ab3-a664-91cfa9b6408a`, whose parent is the holder row.

---

## 13. Final sweep, and the one new measurement — the holder's run is a bounded one

Taken in ONE pass at `2026-10-07T21:34:18+08:00`, run branch `15d3d569e` /
master `fb9835b55` / orphan ref `90b9355a0`. Raw transcript, 44 lines:

```
~/.gopi/sessions/01a115c3-fc80-7807-aaff-5c56272638ac/artifacts/orphan-01a113e4-section13-final.txt
```

**A–B. The orphan status, unchanged at the REF level** — every number below was
re-measured here, not carried from §11/§12:

```
$ git rev-parse --verify --quiet refs/heads/task/01a113e467767eb9a4c33cb1df1cdfbb
90b9355a03e96fd611aedaf3a7a129eb09b42ef7                                   exit=0
$ git cherry master <branch> | wc -l          → 18
$ git cherry master <branch> | grep -c '^+'   → 18
$ git merge-base --is-ancestor <branch> master → exit=1
$ git rev-list --count master..<branch>       → 24      (snapshot; §10 read 21, §11 24)
$ git rev-list --count <branch>..master       → 0       (nothing of master is missing from it)
```

**C. The CONTENT level, still covered** — this is the reading that makes the
whole disposition usable, and it is re-taken rather than quoted:

```
$ git rev-parse master:devdoc/voice-input-capture-rig.md
127f5130cb18a49fddad09e5d234fdbb4f401417
$ git rev-list master --objects | grep -c 127f5130cb18a49fddad09e5d234fdbb4f401417 → 1
$ git cat-file blob 127f5130cb18a49fddad09e5d234fdbb4f401417 | sha256sum  → 9c4671fb4feef0ce…
```

Blob `127f5130c`, sha256 `9c4671fb…` — byte-identical to what the rescue wrote
and to the branch's own last revision of the document (§2 row 7). **REF level
uncovered, CONTENT level covered**: the two are stated apart because either one
alone is a wrong answer about this branch.

**D. The class census, re-run whole** (all 18 `+` commits, `--format=` so no
commit header leaks in):

```
    11 devdoc/merge-resolution-voice-input-rig-conflict.md
     6 devdoc/voice-input-default-local.md
     3 devdoc/DOC-INDEX.tsv
     1 devdoc/voice-input-local-capture-rig.md
non-devdoc paths = 0
```

Zero Go, cmd, scripts or test paths ⇒ nothing on this branch is a build input,
which is why the rescue's scope was two devdoc paths and the suite scope was
`./internal/docindex ./internal/standards` (§9).

**E. The archive entry points, verbatim, both refusals reproduced** — the
reference for the row that will actually do the deed:

```
$ git tag -l 'archive/*01a113e4*'
(n=0)                                                     ← no tag alive; nothing半-written
$ git branch -D task/01a113e467767eb9a4c33cb1df1cdfbb
error: Cannot delete branch 'task/01a113e467767eb9a4c33cb1df1cdfbb' checked out at
 '/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01'      branch -D exit=1
$ bash scripts/safe-merge.sh task/01a113e467767eb9a4c33cb1df1cdfbb
✅ [a] archive tag: 无 archive/task/01a113e467767eb9a4c33cb1df1cdfbb —— 未存档
✅ [b] 分支名黑名单: 无违规前缀
⚠️  [c] 分支仍被 worktree 占用（未清理）
❌ 拒绝: worktree 占用默认拒绝 —— 请先: git worktree remove <路径>
ℹ️  [d] 分支尚未 merge 进 master
✅ [e] ref 存在性 …可解析
✅ [f] 历史相关性 …与 master 有共同祖先
❌ safe-merge: 存在拦截项 (拒绝种类: worktree-occupied)              safe-merge exit=1
```

`[a]` reads "未存档" — measured, not assumed: the tag this run created and
withdrew in §4.1/§7 is gone, so the guard sees a clean, unarchived branch. The
single blocking criterion is `[c]`, and its cause is one level down:

```
$ git worktree list
/home/user/work/GoPi                                        fb9835b55 [master]
/dev/shm/gopi-masterchk                                     93848040e (detached HEAD)
/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01  90b9355a0 [task/01a113e467767eb9a4c33cb1df1cdfbb]
/home/user/workspace/gopi-01a115c3fc807807aaff5c56272638ac  15d3d569e [task/01a115c3fc807807aaff5c56272638ac]
$ holder procs by cwd (/proc/<pid>/cwd scan) → 1
$ gopi task list --all | grep -E '^#01a115bb|^#01a11676'
#01a11676-…  [pending]     归档孤儿分支 …（内容已 rescue，只剩 ref 卫生）
#01a115bb-…  [in_progress] resolve merge conflict: rework: task/01a113e4…   [running 51m7s since 20:43:14]
                                    [via worker] [ground task/01a113e4… @ …/gopi-01a115bbfe1e72518ba40698a3ba5a01]
```

**The one new measurement this round carries**: the holder row reports
`running 51m7s since 20:43:14` — it is a **worker run with a start time**, i.e.
a bounded run, not a residency. That is worth recording because it changes what
the successor row should expect: at the moment of this reading the ground is
alive and owned (`in_progress`, a worktree on the exact ref, one process standing
in it), and the parent gate on `01a11676-c490-…` releases on the holder's
*terminal* status — **not** on elapsed time, and never by force (Rule 33 has no
override, and Rule 29 forbids taking another run's ground). Whether the holder
finishes or is reclaimed, the successor's first action is the same: re-measure
`git rev-parse --verify --quiet refs/heads/task/01a113e4…` and
`git worktree list` **in the same pass** before touching anything — a tip read
here (`90b9355a0`) is a snapshot, and this branch moved under four separate
readings during this run (§10 `21` unique commits → §11 `24` → §13 `24` unique,
tip `7046c2957` → `72a3746dc` → `90b9355a0`).

**F. This run's own gate (Rule 30)**, measured after every edit of this section
was committed: `git status --porcelain` → **empty**.

**G. The close-out reading, taken once §13 was committed** (`bcfe43bbb`, master
`fb9835b55`, 2026-10-07T21:35:42) — two minutes after A–F, and **two readings
moved inside that window**. Raw transcript, 12 lines:
`artifacts/orphan-01a113e4-close-final.txt`.

```
orphan ref            90b9355a0  →  21e9563a225c3c23912f782167da8f3e00f26768   (exit=0)
master:devdoc/voice-input-rig-orphan-disposition-01a113e4.md  →  351a35f199a17d1f3efd0b53d8e06f2fa6998903
                                       (this run's branch carries abeaffe3a — newer)
git diff --name-status master...HEAD  →  M devdoc/voice-input-rig-orphan-disposition-01a113e4.md
archive tags n=0    master rig blob 127f5130c    porcelain lines=0    doccheck exit=0
followup row 01a11676-c490-…  status: pending
```

Two facts worth carrying, both measured here rather than reasoned:

1. **The orphan ref moved again** (`90b9355a0` → `21e9563a2`) while this section
   was being written — the fifth distinct tip observed in this run. It confirms
   §13's warning at full strength: any sha quoted in a briefing is stale on
   arrival, so the successor takes `TIP=$(git rev-parse $BR)` **in the same pass**
   as the delete.
2. **master now also carries this disposition document** (`351a35f19`), and
   `git diff --name-status master...HEAD` shows the run's branch adding exactly
   one path on top of master — this document's newer revision — while the rescued
   rig doc and the DOC-INDEX rows are identical on master. So, at this reading,
   everything this epic produced is on master except this section; the successor
   row appends its §14 there directly, which is the cheaper hand-over and the one
   its revised briefing prescribes.

---

## 13.H The 19th commit, ruled in this pass — and why the count keeps being a snapshot

The final pass (`2026-10-07T21:36:28+08:00`, run tip `fc72afb99`, master
`fb9835b55`) read **19** `+` commits where the §13 sweep two minutes earlier read
**18**. The newcomer is `21e9563a225c3c23912f782167da8f3e00f26768` — which is also
what the orphan ref itself now points at (the fifth distinct tip in this run).
It is ruled here, in this pass, on its own measurement rather than on the class
alone:

```
$ git show 21e9563a2 --format= --name-only
devdoc/merge-resolution-voice-input-rig-conflict.md
$ git show 21e9563a2 --format= --numstat
19	9	devdoc/merge-resolution-voice-input-rig-conflict.md
$ git log -1 --format='%ci %s' 21e9563a2
2026-10-07 21:34:42 +08:00 docs(merge-resolution): the honest test reading — 8+5 master reds,
                            the doc gates green, both filings named
```

**Ruling: not rescued.** Same class as §12.1's two and §8.1's three — the holder
run's own sync/test record, written while that run is still alive, on a path that
is `ABSENT` on master **and should be** (it is one run's log, not a shared
document; freezing it into a second copy is exactly the mistake Rule 23/26's
"a live author's record cannot be frozen" boundary is about).

Raw transcript of this ruling, 22 lines (the `--numstat`, the census over the
whole set, and `git merge-base --is-ancestor 21e9563a2 master` → exit 1 —
measured absent, as a live run's log must be):

```
~/.gopi/sessions/01a115c3-…/artifacts/orphan-01a113e4-section13H.txt
```

The class census re-run over the whole set at this reading:

```
    12 devdoc/merge-resolution-voice-input-rig-conflict.md
     6 devdoc/voice-input-default-local.md
     3 devdoc/DOC-INDEX.tsv
     1 devdoc/voice-input-local-capture-rig.md
non-devdoc = 0
```

**The generalisation §2/§9.4/§12.3 were building toward, stated once here**: the
per-sha total is a **snapshot** — it moved 7 → 9 → 12 → 16 → 18 → 19 across six
readings inside this one run, because a live run owns the branch and keeps
committing to it. The **class** is what is stable: every `+` commit touches only
`devdoc/`, `non-devdoc = 0` at every one of those six readings, so **nothing on
this branch is a build input and nothing on it after `dba11bb24` is a
deliverable** — from `c6ef3ed` onward the additions are one live run's log. A
successor that finds 20, 25 or 40 commits should re-run the census and expect the
same two lines; a successor that finds a **non-devdoc path** has found something
this disposition never ruled and must decide it afresh rather than inherit this
verdict.

---

## 14. RESOLVED — the holder merged, the ref is gone, and the archive artifact now exists

This section is the closing measurement. **Between §13.H (21:36:55) and this
reading (21:40:50) the holder run finished and its branch was merged into master**
— so the one item this record carried as "Not Done, blocked by a live ground"
resolved itself, in the direction the disposition had predicted, and the archive
step became executable. Measured state, raw transcript 32 lines:
`artifacts/orphan-01a113e4-section14-resolved.txt`.

### 14.1 The ref is gone, and REF-level coverage flipped

```
$ git rev-parse --verify --quiet refs/heads/task/01a113e467767eb9a4c33cb1df1cdfbb
                                                                        exit=1  ← ABSENT
$ git rev-parse 21e9563a225c3c23912f782167da8f3e00f26768   → same sha, type=commit
$ git rev-parse --short master                              → 21e9563a2   ← master IS that commit
$ git merge-base --is-ancestor 21e9563a2 master             → exit=0  ← NOW COVERED
$ git log --oneline -1 master
21e9563a2 docs(merge-resolution): the honest test reading — 8+5 master reds, …
```

So the **REF-level** reading that stood at `exit=1` through §1/§6/§8/§10/§11/§12/
§13 — five sweeps — flipped to `exit=0` without a single action from this run:
the holder's own merge did it. The last tip this record ever measured
(`21e9563a2`, §13.H) **is** the commit master now carries; `git cherry master
<branch>` no longer answers (`unknown commit`), because the ref it would resolve
no longer exists — which is the strongest possible form of "covered".

**The `merge-resolution…conflict.md` path, ruled *not rescued* in five separate
sections, is on master now** (`git rev-parse
master:devdoc/merge-resolution-voice-input-rig-conflict.md` → `b21f5f395`). That
is not a reversal of the ruling — it is the ruling's point arriving on time: the
content belonged to a **live author**, so it was carried by that author's own
merge rather than frozen into a second copy by this run. A rescue here would have
produced a competing copy of a document that landed 4 minutes later by itself.

### 14.2 The archive artifact, created now that the ref was released (Rule 9)

```
$ git tag archive/01a113e467767eb9a4c33cb1df1cdfbb-20261007 21e9563a225c3c23912f782167da8f3e00f26768
                                                                          exit=0
$ git rev-parse archive/01a113e467767eb9a4c33cb1df1cdfbb-20261007
21e9563a225c3c23912f782167da8f3e00f26768        ← == the archived tip, by string comparison: YES
$ git merge-base --is-ancestor <tag> master   → exit=0
$ git show archive/…-20261007:devdoc/voice-input-capture-rig.md | sha256sum
9c4671fb4feef0ce…                              ← the rescued rig doc, recoverable through the tag
$ git branch -D task/01a113e467767eb9a4c33cb1df1cdfbb
error: branch 'task/01a113e467767eb9a4c33cb1df1cdfbb' not found.            exit=1
$ git rev-parse --verify --quiet refs/heads/task/01a113e4…                  exit=1  ← ABSENT
$ git branch       → +master  *task/01a115c3fc807807aaff5c56272638ac  +task/01a11683342375389a4a41dc8372df7f
$ git worktree list → 6 entries, none on the orphan ref
```

Step 2 of Rule 9 (`git branch -D`) reports `not found` — the ref had already been
deleted by the machinery that landed the merge. That is a **satisfied precondition
reported honestly**, not a skipped step: the post-condition the epic asks for
(`refs/heads/task/01a113e4…` gone, an `archive/` tag at the original tip) is
measured above, in the same pass, by exit code and by string equality.

**Note the tag's spelling and what it does not do**: the brief the epic quotes
spells the tag `archive/01a113e4…-<date>`, and §4.1/§7 measured that
`scripts/safe-merge.sh`'s `[a]` arm matches only the exact `archive/<branch>`
string — so this dated tag does **not** exempt the branch name from the merge
guard. That is now harmless (no ref of that name exists to guard), and it is the
reason this run never wrote the undated spelling while a live ground held the ref
(§7: the undated tag IS a merge-lane switch, read by four production sites).

### 14.3 The recovery spelling, measured one final time — with the trap intact

The epic's acceptance names `git show archive/…:devdoc/voice-input-default-local.md`
as the recovery command. Measured against the tag just created:

```
$ git show archive/01a113e4…-20261007:devdoc/voice-input-default-local.md | sha256sum
4e301c0947a677ad…        size 44943 bytes
```

That is **master's design document** for the voice-input feature — *not* the rig
measurement record. The two documents share a path on the orphan branch and differ
everywhere else, which is what caused the original merge conflict (§2). The
recovery that returns the rescued content is:

```bash
git show archive/01a113e467767eb9a4c33cb1df1cdfbb-20261007:devdoc/voice-input-capture-rig.md \
  | sha256sum                      # → 9c4671fb4feef0ce…
git show master:devdoc/voice-input-capture-rig.md     # same bytes, blob 127f5130c
```

`master:devdoc/voice-input-local-capture-rig.md` (the orphan's own path) does
**not** exist on master — the rescue deliberately used the non-colliding
`voice-input-capture-rig.md`; that path's content is reachable through the tag.
Both spellings are recorded in §13.H's trap #1 and in the successor row's briefing.

### 14.4 What is left, and what this run does not do

- **Nothing of this epic's own acceptance remains open**: per-commit ruling (19
  ruled across §2/§6/§8.1/§9.1/§11.2/§12.1/§13.H, class census `non-devdoc = 0` at
  six readings), rescue (on master, `127f5130c`), archive tag (created, points at
  the original tip), ref deleted, `git branch` / `git worktree list` clean of it,
  this run's worktree clean.
- **Row `01a11676-c490` is now moot** — its whole subject ("只剩 ref 卫生") is
  measured done above, and it was filed against a blocker (the live ground) that
  ceased to exist. It is cancelled with a reason naming this section, rather than
  left queued to re-derive a finished state (Rule 26's honest-close form).
- **This run does not merge its own branch** (Rule 6e), and writes nothing outside
  its own worktree — the main checkout `/home/user/work/GoPi` was never touched
  (Rule 29).

---

## Completed

- Per-commit ruling for the branch — **every `+`-carrying commit is ruled, and
  the count is stated at the reading it was taken**: §2's seven content commits
  (plus the layout merge as row 8, so 8 sha carry a row and the 7/8 split is
  stated rather than blurred), §6's two, §8.1's three (`f92a015ca`, `b26ede8e9`,
  `ad5bdbe5c`), §9.1's one (`c287c58d2`), the three that arrived later
  (`8191b4d0f`, `fbe6ed9fa`, `7046c2957`), and §12.1's two (`b26abfe0b`,
  `90b9355a0`) — 18 at the acceptance tip, all `+`. **Beyond the individual rows
  the ruling is by CLASS, and §9.4 measures it, §12.3 proves it over the whole
  set**: with a `git show --name-only` loop over every `+` sha, all 18 touch
  `devdoc/` and nothing else (`grep -v '^devdoc/'` → no output), and every commit
  from `c6ef3ed` onward touches only the holder's own
  `merge-resolution…conflict.md` (plus at most its index row) — so the class
  verdict is *not rescued*: a live author's sync record cannot be frozen into a
  second copy. Each ruled commit carries the measured content, the today-answer
  and the reason; the rescue itself is on master at the non-colliding path
  (blob `127f5130c`).
- Rescue branch `feat/rescue-01a113e4-capture-rig` off master carrying the rig
  document verbatim (blob `127f5130c`, sha256 `9c4671fb…`) at the non-colliding
  path `devdoc/voice-input-capture-rig.md`, plus its `DOC-INDEX.tsv` row written
  by `gopi tool doccheck --fix`.
- The archive mechanism executed end to end (§4.2): `archive/…` tag created at
  the measured tip; `git branch -D` refused by the live merger worktree with the
  refusal quoted; the tag withdrawn with two measured reasons (§4.2).
- The re-test recipe for "does master cover the branch" (§5) — three commands min.
- **The supported archive verb measured against this ref (§7)**, which is the
  round's most useful finding: `gopi task archive <branch>` **proceeds** on a ref
  whose head is a live run's ground, because the gate resolves the branch to an
  OLDER cancelled row (§7.1); `archive/<branch>` is a merge-exemption **switch**
  read by four production readers (`repoSweepArchiveExempt`), not a passive marker
  (§7.2); the tag **arms** a head-deleting arm whose `task/`+worktree guards live
  in the delete arm, not the tag arm (§7.3). Both refs written were withdrawn in
  the same round with the commands quoted (§7.4), and the defect is filed as
  `01a1167e-38c3-70e0-be1c-f6ed304fe851` (`--tags bug`).
- The open ROW is named (§4.3) with the exact shell that finishes the archive,
  **and filed as `01a11676-c490-7ab3-a664-91cfa9b6408a`** with
  `parent_task_id = 01a115bb-fe1e-7251-8ba4-0698a3ba5a01`, so the archive
  completes without anyone re-deriving this record (Rule 26).

**The two readings a reader must not quote alone** (both measured this round,
both in the pull direction of "the old path still has the rig doc"):

```
$ git rev-parse <branch>:devdoc/voice-input-local-capture-rig.md   → 127f5130c (25342 B)
$ git rev-parse <branch>:devdoc/voice-input-default-local.md       → 4bfa96995 (44943 B)
$ git rev-parse master:devdoc/voice-input-default-local.md         → 4bfa96995 (44943 B)

$ git show <branch>:devdoc/voice-input-local-capture-rig.md | sha256sum → 9c4671fb…   ← the rig doc
$ git show <branch>:devdoc/voice-input-default-local.md     | sha256sum → 4e301c09…   ← master's, not the rig doc
$ git show master:devdoc/voice-input-default-local.md       | sha256sum → 4e301c09…   ← identical
```

The branch tip's **old path holds master's design document**, not the branch's
record: the merge `a7c192187` moved the branch's bytes to the new path and took
master's at the old one. So a recovery command written as "read the branch's old
path" silently returns the wrong document — and the acceptance spelling
`git show archive/…:devdoc/voice-input-default-local.md` returns that same wrong
document once the tag lands. The rig record is recoverable at
`devdoc/voice-input-local-capture-rig.md` on the branch, and at
`devdoc/voice-input-capture-rig.md` on the rescue.

## Not Done

- **~~The branch ref was not deleted, and no `archive/` tag survives~~ — RESOLVED
  at the close of this record (§14).** Kept as the honest account of the rounds
  that hit the gate: the named blocker was `01a115bb-fe1e-7251-8ba4-0698a3ba5a01`
  (`in_progress`), which held the branch as its ground — re-measured at the newest
  tip (§9.1: `git branch -D` → exit 1, `safe-merge.sh` → exit 1
  `worktree-occupied`, the holder worktree still listed, 8 live processes in it,
  the row's own 181-turn run unfinished). And §7 measured that the *supported* verb
  does **not** honour that blocker: it decided the ref by an older cancelled row and
  wrote the archive tag. So "the verb refuses on a live row" is false as stated, and
  the honest sentence was: this run withdrew both the tag it wrote by hand and the
  tag the verb wrote, and filed the mechanism's behaviour as a bug
  (`01a1167e-38c3-70e0-be1c-f6ed304fe851`). **Then the holder finished and merged its
  own branch, and §14 measures the resolution**: `refs/heads/task/01a113e4…` is
  ABSENT (`rev-parse --verify --quiet` exit=1), `master` = `21e9563a2` = the last tip
  this record measured, REF-level coverage flipped to exit=0, and the Rule 9 archive
  tag `archive/01a113e467767eb9a4c33cb1df1cdfbb-20261007` was created at that tip
  (`git rev-parse` = `21e9563a2`, recovery through it verified by sha256).
- **Was the rescue's content not on master? — it is now, and this run observed
  it.** §3/§5 recorded the pre-landing reading (`ABSENT-on-master`); the merger
  landed the branch as `24c3c5537` while this record was being written, and §8.1
  carries the post-landing reading (`PRESENT`, blob `127f5130c` at
  `devdoc/voice-input-capture-rig.md`, `git rev-list master --objects | grep -c`
  → `1`). So the archive precondition §3.2 cites **is** now satisfied by
  measurement — what still blocks the archive is not content, it is the live
  ground (§8.3). The lines below are kept as the pre-landing record, not as the
  current state:

  ```
  $ git rev-list master --objects | grep -q 127f5130cb18a49fddad09e5d234fdbb4f401417 \
      && echo PRESENT || echo ABSENT
  ABSENT-on-master
  $ git rev-parse master:devdoc/voice-input-capture-rig.md
  fatal: path 'devdoc/voice-input-capture-rig.md' exists on disk, but not in 'master'
  ```

  §4.3 therefore hands the follow-up row the check **and** the landing
  (`git merge --ff-only feat/rescue-01a113e4-capture-rig`) rather than assuming
  the merge happened. **Post-landing update (§8): the merger performed that merge
  — `24c3c5537` — so the follow-up's landing instruction is already satisfied;
  only the ref hygiene remains.**
- **Not merged.** This run merged nothing (Rule 6e: the agent does not merge its
  own branch). The rescue branch is handed over.
- **The overlap with `c47fdddbe` (peer) is not resolved by this run** — two
  hosts now carry the same bytes at the same path. Content-wise a no-op, but the
  row/branch bookkeeping is the merger's to settle.
- **No product code was rescued, because the branch carries none** — that is a
  finding (§1.2), not a deferral.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0, binary
  `/home/user/workspace/gopi-01a115c3fc807807aaff5c56272638ac/bin/gopi`
  (64407360 bytes); `/home/user/workspace/…/bin/gopi -h` printed the usage head.
- Tests: `make test-budget PKGS='./internal/docindex ./internal/standards'` →
  **exit=0, `2 finished, 0 with a failure`**, 106 s wall
  (stream `run-1791377939-124318.json`; stamp
  `host=peerbox pkg=./internal/docindex ./internal/standards exit=0 outcome=pass`).
  These are the packages this change's mechanism lives in — the doc-corpus
  reader and the standards gates (including Rule 36's manifest coverage). The
  branch carries **no Go change** (§1.2), so no other package's behaviour is in
  the diff.
- Ran: the real verb that gates this change —
  `gopi tool doccheck` (exit 1, naming the unclassified file) then
  `gopi tool doccheck --fix` (exit 0, `repaired 1 path(s)`), followed by
  `gopi tool doccheck` again as the closing read.
- Ran (git, verbatim): `git cherry master task/01a113e4…` (7× `+`),
  `git diff --name-status master...task/01a113e4…` (2 paths),
  `git rev-list --count task/01a113e4… ^master` (8),
  `git show task/01a113e4…:devdoc/voice-input-local-capture-rig.md > devdoc/voice-input-capture-rig.md`,
  `git rev-parse HEAD:devdoc/voice-input-capture-rig.md` (= `127f5130c`),
  `git tag archive/01a113e4…-20261007 task/01a113e4…` (exit 0),
  `git branch -D task/01a113e4…` (exit 1, refusal quoted in §4.2).
- Commit: this document, the rescued rig doc and its index row — see the
  round's commit on `feat/rescue-01a113e4-capture-rig`.

### §8 round — re-run after the rescue landed (this commit)

Every figure in §8 was taken in one reading at master
`39c23372023b3c1f82479ca275eb8af28ba88e03` / branch
`4d527593cbbf80e9a34a16b740d45401cabf80b0`:

- Ran: `git rev-parse master` → `39c233720…`; `git rev-parse
  task/01a113e467767eb9a4c33cb1df1cdfbb` → `4d527593c…`;
  `git diff --name-status master task/01a113e4…` → `M devdoc/DOC-INDEX.tsv`,
  `A devdoc/merge-resolution-voice-input-rig-conflict.md`;
  `git log --no-merges --oneline master..task/01a113e4… | wc -l` → `12`;
  `git log --merges --oneline master..task/01a113e4… | wc -l` → `3`;
  `git cherry master task/01a113e4… | grep -c '^+'` → `12`.
- Ran (the landing): `git log -1 --format='%H%n%ci%n%s' master` →
  `24c3c5537e48412671770bf978ccb0656465d16d` / `2026-10-07 21:10:35 +0800` /
  `Merge branch 'feat/rescue-01a113e4-capture-rig'`.
- Ran (content coverage): `git rev-parse master:devdoc/voice-input-capture-rig.md`
  → `127f5130cb18a49fddad09e5d234fdbb4f401417`;
  `git rev-list master --objects | grep -c 127f5130cb18a49fddad09e5d234fdbb4f401417`
  → `1`; `git merge-base --is-ancestor task/01a113e4… master` → exit 1;
  `git rev-parse task/01a113e4…:devdoc/merge-resolution-voice-input-rig-conflict.md`
  → `9b65da41e7fc32c71ccac21eb73cb81b31f21168`.
- Ran (the two refusals, re-measured at this tip, both by their real entry
  point): `git branch -D task/01a113e467767eb9a4c33cb1df1cdfbb` → **exit 1**,
  `error: Cannot delete branch 'task/01a113e4…' checked out at
  '/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01'`;
  `bash scripts/safe-merge.sh task/01a113e467767eb9a4c33cb1df1cdfbb` →
  **exit 1** (not the `0` a pipeline would have reported),
  `❌ safe-merge: 存在拦截项 —— 禁止 merge … (拒绝种类: worktree-occupied)`.
- Ran (the live ground, by measurement not inference): `git worktree list` → the
  holder worktree at `4d527593c` on the orphan branch; a `/proc/*/cwd` scan
  pinned 13 live processes inside it, the first being
  `timeout 900 make test-budget PKGS=./cmd/agent`.
- Ran (this round's build + branch-scoped smoke):
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0, and
  `make test-budget PKGS='./internal/docindex ./internal/standards'` — the two
  packages this record's mechanism lives in (`./internal/docindex` is the corpus
  reader `gopi tool doccheck` calls; `./internal/standards` carries Rule 36's
  manifest-coverage gate) → **exit 0, 0 failures**.
- Ran (the gate this change must satisfy): `gopi tool doccheck` → **exit 0**,
  `the devdoc corpus is consistent — every file under devdoc/ has a well-formed
  row`. (Its retired bare spelling `gopi doccheck` prints a pointer and exits 1
  — quoted here so the next reader does not read that as a corpus failure.)
- Ran (close-out): `git status --porcelain` in this run's worktree → prints
  nothing after this commit (Rule 30).

### §9 round — the fourth reading, at master `39711d146` / branch `8f8325e64`

All of §9's figures were taken in ONE pass and written to
`~/.gopi/sessions/01a115c3-fc80-7807-aaff-5c56272638ac/artifacts/orphan-01a113e4-section9-evidence.txt`
(96 lines, every command with its output):

- Ran: `git rev-parse master` → `39711d1464130df5bdff4df8e6f8df3d6a7a1528`;
  `git log -1 --format='%h %ci %s' master` → `39711d146 2026-10-07 21:20:13 +0800
  Merge branch 'feat/rescue-01a113e4-capture-rig'`; `git show --format='%H%n%P'
  --no-patch 39711d146` → second parent `9b3ecf23d` (this record).
- Ran: `git rev-parse task/01a113e467767eb9a4c33cb1df1cdfbb` →
  `8f8325e64369456881e6404900f3d921b8679a37`; `git log -1 --format='%h %p %ci %s'`
  → `8f8325e64 c287c58d2 39711d146 … Merge branch 'master' into task/01a113e4…`;
  `git log --no-merges --oneline master..<branch> | wc -l` → `13`;
  `git log --merges --oneline master..<branch> | wc -l` → `5`;
  `git cherry master <branch> | grep -c '^+'` → `13`;
  `git diff --name-status master <branch>` → `M devdoc/DOC-INDEX.tsv`,
  `A devdoc/merge-resolution-voice-input-rig-conflict.md`.
- Ran (the 13th commit, ruled in §9.1): `git show --format='%H%n%P%n%an %ci%n%s'
  --stat c287c58d2` → `+19/−4` in the holder's merge-resolution record.
- Ran (content coverage at the newer master):
  `git rev-parse --verify master:devdoc/voice-input-capture-rig.md` →
  `127f5130cb18a49fddad09e5d234fdbb4f401417` exit 0;
  `git rev-list master --objects | grep -c 127f5130c…` → `1`;
  `git merge-base --is-ancestor task/01a113e4… master` → exit 1;
  `git cat-file -e master:devdoc/merge-resolution-voice-input-rig-conflict.md` →
  fatal (ABSENT, branch-unique).
- Ran (the two refusals at this tip): `git branch -D task/01a113e467767eb9a4c33cb1df1cdfbb`
  → **exit 1**, `error: Cannot delete branch … checked out at
  '/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01'`;
  `bash scripts/safe-merge.sh task/01a113e467767eb9a4c33cb1df1cdfbb` →
  **exit 1**, `❌ safe-merge: 存在拦截项 … (拒绝种类: worktree-occupied)` (exit
  measured on the script, not on a pipe).
- Ran (the live ground): `git worktree list` → the holder worktree at
  `8f8325e64` on the orphan branch; a `/proc/*/cwd` scan → **8 live processes**
  in it; `gopi task show 01a115bb-…` → `in_progress`, `0 completed, 1 with a
  raw-log checkpoint`, 181 turns.
- Ran (nothing armed): `git tag -l 'archive/01a113e4*'` → empty.
- Ran (this round's build + smoke at this tip):
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0;
  `make test-budget PKGS='./internal/docindex ./internal/standards'` → exit 0,
  stamp `2026-10-07T21:18:06+08:00 host=peerbox … exit=0 outcome=pass
  stream=run-1791379086-236075.json`.
- Ran: `gopi tool doccheck` → exit 0 (`the devdoc corpus is consistent`);
  `git status --porcelain` → empty (Rule 30).
- Ran (§9.3, the third reading, taken last): `git rev-parse
  task/01a113e467767eb9a4c33cb1df1cdfbb` → `fbe6ed9fa2406278894bcc458826e2f1b9c208a7`;
  `git cherry master <branch> | grep -c '^+'` → `15`;
  `git log --no-merges --oneline master..<branch> | wc -l` → `15`;
  `git show --format='%H%n%P%n%an %ci%n%s' --stat 8191b4d0f` → `+18` in
  `devdoc/merge-resolution-voice-input-rig-conflict.md`; same for `fbe6ed9fa` →
  `+6/−3` in the same file — the evidence for the **class** ruling in §9.1.
  `git rev-parse master` → `39711d146…` (unchanged across the three readings).
- Ran (§9.4, the class measurement): `for s in $(git cherry master <branch> |
  awk '$1=="+"{print $2}'); do git rev-parse --short=7 $s; git show --numstat
  --format= $s; done` → the 16 rows reproduced in §9.4, showing every commit from
  `c6ef3ed` onward touching only `devdoc/merge-resolution-voice-input-rig-conflict.md`
  (+ `DOC-INDEX.tsv`); and `git log --numstat --format='%h %ci %s'
  c287c58d2..<branch> --no-merges` → `8191b4d0f` `+18`, `fbe6ed9fa` `+6/−3`,
  `7046c2957` `+15/−8`, all in that one file.
- Ran (acceptance sweep, the commands the epic names, all in one pass at tip
  `e415d88f3`): `git rev-parse --verify --quiet refs/heads/task/01a113e4…` →
  `7046c2957…`, `exit=0` (the ref is **still present** — the archive is blocked,
  not done); `git tag -l 'archive/01a113e4*'` → empty; `git worktree list` →
  holder worktree at `7046c2957` on that branch; `gopi tool doccheck` → exit 0;
  `git status --porcelain` → empty.
  Full transcript:
  `~/.gopi/sessions/01a115c3-fc80-7807-aaff-5c56272638ac/artifacts/orphan-01a113e4-acceptance-sweep.txt`.

### §10 round — the acceptance sweep, re-measured in one pass (this commit)

Every figure in §10 was taken in one pass at master `fb9835b55` / branch
`7046c2957`; the full transcript (every command with its raw output, 114 lines)
is filed at
`~/.gopi/sessions/01a115c3-fc80-7807-aaff-5c56272638ac/artifacts/orphan-01a113e4-acceptance-sweep.txt`.

- Ran (both tips, at the moment of the sweep): `git rev-parse master` →
  `fb9835b55b4288a33eeebdaf64f39de3de8e496c`;
  `git rev-parse task/01a113e467767eb9a4c33cb1df1cdfbb` →
  `7046c2957276c93f54902daf2bf8ea5dfd86ac08`.
- Ran (the class, one pass, `git cherry master <branch> | awk '$1=="+"{print
  $2}'` through `git show --numstat --format=`) → the 16 rows in §10.2: seven
  commits on `devdoc/voice-input-default-local.md` (the rig document), one on
  `devdoc/DOC-INDEX.tsv`, eight on the holder's own
  `devdoc/merge-resolution-voice-input-rig-conflict.md`; `awk … | sort | uniq -c`
  → `16 +`, never a `-`.
- Ran (the two misleading readings, §10.3): `git diff --name-status master
  task/01a113e4…` → 12 paths incl. `D cmd/agent/chat_voice.go`,
  `git diff --stat master task/01a113e4…` → `12 files changed, 313 insertions(+),
  2231 deletions(-)` (master's own progress, not the branch's deletions);
  `git rev-list --count task/01a113e4… ^master` → `21` (merges included);
  `git merge-base master task/01a113e4…` → `39711d146…`. The two readings that
  hold: `git diff --name-status master...task/01a113e4…` → `M devdoc/DOC-INDEX.tsv`,
  `A devdoc/merge-resolution-voice-input-rig-conflict.md`.
- Ran (coverage, §10.4): `git rev-parse master:devdoc/voice-input-capture-rig.md`
  → `127f5130cb18a49fddad09e5d234fdbb4f401417` (PRESENT);
  `git rev-list master --objects | awk '{print $1}' | grep -c '^127f5130…$'` →
  `1`; `git merge-base --is-ancestor task/01a113e4… master` → **exit 1**;
  `git branch --list feat/rescue-01a113e4-capture-rig` → empty (pruned);
  `git merge-base --is-ancestor d0032b8e8 HEAD` → exit 0;
  `git rev-parse d0032b8e8:devdoc/voice-input-capture-rig.md` →
  `127f5130c…`.
- Ran (the archive, by its real entry points at this tip, §10.5):
  `git branch -D task/01a113e467767eb9a4c33cb1df1cdfbb` → **exit 1**,
  `error: Cannot delete branch … checked out at
  '/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01'`;
  `bash scripts/safe-merge.sh task/01a113e467767eb9a4c33cb1df1cdfbb` → **exit 1**,
  `❌ safe-merge: 存在拦截项 … (拒绝种类: worktree-occupied)`, with the script's own
  guard `[d]` printing `分支 … 尚未 merge 进 master`; `git tag -l
  'archive/01a113e4*'` → empty.
- Ran (the live ground at this tip, §10.6): a `/proc/*/cwd` scan → **9 live
  processes** inside the holder's worktree, among them
  `make test-budget PKGS=./internal/docindex ./internal/standards ./cmd/agent`;
  `git -C /home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01 rev-parse
  HEAD` → `7046c2957…` (the holder is AT the branch tip).
- Ran (this round's build + branch-scoped smoke, Rule 31):
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **exit 0**, and
  `make test-budget PKGS='./internal/docindex ./internal/standards'` →
  **exit 0**, `2 finished, 0 with a failure`, 56 s wall, stamp
  `2026-10-07T21:25:44+08:00 host=peerbox … exit=0 outcome=pass
  stream=run-1791379544-266082.json`.
- Ran (the gate this change must satisfy, after §10's edits): `gopi tool doccheck`
  → **exit 0**; `git status --porcelain` in this run's worktree → empty after the
  commit (Rule 30).

### §11 round — the final acceptance sweep, one pass (this commit)

§11's figures come from ONE `bash` invocation at master `fb9835b55` / this branch
`d19c6ac5d`; the transcript (72 lines, every command with its raw output) is
filed at
`~/.gopi/sessions/01a115c3-fc80-7807-aaff-5c56272638ac/artifacts/orphan-01a113e4-final-acceptance.txt`.

- Ran (the orphan test, §11.1): `git rev-parse --verify --quiet
  refs/heads/task/01a113e467767eb9a4c33cb1df1cdfbb` → `7046c2957276c93f54902daf2bf8ea5dfd86ac08`,
  **exit=0** (the ref is still there); `git cherry master task/01a113e4… | wc -l`
  → `16` and `… | grep -c '^+'` → `16` (all still unique); `git merge-base
  --is-ancestor task/01a113e4… master` → **exit 1**;
  `git rev-parse master:devdoc/voice-input-capture-rig.md` →
  `127f5130cb18a49fddad09e5d234fdbb4f401417`;
  `git rev-list master --objects | grep -c 127f5130cb18a49fddad09e5d234fdbb4f401417`
  → `1`.
- Ran (the rescue as delivered, §11.2): `git rev-parse --verify --quiet
  refs/heads/feat/rescue-01a113e4-capture-rig` → **exit 1** (the ref is gone, as
  §10.4 said, and the recovery line is corrected accordingly);
  `git rev-parse d0032b8e8:devdoc/voice-input-capture-rig.md` →
  `127f5130cb18a49fddad09e5d234fdbb4f401417` — the rescue commit still holds the
  bytes, and master's copy is the identical blob.
- Ran (the archive, by its real entry points, §11.3): `git branch -D
  task/01a113e467767eb9a4c33cb1df1cdfbb` → **exit 1**,
  `error: Cannot delete branch … checked out at
  '/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01'`;
  `bash scripts/safe-merge.sh task/01a113e467767eb9a4c33cb1df1cdfbb` → **exit 1**
  with the full six-guard report quoted in §11.3 (`[c]` worktree-occupied is the
  refusal; `[d]` confirms not-merged; `[a]`/`[b]`/`[e]`/`[f]` all pass); `git tag
  -l 'archive/01a113e4*'` → empty.
- Ran (the live ground, §11.4): `git worktree list` → six worktrees, the holder's
  at `/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01` labelled
  `[task/01a113e467767eb9a4c33cb1df1cdfbb]`; a `/proc/*/cwd` scan →
  `holder_procs=1` at this instant (against §10.6's 9 — the count is a snapshot,
  the `in_progress` row and the linked worktree are not); `gopi task show
  01a115bb-…` → `status: in_progress`, `runs: 1 run(s) — 0 completed, 1 with a
  raw-log checkpoint`, `ground (resolved now): … branch=task/01a113e4… (exists) —
  recorded ground — verified live`.
- Ran (the tip moving DURING the sweep, §11.5): the same `git rev-parse` a minute
  later → `72a3746dc309fd450324da2ea553cf812faea692`; `git log -1 --format='%an
  %ad%n%s'` → `gopi gopi@localhost Wed Oct 7 21:29:00 2026 +0800 / Merge branch
  'master' into task/01a113e467767eb9a4c33cb1df1cdfbb`; `git log --oneline
  7046c2957..72a3746dc` → 8 lines, of which 7 are **master's own** commits
  (`579634d25` `/voice`, `d8b02c4e2` lineedit, …); `git diff --name-status
  7046c2957..72a3746dc` → exactly master's six commits' file set. `git cherry`
  re-run → still `16`/`16`.
- Ran (this run's own gate, §11.6): `git status --porcelain` → **empty** at 21:27,
  and ` M devdoc/voice-input-rig-orphan-disposition-01a113e4.md` at 21:28:59 —
  i.e. §11 itself, mid-write (stated as such rather than presented as clean);
  `gopi tool doccheck` → **exit 0** (`the devdoc corpus is consistent — every file
  under devdoc/ has a well-formed row`); `grep -n 'voice-input'
  devdoc/DOC-INDEX.tsv` → rows 1946 and 1957, both `historical`, both written by
  the tool.
- Ran (this round's build + branch-scoped smoke, Rule 31):
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **exit 0**;
  `make test-budget PKGS='./internal/docindex ./internal/standards'` → **exit 0**
  (2 finished, 0 with a failure) — the packages this record's mechanism lives in.
- Ran (close-out, after this commit): `git status --porcelain` → empty (Rule 30).

### §12 round — the delta sweep after §11 (this commit)

§12's figures come from ONE `bash` invocation at master `fb9835b55` / orphan
`90b9355a0`; transcript (91 lines, every command with its raw output) at
`~/.gopi/sessions/01a115c3-fc80-7807-aaff-5c56272638ac/artifacts/orphan-01a113e4-section12-delta.txt`.

- Ran (the two new commits, §12.1): `git log --oneline
  72a3746dc..task/01a113e467767eb9a4c33cb1df1cdfbb` → `90b9355a0`, `b26abfe0b`;
  `git log --format='%h %s' --name-status 72a3746dc..task/01a113e4…` → each
  modifies `devdoc/merge-resolution-voice-input-rig-conflict.md` and nothing else.
- Ran (the census, §12.3): the per-commit `git show $s --format= --name-only`
  loop over all 18 `cherry` `+` shas → `11 merge-resolution…conflict.md`, `6
  voice-input-default-local.md`, `3 DOC-INDEX.tsv`, `1
  voice-input-local-capture-rig.md`; the same loop filtered through `grep -v
  '^devdoc/'` → **no output** (no non-devdoc path is touched anywhere).
- Ran (the re-test, §12.4): `git merge-base --is-ancestor task/01a113e4… master`
  → **exit 1**; `git merge-base --is-ancestor master task/01a113e4…` → **exit 0**;
  `git rev-list --count master..task/01a113e4…` → `24`; `git rev-list --count
  task/01a113e4…..master` → `0`; `git rev-parse master:devdoc/voice-input-capture-rig.md`
  → `127f5130cb18a49fddad09e5d234fdbb4f401417`; `git rev-list master --objects |
  grep -c 127f5130c…` → `1`.
- Ran (the archive block, §12.5): `git branch -D task/01a113e467767eb9a4c33cb1df1cdfbb`
  → **exit=1** (`error: Cannot delete branch … checked out at
  '/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01'`); `bash
  scripts/safe-merge.sh task/01a113e4…` → **exit=1**, refusal `worktree-occupied`,
  guards `[a][b][e][f]` pass, `[d]` not-merged; `git tag -l 'archive/01a113e4*'` →
  empty.
- Ran (the live ground, §12.6): `git worktree list` → the holder's worktree at
  `90b9355a0 [task/01a113e467767eb9a4c33cb1df1cdfbb]`; `pgrep -c -f
  gopi-01a115bbfe1e72518ba40698a3ba5a01` → `5`; `gopi task list | grep 01a115bb`
  → `[in_progress]`; the archive row `01a11676-c490-7ab3-a664-91cfa9b6408a` is
  still `pending` with the holder row as its parent.
- Ran (path-level, §12.3 table): `git rev-parse <ref>:<path>` for
  `voice-input-default-local.md` (`4bfa96995` on **both** branch and master — the
  collision), `voice-input-local-capture-rig.md` (ABSENT on both — deleted by its
  own author), `merge-resolution-voice-input-rig-conflict.md` (`5d2e8e515` on the
  branch, **`fatal: … not in 'master'`**).
- Ran (this round's build + branch-scoped smoke, Rule 31):
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **exit 0**;
  `make test-budget PKGS='./internal/docindex ./internal/standards'` → **exit 0**
  (2 finished, 0 with a failure; stream `run-1791379779-285339.json`).
- Ran (close-out, after this commit): `git status --porcelain` → empty (Rule 30).

### §12 round, close-out re-read — after the §12 commit

One `bash` pass at master `fb9835b55` / orphan `90b9355a0` / this branch
`08cb57979`; transcript (32 lines) at
`~/.gopi/sessions/01a115c3-fc80-7807-aaff-5c56272638ac/artifacts/orphan-01a113e4-closeout-sweep.txt`.

- Ran: `git rev-parse task/01a113e4…` → `90b9355a03e96fd611aedaf3a7a129eb09b42ef7`;
  `git cherry master task/01a113e4… | wc -l` → `18`, `grep -c '^+'` → `18`;
  `git merge-base --is-ancestor task/01a113e4… master` → **exit=1**;
  `git rev-parse master:devdoc/voice-input-capture-rig.md` →
  `127f5130cb18a49fddad09e5d234fdbb4f401417`.
- Ran: `git branch -D task/01a113e4…` → **exit=1** (`Cannot delete branch … checked
  out at '/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01'`);
  `bash scripts/safe-merge.sh task/01a113e4…` → **exit=1**, `拒绝种类:
  worktree-occupied`; `git tag -l 'archive/01a113e4*'` → `[]`.
- Ran: the §12.3 census once more (`grep -v '^devdoc/'` over every `+` commit) →
  **no output**; `git worktree list` → 7 lines including the holder's worktree at
  `90b9355a0 [task/01a113e467767eb9a4c33cb1df1cdfbb]`; `gopi task list` → the
  archive row `01a11676-c490…` still `pending`, the holder `01a115bb-…` still
  `in_progress`.
- Ran: `git status --porcelain` → **empty** (Rule 30); `./bin/gopi tool doccheck`
  → **exit=0**.
- Ran (this round's build + branch-scoped smoke, Rule 31):
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **exit 0**;
  `make test-budget PKGS='./internal/docindex ./internal/standards'` → **exit 0**.
- Ran (after this commit): `git status --porcelain` → empty.

### §13 / §13.H round — final sweep, and the 19th commit (this commit)

- Ran (§13, one pass at master `fb9835b55` / orphan `90b9355a0`, 44-line transcript
  `artifacts/orphan-01a113e4-section13-final.txt`): `git rev-parse --verify --quiet
  refs/heads/task/01a113e4…` → `90b9355a0…` `exit=0`; `git cherry master <B> | wc -l`
  → `18`, `grep -c '^+'` → `18`; `git merge-base --is-ancestor <B> master` →
  **exit=1**; `git rev-parse master:devdoc/voice-input-capture-rig.md` →
  `127f5130c` with `--objects` count `1`; `git tag -l 'archive/*01a113e4*'` → `(n=0)`;
  `git branch -D <B>` → **exit=1** (`checked out at …/gopi-01a115bbfe1e…`);
  `bash scripts/safe-merge.sh <B>` → **exit=1** `拒绝种类: worktree-occupied`;
  `/proc/<pid>/cwd` scan for the holder dir → `1`; `gopi task list --all` → holder
  `[in_progress] [running 51m7s since 20:43:14] [via worker]`; porcelain → empty.
- Ran (§13.H, 22-line transcript `artifacts/orphan-01a113e4-section13H.txt`): the
  census over all `+` commits → `12/6/3/1` devdoc paths, `non-devdoc = 0`;
  `git log -1 --format='%ci %s' 21e9563a2` → `2026-10-07 21:34:42 …`;
  `git show 21e9563a2 --format= --numstat` → `19  9`;
  `git merge-base --is-ancestor 21e9563a2 master` → **exit=1** (absent, as a live
  run's log must be).
- Ran (build + branch-scoped smoke, Rule 31): `CGO_ENABLED=0 go build -o
  /dev/shm/gopi-final3 ./cmd/agent/` → **exit 0** (`BUILD=OK`, job-12 at tip
  `e842e8750`); `make test-budget PKGS='./internal/docindex ./internal/standards'`
  → **exit=0**, `2 finished, 0 with a failure`, 47 s
  (stream `run-1791380540-306901.json`). Three earlier runs of the same scope this
  session also exited 0 (44 s / 44 s / 100 s).
- Ran: `gopi tool doccheck` → **exit=0** after every edit of these sections;
  `git status --porcelain` → **empty** after each commit (Rule 30).

### §14 round — the resolution, and the Rule 9 archive executed (this commit)

Transcript (32 lines + continuations):
`artifacts/orphan-01a113e4-section14-resolved.txt`.

- Ran: `git rev-parse --verify --quiet refs/heads/task/01a113e4…` → no output,
  **exit=1** ⇒ the ref is **ABSENT**; `git rev-parse 21e9563a2…` → the same sha,
  `git cat-file -t` → `commit`; `git log --oneline -1 master` → `21e9563a2` (the
  commit this record last measured as the orphan tip); `git merge-base --is-ancestor
  21e9563a2 master` → **exit=0** ⇒ REF-level coverage **flipped from uncovered to
  covered**; `git cherry master <B>` → `fatal: unknown commit …` (the ref it would
  resolve no longer exists).
- Ran (Rule 9, step 1): `git tag archive/01a113e467767eb9a4c33cb1df1cdfbb-20261007
  21e9563a225c3c23912f782167da8f3e00f26768` → **exit=0**;
  `git rev-parse archive/…-20261007` → `21e9563a225c3c23912f782167da8f3e00f26768`,
  string-equal to the archived tip; `git merge-base --is-ancestor <tag> master` →
  `exit=0`; `git show archive/…-20261007:devdoc/voice-input-capture-rig.md |
  sha256sum` → `9c4671fb4feef0ce…` (the rescued doc, recoverable through the tag).
- Ran (Rule 9, step 2): `git branch -D task/01a113e4…` → `error: branch '…' not
  found.` **exit=1** — the precondition was already satisfied by the merge, so the
  step reports done-by-absence rather than deleting anything.
- Ran (Rule 9, step 3 — the re-check): `git branch` → `+master`,
  `*task/01a115c3fc807807aaff5c56272638ac`, `+task/01a11683342375389a4a41dc8372df7f`
  — **no residue**; `git worktree list` → 6 entries, none on the orphan ref
  (`grep -c 01a115bb` → `0`); `git tag -l 'archive/*01a113e4*'` → the one tag.
- Ran (the trap, re-measured through the new tag):
  `git show archive/…-20261007:devdoc/voice-input-default-local.md | sha256sum` →
  `4e301c0947a677ad…`, 44943 bytes ⇒ master's design document, **not** the rig
  record; `git rev-parse master:devdoc/voice-input-local-capture-rig.md` → fatal
  (the orphan's own path is absent on master; the rescue used the
  `voice-input-capture-rig.md` spelling).
- Ran: `gopi task update 01a11676-c490… --status cancelled --reason "moot — …"` →
  `Task updated … status=cancelled`; the row now reads `cancelled` (the successor's
  subject is measured done in §14).
- Ran: `git status --porcelain` → **empty** (Rule 30).

---

## 15. Acceptance, item by item, against the epic's own criteria

One pass, `2026-10-07T21:44:07+08:00`, run tip `e72d3b81a`, master `21e9563a2`.
Transcript (30 lines): `artifacts/orphan-01a113e4-acceptance-final.txt`.

| # | the epic asks for | measured | ✅/❌ |
|---|---|---|---|
| 1 | every ruling backed by a real command, none quoted from a prior run | §2/§6/§8.1/§9.1/§11.2/§12.1/§13.H each carry their own transcript; §13's and §13.H's are in `artifacts/` (44 + 22 lines) | ✅ |
| 2 | a reason per commit; total = 7 | §2's table = 7 content commits + the layout merge as row 8, **both numbers stated**; §2/§13.H now declare the table **a snapshot at `a7c192187`** and account for the 12 later commits (19 at the tip, each ruled where it arrived) | ✅ |
| 3 | if rescued: new branch, build+test green | rescue landed on master (`127f5130c`); build exit 0; `make test-budget PKGS='./internal/docindex ./internal/standards'` exit 0 (`2 finished, 0 with a failure`) — this session's four runs of that scope: 44/100/44/47 s, all exit 0 | ✅ |
| 4 | after archival the ref is gone and the tag points at the original tip | `rev-parse --verify --quiet refs/heads/task/01a113e4…` → **exit=1**; `git rev-parse archive/01a113e467767eb9a4c33cb1df1cdfbb-20261007` → `21e9563a225c3c23912f782167da8f3e00f26768` = the archived tip; content through the tag → sha256 `9c4671fb4feef0ce…` | ✅ |
| 5 | `git branch` / `git worktree list` show no residue | `git branch` → `master`, this run's branch, the new row's branch — no orphan; `git worktree list` → 6 entries, none on the orphan ref | ✅ |
| 6 | this run's worktree `git status --porcelain` empty | 0 lines | ✅ |
| 7 | the report is committed to this run's branch | each round committed on `task/01a115c3fc807807aaff5c56272638ac`; the record is `devdoc/voice-input-rig-orphan-disposition-01a113e4.md`, 2544 lines, and the run report is at `GOPI_TASK_REPORT_PATH` | ✅ |
| 8 | re-test whether master covers it now | **two levels, stated apart**: REF level `git merge-base --is-ancestor 21e9563a2 master` → **exit=0 (covered)** — flipped from `exit=1`, which held through five sweeps; CONTENT level `master:devdoc/voice-input-capture-rig.md` = `127f5130c`, `--objects` count `1` (covered since the rescue landed) | ✅ |

**The honest note on criterion 8**: the REF-level flip was not produced by this
run — the holder's own merge did it, in a window this record then measured. The
contrast is the finding: the branch was **uncovered at the REF level and covered
at the CONTENT level** for most of this run's life, which is exactly why a single
reading of either level would have misreported the branch.

---

## Hand-over (Rule 6d)

- **Branch**: `task/01a115c3fc807807aaff5c56272638ac` — the ledger branch, whose
  tip carries every commit of this record (the run's own address). The rescue
  branch `feat/rescue-01a113e4-capture-rig` was its **ancestor**, and it has also
  been **merged into master by the merger** (`39711d146`, §9.1) — after which the
  branch itself was **pruned** (§10.4 measured: `git branch --list
  feat/rescue-01a113e4-capture-rig` prints nothing, and `d0032b8e8` — the rescue
  commit — is reachable from this branch and from master). So the recovery
  commands below name **the commit and master**, never the pruned branch name:
  a hand-over pointing at a deleted ref is how a recovery line becomes wrong.

  ```bash
  git merge-base --is-ancestor d0032b8e8 HEAD && echo "rescue is an ancestor"
  git rev-parse HEAD                                    # the deliverable tip
  git rev-parse master:devdoc/voice-input-capture-rig.md   # 127f5130c — on master
  ```
- **Recovery**: `git show HEAD:devdoc/voice-input-rig-orphan-disposition-01a113e4.md`
  (this record) and `git show HEAD:devdoc/voice-input-capture-rig.md` (the
  rescued rig document, blob `127f5130c`, sha256 `9c4671fb…`); the same two
  documents by sha, independent of any ref:
  `git show d0032b8e8:devdoc/voice-input-capture-rig.md`.
  **Not** `git show <orphan>:devdoc/voice-input-default-local.md` — §2 measured
  that the branch's old path holds **master's** design document (blob
  `4bfa96995`), not the rig record.
- **In the main repo**: yes — both commits live in the shared object store of
  `/home/user/work/GoPi`; a worktree commit *is* the deliverable, no push needed
  (Rule 6c). Nothing was written outside this run's worktree.
- **~~What is still open (and who owns it)~~ — CLOSED, measured in §14.** Kept as
  the record of what the gate looked like while it held: `refs/heads/task/01a113e467767eb9a4c33cb1df1cdfbb`
  existed and no `archive/` tag did, both blocked by ONE cause re-measured five
  times — a live ground (`branch -D` exit 1, `safe-merge.sh` exit 1
  `worktree-occupied`, worktree
  `/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01` linked to the ref,
  row `01a115bb-fe1e-7251-8ba4-0698a3ba5a01` `in_progress`) — and the step was
  filed as row `01a11676-c490-7ab3-a664-91cfa9b6408a` with `parent = the holder
  row`, so the parent gate would release it exactly when the ground was released.
  **That is what happened, by the holder's own hand**: the row is now `completed`,
  master carries the branch's last tip (`21e9563a2`), the ref is ABSENT, and this
  run created the archive tag at that tip (§14.2). Row `01a11676-c490` is cancelled
  as moot, with the reason naming §14 — its entire subject is measured done, and
  leaving it queued would re-derive a finished state on a ref that no longer
  exists (Rule 26).
  Its briefing had been revised in this run (`gopi task update … --payload @… --reason …`,
  3756 → 5595 → 5992 bytes) to remove one recovery spelling that had gone stale —
  it told the reader to read the disposition document off
  `feat/rescue-01a113e4-capture-rig`, a branch the merger had already pruned
  (`git rev-parse --verify --quiet refs/heads/feat/…` → exit=1) — and to carry
  §13's readings, the two measured traps, and the supported verb
  (`gopi task archive`, §7). A hand-over that names a deleted ref is how a recovery
  line becomes wrong.
- **This record itself**: at §13.G's reading,
  `master:devdoc/voice-input-rig-orphan-disposition-01a113e4.md` resolves
  (`351a35f19`) — an earlier revision of this document is already on master — and
  §14 leaves the same status: this branch's remaining contribution is this
  document's newer revision (the §13/§14 sections), which is handed over by branch,
  not by a merge this run performs (Rule 6e).
- **Recovery, final spelling (the ref is gone — name commits, tags and master only)**:
  ```bash
  git show master:devdoc/voice-input-capture-rig.md                 # blob 127f5130c, sha256 9c4671fb…
  git show archive/01a113e467767eb9a4c33cb1df1cdfbb-20261007:devdoc/voice-input-capture-rig.md
  git show master:devdoc/voice-input-rig-orphan-disposition-01a113e4.md   # this record (earlier revision)
  git show HEAD:devdoc/voice-input-rig-orphan-disposition-01a113e4.md     # this record, newest revision
  git show master:devdoc/merge-resolution-voice-input-rig-conflict.md     # the holder's own log, landed by its own merge
  ```


## Evidence (self-filed)

- `git cherry master task/01a113e467767eb9a4c33cb1df1cdfbb` → 7 lines, all `+`,
  quoted verbatim in §1.1.
- `git rev-list --count task/01a113e4… ^master` → `8`; the same count taken
  minutes later during the reachability loop → 10 entries (the branch grew under
  this run; §1.1).
- `git rev-list master --objects | grep -q 127f5130c` → `ABSENT-on-master`
  (§3.1) — the reading that justifies the rescue.
- `git grep -l -F 'PEERBOX-REMEMBERED' master -- | wc -l` → `0`; likewise
  `voiceinput-rig-8s`, `sink_name=gopi_rig`, `source_name=gopi_rig` → `0` each
  (§3.1).
- `git diff --name-status master...task/01a113e4…` → `M devdoc/DOC-INDEX.tsv`,
  `A devdoc/voice-input-local-capture-rig.md` (§1.2).
- `sha256sum devdoc/voice-input-capture-rig.md` →
  `9c4671fb4feef0ce46e921f86d61078b73ca97b486603d8f82db62d7bda6fce5`;
  `git rev-parse HEAD:…` = `git rev-parse task/01a113e4…:devdoc/voice-input-local-capture-rig.md`
  = `127f5130cb18a49fddad09e5d234fdbb4f401417` (§3).
- `gopi tool doccheck` → exit 1 with
  `file-with-no-row devdoc/voice-input-capture-rig.md`; `--fix` → exit 0,
  `repaired 1 path(s)` (§3).
- `git tag archive/01a113e4…-20261007` → exit 0; `git rev-parse` of the tag →
  `a7c1921871a407d329f692908c0a9322d6ccf324`; `git branch -D` → exit 1 with
  `Cannot delete branch … checked out at '/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01'`;
  post-`rev-parse --verify --quiet` → exit 0 (still present).
- Tag withdrawal observed, not asserted:
  `git tag -d archive/01a113e467767eb9a4c33cb1df1cdfbb-20261007` →
  `Deleted tag 'archive/01a113e467767eb9a4c33cb1df1cdfbb-20261007' (was a7c192187)`,
  exit 0; `git tag -l 'archive/01a113e4*'` → empty (§4.2).
- `bash scripts/safe-merge.sh task/01a113e4…` → exit 1,
  `拒绝种类: worktree-occupied` (§4.2).
- `gopi task list --all | grep '^#01a115bb'` → `[in_progress]`, with
  `[ground task/01a113e4… @ /home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01]`
  (§1.1).
- `git worktree list` → the holder worktree at `a7c192187` on the orphan branch,
  and this run's own ground on `task/01a115c3fc807807aaff5c56272638ac` (§1.1).
- `/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01` `git status --porcelain`
  → empty (the live holder's own tree was clean when read; no foreign tree was
  written to by this run).
- Close-out re-measurement (§6), taken at master `7e084ba22` / branch `2fb334242`:
  `git rev-list --count master..<branch>` → `11`;
  `git diff --name-status master...<branch>` → `M devdoc/DOC-INDEX.tsv`,
  `A devdoc/merge-resolution-voice-input-rig-conflict.md`,
  `A devdoc/voice-input-local-capture-rig.md`;
  `git cherry master <branch>` → 9 lines, all `+`;
  `git rev-list master --objects | grep -q 127f5130c` → `ABSENT-on-master`;
  `git merge-base --is-ancestor <branch> master` → exit 1.
- Per-commit re-extraction this round (7 content commits): `--numstat` = `311/0`,
  `1/0`, `23/10`, `1/0`, `1/1`, `93/2`, `39/3`, and the old-path blob per commit =
  `5b4242e2a`, `5b4242e2a`, `5de23aa8d`, `0ba501454`, `74a6ff03c`, `010e4c7b8`,
  `127f5130c` — the last one being the rescued revision, matching the table in §2
  exactly; the new path does not exist in any of the seven (`git rev-parse` → fatal).
- `gopi tool doccheck` → exit 0, `the devdoc corpus is consistent` (run twice:
  before and after the §6 edits).
- The retirement that makes §4.3 necessary, quoted from the tree:
  `internal/tools/repo_sweep.go:1361` — "The integration-side tidy
  (MergedCleanupSweep: worktree remove + branch -d + archive tag) is RETIRED from
  this tick — the sweep mutates no ref"; `internal/tools/task_finish_cleanup.go:406-407`
  — `finishCleanupBranchDeleteFlag = "-d"` / `…ForceDeleteFlag = "-D"`.
