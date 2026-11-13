package agent

// projectctx.go — WHERE the project context file (PROJECT.md / GOPI.md) comes from.
//
// THE TWO NAMES (2026-10-19). The root rule file was SPLIT BY CONTENT KIND: the
// project's engineering contract (Rules 1-41) is the TRACKED `PROJECT.md` — it
// holds in a bare clone on any machine — while the host-local operations policy
// stayed in `GOPI.md`, which is gitignored and never travels. The loader accepts
// BOTH names, in priority order: `PROJECT.md` first, `GOPI.md` second. A
// directory holding both resolves to PROJECT.md and the loser is NAMED on stderr
// (Rule 15: a conflict is never silent). A directory holding only `GOPI.md`
// behaves exactly as it did before the split, which is what keeps every existing
// checkout and every machine-local rule file working.
//
// THE DEFECT THIS CLOSES (measured 2026-09-21): the loader read the file from
// the process cwd ONLY, so any run started from a subdirectory of the repo —
// a build in cmd/agent, a test run in internal/tools, a `cd` inside a bash
// tool call — silently lost the entire project context. No error, no warning,
// just a system prompt without the project's rules.
//
// THE TWO-BOUNDARY MODEL. The search has exactly two stops, both decided by
// THIS project's own rules rather than by a user knob (a knob that must be
// set per project is the failure mode this replaces):
//
//  1. THE REPO BOUNDARY — where the file's search must NOT continue past.
//     A `GOPI.md` is a PROJECT's rules file, so a project's own boundary
//     answers it. The boundary is the repo this machine treats as the file
//     GOVERNS: inside a linked worktree that is the WORKTREE ITSELF (its own
//     checkout carries its own tracked copies of the file); in the main
//     checkout it walks up through nested checkouts to the OUTERMOST
//     repository — so a nested repo (a test fixture, a vendored checkout)
//     cannot answer for the project the operator is standing in.
//
//  2. THE FALLBACK — nothing but the search's own sanity bounds. A directory
//     outside version control may still hold the file (a project under
//     development, a scratch tree), so the walk continues to the FILESYSTEM
//     ROOT, subject only to a hop ceiling that can never bind on a real host
//     and a visited-directory guard against a symlink loop.
//
// The boundary is never enforced by a git call: it is a second walk over the
// directory entries inside the hop budget already being walked for the file
// (isRepoBoundary / isWorktreeTop — one os.Stat each), so the resolution
// costs one wall-clock syscall per hop and shells out to nothing.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/config"
)

const (
	// projectContextFileName is the PROJECT's rules file, by NAME — the tracked
	// one, the one a bare clone carries. Its value is also the product's public
	// contract (`gopi --help`'s input row, CONFIG.md, the runner's
	// project-context hooks), and this file is therefore its documented source
	// of truth for the loader; the tests reference this constant rather than
	// retyping the literal (Rule 1b/13).
	projectContextFileName = "PROJECT.md"

	// localProjectContextFileName is the CLIENT's own rules file: the operator's
	// host-local policy (this machine's addresses, ssh aliases, task-sync pins,
	// the ```chat-policy fence). It is gitignored at the repository root by
	// design (2026-10-04) and therefore exists only on the host that wrote it —
	// which is exactly why it cannot be the ONLY name the loader accepts.
	localProjectContextFileName = "GOPI.md"

	// projectContextMaxHops bounds the upward walk. It can never bind on a
	// real host (4096 levels against a PATH_MAX of 4096 bytes — even a
	// one-character name per level runs out of bytes first), so it is a
	// SYMLINK-LOOP failure bound, not a policy: a directory cycle would
	// otherwise spin forever. The visited-dir guard below is the primary
	// loop defence; this is the belt to its braces.
	projectContextMaxHops = 4096

	// maxSearchedDirsPrinted caps the "searched these directories" list on
	// stderr. The walk itself is unbounded to the filesystem root; only the
	// RENDERING is capped, and a capped rendering says how many it dropped
	// (Rule 15: a truncation is announced, never silent).
	maxSearchedDirsPrinted = 16

	// gitDirEntryName is a repository's administrative entry: a DIRECTORY in
	// a main checkout, a FILE in a linked worktree. The distinction is the
	// whole of isWorktreeTop (Rule 1b: the entry's name has one source here).
	gitDirEntryName = ".git"
)

