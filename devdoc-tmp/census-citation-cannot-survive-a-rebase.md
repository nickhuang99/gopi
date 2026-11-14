<!-- doc-class: historical -->

> **Retired mechanism (documentation-citation teardown, 2026-11-05).** 本页记录的机制在树里
> **已不存在**。`78bdf05a`（operator ruling）与 `0ee5745c` / `1e3b0976` 删掉了整个
> documentation-citation 家族：三个生成器 `internal/standards/testdata/config-anchor-census.go`、
> `citation-census.go`、`citation-selector-census.go`，它们写的三张 `.tsv` 表，驱动它们的
> 两道门 `internal/standards/citation_gate.go` / `citation_selector_gate.go`（其表已不存在，
> 这两道门本体由 `1e3b0976` 删除；`78bdf05a` 删的是它们的测试），
> 以及交付路径上的 census-citation 腿（`internal/devworkspace/census_gate.go`、
> `census_redrive.go`，见 `0ee5745c`）。
>
> **下面没有任何一条命令还能跑**：`go run …` 得到 `stat … no such file or directory`，
> `go test -run TestMergeConfigAnchorGate` 打印 `[no tests to run]` 并 exit 0（**空绿，不是通过**）。
> 本页保留为**历史记录**：它记的是当时那个缺陷与堵住它的推理。测量方法本身已随生成器消失。
>
> 今天仍能跑、也仍该跑的，是 Rule 36 的语料门：
>
> ```bash
> go test ./internal/standards/ -run RetiredPipeline -count=1
> go test ./internal/standards/ -run TestEveryCorpusDocDeclaresItsClassInItsHeader -count=1
> ```

# A commit-hash citation cannot survive a rebase — what to anchor on instead

**Scope.** The `# measured-at: <rev> [provenance: committed]` line that BOTH census
tables carry (`internal/standards/testdata/citation-baseline.tsv`,
`internal/standards/testdata/config-anchor-census.tsv`), and the row that keeps
returning: a table cites a revision that is an object in NO clone, so
`CitationCensus` / `TestMergeConfigAnchorGate_RealTreeIsClean` go red, somebody
re-derives, and the repaired table is red again after the next rebase.

This page answers the ONE question the earlier pages did not: **why "re-derive on
the rebased tip" keeps invalidating itself.** It is not a bug in the generators,
not a stale table, and not a missing gate — it is that the citation's ANCHOR TYPE
is a commit id, and a commit id is the one identifier `git rebase` deliberately
throws away. Measured on this tree, second occurrence of the class (the first is
`devdoc/census-citation-row-move-publishable.md`, which fixed a different half:
whether a ROW move may be published).

## 1. The measurement, on this tree

Both shipped tables cited `54ea82f4289c612d3796ab546e0e87c1fc5a745a`:

```
$ git cat-file -t 54ea82f4289c612d3796ab546e0e87c1fc5a745a
fatal: Not a valid object name 54ea82f4          (exit 128)
$ git reflog show --all | grep -c 54ea82f4
0
$ git -C /home/user/work/gopi cat-file -t 54ea82f4     # the dev repo itself
fatal: Not a valid object name 54ea82f4
```

So the object is gone from this clone, from the reflog, and from the dev repo —
there is nowhere to fetch it back from. Two gates refuse on exactly that fact:

```
$ go run internal/standards/testdata/citation-census.go . \
      internal/standards/testdata/citation-baseline.tsv --check
citation-census --check: … claims provenance "committed", but the revision it cites
(54ea82f4…) is NOT AN OBJECT in this repository — no clone can show it, so the 527
row(s) below cannot be re-derived at it.                              (exit 1)

$ go run internal/standards/testdata/config-anchor-census.go . \
      internal/standards/testdata/config-anchor-census.tsv --check
config-anchor-census --check: … claims provenance "committed", but the revision it
cites (54ea82f4…) is NOT AN OBJECT in this repository …                (exit 1)
```

The second table ALSO carried an independent defect — its rows had drifted from
`CONFIG.md` (5+ anchors re-pointed by later commits, reported as `-table`/`+tree`
deltas). Two independent reds, which is why a single re-derivation must fix both
and why "it was green last time" was never evidence.

## 2. Why the anchor type is the root cause

`git rebase` does not edit commits; it **replays** them. A replayed commit keeps
its TREE and its message and gets a **new parent**, therefore a new object id. The
old commit stays reachable only through the reflog until `gc` collects it — and
`gc.pruneExpire` is 2 weeks, not forever.

