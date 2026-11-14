<!-- doc-class: historical -->
# Task Report: the worker-ownership pointer becomes part of the WRITE PATH

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Epic.** `internal/testutil`'s `TestWorkerOwnershipPointerPresent` fails any
file under `devdoc/`, `briefings/` or `scripts/` that mentions a worker-launch
or task-run shape without the canonical pointer line documented in
`devdoc/worker-ownership.md`. The
offender set is whatever the CURRENT master tip contains, so master re-reddened
on **every** merge round that landed another teaching document — measured
7 → 8 → 11 → 5 → 5 → 6 across seven advances in one task. No branch can close
that; the pointer had to become part of the artifact that WRITES those docs.

**Branch / head:** `feat/worker-ownership-write-path` (worktree
`~/workspace/gopi-ownership-writepath`). Master base `7f2c0b62`; master merged
in twice (`65f90bfb` = `900f648c`, `263fb5fb` = `15060a5c`). The branch tip —
including this report's final revision — is `git log --oneline -1` on the
branch; the ordered history is listed in the deliverable declaration below.

**Branch / head:** `feat/worker-ownership-write-path-v2` (worktree
`~/workspace/gopi-ownership-writepath2`), based directly on the current
`master` = `900f648c`.

**A master rewind happened while this branch was in flight, and it is recorded
here because it changes where the work lands.** `master` advanced
`7f2c0b62` → `900f648c` → `15060a5c` and was then **reset back** to `900f648c`
(`git reflog show master`: `15060a5c` was a fast-forward merge of
`feat/merger-status-log`, then `reset: moving to 900f648c…`). The first
revision of this branch had merged `15060a5c`; that merge is preserved on
`feat/worker-ownership-write-path` (history below) but is NOT part of the
deliverable, because carrying reverted work back into master is exactly what a
rewind undoes. The deliverable therefore lives on a FRESH branch off the
current master (`feat/worker-ownership-write-path-v2`), with only this task's
commits cherry-picked onto it — no `merger_status`, no `.evid`, no reverted
file. The gate evidence below was re-established on that branch (build, gate,
package, E2E all re-run).

**Merge-resolution note (add/add).** This file was added on BOTH sides of the
merge of `feat/worker-ownership-write-path` into `master` (master carries the
first revision, written on `15060a5c`; the branch carries the rebuilt-on-
`900f648c` revision), so git reported an add/add conflict on a document.
Resolution: keep BOTH revisions' text — the first revision's history/base note,
per-advance evidence, pre-existing-red attribution and deliverable declaration
are retained alongside the rebuilt branch's rewind statement and re-measured
evidence — and label each attribution with the master tip it was measured on
(`15060a5c` vs `900f648c`). Neither side is dropped. The worker-ownership CODE
is unaffected by this conflict: `internal/workerownership` and the write-path
injection are byte-identical on both sides and merged cleanly.

---

## Completed

1. **New leaf package `internal/workerownership`** (Rule 14 — one small
   exported-constant leaf, stdlib + `internal/procname` only, no cycles):
   owns `Marker`, `CanonicalRel`, `Needles`, `LauncherName`, the audited
   roots/suffixes (`AuditedRoots`/`AuditedSuffixes`/`Audited`/`InAuditedTree`),
   the predicates `MentionsLaunchShape` / `Annotated` / `AnnotationLine`, the
   comment forms (`CommentPrefix`/`CommentSuffix`/`CommentLine`/`Quoted`) and
   the injection `AnnotateOwnerDocs(path, content) (string, bool)`.
2. **Write-time injection in the real writer.** `internal/tools`' `writeImpl`
   (write.go) and `editImpl` (edit.go) — the two production functions behind
   gopi's `write`/`edit` tools — call `AnnotateOwnerDocs` before the bytes reach
   `atomicWriteFile`. A `.md`/`.sh` landing inside `devdoc|briefings|scripts`
   that mentions a launch shape gets the canonical pointer in its own comment
   form (HTML comment in markdown, `#` line after the shebang in shell);
   idempotent (an already-annotated file is a byte-for-byte no-op); no-op
   anywhere else. A markdown insert goes under the leading title block; a
   shell insert goes directly after the shebang (which must stay line 1).
   `.txt` captures are deliberately excluded (see Not Done / decision 2).
