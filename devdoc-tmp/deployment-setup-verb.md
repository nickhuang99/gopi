<!-- doc-class: normative -->
# Deployment: `gopi setup` — the One-Run Run-Ready Path

Epic: the `~/.gopi/sessions/threadsig.sh` question — "is it runtime support, or
is it a deployment script?" (task `01a0cbac`).

## Verdict on the script

**Not runtime support. Ad-hoc debug ephemera.** The evidence:

- `grep -rn 'threadsig'` over the whole repo returns exactly **one** hit:
  a row in a **session handoff snapshot's process table**
  (`devdoc/handoff-20260922-0503.md:28`: `| bash threadsig.sh | 1116640 |
  PPID=**1**（3天16h） | 可能存活 |`). No code, no test, no build step references it.
- It was an *observer* for one test family — it polled
  `/proc/<pid>/task/<tid>/status` for `SigPnd`/`SigBlk` on `gopi merger` loops,
  the same job as its five deleted siblings (`poll-loop.sh`, `watch-loops.sh`,
  `stack-dumper.sh`, `sigstate.sh`, `threadsig.txt`).
- It outlived its session (PPID=1, 3d16h, still attached to a stale
  `session-18855.scope`) — the Rule 24 orphan shape, not a deployment unit.

It has been **removed** (the orphan process and all six scripts). Nothing in
gopi needed it, and nothing needs to replace it.

## The gap the question exposed — and the fix

The question's real content: *"if a script were needed at run time, that is a
deployment defect; gopi has a zero-dependency principle — either integrate it,
or make one command set up all the runtime state."*

Measured truth:

| path | who created it |
|---|---|
| `<profile>/memory.db` + schema | gopi itself, on first ledger use (`task list`, `memory …`) |
| `<profile>/sessions/` | gopi itself, on first task session |
| `<profile>/settings.json` | **the operator, by hand** |

So a fresh host **self-provisions** — except for one thing an operator must
hand-write, and no documented single command stated the run-ready contract. That
is a real deployment gap, and a shell script could not fix it (a script would
itself violate the zero-dependency rule).

`gopi setup` closes it:

```
$ gopi setup
gopi profile: /home/user/.gopi
  profile data root  created      /home/user/.gopi
  sessions           created      /home/user/.gopi/sessions
  settings           created      /home/user/.gopi/settings.json
  task ledger        initialized  /home/user/.gopi/memory.db
4 path(s) provisioned — the profile is run-ready
```

- **Idempotent**: a second run prints `present` for every path and
  `nothing to do`, exit 0.
- **Never destructive**: an existing `settings.json` is never rewritten; an
  existing `memory.db` is never touched (the schema batch is idempotent).
- **Private by default**: a new profile/sessions tree is `0700`, a new
  `settings.json` is `0600` (it may hold an `api_key`).
- **Honest about overrides**: the ledger path is resolved through
  `config.LedgerPath` (the ONE reader of `GOPI_MEMORY_DB` / `GOPI_HIPPO_DB`,
  Rule 2). When the operator points the ledger elsewhere, `gopi setup` reports
  that path and does **not** materialize a file there.
- **Zero dependency**: stdout only — no external binary, no script.

### What it deliberately does NOT provision

- `<profile>/raw` — the raw-log directory is configurable
  (`config.RawLogDir`) and new sessions co-locate their raw log with the session
  JSONL, so pre-creating a hardcoded `raw/` would be wrong for some
  configurations. `sessions/` and the ledger are what a first run requires.
- an API key or any model configuration — those are operator policy, not
  provisioning.

## Files

- `cmd/agent/cli_setup.go` — the verb (`CliVerbSetup` lives in
  `cmd/agent/cli_verbs.go`, the top-level verb SSOT).
- `internal/tools/ledger_ensure.go` — `EnsureLedger`, the provisioning face of
  the existing ledger bootstrap (`openStoreDB`).
- `internal/config/help.go` — the top-level command index entry, so `gopi setup`
  is DISCOVERABLE from `gopi -h`. `TestTopLevelUsageAdvertisesEveryDispatchedVerb`
  derives the required set from main.go's dispatch chain and failed the build
  until this landed — the guard the 2026-09-13 help-format epic exists for.
- `cmd/agent/cli_setup_test.go` — fresh-profile and idempotence regression
  tests, plus "an existing settings.json is untouched".

## Verification

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     # OK
$ GOPI_HOME=<fresh> ./bin/gopi setup                  # 4 paths provisioned, exit 0
$ GOPI_HOME=<same>  ./bin/gopi setup                  # nothing to do, exit 0
$ GOPI_HOME=<fresh> ./bin/gopi task list              # usable afterwards
```
