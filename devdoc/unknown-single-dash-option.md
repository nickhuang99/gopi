<!-- doc-class: historical -->
# `run_cmd: gopi -z` — what it means, and the silent-degradation fix behind it

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Date: 2026-09-21 · Branch `fix/unknown-dash-flag-refusal` · Origin: operator ticket
"what is task run cmd -z? I seriously suspect this is big bug" (rows
`01a0c2df-c847-…` / `01a0c2df-7480-…`, seen via `gopi task show`).

## 1. Answer: neither a typo nor a corrupted command

`run_cmd` is **the verbatim launcher argv**, rendered by the one shared renderer
(`internal/runcmd.Command`: argv[0] reduced to a basename, every element
shell-quoted). It is written by the harness auto-registration hook
(`internal/agent/taskregister.go` → `RunCmd: runcmd.Command(os.Args)`), so a bare
`gopi` run records **exactly how it was launched** — that is the field's whole
purpose (a crashed row's re-run recipe).

So `run_cmd: gopi -z` says: *this run was launched as `gopi -z`, with `-z` on
the command line* — and that is literally what happened. Verified on master by
reproducing the shape (scratch `--home`/`--db`):

```
$ ./bin/gopi --home <scratch> --db <scratch>/mem.db -z < /dev/null
[gopi] task ledger: registered 01a0c311-… (in_progress, auto, session 2a19e596-…)
…
$ ./bin/gopi --home <scratch> task list --db <scratch>/mem.db --all
#01a0c311  …  [failed]  [auto] -z  (model deepseek-flash, source auto, …) [via auto]
$ ./bin/gopi --home <scratch> task show 01a0c311-…
run_method:     auto
run_cmd:        gopi --home <scratch>/home --db <scratch>/mem.db --base-url … -z
```

`-z` **was the prompt**. The parser accepted it as a positional
(`!strings.HasPrefix(a, "--")` → Positionals), `readInput` turned the positionals
into the task text, the session registered an auto row whose summary is
`"[auto] " + briefing` = `[auto] -z`, and `run_cmd` recorded its own argv.
Nothing was random and nothing was mis-recorded.

The two rows the operator saw (15:30:19 and 15:30:40, siblings of a 15:30
probe family whose sessions were born in `/home/user/work/GoPi` and
`/home/user/work/GoPi/internal/tools` — see each session's `session_start.workdir`)
were killed 1–2 s in; a killed run never writes back, which is why both were
left `in_progress` with `process: … (live: gone)`. The rows are the audit trail
of a harness probe, not a task that "ran as gopi -z".

## 2. The real defect this exposed (and what changed)

The surprise is not the ledger, it is the parser: **an unknown single-dash token
was silently accepted as prompt text.** Every other gopi surface already refuses
an unknown option loudly — `gopi: unknown flag --bogus` for the bare-run long
flags, `gopi worker: unexpected argument "--bogus" — refusing to start the
worker loop` (exit 2) for the worker grammar (Rule 15, `devdoc/worker-unknown-flag.md`).
A one-letter typo of that kind therefore produced a **junk run**: a real
harness session, a real ledger row, real model tokens spent on the prompt `-z`.

### The fix (this branch)

`internal/config/flags.go` — the ONE parser of the bare-run argv, shared by the
agent path and the pre-subcommand `tmpdir`/scratch hoist:

- the option spellings are named constants (`flagPrefix = "--"`,
  `shortFlagPrefix = "-"`) — Rules 1b/13, no bare literal decides a grammar;
- a token starting with a single `-` (and not `--`) is refused with
  `unknownSingleDashOptionFmt`, naming the token, stating that gopi has no
  short options, and naming the two deliberate ways to pass such text:
  a positional prompt, or `--prompt -z`.

Refusals reach the operator where they already do — `gopi: <err>` + usage, exit 1
(`cmd/agent/main.go`). The help text (`internal/config/help.go`) and `CONFIG.md`
§3 now state that there are NO single-dash options, so the rule is advertised
where a reader looks.

## 3. Why not a broader "reject everything dash-shaped"

The escape hatches stay open by design, because a prompt is free text:

| intent | spelling |
|---|---|
| prompt that *is* `-z` | `gopi --prompt -z` (a flag VALUE is never reclassified) |
| prompt *mentioning* a dash word | `gopi "explain what -z does"` (a positional that starts with a normal character) |
| multi-line / meta-character payload | `gopi @payload.txt`, `cat p.txt \| gopi @-` |

Only a token that *starts* with a single dash is refused, and only on the
bare-run argv (`gopi <token>`); subcommand grammars were already closed.

## 4. Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK.
- Tests: `go test ./internal/config/ -count=1` — ok (includes the new
  `unknown_single_dash_option_test.go`: refusal + `--prompt -z` still accepted +
  a prompt mentioning `-z` still accepted + positional collection unchanged).
- Red → green: with the fix stashed the new test fails
  (`FlagOverrides(-z) = nil error, want a refusal`); with it, green.
- Ran (real binary, scratch home/DB):
  - `./bin/gopi --home <s> --db <s>/mem.db -z` → `gopi: unknown option "-z": …`
    exit 1, and `task list --all` afterwards → `No tasks found.` (no junk row);
  - `./bin/gopi --home <s> --db <s>/mem.db --prompt -z` → the run still executes
    (ledger gets `[auto] -z`, proving the honest path is untouched);
  - `./bin/gopi --home <s> --db <s>/mem.db "say OK"` → still runs;
  - `./bin/gopi -h` → prints the new "There are NO single-dash options" line.

## 5. Not done / noted

- The three 15:30 probe rows were **not** cleaned up (the ledger is append-only
  and rows are not mine to delete); they are `in_progress` with a dead holder
  and are reclaimable by the ordinary stale sweep.
- `internal/standards` reports `TestDocManifestCoversEveryDevdocFile` red for
  three pre-existing devdoc files (`orphan-stash-7daab90b-salvage-report.md`,
  `worktree-guest-eviction-refusal.md`, `worktree-guest-row-release-refusal.md`)
  — reproduced on stashed master, not from this change. This doc carries its own
  DOC-INDEX row.
