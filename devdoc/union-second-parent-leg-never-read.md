<!-- doc-class: historical -->
# The union's second-parent leg never read a parent — and why the error arm had to change primitive

Task `01a13031-4eca-7c8d-a829-d0ffd0ad7180`. Branch `task/01a130314eca7c8da829d0ffd0ad7180`.
Subject: `internal/tools/mergeset_gendoc.go`, `mergesetGendocUnion`.

## 1. The defect

The published-anchor union reads three change sets: the landing's own diff, the
first parent's diff, and the second parent's diff. The second leg composed its
revision as

```go
mergeRev + gendocUnionSecondParent   // gendocUnionSecondParent = "2"
```

git has **no `<rev>N` parent syntax at all**. The Nth parent is `<rev>^N`;
`<rev>2` is `<rev>` followed by a *path*. So the leg's `git rev-parse` failed on
**every** landing, and the loop's `continue` — written for a root commit, which
genuinely has no parent — swallowed it. One half of the union contributed no path,
ever, with every test in `mergeset_gendoc_test.go` green: not one of them asserted
that a leg had READ anything.

## 2. The measurement that reshaped the fix (the surprising part)

The obvious repair is "distinguish a missing parent from a failed read". The
obvious instrument is `git rev-parse`'s exit status. **That instrument cannot do
it.** Measured on this host, on a throwaway repo:

| command | exit | stdout | stderr |
|---|---|---|---|
| `rev-parse <root>^`      | **128** | `<root>^` | `fatal: ambiguous argument …` |
| `rev-parse <root>^2`     | **128** | `<root>^2` | `fatal: ambiguous argument …` |
| `rev-parse <non-merge>^2`| **128** | `<non-merge>^2` | `fatal: ambiguous argument …` |
| `rev-parse <non-merge>2` | **128** | `<non-merge>2` | `fatal: ambiguous argument …` |
| `rev-parse <merge>^2`    | 0 | the second parent | — |
| `rev-parse <merge>^1`    | 0 | the first parent | — |

A parent that does not exist and a revision expression git has no grammar for are
**indistinguishable**: same status, same echoed argument. Any arm built on that
status would report a root commit — the normal case, every repo's first commit —
as an error on every landing that touched it.

So the repair changes the **primitive**: the commit's shape is read once, in one
call, as a *list*.

```
git rev-list --parents -n 1 <mergeRev>      # "<commit> <parent1> <parent2>"
```

`rev-list` succeeds whenever it can read the commit, and **what it prints IS the
parent list**. Absence is then read from the list's LENGTH (`len(parents) <= leg.index`
⇒ the commit has no such parent — a fact about the commit), and every failure is a
genuine failure to read. One call for both legs, so the two legs cannot disagree
about the commit's shape, and both parents come from git's own order rather than
from two independent `^1`/`^2` expressions.

## 3. The second instance, found by the same reasoning

With the parent list in hand, 1 fixture of 6 went red:

```
--- FAIL: TestMergesetUnionGateIsSilentForALandingThatTouchesNoAnchorSource
    the union's first-parent leg (parent 0149dea94… of e2b3cf0e8…) was NOT read:
      git diff --name-only 0149dea94^ 0149dea94: exit status 128
```

The same class, a second place: a leg asks what **its parent** brought in, which
is read as that parent's own diff base (`<parent>^ <parent>`) — and **that base
does not exist when the parent IS the repository's root commit**. The pre-fix code
swallowed this too (a fixture whose branch is the first commit on master has a
root as its first parent). It is now decided from the parent's own parent list and
stated as a **boundary**: `first-parent <sha> (the repository's root: no preceding
state)` — read, contributing nothing, and never confused with a leg that failed.

## 4. The error arm, as shipped

- a leg that cannot be resolved is its OWN note (`notes`), carried into the
  caller's abstention and the log — never equal to "the commit has no such
  parent";
- the legs that READ are named in the note (`; parent leg(s) read: first-parent
  <sha>, second-parent <sha>`), so "the union covers no anchor source" is
  distinguishable from "one half of the union was never read";
- a leg whose diff pair cannot be resolved names the leg and its parent
  (`<label>: git diff …`), and carries its own parent SHA so a failed pair reads
  as a failed pair rather than as a difference against the merge rev.

## 5. Audit — sibling sites composing `gendocUnionSecondParent`

Read the tree, not the list. `gendocUnionSecondParent` now has **one composing
site**, and the second-parent index is stated in exactly one place:

```
internal/tools/mergeset_gendoc.go:  const gendocUnionSecondParent = "2"      # the suffix, alone
internal/tools/mergeset_gendoc.go:  {label: second-parent, index: 1}          # gendocUnionLegs — the ONE site
```

Nothing else in `internal/` or `cmd/` composes the constant, and no other site
composes a parent index onto a revision: the other parent reads in this package
are `rev-list --parents` (`verify_gate.go:356`) and `^`-suffixed diffs
(`rebuild_vet_mergeset.go:463,604`), both of which are real git syntax.

## 6. Evidence

**The defect, on real state** (a real merge on master, not a fixture):

```
$ MR=$(git rev-list --merges -n 1 master); git rev-parse "$MR^2"
167afc4a47a94c7a011285d401076da64be2f665          # ^2 — the real parent, OK
$ git rev-parse "${MR}2"
fatal: ambiguous argument '3c0fbce75…ec2': unknown revision or path not in the working tree.   # rc=128
```

`<rev>2` is a path, not a parent — measured on the same commit whose second
parent `^2` resolves.

**Red proof** (the pre-fix second-parent spelling restored, the new test only):

```
--- FAIL: TestMergesetUnionReadsBothParentsOfARealMerge (0.20s)
    mergeset_gendoc_test.go:581: the union must READ the second-parent leg (c36473507…);
      the legs it reports reading are [first-parent 76320f7ab…]
```

A full revert of the whole leg loop failed one assertion earlier and with the
same causation: `the union must READ the first-parent leg (…); the legs it
reports reading are []`.

**Green with the fix, and the leg OBSERVED on real state** — `mergesetGendocUnion`
run against that same real landing:

```
REAL LANDING 3c0fbce751ce4c82b81270779229d2c24a3225ec
  note     = ""
  readLegs = [first-parent 69c24fadb… second-parent 167afc4a4…]
  union    = 28 path(s); first 5: [COMMANDS.md CONFIG.md cmd/agent/cli_scratch_gc.go …]
```

Both legs read the SHAs git itself reports; the second parent's own change is in
the union, where pre-fix it contributed nothing.

The original tree (constant `"2"`, original tests) was **green** — that is the
whole defect.
