package tools

// session_search.go — the SESSION scope of keyword_search: full-text search over
// a session's OWN on-disk record (`sessions/<id>/session.jsonl`, the detail
// layer that holds every row at full length), rendered as STRUCTURED hits.
//
// WHAT IT IS FOR. After a compaction the request no longer carries the old
// rounds; the [IMPORTANT] notes beside it are one line per round, so they cannot
// answer "what exactly did that command print 900 rounds ago". The disk record
// can, and it is the ONLY place that can: the summariser trims what it SENDS,
// never what it STORES.
//
// WHY THE SCOPE LIVES ON keyword_search AND NOT IN A NEW TOP-LEVEL TOOL
// (PROJECT.md Rule 3: "Do NOT add new top-level tools"). The briefing proposed a
// new tool and left the fork open; the rule's letter decides it. The routing
// question the new tool was supposed to answer is answered by the argument
// instead: "I want to look INSIDE the record of THIS session" is a SCOPE, and
// keyword_search already carries the one thing that makes the search correct —
// the ripgrep wrapper (keywordSearchImpl / SearchScope.RootPath is a free path).
// Making keyword_search's RESULT STRUCTURE depend on its scope is what keeps the
// model's reading cheap: a session hit renders as
//
//	round · row type · timestamp · snippet · a command that opens it
//
// and never as the raw JSON line, which for this corpus is up to 66 KB of one
// line (measured on session e68fddf5: 1546 tool_result rows, max 66536 chars).
//
// WHY raw.log IS NOT SEARCHED BY DEFAULT. It is the same history re-sent on every
// turn: measured on that session, session.jsonl is 13,348,744 B while the raw log
// is 774,552,797 B — 58× the bytes for the same rounds — and one tool call id
// appears 2× in the JSONL against 18× in the raw log. So the raw log is behind an
// explicit flag and every hit it produces is LABELLED with its source, because a
// reader must be able to tell which file a hit came from (Rule 15: a silent
// source would make two different readings indistinguishable).
//
// WHAT THIS FILE DOES AND DOES NOT REUSE (Rule 14 / 35 — reuse, never re-build):
//   - the SEARCH is tools.KeywordSearch, i.e. the existing ripgrep wrapper. No
//     second search engine.
//   - the ROUND NUMBER is sessionevent.RoundNumberOf — the one definition of
//     "which round is this", shared with internal/gopilog and internal/agent.
//   - the SESSION ADDRESS is config.SessionsRoot + sessionlayout's own layout
//     vocabulary (RecordName / JSONLSuffix / Records), so all three layouts
//     (bare dir, flat file, task-run dir) resolve the way every other session
//     reader resolves them.
//   - it does NOT import internal/gopilog (whose ParseFile would decode the whole
//     session for us): gopilog imports internal/agent (formatter.go) and
//     internal/agent imports this package, so tools→gopilog is an import CYCLE.
//     That is a structural fact, not a preference, and it is why the per-row
//     attribution below reads two wire keys itself instead of asking gopilog.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/sessionlayout"
)

