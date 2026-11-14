package chatpolicy

// category.go — the SECOND fence: the ```chat-op-category fence, which declares
// WHICH ACTION CLASS a command line belongs to.
//
// WHY A SECOND FENCE AND NOT MORE RULES IN THE FIRST ONE. The chat surface asks
// two different questions and they must never be answered from one table:
//
//	```chat-policy   : "may this line RUN?"        answer: allow | prompt | forbidden
//	```chat-op-category: "WHAT is this action?"    answer: operator-work | repo-write | long
//
// THE VACUUM THIS SEPARATION PREVENTS. If a category word were written into the
// chat-policy table, the DECIDING engine (chatBashPolicyMatch, the ONE execution
// point: policy + posture) would read it as a decision — so renaming a class, or
// adding an operation to a class, would silently widen or narrow what the console
// may RUN. That is the measured defect of the criterion this package replaced: a
// classification list that is also an enforcement table changes the policy when
// somebody merely edits a label (memory #11565). Here the category table carries
// no decision at all: its lines name a CLASS, and the class names the vocabulary.
// The two fences are parsed by the SAME parser (the same
// `<word> <words…> [*]` grammar, the same error shape, the same file-order tie
// rule) — one grammar, two vocabularies, zero shared enforcement.
//
// WHO CONSUMES IT. internal/tools' ChatOpClassify / ChatOpAdmit (the B-series
// criterion) and, through their rendered sentences, the chat prompt
// (internal/agent/chat_write_boundary.go rule (6), chat_pa_duty.go clause (1b)).
// The renderers below are the ONE place the words a model reads are produced, so
// a prompt cannot teach an operation the classifier does not know.

import (
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/gitargv"
	"github.com/nickhuang99/gopi/internal/runcmd"
	"github.com/nickhuang99/gopi/internal/taskverbs"
)

// categoryFenceInfoWord is the info string of the category fence
// (```chat-op-category). A Markdown info string may carry more after it, so the
// match is on the first whitespace-separated word, exactly as the rule parser
// does it.
const categoryFenceInfoWord = "chat-op-category"

// The class words — the spellings an operator writes on a category line AND the
// words the renderers print. Declared once (Rule 1/13): a category file, the
// parser's error message and the prompt all read these constants.
const (
	// CategoryOperatorWorkWord is work the operator would otherwise do by hand;
	// it needs no permission at any posture.
	CategoryOperatorWorkWord = "operator-work"
	// CategoryRepoWriteWord is WRITING THE REPOSITORY: what the console may do for
	// the operator under the 2026-10-24 ruling (edit, run the gate, stage, commit).
	CategoryRepoWriteWord = "repo-write"
	// CategoryRefMoveWord is the DESTRUCTIVE REF-MOVE FAMILY: what REWRITES what a
	// LOCAL branch points at — a branch force-move or force-delete (`git branch
	// -f/-D/-B/--force/--delete`), `git update-ref`, `git reset --hard`. It used to
	// be spelled `repo-write` like an ordinary source edit, and that was the
	// measured defect — the class criterion admitted every `repo-write` line
	// unconditionally, so `git branch -f` matched NO category line at all and fell
	// through to OPERATOR WORK (task 01a16bb6/01a16bfd, measured 2026-10-24: `git
	// branch -f main HEAD~3` → OPERATOR WORK, allowed). It is its own word because
	// the operator's rule is about it (PROJECT.md Rule 37) while `git add` / `git
	// commit` are not.
	//
	// WHAT LEFT THIS FAMILY, AND WHY (operator's ruling, 2026-11-03: "push 都可以";
	// row 01a1a786). Every `git push` SPELLING — bare, `--force`/-f, `--delete`,
	// `--mirror`, `--all` — is a REPOSITORY WRITE now. The measured cost of the old
	// classification was one change paid for twice: a console commit with no
	// publication path, then a worker redoing it. The entry point is the console's
	// (internal/agent/chat_pa_duty.go clause (2b)), and it is not a gate's to
	// second-guess.
	CategoryRefMoveWord = "ref-move"
	// CategoryLongWord is an action that is ITSELF long: the one class the operator
	// must be asked about.
	CategoryLongWord = "long"
)

// CategoryWildcardWord is the trailing wildcard as a category line writes it,
// exported so a renderer in another package can recognise and drop it without
// re-spelling "*" (it is the SAME `*` the rule lines use: one wildcard, one
// meaning).
const CategoryWildcardWord = wildcardWord

