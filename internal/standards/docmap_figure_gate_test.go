package standardgate

// docmap_figure_gate_test.go — README's doc-map table states POPULATION FIGURES
// about the documents it maps, and until this file nothing measured them.
//
// THE DRIFT, MEASURED. The table read "COMMANDS.md — 421 行", "CONFIG.md — 1471
// 行", "PROJECT.md — 43 条规则" while this tree carried 408 / 1469 / a numbering
// whose highest heading is 43 across 39 headings. Each figure was true at some
// earlier revision and then silently stopped being true: a figure in a table is
// a CENSUS value (Rule 38) with no generator and no reader, which is exactly the
// shape that rots — no build sees it, no test sees it, and a reader who acts on
// it ("421 lines, I'll find it in the second half") is misled by a number that
// LOOKS measured.
//
// WHAT THIS FILE DOES. It re-derives each figure from the tree with the command
// the table's own wording states (`wc -l`, a directory listing, a rule-heading
// count) and fails, naming the row, the stated figure, the measured value and
// the command that produced it, when the two disagree. Repair is one edit to
// README.md; the gate prescribes the number rather than a range, so a stale
// figure cannot be "fixed" by widening it into vagueness.
//
// WHY IT IS NOT WEAKER THAN "DELETE THE NUMBERS". Dropping the figures would
// also stop the drift, and it is the wrong repair: the count tells a reader
// whether the document is a page or a manual before they open it. Making the
// number CHECKED is the version where the reader keeps the information.
//
// ROW IDENTITY IS THE FILE NAME, NOT THE PROSE. The table is written in Chinese
// and is being translated; a gate keyed on a heading or on the unit word alone
// would be red one translation later for a reason that is not drift. So a row is
// found by the document it LINKS (`COMMANDS.md`, `CONFIG.md`, `man/`,
// `PROJECT.md`) and the figure is the first integer in that row followed by a
// unit word — both spellings are accepted (`行`/`lines`, `条`/`rules`, `份`/
// `files`). A row that carries no figure at all is a FAILURE, not a pass: a
// figure that was deleted while the unit word survived is the silent shape this
// gate exists to refuse (Rule 15).
//
// WHAT IT DOES NOT DO. It never writes README.md. The figure is prose a human
// authored; a gate that rewrote it would be the generator this rule (Rule 38)
// says should exist INSTEAD of a hand-typed number — and a generator for four
// cells would be more machinery than the four cells are worth. This is the
// reader; the edit stays manual and the failure message carries the value to
// type.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	// docMapReadmeFile is the document whose doc-map table is under test.
	docMapReadmeFile = "README.md"

	// docMapUnitAlternation is the set of unit words a figure may carry, in
	// BOTH languages the table has been written in. It is an alternation rather
	// than a single word because the table is being translated: a gate that
	// only read `行` would go red on a translation rather than on drift, which
	// is a false alarm with a real cost — the next author deletes the check.
	docMapUnitAlternation = `行|条|份|篇|页|lines?|rules?|files?|pages?|entries`

	// docMapRuleHeading matches a numbered rule heading. The number is the
	// rule's IDENTITY (it is what `Rule 27` means in a briefing, a task payload
	// or a commit message), so the heading count is not the claim the table
	// makes — `43 条规则` claims the numbering reaches 43, which survives the
	// fact that Rules 16–21 share one section heading.
	docMapRuleHeading = `(?m)^## Rule (\d+)`

	// docMapRuleRange matches a heading that carries SEVERAL rule numbers at
	// once (`## Machine-Checkable Gates (Rules 16–21)`). Those numbers have no
	// `## Rule N` heading of their own, so the contiguity arm below has to read
	// them or it would report a gap that is not there.
	docMapRuleRange = `\(Rules (\d+)\s*[–—−-]\s*(\d+)\)`

	// docMapRowFloor is the non-vacuity floor: the doc-map table maps four
	// surfaces, and a run that matched fewer than this judged a table it did
	// not find. Without the floor an empty README, a renamed table or a broken
	// row pattern would all pass — "I scanned nothing" reading as "clean",
	// which is the vacuum Rule 36 names as worse than no check at all.
	docMapRowFloor = 4
)

