package gopilog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/rawlogfmt"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/sessionlayout"
)

const rawLogsDir = "raw" // under the gopi data root (config.GopiDir)

// Raw log marker text is NOT re-spelled here (Rules 13/14 — the vocabulary's one
// owner is `internal/rawlogfmt`, and this package reads it from there when it needs
// it; `rounds.go` is the only consumer, for the round's user-visible marker line).
//
// Marker text can also appear INSIDE a payload (a prompt that quotes the format, or
// any payload byte sequence at all in the length-prefixed framing), so a reader must
// compare the WHOLE line, and — since the framed framing exists — a reader that must
// be format-independent dispatches on a record's TAG instead of on this text.

// Raw log scanner buffer sizes. Request bodies routinely exceed the session
// JSONL line limit (observed > 500 KiB in real logs), so the raw scanner gets
// its own named constants (PROJECT.md Rule 1: no magic numbers).
const (
	rawScannerInitBufSize = 1 << 20  // initial raw scanner buffer (1 MiB)
	rawScannerMaxBufSize  = 64 << 20 // max raw line length before scanner error (64 MiB)
)

// maxRawLogMatchDelta is the largest allowed gap between a session's
// session_start timestamp and a raw log's embedded start time. Raw log
// filenames carry second precision in the machine's local wall clock; real
// pairs match to well under a second, while unrelated raw logs are >= 1 hour
// away (verified across all 190 sessions on this machine).
const maxRawLogMatchDelta = 2 * time.Minute

// jsonIndent is the indentation used when pretty-printing the full request
// body for --request --json output.
const jsonIndent = "  "

// maxRawViewBytes caps how much of a raw log the --raw view prints verbatim.
// Raw logs grow to hundreds of MB; a view must never slurp the whole file
// into memory. Beyond the cap the output is truncated with an explicit
// marker so the user knows the tail was cut (the tail holds the LAST round,
// which is usually what debugging wants — the truncation note points at the
// file for the full picture).
const maxRawViewBytes = 64 << 20 // 64 MiB

// rawLogNameRe matches gopi-raw-YYYYMMDD-HHMMSS.log and the collision-suffixed
// forms the writer falls back to when a second agent starts inside the same
// wall-clock second (internal/agent/rawlog.go createRawLogFile):
// gopi-raw-YYYYMMDD-HHMMSS-<n>.log and gopi-raw-YYYYMMDD-HHMMSS-u<pid>-<n>.log.
// The suffix carries no time information — the embedded start second is
// identical for every sibling — so rawLogStart ignores it.
var rawLogNameRe = regexp.MustCompile(`^gopi-raw-(\d{8})-(\d{6})(?:-\d+|-u\d+-\d+)?\.log$`)

// RequestPayload is the Anthropic Messages API request body captured in a raw
// log after a → REQUEST marker. Raw preserves the exact logged body for
// --request --json output.
type RequestPayload struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    []SystemBlock    `json:"system"`
	Messages  []RequestMessage `json:"messages"`
	Raw       json.RawMessage  `json:"-"`
}

// SystemBlock is one entry of the top-level "system" array (modern shape).
type SystemBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// RequestMessage is one entry of the "messages" array. Content stays raw
// because legacy logs carry the system prompt as messages[0].content, which
// may be a plain string or a content-block array.
type RequestMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func resolveRawLogDir() (string, error) {
	gd, err := config.GopiDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve gopi dir: %w", err)
	}
	return filepath.Join(gd, rawLogsDir), nil
}

// listRawLogFiles returns raw log paths sorted by embedded start time
// (oldest first).
func listRawLogFiles() ([]string, error) {
	dir, err := resolveRawLogDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && rawLogNameRe.MatchString(e.Name()) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Slice(files, func(i, j int) bool {
		ti, _ := rawLogStart(filepath.Base(files[i]), time.Local)
		tj, _ := rawLogStart(filepath.Base(files[j]), time.Local)
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		// Same embedded second (collision siblings): order by name so the
		// listing is deterministic run to run.
		return filepath.Base(files[i]) < filepath.Base(files[j])
	})
	return files, nil
}

