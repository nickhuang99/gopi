<!-- doc-class: historical -->
# Three defects on one row, each closed by measurement: a premise nobody stamped, a landing that files no row for its own fresh red, and a dash-less session path nothing owns

Row `01a1426f-5541-7a1a-bcc8-92808000ef01` · host **hosta** (client) · branch
`task/01a1426f55417a1abcc892808000ef01` · tip `c56e170a9` (== cut point; this run's diff
touches **no** `.go` file) · worktree
`/home/user/workspace/gopi-01a1426f55417a1abcc892808000ef01` · evidence
`~/.gopi/sessions/01a1426f-5541-7a1a-bcc8-92808000ef01/artifacts/`

The row carries three defects. None of them is repaired here, and that is a deliberate
outcome, not a shortfall: **all three live in the merge / close-out machinery, which is
peerbox's layer (Rule 28)**. What this run owes — and delivers — is the stamp: a premise nobody
had ever measured, a coverage question that was open, and a writer/reaper that had never been
named. Measurements and inferences are kept apart (Rule 41 ③); every inference below is
labelled `⇒ INFERENCE`.

## Defect A — a dispatch premise that was never stamped, and the two arms that are red instead

### A.1 The premise, measured at this run's own tip

The row was dispatched on a sentence asserting that the wire tip is red on the **STALE PERMIT**
arm. Measured, at the surface the sentence is about, on this host, this run (Rule 31 — the
surface run for real, not read):

| # | command (verbatim) | exit | what it printed |
|---|---|---|---|
| 1 | `CGO_ENABLED=0 go build -o /dev/shm/gopi-dc ./cmd/agent/` | **0** | — |
| 2 | `/dev/shm/gopi-dc tool doccheck` | **1** | `verdict: drift` — `stored-cell drift: 9 of 2580 rendered row(s) whose stored text is not what the classifier renders today` |
| 3 | `go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv --check` | **0** | `the freeze table is EMPTY and this scan accounts for it: 276 anchor row(s) scanned, 0 incoherent, 0 of them still record a line number` … `table matches the tree — the freeze is exact` |
| 4 | `grep -c "STALE PERMIT"` on #2's output | — | **0** |

So the arm the premise names is **green**, and the reason is structural, not accidental: the
freeze table is now **EMPTY**. `doccheck` reds — it genuinely does — but on a *different* arm
(`stored-cell drift`), which the premise sentence never mentions.

⇒ INFERENCE: the arm the row names cannot be red at any tip from which the table is empty,
because a stale permit requires a permit to exist. The sentence is not a stale measurement of
the same thing; it is a measurement of a thing that has since been retired.

### A.2 A second, older arm is ALSO red, and it is attributable

`doccheck`'s stored-cell-drift count moved **15 → 9 of 2580** between the filing (2026-10-16
09:48) and this run, so the drift is being worked down; that movement is the drift repair
family's business, not this record's. **Re-taken at this branch's tip `438901436`** (a reading
is anchored: this run's own devdoc landed between the two reads, so the denominator moved and
the reading had to be retaken, not carried) → **`9 of 2581`**: the drifted-row count is the
same **9**, the denominator grew by exactly the one doc this branch added. What this record
adds, because it is measured and was not named in the row:

| command (verbatim) | exit | result |
|---|---|---|
| `go test -count=1 -run TestDocClassBannerMatchesManifest ./internal/standards/` | **1** | `devdoc/removal-audit-shell-census.md: carries the "historical" banner but devdoc/DOC-INDEX.tsv classes it "normative" — only [historical] classes are bannered` … `1 banner defects` |

**It is attributable, not free-floating** (measured): the file landed in `8ddf9cf28`
(2026-10-15 01:00:13 +0800), whose manifest row `devdoc/removal-audit-shell-census.md` carries
class **`normative`**, while the file's own first line is
`<!-- doc-class: historical -->`. The tree and the manifest disagree about ONE document's
class, and both sides are committed.

