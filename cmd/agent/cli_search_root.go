package main

// cli_search_root.go — the SHARED "is there anything here to search?" refusal
// for the two search CLI faces (`gopi tool code-search`, `gopi tool keyword_search`).
// One file, one predicate, two callers (Rule 3: no third tool, no new verb).
//
// THE DEFECT THIS REMOVES. Both verbs answered a search performed in the WRONG
// PLACE exactly as they answered a search that looked and found nothing:
//
//	$ cd $(mktemp -d) && gopi tool code-search taskverbs.DispatchVerbs
//	no symbol found: taskverbs.DispatchVerbs        # exit 1  — the symbol exists
//	$ cd <empty dir>   && gopi tool keyword_search <a token that exists in the repo>
//	No matches.                                     # exit 1  — same code, same shape
//
// The exit code and the output shape were identical to a genuine miss, so
// neither an LLM nor a script could tell "I searched and it is not there" from
// "I never searched your tree" — the second is a claim about a codebase that
// was never read. Measured (before/after table in
// devdoc/cli-search-wrong-ground-refusal.md): inside the repo a real symbol is
// exit 0 and a miss is exit 1, while both wrong-ground shapes were exit 1.
//
// THE SPLIT (Rule 15 — a named rejection is not the answer "nothing"):
//
//	exit 0  the search ran and found something
//	exit 1  the search RAN, on a real tree, and found nothing   ← an ANSWER
//	exit 4  the search could NOT run: nothing under the root matches the
//	        scope, so there was no code repository to search   ← a REFUSAL,
//	        printed with the absolute root, the channel, the git work tree it
//	        sits in, what the walk saw, and the remedy
//
// Exit 4 is the family precedent for "the universe is empty, and that is an
// ANSWER" (`gopi task show --branch`, internal/tools.TaskForBranchExitNoOwner),
// chosen so the two search verbs join that family instead of minting a third
// code. The same code also covers `--root` naming a non-directory: "the
// directory guard answered" and "there is nothing to search here" are one
// outcome to a caller — the search could not run.
//
// WHAT IT DELIBERATELY DOES NOT DO:
//   - It does not refuse a MISS. A root that DOES hold scannable sources stays
//     exit 1 with the miss line unchanged when nothing matches: refusing there
//     would break the "you searched the wrong tree, but this tree is real"
//     workflow the verb exists for.
//   - It is NOT a git guard in disguise: what makes a root searchable is at
//     least one file its scope covers — the SAME include globs the caller hands
//     the backend (Rule 14: one definition, no mirror).
//   - It is NOT satisfied by gopi's own state. A root holding nothing but the
//     profile directory and a sandbox ownership marker IS a directory with files
//     in it, so a bare "is there any file?" predicate passed it and
//     `keyword_search` answered "No matches." — exit 1, "I searched a real tree
//     and it is not there" — about a gopi profile that is no code repository at
//     all, while its `code-search` sibling refused the same directory by name.
//     One tree, two verbs, two readings: the two faces of one guard have to
//     agree about what a searchable root IS, so gopi's own state is set aside
//     by EXACT basename (searchScopeStateNames) — never by a `.json` suffix or
//     any other pattern, which would swallow a colleague's `package.json`.
//   - It never guesses when it cannot finish. A walk that hits its entry cap
//     without finding a source does NOT refuse: "I did not finish looking" is
//     not "there is nothing" (Rule 15), so the search proceeds exactly as
//     before.
//   - It changes NOTHING below the CLI boundary: tools.CodeSearch /
//     tools.KeywordSearch and their scopes are untouched. The agent loop's
//     contract is not what was ambiguous — the SHELL contract was.

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/config"
)

// searchExitOK is the shared success code: the search ran and found something.
// It is named for the same reason as its siblings — the -h code table is
// rendered from it, so the documented code and the process's own success path
// are one value (Rule 13).
const searchExitOK = 0

// searchExitNoHits is the shared "the search ran and found nothing" code — an
// ANSWER about a real tree, and the reason the refusal below needs its own
// number: these two must never be confused again.
const searchExitNoHits = 1

