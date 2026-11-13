package main

// cli_keyword_search_session_test.go — the CLI face of the keyword_search TOOL's
// SESSION scope.
//
// THE DEFECT THIS PINS (measured 2026-11-09, task 01a1bd40-8110): the tool's
// session scope (`scope=session`, `session`, `limit`, `include_raw_log`) was
// reachable ONLY through a tool_use call. `gopi tool keyword_search --scope
// session …` answered `unknown flag --scope`, so the operator could not
// reproduce a session search in a terminal, could not hand-check a session's
// history, and no CLI-level regression could exist at all. A capability the
// model can use and the person cannot is half a feature.
//
// Two layers, both necessary:
//  1. unit — the parser: the four flags are ACCEPTED with the tool's own
//     semantics, and every shape that cannot run is REFUSED BY NAME rather than
//     silently reinterpreted (Rule 15). Ignoring `--session` on a tree search is
//     the failure this half forbids: it answers a different question than the
//     one typed.
//  2. live binary — the acceptance itself: the same request must print the SAME
//     page through the CLI as through the tool (both call
//     tools.SessionSearch + tools.FormatSessionSearchHits — one function path,
//     never a second implementation).

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// kwSessionFixtureID is the id the fixtures below file their run record under.
// It is a REAL task-run shape: the directory is named after the id and the
// record inside it is named after the id too (`<root>/<id>/<id>.jsonl`), which
// is the fourth layout resolveSessionSearchTarget now covers.
const kwSessionFixtureID = "01a1bd40-8110-75e6-8858-e64e21db699b"

// kwSessionNeedle is built at run time and never spelled on disk by this
// package's own source... except that it IS spelled here — so the fixture's
// record is a TEMP file, never this tree, and the live-binary case searches the
// fixture by absolute path. Keeping the token distinctive is what stops a
// neighbouring search of the repository from finding it.
const kwSessionNeedle = "CLI_SESSION_SCOPE_NEEDLE_ZZZ"

