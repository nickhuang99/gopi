<!-- doc-class: historical -->
# Task Report: code_search indexes JavaScript and TypeScript (Task G)

## Completed

- `internal/tools/symbol.go`:
  - `ctagsLanguages` extended to `Go,Python,Sh,C++,Rust,JavaScript,TypeScript`.
  - `indexedSourceExtensions` extended with `.js`, `.jsx`, `.mjs`, `.cjs`,
    `.ts`, `.tsx` (all six required extensions from the Epic).
  - New `ctagsLanguageMapArgs` (`--map-JavaScript=+.cjs`,
    `--map-TypeScript=+.tsx`) wired into the `ensureTags` ctags invocation so
    `.cjs` and `.tsx` — extensions universal-ctags does not map by default —
    are indexed. Must be separate args: a comma-joined `--map-A=+x,--map-B=+y`
    option silently drops the second mapping (verified empirically).
  - Per-language kind maps for `JavaScript` (class, func, method, var, const,
    property, getter, setter, field, generator) and `TypeScript` (class, func,
    method, interface, namespace, const, enum, alias, enumerator, property,
    var, generator), so hits render readable kinds instead of bare letters.
  - Comments updated (language list, map-flag rationale, kind-map provenance).
- `internal/agent/loop.go`: the `code_search` tool Description, the
  `toolRoutingRules` indexed-language list, and the `codeSearchScope` comment
  now include JavaScript and TypeScript.
- `devdoc/search-tool-split.md`: tool-scope table row now lists JavaScript and
  TypeScript.
- Tests:
  - `symbol_test.go`: JS/TS cases in `TestKindNameByLanguage` and
    `TestFormatTagEntryLanguage`; required-extension assertions in
    `TestCodeSearchGlobsMatchExtensions`; new
    `TestEnsureTagsRegeneratesForNewerTS` proving a newer `.tsx` triggers index
    regeneration and the regenerated index contains `.js`/`.ts`/`.tsx` entries.
  - `search_test.go`: new end-to-end `TestCodeSearchJavaScriptAndTypeScript`
    resolving `helper`/`Widget`/`NAME`/`counter` (app.js), `Shape`/`compute`/
    `Circle` (app.ts), `App` (comp.tsx) and `legacyFn` (legacy.cjs) through the
    full ctags → format → dedup pipeline.
  - `loop_test.go`: `TestCodeSearchScopeCoversIndexedLanguages` now asserts the
    six JS/TS globs (`**/*.js`, `**/*.jsx`, `**/*.mjs`, `**/*.cjs`, `**/*.ts`,
    `**/*.tsx`).

## Not Done

- `GOPI.md` needed no change: the Epic listed it as a site mentioning indexed
  languages, but a full read + grep found no language list there (Rule 3 only
  lists tool *kinds*). No edit was required or made.
- `bugs/` scratch files in the main repo were left untouched (untracked, not
  part of this task).

## Deviations from the Epic (verified against installed tooling)

- "universal-ctags language name for both: JavaScript" is not true for
  universal-ctags 5.9.0: TypeScript is a separate parser (`ctags
  --list-languages`). The implementation uses the real parser names
  (`JavaScript` for .js/.jsx/.mjs/.cjs, `TypeScript` for .ts/.tsx), which is
  what makes the behavior requirement hold.
- `.cjs` and `.tsx` are not indexed by default (`--languages` only restricts
  parsers; it cannot add extensions). Both are covered via
  `--map-JavaScript=+.cjs` and `--map-TypeScript=+.tsx`, keeping the
  extension set and the parser set in sync.

## Verification

- Build: `go build ./...` — OK; `go vet ./internal/tools/ ./internal/agent/` — OK
- Tests: `go test ./...` — all packages PASS (tools 14.6s, agent 11.9s,
  deepseek 2.3s; new JS/TS tests ran, none skipped)
- Commit: `5f58f11` (feat(tools): index JavaScript and TypeScript in code_search)