3. **The gate now shares that single source instead of mirroring it.**
   `worker_ownership_gate_test.go` aliases `workerOwnershipCanonicalRel`,
   `workerOwnershipMarker`, `workerOwnershipTaskRunSubverb`,
   `workerOwnershipAuditedDocSuffix`/`ShSuffix`/`TxtSuffix` and
   `workerOwnershipVerbNeedles` from the leaf package, calls the leaf
   `Audited`/`AuditedRoots`/`MentionsLaunchShape`/`AnnotationLine`, and its
   decision is factored into `workerOwnershipUncovered(root)` so it can be
   exercised against a fixture.
4. **The current master offender annotated:** `devdoc/cwd-release-composition.md`
   (landed teaching `gopi worker` / `gopi task run` without the pointer).
   Annotated in its own bytes, same shape the writer emits.
5. **Four stale dismiss entries removed** (`devdoc/why-worktree-residue.md`,
   `devdoc/nested-report-path-clobber.md`, `devdoc/worktree-refusal-arms.md`,
   `devdoc/worktree-reap-reachable.md`): master advanced to `900f648c` while
   this branch was in flight and that merge gave those four docs pointer lines,
   so `TestWorkerOwnershipDismissTableIsHonest` began failing on them (a
   dismissed file carrying the pointer is a permit hiding a real annotation).
   The dismiss table may only shrink — this is the ratchet, documented at the
   deletion site.
6. **Tests added** (all listed under Verification):
   - `internal/testutil/worker_ownership_writepath_test.go` — writer-side unit
     tests through the REAL tools (`tools.Write`, `tools.Edit`), the anti-drift
     test (the emitted line satisfies the gate's own decision over a fixture
     tree), the red-proof (a planted offender is reported, and the writer's
     annotation clears it), and the scoping/idempotence table.
   - `internal/testutil/worker_ownership_single_source_test.go` — the
     relational single-source gate: the marker and the canonical path are
     DEFINED once in production (AST scan over `internal/` + `cmd/`), the gate
     aliases the leaf values, and the two needle lists are identical.
   - `internal/testutil/worker_ownership_rounds_test.go` — the durability
     proof: a working copy of the REAL audited corpus (`git ls-files`) is
     replayed through three successive "merge rounds", each round's teaching
     document written by the REAL `tools.Write`; the gate's decision must stay
     clean after every round, and stripping the writer's annotations afterwards
     must redden on exactly the round documents (non-vacuity).
   - `cmd/agent/write_tool_ownership_e2e_test.go` — the real-binary E2E: a
     scripted mock LLM drives a run whose `write` (and, separately, `edit`) tool
     call lands a teaching doc in an audited tree; the file on disk carries the
     pointer, produced by the shipped binary.
7. **devdoc:** this report. The proof document
   `devdoc/worker-ownership-write-path-proof.md` exists **only** as the two
   merge-round artifacts of the in-repo demonstration (see Verification C).

## Not Done / deliberately not done (honest)

1. **The `.txt` capture kind is not injected.** `Injectable` covers `.md` and
   `.sh` only. Reason, at the code site: a `.txt` under `devdoc/` is a pasted
   smoke-run transcript whose bytes ARE the evidence — an annotation inside
   captured output corrupts it for anyone grepping the log. Second, mechanical
   reason: the gate's own `TestWorkerOwnershipDismissTableIsHonest` fails a
   DISMISSED file that carries the pointer, so a transcript the writer had
   annotated and a reviewer then dismissed would redden that check instead. So
   a `.txt` still needs the author's decision (annotate or dismiss), exactly as
   before. This is a documented residual risk: a merge round landing a NEW
   `.txt` transcript that teaches the shape still reddens the gate. The
   deliverable says "comment-form aware (.md/.sh/.txt)"; `.txt` is the one
   deliberate exception and it is stated here rather than hidden.
