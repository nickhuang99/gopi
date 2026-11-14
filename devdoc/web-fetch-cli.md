<!-- doc-class: historical -->
# Task Report: Expose web_fetch as a gopi CLI subcommand

Branch: `feat/web-fetch-cli` (worktree `~/work/gopi-webfetch-cli`, main repo `~/work/GoPi`)

## Completed
- **`cmd/agent/cli_webfetch.go`** — new `gopi web_fetch <url> [--max-chars N]` subcommand,
  following the cli_websearch.go pattern exactly:
  - `isWebFetchSubcommand()` wired into main.go's dispatch list (before the general -h scan).
  - `parseWebFetchArgs()` — pure/unit-testable parser using the shared `newMemoryFlagSet`
    hand-rolled `--name value` / `--name=value` parser; positional url (trimmed),
    `--max-chars` positive-int validation, unknown-flag rejection.
  - `runWebFetchSubcommand()` dispatches to the SAME `tools.WebFetch` the agent tool_use
    uses → CLI behavior identical to tool calls; tool-level failures print as the tool's
    structured JSON payload (`status ok|error|timeout|unavailable`) verbatim, exit 0.
  - Usage errors → stderr + full usage + exit 1; `-h/--help` → usage, exit 0.
- **`internal/config/help.go`** — top-level `gopi -h` subcommand list now advertises
  `gopi web_fetch` with the Search ≠ Read one-liner (search=snippets, fetch=full text).
- **`cmd/agent/usage_test.go`** — discoverability guard extended with `"gopi web_fetch"`.
- **gopi log filter**: verified already complete — `toolFamilies["web"]` =
  `{web_search, web_fetch}`, formatter has web_fetch display case (URL first-line).
  No code change needed; live-smoked (below).
- **`cmd/agent/cli_webfetch_test.go`** — 10 unit tests: parse basic/default/equal-form,
  cap clamping, missing-url, bad --max-chars (abc/-5/0), unknown-flag rejection,
  whitespace trim, dispatch negative space, usage-text guards.

## Not Done
- No separate `--fetch` log flag: the epic's "log filter" requirement is satisfied by the
  existing `--web` family (web_search+web_fetch together, matching how the tools are
  paired in the agent). A lone `--filter web_fetch` already works for fetch-only.
- example.com returns `chars:0` (jump host strips its near-empty body) — tool semantics,
  not a CLI bug; verified with content-rich pages instead.

## Verification
- Build: `go build ./...` OK; binary `CGO_ENABLED=0 go build -o /tmp/gopi-webfetch-test ./cmd/agent` OK.
- Tests: `go test ./cmd/agent/ ./internal/config/ ./internal/gopilog/` all ok
  (10 new webfetch tests incl.). Full `go test ./...` result: see session log.
- Live smoke:
  - `gopi web_fetch -h` → usage printed.
  - `gopi web_fetch https://en.wikipedia.org/wiki/Go_(programming_language) --max-chars 600`
    → `{"status":"ok",...,"chars":627,"truncated":true,"content":"Go (programming language)..."}`
  - `gopi web_fetch "not-a-url"` → `{"status":"error",...,"message":"only http/https URLs are supported"}`
  - `gopi -h` line 175: `gopi web_fetch <url> [--max-chars N]` present.
  - `gopi log f8010dcb-... --web` → `Filter: web_fetch, web_search` + TOOL lines listed.
- Commit: `683e88f feat(cli): expose web_fetch as a gopi subcommand` on `feat/web-fetch-cli`
  (doc-hash fix landed as a follow-up commit; see `git log --oneline -- devdoc/web-fetch-cli.md`).

## Rule 11 (LLM adoption) note
This is a CLI subcommand exposure (human-invoked surface), not a new agent tool: the
agent-side `web_fetch` tool already exists and is adopted via tool_use. The CLI path
calls the identical `tools.WebFetch`, so agent and CLI behavior cannot diverge.
