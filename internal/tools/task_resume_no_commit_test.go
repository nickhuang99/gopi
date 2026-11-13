package tools

// task_resume_no_commit_test.go — the resume-zero-commit criterion, driven
// through the PRODUCTION door (ResumeNoCommitVerdict), never a re-implementation
// of it.
//
// THE DEFECT THESE ARMS PIN (epic resume-zero-commit; row 01a19d78): a
// `gopi worker --resume <failed-row>` that produced NO commit closed its row
// `completed`, on the channel's true measurement ("nothing to deliver" /
// "already on the integration branch"). For an ordinary run that reading is
// correct; for a RESUME it claims the failure was repaired when the failure was
// not touched at all.
//
// RED BEFORE THE FIX, and the recipe is one edit: make ResumeNoCommitVerdict
// return (false, "") unconditionally and every arm below except the CONTROL ones
// fails — the CONTROL arms are the half that must stay green in both directions,
// because the fix they guard against over-reaching is rows
// 01a18244/01a1907d (`completed` for a run asked to produce nothing).

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/devworkspace"
)

const resumeNoCommitTestRow = "01a19d78-4733-7c54-a20a-be52db1ce8b0"

// TestResumeNoCommitVerdictRefusesCompletion is the arm the epic exists for: a
// DECLARED resume whose delivery leg measured no commit of its own must be told
// NOT to write `completed`, and the sentence must say what was measured.
func TestResumeNoCommitVerdictRefusesCompletion(t *testing.T) {
	channelReason := devworkspace.CloneChannelPrefix +
		"ALREADY ON THE INTEGRATION BRANCH — carries no commit to deliver: " +
		"`rev-list --count HEAD --not origin/master` answered 0 against origin/master"

	for _, tc := range []struct {
		name   string
		arm    string
		reason string
	}{
		{"inherited delivery", "inherited", channelReason},
		{"nothing to deliver", "none", devworkspace.CloneChannelReason("nothing to deliver — the ground carries no commit to deliver: `git rev-list --count HEAD --not origin/master` answered 0")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refused, reason := ResumeNoCommitVerdict("session-abc", resumeNoCommitTestRow, tc.reason, true)
			if !refused {
				t.Fatalf("a RESUMED run with no commit of its own must not close `completed` — the verdict allowed it (arm %q)", tc.arm)
			}
			if !strings.Contains(reason, ResumeNoCommitToken) {
				t.Fatalf("the reason must carry the ONE spelling %q so an operator scanning `task list` and the row's own reason read the same words:\n%s",
					ResumeNoCommitToken, reason)
			}
			if !strings.Contains(reason, resumeNoCommitTestRow) {
				t.Fatalf("the reason must NAME the row it is about:\n%s", reason)
			}
			// Rule 14: the channel's own sentence is carried VERBATIM, never
			// re-worded by this layer — a reader must be able to match the row's
			// reason against the run's own log line.
			body := devworkspace.CloneChannelBody(tc.reason)
			if !strings.Contains(reason, body) {
				t.Fatalf("the delivery leg's own measurement must ride the reason verbatim\nwant substring: %q\ngot: %s", body, reason)
			}
			// The prefix is composed ONCE, by the channel's own composer.
			if n := strings.Count(reason, devworkspace.CloneChannelPrefix); n != 1 {
				t.Fatalf("`%s` must appear exactly once (got %d): %s", devworkspace.CloneChannelPrefix, n, reason)
			}
		})
	}
}

// TestResumeNoCommitVerdictLeavesOrdinaryRunsAlone is the CONTROL half, and it
// is the reason the fix can land at all. Every case here must answer (false, "")
// — a criterion that fires on an ordinary run would turn rows 01a18244 /
// 01a1907d's legitimate `completed` into failures, which is the regression this
// arm exists to prevent.
func TestResumeNoCommitVerdictLeavesOrdinaryRunsAlone(t *testing.T) {
	reason := devworkspace.CloneChannelReason("nothing to deliver — the ground carries no commit to deliver")
	for _, tc := range []struct {
		name             string
		resumedFrom      string
		producedNoCommit bool
	}{
		{"an ORDINARY run with an empty ground keeps its conclusion (row 01a18244)", "", true},
		{"a run that produced a commit", "", false},
		{"a resumed run that DID produce a commit — the push landed", "session-abc", false},
		{"whitespace is not a declaration", "   ", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refused, out := ResumeNoCommitVerdict(tc.resumedFrom, resumeNoCommitTestRow, reason, tc.producedNoCommit)
			if refused || out != "" {
				t.Fatalf("this run must keep the status its own measurement chose, but the criterion fired: reason=%q", out)
			}
		})
	}
}

// TestResumeNoCommitVerdictNeverCarriesAnEmptyReason: a terminal row may not
// wear a fact-free reason (the rule taskDefaultReason and
// nothingToDeliverDeclaredPrefix enforce elsewhere). A delivery leg that
// answered with no sentence still yields a reason a reader can act on.
func TestResumeNoCommitVerdictNeverCarriesAnEmptyReason(t *testing.T) {
	for _, blank := range []string{"", "   "} {
		refused, reason := ResumeNoCommitVerdict("session-abc", "", blank, true)
		if !refused {
			t.Fatalf("blank measurement still means no commit was produced — the criterion must fire")
		}
		if strings.TrimSpace(devworkspace.CloneChannelBody(reason)) == "" {
			t.Fatalf("an empty measurement must still produce a reason with content, got %q", reason)
		}
		if !strings.Contains(reason, ResumeNoCommitToken) {
			t.Fatalf("the token must survive the empty-measurement path: %s", reason)
		}
	}
}

// TestAlreadyDeliveredIsNotResumeNoCommit pins the ONE exclusion in the caller's
// classification, at the level this file can prove: the criterion is about
// producing NO commit, and an `already delivered` arm means this run's OWN push
// moved the integration branch — i.e. it DID produce one. The caller composes the
// boolean, so this arm asserts the pairing that composition must never get wrong.
func TestAlreadyDeliveredIsNotResumeNoCommit(t *testing.T) {
	// The states the close-out maps to producedNoCommit — see the call site in
	// internal/agent/taskregister.go: NothingToDeliver || InheritedDelivery.
	producedNoCommit := map[string]bool{
		"NothingToDeliver":    true,
		"InheritedDelivery":   true,
		"AlreadyDelivered":    false,
		"Delivered":           false,
		"Blocked/undelivered": false,
	}
	for arm, want := range producedNoCommit {
		refused, _ := ResumeNoCommitVerdict("session-abc", resumeNoCommitTestRow, "clone channel: measured", want)
		if refused != want {
			t.Fatalf("arm %s: producedNoCommit=%v must yield refused=%v, got %v", arm, want, want, refused)
		}
	}
}

// TestResumeNoCommitHintIsTheResumePointer: the row that is refused `completed`
// must still tell an operator how to pick the work back up. The digest arm at
// the call site appends autoTaskResumeHintFormat; this asserts the token it is
// composed beside is the SAME conclusion, so the two sentences on one row cannot
// describe different outcomes.
func TestResumeNoCommitHintIsTheResumePointer(t *testing.T) {
	_, reason := ResumeNoCommitVerdict("session-abc", resumeNoCommitTestRow, "clone channel: measured", true)
	if !strings.Contains(reason, "not `completed`") {
		t.Fatalf("the reason must say the status was refused, not merely that a measurement was taken:\n%s", reason)
	}
	if !strings.Contains(strings.ToLower(reason), "resume") {
		t.Fatalf("a reader of the row must learn that the resume is what did not repair anything:\n%s", reason)
	}
}
