<!-- doc-class: normative -->
# code_search split → code_search + keyword_search

**Date:** 2026-08-20
**Branch:** `feat/search-split` (from `feat/code-search`)

## Motivation

The original `code_search` tool was a single ToolDef with a `mode` param
(`symbol` / `refs` / `text`). The mode string forced the LLM to make a routing
decision ("is this a definition lookup or a text grep?") inside a tool
parameter, and the tool then had to re-derive its own behavior from it.

The split moves that routing decision **up** into the planning layer: the LLM
picks the tool by name — `code_search` for "where is X defined", `keyword_search`
for "grep anything". Two independent tools, no mode strings.

## Interface contract

Both tools return structured results, not bare strings. Shared types in
`internal/tools/search.go`:

```go
type SearchScope struct {
    RootPath    string   // search root dir
    IncludeGlob []string // include globs, e.g. ["**/*.go", "**/*.md"]
    ExcludeGlob []string // exclude globs, e.g. ["vendor/**", ".git/**"]
    MaxResult   int      // result cap (prevent explosion)
}

type CodeSymbolHit struct {
    SymbolName string // symbol name
    SymbolKind string // func / struct / var / const / type / ...
    FilePath   string
    LineNo     int
    Signature  string // raw definition line snapshot
}

type TextHit struct {
    FilePath string
    LineNo   int
    Snippet  string // context snippet around the match
}

func CodeSearch(scope SearchScope, symbolName string) ([]CodeSymbolHit, error)
func KeywordSearch(scope SearchScope, query string) ([]TextHit, error)
```

`CodeSearch` takes a **symbol name** (an identifier), never a natural language
sentence. `KeywordSearch` takes arbitrary text or a regex.

## Tool definitions (agent layer)

`internal/agent/loop.go` now exposes two ToolDefs:

| Tool | Backend | Param | Scope |
|---|---|---|---|
| `code_search` | ctags index (`.gopi-tags`) | `symbol` | indexed languages (Go, Python, shell, C++, Rust, JavaScript, TypeScript), exclude `.git/**`, `vendor/**`, cap 20 |
| `keyword_search` | ripgrep | `query` | all files, exclude `.git/**`, `vendor/**`, cap 20 |

The system prompt tool list was updated accordingly (`Tools: read, write, edit,
bash, code_search, keyword_search.`), and `validateToolArgs` gained the
`symbol` / `query` required-param checks.

## Fallback behavior

`CodeSearch` resolves the symbol against the ctags index first. On a **clean
miss** (not an error), it falls back to `KeywordSearch` over the same scope, so
a symbol that actually exists in the codebase never comes back empty:

- ctags hit → structured `CodeSymbolHit` (name, kind, `file:line`, signature
  from the ctags pattern field; `LineNo` from `--fields=+n`)
- ctags miss + rg hit → hits with `SymbolKind == "reference"` (occurrence, not
  definition), signature = rg snippet
- ctags miss + rg miss → empty slice, `nil` error (rendered as
  `No symbol "X" found in ctags index or ripgrep.`)

rg exit code 1 (no matches) is treated as an empty result, not an error.

## Implementation notes

- `internal/tools/search.go` (new): contract types, both public functions,
  timeout wrappers, ripgrep impl (`-n --column`), snippet building (centered
  on the match column, rune-boundary safe), and the LLM-facing formatters
  `FormatCodeSymbolHits` / `FormatTextHits`.
- `internal/tools/symbol.go` (refactored): ctags internals only — `ensureTags`
  (now takes the search root and runs with `cmd.Dir = root`), staleness check,
  `formatTagEntry` → now returns `CodeSymbolHit`, `kindName`,
  `signatureFromPattern`, `newestGoFile`. The old `query + mode` entry points
  and `rgSearch` are gone.
- Tags are now generated with `--fields=+n` so `CodeSymbolHit.LineNo` is
  populated; indices generated before that flag degrade to `LineNo == 0`.
- Dedup: qualified/unqualified ctags variants collapse to one hit per
  (file, line, kind); `SymbolName` is normalized to the queried name.

## Tests

`internal/tools/search_test.go` + updated `symbol_test.go` cover: structured
hit shape, absolute `RootPath` (no chdir), fallback path (ctags miss → rg),
missing symbol → empty/nil, empty query errors, regex queries, include/exclude
globs, `MaxResult` cap, formatters, and the rg-line/snippet parsers.
Integration tests skip when `ctags`/`rg` are absent. Agent-layer
`loop_test.go` covers the new validation cases and a real `keyword_search`
execution.

## Verification

- `go build ./...` clean
- `go vet ./...` clean
- `go test ./...` all green (ctags 5.9 + rg 13 present on the dev machine)
- `gofmt -l .` clean
