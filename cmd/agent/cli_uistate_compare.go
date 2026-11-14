package main

// cli_uistate_compare.go — `gopi ui_state --compare "<text>"`: the ONE call that
// checks a piece of the model's output against the PAINTED grid, using the
// session log as the authority.
//
// WHY THIS RIDES `gopi ui_state` AND NOT A NEW TOOL. The verb already OWNS the
// three things the check needs: it discovers the dump (discovery + identity +
// ambiguity refusal), it reads the artifact (the record, the grid, the blind
// spots), and it prints an honest, qualified observation. The compare is the
// only missing side — the authoritative transcript — and adding it as a flag
// keeps the tool budget untouched (Rule 3) while making the check reachable in
// the call an agent already makes for "what is on screen": `gopi ui_state
// --compare`.
//
// WHY IT MATTERS. A dump re-interpreted at the wrong width looks exactly like a
// dump whose text was lost: correct wraps read as truncation, and an agent
// reasoning from the artifact concludes its own output is corrupted. The
// two-sided containment answer separates the two — the line is in the transcript
// (the authority) and on the grid (the paint) or it is not — and it REFUSES to
// answer at all when the grid's geometry is a fallback or was never painted
// (uiclient.CompareRefused), because a containment result at an unmeasured width
// is the very false signal this whole change exists to remove.
//
// THE TRANSCRIPT SIDE IS THE SAME AUTHORITY `gopi session log <id> --response --text`
// prints: the session log's assistant text blocks, line by line. It is read
// through internal/gopilog — the package that owns that format — so this flag is
// not a second parser of session.jsonl.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// The compare flag vocabulary (Rule 1b: the parser, the usage page and the tests
// read these, never a re-typed literal).
const (
	// uiStateFlagCompare carries the text to look for. Its value may be a whole
	// line, or several lines separated by newlines (each compared on its own).
	uiStateFlagCompare = "--compare"
	// uiStateFlagSession names the authoritative transcript: a session id, or a
	// path to a session.jsonl.
	uiStateFlagSession = "--session"
	// uiStateFlagAll audits the WHOLE answer: every line of the session's text
	// against the grid, as one unit. It is the flag the operator's question needs
	// ("did my answer arrive?") — see uiStateRunAudit.
	uiStateFlagAll = "--all"
	// uiStateFlagLast narrows the audit to the last N lines of the answer (the
	// last turn), so an answer of forty turns does not have to be audited in full
	// to answer "what just went wrong on screen".
	uiStateFlagLast = "--last"
)

// uiStateExitCouldNotRun is the "could not run" code for BOTH checks — the
// one-line compare and the whole-answer audit: no dump armed, a grid nothing
// painted, a fallback geometry, no transcript to compare against, or an answer
// with no lines. It is deliberately not 0: an unchecked screen is not a verified
// one, and a screen nobody could read must never be reported as agreement.
//
// It is ONE constant for both checks because they are the same fact at two
// units: they used to be two constants holding the same number, which is how a
// caller ends up learning two vocabularies for one contract.
const uiStateExitCouldNotRun = 1

// uiStateExitAgreed is what a check that RAN and agreed exits with. Named rather
// than written as a bare 0, so the three codes read as one contract at the ONE
// place that decides them (Rule 1).
const uiStateExitAgreed = 0

// uiStateExitCompareMismatch is the exit code of a check that RAN and found a
// line the two sides disagree about. It is distinct from 1 (usage / nothing to
// read / could not run) and from uiStateExitAmbiguous (2) so a script can tell
// "the observation is intact" from "the paint and the log disagree" without
// parsing prose. The whole-answer audit shares it: an answer whose lines
// disagree with the grid is the same fact at a larger unit.
const uiStateExitCompareMismatch = 3

// uiStateExitOffGrid is what a check that RAN and could NOT DECIDE exits with:
// the line is not in the visible window at all, and the artifact's own saved
// history holds it, so the surface painted it and the terminal has since pushed
// it off the top. It is a code of its own — not 0 (nothing about the CURRENT
// frame was verified) and not 3 (the two sides do not contradict each other) —
// because the alternative is what a caller had before: a 3 that meant "lost" and
// "cannot see it" at once, so a script branching on the exit code concluded the
// paint was broken from a question the observation cannot answer (task
// 01a1c05c, measured: a correct answer that had scrolled out of a 22-row
// viewport reported rc=3 beside a grid holding it whole in its 2,518-row
// history).
const uiStateExitOffGrid = 4

// uiStateOutcome is the exit contract's vocabulary — one word per fact, shared
// by the one-line compare and the whole-answer audit so an operator reads the
// same three words whichever check they ran.
type uiStateOutcome string

// The words themselves (Rule 1b: the code, the rendering and the tests read
// these, never a re-typed literal).
const (
	uiStateAuditWordAgreed          = "agreed"
	uiStateAuditWordDisagreed       = "disagreed"
	uiStateAuditWordCouldNotRun     = "could-not-run"
	uiStateAuditWordNoAuthoritative = "no-authoritative-side"
	// uiStateAuditWordOffGrid is the third answer beside agreed and disagreed:
	// the line is OUTSIDE the observed window (the grid's saved history has it,
	// the visible grid does not), so the check could not decide. It gets its own
	// word and its own exit code because "the paint lost it" and "I cannot see
	// it from here" are different findings, and the one-word vocabulary is what
	// stops a caller having to read an English sentence to tell them apart.
	uiStateAuditWordOffGrid = "off-grid"
)

