package main

// chat_prefetch_notes.go — the SECOND retrieval leg of the typing-time prefetch:
// the round-level [IMPORTANT] note index (internal/tools/chat_notes.go).
//
// WHY A SECOND LEG. Until this file existed the prefetch asked ONE store — the
// distilled memory ledger (prefetchLookup's "the ONE read-only retrieval") — so a
// lesson that lived in a recorded note was invisible to the retrieval that runs
// while the operator types, even though the notes are indexed, ranked and
// readable (`gopi session notes <query>` has always read them). The measured
// cost of that gap is this epic's own origin: a search of the memory ledger for
// a design decision returned nothing, and the decision was found only after a
// human said "search the important notes" — the note index held it, the prefetch
// never asked.
//
// THE THREE PROPERTIES THAT MAKE IT A LEG AND NOT A SECOND FEATURE:
//
//  1. PARALLEL, NOT REPLACING. Both legs' hits are merged into the ONE section
//     (prefetchLookupPhased), and every line says which INDEX it came from
//     (memoryprefetch's Source annotation + the note provenance cell).
//  2. NO NEW STORAGE, NO SECOND SEARCH. The table, the three tiers (fts →
//     trigram → like), the ranking and the limit semantics stay in
//     internal/tools; this file maps rows onto the renderer's hits and nothing
//     else (Rule 14, Rule 35).
//  3. IT CANNOT COST THE MEMORY LEG ANYTHING. It runs under its OWN named
//     sub-budget derived from the lookup's context, its errors are recorded in
//     the note field group instead of the lookup's outcome, and a note leg that
//     is slow, absent or broken leaves the memory leg's result exactly as it
//     would have been (Rule 15: recorded, never swallowed into "no results").

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/memoryprefetch"
	"github.com/nickhuang99/gopi/internal/tools"
)

// The note leg's own numbers, named (Rule 1).
const (
	// prefetchNotesLegBudgetMs is the note leg's share of the lookup's budget,
	// spent UNDER the lookup's own context (prefetchGoroutineTimeoutMs is still
	// the hard ceiling on one prefetch run: this is a sub-budget, never an
	// addition). It is deliberately the smaller half — the memory ledger is the
	// primary leg, and the note index is a local SQLite file with an FTS pair,
	// measured well inside this bound; a note leg that needs more than this
	// gives up, records why, and the memory leg's result is untouched.
	prefetchNotesLegBudgetMs = 400

	// prefetchNotesHitLimit is how many note rows the leg asks for. Fewer than
	// the memory leg's prefetchHitLimit on purpose: the section's slot budget
	// (memoryprefetch's MaxSegmentHits) is shared, the memory leg has priority,
	// and a note leg that floods the section would push the distilled lessons
	// out of it.
	prefetchNotesHitLimit = 3
)

// prefetchNotesOutcome* are the note leg's own outcome vocabulary — the
// `notes_outcome` field. They are SEPARATE from prefetchOutcome* on purpose
// (Rule 1, and Rule 15's "two different facts"): the lookup's own outcome
// describes what the operator's block ended up carrying, while these describe
// what the second leg did, and a reader must never have to infer one from the
// other. `absent` and `failed` are the pair that matters most — "this machine
// has no note index" and "the note index could not be read" send a reader in
// opposite directions.
const (
	// prefetchNotesOutcomeHits: the leg returned rows that were offered to the
	// section.
	prefetchNotesOutcomeHits = "hits"
	// prefetchNotesOutcomeEmpty: the index answered, and no note matched.
	prefetchNotesOutcomeEmpty = "empty"
	// prefetchNotesOutcomeAbsent: there is no note index on this machine (no
	// file, or no chat_notes table yet). Nothing to read, nothing broken.
	prefetchNotesOutcomeAbsent = "absent"
	// prefetchNotesOutcomeFailed: the index exists and could not be read
	// (a refused open, a broken index, a query error). Recorded with its reason.
	prefetchNotesOutcomeFailed = "failed"
	// prefetchNotesOutcomeGaveUp: the leg's own budget expired before it
	// answered. Distinct from `failed` for the same reason the memory leg's
	// gave-up is distinct from error.
	prefetchNotesOutcomeGaveUp = "gave-up"
	// prefetchNotesOutcomeLatched: this session already tried and could not use
	// the index (absent or unopenable), and the fact is latched so a keystroke
	// does not re-pay a failed stat-and-open on every lookup.
	prefetchNotesOutcomeLatched = "unavailable"
	// prefetchNotesOutcomeNotConsulted: no note source was handed to the lookup
	// (the one-shot shape a caller with no executor uses). It is stated rather
	// than left empty so "we did not ask" cannot be mistaken for "there was
	// nothing".
	prefetchNotesOutcomeNotConsulted = "not-consulted"
)

