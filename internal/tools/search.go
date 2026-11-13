// Search tools: code_search (ctags symbol lookup) and keyword_search (ripgrep
// full-text search). Both return structured results (CodeSymbolHit / TextHit)
// so the routing decision between "definition lookup" and "full-text search"
// lives in the caller (the planning layer), not in a mode string.
package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// searchOutputLineCeiling bounds ONE line of a search backend's own output while
// the wrapper parses it (Rule 1: a named limit, not a literal at the append
// site). It is far above any source line because the wrapper is also used on
// SESSION JSONL, whose rows are whole messages: measured on this host, the
// largest session row is 3,987,146 bytes, and the old 1 MiB cap made a perfectly
// ordinary query (the word "tool_result") fail the whole call with
// `bufio.Scanner: token too long` — the repro is
// TestSessionSearchSurvivesALongRow in session_search_test.go. The snippet that
// finally reaches the caller is still 100 bytes (buildSnippet); this is only how
// much the PARSER must hold to see the match column.
const searchOutputLineCeiling = 128 << 20

// searchOutputByteCeiling is the STRUCTURAL bound on how much a search backend
// may write to this wrapper before the wrapper stops reading and kills it
// (Rule 1: a named limit with its own measurement, never a literal at the read
// site).
//
// Why it has to exist at all: until this constant, the wrapper read the whole of
// rg's stdout into memory with cmd.CombinedOutput() and parsed it afterwards, so
// total memory was O(sum of the byte length of every matching line) with no
// bound whatsoever — the only ceiling was per-LINE (searchOutputLineCeiling,
// 128 MiB). MEASURED on this host (2026-11-08, the CLI face
// `gopi tool keyword_search` under /usr/bin/time -f %M):
//
//	query "the"         over a 48 MB session.jsonl → 38,874,644 bytes of rg
//	                    output → peak RSS 94,392 KB
//	query "tool_result" over the same file         → 13,989,452 bytes of output
//	                    → peak RSS KILLED after 10 s at 11,364,096 KB
//
// — the last line is the defect this constant closes: the bound was the size of
// the DATA, not a design choice, and a slightly larger file (this host also holds
// a 3.37 GB raw HTTP log) reaches the kernel's OOM path instead of a refusal.
//
// 64 MiB is chosen so that every query that RUNS CORRECTLY TODAY is untouched:
// the largest real output measured on this host is 38.9 MB (61% of the ceiling)
// and an ordinary source-tree query is well under 1 MB. What it does NOT claim is
// that 64 MiB fits every host's memory — it is a READING limit, and a query that
// wants more is answered by narrowing it, which is what the truncation notice
// tells the caller.
const searchOutputByteCeiling = 64 << 20

// searchOutputStderrTail bounds what this wrapper keeps of a backend's STDERR.
// It is a tail, not a whole: the text exists to be reported when the backend
// fails (Rule 15), so it must not become a second unbounded buffer.
const searchOutputStderrTail = 8 << 10

// SearchCap reports whether a search backend's output was CUT SHORT by
// searchOutputByteCeiling, and how much of it the wrapper read. It travels to
// every caller so a partial answer can never be mistaken for a complete one
// (Rule 15): the hits a truncated call returns are the matches that happened to
// come first, and nothing else in the result says so.
//
// A zero SearchCap means "nothing was cut": MaxBytes is left 0 and Stopped false
// by every path that reads the backend to its end, which is what a normal query
// takes — so a caller that renders a notice for Stopped renders nothing at all in
// the ordinary case, and the ordinary case's output is byte-identical to before
// this type existed.
type SearchCap struct {
	Bytes    int64 // stdout bytes the wrapper actually read
	MaxBytes int64 // the ceiling that applied (0 = unlimited)
	Stopped  bool  // the ceiling was hit: the subprocess was killed, hits are partial
	Kept     int   // matches the caller's result list holds (0 = the caller did not say)
}

// FormatSearchCap renders a truncation notice, or "" when nothing was cut. The
// wording names what an LLM caller must do next (narrow the query), because a
// partial result that reads like a complete one is exactly the silent failure
// Rule 15 forbids.
func FormatSearchCap(c SearchCap) string {
	if !c.Stopped {
		return ""
	}
	kept := ""
	if c.Kept > 0 {
		kept = fmt.Sprintf(" (%d matches kept)", c.Kept)
	}
	return fmt.Sprintf("[search truncated: the backend's output reached the %s reading limit — "+
		"%d bytes read%s. These are ONLY the matches that came first; narrow the query to see the rest.]",
		humanBytes(c.MaxBytes), c.Bytes, kept)
}