const (
	uiStateOutcomeAgreed          uiStateOutcome = uiStateAuditWordAgreed
	uiStateOutcomeDisagreed       uiStateOutcome = uiStateAuditWordDisagreed
	uiStateOutcomeCouldNotRun     uiStateOutcome = uiStateAuditWordCouldNotRun
	uiStateOutcomeNoAuthoritative uiStateOutcome = uiStateAuditWordNoAuthoritative
	// uiStateOutcomeOffGrid is "the line is outside the window this artifact
	// observes": painted (the saved history holds it), unverifiable on the
	// current frame. Its own value so the exit code can be its own number —
	// folding it into could-not-run would put it back beside the cases where no
	// comparison was attempted at all, and folding it into disagreed is the
	// measured defect.
	uiStateOutcomeOffGrid uiStateOutcome = uiStateAuditWordOffGrid
)

// uiStateCompareOutcomeOf maps ONE per-line verdict onto the outcome vocabulary.
// The mapping is the whole point of the contract: `no-transcript` and `refused`
// are verdicts the compare can reach, and NEITHER of them is a disagreement —
// both mean "the authoritative side was not read, so nothing was compared", and
// reporting either as a mismatch would let a script conclude the paint is broken
// from a check that never ran.
func uiStateCompareOutcomeOf(v uiclient.CompareVerdict) uiStateOutcome {
	switch v.Status {
	case uiclient.CompareMatch:
		return uiStateOutcomeAgreed
	case uiclient.CompareMismatch:
		return uiStateOutcomeDisagreed
	case uiclient.CompareOffGrid:
		// The line is OUTSIDE the observed window: the surface painted it and it
		// has scrolled out of the visible grid. It is neither an agreement (the
		// current frame does not hold it) nor a disagreement (the two sides do
		// not contradict each other, the window is simply smaller than the
		// question), so it gets the third word and the third code.
		return uiStateOutcomeOffGrid
	case uiclient.CompareNoAuthority:
		// The surface keeps no authoritative record and the caller named none:
		// said in its OWN word, never as the neutral could-not-run. The two are
		// different facts (one names a MISSING SIDE, the other an unchecked
		// screen) and the measured complaint (task 01a0e62d) is that a reader
		// could not tell them apart.
		return uiStateOutcomeNoAuthoritative
	default:
		// CompareNoTranscript / CompareRefused — and any verdict a later version
		// adds that is not an agreement and not a localised loss. A new verdict
		// defaults to "could not run", which is the honest reading of a status
		// nobody classified: it never claims the check passed.
		return uiStateOutcomeCouldNotRun
	}
}

// uiStateOutcomeRank orders the outcomes by how much a reader learns from them,
// so a whole-call outcome is decided by a MAX rather than by a chain of
// special cases (the chain this replaces grew one `if` per new word, and each
// `if` was a place two words could be compared wrongly).
//
//	disagreed (a real, localised loss) — most informative
//	off-grid  (a definite statement about the WINDOW)
//	no-authoritative-side (a definite statement about the ARTIFACT)
//	could-not-run (nothing was checked)
//	agreed (everything checked, nothing wrong) — least informative
func uiStateOutcomeRank(o uiStateOutcome) int {
	switch o {
	case uiStateOutcomeDisagreed:
		return 4
	case uiStateOutcomeOffGrid:
		return 3
	case uiStateOutcomeNoAuthoritative:
		return 2
	case uiStateOutcomeCouldNotRun:
		return 1
	default:
		return 0
	}
}

// uiStateCompareOutcome is the outcome of a WHOLE compare call, from its
// per-line verdicts: the HIGHEST-ranked outcome any line reached (see
// uiStateOutcomeRank). A localised loss outranks a window limitation, which
// outranks an artifact with no authority, which outranks "nothing was checked";
// `agreed` is the answer only when every line agreed.
func uiStateCompareOutcome(verdicts []uiclient.CompareVerdict) uiStateOutcome {
	out := uiStateOutcomeAgreed
	for _, v := range verdicts {
		if o := uiStateCompareOutcomeOf(v); uiStateOutcomeRank(o) > uiStateOutcomeRank(out) {
			out = o
		}
	}
	return out
}

// uiStateAuditOutcomeOf maps the audit ledger's status onto the SAME three
// words, so the two checks cannot drift apart in what they report.
func uiStateAuditOutcomeOf(status uiclient.AuditStatus) uiStateOutcome {
	switch status {
	case uiclient.AuditAgreed:
		return uiStateOutcomeAgreed
	case uiclient.AuditDisagreed:
		return uiStateOutcomeDisagreed
	case uiclient.AuditNoAuthority:
		return uiStateOutcomeNoAuthoritative
	case uiclient.AuditOffGrid:
		// The whole-answer twin of the one-line case: at least one line is outside
		// the observed window, so the audit cannot claim agreement (it never saw
		// those lines) and is not a disagreement (nothing contradicts anything).
		return uiStateOutcomeOffGrid
	default:
		// AuditRefused / AuditNoTranscript — nothing was checked.
		return uiStateOutcomeCouldNotRun
	}
}

