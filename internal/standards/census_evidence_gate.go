package standardgate

// census_evidence_gate.go — a census figure must name how it is re-derived
// (PROJECT.md Rule 38), and the check the rule cites must EXIST.
//
// WHY THIS FILE EXISTS AT ALL, in this shape. Rule 38 named this gate for
// months while the gate was gone: commit 29882b3 ("standards: drop the
// document-pipeline gates, slice 1/5") deleted census_evidence_gate.go and its
// test WHOLE, because this package was the only importer of internal/docclass
// and internal/docindex, and the slice removed those packages. PROJECT.md's
// Check line was not touched, so `go test ./internal/standards/ -run
// TestCensusGate` printed "no tests to run" and exited 0 — a permanently green
// check, the worst shape a gate has, because the green is indistinguishable
// from a clean corpus. This file is the restored implementation, ported off the
// deleted internal/docclass onto the still-living internal/devdoc, and its test
// is named TestCensusGate* so the rule's own citation runs something again.
//
// WHY THE GATE ITSELF IS STILL RIGHT. A number about the WHOLE project is the
// cheapest thing in this repository to write and the fastest thing in it to
// become false. Nothing in the tree said which numbers a reader may ACT on and
// which are one run's narrative, so nothing could be gated. The boundary this
// gate encodes, stated once here so any document can be classified without
// reading the predicate:
//
//	CENSUS CLAIM — a figure about the project AS A WHOLE that a reader acts on
//	("the repo has N tracked files", "N% of tool calls"). It must be
//	REGENERABLE: the document cites the GENERATOR that prints it and the COMMIT
//	it was measured at, so the figure can be re-measured instead of trusted.
//
//	NARRATIVE RECORD — a figure that is part of a story about ONE run: a log
//	line number, a pid, an incident's byte offset, "line 1716 of a file at
//	commit X", "13 producer arms in internal/tools". It stays in prose and is
//	NOT judged here: the number IS the evidence of that run, it is falsified by
//	being "fixed", and no generator could reproduce it.
//
// WHAT IT CHECKS, mechanically (no judgement call in the predicate — the bar
// Rules 16–21 set). A markdown document under devdoc/ is JUDGED when its TITLE
// declares a census of the whole project — BOTH title cues, because either one
// alone is not the claim:
//
//	A. a CENSUS word — census | statistics | inventory | snapshot; and
//	B. a SCOPE word — project | repository | repo | codebase | entire | whole |
//	   full | top-level | every,
//
// and its body carries a PROJECT-WIDE QUANTITY: a comma-grouped figure, a
// four-digit-or-longer figure that is not a year, or a percentage, on a line
// that also names a project DIMENSION (tracked files, commits, LOC, packages,
// env vars, verbs, tool names, markdown/devdoc counts, Go/test file counts, …).
// It must then show BOTH:
//
//	1. a GENERATOR — a `scripts/<name>.sh|py` span, or the word "generator";
//	2. a PINNED COMMIT — a 7–40 hex token that RESOLVES in this repository
//	   (`git cat-file -e <tok>^{commit}` — the same check a reader would run).
//	   A token that does not resolve is not a pin: a uuid, a date and a
//	   content hash all LOOK like one, and only a resolving commit is one.
//
// THE INLINE ESCAPE, and its price: a judged document that legitimately carries
// such a figure and cannot be re-derived (a past run's own record of a tree
// that no longer exists) marks it `census-evidence-ok: <reason>`. The reason is
// REQUIRED and has a floor length, so "escaped" is a stated decision rather
// than a silent exemption (Rule 15); a stub reason does NOT exempt the document
// — it stays judged, and the report prints the stub so a reader sees what the
// author wrote. The gate PRINTS every escape with its reason, and the gate's
// own test holds the escape count at its MEASURED ceiling
// (censusMaxEscapedDocs = 0 today). Raising that ceiling is a deliberate,
// reviewed edit that states why — the escape is a decision, never a growth
// (Rule 13: the constant exists to be a ratchet, not a loophole).
//
// SCOPE, honestly declared:
//
//   - only the devdoc corpus (devdoc.Root) — the documented corpus whose files
//     Rule 36's header gate already classifies. Reports under tasks/ and bugs/
//     are per-task records; briefings/ are instructions TO a run; none is a
//     standing claim a reader of the corpus acts on.
//   - only the TITLE declares a census, and it must carry BOTH cues. A census
//     figure in a document whose title names a command group ("`gopi memory`
//     help-surface inventory") or one run ("Task Report: …") is NOT judged: 17
//     verbs in one group and 13 producer arms in one package are LOCAL figures,
//     and this gate declines to call them project-wide.
//   - a FIGURE inside a code span is not a quantity — a commit hash, a task id
//     and a file path all live in backticks, and reading them as census figures
//     is exactly the noise that would make this gate unkeepable.
//   - a bare four-digit year (censusYearLow–censusYearHigh) is a date, not a
//     quantity.
//   - the dimension must be within censusQuantityWindow characters of the
//     figure on the same line, so this gate UNDER-reports rather than guessing.
//   - the corpus walk and the markdown vocabulary come from internal/devdoc
//     (the ONE definition of both); this gate adds no class, no manifest and no
//     second policy — it asks a different question of the same corpus.
//   - NOT a duplicate of Rule 36's header gate: that one holds a document's
//     CLASS declaration; this one holds its RE-DERIVATION. A document can pass
//     either one and fail the other, which is why both exist.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/devdoc"
)