// rawLogStart parses the start time embedded in a raw log filename
// (gopi-raw-YYYYMMDD-HHMMSS.log, local wall clock) into loc.
func rawLogStart(name string, loc *time.Location) (time.Time, bool) {
	m := rawLogNameRe.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("20060102-150405", m[1]+"-"+m[2], loc)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// FindRawLogForSession pairs a session with the raw log whose embedded start
// time is closest to the session's session_start timestamp. There is no
// explicit session-id link between the two log kinds, so timestamp proximity
// is the only signal (both are written by the same machine in the same local
// zone).
//
// It returns a clear error (never a crash) when no raw log is close enough —
// raw HTTP logging was probably not enabled when the session ran — or when
// two raw logs are equally close.
func FindRawLogForSession(sessionStart time.Time) (string, error) {
	files, err := listRawLogFiles()
	if err != nil {
		return "", fmt.Errorf("cannot list raw logs: %w", err)
	}
	if len(files) == 0 {
		dir, _ := resolveRawLogDir() //stylegate:ignore path probe; the empty result is named in the error the caller returns
		return "", fmt.Errorf("no raw logs found in %s (the raw HTTP logger may not be enabled)", dir)
	}

	loc := sessionStart.Location()
	best := ""
	bestDelta := time.Duration(0)
	// secondDelta tracks the runner-up delta; -1 means "no second candidate
	// seen yet" so a single exact-second match (bestDelta == 0) is not
	// misread as an ambiguity tie.
	secondDelta := time.Duration(-1)
	for _, f := range files {
		start, ok := rawLogStart(filepath.Base(f), loc)
		if !ok {
			continue
		}
		d := sessionStart.Sub(start)
		if d < 0 {
			d = -d
		}
		if best == "" {
			best = f
			bestDelta = d
		} else if d < bestDelta {
			secondDelta = bestDelta
			bestDelta = d
			best = f
		} else if d == bestDelta {
			secondDelta = d
		}
	}
	if best == "" {
		return "", fmt.Errorf("no raw log matched session start %s", sessionStart.Format(time.RFC3339))
	}
	if bestDelta > maxRawLogMatchDelta {
		return "", fmt.Errorf(
			"no raw log found for session started %s: closest candidate is %s (%s away, more than %s) — raw HTTP logging was probably not enabled for this session",
			sessionStart.Format(time.RFC3339), filepath.Base(best), bestDelta.Round(time.Second), maxRawLogMatchDelta)
	}
	if secondDelta == bestDelta {
		return "", fmt.Errorf(
			"ambiguous raw log match for session started %s: %s and other raw log(s) share the same start second — name the log explicitly with --file",
			sessionStart.Format(time.RFC3339), filepath.Base(best))
	}
	return best, nil
}

// sessionStartLineBytes bounds how much of a session file's head is read to
// find its session_start event. The event is the file's first line and is
// short (id, model, workdir, raw log path), so 8 KiB is generous; the bound
// keeps the shared-raw-log scan O(one small read) per session instead of
// O(session length).
const sessionStartLineBytes = 8 << 10

// SessionsRecordingRawLog returns the id of every session in the session
// directory whose session_start event records rawLogPath as its raw log.
//
// A raw log is written by one agent into one file, so normally exactly one
// session records it. More than one means the file is SHARED — two agents
// resolved the same name and O_APPEND interleaved their HTTP traffic — and the
// file's round numbers are not session-scoped: `gopi session raw <session> N` can hand
// back another session's request. Callers surface that instead of silently
// printing a foreign session's bytes (best-effort: an unreadable session dir
// yields an empty result, never an error path that blocks the view).
func SessionsRecordingRawLog(rawLogPath string) []string {
	dir, err := ResolveSessionDir()
	if err != nil {
		return nil
	}
	want := filepath.Clean(rawLogPath)

	// SCOPE FIRST (measured 2026-09-25): this runs on the `gopi session raw` path, and
	// a whole-tree walk costs 173,589 openat here — 100x the actual work. Under
	// the co-located layout (session-dir-unify S1) the owner can only be in the
	// log's OWN directory, so that is the whole search.
	if ids, scoped := rawLogOwnersInRunDir(dir, want); scoped {
		return ids
	}

	// Legacy flat layout (the log in <gopi dir>/raw, sessions directly under
	// the sessions root): no directory links the two, so the walk is the only
	// way — correctness here, not speed.
	var ids []string
	head := make([]byte, sessionStartLineBytes)

	// Scan the session tree through the shared layout classifier
	// (internal/sessionlayout): session JSONLs live one level deep — task-run
	// files <task_id>/<uuid>.jsonl and bare <uuid>/session.jsonl — alongside
	// legacy flat <uuid>.jsonl files, and a session dir's SIDE FILES
	// (signal-provenance.jsonl) are not sessions at all. Globbing "*.jsonl"
	// here would inspect the provenance record's head as if it were a
	// session.
	records, err := sessionlayout.Records(dir)
	if err != nil {
		return nil
	}
	for _, r := range records {
		p := r.Path
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		n, _ := f.Read(head) //stylegate:ignore short read of a raw-log head; the caller inspects the returned n
		f.Close()
		line := firstLine(head[:n])
		if len(line) == 0 {
			continue
		}
		var ev RawEvent
		if json.Unmarshal(line, &ev) != nil || ev.Type != sessionevent.EventTypeSessionStart {
			continue
		}
		if filepath.Clean(ev.RawLogPath) == want {
			ids = append(ids, FileToID(p))
		}
	}
	sort.Strings(ids)
	return ids
}

// rawLogOwnersInRunDir returns every session that records want as its raw log
// when want sits INSIDE the sessions tree, which is the co-located layout the
// writer produces (internal/agent.NewRawLoggerForRun).
//
// scoped=false means "not a co-located log" (want is outside the tree, or
// directly in the sessions root) and the caller falls back to the tree walk:
// the narrowing must never turn into a missing warning.
//
// WHY THE OWNER CAN ONLY BE IN THAT ONE DIRECTORY: a run directory holds the
// assets of ONE run — the session record, its side files and its raw log — so
// a session that recorded a raw log beside its own JSONL is in that JSONL's
// directory by construction. Each record there is confirmed against the
// session_start it recorded (SessionRawLogPath, one small first-line read), so
// the answer stays a comparison and not an inference from the directory.
func rawLogOwnersInRunDir(dir, want string) ([]string, bool) {
	parent := filepath.Dir(want)
	rel, err := filepath.Rel(dir, parent)
	if err != nil || rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") || strings.ContainsRune(rel, filepath.Separator) {
		return nil, false
	}
	subs, err := os.ReadDir(parent)
	if err != nil {
		return nil, false
	}
	ids := []string{}
	for _, se := range subs {
		if se.IsDir() || !sessionlayout.IsJSONL(se.Name()) || sessionlayout.IsSideFile(se.Name()) {
			continue
		}
		p := filepath.Join(parent, se.Name())
		if filepath.Clean(SessionRawLogPath(p)) != want {
			continue
		}
		ids = append(ids, FileToID(p))
	}
	sort.Strings(ids)
	return ids, true
}