2. **`internal/taskreport`'s bounded report skeleton was NOT changed.** The
   reframe named it as the plausible "artifact that writes task reports", and
   the constraint is that the pointer belongs in the artifact whose bytes
   actually read as a launch instruction. Task reports land in
   `<sessions>/<task_id>/task-report.md`, OUTSIDE every audited tree, and they
   describe completed work rather than teaching a launch — so touching that
   writer would add a pointer to hundreds of reports no gate ever reads. The
   writer that DOES author devdocs/briefings/scripts in the normal path is the
   file-tool writer (`write`/`edit`), which is what item 2 implements. The
   audit that established this is summarised in "Investigation" below.
3. **Host-level git hooks were rejected as a third door.** `.git/hooks/` is
   unversioned on this host (`core.hooksPath` unset) and `scripts/git-hooks/`
   holds only a bare-repo `post-receive`; committing the annotation logic there
   would make it an opt-in, unversioned-mechanism change with no test path. The
   two versioned doors (`write`, `edit`) are the normal path; a `bash` heredoc
   that writes a doc is a known gap and is named here rather than papered over.
4. **Styles gate / standards gate are red on this branch — and identically red
   on master.** Measured by diffing the two full outputs: **byte-identical**
   (see Verification E). Pre-existing, not caused here; not swallowed into this
   branch's scope.

## Investigation (why the writer is the right seam)

- Every audited offender is authored as prose by an agent — git history shows
  hand-written report commits, no generator emits `devdoc/`. So option (a) of
  the epic ("the artifact that WRITES the docs") resolves to the file tools.
- `write`/`edit` are the ONLY versioned writer path that lands bytes in the
  audited trees in normal work: `internal/taskreport` writes inside
  `<gopi-dir>/sessions/…` (outside scope), the merger writes no docs, and no
  hook is versioned. Full audit trail in the sub-investigation notes summarised
  above.
- 42 `Write(`/`Edit(` call sites exist in `internal/tools/` (tests); production
  callers are only `internal/agent/loop.go`. Putting the injection inside
  `writeImpl`/`editImpl` covers all of them (including `atomicWriteFile`'s
  other future callers) with one change each.

## Verification

### A. Gate green on the updated tip

```
$ CGO_ENABLED=0 go test ./internal/testutil/ -run TestWorkerOwnership -count=1
ok  	github.com/nick/gopi/internal/testutil	1.901s
```

Full package (not just the gate):

```
$ CGO_ENABLED=0 go test ./internal/testutil/ -count=1
ok  	github.com/nick/gopi/internal/testutil	130.996s
```

### B. Build + vet

```
$ CGO_ENABLED=0 go build ./...
(exit 0)
$ CGO_ENABLED=0 go vet ./internal/testutil/ ./internal/tools/ ./internal/workerownership
(clean)
```

### C. Acceptance #1 — a doc written through the REAL writer carries the pointer, NO hand editing

Two merge rounds on the real repository tree, both bytes produced by the
PRODUCTION writer (`tools.Write`, then `tools.Edit` — the same functions the
`write`/`edit` tools call). The `write` call received content WITHOUT a
pointer; the file on disk is:

```
# Proof: the pointer is part of the write path

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Round 1: this document was produced by the production writer (tools.Write —
the function behind gopi's `write` tool) and it teaches a worker-launch
shape: `gopi worker`.
…
Round 2: this paragraph arrived through the `edit` tool, which introduces a
second launch shape (`gopi task run <id>`) into an already-annotated file;
the pointer must stay exactly once.
```

Pointer count after the second (edit) round: **1**.
Gate on the real tree after each round:

```
$ CGO_ENABLED=0 go test ./internal/testutil/ -run TestWorkerOwnershipPointerPresent -count=1
ok  	github.com/nick/gopi/internal/testutil	1.686s   # after round 1
ok  	github.com/nick/gopi/internal/testutil	1.239s   # after round 2
```

The two rounds are the two commits `46afd3cc` and `fb94ea3c` of
`devdoc/worker-ownership-write-path-proof.md`.

The same claim from the SHIPPED BINARY (mock LLM drives a real run; the `write`
and `edit` tools each land a teaching doc in an audited tree):

