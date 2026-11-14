<!-- doc-class: historical -->
# The reported red was real and is already repaired — but the ESCAPE that produced it is not

**Status:** a measurement record of one run's findings (2026-10-13, task
`01a13512-2d06-7f2d-b81a-3b06f88fec4f`, hosta — client), classified **historical**
by the corpus's own classifier, which is the authority on the class: the doc's
prose reads as a run's account and no current artifact cites it as authority.
Sibling documents in this family carry the same class. The section "The repair,
and why it is filed rather than implemented here" nevertheless states a standing
claim about the merge path, and is intended to be readable as one.

Its evidence spot-checks are the structural facts behind the escape — the
candidate scope the published-anchor edge carries (`internal/gendoc/edge.go:308`),
which is why a `devdoc/` file is not a document that edge would even consider, and
the fixed membership list the merged-result step assembles its run set from
(`internal/tools/rebuild_vet_mergeset.go:159`).

Another drift of the same family landed on the integration branch while this run
was in flight, and the resulting red was measured and repaired **on this branch**
(§7). The escape is therefore not an inference from an old report: it reproduced,
unattended, within about twenty minutes of the measurement recorded below.

**Headline.** A bug row reported `gopi tool doccheck` exiting 1 on a clean master
checkout, with two unpermitted anchor-NUMBER drifts. Everything in that report was
accurate, and it is now stale in exactly one respect: the two drifts were repaired
independently (docclass task `01a1350c`, commit `38577cc3e`) and master was green
at `0c4679a74`.
What the report was actually reaching for — the reason a red could reach master
**unilaterally** — is unrepaired, and this document measures it, names its three
arms, and states the repair that belongs to the system layer (peerbox, GOPI.md Rule
28).

## 1. What was measured, and at which revisions

Rule 38: every figure below names its generator and the commit it was measured at.

| revision | what it is | `gopi tool doccheck` | table data rows |
|---|---|---|---|
| `6ef428c16` | the filing's baseline | **exit 1** — both drifts NOT PERMITTED | 163 |
| `0c4679a74` | the wire tip of master = this run's HEAD | **exit 0** — both drifts PERMITTED | 166 |

The verb was run for real at both revisions — at `6ef428c16` inside an archived
detached checkout (`git worktree add --detach /dev/shm/gopi-base-6ef428c16
6ef428c16`, Rule 22: `/dev/shm` is a tmpfs, off the `/` partition), never inferred
from a diff. Raw output:
`/home/user/.gopi/sessions/01a13512-2d06-7f2d-b81a-3b06f88fec4f/artifacts/evidence-doccheck-anchor-number-state.md`.

At `6ef428c16` the verb printed, verbatim:

```
doccheck:   NOT PERMITTED (drifts, absent from the baseline): devdoc/hp-push-topology-audit.md#996: anchor cmd/agent/cli_tasks_worker.go:638 at serveGitSyncSweep(workerRoleLabel) — number-drifted (recorded text is at :650)
doccheck:   NOT PERMITTED (drifts, absent from the baseline): devdoc/scratch-gc-flight-join.md#1698: anchor cmd/agent/cli_worker_control.go:2167 at func (c workerDaemonCtx) installFileWatcher() (uninstall func()) { — number-drifted (recorded text is at :2183)
doccheck: this run exits 1 on anchor NUMBER drift (see the section above), independently of the 0 gap(s)
```

The report's own `#997` / `#1699` are the **manifest row numbers**, not the
recorded line numbers — the drift itself is `:638 → :650` and `:2167 → :2183`,
which is exactly what the report named. The filing was accurate.

At `0c4679a74` both keys are `first-frozen` rows of
`internal/docclass/testdata/anchor-number-baseline.tsv` (rows 97 and 147), and its
header states `# figures: frozen=166 pre-existing=152 first-frozen=14` — the
output of `internal/docclass/testdata/anchor-number-census.go`
(`docclass.AnchorNumberCensusCmd`), the table's ONE generator (Rule 38; this table
and its header are DATA, never hand-edited).

**Ancestry** (`git merge-base --is-ancestor`): the repair `38577cc3e` is an
ancestor of `0c4679a74` and of this run's HEAD, and is **not** an ancestor of
`6ef428c16`. So the red at the filing's baseline and the green at the wire tip are
two genuinely different trees, and both facts are true. `38577cc3e` is
`chore(docclass): re-emit the frozen anchor-NUMBER baseline on current master`,
authored on branch `task/01a1350c31a87bcf8fd4b6ef7bd10e5b`.

## 2. The escape — why a red could reach master unilaterally

