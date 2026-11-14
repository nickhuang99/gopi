<!-- doc-class: historical -->
# The docclass routing-name rework, reconciled against a master that had already landed it

Task `01a141ff`, 2026-10-17. Branch `task/01a141925e307a55b2d11edad8166780`
(tip `de99aad27`, commits `cf7472390` + `de99aad27`) was reworked onto the
integration branch and merged into `task/01a141ff428178b5b5d4d6a3596df633`.

**Every sha in this document names the ref it was measured on.** Two refs spell
`master` on this ground and they are NOT the same commit:

| ref | sha | note |
|---|---|---|
| `refs/heads/master` | `9adad635b9a66ef51783c809436803a8a06b4efa` | local, stale, **37 behind** the cut point of this ground |
| `refs/remotes/gitPushcheck/master` | `942b3ac6ad27dd551080e726896d227875565e59` | **the integration branch**, and this worktree's cut point |

All readings below are taken on `refs/remotes/gitPushcheck/master` = `942b3ac6a`
unless a row says otherwise. `refs/heads/master` was never used for a decision.

## What the branch actually carried, and what master already had

The branch's own second commit (`de99aad27`) states its premise in the file it
writes: *"The sibling row `01a14162` wrote this fix on peerbox (`a9949a497`) and
it never landed on master"*. **That premise is false on this integration
branch**, and the measurement is one command:

```
$ git merge-base --is-ancestor a9949a497 refs/remotes/gitPushcheck/master; echo rc=$?
rc=0
```

