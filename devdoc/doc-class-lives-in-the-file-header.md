<!-- doc-class: normative -->

> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.

# The doc-class lives in the file's header

**Status:** landed, and the deletion it deferred has happened. The chain that
owned the removal (`01a16043-2b13`, `[1/5]`) is `completed`, and its products are
gone from this tree: `internal/docindex/`, `internal/docclass/` and
`tools/docclass/` do not exist, and the `gopi tool doccheck` verb is not
registered (`cmd/agent/cli_doccheck.go` is absent). What SURVIVED that deletion
is the half running code asks — `internal/devdoc` (the banner parser, `IsProse`)
and `internal/tools/header_hook.go` — and the sections below name it under those
paths, which is where a reader of this rev will find it. Re-measured 2026-10-22.
**Scope:** the CLASSIFICATION mechanism only. `devdoc/DOC-INDEX.tsv` is still a
tracked file and the merge path still routes conflicts in it
(`cmd/agent/merge_union.go`, `internal/tools/manifest_union_gate.go`), but no
check reads it for a document's class any more: the class is the file's own
first line. §6 keeps the overlap record as it stood when this landed.

## 1. What a devdoc's truth class is now

Every markdown document under `devdoc/` opens with its class on **line 1**:

```
<!-- doc-class: historical -->
```

Four classes, unchanged: `normative`, `historical`, `roadmap`, `index-meta`
(`devdoc.Classes` in `internal/devdoc/devdoc.go`). The header is the **only**
source of truth. Nothing else records a document's class, and no reader consults
anything else.

Non-markdown files under the corpus (captures, fixtures, TSVs, scripts) are **out
of scope by a written rule**, not by neglect: a record that is not prose cannot
make a standing claim, it is historical by definition, and a banner comment in a
shell script is not a statement a reader would meet.
`devdoc.IsProse` (`internal/devdoc/header.go`) is that rule, and the gate *calls*
it rather than re-spelling it (Rule 13/14).

## 2. Why the table had to go

Three costs the manifest carried, none of which any gate could repair:

1. **It was a second artifact** every author had to edit in step, so two branches
   that each add a document both append to `devdoc/DOC-INDEX.tsv` and the merge
   conflict is the table's mechanism announcing itself. The merge path had a
   dedicated union lane and a pre-land gate whose only subject was that file.
2. **It was invisible where the reader stands** — the class was a column in a
   file, not the top of the page.
3. **Producing it was derived work by a subprocess.** `tools/docclass/classify.py
   --only <path>` measured **14.76 s** for ONE path (write-free `--dry-run`), and
   the write path was reported at ~29 s per document. A hook that expensive had
   to be pushed outside the write's deadline, given its own bound
   (`ClassifyHookTimeout`) and a substitutable seam — machinery that existed only
   to survive the cost.

## 3. The mechanism

### Reader — `internal/devdoc/header.go`

| function | what it answers |
|---|---|
| `HeaderClass(firstLine) (class, error)` | parse line 1 against the closed vocabulary |
| `ReadHeaderClass(path) (class, error)` | same, from disk |
| `IsProse(rel) bool` | is this path subject to the header rule |

(`HeaderOf(root, rel)` and `CorpusHeaderScan(root)` lived beside these in the
deleted `internal/docclass`; the corpus SWEEP is now the gate below, which owns
the walk and the counts because that is where the verdict is made.)

The parse is exact: a banner whose payload carries extra prose
(`<!-- doc-class: historical — classified in … -->`) is an **error**, not a near
miss, because a value with commentary in it cannot be compared against another
reader's answer. Measured: two documents in this corpus carried exactly that
shape and silently compared unequal to their own class in every consumer.

### Gate — `internal/standards/docclass_corpus_gate_test.go`

Three manifest gates (coverage / banner agreement / row shape) are replaced by
ONE header gate that walks `devdoc/**/*.md`: `TestEveryCorpusDocDeclaresItsClassInItsHeader`,
the name PROJECT.md Rule 36 has been citing all along.

**That name cited nothing for months.** Until this file landed, `go test
./internal/standards/ -run TestEveryCorpusDocDeclaresItsClassInItsHeader`
matched no test: it printed `no tests to run` and exited 0, so a rule whose
stated check was a vacuum read green forever. The gate below is what that
command now runs, re-measured 2026-10-22 on this rev (`walked 2538 file(s) under
devdoc/, judged 2368, clean 2368; normative=734; historical=1622; roadmap=12;
index-meta=0`).

It fails, naming `<path>: <reason>` one line per file, on all three shapes
`HeaderClass` distinguishes — a missing banner, a malformed one (the prefix
opened and never closed), and a well-formed banner naming a value outside the
closed vocabulary. It prints its own accounting (files walked, files judged,
files clean, count per class), and it refuses a green that was not measured:
`judged` must clear `docCorpusScanFloor` (2000, slack against the measured
corpus) and a scan that judged NOTHING always fails. Its red proofs run the same
scan over fixture corpora, one defect per arm, so a predicate that stopped
naming a defect turns the suite red rather than reading green.

The gate only READS — it never writes a header, because the class is the
author's statement of what the page is.

### Write path — `internal/tools/header_hook.go`

The hook is now **a string test on bytes the caller already handed us**:

