package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// cli_keyword_search.go — `gopi tool keyword_search <query>`, the CLI face of the
// keyword_search TOOL.
//
// WHY THIS FILE EXISTS (task 01a0dde8…, EUAUDIT slice 4 TOOLS-1 F2): the
// top-level help promises "a tool-named command keeps the tool's own
// underscore spelling", but `gopi tool keyword_search` was NOT in the dispatch
// chain at all. A word that dispatches nowhere is not refused — it is a
// PROMPT, so the command fell through into the bare-prompt path and bought a
// real agent run (session dir + `[auto] keyword_search …` ledger row +
// tokens) for what the operator typed as a command. Measured before the fix:
// `gopi tool keyword_search -h` was BYTE-IDENTICAL to `gopi --help` (same md5) and
// the page did not contain the string `keyword_search` even once — the
// strongest possible signature of "there is no such command".
//
// The naming rule places this word (devdoc/cli-naming-convention.md rule 1):
// keyword_search is TOOL-DERIVED, so it is spelled with the tool's own
// underscore and takes its value from internal/toolnames — never a literal
// (Rules 1/13/14). Its sibling `code-search` deliberately does NOT mirror its
// tool's wire name (rule 2: dash-cased CLI verb), which is why `code_search`
// stays a NON-command and gets a pointed refusal instead (see
// cli_undispatched_tool_words.go).

// keywordSearchDefaultRoot is the search root when --root is omitted (the
// current directory — matches the agent loop's keywordSearchScope()).
const keywordSearchDefaultRoot = "."

// keywordSearchMaxResults caps the returned text hits (mirrors the agent
// loop's maxSearchResults, like codeSearchMaxResults, so the CLI never floods
// a script with one noisy query's full cluster).
const keywordSearchMaxResults = 50

// keywordSearchExitPathArg is the status of the THIRD reading of a zero-hit
// run, and it exists because two readings were not enough.
//
// DEFECT THIS NAMES (measured on the real binary, 2026-11-08):
//
//	$ gopi tool keyword_search "needle" internal/config
//	No matches.                       # exit 1
//
// Every positional word is joined into ONE query — deliberately, because
// `gopi tool keyword_search two words` is a supported two-word query — so the
// path silently became part of the search TEXT. "needle internal/config" is not
// a thing any tree contains, so the command always answered "No matches." with
// the miss code, byte-identical to a search of a real tree that genuinely holds
// no `needle`. The caller could not tell "your argument shape was wrong" from
// "the tree does not have it", and the second reading is a claim about a
// repository that was never asked anything.
//
// So the code SPLITS, and the split is the whole point (Rule 15 — a named
// rejection is not the answer "nothing"):
//
//	searchExitOK             the search RAN and printed hits
//	searchExitNoHits         the search RAN, on a real tree, and found nothing
//	keywordSearchExitPathArg the search RAN and found nothing, AND one of the
//	                         positional words names an EXISTING path — so the
//	                         words were probably `<query> <path>`, and the miss
//	                         is the CALLER's, not the tree's
//	searchExitWrongGround     the search could NOT run (no repository there)
//
// It is deliberately a NEW number rather than searchExitWrongGround: a path
// argument does not stop the search (the search really ran, and the exit-4 code
// states that nothing was searched — reusing it would be a lie), and it is not
// the plain miss either (that is the confusion this code removes).
//
// WHY THE HINT ONLY RIDES A MISS: a path-named word is not always a mistake —
// `gopi tool keyword_search internal` searches for the WORD "internal", and a
// tree holding a directory of that name must keep answering with its hits. The
// argument shape is only INDISTINGUISHABLE when there are no hits to explain it
// away, so a run that matched something keeps the exit-0 contract untouched and
// prints no hint.
const keywordSearchExitPathArg = 2

// keywordSearchPathArgLabel is the hint's one-line verdict, phrased so a reader
// (and a test) greps the same words the exit code means.
const keywordSearchPathArgLabel = "a positional word names an EXISTING path"