// categoryWords is the accepted vocabulary in DOCUMENTATION order — least to most
// restrictive, the same order a reader expects of the decision words.
var categoryWords = []string{CategoryOperatorWorkWord, CategoryRepoWriteWord, CategoryRefMoveWord, CategoryLongWord}

// CategoryRule is ONE parsed category line: the class, the command-word prefix
// that names it, and where it came from. It is a SEPARATE type from Rule on
// purpose — a value of the wrong type is a compile error, which is how "a
// category line got read as an enforcement rule" is made unexpressible rather
// than merely discouraged.
type CategoryRule struct {
	// Category is the class the line declares.
	Category string
	// Pattern is the command-word prefix, in file order; a final "*" means "any
	// remaining words".
	Pattern []string
	// Line is the 1-based line number inside the source text.
	Line int
	// Source names where the line came from.
	Source string
}

// PatternString renders the line's prefix as the operator wrote it.
func (r CategoryRule) PatternString() string { return strings.Join(r.Pattern, " ") }

// CategoryTable is a parsed ```chat-op-category fence in FILE ORDER.
//
// ORDER IS MEANING here for the same reason it is in Policy: two prefixes of equal
// length are two readings of one command, and the operator resolved that by
// writing one above the other.
type CategoryTable struct {
	// Rules is the table, in the order the lines were written.
	Rules []CategoryRule
	// Source names the text the table came from.
	Source string
	// Fenced records whether the source actually carried a category fence.
	Fenced bool
}

// Len reports how many category lines the table holds.
func (t *CategoryTable) Len() int {
	if t == nil {
		return 0
	}
	return len(t.Rules)
}

// WordsOf returns the prefixes declared for one class, in file order — the list a
// renderer prints and a same-origin test walks.
func (t *CategoryTable) WordsOf(category string) []string {
	words := make([]string, 0, t.Len())
	if t == nil {
		return words
	}
	for _, r := range t.Rules {
		if r.Category == category {
			words = append(words, r.PatternString())
		}
	}
	return words
}

// ParseCategories reads a project-context document (GOPI.md) and returns the
// category table its ```chat-op-category fence(s) declare.
//
// The acceptance rules are Parse's, deliberately: only text inside the fence is
// data, no fence is a VALID empty table, several fences concatenate in file order,
// blank lines and `#` comments are skipped, and every malformed line is a
// *ParseError naming its line (Rule 15 — a line the operator wrote must either
// take effect or be reported).
func ParseCategories(source, text string) (*CategoryTable, error) {
	tab := &CategoryTable{Source: source}
	var (
		inFence  bool
		fenceLen int
	)
	for i, raw := range strings.Split(text, "\n") {
		lineNo := i + 1
		trimmed := strings.TrimSpace(raw)
		if !inFence {
			if open, ok := fenceOpenAs(trimmed, categoryFenceInfoWord); ok {
				inFence = true
				fenceLen = open
				tab.Fenced = true
			}
			continue
		}
		if closeFence(trimmed, fenceLen) {
			inFence = false
			fenceLen = 0
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, commentPrefix) {
			continue
		}
		rule, err := parseCategoryLine(source, lineNo, trimmed)
		if err != nil {
			return nil, err
		}
		tab.Rules = append(tab.Rules, rule)
	}
	if inFence {
		return nil, &ParseError{
			Source: source,
			Line:   strings.Count(text, "\n") + 1,
			Msg: "```" + categoryFenceInfoWord + " fence is never closed — add a line containing only " +
				fenceOpenPrefix,
		}
	}
	return tab, nil
}

// parseCategoryLine turns one non-blank, non-comment fence line into a
// CategoryRule: `<class> <word> <word> … [*]`.
func parseCategoryLine(source string, lineNo int, trimmed string) (CategoryRule, error) {
	fields := strings.Fields(trimmed)
	category, ok := categoryNamed(fields[0])
	if !ok {
		return CategoryRule{}, &ParseError{
			Source: source,
			Line:   lineNo,
			Msg:    fmt.Sprintf("unknown action class %q — a category line starts with one of %s", fields[0], strings.Join(categoryWords, "|")),
		}
	}
	pattern := fields[1:]
	if len(pattern) == 0 {
		return CategoryRule{}, &ParseError{
			Source: source,
			Line:   lineNo,
			Msg:    fmt.Sprintf("%s line names no command words — write the prefix it classifies, e.g. %q", category, category+" some command"),
		}
	}
	if err := checkWildcard(pattern); err != nil {
		return CategoryRule{}, &ParseError{Source: source, Line: lineNo, Msg: err.Error()}
	}
	return CategoryRule{Category: category, Pattern: pattern, Line: lineNo, Source: source}, nil
}

