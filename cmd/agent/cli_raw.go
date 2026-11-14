package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/tools"
)

// rawUsage is the help text of the `gopi session raw` verb: the verbatim view of what
// the raw HTTP logger recorded (FINAL-USAGE-PROPOSAL — "线上实际收发什么字节").
// It is the successor of the byte half of the old `gopi session log --raw/--http N`
// surface.
//
// Design rule kept from the usage proposal: raw = BYTES. No --json/--text/
// --format flags exist here (the request body IS JSON already — pipe to jq),
// and stdout stays pure: no banners, no headers, nothing that would break
// `gopi session raw <id> 39 request | jq .` or `> file`.
const rawUsage = `gopi session raw — GoPi raw HTTP log, byte for byte (verbatim)

Usage:
  gopi session raw <session-id>                    Whole raw log verbatim (auto-paired)
  gopi session raw <session-id> <round>            One round's full body
  gopi session raw <session-id> <round> request    Round N request body VERBATIM
  gopi session raw <session-id> <round> response   Round N response VERBATIM
  gopi session raw <session-id> --rounds           Round index (round# → line span → bytes)
  gopi session raw <session-id> --lines A-B        Verbatim bytes in a raw-log line span
  gopi session raw <session-id> --sysprompt        System prompt as sent (first request)
  gopi session raw <session-id> --userprompt       User message as sent (first request)
  gopi session raw --file PATH                     Read a raw log file directly
  gopi session raw --branch BRANCH                 The session the row owning BRANCH is
                                           running — the SAME input as
                                           <session-id>, named by what
                                           ` + "`git branch --show-current`" + `
                                           prints in a task worktree (--worktree
                                           PATH takes pwd instead); refused
                                           beside <session-id>, --file, --last
  gopi session raw --last [N]                      Most recent session(s) with a raw log

Rules:
  - raw prints BYTES. There are no --json/--text/--format flags: the request
    body is JSON already, so structure questions go to jq
    (gopi session raw <id> 39 request | jq .). Passing one of them is an error, never
    a silent reinterpretation.
  - stdout is pure: no banner, no header, no decoration. Diagnostics (a
    truncated tail, a separator between sessions of --last N) go to stderr.
  - Output is capped at 64 MiB, cut on a rune boundary (never mid-CJK).
  - session-id and --file are EXCLUSIVE: pick ONE input, never both.

<round> is the SHARED round selector — the same grammar ParseHTTPSelector
accepts (and the same one the historical "gopi session log --http" accepted), so a
selector means the same thing on every surface that names a round:

  3      the 3rd round        0      the last round
  last   the last round       -2     the 2nd-from-last round

all  is a LISTING selector: it names no single round, so as a <round> it is
refused (use --rounds for the round index).

Round numbers are the raw log's own ordinals (1-based, guard probes included —
annotated markers are their own rounds). gopi session stats <id> --rounds shows the
same numbering, so a round number moves between the two commands unchanged.

"Request" means the HTTP request message, verbatim: the full JSON body sent to
the provider, whose "messages" array carries the conversation. The user's own
prompt is not a separate artifact — it is messages[0] of round 1's body — so
gopi session raw <id> 1 request | jq '.messages[0]'  is the way to read it back, and
--userprompt prints that message's text directly (as sent, agent-appended
directives included: the wire does not separate them from what was typed).
Raw logs are created exclusively, so one file belongs to one session; a log
that predates that fix and is recorded by several sessions says so on stderr
(its round numbers span sessions and cannot be attributed from the file alone).
`