// mergeSearchCap combines the caps of two reads of the SAME query (session search
// reads session.jsonl and, on request, the raw HTTP log). Bytes are the sum of
// what was actually read; Stopped is true when EITHER read was cut, because a
// partial answer is partial whichever file stopped short.
func mergeSearchCap(a, b SearchCap) SearchCap {
	out := SearchCap{
		Bytes:    a.Bytes + b.Bytes,
		MaxBytes: a.MaxBytes,
		Stopped:  a.Stopped || b.Stopped,
		Kept:     a.Kept + b.Kept,
	}
	if out.MaxBytes == 0 {
		out.MaxBytes = b.MaxBytes
	}
	return out
}

// humanBytes renders a byte count as MiB/KiB for the notices above.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MiB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KiB", float64(n)/float64(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// isMissingBinaryError reports whether err is the "the binary is not there"
// failure of an external backend (exec.Error wrapping exec.ErrNotFound), as
// opposed to a backend that RAN and failed. The two want opposite answers: the
// first is repaired by the in-process scanner (search_fallback.go — the host has
// no package and no repository change can install one), the second is the
// backend's own verdict and is reported as-is. Used by both search legs, so it
// is defined once here (Rule 14).
func isMissingBinaryError(err error) bool {
	var execErr *exec.Error
	return errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound)
}

// SearchScope is shared by both search tools.
type SearchScope struct {
	RootPath    string   // search root dir
	IncludeGlob []string // include globs, e.g. ["**/*.go", "**/*.md"]
	ExcludeGlob []string // exclude globs, e.g. ["vendor/**", ".git/**"]
	MaxResult   int      // result cap (prevent explosion)
}

// CodeSymbolHit is a structured symbol lookup result.
type CodeSymbolHit struct {
	SymbolName string // symbol name
	SymbolKind string // func / struct / var / const / type / ...
	FilePath   string
	LineNo     int
	Signature  string // raw definition line snapshot
}

// TextHit is a structured full-text search result.
type TextHit struct {
	FilePath string
	LineNo   int
	Snippet  string // context snippet around the match
}

// CodeSearch looks up a known symbol via ctags. Falls back to KeywordSearch
// (ripgrep) when ctags has no hit, so the tool never returns empty for a
// symbol that actually exists in the codebase. Returns the hits plus the
// number dropped by the result cap (0 when none), so the caller can render
// "N more matches" instead of silently truncating (R3-8).
func CodeSearch(scope SearchScope, symbolName string) ([]CodeSymbolHit, int, SearchCap, error) {
	return codeSearchWithTimeout(scope, symbolName, FileToolTimeout())
}

// codeSearchWithTimeout wraps CodeSearch in a goroutine+channel timeout.
func codeSearchWithTimeout(scope SearchScope, symbolName string, timeout time.Duration) (hits []CodeSymbolHit, dropped int, cap SearchCap, err error) {
	type csResult struct {
		hits    []CodeSymbolHit
		dropped int
		cap     SearchCap
		err     error
	}
	r, err := withTimeout(timeout,
		fmt.Errorf("code_search: timed out after %s", timeout),
		func(ctx context.Context) (csResult, error) {
			h, d, c, e := codeSearchImpl(ctx, scope, symbolName)
			return csResult{h, d, c, e}, e
		})
	return r.hits, r.dropped, r.cap, err
}

// KeywordSearch does full-text ripgrep search over any file (code, docs,
// config, scripts). Query may be a regex. Returns the hits plus the number
// dropped by the result cap (0 when none) — see CodeSearch.
func KeywordSearch(scope SearchScope, query string) ([]TextHit, int, SearchCap, error) {
	return keywordSearchWithTimeout(scope, query, FileToolTimeout())
}

