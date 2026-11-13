// cli_tools_maintenance_help_surface_failure_text_test.go — the guard for the
// SHAPE of a token-assertion failure (bug 01a13207, from task 01a131c8's
// correction).
//
// The defect this pins: g6AssertTokens used to print its expectation and the
// case's `note` as one unlabelled run-on line —
//
//	`gopi X`: none of ["…"] appeared in the output — note: <the author's description>\n<g6Head(out)>
//
// — so the FIRST two things a reader met were the CASE AUTHOR's words (the
// case's wantAny, and its note), while the only part the invoked binary produced
// was the next line. A reader (measured: the author of that bug row, who spent a
// whole turn chasing a string that was never printed) takes `%q` for the
// command's stdout and looks for it in the output, and cannot tell from the text
// which lines the binary actually wrote. The fix labels all three parts (see
// g6FailureText/g6Expectation*Fmt/g6AuthorNoteLabel/g6ActualOutputLabel in the
// sibling cases file). This test makes that a MACHINE criterion instead of an
// author's habit: it drives the real assertion helper against recordingTB and
// reads the message it produced.
package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

const (
	// g6CasesTestFileName is the file whose failure text is under guard.
	g6CasesTestFileName = "cli_tools_maintenance_help_surface_cases_test.go"
	// g6UnlabelledNoteForm is the banned shape: a note printed with no label
	// saying whose words they are. Its presence in the cases file is the
	// regression.
	g6UnlabelledNoteForm = "— note: "
	// The probe values below are DELIBERATELY distinct, so an assertion can tell
	// which part of the message each one landed in.
	g6ProbeAbsentToken  = "g6-probe-token-that-was-never-printed"
	g6ProbePresentToken = "g6-probe-token-that-is-in-the-output"
	g6ProbeAuthorNote   = "g6-probe-author-note-describing-the-case-not-the-stdout"
	g6ProbeOutputLine   = "g6-probe-the-line-the-invoked-binary-actually-printed"
	g6ProbeOutputSecond = "g6-probe-second-output-line"
)

