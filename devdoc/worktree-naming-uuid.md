<!-- doc-class: normative -->
# Worktree / branch naming is derived from the row's FULL uuid — the three families on this host, and why the orphan branch was never a prefix

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a11548-7cba-7170-91f5-430964f8e087` · host: **peerbox** · branch
`task/01a115487cba717091f5430964f8e087` · worktree
`/home/user/workspace/gopi-01a115487cba717091f5430964f8e087`

Epic: *收口 gopi 的 worktree/分支命名* — every ground name derives from the
task's **full uuid**; a non-uuid shape must be **refused loudly** (Rule 15);
the 7-commit orphan branch `task/01a113e467767eb9a4c33cb1df1cdfbb` had to be
traced to its origin and its archiving made conditional on **master covering
its content**.

**Result in one line:** the naming rule is implemented and pinned by tests; the
orphan branch's provenance is measured (it is a *correct* full-uuid name, not a
prefix-built one); and **master does NOT cover it**, so it is **not** archived —
the archiving precondition fails, and Rule 15 forbids destroying it silently.
§5 states what would satisfy the precondition.

---

## 1. The three name families, each traced (measured 2026-10-07, this host)

| family | example | who makes it | a row behind it? | reclaim path |
|---|---|---|---|---|
| **uuid-derived** | `gopi-01a115487cba717091f5430964f8e087` / `task/01a115487cba717091f5430964f8e087` | `internal/tools/task_run_ground.go` — `taskGroundIDStem` (the ONLY minter) | yes, exactly one | finish-cleanup, keyed on the row |
| **human-control** | `gopi-crelease`, `gopi-masterchk`, `gopi-mastercheck-docanchor`, and — measured later in this run — `gopi-fixproof`, `gopi-voicerung2` (§1.2.1) | a person, by hand | **no** | none — by design |
| **the orphan** | `task/01a113e467767eb9a4c33cb1df1cdfbb` | the claim path of row `01a113e4-6776` | yes, a **cancelled** row | none ran (the row was cancelled) |

### 1.1 uuid-derived — one minter, two consumers

`taskGroundIDStem(taskID)` strips every non-alphanumeric character from the
normalised id (`task_run_ground.go:481`). For a canonical uuid that is exactly
the 32-character dash-free id. Two names are built from it and from nothing
else:

```go
// task_run_ground.go
func taskGroundDirFor(repo, taskID string) string      // <worktree_root>/gopi-<stem>
func taskGroundBranchFor(taskID, recorded string) string // task/<stem>, unless the row recorded its own
```

Grep (non-test, whole tree, re-measured at `30458e4a6` —
`…/artifacts/prefix-census.txt`) — the prefixes have exactly one definition each
and their only *name-making* consumers are those two functions:

```
TaskGroundDirPrefix    = "gopi-"   (task_run_ground.go:60)
TaskGroundBranchPrefix = "task/"   (task_run_ground.go:66)
```

`git grep -n '"task/"' -- '*.go' ':!*_test.go'` returns exactly one
non-comment line — the definition above. `taskGroundIDStem` has exactly three
call sites in production code (`:522`, `:526`, `:540`) and all three are the
name builders of this file: there is no second stem minter anywhere in the tree.

Other references are readers or legitimate second definitions: `TaskGroundRole`
(same file), the chat sweep's own constants (`worktree_sweep_chat.go:68` — an
**assignment** from the constant, never a literal), `archive_ref_reclaim.go:135`
— a `strings.HasPrefix(branch, TaskGroundBranchPrefix)` test that PROTECTS live
grounds from the archive arm — and `internal/scratch/worktree_stub.go:90`,
a `gopi-` literal in a leaf package that may not import `internal/tools`, tied
back by `internal/tools/worktree_stub_prefix_test.go:17` (Rule 14's rule for a
value that cannot be hoisted). No consumer re-types either literal, and the
re-measured census is in `…/artifacts/prefix-census.txt`.

### 1.2 human-control — legal, unowned, and un-reclaimable

`/home/user/workspace/gopi-crelease` — worktree on branch **`task/crelease`**,
tip `380679ad2 delivery-point sync: merge master into the delivered branch
before close-out`; **zero** ledger rows name it (`gopi task list --all | grep -c
crelease` → `0`). `/dev/shm/gopi-masterchk` — detached HEAD; its tip read
`bdc64dcb1` at the start of this run and `93848040e` at close-out, i.e. **it
moved while this task was running** without any gopi machinery touching it.
That is the cleanest available evidence that these trees belong to a person who
is using them: nothing in gopi writes to a detached-HEAD control tree, so the
only writer left is the operator.
`/dev/shm/gopi-mastercheck-docanchor` appeared in the worktree listing at
t+953 and was gone by t+1393 without a prune entry (`git worktree prune
--dry-run` → empty) — someone removed it by hand, which is itself the evidence
that these are a person's own scratch trees and not gopi's.

Two consequences, and both are *correct* behaviour rather than bugs:

- **`task/crelease` is a name the rule would now refuse to MINT**, but it was
  never minted: `taskGroundBranchFor` mints `task/<stem>` only for a row that
  recorded no branch, and no row exists here. The name reaches gopi from
  outside (a human's `git worktree add`), so nothing refuses it — the rule
  governs what *gopi* creates, not what a person names their own tree.
- Nothing in gopi may claim, free, or attribute them. `archive_ref_reclaim`
  keeps `task/*` out of its reach entirely (`archiveReclaimProtected`), so
  `task/crelease` is protected on the prefix alone; `gopi-crelease` carries no
  row stamp, so finish-cleanup cannot select it.

This is exactly why §4 classifies by **shape** instead of by a ledger lookup: a
control worktree has no row *by construction*, so a lookup could only ever
return a false negative.

#### 1.2.1 `gopi-fixproof` / `gopi-voicerung2` — a FOURTH family, and the one that
#### breaks the two-name split above

The worktree-root census (`gopi tool scratch-gc stubs --dry-run`, run for real
in this worktree; transcript
`…/artifacts/stubs-dry-run.txt`) reports **6** candidates directly under
`/home/user/workspace`, and two of them are neither a uuid-derived ground nor a
worktree at all:

```
keep /home/user/workspace/gopi-fixproof     reason="the .git pointer is unreadable, is not a `gitdir: <path>` line, or names a relative path"
keep /home/user/workspace/gopi-voicerung2   reason="the .git pointer is unreadable, is not a `gitdir: <path>` line, or names a relative path"
```

A uuid-derived ground's `.git` is a **file** holding one `gitdir: /abs/path`
line; these two hold something else under that name (an unreadable value, a
directory, a relative path — the verdict names all three because the predicate
keeps them for the same reason). So the trio in the table above is not the whole
vocabulary: there is a **person's own repository or working tree that happens to
carry the `gopi-` prefix**, and the earlier two-name split (`derived` vs
`human-control`) collapses it into `human-control` because both are
`gopi-<not-32-hex>`.

That collapse is a **reported limitation, not a hidden one**, and it is left
standing deliberately:

- The *reclaim* side is already safe, and by a different and stronger condition
  than the name: the stub arm's predicate requires a `.git` **pointer file**
  naming an **absolute** path that **does not exist**, and a live registration
  is kept on the pointer's target existing *alone* (`WorktreeStubKeepTargetLive`
  — cheap, positive, and independent of the name family). Measured above: the
  two unreadable-pointer directories are kept, and so are the three live
  registrations.
- The *naming* predicate this task added (`TaskGroundRole`, and the
  `fullid.IsUUID` gate at the derivation) is about what gopi MINTS. A
  `gopi-<word>` directory that gopi did not mint is out of its jurisdiction, and
  a classification that guessed "this one is a repo, that one is a control
  worktree" from the name alone would be inventing a distinction the name does
  not carry — the same over-reach §3's first attempt measured (a 60 s E2E
  timeout) in a different place.
- The honest count: **three real families (one of them with a naming
  vocabulary this task does not claim to distinguish), not two.** A future
  consumer that wants the distinction must measure the tree (`.git` kind, the
  pointer's target, whether a registration is live), never the name.

#### 1.2.2 The date-shaped stem — the family predicate is the one place a prefix
#### can still be mistaken, and it is OUT of this task's jurisdiction

`scratch.WorktreeStubNameMatches` accepts `^gopi-[0-9a-zA-Z]{8,64}$` (measured
directly in this worktree by a temporary probe that the same command removed —
transcript `…/artifacts/stub-family-probe.txt`, so this is a reading and not a
reading of the regex):

```
WorktreeStubNameMatches("gopi-20261007")                         = true   ← a DATE
WorktreeStubNameMatches("gopi-01a113e4")                         = true   ← an 8-hex prefix
WorktreeStubNameMatches("gopi-01a113e467767eb9a4c33cb1df1cdfbb") = true   ← a real uuid stem
WorktreeStubNameMatches("gopi-crelease")                         = true
WorktreeStubNameMatches("gopi-masterchk")                        = true
WorktreeStubNameMatches("gopi-fixproof")                         = true
WorktreeStubNameMatches("gopi-voicerung2")                       = true
WorktreeStubNameMatches("gopi-12345678")                         = true
```

So **`gopi-20261007` — an 8-character stem, the exact "prefix-shaped" class this
task refuses to MINT — IS in the stub sweep's family.** §4's census therefore
has one more row, and the honest verdict for it is:

| site | what it matches | verdict |
|---|---|---|
| `internal/scratch/worktree_stub.go:107` (`worktreeStubNameRe`) | `gopi-` + 8..64 alphanumerics | **a family selector, and it cannot be narrowed to the uuid length** — see below |

Narrowing it to `{32}` would be wrong, and the measurement says why: the SAME
predicate is the only reclaim path for the 174 leaked stubs of
`devdoc/…`'s incident (all 32-char, but the arm's own doc says the family is
"`gopi-` + a GENERATED stem"), and a *date* is the one shape a human picks that
a machine could not tell from a uuid stem by class. But the danger the epic is
about — a name being **attributed to a row** — does not exist here: the stub arm
never asks which row a directory belongs to. It asks whether the directory is a
**pointer to nothing** (eight positive conditions, each of which a real tree
fails), so a date-named directory that is somebody's real repository is kept by
the `.git`-is-a-directory condition, not by the name.

**Conclusion for this row:** safe *as it stands*, and it stays outside the rule
this task implements — the rule governs the NAME gopi MINTS (one minter, §1.1),
while this predicate governs a DIRECTORY gopi may DELETE (one predicate, with
its own eight conditions). Changing it would be a change to the reclaim arm's
family definition, which is a different rule with a different blast radius; it
is named here so the next reader does not have to rediscover it.

### 1.3 The orphan branch — and why it LOOKED like a prefix bug

7 commits, tip `dba11bb24`, merge-base with master `3255973cc`
(2026-10-07 11:09), i.e. cut from the delivery-point-sync of that morning:

```
dba11bb24 docs(voice): attribute the listener figure by pid and re-measure the session-copy artifacts at close-out
e2331156f docs(voice): record the rig's re-verification on peerbox and the determinism boundary
9fedce85e docs(voice): fix the voiceResolveSelection citation range
7374cd07a docs(voice): carry the capture artifact's sha256 in the measurement table
8cc4e7e86 docs(voice): correct the race/probe mechanism in the rig doc after re-reading the code
5848c46e4 docs(voice): classify devdoc/voice-input-default-local.md in DOC-INDEX.tsv (historical)
09ad05915 docs(voice): the deterministic local-capture rig for voice_input (peerbox, null-sink + remap-source)
3255973cc delivery-point sync: merge master into the delivered branch before close-out
```

**Its origin, fully traced:** the stem is the **dash-free full uuid of row
`01a113e4-6776-7eb9-a4c3-3cb1df1cdfbb`**, item **[2/5]** of the voice epic:

```
#01a113e4-6776-7eb9-a4c3-3cb1df1cdfbb  [cancelled]  [2/5] 在 peerbox 上搭起确定性麦克风装置…
#01a113e4-6730-7b5b-bc35-0278c2c20b8b  [cancelled]  [1/5] 读取现有 voice_input 实现…产出改造设计
```

The two rows share their first **8 hex characters** (`01a113e4`) because the
first field is a time-based prefix — 6776's own `parent_task_id` is 6730, the
epic's design item. So the branch name, read as "8 hex prefix + something", looks
like a name built from the **parent's** prefix; read as what it is — the child's
own 32-hex id — it is the name the current rule would mint, byte for byte.

**Why the branch outlived its cancelled row:** row 6776's run wrote
`devdoc/voice-input-default-local.md`, and the *parent's* branch (row 6730) had
already landed a **different document at the same path** on master (`2dca42b86`,
11:33 — the branch's own file was created at 11:48). The merger therefore hit a
same-path/different-content conflict and filed rework rows instead of merging:

```
#01a114aa-eb19-7ce3-8333-d29df8e65e55  [cancelled]  resolve merge conflict: rework: task/01a113e467767eb9a4c33cb1df1cdfbb
#01a11564-3c65-7b10-b9fa-d0f04a9d509a  [cancelled]  resolve merge conflict: rework: task/01a113e467767eb9a4c33cb1df1cdfbb
#01a1156f-0b6a-723d-be9f-75d980a993ea  [pending  ]  resolve merge conflict: rework: task/01a113e467767eb9a4c33cb1df1cdfbb
```

**Re-read late in the run, and the queue had moved**: `01a1156f-0b6a` is gone
from the pending list, and a **new** row of the same subject now stands there —
`gopi task list --status pending | grep -i 'merge conflict'` →

```
#01a115bb-fe1e-7251-8ba4-0698a3ba5a01  [pending]  resolve merge conflict: rework: task/01a113e467767eb9a4c33cb1df1cdfbb  (merger, created 2026-10-07 17:40:04) [next]
```

So the merger is still filing conflict rows for this branch — the rework row is
now the row a dispatcher would take `[next]` — while 173 rows in the ledger
carry the same subject string (the epic's voice_input series produced a long
tail of them; the number is a census of the subject, not of this branch). Read
the live list before acting on any id in this section; the ids move, the
collision does not.

**That is the whole of the "prefix conflict" the epic reported**: a **doc-path
collision between a row and its own parent**, plus a name whose first 8
characters are identical to a sibling's. Neither is a naming defect — and the
pending row `01a1156f-0b6a` is the live consequence still sitting in the queue.

---

## 2. Why the ground name is asserted by LENGTH, not by "looks like a uuid"

An 8-character prefix and a 32-character stem are both lowercase hex; character
class cannot separate them, and neither can a human eye reading
`task/01a113e4…` (which is what happened here). So the assertion is exact:

```go
const taskGroundUUIDStemLen = fullid.UUIDLen - 4   // 36 - 4 dashes = 32

func taskGroundStemIsUUIDDerived(stem string) bool {
    return len(stem) == taskGroundUUIDStemLen && hexStemOnly(stem)
}
```

`fullid.UUIDLen` is the existing named constant (`internal/fullid/fullid.go:29`)
and `fullid.IsUUID` (`:48`) is the existing full-shape predicate — the rule
references them rather than restating the shape.

---

## 3. The implementation (uuid-only naming)

`internal/tools/task_run_ground.go`, +118 lines:

1. **`taskGroundUUIDStemLen`** + **`taskGroundStemIsUUIDDerived`** /
   **`hexStemOnly`** — the shape, in one place (Rule 1b/13/14).
2. **`TaskGroundRole(name) string`** → `uuid-derived` | `human-control` |
   `foreign`, by shape alone (exported consts, so a consumer needs no literal).
3. **`taskGroundNameRefusal(taskID, repo)`** — the Rule-15 refusal: it names the
   id, the cause (`not a full uuid`), the consequence (a `prefix/slug shape`
   that other rows and hand-made control worktrees also carry), and the door
   (`TaskRepoRepairVerb`).
4. **The gate, placed at the DERIVATION and nowhere earlier:**

```go
if !fullid.IsUUID(taskID) && liveDir(strings.TrimSpace(worktree)) == "" {
    return TaskGroundResolution{}, taskGroundNameRefusal(taskID, repo)
}
```

The position is part of the design. `TaskRunGroundFor` has three ways to answer,
and only two of them **mint a name**: an already-recorded ground (path #1) is
reused verbatim, while the deterministic derived ground (path #2) and the create
path (#3) both build `<root>/gopi-<stem>` and `task/<stem>` from the row's id.
A row whose id is not a full uuid is a problem exactly where a name would be
DERIVED from it — and nowhere else. So the gate sits there:

- **A row that records its own worktree is exempt** — it never mints a name, so
  its id is not this rule's business (and it is not: a legacy row id is a fact
  about the ledger's history, not something this code names).
- **A row that names no repo is exempt** — the function returns earlier, because
  such a row has no ground and never gets one.
- **Everything that reaches the derivation must carry a full uuid** — the row
  shapes this refuses are precisely the measured ones: a hand-written slug used
  as an id, and any prefix-cut id.

This boundary was **not guessed — the first full-package run measured it**. An
unconditional gate failed seven existing tests: five fixtures passed readable
slugs (`t-ground-stamp`, `01a0anchor-t1`, …) as ids, one E2E failed *because the
gate refused its id and the run never started* (`t-dh-live`), and one durable-repo
fixture was already being changed by this task. The E2E is the decisive reading:
it records `deliverable.worktree` itself, so its id never names anything — the
unconditional form refused where no name is derived, which is the over-reach, not
the rule. The fixtures that DO derive were given full uuids (§6).

---

## 4. Prefix-matching census — every `HasPrefix` on a task-id-shaped value

| site | what it matches | verdict |
|---|---|---|
| `internal/tools/task_resumed_session.go:183` | `!HasPrefix(name, short) \|\| !fullid.IsUUID(name)` | **already uuid-only** — a short prefix narrows, but only a full uuid can be selected (the false-positive is filtered out by the second half) |
| `internal/tools/tasks.go:1952` | `HasPrefix(b, taskAliveProcessNamePrefix)` | process-name prefix (`gopi-…` argv), not a task id — safe |
| `internal/gopilog/reader.go:706` | `HasPrefix(r.id, id)` | session-id resolution, `fullid.Refusal` reports candidates — not ground attribution — safe |
| `internal/tools/tasks.go:4749` | directory containment under a parent path | not an id — safe |
| all others (`taskReasonStatsPrefix`, `taskTagResumedPrefix`, `TaskListRowPrefix`, `bugFamilyPrefix`, …) | render markers, not ids | safe |

**Conclusion:** exactly one site attributes an artifact by *task-id prefix* —
`taskResumedShortCandidates` — and it was already hardened to require a full
uuid. The ground-naming path was the one place where a prefix could still have
*become* a name (via `taskGroundIDStem`), and §3 closes it. No further prefix
attribution point exists.

The two rows in §1.3 are the fixture that proves the closure: they share
`01a113e4` and they resolve to two different grounds, two different branches,
each stem equal to its own row's id.

---

## 5. The orphan branch: coverage measurement and the disposition

### 5.1 The measurement

```bash
git merge-base --is-ancestor task/01a113e467767eb9a4c33cb1df1cdfbb master
# → exit 1: NOT an ancestor (never merged)
git cherry master task/01a113e467767eb9a4c33cb1df1cdfbb
# → 7 lines, all '+', 0 lines '-'  ⇒ no commit has a patch-equivalent on master
git rev-list --count task/01a113e4…  ^master
# → 7
```

`git diff --name-status master...ORPHAN` → `M devdoc/DOC-INDEX.tsv`,
`A devdoc/voice-input-default-local.md` (three-dot `--shortstat`: *2 files
changed, 453 insertions* — the branch's own increment, and **doc-only**) — while
the **two-dot** `master..ORPHAN` shows *229 files, 1991 insertions, 19239
deletions*: that is the day of merges master gained **after** this branch's
merge-base, read backwards. (The two-dot figure is a moving number — it was
*103 files / 1629 / 14608* when the first measurement was taken hours earlier in
this same run, because master kept merging; it measures master's traffic, not
this branch, which is exactly why it must not be quoted as "the branch's
change".) Neither figure answers "does master cover it"; only §5.2 does — and
**doc-only is not the same answer**: `--name-status` says the change is narrow,
while §5.2 shows the same path carries a *different document* on master.

### 5.2 The trap: same path, different document — and the content check

`git cat-file -e master:devdoc/voice-input-default-local.md` → **present**. But
it is not this branch's file:

| | master's file | the orphan branch's file |
|---|---|---|
| added by | `2dca42b86` (11:33), row **6730** (item 1/5, the design) | `09ad05915` (11:48), row **6776** (item 2/5, the rig) |
| lines / bytes | 556 / 44943 | 452 / 25342 |
| **blob sha** | `4bfa9699597c42875e0f433a81519147638e8484` | `127f5130cb18a49fddad09e5d234fdbb4f401417` |
| class | normative design doc | `<!-- doc-class: historical -->` measurement record |

Two different blobs ⇒ the file at that path on master is **not** this branch's
content, however similar the subject and the path are. (Blob shas are the
strongest form of this reading: it is content-addressed, so it cannot be fooled
by a moved line or a re-worded heading.)

**Whole-master content check for the branch's unique markers** — the rig's
numbers and the artifact's digest, none of which a same-subject document would
happen to reproduce (`git grep -l -F <marker> master | wc -l`):

```
15443                               0      (peak sample)
0.8296                              0      (non-zero-sample ratio)
2374.2                              0      (RMS)
3ba06d3bbdb356a7d5ac1f0056e89937    0      (the capture artifact's sha256)
Front_Center.wav looped             0      (the fixture)
remap-source                        4      (the RIG, from other runs — see below)
```

**The counter-check, so "not covered" is not overstated:** the *recipe* IS
reproducible from master — `devdoc/voice-input-bare-acceptance-20261007.md` and
`devdoc/voice-input-default-shape-reverification.md` both describe a
null-sink + remap-source rig, and four master files name `remap-source`. What is
unique to this branch is **one run's measured record** (that run's artifact
digest, its 8.000 s capture, its peak/RMS, its transcript), not the ability to
reproduce the technique.

### 5.3 The disposition (decided by the measurement, not by preference)

**The archiving precondition FAILS** — master does not carry the branch's
unique content — so this run **does not** create an `archive/…` tag and **does
not** delete the branch. Rule 15: a decision that destroys work must be
reported, never taken on a technicality (and `git branch -D` + tag would be
exactly that, since the tag is only a ref the next sweep may see).

What the branch costs while it stands: it stays a merge candidate with a
same-path/different-content conflict, which is precisely the pending row
`01a1156f-0b6a-723d-be9f-75d980a993ea` waiting in the queue. (Latest ledger
reading, taken when the handoff row was re-read: that row and its sibling
rework rows stand as recorded in §1.3; re-read them with
`gopi task list | grep -i 'resolve merge conflict'` before acting — the ledger
moves.)

**What would make it archivable** (any ONE of these, then the ordinary archive
path applies — `git tag archive/<branch>-<date>` + `git branch -D` + worktree
removal, Rule 9):

1. **Land the unique record on master.** The document is *historical evidence*
   with no product code (`Product code: none changed.` in its own header), so it
   can be committed on master under a non-colliding path — e.g.
   `devdoc/voice-input-local-rig-01a113e4-6776.md` — which also resolves the
   pending conflict row. Its bytes are the diff this run measured, and
   `git show task/01a113e467767eb9a4c33cb1df1cdfbb:devdoc/voice-input-default-local.md`
   preserves them exactly.
2. **Re-run the rig** and land a fresh record (the recipe is on master; the
   measurement is what would be new).
3. **Decide the record is disposable** — an explicit operator decision, which
   is the only thing that may stand in for coverage. Then archive, citing this
   document as the reason.

Recommended: (1), as a separate task, because it closes the pending row at the
same time. This run reports and does not choose.

### 5.4 The epic's own acceptance criterion, read against the measurement

The epic's deliverable 4 asked for the coverage proof in two halves — "`git diff
--name-status master...<branch>` 仅剩 doc 类文件 **+** 文件内容对比". Measured,
**the two halves disagree**, and it is worth saying so plainly because the
disagreement is the whole finding:

| the epic's half | measured | what it supports |
|---|---|---|
| `--name-status` leaves only doc-class files | **true** — `M devdoc/DOC-INDEX.tsv` + `A devdoc/voice-input-default-local.md` (`--shortstat`: 2 files, +453) | the change is *narrow*: no product code |
| "compare the file contents" | **fails** — same path, blob `127f5130…` (25342 B) on the branch vs `4bfa9699…` (44943 B) on master; the branch's unique markers are 0-hit across master | the branch's document is **not** on master |

**The second half decides**, and not by preference: a diff *path list* answers
"which paths differ", while a blob sha answers "is this content present" — and
only the second is what "master covers it" means. A doc is content: this one is
one run's measurement record (artifact digest, peak, RMS, transcript), of which
master holds no copy. "Doc-only" would license `git branch -D` on a **reading of
the path list**, and the paths are exactly where the collision is (the parent
landed a *different* document at the same path).

So deliverable 4 is met in the form the measurement supports — a written
disposition record with commands and outputs, concluding **not archived** — and
the archive itself is handed to `01a115c3-fc80-7807-aaff-5c56272638ac`, whose
briefing carries both readings and this table's conclusion. Reporting the
disagreement is the deliverable; picking the convenient half would be the
defect the epic was opened to find.

---

## 6. The regression tests (red first, then green)

New file `internal/tools/task_ground_name_uuid_test.go` (four tests). It carries
two fixtures added after the census of §1.2.1 — `gopi-fixproof` and
`gopi-voicerung2` — so the classification that *collapses* those two families is
asserted rather than left to be discovered:

- **`TestGroundNameRefusesANonUUIDRowId`** — acceptance ①. Two ids in the
  measured class (`gopi-masterchk`, `gopi-crelease`): the resolution must
  **error**, the directory must **not exist** afterwards (a refusal that leaks a
  directory is not a refusal), and the sentence must name the id, the cause
  (`not a full uuid`), the consequence (`prefix/slug shape`) and the door
  (`TaskRepoRepairVerb`).
- **`TestGroundPrefixCollisionRowsGetDistinctGrounds`** — acceptance ②, on the
  **real** two rows of §1.3: different `Dir`, different `Branch`, stems of
  length 32, each stem equal to its own row's dash-free id, the shared 8-char
  prefix asserted (so the fixture cannot silently stop being the incident), and
  the orphan branch's stem asserted equal to the **child's** and different from
  the parent's.
- **`TestGroundRoleSeparatesDerivedFromHumanControl`** — the three families,
  and the assertion that makes the misattribution impossible by construction: a
  prefix-shaped stem (`gopi-01a113e4`) is **never** `uuid-derived`.
- **`TestGroundStemIsUUIDDerivedRequiresTheFullLength`** — length is the
  assertion; 8-, 12-, 31-character stems and a 32-char non-hex stem all fail.

Both red proofs were produced by mutation and then reverted (transcripts:
`~/.gopi/sessions/01a11548-7cba-7170-91f5-430964f8e087/artifacts/red-proof.txt`;
the green run of the same four tests, same worktree, at HEAD `19caf9de9`:
`…/artifacts/green-proof.txt`):

| mutation | observed red |
|---|---|
| **A** — delete the `fullid.IsUUID` gate from `TaskRunGroundFor` | `TestGroundNameRefusesANonUUIDRowId` fails: the resolution **succeeds** and creates `<root>/gopi-gopimasterchk` (and `gopi-crelease`) — the silent slug that the epic measured |
| **B** — truncate the stem in `taskGroundIDStem` to its first 8 characters | `TestGroundPrefixCollisionRowsGetDistinctGrounds` fails: both rows resolve to **one** ground `gopi-01a113e4` — the two-row conflation, produced on demand |
| **C** — relax the predicate to `len(stem) >= 8` (i.e. "an 8-hex prefix looks uuid-derived") | `TestGroundRoleSeparatesDerivedFromHumanControl` fails: `TaskGroundRole("gopi-01a113e4") = "uuid-derived"` and `…("gopi-01a11548") = "uuid-derived"`; `TestGroundStemIsUUIDDerivedRequiresTheFullLength` fails on the 8-, 10- and 31-character stems — i.e. the misattribution the epic reported, reproduced by mutation |

Each mutation was applied with a scripted single-string replacement, the test
was run, and the file was restored **before** the next mutation — so both
transcripts come from one unmodified tree, and `git status --porcelain` was
empty after each restore (the deliverable of this task is the committed
19caf9de9, never a mutant).

Also updated — the fixtures that DERIVE a ground (they passed readable slugs as
ids, which the rule now refuses):

| file | before | after |
|---|---|---|
| `internal/tools/task_run_ground_test.go` | `t-ground-create/-refuse/-stamp/-reuse/-assert` | full uuids `01a11500-0000-7000-8000-00000000000{1..5}` |
| `internal/tools/run_ground_anchor_test.go` | `01a0anchor-t1` | `01a11500-0000-7000-8000-000000000006` |
| `internal/tools/task_holder_resume_e2e_test.go` | `t-dh-e2e`, `t-dh-live` | `01a11500-0000-7000-8000-00000000000{7,8}` |
| `internal/tools/task_submit_durable_repo_test.go` | `t-submit-durable-runnable` | `01a11548-7cba-7170-91f5-430964f8e087` |
| `cmd/agent/cli_tasks_recycle_e2e_test.go` | `t-recycle-e2e`, `t-master-e2e`, `t-recycle-held` | `…001{1,2,3}` |
| `cmd/agent/cli_tasks_rancheck_seam_test.go` | `t-ran-seam-red`, `t-ran-seam-green` | `…001{4,5}` |
| `cmd/agent/cli_tasks_test.go` | `t-single-row` (×4), `t-e2e-brief` (×2), `t-e2e-pipe` (×4) | `…002{1,2,9}` |
| `cmd/agent/cli_tasks_waitpid_test.go` | `t-sigkill-e2e` (×5), `t-nonzero-e2e` (×5), `t-normal-e2e` (×4) | `…002{6,7,8}` |
| `cmd/agent/cli_tasks_report_seam_test.go` | `t-report-seam`, `t-report-nostory` | `…002{3,4}` |
| `cmd/agent/cli_tasks_submit_payload_test.go` | `t-cli-positional-e2e` (×2) | `…0025` |
| `cmd/agent/cli_completion_exceptions_test.go` | *(not a fixture: this one is a GAP — see below)* | `web_search` / `web_fetch` added to the predicates map |

**The last four rows are a correction, and the way they were found is the
point.** The first pass updated the fixtures the **targeted** runs flagged. A
full `make test-budget PKGS='./cmd/agent'` run then failed **10** more tests, all
of them for the same reason (`id "t-normal-e2e" would name the ground
"gopi-tnormale2e"`), and each of them reached the derivation through a real
`gopi worker <id>` invocation rather than through an argument. The lesson is the
one the epic is about, applied to the change itself: **the targeted run proves
the tests you named, and only the package's own entry point proves the package**
— these eight slugs were invisible until the package ran as a whole.
`TestTaskRunSurvivesClosedStdoutPipe` is the sharpest case: it failed with
`task never reached in_progress (status="failed")` after a **60 s** wait, which
reads like a flake and was in fact the same refusal (the child dies before its
first status write). Its fixture `t-e2e-pipe` is in the table.

**A second sweep was needed, and its shape is a lesson.** The first replacement
pass matched the slug as a **quoted string** (`"t-normal-e2e"`). Three fixtures
carry it as **substring text** instead, and the full-package run caught all
three:

| occurrence | why the quoted-string sweep missed it |
|---|---|
| `cmd/agent/cli_tasks_waitpid_test.go:160` — `needle := tools.TaskWrapperEnv + "=t-sigkill-e2e"` | the slug is a **suffix of a concatenation**, not a standalone literal |
| `cmd/agent/cli_tasks_submit_payload_test.go:131` — `strings.Contains(stdout, "claimed: t-cli-positional-e2e")` | the slug sits inside a **longer quoted string** (an assertion on rendered output) |
| `cmd/agent/cli_tasks_test.go:714`, `cli_tasks_report_seam_test.go:428`, `internal/tools/cov_finish_cleanup_test.go:478`, `cmd/agent/cli_tasks.go:2833` | **comments** — a slug in prose still rots: it names a fixture that no longer carries that id |

The rule that follows: **replace by token, not by quoted literal** — sweep for
the bare slug everywhere in the package (code, comments and assertion text
alike), then re-run the package's own entry point. `git grep -F <slug>` over
`cmd/agent/` + `internal/tools/` returning **zero** is the closure condition, and
it is checkable in one command:

```
$ git grep -F -l $(for s in t-single-row t-e2e-brief t-e2e-pipe t-sigkill-e2e t-nonzero-e2e t-normal-e2e \
    t-report-seam t-report-nostory t-cli-positional-e2e t-recycle-e2e t-master-e2e t-recycle-held \
    t-ran-seam-red t-ran-seam-green t-run-closeout-reap-e2e t-executor-repo-cwd-e2e t-executor-env-leak-e2e \
    t-session-cwd-e2e t-worker-console-fg t-worker-console-daemon t-wrapper-false-success-red \
    t-wrapper-honest-success-green t-ground-e2e-run tgroundannounce; do echo -e "$s"; done) \
    -- cmd/agent internal/tools
→ exit 1, no output — zero occurrences of every superseded row id
```
(The two hits that remain for a *name-shaped* string are deliberate: `gopi-masterchk`
/ `gopi-crelease` inside `task_run_ground.go` and its test are the **corpus** the
shape predicate classifies, not a fixture id.)

**A THIRD round, and it is the same lesson one turn further out.** The second
full `make test-budget PKGS='./cmd/agent'` run (after the substring sweep) failed
**ten more** tests, again all with `the task id is not a full uuid, so no ground
name can be derived from it`. These are not fixtures that pass an id to a
derivation — they are **e2e walks that submit a row and then spawn a real
`gopi worker <id>` for it** (`run_closeout_reap_e2e_test.go`,
`task_executor_repo_cwd_e2e_test.go`, `task_session_cwd_e2e_test.go`,
`worker_foreground_console_e2e_test.go`, `task_run_success_path_ledger_read_test.go`,
`task_ground_e2e_test.go`, `task_ground_announcement_e2e_test.go`). With a slug
id the run refuses to start, so the child never appears and the walk dies on its
own timeout — `no executor child of … carrying GOPI_TASK_WRAPPER_ID appeared
within 1m0s`, or a 60 s `task never reached in_progress` — which reads like a
flake and is the refusal. Each of the ten rows is now a **distinct** uuid, so no
two walks share a ground or a branch:

| file | row(s) before |
|---|---|
| `cmd/agent/run_closeout_reap_e2e_test.go` | `t-run-closeout-reap-e2e` |
| `cmd/agent/task_executor_repo_cwd_e2e_test.go` | `t-executor-repo-cwd-e2e`, `t-executor-env-leak-e2e` |
| `cmd/agent/task_session_cwd_e2e_test.go` | `t-session-cwd-e2e` |
| `cmd/agent/worker_foreground_console_e2e_test.go` | `t-worker-console-fg`, `t-worker-console-daemon` |
| `cmd/agent/task_run_success_path_ledger_read_test.go` | `t-wrapper-false-success-red`, `t-wrapper-honest-success-green` |
| `cmd/agent/task_ground_e2e_test.go` | `t-ground-e2e-run` |
| `cmd/agent/task_ground_announcement_e2e_test.go` | `tgroundannounce` |

`task_ground_announcement_e2e_test.go` carried the premise in a comment — *"the
row id: alphabetic+digits only, so the ground's path and branch stems are its
own"* — which this change makes false; the comment is rewritten to state what the
fixture now needs (a **full uuid**, because the stem is derived from it) rather
than left to mislead the next reader.

**The six other failures in that run are not this branch's, and that is
measured, not asserted.** Five — `TestMergePromptCoversEveryRequiredSection`,
`TestMergeUnionRowCheckOnTheRealManifest` (an evidence anchor in
`devdoc/requirements/01-per-module-briefings.md` that names text
`internal/checkpoint/parse.go` no longer carries),
`TestSpawnersPinTheRepoContext` and `TestSubmitSpawnersGiveTheChildAResolvableRepo`
(+ `_ExactMatchRule`) (scan-floor drifts: "found only 64 lifecycle spawn
site(s), expected at least 80") — reproduce **identically on `master`**
(worktree at `ce1168dea`, `-run` the same five, same messages), and the sixth,
`TestLedgerOpenersUseProductionDSNContract`, fails on both trees **running alone**
(`cli_memory_help_surface_basic_test.go:1178 opens a ledger with a bare sqlite
DSN`), so it is a standing repo red rather than a package-order artefact.

**The closure reading, at the delivered commit.** `make test-budget PKGS='./cmd/agent'`
re-run at `3ac34711c` → **the same six failures and no others** (zero naming-gate
failures): `TestLedgerOpenersUseProductionDSNContract`, `TestMergePromptCoversEveryRequiredSection`,
`TestMergeUnionRowCheckOnTheRealManifest`, `TestSpawnersPinTheRepoContext`,
`TestSubmitSpawnersGiveTheChildAResolvableRepo`, `TestSubmitSpawnersGiveTheChildAResolvableRepo_ExactMatchRule`
— i.e. the branch adds no red to the package, and the ten walks that were red in
the previous run are green. (Transcript:
`…/artifacts/cmd-agent-final2.txt`, run at `3ac34711c`, elapsed 790s, exit 1 on
exactly those six; the same six are red on `master` `ce1168dea`.)

**The merge round (Rule 30a — the delivery-point sync, done last).** `master` had
moved from `ce1168dea` to `079e1efa1` while this branch was in flight, and it
brought a **sibling epic on the same subject**: `task_run_ground_uuid_branch_test.go`
("why cannot we use task UUID as branch name?"), whose two walks submit rows
`t-uuid-branch-orphan` / `t-uuid-branch-{other,mine}` through the same derivation
— so the uuid-only gate refuses them and both walks die at their first call.
Fixed the same way as every other fixture (`4a6fe7e74`: three full uuids, one per
row). The merge itself was conflict-free: `516567ad4`, `go build ./...` OK, tree
clean, `git merge-base --is-ancestor master HEAD` passes, and the gate this epic
added is still in place after it (`taskGroundUUIDStemLen` /
`taskGroundStemIsUUIDDerived` at `task_run_ground.go:133/158`).

**What the post-merge run says, split by cause.** `make test-budget PKGS='./internal/tools ./cmd/agent'`
at `516567ad4` reported the two slug-row walks above **plus four others**; the
four are the **newly-merged master's**, measured on the same revision
(`079e1efa1`, detached worktree, same `-run`):

| failure | on master `079e1efa1` | verdict |
|---|---|---|
| `TestResumeKeepsADirtyWorktree`, `TestWorkerResumeKeepsTheGuardsIntact` | FAIL, same two | master's (the branch that landed `8d3b4b19e`, "rescue a finished run's ground before reclaiming it") |
| `TestSyncTaskIDRefusedOnEverySessionFace` | FAIL, same | master's |
| `TestPrefetchBusyIsRetriedWithinTheSameBudget` | FAIL alone, **PASS** in the same `-run` on this branch | load-flaky (a 1500 ms prefetch budget under `load 9.20`) — not attributed to either tree |
| the two `task_run_ground_uuid_branch_test.go` walks | PASS on master (they only break once the gate exists) | **this branch's, and fixed in `4a6fe7e74`** |

Re-run on this branch after the fix, same `-run`: `internal/tools` green and
exactly the three master reds left in `cmd/agent`.

**And one red was NOT this task's, at either end of this work.** In an earlier
full-package run, `TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses/--tools`
failed with `runChat([--tools -h]) = 1, want 0` and `write |1: bad file
descriptor`, from a fixture that is **byte-identical to master** (`git diff master
-- cmd/agent/chat_argv_test.go` → empty). The failure is in the fixture's
**descriptor-teardown diagnostic** — a write end already released by a foreign
path, which the fixture's own helper classifies as the suite's descriptor leak
and reports rather than fails on — so it is a **pre-existing flaky teardown**,
aggravated by the load of concurrent test runs on this host (load average 12 on 4
cores during the runs above), not a naming regression. It reproduced **once** in
several thousand events, and the test passed on 4 consecutive isolated re-runs
and a `-count=8` run. Recorded here rather than "fixed": it is outside this
task's diff, and silently editing another fixture's teardown to make a report
green is exactly the over-reach this devdoc warns about elsewhere.

**The `cli_completion_exceptions_test.go` row is a DIFFERENT find, and it is not
this task's defect.** `TestCLINamingExceptionsAreStillDispatchable` fails on the
**pristine base** too: the spelling table
(`cmd/agent/cli_tool_spelling_table.go`) carries rows for
`toolnames.ToolWebSearch` / `ToolWebFetch` whose underscore spelling is an
exception, but the test's `predicates` map — the five tokens it checks for a
contract-inventory entry — was never extended with those two, so the gate reports
its own table as a hole (`"web_search" has no contract-inventory entry — the
exception would be unreachable from the contract suite`). Both tokens ARE
reachable (`gopi tool web search` / `gopi tool web fetch`); only the map was
short. It was corrected here because leaving it would have made the package's
declared entry point red for a reason unrelated to this task, i.e. it would have
hidden the naming regressions above inside an unrelated red. Recorded as a
separate finding, not folded into the naming work.

The six `cmd/agent`/`internal/tools` E2E fixtures were found the same way — by
running the package's declared entry point and reading which tests went red —
rather than by grepping for suspicious-looking ids: every one of them submits a
row that must **resolve a ground**, so each is a genuine consumer of the derived
name.

`TestTaskSubmitLinkedRepoRowIsStillRunnable` had been asserting that a ground
**resolves** while its row id was a slug, so an unconditional gate would have
made it test the refusal instead; the comment added there says so. The other two
fixtures in the same file (`t-submit-durable-repo`, `t-submit-main-repo`) are
left alone on purpose: they assert the **repo stamp**, never a ground, so no id
is derived from them — the same boundary §3 draws.

The `TestResumeArmNeverTouchesALiveHolderWorktreeE2E` reading is the one that
fixed the boundary rather than the fixture: it FAILED (60 s timeout, "timed out
waiting for the run to reach in_progress with a recorded identity") when the gate
was unconditional, because its row records its own worktree and the refused id
prevented the run from starting at all. After the gate moved to the derivation it
passes in 2.2 s.

---

## 7. Rule 5 report

### Completed
- The three naming families traced to their makers, one row/file each (§1).
- The orphan branch's full provenance: row `01a113e4-6776` (cancelled, item 2/5),
  parent `01a113e4-6730`, the shared 8-hex prefix, the doc-path collision with
  the parent's landed document, and the three rework rows it produced (§1.3).
- uuid-only ground naming: one minter, one shape predicate, one refusal, one
  gate before creation (§3).
- Prefix-attribution census and its closure (§4).
- Two regression tests with mutation red proofs; one existing fixture corrected
  that had been relying on a non-uuid id (§6).
- Coverage measurement of the orphan branch, and the resulting disposition
  (not archived, with the three paths that would make it archivable) (§5).

### Not Done (honest)
- **The orphan branch is not archived.** The epic's precondition ("prove master
  covers it") was measured and **fails**; §5.3 lists what would satisfy it. No
  `archive/…` tag was created and the branch still stands.
- **The pending merge-conflict rows are untouched** — resolving them means
  writing on master, which is outside this run's worktree. Their ids moved
  during the run; §1.3 records the re-read rather than the id that was true when
  this section was first written.
- **The `human-control` classification is exported but has no production
  consumer yet** (true as of the delivery commit; §1.2.1 is a later measurement
  of the same run). It is the vocabulary the chat/archive sweeps would need in
  order to stop reasoning about these directories by prefix alone; wiring it in
  is a separate change to code this task did not need to touch, and inventing a
  caller now would be a dependency created for symmetry (Rule 35).
  **After §1.2.1 the honest statement is stronger**: the classification also
  *collapses two different real families* (`gopi-fixproof`-style repositories and
  `gopi-crelease`-style control worktrees) into that one label, so a consumer
  wired in today would inherit the collapse — and the reclaim side does not need
  it (the stub predicate keeps a real tree on `.git`-is-a-directory /
  target-exists, neither of which reads the name's length). The distinction is
  expressible only by measuring the tree (`.git` kind, the pointer's target,
  whether a registration is live) — a different predicate, not a longer name
  rule.
- **`/dev/shm` control worktrees** were not touched or reclaimed: they are the
  operator's own trees.