// categoryNamed reports whether s spells one of the classes (case-SENSITIVE, the
// same reading as decisionNamed: "Long" is a typo the parser names with its line,
// never a synonym).
func categoryNamed(s string) (string, bool) {
	for _, w := range categoryWords {
		if s == w {
			return w, true
		}
	}
	return "", false
}

// categoryMatchLength is the category table's prefix match: an exact WORD match,
// except that a pattern word ENDING IN "/" matches a word that starts with it.
//
// WHY THE SECOND RULE EXISTS, and why it is confined to this table. The words the
// operator writes for a source tree are `internal/`, `cmd/`, `web/` — "this whole
// tree" — while a command line's word is the full path (`internal/agent/x.go`).
// Without a directory prefix, `> internal/agent/x.go` could only be named by
// listing every file, or by a rule so broad it also covered the session directory
// the write boundary MANDATES. The exemption is confined here on purpose: the
// ENFORCEMENT table (DecideWords) keeps its exact-word reading untouched, so this
// rule can never widen or narrow what the console may run.
func categoryMatchLength(pattern, words []string) (int, bool) {
	max := len(pattern)
	if max > 0 && pattern[max-1] == wildcardWord {
		max--
	}
	if max == 0 {
		return 0, false
	}
	// A WRITE REDIRECTION IS NOT A COMMAND VERB, so its line is not anchored at
	// word 0: `echo x > internal/y.go` is a repository write whose subject stands
	// third. The table's other lines ARE anchored (a command line starts with its
	// command), and this is the one shape that needs the wider reading — so it is
	// asked for explicitly rather than by turning every rule into a search.
	if redirectionIsOperator(pattern[0]) {
		words = splitRedirectionWords(words)
		for start := 0; start+max <= len(words); start++ {
			if categoryWordsMatchAt(pattern, words, start, max) {
				return max, true
			}
		}
		return 0, false
	}
	if len(words) < max {
		return 0, false
	}
	if !categoryWordsMatchAt(pattern, words, 0, max) {
		return 0, false
	}
	return max, true
}

// categoryWordsMatchAt reports whether pattern[0:max] matches words[start:start+max].
func categoryWordsMatchAt(pattern, words []string, start, max int) bool {
	for i := 0; i < max; i++ {
		if !categoryWordMatches(pattern[i], words[start+i]) {
			return false
		}
	}
	return true
}

// redirectionIsOperator reports whether a pattern word is one of the shell's
// write redirections — the shape whose line is not anchored at the command verb.
func redirectionIsOperator(word string) bool {
	for _, r := range redirectionPrefixes {
		if word == r {
			return true
		}
	}
	return false
}

// categoryWordMatches is one pattern word against one command word: exact, or a
// directory prefix when the pattern word ends in "/".
//
// THE OPERATOR'S PEOPLE WRITE THE SHELL'S OWN FORM. A redirection is a WORD in
// this tokenizer only when it stands alone (`echo x > internal/y.go`), but
// `>>` is written flush against its target far more often
// (`echo x >> internal/y.go` is the idiom), where the token is `>>internal/y.go`.
// So the prefix reading also applies to a word that STARTS WITH A REDIRECTION:
// the pattern's directory word must then follow it. Without that, the source-tree
// write — the one class this table exists to catch — would be caught only in the
// spelling nobody types.
func categoryWordMatches(pattern, word string) bool {
	if pattern == word {
		return true
	}
	// A shell's two write operators are ONE act: appending to a file writes it
	// exactly as truncating it does, so a table that names `>` must not be
	// defeated by the operator typing `>>` (the idiom for a log or a note).
	if redirectionIsOperator(pattern) && redirectionIsOperator(word) {
		return true
	}
	if strings.HasSuffix(pattern, "/") && len(word) > len(pattern) {
		if strings.HasPrefix(word, pattern) {
			return true
		}
		for _, r := range redirectionPrefixes {
			if strings.HasPrefix(word, r) && strings.HasPrefix(word[len(r):], pattern) {
				return true
			}
		}
	}
	return false
}

