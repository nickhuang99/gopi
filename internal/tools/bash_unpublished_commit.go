package tools

// bash_unpublished_commit.go — THE UNDEFINED STEP: what becomes of the commit
// that is left behind when a publish is correctly refused.
//
// ── THE DEFECT THIS FILE CLOSES (measured 2026-10-24, revision d50f746d7) ────
//
// Three gates were each individually RIGHT and their sum lost a commit:
//
//	① Rule 37 says a chat does not commit — but the 2026-10-24 operator ruling
//	  lets a chat edit, stage and commit. So the commit was made, legally, in
//	  the MAIN working area.
//	② The REF MOVE guard correctly refused the chat's `git push` (a ref move is
//	  the operator's decision, never a posture's). Correct, and it left the
//	  commit with no destination. (Since the operator's 2026-11-03 ruling —
//	  "push 都可以", row 01a1a786 — `git push` is a REPOSITORY WRITE and that
//	  particular refusal no longer happens; the notice is about a commit no
//	  remote-tracking ref reaches, whatever the reason.)
//	③ `make build` correctly built HEAD — and HEAD was that local commit. So the
//	  binary installed at /usr/local/bin/gopi carried `revision d50f746d7`,
//	  a commit that existed on NO server.
//
// Nothing in that chain is broken. The missing piece is the step nothing
// defined: AFTER a publish is refused, the local commit stays in a branch that
// no remote-tracking ref reaches, and NOTHING SAYS SO. The run ends believing
// it delivered; the machine ships a binary built from work no reviewer can see;
// the fact is discovered (if ever) by a human comparing revisions by hand.
//
// ── SO THIS NAMES IT, AT THE MOMENT IT HAPPENS ──────────────────────────────
//
// A commit is created THROUGH this tool (`git commit -m "<subject>"` is what
// every run does — the same seam git_commit_task_trailer.go uses, for the same
// reason: it is the ONE place in the process that sees the command). Right after
// such a command the tool now measures whether HEAD is reachable from ANY
// remote-tracking ref, and when it is not, it prints the sha, the branch, the
// ahead count and the two routes out of the state. The design rules:
//
//	· IT NAMES THE COMMIT (sha + branch + count), never "some commits are
//	  unpushed": a reader who cannot copy an identifier cannot act (Rule 15).
//	· IT GIVES A PUBLISH PATH — `gopi task submit` (the worker commits in its
//	  own ground and the clone channel's delivery loop publishes it) or the
//	  operator decides where the commit goes. A dead end stated without an exit
//	  is a report, not a fix.
//	· IT PUSHES NOTHING AND MOVES NO REF. This file runs two READ-ONLY git
//	  probes and renders a string; publishing is the caller's own next step.
//	· IT GOES INTO THE TOOL RESULT, not into the parent's stderr. The result is
//	  the channel that reaches BOTH audiences: it is returned to the model that
//	  ran the command (so the run can act on it in the same turn) and written
//	  into that run's session JSONL (so the fact survives the context — a notice
//	  on stderr is visible to a human watching a terminal and to nobody else).
//
// ── WHERE IT IS GATED, AND WHY THE GATE IS THE CHAT IDENTITY ───────────────
//
// The condition below is "a commit exists that no remote-tracking ref reaches".
// That is TRUE for a worker's ground too, for the minutes between its commit and
// the clone channel's delivery push — and there the sentence "this checkout has
// no publication path" would be FALSE: the channel's delivery loop is exactly
// that path (Rule 6c). Printing a false fact is worse than printing nothing, so
// the notice is gated on the surface for which the claim is true: a CHILD OF A
// CHAT (`config.ChatSurfaceIdentityEnv`, published by `gopi chat` into its own
// environment, inherited by the `bash` tool's shell). A chat owns no branch, no
// ground and no delivery loop (Rule 37), so a commit standing in one really has
// nowhere to go — and the gate costs nothing: worker children do not carry the
// variable at all.
//
// A malformed identity reads as "no chat ancestor" rather than as a refusal:
// the variable AUTHORISES nothing (config.ReadChatSurfaceIdentity's contract),
// and a run that cannot prove it is a chat child is treated as one that is not.
// That is the safe direction — the notice is a provenance claim, and this file
// never invents one.

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// unpublishedRevListVerb / …CountFlag / …NotFlag / …RemotesFlag / …HeadArg
	// are git's own spelling for the ONE question this file asks: how many
	// commits does HEAD carry that no ref under refs/remotes/ carries. The verb
	// and the count flag are referenced from the place that already owns them
	// (Rule 13/14: no second definition of a shared spelling); the two flags and
	// the pseudo-ref are this file's own and are named here so the probe, the
	// sentence quoted in the notice, and the test's assertion cannot drift.
	unpublishedRevListVerb = integrationClaimRevListVerb
	unpublishedCountFlag   = integrationClaimCountFlag
	unpublishedNotFlag     = "--not"
	unpublishedRemotesFlag = "--remotes"
	unpublishedHeadArg     = "HEAD"

	// unpublishedRevParseVerb resolves the commit this notice NAMES. It is the
	// guard family's rev-parse verb — the same one currentGitBranch uses — so the
	// sha the notice prints and the branch it prints come from one git.
	unpublishedRevParseVerb = integrationClaimRevParseVerb

	// unpublishedDetachedBranchLabel is what the notice prints when the checkout
	// is not on a branch. A detached HEAD commit is the WORST case of this
	// defect (a real object, no branch, no reflog anybody reads), so it is
	// reported rather than skipped — with the state named, never with an empty
	// slot a reader would take for "unknown branch".
	unpublishedDetachedBranchLabel = "(detached HEAD)"

	// unpublishedNothingPushedClaim is the sentence that keeps the notice from
	// being read as "a push failed": nothing in this mechanism moves a ref, and a
	// reader who believes otherwise goes looking for a delivery that cannot exist.
	// It is a constant because the format and the test that asserts on it must
	// agree — two spellings would let the assertion pass over a changed sentence.
	unpublishedNothingPushedClaim = "NOTHING WAS PUSHED"

	// unpublishedNoticeFmt is the notice. Its slots, in order: the commit's full
	// sha, the branch (or unpublishedDetachedBranchLabel), the exact command whose
	// answer made the claim, that answer, the directory it was answered in, and
	// the sha again as the thing to hand to the operator.
	//
	// THE COMMAND AND ITS ANSWER ARE INSIDE THE SENTENCE (Rule 15 + Rule 41): the
	// reader must be able to re-run the measurement rather than trust a
	// conclusion, and a claim with no command behind it is the shape this
	// repository has paid for more than once.
	unpublishedNoticeFmt = "\n[unpublished commit] %s on branch %s is reachable from NO remote-tracking ref " +
		"(`%s` answered %d in %s), so no remote-tracking ref reaches it and this remote has no publication path " +
		"for it yet. " + unpublishedNothingPushedClaim + " — the commit stays local until one of these runs:\n" +
		"  - publish it from here: `git push` is a REPOSITORY WRITE since the operator's 2026-11-03 ruling " +
		"(row 01a1a786), so the console may run it against a remote it names;\n" +
		"  - file the work: `gopi task submit \"<what this commit does>\"` — its worker commits in its own " +
		"ground and the clone channel's delivery loop publishes it;\n" +
		"  - hand the sha to the operator and let them decide where it goes: %s\n"
)

