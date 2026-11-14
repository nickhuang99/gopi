<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The merge conflict the run manufactures against itself — and the mechanical lane that resolves it

**Epic:** gopi-merge-llm-resolves (2026-09-21). The merge engine is supposed to
fast-forward what it can and hand genuine conflicts to an LLM (the entry point it
was reached through in 2026-09 was the top-level `gopi merge`, removed
2026-11-01 — the spelling today is `gopi task merge`); real runs were
instead parking branches in the rework lane. This document records what was
measured, what the mechanism is, what changed, and what is deliberately NOT
fixed.

## 1. What was measured

Nine rows sat parked in the branch-rework lane. Every one of them was within the
conflict ceiling by a wide margin, and **four conflicted on exactly one file:
`devdoc/DOC-INDEX.tsv`**.

The decisive number was the hand-over FAILURE counter: **0**. The `handover`
lane had never been entered — so those branches were not failing at the LLM
stage, they never reached it.

## 2. The mechanism: the run conflicts a branch with its own siblings

The sweep in `mergeSweepRepo` decides branch 1, LANDs it, then decides branch 2
**against a master branch 1 has already moved**. Every branch that adds a devdoc
appends a row to `devdoc/DOC-INDEX.tsv` — a checked-in generated index whose
only writer is a branch's own commit. Two branches always collide on its tail.

Reproduced without the command, in
`cmd/agent/merge_union_test.go:TestMergeSiblingAppendIsCleanAtStartAndConflictedAfterTheRunMovesMaster`:

1. build two siblings that each append a row; land neither;
2. the second sibling merges **cleanly** against the run's starting master;
3. land the first sibling, then re-measure the same branch → it conflicts on
   `devdoc/DOC-INDEX.tsv` and nothing else.

The collision is textual, not semantic: both sides want to keep their own rows.

### Why the existing design did not prevent it

`cmd/agent/merge_prompt.md` §3.0 ALREADY says "do the easiest first" — the
numbering and the escalation rules are complete. What was missing was any
enforcement: the ordering is a request to an LLM, the child deletes the branch
after filing its ticket, and the counter that would have exposed the loop reads
zero because nothing was counting decisions the command itself made. So the two
halves of the design — the command measures, the child decides — disagreed about
who owns the artifacts a branch's own generator writes.

## 3. The change

### 3.1 A mechanical lane for declared append-only artefacts (`cmd/agent/merge_union.go`)

> Absent on this tree: `cmd/agent/merge_union.go` was removed with the merge
> route (`5a9af79b`). This section is the record of what the lane did, not a
> description of code here — see the retirement note in §6.

Before any context is spent, and only for a diverged branch whose conflicted set
is **entirely** declared generated artefacts, the run:

1. materialises the merge in a throwaway worktree (Rule 22 scratch tree);
2. reads both sides from git's index stages (`:2:` ours, `:3:` theirs);
3. assembles the **union** of both sides' rows (one row per path, see below);
4. **proves** it — the union parses, carries no path twice, and no path either
   side carries is missing (`mergeUnionNoRowLost`);
5. commits the resolution in the probe and lands it by
   `git merge --ff-only <commit>` — a REAL merge commit, with the branch as its
   second parent, so the history records the join;
