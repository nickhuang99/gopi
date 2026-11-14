<!-- doc-class: historical -->
# The assertion-totality gate's `why:` print and its UNCLASSIFIED line are about DIFFERENT assertions

**Class:** historical (a past run's record — the measurement below is frozen at the
revisions it names).
**Filed from:** task `01a13461-ec9a-7c5c-be32-7e0fd40a7e1f` (Rule 41 finding, filed by
the run of `01a134d1`), dispatched to the system layer per Rule 28.

## The report, verbatim (task `01a134d1-8e3e-7dde-b7dc-9a58a5ddc584`)

```
    assertion_totality_audit_test.go:152: internal/tools/mergeset_docclass_test.go:TestMergesetDocclassAbstainsWhenTheUnionNamesNoManifestPath (internal/tools/mergeset_docclass_test.go:225): negative-totality assertion "a landing that names no manifest path must add no run, got %+v" is UNCLASSIFIED.
FAIL	github.com/nick/gopi/internal/tools	3.032s
```

The filer's reading: `:152` calls that assertion UNCLASSIFIED **while the same run's
follow-on lines print `auditTotalityAllow` `why:` rationales** for that same assertion —
i.e. one run contradicting itself. It reproduced identically at the reframed epic's
revision `b888461f2`.

## Root cause: no assertion is in both halves — the two halves name NEIGHBOURS

The gate has two independent lookup halves, and each one is fed by a different walk:

| half | gate code | what it walks | what it prints |
|---|---|---|---|
| the verdict | `assertion_totality_audit_test.go` `:152` (the unclassified loop, `:143-150`) | **only** the hits the verdict loop did not accept | `… is UNCLASSIFIED` + the remedy |
| the data report | the same file `:183` (the classified loop) | every hit `auditClassified` **accepted** | `classified <key> <pos> via <carrier>` + `claim:` + `why:` |

So a site whose own assertion is UNCLASSIFIED can never also get a `why:` print: the
`:183` walk is the *complement* of the `:152` walk, and `auditClassified`
(`:668-686`) takes the map first, then the site's `why:`. "Printed a `why:` for key K"
and "K is UNCLASSIFIED" are mutually exclusive — the contradiction cannot occur for one
key.

What the filer actually saw was **same-NAME siblings**, and the mergeset-docclass leg has
three functions whose names all begin `TestMergesetDocclass…`:

| assertion | `why:` carrier | UNCLASSIFIED at `b888461f2`? |
|---|---|---|
| `mergeset_docclass_test.go:TestMergesetDocclassAbstainsWhenTheUnionNamesNoManifestPath` (assertion `:226`) | **none** — the directory has no `// why:` comment at all (only a doc comment on the function) | **yes** — the `:152` line |
| `mergeset_docclass_test.go:TestMergesetDocclassRunsRequiresTheCarrierWhenTheUnionNamesAnAnchoredSource` | map row (long) | no |
| `mergeset_docclass_test.go:TestMergesetDocclassGatedDocumentAloneObligesTheCarrier` | map row (long) | no |

Two coincidences made neighbours read as one key:

1. **The `why:` text is inside the ASSERTION's own message.** The judged assertion reads
   `"a landing that names no manifest path must add no run, got %+v"`, and the site the
   `why:` print belongs to (`mergeset_gendoc_test.go:TestMergesetUnionGateIsSilentForALandingThatTouchesNoAnchorSource`,
   whose message is `"a landing that touches no published anchor source must oblige no
   run, got %+v"`) follows it in the sorted output. A reader scanning for
   "a landing that … no … run" lands on the printed `why:` and never notices it belongs
   to the *touches-no-anchor-source* sibling, not to the *names-no-manifest-path* key.
2. **The gate's own remedy line quotes the reason FORMAT.** `:156-157` print
   `auditTotalityAllow["<the key>"] = "<correct | over-broad: which half> — <evidence>"`
   — a `why:`-shaped sentence carrying the exact key that is UNCLASSIFIED. A filer
   grepping their scrollback for the key therefore finds it twice: once in the verdict,
   once inside the remedy's template.

The stale line number is a residue of the same confusion: `:225` was the assertion's
line when the failing run happened (`8a017174d`, 2026-10-13 12:33); a later commit in the
same family shifted it to `:226`, so any `:225` in an old scrollback no longer resolves.

## The row was genuinely absent at the reported revision — and the gate was RIGHT

Measured, not inferred:

- `git show b888461f2:internal/tools/assertion_totality_allow_test.go | grep -c
  TestMergesetDocclassAbstainsWhenTheUnionNamesNoManifestPath` → **0**. No map row, and
  no `// why:` at the site: the verdict was **correct**.
