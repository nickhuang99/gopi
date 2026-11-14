<!-- doc-class: historical -->
# The `cd` carry: one reader for "which directory does this command run in"

**File**: `internal/tools/path_policy.go` (+ `internal/worktreeroots/roots.go`)
**Branch**: `fix/path-policy-cd-base`
**Date**: 2026-09-15

## The defect

The worktree-target arm of the blast-radius guard extracts the positional
operand of `git worktree add|remove <target>` and judges it against the declared
roots. Two different spellings name a run directory for that command, and only
one of them was read:

- **git's `-C` global** — `git -C <repo> worktree remove <wt>`. Consumed by
  `gitInvocationStart` since the 2026-09-15 `-C` fix; that fix lived only on
  `fix/worktree-residue-closeout`, **not on master**.
- **the shell's `cd` builtin** — `cd <repo> && git worktree remove <wt>`. Not
  read at all. A `cd` is not part of the git command line, so no git-side reader
  could see it: the operand resolved against the **session workdir** while git
  resolved it against the directory the `cd` selected.

Two different paths ⇒ the guard judged (and could refuse) a path git would never
touch, and the worktree it was asked to remove stayed registered.

## Root cause, found the hard way

The first implementation assumed the `cd` sits in the **same** simple command as
the git invocation. It does not: `&&` is a sequencing operator and
`splitSimpleCommands` cuts on it, so `cd <repo> && git …` is TWO segments. A
debug probe (`TestDbgSplit`) showed the split and the empty base:

```
simple="cd /repo"                    cd="/repo" ok=true
simple="git worktree remove /wt"     cd=""      ok=false
TARGET={path:/wt baseDir:]
```

The base has to be read from the **preceding** segments — which is not a
widening of the rule but the rule itself: git resolves a relative operand
against the cwd it was launched in, i.e. whatever the `cd`s before it selected.

A second root cause sat in the red-proof harness: reverting
`path_policy.go` to master cannot compile the new tests (they call
`gitWorktreePathTargets(command, workdir)`, `worktreeTarget`, `targetBaseDir`).
The fix was a faithful **partial revert** — master's file plus a temporary shim
restating only the *seam* names while every *behaviour* stayed at master's
(`gitInvocationStart` not consuming `-C`; `carriedChangeDir` returning `""`).

## The design

One reader for the shell half, `carriedChangeDir(dirs, workdir)`, which replays
the `cd`s in order, each resolving against the directory the previous ones
selected (`cd /a && cd b && git …` ⇒ `/a/b` — last one wins, as a shell does).
Both consumers of the question call the same reader:

| consumer | reads | why |
|---|---|---|
| `gitWorktreePathTargets` | the **base** a relative operand resolves against | so the judged path is the path git acts on |
| `commandChangeDirRoots` | the **root beam** the judgement uses | so the operation is judged against the tree it runs in |

### Update 2026-10-16: the beam is an AUTHORIZATION, not a resolution base

The table above conflated two questions a `cd` answers, and the conflation was a
hole (Rule 41, measured on the live binary, task
`01a1361baa0d7ba58c37f2dda06230c7`): with the cd carried as a **root**, `cd <any
out-of-root dir> && git worktree add --detach <that dir>/wt <rev>` was ALLOWED —
and so was the matching `worktree remove --force` — while the SAME target
WITHOUT the leading cd was refused. A linked worktree is a whole checkout, so
that shape created and destroyed whole trees at any path on the host, including
the session directory on the `/` partition (Rule 22).

The two questions are now separate:

- **resolution base** — unchanged. `carriedChangeDir` still feeds
  `targetBaseDir`, so a relative operand resolves against the directory the cd
  selected, which is the directory git itself acts in.
- **authorization** — narrowed. `commandChangeDirRoots` takes the caller's
  **configuration root set** and grants the beam only when the selected
  directory already lies inside one of those roots (worktree root / scratch home
  / temp root / session workdir). A cd outside every declared root still
  resolves relative operands; it merely authorises nothing.

The beam the carry exists for is intact — `cd <my tree> && git worktree remove
<wt>` keeps working wherever the cd really is inside a declared root — and the
narrowing never relaxes the outside-every-declared-root refusal.

git's `-C` and the shell's `cd` are the two halves of one concept and are kept
adjacent in the constants block; `-C` wins when both are present (it *is* the
invocation's own run directory — asserted by a test case).

`targetBaseDir(target, workdir)` is the single place the two halves meet, so a
caller cannot pair a target with the wrong base (Rules 13/14).

## Deliberate non-widening

The `cd` carry adds a beam; it does not remove the property. `commandRoots` is
`declaredRoots(workdir)` **plus** the cd carry, so a target outside *both* is
still refused — and the refusal names the resolved target
(`TestPathPolicyChangeDirStillRefusesOutOfRootTarget`).

Two judged forms are deliberately **not** carried, and the tests pin the
negative direction:

- a `cd` that **follows** the invocation (`git … && cd /elsewhere`) — a sibling
  command whose effect cannot reach a command that already ran;
- an **operandless** `cd` (selects `$HOME`) and `cd -` (selects `$OLDPWD`) —
  statically unknowable, so nothing is guessed; the session workdir stays in
  charge, exactly as before this reader existed.

## Two bugs found in the existing seam while proving the red direction

`splitSimpleCommands` was also fixed, because proving the red direction exposed
both:

1. **`&&` was cut into two separators** — `FieldsFunc` drops *every* separator,
   so `a && b` yielded the empty middle field and, worse, composed the two
   sides' tokens into one inference unit.
2. **the POSIX group delimiters `(`/`)` were not cut at all**, so
   `(cd <repo> && git worktree remove <wt>)` glued the closing `)` onto the last
   token — the target was extracted as `…/gopi-task)` and the real path was
   never judged. That is a *false-negative* in the guard, i.e. a hole.

`TestPathPolicyChangeDirCarriesTheTargetBase` asserts the grouped spelling
equals the bare one.

## Verification

See the task report for the full command output, the red-proof failure messages
and the acceptance results.
