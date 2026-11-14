package main

// chat_prefetch_turn.go — the TURN-BOUNDARY half of memory prefetch: ONE
// retrieval per sealed turn, driven by the turn's WHOLE input rather than by the
// line editor's in-progress buffer.
//
// THE BLIND SPOT THIS FILE CLOSES, stated the way it was measured. The typing
// path (chat_prefetch.go's loop + internal/memoryprefetch's Session) samples
// `Editor.Value().Text()` — "the line the operator is typing right now". Four
// input classes have NO such line, so nothing was ever retrieved for them:
//
//  1. `gopi notify --session <id> --message <text>` — another process hands this
//     session a message. It reaches the REPL as a NOTICE TURN: the operator types
//     nothing, the editor's buffer is empty, and the gate correctly refuses an
//     empty line. The words that arrived are in the notice, not in a buffer.
//  2. a whole-block PASTE — the editor spills the bytes to a file and leaves
//     `[paste N: /abs/path]` on the line. The typing machine sees a reference
//     line, whose only "keyword" is a path; the payload's words are in the file.
//  3. a `/queue`d prompt — a line that becomes a turn LATER, at a boundary the
//     operator's editor had already cleared.
//  4. a line the MODEL typed for itself (`chat_surface` tool, `type` action) —
//     the budget-renewal arm exists precisely because this line is indistinguishable
//     from the operator's by text, and the typing sampler watched a buffer nobody
//     typed into.
//
// THIS FILE ADDS NO SECOND MECHANISM (the epic's own constraint). The decisions
// stay in internal/memoryprefetch, whose state machine was written for this
// caller ("Step decides whether a TURN is worth a retrieval … a turn boundary,
// driven by a composer that has the whole buffer" — session.go) and had no
// caller until now: gate, delta, throttle, budget trim, RAG section and injected
// form are the SAME code the typing path uses. What was missing was the INPUT —
// `memoryprefetch.TurnInput`/`TurnText`/`TurnStep` — and this file, which owns
// what the pure package may not: reading the referenced payload off disk, and
// the adoption at the one place a turn becomes a model request.
//
// THE BOUNDARY IS NOT BLOCKED, and that is a measurement rather than a promise.
// The lookup runs on its own goroutine; the seal waits at most
// prefetchTurnWaitMs for it, which is far below the 1500ms discipline the lookup
// itself obeys (prefetchGoroutineTimeoutMs). A lookup that misses that window
// leaves the turn WITHOUT a section and still records its own outcome when it
// finishes — recorded, never silent, and never a hang: worst case the operator's
// turn starts 250ms late, typical case ~20-90ms (the numbers the give-up
// diagnosis measured on this host's 236MB ledger).

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/memoryprefetch"
	"github.com/nickhuang99/gopi/internal/replaymode"
	"github.com/nickhuang99/gopi/internal/tools"
)

// The turn-boundary numbers, named (Rule 1).
const (
	// prefetchTurnWaitMs is how long the SEAL may wait for a turn lookup already
	// in flight, in milliseconds. It is deliberately SMALLER than the DSN's own
	// busy timeout (prefetchLedgerBusyTimeoutMs, 250ms is its equal) and far
	// below the lookup's own 1500ms bound: the seal is the operator's Enter, and
	// the honest answer for a ledger that is not answering is "this turn goes out
	// without a section", recorded, rather than a turn held open.
	prefetchTurnWaitMs = 250

	// prefetchTurnPayloadReadBytes is how many bytes of ONE referenced payload
	// are read for the retrieval key. It is the RUNE budget the pure core
	// enforces (memoryprefetch.DefaultMaxTurnPayloadRunes) converted at UTF-8's
	// widest rune, so the read can never be the thing that decides the bound —
	// TurnText clips by runes whatever this hands it.
	prefetchTurnPayloadReadBytes = 4 * memoryprefetch.DefaultMaxTurnPayloadRunes
)