// ProjectContextFile is the resolved project context: the file's absolute
// path ("" when no file was found — the optional case) and the bounded list
// of directories the search actually visited, in order.
type ProjectContextFile struct {
	// Path is the absolute path of the file that was loaded, "" when none.
	Path string
	// Searched is every directory visited, in walk order.
	Searched []string
	// Ignored are the absolute paths of accepted-name SIBLINGS that exist in
	// the directory that answered but LOST the priority order — a directory
	// holding both PROJECT.md and GOPI.md reports the second one here so the
	// caller can say which file won and which was passed over (Rule 15: the
	// conflict is stated, never resolved in silence).
	Ignored []string
}

// projectContextFileNames are the accepted names, in PRIORITY ORDER: the
// project's TRACKED contract first, the client's local file second. Order is
// the whole rule — index 0 wins a conflict — so it is a named value, not an
// argument a caller may permute (Rule 1/14).
var projectContextFileNames = []string{projectContextFileName, localProjectContextFileName}

// Dir is the directory holding the resolved file ("" when none was found) —
// what a log line needs to say WHERE the context came from.
func (p ProjectContextFile) Dir() string {
	if p.Path == "" {
		return ""
	}
	return filepath.Dir(p.Path)
}

// ResolveProjectContextFile walks up from start (a directory; "" means the
// process cwd) looking for the accepted rule-file names IN PRIORITY ORDER
// (projectContextFileNames), stopping — in this order — at the first directory
// that holds one of them, at the repo boundary described in the file comment
// above, or at the filesystem root.
//
// It is the ONE read-path decision of the context loader (ResolveLocalProject-
// ContextFile is the same walk with the names reversed): it is called once
// per session start and the loop never re-reads (prompt-cache byte stability).
// Absence is a RESULT, never an error: a directory with no project context
// resolves to a zero Path and a working run.
func ResolveProjectContextFile(start string) ProjectContextFile {
	return resolveProjectContextFileWith(start, projectContextFileNames)
}

// ResolveLocalProjectContextFile is the SECOND resolver, for the readers that
// want the CLIENT's document rather than the project's: the ```chat-policy and
// ```chat-op-category fences are this machine's operations policy, so when a
// directory holds both files the LOCAL name wins here — the opposite priority
// from the prompt-injection walk. One walk, two name orders (Rule 2/14): the
// boundary rules, hop budget and loop guards are the same code.
func ResolveLocalProjectContextFile(start string) ProjectContextFile {
	return resolveProjectContextFileWith(start, localFirstProjectContextFileNames)
}

// localFirstProjectContextFileNames is the fence readers' priority order: the
// client's own file first, the tracked contract second.
var localFirstProjectContextFileNames = []string{localProjectContextFileName, projectContextFileName}

// resolveProjectContextFileWith is the walk itself, with the accepted names'
// PRIORITY ORDER as an argument — the only thing the two resolvers disagree on.
func resolveProjectContextFileWith(start string, names []string) ProjectContextFile {
	dir := strings.TrimSpace(start)
	if dir == "" {
		// getwd's error is deliberately ignored: an unreachable cwd leaves
		// dir == "", which the resolution below answers exactly as an
		// unavailable directory — "no project context, here is where we looked"
		// — which is the same outcome and never a hard failure (optional
		// stays optional).
		dir, _ = os.Getwd() //stylegate:ignore an unreachable cwd leaves dir empty, which the resolution below answers exactly as an unavailable directory (no project context)
	}
	if dir == "" {
		return ProjectContextFile{}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}

	var searched []string
	visited := make(map[string]bool)
	for hop := 0; hop <= projectContextMaxHops; hop++ {
		if dir == "" || visited[dir] {
			break
		}
		visited[dir] = true
		searched = append(searched, dir)

		// PRIORITY ORDER IS THE WHOLE RULE: at THIS hop the accepted names are
		// tried in order and the first one that exists answers; every OTHER
		// accepted name that also exists here is recorded as IGNORED, so the
		// caller can name the document the session actually received AND the one
		// the priority order passed over (Rule 15 — a conflict is stated, never
		// resolved in silence).
		//
		// THE HOP IS THE UNIT, never the walk: the walk still stops at the first
		// DIRECTORY that answers, so a local GOPI.md one level up can never
		// outrank a tracked PROJECT.md found here (distance wins over name — the
		// behaviour the single-name walk already had).
		winner, ignored := resolveAcceptedNameIn(dir, names)
		if winner != "" {
			return ProjectContextFile{Path: winner, Searched: searched, Ignored: ignored}
		}
		if isRepoBoundary(dir) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir { // the filesystem root
			break
		}
		dir = parent
	}
	return ProjectContextFile{Searched: searched}
}