// keywordSearchWithTimeout wraps KeywordSearch in a goroutine+channel timeout.
func keywordSearchWithTimeout(scope SearchScope, query string, timeout time.Duration) (hits []TextHit, dropped int, cap SearchCap, err error) {
	type kwResult struct {
		hits    []TextHit
		dropped int
		cap     SearchCap
		err     error
	}
	r, err := withTimeout(timeout,
		fmt.Errorf("keyword_search: timed out after %s", timeout),
		func(ctx context.Context) (kwResult, error) {
			h, d, c, e := keywordSearchImpl(ctx, scope, query)
			return kwResult{h, d, c, e}, e
		})
	return r.hits, r.dropped, r.cap, err
}

// codeSearchImpl resolves the symbol against the ctags index at scope.RootPath.
// Three legs, in order: (1) the ctags index; (2) on a clean ctags miss, the
// symbol's occurrences over the same scope — through ripgrep when this host can
// start it, through the in-process scanner when it cannot; (3) when ctags is not
// installed at all, the in-process scanner (search_fallback.go), which answers
// definitions first and occurrences second. A symbol that exists in the codebase
// is therefore never reported as absent because a host package is missing, and
// every error the CALLER sees carries this surface's own name.
// An empty result + nil error means the symbol exists nowhere.
func codeSearchImpl(ctx context.Context, scope SearchScope, symbolName string) ([]CodeSymbolHit, int, SearchCap, error) {
	symbolName = strings.TrimSpace(symbolName)
	if symbolName == "" {
		return nil, 0, SearchCap{}, fmt.Errorf("code_search: symbol must not be empty")
	}
	root := scope.RootPath
	if root == "" {
		root = "."
	}

	hits, err := queryDefinition(ctx, root, symbolName)
	if err != nil {
		if !isMissingBinaryError(err) {
			return nil, 0, SearchCap{}, err
		}
		// ctags is not installed at all: the in-process scanner is the whole
		// engine for this lookup (search_fallback.go). The error is NOT
		// propagated — propagating it was defect ②, where installing only ctags
		// turned this call into `keyword_search: rg: ...`.
		return codeSearchScannerHits(root, scope, symbolName)
	}
	if len(hits) > 0 {
		// The ctags index answers in one piece: its own size IS bounded by the
		// symbol table, so there is no reading ceiling to report here.
		kept, dropped := capSlice(hits, scope.MaxResult)
		return kept, dropped, SearchCap{}, nil
	}

	// Fallback: ctags missed (a stale index, generated code, a symbol that only
	// appears in non-definition files) — search the same scope for the symbol's
	// occurrences so the lookup never comes back empty for a symbol that actually
	// exists. The hop runs on whichever engine this host HAS: ripgrep when it is
	// installed, and the in-process scanner when it is not. Requiring rg here was
	// defect ② — a host with ctags alone got an error naming the OTHER tool
	// instead of this fallback's hits.
	if searchBackendAvailable(searchBackendRipgrep) {
		return referenceHitsViaRipgrep(ctx, scope, symbolName)
	}
	return codeSearchScannerHits(root, scope, symbolName)
}

// codeSearchScannerHits runs the in-process scanner for code_search and reports
// its failures under THIS surface's name — the leg a host without ctags runs, so
// its error text is what such a host sees (defect ③: the surface that was called is
// the surface that reports).
func codeSearchScannerHits(root string, scope SearchScope, symbolName string) ([]CodeSymbolHit, int, SearchCap, error) {
	hits, dropped, err := scanSymbolHits(root, scope, symbolName)
	if err != nil {
		return nil, 0, SearchCap{}, fmt.Errorf("code_search: %w", err)
	}
	// The in-process scanner reads file by file and never buffers a backend's
	// whole output, so nothing was cut short (see search_fallback.go).
	return hits, dropped, SearchCap{}, nil
}

