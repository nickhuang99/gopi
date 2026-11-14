<!-- doc-class: historical -->
# The published-anchor read floor refused every tree without a corpus — 24 finish-cleanup tests, one root cause

**Task** `01a12e01-8048-7f35-88f1-67d9a82c3825` · **Verdict** DOABLE · reproduced, root-caused, fixed
**Branch** `task/01a12e0180487f3588f167d9a82c3825` · **Worktree** `/home/user/workspace/gopi-01a12e0180487f3588f167d9a82c3825`

## Completed

1. **Reproduced on the CURRENT base**, not on the reported one. Task `01a12db7` observed
   8 failures at `e07d222dd`; re-running the same command at this worktree's HEAD
   (`1d6a50184`, whose merge-base with master is `d7fb65344`) produces **24 failures**,
   not 8 — the three symptom families named in the briefing are all present, and 21 more
   failures share their mechanism.
2. **Root cause found by measurement, and it is ONE cause, not three.** The briefing's
   hypothesis ("one shared root cause") is *correct on this point* — established here by
   A/B, not by reading: with `GOPI_TASK_PUBLISHED_ANCHOR_GATE=0` the same 24 tests all
   pass (`ok … 39.4s`).
3. **The mechanism, in one sentence.** `gendoc.Impact` (`internal/gendoc/edge.go`) fired
   its zero-edge read floor on **every repository that has no generated-document corpus**
   — which is every fixture repo the suite builds — and the delivery path
   (`internal/tools/task_finish_gendoc.go:209`) read that finding as a BROKEN EDGE and
   REFUSED the close-out, so the worktree was never released.
4. **Fixed** in `internal/gendoc/edge.go`: the floor is now read against the tree
   (`CorpusPresent`), so it fires where an edge was **lost** and stays silent where the
   corpus was **never there**. Regression test added: `TestGeneratedDocImpactSparesATreeWithNoCorpus`.

## Files

- `internal/gendoc/edge.go` — **changed**: `CorpusPresent(root)` added (`:111`, `:137`);
  the zero-edge floor now reads the tree (`:436-443`).
- `internal/gendoc/edge_test.go` — **changed**: `TestGeneratedDocImpactSparesATreeWithNoCorpus`
  (`:207`), plus the touched-expectation updates around it.