// recordingTB implements just enough of testing.TB to OBSERVE g6AssertTokens'
// Errorf instead of failing the test that drives it (the same shape as
// internal/testutil's captureT). A nil embedded testing.TB is never called —
// g6AssertTokens only uses Helper and Errorf.
type recordingTB struct {
	testing.TB
	msgs []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Errorf(format string, args ...interface{}) {
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

// g6OnlyFailure drives one assertion and returns the single failure it must
// produce — a helper that said nothing, or said it twice, is a defect of the
// guard rather than a pass.
func g6OnlyFailure(t *testing.T, assert func(tb testing.TB)) string {
	t.Helper()
	rec := &recordingTB{}
	assert(rec)
	if len(rec.msgs) != 1 {
		t.Fatalf("the assertion must report exactly one failure, got %d: %q", len(rec.msgs), rec.msgs)
	}
	return rec.msgs[0]
}

// g6AfterLast returns the text following the LAST occurrence of label — the
// part of the message that label attributes to itself.
func g6AfterLast(t *testing.T, msg, label string) string {
	t.Helper()
	idx := strings.LastIndex(msg, label)
	if idx < 0 {
		t.Fatalf("the failure text does not carry the label %q at all:\n%s", label, msg)
	}
	return msg[idx+len(label):]
}

// g6ExpectationPart isolates the EXPECTATION segment: everything from the
// expectation label up to whichever attribution label follows it. A wanted token
// found HERE is the author's expectation; the same string under the output label
// is the command's stdout.
func g6ExpectationPart(t *testing.T, msg string) string {
	t.Helper()
	start := strings.Index(msg, g6ExpectationLabel)
	if start < 0 {
		t.Fatalf("the failure text does not carry the expectation label %q:\n%s", g6ExpectationLabel, msg)
	}
	rest := msg[start+len(g6ExpectationLabel):]
	for _, end := range []string{g6AuthorNoteLabel, g6NoAuthorNote} {
		if i := strings.Index(rest, end); i >= 0 {
			return rest[:i]
		}
	}
	t.Fatalf("the expectation part is not followed by a note label — the three parts are not separated:\n%s", msg)
	return ""
}

// g6AssertLabelsOnce requires each attribution label exactly once: a message
// naming its output twice cannot be read as "these three parts, in this order".
func g6AssertLabelsOnce(t *testing.T, msg string) {
	t.Helper()
	for _, label := range []string{g6ExpectationLabel, g6AuthorNoteLabel, g6ActualOutputLabel} {
		if n := strings.Count(msg, label); n != 1 {
			t.Errorf("the message must carry the label %q exactly once, found %d:\n%s", label, n, msg)
		}
	}
}

// TestG6FailureTextSeparatesExpectationNoteAndOutput is the guard for the OR arm
// (wantAny): the three parts are labelled and ordered, and the author's words are
// NOT under the output label.
func TestG6FailureTextSeparatesExpectationNoteAndOutput(t *testing.T) {
	out := g6ProbeOutputLine + "\n" + g6ProbeOutputSecond + "\n"

	msg := g6OnlyFailure(t, func(tb testing.TB) {
		g6AssertTokens(tb, []string{"tool", "probe"},
			out, "/g6-probe-home", []string{g6ProbeAbsentToken}, false, g6ProbeAuthorNote)
	})

	g6AssertLabelsOnce(t, msg)

	// The parts must appear in the order expectation → note → output, which is
	// what makes the message readable top-down as "wanted / described / observed".
	exp, note, actual := strings.Index(msg, g6ExpectationLabel), strings.Index(msg, g6AuthorNoteLabel), strings.Index(msg, g6ActualOutputLabel)
	if !(exp < note && note < actual) {
		t.Errorf("the parts must appear in the order expectation → note → output (got %d/%d/%d):\n%s", exp, note, actual, msg)
	}

	// (1) The expectation segment is the CASE AUTHOR's, and it must name the
	// token the case wanted — otherwise the failure says nothing about WHY it
	// failed.
	if !strings.Contains(g6ExpectationPart(t, msg), g6ProbeAbsentToken) {
		t.Errorf("the expectation must name the token the case wanted:\n%s", msg)
	}

	// (2) THE DEFECT ITSELF: nothing the binary did not print may sit under the
	// output label. A note or an expected token there reads as the command's
	// stdout — which is exactly the misreading this bug row was written from.
	tail := g6AfterLast(t, msg, g6ActualOutputLabel)
	if strings.Contains(tail, g6ProbeAuthorNote) {
		t.Errorf("the author's note is printed under the ACTUAL OUTPUT label, so it reads as the command's stdout:\n%s", msg)
	}
	if strings.Contains(tail, g6ProbeAbsentToken) {
		t.Errorf("the case's expectation is printed under the ACTUAL OUTPUT label, so it reads as the command's stdout:\n%s", msg)
	}

	// (3) And the output is there, so the reader can compare expected vs observed.
	if !strings.Contains(tail, g6ProbeOutputLine) {
		t.Errorf("the message must carry what the command actually printed:\n%s", msg)
	}
}

// TestG6FailureTextSeparatesTheAllArm covers wantAll (the AND direction), whose
// failure names the missing conjuncts: those are still the AUTHOR's expectation,
// never the command's output.
func TestG6FailureTextSeparatesTheAllArm(t *testing.T) {
	// The FIRST conjunct IS in the output (that is what makes it not-missing);
	// only the second is absent, so the failure must name exactly one token.
	out := g6ProbePresentToken + "\n" + g6ProbeOutputLine + "\n"

	msg := g6OnlyFailure(t, func(tb testing.TB) {
		g6AssertTokens(tb, []string{"tool", "probe"},
			out, "/g6-probe-home", []string{g6ProbePresentToken, g6ProbeAbsentToken}, true, g6ProbeAuthorNote)
	})

	g6AssertLabelsOnce(t, msg)

	tail := g6AfterLast(t, msg, g6ActualOutputLabel)
	if strings.Contains(tail, g6ProbeAbsentToken) || strings.Contains(tail, g6ProbeAuthorNote) {
		t.Errorf("the AND arm attributes the author's expectation/note to the command's output:\n%s", msg)
	}
	// "only the missing ones" is a property of the EXPECTATION segment, because
	// the satisfied token legitimately reappears as observed output.
	expectation := g6ExpectationPart(t, msg)
	if !strings.Contains(expectation, g6ProbeAbsentToken) {
		t.Errorf("the AND arm must name the missing conjunct %q in its expectation:\n%s", g6ProbeAbsentToken, msg)
	}
	if strings.Contains(expectation, g6ProbePresentToken) {
		t.Errorf("the AND arm must name only the MISSING conjunct, not %q:\n%s", g6ProbePresentToken, msg)
	}
	if !strings.Contains(tail, g6ProbeOutputLine) {
		t.Errorf("the message must carry what the command actually printed:\n%s", msg)
	}
}

// TestG6FailureTextSaysWhenACaseDeclaresNoNote: a silent omission would leave
// the reader guessing whether the note is empty or was simply not printed, so
// the absence is stated in words (Rule 15: no silent failure).
func TestG6FailureTextSaysWhenACaseDeclaresNoNote(t *testing.T) {
	msg := g6OnlyFailure(t, func(tb testing.TB) {
		g6AssertTokens(tb, []string{"tool", "probe"},
			g6ProbeOutputLine+"\n", "/g6-probe-home", []string{g6ProbeAbsentToken}, false, "")
	})
	if !strings.Contains(msg, g6NoAuthorNote) {
		t.Errorf("a case with no note must say so rather than print nothing:\n%s", msg)
	}
	if strings.Contains(msg, g6AuthorNoteLabel) {
		t.Errorf("a case with no note must not print the attribution label with nothing after it:\n%s", msg)
	}
}

// TestG6FailureTextIsTheOnlyWayTheCasesFilePrintsTheOutput is the SOURCE-level
// half of the guard: the unlabelled run-on form (`— note: <note>` followed by the
// output) may not come back to the cases file. It catches a re-introduction the
// runtime arms above cannot see — a NEW assertion helper written in the old
// shape.
func TestG6FailureTextIsTheOnlyWayTheCasesFilePrintsTheOutput(t *testing.T) {
	src, err := os.ReadFile(g6CasesTestFileName)
	if err != nil {
		t.Fatalf("the cases file must be readable from the package directory: %v", err)
	}
	if n := strings.Count(string(src), g6UnlabelledNoteForm); n != 0 {
		t.Errorf("%s carries %d occurrence(s) of the unlabelled %q form — a note printed with no attribution label reads as the command's stdout; render it through g6FailureText instead",
			g6CasesTestFileName, n, g6UnlabelledNoteForm)
	}
	if !strings.Contains(string(src), "g6FailureText(") {
		t.Errorf("%s no longer renders its probe failures through g6FailureText — the three parts would be unlabelled again", g6CasesTestFileName)
	}
}
