<!-- doc-class: historical -->
# G4 report — the session-inspection + resume help-surface hardening

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: `01a0c2d1-9e10-7ad9-912d-ae0f47618775` (epic brief:
`briefings/brief-help-surface-hardening.md`, group **G4**).
**Branch**: `task/01a0c2d19e107ad9912dae0f47618775` (base `953a92c2` == master).
**Group**: `gopi log`, `gopi stats`, `gopi raw`, `gopi checkpoint list|export`,
`gopi resume` (incl. `--task`) — the session-inspection and resume verbs.

## What the task asked, and what landed

| asked | landed |
|---|---|
| derive the advertised verb/flag surface from the help text itself | `scripts/g4-session-help-surface.sh` (operator-facing, 73 rows) + `g4AdvertisedRows()` in the test package, re-derived from the SAME `logUsage` / `statsUsage` / `rawUsage` / `checkpointUsage` / `resumeUsage` constants the binary prints |
| pin it in a checked-in inventory quoting the derivation command | `devdoc/gopi-session-help-surface-inventory.md` — the re-derivation section quotes the build + script + both gate invocations verbatim, and the 73-row table is compared with the derivation by a test that reads the FILE from disk |
| prove each advertised surface with a REAL invocation (happy + first refusal; `-h` alone never counts) | `cmd/agent/cli_session_help_surface_basic_test.go` → `TestG4EveryAdvertisedSurfaceRuns` drives each row's OWN argv through the real binary against a sandboxed `GOPI_HOME` and the real fixture session; `TestG4UnknownFlagRefusals` pins 14 refusal invocations |
| fix contained one-invocation defects | `cli_checkpoint.go`: an unknown `--flag` was silently swallowed (see below); `cli_log.go`: one synopsis line broke the page's own column rule |
| file non-contained ones as bug tasks | none — every defect found was contained (see `## Not Done` for the honest boundary of that claim) |

## The defects found and fixed

### G4-D1 — `gopi checkpoint list --bogus` exited 0 (silently swallowed)

`runCheckpointSubcommand` runs **before** config resolution in `main()`, so the
config parser's `unknown flag --x` refusal never sees the group's argv: an
unrecognised `--flag` fell into the positional bucket and was dropped. Measured
on the pre-fix binary (`/home/user/workspace/tmp/g4/gopi-prefix`, built at
`953a92c2`):

```
$ gopi-prefix checkpoint list --bogus ; echo exit=$?
log                    reqs last-model  msgs  bytes sha256  last-message
…                       (a full listing)
exit=0
```

A typo was obeyed as the plain listing — the worst shape, because the operator
gets a plausible answer to a question they did not ask. After the fix:

```
$ gopi checkpoint list --bogus ; echo exit=$?
gopi checkpoint: unknown flag --bogus

<the checkpoint page>
exit=1
```

The **tolerance that remains is deliberate and narrow** and is pinned by
`TestG4CheckpointPositionalToleranceIsDeliberate`: a bare POSITIONAL after the
subcommand (`checkpoint list extra`) is still ignored, because the subcommand
decides its own positional arity (`list` takes none, `export` takes exactly one)
and each of those paths already reports what it needs. A `--flag` has no such
reading, which is why only flags are refused. `resume` is untouched: its first
argument is a *target*, so a leading `--x` is already refused by name
(`is neither a raw log path nor an existing session id`).

### G4-D2 — a synopsis line broke the page's own column rule (`cli_log.go`)

Every line of the `gopi log` page puts the description at a fixed column, and
the derivation cuts there. One line did not:

```
  gopi log <session-id> --request --json Full first request body as indented JSON
```

With a single space instead of the column, argv and prose read as one run. This
was fixed **on the page** (a second space) rather than papered over in the
scanner: the derivation deliberately has no special case for it, so the next
such line fails the same way — visibly.

### One false lead, recorded so nobody re-chases it

`gopi checkpoint --dir PATH list` (the flag BEFORE the subcommand) looked broken
in my first probe, refusing with `gopi: unknown flag --dir`. That was **my own
probe bug**: the argv was `./bin/gopi timeout 5 checkpoint …`, so `timeout` was
read as the subcommand and the process went to boot mode. Re-run cleanly,
`checkpoint --dir X list` and `checkpoint list --dir X` BOTH exit 0 and produce
the listing. No defect; the earlier "finding" is retracted here rather than left
in the record.

## The derivation, and why it was re-derived rather than typed

Two independent derivations, which the build forces to agree:

1. **Operator-facing** — `scripts/g4-session-help-surface.sh ./bin/gopi` reads
   the shipped `gopi <verb> -h` pages. Quoted verbatim in the inventory doc.
2. **In-package** — `g4AdvertisedRows()` scans the same usage CONSTANTS
   (`logUsage` …), so the derivation runs with no binary and no shell.

