package tools

// chat_ground.go — A CHAT SESSION DOES NOT MINT A GROUND; IT REPORTS WHERE IT
// IS WRITING (epic chat-session-ground-retired-when-not-a-row, 2026-09-29).
//
// WHAT THIS FILE USED TO DO, and why it no longer does. Until 2026-09-27 a chat
// session MINTED its own linked worktree — `git worktree add <root>/gopi-<stem>
// -b chat/<stem>` — and chdir'd into it, so its writes would not land
// uncommitted in the repository's main checkout. The operator retired that
// decision: a chat is an interactive foreground session and, in this file's own
// words from the beginning, "a chat is not a row". A ground that is in no
// ledger row is a ground no arm can collect — finish-cleanup is keyed on a row's
// deliverable, scratch-gc requires a dead-holder row, and the worktree sweep's
// gates are process-global and all-or-nothing (see
// devdoc/chat-session-ground-lifecycle-diagnosis.md). Measured on this host: the
// ONLY non-master branches a chat left behind were exactly these `chat/*` refs,
// 0 commits ahead, no holder — residue with a producer and no collector.
//
// So the CREATION PATH IS GONE (this file), and its absence is not a silent
// one: the session still says, by name, WHERE ITS WRITES WILL LAND. That is the
// whole remaining contract, and it is the contract this file now implements:
//
//   - RESOLVE-LESS. No branch is named, no stem is derived, no path under a
//     worktree root is computed, `git` is not invoked at all. Nothing is
//     created, so nothing can be left behind — the residue cannot recur because
//     the producer is gone, which is a stronger guarantee than any collector
//     (Rule 35: fix the thing, do not wrap it).
//
//   - IT STILL REPORTS. Silence is the defect (Rule 15): an interactive session
//     whose writes land in a repository's main checkout must SAY so, in one
//     line, at start-up. The line names the directory the session stands in and
//     says whose directory it is — the main checkout of a repository, or a
//     directory that is not one. The operator then knows, before the first
//     turn, that this is a plain working directory and not a branch of its own.
//
//   - IT STILL DEGRADES RATHER THAN REFUSES. Unchanged from the epic this
//     supersedes: a chat is a human's session and must start wherever the
//     caller stands. "Degraded" now means "this run could not even determine
//     what directory it is in" — the one shape in which the session cannot say
//     where its writes land. Everything else is a RESOLVED report.
//
// WHAT IS DELIBERATELY NOT HERE: no deletion, no commit, no ledger write, and
// now no creation either. Nothing to sweep, nothing to register.
//
// Historic texts the rest of the tree used to read are NOT kept "for
// compatibility": a branch namespace nothing mints is a claim about code that no
// longer exists. They are deleted with the code that used them, and every reader
// is updated in the same commit — with ONE exception, ChatGroundBranchPrefix,
// which survives as the record of the namespace a SIBLING task must recognise
// when it reclaims the refs earlier gopi versions left on this host.
//
//   - THE OTHER HALF OF THE DECISION LANDED ON MASTER, SEPARATELY (2026-09-29).
//     An INERT chat ground left by an OLD gopi (clean, nothing its session
//     committed, session over) is now collected by the sweeper that already
//     ticks — `worktree_sweep_chat.go`, the ladder's ④C rung — while an
//     uncommitted one is kept by the dirty guard all the same (`worktreeSweepJudge`
//     guard ④: "dirty (uncommitted work) — left in place"). Both halves live in
//     the EXISTING sweeper: no new reaper, no new state file, no new config key
//     (Rule 35). THIS file mints nothing for that sweeper to find, which is the
//     stronger half of the same decision: the residue cannot recur.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/replaymode"
)

// ChatGroundBranchPrefix is RETIRED, and this declaration is the record of that
// (not a live namespace). It is kept for exactly one reason: a sibling task must
// RECLAIM the existing `chat/*` refs a previous gopi left on this machine
// (epic chat-session-ground-lifecycle-diagnosis §"recommendation"), and the
// collector's test needs the namespace the residue carries. Nothing mints it.
// When the last such ref is gone from every host, this constant goes too.
const ChatGroundBranchPrefix = "chat/"

