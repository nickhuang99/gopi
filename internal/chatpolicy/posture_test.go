package chatpolicy

import "testing"

// THE POSTURE'S FOUR CASES: yolo allows EVERYTHING (with evidence a force-push
// is allowed, which is the whole point), the default is on-request, the value is
// FROZEN, and each accepted spelling resolves to the one canonical decision.

// TestPostureYoloAllowsEveryCommandIncludingDestroyers is the EVIDENCE case: a
// yolo posture answers allow to the commands an earlier hardcoded table refused,
// and it does so with NO branch — there is no verb or path in this package to
// exempt them.
func TestPostureYoloAllowsEveryCommandIncludingDestroyers(t *testing.T) {
	yolo := NewPosture(DecisionAllow, "test")
	if !yolo.AllowsEverything() {
		t.Fatalf("a DecisionAllow posture does not report AllowsEverything (%+v)", yolo)
	}
	pol := table(t, "forbidden *") // the MOST restrictive table there is
	for _, cmd := range [][]string{
		{"git", "push", "--force"},
		{"git", "reset", "--hard"},
		{"git", "clean", "-fdx"},
		{"rm", "-rf", "/"},
		{"git", "push", "--force", "--no-verify", "origin", "main"},
	} {
		m := yolo.Resolve(pol, cmd)
		if !m.Matched {
			t.Errorf("%v: Matched = false under yolo — yolo answers everything", cmd)
		}
		if m.Decision != DecisionAllow {
			t.Errorf("%v: Decision = %v under yolo, want allow (no exception branch exists)", cmd, m.Decision)
		}
	}
	// And it matches a table-less run too.
	if m := yolo.Resolve(nil, []string{"git", "push", "--force"}); m.Decision != DecisionAllow {
		t.Errorf("yolo with no table = %v, want allow", m.Decision)
	}
}

// TestPostureZeroValueAsksNeverAllows is the SAFETY property of the type: an
// unresolved posture asks. Go's zero Decision would have been Allow, which is the
// silent widening this type exists to prevent.
func TestPostureZeroValueAsksNeverAllows(t *testing.T) {
	var zero Posture
	if got := zero.Decision(); got != DecisionPrompt {
		t.Errorf("zero Posture Decision = %v, want prompt (ON-REQUEST, never a silent allow)", got)
	}
	if zero.AllowsEverything() {
		t.Error("the zero Posture reports AllowsEverything — an unresolved posture must not run anything")
	}
	if got := zero.Source(); got != "default" {
		t.Errorf("zero Posture Source = %q, want %q", got, "default")
	}
}

// TestPostureIsFrozen: the posture is a VALUE. Every method returns a copy, so a
// note added to one copy cannot change the resolved one — there is no setter on
// the type at all.
func TestPostureIsFrozen(t *testing.T) {
	orig := NewPosture(DecisionForbidden, "settings.json \"chat_policy\"")
	withNote := orig.WithNote("a remark")
	if withNote.Note() == "" {
		t.Fatal("WithNote did not attach the note")
	}
	if orig.Note() != "" {
		t.Error("WithNote MUTATED the original posture — the value must be frozen")
	}
	if withNote.Decision() != orig.Decision() || withNote.Source() != orig.Source() {
		t.Error("WithNote changed the decision or the source")
	}
	// A posture built by PostureNamed is equally immutable in the sense that
	// matters: its answer cannot be revised in place.
	p, ok := PostureNamed("forbidden", "env")
	if !ok {
		t.Fatal("PostureNamed refused a documented spelling")
	}
	_ = p.WithNote("x")
	if p.Decision() != DecisionForbidden {
		t.Error("a copy's note changed the resolved decision")
	}
}

// TestPostureSpellingsResolveToTheCanonicalThree pins the accepted vocabulary and
// its aliases, and proves an unknown word is REFUSED (never read as the default).
func TestPostureSpellingsResolveToTheCanonicalThree(t *testing.T) {
	cases := map[string]Decision{
		"allow":      DecisionAllow,
		"yolo":       DecisionAllow,
		"auto":       DecisionAllow,
		"on-request": DecisionPrompt,
		"ask":        DecisionPrompt,
		"prompt":     DecisionPrompt,
		"forbidden":  DecisionForbidden,
		"deny":       DecisionForbidden,
		"never":      DecisionForbidden,
		// The strictest spelling: a line the table is silent about is refused, not
		// asked. It maps onto the SAME three-valued decision as `forbidden` (there is
		// no fourth behaviour), so no call site can branch on it.
		"unless-trusted": DecisionForbidden,
	}
	for word, want := range cases {
		p, ok := PostureNamed(word, "test")
		if !ok {
			t.Errorf("PostureNamed(%q) refused a documented spelling", word)
			continue
		}
		if p.Decision() != want {
			t.Errorf("PostureNamed(%q).Decision = %v, want %v", word, p.Decision(), want)
		}
		// The STARTUP LINE always prints the canonical word, never the alias, so
		// two runs that mean the same thing log the same bytes.
		if got := p.String(); got != want.String() {
			t.Errorf("PostureNamed(%q).String = %q, want the canonical %q", word, got, want.String())
		}
	}
	for _, bad := range []string{"", "ture", "YOLO!", "on_request", "allways"} {
		if _, ok := PostureNamed(bad, "test"); ok {
			t.Errorf("PostureNamed(%q) accepted an unknown word — a typo must be REFUSED, not read as the default", bad)
		}
	}
	// Every word PostureWords advertises must resolve (one table, Rule 1/13).
	for _, w := range PostureWords() {
		if _, ok := PostureNamed(w, "test"); !ok {
			t.Errorf("PostureWords advertises %q but PostureNamed refuses it", w)
		}
	}
}

// TestResolveCombinesRulesAndPosture: a matched rule decides under an asking
// posture; an unmatched line takes the posture's answer; and the answer is marked
// Matched with the posture's SOURCE so a report can name the channel.
func TestResolveCombinesRulesAndPosture(t *testing.T) {
	pol := table(t, "forbidden git push")
	ask := NewPosture(DecisionPrompt, "default")

	if m := ask.Resolve(pol, []string{"git", "push"}); m.Decision != DecisionForbidden {
		t.Errorf("a matched rule = %v, want the rule's forbidden (the table outranks an asking posture)", m.Decision)
	}
	m := ask.Resolve(pol, []string{"ls", "-la"})
	if !m.Matched || m.Decision != DecisionPrompt {
		t.Errorf("an unmatched line = %+v, want a Matched prompt from the posture", m)
	}
	if m.Rule.Source != "default" {
		t.Errorf("posture answer Source = %q, want the channel that decided", m.Rule.Source)
	}
	// A nil table is the posture's own question, not a crash.
	if m := ask.Resolve(nil, []string{"anything"}); !m.Matched || m.Decision != DecisionPrompt {
		t.Errorf("nil-table Resolve = %+v, want a Matched prompt", m)
	}
}