// splitRedirectionWords splits a redirection written FLUSH against its target
// (`>>internal/y.go`) into the operator and the target, so a table line naming
// them as two words reads the same command however the operator spaced it. Words
// that do not begin with a write operator are returned unchanged, and the input
// slice is never modified.
func splitRedirectionWords(words []string) []string {
	out := make([]string, 0, len(words)+1)
	for _, w := range words {
		split := false
		for _, r := range redirectionPrefixes {
			if len(w) > len(r) && strings.HasPrefix(w, r) {
				out = append(out, r, w[len(r):])
				split = true
				break
			}
		}
		if !split {
			out = append(out, w)
		}
	}
	return out
}

// redirectionPrefixes are the shell's write operators as they can be written
// flush against a target word. Declared once so the matcher above and any future
// reader share one spelling.
var redirectionPrefixes = []string{">>", ">"}

// chooseCategory is Decide's category twin: the LONGEST matching prefix wins and
// an equal length is decided by FILE ORDER (first written wins) — one matching
// rule, shared by both tables, so the two fences cannot drift apart in how they
// read a line.
func (t *CategoryTable) chooseCategory(words []string) (CategoryRule, bool) {
	if t == nil {
		return CategoryRule{}, false
	}
	best := -1
	bestLen := -1
	for i := range t.Rules {
		spec, ok := categoryMatchLength(t.Rules[i].Pattern, words)
		if !ok {
			continue
		}
		if spec > bestLen {
			best, bestLen = i, spec
		}
	}
	if best < 0 {
		return CategoryRule{}, false
	}
	return t.Rules[best], true
}

// MatchCategory answers ONE clause's words: the class the table declares, and
// whether any line applied at all.
//
// MATCHED IS FALSE WHEN NOTHING APPLIES, and the caller must read it before
// Category: no class is not operator-work by default HERE — the default belongs to
// the consumer (internal/tools' ChatOpClassify makes it explicit and says why). A
// package that silently answered "the permissive class" for an unexplained line
// would hide the one case an operator needs to see.
//
// THE ROWS NAME WORDS, AND GIT'S OWN GLOBAL OPTIONS STAND BETWEEN THEM. Every
// ref-move row is `<class> git <subcommand> …`, so a line that passes one of
// git's globals before the subcommand (`git -C <dir> push --force`,
// `git --git-dir=<p> update-ref -d <ref>`, `git -c k=v reset --hard`) matched NO
// row at all and fell through to the consumer's default — OPERATOR WORK, the class
// that RUNS. One inserted word therefore bypassed the entire fence that Rule 41
// (2b) keeps for the one act a console may not decide (measured on row 01a19d6e,
// 2026-11-03: all eight spellings read OPERATOR WORK / allowed=true).
//
// SO THE MATCH IS MADE ON THE INVOCATION, NOT ON THE TYPED WORD ORDER: the clause
// is read through gitargv.SubcommandPath — the ONE reader of git's global-option
// vocabulary, shared with internal/tools' ref guard rather than restated here
// (Rule 13/14) — and the table is matched against `git <subcommand> <rest>`. A
// clause that is not a git invocation, and a git invocation with no readable
// subcommand, are matched on their words AS TYPED, so a line the normaliser
// cannot resolve can only keep the class it already had.
//
// THE TYPED READING IS KEPT AS THE FALLBACK, in that order: the normalised
// reading decides when it applies, and the typed one is consulted only when it
// does not. What this changes is ONE direction only — a line can be RAISED into
// the class its subcommand names, never lowered out of it, because the demotions
// that may lower a class (a dry run, a clone ground's own delivery, a
// fast-forward publish) are asked about the ORIGINAL words by their own caller
// and never see this normalised slice.
func (t *CategoryTable) MatchCategory(words []string) (CategoryRule, bool) {
	if path := gitargv.SubcommandPath(words); path != nil {
		if rule, ok := t.chooseCategory(path); ok {
			return rule, true
		}
	}
	return t.chooseCategory(words)
}

