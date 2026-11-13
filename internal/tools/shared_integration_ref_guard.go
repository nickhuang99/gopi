package tools

// shared_integration_ref_guard.go — refuse a `git update-ref` / `git
// symbolic-ref` that moves a `refs/heads/<branch>` which a DIFFERENT worktree of
// the same repository has checked out.
//
// ── THE INCIDENT (measured 2026-10-12, HP; row 01a12d86, bug row 01a12da8) ────
//
// A run issued, from its OWN linked worktree
// (/home/nick/workspace/gopi-01a12d30…), a command whose first step was
//
//	git update-ref refs/heads/master 1508e3821 2>/dev/null || true
//
// That worktree belongs to the SAME repository as the operator's main checkout
// (`git rev-parse --git-common-dir` → /home/nick/work/GoPi/.git), so the write
// landed on the MAIN CHECKOUT'S integration-branch ref — a full pull rolled
// back, fast-forward `6f7acc1e1 → dfa56ff93` undone to `1508e3821`. No tree and
// no index were written (`.git/logs/HEAD` mtime unchanged; the only mutation was
// the ref), so the main checkout's index still held the PULLED tree while its
// ref pointed a pull behind: `git status` rendered the difference as **60 staged
// paths / 9419 insertions**. A tracked-dirty main checkout PARKS the merge queue
// (Rule 30; `mergeDetailTrackedDirty`, cmd/agent/merge_run.go) — every branch
// waiting to land queued behind a ref nobody meant to write.
//
// ── WHY THIS SEAM, AND WHY THIS VERB PAIR ────────────────────────────────────
//
// `refs/heads/*` is the repository's COMMON ref namespace: every linked worktree
// resolves `--git-common-dir` to the same directory, so a branch ref written from
// any worktree IS the same ref the main checkout sees. The blast radius — not
// convenience — is what makes a refusal legitimate (the same reasoning
// shared_stash_guard.go carries for `refs/stash`).
//
// The scope is EVERY verb of that family whose write is a `refs/heads/<branch>`,
// because "git refuses the siblings" is FALSE and was MEASURED false on git
// 2.34.1 (2026-10-12) — the file once claimed otherwise, in the same commit whose
// acceptance test went green. THE MEASURED VERB TABLE (this run's own census,
// generator + raw output in the session's artifacts/verb-census.{sh,txt}; every
// line issued from a LINKED worktree whose HEAD is `other`, aimed at
// `refs/heads/main`, the branch the MAIN checkout holds):
//
//	target = refs/heads/<held by ANOTHER worktree>
//	git update-ref refs/heads/<b> <sha>        exit 0    ref MOVED     ← the incident
//	git update-ref -d refs/heads/<b>           exit 0    ref DELETED
//	git branch -f <b> <sha>                    exit 0    ref MOVED
//	git branch --force <b> <sha>               exit 0    ref MOVED     (same flag, long form)
//	git branch -M <b> <new>                    exit 0    ref RENAMED AWAY (goes absent)
//	git branch -M <b>                          exit 0    IN PLACE — moves no ref (measured)
//	git checkout -B <b> <sha>                  exit 0    ref MOVED, caller switched onto it
//	git switch -C <b> <sha>                    exit 0    same (switch's force-create)
//	git fetch . src:<b> / +src:<b> / src:<short>  exit 0 ref MOVED
//	git fetch <remote> with remote.<r>.fetch=+refs/heads/*:refs/heads/* and NO
//	  refspec, caller DETACHED                  exit 0    ref MOVED     (6th hole, measured)
//	git fetch --refmap=+refs/heads/*:refs/heads/* <remote> '+refs/heads/*:refs/remotes/origin/*'
//	                                           exit 0    ref MOVED     (measured)
//	git branch -D <b>                          exit 1    "Cannot delete branch '<b>' checked out at '<other worktree>'"
//	git worktree add -B <b>                    exit 128  "'<b>' is already checked out at '<other worktree>'"
//	git branch -c src <b>                      exit 128  "A branch named '<b>' already exists."
//	target = refs/heads/<the CALLER's own branch>
//	git branch -f <own> / --force <own>        exit 128  "Cannot force update the current branch."
//	git checkout -B <own>                      exit 0    resets the caller's own branch (its own lane)
//	git fetch . src:<own>                      exit 128  "Refusing to fetch into current branch … of non-bare repository"
//
// So git's own refusal covers only PART of the family, and it keys on TWO
// different conditions — `branch -D` and `worktree add -B` refuse a branch held
// by ANOTHER worktree, while `branch -f` on the caller's own branch and `fetch`
// into it refuse the CALLER'S OWN current branch. Neither condition covers the
// verbs this guard now judges. The implementation is a SUPERSET of the epic's
// four named verbs and the superset is stated deliberately (Rule 41: measured,
// not inferred): `-M` in place, `switch -C` / `--force-create` and the two
// refmap spellings all write the SAME ref through the SAME silence, so guarding
// four exact spellings would leave a hole one flag away.
//
// ── THE VERSION DEPENDENCE OF THAT TABLE (measured 2026-10-12, git 2.43.0) ───
//
// The table above was taken on git 2.34.1, where NONE of the value-writing forms
// was refused. On git 2.43.0 the same measurement comes out DIFFERENT — git grew
// a HOLDER-HEAD check, and it answers a question about the holder's HEAD rather
// than about the write:
//
//	holder (another worktree) ATTACHED to <b>, caller in a LINKED worktree:
//	git branch -f <b> <sha>          exit 128  "cannot force update the branch
//	                                           '<b>' used by worktree at '<path>'"
//	git branch --force <b> <sha>     exit 128  same
//	git branch -M <old> <b>          exit 128  same
//	git fetch . +src:<b>             exit 128  "refusing to fetch into branch
//	                                           'refs/heads/<b>' checked out at '<path>'"
//	git fetch <remote> (config wildcard, no refspec)  exit 128  same
//	git checkout -B <b> <sha>        exit 0    ref MOVED   (no holder check)
//	git update-ref refs/heads/<b>    exit 0    ref MOVED   (no holder check)
//
// `git -C <holder> checkout --detach` takes that check out of the picture WITHOUT
// touching the holder's tree or index, and the same raw form then reaches the
// shared ref (exit 0, ref moved); re-pointing the holder's HEAD at the moved ref
// (`git -C <holder> symbolic-ref HEAD refs/heads/<b>`) leaves its index holding the
// tree it had, which `git status` reports as STAGED paths — the incident's park.
// This is the state the RED half's proof reproduces on this host
// (shared_integration_ref_guard_verbs_test.go: refGuardRawWriteIsReachable), and it
// changes NOTHING about the guard: a form git now refuses for a held branch is
// still a form that reaches the ref through the other spellings, and through the
// same spelling on any git without the check — so the widening stands, and the
// guard is a superset of git's own refusal on both versions.
//
// WHAT IS DELIBERATELY NOT JUDGED, and why it is not laziness — the two
// source-side forms, both measured, are LEFT ALONE because they never write the
// destination that matters:
//
//	git branch -m <held> <free>   exit 0 — renames a foreign-held branch AWAY
//	                              (refs/heads/main goes ABSENT), which desyncs
//	                              nothing staged and deletes nobody's ref; the
//	                              holder merely has a branch that no longer
//	                              answers to its name. Measured: 0 staged paths.
//	git branch -m <held> <held2>  exit 128 "A branch named '<b>' already exists."
//
// A refusal here would be a false positive on a `git branch -m` an operator may
// legitimately run from any worktree, so the guard does not take that lane; the
// measurement is recorded in the devdoc instead.
//
// WHY THE WIDENING IS SAFE FOR GOPI'S OWN LANES (the no-weakening half, each
// case exercised through the tool in the acceptance set): `worktree add -B`
// (GOPI.md's own branch-creation step) is refused by GIT for a held branch and
// is NOT judged here — `-B` only creates a branch when one of `worktree add`,
// `checkout`, `switch` says so, and a bare `-B` is not a branch write at all.
// `checkout -B` / `switch -C` on the caller's OWN branch, `branch -f` on its own
// branch, and every one of these aimed at a branch NO worktree holds stay
// allowed, because the predicate is the HOLDER and never the flag or the name.
//
// `update-ref` has no such refusal — which is exactly how the measured write
// succeeded silently, and how the `2>/dev/null || true` in the same command line
// kept the run from learning it had rewritten a shared repo.
//
// ── THE PREDICATE: THE HOLDER, NOT A BRANCH NAME ─────────────────────────────
//
// The rule is NOT "never write refs/heads/master" (the merger, finish-cleanup and
// the sync lane must all move the integration ref legitimately from the MAIN
// checkout, and a run must be free to write the branch ITS OWN worktree holds).
// It is: refuse when the target branch is checked out in a worktree that is NOT
// the caller's. So the guard asks git three read-only questions and decides:
//
//	1. is the caller in a linked worktree (or the main checkout)? — always; the
//	   probe is `--git-dir` vs `--git-common-dir`, and the MAIN checkout is never
//	   refused because it holds its own current branch (this is the merger's own
//	   path: it stays permitted);
//	2. does the repository have more than one worktree? — a single-checkout repo
//	   has no sharing and is never refused (the ordinary local flow);
//	3. which worktree holds the target branch? — the caller's own entry is
//	   excused; any OTHER entry is the refusal, and its PATH is named.
//
// Failing CLOSED: a worktree whose checkouts cannot be read leaves the target
// branch UNJUDGED, and an unjudged write to a shared namespace is refused rather
// than permitted (Rule 15: never a silent "probably fine"). A worktree directory
// that has already VANISHED is a different fact — provably holding nothing — and
// is skipped.
//
// What the refusal is not: it is not a substitute for a sandbox. A run that needs
// "a repository whose master is stale" (exactly what the incident's author was
// building) must use `gopi tool sandbox run` (Rule 34) or `git init` in a scratch
// directory, never the shared repo's ref namespace — which is why the refusal
// names that route rather than only saying no.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Integration-ref guard constants (Rule 1/1b: every dispatched-on spelling is a
// named constant; nothing here is typed inline at a comparison site).
const (
	// The verbs this guard governs. Each one writes a `refs/heads/<branch>` and
	// each one does it SILENTLY on git 2.34.1 when the branch is held by another
	// worktree (the measured table in the file head): a verb added here without
	// that property would refuse commands git would already have refused.
	refGuardUpdateRefVerb   = "update-ref"
	refGuardSymbolicRefVerb = "symbolic-ref"
	// refGuardBranchVerb is `git branch` — the refs-only verb. Only its
	// ref-MOVING flags are judged (see refGuardBranchRefOperands): `git branch
	// <name>` creates, `git branch -a` lists, and neither writes a held ref.
	refGuardBranchVerb = "branch"
	// refGuardCheckoutVerb / refGuardSwitchVerb are git's two branch-moving
	// verbs, judged only in their FORCE-CREATE spellings.
	refGuardCheckoutVerb = "checkout"
	refGuardSwitchVerb   = "switch"
	// refGuardFetchVerb writes a local ref when its refspec names a DESTINATION
	// (`<src>:<dst>`, `+<src>:<dst>`, a configured remote.<name>.fetch mapping).
	// A refspec with no destination writes no local branch ref at all.
	refGuardFetchVerb = "fetch"

	// The flags that make `git branch` MOVE an existing ref rather than create
	// one. Each is measured in the file head.
	//
	// refGuardBranchForceFlag / refGuardBranchForceLongFlag force-move a ref onto
	// a new value (`git branch -f <name> <base>`).
	refGuardBranchForceFlag     = "-f"
	refGuardBranchForceLongFlag = "--force"
	// refGuardBranchMoveFlag / refGuardBranchMoveLongFlag rename a ref
	// (`git branch -M <old> <new>`). Measured: the RENAME DESTINATION is the
	// foreign-held branch that matters — renaming a held branch AWAY moves no
	// ref and is deliberately not judged, and the in-place form (`-M <held>`
	// alone) moves nothing at all.
	refGuardBranchMoveFlag     = "-M"
	refGuardBranchMoveLongFlag = "--move"

	// refGuardCreateForceFlag / refGuardSwitchCreateForceFlag are checkout's and
	// switch's force-create: create the name if absent, RESET it if present.
	// switch's long spelling is measured live too
	// (refGuardSwitchCreateForceLongFlag).
	refGuardCreateForceFlag           = "-B"
	refGuardSwitchCreateForceFlag     = "-C"
	refGuardSwitchCreateForceLongFlag = "--force-create"

	// refGuardVerbFlagsTakesNoArgument is the short-flag rule for the verbs this
	// guard judges: NONE of their flags consumes the following word. `git branch -f
	// <name> <base>` parses as the flag plus two POSITIONALS (git's own getopt
	// declares -f argument-less), so reading <name> as -f's operator would shift the
	// positional list and judge the BASE — which is a read — instead of the ref being
	// moved. Measured, and the bug this constant's value replaced.
	refGuardVerbFlagsTakesNoArgument = ""

	// refGuardRefSep is git's refspec separator (`<src>:<dst>`); a word with no
	// separator names no destination.
	refGuardRefSep = ":"

	// refGuardRefspecGlob is git's refspec wildcard. It lives in the refSPEC
	// grammar (matched against a ref NAME), which is why a destination carrying
	// one is judged by its literal prefix rather than by the name test.
	refGuardRefspecGlob = "*"

	// refGuardDashCShort / refGuardDashCArgRule spell `git -C <dir>` for the
	// option scanner: the SHORT letter, and the short-flag rule that says it
	// consumes an operand (a long flag's operands are declared by its own name).
	refGuardDashCShort   = "-C"
	refGuardDashCArgRule = "C"

	// refGuardRefsRootPrefix is the ref namespace the DWIM probe walks when a name
	// is not in refs/heads (`refs/<name>`), spelled here because the guard's
	// existence test needs it and no package constant exists for the bare root.
	refGuardRefsRootPrefix = "refs/"

	// refGuardRemoteFetchKeyFmt is the config key a fetch with no refspec obeys
	// (`remote.<name>.fetch`). The EMPTY name is git's own default-remote spelling
	// of the key — `git config --get-all remote..fetch` answers the entries whose
	// remote is the empty name — so one format covers both readings.
	refGuardRemoteFetchKeyFmt = "remote.%s.fetch"

	// forEachRefVerb is `git for-each-ref`, the verb that ENUMERATES refs (the
	// wildcard fetch mapping resolves against the remote, so the guard reads the
	// local side it can see).
	forEachRefVerb = "for-each-ref"

	// refGuardLineSep splits a probe's multi-line answer.
	refGuardLineSep = "\n"

	// refGuardForEachRefFormat asks for the REF NAME alone: the guard judges the
	// name, never the value (the holder predicate is name-keyed).
	refGuardForEachRefFormat = "--format=%(refname)"

	// refGuardVerifyQuietFlag / refGuardRefQuietFlag are rev-parse's two silent
	// existence-test flags: report the ref if it exists and say NOTHING (exit 1)
	// when it does not.
	refGuardVerifyQuietFlag = "--verify"
	refGuardRefQuietFlag    = "--quiet"

	// refGuardRevParseRefMissing is git's exit code for `rev-parse --verify
	// --quiet` when the ref does not exist. It is mergeVerifyGitExitNo (1) — the
	// SAME exit code task_staleness_guard.go already names for the same question
	// (Rule 13: one definition, referenced never re-spelled).
	refGuardRevParseRefMissing = mergeVerifyGitExitNo

	// refGuardDeleteFlag / refGuardDeleteLongFlag spell update-ref's deletion
	// form, in which the refname is the flag's OPERAND rather than a later
	// positional (`git update-ref -d <refname> [<old-oid>]`) — the spelling
	// archive_ref_reclaim.go and the close-out chain both use.
	refGuardDeleteFlag     = "-d"
	refGuardDeleteLongFlag = "--delete"

	// refGuardDetachedBranch is what `git rev-parse --abbrev-ref HEAD` answers
	// for a detached HEAD: a detached checkout holds NO branch ref, so it can
	// neither be blocked nor do the blocking. It NAMES the package's one
	// spelling of that token (TaskGroundDetachedHeadBranch, exported because the
	// clone channel reads it too) rather than writing the literal again (Rule 13).
	refGuardDetachedBranch = TaskGroundDetachedHeadBranch

	// refGuardRefPeelSuffix is the peel suffix git's update-ref accepts on a
	// NEW-REF-NAME (`refs/heads/x^{commit}`, `refs/heads/x^{}`). The REFNAME is
	// the part BEFORE it, so truncating here is what makes
	// `refs/heads/<b>^{commit}` judge <b> rather than answering "not a
	// refs/heads name" (Rule 15: the guard must not fail open on a spelling).
	refGuardRefPeelSuffix = "^{"

	// refGuardMessageFlag / refGuardMessageLongFlag take a REASON operand, so
	// the word after one is prose and never a refname.
	refGuardMessageFlag     = "-m"
	refGuardMessageLongFlag = "--message"

	// refGuardStatusCommandLabel is the sandbox label the refusal offers for the
	// fixture case — a render-time value, named so the sentence and any caller
	// agree on it.
	refGuardStatusCommandLabel = "refguard"

	// refGuardReadAlternativeFmt and refGuardStatusAlternativeFmt are the two
	// NON-DESTRUCTIVE routes the refusal names, kept as constants because the
	// acceptance test addresses both strings by name (a refusal that stops a
	// session without redirecting it is the failure Rule 15 forbids).
	refGuardReadAlternativeFmt   = "`git rev-parse refs/heads/%s` reads the current value"
	refGuardStatusAlternativeFmt = "`gopi tool sandbox run --label %s -- <cmd>` gives a private chain of refs (Rule 34)"
	refGuardResetAlternativeFmt  = "`git reset` writes YOUR HEAD and never a shared ref"
	// refGuardRenameAlternativeFmt is the route for a RENAME: a rename has no
	// "current value" to read, so suggesting a read would be the "stops a session
	// without redirecting it" failure Rule 15 forbids.
	refGuardRenameAlternativeFmt = "`git rev-parse refs/heads/%s` reads what you would be renaming away"

	// refGuardInLineSeparators are the shell's sequencing operators INSIDE one
	// line. It is stashClauseSeparators without the newline, because the clause
	// scanner now consumes the newline itself (it must, to know where a
	// here-document body ends) — the SAME character set, one definition of which
	// characters cut a simple command.
	refGuardInLineSeparators = ";&|"

	// refGuardCommentPrefix is the shell's comment marker: a `#` that BEGINS a
	// word starts a comment, and the rest of that line is text rather than
	// commands (measured, task 01a1621f — the guard read a commented-out
	// invocation as a real one).
	refGuardCommentPrefix = "#"

	// refGuardHeredocTabOp / refGuardHeredocOp are the here-document operators, in
	// the order the scanner must test them so `<<-` is not read as `<<` with a
	// delimiter of `-…`.
	refGuardHeredocTabOp = "<<-"
	refGuardHeredocOp    = "<<"

	// refGuardHereStringOp is the here-STRING operator: it feeds the following
	// WORD to stdin and opens no body, so it must not queue a delimiter.
	refGuardHereStringOp = "<<<"

	// refGuardHeredocTabTrim are the characters a `<<-` body may be indented with
	// and bash still recognises its terminator (tabs only — measured: spaces are
	// not stripped by POSIX `<<-`). The trailing-space trim beside it is bash's own
	// tolerance for a CRLF-pasted terminator.
	refGuardHeredocTabTrim = "\t"

	// refGuardSingleQuote / refGuardDoubleQuote are the shell's two quoting forms.
	// NEITHER separates commands: every separator character inside a quote is
	// DATA, and a double-quoted string may even span lines — which is exactly how
	// an ordinary multi-line `git commit -m "<body>"` carries a body of prose
	// (measured, task 01a1622f: the body quoted a refused `git push …` spelling
	// and the scanner cut the message into clauses, so the guard refused the
	// commit that DOCUMENTED it). The scanner therefore tracks the quote state and
	// only the text OUTSIDE the quotes is cut.
	refGuardSingleQuote = "'"
	refGuardDoubleQuote = "\""

	// refGuardEscapeChar is the shell's backslash: outside quotes it escapes the
	// NEXT character (an escaped separator is data), and inside double quotes it
	// escapes only `$`, `` ` ``, `"`, `\` and newline — spelled here as "the next
	// character is not a separator", the property the scanner needs.
	refGuardEscapeChar = "\\"

	// refGuardWordGap are the characters that separate WORDS inside one simple
	// command (the same set strings.Fields cuts on), named because the `#`
	// comment rule keys on "does this `#` BEGIN a word".
	refGuardWordGap = " \t"
)