const (
	// SessionSearchScopeSession is the VALUE keyword_search's `scope` argument
	// takes to search a session's own record. The empty scope is the project
	// tree (the tool's original and still ordinary meaning).
	SessionSearchScopeSession = "session"

	// sessionSearchDefaultLimit is how many hits one call returns when the
	// caller says nothing. It is deliberately SMALL: a hit carries a snippet and
	// a locator, and a page of them is meant to be enough to choose one round to
	// open — the cap is reported, never silent, so a wider sweep is one flag away.
	sessionSearchDefaultLimit = 5

	// sessionSearchMaxLimit bounds what a caller may ask for. Above this the one
	// page stops being readable and becomes an export; the drop is reported.
	sessionSearchMaxLimit = 50

	// SessionSearchMaxLimit exposes the ceiling to a CLI face, which must state
	// the SAME number in its -h page and its flag validation (Rules 13/14): a
	// page promising 50 while the parser refuses 50 is the drift this exists to
	// prevent, and a second literal in cmd/ would be exactly that.
	SessionSearchMaxLimit = sessionSearchMaxLimit

	// The snippet is built from the MATCHING LINE's own bytes, so these two
	// numbers are what keep a 66 KB line from being pasted into the answer: the
	// window is `lead` runes before the match and `total` runes around it.
	sessionSearchSnippetTotal = 160
	sessionSearchSnippetLead  = 48

	// sessionSearchScanFactor is how many raw matches the search keeps per page
	// slot before attribution. It exists because the page cap applies AFTER
	// attribution (a query whose matches cluster in a few rows must still fill
	// the page), so the search itself gets a bounded oversample instead of
	// decoding every match of a common word across a 13 MB file.
	sessionSearchScanFactor = 20

	// sessionSearchLineCeiling bounds ONE line's bytes while the attribution pass
	// streams the file. It is a MEMORY guard, not a content policy (the same
	// reasoning internal/gopilog's jsonlLineSafetyCeiling states): the largest
	// session line measured on this host is 87,618,404 bytes, so this is ~3×
	// headroom, and a line beyond it means the file is not a session JSONL.
	// Exceeding it is reported with the file, the line number and the ceiling.
	sessionSearchLineCeiling = 256 << 20

	// sessionSearchSourceJSONL / sessionSearchSourceRawLog are the SOURCE labels
	// every hit carries. Two files can answer one query and they are not
	// interchangeable (see the header), so the label is part of the hit.
	sessionSearchSourceJSONL  = "session.jsonl"
	sessionSearchSourceRawLog = "raw.log"

	// The session JSONL's own row-type spellings, as written by
	// (*agent.SessionLogger). internal/gopilog's reader holds these same values
	// as its unexported eventType* constants; that package is unreachable from
	// here (the import cycle named in the header), so the values are declared
	// once HERE rather than typed inline at each comparison (Rule 1).
	sessionRowTypeRequest  = "request"
	sessionRowTypeUser     = "user"
	sessionRowTypeToolRslt = "tool_result"

	// sessionRowTimestampLayouts are the two timestamp spellings a session row
	// carries: the writer emits RFC3339Nano, and a row written by another binary
	// may carry plain RFC3339. Rendering truncates to the second — a hit's
	// identity is its round and its line, not its nanoseconds.
	sessionRowTimestampLayoutNano = time.RFC3339Nano
	sessionRowTimestampLayout     = time.RFC3339
	sessionHitTimeLayout          = "2006-01-02T15:04:05"
)

// SessionSearchOptions is one session-scope search request.
type SessionSearchOptions struct {
	Query string // required

	// SessionID is a session id, or an ABSOLUTE path to a session record. Empty
	// means "the caller tells us" — the tool arm fills it with the live session
	// id, and a bare CLI call with neither is refused by name (Rule 15) rather
	// than searching an arbitrary session.
	SessionID string

	// Limit is the page size; 0 takes sessionSearchDefaultLimit, and anything
	// above sessionSearchMaxLimit is lowered to it (reported, never silent).
	Limit int

	// IncludeRawLog puts the session's raw HTTP log in scope as well. Off by
	// default: it is the same history re-sent, 58× the bytes. When on, every hit
	// is labelled with its source, and a raw-log hit carries its line number but
	// NO round number — the raw log's rounds are its own `→ REQUEST` ordinals and
	// pairing them to JSONL rounds is a different job (`gopi session raw`).
	IncludeRawLog bool
}

// SessionSearchHit is ONE match, already attributed: the structure the caller
// reads instead of a raw JSON line.
type SessionSearchHit struct {
	Source    string // sessionSearchSourceJSONL | sessionSearchSourceRawLog
	SessionID string
	Path      string // the file the hit is in
	LineNo    int    // 1-based line in THAT file
	RowType   string // request / user / assistant / tool_result ("?" in the raw log)
	ToolUseID string // the tool_use id a tool_result row answers ("" otherwise)
	Round     int    // sessionevent round number; RoundIndexUnknown when unrecorded
	Timestamp string // as recorded, truncated to the second
	Snippet   string // a window around the match, never the whole line
	Locate    string // the command that opens this hit's round
}

// SessionSearchResult is the page: the hits, what the cap dropped, and the facts
// a reader needs to judge the answer (which file(s) were searched, how long it
// took, and whether the file was refused as binary).
type SessionSearchResult struct {
	Query     string
	SessionID string
	Files     []string // the files actually searched, in order
	Hits      []SessionSearchHit
	Dropped   int
	// Cap says whether the underlying READ was cut short at
	// searchOutputByteCeiling. A truncated session scan returns the matches that
	// came first, so a reader told "N hits" must also be told the scan stopped
	// short (Rule 15).
	Cap            SearchCap
	Elapsed        time.Duration
	RawLogSearched bool
}