// chatPrefetchTurnTerms names WHAT the turn is about, from the ARM that produced
// the line — never from the sealed message.
//
// WHY NOT THE SEALED MESSAGE: the boundary frames what it bills with constant
// prose ("[system] An external process sent this session a message: ", "[gopi
// message boundary] What you received is a REFERENCE …"). Those words are
// identical on every turn of that shape, so extracting from the framed message
// would put the SAME candidates in the gate's hands every time — a constant term
// satisfies the gate by itself (making the gate's primary signal say "worth
// retrieving" about nothing) and pollutes the delta machine with a keyword that
// never changes. The turn's own words are what a reader would call the question:
//
//   - a model-driven line (`selfDriven`) → the line, because the model typed it
//     as its own input and there is no operator text to prefer;
//   - a turn opened by notices → each EXTERNAL message's verbatim text (the
//     words a `gopi notify … --message …` delivery carried) and each background
//     job's COMMAND (what the completion notice is about);
//   - everything else → the line itself: the operator's typed line, a paste
//     turn's line, or a `/queue`d prompt, which are all the operator's words.
//
// A notice turn whose notices carry neither an external message nor a command
// contributes nothing rather than falling back to the composed opening line:
// that line is constant prose about ids, and the honest answer for it is "no
// usable terms", which the state machine already names.
func chatPrefetchTurnTerms(selfDriven bool, notices []tools.JobNotice, line string) []string {
	if selfDriven {
		return []string{line}
	}
	if len(notices) == 0 {
		return []string{line}
	}
	var terms []string
	for _, n := range notices {
		if n.External != nil {
			if m := strings.TrimSpace(n.External.Message); m != "" {
				terms = append(terms, m)
			}
			continue
		}
		if c := strings.TrimSpace(n.Command); c != "" {
			terms = append(terms, c)
		}
	}
	return terms
}

// chatPrefetchTurnInput assembles the turn's retrieval input: the terms the arm
// named, plus the TEXT of every payload the sealed message refers to.
//
// WHY THE FILE IS READ HERE rather than taken from the sealed message: the
// boundary deliberately does NOT quote a spilled payload ("the payload's bytes
// appear in it NOWHERE — that absence is the criterion"), so a paste turn's
// message carries a reference and a size, not the pasted words. The reference's
// path is the handle the model itself is told to `read`; this reads the same file
// for the retrieval key, under prefetchTurnPayloadReadBytes.
//
// A payload that cannot be read is RETURNED AS A FAILURE rather than dropped
// (Rule 15): the turn then carries a key that is missing the very bytes the turn
// refers to, which is a fact about this turn and must be said out loud.
func chatPrefetchTurnInput(out chatOutbound, terms []string) (memoryprefetch.TurnInput, []string) {
	in := memoryprefetch.TurnInput{}
	for _, t := range terms {
		if s := strings.TrimSpace(t); s != "" {
			in.Parts = append(in.Parts, s)
		}
	}
	var failures []string
	seen := map[string]bool{}
	read := map[string]bool{}
	var paths []string
	for _, path := range append(lineedit.PasteRefPaths(out.Message), out.PayloadPath) {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
		text, err := prefetchTurnPayload(path)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		read[path] = true
		in.Payloads = append(in.Payloads, text)
	}
	// THE POINTER LEAVES THE KEY ONCE ITS PAYLOAD IS IN IT (measured — see
	// lineedit.StripPasteRefs). A reference renders the path, the size and the
	// rune count, and the keyword budget is smaller than that: on the live paste
	// run the recorded query was `paste turn 5411 home the operator workspace gopi
	// sandbox`, i.e. the operator's two words plus the pointer's, with the
	// payload's own terms never reaching the extractor's cut. The payload is read
	// ABOVE, so keeping the pointer as well pays twice for the same content and
	// spends the budget on the one token that answers no question.
	//
	// It is stripped only when EVERY reference this turn carries was read: a
	// reference whose file could not be opened is the only description of that
	// payload the turn has, and deleting it would turn a recorded failure into a
	// silent loss of the content (Rule 15).
	allRead := true
	for _, p := range paths {
		if !read[p] {
			allRead = false
			break
		}
	}
	if allRead {
		for i, part := range in.Parts {
			in.Parts[i] = lineedit.StripPasteRefs(part)
		}
	}
	return in, failures
}

// prefetchTurnPayload reads ONE referenced payload, bounded. The read is a plain
// file read of a path the message itself names — no ledger, no lock, no
// connection — so it cannot be blocked by a writer and needs no timeout of its
// own; the byte bound is what keeps a paste from becoming a memory event.
func prefetchTurnPayload(path string) (text string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("turn payload %s could not be opened: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("turn payload %s could not be closed: %w", path, cerr)
		}
	}()
	raw, rerr := io.ReadAll(io.LimitReader(f, prefetchTurnPayloadReadBytes))
	if rerr != nil {
		return "", fmt.Errorf("turn payload %s could not be read: %w", path, rerr)
	}
	// A byte-bounded read can stop inside a multi-byte rune; ToValidUTF8 drops
	// the incomplete tail, so the key is a shorter string and never a corrupt one.
	return strings.ToValidUTF8(string(raw), ""), nil
}