// docMapFigure is one row of the doc-map table: which document it maps, the
// figure it states, and the command that re-derives it. The command is a FIELD
// rather than a comment because it is what the failure prints — a reader who
// sees `stated 421, measured 408` still has to ask how to check it themselves,
// while `wc -l COMMANDS.md` answers that without a round trip.
type docMapFigure struct {
	link    string // the substring that identifies the row
	doc     string // the path the figure counts
	command string // the exact command that reproduces the figure
	measure func(root, doc string) (int, error)
}

// docMapFigures is the table's claim set. It is data, one row per mapped
// document that carries a figure, so the arms below count what they judged
// rather than trusting a pattern to have matched something.
func docMapFigures() []docMapFigure {
	return []docMapFigure{
		{
			link:    "COMMANDS.md",
			doc:     "COMMANDS.md",
			command: "wc -l COMMANDS.md",
			measure: docMapLineCount,
		},
		{
			link:    "CONFIG.md",
			doc:     "CONFIG.md",
			command: "wc -l CONFIG.md",
			measure: docMapLineCount,
		},
		{
			link:    "man/",
			doc:     "man",
			command: "ls man/*.md | wc -l",
			measure: docMapManPageCount,
		},
		{
			link:    "PROJECT.md",
			doc:     "PROJECT.md",
			command: "grep -oE '^## Rule [0-9]+' PROJECT.md | sed 's/.* //' | sort -un | wc -l",
			measure: docMapRuleSectionCount,
		},
	}
}

// docMapLineCount is `wc -l`: the number of newline characters in the file.
// Deliberately newline characters and not "lines a scanner yields" — the two
// differ by one on a file with no trailing newline, and the point of the gate is
// that the figure in the table is the figure the stated command prints.
func docMapLineCount(root, doc string) (int, error) {
	raw, err := os.ReadFile(filepath.Join(root, doc))
	if err != nil {
		return 0, err
	}
	return strings.Count(string(raw), "\n"), nil
}

// docMapManPageCount is `ls man/*.md | wc -l`.
func docMapManPageCount(root, doc string) (int, error) {
	entries, err := os.ReadDir(filepath.Join(root, doc))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		n++
	}
	return n, nil
}

// docMapRuleSectionCount is the COUNT of numbered rules: `43` was the largest
// rule NUMBER, which is NOT the same quantity and is the second way this row was
// wrong before it was measured.
//
// WHY THE COUNT AND NOT THE LARGEST NUMBER. PROJECT.md carries 39 headings that
// begin with `## Rule ` but only 37 DISTINCT rule numbers, and the numbering
// reaches 43, because Rules 16-21 are six SEPARATE
// rules (six distinct `## Machine-Checkable Gates` bullets, each with its own
// check) that share ONE heading, while `## Rule 30a` and `## Rule 41 的下半` are
// extra sections OF rules 30 and 41 rather than rules of their own (the
// contiguity arm below is what pins that, by requiring every number to be
// accounted for either by its own heading or by a `(Rules N–M)` range). A reader
// who takes `43 rules` and counts the
// sections finds 37 and concludes the table lies; a reader who takes `37 rules`
// and looks for Rule 43 finds it and draws the same conclusion. The figure must
// therefore be the unit the table's OWN wording names — rules the document
// states — and the wording is corrected to match this count, with the shared
// heading and the two sub-sections called out in the table beside it so the
// numbers reconcile without opening PROJECT.md.
func docMapRuleSectionCount(root, doc string) (int, error) {
	raw, err := os.ReadFile(filepath.Join(root, doc))
	if err != nil {
		return 0, err
	}
	nums, err := docMapRuleNumbers(string(raw))
	if err != nil {
		return 0, err
	}
	if len(nums) == 0 {
		return 0, fmt.Errorf("no `## Rule N` heading found — the rule count cannot be derived")
	}
	return len(nums), nil
}