The repair existed **before** the row was filed: the shipped gate had already
detected it. At `6ef428c16`, `go test ./internal/docclass/ -run
'AnchorNumberGateOnTheRealTree|…' -count=1` FAILS (exit 1, 4.891s):

```
--- FAIL: TestAnchorNumberGateOnTheRealTree (0.19s)
    anchor_number_gate_test.go:142: 2 anchor row(s) record a line number that no longer points at
    their recorded text, and are NOT in the frozen baseline
```

So this was never a *detection* gap. It was a **routing** gap, and it has four
arms which compose into an abstention everywhere.

**Arm ① — the leg IS wired, but every arm asks about the UNION.**
`RebuildAfterMergeMergeset` does widen its run set with a devdoc-anchor leg
(`internal/tools/mergeset_docclass.go` → `mergesetDocclassRuns`,
`internal/tools/rebuild_vet_mergeset.go:441-447`), and its trigger is pinned by a
shipped test (`TestMergesetDocclassRunsRequiresTheCarrierWhenTheUnionNamesAnAnchoredSource`).
So the gap is **not** "no lane asks". It is *what each lane asks*: the leg reads
`mergesetGendocUnion(ctx, repo, mergeRev)` — the union at MERGE time — and fires
only when that union names a path the manifest anchors into (or a gated document)
(`mergesetDocclassHits`, `mergeset_docclass.go:236`). The four refusing merge-time
gates compose `merge-tree --write-tree master branch`; the pre-push arm computes
`finishCleanupBranchAnchorChangedPaths(dir, base, branch)`. Every one of them is a
statement about `merge(master, branch)`.

That gives a drift master made by itself exactly **one window** in which any arm
can see it: while a branch is actually merging. Once the union stops naming a
manifest path — which it does as soon as no landing is in flight that touches one
— all arms abstain with their own counts. A red integration branch with no
suitable landing in flight is measured by **nothing on the merge/open path**. The
only detector left is a human running `gopi tool doccheck`, which is precisely
what the filed row was.

This is the same shape as arm ④, one level up: the lanes are scoped to the
*incoming* change, so a defect the *target* holds is nobody's diff.

**Arm ② — the leg that DOES ask is report-only, by design.**
`internal/tools/mergeset_docclass.go` is the member that closes arm ①'s gap, and
its own contract leaves the step verdict alone. The file's header (lines 69-72)
says it plainly: the red is *"REPORTED through the merged-result step's own
channels (notify + bug row, deduped by defect identity) with `res.OK` left alone,
exactly like the sibling union leg."* The only occurrence of `res.OK` in the whole
file is that comment.

**Arm ③ — the close-out arm fires only on the branch's OWN diff.**
`finishCleanupBranchAnchorCheck` (`internal/tools/task_finish_premerge_smoke.go`
:342) asks the question of the branch's change against its base — its first act is
`finishCleanupBranchAnchorChangedPaths(dir, base, branch)`, and a landing that
touched no devdoc manifest anchor source or gated document abstains with the
counts it measured. Both drifted rows anchor into `cmd/agent/*.go` (an *anchored
source*, not a regenerated document), so which landings oblige it is a statement
about their diff — and the landing that moved `:638 → :650` / `:2167 → :2183` did
not.

**Arm ④ — every REFUSING merge-time lane measures the UNION.** `mergeBuildGate`,
`mergeUnionGate`, `mergeContractGate` and `mergeAnchorGate` all take
`(repo, master, branch)` and compose `merge-tree --write-tree master branch`. When
**master itself** is the red side, the union's failure cannot be attributed to the
branch, and each lane abstains by its own stated contract — `merge_anchor_gate.go`
:305-318 says it in its own words: *"the TARGET is deliberately NOT required to
pass … What replaces it is BLAME … the union's failure must name an anchor in a
file THIS BRANCH's own change touched."* A drift master made by itself names no
file the incoming branch changed, so all four abstain.

**The composed result, in one sentence.** A landing can move a line that some
devdoc anchors into; its own touched-file set obliges no lane (①②③); the landed
master is then red on its own account; and every refusing lane (④) abstains
because the red is not the *incoming* branch's. The only remaining detector is a
human running `gopi tool doccheck` — which is precisely what the filed row was.

## 3. What is NOT claimed here

- **No claim that master is red now.** It is not: `gopi tool doccheck` exits 0 at
  `0c4679a74`, the wire tip, measured this run.
- **No claim that the escape is undetected.** Arm ② *does* detect and report it
  (notify + a deduped bug row). What it does not do is **refuse** the landing. The
  distinction that matters is report → refuse, not detected → undetected.