// resolveAcceptedNameIn answers ONE hop: the accepted name that wins in dir
// ("" when the directory holds none) and the accepted-name siblings that exist
// there but lost the priority order.
//
// The loser list is a RESULT of the same stat pass, not a second question: a
// caller that had to re-stat the directory to learn what it passed over could
// disagree with what was loaded.
func resolveAcceptedNameIn(dir string, names []string) (winner string, ignored []string) {
	for _, name := range names {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if winner == "" {
			winner = candidate
			continue
		}
		ignored = append(ignored, candidate)
	}
	return winner, ignored
}

// isRepoBoundary reports whether the walk must stop AFTER dir, because dir is
// the top of the project the file governs:
//
//   - a LINKED worktree's top — its own checkout, its own tracked file, and
//     the walk must not continue into the main checkout beside it;
//   - a repository inside a repository — the OUTERMOST one wins, because the
//     outer repo is what the operator is standing in (the inner one is a
//     fixture, a vendored copy, or a worktree artifact). This is what keeps a
//     nested checkout from answering for the enclosing project.
//
// A plain repository with nothing repo-shaped above it is NOT a boundary: the
// walk continues to the filesystem root (an ancestor file is then the answer),
// which is the deliberate simplicity stop for directories outside version
// control.
func isRepoBoundary(dir string) bool {
	if isWorktreeTop(dir) {
		return true
	}
	if !isRepoTop(dir) {
		return false
	}
	// A repo with no repo above it is the outermost one: the walk's stop.
	// A repo WITH one above it is nested (a fixture, a vendored checkout) —
	// the outer project is the one the operator stands in, so the walk keeps
	// climbing and stops at the outer top instead.
	return !isRepoTop(filepath.Dir(dir))
}

// isRepoTop reports whether dir holds a repository's administrative
// directory directly (the `.git` entry — a DIRECTORY in a main checkout, a
// FILE in a linked worktree).
func isRepoTop(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, gitDirEntryName))
	return err == nil
}

// isWorktreeTop reports whether dir is the top of a LINKED git worktree: its
// `.git` entry is a FILE (the gitdir pointer). A `.git` DIRECTORY is a main
// checkout or a plain repository — not a worktree (the same distinction
// internal/daemoncwd draws for the same reason: a worktree is a checkout of
// its own, a main checkout is the project itself).
func isWorktreeTop(dir string) bool {
	info, err := os.Lstat(filepath.Join(dir, gitDirEntryName))
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// searchDirsLine renders the searched-directory list: a comma-separated,
// bounded list joined to one line. Bounded means the RENDERING is capped, not
// the walk — a dropped tail is stated with its count, so the line is never a
// silent truncation of what the search actually did (Rule 15).
func searchDirsLine(dirs []string) string {
	if len(dirs) == 0 {
		return "(the working directory was unavailable — no directory could be searched)"
	}
	shown := dirs
	dropped := 0
	if len(shown) > maxSearchedDirsPrinted {
		dropped = len(shown) - maxSearchedDirsPrinted
		shown = shown[:maxSearchedDirsPrinted]
	}
	line := strings.Join(shown, ", ")
	if dropped > 0 {
		line += fmt.Sprintf(", … (%d more)", dropped)
	}
	return line
}

// systemProjectContextPath resolves the SYSTEM-level rules file: the one that
// rides the gopi PROFILE (<config.GopiDir()>/GOPI.md) instead of a checkout.
//
// IT KEEPS THE LOCAL NAME ON PURPOSE. The profile is THIS MACHINE, so the file
// there is the operator's own policy — the very content that must never be
// tracked. A tracked PROJECT.md in the profile would be a second, mobile copy of
// the repository contract, which is the duplication the split removed.
//
// WHY THIS LEVEL EXISTS (measured 2026-10-19): GOPI.md is gitignored by design
// (it is the client's own rule file — see .gitignore:119), so a bare clone, a
// worker's worktree and a sandbox profile all start with NO project rules at
// all, silently. The repository level answers "what this checkout says"; it
// cannot answer "what this machine's operator always wants", because that file
// never travels with the code. The profile is where this product already keeps
// its per-machine facts (envctx.Path() resolves environment.md the same way),
// so the system-level rules live beside it — one location, resolved through
// ONE function (Rule 2/14), never a hardcoded /home/nick/.gopi and never a shell path.
//
// The path is a RESULT even when it cannot be resolved: an unresolvable profile
// (no HOME, a refused nested GOPI_HOME) leaves ok=false, which the caller
// reports as the system level being missing — the repository level still loads
// and the run continues (optional stays optional, Rule 15: it says why).
func systemProjectContextPath() (path string, ok bool) {
	dir, err := config.GopiDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] system %s unavailable: the gopi profile directory could not be resolved (%v) — continuing with the repository level only\n",
			localProjectContextFileName, err)
		return "", false
	}
	return filepath.Join(dir, localProjectContextFileName), true
}

