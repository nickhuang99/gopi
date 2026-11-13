// cli_session_flag_order_test.go — ONE criterion for the whole `gopi session`
// family: where a flag may stand relative to the positional <session-id>.
//
// THE MEASURED DEFECT (2026-10-27). The sibling verbs disagreed. `session show`
// and `session replay` parse their own argv and accepted BOTH orders, while
// `session log`, `session stats` and `session raw` share the hand-rolled
// `memoryFlagSet` scanner, whose default branch reads "a bare --flag takes the
// NEXT token as its value". So `--json <id>` handed the session id to the flag
// as its value, left no positional behind, and the verbs answered
// "missing <session-id> argument" / "pick one input: <session-id>, …" — a claim
// about an input the operator HAD given. The id was present; the scanner ate it.
//
// THE CRITERION THE FIX MUST HOLD, for EVERY verb of the family that takes a
// positional session id: the invocation either (a) behaves identically whichever
// side of the id the flag stands on, or (b) refuses NAMING the ordering
// requirement, and never claims the id is absent while it is on the command
// line. This test pins (a) as the answer, and pins it in three places:
//
//  1. the shared scanner's own arity declarations (TestSessionFlagOrderArity*),
//     which is where the defect lived;
//  2. the three sharing parsers, both orders, argv level;
//  3. the REAL BINARY over the real entry point (`gopi session <verb> …`),
//     because a parse-level green can still be unreachable (PROJECT.md Rule 31).

package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/tools"
)

// sessionFlagOrderID is the fixture session every case addresses. The sandbox
// below installs it (session JSONL + its paired raw log) exactly as
// cli_session_help_surface_basic_test.go's g4NewSandbox does, so no case
// depends on a constructed log format.
var sessionFlagOrderID = g4FixtureSessionID

// sessionFlagOrderCase is one verb's flag, spelled on BOTH sides of the id.
// before/after are the two argv tails (`<flag> <id>` and `<id> <flag>`) — the
// same invocation, one token apart.
type sessionFlagOrderCase struct {
	verb   string
	flag   string
	before []string // flag FIRST: the spelling that used to be eaten
	after  []string // id FIRST: the spelling that always worked
}

// sessionFlagOrderCases covers every verb of the family that takes a positional
// session id, with the flags each verb actually advertises. The verbs whose
// parsers are their own (show, replay, notes) are in the table on purpose: the
// criterion is the FAMILY's, so a future edit that breaks one of them is the
// same defect and must fail here.
func sessionFlagOrderCases() []sessionFlagOrderCase {
	id := sessionFlagOrderID
	return []sessionFlagOrderCase{
		// show — its own parser, already order-agnostic; pinned so it stays.
		{verb: sessionSubcmdShow, flag: sessionJSONFlag,
			before: []string{sessionJSONFlag, id}, after: []string{id, sessionJSONFlag}},

		// log — via the shared scanner: the reported defect.
		{verb: CliVerbLog, flag: sessionJSONFlag,
			before: []string{sessionJSONFlag, id}, after: []string{id, sessionJSONFlag}},
		{verb: CliVerbLog, flag: "--" + logFlagRounds,
			before: []string{"--" + logFlagRounds, id}, after: []string{id, "--" + logFlagRounds}},

		// stats — via the shared scanner.
		{verb: CliVerbStats, flag: sessionJSONFlag,
			before: []string{sessionJSONFlag, id}, after: []string{id, sessionJSONFlag}},
		{verb: CliVerbStats, flag: "--rounds",
			before: []string{"--rounds", id}, after: []string{id, "--rounds"}},

		// raw — via the shared scanner.
		{verb: CliVerbRaw, flag: "--" + rawFlagRounds,
			before: []string{"--" + rawFlagRounds, id}, after: []string{id, "--" + rawFlagRounds}},
		{verb: CliVerbRaw, flag: "--" + rawFlagSysprompt,
			before: []string{"--" + rawFlagSysprompt, id}, after: []string{id, "--" + rawFlagSysprompt}},

		// replay — its own parser, already order-agnostic; pinned so it stays.
		{verb: sessionSubcmdReplay, flag: sessionReplayTurnsFlag,
			before: []string{sessionReplayTurnsFlag, id}, after: []string{id, sessionReplayTurnsFlag}},
		{verb: sessionSubcmdReplay, flag: sessionReplayRoundsFlag,
			before: []string{sessionReplayRoundsFlag, id}, after: []string{id, sessionReplayRoundsFlag}},
	}
}