// refGuardHeredocOps is the operator list the scanner walks, in test order — one
// definition (Rule 14), derived from the two constant spellings above.
var refGuardHeredocOps = []string{refGuardHeredocTabOp, refGuardHeredocOp}

// refGuardVerbFlagsWithoutRef are the update-ref/symbolic-ref options whose
// FOLLOWING word is an operand but never a refname, so it must be skipped rather
// than judged. `-d`/`--delete` is deliberately absent: its operand IS the refname
// being deleted, and refGuardRefOperands handles it explicitly.
var refGuardVerbFlagsWithoutRef = map[string]bool{
	refGuardMessageFlag:     true,
	refGuardMessageLongFlag: true,
}

// ── the RESOLVED unit: ONE git invocation ────────────────────────────────────
//
// Every judgement below is made on a clause resolved on its own, because a
// `-C <dir>` operand is a property of the invocation that carries it and not of
// the line: `cd /elsewhere && git -C <repo> update-ref refs/heads/master <sha>`
// reaches <repo>'s shared namespace from a working directory the repo probes
// cannot speak for (measured, task 01a12da8). The file head's own settled design
// is kept here — one invocation, its own `-C` operand — and the resolver lives in
// shared_stash_guard.go beside the probe it feeds.
func refGuardResolve(repoDir, workdir, verb string, verbIdx int, clause []string) refGuardResolved {
	// git's OWN options live BEFORE the subcommand: scanning the whole clause for
	// `-C` reads `switch -C <name>` (the subcommand's force-create) as if it were
	// `git -C <dir>`, which mis-resolves the repository to the branch name. Only the
	// words before the verb can carry a global option (measured: the debug probe
	// that found this read repoDir="main" for `switch -C main <sha>`).
	opts := refGuardScanOptions(refGuardDashCArgRule, "", clause[:verbIdx])
	r := refGuardResolved{repoDir: repoDir, workdir: workdir, verb: verb, pos: refGuardScanOptions(refGuardVerbFlagsTakesNoArgument, "", clause[verbIdx:]).pos}
	if dir := opts.valueOf(refGuardDashCShort, gitDashCDirOption); dir != "" {
		r.dir = expandTilde(dir)
		r.repoDir = r.dir
	}
	return r
}

