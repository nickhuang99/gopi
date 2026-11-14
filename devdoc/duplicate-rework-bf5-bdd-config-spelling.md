<!-- doc-class: historical -->
# Two branches carried ONE deliverable — the merged one owns the report

Task: `01a12c07-762e-7186-9b8e-352b5cc6e967`, branch
`task/01a12c07762e71869b8e352b5cc6e967`, repo `/home/user/work/GoPi`.
Adjudicates the pair **`task/01a12bf5…`** vs **`task/01a12bdd-cc53…`** over the
CONFIG.md "retired spelling on a live command face" fix.
Every claim below carries the command that produced it and its reading. Nothing here
is inferred from a branch name or a commit message.

## 0. The verdict, first

**The surviving report file is
`tasks/01a12bdd-config-md-live-face-refused-spelling-gate-report.md`
(the `01a12bdd-cc53` report). `tasks/01a12bf5-config-md-648-gopi-shot-report.md` is
dropped — it reaches no ref and is recoverable only from the object store.**

No file is deleted by this run, and none needs to be: the bf5 report **is in no ref**,
which is the "already dropped" state this task was filed to reach. There is nothing to
`git rm`, because the file never landed anywhere a merge path can see.

## 1. What the two branches are — measured, not assumed

### 1.1 They are ONE commit, authored twice

```
$ git show bb34be4e4 | git patch-id --stable | awk '{print $1}'
a8959e2eb8b6f4868494c20014192176fe5c5637
$ git show 4bf754d01 | git patch-id --stable | awk '{print $1}'
a8959e2eb8b6f4868494c20014192176fe5c5637
```

**Identical patch-ids.** `bb34be4e4` is `01a12bdd-cc53`'s gate commit
(`gate(standards): CONFIG.md is a live command face — refuse a retired spelling there
too`, 3 files, +246/−22). `4bf754d01` is `01a12bf5`'s commit, same subject, same 3
files, same `+246/−22`. A patch-id is content-, not metadata-, derived: the two
commits are the same change.

bf5's own report states the mechanism, and this run confirms it independently:

> The commit reused here is `bb34be4e4` (family commit, not yet merged), cherry-picked
> onto this task branch as **`4bf754d01`**.

So the duplication is not two agents solving one problem twice — it is one branch
**cherry-picking the other's commit** and re-authoring it under a second task id.

### 1.2 One of them is already on the integration branch; the other is on nothing

```
$ git merge-base --is-ancestor bb34be4e4 origin/master && echo IN || echo OUT
IN
$ git merge-base --is-ancestor 4bf754d01 origin/master && echo IN || echo OUT
OUT
```

The whole `01a12bdd-cc53` line is in — every commit of it, tip included:

```
$ for c in ef8b69fd9 bb34be4e4 aedec4627 7f321422a; do \
    printf "%-10s %s\n" "$c" "$(git merge-base --is-ancestor $c origin/master && echo IN || echo OUT)"; done
ef8b69fd9  IN
bb34be4e4  IN
aedec4627  IN
7f321422a  IN
```

`7f321422a` is `01a12bdd-cc53`'s **tip** — that branch is fully merged, not
partially. bf5's tip `7085f00f8`, by contrast, is a dangling object:

```
$ git branch -a --contains 7085f00f8 | wc -l
0
$ git for-each-ref --contains 7085f00f8 --format='%(refname)' | wc -l
0
```

Zero refs contain it. And the row agrees with the refs — `gopi task show
01a12bf5-fee5-7331-ae41-78e523e4d1ba` reads `status: completed` while its own recorded
ground reads:

```
ground (resolved now): … worktree=/home/user/workspace/gopi-01a12bf5… (NOT FOUND now
— a stale reading) branch=task/01a12bf5… (NOT FOUND now — a stale reading)
verdict: removal skipped (delivery already attempted): the recorded worktree directory
is not there — nothing to remove
```

**A `completed` row whose branch exists in no ref is the exact state the rework lane
exists to catch** (Rule 30: the deliverable *is* the code it wrote, and it is in no
ref while the row reads completed).

### 1.3 The FIX itself is live on the integration branch

The thing the pair was about — `CONFIG.md:648` printing the retired top-level verb:

```
$ git show master:CONFIG.md        | sed -n '648p'
| `GOPI_SHOT_CHROME` | Chromium binary for `gopi shot`. Default: …  ← the DEFECT (local master is stale)
$ git show origin/master:CONFIG.md | sed -n '648p'
| `GOPI_SHOT_CHROME` | Chromium binary for `gopi tool shot`. Default: …  ← FIXED
```

and the gate that keeps it fixed is there too:

```
$ git grep -c 'TestConfigDocTeachesNoRefusedSpelling' origin/master -- internal/standards/
origin/master:internal/standards/commands_doc_gate_test.go:2
origin/master:internal/standards/config_doc_gate_test.go:1
```

`git log origin/master -- CONFIG.md` names `bb34be4e4` as the commit that last touched
that line, and `git blame -L 648,648 origin/master -- CONFIG.md` blames the same commit.
The defect is closed **by the merged half**, on the real integration branch.

## 2. Why "pick one" was not a coin toss

