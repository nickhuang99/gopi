<!-- doc-class: historical -->
# The published-anchor edge on a tree that does not take part in it — ABSTAIN, never REFUSE

**Row**: `01a12de05cfb7a31bbbfb4e8419808ef` (successor of epic `01a12ba1`)
**Branch**: `task/01a12de05cfb7a31bbbfb4e8419808ef`
**Symptom**: the delivery chain (finish-cleanup → delivery-point sync) refused
**every** delivery whose `deliverable.repo` keeps no document of record, and with
it the release of every worktree — 26 finish-cleanup/closeout fixtures red on
pristine master, no branch's change involved.

## The defect, in one sentence

`internal/gendoc` answered **one** question — "which gate packages must run, and
what is broken" — and the delivery path read its *empty* discovery as a **BROKEN
EDGE**, while an empty discovery also means the plainest thing in the world:
*this tree publishes no generated document at all*.

The two readings have opposite remedies, and only one of them was expressible:

| tree | correct verdict | what master did |
|---|---|---|
| publishes `COMMANDS.md` with a generated block, generator present | VERIFIED / run the named gate | correct |
| publishes such a doc, generator **absent** | REFUSE (an instruction a reader would follow runs nothing) | correct — this is the epic's tooth |
| publishes **no such document** (another project's repo; the close-out's own throwaway fixture) | **ABSTAIN** | **REFUSED** — the defect |

## Why the fix belongs in the GATE, not in ~20 fixture manifests

Measured, on the failing fixtures themselves (`control-family-at-base.log`):

```
Detail:the published-anchor edge is BROKEN — no generated-document edge was discovered at all
```

The fixtures are `t.TempDir()` git worlds carrying `a.txt`, `b.txt` and a bare
"server" — **zero markdown files**. Three independent reasons the other
direction is wrong:

1. **The fixtures are genuinely non-participating, not merely un-excluded.** A
   fixture manifest would record "this tree opts out of an edge it was never
   in": the exclusion has to be *maintained* forever, in every future fixture,
   to say something the tree's own shape already says.
2. **The fixture's edits can NEVER hit an anchor.** The edge maps changed paths
   onto anchors published as `path/file.go:NNN` (`AnchorPattern`); a fixture
   that edits `a.txt` cannot move a published line number. There is no drift for
   the check to catch there — it has nothing to measure, which is exactly what
   ABSTAIN means.
3. **The delivery path runs on an ARBITRARY `deliverable.repo`.** A manifest
   inside *our* fixtures would do nothing for the next project whose rows this
   node closes out. The gate is where the general answer belongs.

The tooth is preserved by construction: a tree that *does* carry a document
stating a generated region or a regeneration command is still **PRESENT**, so
its broken edge is still a finding, and the delivery is still refused. An
abstention is bought by the tree's **shape** (nothing states a generated
region), never by weakening the refusal.

## The fix

ONE predicate, in the leaf package every consumer imports (Rule 14):

```go
// internal/gendoc/edge.go
func DepsSurvey(root string) (deps []Dep, marked, candidates int, err error)  // the walk, with its counts
func ClassifyParticipation(candidates, marked, docs int) (participation, reading, reason string) // THE predicate
func GeneratedDocAnswer(root string, changed []string) (Answer, error)        // the whole reply, one read
type Answer struct { Required, Findings []string; Participation, Reason, Reading string; Docs, Candidates int }
func (a Answer) TakesPart() bool
```

- `Participation` ∈ `{absent, present, unknown}`; `unknown` is the ZERO VALUE, so
  an `Answer` nobody filled abstains rather than passes (Rule 15a).
- `Reading` names the floor that came back empty (`no-candidate-document` /
  `no-generated-region`) — the abstention states which measurement produced it
  instead of a sentence a reader has to trust (Rule 15).
