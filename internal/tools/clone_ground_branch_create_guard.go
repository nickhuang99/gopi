package tools

// clone_ground_branch_create_guard.go — refuse a git invocation that CREATES,
// MOVES or RENAMES a ref, when a run with its own ground performs it in a
// DECLARED shared checkout.
//
// ── THE SUBJECT WAS WIDENED, and the first version's wording is why ─────────
//
// This file was born as a "branch creation" guard: it recognised the four
// CREATION FLAGS (checkout -b/-B, switch -c/-C, worktree add -b/-B) and nothing
// else. Measured on the next row (01a19640, 2026-11-01, git 2.43.0) that scope
// was a hole, not a boundary: with this run's ground announced and the guard
// wired in, in the declared shared checkout /home/user/work/gopi,
//
//	git -C /home/user/work/gopi branch task/probe-b1 a4899a4f          exit 0  ref created
//	git -C /home/user/work/gopi branch -f task/probe-b1 90927414       exit 0  ref MOVED
//	git -C /home/user/work/gopi update-ref refs/heads/task/probe-u1 …  exit 0  ref created
//
// — a flag-driven recogniser cannot see a write that carries no flag. So the
// subject is stated as the PROPERTY (which subcommands write a ref), in ONE list
// (cloneGroundRefWritingVerbs) read by both the recogniser and the fail-closed
// arm, and a subcommand in NEITHER that list nor the closed read-only list is
// REFUSED rather than assumed harmless. Evidence, both directions:
// /home/user/.gopi/sessions/01a19640-7632-7d19-9636-67539250995b/probe-before-fix.txt
// and …/probe-after-fix-v2.txt.
//
// ── THE INCIDENT (measured 2026-11-01, row 01a1957a-0e54) ────────────────────
//
// The row was dispatched with a clone ground — the runner created
// /home/user/workspace/01a1957a-…, set cmd.Dir into it, and appended the
// announcement "[Your ground — ALREADY CREATED for you]" to the briefing (the
// worker's own output.log line 7/8 records both). The run nevertheless ran
//
//	cd /home/user/work/gopi && git checkout -b task/01a1957a0e54-…    (14:02:27)
//
// — the MAIN checkout, shared with the operator and with every other run — and
// then edited cmd/agent/cli_scratch_gc.go THERE. The main checkout's reflog
// carries the pair `checkout: moving from master to task/…` (14:02:27) and
// `moving from task/… to master` (14:10:07), and the committed tree it produced
// (d66fc04e7, 14:21) is byte-identical to the fix that landed on master later
// that evening: the branch was real work in the wrong tree.
//
// WHY THIS IS A GUARD AND NOT (ONLY) A PROSE RULE. Every channel that could
// have said "this tree is not yours" already existed and every one of them was
// a TEXT: the announcement block, the publication clause, the authority clause
// ("the ONLY tree this run writes"), and PROJECT.md Rule 6a. They were in the
// briefing this run received. What was missing is the half a machine owns: the
// bash tool let a `git checkout -b` in the SHARED checkout run without a word,
// exactly as it once let a hand-rolled push create a ref no merge path consumes
// (the sibling guard git_publish_target_guard.go, whose shape this one copies:
// one channel, one verb, an announced ground, and GOPI_ALLOW_DANGEROUS as the
// operator's deliberate escape).
//
// ── WHAT "SHARED CHECKOUT" MEANS HERE — a DECLARATION, not a heuristic ──────
//
// The set is the settings.json `repos` table, read through the ONE existing
// resolver (config.ResolveMergerRepos). That table is already the system's
// statement of "these directories are repos THIS host merges into and builds
// in" — every key is swept by the merger (PROJECT.md Rule 12), so every key is
// by definition a checkout whose branch must not be parked by a run. No new
// config key, no new artifact (Rule 35): the guard reads a declaration that
// already exists and already has an owner.
//
// ── THE FOUR THINGS THIS DELIBERATELY DOES NOT DO ───────────────────────────
//
//  1. It does NOT require an announced ground to judge a ref write. It used to,
//     and that was the hole: `git -C <shared> …` names the repository without
//     the caller standing in it, so a process whose cwd is elsewhere (or one
//     with no ground at all) still wrote a ref in the shared checkout. Measured
//     2026-11-01 with `env -u GOPI_RUN_GROUND`: the branch was created and the
//     reflog kept `moving from master to task/probe-NOGROUND`. The judge is the
//     DECLARATION (`cloneGroundSharedCheckoutFor`), and the only exemption is a
//     tree that is not declared — which is what a run's own ground is, since no
//     declared root contains it.
//  2. It does not judge a creation INSIDE the run's own ground, or inside any
//     directory that is not declared as a shared repo. `git checkout -b` in
//     one's own clone is the ordinary way a ground is used.
//  3. It does not judge a creation the command only SPAWNS indirectly. A
//     branch created by a script the command invokes is NOT caught — the scan
//     is the command's own simple commands, through the package's ONE splitter
//     (refGuardClauses), because a second splitter is how two spellings drift
//     apart (Rules 13/14). The two spellings the command line CAN carry are
//     both covered: `git checkout -b`/`git switch -c` and `git worktree add
//     -b|-B` (a worktree add writes OUTSIDE the checkout it runs in, so its
//     -b is judged by the same predicate — see cloneGroundWorktreeCreateBranch).
//  4. It does not REFUSE a read. `git checkout <existing branch>`, `git switch
//     <name>`, `git checkout -- <file>` create nothing and are untouched.