// searchExitWrongGround is the named refusal: the search could not run, because
// there is no code repository at the root. It is the family's existing "the
// universe is empty" code (tools.TaskForBranchExitNoOwner) and must not collide
// with the miss (searchExitNoHits) or with success.
const searchExitWrongGround = 4

// searchRootDefaultName is the channel label for the DEFAULT root — the process
// working directory, i.e. no --root was passed. Naming the channel is half the
// refusal: "the default (your cwd) pointed at <abs dir>" tells the caller which
// knob to turn, while an unlabelled "no results" leaves them guessing.
const searchRootDefaultName = "default (cwd)"

// searchRootFlag is the one flag the two verbs accept for the root, named once
// (Rule 13/14) because the two parsers, the two usage pages, the channel line
// and the advice line must agree on the spelling.
const searchRootFlag = "root"

// searchGroundLabel is the refusal's first line: what could NOT be found. A
// caller greps this line or reads the exit code; the two carry the same fact.
const searchGroundLabel = "refusing: no code repository at the search root"

// searchGroundChannelFmt names the CHANNEL the root came from and the ABSOLUTE
// directory it resolved to. Both halves matter: the channel says which knob to
// turn, the absolute path says where the process actually stood (a relative
// "." would name nothing in a log read from elsewhere).
const searchGroundChannelFmt = "  channel: %s; root = %s\n"

// searchGroundSlotFmt reports what the root IS, when the root is inside a git
// work tree: that work tree's toplevel, and this root's slot in it.
const searchGroundSlotFmt = "  in git work tree: %s (this root is %s)\n"

// searchGroundSlotSelf is the slot value for a root that IS the work-tree
// toplevel.
const searchGroundSlotSelf = "the toplevel"

// searchGroundNoWorkTreeLine is the honest half when git cannot name a
// work-tree at all — INCLUDING the case where git is not installed: the refusal
// states the absence of evidence, never a guess about the machine.
const searchGroundNoWorkTreeLine = "  git work tree: none (not inside any git work tree, or git is unavailable)\n"

// searchGroundScanFmt counts what the walk actually saw, so "nothing here is
// searchable" is an observation rather than a shrug. The scope's own extension
// list is printed because it IS the criterion: a directory full of .txt/.pdf is
// a real directory that this particular search cannot read.
const searchGroundScanFmt = "  scan: %d entries under the root, 0 files matching the search scope (scope extensions: %s)\n"

// searchGroundScanAllFmt is the same line for the full-text verb, whose scope
// has no extension filter (ripgrep reads every file): the criterion is then
// simply "no file at all", and saying so is more honest than printing a list.
const searchGroundScanAllFmt = "  scan: %d entries under the root, 0 files at all (this scope reads every file)\n"

// searchGroundStateLead is the state line's leading phrase — the sentence both
// search faces print, and the words a test and a reader grep for. One literal,
// used by the format string below and asserted by the contract test (Rule 13).
const searchGroundStateLead = "what IS here is gopi's own state, not searchable material"

// searchGroundStateFmt renders the state line; the lead phrase is taken from its
// constant so the printed sentence and the asserted one cannot drift apart.
const searchGroundStateFmt = "  " + searchGroundStateLead + ": %s\n"

// searchGroundStateJoin separates the state classes the refusal names.
const searchGroundStateJoin = ", "

// searchGroundScopeNoExt is printed for a scope with an empty extension list
// (every file matches) — never an empty parenthesis.
const searchGroundScopeNoExt = "any file"

// searchGroundScanRootDirsFmt is the same scan when the root holds git work
// trees and no sources of its own: naming them is what turns the refusal into a
// remedy (the caller wanted --root <one of these>).
const searchGroundScanRootDirsFmt = "  scan: %d entries under the root, 0 files matching the search scope; it contains git work tree(s): %s\n"

// searchGroundUndeterminedLine is the honest third outcome: the root could not
// be read, so whether it holds searchable files is UNKNOWN. Never merged into
// the "no sources here" case (Rule 15).
const searchGroundUndeterminedLine = "  scan: the root could not be read, so whether this directory holds searchable files is UNKNOWN\n"

