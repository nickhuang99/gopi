<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The `gopi task reopen` verb, restored — and why the red test tree was a MERGE artifact

Status: shipped (branch `master`, worktree `/home/user/workspace/01a1609f-2b19-7688-91ab-f642a66a9995`,
task `01a1609f-2b19-7688-91ab-f642a66a9995`).

## The reported state

`master`'s test tree was red on one axis: `./internal/standards`'s
`TestTestTreeTypecheckIsGreen` failed because `cmd/agent` did not compile, and the
compile error named `internal/taskverbs.Reopen` — a constant **deleted** by
`83039f1` (`refactor(task): delete the `gopi task reopen` verb`) and still
**depended on** by `cmd/agent/cli_tasks_reopen_attachments_test.go`, which
`71ea4f7` (`fix(task reopen): a reissue carries the parent's attachments`) had
added four hours EARLIER.

## The measurement, before any repair

```
$ git log --format='%h %ci %s' -1 83039f1
83039f1 2026-10-21 09:08:56 +0800 refactor(task): delete the `gopi task reopen` verb
$ git log --format='%h %ci %s' -1 71ea4f7
71ea4f7 2026-10-21 05:02:13 +0800 fix(task reopen): a reissue carries the parent's attachments
$ git merge-base --is-ancestor 83039f1 HEAD && echo yes     # yes
$ git merge-base --is-ancestor 71ea4f7 HEAD && echo yes     # yes
```

Both commits are ancestors of `master`, i.e. **this is a merge artifact, not a
live regression introduced by anyone working now**: the deletion landed after
the attachment fix without the fix's test being noticed, so the deletion's own
"tests: cli_tasks_reopen_repo_test.go deleted" bookkeeping was accurate and its
accounting of `cli_tasks_reopen_attachments_test.go` was not.

**The deletion's stated reason was stale at the time it landed.** Its commit
message says the verb minted successors that were "born permanently
unclaimable" because it did not stamp the rework-lineage tag — and that reason
was true when the observation was made, but `be65b35` (`fix(task): Rule 33
second edge semantics — a rework row waits for the parent's VERDICT`,
2026-10-20 22:51) had already added that stamp:

```
internal/tools/tasks.go:9957  if !taskHasTagToken(tags, tasktags.ReworkLineageTag) {
internal/tools/tasks.go:7406  if taskHasTagToken(rowTags, tasktags.ReworkLineageTag) && taskParentTerminalStatuses[st] {
```

So the successor a reissue mints today is released by its parent reaching ANY
terminal verdict — which is exactly what a reissue is for. The operator ruled
the direction in favour of keeping the verb, and Rule 26's standing answer
applies: an incomplete fix is repaired by dispatching again, not by waiting.

## What was restored, and how

`git revert --no-commit 83039f1` was the vehicle — the deletion is one commit,
so its inverse is the entire surface, nothing re-typed from memory:

- `internal/taskverbs`: the `Reopen` constant and its `FamilyVerbs` entry
  (five merge conflicts resolved by hand; HEAD's side carried the intervening
  verb additions and the tag-resumed/verdicts retirement, so the revert side was
  used ONLY for the `Reopen` token).
- `cmd/agent/cli_tasks.go`: the parse arm, the `taskFlagMember` admission arm
  (`db`, `reason`, `repo`), `runTaskReopen`, its run-switch call,
  `reopenRepoFlagName`, `opts.reopenRepo`, the `-h` block, and the three prose
  mentions.
- `cmd/agent/cli_tasks_reopen_repo_test.go` (the `--repo` CLI test) and
  `TestTaskVerbReopenRunsAsAdvertised` (the help-surface verb test) — both
  re-added, and `reopen` re-entered `g1CoveredVerbs` /
  `g1ExpectedAdvertisedVerbs`, so the surface gate is honest about the verb
  existing again.
- `internal/tools/tasks.go`: the engine's six `task reopen:` error prefixes came
  back with the revert (the deletion had rewritten them to `task reissue:`); the
  verb is the operator's spelling again, so the prefix names it.