// prefetchTurnLookup is ONE turn-boundary retrieval in flight: the channel the
// seal waits on, and the result the goroutine wrote before closing it (the close
// is the happens-before edge, so the seal reads a published value rather than
// racing the writer).
type prefetchTurnLookup struct {
	done   chan struct{}
	result prefetchResult
}

// chatPrefetchAdoptTurn is the turn-boundary adoption: it runs ONE bounded,
// read-only retrieval for the turn's whole input and appends its section to the
// OUTBOUND message (never to Display — the operator's own line on screen is not
// a background retrieval's to edit).
//
// IT IS A SECOND CALLER OF THE SAME MACHINERY, not a second machine: the gate,
// the delta, the throttle and the rendering are internal/memoryprefetch's, and the
// lookup is chat_prefetch.go's own (same read-only DSN, same warmed handle, same
// 1500ms bound, same record). What differs is only WHERE the input comes from.
//
// THE OFF PATH: `p == nil` returns before anything is read, so with the switch off
// there is no file read, no candidate set, no goroutine and no record line — the
// message stays byte-identical and the record file is not written. That ordering
// is the contract, not an optimisation (cmd/agent/chat_prefetch_test.go pins it).
func chatPrefetchAdoptTurn(out chatOutbound, p *chatPrefetch, source string, terms []string) (chatOutbound, bool) {
	if p == nil {
		return out, false
	}
	in, failures := chatPrefetchTurnInput(out, terms)
	for _, f := range failures {
		p.reportTurnPayloadFailure(f)
	}
	// THE ONE-RETRIEVAL-PER-TURN CONTRACT (task 01a11601). When the operator
	// TYPED the line, the typing sampler has already fetched a key built from the
	// same words — so the turn boundary would pay a second time for one question.
	// The test is on the QUERY, built here by the same pure entry point the
	// lookup would use (chatPrefetchTurnQuery), because that is the only honest
	// statement of "this turn's question is already answered": a notify turn, a
	// paste turn and a /queue'd prompt all produce a DIFFERENT key and therefore
	// still retrieve (their `terms` carry words the sampler never saw).
	//
	// It runs BEFORE TurnStep on purpose. TurnStep's gate is a decision about the
	// turn, and the delta machine would consume the step (and the terms would be
	// admitted by the shared path below) — a sharing that first advanced the turn
	// machine would make the same key look NEW to a later turn.
	if shared, ok := p.takeShared(out.Message, p.turnQuery(in), source); ok {
		out.Prefetch = shared
		return out, true
	}

	// The turn's own state machine. It is touched from the REPL goroutine only
	// (one turn at a time, and the seal is on that goroutine), which is why it is
	// not behind the executor's mutex — the loop goroutine owns the TYPING state
	// (p.session) and never this one.
	action, err := memoryprefetch.TurnStep(&p.turnState, in, p.cfg)
	if err != nil {
		// The input was unusable for a reason the pure core named. Recorded
		// rather than swallowed (Rule 15) — and the turn still runs: a retrieval
		// that cannot be made is not a reason to refuse the operator's message.
		p.recordLine(prefetchRecord{
			Outcome: prefetchOutcomeError,
			Adopted: prefetchRecordAdoptedNo,
			Source:  source,
			Err:     prefetchErrText(err),
		})
		return out, false
	}
	if action.Kind == memoryprefetch.ActionNone {
		// The named why (gate-not-triggered / no-new-terms / throttled-typing-steps)
		// is the machine's own answer, and a turn with nothing to retrieve is not
		// an event worth a record line: the typing path does not record one either.
		return out, false
	}
	lookup := p.startTurnLookup(action.Query, source)
	select {
	case <-lookup.done:
	case <-time.After(time.Duration(prefetchTurnWaitMs) * time.Millisecond):
		// The lookup is slower than the seal may wait. The turn goes out with no
		// section and the lookup's OWN line lands when it finishes, with its real
		// outcome and segments — a bounded miss, never a hang and never silence.
		return out, false
	}
	if lookup.result.Outcome != prefetchOutcomeHits || lookup.result.Section == "" {
		return out, false
	}
	// Admit only AFTER the retrieval succeeded, which is the state machine's own
	// contract (a failed fetch costs a retry rather than pretending the terms are
	// in hand).
	if err := p.turnState.Admit(action.NewTerms); err != nil {
		p.reportTurnPayloadFailure(err.Error())
		return out, false
	}
	out.Prefetch = chatPrefetchSection{Query: action.Query, Section: lookup.result.Section}
	p.recordLine(prefetchRecord{
		Query:        action.Query,
		Outcome:      prefetchOutcomeAdopted,
		Adopted:      prefetchRecordAdoptedYes,
		SectionBytes: len(lookup.result.Section),
		Source:       source,
	})
	return out, true
}