// The raw verb's flag and positional spellings (PROJECT.md Rule 1b/13: one
// definition, referenced — never retyped). They are referenced by rawUsage,
// rawFlagMember, parseRawArgs' reads, the dispatch switch and cli_log.go's
// logMovedPointer pointer text, so a rename cannot leave one surface spelling a
// flag or part the parser no longer accepts.
const (
	// rawFlagFile is the `--file` input selector's WORD (the parser compares bare
	// words and prepends `--`). It is an ALIAS, not a literal, because the flag
	// has a second surface now: the chat system prompt renders the same spelling
	// as an invocation (`gopi session raw --file <path>` — internal/agent/chat_context.go),
	// and internal/agent cannot import package main. The ONE definition is
	// cliverbs.RawFileWord (Rule 13/14); internal/cliverbs'
	// TestSenseFlagsMatchCmdAgentDispatch reads rawFlagMember and the prompt's
	// constant to prove the two still agree.
	rawFlagFile       = cliverbs.RawFileWord
	rawFlagRounds     = "rounds"
	rawFlagLines      = "lines"
	rawFlagSysprompt  = "sysprompt"
	rawFlagUserprompt = "userprompt"
	rawFlagLast       = "last"
	// rawFlagBranch / rawFlagWorktree are the DELIVERABLE SELECTORS — the same
	// two spellings `gopi session log` / `gopi session stats` / `gopi task show` accept, taken
	// from the ONE definition in cli_session_selector.go (Rule 1b/13).
	rawFlagBranch   = sessionBranchFlag
	rawFlagWorktree = sessionWorktreeFlag
)

// The two sides of a round `gopi session raw <id> <round> [request|response]` can print
// verbatim: one spelling shared by parseRawArgs, printRawRound's dispatch and
// cli_log.go's pointer text.
const (
	rawPartRequest  = "request"
	rawPartResponse = "response"
)

// rawFlagMember reports whether a flag name is valid for the raw verb.
func rawFlagMember(flag string) bool {
	// Read from the verb's ONE table (cli_session_flag_arity.go): admission and
	// arity are the same declaration, so a flag cannot be admitted here while
	// the scanner still guesses it claims a value.
	_, admitted := rawFlagArity(flag)
	return admitted
}

// rawOptions is the parsed result of a `gopi session raw` invocation.
type rawOptions struct {
	// sessionID is the positional <session-id> ("" when --file is used).
	sessionID string
	// sel is the positional <round> selector. It is parsed by the SHARED
	// gopilog.ParseHTTPSelector grammar ("last" / 0 / negative / positive), so
	// gopi session raw accepts one language on every surface naming a round; the zero
	// value means "no round named" (Detail=false) and the flag modes below
	// apply. Resolution against the parsed round list goes through
	// rawRoundPosition/gopilog.HTTPDetailIndex, which is length-independent
	// (never opts.sel.Round-1 arithmetic).
	sel gopilog.HTTPRoundSelector

	// part is the positional request|response selector ("" = whole round).
	part string
	// dsel is the DELIVERABLE SELECTOR (--branch / --worktree): the SAME input
	// as sessionID, named by what the operator standing in a task worktree
	// holds. At most one spelling is set; it is resolved to a session id before
	// dispatch (sessionIDForSelector) and never carried into the dispatch as a
	// second way to address a session. (It is named dsel, not sel: sel below is
	// the ROUND selector — two different selectors, two names.)
	dsel tools.DeliverableSelector
	// filePath is the --file value (exclusive with sessionID).
	filePath string
	// roundsIndex asks for the round index table instead of bytes.
	roundsIndex bool
	// lineSpan is the parsed --lines A-B (0 = none).
	lineFirst, lineLast int
	// sysprompt asks for the first request's system prompt.
	sysprompt bool
	// userprompt asks for the first request's user message (the user's own
	// input as sent).
	userprompt bool
	// lastN > 0 means --last [N] was given (1 when no explicit N).
	lastN int
}

// rejectedRawFlags are the format flags `gopi session log` used to accept. `raw` is a
// byte view: a format flag has no meaning here, and silently ignoring one
// would hand the user reinterpreted output. They are refused by name.
var rejectedRawFlags = []string{"json", "text"}