// ── TWO FACES, ONE PREDICATE ───────────────────────────────────────────────
//
// The same judgement has to hold on three entry points, and it is expressed
// ONCE (cloneGroundSharedCheckoutVerdict) so a fourth cannot be added with a
// second opinion:
//
//	bash `git checkout -b|-B` / `git switch -c|-C`   (the measured incident)
//	bash `git worktree add -b|-B`                    (Rule 29's subject: it
//	                                                  writes OUTSIDE the
//	                                                  checkout the command
//	                                                  runs in)
//	the write/edit tool's own write path             (the other half of that
//	                                                  same incident: the run
//	                                                  edited
//	                                                  cmd/agent/cli_scratch_gc.go
//	                                                  in the shared checkout)
//
// Each face renders its OWN sentence — a branch creation and a file write are
// different acts — but "is this directory a declared shared checkout outside my
// ground?" is answered in exactly one place.

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// ── THE REF-WRITING VOCABULARY (Rules 1/13/14) ─────────────────────────────
//
// The guard's subject is "a git invocation that CREATES, MOVES or RENAMES a ref
// inside a declared shared checkout", so the vocabulary below is stated as
// WHICH VERBS WRITE A REF — not as "the flag spellings that were measured red".
// That framing is the fix, not a tidy-up: the earlier version recognised only
// checkout/switch/worktree's CREATION FLAGS, so a ref write that carries no such
// flag walked straight through it. Measured on this host (git 2.43.0), in the
// declared shared checkout /home/user/work/gopi, with this run's ground
// announced and the guard wired in:
//
//	git -C /home/user/work/gopi branch task/probe-b1 a4899a4f          exit 0  ref created
//	git -C /home/user/work/gopi branch -f task/probe-b1 90927414       exit 0  ref MOVED
//	git -C /home/user/work/gopi update-ref refs/heads/task/probe-u1 …  exit 0  ref created
//
// while `checkout -b`, `switch -C` and `worktree add -B` in the same directory
// were refused. Evidence: /home/user/.gopi/sessions/01a19640-…/probe-before-fix.txt.
const (
	// cloneGroundBranchVerb is git's refs-only verb. Every one of its creating,
	// moving and renaming spellings is judged — `<name>` (creation with no flag
	// at all), `-f`/`--force` (a move), `-m`/`-M` (a rename onto a new name),
	// `-c`/`-C` (copy/force-copy) — because each of them writes a ref the shared
	// checkout's other users depend on.
	cloneGroundBranchVerb = refGuardBranchVerb
	// cloneGroundTagVerb writes a TAG ref. `git tag -f` MOVES one, and a tag is
	// the DECISION ref every merge path reads (PROJECT.md Rule 9): which
	// exclusions are decided is not a run's call to make in the shared repo.
	cloneGroundTagVerb = gitTagVerb
	// cloneGroundReplaceVerb / cloneGroundPackRefsVerb /
	// cloneGroundFilterBranchVerb write refs under names the caller chooses
	// (refs/replace, the packed-refs file, a rewritten history). They are listed
	// as KNOWN WRITERS so the fail-closed half refuses them by the same sentence
	// as any other ref write rather than reporting them as unrecognised.
	cloneGroundReplaceVerb      = "replace"
	cloneGroundPackRefsVerb     = "pack-refs"
	cloneGroundFilterBranchVerb = "filter-branch"
)

// cloneGroundRefWritingVerbs is the ONE list of subcommands that can write a
// ref. The recogniser and the fail-closed scan both read it (Rule 14: one
// definition, two readers), so a verb in this map is recognised on BOTH paths
// and cannot fall between them.
var cloneGroundRefWritingVerbs = map[string]bool{
	cloneGroundBranchVerb:       true, // branch <name>|-f|-m|-M|-c|-C
	gitCheckoutSubcmd:           true, // checkout -b|-B
	gitSwitchSubcmd:             true, // switch -c|-C
	worktreeToken:               true, // worktree add -b|-B
	refGuardUpdateRefVerb:       true, // update-ref [--stdin]
	refGuardSymbolicRefVerb:     true, // symbolic-ref [--delete] HEAD <ref>
	refGuardFetchVerb:           true, // fetch (a refspec destination writes a local ref)
	cloneGroundTagVerb:          true, // tag [-f] <name> [<base>]
	cloneGroundReplaceVerb:      true,
	cloneGroundPackRefsVerb:     true,
	cloneGroundFilterBranchVerb: true,
}

// cloneGroundReadOnlyVerbs is the CLOSED list of subcommands the fail-closed
// half lets through: pure readers, plus the three that write something that is
// NOT a ref (`clean` removes untracked files, `prune` removes worktree metadata,
// `gc` collects objects). Anything in neither list is refused BY NAME — a git
// verb this file has never heard of is the one most likely to be the spelling
// nobody guarded yet, and "I do not recognise it" must never read as "it is
// safe" (Rule 15).
//
// A verb whose READ and WRITE forms differ is deliberately NOT in this list —
// `remote`, `config` and `reflog` each have both, so listing the verb would
// allow `git remote prune` (deletes remote-tracking refs) and
// `git config --unset` (rewrites .git/config in the shared checkout) exactly as
// freely as `git remote -v`. Those three are judged by SUB-FORM instead, in
// cloneGroundReadOnlyInvocation below.
var cloneGroundReadOnlyVerbs = map[string]bool{
	cloneGroundStatusVerb:      true,
	cloneGroundLogVerb:         true,
	cloneGroundShowVerb:        true,
	cloneGroundDiffVerb:        true,
	cloneGroundRevParseVerb:    true,
	cloneGroundRevListVerb:     true,
	cloneGroundCatFileVerb:     true,
	cloneGroundLsFilesVerb:     true,
	cloneGroundLsTreeVerb:      true,
	cloneGroundDescribeVerb:    true,
	cloneGroundBlameVerb:       true,
	cloneGroundMergeBaseVerb:   true,
	cloneGroundCheckIgnoreVerb: true,
	cloneGroundNameRevVerb:     true,
	cloneGroundVarVerb:         true,
	cloneGroundVersionVerb:     true,
	cloneGroundHelpVerb:        true,
	forEachRefVerb:             true,
	cloneGroundShortlogVerb:    true, // reads the log; writes nothing, ever
	cloneGroundCleanVerb:       true,
	cloneGroundPruneVerb:       true,
	cloneGroundGCVerb:          true,
}

// The vocabulary's spellings, named once (Rule 1: a string literal that decides
// a branch is a constant, never a literal at the comparison site).
const (
	cloneGroundStatusVerb      = "status"
	cloneGroundLogVerb         = "log"
	cloneGroundShowVerb        = "show"
	cloneGroundDiffVerb        = "diff"
	cloneGroundRevParseVerb    = gitRevParseVerb
	cloneGroundRevListVerb     = "rev-list"
	cloneGroundCatFileVerb     = "cat-file"
	cloneGroundLsFilesVerb     = "ls-files"
	cloneGroundLsTreeVerb      = "ls-tree"
	cloneGroundDescribeVerb    = "describe"
	cloneGroundBlameVerb       = "blame"
	cloneGroundMergeBaseVerb   = "merge-base"
	cloneGroundCheckIgnoreVerb = "check-ignore"
	cloneGroundNameRevVerb     = "name-rev"
	cloneGroundVarVerb         = "var"
	cloneGroundVersionVerb     = "version"
	cloneGroundHelpVerb        = "help"
	cloneGroundCleanVerb       = "clean"
	cloneGroundPruneVerb       = "prune"
	cloneGroundGCVerb          = "gc"
	// cloneGroundShortlogVerb reads the commit log; it has no writing form at
	// all, which is why it is listed wholesale rather than judged by sub-form.
	cloneGroundShortlogVerb = "shortlog"
	// cloneGroundRemoteVerb / cloneGroundConfigVerb / cloneGroundReflogVerb are
	// judged by SUB-FORM (cloneGroundReadOnlyInvocation): each has a read that a
	// run needs for ordinary investigation and a write that must not happen in
	// the shared checkout. Measured before they were judged this way, with the
	// fail-closed arm in place: `remote -v`, `reflog -3` and `config --get` were
	// REFUSED as unrecognised — the guard had made the shared checkout
	// un-inspectable, which is its own defect (probe-overreach.txt).
	cloneGroundRemoteVerb = "remote"
	cloneGroundConfigVerb = "config"
	cloneGroundReflogVerb = gitReflogVerb
)

