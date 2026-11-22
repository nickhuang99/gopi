// push_target_acceptance.go — is the declared delivery point a repository that
// will actually ACCEPT the branch this run pushes?
//
// THE DEFECT THIS ANSWERS (measured on hp, task 01a1df53, 2026-11-15 22:11:12):
// the clone channel read `dev_repo = /home/user/work/gopi` from the settings
// document, cloned it, built in the clone, and only then pushed — and the far
// end answered
//
//	! [remote rejected]     HEAD -> master (branch is currently checked out)
//	error: failed to push some refs to '/home/user/work/gopi'
//
// because on that host `dev_repo` names the host's OWN non-bare working tree
// with `master` checked out and `receive.denyCurrentBranch` at its default
// (`refuse`). Nothing in the channel looked at the destination before the push
// that was already most of the way through. The same declaration is HARMLESS on
// a host that sets `receive.denyCurrentBranch = updateInstead` (peer1 does), so
// the answer is not "this path is wrong" — it is a question about the DESTINATION'S
// git configuration, and it can be asked before the first clone.
//
// WHAT THIS IS NOT: a refusal. A delivery point that would reject the push is a
// configuration fact, and the fix may be on either side (declare a bare/peer
// destination, or set `updateInstead` there). So this reports the measured shape
// BY NAME, at the moment the channel is turned on, and the run keeps going: the
// measurement exists to be READ, not to take the decision away (PROJECT.md Rule
// 15 — a refusal must name its cause; Rule 35 — no new persistent dependency, so
// this is a probe on an existing path, never a hook, cron or daemon).
//
// THE PROBE IS BEHAVIOURAL, NOT A STRING TEST (Rule 31). It asks the destination
// repository itself, with `git rev-parse --is-bare-repository`, `git symbolic-ref`
// and `git config --get receive.denyCurrentBranch`. `git push --dry-run` is NOT
// usable here and is named so a later reader does not reach for it: measured on
// hp, `git push --dry-run /home/user/work/gopi HEAD:master` printed
// "Everything up-to-date" and never exercised `denyCurrentBranch` at all, because
// the objects were already there. A dry run is not evidence (see the epic).
package devworkspace

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// The names this probe reads and the values it distinguishes. Rule 1/13/14: the
// git spellings are taken from constants rather than typed inline, so the
// command this package says it ran and the command it did run cannot drift.
const (
	// gitRevParseVerb is declared once in ground_rebase.go and REFERENCED here
	// (Rule 13/14: a shared spelling is not re-typed). isBareRepoFlag answers
	// "is this destination a working tree or a bare repository" — the first half
	// of the shape.
	isBareRepoFlag = "--is-bare-repository"
	// gitSymbolicRefVerb / headShortFlag / headRefName answer "which branch is
	// checked out there" — the second half.
	gitSymbolicRefVerb = "symbolic-ref"
	headShortFlag      = "--short"
	headRefName        = "HEAD"
	// gitConfigVerb / gitConfigGetFlag / denyCurrentBranchKey ask the ONE
	// setting that decides whether a push to a checked-out branch is accepted.
	gitConfigVerb        = "config"
	gitConfigGetFlag     = "--get"
	denyCurrentBranchKey = "receive.denyCurrentBranch"

	// The four values `receive.denyCurrentBranch` may hold. Anything the
	// destination does not SET reads as GitDenyCurrentBranchRefuse, because that
	// is git's own default when the key is absent.
	GitDenyCurrentBranchRefuse        = "refuse"
	GitDenyCurrentBranchIgnore        = "ignore"
	GitDenyCurrentBranchWarn          = "warn"
	GitDenyCurrentBranchUpdateInstead = "updateInstead"

	// sshVerb and its two options: the probe must never block on a credential
	// prompt (a peer that asks is a configuration fact, not a hang).
	sshVerb              = "ssh"
	sshBatchModeOption   = "-o"
	sshBatchModeValue    = "BatchMode=yes"
	sshConnectTimeoutKey = "ConnectTimeout="
	// remoteProbeConnectSec is the ssh connect budget in seconds, and
	// remoteProbeTotalTimeout is the whole probe's budget: a peer that is simply
	// not there must not hold the dispatch up. Both are NAMED (Rule 1) rather
	// than appearing as literals at the call site.
	remoteProbeConnectSec   = 3
	remoteProbeTotalTimeout = 6 * time.Second

	// devRepoAliasSeparator is the `<alias>:` of a `peer1:/path` declaration.
	devRepoAliasSeparator = ":"
	// devRepoUnixPathPrefix distinguishes an absolute POSIX path from an alias
	// prefix: `/home/...` starts with `/`, an alias does not. A value carrying
	// `://` is an ordinary URL and is not split at all.
	devRepoUnixPathPrefix = "/"
	devRepoSchemeMarker   = "://"
)

