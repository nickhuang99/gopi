<!-- doc-class: historical -->
# Review: What Is "request" in `gopi raw` / `gopi log`? — Definition + Fidelity Verdict

Source question: `~/work/claude/nick/what_is_request.txt`
Session under investigation: `c6985118-f087-4034-ad6f-cedb50524cbe` (`gopi raw c6985118 1 request`)
Branch: `review/what-is-request`

## TL;DR

1. **A "request" in `gopi raw` IS the HTTP request body — byte for byte.** Each
   `→ REQUEST` marker in the raw log is followed by exactly one JSON line: the
   request body that was POSTed to the Anthropic-compatible Messages API
   (`/v1/messages`). `gopi raw <id> <round> request` prints `r.RequestRaw` —
   the **exact logged bytes**, no re-serialization, no trimming (verified:
   `RawRoundRequest` returns `string(r.RequestRaw)` verbatim; output cap 64 MiB
   is rune-safe with a stderr-only note).

2. **The user's input IS inside the round-1 request.** Live evidence from
   `c6985118`:
   - Body top keys: `model, max_tokens, system, messages, tools, stream,
     thinking, reasoning_effort`
   - `system` = 2 blocks (GoPi system prompt + task-workflow rules)
   - `messages` = `[{role:user, content:[{type:text, text:"[EPIC GOAL]…"}]}]`
     — the user's epic/task text is `messages[0].content[0].text`
   - `tools` = 23 tool schemas
   - So the user's prompt is NOT excluded from the raw request; it lives in
     the `messages[]` array, which is the standard Anthropic Messages API
     shape.

3. **The confusion comes from TWO different "request" notions sharing one
   name:**
   - `gopi raw <id> <round> request` → whole request body (system + messages
     + tools). User prompt **included**.
   - `gopi log <id> --request` → **system prompt only**. `runRequest` →
     `ParseFirstRequest` → `FormatRequestSystem` extracts only the `system`
     blocks (modern shape) / role=system message (legacy shape). The flag is
     documented as "System prompt sent with the session's first API request"
     (`cli_log.go:28`). This is the "why is my prompt not included" case.

4. **Definition should follow the HTTP protocol** (the user's own demand), and
   it does at the raw layer: HTTP request = one POST body = `{model, system,
   messages, tools, stream, …}`. System prompt and user turn are **separate
   protocol fields** with different roles — "user request" ≠ "system prompt".
   The user turn is a `user` message inside `messages[]`; the system prompt is
   the top-level `system` field. Both together form the one HTTP request.

## How to retrieve the user's prompt (answers to the file's questions)

| Question | Answer |
|---|---|
| Why isn't my input/prompt in the 1st request? | It IS — in `gopi raw <id> 1 request` under `messages[0]`. What excludes it is `gopi log --request`, which is system-prompt-scoped by design. |
| Is user request == "prompt"? | No. Protocol roles are distinct: `system` (instructions) vs `messages[role=user]` (the user turn). |
| How to retrieve the user input? | `gopi raw <id> 1 request \| jq .messages` — or full body `gopi log <id> --request --json` (contains system AND messages). |
| user input + system prompt together = request? | At the HTTP layer yes — one POST body carries both. `gopi raw <id> 1 request` shows that whole body. |

## Fidelity verdict: is raw trustworthy? — YES (with one naming trap)

- `gopi raw` never reinterprets: `RawRoundRequest` returns the logged bytes
  verbatim; the request body IS JSON so structure questions go to `jq`; no
  `--json/--text/--format` flags exist by design (`cli_raw.go` rejects them).
  Diagnostics go to stderr, stdout stays pure bytes.
- Round numbering = raw log's own ordinals (1-based, every HTTP send counts —
  retries included, which the session JSONL hides; guard probes are annotated
  rounds of their own). 187 `→ REQUEST` markers in this session vs 186 JSONL
  request events — raw shows one extra HTTP send (a retry/attempt), which is
  exactly what "system debugging" wants.
- **The trap**: `gopi log --request` uses the word "request" for a *filtered*
  system-prompt view. A user expecting "the request" (HTTP semantics) gets
  only `system`, silently. That is the "arbitrarily trim/adjust by its free
  will" impression. Root cause: flag name `--request` + doc "system prompt"
  mismatch the protocol meaning of request.

## Findings (severity)

- **P1 — naming/documentation trap**: `gopi log <id> --request` prints the
  system prompt only, contradicting the protocol meaning of "request". Users
  of `gopi raw <id> N request` (whole body) switching to `gopi log --request`
  lose the user messages without warning. Fix options:
  (a) rename the log flag semantics in help text to state clearly it is
  "system prompt (filtered view)" — the first usage line should say
  `System prompt of the first API request — NOT the full request`;
  (b) add a `--request --json` hint in the default (non-json) output footer
  pointing at `gopi raw <id> 1 request | jq .messages` when user messages
  exist. Docs-only change, low risk.
- **P2 — `log` lacks a "full request body incl. user messages" path name**:
  `gopi log --request --json` DOES print the full body (system+messages) via
  `FormatRequestJSON` (`p.Raw`), but the non-json default hides everything
  except system. The asymmetry between `--request` (system-only) and
  `--request --json` (full body) is undocumented.
- **OK as-is (confirmed correct)**: raw byte-exactness, round numbering
  including retries/guard sends, stdout purity, 64 MiB cap handling, no
  format flags.

## Files reviewed

- `cmd/agent/cli_raw.go` — parse + dispatch; `request|response` selector
  prints `RawRoundRequest` verbatim.
- `internal/gopilog/rawview.go` — `RawRoundRequest`, `RawRoundResponseSpan`,
  cap.
- `internal/gopilog/rounds.go` — `ParseHTTPRounds`: `→ REQUEST` marker →
  single following JSON line = request body (`RequestRaw`). Marker matching
  is whole-line (never substring), so marker text inside a body never
  misfires.
- `internal/gopilog/rawlog.go` — `RequestPayload` (model/system/messages/Raw),
  `ParseFirstRequest` (first request only), `extractSystemPrompts` (the
  system-only filter), `FormatRequestSystem`/`FormatRequestJSON`.
- `cmd/agent/cli_log.go` — `runRequest` (system-prompt path), `--request`
  doc line 28.
- Live session `c6985118` raw log `gopi-raw-20260906-185706.log` (33 MB,
  187 rounds) + its JSONL (186+ request events).

## Verification commands used

```
gopi raw c6985118 1 request | jq '{model, system_blocks:(.system|length),
  messages:(.messages|length), user_text:.messages[0].content[0].text[:60]}'
gopi log c6985118 --request        # → system prompt only (2 blocks)
gopi log c6985118 --request --json # → full body incl. messages[]
```

Live run of the first two confirmed: raw request body contains `messages[0]`
with `[EPIC GOAL]…` user text; `gopi log --request` prints only the 2 system
blocks.

## Suggested follow-up (if approved)

Docs-only: amend `cli_log.go` `--request` usage text to disambiguate
("system prompt of the first request — use `gopi raw <id> 1 request` for the
full HTTP body incl. user messages"), and note the `--json` full-body
asymmetry. No code behavior change recommended — the raw layer is already
protocol-faithful.
