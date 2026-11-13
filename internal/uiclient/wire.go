package uiclient

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ChatPath is the endpoint the client POSTs one turn to. It is the SAME route
// `gopi web`'s page uses (cmd/agent/cli_web.go, mux.HandleFunc("/api/chat" …));
// declared here so the client never types the path twice, and pinned to the
// server's literal by cmd/agent/cli_ui_wire_guard_test.go.
const ChatPath = "/api/chat"

// InterruptPath is the route that abandons the SERVER's in-flight turn.
//
// WHY IT EXISTS: cancelling a turn locally cannot stop the work behind it —
// closing the SSE connection ends this side of the stream while the engine runs
// the turn to completion (cmd/agent/cli_web.go, the disconnect note on
// handleChat), and that turn holds the server's single-turn lock. The operator
// then waits behind a lock with nothing on screen accounting for the wait, which
// is the reported `gopi ui` freeze. Declared here so the surface never types the
// path twice, and pinned to the server's literal by
// cmd/agent/cli_ui_wire_guard_test.go.
const InterruptPath = "/api/chat/interrupt"

// SSE frame event names — the wire vocabulary the server writes and this
// client switches on. NEVER a bare literal at a dispatch site (Rule 1b): the
// names live here and are mirrored-checked against the server's own constants.
const (
	// EventDelta is one streamed model text fragment: {"text":"…"}.
	EventDelta = "delta"
	// EventThinking is one RAW reasoning fragment: {"text":"…"} — the live
	// "the model is thinking" stream, with NO marker bytes (the engine emits
	// the fragment unwrapped; a carrier that receives this frame is expected to
	// decorate it itself).
	//
	// It is a frame of its own rather than a field on delta because the two
	// carry different kinds of content: delta IS the answer (it is accumulated
	// into the assistant row and becomes the transcript), while a reasoning
	// fragment is a display-only aside that must never reach the assistant
	// history — an unknown frame would be refused by THIS client (unknown
	// stream frame), which is the loud failure a third field could not give.
	EventThinking = "thinking"
	// EventTool is one tool activity: {"kind","name","summary"} — `kind` is
	// UIStepCall before execution and UIStepResult after.
	EventTool = "tool"
	// EventCommand is a locally-executed slash command: {"command","output"}
	// or {"command","error"}.
	EventCommand = "command"
	// EventQueued is sent when this turn waits on an in-flight one:
	// {"placement","notice"}.
	EventQueued = "queued"
	// EventError is a carrier-level failure: {"error":"…"}.
	EventError = "error"
	// EventDone is the terminal frame of a turn: {"result":"…","error":"…"}.
	EventDone = "done"
)

// Tool activity kinds (the server's internal/agent.ToolEvent.Kind values).
const (
	// UIStepCall marks a tool call, emitted just before it executes.
	UIStepCall = "call"
	// UIStepResult marks a tool result, emitted just after it is available.
	UIStepResult = "result"
)

// Body field names of the POST /api/chat request. The server declares them in
// cmd/agent/cli_web.go (webChatRequest) and IGNORES `history` on purpose (the
// server holds the transcript); `send` is deliberately absent from the wire
// because the transcript is authoritative on the server — shipping nothing is
// the contract, not an omission (see devdoc' defect-2 note).
const (
	// FieldMessage carries the user's own words for this turn.
	FieldMessage = "message"
)

// Frame field names the server writes on the wire.
const (
	// FieldText is the `delta` frame's fragment.
	FieldText = "text"
	// FieldKind / FieldName / FieldSummary are the `tool` frame's fields.
	FieldKind    = "kind"
	FieldName    = "name"
	FieldSummary = "summary"
	// FieldPlacement / FieldNotice are the `queued` frame's fields.
	FieldPlacement = "placement"
	FieldNotice    = "notice"
	// FieldCommand carries the slash word a `command` frame answers.
	FieldCommand = "command"
	// FieldOutput / FieldTurnError are shared by `command` and `done`.
	FieldOutput    = "output"
	FieldTurnError = "error"
	// FieldResult is the `done` frame's final assistant text.
	FieldResult = "result"
)

// LoopbackHostName is the host the client addresses a loopback bind by. It is
// a constant because TWO places must agree on it: the default base URL below
// and the caller's resolution of a wildcard listen address (cmd/agent's
// uiLoopbackBaseURL) — a second spelling would let the TUI call a name the
// server did not bind.
const LoopbackHostName = "127.0.0.1"

// DefaultServerBase is the base URL the client targets when the caller passes
// none: the loopback bind `gopi ui` starts by default, on the same port
// `gopi web` defaults to (cmd/agent/cli_web.go webDefaultAddrValue). Loopback
// is the whole reason no token is needed, so it is the default and not a
// fallback.
const DefaultServerBase = "http://" + LoopbackHostName + ":8090"

// Frame is one decoded SSE frame: the event name plus its raw data payload.
// The payload is kept RAW (never auto-substituted into a text field) — a frame
// this client does not understand is reported as a protocol error by the
// renderer, never printed as assistant prose.
type Frame struct {
	Event string
	Data  string
}

// ToolActivity is the decoded body of an `event: tool` frame.
type ToolActivity struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

