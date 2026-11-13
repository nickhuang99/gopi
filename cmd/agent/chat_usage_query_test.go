package main

// chat_usage_query_test.go — the C5/C14 contracts of `gopi chat -h`, pinned.
//
// WHY THIS FILE EXISTS. Two anchors of scripts/replay-check.sh measured red on
// the real binary while the row that owned them (task 01a10351) read completed:
//
//	C5   `gopi chat --replay -h` answered a usage error at rc=2 with stdout
//	     EMPTY — the operator asking how `--replay` is spelled was refused by
//	     the flag they were asking about;
//	C14  `gopi chat -h` never named `--scratch`, `--cols` or `--rows`, though
//	     the parser accepts them and this project's own film command lines use
//	     them.
//
// Both are the SAME defect family — a command line the operator can type, whose
// page does not answer — and both are pinned here from BOTH sides the argv
// contract of this package already uses: the parser IN PROCESS (a failure names
// the line that broke) and the REAL BINARY (Rule 31: a surface in no execution
// has not been delivered).
//
// The rule the first contract states is ONE-SIDED, and the assertions keep it
// that way: a usage query sitting where a valued flag's VALUE would be is
// answered as the QUERY, never consumed as a value — so no value is invented
// for a flag that got none, and `gopi chat --replay` alone is still a usage
// error naming the missing session.

import (
	"strings"
	"testing"
)

// chatUsageQueryProbeValue is the dummy value the walk hands a valued flag when
// the point of the case is the value slot itself (the flag's own parser reads
// it and the run never starts): the same spelling chat_argv_test.go's vocabulary
// walk uses, declared here so this file's cases can state the shape they mean.
const chatUsageQueryProbeValue = "some-target"

// chatUsageQueryProbeFor is the value this walk hands ONE valued flag. It is
// per-flag for the same measured reason chatValuedFlagProbeValue is
// (chat_argv_test.go): taking a value in the next slot says nothing about what
// the value must MEAN, and `--from-turn` parses its slot as a positive integer.
// Handing it "some-target" made `gopi chat --from-turn some-target -h` a usage
// refusal at rc=2 — correct for that command line, but the case was asserting
// that a flag this verb advertises parses, so the fixture was the defect
// (2026-10-27).
func chatUsageQueryProbeFor(flag string) string {
	if flag == chatFlagFromTurn {
		return "1" // the smallest ordinal a recording has (1-based, N >= 1)
	}
	return chatUsageQueryProbeValue
}

// chatUsageQueryPageMarker is the first line of the page this verb prints for a
// usage query — the ONE string that says "the help page was rendered" rather
// than "a refusal quoted the page back at you".
const chatUsageQueryPageMarker = "Usage: gopi chat"

// chatUsageQueryDocFlags are the three flags C14 measures: the doc's own command
// examples use them and the page must name them. They come from the parser's
// constants (Rule 1/13), never retyped as literals.
func chatUsageQueryDocFlags() []string {
	return []string{chatFlagScratch, chatFlagCols, chatFlagRows}
}

// chatUsageQueryCases is every valued flag of this verb paired with the two
// spellings of the usage query. It is built from chatResumeValuedFlags — the
// parser's OWN list — so a flag that gains a value slot joins this contract by
// construction rather than by somebody remembering to add a case here.
func chatUsageQueryCases() [][]string {
	cases := make([][]string, 0, len(chatResumeValuedFlags())*2)
	for _, flag := range chatResumeValuedFlags() {
		cases = append(cases,
			[]string{flag, HelpFlag},
			[]string{flag, HelpFlagLong},
		)
	}
	return cases
}

// TestChatUsageQueryWinsOverEveryValuedFlag pins the C5 contract for EVERY flag
// that takes a value: `gopi chat <valued-flag> -h` answers the page at rc=0 on
// stdout, and prints no usage error at all. The pre-change binary is RED here
// for --replay/--out/--resume/--chat-ideal-geometry/--film-terminal (rc=2 with
// stdout empty and the "needs a …" sentence on stderr).
func TestChatUsageQueryWinsOverEveryValuedFlag(t *testing.T) {
	for _, argv := range chatUsageQueryCases() {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			stdout, stderr, code := chatRunArgs(t, argv...)
			if code != chatExitOK {
				t.Fatalf("runChat(%v) = %d, want %d (a usage query, not a run)\nstderr:\n%s", argv, code, chatExitOK, stderr)
			}
			if strings.Contains(stderr, chatErrorPrefix) {
				t.Errorf("runChat(%v) REFUSED the command line — a usage query answers, it does not refuse:\n%s", argv, stderr)
			}
			// stderr is not required to be EMPTY: a config channel that disagrees
			// with the environment (the fixture exports a deliberately malformed
			// GOPI_CHAT_IDEAL_GEOMETRY) announces itself there and says which
			// channel decides next — a NOTICE, not a refusal, and not this
			// contract's business. The refusal is what is pinned, and its one
			// spelling is this verb's own prefix.
			if !strings.Contains(stdout, chatUsageQueryPageMarker) {
				t.Errorf("runChat(%v) did not print the page on stdout:\n%s", argv, stdout)
			}
		})
	}
}

