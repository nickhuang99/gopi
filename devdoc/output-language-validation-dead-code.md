<!-- doc-class: historical -->
# Task Report: the reply-language knob's validation was dead code on the real path

**Epic**: `01a0e04c-bb5b-7285-a0bf-7e40f0dc25d1` (worktree
`~/workspace/gopi-01a0e04cbb5b7285a0bf7e40f0dc25d1`, branch
`task/01a0e04cbb5b7285a0bf7e40f0dc25d1`).
**Scope**: the VALIDATION/REFUSAL path only — sibling task `01a0e049` owns the
help enum-value vocabulary and was not touched.

## The defect

`internal/config/output_language.go` had complete, correct Rule 15 validation: a
shape check, a named stderr refusal, and a "the next channel decides"
fallthrough. It was **unreachable on every surface a user actually meets**.

The resolution's ONE production caller was the prompt path
(`systemPromptWithOutputLanguage` in `internal/agent/loop.go`, reached from
`assembleSystemPrompt` / `chatSystemPromptServed`). But `gopi -h`,
`gopi --help-env`, `gopi config show`, `gopi chat` and a task run all **return
from `main()` before that path** — the top-level help scan and the subcommand
chain both exit there. So a declaration the operator *wrote* was read by
nothing:

| surface | `GOPI_OUTPUT_LANGUAGE=chinese` | illegal `--output-language` |
|---|---|---|
| `gopi -h` | rc=0, **0 bytes** | rc=0, **0 bytes** |
| `gopi --help-env` | rc=0, **0 bytes** | rc=0, **0 bytes** |
| `gopi config show` | rc=0, **0 bytes** | rc=0, **0 bytes** |

Worse, the knob was *advertised* on those very surfaces (`gopi -h` carries an
`--output-language TAG` row): the page promised a knob the same process did not
validate. And the second half of the defect was in the resolver itself — it
collapsed **two different facts into one value**: "nobody asked" and "I refused
what you wrote" both returned `source = not declared`, contradicting the
contract its own comment declared.

## The two changes

**1. One entry point, called where it cannot be bypassed** —
`config.OutputLanguageReport()` (`internal/config/output_language.go`), called
from `cmd/agent/main.go` **after** `BootstrapEnvFromArgs` (so the
`--output-language` flag has already been hoisted into the env — that hoist is
what makes the flag visible to these surfaces at all) and **before** the
subcommand chain.

It takes no arguments and returns nothing, because it *is* the resolution: there
is no second validator (Rule 2), so the prompt path and the non-prompt surfaces
cannot drift into two opinions of what a language tag is. What it adds is the
report for the runs no prompt is ever assembled for.