// SessionSearch runs the query over a session's on-disk record and returns the
// attributed page. See the file header for what it reuses and why.
func SessionSearch(opts SessionSearchOptions) (SessionSearchResult, error) {
	started := time.Now()
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return SessionSearchResult{}, fmt.Errorf("keyword_search: query must not be empty")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = sessionSearchDefaultLimit
	}
	if limit > sessionSearchMaxLimit {
		limit = sessionSearchMaxLimit
	}

	id, jsonlPath, err := resolveSessionSearchTarget(opts.SessionID)
	if err != nil {
		return SessionSearchResult{}, err
	}

	res := SessionSearchResult{Query: query, SessionID: id}

	// The JSONL pass. The page cap applies AFTER attribution, so the search keeps
	// an oversample of limit*sessionSearchScanFactor raw matches: a query whose
	// matches cluster in a few rows must still fill the page, while a common word
	// cannot make this decode every match in a 13 MB file.
	hits, dropped, searchCap, err := searchSessionFile(jsonlPath, id, query, limit)
	if err != nil {
		return res, err
	}
	res.Files = append(res.Files, jsonlPath)
	res.Hits = append(res.Hits, hits...)
	res.Dropped += dropped
	res.Cap = mergeSearchCap(res.Cap, searchCap)

	if opts.IncludeRawLog {
		rawPath := sessionRawLogPath(jsonlPath)
		if rawPath == "" {
			return res, fmt.Errorf("keyword_search: session %s records no raw HTTP log (no rawLogPath in its session_start)", id)
		}
		rawHits, rawDropped, rawCap, rawErr := searchSessionFileRawLog(rawPath, id, query, limit)
		if rawErr != nil {
			return res, rawErr
		}
		res.RawLogSearched = true
		res.Files = append(res.Files, rawPath)
		res.Hits = append(res.Hits, rawHits...)
		res.Dropped += rawDropped
		res.Cap = mergeSearchCap(res.Cap, rawCap)
	}

	kept, droppedByPage := capSlice(res.Hits, limit)
	res.Hits = kept
	res.Dropped += droppedByPage
	res.Elapsed = time.Since(started)
	return res, nil
}

