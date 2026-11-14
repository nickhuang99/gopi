<!-- doc-class: historical -->
> **RETIRED — this page is HISTORY, not machinery.** `internal/tools/merge_config_anchor_gate.go`
> and `internal/tools/merge_config_anchor_gate_test.go` were deleted whole in commit
> `5a35f6104` (*refactor(docclass): delete the document-pipeline corpus package (slice 3/5)*),
> together with the `internal/docclass` package they belonged to. **Do not run a command this
> page shows**: the files it names do not exist, and `go test ./internal/tools/ -run
> 'TestMergeConfigAnchorGate'` matched NOTHING for months after the deletion — it printed
> `[no tests to run]` and exited 0, a vacuous green.
>
> The LIVE check **was** the census generator's own delta leg plus the arms restored under the
> same documented names (they drove the generator, not a merge-time lane). **BOTH ARE GONE
> (2026-11-05)**: `78bdf05a` (operator ruling) deleted the whole documentation-citation family —
> `internal/standards/testdata/config-anchor-census.go`, the `.tsv` it wrote,
> `citation-census.go`, `citation-selector-census.go` and the gates that drove them — and
> `b8cad761` deleted `internal/tools/merge_config_anchor_gate_test.go` with the last census arm.
> `go run` on the generator prints `stat … no such file or directory`; `-run
> TestMergeConfigAnchorGate` prints `[no tests to run]` and exits 0, an EMPTY GREEN that is not
> a pass. **Nothing judges CONFIG.md's anchors today** — the anchor rules are prose authors
> obey, and a reader must not run either command this page used to give.
>
> The merge-time SIGNAL this page describes (a rotted anchor found in a merge TARGET, reported
> as one bug row) is **gone**: nothing in the tree reads CONFIG.md's anchors at merge time any
> more. `internal/configanchor/anchor.go` says so in its own package doc.

# The CONFIG.md anchor audit: settling a citation with a WITNESS, not a class

Row `01a13999-4785-7c42-8a5a-c3a51335b0e8` (3/3 of the config-anchor epic), branch
`task/01a1399947857c428a5ac3a51335b0e8`, base `88d664b38`. Items 1/2 built the census
(`internal/standards/testdata/config-anchor-census.go` + `.tsv`) and the merge-time leg
(`internal/tools/merge_config_anchor_gate.go`); this item is the AUDIT those two exist for.

## What was actually wrong, in numbers

`CONFIG.md` publishes 194 distinct `path.go:NNN` anchors. The gate's A4(b) declares a
target that is *a line of ordinary content naming nothing of the row's* out of scope, and
that is 84 of the 194 (COMMENT_CONTINUATION=53 + NAME_MENTION=31). **Out of scope is not
"correct"** — it is "the gate cannot tell a deliberate point-at-the-region anchor from a
rotted ordinal on that shape", and the shape is where all measured drift lives.

## The witness that turns the class question into a measurement

A `CONFIG.md` row names its own knob (`GOPI_...`), and the cited Go file spells that token
as a quoted literal on the line that READS it. So each anchor is settled against a
witness, derived top-down by the census generator (never typed):

| verdict | n | meaning |
|---|---|---|
| `AT-CLAIM` | 24 | the target line itself names the row's knob — correct |
| `AT-REGION` | 6 | the target sits in the comment block that names the knob — accepted on purpose |
| `CARRIER-AT:n` | **0** | drift: the text moved under the number — the audit's whole worklist |
| `REGION-UNWITNESSED` | 125 | no carrier in the cited block — accepted, nothing to witness |
| `NO-KNOB` | 39 | the row names no `GOPI_...` token — nothing to witness |

The half is written into the table as `#`-prefixed lines, so `ParseRows` and the merge-time
leg skip it exactly as they skip the header and the four-column data rows are untouched.

## The three conditions of the drift verdict, and the two counter-examples that set them

The first draft reported SIX `CARRIER-AT`. Measured against the rows, only three were real:

- **Kept**: `cmd/agent/main.go:1016 → :1009` (the cited line was `} else {`; the row's
  `os.Getenv(config.PromptEnvVar)` read is at `:1009`), `cmd/agent/cli_tasks_worker.go:81 →
  :82`, `internal/tools/task_finish_premerge.go:76 → :77` (both cited a doc-comment line
  while the row's env constant — the constant IS the row's claim — sits one line below).
- **Excluded**: `internal/tools/memory_sync_reconcile.go:58` cites the DEFAULT constant —
  a line of CODE, with the env constant two lines ABOVE; `internal/scratch/gc.go:118` is a
  second claim of a multi-anchor row (`gc.go:116` is the first) with its carrier above it.
  Both are correct citations, and reporting them would have been this audit inventing work.

So `CARRIER-AT` requires all three: the target is a COMMENT; the comment block it sits in
names the knob NOWHERE; and the knob's ONE carrier is BELOW it within 3 lines. A wider rule
red the correct majority — the same lesson the gate's own A1 already records.

## Cost of re-deriving, and where the recipe lives

The audits a reader would otherwise repeat by hand are written into `CONFIG.md`'s
ANCHOR SEMANTICS section as **A5** (the verdict vocabulary, the witness, the three
conditions and the counter-examples) and **A6** (the provenance criterion: re-point when the
sentence describes the CURRENT tree, FREEZE when it describes an older one — re-pointing the
latter fabricates a citation). A6 also carries both named anchors' histories, measured with
`git show <commit>:<file> | sed -n Np`:

- the settings read: `486` → `706` (`b34c7f86d`) → `711` (`ab428b993`, after `612016700`
  moved the file under it);
- the `prompt` row's read: `805` → `1016` (`e75bd0959`) → `1009` (this audit).

## Not done (honest)

- The 125 `REGION-UNWITNESSED` and 39 `NO-KNOB` anchors are ACCEPTED, not verified: the
  witness predicate cannot see them, and no prose judgement was substituted for a
  measurement. Their residual risk is stated in A5 rather than closed.
- `CONFIG.md:111`'s `cmd/agent/main.go:691` and `:911`'s range `684-743` are range/first-line
  citations the census resolver leaves UNRESOLVED by design; they carry no verdict.