// parseRawArgs parses the args of the raw verb without touching the process.
func parseRawArgs(args []string) (*rawOptions, error) {
	for _, a := range args {
		if !strings.HasPrefix(a, "--") {
			continue
		}
		name := strings.TrimPrefix(a, "--")
		if eq := strings.Index(name, "="); eq >= 0 {
			name = name[:eq]
		}
		for _, bad := range rejectedRawFlags {
			if name == bad {
				return nil, fmt.Errorf("--%s is not a raw flag: raw prints verbatim bytes (use gopi session log <id> for formatted content, or pipe to jq)", bad)
			}
		}
	}

	fs := newMemoryFlagSet()
	// The flags' ARITY is declared BEFORE the scan: a valueless flag (--rounds,
	// --sysprompt, --userprompt) must never claim the next token as its value,
	// or it eats the positional <session-id> standing beside it. It also refuses
	// an unknown flag by NAME, on the set actually typed
	// (cli_session_flag_arity.go).
	if err := sessionFlagDeclare(fs, args, rawFlagArity); err != nil {
		return nil, err
	}
	positionals, err := fs.parse(args)
	if err != nil {
		return nil, err
	}

	opts := &rawOptions{
		roundsIndex: fs.has(rawFlagRounds),
		sysprompt:   fs.has(rawFlagSysprompt),
		userprompt:  fs.has(rawFlagUserprompt),
		filePath:    fs.get(rawFlagFile),
	}

	if fs.has(rawFlagLines) {
		first, last, err := parseRawLineSpan(fs.get(rawFlagLines))
		if err != nil {
			return nil, err
		}
		opts.lineFirst, opts.lineLast = first, last
	}

	if fs.has(rawFlagLast) {
		v := strings.TrimSpace(fs.get(rawFlagLast))
		if v == "" {
			opts.lastN = 1
		} else {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("--%s must be a positive integer, got %q", rawFlagLast, fs.get(rawFlagLast))
			}
			opts.lastN = n
		}
	}

	// The DELIVERABLE SELECTOR (--branch / --worktree) is read BEFORE the
	// positional slots are computed, because it decides them: the selector names
	// the session, so the positionals after it shift one to the left.
	sel, err := sessionSelectorFromArgs(args)
	if err != nil {
		return nil, err
	}
	opts.dsel = sel
	var otherInputs []string
	if opts.filePath != "" {
		otherInputs = append(otherInputs, "--"+rawFlagFile)
	}
	if opts.lastN > 0 {
		otherInputs = append(otherInputs, "--"+rawFlagLast)
	}
	if err := sessionSelectorInputCheck(sel, opts.sessionID, otherInputs); err != nil {
		return nil, err
	}

	// Positionals. With a session id: <session-id> [<round>] [request|response].
	// With --file the file IS the input, so the first positional is the round:
	// --file PATH [<round>] [request|response]. The --branch/--worktree selector
	// occupies the session-id slot and therefore shifts them the same way.
	maxPos := 3
	roundIdx, partIdx := 1, 2
	if opts.filePath != "" {
		maxPos, roundIdx, partIdx = 2, 0, 1
		// A first positional that is not a round selector means the user
		// passed BOTH inputs — the exclusivity rule, not a bad round number.
		// The test is the SHARED grammar, so `--file PATH last` names a round.
		if len(positionals) > 0 {
			if _, err := gopilog.ParseHTTPSelector(positionals[0]); err != nil {
				return nil, fmt.Errorf("session-id and --file are EXCLUSIVE — got both %q and --file %s",
					positionals[0], opts.filePath)
			}
		}
	}
	if opts.filePath == "" && (sel.Branch != "" || sel.Worktree != "") {
		maxPos, roundIdx, partIdx = 2, 0, 1
	}
	if len(positionals) > maxPos {
		return nil, fmt.Errorf("unexpected extra argument %q", positionals[maxPos])
	}
	if opts.filePath == "" && len(positionals) >= 1 && opts.dsel.Branch == "" && opts.dsel.Worktree == "" {
		// Without a deliverable selector the first positional IS the session id;
		// WITH one it is the ROUND, because the selector already named the
		// session (the slots below are shifted).
		opts.sessionID = positionals[0]
	}
	if len(positionals) > roundIdx {
		// The <round> positional is the SHARED gopilog grammar — the same
		// parser every surface naming a round uses, so "last" / "0" / "-2" /
		// "3" mean here exactly what they mean there. Before this, raw
		// re-implemented the selector with a positive-only rule, so 100% of
		// the documented grammar was unreachable from the CLI.
		sel, err := gopilog.ParseHTTPSelector(positionals[roundIdx])
		if err != nil {
			if opts.dsel.Branch != "" || opts.dsel.Worktree != "" {
				// The operator likely typed the session twice (selector +
				// positional): with a deliverable selector the positional is the
				// ROUND, so the shared parser's refusal is completed by the shape
				// it was measured against (Rule 15: name the real problem).
				return nil, fmt.Errorf("%w — with --%s/--%s naming the session, the positional here is <round> [%s|%s]",
					err, rawFlagBranch, rawFlagWorktree, rawPartRequest, rawPartResponse)
			}
			return nil, err
		}
		if !sel.Detail {
			// "all" is a LISTING selector: it names no single round, so as a
			// positional it is a mistake, not a request for round 0. The
			// message says what a round selector IS, and where the listing
			// lives — never presenting the listing keyword as round.
			return nil, fmt.Errorf("<round> must name one round — a round number, 0, or %s/-N; %q asks for the whole-round listing (use --%s)",
				gopilog.RoundKeywordLast, positionals[roundIdx], rawFlagRounds)
		}
		opts.sel = sel
	}
	if len(positionals) > partIdx {
		part := strings.ToLower(positionals[partIdx])
		if part != rawPartRequest && part != rawPartResponse {
			return nil, fmt.Errorf("the last argument is %s|%s, got %q", rawPartRequest, rawPartResponse, positionals[partIdx])
		}
		opts.part = part
	}

	// Exactly one INPUT — counted over every spelling this verb accepts: the
	// positional <session-id> (or the --branch/--worktree selector, which names
	// that same input), --file PATH, or --last [N]. The checks above refuse the
	// PAIRS by name, because they can say WHY; this one is the plurality itself,
	// and it is the only check that catches ZERO inputs. Without it
	// `gopi session raw` with no argument falls through with an empty session id and
	// hands it to FindSession, whose "no id" behaviour is to LIST every session
	// on the box — an invocation that named nothing answered with everything
	// (Rule 15: name the real problem). Restored verbatim in spirit from the
	// form this verb carried before 3272c1d3 dropped it, and spelled like its
	// sibling `gopi session stats` (cli_stats.go: checkStatsArgs) so the two verbs
	// answer a nameless invocation the same way.
	selectors := 0
	if opts.sessionID != "" || opts.dsel.Branch != "" || opts.dsel.Worktree != "" {
		selectors++
	}
	if opts.filePath != "" {
		selectors++
	}
	if opts.lastN > 0 {
		selectors++
	}
	if selectors != 1 {
		return nil, fmt.Errorf("pick one input: <session-id>, --%s <branch>, --%s <path>, --%s PATH or --%s [N]",
			rawFlagBranch, rawFlagWorktree, rawFlagFile, rawFlagLast)
	}

	// Modes that need no round/part must not be combined with them.
	mode := 0
	for _, b := range []bool{opts.roundsIndex, opts.sysprompt, opts.userprompt, opts.lineFirst > 0} {
		if b {
			mode++
		}
	}
	if mode > 1 {
		return nil, fmt.Errorf("--rounds, --lines, --sysprompt and --userprompt are separate modes — pick one")
	}
	if opts.sel.Detail && (opts.roundsIndex || opts.sysprompt || opts.userprompt || opts.lineFirst > 0) {
		return nil, fmt.Errorf("<round> cannot be combined with --%s/--%s/--%s/--%s",
			rawFlagRounds, rawFlagLines, rawFlagSysprompt, rawFlagUserprompt)
	}
	if !opts.sel.Detail && opts.part != "" {
		return nil, fmt.Errorf("%s|%s needs a <round>: gopi session raw <id> <round> %s", rawPartRequest, rawPartResponse, opts.part)
	}
	return opts, nil
}

