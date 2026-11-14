<!-- doc-class: historical -->

# doccheck vs TestDocManifestCoversEveryDevdocFile: one tree, two verdicts

**What this document asserts, and how to re-check it:** `internal/docindex.CheckCorpus` (the
engine behind `gopi tool doccheck`) and `docclass.Load` (the reader
`TestDocManifestCoversEveryDevdocFile` uses) reach the same verdict about the same
manifest. Check it with
`go test ./internal/docindex/ -run TestCorpusFacesAgreeOnAnUndeclaredClass` and
`go test ./internal/standards/ -run TestDocManifestCoversEveryDevdocFile`.

## What was reported

`gopi tool doccheck` named `devdoc/archive-residual-refs-reclaim-check.md` as
`file-with-no-row` while — in the same worktree, at the same moment — the file
existed, its `DOC-INDEX.tsv` row existed, and the coverage gate was PASS.

## Root cause: TWO facts, and only one of them is a code defect

### Fact 1 (temporal, not a defect): the reading predated the row

Evidence, from the reporting run's own session log
(`~/.gopi/sessions/01a14a08-5878-7000-95fb-8ff63ee33be4/`, the JSONL transcript):

| time | event |
|---|---|
| 23:50:15 | the devdoc file is written (4414 bytes) — **untracked**, no row yet |
| 23:52:29 | `gopi tool doccheck > evidence/doccheck.txt` starts (a background job) |
| 23:53:23 | `grep -c 'archive-residual-refs-reclaim-check' devdoc/DOC-INDEX.tsv` → **0**; `git status --porcelain` → `?? devdoc/archive-residual-refs-reclaim-check.md` |
| 23:53:34 | `gopi tool doccheck --fix` **writes the row** |
| 23:56:17 | the commit lands file+row together |
| 00:02:02 | `doccheck.txt` mtime — the report file finishing its slow stored-cell render |

So the artefact the report quoted was a snapshot taken **while the corpus really
was unclassified**. The `file-with-no-row` sentence was TRUE of the tree at
23:52–23:53. It is not a false positive, and no code change can make it one. What
made it *look* like a contradiction is that the report compared it against
gate/file/row checks taken **four to eleven minutes later** — after the fix run,
after the commit. Two observations at two different times were printed as one
contradiction.

**The lesson (Rule 41 evidence discipline):** a claim of the form "face A and face
B disagree" must carry one timestamp per face, and the two must be the same
instant. Here they were 10 minutes apart on a corpus that was being actively
written.

### Fact 2 (real code defect): the faces validated different things

While hunting Fact 1, a genuine split was measured, and it is the one this change
fixes. `docclass.Load` — the coverage gate's reader — **validates the class
vocabulary** and refuses the entire manifest over an undeclared class:

```
$ go test ./internal/standards/      # via docclass.ParseManifest
… DOC-INDEX.tsv:4: unknown class "bogusclass" — declare it in internal/docclass.Classes
```

`CheckCorpus` read the same row, asked about columns, files, reason length and
evidence tier — **and never asked whether the class was declared**. Measured
(RED, before the fix), on a two-row fixture whose only defect is the class:

```
FACECHECK err=<nil> gaps=0
FACEGATE rows=0 err=… unknown class "bogusclass" — declare it in internal/docclass.Classes
```

`doccheck` printed "0 gap(s) … the devdoc corpus is consistent" for a corpus the
gate **could not load at all**. That is one tree, two verdicts, in its purest
form — and the consequence is exactly the reporting run's complaint: an operator
sent to add a row that already exists, against a manifest that is broken for an
entirely different reason.

## The fix

One place, one truth source: `internal/docindex/docindex.go` — `CheckCorpus` now
asks the vocabulary question through the same predicate the gate uses
(`docclass.IsClass`), and reports it as its own shape.

- **New gap shape** `GapUnknownClass = "unknown-class"`, deliberately NOT folded
  into `GapMalformedRow`: the line is perfectly readable, so the remedy is to
  declare or rename the class, not to repair columns. Two remedies, two names.
- The row is still fully read (path/class/reason/evidence), so the other arms
  keep answering about it.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK.
- Tests: `go test ./internal/docindex/ -run TestCorpusFacesAgree` — 3/3 PASS
  (the two pre-existing arms plus the new one).
- **Red proof** (guard removed, test kept, `GapUnknownClass` const kept so the
  package still compiles):

  ```
  --- FAIL: TestCorpusFacesAgreeOnAnUndeclaredClass
      THE TWO FACES OF RULE 36 DISAGREE ABOUT ONE TREE: doccheck reported 0 gap(s) []
      while the coverage gate's reader REFUSED it: … unknown class "unclassified"
      — declare it in internal/docclass.Classes
  ```

- **Green** (guard restored): `ok github.com/nick/gopi/internal/docindex`.
  The restore was verified by content identity against the pre-mutation snapshot
  (`git hash-object` equal on both sides) — not by an index-relative diff.
- Gate on this worktree: `TestDocManifestCoversEveryDevdocFile` PASS,
  `2634 rows cover 2634 files`.

## Not done (honest)

- **Fact 1 cannot be fixed in code.** The reporting surface prints one snapshot;
  the operator read it beside later measurements. What could be added is a
  timestamp on doccheck's own report so a snapshot can never be mistaken for a
  present-tense verdict — that is a separate change to `cmd/agent/cli_doccheck.go`
  and is NOT in this commit. It is filed rather than smuggled in.
- The stored-cell drift arm still exits 1 on this tree (43 text-ambiguous anchor
  rows, 0 gaps). That is a pre-existing, branch-independent state owned by
  separate rows (`01a14a47`, `01a13d96`); this change neither causes nor clears it.
