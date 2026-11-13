package uiclient

// selectself.go — "WHICH OF THESE ARTIFACTS IS MINE?", asked from inside a run.
//
// THE QUESTION THE GENERAL RULE CANNOT ANSWER. `SelectLive` answers "which
// surface is the target" for a caller who has NOTHING but the candidates (a
// verb invoked by hand, a scan of the host): it picks the one live surface and
// refuses on an ambiguity, because between two live surfaces it has no ground to
// prefer either. A run asking about ITS OWN screen is in a different position —
// it holds a fact no scan can derive: WHICH SURFACE IT IS. Its own side of that
// fact is a pid and the session directory it writes (published to its children
// by internal/config's run-scoped identity), and the artifacts carry exactly the
// same two facts in their provenance record and in their location.
//
// WHY THIS IS NOT "PICK THE BEST MATCH". A claim that matches nothing must never
// resolve to the nearest candidate: a reader that fell back to SelectLive after
// a miss would hand the caller SOMEBODY ELSE'S screen while the caller believed
// it was reading its own, which is the defect this whole capability exists to
// make impossible. So the arms are:
//
//   - a candidate the caller IS          -> it (deterministic, verified);
//   - a claim that matches nothing, with
//     ONE candidate on hand              -> that candidate (nothing to confuse it
//     with: the artifact is the only screen
//     there is);
//   - a claim that matches nothing, with
//     SEVERAL on hand                    -> REFUSAL naming the claim and the
//     candidates, so the caller learns the pid and
//     passes --pid (Rule 15: an unproven claim is
//     a fact to report, never a licence to guess).
//
// The candidate-side facts are the ones the artifact already proved about itself
// (dumpIdentityVerdict): a "match by pid" is a pid the dump's own record names
// AND a live process the (pid, start-ticks) check agreed with, so a recycled pid
// cannot be claimed by a stale identity, and "match by session directory" is the
// location the artifact was READ from (SessionDirOfDump), not a number parsed
// out of its contents.

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// SelfIdentity is the caller's own side of the question: which surface process it
// is, and which session directory that surface writes into. Both halves are
// optional and independently useful — a caller that knows only its pid still
// identifies the artifact its own process wrote, and one that knows only its
// session directory identifies the artifact of its own run.
//
// The struct is spelled here rather than re-using internal/config's
// ChatSurfaceIdentity so this package keeps its leaf position (Rule 14: the
// constants package must stay importable without cycles, and the reader of a
// channel is not the same thing as the shape of a claim). cmd/agent maps one to
// the other; the two halves are strings and an int, so the mapping cannot drift.
type SelfIdentity struct {
	// PID is the surface process the caller claims to be, 0 when it claims none.
	PID int
	// SessionDir is the session directory the caller's dump is written into, ""
	// when it claims none. It is compared against SessionDirOfDump, the SAME
	// derivation the artifact side uses.
	SessionDir string
}

// Empty reports that the caller claims nothing at all. A caller in that state has
// no self to claim with, so its reader must use the general rule — and say so
// rather than pretending the absence of a claim is a claim that failed.
func (id SelfIdentity) Empty() bool { return id.PID <= 0 && id.SessionDir == "" }

// String renders the claim for a report or a refusal: the raw halves, so a
// reader can compare them with the artifact's own `pid=` field by eye.
func (id SelfIdentity) String() string {
	pid := "pid=?"
	if id.PID > 0 {
		pid = fmt.Sprintf("pid=%d", id.PID)
	}
	dir := "session=?"
	if id.SessionDir != "" {
		dir = "session=" + id.SessionDir
	}
	return pid + " " + dir
}

// SelfMatchBy reports which half of a claim a candidate answers, for a report or
// a refusal that must say HOW it was identified rather than only that it was:
// "pid", "session", "pid+session", or "" when the candidate answers neither. A
// reader that matched on the session directory alone is in a different position
// from one that matched the writer's pid, and only the second can quote a
// `pid=` line as its own.
func SelfMatchBy(cand Candidate, id SelfIdentity) string {
	m := selfIdentityMatches(cand, id)
	switch {
	case m.byPID && m.byDir:
		return selfMatchByBoth
	case m.byPID:
		return selfMatchByPID
	case m.byDir:
		return selfMatchBySession
	default:
		return ""
	}
}

// The vocabulary of SelfMatchBy's answer (Rule 1b: the caller branches on these,
// never on a typed-in string).
const (
	selfMatchByPID     = "pid"
	selfMatchBySession = "session"
	selfMatchByBoth    = "pid+session"
)

// selfIdentityMatch reports HOW a candidate answers the claim: whether it matched
// at all, and by which half — because a report that said "matched" without saying
// by what would leave the caller unable to tell a pid identity from a session
// one.
type selfIdentityMatch struct {
	matched bool
	byPID   bool
	byDir   bool
}