// ChatGroundResolution is what a chat session's ground resolved to — now
// WITHOUT a ground: the session reports the directory it stands in, and the
// session has no worktree and no branch of its own.
type ChatGroundResolution struct {
	// Dir is the directory the session is standing in — the directory its
	// writes land in. It is the caller's own directory, ALWAYS: nothing moves
	// the session (the field is kept because the caller and the notices name
	// the same value, and because a resolution that names no directory and one
	// that names the caller's are different facts).
	Dir string
	// Repo is the repository whose MAIN CHECKOUT the directory belongs to (the
	// top level as git reports it), or "" when the directory is not inside a
	// git work tree. Reported so the operator line can say WHICH checkout the
	// writes are landing in.
	Repo string
	// InMainCheckout is true when Dir is the main checkout (or a subdirectory
	// of it) of Repo — the shape the retired epic was built for, and the shape
	// the operator line calls out loud.
	InMainCheckout bool
	// Degraded is true when the resolution could not even determine the
	// directory the session stands in. It is the flag the caller branches on —
	// Detail is the text it prints.
	Degraded bool
	// ReadOnly is true when this run re-runs a recording (`gopi chat --replay`),
	// i.e. when the session performs NO write of its own: internal/replaymode
	// refuses the session mint, the slot registration, the editor history and
	// every tool round's effect. It is REPORTED here so the start-up line can
	// stop claiming a write behaviour this run does not have (Rule 15: a run
	// must never state a fact about itself that the code contradicts) — the
	// directory is still named, because it is still the directory the session
	// stands in and reads from.
	ReadOnly bool
	// Detail is the one-line human record of what happened (Rule 15 — the
	// decision is never invisible).
	Detail string
}

// ChatGroundFor reports where a chat session with sessionID will write. dir is
// the directory the caller was started in — the process cwd a `gopi chat`
// inherits, passed explicitly so the resolution is a pure function of its
// arguments and a test never has to chdir to exercise it.
//
// IT NEVER CREATES ANYTHING AND NEVER RETURNS AN ERROR. There is no ground to
// make and no shape that could justify refusing a human's session (this epic's
// superseded rule, kept): every outcome is a resolution the caller acts on —
// `Degraded == false` (run where you are; say Detail) or `Degraded == true`
// (the directory could not be determined at all; say Detail).
//
// sessionID is accepted and deliberately UNUSED: it named the ground's stem and
// the branch, and both are gone. The parameter stays in the signature because
// the caller has it, the reader of a call site should see that the session
// identity no longer decides anything here, and because a future reader asking
// "does a chat still get a per-session path?" must be able to answer it from the
// call site alone. It must NOT be used to name anything.
//
// THE WRITE FACT IS DECIDED HERE, AND THAT IS THE 2026-11-04 CORRECTION. A
// `gopi chat --replay <id>` performs NO write — internal/replaymode refuses the
// session mint, the slot registration, the editor history and every tool round's
// effect — so a resolution that ChatGroundAnnouncement renders as a WRITE claim
// ("this session writes in … every round lands uncommitted") states a behaviour
// that run does not have. The flag is therefore asked INSIDE the producer rather
// than at the caller: a caller-side `replaymode.Active()` that set this field
// would be a bypass, and every OTHER caller of this function would go on
// printing the write claim for a replay (measured: the caller and the producer
// are different files, and the planner's whole point was that the producer must
// carry the fact). internal/replaymode imports nothing but stdlib, so there is
// no cycle. ONE place asks the flag, every caller gets the right resolution.
func ChatGroundFor(sessionID, dir string) ChatGroundResolution {
	g := chatGroundResolve(dir)
	g.ReadOnly = replaymode.Active()
	return g
}

