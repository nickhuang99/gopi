package tools

// shared_stash_guard.go — refuse a stash MUTATION in a repo that has more than
// one linked worktree.
//
// WHY this exists (measured, 2026-09-27; the incident family goes back to
// devdoc/incident-git-stash-worktree-collision-20260910.md and
// devdoc/task-session-cwd.md):
//
// `refs/stash` lives in the repository's COMMON dir, not in a worktree's own
// git dir. `git rev-parse --git-common-dir` in ANY linked worktree answers the
// SAME path, so the stash stack is a REPO-WIDE resource that every worktree
// shares. A `git stash apply`/`pop` MUTATES the index and the worktree of
// whatever checkout it runs in — and a session's checkout is a LIVE run's
// workspace (PROJECT.md Rule 29: the main repo hosts work in progress and is never
// a scratch target).
//
// The destructive interleaving is then ordinary, not exotic: session B's
// uncommitted tree is stashed (into the shared stack), a session A working in
// another worktree runs `git stash pop` for its own WIP and gets B's tree —
// CONFLICTED — written through its checkout, leaving `UU` entries over trunk
// files that A's own branch also owns. A conflicted pop KEEPS the entry
// (git's own rule), so the same foreign tree is re-applied on every honest
// retry: three measured incidents applied the same entry (7daab90b, "WIP on
// (no branch)"), and the residue blocks the resident run's close-out because
// Rule 30's clean-worktree gate refuses exactly that state.
//
// The guard's rule is deliberately narrow, because the blast radius is what
// makes a refusal legitimate:
//
//   - READ-ONLY stash commands (`stash list`, `stash show`) always pass: they
//     are how a session inspects the shared stack, and refusing them would
//     leave the operator with no diagnostic.
//   - A MUTATION is refused ONLY when the repo actually has more than one
//     linked worktree — the single-worktree case is the ordinary local stash
//     flow, where the stack and the checkout belong to the same person, and
//     refusing it would be a false positive on the most common use of the
//     command.
//
// The refusal names the repo, its worktree count, and the NON-DESTRUCTIVE
// transfer to use instead (`git stash show -p` / `git show` / `git diff |
// git apply --3way` against the target worktree, or `git stash branch`), so a
// blocked session is redirected rather than stopped (Rule 15: no silent
// failure — the refusal is loud and actionable).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/gitargv"
)

// Shared-stash guard constants (Rule 1: no bare literals).
const (
	// stashCommandWord is the git subcommand this guard governs. It is a
	// textual pre-filter only: the authoritative judgement is the subcommand
	// word extracted by stashSubcommandWord.
	stashCommandWord = "stash"

	// stashVerbList and stashVerbShow are the READ-ONLY stash subcommands —
	// the diagnosis path, never refused.
	stashVerbList = "list"
	stashVerbShow = "show"

	// stashMutationVerbs are every stash subcommand that writes the stack or
	// the running checkout's index/worktree. Keep this list in step with
	// `git stash -h`; a verb missing here is a refusal that does not fire.
	stashMutationApply  = "apply"
	stashMutationPop    = "pop"
	stashMutationPush   = "push"
	stashMutationSave   = "save"
	stashMutationDrop   = "drop"
	stashMutationClear  = "clear"
	stashMutationBranch = "branch"
	stashMutationCreate = "create"
	stashMutationStore  = "store"
	stashMutationImport = "import"
)

// sharedStashMutationVerbs is the lookup set built from the constants above.
var sharedStashMutationVerbs = map[string]bool{
	stashMutationApply:  true,
	stashMutationPop:    true,
	stashMutationPush:   true,
	stashMutationSave:   true,
	stashMutationDrop:   true,
	stashMutationClear:  true,
	stashMutationBranch: true,
	stashMutationCreate: true,
	stashMutationStore:  true,
	stashMutationImport: true,
}

