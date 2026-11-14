<!-- doc-class: normative -->
# The ground's shape is MEASURED, never assumed — two sentences, one briefing, two answers

**Row:** `01a156d8-c7bf-739a-9308-f8f7e74513a4-r1`
**Mechanism it closes:** PROJECT.md Rule 6c + Rule 6a and the spawn seam's ground
announcement said "linked worktree / NO push is needed" for a ground whose shape
nobody had measured.

## What was measured

The runner creates **two different ground shapes** depending on the channel, and
the discriminator is the KIND of the `.git` entry.

### The clone channel (this row's own ground)

```
$ pwd
/home/user/workspace/01a156d8-c7bf-739a-9308-f8f7e74513a4-r1
$ test -d .git && echo "DIRECTORY (clone)"
DIRECTORY (clone)
$ ls -ld .git
drwxrwxr-x 8 nick nick 4096 Oct 27 03:57 .git
$ git rev-parse --git-common-dir
.git
$ git rev-parse --git-dir
.git
$ git remote -v
origin  peer1:/home/user/work/gopi (fetch)
origin  peer1:/home/user/work/gopi (push)
$ git rev-parse --abbrev-ref HEAD
master
$ git -C /home/user/work/gopi worktree list
/home/user/work/gopi  1d4a419a8 [master]        # ONE entry: this clone is registered NOWHERE
```

### A linked worktree (real, on this host)

```
$ W=/home/user/workspace/gopi-01a13019-teeth2
$ test -d .git && echo TRUE || echo "FALSE — a FILE, i.e. the LINKED-worktree shape"
FALSE — a FILE, i.e. the LINKED-worktree shape
$ test -f .git
TRUE
$ cat .git
gitdir: /home/user/work/GoPi/.git/worktrees/gopi-01a13019-teeth2
$ git rev-parse --git-common-dir
/home/user/work/GoPi/.git
$ git rev-parse --git-dir
/home/user/work/GoPi/.git/worktrees/gopi-01a13019-teeth2
$ git -C /home/user/work/GoPi worktree list --porcelain | grep -c "^worktree $W$"
1                                               # registered — alive only while listed
```

The two shapes answer the same question oppositely:
`--git-common-dir` prints `.git` (its own repo) for a clone, and the MAIN
checkout's `.git` for a linked worktree.

## The code path that creates each

| channel | creation code | shape it leaves |
|---|---|---|
| clone channel (`dev_repo` set) | `internal/devworkspace/devworkspace.go:269` `Open` → `:299` `run(…, gitBinary, "clone", cfg.DevRepo, dir)`; switched in at `cmd/agent/cli_tasks.go:2732` (`if cloneRun != nil` replaces the worktree resolution), opened at `cmd/agent/devworkspace_channel.go:123` | **CLONE** (`.git` a DIRECTORY) |
| worktree channel (default) | `internal/tools/task_run_ground.go:957` `args := []string{finishCleanupWorktreeVerb, taskGroundWorktreeAddVerb, dir}` → `git worktree add … -b <branch>`; resolved at `internal/tools/task_run_ground.go:285` `TaskRunGroundFor` | **LINKED WORKTREE** (`.git` a FILE) |

## The defect this closed (measured, not hypothetical)

This row's ground is a CLONE, and its own briefing carried **both** sentences in
one message:

```
[Your worktree — ALREADY CREATED for you]
it is a linked worktree of your task's repo, checked out on branch master.      ← FALSE here
[Publishing your work — the delivery loop does it, not you]
This ground is a CLONE standing on branch master.                              ← TRUE, adjacent
```

The shape sentence was a typed literal rendered unconditionally
(`taskGroundAnnouncementFmt`), while the publishing clause was rendered
`if cloneRun != nil`. Neither sentence was a lie about its own code; the
disagreement is what a run reads. The delivery consequence is opposite — a
clone's commit is owed by the channel, a linked worktree's is already in the
shared repo — so the wrong sentence tells a run its **undelivered** commit is
safe, which is exactly the loss the epic exists to prevent.

## What changed

1. **`internal/devworkspace/ground.go`** — the ONE discriminator, exported:
   `GroundGitEntryName = ".git"` (the entry's single spelling) and
   `GroundModelOf(dir) GroundModel` (the CHEAP half of `ResolveGroundState`: the
   model alone, no liveness). Returns `GroundModelClone` for a DIRECTORY,
   `GroundModelLinkedWorktree` for a FILE, `GroundModelMissing` when there is no
   entry — it never guesses between the two claims.
2. **`internal/tools/task_run_ground.go`** — the announcement's shape sentence is
   now RENDERED FROM the measurement (`taskGroundShapeSentence` /
   `GroundShapeSentence`), with one template per shape and each carrying its own
   delivery consequence. An unmeasurable ground claims **neither** answer. The
   label became shape-neutral (`[Your ground — ALREADY CREATED for you]`): it is
   the first thing a model reads, and a clone ground is not a worktree.
3. **`internal/tools/task_delivery_proof.go`** and
   **`internal/tools/git_publish_target_guard.go`** — both now ask
   `devworkspace.GroundModelOf` instead of re-statting the entry through their own
   local `gitPublishGroundGitDirName` literal (which is deleted). The reader that
   decides whether a push is OWED and the prose that TELLS a run who owes it are
   now one answer, not two implementations of one rule.
4. **`PROJECT.md` Rule 6a + 6c**, mirrored in `devdoc/rules-reference.md` — the
   "NO push is needed" sentence is scoped to a **measured linked worktree** and
   names the discriminator; the shape test is no longer spelled as a bare
   `test -d .git` a reader has to interpret, and Rule 6c names
   `devworkspace.GroundModelOf` as the one definition.
5. **`internal/tools/task_ground_shape_clause_test.go`** — on REAL git fixtures
   (a repo, a linked worktree of it, and a clone of it), three arms: clone ⇒
   "NOT already on the server" and never "no push needed"; linked ⇒ "NO push is
   needed"; unmeasurable ⇒ neither claim; plus an arm proving the announcement
   carries the measured sentence and an arm proving the discriminator's two
   consumers agree.

## The regression test, and its red proof

`go test ./internal/tools/ -run 'TestGroundShape|TestGroundAnnouncementClauseCarries'`
is green; disabling the discriminator (making `GroundModelOf` answer
`linked-worktree` for every directory — the sentence that shipped) turns three
tests RED, naming the clone fixture and printing the wrong announcement. The
restore is verified against `HEAD` by blob comparison, not by an index-relative
diff:

```
$ test "$(git hash-object internal/devworkspace/ground.go)" != "$(git rev-parse HEAD:internal/devworkspace/ground.go)"
(file differs from HEAD ⇒ my change is present)
$ grep -c "RED-PROOF MUTATION" internal/devworkspace/ground.go
0
```

## Why no new dependency (Rule 35)

No cron line, no script, no state file, no config key, no resident process, no
new tool. `GroundModelOf` is a function in a package that already existed; the
callers that were re-statting `.git` now call it. Nothing new exists after the
process exits.

## What this does NOT claim

- It does not make a run push, and it does not weaken
  `git_publish_target_guard.go` — the guard's refusals are unchanged (the same
  tests pass), only its SHAPE INPUT moved to the shared discriminator.
- It does not reword the other places in the tree that say "no push is needed"
  (`devdoc/*.md` historical records). Those are records of runs that measured
  their own shape; rewriting a record destroys it (Rule 38's narrative/census
  split). The live claims — the rule, the briefing, the two code readers — are
  the ones that had to become machine-true.