// parseRawLineSpan parses --lines A-B (also a single A for one line).
func parseRawLineSpan(v string) (int, int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, 0, fmt.Errorf("--lines needs a span A-B")
	}
	firstS, lastS, two := strings.Cut(v, "-")
	first, err := strconv.Atoi(strings.TrimSpace(firstS))
	if err != nil || first <= 0 {
		return 0, 0, fmt.Errorf("--lines expects A-B with positive line numbers, got %q", v)
	}
	if !two {
		return first, first, nil
	}
	lastS = strings.TrimSpace(lastS)
	if lastS == "" {
		// A- means "to the end of the log".
		return first, -1, nil
	}
	last, err := strconv.Atoi(lastS)
	if err != nil || last < first {
		return 0, 0, fmt.Errorf("--lines expects A-B with B >= A, got %q", v)
	}
	return first, last, nil
}

// isRawSubcommand reports whether argv begins with the raw verb.
func isRawSubcommand(args []string) bool {
	if len(args) == 0 || args[0] != CliVerbRaw {
		return false
	}
	for _, a := range args[1:] {
		if a == "-h" || a == "--help" {
			fmt.Print(rawUsage)
			os.Exit(0)
		}
	}
	runRawSubcommand(args[1:])
	return true
}

// runRawSubcommand parses and dispatches `gopi session raw`, exiting non-zero on any
// argument/session error. stdout carries bytes only; every diagnostic goes to
// stderr.
func runRawSubcommand(args []string) {
	opts, err := parseRawArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session raw: %v\n%s", err, rawUsage)
		os.Exit(1)
	}

	switch {
	case opts.lastN > 0:
		runRawLast(opts.lastN)
		return
	}

	// The DELIVERABLE SELECTOR (--branch / --worktree) resolves to the session
	// id the row behind it is running; from here on the verb is the ordinary
	// one — the id is a SUBSTITUTION into the existing resolution, so the raw
	// log is still paired by gopilog's own resolver (one answer to "which raw
	// log belongs to this session?", see cli_session_selector.go).
	if opts.dsel.Branch != "" || opts.dsel.Worktree != "" {
		id, err := sessionIDForSelector(CliVerbRaw, opts.dsel)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		opts.sessionID = id
	}

	// One input: the session's paired raw log, or an explicit --file.
	rawLog := opts.filePath
	if rawLog == "" {
		rawLog = resolveRawForSession(opts.sessionID)
	}

	switch {
	case opts.roundsIndex:
		printRawRoundsIndex(rawLog)
	case opts.sysprompt:
		printRawSysprompt(rawLog)
	case opts.userprompt:
		printRawUserprompt(rawLog)
	case opts.lineFirst > 0:
		printRawLines(rawLog, opts)
	case opts.sel.Detail:
		printRawRound(rawLog, opts)
	default:
		// No mode: the whole raw log verbatim (the `gopi session raw <session-id>` /
		// `gopi session raw --file PATH` form).
		printRawFile(rawLog)
	}
}

