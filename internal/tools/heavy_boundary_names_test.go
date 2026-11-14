package tools

// heavy_boundary_names_test.go — the tools-side half of the heavy-test
// boundary's mirrored env-name bridge (internal/heavytest/boundary.go).
//
// internal/heavytest is a LEAF and cannot import this package, so the names the
// boundary tests for the task-child signature are a documented MIRROR of the
// constants defined HERE (TaskWrapperEnv / TaskRunIDEnv). Rule 14 bans a mirror
// without a check; this test is that check.
//
// WHY IT MATTERS: if the spawn seam renamed its marker, the boundary would look
// for a name nothing sets, conclude "no signature", and let a task child run the
// heavy set — the defect it exists to close, reintroduced silently by a rename.

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/heavytest"
)

func TestHeavyBoundaryMirrorsTheTaskChildIdentityNames(t *testing.T) {
	for _, c := range []struct {
		what          string
		defined, mirr string
	}{
		{"task-child wrapper identity", TaskWrapperEnv, heavytest.TaskWrapperEnv},
		{"task-child run/close-out id", TaskRunIDEnv, heavytest.TaskRunIDEnv},
	} {
		if c.defined != c.mirr {
			t.Errorf("heavytest mirrors the %s as %q while this package defines %q — the boundary "+
				"would look for a name the spawn seam never sets, so a task child would be read as "+
				"an operator invocation and the heavy set would run (Rule 14: a mirror needs a check)",
				c.what, c.mirr, c.defined)
		}
	}
}
