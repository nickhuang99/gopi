package gopilog

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Library surface for the `gopi session raw` verb: verbatim byte views over a raw
// HTTP log. `raw` prints BYTES — no banners, no decorations — so stdout stays
// pipeable (| jq / > file / | head). The only non-byte output is the --rounds
// index, which is a table by definition.

// RawLogLineSpan returns raw-log lines [first, last] (1-based, inclusive)
// verbatim — markers and payloads exactly as the raw HTTP logger wrote them,
// modulo the per-line newline stripped by the scanner and re-added here.
// total is capped at maxRawViewBytes; beyond the cap the tail is cut on a
// rune boundary and truncated=true is returned (the caller may surface that
// on stderr, never on stdout — stdout stays pure bytes).
func RawLogLineSpan(path string, first, last int) (lines []string, truncated bool, err error) {
	if last < first {
		return nil, false, fmt.Errorf("empty line span %d..%d", first, last)
	}
	all, err := readRawLogLineSpan(path, first, last)
	if err != nil {
		return nil, false, err
	}
	total := 0
	for i, l := range all {
		need := len(l) + 1 // the re-added newline
		if total+need > maxRawViewBytes {
			// This line does not fit whole. Emit what fits — a rune-safe
			// prefix of the line when part of it still fits, nothing when the
			// cap was reached exactly at its start — and report the cut.
			if budget := maxRawViewBytes - total; budget > 1 {
				head, _ := capRuneSafe(l, budget-1)
				return append(all[:i:i], head), true, nil
			}
			return all[:i:i], true, nil
		}
		total += need
	}
	return all, false, nil
}

// RawRoundRequest returns one round's request body verbatim (the exact bytes
// logged after the round's → REQUEST marker). ok is false when the round
// logged no request body.
func RawRoundRequest(r *HTTPRound) (body string, ok bool) {
	if len(r.RequestRaw) == 0 {
		return "", false
	}
	return string(r.RequestRaw), true
}

// RawRoundResponseSpan is the line span of one round's response side: the
// first ← CHUNK / ← RESPONSE / HTTP-ERROR through the round's last line. ok is
// false when the round logged no response markers (nothing verbatim to print).
func RawRoundResponseSpan(r *HTTPRound) (first, last int, ok bool) {
	if r.RespLineStart == 0 || r.RespLineStart > r.LineEnd {
		return 0, 0, false
	}
	return r.RespLineStart, r.LineEnd, true
}

// RawViewCapBytes is the exported form of the raw view cap (maxRawViewBytes):
// every verbatim raw view is capped at this many bytes, rune-safe.
const RawViewCapBytes = maxRawViewBytes

// FormatRawRoundIndex renders the --rounds index: each round's ordinal, its
// raw-log line span, its byte size and its kind (the map a user needs to turn
// a round number into file offsets).
func FormatRawRoundIndex(rounds []HTTPRound, rawLogName string) string {
	var sb strings.Builder
	sb.WriteString(headerSep + "\n")
	sb.WriteString("RAW ROUNDS (raw log: " + rawLogName + ")\n")
	sb.WriteString(headerSep + "\n")
	if len(rounds) == 0 {
		sb.WriteString("(no request rounds found in the raw HTTP log)\n")
		return sb.String()
	}
	sb.WriteString(fmt.Sprintf("%6s %10s %10s %10s  %s\n", "RND", "START", "END", "BYTES", "KIND"))
	for i := range rounds {
		r := &rounds[i]
		kind := roundKind(r)
		sb.WriteString(fmt.Sprintf("%6d %10d %10d %10s  %s\n",
			r.Number, r.LineStart, r.LineEnd, humanBytes(roundBytes(r)), kind))
	}
	return sb.String()
}

// roundKind describes what a round recorded: the annotation for a probe
// round, the response kind otherwise (stream / response / http-error / none).
func roundKind(r *HTTPRound) string {
	if r.Annotation != "" {
		return "probe · " + r.Annotation
	}
	switch r.Response.Kind {
	case "", "none":
		return "request, no response"
	case "stream":
		return fmt.Sprintf("request + stream (%d chunks)", r.Response.Chunks)
	case "response":
		return "request + response"
	case "http-error":
		return "request + HTTP-ERROR"
	}
	return r.Response.Kind
}

// roundBytes is a round's total payload size (request body + response side).
func roundBytes(r *HTTPRound) int {
	n := len(r.RequestRaw)
	n += r.Response.Bytes
	return n
}

// capRuneSafe trims s to at most max bytes without splitting a UTF-8 rune at
// the cut. Returns the (possibly shortened) string and whether it was cut.
func capRuneSafe(s string, max int) (string, bool) {
	if max <= 0 || len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}
