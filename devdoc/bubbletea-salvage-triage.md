<!-- doc-class: historical -->
# devdoc: bubbletea-poc-20260928 salvage triage — 24 files, one survivor

**Run:** task `01a0e922-73f7-7e65-be26-fa669450041d` · branch `rework/salvage-bubbletea-20260928`
**Base:** `cb89fff6` (cut from master) · **integration branch at close:** master `7de8045c`
**Salvage tip (read-only evidence, never a place to continue — Rule 23):** `145738c1`
**Reconstructed artifact:** `git merge-base master 145738c1` = `29d0fb75`

## The situation

The branch `salvage/bubbletea-poc-20260928` had its ref deleted; only a
24-file / 4347-line `git diff master...tip` survived, gzipped, as
`merge-archives/salvage_bubbletea-poc-20260928.diff.gz`. A one-shot `gopi merge`
could not land it (substantive, order-dependent conflict).

Per the briefing, the diff was **never applied and never merged**. The INTENT was
recovered from it, then every intended change was measured against **current
master** — which has moved: its tip `cb89fff6` is a descendant of the intent's own
landing.

## Measurement first: the intent already LANDS on master (ancestry, not inference)

```
git merge-base --is-ancestor 1c15a7a0 HEAD   # 1c15a7a0 = "feat(ui): land the Bubble Tea
                                             # terminal chat surface on current master"
landing1c15_ancestor_rc=0                    # ← the intent IS on master
git merge-base --is-ancestor 145738c1 HEAD   # 145738c1 = the salvage branch's tip
salvage_ancestor_rc=1                        # ← the branch itself never landed; its
                                             #   INTENT did, via a sibling branch
```

So this is the productive case from memory #4452 / #4439: a stale rework branch
whose intent is **mostly landed** because master moved and independently fixed the
same thing. The rework collapses to the true survivors.

## The 24-file triage (measured, not asserted)

Method — reproduce the archived artifact first, then measure each path twice,
because one number is not enough to decide anything:

```
MB=$(git merge-base master 145738c1)          # 29d0fb75
git diff --shortstat $MB..145738c1             # 24 files, 4047 insertions(+), 8 deletions(-)
git diff --shortstat master...145738c1         # 24 files, 4047 insertions(+), 8 deletions(-)  ← same
```

Both spellings agree byte-for-byte with the archived shortstat, so the artifact is
reproduced. The two columns below are the decision instrument:

- `branch_own` = `git diff MB..<tip> -- <path> | grep -c '^+[^+]'` — what the
  BRANCH was trying to add.
- `artifact` = `git diff master..<tip> -- <path> | grep -c '^+[^+]'` — what the
  `master...tip` artifact attributes to the branch. **The gap between the columns
  is master's own forward motion**: where `artifact ≫ branch_own`, the artifact's
  `+` lines are mostly master's *deletions* of lines the branch still carries.

`artifact = 0` means **master adds no line the branch lacks** on that path. It does
**not** mean the files are equal — measured with `git rev-parse <rev>:<path>`, only
**3 of the 24** are byte-identical (`go.mod`, `go.sum`, `internal/uiclient/doc.go`);
the other `artifact = 0` paths differ in the other direction, master being larger
(`cli_ui_test.go` 552 vs 293 lines, `run.go` 154 vs 94, `wire.go` 248 vs 236,
`drive_ui.py` 341 vs 289, `cli-bubbletea-carrier.md` 224 vs 223 — its one extra line
is the Rule 36 `<!-- doc-class: historical -->` banner). In every one of those,
**master is the superset**.