`TestG4HelpSurfaceMatchesInventory` compares (1)/(2)'s result against the pinned
`g4ExpectedRows`, `TestG4HelpSurfaceInventoryTableMatchesHelp` reads the shipped
**table out of the .md file on disk** and compares it with the same derivation,
and `TestG4CoveredSetMatchesCases` closes the other direction (a case driving an
argv the help does not advertise). A help edit with no row, or a table edit that
outruns the help, is a red build — which is exactly the staleness a prose
inventory cannot prevent.

Three details the real pages forced, each recorded in code:

- **The verb comes from the line, not the page.** `checkpointUsage` prints the
  `gopi resume <log> [continuation...]` synopsis (the two commands share one
  page), so attributing by page would file the resume synopsis under
  `checkpoint`.
- **A shell pipe ends the invocation.** `gopi stats <id> --json | jq
  .prompt_tokens` is an example whose second half is prose; the scanner cuts at
  ` | ` in both derivations.
- **Sections travel with the row.** `Usage:` / `Flags can be combined:` /
  `Examples:` are unlike claims, and `Examples:` is where `resume --task` is
  spelled.

## The proof, per advertised surface

73 rows, each driven with the row's own argv through the real binary under a
sandboxed `GOPI_HOME` (fixture: `tests/fixtures/log-stats-raw`, the same real
session + paired raw log the package's log/stats/raw goldens use — no
constructed session). Every row's assertion is the ADVERTISED EFFECT, chosen per
flag by `g4AssertionsFor` (banner vs content vs JSON), and no row is proven by
`-h`.

The one limit, stated precisely rather than papered over (GOPI.md Rule 31): inside
the TEST sandbox the resume rows stop at the agent boundary, and that is **by
construction, not by absence of a credential** — `cliContractEnv`
(`cli_contracts_test.go:471`) deliberately drops every ambient `ANTHROPIC_*` /
`DEEPSEEK_*` / `GOPI_*` var so a probe cannot reach a provider, so the process
fails with `Error creating agent: … ANTHROPIC_AUTH_TOKEN not set`.
`TestG4ProbeToTheAuthBoundaryIsHonest` asserts that boundary and asserts the
failure is NOT an argument or resolution error (which would mean the probe never
reached the entry point); it SKIPS itself when a credential is live, deferring to
the stronger result.

That stronger result was obtained, as Rule 31 requires — an operator invocation
outside the test env, where credentials ARE present, runs the continuation round
to completion:

```
$ gopi resume <fixture-raw-log>          # real argv, real credential, real state
France → Paris → Seine → English Channel (La Manche) → North Atlantic Ocean
exit=0   elapsed=23.9s   Completion: 2,074 tokens   Total: 297,413 tokens

$ ls <sandbox>/.gopi/sessions/b6e5f799-…/   # the round's own artifacts
session.jsonl  (41 records)   b6e5f799-….raw.log   metadata.json
```

So the resume path is proven end to end here; what the sandboxed test file proves
is the weaker, offline-reproducible half (target resolves → checkpoint loads →
argv reaches the entry point), which is the half that can run in CI.

## Files

- `cmd/agent/cli_session_help_surface_basic_test.go` (new, 1163 lines, 10 Test
  funcs) — the derivation, the three gates, the 73-row runner, the refusal
  table, the positional-tolerance pin, the auth-boundary pin.
- `devdoc/gopi-session-help-surface-inventory.md` (new) — the deliverable
  inventory: quoted derivation + the 73-row table + what is deliberately out.
- `devdoc/DOC-INDEX.tsv` — the inventory classified **normative** (a standing
  claim about master, spot-check `path scripts/g4-session-help-surface.sh`).
- `scripts/g4-session-help-surface.sh` (new) — the operator-facing derivation.
- `cmd/agent/cli_checkpoint.go` — G4-D1 (unknown flag refused by name).
- `cmd/agent/cli_log.go` — G4-D2 (the synopsis line's column).

## Not Done (honest)

- **The resume continuation round is not executed inside the test file.** It runs
  in the sandbox behind the auth boundary `cliContractEnv` imposes (and is proven
  end to end OUTSIDE it — see the proof section above). The group's own mocked e2e
  resume coverage is `cli_resume_cleanup_e2e_test.go`; this file deliberately
  drives the real binary and does not mock a provider.
- **The `log --request --json` content assertion is a key-presence check**
  (`"max_tokens"`), not a byte-comparison against a golden. A golden belongs to
  the view's own test (`cli_logstats_fixture_test.go`); repeating it here would
  couple this file to render details it does not own.
- **`gopi log --json`'s header is the JSONL dump itself** (it starts at the
  `session_start` record), so its assertion is `"sessionId"` present rather than
  a banner. Same reasoning as above.