// refGuardOptions is one invocation's options as the scanner saw them: the flags
// that were SET, the operand each argument-taking flag captured, and the
// POSITIONAL words (everything that is neither an option nor an option's operand).
type refGuardOptions struct {
	set   map[string]bool
	value map[string]string
	pos   []string
}

// set reports whether a flag was spelled (short form, or long form with its
// leading dashes).
func (o refGuardOptions) isset(flag string) bool { return o.set[flag] }

// longSet reports whether a long flag was spelled.
func (o refGuardOptions) longSet(flag string) bool { return o.set[flag] }

// valueOf returns the operand captured for an argument-taking flag, preferring the
// SHORT spelling (they are different spellings of one flag, and either may carry
// the operand).
func (o refGuardOptions) valueOf(short, long string) string {
	if v := o.value[short]; v != "" {
		return v
	}
	return o.value[long]
}

// refGuardScanOptions walks one invocation's words (verb included) and returns its
// flags and positionals. shortArgFlags lists the single-letter flags that CONSUME
// an operand (`fM` for `git branch`), longArgFlags the long ones (`--force`); both
// accept git's two spellings of a consumed operand (`-f main`, `-fmain`,
// `--force main`, `--force=main`), which is what keeps `git branch -f <name> <base>`
// from reading <base> as a second written ref.
func refGuardScanOptions(shortArgFlags, longArgFlags string, words []string) refGuardOptions {
	o := refGuardOptions{set: map[string]bool{}, value: map[string]string{}}
	for i := 1; i < len(words); i++ {
		w := words[i]
		switch {
		case strings.HasPrefix(w, "--"):
			name, inline, hasInline := strings.Cut(w, "=")
			o.set[name] = true
			if strings.Contains(longArgFlags, name) {
				if hasInline {
					o.value[name] = inline
					continue
				}
				if i+1 < len(words) {
					o.value[name] = words[i+1]
					i++
				}
			}
		case strings.HasPrefix(w, "-") && len(w) > 1:
			for j, c := range w[1:] {
				short := "-" + string(c)
				o.set[short] = true
				if !strings.ContainsRune(shortArgFlags, c) {
					continue
				}
				if rest := w[j+2:]; rest != "" {
					o.value[short] = rest
					break
				}
				if i+1 < len(words) {
					o.value[short] = words[i+1]
					i++
				}
				break
			}
		default:
			o.pos = append(o.pos, w)
		}
	}
	return o
}