This is visible in this repository's own object store, without any archaeology:

```
$ git log --all --format='%T' | sort | uniq -d | wc -l
10                     # ten trees each carried by two or more commits — the
                       # signature of a replay that kept the content and changed the id
```

Now the decisive table. For every `chore(census)` commit in this history, what did
it cite, was that value an object, and — the question that matters — was it
ALREADY PUBLISHED on `origin/master` at the time it was written?

```
HASH      CITED       PARENT      CITED=PARENT?  EXISTS   ANCESTOR-OF-HEAD
400f837   54ea82f42   f564f2ed2   no             MISSING  no      [both tables]
30f1fa9   09a90b272   d00e7392c   no             commit   yes     [citation]
30f1fa9   d00e7392c   d00e7392c   YES            commit   yes     [config-anchor]
858aac0   09a90b272   9fbe070e0   no             commit   yes
b857335   53c694a72   d5821d223   no             MISSING  no      [config-anchor]
da9d748   4a6dac6ce   da4200543   no             MISSING  no      [citation]
8f68e59   4a6dac6ce   a82157d6f   no             MISSING  no      [citation]
9b028a7   4a6dac6ce   619a3db43   no             MISSING  no      [citation]
e341a7e   af0972fd9   5ef68bd71   no             MISSING  no      [config-anchor]
```

Two facts fall out, and they are the whole answer:

- **`400f837` cites `54ea82f4`, and its own parent is `f564f2e`.** The machine
  commit that was supposed to REPAIR the citation wrote a value that was neither
  its own id nor its parent's — a revision that had already been rewritten away
  before it was typed in. Its diff is a three-line change to a `# measured-at:`
  line (`git show 400f837 --numstat` → `3 2` on the citation table, `4 4` on the
  anchor table), i.e. the repair ran, and it repaired *toward a dead hash*.
- **Every citation that survived is one whose value was already published or is
  the writer's own parent.** Every citation that died names a commit that the
  channel's `pull --rebase` later replayed. The channel is working as designed:
  `internal/devworkspace/devworkspace.go` runs `clean → pull --rebase → …`, and the
  rebase step *is* the step that replaces the base the table was measured on
  (`census_gate.go` says so in its own comments — it was built to catch precisely
  this).

So the recurrence is not "somebody forgot to re-derive". It is:

> A census table cites the tip it was measured at. The tip of a branch that has not
> been PUBLISHED yet is a commit the rebase will replace. Therefore a table whose
> citation names its own branch's tip is **wrong the moment the delivery loop runs
> — by construction**, and re-deriving on the new tip merely moves the citation to
> the next tip that will be replaced.

## 3. What to anchor on instead

The decisive table, computed over every citation in this history (the same
population as §2, grouped by the two candidate criteria):

```
ALIVE + published (ancestor of origin/master)   18 / 18
DEAD  + unpublished                              8 /  8
"cited == the writer's own parent"   NOT a criterion: 8 ALIVE are parent,
                                     8 ALIVE are not parent, all 8 DEAD are not parent
```

So the criterion is ONE property, and it is about publication, not about ordering,
not about ancestry-of-this-clone, and not about "the previous commit":

> **A citation must name a revision that the channel's `git rebase` will never
> replay — i.e. a revision already reachable from the integration branch. The only
> second way to satisfy it is to write the citation AFTER the last rewrite in the
> attempt, so that the value it names is the very tip that push publishes.**

