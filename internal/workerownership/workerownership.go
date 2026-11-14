// Package workerownership is the single source of truth for the ONE annotation
// that keeps the repo's worker-ownership doc gate GREEN when a new document
// teaches a worker-launch or task-run command (Rule 14: a shared value lives in
// exactly one small exported-constant leaf package — never mirrored, never
// typed inline).
//
// THE DEFECT IT CLOSES. internal/testutil's TestWorkerOwnershipPointerPresent
// fails any file under devdoc/, briefings/ or scripts/ that mentions a launch
// shape ("gopi worker", "gopi task run", …) without the canonical pointer line.
// The gate's offender set is whatever the CURRENT master tip contains, so
// master re-reddened on EVERY merge round that landed another teaching document
// — measured 7 -> 8 -> 11 -> 5 -> 5 -> 6 across seven advances in one task
// (task 01a0a941). No branch can close that: the fix has to be part of the
// WRITE PATH, so the annotation is present in the bytes the writer lands rather
// than added by hand afterwards — which is also what made the failure
// non-actionable, because the author of a report has no reason to know the gate
// exists until it reddens somebody else's merge.
//
// WHERE IT IS CONSUMED. Both faces reference THIS package, so the writer and
// the gate can never drift apart:
//
//	the gate    internal/testutil/worker_ownership_gate_test.go — walks the
//	            audited trees, matches the needles, and demands an annotation
//	            line carrying Marker AND CanonicalRel
//	the writer  internal/tools (the write/edit file tools — the artifact that
//	            authors devdocs, briefings and scripts) — calls AnnotateOwnerDocs
//	            before the bytes reach disk
//
// THE PACKAGE IS A LEAF: stdlib plus internal/procname (itself a zero-import
// constants package), pure text functions, no I/O. internal/testutil imports it
// (a test package may) and internal/tools imports it (production), so no
// import cycle can form around it — the tasktags / taskverdict / toolnames
// precedent.
package workerownership

import (
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/procname"
)

const (
	// Marker is the machine-detectable token an annotation carries. It is
	// deliberately stricter than "see worker-ownership.md": the token says WHO
	// may run the command (operator-only), which is the ruling's content, so an
	// annotation cannot be mistaken for a bare cross-reference.
	Marker = "WORKER-OWNERSHIP: OPERATOR-ONLY"

	// CanonicalRel is the ONE repo-relative file that owns the policy: who
	// starts a worker, what an agent does instead, and what the guard's refusal
	// means. Every annotation names it, and the gate refuses an annotation that
	// carries the marker without naming it (a marker with a dead reference is
	// not a pointer).
	CanonicalRel = "devdoc/worker-ownership.md"

	// TaskRunSubverb is the `task` sub-verb that ran one task directly — the
	// RETIRED spelling (epic worker-run-entry, 2026-09-16: running a row is
	// `gopi worker`'s single job). It stays a needle because the shape must
	// still be recognised: the audited corpus is FULL of historical
	// "nohup gopi task run …" transcripts that read as instructions as long as
	// they sit there, and `gopi task run` still reaches a pointed removal
	// message rather than disappearing. cmd/agent spells it as
	// internal/taskverbs.Run / internal/runcmd.VerbRun; naming it once here
	// keeps it out of the needle list's body and gives the tests one value to
	// join.
	TaskRunSubverb = "run"

	// LauncherName is the operator's worker launcher script, named in the
	// needles because a document that mentions only the script name still reads
	// as a launch instruction.
	LauncherName = procname.NameGopi + "-" + procname.VerbWorker + "-up.sh"

	// Line is the canonical annotation WITHOUT its comment form. The gate
	// matches on the contents (marker AND canonical path on one line), so every
	// comment form satisfies it; this value is what those forms are built from,
	// which is why they can never disagree. The em-dash is U+2014, copied
	// verbatim from the canonical block.
	Line = Marker + " — " + CanonicalRel

	// OwnerTail is the sentence the canonical block itself uses to say what the
	// marker means: the operator owns the command, and an agent has a different
	// verb. Keeping the tail fixed here means every emission carries the same
	// explanation — an annotation a reader cannot act on is the failure this
	// closes.
	OwnerTail = " — the operator owns this; an agent must never run it (submit the work instead: " +
		procname.NameGopi + " " + procname.VerbTask + " submit)."
)

// The audited trees and the file kinds the audit covers. They are values here
// (not in the gate) because the WRITER asks the same question — "does this
// destination land inside the audited scope?" — and must give the same answer
// as the walker.
const (
	DirDevdoc    = "devdoc"
	DirBriefings = "briefings"
	DirScripts   = "scripts"

	// SuffixDoc is the markdown of both doc trees; SuffixSh the shell scripts
	// of all three trees; SuffixPlain the pasted run transcripts the audit's
	// §7.2 locations are made of — a committed smoke-run log still READS as a
	// copyable launch sequence.
	SuffixDoc   = ".md"
	SuffixSh    = ".sh"
	SuffixPlain = ".txt"
)

