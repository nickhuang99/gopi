// wire_vocab.go — the Claude Messages wire vocabulary as EXPORTED constants.
//
// WHY IT IS A PACKAGE-LEVEL EXPORT AND NOT A LITERAL AT EACH CALL SITE. These
// values are compared and switched on by code on BOTH sides of the package
// boundary: the pairing walker inside internal/deepseek, and — before its
// retirement — the carriers in cmd/agent that read a request they did not build
// (the per-turn request summary). Typing "user" or "text" in a second package is
// the bare string keyword Rule 1b forbids — the value could drift from the one
// the wire actually carries, and nothing would notice.
//
// The package's own private spellings (wireRoleUser, wireBlockTypeText, …)
// REFERENCE these, so there is exactly one definition of each value (Rules
// 13/14).
package deepseek

const (
	// RoleUser is the message role carrying the person's input — and, on the
	// wire, also the tool-result exchange (see BlockTypeToolResult).
	RoleUser = "user"
	// RoleAssistant is the message role carrying the model's own output.
	RoleAssistant = "assistant"
	// BlockTypeText is a text content block: the only block type that carries
	// what a person actually typed.
	BlockTypeText = "text"
	// BlockTypeThinking is a reasoning content block.
	BlockTypeThinking = "thinking"
	// BlockTypeToolUse is a tool call issued by the model.
	BlockTypeToolUse = "tool_use"
	// BlockTypeToolResult is a tool call's answer, which rides a RoleUser
	// message — the reason a reader looking for "the user's message" must also
	// require a text block.
	BlockTypeToolResult = "tool_result"
	// BlockTypeImage is an image content block.
	BlockTypeImage = "image"
)