// refGuardGovernedVerb reports whether verb is one of the verbs this guard
// governs: the refs-plumbing pair, `branch`, `checkout`/`switch`, and `fetch`.
func refGuardGovernedVerb(verb string) bool {
	switch verb {
	case refGuardUpdateRefVerb, refGuardSymbolicRefVerb, refGuardBranchVerb,
		refGuardCheckoutVerb, refGuardSwitchVerb, refGuardFetchVerb:
		return true
	}
	return false
}

// guardSharedIntegrationRef reports a non-empty refusal reason when command is a
// git invocation that writes a refs/heads/<branch> held by a DIFFERENT worktree of
// the caller's repository, and "" when the command is permitted (not such a
// command at all, a refs/gopi or refs/tags target, a single-worktree repo, a
// detached caller with no other holder, or the caller's own branch).
//
// workdir is the session's working directory; it is the directory the command's
// git call resolves its repository from unless the invocation carries its own
// `-C <dir>` operand, which is honoured per INVOCATION (the same operand
// resolution shared_stash_guard.go applies: `git -C <repo> update-ref …` reaches
// the same shared namespace from anywhere).
func guardSharedIntegrationRef(command, workdir string) string {
	targets, commands := refGuardRecognisedTargets(command, workdir)
	if len(targets) == 0 {
		// The overwhelming majority of commands: no governed verb, or a verb
		// whose operands name no branch ref. Nothing is probed and nothing is
		// refused.
		return ""
	}

	// The probes are keyed by DIRECTORY, not by clause: a command that writes two
	// refs of the same repository is still one repository, and re-reading
	// `worktree list` per clause would judge the same world twice. The per-clause
	// `-C` operand is what makes the key differ (`cd /a && git -C /b … && git -C
	// /c …` is three repositories).
	type repoState struct {
		guard     refGuardCheckout
		worktrees []string
		ok        bool
	}
	states := make(map[string]repoState)

	// Every target must be judged: a chain that writes its own branch and then
	// another worktree's branch is refused BY that branch, never excused by the
	// first (clause-scoped is not clause-exhaustive — the measured residual of
	// the stash-guard family).
	for _, tgt := range targets {
		st, seen := states[tgt.repoDir]
		if !seen {
			if guard, err := refGuardCheckoutOf(tgt.repoDir); err == nil {
				if worktrees, err := refGuardWorktreePaths(tgt.repoDir); err == nil {
					st = repoState{guard: guard, worktrees: worktrees, ok: true}
				}
			}
			states[tgt.repoDir] = st
		}
		if !st.ok {
			// Not a readable checkout: git itself will fail the command loudly, and
			// there is no shared namespace to judge. (A single-checkout repository
			// falls out of refGuardBranchHolder's caller-own-entry rule: it holds
			// its own current branch, so the ordinary local flow is never refused —
			// the same threshold discipline shared_stash_guard.go uses.)
			continue
		}
		holder, reason := refGuardBranchHolder(tgt.branch, tgt.verb, st.guard, st.worktrees)
		if reason != "" {
			return reason
		}
		if holder == "" {
			continue // no worktree holds it — nothing to desync
		}
		return refGuardRefusal(tgt, holder, st.guard, strings.Join(commands, " "))
	}
	return ""
}

// refGuardRecognisedTargets is the guard's PARSE separated from its VERDICT: which
// written-branch-ref candidates a command offers, and the invoked command TEXT that
// offered them. A caller that only tests permission cannot tell "the guard judged
// this command and permitted it" from "the guard did not recognise the verb at all"
// — and the second is exactly how a widening silently stops covering a verb, so the
// acceptance set asserts recognition for every command it expects to be REFUSED, and
// asserts that the refusal quotes the command as the caller spelled it.
func refGuardRecognisedTargets(command, workdir string) ([]refGuardTarget, []string) {
	var targets []refGuardTarget
	var commands []string
	// ONE invocation can reach this walk TWICE, and that is the parse layer
	// working correctly rather than a duplicate finding: the clause
	// `OUT=$(git -C <dir> update-ref refs/heads/x deadbeef 2>&1)` is read once as
	// the clause's own words (refGuardUnwrapShellHead strips the `OUT=$( ` prefix)
	// and once again as the substitution BODY, which is the same simple command
	// spelled out. Both readings name the same write, so the target set is
	// deduplicated on (directory, invocation text) — a set of writes to judge,
	// not a log of how many ways they were parsed. Measured (task 01a1964e): the
	// undeduplicated walk handed the refusal the same ref twice.
	seen := make(map[string]bool)
	for _, clause := range refGuardClauses(stripQuotes(command), workdir) {
		for _, seg := range refGuardClauseSegments(clause) {
			words := refGuardUnwrapShellHead(seg.words)
			if tgt, ok := refGuardWrittenRefs(words, seg.runDir, workdir); ok {
				// The invocation as the caller SPELLED it (git's own options and operands
				// included) — the refusal quotes it, so a `-C <repo>` form must not be
				// rendered as a bare `git <verb>` that would read as having run elsewhere.
				invocation := strings.Join(words, " ")
				key := seg.runDir + refGuardWordJoin + invocation
				if seen[key] {
					continue
				}
				seen[key] = true
				targets = append(targets, tgt...)
				commands = append(commands, invocation)
			}
		}
	}
	return targets, commands
}

// refGuardRefusal renders the refusal for one judged write: WHAT the command would
// move, WHICH checkout holds it, the CONSEQUENCE, and the non-destructive routes —
// Rule 15's four requirements, with the route that matches the verb (a rename has
// no `rev-parse` reading, and suggesting one would be the "stops a session without
// redirecting it" failure).
func refGuardRefusal(tgt refGuardTarget, holder string, guard refGuardCheckout, commands string) string {
	var alternative string
	if tgt.renames {
		alternative = fmt.Sprintf(refGuardRenameAlternativeFmt, tgt.branch)
	} else {
		alternative = fmt.Sprintf(refGuardReadAlternativeFmt, tgt.branch)
	}
	return fmt.Sprintf(
		"`%s` from %s would rewrite the SHARED branch `%s` of %s, which is checked out by ANOTHER worktree (%s). "+
			"Every linked worktree of a repository shares `refs/heads/*` (each resolves `git rev-parse --git-common-dir` to the same directory), so this command rewrites THAT checkout's branch from here — "+
			"while its index and working tree keep the old tree, which renders as a large staged diff (`git status --porcelain` non-empty) and PARKS the merge queue (Rule 30) for every branch waiting to land. "+
			"Nothing was run and no ref was touched. "+
			"The non-destructive routes: %s; if the intent was a FIXTURE, %s or `git init` in a scratch directory outside every real repo; to move YOUR OWN position, %s",
		commands, guard.dir, tgt.branch, tgt.repoDir, holder,
		alternative,
		fmt.Sprintf(refGuardStatusAlternativeFmt, refGuardStatusCommandLabel),
		refGuardResetAlternativeFmt)
}

// refGuardRefOperands returns the refname operands of a governed verb's argv (the
// words after the verb), decoding update-ref's `-d <refname>` deletion form and
// skipping the operand of the flags that never carry a refname.
func refGuardRefOperands(argv []string) []string {
	var ops []string
	for i := 0; i < len(argv); i++ {
		w := argv[i]
		if w == refGuardDeleteFlag || w == refGuardDeleteLongFlag {
			if i+1 < len(argv) {
				ops = append(ops, refGuardTrimRefPeelSuffix(argv[i+1]))
				i++
			}
			continue
		}
		if refGuardVerbFlagsWithoutRef[w] {
			i++ // the flag's operand is a reason, not a refname
			continue
		}
		if strings.HasPrefix(w, "-") {
			continue
		}
		ops = append(ops, refGuardTrimRefPeelSuffix(w))
	}
	return ops
}

// refGuardActualBranchRef resolves a BRANCH NAME (a short refspec destination or a
// `git branch -M <new>` operand) to the full ref it names, using the SAME
// DWIM rule git applies to a positional branch name: `refs/heads/<name>` first,
// then `refs/<name>`. It reports "" when the name resolves to nothing — creating
// a NEW branch is the ordinary case and moves no held ref.
func refGuardActualBranchRef(repoDir, name string) string {
	if name == "" || strings.ContainsAny(name, refGuardRefspecGlob+refGuardRefSep) {
		return ""
	}
	candidates := []string{name}
	if !strings.HasPrefix(name, refGuardRefsRootPrefix) {
		candidates = []string{refsHeadsPrefix + name, refGuardRefsRootPrefix + name}
	}
	for _, ref := range candidates {
		code, err := gitProbeExitCode(repoDir, "rev-parse", refGuardVerifyQuietFlag, refGuardRefQuietFlag, ref)
		if err == nil && code == 0 {
			return ref
		}
	}
	return ""
}

