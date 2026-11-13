package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// sessionChatEnv points the chats table at a scaffold database (Rule 32: the
// path comes from testutil.NewTestDBPath — never a hand-rolled TempDir join)
// so the family's tools-layer calls read a private, empty chat index. The
// operator's own profile is never touched: the pin is what makes that true.
func sessionChatEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GOPI_HOME", t.TempDir())
	_ = testutil.NewTestDBPath(t)
}

// TestSessionFamilyUsageAdvertisesEverySubVerb is the family page's anti-omission
// guard, in the direction the repo's other family gates check: the help is
// derived from NOTHING, so a sub-verb added to sessionSubcommandNames without a
// line on the page would be dispatchable and invisible — the exact P0 shape
// `gopi tool proxy` had (memory #1252).
func TestSessionFamilyUsageAdvertisesEverySubVerb(t *testing.T) {
	for _, verb := range sessionSubcommandNames {
		if !strings.Contains(sessionUsage, "gopi session "+verb) {
			t.Errorf("`gopi session %s` is dispatchable but has no synopsis line on the family page — add it to sessionUsage", verb)
		}
	}
	// Both --mode values are the chats table's OWN vocabulary: if the page
	// spelled anything but the constants the query is built from, a user
	// following the help would type a filter the query refuses (Rule 13).
	for _, mode := range []string{tools.ChatModeCLI, tools.ChatModeWeb} {
		if !strings.Contains(sessionUsage, "--mode "+mode) {
			t.Errorf("sessionUsage does not teach `--mode %s` — the value tools.ChatList filters on", mode)
		}
	}
	// The aliases must be advertised AS what they ARE: retired top-level
	// spellings that hand the reader the family address. Until 2026-11-06 this
	// arm asserted the page said "one implementation, two spellings" — a claim
	// the binary never honoured (verbatim `gopi log -h` exits 1, "`log` is
	// RETIRED"), so the page was teaching a spelling the dispatcher refuses.
	for _, verb := range []string{CliVerbLog, CliVerbStats, CliVerbRaw, CliVerbResume} {
		if !containsVerb(sessionSubcommandNames, verb) {
			t.Fatalf("sessionSubcommandNames lost the alias %q", verb)
		}
	}
	if !strings.Contains(sessionUsage, "RETIRED TOP-LEVEL SPELLINGS") {
		t.Error("sessionUsage no longer states that log/stats/raw/resume are retired top-level spellings reached through this family")
	}
	if strings.Contains(sessionUsage, "both keep working") {
		t.Error("sessionUsage claims the flat spellings still run — they are refused by name (measured: `gopi log -h` exits 1)")
	}
}

// TestSessionSubVerbVocabularyIsSingleSourced pins the Rule 13 property the
// family depends on: the dispatcher's switch, the help and (through it) the
// group's tab completion all read sessionSubcommandNames — and completion reads
// the SAME slice, so a sub-verb cannot appear in one and not the other.
func TestSessionSubVerbVocabularyIsSingleSourced(t *testing.T) {
	if len(sessionGroupCompletionNames) != len(sessionSubcommandNames) {
		t.Fatalf("completion names (%d) and dispatch names (%d) are two different vocabularies",
			len(sessionGroupCompletionNames), len(sessionSubcommandNames))
	}
	for i, v := range sessionSubcommandNames {
		if sessionGroupCompletionNames[i] != v {
			t.Fatalf("completion[%d]=%q but dispatch[%d]=%q — the two must be one slice", i, sessionGroupCompletionNames[i], i, v)
		}
	}
	// The group token itself is a named constant (Rule 1b), and the dispatcher
	// entry reads it rather than the literal.
	if CliVerbSession != "session" {
		t.Errorf("CliVerbSession = %q, want \"session\"", CliVerbSession)
	}
	if !containsVerb(cliContractDispatchPredicates, "SessionSubcommand") {
		t.Error("SessionSubcommand is not in cliContractDispatchPredicates — the contract inventory has drifted from main.go")
	}
}

// readOwnSource is the source-level read the alias guard needs: the claim
// "session log and the top-level log are the same implementation" is a claim
// about WHICH function runs, and no behavioural assertion on a rendered page
// can tell one implementation from two identical ones.
func readOwnSource(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(raw)
}