// keywordSearchPathArgHintFmt is the hint itself. It is a format string so
// every number and every command spelling in it comes from the constants that
// own them (Rules 1/13/14): the door's two words, the root flag, and BOTH exit
// codes the sentence compares. Args: (door word, leaf word, label, path, door
// word, leaf word, query, root flag, path, the plain-miss code).
//
// The remedy line spells the FULL working command — `gopi tool keyword_search`
// is the only spelling this binary dispatches (the flat leaf word alone is
// refused by name), and a remedy that has to be corrected into a second command
// is a dead end (Rule 15).
const keywordSearchPathArgHintFmt = "gopi %s %s: %s — %q\n" +
	"  a path is not searchable TEXT, so passing it beside the query makes the query the whole string you typed, which no tree contains.\n" +
	"  if you meant to search INSIDE that directory:\n    gopi %s %s %q --%s %s\n" +
	"  this is NOT the plain miss (exit %d is that one): the search RAN and found nothing, and the miss is probably the argument shape's, not the tree's.\n"

// keywordSearchPathArg returns the first positional word that names an EXISTING
// path on disk, or "" when the words cannot be read as `<query> <path>`.
//
// The two conditions are both load-bearing:
//
//   - FEWER THAN TWO words means there is no `<query> <path>` shape to mistake
//     — one word is the query, whatever it happens to name on disk (searching
//     for the word "internal" must stay a search).
//   - The word must EXIST. This is the only evidence the command has: without
//     it, any two-word query mentioning a directory name would be second-
//     guessed, and a hint that fires on healthy calls trains its reader to
//     ignore it (which is worse than no hint — Rule 15).
func keywordSearchPathArg(words []string) string {
	if len(words) < 2 {
		return ""
	}
	for _, w := range words {
		if w == "" || strings.HasPrefix(w, "-") {
			continue // a flag-shaped word is not a path the caller meant
		}
		if _, err := os.Stat(w); err == nil {
			return w
		}
	}
	return ""
}

// keywordSearchPathArgQueryFor returns the query the caller PROBABLY meant: the
// same words with the path argument taken out, joined the way a query is.
//
// The path word must be REMOVED, and this is not a nicety — measured on the
// first cut of the hint, which prescribed the whole joined sentence:
//
//	gopi tool keyword_search "PATHARG_NEEDLE_ZZZ sub" --root sub
//
// i.e. a remedy that repeats the very mistake it is diagnosing, and that finds
// nothing when pasted. A remedy has to be the command that WORKS, so the words
// are filtered and then joined by the same rule a query is (joinKeywordSearchQuery),
// never re-typed.
func keywordSearchPathArgQueryFor(words []string, pathArg string) string {
	kept := make([]string, 0, len(words))
	for _, w := range words {
		if w == pathArg {
			continue
		}
		kept = append(kept, w)
	}
	return joinKeywordSearchQuery(kept)
}

// keywordSearchPathArgHint renders the hint for the given path argument and the
// words that produced it. One renderer, so the live command and any test read
// the same words — and so the remedy it prints is the command a caller can
// paste, built from the query that EXCLUDES the path (keywordSearchPathArgQueryFor).
func keywordSearchPathArgHint(pathArg string, words []string) string {
	query := keywordSearchPathArgQueryFor(words, pathArg)
	return fmt.Sprintf(keywordSearchPathArgHintFmt,
		CliVerbTool, toolnames.ToolKeywordSearch, keywordSearchPathArgLabel, pathArg,
		CliVerbTool, toolnames.ToolKeywordSearch, query, searchRootFlag, pathArg,
		searchExitNoHits)
}

