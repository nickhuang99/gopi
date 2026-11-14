package main

import (
	"fmt"
	"os"

	"github.com/nickhuang99/gopi/internal/gopilog"
)

// Round-scoped views of `gopi session log`: the positional <round> and --rounds.
//
// This file owns NO grammar and NO numbering rule of its own: the selector is
// parsed by the shared gopilog.ParseHTTPSelector (the same `all|last|N` grammar
// `gopi session raw <round>` takes), a session's rounds and their raw-log counterparts
// come from internal/gopilog's sessionround.go (SessionRoundIndex /
// SessionRoundRawOrdinals — the pairing that absorbs the extra raw rounds the
// session JSONL never numbered), and the BYTE views are `gopi session raw`'s own
// printer (printRawRoundAt), so `gopi session log <id> <round> request` and
// `gopi session raw <id> <n> request` print the same bytes from the same resolver.

// rawRoundsForSession pairs a session file with its raw HTTP log and parses the
// rounds. The pairing is gopilog's own resolver (never a second answer to
// "which raw log belongs to this session?"), and the missing/gone cases are
// reported by the same helpers `gopi session raw` uses.
func rawRoundsForSession(sessionPath string) (string, []gopilog.HTTPRound, error) {
	rawLog, err := gopilog.ResolveRawLogForSession(sessionPath)
	if err != nil {
		return "", nil, err
	}
	rounds, err := gopilog.ParseHTTPRounds(rawLog)
	if err != nil {
		return rawLog, nil, err
	}
	return rawLog, rounds, nil
}

// renderSessionRound renders the round a `gopi session log <id> <round>` invocation
// names: the round's content from the session JSONL, or — with --request /
// --response — the round's request/response bytes from the raw HTTP log.
func renderSessionRound(sessionPath string, opts *logOptions) {
	s, err := gopilog.ParseFile(sessionPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", sessionPath, err)
		os.Exit(1)
	}
	n := gopilog.SessionRounds(s)
	// The selector resolves to a CANONICAL round ordinal (`request` row); the
	// content for that round is a SEPARATE question, because a round that
	// carried no content still has an ordinal and must not be answered with a
	// neighbour's body.
	ord, ok := gopilog.SessionCanonicalOrdinal(s, opts.roundSel)
	if !ok {
		fmt.Fprintf(os.Stderr,
			"gopi session log: round %d does not exist — session %s has %d round(s) (%s; numbering is 1-based and counts the session's API calls, first round = 1; --%s lists them)\n",
			opts.roundSel.Round, s.ID, n, gopilog.SessionRoundUnitWords(), logFlagTurns)
		os.Exit(1)
	}
	round, _ := gopilog.SessionCanonicalRound(s, ord)
	if !round.HasContent() {
		// A real round with no body: a byte view has nothing to print for it, and
		// the raw side is reached through the pairing map, which also has no
		// entry. Say which of the two facts this is (Rule 15).
		fmt.Fprintf(os.Stderr,
			"gopi session log: round %d of session %s recorded no content (the reader holds no blocks for it), so there is nothing to print here; --%s lists the rounds\n",
			ord, s.ID, logFlagTurns)
		os.Exit(1)
	}
	idx := round.Iter

	if opts.request || opts.response {
		printSessionRoundBytes(s, sessionPath, ord, opts)
		return
	}
	// The header CROSS-REFERENCES the raw log's own round count, so the pairing
	// is resolved here (best effort): a session whose raw log cannot be paired
	// still renders, with that third count simply absent — never a fabricated 0.
	rawTotal := 0
	if _, rounds, err := rawRoundsForSession(sessionPath); err == nil {
		rawTotal = len(rounds)
	}
	fmt.Print(gopilog.FormatSessionRoundWithRaw(s, idx, ord, rawTotal))
}