// docMapRuleNumbers returns the SORTED, DEDUPED rule numbers carried by
// headings. Sorted means the largest is the last element.
func docMapRuleNumbers(body string) ([]int, error) {
	re := regexp.MustCompile(docMapRuleHeading)
	seen := map[int]bool{}
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("rule heading %q: %w", m[0], err)
		}
		seen[n] = true
	}
	nums := make([]int, 0, len(seen))
	for n := range seen {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	return nums, nil
}

// docMapRowFigure pulls the figure out of ONE table row: the first integer
// followed by a unit word. `0` with ok=false means the row carries no figure,
// which the caller treats as a failure rather than a pass — the table's own
// promise is that the number is there.
func docMapRowFigure(row string) (figure int, unit string, ok bool) {
	re := regexp.MustCompile(`(\d+)\s*(` + docMapUnitAlternation + `)`)
	m := re.FindStringSubmatch(row)
	if m == nil {
		return 0, "", false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, "", false
	}
	return n, m[2], true
}

// docMapRow returns the table line that maps `link`, or "" when the table does
// not map it. A row is recognised by its leading `|` and by containing the
// link, so a prose mention of the same filename outside the table is not
// mistaken for a figure-carrying row.
func docMapRow(body, link string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		if strings.Contains(trimmed, link) {
			return trimmed
		}
	}
	return ""
}

// docMapVerdict is the whole check as a pure function over the README text and
// the tree, so the planted arms below drive the SAME code the real-tree arm
// drives. A gate whose proof path is a second implementation proves nothing
// about the first.
func docMapVerdict(body, root string) (judged int, problems []string) {
	for _, f := range docMapFigures() {
		row := docMapRow(body, f.link)
		if row == "" {
			problems = append(problems, fmt.Sprintf(
				"the doc-map table no longer maps %s — the row was removed or renamed, so its figure is unmeasured", f.link))
			continue
		}
		judged++
		stated, unit, ok := docMapRowFigure(row)
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"the row for %s states no figure — a deleted number is drift that reads as a fix; "+
					"put the measured value back (%s)", f.link, f.command))
			continue
		}
		measured, err := f.measure(root, f.doc)
		if err != nil {
			problems = append(problems, fmt.Sprintf(
				"the row for %s cannot be checked: %v (command: %s)", f.link, err, f.command))
			continue
		}
		if stated != measured {
			problems = append(problems, fmt.Sprintf(
				"%s: the table states %d %s, the tree measures %d — run `%s` and write that value",
				f.link, stated, unit, measured, f.command))
		}
	}
	return judged, problems
}

// TestDocMapFiguresMatchTheTree is the real-tree arm, and the one the epic's
// acceptance names.
func TestDocMapFiguresMatchTheTree(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, docMapReadmeFile))
	if err != nil {
		t.Fatalf("read %s: %v", docMapReadmeFile, err)
	}
	judged, problems := docMapVerdict(string(raw), root)
	if judged < docMapRowFloor {
		t.Fatalf("%s: only %d doc-map row(s) judged (floor %d) — the table was not found, so this "+
			"green would cover nothing; the rows are located by the document each one links",
			docMapReadmeFile, judged, docMapRowFloor)
	}
	for _, p := range problems {
		t.Errorf("%s doc-map: %s", docMapReadmeFile, p)
	}
	t.Logf("%s: judged %d doc-map figure(s), %d mismatch(es)", docMapReadmeFile, judged, len(problems))
}

