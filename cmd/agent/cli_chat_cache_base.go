package main

import (
	"fmt"
	"os"

	"github.com/nickhuang99/gopi/internal/gopilog"
)

// This file is the READ half of the status row's Σ reading on a RESUMED chat:
// it measures, ONCE, the cache counters the session had already accumulated
// before this run began, so the row's "Σ" really is the whole session instead
// of just this process.
//
// THE DEFECT IT CLOSES (measured by the operator, 2026-11-01). The row's Σ
// reading was assembled from ag.Cost.TotalCacheHitTokens / TotalCacheMissTokens
// — a RUNTIME counter that starts at zero every time a process starts. So a
// `gopi chat --resume <id>` inherited a conversation whose log already held
// hundreds of millions of prompt tokens and reported a Σ computed from two
// zeroes plus whatever this run had sent: the same session read 81.5% on the
// row and 86.03% in `gopi session stats` (335.07M hit / 54.42M miss), a 4.5
// point gap that is not a measurement difference but a SCOPE difference nobody
// printing the Σ marker could see.
//
// WHY THE BASE IS ADDED AT THE READ-OUT AND NOT INTO ag.Cost. The obvious fix —
// seed the CostTracker at resume — is WRONG, and it is worth stating because it
// looks right: the chat path writes ag.Cost's running totals onto every
// `request` row (internal/agent/chat_ops.go, LogRequestCumulative) and onto the
// run's `session_end`. internal/requestusage reads such a log by taking EACH RUN
// SEGMENT'S LAST READING and summing the segments, so a segment whose counters
// started at the previous segments' total would be counted twice — the exact
// double-counting that package exists to remove (its header measures 48.6x on a
// real session). The base is therefore a DISPLAY quantity, exactly like
// chatSession.turnBaseline: it rides the read-out, never the counters.
//
// WHY THE SUMS COME FROM internal/gopilog. The quantity the row must agree with
// is the one `gopi session stats` and `gopi session log`'s COST block print, and
// those print gopilog's own two sums (sumPromptCacheHit / sumPromptCacheMiss,
// now exported beside their definitions). A second addition written here would
// be a second definition of "the session's cache hit count", free to drift from
// the one the acceptance criterion compares against (Rule 14/43).

// chatCostBase is the cache pair a session had ALREADY accumulated when this run
// started — the base the status row's Σ reading adds to this run's counters.
//
// known distinguishes the two zeroes (Rule 15a): a session with no history and a
// session whose history could not be measured both add 0, but only the first is
// a measurement. An unknown base leaves Σ reading exactly as it was before this
// fix (this run only) AND says so on stderr — a named degradation, never a
// silent fallback.
type chatCostBase struct {
	cacheRead  int
	cacheWrite int
	known      bool
}

// chatResumeCacheBaseNoticeFormat states the base the row's Σ now includes, so
// an operator comparing the row against `gopi session stats` can see why the two
// agree: the row is no longer reporting this run alone.
const chatResumeCacheBaseNoticeFormat = "[gopi] chat: the %s cache reading includes what this session already carried: %s hit / %s miss token(s)\n"

// chatResumeCacheBaseUnreadableFormat reports a session record this run could not
// fold. It is NOT fatal — the conversation resumes from the checkpoint either way
// — but the consequence is that the row's Σ falls back to this run alone, which
// is exactly the misreading this fix removes, so the run must say so (Rule 15).
const chatResumeCacheBaseUnreadableFormat = "[gopi] chat: cannot read the cache usage this session already carried from %s (%v) — the status row's Σ reading covers THIS RUN only, so it will read lower than `gopi session stats`\n"

// chatResumeCacheBase measures the cache pair the session this run continues has
// already recorded, by parsing its session record ONCE.
//
// The seed/rawLogPath pair is the SAME one the turn baseline is resolved from
// (chatSessionFileFor), so the record whose turns are counted and the record
// whose cache usage is folded are one answer to one question — a second
// resolution could pick a different layout.
//
// The fold is gopilog's, not this file's: ParseFile classifies every `request`
// row's quantity (internal/requestusage) and derives each row's OWN usage, so
// summing the iterations is the session's counted total — the same number the
// COST blocks print, and it excludes a round that carried no content on both
// sides by construction.
//
// ok=false is a run with no session record to read (the operator resumed a bare
// raw log): there is no history to fold, which is a fresh numbering, not a
// failure. An error means the record EXISTS but could not be read.
func chatResumeCacheBase(seed, rawLogPath string) (chatCostBase, error) {
	path, ok, err := chatSessionFileFor(seed, rawLogPath)
	if err != nil {
		return chatCostBase{}, err
	}
	if !ok {
		return chatCostBase{}, nil
	}
	sess, perr := gopilog.ParseFile(path)
	if perr != nil {
		return chatCostBase{}, fmt.Errorf("parse %s: %w", path, perr)
	}
	return chatCostBase{
		cacheRead:  gopilog.SessionPromptCacheHit(sess),
		cacheWrite: gopilog.SessionPromptCacheMiss(sess),
		known:      true,
	}, nil
}

// reportChatResumeCacheBase prints the base this run folded, or the reason there
// is none. It is called where the base is resolved (the resume block), so the
// announcement and the number it describes are written by the same branch.
func reportChatResumeCacheBase(base chatCostBase, err error, record string) {
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, chatResumeCacheBaseUnreadableFormat, record, err)
	case !base.known:
		// No record to read (a bare raw log): nothing was carried, so there is
		// nothing to announce. The row's Σ is this run, which is TRUE here.
	case base.cacheRead > 0 || base.cacheWrite > 0:
		fmt.Fprintf(os.Stderr, chatResumeCacheBaseNoticeFormat,
			statusHitRateCumulativeGlyph,
			formatCountBounded(base.cacheRead), formatCountBounded(base.cacheWrite))
	}
}
