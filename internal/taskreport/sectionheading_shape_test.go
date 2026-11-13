package taskreport

// sectionheading_shape_test.go — the heading predicate is STRUCTURAL: a section
// heading is a line that carries its heading text at COLUMN 0 of a line of its
// own. A wrapped continuation line that merely begins with a heading's text,
// after indentation, is prose — never a heading.
//
// WHY (the measured case, task 01a19466): sectionBody asked
// `strings.HasPrefix(strings.TrimSpace(line), heading)`, so the TRIM made a
// wrapped bullet inside `## Completed` — whose text opened with
// `## Verification` — indistinguishable from the real `## Verification` line
// below it. sectionBody therefore started at the continuation, read a few lines
// of the bullet, and stopped at the next heading it met; the report's three real
// `- Ran:` lines were never in the adjudicated body, so
// RunCheckVerdict(...).RanLine was false and `ran-line-missing` was recorded
// against a report that DID state its invocations. The same trim+prefix
// semantics is `hasSection`'s (MissingSections), where it fails the other way
// round: an indented continuation SATISFIES a section whose real heading is
// absent, so a missing section is reported present.
//
// THE PROOF IS BIDIRECTIONAL AND IN ONE TABLE, on purpose: a predicate that
// always fires passes every red arm, and one that never fires passes every green
// arm. The pre-fix predicate is reproduced verbatim (`sectionBodyPrefixTrim`)
// beside the shipped one, so the red direction stays measurable after the fix:
// it pins WHAT the bug was, and the green direction pins that the fix reaches
// the real body rather than merely refusing more.

import (
	"strings"
	"testing"
)

// continuationIndent is the wrap indent a markdown list item's second line
// carries (the shape the motivating report used: two spaces under a `- ` bullet).
const continuationIndent = "  "

// continuationOf returns the heading's spelling as a WRAPPED CONTINUATION: the
// indent, then the heading text, then prose that continues the sentence. Trimmed
// of the indent it still OPENS with the heading — which is exactly the input
// that fooled the trimmed-prefix predicate.
func continuationOf(heading string) string {
	return continuationIndent + heading + " — see the run below"
}

// verificationFixture returns the motivating report shape: a wrapped bullet
// inside `## Completed` that opens with the Verification heading, and below it
// the real Verification section carrying THREE `Ran:` lines (Rule 31 wants one;
// three fakes the report's measured redundancy) plus the heading that follows
// it, whose body carries verificationTailMarker.
func verificationFixture() string {
	heading := VerificationHeading()
	return "# Task Report: the heading/continuation collision\n\n" +
		"## Completed\n" +
		"- shipped the change; the invocation and its output are quoted\n" +
		continuationOf(heading) + "\n" +
		"\n" +
		NotDoneHeading() + "\n" +
		"- (nothing)\n" +
		"\n" +
		"## " + verificationTailSectionName + "\n" +
		"- " + verificationTailMarker + "\n" +
		"\n" +
		heading + "\n" +
		"- Build: `go build ./...`: OK\n" +
		"- Ran: `go test ./internal/taskreport/ -run TestSectionBody -count=1`\n" +
		"- Result: PASS\n" +
		"- Ran: `./bin/gopi --version`\n" +
		"- Result: revision matches HEAD\n" +
		"- Ran: `git status --porcelain`\n" +
		"- Result: prints nothing\n"
}

const (
	// verificationTailSectionName is a level-2 heading AFTER the Verification
	// section; its body must not leak into the Verification body (the END side
	// of the same predicate).
	verificationTailSectionName = "Files"
	// verificationTailMarker is the sentence under that heading.
	verificationTailMarker = "the artifact declaration belongs to its own section"
)

// sectionBodyPrefixTrim is the PRE-FIX predicate, kept verbatim so the red arm
// can be re-measured after the fix: the heading is matched against the TRIMMED
// line, i.e. indentation is discarded before the comparison.
func sectionBodyPrefixTrim(body, heading string) (string, string) {
	if heading == "" {
		return "", ""
	}
	lines := strings.Split(body, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), heading) {
			start = i
			break
		}
	}
	if start < 0 {
		return "", ""
	}
	var b strings.Builder
	for _, line := range lines[start+1:] {
		if strings.HasPrefix(strings.TrimSpace(line), SectionHeadingPrefix) {
			break
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return heading, b.String()
}

// hasSectionPrefixTrim is the PRE-FIX MissingSections predicate (the same
// trim+prefix read), kept for the sibling red arm.
func hasSectionPrefixTrim(body, heading string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), heading) {
			return true
		}
	}
	return false
}