// resolveSessionSearchTarget turns whatever the caller named into (session id,
// JSONL path). An ABSOLUTE path is taken as given (it is the caller's evidence,
// and re-resolving it through the profile would refuse a path outside it). A
// session id is resolved through the profile's sessions root in the three
// SHAPES the tree actually uses — a BARE dir (`<id>/session.jsonl`), a FLAT file
// (`<id>.jsonl`), and any record inside a task-run dir (`<id>/<id>.jsonl`, or a
// per-run `<id>/<uuid>.jsonl`) — because a reader that can only find one of them
// reports a log it HAS as absent: the defect internal/tools/chat.go's presence
// probe documents at length.
//
// The task-run shape is covered by the Records() loop (layout 3), and that is
// MEASURED, not assumed: the matrix in TestSessionSearchResolvesEveryLayoutShape
// drives eight directory shapes — bare, flat, `<id>/<id>.jsonl` with and without
// the side files a real run dir carries, an older per-run `<uuid>.jsonl`, a run
// dir holding an attachments/ subtree, a bare dir that ALSO holds a run record,
// and a run dir holding only side files — and pins which of them a bare id
// reaches. A cheap stat for the `<id>/<id>.jsonl` shape is deliberately NOT
// added: Records() already answers it, and a second arm would be a second place
// for the id derivation to drift (Rule 14).
func resolveSessionSearchTarget(target string) (id, path string, err error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", "", fmt.Errorf("keyword_search: scope=%s needs a session id or an absolute session path (set `session`)", SessionSearchScopeSession)
	}
	if filepath.IsAbs(target) {
		p, statErr := os.Stat(target)
		if statErr != nil {
			return "", "", fmt.Errorf("keyword_search: session path %q: %w", target, statErr)
		}
		if statErr := validateSessionTargetMode(p); statErr != nil {
			return "", "", statErr
		}
		return filepath.Base(filepath.Dir(target)), target, nil
	}

	root, err := config.SessionsRoot()
	if err != nil {
		return "", "", fmt.Errorf("keyword_search: resolve sessions root: %w", err)
	}

	// Layout 1 and 2 are the cheap stat-able shapes: a BARE dir holding
	// session.jsonl, and a FLAT file named <id>.jsonl.
	bare := filepath.Join(root, target, sessionlayout.RecordName)
	if _, statErr := os.Stat(bare); statErr == nil {
		return target, bare, nil
	}
	flat := filepath.Join(root, target+sessionlayout.JSONLSuffix)
	if _, statErr := os.Stat(flat); statErr == nil {
		return target, flat, nil
	}
	// Layout 4 is the TASK-RUN shape, and it must be tried HERE — as a stat,
	// before the directory listing — because it is the layout a task row's
	// session actually uses: the directory is named after the id and the record
	// inside it is named after the id too (`<root>/<id>/<id>.jsonl`), so layout
	// 1's `session.jsonl` stat misses and layout 2 looks one level too high.
	//
	// MEASURED (2026-11-09, task 01a1bcd6-96d3-7aaf-bb10-a903fb940c4a): the file
	// sat in that exact directory and a bare id was refused with "no session
	// record for … " — the tool's most obvious target, unreachable from its most
	// natural argument. Layout 3's sessionlayout.Records does NOT cover it for a
	// run dir carrying side files, which is why the cheap stat has to come
	// first: one os.Stat, no listing, and it answers the shape the fleet is
	// full of.
	runRecord := filepath.Join(root, target, target+sessionlayout.JSONLSuffix)
	if _, statErr := os.Stat(runRecord); statErr == nil {
		return target, runRecord, nil
	}

	// Layout 3: the id names a TASK-RUN directory whose record is not called
	// session.jsonl. It costs a directory listing, and it is the layout a task
	// row's session lives in.
	recs, listErr := sessionlayout.Records(root)
	if listErr == nil {
		for _, r := range recs {
			if filepath.Base(r.Dir) == target {
				return target, r.Path, nil
			}
			if strings.TrimSuffix(filepath.Base(r.Path), sessionlayout.JSONLSuffix) == target {
				return target, r.Path, nil
			}
		}
	}

	// The refusal names EVERY layout that was tried, in the order it was tried
	// (Rule 15): the list is what a reader checks their directory against, and
	// a list that omits the layout they are looking at is a lie with a helpful
	// face. It used to say "looked for session.jsonl/, <id>.jsonl, and any
	// record naming it" — which is wrong on BOTH counts: layout 1 is
	// `<id>/session.jsonl`, not a directory called "session.jsonl", and the
	// task-run shape `<id>/<id>.jsonl` deserves its own row because it is the
	// shape a task row's session actually has.
	return "", "", fmt.Errorf("keyword_search: no session record for %q under %q (looked for %s, %s, and any record inside %s/ naming it — the last covers the task-run shape %s)",
		target, root,
		filepath.Join(target, sessionlayout.RecordName),
		filepath.Join(root, target+sessionlayout.JSONLSuffix),
		target, filepath.Join(target, target+sessionlayout.JSONLSuffix))
}

// validateSessionTargetMode refuses a target that is not a readable regular
// file, naming which of the two facts failed.
func validateSessionTargetMode(info os.FileInfo) error {
	if info.IsDir() {
		return fmt.Errorf("keyword_search: session path is a DIRECTORY, not its record — name the file (%s inside it)", sessionlayout.RecordName)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("keyword_search: session path is not a regular file (mode %s)", info.Mode())
	}
	return nil
}

