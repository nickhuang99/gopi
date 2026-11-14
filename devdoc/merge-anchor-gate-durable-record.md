<!-- doc-class: historical -->
# The published-anchor lane's decision had no durable record — 2026-10-16

**Task:** epic 01a144bfb (row `01a144bf-bce3-7f30-b634-1853e2eb1604`), host hostb-Laptop.
**Class:** the merge-time lane's OBSERVABILITY (system layer nominally peerbox's, Rule 28 —
see "Host note" at the end).

## The shape of the failure

Four published-anchor drifts landed on master in one day (2026-10-16), each a `Merge
branch …` whose own diff touched no generated document:

| landing | second parent | mover |
|---|---|---|
| `14861c100` | `e2a36517e` | `gopi task orphans` |
| `57fa55834` | `d0645ee41` | notify |
| `448d2a67c` | `d5682c17e` | scratch-gc |
| `3e07a4052` | `21c6d39e3` | doccheck |

The lane that exists to refuse exactly this (`mergeAnchorGate`,
`internal/tools/merge_anchor_gate.go`, called from `task_merge.go:745`) has **one refusing
arm and ~26 abstaining ones**, and until this change every one of them spoke only into
`taskNotifyLog` → stderr. Nothing on disk kept it: `~/.gopi/merger/` holds no log file at
all on this host. So the question "which arm let this through, and why" had no instrument —
the answer had been printed and had scrolled away, and the four post-mortems had to be
re-derived by reading the source.

**A lane whose reasoning is unreadable after the fact cannot be reviewed**, which is why
this is the prerequisite for every other judgement about it.

## What was built

`internal/tools/merge_anchor_gate_trail.go` — the DURABLE per-merge record, riding
machinery that already runs:

- The gate holds a trail (an ordinary local, `defer`red), and every one of the 30 lines
  the lane emits goes through `trail.log`, which writes the **same string** to the merger
  log it always wrote AND into the trail. A second vocabulary cannot drift from the first
  because there is only one.
- On **every** exit — refusal, abstention, pass, the operator switch, a panic — `commit`
  appends ONE row to `task_events` on the row the existing branch→row loader
  (`TaskForBranch`) resolves for the branch. That is the SAME table and the same ownership
  the sibling `ref-gone` arm already uses (`repo_sweep_ref_guard.go`), and it is registered
  in `verdictTrailEventNames`, so `gopi task show <id>` renders it on the row itself.
- The arm class is **DERIVED**, never typed at a return site: `Refused` from the verdict
  struct, `DISABLED` from the ONE knob reader, otherwise the leading token of the first
  line. The fifth value is the interesting one — a lane that emitted **no line at all** is
  recorded as `SILENT`, because that is a silent pass and Rule 15 forbids it. An empty
  record must never read like a clean measurement.
- Idempotent: the same decision about the same branch leaves one record; a different arm,
  branch or reason gets its own. A record that could not be written SAYS SO (no owner row,
  an open failure, an insert failure) rather than silently vanishing — the failure mode
  this file exists to repair.

## Rule 35 — the four questions, answered

1. **What breaks without it.** The four landings above are unexplainable after the fact;
   the reason survived only in a stream nobody kept. Observed, not hypothesised.
2. **Why not in-process.** The question is asked DAYS later, by a different process, about
   a merge that already happened. A stream is not a record.
3. **Who owns it.** Nobody has to: the change adds **no artifact of its own** — no file, no
   tick, no script, no tool, no verb, no knob. It writes rows into the ledger's existing
   append-only `task_events`, which the ledger's own backup/reclaim already covers, on a row
   resolved by the existing `TaskForBranch`.
4. **What the next person sees.** They do not have to find anything — the record is on the
   row they are already reading, rendered by `task show` as the row's newest verdict.

## The measurement that changes how this lane should be read

**On this host the lane's refusal arm is unreachable, and every merge lands unmeasured:**

```
$ git --version
git version 2.34.1
$ git merge-tree --write-tree master master
fatal: unknown rev --write-tree
```

`--write-tree` is git 2.38+ (`merge_union_gate.go:593` records the same measurement for
peerbox on 2026-10-13). The lane's FIRST materialiser is `mergeAnchorGateUnionTree`; when it
returns `ok=false` the lane abstains — so on a git-2.34 host the published-anchor question
is never asked at all, and the four 2026-10-16 landings were not "let through by a lenient
arm" but **never measured**. That is now readable in the record in the lane's own words:

```
merge-anchor-gate: <branch> ABSTAIN for <branch> into master (repo …):
  ABSTAINS for <branch> (merge-tree produced no tree: fatal: unknown rev --write-tree)
  — merging unchanged
```

This is why acceptance item 1 (the record) had to come first: without it the actual
mechanism — an old git on the merging host — was indistinguishable from a lenient gate.

## Known gaps (honest)

- **The refusal path is not exercised end to end here.** Every `TestMergeAnchorGate_*`
  fixture in `merge_anchor_gate_test.go` is `heavytest.Require(CriterionDeviceExec)`, and
  `GOPI_HEAVY_TESTS` is REFUSED inside a task run. The refusal's RECORDING is covered by a
  unit arm (`TestMergeAnchorGateArmVocabulary`); the refusal itself is not.
- **The four real landings were not replayed** through the lane: on this host the lane
  abstains before reaching any published-anchor question (above), so a replay would measure
  git's version, not the gate. The drift itself was confirmed directly instead
  (`commands-census.go --check` on a pristine HEAD tree: exit 1, four rows differing —
  exactly doccheck 393→415, notify 573→605, scratch-gc 647→670, task 525→531).
- **ABSTAIN is correct for the arm the regression test drives** ("the target already
  contains the branch" — there is no union to measure), and the record now states that
  reason on the row. Whether ABSTAIN is correct for the old-git arm is a **moot question
  until this host's git is new enough**: the lane cannot measure anything at all.
- **Host note (Rule 28):** the merge/verify machinery is peerbox's layer; this change was made
  from HP by the operator's dispatch, and its value on this host is bounded by the git
  version above. A host running git ≥ 2.38 is where the record's refusal arm becomes
  observable.

---

# Addendum 2026-10-16 (later): the refusal arm WAS unreachable — and for a second, unrelated reason

**Task:** row `01a144eb-0d51-7d2d-ab3b-51500a7132e2` (the follow-up the section above asked
for: "a host running git ≥ 2.38 is where the record's refusal arm becomes observable").

## How a git-2.38 host was obtained without a new dependency (Rule 35)

No host in this fleet has git ≥ 2.38 (measured again here: HP `git version 2.34.1`; peerbox
`git version 2.34.1`, and `apt-cache policy git` on peerbox offers only 2.34.1). So the
toolchain was **built from source inside a session directory** and used as a per-run `PATH`
prepend — the lane's `git` lookup is `exec.CommandContext(mergeUnionGateGitBinaryName, …)`
with `mergeUnionGateGitBinaryName = "git"` (`merge_union_gate.go:143`), i.e. `PATH` itself
is the injection point:

```
1 curl -sSL -o git-2.38.1.tar.gz https://gitee.com/mirrors/git/repository/archive/v2.38.1.tar.gz
2 tar xzf git-2.38.1.tar.gz && cd git-v2.38.1
3 make -j8 NO_GETTEXT=1 NO_TCLTK=1 NO_PERL=1 NO_PYTHON=1 NO_CURL=1 NO_OPENSSL=1 NO_EXPAT=1 NO_ICONV=1 prefix=$SB/gitbuild/prefix all
4 make … install          # → $SB/gitbuild/prefix/bin/git  → "git version 2.38.1"
5 mkdir $SB/toolchain && ln -s $SB/gitbuild/prefix/bin/git $SB/toolchain/git
   PATH=$SB/toolchain:$PATH gopi task merge --all --repo <fixture> --db <sandbox ledger>
```

`git merge-tree --write-tree master master` → `187438d7…`, exit 0 (2.34.1 answers
`fatal: unknown rev --write-tree`). Nothing was installed system-wide and nothing persists
past the session directory.

## Finding 1 — the premise leg never materialised anything (the real blocker)

With the union arm now gone through, the lane reached the PREMISE leg and died there on
every branch:

```
PUBLISHED-ANCHOR gate ABSTAINS for feat/anchor-drift (the branch's own document tests could
  not be measured: materialising feat/anchor-drift's tree failed: git archive
  2bac4d1ec26f33a5871af3541d001d9350c4bbda
  : exit status 128: fatal: not a valid object name: 2bac4d1ec26f33a5871af3541d001d9350c4bbda)
```

The newline in that message is the defect. `mergeAnchorGateTreeRun` fed `git rev-parse
<rev>^{tree}`'s **raw** output straight into `git archive`; `rev-parse` ends its line, so the
argument reached git as `"<oid>\n"`:

```
$ git -C $R archive "$(git -C $R rev-parse feat/anchor-drift^{tree})
"            → fatal: not a valid object name: 2bac4d1e…
$ git -C $R archive  "2bac4d1ec26f33a5871af3541d001d9350c4bbda"        → COMMANDS.md, go.mod, …
```

Its sibling `mergeUnionGateTreeAlone` (`merge_union_gate.go:431-437`) had measured exactly
this and trims — with a comment saying so; the anchor lane's own copy of the idiom did not,
and `mergeAnchorGateTreeRun` is reached ONLY on the path about to refuse, so **the lane could
not refuse anything, ever, on any host**. Same class, third site:
`mergeAnchorGateChangedPaths` (the BLAME arm) and `mergeAnchorGateTreeBlobs` (`git ls-tree`)
had it too — measured: `git ls-tree -r --full-tree "<oid>\n"` → `fatal: Not a valid object
name`.

**Fix** (three sites, one behaviour): `strings.TrimSpace` on the resolved tree, the sibling
leg's own rule. **Minimal repro**:
`TestMergeAnchorGateTreeRunResolvesATrimmedTree` — red with the trim removed (`git archive
<oid>\n … not a valid object name`, the same string the live run produced), green with it,
and NOT heavy-classified (it drives the gate's own plumbing over a real repo, no whole-tree
suite).

## Finding 2 — the durable record contradicted itself on a refusal

The refusal the lane finally reached wrote this row (`gopi task show`):

```
merge-anchor-gate: feat/anchor-drift REFUSED for feat/anchor-drift into master (repo …):
  the lane emitted NO line of its own — an exit that says nothing is a silent pass (Rule 15)
  and is recorded as SILENT | findings: …the document says internal/maganchor/thing.go:18 is
  the declaration, but that line reads "// branch-move01" — the document was NOT regenerated
```

The envelope said **REFUSED**; its own body said **SILENT**. `arm()` derives REFUSED from
the verdict struct before it looks at the lines (the refusal is the one arm that logs no
line of its own BY DESIGN), but `commit()` rendered the empty-lines branch unconditionally —
so the one record an operator reads to answer "which arm decided?" contradicted itself on
that exact field, and the SILENT token lost its meaning (it exists to mark an exit that said
nothing *and decided nothing*). **Fix**: the empty-lines branch now distinguishes a refusal
(it states that the findings ARE the reasons) from a genuine silent exit. **Repro**:
`TestMergeAnchorGateRefusalRecordNamesItsOwnArm` — asserts the envelope names REFUSED and the
body does NOT carry `SILENT`, and that the findings ride along.

## The answer the epic asked for

| question | answer (measured, git 2.38.1, scripted `task merge --all` on a real fixture repo) |
|---|---|
| which arm fires on the drift shape? | the **REFUSAL** arm — `verdict.Refused = true`; the merge line reads `build-refused … fails the published-anchor document tests internal/maggate` |
| verbatim reason | `magfixture/internal/maggate.TestCommandsDocThingAnchorResolves \| gate_test.go:40: the document says internal/maganchor/thing.go:18 is the declaration, but that line reads "// branch-move01" — the document was NOT regenerated` |
| is the trail readable on real hardware? | **yes** — `gopi task show 01a144eb-1111-…` renders `result:`/`verdict:`/`verdict.event: merge-anchor-gate` straight out of `task_events` |
| and the control? | `feat/anchor-control` (branch regenerates, master edits an unrelated file) → `PUBLISHED-ANCHOR gate PASS`, merge lands, `merged=1` |

So the arm is **not** defective: the drift shape IS refused, with a finding that names the
document, the line it published and the line it is stale against. What was defective was (a)
the lane's ability to reach the arm at all, and (b) the record's accuracy once it did.

## Host note (Rule 28), corrected

The earlier section's host note stands but is now narrower: the record's refusal arm is
observable **on any host**, because the only thing that was missing was a git that
understands `--write-tree` — and that is obtainable per-run (`PATH`, above), not by
installing anything. The lane's remaining coupling to a peer host's layer is unchanged.