// firstLine returns line 1 of b (the text before the first newline), or the
// whole buffer when b holds no newline (a truncated first line still parses if
// it is short enough to have fit in the read).
func firstLine(b []byte) []byte {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return b[:i]
	}
	return b
}

// SessionRawLogPath reads the rawLogPath recorded in a session file's
// session_start event (written by the agent since the resume feature). Empty
// string means the session predates the field (or the file is unreadable) —
// callers fall back to timestamp-based pairing.
func SessionRawLogPath(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := newJSONLScanner(path, f, jsonlLineSafetyCeiling)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if ev.Type == sessionevent.EventTypeSessionStart {
			return ev.RawLogPath
		}
	}
	return ""
}

// SessionStartTime reads the session_start timestamp from a session file
// without parsing the whole conversation.
func SessionStartTime(path string) (time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, err
	}
	defer f.Close()

	scanner := newJSONLScanner(path, f, jsonlLineSafetyCeiling)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if ev.Type == sessionevent.EventTypeSessionStart {
			return ev.Timestamp, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return time.Time{}, err
	}
	return time.Time{}, fmt.Errorf("no session_start event found in %s", path)
}

// SessionRecordedRawLog returns the raw log path the session RECORDED in its
// session_start event, plus whether that file still exists. An empty path
// means the session recorded none at all (it predates the field, or raw
// logging was off).
//
// The two halves are reported separately because they call for different
// answers, and collapsing them into one "no raw log" is what made the failure
// unexplainable: a session that RECORDED a path but whose file is gone did
// engage raw logging (removal by the scratch GC is the usual cause), while a
// session that recorded nothing never opened a log at all. The caller can then
// name the path that is missing instead of scanning for a replacement and
// reporting a message about a directory the operator never asked about.
func SessionRecordedRawLog(sessionPath string) (string, bool) {
	p := SessionRawLogPath(sessionPath)
	if p == "" {
		return "", false
	}
	if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
		return p, true
	}
	return p, false
}