// TestSessionFlagOrderIsTheSameOnBothSides drives the REAL BINARY: for every
// verb+flag pair, `<flag> <id>` and `<id> <flag>` must produce the SAME exit
// code and the SAME stdout. A verb that works in one order and refuses in the
// other is the defect; a refusal that names the id as missing while the id is
// on the command line is its most misleading form (Rule 15).
func TestSessionFlagOrderIsTheSameOnBothSides(t *testing.T) {
	cases := sessionFlagOrderCases()
	// Non-vacuity floor: a broken table must not pass as a clean run.
	if len(cases) < 8 {
		t.Fatalf("the flag-order table carries %d case(s) — the family covers "+
			"show/log/stats/raw/replay, so this run proves nothing", len(cases))
	}
	sb := g4NewSandbox(t)
	// The chat INDEX row: `session show` reads tools.ChatShow (the chat table),
	// not the JSONL alone, so a sandbox holding only the log answers "no row for
	// session … — re-index with `gopi chat backfill`". A row is seeded through
	// the tools layer's own writer (Rule 32), pinned to the sandbox profile so the
	// CHILD resolves it (cliContractEnv hands the child GOPI_HOME and nothing
	// else, so the ledger must live at <home>/.gopi/memory.db).
	seedSandboxChatRow(t, sb.home)
	for _, c := range cases {
		t.Run(c.verb+"_"+strings.TrimLeft(c.flag, "-"), func(t *testing.T) {
			beforeOut, beforeErr, beforeCode := sb.runSession(t, c.verb, c.before)
			afterOut, afterErr, afterCode := sb.runSession(t, c.verb, c.after)

			if beforeCode != afterCode {
				t.Fatalf("`gopi session %s %s` exits %d, `gopi session %s %s` exits %d — "+
					"the flag's position changed the OUTCOME\n"+
					"flag-first stderr:\n%s\nid-first stderr:\n%s",
					c.verb, strings.Join(c.before, " "), beforeCode,
					c.verb, strings.Join(c.after, " "), afterCode, beforeErr, afterErr)
			}
			if beforeCode != 0 {
				t.Fatalf("`gopi session %s %s` exits %d — both orders must accept the flag "+
					"beside the id (the family criterion is 'accept both', not "+
					"'refuse one')\nstderr:\n%s",
					c.verb, strings.Join(c.before, " "), beforeCode, beforeErr)
			}
			if beforeOut != afterOut {
				t.Fatalf("`gopi session %s %s` and `%s %s` disagree on stdout "+
					"(%d vs %d bytes) — the flag's position must not change the view",
					c.verb, strings.Join(c.before, " "), c.verb, strings.Join(c.after, " "),
					len(beforeOut), len(afterOut))
			}
			// The most misleading form of the defect: the id IS on the command
			// line and the verb says it is not.
			if claimsMissingInput(beforeErr) || claimsMissingInput(afterErr) {
				t.Fatalf("the invocation carries the id %q and the verb still claims an "+
					"input is missing\nflag-first stderr:\n%s\nid-first stderr:\n%s",
					sessionFlagOrderID, beforeErr, afterErr)
			}
		})
	}
}

