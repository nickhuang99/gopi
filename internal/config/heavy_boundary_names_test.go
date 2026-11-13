package config

// heavy_boundary_names_test.go — the bridge for the heavy-test boundary's
// MIRRORED env names (internal/heavytest/boundary.go).
//
// WHY A MIRROR EXISTS AT ALL: internal/heavytest must stay a LEAF (the
// in-package tests of internal/scratch, internal/tools and cmd/agent import it,
// so it may not reach a package that could close a cycle — config imports
// scratch, so heavytest cannot import config). Rule 14 bans a mirror without a
// check; this test IS the check, and it can exist precisely because package
// config may import the leaf.
//
// A drift here is not cosmetic: the boundary would look for a name nothing
// exports, decide "no signature", and let a task child run the heavy set — the
// exact defect the boundary closes.

import (
	"testing"

	"github.com/nick/gopi/internal/heavytest"
)

func TestHeavyBoundaryMirrorsTheAgentGuardNames(t *testing.T) {
	for _, c := range []struct {
		what            string
		guard, mirrored string
	}{
		{"agent session handle", AgentSessionEnv, heavytest.AgentSessionEnv},
		{"foreign harness session", HermesSessionIDEnv, heavytest.HermesSessionEnv},
		{"agent chain anchor", AgentSessionAncestorEnv, heavytest.AgentAncestorEnv},
		{"operator terminal declaration", HumanTerminalEnv, heavytest.HumanTerminalEnv},
	} {
		if c.guard != c.mirrored {
			t.Errorf("heavytest mirrors the %s as %q while this package defines %q — the boundary "+
				"would read a name nothing exports and conclude 'no work signature', letting a task "+
				"child run the heavy set (Rule 14: a mirror needs a check, and this is it)",
				c.what, c.mirrored, c.guard)
		}
	}
}

// TestHeavyBoundaryTruthySetMatchesEnvBool pins the boundary's declaration
// parser to the repo's boolean convention: heavytest cannot import this package,
// so it re-implements the truthy set, and the two must agree on every accepted
// spelling and on every rejected one (a canonical "off" must stay off).
func TestHeavyBoundaryTruthySetMatchesEnvBool(t *testing.T) {
	for _, raw := range []string{"1", "on", "yes", "true", "enabled", "TRUE", "  yes  ", "On", "1 "} {
		if !heavytest.DeclaredOperatorTerminal(raw) {
			t.Errorf("heavytest rejects %q as a terminal declaration while envTruthy accepts it", raw)
		}
	}
	for _, raw := range []string{"", " ", "off", "false", "0", "no", "nope", "enabled!", "2"} {
		if heavytest.DeclaredOperatorTerminal(raw) {
			t.Errorf("heavytest accepts %q as a terminal declaration while this package's envTruthy "+
				"rejects it — a wrapper's canonical %q must not enable the heavy set", raw, raw)
		}
	}
}
