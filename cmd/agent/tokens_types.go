// The types of `gopi tool tokens`.
package main

import "encoding/json"

// requestBody is one request as it appears in a raw log, reduced to the parts
// that carry tokens. Unknown fields are ignored on purpose: rejecting an
// unexpected field would make the tool useless on a log written by a newer
// client, and the question here is a count, not a schema check.
type requestBody struct {
	Model           string            `json:"model"`
	System          json.RawMessage   `json:"system"`
	Messages        []json.RawMessage `json:"messages"`
	Tools           json.RawMessage   `json:"tools"`
	ReasoningEffort json.RawMessage   `json:"reasoning_effort"`
	Thinking        struct {
		Type string `json:"type"`
	} `json:"thinking"`
}

// promptUsage is the prompt-side total the server reported for one call. The
// three fields are summed the way gopi itself sums them
// (internal/deepseek/claude_client.go, PromptSideTokens): the endpoint is
// Anthropic-compatible, so a cached prompt arrives split across input,
// cache-creation and cache-read.
type promptUsage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// prompt is the one definition of "the prompt side" for this tool.
func (u promptUsage) prompt() int {
	return u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

// tokenRow is one line of the report: what we counted, what the server counted,
// and the request's shape (messages and tool definitions) — the two variables
// that move the number.
//
// IT IS NOT CALLED `result`. That was its name until an unrelated package-level
// type in cmd/agent's tests took it (cli_tasks_sync_surface_test.go:287), and the
// collision did not fail in the file that caused it: it failed the WHOLE package's
// test binary with `result redeclared in this block`, so every test in cmd/agent —
// this tool's included — could not even compile. A name shaped like "the row a
// report prints" says what the type is and cannot collide with a generic harness
// word (Rule 1's spirit: name the value, not the shape of the moment).
type tokenRow struct {
	line   int
	msgs   int
	tools  int
	ours   int
	server int
}
