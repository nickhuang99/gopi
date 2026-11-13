// Package inputsource is the ONE place a queue message's PROVENANCE is named
// (Rule 14: a value shared across packages gets exactly ONE exported definition
// in a small leaf package, so no consumer mirrors it or types a bare literal).
//
// WHAT IT NAMES. Four faces put text into a session's queue (`theJobs.notices`,
// the single take point `TakeJobNotices`), and the queue is deliberately ONE
// queue rather than four:
//
//   - HUMAN  — the operator's own input AT THE PROMPT: a line typed at the chat
//     prompt, or a paste the editor spilled to a file. The operator is the
//     source even when the bytes came from the terminal rather than from typing.
//   - QUEUE  — a prompt the operator PARKED with `/queue <prompt>`
//     (cmd/agent/chat_queue.go) and the REPL took off its own queue at a turn
//     boundary. The operator authored it too — and that is why it is NOT filed
//     under HUMAN: what produced THIS TURN's line is the REPL's queue, not a
//     keystroke. A parked prompt becomes a turn with nobody at the prompt, so a
//     per-source reading that folded the two together could not tell an attended
//     line from a parked one, and the reference would name the wrong producer.
//   - NOTIFY — another process handing this session one message
//     (`gopi notify --session <id> --message <text>`), which lands in
//     `<session-dir>/messages/inbox.jsonl` and is merged into the same queue at
//     the round boundary (internal/tools/session_message.go).
//   - LLM    — the model typing a line for itself through the `chat_surface`
//     tool's `type` action (internal/agent/chat_surface.go), consumed at the
//     same turn seam the operator's queue is.
//
// WHY THE NAME IS A VALUE AND NOT A BOOL. A reader of a spilled payload's
// reference must be able to tell WHERE the bytes came from — "the operator sent
// this" and "another agent sent this" carry different trust and different
// answers to "who do I reply to". A boolean would collapse that, and a bare
// string at each of the four call sites would drift (the same defect Rule 1's
// string-keyword clause exists for).
//
// ZERO IMPORTS ON PURPOSE (Rule 14: the package must stay a leaf — any project
// import here would make every consumer's dependency graph depend on it).
package inputsource

// The four faces. Their VALUES are what the reference spelling carries
// (`source=human`), so a value change is a format change: search the tree for
// the constant, never for the word.
const (
	// Human is the operator AT THE PROMPT: a line typed there, a terminal paste
	// the editor spilled, or a line spilled by the chat message boundary. A
	// `/queue <prompt>` is NOT this face — see Queue below.
	Human = "human"
	// Queue is a prompt the operator PARKED for a later turn with
	// `/queue <prompt>` (cmd/agent/chat_queue.go), taken off the REPL's own
	// queue at a turn boundary. Same author as Human, different PRODUCER: the
	// turn's line came off a queue rather than out of a keystroke, and only the
	// read arm can tell the two apart (chatSealSource, cmd/agent/chat_boundary.go)
	// — the text of the two is identical.
	Queue = "queue"
	// Notify is another process delivering into this session's inbox
	// (`gopi notify --session … --message …`).
	Notify = "notify"
	// LLM is the model itself, typing a line for its own next turn through the
	// `chat_surface` tool's `type` action.
	LLM = "llm"
)

// Known reports whether s is one of the values this package defines. It is the
// guard for a value read back off a reference: an unknown spelling is reported
// by the caller rather than silently treated as one of the four.
func Known(s string) bool {
	switch s {
	case Human, Queue, Notify, LLM:
		return true
	}
	return false
}