6. reports `union-resolved` and counts the branch as a LANDING. **(Until
   2026-11-01 this line continued "…which is what owes the post-pass `go build
   ./...` + `go vet ./...` (and its withdrawal on red)". There is no such thing
   to owe any more: the post-pass verify/withdraw lane
   (`internal/tools/merge_pass_verify.go`) was deleted with the retired top-level
   `gopi merge` verb, and `gopi task merge` does not verify or withdraw the merged
   tree. The landing count still exists; the post-pass obligation it referenced
   does not.)**

The artefact set is **declared**, a closed list (`mergeUnionArtefacts`), not a
path pattern guessed at the call site. Today it holds exactly one entry —
`docclass.Path`, the devdoc truth-class manifest — because that is the artefact
the measurement showed colliding.

**The boundary is hard and tested:** one non-artefact path in the conflicted set
leaves the whole branch to the ordinary ladder. Two branches that disagree about
what CODE should say is the hand-over's judgement; no rule here guesses at it
(`TestMergeUnionLaneRefusesAGenuineSemanticConflict`).

**Union, never `checkout --theirs`.** Measured on the first probe: `git checkout
--theirs <path>` keeps the other side and DROPS ours. The manifest is read by a
gate, and a dropped row is a dropped classification — the union is assembled
explicitly instead, and the row-loss proof is what makes the claim checkable.

**The union is a row SET, not a line splice** (added after the first real
branch, see §5). Each path ends with exactly ONE row:

- rows are merged keyed by `(path, class)`;
- a path whose two sides carry rows of the SAME class collapses to one row — the
  appended-then-edited shape a manifest naturally accumulates (master carries
  three such paths);
- a path whose two sides DISAGREE about the class is decided by **the document**:
  the class the doc advertises in its own banner (`<!-- doc-class: historical -->`)
  wins, because the manifest's own gate already fails a doc whose banner and row
  disagree. With no banner to corroborate a class, the union is REFUSED — this
  lane is not entitled to choose which of two classifications another agent's
  work keeps.

Without that rule the first real branch was refused: master carries
`devdoc/orphan-stash-7daab90b-salvage-report.md` classified twice (once
`historical`, once phrase-varied `historical`), so the union of two branches each
carrying a copy produced two rows and the no-duplicate proof — correctly —
refused. The lane was declining, silently, on the exact shape it exists for.

### 3.2 The post-union row-integrity check (`cmd/agent/merge_union_rows.go`)

> Absent on this tree, same removal (`5a9af79b`) — record, not current code.

A union keeps every ROW and can still leave a row whose spot-check no longer
resolves (the branch it came from deleted the path it names, or moved a symbol).
That would be a merged master with a red canonical doc gate. The check resolves
every normative row's evidence against the merged tree and **prints** what it
finds — in the session that is already looking at the branch.

It does **not** block the landing: the manifest's gate is doc policy, not a
compile gate, and refusing a landing over a doc spot-check would re-create the
loop this epic removes. A named, actionable finding plus a landing is the honest
direction; a silent red master is not.

The vocabulary is `internal/docclass`'s, asked (not re-implemented) — a second
reading of the evidence tiers in the merge lane would be exactly the drift class
that package exists to remove (Rule 14).

**It found two genuinely rotted rows on master** (see §5).

### 3.3 An enforced wall-clock budget for the hand-over child (`cmd/agent/merge_handover_budget.go`)

> Absent on this tree, same removal (`5a9af79b`); and the `--handover-budget`
> flag it describes was deleted with the retired command's parser — `gopi task
> merge` accepts no such flag. Record, not current code.

The prompt has always ASKED the child for a ~45 minute budget (§3.4) and nothing
enforced it; the only real bound was the context ceiling, which is a token count,
not a clock. The run now **publishes** that budget into `GOPI_TIMEOUT_SEC` — the
loop's OWN documented deadline (CONFIG.md; no second mechanism, Rule 2) — and the
child inherits it. `--handover-budget MIN` overrides it; an ambient
`GOPI_TIMEOUT_SEC` wins (the environment is the operator speaking to the loop
directly); `0` disables it and **says so**.

The consequence is stated in the prompt (§3.4.1) and in the code: a cut run files
nothing and deletes nothing (a clock is not a judgement), but the failure IS
counted, so a branch whose hand-over keeps being cut escalates into the rework
lane instead of being re-handed over forever.

## 4. What is NOT done (honest)

1. **The sweep-order root cause is untouched.** This lane removes the class of
   parked branch the measurement found, but branches decided late are still
   measured against a master the run itself moved. The `--fast` arm is unchanged
   (the closure still has to win for that).
2. **`internal/standards/baseline.go` is the sibling of this class** — an
   append-only registry surface that re-conflicts as siblings land (row 3018's
   own payload says so). It is NOT added to the union set: a baseline value is
   "the set of violations that exist in THIS tree", and a blind union there could
   turn the standards gates green by admitting violations. Making that mechanical
   needs a real argument (or a regeneration-from-a-clean-tree pass) and is its own
   task.
3. **The nine parked rows.** Their branches are GONE (several salvageable only
   from `rework-backup/*` tags) and their diffs conflict against a master that
   moved well past them. The tickets do not describe a mechanical fix. Eight of
   them were filed by the very loop this lane removes; folding them back requires
   deciding whether each branch's work is still wanted. That decision is the
   operator's, not a merge rule's — see the row-by-row measurements in §5.
4. The prompt's §3.4 self-imposed budget text is left in place: it is now the
   child's own discipline UNDER an enforced ceiling, not a substitute for one.

**CLOSED SINCE (2026-09-21, same day):** an earlier revision of this list said the
post-pass measurement was "NOT yet observed on a Go-module repo". It then WAS —
§5, "the post-pass on a real Go module" — and the observation carried a finding
the unit tests could not reach: the union lane could land CORRECTLY and have the
whole batch withdrawn by the post-pass gate. **Retired mechanism (merge pass
verify/withdraw deletion, 2026-11-01):** that gate was deleted with the retired
top-level `gopi merge` verb, so "the intended contract" named below no longer
exists — read it as the record of a contract that was measured, not as one the
shipped engine keeps.

## 5. Evidence (all measured 2026-09-21, branch `fix/merge-llm-resolves-conflicts`)

**The reproduction** (real git, no command):
`git merge --no-commit --no-ff sib2` → clean at the run's starting master;
after `git merge --ff-only sib1` the same command → `CONFLICT` on
`devdoc/DOC-INDEX.tsv`, unmerged set exactly `{devdoc/DOC-INDEX.tsv}`.

**The real branches** (nine parked rows, probed against master `2c14d74c`):

| branch | ref | conflicted against current master |
|---|---|---|
| feat/rule31-ran-check | `rework-backup/feat/rule31-ran-check` | `devdoc/DOC-INDEX.tsv` |
| fix/unknown-dash-flag-refusal | `rework-backup/fix/unknown-dash-flag-refusal` | `devdoc/DOC-INDEX.tsv` |
| feat/cli-settled-state | commit `14cde0d5` | `devdoc/DOC-INDEX.tsv` |
| rework/chat-three-branches | commit `a1229f31` | `devdoc/DOC-INDEX.tsv` |
| feat/chat-colour-palette | `rework-backup/feat/chat-colour-palette` | `cmd/agent/cli_chat.go`, `devdoc/DOC-INDEX.tsv`, `internal/agent/loop.go` |
| feat/gopi-cli-bubbletea-landing | `rework-backup/feat/gopi-cli-bubbletea-landing` | `cmd/agent/main.go` |
| fix/serve-lock-refusal-diagnosis | `rework-backup/fix/serve-lock-refusal-diagnosis` | `cmd/agent/cli_tasks.go`, `cmd/agent/merger_lock_ownership_e2e_test.go`, `cmd/agent/serve_lock_errno_test.go` |
| feat/g2-sync-help-surface | commit `cc0f9a44` | `internal/standards/baseline.go` |

**Four of the nine are the append-only case** and are resolved by this lane today.

**None of the parked work is in master** — measured with
`git merge-base --is-ancestor <ref> master` (all negative) and
`git rev-list --count master..<ref>` (1–9 commits each). Six of the eight are
reachable only through an `rework-backup/*` salvage tag; three
(`feat/cli-settled-state`, `rework/chat-three-branches`, `feat/g2-sync-help-surface`)
have no tag and exist only as commits named in their tickets' payloads.

**The row check against the repository's own manifest** found two genuine rot
rows, both re-pointed in this change — and both are now `sym` rows rather than
line `anchor`s, deliberately:

- `devdoc/merge-command.md` — its anchor named `cmd/agent/merge_run.go:343` for
  `mergeNoVerifyLineFormat`, which had drifted (this branch's own edits moved it
  to `:385`). A line anchor on a file this volatile rots on every edit above it,
  so the row now names the SYMBOL: `sym cmd/agent/merge_run.go
  mergeNoVerifyLineFormat`.
- `devdoc/taskid-whitespace-normalize-salvage.md` — its anchor named
  `internal/tools/memory_sync_claim.go:185` for `StartClaimLockBroadcast`, which
  is at `:220`; now `sym internal/tools/memory_sync_claim.go
  StartClaimLockBroadcast`.

> **Retired mechanism (merge verb removal, 2026-11-01).** The `bin/gopi merge …`
> commands in the transcript below are the RECORD of what this run executed in
> 2026-09-21. The top-level verb was removed afterwards (its implementation and
> flag parser deleted), so those exact lines cannot be re-run today — and the
> flags they carry (`--fast`, `--handover-budget`) have no counterpart on the
> surviving spelling at all. The mechanism the transcript documents (the
> regenerate/union lane) is unchanged and still runs inside the engine, which is
> reached today as `gopi task merge`; its contract page is the FILE
> `man/gopi-merge.md`.

**The end-to-end run on a real branch** (binary built from this branch, a clone
of the repo, `probe/append-only` = commit `451a0117` of the real
`feat/rule31-ran-check`, a 1-minute hand-over budget so no LLM could win the
branch):

```
$ bin/gopi merge --fast --handover-budget 1      # the fast arm writes nothing
master after --fast: 2c14d74c  (want 2c14d74c)   # unchanged, tree clean

$ bin/gopi merge --handover-budget 1             # the ordinary run
  .../repo probe/append-only union-resolved  unioned devdoc/DOC-INDEX.tsv
      (2175 row(s)) in a merge commit — mechanical, no context spent
master AFTER:  b4306559
b4306559 merge: union the append-only rows of devdoc/DOC-INDEX.tsv
         (2175 row(s), mechanical, gopi merge union lane)
  parents: 2c14d74c 451a0117
both sides' rows present: 1  (devdoc/rule31-ran-check.md)
duplicate paths in the merged manifest: (none)
git status --porcelain: (empty)
post-pass verification: go build ./... + go vet ./... green
```

The union commit has the conflicted branch as its second parent — a REAL merge
commit, not a fast-forward. That run passed `--no-verify` deliberately (the
fixture repo declares no build, so the post-pass gate ABSTAINED there and would
have measured nothing). **Retired mechanism (merge pass verify/withdraw
deletion, 2026-11-01).** The paragraph that stood here went on to say the
post-pass measurement was "the first thing to run when this branch merges into a
real checkout", and named the arithmetic (`mergeVerifyLandedPasses`) that made a
union-resolved branch owe it. The post-pass verify/withdraw lane
(`internal/tools/merge_pass_verify.go`) was deleted with the retired top-level
`gopi merge` verb, so nothing owes it now and there is no run left to make.

**The measurement that came OUT of the real run** (the reason §5 exists): the
first binary through this lane landed the union commit and LEFT THE SPENT REF
BEHIND — the prune lived on the fast-forward arm only, so `sib2` still resolved
after master had moved past it. A spent ref left lying is a branch every later
run re-decides. Commit `4f5a4387` wired the prune onto the landing (recorded
through `removalaudit`, reporting "ref kept" if the delete fails — Rule 15) and
`TestMergeUnionLaneReportsTheTransitionOnTheOrdinaryRun` now pins both halves:
the lane itself never deletes the ref it did not judge, and the run that lands
the union commit prunes it, with a second run seeing the ref spent rather than
re-resolving it. A stale binary is the trap to re-check on any re-run.

## 6. The verification recipe (reusable)

> **Retired mechanism (merge verb removal, 2026-11-01).** Every test this recipe
> names is GONE, and so are the files it was written against —
> `cmd/agent/merge_union.go`, `cmd/agent/merge_union_rows.go` and
> `cmd/agent/merge_handover_budget.go` are all absent from this tree. The tests
> went with the merge route in `5a9af79b` ("test(merge): 退役 merge 路线的 95 个测试文件",
> 103 files, −29538 lines), which is an ancestor of HEAD. **Measured on HEAD — all
> three commands exit 0 and run nothing**, which is the trap this note exists to
> stop: `go test` prints `no tests to run` and exits 0, so a reader who pastes
> the recipe below gets a green that measured nothing.
>
> ```
> $ go test ./cmd/agent -run TestMergeSiblingAppendIsCleanAtStartAndConflictedAfterTheRunMovesMaster -count=1
> ok  	github.com/nick/gopi/cmd/agent	3.346s [no tests to run]
> $ go test ./cmd/agent -run 'TestMergeUnion|TestMergeHandoverBudget' -count=1
> ok  	github.com/nick/gopi/cmd/agent	3.033s [no tests to run]
> $ go test ./cmd/agent -run TestMergeUnionRowCheckOnTheRealManifest -count=1
> ok  	github.com/nick/gopi/cmd/agent	3.073s [no tests to run]
> ```
>
> Kept as the record of how that lane was verified, not as a recipe to run. The
> live union-adjacent machinery is the manifest-duplicate gate, and its recipe is
> in `man gopi-merge`.

```bash
# 1. the mechanism, without the command (fast, no binary):
go test ./cmd/agent -run TestMergeSiblingAppendIsCleanAtStartAndConflictedAfterTheRunMovesMaster

# 2. the lane, the boundary, the report line:
go test ./cmd/agent -run 'TestMergeUnion|TestMergeHandoverBudget'

# 3. the lane on the repository's own manifest (read floor inside):
go test ./cmd/agent -run TestMergeUnionRowCheckOnTheRealManifest

# 4. a real branch against a real master (no command, no LLM):
git merge --no-commit --no-ff <ref>    # then: git diff --name-only --diff-filter=U
```

The fixture that proved fastest: build the collision with two sibling branches
each appending one row to a manifest whose header is
`internal/docclass.ManifestHeader`, land the first, then measure the second.

### The post-pass on a real Go module (2026-09-21, `probe13`)

> **Retired mechanism (merge pass verify/withdraw deletion, 2026-11-01).** This
> section measures a gate that no longer exists: the post-pass verify/withdraw
> lane was deleted with the retired top-level `gopi merge` verb. It is kept as
> the record of a measurement that was really made — read the sentences below as
> a description of that run, never as a description of what `gopi task merge`
> does today.

The end-to-end proof that the union lane and the post-pass gate agree. A
throwaway module with a real `main` package (`go build ./...` must have a package
to build — a library-only module reports `no main packages to build` and the
post-pass reads that as RED, correctly), a manifest in the DECLARED form
(`path\tclass\treason\tevidence`, class from `internal/docclass.Classes`), and two
sibling branches each appending one row:

```bash
# scope the run to the fixture: the sweep walks settings.json "repos", and that
# table is ABSOLUTE paths (a bare `cd <fixture>` does NOT scope `gopi merge`)
cat > "$P/settings.json" <<EOF
{"repos": {"$P": {"build": "go vet ./..."}}}
EOF
GOPI_SETTINGS="$P/settings.json" ./bin/gopi merge --handover-budget 0
```

Observed (verbatim):

```
gopi merge: sib2: union-resolved — unioned devdoc/DOC-INDEX.tsv (3 row(s)) in a
            merge commit — mechanical, no context spent, ref pruned
gopi merge: post-pass verification verified — post-pass verification PASSED
            (go build ./... + go vet ./... green)
master AFTER: b5c0158 merge: union the append-only rows of devdoc/DOC-INDEX.tsv
              (3 row(s), mechanical, gopi merge union lane)
  parents: e39ee3d fd591b1          # a REAL merge commit, not a fast-forward
  merged manifest: row0 + sib1's row + sib2's row   (3 — none lost)
  branches: *master                  (both spent refs pruned)
```

**The finding the unit tests cannot reach** (seen on `probe11`, and it is the
contract working): with a `go.mod` that has no buildable package, the union lane
landed the branch correctly and the post-pass then reported

```
MERGE pass verify RED build: go build ./... FAILED (first: go: no main packages to build)
MERGE pass verify: batch WITHDRAWN — d905fe68 → e7e87e9a (reset --hard, …);
                  2 branch ref(s) kept: sib1 sib2
```

i.e. a mechanical landing is NOT exempt from the measurement: `union-resolved`
counts as a LANDING (that is why it is in the post-pass's scope at all) and a red
verdict resets master and puts BOTH refs back, with the branch attribution printed
alongside. That is the whole point of counting it — and it was only observation
that showed the lane and the gate line up.

**Two refusals that were CORRECT, recorded so they are not "fixed" later**:
`probe9` wrote a two-column header and got *"our side does not open with the
declared artefact's column line"*; `probe10` wrote class `keep` and got
*"unknown class \"keep\" — declare it in internal/docclass.Classes"*. Both times
the LANE was right and the FIXTURE was not the declared artefact — the guards that
keep this lane scoped to a known shape are what produced each refusal.

**The scoping trap**: `probe7` ran `gopi merge` with the cwd inside the fixture
and the run swept `/home/user/work/GoPi` — the real repo — because the sweep list
comes from the settings file's `repos` table (absolute paths), never from the cwd.
A fixture MUST set `GOPI_SETTINGS`; scoping is a settings question.

## 7. Does the writer that rewrites the SOURCE also refresh the anchor? (measured 2026-10-12)

Task `01a12bf415cd7359bcd3c6db9261857f`. The epic was "the row-integrity check
is red on master with two rotted rows — re-point them, and judge whether an
anchor that rots should be refreshed by the writer that rewrites the source".
The two rows were already re-pointed before this run (§7.2b); what follows is
the measurement that the JUDGEMENT needs, and the answer.

### 7.1 What the check actually re-verifies

`mergeUnionCheckRows` (this file's §3.2) resolves every `normative` row's
evidence against the tree, through `internal/docclass`. For the anchor tier the
AUTHORITY is the cell's RECORDED TEXT — `anchor <path>:<line> at <text>`, where
`<text>` is the source line the row saw when it was written; the line number
beside it is an ACCELERATOR, consulted first and abandoned the moment the text
turns up elsewhere in the same file (`Evidence.anchoredTextLine`; the tier's own
doc block states why). So a row rots when the tree stops CARRYING THE TEXT,
whatever the number then says — and it does not rot when only the number moved.
That asymmetry is the whole question: the cell is a quotation, and a quotation
decays when you edit the thing it quotes.

### 7.2 What was measured

**(a) The refresh is a pure function of the tree — the code writer *could* do
it.** `python3 tools/docclass/classify.py . --dry-run --only <doc>` re-derives
the cell from the current tree alone (no history, no bookkeeping): today it
emits `anchor cmd/agent/chat_voice_live.go:249 at run := runVoiceLiveSession(`
and `anchor internal/memoryprefetch/session.go:119 at func (s *Session) Cl…`.

**(b) The rot is five days wide, and it was never a LOST refresh.** On master
(`a3a31da42`) the check is RED with exactly two findings — in an extracted
`git archive` of master, with the script from §6:

```
--- FAIL: TestMergeUnionRowCheckOnTheRealManifest (0.96s)
  devdoc/doubaostream-session-test-coverage.md (line 644): evidence anchor:
    cmd/agent/chat_voice_live.go no longer carries the recorded text
    "defer func() { _ = sess.Close() }()" …
  devdoc/memory-prefetch-one-lookup-per-seal.md (line 1044): evidence anchor:
    cmd/agent/chat_prefetch.go no longer carries the recorded text
    "func (p *chatPrefetch) takeShared(line, query, source string) (string, bool) {" …
```

Both moving landings (`469fb49d6` "one runner for a live utterance",
`8cbddb52a` "one typed line is retrieved ONCE") DID touch
`devdoc/DOC-INDEX.tsv` — each appending its own new doc's row — and neither
touched the doc whose row rotted (`git show --name-only`). So the rot is not a
merge that dropped a refreshed row: **a code commit that moves text a row quotes
appends its own row and refreshes nobody else's.**

**(c) "Which site?" is a reading, not a derivation.** At master the doc's own
literal `chat_voice_live.go:261` pointed at a line that no longer held the
teardown — but the generator resolves the FIRST literal site it finds, and the
literal was still literally present in the file (an unrelated comment line sits
there now). Re-deriving blindly would have re-pointed the row at a stranger's
line. `6ffc1f3c3` instead re-anchored the DOC's prose to the new home
(`cmd/agent/chat_voice_live.go:249` → `runVoiceLiveSession`, whose shared body
`cmd/agent/voice_stream_runtime.go:191` defers the close and
`cmd/agent/voice_stream_runtime.go:237` calls `sess.Finish`) — i.e. the repair was made by re-reading the CLAIM, not by re-running the
generator.

**(d) "Just re-run the writer" is not the operation a code commit can call.**
`6ffc1f3c3` committed the cite cell it had (a landed ruling: cited by
`internal/doubaostream/session_teardown_test.go:6`), while the generator TODAY
derives a different one (`cited by internal/agent/testdata/docanchor-baseline.tsv:306`).
Both are true — a `cite` claim is name-shaped (the artifact must still NAME the
doc, not name it on the same line) and the baseline row at
`internal/agent/testdata/docanchor-baseline.tsv:306` does name it —
so the committed cell is not wrong. But it shows the shape: the doc-side writer
is a CORPUS writer (a row, its reason cell, its banner), not a per-cell poke,
and re-running it rewrites the cells of the rows it touches.

**(e) The line number is what makes the repair recur.** The same landing
(`8cbddb52a`) had shifted five MORE anchors inside
`devdoc/memory-prefetch-one-lookup-per-seal.md` (`turnQuery :492→:506`,
`prefetchOutcomeShared :133→:147`, `chatPrefetchAdopt :1327→:1363`,
`discardTypingSection :338→:344`, `takeShared :552→:566`) — one landing, six
cells, every one of them for an edit ABOVE it in the same file.

**(f) The alternative is already this document's own prior art.** §5 records two
rotted rows whose repair was to name the SYMBOL instead — "both are now `sym`
rows rather than line `anchor`s, deliberately: a line anchor on a file this
volatile rots on every edit above it". The `sym` tier records no text and no
number, so it cannot rot from an edit above it at all.

### 7.3 The answer, in the terms the check defines

**No — and not for a division-of-labour reason: a code commit is not competent
to make the claim.** Three parts, each owed by one commit:

1. **The code commit owes the DECLARATION, never the repair.** It knows it moved
   the text; it cannot know whether the doc's CLAIM still stands. In (b) the
   claim did survive — the Finish-then-deferred-Close sequence is still the
   ordinary path, just in a shared body — and nothing in the code says so. And in
   (c) the honest re-derivation was ambiguous: the naive fix (re-run the
   generator) would have re-pointed the row at an unrelated comment line. A
   commit that re-points a row it cannot read the claim for is not repairing the
   corpus, it is guessing at it, and (c) is the measured case where the guess is
   wrong. What it CAN do for free is name the rotted row in its message (or
   better, the doc's prose anchor), so the repair is discoverable from the diff.
2. **The doc's own edit owes the re-anchor, through the corpus writer.** Any
   commit that changes a normative doc's text re-runs that doc's row
   (`classify.py --only <doc>`, Rule 36 — the manifest is regenerated, never
   hand-edited) in the SAME commit. That is `6ffc1f3c3`'s shape: doc prose +
   row cell + nothing else, three files.
3. **The vocabulary owes the durability, and this is where the real fix is.**
   The recurring cost is (e) — the recorded TEXT is drift-resistant but the
   NUMBER it is paired with is not, so every edit above an anchor buys a
   re-anchor. A standing claim whose subject is a DECLARED SYMBOL should be a
   `sym` row ((f)); the anchor tier should be kept for claims whose subject
   really is a line of text (here: the live arm's call site and the doc's
   quotation of the teardown), and those are the rows that will recur.

**The cost of the honest answer, stated so it is not mistaken for a gap**: the
check does NOT block a landing (§3.2, by design) — so the rot is discovered at
merge time, in the run already looking at the branch, and repaired by whoever
picks up the finding. That is the deliberate direction (`a named, actionable
finding plus a landing`), and the alternative — refusing landings over a doc
spot-check — re-creates the park loop this epic exists to remove. Two rows in
five days is the measured rate; if it grows, the lever is part 3 (the tier),
not part 1 (a writer that guesses).
