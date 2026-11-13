package tools

// Shape-based path/command policy — the blast-radius guard for operations that
// destroy or create a whole tree.
//
// WHY A SHAPE POLICY, AND NOT A COMMAND BLACKLIST: the pre-existing guard
// (rejectDangerous, bash.go) is a short list of dangerous PATTERNS plus a
// working-directory whitelist. That list is a blacklist by construction: it
// names "rm -rf" and "mkfs." and can only ever refuse what somebody remembered
// to write down, while the 2026-09-13 /dev/shm assessment measured the real
// exposure to be a WRITE of several hundred MB outside the session's own tree
// (19 linked worktrees = 1 015 MB in a tmpfs whose cap nobody chose, with
// MemFree at 265 MB and a mostly-spent swap cushion) and a recursive delete of
// a path that is not the agent's to delete. Enumerating forbidden commands
// rots; a property does not. So this file states the property:
//
//	an operation that creates or destroys a whole tree may only touch a
//	DECLARED ROOT — the repo's worktree root (internal/worktreeroots, declared
//	in the settings.json repos table, default <home>/workspace), the tmpfs
//	scratch home, or the throwaway system temp root.
//
// and enforces it on the RESOLVED path, because a string compare is exactly the
// bug that lets "/dev/shm/x" through when the root is spelled through a symlink
// (the scratch home on this host IS such a symlink).
//
// TWO CHECKS, one file:
//
//  1. worktree-target: `git worktree add <path>` — the operation the assessment
//     measured filling /dev/shm — must name a path inside a declared root. The
//     path is POSITIONAL, so branch/flag values are resolved first (a naive
//     "first non-flag token" read of `git worktree add -b feat/x ~/workspace/wt`
//     picks the BRANCH and would refuse every legitimate worktree).
//  2. destructive-target: a tree-level destroy (recursive delete, recursive
//     chown/chmod, mount/umount, filesystem creation, dd to a device, shred,
//     find -delete, a shell write redirection) must target a declared root.
//
// The NEGATIVE probe lives in bash_path_policy_test.go: an in-root path is
// still allowed — the refusal is a property check, never a blanket ban. That
// matters because a blanket ban on destruction would break the very cleanup the
// system must keep doing (a reclaim, a fixture teardown, a scratch GC).

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/worktreeroots"
)

const (
	// shellExpansionChars are the characters that make a token's VALUE depend on
	// the shell at run time: `$` opens a variable or a command substitution and
	// a backtick opens a command substitution. A token carrying one of them has
	// no statically known value, so the path the operating system will act on is
	// not the path this policy judges.
	//
	// It is a CONSTANT (Rule 1b) because the two arms of the policy — the
	// destructive-token arm and the worktree-target arm — must refuse the same
	// spelling. They did not: the destructive arm refused it and the worktree
	// arm resolved the token as a literal RELATIVE path, so `V=<out-of-root>;
	// git worktree add "$V"` was judged against a FABRICATED in-root path and
	// allowed (measured 2026-10-12 on the live binary — the asymmetry this
	// constant exists to remove).
	shellExpansionChars = "$`"

	// gitBinaryName is the command whose worktree subcommands this policy
	// inspects. Named (Rule 1b) because the token match below and the refusal
	// text must agree on one spelling.
	gitBinaryName = "git"

	// gitDirOptionName is git's GLOBAL option that names the repository to
	// operate on. Named (Rule 1b): the option-shape rule below builds both the
	// separated and the attached spelling from this one value, and the tests
	// that pin the behaviour spell the same word.
	gitDirOptionName = "--git-dir"

	// runDirGitOptionNames are the git GLOBAL options that decide WHERE a
	// worktree operation acts, and which the shape policy therefore must NOT
	// honour as a declared root.
	//
	// WHY (Rule 41, measured 2026-10-14 on the live binary): the policy accepts
	// a leading `cd <dir>` as a declared root — a deliberate beam for the
	// legitimate "cd <my tree> && git worktree remove …" cleanup — and git
	// resolves a RELATIVE repository the same way the shell does. So when the
	// repository is named ABSOLUTELY through one of these options, the cd
	// selects no directory at all as far as git is concerned, while the policy
	// still carried it as a root. Measured, both operations SUCCEEDED:
	//
	//	cd <out-of-root dir> && git --git-dir=<repo>/.git worktree add --detach <same dir>/wt <rev>
	//	cd <out-of-root dir> && git --git-dir=<repo>/.git worktree remove --force <same dir>/wt
	//
	// — a worktree created and destroyed at a path no declared root covers.
	// The beam authorises the tree the COMMAND RUNS IN; git reading its
	// repository elsewhere means this command runs in the caller's cwd, not
	// there, so the cd no longer names a tree the operation belongs to.
	//
	// The list is by NAME (content), not by spelling: the separated form, the
	// attached `=` form and git's `-C` are three spellings of one question —
	// which directory does this command act on — and it is the question that is
	// refused here, never one lucky spelling of it.
	runDirGitOptionNames = " " + worktreeChangeDirFlag + " " + gitDirOptionName + " "

	// worktreeValueFlagNames are the `git worktree add` options that consume the
	// NEXT token as their value, so the positional path is not mistaken for a
	// branch name (or vice versa). Named separately from the list above because
	// `--git-dir` is not a worktree flag: it is git's GLOBAL repository
	// selector, which before `worktree` must be consumed as an option+value pair
	// for exactly the same reason (its VALUE is a repository path, never the
	// worktree target), and it is ALSO a run-directory option that the cd carry
	// must not honour.
	worktreeValueFlagNames = " -b -B --reason "
	// worktreeToken / worktreeAddToken / worktreeMoveToken / worktreeRemoveToken
	// are git's own subcommand spellings, named once: the target extractor, the
	// creation-moment reader in task_deliverable_worktree.go and the refusal
	// message all key on them (Rule 13 — one spelling per verb).
	worktreeToken       = "worktree"
	worktreeAddToken    = "add"
	worktreeMoveToken   = "move"
	worktreeRemoveToken = "remove"

	// worktreeMoveSourceIndex / worktreeMoveDestIndex are `git worktree move`'s
	// two positional operand positions, named (Rule 1) because they answer two
	// DIFFERENT questions: the SOURCE names a directory that already exists and
	// is being relocated (judged by the declared-root property only), while the
	// DESTINATION is the path git creates (declared-root property AND the
	// fresh-path predicate). Reading them in the wrong order would refuse every
	// legitimate move, so the two positions are spelled once, here, and the
	// extractor and its test both key on them.
	worktreeMoveSourceIndex = 0
	worktreeMoveDestIndex   = 1

	// worktreeValueFlags are the `git worktree add` options that consume the
	// NEXT token as their value, so the positional path is not mistaken for a
	// branch name (or vice versa). Everything else on the subcommand is a
	// boolean flag; an unknown flag is treated as boolean, which errs toward
	// reading the path from the real positional argument.
	flagsWithValues = worktreeValueFlagNames + " " + gitDirOptionName + " "

	// worktreeChangeDirFlag is git's own GLOBAL option "run as if git was
	// started in <path>" — the one option that appears BEFORE the subcommand.
	//
	// It must be consumed as an option, never read as a path argument. The
	// value is usually the REPOSITORY the command operates on, which is
	// deliberately outside the worktree roots (the main checkout is not a
	// worktree root), so reading it as the target refused the exact shape every
	// reclaim and every cleanup uses:
	//
	//	git -C /home/nick/work/GoPi worktree remove /home/nick/workspace/wt
	//
	// measured 2026-09-15: a verifier that had created its OWN probe worktree
	// tried that shape and was refused with `target "/home/nick/work/GoPi" is
	// outside every declared root` — the guard named the repository, not the
	// worktree — so the probe worktree stayed registered and became permanent
	// residue (the very leak this fix closes). Every internally generated
	// removal (`git -C <repo> worktree remove <wt>`) was refused the same way;
	// only a `cd <repo> && git worktree remove <wt>` form slipped through.
	//
	// That `cd` form is the SHELL side of the same run-directory question, and
	// it is read by commandChangeDirRoots below — the reader that feeds the
	// cd carry. The two options are one concept ("which directory does this
	// command run in") and are kept adjacent so a change to either is read
	// against the other.
	worktreeChangeDirFlag = "-C"

	// shellChangeDirBuiltin is the shell builtin that selects a simple
	// command's run directory. Named (Rule 1b): the reader below and its tests
	// must agree on one spelling — a bare "cd" at one site and "pushd" at
	// another is exactly the drift the cd carry must not have.
	shellChangeDirBuiltin = "cd"
	// shellChangeDirPrevious is the builtin's own "go back" argument, which
	// names the shell's OLDPWD variable rather than a path — the one cd
	// argument that carries no statically known directory.
	shellChangeDirPrevious = "-"
	// shellCommandHeadTrimmers are the characters that may glue onto the head
	// of a simple command: `(` opens a POSIX subshell group, `{` a brace group,
	// and both belong to the group, not to the builtin that follows them. Named
	// because the cd reader trims them and a test pins that the grouped
	// spelling `(cd <dir> && …)` is read identically to the bare one.
	shellCommandHeadTrimmers = "({"
)