// Needles are the command SHAPES that read as an instruction to start a worker
// or run a task. They are built from internal/procname's constants — the same
// values the launcher script and the CLI read — plus the launcher's file name,
// so a rename on either side reddens the gate instead of silently un-matching
// the corpus.
var Needles = []string{
	procname.NameGopi + " " + procname.VerbWorker,                      // "gopi worker"
	procname.NameGopi + " " + procname.VerbTask + " " + TaskRunSubverb, // "gopi task run"
	LauncherName, // "gopi-worker-up.sh" — the operator's launcher, by name
}

// LaunchShapeSeparator joins TWO command shapes in the corpus's slash-pair
// idiom — `gopi task run/worker`, `gopi worker/merger`, `gopi task run/serve`
// — where an author names two group verbs at once instead of writing both
// shapes out. The idiom is shorthand, not a command: `gopi task run/worker` is
// not runnable as written.
//
// It is the reason a bare substring match is not enough (Rule 1b: the
// separator is a constant, never a literal at the matching site). The pair
// `task run/worker` CONTAINS the needle `task run`, and `worker/merger`
// contains `worker`, so a document that only says "these two verbs return from
// main()" used to read as a launch instruction (measured: the gate reddened on
// devdoc/tmpdir-param-not-banner.md:60 through two successive master tips).
// The matcher therefore ignores a needle that sits at the LEFT of this
// separator (see abbreviatedWithSlash): a real launch shape is written out in
// full, never abbreviated to the right of a `/`.
const LaunchShapeSeparator = '/'

// AuditedRoots returns the trees the gate walks and the writer injects into. It
// returns a fresh slice so a caller can never mutate the shared list.
func AuditedRoots() []string {
	return []string{DirDevdoc, DirBriefings, DirScripts}
}

// AuditedSuffixes returns the file extensions the audit covers, likewise copied.
func AuditedSuffixes() []string {
	return []string{SuffixDoc, SuffixSh, SuffixPlain}
}

// Audited reports whether a suffix is inside the audit's scope. It takes the
// extension rather than a whole path so both consumers answer the question the
// same way: the gate calls it while walking a tree, the writer calls it on the
// destination it is about to land.
func Audited(suffix string) bool {
	for _, s := range AuditedSuffixes() {
		if suffix == s {
			return true
		}
	}
	return false
}

// devdoc-ref-ok: the paths below illustrate the three SPELLINGS this function
// normalises (relative, dot-relative, absolute); they are not citations.
//
// InAuditedTree reports whether a destination path lies inside one of the
// audited trees. It asks about the DIRECTORY COMPONENTS, not a string prefix,
// because the gate's walk recurses: devdoc/report.md and
// devdoc/worker-daemon-acceptance/closing-run-1806.txt are both in scope, and
// the path a tool receives may be relative ("devdoc/x.md"), cleaned
// ("./devdoc/x.md") or absolute ("/home/…/devdoc/x.md") — all three must
// answer yes, and a same-named directory in an unrelated tree
// ("other/devdoc/x.md") is the only shape this cannot tell apart, where an
// extra comment line is harmless.
func InAuditedTree(path string) bool {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	if len(parts) < 2 {
		return false
	}
	for _, dir := range parts[:len(parts)-1] {
		for _, root := range AuditedRoots() {
			if dir == root {
				return true
			}
		}
	}
	return false
}

// Injectable reports whether the WRITE-TIME annotation applies to a
// destination path: it must land inside an audited tree AND be a file kind the
// write path actually authors.
//
// It is deliberately narrower than Audited, and the difference is the pasted
// capture: a `.txt` under devdoc/ is a smoke-run transcript whose bytes are the
// evidence — an annotation inside captured output corrupts it for anyone
// grepping the log. There is a second, mechanical reason: the gate's own
// dismissal-honesty rule fails a dismissed file that carries the pointer, so a
// transcript the writer had annotated and a reviewer then dismissed would
// redden THAT check instead. The kinds the write path authors (markdown docs,
// shell scripts) are covered automatically; for a transcript the author
// decides, annotate or dismiss, exactly as before.
func Injectable(path string) bool {
	suffix := OwnerDocsSuffix(path)
	return (suffix == SuffixDoc || suffix == SuffixSh) && InAuditedTree(path)
}