// seedSandboxChatRow files the chat row for the fixture session into the ledger
// the CHILD resolves — the sandbox profile's own memory.db, the path gopi
// derives from GOPI_HOME. Both the writer pin and the derived path are needed:
// the suite scaffold pins the process's ledger, and that pin would otherwise win
// over GOPI_HOME while the child (whose env drops every GOPI_* variable) reads
// the sandbox one.
func seedSandboxChatRow(t *testing.T, home string) {
	t.Helper()
	ledger := filepath.Join(home, config.ProfileDirName, config.MemoryDBFileName)
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(config.MemoryDBEnv, ledger)
	if _, err := tools.ChatEnsureRow(tools.ChatSession{
		SessionID: sessionFlagOrderID,
		Mode:      tools.ChatModeCLI,
		Model:     "glm-5.3-flash",
		FirstMsg:  "flag-order fixture " + sessionFlagOrderID,
		Workdir:   "/tmp/flag-order",
		LogPath: filepath.Join(home, config.ProfileDirName, config.SessionsDirName,
			sessionFlagOrderID+".jsonl"),
	}); err != nil {
		t.Fatalf("seed chat row: %v", err)
	}
}

// TestSessionResumeTargetIsTheSameInBothOrders covers the fourth verb of the
// family, whose operand is the resume TARGET rather than a <session-id>: it is
// its own test because this verb REFUSES an operand that names no session, so
// "both orders exit 0" is the wrong assertion — "both orders name the SAME
// target" is the right one.
//
// THE MEASURED DEFECT (2026-10-27). runResume read args[0] as the target
// unconditionally, so `gopi session resume --task t-123 <id>` reported
// `resume target "--task" is neither a raw log path nor an existing session id`
// — the operand was on the command line and the verb named the flag as it.
func TestSessionResumeTargetIsTheSameInBothOrders(t *testing.T) {
	const operand = "no-such-session-for-flag-order"
	target, tail := resumeTargetAndTail([]string{"--task", "t-none", operand})
	if target != operand {
		t.Errorf("flag-first target = %q, want %q — --task's VALUE (or the flag "+
			"token itself) was read as the thing to resume", target, operand)
	}
	if len(tail) != 2 || tail[0] != resumeTaskFlag || tail[1] != "t-none" {
		t.Errorf("flag-first tail = %v, want [%s t-none] — the flag and its value "+
			"belong to the --task parser, not to the target slot", tail, resumeTaskFlag)
	}

	target, tail = resumeTargetAndTail([]string{operand, "--task", "t-none"})
	if target != operand {
		t.Errorf("id-first target = %q, want %q", target, operand)
	}
	if len(tail) != 2 || tail[0] != resumeTaskFlag || tail[1] != "t-none" {
		t.Errorf("id-first tail = %v, want [%s t-none]", tail, resumeTaskFlag)
	}

	// No operand at all is refused by NAME, and the refusal never pretends a
	// -flag was the operand.
	if target, _ := resumeTargetAndTail([]string{"--task", "t-none"}); target != "" {
		t.Error("a lone --task was accepted as a complete invocation — the target " +
			"is the one input this verb cannot do without")
	}

	// The REAL BINARY, both orders: same exit code, same operand named.
	sb := g4NewSandbox(t)
	beforeOut, beforeErr, beforeCode := sb.runSession(t, CliVerbResume,
		[]string{resumeTaskFlag, "t-none", operand})
	afterOut, afterErr, afterCode := sb.runSession(t, CliVerbResume,
		[]string{operand, resumeTaskFlag, "t-none"})
	if beforeCode != afterCode {
		t.Fatalf("resume exits %d flag-first and %d id-first\n%s%s", beforeCode, afterCode, beforeErr, afterErr)
	}
	if beforeOut != afterOut {
		t.Errorf("resume's stdout differs between orders:\nflag-first:\n%s\nid-first:\n%s", beforeOut, afterOut)
	}
	if strings.Contains(beforeErr, `target "`+resumeTaskFlag+`"`) {
		t.Errorf("flag-first named the FLAG as the resume target:\n%s", beforeErr)
	}
	if !strings.Contains(beforeErr, operand) || !strings.Contains(afterErr, operand) {
		t.Errorf("both orders must name the operand %q:\nflag-first:\n%s\nid-first:\n%s",
			operand, beforeErr, afterErr)
	}
}

// claimsMissingInput reports whether a verb's diagnostics assert that the input
// is absent — the sentences the shared scanner produced while it was eating the
// session id as a flag value.
func claimsMissingInput(stderr string) bool {
	for _, claim := range []string{
		"missing <session-id> argument",
		"pick one input: <session-id>",
		"missing <id>",
	} {
		if strings.Contains(stderr, claim) {
			return true
		}
	}
	return false
}