const (
	// censusCorpusRoot is the judged corpus — the same root (and therefore the
	// same reader) the devdoc truth-class gates use, never a second spelling.
	censusCorpusRoot = devdoc.Root

	// censusMarkdownExt is the corpus's prose extension, from the same package
	// that owns Root: a walk spelled with its own ".md" would silently narrow
	// the moment the corpus vocabulary moves (Rule 1b).
	censusMarkdownExt = devdoc.MarkdownExt

	// censusTitleScanLines bounds the title search: a markdown doc's H1 comes
	// first, and a Rule 36 banner / work-ownership comment may precede it.
	censusTitleScanLines = 40

	// censusTitleMarker is the markdown H1 prefix a title is read from.
	censusTitleMarker = "# "

	// censusQuantityWindow is how far from its dimension a figure may sit on one
	// line before this gate declines to call it project-wide. Sized to cover the
	// shapes this corpus actually writes — "| Tracked files | 5,258 |" and
	// "1,542 markdown files" (a table cell or a short sentence) — and to stop
	// short of a line that merely happens to carry both a large number and an
	// unrelated dimension word ("a merge-base --is-ancestor 38d8509 … 866 clean
	// renames"). Measured on the corpus: 24 reads the three positive controls'
	// headline rows; a wider window starts matching prose line numbers.
	censusQuantityWindow = 24

	// censusYearLow/censusYearHigh bound the four-digit numbers that are DATES
	// rather than quantities: "2026-09-27" is a snapshot's date, not its census.
	// RE2 has no lookahead, so the exclusion is applied by value, in Go.
	censusYearLow  = 1900
	censusYearHigh = 2099

	// censusEscapeMarker is the inline escape; censusMinEscapeReasonLen is its
	// price — the same discipline (and the same floor) the manifest's rows
	// carry: a reason must say WHY, not merely claim the exemption.
	censusEscapeMarker       = "census-evidence-ok:"
	censusMinEscapeReasonLen = 24

	// censusDeniedCuePattern is a generator mention that is a DENIAL rather than
	// evidence: a negation word, at most a couple of words, then the cue. A
	// document that writes "no generator is named here" must not satisfy the
	// generator half of the gate on the strength of the word it used to say it
	// has none — measured 2026-10-22 on the real-corpus red proof, whose refusal
	// sentence read `generator generator`: the gate accepted its own absence as
	// evidence. The window is deliberately NARROW (0–2 words): "there is no
	// substitute for re-running the generator" is a CITATION, not a denial, and
	// a line-wide negation test drops it.
	censusDeniedCuePattern = `(?i)\b(?:no|not|none|without|never|nothing|nobody)\s+(?:\w+\s+){0,2}(?:generator|script)s?\b`

	// censusHTMLCommentClose is the tail the escape marker's line carries when it
	// is written as an HTML comment (`<!-- census-evidence-ok: <reason> -->`).
	// The reason is the text BETWEEN the marker and the closer; leaving the
	// closer attached would print it in the report as if it were part of the
	// author's sentence.
	censusHTMLCommentClose = "-->"

	// censusMinJudgedDocs is the non-vacuity floor for the REAL-corpus run: the
	// gate measured 5 judged documents when it first landed (the three named
	// positive controls plus two more carrying the same evidence). A sweep that
	// judges nothing is vacuously green, so it fails here instead.
	censusMinJudgedDocs = 3

	// censusMinPassingDocs is the floor in the OTHER direction: how many judged
	// documents must actually CARRY the evidence. Without it, a corpus whose
	// judged documents all escaped or all went red would still pass a
	// judged-count floor — floors are needed on BOTH sides, so the gate can
	// neither shrink to nothingness nor rest on a corpus that stopped showing
	// the evidence it is supposed to hold.
	censusMinPassingDocs = 2

	// censusMaxEscapedDocs is the escape RATCHET, at its measured value: the
	// corpus carries no escape, so every escape is a deliberate edit that raises
	// this number in the same commit and says why. A gate whose permits grow
	// without a decision is not a gate.
	censusMaxEscapedDocs = 0

	// censusMarkdownFloor is the corpus-size floor: the devdoc corpus measures
	// 895 markdown documents on this rev. A walk that reads a wrong root, an
	// empty directory or a filtered listing is caught here by name instead of
	// passing on zero files.
	//
	// IT FELL FROM 2538 WITH THE CORPUS, deliberately. The public export carries
	// only the documents that still make a standing claim (doc-class normative
	// and roadmap) plus the historical records a code literal or a root document
	// links to, so one cut removed ~1700 documents. A floor left at the old
	// value measures nothing: it refuses the corpus that exists, which is not
	// the failure it was written for (a green that covered nothing). It stays far
	// above zero and above any single directory, so a broken root is still named.
	censusMarkdownFloor = 700

	// censusGitBinary / censusCommitPeel spell the reader's own pin check:
	// `git cat-file -e <tok>^{commit}`. The `^{commit}` peel is what rejects a
	// tree, a blob or a tag object — a token that names anything else is not a
	// pinned COMMIT. The binary is a named constant here, as every production
	// git caller in this package spells it (Rule 1).
	censusGitBinary  = "git"
	censusCommitPeel = "^{commit}"

	// censusGitDirFlag / censusGitCatFileVerb / censusGitExistsFlag spell the
	// argv of that check: `git -C <root> cat-file -e <tok>^{commit}`.
	censusGitDirFlag     = "-C"
	censusGitCatFileVerb = "cat-file"
	censusGitExistsFlag  = "-e"
)

