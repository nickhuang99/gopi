<!-- doc-class: historical -->
# The verb guard's prompt channel: what is IN FORCE, not what the environment says

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a0de86-309c-76b2-8911-12312a7dde5f` (2026-09-26), branch
`task/01a0de86309c76b2891112312a7dde5f`.

## The defect

`config.PositionalCommandVerb` refuses to start a run when a leftover positional
names a dispatched top-level verb (`gopi --timeout 3 version`), unless "an
explicit prompt" is present — and it answered that question with
`envPromptSet()` (`os.Getenv(config.PromptEnvVar) != ""`), i.e. *does the
environment mention a prompt*.

`GOPI_PROMPT` survives the process that set it. The documented env-name-as-flag
equivalence makes `--gopi-prompt 1` the same invocation as `--prompt 1`
(`internal/config/flags.go` `FlagOverrides` + the catalog's `CLIFlag` column),
and `ApplyEnv(overrides, true)` publishes the merged config into the process
environment — including `GOPI_PROMPT`. So a probe spelling

```
gopi --gopi-prompt 1 version
```

started a REAL run (prompt `"1"`, the leftover `version` swallowed), minted a
session directory and an `[auto] 1` ledger row — and left `GOPI_PROMPT=1`
behind. Every LATER `gopi --timeout 3 version` then found the guard silent for
the same reason, with no prompt of its own anywhere in its argv: the leak was
self-propagating for the whole lifetime of the environment it landed in.

Measured on the operator ledger: four `[auto] 1` rows
(`#01a0de78/80/82/86`, 2026-09-27 00:06–00:23), one still `in_progress` and
`[died 36m58s]` — a worker slot consumed by a stray probe.

## The fix

**Provenance, not value.** The two situations are the *same string* and opposite
answers, so the value alone can never decide:

| shape | prompt | may a leftover verb positional be swallowed? |
|---|---|---|
| `gopi --prompt 1 version` | `1`, from the FLAG layer | yes — the word is an input FILE |
| settings.json `"prompt": "1"` | `1`, from the SETTINGS layer | yes — same reason |
| `GOPI_PROMPT=1 gopi --timeout 3 version` | `1`, from the ENV layer | **no** — an inherited variable says "a prompt applied to some invocation", never "to THIS one" |

* `internal/config/cli_verb_guard.go` gained `LoadMerged(args)`: the read-only
  twin of `cmd/agent/main.go`'s resolution (flags > env > settings > defaults),
  landing the prompt — and WHERE it came from (`promptSource`: flag / settings /
  env) — on a `*Config`, together with the flag-aware positionals and the flag
  they follow (`FlagOverrides`' output, which the settings layer never carries).
* `promptInForce(cfg)` is the single disarmer predicate: a prompt governs the
  invocation when the command line or the settings file stated one. An
  environment-supplied prompt carries the value (the run path's `readInput`
  still consumes `GOPI_PROMPT`) but not the authority to disarm the refusal.
* `cmd/agent/main.go` calls `LoadMerged` and hands THAT config to both
  `PositionalCommandVerb` and `PositionalCommandVerbRefusal`, **before**
  `ApplyEnv` publishes anything — the one moment the question is still
  answerable. Ask after the publication and the config's own prompt is
  indistinguishable from an inherited one.
* `CONFIG.md`'s `prompt` row says the same thing in one sentence.

Why the guard is a *reason*, not an escalation: the operator who really means
`version` as prompt text has the spelling the refusal message already hands them
(`--prompt version`) — explicit, and independent of what some earlier process
exported. Nothing was weakened and no bypass flag was added.

## The third direction, measured: does `--gopi-<env>` publish into the env?

**Yes, and that is by design — recorded decision, no change.**

```
$ GOPI_PROMPT=7 gopi sandbox run --label guardfix --task-id 01a0de86… -- bash -c 'env | grep GOPI_PROMPT'
GOPI_PROMPT=7
```

A gopi-spawned child inherits exactly what the launcher exported. Publishing is
`ApplyEnv`'s whole job ("so the existing per-file `os.Getenv` parses in
internal/agent and internal/tools pick them up unchanged"), it is how the
task-run wrapper and `gopi worker` children resolve the same config as their
parent, and the guard no longer depends on the published copy. Removing the
publication would be a change to the process-tree contract with no consumer
asking for it; the decision is recorded here instead.

What the probe left behind is visible too: after `--gopi-prompt 1 version` the
*launcher's* environment is clean (`GOPI_PROMPT: NOT present`) because the
publication happens inside the child, but the child's own children — and any
shell that child spawns — inherit `GOPI_PROMPT=1`.

## Verification (the shape a later reader should re-run)

```
# the pair: identical argv, only the ambient variable differs
env -u GOPI_PROMPT GOPI_HOME=$SB bin/gopi --timeout 3 version --db $BAD   # rc=1, same refusal
env GOPI_PROMPT=1   GOPI_HOME=$SB bin/gopi --timeout 3 version --db $BAD   # rc=1, same refusal
# no run happened: sessions 1→1, ledger rows 0→0, $BAD never created

go test ./cmd/agent/ -run 'TestRunPathRefusesVerbWithAmbientPromptInEnv|TestRunPathStillRunsWithExplicitPromptBesideAFilePositional'
go test ./internal/config/ -run TestPositionalCommandVerb
```

Red proof: restoring the pre-fix predicate (`cfg.Prompt != "" || envPromptSet()`)
turns `TestRunPathRefusesVerbWithAmbientPromptInEnv/GOPI_PROMPT_set_by_a_probe_(the_leak)`
red ("the refusal text is absent"), while the unset leg stays green — the pair
really does isolate the channel.

## Not done

`web/`-side or help-text wording beyond CONFIG.md's row was left alone. The
`[auto] 1` rows already on the operator ledger were not touched: they are
history, and this fix does not retract them.