// cloneGroundReadOnlySubForms is the SUB-FORM half of the read-only vocabulary:
// a verb whose read and write forms share a name is let through only in the
// forms named here, and refused in every other form. The shape is a verb → set
// of first-operand words (empty string = "no operand at all, e.g. `git remote`"),
// so adding a read form is one line and an unlisted form is a refusal by
// construction rather than by remembering to add a case.
//
// Each entry states WHY the form cannot write a ref in the shared checkout.
var cloneGroundReadOnlySubForms = map[string]cloneGroundSubFormRule{
	cloneGroundRemoteVerb: {
		// `git remote` (list), `-v`/`--verbose`, `show <name>`, `get-url <name>`.
		// The WRITE forms — `add`, `remove`/`rm`, `rename`, `set-url`, `prune`,
		// `update` — are NOT listed: `prune`/`update` delete or move
		// remote-tracking refs, which is exactly the ref write this guard exists
		// to stop, and they are refused by falling through.
		words: map[string]bool{"": true, "-v": true, "--verbose": true, "show": true, "get-url": true},
	},
	cloneGroundConfigVerb: {
		// `--get`, `--get-all`, `--get-regexp`, `--list`/`-l` and a bare key read.
		// Every one of them is a READ of .git/config. `--unset`, `--add`, `--replace-all`
		// and a bare `key value` pair REWRITE the shared checkout's config and are
		// refused by falling through.
		words: map[string]bool{"--get": true, "--get-all": true, "--get-regexp": true, "--list": true, "-l": true},
	},
	cloneGroundReflogVerb: {
		// `git reflog` (show) and `git reflog show [<flags>]`. The other
		// subcommands — `expire` (DELETES reflog entries and can drop refs),
		// `delete`, `drop` — are refused by falling through.
		//
		// anyLeadFlag is TRUE here and false for `remote`/`config`, and the
		// difference is not a loosening: `reflog`'s WRITE forms are all positional
		// subcommand words, so a leading `-`-prefixed token can only be a flag of
		// the read form (`git reflog -3`, `--date=iso`, `-n 5`). `remote` and
		// `config` are the opposite shape — their write forms are positional
		// (`prune`, `--unset`) — so there a leading flag must be named to be a read.
		words:       map[string]bool{"": true, "show": true},
		anyLeadFlag: true,
	},
}

// cloneGroundSubFormRule is one verb's allowed read forms.
type cloneGroundSubFormRule struct {
	// words are the first-operand spellings that are READ-ONLY. The empty string
	// stands for "no operand at all".
	words map[string]bool
	// anyLeadFlag says every `-`-prefixed first word is a flag of the READ form,
	// which is true exactly when the verb's WRITE forms are positional
	// subcommand words (`reflog expire`). It is false for the verbs whose write
	// forms are themselves flags (`config --unset`), where a flag must be named.
	anyLeadFlag bool
}

// cloneGroundReadOnlyInvocation answers "is this argument list a READ of the
// shared checkout?" for the verbs whose read and write forms share a name.
//
// It is deliberately narrow: an argument list it does not positively recognise
// as a read gets "false", and the caller then applies the ordinary fail-closed
// refusal. That direction is the point — a form nobody listed is refused, so a
// git release that adds `git remote frobnicate` cannot silently reach the shared
// checkout's refs.
func cloneGroundReadOnlyInvocation(verb string, rest []string) bool {
	rule, ok := cloneGroundReadOnlySubForms[verb]
	if !ok {
		return false
	}
	if len(rest) == 0 {
		return rule.words[""]
	}
	first := rest[0]
	if rule.words[first] {
		return true
	}
	if rule.anyLeadFlag && strings.HasPrefix(first, "-") {
		// The verb's WRITE forms are positional (`reflog expire`), so a leading
		// flag can only belong to the read form (`git reflog -3`).
		return true
	}
	// A bare `git config <key>` read: the first word is a key, not a verb, and a
	// key carries a dot. `git config --unset key` is rejected above (the flag is
	// not listed); `git config key value` has a SECOND word, which a read never
	// has, so it is not a read either.
	if verb == cloneGroundConfigVerb && len(rest) == 1 && strings.Contains(first, ".") {
		return true
	}
	return false
}

// cloneGroundWorktreeCreateFlagSet is the SUBSET of the creation flags that
// `git worktree add` accepts: only `-b` and `-B` create a branch there — `-c`/
// `-C` are `git switch`'s spellings and git refuses them on this verb. The two
// values still come from the constants the package already owns, so the family
// keeps ONE spelling (Rules 13/14) while the verb-specific subset is stated
// instead of inherited from a broader set.
var cloneGroundWorktreeCreateFlagSet = map[string]bool{
	taskBranchCreateFlagShort: true,
	taskBranchCreateFlagForce: true,
}

// cloneGroundBranchCreateFlagSet is the set of git flags that CREATE a branch —
// read from the constants internal/tools already owns for the same four argv
// tokens (Rules 13/14: one spelling for `-b`/`-B`/`-c`/`-C`).
var cloneGroundBranchCreateFlagSet = map[string]bool{
	taskBranchCreateFlagShort:       true,
	taskBranchCreateFlagForce:       true,
	taskBranchCreateFlagSwitch:      true,
	taskBranchCreateFlagSwitchForce: true,
}

// guardCloneGroundBranchCreate refuses a branch-creating git invocation that a
// run with its OWN announced ground performs in a declared shared checkout.
//
// It returns the refusal sentence, or "" when nothing is refused. A refusal is
// WORDED and names its evidence (Rule 15): the branch, the directory, the
// ground, the declaration that made the directory "shared", and the route that
// is correct.
func guardCloneGroundBranchCreate(command, workdir string) string {
	// THE GROUND IS NO LONGER A PRECONDITION (measured 2026-11-01). The guard
	// used to return "" the moment this process had no announced ground, and that
	// early return was the hole: `git -C <shared> …` names the repository WITHOUT
	// the caller standing in it, so a process whose cwd is somewhere else can
	// still write a ref in the shared checkout. Measured with a process that had
	// no ground at all (env -u GOPI_RUN_GROUND):
	//
	//	OUT=$(git -C /home/user/workspace/_tmp/refguard-subst-probe/shared \
	//	      checkout -b task/probe-NOGROUND 2>&1)   → exit 0, branch created
	//	git -C … reflog → HEAD@{1}: moving from master to task/probe-NOGROUND
	//
	// So the judge is the DECLARATION, never the ground: the question is "is the
	// repository this command acts on one this host declares as a shared
	// checkout?", and cloneGroundSharedCheckoutVerdict answers it for BOTH
	// populations — a run that owns a ground (whose own tree stays exempt) and a
	// process that owns none (whose exemption is "nothing in the declaration").
	// A process with no ground has no tree to defend, so the route the sentence
	// recommends is the declaration rule itself (stay out of the declared
	// checkout), not "run it in your ground".
	ground, _ := TaskGroundAnnounced()
	ground = strings.TrimSpace(ground)
	// The cheap gate first: a command that never names git creates nothing. It
	// is a SUBSTRING test on purpose — a false positive here only costs one
	// clause scan, while a false negative would skip the guard entirely.
	if !strings.Contains(command, gitBinaryName) {
		return ""
	}
	// ITERATE SEGMENTS, not clauses: a clause also yields the simple commands
	// that NESTED constructs inside it execute (`$( … )`, `` `…` ``, `eval`,
	// `bash -c`). That is the parse-layer fix for this file's own measured
	// bypass — `OUT=$(git -C <shared> checkout -b X)` split into ONE clause whose
	// head was `OUT=$(git`, so the recogniser found no `git` and the clause was
	// DROPPED while git really ran. See ref_guard_shell_nesting.go.
	for _, clause := range refGuardClauses(command, workdir) {
		for _, seg := range refGuardClauseSegments(clause) {
			if reason := cloneGroundSegmentRefusal(seg, workdir, ground); reason != "" {
				return reason
			}
		}
	}
	return ""
}