// censusTitleCues are the title words that declare a document a CENSUS — any
// inflection, because the title is the document's own claim about what it is
// ("statistics", "statistic", "inventory", "inventories", "snapshot",
// "snapshots", "census", "censuses"). The stems are matched with a trailing
// \w* so no call site enumerates spellings.
var censusTitleCues = []string{"census", "statistic", "inventor", "snapshot"}

// censusTitleScopeCues are the title words that put the census at the SCOPE of
// the whole project — the half without which a census word alone means "this
// group's own surface" ("`gopi task` help-surface inventory"), which is a local
// figure and stays narrative. The list is CALIBRATED against this corpus, not
// guessed: `full` and `top-level` were cues in the first draft and each dragged
// an ordinary generator-less report into the gate, so they are absent here.
var censusTitleScopeCues = []string{"project", "projects", "repository", "repositories", "repo", "repos", "codebase", "entire", "whole", "every"}

// censusTitleCueRe matches a census word at a word boundary, any inflection.
var censusTitleCueRe = regexp.MustCompile(`(?i)\b(?:` + strings.Join(censusTitleCues, "|") + `)\w*\b`)

// censusTitleScopeRe matches a project-scope word at a word boundary.
var censusTitleScopeRe = regexp.MustCompile(`(?i)\b(?:` + strings.Join(censusTitleScopeCues, "|") + `)\b`)