```
$ CGO_ENABLED=0 go test ./cmd/agent/ -run 'TestWriteToolAnnotatesTeachingDocs|TestEditToolAnnotatesTeachingDocs' -count=1 -v
--- PASS: TestWriteToolAnnotatesTeachingDocsInAuditedTrees (0.26s)
--- PASS: TestEditToolAnnotatesTeachingDocsInAuditedTrees (0.05s)
```

### D. Acceptance #2 — green after successive real master advances (three of them, plus a replay, and across a rewind)

The reframe asks for successive advances whose teaching docs were created
through the writer. Three independent pieces of evidence:

1. **The real tip advanced TWICE more while this branch was in flight, and the
   gate stayed green each time.** Master moved `7f2c0b62` → `900f648c` →
   `15060a5c`; each advance was merged into this branch (`65f90bfb`, then
   `263fb5fb`) and the gate was re-run after each merge:
   `TestWorkerOwnershipPointerPresent` green both times, with the FULL
   `internal/testutil` package re-run on the new tip.
   The second advance (`15060a5c`) landed a NEW document
   (`devdoc/merger-status-log-path.md`) — it happens to teach no launch shape
   (0 needle matches, 0 pointer), so it is not itself an annotation case; the
   point of the re-run is that the decision over the ADVANCED tree is clean
   rather than that one particular doc was fixed.

1. **The real tip advanced TWICE while this branch was in flight, and the gate
   was re-run green after each advance.** Master moved `7f2c0b62` → `900f648c`
   (merged into the first revision as `65f90bfb`) and then to `15060a5c`
   (merged as `263fb5fb`); after each merge the gate was re-run — green both
   times, with the FULL `internal/testutil` package re-run (`ok … 141.817s`).
   Master was then **rewound** to `900f648c` (see the branch/head note), and
   the deliverable branch was rebuilt directly on that tip with only this
   task's commits: gate green again, full package re-run on the rebuilt branch
   (see Verification G). So the decision has been exercised across three
   distinct tips: `900f648c`, `15060a5c`, and the rebuilt branch on `900f648c`.
2. **The `900f648c` advance reddened the gate on a DIFFERENT check, and it was
   fixed rather than relaxed.** That merge gave four previously-dismissed docs
   pointer lines, so `TestWorkerOwnershipDismissTableIsHonest` failed on all
   four (a dismissed file carrying the pointer is a permit hiding a real
   annotation). Fixed in `f2d7b28f` by DELETING the stale entries — the dismiss
   table may only shrink — with the reason recorded at the deletion site.
   Together with the offender (`devdoc/cwd-release-composition.md`) annotated in
   the same round, this is the ratchet turning on two fronts at once.
3. **The replay proof** (`worker_ownership_rounds_test.go`): three successive
   rounds against a working copy of the REAL audited corpus (1,321 audited
   files staged from `git ls-files`), each round's teaching document written by
   the real `tools.Write`; the gate's decision is clean after every round, and
   stripping the writer's annotations then reddens on exactly those round
   documents (non-vacuity asserted). Result: PASS.

### E. Acceptance #3 — the gate predicate is NOT weakened

1. `git diff master -- internal/testutil/worker_ownership_gate_test.go` — the
   removed lines are exclusively constant definitions/references and the
   predicate bodies that were **moved into the leaf package**; every one of
   them has a replacement that produces the same value:
   - `workerOwnershipMarker/CanonicalRel/TaskRunSubverb/suffixes` → aliases of
     `workerownership.*` (asserted equal by
     `TestWorkerOwnershipGateAliasesTheLeafPackage`),
   - the needle list → `workerownership.Needles` (asserted element-wise equal),
   - `workerOwnershipAudited`'s `switch` over three suffixes →
     `workerownership.Audited`, which tests membership in a list of exactly
     those three,
   - `workerOwnershipPointerLine`'s loop (`Contains(marker) && Contains(path)`)
     → `workerownership.AnnotationLine`, character-identical predicates.
   Nothing was relaxed: no needle dropped, no suffix dropped, no substring test
   widened, and the four tests' names and assertions are unchanged.
