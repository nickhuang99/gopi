<!-- doc-class: historical -->
# The provenance of a forwarded row — decision, vocabulary, and the audit

**Task** 01a0daab-074c-7051-bf14-17868d8e89b8 · epic: *the retired `auto` alias
stamped by `task_submit_forward.go`*

## The defect

`internal/tools/task_submit_forward.go` stamped the **retired** spelling `"auto"`

- on the WIRE row (the copy the authoritative peer's ledger stores verbatim
  through `SyncTaskImportDelta` → `importTaskFreshTx` → `importTaskInsertTx`),
- on the LOCAL MIRROR ECHO row (the copy `task show` answers with on the
  submitting node).

`internal/tasksource.Normalize` reads `"auto"` back as **`merger`** (its
majority historical writer, and the only one still writing it when that package
landed). So every forwarded submit reached *both* ledgers narrated as a
**merge-path machine enqueue** — a role no merge ever played for it, and on the
very ledger that claims, runs and adjudicates the row. `task show` rendered it
`harness/external tooling (source=auto, no session recorded)`.

## The decision: a forward moves a row's HOME, never its AUTHORSHIP

The old comment justified the value only as "a COLUMN value the cross-project
guard's warning can be distinguished by" — it never asked what role a forwarded
row actually has. The role, decided here:

| copy | ledger | who is the row's author | value stamped |
|---|---|---|---|
| **wire row** | the AUTHORITATIVE peer | the submitter on the forwarding node — the agent that made the `task_submit` call, or the operator who typed `gopi task submit` | the submitter's own role (`opts.Source`, already canonicalized by `TaskSubmit`'s funnel: `agent` / `human` / `worker`) |
| **echo row** | the LOCAL MIRROR | nobody new — the copy itself is the transport's product | `tasksource.Forward` — *this ledger holds the mirror copy of a row that lives elsewhere* |

Why the submitter's role rides the wire row: the authoritative ledger is where
the row is **claimed, run and adjudicated**, and its provenance line must answer
*who filed this*. Stamping a transport/arrival role there loses the only fact
that ledger cannot reconstruct (the submitting node knows it; the peer was
never told). The journey is not lost — it is recorded where it belongs, in the
**tags** (`task-submit-forward` on both copies per `taskSubmitForwardKind`,
`forwarded-to:<peer>` on the local one). Tags describe the journey; the source
column describes the author.

The constant keeps the property its old comment claimed, on the copy where the
claim is true: the mirror's own provenance (`task-source=forward`) makes a
forwarded submit distinguishable from a merger row — which is exactly what the
retired alias destroyed.

## The vocabulary change (`internal/tasksource` — Rule 1/13/14)

Two values added at the ONE home, referenced never re-typed:

```go
Forward        = "forward"          // a mirror leg's forwarded row
AgencyProposal = "agency-proposal"  // the unattended improvement miner's row
```

Both wired through `lexical` (the validation list), `Normalize` (the
canonicalization switch) and `Policy()` (the refusal text) — the three lists a
new value must join in that package, plus the package doc's vocabulary table.

## The audit of the write side (the epic's acceptance grep)

`grep -rn '"auto"' --include=*.go internal/ cmd/ | grep -v _test.go` after the
change returns **no production writer and no production predicate** stamping the
alias. What the sweep found and fixed, beyond the epic's two named sites:

1. **`agencyscan.AutoTaggerSource` (`"auto"`)** — the miner files every
   improvement proposal through `internal/tools/agency.go`'s submit seam into
   the SAME funnel, so every proposal row was stored as `merger` and narrated
   as a merge path. Now `tasksource.AgencyProposal`.
2. **the `source` column's SQL DEFAULT (`'auto'`)** — two sites
   (`tasksSchemaTable`, `tasksP2Columns`) plus the ledger-rebuild projection's
   `colExpr("source", "'auto'")` fallback. A default is a WRITER: `TaskNewOID`
   inserted `f.Source` straight through, so a row created without an explicit
   role landed as the retired alias in the column. Default is now `''` —
   meaning "no role recorded", the shape `TaskShow` already handles
   (`submitted_by: —`) — and nothing silently claims `merger`.
3. **`syncRowIsMirrorNoise` (`memory_sync_delta.go`)** — a PREDICATE comparing
   the raw literals `"human"`/`"llm"`. It protects real tickets from the
   server's blind-flatten ruling, and `"llm"` is a retired alias: a row written
   today is `agent`, so the predicate classified today's real tickets as
   registration noise — the same class of defect (`be17a9bd` retired the alias
   from the writers, the predicate aged out of agreement). It now reads
   `tasksource.Normalize` and compares the constants. **Deliberately NOT
   widened**: whether `merger`/`worker`/`forward` rows with a briefing should
   also be protected is a *sync-ruling* decision (it changes which rows the
   server may flatten), not a provenance rename — the membership is unchanged.
