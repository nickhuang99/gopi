package agent

import (
	"fmt"
	"sort"

	"github.com/nick/gopi/internal/toolnames"
)

// editArgRefusalKeysLimit caps how many received argument NAMES a refusal
// quotes: the point is to identify what the caller sent, not to echo its
// payload. A refusal that dumps every key of a large call costs the model the
// same tokens as the call it is correcting (Rule 1: a named limit, not a
// literal at the call site).
const editArgRefusalKeysLimit = 8

// editEmptyReplacementRefusal is the message returned when an `edit` call
// arrives with an empty replacement.
//
// WHY AN EMPTY REPLACEMENT IS REFUSED RATHER THAN PERFORMED — measured
// 2026-10-04 (task 01a103c2-36e2-7808-bc42-d652762c4691, host zbook). One
// session's raw log recorded 17 `edit` tool_use blocks; 6 of them carried
// their replacement under the key `newText` instead of `newStr`. The
// dispatcher read the absent `newStr` as "" and called
// tools.Edit(path, oldStr, "", …) — which is a DELETION, not an edit — and
// answered "Successfully edited <path>". The matched line vanished, the
// replacement never landed, and nothing anywhere said so: the only detector
// was a standards gate that happened to cover the file. Rule 15's worst shape.
//
// The refusal does three things the old success could not: it says the call
// was refused, it quotes the argument NAMES the caller actually sent (so a
// misnomer is visible as itself), and it names the correct key. An empty
// replacement cannot be what the caller meant — the wire schema's only
// required replacement parameter is `newStr`, and a caller that wants the
// matched text GONE rewrites the surrounding content with `write`.
func editEmptyReplacementRefusal(path string, args map[string]interface{}) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > editArgRefusalKeysLimit {
		keys = keys[:editArgRefusalKeysLimit]
	}

	msg := fmt.Sprintf(
		"Error: edit refused for %s: the replacement is EMPTY, and an empty replacement is not an edit — it DELETES the text oldStr matched (the file shrinks and the call looks like it worked). "+
			"Re-issue the call with %s=<the replacement text>. Argument names received: %v.",
		path, toolnames.ToolArgNewStr, keys)

	// The measured misnomer gets its own sentence, named from the constant that
	// exists to name it: a model that typed `newText` is told WHAT it typed and
	// what the schema key is, instead of being left to guess which of several
	// spellings the tool wanted.
	if sent, ok := args[toolnames.ToolArgNewTextMisspelling].(string); ok && sent != "" {
		msg += fmt.Sprintf(" This call sent its replacement under %q — that is a known misnomer; the schema's key is %q.",
			toolnames.ToolArgNewTextMisspelling, toolnames.ToolArgNewStr)
	}
	return msg
}
