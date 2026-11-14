<!-- doc-class: historical -->
# CLI Code Review: web_search · web_fetch · code-search

**Branch:** `review/cli-search` — child task of the CLI code-review epic.
**Scope:** `gopi web_search`, `gopi web_fetch`, `gopi code-search` (cmd/agent/cli_websearch.go,
cli_webfetch.go, cli_code_search.go) + engines (internal/tools/websearch.go, search.go,
symbol.go, timeout.go) + plugin scripts (plugins/websearch/scripts/, deployed ~/.gopi).
**Review method:** run -h → read source → read engines → docs → git log/blame → runtime probes.
Docs-only task: no tracked code changed.

---

## Per-verb findings

### 1. `gopi web_search "<query>" [--method M] [--count N]`

**Design purpose.** Expose the `web_search` tool as a scriptable CLI face
(method/query/count → unified JSON), dispatch to the SAME `tools.WebSearch`
function the agent loop uses, print the tool's JSON verbatim. Dispatch via
`isWebSearchSubcommand` in main.go's pre-`-h` chain. Parsing is a pure
`parseWebSearchArgs` (unit-testable, no os.Exit) reusing the hand-rolled
`memoryFlagSet` — no stdlib flag, consistent with sibling subcommands.

**Method parity — VERIFIED GOOD.** CLI `webSearchMethods` map
(cli_websearch.go:15-22), tool enum in loop.go:897, tool-level
`webSearchScripts` map (websearch.go:112-120) and the two error strings all
list exactly `bing|google|arxiv|deepseek|github|wiki`. Runtime probe of
`--method bogus` errors with the full enum + usage and exit 1. The wiki method
(56f403f) was added in all four sites simultaneously — no drift found.
Merged multi-method default (method="") → bing+google+arxiv concurrent +
merge_results.py is implemented and documented (help text: "Empty = merged").

**Count caps — Rule 1 satisfied, Rule 2 duplicated.** All magic numbers are
named consts: CLI `webSearchDefaultCount=5` / `webSearchMaxCount=20`
mirror `tools.webSearchDefaultCount` / `webSearchMaxCount`; result caps
`webSearchResultCap=16KB`, merge timeout 120s, script timeout 200s are
named consts in the engine. **Rule 2 finding (P2):** the 5/20 pair is
deliberately duplicated (CLI const + tools const) with a comment saying
"mirrors" — no compile-time or test-time linkage. A future tools-side change
to max 30 silently desyncs the CLI cap. Same for web_fetch's 8000/100000.
A single source (exported consts) or a parity test would close this.

**Deepseek/count semantics.** `--count` is documented "ignored for deepseek
(AI-ask method)" — verified in the engine (`webSearchAskMethods` → args
become just `[query]`). Good.

**CLI-visible quirks (P2/P3):**
- **No exit-code distinction for unavailable** — a `status=unavailable` JSON
  (scripts dir missing, network down, unknown-to-engine method) prints to
  stdout and exits **0**. Only arg errors exit 1. Scripts keying on exit code
  cannot distinguish "search ran and found nothing/errored" from "success".
  (Consistent with "tool never fails" design, but the CLI is a script
  boundary — worth documenting or an `--exit-on-error`.)
- `--count` values above max are silently **capped** (9999 → 20) rather than
  rejected; below/equal 0 rejected as "must be positive integer". The tool
  clamps non-positive to default; the CLI is stricter — acceptable, but the
  cap-vs-error asymmetry is undocumented in help (help says "default 5, max 20").
- Multiple positionals are joined with spaces into one query
  (`strings.Join(positionals, " ")`) — quoting-friendly but silently
  accepts `gopi web_search foo bar` as query "foo bar". Harmless; by design.

**Tool-vs-CLI drift:** none found in the 6 method names, count defaults, or
JSON output shape. The tool description in loop.go:887 advertises "github
(repos/issues; code needs a token)" while the CLI usage example says "github
repository search" — the pinned `--type repositories` reality sits between
them (engine comment is accurate: code/issues need a token). Cosmetic.

### 2. `gopi web_fetch <url> [--max-chars N]`