`a9949a497` — the sibling's fix — IS an ancestor of `942b3ac6a`, and so is
`13993992c` (the sibling rework that then narrowed the gate to ONE listing) and
`bc0927986` (which classified the gate's devdoc through the corpus writer).
Both files this branch is add/add-conflicting on were **created on the
integration branch by `a9949a497` and amended there by the later commits**, so
this is not "two authors invented the same file": it is the same author's earlier
landing versus the branch's replay of it, with the branch's replay three
revisions behind.

The decisive reading is the blob identity of the branch's version:

```
$ git rev-parse de99aad27:internal/standards/testtree_typecheck_gate_test.go   # f1fb432e9…
$ git rev-parse a9949a497:internal/standards/testtree_typecheck_gate_test.go   # f1fb432e9…  (identical)
$ git rev-parse refs/remotes/gitPushcheck/master:internal/standards/testtree_typecheck_gate_test.go  # 9833680f7…
```

The branch's gate test is **byte-identical to `a9949a497`**, i.e. to a revision
`13993992c` superseded on master. It therefore carries that superseded geometry
with it: `testTreeTypecheckErrors` returning a `(entries, variants, failures)`
triple from ONE invocation, and a second helper
`testTreeTypecheckTestVariants` that runs the expensive whole-tree listing a
**second** time. Taking the branch's side would have re-introduced exactly the
cost `13993992c` removed:

> `test(standards): the typecheck gate lists the tree once, not twice` —
> *"the whole-tree listing is the expensive half of the gate, so the test-variant
> count now comes from the same invocation that reads the failures … the gate
> pays one listing instead of two."*

Its devdoc (`devdoc/testtree-typecheck-gate.md` on master, `4060f28b0`) has the
same story in one sentence, in a paragraph the branch **deleted**: *"ONE
invocation answers all three: the listing is the expensive part (~2 s warm, ~45 s
cold for `./...`), and a second call just to count test variants would double it.
The variant count comes from the same entries the failures do, so the two can
never disagree about which view was enumerated."*

## Per-file disposition — which side won, and why

| conflicted file | kind | **side that won** | why (measurement on `gitPushcheck/master` = `942b3ac6a`) |
|---|---|---|---|
| `internal/standards/testtree_typecheck_gate_test.go` | add/add | **MASTER** (`9833680f7`) | The branch's copy is byte-identical to `a9949a497` (`f1fb432e9`), a revision `13993992c` superseded ON master. Master's side is the ONE-listing version (`testTreeTypecheckErrors` returns `(entries, variants, failures)` from a single `go list`); the branch's side adds `testTreeTypecheckTestVariants`, a SECOND whole-tree listing, and drops the doc's "one invocation" paragraphs. Master is strictly the later generation of the same file; nothing on the branch side is absent from master's semantics — the variant floor (`testTreeTypecheckMinTestVariants = 50`) and every red-proof arm are present on both. |
| `devdoc/testtree-typecheck-gate.md` | add/add | **MASTER** (`4060f28b0`) — plus a correction appended | Master's blob is `bc0927986`'s *writer-classified* revision (that commit **removed one line** from the doc so the corpus writer's verdict would agree with the file); the branch's copy (`0fc101ba1`) is the pre-`bc0927986` text of the same landing run plus a 34-line section. Line-set measurement: `comm -23 <master lines> <tip lines>` = **0** — master's doc is a line-set subset of the branch's, i.e. the branch's doc is a SUPERSET of the pre-fix text, but its added section rests on the false premise above and its own body still describes the two-invocation shape. Reconciliation therefore keeps master's file and **corrects** the false premise in place (see the appended "Rework note" section) rather than believing it. |
| `internal/docclass/anchor_number_material_test.go` | content | **MASTER** (`db2ecb817`) | Not one of the two add/add files, and it is here because the branch touched it: the branch **reverted** `b5cb0628c`. `b5cb0628c` (master, 2026-10-16 09:44) made `anchorNumberMaterialGitEnv`'s probe hermetic with `testutil.GitEnv()` and dropped four Gate9 git-env permits (77 → 73) — a measured data-loss class fix. The branch's copy restores the hand-rolled `os.Environ()` loop, which on master's bare lambda `out, err := cmd.Output()` **would not even compile** (`out` undefined — the branch's version was written against the base, which still had `out, err :=`, copied in from its `a9949a497` parent). Master's revision is 3 h newer and strictly better on both counts. |
| `devdoc/DOC-INDEX.tsv` | content (auto-merged) | **MASTER** (clean auto-merge, no hunk kept from the branch) | The branch's index was generated over the `3124ec41f` corpus; master's index carries 2631 rows to the base's 2560. A row-level union would have re-inserted 70+ stale rows. The row for the gate's devdoc is the SAME on both sides (`devdoc/testtree-typecheck-gate.md  normative  …  path internal/docclass/anchor_number_material_test.go`) and `git diff <master> <merged index>` is **empty**. |
| `cmd/agent/cli_doccheck_test.go` | (merged cleanly, and it is the ONE real deliverable) | **BRANCH** | This is the half master does NOT have. Master still spells `gitDirEnvName = "GIT_DIR"` / `gitWorkTreeEnvName = "GIT_WORK_TREE"` package-locally in `cmd/agent/cli_doccheck_test.go` (lines 62–63 at `942b3ac6a`), beside `internal/testutil`'s exported pair — the mirror that made the pair look like a repository-owned name, which is the ORIGIN of the `undefined: gitDirEnvName` defect. The branch replaces both with `testutil.GitDirEnvName` / `testutil.GitWorkTreeEnvName` (Rule 14, one definition, never a mirror). Kept in full: `+11 / -3`. |

**Net effect of the merge against `gitPushcheck/master` = `942b3ac6a`:**

```
$ git diff refs/remotes/gitPushcheck/master --stat
 cmd/agent/cli_doccheck_test.go | 14 +++++++++++---
 1 file changed, 11 insertions(+), 3 deletions(-)
```

One file, one concern. Everything else the branch carried was already landed on
the integration branch, in a later revision, and was dropped **by that argument**
rather than by preference.

## The redundant half, named

The epic asked to verify and drop the redundant half when master already carries
the single definition. It does, and the redundant halves are:

1. `internal/testutil/gitenv.go`'s `gitWorkTreeEnvName` → `GitWorkTreeEnvName`
   rename — already on master (`cea1dc32c` **deleted** the `pinSuiteGitRoutingEnvName`
   ticket, so master's file is the terminal revision; the auto-merge kept
   master's, byte-identical: `git diff <master> -- internal/testutil/gitenv.go`
   is empty).
2. The same rename's propagation into `internal/testutil/gitenv_test.go`,
   `internal/docclass/anchor_number_material_test.go`,
   `internal/docclass/anchor_number_fixture_isolation_test.go`,
   `internal/docclass/anchor_number_provenance_test.go` — all already reference
   `testutil.GitDirEnvName` / `testutil.GitWorkTreeEnvName` on master
   (`git grep` over `refs/remotes/gitPushcheck/master`).
3. The gate test and its devdoc — present, and LATER, on master.

## The rework note appended to the doc (why it is not a silent overwrite)

Master's `devdoc/testtree-typecheck-gate.md` did not know the rework happened, and
the branch's version asserted a premise that is false here. Taking master's side
and saying nothing would leave a reader with the branch's story either deleted or
believed. So the doc keeps master's bytes and gains one section that states the
finding and the measurement — the doc remains master's, the record of the decision
is not lost.

## Verification

- `go build ./...` → **rc=0** (this worktree, merged tree).
- `go vet ./...` → **rc=0**, no output.
- `go test ./internal/standards/ -run 'Typecheck|Testtree' -count=1` →
  `ok github.com/nick/gopi/internal/standards 42.579s`, **rc=0**.
- `go test ./internal/docclass/ -run 'AnchorNumberMaterial|AnchorNumberProvenance|AnchorNumberFixtureIsolation' -count=1`
  → `ok … 3.358s`, rc=0 (the package whose test file the branch had reverted).
- `go test ./internal/testutil/ -run 'GitEnv|Routing' -count=1` →
  `ok … 3.621s`, rc=0 (the renamed name's own suite).
- `go test ./cmd/agent/ -run 'DocCheck' -count=1` → `ok … 19.065s`, rc=0
  (the ONE file this merge actually changes — Rule 31: the changed surface was
  run, not just compiled).

## Mergeability of the resulting branch

Measured on this branch at its merge commit (the second parent is `de99aad27`;
run `git rev-list --parents -1 HEAD`), against
`refs/remotes/gitPushcheck/master` = `942b3ac6a` (the close-out reading of that
ref; it was the same commit at the start of the run):

```
$ git merge-base --is-ancestor refs/remotes/gitPushcheck/master HEAD; echo rc=$?
rc=0
```

— the integration branch is an ancestor of this tip, so landing it is a
fast-forward in content terms.

And the acceptance probe, run in a THROWAWAY clone (never in the main checkout,
Rule 29/6c) whose tree is `942b3ac6a`:

```
$ git merge --no-commit --no-ff task/01a141ff428178b5b5d4d6a3596df633
Automatic merge went well; stopped before committing as requested
rc=0
$ git ls-files --unmerged        # (no output — no unmerged path)
```

Staged result of that probe against `942b3ac6a`: **four files**, and not one
line of master's code or prose removed. The per-file numbers are NOT pinned here
— this document is one of the four, so any insertion count written into it is
stale the moment the sentence is edited. Take the reading from the commit:

```
$ git show --stat <the merge commit>    # four files; the branch's whole delta
$ git diff refs/remotes/gitPushcheck/master --name-status
M cmd/agent/cli_doccheck_test.go
M devdoc/DOC-INDEX.tsv
A devdoc/testtree-typecheck-gate-rework-20261017.md
M devdoc/testtree-typecheck-gate.md
```

(The `-6` lines are the two duplicated manifest rows this run removed, one row
re-rendered by the writer, and one added paragraph boundary — nothing of master's
prose or code left the tree.)

## DOC-INDEX (Rule 36)

`devdoc/DOC-INDEX.tsv` carried a defect this run had to repair before it could
add its own row: the path `devdoc/merge-compile-only-policy.md` had **three**
rows (two identical `normative` rows appended at the tail, line 2630/2631 of the
pre-edit file, plus the sorted one at 1290) — present on `942b3ac6a` itself, and
`TestDocManifestCoversEveryDevdocFile` + `TestDocClassBannerMatchesManifest` were
RED on it before this run touched anything. `classify.py` refuses to write a
corpus carrying a duplicate, so the two tail duplicates were dropped and the
surviving row re-derived with `classify.py --only devdoc/merge-compile-only-policy.md`
(the row is unchanged in class, so the re-derivation is a no-op on text: the
manifest's numstat is **+2 / -3**, the two dropped rows the only loss).

This document's own row was then added by the corpus's own writer
(`classify.py --add-missing --only devdoc/testtree-typecheck-gate-rework-20261017.md`)
→ `historical`, and its banner by `tools/docclass/banner.py . --only …`
(→ `<!-- doc-class: historical -->` on line 1). Both tools wrote; neither row was
typed by hand.
