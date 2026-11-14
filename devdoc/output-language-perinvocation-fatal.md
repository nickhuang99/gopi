<!-- doc-class: historical -->
# Devdoc — the reply-language knob's per-invocation channels are FAIL-CLOSED

Task `01a0e2e6-5300-7369-bc42-adcd7cb17c81`, branch
`task/01a0e2e653007369bc42adcd7cb17c81`, base master `6936c787`.
Predecessor work: `devdoc/output-language-validation-dead-code.md` (the fix that
made the knob *reported* on every surface) — this change is the second half:
*reported* is not *refused*.

## The defect, measured

The knob's validation was correct and its report reached every surface, and the
process still exited **0** having run the verb:

```
$ gopi --output-language "not a lang!!" version ; echo $?
config: GOPI_OUTPUT_LANGUAGE="not a lang!!" is not a language tag (want e.g. zh, zh-CN, en-GB) — this channel supplies no value; the next channel decides
config: GOPI_OUTPUT_LANGUAGE="not a lang!!" was not adopted; no channel supplied a usable value, so no directive is emitted
v0.1.0                      ← the version verb RAN
0                           ← and this is the whole defect
```

Same for `GOPI_OUTPUT_LANGUAGE=chinese gopi version`. The refusal text was
present and correct, so a text-asserting test stayed green: **only the exit code
can express "this run did not happen"**, and nothing asserted it. An explicit,
per-invocation declaration was indistinguishable, by status, from a typo that
did nothing.

## Root cause (5-why)

1. The process exited 0 with an unusable explicit declaration — why?
2. Because `config.OutputLanguageReport()` only *reports* — why?
3. Because the resolution it calls is a **fall-through** resolver: a channel that
   fails the shape check hands over to the next channel, which is right for the
   *resolution* (settings may still supply a value) and wrong for the *policy*
   (an unusable command line is not a channel to skip) — why?
4. Because the two questions were never separated: `resolveOutputLanguage`
   answers *which language and which channel decided*, and the caller asked it
   to also answer *should this process run at all* — why?
5. Because the knob shipped with one policy for three channels of different
   kinds. A per-invocation declaration is a statement about **this run**; a
   settings value is a statement about **this machine**, read by every gopi
   process on it. One policy cannot be right for both.

## The pinned decision (the asymmetry is the design)

| channel | declared by | unusable value |
|---|---|---|
| `--output-language TAG` | this command line | **FATAL** — exit 2, before the subcommand chain |
| `GOPI_OUTPUT_LANGUAGE=TAG` | this run's environment | **FATAL** — exit 2, same predicate |
| settings.json `"output_language"` | the machine | **warn + continue** — reported, exit 0 |

Why the settings channel keeps the softer policy, stated once in the source and
in `CONFIG.md`: it is read by **every** gopi process on the host — a background
worker, a merger tick, `gopi -h` — so one stale line there would fail *every
command run on that machine*, with the remedy (edit that file) unavailable from
the failing invocation and the failure attributed to whatever the operator
happened to run. The person who gets the fatal is the person who typed the
value, and the fix is on the same command line he is already editing. That is
the whole justification, and it is why making the settings channel fatal would
be a worse defect than the one this task closes.

Corollary, and it is load-bearing: the fatal fires **only** on an explicit
declaration. A value that merely reached the environment from settings.json
(`ApplyEnv`'s fill) is not a command line — which is why the gate scans **argv**
for the flag's presence rather than inspecting the env. `GOPI_OUTPUT_LANGUAGE=`
(exported but empty) is **not** a declaration: the knob's documented "absent or
empty means no directive" contract keeps it silent and exit 0.

## Shape of the fix

- `internal/config/output_language.go` — the fatal policy lives beside the
  resolution it judges (one file, so the fatal and the warning are readable side
  by side):
  - `OutputLanguagePerInvocationExitCode = 2` — the POSIX **usage** class, the
    same value `cmd/agent` uses for a refused worker command line, deliberately
    not 1 ("the run failed" — nothing ran) and not `AgentGuardRefusalExitCode`
    ("this launch is forbidden" — a different operator action).
  - `OutputLanguageFlagWasGiven(args)` — the argv scan that decides whether the
    declaration was explicit. It mirrors `BootstrapEnvFromArgs`' token grammar
    (space and `=` forms) and returns a **dangling flag** as present-with-no
    value, which is what makes `--output-language` at the end of argv refuse
    instead of being read as "no flag at all".
  - `OutputLanguageRefuseUnusablePerInvocation(args)` — the ONE gate, called by
    `cmd/agent/main.go` **before** the subcommand chain. It re-publishes a
    usable flag into `OutputLanguageEnv` (`os.Setenv`, the same unconditional
    publish the hoist performs) so the ONE representation the resolution reads
    is set by one predicate, and it exits via the ONE fatal site.
  - `outputLanguageFatal(...)` — the ONE message shape for both channels. It
    names: the declaration as written, the class of the failure ("is not a
    language tag" / "received no value"), the channel-specific hint (the flag
    was on THIS command line / the variable was exported for THIS run), the
    legal shape in the SAME vocabulary constants the `-h` row advertises
    (`OutputLanguageValueQualifier/Shape/ExampleTags/NotAValueClaim`), the
    pinned policy (per-invocation fatal, settings warn+continue), and a
    copy-pasteable remedy that also says the knob is optional.
- `cmd/agent/main.go` §2b — one call, above `OutputLanguageReport()`, with the
  policy stated at the call site: it must precede the chain because `-h`,
  `--help-env`, `config show`, `chat`, `version` and a task run all return from
  `main()` right there.
- `internal/config/output_language_perinvocation_test.go` — the unit half: the
  argv scan (9 shapes) and the exit-code CONTRACT (non-zero, not 1, not the
  guard's class). The contract test exists because the binary test's
  `code == constant` assertion is self-referential: constant 0 would satisfy it.
- `cmd/agent/cli_output_language_validation_test.go` — the executed-binary half,
  extended with the exit code and a **did-not-proceed** observation (the
  `version` banner must be ABSENT), plus new cases for the missing value, the
  `--flag=` spelling, and the settings channel *running the verb* at exit 0.

## Red proof

`GOPI_TEST_BIN=<base-commit binary> go test ./cmd/agent/ -run TestOutputLanguage`
is **RED** — 10 subtests fail with `exited 0, want 2`, every one of them an
explicit per-invocation case (`env refused / {help, help-env, config show,
version, settings adopted}`, `flag refused / {help, version}`, the missing and
empty value, `env refused, settings adopted`). Failing cases include the two
commands from the briefing verbatim.

The base binary was built at commit `6936c787` **before** any edit, and both
measured commands were re-run on it: `flag exit=0`, `env exit=0` — the same
commands now exit 2.

## Pitfalls worth remembering

- **A text assertion cannot see a wrong exit status.** The predecessor fix
  asserted the refusal prose and was green on a binary that exited 0 and ran the
  verb. Any refusal whose text exists must also be asserted in the channel a
  caller actually reads — the status.
- **`code == SomeConstant` is self-referential.** Pair every exit-code assertion
  on the binary with a unit assertion on the constant's own contract, or a
  future edit to 0 makes the binary test assert the defect.
- **A knob's channels can legitimately have different error policies** — decide
  which kind of declaration each channel is (this run vs this machine) rather
  than applying one policy to all three.
- The settings fixture and env in `testutil.MockEnv` mean a case that sets no
  knob really has none; a case that writes no settings file gets the honest
  "nobody asked".
