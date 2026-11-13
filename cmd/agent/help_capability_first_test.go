package main

// help_capability_first_test.go — the SHAPE gate for the two service verbs'
// help pages (worker, merger), from the operator complaint that `gopi worker
// --help` "is not a help, but a devdoc": it opened with a five-step loop
// protocol ("One loop round (the contract)": CAS claims, must-succeed push,
// db-sync pull-then-push) and spent a whole section on a privilege question
// whose answer is "no privilege is involved".
//
// WHAT THIS PINS (the whole point of the epic): a user typing `<verb> -h`
// asks WHAT IT CAN DO. So the page must open with the invocations, then the
// options and environment, and hold the why/how detail — the loop mechanics,
// the cgroup measurement, the rationale for the verb's position in the CLI —
// for the END, if it is kept at all.
//
// The predicate is shared by the case-table and the red proof below, so a
// green run cannot be a blind one (Rule 10.4: apply the check to the text
// that ACTUALLY SHIPPED before trusting it).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/mansource"
)

const (
	// hcfDetailHeading is the tail section the restructured pages end with:
	// everything a reader needs only AFTER deciding to use the verb.
	hcfDetailHeading = "Notes:"
	// hcfInvocationPrefix marks an invocation line ("  gopi worker …"): the
	// capability the page must lead with.
	hcfInvocationPrefix = "  gopi "
	// hcfProseTerminators end a sentence on every published page — a period, a
	// colon introducing a block, an em dash or an arrow used as the gloss
	// separator (Rule 1: a meaningful literal gets a name).
	hcfProseTerminators = ".:—→…"

	// hcfLabelSuffix ends a section label — the character that both ends a
	// sentence in this corpus and closes a bare label, named once so
	// hcfBareLabelLine cannot drift from hcfProseTerminators.
	hcfLabelSuffix = ":"

	// hcfSectionMaxLine is the RETIRED position bound: the window clause ① used
	// to admit ("the first hcfSectionMaxLine+1 lines"). It survives only
	// because the red proof below pins WHY it was retired — under it, one blank
	// line flipped a page's verdict (TestClauseOnePositionIsOneLineWide). No
	// shipped judgement reads it any more; hcfLegacyInvocationInWindow is the
	// fixture that keeps the comparison runnable.
	hcfSectionMaxLine = 3
)

// hcfHeaderInvocationPrefix marks the SECOND spelling of an invocation line:
// the synopsis sitting ON the `Usage:` header ("Usage: gopi chat …", the form
// `chat` and `version` print). It is a var, not a const, because it is BUILT
// from the man reader's own `Usage:` token (Rule 13/14: one definition,
// referenced — never typed a second time here), and that token is a package
// variable in internal/mansource.
var hcfHeaderInvocationPrefix = mansource.UsageHeader + " " + gopiProgramWord + " "

// hcfForbiddenFragments are the SHIPPED shapes of the defect — each was on the
// page before this epic, and each is a fragment a returning reader could
// re-add ("the protocol matters to operators"). The gate spells them so a
// failure names what came back.
var hcfForbiddenFragments = []struct {
	fragment string
	why      string
}{
	{"One loop round (the contract)", "the five-step protocol dump that made the help read as a devdoc — the loop belongs in the tail, as prose, or not at all"},
	{"Privileges (--daemon)", "a whole section answering a question the user did not ask, about a route that does not work"},
	{"cgroup", "the measurement internals: the operator-facing answer is one sentence ('needs NO privilege'), not the experiment"},
	{"TOP-LEVEL BECAUSE IT IS A SERVICE", "the CLI-design rationale (why the verb sits at top level) — a maintenance note, not user help"},
	{"the must-succeed push", "sync-protocol vocabulary in the help page"},
	{"db-sync", "sync-protocol vocabulary in the help page"},
	{"Detached mode (--daemon)", "the detached-mode internals section (lock-acquisition order, exit-status proof) — a --daemon user needs the answer, not the mechanism"},
	{"setsid", "process-mechanics vocabulary (session leadership) in user help. The entry stays (exemption was considered at master 9ce8e159 and REFUSED): nothing a `-h` reader does needs the identifier, and the user-facing FACT is 'a flag detaches nothing — the loop is your own foreground process', which cmd/agent/main.go's Notes now say. 06438272 dropped only `setsid` from that clause and kept the detaching PROGRAM ('nohup') — the same class, so the class is now closed: the page names no launcher at all. This is NOT Rule 25's mention-to-deny case: that applies to PRIVILEGE text, which the pages must be able to name in order to DENY it; no page needs to name a detach call to state that it does not detach. Reword the page; never widen this list"},
	{"LOCK_EX|LOCK_NB", "the flock internals (lock mode spelled out) in user help — 'only one merger runs, the second is refused' is the user-facing fact"},
}