// TestDocMapFigureGateIsNotVacuous plants one defect per shape the verdict
// distinguishes — a stale figure, a missing figure, a missing row — and one
// clean control, and requires the SAME verdict function to tell them apart. A
// predicate that silently stopped matching would otherwise leave the real-tree
// arm green forever.
func TestDocMapFigureGateIsNotVacuous(t *testing.T) {
	root := repoRoot(t)
	waiting, err := docMapLineCount(root, "COMMANDS.md")
	if err != nil {
		t.Fatalf("measure COMMANDS.md: %v", err)
	}

	// Rows are built by ONE expression each and REUSED for the plants. Retyping
	// a row into a replacement target is how the first version of this arm went
	// quiet: the target read `—13` while the corpus read `— 13`, strings.Replace
	// matched nothing, and a case that was meant to remove a row removed
	// nothing while the assertion below still passed on the other three. A plant
	// that cannot be trusted to plant is a bound-not-binding check, so the
	// helper is the only place a row's spelling exists.
	commandsRow := func(n int) string {
		return "| **每个 CLI 动词的完整用法** | [`COMMANDS.md`](COMMANDS.md) — " + strconv.Itoa(n) + " 行，按动词列 |\n"
	}
	configRow := func(n int) string {
		return "| **每个配置项的默认值** | [`CONFIG.md`](CONFIG.md) — " + strconv.Itoa(n) + " 行 |\n"
	}
	manRow := func(n int) string {
		return "| **某个子系统的官方说明** | [`man/`](man/) — " + strconv.Itoa(n) + " 份 |\n"
	}
	projectRow := func(n int) string {
		return "| **工程约束** | [`PROJECT.md`](PROJECT.md) — " + strconv.Itoa(n) + " 条规则 |\n"
	}
	manPages := docMapMustMeasure(t, root, "man")
	clean := commandsRow(waiting) +
		configRow(docMapMustMeasure(t, root, "CONFIG.md")) +
		manRow(manPages) +
		projectRow(docMapMustMeasure(t, root, "PROJECT.md"))

	staleBody := commandsRow(waiting+1) +
		configRow(docMapMustMeasure(t, root, "CONFIG.md")) +
		manRow(manPages) +
		projectRow(docMapMustMeasure(t, root, "PROJECT.md"))
	noFigure := "| **每个 CLI 动词的完整用法** | [`COMMANDS.md`](COMMANDS.md) — 行，按动词列 |\n" +
		configRow(docMapMustMeasure(t, root, "CONFIG.md")) +
		manRow(manPages) +
		projectRow(docMapMustMeasure(t, root, "PROJECT.md"))
	rowGone := commandsRow(waiting) +
		configRow(docMapMustMeasure(t, root, "CONFIG.md")) +
		projectRow(docMapMustMeasure(t, root, "PROJECT.md"))
	translated := strings.Replace(
		strings.Replace(clean, " 行，按动词列", " lines, one per verb", 1),
		" 条规则", " rules", 1)

	cases := []struct {
		name        string
		body        string
		wantProblem bool
	}{
		{"clean control — every figure re-derived", clean, false},
		{"stale figure — the defect this gate exists for", staleBody, true},
		{"figure deleted, row kept", noFigure, true},
		{"row removed", rowGone, true},
		{"english units — a translated table is not drift", translated, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			judged, problems := docMapVerdict(tc.body, root)
			if judged < docMapRowFloor-1 { // one case removes a row on purpose
				t.Fatalf("judged only %d row(s) — the planted body was not parsed", judged)
			}
			if tc.wantProblem && len(problems) == 0 {
				t.Fatalf("the verdict reported no problem for a table that must be refused")
			}
			if !tc.wantProblem && len(problems) > 0 {
				t.Fatalf("the verdict refused a table that must pass: %v", problems)
			}
		})
	}
}