// cloneGroundSegmentRefusal judges ONE simple command the shell will execute —
// the clause's own, or one a nesting construct runs as its body.
//
// It is the loop body the guard has always had, lifted into its own function so
// the nesting walk and the refusal share one reading (Rules 13/14: one predicate,
// two call sites is how two opinions start).
func cloneGroundSegmentRefusal(seg refGuardClauseSegment, workdir, ground string) string {
	// NORMALISE the head first: a clause that WRAPS its command in a nesting
	// construct (`OUT=$(git -C <shared> checkout -b X)`) has its first word
	// turned into the command it wraps, in place — so the recogniser below sees
	// an ordinary git invocation with an ordinary target repo, and every reader
	// that computes a verb index from the word count stays correct.
	clause := refGuardClause{runDir: seg.runDir, words: refGuardUnwrapShellHead(seg.words)}
	// A clause that nests something the parse layer could NOT read to the end is
	// refused by name, not skipped. The original defect was never the missing
	// spelling — it was that "I did not understand this command" read as "there
	// is nothing here to judge" (Rule 15).
	if refGuardSegmentUnread(seg.words) {
		return cloneGroundUnreadNestingRefusal(seg.runDir, ground)
	}
	verb, rest, ok := refGuardGitSubcommand(clause.words)
	if !ok {
		return "" // genuinely not a git invocation
	}
	repoDir := cloneGroundGitRepoDir(clause, rest, workdir)
	// FAIL-CLOSED (the second half of this fix). A verb neither recognised
	// as a ref writer nor on the closed read-only list is REFUSED when it
	// would run in a declared shared checkout, because "this guard has
	// never heard of it" is not evidence that it writes no ref. Measured
	// before the fix: `git update-ref refs/heads/task/probe-u1 …` exited 0
	// in the shared checkout — a recognised-by-git verb the guard simply did
	// not list. A new git release adds verbs; this arm is what makes that a
	// refusal instead of a silent hole.
	if reason := cloneGroundUnknownVerbRefusal(verb, rest, repoDir, ground); reason != "" {
		return reason
	}
	if !cloneGroundRefWritingVerbs[verb] {
		return "" // a known reader (or a writer of something that is not a ref)
	}
	for _, written := range cloneGroundWrittenRefs(verb, rest) {
		// taskBranchExempt is ONE rule for every verb: `master`/`main` and the
		// `archive/` namespace are the two names a run may write here. The
		// archive exemption is deliberate rather than an oversight — Rule 9
		// makes `archive/<branch>` the DECIDED-exclusion lane and `gopi task
		// archive` its one writer, a verb that does not go through this guard;
		// refusing the namespace on the bash path would take over a lane the
		// system owns while the lane's own writer kept working.
		if written.name == "" || taskBranchExempt(written.name) {
			continue
		}
		// A worktree add writes OUTSIDE the directory the command runs in,
		// so BOTH directories are judged: the checkout the command was
		// issued from (where a creation in the shared tree is the same
		// defect) and the target git will create.
		if reason := cloneGroundSharedCheckoutRefusal(written.name, repoDir, ground); reason != "" {
			return reason
		}
		if written.target != "" {
			if reason := cloneGroundSharedCheckoutRefusal(written.name, written.target, ground); reason != "" {
				return reason
			}
		}
	}
	return ""
}

// cloneGroundUnreadNestingRefusal renders the parse layer's own sentence: the
// shell will run a command this guard could not read to the end, in a declared
// shared checkout, while the run owns a ground of its own.
func cloneGroundUnreadNestingRefusal(dir, ground string) string {
	shared, judged := cloneGroundSharedCheckoutVerdict(dir, ground)
	if !judged {
		return ""
	}
	ground = strings.TrimSpace(ground)
	if ground == "" {
		return fmt.Sprintf(cloneGroundNoGroundUnreadNestingRefusalFmt, dir, dir, shared)
	}
	return fmt.Sprintf(cloneGroundUnreadNestingRefusalFmt, dir, dir, shared, ground, ground)
}

// cloneGroundNoGroundUnreadNestingRefusalFmt is the parse layer's sentence for a
// process with NO announced ground: it names the declaration, never a tree the
// process does not have.
const cloneGroundNoGroundUnreadNestingRefusalFmt = "cannot verify the command that runs in %s: %s is the declared shared checkout %s " +
	"of this host (a settings `repos` entry the merger sweeps and rebuilds), and this guard could NOT read a command " +
	"nested inside the one you wrote to the end — so it cannot tell whether that nested command writes a ref here. " +
	"An unreadable command is refused rather than assumed harmless: that is exactly how `OUT=$(git -C <shared> " +
	"checkout -b <name>)` ran while the guard said nothing. Nothing was run. Write the command so its nested parts " +
	"are readable (a plain `git ...` clause), or run it in a repository that is NOT declared here (a clone, or your " +
	"task's own ground); if the nesting is genuinely required, that is an OPERATOR decision " +
	"(GOPI_ALLOW_DANGEROUS=1 opens this guard deliberately)."

// cloneGroundUnreadNestingRefusalFmt is that sentence's ONE spelling.
const cloneGroundUnreadNestingRefusalFmt = "cannot verify the command that runs in %s: %s is the declared shared checkout %s " +
	"of this host (a settings `repos` entry the merger sweeps and rebuilds), while this run's own ground is %s " +
	"(announced to this run by the runner), and this guard could NOT read a command nested inside the one you wrote " +
	"to the end — so it cannot tell whether that nested command writes a ref here. An unreadable command is refused " +
	"rather than assumed harmless: that is exactly how `OUT=$(git -C <shared> checkout -b <name>)` ran while the guard " +
	"said nothing. Nothing was run. Write the command so its nested parts are readable (a plain `git ...` clause), or " +
	"run it INSIDE your ground (the tool already runs in %s); if the nesting is genuinely required, that is an OPERATOR " +
	"decision (GOPI_ALLOW_DANGEROUS=1 opens this guard deliberately)."

// cloneGroundRefWrite is ONE ref an invocation would write: the branch-ish NAME
// it writes, and (for `worktree add`) the directory the write lands in.
type cloneGroundRefWrite struct {
	name   string
	target string
}

