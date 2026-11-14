package main

// ui_state_self.go — HOW `gopi ui_state` claims the caller's OWN screen.
//
// THE PROBLEM THE GENERAL RULE CANNOT SOLVE. `gopi ui_state --target chat` run
// from a bash child inherits an environment, two pids and a working directory —
// and NOT one of those said which of the profile's chat surfaces the caller
// belonged to. So the verb asked the only question it could (uiclient.SelectLive:
// the one live surface, else a refusal) and, on the ordinary case of a chat run
// beside the agent's own, refused — correctly and uselessly: the reader holds a
// fact the scan cannot derive, and nothing carried it across the process
// boundary.
//
// THE CHANNEL, AND WHY IT IS THE ENVIRONMENT. The chat process writes its own
// identity (`<pid>:<session-directory>`) into ITS environment at arm time
// (internal/config.ChatSurfaceIdentityEnv; cmd/agent/chat_surface_identity.go),
// and `exec(2)` carries the environment to every child for free — including the
// `bash` tool's shell, the process that runs THIS verb (internal/tools/bash.go
// builds the child env from os.Environ() and strips only the wrapper-child
// identity names, which this is not). Nothing is created, found, aged or
// reclaimed: the fact lives exactly as long as the process it describes, which is
// Rule 35's "prefer the process tree's own channel over a new artifact".
//
// PRECEDENCE, AND WHAT A MISS MEANS. `--pid` still wins outright (the caller
// naming a process is the unambiguous route), then a `--session` with a check to
// run (the caller naming a session), then THIS run's own published identity, and
// only then the general rule. The claim is verified on the artifact side before
// it is believed: `SelectBySelf` (internal/uiclient) matches it against dumps that
// have ALREADY proved their own (pid, start-ticks) identity against /proc, and a
// claim that matches nothing while several artifacts are on hand is a REFUSAL
// naming the claim and the candidates — never a fallback to "the one I found"
// (Rule 15: a silent substitution would hand the caller somebody else's screen
// under the name of its own).

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// uiStateClaimNoticeFormat is the sentence a successful claim prints beside the
// artifact's own header, on stdout with the observation it qualifies: the caller
// must be able to see WHICH rule chose the dump and by which half it matched,
// because "the verb read a dump" and "the verb read MY dump" are the two
// different facts this whole file exists to make distinguishable.
const uiStateClaimNoticeFormat = "#gopi-ui-state-claim by=%s %s dump=%s"

// uiStateClaimNotice is the same fact when the claim was used and fell back to
// the only artifact on hand: the claim matched nothing, and the read is
// nonetheless determinate (there was nothing to confuse it with). Said out loud,
// because a reader that believed the artifact was claimed by identity when it was
// claimed by scarcity would overstate the evidence.
const uiStateClaimNotice = "#gopi-ui-state-claim own-identity matched no artifact; this is the only one on hand (%s)"

// uiStateClaimUnusableFormat is the sentence for a published identity that cannot
// be read as the declared shape. It is printed, and the read then proceeds under
// the general rule: a channel whose garbage read as "no claim" would make every
// defect in it invisible (Rule 15), while refusing to read a screen at all because
// an unrelated variable is malformed would be worse.
const uiStateClaimUnusableFormat = "gopi %s: cannot read this run's own surface identity: %v (falling back to the general rule: the one live surface, else a refusal)"
const uiStateClaimUnusablePrefix = "own-identity unusable: "

