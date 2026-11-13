package tasksource

import (
	"testing"

	"github.com/nick/gopi/internal/config"
)

// The derivation table: every (signature, request) pair the ruling speaks to.
// The rows that matter most are the ones that used to be wrong — an agent
// session's row claiming `human`.
func TestDeriveHonoursEvidenceForAHumanClaim(t *testing.T) {
	agentSig := config.AgentSpawnSignature{AgentSession: true, AnchorInherited: true}
	workerSig := config.AgentSpawnSignature{AgentSession: true, AnchorInherited: true, ParentIsWorker: true}
	terminalSig := config.AgentSpawnSignature{AgentSession: true, AnchorInherited: true, HumanTerminal: true}
	cleanSig := config.AgentSpawnSignature{}

	cases := []struct {
		name      string
		requested string
		sig       config.AgentSpawnSignature
		want      string
	}{
		// THE DEFECT, pinned: an agent session's row may not claim a person,
		// whether it asked for it explicitly or said nothing at all.
		{"agent session asks for human → downgraded", Human, agentSig, Agent},
		{"agent session says nothing → agent", "", agentSig, Agent},
		// A human terminal that happens to sit inside an agent chain: the
		// operator's own per-invocation claim is the evidence.
		{"operator terminal claims human", Human, terminalSig, Human},
		{"operator terminal says nothing → human", "", terminalSig, Human},
		// A clean process (the ordinary CLI: a person typing at a shell) is
		// the strongest human evidence available without authentication.
		{"clean process claims human", Human, cleanSig, Human},
		{"clean process says nothing → human", "", cleanSig, Human},
		// A worker's own child is INSIDE an agent session, so the procfs
		// parent check must win — otherwise every worker follow-up row would
		// lose the role distinction the operator asked for.
		{"worker child says nothing → worker", "", workerSig, Worker},
		{"worker child asks for human → worker", Human, workerSig, Worker},
		// A stated machine role speaks for itself: no evidence is required.
		{"stated merger is kept", Merger, agentSig, Merger},
		{"stated server is kept", Server, agentSig, Server},
		{"stated worker is kept", Worker, agentSig, Worker},
		// Retired spellings.
		{"legacy llm → agent", legacyLLM, agentSig, Agent},
		{"legacy auto with agent evidence → agent", legacyAuto, agentSig, Agent},
		{"legacy auto with worker evidence → worker", legacyAuto, workerSig, Worker},
		{"legacy auto, clean process → human", legacyAuto, cleanSig, Human},
		// An unknown spelling is never recorded as written: the row gets the
		// honest derived value instead of a fabricated provenance.
		{"unknown spelling → derived", "bogus", agentSig, Agent},
		{"padded human claim is still a human claim", "  " + Human + "  ", agentSig, Agent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Derive(tc.requested, tc.sig); got != tc.want {
				t.Errorf("Derive(%q, %+v) = %q, want %q", tc.requested, tc.sig, got, tc.want)
			}
		})
	}
}

// The alias table: what the vocabulary accepts and what it maps onto. `ok`
// answers ONE question — was the spelling recognized — so both a current value
// and a retired alias are ok; they are told apart by whether the result differs
// from the input (the distinction a caller actually needs).
func TestNormalizeMapsRetiredSpellingsOntoTheVocabulary(t *testing.T) {
	for _, v := range []string{Human, Agent, Worker, Merger, Server} {
		got, ok := Normalize(v)
		if !ok || got != v {
			t.Errorf("Normalize(%q) = (%q, %v), want (%q, true) — a current value passes through", v, got, ok, v)
		}
		if !Known(v) {
			t.Errorf("Known(%q) = false, want true", v)
		}
	}
	aliases := map[string]string{legacyLLM: Agent, legacyAuto: Merger}
	for alias, want := range aliases {
		got, ok := Normalize(alias)
		if !ok {
			t.Errorf("Normalize(%q) reported NOT recognized, yet it is the alias of %q — "+
				"a caller reading that as \"skip\" would store the retired spelling", alias, want)
		}
		if got != want {
			t.Errorf("Normalize(%q) = %q, want %q", alias, got, want)
		}
		if got == alias {
			t.Errorf("Normalize(%q) returned the input unchanged — the alias was not canonicalized", alias)
		}
		if !Known(alias) {
			t.Errorf("Known(%q) = false — a stored legacy row must stay readable", alias)
		}
	}
	// An unknown value is refused rather than guessed.
	if got, ok := Normalize("bogus"); got != "" || ok {
		t.Errorf("Normalize(bogus) = (%q, %v), want (\"\", false)", got, ok)
	}
	if Known("") || Known("bogus") {
		t.Error("Known must refuse an empty or invented spelling")
	}
}

// The human claim is the ONE value the framework holds to evidence — IsHumanClaim
// is that predicate, and it must not be fooled by padding.
func TestIsHumanClaim(t *testing.T) {
	if !IsHumanClaim(Human) || !IsHumanClaim("  "+Human) {
		t.Error("a human claim must be recognized")
	}
	for _, v := range []string{Agent, Worker, Merger, Server, legacyLLM, legacyAuto, ""} {
		if IsHumanClaim(v) {
			t.Errorf("IsHumanClaim(%q) = true — only %q asserts a person", v, Human)
		}
	}
}

// The refusal message NAMES the retired aliases, so a caller passing "llm" or
// "auto" reads it as a known older spelling rather than a typo (Rule 15: a
// silent failure is worse than a loud one).
func TestPolicyNamesTheRetiredAliases(t *testing.T) {
	p := Policy()
	for _, want := range []string{Human, Agent, Worker, Merger, Server, legacyLLM, legacyAuto} {
		if !stringsContains(p, want) {
			t.Errorf("Policy() = %q, want it to name %q", p, want)
		}
	}
}

func stringsContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