// sharedStashReadOnlyVerbs is the lookup set of stash subcommands that only
// READ the shared stack. It is named rather than inferred from "absent from
// sharedStashMutationVerbs", so the permissive branch in the predicate is an
// explicit statement about `list`/`show` (Rule 1b) — and so a future mutation
// verb left out of the mutation set cannot silently inherit the read-only
// verdict by omission.
var sharedStashReadOnlyVerbs = map[string]bool{
	stashVerbList: true,
	stashVerbShow: true,
}

// stashClauseSeparators end a simple command; the stash word's own clause is
// everything after it up to the first of these (Rule: the guard must not read a
// stash word inside a LATER, unrelated clause of the same command line —
// MEASURED 2026-09-27 on the fixed-but-incomplete guard: `git stash pop;
// echo "…"` was permitted, because the trailing prose was read as the stash
// subcommand word — git then ran `pop` raw).
const stashClauseSeparators = "\n;&|"

// envAssignPattern matches a leading VAR=VAL word (an environment prefix, e.g.
// `GIT_DIR=/x git stash pop`) — it is not a git option and not a subcommand.
var envAssignPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// gitCommandPreambleWords are the wrapper commands that may legitimately
// precede the `git` command word (`sudo git stash pop`, `xargs git stash
// drop`). Any OTHER preceding word means the git token is an argument to some
// other command — `echo git stash`, `grep -rn stash file` — and the line holds
// no git invocation at all.
// The list itself lives in internal/gitargv (Rule 14): it is a fact about git's
// command line, not about this guard, and internal/chatpolicy needs the same one
// to recognise a ref move behind a global option. Referenced, never restated —
// a second copy would keep compiling while the two drifted.
var gitCommandPreambleWords = gitargv.OperatorWords

// gitWordPattern matches the `git` command word itself as a WHOLE SHELL WORD —
// the fail-closed gate that keeps a `grep stash <file>` (no git at all) from
// reaching a repo probe.
//
// It spells the separators instead of `\b` because a word boundary is not a
// shell boundary: `\b` treats `-` as a boundary, so `\bgit\b` also matches the
// `git` inside a PATH-shaped token (`my-git-repo`, and the `…-stash-…` workdir
// names this guard's own reports carry), which would let a line with no git
// COMMAND reach the probe. Requiring a real shell separator on both sides
// (start/whitespace/quote/`;&|()`/backtick) keeps the pre-filter to the
// command word the guard actually governs.
var gitWordPattern = regexp.MustCompile(`(^|[\s"';&|()` + "`" + `])git([\s"';&|()` + "`" + `]|$)`)

// guardSharedStashIsolationThreshold is the worktree count above which the
// stash stack is a SHARED resource and a mutation is refused. One is the
// ordinary local case; two or more means another checkout exists that the
// stack's contents may belong to.
const guardSharedStashIsolationThreshold = 1

// gitSubcommandTimeoutSec bounds the two read-only git probes the guard runs.
// They are local metadata reads (`rev-parse`, `worktree list`), so a hung git
// is a broken environment, not a slow one — the guard fails CLOSED on the
// timeout (refusing the mutation) rather than letting it through unjudged.
const gitSubcommandTimeoutSec = 5