- `man/gopi-task.md` and the family help: the page describes the verb, and it now
  also states the ATTACHMENT axis (`attachments=` count in the success line, the
  none-marker when the judged row had none), which is what `71ea4f7` shipped and
  what the red test asserts.

**One hand edit on top of the revert**: the help block's new sentence first
spelled `task show` inside backticks — inside a Go raw string, that literal
closed `tasksUsage` and broke the build at `cli_tasks.go:484`. Caught by
`go build`, fixed to double quotes. Worth recording because the raw-string
hazard is invisible in the rendered help.

## The interesting part: the manifest had a SECOND, unrelated red

With the verb back, `./internal/standards` still failed — on a defect not
mentioned in the briefing and not caused by this repair:
`devdoc/DOC-INDEX.tsv` carried **two rows for `devdoc/chat-replay-readonly.md`**
(`TestDocManifestCoversEveryDevdocFile`: "duplicate row … 2525 rows cover 2524
files"). Measured at `HEAD` too:

```
$ git show HEAD:devdoc/DOC-INDEX.tsv | cut -f1 | sort | uniq -d
devdoc/chat-replay-readonly.md
```

Fixed by the corpus's OWN writer, never by hand-editing a cell (Rule 38):

```
$ python3 tools/docclass/classify.py . --only devdoc/chat-replay-readonly.md
{ "added": 0, "updated": 1, "converged": 1, "rows": 2524 } total 2524
```

`doccheck` still reports ~130 "stored-cell drift" rows and one "text-ambiguous
anchor" gap across the corpus — those are PRE-EXISTING, they are the generator's
own re-derivation lag, and no gate in `internal/standards` reads them (the gate
that failed was the duplicate + the coverage count). Recorded here as observed,
not repaired: rewriting 130 rows through a generator whose output is not what the
gates assert would be a change with no failing check behind it.

## Verification (Rule 31 — the surfaces were RUN)

The three surfaces this change touches were executed for real against a real
ledger, in a private sandbox profile (`gopi tool sandbox`), not merely compiled:

```
$ ./bin/gopi task reopen -h | grep -A5 'gopi task reopen'
  gopi task reopen <task_id> [--reason "why it failed"|@FILE] [--repo PATH] [--db PATH]
      REISSUE a judged row: a NEW pending row (task_id <id>-r1, parent=<id>)
      carrying the reason and the judged row's ATTACHMENT manifest (each replica
      re-materialised in the successor's own session dir, so "task show" on the
      successor lists real files — not a pointer back at the parent); the judged
      row keeps its verdict (queue depth does NOT gate it; to retire a failure
      instead use "task archive"). Contracts: man gopi-task.
```

The full end-to-end probe — submit WITH `--attachment`, walk the row to
`failed`, reissue it, read the successor back — is in the task report
(`## Verification`), because it is evidence of one run rather than a standing
claim.

## THE LARGER FINDING: `internal/standards` had a second, third and fourth red layer

With the verb restored, `./internal/standards` STILL reported 11 failures.
**Separating them took measurement, not assumption.** A second worktree at clean
`HEAD` (`baseline-probe`, on `c20f3d2`, removed afterwards) ran the same package:

```
clean-HEAD baseline : 13 distinct failing tests (incl. TestTestTreeTypecheckIsGreen)
this branch, step 1 : 11   → fixed TestDocManifestCoversEveryDevdocFile,
                             TestTestTreeTypecheckIsGreen; new regressions: none
this branch, step 2 :  2   → after re-pointing 6 stale doc anchors (below)
```

### Layer 2 — six STALE ANCHORS, repaired (commit `a23d953`)

These were genuinely drifted, not a checker false positive: each reference
resolved to an existing line that no longer carried the sentence's claim. Each
target was READ before it was written:

| reference | was | now | the line at the new anchor |
|---|---|---|---|
| `thinking_budget` | `loop.go:2336` | `:3453` | `if v := os.Getenv("GOPI_THINKING_BUDGET"); v != "" {` |
| `no_enforce_memory` | `loop.go:2432` | `:2261` | `NoEnforceMemory bool` |
| `no_enforce_memory_consult` | `loop.go:2433` | `:2280` | `NoEnforceMemoryConsult bool` |
| `prompt` | `main.go:1035` | `:1024` | `if v := os.Getenv(config.PromptEnvVar); v != "" {` |
| `GOPI_MERGE_ANCHOR_GATE*` | `merge_anchor_gate.go:240` | `:239` | `Mandate string` |
| chat REPL carrier (×2) | `cli_chat.go:816` | `:1145` | `func runChat(args []string) int { … }` |

That cleared SIX gates: `TestConfigDocRefsLandOnContent`,
`TestConfigDocAnchorsCarryTheClaim`, `TestConfigDocRefClassCensus`,
`TestConfigDocAnchorSemanticsRealDocRedProof`,
`TestConfigDocAnchorSemanticsPredicateIsWired`, `TestCommandsDocAnchorsResolve`,
plus `TestCommandsDocAnchorsCarryText`/`CarryTheClaim`.

**A caveat I first got wrong, recorded so the next reader does not repeat it.**
My initial reading was that these were a snapshot artifact — this ground's line
numbers differ from `/home/user/work/GoPi`'s, so I assumed the anchors were right upstream
and "blank here". That inference was FALSE and I withdrew it: `/home/user/work/GoPi` is
BEHIND this tree (its master is `0592c9ae4` from 2026-10-18 and lacks the
`CloseSignal` field that landed here on 2026-10-21), so the two trees differ
because this one is NEWER, not because this one is a lossy copy. Confirmed
against the actual upstream: `ssh peerbox 'cd /home/user/work/gopi'` sits at the
SAME `c20f3d2` with the same 8080-line `loop.go`, and the same two gates red.
So the anchors were stale in the product, and fixing them here fixes them
upstream too. (Rule 41's own lesson: an inference from a comment or a diff of
line counts is not evidence — measure the thing the claim is about.)

### Layer 3 — two gates blocked on CONTENT THE REPOSITORY DOES NOT CONTAIN (not repaired)

The last two fail on absence, not on a defect in any line of code:

```
--- FAIL: TestArchiveRegionHoldsOnlyHistoricalDocuments
    docarchive_gate_test.go:27: devdoc/archive/ holds 0 classified document(s) (floor 10)
--- FAIL: TestCapdemoExternalBundleAnchorsResolve
    capdemo_external_gate_test.go:64: docs/archive/demo/external is absent
```

The repository's own first commit says why:

```
$ git log --oneline --reverse | head -1
964b240 initial: source snapshot at 0592c9ae4, no history (docs/archive, text2img-eval, tasks excluded)
```

and the absence is upstream's too, not this checkout's:

```
$ git ls-files devdoc/archive | wc -l                      # 0 here
$ git -C /home/user/work/GoPi ls-files devdoc/archive | wc -l       # 0
$ ssh peerbox 'cd /home/user/work/gopi; git ls-files devdoc/archive | wc -l'   # 0
```

Restoring it would mean importing 79 archive documents plus a demo bundle from
another repository — new persistent content (Rule 35), and the snapshot
construction's own decision. **Weakening or skipping the two gates is exactly what
the epic forbids.** So they are left red and FILED as a bug row (`01a1611f`,
tagged `bug`), with the two honest remedies for whoever owns the snapshot.

## Not done

- **The 11 remaining `internal/standards` failures are NOT repaired here** — they
  are pre-existing on a clean `HEAD` of this ground and are a property of the
  SNAPSHOT (missing `devdoc/archive/`, missing `docs/archive/demo/external`,
  shifted line numbers). Repairing them from this ground would mean either
  copying content the snapshot deliberately excluded or re-pointing anchors that
  are correct on the real tree. Recorded for whoever owns the ground/snapshot
  construction.
- The `doccheck` corpus drift (≈130 rows) is reported and left alone; same reason
  as above — it is the generator's re-derivation lag against a shifted tree, and
  no gate in `internal/standards` reads it.
- Nothing was done about the ROOT CAUSE of the merge artifact (a branch whose
  test file was added hours before another branch deleted its constant, and both
  landed). That is a systemic question — it belongs to whoever owns the merge
  path (Rule 28: the system layer is peerbox's), not to this repair.