// uiStateExitCode is THE ONE place the exit code is decided, for both checks:
//
//	0  the check RAN and every line agreed
//	3  the check RAN and at least one line disagreed
//	1  the check COULD NOT RUN (no dump, nothing painted, a fallback geometry, no
//	   transcript, or an answer with no lines) — never reported as agreement and
//	   never reported as a disagreement
//
// The printed outcome word and the process exit code are derived from the SAME
// outcome value here, which is what stops a report from saying "agreed" beside a
// non-zero exit — the confusion this contract exists to remove. Before this
// function the mapping existed twice (auditExitCode for the audit, an inline
// `if !rec.Match` for the compare) and the compare's could-not-run path exited 3,
// i.e. it reported "no transcript to compare against" with the DISAGREEMENT code.
func uiStateExitCode(outcome uiStateOutcome) int {
	switch outcome {
	case uiStateOutcomeAgreed:
		return uiStateExitAgreed
	case uiStateOutcomeDisagreed:
		return uiStateExitCompareMismatch
	case uiStateOutcomeOffGrid:
		// The third code, and the reason this rule exists: a line outside the
		// observed window is a definite finding ("the window does not hold it")
		// that is NOT a paint loss, and the measured defect was a caller
		// branching on 3 and concluding the paint was broken.
		return uiStateExitOffGrid
	default:
		// uiStateOutcomeCouldNotRun and uiStateOutcomeNoAuthoritative: the check
		// did not compare anything either way, and 1 is the code for "not a
		// comparison". They are told apart by the WORD (and the reason), never by
		// the code, because an unchecked screen is not a verified one.
		return uiStateExitCouldNotRun
	}
}

// uiStateCompareMaxLines bounds one compare call: the flag is a check of a line
// the caller already has in hand, not a bulk diff (a whole answer belongs to a
// file, and an unbounded loop over a path-qualified argument is how a CLI turns
// into a loader). Exceeding it is REFUSED by name, never silently truncated. The
// WHOLE-ANSWER audit is bounded instead by uiclient.AuditBound, which is a bound on
// the answer rather than on what a caller typed — so exceeding IT is reported on
// the ledger rather than refused as a usage error.
const uiStateCompareMaxLines = 64

// uiStateCompareRefusalNoSession is the refusal when no authority was named AND
// none could be derived from the artifact. The check cannot be run one-sided,
// and saying so is the difference between "the paint is faithful" and "nobody
// checked" (Rule 15).
const uiStateCompareRefusalNoSession = "%s needs the authoritative transcript: pass %s <session-id|session.jsonl> (the same text `gopi session log <id> --response --text` prints)"

// The compare/audit TEXT header's authority provenance (Rule 1b: the rendering
// and the tests read these, never a re-typed literal).
const (
	// uiStateFieldSessionFile names the authority's PROVENANCE in the text header:
	// the path that was actually read, or uiStateSessionNotRead. It is the text
	// twin of the JSON envelope's `session_file` field, and it exists because the
	// header's `transcript_lines=0` alone cannot tell "the log was read and held
	// no lines" from "the log was never read" — the two printed byte-identically,
	// so a reader of the text form had to guess which fact they were looking at.
	uiStateFieldSessionFile = "session_file"
	// uiStateSessionNotRead is the provenance of an authority that was NOT read:
	// a grid that could not be read as a screen (an unpainted or fallback-geometry
	// artifact) refuses the check before the transcript is ever opened, and saying
	// so out loud is what keeps `transcript_lines=0` from reading as an empty log.
	uiStateSessionNotRead = "not-read"
)

// uiStateSessionProvenance renders the authority's provenance for a TEXT header:
// `session_file=<the resolved path>` when a transcript was read, and
// `session_file=not-read` when none was — the SAME fact the JSON envelope carries
// in its session_file field, spelled for the channel a human reads.
//
// One function, because the compare and the whole-answer audit print the same
// header shape and must never diverge again: the divergence that produced this
// helper was the text form losing the provenance while the machine form kept it.
func uiStateSessionProvenance(sessionFile string) string {
	file := sessionFile
	if strings.TrimSpace(file) == "" {
		file = uiStateSessionNotRead
	}
	return uiStateFieldSessionFile + uiStateFlagSep + file
}

// uiStateCompareDerivedSessionUnreadable is the refusal when the authority was
// DERIVED from the dump (see uiStateCompareSiblingSession) but could not be
// read. It names the path it tried, because the caller never typed it and has
// no way to guess what the derivation picked.
const uiStateCompareDerivedSessionUnreadable = "cannot read the transcript %s (derived from the dump's own session directory): %w"