**Design purpose.** The read side of the search/fetch split. Same pattern:
pure parse → `tools.WebFetch` → print JSON verbatim. Scheme validation is
deliberately deferred to the engine ("stays in tools.WebFetch, which returns
a structured error payload the CLI prints verbatim") — verified: `web_fetch
notaurl` → `{"status":"error",...,"message":"only http/https URLs are
supported"}` exit 0.

**Cleaned-text quality / boilerplate stripping.** Lives in the deployed+
repo script `plugins/websearch/scripts/web_fetch.py` (in-repo copy is in sync
with deployed — md5 SAME). Solid: regex drop of script/style/nav/header/
footer/noscript/iframe/form/etc., HTMLParser text extraction with block-tag
newlines, entity unescaping, whitespace collapse, NUL-byte binary detection,
@@HTTP marker parsing that survives single-line bodies (a regression fix with
contract test). Contract suite `testsuite/webfetch_contract_test.go` covers
geo-route, status contract, single-line-body regression, and deployed-vs-repo
drift (`TestWebFetchContract_DeployedScriptSync`).

**max-chars semantics — VERIFIED TAIL truncation, consistent.** max-chars is
applied to the CLEANED text, from the TAIL, with a marker
`…[truncated — tail omitted]` (script) — matches the help text
"tail-truncated to max_chars". Go side clamps [1, 100000] (non-positive →
default 8000); script floors at MIN_MAX_CHARS=200 and caps 100000 — so a
requested 50 chars returns ~200+, and `--max-chars 1` CLI-valid (positive)
lands at 200. Documented asymmetry between the CLI's accepted range and the
script's floor (P3).

**Security/SSRF — GOOD (best-in-class for this codebase).** Two-layer gate:
Go `isHTTPURL` (scheme + host present) then the script's `_pick_route`:
localhost/127./10./192.168./172.16-31./169.254./[::1] are hard-blocked from
the direct path (always ssh jump-host), `_DIRECT_BLOCK_RE` gates the raw
netloc (handles bracketed IPv6 correctly). Direct-fetch whitelist is
env-configurable (WEB_FETCH_DIRECT_DOMAINS) with suffix matching
(`evil-bigmodel.cn` cannot pass). URL is base64-wrapped through ssh — no
shell injection. Geo-direct fallback deliberately does NOT silently fall
back to ssh (geo-correctness contract). **Note:** SSRF protection assumes the
ssh jump host itself is not on the internal network — remote curl can still
reach private ranges from remotebox's vantage. The gate protects the LOCAL
host only. Documented in-code; worth a comment in the report as an accepted
residual risk.

**Rule 2:** 8000/100000 duplicated CLI↔tools with "mirrors" comment — same
P2 as web_search (see above). `webSearchScriptTimeout` (200s) is a `var`
(shrinkable in hermetic tests) — good pattern, but note the Go side passes
the FULL 200s as the script's --max-time budget while the script default is
30s capped 120s: the Go-side ssh_timeout uses the arg, so a pathological URL
can hold a Go goroutine ~200s. Acceptable last-resort bound (named const).

### 3. `gopi code-search <symbol> [--root DIR]`

**Design purpose.** Expose `code_search` with a --root the tool itself lacks.
CLI scope EXACTLY mirrors the agent loop's `codeSearchScope()` (loop.go:3575):
root "." default, `tools.CodeSearchGlobs`, excludes `.git/** vendor/**`,
MaxResult 50 (`maxSearchResults`). `--root` is the only CLI-only surface.
Greppable `name file:line kind signature` output via
`tools.FormatCodeSymbolHits` — verified live (relative --root works:
`clampWebSearchCount  websearch.go:191  func ...`).

**Relative --root — FIXED, with a documented gap.** The backend bug (ensureTags
composes `filepath.Join(root, ".gopi-tags")` and runs ctags with cmd.Dir=root,
so a relative root double-composes `<root>/<root>/.gopi-tags`) is escaped in
the CLI by `filepath.Abs(root)` before dispatch (cli_code_search.go:127-137,
from a8799d8). Verified live. **Defense-in-depth gap (P2):** ensureTags itself
never Abs-es; any FUTURE relative-root caller of tools.CodeSearch re-breaks it.
The agent loop always passes "." so it never bites — but this is a latent
backend trap with only the CLI patched around it. Also: `--root` with no value
silently resolves to "." (memoryFlagSet treats missing value as empty) and a
failed Abs silently keeps the raw root.

**Index staleness — GOOD.** ensureTags regenerates when `.gopi-tags` is
missing or older than the newest indexed source file (newestSourceFile walk,
vendor/.git/hidden-dir pruned). R6 added a cached staleness verdict (mtime
guard + short TTL) so repeat lookups skip the full walk. Regeneration is
lazy (first lookup / stale trigger). On a clean ctags miss, codeSearchImpl
falls back to ripgrep over the SAME scope, hits marked kind "reference"
(occurrences, not definitions) — documented in help. Miss + nil error =
symbol exists nowhere → CLI prints `no symbol found: <symbol>` + exit 1.

**Exit codes — VERIFIED, documented in -h.** `code-search -h` documents the
full exit contract ("0 at least one hit; 1 symbol not found / usage error /
ctags could not be built"). Live probes: real hit → 0; miss → 1 with the
canonical message; missing symbol arg → usage + 1; ctags missing → clear
exec error + 1. This is the ONLY one of the three verbs with a proper
script-facing exit contract — web_search/web_fetch print unavailable-JSON
with exit 0 instead (see P2 below).

**Output greppability — GOOD.** `file:line` always present for definitions;
`dropped > 0` appends "… plus N more matches (capped at 50)" — no silent
truncation (R3-8, cap raised 20→50 after A/B A14). Code-search's ripgrep
fallback output uses the same FormatCodeSymbolHits, so file:line is stable
across both paths.

**Missing --root on the tool itself (P2, design).** code_search tool schema
(loop.go:858) has no root/scope — it always searches cwd. The CLI adds
--root as its one extension. The slash command `/code-search` and `gopi
code-search` both exist (commands.go:515) sharing the backend; three call
sites each re-derive the scope (loop.go codeSearchScope, cli_code_search.go
inline, commands.go slashSearchRoot which reads GOPI_CODE_SEARCH_ROOT env).
Scope is duplicated 3× (a Rule-2 smell): root default ".", same globs,
MaxResult 50 in three places. `codeSearchMaxResults` CLI const mirrors
`maxSearchResults` agent const with a comment.

**Index-in-repo pollution (P3).** ctags writes `.gopi-tags` INTO the search
root (repo) on first lookup; `git status` will show it untracked and a
subsequent `git add -A` (forbidden by Rule 8, but a human could) would
commit a machine-generated file. No .gitignore entry observed in-repo for
`.gopi-tags` (verify: repo root .gitignore). The tool writes it for the
agent's cwd too. Suggest: default tags path under a cache dir (e.g.
$XDG_CACHE_HOME/gopi) or a documented .gitignore entry.

---

## Cross-cutting

- **Rule 1 (magic numbers): PASS.** Every timeout/size/cap in the reviewed
  files is a named const (webSearchDefaultCount/MaxCount, ResultCaps,
  MergeTimeout, ScriptTimeout, DefaultMaxChars, MaxChars, maxSearchResults,
  codeSearchMaxResults, buildSnippet's 100, script constants with Rule-1
  comment headers). No inline literals found in the three CLI files or
  websearch.go/search.go/symbol.go review windows.
- **Rule 2 (single source of truth): 3 partial violations** — the 5/20 and
  8000/100000 default/cap pairs are duplicated CLI↔tools behind "mirrors"
  comments; code-search scope (root/globs/MaxResult 50) is re-derived in
  three places (agent loop, CLI, slash command). None has a parity test that
  would catch drift.
- **Method-list parity (web_search): PASS** — 6 methods consistent across
  CLI map, tool enum, engine map, error strings, help text, usage tests.
- **Repo-vs-deployed script sync: PARTIAL FAIL (P1).** Only 5 of the 8
  deployed plugin scripts are mirrored in-repo: arxiv/google/github/
  web_fetch/wiki_search.py SAME md5. **bing_search.py, deepseek_ask.py and
  merge_results.py exist ONLY in ~/.gopi/plugins/websearch/scripts** — the
  merged default (bing+google+arxiv) and the deepseek method DEPEND on three
  unversioned scripts. A fresh checkout of this repo + `gopi web_search
  "q"` (merged) or `--method deepseek`/`--method bing` fails with
  status=unavailable unless the operator separately deploys those scripts.
  reports/archive/2026-08/WEBSEARCH-AUDIT-REPORT.md even documents "scripts live in ~/.gopi … only
  arxiv + github mirrored" — stale: web_fetch/wiki were since mirrored and
  the sync test only guards web_fetch. bing/deepseek/merge_results need
  mirroring (or an explicit documented deploy step), and the sync test should
  cover all scripts.
- **Doc staleness: P2.** reports/archive/2026-08/WEBSEARCH-CLI-REPORT.md (now reports/archive/2026-08/) usage examples
  list only 5 methods (pre-wiki) and the header design says nothing about
  web_fetch; reports/archive/2026-08/WEBSEARCH-AUDIT-REPORT.md says "only arxiv+github mirrored";
  both predate 56f403f/683e88f. GOPI.md lists the tools but not the CLI
  verbs (that's in help.go — fine). Suggest a doc touch when the script
  mirroring lands.
- **Error handling: GOOD overall.** Engine never crashes (every failure →
  status=unavailable JSON with message); CLI arg errors → stderr + usage +
  exit 1. The gap is the CLI exit-code contract for fetch/search failures
  (see P2 below).
- **JSON consistency: GOOD.** Both tools return the same unified-JSON shape
  as the tool_use calls, printed verbatim; renderWebSearch/Fetch have
  never-crash fallbacks.
- **Security: web_fetch SSRF gate is the strongest in the codebase** (see
  per-verb notes; residual risk is the jump host's own reach). web_search
  runs only bundled plugin scripts (no user input reaches a shell).
  code-search executes `ctags`/`rg` on a user-supplied --root path — the
  symbol arg is passed as a ctags binary-search key and rg query argument
  (not shell), so no injection; --root is only a directory path.

---

## Prioritized findings

### P1
1. **Three plugin scripts unversioned** — bing_search.py, deepseek_ask.py,
   merge_results.py live only in ~/.gopi. `gopi web_search "<q>"` (merged
   default) and `--method bing|deepseek` fail on any fresh environment; the
   merged path is the tool's default. Mirror all scripts into
   plugins/websearch/scripts/ and extend TestWebFetchContract_DeployedScriptSync
   (or a sibling) to cover every deployed script, not just web_fetch.

### P2
2. **web_search/web_fetch have no script-facing failure exit** — fetch/search
   failures (network down, unavailable, HTTP error) print JSON with exit 0;
   only arg errors exit 1. code-search documents a full exit contract.
   Either document the JSON-status contract for scripts or add
   `--exit-on-error`/exit-2 on status=unavailable|error.
3. **ensureTags relative-root trap is only patched in the CLI** — the Abs
   fix lives in runCodeSearchSubcommand; any future relative-root caller of
   tools.CodeSearch hits the `<root>/<root>/.gopi-tags` double-compose. Fix
   in the backend (Abs inside ensureTags) and keep the CLI guard as defense.
4. **Rule 2: default/cap pairs duplicated** — 5/20 (web_search) and
   8000/100000 (web_fetch) exist as CLI consts + tools consts behind
   "mirrors" comments, and code-search scope (root/globs/MaxResult) is
   re-derived in 3 sites (loop.go / cli_code_search.go / commands.go slash
   variant reads GOPI_CODE_SEARCH_ROOT). No parity test would catch drift.
5. **Stale docs** — reports/archive/2026-08/WEBSEARCH-CLI-REPORT.md (5 methods, pre-wiki/pre-fetch)
   and reports/archive/2026-08/WEBSEARCH-AUDIT-REPORT.md ("only arxiv+github mirrored") contradict
   the current 6-method + 5-mirrored reality.

### P3
6. **max-chars floor asymmetry** — CLI accepts `--max-chars 1..100000` but
   the script floors at 200; help should state the effective floor.
7. **`.gopi-tags` written into the search root** — machine-generated index
   lands in the repo on first lookup; consider a cache-dir location or
   .gitignore entry.
8. **Cosmetic** — `--count` cap-vs-reject asymmetry undocumented; tool
   description "github (repos/issues…)" vs pinned `--type repositories`
   wording; multiple positionals silently joined into one query.

---

## Verification

- Build: `go build ./...` OK (binary at /tmp/gopi-review).
- Help: all three `-h` outputs read; exit contracts and flags documented.
- Runtime probes: code-search hit → exit 0 greppable output; miss → "no
  symbol found" exit 1; bad --method / missing query / bad --max-chars →
  stderr + usage + exit 1; web_fetch non-URL → structured JSON error.
- Tests: `go test ./cmd/agent -run 'WebSearch|WebFetch|CodeSearch'` PASS;
  `go test ./internal/tools -run 'WebSearch|WebFetch|CodeSearch'` PASS.
- Script sync: md5 comparison repo-vs-deployed (5 SAME, 3 repo-missing).
- Commit: `docs(review): web_search + web_fetch + code-search code review`
  (see git log).