// keywordSearchUsage is the help text for the keyword_search CLI command. It
// is printed on -h/--help and on a usage error.
//
// Like its code-search sibling it is a FORMAT string with one %d per exit code:
// the printed code table and the process's os.Exit calls read the SAME constants
// (Rule 13). Render it with keywordSearchUsageText(), never by printing this
// constant — the %d's would reach the operator verbatim.
const keywordSearchUsage = `GoPi keyword_search command — expose the keyword_search tool as a CLI command.
Behavior is identical to the tool_use call (same function path — ripgrep over
the tree, no ctags index needed).

Usage:
  gopi tool keyword_search <query> [--root DIR]
      Full-text (regex) search of the current directory (or --root DIR) and
      print one line per match:
        <file>:<line>: <snippet>
      "No matches." is printed and the exit status is %d when nothing matches.

      SEVERAL WORDS ARE ONE QUERY — they are joined with single spaces, so
      quoting is optional and "gopi tool keyword_search two words" searches for
      the string "two words", never for "two" plus a path called "words":
        gopi tool keyword_search pointed hint          # ONE two-word query
      A DIRECTORY is never a positional argument. Pass it with --root, or the
      words are joined into the query and the search asks the tree for a
      sentence it cannot contain (see the exit table's third row).

  gopi tool keyword_search codeSearchScope            # search from the current dir
  gopi tool keyword_search "pointed hint" --root internal/config

  SESSION SCOPE — search ONE session's own on-disk record instead of the tree
  (the same call the keyword_search TOOL makes with scope=session):
    gopi tool keyword_search --scope session --session <id> "<query>"
    gopi tool keyword_search --scope session --session <id> --limit 20 "<query>"
    gopi tool keyword_search --scope session --session <id> --include-raw-log "<query>"
  The answer is the tool's structured page — one block per hit with its round,
  row type, timestamp, line number, snippet and the exact "gopi session log <id>
  <round>" command that opens it — printed by the same renderer the tool uses,
  so a terminal run and a tool_use run of one request are byte-identical.

Exit status:
  %d    at least one match printed
  %d    the search RAN on a real tree and found nothing ("No matches."), a usage
        error, or ripgrep failed (missing binary → a clear message naming it)
  %d    %s — "No matches." is printed and
        stderr carries a named hint (the path it saw, the exact --root command
        to run instead). The search RAN, so the argv probably meant
        "<query> <path>", and the miss is the argument shape's. This is NOT the
        row above: that one says the tree does not hold the query, this one
        says the query was probably never the query. It has its own code
        because a path argument does not stop the search — the search really
        ran — so reusing the REFUSED code below would claim nothing was
        searched. A run that MADE HITS keeps exit 0 and prints no hint:
        "gopi tool keyword_search internal" is a legitimate search for a word.
  %d    REFUSED — the search could NOT run: no code repository at the search
        root. The refusal names the absolute root, the channel it came from
        (--root or the default cwd), the git work tree it sits in, and what the
        walk saw. This is NOT "No matches.": nothing was searched, so a caller
        that only checks the miss code would read it as an answer about a tree
        that was never read. The same code is used by gopi tool code-search and by
        gopi task show --branch ("no row records it" is an answer, not a failure).

Flags:
  --scope VALUE
               which corpus to search. Only two values are accepted:
                 tree      (the default) the project tree under --root
                 session   ONE session's own record, named by --session
               Anything else is REFUSED by name. The value is the TOOL's own
               (scope), not a CLI-only spelling, because the acceptance is
               that this command and the tool_use call print the same bytes.

  --session ID|PATH
               the session to search: a bare session id, or an ABSOLUTE path to
               a session record. A bare id is resolved through the profile's
               sessions root in every shape the tree uses — a bare dir holding
               session.jsonl, a flat <id>.jsonl, and a task-run dir whose record
               is <id>/<id>.jsonl (or a per-run <uuid>.jsonl).
               REQUIRED with --scope session and REFUSED by name without it: a
               one-shot CLI process has no "current session", so there is
               nothing to default to and guessing would search someone else's
               log. An id that resolves to no record is REFUSED naming every
               shape that was tried, never answered with "no matches." (that
               sentence claims the log was read).

  --limit N    how many hits the page returns (default 5, ceiling %d). The cap
               is applied after attribution and any drop is REPORTED in the
               page footer, never silent. Requires --scope session.

  --include-raw-log
               also search the session's raw HTTP log — the same history
               re-sent on every turn, ~58x the bytes of the record. Off by
               default; when on, every hit is labelled with its source, and a
               raw-log hit carries a line number but NO round (the raw log's
               rounds are its own request ordinals). Requires --scope session.

  --root DIR   search root (default: the current directory — the command
               searches WHERE YOU STAND, so the cwd premise is part of the
               call). It must exist and hold at least one file this search can
               read that is NOT gopi's own state (the profile directory and a
               sandbox's ownership marker are not searchable material); a
               directory with nothing searchable is REFUSED (see the exit
               table above) and the refusal names what it found. Remedy: pass
               --root DIR naming the repository, or run the command from
               inside it.
               --root IS MEANINGLESS WITH --scope session — a session search
               reads one record file, not a tree — so passing both is REFUSED
               by name rather than having one of them silently win.
`

// keywordSearchUsageArgs returns the fmt arguments keywordSearchUsage expects,
// in the page's own order (the first is the inline "exit status is %d when
// nothing matches" sentence). One definition, read by both the renderer and the
// width sweep (Rule 14).
func keywordSearchUsageArgs() []any {
	return []any{searchExitNoHits, searchExitOK, searchExitNoHits, keywordSearchExitPathArg, keywordSearchPathArgLabel, searchExitWrongGround, tools.SessionSearchMaxLimit}
}