// uiStateCompareSiblingSession derives the authoritative transcript from the
// dump the caller is comparing against.
//
// It answers ONE question — "which session directory owns this dump" — through
// the ONE resolver that knows every layout the sessions tree uses
// (uiclient.SessionDirOfDump → internal/sessionlayout's classifier), then asks
// for that directory's record. The narrower rule it replaces checked exactly two
// hard-coded shapes (`<dir>/session.jsonl`, `<dir>/../session.jsonl`), so a dump
// written under the layout the TASK MACHINERY ITSELF USES —
// `<sessions>/<task-id>/chat-screen.dump` beside `<task-id>/<run-uuid>.jsonl` —
// derived nothing, and the one-call check refused the very surface the caller
// was looking at (measured 2026-09-27, this host).
//
// WHY THIS EXISTS. `--compare` is the ONE-CALL check, and it shipped as a
// TWO-call experience: naming only the line produced
// uiStateCompareRefusalNoSession ("pass --session <id|path>", exit 1), which is
// a correct refusal standing in for a question the artifact could already
// answer. The dump KNOWS which session wrote it; making the caller retype that
// is ceremony, and a first-time caller naturally omits it, so the check an
// agent reachably needs is exactly the one it bounces off.
//
// It DEFAULTS, it does not GUESS: the directory's record must exist, and when
// none does the caller gets the refusal naming --session rather than a
// comparison against nothing (Rule 15 — never a silent fallback). An explicit
// `--session` always wins (uiStateResolveCompareSession checks it FIRST), so
// this cannot shadow a caller who named a different authority on purpose.
func uiStateCompareSiblingSession(dumpPath string) (string, bool) {
	dir, ok := uiclient.SessionDirOfDump(dumpPath)
	if !ok {
		return "", false
	}
	rec, ok := sessionlayout.RecordInDir(dir)
	if !ok {
		return "", false
	}
	return rec.Path, true
}

// uiStateAuditRefusalNoSession is the same refusal for the whole-answer audit,
// spelled with the flag the caller actually passed: an audit that silently fell
// back to "no session" would answer about nothing.
const uiStateAuditRefusalNoSession = "%s needs the authoritative transcript to audit: pass %s <session-id|session.jsonl> (the same text `gopi session log <id> --response --text` prints)"

// uiStateCompareSessionUnresolved is the refusal when a session OPERAND names no
// session the verb can read — an id matching nothing, or a path that is not
// there.
//
// It quotes the operand THE CALLER TYPED, and it carries a reason as a string
// rather than leaning on a nil error. Both halves are the fix for a measured
// defect (2026-09-27): `--session <an-id-that-matches-nothing>` printed
//
//	cannot read the transcript : open : no such file or directory
//
// — an EMPTY operand (the resolution had returned "" with a nil error, so the
// message named a path nobody typed) and a nil cause (Rule 15: a swallowed
// error reports a failure with no reason). A caller cannot act on either.
const uiStateCompareSessionUnresolved = "cannot read the transcript %q: %s"

// uiStateNoSessionForID is the reason an id operand that matched no session
// carries: FindSession answers "no such session" with a nil error (its contract
// — "not found" is an ordinary answer for a lookup), and a caller of THIS verb
// needs the sentence, not the silence.
const uiStateNoSessionForID = "no session matches that id (a session id is the full uuid; `gopi session log` lists the sessions this profile has)"

// uiStateCompareSessionUnreadable names the transcript read failure.
const uiStateCompareSessionUnreadable = "cannot read the transcript %s: %w"

// uiStateCompareRecord is the machine-checkable rendering's envelope: which
// artifact was examined, whose transcript it was checked against, and the
// per-line verdicts. It is emitted for --json, so a script gets the same facts
// the text rendering prints.
type uiStateCompareRecord struct {
	Tool string `json:"tool"`
	// DumpPath is the artifact the grid came from (the SURFACE is in
	// Surface, because two surfaces can be on hand and only one was read).
	DumpPath string `json:"dump"`
	// Surface is which surface wrote that dump.
	Surface uiclient.Surface `json:"surface"`
	// State is the dump's identity verdict (live / exited / foreign).
	State uiclient.State `json:"state"`
	// Session is the transcript's source, as the caller named it (a path when
	// the caller gave a path).
	Session string `json:"session"`
	// SessionFile is the resolved path that was actually read.
	SessionFile string `json:"session_file"`
	// TranscriptLines is how many authoritative lines were available.
	TranscriptLines int `json:"transcript_lines"`
	// Outcome is the exit contract in one word: agreed / disagreed /
	// could-not-run. A script branches on THIS, not on Match: `no-transcript` and
	// `refused` are verdicts that mean the check never ran, and Match's two values
	// could not say so.
	Outcome uiStateOutcome `json:"outcome"`
	// Verdicts is one entry per compared line.
	Verdicts []uiclient.CompareVerdict `json:"verdicts"`
	// Match reports whether EVERY compared line matched. It is kept for callers of
	// the older envelope and is derived from the SAME outcome as the exit code, so
	// the two can never disagree; a check that could not run is NOT a match.
	Match bool `json:"match"`
}

