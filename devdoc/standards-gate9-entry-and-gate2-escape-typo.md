<!-- doc-class: historical -->
# Two `internal/standards` readings, checked against the tree: a Gate9 entry that was already gone, and a Gate2 hit caused by one missing space

Task `01a14259-caa8-7559-bea9-6d285ab79eaa`, 2026-10-16, on the ground cut at
`f007c880214127e173f378d5c08d94b441500bb4`. This is a **run's record**: both
measurements are anchored to a revision and say nothing about a later tip.

## 1. The reported Gate9 entry (`anchor_number_material_test.go:155`) — real at `ab19708`, already deleted at HEAD

The briefing reported the `gitEnvBaseline` entry
`"internal/docclass/anchor_number_material_test.go:155"` as a stale permit that
matches no hit, red at the integration tip `ab19708`. **That reading is correct —
at `ab19708`.** It is **not** a live fact at this ground's HEAD, and the commit
that removed it is already an ancestor of HEAD, so nothing was pending here.

| measurement | at `ab19708` | at HEAD `f007c88` |
|---|---|---|
| the entry in `internal/standards/baseline.go` | **present** (`:1008`) | **absent** (the path survives only in a *comment* at `:972`) |
| `gitEnvBaseline` entry count | **77** | **72** |
| `TestGate9GitEnvRedProof` | red (the reading reported) | `ok github.com/nick/gopi/internal/standards 2.152s` |

`ab19708` **is an ancestor of HEAD** (`git merge-base --is-ancestor ab19708 HEAD`
→ exit 0), so the report was an earlier reading of this same history, not a
falsified one. The commit that deleted the permit, found by `-S` on the exact
literal:

```
$ git log --oneline -S'"internal/docclass/anchor_number_material_test.go:155"' ab19708..HEAD -- internal/standards/baseline.go
b5cb0628c fix(standards): repair the 4 fixture git-env leaks and ratchet the Gate9 baseline down
```

`b5cb0628c`'s own message states the direction and the arithmetic — *"FOUR
entries leave the baseline (77 -> 73)"*, and it names `:155` explicitly as
**"the ROTATED key — the hit had moved to :166"**. So the entry was not re-keyed
into a new permit; it was deleted after the underlying site was **repaired**
(`anchorNumberMaterialGitEnv` now builds its environment from the existing
`testutil.GitEnv()` helper instead of `append(os.Environ(), GIT_DIR=…,
GIT_WORK_TREE=…)`). That is the gate's own design obeyed: a permit whose hit no
longer fires is an error, and the repair is to fix the site, not to re-key the
row. `de5b3315c` (same family, Gate6+Gate9) is **not** an ancestor of
`refs/heads/master`, but `b5cb0628c` — the one that carried *this* deletion — is.

**Direction of the ratchet: down** (77 → 72 on the way to HEAD). No permit was
added and no `git-env-ok` escape was used to reach green.

**Lesson, and it is the general one:** a report entry is a record of a *revision*,
not a standing claim about the tree. Before acting on one, re-measure it at the
revision you actually hold — `git grep <literal> <rev> -- <file>` plus
`git merge-base --is-ancestor <rev> HEAD` answers "already fixed upstream?" in
two commands, and the answer flips the whole task. `devdoc/gate9-git-env-red-was-fixed-upstream.md`
records the same shape for a different entry (`cli_tasks_help_surface_basic_test.go:1365`).

## 2. The red that *was* live: Gate2's escape token needs the space

With Gate9 green, `TestStandardGates` was still red on Gate2 (Rule 17, test env
isolation) — one **NEW** (unannotated) hit, and the acceptance command for this
task is that whole run:

```
--- FAIL: TestStandardGates/TestGate2TestEnvRedProof (0.56s)
    gate_test.go:105: Rule 17 (test env isolation): 112 total hits, 62 baselined, 49 escaped, 1 new (unannotated), 62 baseline entries
    gate_test.go:117: Rule 17 (test env isolation): NEW violation (checklist 5.2) at internal/docclass/walk_ignored_test.go:297
```

### Root cause (5-why, measured at each step, not inferred)

1. **Why is the site NEW?** The gate found an `os.Setenv` with no escape marker
   registered for it.
2. **Why did the marker not register?** The token the gate looks for is spelled
   with a space after `//`, and this one line is not:

   ```
   $ grep -n 'testEnvEscape = ' internal/standards/gate.go
   794:const testEnvEscape = "// env-leak-ok:"
   ```
3. **Why only this site?** It is the **only** occurrence of the spaceless form in
   the tree, against 58 of the spaced form — so this is a typo, not a second
   dialect:

   ```
   $ grep -rn '//env-leak-ok' internal/ cmd/ | wc -l
   1
   $ grep -rn '// env-leak-ok' internal/ cmd/ | wc -l
   58
   ```
4. **Why is the site not simply a legacy unmarked site?** It was **added** by
   `c2890a772` (2026-10-16 14:43:08, *"fix(docclass): a git fatal is not the
   ignore rule's 'nothing is ignored' answer"*) — long after the 2026-09-12
   sweep that baselined the legacy sites — and it carries a **second** marker
   beside it (`// stylegate:ignore`), plus a prose comment above that names Rule
   17's reviewed `SnapshotEnv` pair. The author's intent is unambiguous: this
   line was meant to carry the **reviewed inline escape**.
5. **Why did nobody notice?** The control in the *same file* is written
   correctly (`:239`, `// env-leak-ok:` with the space) and is recognised, so a
   reader eyeballing the two lines sees two marked sites; only the gate
   distinguishes them. Nothing else consumes the token — `gate.go:794` is its
   **one** definition (Rule 14).

**Root cause: a one-character typo in a reviewed escape marker.** Not a missing
permit, not a legacy site, not a semantic disagreement with the gate.

### The fix — one character, and the token's definition untouched

`internal/docclass/walk_ignored_test.go:297`: `//env-leak-ok:` → `// env-leak-ok:`
(the surrounding prose and the `stylegate:ignore` marker are unchanged).

This is deliberately **not** the alternatives, each of which would trade a real
guard for green:

- **Not** a new `baseline.go` permit — that would ratchet the allow-list **UP**
  for a site that is not legacy, and the constraint on this task is down-only.
- **Not** a weakening of the token (`gate.go:794` is its single definition and
  was not touched) — e.g. accepting a spaceless prefix would make the gate blind
  to a marker that any tooling searching for the literal form would also miss.
- **Not** `t.Setenv` — the comment above the site explains why a test-scoped pin
  is wrong here (it would stay in force for every later test in the binary,
  pointing them at a nonexistent repository); the per-call flip with an explicit
  restore is the reviewed shape, and the sibling `:239` uses exactly it.

### Regression guard

The repro test already exists and is the one that caught this: **`TestGate2TestEnvRedProof`**
failed before the change and passes after (the red reading is filed under
`artifacts/gate2-red-before-fix.txt` in this run's session directory). No new
test was added, and that is the correct call rather than an omission: the gate's
own NEW-detection **is** the guard for "a reviewed escape did not register", and
it demonstrably fires. A test asserting the *spelling* of markers would duplicate
the gate's scan while being weaker than it.

## Evidence ledger (this run's session directory)

| file | what it holds |
|---|---|
| `artifacts/gate9-anchor-entry-determination.txt` | the revision-anchored Gate9 measurement (revs, counts, the deleting commit) |
| `artifacts/gate2-red-before-fix.txt` | the red Gate2 reading taken on the clean worktree, plus the token/typo measurement |