// hcfCapabilityFirstOffenses is the predicate: it returns every reason a help
// page reads as a devdoc rather than as help. An empty result is a pass.
func hcfCapabilityFirstOffenses(text string) []string {
	var out []string

	// ① The invocations come first, before any PROSE: what it can do, before
	// any explanation of how it does it.
	//
	// ORDER, NOT POSITION — decided here on measurement, not on taste (task
	// 01a12bb2-3772-72b7-bb2a-a5a3e8fecf13, converging the follow-up row filed
	// by 01a12b9f). Clause ① used to be a POSITION test: an invocation within
	// the first hcfSectionMaxLine+1 lines, i.e. `index <= 3`. Measured over the
	// 12 published pages (artifacts/clause1-census.txt, artifacts/
	// pages-12-baseline.txt), that test had two defects at once:
	//
	//   (a) its verdict was decided by a BLANK LINE. Four pages that satisfied
	//       it (`config`, `memory`, `task`, `web`) put their first invocation on
	//       0-based line 3 — index hcfSectionMaxLine EXACTLY, zero headroom; one
	//       cosmetic blank line flips each of them red. `merge`, `notify` and
	//       `worker` had exactly one line of headroom. A criterion whose answer
	//       a blank line decides measures LAYOUT ACCIDENTS, not the property it
	//       names. TestClauseOnePositionIsOneLineWide pins that fragility.
	//   (b) it MISSED the pages that breached the property. It read only the
	//       "  gopi " row spelling, so `chat` and `version` — whose synopsis
	//       sits ON the `Usage:` header ("Usage: gopi chat …") — were reported
	//       as printing no invocation at all, and the five real breaches
	//       (chat, session, tool, ui_state, version) were indistinguishable from
	//       a reader that did not know the page's form.
	//
	// The property the clause NAMES is a relation between two things on the
	// page: the reader must meet an invocation before meeting an explanation.
	// So that is what it now tests — hcfFirstInvocationLine (either invocation
	// spelling, the whole page, no window) VERSUS hcfFirstProseLine (the first
	// prose paragraph, narrowly defined). Both are position-free in the sense
	// that matters: inserting a blank line cannot change the verdict, because
	// neither side counts lines from the top.
	//
	// SCOPE — NOW THE WHOLE PUBLISHED SET. The old clause ran on
	// `workerUsageHead`/`mergeUsageHead` only, and that scope was a property of
	// the epic that created it (9e24d228fd, "service help is
	// capability-first, not a devdoc"), not of the predicate. The re-specified
	// clause is evaluated on all 12 published pages by TestClauseOneServes
	// EveryPublishedPage and counted by help_usage_form_census_test.go's
	// TestUsageFormCensusCarriesClauseOne, both through the SAME two functions
	// — a census that re-implemented the scan would drift from the gate it
	// reports on.
	if first, ok := hcfFirstInvocationLine(text); !ok {
		out = append(out, "the page prints no invocation — a user asking -h must be told what the verb can do")
	} else if prose := hcfFirstProseLine(text); prose >= 0 && prose < first {
		out = append(out, fmt.Sprintf("opens with prose: the first paragraph starts on line %d, before the first invocation on line %d — a user asking -h must see what it can do before any explanation", prose+1, first+1))
	}

	// ② The shipped defect fragments are gone for good.
	for _, bad := range hcfForbiddenFragments {
		if strings.Contains(text, bad.fragment) {
			out = append(out, "carries "+bad.why+" ("+bad.fragment+")")
		}
	}

	// ③ The detail is LAST: nothing but the tail section may follow it. A
	// section header is an unindented line ending in ':'.
	tail := strings.Index(text, "\n"+hcfDetailHeading+"\n")
	if tail < 0 {
		out = append(out, "has no "+hcfDetailHeading+" tail section — the why/how detail must be collected at the end, not scattered through the page")
		return out
	}
	for _, line := range strings.Split(text[tail+1:], "\n")[1:] {
		if line == "" || strings.HasPrefix(line, " ") || !strings.HasSuffix(line, ":") {
			continue
		}
		out = append(out, "puts the section "+strings.TrimSpace(line)+" after the detail tail — the page must not return to instructions once the notes started")
	}
	return out
}