// TestChatUsageQueryBesideAValuedFlagsOwnValue: the shape the parser already
// answered (`--replay <id> -h`) still answers, for every valued flag — the fix
// must not have been bought by breaking the spelling that worked.
func TestChatUsageQueryBesideAValuedFlagsOwnValue(t *testing.T) {
	for _, flag := range chatResumeValuedFlags() {
		argv := []string{flag, chatUsageQueryProbeFor(flag), HelpFlag}
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			stdout, stderr, code := chatRunArgs(t, argv...)
			if code != chatExitOK {
				t.Fatalf("runChat(%v) = %d, want %d\nstderr:\n%s", argv, code, chatExitOK, stderr)
			}
			if !strings.Contains(stdout, chatUsageQueryPageMarker) {
				t.Errorf("runChat(%v) did not print the page:\n%s", argv, stdout)
			}
		})
	}
}

// TestChatUsageQueryLaterOnTheLineWinsOverAValueRefusal pins the OTHER position
// a query can occupy, and the refusal that must survive it.
//
// THE DEFECT (measured 2026-10-27 at master d2dcd2b): the walk's help-wins arm
// fired only when the query sat IN a valued flag's value slot, so
// `gopi chat --from-turn some-target -h` — the query one word LATER — had its
// operand read by the flag's parser, refused as "not a turn number", and
// answered with exit 2 before the page was asked for. The test above pins the
// sibling flags; --from-turn was the only one red, because it is the only valued
// flag whose parser VALIDATES the word it reads.
//
// BOTH HALVES ARE PINNED HERE, and the rule stays one-sided:
//   - WITH the query, the page answers at rc=0 and nothing is refused;
//   - WITHOUT it, the SAME operand is still refused at rc=2, naming it (Rule 15:
//     a typo that fell back to "play from the start" would re-run the whole
//     recording and look as if it had obeyed).
//
// The second half is what stops the fix being bought by deleting the validation:
// accepting the operand would pass the first case and destroy the reason the
// flag has a parser at all.
func TestChatUsageQueryLaterOnTheLineWinsOverAValueRefusal(t *testing.T) {
	// A NON-NUMERIC operand: it is what makes --from-turn's validator speak, and
	// the same word the closed-vocabulary walk types.
	operand := chatUsageQueryProbeValue

	t.Run("query later on the line answers the page", func(t *testing.T) {
		argv := []string{chatFlagFromTurn, operand, HelpFlag}
		stdout, stderr, code := chatRunArgs(t, argv...)
		if code != chatExitOK {
			t.Fatalf("runChat(%v) = %d, want %d — a usage query documents a command line, it does not validate one\nstderr:\n%s",
				argv, code, chatExitOK, stderr)
		}
		if !strings.Contains(stdout, chatUsageQueryPageMarker) {
			t.Errorf("runChat(%v) did not print the page on stdout:\n%s", argv, stdout)
		}
	})

	t.Run("no query keeps the refusal, naming the operand", func(t *testing.T) {
		argv := []string{chatFlagFromTurn, operand}
		stdout, stderr, code := chatRunArgs(t, argv...)
		if code != chatExitUsage {
			t.Fatalf("runChat(%v) = %d, want %d — %q is not a turn number and must still be refused\nstderr:\n%s",
				argv, code, chatExitUsage, operand, stderr)
		}
		if !strings.Contains(stderr, operand) {
			t.Errorf("runChat(%v): the refusal does not name the operand %q:\n%s", argv, operand, stderr)
		}
		if strings.Contains(stdout, chatUsageQueryPageMarker) {
			t.Errorf("runChat(%v) printed the page for a line with no query on it:\n%s", argv, stdout)
		}
	})
}