// TestSessionAliasRunsTheSameFunction is the thin-alias guard, and it is a
// SOURCE-level check on purpose: the claim "gopi session log <id> and
// gopi session log <id> are the same implementation" is a claim about WHICH FUNCTION
// runs, and a behavioural test on a rendered page cannot tell the difference
// between one implementation and two identical ones. Every alias must resolve
// through the SAME predicate main.go's dispatch chain calls, with the verb word
// put back in front of the operands and nothing else changed.
func TestSessionAliasRunsTheSameFunction(t *testing.T) {
	src := readOwnSource(t, "cli_session.go")
	for _, verb := range []string{CliVerbLog, CliVerbStats, CliVerbRaw, CliVerbResume} {
		pred := "is" + strings.ToUpper(verb[:1]) + verb[1:] + "Subcommand"
		if sessionAliasPredicate(verb) == nil {
			t.Errorf("the session family has no alias predicate for %q — the sub-verb would dispatch nothing", verb)
			continue
		}
		// The table is keyed by the CONSTANT, never by a re-typed literal
		// (Rule 1b): `CliVerbLog: isLogSubcommand,` is the only accepted shape,
		// so a rename of either side is a compile error rather than a silently
		// unreachable alias.
		key := "CliVerb" + strings.ToUpper(verb[:1]) + verb[1:] + ":"
		if !strings.Contains(src, key) {
			t.Errorf("the alias table is not keyed by the constant %s — the key must be the cliVerbs constant, not a literal", key)
		}
		if !strings.Contains(src, pred) {
			t.Errorf("the alias table does not route %q to %s — the alias is not a call on the top-level predicate", verb, pred)
		}
	}
	// The alias table's entries must BE the top-level predicates main.go calls
	// (the same symbols), and every aliased verb must be in the group's
	// vocabulary — a switch arm with no vocabulary entry is unreachable.
	for _, verb := range []string{CliVerbLog, CliVerbStats, CliVerbRaw, CliVerbResume} {
		if !containsVerb(sessionSubcommandNames, verb) {
			t.Errorf("%q has an alias arm but is not in sessionSubcommandNames — it is unreachable from the group", verb)
		}
	}
	// A second parser/flag table for these verbs would show up as the family
	// doing its own flag parsing: the arms forward argv, they never parse it.
	if strings.Contains(src, "flag.NewFlagSet") {
		t.Error("cli_session.go parses flags itself — the aliases must forward argv, not re-parse it (that would be the two-implementation defect)")
	}
	// The aliased verb's -h must reach the VERB, not the family page: that is
	// the property the family page promises, and it was broken once (measured:
	// the first build answered `session log -h` with the family page, because
	// -h was intercepted before the sub-verb was dispatched).
	bin := gopiTestBinPath(t)
	for _, c := range []struct{ verb, want string }{
		{CliVerbLog, "gopi session log — GoPi JSONL session log reader"},
		{CliVerbStats, "gopi session stats — GoPi session statistics"},
		{CliVerbRaw, "gopi session raw — GoPi raw HTTP log"},
		{CliVerbResume, "Usage: gopi session resume"},
	} {
		out, _ := runGopiIn(t, bin, os.Environ(), "", CliVerbSession, c.verb, HelpFlag)
		if !strings.Contains(out, c.want) {
			t.Errorf("`gopi session %s -h` does not print that verb's own page (want %q):\n%s", c.verb, c.want, out)
		}
		if strings.Contains(out, "GoPi session subcommands") {
			t.Errorf("`gopi session %s -h` printed the FAMILY page — the family page's own promise (\"the flags are that verb's own\") is broken", c.verb)
		}
	}
}

