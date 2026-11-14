package agent

// chat_policy_category_file.go — WHERE the action-class table comes from, in
// priority order, now that a project can carry its own.
//
// THE DEFECT THIS CLOSES (measured, task 01a196bf). The ```chat-op-category table
// lived in the BINARY (internal/chatpolicy/category.go), and every one of its rows
// named a tree of THIS repository: `> internal/`, `>> cmd/`, `> web/`. A table
// compiled into a binary describes the repository that built it, so a console
// opened in any other project — source under `src/`, `app/` or `lib/` — matched
// NOT ONE row: `sed -i` on that project's own source classified as OPERATOR WORK
// and ran, and the gate run over the console's own edit classified as operator
// work too. The vocabulary was correct and the POPULATION was wrong.
//
// SO THE TABLE MOVES OUT, and the file it moves to is TRACKED (`.gopi/chat-policy.md`
// at the repository root): a table that belongs to a project must be the same in
// every clone of it, which is exactly the property `GOPI.md` does not have (that
// file is the CLIENT's and is gitignored by design — internal/standards'
// scratch_gate.go states why). The tracked file is therefore READ FIRST, the
// local fence second, the binary last.
//
// THE THREE STATES, and each one is a different ANSWER (Rule 15 — a line the
// operator wrote must either take effect or be reported, never dropped):
//
//	absent      → the next channel answers (GOPI.md's fence, then the built-in
//	              table). A project with no policy file is a NORMAL project.
//	malformed   → the error is RETURNED and names `file:line: what is wrong`, so
//	              the startup line can point at the line instead of judging by a
//	              half-read table. The caller reports it and falls back to the
//	              built-in table for that run.
//	well-formed → it REPLACES the built-in table COMPLETELY. There is no merge,
//	              and that is deliberate: a merge would let a word gopi happens to
//	              ship (`> cmd/`) reach into a project that never declared it, so
//	              a user's own `cmd/` would be refused as a repository write by a
//	              rule about somebody else's repository.
//
// ONE PARSER. The file is read by chatpolicy.ParseCategories — the SAME function
// that reads the GOPI.md fence and the built-in table — so the three channels
// cannot disagree about the grammar, the error shape or the tie rule (Rule 2).

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nickhuang99/gopi/internal/chatpolicy"
	"github.com/nickhuang99/gopi/internal/config"
)

// ChatPolicyProjectFileName is the tracked project policy file's name, and
// ChatPolicyProjectDirName the directory it lives in at the repository root.
//
// The directory is a PROJECT-level `.gopi/` (not the profile: the profile is
// <home>/.gopi, a different thing with a different owner). It is spelled from
// config.ProfileDirName because both are "gopi's directory" and a second literal
// would be the mirror defect Rules 13/14 name — the file NAME is this package's
// own value, and CONFIG.md reads it here.
const (
	ChatPolicyProjectDirName  = config.ProfileDirName
	ChatPolicyProjectFileName = "chat-policy.md"
)

// ChatPolicyProjectRelPath is the file's path relative to the repository root —
// what a diagnostic prints, so a reader is told exactly where to put the file.
const ChatPolicyProjectRelPath = ChatPolicyProjectDirName + "/" + ChatPolicyProjectFileName

// ChatPolicyProjectPath answers the ABSOLUTE path of the project policy file that
// governs start, and whether a repository was found at all.
//
// The walk ascends from start to the TOP of the checkout — the directory holding
// the `.git` entry (a DIRECTORY in a clone, a FILE in a linked worktree; the same
// predicate projectctx.go's own walk uses, so the two resolvers agree about where
// a project ends). A start outside any repository has no project file by
// definition, and that is reported as found=false rather than as an error.
func ChatPolicyProjectPath(start string) (path string, found bool) {
	root, ok := projectRootOf(start)
	if !ok {
		return "", false
	}
	return filepath.Join(root, ChatPolicyProjectDirName, ChatPolicyProjectFileName), true
}

// projectRootOf walks up from start to the checkout top. It mirrors the
// project-context walk's boundary rule (outermost repository wins, so a nested
// fixture cannot answer for the enclosing project) without importing it: that
// walk returns a FILE, and this one returns the DIRECTORY a second file hangs
// off.
func projectRootOf(start string) (string, bool) {
	dir := start
	if dir == "" {
		dir, _ = os.Getwd() //stylegate:ignore an unreachable cwd leaves dir empty, answered as "no repository" below (the file is optional)
	}
	if dir == "" {
		return "", false
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	for hop := 0; hop <= projectContextMaxHops; hop++ {
		if dir == "" {
			break
		}
		if isRepoTop(dir) && !isRepoTop(filepath.Dir(dir)) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir { // the filesystem root
			break
		}
		dir = parent
	}
	return "", false
}

// LoadProjectCategoryTable reads the TRACKED project policy file, if this
// checkout has one, and returns the table it declares.
//
// The three states are the return values, and NOTHING is guessed between them:
//
//	(nil, "", nil)          — no repository, or no .gopi/chat-policy.md in it.
//	                          The caller moves to the next channel.
//	(nil, path, err)        — the file exists and does not parse. err already
//	                          renders `path:line: what is wrong` (chatpolicy's
//	                          ParseError). The caller REPORTS it and falls back.
//	(tab, path, nil)        — the file exists and parses. tab REPLACES the
//	                          built-in table; the caller must not merge.
//
// A readable file that declares no ```chat-op-category fence is state three with
// an EMPTY table, and that is the parser's own documented rule ("no fence is a
// VALID empty table"): a project that puts the file in place and writes no rows
// has declared that nothing is a repository write, which is a decision an
// operator is allowed to make and is visible in the table's own length.
func LoadProjectCategoryTable(start string) (*chatpolicy.CategoryTable, string, error) {
	path, found := ChatPolicyProjectPath(start)
	if !found {
		return nil, "", nil
	}
	info, statErr := os.Stat(path)
	if statErr != nil || info.IsDir() {
		// No file here is the ABSENT state, not a failure: the project simply has
		// not declared a table. A stat error other than "does not exist" is also
		// answered as absent — the file is optional, and the next channel still
		// answers (Rule 15: the alternative is refusing to start a console over a
		// permission bit on an optional file).
		return nil, "", nil
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return nil, path, fmt.Errorf("%s: %w", path, readErr)
	}
	tab, perr := chatpolicy.ParseCategories(path, string(data))
	if perr != nil {
		return nil, path, perr
	}
	return tab, path, nil
}