4. **`taskSubmittedBy` (`tasks.go`)** — two new arms (`Forward`,
   `AgencyProposal`) so the renderer covers the whole vocabulary; a value with
   no arm would fall through to the raw string.
5. **Doc/comment sites that named the alias as live behaviour**:
   `TaskSubmitOptions.Source`'s field comment, `TaskMergeOptions.Source` /
   `TaskAdoptionOptions.Source` (both `"" = "auto"`), the schema comment's
   `llm|human|auto` list, and `internal/testutil/dbfixture.go`'s sample seed
   (a fixture narrating the miner as `auto`→`merger`).

## Verification

- **Red → green, both new tests.** With `taskSubmitForwardSource` reverted to
  `"auto"` and the wire row re-pointed at it, `TestForwardedRowCarriesTheSubmittersRoleNotATransport`
  failed with *"the forwarded row reads as "merger" (raw "auto") — a MERGE-PATH
  enqueue the merge path never made"*; with `AutoTaggerSource` reverted to
  `"auto"`, `TestAgencyProposalSourceIsItsOwnVocabulary` failed with
  *"proposal row source = "merger", want "agency-proposal""*. Both pass after.
- **The wire boundary was crossed for real** (Rule 31): a v1 task patch whose
  row carries `"source":"agent"` was imported into a fresh ledger through the
  real CLI (`gopi memsync --import-tasks`), then read back with the real
  `gopi task show` → `source: agent`, `submitted_by: LLM session … (task_submit)`.
  The echo patch (`"source":"forward"`, `forwarded-to:peerbox`) imported the same
  way → `source: forward`, `submitted_by: forwarded to the authoritative ledger
  (forward; this ledger holds the mirror copy)`. A fresh submission through the
  real CLI (`gopi task submit --source agency-proposal`) printed
  `source=agency-proposal` on its receipt and rendered it in `gopi task list`.
- `go build ./...` clean; `go test ./internal/standards/` green (all six
  machine-checkable gates, Rule 1/13/14 included); the tools/tasksource/
  agencyscan/testutil packages green.

## Post-run re-verification (the run's own follow-up, 2026-09-26)

The first version of this branch shipped with two claims that were **weaker than
the report said**, and both were closed here rather than left to a reviewer:

1. **The FULL `internal/tools` package was RED.** The acceptance subset
   (`-run 'Forward|TaskSubmit'`) was green, but
   `go test ./internal/tools/ -count=1` → `--- FAIL:
   TestEnsureTasksSchemaMigratesLegacyFourStatusDB` (849.701s). The test asserted
   a migrated legacy row's `Source == "auto"` — a **test-side literal of the
   retired alias**, the same defect one layer out: the ALTER's default became
   `''` and this assertion still demanded the old spelling. Fixed at the
   assertion (`task_dispatch_test.go`), whose comment now names why the alias is
   not a neutral filler (Normalize resolves it to `Merger`). Re-run:
   `go test ./internal/tools/ -run 'Legacy|Forward|TaskSubmit|MirrorNoise'
   -count=1` → `ok … 26.710s`.
2. **`internal/standards` went RED once the devdoc landed in the tree.** The new
   `devdoc/forwarded-row-provenance.md` had no `DOC-INDEX.tsv` row and no
   `<!-- doc-class: historical -->` banner
   (`TestDocManifestCoversEveryDevdocFile`: 2384 rows / 2385 files;
   `TestDocClassBannerMatchesManifest`). Row added in C-sort position with its
   class + reason, banner added as line 1 →
   `go test ./internal/standards/ -count=1` → `ok … 21.960s` (782 bannered docs,
   0 mislabelled; 535 normative spot-checks).

The lesson is the ordinary one and worth stating: **a corpus gate is part of the
deliverable's own gate set** — the new doc is an artifact the diff ships, so the
manifest that classifies it must ship in the same commit.

### A sibling defect found by the audit and deliberately NOT fixed here

`internal/tools/memory_sync_task.go:2245` (`importTaskMirrorTx`, the terminal-row
mirror INSERT) hardcodes `source` as the literal `'sync'`. Verified:
`tasksource.Normalize("sync")` = `("", false)`, `Known("sync")` = false, and the
operator's ledger holds **no** `sync` rows (`auto|998 human|819 llm|641 agent|87
server|20 merger|7`) — so a mirrored history row carries a provenance the ledger's
own validation predicate would refuse, and `taskSubmittedBy` falls through to
`orDash`, rendering the raw spelling.

That is a **different literal from the retired `auto` alias** this epic scopes,
and choosing its honest value is a decision of its own (is a terminal-mirror row
the same provenance as the fresh-replication row that already writes
`tasksource.Server`, or a second one?). It was implemented, found to be a scope
widening with an undecided role, **reverted**, and **dispatched as its own task**
(`01a0dad8-4c47-73f1-894c-eddf4ac8b585`, tag `bug`) with the repro and the
acceptance. Rule 26: an incomplete fix is repaired by dispatching again, never by
silently widening the diff.
