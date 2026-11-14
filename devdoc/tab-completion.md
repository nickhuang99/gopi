<!-- doc-class: normative -->
# Shell Tab-Completion for the gopi CLI (feat/completion-epic)

## Epic goal
`gopi <TAB>` should offer the first-level commands (task, memory, web, log, …)
and `gopi <verb> <TAB>` the second-level subcommands (web → search|fetch,
memory → search|store|get|…, task → list|show|submit|run|…).

## Deliverable
New top-level verb **`gopi completion bash|zsh|fish`** — prints a shell
completion script to stdout. Files:

- `cmd/agent/cli_completion.go` — the verb + candidate tables + script renderers
- `cmd/agent/cli_completion_test.go` — locks the tables against the dispatchers
- `cmd/agent/main.go` — `isCompletionSubcommand` added first in the dispatch chain
- `internal/config/help.go` — top-level usage advertises `gopi completion bash|zsh|fish`

## Design decisions
1. **Static scripts, no binary back-call.** cobra's dynamic `__complete`
   protocol is unnecessary: every candidate set here is a compile-time table.
   The generated scripts embed the candidate lists, so completion is instant,
   offline, and works in shells where invoking the binary is awkward. (The
   `__complete` reserved verb is noted in the source for a future dynamic
   extension — e.g. session-id / task-id completion.)
2. **Single source of truth for candidate sets** (GoPi Rule 2): the tables in
   cli_completion.go are the one place a verb/subcommand list lives for
   completion; cli_completion_test.go **locks each table against the actual
   dispatch code** (scans runTasksSubcommand's case labels, compares against
   memorySubcommandNames) so a new verb added to a dispatcher without updating
   the table fails the test.
3. **User-level install — no /etc/.** /etc/bash_completion.d is root-owned and
   the epic's "some file in /etc/…" registration is not required by any shell:
   - bash: `~/.local/share/bash-completion/completions/gopi` (bash-completion's
     BASH_COMPLETION_USER_DIR / XDG convention) or `source <(...)`
   - zsh: `~/.zfunc/_gopi` + fpath (the `#compdef` header auto-registers)
   - fish: `~/.config/fish/completions/gopi.fish`
   This matches git/docker/kubectl/gh — none write /etc/.
4. **Bash `-o default`** keeps filename completion for leaf positions (after a
   group verb's subcommand, gopi's args are files/session ids).
5. **Rule 1 (no magic numbers)** honored — no literal sizes/timings added.

## Verification (LLM-adoption standard — Rule 11)
The feature was verified by a REAL interactive bash on a pty (python pty.fork
+ INPUTRC disabling bracketed paste), not just unit tests:

```
$ source <(./bin/gopi completion bash)
$ gopi mem<TAB><TAB>
memory   memsync
$ gopi memory se<TAB>      →  gopi memory search
$ gopi task li<TAB>        →  gopi task list
$ gopi web <TAB><TAB>
fetch   search
```

compgen-level checks also pass for every level (first-level prefix matching,
group subcommand listing, full 21-verb task set, completion→bash|zsh|fish).

## Build / test
- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- Targeted tests: `go test ./cmd/agent/ -run 'Completion|Usage'` — all PASS
  (candidate-set lock tests, bash/zsh/fish script structure, usage advertisement)
- Full `go test ./cmd/agent/` (~2.5 min E2E): all completion tests PASS. Two
  UNRELATED tests failed — `TestRunTaskWorker_OnceSweepsZombieFirst` (failed
  only under concurrent-agent SQLite/worker contention) and
  `TestTaskRunSurvivesClosedStdoutPipe` — the latter **fails identically on
  clean master** (pre-existing SIGPIPE-timing flake, reproduced in the main
  repo with zero local changes; memory #1299's contention warning).
- Commit: `e018430` (feat) + `6998f24` (zsh fix) + `4e98d74` (devdoc) on
  `feat/completion-epic`.

## Not done / deferred
- Dynamic completion (session ids, task ids, memory db rows) — static sets
  only; the `__complete` back-call seam is reserved in source comments.
- Flag completion (e.g. `gopi task list --st<TAB>`) — beyond the epic's
  command+subcommand scope; per-verb flag tables (taskFlagMember etc.) exist
  as the future data source.