// docMapMustMeasure is the test-side reader for the fixture above; it fails the
// test rather than returning a zero that would make a planted "clean" body agree
// with a broken measurement.
func docMapMustMeasure(t *testing.T, root, doc string) int {
	t.Helper()
	for _, f := range docMapFigures() {
		if f.doc == doc {
			n, err := f.measure(root, f.doc)
			if err != nil {
				t.Fatalf("measure %s: %v", doc, err)
			}
			return n
		}
	}
	t.Fatalf("no doc-map figure declares %q — the fixture reads a document the gate does not measure", doc)
	return 0
}

// TestProjectRuleNumberingIsContiguous guards the OTHER half of the count
// claim. `37 rules` says how MANY rules there are; it says nothing about
// whether they are numbered 1..43 or 1..37 with twenty rules silently renumbered.
// Those are different failures with the same count, so the count arm above
// cannot see this one: a rule deleted and the remaining ones renumbered would
// leave the figure green while every `Rule 27` in a briefing, a task payload or
// a commit message had silently come to mean a different rule.
//
// The exemption is READ from the tree (a heading that carries a range, e.g.
// `(Rules 16-21)`), never hard-coded, so consolidating another group of rules
// into one section needs no edit here — while a range that stops covering the
// gap turns this red.
func TestProjectRuleNumberingIsContiguous(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "PROJECT.md"))
	if err != nil {
		t.Fatalf("read PROJECT.md: %v", err)
	}
	body := string(raw)
	nums, err := docMapRuleNumbers(body)
	if err != nil {
		t.Fatalf("parse rule headings: %v", err)
	}
	if len(nums) == 0 {
		t.Fatalf("PROJECT.md carries no `## Rule N` heading")
	}
	seen := map[int]bool{}
	for _, n := range nums {
		seen[n] = true
	}
	// A range heading covers the numbers it names; those have no heading of
	// their own by design (Rules 16-21 are six rules under one heading).
	rangeRe := regexp.MustCompile(docMapRuleRange)
	covered := 0
	for _, m := range rangeRe.FindAllStringSubmatch(body, -1) {
		lo, errLo := strconv.Atoi(m[1])
		hi, errHi := strconv.Atoi(m[2])
		if errLo != nil || errHi != nil || hi < lo {
			t.Fatalf("unreadable rule range %q", m[0])
		}
		for n := lo; n <= hi; n++ {
			if !seen[n] {
				seen[n] = true
				covered++
			}
		}
	}
	var gaps []string
	for n := 1; n <= nums[len(nums)-1]; n++ {
		if !seen[n] {
			gaps = append(gaps, strconv.Itoa(n))
		}
	}
	if len(gaps) > 0 {
		t.Fatalf("PROJECT.md's numbering reaches %d but %s carries no heading and no range "+
			"heading covers it — a hole in the numbering silently redefines what every "+
			"`Rule N` citation means, and the count arm above cannot see it",
			nums[len(nums)-1], strings.Join(gaps, ", "))
	}
	// The table states the COUNT; this arm is what makes that count reconciliable
	// with the largest number a reader may meet in a briefing.
	if len(nums)+covered != nums[len(nums)-1] {
		t.Fatalf("the numbering reaches %d, but %d headings + %d range-covered = %d rule(s) — "+
			"the table's figure and the numbering no longer describe the same document",
			nums[len(nums)-1], len(nums), covered, len(nums)+covered)
	}
	t.Logf("PROJECT.md: %d heading(s) + %d range-covered = %d rule(s), numbering contiguous 1..%d",
		len(nums), covered, len(nums)+covered, nums[len(nums)-1])
}