// referenceHitsViaRipgrep is code_search's ctags-miss hop when ripgrep IS
// installed: the same scope is grepped for the symbol and every occurrence comes
// back kind codeSearchReferenceKind (occurrences, not definitions). An rg failure
// the scanner can repair (rg gone between the probe and the run, or never
// startable) falls through to the scanner; any other rg verdict is delivered
// under THIS tool's name, so the surface that was called is the surface that
// reports (defect ③: this hop used to surface itself as `keyword_search: rg: …`
// inside a code_search call).
func referenceHitsViaRipgrep(ctx context.Context, scope SearchScope, symbolName string) ([]CodeSymbolHit, int, SearchCap, error) {
	textHits, dropped, searchCap, err := keywordSearchViaRipgrep(ctx, scope, symbolName)
	if err != nil {
		var missing *backendMissingError
		if errors.As(err, &missing) {
			hits, d, scanErr := scanSymbolHits(searchRoot(scope), scope, symbolName)
			return hits, d, SearchCap{}, scanErr
		}
		return nil, 0, SearchCap{}, fmt.Errorf("code_search: fallback: %w", err)
	}
	hits := make([]CodeSymbolHit, 0, len(textHits))
	for _, th := range textHits {
		hits = append(hits, CodeSymbolHit{
			SymbolName: symbolName,
			SymbolKind: codeSearchReferenceKind,
			FilePath:   th.FilePath,
			LineNo:     th.LineNo,
			Signature:  th.Snippet,
		})
	}
	return hits, dropped, searchCap, nil
}

// backendMissingError marks "the external backend for this surface is not
// startable" — the condition the in-process scanner repairs. It is a distinct
// type so a caller can tell it apart from the backend's own verdict (a refusal, a
// timeout, an unreadable tree), which must be reported rather than papered over.
type backendMissingError struct {
	backend string
	err     error
}

func (e *backendMissingError) Error() string {
	return fmt.Sprintf("exec: %q: executable file not found in $PATH", e.backend)
}

func (e *backendMissingError) Unwrap() error { return e.err }

// searchRoot is the search root a scope names, with the package's own ""-means-cwd
// rule applied in ONE place (Rule 14).
func searchRoot(scope SearchScope) string {
	if scope.RootPath == "" {
		return "."
	}
	return scope.RootPath
}

// keywordSearchImpl is the ARGUMENT-checking entry point for keyword_search and
// the leg chooser: ripgrep when this host can start it, and the in-process
// scanner (search_fallback.go) when it cannot — the defect this fixes, where a
// host with no ripgrep package had NO full-text search at all.
func keywordSearchImpl(ctx context.Context, scope SearchScope, query string) ([]TextHit, int, SearchCap, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, 0, SearchCap{}, fmt.Errorf("keyword_search: query must not be empty")
	}
	root := searchRoot(scope)

	if searchBackendAvailable(searchBackendRipgrep) {
		hits, dropped, searchCap, err := keywordSearchViaRipgrep(ctx, scope, query)
		if err == nil {
			return hits, dropped, searchCap, nil
		}
		var missing *backendMissingError
		if !errors.As(err, &missing) {
			return nil, 0, SearchCap{}, err
		}
		// rg became unstartable between the probe and the run: the scanner
		// answers rather than the call failing on a binary that was there a
		// moment ago. Any OTHER rg verdict (a bad regex, an unreadable tree) is
		// delivered as-is — the scanner is not a retry for the backend's own
		// answer (Rule 15).
	}
	hits, dropped, err := scanTextHits(root, scope, query)
	if err != nil {
		return nil, 0, SearchCap{}, fmt.Errorf("keyword_search: %w", err)
	}
	// The in-process scanner streams file by file: no backend output is buffered,
	// so no reading ceiling applies (search_fallback.go:scanTextHits).
	return hits, dropped, SearchCap{}, nil
}

// keywordSearchViaRipgrep runs ripgrep over scope.RootPath with the include/exclude
// globs applied, returning one TextHit per match. A clean no-match returns an
// empty slice with nil error (rg exit 1 is "no matches", not a failure); an rg the
// process cannot start at all is reported as a backendMissingError so the caller
// can fall through to the in-process scanner.
func keywordSearchViaRipgrep(ctx context.Context, scope SearchScope, query string) ([]TextHit, int, SearchCap, error) {
	return keywordSearchViaRipgrepCapped(ctx, scope, query, searchOutputByteCeiling)
}

