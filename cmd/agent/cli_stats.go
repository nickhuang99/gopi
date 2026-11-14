package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/pricing"
	"github.com/nickhuang99/gopi/internal/tools"
)

// statsUsage is the help text of the `gopi session stats` subcommand: the numeric view
// of what a session did (FINAL-USAGE-PROPOSAL — "数字上发生了什么"). It is the
// successor of the numeric half of the old `gopi session log --http` surface.
const statsUsage = `gopi session stats — GoPi session statistics (numbers, not content)

Usage:
  gopi session stats <session-id>            One session's aggregate statistics:
                                     human turns first, the request count
                                     beside it (the session's turn count is
                                     the ledger's: gopi session show <id>)
  gopi session stats <session-id> --rounds   Per-round table (trim cliffs marked *)
                                     — one row per HTTP request/response, each
                                     naming the TURN it belongs to
  gopi session stats <session-id> --tools    Tool-call ranking (which tool how often)
  gopi session stats <session-id> --bytes    Per-round body traffic (req/resp bytes)
  gopi session stats <session-id> --cost     Real money (元): total + hit/miss/output split
  gopi session stats <session-id> --bill     Per-round + per-tool cost (cost curve + ranking)
  gopi session stats <session-id> --json     Full stats as JSON (includes the rounds)
  gopi session stats --branch BRANCH         The session the row owning BRANCH is
                                     running: the SAME input as <session-id>,
                                     named by what
                                     ` + "`git branch --show-current`" + ` prints
                                     in a task worktree (--worktree PATH takes
                                     pwd instead); combines with any view flag,
                                     refused beside --last/--all
  gopi session stats --last [N]              Most recent / last N sessions, one block each
  gopi session stats --all                   Aggregate across all sessions

Sources (stated in every output):
  raw      the session's paired raw HTTP log — the PRIMARY source. One file
           carries the request bodies (model, messages) and the SSE usage
           lines (input / cache / output tokens), so the aggregate needs
           nothing else.
  jsonl    the session's JSONL request events, used when no raw log can be
           paired (raw HTTP logging was off). Raw-only dimensions are absent.
  aggregate the sum over the sessions selected by --all. --all sums the JSONL
           request events (the raw corpus is tens of GB; the JSONL carries the
           same usage numbers, guard-probe rounds included).

Reading the numbers:
  turns          how many times a PERSON asked (one operator row to the next) —
                 THE HEADLINE. Every request/response and tool call the harness
                 ran to answer one ask is that turn's detail, printed on the
                 line under it (a turn routinely takes many requests)
  requests       every request marker of the source (guard probes included) —
                 one HTTP request/response each, the same quantity this table
                 calls a round in the per-round views below
  TURN (--rounds) the turn a round answered — one operator input to the next.
                 A turn routinely spans several rounds, so the column repeats
                 while the round numbers advance; it is blank for a raw-only
                 source, which records no operator rows
  prompt tokens  the sum of the per-round prompt usage (input + cache-read +
                 cache-write, exactly the JSONL prompt_tokens semantics)
  Δreq           the round's prompt-token change vs the previous counted round
  *              a trim cliff: the prompt dropped > 4,000 tokens AND to under
                 60% of the previous round (context_trim / reset signature)
  p              a guard/fail-fast ladder probe round — probes never enter the
                 Δreq chain (their small body would read as a cliff)
  wall time      from the session JSONL's session_end event (the raw log has
                 no timestamps)

--bytes (body traffic, raw-source only):
  req bytes      the round's request-body wire bytes (the exact JSON body as
                 sent — request size, not tokens)
  msgs           the number of user/tool messages that body carried (the
                 context-growth signal behind the token numbers)
  resp bytes     the round's response-side wire bytes (chunks / body incl.
                 marker overhead)
  text/think     the response's reassembled text/thinking characters — a
                 volume proxy, NOT token counts
  kind           stream / response / http-error / none (how the round ended)
  TOT            the table's last row sums every round's traffic (probe and
                 no-usage rounds included — body traffic is real regardless
                 of whether the provider reported usage)
  A jsonl-sourced session has no wire bodies → --bytes states that and shows
  no rows; --bytes with --all is rejected (the aggregate has no round rows).

Round numbering is NEVER cross-indexed: a raw-source table counts raw round
ordinals (1-based, probes included), a jsonl-source table counts JSONL
iterations (0-based). The table header states which one is in use.

Flags can be combined:
  gopi session stats <id> --rounds --json     Per-round numbers as JSON
  gopi session stats <id> --rounds --bytes    Both per-round tables
  gopi session stats <id> --cost --rounds     Money box + the round table
  gopi session stats --last 5 --tools         Tool ranking for each of the last 5 sessions
  gopi session stats --all --tools            Tool ranking over the aggregate
  gopi session stats <id> --json | jq .prompt_tokens

--cost / --bill (real money, 元):
  prices the session's usage at its session_start moment (date/time-aware):
  GLM-5.3-flash 五折 (0.4/0.115/1.4 per M) through 2026-09-09, 原价
  (0.8/0.23/2.8) after — and DeepSeek peak hours (9-12/14-18 Beijing) double
  the flat 1.5/0.05/4.5. Provider is inferred from the model string
  (deepseek-* / glm-*); an unknown model prints why no money is shown. Every
  number cites its price source (devdoc/glm-5.3-pricing.md etc.). --cost and
  --bill need one session (not --all — an aggregate mixes models and dates).
`