**2. The two empty states are distinct** — `outputLanguageUnset` ("not
declared") vs `outputLanguageRefused` ("refused"). A refusal is now reported
**where it happens** (naming the channel and the value) *and* **carried to the
end of the chain**, where one site (`refuseOutputLanguage`) states the chain's
verdict:

```
config: GOPI_OUTPUT_LANGUAGE="chinese" is not a language tag (want e.g. zh, zh-CN, en-GB) — this channel supplies no value; the next channel decides
config: GOPI_OUTPUT_LANGUAGE="chinese" was not adopted; no channel supplied a usable value, so no directive is emitted
```

…and when a later channel *did* supply a value, the same line says so
(`was not adopted; adopting output_language="zh-CN" instead`) — the "next
channel decides" contract, observed from outside the process.

**The silence contract is preserved**: a usable or unset knob prints **nothing**
(an unset knob must emit a byte-identical prompt on every machine that
configured nothing). Only two prose claims are exported — `OutputLanguageRefusalPhrase`
and `OutputLanguageNotAdoptedClaim` — so the tests in *both* packages match the
production wording instead of a second copy of it (Rule 14).

## The trap the binary-level test caught (in my own first draft)

The first version passed `refused` as the *report's second* argument, so a
**settings-only** refusal (the last channel, no earlier refusal, `refused == ""`)
printed **nothing**: the very silence this task exists to remove, reproduced one
branch over. The subprocess test flagged it (`settings refused / top-level help`
was the one case red on the fixed tree). Fixed by naming the settings refusal
explicitly at its own site. *A unit test of the resolver would not have caught it
— the resolver's return values were right; the reporting call was wrong.*

## Verification

- **Build**: `go build ./...` → OK.
- **Tests**: `go test ./internal/config/ -run OutputLanguage` → ok;
  `go test ./internal/agent/ -run 'OutputLanguage|SystemPrompt'` → ok;
  `go test ./cmd/agent/ -run TestOutputLanguageValidation` → **10/10 PASS**;
  `go test ./internal/standards/ -run TestConfigDoc` → ok.
- **Ran (the surface actually changed, real binary, real args)**:
  - Before: `GOPI_OUTPUT_LANGUAGE=chinese <pre-fix gopi> -h 2>&1 >/dev/null | wc -c` → **0**;
    after: → **319**. Same for `--help-env` (**0 → 319**), `config show`
    (**0 → 319**) and an illegal `--output-language` (**0 → 345**).
  - Silence preserved: usable (`zh-CN`) and unset → **0 bytes** on the fixed binary.
- **Red proof, binary level**: the same subprocess test against a binary built
  from the pre-fix commit (`GOPI_TEST_BIN=<pre-fix>` → **7/10 subtests FAIL**,
  every refusal case with zero stderr; the 3 silence cases pass in both, which is
  what stops a chatty "fix" from passing).
- **Red proof, unit level**: a minimal literal-spelled probe run in a detached
  worktree at the pre-fix commit (`abcd75dd`) → FAIL with
  `a REFUSED declaration reports source="not declared"`.

## Files

- `internal/config/output_language.go` — `OutputLanguageReport`, the
  refused/unset constants, `refuseOutputLanguage`, exported prose claims.
- `cmd/agent/main.go` — the pre-subcommand-chain call (§2b).
- `internal/config/output_language_test.go` — the two-states contract + the
  entry point's behavioural test.
- `cmd/agent/cli_output_language_validation_test.go` — the 10-case real-binary
  surface test (the only instrument that can see a *caller-side* defect).
- `CONFIG.md` — the knob's row: moved line refs (`141`/`113`/`273`) + the new
  surfaces and the refused-vs-not-declared distinction.

## Not Done (honest)

- `gopi chat` and `task run` are not driven end-to-end here: they need a live
  model. They are covered structurally — both return from `main()` at the chain
  the call precedes — and by the `version` case, which proves the call is not
  attached to one verb's path.
- **A flag-only refusal names the ENV VAR, not the flag** (measured on the merged
  binary, 2026-09-27): `gopi --output-language "chinese (simplified)" -h` prints
  `config: GOPI_OUTPUT_LANGUAGE="chinese (simplified)" is not a language tag …`.
  The cause is deliberate and stays: `BootstrapEnvFromArgs` publishes the flag
  into `OutputLanguageEnv` so exactly ONE reader sees all three channels
  (Rule 2), and by the time the resolution runs the origin is no longer
  recoverable from the value alone. The refusal is NOT silent and the value
  quoted is the one the user typed, so the operator is informed either way — but
  a user who passed only a flag does not see the spelling he used. Fixing this
  properly means carrying the channel's ORIGIN (a typed enum, not a string)
  through the hoist, which is a wider change than this epic's path and would
  touch the sibling knob hoists that share the same shape. Left as-is and named
  here rather than papered over.
- The report fires **once per process**, before any verb. A `gopi config show`
  run therefore prints the refusal to stderr while its *stdout* report still
  lists the settings file's raw `output_language` key (the report reads the
  FILE, not the resolution). Making `config show` render the resolved value with
  its provenance is a separate, larger change and was not in this epic's scope.
