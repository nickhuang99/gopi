<!-- doc-class: historical -->
# CLI Code Review: `gopi proxy` + `gopi version`

Date: 2026-09-02 · Epic child task #10 (t-1788305239) · Branch: `review/cli-proxy-version`
Scope: `gopi proxy` (cli_proxy.go + internal/proxy/) and `gopi version` (cli_version.go).
Review method: run the real subcommand → read source → engine cross-check → docs →
git history/git_blame → prioritized findings (P0/P1/P2). Docs-only review — no code changes.

---

## 1. `gopi version` — design purpose, findings

### Design purpose
`gopi version` / `--version` / `-v` print the build version and exit 0. It is the
first check in main()'s dispatch chain (before config resolution), so a version
probe can never start an agent session. This was the explicit bug-030 fix
(0a00728, devdoc/bug030-cli-shortcut.md): a pre-fix `gopi version` fell through to
the prompt path and spun up a whole agent run (session JSONL shell + a stuck
in_progress task-ledger row). The design is correct and well-tested.

Version string resolution (single source of truth — **Rule 2 satisfied**):
1. `var version = "dev"` — stamped at build time via `-ldflags "-X main.version=<tag>"`.
2. Else fall back to the VCS revision embedded by the toolchain
   (`debug.ReadBuildInfo()` → `vcs.revision`), short revision (first 12 chars) with
   `-dirty` suffix when the tree had local modifications.
3. Else plain `"dev"` (no VCS info, e.g. tarball build).

`version` is defined exactly once (cli_version.go:15); no other file hardcodes or
re-derives it. `-ldflags` stamping is the only override path. **No drift risk.**

### Parity
`gopi version` ≡ `--version` ≡ `-v` — all three hit the same switch case
(verified by test `TestVersionSubcommand` + manual run; all print the same value).
`gopi version -h` / `--help` / `help` print versionUsage and exit 0 (a trailing
`help` keyword is honored, unlike some other subcommands).

### Issues

**P1-1. `-dirty` suffix can lie about the source.** `buildVersion()` appends
`-dirty` when `vcs.modified=true` — the embedded VCS info is captured at *build*
time, but the string also reflects it only for the build tree. This is standard Go
behavior and generally accurate, so the label itself is fine. The real gap: the
12-char truncation has no `-dirty` awareness edge — a dirty 12-char rev prints
`deadbeef1234-dirty`, which is unambiguous. **Low severity, cosmetic.** The actual
issue is that a release build *with* ldflags stamping reports only the tag and
**hides the VCS revision** — you cannot tell which commit a tagged binary came
from. Suggest: append the short revision alongside a tag when stamped
(e.g. `v1.2.3 (deadbeef)`) — but only if release tooling wants it; current
behavior (tag wins, no rev) is a deliberate, documented choice. **P2.**

**P1-2 (minor). Magic numbers.** cli_version.go:71-72 truncate rev to 12 chars
(`len(rev) > 12`) — an inline literal, not a named constant (Rule 1). Cosmetic.

### Verdict
`gopi version` is clean, minimal, correctly short-circuits before any session
path, is fully tested (bug-030 regression guards), and has a single source of
truth for the version string. Only nit: unnamed 12-char constant.

---

## 2. `gopi proxy` — design purpose, findings