- `devdoc/gendoc-read-floor-corpus-less-tree.md` — this report; `devdoc/DOC-INDEX.tsv` row at `:804`.
- **Not changed, deliberately**: `internal/tools/task_finish_gendoc.go` (the consumer still
  refuses on any finding — the producer's answer is what was wrong) and
  `internal/tools/task_finish_cleanup.go` (the release rung was never the defect).
- Raw evidence, outside the worktree (Rule 30/37):
  `/home/user/.gopi/sessions/01a12e01-8048-7f35-88f1-67d9a82c3825/artifacts/` —
  `repro-head-gate-on.txt` (RED, 22157 B), `repro-after-fix.txt`, `repro-final-head.txt`,
  `repro-final-head-748ee9530.txt`, `probe-gendoc.txt`,
  `standards-preexisting-and-smoke.txt`, `sibling-row-01a12e0d-reachability.txt`,
  `bug-brief-standards-red.md`.

## The three symptom families are ONE cause

| Family | Symptom (verbatim) | Mechanism | Code |
|---|---|---|---|
| (a) | `the published-anchor edge is BROKEN — no generated-document edge was discovered at all` (21 lines of the 24 red runs) | the read floor fires on a corpus-less tree | the finding: `internal/gendoc/edge.go:443` (pre-fix `:368`, floor gate at `:422`, pre-fix `:367`). The marker the test sees: `internal/tools/task_finish_gendoc.go:102` (`gendocGateBrokenEdgeMarker`); the REFUSAL that consumes it: `internal/tools/task_finish_gendoc.go:209-211` (`if len(findings) > 0 { return finishCleanupGeneratedDocRefusal(…) }`, comment at `:206-208`) |
| (b) | `server-arm recycle must release worktree … (stat err = <nil>)` (1 line) | the refusal above returns **before** the release rung | `internal/tools/task_finish_cleanup.go:1748` (`gates := finishCleanupGeneratedDocGateAligned(…)`) → `:1750-1755` (`if !gates.OK() { rep.Skipped = true; …; return rep, nil }`) — the `return` is ahead of the worktree removal. The failing assertion is `internal/tools/cov_finish_cleanup_test.go:224` |
| (c) | `untracked-only residue … (removal deliberately skips … )` (3 lines) + `delivery-point sync refused — the worktree was NOT released` (21 lines) | (c)'s first half is the *pre-existing* residue rung (a real, separate fact); its second half is family (a) again, recorded on the same report line | the residue note constant: `internal/tools/task_finish_cleanup.go:789`, recorded as a step at `:773-775`, classified untracked-only at `:754-780` (`finishCleanupWorktreeDirtyReason`) and salvaged/refused at `:1778-1795`. The second half is the marker built at `internal/tools/task_finish_deliverysync.go:85` (`finishCleanupLocalSyncBlockedMarker`), aliased to `finishCleanupAlignBlockedMarker` at `:143` and used at `task_finish_cleanup.go:1752` |

**The counts above are the red file's own lines**, not an impression —
`grep -c` over `artifacts/repro-head-gate-on.txt`: `^--- FAIL` → **24**,
`published-anchor edge is BROKEN` → **21**, `server-arm recycle must release worktree` → **1**,
`worktree was NOT released` → **21**, `removal deliberately skips` → **3**; the 24
`--- FAIL` lines are 24 DISTINCT tests (`sort -u | wc -l` → 24).

**The briefing's three families do not cover the file — measured, block by block.** Splitting
the red output at its `--- FAIL` headers and asking each block which sentences it carries
gives four shapes, not three:

| sentences in the block | tests |
|---|---|
| BROKEN-EDGE + `the worktree was NOT released` | 18 |
| BROKEN-EDGE + `removal deliberately skips` + `the worktree was NOT released` | 3 |
| `server-arm recycle must release worktree` only | 1 |
| **none of the three families at all** | **2** |

The last row is the one the briefing cannot see, and it is named here rather than folded in:
`TestFinishCleanupGoneWorktreeIsReportedAsSettled` (`task_finish_cleanup_test.go:2153`,
"fixture: the first run must remove …/001/wt") and
`TestFinishCleanupInProcessTestCannotPushOutsideTestSpace` (`task_finish_cleanup_test.go:2552`,
`git rev-parse refs/heads/… → exit status 128: unknown revision`). Both are **consequences, not a
fourth cause** — the gate-off A/B run is green for the whole package, i.e. all 24 pass when the
finding is not produced — and the mechanism is the same `return` at
`task_finish_cleanup.go:1750-1755`: the chain stops before the removal, so a fixture that asserted
"the first run must remove …" fails, and the branch the next step expects was never pushed. Their
absence from the three named families is exactly why the A/B run — not the red output's wording —
is what establishes the single cause.

**Honest split.** (b) and (c)'s *release* half are pure consequences of (a) — one cause.
(c)'s *first* half is **not** the same cause and not a defect: `untracked-only residue in
the worktree of a finished run — removal deliberately skips` is the salvage rung declining
to `git clean` a directory the test itself dirtied. It is reported, the ground is kept, and
that is the designed behaviour. **Measured, not argued:** with the gate switched off,
`TestFinishCleanupChainDirtyWorktree` — the test that carries this exact reason — PASSES,
and its assertion is that the chain reached the `push` step. So the residue rung does not by
itself refuse the release; the salvage path clears it and the chain continues. The residue
sentence appearing *beside* the BROKEN-EDGE sentence on the red lines is two facts on one
report, not two causes.

## The decisive evidence

**Probe — the predicate, run outside any test** (`artifacts/probe-gendoc.txt`):

```
root=/dev/shm/gendoc-probe-1791774404/repo          ← a fixture repo: README.md + one commit
  Deps -> n=0 err=<nil>
  GeneratedDocGates -> required=[]
  findings=[no generated-document edge was discovered at all — the scan either did not read
            the tree or every document lost its generator instruction]
  err=<nil>
root=/home/user/workspace/gopi-01a12e0180487f3588f167d9a82c3825   ← the real tree
  Deps -> n=1 err=<nil>
    doc=COMMANDS.md gen=internal/standards/testdata/commands-census.go anchors=54 gates=[internal/gendoc internal/standards]
  findings=[]
```

A tree with no corpus produced a **finding**; a real tree produced none. The suite's
fixtures (`covLifeGitRepo`, `preMergeGit` worlds) are all the first shape.

**A/B — the same command, one env var apart** (Rule 41: measured, not inferred):

```
$ CGO_ENABLED=0 GOPI_TASK_PUBLISHED_ANCHOR_GATE=0 go test -count=1 -run 'TestCovFinishCleanup.*|TestFinishCleanup.*' ./internal/tools/
ok  	github.com/nick/gopi/internal/tools	39.411s          ← 24/24 green
```

## Not Done (honest)

- **Family (c)'s residue rung was not changed**, because measurement shows it never needed
  changing: with the gate switched off, `TestFinishCleanupChainDirtyWorktree` and
  `TestFinishCleanupDirtyFileNamedNoOutput` PASS, and their own assertions are that the
  chain reached the `push` step. The residue sentence is a step detail on a report that
  then continued — the salvage path cleared it. (An earlier draft of this report asserted
  the opposite from reading the red lines; the gate-off run refutes it, and the measurement
  wins.)
- **The briefing's figure of 8 failures is stale.** This base shows 24. I did not chase the
  difference between the two bases: the mechanism is identical and the command is the same,
  so the count moves with which fixture files exist at each revision. Recorded as a fact, not
  explained.
- The fix is in `internal/gendoc` (a leaf package) and does **not** touch the delivery path's
  decision logic — the consumer still refuses on any finding; only the producer's answer about
  a corpus-less tree changed.

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → exit 0 (`BUILD OK`)
- **Tests**:
  - `CGO_ENABLED=0 go test -count=1 -run 'TestGeneratedDoc' ./internal/gendoc/` → `ok github.com/nick/gopi/internal/gendoc 1.100s`
  - **branch-scoped smoke** — `make test-budget PKGS='./internal/gendoc ./internal/tools'` →
    `packages: 2 finished, 0 with a failure`, `executed: 3274 test(s)`, `exit=0 outcome=pass`
    (889s wall; stream `run-1791774691-3155548.json`). `internal/tools` — the very package the
    epic's command targets — is **green** on this branch.
  - A wider branch-scoped run adding `./internal/standards` (`/…/run-1791774593-3150047.json`)
    exits 1, and **none of its five failures is introduced by this branch** — measured, not
    argued (`artifacts/standards-preexisting-and-smoke.txt`): this branch's diff touches **zero**
    files under `internal/standards` or `internal/tools`; the Rule 17 sites are **byte-identical**
    at the base (`git grep os.Unsetenv HEAD~1 -- …taskid_whitespace_normalize_residual_test.go` →
    `:546 :547 :653 :654`, while `internal/standards/baseline.go` names `:543 :544 :650 :651` ⇒
    a STALE baseline plus 4 NEW, **on the base, before this branch**); and
    `internal/tools/task_finish_cleanup.go` is 262502 bytes at base, at master and at HEAD alike,
    with **no permit row in `repo-hygiene-baseline.tsv` at base or at master** ⇒ the Rule 39
    oversize finding is pre-existing too. (The same run's status line lists `TestAnchor` /
    `TestPublishedAnchorResolves` under `ran:` — that is summary noise, not a failure; the
    stream's `"Action":"fail"` records name exactly the five standards tests above.)
- **Ran** (the surface the epic names, verbatim, red then green):
  ```
  CGO_ENABLED=0 go test -count=1 -timeout 20m -run 'TestCovFinishCleanup.*|TestFinishCleanup.*' ./internal/tools/
  ```
  RED, before the fix (`artifacts/repro-head-gate-on.txt`, HEAD `1d6a50184`):
  `--- FAIL` × 24, ending `FAIL github.com/nick/gopi/internal/tools 45.536s`
  GREEN, after the fix (`artifacts/repro-after-fix.txt`):
  `ok github.com/nick/gopi/internal/tools` — **see the Result section below for the run that
  produced it** (this line is filled in from the run's own output, not predicted).
- **Result**: recorded below from the actual run.

## Evidence (self-filed)

- **Reproduction, gate ON (RED)** — `artifacts/repro-head-gate-on.txt`, 24 `--- FAIL` lines, exit non-zero:
  `FAIL github.com/nick/gopi/internal/tools 45.536s`
- **A/B control, gate OFF — all 24 pass**: `ok github.com/nick/gopi/internal/tools 39.411s`
- **Probe, predicate outside the suite** — `artifacts/probe-gendoc.txt`, quoted above in full:
  fixture tree → `Deps n=0` + the finding sentence; real tree → `Deps n=1`, `findings=[]`
- **Green after the fix** — `artifacts/repro-after-fix.txt`: `ok github.com/nick/gopi/internal/tools 42.280s`
- **The regression guard's RED proof, targeted and honest about what it is**
  (`artifacts/regression-guard-red-proof.txt`). The first attempt swapped the whole
  `edge.go` for the base revision, and it **could not run the test at all**:
  `edge_test.go:217:18: undefined: CorpusPresent` → `FAIL … [build failed]` — a
  compile failure is not a red proof, so it is recorded as a failed attempt, not as
  evidence. The second attempt reverts **only the guarded body** (the `if len(deps) == 0`
  branch) inside the fixed file, so the test compiles against the pre-fix logic:
  ```
  --- FAIL: TestGeneratedDocImpactSparesATreeWithNoCorpus (0.02s)
      edge_test.go:225: a tree with NO generated-document corpus must not report a broken edge — … : [no generated-document edge was discovered at all — the scan either did not read the tree or every document lost its generator instruction]
      edge_test.go:265: a clean tree with no corpus must answer with no finding at all: [ … same sentence … ]
  FAIL	github.com/nick/gopi/internal/gendoc	0.033s
  ```
  then `git checkout -- internal/gendoc/edge.go` restores it and the same test prints
  `ok github.com/nick/gopi/internal/gendoc 0.016s`. Red then green on the same predicate.
- **Green AT THE DELIVERED HEAD** — `artifacts/repro-final-head.txt`: HEAD `da7842a3e`,
  `ok github.com/nick/gopi/internal/tools 91.374s`, `exit=0`
- **Branch-scoped smoke (the package the epic targets), exit 0**:
  `make test-budget PKGS='./internal/gendoc ./internal/tools'` → `2 finished, 0 with a failure`,
  `3274 test(s)`, `exit=0 outcome=pass` (stream `run-1791774691-3155548.json`)
- **The five `internal/standards` failures are PRE-EXISTING on this base — measured**
  (`artifacts/standards-preexisting-and-smoke.txt`): this branch's diff touches **zero** files
  under `internal/standards`/`internal/tools`; the Rule 17 sites are byte-identical at base
  (`:546 :547 :653 :654` vs baseline rows `:543 :544 :650 :651` ⇒ stale) and
  `task_finish_cleanup.go` (262502 B, oversize) carries no permit row at base **or** at master.

## Result

**Red → green, the exact command the epic names, run twice by THIS run.**

**(1) The run the analysis left behind** — `CGO_ENABLED=0 go test -count=1 -timeout 20m -run 'TestCovFinishCleanup.*|TestFinishCleanup.*' ./internal/tools/`, at base
`1d6a50184` (`artifacts/repro-head-gate-on.txt`, gate ON):
`--- FAIL` × **24** → `FAIL github.com/nick/gopi/internal/tools 45.536s` (exit 1).

**(2) A red-then-green pair issued by this run, in ONE job, without checking out another
revision** (`artifacts/red-green-acceptance-this-run.txt`) — so the pair cannot be an artifact
of two different trees. HEAD `e7652470e`:

```
--- [1] RED: guarded body reverted to its pre-fix form (CorpusPresent not consulted)
FAIL	github.com/nick/gopi/internal/tools	156.363s
RED exit=1
--- restoring the fix (git checkout -- internal/gendoc/edge.go)   → porcelain: (empty)
--- [2] GREEN: the fix in place
ok  	github.com/nick/gopi/internal/tools	155.661s
GREEN exit=0
final porcelain: (empty)
```

**An honest limit on the RED half of that pair**: it was captured with `tail -40`, so the
artifact shows **13** `--- FAIL` lines and not the full total (§ the count in the 24-failure run
above is the whole-file figure, `grep -c '^--- FAIL'`). The pair establishes the transition
(exit 1 → exit 0 on the same command, same tree, one text-only revert between them); the
*count* is carried by run (1), not by this one.

**Independent control that isolates the cause** (the same 24 tests, the gate switched off
rather than the code changed) → `ok  github.com/nick/gopi/internal/tools 39.411s`
(`artifacts/ab-gateoff-analysis-run.txt`).


**Red → green, the exact command the epic names, run by this run:**

```
$ CGO_ENABLED=0 go test -count=1 -timeout 20m -run 'TestCovFinishCleanup.*|TestFinishCleanup.*' ./internal/tools/
```
- **before the fix** (HEAD `1d6a50184`, `artifacts/repro-head-gate-on.txt`):
  `--- FAIL` × 24 → `FAIL github.com/nick/gopi/internal/tools 45.536s` (exit 1)
- **after the fix** (`artifacts/repro-after-fix.txt`):
  `ok  github.com/nick/gopi/internal/tools 42.280s` (exit 0)
- **re-run at the delivered HEAD `da7842a3e`** (`artifacts/repro-final-head.txt`, same command,
  `2026-10-12T11:27:39+08:00 → 11:29:19+08:00`):
  `ok  github.com/nick/gopi/internal/tools 91.374s`, `exit=0`. The one commit between the two
  green runs (`da7842a3e`) touches only `devdoc/*.md` + `devdoc/DOC-INDEX.tsv`, so the Go tree
  under test is identical — stated as the reason this re-run is evidence, rather than implied.

**Independent control that isolates the cause** (the same 24 tests, the gate switched off
rather than the code changed) → `ok  github.com/nick/gopi/internal/tools 39.411s`.

`gofmt -l internal/gendoc/` prints nothing for the files this change touches
(`internal/tools/` carries pre-existing gofmt entries this change did not introduce —
`task_finish_cleanup.go`, `task_finish_premerge.go`, … are listed on the base too).


## Sibling work on the same defect — measured, so the merge order is decidable

A second row (`01a12e0d`) targets the same symptom, and its own distilled memory
(`#9450`) describes a near-identical root cause. Its **branch carries no commit**,
which is a fact, not a guess
(`artifacts/sibling-row-01a12e0d-reachability.txt`):

```
task/01a12e0d348a77a4bef080e397d0afe1  1d6a50184fad2c91204e9aa5ae84b78f72f393f2   # == the BASE
git log --oneline 1d6a50184..task/01a12e0d348a77a4bef080e397d0afe1   →  (empty)
git merge-base --is-ancestor 40efeef8b master  →  NO   # master does NOT carry the fix
git show master:internal/gendoc/edge.go | grep -c 'no generated-document edge was discovered at all'  →  1
```

So on this machine the fix that is **reachable** is this branch's `40efeef8b`, and
the defect is still live on `master`. (The sibling's memory record is the only
route to its shape; its branch is not.)

**One deliberate divergence from that record, recorded so it is not read as an
oversight.** Record `#9450` classifies a tree whose candidate carries a
`BEGIN GENERATED` / `BEGIN VERB INDEX` marker but **states no `go run …` command**
as *no corpus* (abstain). Measured in the same record: the REAL tree's `man/gopi.md`
carries `BEGIN VERB INDEX` and states no `go run …`. Under that rule the real tree
abstains — i.e. the delivered repository would skip the check entirely, and the
very shape we most want to catch (a document that kept its region but lost its
regeneration command) is the one that silences the gate. `CorpusPresent` therefore
keys on the **region marker alone**: marker present ⇒ the corpus is here ⇒ any
finding stands. The real tree keeps its finding; a tree that never had a corpus
(the suite's fixture repos) is spared. This is the conservative direction — it can
only refuse *more*, never less.

**Not Done / hand-over**: if the sibling row lands a fix of its own, the two are
alternatives for the same seam (`internal/gendoc/edge.go`) and one should be
dropped rather than merged on top of the other. This run does not decide that: it
is the merger's call, made from the two diffs.

**The acceptance command re-run at the DELIVERED HEAD** — executed at `748ee9530`;
this sentence is the only thing that separated it from the branch tip as it then stood —
`artifacts/repro-final-head-748ee9530.txt`:

```
CGO_ENABLED=0 go test -count=1 -timeout 20m -run 'TestCovFinishCleanup.*|TestFinishCleanup.*' ./internal/tools/
ok  	github.com/nick/gopi/internal/tools	68.508s
exit=0
```

**A ledger correction, from this run's own reflog** (Rule 41: an earlier revision of this
paragraph named a commit that no longer exists on the branch). The paragraph used to say
"the branch tip is `03d2dc6ec`"; that hash is in **no ref** now —
`git for-each-ref --contains 03d2dc6ec` prints nothing, `git merge-base --is-ancestor
03d2dc6ec HEAD` → NO — it survives only as the reflog entry the tip's commit **amended**
(`…@{2}: commit: docs(gendoc): record the acceptance re-run…`, amended by `…@{1}`). So the
hash named in the older revision is unreachable, and this revision replaces it with the tip
that is real. **What the correction does not change**: the diff between the re-run HEAD and
the final tip is `devdoc/gendoc-read-floor-corpus-less-tree.md` only —
`git diff --stat 748ee9530..HEAD -- '*.go'` prints nothing — so the Go tree under test is
identical and the run above is evidence for the delivered branch. That claim is the
measurement, not the sentence: run it.

**And a fresh run at the FINAL tip is recorded here, not implied** — the same command at
`20245dccf`, `artifacts/repro-final-tip-20245dccf.txt`:

```
HEAD=20245dccf43b83ad2f915867c235cea0d038589f
2026-10-12T11:34:25+08:00
ok  	github.com/nick/gopi/internal/tools	177.324s
exit=0
2026-10-12T11:37:32+08:00
```

The branch tip has since advanced by `devdoc/`-only commits (`git diff --stat <that-head>..HEAD
-- '*.go'` prints nothing), so this run is evidence for the delivered Go tree; the hash is named
so the reader can re-check that claim instead of believing it.
