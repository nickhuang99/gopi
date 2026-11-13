package taskreport

// runcheck_test.go — Rule 31's checked face (runcheck.go).
//
// The epic's acceptance is a BIDIRECTIONAL proof, and both directions are
// pinned against the real shapes:
//
//	RED    the pre-Rule-31 Verification section (Build:/Tests: only — the shape
//	       that shipped `gopi task sync --push-only --task-id T` reported done
//	       while it failed on its first real invocation) → the finding FIRES;
//	GREEN  a real `Ran:` line → silent; a report whose `## Not Done` states the
//	       limit → silent. The honesty case is the one a wrong implementation
//	       breaks first, so it is pinned in BOTH forms the author may use.

import (
	"strings"
	"testing"
)

// ranCheckPreRule31Body is the motivating shape: what the report said, verbatim
// in structure, on 2026-09-21 — the project's own template, before Rule 31
// added the last two lines.
const ranCheckPreRule31Body = `# Task Report: the manual push face

## Completed
- implemented --task-id on ` + "`gopi task sync --push-only`" + `

## Not Done
- (nothing)

## Verification
- Build: ` + "`go build ./...`" + `: OK
- Tests:  ` + "`make test-budget PKGS='./internal/tools'`" + `: 12/12 PASS
- Commit: cbed046b feat(sync): the manual push face
`

// ranCheckRanBody is the same report WITH the invocation Rule 31 requires.
const ranCheckRanBody = `# Task Report: the manual push face

## Completed
- implemented --task-id on ` + "`gopi task sync --push-only`" + `

## Not Done
- (nothing)

## Verification
- Build:  ` + "`go build ./...`" + `: OK
- Tests:  ` + "`go test ./internal/tools -run TestTaskSyncPushOnly -count=1`" + `: ok
- Ran:    ` + "`./bin/gopi task sync --push-only --task-id 01a0c2b1-...`" + `
- Result: push hp: exit status 1 (stderr: claim-remote: want 1..8 row(s), got 2181)
`

// ranCheckBlockerBody is the HONESTY case: the surface could not be executed and
// the report says why (Rule 31: "an honest 'I could not run this' is acceptable;
// a silent claim of completion is not").
const ranCheckBlockerBody = `# Task Report: the cross-host leg

## Completed
- wired the transport

## Not Done
- could not run the push against a peer: needs the ` + "`hp`" + ` host, unreachable from this sandbox

## Verification
- Build: ` + "`go build ./...`" + `: OK
- Tests: ` + "`go test ./internal/tools`" + `: ok
`

// ranCheckDeclaredBlockerBody states the same limit on the declared token.
const ranCheckDeclaredBlockerBody = `# Task Report: the cross-host leg

## Completed
- wired the transport

## Not Done
- (nothing)

## Verification
- Build: ` + "`go build ./...`" + `: OK
- Tests: ` + "`go test ./internal/tools`" + `: ok
- Blocked: needs the ` + "`hp`" + ` peer host, unreachable from this sandbox
`

// TestRunCheckFiresOnThePreRule31Shape is the RED direction: Build:/Tests: only,
// nothing stating a limit → the finding fires, names the heading it looked
// under, and reaches the record + the operator line.
func TestRunCheckFiresOnThePreRule31Shape(t *testing.T) {
	v := RunCheckVerdict(ranCheckPreRule31Body)
	if !v.Missing() {
		t.Fatalf("the pre-Rule-31 shape must FIRE (a report that never says the surface ran): %+v", v)
	}
	if v.Section != VerificationHeading() {
		t.Fatalf("the finding must name the section it adjudicated, got %q want %q", v.Section, VerificationHeading())
	}
	if v.RanLine {
		t.Errorf("no `Ran:` line is present, so RanLine must be false")
	}
	rec := RunCheckRecord(v)
	if !strings.HasPrefix(rec, RanCheckDigestPrefix) || !strings.Contains(rec, VerificationHeading()) {
		t.Errorf("record must carry the prefix and the section: %q", rec)
	}
	warn := RunCheckWarning("t-rule31", v)
	for _, want := range []string{RanCheckWarningLabel, "t-rule31", VerificationHeading(), RanLinePrefix} {
		if !strings.Contains(warn, want) {
			t.Errorf("warning must carry %q:\n%s", want, warn)
		}
	}
}

