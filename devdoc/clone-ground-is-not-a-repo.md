<!-- doc-class: normative -->
# The clone channel's ground is not a repo — the clone-ground arm of the durable-repo resolution

**Epic:** `01a160a2-3bae-7503-990b-0e7b02857231` · **Measured rows:** `01a1604a`, `01a15b96`, `01a16053`

## The defect, and why the existing fix could not see it

`DurableRepoForSubmit` (epic `01a0d55b`) already answered "which repository outlives
this run" — for the WORKTREE channel. Its whole resolution is
`mergeExecMainRepo` → `git rev-parse --path-format=absolute --git-common-dir`:
a linked worktree answers with the checkout that owns it, a main checkout is identity.

In the CLONE channel a worker's ground is not a linked worktree. It is a plain
`git clone` at `<workspace_root>/<task-id>` (`internal/devworkspace.Open`), whose
`.git` is a **directory**. Measured on the fixture:

```
$ git -C /home/user/.gopi/sessions/.../workspace/01a1604a-... rev-parse --git-dir
.git
$ git -C /home/user/.gopi/sessions/.../workspace/01a1604a-... rev-parse --path-format=absolute --git-common-dir
/home/user/.gopi/sessions/.../workspace/01a1604a-.../ .git      ← the workspace itself
```

So `mergeExecMainRepo` returned the workspace path verbatim — correctly concluding
"this is already a main checkout" — and every row submitted from inside such a run
was born naming the directory the run's own end deletes:

```
deliverable.repo: /home/user/workspace/01a16033-c069-72fe-8b4e-d15d706f41ac
reason: clone channel: repo "…/01a16033-c069-…" is not usable: the directory no longer
        exists on disk — refusing to push a workspace that cannot be built
runs:   none (no run log in the directory)
```

The refusal is CORRECT. The row is stranded, not broken: the directory it names
will never exist again. Three measured rows, all children of a run in that workspace.

## The fix: `cloneGroundSourceRepo` (`internal/tools/task_submit_clone_ground.go`)

`DurableRepoForSubmitWithNote` now asks a new arm BEFORE handing the value back:

1. **Boundary** — `config.WorkspaceRootConfigured()` (the ONE root reader) plus
   `worktreeroots.Within`. A path outside the root is untouched: no stat, no git
   call, no behaviour change for an ordinary repository or an ordinary clone.
2. **Model** — `devworkspace.ResolveGroundState(dir)` (the ONE ground-model
   resolver). Only `GroundModelClone` (`Alive && .git is a DIRECTORY`) is this
   arm's business; a LINKED worktree under the same root keeps its existing answer
   (the main checkout that registers it).
3. **Provenance** — `git remote get-url origin` + `devworkspace.SourceRepoDisplayPath`
   (the existing renderer of "the local checkout path an origin URL names").
4. **Verification** — `taskRepoVerifiable(source)`. If the origin cannot be turned
   into a LIVE git work tree the arm **refuses**, naming the workspace, the root,
   the reason and the repair verb. It never falls back to the doomed path
   (that fallback IS the defect) and never guesses a repository.

The substitution is reported in the string the submitter actually reads — the
`task_submit` tool result / the CLI's confirmation, through a new
`TaskSubmitOptions.RepoNote` field — not only on stderr, because the model is the
one that must see that its row names a different repository than it typed.

## The three limits, each a decision

| limit | why |
|---|---|
| outside the workspace root → untouched | a normal submit must cost nothing and its log must be byte-identical |
| linked worktree under the root → existing answer | that question already had one correct answer (the main checkout); this arm adds a case, it does not re-decide it |
| unnameable origin → REFUSE | the only other value is the path about to be deleted; a row that never starts is worse than a refused submit (Rule 15) |

## Regression guard

`internal/tools/task_submit_clone_ground_test.go`, four independent readings:
the arm, the submit seam (the ledger's `deliverable.repo` + the confirmation's
note), the refusal, and the boundary (outside-the-root identity, linked-worktree
unchanged). The fixture proves the OLD resolution answers identity for the same
directory first, so the test cannot pass for the wrong reason.

Red proof (fix removed via a `false &&` in front of the new arm):

```
--- FAIL: TestDurableRepoForSubmitResolvesACloneGroundToItsSource
    a clone-channel workspace must resolve to the repository it was cloned from (…/source);
    got …/workspace/01a16033-c069-…
--- FAIL: TestTaskSubmitFromACloneGroundStampsTheSourceRepo
--- FAIL: TestTaskSubmitRefusesACloneGroundWhoseOriginIsUnnameable
FAIL	github.com/the operator/gopi/internal/tools	1.272s
```

Restore verified against a revision, not the index:
`git diff HEAD --stat internal/tools/tasks.go` printed nothing.

## Historical rows

`01a1604a`, `01a15b96`, `01a16053` already carry `deliverable.repo: /home/user/work/gopi`
(re-pointed by earlier runs); each is `failed` with `runs: none` — it never started,
so nothing is recoverable and nothing is lost. They stay as the measured evidence;
the fix prevents the class, it does not rewrite history. Re-issue them only if the
work itself still matters — that is a queue decision, not a submit-seam one.