// censusDimensionPattern names the project dimensions a figure can be a census
// OF. A number beside none of these is not this gate's subject.
const censusDimensionPattern = `(?:tracked files?|tracked content|commits?|\bloc\b|lines of code|functions?|packages?|env(?:ironment)? vars?|modules?|subcommands?|verbs?|tool names?|markdown[\w ]*|devdoc[\w ]*|test files?|go files?|shell[\w ]*|python[\w ]*|settings\.json keys)`

// censusQuantityPattern is what a project-wide quantity LOOKS like: a
// comma-grouped figure, a four-digit-or-longer run, a two-digit run, or a
// percentage. Four digits are included because "5258 tracked files" is written
// that way in this corpus; the year band is excluded by value, not by pattern.
const censusQuantityPattern = `(?:\b\d{1,3}(?:,\d{3})+\b|\b\d{4,}\b|\b\d{2,}\b|\b\d+(?:\.\d+)?\s?%)`

var censusFigureRe = regexp.MustCompile(`(?i)` + censusQuantityPattern)
var censusDimensionRe = regexp.MustCompile(`(?i)` + censusDimensionPattern)

// censusCodeSpanRe strips a markdown code span before figures are read: a
// commit hash, a task id and a file path all live in backticks.
var censusCodeSpanRe = regexp.MustCompile("`[^`]*`")

// censusGeneratorPathRe / censusGeneratorCueRe are the two shapes of generator
// evidence: the script that prints the figures, or the word naming one.
var censusGeneratorPathRe = regexp.MustCompile(`(?i)scripts/[A-Za-z0-9_.-]+\.(?:sh|py)`)
var censusGeneratorCueRe = regexp.MustCompile(`(?i)\bgenerator\b`)

// censusPinCueRe restricts where a commit token may be READ from: only a line
// that states a pin. A token that merely appears in prose is a quotation, not a
// declaration of where the figures came from.
var censusPinCueRe = regexp.MustCompile(`(?i)(base commit|base:|measured on master at|measured at|pinned commit|\bcommit\b|@[0-9a-f]{7,40})`)

// censusHexTokenRe is the shape of a commit token candidate.
var censusHexTokenRe = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)

// censusEscapeRe captures the escape marker's reason payload.
var censusEscapeRe = regexp.MustCompile(censusEscapeMarker + `\s*([^\n]*)`)

// censusDeniedCueRe is the denial test censusGeneratorEvidence applies per line.
var censusDeniedCueRe = regexp.MustCompile(censusDeniedCuePattern)

// CensusFinding is one judged document: what the gate saw, and its verdict.
type CensusFinding struct {
	Path         string // repo-relative path of the judged document
	Title        string // its H1, the self-declaration the scope keys on
	Figure       string // the first project-wide quantity found (for the report)
	Generator    string // the generator evidence, "" when none was cited
	Pin          string // the pinned commit that RESOLVED, "" when none did
	EscapeReason string // the inline escape's reason, "" when not escaped
	EscapeStub   string // the escape payload when it was TOO SHORT to exempt
}

// CensusGated reports whether the document is inside the gate — an escaped
// document is RECORDED, not judged (the escape is the stated decision).
func (f CensusFinding) CensusGated() bool { return strings.TrimSpace(f.EscapeReason) == "" }

// CensusPasses is the gate's requirement, evaluated: a gated document must name
// a generator AND a pinned commit that resolves in this repository.
func (f CensusFinding) CensusPasses() bool {
	return strings.TrimSpace(f.Generator) != "" && strings.TrimSpace(f.Pin) != ""
}

// CensusEvidenceFindings judges the corpus under root and returns one finding
// per judged document, in the corpus's path order. A document that is not a
// title-declared project census, or that carries no project-wide figure,
// produces no finding — that is the gate's scope, not an exemption.
func CensusEvidenceFindings(root string) ([]CensusFinding, error) {
	files, err := censusCorpusFiles(root)
	if err != nil {
		return nil, err
	}
	var findings []CensusFinding
	for _, rel := range files {
		body, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			// Rule 15: a corpus file that cannot be read is REPORTED, never
			// skipped as if it had passed.
			return nil, rerr
		}
		text := string(body)
		title := censusTitle(text)
		if !CensusTitleDeclaresProjectCensus(title) {
			continue
		}
		figure := CensusFigure(text)
		if figure == "" {
			continue
		}
		f := CensusFinding{Path: rel, Title: title, Figure: figure}
		f.Generator = censusGeneratorEvidence(text)
		f.Pin = censusResolvingPin(root, text)
		f.EscapeReason, f.EscapeStub = censusEscapeReason(text)
		findings = append(findings, f)
	}
	return findings, nil
}

