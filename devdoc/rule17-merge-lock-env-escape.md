<!-- doc-class: historical -->
# The Rule 17 / stylegate env-escape rework, landed on master 92d14684

**Rework of** `rework/rule17-envescape-rework` (tip `7332a59f`, the branch ref
was pruned by a merge run; only its attachment diff survived), whose own parent
was `fix/rule17-envescape` (`42af90ab`, `f2bcffd7`). The branch could not
auto-merge: `git merge --no-commit --no-ff rework/rule17-envescape-rework` on
master reported exactly **one** content conflict, `devdoc/DOC-INDEX.tsv` — the
generated manifest row the branch had produced against a stale base. This run
re-implements the **intent** on current master (`92d14684`), on a FRESH branch
(Rule 23), and regenerates the manifest row instead of patching the stale one.

## What the conflict actually was

`devdoc/DOC-INDEX.tsv` is a generated, sorted manifest (`path\tclass\treason\tevidence`)
whose rows were generated against the branch's own base. Both sides appended a
row near the same sort position, so git saw a content conflict in a **generated**
file. No substantive file conflicted. The fix is arithmetic, not diplomacy: the
reworked row must be a **pure insertion** — `git diff --numstat
devdoc/DOC-INDEX.tsv` reads `1 0` (one added line, zero deletions), which cannot
conflict with anything that has landed at the same anchor.

## The delta that was still open, and the one that had closed

Measured on current master `92d14684`, the branch's two code/annotation targets
resolved differently:

| attachment target | state on master `92d14684` | what this run did |
|---|---|---|
| `cmd/agent/merge_lock_test.go` — the merge-lock profile pin | **already carries BOTH literals** on line 207 (`dcaebcaa` → `26883d07`, a sibling landed the same fix independently): `… //stylegate:ignore the profile stays pinned for the whole SCENARIO … // env-leak-ok: the child merge-lock holder inherits this profile at exec time …` | **nothing.** `git diff master -- cmd/agent/merge_lock_test.go` is empty; a third re-wording against a reviewed record would be a gratuitous diff |
| `internal/shot/launch_test.go` — `TestChromeMemoryMaxMB`'s per-case flip | **still the last open `test-setenv` hit**: `stylegate test-setenv: 1 test hit(s), ceiling 0` | **the one-line marker** this run lands |

This branch therefore IS the row the sibling left open
(`devdoc/master-batch-land-red-20260918.md`: *"Fixing it is one marker; it is
left to its own row"*).

## Why a marker at all — the per-case flip is not a defect

The site is a per-**case** flip inside a loop:

```go
for _, c := range cases {
    // env-leak-ok: one reviewed per-case flip under a SnapshotEnv restore; a
    // t.Setenv here would persist the last case's value past the loop
    // (internal/stylegate RuleTestSetenvScope).
    //stylegate:ignore one reviewed per-case flip under a SnapshotEnv restore; …
    os.Setenv(chromeMemMaxEnvName, c.value)
```

`t.Setenv` is the **wrong instrument** here: its undo is registered with
`t.Cleanup`, so it runs only when the TEST ends and would leave the LAST case's
value installed for the remainder of the test — the exact failure
`RuleTestSetenvScope` exists to prevent. The test already holds the correct
instrument, `defer testutil.SnapshotEnv(t, chromeMemMaxEnvName)()` (an
`os.LookupEnv`-based restore, so an ABSENT variable comes back absent rather
than `""`). So the site is a **reviewed exception**, and the stylegate escape
records it as such. No baseline permit was added and no ceiling moved — a permit
for a marker-able site is a silent permission, and the ratchet may only go down.

## The marker is LITERAL, not semantic (the family's recurring trap)

Two gates govern these sites and each recognises only its own spelling:

| gate | escape literal | where it must sit |
|---|---|---|
| `internal/stylegate` `RuleTestSetenv` (`stylegate.IgnoreMarker`) | `//stylegate:ignore` | the offending statement's line span, or the contiguous comment block above (`reasonOverSpan` → `lineComment`, walking at most `maxReasonCommentLines = 4` lines up) |
| `internal/standards` gate 2 (Rule 17) | `// env-leak-ok:` (with the space, with the colon) | the hit's line, or the contiguous comment block above (`linesAbove`) |

`// env-leak-ok` without the colon, or `// stylegate:ignore` with a space, does
NOT satisfy the corresponding gate. When one site must satisfy both, write both
literals — and note that the reason must carry at least `MinReasonLen = 10`
characters after the marker.

**Where the marker may sit matters too**: `lineComment` walks up from the
statement's own line and stops at the first blank/comment-free line, so an
escape split off into its own comment *paragraph* (blank line in between) is
silently unreachable. The marker belongs in the same contiguous block as the
statement.

## Line shift (escapes are line-keyed)

Adding the comment moved the `internal/shot` `os.Setenv` site `100 → 101`. The
Rule 17 escape moved with it and the standards data report quotes the new
number; the `cmd/agent` site is untouched (line 207).

## Verification (this run, master `92d14684`)

| check | result |
|---|---|
| `go test ./internal/stylegate/ -run TestStyleGate` — `test-setenv` | **RED before** (`gate_test.go:174: stylegate test-setenv: 1 test hit(s), ceiling 0`), **line gone after**: the emitted failures are the pre-existing `unchecked-close` (2) / `test-sleep` (9) / `test-skip` (2) / `ignored-error` (227 test, 9 production) rules, none of which this diff touches |
| `go test ./internal/shot/` | **ok** (61.2s) — the suite the marker's site belongs to |
| `gofmt -l internal/shot/launch_test.go` | no output |
| `git diff --numstat internal/shot/launch_test.go` | `1 0` — one added line, zero deletions |
| `git diff master -- internal/standards/baseline.go internal/stylegate/` | empty — no permit added, no ceiling moved |

The stylegate suite as a WHOLE is red on this base for reasons that live in
other files (`ignored-error` 227 vs ceiling 163, `unchecked-close` 2,
`test-sleep` 9, `test-skip` 2, plus 180 undecidable sites reported and never
guessed). Those reds are measured on the base itself and belong to their own
rows; this diff moves exactly one rule, `test-setenv`, from 1 to 0.

## Notes for the next reader

* **Keep the wording of an already-landed escape.** Master's `env-leak-ok`
  sentence is quoted by the standards data report and by `devdoc/`; a re-wording
  is a gratuitous diff against a reviewed record.
* **Never regenerate the whole manifest to fix one row.** Running
  `tools/docclass/classify.py` end-to-end re-anchors ~130 unrelated rows
  (master's own anchor drift), turning a one-line bookkeeping row into a large
  unrelated rewrite and a fresh conflict surface. Compute the class for the ONE
  doc and insert the row at its sorted position.
* A doc under `devdoc/` needs its manifest row AND, when its class is
  `historical` (`BANNERED_CLASSES`), the matching `<!-- doc-class: historical -->`
  banner — `TestDocClassBannerMatchesManifest` re-verifies the pair.
