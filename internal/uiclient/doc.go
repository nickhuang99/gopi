// Package uiclient is the CLIENT half of `gopi ui` — the Bubble Tea terminal
// chat surface (POC #2, the comparison twin of the Links carrier).
//
// It is a pure CONSUMER of an already-running engine: `gopi web`'s
// POST /api/chat endpoint, which streams
//
//	event: queued   (this turn waits on the in-flight one)
//	event: delta    {"text":"…"}                     one model text fragment
//	event: tool     {"kind","name","summary"}        live tool call / result
//	event: command  {"command","output"|"error"}     a locally-run /slash command
//	event: error    {"error":…}                      carrier-level failure
//	event: done     {"result":…,"error":…}           end of the turn
//
// over one SSE response. The conversation is SERVER-held (s.history), so this
// client ships only the new message and therefore needs no token as long as it
// talks to a loopback bind — the gate that a LAN bind mints is never weakened
// or bypassed here (GOPI_WEB_TOKEN / webTokenGate stay untouched).
//
// WHY A CLIENT, NOT A SECOND ENGINE: the model call, the tool loop, the slash
// table, the session JSONL and the task-ledger row all stay where they are
// (cmd/agent/cli_web.go handleChat). This package adds a VIEW: it decodes the
// frames and renders them, and it holds no conversation state of its own.
//
// The wire vocabulary below is MIRRORED from cmd/agent/cli_web.go
// (webEventDelta …) and internal/agent.ToolEvent. A mirror drifts unless it is
// pinned, so cmd/agent/cli_ui_wire_guard_test.go asserts every constant here
// equals the server's own — renaming a frame on either side fails the build
// (Rule 13/14: one definition, checked).
package uiclient