// hcfFirstInvocationLine returns the 0-based index of the page's FIRST line
// carrying an invocation — either spelling: an indented row (hcfInvocationPrefix)
// or a header-line synopsis (hcfHeaderInvocationPrefix, the `Usage: gopi chat …`
// form). ok is false when the page prints no invocation at all.
//
// This is the ONE implementation of clause ①'s judgement: the census over the 12
// published `-h` pages (help_usage_form_census_test.go) runs the SAME function.
// It is a function (not an inlined loop) because a census re-implementing the
// scan could drift from the gate it claims to report on, and then the number a
// reader quotes would be about the census rather than about the gate.
func hcfFirstInvocationLine(text string) (index int, ok bool) {
	for i, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, hcfInvocationPrefix) || strings.HasPrefix(line, hcfHeaderInvocationPrefix) {
			return i, true
		}
	}
	return 0, false
}

// hcfFirstProseLine returns the 0-based index of the page's first PROSE
// paragraph, and -1 when it has none. The definition is deliberately NARROW:
// one line qualifies only when ALL of these hold — it is not blank, it is NOT
// indented (a usage row and its continuation paragraphs are), it carries a
// sentence terminator ('.', ':', '—', '→' or '…'), and it is NOT the first
// non-blank line of the page (the title — a page is allowed to introduce
// itself, and every published page does).
//
// WHY NARROW (the trap this definition exists to avoid): a WIDE definition
// ("the first non-empty line that is not an invocation") flags TWO shipped
// pages that are already capability-first, and the flag is an artefact of the
// page's own layout, not of its shape — `merge` puts its first invocation on
// line 3 under the title and its invocations block has NO blank line
// separating the rows from the trailing note on line 31 (measured 2026-10-12,
// artifacts/order-predicate-probe.txt); `worker` is the same. A rule that
// refused those pages would be measuring the presence of a blank line. The
// narrow form asks the question clause ① actually asks — is there a PARAGRAPH
// of explanation before the reader learns what the verb can do — and every
// published page answers it unambiguously.
func hcfFirstProseLine(text string) int {
	for i, line := range strings.Split(text, "\n") {
		if !hcfProseLine(line) {
			continue
		}
		if i == hcfFirstNonBlankLine(text) {
			// The title: a page introduces itself before it shows anything.
			continue
		}
		return i
	}
	return -1
}

// hcfProseLine reports whether ONE line is a prose line in the narrow sense
// hcfFirstProseLine documents: unindented, non-blank, not an invocation in
// either spelling, not a bare section label, and terminated like a sentence.
//
// The two exclusions beyond "unindented and terminated" are what keep this a
// statement about CONTENT rather than about layout (measured 2026-10-12,
// artifacts/order-predicate-probe.txt): without them, the page's own `Usage:`
// header — the line that INTRODUCES the invocation block and therefore cannot
// precede it in any meaningful sense — counted as the prose that preceded it,
// and five shipped pages (config, memory, notify, task, web) were reported as
// opening with an explanation they do not have. A bare label is not a
// paragraph; a synopsis is an invocation, not prose.
func hcfProseLine(line string) bool {
	if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
		return false
	}
	if strings.HasPrefix(line, hcfInvocationPrefix) || strings.HasPrefix(line, hcfHeaderInvocationPrefix) {
		return false
	}
	if hcfBareLabelLine(line) {
		return false
	}
	return strings.ContainsAny(line, hcfProseTerminators)
}