// prefetchNotesSource is the note leg's session-scoped state: a read handle on
// the index (the memory ledger's WARM handle when the two stores are the same
// file, its own dial otherwise) and the id of the run whose owns notes must not
// be injected.
//
// IT IS A TYPE OF ITS OWN rather than a few fields on chatPrefetch so the leg can
// be driven — and its latches measured — without an executor, a goroutine or a
// terminal (Rule 31: the surface a claim is made about is the surface the test
// calls).
type prefetchNotesSource struct {
	mu sync.Mutex
	// handle is the leg's own read-only index handle, or nil. In the ordinary
	// configuration it is NEVER built (see handleFor below): the note index and
	// the memory ledger resolve to the same file, so the leg reuses the handle
	// the memory leg already warmed.
	handle *prefetchHandle
	// tried latches the ONE dial attempt: an index that is ABSENT or unopenable
	// is not retried on every lookup.
	tried bool
	// reused records whether the leg answered through the SHARED (memory) handle.
	// It is an observable, not bookkeeping: it says which of the two shapes the
	// leg ran in, and the two shapes differ in cost.
	reused bool
	// sessionID is THIS run's own session id. Notes recorded by it are excluded
	// by the renderer (memoryprefetch.NoteHits): a hit written moments earlier by
	// the very run that is asking is text already in the reader's context, and
	// injecting it is an echo rather than a retrieval. Empty excludes nothing.
	sessionID string
}

// newPrefetchNotesSource builds the leg's state for a run. It dials nothing —
// the dial belongs to the first lookup that asks, which is the goroutine, never
// the operator's typing path.
func newPrefetchNotesSource(sessionID string) *prefetchNotesSource {
	return &prefetchNotesSource{sessionID: strings.TrimSpace(sessionID)}
}

// prefetchNotesSessionIDFromDir derives the run's session id from its session
// directory. The directory IS the id (<sessions root>/<session-uuid>, see
// agent.SessionDir), and this is the SAME id the note index stamps into every
// row's session_id column (internal/agent's storeSessionID), so the exclusion
// above compares like with like. A directory that is not an id (empty, a root, a
// relative ".") yields "" — no exclusion — rather than a guess.
func prefetchNotesSessionIDFromDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	base := filepath.Base(filepath.Clean(dir))
	switch base {
	case "", ".", string(filepath.Separator), "..":
		return ""
	}
	return base
}

// close releases the leg's handle. Safe on nil, safe twice, and a NO-OP on a
// REUSED handle: a handle the leg did not open is not the leg's to close (the
// executor closes it, in Stop).
func (s *prefetchNotesSource) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reused {
		s.handle = nil
		return
	}
	s.handle.close()
	s.handle = nil
}

// handleFor returns the read handle the note leg will query through, in ONE of
// two shapes — and the choice is a measured fact about this host, not a
// preference.
//
// THE SHARED SHAPE IS THE ORDINARY ONE. tools.ChatNoteIndexPath() resolves
// through the same memoryDBPath() the ledger does, so unless GopiChatDB or
// GopiMemoryDB splits them, the note index IS the memory ledger's file — and
// then a second dial would be a second connection to a store this session has
// already warmed, paying a dial and a pragma readback per chat to reach tables
// that sit in a file already open (Rule 35: no second dependency on a resource
// already held). So when the caller's warm handle points at the SAME path, this
// returns it and records `reused`.
//
// THE OWN DIAL IS THE FALLBACK, and it is latched. If the stores are split (a
// test, a diagnostic, an operator who pointed them apart) or the memory leg has
// no handle yet, the leg dials once per session; and an index that is ABSENT or
// unopenable is tried ONCE — a keystroke path must not pay a failed stat-and-open
// on every lookup for a fact that cannot change while the operator types. The
// latch is also why a session started before the index existed keeps reporting
// the absence instead of silently recovering mid-conversation: the record says
// which one happened.
//
// Both dials are bounded by ctx (the leg's own sub-budget), so a wedged index
// cannot hold the leg open past it.
func (s *prefetchNotesSource) handleFor(ctx context.Context, shared *prefetchHandle, path string) (*prefetchHandle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if shared != nil && s.handlesThePath(shared, path) {
		// Same file, already warm: nothing is dialled, nothing is latched, and the
		// leg answers through the executor's one connection.
		s.reused = true
		return shared, nil
	}
	s.reused = false
	if s.tried {
		if s.handle == nil {
			return nil, fmt.Errorf("the note index was unusable earlier in this session and was not retried")
		}
		return s.handle, nil
	}
	s.tried = true
	h, err := prefetchOpenHandle(ctx, path)
	if err != nil {
		return nil, err
	}
	s.handle = h
	return h, nil
}

// handlesThePath reports whether an existing handle holds exactly this file.
// The comparison is on the RECORDED path (prefetchHandle.path), never on a fresh
// resolution: two resolutions of the same environment could disagree, and the
// question here is what the open connection is actually holding.
func (s *prefetchNotesSource) handlesThePath(h *prefetchHandle, path string) bool {
	return h.usable() && strings.TrimSpace(h.path) != "" && h.path == strings.TrimSpace(path)
}

// prefetchNotesResult is what ONE note-leg lookup produced: the hits it offers to
// the section, the degradations it wants named there, and the four fields the
// record carries (Rule 15: an observable that cannot say "I could not ask" is
// not an observable).
type prefetchNotesResult struct {
	Hits         []memoryprefetch.Hit
	Degradations []memoryprefetch.Degradation
	Outcome      string
	Tier         string
	HitsCount    int
	ElapsedMs    int64
	Err          error
}