// refGuardTrimRefPeelSuffix truncates update-ref's peel suffix from a
// NEW-REF-NAME (`refs/heads/x^{commit}` → `refs/heads/x`), so the namespace test
// judges the REFNAME rather than answering "not a branch ref" for a spelling git
// itself rejects when it does not name a ref.
func refGuardTrimRefPeelSuffix(word string) string {
	if i := strings.Index(word, refGuardRefPeelSuffix); i >= 0 {
		return word[:i]
	}
	return word
}

// refGuardResolved is ONE git invocation resolved on its own: the subcommand git
// was handed, the words after it, the POSITIONAL words (options and their operands
// removed), and the directory it resolves its repository against — the invocation's
// own `-C <dir>` operand when it carries one, else the session directory.
type refGuardResolved struct {
	verb    string
	argv    []string
	pos     []string
	dir     string
	repoDir string
	workdir string
}

// refGuardTarget is the resolved form of ONE written-branch-ref candidate: the
// ref, the verb that would write it, the repository the decision is made against,
// and the two properties the refusal's wording depends on.
type refGuardTarget struct {
	// ref is the full refname that would be written (always refs/heads/<branch>).
	ref string
	// branch is ref's short branch name — what the refusal quotes and what the
	// holder predicate is asked about.
	branch string
	// verb is the git subcommand the caller actually spelled, so the diagnostic
	// quotes the invocation rather than a normalised form of it.
	verb string
	// repoDir is the repository THIS invocation resolves against: the clause's own
	// `-C <dir>` operand when it carries one, else the session directory.
	repoDir string
	// renames marks a ref-MOVING RENAME (`git branch -M <held> <new>`), whose
	// non-destructive route is different from a value write's.
	renames bool
}

// refGuardWrittenRefs returns every `refs/heads/<branch>` ref that ONE resolved git
// invocation would WRITE, in operand order, deduped.
//
// Per verb, because the write target is positional and differs:
//
//   - `update-ref` — every refs/heads-prefixed operand is a written refname (the
//     `<new-oid>`, which can never start with `refs/`, is the other shape), so
//     `git update-ref refs/heads/b <oid>` and `git update-ref <oid> refs/heads/b`
//     are both judged;
//   - `symbolic-ref` — the FIRST operand is the ref being SET and the second is
//     the ref it is pointed AT, which is only read. Judging the target would
//     refuse `git symbolic-ref HEAD refs/heads/master`, a command that writes no
//     branch ref at all;
//   - `branch -f|--force <name> <base>` — the FIRST operand is the ref moved onto
//     `base`, and base is READ (measured: `git branch -f <free> main` leaves
//     refs/heads/main untouched), which is what refGuardBranchRuleNoArg's "f takes
//     an argument" reading buys;
//   - `branch -M|--move <held> <new>` — the rename DESTINATION is the ref written.
//     Renaming a held branch AWAY moves no ref (measured: a rename leaves the
//     destination byte-identical and stages nothing), so only the destination is
//     judged; the destination may be a refname, a namespaced name, or a name that
//     does not exist yet (resolved through the local ref DWIM);
//   - `checkout -B` / `switch -C|--force-create <name>` — `-B`/`-C` RESET the name
//     through a real ref write even when the caller is detached (measured: from a
//     detached HEAD, `checkout -B main <sha>` moves the held ref), so <name> is
//     judged whether it exists or not. `git checkout -b` / `switch -c` are
//     deliberately NOT judged: they refuse an existing name, so git is already the
//     guard, and a refusal here would take a lane gopi's own branch creation uses;
//   - `fetch` — the refspec DESTINATIONS, plus (measured, the sixth hole) every
//     destination a configured `remote.<name>.fetch` mapping would write when the
//     invocation carries no refspec of its own. A refspec with no `:` writes no
//     local ref.
func refGuardWrittenRefs(clause []string, fallbackRepoDir, workdir string) ([]refGuardTarget, bool) {
	verb, argv, ok := refGuardGitSubcommand(clause)
	if !ok || !refGuardGovernedVerb(verb) {
		return nil, false
	}
	// The verb's own index: refGuardGitSubcommand returns everything AFTER it, so
	// the words before the verb (the only ones that can carry a global option) are
	// clause[:verbIdx].
	verbIdx := len(clause) - len(argv) - 1
	r := refGuardResolve(fallbackRepoDir, workdir, verb, verbIdx, clause)
	targets := refGuardVerbTargets(r, argv)
	if len(targets) > 0 {
		return targets, true
	}
	// A governed verb that writes nothing is still a governed invocation and is
	// reported as such: the caller uses this to tell "there was a fetch here" from
	// "there was no git at all" (the clause list is only advisory).
	return nil, true
}

// refGuardVerbTargets dispatches one resolved invocation to its verb's write-target
// reader.
func refGuardVerbTargets(r refGuardResolved, argv []string) []refGuardTarget {
	switch r.verb {
	case refGuardUpdateRefVerb, refGuardSymbolicRefVerb:
		return refGuardPlumbingTargets(r, argv)
	case refGuardBranchVerb:
		return refGuardBranchTargets(r, argv)
	case refGuardCheckoutVerb, refGuardSwitchVerb:
		return refGuardCheckoutTargets(r, argv)
	case refGuardFetchVerb:
		return refGuardFetchTargets(r, argv)
	}
	return nil
}

// refGuardPlumbingTargets reads update-ref/symbolic-ref's written refnames and
// keeps the refs/heads ones.
func refGuardPlumbingTargets(r refGuardResolved, argv []string) []refGuardTarget {
	ops := refGuardRefOperands(argv)
	if r.verb == refGuardSymbolicRefVerb && len(ops) > 1 {
		ops = ops[:1]
	}
	var out []refGuardTarget
	seen := make(map[string]bool)
	for _, op := range ops {
		if t, ok := refGuardBranchTarget(r, op, false); ok && !seen[t.ref] {
			seen[t.ref] = true
			out = append(out, t)
		}
	}
	return out
}

// refGuardBranchTarget turns a WRITTEN refname into a target when it is in the
// refs/heads namespace, resolving a SHORT name the way git's own DWIM does (with
// refGuardActualBranchRef) when shortName is set.
func refGuardBranchTarget(r refGuardResolved, ref string, shortName bool) (refGuardTarget, bool) {
	if shortName {
		ref = refGuardActualBranchRef(r.repoDir, ref)
	}
	if !strings.HasPrefix(ref, refsHeadsPrefix) {
		return refGuardTarget{}, false
	}
	return refGuardTarget{
		ref:     ref,
		branch:  strings.TrimPrefix(ref, refsHeadsPrefix),
		verb:    r.verb,
		repoDir: r.repoDir,
	}, true
}

// refGuardBranchTargets reads `git branch`'s ref-MOVING forms. Every other
// spelling of the verb (create, list, delete, copy) writes no held ref on its own.
func refGuardBranchTargets(r refGuardResolved, argv []string) []refGuardTarget {
	flags := refGuardScanOptions(refGuardVerbFlagsTakesNoArgument, "", append([]string{r.verb}, argv...))
	// git's rename grammar, MEASURED: `-M` is rename+force, `--move` alone is
	// rename-without-force and is REFUSED by git when the destination exists
	// (`A branch named 'main' already exists.`), and `--move --force` is `-M`'s
	// long spelling. So a rename that can overwrite an existing ref is exactly the
	// forced form, and the unforced form writes nothing the guard must judge.
	renameForce := flags.isset(refGuardBranchMoveFlag) ||
		(flags.longSet(refGuardBranchMoveLongFlag) && flags.longSet(refGuardBranchForceLongFlag)) ||
		(flags.longSet(refGuardBranchMoveLongFlag) && flags.isset(refGuardBranchForceFlag))
	switch {
	case renameForce:
		// The rename DESTINATION (`<old> <new>`): only a destination that already
		// resolves can be a ref written OVER — a new name is the ordinary branch
		// rename and writes no ref anybody held. (`git branch -M <held>` alone is the
		// in-place form: measured to move no ref at all.)
		if len(flags.pos) > 1 {
			if t, ok := refGuardBranchTarget(r, flags.pos[1], true); ok {
				t.renames = true
				return []refGuardTarget{t}
			}
		}
	case flags.longSet(refGuardBranchForceLongFlag) || flags.isset(refGuardBranchForceFlag):
		// `-f <name> <base>`: the FIRST POSITIONAL is the ref moved, the second is
		// the base it is moved onto, which git only READS (measured:
		// `git branch -f <free> <held>` leaves the held ref untouched).
		if len(flags.pos) > 0 {
			// The name is a BRANCH NAME and git prefixes it with refs/heads/
			// LITERALLY, even when it already looks like a full refname: MEASURED
			// (2026-10-12) `git branch -f refs/heads/main <sha>` from a linked
			// worktree creates `refs/heads/refs/heads/main` and leaves the held ref
			// alone. So this target is built by prefixing, never by resolving — and
			// the harmless nested-name spelling is therefore permitted, which is what
			// keeps the guard from refusing a command that cannot desync anything.
			if t, ok := refGuardBranchTarget(r, refGuardHeadsPrefixedName(flags.pos[0]), false); ok {
				return []refGuardTarget{t}
			}
		}
	}
	return nil
}