### Design purpose
`gopi proxy --upstream openai|anthropic [options]` is a local HTTP server that
accepts the Anthropic Messages protocol (`POST /v1/messages` + `/anthropic/messages`,
stream + non-stream), translates requests to the OpenAI Chat Completions protocol,
forwards to an OpenAI-compatible upstream (GLM-5.3-Flash etc.), and translates
responses back to Anthropic SSE. This lets an unmodified Anthropic client (incl.
gopi's own DeepSeek-style client) talk to an OpenAI-only provider. Zero external
deps. Introduced 2026-08-27 (4661c41 feat → dec7990 tests → 1a860b0/65ddc94 fixes →
87b9f21 /anthropic/messages alias → e676d90 cache_read passthrough → 4ad90ab
double-dash fix). Docs: devdoc/proxy-anthropic-openai.md, devdoc/proxy-cache-fix.md.

The engine (internal/proxy/, ~2155 lines) is well-structured with thorough
comments. Request translation (request.go), non-stream response (response.go),
stream translator (stream.go), and HTTP server (server.go) are cleanly separated.
Unit + E2E test coverage is strong (13+ tests: request conversion, stream
thinking/tool-delta accumulation, tool roundtrip id preservation, error passthrough,
cache_read passthrough).

### Issues

**P0-1. `gopi proxy` is MISSING from the top-level `--help` / subcommand list.**
The `Subcommands` section of `gopi --help` (internal/config/help.go, FormatUsage)
lists every dispatchable subcommand EXCEPT `proxy`: log, memory_*, task family,
worker, web_search/web_fetch, code-search, vision, text2img, shot, checkpoint,
resume — all present; `proxy` absent. Yet `isProxySubcommand` is in the main()
dispatch chain (main.go:41) and `gopi proxy -h` works. A user who sees only
`--help` cannot discover the proxy exists. **Fix: add a `gopi proxy` line to the
help.go Subcommands section.** (Verified against HEAD-built binary: 0 matches for
"proxy" in full help text.) This is an epic-lens finding (help text drift).

**P1-2. `--upstream anthropic` mode is misleading / semantically broken.**
The anthropic mode does NOT speak Anthropic to an Anthropic-native upstream — it
still translates Anthropic→OpenAI and sends the OpenAI wire protocol to whatever
`--anthropic-base-url` points at. The only requirement is base+key (model is
optional, defaulting to the literal string `"default"`). The flag/help naming
(`--anthropic-base-url`, "Upstream Anthropic base URL") implies Anthropic-native
forwarding, which does not exist. Documented in code comments ("for a true
Anthropic-native upstream we'd forward Anthropic-to-Anthropic") and in the devdoc
"Not Done" — so it is *honest* — but a user configuring
`--upstream anthropic --anthropic-base-url https://api.anthropic.com` will get a
translated-to-OpenAI request sent to Anthropic's OpenAI-incompatible endpoint →
confusing 4xx. **At minimum: document loudly in proxyUsage help that anthropic
mode still speaks OpenAI wire (only for OpenAI-compatible endpoints reached via
a differently-named key/base).** Also the `"default"` model fallback is a magic
string that will 400 against a real Anthropic endpoint. **P1 (docs+UX) / design.**

**P1-3. Stream response does not check `sc.Err()` and does not propagate client
disconnect.** internal/proxy/server.go:138 streamResponse loops
`for sc.Scan()` on the upstream body with no `sc.Err()` check and no
`r.Context().Done()` monitoring. Upstream stream error / client disconnect →
the loop just ends on EOF, and **no terminating event** (message_stop / error) is
emitted — the Anthropic client receives a dangling SSE stream. Upstream request
uses `http.DefaultClient.Do` (no ctx), so a client cancel does not cancel the
upstream request. (Known finding #1017/#1022 — re-verified, still stands.)
**P1.** (Note: the well-formed happy path terminates correctly via
`acc.Feed`'s Finish on the `finish_reason` chunk + `[DONE]` break.)

**P1-4. `anyToolSeen` dead field.** stream.go:141 field is written
(stream.go:249) but never read. Leftover from a tool-detection design. **P2 cleanup.**

**P1-5. `tool_choice: any` is silently narrowed to OpenAI `auto`.** request.go
convertToolChoice: Anthropic `{"type":"any"}` (force tool use) maps to `"auto"`,
which lets the model choose no tool — a semantic narrowing. OpenAI has no direct
"any" equivalent, so a 1:1 mapping is impossible; but the narrowing should at
least be documented (it currently passes silently through the same case as
`auto`). **P2** (no clean fix on OpenAI wire).

**P1-6. Thinking-block stream content ordering relies on upstream order.** In
stream.go Feed, reasoning deltas open a thinking block, tool deltas open a
tool_use block, text deltas open a text block — each `beginBlock` closes whatever
was open. If an upstream interleaves (e.g. reasoning resumes after text), the
translator emits separate blocks correctly but the *Anthropic spec* requires a
thinking block to be complete (start..stop) before text begins. GLM does not
interleave, and tests pin thinking→text ordering — acceptable for the target
upstream, worth a comment. **P2.**

**P1-7. Non-stream usage: `message_start` carries no `usage`** (stream.go start()),
usage appears only in `message_delta`. Anthropic's real API puts usage on
message_start too; gopi's client (claude_client.go) reads usage from either
message_start or message_delta, so this is client-compatible. For third-party
Anthropic clients that read usage only from message_start, the count is missing.
**P2** (client-verified OK; spec-purity nit).

**P2-8. Error-body truncation magic 300.** server.go:233-234 truncates an
upstream error body to 300 bytes with an inline literal (Rule 1). Cosmetic.

**P2-9. No read/write timeouts on http.Server** (cli_proxy.go:193-195) — deliberate
for SSE idle, correctly commented. Fine as designed; noted for the record.

**P2-10. Auth/`x-api-key` passthrough.** The proxy overwrites client auth with
`Authorization: Bearer <upstream-key>` — correct for the OpenAI forward. The
Anthropic client's `x-api-key`/`anthropic-version` headers are dropped on the
forwarded request, which is right (the upstream is OpenAI). No issue.

### Verdict
`gopi proxy` is a focused, well-tested single-purpose tool. The engine quality is
high and the design (translate-to-OpenAI for an OpenAI-only upstream) is sound and
explicitly documented. The two headline findings are (a) missing from `--help`
(P0 doc drift — makes the whole subcommand undiscoverable), and (b) the
`--upstream anthropic` mode's misleading semantics (P1 UX). Engine robustness
gaps (P1-3 dangling stream on error) mirror the known #1017 findings and remain
open.

---

## 3. Cross-cutting checks

- **Rule 1 (magic numbers):** cli_version.go:12-char rev truncation + proxy
  server.go 300-byte error truncation are the only unnamed literals — both minor.
- **Rule 2 (single source of truth):** version string defined once (cli_version.go:15)
  — satisfied. Proxy default listen `127.0.0.1:8090` appears twice
  (help text line + parseProxyArgs default) — the help/impl pair can drift, but
  they live side by side in one file; low risk.
- **Tool-vs-CLI drift:** the proxy is CLI-only (no LLM tool counterpart) — N/A.
  The `version` CLI has no tool counterpart — N/A.
- **Dispatch ordering:** `isVersionSubcommand` is first in the chain — correct
  (fastest, side-effect-free). `isProxySubcommand` runs before config resolution —
  correct (proxy must not start an agent session either).
- **Docs:** devdoc proxy reports are thorough and accurate; GOPI.md does not
  mention either subcommand (proxy docs live in devdoc only). `--help` missing
  proxy is the sole doc gap.

## 4. Prioritized fix list

| Pri | Item | Where | Type |
|-----|------|-------|------|
| P0 | `proxy` missing from top-level `--help` Subcommands | internal/config/help.go | doc drift |
| P1 | `--upstream anthropic` mode misleading (still OpenAI wire; model defaults to `"default"`) — document loudly in help | cmd/agent/cli_proxy.go proxyUsage | UX/doc |
| P1 | streamResponse: no `sc.Err()` check, no client-disconnect ctx, no terminating event on stream error | internal/proxy/server.go:138 | robustness |
| P2 | `anyToolSeen` dead field | internal/proxy/stream.go:141 | cleanup |
| P2 | `tool_choice: any` → `auto` silent narrowing — document | internal/proxy/request.go | doc |
| P2 | `message_start` carries no usage (spec purity; client-compatible) | internal/proxy/stream.go | spec nit |
| P2 | magic numbers: 12-char rev truncation, 300-byte error truncation | cli_version.go / server.go | Rule 1 |
| P2 | thinking block ordering assumes non-interleaving upstream (comment) | internal/proxy/stream.go | doc |

## 5. Verification

- Build: `go build ./cmd/agent/` → OK (HEAD fc1dfc9).
- Manual: `gopi proxy -h` (usage, exit 0), `gopi version` / `--version` / `-v`
  (all print `fc1dfc9b4066`, exit 0), `gopi version -h`, error exits (bad upstream
  → 1, missing fields → 1).
- Help cross-check: `gopi --help` contains `version` but 0 occurrences of `proxy`
  (HEAD-built binary).
- Tests: `go test ./cmd/agent/ -run TestVersion` → all PASS
  (TestVersionSubcommand{version,--version,-v}, TestVersionSubcommandNoLedgerRow,
  TestVersionHelpFlag). internal/proxy suite green per devdoc; no code changed here.
- No code changes made — docs-only review.

## 6. Sources

- cmd/agent/cli_proxy.go, cli_version.go, main.go (dispatch chain)
- internal/proxy/{types,request,response,stream,server}.go
- internal/config/help.go (FormatUsage)
- devdoc/proxy-anthropic-openai.md, devdoc/proxy-cache-fix.md,
  devdoc/bug030-cli-shortcut.md, devdoc/cli-review-epic-manifest.md (task #10)
- git log/blame: cli_proxy.go 4661c41→4ad90ab; cli_version.go 0a00728 (bug030)
- Memory: #1017/#1022 prior proxy review findings (re-verified in HEAD)