// runSession executes `gopi session <verb> <tail...>` against the sandbox
// profile — the family's real entry point (`runSessionGroup`), one level below
// the top-level alias spellings the G4 cases use.
func (s g4Sandbox) runSession(t *testing.T, verb string, tail []string) (stdout, stderr string, code int) {
	t.Helper()
	argv := append([]string{CliVerbSession, verb}, tail...)
	return s.g4RunSplit(t, argv...)
}

// TestSessionFlagOrderArityIsDeclaredPerVerb pins the DECLARATION the fix adds:
// every flag a verb admits says whether it takes the next token as its value.
// This is the one place the defect can be reintroduced, so it is checked
// directly — a flag that is admitted but whose arity is unstated falls back to
// the scanner's "takes a value" default, which is exactly what ate the id.
func TestSessionFlagOrderArityIsDeclaredPerVerb(t *testing.T) {
	table := sessionFlagArityTables()
	if len(table) < 3 {
		t.Fatalf("only %d verb(s) declare arity — log/stats/raw all share the scanner", len(table))
	}
	for verb, arity := range table {
		// Every admitted flag must state its arity, in BOTH directions: a
		// valueless flag declared as value-taking eats the next token, and a
		// value-taking flag declared valueless leaves its value behind as a
		// stray positional.
		for _, f := range []string{sessionJSONFlag, "--" + logFlagRounds} {
			name := strings.TrimLeft(f, "-")
			takesValue, known := arity(name)
			if !known {
				continue // not every verb admits every flag
			}
			if takesValue {
				t.Errorf("%s admits %q as VALUE-TAKING — %q is a complete "+
					"instruction, and declaring it value-taking is the defect "+
					"(it claims the next token, which is the session id)", verb, f, f)
			}
		}
		if takesValue, known := arity("last"); known && !takesValue {
			t.Errorf("%s admits --last as VALUELESS — --last [N] carries its "+
				"count as the next token; declaring it valueless strands that "+
				"count as a stray positional", verb)
		}
	}
}

// TestSessionParsersAgreeOnFlagOrder is the argv-level half of the criterion:
// the three parsers that share the scanner must read BOTH orders to the same
// option set. It fails on the pre-fix tree for the flag-first spelling.
func TestSessionParsersAgreeOnFlagOrder(t *testing.T) {
	const id = "sess-flag-order"
	parsers := []struct {
		verb  string
		flag  string
		parse func([]string) (got string, err error)
	}{
		{verb: CliVerbLog, flag: sessionJSONFlag, parse: func(args []string) (string, error) {
			opts, err := parseLogArgs(args)
			if err != nil {
				return "", err
			}
			return opts.sessionID, nil
		}},
		{verb: CliVerbStats, flag: sessionJSONFlag, parse: func(args []string) (string, error) {
			opts, err := parseStatsArgs(args)
			if err != nil {
				return "", err
			}
			return opts.sessionID, nil
		}},
		// raw refuses --json by NAME (it prints verbatim bytes), so its case
		// uses a flag it admits: --rounds, the raw round index.
		{verb: CliVerbRaw, flag: "--" + rawFlagRounds, parse: func(args []string) (string, error) {
			opts, err := parseRawArgs(args)
			if err != nil {
				return "", err
			}
			return opts.sessionID, nil
		}},
	}
	for _, p := range parsers {
		t.Run(p.verb, func(t *testing.T) {
			// BOTH orders, one token apart.
			for _, args := range [][]string{
				{p.flag, id},
				{id, p.flag},
			} {
				got, err := p.parse(args)
				if err != nil {
					t.Fatalf("parse(%s %s): %v — the id is on the command line",
						p.verb, strings.Join(args, " "), err)
				}
				if got != id {
					t.Errorf("parse(%s %s).sessionID = %q, want %q — the flag's "+
						"position changed where the id landed",
						p.verb, strings.Join(args, " "), got, id)
				}
			}
		})
	}
}