// MentionsLaunchShape reports whether body mentions one of the needles — the
// exact predicate the gate applies to a file's bytes.
//
// A needle that sits at the LEFT of a slash-joined pair does NOT count (the
// abbreviation the corpus writes to name two verbs at once — see
// LaunchShapeSeparator). EVERY occurrence is examined, so a document that
// abbreviates a shape in one sentence and writes it out in the next is still a
// teacher: the qualifying occurrence is what makes it one.
func MentionsLaunchShape(body string) bool {
	for _, needle := range Needles {
		for from := 0; from < len(body); {
			at := strings.Index(body[from:], needle)
			if at < 0 {
				break
			}
			end := from + at + len(needle)
			if !abbreviatedWithSlash(body, end) {
				return true
			}
			from = from + at + 1 // step past this occurrence; keep looking
		}
	}
	return false
}

// abbreviatedWithSlash reports whether the byte at end opens the slash-pair
// idiom for the shape that ended there — i.e. the shape is being named as the
// left member of a `A/B` abbreviation rather than written out as a command.
func abbreviatedWithSlash(body string, end int) bool {
	return end < len(body) && body[end] == LaunchShapeSeparator
}

// Annotated reports whether body already carries the canonical annotation: one
// line carrying BOTH the marker and the canonical path. One line, both halves —
// that is the contract the gate enforces, so it is the contract the writer uses
// to decide "nothing to do" — which is what makes the injection idempotent.
func Annotated(body string) bool {
	_, ok := AnnotationLine(body)
	return ok
}

// AnnotationLine returns the line carrying the canonical annotation, if any.
func AnnotationLine(body string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, Marker) && strings.Contains(line, CanonicalRel) {
			return line, true
		}
	}
	return "", false
}

// CommentPrefix opens the comment form of an audited file kind: markdown gets
// an HTML comment (invisible when rendered), a shell script gets a `#` line.
func CommentPrefix(suffix string) string {
	if suffix == SuffixDoc {
		return "<!-- "
	}
	return "# "
}

// CommentSuffix closes the comment form opened by CommentPrefix (empty for the
// `#` forms, which run to end of line).
func CommentSuffix(suffix string) string {
	if suffix == SuffixDoc {
		return " -->"
	}
	return ""
}

// CommentLine renders the canonical annotation in the comment form of suffix —
// the exact string the write path inserts and the gate searches for.
func CommentLine(suffix string) string {
	return CommentPrefix(suffix) + Line + CommentSuffix(suffix)
}

// Quoted is the annotation as it appears INSIDE a document body, rather than at
// a file's head. It is the form the backfilled devdocs already carry — a
// blockquote directly under the title — so an in-body emission reproduces the
// established shape instead of inventing a second one.
func Quoted() string {
	return "> **" + Line + "**" + OwnerTail
}

// OwnerDocsSuffix returns the extension (lower-cased) of a destination path, or
// "" when the path names no file.
func OwnerDocsSuffix(path string) string {
	return strings.ToLower(filepath.Ext(path))
}

// AnnotateOwnerDocs is the WRITE-TIME injection: given the destination path and
// the content about to be written, it returns content carrying the canonical
// annotation when — and only when — all three hold:
//
//	the destination is an audited file kind the write path authors (.md/.sh),
//	the content mentions a launch shape (see Needles), and
//	it does not already carry the annotation (idempotent: a second write of an
//	annotated file is a byte-for-byte no-op).
//
// It is a PURE text function — no I/O, no git, no repository walk — so the
// write path stays cheap and the same call can be unit-tested with a literal.
// The returned bool reports whether the content was changed, which is what the
// end-to-end tests assert.
func AnnotateOwnerDocs(path, content string) (string, bool) {
	if !Injectable(path) || content == "" {
		return content, false
	}
	suffix := OwnerDocsSuffix(path)
	if !MentionsLaunchShape(content) || Annotated(content) {
		return content, false
	}
	line := CommentLine(suffix)
	if suffix == SuffixSh {
		// A script's shebang must stay the FIRST line — a shell reads it to
		// pick the interpreter — so the annotation goes directly AFTER it.
		if strings.HasPrefix(content, "#!") {
			if i := strings.Index(content, "\n"); i >= 0 {
				return content[:i+1] + line + "\n" + content[i+1:], true
			}
		}
		return line + "\n\n" + content, true
	}
	// Markdown: after the leading title block (the head is the contiguous run
	// of non-blank lines at the top), so the annotation is visible immediately
	// and the document still opens with its own title.
	if i := strings.Index(content, "\n\n"); i >= 0 {
		return content[:i+2] + line + "\n\n" + content[i+2:], true
	}
	// No blank line anywhere: the whole file is one head block — append after
	// it rather than splitting its first line apart.
	if strings.HasSuffix(content, "\n") {
		return content + "\n" + line + "\n", true
	}
	return content + "\n\n" + line + "\n", true
}