// guardSharedStash reports a non-empty refusal reason when command mutates the
// stash stack of a repository that has more than one linked worktree, and ""
// when the command is permitted (not a stash command at all, a read-only stash
// command, a single-worktree repo, or not inside a git repository).
//
// workdir is the session's working directory ("" = the process cwd). It is the
// directory the command's git call resolves its repository from, unless the
// command itself carries a `-C <dir>` operand — which is honoured, because
// `git -C <repo> stash pop` reaches the same shared stack from anywhere.
func guardSharedStash(command, workdir string) string {
	// Cheap textual pre-filter: the overwhelming majority of bash commands are
	// not git stash commands, and they must not pay for a git probe. Both
	// words are required so a `grep stash` (no git) never reaches the probe.
	stripped := stripQuotes(command)
	if !strings.Contains(stripped, stashCommandWord) || !gitWordPattern.MatchString(stripped) {
		return ""
	}

	verb, ok := stashMutationVerb(stripped)
	if !ok {
		// No clause invokes a stash MUTATION: either the word appeared in
		// another context (`echo git stash`, a PATH carrying the token) or the
		// only stash invocations are the read-only `list`/`show` — the
		// diagnosis path always passes. The false positive this replaces is
		// recorded above: a mere occurrence of the token (a PATH, a report
		// name) was refused as a mutation, and the epic's own `git stash pop;
		// echo "…"` was announced as the WRONG verb `push`.
		return ""
	}

	repoDir := stashGuardRepoDir(command, workdir)
	repoPath, worktrees := sharedStashRepoState(repoDir)
	if worktrees <= guardSharedStashIsolationThreshold {
		return ""
	}
	// The refusal quotes WHERE the repo is: the path the guard probed, which is
	// the directory the command's own git call resolves against.
	if repoPath == "" {
		repoPath = repoDir
	}
	return fmt.Sprintf(
		"`git stash %s` in %s would write THIS checkout's index/worktree from a stash stack shared by %d linked worktrees "+
			"(refs/stash lives in the repo's common git dir, so another session's WIP is on the same stack — a conflicted apply/pop "+
			"leaves UU entries over files your branch owns, and git KEEPS the entry, so every retry re-applies it). "+
			"Transfer non-destructively instead: `git stash show -p [stash@{N}]` to inspect, "+
			"`git -C <repo> diff stash@{N}^ stash@{N} -- <path> | git apply --3way` (or `git show stash@{N}:<path> > <path>`) "+
			"to move the EXACT files you mean into YOUR worktree, or `git stash branch <name>` to park the entry on a branch of its own",
		verb, repoPath, worktrees)
}

// stashMutationVerb reports the FIRST stash MUTATION verb invoked anywhere in
// the (already quote-stripped) command, plus whether such an invocation exists
// at all. EVERY clause is judged — the guard's job is to stop a mutation, not
// to classify the first stash word it meets:
//
//   - ok == false — no clause invokes a stash MUTATION: the line holds no
//     `git` COMMAND (its `git` token is an argument to another command —
//     `echo git stash`, `grep -rn stash file`), or the only stash invocations
//     are read-only (`list`/`show`, the diagnosis path), or the verb is one
//     this guard does not govern;
//   - ok == true, verb == "<v>" — some clause invokes a stash mutation whose
//     storage verb is EXACTLY the word git was given (`pop` is reported as
//     `pop`, never as its implicit-push default `push`), and that verb is the
//     one the refusal names.
//
// WHY every clause, and never the FIRST invocation (measured 2026-09-27, the
// residual bypass this loop closes): a predicate that RETURNED on the first
// clause carrying a stash invocation let a leading READ-ONLY clause answer for
// the whole line —
//
//	git rev-parse --show-toplevel; git stash list; echo "---run---"; git stash pop
//	→ PERMITTED, and git ran `stash pop` raw
//
// The single-verb spelling (`git stash pop`) was always the easy case; the
// chain is the shape an agent naturally emits, so the predicate must not be
// satisfiable by a clause that mutates nothing (devdoc/shared-stash-guard.md,
// "clause-scoped is not clause-exhaustive").
func stashMutationVerb(stripped string) (string, bool) {
	for _, clause := range strings.FieldsFunc(stripped, func(r rune) bool {
		return strings.ContainsRune(stashClauseSeparators, r)
	}) {
		if !stashWordIsInvocation(clause) {
			continue
		}
		verb := stashStorageVerbInClause(clause)
		if sharedStashReadOnlyVerbs[verb] {
			continue // read-only (`list`/`show`): the diagnosis path always passes
		}
		if !sharedStashMutationVerbs[verb] {
			continue // an unrecognised word is not this guard's to refuse
		}
		return verb, true
	}
	return "", false
}