// refGuardCheckoutTargets reads checkout's and switch's force-CREATE forms, whose
// ref write happens whether or not the name already exists.
func refGuardCheckoutTargets(r refGuardResolved, argv []string) []refGuardTarget {
	flags := refGuardScanOptions(refGuardVerbFlagsTakesNoArgument, "", append([]string{r.verb}, argv...))
	forced := flags.isset(refGuardCreateForceFlag) || flags.isset(refGuardSwitchCreateForceFlag) ||
		flags.longSet(refGuardSwitchCreateForceLongFlag)
	if !forced || len(flags.pos) == 0 {
		return nil
	}
	if t, ok := refGuardBranchTarget(r, flags.pos[0], true); ok {
		return []refGuardTarget{t}
	}
	return nil
}

// refGuardFetchTargets reads fetch's refspec DESTINATIONS: the `<dst>` of every
// `<src>:<dst>` / `+<src>:<dst>` operand in the invocation, plus every destination
// the configured `remote.<name>.fetch` mappings would write when the invocation
// carries no refspec of its own (measured: with `remote.origin.fetch` set to
// `+refs/heads/*:refs/heads/*` and a DETACHED caller, a bare `git fetch origin`
// moves a foreign-held branch with exit 0).
//
// The destination is a REF NAME, not a shell word: it is a literal refname, or a
// SHORT name that git resolves with the same DWIM a positional branch name gets
// (`src:main` writes refs/heads/main), or a wildcard mapping whose refs/heads
// prefix is what decides the namespace.
func refGuardFetchTargets(r refGuardResolved, argv []string) []refGuardTarget {
	var out []refGuardTarget
	seen := make(map[string]bool)
	add := func(ref string) {
		t, ok := refGuardBranchTarget(r, ref, true)
		if ok && !seen[t.ref] {
			seen[t.ref] = true
			out = append(out, t)
		}
	}
	// A refspec operand is the one that carries a `:` and is not a remote NAME: the
	// first non-option operand is the repository (or a remote group), and only the
	// operands AFTER it are refspecs. An option-driven spelling (`--all`,
	// `--multiple`) makes the operand slot ambiguous, which is why the CONFIGURED
	// mappings are read as well rather than instead.
	remote := ""
	for _, op := range r.pos {
		if strings.HasPrefix(op, refGuardRefsRootPrefix) || strings.Contains(op, refGuardRefSep) {
			if dst, ok := refGuardRefspecDest(op); ok {
				add(dst)
			}
			continue
		}
		if remote == "" && !refGuardLooksLikeRefspecSource(op) {
			remote = op
		}
	}
	for _, dst := range refGuardConfiguredFetchDests(r.repoDir, remote) {
		add(dst)
	}
	return out
}

// refGuardLooksLikeRefspecSource reports whether a non-option fetch operand is a
// REFSPEC whose destination is on the other side of its own separator — a word
// like `refs/heads/x` with no `:` names no destination and is merely a source.
func refGuardLooksLikeRefspecSource(op string) bool {
	return strings.Contains(op, refGuardRefSep)
}

// refGuardRefspecDest returns the DESTINATION of one fetch refspec, and whether it
// has one at all (`<src>` alone writes only FETCH_HEAD). The `+` force marker and a
// leading `refs/heads/` on the source are both git's grammar, not paths.
func refGuardRefspecDest(refspec string) (string, bool) {
	spec := strings.TrimPrefix(refspec, "+")
	_, dst, ok := strings.Cut(spec, refGuardRefSep)
	if !ok || dst == "" {
		return "", false
	}
	return dst, true
}

// refGuardConfiguredFetchDests returns the refs/heads destinations the configured
// `remote.<name>.fetch` mappings would write. It is read from the repository's OWN
// config (the same file `git config` reads), through the SAME git probe every other
// question here uses, so the guard and git cannot disagree about what is
// configured. An answer it cannot read yields no destinations, which is the
// pre-existing behaviour for every fetch this guard already saw.
func refGuardConfiguredFetchDests(repoDir, remote string) []string {
	out, err := gitProbeOutput(repoDir, "config", "--get-all", refGuardRemoteFetchKey(remote))
	if err != nil {
		return nil
	}
	var dests []string
	for _, line := range strings.Split(out, "\n") {
		dst, ok := refGuardRefspecDest(strings.TrimSpace(line))
		if !ok {
			continue
		}
		// A wildcard destination maps a whole namespace; only a refs/heads one can
		// write a branch ref, and the literal prefix is what decides it.
		// A wildcard destination maps a whole NAMESPACE, and git decides which
		// branches it writes by what the remote has — which the guard cannot know
		// without asking the remote. What it CAN read is which local branches such a
		// mapping would rewrite: every `refs/heads/*` ref of THIS repository. So the
		// branches are enumerated, and the judgement is made per branch — the same
		// holder predicate, no new rule. A non-refs/heads wildcard (git's own DEFAULT
		// `+refs/heads/*:refs/remotes/<remote>/*`) writes tracking refs only and is
		// therefore left alone, which is why an ordinary `git fetch <remote>` stays
		// permitted.
		if strings.Contains(dst, refGuardRefspecGlob) {
			if !strings.HasPrefix(dst, refsHeadsPrefix) {
				continue
			}
			dests = append(dests, refGuardLocalBranchRefs(repoDir)...)
			continue
		}
		dests = append(dests, dst)
	}
	return dests
}

// refGuardHeadsPrefixedName is the ref `git branch <flag> <name>` writes: git
// prefixes the operand with refs/heads/ LITERALLY (measured: an operand already
// spelled refs/heads/main becomes refs/heads/refs/heads/main).
func refGuardHeadsPrefixedName(name string) string {
	if strings.HasPrefix(name, refsHeadsPrefix) {
		return refGuardRefsRootPrefix + name
	}
	return refsHeadsPrefix + name
}

// refGuardLocalBranchRefs returns this repository's local branch refs, for the
// wildcard mapping whose destinations git resolves against the REMOTE rather than
// against a constant (the guard reads the local side, which is the side the damage
// would land on).
func refGuardLocalBranchRefs(repoDir string) []string {
	out, err := gitProbeOutput(repoDir, forEachRefVerb, refGuardForEachRefFormat, refsHeadsPrefix)
	if err != nil || out == "" {
		return nil
	}
	var refs []string
	for _, line := range strings.Split(out, refGuardLineSep) {
		if line = strings.TrimSpace(line); line != "" {
			refs = append(refs, line)
		}
	}
	return refs
}

// refGuardRemoteFetchKey builds `remote.<name>.fetch`. A fetch with no remote
// operand falls back to git's own default remote for the current branch, which the
// guard resolves through the same probe rather than guessing a name.
func refGuardRemoteFetchKey(remote string) string {
	if remote == "" {
		return refGuardRemoteFetchKeyFmt
	}
	return fmt.Sprintf(refGuardRemoteFetchKeyFmt, remote)
}

// refGuardClause is ONE simple command of a command line: the words the shell
// will execute, and the directory the shell has SELECTED by the time it does.
//
// The directory is carried PER CLAUSE and not read from the session, because a
// shell command line selects its own run directory: `cd <dir> && git fetch …`
// runs that fetch in <dir>, not where the caller stood (measured, task
// 01a1621f — the guard judged such a fetch in the session's clone, refused it,
// and NAMED that clone, a tree the command never touches).
type refGuardClause struct {
	// runDir is the directory commands in this clause run in — the session
	// workdir, or the directory a preceding `cd` on the same line selected.
	runDir string
	// words are the clause's words with the leading VAR=VAL prefix removed.
	words []string
}

