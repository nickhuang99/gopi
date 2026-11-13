// doc-class: normative
package tools

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// keywordsTailConvergeSelect reads the two columns a convergence decision
// needs — the answer (the ONLY column this migration writes) and search_kw, the
// column the writer normally derives the tail from. It is ordered by id so a
// run's log is reproducible, and it carries no predicate: the affected set is
// decided in Go by the SAME rule the writer uses, never by a second SQL spelling
// of "has a duplicated tail" (Rule 13).
const keywordsTailConvergeSelect = "SELECT id, answer, COALESCE(search_kw, '') FROM distilled_qa ORDER BY id"

// keywordsTailConvergeUpdate rewrites ONE row's answer and nothing else. A
// migration that touched tier, confidence, counters or timestamps would be a
// data rewrite wearing an index repair's clothes (see BackfillJiebaTokens for
// the same contract on the other column).
const keywordsTailConvergeUpdate = "UPDATE distilled_qa SET answer = ? WHERE id = ?"

// keywordsTailHistogramSelect is the ONE instrument that produces a tail-block
// histogram. `before` and `after` MUST come through it: a migration that measured
// its own effect with a different query than the one that found the defect could
// report a convergence it did not perform (Rule 38 — a figure names its
// generator).
const keywordsTailHistogramSelect = "SELECT answer FROM distilled_qa"

// blankLine is the ONE blank-line spelling this file's structural test asks
// about (a tail segment opens with it, and so does every separator). Named
// rather than inlined (Rule 1).
const blankLine = "\n\n"

// keywordsTailSeparator is the two-newline run that joins an answer's body to
// its tail block. It is built from keywordsTailMarker's own spelling rather than
// retyped, because the marker IS the tail's opening ("keywords: " preceded by a
// blank line); a second literal here is how the writer and this reader would
// drift apart silently (Rules 1b/13/14).
func keywordsTailSeparator() string { return "\n\n" + keywordsTailMarker }

// KeywordsTailBody returns the answer with EVERY trailing keywords block
// stripped — the "body" a convergence must leave byte-identical.
//
// It is exactly stripKeywordsTail: keywordsTailRe is `(?s)\n\nkeywords: .*$`,
// greedy to the end of the string, so ONE call eats every tail block. That
// greediness is the whole reason the convergence is "strip all, then re-attach
// one block" rather than "remove one duplicated block".
//
// hasTail reports whether the row carries a separator at all. The body is
// therefore always the text BEFORE the first separator, which is what makes the
// byte-identity verification expressible: body(before) == body(after) for every
// affected row.
func KeywordsTailBody(answer string) (body string, hasTail bool) {
	if !strings.Contains(answer, keywordsTailSeparator()) {
		return answer, false
	}
	return stripKeywordsTail(answer), true
}

// KeywordsTailBlocks splits an answer into its body and its tail blocks (marker
// stripped). ok is false when the answer carries no tail at all.
//
// interleaved is TRUE when a blank-line-delimited segment after the body does not
// open with keywordsTailMarker — i.e. real text (measured: a "SOURCE: …" or
// "lesson_type: …" provenance line) sits AFTER a tail block. That is reported
// rather than silently normalised: for those rows the text between the first and
// the last block is dropped by the convergence, and the promise still held is
// only that the BODY is unchanged.
//
// The segments are split on blankLine, never on a bare newline: a keywords block
// may legitimately wrap one line (measured, id 7770 — its search_kw column
// carries the same single newline), and counting that as body would misreport an
// ordinary duplicated tail as an interleaved one.
func KeywordsTailBlocks(answer string) (body string, blocks []string, interleaved bool, ok bool) {
	body, hasTail := KeywordsTailBody(answer)
	if !hasTail {
		return answer, nil, false, false
	}
	// Split from the BODY boundary: blankLine IS the separator (the marker's own
	// two-newline prefix), so the first piece is empty, the second starts with
	// the marker, and every later piece is a segment that claims to be a tail —
	// exactly the claim the interleaved test checks.
	segs := strings.Split(answer[len(body):], blankLine)[1:]
	for i, seg := range segs {
		maybe := seg
		if i > 0 && !strings.HasPrefix(seg, keywordsTailMarker) {
			interleaved = true
		}
		blocks = append(blocks, strings.TrimPrefix(maybe, keywordsTailMarker))
	}
	return body, blocks, interleaved, true
}