// TestRunCheckIsSilentOnARealRanLine is the GREEN direction: the line Rule 31
// asks for silences the check — including the shapes an author reaches for
// (`- Ran:` in the template, `* Ran:`, and an indented line).
func TestRunCheckIsSilentOnARealRanLine(t *testing.T) {
	for _, body := range []string{
		ranCheckRanBody,
		strings.Replace(ranCheckRanBody, "- Ran:", "* Ran:", 1),
		strings.Replace(ranCheckRanBody, "- Ran:", "+ Ran:", 1),
		strings.Replace(ranCheckRanBody, "- Ran:", "  Ran:", 1),
	} {
		v := RunCheckVerdict(body)
		if !v.RanLine || v.Missing() {
			t.Errorf("a real `Ran:` line must silence the check: %+v", v)
		}
		if RunCheckRecord(v) != "" || RunCheckWarning("t-x", v) != "" {
			t.Errorf("a satisfied verdict must render nothing")
		}
	}
}

// TestRunCheckIsSilentOnAStatedBlocker is the honesty clause in both accepted
// forms: the limit stated in `## Not Done`, and the declared `Blocked:` token.
// An implementation that flags these punishes Rule 31's own documented escape.
func TestRunCheckIsSilentOnAStatedBlocker(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"stated in Not Done", ranCheckBlockerBody},
		{"declared Blocked: token", ranCheckDeclaredBlockerBody},
	} {
		v := RunCheckVerdict(tc.body)
		if v.Missing() {
			t.Errorf("%s: an honest stated limit must NOT be flagged: %+v", tc.name, v)
		}
		if v.RanLine {
			t.Errorf("%s: no `Ran:` line is present", tc.name)
		}
		if strings.TrimSpace(v.Blocker) == "" {
			t.Errorf("%s: the stated limit must be recorded on the verdict", tc.name)
		}
	}
}

// TestRunCheckIgnoresAProseMentionOfRunning is the false-POSITIVE direction of
// clause 2, word for word the case the epic names: the word appearing inside a
// sentence ("I did not run this") must never satisfy the check by accident.
func TestRunCheckIgnoresAProseMentionOfRunning(t *testing.T) {
	body := "# Task Report: x\n\n## Completed\n- a\n\n## Not Done\n- (nothing)\n\n" +
		VerificationHeading() + "\n- Build: OK\n- Tests: 3/3 PASS\n" +
		"- i did not run this: the surface is only reachable from the peer host\n"
	v := RunCheckVerdict(body)
	if v.RanLine {
		t.Fatalf("a mid-sentence mention is not a `Ran:` line: %+v", v)
	}
	// The prose ALSO sits in Verification, not in `## Not Done` — where Rule 31
	// puts the limit — so it is not an accepted statement of a limit either.
	if !v.Missing() {
		t.Errorf("a prose mention states no limit the contract accepts: %+v", v)
	}
}

// TestRunCheckRejectsAnEmptyRanLine: an empty `Ran:` is the harness skeleton's
// hole, not a measurement. Letting it satisfy the check would be the same
// silent hole in a new costume (and would make the skeleton self-satisfying).
func TestRunCheckRejectsAnEmptyRanLine(t *testing.T) {
	for _, body := range []string{
		ranCheckPreRule31Body + "- Ran:\n",
		ranCheckPreRule31Body + "- Ran:   \n",
		ranCheckPreRule31Body + "- Ran: (not run)\n",
	} {
		if v := RunCheckVerdict(body); !v.Missing() {
			t.Errorf("an empty / non-committal `Ran:` must not satisfy Rule 31: %q", body)
		}
	}
}

// TestRunCheckNoOpsWithoutTheVerificationSection: the section's ABSENCE is
// MissingSections' finding. Re-deciding it here would double-report one defect
// and would let a report dodge this check by deleting the section — so the
// check must be silent, with an empty Section.
func TestRunCheckNoOpsWithoutTheVerificationSection(t *testing.T) {
	body := "# Task Report: x\n\n## Completed\n- a\n\n## Not Done\n- (nothing)\n\n## Files\n- a.go\n"
	v := RunCheckVerdict(body)
	if v.Section != "" || v.Missing() || v.RanLine {
		t.Fatalf("a missing Verification section is not this check's finding: %+v", v)
	}
	if RunCheckRecord(v) != "" || RunCheckWarning("t-x", v) != "" {
		t.Errorf("a no-op verdict must render nothing")
	}
}

