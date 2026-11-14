<!-- doc-class: historical -->
# Task Report: the devdoc anchor-NUMBER drift — permissive or repair, and a standards edge

> **devdoc class: record** — this file accounts for one run (task `01a132a6`,
> 2026-10-13, re-landed by task `01a1335a` on 2026-10-13 after the original
> branch was deleted before merge) and is frozen by nature. Its claim is a
> MEASUREMENT, so re-running its commands is meaningful; "fixing" the prose is
> not.

## What the briefing asked, and what the tree actually held

The briefing said the standing red was a row of
`devdoc/session-record-view-refilter-gate.md` (manifest line 1722), whose anchor
cell recorded a site in `cmd/agent/cli_notify_deliver_idle_test.go`, five lines
above where the text now sits. **Measured, that row is not the red one** — it
was already permitted
(baseline row 141, reason `first-frozen at 0b3c1de29`) by the commit `e468094b0`
that landed it. The live red was a DIFFERENT doc:

```
$ gopi tool doccheck
doccheck: anchor NUMBER coherence (recorded text still present, line number stale):
doccheck:   NOT PERMITTED (drifts, absent from the baseline): devdoc/merge-resolve-peak-hour-guard.md#1220: anchor cmd/agent/main.go:982 at } — number-drifted (recorded text is at :80)
doccheck: 0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
doccheck: this run exits 1 on anchor NUMBER drift (see the section above), independently of the 0 gap(s)
```

The briefing's own warning applies to itself: a briefing's red line is a reading,
not the tree. Nothing else was red — 0 stale permits, 0 gaps.

**The re-landing measured a different tree, and this is worth recording.** The
2026-10-13 tree carries 253 anchor rows (the original run measured 244), and the
row that was then `number-drifted` is on today's tree a **`text-ambiguous`**
row: the manifest cell for `devdoc/merge-resolve-peak-hour-guard.md` records the
text `}`, and `}` is satisfied by more than one line of `cmd/agent/main.go`, so
the line-number rung makes **no determination** about it and the row is neither
frozen nor reported as drift (`AnchorNumberAmbiguous`). That class was added
after this work was first authored (task `01a131fd`), and it is exactly the
ruling that makes the permit unnecessary: a text more than one line satisfies
cannot be tested for line coherence at all. The re-landing therefore carries the
**gate**, not a permit for that row — the table regeneration is a no-op
(`0 permit(s) added, 0 permit(s) removed`), which is the honest outcome: the
freeze was already exact on this tree before this change (measured: `--check`
exit 0, `table matches the tree — the freeze is exact`, 139 of 253 incoherent,
all frozen).

## Permit or re-point — the criterion, and which way it fell

`cmd/agent/main.go:982` (`}`) is the doc's own transcription of its
**retired** subject: the Rule-16 permit `internal/standards/baseline.go` carried
in September 2026 for the peak-hour guard, whose flags `bb0c4c1a` removed
outright (the doc says so in its own banner). The row's claim (the text is in the
file) still holds; its number is an accelerator that the file's later edits moved.
The doc is a historical record; the site it names no longer exists as a site.

So this is the **permit** arm — and for the reason the package gives: a
"re-point" would be a hand edit of the manifest's evidence cell, and `Rule 36`
makes the table DATA — rewritten by its generator, never typed. Repairing the
NUMBER is not available as a decision here; the generator's reason column is.

The generator decides the reason itself, by measurement, and refuses anything
else (`AnchorNumberProvenanceProbe` + `AnchorNumberAssertReasonIsTrue`): a key
that is already incoherent at the freeze commit `0b3c1de29` reads
`pre-existing at the 0b3c1de29 freeze`, and one that first became incoherent
later reads `first-frozen at …`. **The drift this run was briefed about is
inherited, not this change's** — which is also what the git history says: the
doc's manifest row was inlined by a landing that stated the freeze table out of
its own scope.

## Completed

