<!-- doc-class: historical -->
# devdoc: the peak-hour guard's exemption is TOKEN COST, not a verb list

Epic: `peak-guard-token-free-verbs` (task summary: "basic version should be
allowed"). Branch `fix/peak-guard-token-free-verbs`.

## The report

```
$ gopi version
[gopi] peak-hour cost guard: refusing to start — `gopi version` would run inside
DeepSeek's peak window 09:00-12:00 … every token is billed at 2x until …
```

`gopi version` prints a git revision. It resolves no config, opens no session,
calls no model. It cannot cost 2x of anything — there is nothing to double.
Refusing it is not a conservative default; it is a false positive of the
guard's *criterion*.

## Root cause (5-why)

1. **Symptom**: `gopi version` exits 4 inside 09:00-12:00.
2. **Mechanism**: `applyPeakGuard` (cmd/agent/peak_guard.go) runs at the top of
   `main()` for EVERY argv, exempting only `isUsageQuery` — a *bare help token*
   (`-h` / `--help` / `help` / `--help-env` / `help-env`).
3. **Why that exemption set**: the original epic's requirement was "no verb
   escapes the guard" (worker, merger, prompt, all modes). The exemption was
   written as narrowly as possible — one shape, the help query — because every
   widened exemption is a possible escape hole.
4. **Why that made `version` a refusal**: the guard's placement (before the
   subcommand chain) is *argv-blind* — it cannot see that the verb it is about
   to dispatch is one that never spends. It classified by **argv shape**, so
   any verb whose shape is not a help token was treated as a spending run.
5. **Root cause**: **the guard's criterion was "a gopi process is starting",
   not "tokens are about to be spent".** The guard's name and its message both
   say TOKEN COST ("every token is billed at 2x"), but its predicate was
   *process start*. A zero-token process start was therefore refused on
   cost grounds — a criterion/mechanism mismatch, exactly the operator's
   reading: "this is not a feature issue, it is our mindset about what peak
   hour really means."

A NOTE ON THE PRIOR TASK (01a098a1): a previous session probed this exact case
(`gopi --peak-hours 20-21 --version` → exit **4**) and recorded it as correct
in its report. The probe was right; the *expectation* was wrong. This epic is
that expectation being corrected by the operator — evidence that "the test
passes" is not the same claim as "the behaviour should be the behaviour".

## The fix: a criterion change, not a verb blacklist

`isUsageQuery` (one shape) became `costFreeInvocation` (a class):

```go
func costFreeInvocation(args []string) bool {
    if isUsageQuery(args) || isVersionQuery(args) {
        return true
    }
    if len(args) < minVerbArgvLen || !isTopLevelVerb(args[0]) {
        return false
    }
    if isHelpToken(args[1]) {
        return true
    }
    return len(args) > 2 && isHelpToken(args[2])
}
```

Three shapes qualify, and each is justified by a **provable** property, not by
a guess about a file's imports:

1. **Bare usage query** (`gopi -h`) — unchanged.
2. **`<verb> -h|--help`** — NEW. Every subcommand's `-h` is answered by a
   zero-side-effect scan that runs *before* the work: `for _, a := range args {
   if a == HelpFlag … os.Exit(0) }` (isWorkerSubcommand, isMergerSubcommand,
   cli_log.go, cli_memory.go, cli_code_search.go, `runTasksSubcommand` …).
   Refusing it hid, from inside a peak window, the text documenting how to
   leave the guard on. Measured: all 24 top-level verbs answer `-h` in 6-28 ms,
   exit 0, no network.
3. **The version query** (`gopi version` / `--version` / `-v`) — NEW, spelled
   from the new constants `VersionFlagLong` / `VersionFlagShort` that
   `isVersionSubcommand` now also reads (Rule 13: one definition, both sites).

**Why a help word is only a query on a COMMAND line**: a prompt is also argv.
`gopi "run this prompt that says --help inside it"` must NOT be exempted, and
neither must `gopi "summarize --help"`. The shape therefore requires the FIRST
token to be a known top-level verb (`isTopLevelVerb` against the new
`topLevelVerbWords` list in cli_verbs.go). The list failing to know a word
fails CLOSED — the start is refused — so it is not a security surface; the
drift test `TestTopLevelVerbWordsCoverTheDispatchTokens` closes it both ways
against `dispatcherOfToken` anyway.

**Why the guard cannot be escaped by this widening**: the predicate consumes
only argv. Any invocation with a verb is still refused unless a help token
rides along, and a help token makes the process print usage and exit before any
verb body runs. A future verb that gains an LLM call is *still* refused, because
nothing was exempted by "this file imports no LLM".

### Deliberately NOT exempt (recorded so a future agent does not "finish" this)

Local report verbs that also spend nothing — `log`, `stats`, `raw`,
`memory search`, `code-search`, `shot`, `screen_capture`, `act`, `scratch-gc`,
`completion`, `memsync`. Exempting them needs a per-verb token-spend
classification, and such a classification rots silently the moment one of them
gains a model call (a `gopi memory search` embedding index is a plausible
future). The operator named the version query; the exemption stays the size of
the *provable* set. If a future operator asks for `gopi stats` at 09:30, the
honest answer is: that is a new criterion decision, with the same evidence bar
(prove no model path), not an obvious widening.