// hcfBareLabelLine reports whether a line is a section label and nothing else —
// unindented, its content ending at the colon (`Usage:`, `Flags:`, `Options:`,
// `Subcommands:`, `Notes:`). The same shape internal/mansource bounds its
// usage-block scan with, spelled here because the man reader's predicate is
// unexported and this package's help gates own their own definitions.
func hcfBareLabelLine(line string) bool {
	return strings.HasSuffix(strings.TrimRight(line, " \t"), hcfLabelSuffix)
}

// hcfFirstNonBlankLine is the 0-based index of the page's first non-blank line
// (its title), or -1 for an empty page.
func hcfFirstNonBlankLine(text string) int {
	for i, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			return i
		}
	}
	return -1
}

// hcfProseTerminators' value lives in the const block at the top of this file
// (Rule 1: meaningful literals are named once, where the reader looks first).

// TestClauseOnePositionIsOneLineWide is the RED PROOF for the fragility that
// RETIRED clause ①'s position form (Rule 10.4/10.5: the smallest sample that
// separates the two behaviours). The page below is the shape clause ① asks
// for; the second sample is the same page with ONE blank line inserted after
// the title — no text added, removed or reworded — and the LEGACY window test
// flips from satisfied to violated while the SHIPPED order test does not move.
// That is the whole argument, as a measurement rather than as a paragraph.
func TestClauseOnePositionIsOneLineWide(t *testing.T) {
	const title = "GoPi thing — do the useful thing, then exit."
	const invocations = "  gopi thing        do it\n  gopi thing --once one pass, then exit 0"
	// The tail is part of the fixture so clause ③ stays satisfied and the ONLY
	// thing that can differ between the two samples below is the blank line.
	// (A fixture missing `Notes:` would trip ③ and make the comparison say
	// nothing about ①.)
	const tail = "Options:\n  --once             one pass, then exit 0\n\nNotes:\n  Privileges: none needed.\n"

	onBoundary := title + "\n\n\n" + invocations + "\n\n" + tail
	if idx, ok := hcfFirstInvocationLine(onBoundary); !ok || idx != hcfSectionMaxLine {
		t.Fatalf("the control page must sit exactly ON the retired boundary (index %d): got index=%d ok=%v", hcfSectionMaxLine, idx, ok)
	}
	if got := hcfCapabilityFirstOffenses(onBoundary); len(got) != 0 {
		t.Fatalf("the control page must pass every clause — it is the shape clause ① asks for: %v", got)
	}
	if !hcfLegacyInvocationInWindow(onBoundary) {
		t.Fatal("the legacy window must accept the control page, or the comparison below would be about something else")
	}

	oneBlankLine := title + "\n\n\n\n" + invocations + "\n\n" + tail
	if hcfLegacyInvocationInWindow(oneBlankLine) {
		t.Fatalf("the LEGACY position test accepted the one-blank-line page — this fixture no longer demonstrates the fragility it exists to pin")
	}
	// The measurement the retirement rests on: the page that the legacy window
	// now refuses is the SAME page, one blank line apart, and the shipped order
	// predicate — which asks a question about the page rather than about its
	// layout — reports it clean.
	if got := hcfCapabilityFirstOffenses(oneBlankLine); len(got) != 0 {
		t.Fatalf("the ORDER predicate flipped on a blank line — the repair did not take: %v", got)
	}
}

// hcfLegacyInvocationInWindow is the RETIRED position form, kept as a local
// fixture. No shipped judgement calls it: it exists so the red proof above can
// measure, rather than assert in prose, that a one-line-wide window decides a
// page's verdict on layout. Deleting it would leave that claim untestable —
// which is the same failure as a gate whose only evidence is a comment.
func hcfLegacyInvocationInWindow(text string) bool {
	for i, line := range strings.Split(text, "\n") {
		if i > hcfSectionMaxLine {
			break
		}
		if strings.HasPrefix(line, hcfInvocationPrefix) {
			return true
		}
	}
	return false
}