// uiStateCompareLines splits the flag's value into the lines to check, and
// refuses an oversized request by name.
func uiStateCompareLines(raw string) ([]string, error) {
	trimmed := strings.TrimRight(raw, "\n")
	if strings.TrimSpace(trimmed) == "" {
		return nil, fmt.Errorf("%s needs text to look for (the line as the model produced it)", uiStateFlagCompare)
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > uiStateCompareMaxLines {
		return nil, fmt.Errorf("%s: %d lines is beyond this flag's bound of %d — compare the line you are asking about, or diff the files",
			uiStateFlagCompare, len(lines), uiStateCompareMaxLines)
	}
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			// A blank line is inside every row, so an "it matched" answer for it
			// is vacuous. Refused rather than silently skipped: a caller that
			// sent a blank line believes it learned something.
			return nil, fmt.Errorf("%s: line %d is blank — a blank line appears in every row, so comparing it would answer nothing",
				uiStateFlagCompare, i+1)
		}
	}
	return lines, nil
}

// uiStateTranscriptLines reads the authoritative transcript: the assistant text
// blocks of the session log, one entry per LINE, AS THE LOG HOLDS IT (the model's
// own text, intra-line markers included — the same bytes `gopi session log --response
// --text` prints).
//
// It resolves the source through internal/gopilog (FindSession for an id, ParseFile
// for a path), so both callers read the SAME bytes rather than re-deriving them.
// The DRAWN form (markers removed, as the band paints it) is derived from this one
// slice by uiclient — never by a second reader here.
func uiStateTranscriptLines(session, flag string) (path string, lines []string, err error) {
	session = strings.TrimSpace(session)
	if session == "" {
		return "", nil, fmt.Errorf(uiStateCompareRefusalNoSession, flag, uiStateFlagSession)
	}
	path = session
	if !strings.HasSuffix(session, ".jsonl") && !strings.ContainsRune(session, filepath.Separator) {
		dir, dirErr := gopilog.ResolveSessionDir()
		if dirErr != nil {
			return "", nil, fmt.Errorf(uiStateCompareSessionUnresolved, session, dirErr.Error())
		}
		found, findErr := gopilog.FindSession(dir, session)
		if findErr != nil {
			// The lookup itself failed (an unreadable tree, a truncated id): said
			// with its own reason, never flattened to "not found".
			return "", nil, fmt.Errorf(uiStateCompareSessionUnresolved, session, findErr.Error())
		}
		if strings.TrimSpace(found) == "" {
			// FindSession's "no such session" is ("" , nil) — an ordinary answer for
			// a lookup, and a SWALLOWED failure for this verb. It becomes a sentence.
			return "", nil, fmt.Errorf(uiStateCompareSessionUnresolved, session, uiStateNoSessionForID)
		}
		path = found
	}
	sess, parseErr := gopilog.ParseFile(path)
	if parseErr != nil {
		return path, nil, fmt.Errorf(uiStateCompareSessionUnreadable, path, parseErr)
	}
	for _, iter := range sess.Iterations {
		for _, block := range iter.TextBlocks {
			if strings.TrimSpace(block) == "" {
				continue
			}
			for _, l := range strings.Split(block, "\n") {
				lines = append(lines, l)
			}
		}
	}
	return path, lines, nil
}

// uiStateNoAuthorityReason is the reason a check carries when the derive step
// found NO authoritative record for the artifact. It is the machine-side twin of
// uiclient.NoAuthoritativeSideNotice, and it is a reason rather than an empty
// string so `--json` carries the same fact the text form prints (Rule 15).
//
// The FLAG is prepended and the remedy is named on every arm, because this
// report is the one a caller reaches on a first, unadorned invocation: it must
// say which verb needs a side and which flag supplies one, or the reader is left
// with a fact and no next move (the contract the older refusals already kept,
// uiStateCompareRefusalNoSession).
func uiStateNoAuthorityReason(flag string) string {
	return flag + ": " + uiclient.NoAuthoritativeSideNotice + " — pass " +
		uiStateFlagSession + " <session-id|session.jsonl> to compare against a record held elsewhere"
}

// uiStateResolveCompareSession picks the authority for a check: the caller's
// --session when one was given, otherwise the transcript derived from the dump
// itself. It returns the session to read, whether it was DERIVED, and whether an
// authority to read exists at ALL — the one distinction the reader acts on:
//
//	ok=true                   read the session; a failure to read it is reported
//	                          as its own error (could-not-run, with the reason);
//	ok=false                  nothing was named and the dump's own session
//	                          directory holds no record: there is no authority
//	                          HERE. The check is not "unchecked because something
//	                          went wrong" — it is "this artifact HAS no other
//	                          side", which is its own outcome word (task
//	                          01a0e62d).
func uiStateResolveCompareSession(cand uiclient.Candidate, named string) (session string, derived bool, ok bool) {
	if strings.TrimSpace(named) != "" {
		return named, false, true
	}
	path, found := uiStateCompareSiblingSession(cand.Path)
	if !found {
		return "", false, false
	}
	return path, true, true
}