// worktreeTarget is ONE path a worktree-shaped git command names, plus the
// directory its own relative spelling resolves against (git's -C value when one
// was given, else the session workdir). Keeping the base with the target is
// what makes the judged path the SAME path git will act on: a relative target
// under `-C <repo>` is relative to <repo>, not to the caller's cwd.
type worktreeTarget struct {
	path    string
	baseDir string
	// create reports that this target is a CREATION (`worktree add <path>`, or
	// the DESTINATION operand of `worktree move <src> <DEST>`), not a removal or
	// a relocation SOURCE. Only a creation is subject to the fresh-path
	// predicate: a `worktree remove <path>` and a move's SOURCE both name a
	// directory that is supposed to be there, and applying the predicate to
	// either would refuse every legitimate removal and every legitimate move.
	create bool
}

// worktreeValueFlagSet is flagsWithValues as a set — one definition, two
// readers (the extractor below and its tests).
var worktreeValueFlagSet = func() map[string]bool {
	set := map[string]bool{}
	for _, f := range strings.Fields(flagsWithValues) {
		set[f] = true
	}
	return set
}()

// destructiveShapes are the tree-level destroy forms, each with the reason it
// is listed. They are matched case-insensitively against a quote-stripped copy
// of the command, and a match makes every path-like target of the command
// subject to the declared-root rule.
//
// The list is deliberately SHAPES, not command names: "recursive flag on a
// removal", "recursive flag on an ownership/permission change", "a device
// writer", "a mount mutation", "a filesystem creator", "a shell redirection"
// (a write) — so a new spelling of the same shape (rm -fr, chown --recursive,
// shred) is covered without a new entry.
var destructiveShapes = []struct {
	why string
	re  *regexp.Regexp
}{
	{
		why: "recursive/forced removal (rm with -r or -f): one command can destroy a tree outside the session's own work",
		re:  regexp.MustCompile(`(?i)(^|[\s;&|(])rm\s+[^\s;&|]*\s*(-{1,2}[a-z]*[rf][a-z]*)`),
	},
	{
		why: "recursive ownership change (chown -R): it re-attributes a whole tree, including trees the agent does not own",
		re:  regexp.MustCompile(`(?i)(^|[\s;&|(])chown\s+[^\s;&|]*\s*(-{1,2}[a-z]*r[a-z]*)`),
	},
	{
		why: "recursive permission change (chmod -R): it can make a system tree unwritable or a data tree world-readable",
		re:  regexp.MustCompile(`(?i)(^|[\s;&|(])chmod\s+[^\s;&|]*\s*(-{1,2}[a-z]*r[a-z]*)`),
	},
	{
		why: "filesystem creation (mkfs): it destroys whatever was on the target",
		re:  regexp.MustCompile(`(?i)(^|[\s;&|(])mkfs(\.|\s|$)`),
	},
	{
		why: "mount/umount: it changes what every other process on the host sees",
		re:  regexp.MustCompile(`(?i)(^|[\s;&|(])(mount|umount)\s`),
	},
	{
		why: "device/file overwriter (dd with a path operand, shred, truncate): a byte-level destroy that ignores filesystem structure",
		re:  regexp.MustCompile(`(?i)(^|[\s;&|(])(dd\s+[^\s;&|]*\b(of|if)=|shred\s|truncate\s)`),
	},
	{
		why: "find-driven deletion (find … -delete / -exec rm): the removal is expressed as a predicate, so the target is not visible as an rm argument",
		re:  regexp.MustCompile(`(?i)(^|[\s;&|(])find\s[^\n]*(-delete|-exec\s+rm)`),
	},
	// NO ENTRY for `git worktree add|remove`. It is a POSITIONAL shape — exactly
	// ONE token is the target and every other path-looking token is an option's
	// VALUE (the repository named by `-C`, the directory named by `cd`) — so the
	// token-set judgement below is the wrong instrument for it: it judged the
	// REPOSITORY, which is deliberately outside every worktree root (the main
	// checkout is never a worktree root), and refused the command. MEASURED
	// 2026-09-15 on the unfixed tree, both shapes refused naming the repo:
	//
	//	git -C /home/nick/work/GoPi worktree remove /home/nick/workspace/wt
	//	cd /home/nick/work/GoPi && git worktree remove /home/nick/workspace/wt
	//
	// The first is the shape EVERY internal removal uses (`git -C <repo>
	// worktree remove <wt>`) and the second is how a session re-runs it by hand;
	// both were refused with a message blaming the repository, so a verifier that
	// had created its own probe worktree could not clean it up and the probe
	// became permanent residue (the live instance this fix closes). arity is the
	// fix: the shape is judged by `gitWorktreePathTargets` — the dedicated arm
	// ABOVE, which reads the positional operand and the -C base together — and
	// this arm no longer judges it a second time by a different rule (Rule 13:
	// one judge per shape, never two that disagree).
}