- On a non-participating tree `Answer.Findings` is **nil by construction**:
  `Impact` validates the edges the discovery *returned*, and an empty discovery
  returns none — so the old read-floor finding ("no generated-document edge was
  discovered at all") now means only what it says: *a participating tree lost
  its edges*.
- `GeneratedDocGates` / `GeneratedDocImpact` / `GeneratedDocMandate` are kept
  unchanged for existing consumers.

The consumer (`internal/tools/task_finish_gendoc.go`) reads it in order —
`TakesPart()` false → **ABSTAIN** (`finishCleanupPreMergeStatusSkipped`, detail
carrying the edge's own reason + both counts + the reading + the path count);
findings → **REFUSE**; else run the named gate package. The operator's escape
hatch (`GOPI_TASK_PUBLISHED_ANCHOR_GATE`) is untouched.

`MinEdges` (renamed from `minDeps`, previously referenced by nobody) is the
read-floor constant, now referenced by name.

## Evidence

**Red → green, the whole family** (`internal/tools`, same run banner, same
machine, `-count=1`):

| tree | FAIL |
|---|---|
| pre-fix (HEAD's five files) | **28** — every `TestFinishCleanup*`, `TestSalvage*`, both `TestCovFinishCleanup*`, 3 `TestRunCloseout*` |
| fixed | **3** — `TestRunCloseoutReapWatchesAPidItCannotMeasureYet`, `TestRunCloseoutSelectionReportsAHoleAndNotAMiss`, `TestRunCloseoutSelectionDoesNotCountAnotherProcessAsItsOwnHole` |

The 3 that remain were measured **change-independent**: they fail identically
with the fix reverted (`control-runcloseout-prefix.log`) and their message is a
kernel-timing premise (`the kernel has not reparented the fixture's member to
init (pid 1)` after 8s) with nothing `gendoc`-shaped in it.

The pre-fix refusal, verbatim, from the fixture runs — this IS the epic's
premise, measured rather than inherited:

```
--- FAIL: TestFinishCleanupCleanWorktree (0.08s)
  step push missing from report [{Step:align-before-delivery OK:true …}
  {Step:published-anchors Target:feat/sync-conflict-completed OK:false
   Detail:the published-anchor edge is BROKEN — no generated-document edge was
   discovered at all — the scan either did not read the tree or every document
   lost its generator instruction delivery-point sync refused — the worktree was
   NOT released; the branch keeps its check-out until it merges cleanly}]
```

**The red proof, per assertion** (`redproof-gendoc-prefix.txt`,
`redproof-consumer-prefix.txt`): with the pre-fix expressions restored, the new
arms fail on their own sentences — `TestGeneratedDocAnswerOnATreeThatDoesNotParticipate`
(an absent tree produced the broken-edge finding),
`TestGeneratedDocParticipationDistinguishesAbsentFromPresent` (a document stating
a generated region yielded "absent"), and on the consumer side
`TestPublishedAnchorGateAbstainsOnATreeWithoutADocumentOfRecord` and
`TestPublishedAnchorGateKeepsRefusingABrokenEdgeOnSuchATree`.

**The tooth, at the consumer's own level**:
`TestPublishedAnchorGateKeepsRefusingABrokenEdgeOnSuchATree` starts from the
abstention arm's exact tree shape (no document of record, no `cmd/` half) and
adds one document whose generator the tree does not hold → the delivery still
**REFUSES**. On the gendoc side, `Answer.Findings` on such a tree is non-empty.

**The real surface, run for real** (Rule 31 — private profile, throwaway git
world, no real row touched; `artifacts/e2e-*.log`):

```
$ GOPI_HOME=<session>/anchors-profile bin/gopi task finish-cleanup --task 01a12e08-… --apply
01a12e08-3064-7d65-8cd6-544ca11de9f9 branch=feat/anchors-e2e
  done=[align-before-delivery,published-anchors] failed=[pre-merge: …]
finish-cleanup (APPLY): candidates=1 removed=0 settled=1 refused=0
```

`published-anchors` **completed** — the delivery path no longer refuses this
tree (the chain then stops at `pre-merge`, correctly: the throwaway world's peer
master shares no history). Same verb, same row, same tree, **pre-fix binary**:

```
01a12e08-… done=[align-before-delivery] failed=[published-anchors: the
  published-anchor edge is BROKEN — no generated-document edge was discovered at all …]
```

And the tooth through the same CLI, on that same world once it carries
`PUBLISHED.md` (a generated region + a generator path the tree does not hold):

```
failed=[published-anchors: the published-anchor edge is BROKEN — PUBLISHED.md:
  names generator tools/anchors-census.go, which is not in the tree — the
  instruction a reader would follow runs nothing; PUBLISHED.md: no test package
  reads this document …]
```

## Not done / not claimed

- **The `--apply` run never reached `push`/`worktree`/`branch`** on that world:
  `pre-merge` refused (unrelated histories — the throwaway server's master and
  the branch have no common ancestor). The abstention's own worktree release is
  covered by the chain-level arm
  (`TestPublishedAnchorGateAbstainsOnATreeWithoutADocumentOfRecord` →
  `done=[align-before-delivery,published-anchors,pre-merge,worktree]
  worktree-freed=…`), which drives the same `finishCleanupServerRecycle` the CLI
  drives.
- **Three `TestRunCloseout*` arms stay red on this machine** — measured
  change-independent (pre-fix control above), a kernel-timing premise, filed as
  its own row with the measurement rather than folded into this fix:
  `01a12e1a-ab97-7a2b-bae8-371748513db8` (`runReapTestWaitAdopted` requires
  `ppid<=1`; `systemd --user` pid 3100 adopts the orphan — 3/3 FAIL on three
  consecutive runs, evidence `artifacts/bug-evidence-reap-window-adoption.md`)
  — the same root cause the rows `01a12c8a-9543` / `01a12cb5-eee0` record at two
  other call sites.
- **Two pre-existing gate failures on the untouched baseline** were found while
  measuring (`Rule 39`'s oversize permit for `internal/tools/task_finish_cleanup.go`;
  `Rule 17`'s four drifted hit lines in
  `internal/tools/taskid_whitespace_normalize_residual_test.go`) — **filed as
  their own bug rows** (`01a12e11-a959-7313-bcfe-2c2a78135acf`,
  `01a12e11-a972-7a2d-991c-9e54e24260de`) with the raw output attached, not
  repaired here (Rule 41: the finder evidences and files, it does not fix).