// cloneGroundWrittenRefs answers the question the guard exists to ask — "which
// refs would this invocation write?" — for EVERY verb in
// cloneGroundRefWritingVerbs, in one place. The earlier shape asked it per verb
// inside a switch, which is exactly how three of them came to be missing: a verb
// with no case is not a compile error, it is a silent allowance. Adding a verb to
// the map without adding it here is caught by the vocabulary test
// (TestCloneGroundBranchCreate_EveryWritingVerbIsJudged).
//
// A verb that legitimately writes nothing in a given invocation returns no
// entries: `git branch -a` lists, `git tag` with no operand lists, `git worktree
// list` lists, `git symbolic-ref <name>` with one operand READS. Those are the
// switches' `-l`/`--list`-shaped branches, and each one states its reason.
func cloneGroundWrittenRefs(verb string, rest []string) []cloneGroundRefWrite {
	switch verb {
	case gitCheckoutSubcmd, gitSwitchSubcmd:
		if branch := cloneGroundBranchCreateName(rest); branch != "" {
			return []cloneGroundRefWrite{{name: branch}}
		}
	case worktreeToken:
		if branch, target := cloneGroundWorktreeCreateBranch(rest); branch != "" {
			return []cloneGroundRefWrite{{name: branch, target: target}}
		}
	case cloneGroundBranchVerb:
		return cloneGroundBranchVerbWrites(rest)
	case refGuardUpdateRefVerb:
		return cloneGroundUpdateRefWrites(rest)
	case refGuardSymbolicRefVerb:
		return cloneGroundSymbolicRefWrites(rest)
	case cloneGroundTagVerb:
		return cloneGroundTagWrites(rest)
	case refGuardFetchVerb:
		return cloneGroundFetchWrites(rest)
	case cloneGroundReplaceVerb, cloneGroundPackRefsVerb, cloneGroundFilterBranchVerb:
		// These write refs under names only the implementation knows, so the
		// honest answer is the verb's own name: it is refused in a shared
		// checkout whatever its operands are.
		return []cloneGroundRefWrite{{name: verb}}
	}
	return nil
}

// cloneGroundBranchVerbWrites reads `git branch …`. The refs-only verb's whole
// family writes: `<name>` creates, `-f`/`--force` moves, `-m`/`-M` renames onto
// a new name, `-c`/`-C` copies a ref. The renaming/copying forms write their
// SECOND operand (the new name) — and `-M <name>` alone renames the CURRENT
// branch onto <name>, which is why the single-operand form is judged on that
// operand too.
//
// Reading forms are stated, not silently absent: `-a`/`-r`/`--list`/`-l` list,
// `-d`/`-D` and `--delete` DELETE (a deletion writes no ref, and Rule 9 makes
// `gopi task archive` the one writer of a decided exclusion), `--merged` /
// `--no-merged` / `--contains` filter a listing, and `--show-current` prints.
func cloneGroundBranchVerbWrites(rest []string) []cloneGroundRefWrite {
	// NO flag of `git branch` consumes the following word (git's own getopt
	// declares `-f`/`-m`/`-c` argument-less, so `branch -f <name> <base>` is one
	// flag plus TWO positionals) — the same rule refGuardVerbFlagsTakesNoArgument
	// states for the shared guard, referenced rather than re-spelled.
	opts := refGuardScanOptions(refGuardVerbFlagsTakesNoArgument, "", append([]string{cloneGroundBranchVerb}, rest...))
	// Listing and filtering forms write nothing. `-a`/`-r`/`-l`/`--list` never
	// take a name; `-d`/`-D`/`--delete` is the documented deletion lane.
	for _, list := range []string{cloneGroundBranchAllFlag, cloneGroundBranchRemotesFlag,
		cloneGroundBranchListFlag, cloneGroundBranchListLongFlag,
		cloneGroundBranchDeleteFlag, cloneGroundBranchDeleteForceFlag, cloneGroundBranchDeleteLongFlag} {
		if opts.isset(list) {
			return nil
		}
	}
	// FORCING (`-f`/`--force`) and RENAMING/COPYING (`-m`/`-M`/`-c`/`-C`) write
	// DIFFERENT positionals, and saying so is the fix for a defect the real probe
	// caught (2026-11-01): `git branch -f task/probe-b1 HEAD` was refused with
	// `cannot create branch "HEAD"` — the destination was read off the LAST
	// positional, which for a FORCE is the start point (a read), so the sentence
	// named the wrong ref. The two forms are therefore judged separately below.
	forcing, renaming := false, false
	for _, flag := range []string{refGuardBranchForceFlag, refGuardBranchForceLongFlag} {
		if opts.isset(flag) {
			forcing = true
		}
	}
	for _, flag := range []string{cloneGroundBranchMoveFlag, cloneGroundBranchMoveLongFlag,
		cloneGroundBranchCopyFlag, cloneGroundBranchCopyLongFlag} {
		if opts.isset(flag) {
			renaming = true
		}
	}
	if len(opts.pos) == 0 {
		return nil // `git branch` with no operand, or a listing-only invocation
	}
	if renaming {
		// `git branch -m <old> <new>` / `-M <name>` / `-c <old> <new>`: the ref
		// that comes to EXIST is the destination — the last positional.
		return []cloneGroundRefWrite{{name: opts.pos[len(opts.pos)-1]}}
	}
	if forcing {
		// `git branch -f <name> [<start-point>]`: the MOVED ref is the FIRST
		// positional; the start point after it is a read. (A rename also accepts
		// `-f`, so `renaming` above is consulted first — a force-rename's
		// destination is still the last positional.)
		return []cloneGroundRefWrite{{name: opts.pos[0]}}
	}
	// `git branch <name> [<start-point>]`: the NAME is the first positional and
	// the start point is a READ.
	return []cloneGroundRefWrite{{name: opts.pos[0]}}
}

// cloneGroundUpdateRefWrites reads `git update-ref …`: the refname is the
// positional (or, in the `-d`/`--delete` form, the flag's own operand). The
// deletion form writes no ref and is skipped — `gopi task archive` is the one
// writer of a ref removal (Rule 9).
func cloneGroundUpdateRefWrites(rest []string) []cloneGroundRefWrite {
	if cloneGroundFlagSpelled(rest, refGuardDeleteFlag) || cloneGroundFlagSpelled(rest, refGuardDeleteLongFlag) {
		return nil
	}
	for _, w := range rest {
		if strings.HasPrefix(w, "-") || refGuardVerbFlagsWithoutRef[w] || w == "" {
			continue
		}
		return []cloneGroundRefWrite{{name: cloneGroundRefName(w)}}
	}
	return nil
}