// rejectOutsideDeclaredRoots applies the shape policy to one command and returns
// a NON-EMPTY refusal reason when a destructive shape or a worktree creation
// names a target outside every declared root ("" = the command is permitted).
//
// workdir is the calling session's working directory: it is itself a declared
// root (RoleSession) because a command may always clean up inside the tree it
// was started in — that beam is what keeps an ordinary "rm -rf build" legal.
func rejectOutsideDeclaredRoots(command, workdir string) string {
	scan := stripQuotes(command)

	// The directories a `cd` in this command selects join the DECLARED ROOTS
	// for the judgement only when the operator's CONFIGURATION already covers
	// them: "cd <dir> && …" runs IN <dir>, so a tree-level operation inside it
	// is inside the tree the command was started in — the same beam
	// `declaredRoots` gives the session workdir. The cd is a shell builtin
	// anyone can type at any directory, so the cd is an AUTHORIZATION only where
	// the configuration reaches; it remains a RESOLUTION BASE everywhere (the
	// relative operands of the command still resolve against it). See
	// commandChangeDirRoots for what that costs when the two questions are
	// conflated, and the measured asymmetry that named it.
	configurationRoots := declaredRoots(workdir)
	commandRoots := append(configurationRoots, commandChangeDirRoots(command, workdir, configurationRoots)...)

	// (1) worktree creation/removal: the path is positional, so it is extracted
	// from argument order rather than from the "looks like a path" heuristic.
	// Each target carries its OWN base directory (git's -C value when the
	// command gave one, else the cd carry, else the session workdir), because
	// that is the directory git itself will resolve a relative target against.
	if targets := gitWorktreePathTargets(command, workdir); len(targets) > 0 {
		// FAIL-CLOSED ON AN UNVERIFIABLE TARGET — the same rule the destructive
		// arm below applies, and it lives HERE because a worktree is a whole
		// checkout: creating one outside the declared roots is the blast-radius
		// this policy exists to bound (the /dev/shm assessment), and destroying
		// one removes another session's tree.
		//
		// Without it a shell expansion was silently resolved as a LITERAL
		// RELATIVE path against the base dir, producing an in-root path the
		// policy then authorised — a FABRICATED judgement, because the path the
		// shell builds is elsewhere entirely (measured 2026-10-12; the shapes
		// are pinned by TestPathPolicy_RefusesUnverifiableWorktreeTarget).
		// The check is on the command that CARRIES the target — the git
		// invocation itself plus the `cd` commands before it (they select the
		// directory a relative target resolves against) — never on the whole
		// command line: `… git worktree add <literal> -b feat/x && echo "rc=$?"`
		// names an expansion in a DIFFERENT simple command that cannot move the
		// target, and refusing that would be a false refusal.
		if token := unverifiableTargetToken(worktreeScannedTokens(command)); token != "" {
			return unverifiableTargetRefusal(token, worktreeShapeWhy)
		}
		for _, target := range targets {
			base := targetBaseDir(target, workdir)
			if reason := judgeTarget(target.path, base, commandRoots, worktreeShapeWhy); reason != "" {
				return reason
			}
			// EXISTENCE-BEFORE-CREATION (worktree_fresh_path.go): the guard
			// above answers "may a worktree live HERE at all"; this one answers
			// "is the path FREE right now". Both are needed and they are not
			// the same question — a path inside a declared root is authorised
			// AND may still be occupied, and `git worktree add` silently reuses
			// an occupied-but-empty one. Only CREATION is checked: a `worktree
			// remove` names a path that is supposed to exist.
			if target.create {
				abs, ok := absolutize(target.path, base, nil)
				if !ok {
					return fmt.Sprintf("target %q cannot be resolved to an absolute path — refusing, because an unverifiable worktree path is never created (%s)", target.path, worktreeShapeWhy)
				}
				if ferr := worktreeFreshPath(abs); ferr != nil {
					return ferr.Error()
				}
			}
		}
	}

	// (2) destructive shapes: every path-like token of the command is judged.
	for _, shape := range destructiveShapes {
		if !shape.re.MatchString(scan) {
			continue
		}
		// Fail-closed on shell expansion, exactly as the worktree arm above
		// does: a target that cannot be statically resolved (${DIR}/inner,
		// `hostname`) must not be destroyed, so it is refused BY NAME rather
		// than skipped. One predicate, both arms (Rules 13/14).
		if token := unverifiableTargetToken(strings.Fields(scan)); token != "" {
			return unverifiableTargetRefusal(token, shape.why)
		}
		// EVERY target of a destructive shape is judged against the directory
		// IT resolves against — the shell's run directory at that point in the
		// command (see commandPathTargets), never the session workdir once the
		// command has selected its own. Measured 2026-10-14 on b03796d: the arm
		// read the session workdir for a target the command had already moved
		// away from, so `cd <dir> && rm -rf <relative>` was judged against a
		// path the shell never visits.
		for _, target := range commandPathTargets(command, workdir) {
			if reason := judgeTarget(target.path, targetBaseDir(target, workdir), commandRoots, shape.why); reason != "" {
				return reason
			}
		}
	}
	return ""
}