// keywordSearchViaRipgrepCapped is keywordSearchViaRipgrep with the reading
// ceiling as a PARAMETER, for one reason: the production value
// (searchOutputByteCeiling, 64 MiB) cannot be exercised by a test without
// writing 64 MiB of fixture, and a bound nobody can test is a bound nobody can
// trust. The production entry point above passes the constant and nothing else
// does (Rule 14: one definition, one production reader); the tests pass a few KB
// and assert the SAME code path stops, kills and reports.
func keywordSearchViaRipgrepCapped(ctx context.Context, scope SearchScope, query string, byteCeiling int64) ([]TextHit, int, SearchCap, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, 0, SearchCap{}, fmt.Errorf("keyword_search: query must not be empty")
	}
	root := scope.RootPath
	if root == "" {
		root = "."
	}

	// --column gives the 1-based match column, which buildSnippet uses to
	// center the snippet on the match. No -m per-file cap: the full rg output
	// is already captured for capping, and counting every match lets the
	// result render "N more matches" when the cap bites (R3-8 — the old
	// silent cap at 20 hid the real target in A/B query A14). The total cap
	// below bounds what the model sees; the transient buffer is bounded by
	// the tool timeout.
	// -H forces the filename prefix even when the root is a SINGLE FILE. Without
	// it rg prints `line:col:text` for that shape and splitRgColumnLine reads the
	// COLUMN as the line number — measured: every hit of a file-rooted search
	// reported line 36 (the match column) instead of its own line. The wrapper is
	// file-rooted by every session search, so the flag is unconditional rather
	// than conditional on the root's kind: a directory search is unaffected
	// (rg prints the prefix there anyway) and one code path is easier to keep
	// correct than two.
	args := []string{"-H", "-n", "--no-heading", "--color", "never", "--column"}
	for _, g := range scope.IncludeGlob {
		args = append(args, "-g", g)
	}
	for _, g := range scope.ExcludeGlob {
		args = append(args, "-g", "!"+g)
	}
	args = append(args, "--", query, root)

	cmd := exec.CommandContext(ctx, searchBackendRipgrep, args...)
	hits, dropped, searchCap, err := runSearchBackend(ctx, cmd, scope.MaxResult, byteCeiling)
	if err != nil {
		return nil, 0, SearchCap{}, err
	}
	return hits, dropped, searchCap, nil
}

