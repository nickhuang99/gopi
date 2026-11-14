<!-- doc-class: historical -->
# devdoc/merge-branch-selection.md — `gopi merge <branch>`: deciding ONE branch

Class: **normative** (a standing claim about master). Every claim below is held
by a test or by the invocation quoted under *Evidence*.

## What the epic asked and what landed

> "sometimes it is desired to just merge particular branch with high priority,
> so parameter branchname should be allowed like `gopi merge branchname`"

> **Retired mechanism (merge verb removal, 2026-11-01).** The `gopi merge …` forms
> below are the RECORD of the surface as it was when this change landed, kept
> verbatim; the top-level verb was removed afterwards and answers **exit 1** today.
> The family spelling that survives is `gopi task merge`, documented by
> `man/gopi-merge.md`.

The command now takes a branch selection:

```
gopi merge                          one run over every repo in the settings.json "repos" table
gopi merge <branch> [<branch>...]   decide ONLY the named branches, in every repo that holds one
gopi merge --branch <name>          the same selection, for a name that starts with '-'
```

Both spellings parse into the same place (`mergeOptions.branches`, appended by
`mergeAppendBranch`), and the run is otherwise **the same run over fewer
branches** — never a weaker one.

## The four design decisions, and why each one is not the obvious alternative

### 1. A bare positional, not a flag-only surface

The epic's example is `gopi merge branchname`; a flag-only surface would have
made the epic's own invocation a usage error. A name a bare word cannot express
(one starting with `-`) gets `--branch`, and `--branch` is also the only
spelling that can be typed unambiguously in a script.

**The positional used to be REFUSED.** `parseMergeArgs` previously left positionals
in the remainder and `runMerge` rejected any stray word — so "the positional is
now meaning" is a deliberate reversal, and the reversed refusal is pinned by a
test (`TestMergeBranchSelectionRefusesSilentNarrowing` is its successor: what is
refused now is *silent narrowing*, not the word itself).

### 2. The selection is an INTERSECTION with the candidate set, never a replacement

`mergeSweepRepo` still derives its candidates the same way
(`mergeCandidateBranches`: `refs/heads` minus the integration branch, minus
`archive/<name>`-tagged branches, minus branches a worktree holds) and then, if a
selection is present, keeps only the named ones.

The alternative — build the branch list FROM the selection — is the dangerous
one: it would let `gopi merge <a worktree-held branch>` act on a checkout
somebody is standing in, and would resurrect archive-tagged branches that were
deliberately decided out of the queue. Filtering the already-safe list means a
targeted merge inherits every safety property of a full run, including the
restore path. A named branch that a full run would skip is therefore *reported*
the way a full run reports it (nothing to decide), never acted on — deliberately
**not** a refusal, because the branch is right where the operator thinks it is.

### 3. Two refusals, because "0 branches decided" is not an answer

| named | outcome | why |
|---|---|---|
| a name no repo in the table holds | **refused**, exit 1, refs untouched | the alternative prints "0 branches decided" and makes the operator guess between a typo and a queue that was already drained (Rule 15) |
| the integration branch itself | **refused** with its own message | it can never be a candidate, so without a dedicated arm the absent-everywhere refusal above would fire and blame the spelling of a branch that exists |
| a worktree-held / archive-tagged branch | **walked, reported as nothing to decide** | see decision 2 — a real branch this command deliberately never acts on |

The whole-queue arms (`--status`, `-h`/`--help`) refuse to be combined with a
selection: they answer for the whole queue, so a narrowed reading of them would
be a different question answered silently. The arms that narrow *what the
selected branches get* (`--fast`, `--no-verify`, the ceilings) do compose — that
is the ordinary "merge this one branch, cheaply".

### 4. Resolution happens BEFORE the lock, and takes no lock itself

`runMerge` resolves the selection (`mergeResolveBranchSelection`) **before**
`mergeAcquireLock()`. The resolution is a read — three `for-each-ref` listings
per repo, no ref written — and a question must not hold the single-instance
merge lock. The lock is still the run's first *act of work*, which is what
`devdoc/merge-ownership-requirement.md` requires; that doc's table carries the
re-keyed line for this ordering.