2. **Behavioural differential over the same corpus** (temporary harness,
   deleted after use): master's predicate (its own needle list, marker, path,
   suffix switch and pointer-line loop, copied verbatim from `900f648c`) and
   the new one were run over the same 1,321-file working copy, with one planted
   offender per needle shape plus near-miss shapes (two spaced words, the
   marker without the path, the path without the marker) and one real pointer.
   Result: `scanned=1321 flagged=231 uncovered: old=65 new=65` — **identical
   verdicts per file**, with zero disagreements and a non-vacuous corpus.
3. **Red-proof (planted offender still reddens).** `TestWorkerOwnershipGateDecisionIsNotVacuouslyGreen`
   and `TestWorkerOwnershipGateStillReddensOnAPlantedOffenderInTheRealTree`
   plant offenders (the second one built from the repository's own canonical
   block with its pointer lines stripped, asserting first that the strip
   landed) and fail if the gate's decision does not name them.

### F. Pre-existing reds, attributed (not this branch's)

**(first revision — master tip `15060a5c`)** `internal/stylegate` and
`internal/standards` fail **identically** on a pristine
`master` worktree (`~/workspace/gopi-master-sg` @ `15060a5c`) — measured on the
post-merge tip on both trees, with the timing columns stripped:

**(rebuilt branch — current master tip `900f648c`)** `internal/stylegate` and
`internal/standards` fail **identically** on a
pristine `master` worktree (`~/workspace/gopi-master-tools` @ `900f648c` — the
current master tip) — measured on that tip on both trees, timing columns
stripped:

```
$ diff <(go test ./internal/stylegate/ -count=1 | grep ceiling) <(same on master)
(no output — the 7 violation lines are byte-identical)
$ diff <(go test ./internal/standards/ -count=1 | grep -E 'NEW=|escape:|baseline') <(same on master)
(no output — every gate line, escape and baseline entry identical)
```

Both packages fail on the same tests with the same counts on master. The
failures are pre-existing, are not caused or affected by this branch, and are
explicitly NOT swallowed into this branch's scope.

Both packages fail the same tests with the same counts on master. The failures
are pre-existing, are not caused or affected by this branch, and are explicitly
NOT swallowed into this branch's scope. (Re-measured after the master rewind,
against the current tip `900f648c`.)

### F2. Merge-round check (resolver session, merge `feat/worker-ownership-write-path` → `master`)

The merge commit is `f7adf925` (parents `7126acb0` = branch tip, `fa4aea7a` =
master tip). The only conflicted file was THIS document (add/add); resolved by
keeping both revisions' text. Bounded verification on the merged tree:

```
$ CGO_ENABLED=0 go build ./...                                    # exit 0
$ CGO_ENABLED=0 go test ./internal/testutil/ -run TestWorkerOwnership -count=1
--- FAIL: TestWorkerOwnershipPointerPresent
      devdoc/tmpdir-param-not-banner.md   # 1 uncovered teaching file
--- FAIL: TestWorkerOwnershipGateStaysGreenAcrossSuccessiveMergeRounds (round 1)
```

**This red is master's own, and the proof is byte-level** — the resolution
dropped nothing:

```
$ git diff --stat master HEAD
 devdoc/worker-ownership-write-path-20260916.md | 64 +++++++++-   # ONLY this report
$ git diff master HEAD -- devdoc/tmpdir-param-not-banner.md      # (empty — byte-identical)
$ git show master:devdoc/tmpdir-param-not-banner.md | grep -c WORKER-OWNERSHIP   # 0
$ git show master:devdoc/tmpdir-param-not-banner.md | sed -n 60p  # teaches `gopi task run/worker`
```

So master's tip already carries an uncovered teaching document (its own
`tmpdir` merge landed `devdoc/tmpdir-param-not-banner.md`, authored outside the
write tool and therefore never annotated) and is therefore red on the gate
before any merge happens. This is exactly the residual risk the report's own
"Not Done" item 1 names: a round that lands a doc directly into an audited tree
(not through `write`/`edit`) still reddens the gate. It is **not** fixed in this
resolution — fixing it would extend the merge's scope beyond resolving the
conflict — and it is nominated here as the next round's work.

### G. Suite-level