// statsFlagMember reports whether a flag name is valid for the stats
// subcommand.
func statsFlagMember(flag string) bool {
	// Read from the verb's ONE table (cli_session_flag_arity.go): admission and
	// arity are the same declaration, so a flag cannot be admitted here while
	// the scanner still guesses it claims a value.
	_, admitted := statsFlagArity(flag)
	return admitted
}

// statsOptions is the parsed result of a `gopi session stats` invocation.
type statsOptions struct {
	sessionID string
	// sel is the DELIVERABLE SELECTOR (--branch / --worktree): the SAME input
	// as sessionID, named by what the operator standing in a task worktree
	// holds. At most one spelling is set; it is resolved to a session id before
	// dispatch (sessionIDForSelector) and never carried further.
	sel    tools.DeliverableSelector
	rounds bool
	tools  bool
	bytes  bool
	json   bool
	cost   bool
	bill   bool
	all    bool
	// lastN > 0 means --last [N] was given (1 when no explicit N).
	lastN int
}

// parseStatsArgs parses the args of the stats subcommand without touching the
// process (no os.Exit), so the parsing is unit-testable.
func parseStatsArgs(args []string) (*statsOptions, error) {
	fs := newMemoryFlagSet()
	// The flags' ARITY is declared BEFORE the scan: a valueless flag (--json,
	// --rounds, --tools, --bytes, --all, --cost, --bill) must never claim the
	// next token as its value, or it eats the positional <session-id> standing
	// beside it and the verb reports an input the operator DID give as missing.
	// This also refuses an unknown flag by NAME, on the set actually typed.
	if err := sessionFlagDeclare(fs, args, statsFlagArity); err != nil {
		return nil, err
	}
	positionals, err := fs.parse(args)
	if err != nil {
		return nil, err
	}
	opts := &statsOptions{
		sessionID: "",
		rounds:    fs.has("rounds"),
		tools:     fs.has("tools"),
		bytes:     fs.has("bytes"),
		json:      fs.has("json"),
		cost:      fs.has("cost"),
		bill:      fs.has("bill"),
		all:       fs.has("all"),
	}

	if fs.has("last") {
		v := strings.TrimSpace(fs.get("last"))
		if v == "" {
			opts.lastN = 1
		} else {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("--last must be a positive integer, got %q", fs.get("last"))
			}
			opts.lastN = n
		}
	}

	if len(positionals) > 1 {
		return nil, fmt.Errorf("unexpected extra argument %q", positionals[1])
	}
	if len(positionals) == 1 {
		opts.sessionID = positionals[0]
	}

	// The DELIVERABLE SELECTORS (--branch / --worktree) name the SAME input as
	// the positional <session-id>, so both together is a usage error rather
	// than a silent preference — and so is a selector beside --last/--all (a
	// different input). The refusals are named (Rule 15) and their text comes
	// from the ONE definition in cli_session_selector.go.
	sel, err := sessionSelectorFromArgs(args)
	if err != nil {
		return nil, err
	}
	opts.sel = sel
	var otherInputs []string
	if opts.lastN > 0 {
		otherInputs = append(otherInputs, "--last")
	}
	if opts.all {
		otherInputs = append(otherInputs, "--all")
	}
	if err := sessionSelectorInputCheck(sel, opts.sessionID, otherInputs); err != nil {
		return nil, err
	}

	// Exactly one selector: a positional session id (or the --branch/--worktree
	// selector that names the same input), --last [N] or --all.
	selectors := 0
	if opts.sessionID != "" || sel.Branch != "" || sel.Worktree != "" {
		selectors++
	}
	if opts.lastN > 0 {
		selectors++
	}
	if opts.all {
		selectors++
	}
	if selectors != 1 {
		return nil, fmt.Errorf("pick one input: <session-id>, --%s <branch>, --%s <path>, --last [N] or --all",
			sessionBranchFlag, sessionWorktreeFlag)
	}
	return opts, nil
}