// keywordSearchUsageText renders the page above with the REAL exit codes, so the
// printed table cannot disagree with the code path (Rule 13).
func keywordSearchUsageText() string {
	return fmt.Sprintf(keywordSearchUsage, keywordSearchUsageArgs()...)
}

// isKeywordSearchSubcommand reports whether argv begins with the
// keyword_search command. When it does it runs the command and returns true
// (the caller must not treat the remaining args as an input file or a prompt).
func isKeywordSearchSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == toolnames.ToolKeywordSearch {
		runKeywordSearchSubcommand(args[1:])
		return true
	}
	return false
}

// keywordSearchOptions is the parsed result of a keyword_search invocation.
type keywordSearchOptions struct {
	query string
	root  string
	// rootGiven records whether --root was passed at all — see
	// codeSearchOptions.rootGiven: the refusal names the channel, and that fact
	// is destroyed by defaulting "" to ".".
	rootGiven bool
	// positionals are the words exactly as typed, BEFORE they were joined into
	// query. They are kept because the join is what makes a `<query> <path>`
	// invocation indistinguishable from a genuine miss: the reader that splits
	// them again is keywordSearchPathArg, and it must see the words, not the
	// sentence they became.
	positionals []string

	// scope is the TOOL's own value (tools.SessionSearchScopeSession or the
	// default ""), never a CLI-only spelling: the dispatch below branches on it
	// and hands it to the SAME function the agent loop calls.
	scope string
	// session is a session id OR an absolute session-record path; empty with
	// scope=session is REFUSED by name (there is no "current session" for a
	// one-shot CLI process, so guessing one would search someone else's log).
	session string
	// limit is the page size (0 = tools' own default). It is clamped there,
	// reported by the renderer, never silently.
	limit int
	// includeRawLog puts the session's raw HTTP log in scope — the tool's
	// `include_raw_log`. Only meaningful with scope=session, which is refused
	// below rather than ignored.
	includeRawLog bool
}

// parseKeywordSearchArgs parses args for the keyword_search command without
// touching the process (no os.Exit, no env mutation). It returns the parsed
// options or a usage-style error. Separate from dispatch so the parsing is
// unit-testable.
func parseKeywordSearchArgs(args []string) (*keywordSearchOptions, error) {
	fs := newMemoryFlagSet() // same hand-rolled `--name value` parser
	// --include-raw-log takes NO value: without this the parser's "a bare flag
	// takes the next token as its value" rule would eat the QUERY, so
	// `--scope session --session <id> --include-raw-log needle` would search
	// for nothing and report "missing <query>".
	fs.boolean(keywordSearchRawLogFlag)
	positionals, err := fs.parse(args)
	if err != nil {
		return nil, err
	}
	for _, fname := range fs.order {
		switch fname {
		case searchRootFlag,
			keywordSearchScopeFlag,
			keywordSearchSessionFlag,
			keywordSearchLimitFlag,
			keywordSearchRawLogFlag:
		default:
			return nil, fmt.Errorf("unknown flag --%s", fname)
		}
	}

	if len(positionals) == 0 {
		return nil, fmt.Errorf("missing <query> argument")
	}
	// Several words are one query: a shell splits an unquoted multi-word
	// regex, and refusing that would make the common `gopi tool keyword_search two
	// words` shape an error. joinKeywordSearchQuery keeps the words in order
	// and trims the result.
	query := joinKeywordSearchQuery(positionals)
	if query == "" {
		return nil, fmt.Errorf("missing <query> argument")
	}

	opts := &keywordSearchOptions{
		query:       query,
		root:        fs.get(searchRootFlag),
		rootGiven:   fs.has(searchRootFlag),
		positionals: positionals,
	}
	if opts.root == "" {
		opts.root = keywordSearchDefaultRoot
	}

	// The session scope's four flags. Each shape that cannot run is refused BY
	// NAME here (Rule 15) instead of being silently reinterpreted, because the
	// alternative — ignoring `--session` on a tree search — answers a different
	// question than the one typed.
	opts.scope = strings.TrimSpace(fs.get(keywordSearchScopeFlag))
	switch opts.scope {
	case "", keywordSearchScopeTreeName, tools.SessionSearchScopeSession:
	default:
		return nil, fmt.Errorf("unknown --%s %q (want %q or %q)",
			keywordSearchScopeFlag, opts.scope, keywordSearchScopeTreeName, tools.SessionSearchScopeSession)
	}
	opts.session = strings.TrimSpace(fs.get(keywordSearchSessionFlag))
	opts.includeRawLog = fs.has(keywordSearchRawLogFlag)

	if opts.scope != tools.SessionSearchScopeSession {
		// A tree search has no session, no page of hits to cap and no raw log.
		// Accepting the flags and doing nothing would be the silent-failure
		// shape this command's exit table was written to avoid.
		for _, stray := range []struct{ flag, why string }{
			{keywordSearchSessionFlag, "names a session; add --" + keywordSearchScopeFlag + " " + tools.SessionSearchScopeSession},
			{keywordSearchLimitFlag, "caps a session page; add --" + keywordSearchScopeFlag + " " + tools.SessionSearchScopeSession},
			{keywordSearchRawLogFlag, "widens a session search; add --" + keywordSearchScopeFlag + " " + tools.SessionSearchScopeSession},
		} {
			if fs.has(stray.flag) {
				return nil, fmt.Errorf("--%s %s — a %s search has no session", stray.flag, stray.why, keywordSearchScopeTreeName)
			}
		}
		return opts, nil
	}

	if opts.session == "" {
		return nil, fmt.Errorf("--%s %s needs --%s <id|absolute-path>: a one-shot CLI process has no current session, so there is nothing to default to",
			keywordSearchScopeFlag, tools.SessionSearchScopeSession, keywordSearchSessionFlag)
	}
	// --root has no meaning here: a session search reads ONE record file, not a
	// tree, so accepting the flag and ignoring it would let a caller believe
	// they had restricted the search to a directory (Rule 15 — one flag, one
	// meaning, and a contradiction refused by name).
	if fs.has(searchRootFlag) {
		return nil, fmt.Errorf("--%s has no meaning with --%s %s: a session search reads one record, not a tree (drop --%s, or drop --%s to search the tree)",
			searchRootFlag, keywordSearchScopeFlag, tools.SessionSearchScopeSession, searchRootFlag, keywordSearchScopeFlag)
	}
	if raw := strings.TrimSpace(fs.get(keywordSearchLimitFlag)); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil {
			return nil, fmt.Errorf("--%s %q is not a number", keywordSearchLimitFlag, raw)
		}
		if n < 1 {
			return nil, fmt.Errorf("--%s %d is not a page size (want 1 or more; the tool caps it at %s)", keywordSearchLimitFlag, n, strconv.Itoa(tools.SessionSearchMaxLimit))
		}
		opts.limit = n
	}
	return opts, nil
}

