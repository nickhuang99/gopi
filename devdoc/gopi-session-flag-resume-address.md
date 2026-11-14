<!-- doc-class: historical -->
# `gopi --session <id> ["<prompt>"]` — the no-verb resume address

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Status:** shipped (epic resume-surface convergence, step 1 of 3).
**Terrain map:** memory #12557 (the three-step split and every gate this step avoids).

## What this step is, and what it is NOT

The resume pipeline already existed exactly once, as the body of
`runResume` (`cmd/agent/cli_resume.go`) reached by `gopi session resume
<target> [continuation-file]`. Step 1 adds a **second address** for that one
implementation, on the invocation an operator is already typing:

```bash
gopi --session <session-id>                  # continue from the checkpoint
gopi --session <session-id> "run the tests"  # continue WITH an instruction
```

Deliberately **not** in this step:

- `gopi session resume` is still dispatched (its retirement is step 3).
- `internal/runcmd.ResumeArgv` was untouched **in step 1** — the worker's child
  argv still spelled `gopi session resume …`, because touching the spawned shape
  before the address it points at existed would have broken `gopi worker
  --resume`. **Step 2 converged it**; see "Step 2" below.
- No new verb, no new tool, no new dependency (Rule 3/35): the surface is a
  FLAG, and `--session` is a free namespace in `config.FlagOverrides`
  (`flagArity` builds its arity table from the env catalog, which has no
  `session` row; `BootstrapEnvFromArgs` hoists only settings/home/tmpdir/
  output-language/…). A dispatch PREDICATE would have cost two mirror gates
  (`cli_contracts_test.go`'s `cliContractDispatchPredicates` count/order check
  and the verb census); a flag costs none.

## The two halves of the change

### 1. `runResume` → `resumeSession` (one implementation, two addresses)

`runResume` keeps only its ADDRESS contract — the `-h` page, the target
positional, `parseResumeArgs` for `--task` / the continuation FILE — and then
calls:

```go
resumeSession(progName, target, continuation, taskID, prompt string) int
```

The core took every `args[0]` reference it still had and re-keyed it to
`target`, and every diagnostic it emits on its own behalf now carries
`progName` (`"gopi session resume"` / `"gopi --session"`) instead of a hard-coded
verb spelling. **This is why the extraction is not cosmetic:** a face that
printed another face's name would send an operator to a command they did not
type.

The instruction now has **two channels, exactly one set**:

| channel | comes from | semantics |
|---|---|---|
| `prompt` | the no-verb face's argv tail | inline text, used verbatim |
| `continuation` | the verb's first positional | a FILE, read and trimmed — **unchanged** |

The core reads the file only when `prompt` is empty, so the verb face's
behaviour is byte-identical to before. The motivation for a parameter rather
than pushing the prompt into `continuation` is measured, not stylistic:
`parseResumeArgs` treats its positional as a path, so a prompt arriving there
is `os.ReadFile("<prompt>")` → "no such file".

### 2. Where the face is wired, and why not one line higher

```text
main()
  ├── config.OutputLanguage*            (needs BootstrapEnvFromArgs's hoist)
  ├── refuseNestedSandboxProfile()
  ├── tmpDirBootstrapFromArgs()         (settings/GOPI_TMPDIR before any verb)
  ├── signalrec.Install()
  ├── subcommand dispatch chain         ← a verb spelling still wins
  ├── top-level help scan               ← guarded by sessionResumeFaceArmed
  ├── taskWorkdirCapture()
  ├── runSessionResumeFlag(args)        ← THIS FACE
  ├── config.FlagOverrides(args)        ← --session is unknown to this parser
  └── … bare-prompt/agent path (readInput → newAgent → ag.RunPrompt)
```

It must sit **below** `config.FlagOverrides`: that parser rejects any flag it
does not know, and `--session` is not a knob — so a face placed above it would
still have to *be* a catalog row (i.e. a new env var/settings key, which Rule 35
forbids for an address that needs no configuration). It must sit **above** the
bare-prompt path, or the flag would buy an agent run.

## The help-query guard (the one non-obvious interaction)

`main()`'s top-level help scan matches **any** `-h` in argv and prints the
top-level page. So `gopi --session -h` printed gopi's page, and the flag's own
page was unreachable — while `scanSessionResumeFlag` was *also* taking `-h` as
the session id, which would answer a usage question with a shape refusal.

Both halves are fixed in the flag's own reader, and the fix is one predicate
used by two call sites (Rules 13/14 — not two opinions):

- `scanSessionResumeFlag` treats a `-h`/`--help` following the flag as **not a
  value** and leaves it in the tail;