// isStatsSubcommand reports whether argv begins with the stats verb.
func isStatsSubcommand(args []string) bool {
	if len(args) == 0 || args[0] != CliVerbStats {
		return false
	}
	for _, a := range args[1:] {
		if a == "-h" || a == "--help" {
			fmt.Print(statsUsage)
			os.Exit(0)
		}
	}
	runStatsSubcommand(args[1:])
	return true
}

// runStatsSubcommand parses and dispatches `gopi session stats`, exiting non-zero on
// any argument/session error.
func runStatsSubcommand(args []string) {
	opts, err := parseStatsArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session stats: %v\n%s", err, statsUsage)
		os.Exit(1)
	}
	// settings.json "pricing" override (optional): an operator's effective
	// rates replace the built-in table before any session is priced. A
	// malformed section is loud (mis-priced money is worse than no money).
	if opts.cost || opts.bill {
		if perr := pricing.LoadSettingsOverride(config.ResolveSettingsPath()); perr != nil {
			fmt.Fprintf(os.Stderr, "gopi session stats: %v\n", perr)
			os.Exit(1)
		}
	}

	// The DELIVERABLE SELECTOR (--branch / --worktree) resolves to the session
	// id the row behind it is running, and is then a SUBSTITUTION into the
	// ordinary dispatch — the session is still found by this verb's own
	// resolver, so there is one answer to "where is this session's log?" (see
	// cli_session_selector.go).
	if opts.sel.Branch != "" || opts.sel.Worktree != "" {
		id, err := sessionIDForSelector(CliVerbStats, opts.sel)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		opts.sessionID = id
	}

	var blocks []string
	switch {
	case opts.all:
		blocks, err = runStatsAll(opts)
	case opts.lastN > 0:
		blocks, err = runStatsLast(opts.lastN, opts)
	default:
		var block string
		block, err = runStatsSession(opts.sessionID, opts)
		blocks = []string{block}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session stats: %v\n", err)
		os.Exit(1)
	}

	for i, b := range blocks {
		if i > 0 && !opts.json {
			fmt.Println()
		}
		fmt.Print(b)
	}
}

// runStatsSession resolves one session id and renders its requested views.
func runStatsSession(sessionID string, opts *statsOptions) (string, error) {
	dir, err := gopilog.ResolveSessionDir()
	if err != nil {
		return "", err
	}
	path, err := gopilog.FindSession(dir, sessionID)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}
	st, err := statsForSessionPath(path, false)
	if err != nil {
		return "", err
	}
	return renderStats(st, opts)
}

// runStatsLast renders the requested views for each of the last N sessions.
func runStatsLast(n int, opts *statsOptions) ([]string, error) {
	files, err := activeSessionFiles()
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return []string{"No sessions found.\n"}, nil
	}
	if n > len(files) {
		n = len(files)
	}
	var out []string
	for _, path := range files[:n] {
		st, err := statsForSessionPath(path, false)
		if err != nil {
			// One unreadable session must not kill the listing — it is
			// reported and skipped (same policy as `gopi session log --last N`).
			fmt.Fprintf(os.Stderr, "gopi session stats: skipping %s: %v\n", filepath.Base(path), err)
			continue
		}
		block, err := renderStats(st, opts)
		if err != nil {
			return nil, err
		}
		out = append(out, block)
	}
	return out, nil
}