// sessionRawLogPath returns the raw HTTP log a session records in its
// session_start row, or "" when it recorded none (an older binary, or capture
// off). It reads the FIRST rows only — the value is written once, at the start.
func sessionRawLogPath(jsonlPath string) string {
	f, err := os.Open(jsonlPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := newSessionSearchScanner(f)
	for sc.Scan() {
		var row struct {
			Type       string `json:"type"`
			RawLogPath string `json:"rawLogPath"`
		}
		if json.Unmarshal(sc.Bytes(), &row) != nil {
			continue
		}
		if row.Type == sessionevent.EventTypeSessionStart {
			return row.RawLogPath
		}
		// session_start is the first event; a couple of rows of tolerance keeps
		// this from failing on a log whose head a resume rewrote.
		if sc.LineNo() > 1 {
			if _, statErr := os.Stat(row.RawLogPath); row.RawLogPath != "" && statErr == nil {
				return row.RawLogPath
			}
			return ""
		}
	}
	return ""
}

// searchSessionFile searches ONE session JSONL and attributes every hit. The
// search itself is the existing ripgrep wrapper; the attribution is one forward
// pass over the file that decodes only three wire keys per row (`type`,
// `timestamp`, `iteration`).
func searchSessionFile(path, sessionID, query string, limit int) ([]SessionSearchHit, int, SearchCap, error) {
	textHits, dropped, searchCap, err := KeywordSearch(SearchScope{
		RootPath: path,
		// No include/exclude globs: the ROOT is this one file, and a glob that
		// says "*.jsonl" is a filter that can only ever be true or — worse —
		// false for a record named something else.
		MaxResult: limit * sessionSearchScanFactor,
	}, query)
	if err != nil {
		return nil, 0, SearchCap{}, err
	}
	if len(textHits) == 0 {
		return nil, dropped, searchCap, nil
	}

	attr, err := sessionRowAttributes(path, textHits)
	if err != nil {
		return nil, 0, SearchCap{}, err
	}

	hits := make([]SessionSearchHit, 0, len(textHits))
	for _, th := range textHits {
		a := attr[th.LineNo]
		hits = append(hits, sessionHitFromRow(path, sessionID, query, th, a))
	}
	// The wrapper's own oversample drop is carried out, never swallowed: the page
	// cap downstream counts what IT drops, and a reader who is told "5 shown" must
	// also be told that the scan itself stopped short (Rule 15). The READING cap
	// rides the same rule: a truncated scan's hits are the matches that came
	// first, and the caller has to be able to say so.
	return hits, dropped, searchCap, nil
}

// searchSessionFileRawLog searches the RAW HTTP log. Its hits are labelled with
// their source and carry a line number but no round: the raw log's rounds are
// its `→ REQUEST` ordinals, and pairing them is `gopi session raw`'s job, not a
// guess made here.
func searchSessionFileRawLog(path, sessionID, query string, limit int) ([]SessionSearchHit, int, SearchCap, error) {
	textHits, dropped, searchCap, err := KeywordSearch(SearchScope{RootPath: path, MaxResult: 0}, query)
	if err != nil {
		return nil, 0, SearchCap{}, err
	}
	hits := make([]SessionSearchHit, 0, len(textHits))
	for _, th := range textHits {
		hits = append(hits, SessionSearchHit{
			Source:    sessionSearchSourceRawLog,
			SessionID: sessionID,
			Path:      path,
			LineNo:    th.LineNo,
			RowType:   "?",
			Round:     sessionevent.RoundIndexUnknown,
			Snippet:   th.Snippet,
			Locate:    fmt.Sprintf("gopi session raw %s   (raw-log line %d)", sessionID, th.LineNo),
		})
	}
	return hits, dropped, searchCap, nil
}

// sessionRowAttr is what one matching LINE contributes to the page. Everything
// here is read out of the line itself; nothing is inferred from position alone
// except the ROUND, which is a running value (see sessionRowAttributes).
type sessionRowAttr struct {
	RowType   string
	ToolUseID string
	Timestamp string
	Round     int
	// Text is the row's HUMAN-READABLE body — the words the session actually
	// carried, extracted from the JSON envelope (see sessionRowText). It is what
	// the snippet is built from, because a snippet of the raw line is JSON
	// punctuation with the words buried inside it.
	Text  string
	Found bool
}

// sessionRowProbe is the minimal shape the attribution pass decodes. `Iteration`
// is a POINTER on purpose: the field is written `omitempty`, so a row that
// records iteration 0 and a row that records nothing are the same bytes unless
// presence is asked for — and 0 is a real iteration (the session's FIRST send).
type sessionRowProbe struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Iteration *int   `json:"iteration"`
	Message   struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// sessionRowAttributes makes ONE forward pass over a session JSONL and answers
// "what is at each matching line", including its ROUND.
//
// THE ROUND IS THE ORDINAL OF THE `request` ROW, and this is the one place where
// getting it wrong is silent and expensive — so the model is stated with the
// measurement that settled it.
//
// NOT `iteration + 1`. The row's `iteration` field is the LOOP iteration inside
// one run, and a RESUMED session restarts it from 0. Measured on session
// e68fddf5-32a9-4e6d-b06f-13a101295883 (5 session_start rows, i.e. resumed four
// times): the field's maximum value in the whole file is 35, while the file
// carries 1381 `request` rows — and `gopi session log <id> <round>` reports
// "ROUND N of 1381" for exactly the ordinal N. An `iteration + 1` locator
// therefore opened the WRONG round for every hit (round 5 for a row that is
// round ~1300) while looking entirely plausible. The regression guard is
// TestSessionRowAttributesSurvivesAResume.
//
// WHAT THE READER ACTUALLY DOES, for the same reason: internal/gopilog's
// `case "request"` increments reqCount and calls flushIter(reqCount), so a round
// IS the position of its request row. The arithmetic below goes through
// sessionevent.RoundNumberOf — the package that owns "what a round number means" —
// with the ordinal made 0-based first, so the value stays defined in ONE place
// rather than being counted here.
//
// THE DIRECTION IS "THE NEXT REQUEST ROW", and it is measured, not reasoned — the
// first cut of this file used the PREVIOUS one and was off by exactly one round
// for every single hit. The reader's `case "request"` FLUSHES the content
// accumulated before it into iteration reqCount, so a content row belongs to the
// round of the request row that comes AFTER it. Measured on session e68fddf5,
// `gopi session log <id> <round>` as the oracle: the rows at lines 7570, 7573 and
// 7576 all sit between request ordinals 1320 (line 7569) and 1321 (line 7574),
// and all three render in round 1321 — the next request, never the previous one.
// TestSessionRowAttributesResolvesAgainstTheReader pins both directions.
//
// A row the log never flushes (nothing after the last request row) belongs to no
// canonical round and says so (RoundIndexUnknown) instead of being charged to the
// last real one — the reader's own final `flushIter(0)` draws the same line.
func sessionRowAttributes(path string, matches []TextHit) (map[int]sessionRowAttr, error) {
	want := make(map[int]bool, len(matches))
	for _, m := range matches {
		want[m.LineNo] = true
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("keyword_search: open %q: %w", path, err)
	}
	defer f.Close()

	out := make(map[int]sessionRowAttr, len(want))
	sc := newSessionSearchScanner(f)
	reqOrdinal := 0

	// pending holds matched lines whose round is not decided yet: a content row is
	// resolved by the request row that FLUSHES it, which is the one AFTER it (see
	// the doc comment). The buffer is tiny — matched lines only — and a row the
	// file never flushes stays unresolved, exactly like the reader's final
	// flushIter(0).
	var pending []int
	resolvePending := func(round int) {
		for _, p := range pending {
			a := out[p]
			a.Round = round
			out[p] = a
		}
		pending = pending[:0]
	}

	for sc.Scan() {
		lineNo := sc.LineNo()
		if !want[lineNo] {
			// Still decoded: a `request` row we do not print decides the round of
			// every matched row before it. This is the one pass the whole page costs.
			if isRequestRow(sc.Bytes()) {
				reqOrdinal++
				resolvePending(sessionevent.RoundNumberOf(reqOrdinal - 1))
			}
			continue
		}
		var probe sessionRowProbe
		if json.Unmarshal(sc.Bytes(), &probe) != nil {
			out[lineNo] = sessionRowAttr{Found: false}
			continue
		}
		out[lineNo] = sessionRowAttr{
			RowType:   probe.Type,
			ToolUseID: toolResultUseID(probe),
			Timestamp: probe.Timestamp,
			Text:      sessionRowText(sc.Bytes()),
			Found:     true,
		}
		if probe.Type == sessionRowTypeRequest {
			// A request row belongs to the round it closes: it is the row that
			// flushes the content accumulated before it, itself included.
			reqOrdinal++
			round := sessionevent.RoundNumberOf(reqOrdinal - 1)
			a := out[lineNo]
			a.Round = round
			out[lineNo] = a
			resolvePending(round)
			continue
		}
		pending = append(pending, lineNo)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("keyword_search: read %q: %w", path, err)
	}
	// Whatever the log left after its last request row belongs to no canonical
	// round — the reader's own `flushIter(0)` says the same, and ordinal 0 is the
	// value that refuses to be mistaken for a round (RoundIndexUnknown).
	resolvePending(sessionevent.RoundIndexUnknown)
	return out, nil
}

// isRequestRow reports whether a row we are NOT printing is a `request` row — the
// only fact the round walk needs from it. It decodes ONE key, because this runs on
// every row of a 13 MB file: the round is the row's ORDINAL, not a field inside
// it (see the attribution doc comment for the measurement that settled that).
func isRequestRow(line []byte) bool {
	var probe struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &probe) != nil {
		return false
	}
	return probe.Type == sessionRowTypeRequest
}