// chatGroundResolve is the resolution itself, WITHOUT the replay flag: every
// branch below is a pure read of the filesystem (the directory, its work-tree
// root, whether that root is the shared checkout). ChatGroundFor adds the ONE
// run-scoped fact on top, so no branch here can disagree with another about it.
func chatGroundResolve(dir string) ChatGroundResolution {
	base := strings.TrimSpace(dir)
	if base == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return chatGroundDegrade(fmt.Sprintf(chatGroundNoDirDetailFmt, err))
		}
		base = cwd
	}
	repo, err := repoRootOf(base)
	if err != nil || strings.TrimSpace(repo) == "" {
		// NOT A GIT WORK TREE — an ordinary, documented outcome, and never a
		// reason to refuse: a chat needs no repository to be useful. The line
		// says plainly that there is no checkout here to write into.
		return ChatGroundResolution{
			Dir:    base,
			Detail: fmt.Sprintf(chatGroundNotRepoDetailFmt, base),
		}
	}
	repo = filepath.Clean(strings.TrimSpace(repo))
	// WHICH CHECKOUT: base's own work-tree root (above) is where the session
	// stands; `taskGroundMainCheckout` answers the different question the notice
	// is about — is that root the repository's SHARED checkout, or a linked
	// worktree somebody took deliberately? Without it, a chat started in a
	// linked worktree would be told it is writing in a main checkout, which is
	// the opposite of true and the one claim this line must never get wrong. It
	// delegates to the sweeper's own resolver (one implementation, Rules 13/14);
	// an unresolvable answer falls back to base's own root — the conservative
	// reading, in which the session is told it writes in the checkout it stands
	// in, which it does.
	main := taskGroundMainCheckout(repo)
	if strings.TrimSpace(main) == "" {
		main = repo
	}
	main = filepath.Clean(strings.TrimSpace(main))
	// In the main checkout OR under it: both are "the shared checkout's working
	// area", which is the fact the operator must hear (a subdirectory is not a
	// worktree; the retired defect was exactly as live one level down).
	if SameDir(base, main) || pathWithin(base, main) {
		return ChatGroundResolution{
			Dir:            base,
			Repo:           main,
			InMainCheckout: true,
			Detail:         fmt.Sprintf(chatGroundMainCheckoutDetailFmt, base, main),
		}
	}
	// A directory that is not the main checkout: a linked worktree (the operator
	// already put this session somewhere deliberate) or a nested repo. Either way
	// the session stands where it stands — nothing is minted, nothing is moved.
	return ChatGroundResolution{
		Dir:    base,
		Repo:   main,
		Detail: fmt.Sprintf(chatGroundNotMainCheckoutDetailFmt, base, main),
	}
}

// chatGroundDegrade builds the "I cannot even say where you are" result. One
// constructor, so Degraded and Detail can never disagree (Rules 13/14).
func chatGroundDegrade(detail string) ChatGroundResolution {
	return ChatGroundResolution{Degraded: true, Detail: detail}
}

// ChatGroundAnnouncement renders the line a chat session prints at start-up: the
// directory its writes will land in, and whose directory that is. It is the
// surviving half of the retired epic's announcement (ChatGroundAnnouncement,
// "this session's worktree %s (branch %s)") and exists for the same reason: the
// operator is TOLD where the session writes, never left to infer it from a later
// `git status`. "" for a degraded resolution — the degradation has its own line.
//
// A READ-ONLY resolution (a replay) is delegated to ChatGroundReadOnlyAnnouncement:
// this function's three sentences all state a WRITE, and a replay performs none.
func ChatGroundAnnouncement(g ChatGroundResolution) string {
	if strings.TrimSpace(g.Dir) == "" || g.Degraded {
		return ""
	}
	if g.ReadOnly {
		return ChatGroundReadOnlyAnnouncement(g)
	}
	if g.InMainCheckout {
		return fmt.Sprintf(chatGroundMainCheckoutNoticeFmt, g.Dir, g.Repo)
	}
	if strings.TrimSpace(g.Repo) == "" {
		return fmt.Sprintf(chatGroundNoCheckoutNoticeFmt, g.Dir)
	}
	return fmt.Sprintf(chatGroundNotMainCheckoutNoticeFmt, g.Dir, g.Repo)
}

// ChatGroundReadOnlyAnnouncement renders the start-up line for a run that will
// NOT perform the writes ChatGroundAnnouncement describes: a replay. It exists
// as its own function (rather than three more format strings inside
// ChatGroundAnnouncement) so the two families are separable at the call site and
// in a test — "this run writes" and "this run writes nothing" are opposite
// statements, and a reader comparing them should see them as such (Rule 15).
//
// It keeps every TRUE fact the ordinary line carries: the directory, the
// checkout when there is one, and the absence of a worktree and a branch. What it
// drops is the write claim, which under a replay the code contradicts
// (internal/replaymode; the refusal catalogue is printed beside this line).
func ChatGroundReadOnlyAnnouncement(g ChatGroundResolution) string {
	if strings.TrimSpace(g.Dir) == "" || g.Degraded {
		return ""
	}
	if g.InMainCheckout {
		return fmt.Sprintf(chatGroundReadOnlyCheckoutNoticeFmt, g.Dir, g.Repo)
	}
	if strings.TrimSpace(g.Repo) == "" {
		return fmt.Sprintf(chatGroundReadOnlyNoticeFmt, g.Dir)
	}
	return fmt.Sprintf(chatGroundReadOnlyWorktreeNoticeFmt, g.Dir, g.Repo)
}