| path | branch_own | artifact | on master | verdict |
|---|---|---|---|---|
| `cmd/agent/cli_ui.go` | 535 | 26 | yes | **landed** — master restructured it (1192 lines vs 582; 15 master-only funcs) |
| `cmd/agent/cli_ui_test.go` | 278 | 0 | yes | **landed** — master superset (552 vs 293 lines) |
| `cmd/agent/cli_ui_wire_guard_test.go` | 182 | 2 | yes | **landed** — master superset (270 vs 196) |
| `internal/uiclient/client_test.go` | 857 | 6 | yes | **landed** — master superset (1022 vs 923); the 6 are the branch's own `//nolint` comment lines |
| `internal/uiclient/model.go` | 625 | 16 | yes | **landed** — master 987 lines vs branch 685 |
| `internal/uiclient/stream.go` | 207 | 1 | yes | **landed** — master 271 vs 223 |
| `internal/uiclient/wire.go` | 210 | 0 | yes | **landed** — master 248 vs 236 |
| `internal/uiclient/run.go` | 88 | 0 | yes | **landed** — master 154 vs 94 |
| `internal/uiclient/doc.go` | 29 | 0 | yes | **landed** — byte-identical (29 = 29) |
| `devdoc/cli-bubbletea-carrier.md` | 183 | 0 | yes | **landed** — master superset (+the doc-class banner) |
| `scripts/ui/drive_ui.py` | 258 | 0 | yes | **landed** — master 341 vs 289 |
| `scripts/ui/render_transcript.py` | 155 | 14 | yes | **landed** — master 249 vs 174 (its CSI/OSC scanner is stricter) |
| `go.mod` | 22 | 0 | yes | **landed** — byte-identical (bubbletea v1.3.10) |
| `go.sum` | 36 | 0 | yes | **landed** — byte-identical |
| `cmd/agent/cli_completion.go` | 1 | 13 | yes | **superseded** — master's list is *larger* (`CliVerbUI`, `CliVerbUIState`, archive/capture/config/doccheck/sandbox/setup/cli restored) |
| `cmd/agent/cli_contracts_test.go` | 4 | 25 | yes | **superseded** — master enumerates more verbs |
| `cmd/agent/cli_completion_test.go` | 1 | 2 | yes | **superseded** |
| `cmd/agent/usage_test.go` | 1 | 3 | yes | **superseded** |
| `cmd/agent/main.go` | 1 | 452 | yes | **landed** — `isUISubcommand(dispatchArgs)` is wired (main.go:554) |
| `cmd/agent/cli_verbs.go` | 7 | 17 | yes | **landed** — `CliVerbUI = "ui"` (cli_verbs.go:189) |
| `cmd/agent/cli_web.go` | 16 | 75 | yes | **landed** — `webToolsFlag`/`webToolsEnv`/`webToolsEnabled` all present |
| `internal/config/help.go` | 1 | 42 | yes | **landed, richer** — master's row names `gopi ui` **and** `gopi ui_state` |
| `internal/config/settings.go` | 1 | 11 | yes | ← **the one survivor, below** |
| `internal/scratch/scratch.go` | 5 | 6 | yes | **dead code — deliberately NOT revived** (below) |

## Capability check: what the branch carries that master does not

The line columns above decide nothing on their own — a large `artifact` number can
mean master *deleted* lines the branch still holds. Two checks were run, and the
second corrected the first.

**Check 1 — per-file function sets (`comm -23` on `^func`).** This said
`internal/uiclient/model.go` had one branch-only function, `wrapEntry`, and that
master's `wrapText` supersedes it.

**Check 2 — repo-wide symbol presence (`git grep`), which DISPROVED check 1.**
`wrapEntry` **exists on master**:

```
git grep -n 'func.*wrapEntry' master -- '*.go'
master:internal/uiclient/model.go:788:func (m model) wrapEntry(e entry, width int) []string
```

Check 1 missed it because its regex (`^func [A-Za-z_]\w*`) does not match a
**method** (`^func (m model) …`). So the claim "`wrapEntry` is the only function the
branch has that master does not" is **false**, and it is withdrawn. On master
`wrapEntry` is a method (richer: it takes `entry` and selects a label per kind),
while the branch had it as a free function — a refactor, not a gap.

