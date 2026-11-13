package main

import (
	"fmt"
	"strings"
)

// chat_replay_help_scope.go — the replay/film family's OWN help page, and the
// resolver that decides which page a `-h` asks for.
//
// WHY THIS FILE EXISTS. `gopi chat -h` was ONE flat page in which the
// replay/film family (`--replay`, `--film`, `--out`, `--wechat`,
// `--film-terminal`, `--scratch`, `--cols`/`--rows`, `--max-beats`,
// `--ack-timeout-ms`) was interleaved with the interactive session's flags, so a
// reader could not tell which words apply to a normal `gopi chat` and which only
// make sense beside `--replay`. The operator's complaint was a SCOPE failure, not
// a missing-flag one: every word was documented, and the page never said which
// unit they belong to.
//
// WHAT IS NEW HERE, AND WHAT IS DELIBERATELY NOT. No new verb, no new top-level
// tool and no second copy of any flag's prose: the scoped page is COMPOSED from
// the very same named parts chatUsage joins (chatReplayUsage, chatFilmUsage,
// chatBeatUsageSection, chatReplayExamples) — one definition, two renderings
// (Rule 13). The main page keeps a compact INDEX of the family, so a reader
// reaching `gopi chat -h` still meets every family word once, learns they are one
// unit, and is told the page that documents them in full.
//
// THE SCOPE DECISION READS ARGV, NEVER A CONSTANT (Rule 31). chatHelpScopeFor is
// handed the command line exactly as the operator typed it — before the argv walk
// has consumed anything — and answers "which page did this caller ask about". A
// test that pinned a constant would pin the page's text and prove nothing about
// the routing; chat_replay_help_scope_test.go therefore drives REAL argv through
// the real binary.

// chatReplayHelpFlags is the family's own vocabulary, in the order a reader walks
// it (re-run → film → the film's knobs). Every word comes from its parser's
// constant (Rule 1/13/14): a page that spelled a flag differently from the parser
// would advertise a command line the verb refuses.
func chatReplayHelpFlags() []string {
	return []string{
		chatFlagReplay,
		// --from-turn states WHERE in that recording the run begins, so it is the
		// replay family's own word: leaving it off this list left it off both
		// pages while the parser read it (measured: TestChatCLIVocabularyIsClosed
		// EveryAcceptedFlagParses reported "--from-turn parses but is not
		// documented in chatUsage").
		chatFlagFromTurn,
		chatFlagFilm,
		chatFlagOut,
		chatFlagWechat,
		chatTerminalFlag,
		chatFlagScratch,
		chatFlagCols,
		chatFlagRows,
		chatFlagMaxBeats,
		chatFlagAckTimeoutMS,
	}
}

// chatHelpScope is which help page a command line asked for.
type chatHelpScope int

const (
	// chatHelpScopeMain is the ordinary `gopi chat -h` page.
	chatHelpScopeMain chatHelpScope = iota
	// chatHelpScopeReplay is the replay/film page: `gopi chat --replay -h`.
	chatHelpScopeReplay
)

// chatHelpScopeFor answers which page a command line asked about, by looking for
// the FAMILY's own words in it.
//
// IT READS THE RAW ARGV ON PURPOSE. The walk below this point has already
// consumed `--replay`'s value into replayTarget and dropped the word; a resolver
// that read the parse result would be reading a DIFFERENT command line (and would
// answer "main" for `gopi chat --replay <id> -h`, because a successful parse
// leaves no trace of which flags were named). The argv is the input the operator
// wrote, so the argv is what the scope is read from.
//
// BOTH SPELLINGS ARE RECOGNISED (`--replay ID` and `--replay=ID`), because both
// are accepted by the parser — the joined form is matched through the same
// flag+separator shape chatValuedFlagWord uses, so a rename follows the compiler.
//
// A NON-FAMILY WORD NEVER SELECTS THE SCOPED PAGE: `gopi chat --tools -h` and a
// bare `gopi chat -h` both ask about the ordinary session, which is the page that
// documents `--tools`.
func chatHelpScopeFor(argv []string) chatHelpScope {
	for _, a := range argv {
		for _, flag := range chatReplayHelpFlags() {
			if a == flag || strings.HasPrefix(a, flag+chatFlagValueSep) {
				return chatHelpScopeReplay
			}
		}
	}
	return chatHelpScopeMain
}

// chatHelpPage returns the page a scope names. It is ONE function so the two
// help-answer points in this verb (the early return and the positional-beside-
// query arm) cannot pick different pages for the same command line.
func chatHelpPage(scope chatHelpScope) string {
	if scope == chatHelpScopeReplay {
		return chatReplayHelp
	}
	return chatUsage
}