// runStatsAll aggregates every active session into one block.
func runStatsAll(opts *statsOptions) ([]string, error) {
	files, err := activeSessionFiles()
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return []string{"No sessions found.\n"}, nil
	}
	var list []*gopilog.SessionStats
	var skipped int
	for _, path := range files {
		st, err := statsForSessionPath(path, true)
		if err != nil {
			skipped++
			continue
		}
		list = append(list, st)
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("no session could be read (%d skipped)", skipped)
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "gopi session stats: --all skipped %d unreadable session(s)\n", skipped)
	}
	st := gopilog.AggregateStats(list)
	st.SourceDetail = ""
	block, err := renderStats(st, opts)
	if err != nil {
		return nil, err
	}
	return []string{block}, nil
}

// activeSessionFiles lists the session files that carry activity, newest
// first — the same degenerate-session filter `gopi session log --last` applies (a
// bare session_start did nothing and would only dilute the aggregate).
func activeSessionFiles() ([]string, error) {
	// One listing, one head scan per file: the activity flag rides the same
	// ref that carries the ordering (gopilog.ListSessionRefs).
	refs, err := gopilog.ListSessionRefs()
	if err != nil {
		return nil, err
	}
	active := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.Active {
			active = append(active, r.Path)
		}
	}
	return active, nil
}

// statsForSessionPath builds one session's statistics: the paired raw HTTP log
// is the primary source, the session JSONL request events the fallback. With
// preferJSONL the raw log is skipped — the --all aggregate does that, because
// the raw corpus is tens of GB while the JSONL request events carry the same
// usage numbers. The returned block always names its source.
func statsForSessionPath(sessionPath string, preferJSONL bool) (*gopilog.SessionStats, error) {
	st, source, err := buildStats(sessionPath, preferJSONL)
	if err != nil {
		return nil, err
	}
	// Display session id: the session_start event's sessionId wins (a bare
	// session file is literally session.jsonl — its basename stem "session"
	// must never be shown); gopilog.FileToID's layout-aware fallback covers
	// the unreadable-head case. Only overwrite when the builder (raw source)
	// left it empty — BuildStatsFromUsage already set it from the event.
	if st.SessionID == "" {
		st.SessionID = gopilog.FileToID(sessionPath)
	}
	if source != "" {
		st.SourceDetail = filepath.Base(source)
	} else {
		// jsonl source: the session file itself is where the numbers came from.
		st.SourceDetail = filepath.Base(sessionPath)
	}
	// Wall time only exists in the session JSONL (the raw log has no
	// timestamps) — the one field both sources read from there.
	if !st.HasWall {
		if sec, ok := gopilog.SessionWallSeconds(sessionPath); ok {
			st.WallSeconds = sec
			st.HasWall = true
		}
	}
	// Started (the cost-pricing moment) comes from the session JSONL's
	// session_start event, read the same cheap way.
	if st.Started.IsZero() {
		if ts, ok := gopilog.SessionStarted(sessionPath); ok {
			st.Started = ts
		}
	}
	return st, nil
}