// runSearchBackend executes one search backend and parses its stdout AS IT
// ARRIVES, stopping at byteCeiling. Everything about the memory bound lives here
// and nowhere else (Rule 14): the caller supplies the command and the result cap,
// and gets back the hits, the byte counts, the result cap's own drop count and the
// SearchCap that says whether the READING was cut.
//
// The three shapes of a backend run, all preserved from the previous
// CombinedOutput implementation:
//   - exit 1 with no output is "no matches" — an empty result, not an error;
//   - a binary that cannot start is a *backendMissingError (the in-process
//     scanner repairs it);
//   - any other non-zero exit is the backend's own verdict, reported with its
//     stderr tail.
//
// What is NEW is the reading loop: the previous code held the backend's ENTIRE
// stdout in a []byte before parsing one line of it, so peak memory was
// O(sum of matching line lengths) — measured on this host: 38.9 MB of output for
// query "the" over one session file, and a run killed at 11.4 GB RSS for query
// "tool_result". Reading incrementally across lines makes the bound a constant:
// one line (searchOutputLineCeiling) plus the ceiling itself.
func runSearchBackend(ctx context.Context, cmd *exec.Cmd, maxResult int, byteCeiling int64) ([]TextHit, int, SearchCap, error) {
	var stderr tailBuffer
	stderr.limit = searchOutputStderrTail
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, 0, SearchCap{}, fmt.Errorf("keyword_search: %s: stdout pipe: %w", searchBackendRipgrep, err)
	}
	if err := cmd.Start(); err != nil {
		if isMissingBinaryError(err) {
			// Not startable at all: the caller decides between the in-process
			// scanner and reporting the miss — the error is typed, never a
			// formatted string the caller would have to pattern-match.
			return nil, 0, SearchCap{}, &backendMissingError{backend: searchBackendRipgrep, err: err}
		}
		return nil, 0, SearchCap{}, fmt.Errorf("keyword_search: %s: %w", searchBackendRipgrep, err)
	}

	// REBASE NOTE (delivery round 2/3, 055ee385 onto 83af03c5): the two sides are
	// ORTHOGONAL properties of the same read, and both are kept.
	//   - this branch (055ee385) replaced the whole-output loop with a STREAMING
	//     one bounded by byteCeiling, so peak memory is a constant instead of
	//     O(sum of matching line lengths);
	//   - master (83af03c5 / e4833f57) made an unreadable output line a FAILURE
	//     instead of a silent `continue` (Rule 15, search_parse_failure.go).
	// The loop is THIS branch's (that is what this commit delivers); the
	// unparseable counter master added lives inside readBackendStdout, which is
	// the function that now owns the per-line parse.
	hits, matched, read, stopped, unparseable, firstUnparseable, scanErr := readBackendStdout(stdout, maxResult, byteCeiling)
	dropped := matched - len(hits) // matches beyond the result cap — the renderer's "N more"

	// A line the backend printed but the parser could not read is a FAILURE even
	// when other lines parsed: a partial answer that silently omits matches is
	// the same defect at a smaller dose, and nothing downstream could tell the
	// caller which matches were lost (Rule 15; search_parse_failure.go). The
	// count and the first offending line travel with it so a reader learns the
	// scale and can find the line. It is checked BELOW, after the child's own
	// verdict has been collected, so a backend that failed outright reports THAT.
	if stopped {
		// The ceiling was reached: stop the producer NOW rather than draining
		// output we will not parse. The hits already collected ARE the answer,
		// and SearchCap.Stopped says they are only part of it (Rule 15) — the
		// fact the old cmd.CombinedOutput() implementation could not express,
		// because it had no way to stop reading at all.
		if cmd.Process != nil {
			if killErr := cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
				// A process that exited between the read and the kill is not a
				// failure to report — the reading stopped either way. Anything
				// else deserves a line in the notice's own text.
				stderr.Write([]byte(" kill: " + killErr.Error())) //nolint:errcheck // best-effort note inside an already-failed read
			}
		}
		// Wait reaps the killed child. Its exit status is the KILL's, not a
		// verdict, so it is deliberately not an error here.
		_ = cmd.Wait() //stylegate:ignore the child was killed deliberately; its status is this run's own kill, not the backend's verdict
		// The kill above is the caller's own act, so the child is not leaked —
		// and this is the ONE place the two sides meet: a truncated read that
		// ALSO lost an unreadable line reports the harder fact (Rule 15), because
		// "partial" already says the answer is short and "unreadable" says the
		// wrapper is wrong.
		if unparseable > 0 {
			return nil, 0, SearchCap{}, searchParseFailure(firstUnparseable, unparseable, len(hits))
		}
		return hits, dropped, SearchCap{Bytes: read, MaxBytes: byteCeiling, Stopped: true, Kept: len(hits)}, nil
	}

	// The read reached EOF, so the child never blocks on a full pipe; Wait closes
	// the pipe and collects the backend's own verdict.
	waitErr := cmd.Wait()

	if scanErr != nil {
		return nil, 0, SearchCap{}, fmt.Errorf("keyword_search: scan rg output: %w", scanErr)
	}
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, 0, SearchCap{}, nil // no matches — not an error
		}
		if isMissingBinaryError(waitErr) {
			return nil, 0, SearchCap{}, &backendMissingError{backend: searchBackendRipgrep, err: waitErr}
		}
		return nil, 0, SearchCap{}, fmt.Errorf("keyword_search: %s: %w: %s", searchBackendRipgrep, waitErr, stderr.String())
	}
	// The backend finished cleanly, so every line it printed was a match it meant
	// to report — and a line the parser could not read is therefore a wrapper
	// defect, not an empty tree (Rule 15).
	if unparseable > 0 {
		return nil, 0, SearchCap{}, searchParseFailure(firstUnparseable, unparseable, len(hits))
	}
	return hits, dropped, SearchCap{Bytes: read}, nil
}