// selfIdentityMatches compares ONE candidate against the caller's claim. A pid
// claim matches on the dump record's pid, a directory claim on the directory the
// artifact was read from (SessionDirOfDump: the walk that resolves an artifact
// back to the session that owns it, so the two sides cannot disagree about what
// "the session directory" is).
func selfIdentityMatches(cand Candidate, id SelfIdentity) selfIdentityMatch {
	m := selfIdentityMatch{}
	if id.PID > 0 && cand.Record.PID == id.PID {
		m.byPID = true
	}
	if id.SessionDir != "" {
		if dir, ok := SessionDirOfDump(cand.Path); ok && samePath(dir, id.SessionDir) {
			m.byDir = true
		}
	}
	m.matched = m.byPID || m.byDir
	return m
}

// SelectBySelf claims the caller's OWN artifact from a candidate scan. It is the
// rule a run uses for its own screen (and the CLI verb uses first, before falling
// back to SelectLive), and its contract is:
//
//	claim matches exactly one   -> it;
//	claim matches several       -> *AmbiguityError* over those (two artifacts a
//	                               caller's own claim covers are two screens it
//	                               cannot choose between, and a silent pick there
//	                               is the defect, whatever the claim says);
//	claim matches none,
//	  one candidate on hand     -> that candidate (it is the only screen; this is
//	                               the same arm SelectBySession takes, and it is
//	                               what keeps a run whose identity could not be
//	                               published working exactly as before);
//	claim matches none,
//	  several on hand           -> *SelfIdentityMissError* (a refusal that names
//	                               the claim, the candidates' pids and the way to
//	                               disambiguate);
//	no candidates               -> ErrNoCandidates;
//	no claim at all             -> the caller has nothing to claim with; this
//	                               function reports *unavailable* and the caller
//	                               MUST fall back to SelectLive (never silently to
//	                               a candidate).
//
// The `available` return value is that last distinction made explicit: it says
// whether this rule APPLIED, so a caller can tell "the claim was used and
// refused" from "there was no claim to use". A rule that returned only an error
// would let a caller treat "nothing to claim with" as a refusal and stop reading
// a screen it could have read.
func SelectBySelf(cands []Candidate, id SelfIdentity, truncated bool) (sel Selection, available bool, err error) {
	sel = Selection{All: cands, Truncated: truncated}
	if id.Empty() {
		return sel, false, nil
	}
	if len(cands) == 0 {
		return sel, true, ErrNoCandidates
	}
	var hits []Candidate
	for _, c := range cands {
		if selfIdentityMatches(c, id).matched {
			hits = append(hits, c)
		}
	}
	switch {
	case len(hits) == 1:
		sel.Candidate = hits[0]
		return sel, true, nil
	case len(hits) > 1:
		sel.Ambiguous = hits
		return sel, true, &AmbiguityError{Candidates: hits}
	case len(cands) == 1:
		sel.Candidate = cands[0]
		return sel, true, nil
	}
	sel.Ambiguous = cands
	return sel, true, &SelfIdentityMissError{Identity: id, Candidates: cands}
}

// SelfIdentityMissError is the refusal for a claim that matched nothing while
// several artifacts were on hand. It is its own type rather than a formatted
// error so the ONE predicate below can recognise it without matching prose (the
// same reason NoDumpForSessionError exists), and it carries the candidates
// because a caller told only "your claim matched nothing" cannot act, while a
// caller told the pids CAN: it passes --pid.
type SelfIdentityMissError struct {
	// Identity is the claim that did not hold, rendered by SelfIdentity.String.
	Identity SelfIdentity
	// Candidates are the artifacts that were on hand.
	Candidates []Candidate
}

func (e *SelfIdentityMissError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "this run's own surface identity (%s) matches NONE of the %d %s artifacts on hand, and %d candidates are too many to pick from safely — refusing to read a screen that is not this run's (pass --pid to choose):",
		e.Identity, len(e.Candidates), knownDumpFileList(), len(e.Candidates))
	for _, c := range e.Candidates {
		fmt.Fprintf(&b, "\n  pid=%d target=%s started=%s dump=%s",
			c.Record.PID, c.Record.Target, c.Record.Started.UTC().Format(time.RFC3339), c.Path)
	}
	return b.String()
}

// SelfIdentityMiss reports whether err is that refusal. It exists so a caller
// can render the right ROUTE OUT (this one's remedy is `--pid`, an absence's is
// arming a dump) without matching on the sentence — the same division
// DumpAbsent makes for the absence shapes.
func SelfIdentityMiss(err error) bool {
	if err == nil {
		return false
	}
	var miss *SelfIdentityMissError
	return errors.As(err, &miss)
}