// -----------------------------------------------------------------------------
// THE FOOTNOTE IS THE SECOND COPY, AND IT WAS THE HALF NOBODY MEASURED.
//
// The table's own arms above re-derive each figure from the tree, so a drifted
// TABLE cell is caught. But the paragraph under the table restated the same
// figures in prose ("`408 lines` / `1469 lines` come from `wc -l <file>` …"),
// and that copy had no reader at all: when `be0f8fc7` repaired the COMMANDS.md
// row to the measured value, the footnote went on saying `408 lines`, one line
// below a table that said 421. Two copies of one census figure, one of them
// checked — the unchecked one is the one that rots, and it rots in the same
// paragraph that promises "every figure is measured".
//
// THE ROOT-CAUSE REPAIR IS STRUCTURAL, NOT A SECOND NUMBER. Re-typing 421 into
// the footnote would leave two copies to keep in step, and the next document
// move re-opens exactly this defect. So the footnote states NO figure: it names
// the re-derivation command per document and says the row is the only place the
// number is written down. One copy, one gate, no drift surface.
//
// THIS ARM IS WHAT MAKES THAT STRUCTURE BINDING. It fails when the footnote
// (a) cannot be found, (b) is empty, (c) has lost its re-derivation commands, or
// (d) restates any `<integer><unit>` figure at all — stale OR correct, because a
// correct copy is the one that becomes stale tomorrow. The planted arms below
// drive the SAME predicate, including the briefing's own defect (`408 lines`).
const (
	// docMapFootnoteOpening identifies the footnote paragraph; the table's
	// wording is its identity, as the rows' identity is the linked document.
	docMapFootnoteOpening = "**Every figure in these tables is measured"

	// docMapFootnoteCommandFloor is the non-vacuity floor for the re-derivation
	// commands: the footnote covers three different measurements, and a
	// paragraph that kept its "no numbers" promise by deleting the commands too
	// would pass a naive figure-free check while telling a reader nothing about
	// how to re-derive anything.
	docMapFootnoteCommandFloor = 2
)

// docMapFootnote returns the footnote block — the run of `>` lines that starts
// at the opening marker — or "" when the paragraph is gone. A removed footnote
// is a FAILURE downstream, not a pass: the paragraph's own promise ("every
// figure … names how to re-derive it") would then be made by nothing.
func docMapFootnote(body string) string {
	lines := strings.Split(body, "\n")
	start := -1
	for i, line := range lines {
		if strings.Contains(line, docMapFootnoteOpening) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	var b strings.Builder
	for i := start; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, ">") {
			break
		}
		b.WriteString(trimmed)
		b.WriteString("\n")
	}
	return b.String()
}

// docMapFootnoteFigures lists every `<integer><unit>` figure the footnote
// restates in PROSE. Backticked spans are stripped first: a command such as
// `grep -oE '^## Rule [0-9]+'` carries digits that are not a claim about the
// tree, and a heading spelling like `## Rule 30a` is an identifier, not a
// census value. What survives the strip is the prose, and prose is where a
// second copy of a measured figure lives.
func docMapFootnoteFigures(footnote string) []string {
	// ONLY a shell command inside backticks is excluded — NOT every backticked
	// span. The first version of this function stripped them all, and that was a
	// FALSE NEGATIVE on the exact defect this arm exists for: the drifted
	// footnote wrote its figures AS code spans ("`408 lines` / `1469 lines` come
	// from `wc -l <file>`"), so a blanket strip deleted the very copy it was
	// meant to catch and every planted case passed. Measured at the first run of
	// this arm: the `408 lines` plant reported "verdict passed" — the gate was
	// bound to itself, not to the paragraph.
	backtick := regexp.MustCompile("`[^`]*`")
	prose := backtick.ReplaceAllStringFunc(footnote, func(span string) string {
		inner := strings.Trim(span, "`")
		if docMapIsShellCommand(inner) {
			return " "
		}
		return " " + inner + " " // a backticked FIGURE is still prose to judge
	})
	// A RULE NUMBER IS AN IDENTIFIER, NOT A CENSUS CLAIM. The footnote has to
	// name which headings it is talking about ("`## Rule 30a` and
	// `## Rule 41 的下半` are sections", "Rules 16–21 share one heading") — a
	// `Rule N` citation is how every briefing, payload and commit message refers
	// to a constraint, so stripping those citations is what lets the scan below
	// be BRUTAL without being wrong. The range form is stripped first, or the
	// leading `Rules 16` would be consumed and its `–21` left behind as a bare
	// number.
	// Case-insensitive: the paragraph writes both `Rules 16–21` and the
	// lowercase `sections of rules 30 and 41`, and only the first spelling was
	// stripped on the first attempt — which left the second one behind as two
	// bare integers and turned a CITATION into a false failure.
	ident := regexp.MustCompile(`(?i)rules \d+\s*[–—−-]\s*\d+|rules? \d+[a-z]?`)
	prose = ident.ReplaceAllString(prose, " ")
	// WHAT REMAINS IS BRUTAL ON PURPOSE: ANY bare integer is a figure restated in
	// prose, with or without a unit word. The unit-word shape is the one that
	// drifted (a stale `408 lines`), but `reaches 43 while the number of sections
	// is 37` is the SAME defect in a different spelling — a number written down
	// where no arm re-derives it — and it was the shape still lurking in this
	// footnote after the first pass. A prose number in this paragraph has no
	// reader, so the paragraph is not allowed to carry one.
	re := regexp.MustCompile(`\b\d+\b`)
	var found []string
	for _, m := range re.FindAllString(prose, -1) {
		found = append(found, m)
	}
	return found
}