// unpublishedCommitHit reports whether a command contains a `git commit` clause
// that creates a commit. It asks gitCommitInsertionPoints — the ONE scanner the
// trailer seam already uses — so a command this tool would add a trailer to is
// exactly a command this notice examines, and the two cannot disagree about what
// "a commit happened here" looks like. An `--amend` clause is skipped by that
// scanner (it rewrites a commit rather than making one), which is also right
// here: an amended commit was already named when it was first created.
func unpublishedCommitHit(command string) bool {
	return len(gitCommitInsertionPoints(command)) > 0
}

// unpublishedCommitProbe is the ONE measurement: how many commits does dir's HEAD
// carry that no remote-tracking ref carries.
//
// ok=false means the question was NOT answered (git refused, the directory is not
// a checkout, the count was not a number). The caller prints nothing then: an
// unmeasured question is never a fact (Rule 15), and a notice invented from a
// failed read would be exactly the "I saw it" claim this repository has been
// burned by. The probe is read-only and writes nothing.
func unpublishedCommitProbe(dir string) (ahead int, ok bool) {
	out, err := gitProbeOutput(dir, unpublishedRevListVerb, unpublishedCountFlag,
		unpublishedHeadArg, unpublishedNotFlag, unpublishedRemotesFlag)
	if err != nil {
		return 0, false
	}
	n, perr := strconv.Atoi(strings.TrimSpace(out))
	if perr != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// unpublishedProbeCommand is the command text the notice quotes, built from the
// same constants the probe runs, so a reader who copies it re-runs exactly the
// measurement that produced the sentence.
func unpublishedProbeCommand() string {
	return unpublishedRevListVerb + " " + unpublishedCountFlag + " " + unpublishedHeadArg + " " +
		unpublishedNotFlag + " " + unpublishedRemotesFlag
}

// unpublishedCommitNotice returns the notice for a command that has just run, or
// "" when it does not apply. Its four preconditions are each a fact about a
// DIFFERENT thing, which is why they are separate tests and not one:
//
//	the command SUCCEEDED     — a failed `git commit` created nothing to name;
//	the command MADE a commit — a read-only command is not evidence of anything;
//	the run is a CHAT CHILD   — the only surface with no publication path;
//	HEAD is UNREACHABLE       — the measurement above, and the whole point.
func unpublishedCommitNotice(command, workdir string, exitCode int) string {
	if exitCode != 0 || !unpublishedCommitHit(command) {
		return ""
	}
	if id, err := config.ReadChatSurfaceIdentity(); err != nil || id.Empty() {
		return ""
	}
	dir := workdir
	if strings.TrimSpace(dir) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return ""
		}
		dir = cwd
	}
	ahead, ok := unpublishedCommitProbe(dir)
	if !ok || ahead <= 0 {
		return ""
	}
	sha, err := gitProbeOutput(dir, unpublishedRevParseVerb, unpublishedHeadArg)
	if err != nil {
		return ""
	}
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return ""
	}
	// A branch that cannot be read is REPORTED as the detached label, never
	// dropped: the notice's job is to name the state a reader must act on, and
	// "unknown branch" would leave the one slot blank that says where the commit
	// lives. The reader's error is recorded with it (Rule 15b): a probe that
	// failed and a checkout that is genuinely detached are different facts, and
	// the message says which one it saw.
	branch, berr := currentGitBranch(dir)
	if berr != nil {
		branch = unpublishedDetachedBranchLabel + ": " + berr.Error()
	}
	if branch == "" {
		branch = unpublishedDetachedBranchLabel
	}
	return fmt.Sprintf(unpublishedNoticeFmt, sha, branch, unpublishedProbeCommand(), ahead, dir, sha)
}
