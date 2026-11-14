<!-- doc-class: historical -->
# The post-merge gate red at 5de18bb6 — Rule 18 in `chat_write_guard.go`, and the Rule 16 "escape" that was never red

Task `01a10adb-a29b-730d-8c1c-c0c4a5e9e82b`, branch `task/01a10adba29b730d8c1cc0c4a5e9e82b`.

## What was reported, and what was measured

The post-merge gate re-run of `/home/user/work/GoPi` at `5de18bb6` reported
`go test -count 1 ./internal/standards` RED with **two** findings:

1. **Rule 18** (no `else` after a terminating `if`) — 1 NEW unannotated hit at
   `internal/tools/chat_write_guard.go:839`.
2. **Rule 16** (`fmt.Errorf` must wrap with `%w`) — an escape at
   `internal/tools/worktree_sweep.go:2431`.

Re-running the gate **on the landing commit itself** (pristine, no local edits —
`git status --porcelain` empty, `HEAD == 5de18bb6`) separates the two:

```
style-gate (Rule 18):  ... NEW unannotated hits: 1   internal/tools/chat_write_guard.go:839
error-wrap (Rule 16):  hits=4 baselined=3 escaped=1 new=0
```

- **Rule 18 was the only real red.** One hit, unannotated, at the named line.
- **Rule 16 was never red.** Its line reads `escaped=1 new=0` — the gate's own
  vocabulary, where an *escaped* hit is a **PASS**. The finding conflated two
  opposite outcomes: `new=1` (red) with `escaped=1` (green, correctly annotated).
  The trigger was almost certainly a `grep -c no-wrap` over the gate's output: it
  matches a `%w` violation line **and** a compliant escape line about equally, so
  it reports a red that the gate did not.

That distinction is the whole reason this task is one edit and not two. Fixing
Rule 16 would have meant editing code that the gate already accepts — churn on a
green surface, and (worse) deleting a reviewed escape whose reason is true.

### Why the escape's reason is true (verified, not assumed)

`worktreeDropStaleLock` runs `git worktree unlock <repo> <worktree>` and then asks
whether the registration is still locked. When the worktree directory is **gone**,
the verb has nothing to unlock and exits non-zero — that is the expected outcome,
not a cause to unwrap. Measured:

```
$ git worktree unlock <gone-directory>
fatal: '<path>' is not a locked working tree
$ echo $?
128
```

So `%v` for `unlockErr` is correct: it is evidence the git verb no longer applies,
and the failure worth unwrapping is the lock-file one, which the same statement
**does** wrap with `%w`. The contiguous comment block above the statement
(lines 2427–2430, the paragraph immediately adjacent to line 2431) carries the
`// no-wrap:` marker the gate looks for.

## The Rule 18 fix — statement scoping, not a real `else`

The offending shape was:

```go
if staged, ok := chatStagedPaths(root); !ok {
    return chatGitInvocationVerdict{shape: chatGitIndexUnreadableSubject}
} else if bad, found := chatFirstDisallowedStagedPath(staged); found {
    return chatGitInvocationVerdict{path: bad}
}
```

The `else` existed **only because `staged` was scoped to the `if` statement** —
without hoisting it, the second branch had no way to read the value. That is the
usual cause of this gate hit, and the fix is the shape the gate prescribes:
hoist the binding, drop the `else`, guard-clause the two branches apart.

```go
staged, ok := chatStagedPaths(root)
if !ok {
    return chatGitInvocationVerdict{shape: chatGitIndexUnreadableSubject}
}
if bad, found := chatFirstDisallowedStagedPath(staged); found {
    return chatGitInvocationVerdict{path: bad}
}
```

No behavioural change: the same two conditions, the same order, the same returns —
only the scope of `staged` and the removal of the `else`. An `// else-ok:`
annotation was available as the alternative exit; it was not used, because the
de-indented form is in every respect the better code and the annotation would have
permanently frozen a shape that had no reason to exist.

**No baseline permit was added.** This branch does not touch
`internal/standards/baseline.go` at all:

```
$ git diff master HEAD --stat -- internal/standards/baseline.go
(empty)
```

(The allow-list did move *between* `5de18bb6` and master — commit `1fed197f`
"…%w at docclass refusal + stale permit" — i.e. it ratcheted DOWN by one row. That
is somebody else's landed work, already on master when this branch was cut; this
branch neither adds nor removes a permit.) Rule 13/Rule 1: the allow-list only
ratchets down, and a new permit is itself a gate error.

## The lesson worth keeping

A gate's output has **three** outcomes per rule, and two of them are green:
no-hit, baselined, and **escaped** (a reviewed inline annotation). Only `new ≥ 1`
is red. Reading the gate through a text match (`grep no-wrap`) collapses `escaped`
into `new`, which manufactures a red out of a pass and points the next agent at
code that needs no change. Read the rule's own counters — `hits= / baselined= /
escaped= / new=` — not a count of matching lines.

## Verification

See `## Verification` in the task report at `$GOPI_TASK_REPORT_PATH`
(`~/.gopi/sessions/01a10adb-a29b-730d-8c1c-c0c4a5e9e82b/task-report.md`):
build, the standards gate itself, the branch-scoped smoke for the owning package,
and the real exercise of the changed surface.
