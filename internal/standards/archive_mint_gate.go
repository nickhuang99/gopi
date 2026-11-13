package standardgate

import (
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/tools"
)

// archive_mint_gate.go — nothing outside gopi's one writer may MINT an archive
// tag. Prose context, the measured incident and the full argument:
// devdoc/archive-mint-outside-writer.md.
//
// WHY THIS GATE EXISTS. On 2026-10-12 09:45:09 ONE archive tag was minted with a
// reason record (`archive/task/01a12d22dac07bdea9f895926cdc64ea` — the exact shape
// `ArchiveTagReasoned` writes). ONE MINUTE LATER, at 09:46:14, TEN more appeared
// carrying a `-20261012` suffix appended to the branch name — a shape no Go writer
// in this project produces. Their loose-ref mtimes span 0.12s (one shell loop) and
// none has a companion reason record.
//
// THE SUFFIX IS THE TELL, AND IT IS PRESCRIBED. `-<date>` is PROJECT.md Rule 9 step
// 2's own spelling, so the ten were minted by an agent FOLLOWING GOPI.md — and the
// same bare mint was rendered by two Go string literals (a pushed-branch hint and
// a branch-delete refusal) and by scripts/post-merge-cleanup.sh. That is why the
// fix is a FUNNEL ON THE INSTRUCTIONS rather than a patch on one script: the reachable
// path is whatever teaches the shape, and gopi teaches it in four places.
//
// WHAT THE SCRIPT WAS NOT: the suspect the epic named, and the measurement cleared
// it. post-merge-cleanup.sh mints only when `AHEAD > 0`, and `AHEAD` is exactly
// `git rev-list --count master..<branch>` — while the script only ever cleans
// branches ALREADY merged into master. Every branch it touches therefore has
// AHEAD=0 and that arm is DEAD; a previous amendment to it did nothing (see
// devdoc/archive-mint-outside-writer.md §3 for the two commands that settle it).
// It was a coincidental CARRIER of Rule 9's naming convention, not the writer.
//
// SCOPE — deliberately narrow, and the narrowness is the design:
//
//   - four carriers: GOPI.md, scripts/, and the PRODUCTION Go trees internal/ +
//     cmd/ (test files excluded — a fixture exists to construct the shape).
//   - devdoc/, tasks/ and briefings/ are OUT. They are HISTORY and must stay able
//     to QUOTE the pre-fix command; a record that cannot print its own subject is
//     not a record (the branch-force gate's rationale).
//   - LIST/INSPECT/DELETE forms are not mints. `git tag -l 'archive/*'`,
//     `git tag --contains <tip>` and a bare `git tag` (which PRINTS the list) are
//     readers — safe-merge.sh, d2-disposition.sh and gopi-stats.sh all read the
//     namespace legitimately.
//   - a `#`/`//` comment is prose, never a mint (`mention is not prescription`).
//
// WHAT THIS GATE DOES NOT COVER (Rule 15: stated, not implied): a shape inside a
// multi-line `/* … */` block is reported (a textual scan cannot tell it from code),
// a string literal SPLIT across lines is not seen, and an `eval`-assembled command
// is invisible. No text scan can catch every spelling of a shell command; the
// escape hatch keeps a reviewed exception visible instead of hidden.
func archiveMintGate() gate {
	return gate{
		id:             "archive-why (a shipped instruction must not mint an archive tag outside gopi's one writer)",
		checklistRow:   "archive/mint-outside-writer",
		probe:          "TestArchiveMintGateRedProof",
		scanTextRoot:   textScanRoot,
		escape:         archiveMintEscape,
		baselineReason: "EMPTY by construction: minting an archive tag with no reason is never an acceptable state, so there is nothing to permit. The three measured carriers were FUNNELLED through the one writer rather than permitted — GOPI.md Rule 9, the pushed-branch visibility hint and the branch-delete refusal — and scripts/post-merge-cleanup.sh was funnelled too even though its mint arm is provably dead.",
		baseline:       archiveMintBaseline,
		find:           archiveMintFind,
	}
}

// archiveMintBaseline is empty BY CONSTRUCTION. A baseline entry here would be a
// permanent exemption for a silent mint, which is the defect this gate exists to
// stop — the fix for a hit is the funnel (`gopi task archive`), never a permit.
var archiveMintBaseline []string

// archiveMintEscape marks a line whose SUBJECT is the mint — a fixture, or a file
// quoting the pre-fix shape to explain what it replaced (Rule 25). Spelled in one
// place so the gate, its red proof and its report share it (Rule 14).
const archiveMintEscape = "archive-mint-ok:"

// The shell vocabulary the predicate reads. These are tokens, not prose: they are
// the difference between a reader and a writer of the ref namespace.
const (
	archiveMintGitToken      = "git"
	archiveMintTagToken      = "tag"
	archiveMintCommentPrefix = "#"
	archiveMintFlagPrefix    = "-"
)