// keywordSearchRootFlag is the one flag accepted here, named once (Rule 13/14)
// because the parser, the usage text, the refusal line and the error path must
// agree on it: it IS the shared root flag (cli_search_root.go), so the two
// search verbs cannot document different spellsings of the same knob.
const keywordSearchRootFlag = searchRootFlag

// The session scope's flags — the CLI face of the keyword_search TOOL's
// `scope` / `session` / `limit` / `include_raw_log` arguments.
//
// WHY THEY EXIST (measured 2026-11-09, task 01a1bd40-8110): the tool's session
// scope was reachable ONLY through a tool_use call. `gopi tool keyword_search
// --scope session …` answered `unknown flag --scope`, so the operator could not
// reproduce a session search in a terminal, could not hand-check a session's
// history, and could not write a CLI-level regression for the feature at all —
// which is why the first report on it had to smuggle a throwaway Go test into
// the tree to get any evidence. A capability the model can use and the person
// cannot is half a feature.
//
// They are spelled after the TOOL's argument names, not after each other's CLI
// conventions, because the acceptance is "the CLI and the tool_use path print
// the same bytes" — one vocabulary makes that checkable by eye.
const (
	keywordSearchScopeFlag     = "scope"
	keywordSearchSessionFlag   = "session"
	keywordSearchLimitFlag     = "limit"
	keywordSearchRawLogFlag    = "include-raw-log"
	keywordSearchScopeTreeName = "tree" // the default scope, named for the -h page
)

// joinKeywordSearchQuery joins the positional words into ONE query (a query
// with spaces is legal — quoting is optional, not required).
func joinKeywordSearchQuery(words []string) string {
	joined := make([]string, 0, len(words))
	for _, w := range words {
		if t := strings.TrimSpace(w); t != "" {
			joined = append(joined, t)
		}
	}
	return strings.Join(joined, " ")
}

