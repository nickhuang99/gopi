package tools

// git_publish_target_guard.go — refuse a hand-rolled `git push` from a
// CLONE-CHANNEL ground to any ref OTHER than the branch that ground stands on.
//
// ── THE INCIDENT (measured 2026-10-21 on peerbox, rows 01a15b70-a6aa…,
// 01a15b70-c35f…, 01a15b2c-…, 01a15b11-…) ─────────────────────────────────────
//
// The clone channel's contract is: the run works in a clone at
// <workspace_root>/<task-id>, stands on the INTEGRATION branch (what it cloned —
// `master`), and the channel's own delivery loop (clean → pull --rebase →
// declared build → push, `internal/devworkspace`) is the ONLY writer that puts
// its commits on the server. Nothing in that loop names a branch: it pushes
// `HEAD:<the branch the ground stands on>`.
//
// Four runs, however, ended with a ref the channel never creates:
//
//	peerbox:/home/user/work/gopi
//	  refs/heads/feat/reopen-carries-evidence             71ea4f7  @05:16:55 push
//	  refs/heads/task/01a15b2c-7096-72c7-82b4-c64e1096914d efaa891 @07:11:22 push
//	  refs/heads/task/01a15b70-a6aa-7ad2-b7d4-9171a4bc8abd 197c951  @08:23:03 push
//	  refs/heads/task/01a15b70-c35f-7c7c-bd07-aad6c74cb70b 3e355d1  @09:25:18 push
//
// `git reflog show` on each of the four carries exactly ONE entry, `push`, on
// the same second the run's own bash tool logged
//
//	cd /home/user/workspace/01a15b70-a6aa-… && git push origin HEAD:refs/heads/task/01a15b70-a6aa-…
//
// and the run's reasoning is in the same log: "Better: push to a named branch,
// e.g. `git push origin HEAD:refs/heads/task/…`" — the agent applied PROJECT.md
// Rule 6c (written for a `/tmp` clone that has no channel) to a channel whose
// delivery loop had ALREADY pushed its commits to the integration branch. The
// clerk that records `deliverable.branch` was right all along: those rows read
// `master`, and master had in fact advanced (efaa891 is an ancestor of `master`
// and appears in its reflog as `push`) — the stray refs were pure residue that
// no merge path consumes and no reclaim path collects.
//
// ── WHY A REFUSAL, AND WHY THIS NARROW ONE ───────────────────────────────────
//
// The push that helps is the one whose DESTINATION is the ground's own branch:
// it is bounded by the branch the channel already publishes, so it cannot mint
// a ref. The push that harms is every other destination — `HEAD:<some name>`
// CREATES a ref on the server (the operator has to delete those by hand, and a
// ref deletion is operator-only), and `HEAD:<integration>` from a checkout that
// is not the integration branch MOVES the server's integration branch
// (`receive.denyCurrentBranch=updateInstead` on the non-bare server checkout
// checks it out on the spot). Both are the same defect: a destination chosen by
// the run instead of by the channel.
//
// The scope is deliberately one channel and one verb. A run in the WORKTREE
// channel is unaffected (its ground is a linked worktree, and Rule 6a/6c's
// `git push origin feat/<name>` remains the correct route there), a command with
// no announced ground is unaffected, and every `git push origin` that names no
// refspec is unaffected (its destination is the caller's own upstream).
//
// The escape hatch is the one this file family already shares
// (`GOPI_ALLOW_DANGEROUS=1`, read by the caller): an operator who has decided
// to write a ref by hand is taking the blast radius deliberately.

import (
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/devworkspace"
)