// refGuardRunDir resolves the directory a simple command runs in from the three
// sources the shell itself distinguishes, in order of authority: the workdir the
// CALLER named for the command (the bash tool's own parameter — the process's
// cwd when the tool starts it, so it outranks anything the command line says),
// the directory a `cd` selected on the command line, and the process cwd.
//
// One definition, two readers (Rule 14): the clause scanner below and the
// stash guard's repository resolver both ask this question.
func refGuardRunDir(selected, workdir string) string {
	if selected != "" {
		return selected
	}
	if workdir != "" {
		return workdir
	}
	cwd, err := os.Getwd()
	if err != nil {
		// No cwd to name: the judgement has no directory to speak about, and every
		// caller treats "" as "nothing to judge" rather than guessing one.
		return ""
	}
	return cwd
}

// refGuardClauses splits a quote-stripped command into its simple commands, each
// carrying the run directory the shell has selected for it. The separators are the
// shared ones (`;`, `&`, `|`, newline) — the SAME constant shared_stash_guard.go
// and chat_write_guard.go split on (Rule 14: one definition, three consumers).
//
// Two shell facts the split must honour, both measured (task 01a1621f):
//
//   - a HERE-DOCUMENT body is DATA, not commands. `cat <<'EOF' … git update-ref
//     refs/heads/main … EOF` executes one `cat`; the governed verb inside the body
//     is a string. Reading it as a clause refused a documentation write whose only
//     "git" was the text of a document, so the body is skipped up to its
//     terminator line (the delimiter bash itself compares against), in the order
//     the delimiters were opened;
//   - a `#` that begins a word starts a comment to the end of the line, and the
//     rest of that line is text too — the same defect in its other spelling.
//
// The cd carry is replayed per clause because a `cd` only affects the commands
// that FOLLOW it on the line, and an unresolvable `cd` (a shell expansion) keeps
// the last directory that could be resolved rather than abandoning it.
//
// THE THIRD SHELL FACT, and the one this scanner was missing (measured, task
// 01a1622f): a QUOTED string is DATA end to end. The shell cuts simple commands
// only OUTSIDE quotes, so `git commit -m "<a body that quotes a refused push>"`
// is ONE simple command whose last word is a message — but the scanner cut on
// every separator it met, including the ones inside the message, and handed the
// guard a `git push` clause that the run never spelled as a command. The guard
// then refused the commit that DOCUMENTED its own refusal. So the scan direction
// is character-wise and quote-aware: `'…'` and `"…"` are copied through
// (newlines included — a multi-line `-m` body is the ordinary shape), a
// backslash outside single quotes protects the next character, and only the
// UNQUOTED text is cut into clauses.
func refGuardClauses(stripped, workdir string) []refGuardClause {
	var clauses []refGuardClause
	var selected string
	var pendingDelims []string
	var buf []byte
	var quote byte // 0 = unquoted; otherwise the quote character still open
	// nest counts the OPEN nesting constructs the scan is inside — a `$( … )`,
	// a bare `( … )` group, a `` `…` `` body. It exists because a separator
	// inside one of them belongs to the NESTED sequence, never to the caller's
	// clause list: the shell runs `OUT=$(cd /shared && git …)` as one simple
	// command whose word list merely CONTAINS a sequence, and cutting at that
	// `&&` produced two clauses — the second of which carried no `cd`, so the
	// git invocation was judged against the caller's directory and passed
	// (measured, task 01a1964e: the substitution body arrived truncated and
	// half of it was never read as a command at all).
	var nest int
	var backtick int // 0/1: a backtick body is toggled, not counted (same char both ends)

	// emit closes the simple command accumulated in buf, if any.
	emit := func() {
		raw := strings.TrimSpace(string(buf))
		buf = buf[:0]
		if raw == "" {
			return
		}
		words := strings.Fields(raw)
		if len(words) == 0 {
			return
		}
		// Every here-document opened in this simple command contributes a body
		// that starts on the NEXT line, so the delimiters are queued in the order
		// they were opened.
		pendingDelims = append(pendingDelims, refGuardHeredocDelimiters(words)...)
		// Drop the leading VAR=VAL environment prefix: `GIT_DIR=/x git
		// update-ref …` is still a git invocation, and the prefix is not the
		// command head.
		// A leading VAR=VAL word is an ENVIRONMENT PREFIX, not the command head —
		// EXCEPT when its VALUE opens a nesting construct: `OUT=$(git -C <dir>
		// checkout -b X)` assigns the OUTPUT of a command, and dropping that word
		// drops the command itself. Measured (row 01a1964f): the head word
		// `OUT=$(git` was consumed by this loop, the clause that reached the
		// recogniser began at `-C`, and the git invocation inside the substitution
		// became invisible — the whole bypass in one line.
		i := 0
		for i < len(words) && envAssignPattern.MatchString(words[i]) &&
			!refGuardWordCarriesNestingOpener(words[i]) {
			i++
		}
		// Shell COMPOUND-COMMAND keywords are syntax, not a command: `then git …
		// checkout -b X` runs the git invocation, and a recogniser reading the
		// clause's head word saw `then` and answered "not a git invocation". They
		// carry no value of their own, so dropping them here is safe for every
		// reader — unlike the VAR=VAL prefix above, whose VALUE can hold a command.
		for i < len(words) && refGuardShellCompoundKeywords[words[i]] {
			i++
		}
		if i >= len(words) {
			return
		}
		// A `cd` selects the directory for the clauses that follow it; it is not
		// itself a command anybody judges (it writes no ref and no file), so it
		// only advances the carry.
		if dir, ok := commandChangeDir(strings.Join(words, " ")); ok {
			if resolved, ok := absolutize(dir, refGuardRunDir(selected, workdir), nil); ok {
				selected = resolved
			}
		}
		clauses = append(clauses, refGuardClause{
			runDir: refGuardRunDir(selected, workdir),
			words:  words[i:],
		})
	}

	for i := 0; i < len(stripped); i++ {
		c := stripped[i]
		if quote == 0 {
			if len(pendingDelims) > 0 {
				// Inside a here-document body: the rest of this LINE is DATA. The
				// body is skipped up to the terminator line the shell compares
				// against, in the order the delimiters were opened.
				if nl := strings.IndexByte(stripped[i:], refGuardLineSep[0]); nl >= 0 {
					line := stripped[i : i+nl]
					if refGuardHeredocTerminates(line, pendingDelims[0]) {
						pendingDelims = pendingDelims[1:]
					}
					i += nl
					continue
				}
				line := stripped[i:]
				if refGuardHeredocTerminates(line, pendingDelims[0]) {
					pendingDelims = pendingDelims[1:]
				}
				i = len(stripped)
				continue
			}
			if c == refGuardCommentPrefix[0] && (len(buf) == 0 || strings.IndexByte(refGuardWordGap, buf[len(buf)-1]) >= 0) {
				// A `#` that BEGINS a word starts a comment: the rest of the line is
				// text, and text is not a command.
				if nl := strings.IndexByte(stripped[i:], refGuardLineSep[0]); nl >= 0 {
					i += nl - 1 // the newline itself is consumed by the clause cut
					continue
				}
				break
			}
			if c == refGuardSingleQuote[0] || c == refGuardDoubleQuote[0] {
				quote = c
				buf = append(buf, c)
				continue
			}
			// A NESTING construct opens HERE and its separators belong to the
			// nested sequence, so they must not cut the caller's clause list: the
			// whole construct stays in ONE clause and the segment layer
			// (refGuardClauseSegments) re-parses its body. Cutting inside it is
			// what truncated the substitution body and lost the `cd` that selected
			// the shared checkout (measured, task 01a1964e).
			if strings.HasPrefix(stripped[i:], refGuardShellSubstOpen) {
				nest++
				buf = append(buf, refGuardShellSubstOpen...)
				i += len(refGuardShellSubstOpen) - 1
				continue
			}
			if len(stripped[i:]) >= len(refGuardShellBacktick) && strings.HasPrefix(stripped[i:], refGuardShellBacktick) {
				// ``` is the one delimiter whose OPEN and CLOSE are the same
				// character, so it toggles rather than counts.
				if backtick == 0 {
					backtick = 1
				} else {
					backtick = 0
				}
				buf = append(buf, c)
				continue
			}
			if c == refGuardShellSubstOpenChar && (len(buf) == 0 || strings.IndexByte(refGuardWordGap, buf[len(buf)-1]) >= 0) {
				// A bare `(` at a WORD BOUNDARY opens a subshell GROUP: `( cd /x &&
				// git … )` is a sequence of its own. An unquoted `(` anywhere else
				// is a syntax error in the shell, so this test needs no more.
				nest++
				buf = append(buf, c)
				continue
			}
			if c == refGuardShellSubstCloseChar && (nest > 0 || backtick > 0) {
				if nest > 0 {
					nest--
				}
				buf = append(buf, c)
				continue
			}
			if c == refGuardEscapeChar[0] ||
				(len(pendingDelims) == 0 && nest == 0 && backtick == 0 && refGuardIsClauseSeparator(rune(c), buf)) {
				if c == refGuardEscapeChar[0] {
					// An escaped character is data: copy it and the one it protects.
					if i+1 < len(stripped) {
						buf = append(buf, stripped[i+1])
						i++
					}
					continue
				}
				emit()
				continue
			}
			buf = append(buf, c)
			continue
		}
		// Inside a quote: everything is data. A backslash in DOUBLE quotes still
		// protects the next character; in SINGLE quotes it is literal.
		if c == quote {
			quote = 0
			buf = append(buf, c)
			continue
		}
		if c == refGuardEscapeChar[0] && quote == refGuardDoubleQuote[0] && i+1 < len(stripped) {
			buf = append(buf, stripped[i+1])
			i++
			continue
		}
		buf = append(buf, c)
	}
	emit()
	return clauses
}

