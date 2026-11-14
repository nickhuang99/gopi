<!-- doc-class: normative -->
# gopi log --git filter for git_blame toolcalls

## Why
`git_blame` (internal/tools/gitblame.go) is a new agent tool that mimics
gitlens/gitblame for the LLM, but `gopi log` offered no way to filter its
toolcall usage — so users couldn't study/debug how the LLM calls it.

## What
- New `--git` family shortcut in `gopi log` → filters `git_blame` toolcalls:
  `gopi log <id> --git`
- Full-session view + filtered view render git_blame via a dedicated
  `formatToolCall` case (file + line range + first result line), not the raw
  `%v` Args dump.
- Discoverability: `gopi log -h` and the no-match hint list `--git`/`git_blame`.

## Files
- `cmd/agent/cli_log.go` — `--git` family (toolFamilies/familyOrder/
  activeFamilies), logFlagMember, logOptions, parseLogArgs, logUsage.
- `internal/gopilog/formatter.go` — `formatToolCall` git_blame case; no-match
  hint list.
- `cmd/agent/cli_log_test.go`, `internal/gopilog/formatter_test.go` — tests.

## Recipe (add a future agent tool to `gopi log` family filtering)
A toolcall added to `internal/agent/loop.go` becomes log-filterable by:
1. `toolFamilies` — add `"<flag>": {"<toolname>"}`.
2. `familyOrder` — add flag name (display order).
3. `logFlagMember`, `logOptions` field, `parseLogArgs` `fs.has(...)`.
4. `activeFamilies` switch case for the new flag.
5. `logUsage` — usage line + family-shortcuts block + "Flags can be combined".
6. (optional) `formatToolCall` case for a readable per-call summary.
7. (optional) no-match hint in formatter.go lists the tool name.
8. Tests: parse family shortcut, resolveFilterSet exact set, FormatToolFilter
   renders+excludes, formatToolCall summary.

Commit `b6f6cea`.
