package fullid

import (
	"strings"
	"testing"
)

// TestTruncatedUUIDShape pins the predicate that decides whether an operand is
// a CUT-SHORT id (refusable with a helpful message) or something else entirely
// (which keeps the caller's ordinary "not found" answer).
func TestTruncatedUUIDShape(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"01a0ce09", true},                              // the 8-hex chunk a listing printed
		{"01a0ce09-6395", true},                         // two groups, still cut short
		{"01a0ce09-6395-7e7e", true},                    // three groups
		{"01a0ce09-6395-7e7e-9f04", true},               // four groups
		{"01a0ce09-6395-7e7e-9f04-6ca51716c90b", false}, // FULL: IsUUID's job, not this
		{"01a0ce0", false},                              // shorter than the printed chunk
		{"01a0ce09-6395_7e7e", false},                   // a dash is missing where the pattern has one
		{"01a0ce0g", false},                             // 'g' is not hex
		{"t-1788387314", false},                         // legacy auto id (not hex)
		{"t-proof-wanted", false},                       // custom id
		{"", false},
	}
	for _, c := range cases {
		if got := TruncatedUUID(c.in); got != c.want {
			t.Errorf("TruncatedUUID(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestIsUUIDAcceptsExactlyTheFullShape — the predicate every resolution path
// gates on: only a complete canonical uuid may be looked up.
func TestIsUUIDAcceptsExactlyTheFullShape(t *testing.T) {
	if !IsUUID("01a0ce09-6395-7e7e-9f04-6ca51716c90b") {
		t.Fatal("a canonical uuid must be accepted")
	}
	for _, bad := range []string{
		"01a0ce09",
		"01a0ce09-6395-7e7e-9f04-6ca51716c90",
		"01a0ce09-6395-7e7e-9f04-6ca51716c90bb",
		"01a0ce09-6395-7e7e-9f04-6ca51716c90B", // uppercase is not the canonical spelling
		"t-1788387314",
		"t-proof-wanted",
		"",
	} {
		if IsUUID(bad) {
			t.Errorf("IsUUID(%q) = true — only a full canonical uuid may be looked up", bad)
		}
	}
}

// TestRefusalNamesEveryAmbiguity is the operator-facing contract: the error must
// say that a prefix is not an id, and — when several identities share it — name
// them ALL, so the reader can pick the full id instead of guessing.
func TestRefusalNamesEveryAmbiguity(t *testing.T) {
	err := Refusal(KindTask, "01a0aba1", []string{
		"01a0aba1-44c8-7179-9f5c-635f1ac2b2e7",
		"01a0aba1-a4d9-726a-8d46-652011113dff",
	})
	msg := err.Error()
	for _, want := range []string{
		"not a full id",
		"ambiguous",
		"2 identities",
		"01a0aba1-44c8-7179-9f5c-635f1ac2b2e7",
		"01a0aba1-a4d9-726a-8d46-652011113dff",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal must contain %q, got: %s", want, msg)
		}
	}
}

// TestRefusalSingleCandidatePointsAtTheFullID — a prefix that happens to be
// unique is STILL refused (the rule has no exception), but the message hands
// over the one id it meant so the re-run is a copy-paste.
func TestRefusalSingleCandidatePointsAtTheFullID(t *testing.T) {
	msg := Refusal(KindSession, "2ab3facd", []string{"2ab3facd-1111-4111-8111-111111111111"}).Error()
	if !strings.Contains(msg, "2ab3facd-1111-4111-8111-111111111111") {
		t.Errorf("a unique-prefix refusal must name the full id, got: %s", msg)
	}
	if strings.Contains(msg, "ambiguous") {
		t.Errorf("a unique prefix is not ambiguous, got: %s", msg)
	}
}

// TestRefusalWithoutCandidatesStillRefuses — the path taken by a prefix that
// matches NOTHING (e.g. a listing chunk of a session that was already pruned).
// The answer must still be an explicit refusal saying a prefix is never
// resolved, never the bare "not found" that reads as a typo.
func TestRefusalWithoutCandidatesStillRefuses(t *testing.T) {
	msg := Refusal(KindSession, "01a0ce09", nil).Error()
	if !strings.Contains(msg, "never resolved") {
		t.Errorf("a prefix with no candidate must still be refused as a prefix, got: %s", msg)
	}
}