// Built-in category table — the SHIPPED fallback, used when the project declares
// no table of its own (neither the TRACKED `.gopi/chat-policy.md` nor a
// ```chat-op-category fence in `GOPI.md`).
//
// IT NAMES NO PATH, and that is the one property a compiled-in table can honestly
// have. The table used to carry the operator's own operation families verbatim —
// fourteen rows of them naming `internal/`, `cmd/` and `web/`, the trees THIS
// repository is built out of. Moved from Go slices into data they were correct
// for one project and wrong for every other: a console opened in a checkout whose
// source lives in `src/`, `app/` or `lib/` matched not one of those rows, so
// `sed -i` on its own source was classified as operator work and RUN, and the
// gate run over the console's own edit was classified as operator work too. A
// default that names a path is a default that guesses which project it is in.
//
// SO WHAT IS LEFT HERE is the part that is true of the REPOSITORY as such rather
// than of one repository's layout: the GIT verbs whose effect is a repository
// write or a ref move (a stage, a commit, a checkout, a history rewrite — the
// same acts in every checkout, whatever the trees are called), the toolchain
// verbs that rewrite files wherever the files are (`sed -i`, `gofmt -w`,
// `goimports -w`), and the one long class, whose rows name operations that take
// the operator's minutes rather than paths.
//
// WHERE THE PATHS WENT: to the tracked project file, `.gopi/chat-policy.md`,
// which travels with the clone precisely so that "which trees are this project's
// source" is a fact stated BY the project. Its rows REPLACE this table — they do
// not extend it — so a repository that declares its own trees is judged by them
// alone, and a word shipped here can never reach into a project that did not ask
// for it.
//
// WHAT MUST NEVER BE ADDED HERE: a `forbidden` decision — this fence carries no
// decisions at all — and no PATH, no directory prefix, no tree name. The two
// halves are pinned by TestBuiltInCategoryTableNamesNoPath (a path in this
// constant fails the suite) and by the three-state tests that drive the project
// file over it.
var defaultCategoryText = fenceOpenPrefix + categoryFenceInfoWord + "\n" +
	"# The BUILT-IN fallback: no path, no tree name — a compiled-in table cannot\n" +
	"# know what a project calls its own source. A project that wants its trees\n" +
	"# guarded declares them in .gopi/chat-policy.md, whose rows REPLACE these.\n" +
	"#\n" +
	"# operator-work is the DEFAULT class and names no word: every command line\n" +
	"# no row below mentions is work the operator would do by hand.\n" +
	"repo-write sed -i *\n" +
	"repo-write gofmt -w *\n" +
	"repo-write goimports -w *\n" +
	"repo-write git add *\n" +
	"repo-write git commit *\n" +
	// THE REF-MOVE FAMILY IS THE MATCHED PATTERN, NEVER A COMMAND ANYONE IS TOLD
	// TO RUN. classify() tokenises an ARRIVING command line and compares it
	// against this table, so the subject of every row below is the operator's OWN
	// input: a row names a shape in order to RECOGNISE — and route — it.
	//
	// branch-force-ok: deleting the `-f`/`--force` rows would not remove an
	// instruction; it would remove the rule that ROUTES the form, re-opening the
	// hole measured 2026-10-24 when `git branch -f main HEAD~3` matched NO row
	// here and fell through to OPERATOR WORK — the class that RUNS (tasks
	// 01a16bb6/01a16bfd). removal-audit-ok: same subject one row down — `-D` and
	// `--delete` classify an arriving deletion, and nothing on these lines invokes
	// git, so the audit record belongs to the command line the console then
	// REFUSES or ASKS about. The behaviour these rows exist for is pinned by
	// TestRefMoveFamilyIsItsOwnClassAndIsRefused (internal/tools), not by prose.
	//
	// Each row carries its OWN trailing marker because the gates' linesAbove
	// window is the offending line plus the contiguous comment block directly
	// above it, and the row between two of them is not a comment.
	"ref-move git branch -f *\n" + // branch-force-ok: this row RECOGNISES an arriving command line; no child is told to run it
	"ref-move git branch -D *\n" + // removal-audit-ok: this row only CLASSIFIES an arriving deletion; nothing here invokes git
	"ref-move git branch -B *\n" +
	"ref-move git branch --force *\n" + // branch-force-ok: this row RECOGNISES an arriving command line; no child is told to run it
	"ref-move git branch --delete *\n" + // removal-audit-ok: this row only CLASSIFIES an arriving deletion; nothing here invokes git
	"ref-move git update-ref *\n" +
	// EVERY `git push` SPELLING IS A REPOSITORY WRITE (operator's ruling,
	// 2026-11-03: "push 都可以", no authorisation at any posture). The whole
	// push family used to sit in ref-move, so publishing a commit the console
	// had just made was refused forever while `git commit` itself ran — the
	// measured cost was one change paid for twice (a console commit with no
	// publication path, then a worker redoing it). The operator owns every ref
	// this machine can reach and said so; the entry point is the console's, not
	// a gate's to second-guess. The DESTRUCTIVE REF-MOVE FAMILY that remains is
	// the one that rewrites what a LOCAL branch points at (`git branch -f/-D/-B`,
	// `git update-ref`, `git reset --hard`) — those stay refused.
	"repo-write git push *\n" +
	"repo-write " + stashWordPlaceholder + " *\n" +
	"repo-write git checkout *\n" +
	"ref-move git reset --hard *\n" +
	"repo-write git reset --soft *\n" +
	"repo-write git reset --mixed *\n" +
	"repo-write git reset\n" +
	"repo-write git rebase *\n" +
	"repo-write git cherry-pick *\n" +
	"repo-write git merge *\n" +
	"repo-write git switch *\n" +
	"repo-write git restore *\n" +
	"repo-write make test-budget *\n" +
	"repo-write golangci-lint run *\n" +
	"repo-write go test *\n" +
	"repo-write go build *\n" +
	"repo-write go vet *\n" +
	"long go build ./... *\n" +
	"long go test ./... *\n" +
	"long make test *\n" +
	"long make build *\n" +
	"long make check *\n" +
	"long npm install *\n" +
	"long npm ci *\n" +
	"long npm run build *\n" +
	"long yarn install *\n" +
	"long pnpm install *\n" +
	"long pip install *\n" +
	"long go get *\n" +
	"long go mod download *\n" +
	"long apt-get install *\n" +
	"long apt install *\n" +
	"long " + runcmd.BinaryName + " " + cliverbs.Worker + " *\n" +
	"long " + runcmd.BinaryName + " " + cliverbs.Task + " " + taskverbs.Merge + " *\n" +
	"long " + runcmd.BinaryName + " " + cliverbs.Task + " " + taskverbs.Sync + " *\n" +
	"long " + runcmd.BinaryName + " " + cliverbs.Task + " " + taskverbs.Push + " *\n" +
	"long " + runcmd.BinaryName + " " + cliverbs.Tool + " " + cliverbs.ScratchGC + " *\n" +
	fenceOpenPrefix + "\n"