// SiblingRawLog returns the raw log that sits BESIDE a session file, when its
// basename matches what the session recorded as its rawLogPath. Empty string
// when there is no such file.
//
// WHY this exists (epic task-session-cwd, the flat-raw re-home): the rawLogPath
// a session records is an ABSOLUTE path written when the run started, so a raw
// log that was later MOVED into its session directory (the re-home sweep's
// whole purpose) would otherwise stop resolving — the record would point at a
// file that no longer exists while the evidence sits one directory below, and
// `gopi session raw` / `gopi session stats` / `gopi session resume` would answer "no raw log". The
// recorded BASENAME is stable across the move, which is what makes the sibling
// lookup a resolution and not a guess: same run uuid / same timestamp name, and
// only ever the session's OWN directory.
func SiblingRawLog(sessionPath, recorded string) string {
	if sessionPath == "" || recorded == "" {
		return ""
	}
	cand := filepath.Join(sessionlayout.RecordContainer(sessionPath), filepath.Base(recorded))
	if cand == recorded {
		return "" // the recorded path IS the sibling — nothing new to offer
	}
	if _, err := os.Stat(cand); err != nil {
		return ""
	}
	return cand
}

// ResolveRawLogForSession pairs a session file with its raw HTTP log,
// preferring the exact rawLogPath recorded in the session's session_start
// event (written by the agent since the resume feature) over timestamp-based
// pairing. The recorded path is authoritative even for custom
// GOPI_RAW_LOG_DIR setups; timestamp pairing (FindRawLogForSession) is only
// the fallback for sessions that predate the field.
func ResolveRawLogForSession(sessionPath string) (string, error) {
	if p := SessionRawLogPath(sessionPath); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		// The recorded file is gone from where it was written. Before giving up
		// on the RECORD (and before falling back to a timestamp pairing that
		// scans the whole flat dir), try the session's own directory: a raw log
		// re-homed next to its session keeps its basename, so the record still
		// identifies it exactly.
		if sib := SiblingRawLog(sessionPath, p); sib != "" {
			return sib, nil
		}
		// Recorded path exists in the log but the file is gone — fall
		// through to timestamp pairing so the error (if any) is accurate.
	}
	start, err := SessionStartTime(sessionPath)
	if err != nil {
		return "", err
	}
	return FindRawLogForSession(start)
}