// startTurnLookup runs ONE turn lookup on its own goroutine and records its
// outcome there. The goroutine — not the seal — owns the record line, so a
// lookup that outlives the wait window still reports what it found and how long
// it took (the alternative, recording on the seal's side, would silently drop
// exactly the slow case a reader most needs to see).
func (p *chatPrefetch) startTurnLookup(query, source string) *prefetchTurnLookup {
	l := &prefetchTurnLookup{done: make(chan struct{})}
	go func() {
		defer close(l.done)
		started := time.Now()
		// A REPLAY ASKS NOTHING OF THE LIVE LEDGER — and THIS is the leg that
		// makes the guard necessary rather than tidy (epic defect B, measured
		// 2026-10-20): guarding fetch() alone left the record full of `hits`
		// rows during a replay, because the sealed-turn retrieval is a SECOND
		// caller of prefetchLookupDetailed and it runs on every recorded turn.
		// The record says `refused` — not `empty` — so the run's own evidence
		// distinguishes "the store had nothing" from "this run was not allowed
		// to ask" (Rule 15).
		if replaymode.Active() {
			// RECORDED, NOT DRAWN (see chat_prefetch.go): the refusal belongs in the
			// run's evidence file, never in the band that draws the recording.
			announceReplayMemoryRefusal("run a turn-boundary memory prefetch against the operator's ledger (" + query + ")")
			p.recordLine(prefetchRecord{
				Query:   query,
				Outcome: prefetchOutcomeRefused,
				Adopted: prefetchRecordAdoptedNo,
				Source:  source,
			})
			return
		}
		res := prefetchLookupDetailed(p.coreOrBackground(), query, p.cfg, p.handle(), p.notesSource())
		l.result = res
		p.recordLine(prefetchRecord{
			Query:         query,
			Hits:          prefetchHitsLabel(res.Hits),
			Ms:            prefetchMsLabel(time.Since(started).Milliseconds()),
			Outcome:       res.Outcome,
			Adopted:       prefetchRecordAdoptedNo,
			Source:        source,
			Err:           prefetchErrText(res.Err),
			Segment:       res.Segment,
			Segments:      &res.Seg,
			Notes:         prefetchNotesLabel(res.Notes),
			MemoryOutcome: res.MemoryOutcome,
		})
	}()
	return l
}

// discardTypingSection drops a TYPING-TIME section a turn is not going to use,
// because the same turn already carries the turn-boundary section. It records the
// supersession rather than deleting it in silence: the two retrievals both
// happened, and a reader of the record must be able to see that one of them was
// paid for and not used (Rule 15).
func (p *chatPrefetch) discardTypingSection(line string) {
	if p == nil {
		return
	}
	if _, ok := p.take(line); !ok {
		return
	}
	p.recordLine(prefetchRecord{
		Outcome: prefetchOutcomeSuperseded,
		Adopted: prefetchRecordAdoptedNo,
	})
}

// reportTurnPayloadFailure names a referenced payload that could not be read into
// the turn's retrieval key. It goes to the executor's stderr — the same channel
// the constructor already reports an unusable record on — because the fact is
// about the INPUT, and it is true whether or not a lookup follows it.
func (p *chatPrefetch) reportTurnPayloadFailure(detail string) {
	if p == nil || p.stderr == nil {
		return
	}
	fmt.Fprintf(p.stderr, "[gopi] memory prefetch: %s — this turn's retrieval key omits it\n", detail)
}
