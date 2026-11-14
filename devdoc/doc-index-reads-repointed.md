<!-- doc-class: historical -->

# Task Report: the cmd/agent manifest reads are re-pointed at the doc-class header (route (a))

## Completed

**Route taken: (a)** — each former `devdoc/DOC-INDEX.tsv` read is re-pointed at the artifact that
replaced it, the `<!-- doc-class: … -->` header on **line 1 of the document**, parsed through
`internal/devdoc` (`devdoc.FirstLine` + `devdoc.HeaderClass`/`ParseBanner`). Route (b) — deleting a
check whose subject is gone — was **not** needed, because every one of the three checks still has a
subject (PROJECT.md Rule 36: the header is the only record of a document's class, and no check reads
the table to answer it).

**Commit 475001d** (`fix(chat,tools): … the doc index stops being tracked`) untracked
`devdoc/DOC-INDEX.tsv` — `git rm --cached` plus a `.gitignore` row, on the operator's instruction,
because a generated table conflicts on every branch that adds a document. Three tests in `cmd/agent`
still opened it off disk and `t.Fatalf`'d on the missing file, so `./cmd/agent` was red on every
ground cut after that commit:

| test | former subject |
|---|---|
| `TestG4HelpSurfaceInventoryRowIsInTheTruthManifest` | `cli_session_help_surface_basic_test.go` |
| `TestTaskHelpSurfaceInventoryRowIsInTheTruthManifest` | `cli_tasks_help_surface_basic_test.go` |
| `TestMergeUnionRowCheckOnTheRealManifest` | `merge_union_rows_test.go` (the third site — a briefing naming only two misses it) |

**Re-measured HERE, on this ground.** HEAD is `e717377` and commit **`96f9886`**
("the help-surface witnesses read the doc class off line 1, not a table 475001d untracked") is an
**ancestor of it** — `git merge-base --is-ancestor 96f9886 HEAD` exits 0. So the re-pointing had
already landed on the integration branch before this ground was cut; this run's job was to *verify*
it rather than to author it a second time, and it did verify it by measurement, not by reading a
commit message:

- all three tests **PASS** with their re-pointed readers (`-run` scoped run, output below);
- the property each was written for is still live: `TestDevdocClassOfCatchesAnUnclassifiedDoc`
  exercises the package's one reader (`devdocClassOf`, `cmd/agent/devdoc_class_header_test.go`) and
  passes, and the real-corpus arm resolves the shipped tree —
  `merge_union_rows_test.go:177: devdoc: 2407 tracked document(s) resolved, 763 of them normative,
  every one declaring its class on line 1`;
- **no `t.Skip`** was introduced or is present on this path: a document with no banner is a
  `t.Fatalf` naming the file and the illegal first line (Rule 27/15 — a silent skip is a deleted
  test). The red run below shows exactly that refusal.

Why the third test is the interesting one: `mergeUnionCheckRows` still knows the table's *shape*
(the unioned-bytes fixtures above it feed it synthetic rows, which is legitimate — they own their
fixture), but the **real-corpus floor** no longer reads a file on disk. It walks the tracked prose
corpus and asks each document for its own class, with `mergeUnionRowsReadFloor` (100) keeping a scan
that looked at nothing from passing as a clean corpus.

## Not Done

- **No production or test source change was required on this ground**, so this branch carries no
  code commit: the refactor is `96f9886`'s, and this ground's HEAD already contains it. Re-writing
  the same change would have been a duplicate edit, not a fix. What this run contributes is the
  re-measurement, the red→green proof taken on *this* tree, and this report.
