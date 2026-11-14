package agent

// Regression guards for the brief identity/display alignment (epic: hippocampus
// child 4/4 noise control, follow-up branch fix/hippocampus-key-aligns-display).
//
// The defect these tests pin down: the convergence gate compares the keys the
// ORGAN reports (hippoBriefKeys), while the model only reads the leads the
// briefing BODY displays (hippoSummaryMaxHits of them). On master (ca27da4) the
// key set was "one key per action + one PER HIT over ALL hits", so a brief whose
// displayed top-N leads repeated while its tail hits moved looked like a new
// memory state to the gate: adjudicate returned Lv-1 and the SAME body was
// pushed into the context again. That is the prompt-budget leak the gate exists
// to stop (the measured "one identical memo per round"), one narrower trigger.
//
// The fix derives the identity from the SAME rendering the model is shown:
// hippoLeadLines (shared by hippoBriefSummary and hippoBriefKeys) plus the
// rendered body text as a second dimension. These tests assert the delivered
// CONTRACT, not an internal: two cycles that display the same leads and the same
// body hand the host the same keys, so the host's gate archives the second one.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/hippocampus"
	"github.com/nickhuang99/gopi/internal/tools"
)

// briefTestConfidence is the hit confidence of the synthesized leads — above
// hippocampusAppendMinConfidence, so convergence is the only gate these tests
// exercise.
const briefTestConfidence = 0.9

// briefHit builds one retrieval lead. Only the fields the rendering and the
// convergence gate touch are set: the displayed line is
// "- #id [tier] topic — question".
//
// Tokens is set to the FULL coverage of a one-meaningful-token query on
// purpose: a row a search actually RETURNED matched at least one query token
// (tools memory.go's queryTokenCoverage), so a fixture hit with zero coverage
// would model a hit no retrieval can produce — and would silently disable the
// confidence damping these fixtures exist to exercise (Tokens==0 damps the
// brief's confidence to just above the no-hits floor).
func briefHit(id int, topic, question string) tools.MemoryHit {
	return tools.MemoryHit{ID: id, Topic: topic, Question: question,
		Tier: tools.TierWarm, Confidence: briefTestConfidence, Tokens: 1}
}

// displayedLeadKeySet returns only the displayed-lead identity keys.
func displayedLeadKeySet(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if strings.HasPrefix(k, hippoLeadKeyNamespace) {
			out = append(out, k)
		}
	}
	return out
}

// bodyKey returns the rendered-body identity key, or "" when absent.
func bodyKey(keys []string) string {
	for _, k := range keys {
		if strings.HasPrefix(k, hippoBriefBodyKeyNamespace) {
			return k
		}
	}
	return ""
}

// sameStringSet reports set equality (element multiplicity included).
func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	count := make(map[string]int, len(a))
	for _, s := range a {
		count[s]++
	}
	for _, s := range b {
		count[s]--
		if count[s] < 0 {
			return false
		}
	}
	return true
}

func hasKey(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}

// briefTailFixture returns the displayed leads plus `extra` tail hits — the
// fixture that makes the display cap the ONLY thing separating "old" from "new".
func briefTailFixture(extra ...int) []tools.MemoryHit {
	hits := []tools.MemoryHit{
		briefHit(871, "worktree 隔离", "如何在不动主仓的前提下开发"),
		briefHit(1330, "收敛门", "重复注入怎么止住"),
		briefHit(42, "go test 环境", "GOPI_HIPPOCAMPUS 残留导致假失败"),
	}
	for _, id := range extra {
		hits = append(hits, briefHit(id, "display cap 之外的一条", "tail lead"))
	}
	return hits
}

// TestHippocampus_ConvergenceGateSuppressesTailOnlyChange is the repro, run
// against the host's real adjudication: round 1 delivers, round 5 (past the
// min-inject interval, so convergence is the ONLY possible reason) sees the SAME
// displayed top-3 with a MOVED tail ⇒ Lv-0 archive, no injection.
func TestHippocampus_ConvergenceGateSuppressesTailOnlyChange(t *testing.T) {
	h := newHippocampusHost(testHippoCfg(hippocampusLv1Append), nil)
	first := hippoBriefFromResult(&hippocampus.Result{Query: "hippocampus noise gate", Hits: briefTailFixture(104, 105)})
	moved := hippoBriefFromResult(&hippocampus.Result{Query: "hippocampus noise gate", Hits: briefTailFixture(200, 201)})

	if first.Summary != moved.Summary {
		t.Fatal("fixture is wrong — the two bodies must be byte-identical")
	}
	if !sameStringSet(first.Keys, moved.Keys) {
		t.Fatalf("a moved tail changed the identity:\n%v\n%v", first.Keys, moved.Keys)
	}
	if got := h.adjudicate(first, 1); got != hippocampusLv1Append {
		t.Fatalf("the run's first brief = %v, want Lv-1 (a first brief is never suppressed)", got)
	}
	if got := h.adjudicate(moved, 1+hippocampusMinInjectIntervalRounds+1); got != hippocampusLv0Archive {
		t.Fatalf("identical displayed leads (moved tail) = %v, want Lv-0 archive — the body the model already read must not be re-injected", got)
	}
}