// The merge headings. Two VALUES, because they are what an operator greps for
// in a session log and what a reader of the assembled prompt sees: the merge
// must be visibly TWO documents, not one blended one (Rule 15 — an injected
// block says where it came from).
const (
	// The heading FRAMES are the constant halves; the FILE NAME between them is
	// filled from the path the level actually loaded, so a session that got
	// PROJECT.md and a session that got GOPI.md each state which document their
	// rules came from (Rule 15 — an injected block says where it came from).
	systemHeadingPrefix     = "## System rules ("
	systemHeadingSuffix     = " in the gopi profile)"
	repositoryHeadingPrefix = "## Repository rules ("
	repositoryHeadingSuffix = ")"

	// systemProjectContextHeading / repositoryProjectContextHeading are the
	// DEFAULT renderings (the tracked name), kept as values because tests and
	// readers compare against them; a level that loaded the local name renders
	// through systemHeading / repositoryHeading with that basename instead.
	systemProjectContextHeading     = systemHeadingPrefix + projectContextFileName + systemHeadingSuffix
	repositoryProjectContextHeading = repositoryHeadingPrefix + projectContextFileName + repositoryHeadingSuffix
)

// systemHeading renders the profile-level heading for the document actually
// loaded (its basename), so the prompt names the real file rather than the
// default one.
func systemHeading(name string) string { return systemHeadingPrefix + name + systemHeadingSuffix }

// repositoryHeading renders the checkout-level heading for the document
// actually loaded (its basename).
func repositoryHeading(name string) string {
	return repositoryHeadingPrefix + name + repositoryHeadingSuffix
}

// loadedName is the document name a level reports and heads itself with: the
// basename of the file it read, or the tracked default when the level is
// absent (that form is never rendered — an absent level contributes nothing).
func loadedName(level projectContextLevel) string {
	if level.Path == "" {
		return projectContextFileName
	}
	return filepath.Base(level.Path)
}

// projectContextLevel is ONE level's resolved outcome: the absolute path the
// level resolved to ("" when nothing was found) and its bytes.
type projectContextLevel struct {
	// Path is the file this level loaded, "" when the level is absent.
	Path string
	// Body is the file's bytes EXACTLY as read (the loaded line reports this
	// length, so the operator's byte count is the file's own); the merge trims
	// only the rendering edges. "" when the level is absent.
	Body string
	// Unreadable is set when a file was found and os.ReadFile failed: the
	// level still counts as ABSENT (returned Body ""), with the reason kept so
	// the caller reports "there but unreadable" rather than "no file".
	Unreadable error
	// Unresolved marks "the profile directory itself could not be resolved" —
	// a reason the caller prints INSTEAD of the plain absence line, because the
	// remedy is different (no profile at all, vs. a profile without rules).
	Unresolved bool
	// Searched is the repository walk's visited directories, in order. Only
	// the repository level has one (the profile level is a single fixed path),
	// and it exists to print WHERE the walk looked when nothing was found.
	Searched []string
	// Ignored are the accepted-name SIBLINGS that exist beside the file this
	// level loaded and lost the priority order (PROJECT.md beats GOPI.md). The
	// caller NAMES them on stderr: a directory holding both documents resolves
	// to one, and the operator must be told which one was passed over rather
	// than discovering it from a prompt that omits half their rules (Rule 15).
	Ignored []string
}