**What the repo-wide diff actually shows.** Taking every top-level declaration
(`func`/`const`/`var`/`type`) on the salvage tip and subtracting master's gives 465
names. **336 of them match `merger|takeover|progress|tag|revision|verdict|serve|
cleanup|sweep|lock`** — i.e. they are the **resident-merger / take-over / version-tag
subsystem that master RETIRED** (2026-09-19 and 2026-09-27,
`devdoc/merger-retired.md`), which the branch's base predates. They are the branch
being **behind**, not an intent master lacks. That same retirement is why
`internal/scratch/scratch.go` shows `RuntimeClassMergerTakeover` on the branch
(4 hits at the tip, **0 on master**): its consumer `cmd/agent/merger_takeover.go`
does not exist on master.

The remaining names are test helpers and local counters inside those retired files.
**No UI-surface capability was found that master lacks** — the carrier itself is
complete on master: `cmd/agent/cli_ui.go` has **96 more lines** than the branch's
version and 15 master-only functions, `internal/uiclient/model.go` 987 vs 685,
`scripts/ui/render_transcript.py` 249 vs 174 (master's CSI/OSC scanner is stricter —
it distinguishes "not a CSI" from "a CSI whose final byte has not arrived yet").

**Honest limit of this claim.** The symbol-set diff (465 names) is noisy: its regex
does not see declarations inside a `const (` block, so it reports false tip-only hits
(e.g. `webLANWarning`, which master declares in 3 files). It is used here only for
the 336-name retirement count, which was spot-checked. It is NOT used to claim
"master is a superset in every code path" — that stronger sentence is withdrawn; the
per-file `git diff` readings in the table above are the evidence, and where the
reading is only a reading, this doc says so.

## Two branch deltas that are NOT survivors (measured, then refused)

**1. `internal/scratch/scratch.go` — the branch is BEHIND, not ahead.** Its 5
branch-own additions are one runtime-class constant,
`RuntimeClassMergerTakeover = "merger-takeover"`, documented in the diff as the
staged prompt file of `cmd/agent/merger_takeover.go`. That file **does not exist
on master** (`ls cmd/agent/merger_takeover.go` → rc=2): the resident merge tick was
**RETIRED 2026-09-27** (`devdoc/merger-retired.md`). Reviving the constant would
catalogue a runtime class no producer can create — precisely the "row advertising
a knob nothing honours" defect the retirement removed. Master also carries
`MkdirTempIn` + `WriteOwnerRecord` on this path, which the branch does not.

**2. The completion/contracts test files keep a SMALLER verb set than master.**
Their deltas are the branch *removing* verbs (`archive`, `capture`, `config`,
`doccheck`, `sandbox`, `setup`, `cli`, `keyword_search`). Re-applying them would
delete live verbs from master's tab completion. Refused.

## The surviving intent (re-implemented, not re-applied)

One genuine gap. Master's own terminal carrier **consumes** the shared switch:

```
cmd/agent/cli_ui.go:592:  func uiToolsFromEnv() bool { return webToolsEnabled() }
cmd/agent/cli_web.go:821: func webToolsEnabled() bool { return os.Getenv(webToolsEnv) != "" }
```

…yet both surfaces that DESCRIBE the switch told the user it was `gopi web`-only —
so `GOPI_WEB_TOOLS=1 gopi ui` had its tools enabled by a knob the documentation
said belonged to a different verb. `gopi ui`'s own `-h` already says
(`cli_ui.go:1187`) that `webToolsEnv` "(the SAME switch `gopi web`…)" enables tools
— the catalog and CONFIG.md were the two that had not followed.

Applied (the intent, on master's current text — not the branch's bytes):

- `internal/config/settings.go` — `EnvVars` row for `GOPI_WEB_TOOLS`: description
  now names both carriers (`gopi web` / `gopi ui`).
- `CONFIG.md` — the same row (Rule 12: the doc updates in the same commit), line
  reference unchanged, so `internal/standards/config_doc_gate_test.go` still
  resolves.

Nothing else from the 24 files was re-implemented, because nothing else was
missing: re-touching landed files is exactly what recreates the conflict this
rework exists to avoid.

## What this doc is NOT

It does not claim the salvage branch's 4047 inserted lines were re-delivered — they
were already on master. The honest finding is that **23 of 24 files needed no work
at all**, and the measured, user-visible defect left behind was one knob whose
documentation named one of its two readers.