// TestClauseOneIsAnOrderRelation is the RED PROOF for the SHIPPED judgement,
// in both directions and at the boundary that separates the two predicates
// (Rule 10.4/10.5: the smallest sample that separates the behaviours).
//
// The four samples are all the SAME page modulo one line, so nothing but the
// order of prose and invocation can explain a difference in verdict.
func TestClauseOneIsAnOrderRelation(t *testing.T) {
	const tail = "Options:\n  --once             one pass, then exit 0\n\nNotes:\n  Privileges: none needed.\n"
	const invocation = "  gopi thing        do it"
	const prose = "A paragraph explaining how the thing works."

	// (control) invocations first: the shape clause ① asks for — and the tail
	// keeps clause ③ satisfied so ONLY ① is under test.
	control := "GoPi thing — do the useful thing.\n\n" + invocation + "\n\n" + prose + "\n\n" + tail
	if got := hcfCapabilityFirstOffenses(control); len(got) != 0 {
		t.Fatalf("(control) the capability-first page must pass every clause: %v", got)
	}

	// (a) the SAME page with the paragraph moved above the invocation: the
	// defect clause ① names, and the clause has to say so.
	proseFirst := "GoPi thing — do the useful thing.\n\n" + prose + "\n\n" + invocation + "\n\n" + tail
	if got := strings.Join(hcfCapabilityFirstOffenses(proseFirst), "\n"); !strings.Contains(got, "opens with prose") {
		t.Fatalf("(a) clause ① did not fire on a page whose paragraph precedes its invocation: %v", got)
	}

	// (b) THE BOUNDARY, prose side: the paragraph and the invocation are
	// adjacent — no blank line between them. An order relation still holds, so
	// the clause must pass; a test that counted lines or demanded a gap is the
	// fragility this row exists to remove.
	tight := "GoPi thing — do the useful thing.\n\n" + invocation + "\n" + prose + "\n\n" + tail
	if got := hcfCapabilityFirstOffenses(tight); len(got) != 0 {
		t.Fatalf("(b) an invocation IMMEDIATELY followed by prose must pass — the relation is order, not spacing: %v", got)
	}

	// (b′) THE BOUNDARY, prose side, the other way: adjacent, prose first. The
	// same adjacency must NOT save a page that explains itself first.
	tightBad := "GoPi thing — do the useful thing.\n\n" + prose + "\n" + invocation + "\n\n" + tail
	if got := strings.Join(hcfCapabilityFirstOffenses(tightBad), "\n"); !strings.Contains(got, "opens with prose") {
		t.Fatalf("(b′) an adjacent paragraph BEFORE the invocation must still be refused: %v", got)
	}

	// (c) blank-line insensitivity: the defect page with a blank line inserted
	// in front of the paragraph keeps its verdict, and so does the control —
	// the property that distinguishes this clause from the position test it
	// replaced.
	proseFirstSpaced := "GoPi thing — do the useful thing.\n\n\n\n" + prose + "\n\n" + invocation + "\n\n" + tail
	if got := strings.Join(hcfCapabilityFirstOffenses(proseFirstSpaced), "\n"); !strings.Contains(got, "opens with prose") {
		t.Fatalf("(c) a blank line changed the verdict of a defect page — the clause is still measuring layout: %v", got)
	}
	controlSpaced := "GoPi thing — do the useful thing.\n\n\n\n" + invocation + "\n\n" + prose + "\n\n" + tail
	if got := hcfCapabilityFirstOffenses(controlSpaced); len(got) != 0 {
		t.Fatalf("(c) a blank line changed the verdict of a conforming page — the clause is still measuring layout: %v", got)
	}

	// (d) a page that prints NO invocation at all is refused, and for its own
	// reason (the second arm of ①) rather than as a side effect of the ordering.
	none := "GoPi thing — do the useful thing.\n\n" + prose + "\n\n" + tail
	if got := strings.Join(hcfCapabilityFirstOffenses(none), "\n"); !strings.Contains(got, "prints no invocation") {
		t.Fatalf("(d) a page with no invocation must be refused as such: %v", got)
	}

	// (e) the SECOND invocation spelling is recognized: a synopsis sitting ON
	// the `Usage:` header (the form `chat` and `version` print) counts as an
	// invocation, so removing it from the scan — the defect the old clause's
	// reader had — would make this page look like the (d) case.
	headerLine := "Usage: gopi thing [--once]\n" + prose + "\n\n" + tail
	if got := hcfCapabilityFirstOffenses(headerLine); len(got) != 0 {
		t.Fatalf("(e) the `Usage: gopi …` spelling must count as an invocation: %v", got)
	}
	headerLineBad := "GoPi thing — do the useful thing.\n\n" + prose + "\n\nUsage: gopi thing [--once]\n\n" + tail
	if got := strings.Join(hcfCapabilityFirstOffenses(headerLineBad), "\n"); !strings.Contains(got, "opens with prose") {
		t.Fatalf("(e) prose before a header-line synopsis must be refused: %v", got)
	}
}