// loadSystemProjectContext reads the profile-level file. Absence is a result:
// the returned level carries no body and no error path.
//
// The read is the SAME one-read contract as the repository level: once per
// session start, never re-read (prompt-cache byte stability).
func loadSystemProjectContext() projectContextLevel {
	path, ok := systemProjectContextPath()
	if !ok {
		return projectContextLevel{Unresolved: true}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return projectContextLevel{}
		}
		return projectContextLevel{Path: path, Unreadable: err}
	}
	return projectContextLevel{Path: path, Body: string(data)}
}

// loadRepositoryProjectContext reads the repository-level file through the
// EXISTING walk (ResolveProjectContextFile) — its criteria are deliberately
// untouched by the two-level change: this level answers exactly what it
// answered before, and the profile level is a second, independent source.
func loadRepositoryProjectContext(start string) projectContextLevel {
	found := ResolveProjectContextFile(start)
	if found.Path == "" {
		return projectContextLevel{Searched: append([]string(nil), found.Searched...)}
	}
	data, err := os.ReadFile(found.Path)
	if err != nil {
		return projectContextLevel{
			Path:       found.Path,
			Unreadable: err,
			Searched:   append([]string(nil), found.Searched...),
			Ignored:    append([]string(nil), found.Ignored...),
		}
	}
	return projectContextLevel{
		Path:     found.Path,
		Body:     string(data),
		Searched: append([]string(nil), found.Searched...),
		Ignored:  append([]string(nil), found.Ignored...),
	}
}

// mergeProjectContext assembles the session's project context from the two
// independently-optional levels: SYSTEM first, REPOSITORY second — a FIXED
// order, never the caller's choice.
//
// The output is a pure function of the two bodies: no timestamp, no hostname,
// no map iteration, no environment read inside the render — the same inputs
// produce the same BYTES (bytes.Equal on two calls), which is what keeps the
// prompt cache stable across sessions and workers.
//
// A level that is absent contributes nothing: no heading, no separator, no
// blank line that would leak its absence into the bytes.
func mergeProjectContext(system, repository string) string {
	return mergeProjectContextNamed(system, repository, projectContextFileName, projectContextFileName)
}

// mergeProjectContextNamed is mergeProjectContext with each level's DOCUMENT
// NAME supplied, so the two headings name the files this session actually
// loaded (a checkout carrying only the local GOPI.md heads itself GOPI.md).
// Determinism is unchanged: the names are inputs, never read from the
// environment inside the render.
func mergeProjectContextNamed(system, repository, systemName, repoName string) string {
	blocks := make([]string, 0, 2)
	if s := strings.TrimSpace(system); s != "" {
		blocks = append(blocks, systemHeading(systemName)+"\n\n"+s)
	}
	if r := strings.TrimSpace(repository); r != "" {
		blocks = append(blocks, repositoryHeading(repoName)+"\n\n"+r)
	}
	return strings.Join(blocks, "\n\n")
}