// FormatRawLog returns a raw log's content verbatim (markers + payloads,
// exactly as the raw HTTP logger wrote them), capped at maxRawViewBytes. When
// the log is larger than the cap, the returned text ends with an explicit
// truncation marker naming the file, so debugging the LAST round (the usual
// ask) still works via `gopi session log --last --raw` against smaller logs or by
// pointing the user at the file itself.
func FormatRawLog(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	// If the file fits, stream it verbatim (io.Copy is faster than a
	// scanner for byte-exact passthrough). If not, copy only the cap.
	limit := fi.Size()
	truncated := false
	if limit > maxRawViewBytes {
		limit = maxRawViewBytes
		truncated = true
	}
	if _, err := io.Copy(&sb, io.LimitReader(f, limit)); err != nil {
		return "", err
	}
	if truncated {
		sb.WriteString(fmt.Sprintf(
			"\n… [--raw view truncated at %d bytes; full log: %s]\n",
			maxRawViewBytes, path))
	}
	return sb.String(), nil
}

// ParseFirstRequest walks a raw log and returns the FIRST request body's payload.
// The system prompt does not change across iterations of a session, so the first
// request's system blocks are authoritative.
//
// It dispatches on the RECORD'S TAG, so it reads both framings: the legacy marker
// line (`→ REQUEST`) and the length-prefixed header (`R 412`) open the same kind of
// record, and a payload that happens to CONTAIN marker text (which the framed
// framing makes ordinary) cannot open one.
func ParseFirstRequest(path string) (*RequestPayload, error) {
	format, err := rawlogfmt.Detect(path)
	if err != nil {
		return nil, err
	}
	r, err := rawlogfmt.OpenReader(path, format)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	for {
		rec, rerr := r.Next()
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				// The message names the FORMAT-INDEPENDENT fact: both framings carry
				// request records, so a message spelling one framing's marker would
				// misdescribe the other file the reader may be holding.
				return nil, fmt.Errorf("no request record found in raw log %s", path)
			}
			return nil, fmt.Errorf("raw log %s: %w", path, rerr)
		}
		if rec.Tag != rawlogfmt.TagRequest && rec.Tag != rawlogfmt.TagProbeRequest {
			continue
		}
		body := trimFramingNewline(rec.Payload)
		if len(bytes.TrimSpace(body)) == 0 {
			return nil, fmt.Errorf("raw log %s: request record at offset %d carries no body", path, rec.Offset)
		}
		p, perr := parseRequestBody(bytes.TrimSpace(body))
		if perr != nil {
			return nil, fmt.Errorf("raw log %s: invalid request body in the record at offset %d: %w", path, rec.Offset, perr)
		}
		return p, nil
	}
}

// trimFramingNewline removes the ONE trailing newline the framing owns (both the
// legacy logger and the framed encoder append exactly one after a payload), leaving
// the payload's own bytes.
func trimFramingNewline(payload []byte) []byte {
	if n := len(payload); n > 0 && payload[n-1] == rawlogfmt.RecordTerminator {
		return payload[:n-1]
	}
	return payload
}

// parseRequestBody unmarshals a request body, keeping the exact bytes for
// --request --json output.
func parseRequestBody(body []byte) (*RequestPayload, error) {
	p := &RequestPayload{}
	if err := json.Unmarshal(body, p); err != nil {
		return nil, err
	}
	// Preserve the exact logged body for full-JSON output.
	p.Raw = append(json.RawMessage(nil), body...)
	return p, nil
}