// uiStateSelfIdentity assembles the caller's own surface claim: the published
// identity when there is one, else the identity this process can state about
// itself (its own pid, and the session directory of a session path it holds).
//
// fallbackSessionPath is the caller's OWN session record when it has one — the
// chat process itself passes the file it is writing, which is exactly the fact a
// child could not know. It is a fallback and not the primary, because the
// published value is the one that survives the process boundary this verb exists
// to cross.
//
// An error here is the MALFORMED case, and it is returned rather than swallowed:
// the identity is unusable, the caller must say so, and the read then proceeds
// under the general rule. An EMPTY identity is not an error — a caller with no
// chat ancestor has nothing to claim with, which is the state every caller was in
// before this channel existed.
func uiStateSelfIdentity(fallbackSessionPath string) (uiclient.SelfIdentity, error) {
	published, err := config.ReadChatSurfaceIdentity()
	if err != nil {
		return uiclient.SelfIdentity{}, err
	}
	if !published.Empty() {
		return uiclient.SelfIdentity{PID: published.PID, SessionDir: published.SessionDir}, nil
	}
	if dir := strings.TrimSpace(sessionlayout.RecordContainer(fallbackSessionPath)); fallbackSessionPath != "" && dir != "." {
		return uiclient.SelfIdentity{PID: os.Getpid(), SessionDir: dir}, nil
	}
	return uiclient.SelfIdentity{}, nil
}

// uiStateSelection is the verb's whole answer to "which artifact did you read":
// the selection, the refusal when there is one, and the sentence that says HOW
// the artifact was claimed (empty when no claim applied at all).
type uiStateSelection struct {
	// Sel is the chosen artifact (or the sets a refusal carries).
	Sel uiclient.Selection
	// Err is the refusal, if any.
	Err error
	// ClaimNote is the claim sentence, printed BESIDE the observation on the
	// success path and on stderr for the arms where the claim was unusable or
	// missed.
	ClaimNote string
}

// uiStateSelectOwn applies the caller's own claim, falling back to the general
// rule exactly when there is nothing to claim with. The three arms it can answer
// with are uiclient.SelectBySelf's, and its contract (see that function) is what
// keeps a claim from ever becoming a silent substitution:
//
//	matched one        -> that artifact, with the matching half named;
//	matched several    -> refusal (two of the caller's own artifacts are two
//	                      screens it cannot choose between);
//	matched none, one  -> that artifact, said as "the only one on hand";
//	matched none, many -> refusal naming the claim, the pids, and `--pid`;
//	no claim           -> the general rule, untouched.
func uiStateSelectOwn(cands []uiclient.Candidate, claim uiclient.SelfIdentity, truncated bool) uiStateSelection {
	if claim.Empty() {
		sel, err := uiclient.SelectLive(cands, truncated)
		return uiStateSelection{Sel: sel, Err: err}
	}
	sel, available, err := uiclient.SelectBySelf(cands, claim, truncated)
	if !available {
		// SelectBySelf reports the rule as NOT APPLIED only when there is no
		// claim; the caller asked it with a non-empty one, so this is
		// unreachable — and if it ever is reached, the general rule is the safe
		// answer rather than a second guess at what "unavailable" meant.
		fallback, ferr := uiclient.SelectLive(cands, truncated)
		return uiStateSelection{Sel: fallback, Err: ferr}
	}
	note := ""
	switch {
	case err == nil:
		by := uiclient.SelfMatchBy(sel.Candidate, claim)
		if by == "" {
			// The candidate was selected as the ONLY one on hand. Saying which
			// arm answered is the point: the evidence is scarcity, not identity.
			note = fmt.Sprintf(uiStateClaimNotice, claim.String())
		} else {
			note = fmt.Sprintf(uiStateClaimNoticeFormat, by, claim.String(), sel.Candidate.Path)
		}
	case uiclient.SelfIdentityMiss(err), isAmbiguity(err):
		// The refusal carries the facts (the claim, the pids, the remedy); the
		// note would repeat them, so it stays empty and the refusal is rendered
		// by the caller as the refusal it is.
		note = ""
	}
	return uiStateSelection{Sel: sel, Err: err, ClaimNote: note}
}

// isAmbiguity reports whether err is the multi-candidate refusal — the shape that
// already lists its members and its remedy, and therefore needs no extra note.
func isAmbiguity(err error) bool {
	var ambiguous *uiclient.AmbiguityError
	return errors.As(err, &ambiguous)
}
