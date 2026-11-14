<!-- doc-class: normative -->

# The clone-ground arm's boundary is a SHAPE, not a containment test

Task `01a16218`. Measured on master `ebbecc7`, 2026-10-22.

> **THE SPELLINGS BELOW ARE HISTORIC (2026-10-30).** `gopi task reopen` and its
> `--repo` arm were RETIRED when the whole verb was deleted, together with the
> CLI test that carried them; the same repo axis is now reached through
> `gopi task clone --repo PATH` (`cmd/agent/cli_tasks.go`, `internal/tools/task_clone.go`)
> and pinned by the clone suite (`internal/tools/task_clone_test.go`). The
> MEASUREMENT below — the guard was wrong, not the fixture — is unchanged and
> still governs the axis wherever it lives. Read the historic verb's
> repo-carries-a-repo arm as the clone verb's equivalent arm; nothing else in
> this page depends on the verb's name.

## The red

```
$ go test ./cmd/agent/ -run 'TestTaskVerbReopenCarriesARepo' -count=1
--- FAIL: TestTaskVerbReopenCarriesARepo (2.36s)
    cli_tasks_reopen_repo_test.go:48: gopi task reopen … --repo /home/user/workspace/gopi-test-scratch/queue/queue-device-cli-reopen-repo-3615867/override-main: exit 1 (want 0)
        gopi task reopen: task reopen: --repo: no durable repository for …/override-main: it is the clone
        channel's workspace for one run (under the workspace root "/home/user/workspace"), so it cannot be
        the row's deliverable.repo — the checkout declares no readable origin remote: …
```

## Which side was wrong: the GUARD

The test's fixture is legitimate. Arm (b) builds a throwaway repo with `git init`
(no origin) under the test suite's own scratch home and expects
`task reopen --repo <it>` to succeed — that is what an operator pointing a rework
at a real checkout does.

The guard (`cloneGroundSourceRepo`, `internal/tools/task_submit_clone_ground.go`)
decided "is this the clone channel's per-run workspace?" with
`worktreeroots.Within(dir, root)` — **anything at or below the root**. The
workspace root is not a private namespace: by default it IS `<home>/workspace`
(`worktreeroots.DefaultRoot`), which is where checkouts, an operator's scratch
repositories and this host's test scratch home
(`/home/user/workspace/gopi-test-scratch`) all live. So the fixture was
announced as a workspace no run had ever created, and refused.

## The channel's own invariant

`devworkspace.Dir(root, taskID)` is `filepath.Join(root, taskID)` — the root,
**one** task-id component, nothing else. The opener, the reclaimer and the ledger
stamp all go through it, so it is the one definition of the shape. A workspace
is therefore a DIRECT child of the root; anything deeper cannot be one.

## The fix

`cloneWorkspaceDirShape(dir, root)` replaces the bare containment test: under the
root, not the root itself, and `filepath.Dir(dir) == root`.

It is deliberately NOT "the basename looks like a task id". An id-shape predicate
would re-decide the naming rule in a second place and would still mis-fire on an
operator's own `<root>/<uuid>` checkout. Depth is the channel's invariant; the
`devworkspace.ResolveGroundState` model check that follows supplies the rest.

No escape hatch, no new knob, no new dependency: one predicate at one place,
narrower in exactly the direction the measurement demanded.

## What still fires, and is pinned

- a real clone workspace (`<root>/<task-id>`, clone model, with an origin) →
  substituted with its source repository, with the one-line note;
- the same with NO origin → still REFUSED (the refusal arm is intact; the
  regression test asserts the nested case takes the IDENTITY arm, and the red
  proof below shows the difference is the predicate);
- a repository OUTSIDE the root → identity, no git call;
- a LINKED worktree under the root → its main checkout, unchanged.

## Red proof

`TestDurableRepoForSubmitLeavesOtherReposAlone` arm (c) is the regression guard.
Reverting ONLY the production file (keeping the test) fails it:

```
--- FAIL: TestDurableRepoForSubmitLeavesOtherReposAlone (0.05s)
    task_submit_clone_ground_test.go:212: a repository nested below the root must be IDENTITY with no note; got (, "")
```

Restored → PASS. Real-surface check (Rule 31), driven through the built binary in
a sandbox: a nested clone-model repo is queued with
`deliverable.repo` = itself, while a direct child of the root is still refused
with the workspace sentence.
