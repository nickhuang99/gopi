<!-- doc-class: normative -->
# Same-identity line references in CONFIG.md — the anchor gate's second half

**Date:** 2026-10-13 · **Task:** `01a131791d3d74948c5a26be8c8b199a` · **Branch:** `task/01a131791d3d74948c5a26be8c8b199a`

## Completed

- **The blindness, closed for the class that carries a claim.** `CONFIG.md`'s
  `file.go:NNN` references were policed only for EXISTENCE
  (`TestConfigDocLineRefsResolve`): a reference that RESOLVES but lands on a
  comment, on another function, or on a closing brace passed green while the
  document confidently misdirected whoever followed it. A new predicate
  (`configDocNameIdentityFindings`) and its assertion
  `TestConfigDocNameCarryingRefsDeclareTheirSymbol` now require a reference that
  carries its own claimed symbol to point at the line that **declares** that
  symbol.
- **The reference population is read as one population.** The document has two
  reference FORMS: the full `` `path.go:NNN` `` span, and the bare
  `` `:NNN` `` continuation span meaning "the file named last, at this line".
  The historical failure lived on the second form (`:827` → `:854`), so a
  predicate reading only the first could not see the incident it was written to
  refuse. `configDocRefTokens` reads both, carrying the file forward.
- **18 drifted name-carrying references re-pointed** in `CONFIG.md` at the line
  that truly declares the claimed symbol — measured with `sed -n '<N>p'` and
  `grep -n`, never counted.
- **Red proofs** that run the SAME function the assertion calls: a minimal
  three-line Go fixture (wrong symbol → red, right symbol → control green, bare
  form → counted not enforced), a read-floor proof, and the historical fixture
  below.
- **The historical bytes are in the tree** as a fixture
  (`internal/standards/testdata/config-doc-historical-anchor/`): commit
  `5872b341e`'s two `GOPI_MERGE_ANCHOR_GATE` rows verbatim, plus the
  `internal/tools/merge_anchor_gate.go` (977 lines) those rows cited. The proof
  asserts the CONTRAST on those bytes rather than describing it: the
  existence-only predicate is **green** on them, the same-identity predicate is
  **red**, naming the two lines.

## The historical measured instance

Commit `5872b341e` (branch-only, `247…`-era) documented the new knobs as:

```
| `GOPI_MERGE_ANCHOR_GATE` | … | `internal/tools/merge_anchor_gate.go:92` (the constant) → `:827` (the ONE reader, `mergeAnchorGateDisabled`) |
| `GOPI_MERGE_ANCHOR_GATE_TIMEOUT_SEC` | … | `internal/tools/merge_anchor_gate.go:100` (the constant) → `:854` (the ONE reader, `mergeAnchorGateTimeout`) |
```

Measured on that commit's own bytes:

| cited line | what is actually there | the claim |
|---|---|---|
| `:92` | a comment (`// default below is the constant, …`) | the constant → declared at `:93` |
| `:100` | a comment (`// compile group must never hold …`) | the constant → declared at `:101` |
| `:827` | a comment naming **`mergeAnchorGateMergeTreeConflicts`** — a DIFFERENT function | `mergeAnchorGateDisabled` → declared at `:975` |
| `:854` | the bare comment `// reports.` | `mergeAnchorGateTimeout` → declared at `:952` |

Every one of the four RESOLVED, so the existence-only gate said ok. Two were
off-by-one onto the comment above the constant; two named the wrong function
outright. Re-pointed by hand in `8e4d7c4a6` — whose own commit message records
the same reading.

## The predicate's rule

**The witness.** The claim is read from the document's own typography, never
inferred:

- **prefix** — the identifier code span immediately BEFORE the reference,
  separated only by `(` or `,`:
  `` `config.ApplyEnv` (`internal/config/env.go:75`) ``
- **suffix** — the identifier code span inside the parenthetical AFTER it:
  `` (`ResolveMergerRepos`, `internal/config/config.go:1700`) ``

The LAST (nearest) such span wins, because a doc may name a container and then
the exact symbol — `` `config.GopiDir` → `config.ProfileDirIn`,
`internal/config/gopihome.go:58` ``. A package-qualified name is matched by its
base identifier.

**The declaration.** The cited line must be one of `func … Name(` (method
receivers included), `type Name`, `var Name`, `const Name`, `Name =` (constant
or field with a value), or `Name Type` (a struct field). **A comment line is
never a declaration, however exactly it names the symbol** — that single
exclusion is what refuses `:827` above. An unreadable file, a missing file, and
an out-of-range line all fail the claim rather than pass it.

**Same-identity, not proximity.** The proof pins this: pointing the second
historical row at its SIBLING reader's line (`:975`, a real function
declaration one screen from the right answer) is still refused, because the
check is the symbol's identity and not the neighbourhood of a line number. A
proximity-shaped check would have called that a repair.

## Honest scope — what it does and does not police

On `CONFIG.md` as committed: **208 reference tokens → 23 enforced (carry a
claim) and 185 bare**, and the assertion FAILS if the enforced count falls under
its read-floor (`configDocMinWitnessedRefs = 10`) or the bare count under
`configDocMinUnwitnessedRefs = 50`. Both counts are reported in every failure
message, so the enforced share is visible rather than implied.

**Not policed, and named as such rather than glossed:**

- **The 185 bare references stay existence-only.** A bare reference can still
  point at a comment — the existence-only predicate's blindness is unchanged for
  them. They are not checkable by same-identity (no claim to compare against),
  and asserting them would mean reading the surrounding prose. Measured: of the
  112 full references, 87 point at a comment and 8 at a BLANK line today. That
  is the residual class, and it is a real limit of this predicate.
