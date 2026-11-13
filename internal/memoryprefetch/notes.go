package memoryprefetch

// notes.go — the SECOND retrieval leg of the same prefetch: the round-level
// [IMPORTANT] note index (internal/tools/chat_notes.go).
//
// WHAT THIS IS. Until this file existed the prefetch knew ONE store — the
// distilled memory ledger (render.go's `memoryLedgerOrigin`) — and a lesson that
// lived in a recorded note was invisible to a retrieval that runs while the
// operator types, even though the notes are indexed, ranked and searchable
// (`gopi session notes <query>` reads that index today). This file is the
// RENDERING half of the second leg, and it is deliberately the same two things
// the memory leg is:
//
//   - the same SECTION (one header, one line per hit, the same rune budget, the
//     same named degradations) — a second section would be a second answer to
//     "what does an injected block look like" (Rule 14);
//   - the same provenance discipline (every line says WHERE it came from).
//
// IT DOES NOT SEARCH, and that is the whole point of the split. The tier ladder
// (fts → trigram → like), the ranking, the limit semantics and the provenance
// columns live in internal/tools, which owns the table; this package must not
// import the tools layer (config.go's header states why: it is standalone by
// contract, so its decisions are testable without a ledger). So internal/tools
// answers the query, the caller maps the rows onto NoteHit, this package renders
// them. ONE implementation of the search, ONE of the render, no third store
// (Rule 14, and Rule 35's zero-dependency posture).
//
// SOURCE ANNOTATION IS THE POINT OF THIS FILE. A reader of an injected block
// must be able to tell which INDEX a line came from — a distilled lesson or this
// machine's own round-level record — because the two carry different evidence.
// The note leg's own provenance (the host and the full path of the record file)
// is what disambiguates two machines whose home directories are literally the
// same string.

import (
	"fmt"
	"strconv"
	"strings"
)

// noteIndexOrigin is the note leg's provenance origin, the sibling of
// memoryLedgerOrigin ("memory"). It is the token a reader — and every test in
// this tree — matches on to tell the two legs apart inside ONE section.
const noteIndexOrigin = "notes"

// NoteIndexOrigin exposes the note leg's provenance token — the string that
// appears in a rendered line's source annotation (render.go's SourceAnnotation)
// and, from this package's side, tells the two retrieval legs apart.
//
// EXPORTED BECAUSE IT IS AN OBSERVABLE, not because a caller needs to build one:
// the token is what a reader (and a test) matches on to answer "which index
// answered this line?", and a consumer forced to re-type "notes" would be a
// mirror of a constant (Rule 1b/13) that silently stops matching the moment the
// token changes.
func NoteIndexOrigin() string { return noteIndexOrigin }

// sourceCellSep separates the cells of a note hit's provenance slot. It is a
// constant (Rule 1b) because the renderer and the tests match on the same shape.
const sourceCellSep = " | "

// NoteHit is ONE retrieved note, as data: the same split the memory leg's Hit
// makes — a local struct rather than internal/tools.ChatNote, one field at a
// time mapped at the seam.
//
// EVERY FIELD SURVIVES THE SEAM, including the zero-able ones, and that is a
// deliberate asymmetry with the store's own contract (Rule 15a): an empty Host
// or Path is a REAL state the store preserves ("this run had no directory to
// name"), so it must arrive here as the absence it is rather than be filled with
// a guess. What the renderer refuses is the opposite — a row with no text is
// dropped BY NAME, never rendered as an anonymous line (see NoteHits).
type NoteHit struct {
	// Record is the note's row identity in chat_notes. It is rendered as the
	// `#N` a reader can hand back to `gopi session notes` — an address in the
	// NOTE index, which the line's own source token says.
	Record int
	// Session is the run the note was recorded in.
	Session string
	// Round is that run's 1-based round number, or 0 when the run could not say
	// (internal/tools' RoundIndexUnknown) — never a fabricated round 1.
	Round int
	// Note is the recorded text.
	Note string
	// Host is the machine the note was recorded on.
	Host string
	// Path is the FULL local path of the record file, so a reader can tell two
	// same-looking home directories apart.
	Path string
	// CreatedAt is the record's own clock (Unix seconds); <= 0 is a stated
	// unknown, never a fabricated time.
	CreatedAt float64
}

// noteRoundUnknown / noteHostUnknown / notePathUnknown / noteTimeUnknown are
// what an absent provenance cell renders as. They exist so an absence reads as a
// stated absence instead of a blank a reader might take for a formatting
// accident (Rule 15) — the house style internal/tools' note renderer already
// uses (`round ?`, `unknown-host`, `(no path recorded)`).
const (
	noteRoundUnknown  = "round ?"
	noteHostUnknown   = "unknown-host"
	notePathUnknown   = "(no path recorded)"
	noteTimeUnknown   = "time unknown"
	noteSessionAbsent = "(no session)"
)