// TestHippocampus_IdentityCoversTheDisplayedLeadsOnly keeps the fix honest in
// the other direction: the identity must still carry the leads the model SAW (an
// empty key set would mean "nothing to compare" and re-deliver every time), and
// must NOT carry a hit beyond the display cap.
func TestHippocampus_IdentityCoversTheDisplayedLeadsOnly(t *testing.T) {
	hits := briefTailFixture(900, 901)
	b := hippoBriefFromResult(&hippocampus.Result{Query: "cap", Hits: hits})
	leadKeys := displayedLeadKeySet(b.Keys)
	if len(leadKeys) != hippoSummaryMaxHits {
		t.Fatalf("displayed-lead keys = %d, want the %d-lead display cap", len(leadKeys), hippoSummaryMaxHits)
	}
	for _, line := range hippoLeadLines(hits) {
		if !strings.Contains(b.Summary, line) {
			t.Errorf("displayed lead missing from the body: %q", line)
		}
		if !hasKey(b.Keys, hippoLeadKeyNamespace+line) {
			t.Errorf("displayed lead has no identity key: %q", line)
		}
	}
	for i := hippoSummaryMaxHits; i < len(hits); i++ {
		undisplayed := fmt.Sprintf("#%d", hits[i].ID)
		if strings.Contains(b.Summary, undisplayed) {
			t.Errorf("a hit beyond the display cap (%s) reached the body", undisplayed)
		}
		for _, k := range leadKeys {
			if strings.Contains(k, undisplayed) {
				t.Errorf("a hit beyond the display cap (%s) reached the identity: %q", undisplayed, k)
			}
		}
	}
}

// TestHippocampus_LeadsRepeatWithNewBodyIsDeliverable proves the SECOND identity
// dimension. The same displayed leads with different prose (a re-phrased query
// is the real-world case) mean the model would read a body it has never read —
// so the brief must stay deliverable. The lead dimension alone would archive it
// at Lv-0; the body dimension is what keeps display and identity in step at that
// boundary.
func TestHippocampus_LeadsRepeatWithNewBodyIsDeliverable(t *testing.T) {
	h := newHippocampusHost(testHippoCfg(hippocampusLv1Append), nil)
	leads := []tools.MemoryHit{
		briefHit(871, "worktree 隔离", "如何在不动主仓的前提下开发"),
		briefHit(1330, "收敛门", "重复注入怎么止住"),
	}
	a := hippoBriefFromResult(&hippocampus.Result{Query: "hippocampus noise gate", Hits: leads})
	b := hippoBriefFromResult(&hippocampus.Result{Query: "hippocampus 去重键", Hits: leads})

	if a.Summary == b.Summary {
		t.Fatal("fixture is wrong — the two bodies must differ (the query moved)")
	}
	if !sameStringSet(displayedLeadKeySet(a.Keys), displayedLeadKeySet(b.Keys)) {
		t.Fatal("fixture is wrong — the displayed leads must repeat")
	}
	if bodyKey(a.Keys) == bodyKey(b.Keys) {
		t.Fatal("the rendered body is not part of the identity — an unread body would be archived")
	}
	if sameStringSet(a.Keys, b.Keys) {
		t.Fatal("new prose over the same leads must NOT look converged")
	}
	if got := h.adjudicate(a, 1); got != hippocampusLv1Append {
		t.Fatalf("first brief = %v, want Lv-1", got)
	}
	if got := h.adjudicate(b, 1+hippocampusMinInjectIntervalRounds); got != hippocampusLv1Append {
		t.Fatalf("a changed body over the same leads = %v, want Lv-1 (the model has not read this text)", got)
	}
}

// TestHippocampus_NoHitsNoBodyYieldsNoKeys keeps the host's keyless-brief
// boundary: nothing retrieved and nothing rendered ⇒ no identity at all, which
// the host reads as "no comparison possible, the model stays the judge" — never
// as "converged" (prefer over-delivery to silence). The empty body must not
// contribute a constant "body:" key either: that would make two empty briefs
// look like a converged pair.
func TestHippocampus_NoHitsNoBodyYieldsNoKeys(t *testing.T) {
	b := hippoBriefFromResult(&hippocampus.Result{Query: "nothing to find"})
	if b.Summary != "" {
		t.Fatalf("a hitless brief must render no body, got %q", b.Summary)
	}
	if len(b.Keys) != 0 {
		t.Fatalf("a hitless, bodyless brief must carry no keys (keyless = never converged); got %v", b.Keys)
	}
	if hasKey(b.Keys, hippoBriefBodyKeyNamespace) {
		t.Fatalf("an empty body produced a body key: %v", b.Keys)
	}
	// A keyless brief with a body is never "converged" either — the host keeps
	// delivering it (over-delivery beats silence).
	h := newHippocampusHost(testHippoCfg(hippocampusLv1Append), nil)
	keyless := &hippocampusBrief{Confidence: 0.95, Summary: "a body with no identity", Keys: nil}
	if got := h.adjudicate(keyless, 1); got != hippocampusLv1Append {
		t.Fatalf("first keyless brief = %v, want Lv-1", got)
	}
}
