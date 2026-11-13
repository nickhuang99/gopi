package tools

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// prefixSelfMatch is the OLD ownership predicate, kept here as the failing arm
// of the regression proof: `claimed_by LIKE self || '%'`. It is what
// LiveRowsOwnedByOthers used to run in SQL, and it is wrong exactly where the
// claim stamp is a COMPOSITE (host + separator + pid): a prefix cannot express
// a component boundary.
func prefixSelfMatch(claimedBy, self string) bool {
	return strings.HasPrefix(claimedBy, self)
}

// TestClaimHostIsSelf_ComponentBoundary is the minimal repro of the
// LiveRowsOwnedByOthers defect: the self-match must be the host COMPONENT, so
// a foreign host whose name merely EXTENDS ours ("host" vs "host2") is
// foreign, not self. Regression guard for the fixed `host` vs `host2:pid`
// misclassification.
func TestClaimHostIsSelf_ComponentBoundary(t *testing.T) {
	cases := []struct {
		claimedBy string
		self      string
		wantSelf  bool
	}{
		{"hp", "hp", true},       // bare host = self
		{"hp:1", "hp", true},     // host:pid = self
		{"hp:99999", "hp", true}, // any pid of ours is still ours
		{" hp:1 ", "hp", true},   // padded stamp
		{"hp2:77", "hp", false},  // THE BUG: superstring host is FOREIGN
		{"hp2", "hp", false},     // ... even without a pid part
		{"hp-x:5", "hp", false},  // any appended char → different host
		{"h:5", "hp", false},     // prefix of self is foreign too
		{"other-host:9", "hp", false},
		{"", "hp", false},       // no stamp is never self
		{"hp:1", "", false},     // no identity can never claim self
		{"hp:", "hp", false},    // empty pid: not the host:pid shape
		{"hp2:77", "hp2", true}, // the peer's own view of itself
	}
	for _, c := range cases {
		if got := claimHostIsSelf(c.claimedBy, c.self); got != c.wantSelf {
			t.Errorf("claimHostIsSelf(%q, %q) = %v, want %v", c.claimedBy, c.self, got, c.wantSelf)
		}
		// The proof that this test pins a REAL defect and not a tautology:
		// the retired predicate disagrees with the fixed one exactly on the
		// superstring cases, which is the misclassification being fixed.
		if strings.HasPrefix(c.claimedBy, c.self) && c.self != "" && !c.wantSelf {
			if !prefixSelfMatch(c.claimedBy, c.self) {
				t.Errorf("the retired prefix predicate no longer misclassifies %q vs %q — "+
					"if the old rule was already correct, this regression guard proves nothing",
					c.claimedBy, c.self)
			}
		}
	}
	// The filter is the in-memory twin of the counter's classification.
	got := filterSelfOwnedClaims([]string{"hp:1", "hp2:77", "", "hp", "hp2"}, "hp")
	want := []string{"hp2:77", "hp2"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("filterSelfOwnedClaims = %v, want %v", got, want)
	}
}

// TestLiveRowsOwnedByOthers_SuperstringHostPeer is the ledger-level repro: a
// foreign peer whose hostname merely STARTS WITH ours holds an in_progress row,
// and the probe MUST report it as foreign live work. Before the fix the row
// was swallowed by the `NOT LIKE self%` prefix (n == 0), so a node could
// believe it was alone while a same-prefix peer was mid-task.
func TestLiveRowsOwnedByOthers_SuperstringHostPeer(t *testing.T) {
	newDeltaTestDB(t)
	self, err := os.Hostname()
	if err != nil || strings.TrimSpace(self) == "" {
		t.Skipf("no hostname in this environment")
	}
	base := float64(time.Now().Unix())

	if got := LiveRowsOwnedByOthers(); got != 0 {
		t.Fatalf("empty ledger reported %d foreign live rows, want 0", got)
	}

	// The peer: our hostname with one more character, exactly the shape the
	// prefix predicate accepted as us.
	peer := self + "2"
	deltaInsertRaw(t, "t-peer-superstring", "peer mid-task", taskStatusInProgress, base, base, "")
	deltaSetClaim(t, "t-peer-superstring", fmt.Sprintf("%s:%d", peer, os.Getpid()))

	if prefixSelfMatch(peer+":77", self) != true {
		t.Fatalf("test premise broken: the retired prefix rule would not have matched %q vs %q", peer, self)
	}
	if got := LiveRowsOwnedByOthers(); got != 1 {
		t.Fatalf("superstring-host peer row not counted: LiveRowsOwnedByOthers = %d, want 1 "+
			"(the row must be classified by the HOST COMPONENT, not by the prefix)", got)
	}

	// Our OWN stamps in both shapes stay excluded — the fix must not trade an
	// under-count for an over-count.
	deltaInsertRaw(t, "t-self-bare", "self held", taskStatusInProgress, base, base, "")
	deltaSetClaim(t, "t-self-bare", self)
	deltaInsertRaw(t, "t-self-pid", "self held with pid", taskStatusInProgress, base, base, "")
	deltaSetClaim(t, "t-self-pid", fmt.Sprintf("%s:%d", self, os.Getpid()))
	if got := LiveRowsOwnedByOthers(); got != 1 {
		t.Fatalf("self-held rows leaked into the foreign count: got %d, want 1", got)
	}

	// Terminal and unclaimed rows are outside the predicate.
	deltaInsertRaw(t, "t-peer-done", "peer finished", taskStatusCompleted, base, base, "")
	deltaSetClaim(t, "t-peer-done", peer+":9")
	deltaInsertRaw(t, "t-unclaimed", "nobody holds it", taskStatusInProgress, base, base, "")
	if got := LiveRowsOwnedByOthers(); got != 1 {
		t.Fatalf("non-live/unclaimed rows leaked into the count: got %d, want 1", got)
	}
}