// searchGroundAdviceLine names the remedy — the third element the epic asks for
// (channel + reason + what to do). It is what turns exit 4 from a wall into a
// next step, and it restates the split in words so a human reading stderr is not
// misled by the "no results" reading that exit 1 carries.
const searchGroundAdviceLine = "  pass --root DIR naming the repository (or run from inside it) — this is NOT a \"no results\" answer: the search did not run\n"

// searchGroundMaxEntries bounds the walk behind a refusal. The refusal is a
// diagnostic attached to a failed command, so it must never be the expensive
// part of that command: the walk stops here, and a walk that stopped early
// without finding a source reports UNKNOWN instead of refusing.
const searchGroundMaxEntries = 20000

// searchGroundNestedProbeCap bounds the per-directory git probes the refusal
// runs to name the git work trees under the root (each probe is one git
// process). Beyond it the list is truncated and says so.
const searchGroundNestedProbeCap = 32

// searchRootChannel names WHERE the root came from: the flag, or the default
// (the process working directory). The parser records the fact; this is its one
// reader, so a verb that starts accepting another root spelling must declare it
// here.
func searchRootChannel(given bool) string {
	if given {
		return "--" + searchRootFlag
	}
	return searchRootDefaultName
}

// searchableExtensionsIn returns the extensions the given scope hands the
// backend, deduplicated and in scope order (a nil include list means the
// full-text verb, which scans every file). They are derived from the SAME glob
// strings the caller passes to tools.*Search, so this predicate can never
// disagree with the search it guards about which files count.
//
// The glob shapes are ripgrep's (`**/*.go`, and any leading `*`/`/` run is
// glob syntax rather than part of the extension): the extension is the final
// path element's suffix, which is why the derivation goes through the path
// splitter instead of trimming characters by hand — the first cut of this
// function trimmed leading `*` and produced "/*.go", a string that matches no
// file and would have made the refusal fire on a healthy checkout (measured:
// 5910 entries walked, 0 "matching").
func searchableExtensionsIn(include []string) []string {
	if len(include) == 0 {
		return nil // no include filter: every file is scannable
	}
	seen := map[string]bool{}
	var exts []string
	for _, g := range include {
		ext := path.Ext(g)
		if ext == "" || seen[ext] {
			continue
		}
		seen[ext] = true
		exts = append(exts, ext)
	}
	return exts
}

// searchScopeExcludedDir reports whether a directory entry is outside the
// search scope for BOTH verbs: the callers hand `.git/**` and `vendor/**` to
// their backends as exclusions, so files under them are not searchable material
// and must not make an otherwise empty root look like a repository.
func searchScopeExcludedDir(name string) bool {
	return name == searchScopeGitDir || name == searchScopeVendorDir
}

// The two directory names both verbs exclude (the globs their callers build).
const (
	searchScopeGitDir    = ".git"
	searchScopeVendorDir = "vendor"
)

// searchScopeStateDir reports whether a directory entry is gopi's own state: the
// profile directory (config.ProfileDirName), by the name the initializer
// actually writes it under (Rule 14 — a mirrored literal here would silently
// stop matching the day the profile name changes). Its whole subtree is state —
// sessions, raw logs, the ledger, a settings symlink — and none of it is a code
// repository, so the walk does not descend into it. This is what keeps a sandbox
// (`.gopi/` + the ownership marker, nothing else) and a bare `GOPI_HOME` from
// passing as searchable.
//
// The rule is exact basename match, deliberately: a suffix test on ".json"
// would call a colleague's `package.json` gopi state and refuse a real tree.
func searchScopeStateDir(name string) bool {
	return name == config.ProfileDirName
}

// searchScopeStateFile reports whether a FILE is gopi's own state rather than
// material the scope covers. Only the sandbox ownership marker
// (config.SandboxOwnerFileName) qualifies: the profile directory is handled as a
// directory, so a stray `.gopi` file could never be confused with it.
func searchScopeStateFile(name string) bool {
	return name == config.SandboxOwnerFileName
}

