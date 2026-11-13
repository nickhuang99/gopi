// Package-private vocabulary the replay pipeline SHARES with its verifier.
//
// WHY THESE ARE HERE AND NOT IN replay.go. They are the renderer's own numbers
// and names, and a CHECKER must reproduce them exactly or it checks against a
// copy that has already drifted: the activity row's digest cap and separator,
// and the order the digest reads a call's arguments in. They used to be
// unexported constants inside replay.go — the self-drawing replayer this epic
// deletes — and a checker in another package cannot import an unexported
// constant, so a port would have had to re-type the values (Rule 14: never
// mirror, never bare-write). Moving them to a file that survives the deletion,
// exported, keeps ONE definition and lets internal/film reference it.
//
// Rule 13's discipline applies to the move: the values are not copied, they are
// RELOCATED, and replay.go now aliases them.
package replay

// ToolSummaryRunes caps a tool call's digest on its activity row: how many runes
// of the call's first informative string argument the row carries.
const ToolSummaryRunes = 58

// ArgSep joins a tool call's arguments into its activity-row digest.
const ArgSep = " "

// DigestKeyOrder is the order digestArgs reads a call's arguments in: the first
// string-valued field it finds, in this order, is the digest. It is the
// recording format's own vocabulary (an Anthropic tool_use block's arguments),
// which is why the list is here rather than at the scan site.
var DigestKeyOrder = []string{"command", "cmd", "path", "file_path", "query", "pattern", "url", "name"}