// TestChatUsageQueryOverTheBeatProtocolsOwnWords pins the same contract for the
// words a DIFFERENT parser owns (chat_replay_beats.go): they are accepted by
// this verb, so their page must be reachable the same way — `--scratch -h` is
// the shape an operator types while reading the section that documents it.
func TestChatUsageQueryOverTheBeatProtocolsOwnWords(t *testing.T) {
	for _, flag := range chatBeatOwnedWords() {
		argv := []string{flag, HelpFlag}
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			stdout, stderr, code := chatRunArgs(t, argv...)
			if code != chatExitOK {
				t.Fatalf("runChat(%v) = %d, want %d\nstderr:\n%s", argv, code, chatExitOK, stderr)
			}
			if !strings.Contains(stdout, chatUsageQueryPageMarker) {
				t.Errorf("runChat(%v) did not print the page:\n%s", argv, stdout)
			}
		})
	}
}

// TestChatUsageQueryInventsNoValueForAFlagThatGotNone is the OTHER half of the
// one-sided rule: a valued flag with NO value beside it is still a usage error,
// named, with the page. Answering `-h` where a value should be must not turn a
// missing value into an acceptable command line — that would be the quiet drop
// this parser exists to close.
func TestChatUsageQueryInventsNoValueForAFlagThatGotNone(t *testing.T) {
	for _, flag := range chatResumeValuedFlags() {
		t.Run(flag, func(t *testing.T) {
			_, stderr, code := chatRunArgs(t, flag)
			if code != chatExitUsage {
				t.Fatalf("runChat([%s]) = %d, want %d (no value was given, so the command line is wrong)\nstderr:\n%s",
					flag, code, chatExitUsage, stderr)
			}
			if !strings.Contains(stderr, flag) {
				t.Errorf("runChat([%s]): the refusal does not name the flag:\n%s", flag, stderr)
			}
		})
	}
	for _, flag := range chatBeatOwnedWords() {
		t.Run(flag, func(t *testing.T) {
			_, stderr, code := chatRunArgs(t, flag)
			if code != chatExitUsage {
				t.Fatalf("runChat([%s]) = %d, want %d\nstderr:\n%s", flag, code, chatExitUsage, stderr)
			}
		})
	}
}

// TestChatUsageQueryDoesNotLaunderAnUnusableValue is the OTHER half of the
// held-refusal repair, and it is the half a careless fix breaks. The walk now
// HOLDS a flag parser's refusal until the end of the argv, so the query can win
// — but a query may not turn a value no parser could use, or a command line that
// states one flag twice, into a page that runs nothing:
//
//   - no query on the line  → the refusal is returned verbatim, naming the flag
//     (`gopi chat --from-turn some-target` is still rc=2, and the sentence is the
//     parser's own);
//   - a query on the line AND a duplicate spelling → still rc=2, because "given
//     twice" is a statement about the command line, not about a value.
//
// Both are driven IN PROCESS through the same fixture the sibling cases use, so
// the red side of the repair (before the held refusal existed, the first arm was
// already green and the second was answered as the page) is stated on the same
// surface as the green one.
func TestChatUsageQueryDoesNotLaunderAnUnusableValue(t *testing.T) {
	// (a) a value the flag's own parser refuses, with NO query anywhere.
	_, stderr, code := chatRunArgs(t, chatFlagFromTurn, chatUsageQueryProbeValue)
	if code != chatExitUsage {
		t.Fatalf("runChat([%s %s]) = %d, want %d — the held refusal must be returned when no query is present\nstderr:\n%s",
			chatFlagFromTurn, chatUsageQueryProbeValue, code, chatExitUsage, stderr)
	}
	if !strings.Contains(stderr, chatFlagFromTurn) {
		t.Errorf("the refusal does not name %s:\n%s", chatFlagFromTurn, stderr)
	}
	// (b) the same bad value BESIDE the query: the QUERY wins (that is the
	// repair), and the page is printed.
	stdout, _, code := chatRunArgs(t, chatFlagFromTurn, chatUsageQueryProbeValue, HelpFlag)
	if code != chatExitOK || !strings.Contains(stdout, chatUsageQueryPageMarker) {
		t.Errorf("runChat([%s %s %s]) = %d with the page printed=%v, want %d and the page — a value the parser refused must not outrank the query",
			chatFlagFromTurn, chatUsageQueryProbeValue, HelpFlag, code,
			strings.Contains(stdout, chatUsageQueryPageMarker), chatExitOK)
	}
	// (c) a DUPLICATE spelling is not a bad value: the query does not launder it.
	stdout, stderr, code = chatRunArgs(t, chatFlagFromTurn, "2", chatFlagFromTurn, "3", HelpFlag)
	if code != chatExitUsage {
		t.Errorf("runChat([%s 2 %s 3 %s]) = %d, want %d — a duplicate statement about one run is a command-line error the query cannot answer away",
			chatFlagFromTurn, chatFlagFromTurn, HelpFlag, code, chatExitUsage)
	}
	if strings.Contains(stdout, chatUsageQueryPageMarker) {
		t.Errorf("the page was printed for a command line it refused:\n%s", stdout)
	}
	if !strings.Contains(stderr, chatUsageQueryDuplicateMarker) {
		t.Errorf("runChat([%s 2 %s 3 %s]): the refusal does not say the flag was stated twice:\n%s",
			chatFlagFromTurn, chatFlagFromTurn, HelpFlag, stderr)
	}
}