// printSessionRoundBytes prints one round's request or response VERBATIM, by
// pairing the CANONICAL round with the raw-log round internal/gopilog's pairing
// table resolved for it and handing the ordinal to `gopi session raw`'s printer.
// The raw ordinal actually used is named on stderr, so a byte view never hides
// which raw round it read.
func printSessionRoundBytes(s *gopilog.Session, sessionPath string, ord int, opts *logOptions) {
	_, rounds, err := rawRoundsForSession(sessionPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session log: no raw HTTP log for session %s: %v\n", s.ID, err)
		os.Exit(1)
	}
	if len(rounds) == 0 {
		fmt.Fprintf(os.Stderr, "gopi session log: the raw log for session %s has no request rounds\n", s.ID)
		os.Exit(1)
	}
	ords := gopilog.SessionRoundRawOrdinals(s, rounds)
	// The ordinal is 1-based, so the range check is on BOTH ends: a caller that
	// passes 0 (or a negative) must be refused, not index -1 (a panic is not a
	// refusal — Rule 15).
	idx := ord - 1
	if ord < 1 || idx >= len(ords) || ords[idx].Index < 0 {
		fmt.Fprintf(os.Stderr, "gopi session log: round %d has no paired raw-log round (the raw log holds %d raw rounds; --%s shows the pairing)\n",
			ord, len(rounds), logFlagTurns)
		os.Exit(1)
	}
	part := rawPartRequest
	if !opts.request {
		part = rawPartResponse
	}
	fmt.Fprintf(os.Stderr, "[gopi session log] session round %d is raw round %d of %d — %s bytes below (stdout carries the bytes only)\n",
		ord, ords[idx].Raw, len(rounds), part)

	// printRawRoundAt prints BYTES: the request body verbatim, or the raw-log
	// line span of the response side. stdout must stay a byte stream, so the
	// provenance line above goes to stderr.
	printRawRoundAt(resolveRawLogOrExit(s, sessionPath), s.ID,
		gopilog.HTTPRoundSelector{Detail: true, Round: ords[idx].Raw}, part)
}

// resolveRawLogOrExit resolves a session's raw log for the byte path, exiting
// with the shared diagnostics (a session that RECORDED a log whose file is gone
// is a different answer from one that never had a log).
func resolveRawLogOrExit(s *gopilog.Session, sessionPath string) string {
	if notice := missingRawLogNotice(s.ID, sessionPath); notice != "" {
		fmt.Fprint(os.Stderr, notice)
		os.Exit(1)
	}
	rawLog, err := gopilog.ResolveRawLogForSession(sessionPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session log: %v\n", err)
		os.Exit(1)
	}
	return rawLog
}

// printSessionTurns renders the session's TURN table
// (`gopi session log <id> --turns`): one row per TURN — the operator's own row
// to the next — with the number of ROUNDS that turn took.
//
// The table and the count come from ONE producer (gopilog's turn boundary
// slice, via gopilog.FormatSessionTurnTable), so this table and the round
// count can never be confused: a turn is a person's ask, a round is one HTTP
// request/response, and one turn routinely spans many rounds (Rule 14).
func printSessionTurns(sessionPath string) {
	s, err := gopilog.ParseFile(sessionPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", sessionPath, err)
		os.Exit(1)
	}
	fmt.Print(gopilog.FormatSessionTurnTable(s))
}

// printSessionRounds renders the session ROUND table (`gopi session log
// <id> --rounds`): each round's number, the raw-log ordinal it pairs with, its
// token usage and the raw-log line span — the map an operator needs before
// naming a round. A session with no raw log still gets its own rounds (the raw
// columns dashed) with the reason on stderr, because "no raw log" is not "no
// rounds".
func printSessionRounds(sessionPath string) {
	s, err := gopilog.ParseFile(sessionPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", sessionPath, err)
		os.Exit(1)
	}
	_, rounds, err := rawRoundsForSession(sessionPath)
	if err != nil || len(rounds) == 0 {
		fmt.Print(gopilog.FormatSessionRoundTable(s, nil, nil))
		reason := "the session recorded no raw HTTP log"
		if err != nil {
			reason = fmt.Sprintf("no raw HTTP log could be paired (%v)", err)
		}
		fmt.Fprintf(os.Stderr,
			"[gopi session log] %s — the raw-ordinal and byte-span columns are '-' (the session's own round numbering is unaffected)\n", reason)
		return
	}
	fmt.Print(gopilog.FormatSessionRoundTable(s, gopilog.SessionRoundRawOrdinals(s, rounds), rounds))
}