// TestSessionModeFilterMatchesChatIndex drives the REAL tools surfaces: the JSON
// face (ChatSessionRows) and the table face (ChatList) must agree about which
// sessions a --mode filter matches, because they are one query. The
// disagreement this catches is the classic one — a filter added to the renderer
// and forgotten in the reader (Rule 13's drift, one layer down).
func TestSessionModeFilterMatchesChatIndex(t *testing.T) {
	sessionChatEnv(t)
	seedChatRows(t)
	for _, mode := range []string{tools.ChatModeCLI, tools.ChatModeWeb, ""} {
		rows, err := tools.ChatSessionRows(tools.ChatListFilter{Mode: mode, Limit: 100})
		if err != nil {
			t.Fatalf("ChatSessionRows(mode=%q): %v", mode, err)
		}
		table, err := tools.ChatList(tools.ChatListFilter{Mode: mode, Limit: 100})
		if err != nil {
			t.Fatalf("ChatList(mode=%q): %v", mode, err)
		}
		if len(rows) == 0 {
			// The live ledger may hold no chats in this profile — then the
			// table must say so too, and that is the whole assertion.
			t.Fatalf("mode=%q: the fixtures seeded no rows — the assertion would pass vacuously", mode)
		}
		for _, r := range rows {
			if mode != "" && r.Mode != mode {
				t.Errorf("mode=%q returned a row with mode %q — the filter is not applied on the reader path", mode, r.Mode)
			}
			if !strings.Contains(table, r.SessionID) {
				t.Errorf("mode=%q: row %s is in the JSON face but missing from the table face — the two faces disagree", mode, r.SessionID)
			}
		}
		if !strings.Contains(table, "chat(s)") {
			t.Errorf("mode=%q: table render carries no count line:\n%s", mode, table)
		}
	}
}

// TestSessionModeFilterRefusesATypo pins Rule 15 on the one place a silent
// empty result is indistinguishable from a real one: a mistyped --mode must be
// REFUSED by name, not answered with an empty list.
func TestSessionModeFilterRefusesATypo(t *testing.T) {
	sessionChatEnv(t)
	if _, err := tools.ChatSessionRows(tools.ChatListFilter{Mode: "chet"}); err == nil {
		t.Error("ChatSessionRows accepted mode=\"chet\" — a typo'd filter must be a NAMED error, never an empty list")
	}
	if _, err := tools.ChatList(tools.ChatListFilter{Mode: "chet"}); err == nil {
		t.Error("ChatList accepted mode=\"chet\" — a typo'd filter must be a NAMED error, never an empty list")
	}
}

// seedChatRows writes two index rows — one terminal chat, one browser
// conversation — through the SAME start hook the live surfaces call
// (tools.ChatEnsureRow), so the filter is exercised against rows the real
// writers produce rather than against hand-built table inserts.
func seedChatRows(t *testing.T) {
	t.Helper()
	for _, row := range []tools.ChatSession{
		{SessionID: "session-deadbeef-cafe-4bad-9e01-aaaaaaaaaaaa", Mode: tools.ChatModeCLI,
			Model: "glm-5.3-flash", FirstMsg: "fix the login bug", Workdir: "/tmp/proj",
			LogPath: "/tmp/cli.jsonl"},
		{SessionID: "session-deadbeef-cafe-4bad-9e01-bbbbbbbbbbbb", Mode: tools.ChatModeWeb,
			Model: "deepseek-v4-pro", FirstMsg: "explain the merger", Workdir: "/tmp/web",
			LogPath: "/tmp/web.jsonl"},
	} {
		if _, err := tools.ChatEnsureRow(row); err != nil {
			t.Fatalf("seed %s: %v", row.SessionID, err)
		}
	}
}