// worktreeShapeWhy is the reason a worktree-target refusal carries; it is a
// constant because the same sentence is asserted by the policy tests and
// rendered to the operator (Rule 1b).
const worktreeShapeWhy = "a linked worktree is created/destroyed in a DECLARED root"

// worktreeScannedTokens returns the tokens that must be statically readable
// before a worktree target may be judged: the tokens of every simple command
// that IS a `git … worktree add|remove` invocation, plus the tokens of the `cd`
// commands that precede it (a cd selects the directory a relative target
// resolves against, so an expansion there makes the REAL target unknowable even
// when the operand itself is spelled out).
//
// Scoping the fail-closed check to this set — rather than to the whole command
// line — is what keeps it from refusing a legitimate command for an unrelated
// token: `… git worktree add <literal> -b feat/x && echo "rc=$?"` names an
// expansion in a DIFFERENT simple command, which cannot move the target.
func worktreeScannedTokens(command string) []string {
	var out, preceding []string
	for _, simple := range splitSimpleCommands(stripQuotes(command)) {
		tokens := strings.Fields(simple)
		if _, _, ok := gitInvocationStart(tokens); !ok {
			// Not a git invocation: it may still SELECT the directory the
			// commands that follow it run in. Its own tokens are scanned (an
			// unverifiable cd root makes every relative target below it
			// unverifiable), and it is remembered as carry.
			if _, ok := commandChangeDir(simple); ok {
				preceding = append(preceding, tokens...)
				out = append(out, tokens...)
			}
			continue
		}
		if !hasWorktreeSubcommand(tokens) {
			continue
		}
		out = append(out, preceding...)
		out = append(out, tokens...)
	}
	return out
}

// hasWorktreeSubcommand reports whether a git invocation carries a
// `worktree add|remove` subcommand — the shape whose positional target this
// policy judges (and the shape the fail-closed scan above is scoped to).
func hasWorktreeSubcommand(tokens []string) bool {
	_, start, ok := gitInvocationStart(tokens)
	if !ok {
		return false
	}
	for i := start; i+1 < len(tokens); i++ {
		if tokens[i] != worktreeToken {
			continue
		}
		sub := tokens[i+1]
		return sub == worktreeAddToken || sub == worktreeRemoveToken
	}
	return false
}

// unverifiableTargetToken returns the first token of a command whose value is
// decided by the shell at run time (a `$` variable, a `$(` / backtick command
// substitution), or "" when every token is statically readable.
//
// It is the ONE predicate both arms of the shape policy consult — the
// destructive-token arm and the worktree-target arm — because "an unverifiable
// target is never created or destroyed" is a property of the TARGET, not of the
// command that happens to carry it (Rules 13/14; the arm that lacked it is the
// measured 2026-10-12 asymmetry).
func unverifiableTargetToken(tokens []string) string {
	for _, tok := range tokens {
		if strings.ContainsAny(tok, shellExpansionChars) {
			return tok
		}
	}
	return ""
}

// unverifiableTargetRefusal renders the fail-closed refusal for such a token:
// it names the token, says WHY it cannot be judged, and names the shape that
// was being judged (Rule 15: a refusal names its cause).
func unverifiableTargetRefusal(token, why string) string {
	return fmt.Sprintf("token %q contains shell variable/command substitution — its target cannot be verified, and an unverifiable target is never created or destroyed (%s)", token, why)
}

// judgeTarget decides ONE target path against the declared roots and returns a
// refusal naming the target, the shape and the roots that were consulted. An
// unresolvable target (a shell expansion) is refused too: an unverifiable
// target must not be destroyed or created (the fail-closed rule the existing
// cwd whitelist already applies).
func judgeTarget(target, workdir string, roots []worktreeroots.Root, why string) string {
	path, ok := absolutize(target, workdir, roots)
	if !ok {
		return fmt.Sprintf("target %q cannot be resolved to an absolute path — refusing (%s): an unverifiable target is never created or destroyed", target, why)
	}
	if _, permitted := worktreeroots.PermitsAny(roots, path); permitted {
		return ""
	}
	return fmt.Sprintf("target %q is outside every declared root (%s) — refusing (%s). Declared roots: %s. Declare the repo's root in settings.json (\"repos\": {\"<repo>\": {\"%s\": \"<path>\"}}) or set %s for a sandbox.",
		path, rootsSummary(roots), why, rootsSummary(roots), worktreeroots.WorktreeRootSettingsKey, worktreeroots.WorktreeRootEnvName)
}

// absolutize turns a command target into an absolute path: tilde expansion, then
// resolution against the session workdir for a relative spelling.
//
// A relative target is resolved against the WORKDIR (not the process cwd),
// because the command runs with workdir as its cwd — and the roots list carries
// that workdir as its RoleSession beam, so "rm -rf build" stays legal while
// "../../elsewhere/build" does not.
func absolutize(target, workdir string, roots []worktreeroots.Root) (string, bool) {
	t := strings.TrimSpace(strings.Trim(target, `"'`))
	if t == "" {
		return "", false
	}
	expanded, err := worktreeroots.ExpandTilde(t)
	if err != nil {
		return "", false
	}
	if filepath.IsAbs(expanded) {
		return filepath.Clean(expanded), true
	}
	base := workdir
	if base == "" {
		if cwd, err := os.Getwd(); err == nil {
			base = cwd
		} else {
			return "", false
		}
	}
	abs, err := filepath.Abs(filepath.Join(base, expanded))
	if err != nil {
		return "", false
	}
	_ = roots
	return filepath.Clean(abs), true
}