const (
	// gitPublishVerb is git's ref-publishing verb, taken from the value
	// internal/taskverbs owns for the same word (Rule 13: the literal has ONE
	// home in this package family — `refGuardFetchVerb`'s sibling).
	gitPublishVerb = "push"

	// gitPublishRefspecSep is git's `<src>:<dst>` separator (the value
	// refGuardRefSep already names for the refspec grammar).
	gitPublishRefspecSep = refGuardRefSep

	// gitPublishForcePrefix is a refspec's force marker (`+<src>:<dst>`); it
	// says how a destination is written, never WHICH destination, so it is
	// stripped before the destination is judged.
	gitPublishForcePrefix = "+"

	// gitPublishHeadsPrefix is the fully-qualified head namespace. A refspec
	// destination may be written either way (`master` / `refs/heads/master`),
	// and git resolves both to the same ref — so the guard does too.
	gitPublishHeadsPrefix = refsHeadsPrefix

	// gitPublishAllRefsFlag is git's "push every matching ref" (a destination
	// nobody decided), and gitPublishMirrorFlag mirrors the whole ref space;
	// neither names a single ref, so neither is judged here. They are named so
	// the no-weakening cases read as decisions rather than as omissions — the
	// parser already skips every option word, so a case for one of them proves
	// that skipping, not a branch of its own.
	gitPublishAllRefsFlag = "--all"
	gitPublishMirrorFlag  = "--mirror"

	// gitPublishDeleteFlag / gitPublishDeleteLongFlag spell the deletion form,
	// in which the refspec's DESTINATION half is empty (`:refs/heads/x`): a
	// delete writes no new ref and is left to the operator's own decision (an
	// archive/removal audit, Rule 9).
	gitPublishDeleteFlag     = "-d"
	gitPublishDeleteLongFlag = "--delete"

	// gitPublishOptionOperandFlags are push's options that consume the NEXT
	// word, so that word is an operand and never a refspec.
	gitPublishOptionOperandO  = "-o"
	gitPublishOptionOperandO2 = "--push-option"
	gitPublishRepoOption      = "--repo"
	gitPublishReceivePackFlag = "--receive-pack"
	gitPublishExecFlag        = "--exec"
)

// gitPublishIsDryRun lives in git_push_writes.go: it is the ONE definition of
// "does this push write nothing" (Rule 13/14). It had a second reader until
// 2026-11-03 — the chat op-category criterion — which the operator's ruling on
// `git push` (row 01a1a786) removed; this guard is its only caller now, and the
// declaration stays in its own file so a future reader is not surprised by the
// constant block moving.

// gitPublishOptionOperands is the lookup form of the operand-taking options
// above (a map literal is the only shape a `switch` over five names improves
// on, and Rule 1b wants the values named once).
var gitPublishOptionOperands = map[string]bool{
	gitPublishOptionOperandO:  true,
	gitPublishOptionOperandO2: true,
	gitPublishRepoOption:      true,
	gitPublishReceivePackFlag: true,
	gitPublishExecFlag:        true,
}

// gitPublishRefspecs returns the refspecs a `git push` clause names: the
// positional words AFTER the remote, with options (and their operands) skipped.
//
// A word carrying no `:` names no DESTINATION — it means "push this ref to its
// configured upstream", a destination the caller did not choose — so it is not
// returned. Neither is a deletion refspec (empty destination): a delete creates
// no ref.
func gitPublishRefspecs(argv []string) []string {
	var out []string
	positional := 0
	for i := 0; i < len(argv); i++ {
		w := argv[i]
		if gitPublishOptionOperands[w] {
			i++ // the operand is not a refspec
			continue
		}
		if strings.HasPrefix(w, "-") {
			continue
		}
		positional++
		if positional == 1 {
			continue // the remote
		}
		out = append(out, w)
	}
	return out
}

// gitPublishDestination normalizes a refspec to the fully-qualified ref it
// writes, and reports whether the refspec writes a single local branch ref at
// all (an empty destination is a delete; a glob writes a family nobody picked).
func gitPublishDestination(refspec string) (string, bool) {
	rs := strings.TrimPrefix(refspec, gitPublishForcePrefix)
	src := rs
	dst := ""
	if i := strings.Index(rs, gitPublishRefspecSep); i >= 0 {
		src, dst = rs[:i], rs[i+1:]
	}
	// `:<ref>` is git's DELETION spelling (empty source): it writes no ref, and
	// removing a ref on the server is an operator's decision (Rule 9's one
	// archive writer), not something this guard judges.
	if dst == "" || src == "" {
		return "", false
	}
	if strings.Contains(dst, refGuardRefspecGlob) || strings.Contains(src, refGuardRefspecGlob) {
		return "", false
	}
	if strings.HasPrefix(dst, gitPublishHeadsPrefix) {
		return dst, true
	}
	// A destination in the DWIM form (`master`, `task/x`): git resolves it
	// under refs/heads, so the guard judges the same ref git would write.
	if strings.HasPrefix(dst, refGuardRefsRootPrefix) {
		return dst, true
	}
	return gitPublishHeadsPrefix + dst, true
}