// readBackendStdout parses a backend's stdout incrementally, accumulating the
// bytes it has read. It stops as soon as byteCeiling would be exceeded and
// reports that fact, so the caller can kill the producer.
//
// Bounded memory, stated exactly: the line buffer is capped at
// searchOutputLineCeiling, the byte counter at byteCeiling, the hit list at
// maxResult — so the total is a CONSTANT, independent of how much the backend
// would have written. The bytes counter counts what was READ (the ceiling plus at
// most one line), which is what the truncation notice reports.
func readBackendStdout(r io.Reader, maxResult int, byteCeiling int64) (hits []TextHit, matched int, read int64, stopped bool, unparseable int, firstUnparseable string, err error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), searchOutputLineCeiling)
	for scanner.Scan() {
		line := scanner.Bytes()
		read += int64(len(line)) + 1 // +1 for the newline the scanner consumed
		if byteCeiling > 0 && read > byteCeiling {
			return hits, matched, read, true, unparseable, firstUnparseable, nil
		}
		file, lineNo, snippet, ok := splitRgColumnLine(string(line))
		if !ok {
			// master's Rule 15 arm, kept INSIDE the streaming loop: a line the
			// parser cannot read is a FAILURE, not a miss, and it is counted here
			// because this function is what owns the per-line parse now. The
			// string() copy is required — scanner.Bytes() reuses its buffer.
			if unparseable == 0 {
				firstUnparseable = string(line)
			}
			unparseable++
			continue
		}
		// EVERY match is COUNTED, only maxResult of them are KEPT: `dropped` must
		// keep meaning "matches beyond the cap" exactly as capSlice defined it
		// (R3-8: the renderer prints that number). Counting is free; holding every
		// one of them is what this function exists to stop.
		matched++
		if maxResult > 0 && len(hits) >= maxResult {
			continue
		}
		hits = append(hits, TextHit{FilePath: file, LineNo: lineNo, Snippet: snippet})
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return hits, matched, read, false, unparseable, firstUnparseable, scanErr
	}
	return hits, matched, read, false, unparseable, firstUnparseable, nil
}