// declaredRoots lists every root the policy consults, in the order the refusal
// prints them. It is rebuilt per call because the ambient layer (the sandbox
// env override, the session cwd) is per-call state.
//
// The settings read is memoized per repo path (settingsMemo) so a hot bash loop
// does not re-parse settings.json on every command; a settings file that cannot
// be read is REPORTED once and falls back to the default root, never to an empty
// root set (an empty set would refuse every legitimate operation — the failure
// mode of a guard that cannot read its own policy).
func declaredRoots(workdir string) []worktreeroots.Root {
	var roots []worktreeroots.Root
	if workdir != "" {
		roots = append(roots, worktreeroots.Root{Path: filepath.Clean(workdir), Role: worktreeroots.RoleSession, Source: worktreeroots.SourceSessionCwd})
	}
	roots = append(roots, repoWorktreeRoot(workdir))
	roots = append(roots, worktreeroots.Root{
		Path:   scratch.CanonicalPath(scratch.HomeDir()),
		Role:   worktreeroots.RoleScratch,
		Source: worktreeroots.SourceScratchHome,
	})
	if temp := scratch.CanonicalPath(os.TempDir()); temp != "" {
		roots = append(roots, worktreeroots.Root{Path: filepath.Clean(temp), Role: worktreeroots.RoleFixture, Source: worktreeroots.SourceTempRoot})
	}
	if scratch.CanonicalPath(worktreeroots.AmbientTempRoot) != scratch.CanonicalPath(os.TempDir()) {
		roots = append(roots, worktreeroots.Root{Path: worktreeroots.AmbientTempRoot, Role: worktreeroots.RoleFixture, Source: worktreeroots.SourceTempRoot})
	}
	return roots
}

// settingsMemo caches one repo's worktree-root declaration per process run.
// The value is a settings.json read; the key is the repo path, and the answer
// for a given repo does not change within a process run (a settings edit is an
// operator action, and every resident component re-reads at its next start).
var settingsMemo sync.Map // repo path -> worktreeroots.Root

// settingsWarnOnce guards the stderr report for a settings file that could not
// be read — reported once per process, never per command (a report the reader
// cannot get through is a report nobody acts on).
var settingsWarnOnce sync.Once

// repoWorktreeRoot resolves the worktree root for the repo a command runs in
// (the session workdir, which is where the row's repo is checked out). A
// settings failure falls back to the DEFAULT root with one stderr report
// (Rule 15b: report, never swallow) rather than to an empty root set.
func repoWorktreeRoot(workdir string) worktreeroots.Root {
	if workdir != "" {
		if cached, ok := settingsMemo.Load(workdir); ok {
			return cached.(worktreeroots.Root)
		}
	}
	root, err := config.ResolveRepoWorktreeRoot(workdir)
	if err != nil {
		settingsWarnOnce.Do(func() {
			fmt.Fprintf(os.Stderr, "path policy: %v — falling back to the default worktree root\n", err)
		})
		fallback, ferr := worktreeroots.Resolve("")
		if ferr != nil {
			fallback = worktreeroots.Root{Path: "", Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceDefault}
		}
		return fallback
	}
	if workdir != "" {
		settingsMemo.Store(workdir, root)
	}
	return root
}

// rootsSummary renders the consulted roots for a refusal, one "role=path" per
// root, so the reader can see exactly what the policy allowed and what the
// target missed (Rule 15: a refusal names its cause).
func rootsSummary(roots []worktreeroots.Root) string {
	parts := make([]string, 0, len(roots))
	for _, r := range roots {
		if r.Path == "" {
			continue
		}
		parts = append(parts, string(r.Role)+"="+r.Path+" ["+r.Source+"]")
	}
	return strings.Join(parts, ", ")
}

// stripQuotes removes quoting so a shape cannot hide behind a quote
// ("rm -rf \"/dev/shm/x\"" is the same filesystem operation as the unquoted
// form). It mirrors the existing guard's fail-closed rule.
func stripQuotes(command string) string {
	out := strings.ReplaceAll(command, `"`, "")
	return strings.ReplaceAll(out, "'", "")
}

// commandPathTargets extracts every path-like token of a command — absolute,
// ./, ../, ~/ or . forms, plus dd's if=/of= operands through pathTarget — and
// the destination of a shell write redirection (> file / >> file).
//
// Each target carries the base directory IT resolves against: the directory the
// shell has selected by the time its own simple command runs — replayed from the
// preceding `cd`s and this segment's own leading `cd`, and falling back to the
// session workdir when the command selects none. Judging a relative target
// against the session workdir while the shell runs it somewhere else is a
// FABRICATED judgement in both directions (see rejectOutsideDeclaredRoots).
//
// Tokens carrying a shell expansion are NOT returned here: the caller decides
// their fate (the existing cwd whitelist already refuses them fail-closed, and
// this policy refuses them by name in judgeTarget when they surface as a
// worktree target).
//
// One reader for "which directory does THIS target resolve against" — the same
// shape the worktree arm builds in gitWorktreePathTargets, through the same
// carriedChangeDir half (Rules 13/14: one definition, both arms).
func commandPathTargets(command, workdir string) []worktreeTarget {
	var out []worktreeTarget
	var preceding []string
	for _, simple := range splitSimpleCommands(stripQuotes(command)) {
		// The run directory IN FORCE for this simple command: every `cd` the
		// command applied before it, plus its own leading `cd` (which selects
		// the directory its own operands resolve against).
		dirs := preceding
		if dir, ok := commandChangeDir(simple); ok {
			dirs = append(append([]string{}, preceding...), dir)
		}
		baseDir := carriedChangeDir(dirs, workdir)
		for _, tok := range strings.Fields(simple) {
			if target, ok := pathTarget(tok); ok {
				out = append(out, worktreeTarget{path: target, baseDir: baseDir})
				continue
			}
			if target, ok := redirectTarget(tok); ok {
				out = append(out, worktreeTarget{path: target, baseDir: baseDir})
			}
		}
		if dir, ok := commandChangeDir(simple); ok {
			preceding = append(preceding, dir)
		}
	}
	return out
}

// redirectTarget extracts the destination of a shell write: "> path",
// ">> path", "2> path". A file-descriptor dup (">&1", ">&2") and the kernel
// pseudo-devices are not filesystem targets.
func redirectTarget(tok string) (string, bool) {
	trimmed := strings.TrimLeft(tok, "0123456789")
	if !strings.HasPrefix(trimmed, ">") {
		return "", false
	}
	dest := strings.TrimLeft(trimmed, ">")
	dest = strings.TrimSpace(dest)
	if dest == "" || strings.HasPrefix(dest, "&") {
		return "", false
	}
	return pathTarget(dest)
}