// stashWordIsInvocation reports whether the stash token in clause is a
// subcommand of a real `git` invocation. It is the discriminator that makes the
// guard a predicate rather than a substring search (task 01a0c54a): a token
// inside a quoted string, inside a longer word (`gopi-stash-guard`), or in a
// clause with no `git` command (`echo …/gopi-stash-guard`) is NOT an
// invocation.
func stashWordIsInvocation(clause string) bool {
	words := strings.Fields(clause)
	stashIdx := -1
	gitIdx := -1
	for i, w := range words {
		if stashIdx < 0 && w == stashCommandWord {
			stashIdx = i
		}
		if gitIdx < 0 && w == gitBinaryName {
			gitIdx = i
		}
	}
	if stashIdx < 0 || gitIdx < 0 {
		return false
	}
	if stashIdx < gitIdx {
		return false // the token precedes the git command word — never its subcommand
	}
	// Every word before the git command word must be a wrapper command
	// (`sudo git stash pop`), one of ITS OWN options (`xargs -r git …`) or that
	// option's OPERAND (`sudo -u the operator git …`, `env FOO=1 git …`). Any other word
	// means the git token is an ARGUMENT to some other command — `echo git stash`,
	// `grep -rn stash file`.
	if head, ok := gitCommandHeadIdx(words, gitCommandPreambleWords); !ok || head != gitIdx {
		return false
	}
	// Between the git command word and the stash token there may only be git's
	// OPTIONS (and their operands). A plain word there IS git's own subcommand,
	// so the stash token is an argument to it (`git log --name-only stash`).
	i := gitIdx + 1
	for i < stashIdx {
		w := words[i]
		if !strings.HasPrefix(w, "-") && !envAssignPattern.MatchString(w) {
			return false
		}
		if gitOptionsWithOperand[w] {
			i += 2 // the option AND its operand (`git -C <dir> stash pop`)
			continue
		}
		i++
	}
	return true
}

// gitOptionsWithOperand are git's own options that consume the FOLLOWING word
// as their operand, so the word after one is not git's subcommand. The operand
// itself is honoured by stashGuardRepoDir, which probes the repository the
// command actually resolves against.
//   - gitDashCDirOption is the one of them the ref guard reads as an OPERAND
//     rather than skipping: `git -C <dir>` names the repository the
//     invocation resolves against, and one spelling has one home (Rule 13).
var gitOptionsWithOperand = gitargv.OptionsWithOperand

// gitDashCDirOption is `git -C <dir>`'s option word. It is a CONSTANT because two
// readers now resolve it (stashGuardRepoDir's line scan and
// refGuardResolve's per-invocation operand view) and a value used twice has one
// home (Rule 1b).
const gitDashCDirOption = gitargv.DashCDirOption

// gitProbeExitCode runs the same probe as gitProbeOutput and returns git's EXIT
// CODE, for the questions whose answer is a verdict rather than a value
// (`rev-parse --verify --quiet <ref>`: 0 = the ref exists, 1 = it does not). A
// probe that cannot be RUN is an error, deliberately distinct from git answering
// "no" — the two are different facts and only one of them is a judgement.
func gitProbeExitCode(dir string, args ...string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitSubcommandTimeoutSec*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = withoutGitDirEnv(os.Environ())
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil
		}
		return 0, fmt.Errorf("git %s in %q: %w", strings.Join(args, " "), dir, err)
	}
	return 0, nil
}

// stashStorageVerbInClause returns the storage verb of the clause's stash
// invocation — the word git was actually given, or the implicit-push default
// when the clause carries only options.
func stashStorageVerbInClause(clause string) string {
	words := strings.Fields(clause)
	for i, w := range words {
		if w != stashCommandWord {
			continue
		}
		if verb, ok := stashVerbAfterOptions(strings.Join(words[i+1:], " ")); ok {
			return verb
		}
		return stashMutationPush // `git stash` / `git stash --quiet` — implicit push
	}
	return stashMutationPush
}