// missingRawLogNotice reports the session that RECORDED a raw log whose file is
// gone — a different answer from one that never had a log, and the answer the
// timestamp pairing would bury: it scans the legacy flat raw dir and reports
// "no raw logs found in <gopi dir>/raw", naming a directory this session never
// used and hiding the fact that a log WAS recorded here (the scratch GC
// collecting an old run's assets is the usual cause). "" means the session
// either has its log or never recorded one, so the caller resolves as usual.
func missingRawLogNotice(sessionID, sessionPath string) string {
	recorded, ok := gopilog.SessionRecordedRawLog(sessionPath)
	if ok || recorded == "" {
		return ""
	}
	return fmt.Sprintf("Error: session %s recorded raw log %s, but that file does not exist.\n"+
		"The session record is intact (%s) — only the raw HTTP log is gone, most often collected by the\n"+
		"scratch GC once the run was old. Nothing can reconstruct it: the raw log is the only byte-level\n"+
		"record of what went on the wire.\n", sessionID, recorded, sessionPath)
}

// resolveRawForSession pairs a session id with its raw log.
func resolveRawForSession(sessionID string) string {
	dir, err := gopilog.ResolveSessionDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	path, err := gopilog.FindSession(dir, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if path == "" {
		fmt.Fprintf(os.Stderr, "Session not found: %s\n", sessionID)
		os.Exit(1)
	}
	// A session that RECORDED a raw log whose file is gone is a different
	// answer from one that never had one — name the missing file instead of
	// reporting a directory this session never used.
	if notice := missingRawLogNotice(sessionID, path); notice != "" {
		fmt.Fprint(os.Stderr, notice)
		os.Exit(1)
	}
	rawLog, err := gopilog.ResolveRawLogForSession(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	warnSharedRawLog(path, rawLog)
	return rawLog
}

// warnSharedRawLog reports a raw log that more than one session recorded as
// its own. The raw view's contract is "round N = the raw log's Nth → REQUEST",
// which maps to "this session's Nth send" only while one file belongs to one
// session. Older logs written before the exclusive-create fix can hold two
// sessions' traffic interleaved (two agents resolved the same second-granularity
// name and both O_APPENDed) — there round N may be another session's request
// and the user's own first prompt/epic goal is simply not round 1. Printing
// that on STDERR is the difference between a raw view the user can trust and
// one that silently hands back foreign bytes; stdout stays byte-pure.
func warnSharedRawLog(sessionPath, rawLog string) {
	ids := gopilog.SessionsRecordingRawLog(rawLog)
	if len(ids) <= 1 {
		return
	}
	self := gopilog.FileToID(sessionPath)
	var others []string
	for _, id := range ids {
		if id != self {
			// FULL ids, never a leading block: this warning exists so the
			// operator can go LOOK at the other sessions (`gopi session log <id>`), and
			// a truncated id is refused there (epic full-id-no-prefix).
			others = append(others, id)
		}
	}
	fmt.Fprintf(os.Stderr, "[gopi session raw] %s is recorded by %d sessions (%s among others) — their HTTP traffic is interleaved in this file, so round numbers are NOT session-scoped: round N may belong to another session and this session's first request may not be round 1. Read it as a plain byte stream with --file, or match rounds by content.\n",
		filepath.Base(rawLog), len(ids), strings.Join(others, ", "))
}

// printRawFile prints a raw log file verbatim (the --file path).
func printRawFile(path string) {
	content, err := gopilog.FormatRawLog(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading raw log %s: %v\n", path, err)
		os.Exit(1)
	}
	// An empty log prints nothing at all, which is indistinguishable from a
	// broken verb: the user sees a command that "did nothing". Say what the
	// file is — 0 bytes of captured traffic — so the answer is a fact rather
	// than silence. (Raw logs written before the creation header are empty for
	// exactly this reason; new ones always carry it.)
	if len(content) == 0 {
		fmt.Fprintf(os.Stderr, "[gopi session raw] %s is 0 bytes: the log exists but no HTTP byte was ever captured for this run (it ended before its first request), or it was written by a gopi build predating the raw log's creation header. This is not a read failure.\n",
			filepath.Base(path))
	}
	fmt.Print(content)
}

// runRawLast prints the most recent sessions' raw logs verbatim. One session
// is byte-pure; several are separated by a stderr-only header line so stdout
// stays a byte stream.
func runRawLast(n int) {
	files, err := activeSessionFiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "No sessions found.")
		os.Exit(1)
	}
	if n > len(files) {
		n = len(files)
	}
	for _, path := range files[:n] {
		rawLog, err := gopilog.ResolveRawLogForSession(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi session raw: skipping %s: %v\n", gopilog.FileToID(path), err)
			continue
		}
		if n > 1 {
			// Separator on STDERR only — stdout remains pure bytes.
			fmt.Fprintf(os.Stderr, "===== %s (raw log %s) =====\n", gopilog.FileToID(path), filepath.Base(rawLog))
		}
		warnSharedRawLog(path, rawLog)
		printRawFile(rawLog)
	}
}

