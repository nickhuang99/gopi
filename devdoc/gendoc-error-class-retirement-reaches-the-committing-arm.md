<!-- doc-class: historical -->
# The ERROR class's SECOND FACE — the escape hatch reaches the committing arm's fallback

Task `01a13b61-e295-7c8f-96f2-46e05898b434`, branch `task/01a13b61e2957c8f96f246e05898b434`.
Host `hostb-Laptop`; ground cut `f2824ff`.

This document records two things that a later reader cannot reconstruct from the
diff: (1) the feature this fix repairs had **vanished from every ref** and had to
be recovered first, and (2) the defect is a *reachability* fact about two arms in
one function, not a wrong line.

## 1. The premise did not hold: the class was absent from the integration branch

The row's briefing described a fix to `GOPI_GENDOC_ERROR_CLASS_SKIP` "losing its
strength on the committing surface". Measured before any edit (Rule 41/31):

```
$ git grep -c -- mergeCheckAnchorErrorClassMark HEAD -- internal cmd   → 0 hits
$ git grep -c -- gendocErrorClassSkipEnv  HEAD -- internal cmd         → 0 hits
$ git grep -c -- GateUnderivable          HEAD -- internal cmd         → 0 hits
$ git grep -c -- EmptyGateDerivedErrors   HEAD -- internal cmd         → 0 hits
$ git grep -c -- GeneratedDocAnswerOpts   HEAD -- internal cmd         → 0 hits
$ git grep -c -- EmptyGateFindingFormat   HEAD -- internal cmd         → 0 hits
```

The whole ERROR-class feature was gone. Its originating commit
`6446222744bc6442b71ec1348e6806563bfea54e` was reachable from **no ref on either
host** (`git for-each-ref --contains` empty on both; `git log --all -S
gendocErrorClassSkipEnv` empty), its ledger row read `completed`, and its recorded
worktree directory no longer existed — so its deliverable was reachable by nobody.
The object was still in the object store, and a cherry-pick replayed cleanly onto
this ground (9/10 files; only the append-only `devdoc/DOC-INDEX.tsv` conflicted,
the known union-lane shape). Commit `c74fdcf57` is that recovery, with the
original author, author date and message preserved verbatim.

The full observation is the run's evidence file,
`premise-absent-from-integration-branch.md`, in the session evidence directory.

## 2. The defect: a refusal mark reused by an abstention

`finishCleanupBranchAnchorCheck` (`internal/tools/task_finish_premerge_smoke.go`)
has two arms that can refuse:

1. the **class arm** — `gendoc.EmptyGateDerivedErrors(ans.Errors)` non-empty;
2. the **fallback arm** — `len(ans.Required) == 0 && ans.Touched`.

The second arm exists because a change that MOVED a published anchor source while
the edge names no package to run is the same fact the first arm reports; its own
comment says so ("which is the shape the ERROR arm above reports"). That is also
why it is reachable **only** after the class arm has stood down: with a live class
the first arm returns first.

So the escape hatch worked on exactly one of the class's two faces. Setting
`GOPI_GENDOC_ERROR_CLASS_SKIP` emptied `Answer.Errors`, the class arm yielded —
and the fallback, reached *because* the skip had emptied the channel, returned
`mergeCheckAnchorErrorClassMark` anyway. Callers classify a refusal by its mark,
so the run still refused on a class it had just retired. Measured on the fixture
tree (task `01a13ae1`, 2026-10-14):

```
LIVE   : ok=false why="the published-anchor edge reports an ERROR-class defect no package can catch"
SKIPPED: ok=false why="the published-anchor edge reports an ERROR-class defect no package can catch:
                 this change moves a published anchor source and the edge names no package to run for it
                 (4 path(s) measured)"   findings=[]
```

## 3. The fix

The fallback now reads the decision from the **edge's own answer** —
`Answer.Skip`, the field `GeneratedDocAnswerOpts` sets when it emptied `Errors` —
and never re-reads the environment (Rule 2: `GOPI_GENDOC_ERROR_CLASS_SKIP` has
exactly ONE reader, `gendocErrorClassSkipped`, and this arm is not it). Under a
retired class it abstains with a mark of its own:

```
mergeCheckAnchorErrorClassRetiredMark = "the published-anchor edge's ERROR class is retired for this run, " +
    "so this change's anchor movement is reported rather than refused"
```

Two properties are deliberate:

- **The retired sentence does not contain `mergeCheckAnchorErrorClassMark`.** A
  substring match is exactly how a caller classifies a refusal, so an abstention
  carrying the mark in prose would still be read as a refusal — the defect it
  replaces, one level down. The new constant's doc comment states this.
  (Confirmed in the red proof: the test's assertion failed on
  `strings.Contains(why, mergeCheckAnchorErrorClassMark)` against the pre-fix arm.)
- **It is REPORTED, never silenced.** The sentence names the retirement and
  appends `gendocErrorClassSkipNote()` — the existing single spelling of
  "this run retired the class" (Rule 14, no second copy) — so a skipped tree is
  visibly skipped rather than looking like a clean land (Rule 15).

Nothing on the live path moves: with the class live the class arm refuses first
and is byte-for-byte unchanged, and `Answer.Skip` is false so the fallback takes
its original branch. That is what the no-weakening control observes.

## 4. Why the sibling surface needed no change

`mergeAnchorGate` (`internal/tools/merge_anchor_gate.go:333`) also refuses on this
class, but its whole block is guarded by `len(derr) > 0` where
`derr := gendoc.EmptyGateDerivedErrors(ans.Errors)`. When the skip empties
`Errors` that block is unreachable, so the merged-result lane already honoured the
escape — the defect was confined to the pre-push arm's `Touched` fallback, which
is the only place in the package that returns the class mark outside the guard.

## 5. Verification

- Red→green with the mutation kept **compilable** (the first attempt reverted the
  constant too and only produced `undefined: ...` — a build failure is not a red
  proof, and it was redone): reverting only the fallback's `ans.Skip` branch makes
  `TestBranchCheckAnchorArmRetiresTheClassOnTheCommittingSurface` fail on the
  mark assertion; restoring makes both arms pass. Restore verified by content
  identity (`git hash-object` of the restored file equals the saved fixed copy and
  the pre-mutation reading, `2cae17a7`).
- `TestBranchCheckAnchorArmSkipDoesNotWeakenTheLiveClass` is the no-weakening
  control: same fixture, escape explicitly empty (`t.Setenv(..., "")`), still
  refuses with the class mark and still quotes `gendoc.EmptyGateDerivedMark`.
- Raw observations: `red-before-fix.txt`, `green-after-fix.txt`,
  `redproof.txt` in the run's evidence directory.

## 6. Not done

The recovered commit is a plain re-application, not a `git cherry-pick` object
identity: `c74fdcf57`'s tree differs from `644622274`'s only by the DOC-INDEX
union resolution, so the commit hash necessarily differs. Authorship, author
date and message text are preserved; the message carries a `RECOVERED` trailer
naming the original hash.