**Correction of record (Rule 41 ③).** An earlier measurement pass in this run recorded
doccheck's text-ambiguous count as `40`; this run measures **41 of 276**. The value is a live
render, not a stored figure, and the corpus grew between the two reads. The `40` is withdrawn;
`41` is what was counted here, and neither number is a claim about the other revision.

## Defect B — "does the landing path have end-to-end coverage, or only a unit predicate?"

The open question, verbatim from the row. Both halves are measurable, and the answer is
**both, and the join between them is missing**.

### B.1 The per-revision replay, each cell a real command (Rule 31)

Each revision in its own detached checkout (`git worktree add --detach /dev/shm/dp-<rev> <rev>`;
`/dev/shm` is off-`/`, Rule 22), running the question **the leg itself asks**
(evidence: `artifacts/a2-leg-replay.txt`):

| revision | role | `go run …/anchor-number-census.go . <table> --check` | `go test -count=1 -run TestDocClassBannerMatchesManifest ./internal/standards/` | `grep -c ManifestGatePackageRel internal/tools/mergeset_docclass.go` |
|---|---|---|---|---|
| `476e47ede` | the integration branch (merge's 1st parent), *today's content* | **exit 0** — `scanned 251, 168 incoherent; table carries 168` … `the freeze is exact` | **exit 1** — 1 mislabelled (`ledger-readonly-faces.md`) | **0** |
| `77be22766` | the landed branch (merge's 2nd parent) | **exit 0** — `scanned 252, 167 incoherent; table carries 167` … `the freeze is exact` | **exit 1** — `class "historical" but the file does not open with its banner` (0 mislabelled) | **0** |
| `bbf8d97` | their merge | **exit 1** — `STALE PERMIT (in the table, no longer drifts): devdoc/merge-backlog-terminal-guard.md internal/tools/tasks.go }` — `the table is not the tree` | **exit 1** — 1 mislabelled (`ledger-readonly-faces.md`) | **0** |

**Two corrections to the row's own account, both measured, and each one *weakens* it rather
than strengthening it:**

1. **The parents were not both green on the question the row names.** The row says "两个各自
   绿的父提交". Measured at today's content of those two revisions, `TestDocClassBannerMatchesManifest`
   is **exit 1 on both parents** — the mislabelled `devdoc/ledger-readonly-faces.md`. The
   parents were green on the *census arm* (B.1 column 3, both exit 0) and red on the *banner
   arm*. So the merge composed **one** fresh red, not two, and the banner arm it inherits is
   **pre-existing on both sides** and therefore not the landing's.
2. **`77be22766`'s red is a DIFFERENT arm of the same test** — it fails on *"the file does not
   open with its banner"* with `0 mislabelled`, i.e. a half-state this run did not chase. It is
   recorded rather than explained, because the mechanism behind it was not measured here.

What survives the correction, and is the reason the row was worth filling:

- **`bbf8d97` is red on an arm both parents are green on** — the census: parents exit 0, merge
  exit 1, `STALE PERMIT`. That is a red only the *composition* carries.
- **No revision-scoped lane holds it**, because there is no revision to hold: the union tree of
  the two parents **is** the merge's tree, so the defect exists in no parent.

### B.2 The coverage verdict — with test names and call chains

Three facts, each measured, each separately decisive.

**(a) The union DOOR is closed — and this is not a read, it is the code.**
`internal/tools/mergeset_docclass.go:345-357` appends the manifest's own gate package
**unconditionally**:

```go
refs := []string{ref}
manifestRef := "." + mergesetGatePackageSep + docclass.ManifestGatePackageRel
if manifestRef != ref {
    refs = append(refs, manifestRef)
}
```

`docclass.ManifestGatePackageRel` is `./internal/standards`, and the `os.Stat` that used to
abstain on its absence was removed deliberately — its own comment states the reasoning ("A tree
without the package is not a fixture question … not told by a stat call that the gate was not
obliged"). `grep -c ManifestGatePackageRel` returns **0** in all three replayed revisions
(B.1, last column) and **4** at the tip: the door was closed by `10217dc02`
(2026-10-14, *"the devdoc-anchor leg asks for the manifest's own gate package too"*), which is
an ancestor of master.

**(b) The real landing path FILING exists, unguarded by tests.** `RebuildAfterMergeMergeset`
(`internal/tools/rebuild_vet_mergeset.go:283`) → `mergesetGateMeasure` (`:376`) →
`mergesetGateReport` (`:787`) + `mergesetGateFileBugRow` (`:815`) → `TaskSubmit`. So a red
landing does become a row.

**(c) The JOIN between (a) and (b) has no test at any level.** Counted per file, not eyeballed
(evidence: `artifacts/a3-coverage.txt`):

| file | calls `RebuildAfterMergeMergeset` | calls `mergesetDocclassRuns` |
|---|---|---|
| `internal/tools/mergeset_docclass_test.go` | **0** | 4 |
| `internal/tools/mergeset_gendoc_test.go` | 1 | **0** |
| `internal/tools/mergeset_gendoc_base_test.go` | 2 | **0** |
| `internal/tools/rebuild_vet_mergeset_test.go` | 11 | **0** |
| `internal/tools/mergeset_partial_preexisting_test.go` | **0** | 1 |

No test calls `mergesetGateFileBugRow`, `mergesetGateMeasure` or `mergesetGateAttribute`
directly. **The docclass leg's file never drives the real step; the step's files never touch
the docclass leg.** The strongest existing test of the leg —
`TestMergesetDocclassAsksTheManifestGatePackageForTheCleanMergeHalfState`
(`mergeset_docclass_test.go:410`) — builds a real clean merge and a real checkout, then asserts
on the **run set** (`mdAssertRunSet`, `:190`: `run.Packages` / `run.Argv`), never on a filed
row. The leg's own test file carries **no** `heavytest.Require`, so a missing real-tree hardlink
is not the reason it cannot be composed — the composition simply was never written.

**(d) The end-to-end half that DOES exist is skipped by the default gate.** Running the merged
set as the ordinary gate runs it (`go test -count=1 -run 'Mergeset' ./internal/tools/ -v`,
heavy **off**, Rule 27): **26 pass / 10 skip**, and the skips are precisely the end-to-end arms —

```
--- SKIP: TestMergesetGate_FilesABugRowOnRed
--- SKIP: TestMergesetGate_RedMergeResultIsReported
--- SKIP: TestMergesetGate_RegressionIsAttributedToTheLanding
--- SKIP: TestMergesetGate_TouchedGatePackagesDriveTheGatesLegWithTheRealToolchain
--- SKIP: TestMergesetUnionGateDrivesTheMergedResultStep
```

The one test that would prove **a red landing files a row** is behind `heavytest.Require`.

### B.3 The verdict, in one sentence

**The unit predicate exists and runs (`mergesetDocclassRuns`' four arms, all PASS under the
default gate); the end-to-end composition through the real landing path exists nowhere at any
level; and the nearest shape that does exist — the step plus its filing arm,
`TestMergesetGate_FilesABugRowOnRed` — is skipped by the default gate.** A landing is
consequently free to compose a red in a package no leg asks for, and nothing in the default
suite would fail on that gap.

⇒ INFERENCE: this yields a **dispatchable, non-system-layer** piece for peerbox — *one test that
drives the real landing path against a fixture whose union names the devdoc family and asserts
the filed row*. It is named as a roadmap, not done here: `internal/tools` is peerbox's layer.

## Defect C — a dash-less session path: the guard exists, the writer does not use it, and nothing reaps it

The row measured the symptom (a dash-less spelling accepted writes for a while, then stopped
resolving; `find` reports only the hyphenated directory) and stated honestly that the writer
and the reaper were **not** found. Both are named below, and the answer is not what either
would predict.

### C.1 The spelling the row observed

`gopi task show` on the exact operand from the row:

```
$ gopi task show '01a136ee-88177a618f3d942bdbfa31d4'
gopi task show error: task: no task with task_id="01a136ee-88177a618f3d942bdbfa31d4" — it is a
uuid's 16 bytes in a non-canonical spelling (dashes dropped or moved); did you mean
"01a136ee-8817-7a61-8f3d-942bdbfa31d4"?
```

The operand is the uuid's **first dash group kept** and the other three dropped
(`01a136ee` + `88177a618f3d942bdbfa31d4`), not `taskGroundIDStem`'s fully dash-less form. The
ledger refuses that shape and names the canonical id — so it can never resolve to a row.

### C.2 The guard: it exists, at both session-dir seams, and it was measured

`internal/sessionid` (first commit **`bcf8bb03e`, 2026-10-12**, *"gate the task-id shape at both
session-dir seams"*) provides `IsAdmissible` / `Refuse`; the shape gate was added to the two
seams on 2026-10-12 (`503624e84`, `bcf8bb03e`):

- `internal/tools/task_session.go:157` — `if taskID != "" && !sessionid.IsAdmissible(taskID)`
- `internal/agent/session.go:110` — same predicate, "from one definition"
- `internal/signalrec/signalrec.go:731` — the same rule for the provenance destination

`TaskReportPath` (`internal/tools/task_report.go:90`) resolves *through* `taskSessionDir`, so it
inherits the gate — i.e. **no gopi code path can compose a dash-less report path** (measured:
`grep -rn 'PathEnv+"="' cmd/ internal/` yields exactly one writer,
`cmd/agent/cli_tasks.go:3059`, fed by `tools.TaskReportPath`).

### C.3 The un-guarded writer, and it is measured, not inferred

The guard is bypassable by construction: **a `bash` tool call writes to a path as a string, and
a string is not a task id.** Nobody passes it to `TaskSessionDir`, so nothing refuses it.

The proof is a container born **40 hours after the guard landed**:

```
$ find ~/.gopi/sessions/01a1361ba9ee7f198271a702059e958b -printf '%T+ %p\n' | sort
2026-10-14+06:17:50  …/01a1361ba9ee7f198271a702059e958b
2026-10-14+06:17:57  …/01a1361ba9ee7f198271a702059e958b/evidence/rule41-predicate-rows-PREFIX.txt
2026-10-14+06:18:38  …/01a1361ba9ee7f198271a702059e958b/evidence/rule41-predicate-rows.txt

$ gopi task show 01a1361b-a9ee-7f19-8271-a702059e958b | head -3
task_id: 01a1361b-a9ee-7f19-8271-a702059e958b
status:  failed
summary: Rule41 defect: gopi bash guard does not see the shell's OWN cwd — chdir persistence hole
```

**The writer is an agent run itself**, via the `bash` tool, under the row's dash-less
`taskGroundIDStem` spelling — a spelling that *is* legitimate as a branch/worktree name and
therefore sits in every run's environment. The evidence file is named `…-PREFIX.txt`, i.e. the
run was reasoning about prefixes while writing under one. **Not a tool**, not a script, not a
harness: the shape gate guards the doors gopi owns, and this is not one of them.

### C.4 The reaper: there is none, and that is the whole finding

Population, measured now (evidence: `artifacts/a4-defect-c.txt`):

```
$ ls -1 ~/.gopi/sessions | grep -cE '^[0-9a-f]{32}$'                              → 23
$ ls -1 ~/.gopi/sessions | grep -cE '^[0-9a-f]{8}-…-…-…-…$'                       → 2005
$ … dash-less containers WITH a canonical twin (same 16 bytes)                    → 23 of 23
$ stat -c '%w' on each   →  born 2026-10-10 00:24:35 … 2026-10-14 06:17:50
```

Every one has a canonical twin: each is a **second container beside a real row's own**, the
exact defect `internal/sessionid`'s header describes. And their **birth** times end at
2026-10-14 06:17 — nothing newer, which is the guard working on the gopi-owned doors.

The reaper, run for real:

```
$ /dev/shm/gopi-dc tool scratch-gc trees --dry-run | grep -E 'tree=session'
scratch-gc role=cli-trees … tree=session-legacy present=true entries=18 … dry-run=true
scratch-gc … tree=session-legacy/files present=true entries=0 …
scratch-gc … tree=session-legacy/containers present=true entries=0 …
```

**23 dash-less containers; `tree=session-legacy` reports `entries=18`** — and that row's
predicate is `IsRootTaskContainerName ∨ IsStrayRootDirName`, i.e. containers that are *not* of
the id-named family. Per `internal/sessionlayout/families.go:680-691`, "an id-named container
is the container family's, never this one's". A dash-less 32-hex directory is classified as an
**id-named container** (that is why the census is 23 and not 0) while not being a name any
*admissible* id produces.

### C.5 The namespace, measured per container — the inference above, replaced by a count

The paragraph that stood here was an inference ("no declared tree claims them"). It is now a
**measurement**, and the measurement is sharper than the inference was. The two predicates the
reaper's directory row is built from are the registry's own exported functions
(`sessionLegacyRootDirName`, `declared_session_legacy.go:146-160`, "the union of the registry's
two `bound:` directory families at that depth"). Evaluated on all 23 containers
(`IsRootTaskContainerName` ∨ `IsStrayRootDirName`, heads measured through the package's own
exported predicates):

```
NOMATCH  01a12161de4075a487774f0042b63d6a root=false stray=false   … (23 rows, all NOMATCH)
MATCHES  — none
```

**23 of 23 are `NOMATCH`.** And the other half of the same question, measured on the same set:
`IsSessionContainerName(name)` returns **true for all 23** (`families.go:701`). So each
container is simultaneously *an id-named container* — which is why no `t-`/stray rule ever
looks at it — and *a name no admissible id produces*, which is the near-miss the classifier's
own comment describes.

This also settles the census honestly: the reaper's `entries=18` is **not** this population.
Measured in the same tree: `t-`-prefixed root dirs = **2** (`t-acc-live`, `t-acc-unowned`),
`process-`-prefixed = **13**, i.e. 15 of the union's task-named arm; the remaining 3 belong to
the stray arm, which was not enumerated name-by-name here. None of the 18 is a dash-less
container, and the shape assertion that first looked suspicious is therefore exact, not a
tolerating default.

Two further facts make "permanent" the right word rather than "slow":

- **The window is not what holds them back.** The row's `StaleAge` is `4320h` (**180 days**),
  measured in the reaper's own line (`stale=4320h0m0s`) — the age half is therefore satisfied
  by all 23 today, and the count stayed exactly 18 at `--scratch-gc-stale-min 1`. An age
  argument cannot explain their survival; only the namespace verdict does.
- **The veto the source describes is present but is not the binding constraint here.** All 23
  carry **no** `metadata.json` (measured: `dashless WITH metadata.json=0 WITHOUT=23`) while all
  23 canonical twins **do** (`twins with metadata.json=23 of 23`) — the `unheldRegistered`
  veto's input, in exactly the shape `families.go` describes. It is moot, because the namespace
  filter never reaches them.

⇒ CONCLUSION (measured, not inferred): **no declared tree claims a dash-less container.** The
rows over the sessions tree are `session-raw` (namespace: `*.raw.log`), `session-legacy`
(`t-`/`process-`/stray root dirs), `session-legacy/files`, `session-legacy/containers` and
`session-attachments`; a dash-less container is outside all five, and `RemoveAll` appears nowhere
in the sessions-container path (`grep -rn RemoveAll internal/ cmd/` — the only hit touching a
session tree is `task_session.go:191`, the legacy→S1 migration). **Nothing reaps them; they are
permanent residue, and 1.93 MB of agent-written evidence sits inside them.**

⇒ INFERENCE (labelled, and deliberately narrow): the row's own observation — a dash-less path
that accepted writes and was later gone — is *consistent* with this, but the mechanism by which
its files arrived is **not measured here** and is not claimed. What is measured is that the
container population as it stands today is unclaimed and unreaped.

### C.6 Why the spelling reaches an agent run at all — measured on THIS run

The guard's own test passes (`go test ./internal/sessionid/` → `ok`, both arms PASS), and the
Defect C row's path is canonical **today**. The mechanism that still exposes the spelling is in
every run's environment, and this run demonstrates it:

```
$ basename "$PWD"
gopi-01a1426f55417a1abcc892808000ef01          ← the dash-less spelling, in the CWD itself

$ ls -d ~/.gopi/sessions/01a1426f55417a1abcc892808000ef01
ls: cannot access '…': No such file or directory   ← the guard kept the container canonical
$ ls -d ~/.gopi/sessions/01a1426f-5541-7a1a-bcc8-92808000ef01
…/01a1426f-5541-7a1a-bcc8-92808000ef01            ← the row's real container
```

So the two halves are now fully measured, and they are **not in conflict**: the dash-less
spelling is *legitimate* as a worktree/branch name (it appears in the CWD, in `git worktree
list`, and in the branch), and it is *refused* as a session directory. An agent that reaches
for `$PWD`-derived or `$TASK_ID`-derived evidence paths composes the refused spelling from
its own environment, and the `bash` tool — which takes a path, not a task id — has no gate on
it. That is precisely the 23-container population, and it is why the guard's existence and the
population's existence are both true at once.

## Not done, and why — the honest boundary

- **Defect A: not repaired.** The named arm is green and the census arm is green; the reds are
  the drift arm (another family's, moving — 15→9) and the banner arm (attribute to
  `8ddf9cf28`, a content fix in `devdoc/`). Neither is a merge-machinery change, and neither
  was dispatched to this row.
- **Defect B: not repaired, and the piece is now FILED.** The closure is a test in
  `internal/tools` — peerbox's layer (Rule 28). B.3 states the piece; at the end of this run it
  was submitted as row **`01a143ff-f921-7ce8-9913-d653a9fddd6f`** (`--tags bug`, pending,
  no parent), carrying the coverage counts inline so a peerbox executor needs no cross-host read.
- **Defect C: not repaired.** The writer is a `bash` string; the fix would be a new namespace
  rule in `internal/scratch` plus a deletion policy — both system layer (Rule 28), and the
  deletion policy is exactly the kind of thing Rule 22/35 says not to invent casually.
  The namespace half is filed as row **`01a14400-1e4b-7086-b97d-257497a832ed`** (`--tags bug`,
  pending, `parent_task_id=independent` — no edge to the row above).
  **No cleanup was performed**: this run removed nothing, and the 23 containers stand.
- **Both rows queue on hosta, not peerbox** — `task_sync` is `false` in this host's
  settings.json, so an auto-submitted row does not travel upward (measured: `task send`/
  `--push-only` is the only writer that puts a row in a peer's ledger, and it was **not**
  run). Whether they should be visible on peerbox is the operator's configuration decision.
- **Why the premise was never stamped is not answered here.** The row's own executor would have
  to be asked; the sibling decision `devdoc/submit-time-premise-window-decision.md` (row
  `01a135ff`) already ruled the submit seam has no mechanism to place, and this record does not
  re-open that ruling. What this record adds is the observation that it **cost this run** the
  same triage it cost that one.
- **`476e47ede`/`77be22766` were read at their content, not at their contemporaneous tips.**
  They are ordinary merges whose parents may long since have moved; the table answers "what
  does that tree's content say", which is the question the leg would have asked.

## Evidence index

`~/.gopi/sessions/01a1426f-5541-7a1a-bcc8-92808000ef01/artifacts/`

| file | what it holds |
|---|---|
| `a1-premise-stamp.txt` | §A — doccheck + census + bare `TestDocClassBannerMatchesManifest`, with exit codes |
| `a1-doccheck-raw.txt` | `gopi tool doccheck`'s full output at this tip |
| `a1-census-raw.txt` | the census generator's `--check` output at this tip |
| `a2-leg-replay.txt` | §B.1 — the three-revision replay, each with its verbatim command and exit code |
| `a3-coverage.txt` | §B.2(c)(d) — the per-file counts and the default-gate run (26 pass / 10 skip) |
| `a4-defect-c.txt` | §C — the population, the births, the guard commits, the un-guarded writer, the reaper's own plan |