// targetBaseDir is the directory a worktree target's own relative spelling
// resolves against: git's -C value when the command gave one (the invocation
// carries its own run directory), else the target's cd carry, else — when the
// command selected no directory at all — the session workdir, which is the
// directory the shell itself will start in.
//
// It is the ONE place the two halves of the run directory meet, so a future
// caller cannot pair a target with the wrong base (Rule 14).
func targetBaseDir(target worktreeTarget, workdir string) string {
	if target.baseDir != "" {
		return target.baseDir
	}
	return workdir
}

// carriedChangeDir returns the directory a command runs in, as selected by the
// `cd` builtins the invocation's own shell command line applied before it.
//
// It is ONE reader for the SHELL half of the run directory (the git half is the
// `-C` global, read by gitInvocationStart), and both consumers of the question
// "which directory does this command run in" — the worktree-target extraction
// that judges a relative operand, and the root beam the judgement uses — call
// it rather than re-deriving it (Rules 13/14: one definition, both consumers).
//
// The cd's are replayed in ORDER, each resolving against the directory the ones
// before it selected, exactly as a shell would: `cd /a && cd b && git …` runs in
// /a/b. The LAST one wins, because it is the directory the shell is standing in
// when the invocation starts. No cd at all leaves "" — the caller's own workdir
// stays in charge, which is the behaviour that existed before this reader.
func carriedChangeDir(dirs []string, workdir string) string {
	base := ""
	for _, dir := range dirs {
		resolved, ok := absolutize(dir, firstNonEmpty(base, workdir), nil)
		if !ok {
			// An unresolvable cd (a shell expansion) selects a directory the
			// policy cannot name. It is NOT guessed at: the carry keeps the
			// last directory it could resolve, so a target is judged against a
			// path that is known rather than against a fabricated one.
			continue
		}
		base = resolved
	}
	return base
}

// firstNonEmpty returns the first non-empty argument ("" when all are empty).
// It renders the "resolve against the directory selected so far, else against
// the caller's workdir" fallback as one readable expression at both call sites.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// runDirGitOptionSet is runDirGitOptionNames as a lookup set — one definition,
// two readers (the scanner above and the command builder below).
var runDirGitOptionSet = func() map[string]bool {
	set := map[string]bool{}
	for _, f := range strings.Fields(runDirGitOptionNames) {
		set[f] = true
	}
	return set
}()

// runDirOptionsSelected reports whether an option TOKEN is one of git's
// run-directory options — in either the separated form (`--git-dir /repo/.git`)
// or the attached form (`--git-dir=/repo/.git`, and their `-C` spellings).
//
// It is the ONE reader of that question (Rule 13/14): the cd-carry rule and
// anything else that must tell "a directory this command RUNS in" apart from "a
// directory git was merely told about" asks it, so a new attached spelling
// cannot be honoured by one arm and refused by another.
func runDirOptionsSelected(tok string) bool {
	if runDirGitOptionSet[tok] {
		return true
	}
	name, _, found := strings.Cut(tok, "=")
	if !found {
		return false
	}
	for _, option := range strings.Fields(runDirGitOptionNames) {
		if option != worktreeChangeDirFlag && name == strings.TrimSpace(option) {
			return true
		}
	}
	return false
}

// commandNamesRepoElsewhere reports whether a simple command hands git the
// repository through a run-directory option — `-C <path>`, `--git-dir <path>`
// or the attached `--git-dir=<path>` — rather than letting it resolve the
// repository from the directory the command runs in.
//
// It exists for ONE reader (the cd carry below) and states one fact: the cd of
// this command did not pick where the work happens. Kept as a named predicate
// rather than inlined so the cd-carry rule reads as the two questions it
// actually answers — does the command name a repository itself, and where does
// it run — instead of as a token hunt.
func commandNamesRepoElsewhere(tokens []string) bool {
	_, start, ok := gitInvocationStart(tokens)
	if !ok {
		return false
	}
	for _, tok := range tokens[1:start] {
		if runDirOptionsSelected(tok) {
			return true
		}
	}
	return false
}

// commandChangeDirRoots returns the directory the SHELL command line selects as
// its working directory (the cd carry, resolved), as a declared-root beam for
// the policy's judgement — and the SAME reader feeds the worktree-target base,
// so the path a target is judged against and the root that authorises it can
// never disagree.
//
// A cd selects TWO things and they are NOT interchangeable (Rule 41, measured
// 2026-10-16 on the live binary; the family's SECOND shape, its sibling being
// the repository-naming options below):
//
//   - a RESOLUTION BASE — the directory a relative operand resolves against.
//     This is what git itself does (git resolves against the cwd it was
//     launched in), and it is unchanged by this rule.
//
//   - an AUTHORIZATION — whether the tree a whole-tree operation touches is one
//     the operation may touch at all. This is what a root beam grants, and a
//     `cd` grants it ONLY when the directory it selected is itself inside a root
//     the operator's CONFIGURATION declares (the worktree root, the scratch
//     home, the temp root, the session workdir). `cd` is a shell builtin anyone
//     can type at any directory, so treating the cd itself as a declaration
//     made EVERY directory on the host a declared root for create/destroy:
//
//     cd <any out-of-root dir> && git -C <repo> worktree add --detach <that dir>/wt
//
//     was ALLOWED (exit 0, the registration appeared, the directory was
//     created) and so was the matching `worktree remove --force` (exit 0, the
//     registration gone) — while the SAME targets WITHOUT the leading cd were
//     refused, which is the asymmetry that names the cd as the authorising
//     step. Measured on the session directory (a path on the `/` partition,
//     Rule 22) and on the repository's own checkout.
//
// What survives, and why the beam was written at all: "cd <my tree> && git
// worktree remove <wt>" — the cleanup an agent writes after itself — where the
// cd directory is the run's own tree inside a declared root, and the relative
// spelling of the operand still resolves against the cd (a relative repository
// is resolved against it by git too). Naming a repository through `-C` /
// `--git-dir` removes the authority as well, on the same reasoning: the cd then
// selects no tree the operation belongs to (see runDirGitOptionNames).
//
// configurationRoots is the root set the operator's configuration declares —
// the caller passes the set it already built, so this predicate and the
// judgement it feeds cannot disagree about what "declared" means (Rule 14).
func commandChangeDirRoots(command, workdir string, configurationRoots []worktreeroots.Root) []worktreeroots.Root {
	var dirs []string
	for _, simple := range splitSimpleCommands(stripQuotes(command)) {
		tokens := strings.Fields(simple)
		if dir, ok := commandChangeDir(simple); ok {
			if commandNamesRepoElsewhere(tokens) {
				continue
			}
			dirs = append(dirs, dir)
		}
		// THE SAME QUESTION, ASKED OF THE GIT INVOCATION — and the reason the
		// cd carry exists at all now holds the other way round. git resolves a
		// RELATIVE repository against the directory the shell selected, so such
		// a cd names a tree the operation belongs to and authorises it. When
		// the invocation instead NAMES ITS OWN REPOSITORY (`-C <path>`, the
		// attached `-C<path>`, `--git-dir[=]`), git acts on the repository it
		// was HANDED, from wherever the caller stood: the cd then selects no
		// tree this operation belongs to, and authorising it is the hole
		// measured 2026-10-14 (a worktree created at a path no declared root
		// covers, while the same target without the cd was refused).
		if _, _, ok := gitInvocationStart(tokens); ok && commandNamesRepoElsewhere(tokens) {
			return nil
		}
	}
	selected := carriedChangeDir(dirs, workdir)
	if selected == "" {
		return nil
	}
	// AUTHORIZATION, not resolution: the cd is a root beam only when the
	// directory it selected already lies inside a root the operator's
	// configuration declared. A cd outside every declared root still resolves
	// relative operands (carriedChangeDir feeds targetBaseDir, untouched) — it
	// merely authorises nothing, so the target is judged on its own against the
	// declared roots, exactly as it would be without the cd.
	if _, permitted := worktreeroots.PermitsAny(configurationRoots, selected); !permitted {
		return nil
	}
	return []worktreeroots.Root{{
		Path:   selected,
		Role:   worktreeroots.RoleSession,
		Source: worktreeroots.SourceCommandChangeDir,
	}}
}