// cloneGroundSymbolicRefWrites reads `git symbolic-ref …`: with ONE operand it
// READS the ref; with two it points the first at the second. `--delete` removes
// the symref (a removal, so it writes no ref).
func cloneGroundSymbolicRefWrites(rest []string) []cloneGroundRefWrite {
	if cloneGroundFlagSpelled(rest, refGuardDeleteFlag) || cloneGroundFlagSpelled(rest, refGuardDeleteLongFlag) {
		return nil
	}
	var pos []string
	for _, w := range rest {
		if strings.HasPrefix(w, "-") || refGuardVerbFlagsWithoutRef[w] {
			continue
		}
		pos = append(pos, w)
	}
	if len(pos) < 2 {
		return nil // `git symbolic-ref HEAD` prints; it writes nothing
	}
	return []cloneGroundRefWrite{{name: cloneGroundRefName(pos[0])}}
}

// cloneGroundTagWrites reads `git tag …`: with no operand it LISTS (writes
// nothing); with a name it CREATES a tag, and with `-f`/`--force` it MOVES one.
// `-d`/`--delete` and `-l`/`--list`/`-n` write nothing.
func cloneGroundTagWrites(rest []string) []cloneGroundRefWrite {
	for _, quiet := range []string{cloneGroundTagListFlag, cloneGroundTagListLongFlag,
		cloneGroundTagDeleteFlag, cloneGroundTagDeleteLongFlag, cloneGroundTagNumberFlag} {
		if cloneGroundFlagSpelled(rest, quiet) {
			return nil
		}
	}
	for _, w := range rest {
		if strings.HasPrefix(w, "-") {
			continue
		}
		return []cloneGroundRefWrite{{name: w}}
	}
	return nil
}

// cloneGroundFetchWrites reads `git fetch …`: only a refspec WITH A DESTINATION
// (`<src>:<dst>`, `+<src>:<dst>`) writes a local ref. A fetch of a source only
// (the ordinary `git fetch origin`) writes no refs/heads name, and a fetch from
// nothing at all writes nothing.
func cloneGroundFetchWrites(rest []string) []cloneGroundRefWrite {
	var out []cloneGroundRefWrite
	for _, w := range rest {
		if strings.HasPrefix(w, "-") || !strings.Contains(w, refGuardRefSep) {
			continue
		}
		_, dst, _ := strings.Cut(strings.TrimPrefix(w, cloneGroundRefspecForcePrefix), refGuardRefSep)
		if dst == "" {
			continue
		}
		out = append(out, cloneGroundRefWrite{name: cloneGroundRefName(dst)})
	}
	return out
}

// cloneGroundRefName reduces a ref operand to the NAME the refusal should
// mention: a full `refs/heads/<b>` is reported as `<b>` (the same string
// `taskBranchExempt` and every message in this package speak), and anything else
// is reported as written.
func cloneGroundRefName(ref string) string {
	ref = refGuardTrimRefPeelSuffix(strings.TrimSpace(ref))
	return strings.TrimPrefix(ref, cloneGroundRefsHeadsPrefix)
}

// cloneGroundFlagSpelled reports whether a flag appears among a verb's operands,
// in either of git's spellings (`-d`, `--delete`, `--force=true`).
func cloneGroundFlagSpelled(rest []string, flag string) bool {
	for _, w := range rest {
		if w == flag {
			return true
		}
		if strings.HasPrefix(w, flag+"=") {
			return true
		}
	}
	return false
}

// cloneGroundUnknownVerbRefusal is the FAIL-CLOSED arm: it refuses a git
// invocation whose subcommand this guard has never classified, when that
// invocation would run in a declared shared checkout. The answer is deliberately
// "refuse", never "allow": a silent allowance for an unknown verb is precisely
// how `git branch -f` and `git update-ref` were measured writing refs in the
// shared checkout while the guard said nothing.
//
// What it does NOT do: it judges only the DECLARED shared checkout, so a run's
// own ground keeps every git verb it likes; a process with no announced ground
// is not judged at all (its cwd is not a claim anybody made); and an invocation
// positively recognised as a READ (a verb on the closed read-only list, or a
// listed read SUB-FORM of `remote`/`config`/`reflog`) is let through with its
// reason stated where that list lives.
//
// It takes the argument list, not only the verb, because three verbs have a read
// form a run needs (`git remote -v`) and a write form this guard exists to stop
// (`git remote prune`, which deletes remote-tracking refs). Judging those by
// VERB alone gave one of two wrong answers, and BOTH were measured: allowing the
// verb lets `prune` through, refusing the verb makes the shared checkout
// un-inspectable (`remote -v`, `reflog -3`, `config --get` refused —
// probe-overreach.txt, 4 of 17 read commands refused).
func cloneGroundUnknownVerbRefusal(verb string, rest []string, dir, ground string) string {
	if verb == "" || cloneGroundRefWritingVerbs[verb] || cloneGroundReadOnlyVerbs[verb] {
		return ""
	}
	if cloneGroundReadOnlyInvocation(verb, rest) {
		return ""
	}
	shared, judged := cloneGroundSharedCheckoutVerdict(dir, ground)
	if !judged {
		return ""
	}
	ground = strings.TrimSpace(ground)
	if ground == "" {
		// The population that owns no ground gets the same evidence without a
		// claim about a tree it does not have (Rule 15).
		return fmt.Sprintf(cloneGroundNoGroundUnknownVerbRefusalFmt, verb, dir, shared, dir)
	}
	return fmt.Sprintf(cloneGroundUnknownVerbRefusalFmt, verb, dir, shared, ground, ground)
}

// cloneGroundNoGroundUnknownVerbRefusalFmt is the fail-closed arm's sentence for
// a process with NO announced ground: the evidence is the declaration alone.
const cloneGroundNoGroundUnknownVerbRefusalFmt = "cannot run `git %s` in %s: that directory is the declared shared checkout %s " +
	"of this host (a settings `repos` entry the merger sweeps and rebuilds), and %s is reached through git's own " +
	"repository selector (`-C`, `--git-dir`, `--work-tree`) or a plain `cd` — neither of which makes the target a " +
	"tree of your own — and this guard does NOT recognise that subcommand as one that cannot write a ref. An " +
	"unrecognised verb is refused rather than assumed harmless. Nothing was run. Run the command in a repository " +
	"that is NOT declared here (a clone, or your task's own ground), or — if it genuinely does not touch a ref — " +
	"that is an OPERATOR decision (GOPI_ALLOW_DANGEROUS=1 opens this guard deliberately)."

// The vocabulary the readers above compare against, named once (Rule 1).
const (
	cloneGroundBranchAllFlag                  = "-a"
	cloneGroundBranchRemotesFlag              = "-r"
	cloneGroundBranchListFlag                 = "-l"
	cloneGroundBranchListLongFlag             = "--list"
	cloneGroundBranchDeleteFlag               = "-d"
	cloneGroundBranchDeleteForceFlag          = "-D"
	cloneGroundBranchDeleteLongFlag           = "--delete"
	cloneGroundBranchMoveFlag                 = "-m"
	cloneGroundBranchMoveLongFlag             = "--move"
	cloneGroundBranchCopyFlag                 = "-c"
	cloneGroundBranchCopyLongFlag             = "--copy"
	cloneGroundTagListFlag                    = "-l"
	cloneGroundTagListLongFlag                = "--list"
	cloneGroundTagDeleteFlag                  = "-d"
	cloneGroundTagDeleteLongFlag              = "--delete"
	cloneGroundTagNumberFlag                  = "-n"
	cloneGroundRefspecForcePrefix             = "+"
	cloneGroundBranchShortFlagsWithoutOperand = "dvla"
)

