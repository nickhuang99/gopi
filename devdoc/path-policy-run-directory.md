<!-- doc-class: historical -->
# One run directory for the judge and the resolve (Rule 41 rework)

**File**: `internal/tools/path_policy.go`
**Branch**: `task/01a14480e5bf7deca16f7d8bd545d05d`
**Measured against**: `refs/remotes/gitPushcheck/master` = `b03796d9f9faeb4479b4f6c8b9323c8ff621dcf8`
(the integration tip, and this branch's cut point). `refs/heads/master` = `9adad63…`
is **50 commits behind** and is NOT the criterion ref for any number below.
**Date**: 2026-10-14

## Why this document exists

A branch `task/01a1361ba9ee7f198271a702059e958b` ("one run-directory for the judge
and the resolve") was to be reworked onto the integration tip. **Nothing of it
survived in this repo**: the branch ref is gone
(`git rev-parse --verify --quiet refs/heads/task/01a1361b…` → exit 1), its stated
tip `2d4f094cf` is not an object (`git cat-file -t 2d4f094cf` → *fatal: Not a
valid object name*), and the saved diff's attached path does not exist on disk.
So the work could not be *applied* — it had to be **re-derived** from the three
intent items the briefing states, and each item measured against `b03796d`
before anything was written (Rules 41/15: measure, quote output, repair only
what still reproduces).

The three intent items, as stated:

- **(a)** rejection-base = the command's **run directory**;
- **(b)** an **empty-workdir fallback** to `os.Getwd()`;
- **(c)** a **delete-target fallback** to the session workdir.

## The verdict per item, measured on `b03796d`

### (a) rejection-base = the command's run directory — **ABSENT, repaired here**

The working-tree arm resolves a relative operand against the command's run
directory: `gitWorktreePathTargets` computes a `baseDir` from git's `-C` or the
`cd` carry (`carriedChangeDir`) and hands it to `targetBaseDir`. The
**destructive** arm did not — it resolved every path-like token against the
session `workdir`. The `cd` was read there as an *authorization root* only
(`commandChangeDirRoots`), never as a *resolution base*.

Measured with the pre-repair code (the number is the refusal it produced):

```
cd <root>/a && rm -rf ../../declared-root/build
  → target "/…/declared-root/build" is outside every declared root …
```

`<root>/a` is *inside* the declared root `<root>`, so the shell's real target is
`<root>/build` — **inside** the root, and the guard refused it. The judge was
resolving against a path the shell never visits. That is the Rule-41 defect in
this file: two arms of one policy answering "where does this target land?"
against two different directories.

Repair: `commandPathTargets` now returns `[]worktreeTarget` — each target
carrying the base directory **it** resolves against (`carriedChangeDir` over the
`cd`s in force at that simple command, this segment's own leading `cd`
included), and the destructive arm judges through the same `targetBaseDir` the
worktree arm uses. One reader for the run directory, both arms (Rules 13/14).

### (b) empty-workdir fallback to `os.Getwd()` — **PRESENT on `b03796d`, nothing added**

`absolutize` (`internal/tools/path_policy.go`) already falls back when the base
is empty:

```go
base := workdir
if base == "" {
    if cwd, err := os.Getwd(); err == nil {
        base = cwd
    } else {
        return "", false
    }
}
```

and the cwd whitelist's own reader (`rejectDangerous`, `internal/tools/bash.go`)
has the same fallback with an explicit refusal when it fails (`cannot determine
working directory`). The empty-workdir case is therefore covered twice over; a
second copy of the fallback would be a mirrored literal-shaped duplicate of the
thing Rule 14 forbids. **No change made.**

### (c) delete-target fallback to the session workdir — **PRESENT as a shape, but unreachable for deletes; now shared**

`targetBaseDir` already renders exactly this fallback — "the target's own base
when it has one, else the session workdir" — but on `b03796d` only the worktree
arm ever built a target that *had* a base, so the fallback was structurally
unreachable for a recursive delete. After (a)'s repair the destructive arm walks
the same path, so the fallback is genuinely one definition serving both arms
rather than a branch only one caller could reach. **No new fallback written** —
the existing one was put on the path.

## What this run added

| | |
|---|---|
| `internal/tools/path_policy.go` | `commandPathTargets(command, workdir)` returns `[]worktreeTarget` with a per-target `baseDir`; the destructive arm judges through `targetBaseDir`. +42/−10. |
| `internal/tools/path_policy_run_directory_test.go` | New. Pins the repaired shape and the property, and re-asserts the guard still refuses an out-of-root target reached through a `cd`. |

**Red proof** (the test run against the pre-repair judge line, then restored):

- with `judgeTarget(target.path, workdir, …)` — the old base — the test FAILS
  with `was REFUSED, but its target resolves inside the declared root`.
- with `judgeTarget(target.path, targetBaseDir(target, workdir), …)` it PASSES.
- the restore was verified by hash identity against the fixed copy
  (`sha256sum` equal on both, `16683928…`), then re-run green.

## What master already had (so this is not a second copy)

Four commits on the integration tip had already built the run-directory
machinery under other names, and this change **consumes** them rather than
re-deriving them:

- `7b0c2b063` — a leading `cd` contributes the base a worktree target resolves against (`carriedChangeDir`, `targetBaseDir`);
- `67d2277a4` / `1b8bd26c8` — a repository-naming option (`-C`, `--git-dir`) removes the `cd`'s **authority**;
- `5e1ee458a` — a `cd` is a resolution base, never an authorization root;
- `1778df09f` — `git worktree move`'s operands enter the same judgement.

The surviving gap was exactly one arm: the destructive arm never asked for the
base those commits produce.

## Honestly NOT done

- **The deleted branch's own diff was never seen.** Every claim here is derived
  from the three intent items as stated in the briefing, not from its code. If
  that branch carried anything beyond (a)/(b)/(c) — a test, a comment, an
  unrelated refactor — it is not in this change and nobody can reconstruct it.
- **(b) and (c) were not touched**, per the measurement: adding them would have
  duplicated behaviour already present (Rule 14).
- **`git worktree move`'s destination and `.`/`..`-only tokens** inherit the
  same carried base now, but no *new* test covers the move-destination shape
  under a `cd`; the existing move tests still pass unchanged.
- The rework does not attempt to judge a target whose base itself came from an
  unresolvable `cd` (a shell expansion). `carriedChangeDir` keeps the last
  resolvable directory rather than guessing — unchanged behaviour, and the
  fail-closed expansion check upstream still refuses those commands by name.

## Related

`devdoc/path-policy-cd-base.md` (historical) records the `cd`-carry work on the
worktree arm; this document is its destructive-arm counterpart and cites the
same reader rather than restating it.
