package tools

// chat_cleanup_authorization.go — the CHANNEL through which a chat may run the
// DESTRUCTIVE half of the repository's own cleanup, and the reason it cannot
// become an always-open switch.
//
// THE GAP THIS CLOSES. The boundary's rule is "a chat commits not, and deletes
// nothing that belongs to another run" (Rule 37). But a chat IS where an operator
// notices that a finished row's ground is still on disk, so the boundary must
// have a way to let the console COLLECT it — otherwise the operator's only route
// is a shell outside the console, which is exactly the "invisible hand" Rule 35
// refuses. The repository already owns that capability twice (Rule 3/35: no new
// cleaner, no new script):
//
//	gopi task sweep-worktrees   — reclaim stale worktree registrations
//
// and it is PLAN-BY-DEFAULT: measured in its own parser, `dryRunGate()` is
// `!apply || dryRun`, so a run with no `--apply` reports and removes nothing.
// `--apply` is the destructive opt-in, and it is what this file gates.
//
// (A second face, `gopi task finish-cleanup`, sat in this list until worktree
// retirement phase 2 deleted the verb; the enumeration below is now the ONE
// source both the prompt and the refusals read.)
//
// WHERE THE AUTHORIZATION COMES FROM, AND WHY IT IS THE OPERATOR'S OWN WORDS.
// The channel is not a flag, not an environment variable and not a setting: it is
// the CURRENT TURN'S USER MESSAGE, tested by ChatTurnAuthorizesCleanup. Three
// properties fall out, and each is the reason the other designs were rejected:
//
//   - It cannot become an always-on switch. It is not stored ANYWHERE: the guard
//     re-derives it from the turn's own text on every call (internal/agent holds
//     the turn's input for the length of one turn and overwrites it on the next),
//     so there is no state to leave enabled, no key to forget, and a turn that
//     says nothing about cleanup authorizes nothing.
//   - A prior turn's authorization cannot leak into a later one: each turn
//     carries its own message, and the message is the input.
//   - A setting or an env var WOULD be a persistent dependency (Rule 35) created
//     to make one sentence unnecessary — the operator writes the sentence instead.
//
// THE PRIMARY TEACHER IS STILL THE PROMPT (internal/agent/chat_write_boundary.go),
// which tells the model to reach these faces and to run the destructive half only
// when the operator asked for cleanup; this file is the SECOND gate, and it is
// deliberately crude in the same way a seatbelt is: it reads the operator's
// vocabulary and refuses the destructive flag when no such word is present. Its
// known limits are stated rather than hidden (Rule 15): a negation ("do not clean
// up") is not parsed semantically — a message carrying a negation marker is
// required to name the FACE ITSELF before its phrases count — and a paraphrase
// outside the vocabulary is a false negative that leaves the operator able to
// authorize on the next turn by naming the verb, which is why the refusal prints
// that exact spelling.

import "strings"

// chatCleanupPhrases are the operator's own words for the act — the vocabulary
// that authorizes the destructive half of the cleanup faces. Both languages the
// console is used in, because the message is the operator's and the operator
// writes either.
var chatCleanupPhrases = []string{
	"cleanup", "clean up", "clean-up", "reclaim", "sweep",
	"清理", "回收", "清掉", "收掉", "收拾",
}

// chatCleanupNegations are the markers that flip a PHRASE into a refusal: a
// message that says "do not clean up" must not authorize cleanup merely by
// containing the word. When one is present, only an explicit naming of the FACE
// (its own invocation, or its verb with the apply flag) authorizes.
var chatCleanupNegations = []string{
	"don't", "do not", "dont", "never", "不要", "别", "禁止", "不准",
}

// ChatTurnAuthorizesCleanup reports whether THIS turn's user message authorized
// the destructive half of the cleanup faces. The message is the operator's own
// words, and it is passed in rather than read from anywhere: a function of its
// input cannot hold a switch open.
func ChatTurnAuthorizesCleanup(userInput string) bool {
	msg := strings.ToLower(userInput)
	if msg == "" {
		return false
	}
	// Naming a face and its destructive flag IS the authorization, negation or
	// not ("run finish-cleanup --apply for t-123").
	if chatCleanupFaceNamed(msg) && strings.Contains(msg, ChatTaskApplyFlag) {
		return true
	}
	if chatHasAny(msg, chatCleanupNegations) {
		return false
	}
	return chatHasAny(msg, chatCleanupPhrases) || chatCleanupFaceNamed(msg)
}