// cloneGroundRefsHeadsPrefix is git's local-branch namespace, taken from the ONE
// definition this repository already has (internal/devworkspace's refsHeadsPrefix
// is unexported there, so the value is stated here with its source named rather
// than duplicated by luck).
const cloneGroundRefsHeadsPrefix = "refs/heads/"

// cloneGroundGitRepoDir answers WHICH directory this invocation's git acts on —
// the one question the shell cannot answer for us.
//
// THE HOLE THIS CLOSES (measured 2026-11-01, on the revision that introduced
// the worktree face): the guard read the directory from the SHELL only — the
// tool's workdir, or a `cd` earlier on the same line. Git has a second, equally
// ordinary way to name that directory, and it is not a shell construct at all:
//
//	git -C /home/user/work/gopi checkout -b task/probe-dashc     → created, exit 0
//	git -C /home/user/work/gopi worktree add -b task/probe-r2 …  → created, exit 0
//
// Both ran with the shared checkout declared, this run's ground announced, and
// the guard wired in — the branch really was created in the shared checkout and
// its HEAD really moved off the integration branch. A guard that a one-flag
// spelling walks around is not a guard; `git -C` is the spelling every careful
// author reaches for (it is what the fixtures themselves use, which is how the
// hole was found: the probe that was SUPPOSED to be refused returned 0).
//
// The operand is read through the package's ONE option scanner, and only from
// the words BEFORE the subcommand — the same rule refGuardResolve states, and
// for the same measured reason: scanning the whole clause reads `switch -C
// <name>` (the subcommand's own force-create flag) as if it were `git -C <dir>`.
func cloneGroundGitRepoDir(clause refGuardClause, rest []string, workdir string) string {
	if verbIdx := len(clause.words) - len(rest) - 1; verbIdx > 0 {
		opts := refGuardScanOptions(refGuardDashCArgRule, "", clause.words[:verbIdx])
		if dir := opts.valueOf(refGuardDashCShort, gitDashCDirOption); dir != "" {
			return expandTilde(dir)
		}
	}
	return refGuardRunDir(clause.runDir, workdir)
}

// cloneGroundSharedCheckoutVerdict is the ONE predicate behind every face of
// this guard: is dir a directory this host DECLARES as a shared checkout, and
// is it OUTSIDE the run's own announced ground?
//
// It answers (declared root, judged) — judged false means "nothing to refuse
// here", and the caller renders its own sentence. Keeping the decision here and
// the wording at each face is what stops three entry points from drifting into
// three opinions (Rules 13/14).
func cloneGroundSharedCheckoutVerdict(dir, ground string) (string, bool) {
	if dir == "" {
		return "", false // a judgement with no directory to speak about
	}
	if SameDir(dir, ground) || pathWithin(dir, ground) {
		return "", false // the run's own ground: the ordinary way a ground is used
	}
	return cloneGroundSharedCheckoutFor(dir)
}

// cloneGroundSharedCheckoutRefusal renders the branch-creation face's sentence,
// or "" when the predicate says there is nothing to refuse.
func cloneGroundSharedCheckoutRefusal(branch, dir, ground string) string {
	shared, judged := cloneGroundSharedCheckoutVerdict(dir, ground)
	if !judged {
		return ""
	}
	return cloneGroundBranchCreateRefusal(branch, dir, shared, ground)
}

// cloneGroundWorktreeCreateBranch reads a `git worktree add` argument list and
// returns the branch it CREATES plus the directory operand it targets.
//
// `git worktree add -b|-B <branch> <path>` is the same creation event as
// `git checkout -b`, judged by the same predicate — one flag vocabulary, two
// verbs (Rule 13). The target is the operand AFTER the branch name, which is the
// shape git accepts.
func cloneGroundWorktreeCreateBranch(rest []string) (branch, target string) {
	if len(rest) == 0 || rest[0] != worktreeAddToken {
		return "", ""
	}
	for i := 1; i < len(rest); i++ {
		if !cloneGroundWorktreeCreateFlagSet[rest[i]] || i+1 >= len(rest) {
			continue
		}
		branch = strings.TrimRight(rest[i+1], ";&|")
		if i+2 < len(rest) {
			target = strings.TrimRight(rest[i+2], ";&|")
		}
		return branch, target
	}
	return "", ""
}

// cloneGroundBranchCreateName returns the branch a checkout/switch argument
// list CREATES, or "" when the invocation creates none. `-b`/`-B`/`-c`/`-C`
// each take the branch as the NEXT word; anything else (a plain `checkout
// <branch>`, a file restore) creates nothing.
func cloneGroundBranchCreateName(rest []string) string {
	for i := 0; i+1 < len(rest); i++ {
		if cloneGroundBranchCreateFlagSet[rest[i]] {
			return strings.TrimRight(rest[i+1], ";&|")
		}
	}
	return ""
}

// cloneGroundSharedCheckoutFor answers "is dir a checkout this host DECLARES as
// a repo it merges into?" and returns the declared root's own spelling.
//
// The declaration is the settings.json `repos` table, resolved through the ONE
// reader every other consumer uses (config.ResolveMergerRepos). An unreadable
// settings document answers "not declared": the guard may not invent a shared
// tree out of a broken file, and a guard that refused on an unrelated config
// error would block work for a reason that has nothing to do with the command.
func cloneGroundSharedCheckoutFor(dir string) (string, bool) {
	roots, err := config.ResolveMergerRepos()
	if err != nil {
		return "", false
	}
	for _, root := range roots {
		if SameDir(dir, root) || pathWithin(dir, root) {
			return root, true
		}
	}
	return "", false
}

// cloneGroundUnknownVerbRefusalFmt is the FAIL-CLOSED arm's ONE spelling.
const cloneGroundUnknownVerbRefusalFmt = "cannot run `git %s` in %s: that directory is the declared shared checkout %s " +
	"of this host (a settings `repos` entry the merger sweeps and rebuilds), while this run's own ground is %s " +
	"(announced to this run by the runner), and this guard does NOT recognise that subcommand as one that cannot " +
	// branch-force-ok: the subject of this sentence IS the destructive form — it is
	// the measured spelling the guard's fail-closed arm exists FOR (see the header:
	// `branch -f task/probe-b1` moved a ref here while the guard said nothing). The
	// string is prose inside a refusal, never a command gopi runs.
	"write a ref. An unrecognised verb is refused rather than assumed harmless: that is how `git branch -f` and " +
	"`git update-ref` were measured moving refs in this tree while the guard said nothing. Nothing was run. " +
	"Run the command INSIDE your ground instead (the tool already runs in %s), or — if it genuinely does not " +
	"touch a ref — that is an OPERATOR decision (GOPI_ALLOW_DANGEROUS=1 opens this guard deliberately)."