// keywordsTailBlockCount counts the tail blocks an answer carries. Every
// measurement of this migration — the histogram before, the histogram after, the
// affected set — goes through this ONE function.
func keywordsTailBlockCount(answer string) int {
	return strings.Count(answer, keywordsTailSeparator())
}

// KeywordsTailHistogram maps "how many trailing keywords blocks does this answer
// carry" to "how many rows" — the shape the migration is about.
func KeywordsTailHistogram(ctx context.Context, db *sql.DB) (map[int]int, error) {
	if db == nil {
		return nil, fmt.Errorf("keywords tail histogram: no ledger handle")
	}
	rows, err := db.QueryContext(ctx, keywordsTailHistogramSelect)
	if err != nil {
		return nil, fmt.Errorf("keywords tail histogram: %w", err)
	}
	defer rows.Close() //stylegate:ignore read-only cursor closed after iteration
	hist := make(map[int]int)
	for rows.Next() {
		var answer string
		if err := rows.Scan(&answer); err != nil {
			return nil, fmt.Errorf("keywords tail histogram: %w", err)
		}
		hist[keywordsTailBlockCount(answer)]++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("keywords tail histogram: %w", err)
	}
	return hist, nil
}

// KeywordsTailConvergeReport is what one convergence run measured. It is
// returned rather than printed so the CLI renders it and a test asserts on it.
type KeywordsTailConvergeReport struct {
	Scanned  int // rows read
	Affected int // rows carrying 2+ tail blocks
	Changed  int // rows rewritten (the answer actually moved)
	Skipped  int // affected rows whose convergence is a no-op
	// FirstDiffers counts affected rows whose FIRST block differs from the
	// block kept (the last) — the rows where the choice of block changes text.
	// They are counted and reported, never selected silently: the briefing
	// requires it, and the number makes the identical-vs-divergent split
	// visible in every run's own output.
	FirstDiffers int
	// LastMatchesSearchKW counts affected rows whose kept block equals the
	// row's own search_kw column — the evidence for WHICH block is current.
	LastMatchesSearchKW int
	// InterleavedRows counts affected rows whose blocks are separated by text
	// that is not a keywords block (a provenance line, or prose that merely
	// LOOKS like a tail). Those rows are REFUSED: collapsing them would delete
	// text, so they are left exactly as stored and counted here.
	InterleavedRows int
	// Preserved counts the rows left untouched for that reason — the honest
	// "not done" figure a report must carry.
	Preserved int
	Before    map[int]int // histogram before the write
	After     map[int]int // histogram after the write (nil on a dry run)
	DryRun    bool
}