- **No claim about who should decide.** Whether the devdoc-anchor family becomes a
  refusing lane is a policy decision for the system layer; this document supplies
  the measurement, not the verdict.

## 4. The repair, and why it is filed rather than implemented here

The repair rides arms that **already run** (GOPI.md Rule 35 — no new script, hook,
cron or resident dependency). Two candidate shapes are visible from the
measurement, and both are edits to the merge/close-out machinery:

1. **Widen the membership list** (`rebuild_vet_mergeset.go:151-162`) so a landing
   whose diff touches a devdoc manifest anchor source also puts `internal/docclass`
   in the run set. Arm ① disappears, and the measurement is the same
   branch-scoped smoke run the other three directories already get.
2. **Let the docclass leg set the verdict** (`mergeset_docclass.go`), turning
   report-only into refuse — with the attribution question arm ④ answers for the
   other gates (the failure must name a file *this landing* changed), so a red
   target does not freeze the queue for someone else's drift.

`internal/tools`' merge/close-out machinery **is the system layer** (GOPI.md Rule
28: peerbox owns it), so this hosta run measures and files, and does not implement.
The row carries the evidence paths and the mechanism; the host requirement is
stated on it.

## 5. Evidence

- Raw commands and outputs, both revisions, with exit codes:
  `/home/user/.gopi/sessions/01a13512-2d06-7f2d-b81a-3b06f88fec4f/artifacts/evidence-doccheck-anchor-number-state.md`
- The measurement was taken by running the verb, never by reading a diff: the
  baseline leg ran in an archived detached checkout at `/dev/shm/gopi-base-6ef428c16`
  (off the `/` partition, Rule 22), created with `git worktree add` and removed
  after the measurement.
- Generator of this document's figures: `internal/docclass/testdata/anchor-number-census.go`
  for the table row count and the header figures; the verb's own output for the
  exit statuses.

## 6. The rows this run filed (Amendment, same session)

Both are `--tags bug` rows on this host's ledger, filed per Rule 41 with the
evidence paths above; neither is implementable here (Rule 28):

- `01a135da-02fa-726b-963f-76187d31001d` — this document's defect: the
  devdoc-anchor family is report-only and union-scoped, so a drift master makes by
  itself is refused by no lane. Host requirement: **peerbox**.
- `01a135da-debd-786d-b384-c0a0751d5923` — found while verifying that the first row
  was actually readable: its predecessor submit had landed in a **nested profile
  ledger** (`/home/user/.gopi/.gopi/memory.db`, 2 rows) instead of the profile's own
  (`/home/user/.gopi/memory.db`, 1376 rows), because `config.GopiDir()` resolves
  `$GOPI_HOME/.gopi` and the mis-spelled value that produced it is the profile root
  itself. Measured and evidenced separately in
  `/home/user/.gopi/sessions/01a13512-2d06-7f2d-b81a-3b06f88fec4f/artifacts/evidence-nested-profile-ledger.md`.
  This run's own submit went to the correct ledger, so the repair is about the
  resolution window, not about a reproduction of the write.

A claim in this document is a claim about the code as measured; a claim in an
evidence file is a command and its output. Neither file asserts the other's
subject.

## 7. Amendment (later in the same session): the escape REPRODUCED, and the red it left is live on master

The sections above measured the escape from a repaired tree. Before this run
closed, the integration branch moved again — and the same family produced the same
unattended red. This section is the measurement; the repair it forces (§7.4) is on
this branch, and the system-layer defect (§7.3) is unchanged by it.

### 7.1 The revision, and the run