// uiStateCompareReportSessionError renders a transcript read failure.
//
// A DERIVED path gets its own sentence naming the derivation, because the caller
// never typed it and a bare "cannot read <path>" would read as a complaint about
// their own argument.
//
// AN EXPLICIT `--session` gets the opposite treatment, and it is the fix for a
// measured defect: the failure was reported with the derived-path sentence even
// when the caller had typed the operand itself, so
// `--session <an-id-matching-nothing>` was answered with "derived from the
// dump's own session directory" — a false statement about the caller's own
// argument, which sends them looking at the dump instead of at the id they
// mistyped. The explicit form names the OPERAND and never the derivation.
func uiStateCompareReportSessionError(session string, derived bool, err error) {
	if derived {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbUIState,
			fmt.Errorf(uiStateCompareDerivedSessionUnreadable, session, err))
		return
	}
	fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbUIState, err)
}

// uiStateRunCompare executes the check and returns the process exit code.
//
// The order matters and is deliberate: the dump's own qualification comes FIRST
// (the containment core refuses an unpainted or fallback-geometry grid before it
// looks at any text), and the transcript is read only when a comparison is
// actually possible — a refusal must not require a session to explain itself.
func uiStateRunCompare(cand uiclient.Candidate, raw, session string, asJSON bool) int {
	lines, err := uiStateCompareLines(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbUIState, err)
		return uiStateExitCouldNotRun
	}
	rec := uiStateCompareRecord{
		Tool:     CliVerbUIState,
		DumpPath: cand.Path,
		Surface:  cand.Surface,
		State:    cand.State,
		Session:  session,
	}
	// A grid that cannot be read as the screen's layout is refused BEFORE the
	// transcript is fetched: the answer would be about the artifact, not about
	// the text, and asking for a session would suggest the missing piece is the
	// one that would have helped.
	if refusal := uiclient.CompareGridUnusable(cand); refusal != "" {
		for _, l := range lines {
			rec.Verdicts = append(rec.Verdicts, uiclient.CompareVerdict{
				Status: uiclient.CompareRefused, Line: l, GridRow: -1,
				Geometry: uiclient.GeometryOf(cand), Cols: cand.Dump.Cols, Rows: cand.Dump.Rows,
				Frames: cand.Frames, Reason: refusal,
			})
		}
		return uiStateReportCompare(rec, asJSON)
	}
	session, derived, haveAuthority := uiStateResolveCompareSession(cand, session)
	rec.Session = session
	if !haveAuthority {
		// NO AUTHORITY HERE. Nothing was named and the dump's own session
		// directory holds no record, so no transcript exists to read — the check
		// is not "unchecked because something failed", it is "this artifact has
		// no other side". Reported in its OWN outcome word with the reason, never
		// as the neutral could-not-run beside transcript_lines=0 (the measured
		// ambiguity: task 01a0e62d).
		for _, l := range lines {
			rec.Verdicts = append(rec.Verdicts, uiclient.CompareVerdict{
				Status: uiclient.CompareNoAuthority, Line: l, GridRow: -1,
				Geometry: uiclient.GeometryOf(cand), Cols: cand.Dump.Cols, Rows: cand.Dump.Rows,
				Frames: cand.Frames, Reason: uiStateNoAuthorityReason(uiStateFlagCompare),
			})
		}
		return uiStateReportCompare(rec, asJSON)
	}
	path, transcript, err := uiStateTranscriptLines(session, uiStateFlagCompare)
	if err != nil {
		// The authority could not be read: the check DID NOT RUN. The record is
		// still printed, and every line carries the verdict that says so
		// (no-transcript, with the read failure as its reason), which is what makes
		// the outcome — and therefore the exit code — come out of the SAME
		// predicate as every other case: could-not-run, never agreement (an empty
		// verdict list used to fall through to "agreed"), and never the
		// disagreement code a script would read as "the paint and the log differ".
		uiStateCompareReportSessionError(session, derived, err)
		for _, l := range lines {
			rec.Verdicts = append(rec.Verdicts, uiclient.CompareVerdict{
				Status: uiclient.CompareNoTranscript, Line: l, GridRow: -1,
				Geometry: uiclient.GeometryOf(cand), Cols: cand.Dump.Cols, Rows: cand.Dump.Rows,
				Frames: cand.Frames, Reason: err.Error(),
			})
		}
		return uiStateReportCompare(rec, asJSON)
	}
	rec.SessionFile = path
	rec.TranscriptLines = len(transcript)
	for _, l := range lines {
		rec.Verdicts = append(rec.Verdicts, uiclient.CompareTranscriptLine(l, cand, transcript))
	}
	return uiStateReportCompare(rec, asJSON)
}

// uiStateReportCompare derives the outcome and the exit code from the record's
// OWN verdicts, prints the record, and returns the code. Both of the compare's
// exits go through here, so the printed outcome and the exit code can never be
// derived from different facts (Rule 13's shape, one level up: one decision, one
// place).
//
// Match stays a convenience for the JSON envelope — derived from the SAME outcome
// so the two fields cannot disagree — but it is the OLD two-valued summary and it
// is exactly the field that made a could-not-run read as a disagreement. The
// `outcome` word is the one a caller should branch on.
func uiStateReportCompare(rec uiStateCompareRecord, asJSON bool) int {
	rec.Outcome = uiStateCompareOutcome(rec.Verdicts)
	rec.Match = rec.Outcome == uiStateOutcomeAgreed
	uiStatePrintCompare(rec, asJSON)
	return uiStateExitCode(rec.Outcome)
}