// TestRunCheckNoOpsOnAnEmptyVerificationHeading: the epic's explicit
// requirement — when no heading is defined at all the check no-ops instead of
// panicking or flagging every report on earth. Pinned by emptying the section
// list through its own accessor, then restoring it.
func TestRunCheckNoOpsOnAnEmptyVerificationHeading(t *testing.T) {
	saved := RequiredSections
	RequiredSections = nil
	defer func() { RequiredSections = saved }()

	if got := VerificationHeading(); got != "" {
		t.Fatalf("precondition: an empty section list has no Verification heading, got %q", got)
	}
	// The motivating shape — no Ran: line, no blocker — must NOT fire when no
	// heading is defined: the check has no text to adjudicate.
	if v := RunCheckVerdict(ranCheckPreRule31Body); v.Missing() {
		t.Fatalf("with no contract heading the check must no-op, got %+v", v)
	}
	if got := NotDoneHeading(); got != "" {
		t.Fatalf("NotDoneHeading must be read from the same list, got %q", got)
	}
}

// TestRunCheckCountsAPlaceholderNotDoneAsNoLimit: "nothing was left undone" is
// the opposite of stating a limit, so an unqualified `## Not Done` must not
// exempt the report — for the ordinary author and for the harness skeleton
// alike (the skeleton's hole is not a reason).
func TestRunCheckCountsAPlaceholderNotDoneAsNoLimit(t *testing.T) {
	for _, filler := range []string{
		"- (nothing)", "- nothing", "- (none)", "- n/a", "- —", SkeletonPlaceholder,
	} {
		body := "# Task Report: x\n\n## Completed\n- a\n\n## Not Done\n" + filler + "\n\n" +
			VerificationHeading() + "\n- Build: OK\n- Tests: ok\n"
		if v := RunCheckVerdict(body); !v.Missing() {
			t.Errorf("a content-free Not Done entry (%q) states no limit: %+v", filler, v)
		}
	}
}

// TestSkeletonHolePrefixMatchesThePlaceholder pins the ONE string runcheck.go
// matches against SkeletonPlaceholder: if the placeholder's wording changes,
// the skeleton would start reading as a stated limit and the check would go
// silent on every harness-written report (Rule 13 — the match is anchored on
// the real value, not on a copy of it).
func TestSkeletonHolePrefixMatchesThePlaceholder(t *testing.T) {
	// The placeholder carries the marker the predicate strips before matching
	// (sectionPlaceholder emits `* ` + SkeletonPlaceholder), so the pin is on
	// the value the predicate actually compares.
	stripped := trimClaimBullet(strings.TrimSpace(SkeletonPlaceholder))
	if !strings.HasPrefix(stripped, skeletonHolePrefix) {
		t.Fatalf("the marker-stripped placeholder %q must open with skeletonHolePrefix %q",
			stripped, skeletonHolePrefix)
	}
}

// TestSkeletonDoesNotSatisfyTheRanCheck: the harness's own fallback states no
// invocation (it has none), so the check must record the gap on a skeleton
// rather than read the skeleton's own placeholder as a limit.
func TestSkeletonDoesNotSatisfyTheRanCheck(t *testing.T) {
	body := Skeleton("t-x", []string{FormatFact("status", "completed")})
	if !strings.Contains(body, RanLinePrefix) {
		t.Fatalf("the verification placeholder must NAME the tokens Rule 31 wants:\n%s", body)
	}
	// The skeleton runs nothing, so it states no invocation — the marker it
	// emits says so, and the gap must still be recorded (a skeleton must never
	// satisfy its own check).
	if v := RunCheckVerdict(body); !v.Missing() {
		t.Fatalf("the skeleton declares no invocation, so the gap must be recorded: %+v", v)
	}
}

// TestRunCheckAdjudicatesTheVerificationSectionOnly: the token must be read
// from the section Rule 31 puts it in — a `Ran:` line in another section is not
// the evidence the Verification contract asks for. (The heading is read through
// its accessor, so this also pins that the check follows the contract's list.)
func TestRunCheckAdjudicatesTheVerificationSectionOnly(t *testing.T) {
	body := "# Task Report: x\n\n## Completed\n- Ran: somewhere else entirely\n\n" +
		"## Not Done\n- (nothing)\n\n" + VerificationHeading() + "\n- Build: OK\n"
	if v := RunCheckVerdict(body); v.RanLine || !v.Missing() {
		t.Fatalf("only the Verification section's `Ran:` line satisfies the contract: %+v", v)
	}
}