// NoteHits maps a lookup's note rows onto this package's Hit — the type the
// renderer, the trim and the rune budget already work on — and returns the
// degradations the mapping itself produced.
//
// excludeSession DROPS EVERY HIT RECORDED IN THAT SESSION, and it is the reason
// this function takes an argument at all. THE MEASUREMENT BEHIND IT (the
// operator's own ledger, this epic's own run): a prefetch query of
// `gopi memory_prefetch 只查 memory 知识库一条腿，不读 chat_notes…应并列检索` was
// answered by the note index with exactly ONE hit — and that hit was the
// [IMPORTANT] block the SAME session had written moments earlier, i.e. text
// already present in the very context the injection would cost tokens to
// re-state. It is an ECHO, not a retrieval: the note index's recall over a
// running conversation is dominated by the conversation itself, because every
// round of it writes a row.
//
// WHY EXCLUSION AND NOT A SCORE OR A LENGTH FLOOR. An echo is not
// "low-confidence" and not "short" — it is a hit whose provenance IS the reader,
// which is a fact the caller knows exactly and the index records exactly. A
// threshold (tier the newest rows down, drop the first hit, require N runes)
// would be a guess about a property that is already known, and it would suppress
// the genuinely good note from another session that happens to be short. The
// caller passes ITS OWN session id; an empty value excludes nothing, which is
// what a caller with no session (a test, a diagnostic) should get.
//
// Note is rendered as the hit body, and the provenance cell is built by
// noteProvenanceCell — together they are the acceptance criterion of this leg
// stated as a rendering decision: machine AND full path, because the two
// machines of this topology have home directories that are the same string.
func NoteHits(notes []NoteHit, excludeSession string, cfg Config) ([]Hit, []Degradation) {
	cfg = cfg.withDefaults()
	exclude := strings.TrimSpace(excludeSession)
	out := make([]Hit, 0, len(notes))
	var degs []Degradation
	for _, n := range notes {
		if exclude != "" && strings.TrimSpace(n.Session) == exclude {
			// Recorded by the reader's own run: stated, never silently dropped
			// (Rule 15) — the count of echoes suppressed is an observable of
			// this leg's precision.
			degs = append(degs, Degradation{
				Code: DegradationNoteSelfEcho,
				Detail: fmt.Sprintf("note #%d was recorded by this same session (%s) and was not injected: it is already in the reader's own context",
					n.Record, exclude),
			})
			continue
		}
		if strings.TrimSpace(n.Note) == "" {
			degs = append(degs, Degradation{
				Code: DegradationNoteHitEmpty,
				Detail: fmt.Sprintf("note #%d (session %s) carries no text and was not rendered",
					n.Record, noteSessionOrAbsent(n.Session)),
			})
			continue
		}
		out = append(out, Hit{
			ID:   n.Record,
			Note: n.Note,
			// The ORIGIN is what makes renderHit annotate the line with the note
			// index instead of the memory ledger, and what makes the
			// adjudicability marker say `recorded-note` rather than borrow the
			// ledger's hot/warm/cold vocabulary — a note has no tier, and
			// rendering one it does not have would be a fabricated judgement
			// (the failure AdjudicabilityUnknown exists to prevent).
			Source: noteIndexOrigin,
			// The provenance cell occupies the tier slot: a note HAS no tier, so
			// the slot carries what stands behind the hit instead — machine,
			// round, session, time and the FULL path of the record file.
			TierCell: noteProvenanceCell(n),
		})
	}
	return out, degs
}

// noteProvenanceCell is the third cell of a note hit's line: which machine
// recorded it, at which round of which run, when, and where its record file
// lives. It occupies the slot the memory leg uses for tier+stale.
func noteProvenanceCell(n NoteHit) string {
	host := strings.TrimSpace(n.Host)
	if host == "" {
		host = noteHostUnknown
	}
	path := strings.TrimSpace(n.Path)
	if path == "" {
		path = notePathUnknown
	}
	cells := []string{
		host,
		noteRoundCell(n.Round),
		noteSessionOrAbsent(n.Session),
		NoteTimeCell(n.CreatedAt),
		path,
	}
	return strings.Join(cells, sourceCellSep)
}

// noteRoundCell renders the round, or states that the run could not say.
func noteRoundCell(round int) string {
	if round > 0 {
		return "round " + strconv.Itoa(round)
	}
	return noteRoundUnknown
}

// noteSessionOrAbsent names a session, or says there is none.
func noteSessionOrAbsent(session string) string {
	if s := strings.TrimSpace(session); s != "" {
		return s
	}
	return noteSessionAbsent
}

// NoteTimeCell renders a note's clock as its Unix seconds, or states the
// absence. It is the note leg's own cell rather than internal/tools' formatting
// because this package renders its own line and must not reach into the tools
// layer for it; the raw seconds are the same fact that layer formats.
func NoteTimeCell(ts float64) string {
	if ts <= 0 {
		return noteTimeUnknown
	}
	return strconv.FormatInt(int64(ts), 10)
}

// NoteRecordPrefix is the literal the renderer puts in front of a hit's cell
// (render.go spells the same shape for the memory leg). It is a function so the
// renderer and any caller that needs to strip or build the prefix cannot drift
// apart by one typing a format and the other trimming a literal.
func NoteRecordPrefix(id int) string {
	return "#" + strconv.Itoa(id)
}

// DegradationNoteLegFailed records that the note leg could not be consulted at
// all, with the reason. It is the SECOND leg's Rule 15 obligation: the memory
// leg's result is unaffected by this (the caller renders what it has), but a
// leg that raised an error must never present itself as a leg that found
// nothing — "no notes matched" and "the note index could not be read" are
// different facts, and only one of them is a reader's reason to stop looking.
func DegradationNoteLegFailed(err error) Degradation {
	return Degradation{
		Code:   DegradationNotesLegFailed,
		Detail: fmt.Sprintf("the note index could not be read: %v (the memory ledger's result is unaffected)", err),
	}
}
