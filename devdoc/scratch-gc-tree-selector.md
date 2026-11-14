<!-- doc-class: normative -->
# `scratch-gc trees --tree NAME` — a per-tree selector for the declared-tree sweep

**Class**: feature + gate. **Status**: shipped on `task/01a13db745a577fd86c847dde19dde78`.

## The defect this closes

`gopi tool scratch-gc trees` swept **every** declared tree. The arm is one press
of a button away from reclaiming the shared build cache (`gopi-build-cache`, tens
of GB on the operator's host) whether or not the build cache was what the operator
was after, and the trees differ in what losing them costs: a `go-build*` directory
left by a killed build is refuse, the build cache is what every later build pays
for, and a raw log is unrecoverable. "Bound the GOTMPDIR tree and leave the cache
alone" was not expressible.

## What was added (Rule 35: one parameter, no new verb/state/daemon)

- `--tree NAME`, accepted on the **existing** `trees` arm, in both spellings
  (`--tree NAME` and `--tree=NAME`).
- **Absent** selector ⇒ every declared tree, exactly as before.
- `--tree all` ⇒ the same pass, asked for by name (so a script can say which of
  the two it meant).
- `--tree=` (explicitly empty) ⇒ **refused**. A truncated argument must not widen
  a destructive pass to the whole set.
- Unknown name ⇒ **fail-closed refusal** before anything is touched, in `--dry-run`
  and in a live pass alike (and on the disabled path, so "the sweep is off" is
  never a second, more permissive parser), listing every valid name.

## The seam (why `cmd/` holds no loop)

`internal/scratch/declared_tree_selector.go` owns the name → rows mapping:

```go
DeclaredTreesForName(selector string) ([]DeclaredTree, error)   // ""/all → all rows; name → its rows; else ErrUnknownDeclaredTreeName
DeclaredTreeNames() []string                                     // DERIVED from DeclaredTrees(), table order
DeclaredTreeFlagName / DeclaredTreeFlagSpelling / DeclaredTreeSelectorAll
```

The declared set is that package's table, so the mapping belongs beside it: a
`for _, tree := range scratch.DeclaredTrees() { if tree.Name == want … }` in the
CLI would be a second, un-syncable answer to "what does this selector mean", and
the CLI cannot import the name of a tree it has not heard of. The valid-name list
is **derived, never spelled** (Rules 13/14): a row added to the table — including
the per-worktree-root rows whose names are built at runtime — is selectable and
listed the moment it exists, and a renamed row cannot leave a stale name
selectable.

The CLI keeps only the rendering: `refuseDeclaredTreeSelector` prints the refusal
and exits 1, and it names `scratch.DeclaredTreeNames()` — the same enumeration the
resolver accepts.

## Why the answer is a slice

The per-root rows contribute more than one row under distinct names
(`build-cache`, `build-cache/shards`, `build-cache/suite-sandbox`,
`build-cache/<root>`), so a selection is `[]DeclaredTree` even when one name was
given: the caller must not have to know which rows are one-per-host and which are
one-per-worktree-root.

## The invariants that had to survive

- **The declared ROOT survives at any age** — enforced inside
  `internal/scratch` (`SweepDeclaredTree` / `removeCollectableOfTree` refuse a
  declared root); this face adds nothing to that rule, it only reports
  `present=true`/`entries=` read **after** the pass, which is what says the root
  is still there.
- **One report line per SELECTED tree**, on the existing logfmt shape, still
  carrying `tree=`, `stale=` (the tree's own window), `bytes=`, `present=`,
  `entries=`, `holding=`.
- **The other declared trees are byte-for-byte untouched** — asserted by
  digest, not by intention (see the gate below).

## The gate

`internal/scratch/declared_tree_selector_test.go`:

- `TestDeclaredTreeNamesAreDerivedFromTheTable` — the list IS the table's names,
  and every listed name resolves (a listed-but-unselectable name is the worst of
  both: the refusal recommends a value the resolver then refuses).
- `TestDeclaredTreesForNameSelectsExactlyTheNamedRows` — `""`/`all` = the whole
  set by identity; a name = exactly its rows; an unknown name wraps
  `ErrUnknownDeclaredTreeName` and carries the value and the valid names.
- `TestDeclaredTreeSelectorConfinesThePassToTheNamedTree` — **the cross-tree
  gate**: a pass driven through the selector removes the named tree's aged entry,
  keeps its fresh control, leaves a sibling declared tree at the same digest, and
  leaves every declared root in place. It also fails when the named tree's own
  digest did not move, so the confinement assertion can never be vacuous.

`cmd/agent/cli_scratch_gc_trees_test.go` drives the same properties through the
**built binary** over a sandbox (`TestScratchGCCLITrees_SelectorSweepsExactlyOneDeclaredTree`,
`…_SelectorRefusesAnUnknownName`, `…_SelectorAllEqualsNoSelector`).

Red proofs (recorded in the task report): removing the selector from the CLI's
`trees` arm turns the binary tests red; making `DeclaredTreesForName` ignore its
selector turns the cross-tree gate red.

## What this deliberately is NOT

No new verb, no state file, no daemon, no environment variable, no schedule
(Rule 35). The selector is a parameter on an existing arm and the resolver is a
function in an existing package.
