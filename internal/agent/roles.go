package agent

// The three wire roles a conversation row can carry. They are the values the
// provider protocol spells, and every reader in this package compares against
// them rather than typing the literal (Rule 1/13).
//
// THEY LIVE HERE, not beside any one mechanism, because more than one does:
// the loop's fallback-answer scan, the resume note splitter and the context
// builders all ask "is this the assistant's row?" — a definition inside a
// single mechanism would make deletion of that mechanism (Rule 43) break the
// callers it never owned.
const (
	roleUserMessage      = "user"
	roleAssistantMessage = "assistant"
	roleToolMessage      = "tool"
)