// writeKwSessionRunFixture materialises a TASK-RUN session directory under a
// throwaway GOPI_HOME: `<home>/.gopi/sessions/<id>/<id>.jsonl` plus the side
// files a real run directory carries. The side files are load-bearing — a bare
// dir holding only a jsonl is a DIFFERENT layout (the bare one), so a fixture
// without them would not reproduce the shape that failed.
func writeKwSessionRunFixture(t *testing.T, home, id, needle string) string {
	t.Helper()
	dir := filepath.Join(home, ".gopi", "sessions", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	record := filepath.Join(dir, id+".jsonl")
	body := `{"type":"session_start","timestamp":"2026-11-09T04:27:46+08:00","iteration":0}` + "\n" +
		`{"type":"user","timestamp":"2026-11-09T04:27:47+08:00","iteration":0,"message":{"content":"` + needle + `"}}` + "\n" +
		`{"type":"request","timestamp":"2026-11-09T04:27:48+08:00","iteration":0}` + "\n"
	if err := os.WriteFile(record, []byte(body), 0o644); err != nil {
		t.Fatalf("write session record: %v", err)
	}
	for _, side := range []string{"output.log", "metadata.json", "task-report.md"} {
		if err := os.WriteFile(filepath.Join(dir, side), []byte("x"), 0o644); err != nil {
			t.Fatalf("write side file %s: %v", side, err)
		}
	}
	return record
}

// TestParseKeywordSearchSessionFlags pins the parser's session arm: the four
// flags are accepted with the TOOL's own values, and the two hard refusals hold.
func TestParseKeywordSearchSessionFlags(t *testing.T) {
	t.Run("the four flags parse into the tool's own option shape", func(t *testing.T) {
		opts, err := parseKeywordSearchArgs([]string{
			"--" + keywordSearchScopeFlag, tools.SessionSearchScopeSession,
			"--" + keywordSearchSessionFlag, kwSessionFixtureID,
			"--" + keywordSearchLimitFlag, "20",
			"--" + keywordSearchRawLogFlag,
			"needle",
		})
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if opts.scope != tools.SessionSearchScopeSession {
			t.Errorf("scope = %q, want %q (the TOOL's own value, never a CLI-only spelling)",
				opts.scope, tools.SessionSearchScopeSession)
		}
		if opts.session != kwSessionFixtureID {
			t.Errorf("session = %q, want %q", opts.session, kwSessionFixtureID)
		}
		if opts.limit != 20 {
			t.Errorf("limit = %d, want 20", opts.limit)
		}
		if !opts.includeRawLog {
			t.Error("include-raw-log was passed and did not reach the options")
		}
		if opts.query != "needle" {
			t.Errorf("query = %q, want \"needle\"", opts.query)
		}
	})

	t.Run("the tree scope is still the default and still needs no session", func(t *testing.T) {
		opts, err := parseKeywordSearchArgs([]string{"needle"})
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if opts.scope != "" {
			t.Errorf("scope = %q, want the empty default (the project tree)", opts.scope)
		}
		if opts.session != "" || opts.limit != 0 || opts.includeRawLog {
			t.Errorf("a bare tree search picked up session state: %+v", opts)
		}
	})

	t.Run("scope=tree is named and accepted", func(t *testing.T) {
		opts, err := parseKeywordSearchArgs([]string{"--" + keywordSearchScopeFlag, keywordSearchScopeTreeName, "needle"})
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if opts.scope != keywordSearchScopeTreeName {
			t.Errorf("scope = %q, want %q", opts.scope, keywordSearchScopeTreeName)
		}
	})

	// Every refusal below names its own cause (Rule 15). A parser that accepted
	// these and ignored the flag would answer a DIFFERENT question than the one
	// typed, which is the failure the exit table was written to make impossible.
	for _, tc := range []struct {
		name    string
		args    []string
		mustSay []string
	}{
		{
			name:    "an unknown scope is refused and both legal values are named",
			args:    []string{"--" + keywordSearchScopeFlag, "everything", "needle"},
			mustSay: []string{"everything", keywordSearchScopeTreeName, tools.SessionSearchScopeSession},
		},
		{
			name:    "scope=session without --session is refused (no current session to guess)",
			args:    []string{"--" + keywordSearchScopeFlag, tools.SessionSearchScopeSession, "needle"},
			mustSay: []string{keywordSearchSessionFlag, tools.SessionSearchScopeSession},
		},
		{
			name:    "--session on a TREE search is refused, not ignored",
			args:    []string{"--" + keywordSearchSessionFlag, kwSessionFixtureID, "needle"},
			mustSay: []string{keywordSearchSessionFlag, tools.SessionSearchScopeSession},
		},
		{
			name:    "--limit on a tree search is refused, not ignored",
			args:    []string{"--" + keywordSearchLimitFlag, "5", "needle"},
			mustSay: []string{keywordSearchLimitFlag, tools.SessionSearchScopeSession},
		},
		{
			name:    "--include-raw-log on a tree search is refused, not ignored",
			args:    []string{"--" + keywordSearchRawLogFlag, "needle"},
			mustSay: []string{keywordSearchRawLogFlag, tools.SessionSearchScopeSession},
		},
		{
			name:    "--root with scope=session is refused (it would restrict nothing)",
			args:    []string{"--" + keywordSearchScopeFlag, tools.SessionSearchScopeSession, "--" + keywordSearchSessionFlag, kwSessionFixtureID, "--" + searchRootFlag, ".", "needle"},
			mustSay: []string{searchRootFlag, tools.SessionSearchScopeSession},
		},
		{
			name:    "a non-numeric --limit is refused",
			args:    []string{"--" + keywordSearchScopeFlag, tools.SessionSearchScopeSession, "--" + keywordSearchSessionFlag, kwSessionFixtureID, "--" + keywordSearchLimitFlag, "many", "needle"},
			mustSay: []string{keywordSearchLimitFlag, "many"},
		},
		{
			name:    "a --limit below 1 is refused",
			args:    []string{"--" + keywordSearchScopeFlag, tools.SessionSearchScopeSession, "--" + keywordSearchSessionFlag, kwSessionFixtureID, "--" + keywordSearchLimitFlag, "0", "needle"},
			mustSay: []string{keywordSearchLimitFlag, "0"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseKeywordSearchArgs(tc.args)
			if err == nil {
				t.Fatalf("parse accepted %q — the shape cannot run and must be refused", tc.args)
			}
			for _, want := range tc.mustSay {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal does not name %q: %v", want, err)
				}
			}
		})
	}
}

// TestKeywordSearchUsageDocumentsTheSessionFlags is the page half: every flag the
// parser accepts is documented, and the two facts a caller cannot infer from the
// flag list — that scope=session needs a session, and that --root is meaningless
// there — are stated on the page (Rule 15: an accepted-but-undocumented flag is
// the "help promises, code refuses" class in reverse).
func TestKeywordSearchUsageDocumentsTheSessionFlags(t *testing.T) {
	page := keywordSearchUsageText()
	for _, flag := range []string{
		keywordSearchScopeFlag,
		keywordSearchSessionFlag,
		keywordSearchLimitFlag,
		keywordSearchRawLogFlag,
		searchRootFlag,
	} {
		if !strings.Contains(page, "--"+flag) {
			t.Errorf("the -h page does not document --%s, which the parser accepts:\n%s", flag, page)
		}
	}
	for _, want := range []string{
		tools.SessionSearchScopeSession,
		keywordSearchScopeTreeName,
		"IS MEANINGLESS WITH --" + keywordSearchScopeFlag + " " + tools.SessionSearchScopeSession,
		// The ceiling printed from the constant, never a second literal.
		strconv.Itoa(tools.SessionSearchMaxLimit),
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the -h page does not carry %q:\n%s", want, page)
		}
	}
}