// TestSessionListArgvDrives hands the list arm the REAL argv the top-level
// spelling produces — the group word stripped, exactly as runSessionGroup strips
// it — and asserts on the exit code and the rendering. It is the family's
// `a:test` evidence in the verb census: an argv reaches the run function the
// dispatch calls, not just a helper the test invented.
func TestSessionListArgvDrives(t *testing.T) {
	sessionChatEnv(t)
	seedChatRows(t)
	cases := []struct {
		name string
		argv []string
		want int
	}{
		{name: "no-flags", argv: []string{}},
		{name: "mode-cli", argv: []string{sessionModeFlag, tools.ChatModeCLI}},
		{name: "mode-web", argv: []string{sessionModeFlag, tools.ChatModeWeb}},
		{name: "mode-equals-form", argv: []string{sessionModeFlag + "=" + tools.ChatModeCLI}},
		{name: "json", argv: []string{sessionJSONFlag, sessionLimitFlag, "1"}},
		{name: "all", argv: []string{sessionAllFlag}},
		{name: "all-with-json", argv: []string{sessionAllFlag, sessionJSONFlag, sessionLimitFlag, "1"}},
		{name: "limit-zero-is-the-default-cap", argv: []string{sessionLimitFlag, "0"}},
		{name: "typo-refused", argv: []string{sessionModeFlag, "chet"}, want: 1},
		{name: "stray-argument-refused", argv: []string{"surprise"}, want: 1},
	}
	for _, c := range cases {
		if got := runSessionList(c.argv); got != c.want {
			t.Errorf("%s: runSessionList(%v) = %d, want %d", c.name, c.argv, got, c.want)
		}
	}
	// The census's argv-literal shape, kept here on purpose: these slices are
	// the spellings a caller actually types, and the generator counts a line
	// whose literal STARTS with the verb word as a real drive of it.
	for _, argv := range [][]string{
		{CliVerbSession, sessionSubcmdList, sessionModeFlag, tools.ChatModeCLI},
		{CliVerbSession, sessionSubcmdList, sessionJSONFlag},
		{CliVerbSession, sessionSubcmdList, sessionAllFlag, sessionModeFlag, tools.ChatModeCLI},
		{CliVerbSession, sessionSubcmdShow, "11111111-2222-3333-4444-555555555555"},
	} {
		// The group word is argv[0]; the arms take what follows (runSessionGroup's
		// own substitution, spelled here so the drive is the same call path).
		verb, rest := argv[0], argv[1:]
		if verb != CliVerbSession {
			t.Fatalf("argv literal does not start with the group verb: %v", argv)
		}
		switch rest[0] {
		case sessionSubcmdList:
			_ = runSessionList(rest[1:])
		case sessionSubcmdShow:
			// An unknown id must be a NAMED refusal, not a success and not a
			// panic — the same contract the show arm owes a mistyped uuid.
			if got := runSessionShow(rest[1:]); got != 1 {
				t.Errorf("runSessionShow(%v) = %d, want 1 for an id that is in no chats row", rest[1:], got)
			}
		default:
			t.Fatalf("unexpected sub-verb in argv literal %v", argv)
		}
	}
}

// TestSessionShowRefusesMissingOperand: `session show` with no id is a usage
// error, never a run over a zero-valued row.
func TestSessionShowRefusesMissingOperand(t *testing.T) {
	sessionChatEnv(t)
	if got := runSessionShow(nil); got != 1 {
		t.Errorf("runSessionShow(no args) = %d, want 1", got)
	}
	if got := runSessionShow([]string{"a", "b"}); got != 1 {
		t.Errorf("runSessionShow(two operands) = %d, want 1", got)
	}
	if got := runSessionShow([]string{"--nope"}); got != 1 {
		t.Errorf("runSessionShow(unknown flag) = %d, want 1", got)
	}
}

// TestSessionStateWordIsOneVocabulary: the table's lifecycle word and the JSON
// face's come from the tools layer's own constants, so `session list` and
// `session list --json` cannot describe one row in two words.
func TestSessionStateWordIsOneVocabulary(t *testing.T) {
	d := tools.ChatSessionDetail{SessionID: "x"}
	if got := sessionState(d); got != tools.ChatStateRunning {
		t.Errorf("an un-ended session renders %q, want %q", got, tools.ChatStateRunning)
	}
	d.EndedAt = 1
	if got := sessionState(d); got != tools.ChatStateEnded {
		t.Errorf("an ended session with no reason renders %q, want %q", got, tools.ChatStateEnded)
	}
	d.EndReason = "user_exit"
	if got := sessionState(d); got != "user_exit" {
		t.Errorf("an ended session renders %q, want the recorded reason", got)
	}
}

// sessionSeedRow writes one chat-index row through the same START hook the live
// surfaces call, so the CLI arms below are driven against rows the real writers
// produce. logPath "" records none.
func sessionSeedRow(t *testing.T, sessionID, logPath string) {
	t.Helper()
	if _, err := tools.ChatEnsureRow(tools.ChatSession{
		SessionID: sessionID, Mode: tools.ChatModeCLI, Model: "glm-5.3-flash",
		FirstMsg: "cli presence sample " + sessionID, Workdir: "/tmp/proj", LogPath: logPath,
	}); err != nil {
		t.Fatalf("seed %s: %v", sessionID, err)
	}
}