The two candidate report files are not two equal documents:

| | `01a12bdd-cc53` report | `01a12bf5` report |
|---|---|---|
| in `origin/master` | **yes** | no — in no ref at all |
| lines | 133 | 120 |
| describes a deliverable that landed | yes | no |

```
$ git ls-tree -r --name-only origin/master | grep 01a12bdd
tasks/01a12bdd-config-md-live-face-refused-spelling-gate-report.md
tasks/01a12bdd-report.md
```

Keeping bf5's report would mean landing a **second** account of the same 3-file change
next to one that is already merged — Rule 5's "written deliverable" is satisfied once,
and the ledger already has the merged half's row (`01a12bdd-cc53`, `completed`). The
bf5 report is not *additional* information either: 235 of its lines differ from the bdd
report, but the difference is the retelling, not the facts — both name the same commit,
the same three files and the same gate.

**And its one unique claim is false at the moment it would be read.** bf5's report ends
its provenance note with "(family commit, **not yet merged**)". Measured now,
`bb34be4e4` IS merged. A dropped report that is stale about its own central fact is
worse than absent: a later reader who found it would be reading a claim the refs
contradict.

## 3. The trap this run hit — local `master` is a stale snapshot, not the integration branch

Every "has this landed?" question above is answered against `origin/master`, and that
is not pedantry:

```
$ git rev-parse master origin/master
a3a31da42d84a7f04892d59955e8512cf19ccfda   ← local master
305ae03d0af5a8abfa30bf7de4ea10c9b8573407   ← origin/master = this run's own HEAD
$ git rev-list --left-right --count master...origin/master
0	233
```

Local `master` is **233 commits behind** and holds `gopi shot` (the defect) at line 648
— so a run that asked local `master` "is the CONFIG.md fix in?" would have concluded
**"not landed"** and re-implemented a fix that is already on the integration branch.
That is precisely the failure mode this rework row was filed to prevent, one level up.

The same reading shows a second fact worth naming: **this run's own branch is exactly
`origin/master`** (`git diff --name-only origin/master HEAD` → 0 files; the two
`rev-parse` outputs are byte-identical). The runner cut this ground from the freshly
fetched integration tip, so the adjudication lands as a first commit on top of it.

## 4. Why bf5's branch must not be merged, in one measurement

The thing a merge of bf5 would actually carry is not its 3-file fix (already present)
but its 233-commit-old base:

```
$ git diff --stat origin/master 7085f00f8 | tail -1
 100 files changed, 950 insertions(+), 10926 deletions(-)
```

`−10926` is 233 commits of other people's work reading as *deletions*. Merging it would
be a large, silent revert. Rule 23 is the rule that names this ("never continue work on
someone else's failed branch"); here the branch is not failed so much as **spent** —
its content arrived by the other path, and only its base is left.

## 5. Honest limits — what this run did NOT do

- **It did not delete or rewrite the bf5 report file.** The file exists only inside the
  commit `7085f00f8`, which no ref points at. Removing it would require pointing a ref
  at that history — the opposite of the disposition. Recoverable for as long as the
  object survives:
  `git show 7085f00f8:tasks/01a12bf5-config-md-648-gopi-shot-report.md`.
- **It did not touch the two ledger rows.** `01a12bf5-…` stays `completed`; the ledger
  is append-only and the row is itself the audit trail of this duplicate. Cancelling it
  is an operator decision, not this run's.
- **It did not mark bf5's branch for deletion.** The branch is already absent from every
  ref on this host; nothing here can or should resurrect it to delete it.
- **The report-file decision is a convention established here, not a mechanism.** No
  gate refuses a second report for a landed change. What makes this case decidable is
  the patch-id + ancestry measurement, and that instrument has to be *run*; a future
  duplicate without a cherry-picked twin may need a different reading.

## 6. Evidence — the readings, verbatim

All readings taken 2026-10-12 on `hosta`, in
`/home/user/workspace/gopi-01a12c07762e71869b8e352b5cc6e967`.

```
git show bb34be4e4 | git patch-id --stable   → a8959e2eb8b6f4868494c20014192176fe5c5637
git show 4bf754d01 | git patch-id --stable   → a8959e2eb8b6f4868494c20014192176fe5c5637
git merge-base --is-ancestor bb34be4e4 origin/master → rc=0
git merge-base --is-ancestor 4bf754d01 origin/master → rc=1
git merge-base --is-ancestor 7f321422a origin/master → rc=0   (bdd-cc53's TIP)
git branch -a --contains 7085f00f8 | wc -l   → 0
git rev-list --left-right --count master...origin/master → 0	233
git rev-parse HEAD origin/master             → 305ae03d0… 305ae03d0…  (identical)
git diff --name-only origin/master HEAD | wc -l → 0
git show bb34be4e4 --stat | tail -1   → 3 files changed, 246 insertions(+), 22 deletions(-)
git show 4bf754d01 --stat | tail -1   → 3 files changed, 246 insertions(+), 22 deletions(-)
```

Artefacts (outside the worktree, Rule 37):
`~/.gopi/sessions/01a12c07-762e-7186-9b8e-352b5cc6e967/artifacts/`.
