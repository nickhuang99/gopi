// source.go — WHERE a rendered request comes from: one raw HTTP log.
//
// Two readers, one answer. `--last` uses checkpoint.LastRequest, which scans
// backwards from the file's TAIL and therefore costs a bounded window of a
// 231 MB log rather than the log. A NUMBERED selector cannot use that door ("the
// last one" is the only question it answers), so it takes the FORWARD one:
// rawlogfmt's record reader decodes record by record and the walk stops at the
// Nth request, discarding every payload it does not need.
//
// THE AXIS HERE IS THE ROUND, ALWAYS. What this file walks is one record per
// HTTP request/response, so every number in it addresses a ROUND; the word
// "turn" belongs to the session's boundary algorithm (address.go, gopilog) and
// does not appear in this file except when stating that it is a different thing.
//
// WHY FORWARD AND NOT AN INDEX: an index would be a second artifact beside the
// log, written by nobody, stale the moment the next round lands, and owned by no
// reclaim path (Rule 35). The forward walk costs O(bytes before round N) with
// BOUNDED memory — it never holds more than one record — and it is the same
// decoder the rest of the system reads this file with (Rule 14: no second
// parser).
//
// The one thing this file does NOT do is guess. A round the log does not carry is
// refused by name with the count that would have worked; a log whose framing is
// broken is refused with the decoder's own error. Neither is answered with a
// neighbour, because "the context of round 57" handed out as round 58's is worse
// than a refusal (取错段比取不到更糟 — the same rule /screen states).
package ctxdump

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/nickhuang99/gopi/internal/checkpoint"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/rawlogfmt"
)

// ErrNoRequests is returned when the log carries no request record at all.
// It is a distinct fact from "there is no ROUND N" and gets its own sentence.
var ErrNoRequests = errors.New("the raw log carries no request record")

// ErrTurnUnmeasurable is returned when the request was read but the LOG's
// request count could not be measured, so the round holding it has no number.
// (The NAME keeps "Turn" because callers match on it; the fact it reports is
// about the ROUND axis — see the message, which says `--round N`.)
//
// IT REFUSES RATHER THAN NAMING THE FILE "0". The artifact's name states WHICH
// ROUND it holds, and a claim that cannot be backed is the defect `/screen` was
// fixed for (`screen-turn58` holding turn 59's screen). A refusal the operator
// can act on (`--round N` names its own number) beats a file whose name lies.
var ErrTurnUnmeasurable = errors.New("cannot name the exported context: the log's request count could not be measured (pass --round N to name the round yourself)")

// FromRawLog returns the request at one ROUND of a raw HTTP log, plus that
// round's ordinal.
//
// A ROUND, NOT A TURN — this is the whole distinction the export's address now
// carries (address.go). A raw log holds one record per HTTP request/response, so
// its sequence numbers address ROUNDS; the word "turn" belongs to the session's
// own boundary algorithm and lives in `RequestAtTurn`. The parameter WAS spelled
// `turn` when this file conflated the two, and that confusion is the defect
// `context-turn336.md` on a 2-turn session came from. It is named `round` now:
// keeping the old spelling "so the diff stays small" left every future reader of
// this signature being taught the conflation, which is the thing being removed.
//
// round <= 0 means "the last one" and goes through checkpoint.LastRequest (the
// tail scan). round > 0 counts records forward from the head.
//
// The second return value is the ROUND that was ACTUALLY selected, and it is the
// number a ROUND-scoped artifact's name carries: `--last` on a log holding 122
// requests writes context-round122.md, never context-round0.md, because a file
// named for a number that is not what it holds is the defect the /screen
// artifact was fixed for.
func FromRawLog(path string, round int) (*deepseek.ClaudeRequest, int, error) {
	if path == "" {
		return nil, 0, fmt.Errorf("ctxdump: no raw log path")
	}
	if round <= 0 {
		_, req, err := checkpoint.LastRequest(path)
		if err != nil {
			return nil, 0, err
		}
		// THE RETURNED NUMBER MUST BE THE REQUEST'S REAL ORDINAL, because the
		// artifact's NAME carries it and a name is a claim about the bytes beside
		// it (the /screen artifact was fixed for exactly this).
		//
		// checkpoint.CountRequests is a HEAD SAMPLE with a 16 MB budget, so on a
		// log past that budget its answer is a LOWER BOUND and it says so
		// (truncated=true). Measured on this host: a 231 MB log reported 46 while
		// holding 696 requests, and the export named that page for round 46 while
		// holding round 696's context. When the sample is truncated, the exact
		// count is taken the cheap way — a header walk.
		total, truncated, cerr := checkpoint.CountRequests(path)
		if cerr != nil || truncated || total <= 0 {
			exact, eerr := countRequestsExact(path)
			if eerr != nil {
				// The request itself parsed, so this is a MEASUREMENT that failed,
				// not an absent request. Answer 0 — "the round is not known" — which
				// callers must render as unknown rather than as round 0.
				return req, 0, nil
			}
			total = exact
		}
		return req, total, nil
	}
	return requestAt(path, round)
}

