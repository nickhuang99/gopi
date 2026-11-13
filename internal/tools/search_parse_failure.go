package tools

// search_parse_failure.go — Rule 15 for the rg output parser.
//
// splitRgColumnLine (search.go) answers ok=false for a line of ripgrep's output
// it cannot read. Until this file existed the wrapper answered `continue` on
// every such line, so a search whose matches were ALL unparseable came back as
// `hits=0, err=nil` — the exact answer a search with no matches gives. The
// caller cannot tell "nothing matched" from "the backend matched something the
// parser threw away", and the second is a defect to report, not a quiet zero.
//
// The choice among the three shapes the epic offers (error / raw line in the
// result / non-zero dropped count):
//
//   - a DROPPED COUNT is already a channel on this surface and is already
//     rendered: capSlice's overflow tally reaches the model as "… plus N more
//     matches", and search_fallback.go's scanner leg maintains the same meaning
//     ("matched but not delivered"). A second, differently-meaning integer
//     beside it — one counting results the cap withheld, one counting output the
//     parser threw away — is how two numbers start disagreeing.
//   - an ERROR is right, because a hit the parser cannot even name is not a
//     result anybody can act on, and rg's output shape is the wrapper's own
//     contract: the in-process fallback (search_fallback.go) writes that shape
//     itself, so a line that breaks it means the WRAPPER is wrong, not the tree
//     being searched — precisely the thing a caller must not read as "no match".
//   - the raw line inside the result was rejected: TextHit's FilePath/LineNo are
//     what the model acts on; a "hit" that is an opaque string would render as a
//     location-shaped entry carrying no location.
//
// So: any unparseable line fails the call, and the error NAMES the count, the
// first offending line (bounded — a session JSONL row is megabytes and the error
// must stay readable) and the parser's own contract. The bound is a named limit
// (Rule 1).
//
// The message carries NO tool-name prefix: both callers already own one —
// keywordSearchImpl returns this error as-is under `keyword_search error:`, and
// referenceHitsViaRipgrep wraps it in `code_search: fallback:` because THAT is
// the surface that was called (search.go, defect ③). A third name baked in here
// would attribute the failure to a tool the caller never invoked.

import "fmt"

const (
	// searchParseFailureLineCap bounds the offending line echoed into the error.
	// The line being described is one the parser could not read — often a whole
	// session message — so echoing it verbatim would turn a diagnostic into a
	// second copy of the payload. 400 bytes is enough to recognise the shape.
	searchParseFailureLineCap = 400

	// searchParseFailureEllipsis marks the bounded echo as a truncation, so a
	// reader never mistakes a cut line for the line that was printed.
	searchParseFailureEllipsis = "…"
)

// searchParseFailure formats the "the backend matched, the parser gave up" error.
// ONE function, so every leg of the rg parser reports it identically (Rule 14).
// unparseable is how many output lines were thrown away, parsed how many became
// hits — the two numbers that tell a reader whether the result was empty or
// merely unreadable.
func searchParseFailure(line string, unparseable, parsed int) error {
	if len(line) > searchParseFailureLineCap {
		line = line[:searchParseFailureLineCap] + searchParseFailureEllipsis
	}
	return fmt.Errorf(
		"%s output did not parse as `path:line:col:text`: %d line(s) unreadable, %d parsed into hits; "+
			"a match the parser cannot read is a FAILURE, not a miss — the wrapper is wrong, the tree is not empty. "+
			"First unparseable line: %q",
		searchBackendRipgrep, unparseable, parsed, line)
}