// gitPublishGroundBranch answers "which branch does the announced ground stand
// on?" and whether this process is in a CLONE-CHANNEL ground at all.
//
// The announcement is the runner's own (`TaskGroundDirEnv` /
// `TaskGroundBranchEnv`, clamped at spawn by the spawn seam): an inherited or
// absent pair means "no ground of mine", and the guard then has nothing to say.
// The CLONE discriminator is the one `isGitWorkTree`-family code already uses —
// a clone's `.git` is a DIRECTORY, a linked worktree's is a FILE — so the
// worktree channel's pushes are never reached by this guard.
func gitPublishGroundBranch() (branch string, inCloneChannel bool) {
	dir := strings.TrimSpace(os.Getenv(TaskGroundDirEnv))
	if dir == "" {
		return "", false
	}
	// THE ONE DISCRIMINATOR (Rules 13/14): `devworkspace.GroundModelOf` decides
	// the shape, so the guard, the delivery-proof arm and the briefing prose
	// cannot disagree about which ground a run is standing in.
	if devworkspace.GroundModelOf(dir) != devworkspace.GroundModelClone {
		return "", false
	}
	branch = strings.TrimSpace(os.Getenv(TaskGroundBranchEnv))
	if branch == "" {
		// The announcement is published at spawn; a caller that did not carry
		// it (a hand-run gopi, a resumed shell) still names a ground whose
		// branch is readable. An unreadable one is NOT guessed: the caller
		// allows the push rather than refusing on a value it invented.
		if b, err := currentGitBranch(dir); err == nil {
			branch = b
		}
	}
	return branch, true
}

// guardCloneGroundPublishTarget refuses a `git push` from a clone-channel
// ground whose destination is not the branch that ground stands on.
//
// It returns the refusal sentence, or "" when nothing is refused. Refusals are
// WORDED, never silent (Rule 15): the sentence names the destination, the ground
// branch, why the destination is harmful, and the route that is correct.
func guardCloneGroundPublishTarget(command, workdir string) string {
	groundBranch, inCloneChannel := gitPublishGroundBranch()
	if !inCloneChannel || groundBranch == "" {
		return ""
	}
	groundRef, _ := gitPublishDestination(groundBranch + gitPublishRefspecSep + groundBranch)
	// SEGMENTS, not clauses: `git push` inside a nesting construct (`OUT=$(git
	// push …)`, `bash -c 'git push …'`) is a simple command the shell will run,
	// and a clause-only walk never sees it — the same parse-layer blind spot the
	// branch-create guard was measured on (ref_guard_shell_nesting.go).
	for _, clause := range refGuardClauses(command, workdir) {
		for _, seg := range refGuardClauseSegments(clause) {
			if reason := gitPublishSegmentRefusal(seg, workdir, groundRef); reason != "" {
				return reason
			}
		}
	}
	return ""
}

// gitPublishSegmentRefusal judges ONE simple command for the publish guard.
func gitPublishSegmentRefusal(seg refGuardClauseSegment, workdir, groundRef string) string {
	clause := refGuardClause{runDir: seg.runDir, words: refGuardUnwrapShellHead(seg.words)}
	verb, rest, ok := refGuardGitSubcommand(clause.words)
	if !ok || verb != gitPublishVerb {
		return ""
	}
	// A DRY RUN writes no ref, so there is no destination to judge — and this
	// is the command Rule 6c tells a run to use to CHECK its addressing
	// (gitPublishIsDryRun carries the reasoning). Judged per clause, on the
	// clause's OWN words, so `git push --dry-run … && git push …` still
	// refuses the second clause.
	if gitPublishIsDryRun(clause.words) {
		return ""
	}
	for _, refspec := range gitPublishRefspecs(rest) {
		dest, judged := gitPublishDestination(refspec)
		if !judged || dest == groundRef {
			continue
		}
		return "a clone-channel run publishes its work through the channel, not through its own push: " +
			"`" + refspec + "` would write " + dest + " on the server, but this run's ground stands on " +
			groundRef + ", which is the ONLY destination the channel's delivery loop publishes (clean, " +
			"pull --rebase, declared build, push of `HEAD:<the ground's branch>`, run once from this run's " +
			"end hook). A destination other than the ground's branch CREATES a ref nothing " +
			"merges (an operator must then delete it by hand) and, when it names the integration branch " +
			"from another checkout, MOVES the server's integration branch. Nothing was pushed. If the " +
			"commit is not on the server yet, re-run the row's delivery (the channel re-pulls, rebuilds " +
			"and re-pushes its own branch) and report a push that is still refused in the run's own " +
			"output; content that must land on another ref is an OPERATOR decision " +
			"(GOPI_ALLOW_DANGEROUS=1 opens this guard deliberately)."
	}
	return ""
}

// currentGitBranch reads the branch a directory stands on, through the same
// `git rev-parse --abbrev-ref HEAD` form the clone channel itself uses.
func currentGitBranch(dir string) (string, error) {
	out, err := gitProbeOutput(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	b := strings.TrimSpace(out)
	if b == "" || b == refGuardDetachedBranch {
		return "", nil
	}
	return b, nil
}