// TurnEnd is the decoded body of an `event: done` frame. Error is non-empty
// when the turn failed on the server; a user-initiated cancel is NOT an error
// (the server streams its own end; the client treats a cancelled context as a
// normal end — see Stream).
type TurnEnd struct {
	Result string `json:"result"`
	Error  string `json:"error"`
}

// DeltaText is the decoded body of an `event: delta` frame.
type DeltaText struct {
	Text string `json:"text"`
}

// ThinkingText is the decoded body of an `event: thinking` frame: one RAW
// reasoning fragment, exactly as the engine received it from the model (no
// marker, no SGR). It is a type of its own so the renderer cannot confuse it
// with the answer — the two are different entry kinds in the transcript.
type ThinkingText struct {
	Text string `json:"text"`
}

// CommandResult is the decoded body of an `event: command` frame.
type CommandResult struct {
	Command string `json:"command"`
	Output  string `json:"output"`
	Error   string `json:"error"`
}

// QueuedNotice is the decoded body of an `event: queued` frame.
type QueuedNotice struct {
	Placement string `json:"placement"`
	Notice    string `json:"notice"`
}

// UnknownFrameEventError reports an SSE frame whose event name this client has
// no renderer for. It is returned (never swallowed) so a protocol change
// surfaces as a visible error instead of silence (Rule 15) — and so the raw
// payload can never be mistaken for model prose.
type UnknownFrameEventError struct {
	Event string
	Data  string
}

func (e *UnknownFrameEventError) Error() string {
	return fmt.Sprintf("unknown stream frame %q (payload %q) — the server speaks a newer wire than this client", e.Event, truncateForError(e.Data))
}

// MalformedFrameError reports a frame whose event IS known but whose payload
// does not decode into the expected shape. The raw bytes are carried in the
// message; they are never rendered as text.
type MalformedFrameError struct {
	Event string
	Data  string
	Err   error
}

func (e *MalformedFrameError) Error() string {
	return fmt.Sprintf("malformed %q frame (%v): %q", e.Event, e.Err, truncateForError(e.Data))
}

// FrameError is the carrier-level failure an `event: error` frame reports.
type FrameError struct {
	// Message is the server's own error text.
	Message string
}

func (e *FrameError) Error() string { return e.Message }

// Unwrap exposes the JSON error (errors.Is/As keep working — Rule 16).
func (e *MalformedFrameError) Unwrap() error { return e.Err }

// ErrStreamClosed reports a stream that ended before the server's terminal
// `done` frame arrived (a dropped connection, not a finished turn).
var ErrStreamClosed = errors.New("stream ended before the turn's done frame")

// frameErrorPayload is the shape of an `event: error` body.
type frameErrorPayload struct {
	Error string `json:"error"`
}

// decodeFrame turns one raw frame into its typed value. A known event with an
// undecodable payload is a MalformedFrameError; an unknown event is an
// UnknownFrameEventError. NEITHER falls back to returning the payload as text:
// that defect (a raw `data:` line rendered as assistant prose) is the one the
// sketch's extractText shipped, and it is closed structurally here — this
// function has no code path that returns a string.
func decodeFrame(f Frame) (any, error) {
	switch f.Event {
	case EventDelta:
		var v DeltaText
		if err := json.Unmarshal([]byte(f.Data), &v); err != nil {
			return nil, &MalformedFrameError{Event: f.Event, Data: f.Data, Err: err}
		}
		return v, nil
	case EventTool:
		var v ToolActivity
		if err := json.Unmarshal([]byte(f.Data), &v); err != nil {
			return nil, &MalformedFrameError{Event: f.Event, Data: f.Data, Err: err}
		}
		return v, nil
	case EventThinking:
		var v ThinkingText
		if err := json.Unmarshal([]byte(f.Data), &v); err != nil {
			return nil, &MalformedFrameError{Event: f.Event, Data: f.Data, Err: err}
		}
		return v, nil
	case EventCommand:
		var v CommandResult
		if err := json.Unmarshal([]byte(f.Data), &v); err != nil {
			return nil, &MalformedFrameError{Event: f.Event, Data: f.Data, Err: err}
		}
		return v, nil
	case EventQueued:
		var v QueuedNotice
		if err := json.Unmarshal([]byte(f.Data), &v); err != nil {
			return nil, &MalformedFrameError{Event: f.Event, Data: f.Data, Err: err}
		}
		return v, nil
	case EventError:
		var v frameErrorPayload
		if err := json.Unmarshal([]byte(f.Data), &v); err != nil {
			return nil, &MalformedFrameError{Event: f.Event, Data: f.Data, Err: err}
		}
		return &FrameError{Message: v.Error}, nil
	case EventDone:
		var v TurnEnd
		if err := json.Unmarshal([]byte(f.Data), &v); err != nil {
			return nil, &MalformedFrameError{Event: f.Event, Data: f.Data, Err: err}
		}
		return v, nil
	default:
		return nil, &UnknownFrameEventError{Event: f.Event, Data: f.Data}
	}
}

// maxErrorPayloadBytes bounds how much of an offending payload an error
// message quotes, so a pathological frame cannot flood the status line.
const maxErrorPayloadBytes = 120

// truncateForError clamps a payload to the error-message budget.
func truncateForError(s string) string {
	if len(s) <= maxErrorPayloadBytes {
		return s
	}
	return s[:maxErrorPayloadBytes] + "…"
}