- `git log --oneline --all -- internal/tools/mergeset_docclass_test.go` → the assertion
  landed in `8a017174d` ("feat(mergeset): derive a devdoc-anchor edge into the
  merged-result run set") **without a classification carrier**, while the allow-file
  change sat uncommitted in a worktree whose owner was gone.
- **No revision ever carried both the assertion and its row**: for every revision that
  touched the test file, the allow file's count for that key is 0. The row first appears
  in `96056cdcc` ("test(tools): classify the mergeset-docclass abstention site",
  2026-10-13 17:11:43, delivered in merge `e72745a1c`, row `01a133d3`).

So the `git diff master...HEAD` cross-check in the filed brief ("the diff does not contain
the file") was **not** evidence the finding was pre-existing — it was evidence the
assertion was *unclassified*, because the fix lived uncommitted in that worktree.

## What this run changed: the remedy string, and nothing else

At this run's ground (`82f8b95`, and identically at `origin/master`) all four involved
files are byte-identical and the row is present:

| file | 82f8b95 | HEAD (before `ebe3dc935`) | origin/master |
|---|---|---|---|
| `assertion_totality_audit_test.go` | `fbdfa620` | `fbdfa620` | `fbdfa620` |
| `assertion_totality_allow_test.go` | `45d859df` | `45d859df` | `45d859df` |
| `mergeset_docclass_test.go` | `f7df76a7` | `f7df76a7` | `f7df76a7` |
| `mergeset_docclass.go` | `aaf9712b` | `aaf9712b` | `aaf9712b` |

`go test ./internal/tools/ -run TestAssertionTotalityAuditGate -count=1` → `exit=0` on
this ground, and `exit=0` on a **read-only** extraction of `origin/master`
(`git archive origin/master | tar -x -C <scratch>`, never a foreign worktree — Rule 29).
The red exists only below `e72745a1c`. **No predicate, carrier or permit edit is owed** —
the classification is sound — and the run's one edit (`ebe3dc935`) is confined to the
remedy string, whose blob becomes `693057b70`; the predicate, both carriers,
`auditWhyLookback` and every permit are byte-identical.

## Why the gate file and its message were left alone — and then narrowed after all

The tidy fix would be to make the remedy line unambiguous — e.g. stop quoting the reason
format, or emit the remedy for a key only once. An earlier pass at this diagnosis
**deliberately rejected** it, for the reason the message exists at all: the remedy is the
gate's whole interface. It must give a reader who has never met this gate the *exact* key
to add and the *exact* row format to write; `auditTotalityAllow[%q]` + the
`"<correct | over-broad: which half> — <evidence>"` shape is what makes the remedy
copy-pasteable.

**That trade was re-opened and settled the other way** (`ebe3dc935`), because the evidence
above localises the defect to exactly this string rather than to the gate's logic: the
template put the requested key and a `why:`-shaped `<the same> — <evidence>` on **one
line**, which is precisely the shape a reader files as "an allow-list rationale for the
assertion just called UNCLASSIFIED." The narrowing keeps every copy-paste property —
the map assignment is still spelled verbatim, the `// why:` alternative still points at
the same lookback — and adds three things: the key is **UNCLASSIFIED, so it appears in NO
`classified …` line below**, the report loop names OTHER snippets, one file may hold
**same-named `Test*` functions**, and the read floor (`auditAllowMinReasonLen`) is printed.
What was forbidden and remains untouched: `auditClassified`'s predicate, both carriers,
`auditWhyLookback`, and every permit in the map. The honest statement is therefore
narrowed but not eliminated — a `why:`-shaped sentence still sits in the same scrollback
as real `why:` lines from `:183`, so the message is **more self-labelling, not fully so**.


## What could not be separated

- **Which exact line the filer read as the second half.** The filing quotes only the
  verdict and the `FAIL` line; the run's full output was not archived, so the
  contradictory pair cannot be reproduced line-by-line as the filer saw it. The two
  candidates above (the near-neighbour `why:` print, and the remedy's own quoted format)
  are both consistent with the quoted evidence, and the *mechanism* (complementary walks,
  so no key can be in both) is proven from the gate's source and the sort order — not
  inferred from the filer's scrollback.
- **Whether the filer's terminal carried extra lines** (a `-v` run, a
  `-run` regex matching the two sibling proofs) — unobservable now.
- The neighbouring `TestPublishedHelpPagesStayWithinTheirUsageBudget/merge` red from the
  same filing is a separate gate in `cmd/agent` and is **not** addressed here.

## Check

```
go test ./internal/tools/ -run TestAssertionTotalityAuditGate -count=1        # exit=0
git diff --stat 82f8b95 HEAD -- internal/tools/assertion_totality_audit_test.go
    # 7 +- — the remedy string and its arg ONLY (predicate/carriers/lookback/permits untouched)
git status --porcelain                                                       # empty
```