// TestKeywordSearchSessionOnTheRealBinary is the ACCEPTANCE arm in a terminal
// shape: the fixture is a real task-run directory (`<id>/<id>.jsonl` beside
// output.log and friends) and the caller names it by BARE ID — the argument
// shape the tool's most obvious use produces.
//
// Before the fix this exact invocation exited non-zero with
// `unknown flag --scope`, and before the resolver fix even a successful
// dispatch answered "no session record" for the id.
func TestKeywordSearchSessionOnTheRealBinary(t *testing.T) {
	requireRipgrep(t)
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	writeKwSessionRunFixture(t, home, kwSessionFixtureID, kwSessionNeedle)

	// cwd is deliberately NOT a repository: a session search reads one record and
	// must not need a tree (the ground guard belongs to the tree scope).
	cwd := t.TempDir()

	out, code := searchProbe(t, bin, home, cwd,
		toolnamesToolKeywordSearch(), "--"+keywordSearchScopeFlag, tools.SessionSearchScopeSession,
		"--"+keywordSearchSessionFlag, kwSessionFixtureID,
		kwSessionNeedle)
	if code != searchExitOK {
		t.Fatalf("`gopi tool keyword_search --scope session --session %s <query>` exited %d, want %d — the session scope is unreachable from a terminal\n%s",
			kwSessionFixtureID, code, searchExitOK, out)
	}
	for _, want := range []string{
		"session " + kwSessionFixtureID,
		"query \"" + kwSessionNeedle + "\"",
		"1 hit(s)",
		"→ gopi session log " + kwSessionFixtureID,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the page does not carry %q — the CLI must print the TOOL's own structured page:\n%s", want, out)
		}
	}
	// The page must name the record it actually searched: a caller checking the
	// answer against a directory needs to see the fourth layout resolved.
	if !strings.Contains(out, filepath.Join(kwSessionFixtureID, kwSessionFixtureID+".jsonl")) {
		t.Errorf("the page does not name the run-record layout it resolved:\n%s", out)
	}

	t.Run("a miss is the miss code, and the page SAYS no matches", func(t *testing.T) {
		missOut, missCode := searchProbe(t, bin, home, cwd,
			toolnamesToolKeywordSearch(), "--"+keywordSearchScopeFlag, tools.SessionSearchScopeSession,
			"--"+keywordSearchSessionFlag, kwSessionFixtureID,
			"zzzNoSuchToken"+uniqueSuffix(t))
		if missCode != searchExitNoHits {
			t.Fatalf("exit = %d, want %d (the search RAN and found nothing)\n%s", missCode, searchExitNoHits, missOut)
		}
		if !strings.Contains(missOut, "no matches.") {
			t.Errorf("the miss page does not say so:\n%s", missOut)
		}
	})

	t.Run("an id that resolves to no record names EVERY layout it tried", func(t *testing.T) {
		refOut, refCode := searchProbe(t, bin, home, cwd,
			toolnamesToolKeywordSearch(), "--"+keywordSearchScopeFlag, tools.SessionSearchScopeSession,
			"--"+keywordSearchSessionFlag, "01a1bd40-0000-0000-0000-000000000000",
			"needle")
		if refCode == searchExitOK || refCode == searchExitNoHits {
			t.Fatalf("exit = %d — a session that does not resolve must be REFUSED, never answered as a miss (that sentence claims the log was read)\n%s",
				refCode, refOut)
		}
		for _, want := range []string{"no session record", "session.jsonl", ".jsonl"} {
			if !strings.Contains(refOut, want) {
				t.Errorf("the refusal does not carry %q:\n%s", want, refOut)
			}
		}
	})

	t.Run("--scope session without --session is refused on the real binary too", func(t *testing.T) {
		noSessOut, noSessCode := searchProbe(t, bin, home, cwd,
			toolnamesToolKeywordSearch(), "--"+keywordSearchScopeFlag, tools.SessionSearchScopeSession,
			"needle")
		if noSessCode == searchExitOK {
			t.Fatalf("a session search with no session exited 0\n%s", noSessOut)
		}
		if !strings.Contains(noSessOut, keywordSearchSessionFlag) {
			t.Errorf("the refusal does not name the missing flag:\n%s", noSessOut)
		}
	})
}

// toolnamesToolKeywordSearch returns the command's own spelling from the ONE
// constant that owns it (Rules 1/13/14) — never a literal in a test.
func toolnamesToolKeywordSearch() string { return toolnames.ToolKeywordSearch }