- `write` → `writeImpl` calls `amendDocHeader(path, content)` **before** the
  bytes land. A corpus markdown file with no legal line 1 gets the banner of the
  fresh class (`historical` — what a run's record is by nature); a file already on
  disk that declares a class keeps **its own** class, so a body-only edit cannot
  drop a declaration.
- `edit` → `preserveDocHeader(path, old, new)` restores the class from the
  file's **own old bytes**. This is the arm a naive implementation misses: an edit
  lands only the replaced span, so a header outside the match window must be put
  back — and it must be put back with the document's class, never a default, or
  an edit to a `normative` page would reclassify it as a run's record.
- Off-corpus paths (a `.md` outside `devdoc/`, a non-markdown record inside it)
  pass through byte-identical.

Nothing runs after the rename. `ClassifyHookTimeout`, `ErrClassifyHookTimeout`,
the `classifyWrittenFile` seam and `runClassifyWrittenFile` are **deleted**: they
existed to survive an expensive post-write leg that no longer exists.

## 4. Measured cost

| leg | before | after |
|---|---|---|
| classify one path (`classify.py --only … --dry-run`, write-free) | **14.76 s** | — |
| write-path hook per document (reported) | **~29 s** | — |
| `classify.py . --dry-run --only <one path>` re-measured here, and the close-out leg it fed | **14.30 s** | — |
| close-out class step, header-shaped (read line 1 of the branch's added docs) | — | **0 subprocess, 4 tests in 0.6 s total** |
| `amendDocHeader` alone (locate corpus + parse + prepend) | — | **19.3 µs** |
| whole `tools.Write` of a corpus doc, incl. fsync+rename | — | **3.0–3.6 ms** |

Commands are in the change's report (`## Verification`), including the
`headerHookCostBound` test arm that fails if a subprocess ever comes back inside
the write path.

## 5. The corpus

2368 markdown documents, all classified (re-measured 2026-10-22; the numbers
below are the ones this change landed with): 1622 `historical` (1611 already
carried a banner), 734 `normative` + 12 `roadmap` + 2 malformed banners repaired
— the classes were copied from the existing manifest rows by a **throwaway script
that is not checked in** (0 added configuration, 0 new dependency; Rule 35). The
gate reports the counts on every run, so the corpus state is readable without
opening the files.

## 6. Overlap with the queued deletion chain `01a16043-2b13`

**That chain has run.** `01a16043-2b13-7124-a70e-405afbf59fab` is `completed`
(`gopi task show`, 2026-10-22), and its products are measurable in this tree:
`tools/docclass/` absent, `internal/docindex/` absent, `internal/docclass/`
absent, `cmd/agent/cli_doccheck.go` absent. What it did NOT delete is the file
`devdoc/DOC-INDEX.tsv` itself, which at the time was still tracked and still had
a merge union lane — see §6.1 for what that leaves live. **Since commit
`475001d` (2026-10-24) it is no longer tracked either**: `git rm --cached` plus a
`.gitignore` row, on the operator's instruction, because a generated table
conflicted on every branch that added a document. A reader of this rev should
read §6.1's "tracked file" as history, and it says so there.

**The change this page documented touched none of them.** It made the header the
authority, so that the deletion was a pure subtraction rather than a redesign —
and that is how it arrived.

What the chain's arrival moved, and what a reader of THIS rev should know:

- **The close-out leg is gone with it.** `finishCleanupDocClassSync`
  (`internal/tools/task_finish_docclass.go`) has no definition in this tree —
  `grep -rn "func finishCleanupDocClassSync" --include=*.go .` prints nothing.
  The header question is asked by the gate above, on the whole corpus, rather
  than by a close-out leg on the branch's added files. Nothing in the write or
  close-out path spawns python.
- **The write path is the only other caller.** `internal/tools/header_hook.go`
  is the surviving consumer of `devdoc.IsProse` / `devdoc.HeaderClass`
  (`amendDocHeader`, `preserveDocHeader`), and it is in-process — no subprocess,
  so the `ClassifyHookTimeout` seam stayed deleted.
- **`docclass.ClassBannered` is gone**, and with it the asymmetry it carried:
  "every legal class is bannered" is now a property of the data (a banner IS the
  class) instead of a function's return value.
- **`tools/docclass/classify.py` and `banner.py` are gone from disk.** They are
  not "on disk, unconsumed" any more; they do not exist. Any document that still
  teaches a reader to run `python3 tools/docclass/...` is describing a mechanism
  this repository no longer has.

### 6.1 What the deletion did NOT remove

`devdoc/DOC-INDEX.tsv` was a **tracked file** (2539 lines) and the merge path
routed conflicts in it — `cmd/agent/merge_union.go` and
`internal/tools/manifest_union_gate.go` still name `devdoc.Path` and parse it
with `devdoc.ParseManifestLines`. **Commit `475001d` (2026-10-24) untracked the
file** (`git rm --cached` + a `.gitignore` row): the table is generated, so it
conflicted on every branch that added a document, and no production Go code read
a row from it. What survives is the merge lane's handling of the ARTEFACT where
one turns up, and `internal/devdoc`'s vocabulary for reading it. What is gone is
every reader that used a row to answer **what class a document is** — that
question is answered by line 1 of the document, and only there.
**Measured consequence on `./cmd/agent` (2026-10-24, task
`01a16dcb-b09e-7aef-96a5-f838e683631c`):** three tests still opened the file off
disk with `t.Fatalf` on a missing path, so the package was permanently red on
every ground cut after `475001d`; they now read the banner through
`devdocClassOf` (`cmd/agent/devdoc_class_header_test.go`) instead.

## 7. What a reader can rely on

- A document's class is on line 1 of the document. Always.
- `go test ./internal/standards -run TestEveryCorpusDocDeclaresItsClassInItsHeader`
  is the corpus-wide truth, and it NAMES failures.
- A new devdoc gets its header from the writer; the author who wants a different
  class edits line 1 (which is exactly the act that publishes a claim).
- Two branches that each add a document now merge without meeting.