// commandChangeDir reads ONE simple command's leading `cd <path>` and returns
// the path it selects.
//
// The scan is deliberately positional from the START of the simple command:
// only a `cd` that runs BEFORE anything else in that simple command can be the
// run directory of the commands that follow it. A `cd` further along —
// `git -C <repo> status; cd /elsewhere` — is a SIBLING command whose effect
// belongs to a different simple command, and reading it here would attribute
// one command's directory to another's target (the token-scan-composing-two-
// commands bug that splitSimpleCommands exists to prevent). An option before
// the directory (`cd -P <path>`, `cd -- <path>`) is skipped, not read as one.
func commandChangeDir(simple string) (string, bool) {
	tokens := strings.Fields(strings.TrimLeft(simple, shellCommandHeadTrimmers))
	if len(tokens) < 2 || tokens[0] != shellChangeDirBuiltin {
		return "", false
	}
	for _, tok := range tokens[1:] {
		if strings.HasPrefix(tok, "-") {
			// `cd -` selects the PREVIOUS directory (a shell variable), never a
			// statically known path: it carries nothing, so the command selects
			// no directory the policy can name.
			if tok == shellChangeDirPrevious {
				return "", false
			}
			continue
		}
		return tok, true
	}
	return "", false
}

// gitWorktreePathTargets returns the positional path operand of every
// `git worktree add|remove <path>` in a command.
//
// Argument-order handling is the whole point: git accepts the path anywhere
// among the flags, and `-b <branch>` takes a VALUE, so a naive "first bare
// token" read picks the branch name and would refuse legitimate worktrees —
// a false refusal that would make the guard unusable rather than safe.
func gitWorktreePathTargets(command, workdir string) []worktreeTarget {
	var out []worktreeTarget
	var preceding []string
	for _, simple := range splitSimpleCommands(stripQuotes(command)) {
		tokens := strings.Fields(simple)
		baseDir, start, ok := gitInvocationStart(tokens)
		if !ok {
			// Not a git invocation: it may still SELECT a directory for the
			// commands that follow it. Remember it, and read nothing into it.
			if dir, ok := commandChangeDir(simple); ok {
				preceding = append(preceding, dir)
			}
			continue
		}
		// git's own `-C` wins when the invocation gave one; otherwise the
		// directory the SHELL selected before it — the `cd <path>` this reader
		// was added for — is the base the relative target resolves against.
		//
		// The cd lives in an EARLIER simple command: `&&` is a sequencing
		// operator, so `cd <repo> && git …` cuts into two segments and the
		// shell itself applies each segment's directory to the one that follows
		// it. Reading the carry from the PRECEDING segments is therefore the
		// same rule git applies (git resolves against the cwd it was launched
		// in), not a widening of it: a `cd` that follows the invocation —
		// `git … && cd /elsewhere` — is never consulted, because it cannot
		// affect a command that already ran.
		if baseDir == "" {
			baseDir = carriedChangeDir(preceding, workdir)
		}
		for i := start; i+2 < len(tokens); i++ {
			if tokens[i] != worktreeToken {
				continue
			}
			sub := tokens[i+1]
			if sub == worktreeMoveToken {
				out = append(out, worktreeMoveTargets(tokens[i+2:], baseDir)...)
				continue
			}
			if sub != worktreeAddToken && sub != worktreeRemoveToken {
				continue
			}
			if target, ok := firstPositional(tokens[i+2:]); ok {
				out = append(out, worktreeTarget{path: target, baseDir: baseDir, create: sub == worktreeAddToken})
			}
		}
	}
	return out
}

