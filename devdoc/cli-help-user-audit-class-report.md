<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Task Report: the docclass red on `devdoc/cli-help-user-audit.md` — the ROW was the stale half

Task `01a11461-db87-74da-8d68-cd98f895a3b4` (branch `task/01a11461db8774da8d68cd98f895a3b4`).
Repairs the one remaining `internal/standards` docclass red:

```
docclass_gate_test.go:212: devdoc/cli-help-user-audit.md: carries the "historical" banner
                           but devdoc/DOC-INDEX.tsv classes it "normative"
docclass_gate_test.go:222: devdoc/DOC-INDEX.tsv: 1166 markdown docs of a bannered class
                           carry their banner (1 mislabelled)
```

## The decision, and the evidence it rests on

Two halves disagreed; exactly one was repaired. **The ROW was stale — `normative` was
never the classifier's verdict for this file, and no live artifact cites the doc.**
Three legs, all run before editing (none of them a guess, none of them the doc's age
or a task row's status — the trap recorded in memory #6502):

**Leg 1 — the corpus's own classifier re-derives the row.** `classify.py --dry-run
--only` prints the row it WOULD write, and it is not the checked-in one:

```
devdoc/cli-help-user-audit.md  historical  historical (7 standing / 2 record): the doc's
own title/name declares a past run's record, and no current artifact cites it as authority
```

The arm that decides it is deterministic and readable in the tool's own source
(`tools/docclass/classify.py:243`, `RECORD_NAME_RE`): the doc's NAME ends in
`-audit`, so `recordy` is true; a recordy doc with no citation is `historical`
outright. Nothing about the class depended on the author's judgement, on mine, or on
how the prose reads.

**Leg 2 — no live artifact cites the doc, so the `normative` side has no footing.**
The citation index is built from every authority file outside `devdoc/` and
`tools/docclass/`. `CITED` has **no entry** for this doc (measured, see Evidence).
A repo-wide text search finds it only in `devdoc/` siblings that *describe this very
mismatch* (`cli-reorg-e2e-verification.md`, `undispatched-tool-words-gate-844d1f3d.md`)
— a doc-does-not-cite-doc corpus: two records of an earlier run mentioning each other
is not one of them becoming authority.

**Leg 3 — the row is not the generator's own output, which is what makes `normative`
the hand-written half.** The row's evidence cell is `path cmd/agent/usage_test.go`.
The `path`/`anchor` tiers are granted only for an explicit reference **from the doc's
own text** (`code_spotcheck`, `classify.py:524-550`): to earn `path cmd/agent/usage_test.go`
the doc would have to backtick that file. It does not — the doc's backticked `.go`
references are `cmd/agent/cli_help_hidden_verbs_test.go`, `internal/config/help.go` and
`cmd/agent/cli_completion.go`. Calling the module's own `classify()` on the doc's text
returns `path cmd/agent/cli_completion.go` — a **different token than the row carries**,
so the checked-in row cannot be a generator output for this file. It was written by
hand, and its claimed anchor does not name the doc at all: it names the *test* that
pins the merger row's behaviour, i.e. the subject the doc documents rather than the doc
being authority for anything.

**Why the banner was NOT the half to remove** (the direction memory #6324 makes the
default, and the wrong one here): the reverse repair is unreachable in practice —
`internal/docindex.EnsureDoc` derives the banner FROM the manifest class, so the row is
upstream of the banner; and `historical` is the class this doc's name earns
mechanically. Deleting the banner would have turned a true statement into a missing one
(Rule 15/20) to satisfy a gate.

**Residual, recorded not hidden.** The doc also carries a prose line
`Truth class: **normative** — a standing claim about master…`. It is not read by any
gate and is neither the banner nor the row, so the epic's "exactly one side" scope
left it alone; it is now the only self-contradicting statement in the file and is
dispatched as its own follow-up (Rule 26) rather than folded in here.

## Completed

- Decided the stale half on three legs of read-only evidence (classifier re-derivation,
  citation index, generator-token check) — recorded above.
- Flipped the row with the corpus's own incremental writer,
  `python3 tools/docclass/classify.py . --only devdoc/cli-help-user-audit.md`
  (`{"added": 0, "updated": 1, "rows": 2085}`) — never by hand (Rule 36).
- Confirmed `banner.py` is a FOLLOWER and needed no edit: with the row corrected,
  `banner.py . --only devdoc/cli-help-user-audit.md` reports
  `banner applied to 0 docs, removed from 0` — the banner was already right, which is
  the second, independent proof of the direction.
- Re-derived the row afterwards with `--dry-run --only`: byte-identical to what the
  writer put in the file, so the row is the generator's own output and not prose fitted
  to a gate.
- Re-ran the gate: `0 mislabelled`, and the two other named reds
  (`TestCommandsDocGeneratedBlockMatchesSource`, `TestCommandsDocGateRedProofStaleGeneratedBlock`)
  confirmed already green on the base commit — the briefing's premise held.
- Classified this report (Rule 36) through the same incremental writer + banner follower.

## Not Done

- **The second `banner.py --check` mismatch is untouched and out of scope.**
  `devdoc/chat-status-hitrate-two-readings.md` carries `<!-- doc-class: normative -->` on
  line 3 while the manifest classes it `normative` — same class, wrong line, so it is
  invisible to the Go gate (which reads line 1 only) and red only in `banner.py --check`.
  It predates this task — **proven, not asserted**, by checking it out of the object
  store at master (`git archive master devdoc tools | tar -x -C <session>/master-tree`
  then `banner.py . --check` inside: `2 markdown docs whose banner disagrees`, this file
  and this task's) — it is a different file, and
  the epic forbids a whole-corpus run; it is reported here rather than swept in.
- **`gopi doccheck` was deliberately NOT used as proof.** It exits 0 on this exact
  red (it validates rows, not banners) — using it would have been a green light with
  nothing behind it. `classify.py --check` (row side) and the Go gate + `banner.py
  --check` (banner side) are the detectors that can see this defect.
- No whole-corpus `classify.py .` / `banner.py .` run was made, and no new persistent
  dependency was added (Rule 35). Evidence artifacts live in the session directory, not
  the worktree.

## Verification

- **Build:** `CGO_ENABLED=0 go build -o /dev/null ./cmd/agent/` → OK (no Go source changed;
  the diff is one TSV row + this report + its row/banner).
- **Tests:**
  - `go test ./internal/standards/ -count=1 -run 'TestDocClassBannerMatchesManifest|TestCommandsDocGeneratedBlockMatchesSource|TestCommandsDocGateRedProofStaleGeneratedBlock'`
    → `ok github.com/nick/gopi/internal/standards` (exit 0)
  - `go test ./internal/standards/ -count=1 -v -run TestDocClassBannerMatchesManifest`
    → `docclass_gate_test.go:222: devdoc/DOC-INDEX.tsv: 1168 markdown docs of a bannered
    class carry their banner (0 mislabelled)` — `--- PASS`
    (1167 before this report's own row landed, 1168 after — the checked-in report is
    itself a bannered `historical` doc, so it moves this count by one. The figure that
    decides the red is `0 mislabelled`, unchanged either way.)
  - `go test ./internal/standards/ -count=1 -run 'DocClass|Banner'` → `ok`
  - Rule 16-21 six gates: `go test ./internal/standards/ -count=1 -run 'Gate'` → `ok`;
    the six red-proofs named in Machine-Checkable Gates (`TestGate1ErrorWrapRedProof`,
    `TestGate2TestEnvRedProof`, `TestGate2EnvRestoreShapeRedProof`,
    `TestGate3ElseAfterReturnRedProof`, `TestGate4PanicRedProof`,
    `TestGate5AccessorPrefixRedProof`, `TestGate6EnvReaderRedProof`) → `ok`.
- **Ran:**
  - `python3 tools/docclass/classify.py . --dry-run --only devdoc/cli-help-user-audit.md`
    (before and after the repair)
  - `python3 tools/docclass/classify.py . --only devdoc/cli-help-user-audit.md`
  - `python3 tools/docclass/banner.py . --only devdoc/cli-help-user-audit.md`
  - `python3 tools/docclass/classify.py . --check` → `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv` (exit 0)
  - `python3 tools/docclass/banner.py . --check` → exit 1, and the ONLY remaining mismatch
    named is `devdoc/chat-status-hitrate-two-readings.md` — this task's file no longer appears
  - `git diff --numstat` → `1	1	devdoc/DOC-INDEX.tsv` (the repair: exactly one row)
  - `git status --porcelain` → prints nothing (Rule 30)
- **Result:** the docclass red is gone without touching the file that was already right;
  the drift between banner and manifest for `devdoc/cli-help-user-audit.md` is resolved in
  the direction the classifier re-derives. `--only` was the exact scope: no other row moved
  (`1 1` numstat; `--check` still 0 gaps; row total 2085 before and after the tool run).
- **Commit:** the tip of `task/01a11461db8774da8d68cd98f895a3b4`, subject
  `fix(docclass): the cli-help-user-audit ROW was the stale half, not its banner`.
  `git diff --numstat master...HEAD` = `2 1 devdoc/DOC-INDEX.tsv` plus this report
  (155 lines added); the repair itself is still the single one-row flip.

## Evidence (self-filed)

Every claim above, with the command that produced it and what it printed.

| claim | command | observed |
|---|---|---|
| base is red, 1 mislabelled | `go test ./internal/standards/ -count=1 -run 'TestDocClassBannerMatchesManifest\|TestCommandsDocGeneratedBlockMatchesSource\|TestCommandsDocGateRedProofStaleGeneratedBlock'` | `--- FAIL: TestDocClassBannerMatchesManifest` … `(1 mislabelled)`; the other two arms did NOT fail |
| the classifier re-derives `historical` | `python3 tools/docclass/classify.py . --dry-run --only devdoc/cli-help-user-audit.md` | `historical (7 standing / 2 record): the doc's own title/name declares a past run's record, and no current artifact cites it as authority` |
| no live artifact cites the doc | python importing the tool from the repo: `C.build_code_index(); C.build_citation_index(); C.CITED.get('cli-help-user-audit.md','')` | `''` (empty) |
| the generator's spot-check for this doc is NOT the row's token | same import, `C.code_spotcheck(open('devdoc/cli-help-user-audit.md').read())` | `'path cmd/agent/cli_completion.go'` — the row carries `path cmd/agent/usage_test.go` |
| the doc never backticks the row's claimed anchor file | `grep -n '\.go' devdoc/cli-help-user-audit.md` | only `cmd/agent/cli_help_hidden_verbs_test.go`, `internal/config/help.go`, `cmd/agent/cli_completion.go` |
| the row and the banner came from ONE commit | `git log --oneline -S'doc-class: historical' -- devdoc/cli-help-user-audit.md` and `git log --oneline -S'cli-help-user-audit.md' -- devdoc/DOC-INDEX.tsv` | both `ac35c924f fix(help): gopi -h drops the one row …` |
| the row flip is the tool's own write | `python3 tools/docclass/classify.py . --only devdoc/cli-help-user-audit.md` | `{"added": 0, "updated": 1, "rows": 2085}` |
| the row now equals the generator's verdict | `python3 tools/docclass/classify.py . --dry-run --only devdoc/cli-help-user-audit.md` (after) | identical to the in-file row, byte for byte |
| the banner was already correct (follower is a no-op) | `python3 tools/docclass/banner.py . --only devdoc/cli-help-user-audit.md` | `banner applied to 0 docs, removed from 0` (exit 0); `git diff --numstat` still `1 1` |
| the repair is one row | `git diff --numstat` | `1	1	devdoc/DOC-INDEX.tsv` |
| the gate is green, 0 mislabelled | `go test ./internal/standards/ -count=1 -v -run TestDocClassBannerMatchesManifest` | `docclass_gate_test.go:222: … (0 mislabelled)` `--- PASS` |
| the row side of the corpus reports no gap | `python3 tools/docclass/classify.py . --check` | `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`, exit 0 |
| the banner side no longer names this doc | `python3 tools/docclass/banner.py . --check` | exit 1, one line: `BANNER MISMATCH devdoc/chat-status-hitrate-two-readings.md` — no line for `cli-help-user-audit.md` |
| the worktree is clean at close | `git status --porcelain` | (no output) |
