// The `gopi tool tokens` predicate: the argv face of the token-count tool.
//
// The tool exists because the operator asked for exactly this shape — read a raw
// log, take each request out of it, count its tokens with the tokenizer we
// already ship, and put that number beside the one the server reported. A count
// nobody compares against the server is a number with no truth value.
package main

import (
	"fmt"
	"os"
)

// The four input shapes `gopi tool tokens` accepts, plus the two modifiers.
// They are named constants (Rule 1) because the parser, the failure sentences
// and the usage page all address the same words: a page that taught `--file`
// while the parser compared `"--file"` typed inline would be two definitions of
// one flag, and the day one moved the page would advertise a word the verb
// refuses.
const (
	tokensFlagFile    = "--file"
	tokensFlagSession = "--session"
	tokensFlagRaw     = "--raw"
	tokensFlagBody    = "--body"
	tokensFlagCompare = "--compare"
	tokensFlagLimit   = "--limit"
)

// isTokensSubcommand is the argv predicate for `gopi tool tokens`; like every
// other subcommand it consumes the rest of argv and handles its own exit.
//
// The tool exists because the operator asked for exactly this shape: read a raw
// log, take each request out of it, count its tokens with the tokenizer we
// already ship, and put that number beside the one the server reported. A count
// nobody compares against the server is a number with no truth value.
func isTokensSubcommand(args []string) bool {
	if len(args) == 0 || args[0] != CliVerbTokens {
		return false
	}
	os.Exit(runTokensSubcommand(args[1:]))
	return true
}

// runTokensSubcommand dispatches the tool and returns an exit status instead of
// exiting itself, so the predicate above stays the ONE place that calls Exit.
func runTokensSubcommand(args []string) int {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			fmt.Println(tokensUsage)
			return 0
		}
	}
	var (
		rawPath   string
		bodyPath  string
		filePath  string
		sessionID string
		compare   bool
		limit     int
	)
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case tokensFlagRaw:
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "gopi tool tokens: "+tokensFlagRaw+" needs a path")
				return tokensExitUsage
			}
			rawPath = args[i]
		case tokensFlagBody:
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "gopi tool tokens: "+tokensFlagBody+" needs a path")
				return tokensExitUsage
			}
			bodyPath = args[i]
		case tokensFlagFile:
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "gopi tool tokens: "+tokensFlagFile+" needs a path")
				return tokensExitUsage
			}
			filePath = args[i]
		case tokensFlagSession:
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "gopi tool tokens: "+tokensFlagSession+" needs a session id")
				return tokensExitUsage
			}
			sessionID = args[i]
		case tokensFlagCompare:
			compare = true
		case tokensFlagLimit:
			i++
			if i >= len(args) || sscanfInt(args[i], &limit) != nil {
				fmt.Fprintln(os.Stderr, "gopi tool tokens: "+tokensFlagLimit+" needs a number")
				return tokensExitUsage
			}
		default:
			fmt.Fprintf(os.Stderr, "gopi tool tokens: unknown argument: %s\n%s\n",
				a, tokensUsage)
			return tokensExitUsage
		}
	}

	switch {
	case filePath != "":
		return runFile(filePath)
	case sessionID != "":
		return runSession(sessionID)
	case rawPath != "":
		return runRaw(rawPath, limit)
	case bodyPath != "":
		return runBody(bodyPath, compare)
	}
	fmt.Fprintln(os.Stderr, "gopi tool tokens: one of "+tokensFlagFile+", "+tokensFlagSession+
		", "+tokensFlagRaw+" or "+tokensFlagBody+" is required\n"+tokensUsage)
	return tokensExitUsage
}