// TestSessionListAllFlagDrivesTheLocalOnlyDefault is the CLI half of the ruled
// behaviour, driven through the REAL argv the dispatch calls: the default view
// is this machine's own sessions (and says what it dropped), and `--all` is the
// one spelling that brings the rest back, marked. The flag name is asserted
// against tools.ChatListAllFlag — the constant the disclosure line itself
// names — so a parser that accepted a different spelling would fail here.
func TestSessionListAllFlagDrivesTheLocalOnlyDefault(t *testing.T) {
	sessionChatEnv(t)
	presentID := "cli-presence-11111111-aaaa"
	missingID := "cli-presence-22222222-bbbb"
	have := writeCLISessionJSONL(t, presentID)
	void := filepath.Join(t.TempDir(), "not-here.jsonl")
	sessionSeedRow(t, presentID, have)
	sessionSeedRow(t, missingID, void)

	def := captureStdoutAndStderr(t, func() int { return runSessionList([]string{}) })
	if !strings.Contains(def, presentID) {
		t.Errorf("the default view dropped a session this machine holds:\n%s", def)
	}
	if strings.Contains(def, missingID) {
		t.Errorf("the default view listed a session whose log is not on this machine:\n%s", def)
	}
	if !strings.Contains(def, tools.ChatFileHiddenNote(1)) {
		t.Errorf("the default view hid a row without disclosing it (want %q):\n%s", tools.ChatFileHiddenNote(1), def)
	}
	if !strings.Contains(def, tools.ChatListAllFlag) {
		t.Errorf("the disclosure must name the revealing flag %q:\n%s", tools.ChatListAllFlag, def)
	}

	all := captureStdoutAndStderr(t, func() int { return runSessionList([]string{sessionAllFlag}) })
	if !strings.Contains(all, presentID) || !strings.Contains(all, missingID) {
		t.Errorf("%s must render the complete index:\n%s", sessionAllFlag, all)
	}
	if !strings.Contains(all, tools.ChatFileMissingPrefix) {
		t.Errorf("the re-admitted absent row carries no marker %q:\n%s", tools.ChatFileMissingPrefix, all)
	}
	if strings.Contains(all, tools.ChatFileHiddenNote(1)) {
		t.Errorf("a view that hid nothing carries a disclosure line:\n%s", all)
	}

	// The flag is the tools layer's own constant, not a second spelling: a
	// parser accepting "--everything" would violate this line's own promise.
	if sessionAllFlag != tools.ChatListAllFlag {
		t.Errorf("sessionAllFlag = %q but the disclosure names %q — one flag, one constant", sessionAllFlag, tools.ChatListAllFlag)
	}
	// An unrelated argument is still refused by name (the closed vocabulary).
	if got := runSessionList([]string{"--everything"}); got != 1 {
		t.Errorf("runSessionList(--everything) = %d, want 1", got)
	}
}

// TestSessionListJSONCarriesFilePresent: the machine-readable face carries the
// presence fact as a FIELD, and its disclosure rides stderr so stdout stays one
// parseable JSON document — a script must not have to strip prose.
func TestSessionListJSONCarriesFilePresent(t *testing.T) {
	sessionChatEnv(t)
	presentID := "cli-json-11111111-aaaa"
	missingID := "cli-json-22222222-bbbb"
	sessionSeedRow(t, presentID, writeCLISessionJSONL(t, presentID))
	sessionSeedRow(t, missingID, filepath.Join(t.TempDir(), "gone.jsonl"))

	out := captureStdoutAndStderr(t, func() int {
		return runSessionList([]string{sessionJSONFlag, sessionAllFlag})
	})
	start := strings.Index(out, "[")
	if start < 0 {
		t.Fatalf("the JSON face printed no array:\n%s", out)
	}
	var rows []sessionJSONRow
	if err := json.Unmarshal([]byte(out[start:]), &rows); err != nil {
		t.Fatalf("the JSON face is not one parseable document (%v):\n%s", err, out)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.SessionID] = r.FilePresent
	}
	if v, ok := got[presentID]; !ok || !v {
		t.Errorf("file_present for the readable row = %v (present in the array: %v), want true", v, ok)
	}
	if v, ok := got[missingID]; !ok || v {
		t.Errorf("file_present for the absent row = %v (present in the array: %v), want false", v, ok)
	}
}

// TestSessionUsageTeachesAllAndItsMarkers: the help is the PROMISE the render
// keeps. The page must name the flag it accepts and the marker its own render
// prints — a reader who follows the help must not be sent to a refused flag or
// left unable to read a marker they can see.
func TestSessionUsageTeachesAllAndItsMarkers(t *testing.T) {
	for _, want := range []string{tools.ChatListAllFlag, tools.ChatFileHiddenPrefix, tools.ChatFileMissingPrefix} {
		if !strings.Contains(sessionUsage, want) {
			t.Errorf("sessionUsage does not name %q — the page must teach the flag and the markers the render uses", want)
		}
	}
}