// printRawRoundsIndex prints the round index table (round# → line span → bytes).
func printRawRoundsIndex(rawLog string) {
	rounds, err := gopilog.ParseHTTPRounds(rawLog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing raw log %s: %v\n", rawLog, err)
		os.Exit(1)
	}
	fmt.Print(gopilog.FormatRawRoundIndex(rounds, filepath.Base(rawLog)))
}

// printRawSysprompt prints the system prompt exactly as it was sent (the first
// request's system blocks, no header).
func printRawSysprompt(rawLog string) {
	p, err := gopilog.ParseFirstRequest(rawLog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	// textMode omits every decoration — the prompt text and nothing else.
	fmt.Print(gopilog.FormatRequestSystem(p, filepath.Base(rawLog), true))
}

// printRawUserprompt prints the first request's user message — the user's own
// input exactly as it went on the wire (no header, pipe-friendly).
func printRawUserprompt(rawLog string) {
	p, err := gopilog.ParseFirstRequest(rawLog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(gopilog.FormatRequestUserPrompt(p))
}

// printRawLines prints a verbatim raw-log line span (--lines A-B).
func printRawLines(rawLog string, opts *rawOptions) {
	last := opts.lineLast
	if last == -1 {
		// A- means to the end of the log: bound it with the file's line count.
		rounds, err := gopilog.ParseHTTPRounds(rawLog)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing raw log %s: %v\n", rawLog, err)
			os.Exit(1)
		}
		if n := len(rounds); n > 0 {
			last = rounds[n-1].LineEnd
		} else {
			last = opts.lineFirst
		}
	}
	printRawSpan(rawLog, opts.lineFirst, last)
}

// rawRoundPosition resolves the positional <round> selector against the parsed
// round list and returns the index to print; ok=false means the selector is out
// of range and names no round (a typo the user must see, in either direction:
// round 7 of 5, or -7 of 5).
//
// The index itself comes from gopilog.HTTPDetailIndex — the SAME resolver every
// round-naming surface uses — so `gopi session raw` can never disagree with the rest
// about what "last" / 0 / -2 mean. A zero/negative selector within range counts
// from the end; the resolver's own forgiving clamp is deliberately NOT relied
// on here, hence the explicit range check.
func rawRoundPosition(rounds []gopilog.HTTPRound, sel gopilog.HTTPRoundSelector) (int, bool) {
	if len(rounds) == 0 || sel.Round > len(rounds) || sel.Round < -len(rounds) {
		return 0, false
	}
	return gopilog.HTTPDetailIndex(rounds, sel.Round), true
}

// printRawRound prints one round (both sides, or its request/response side).
func printRawRound(rawLog string, opts *rawOptions) {
	printRawRoundAt(rawLog, opts.sessionID, opts.sel, opts.part)
}

// printRawRoundAt is the printer above, addressed by VALUES instead of a
// *rawOptions: `gopi session log <id> <round> request` prints the same bytes through
// this entry point, resolving the session's own round number to a raw ordinal
// itself (internal/gopilog's pairing) and then handing that ordinal here. One
// printer, one resolver (`rawRoundPosition` over HTTPDetailIndex), so the two
// verbs cannot disagree about what round N contains.
//
// sessionID only names the session in diagnostics (Rule 15: a refusal says
// which session and how many rounds it has); "" is allowed when the caller has
// no id to offer.
func printRawRoundAt(rawLog, sessionID string, sel gopilog.HTTPRoundSelector, part string) {
	rounds, err := gopilog.ParseHTTPRounds(rawLog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing raw log %s: %v\n", rawLog, err)
		os.Exit(1)
	}
	if len(rounds) == 0 {
		fmt.Fprintf(os.Stderr, "No request rounds found in raw log %s\n", rawLog)
		os.Exit(1)
	}
	idx, ok := rawRoundPosition(rounds, sel)
	if !ok {
		fmt.Fprintf(os.Stderr, "gopi session raw: round %d does not exist — the raw log has %d rounds (round numbering is 1-based and includes guard probes; see gopi session raw %s --%s)\n",
			sel.Round, len(rounds), sessionID, rawFlagRounds)
		os.Exit(1)
	}
	r := &rounds[idx]

	switch part {
	case rawPartRequest:
		body, ok := gopilog.RawRoundRequest(r)
		if !ok {
			fmt.Fprintf(os.Stderr, "gopi session raw: round %d logged no request body\n", r.Number)
			os.Exit(1)
		}
		fmt.Println(body)
	case rawPartResponse:
		respFirst, respLast, ok := gopilog.RawRoundResponseSpan(r)
		if !ok {
			fmt.Fprintf(os.Stderr, "gopi session raw: round %d logged no response markers\n", r.Number)
			os.Exit(1)
		}
		printRawSpan(rawLog, respFirst, respLast)
	default:
		printRawSpan(rawLog, r.LineStart, r.LineEnd)
	}
}

// printRawSpan prints raw-log lines [first, last] verbatim. A truncated tail
// is reported on STDERR — stdout stays pure bytes.
func printRawSpan(path string, first, last int) {
	lines, truncated, err := gopilog.RawLogLineSpan(path, first, last)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading raw log %s: %v\n", path, err)
		os.Exit(1)
	}
	for _, l := range lines {
		fmt.Println(l)
	}
	if truncated {
		fmt.Fprintf(os.Stderr, "[gopi session raw: output truncated at the %d MiB cap — full span %d..%d in %s]\n",
			rawCapMiB, first, last, filepath.Base(path))
	}
}

// rawCapMiB is the display form of gopilog's raw view cap (64 MiB) for the
// stderr truncation note.
const rawCapMiB = gopilog.RawViewCapBytes >> 20