// TestClauseOneServesEveryPublishedPage is clause ① as a GATE over the whole
// published set — the scope widening this row was filed for. It walks the SAME
// list the census and the man set walk (publishedVerbHelpPages) through the
// SAME real-binary entry point (helpPage), so the list has one source and no
// second copy can drift.
//
// It is the gate the census reports on: TestUsageFormCensusCarriesClauseOne
// prints the same judgement page by page and asserts the two sides agree. A
// page added to the set is measured here the day it appears.
func TestClauseOneServesEveryPublishedPage(t *testing.T) {
	if len(publishedVerbHelpPages) == 0 {
		t.Fatal("the published set is empty — a walk over nothing would pass every clause")
	}
	for _, verb := range publishedVerbHelpPages {
		verb := verb
		t.Run(verb, func(t *testing.T) {
			help := helpPage(t, []string{verb})
			first, ok := hcfFirstInvocationLine(help)
			if !ok {
				t.Fatalf("`gopi %s -h` prints no invocation line in either spelling — a user asking -h must be told what the verb can do", verb)
			}
			prose := hcfFirstProseLine(help)
			if prose >= 0 && prose < first {
				t.Fatalf("`gopi %s -h` opens with prose: the first paragraph is line %d, the first invocation line %d — a user asking -h must see what it can do before any explanation", verb, prose+1, first+1)
			}
			t.Logf("clause1 OK  %-8s invocation=%d prose=%s", verb, first, hcfLineOrNone(prose))
		})
	}
}

// hcfLineOrNone renders a 0-based line index for the log, "-" for "the page
// has no such line" (a reader of the census wants the absence visible, not a
// -1 to interpret).
func hcfLineOrNone(index int) string {
	if index < 0 {
		return "-"
	}
	return fmt.Sprintf("%d", index)
}

// TestServiceHelpIsCapabilityFirst is the gate itself, over the REAL constants
// the binary prints.
func TestServiceHelpIsCapabilityFirst(t *testing.T) {
	for _, head := range []struct {
		name string
		text string
	}{
		{"workerUsageHead", workerUsageHead},
		// mergerUsageHead left this set with the retired daemon (2026-09-19),
		// and `mergeUsageHead` left it with the one-shot face it documented
		// (epic merge-verb-removal, item 3, 2026-11-01): the page went with the
		// parser whose flag constants filled its table. The surviving merge
		// surface is the family atom, whose page is taskMergeUsageHead in
		// cmd/agent/cli_tasks_merger_help.go.
	} {
		for _, offense := range hcfCapabilityFirstOffenses(head.text) {
			t.Errorf("%s: %s", head.name, offense)
		}
	}
}