// uiStateAuditRecord is the whole-answer audit's envelope: which artifact was
// examined, whose transcript it was checked against, the ledger, and the exit
// contract's own word for what happened.
type uiStateAuditRecord struct {
	Tool string `json:"tool"`
	// DumpPath / Surface / State identify the artifact the grid came from.
	DumpPath string           `json:"dump"`
	Surface  uiclient.Surface `json:"surface"`
	State    uiclient.State   `json:"state"`
	// Session / SessionFile are the transcript as the caller named it, and the
	// path that was actually read (the resolution is `gopi session log`'s own).
	Session     string `json:"session"`
	SessionFile string `json:"session_file"`
	// Outcome is the exit contract in one word: agreed / disagreed /
	// could-not-run. It is derived from the ledger's status here, so a script does
	// not have to re-derive it, and the exit code is derived from the SAME value
	// (see uiStateExitCode).
	Outcome uiStateOutcome `json:"outcome"`
	// Ledger is the whole-answer verdict and its per-line evidence.
	Ledger uiclient.AuditLedger `json:"ledger"`
}

// uiStateRunAudit executes the WHOLE-ANSWER audit and returns the process exit
// code. It is the unit the operator's question needs: `--compare` checks the line
// the caller already has in hand, so checking a whole answer through it means
// hand-feeding every line — the human-in-the-loop this family of changes removes.
//
// The order is the compare's, for the same reason: the grid's qualification is
// reported before the transcript is read the audit over.
func uiStateRunAudit(cand uiclient.Candidate, session string, last int, asJSON bool) int {
	rec := uiStateAuditRecord{
		Tool:     CliVerbUIState,
		DumpPath: cand.Path,
		Surface:  cand.Surface,
		State:    cand.State,
		Session:  session,
	}
	// A grid that cannot be read as a screen refuses the WHOLE audit: N identical
	// per-line refusals would bury the fact that nothing was checked.
	if refusal := uiclient.CompareGridUnusable(cand); refusal != "" {
		rec.Ledger = uiclient.AuditLedger{Status: uiclient.AuditRefused, Reason: refusal, Geometry: uiclient.GeometryOf(cand), Cols: cand.Dump.Cols, Rows: cand.Dump.Rows, Frames: cand.Frames}
		return uiStateReportAudit(rec, asJSON)
	}
	session, derived, haveAuthority := uiStateResolveCompareSession(cand, session)
	rec.Session = session
	if !haveAuthority {
		// The audit's version of the missing side: the ledger says
		// no-authoritative-side, in its own word, rather than leaving the record
		// empty or reusing the no-transcript word for a fact that is not that one.
		rec.Ledger = uiclient.AuditLedger{Status: uiclient.AuditNoAuthority, Reason: uiStateNoAuthorityReason(uiStateFlagAll), Geometry: uiclient.GeometryOf(cand), Cols: cand.Dump.Cols, Rows: cand.Dump.Rows, Frames: cand.Frames}
		return uiStateReportAudit(rec, asJSON)
	}
	path, transcript, err := uiStateTranscriptLines(session, uiStateFlagAll)
	if err != nil {
		// The audit's authority could not be read: the ledger says so (refused,
		// with the reason) instead of leaving the record empty, and the code is the
		// same could-not-run code every other unchecked case exits with.
		rec.Ledger = uiclient.AuditLedger{Status: uiclient.AuditRefused, Reason: err.Error(), Geometry: uiclient.GeometryOf(cand), Cols: cand.Dump.Cols, Rows: cand.Dump.Rows, Frames: cand.Frames}
		uiStateCompareReportSessionError(session, derived, err)
		return uiStateReportAudit(rec, asJSON)
	}
	rec.SessionFile = path
	// The ANSWER's line count is taken BEFORE the window narrows the slice: an
	// audit of the last N lines that reported N as "the answer's length" would
	// claim the answer was N lines long.
	answerLines := len(transcript)
	transcript, windowed := uiStateAuditWindow(transcript, last)
	rec.Ledger = uiclient.AuditAnswer(cand, transcript)
	rec.Ledger.TranscriptLines = answerLines
	if windowed {
		// The window is said out loud: an audit of the last 40 lines reported as if
		// it covered the answer would claim a coverage it does not have.
		rec.Ledger.Reason = fmt.Sprintf("audited the LAST %d of the %d transcript lines (%s): lines before the window were not checked",
			len(transcript), answerLines, uiStateFlagLast)
	}
	return uiStateReportAudit(rec, asJSON)
}

// uiStateReportAudit derives the outcome and the exit code from the ledger's own
// status, prints the record and returns the code — the audit's single exit, the
// twin of uiStateReportCompare, so both checks reach the contract by the same
// route.
func uiStateReportAudit(rec uiStateAuditRecord, asJSON bool) int {
	rec.Outcome = uiStateAuditOutcomeOf(rec.Ledger.Status)
	uiStatePrintAudit(rec, asJSON)
	return uiStateExitCode(rec.Outcome)
}