// stashVerbAfterOptions returns the first word after the stash token once git's
// own options are skipped, and whether one was found at all. `--` is
// deliberately terminal: in git it ends option parsing, so a word after it is a
// PATHSPEC, not a subcommand — `git stash -- src` stashes paths, and reading
// `src` as a verb would be the wrong judgement either way.
func stashVerbAfterOptions(rest string) (string, bool) {
	for _, w := range strings.Fields(rest) {
		if w == "--" {
			return "", false
		}
		if strings.HasPrefix(w, "-") {
			continue // a git option (`--quiet`, `-q`, `--keep-index`) — never a verb
		}
		return w, true
	}
	return "", false
}

// stashGuardRepoDir resolves the directory the command's git call runs
// against: the `-C <dir>` operand when the command carries one (a single
// operand, git's documented shape), else the session workdir, else the
// process cwd.
func stashGuardRepoDir(command, workdir string) string {
	if dir := gitDashCOperand(command); dir != "" {
		return expandTilde(dir)
	}
	if workdir != "" {
		return workdir
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

// gitDashCOperand extracts the operand of a `-C <dir>` flag from a command,
// or "" when there is none. It is the same operand git itself resolves the
// repository from.
func gitDashCOperand(command string) string {
	toks := strings.Fields(stripQuotes(command))
	for i, tok := range toks {
		if tok == "-C" && i+1 < len(toks) {
			return toks[i+1]
		}
		if strings.HasPrefix(tok, "-C") && len(tok) > 2 {
			return tok[2:]
		}
	}
	return ""
}

// sharedStashRepoState resolves the repository root for dir and counts its
// LINKED WORKTREES (the main checkout included, which is what `git worktree
// list` reports). A non-repository, a git that cannot be run, or a timeout
// yields (path, 0) — zero worktrees, so the caller permits the command, which
// is the honest verdict: with no repository there is no shared stack to
// protect, and git itself will fail the command loudly.
func sharedStashRepoState(dir string) (string, int) {
	if dir == "" {
		return "", 0
	}
	if _, err := os.Stat(filepath.Join(dir)); err != nil {
		return dir, 0
	}
	// Count worktrees from the command's own directory, so a `-C`-free
	// command is judged where it runs.
	out, err := gitProbeOutput(dir, finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag)
	if err != nil {
		return dir, 0
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if _, ok := strings.CutPrefix(strings.TrimSpace(line), finishCleanupPorcelainWorktreePrefix); ok {
			n++
		}
	}
	return dir, n
}

// gitProbeOutput runs a READ-ONLY git probe (rev-parse / worktree list) in dir
// and returns its stdout, under gitSubcommandTimeoutSec so a hung git cannot
// stall the bash tool. The worktree-selection env vars are REMOVED from the
// child's environment so an inherited value cannot point the probe at a
// different repository than the command will use — the same isolation
// `worktree_sweep.go` applies to its own probes. They are removed rather than
// set to "": `GIT_DIR=` is not "unset" to git, it is an empty PATH, and git
// answers `fatal: The empty string is not a valid path` (measured) — which
// would make every probe fail and the guard fail OPEN.
func gitProbeOutput(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitSubcommandTimeoutSec*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = withoutGitDirEnv(os.Environ())
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s in %q: %w", strings.Join(args, " "), dir, err)
	}
	return string(out), nil
}

// withoutGitDirEnv returns env with every GIT_DIR/GIT_WORK_TREE/GIT_INDEX_FILE
// entry REMOVED (prefix-matched, so GIT_DIR is matched but GIT_DIRECTIVE… is
// not — git's own convention for these is exact-name, and a prefix match on
// the first name would over-remove).
func withoutGitDirEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if gitDirEnvNames[name] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// gitDirEnvNames are the environment variables that redirect git at another
// repository or index; the guard's probes must not inherit them.
var gitDirEnvNames = map[string]bool{
	"GIT_DIR":        true,
	"GIT_WORK_TREE":  true,
	"GIT_INDEX_FILE": true,
}