// stashWordPlaceholder is the stash subcommand word, spelled once HERE from the
// two halves the shared-stash guard already names (gitBinaryName + stashCommandWord)
// so this package never types the literal — the same one-spelling discipline
// internal/tools' shared_stash_guard_literal_gate_test.go enforces inside that
// package.
const stashWordPlaceholder = gitVerbWord + " " + stashVerbWord

// The two halves of the stash invocation, as WORDS. They are spelled out here
// (rather than imported from internal/tools, which would be a cycle: tools
// consumes this package) and the category test asserts that the composed
// invocation classifies as repo-write — so the one-spelling property is pinned by
// behaviour, not by prose.
const (
	gitVerbWord   = "git"
	stashVerbWord = "stash"
)

// DefaultCategorySource names the built-in category table in a rule's Source, the
// same convention defaultRulesLabel uses for the decision table.
const DefaultCategorySource = "built-in chat-op-category defaults"

// DefaultCategories returns the built-in category table, parsed by the SAME
// parser that reads an operator's fence. The error can only be a programming
// error in the constant above; it is returned rather than panicked so a caller can
// print it and fall back.
func DefaultCategories() (*CategoryTable, error) {
	return ParseCategories(DefaultCategorySource, DefaultCategoryText())
}

// DefaultCategoryText returns the built-in table's TEXT, exactly as parsed — so a
// documentation page or a startup banner prints the same bytes the engine reads
// (Rule 2: one definition, rendered, never re-typed).
func DefaultCategoryText() string { return defaultCategoryText }

// CategoryFenceInfoWord is the ```chat-op-category info word, exported so a
// caller that REPORTS on the fence (the startup line, a refusal that tells an
// operator where to write the table) spells it from here rather than typing it
// (Rule 1b/13/14 — one definition, referenced).
func CategoryFenceInfoWord() string { return categoryFenceInfoWord }
