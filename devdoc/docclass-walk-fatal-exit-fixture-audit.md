<!-- doc-class: historical -->
# The walked-corpus fatal, second pass: the fixtures that were green on a refusal

Epic `01a14374-ca0a-7f37-bfe9-c3ae82005827` (audit of `01a14222-386a-77fe-ab08-97e9e1b30da4`).
Anchor: `refs/remotes/gitpushcheck/master` = `c59b72952`. Fix commit `c2890a772`
(branch `task/01a14222386a77feab0897e9e1b30da4`, merged as `6c625a3a1`).
Verdict here: **the fix is sound at the tip; its generalisation had two more live
instances, and they are closed.**

## The generalisation, stated so it can be searched for

> Any fixture that lets a CHILD walk a corpus must be a real git repository.

`dropIgnored` consults git's ignore rule for the corpus membership, so a fixture
root that is not a repository makes the walk **refuse**. Before `c2890a772` the
walk *answered instead* — it read the fatal `128` as git's "nothing is ignored"
and returned the filesystem listing — so a repo-less fixture silently got a
corpus nobody had decided. After the fix the same fixture is refused loudly.

The dangerous shape is therefore not "a repo-less fixture" but **a repo-less
fixture whose assertion never depended on the corpus membership** — there the
refusal arrives at a point the test does not check, and the case goes green
while measuring something other than its own subject. That is exactly the pair
found here.

## The two instances (both were GREEN, both were about a different subject)

Measured on this worktree at `c59b729` with an instrumented probe run
(`internal/docindex/zz_audit_probe_test.go`, a scratch file, deleted after the
reading; the reading is quoted verbatim below):

1. `TestDocclassDrift_AbstainsWhenTheTreeHasNoClassifier`
   (`internal/docindex/docclass_drift_test.go`) — its subject is *the tree has no
   classifier*, and its assertion was `NotMeasuredReason != ""`, a predicate
   satisfied by ANY abstention. The fixture root was `t.TempDir()` with no
   `git init`, so the verdict it was actually measuring was:

   ```
   MEASURED Reason="the corpus could not be enumerated: classifying devdoc/: git's
   ignore rule exited 128 without answering — refusing to guess which files the
   corpus holds (git check-ignore exits 1 for \"nothing is ignored\"; anything else
   is a fatal, e.g. \"not a git repository\")"
   ```

   The classifier the case deliberately omits was never reached.

2. `TestDocclassDriftRender_NoClassifierStillReportsTheTruth`
   (`internal/docindex/docclass_drift_progress_test.go`) — same shape on the
   render leg, with the same root and the same predicate
   (`errors.As` into `*os.PathError` / `*exec.Error`).

   **Weaker than case 1, measured:** removing this fixture's `git init` again
   leaves the case **GREEN** — because `docclassDriftRender` stats the classifier
   script BEFORE it walks, so on a root that is not a repository it refuses at
   `os.Stat` and the `*os.PathError` predicate is still satisfied by the right
   file. The case was not measuring a different subject; it was **one line of
   production code away** from doing so, and the subject assertion added below is
   what closes the gap (asserting on a refusal whose ORDER is not the case's
   subject is the same weakness case 1 had, one stat earlier). Reported as
   observed rather than as a second red-then-green.

**Repair (both files):** the root is a real repository (`git(t, root, "init", "-q")`)
**and** the assertion names its subject — the abstention must name
`ClassifyScriptName`. The second half is the one that matters: with only the
`git init` added, the case would be measuring the right subject but any future
refusal of ANY kind would still satisfy it.

**Red-then-green, measured.** Removing the `git init` from case 1 again:

```
--- FAIL: TestDocclassDrift_AbstainsWhenTheTreeHasNoClassifier (0.00s)
    docclass_drift_test.go:193: the abstention does not name the classifier this tree
    omits (classify.py): "the corpus could not be enumerated: classifying devdoc/:
    git's ignore rule exited 128 without answering — refusing to guess which files
    the corpus holds (git check-ignore exits 1 for \"nothing is ignored\"; anything
    else is a fatal, e.g. \"not a git repository\")"
```

and green again with the repository restored (`git status --porcelain` empty).

## The audit: every fixture root handed to a corpus-walking surface

Surfaces that walk: `docclass.Walk`, `docindex.CheckCorpus`,
`docindex.DocclassDrift`, `docindex.docclassDriftRender`. (`ScopeCheck` does NOT
walk — it reads one manifest — which is why `docindex_scope_test.go`'s
`normativeFixture` was never in this class; its fixture is a real repository
anyway, via `gitFixture`.)

Fixture helpers in `internal/docindex` (search: `grep -n 'func .*Fixture(t \*testing.T) string'`)
and whether the root they return is a repository:

| fixture | `git init` | handed to a walking surface? |
|---|---|---|
| `newFixture` (docindex_test.go:21) | no | no — `ScopeCheck`/`Repair`/`EnsureDoc` only |
| `gitFixture` (docindex_test.go:355) | yes | no (feed for `normativeFixture`) |
| `normativeFixture` (docindex_scope_test.go:41) | yes (via `gitFixture`) | no |
| `anchorCellFixture` (:93) | yes | no |
| `anchorDriftFixture` (:81) | yes | no (`SyncBranch`) |
| `facesFixture` (corpus_faces_agree_test.go:34) | yes | yes — `CheckCorpus` |
| `newProgressFixture` (:104) | yes (**repaired by `c2890a772`**) | yes — `docclassDriftRender` |
| `selfRowFixture` (:54) | yes | no |
| `fixpointFixture` (:71) | yes | no |
| `generatorProgressFixture` (:78) | no | **no** — `generatorRunnerSplit`, no walk |
| `refreshFixture` (:83) | yes | no |
| `literalDataFixture` (:80) / `readonlyCellFixture` (:84) / `rowedFixture` (:79) | yes | no |

Call sites outside the package (`internal/standards`, `cmd/agent`,
`internal/tools`) pass the REPO ROOT (`docclass_gate_test.go:84`,
`corpus_faces_agree_test.go`'s `facesFixture`, `docindex_gate_redproof_test.go`,
`docindex_blank_line_gate_test.go`'s `blankLineFixtureRepo` — `git init` at
`:78`, `cli_doccheck_test.go` — `docCheckGit … init` at `:209`/`:587`, and
`task_finish_docclass_test.go`'s real repo+worktree fixture) or a fixture that is
already a repository. **None of the two repo-less helpers is ever handed to a
walking surface.**

## Honest limits of that audit

- It is a **source census plus a per-fixture reading**, not a call-graph
  analysis: a fixture that hands its root to ANOTHER fixture which then reaches a
  walk would not be caught by reading the table alone. The mechanical guard that
  DOES cover that case is the whole package's suite — after the fix, a repo-less
  root anywhere on a walking path is refused loudly instead of being silently
  accepted, which is why the audit can be a reading rather than a proof.
- A fixture root that is a SUBDIRECTORY of a repository would also be accepted
  (git finds the enclosing repo); that is the same property `Walk` relies on in
  production and is deliberately not treated as a defect.