// LoadProjectContext resolves BOTH levels of the project context once and
// returns their merged bytes for the session's second system message. It
// reports each outcome on stderr, never to a caller's error path — every level
// is OPTIONAL:
//
//	loaded:   "[gopi] loaded PROJECT.md (N bytes) [repository] — from <path>"
//	          (the NAME is the file that won: GOPI.md when only the local file
//	          exists, which is the pre-split reading of the same line)
//	          and the same line tagged [system] for the profile level
//	conflict: "[gopi] ignoring <path>/GOPI.md — PROJECT.md wins (priority order
//	          PROJECT.md, then GOPI.md), so only PROJECT.md was injected"
//	absent:   "[gopi] no repository PROJECT.md, then GOPI.md found — searched: <bounded list>"
//	          "[gopi] no system GOPI.md found — <profile path> is absent"
//	          (and, when the profile directory itself could not be resolved,
//	          the reason is printed instead of a path)
//	unreadable (present but permissions/io): the level counts as absent AND
//	          a warning naming the path and the error follows, so a file that
//	          IS there and could not be read is never mistaken for no file.
//
// The two missing cases are DISTINCT LINES because they have distinct
// remedies — put a file in the checkout, vs. generate the profile's own — and
// the pre-change loader's single "[gopi] no GOPI.md found" is exactly the
// sentence that made a bare clone's silent rule-less run look normal.
//
// In every case the caller gets a working run: no level present returns "",
// there is no error, and the exit status is untouched.
func LoadProjectContext(start string) string {
	system := loadSystemProjectContext()
	repository := loadRepositoryProjectContext(start)
	reportSystemLevel(system)
	reportRepositoryLevel(repository)
	return mergeProjectContextNamed(system.Body, repository.Body, loadedName(system), loadedName(repository))
}

// reportSystemLevel prints exactly one outcome for the profile level. The
// loaded line's marker and two fields are the ones the historical readers
// match on ("[gopi] loaded GOPI.md (N bytes)"), with the level tag added so a
// reader can tell WHICH document it was (Rule 15: an injection says where it
// came from).
func reportSystemLevel(level projectContextLevel) {
	if level.Unresolved {
		// systemProjectContextPath already printed the reason and its remedy;
		// a second "no file" line would name an absence nobody can act on.
		return
	}
	if level.Path == "" {
		fmt.Fprintf(os.Stderr, "[gopi] no system rules file in the gopi profile (tried %s) — the profile carries no rules file\n",
			coreNameList())
		return
	}
	if level.Unreadable != nil {
		fmt.Fprintf(os.Stderr, "[gopi] no system rules file — %s could not be read\n", level.Path)
		fmt.Fprintf(os.Stderr, "[gopi] warning: system %s could not be read (%v) — continuing without it\n",
			level.Path, level.Unreadable)
		return
	}
	fmt.Fprintf(os.Stderr, "[gopi] loaded %s (%d bytes) [system] — from %s\n",
		loadedName(level), len(level.Body), level.Path)
}

// reportRepositoryLevel prints exactly one outcome for the checkout level. Its
// absent line keeps the historical marker ("no GOPI.md found — searched: …")
// with the level tag added, and the searched list stays bounded and announced
// (searchDirsLine).
func reportRepositoryLevel(level projectContextLevel) {
	if level.Path == "" {
		fmt.Fprintf(os.Stderr, "[gopi] no repository rules file (tried %s) — searched: %s\n",
			coreNameList(), searchDirsLine(level.Searched))
		return
	}
	if level.Unreadable != nil {
		fmt.Fprintf(os.Stderr, "[gopi] no repository rules file (tried %s) — searched: %s\n",
			coreNameList(), searchDirsLine(level.Searched))
		fmt.Fprintf(os.Stderr, "[gopi] warning: %s could not be read (%v) — continuing without repository context\n",
			level.Path, level.Unreadable)
		return
	}
	reportIgnoredSiblings(level)
	fmt.Fprintf(os.Stderr, "[gopi] loaded %s (%d bytes) [repository] — from %s\n",
		loadedName(level), len(level.Body), level.Path)
}

// reportIgnoredSiblings NAMES every accepted-name document that lost the
// priority order in the directory that answered. This is Rule 15's half of the
// split: two rule files in one directory is a real conflict an operator can
// create by dropping a local GOPI.md beside the tracked PROJECT.md, and the run
// must say which one it used and which one it passed over — otherwise the
// omitted document's rules vanish from the prompt with no trace.
func reportIgnoredSiblings(level projectContextLevel) {
	for _, p := range level.Ignored {
		fmt.Fprintf(os.Stderr, "[gopi] ignoring %s — %s wins (priority order: %s), so only %s was injected\n",
			p, filepath.Base(level.Path), coreNameList(), filepath.Base(level.Path))
	}
}

// coreNameList renders the accepted names in priority order for
// operator-facing lines: "PROJECT.md, then GOPI.md".
func coreNameList() string {
	return strings.Join(projectContextFileNames, ", then ")
}