// CensusEvidenceViolations is the gate's per-document verdict: the gated
// findings that fail.
func CensusEvidenceViolations(findings []CensusFinding) []CensusFinding {
	var bad []CensusFinding
	for _, f := range findings {
		if f.CensusGated() && !f.CensusPasses() {
			bad = append(bad, f)
		}
	}
	return bad
}

// CensusEvidenceVerdict turns a finished sweep into the gate's failure
// sentences, so the vacuity arms are testable on their own (no test file needs
// to re-derive them, and the fixture proofs drive exactly this function):
//
//   - judged 0 → refused: a sweep of nothing is not a clean corpus;
//   - judged < minJudged → refused: the green would rest on a fragment;
//   - escapes > maxEscaped → refused: the ratchet only ratchets down;
//   - each gated non-passing document → one sentence naming path, title,
//     figure, generator and pin, so a reader repairs in one pass.
//
// minJudged 0 / maxEscaped 0 are the FIXTURE settings: a fixture proves which
// defects the predicate names and cannot pretend to be the corpus.
func CensusEvidenceVerdict(findings []CensusFinding, minJudged, maxEscaped int) []string {
	var out []string
	escaped := censusEscapedCount(findings)
	if len(findings) == 0 {
		return append(out, censusProvenance()+": judged 0 census document(s) under "+censusCorpusRoot+"/ — a sweep of nothing is not a clean corpus (the root, the title rule or the figure rule is wrong)")
	}
	if minJudged > 0 && len(findings) < minJudged {
		out = append(out, censusProvenance()+": judged "+strconv.Itoa(len(findings))+" census document(s) under "+censusCorpusRoot+"/, below the non-vacuity floor "+strconv.Itoa(minJudged)+" — the green would rest on a fragment of the corpus")
	}
	if escaped > maxEscaped {
		out = append(out, censusProvenance()+": "+strconv.Itoa(escaped)+" document(s) carry the inline escape "+censusEscapeMarker+" (ceiling "+strconv.Itoa(maxEscaped)+") — an escape is a stated decision, not a growing exemption (raise censusMaxEscapedDocs in the same commit and say why)")
	}
	for _, f := range findings {
		if !f.CensusGated() || f.CensusPasses() {
			continue
		}
		out = append(out, censusDocumentSentence(f))
	}
	return out
}

// censusDocumentSentence is the failure sentence for ONE judged document. It is
// a function so the fixture proofs and the real-corpus run cannot word the same
// defect two ways.
func censusDocumentSentence(f CensusFinding) string {
	sentence := censusProvenance() + ": " + f.Path + ": title-declared project census (" + strconv.Quote(f.Title) + ") carries a project-wide figure (" + f.Figure + ") with generator " + censusTag(f.Generator) + " and pin " + censusTag(f.Pin) + " — a census figure must name the generator that re-derives it and the commit it was measured at (PROJECT.md Rule 38), or carry `" + censusEscapeMarker + " <reason>`"
	if strings.TrimSpace(f.EscapeStub) != "" {
		sentence += " (the escape reason " + strconv.Quote(f.EscapeStub) + " is below the " + strconv.Itoa(censusMinEscapeReasonLen) + "-character floor, so it does not exempt the document)"
	}
	return sentence
}

// CensusEvidenceReport renders one line per finding — the DATA the gate prints,
// so an exemption is visible to a reader of the log, not only to a reader of
// the source (the smoke_path_gate precedent).
func CensusEvidenceReport(findings []CensusFinding) string {
	var b strings.Builder
	for _, f := range findings {
		switch {
		case !f.CensusGated():
			b.WriteString("    escaped  " + f.Path + " — " + censusEscapeMarker + " " + f.EscapeReason + "\n")
		case f.CensusPasses():
			b.WriteString("    pass     " + f.Path + " — generator " + f.Generator + ", pin " + f.Pin + "\n")
		default:
			b.WriteString("    RED      " + f.Path + " — figure " + f.Figure + ", generator " + censusTag(f.Generator) + ", pin " + censusTag(f.Pin) + "\n")
		}
	}
	return b.String()
}