// worktreeMoveTargets reads `git worktree move <src> <DEST>` — the arm that was
// MISSING, and the hole this function closes.
//
// WHY IT WAS MISSING, AND WHY THAT WAS NOT AN INERT GAP. Until this arm existed,
// `gitWorktreePathTargets` recognised `add|remove` only, so a move never entered
// the guard at all. A shape the policy does not read is a shape it does not
// guard, and the failure was not a refusal that went the wrong way — the target
// was never even LOOKED AT. Measured (2026-10-12) against the unfixed file:
//
//	git worktree move <in-root src> /dev/shm/gopi-elsewhere/moved   # MOVE_EXIT=0, DEST=EXISTS
//	git worktree add                 /dev/shm/gopi-elsewhere/moved  # REFUSED
//
// — the same out-of-root location, refused when spelled through `add` and
// silently created when spelled through `move`. `add`'s arm is exactly where the
// blast-radius argument lives (a linked worktree is a whole checkout, ~1 015 MB
// in the 2026-09-13 /dev/shm assessment), and a move WRITES THE SAME TREES the
// add would.
//
// TWO OPERANDS, TWO DIFFERENT QUESTIONS — this is why the arm cannot reuse the
// single-target reader:
//
//   - the SOURCE (worktreeMoveSourceIndex) names a directory that EXISTS and is
//     being relocated. It is judged by the declared-root property alone;
//     applying the fresh-path predicate to it would refuse every move, because a
//     source that does not exist is not a move, it is a typo.
//   - the DESTINATION (worktreeMoveDestIndex) is the path git CHOOSES TO CREATE
//     at, so it is judged by both the declared-root property AND the fresh-path
//     predicate, exactly as `worktree add`'s operand is.
//
// BOTH carry the same base directory (git's `-C` value, else the cd carry, else
// the session workdir): git resolves both relative spellings against the single
// directory it was started in, so pairing them with different bases would judge a
// path nobody acts on.
//
// A malformed move (no DESTINATION) yields nothing: with no destination there is
// no creation to guard, and git refuses the command itself.
func worktreeMoveTargets(args []string, baseDir string) []worktreeTarget {
	operands := positionalOperands(args)
	if len(operands) <= worktreeMoveDestIndex {
		return nil
	}
	return []worktreeTarget{
		{path: operands[worktreeMoveSourceIndex], baseDir: baseDir},
		{path: operands[worktreeMoveDestIndex], baseDir: baseDir, create: true},
	}
}

// gitInvocationStart locates the git invocation inside one simple command: the
// index of the SUBCOMMAND token (the first non-option token after the binary)
// and the directory that invocation runs against — the value of git's global
// -C option when one was given, else "" (meaning the caller's own workdir).
//
// The leading options are consumed HERE so no caller can mistake one for a
// path argument. That is the whole point of the seam: `-C <repo>` names the
// REPOSITORY, which is deliberately outside the worktree roots, and reading it
// as the removal target refused every `git -C <repo> worktree remove <wt>` and
// every `git -C <repo> worktree add <wt>` (measured 2026-09-15 — see
// worktreeChangeDirFlag). Unblocking creation matters as much as removal: the
// same misread refused the ADD that would have re-created a released worktree.
func gitInvocationStart(tokens []string) (baseDir string, start int, ok bool) {
	if !mentionsGit(tokens) {
		return "", 0, false
	}
	for i := 1; i < len(tokens); i++ {
		tok := tokens[i]
		if tok == worktreeChangeDirFlag {
			if i+1 < len(tokens) {
				baseDir = tokens[i+1]
				i++
			}
			continue
		}
		// The attached spelling git also accepts: `-C<path>`.
		if strings.HasPrefix(tok, worktreeChangeDirFlag) && len(tok) > len(worktreeChangeDirFlag) {
			baseDir = strings.TrimPrefix(tok, worktreeChangeDirFlag)
			continue
		}
		if strings.HasPrefix(tok, "-") {
			// Another global option. The ones that take a separate value
			// consume the NEXT token (so their value is never read as the
			// subcommand or as a path); everything else is boolean.
			if gitGlobalValueFlagSet[tok] && i+1 < len(tokens) {
				i++
			}
			continue
		}
		return baseDir, i, true
	}
	return "", 0, false
}

// gitGlobalValueFlags are git's GLOBAL options whose value is a SEPARATE token,
// so the token scan above must consume it. Named once (Rule 1b: the set and its
// set-form reader are one definition) — the common ones an agent command
// carries, not an exhaustive parse of git's option surface.
const gitGlobalValueFlags = " -c --git-dir --work-tree --namespace --exec-path "

// gitGlobalValueFlagSet is gitGlobalValueFlags as a lookup set — one
// definition, two readers (the scanner above and its tests).
var gitGlobalValueFlagSet = func() map[string]bool {
	set := map[string]bool{}
	for _, f := range strings.Fields(gitGlobalValueFlags) {
		set[f] = true
	}
	return set
}()

// mentionsGit reports whether the GIT BINARY leads a simple command — the only
// shape that is a git invocation. Requiring the leading position (rather than
// "somewhere earlier in the tokens") is what keeps a quoted message out of the
// policy: `echo "git worktree add /dev/shm/x"` prints text, it does not create
// anything, and refusing it would be a false positive that teaches an operator
// to distrust the guard.
func mentionsGit(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	head := tokens[0]
	return head == gitBinaryName || strings.HasSuffix(head, string(filepath.Separator)+gitBinaryName)
}

// firstPositional returns the first token of an argument list that is neither a
// flag nor a flag's value — the positional path of `git worktree add|remove`.
func firstPositional(args []string) (string, bool) {
	operands := positionalOperands(args)
	if len(operands) == 0 {
		return "", false
	}
	return operands[0], true
}

// positionalOperands returns every token of an argument list that is neither a
// flag nor a flag's VALUE, in argument order.
//
// It is the shared half of the two readers above: `worktree add|remove` names one
// positional path (firstPositional takes the first operand) while `worktree move`
// names TWO, in order (worktreeMoveTargets takes operands 0 and 1). One scanner,
// so the flag-value rule — `-b feat/x` makes `feat/x` a VALUE, never a path —
// cannot drift between the two shapes (Rule 13: one judge per shape, and one
// definition of "which token is an operand").
func positionalOperands(args []string) []string {
	var out []string
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if worktreeValueFlagSet[arg] {
				skipNext = true
			}
			continue
		}
		out = append(out, arg)
	}
	return out
}

// splitSimpleCommands breaks a command at the shell's sequencing operators so a
// token scan cannot read across two commands (a "git log" in one and a
// "worktree add" message in the next must not compose into an invocation).
//
// `&` and `&&` are ONE cut, not two (strings.FieldsFunc drops every run of
// separators, so the pair never yields the empty field a two-cut reading would);
// the POSIX group delimiters `(` and `)` are cut too, so a grouped command's
// tokens never glue onto its neighbours (`echo x && (cd y && git …)`).
func splitSimpleCommands(command string) []string {
	fields := strings.FieldsFunc(command, func(r rune) bool {
		switch r {
		case ';', '|', '&', '\n', '(', ')':
			return true
		}
		return false
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if trimmed := strings.TrimSpace(f); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