// searchGroundWalk is the shallow observation behind the refusal: what the walk
// under the root saw, and whether it was allowed to finish.
type searchGroundWalk struct {
	visited    int      // entries walked (capped by searchGroundMaxEntries)
	scannable  int      // files matching the scope
	stateNames []string // gopi's own state seen (profile dir, owner marker), in walk order
	topDirs    []string // top-level directories (candidates for the work-tree probe)
	truncated  bool     // the cap stopped the walk before it finished
	readable   bool     // the root could be read at all
}

// markState records one entry of gopi's own state, once per name: the refusal
// names the CLASSES it saw ("what IS here"), so the same class met twice is not
// reported twice.
func (g *searchGroundWalk) markState(name string) {
	for _, seen := range g.stateNames {
		if seen == name {
			return
		}
	}
	g.stateNames = append(g.stateNames, name)
}

// searchGroundWalkRoot walks the root the way the backends do — recursively,
// with the two excluded directories skipped — and counts the files the scope
// covers. It stops as soon as one is found: the question is "is there anything
// to search", so the first scannable file IS the answer.
//
// gopi's own state (the profile directory, the sandbox ownership marker) is set
// aside here rather than counted as material: it is the one class of entry that
// is a file without being anything to search, and counting it is exactly how a
// sandbox profile passed for a repository. The walk does not descend into the
// profile at all — a whole `.gopi` tree is state.
func searchGroundWalkRoot(root string, include []string) searchGroundWalk {
	g := searchGroundWalk{}
	exts := searchableExtensionsIn(include)
	scannable := func(name string) bool {
		if exts == nil {
			return true // the full-text scope reads every file
		}
		lower := strings.ToLower(name)
		for _, ext := range exts {
			if strings.HasSuffix(lower, strings.ToLower(ext)) {
				return true
			}
		}
		return false
	}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err // the root itself is unreadable — the caller reports UNKNOWN
			}
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path != root && filepath.Dir(path) == root {
			g.topDirs = append(g.topDirs, path)
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			if searchScopeStateDir(d.Name()) {
				g.markState(d.Name())
				return fs.SkipDir
			}
			if searchScopeExcludedDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		g.visited++
		if g.visited > searchGroundMaxEntries {
			g.truncated = true
			return fs.SkipAll
		}
		if searchScopeStateFile(d.Name()) {
			g.markState(d.Name())
			return nil
		}
		if scannable(d.Name()) {
			g.scannable++
			return fs.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return g // readable stays false: the answer is UNKNOWN
	}
	g.readable = true
	return g
}

// searchGitWorkTreeOf resolves the work tree a directory lives in: the git
// toplevel, plus whether git named one at all. The bool is the honest half —
// "not inside any work tree" and "git could not be asked" both come back false,
// and the refusal says so rather than guessing.
func searchGitWorkTreeOf(dir string) (string, bool) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--show-toplevel").Output()
	if err != nil {
		return "", false
	}
	top := strings.TrimSpace(string(out))
	if top == "" {
		return "", false
	}
	return top, true
}