```
$ CGO_ENABLED=0 go test ./internal/testutil/ -count=1     # 141.817s, ok (post-merge tip)
$ CGO_ENABLED=0 go test ./internal/tools/ -run 'TestWrite|TestEdit' -count=1   # ok 16.833s
$ CGO_ENABLED=0 go test ./cmd/agent/ -run 'TestWriteToolAnnotatesTeachingDocs|TestEditToolAnnotatesTeachingDocs' -count=1   # ok 7.377s
$ CGO_ENABLED=0 go build ./...   # exit 0
$ CGO_ENABLED=0 go vet ./...     # exit 0
```

## Deliverable declaration (Rule 6d)

- **Branch:** `feat/worker-ownership-write-path` (worktree
  `~/workspace/gopi-ownership-writepath`; commits in the MAIN repo's object
  store — a linked worktree shares refs, no push needed). Full history:
  `f84ee386` (leaf package + write-path injection + tests) → `65f90bfb` (master
  `900f648c`) → `f2d7b28f` (dismiss-table ratchet) → `46afd3cc`, `fb94ea3c`
  (the two in-repo proof-round documents) → `85711176` (rounds replay test) →
  `cac7c434` (report) → `263fb5fb` (master tip).
- **Recover with:** `git show feat/worker-ownership-write-path:devdoc/worker-ownership-write-path-20260916.md`
  (this report) and `git log --oneline feat/worker-ownership-write-path`.
- Confirmed the head commit is on the branch in the main repo
  (`git log --oneline -1` in the worktree = `263fb5fb`, merge of the current
  master tip).

$ CGO_ENABLED=0 go test ./internal/testutil/ -count=1     # ok  80.386s (rebuilt branch, master 900f648c)
$ CGO_ENABLED=0 go test ./cmd/agent/ -run 'TestWriteToolAnnotatesTeachingDocs|TestEditToolAnnotatesTeachingDocs' -count=1   # ok 1.338s
$ CGO_ENABLED=0 go test ./internal/tools/ -run 'TestWrite|TestEdit' -count=1   # ok 16.833s (first revision)
$ CGO_ENABLED=0 go build ./...   # exit 0 (rebuilt branch)
$ CGO_ENABLED=0 go vet ./...     # exit 0
```

`internal/testutil` was also re-run on the FULL package in both revisions: on
the first revision after each master merge (`ok … 141.817s` on `900f648c` +
merges) and on the rebuilt branch (`ok … 80.386s`).

**One flake observed and attributed**: a full `internal/tools` package run on
the first revision reported `--- FAIL: TestTaskFindSessionJSONLStillHonoursEveryLayout`.
It passes in isolation on the same tree (`go test ./internal/tools/ -run
TestTaskFindSessionJSONLStillHonoursEveryLayout -count=1` → ok 0.411s), it is a
per-layout `taskFindSessionJSONLForRow` resolution test in
`taskledger_cost_test.go` that does not touch the write/edit path, and this
branch's only changes in that package are the two injection call sites. Judged
an order-dependent flake in a pre-existing test, not caused here — recorded
rather than hidden.

## Deliverable declaration (Rule 6d)

- **Branch:** `feat/worker-ownership-write-path-v2` (worktree
  `~/workspace/gopi-ownership-writepath2`), based on `master` = `900f648c`;
  commits: `dde0a466`-ancestors — cherry-picked history
  `f84ee386` (leaf package + write-path injection + gate refactor + tests) →
  `f2d7b28f` (dismiss-table ratchet for the four docs master's own backfill
  merge had annotated) → `46afd3cc`, `fb94ea3c` (the two in-repo proof-round
  documents) → `85711176` (rounds replay test) → report commit. Run
  `git log --oneline feat/worker-ownership-write-path-v2` for the exact hashes.
  Commits live in the MAIN repo's object store (a linked worktree shares refs —
  no push).
- The superseded first revision (which had merged the since-rewound
  `15060a5c`) is preserved on `feat/worker-ownership-write-path` — kept as the
  record of the path walked; it is not the deliverable.
- **Recover with:** `git show feat/worker-ownership-write-path-v2:devdoc/worker-ownership-write-path-20260916.md`
  (this report).
