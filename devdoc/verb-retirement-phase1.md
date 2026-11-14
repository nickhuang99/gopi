<!-- doc-class: historical -->
# Verb retirement, phase 1 — `log` / `stats` / `raw` / `resume` leave the top level

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Truth class: **normative** — a standing claim about this branch's surface.

Epic: `01a10dbb-ce55-73b9-bc56-06891cf30908`. Scope of THIS phase: exactly those
four words. `archive`, `checkpoint`, `web`, `ui`, `cli` are later phases.

## What changed

The four words are no longer top-level `gopi` verbs. They are **refused by
name**, with a pointer to the live spelling `gopi session <verb>`, which is
where all four have actually lived since the session family absorbed them.

```console
$ gopi log
gopi: "log" is not a gopi command — this one is spelled `gopi session log` on
the command line (`gopi session log -h` for its help). To send the word as a
prompt instead, say so explicitly: `--prompt log` (or the GOPI_PROMPT env var).
$ echo $?
1
```

Same shape for `stats`, `raw`, `resume`. The live spellings answer:

```console
$ gopi session log -h    # exit 0, the log page
$ gopi session resume -h # exit 0, the resume page
```

## The safety property (why this is a removal AND an addition)

Removing the dispatch entry alone would have sent `gopi log` to the
**bare-prompt path** — a whole agent run bought with the word "log". That is
the exact accident this codebase already has a measured record of: three
`gopi buildinfo` invocations on 2026-09-30, three `[auto]` ledger tombstones,
zero output. So the four words were added to `verbAliases`
(`cmd/agent/cli_verb_aliases.go`) in the same change that removed them from the
dispatch chain. Refusal happens at the **command layer**: exit 1, a pointer,
and no session directory, no ledger row.

## The design decision worth recording: a two-level canonical

`verbAliases` had only ever carried a ONE-word canonical (`buildinfo` →
`version`). These four rows point at a **two-level command line**
(`log` → `session log`).

The renderer needed **no change**: it already joins `gopiProgramWord` and the
row's canonical string, so a canonical that is itself a whole command line
(minus the program word) renders correctly by construction — which is also the
convention the retired web twins already relied on (`refusalAddress()` returns
`gopi tool web search`).

The canonical is **derived**, not typed:

```go
func verbAliasCanonicalSession(sub string) string {
	return CliVerbSession + " " + sub
}
```

so a rename of either half cannot leave a refusal pointing at a spelling that
does not resolve (Rule 13). The four rows use the `cliVerbs` constants
(`CliVerbLog` …) rather than literals.

## The gate cascade (the actual work of this phase)

Removing four dispatch predicates turned out to touch a wide family of gates,
each of which asserts that its own vocabulary agrees with the dispatch chain.
They are listed here because the next retirement phase will meet the same ones:

| gate | what it needed |
|---|---|
| `cli_verb_aliases_test.go` | `verbAliasAddressDispatches` — a two-level canonical must be resolved against the SESSION family's vocabulary, not the top-level word set (which would answer "not dispatched" for a command that runs) |
| `cli_verb_guard_test.go` | `raw`/`stats` leave the "dispatched" set — they are refused synonyms now |
| `cli_contracts_test.go` | four contract rows drive `session <verb> -h`; the dispatch mirror drops the four predicates `main.go` no longer calls |
| `cli_completion.go` + `cli_completion_test.go` | `log`/`resume` leave the candidate set (a completion offering a word the binary refuses is a lie) |
| `cli_session_help_surface_basic_test.go` (g4) | scanner regex, verb extraction, the pinned 86-row list, four real-binary probes |
| `scripts/g4-session-help-surface.sh` | drives `session <verb> -h` now |
| `devdoc/gopi-session-help-surface-inventory.md` | regenerated (86 rows) |
| `cli_verb_census_test.go` + `devdoc/gopi-verb-census.md` | four rows removed; prose figures re-stated |
| `internal/config/help.go` | four "Alias of" lines removed |
| `cli_log_test.go`, `cli_raw_selector_test.go`, `cli_log_roundgrammar_test.go` | pinned pointer literals re-derived |
| `cli_session_test.go`, `cli_session_selector_test.go` | page-title literals derived from the usage constants; the selector sandbox prefixes the group word |

**Prefer the derived form.** Every gate that *could* stop mirroring its literal
now does (`cliUsageTitle(logUsage)`, `gopiProgramWord + " " + CliVerbSession + …`).
That is why the next spelling move will cost less than this one.

