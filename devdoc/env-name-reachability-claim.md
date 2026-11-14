<!-- doc-class: historical -->
# The env-name reachability claim — narrowed to what the parser keeps

**Task** `01a0de69-edfa-770e-b104-0d51ced38eb4` (bug) · **measured** 2026-09-27 ·
`internal/config` · Rules 1/5/13/14/15/30/31

## The defect

`gopi -h` promised, with no qualification, that *"Any other knob is reachable
with its full env var name as a flag (e.g. --gopi-timeout-sec 600 ≡ --timeout
600)"*. The parser keeps that promise for the catalog rows that HAVE a flag. It
cannot keep it for the rows that have none, and it said the wrong thing about
why.

Measured on the built binary over the whole catalog — every row of
`gopi --help-env` — probing `gopi --<env-as-flag> 1 version`:

| | rows | bare `unknown flag` before | after |
|---|---|---|---|
| em dash in the flag column (no flag of its own) | 45 | **45** | **0** (refused by name) |
| flag-bearing | 43 | 0 | 0 |

The em-dash rows are real knobs — `GOPI_BASH_LOCALE`, `GOPI_CODE_SEARCH_ROOT`,
`GOPI_WEB_TOOLS`, `GOPI_TASK_STALL_SEC`, … — so `gopi: unknown flag
--gopi-bash-locale` was false in its first word (the name is known) and useless
in its second (it sends the operator hunting a typo that is not there). 44 of 86
was the count recorded in the briefing; this run measured 45 of 88 — the table
grew, and the counting difference is the non-`GOPI_`-prefixed rows.

**This is the residual half of a known finding, and the fixer must not re-derive
it.** The 2026-09-02 config-layer review (memory `#1376`, P0-2) found the same
false claim and fixed the rows that have a real flag (commit `347331a`). The
empty-`CLIFlag` registration defect (`envFlag[envNameAsFlag(e.Env)] = e.CLIFlag`
with no guard) was also already fixed — `internal/config/flags.go` now opens the
catalog loop with `if e.CLIFlag == "" { continue }`. What was left is exactly
this: the unqualified sentence, and a refusal that hid the reason.

## What changed

1. **The claim says what is true.** `internal/config/help.go` —
   `reachabilityClaimSubset`, rendered by `FormatUsage`:

   ```
   A knob that has a CLI flag is also reachable by its full env var name as a
   flag; a knob with none is env + settings only (— in `gopi --help-env`).
   ```

   **The sentence is two lines because a gate says so, not for taste.**
   `cmd/agent/usage_test.go` holds `gopi -h` to a 160-line budget
   (`TestTopLevelUsageDelegatesEnvVarsToHelpEnv`), and this work first landed a
   5-line version that took the page to 163 and turned it red — with the gate's
   own message naming the remedy ("move detail to `gopi <cmd> -h` /
   `gopi --help-env`"). The detail the long form carried (how to reach a
   flagless knob, that the refusal names the reason) moved to `--help-env`, which
   is where the reader meets the em dash anyway. Trimming it is not a retreat
   from the fix: the universal promise is gone, and the subset + the mark are
   both still on the page.
2. **The em dash is explained — twice, where each reader meets it.**
   `FormatEnvTable`'s preamble now names the three columns' meaning (`--flag`
   also accepts the env-name spelling; `—` is "no flag of its own", env +
   settings key only), replacing *"Every knob below has three equivalent
   spellings … a CLI flag"*, which was the same false universality one screen
   lower. `CONFIG.md` §2c already stated the `—` rule correctly; it now also
   names the refusal, so the doc and the CLI agree.
3. **The refusal names WHY, from the same table the help renders from.**
   `flaglessEnvNames()` (`flags.go`) is derived from the `EnvVars` catalog — the
   rows with `CLIFlag == ""` — and `flaglessEnvRefusal` builds:

   ```
   gopi: --gopi-bash-locale names GOPI_BASH_LOCALE — a real GoPi knob with NO CLI
   flag of its own (the flag column of `gopi --help-env` shows an em dash for it).
   Reach it through its environment variable GOPI_BASH_LOCALE. The env-name-as-flag
   spelling covers the flag-bearing rows only.
   ```

   The settings-key half of the "reach it through" phrase is named only when the
   row has an editable key — the settings-file path row is a pointer, not a key,
   and advertising it as one would be the same defect one layer down.
   `FlagOverrides` consults `flagless` only where it previously fell through to
   `unknown flag`, so every spelling that already worked is untouched by
   construction.
4. **One text for "no such flag".** `unknownFlagFmt`/`unknownFlagPrefix` — the
   parse loop and `applyFlag`'s default arm (a catalog row whose flag exists but
   whose case does not, formerly `unhandled flag --x`) now emit the same
   wording, so there is no second, differently-spelled "unknown flag".

## The gate (claim ↔ reality, whole catalog)

`internal/config/help_env_claim_test.go` — three tests, all driving the REAL
surfaces (`FlagOverrides`, the `flagArity` tables, `FormatUsage`,
`FormatEnvTable`), never a copy of the catalog:

- `TestEnvNameClaimMatchesReality_WholeCatalog` — for EVERY catalog row: a
  flag-bearing row's env-name spelling must parse; a flagless row's must be
  refused with a message that does NOT start with `unknown flag --` and that
  names the env var, the spelling and the reason. So adding a row with no flag
  cannot silently reinstate the false universality, and adding a flag to a row
  cannot leave a stale refusal behind.
- `TestHelpEnvTableEmDashMatchesFlaglessSet` — parses the RENDERED table's flag
  column and asserts the three-way equality: em dash ⟺ `CLIFlag == ""` ⟺
  membership in the by-name refusal set. It also parses each flagless spelling
  through the real parser and requires the refusal to name the variable.
- `TestHelpTextClaimsOnlyTheFlagBearingSubset` — the prose gate: the universal
  sentence must be ABSENT from `gopi -h`, the subset sentence and the em dash
  must be PRESENT, and the env-table preamble must explain the mark and no longer
  claim three equivalent spellings.

`TestHelpEnvNameSentenceMatchesParser` (`config_test.go`) was the existing
sentence-pin; it now reads the shared `reachabilityClaimSubset` constant instead
of a substring of the old wording, and
`TestFlagOverridesEnvNameNoFlagInCatalog` was strengthened from "an error
happens" to "the error does not call the name unknown".

## Verification (see the task report for the verbatim run)

- `go build ./...` clean; `go test ./internal/config/` PASS.
- The catalog gate logs `catalog rows=88: flag-bearing=43 dispatch, flagless=45
  refused by name`.
- Real binary, all 88 rows: 45/45 em-dash rows refused by name, **0** bare
  `unknown flag`; 43/43 flag-bearing rows dispatch or refuse with a named reason,
  **0** bare — the RED baseline before the fix was 45/45 bare.

## Residual (honest)

- The anchor in `devdoc/docanchor-baseline-repair.md` that recorded the old
  sentence at `internal/config/help.go:413` was superseded by this change (the
  fix IS the reason that line no longer matches); the record now says so in
  place, and the doc anchor gate still reports the pre-existing baseline drift
  it reported before this branch — 41 new entries whose files are
  `internal/agent/loop.go` and `internal/tools/cov_finish_cleanup_test.go`, no
  `internal/config` anchor among them (verified against pristine `master` — see
  the report).
- `reachabilityClaimSubset` is a constant the gate compares against the rendered
  help; a spoofed help that carried both the subset sentence and a separate
  universal promise would pass the "present/absent" checks. The catalog half of
  the gate is unaffected — it drives the parser, not the prose.