// cloneGroundBranchCreateRefusalFmt is the refusal's ONE spelling: the branch,
// the directory the creation was measured in, the declared shared root, and the
// ground that was announced to this run. It is rendered ONLY for a process that
// HAS a ground; the words "this run's own ground" are a fact about that process
// and must not be printed about one that owns no tree (cloneGroundNoGroundRefusalFmt
// is that population's spelling).
const cloneGroundBranchCreateRefusalFmt = "cannot create branch %q in %s: that directory is the declared shared checkout %s " +
	"of this host (a settings `repos` entry the merger sweeps and rebuilds), while this run's own ground is %s " +
	"(announced to this run by the runner). A branch created in the shared checkout parks the tree every landing " +
	"and every other run depends on — the main checkout's HEAD moves off the integration branch, the merge step's " +
	"fast-forward is refused, and the commit lands in a tree no merge path publishes. Nothing was created. " +
	"Create the branch INSIDE your ground instead (a bare `git checkout -b <branch>` with no `cd` — the tool " +
	"already runs in %s), or, if this really is meant to be a shared-checkout operation, that is an OPERATOR " +
	"decision (GOPI_ALLOW_DANGEROUS=1 opens this guard deliberately)."

// cloneGroundNoGroundRefusalFmt is the SAME refusal for a process that owns NO
// ground — and it exists because the ground is no longer what makes the
// judgement (2026-11-01). A guard that refused this population without a
// sentence would be a guard whose refusal names a tree the process does not have
// (Rule 15: a refusal has to state its own evidence).
//
// The evidence is the DECLARATION and nothing else, so the sentence says exactly
// that: this repository is a declared shared checkout, the guard does not need to
// know where you stand, and the correct route is "not here".
const cloneGroundNoGroundRefusalFmt = "cannot create branch %q in %s: that directory is the declared shared checkout %s " +
	"of this host (a settings `repos` entry the merger sweeps and rebuilds), and %s is reached through git's own " +
	"repository selector (`-C`, `--git-dir`, `--work-tree`) or a plain `cd` — neither of which makes the target a " +
	"tree of your own. A ref written in the shared checkout parks the tree every landing and every other run depends " +
	"on — its HEAD moves off the integration branch, the merge step's fast-forward is refused, and the commit lands " +
	"in a tree no merge path publishes. Nothing was created. Run the command in a repository that is NOT declared " +
	"here (a clone, or your task's own ground), or, if this really is meant to be a shared-checkout ref operation, " +
	"that is an OPERATOR decision (GOPI_ALLOW_DANGEROUS=1 opens this guard deliberately)."

// cloneGroundBranchCreateRefusal renders the branch-creation face's sentence for
// the population it was written for, chosen by ONE fact: does this process have
// an announced ground?
func cloneGroundBranchCreateRefusal(branch, dir, shared, ground string) string {
	ground = strings.TrimSpace(ground)
	if ground == "" {
		// No ground: the refusal must not claim one exists. Same evidence
		// (branch, directory, declared root), one spelling.
		return fmt.Sprintf(cloneGroundNoGroundRefusalFmt, branch, dir, shared, dir)
	}
	return fmt.Sprintf(cloneGroundBranchCreateRefusalFmt, branch, dir, shared, ground, ground)
}

// ── THE WRITE/EDIT FACE ─────────────────────────────────────────────────────
//
// The 01a1957a incident has TWO halves and the branch creation is only the
// first: the run then EDITED cmd/agent/cli_scratch_gc.go **in the shared
// checkout**, and that half had no machine in it at all — a `write`/`edit` call
// naming an absolute path under the main checkout was accepted, the bytes
// landed, and the only thing that could have objected was prose in the briefing
// the reframe had already dropped.
//
// So the SAME predicate is consulted on the file tools' write seam. What it
// deliberately does NOT do:
//
//   - it does not judge a path inside the run's own ground (the ordinary case);
//   - it does not judge a path in a directory nothing declares as a shared
//     checkout — a session directory under /home/user/.gopi, a scratch root, every
//     temporary file a run writes;
//   - it does not judge a process with NO announced ground (the operator's own
//     shell, `gopi chat`, a repo-less row): that process's writes are not a
//     claim anybody made, and refusing the operator's own edit would be the
//     guard inventing an owner;
//   - it does not judge a read, a directory listing or a delete.
//
// The escape is the SAME one the bash face has (GOPI_ALLOW_DANGEROUS), because
// two escapes for one judgement is how an operator's deliberate decision starts
// meaning two different things.

// cloneGroundFileWriteRefusal refuses a write/edit that a run with its OWN
// announced ground performs into a declared shared checkout. It returns the
// refusal sentence, or "" when nothing is refused.
//
// It is called from atomicWriteFile — the ONE write primitive both the write
// and the edit tool funnel through — so no future caller of that primitive can
// land bytes in the shared checkout while bypassing the judgement.
func cloneGroundFileWriteRefusal(path, op string) string {
	ground, _ := TaskGroundAnnounced()
	ground = strings.TrimSpace(ground)
	if ground == "" {
		return "" // no claim about this process: nothing to defend
	}
	// The SAME two switches the bash face honours, for the same reasons:
	// RejectDangerous is the package's on/off for dangerous-operation guards, and
	// escapeDangerous is the operator's deliberate GOPI_ALLOW_DANGEROUS. Two faces
	// of one judgement must not answer to two different switches.
	if !RejectDangerous || escapeDangerous {
		return ""
	}
	dir := filepath.Dir(path)
	shared, judged := cloneGroundSharedCheckoutVerdict(dir, ground)
	if !judged {
		return ""
	}
	return fmt.Sprintf(cloneGroundFileWriteRefusalFmt, op, path, dir, shared, ground, ground)
}

// cloneGroundFileWriteRefusalFmt is the write face's ONE spelling: the
// operation, the path, the directory it resolves to, the declared shared root,
// and the ground that was announced to this run.
const cloneGroundFileWriteRefusalFmt = "%s: %s resolves to %s, inside the declared shared checkout %s of this host " +
	"(a settings `repos` entry the merger sweeps and rebuilds), while this run's own ground is %s " +
	"(announced to this run by the runner). A write into the shared checkout lands in the tree every landing and " +
	"every other run depends on — no merge path publishes it, and the next `git pull` collides with it. " +
	"Nothing was written. Write the file INSIDE your ground instead (the same path relative to %s), or, if this " +
	"really is meant to be a shared-checkout change, that is an OPERATOR decision (GOPI_ALLOW_DANGEROUS=1 opens " +
	"this guard deliberately)."

// cloneGroundFileWriteOpWrite / …Edit name the operation in the refusal, so the
// sentence says which tool refused (Rule 1: the tool names come from
// internal/toolnames, never typed inline).
const (
	cloneGroundFileWriteOpWrite = toolnames.ToolWrite
	cloneGroundFileWriteOpEdit  = toolnames.ToolEdit
)
