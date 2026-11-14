<!-- doc-class: historical -->
# Evaluation: Routing GoPi to code_search over bash grep

## Goal

GoPi has `code_search` (ctags symbol lookup, structured file:line/kind/signature
hits) and `keyword_search` (ripgrep full-text), but the model's trained default
is "grep the code". Make GoPi **proactively choose `code_search`** to locate
symbol definitions, and stop descending into `bash grep`/`ls` when results are
empty.

## Levers used (all in this branch)

1. **`internal/agent/loop.go` — new `toolRoutingRules` const**, appended to the
   system prompt before the task-management rules. Five imperative routing
   rules + one worked example:
   - symbol definition → `code_search` FIRST with the bare identifier; do NOT
     `bash grep` (slower, noisy raw lines)
   - "No symbol found" → the identifier does not exist (code_search auto-falls
     back to ripgrep, so an empty result is definitive for Go code) → search a
     related identifier with `keyword_search` or read the package dir; do NOT grep
   - non-identifier text (errors, comments, docs, config, scripts) → `keyword_search`
   - bash is for build/test/run/git/file ops, NOT for searching code
   - `read` only after a search tool told you the path and line
2. **`code_search` description** — "Call this FIRST for any definition lookup…
   Faster and cleaner than bash grep… never a sentence, description or tool
   name… 'No symbol found' means the identifier exists nowhere: do not grep for
   it."
3. **`keyword_search` description** — "use for text that is NOT a code
   identifier, and as the follow-up when code_search reports a symbol is not
   found."
4. **`bash` description** — "NOT for searching code: locating a symbol
   definition is code_search's job, full-text search is keyword_search's."
5. **`GOPI.md` Rule 3** — the committed project-context file was stale and
   actively counterproductive ("GoPi has 5 tools: read, write, edit, bash,
   remote"; "bash for local commands (grep, find, ls, git)"). Updated to the
   6 real tools with the same routing.

## Method

Ran the same tasks before vs after the change on branch `feat/code-search-prompt`
(model deepseek-v4-flash, GOPI_THINKING=disabled, GOPI_DEBUG=1 for the tool-call
log). Tools used per run extracted from the `[gopi] TOOL CALLS` debug lines.

## Before (feat/code-search baseline)

| task | observed tool calls |
|---|---|
| "where is the struct DeepSeekClient defined" (symbol does not exist) | `code_search(DeepSeekClient)` → empty → `keyword_search` ×2 → **`bash grep -rn "type DeepSeekClient"`** + `ls` + `grep` descent |
| "keyword_search crashes with very long query" (bugfix) | `code_search(keyword_search)` (tool name as symbol) → **`bash find / -name "*.go"`** (whole filesystem) → git clone → experiments |
| "GOPI_HIDE_THINKING ignored on fallback path" (bugfix) | **`bash ls ~/work/GoPi`** first (no search tool), then `keyword_search`, read of an absolute path outside cwd |
| "find where NewClient is defined" | `code_search(NewClient)` → `read` (already OK for this phrasing) |

## After (feat/code-search-prompt)

| task | observed tool calls |
|---|---|
| "where is the struct DeepSeekClient defined" | `code_search(DeepSeekClient)` → empty → **`code_search(Client)`** (Rule 2: related identifier!) → `keyword_search(DeepSeek)` → `read internal/deepseek/client.go`. **Zero bash grep / ls.** |
| "keyword_search crashes with very long query" (bugfix) | `code_search` → `read search.go` → `read loop.go` → `read search_test.go` → wrote repro test. No filesystem-wide find, no clone. (Hit the 180s probe timeout mid-investigation — same as before.) |
| "GOPI_HIDE_THINKING ignored on fallback path" (bugfix) | `code_search(GOPI_HIDE_THINKING)` → `code_search(Chat)` → reads + `keyword_search(fallback/ChatStream)` ×3 → **`code_search(chatOnce)`** — converging on the right function. **Zero bash grep.** |
| "find where NewClient is defined" | `code_search(NewClient)` → `read(client.go:50)` → correct report `internal/deepseek/client.go:50`. |

Key acceptance evidence (final acceptance run, verbatim log lines):

```
[gopi] 🔧 TOOL CALLS [0]: 1 calls
[gopi]   [1/1] code_search(map[symbol:NewClient])
[gopi] 🔧 TOOL CALLS [1]: 1 calls
[gopi]   [1/1] read(map[limit:8 offset:50 path:internal/deepseek/client.go])
```

## Conclusion

The routing rules convert the two failure modes observed at baseline:

1. **Empty-result descent** (`code_search` miss → `bash grep` + `ls`): after the
   change the model re-queries with a related identifier (`Client`), the
   sanctioned Rule-2 move.
2. **Wrong-input chaos** (tool name / sentence passed as symbol → filesystem-wide
   `find`): after the change the model recovers by reading the package file the
   search pointed at, staying on the search tools throughout.

Residual stochastic noise remains (the model occasionally still runs a redundant
`bash find` as over-verification after already answering correctly), but
definition lookups consistently open with `code_search`.

## Verification

- gofmt: clean
- go vet ./...: clean
- go build ./cmd/agent/: OK
- go test ./...: internal/agent ok, internal/deepseek ok, internal/tools ok