| revision | what it is | `gopi tool doccheck` |
|---|---|---|
| `0c4679a74` | this run's starting HEAD = the wire tip as first measured | **exit 0** (both reported drifts PERMITTED) |
| `595942887` | the wire tip 47 minutes later (`git ls-remote origin`) | **exit 1** — one NEW drift NOT PERMITTED |
| `790387280` | this branch after merging `595942887` (the repair's base) | **exit 1**, identical line (before the table regeneration) |

`git merge-base --is-ancestor 0c4679a74 595942887` → true: the second tip descends
from the first, so the red appeared strictly **later**, from landings merged in
between (`git log --oneline 0c4679a74..595942887`). At `595942887` the verb printed,
verbatim:

```
doccheck:   NOT PERMITTED (drifts, absent from the baseline): devdoc/smokegate-scratchroot-pin-unmerged.md#1833: anchor internal/config/config.go:413 at // whitelist): a client node with task_sync=false still hands its bran — number-drifted (recorded text is at :429)
```

The drift is real and is arithmetic, not semantic: the recorded text
(`// whitelist): a client node with task_sync=false still hands its bran`) is at
`internal/config/config.go:413` in the doc's manifest and at `:429` in the tree —
`5a25cb1fc` (`feat(voice): make the streaming debug sink opt-in`) added lines above
it. Nothing about the claim changed; only its address did.

### 7.2 Why no lane stopped it — the four arms, observed

Every arm is a statement about `merge(master, branch)`, and in the landings that
moved `config.go` the devdoc anchor was on the **target's** side:

- ① the docclass leg of `RebuildAfterMergeMergeset` fires on the union at merge
  time; the landings touch `internal/config/config.go` (an anchored source) but the
  union they saw was **green** at the moment they merged — the drifted anchor is in
  a *different* document, `devdoc/smokegate-scratchroot-pin-unmerged.md`, and a
  union is green if its incoherent rows are all frozen;
- ② the leg is report-only (`mergeset_docclass.go`, `res.OK` untouched) — it
  reports, it never refuses;
- ③ `finishCleanupBranchAnchorCheck` asks about the branch's **own** changed paths,
  so a landing that did not itself re-emit that document abstains;
- ④ the four refusing merge-time gates measure `merge-tree --write-tree master
  branch` and abstain when the red is not attributable to the incoming branch.

The result is the prediction §2 makes, realized: master went red by itself, and
nothing on the merge/open path refused or even reported it. The only detector was
the verb, run by hand — by this run.

### 7.3 The system-layer defect, restated with this observation

The escape is not "a rare window": it has now been observed **twice in one
session**, at two different documents, from two different landings, with no
operator action in between. Rows:

- `01a135da-02fa-726b-963f-76187d31001d` — the escape as measured from a repaired
  tree (sections 1-5 above).
- `01a135de-efb6-7911-9aa3-248c32c970a7` — **this section's** unattended
  reproduction, with the raw commands in
  `artifacts/evidence-escape-reproduced-unattended.md`.

Both carry the mechanism and the host requirement (peerbox, Rule 28).

### 7.4 The repair this branch carries — the freeze table, regenerated

Rule 38 / Rule 36: the table and its header figures are DATA, and they are
**regenerated, never hand-edited**. The one command, run in this worktree at
`790387280`:

```
$ go run internal/docclass/testdata/anchor-number-census.go . \
      internal/docclass/testdata/anchor-number-baseline.tsv
anchor-number-census: 251 anchor row(s) scanned, 167 incoherent (15 first-frozen permit(s), 152 pre-existing claim(s)); the write's own delta: 1 permit(s) added, 0 permit(s) removed → wrote internal/docclass/testdata/anchor-number-baseline.tsv
```

The write's own delta names exactly the defect's shape: **one permit added, none
removed**. The whole textual delta against the shipped table is two lines — the
header figure line and one new row:

```
32c32
< # figures: frozen=166 pre-existing=152 first-frozen=14
---
> # figures: frozen=167 pre-existing=152 first-frozen=15
151a152
> devdoc/smokegate-scratchroot-pin-unmerged.md	number-drifted	internal/config/config.go:413	// whitelist): a client node with task_sync=false still hands its bran	first-frozen at 0b3c1de29; the recorded text is present in its file, but not on the recorded line
```

The two rows the original report named are still present, now at rows **97** and
**147** (`devdoc/hp-push-topology-audit.md → cmd/agent/cli_tasks_worker.go:638`,
`devdoc/scratch-gc-flight-join.md → cmd/agent/cli_worker_control.go:2167`), each
`first-frozen`. A regeneration that had dropped them would have been refused by
`TestAnchorNumberShippedHeaderFiguresMatchTheProbe` and by the `--check` mode,
which compares by permit KEY and reports stale rows as errors.

After the regeneration, in this worktree:

```
$ go run ./cmd/agent/ tool doccheck                                  → exit 0
doccheck:   167 of 167 are frozen in internal/docclass/testdata/anchor-number-baseline.tsv (each permit states whether its drift predates the freeze or was first frozen here; the gate fails only on drift NOT listed there)
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
$ go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv --check
anchor-number-census: scanned 251 anchor row(s), 167 incoherent; table carries 167 row(s)
anchor-number-census: header figures re-derived and equal (frozen=167 pre-existing=152 first-frozen=15)
anchor-number-census: table matches the tree — the freeze is exact        → exit 0
```

**This is a repair of a past red, not a closing of the escape.** The next landing
that moves a line above any of these 167 sites returns the same red, and no lane
on the merge path will refuse it; only repeating this command fixes the symptom.
That is why §7.3's row is the deliverable that matters, and why the table write is
recorded here as what it is.