// TestSectionBodyIgnoresAnIndentedHeadingShapedContinuation is the epic's
// regression: a wrapped continuation that opens with `## Verification` must not
// be mistaken for the section heading, so the section's real `Ran:` lines are
// what the run-check adjudicates.
func TestSectionBodyIgnoresAnIndentedHeadingShapedContinuation(t *testing.T) {
	body := verificationFixture()
	heading := VerificationHeading()

	// RED ARM (the measured defect, reproduced): the pre-fix predicate stops at
	// the continuation, so the adjudicated body is a fragment of `## Completed`
	// and carries no `Ran:` line at all.
	predFound, predBody := sectionBodyPrefixTrim(body, heading)
	if predFound != heading {
		t.Fatalf("red arm must reproduce the collision: pre-fix predicate found %q", predFound)
	}
	if got := ranValue(predBody); got != "" {
		t.Fatalf("red arm must reproduce the defect: pre-fix body must carry no `%s` line, got %q\nbody:\n%s",
			RanLinePrefix, got, predBody)
	}
	if strings.Contains(predBody, "go build ./...") {
		t.Fatalf("red arm must reproduce the truncation: pre-fix body must not reach the real section:\n%s", predBody)
	}

	// GREEN ARM: the shipped predicate finds the real heading, reads its whole
	// body, sees the `Ran:` line, and stops at the next heading.
	found, section := sectionBody(body, heading)
	if found != heading {
		t.Fatalf("the continuation made the section unreadable: sectionBody found %q", found)
	}
	if !strings.Contains(section, "go build ./...") {
		t.Errorf("sectionBody must read the real section body, got:\n%s", section)
	}
	if got := ranValue(section); got == "" {
		t.Errorf("sectionBody must reach the `%s` line in the real section body:\n%s", RanLinePrefix, section)
	}
	v := RunCheckVerdict(body)
	if !v.RanLine || v.Missing() {
		t.Fatalf("RunCheckVerdict must see the `%s` line through the continuation: %+v", RanLinePrefix, v)
	}
	if v.Section != heading {
		t.Errorf("RunCheckVerdict.Section = %q, want %q", v.Section, heading)
	}
}

// TestSectionBodyEndsAtAColumnZeroHeadingOnly is the END side of the same
// predicate: a section stops at the next line that IS a heading at column 0. An
// INDENTED continuation that opens with a level-2 prefix is body text — it never
// ends the section.
func TestSectionBodyEndsAtAColumnZeroHeadingOnly(t *testing.T) {
	heading := VerificationHeading()
	tailHeading := SectionHeadingPrefix + verificationTailSectionName
	body := "# Task Report: the end side\n\n" +
		heading + "\n" +
		"- Build: `go build ./...`: OK\n" +
		continuationOf(SectionHeadingPrefix) + "\n" +
		tailHeading + "\n" +
		"- " + verificationTailMarker + "\n"

	_, section := sectionBody(body, heading)
	if strings.Contains(section, verificationTailMarker) {
		t.Errorf("an indented `%s` continuation must not end the section, got:\n%s",
			SectionHeadingPrefix, section)
	}

	// The control — a real heading at column 0 DOES end it, so the arm above is
	// measuring the predicate and not a section that simply never ends.
	ended := strings.Replace(body, continuationOf(SectionHeadingPrefix)+"\n", "", 1)
	if _, after := sectionBody(ended, heading); strings.Contains(after, verificationTailMarker) {
		t.Errorf("a real %q heading must end the section, got:\n%s", tailHeading, after)
	}
}

// TestMissingSectionsIgnoresAnIndentedHeadingShapedContinuation is the sibling
// of the same predicate: hasSection reads the line the same way, so an indented
// continuation must neither SATISFY a section (the failing direction measured on
// the real corpus: a report whose only `## Not Done` occurrence was a wrapped
// continuation) nor, once the real heading is present, make it disappear.
func TestMissingSectionsIgnoresAnIndentedHeadingShapedContinuation(t *testing.T) {
	for _, s := range RequiredSections {
		onlyContinuation := "# Task Report: x\n\n- a bullet\n" + continuationOf(s.Heading) + "\n"
		if !hasSectionPrefixTrim(onlyContinuation, s.Heading) {
			t.Fatalf("red arm must reproduce the defect: pre-fix hasSection said %q is absent", s.Heading)
		}
		if !missingContains(MissingSections(onlyContinuation), s.Heading) {
			t.Errorf("an indented continuation satisfied section %q — a missing section is reported present", s.Heading)
		}

		realAndContinuation := "# Task Report: x\n\n" + s.Heading + "\n- content\n\n" +
			continuationOf(s.Heading) + "\n"
		if missingContains(MissingSections(realAndContinuation), s.Heading) {
			t.Errorf("a real %q heading at column 0 must satisfy its section even beside a continuation", s.Heading)
		}
	}
}

// missingContains reports whether heading is reported missing.
func missingContains(missing []string, heading string) bool {
	for _, v := range missing {
		if v == heading {
			return true
		}
	}
	return false
}