// countRequestsExact counts the request records of a raw log by walking its
// headers and SKIPPING every payload.
//
// It exists because the bounded head sample cannot answer "how many requests
// does this file hold" past its budget, and the answer is needed to NAME the
// artifact. The walk is sequential and payload-free (rawlogfmt.Reader.NextHeader
// + Skip), so the cost is one pass over the file's bytes and never its memory.
func countRequestsExact(path string) (int, error) {
	format, err := rawlogfmt.Detect(path)
	if err != nil {
		return 0, err
	}
	r, err := rawlogfmt.OpenReader(path, format)
	if err != nil {
		return 0, err
	}
	defer r.Close()
	count := 0
	for {
		h, herr := r.NextHeader()
		if herr != nil {
			if errors.Is(herr, io.EOF) {
				return count, nil
			}
			return 0, herr
		}
		// TagRequest ONLY, so the total this returns and the walk in `requestAt`
		// index the SAME sequence — otherwise "holds N request(s)" names a range
		// the addressing cannot reach.
		if h.Tag == rawlogfmt.TagRequest {
			count++
		}
		if serr := r.Skip(); serr != nil {
			if errors.Is(serr, io.EOF) {
				return count, nil
			}
			return 0, serr
		}
	}
}

// requestAt walks the log forward to the round-th request record.
//
// THE PARAMETER IS A ROUND, and the refusal says so: this walk counts HTTP
// request/response records one at a time, which is the ROUND axis. It used to
// be spelled `turn` and its refusal said "turn 999 is not among them" for a
// selector that had just been named a round — the epic's own defect (one word,
// two axes) surviving in the one place a reader is most likely to meet it, the
// message that tells them their number is out of range.
func requestAt(path string, round int) (*deepseek.ClaudeRequest, int, error) {
	format, err := rawlogfmt.Detect(path)
	if err != nil {
		return nil, 0, fmt.Errorf("ctxdump: %s: %w", path, err)
	}
	r, err := rawlogfmt.OpenReader(path, format)
	if err != nil {
		return nil, 0, err
	}
	defer r.Close()
	seen := 0
	for {
		rec, rerr := r.Next()
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break
			}
			return nil, 0, fmt.Errorf("ctxdump: %s: %w", path, rerr)
		}
		// TagRequest ONLY — the ordinal handed to this walk is a SESSION ordinal
		// (a turn's RequestOrdinal, which numbers the session.jsonl `request`
		// rows, one per real provider call). A probe record IS a real send but it
		// writes no `request` row, so counting it here shifted every later
		// ordinal by one and the export rendered another turn's body — measured
		// on session 6b01dd46: record 1 is a `P`, so session ordinal 1 resolved
		// to raw record 2 and `--turn 1` died parsing a multipart probe body.
		// Pairing against gopilog.SessionRoundRawOrdinals confirms the identity:
		// session ordinals 1..8 pair to TagRequest-only ordinals 1..8, exact.
		if rec.Tag != rawlogfmt.TagRequest {
			continue
		}
		seen++
		if seen < round {
			// The payload is already decoded by Next; dropping the reference is
			// what keeps the walk bounded (one record in memory at a time).
			continue
		}
		var req deepseek.ClaudeRequest
		if uerr := json.Unmarshal(rec.Payload, &req); uerr != nil {
			return nil, 0, fmt.Errorf("ctxdump: %s: parse request %d: %w", path, round, uerr)
		}
		return &req, round, nil
	}
	if seen == 0 {
		return nil, 0, fmt.Errorf("ctxdump: %s: %w", path, ErrNoRequests)
	}
	return nil, 0, fmt.Errorf("ctxdump: %s holds %d request(s), so there is no ROUND %d (a round is one HTTP request/response — NOT a turn; for a turn, drop --round)", path, seen, round)
}

// CountRequests is the number of request records a log holds, from
// checkpoint's own bounded head sample. It is exposed so a caller can print the
// RANGE a refusal should name without opening the file twice.
func CountRequests(path string) (int, bool, error) {
	return checkpoint.CountRequests(path)
}
