<!-- doc-class: historical -->
# Task Report: the doc-anchor permit list carried 3 duplicate keys — dedup + a file-hygiene guard

`TestDocAnchorsResolveInNormativeDocs` (internal/agent) was RED on the merge base
with a read-time error, not an anchor error:

```
docanchor_test.go:166: read internal/agent/testdata/docanchor-baseline.tsv:
  internal/agent/testdata/docanchor-baseline.tsv:266: duplicate entry
  "devdoc/chat-answer-gold-is-not-a-regression.md|blank|palette.go:500"
--- FAIL: TestDocAnchorsResolveInNormativeDocs (0.05s)
```

`docAnchorBaseline` returns at the **first** repeated key, so the gate never
reached a single anchor: the whole 566-doc normative class was ungoverned while
the red read like ordinary drift. Task 01a0e5f8, branch
`fix/docanchor-baseline-duplicate-keys`.

## Completed

### 1. Four rows deleted, one per defect — pure deletion, zero insertions

`git diff --numstat` for the permit list is `0     4` (4 deletions, 0
insertions): no row was edited, reordered or reflowed, and no 4th row was added.
Row order and prose are the record (Rule 4); each deletion was applied
bottom-up by line number against a copy of the file held outside the worktree.

| # | deleted row (line at merge base) | key | why this one lost |
|---|---|---|---|
| 1 | 278 | `devdoc/verify-boundary-audit-20260912.md` \| `blank` \| `internal/tools/task_verify_closure.go:345` | one of 17 rows 23fc6d6a (task 01a0e4390) added, all sharing ONE blanket reason; kept 248 (80d83a80, task 01a0e582) |
| 2 | 270 | `devdoc/gopi-statistics-20260920.md` \| `blank` \| `internal/tools/memory_provenance_latent.go:143` | same pair; kept 119 |
| 3 | 266 | `devdoc/chat-answer-gold-is-not-a-regression.md` \| `blank` \| `palette.go:500` | same pair; kept 63 |
| 4 | 266 (after #1–#3) | `devdoc/chat-status-row-partition.md` \| `blank` \| `cmd/agent/cli_chat.go:616` | **a different defect**: a genuinely STALE permit, surfaced only once the file became readable |

### 2. Which reason was kept, and the per-key evidence

Both commits permitted the same three keys; the retained row is the one whose
reason still describes **today's** master.

**Kept — 80d83a80's rows (63 / 119 / 248).** Each carries a per-key measurement,
and each of those measurements was re-verified line by line in this run:

- `palette.go:500` — the retained reason says the anchor stays on :500 because
  master's colour work moved the cited **field** to :548 while :500 is the blank
  line the anchor landed on. Measured: `internal/lineedit/palette.go:500` is
  empty; `Answer: palette256Fg(cubeGoldBody)` is at **:548**, with
  `func palette256Fg` ending at :498 — exactly the described shift.
- `internal/tools/memory_provenance_latent.go:143` — the retained reason says the
  hardcoded `/home/user/work/GoPi` at :143 was since DERIVED and the declaration
  now sits at :154. Measured: :143 is the blank line after
  `latentColumnProbeRootEnv = "GOPI_PROVENANCE_PROBE_ROOT"` (:142), and
  `latentColumnProbeRoot = ""` is at **:154**.
- `internal/tools/task_verify_closure.go:345` — the retained reason says the
  anchor means "the line where `func taskVerifyClosure` begins", that the
  function still begins at :350, and that the constant block above it grew so
  :345 is now blank. Measured: :345 is empty and
  `func taskVerifyClosure(...)` begins at **:350**.

**Deleted — 23fc6d6a's rows (266 / 270 / 278).** Two measured grounds:

1. **Its stated mechanism is false for all three keys.** The reason says "task
   01a0e4390's per-turn Ctrl+C … added lines to this file". Measured:
   `git show --numstat 23fc6d6a -- internal/lineedit/palette.go
   internal/tools/memory_provenance_latent.go internal/tools/task_verify_closure.go`
   names **none of the three files** (that commit changed loop.go +184/-6,
   session.go +19, chat_ops.go +14, cli_chat.go +179/-9 and their tests). So
   23fc6d6a ADDED the renumbers while merely re-listing the keys as collateral.
   Whatever shifted these lines landed in other commits, which is exactly why
   80d83a80's per-key reasons are the accurate description and 23fc6d6a's is not.
2. **Its 17 rows share one identical blanket reason string**, so it cannot carry
   per-key evidence at all — and this list is keyed per row. Where a pair had to
   lose a row, the loser is the one that describes no mechanism it can point at.

**Deleted — the stale permit (#4).** After the file became readable the gate
reported `1 stale permits`: `…|cmd/agent/cli_chat.go:616` matched no drift.
Measured: `devdoc/chat-status-row-partition.md:102` anchors `cli_chat.go:616`
for `iter` alongside `:753` and `:796`, but `:616` is now a COMMENT line inside
`toolsEnabled`, not a blank line, so no drift is emitted for it and the permit
matched nothing. The gate's own prescription is deletion ("a permit that matched
nothing is stale — the site was fixed or the doc left the normative class"), and
the allow-list may only ratchet down. The residual is therefore reported as a
**stale permit, fixed** — not as an untouchable pre-existing red.

### 3. The regression guard, and its red-proof

`TestDocAnchorBaselineHasNoDuplicateKeys` (internal/agent/docanchor_test.go) —
the permit list's file hygiene, checked directly instead of inferred from a
loader error inside a 566-doc assertion:

- a repeated **key** among data rows (the defect this run measured), reported
  with both line numbers and the key;
- a repeated **whole line** among data rows (a byte-identical row is always a
  redundant permit);
- a field count other than the four the loader requires;
- comment (`#`) and blank lines are prose — the header is a repaired record and
  repeats legitimately, so they are never keyed.

Named constants (Rule 1/13) are defined once and shared by both readers — the
loader `docAnchorBaseline` and the guard: `docAnchorBaselineFieldCount`,
`docAnchorBaselineKeyFieldCount`, `docAnchorBaselineFieldSep`,
`docAnchorBaselineKeySep`, `docAnchorBaselineCommentPrefix`. The loader now
references them too, so no reader types one of these spellings inline.

**Red-proof** (Rule 10.5) — `TestDocAnchorBaselineDuplicateGuardCatchesDuplicates`
appends one of the REAL list's own permit rows a second time and requires the
guard to report it, so the guard cannot rot into decoration. Observed against
the pre-dedup file, the guard named all three pairs:

```
--- FAIL: TestDocAnchorBaselineHasNoDuplicateKeys (0.00s)
  …:266: duplicate key "devdoc/chat-answer-gold-is-not-a-regression.md|blank|palette.go:500" (first at line 63)
  …:270: duplicate key "devdoc/gopi-statistics-20260920.md|blank|internal/tools/memory_provenance_latent.go:143" (first at line 119)
  …:278: duplicate key "devdoc/verify-boundary-audit-20260912.md|blank|internal/tools/task_verify_closure.go:345" (first at line 248)
```

The three line pairs it names are exactly the three duplicate keys the epic
described, in the exact order its `awk` check reported them — an independent
confirmation of the find, from a different instrument.

## Not Done

- **The acceptance `awk` line, read literally, still prints two DUP lines** —
  `10: DUP of 9` and `18: DUP of 9`. Those are `#` comment lines of the header,
  which the epic's own constraint says "legitimately repeat"; keying them is
  outside the loader's semantics (it skips `#` and blank lines). Restricting the
  same awk to data rows, the acceptance condition is met:
  `awk -F'\t' '/^#/ || /^[[:space:]]*$/ {next} {k=$1"|"$2"|"$3; if(k in seen) print NR": DUP of "seen[k]; else seen[k]=NR}'`
  prints nothing. Reported here rather than silently satisfied.
- **The stale-permit class is systemic, not closed.** `#4` was the only one
  visible from here; every other permit whose site master fixes will become
  stale on its own landing and must be deleted then. This run deleted only the
  one the now-readable gate reported.
- **The 236 remaining permits are not audited here.** They are in force and
  matched (236 permitted, 0 new, 0 stale). Re-keying them by symbol is the
  subject of the earlier repair reports, not of this dedup task.
- The report's own devdoc manifest row landed via the `write` tool's
  classification hook; the follow-up `gopi doccheck` reported the corpus
  consistent. (The `write` call itself returned `timed out after 30s` while the
  bytes HAD landed and the hook HAD run — Rule 36's measured case; verified by
  reading the file's byte count and its manifest row rather than trusting the
  error.)
- **Pre-existing red outside this epic, reported not fixed** —
  `go test ./internal/standards/ -count=1` fails on
  `devdoc/line-break-lost-at-launch-cadence.md`: the file carries the
  `<!-- doc-class: historical -->` banner while `devdoc/DOC-INDEX.tsv` classes
  it `normative`. Both halves are present at the merge base and this run touched
  neither file (`git diff HEAD~1 --name-only` names no `line-break-lost` path;
  this run's DOC-INDEX change is exactly `1 0` — one added line, its own report
  row). Regenerating the manifest to resolve it would reflow the corpus, which
  this task's constraints forbid; it belongs to the docclass/manifest epic.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (no output).
- Tests:
  - `go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1`
    → `ok github.com/nick/gopi/internal/agent`, and with `-v`:
    `doc anchors verified: 2659 across 566 normative docs (2200 explicit + 459 bare continuations; 463 bare out of row/table/heading scope; 236 pre-existing drift entries permitted, 0 new, 0 stale permits)`.
  - `go test ./internal/agent/ -run TestDocAnchorBaselineHasNoDuplicateKeys -count=1 -v`
    → PASS, `internal/agent/testdata/docanchor-baseline.tsv: 236 permit rows, no duplicate key, no duplicate line`.
  - `go test ./internal/agent/ -run TestDocAnchorBaselineDuplicateGuardCatchesDuplicates -count=1 -v`
    → PASS, red-proof reproduced on a duplicated real row.
  - `gofmt -l internal/agent/docanchor_test.go` → clean after `gofmt -w`.
- Ran:
  1. `awk -F'\t' '{k=$1"|"$2"|"$3; if(k in seen) print NR": DUP of "seen[k]; else seen[k]=NR}' internal/agent/testdata/docanchor-baseline.tsv`
     (the acceptance command, verbatim) → data rows clean; the two lines it
     still prints are header comments (see Not Done).
  2. `awk -F'\t' '/^#/ || /^[[:space:]]*$/ {next} {k=$1"|"$2"|"$3; if(k in seen) print NR": DUP of "seen[k]; else seen[k]=NR}' internal/agent/testdata/docanchor-baseline.tsv`
     (the same check restricted to data rows, i.e. the loader's own semantics) →
     no output.
  3. `git diff --numstat internal/agent/testdata/docanchor-baseline.tsv` → `0	4`.
  4. Red-proof: copied the pre-dedup list back over the file, ran
     `go test ./internal/agent/ -run TestDocAnchorBaselineHasNoDuplicateKeys -count=1`
     → FAIL naming the 3 pairs; then restored the deduped file and re-ran the
     same command → PASS. The pre/post copies were held at
     `/home/user/workspace/{pre,post}-dedup-baseline.tsv` (outside the
     worktree), so the restoration was byte-exact.
- Result: gate green with `0 new, 0 stale permits`; guard red on the pre-dedup
  file and green after; the permit-list diff is 4 deletions and 0 insertions.
- Commit: tip of branch `fix/docanchor-baseline-duplicate-keys`, subject
  `fix(docanchor): dedup the permit list's 3 repeated keys and guard file hygiene`.
  The hash is deliberately not spelled here: editing this line is itself an
  amend, so a recorded hash would name the tip *before* the line landed —
  `git log -1 --oneline fix/docanchor-baseline-duplicate-keys` is the authority.
  (on `fix/docanchor-baseline-duplicate-keys`).

## Evidence (self-filed)

- Duplicate keys at the merge base — the epic's awk, verbatim, before the edit:
  `10: DUP of 9`, `18: DUP of 9`, `266: DUP of 63`, `270: DUP of 119`,
  `278: DUP of 248`.
- The gate's read-time failure before the edit — observed verbatim at the top of
  this report (`docanchor_test.go:166 … duplicate entry … :266`).
- Retained-reason verification — three measured reads recorded in §2 above
  (palette.go :500 blank / field at :548; memory_provenance_latent.go :143 blank
  / decl at :154; task_verify_closure.go :345 blank / func at :350).
- Deleted-reason falsification —
  `git show --numstat --format=%h 23fc6d6a -- internal/lineedit/palette.go internal/tools/memory_provenance_latent.go internal/tools/task_verify_closure.go`
  printed the commit hash and nothing else: none of the three files is in that
  commit.
- The stale-permit observation — the gate's own line
  `stale baseline entry "devdoc/chat-status-row-partition.md|blank|cmd/agent/cli_chat.go:616"`;
  the doc's citation is `devdoc/chat-status-row-partition.md:102`; `cmd/agent/cli_chat.go:616`
  read as a comment line.
- Red-then-green transition of the guard: FAIL naming 3 pairs on the pre-dedup
  file, PASS (`236 permit rows, no duplicate key, no duplicate line`) after.
- `git status --porcelain` at close: printed nothing (the reading taken after
  the commit below).