"The writer's own parent" is a coincidence, not a rule: a parent that is not
published is exactly as dead as any other unpublished tip (`400f837` cites a value
that is neither its id nor its parent's, and it is dead).

Operationally, in the order an author actually needs it:

1. **Cite an already-published revision when you have the choice.** This is the
   immune shape, and the generators make it cheap without a second checkout
   (Rule 29) — `--at` reads the index and the bytes out of the OBJECT STORE, and
   the written line says so:

   ```
   go run internal/standards/testdata/config-anchor-census.go . \
       internal/standards/testdata/config-anchor-census.tsv --print --at <published-rev>
   ```

2. **If you must cite a fresh tip, write the citation AFTER the last history
   rewrite — never before it.** A `pull --rebase` that runs after the measurement
   replays the very commit the table named. This is why the delivery loop's
   re-drive leg (`internal/devworkspace/census_redrive.go`) sits *after* the pull
   step: it re-derives on the rebased tip and commits that value, so the citation
   names the tip that push uploads and becomes published in the same push. A run
   that re-derives at its own HEAD and then hands over is doing the measurement one
   rewrite too early — that is the whole recurrence, stated as a schedule.
3. **Never hand-edit the line.** Both generators are the only writers, and both
   refuse to write a reading that is not a commit (the write leg checks the
   provenance state and exits rather than baking `dirty-worktree` into a tracked
   file). The repair command is in each table's own header.
4. **A `--check` red is not automatically the table's fault.** Run both generators;
   a row delta (`-table`/`+tree`) means `CONFIG.md` moved and the TABLE must be
   re-derived, while a bare exit 1 on provenance means only the citation died.
   Fixing the citation without fixing the rows leaves a green citation over a wrong
   population — the second red in this occurrence, measured as
   `+cmd/agent/task_ledger_agreement_structural_test.go` plus `rows: 526 → 527` in
   `400f837`'s own diff.

## 4. Why the machinery that already exists could not save these

`internal/devworkspace/census_redrive.go` re-derives the citation inside a delivery
attempt, and it is deliberately narrow: it commits the moved line only when every
ROW is byte-identical, or when the written citation IS the tip being published
(`censusRederivedCitesPublishedTree`). That is the correct answer to "a rebase moved
the base under a good table".

It cannot help a table whose citation is a *dead hash*, because:

- the generator it runs writes the CURRENT `HEAD`, so the "one line moved" arm
  produces a new valid citation — the leg would have repaired this, had the delivery
  attempt been the thing that noticed;
- and the "rows moved" arm is refused unless the citation is HEAD, so a table whose
  ROWS are also stale is left deliberately blocked (Rule 38: a machine must not
  choose between two readings).

Hence: red reached `master`, which means the red was observed OUTSIDE a delivery
attempt — by the branch-scoped gate `TestMergeConfigAnchorGate_RealTreeIsClean`,
which `internal/tools/task_finish_premerge_census.go` lifts to a package-level check
so it fires in the round that introduces the drift, not the round that happens to
touch `internal/tools`. And `400f837` is the proof that a machine re-derive can
itself write a dead citation: the tip it read was already gone.

**Half of this gap is now measured, and this paragraph said it could not be.** The
two probes the generators ran were "is an object here" (`git cat-file -e`) and — for
the citation table — "is it an ancestor of HEAD"; both answer about the LOCAL object
store, and both are true of a commit every ref has been rewritten past. The
consequence was worse than "the check cannot see the future": a table citing such a
commit printed GREEN until `git gc` collected the unreachable object, and only then
turned red for a reason no reader could still see. That is not a prediction problem,
it is a missing measurement, and it is closed: `checkCommittedCitation` in BOTH
generators now asks a third, independent question after existence — is this revision
reachable from any ref (`git for-each-ref --contains <rev>`) — and refuses with its
own diagnosis (the PUBLICATION gap) when nothing is. Publication is exactly the
property this page states as the criterion, so the check now measures the criterion
instead of only describing it.

**What still cannot be measured**: a citation to a commit that exists, is reachable
from a ref *here*, and will be rewritten *later* by the delivery loop's `--rebase` —
a local unpublished tip passes all three probes because it is published in this
clone. Choosing that revision, or writing the citation after the last rewrite, stays
anchoring practice (Rule 35: the fix is a rule obeyed by authors, not a new resident
mechanism).

## 5. What this page changed in this run

- Both tables re-derived at `980d874` — `origin/master`'s tip at the time, i.e. a
  revision the delivery loop will never rewrite, verified with
  `git cat-file -t 980d8742e0f5c83b91dfdf535bb4eea1ea253247` → `commit`.
- `PROJECT.md` Rule 38 gained the practice paragraph this page explains (the anchor
  TYPE requirement), and `CONFIG.md` gained the matching operational line, so the
  next author meets the rule at the two places they already read.
- Re-verified afterwards: `citation-census --check` and `config-anchor-census
  --check` both exit 0, `TestCitationCensusReDerivesTheTable` and
  `TestMergeConfigAnchorGate_RealTreeIsClean` green — the commands are in this run's
  report (`$GOPI_TASK_REPORT_PATH`), not retyped here as if they were evidence.

## 6. The write leg itself now refuses the defect (the generation-2 fix)

§3 stated the criterion as PRACTICE FOR AN AUTHOR — "prefer a published revision",
"if you must cite a fresh tip, do it after the last rewrite". Measured over the
next occurrence, practice was not enough: the sanctioned repair ("run the command
in the table's header") still ran at a head a rebase would replace, and `--check`
stayed GREEN the whole time, because a local branch ref DOES reach the revision at
the moment it is asked. A rule whose only enforcement is that nobody forgets it is
not enforcement (Rule 38's own lesson, one level down).

So the WRITE LEG asks the publication question when the line is written
(`writeLegMeasurementNote` in each generator), and it has exactly four answers —
no silent fallback to citing an unpublished tip, because that fallback IS the
defect:

| # | the measured situation | what the write leg does |
|---|---|---|
| A | HEAD is reached by a ref a rebase does NOT rewrite (a remote-tracking ref, an archive tag, a `chat/*` scratch branch) | cites HEAD, and the `(clean worktree: …)` sentence names the ref that carried it |
| B | HEAD only from this clone's own `refs/heads/*` | cites the nearest ANCESTOR that is (i) reached by a surviving ref and (ii) whose committed copy of the table holds EXACTLY the derived rows; the sentence names BOTH revisions |
| C | no published ancestor holds these rows | REFUSES, exit 2, writes nothing, and names the published candidates the row comparison refuted |
| D | `--publishing-head` | cites HEAD, because the caller is the delivery channel's own re-derive leg, which is about to publish exactly this commit |

**Why the row comparison is part of the answer, and not an optimisation.** Answer B
would otherwise commit the `--at <base>` shape this program refuses elsewhere: a
published revision cited as the source of rows its tree does not hold. So the
candidate is compared against `git show <rev>:<table>` — the reading's own rows,
row for row — and a candidate that differs is REFUTED BY NAME in the refusal. That
is also what makes answer C honest: a reader can see the walk MET published
revisions and rejected them, rather than being told "nothing fit".

**Why an unmeasured probe is not "unpublished" (Rule 15).** A `for-each-ref` that
cannot run prints nothing, which reads exactly like "no ref reaches this
revision". The probe therefore answers TWO things — the refs, and whether it could
measure at all — and a failed probe produces a refusal that says the publication
was NOT MEASURED, never a citation and never the word "unpublished".

**One implementation, two generators.** The publication question lives in
`internal/configanchor/publication.go` (`RefsCarrying`, `RefIsRebasedAway`,
`PublishedAncestor`) — both generators are `package main` programs under testdata/
that cannot import each other, and a copy of a correctness rule in each is two
spellings free to drift apart (Rule 14). The ref half is shared; "the revision
carries this reading" is the CALLER's predicate, because the artifact differs (a
citation table's rows, an anchor table's rendered body).

**The channel's own leg asserts (D), because it is the one caller that knows.** The
re-derive leg (`internal/devworkspace/census_redrive.go`) runs AFTER the attempt's
`pull --rebase` and BEFORE its push, so its HEAD is — by construction — a revision
no surviving ref reaches yet. Under (B)/(C) alone it would be PERMANENTLY refused on
the one case it exists for (a branch that legitimately moved an anchored line, where
the only table describing that tree is measured on that tree). So it passes the
generator's `--publishing-head` (`configanchor.CensusPublishingHeadFlag`,
`censusPublishingCommandFor`) and thereby states the fact it owns. The flag is
refused on `--check` and `--print`, which write no citation at all. Nothing has to
remember that it wrote the line: the next attempt re-asks both probes against the
NEW head, so the claim is re-earned or the table is red.

**The repair command printed in a table's header does NOT carry the flag.** A human
copying it would be asserting something that is not theirs to assert.

**What a refusal costs, measured.** Written by a run that hit it in the very first
draft of its own arms: the fixture's published parent was refuted because the
arm had TYPED the table itself instead of materialising it through the generator's
`--print` leg — so the rows differed by spelling, not by content. The lesson is not
about the fixture: **the rows a citation is compared against must be the rows the
writing program DERIVES**, which is exactly why `renderedRows` is one function with
two readers (the table's body, and the ancestor comparison) rather than two
renderings free to drift.

**The arms** (`internal/standards/citation_census_publication_leg_test.go`): one per
answer, all driven through the real program over a real repository whose ref
topology the arm arranges (A: a published ref on HEAD; B: HEAD reachable only from
`task/fixture`, its parent published and holding the same rows; C: same, with a row
ADDED on the branch, so the parent's copy is refuted; D: the flag), plus a RED
PROOF that runs the generator from the commit BEFORE this change on B's own fixture
and asserts it cited the unpublished tip. The red proof builds it INSIDE the module
(testdata/ is never matched by `go test`'s patterns) because a file outside it
cannot import `internal/configanchor`, and a red proof that could not build the old
program would report a green nobody measured.