// chatUsageQueryDuplicateMarker is the phrase a duplicate refusal must carry:
// the contract's wording, declared once here so the assertion and the production
// sentence cannot drift apart into two spellings of one agreement.
const chatUsageQueryDuplicateMarker = "was given twice"

// TestChatUsageQueryDoesNotLaunderADuplicateSpelling generalises arm (c) above
// to EVERY valued flag, in BOTH spellings of the flag and BOTH orders of the
// pair, through the parser's own list (chatResumeValuedFlags) rather than the
// two flags that were measured.
//
// WHY THE GENERALITY IS THE POINT. The repair sits ABOVE the walk's two
// usage-query arms, either of which can be the path a repeated valued flag
// rides (the query in the value slot; the query later on the line). A fix that
// named --from-turn and --replay would have repaired the two measured cases and
// left `--film DIR --film DIR2 -h` answering the page — the same contract one
// flag over. Driven from the list, a flag that gains a value slot joins this
// contract by construction.
//
// The inline spelling is included deliberately: `--from-turn=2 --from-turn=3 -h`
// is the same statement, and it was measured RED after the first draft of this
// repair because the value sits in the FLAG word rather than in the next slot.
func TestChatUsageQueryDoesNotLaunderADuplicateSpelling(t *testing.T) {
	// Each spelling returns the argv WORDS one flag statement occupies — a slice,
	// not one joined string, because the spaced form is two words and a test that
	// typed it as `"--film 2"` would be describing a flag with a space in its name.
	spellings := map[string]func(flag, value string) []string{
		"spaced": func(flag, value string) []string { return []string{flag, value} },
		"inline": func(flag, value string) []string { return []string{flag + chatFlagValueSep + value} },
	}
	first, second := "2", "3"
	for _, flag := range chatResumeValuedFlags() {
		for name, spell := range spellings {
			join := func(parts ...[]string) []string {
				out := make([]string, 0, len(parts)+1)
				for _, p := range parts {
					out = append(out, p...)
				}
				return append(out, HelpFlag)
			}
			for _, argv := range [][]string{
				join(spell(flag, first), spell(flag, second)),
				join([]string{flag, first}, []string{flag, second}),
				join(spell(flag, first), []string{flag, second}),
			} {
				t.Run(flag+" "+name+" "+strings.Join(argv, " "), func(t *testing.T) {
					stdout, stderr, code := chatRunArgs(t, argv...)
					if code != chatExitUsage {
						t.Fatalf("runChat(%v) = %d, want %d — a duplicate statement about one command line is a command-line error the query cannot answer away\nstderr:\n%s",
							argv, code, chatExitUsage, stderr)
					}
					if !strings.Contains(stderr, chatUsageQueryDuplicateMarker) {
						t.Errorf("runChat(%v): the refusal does not say %q:\n%s", argv, chatUsageQueryDuplicateMarker, stderr)
					}
					if strings.Contains(stdout, chatUsageQueryPageMarker) {
						t.Errorf("runChat(%v) printed the page for a command line it refused:\n%s", argv, stdout)
					}
				})
			}
		}
	}
}