Also still guarded, on purpose: `gopi --peak-hours 20-21 --help-env` — the flag
makes it a *configured* start (the guard must consume those flags), so it is not
a bare query. The env spelling `GOPI_PEAK_HOURS=… gopi --help-env` stays
readable, which is why the hatch is documented in both channels.

## Verification

**Red proof** (the guard's own failure mode reproduced on the pre-fix
predicate): the exemption was disabled in a scratch copy and the suite re-run —

```
--- FAIL: TestPeakGuardKeepsCostFreeVerbsAvailable
    gopi version inside a peak window: exit status 4 — a zero-token invocation must not be refused
--- output ---
    [gopi] peak-hour cost guard: refusing to start — `gopi version` would run inside
    DeepSeek's peak window 00:00-24:00
```

i.e. the test fails with EXACTLY the operator's reported symptom when the fix is
absent. Restored → green.

**Live probe matrix** on the built binary (`CGO_ENABLED=0 go build -o bin/gopi
./cmd/agent/`), `GOPI_PEAK_HOURS=18:00-24:00` armed (18:23 Beijing at probe
time, so the window contained "now"):

| invocation | exit | output |
|---|---|---|
| `gopi version` | 0 | `dev` |
| `gopi --version` | 0 | `dev` |
| `gopi -v` | 0 | `dev` |
| `gopi -h` | 0 | top-level usage |
| `gopi worker -h` | 0 | worker usage |
| `gopi task run -h` | 0 | task usage |
| `gopi web search -h` | 0 | web-search usage |
| `gopi worker` | **4** | refusal |
| `gopi merger` | **4** | refusal |
| `gopi task run` | **4** | refusal |
| `gopi task submit x` | **4** | refusal |
| `gopi brew` | **4** | refusal |
| `gopi memory search foo` | **4** | refusal |
| `gopi code-search foo` | **4** | refusal |
| `gopi stats` | **4** | refusal |
| `gopi "summarize this repo"` | **4** | refusal |
| `gopi "run this prompt that says --help inside it"` | **4** | refusal |
| `gopi --peak-hours 18:00-24:00 --help-env` | **4** | refusal |

All 24 top-level verbs driven with `-h` under the armed window: exit 0, 6-28 ms,
no network. (Method note: each probe used a fresh `GOPI_HOME`, and
`GOPI_IGNORE_PEAK_HOURS` was explicitly set to `0`, because this repo's own
`cmd/agent` suite pins `GOPI_IGNORE_PEAK_HOURS=1` at TestMain level — an
inherited `1` would have silently disarmed every probe and made the matrix a
lie.)

> **RETIRED (history, not a recipe):** `GOPI_IGNORE_PEAK_HOURS` no longer exists. The guard
> was removed outright by `fix/remove-peak-guard` (commit `bb0c4c1a`,
> devdoc/peak-guard-removal.md), the env reader with it, and the `cmd/agent` TestMain pin
> described above is gone too — the variable is read by nothing, so it neither disarms nor
> arms anything. Today a probe needs no peak-hour variable at all; the method note above is
> kept only to explain why the 2026-09-14 matrix had to control for one.

**Tests** (`cmd/agent/peak_guard_test.go`):

- `TestCostFreeInvocationExemptsVersionAndHelp` — the epic's acceptance at the
  predicate level (version × 3 spellings, bare queries, `<verb> -h`).
- `TestCostFreeInvocationRefusesSpendingModes` — the OTHER direction, 20 argv:
  prompt path, worker/merger (+`--daemon`), task run/submit/merge, chat, web,
  resume, brew, vision, and the deliberately-not-exempt local verbs.
- `TestCostFreeInvocationAgreesWithTheVersionSpellings` — round-trip against
  `isVersionSubcommand`'s vocabulary + frozen wire spellings.
- `TestTopLevelVerbWordsCoverTheDispatchTokens` — `topLevelVerbWords` vs
  `dispatcherOfToken`, both ways (no stale word, no missing verb).
- `TestPeakGuardKeepsCostFreeVerbsAvailable` — the LIVE probe: real binary,
  armed window, exempt paths exit 0 with output, spending paths exit with
  `config.PeakGuardExitCode` and the refusal text.

## Files

- `cmd/agent/peak_guard.go` — `costFreeInvocation` / `isHelpToken` /
  `isVersionQuery` (+ `minVerbArgvLen`); `isUsageQuery` narrowed to its own
  shape.
- `cmd/agent/cli_verbs.go` — `VersionFlagLong` / `VersionFlagShort` constants;
  `topLevelVerbWords` + `isTopLevelVerb`.
- `cmd/agent/cli_version.go` — reads the new constants (both the dispatch case
  and its `-h` scan).
- `cmd/agent/main.go` — guard call site: `isUsageQuery` → `costFreeInvocation`.
- `cmd/agent/peak_guard_test.go` — the tests above.
- `internal/standards/baseline.go` — Rule-16 permit re-keyed 1035 → 1040 (the
  pure line shift the same file's expanded comment caused; the `%w` site is
  byte-identical, and the baseline's own comment records the reason).
- `devdoc/daemon-context-isolation.md` — the "Exempt:" line, corrected.