- Not touched: the prose sites that still *mention* `devdoc/DOC-INDEX.tsv` as history
  (`internal/standards/removal_audit_shell_gate.go`, `internal/tools/header_hook.go`,
  `cmd/agent/merge_union_rows_test.go`'s own "what this used to read" comment). Each states a
  historical fact and none of them reads the file to answer a class, so Rule 36 is satisfied;
  PROJECT.md Rule 36 and `devdoc/doc-class-lives-in-the-file-header.md` already carry the corrected
  "no longer tracked" wording.

## Verification

- **Build**: `go build ./...` — see `## Evidence` for the exit status measured.
- **Tests**: `go test ./cmd/agent/ -count=1` (whole package, background job) — see `## Evidence`.
  Scoped arms: `go test ./cmd/agent/ -count=1 -run
  'TestG4HelpSurfaceInventoryRowIsInTheTruthManifest|TestTaskHelpSurfaceInventoryRowIsInTheTruthManifest|TestMergeUnionRowCheckOnTheRealManifest|TestDevdocClassOfCatchesAnUnclassifiedDoc' -v`
  → all four `--- PASS`.
- **Ran** (red proof, verbatim): the two normative inventory documents' banners deleted from line 1
  with `sed -i '1{/^<!-- doc-class: /d}' devdoc/gopi-session-help-surface-inventory.md
  devdoc/gopi-task-help-surface-inventory.md`, then
  `go test ./cmd/agent/ -count=1 -run 'TestG4…|TestTask…|TestMergeUnionRowCheckOnTheRealManifest'`.
- **Result**: red at all three sites, each naming the file and its illegal first line — then restored
  and green again. Full output in `## Evidence`.
- **Commit**: `96f9886` (the re-pointing this run re-measured). This report is the branch's own
  commit.

## Evidence (self-filed)

**1. HEAD carries the re-pointing.** `git merge-base --is-ancestor 96f9886 HEAD` → exit 0, printed
`96f9886 IS ancestor of HEAD(master)`; `git rev-parse HEAD` → `e7173778c7ca…`.

**2. Green, scoped (measured on this ground before any mutation):**

```
=== RUN   TestG4HelpSurfaceInventoryRowIsInTheTruthManifest
--- PASS: TestG4HelpSurfaceInventoryRowIsInTheTruthManifest (0.00s)
=== RUN   TestTaskHelpSurfaceInventoryRowIsInTheTruthManifest
--- PASS: TestTaskHelpSurfaceInventoryRowIsInTheTruthManifest (0.00s)
=== RUN   TestDevdocClassOfCatchesAnUnclassifiedDoc
--- PASS: TestDevdocClassOfCatchesAnUnclassifiedDoc (0.00s)
=== RUN   TestMergeUnionRowCheckOnTheRealManifest
    merge_union_rows_test.go:177: devdoc: 2407 tracked document(s) resolved, 763 of them normative, every one declaring its class on line 1
--- PASS: TestMergeUnionRowCheckOnTheRealManifest (0.02s)
ok  	github.com/nick/gopi/cmd/agent	2.200s
```

**3. Red proof — delete the banner, the properties go red BY NAME (not by a missing-table error):**

```
--- FAIL: TestG4HelpSurfaceInventoryRowIsInTheTruthManifest (0.00s)
    cli_session_help_surface_basic_test.go:547: devdoc/gopi-session-help-surface-inventory.md:
        no doc-class header on line 1 — the first line must be "<!-- doc-class: normative -->"
        (classes: [normative historical roadmap index-meta]); got "# The session-inspection + resume group's help-surface inventory (G4 of …)"
--- FAIL: TestTaskHelpSurfaceInventoryRowIsInTheTruthManifest (0.00s)
    cli_tasks_help_surface_basic_test.go:274: devdoc/gopi-task-help-surface-inventory.md:
        no doc-class header on line 1 — … (classes: [normative historical roadmap index-meta])
--- FAIL: TestMergeUnionRowCheckOnTheRealManifest (0.02s)
    the check reports 2 finding(s) against the repo's OWN tree:
      devdoc/gopi-session-help-surface-inventory.md: no doc-class header on line 1 — …
      devdoc/gopi-task-help-surface-inventory.md: no doc-class header on line 1 — …
FAIL	github.com/nick/gopi/cmd/agent	2.133s
```

**4. Restore, anchored to a REVISION (Rule 5's criterion — an index-relative diff proves nothing):**

```
git checkout -- devdoc/gopi-session-help-surface-inventory.md devdoc/gopi-task-help-surface-inventory.md
test "$(git hash-object $p)" = "$(git rev-parse HEAD:$p)"
RESTORE-OK (revision-anchored) devdoc/gopi-session-help-surface-inventory.md
RESTORE-OK (revision-anchored) devdoc/gopi-task-help-surface-inventory.md
```

**5. Green again after the restore**, same scoped invocation → `ok github.com/nick/gopi/cmd/agent
1.952s`; and `git status --porcelain` printed nothing (Rule 30) before this report was written.
