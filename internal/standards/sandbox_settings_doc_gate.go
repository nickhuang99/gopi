package standardgate

// sandbox_settings_doc_gate.go — the DOC must describe the sandbox profile's
// settings entry as the shape that SHIPPED.
//
// THE DRIFT THIS GATE ENDS (measured 2026-10-15, task
// 01a14a53-d954-7117-889a-2be9bec0b845). PROJECT.md Rule 34 described a sandbox's
// `<profile>/settings.json` as "a **symlink** to the REAL settings file,
// read-only by contract", and a bug row was filed on the observation that a
// created sandbox carries a regular `0444` file instead. The CODE was right and
// the doc was stale: internal/sandbox/settings_doc.go replaced the link with a
// read-only COPY after the 2026-09-27 incident in which
// `printf '{…}' > "$SB/.gopi/settings.json"` FOLLOWED the link into the
// operator's live settings document and truncated it (12 keys lost, including
// the api_key and the `repos` table `gopi merge` keys on). A "read-only" symlink
// is unenforceable — the permission bits of a symlink are the TARGET's — so the
// shipped shape is a copy and always will be.
//
// WHY A TEST AND NOT A COMMENT. This is the second time the same sentence
// disagreed with the same code: a reader who trusts GOPI.md concludes the
// machinery is broken, and a reader who files that conclusion as a bug costs a
// whole session (this one). The rule file is an INSTRUCTION surface — every
// agent reads it at startup — so a stale shape sentence there is not a typo, it
// is a false premise that propagates.
//
// WHAT IS CHECKED, and it is deliberately narrow (a gate that fires on prose
// gets switched off):
//
//	1. a SHAPE ASSERTION about the sandbox settings entry — a line that names the
//	   entry in backticks and then states its KIND after the em-dash the docs use
//	   for that form — must NOT name a link;
//	2. the same assertion, when it names the copy/file shape, must carry the
//	   mode token (`0444`), because "a read-only copy" without the mode is the
//	   same claim one refactor away from drift;
//	3. GOPI.md's sandbox-shape paragraph (the one anchored at "every sandbox is
//	   private") must carry that assertion at all — a reader who reads only the
//	   rule file must be able to learn the shape from it.
//
// WHAT IS OUT OF SCOPE: devdoc/, briefings/, tasks/, reports/ and _test.go
// files MUST stay able to QUOTE the pre-fix shape — a record that cannot print
// its own subject is not a record (the conflict-marker gate's fence rationale,
// and the branch-force gate's history-tree exclusion). A line whose SUBJECT is
// the refused shape carries the inline escape  settings-doc-ok: <reason>  and
// stays visible in the data report.

import (
	"regexp"
	"strings"
)

// sandboxSettingsDocGate is the gate's registration.
func sandboxSettingsDocGate() gate {
	return gate{
		id:             "Rule 34 (a sandbox's settings entry is documented as the shipped 0444 copy, never a link)",
		checklistRow:   "34/sandbox-shape",
		probe:          "TestSandboxSettingsDocGateRedProof",
		scanTextRoot:   sandboxSettingsDocScanRoot,
		textCorpus:     sandboxSettingsDocCorpus,
		escape:         sandboxSettingsDocEscape,
		baselineReason: "EMPTY by construction: the doc described a shape the code had already replaced, and that is the defect this gate exists for — there is no site that is legitimately both a shape assertion about the sandbox settings entry AND a claim of a link.",
		baseline:       sandboxSettingsDocBaseline,
		find:           sandboxSettingsDocFind,
	}
}

// sandboxSettingsDocBaseline is empty BY CONSTRUCTION (see the gate's reason).
var sandboxSettingsDocBaseline []string

const (
	// sandboxSettingsDocScanRoot is the module root: the surfaces this gate
	// covers are the rule file, CONFIG.md, the help pages and the production Go
	// comments, and they live in different trees — the corpus predicate is what
	// narrows the walk (one place, visible to a reader of the scan).
	sandboxSettingsDocScanRoot = "."
	// sandboxSettingsDocEscape marks a line whose SUBJECT is the refused shape
	// (Rule 25: mention is not prescription).
	sandboxSettingsDocEscape = "settings-doc-ok:"
	// sandboxSettingsDocEntryToken is the entry the assertion is about. It is
	// the backticked spelling every surface uses.
	sandboxSettingsDocEntryToken = "`settings.json`"
	// sandboxSettingsDocShapeDash is the shape form the docs use: the entry
	// token, an em-dash, and then the kind ("a **symlink**", "a **read-only
	// COPY**", …). Prose that merely MENTIONS the entry ("resolution reads
	// settings.json", "the self-referential shape the settings copy refuses")
	// carries no dash and is never a shape assertion.
	sandboxSettingsDocShapeDash = "—"
	// sandboxSettingsDocModeToken is the mode a copy assertion must name.
	sandboxSettingsDocModeToken = "0444"
	// sandboxSettingsDocMinFiles is the non-vacuity floor: the corpus is the
	// rule file, the configuration reference and the production tree, and a
	// broken walk would otherwise report a clean green while reading nothing.
	sandboxSettingsDocMinFiles = 100
	// sandboxSettingsDocShapeAnchor is the sentence PROJECT.md's Rule 34 uses to
	// introduce the profile's contents; the paragraph that follows it is the
	// one a reader consults for the shape.
	sandboxSettingsDocShapeAnchor = "every sandbox is private"
)