// toolResultUseID extracts the tool_use id a tool_result row answers. "" when the
// row carries none (a user/assistant/request row, or a result whose block shape
// this decoder does not recognise) — absence is a value, never an error.
func toolResultUseID(probe sessionRowProbe) string {
	if probe.Type != sessionRowTypeToolRslt || len(probe.Message.Content) == 0 {
		return ""
	}
	var blocks []struct {
		ToolUseID string `json:"tool_use_id"`
	}
	if json.Unmarshal(probe.Message.Content, &blocks) != nil {
		return ""
	}
	for _, b := range blocks {
		if b.ToolUseID != "" {
			return b.ToolUseID
		}
	}
	return ""
}

// sessionHitFromRow assembles one page entry: the STRUCTURED reading of the row
// (round, type, timestamp) plus a bounded snippet and the command that opens it.
func sessionHitFromRow(path, sessionID, query string, th TextHit, a sessionRowAttr) SessionSearchHit {
	rowType := a.RowType
	if !a.Found || rowType == "" {
		rowType = "?"
	}
	return SessionSearchHit{
		Source:    sessionSearchSourceJSONL,
		SessionID: sessionID,
		Path:      path,
		LineNo:    th.LineNo,
		RowType:   rowType,
		ToolUseID: a.ToolUseID,
		Round:     a.Round,
		Timestamp: sessionHitTimestamp(a.Timestamp),
		Snippet:   sessionHitSnippet(a.Text, query),
		Locate:    sessionHitLocate(sessionID, a.Round, th.LineNo),
	}
}