// extractSystemPrompts returns the verbatim system prompt text sent with the
// request. It handles both the modern shape ("system" top-level array of
// {type,text} blocks) and the legacy shape (a "messages" entry with
// role="system", whose content is a string or a text-block array). Blocks are
// returned in the order they appear in the payload.
func extractSystemPrompts(p *RequestPayload) []string {
	var prompts []string
	for _, b := range p.System {
		if b.Text != "" {
			prompts = append(prompts, b.Text)
		}
	}
	for _, m := range p.Messages {
		if m.Role != "system" || len(m.Content) == 0 {
			continue
		}
		// content may be a plain string or an array of text blocks;
		// collect ALL text blocks either way.
		var s string
		if json.Unmarshal(m.Content, &s) == nil {
			if s != "" {
				prompts = append(prompts, s)
			}
			continue
		}
		var blocks []ContentBlock
		if json.Unmarshal(m.Content, &blocks) == nil {
			for _, b := range blocks {
				if b.Text != "" {
					prompts = append(prompts, b.Text)
				}
			}
		}
	}
	return prompts
}

// FormatRequestSystem renders the verbatim system prompt block(s) of the
// session's authoritative (first) request. In textMode the header/borders are
// omitted and only the raw prompt text is emitted (pipe-friendly).
func FormatRequestSystem(p *RequestPayload, rawLogName string, textMode bool) string {
	var sb strings.Builder

	prompts := extractSystemPrompts(p)
	if textMode {
		for _, text := range prompts {
			sb.WriteString(text)
			sb.WriteString("\n")
		}
		return sb.String()
	}

	sb.WriteString(headerSep + "\n")
	sb.WriteString("REQUEST — system prompt (verbatim)\n")
	sb.WriteString("Raw log: " + rawLogName + "\n")
	if p.Model != "" {
		sb.WriteString("Model:   " + p.Model + "\n")
	}
	sb.WriteString(headerSep + "\n\n")

	if len(prompts) == 0 {
		sb.WriteString("(no system prompt blocks found in the request body)\n")
		return sb.String()
	}
	for i, text := range prompts {
		if len(prompts) > 1 {
			sb.WriteString(fmt.Sprintf("SYSTEM PROMPT [%d/%d]:\n", i+1, len(prompts)))
		} else {
			sb.WriteString("SYSTEM PROMPT:\n")
		}
		sb.WriteString(text)
		sb.WriteString("\n")
		if i < len(prompts)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// FirstUserMessage returns the text of the FIRST role="user" message of a
// request body — the user's own input as it went on the wire.
//
// "As it went on the wire" is the operative phrase: HTTP has no separate field
// for "what the human typed". The user's text and any directives the agent
// appends to it (a task-start checkpoint, an epic briefing) travel as ONE
// user-role content block, so the returned string is the whole block, injected
// suffixes included. Splitting them back apart is not possible from the log —
// that information only exists before the request is built.
func FirstUserMessage(p *RequestPayload) (string, bool) {
	for _, m := range p.Messages {
		if m.Role != "user" || len(m.Content) == 0 {
			continue
		}
		// Content is either a plain string or an array of content blocks.
		var s string
		if json.Unmarshal(m.Content, &s) == nil {
			return s, true
		}
		var blocks []ContentBlock
		if json.Unmarshal(m.Content, &blocks) != nil {
			return "", false
		}
		for _, b := range blocks {
			if b.Text != "" {
				return b.Text, true
			}
		}
		return "", false
	}
	return "", false
}

// FormatRequestUserPrompt renders the first request's user message verbatim —
// the text only, no header, so it pipes like --sysprompt does.
func FormatRequestUserPrompt(p *RequestPayload) string {
	text, ok := FirstUserMessage(p)
	if !ok {
		return "(no user message found in the request body)\n"
	}
	return text + "\n"
}

// FormatRequestJSON renders the full request body as indented JSON (complete,
// pipe-friendly, no surrounding prose).
func FormatRequestJSON(p *RequestPayload) string {
	if len(p.Raw) > 0 {
		var v interface{}
		if err := json.Unmarshal(p.Raw, &v); err == nil {
			if out, err := json.MarshalIndent(v, "", jsonIndent); err == nil {
				return string(out) + "\n"
			}
		}
	}
	// Fallback: marshal the parsed struct.
	out, err := json.MarshalIndent(p, "", jsonIndent)
	if err != nil {
		return "{}"
	}
	return string(out) + "\n"
}
