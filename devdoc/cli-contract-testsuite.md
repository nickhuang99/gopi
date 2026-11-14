<!-- doc-class: historical -->
# CLI Command-Surface Contract Testsuite — devdoc

Epic: brief-cli-contract-testsuite. Branch: `feat/cli-contract-testsuite`.
File: `cmd/agent/cli_contracts_test.go`.

## What this guards

The 2026-09-07 routing bug: `gopi --settings X task submit "multi word summary"`
reported `unexpected positional argument "is"` because a subcommand refactor
silently changed argument routing. Root cause was NOT a parser bug — it was a
**subcommand refactor that changed which code path argv falls into, with no
test to catch it**. This suite is the persistent regression net for that
failure class: **any future refactor that silently re-routes a subcommand
turns a test red**.

## The command-surface inventory

The authoritative top-level dispatch chain lives in `cmd/agent/main.go` (one `if ... ||
isXxxSubcommand(dispatchArgs)` line, 22 predicates). The suite asserts a
hand-maintained catalog (`cliContractInventory`, 26 entries incl. top-level
`-h`) against that chain in **both directions**:

- **main.go → tests**: every `isXxx` predicate in the chain must have a
  contract case (`TestCLIContractInventoryMatchesDispatch` parses the actual
  main.go dispatch line and compares predicate-by-predicate with
  `cliContractDispatchPredicates`). A refactor that ADDS a subcommand without
  a contract case fails here.
- **tests → main.go**: every catalog entry's first token must map to a
  dispatcher that main.go still wires. A refactor that REMOVES/renames a
  subcommand while a stale case lingers also fails.

Coverage (26 contract cases over 22 dispatchers): completion, version, worker,
memory (group + `memory search` + `memory sync`), web_search/web_fetch
(legacy aliases), vision_analyze, text2img, code-search, log, stats, raw,
resume, checkpoint, proxy, task, chat, web (group + `web search` + `web
fetch`), brew, memsync, shot.

## Test method (per-subcommand contract)

Every case runs the **real built binary** (`go build` once per test binary,
reused via the process-level `gopiBinOnce` cache) against an **isolated
GOPI_HOME** (`t.TempDir()`) with **ambient env scrubbed** (`GOPI_*`,
`ANTHROPIC_*`, `DEEPSEEK_*` dropped — a dev shell or task-run child exports
`GOPI_SETTINGS`/`ANTHROPIC_MODEL`/`GOPI_TASK_WRAPPER_ID` which would redirect
config resolution). Four assertions per case:

1. **exit 0** — a `-h` usage query is success (the P0-1 zero-side-effect
   contract).
2. **output fingerprint** — stdout carries THAT subcommand's usage head
   (proves its `-h` handler ran, not the top-level usage, not a fallthrough).
3. **no "unexpected positional" / no panic** — the shredding bug's signature.
4. **no session artifact** in the sandbox GOPI_HOME — the strongest
   no-agent-loop signature: a correctly-routed CLI handler never opens a
   session JSONL. A fallthrough to the agent loop opens one (and burns an LLM
   session — the M4.13 lesson).

The **short timeout** (`cliContractTimeout = 15s`, exec killed on expiry) is
the agent-loop detector: a correct handler returns in milliseconds; a route
that falls through to the LLM handshake hangs and the test fails RED — the
right failure signal.

## The pinned repro case

`TestCLIContractSettingsConfigFlagRoutesToTaskHandler` runs the exact bug
argv in two settings-file variants (missing path + real sandbox file):
`gopi --settings <path> task submit "multi word summary contract test"
--payload payload-body`. It asserts the output contains **"Task queued"** (=
reached the task handler), never "unexpected positional", and leaves no
session artifact.

## Mutation proof (the net actually catches the bug)

The fix (`b9ea37a`) made main.go dispatch on `config.BootstrapEnvFromArgs(args)`
(the config-stripped remainder) instead of raw `args`. To prove the canary
bites, the mutation check reverted that one line to the pre-fix state
(`config.BootstrapEnvFromArgs(args)` with no assignment, chain on `args`):

```
mutation build OK
--- FAIL: TestCLIContractSettingsConfigFlagRoutesToTaskHandler (0.01s)
    missing-settings-path: exit error: exit status 1   ← RED (bug reproduced)
    real-settings-path:    exit error: exit status 1
```

Restoring the fix → the same test passes GREEN. A future refactor that
reintroduces the misrouting fails on the first `go test ./cmd/agent/`.

## Negative contracts

- `task submit` with no summary → fast exit-1 usage error ("missing required
  parameter"), never a hang / never silent success.
- `task frobnicate` / `web bogus` (unknown verbs) → fast exit-1 usage error
  naming the verb — the pre-refactor failure mode was unknown argv falling
  through to the agent loop.

## Verification

- `go test -run TestCLIContract .` (cmd/agent): all contract tests PASS (~26
  subtests, seconds).
- Mutation RED / restore GREEN: proven above.
- Full `go test ./cmd/agent/`: green, no regression (see task report).