// refGuardHeredocDelimiters returns the here-document delimiters the words of ONE
// simple command open, in the order they are opened. Both spellings are read
// (`<<WORD`, `<<-WORD`, and the detached `<< WORD`) and the here-STRING operator
// (`<<<word`, which has no body) is deliberately not one of them.
func refGuardHeredocDelimiters(words []string) []string {
	var delims []string
	for i := 0; i < len(words); i++ {
		w := words[i]
		if strings.HasPrefix(w, refGuardHereStringOp) {
			// `cat <<<"$x"` feeds its next word to the command's stdin: no body
			// follows it on later lines, so nothing is skipped.
			i++
			continue
		}
		for _, op := range refGuardHeredocOps {
			rest, ok := strings.CutPrefix(w, op)
			if !ok {
				continue
			}
			if rest == "" {
				// The detached spelling: the delimiter is the following word.
				if i+1 < len(words) {
					rest = words[i+1]
					i++
				}
			}
			if rest != "" {
				delims = append(delims, rest)
			}
			break
		}
	}
	return delims
}

// refGuardHeredocTerminates reports whether line is the terminator of a
// here-document opened with delim. bash compares the WHOLE line against the
// delimiter (a `<<-` body has its leading tabs stripped, so those are tolerated).
func refGuardHeredocTerminates(line, delim string) bool {
	return strings.TrimRight(strings.TrimLeft(line, refGuardHeredocTabTrim), "\r ") == delim
}

// refGuardGitSubcommand returns the git SUBCOMMAND a clause invokes plus the
// words after it, and whether the clause invokes git at all.
//
// The command head must be `git` itself, preceded only by wrapper commands
// (`sudo`, `xargs`, …), THEIR OWN OPTIONS AND THE OPERANDS THOSE OPTIONS CONSUME
// (`sudo -u <user> git push`, `env FOO=1 git push`) — the discriminator that
// keeps `echo git update-ref refs/heads/master` (no git invocation) from reaching
// a probe, the same false-positive class shared_stash_guard.go was measured on.
//
// THE HEAD WALK IS SHARED (gitCommandHeadIdx), because the operand rule is the
// half that was missing here: this function used to skip `-u` as "an option" and
// then refuse `nick` as "a foreign word", so `sudo -u nick git push --force` read
// as NO git invocation and the ref-move fence never saw the push (measured, task
// 01a19d8a).
func refGuardGitSubcommand(words []string) (verb string, rest []string, ok bool) {
	head, ok := gitCommandHeadIdx(words, gitCommandPreambleWords)
	if !ok {
		return "", nil, false
	}
	// Between the command word and the subcommand there may only be git's own
	// options and their operands (`git -C <dir> update-ref …`).
	i := head + 1
	for i < len(words) {
		w := words[i]
		if gitOptionsWithOperand[w] {
			i += 2
			continue
		}
		if strings.HasPrefix(w, "-") {
			i++
			continue
		}
		return w, words[i+1:], true
	}
	return "", nil, false
}

// refGuardCheckout is the caller's own checkout, as the probe answers it.
type refGuardCheckout struct {
	dir string
	// branch is the branch ref this checkout holds ("" for a detached HEAD).
	branch string
	// linked is true when dir is a linked worktree rather than the main
	// checkout. It is carried for the refusal's wording, never for the verdict:
	// the main checkout is permitted by the HOLDER test (it holds its own
	// current branch), not by a special case.
	linked bool
}

// refGuardCheckoutOf resolves the checkout at dir: the branch ref it holds, and
// whether it is a linked worktree of its repository.
func refGuardCheckoutOf(dir string) (refGuardCheckout, error) {
	c := refGuardCheckout{dir: dir}
	name, err := gitProbeOutput(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return c, err
	}
	name = strings.TrimSpace(name)
	if name != "" && name != refGuardDetachedBranch {
		c.branch = name
	}
	gitDir, err := gitProbeOutput(dir, "rev-parse", "--git-dir")
	if err != nil {
		return c, err
	}
	commonDir, err := gitProbeOutput(dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return c, err
	}
	c.linked = refGuardAbs(dir, gitDir) != refGuardAbs(dir, commonDir)
	return c, nil
}

// refGuardAbs resolves a possibly-relative git path answer against dir, so the
// main checkout (which answers `.git` twice) and a linked worktree (which
// answers an absolute common dir and a `<common>/worktrees/<name>` git dir) are
// compared as the same kind of thing.
func refGuardAbs(dir, p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	return filepath.Clean(p)
}

// refGuardWorktreePaths lists every worktree of the repository AS OF NOW, by
// parsing `git worktree list --porcelain` — read fresh on every guarded command,
// because the merger and the close-out chain add, remove and reclaim worktrees
// continuously and a cached list would judge against a stale world.
func refGuardWorktreePaths(repoDir string) ([]string, error) {
	out, err := gitProbeOutput(repoDir, finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), finishCleanupPorcelainWorktreePrefix)
		if !ok {
			continue
		}
		if p := strings.TrimSpace(rest); p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// refGuardBranchHolder finds the worktree that holds branch, EXCLUDING the
// caller's own checkout. It returns:
//
//	(holder, "") — the branch is held by another worktree: the caller must be
//	               refused, and holder is its path;
//	("", "")     — no worktree holds it: nothing can desync;
//	("", reason) — the question could not be ANSWERED, so the write is refused
//	               with reason (Rule 15: an unjudged write to a shared namespace
//	               is never permitted).
//
// A worktree DIRECTORY that no longer exists is skipped — it provably holds no
// branch, and a concurrent `worktree remove` must not freeze every guarded
// command. A worktree that exists but whose checkout cannot be read is a
// different fact and fails closed.
//
// verb is the invoking verb, carried only so the unknown-answer refusal quotes
// the command the caller actually spelled.
func refGuardBranchHolder(branch, verb string, guard refGuardCheckout, worktrees []string) (holder, reason string) {
	callerCovered := false
	var unreadable []string
	for _, wt := range worktrees {
		if refGuardSameDir(wt, guard.dir) {
			// ONLY the caller's own entry is excused. Its own current branch is
			// the main checkout's `master` during a merger round, which is what
			// keeps the merger's own integration-ref work permitted.
			callerCovered = true
			continue
		}
		if _, err := os.Stat(wt); err != nil {
			continue // vanished mid-scan: holds nothing
		}
		other, err := refGuardCheckoutOf(wt)
		if err != nil {
			unreadable = append(unreadable, wt)
			continue
		}
		if other.branch == branch {
			return wt, ""
		}
	}
	if len(unreadable) > 0 {
		return "", fmt.Sprintf(
			"`git %s` from %s could not be judged: %s could not be read, so whether the shared branch `%s` is checked out elsewhere is UNKNOWN — refusing rather than permitting an unjudged write into a namespace every worktree shares (Rule 15)",
			verb, guard.dir, strings.Join(unreadable, ", "), branch)
	}
	if !callerCovered {
		// The caller's own directory is not among the repository's worktrees:
		// the scan cannot attribute the branch to anybody, so it is refused as
		// unknown rather than read as "free".
		return "", fmt.Sprintf(
			"`git %s` from %s could not be judged: the caller's own checkout is not among the worktrees git lists for its repository, so whether the shared branch `%s` is checked out elsewhere is UNKNOWN — refusing rather than permitting an unjudged write",
			verb, guard.dir, branch)
	}
	return "", ""
}

// refGuardSameDir reports whether two paths name the same directory, resolving
// symlinks when both exist (a worktree recorded by one spelling and probed by
// another must not read as "a different worktree" — that would refuse the
// caller's own branch).
func refGuardSameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}