// searchNestedWorkTrees names the top-level subdirectories of root that are
// themselves git work trees: the shape where a bare "nothing here" would be
// actively misleading, because the operator is one path segment away from the
// real tree (`/home/nick/workspace` holds work trees and no sources of its own).
//
// A subdirectory counts only when it IS a work tree's toplevel. Asking git alone
// is not enough: from inside an existing work tree, `rev-parse --show-toplevel`
// answers with the ENCLOSING work tree for every subdirectory, which would print
// the root's own children as a list of repositories (measured — the first cut
// named cmd/, internal/ and devdoc/ this way). So the answer is compared to the
// directory that produced it.
//
// Each probe is one git process, so the scan is capped and a capped scan says so.
func searchNestedWorkTrees(candidates []string) []string {
	var roots []string
	probed := 0
	for _, path := range candidates {
		if probed >= searchGroundNestedProbeCap {
			roots = append(roots, fmt.Sprintf("(+%d more directories not probed)", len(candidates)-probed))
			break
		}
		probed++
		top, ok := searchGitWorkTreeOf(path)
		if !ok {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		if filepath.Clean(top) != filepath.Clean(abs) {
			continue // a subdirectory of some work tree, not a work tree itself
		}
		roots = append(roots, fmt.Sprintf("%s (%s)", filepath.Base(path), top))
	}
	return roots
}

// searchRootRefusal is the whole decision: given the post-parse root, the
// channel it came from and the include globs the backend will receive, should
// the command refuse instead of searching? It returns the rendered refusal
// (refused=false → run the search) so the renderer and the tests read ONE
// computation.
//
// The refusal fires on exactly one provable condition: a FINISHED walk under the
// root found no file the scope covers (or the root could not be read at all).
func searchRootRefusal(root, channel string, include []string) (string, bool) {
	walk := searchGroundWalkRoot(root, include)
	if walk.scannable > 0 || walk.truncated {
		return "", false
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", searchGroundLabel)
	fmt.Fprintf(&b, searchGroundChannelFmt, channel, root)
	top, inRepo := searchGitWorkTreeOf(root)
	if !inRepo {
		b.WriteString(searchGroundNoWorkTreeLine)
	} else {
		slot := searchGroundSlotSelf
		if rel, err := filepath.Rel(top, root); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			slot = rel
		}
		fmt.Fprintf(&b, searchGroundSlotFmt, top, slot)
	}
	switch {
	case !walk.readable:
		b.WriteString(searchGroundUndeterminedLine)
	default:
		if roots := searchNestedWorkTrees(walk.topDirs); len(roots) > 0 {
			fmt.Fprintf(&b, searchGroundScanRootDirsFmt, walk.visited, strings.Join(roots, ", "))
			break
		}
		exts := searchableExtensionsIn(include)
		if exts == nil {
			fmt.Fprintf(&b, searchGroundScanAllFmt, walk.visited)
		} else {
			fmt.Fprintf(&b, searchGroundScanFmt, walk.visited, strings.Join(exts, " "))
		}
		if len(walk.stateNames) > 0 {
			fmt.Fprintf(&b, searchGroundStateFmt, strings.Join(walk.stateNames, searchGroundStateJoin))
		}
	}
	b.WriteString(searchGroundAdviceLine)
	return b.String(), true
}

// searchGroundRootMissing renders the refusal for a --root that is not a
// directory at all: the same outcome to a caller (the search could not run),
// with its own named reason. It is a separate renderer because the reason is
// different — the walk cannot even start — and mixing the two into one message
// would make the "no repository" claim false.
func searchGroundRootMissing(root string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", searchGroundLabel)
	fmt.Fprintf(&b, searchGroundChannelFmt, "--"+searchRootFlag, root)
	fmt.Fprintf(&b, "  reason: %s is not a readable directory, so the search could not start\n", root)
	b.WriteString(searchGroundAdviceLine)
	return b.String()
}

// searchRootIsDir reports whether path names a directory that can be walked.
func searchRootIsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// searchGuardOrExit is the ONE call site of the guard, shared by both search
// verbs — the whole point of the file (Rule 14: one definition, two callers).
// It exits the process with searchExitWrongGround when the search cannot run,
// and returns normally when it can (the caller then runs it and keeps its own
// exit contract: 0 for hits, searchExitNoHits for a genuine miss). prefix is the
// verb's own stderr prefix, so the refusal names the command that refused.
func searchGuardOrExit(prefix, root, channel string, include []string) {
	if !searchRootIsDir(root) {
		fmt.Fprintf(os.Stderr, "%s: %s", prefix, searchGroundRootMissing(root))
		os.Exit(searchExitWrongGround)
	}
	if refusal, refused := searchRootRefusal(root, channel, include); refused {
		fmt.Fprintf(os.Stderr, "%s: %s", prefix, refusal)
		os.Exit(searchExitWrongGround)
	}
}

// searchGroundLabelLead is the refusal label's leading phrase, used by the -h
// page so a reader who greps the docs and one who greps a log match the same
// words without the page having to repeat the whole sentence.
const searchGroundLabelLead = "REFUSED"