## A bug this phase found: `resume` was simultaneously retired and reachable

`isCheckpointSubcommand` still listed `CliVerbResume`, so `gopi resume -h`
answered the checkpoint page and exited 0 while `gopi resume extra` was refused
— the word was "retired" and still dispatched. The alias table's own gate caught
it in both directions ("points at `session log`, which this binary does NOT
dispatch" / "IS dispatched — it must not be in the refusal vocabulary").
`CliVerbResume` is now absent from that switch; `gopi session resume` is owned
by `isSessionSubcommand`, which forwards to `isResumeSubcommand`.

## The 88-column reflow

The `session ` prefix (8 columns) pushed 18 synopsis lines past the wrap budget
(`config.HelpWrapCol` = 88). They were reflowed by moving each overflowing
line's DESCRIPTION to a continuation line at that page's own description
column. The **invocation text is untouched**, which is what keeps the g4
scanner's row derivation stable (it cuts at the first double space).

One line cannot be reflowed: a full absolute raw-log example path in
`resumeUsage` (96 columns). It is DATA a reader copies — abbreviating the uuid
or the run id would stop it naming the file it is an example of — so it carries
the gate's reviewed `help-width-ok:` escape, with the reason stated at the
constant. No other line on the four pages exceeds the budget.

## A bug in my own gate edit, recorded because it is instructive

Widening the g4 invocation regex as `(?:session )?(log|stats|raw|checkpoint|resume)`
let the session group apply to `checkpoint` too, and the checkpoint page's
cross-reference line (`gopi session checkpoint list|export …`) then became a row
the help does not advertise. The same over-broad alternation was written into
the shell script's awk pattern. Both are now grouped so the group applies to the
four retired verbs ONLY:

```
^  gopi (?:(?:session (log|stats|raw|resume))|(?:checkpoint)) 
```

Both derivations — Go and shell — now agree exactly (86 rows, zero difference).

## The part the first round missed: the retirement reaches its CALLERS

Removing the dispatch entry is only half the move. Code that **spawns** or
**advertises** a command kept composing the retired spelling, and the
package-wide run (`go test ./cmd/agent/`) came back with 16 failures the
branch-scoped run had not seen. They were two shapes:

**① a spawned child that is now refused.** `gopi worker --resume`, the merger's
detached dead-holder recovery and the retry ladder's recorded action each built
`gopi resume <target> --task <id>` by hand. Every such child now exits 1 with the
refusal instead of resuming — and the same argv was **stamped into `run_cmd`**,
the field whose whole purpose is to be pasted back into a shell.

Fixed at the source (Rule 13/14): `runcmd.ResumeArgv(target, taskID)` is the ONE
renderer of a resume command line, used by all three spawn sites and by
`cli_resume.go`'s run-provenance stamp. `VerbSession` joins `VerbResume` there
for the same reason `VerbTask` exists — two packages compose the same command.
`TestResumeArgvRendersTheLiveSpelling` asserts the RENDERED string, which is what
a reader pastes; a test on the argv slice would not have caught the stamp.

**② a pointer that leads to a second refusal.** `chat_info`'s copy block offered
`gopi log <id>`; the checkpoint refusal offered `gopi resume <log>` *as a live
spelling* beside `gopi session resume <log>`. A removal statement is only worth
printing while the command it names still runs (Rule 31), so both now name the
live spelling only — the checkpoint constant's `%s` count went 3 → 2, and
`go vet` caught the half-edited call the moment the constant changed.

**Two gates asserted the old world.** A census row naming a word `main.go` no
longer dispatches was "an invented verb"; a `cliVerbs` member with no
`dispatcherOfToken` entry was a "stale constant". Both are legitimate for a word
the binary **refuses by name** — the word is still part of its surface, and the
row is the record of the retirement decision. Both exemptions are read from
`verbAliasCanonical()` (the READ side of the table
`verbAliasRefusalReason()` already walked) rather than hand-listed, so neither
can quietly cover a verb that still dispatches.

**The lesson for the next phase.** A retirement's blast radius is not the
dispatch chain; it is every place that COMPOSES or PRINTS a command line. Grep
for the spelled command, not for the constant: the three broken call sites each
type the word into an argv literal.

## The same blast radius inside the GATES (closing round)

The full `cmd/agent` suite — not the targeted `-run` set the earlier rounds
drove — caught two more call sites, and both are the SAME defect one layer out:
a gate whose argv is composed from a word that used to be a command.

- `cli_verb_aliases_test.go` (`TestVerbAliasCanonicalStillDispatches`) ran
  `runCLIContractProbeEnv(…, a.canonical, "-h")`, i.e. ONE argv token. When a
  row's canonical was one word (`version`) that was right; for the four
  two-level rows it passed the string `"session log"` as argv[0] — a word that
  is no command, so the probe reached the RUN path and printed the top-level
  help. The fix is the row's own meaning: the address is a COMMAND LINE, so
  `strings.Fields(a.canonical)` is the argv and the `-h` is appended.
- `sandbox_profile_resolution_test.go` drove `gopi log --last` — the retired
  spelling — to prove the sandbox's created profile is the one its reader
  resolves. It now drives `gopi session log --last`, with the sub-verb taken
  from its own constant (`CliVerbLog`), so the probe cannot drift onto a
  spelling that no longer dispatches.

**What this says about the phase's own gate.** A targeted `-run` set is chosen
by the author and therefore contains what the author already thought of; the
blast radius of a verb retirement is exactly the set of surfaces nobody thought
of. The full-package run is what found these, and it is the reason the closing
verification of a retirement cannot be the targeted set alone.

The third failure in that same full run —
`TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator` — is NOT this
phase's: it fails intermittently under machine load (`the console's renewal
notice … was announced 2 time(s) … want exactly 1`), and it passes alone and in
`-count=2` on an idle machine against this branch. It is reported as an observed
flake, not repaired here (a fix would belong to the chat-bound machinery, a
different surface than this epic).

## Verification

- Build: `CGO_ENABLED=0 go build ./...` — OK; `go vet ./cmd/agent/` — OK
- Tests: `go test -run 'TestResume|TestWorkerResume' ./cmd/agent/` — `ok 17.131s`
  (9 failures before the renderer fix)
- Tests: `go test ./internal/runcmd/` — `ok`
- Tests: `go test -run 'TestG4|TestCLIContract|TestVerbAlias|TestAliases|TestVerbGuard|TestCompletion|TestTopLevel|TestSession|TestPositional|TestLogMoved|TestHelp|TestRound' ./cmd/agent/` — ok
- Tests: `go test -run TestVerbCensusMatchesTheDispatch ./cmd/agent/` — ok
  (log line: `census: 40 dispatched verbs, 40 rows, 32 with literal argv drives`)
- Ran: `./bin/gopi log|stats|raw|resume` — exit 1, each naming `gopi session <verb>`
- Ran: `./bin/gopi session log --last` (real `GOPI_HOME`) — exit 0, prints the block
- Ran: `./bin/gopi session resume -h` — exit 0
- Ran: `./bin/gopi session checkpoint resume /tmp/x` — exit 1, prints
  ``gopi checkpoint resume: removed — one continuation, not two: use `gopi session resume <log>` ``
- Ran: the six interceptors (`buildinfo`, `code_search`, `screen_capture`,
  `ui_state`, `vision_analyze`, `voice_input`) — behaviour unchanged
- Ran (negative control): `gopi frobnicate9` still mints a session (reaches the run path)
- Ran: `./bin/gopi -h | grep -cE '^  (log|stats|raw|resume) '` — `0`
- Ran: `scripts/g4-session-help-surface.sh ./bin/gopi` — 86 rows, agrees with the Go derivation
- Tests (closing round, FULL package): `go test ./cmd/agent/` — 3 failures found and
  triaged: `TestVerbAliasCanonicalStillDispatches` and
  `TestSandboxCreateProfileRootIsWhatReadersResolve` were this phase's (retired
  spelling in the probe argv) and are FIXED; the third
  (`TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator`) is a load flake
  outside this epic's surface — it passes alone and at `-count=2` on this branch.
- Ran: `go test ./cmd/agent/ -run 'TestVerbAlias|TestAliases|TestVerbGuard|TestVerbCensus|TestSandboxCreateProfileRoot|TestSandboxProfile|TestCompletion'` — `ok 28.110s` after both fixes

## Not done in this phase (honest)

- `archive`, `checkpoint`, `web`, `ui`, `cli` — later phases, untouched.
- The four words are still spelled in `internal/config.PositionalVerbs`, which is
  REQUIRED (the guard refuses a leftover positional, e.g. `gopi --timeout 3 log`);
  that is not a leftover of the old surface.
- No new devdoc row was added to `devdoc/DOC-INDEX.tsv` by hand — `gopi doccheck`
  is the writer (Rule 36).
