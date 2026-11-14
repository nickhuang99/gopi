<!-- doc-class: historical -->
# Task Report: `gopi chat` REPL — `/help <cmd>` ignored its argument

**Branch**: `fix/chat-help-command-arg` (worktree `~/workspace/gopi-chat-help-arg`)
**Base**: master `17cec86b`
**Scope**: HP feature layer — `cmd/agent` command surface (not the system layer:
no scheduling/merge/claim/worktree code touched).

## Completed

- **Root cause, from git history (not guessed).** `git blame` on the offending
  arm: `cmd/agent/cli_chat.go:495` `case "/help":` came in with `e408008a`
  (2026-08-28, the REPL command switch); it printed `chatUsage + slashHelpText()`
  **ignoring the argv tail**. The shared command table with real `/help <cmd>`
  semantics landed the NEXT day (`642e9bc3`, R4+R5, 2026-08-29) and the REPL arm
  was never wired to it. The web carrier is correct (it does not intercept, so
  `execSlashCommand` answers `/help <cmd>`) — the defect is REPL-only.
- **Fix.** New `chatHelpText(raw string)` in `cmd/agent/commands.go` owns the
  REPL's help decision in ONE place:
  - bare `/help` (or `/help -h`) → `chatUsage + slashHelpText()` (unchanged
    behaviour);
  - `/help <cmd>` → that command's `Usage` from the shared table;
  - `/help <native>` (`quit` / `exit` / `clear` / `new` / `tools`) → the native
    usage block, instead of the misleading `unknown command`;
  - `/help <no-such-thing>` → `unknown command "…" — try /help`.
  `cli_chat.go`'s `/help` arm now just routes through it.
- **Rule 1b hygiene.** The switch words became named constants in the owning
  package (`quitCommandName`, `exitCommandName`, `toolsCommandName`,
  `helpCommandName`, `helpShortWord`); `execSlashCommand`'s previously bare
  `word == "help" || word == "?"` now references them, so the REPL arm and the
  shared dispatch cannot drift apart by a typo.
- **Regression tests** (`cmd/agent/cli_chat_test.go`):
  - `TestChatHelpText_TakesTheCommandArgument` — the actual defect: `/help task`
    must be `task`'s usage, not the whole table; also pins the bare form, the
    native words, and the unknown word.
  - `TestChatHelpText_AgreesWithSharedDispatch` — the REPL's text for a table
    command must equal `execSlashCommand`'s, so the two help surfaces cannot
    diverge (this is the assertion that would have failed before the fix).
  - `TestChatHelpSwitchRoutesThroughTheArgument` — anti-re-inline gate: the
    `/help` arm must route through `chatHelpText` and must not mention the
    literal word again, so a future edit cannot restore the bug.
- **Real-REPL evidence** (`.evid/help-arg/`, transcripts of the same stdin —
  `/help task`, `/help memory`, `/help clear`, `/help nosuchthing`, `/quit` — on
  the broken and fixed binaries):
  - before: every `/help <cmd>` printed the 60-line table (see
    `before-help-task.txt`, which opens with `Usage: gopi chat …`);
  - after: `after-help.txt` shows `/help memory` → the memory command's 3-line
    usage, `/help clear` → `Usage: /new`, and the unknown word → the error.

## Not Done (honest)

- **Other `/help` spellings were not extended beyond parity with the shared
  dispatch**: `-help` and `?` are accepted by the word matcher, but the
  `--help`-style flag on a *sub*word (e.g. `/help task --json`) is not treated
  as a help request on the field — this mirrors `execSlashCommand` exactly, so
  the two surfaces still agree; changing it here alone would have introduced a
  divergence.
- **The REPL `/help` banner text itself** (`chatUsage`) is unchanged — the task
  was the argument, not the banner wording.
- The `.evid/help-arg/` transcripts are untracked evidence files, not committed
  source (the compiled `gopi-fixed` binary next to them is 22 MB; it stays
  local).

## Verification

- Build: `CGO_ENABLED=0 go build -o /dev/null ./cmd/agent/` → OK
- Vet: `go vet ./cmd/agent/` → clean
- Tests: `go test ./cmd/agent/ -run 'TestChatHelpText|TestChatHelpSwitch' -v`
  → 3/3 PASS (`ok github.com/nick/gopi/cmd/agent 0.785s`)
- Standards gates: `go test ./internal/standards/` → ok (11.6s), no new
  baseline entries needed
- Gate is meaningful (not green-by-accident): the routing test was verified RED
  with the fix stashed, then green again with it restored
- Real REPL, byte-for-byte before/after transcripts on identical stdin:
  `.evid/help-arg/before-help-task.txt` vs `.evid/help-arg/after-help.txt`
- Commit: `fix(chat): /help <cmd> answers for the command (REPL ignored argv)`

## Recovery

```bash
git show fix/chat-help-command-arg:devdoc/chat-help-command-arg.md
git log --oneline -3 fix/chat-help-command-arg   # worktree-shared, already in the main repo
```