// uiStateAuditWindow narrows the answer to its last N lines. n <= 0 means the whole
// answer (the window is the flag's business, so the caller cannot pass a negative
// window that reads as "all").
func uiStateAuditWindow(lines []string, n int) ([]string, bool) {
	if n <= 0 || n >= len(lines) {
		return lines, false
	}
	return lines[len(lines)-n:], true
}

// uiStatePrintAudit renders the audit: text by default, the envelope as JSON on
// request. Both shapes carry the outcome word, the geometry provenance and the same
// counts, so a reader of either gets one story.
func uiStatePrintAudit(rec uiStateAuditRecord, asJSON bool) {
	if asJSON {
		blob, err := json.MarshalIndent(rec, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi %s: cannot render the audit JSON: %v\n", CliVerbUIState, err)
			return
		}
		fmt.Println(string(blob))
		return
	}
	led := rec.Ledger
	fmt.Printf("#gopi-ui-state-audit surface=%s state=%s dump=%s session=%s %s outcome=%s\n",
		rec.Surface, rec.State, rec.DumpPath, rec.Session, uiStateSessionProvenance(rec.SessionFile), rec.Outcome)
	fmt.Printf("  status=%s geometry=%s cols=%d rows=%d frames=%d transcript_lines=%d audited=%d skipped=%d truncated=%t matched=%d disagreed=%d lost_text=%d off_grid=%d rejoined=%d repainted=%d\n",
		led.Status, led.Geometry, led.Cols, led.Rows, led.Frames, led.TranscriptLines, led.Audited, led.Skipped, led.Truncated, led.Matched, led.Disagreed, led.LostText, led.OffGrid, led.Rejoined, led.Repainted)
	fmt.Printf("  emphasis checked=%d unpainted=%d unknown=%d\n",
		led.EmphasisChecked, led.EmphasisUnpainted, led.EmphasisUnknown)
	if led.Reason != "" {
		// The refusal / no-transcript sentence, and the window notice: the facts a
		// reader needs before they trust a count.
		fmt.Printf("  reason=%q\n", led.Reason)
	}
	for _, entry := range led.Lines {
		fmt.Printf("  line %d damage=%s grid_row=%d in_grid=%t in_transcript=%t expected_cols=%d expected_runes=%d grid_cols=%d prefix=%d row_tail_runes=%d display_hits=%d text=%q\n",
			entry.Index, entry.Damage, entry.GridRow, entry.Verdict.InGrid, entry.Verdict.InTranscript,
			entry.ExpectedCols, entry.ExpectedRunes, entry.GridCols, entry.Verdict.GridPrefixRunes, entry.RowTailRunes, entry.Verdict.DisplayHits, entry.Verdict.Line)
		// The DIRECTION, in the verdict's own sentence: which layer lost it, and
		// the longest verbatim prefix on the grid, which is what separates "the band
		// truncated it" from "it never arrived".
		if entry.Verdict.Reason != "" {
			fmt.Printf("    reason=%q\n", entry.Verdict.Reason)
		}
		if entry.Emphasis != nil {
			fmt.Printf("    emphasis verdict=%s row=%d baseline=%q plain_cells=%d\n",
				entry.Emphasis.Verdict, entry.Emphasis.Row, entry.Emphasis.Baseline, auditPlainCells(entry.Emphasis))
			if entry.Emphasis.Reason != "" {
				fmt.Printf("    emphasis_reason=%q\n", entry.Emphasis.Reason)
			}
		}
	}
}

// auditPlainCells totals the measurement for the rendering (the same number the
// emphasis record's own reason quotes).
func auditPlainCells(em *uiclient.AuditEmphasis) int {
	n := 0
	for _, sp := range em.Spans {
		n += sp.PlainCells
	}
	return n
}

// uiStatePrintCompare renders the compare result: text by default, the envelope
// as JSON on request. Both shapes carry the geometry provenance, because a
// containment answer means nothing without the width it was computed at.
func uiStatePrintCompare(rec uiStateCompareRecord, asJSON bool) {
	if asJSON {
		blob, err := json.MarshalIndent(rec, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi %s: cannot render the compare JSON: %v\n", CliVerbUIState, err)
			return
		}
		fmt.Println(string(blob))
		return
	}
	fmt.Printf("#gopi-ui-state-compare surface=%s state=%s dump=%s session=%s %s transcript_lines=%d outcome=%s match=%t\n",
		rec.Surface, rec.State, rec.DumpPath, rec.Session, uiStateSessionProvenance(rec.SessionFile), rec.TranscriptLines, rec.Outcome, rec.Match)
	for _, v := range rec.Verdicts {
		// BOTH sides are printed: the DIRECTION of a mismatch is the diagnosis,
		// and a reader who sees only one of them cannot tell "the paint lost it"
		// from "this is not this session's text".
		fmt.Printf("  %s grid_row=%d in_grid=%t in_transcript=%t damage=%s display_hits=%d geometry=%s cols=%d rows=%d frames=%d prefix=%d",
			v.Status, v.GridRow, v.InGrid, v.InTranscript, v.Damage, v.DisplayHits, v.Geometry, v.Cols, v.Rows, v.Frames, v.GridPrefixRunes)
		if v.Reason != "" {
			fmt.Printf(" reason=%q", v.Reason)
		}
		fmt.Println()
	}
}