// PushTargetAcceptance is the MEASURED answer to "will this delivery point accept
// the push the channel is about to make".
type PushTargetAcceptance struct {
	// Probed is false when the destination could not be asked at all (a peer
	// that did not answer, a path that is not a repository). An unasked question
	// is never reported as a clean bill of health (Rule 15).
	Probed bool
	// IsBare: a bare repository accepts any branch at any time, so it is never
	// the misdeclared shape.
	IsBare bool
	// CheckedOutBranch is the branch HEAD points at in the destination, empty
	// when HEAD is detached or the destination is bare.
	CheckedOutBranch string
	// DenyCurrentBranch is the destination's own value, or
	// GitDenyCurrentBranchRefuse when the key is absent (git's default).
	DenyCurrentBranch string
	// RejectsPush is THE VERDICT: the destination is a non-bare tree with the
	// very branch this run pushes checked out, and its configuration refuses
	// that push. This is exactly the shape that produced the far-end rejection.
	RejectsPush bool
	// Note is the measured detail in one sentence, always filled when Probed,
	// so a reader sees the values the verdict was derived from.
	Note string
}

// PushTargetWarning is the NAMED report a run emits at dispatch time when the
// declared delivery point would reject its own push. It is a sentence a human
// reads in the run log, so it states the measured values, the consequence the
// far end produced the last time, and both ways out.
func PushTargetWarning(devRepo string, res PushTargetAcceptance) string {
	return fmt.Sprintf(
		"clone channel: the declared delivery point %s will REJECT this run's push — "+
			"it is a non-bare working tree with %q checked out and "+
			"`%s` set to %q (git's default is %q). Pushing a checked-out branch to a "+
			"non-bare repository is refused by git itself at the far end with "+
			"`! [remote rejected] HEAD -> %s (branch is currently checked out)`, "+
			"which arrives AFTER the clone, the pull and the build, and which a "+
			"delivery leg cannot win by retrying. Two ways out, both configuration: "+
			"declare a destination that is bare or is not checked out on %s, or set "+
			"`%s = %s` in the destination's own configuration (that is what makes the "+
			"same declaration work on a host configured to receive). %s",
		devRepo, res.CheckedOutBranch, denyCurrentBranchKey, res.DenyCurrentBranch,
		GitDenyCurrentBranchRefuse, DefaultBranch, DefaultBranch,
		denyCurrentBranchKey, GitDenyCurrentBranchUpdateInstead, res.Note)
}