// TestServiceHelpDetailStaysAtTheEnd pins the worker's loop contract to the
// TAIL: the facts a `-h` reader needs must survive (the epic moves detail, it
// does not delete it), but they belong after the options — not in the opening
// paragraph the operator has to read past.
func TestServiceHelpDetailStaysAtTheEnd(t *testing.T) {
	tail := strings.Index(workerUsageHead, "\n"+hcfDetailHeading+"\n")
	if tail < 0 {
		t.Fatalf("workerUsageHead has no %s section", hcfDetailHeading)
	}
	head, detail := workerUsageHead[:tail], workerUsageHead[tail:]
	if !strings.Contains(detail, "claims one pending task") {
		t.Errorf("the loop contract must be described in the tail section:\n%s", detail)
	}
	if strings.Contains(head, "claims one pending task") {
		t.Error("the loop contract is back in the opening half of the help page")
	}
	// The privilege answer is kept — as ONE sentence, in the tail (the
	// daemon_scope_no_privilege gate asserts the wording; this asserts the
	// placement and the restraint).
	if !strings.Contains(detail, "needs NO privilege") {
		t.Errorf("the tail must answer the privilege question:\n%s", detail)
	}
	if strings.Contains(head, "privilege") {
		t.Error("the privilege question must not occupy the instructions half of the page")
	}
}

// TestCapabilityFirstPredicateIsNotBlind is the RED PROOF: the predicate is
// applied to the text that actually shipped — verbatim, not paraphrased — and
// must flag it. A hand-typed "shipped" sample proves only that the check is
// awake; a sample that quietly drifts from the real page proves nothing about
// the defect the epic reports. So both fixtures are the byte-exact pre-epic
// pages (testdata/worker_help_prefix.txt is `gopi worker -h` as the operator
// saw it, testdata/merger_help_prefix.txt its merger sibling) — every entry of
// the forbidden list is covered by at least one of them, and the sweep below
// fails loudly if a fixture stops covering its case.
func TestCapabilityFirstPredicateIsNotBlind(t *testing.T) {
	pages := make(map[string]string)
	for _, name := range []string{"worker_help_prefix.txt", "merger_help_prefix.txt"} {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatalf("read the shipped pre-fix help page %s: %v", name, err)
		}
		text := string(b)
		if !strings.HasPrefix(text, "GoPi ") || len(text) < 1000 {
			t.Fatalf("testdata/%s is not the shipped pre-fix page (it must open with the verb title and be a full usage block)", name)
		}
		pages[name] = text
		if offenses := hcfCapabilityFirstOffenses(text); len(offenses) == 0 {
			t.Fatalf("the predicate failed to flag the shipped devdoc-shaped help in %s — a green sweep would be blind", name)
		}
	}
	// Every shipped fragment must be covered by a fixture AND flagged by the
	// predicate: a case silently dropped from the fixtures is a hole in the
	// red proof.
	for _, bad := range hcfForbiddenFragments {
		covered := ""
		for name, text := range pages {
			if strings.Contains(text, bad.fragment) {
				covered = name
				break
			}
		}
		if covered == "" {
			t.Errorf("no pre-fix fixture carries %q — the red proof lost the case it exists for", bad.fragment)
			continue
		}
		if !strings.Contains(strings.Join(hcfCapabilityFirstOffenses(pages[covered]), "\n"), bad.fragment) {
			t.Errorf("the predicate let %q through on %s — the gate would be blind to it", bad.fragment, covered)
		}
	}
	// ③ must bite on its own: a page that opens with prose and only later
	// lists invocations is a devdoc even when every fragment above is absent.
	noInvocation := `GoPi thing — a service.

Some prose the user has to read past to learn anything.
Another explanatory paragraph.

Usage:
  gopi thing        # the actual capability
`
	if got := hcfCapabilityFirstOffenses(noInvocation); len(got) == 0 {
		t.Error("the predicate accepted a page whose invocations follow the prose")
	}
	// …and the other direction: a page in the SHAPE this epic asks for must
	// pass cleanly, or the gate forbids the very structure it exists for.
	good := `GoPi thing — do the useful thing, then exit.

  gopi thing           do it in the foreground
  gopi thing --once    one pass, then exit 0

Options:
  --once             single pass, then exit 0

Notes:
  Privileges: none needed.
`
	if got := hcfCapabilityFirstOffenses(good); len(got) != 0 {
		t.Errorf("the predicate rejected a capability-first page: %v", got)
	}
}