// TestChatUsageQueryStillRefusesAPositionalBesideIt: the quiet drop stays
// closed. A positional word BESIDE the query is an argument the run would have
// consumed, and help was asked for instead — the pair is refused rather than
// answered, and the refusal must not print the page it declined to serve.
//
// The case is driven THROUGH a valued flag whose value slot is occupied, so the
// positional survives the walk: `gopi chat --replay <id> -h some.log`.
func TestChatUsageQueryStillRefusesAPositionalBesideIt(t *testing.T) {
	argv := []string{chatFlagReplay, chatUsageQueryProbeValue, HelpFlag, "some.log"}
	stdout, stderr, code := chatRunArgs(t, argv...)
	if code != chatExitUsage {
		t.Fatalf("runChat(%v) = %d, want %d — a positional beside a usage query is refused, not dropped\nstderr:\n%s",
			argv, code, chatExitUsage, stderr)
	}
	if strings.Contains(stdout, chatUsageQueryPageMarker) {
		t.Errorf("runChat(%v) printed the page for a command line it refused:\n%s", argv, stdout)
	}
	// The bare spelling of the same contract (declared before this fix, kept).
	stdout, _, code = chatRunArgs(t, HelpFlagLong, "some.log")
	if code != chatExitUsage || strings.Contains(stdout, chatUsageQueryPageMarker) {
		t.Errorf("runChat([%s some.log]) = %d with the page printed=%v, want %d and no page",
			HelpFlagLong, code, strings.Contains(stdout, chatUsageQueryPageMarker), chatExitUsage)
	}
}

// TestChatHelpPageDocumentsTheBeatProtocolFlags pins C14: every word the beat
// protocol's parser accepts is NAMED on the page a reader gets from `gopi chat
// -h`. A flag the parser accepts and the page never mentions cannot be found by
// the operator who needs it.
func TestChatHelpPageDocumentsTheBeatProtocolFlags(t *testing.T) {
	for _, flag := range chatBeatOwnedWords() {
		if !strings.Contains(chatUsage, flag) {
			t.Errorf("%s parses (chatBeatOwnedWords) but the page never mentions it — the help and the parser disagree", flag)
		}
	}
	// The three words the film's own documented command lines use, counted the
	// way scripts/replay-check.sh counts them (C14 reads the page, not the
	// synopsis): the acceptance asks for at least one mention of each.
	n := 0
	for _, flag := range chatUsageQueryDocFlags() {
		if !strings.Contains(chatUsage, flag) {
			t.Errorf("%s is missing from the chat help page — the doc's own examples use it", flag)
			continue
		}
		n++
	}
	if n != len(chatUsageQueryDocFlags()) {
		t.Fatalf("the page mentions %d of %d documented flags", n, len(chatUsageQueryDocFlags()))
	}
}

// TestChatUsageQueryOnTheRealBinary is the SAME contract through the real
// binary (Rule 31): the argv is what a user types, the exit status is what
// their shell sees, and the page is what their terminal prints. The in-process
// cases above cannot prove that the DISPATCH path reaches this parser at all.
func TestChatUsageQueryOnTheRealBinary(t *testing.T) {
	p := g5NewProbe(t)
	for _, argv := range [][]string{
		{CliVerbChat, chatFlagReplay, HelpFlag},
		{CliVerbChat, chatFlagScratch, HelpFlag},
		{CliVerbChat, chatFlagCols, HelpFlag},
		{CliVerbChat, chatFlagRows, HelpFlag},
		{CliVerbChat, HelpFlag},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			res := p.run(t, "", argv...)
			if res.exit != chatExitOK {
				t.Fatalf("`gopi %s` exit %d, want %d (the page)\n%s", strings.Join(argv, " "), res.exit, chatExitOK, res.out)
			}
			if !strings.Contains(res.out, chatUsageQueryPageMarker) {
				t.Errorf("`gopi %s` did not print the page:\n%s", strings.Join(argv, " "), res.out)
			}
		})
	}
	// The refusal that must survive: a valued flag with no value is still wrong.
	if res := p.run(t, "", CliVerbChat, chatFlagReplay); res.exit != chatExitUsage {
		t.Errorf("`gopi %s` exit %d, want %d — the missing value was accepted", strings.Join([]string{CliVerbChat, chatFlagReplay}, " "), res.exit, chatExitUsage)
	}
	// C14, measured on the real page exactly as the audit script measures it.
	page := p.run(t, "", CliVerbChat, HelpFlag)
	mentions := 0
	for _, flag := range chatUsageQueryDocFlags() {
		mentions += strings.Count(page.out, flag)
	}
	if mentions < len(chatUsageQueryDocFlags()) {
		t.Errorf("`gopi chat %s` names the three documented flags %d time(s), want at least %d:\n%s",
			HelpFlag, mentions, len(chatUsageQueryDocFlags()), page.out)
	}
}