// runKeywordSearchSubcommand parses the command's own flags and dispatches to
// the shared tools.KeywordSearch function so CLI behavior is identical to
// tool_use. It exits the process with a non-zero status on argument/usage
// errors, no-match, or a backend (ripgrep) failure.
func runKeywordSearchSubcommand(args []string) {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Print(keywordSearchUsageText())
			os.Exit(0)
		}
	}

	opts, err := parseKeywordSearchArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n%s", toolnames.ToolKeywordSearch, err, keywordSearchUsageText())
		os.Exit(1)
	}

	// The session arm runs the SAME function the agent loop calls
	// (internal/agent/loop.go's keyword_search tool_use branch), with the same
	// options, so the two faces cannot print different bytes for one request.
	// It is dispatched BEFORE the tree arm on purpose: the tree arm resolves a
	// root and runs the ground guard, and a session search has neither.
	if opts.scope == tools.SessionSearchScopeSession {
		runKeywordSearchSession(opts)
		return
	}

	// Scope mirrors the agent loop's keywordSearchScope() (see loop.go): the
	// root is resolved to an absolute path so a relative --root means the same
	// tree regardless of where ripgrep is invoked from.
	root := opts.root
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	scope := tools.SearchScope{
		RootPath:    root,
		ExcludeGlob: []string{".git/**", "vendor/**"},
		MaxResult:   keywordSearchMaxResults,
	}

	// The shared ground guard (cli_search_root.go). This verb has NO include
	// glob (ripgrep reads every file), so a root is searchable as soon as its
	// walk finds ANY file — a directory with no files at all is the refusal.
	searchGuardOrExit("gopi "+toolnames.ToolKeywordSearch, root, searchRootChannel(opts.rootGiven), scope.IncludeGlob)

	hits, dropped, searchCap, err := tools.KeywordSearch(scope, opts.query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", toolnames.ToolKeywordSearch, err)
		os.Exit(1)
	}

	// A truncated READ is never rendered as a complete one: zero hits from a read
	// that stopped at the ceiling must not print "No matches." (that sentence
	// claims the query ran to the end of the data), and a partial list carries the
	// notice underneath it. FormatTextHitsWithCap owns both cases, and a
	// NON-truncated call is byte-identical to FormatTextHits.
	if searchCap.Stopped {
		fmt.Println(tools.FormatTextHitsWithCap(hits, dropped, searchCap))
		return
	}

	if len(hits) == 0 {
		// The THREE readings of a zero-hit run are kept apart (see
		// keywordSearchExitPathArg): a plain miss keeps "No matches." and the
		// miss code, while a miss whose positional words name an existing path
		// says so BY NAME and takes its own code — so a caller can tell "the
		// tree does not have it" from "your argument shape made it a sentence".
		if pathArg := keywordSearchPathArg(opts.positionals); pathArg != "" {
			fmt.Print("No matches.\n")
			fmt.Fprintf(os.Stderr, "%s", keywordSearchPathArgHint(pathArg, opts.positionals))
			os.Exit(keywordSearchExitPathArg)
		}
		fmt.Println("No matches.")
		os.Exit(searchExitNoHits)
	}

	fmt.Println(tools.FormatTextHits(hits, dropped))
}

// runKeywordSearchSession is the session scope's CLI arm: it builds the SAME
// tools.SessionSearchOptions the agent loop builds from a tool_use call and
// prints the SAME rendering (tools.FormatSessionSearchHits), so `Ran:` evidence
// taken from the terminal describes the model's path and vice versa.
//
// The two exit readings a caller needs are kept apart (Rule 15): a session
// search that RAN and found nothing is searchExitNoHits, while a REFUSED one —
// an id that resolves to no record, a session with no raw log, an unreadable
// page size — is an error naming its cause on stderr. Printing "no matches."
// for the second would claim the log was read when it never was.
func runKeywordSearchSession(opts *keywordSearchOptions) {
	res, err := tools.SessionSearch(tools.SessionSearchOptions{
		Query:         opts.query,
		SessionID:     opts.session,
		Limit:         opts.limit,
		IncludeRawLog: opts.includeRawLog,
	})
	if err != nil {
		// REFUSED, not missed. The session search could not run at all (an id
		// that resolves to no record, a session with no recorded raw log, an
		// unreadable record), so it takes searchExitWrongGround — the family's
		// "nothing was searched" code. Using the miss code here would claim the
		// log WAS read and held nothing, which is the confusion the exit table
		// exists to remove (Rule 15).
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", toolnames.ToolKeywordSearch, err)
		os.Exit(searchExitWrongGround)
	}
	fmt.Print(tools.FormatSessionSearchHits(res))
	if len(res.Hits) == 0 {
		os.Exit(searchExitNoHits)
	}
}
