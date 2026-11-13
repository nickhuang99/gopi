package main

// testlines_test.go — the ONE line-splitting helper the CLI tests share.
//
// Harvested out of merge_run.go (Rule 43) when the one-shot merge verb was
// deleted (epic merge-verb-removal, item 3): the helper never belonged to that
// mechanism — its only reader is cli_shot_test.go's stderr assertion, and it
// spent its life as production code that only a test called. It moves here
// unchanged, beside the tests that use it, so the deletion of the merge face
// takes nothing a surviving test needs.

import "strings"

// nonEmptyLines splits s into its non-blank lines, each trimmed. A blank line
// is not "an empty line in the output" for these assertions: it is rendering
// noise, and counting it would let a missing line hide behind a trailing
// newline.
func nonEmptyLines(s string) []string {
	out := make([]string, 0, 8)
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			out = append(out, t)
		}
	}
	return out
}