- `sessionResumeFaceArmed(args)` — which is just that scan — gates the
  top-level help scan, exactly as the subcommand chain above it already does
  (a subcommand's `-h` wins over the top-level page for the same reason).

`gopi -h` stays unconditional whenever the flag is absent.

## Rejection order is the contract (and it is what the run evidence shows)

1. help query → the flag's page, exit 0, nothing resolved;
2. missing id → "`--session` requires a session id" + page, exit 1;
3. **shape** → `sessionid.IsAdmissible` / `sessionid.Refuse`, exit 1. The
   refusal is the package's own sentence, naming the canonical id it meant. The
   gate runs BEFORE any filesystem join because that is the whole reason the
   package exists: joining `01a0ae5ff4d0…` mints a SECOND session container
   beside the row's own;
4. unknown/unresolvable → `resolveResumeTarget` → `resolveResumeLogPath`'s
   "neither a raw log path nor an existing session id (looked under …)",
   exit 1;
5. otherwise → the checkpoint load, the process-identity gate, the row
   adoption/linkage (session-only, reported), `ag.Resume(input, history)`.

A session id names no ledger row, and the face says so: the resume continues
**session-only** and reports it — it never adopts or closes a row nobody named.

## Evidence

- `devdoc/…/task-report.md` (the run's own report) carries the three real runs:
  a successful resume (`RESUMED-OK` printed, `loaded 33 history message(s)` on
  stderr), the non-canonical refusal with `sessionid.Refuse` verbatim, and the
  unresolvable id.
- `cmd/agent/cli_resume_session_flag_test.go` pins the contract on the real
  binary: the flag's own `-h` page, both refusal classes, the missing id, the
  argv split, and the structural pin that this face calls `resumeSession`
  rather than carrying a second pipeline.

## Config-knob symmetry with the bare-prompt face (the limitation, and its repair)

Step 1 refused EVERY word beginning with `-` in this face's argv tail (Rule 15:
name a stray option back instead of ignoring it). A CONSEQUENCE was measured on
the built binary at `c196bfff4`, and it is worth stating because the sibling
surface differs:

| invocation | exit | first line (step 1) |
|---|---|---|
| `gopi --settings /tmp/none.json "hi"` | 0 | `[gopi] loaded GOPI.md …` — the bare-prompt path accepts the knob |
| `gopi --session <id> --settings /tmp/none.json "hi"` | 1 | `gopi --session: unknown option "--settings" …` |
| `gopi session resume <id> --home /tmp/zz` | 1 | `cannot resolve session id …: open /tmp/zz/.gopi/sessions` — the knob itself was consumed |

So the refusal was this face's parser, not a global config rule: a resume through
`--session` could not be combined with `--home` / `--settings` / `--tmpdir`. That
asymmetry was filed as a bug row with the measured evidence attached, and it is
REPAIRED here.

### What the repair is

The two faces are the SAME SHAPE — a prompt plus the catalog's configuration
knobs — so they now resolve configuration through the same code:

- `sessionResumePrompt` no longer refuses every `-` word. It asks
  `config.CatalogFlagArity` about each one, which reads the arity table
  `config.FlagOverrides` itself parses with, built from the `EnvVars` catalog.
  A catalog knob (with its operand, `--name VALUE` or `--name=VALUE`) is
  **consumed**; the remaining positional words are the prompt. Nothing about the
  accepted set is written in `cmd/agent` — a knob added to the catalog is
  accepted here the same day (Rules 1/13/14).
- Any word the catalog does not carry is still **refused by name**, and so is a
  single-dash option — the sentence now also names the alternative channel
  (`gopi --help-env` for the flag surface, `gopi session resume` for the row
  address).
- The consumed words go to `resolveRunConfig` (`cmd/agent/
  run_config_resolution.go`), the function `main()`'s five-step block was
  extracted into: flag parse → `--home` exported before the settings read →
  settings file read (a malformed one still hard-exits, named) → `ApplyEnv`
  (flags win) → temp-workspace publication + `--settings` export. The run path
  and this face call the SAME function, so there is one precedence, not two
  (Rule 2).
- The help page (`gopi --session -h`) states the shape: the knobs may be given
  before or after the id, they are consumed, and the accepted names are the
  catalog's.

### Measured on the built binary after the repair

Pre-change binary = this branch's HEAD before the diff (the step-1 code), rebuilt
from `git archive HEAD`; post-change = the same tree plus this change. Same argv,
same machine, same profile.

| argv | pre-change (exit / first line) | post-change (exit / first line) |
|---|---|---|
| `--session <id> --settings /tmp/zz-nonexistent.json` | 1 / `unknown option "--settings"` | 1 / `resume target … is neither a raw log path nor an existing session id` — the knob was CONSUMED |
| `--session <id> --home <fresh dir>` | 1 / `unknown option "--home"` | 1 / `cannot resolve session id …: open <fresh dir>/.gopi/sessions` — the profile moved |
| `--session <id> --tmpdir <a regular file>` | 1 / `unknown option "--tmpdir"` | 1 / `[gopi] tmpdir: tmpdir is not usable … (value from --tmpdir)` — the workspace was published and validated |
| `--session <id> --settings <malformed json>` | 1 / `unknown option "--settings"` | 1 / `[gopi] config: … failed to parse …` — the FILE was read |
| `--session <id> --bogus` | 1 / `unknown option "--bogus"` | 1 / `unknown option "--bogus"` — still refused by name |
| `--settings <path> --session <id>` (knob FIRST) | 1 / `unknown option "--settings"` | 1 / the same resolution sentence — position does not decide |
| `--session <id> --settings=<path>` (inline) | 1 / `unknown option "--settings=/tmp/…"` | 1 / the same resolution sentence |

The exit statuses are equal in every row on purpose: what changed is WHICH
sentence answered, and the sentences are the observation — a refusal about a
missing session proves the id arm was reached, i.e. the knob did not block it.
Evidence: the run's session directory, `artifacts/ab.txt` (both binaries, every
row), `artifacts/help-page.txt`, `artifacts/knobs.txt`.

### What was deliberately NOT changed

- The `--session` flag itself is still not in the catalog: this face is an
  address for an existing implementation, not a new knob (Rule 3). The catalog
  answers "is this word a configuration flag?", not "is this face a verb".
- A **flagless** catalog name (e.g. `--gopi-db-url`, an em dash in
  `gopi --help-env`'s flag column) is refused by this face as a non-catalog
  option. It has no CLI channel anywhere — the run path refuses it too, with the
  longer "a real GoPi knob with NO CLI flag of its own" sentence; this face's
  shorter refusal is not a second policy, it is the same refusal class.