// sessionHitTimestamp renders a row's timestamp at second resolution. An absent
// or unparseable stamp is returned as recorded (or "—"), never invented.
func sessionHitTimestamp(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "—"
	}
	for _, layout := range []string{sessionRowTimestampLayoutNano, sessionRowTimestampLayout} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.Format(sessionHitTimeLayout)
		}
	}
	return raw
}

// sessionHitSnippet bounds the snippet AROUND THE MATCH, inside the row's
// human-readable text.
//
// WHY NOT the ripgrep snippet: the wrapper's window (buildSnippet, 100 bytes) is
// sized for a SOURCE line, and a session row is JSON — measured on this host's
// corpus, the wrapper's window for a tool_result row reads
// `…sage":{"content":[{"content":"needle-r1","tool_use_id":"call_1","type":"tool_r…`
// which is JSON punctuation with the words buried in it. The caller asked to see
// the SESSION, not its encoding, so the words are extracted first
// (sessionRowText) and the window is taken around the needle inside them.
//
// The needle is located CASE-INSENSITIVELY and LITERALLY. A regex query may not
// be found literally, in which case the text is shown from its start rather than
// pretending to be centred — the hit line and the locator already say exactly
// where the row is.
func sessionHitSnippet(text, query string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "—"
	}
	runes := []rune(text)
	if len(runes) <= sessionSearchSnippetTotal {
		return text
	}

	start := 0
	if q := strings.TrimSpace(query); q != "" {
		if idx := strings.Index(strings.ToLower(text), strings.ToLower(q)); idx >= 0 {
			// Byte index → rune index, so the lead window is measured in the same
			// unit the ceiling is.
			runeIdx := len([]rune(text[:idx]))
			start = runeIdx - sessionSearchSnippetLead
			if start < 0 {
				start = 0
			}
		}
	}
	end := start + sessionSearchSnippetTotal
	if end > len(runes) {
		end = len(runes)
		start = end - sessionSearchSnippetTotal
		if start < 0 {
			start = 0
		}
	}
	s := string(runes[start:end])
	if start > 0 {
		s = "…" + s
	}
	if end < len(runes) {
		s += "…"
	}
	return s
}

// sessionRowText extracts the row's HUMAN-READABLE body from its JSON envelope:
// every string carried under a `content` or `text` key, at any depth, joined in
// document order. Everything else (`type`, `role`, `tool_use_id`, `timestamp`,
// the token counters) is bookkeeping the hit line already states in its own
// fields, so repeating it inside the snippet wastes the one window a reader gets.
//
// The walk is GENERIC on purpose, and deliberately not a per-row-type decoder:
// the session JSONL is written by (*agent.SessionLogger) whose block shapes grew
// over time (a string content, an array of blocks, a result whose content is
// itself a string or a nested array). A generic collect reads all of them, and a
// shape this decoder has never seen degrades to "less text", never to an error —
// the hit's identity (round, type, line, locator) does not depend on it.
func sessionRowText(raw []byte) string {
	var v interface{}
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	var parts []string
	collectRowText(v, "", &parts)
	return strings.Join(parts, " · ")
}

// sessionRowTextKeys are the two wire keys whose string values ARE the session's
// words. Named here so the vocabulary is stated once (Rule 1) rather than typed
// inline in the walk below.
var sessionRowTextKeys = map[string]bool{
	"content": true,
	"text":    true,
}