// archiveMintScanRoot is the tree holding the HAND-RUN carrier: a shell script
// that mints. It is a named constant (Rule 1) because the scope filter and the
// non-vacuity floor must agree about it.
const archiveMintScanRoot = "scripts"

// The gate's SCOPE, spelled as named prefixes (Rule 1). The walk is the module
// tree (textScanRoot) because the carriers live in three different trees, and the
// framework selects a file set by exactly one of scanRoots/scanTextRoot — so a
// mixed-carrier gate filters inside its own predicate instead of widening the
// framework.
const (
	archiveMintGoRoot          = "internal/"
	archiveMintCmdRoot         = "cmd/"
	archiveMintScriptsRoot     = archiveMintScanRoot + "/"
	archiveMintInstructionRel  = ProjectDocName
	archiveMintTestFileSuffix  = "_test.go"
	archiveMintGoSuffix        = ".go"
	archiveMintGoCommentPrefix = "//"
)

// archiveMintMinScripts is the non-vacuity FLOOR for the hand-run carrier: a
// broken root or a filtered walk would otherwise report a clean green while
// reading nothing (the failure mode branchForceMinGoFiles names). Set well below
// the measured tree so ordinary churn cannot trip it.
const archiveMintMinScripts = 8

// archiveMintListFlags turn `git tag` into a READER: they make git list, delete,
// verify or query tags rather than create one. A line whose token after `tag` is
// one of these is not a mint — that is what keeps the namespace's legitimate
// readers out of this gate.
var archiveMintListFlags = []string{
	"-l", "--list",
	"-d", "--delete",
	"-v", "--verify",
	"--contains", "--points-at", "--merged",
}

// archiveMintShellOperators are the pipeline/sequencing tokens that END a command
// rather than extend it. They exist so "the token after `tag` is not a flag" can
// never be read as "the token after `tag` is a tag name": on gopi-stats.sh's
// `$(git tag | cnt)` census the next token is `|`, and that line PRINTS the list.
var archiveMintShellOperators = []string{"|", "&", ";", ">", "<", ")"}

// archiveMintDesc is the one sentence every hit carries. The remedy names the verb,
// and the verb is the funnel: `gopi task archive` records the WHY in the companion
// reason ref beside the tag. The companion ref's path is deliberately NOT spelled
// here — the writer owns that spelling and the verb's own help prints it, so a
// second copy in a gate could only drift (Rule 13/14).
var archiveMintDesc = "mints an archive tag with `git tag` (create form) instead of going through the ONE writer — " +
	"a tag is a DECISION with a WHY. Mint it through `gopi task archive <branch> --reason <code>` (run `gopi task archive -h` " +
	"for the vocabulary), which writes " + tools.ArchiveBranchPrefix + "<branch> AND its companion reason ref. " +
	"The measured case is the 2026-10-12 batch of ten archive tags with no reason record."

// archiveMintFind reports the create-form tag invocations in one in-scope file.
//
// THE PREDICATE IS LINE-BASED FOR EVERY CARRIER, and that is a MEASURED choice
// rather than a simplification. The first version read Go files through the AST
// (literals only, comments excluded — the branch-force gate's split) and was
// therefore DEAD in the real scan: a text gate's traversal builds `parsed{rel,
// src}` with NO file set, so an ast.Inspect over a nil file returns nothing. The
// unit test passed while the production scan reported zero Go hits — a gate
// correct in isolation and unreachable in production. Every carrier is now read as
// TEXT, and the Go half is approximated the way a text scan can: a `//` line
// comment is stripped before the predicate runs, which keeps this tree's many
// incident comments out of the report.
func archiveMintFind(g gate, p *parsed) []hit {
	if !archiveMintInScope(p.rel) {
		return nil // history and unrelated trees may quote the pre-fix shape
	}
	isGo := strings.HasSuffix(p.rel, archiveMintGoSuffix)
	var out []hit
	for i, line := range strings.Split(string(p.src), "\n") {
		if isGo {
			line = archiveMintStripLineComment(line)
		}
		if !archiveMintLineCreatesTag(line) {
			continue
		}
		out = append(out, hit{rel: p.rel, line: i + 1, desc: archiveMintDesc})
	}
	return out
}

// archiveMintInScope reports whether a repo-relative path is one of this gate's
// carriers: the agent-facing rule file, the hand-run scripts tree, or the
// PRODUCTION Go trees. Everything else — devdoc/, tasks/, briefings/, a Go test
// fixture — is out, and the exclusion is a decision rather than an oversight:
// history must be able to print the command it records, and a fixture exists to
// construct the shape.
func archiveMintInScope(rel string) bool {
	slashed := filepath.ToSlash(rel)
	if slashed == archiveMintInstructionRel {
		return true
	}
	if strings.HasPrefix(slashed, archiveMintScriptsRoot) {
		return true
	}
	if !strings.HasPrefix(slashed, archiveMintGoRoot) && !strings.HasPrefix(slashed, archiveMintCmdRoot) {
		return false
	}
	return !strings.HasSuffix(slashed, archiveMintTestFileSuffix)
}