// CensusEvidenceSummary is the one-line accounting a reader of a green run
// needs: how much corpus was walked, how many documents were judged, and how
// those split between passing, escaped and red. A gate that prints only "ok"
// has told nobody what its green is worth.
func CensusEvidenceSummary(walked int, findings []CensusFinding) string {
	passing, escaped, red := 0, 0, 0
	for _, f := range findings {
		switch {
		case !f.CensusGated():
			escaped++
		case f.CensusPasses():
			passing++
		default:
			red++
		}
	}
	return censusProvenance() + ": walked " + strconv.Itoa(walked) + " markdown document(s) under " + censusCorpusRoot + "/, judged " + strconv.Itoa(len(findings)) + ", passing " + strconv.Itoa(passing) + ", escaped " + strconv.Itoa(escaped) + ", red " + strconv.Itoa(red) + " (floors: judged >= " + strconv.Itoa(censusMinJudgedDocs) + ", passing >= " + strconv.Itoa(censusMinPassingDocs) + ", escaped <= " + strconv.Itoa(censusMaxEscapedDocs) + ", corpus >= " + strconv.Itoa(censusMarkdownFloor) + ")"
}

// censusProvenance names who is speaking, so a failure attributed to this gate
// is distinguishable from the other standards gates (Rule 1: one spelling).
func censusProvenance() string { return "Rule 38 census gate" }

// censusEscapedCount is the number a ratchet counts: escaped findings only.
func censusEscapedCount(findings []CensusFinding) int {
	n := 0
	for _, f := range findings {
		if !f.CensusGated() {
			n++
		}
	}
	return n
}

// censusTag renders a finding field for the report, naming its absence rather
// than printing an empty cell a reader has to interpret.
func censusTag(v string) string {
	if strings.TrimSpace(v) == "" {
		return "MISSING"
	}
	return v
}

// censusTitle reads the document's H1 — the line the document declares itself in.
func censusTitle(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) > censusTitleScanLines {
		lines = lines[:censusTitleScanLines]
	}
	for _, l := range lines {
		if strings.HasPrefix(l, censusTitleMarker) {
			return strings.TrimSpace(strings.TrimPrefix(l, censusTitleMarker))
		}
	}
	return ""
}

// CensusTitleDeclaresProjectCensus reports whether a title carries BOTH the
// census cue and the project-scope cue — the gate's scope rule, exported so the
// red-proof pins the same predicate the sweep uses.
func CensusTitleDeclaresProjectCensus(title string) bool {
	return censusTitleCueRe.MatchString(title) && censusTitleScopeRe.MatchString(title)
}

// CensusFigure returns the first project-wide quantity in the document, or "".
// A figure counts only when its own line also names a project dimension within
// censusQuantityWindow characters, and only outside a code span and outside the
// year band.
func CensusFigure(text string) string {
	for _, raw := range strings.Split(text, "\n") {
		line := censusCodeSpanRe.ReplaceAllString(raw, " ")
		if !censusDimensionRe.MatchString(line) {
			continue
		}
		for _, loc := range censusFigureRe.FindAllStringIndex(line, -1) {
			figure := line[loc[0]:loc[1]]
			if censusIsYear(figure) {
				continue
			}
			lo, hi := loc[0]-censusQuantityWindow, loc[1]+censusQuantityWindow
			if lo < 0 {
				lo = 0
			}
			if hi > len(line) {
				hi = len(line)
			}
			if censusDimensionRe.MatchString(line[lo:hi]) {
				return strings.TrimSpace(figure)
			}
		}
	}
	return ""
}

// censusIsYear reports whether a matched figure is a four-digit year — a date
// in a snapshot's title, not a quantity about the tree.
func censusIsYear(figure string) bool {
	if len(figure) != 4 {
		return false
	}
	n, err := strconv.Atoi(figure)
	if err != nil {
		return false
	}
	return n >= censusYearLow && n <= censusYearHigh
}