// writeCLISessionJSONL writes a minimal session log this machine really holds,
// at the sessions tree's own root layout, and returns its path — the "file IS
// here" case the default view must keep. The path is derived from the SAME
// profile the pinned GOPI_HOME names (Rule 13: the directory name comes from
// config, never from a re-typed ".gopi/sessions").
func writeCLISessionJSONL(t *testing.T, sessionID string) string {
	t.Helper()
	home := os.Getenv("GOPI_HOME")
	if home == "" {
		t.Fatal("GOPI_HOME is not pinned — call sessionChatEnv first")
	}
	dir := filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	line := `{"type":"session_start","timestamp":"2026-08-30T08:00:00Z","model":"glm-5.3-flash","sessionId":"` + sessionID + `"}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatalf("write session jsonl: %v", err)
	}
	return path
}

// TestSessionShowRendersUnknownTokensNotZero is the B face's CLI repro: the same
// row shape the tools test seeds (turns recorded, provider reported nothing)
// must reach the page as the unknown WORD, and a measured row must keep its
// numbers. It asserts on what a reader sees, not on the struct — the rendering
// is where the two zeros became indistinguishable.
func TestSessionShowRendersUnknownTokensNotZero(t *testing.T) {
	sessionChatEnv(t)
	const (
		silentID   = "cli-tok-unknown-11111111"
		measuredID = "cli-tok-measured-22222222"
	)
	for _, id := range []string{silentID, measuredID} {
		sessionSeedRow(t, id, writeCLISessionJSONL(t, id))
	}
	if err := tools.ChatRecordTurn(silentID, 0, 0, 0); err != nil {
		t.Fatalf("record silent turn: %v", err)
	}
	if err := tools.ChatRecordTurn(measuredID, 300, 50, 280); err != nil {
		t.Fatalf("record measured turn: %v", err)
	}

	silent := captureStdoutAndStderr(t, func() int { return runSessionShow([]string{silentID}) })
	if !strings.Contains(silent, tools.ChatTokensUnknownWord) {
		t.Errorf("`session show` on a session whose usage was never reported does not say %q:\n%s",
			tools.ChatTokensUnknownWord, silent)
	}
	if strings.Contains(silent, "in=0 out=0 cache=0") {
		t.Errorf("the unmeasured row still renders a measured-looking zero:\n%s", silent)
	}
	measured := captureStdoutAndStderr(t, func() int { return runSessionShow([]string{measuredID}) })
	if !strings.Contains(measured, "total tok in=300 out=50 cache=280") {
		t.Errorf("a row with real counts no longer prints them (labelled as the session total they are):\n%s", measured)
	}
	if strings.Contains(measured, tools.ChatTokensUnknownWord) {
		t.Errorf("a measured row is labelled unknown:\n%s", measured)
	}
}

// TestSessionShowExplainsAnAbsentLog is the C face's CLI repro: an indexed row
// whose log this machine does not hold must render the fact and a next step,
// never a sentence denying the session exists. The present case is the same
// render with the note ABSENT — a disclosure that fires on the normal case stops
// meaning anything (Rule 15).
func TestSessionShowExplainsAnAbsentLog(t *testing.T) {
	sessionChatEnv(t)
	const (
		hereID = "cli-log-here-11111111"
		awayID = "cli-log-away-22222222"
	)
	sessionSeedRow(t, hereID, writeCLISessionJSONL(t, hereID))
	sessionSeedRow(t, awayID, filepath.Join(t.TempDir(), "gone.jsonl"))

	away := captureStdoutAndStderr(t, func() int { return runSessionShow([]string{awayID}) })
	if strings.Contains(away, "no session with id") {
		t.Errorf("a session the index HAS is reported as not existing:\n%s", away)
	}
	if !strings.Contains(away, tools.ChatFileMissingWord) {
		t.Errorf("the absent-log row does not carry the missing word %q:\n%s", tools.ChatFileMissingWord, away)
	}
	if !strings.Contains(away, tools.ChatLogAbsentIndexNote) {
		t.Errorf("the absent-log row does not explain the gap and its next step (%q):\n%s", tools.ChatLogAbsentIndexNote, away)
	}
	if !strings.Contains(away, awayID) {
		t.Errorf("the page does not even name the session being summarised:\n%s", away)
	}

	here := captureStdoutAndStderr(t, func() int { return runSessionShow([]string{hereID}) })
	if strings.Contains(here, tools.ChatLogAbsentIndexNote) {
		t.Errorf("a row whose log IS on this machine carries the absent-log note:\n%s", here)
	}
	if strings.Contains(here, "  index ") {
		t.Errorf("a row whose log IS on this machine renders an index line — the line must exist only when there is a gap to explain:\n%s", here)
	}
}

// TestSessionShowJSONCarriesTokensReported: the machine-readable summary must
// carry the same fact the human page renders, so a script summing input_tokens
// can skip the rows where 0 means "nobody measured" (Rule 15: the structured
// face is not allowed to be the one that loses it).
func TestSessionShowJSONCarriesTokensReported(t *testing.T) {
	sessionChatEnv(t)
	const silentID = "cli-json-tok-unknown-1111"
	sessionSeedRow(t, silentID, writeCLISessionJSONL(t, silentID))
	if err := tools.ChatRecordTurn(silentID, 0, 0, 0); err != nil {
		t.Fatalf("record silent turn: %v", err)
	}
	out := captureStdoutAndStderr(t, func() int { return runSessionShow([]string{silentID, sessionJSONFlag}) })
	var payload struct {
		TokensReported bool `json:"tokens_reported"`
		InputTokens    int  `json:"input_tokens"`
	}
	start := strings.Index(out, "{")
	if start < 0 {
		t.Fatalf("the JSON face printed no object:\n%s", out)
	}
	if err := json.Unmarshal([]byte(out[start:]), &payload); err != nil {
		t.Fatalf("the JSON face is not one parseable object (%v):\n%s", err, out)
	}
	if payload.TokensReported {
		t.Errorf("tokens_reported = true for a session whose usage was never reported:\n%s", out)
	}
	if payload.InputTokens != 0 {
		t.Errorf("input_tokens = %d, want the stored default 0 (the field is what makes the 0 readable)", payload.InputTokens)
	}
}

// --- the family page's synopses are invocations, not shorthand ----------------

// sessionUsageColumnCut is the column at which a family-page synopsis ends: the
// page's own geometry puts the description in a column that opens with two or
// more spaces, and an invocation never contains a double space (the same cut the
// page's sibling derivations use — g4CutInvocation, cpUsageCommandLine).
const sessionUsageColumnCut = 2

// sessionUsageShorthandTokens returns the tokens of ONE synopsis that spell an
// ALTERNATIVE-BRANCH SHORTHAND — a single token carrying a bare `a|b`, which
// reads as a synopsis but is not one: the word is one token no subcommand
// dispatcher answers, so the line promises an invocation nobody can type.
//
// THE TWO FORMS THAT ARE *NOT* SHORTHAND ARE EXCLUDED BY CONSTRUCTION, because
// each is argv an operator really types or a legitimate end to a synopsis:
//
//   - a BRACKETED group (`[--mode chat|web]`) is an OPTIONAL argument list:
//     `--mode` takes one of the pipe-separated values, and the brackets are the
//     page's own way of saying so;
//   - a BARE `|` token is a SHELL PIPE: it ends the synopsis and starts a worked
//     example (`gopi session stats <id> --json | jq .prompt_tokens`).
//
// Both are measured on this page, so the guard's own control
// (TestSessionUsageShorthandInstrumentSeesTheShapes) drives them as GREEN cases:
// a check that flagged them would force a correct page to be rewritten.
func sessionUsageShorthandTokens(synopsis string) []string {
	var out []string
	depth := 0
	for _, tok := range strings.Fields(synopsis) {
		// A token carrying a bare pipe at BRACKET DEPTH 0 is the shorthand.
		// Depth is tracked because an optional group may SPAN tokens
		// (`[--mode chat|web]` is three of them), so a filter on the token's own
		// first character would read the group's middle token as shorthand.
		if depth == 0 && tok != "|" && strings.Contains(tok, "|") {
			out = append(out, tok)
		}
		depth += strings.Count(tok, "[") - strings.Count(tok, "]")
		if depth < 0 {
			depth = 0
		}
	}
	return out
}

// sessionUsageSynopses returns every command line on the family page: a line
// indented by two or more spaces that opens with the program word, cut at the
// description column. The page's prose opens at column 1, so this reads the
// synopsis block and nothing else.
func sessionUsageSynopses(page string) []string {
	var out []string
	for _, line := range strings.Split(page, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) < sessionUsageColumnCut {
			continue
		}
		if !strings.HasPrefix(trimmed, gopiProgramWord+" ") {
			continue
		}
		out = append(out, strings.TrimSpace(strings.SplitN(trimmed, "  ", sessionUsageColumnCut)[0]))
	}
	return out
}

// TestSessionFamilyUsageSynopsesAreRunnable pins the rule the checkpoint page
// learned the hard way (measured on master 2026-10-06): a line on a help page
// that READS as a synopsis — two-space indent, program word, invocation column —
// is a promise that an operator can type it, and every derivation that
// enumerates a page's advertised surface (G4's g4PageRows, the checkpoint
// dispatcher's own cpUsageCommandLines, a reader's eyes) is right to treat it as
// one. The alternative-branch shorthand belongs in PROSE, where a single
// token it is obviously a shorthand rather than a command word.
//
// WHY THE PAGE, AND NOT THE PROBE. The family page used to spell its checkpoint
// row as `gopi session checkpoint list|export`, which no dispatcher answers
// (`gopi session checkpoint list|export` → `unknown subcommand "list|export"`,
// exit 1 — measured). The gate below reads the SAME shape the probe does, so a
// shorthand re-introduced on this page fails HERE as well as at the point that
// tries to run it.
func TestSessionFamilyUsageSynopsesAreRunnable(t *testing.T) {
	synopses := sessionUsageSynopses(sessionUsage)
	// Non-vacuity: the page prints six synopses (one per sub-verb), so a
	// derivation that read none is blind and its green would prove nothing.
	const minSynopses = 6
	if len(synopses) < minSynopses {
		t.Fatalf("the derivation found only %d synopsis line(s) in sessionUsage — "+
			"the scanner is blind, so a green run would prove nothing:\n  %s",
			len(synopses), strings.Join(synopses, "\n  "))
	}
	for _, syn := range synopses {
		for _, bad := range sessionUsageShorthandTokens(syn) {
			t.Errorf("`gopi session -h` prints the synopsis %q, whose token %q is an "+
				"ALTERNATIVE-BRANCH SHORTHAND rather than a command word: no subcommand "+
				"dispatcher answers it, so the line promises an invocation nobody can type. "+
				"Spell one line per branch (what the checkpoint page does), or move the "+
				"shorthand into PROSE where it is not read as argv.", syn, bad)
		}
	}
	// Every derived synopsis must still name a DISPATCHED sub-verb: the page's
	// anti-omission guard (TestSessionFamilyUsageAdvertisesEverySubVerb) checks
	// the other direction; this one catches a row invented on the page.
	for _, syn := range synopses {
		fields := strings.Fields(syn)
		if len(fields) < 3 {
			continue
		}
		if !containsVerb(sessionSubcommandNames, fields[2]) {
			t.Errorf("the page prints `%s` but %q is not in sessionSubcommandNames — "+
				"an advertised sub-verb the group does not dispatch", syn, fields[2])
		}
	}
}

// TestSessionUsageShorthandInstrumentSeesTheShapes is the guard's own control:
// the instrument must fire on the shorthand and stay quiet on the two forms
// that are legitimate argv on these pages. A regex that flagged `[--mode
// chat|web]` or `--json | jq .prompt_tokens` would force a correct page to be
// rewritten; one that missed `list|export` would have let the defect ship.
func TestSessionUsageShorthandInstrumentSeesTheShapes(t *testing.T) {
	for _, bad := range []string{
		"gopi session checkpoint list|export",
		"gopi config completion bash|zsh|fish",
	} {
		if len(sessionUsageShorthandTokens(bad)) == 0 {
			t.Errorf("the instrument does not see the shorthand in %q — it is blind", bad)
		}
	}
	for _, ok := range []string{
		"gopi session checkpoint list [--dir DIR]",
		"gopi session checkpoint export <log>",
		"gopi session list [--mode chat|web] [--source WHO]",
		"gopi session stats <id> --json | jq .prompt_tokens",
		"gopi session raw <id> [round] [part]",
	} {
		if len(sessionUsageShorthandTokens(ok)) != 0 {
			t.Errorf("the instrument flags %q, which is argv an operator really types "+
				"(an OPTIONAL group or a shell PIPE) — a false positive", ok)
		}
	}
}