// tailBuffer is a writer that keeps only the LAST limit bytes written to it, so
// reporting a backend's stderr never becomes a second unbounded buffer (the same
// defect this file's byte ceiling fixes on stdout).
type tailBuffer struct {
	limit int
	buf   []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if t.limit > 0 && len(t.buf) > t.limit {
		t.buf = t.buf[len(t.buf)-t.limit:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return strings.TrimSpace(string(t.buf)) }

// splitRgColumnLine parses one rg --column output line ("path:line:col:text").
// col is a 1-based byte offset.
//
// ok=false means THIS LINE could not be read — and the caller must treat that as
// a FAILURE, not a miss (Rule 15). The line is ambiguous by construction: a path
// containing a colon ("colon:name.cfg") shifts every field, so rg's own output
// for a perfectly ordinary file cannot be split back into (path, line, col,
// text). The pre-fix wrapper answered `continue` here, which made that wrapper
// defect look exactly like "no matches"; keywordSearchViaRipgrep now counts
// these and fails the call.
func splitRgColumnLine(line string) (file string, lineNo int, snippet string, ok bool) {
	parts := strings.SplitN(line, ":", 4)
	if len(parts) < 4 {
		return "", 0, "", false
	}
	lineNo, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, "", false
	}
	col, err := strconv.Atoi(parts[2])
	if err != nil {
		col = 0
	}
	// parts[0] is a SUBSTRING of line too, so it shares the whole row's backing
	// array exactly as the snippet does — and it is the field that survived: the
	// snippet was already cloned, the path was not, and the sweep measured the
	// difference. On 20 matched rows of 4 MB with MaxResult 50, every retained
	// hit pinned its own 4 MB row through FilePath alone: peak RSS 17.9 MB on
	// one row, 98.5 MB on twenty, i.e. still LINEAR in the output size. With the
	// clone the same sweep is flat (the numbers are in this task's report).
	return strings.Clone(parts[0]), lineNo, buildSnippet(parts[3], col), true
}

// buildSnippet returns a context snippet around the match. Short lines are
// returned whole; long lines are trimmed to maxSnippetLen bytes centered on
// the match column, with ellipses marking the truncation. Slicing is stepped
// to rune boundaries so the snippet stays valid UTF-8.
func buildSnippet(line string, matchCol int) string {
	// maxSnippetLen is the window's width in bytes; snippetLeadContext is how
	// much of it sits BEFORE the match column.
	const maxSnippetLen = 100
	const snippetLeadContext = 30
	line = strings.TrimRight(line, "\r\n")
	if len(line) <= maxSnippetLen || matchCol <= 0 {
		return line
	}
	start := matchCol - 1 - snippetLeadContext
	if start < 0 {
		start = 0
	}
	// A column PAST the text is an input this parser declared it must survive:
	// rg's line is "path:line:col:text", so a colon in the PATH shifts every
	// field and the value read as "column" may exceed the text's length.
	// Clamping only from BELOW left start > end here, and line[start:end]
	// panicked (measured: index out of range [3999970] with length 400). The
	// window is therefore pinned fully INSIDE the line, so a match past the end
	// answers the line's tail instead of crashing.
	if start > len(line)-maxSnippetLen {
		start = len(line) - maxSnippetLen
	}
	end := start + maxSnippetLen
	for start > 0 && !utf8.RuneStart(line[start]) {
		start--
	}
	for end < len(line) && !utf8.RuneStart(line[end]) {
		end++
	}
	s := line[start:end]
	if start > 0 {
		s = "…" + s
	}
	if end < len(line) {
		s += "…"
	}
	return s
}

// capSlice truncates results to max (0 = unlimited) and reports how many
// were dropped, so the caller can render "N more matches" instead of
// silently truncating (R3-8 — A/B A14: the 21st+ match was the real target
// under the old cap of 20).
func capSlice[T any](s []T, max int) ([]T, int) {
	if max > 0 && len(s) > max {
		return s[:max], len(s) - max
	}
	return s, 0
}

// FormatCodeSymbolHits renders structured symbol hits as the compact block the
// LLM sees: "name  file:line  kind  signature" per hit. An empty hit list is
// rendered as a "no symbol" hint, not silence. dropped > 0 appends an
// overflow notice — the cap is len(hits) exactly when anything was dropped.
func FormatCodeSymbolHits(symbolName string, hits []CodeSymbolHit, dropped int) string {
	if len(hits) == 0 {
		return fmt.Sprintf("No symbol %q found in ctags index or ripgrep.", symbolName)
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		loc := h.FilePath
		if h.LineNo > 0 {
			loc = fmt.Sprintf("%s:%d", h.FilePath, h.LineNo)
		}
		out[i] = fmt.Sprintf("%s  %s  %s  %s", h.SymbolName, loc, h.SymbolKind, h.Signature)
	}
	if dropped > 0 {
		out = append(out, fmt.Sprintf("… plus %d more matches (capped at %d)", dropped, len(hits)))
	}
	return strings.Join(out, "\n")
}

// FormatTextHitsWithCap renders a keyword_search result and, when the READ was
// cut short, the truncation notice. A truncated call is never rendered as a
// complete one — and in particular a truncated call with ZERO hits kept must not
// print "No matches.", which would claim the query ran to the end of the data
// when it did not (Rule 15; the one case FormatSearchCap exists for).
//
// A NON-truncated call takes FormatTextHits verbatim, so ordinary output is
// byte-identical to before this notice existed.
func FormatTextHitsWithCap(hits []TextHit, dropped int, cap SearchCap) string {
	notice := FormatSearchCap(cap)
	if notice == "" {
		return FormatTextHits(hits, dropped)
	}
	if len(hits) == 0 {
		return "No matches in the part of the output that was read.\n" + notice
	}
	return FormatTextHits(hits, dropped) + "\n" + notice
}

// FormatCodeSymbolHitsWithCap is FormatTextHitsWithCap's sibling for code_search:
// the same "a truncated result is never rendered as a complete one" rule, applied
// to the "No symbol … found" sentence, which a truncated read has not earned.
func FormatCodeSymbolHitsWithCap(symbolName string, hits []CodeSymbolHit, dropped int, cap SearchCap) string {
	notice := FormatSearchCap(cap)
	if notice == "" {
		return FormatCodeSymbolHits(symbolName, hits, dropped)
	}
	if len(hits) == 0 {
		return fmt.Sprintf("No symbol %q in the part of the output that was read.\n%s", symbolName, notice)
	}
	return FormatCodeSymbolHits(symbolName, hits, dropped) + "\n" + notice
}

// FormatTextHits renders structured text hits as "file:line: snippet" lines.
// dropped > 0 appends an overflow notice (see FormatCodeSymbolHits).
func FormatTextHits(hits []TextHit, dropped int) string {
	if len(hits) == 0 {
		return "No matches."
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = fmt.Sprintf("%s:%d: %s", h.FilePath, h.LineNo, h.Snippet)
	}
	if dropped > 0 {
		out = append(out, fmt.Sprintf("… plus %d more matches (capped at %d)", dropped, len(hits)))
	}
	return strings.Join(out, "\n")
}
