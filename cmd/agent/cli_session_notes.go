// cli_session_notes.go — `gopi session notes <query>`: the reader of the
// ROUND-LEVEL note index (internal/tools/chat_notes.go).
//
// WHY IT IS A FLAG-FREE ARM OF THE EXISTING `session` FAMILY (Rule 3: no new
// top-level tool, no new top-level verb). The notes are a property of a
// session — they are the [IMPORTANT] blocks its rounds wrote — so the family
// that already answers "what did this session do" is where a reader looks for
// them, and `gopi session <TAB>` reaches this verb by construction
// (sessionSubcommandNames is the completion set).
//
// WHAT IT IS NOT: it is not a second search implementation. It parses three
// tokens and hands the query to tools.ChatNoteSearch, which owns the tier
// ladder (fts → trigram → like) and the render. One query, one ranking, one
// renderer.

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/tools"
)

// sessionSubcmdNotes is the sub-verb word. It is a named constant because the
// dispatcher's switch, the family's vocabulary slice, the usage page and the
// `-h` contract all compare it (Rule 1b: never type the word twice).
const (
	sessionSubcmdNotes = "notes"

	// sessionNotesUsageFingerprint is the usage page's first line — the string
	// the help contract pins (cli_contracts_test.go).
	sessionNotesUsageFingerprint = "gopi session notes — search the turn-level [IMPORTANT] note index"

	// sessionNotesLimitFlag is the `--limit N` selector.
	sessionNotesLimitFlag = "--limit"

	// sessionNotesMinArgs is how many operands the verb needs: exactly the
	// query. A bare `gopi session notes` is a USAGE error, never an empty
	// search that prints "no notes matching """ (which reads as "you have
	// none" — the failure Rule 15 names).
	sessionNotesMinArgs = 1
)

// sessionNotesUsage is the verb's own page. It is a VAR, not a const: the two
// limit numbers come from the search itself (ChatNoteSearchDefaultLimit /
// MaxLimit), so the page can never advertise a bound the code does not honour.
var sessionNotesUsage = sessionNotesUsageFingerprint + `

Every round of a gopi run records a one-line [IMPORTANT] note (the model's own
summary), stamped with the TURN it was written in — one turn is one complete
input (a typed line, a ` + "`gopi notify` delivery" + `), and it spans every round the
agent ran answering it. This verb searches those notes across every session on
THIS machine — the turn-level index, not the conversation index that
` + "`gopi session list`" + ` renders.

Usage:
  gopi session notes <query> [--limit N]
      Match <query> against the recorded notes. The search runs three tiers in
      order and reports which one answered:
        fts      words and ASCII phrases (BM25-ranked)
        trigram  CJK substrings of 3+ characters — unicode61 keeps a whole CJK
                 run as ONE token, so a Chinese fragment needs this tier
        like     anything below trigram's lower bound (a 2-character fragment)
      --limit N   how many hits to print (default ` + strconv.Itoa(tools.ChatNoteSearchDefaultLimit()) + `, cap ` + strconv.Itoa(tools.ChatNoteSearchMaxLimit()) + `).

Each hit prints its provenance — the MACHINE the note was recorded on, the
turn, the time, the session — and beneath it the FULL path of the record file
and the note text. A note row written before the turn stamp existed prints
` + "`round N`" + ` instead: that row really recorded a round, and saying so is the
honest answer rather than relabelling it. Both machine and path are stored
because two machines in this topology have home directories that are the same
string: the path alone cannot say which one a note came from.

The index is written by the run that records the note, best-effort, in the same
call that appends the side-channel file. ` + "`self-record.md`" + ` remains the
evidence; this verb is the retrieval layer over it.
`

// runSessionNotes implements `gopi session notes <query> [--limit N]`.
func runSessionNotes(args []string) int {
	limit := 0
	operand := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == HelpFlag || a == HelpFlagLong:
			fmt.Print(sessionNotesUsage)
			return 0
		case a == sessionNotesLimitFlag:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "%s needs a number\n%s", sessionNotesLimitFlag, sessionNotesUsage)
				return 1
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				fmt.Fprintf(os.Stderr, "%s needs a positive number, got %q\n", sessionNotesLimitFlag, args[i])
				return 1
			}
			limit = n
		case strings.HasPrefix(a, sessionNotesLimitFlag+"="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, sessionNotesLimitFlag+"="))
			if err != nil || n <= 0 {
				fmt.Fprintf(os.Stderr, "%s needs a positive number, got %q\n", sessionNotesLimitFlag, a)
				return 1
			}
			limit = n
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "gopi session notes: %q is not an option\n%s", a, sessionNotesUsage)
			return 1
		default:
			operand = append(operand, a)
		}
	}
	if len(operand) < sessionNotesMinArgs {
		fmt.Fprintf(os.Stderr, "gopi session notes: which notes? give a query\n%s", sessionNotesUsage)
		return 1
	}
	out, err := tools.ChatNoteSearch(strings.Join(operand, " "), limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session notes: %v\n", err)
		return 1
	}
	fmt.Print(out)
	return 0
}