// archiveMintStripLineComment removes a Go line comment, so a comment that
// DOCUMENTS the pre-fix command is not reported as one that ISSUES it. It is the
// text-scan equivalent of reading only string literals.
func archiveMintStripLineComment(line string) string {
	if i := strings.Index(line, archiveMintGoCommentPrefix); i >= 0 {
		return line[:i]
	}
	return line
}

// archiveMintLineCreatesTag reports whether one line carries a CREATE-form
// `git tag` invocation.
//
// The decision is made on the ONE token immediately after `tag`, and that
// narrowness is measured rather than convenient: this repo's readers and its
// writers differ there and nowhere else.
//
//   - a list/delete/verify flag (archiveMintListFlags) → a READER;
//   - a shell operator (`|`, `&&`, `;`) or nothing at all → a bare `git tag`,
//     which PRINTS the list — gopi-stats.sh's `$(git tag | cnt)` census is exactly
//     this shape and must never be reported;
//   - anything else — a create flag (`-f`, `-a`, `-m`, `-s`) or the tag NAME
//     itself (possibly quoted, possibly a substitution) → a MINT.
//
// Scanning every token after `tag` instead would have reported `git tag | cnt`
// (the tokens are `|` and `cnt`, and `cnt` is not a flag) — measured, not
// hypothesised: the first version of this predicate did exactly that, and its own
// red proof caught it. A gate that fires on a census line is a gate that gets
// switched off.
func archiveMintLineCreatesTag(line string) bool {
	if strings.HasPrefix(strings.TrimSpace(line), archiveMintCommentPrefix) {
		return false
	}
	fields := strings.Fields(line)
	idx := archiveMintTagIndex(fields)
	if idx < 0 || idx+1 >= len(fields) {
		return false
	}
	next := fields[idx+1]
	if archiveMintShellOperator(next) {
		return false // a bare `git tag` in a pipeline: it prints the list
	}
	if !strings.HasPrefix(next, archiveMintFlagPrefix) {
		return true // the tag NAME — a create form
	}
	for _, lf := range archiveMintListFlags {
		if next == lf {
			return false // a reader
		}
	}
	return true // a create flag (`-f`, `-a`, `-m`, `-s`, …)
}

// archiveMintShellOperator reports whether a field is a shell operator rather than
// an operand: the token that follows a bare `git tag` when the line pipes or
// short-circuits its output.
func archiveMintShellOperator(f string) bool {
	for _, op := range archiveMintShellOperators {
		if f == op || strings.HasPrefix(f, op) {
			return true
		}
	}
	return false
}

// archiveMintTagIndex returns the index of the `tag` token in a `git tag`
// invocation, or -1 for a line that has none.
//
// It looks for an ADJACENT `git`+`tag` pair ANYWHERE on the line rather than at its
// head, because a shell mints in a command substitution as readily as in a
// statement (`x=$(git tag -f …)`) and a Go literal renders the pair mid-sentence —
// anchoring at the line head would leave both unreported.
func archiveMintTagIndex(fields []string) int {
	for i := 0; i+1 < len(fields); i++ {
		if archiveMintGitWord(fields[i]) && fields[i+1] == archiveMintTagToken {
			return i + 1
		}
	}
	return -1
}

// archiveMintGitWord reports whether a field is the `git` command word, allowing
// the punctuation a shell and prose wrap it in: `$(git`, “ `git “, `(git`, and
// the assignment that usually precedes one (`tag=$(git`).
//
// The rule is "the field ENDS with the bare word `git`, preceded by a
// non-word character". That single condition keeps the two failure modes apart, and
// both are measured:
//
//   - too strict, and the REAL carrier escapes. The shipped Go literal rendered
//     `"…archive it (git tag archive/<name> + branch -D), "` — its command word is
//     `(git`, and a gate accepting only a bare `git` would have read it as prose.
//   - too loose (a plain substring match), and prose is reported: the `digit` of a
//     variable name would look like a mint.
func archiveMintGitWord(f string) bool {
	i := strings.LastIndex(f, archiveMintGitToken)
	if i < 0 || i+len(archiveMintGitToken) != len(f) {
		return false // does not END with the command word (`gitlab`, `digit`)
	}
	if i == 0 {
		return true // the bare word
	}
	return !archiveMintWordByte(f[i-1])
}

// archiveMintWordByte reports whether a byte continues a word, so that `digit`'s
// trailing `git` is not read as the command. Only ASCII matters here: every shell
// operator and every wrapper character is ASCII.
func archiveMintWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}