// censusGeneratorEvidence names the generator a document cites: the script path
// when it names one, else the word it used — but only where that word is a
// CITATION and not a DENIAL. A path is never negated (a document that writes
// `scripts/x.sh` is naming a generator, whoever wrote the sentence around it);
// the bare cue is checked line by line, and a line that denies it ("no generator
// is named here") is not evidence.
func censusGeneratorEvidence(text string) string {
	if p := censusGeneratorPathRe.FindString(text); p != "" {
		return p
	}
	for _, line := range strings.Split(text, "\n") {
		cue := censusGeneratorCueRe.FindString(line)
		if cue == "" {
			continue
		}
		if censusDeniedCueRe.MatchString(line) {
			continue
		}
		return cue
	}
	return ""
}

// censusResolvingPin returns the first candidate commit token that resolves as
// a commit in this repository, reading candidates only from lines that state a
// pin. A token that does not resolve is not a pin (Rule 15: a document that
// merely QUOTES a hash keeps failing until it names the commit its figures came
// from).
func censusResolvingPin(root, text string) string {
	for _, line := range strings.Split(text, "\n") {
		if !censusPinCueRe.MatchString(line) {
			continue
		}
		for _, tok := range censusHexTokenRe.FindAllString(line, -1) {
			if censusCommitResolves(root, tok) {
				return tok
			}
		}
	}
	return ""
}

// censusCommitResolves runs the reader's own check: does this token name a
// commit in this repository? (`git cat-file -e <tok>^{commit}` — the peel is
// what rejects a tree, a blob or a tag object.) Output is discarded: the exit
// status IS the answer, and printing it would bury the gate's own report.
func censusCommitResolves(root, tok string) bool {
	cmd := exec.Command(censusGitBinary, censusGitDirFlag, root, censusGitCatFileVerb, censusGitExistsFlag, tok+censusCommitPeel)
	cmd.Stdout, cmd.Stderr = nil, nil
	return cmd.Run() == nil
}

// censusEscapeReason reads the inline escape and returns (reason, stub):
//
//	reason non-empty  — a STATED escape: the document is recorded, not judged;
//	reason "" + stub  — the marker is present but its reason is below
//	                    censusMinEscapeReasonLen: the document stays GATED and
//	                    the stub is carried so the report can quote it (a
//	                    permit that says nothing is not a permit, and a failure
//	                    that hides what the author wrote is a worse permit).
//	"" + ""           — no marker at all.
func censusEscapeReason(text string) (reason, stub string) {
	m := censusEscapeRe.FindStringSubmatch(text)
	if len(m) < 2 {
		return "", ""
	}
	payload := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m[1]), censusHTMLCommentClose))
	if len(payload) < censusMinEscapeReasonLen {
		return "", payload
	}
	return payload, ""
}

// censusCorpusFiles lists the judged corpus — every markdown document under
// devdoc/, repository-relative with slashes. The predicate is devdoc.IsProse,
// the ONE definition of "a prose document of this corpus" (Rule 36's gate reads
// the same one), so the two gates cannot disagree about what the corpus holds.
func censusCorpusFiles(root string) ([]string, error) {
	var out []string
	corpusRoot := filepath.Join(root, censusCorpusRoot)
	err := filepath.Walk(corpusRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// A directory the walk cannot enter is REPORTED, not skipped: a
			// silently narrowed corpus reads as a clean one.
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, censusMarkdownExt) {
			return nil
		}
		relToCorpus, relErr := filepath.Rel(corpusRoot, path)
		if relErr != nil {
			return relErr
		}
		rel := censusCorpusRoot + "/" + filepath.ToSlash(relToCorpus)
		if !devdoc.IsProse(rel) {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// censusWalkCount is the non-vacuity diagnostic the gate prints and floors: how
// many markdown documents the corpus carries, so a broken walk cannot pass as a
// green run (a gate over zero files is vacuously green).
func censusWalkCount(root string) (int, error) {
	files, err := censusCorpusFiles(root)
	if err != nil {
		return 0, err
	}
	return len(files), nil
}