- `internal/docclass/testdata/anchor-number-baseline.tsv` — regenerated through
  its ONE generator (Rule 36). Measured on this tree: 253 anchor rows scanned,
  139 incoherent, 15 `first-frozen` + 124 `pre-existing`, **0 permits added,
  0 removed** — the write's own delta. The generator is a fixpoint here: `--check`
  exits 0 (`table matches the tree — the freeze is exact`) and a second writer run
  leaves the file byte-identical (`git status --porcelain` empty, same sha256,
  blob identical to `HEAD:`).
- `internal/standards/devdoc_anchor_number_gate_test.go` — a NEW standards gate
  that gives this red class an edge in `go test ./internal/standards/`, which
  previously said nothing about it. It reads the corpus through the package's ONE
  report (`docclass.AnchorNumberDrift`), so the gate and `gopi tool doccheck`
  cannot answer differently, and it asserts BOTH arms (unpermitted drift red,
  stale permit red) behind a read floor and a permit floor. Four arms total:
  real tree, fixture-red, fixture-silent, fixture-stale-permit.

## Not Done (honest)

- **The `CONFIG.md:174` → a blank line in `cmd/agent/main.go` blank-target red is
  NOT fixed.** It is pre-existing on `master` and untouched by this change; it is
  the subject of its own bug row (Rule 41), and re-pointing it belongs to the
  `CONFIG.md` reference census, not to this file's anchor-number scope.
- **A THIRD red is pre-existing on `master` and is NOT this class at all:**
  `gopi tool doccheck` exits 1 because its `stored-cell drift` leg reports
  `788 of 2514 rendered row(s) whose stored text is not what tools/docclass/
  classify.py renders today` — the leg that landed after this work was first
  authored. Filed as bug row `01a138f0-19ea-7a35-a09b-ce303ad082b0` with the full
  capture, rather than repaired here (a corpus-wide regeneration is not this
  branch's task). What this file's own class needs from that verb is the
  anchor-NUMBER leg, and that leg is clean.
- **The condition that PRODUCES this red is not removed.** A devdoc row can still
  be inlined into `devdoc/DOC-INDEX.tsv` by a landing that declares the freeze
  table out of scope, leaving the table stale for the next tree. The new gate
  covers the PACKAGE in the ordinary suite, and the merged-result gate's
  devdoc-anchor leg runs `./internal/docclass` on such a landing — but the
  authoring side (the manifest row's cell is copied from the document's own
  inline reference) is unchanged.
- **The new gate is not itself a red-proof for the DETECTOR.** It proves the
  detector's direction on a fixture and the freeze's exactness on the real tree;
  the detector's own semantics remain `internal/docclass`'s tests.

## Verification

- Build: `go build ./...` → OK; `go vet ./...` → OK;
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- Tests: `go test ./internal/standards/ -run TestDevdocAnchorNumber -count=1`
  → 4/4 PASS; `make test-budget PKGS='./internal/standards ./internal/docclass'`
  → see `## Evidence` for the recorded result.
- Ran:
  - `./bin/gopi tool doccheck` → exit 0 (the base tree, before this change,
    carried the drift quoted above).
  - `go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv --check`
    → exit 0, `table matches the tree — the freeze is exact`.
  - `go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv`
    (the writer) → `253 anchor row(s) scanned, 139 incoherent (15 first-frozen
    permit(s), 124 pre-existing claim(s)); the write's own delta: 0 permit(s)
    added, 0 permit(s) removed`.
- Result: the tree's own gate, the CLI verb and the generator's check arm agree;
  the red arm is reproducible by restoring the previous revision of the table.
- Commit: see the branch tip — `internal/standards/devdoc_anchor_number_gate_test.go`
  plus this document and its manifest row.

## Evidence (self-filed)

See `## Evidence (self-filed)` entries in the run's report at the task report
path (`$GOPI_TASK_REPORT_PATH`), which carry the verbatim commands, the exit
codes and the red-then-green transition for the new gate's real-tree arm.

## Files

- `internal/docclass/testdata/anchor-number-baseline.tsv` (regenerated, 0-delta)
- `internal/standards/devdoc_anchor_number_gate_test.go` (new gate, 4 arms)