// chatReplayHelpPointsHere is the pointer the MAIN page prints for the family,
// built from the flag and help-query constants so it cannot drift from the
// command line that actually produces the scoped page (Rule 1/13).
const chatReplayHelpPointsHere = "gopi chat " + chatFlagReplay + " " + HelpFlag

// chatReplayFamilyCol is the column the family index's DETAILS start in, so the
// ten rows read as a table instead of as ten ragged lines. One named width
// (Rule 1) — a page whose columns are hand-spaced disagrees with itself the first
// time a flag is renamed.
const chatReplayFamilyCol = 22

// chatReplayFamilyRow renders one index row: the flag from its constant (Rule
// 1/13), padded to the common column.
func chatReplayFamilyRow(flag, detail string) string {
	return fmt.Sprintf("  %-*s%s\n", chatReplayFamilyCol, flag, detail)
}

// chatReplayGroupUsage is the MAIN page's view of the family: every word named
// once (so the surface census and a reader both still meet it), the unit stated
// as one unit, and the destination named. The full contract for each word lives on
// chatReplayHelp — this is an INDEX, and it says so.
//
// It is a `var` because the rows are composed (the padding is a call, Go forbids
// one in a constant expression); help_width_test.go measures the CONSTANTS, and
// this block's width is pinned separately by chat_replay_help_scope_test.go, so
// the exemption is measured rather than assumed (Rule 15).
var chatReplayGroupUsage = `
REPLAY AND FILM — ONE FAMILY, with a page of its own.

  These words only make sense TOGETHER: ` + chatFlagReplay + ` re-runs a recorded session
  instead of continuing one, ` + chatFlagFilm + ` photographs that re-run, and the rest are
  the film's own knobs. ` + chatReplayHelpPointsHere + ` prints this family ALONE —
  one page, no session flags — and states each word's contract in full. Named
  here so the words stay findable from this page:

` + chatReplayFamilyRow(chatFlagReplay+" SESSION", "re-run a RECORDED session on this surface") +
	chatReplayFamilyRow(chatFlagFromTurn+" N", "begin at that recording's Nth operator turn") +
	chatReplayFamilyRow(chatFlagFilm+" DIR", "film that re-run in a private display and terminal") +
	chatReplayFamilyRow(chatFlagOut+" FILE.gif", "encode the frames into one looping GIF") +
	chatReplayFamilyRow(chatFlagWechat, "hold that GIF to a phone view's own byte ceiling") +
	chatReplayFamilyRow(chatTerminalFlag+" NAME", "the terminal the film runs inside") +
	chatReplayFamilyRow(chatFlagScratch+" DIR", "the beat protocol's scratch directory") +
	chatReplayFamilyRow(chatFlagCols+" N", "the layout's width, in cells") +
	chatReplayFamilyRow(chatFlagRows+" N", "the layout's height, in cells") +
	chatReplayFamilyRow(chatFlagMaxBeats+" N", "the most beats one run renders") +
	chatReplayFamilyRow(chatFlagAckTimeoutMS+" N", "how long one beat waits for its acknowledgement")

// chatReplayHelp is the SCOPED page: `gopi chat --replay -h`.
//
// IT IS COMPOSED, NOT WRITTEN. Every block below is the SAME named part the main
// page joins — this page is a RENDERING of sections the verb already had, which is
// why no flag's prose appears twice in this package and the two pages cannot
// disagree about what a flag does (Rule 13).
//
// WHY THE SYNOPSIS IS HERE AND NARROWER. A page needs the shape of the command
// line it documents; the scoped page states ONLY the family's shape, so a reader
// is never handed `--tools` on a page about re-running a recording. The words are
// the same constants the parser matches.
var chatReplayHelp = `Usage: gopi chat ` + chatFlagReplay + ` SESSION [` + chatFlagFilm + ` DIR [` + chatFlagOut + ` FILE.gif] [` + chatFlagWechat + `]
                    [` + chatTerminalFlag + ` NAME]] [` + chatFlagScratch + ` DIR] [` + chatFlagCols + ` N ` + chatFlagRows + ` N]
                    [` + chatFlagMaxBeats + ` N] [` + chatFlagAckTimeoutMS + ` N]

THE REPLAY AND FILM PAGE — the family ` + chatReplayHelpPointsHere + ` renders.

Re-running a recorded session, and filming it, is ONE job with ONE set of words.
They are documented here INSTEAD of on the page ` + "`gopi chat -h`" + ` prints, because a
reader of the ordinary session page was being handed nine flags that only apply
beside ` + chatFlagReplay + `, with nothing saying which was which. This page is that
family's own; ` + "`gopi chat " + HelpFlag + "`" + ` is the ordinary interactive session, and
` + "`man gopi-chat`" + ` carries the contracts these summaries point at.
` + chatReplayUsage + chatFilmUsage + chatBeatUsageSection + chatReplayExamples
