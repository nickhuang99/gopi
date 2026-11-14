package main

// task_ground_rule6c_trigger_test.go — PROJECT.md Rule 6c's CLONE-channel
// paragraph, pinned against the ONE source of the claim: the publishing clause
// the briefing actually renders (internal/tools/task_briefing_publish_clause.go).
//
// WHY A TEST FOR PROSE. This is the same reasoning as the Rule 6a deferral
// clause beside it: the paragraph's reader is a MODEL, the file ships as the
// child's project context, and the instruction it carries is a BEHAVIOURAL one
// on the delivery path — "when does the loop publish, and what counts as
// evidence that it did?". It was wrong in a way no other check could see:
// PROJECT.md and devdoc/rules-reference.md both said the loop "runs after every
// round", while the code says ONCE PER RUN from the END hook. Commit f3d257b25
// corrected the clause and its tests and touched neither rule file, so the two
// documents kept telling every clone-channel run something the machinery
// contradicts — and a run that reads "after every round" concludes from its own
// log (correctly, per that text) that a missing mid-run push means a broken
// delivery leg. Row 01a17921-fa90 did exactly that and was wrong.
//
// THREE ASSERTIONS:
//
//	1. the real trigger is stated — case-insensitively, the words the clause
//	   uses (ONCE PER RUN / END hook / no per-round push), so a re-wrap or a
//	   re-capitalization is not a failure but a rewrite IS;
//	2. the retired wording is GONE from both rule files — the defect was not a
//	   missing sentence, it was a false one, and a "fix" that adds the truth
//	   beside the error leaves both readings reachable;
//	3. the evidence sentence survives: what proves the leg RAN and what proves
//	   it did NOT, named by the machinery's own strings (`clone-channel: attempt
//	   …` / `clone channel: delivery delivered` / DeliveryDidNotLandReasonPref),
//	   so a future edit cannot keep the trigger and drop the half that stops a
//	   run reporting noise as a defect.
//
// The assertion is on the SHIPPED ARTIFACT (PROJECT.md is TRACKED and travels
// with every clone, Rule 6a's groundProjectMDPath reasoning applies unchanged).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

// rule6cRetiredWording is the claim this task REMOVED. It is spelled here, not
// derived, because the test's whole job is to notice it coming back.
const rule6cRetiredWording = "after every round"

// rule6cTierBPath is the Tier B copy of the same rule, relative to the repo root
// resolved by groundProjectMDPath's caller.
const rule6cTierBPath = "devdoc/rules-reference.md"

// TestRule6cStatesTheDeliveryLoopsRealTrigger pins assertion 1 and 2 on
// PROJECT.md and its Tier B copy.
func TestRule6cStatesTheDeliveryLoopsRealTrigger(t *testing.T) {
	root := filepath.Dir(groundProjectMDPath(t))
	for _, name := range []string{"PROJECT.md", rule6cTierBPath} {
		path := filepath.Join(root, filepath.FromSlash(name))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := groundNormWS(string(data))

		// ② The retired claim must be ABSENT. Checked first, so the failure a
		// regression produces is the one that names the defect.
		if strings.Contains(strings.ToLower(text), rule6cRetiredWording) {
			t.Errorf("%s must not claim the delivery loop runs %q — the channel publishes ONCE PER RUN from the END hook (task_briefing_publish_clause.go). The defect this test guards is exactly that sentence surviving beside the corrected one.", name, rule6cRetiredWording)
		}

		// ① The real trigger, in the words the RENDERED clause uses. The clause
		// is the ONE source (Rules 1b/13): a rule file that paraphrases it can
		// drift from it, and did.
		clause := groundNormWS(tools.TaskBriefingPublishClause("task/<id>"))
		if clause == "" {
			t.Fatalf("the publishing clause rendered empty for a branch name — this test cannot check the rule against its source")
		}
		for _, want := range []string{"ONCE PER RUN", "END hook", "no per-round push"} {
			if !strings.Contains(clause, want) {
				t.Fatalf("the clause (internal/tools/task_briefing_publish_clause.go) no longer carries %q — this test's premise changed; re-derive the rule text from the clause rather than deleting the assertion", want)
			}
			if !strings.Contains(text, want) {
				t.Errorf("%s must state %q — the clone channel's delivery loop gets exactly ONE turn, from the run's END hook, and a rule that does not say so invites a run to read a missing mid-run push as a broken leg", name, want)
			}
		}
		// The clause's own sentence names the ONE source; the rule points at it
		// so a future reader has somewhere authoritative to check against.
		if !strings.Contains(text, "internal/tools/task_briefing_publish_clause.go") {
			t.Errorf("%s must name internal/tools/task_briefing_publish_clause.go as the ONE source of the wording — the rule is a copy, and a copy without a pointer is what drifted", name)
		}
	}
}

// TestRule6cNamesWhatCountsAsEvidence pins assertion 3 — the half that turns a
// suspicion into a verdict. The identifiers are taken from the machinery (the
// ledger's reason prefix and the channel's own log lines), not typed as prose.
func TestRule6cNamesWhatCountsAsEvidence(t *testing.T) {
	root := filepath.Dir(groundProjectMDPath(t))
	for _, name := range []string{"PROJECT.md", rule6cTierBPath} {
		path := filepath.Join(root, filepath.FromSlash(name))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := groundNormWS(string(data))
		for _, want := range []string{
			// evidence that it RAN — the channel's own log vocabulary
			"clone-channel: attempt",
			"push accepted",
			"clone channel: delivery delivered",
			// evidence that it did NOT — the row's reason prefix, by its const
			// name (the constant governs the string; a rule may not out-type it)
			"DeliveryDidNotLandReasonPref",
			// the brake that bounds the attempt counter, by name rather than a
			// typed number (Rules 1/13/14)
			"devworkspace.MaxPushAttempts",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("%s must carry %q — the rule tells a run how to DECIDE whether the leg ran, and an evidence sentence missing its identifiers is an invitation to guess", name, want)
			}
		}
		// The ledger's reason prefix is the string the row actually carries; a
		// rule that names the const but misquotes the sentence sends the reader
		// to the wrong line.
		if prefix := strings.TrimSpace(tools.DeliveryDidNotLandReasonPref); prefix != "" {
			if !strings.Contains(text, groundNormWS(prefix)) {
				t.Errorf("%s must quote the row's non-delivery reason as the machinery spells it (%q; internal/tools/tasks.go's DeliveryDidNotLandReasonPref)", name, prefix)
			}
		}
	}
}