- **SIX pre-existing package failures are NOT from this diff**, and the
  instrument that shows it is a MASTER WORKTREE WITH ITS OWN BUILT BINARY:
  `git worktree add --detach <path> 953a92c2` + `CGO_ENABLED=0 go build -o
  bin/gopi ./cmd/agent/` there, then run the family. Measured on master: all six
  FAIL identically —
  `TestResumeReclaimsTheWorktreeItFinishedIn`,
  `TestWorkerRunRecordsTheMissingRanLineE2E`,
  `TestManualRunRecyclesItsWorktree`,
  `TestTaskRunSeamWritesReportAndShowRendersIt`,
  `TestWorkerServerDeliveryRecyclesThenMergerMerges`,
  `TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow`
  (`FAIL github.com/nick/gopi/cmd/agent 38.223s`). They are the worktree-reclaim
  family, owned by other tasks; this group's verbs do not touch them.

  **The mismeasurement this corrected, recorded so nobody repeats it**: master's
  FIRST reading looked GREEN (`ok 4.974s`) — but that was a test that never ran.
  `TestResumeReclaimsTheWorktreeItFinishedIn` resolves the binary through
  `gopiTestBinPath`, which prefers `<worktree>/bin/gopi` when present; `bin/` is
  gitignored, and my development worktree has one while a fresh master worktree
  does not, so master took the "binary not found … run scripts/e2e_smoke.sh
  first" SKIP path. A SKIP is not evidence of correctness — the same trap the
  package's own `t.Skipf` creates. With a binary present on BOTH sides the
  verdict is identical, which is the only fair A/B.

## Verification

- Build:  `go build ./...` → exit 0; `CGO_ENABLED=0 go build -o bin/gopi
  ./cmd/agent/` → OK, 25,182,733 bytes.
- Tests:  `go test -run 'TestG4' ./cmd/agent/ -count=1` →
  `ok github.com/nick/gopi/cmd/agent 10.306s` (all 10 G4 gates);
  `go test ./internal/standards/ ./internal/docclass/ -count=1` →
  `ok …/internal/standards 44.089s` + `ok …/internal/docclass 5.537s`;
  the full three-package budget run
  (`make test-budget PKGS='./cmd/agent ./internal/standards ./internal/docclass'`)
  → see the transcript in `## Evidence (self-filed)`.
  `gofmt -l` on the three touched `.go` files → no output (the two files
  `gofmt -l cmd/agent/` does report — `cli_config.go`,
  `serve_lock_replacement_test.go` — are PRE-EXISTING on master `953a92c2`,
  verified by running `gofmt -l` on the same files extracted from that commit,
  and are untouched by this diff).
- Ran:    `GOPI_HOME=/home/user/workspace/tmp/tmp.5AJ50D3U8L \
            ./bin/gopi checkpoint list --bogus` (first refusal, post-fix);
          the same argv on the PRE-fix binary
          `/home/user/workspace/tmp/g4/gopi-prefix`;
          `… ./bin/gopi checkpoint list bogus` (the deliberate tolerance);
          `… ./bin/gopi resume
            /home/user/workspace/tmp/tmp.5AJ50D3U8L/.gopi/raw/gopi-raw-20260904-204046.log`
          (the happy path — a real continuation round);
          `bash scripts/g4-session-help-surface.sh ./bin/gopi` (the derivation,
          73 rows emitted, `sort -u` → 73 distinct, no duplicates).
- Result: red→green on G4-D1 — pre-fix `exit=0` with a 2-line listing on stdout
  and empty stderr; post-fix `exit=1`, stderr exactly
  `gopi checkpoint: unknown flag --bogus`. The tolerance case stays `exit=0`.
  Resume: `exit=0`, answer
  `France → Paris → Seine → English Channel (La Manche) → North Atlantic Ocean`,
  `elapsed=23.9s`, `Completion: 2,074 tokens`, `Total: 297,413 tokens`, and a new
  session dir `b6e5f799-…` holding `session.jsonl` (41 records) + its raw log.
  Derivation: 73/73 rows distinct, and cross-checked against the shipped table —
  `awk -F'|' '/^\| *[0-9]+ *\|/{…$5…}' devdoc/gopi-session-help-surface-inventory.md
  | sort` `diff`-ed against the script's own output → IDENTICAL (73 lines).
  Table rows whose `covered` column is `yes` → 73/73.

## Evidence (self-filed)

- `bash scripts/g4-session-help-surface.sh ./bin/gopi` → `deriv-exit=0`,
  `wc -l` = 73, `sort -u | wc -l` = 73; per verb
  `2 checkpoint / 40 log / 10 raw / 6 resume / 15 stats`.
- Pre-fix G4-D1: `exit=0`, `stdout lines: 2`
  (`log  reqs last-model …` + the fixture row), `stderr: ` empty.
  Post-fix: `exit=1`, `stderr: gopi checkpoint: unknown flag --bogus`.
  Tolerance: `exit=0`, `stderr` empty, listing printed.
- G4-D2: pre-fix `log -h | grep -- '--request --json'` line 8 shows a single
  space before `Full first request body…`; post-fix the same line carries the
  column's second space (both shown under `cat -A`).
- Every advertised invocation's own argv was executed by
  `TestG4EveryAdvertisedSurfaceRuns` — 73 subtests, all PASS, each asserted on
  the ADVERTISED effect (never `-h`).
- `awk` over the shipped table: `table rows: 73`, `73 [yes]` for the covered
  column, two `DOC-INDEX.tsv` rows for this group.