// KeywordsTailConverge collapses every row's consecutive trailing keywords
// blocks into ONE, keeping the LAST block and leaving the body byte-identical.
//
// WHY THE LAST BLOCK: measured on the prod ledger (2026-10-22, 6854 affected
// rows) 6853 carry a last block whose text equals the row's own search_kw column
// (the one exception, id 7854, has an EMPTY search_kw). The first block is the
// OLD spelling and the last is the CURRENT one — the duplicated tail is a
// leftover of successive keyword updates, each appending its block after the
// previous one was already there. Keeping the first would resurrect the stale
// breadcrumbs those updates exist to replace. 6491 rows have every block
// identical (the choice is immaterial there); 363 diverged, and this rule
// resolves all of them the same way.
//
// The whole write is ONE transaction: a half-applied migration leaves the ledger
// in a shape neither the before nor the after histogram describes, and an
// append-only knowledge base has no other record of what it looked like. A row is
// written only when its converged text actually differs, so re-running is a no-op
// (idempotent, like BackfillJiebaTokens).
func KeywordsTailConverge(ctx context.Context, db *sql.DB, dry bool) (KeywordsTailConvergeReport, error) {
	rep := KeywordsTailConvergeReport{DryRun: dry}
	if db == nil {
		return rep, fmt.Errorf("keywords tail converge: no ledger handle")
	}
	before, err := KeywordsTailHistogram(ctx, db)
	if err != nil {
		return rep, err
	}
	rep.Before = before

	writes, err := collectKeywordsTailWrites(ctx, db, &rep)
	if err != nil {
		return rep, err
	}
	rep.Changed = len(writes)
	if dry {
		return rep, nil
	}
	if len(writes) > 0 {
		if err := applyKeywordsTailWrites(ctx, db, writes); err != nil {
			return rep, err
		}
	}
	after, err := KeywordsTailHistogram(ctx, db)
	if err != nil {
		return rep, err
	}
	rep.After = after
	return rep, nil
}

// keywordsTailWrite is one row's pending rewrite: the FULL new answer, so the
// write half cannot recompute it differently from the half that decided it.
type keywordsTailWrite struct {
	id     int64
	answer string
}

// collectKeywordsTailWrites is the read half: it decides EVERY row's fate before
// a single byte is written, so a refusal anywhere in the scan cannot leave a
// partly converged ledger behind.
func collectKeywordsTailWrites(ctx context.Context, db *sql.DB, rep *KeywordsTailConvergeReport) ([]keywordsTailWrite, error) {
	rows, err := db.QueryContext(ctx, keywordsTailConvergeSelect)
	if err != nil {
		return nil, fmt.Errorf("keywords tail converge: %w", err)
	}
	defer rows.Close() //stylegate:ignore read-only cursor closed after iteration
	var writes []keywordsTailWrite
	for rows.Next() {
		var (
			id       int64
			answer   string
			searchKW string
		)
		if err := rows.Scan(&id, &answer, &searchKW); err != nil {
			return nil, fmt.Errorf("keywords tail converge: %w", err)
		}
		rep.Scanned++
		body, blocks, interleaved, ok := KeywordsTailBlocks(answer)
		if !ok || len(blocks) < 2 {
			continue
		}
		rep.Affected++
		kept := blocks[len(blocks)-1]
		if blocks[0] != kept {
			rep.FirstDiffers++
		}
		if searchKW != "" && kept == searchKW {
			rep.LastMatchesSearchKW++
		}
		if interleaved {
			// Real text (not a "keywords: …" block) sits between two
			// separators: the row is NOT a plain body-plus-tail shape, and
			// collapsing it would delete prose. REFUSE the row — leave it
			// exactly as stored and report the count, because the migration's
			// whole promise is that prose is untouched and a silent cut is the
			// one failure it must not have (Rule 15).
			rep.InterleavedRows++
			rep.Preserved++
			continue
		}
		// keywordsTailSeparator() IS "\n\n" + marker, so the kept block is
		// appended bare — adding the marker again would write "keywords: keywords:".
		converged := body + keywordsTailSeparator() + kept
		if converged == answer {
			rep.Skipped++
			continue
		}
		writes = append(writes, keywordsTailWrite{id: id, answer: converged})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("keywords tail converge: %w", err)
	}
	return writes, nil
}

// applyKeywordsTailWrites is the write half: one transaction, answer column only.
func applyKeywordsTailWrites(ctx context.Context, db *sql.DB, writes []keywordsTailWrite) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("keywords tail converge: begin: %w", err)
	}
	for _, w := range writes {
		if _, err := tx.ExecContext(ctx, keywordsTailConvergeUpdate, w.answer, w.id); err != nil {
			tx.Rollback() //stylegate:ignore the caller's error is the failure being reported
			return fmt.Errorf("keywords tail converge: update id %d: %w", w.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("keywords tail converge: commit: %w", err)
	}
	return nil
}
