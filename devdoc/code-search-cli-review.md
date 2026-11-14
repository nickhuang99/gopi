<!-- doc-class: historical -->
# Task Report: Review of cmd/agent/cli_code_search.go — function inventory + --root relative-path check

## Completed
- Full function inventory (see below).
- Verified `--root <relative-dir>` handling by code read AND runtime repro
  (built binary, ran `gopi-cs code-search HelperFn --root internal` from a
  test dir → hit found, exit 0, `.gopi-tags` written inside `internal/`).

## Function inventory — cmd/agent/cli_code_search.go
| Item | file:line | Kind |
|---|---|---|
| `codeSearchDefaultRoot` = "." | cli_code_search.go:14 | const |
| `codeSearchMaxResults` = 50 | cli_code_search.go:19 | const |
| `codeSearchUsage` | cli_code_search.go:24 | const (help text) |
| `isCodeSearchSubcommand(args []string) bool` | cli_code_search.go:53 | func |
| `codeSearchOptions{symbol, root}` | cli_code_search.go:66 | type |
| `parseCodeSearchArgs(args []string) (*codeSearchOptions, error)` | cli_code_search.go:75 | func |
| `runCodeSearchSubcommand(args []string)` | cli_code_search.go:111 | func |

Supporting parse helper lives in cli_memory.go:98 (`newMemoryFlagSet`,
`--name value` / `--name=value` hand-rolled parser, reused here).

## Findings: --root pointing at a relative directory

**Verdict: CORRECT.** The historical bug (relative root → ctags tags file
resolving to `<root>/<root>/.gopi-tags`) is fixed at exactly the right layer:

1. `parseCodeSearchArgs` (:75) leaves `opts.root` raw, defaulting to "." —
   parsing is pure and root-agnostic (correct unit-testability split).
2. `runCodeSearchSubcommand` (:127-137) resolves `filepath.Abs(root)` BEFORE
   calling `tools.CodeSearch`, with a comment naming the `<root>/<root>/`
   failure mode; `os.Stat` dir-existence check uses the resolved path and
   reports the user-facing relative form on error.
3. Root cause confirmed in backend `internal/tools/symbol.go:599`
   `ensureTags`: builds `tagsPath := filepath.Join(root, ".gopi-tags")` and
   runs `ctags -f tagsPath .` with `cmd.Dir = root`. A relative root WOULD
   double-compose (`<root>/<root>/.gopi-tags`) — the CLI's Abs() call is
   what escapes it.

### Observations (low / informational, no action required)
- **[low] Defense-in-depth gap**: the agent loop passes `RootPath "."` and
  internal/tools tests pass absolute roots, but `ensureTags` itself never
  Abs()-es its input. Any future caller of `tools.CodeSearch` with a
  relative root re-introduces the bug. Optional hardening: Abs() inside
  ensureTags.
- **[low] Silent Abs failure**: `if abs, err := filepath.Abs(root); err == nil`
  keeps the raw root on error. filepath.Abs only fails on os.Getwd failure
  (removed cwd) — a reasonable silent fallback, worth a comment at most.
- **[low] `--root` with no value** (`gopi code-search Sym --root`) parses as
  empty value (memoryFlagSet treats a missing/`--`-prefixed next token as ""),
  then becomes default "." — silent, arguably surprising. The tests at
  cli_code_search_test.go cover default-root and `--root internal/` values.
- CLI and agent loop scope duplicated (globs/exclusions/cap) — consistent
  today (MaxResult 50 both sides), a drift risk only.

## Not Done
- No code changes (review-only task). The two observations above are noted,
  not fixed — no defect found that warrants a fix + regression test.
- Did not touch the worktree's pre-existing modified files (loop.go,
  symbol.go — another task's in-flight state).

## Verification
- Build: `go build -o /tmp/gopi-cs ./cmd/agent/` → OK
- Tests: `go test ./cmd/agent/ -run 'CodeSearch|CodeSearchDefault' -v`
  → 7/7 PASS (parse, default-root, subcommand, usage, backend find/no-symbol,
  top-level usage)
- Runtime repro: `cd /tmp/csroot && gopi-cs code-search HelperFn --root internal`
  → `HelperFn  a.go:4  func  func HelperFn() int { return 1 }`, exit 0,
  `.gopi-tags` created at `internal/.gopi-tags` (no nested dir) — bug absent.
- Commit: n/a (report-only; no tracked file modified)