func collectRowText(v interface{}, key string, out *[]string) {
	switch t := v.(type) {
	case string:
		if sessionRowTextKeys[key] && strings.TrimSpace(t) != "" {
			*out = append(*out, t)
		}
	case []interface{}:
		for _, e := range t {
			// An array does not rename its parent's key: a block inside
			// message.content is still "content" for this purpose, which is what
			// makes a nested tool_result body collect the same way a text block
			// does.
			collectRowText(e, key, out)
		}
	case map[string]interface{}:
		for k, e := range t {
			collectRowText(e, k, out)
		}
	}
}

// sessionHitLocate is the ADDRESS a reader follows: the round verb when the round
// is known, and the line itself when it is not (a note row, a row before the
// first request, a raw-log hit). A locator that names a round nobody can open
// would be worse than the line number.
func sessionHitLocate(sessionID string, round, lineNo int) string {
	if sessionevent.RoundIndexRecorded(round) {
		return fmt.Sprintf("gopi session log %s %d", sessionID, round)
	}
	return fmt.Sprintf("sed -n '%dp' %s/<id>/%s   (no round: this row is outside any round)",
		lineNo, "~/.gopi/sessions", sessionlayout.RecordName)
}

// newSessionSearchScanner is the ONE line reader this file uses: bufio's scanner
// with a named ceiling, so a corrupt or hostile file cannot drive the process out
// of memory, and a line beyond the ceiling is an ERROR naming the file and the
// line instead of bufio's anonymous "token too long" (Rule 15).
func newSessionSearchScanner(f *os.File) *sessionSearchScanner {
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), sessionSearchLineCeiling)
	return &sessionSearchScanner{sc: s}
}

// sessionSearchScanner numbers the lines it hands out (1-based), which is what
// the whole attribution pass is keyed on.
type sessionSearchScanner struct {
	sc   *bufio.Scanner
	line int
}

func (s *sessionSearchScanner) Scan() bool {
	if !s.sc.Scan() {
		return false
	}
	s.line++
	return true
}

func (s *sessionSearchScanner) Bytes() []byte { return s.sc.Bytes() }
func (s *sessionSearchScanner) LineNo() int   { return s.line }

func (s *sessionSearchScanner) Err() error {
	if err := s.sc.Err(); err != nil {
		return fmt.Errorf("%w (the line reader's ceiling is %d bytes)", err, sessionSearchLineCeiling)
	}
	return nil
}

// FormatSessionSearchHits renders the page the model reads. The shape is fixed:
// a header that states WHAT was searched and how long it took, one block per hit
// (round · type · timestamp · line, then the snippet, then the locator), and a
// footer that states what the cap dropped. No raw JSON line is ever printed — the
// whole point is that a 66 KB row becomes five lines.
func FormatSessionSearchHits(res SessionSearchResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "session %s · query %q · searched: %s · %s · %d hit(s)\n",
		res.SessionID, res.Query,
		strings.Join(res.Files, ", "),
		res.Elapsed.Round(time.Millisecond),
		len(res.Hits))

	if res.RawLogSearched {
		b.WriteString("  sources: session.jsonl (the stored record) AND raw.log (the same history re-sent — enabled explicitly)\n")
	}

	if len(res.Hits) == 0 {
		b.WriteString("no matches.\n")
		return b.String()
	}

	for i, h := range res.Hits {
		roundCell := fmt.Sprintf("round %d", h.Round)
		if !sessionevent.RoundIndexRecorded(h.Round) {
			roundCell = "round —"
		}
		typeCell := h.RowType
		if h.ToolUseID != "" {
			typeCell = fmt.Sprintf("%s(%s)", h.RowType, h.ToolUseID)
		}
		fmt.Fprintf(&b, "\n[%d] %s · %s · %s · %s: line %d\n    %s\n    → %s\n",
			i+1, roundCell, typeCell, h.Timestamp, h.Source, h.LineNo, h.Snippet, h.Locate)
	}

	if res.Dropped > 0 {
		fmt.Fprintf(&b, "\n… plus %d more match(es) not shown (page cap). Narrow the query, or raise limit (max %d).\n",
			res.Dropped, sessionSearchMaxLimit)
	}
	if notice := FormatSearchCap(res.Cap); notice != "" {
		fmt.Fprintf(&b, "\n%s\n", notice)
	}
	return b.String()
}

// SortedSessionHitLines is a small helper for tests and for a caller that wants
// the page ordered by position within one file. It is exported so a test can
// assert ordering without re-implementing it.
func SortedSessionHitLines(hits []SessionSearchHit) []int {
	out := make([]int, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.LineNo)
	}
	sort.Ints(out)
	return out
}