// buildStats does the raw-primary / jsonl-fallback selection and returns the
// stats block plus the file the numbers came from ("" for none).
func buildStats(sessionPath string, preferJSONL bool) (*gopilog.SessionStats, string, error) {
	// THE JSONL IS THE CANON (Rule 14 — one grouping definition, internal/gopilog).
	// Rounds mean "one `request` row = one HTTP request/response" on every
	// session surface, so the per-round table here is built from the session's own
	// request rows (BuildStatsFromJSONL) and AGREES with `gopi session log --rounds`
	// and `gopi session replay --rounds` on the same session.
	//
	// WHY THE RAW LOG WAS DEMOTED (measured, 2026-10-11). The raw HTTP log is a
	// DIFFERENT corpus: it numbers every `→ REQUEST` marker, including sends the
	// session JSONL never recorded (non-streaming meta calls, retries). Reading it
	// as the round source is what made `gopi session stats --rounds` print a number
	// no other surface agreed with (348 raw markers against the same session's 321
	// request rows).
	//
	// The raw log keeps what ONLY it holds: the wire bodies. Its traffic
	// dimensions ride onto the canonical rows through the token pairing
	// (AttachRawTraffic), and preferJSONL skips even that (the --all sweep must
	// not read tens of GB of wire bodies).
	if sess, perr := gopilog.ParseFile(sessionPath); perr == nil && gopilog.SessionRounds(sess) > 0 {
		st := gopilog.BuildStatsFromJSONL(sess)
		if !preferJSONL {
			if rawLog, rerr := gopilog.ResolveRawLogForSession(sessionPath); rerr == nil {
				if rounds, herr := gopilog.ParseHTTPRounds(rawLog); herr == nil {
					gopilog.AttachRawTraffic(sess, rounds, st)
					st.WireLogDetail = filepath.Base(rawLog)
				}
			}
		}
		st.SourceDetail = filepath.Base(sessionPath)
		return st, "", nil
	}
	// A session whose JSONL holds no round of its own (unreadable, or a log that
	// recorded no `request` row): fall back to the raw log, then to the usage
	// scan. Which one fed the numbers is printed (statsSourceLabel).
	if !preferJSONL {
		if rawLog, rerr := gopilog.ResolveRawLogForSession(sessionPath); rerr == nil {
			if rounds, herr := gopilog.ParseHTTPRounds(rawLog); herr == nil {
				return gopilog.BuildStatsFromRounds(rounds), rawLog, nil
			}
		}
	}
	u, err := gopilog.ScanSessionUsage(sessionPath)
	if err != nil {
		return nil, "", fmt.Errorf("session jsonl unreadable and no usable raw log: %w", err)
	}
	return gopilog.BuildStatsFromUsage(u), "", nil
}

// renderStats renders the requested view of one stats block: the aggregate box
// by default, the per-round table with --rounds, the tool ranking with --tools,
// the body-traffic table with --bytes and the machine-readable form with --json
// (which carries the rounds too). --bytes has no meaning over an aggregate
// (--all drops the per-round rows the traffic view is built from), so it is
// rejected here rather than silently printing an empty table.
func renderStats(st *gopilog.SessionStats, opts *statsOptions) (string, error) {
	if opts.json {
		// --cost with --json fills st.Cost so the JSON carries the money
		// breakdown (no cost → the field stays absent rather than an error:
		// JSON is machine-readable and an unknown model is a fact, not a
		// failure).
		if opts.cost || opts.bill {
			st.ComputeCost(st.Started)
		}
		return gopilog.FormatStatsJSON(st)
	}
	if opts.bytes && st.Source == gopilog.StatsSourceAggregate {
		return "", fmt.Errorf("--bytes needs per-round rows; use it on one session, not --all")
	}
	if (opts.cost || opts.bill) && st.Source == gopilog.StatsSourceAggregate {
		return "", fmt.Errorf("--cost/--bill need one session's model + start time; use them on one session, not --all")
	}
	var sb strings.Builder
	sb.WriteString(gopilog.FormatStatsOverview(st))
	if opts.cost || opts.bill {
		costBlock, err := gopilog.FormatStatsCost(st)
		if err != nil {
			return "", err
		}
		sb.WriteString(costBlock)
	}
	if opts.rounds {
		sb.WriteString(gopilog.FormatStatsRounds(st))
	}
	if opts.tools {
		sb.WriteString(gopilog.FormatStatsTools(st))
	}
	if opts.bytes {
		sb.WriteString(gopilog.FormatStatsTraffic(st))
	}
	if opts.bill {
		billBlock, err := gopilog.FormatStatsBill(st)
		if err != nil {
			return "", err
		}
		sb.WriteString(billBlock)
		toolBlock, err := gopilog.FormatStatsToolCosts(st)
		if err != nil {
			return "", err
		}
		sb.WriteString(toolBlock)
	}
	return sb.String(), nil
}