// docMapShellCommandWords are the leading words that make a backticked span a
// COMMAND rather than a figure. `grep -oE '^## Rule [0-9]+'` carries digits that
// are a pattern, not a claim about the tree, and `wc -l <file>` names a measure —
// neither is a second copy of a measured value, so both must be excluded from
// the figure scan. Anything else inside backticks is prose and IS judged.
const docMapShellCommandWords = "wc ls grep go sed sort awk find git make bash cat head"

// docMapIsShellCommand reports whether one backticked span is a command.
func docMapIsShellCommand(span string) bool {
	fields := strings.Fields(span)
	if len(fields) == 0 {
		return false
	}
	for _, w := range strings.Fields(docMapShellCommandWords) {
		if fields[0] == w {
			return true
		}
	}
	return false
}

// docMapFootnoteVerdict is the whole footnote check as a pure function, so the
// planted arms drive exactly the code the real-tree arm drives.
func docMapFootnoteVerdict(body string) (found bool, problems []string) {
	footnote := docMapFootnote(body)
	if footnote == "" {
		return false, []string{fmt.Sprintf(
			"the doc-map footnote (%q) is gone — the paragraph that tells a reader how to re-derive "+
				"each figure was removed, so the table's promise is now made by nothing",
			docMapFootnoteOpening)}
	}
	if restated := docMapFootnoteFigures(footnote); len(restated) > 0 {
		problems = append(problems, fmt.Sprintf(
			"the doc-map footnote restates %d figure(s) (%s) — the row is the ONLY place a measured "+
				"number belongs; a second copy in prose is a copy no gate re-derives, and a copy that "+
				"happens to be right today is the one that is wrong tomorrow",
			len(restated), strings.Join(restated, ", ")))
	}
	backticked := regexp.MustCompile("`[^`]*wc -l[^`]*`")
	if n := len(backticked.FindAllString(footnote, -1)); n < docMapFootnoteCommandFloor {
		problems = append(problems, fmt.Sprintf(
			"the doc-map footnote names %d re-derivation command(s) containing `wc -l` (floor %d) — "+
				"keeping the paragraph figure-free by deleting its commands leaves a reader with no way "+
				"to re-derive anything", n, docMapFootnoteCommandFloor))
	}
	if !strings.Contains(footnote, "go test") {
		problems = append(problems, "the doc-map footnote no longer names the guard that checks these figures")
	}
	return true, problems
}