- **The GENERATED §2 tables** — a bare file reference in a value cell, with no
  claimed symbol anywhere on the row. Outside the claim grammar by construction.
- **A file named twice on one doc line** (`` `internal/config/env.go:75`,
  `internal/config/env.go:75` ``), where "the nearest identifier" is not the
  neighbouring token's claim.
- **References whose text names a container rather than a symbol** (a `§` section
  number, a doc path) carry no witness and are counted, not enforced.

A follow-up row is filed for the residual bare-reference class (see below) —
it is a separate change with its own acceptance, not a weakening of this one.

## Not Done (honest)

- **The 185 bare references are not policed** (above). They are counted, and the
  count is a floor, not a claim of correctness.
- **The historical fixture pins `5872b341e`'s rows**, which exist on no branch
  master holds. The fixture is the evidence and is committed; it is bytes, not a
  live document, so it will not drift with the tree — and it is deliberately NOT
  regenerated by anything.
- **`gopi tool doccheck` was not run** — the report landed through the `write`
  tool, whose classification hook owns the DOC-INDEX row; see Evidence.

## Verification

- **Build:** `go build ./...` → **OK** (exit 0, no output).
- **Tests:** `go test ./internal/standards/ -count=1` → **`ok github.com/nick/gopi/internal/standards 35.512s`** (whole gate package, real tree).
  - `go test ./internal/standards/ -run 'TestConfigDocGateRedProof' -count=1` → **`ok … 0.567s`**.
  - `go test ./internal/standards/ -run TestConfigDoc -count=1` → **`ok … 3.151s`** (green on the real tree AFTER the re-point; the same test was RED before it, naming all 18 — see Result).
- **Ran:** the gate's own assertion, verbatim —
  `go test ./internal/standards/ -run 'TestConfigDocNameCarryingRefsDeclareTheirSymbol' -count=1`
  Before the `CONFIG.md` re-point (the red state, observed):
  ```
  CONFIG.md carries 18 name-carrying line reference(s) that do not declare the claimed symbol (23 enforced, 185 bare existence-only):
        chat_session_slots.go:56 does not declare `ChatSessionSlotLimit`
        colour.go:258 does not declare `CapabilityForEnv`
        config.go:1053 does not declare `config.PublishScratchGCStaleMin`
        config.go:1119 does not declare `ResolveRepoBuild`
        config.go:1205 does not declare `ResolveMergerRepos`
        config.go:186 does not declare `Config.SetEnvVars`
        config.go:400 does not declare `RepoConfig`
        config.go:514 does not declare `config.ResolveNotify`
        config.go:706 does not declare `config.ResolveSettingsPath`
        env.go:225 does not declare `config.ApplyEnv`
        flags.go:23 does not declare `config.FlagOverrides`
        gopihome.go:82 does not declare `config.ProfileDirIn`
        settings.go:133 does not declare `EnvVars`
        settings.go:477 does not declare `config.BootstrapEnvFromArgs`
        settings.go:90 does not declare `config.FromSettingsFile`
        task_finish_premerge.go:238 does not declare `finishCleanupBranchCheckOn`
        toolchain_cache.go:357 does not declare `scratch.ResolveToolchainCacheMaxBytes`
        usage_test.go:120 does not declare `TestTopLevelUsageDelegatesEnvVarsToHelpEnv`
  ```
  And the historical contrast, from `go test ./internal/standards/ -run TestConfigDocGateRedProofHistoricalWrongRowsAreRed -count=1 -v`:
  ```
  5872b341e's row 2 cites internal/tools/merge_anchor_gate.go:854, which is: "// reports."
  existence-only predicate on those bytes: 0 findings (GREEN)
  ```
- **Result:** after re-pointing, `go test ./internal/standards/ -run TestConfigDoc -count=1` prints `ok … 3.151s` — **green by measurement, with the predicate unchanged** (the only edits between the red run and the green run were the 18 line numbers in `CONFIG.md`). The historical proof stays red-on-wrong / green-on-right, and the sibling-line mis-repair is refused.
- **Commit:** `0a1fd9e3e` `fix(standards): a CONFIG.md line reference must declare the symbol it claims` (branch `task/01a131791d3d74948c5a26be8c8b199a`; shared with the main repo — no push needed, Rule 6c).

## Evidence (self-filed)

- Every re-point was measured, not counted. Example, `config.go:186` → `config.go:248`:
  `grep -n 'SetEnvVars' internal/config/config.go` → `241` (comment), **`248:	SetEnvVars map[string]bool \`json:"-"\``** (the field the doc names via `Config.SetEnvVars`), plus the readers at 129/132.
- The 18 findings above are the gate's own output (copied verbatim, not restated).
- The historical bytes: `git show 5872b341e:internal/tools/merge_anchor_gate.go | sed -n '827p;854p;952p;975p'` → a comment naming `mergeAnchorGateMergeTreeConflicts`, `// reports.`, `func mergeAnchorGateTimeout() (int, error) {`, `func mergeAnchorGateDisabled() bool {`. Saved under the session's `artifacts/historical-wrong-rows.txt`.
- The fixture's fidelity: `git show 5872b341e:CONFIG.md | grep 'merge_anchor_gate.go'` and `git show 5872b341e:internal/tools/merge_anchor_gate.go | wc -l` → 977 lines, matching the committed fixture.
- `git status --porcelain` at hand-over: see the close-out run; the worktree is committed clean (Rule 30).

## Follow-up filed (Rule 41)

The residual class — **bare references that resolve onto a comment or a blank
line** (measured: 87 comment targets and 8 blank targets of 112 full references
in `CONFIG.md`) — is filed as its own task row rather than folded in here, since
policing it means reading the surrounding prose and is a different predicate with
a different failure mode.