// sandboxSettingsDocLinkClaim matches an AFFIRMATIVE link claim inside a shape
// assertion. The refusal form ("NEVER a link", "not a link") and the historical
// form ("was a symlink") deliberately do not match: the predicate reads
// "— a **symlink**" and nothing else, which is exactly the sentence that shipped
// wrong.
var sandboxSettingsDocLinkClaim = regexp.MustCompile(`—\s*a\s+\*{0,2}(sym)?link`)

// sandboxSettingsDocKindClaim matches a shape assertion that names a kind
// (copy / file) without necessarily naming the mode — the second arm.
var sandboxSettingsDocKindClaim = regexp.MustCompile(`—\s*a\s+\*{0,2}(read-only\s+)?(copy|file)|—\s*a\s+\*{0,2}regular`)

// sandboxSettingsDocCorpus narrows the walk to the surfaces this gate covers.
func sandboxSettingsDocCorpus(rel string) bool {
	if rel == ProjectDocName || rel == "CONFIG.md" {
		return true
	}
	if sandboxSettingsDocHistoryTree(rel) {
		return false
	}
	if strings.HasSuffix(rel, "_test.go") {
		return false
	}
	return strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, sandboxSettingsDocMdSuffix)
}

// sandboxSettingsDocMdSuffix is the markdown suffix, named so the corpus does
// not type it twice.
const sandboxSettingsDocMdSuffix = ".md"

// sandboxSettingsDocHistoryTree reports whether rel lives in a tree that records
// HISTORY rather than prescribing behaviour: those must be able to quote the
// pre-fix shape (see the file comment).
func sandboxSettingsDocHistoryTree(rel string) bool {
	for _, prefix := range sandboxSettingsDocHistoryPrefixes {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

// sandboxSettingsDocHistoryPrefixes are the quote-allowed trees (Rule 1: the
// values are named here, not spelled at the call site).
var sandboxSettingsDocHistoryPrefixes = []string{
	"devdoc/", "briefings/", "tasks/", "reports/", "docs/",
}

// sandboxSettingsDocFind is the pure per-file predicate: it reports a shape
// assertion about the sandbox settings entry that disagrees with the shipped
// shape.
func sandboxSettingsDocFind(g gate, p *parsed) []hit {
	var out []hit
	for i, raw := range strings.Split(string(p.src), "\n") {
		desc := sandboxSettingsDocLineProblem(raw)
		if desc == "" {
			continue
		}
		out = append(out, hit{rel: p.rel, line: i + 1, desc: desc})
	}
	return out
}

// sandboxSettingsDocLineProblem reports the problem on ONE line, or "" when the
// line is not a shape assertion or agrees with the shipped shape.
func sandboxSettingsDocLineProblem(line string) string {
	dash := strings.Index(line, sandboxSettingsDocShapeDash)
	token := strings.Index(line, sandboxSettingsDocEntryToken)
	if token < 0 || dash < 0 || dash < token {
		return ""
	}
	assertion := line[dash:]
	if sandboxSettingsDocLinkClaim.MatchString(assertion) {
		return "the sandbox settings entry is documented as a LINK — the shipped shape is a read-only 0444 COPY " +
			"(a redirect through a link truncated the operator's live settings document on 2026-09-27; " +
			"internal/sandbox/settings_doc.go)"
	}
	if sandboxSettingsDocKindClaim.MatchString(assertion) && !strings.Contains(assertion, sandboxSettingsDocModeToken) {
		return "the sandbox settings entry is documented as a copy/file without its mode — name " +
			sandboxSettingsDocModeToken + ", the mode the profile materialises"
	}
	return ""
}

// sandboxSettingsDocShapeParagraph returns the paragraph GOPI.md uses to state
// the profile's contents, or "" when the anchor is gone (which is itself a
// failure — see the real-tree test).
func sandboxSettingsDocShapeParagraph(doc string) string {
	i := strings.Index(doc, sandboxSettingsDocShapeAnchor)
	if i < 0 {
		return ""
	}
	rest := doc[i:]
	lines := strings.Split(rest, "\n")
	var b strings.Builder
	for idx, l := range lines {
		if idx > 0 && strings.TrimSpace(l) == "" {
			break
		}
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}