// TestDocMapFootnoteStatesNoFigure is the real-tree arm: the footnote carries no
// second copy of any figure, and still says how to re-derive each one.
func TestDocMapFootnoteStatesNoFigure(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, docMapReadmeFile))
	if err != nil {
		t.Fatalf("read %s: %v", docMapReadmeFile, err)
	}
	found, problems := docMapFootnoteVerdict(string(raw))
	if !found {
		t.Fatalf("%s: %s", docMapReadmeFile, strings.Join(problems, "; "))
	}
	for _, p := range problems {
		t.Errorf("%s doc-map footnote: %s", docMapReadmeFile, p)
	}
	// The success sentence is printed only on a CLEAN read: logging it
	// unconditionally made a red run print "carries 0 figure(s)" one line under
	// the failure that had just found three.
	if len(problems) == 0 {
		t.Logf("%s: doc-map footnote carries 0 figure(s) and names its re-derivation commands",
			docMapReadmeFile)
	}
}

// TestDocMapFootnoteGateIsNotVacuous plants the defect this arm exists for — the
// briefing's own `408 lines`, a CORRECT restatement (which must also be refused),
// a footnote with its commands deleted, and a removed footnote — beside a clean
// control, and requires the SAME verdict to tell them apart. Without this arm a
// predicate that silently stopped matching the footnote would leave the
// real-tree arm green over a paragraph it never read.
func TestDocMapFootnoteGateIsNotVacuous(t *testing.T) {
	const opening = "> " + docMapFootnoteOpening + ", and each row names its command:\n"
	commands := "> the line counts come from `wc -l <file>`, the page count from " +
		"`ls man/*.md | wc -l`, guarded by `go test ./internal/standards/ -run 'DocMap'`.\n"
	clean := opening + commands

	commandsMeasured, err := docMapLineCount(repoRoot(t), "COMMANDS.md")
	if err != nil {
		t.Fatalf("measure COMMANDS.md: %v", err)
	}
	stated := func(n int) string {
		return opening + fmt.Sprintf("> `%d lines` come from `wc -l <file>`, the page count from ",
			n) + "`ls man/*.md | wc -l`, guarded by `go test ./internal/standards/ -run 'DocMap'`.\n"
	}

	// A `Rule N` citation is an IDENTIFIER, not a census claim: every briefing,
	// payload and commit message refers to a constraint that way, and a gate that
	// refused those would be red on correct prose. This control is what keeps the
	// scan below brutal without being wrong — and the case AFTER it shows the
	// same sentence going red once the number stops being attached to a rule.
	citation := opening + "> a heading that merely starts with `## Rule ` is not a rule of its own: " +
		"`## Rule 30a` and `## Rule 41 的下半` extend the rule they name, and Rules 16–21 share " +
		"one heading, so sections are fewer than the highest citation; counted by " +
		"`wc -l <file>` and `wc -l COMMANDS.md`, guarded by " +
		"`go test ./internal/standards/ -run 'DocMap'`.\n"
	bare := opening + "> the rule NUMBERING reaches 43 while the number of sections is 37; " +
		"counted by `wc -l <file>` and `wc -l COMMANDS.md`, guarded by " +
		"`go test ./internal/standards/ -run 'DocMap'`.\n"

	cases := []struct {
		name    string
		body    string
		wantBad bool
	}{
		{"clean control — no figure restated", clean, false},
		{"a `Rule N` citation is an identifier, not a figure", citation, false},
		{"a bare number with no unit is STILL a restated figure", bare, true},
		{"the measured value restated is STILL a second copy", stated(commandsMeasured), true},
		{"the briefing's defect — a stale `408 lines`", stated(408), true},
		{"figure-free but the commands were deleted too", opening + "> these figures are guarded.\n", true},
		{"footnote removed", "| table only |\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found, problems := docMapFootnoteVerdict(tc.body)
			if tc.wantBad && len(problems) == 0 {
				t.Fatalf("the verdict passed a footnote that must be refused (found=%v)", found)
			}
			if !tc.wantBad && len(problems) > 0 {
				t.Fatalf("the verdict refused a footnote that must pass: %v", problems)
			}
		})
	}
}