// ChatGroundDegradedNotice renders the line a chat session prints when it could
// not determine the directory it is writing in. It is the degraded half of
// ChatGroundAnnouncement and exists for the same reason (Rule 15). "" for a
// resolved session, so a caller may print both without deciding which it has.
func ChatGroundDegradedNotice(g ChatGroundResolution) string {
	if !g.Degraded {
		return ""
	}
	return fmt.Sprintf(chatGroundDegradedNoticeFmt, g.Detail)
}

// ChatGroundDetailDegradedLabel is the leading half of every degradation
// Detail, spelled once so a caller (and a test, in either package) can tell a
// degraded resolution from a resolved one by reading the line — the two facts
// are also carried by the Degraded flag, and this constant is what keeps a
// human-facing record of the same decision legible.
const ChatGroundDetailDegradedLabel = "no ground: "

// The one-line texts. Each is declared once and referenced by the producer, the
// caller and the tests (Rules 1/13/14: a mirrored literal is the drift these
// rules exist to stop).
const (
	chatGroundDegradedNoticeFmt = "[gopi] chat: %s\n"

	chatGroundMainCheckoutNoticeFmt    = "[gopi] chat: this session writes in %s — the MAIN CHECKOUT of %s. No worktree and no branch of its own: every read/write/edit/bash round lands uncommitted in that checkout's working area.\n"
	chatGroundNotMainCheckoutNoticeFmt = "[gopi] chat: this session writes in %s — a worktree of %s that is not its main checkout. No worktree and no branch of its own: the session stands exactly where it was started.\n"
	chatGroundNoCheckoutNoticeFmt      = "[gopi] chat: this session writes in %s — no git checkout here, so there is no branch to attribute the work to. No worktree of its own.\n"

	// THE READ-ONLY LINE (`gopi chat --replay`). It is a THIRD family, not a
	// variant of the three above, because those three all state a WRITE — "this
	// session writes in", "lands uncommitted", "there is no branch to attribute
	// the work to" — and a replay performs no write at all (internal/replaymode
	// refuses the session mint, the slot registration, the editor history and
	// every tool round's effect, and the refusal catalogue is printed beside
	// this line). Stating a write under a replay is a FACT ERROR, not a
	// stylistic one, which is why the fix is a different sentence rather than
	// softer wording on the same one: there is nothing here to hedge — the
	// writes DO happen in an ordinary chat and DO NOT happen in a replay.
	//
	// It still NAMES THE DIRECTORY, and it still names the checkout when there
	// is one, because those facts remain true and remain useful: the replay reads
	// from where it stands, and an operator who wants to know which tree this
	// recording is being re-run against has no other line that says it.
	chatGroundReadOnlyNoticeFmt         = "[gopi] chat: this session writes NOTHING — a REPLAY re-runs a recording read-only, so it stands in %s without writing to it.\n"
	chatGroundReadOnlyCheckoutNoticeFmt = "[gopi] chat: this session writes NOTHING in %s — a REPLAY re-runs a recording read-only. No worktree and no branch of its own: it stands exactly where it was started, in the MAIN CHECKOUT of %s, and leaves that checkout untouched.\n"
	chatGroundReadOnlyWorktreeNoticeFmt = "[gopi] chat: this session writes NOTHING in %s — a REPLAY re-runs a recording read-only. No worktree and no branch of its own: it stands exactly where it was started, a worktree of %s that is not its main checkout, and leaves it untouched.\n"

	chatGroundNoDirDetailFmt           = "no ground: the starting directory could not be resolved (%v), so gopi cannot say where this session's writes will land"
	chatGroundNotRepoDetailFmt         = "no ground: %s is not inside a git work tree, so there is no checkout to name — this session writes to its own directory"
	chatGroundMainCheckoutDetailFmt    = "no ground: %s is the main checkout (or under it) of %s — this session writes there, with no worktree and no branch of its own; take a worktree yourself if you want a branch to attribute the work to"
	chatGroundNotMainCheckoutDetailFmt = "no ground: %s is not the main checkout of %s, so this session writes exactly where it stands"
)