// chatCleanupFaceNamed reports whether the message names one of the cleanup faces
// — its verb (`finish-cleanup`, `sweep-worktrees`) or its invocation.
func chatCleanupFaceNamed(msg string) bool {
	for _, verb := range chatCleanupFaceVerbs() {
		if strings.Contains(msg, verb) {
			return true
		}
	}
	return false
}

// chatCleanupFaceVerbs is the verbs of the faces, from the ONE list that defines
// them (chatCleanupFaces) — never re-typed here (Rule 13/14).
func chatCleanupFaceVerbs() []string {
	faces := chatCleanupFaces()
	out := make([]string, 0, len(faces))
	for _, f := range faces {
		if i := strings.LastIndexByte(f.invocation, ' '); i >= 0 {
			out = append(out, f.invocation[i+1:])
		}
	}
	return out
}

// chatHasAny reports whether any of the needles occurs in the (lowercased) msg.
func chatHasAny(msg string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(msg, n) {
			return true
		}
	}
	return false
}

// chatCleanupApplyRequest is ONE clause's request to run a cleanup face's
// destructive half: the face's invocation, and whether the clause carried the
// apply flag.
type chatCleanupApplyRequest struct {
	face string
}

// chatCleanupApplyRequests reads the cleanup faces out of a parsed command line:
// for every clause that invokes a face WITH the apply flag, the face's invocation
// is reported. The bash spelling alone is read (the guard judges the command the
// model is about to run), and only the face's own words count — a clause that
// merely MENTIONS the verb in an argument is not a request.
func chatCleanupApplyRequests(command string) []chatCleanupApplyRequest {
	words := chatShellTokens(command)
	var out []chatCleanupApplyRequest
	for _, clause := range chatShellClauses(words) {
		for len(clause) > 0 && envAssignPattern.MatchString(clause[0]) {
			clause = clause[1:]
		}
		for _, face := range chatCleanupFaces() {
			if !chatClauseRunsFace(clause, face) {
				continue
			}
			if chatClauseHasWord(clause, face.applyFlag) {
				out = append(out, chatCleanupApplyRequest{face: face.invocation})
			}
		}
	}
	return out
}

// chatClauseRunsFace reports whether the clause's first words ARE the face's
// invocation (`gopi task finish-cleanup …`, with or without a path spelling).
func chatClauseRunsFace(clause []string, face chatCleanupFace) bool {
	inv := strings.Fields(face.invocation)
	if len(clause) < len(inv) {
		return false
	}
	for i := range inv {
		if i == 0 {
			// The program word may be spelled with a path (`./bin/gopi`).
			if clause[i] != inv[i] && !strings.HasSuffix(clause[i], "/"+inv[i]) {
				return false
			}
			continue
		}
		if clause[i] != inv[i] {
			return false
		}
	}
	return true
}

// chatClauseHasWord reports whether the clause carries the word exactly (an
// option with a value of its own is a different word: `--apply=1` is not
// `--apply`, and the parser's own bare-flag reading is what decides it — a value
// the flag does not take is not an apply).
func chatClauseHasWord(clause []string, word string) bool {
	for _, w := range clause {
		if w == word {
			return true
		}
	}
	return false
}

// chatCleanupApplyRefusal renders the refusal for a cleanup face's destructive
// half when this turn's message did not authorize it. It names the face, the
// reason the gate exists, the PLAN it can run instead, and the words that open
// the gate — so the model's next move is a sentence to the operator rather than a
// retry.
func chatCleanupApplyRefusal(face string) string {
	return chatWriteBoundaryRefusalMarker + " (`" + face + " " + ChatTaskApplyFlag + "`): the destructive " +
		"half of this face REMOVES a run's worktree and deletes its branch, and a chat may run it only when the " +
		"OPERATOR asked for cleanup in THIS turn's own message — the authorization is that message, never a stored " +
		"switch, so it cannot be left open.\nDO THIS INSTEAD: run the face WITHOUT `" + ChatTaskApplyFlag + "` — a " +
		"bare run is a PLAN that reports what it would reclaim and removes nothing — then show the operator the plan " +
		"and ask whether to apply it (their next message saying so opens the gate for exactly that turn)."
}