// ProbePushTargetAcceptance asks the DELIVERY POINT ITSELF whether it would accept
// a push of branch `branch`. `devRepo` is the declaration exactly as the settings
// document carries it — either a local path or `<alias>:<path>`.
//
// It never fails: an unanswerable destination comes back with Probed=false and the
// reason in Note, because "I could not ask" is a different finding from "it will
// refuse" and swallowing that difference is the silent failure this whole epic is
// about (Rule 15).
func ProbePushTargetAcceptance(ctx context.Context, devRepo, branch string) PushTargetAcceptance {
	var res PushTargetAcceptance
	devRepo = strings.TrimSpace(devRepo)
	if devRepo == "" {
		res.Note = "no delivery point is declared, so there is nothing to ask"
		return res
	}
	if branch == "" {
		branch = DefaultBranch
	}

	// Where to run the probe, and the directory to ask about inside it.
	probeDir, repoDir := probeLocation(devRepo)

	probeCtx, cancel := context.WithTimeout(ctx, remoteProbeTotalTimeout)
	defer cancel()

	// ① bare or working tree?
	if isBare, ok := probeGitBool(probeCtx, probeDir, repoDir, isBareRepoFlag); ok {
		res.IsBare = isBare
	} else {
		res.Note = fmt.Sprintf(
			"the delivery point %s could not be asked whether it is a bare repository "+
				"(the probe did not answer), so this run has NOT established that it "+
				"would accept the push", devRepo)
		return res
	}
	if res.IsBare {
		res.Probed = true
		res.Note = fmt.Sprintf("the delivery point %s is a bare repository, which accepts %s at any time",
			devRepo, branch)
		return res
	}

	// ② which branch is checked out there?
	checkedOut, headOK := probeGitString(probeCtx, probeDir, repoDir,
		gitSymbolicRefVerb, headShortFlag, headRefName)
	res.CheckedOutBranch = strings.TrimSpace(checkedOut)
	if !headOK || res.CheckedOutBranch == "" {
		res.Note = fmt.Sprintf(
			"the delivery point %s is a non-bare repository but its HEAD could not be read "+
				"(detached, or the probe did not answer), so this run has NOT established "+
				"which branch it has checked out", devRepo)
		return res
	}

	// ③ the destination's own setting; absent means git's default.
	deny, denyOK := probeGitString(probeCtx, probeDir, repoDir,
		gitConfigVerb, gitConfigGetFlag, denyCurrentBranchKey)
	res.DenyCurrentBranch = strings.TrimSpace(deny)
	if !denyOK || res.DenyCurrentBranch == "" {
		res.DenyCurrentBranch = GitDenyCurrentBranchRefuse
	}
	res.Probed = true

	// THE VERDICT. All three facts are required, and each is measured above:
	// non-bare AND the pushed branch is the one checked out AND the configured
	// behaviour is one that refuses.
	refuses := res.DenyCurrentBranch == GitDenyCurrentBranchRefuse
	res.RejectsPush = refuses && res.CheckedOutBranch == branch
	res.Note = fmt.Sprintf(
		"measured at %s: bare=%t, %s checked out=%q, `%s`=%q",
		devRepo, res.IsBare, headRefName, res.CheckedOutBranch,
		denyCurrentBranchKey, res.DenyCurrentBranch)
	return res
}

// probeLocation splits a delivery-point declaration into (where the git command
// runs, which directory it asks about).
//
//   - ""                  → the local repository named by the path, asked in place
//   - "<alias>:<path>"    → run on <alias> over ssh, asking about <path>
//
// A URL (`ssh://…`, `https://…`) is answered as the local case with the probe
// running nowhere useful, which surfaces as Probed=false — an honest "not asked"
// rather than a guess about a remote host we have no shell on.
func probeLocation(devRepo string) (probeDir, repoDir string) {
	if !strings.Contains(devRepo, devRepoSchemeMarker) {
		if idx := strings.Index(devRepo, devRepoAliasSeparator); idx > 0 {
			alias := devRepo[:idx]
			rest := devRepo[idx+1:]
			// An absolute path never contains the separator before its first
			// slash, so this only splits on a real `<alias>:` prefix.
			if alias != "" && !strings.HasPrefix(alias, devRepoUnixPathPrefix) {
				return alias, rest
			}
		}
	}
	return "", devRepo
}

// probeGitString runs one git query against the destination and returns its
// trimmed stdout. ok=false means the probe could not answer, which callers must
// treat as UNKNOWN and never as a negative (Rule 15).
func probeGitString(ctx context.Context, probeDir, repoDir string, args ...string) (string, bool) {
	out, err := probeGit(ctx, probeDir, repoDir, args...)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(out), true
}

// probeGitBool is probeGitString for the yes/no questions, spelled separately so
// a caller cannot accidentally read "" as false.
func probeGitBool(ctx context.Context, probeDir, repoDir string, flag string) (bool, bool) {
	out, ok := probeGitString(ctx, probeDir, repoDir, gitRevParseVerb, flag)
	if !ok {
		return false, false
	}
	switch strings.TrimSpace(out) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// probeGit is the ONE place the probe becomes a command. A local destination is
// asked in place; a remote one is asked through ssh with BatchMode and a connect
// budget, so a peer that wants a password is a reported fact, not a stall.
func probeGit(ctx context.Context, probeDir, repoDir string, args ...string) (string, error) {
	gitArgs := append([]string{"-C", repoDir}, args...)
	if probeDir == "" {
		return run(ctx, "", "git", gitArgs...)
	}
	sshArgs := []string{
		sshBatchModeOption, sshBatchModeValue,
		sshBatchModeOption, sshConnectTimeoutKey + fmt.Sprint(remoteProbeConnectSec),
		probeDir,
		"git",
	}
	sshArgs = append(sshArgs, gitArgs...)
	return run(ctx, "", sshVerb, sshArgs...)
}