An **unreadable** ref listing is deliberately *not* "this repo does not hold the
branch": the two must never look alike (Rule 15). A repo whose listing fails is
walked anyway (its own error arm reports the unreadable refs), and the
absent-everywhere refusal is suppressed for that run — an unreadable question
never becomes a confident "no".

## Verification recipe (reusable)

Unit tests cannot show what this epic is about — "decide THIS and leave my queue
alone" needs real refs. The split that worked:

1. **argv**: table-driven cases over `parseMergeArgs` for both spellings,
   repeats (deduped), blank names, unknown flags, and the whole-queue arms.
2. **the run, on real repositories**: `mergeTestEnv` + `mergeBranch` build a real
   repo with two landable branches; assert the named one landed (master's tip
   moved, ref pruned) and the other is byte-identically where it was.
3. **the surface, run for real** (Rule 31): a sandbox of real repos with real
   branches, a real `settings.json` `repos` table, and the built binary — the
   invocations and their output are quoted verbatim under *Observed* below.

## Observed (the raw runs this doc is held to)

Sandbox: two real repos (`r` with `feat/one` + `feat/two`, and a second repo with
its own branch), each declared in a real `settings.json` `repos` table; the
built binary `bin/gopi` (this branch).

```
$ ./bin/gopi merge --fast feat/one            # refs before: feat/one=d524028 feat/two=fa83419 master=3df1cd2
gopi merge: branch selection feat/one — 1 of 1 repo(s) hold one
  .../r feat/one  landed  fast-forwarded into master, ref pruned      exit=0
                                              # refs after:  feat/two=fa83419 master=d524028
$ ./bin/gopi merge nosuchbranch
gopi merge: no repo in the "repos" table holds nosuchbranch — ...     exit=1, refs unchanged
$ ./bin/gopi merge master
gopi merge: master is the integration branch — it is never a merge candidate   exit=1
$ ./bin/gopi merge feat/three                 # only the repo that holds it was walked
gopi merge: branch selection feat/three — 1 of 1 repo(s) hold one     exit=0
$ ./bin/gopi merge -h feat/x                  # exit=1, stdout 0 bytes (no help page), refusal on stderr
$ ./bin/gopi merge --status feat/x            # exit=1, stdout 0 bytes, refusal on stderr
$ ./bin/gopi merge --status                   # exit=0, the archived-set report as before
$ ./bin/gopi merge --fast                     # default full sweep: BOTH branches decided, no selection line
```

The `--fast` runs are used because they are the deterministic arm (no LLM
hand-over, hence no child process): what the selection *reaches* is identical in
either arm, and the default sweep is shown deciding every branch, which is what
makes the selection observable.

## Not done / honest limits

- The selection is per NAME, not per repo+name: `gopi merge feat/x` decides
  `feat/x` in **every** repo that holds it. A repo-qualified form
  (`gopi merge <repo>:<branch>`) is not implemented and not needed by the epic.
- Priority is expressed by *the operator running it now*, not by a queue: the
  command decides the named branches and stops, so a second invocation is
  required to sweep the rest. That is the "high priority" reading in the epic
  ("just merge particular branch … should be allowed"); a background scheduler
  for named branches is not part of this change.
- Pre-existing red gates in the tree (doc-manifest duplicate rows for three
  devdocs, the `%w` site in `internal/tools/worktree_sweep.go`, two unindexed
  devdocs) fail identically at this branch's merge base — they are not caused by
  and not fixed by this change.
- A SELECTION does not make a non-fast-forward branch cheaper: `gopi merge
  feat/x` on a diverged branch still measures it and hands it to the same merge
  gopi child (measured: such a run was still live after 90 s, and its own child
  report landed the branch at merge `15a6ae97`). That is the epic's "merge this
  branch" working as the ordinary merge does; the `--fast` arm is the
  deterministic, child-free arm and is what the evidence above pins.
