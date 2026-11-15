<!-- doc-class: normative -->
# Full IDs: a prefix of a task id or a session id is never resolved

Task: `01a0ce09-6395-7e7e-9f04-6ca51716c90b` (epic `full-id-no-prefix`, 2026-09-23).
Branch: `task/01a0ce0963957e7e9f046ca51716c90b`.

## What was wrong

`gopi log --last` printed an **8-character chunk of the SESSION id**, and every
resolution path accepted a prefix back. Two distinct defects came out of that
one habit:

1. **The printed token was not an id at all.** `gopi log --last 4` printed
   `2ab3facd` — the first 8 characters of the session id — so the operand an
   operator copies out of the listing resolves to *nothing*: it is not a task id
   (task ids are the ledger's uuids) and, on a machine with more than one
   session, it is not a session id either. It only LOOKS like one.
2. **A prefix silently names more than one row.** Measured on this host
   (`gopi task list --all`, 2409 rows): `01a0b51d` names **35** rows, `01a0c039`
   and `01a0aba1` name **2** each. The flake registry's `owner` column was an
   8-hex chip and **2 of its 5 rows** were being resolved through such a prefix.

The resolution code made (2) worse rather than safer. Every session resolver had
an "exact match, then first-prefix-hit" fallback, so a truncated operand handed
back **whichever record the directory walk reached first** — for
`gopi task show <row>` that means attaching *another* task's transcript to the
row. One path (`gopilog.FindSession`) did refuse an *ambiguous* prefix, but that
only catches the collision it happens to see: a prefix that is unique *today*
resolves silently and stops being unique the moment another session starts.

## The rule

**A task id and a session id are always used in FULL. A prefix is never
resolved — it is REFUSED, and the refusal names every full id it could have
meant.**

## Where it lives

`internal/fullid` — a small, leaf, exported-constant package (the
`internal/toolnames` pattern, PROJECT.md Rule 14). It owns:

- `IsUUID` / `TruncatedUUID` — the canonical 8-4-4-4-12 shape (a cut-short one
  matches only as a prefix of it), so a legacy `t-<epoch>` id or a custom id is
  never mistaken for a truncated uuid;
- `Refusal(kind, id, candidates)` — the ONE operator-facing message: it says the
  operand is not a full id, names every identity it is ambiguous between, and
  says what to type instead. Three shapes: no candidate, one, many.

Callers compare against **that** predicate instead of each spelling their own.

## What changed

| surface | before | after |
|---|---|---|
| `gopilog.FormatSummary` / `FormatSummaryTable` (`gopi log --last N`, `--all --summary`) | `shortID(id)` = `id[:8]` | the FULL session id; both views share ONE width-derived row format |
| `gopilog.FindSession` | exact → first/ambiguous prefix | exact only; a prefix is refused with its candidates |
| `tools.findSessionJSONLFlat` / `taskFindSessionJSONL` / `taskFindSessionJSONLDeepOnly` | exact, then a remembered first prefix hit | exact only |
| `tools.chatFindSessionJSONL` | exact, then first prefix hit | exact only |
| `tools.taskHashDisplay` (the `#` column of `gopi task list`) | 8-hex chip (`id[:8]`, legacy digits) | the FULL task id |
| `cmd/agent` raw-log warning | leading block of each co-recording session | the FULL id |
| flake registry (`testdata/flakes.tsv`) | 8-hex owner chips | full ids (4 rows); the gate's owner regex is now `flaketest.OwnerShape` |
| `scripts/*` (memconsult probes, flake-catch note) | `sid[:8]` | the full id |

The `#` column keeps its marker; the row no longer carries a **second**,
truncated identity beside it. That is the point: the token an operator copies
out of `gopi task list` is the token every resolution path accepts.

## Deliberate non-changes

- **`internal/tools.taskByID` is untouched.** The ledger has always matched
  `task_id` exactly; the CLI never resolved a task prefix. `gopi task show
  01a0aba1` says `no task with task_id="01a0aba1"` — measured, unchanged. The
  *display* was the defect, not the lookup.
- **An operand that matches nothing keeps its old answer** (session `""`,
  task "no such row"). A prefix refusal is only produced when the operand
  actually matches something, so a stale id or a typo is not reported as an
  ambiguity it is not part of.
- **No cascade UI rework.** Offering a picker for an ambiguous prefix would be a
  new resolution path for a prefix — the opposite of the rule.

## Verification recipes

```bash
go build ./...
go test ./internal/fullid/ ./internal/gopilog/ ./internal/tools/ ./cmd/agent/ ./internal/standards/

# the changed surface, for real (private sandbox: never the operator's sessions)
GOPI_HOME=<sbx> gopi log --last 3          # prints FULL session ids
GOPI_HOME=<sbx> gopi log 03e681e9          # ambiguous → refused, candidates named
GOPI_HOME=<sbx> gopi log acde0001          # unique prefix → STILL refused
GOPI_HOME=<sbx> gopi log <full-id>         # resolves
```

Regression guards: `internal/fullid/fullid_test.go`,
`internal/tools/full_id_no_prefix_test.go`,
`internal/gopilog/reader_test.go` (the four `FindSession*` cases),
`internal/standards/flake_registry_gate_test.go`
(`TestFlakeLedgerOwnerParserReadsEveryRowForm`).

## Pitfall found on the way

`gopi log --last N` walks the ENTIRE sessions tree (75 k+ entries on this host)
and took longer than a 2-minute bound to answer — the first `Ran:` attempt timed
out. Verification used a private sandbox with three sessions instead, which is
also the better evidence: it *constructs* the ambiguity (`03e681e9-1111-…` and
`03e681e9-2222-…`) rather than hoping the operator's tree happens to contain one.