// prefetchNotesSearch runs the note leg: resolve the index, take a handle (the
// memory ledger's, when the two stores are the same file), run the SHARED tier
// ladder on a clock and a budget of its own, and map the rows onto the renderer's
// hits.
//
// EVERY EXIT IS RECORDED, and none of them touches the lookup's own outcome:
//
//	no source            → not-consulted
//	no path              → absent
//	no chat_notes table  → absent   (Rule 15: an absence is not a failure)
//	anything else, open  → failed   (+ the reason)
//	anything else, query → failed
//	budget expired       → gave-up
//
// src may be nil (a lookup with no executor) and the leg then states that
// plainly rather than reporting an empty search. shared is the memory leg's warm
// handle, or nil.
func prefetchNotesSearch(ctx context.Context, query string, cfg memoryprefetch.Config, src *prefetchNotesSource, shared *prefetchHandle) prefetchNotesResult {
	var res prefetchNotesResult
	if src == nil {
		res.Outcome = prefetchNotesOutcomeNotConsulted
		return res
	}
	started := time.Now()
	finish := func() prefetchNotesResult {
		res.ElapsedMs = time.Since(started).Milliseconds()
		return res
	}

	path := strings.TrimSpace(tools.ChatNoteIndexPath())
	if path == "" {
		res.Outcome = prefetchNotesOutcomeAbsent
		res.Err = fmt.Errorf("no note index path resolves on this machine")
		return finish()
	}

	// The leg's own bound, spent UNDER the lookup's context: the memory leg's
	// ceiling stays the hard ceiling, and this one only says how much of what is
	// left the note leg may spend. Armed before the handle is taken so a dial
	// counts too.
	notesCtx, cancel := context.WithTimeout(ctx, time.Duration(prefetchNotesLegBudgetMs)*time.Millisecond)
	defer cancel()

	db, err := src.handleFor(notesCtx, shared, path)
	if err != nil {
		switch {
		case tools.ChatNoteIndexMissing(err):
			// A missing TABLE (or a missing file, which is the same fact one
			// layer down): there is nothing to read here, and nothing is broken.
			res.Outcome = prefetchNotesOutcomeAbsent
		case notesCtx.Err() != nil && ctx.Err() == nil:
			res.Outcome = prefetchNotesOutcomeGaveUp
		case src.wasLatched():
			res.Outcome = prefetchNotesOutcomeLatched
		default:
			res.Outcome = prefetchNotesOutcomeFailed
		}
		res.Err = fmt.Errorf("open the note index %s: %w", path, err)
		return finish()
	}

	hits, tier, err := tools.ChatNoteSearchOn(notesCtx, db.usableDB(), query, prefetchNotesHitLimit)
	if err != nil {
		switch {
		case tools.ChatNoteIndexMissing(err):
			res.Outcome = prefetchNotesOutcomeAbsent
		case notesCtx.Err() != nil && ctx.Err() == nil:
			// The LEG's sub-budget expired (not the lookup's): a slow index costs
			// the second leg its answer and the memory leg nothing.
			res.Outcome = prefetchNotesOutcomeGaveUp
		default:
			res.Outcome = prefetchNotesOutcomeFailed
		}
		res.Err = fmt.Errorf("search the note index: %w", err)
		return finish()
	}
	if len(hits) == 0 {
		res.Outcome = prefetchNotesOutcomeEmpty
		return finish()
	}
	res.Tier = tier
	rendered, degs := memoryprefetch.NoteHits(prefetchNoteHits(hits), src.sessionID, cfg)
	res.Hits = rendered
	res.Degradations = degs
	res.HitsCount = len(rendered)
	if len(rendered) == 0 {
		// The index answered and every row it returned was unusable (an echo, or
		// an empty note). That is `empty` — the leg asked and has nothing to
		// offer — with the reason named in the degradations, not `absent`.
		res.Outcome = prefetchNotesOutcomeEmpty
		return finish()
	}
	res.Outcome = prefetchNotesOutcomeHits
	return finish()
}

// wasLatched reports whether this source's one dial attempt has already been
// spent and failed — read under the same mutex that owns the latch.
func (s *prefetchNotesSource) wasLatched() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tried && s.handle == nil
}

// prefetchNoteHits maps the tools layer's rows onto the renderer's data type —
// one field at a time, at the seam (the same split the memory leg's hits make).
// It is a pure mapping: every field crosses, including the ones that may be
// empty, because an empty host or path is a REAL state the store preserves.
func prefetchNoteHits(rows []tools.ChatNoteHit) []memoryprefetch.NoteHit {
	out := make([]memoryprefetch.NoteHit, 0, len(rows))
	for _, r := range rows {
		out = append(out, memoryprefetch.NoteHit{
			Record:    r.ID,
			Session:   r.SessionID,
			Round:     r.Round,
			Note:      r.Note,
			Host:      r.Host,
			Path:      r.Path,
			CreatedAt: r.CreatedAt,
		})
	}
	return out
}
