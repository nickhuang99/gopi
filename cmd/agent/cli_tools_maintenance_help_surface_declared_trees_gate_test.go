// Anti-regression gates for the G6 arms whose cost was the HOST's rather than
// the case's: the declared-trees arms (owned fixture + own budget, below) and
// the shot render arms (owned BROWSER, at the end of this file).
//
// WHY THIS GATE EXISTS (measured 2026-09-26/27). The `gopi tool scratch-gc trees
// --dry-run` form went red ONCE in a full `go test ./cmd/agent/` run, with an
// elapsed of EXACTLY the shared bound (15.00s) while its same-shape sibling
// passed. The mechanism was not routing and not a slow VERB: the arm's own env
// sandboxed only the SCRATCH home (`g6ScratchEnv`), so `trees` resolved the
// operator's DECLARED trees and `HOME` stayed the operator's — one full /proc
// pass per LIVE process per configured tree, over ~400 processes and a build
// cache of tens of thousands of entries, all inside a 15s ceiling shared with
// every other form in the table. Measured on the real binary, that invocation
// costs ~7.7-8.0s idle and 20.4s under host load (see the task report's
// evidence), i.e. it straddles the shared bound.
//
// The remedy is local: the arm OWNS its fixture (env + setup) and carries its
// own budget. This gate pins exactly that, because both halves are one edit
// away from silently reverting — an env back to `g6ScratchEnv` reads the
// operator's trees again, and a dropped `budget` puts the arm back under the
// shared 15s. Neither revert is visible in the table's shape, and neither would
// fail any other test until the flake reappears on a loaded host.
//
// WHAT IT DOES NOT DO: it never runs a probe and never touches the host's load
// (Rule 27). It reads the case table — the same values the runner uses — and
// makes each one's OWNERSHIP explicit.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/shot"
)

// g6DeclaredTreesForms are the two arms this gate covers: the full sweep and
// its report-only sibling, which share the fixture and the budget.
var g6DeclaredTreesForms = []string{
	"gopi tool scratch-gc trees",
	"gopi tool scratch-gc trees --dry-run",
}

func TestG6DeclaredTreesArmOwnsItsFixtureAndBudget(t *testing.T) {
	// A home that is obviously not the operator's: the checks below are about
	// the DECLARED shape, not about anything on disk.
	home := filepath.Join(t.TempDir(), "g6-gate-home")
	for _, form := range g6DeclaredTreesForms {
		c, ok := g6Cases[form]
		if !ok {
			t.Fatalf("%q is no longer in g6Cases — this gate would be vacuous; if the form was renamed, move the gate with it", form)
		}

		// (1) The arm reads its OWN trees, not the host's. The declared tree
		// roots resolve through $HOME and the gopi data root (raw /
		// session-raw) through $GOPI_HOME, and the Go work tree through
		// $GOTMPDIR — a case that pins only the scratch home leaves the first
		// two resolving to the operator's, which is the pre-fix defect.
		if c.env == nil {
			t.Fatalf("%q declares no env — the arm would resolve the OPERATOR's declared trees (the pre-fix defect this gate pins)", form)
		}
		env := g6EnvMap(c.env(home))
		for _, k := range []string{
			"GOPI_HOME",             // the gopi data root: raw + session-raw trees
			"HOME",                  // the base both scratch and the worktree roots fall back to
			scratch.GoTmpDirEnvName, // the declared tree the fixture SEEDS
		} {
			v, present := env[k]
			if !present {
				t.Errorf("%q does not pin %s — that tree resolves on the OPERATOR's host", form, k)
				continue
			}
			if !g6PathInside(home, v) {
				t.Errorf("%q pins %s=%s, which is NOT inside the case's own home %s — the point of the fixture is that no operator tree is read", form, k, v, home)
			}
		}

		// (2) The arm carries a budget of its OWN, strictly inside the shared
		// bound — and it is a constant, not the shared bound wearing a second
		// name. A zero budget means cliContractTimeout (see g6CaseBudget), which
		// is the pre-fix shape.
		if c.budget <= 0 {
			t.Errorf("%q declares no budget (0 = the shared cliContractTimeout): an arm whose cost scales with the /proc table cannot share a ceiling with a usage print", form)
		} else if c.budget >= cliContractTimeout {
			t.Errorf("%q budgets %v, which is not inside the shared bound %v — a bound at or above it is a WIDENED bound, not an owned one", form, c.budget, cliContractTimeout)
		}

		// (3) The fixture is SEEDED, or the arm has nothing of its own to sweep:
		// an owned home with no aged entry would assert on an empty tree and
		// stop exercising the form.
		if c.setup == nil {
			t.Errorf("%q pins an owned home but seeds no fixture — the form would be asserted against a tree with nothing to find", form)
		}
	}

	// (4) The other direction: the shared bound must not be DEFINED in terms of
	// an arm's budget. If cliContractTimeout were ever expressed as (or derived
	// from) g6DeclaredTreesBudget, check (2) would pass while every untyped form
	// silently lost its ceiling.
	if cliContractTimeout < g6DeclaredTreesBudget {
		t.Errorf("the shared bound %v is BELOW the arm budget %v — the shared bound is the wider one by construction (an arm's own budget is the tighter)", cliContractTimeout, g6DeclaredTreesBudget)
	}
}

// g6EnvMap turns an env slice into a last-wins map, which is how os/exec
// resolves a duplicated variable (the child keeps the LAST spelling — the
// mechanism g6DeclaredTreesEnv relies on when it appends HOME).
func g6EnvMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, kv := range env {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				out[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return out
}

// g6PathInside reports whether p is home itself or lives under it.
func g6PathInside(home, p string) bool {
	rel, err := filepath.Rel(home, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !hasDotDotPrefix(rel))
}

// hasDotDotPrefix reports whether a relative path escapes upward.
func hasDotDotPrefix(rel string) bool {
	return len(rel) >= 2 && rel[0] == '.' && rel[1] == '.'
}

// --- the shot arms' owned browser --------------------------------------------
//
// Anti-regression gate for the G6 render arms' injected browser.
//
// WHY THIS GATE EXISTS (measured 2026-09-27). `gopi tool shot --url URL [--out PATH]`
// went red in a full `go test ./cmd/agent/ -run ...` run with the diagnostic's
// NO-VERB-OUTPUT timeout at the shared bound (15.01s), which reads as "the argv
// fell through to the agent loop" — a ROUTING verdict. It was not: the arm was
// the one entry in the table that launched the HOST's Chromium, and its wall
// clock was that browser's cold start. Measured on the real binary with the
// same argv: 5.60s / 6.11s / 7.73s / 8.70s / 18.02s / 28.36s / 15.49s / 13.93s
// across load 4.7 → 15.6 — i.e. the arm's duration is the HOST's, and it
// straddles a ceiling shared with every usage print in the table. The defect is
// the fixture and the bound's shared use, not the verb: with the case's own
// browser injected, the same arms run in 2.0-2.2s and the launch, the DevTools
// handshake and the CDP discovery are all still exercised for real.
//
// The revert this gate forbids is invisible in the table's shape: drop `env`
// (or point it anywhere but the case's own stub) and the arm silently resolves
// the operator's Chromium again — green on a quiet host, red under load, and
// reported as a routing bug.
//
// WHAT IT DOES NOT DO: it never runs a probe and never touches the host's load
// (Rule 27). It reads the case table — the same values the runner uses.
func TestG6ShotArmsOwnTheirBrowser(t *testing.T) {
	// A home that is obviously not the operator's: these checks are about the
	// DECLARED shape, not about anything on disk.
	home := filepath.Join(t.TempDir(), "g6-shot-gate-home")
	// The fixture writes INTO this home (the runner hands it an existing
	// t.TempDir()), so it is materialised here for the same reason.
	if err := os.MkdirAll(home, g6ShotGateHomeMode); err != nil {
		t.Fatalf("materialise the gate's home %s: %v", home, err)
	}
	for _, form := range g6ShotBrowserForms {
		c, ok := g6Cases[form]
		if !ok {
			t.Fatalf("%q is no longer in g6Cases — this gate would be vacuous; if the form was renamed, move the gate with it", form)
		}

		// (1) The arm injects a browser of its OWN through the verb's documented
		// override, and that path is inside the case's sandbox home — never a host
		// binary, never the operator's playwright cache.
		if c.env == nil {
			t.Fatalf("%q declares no env — the arm resolves the HOST's Chromium (the pre-fix defect this gate pins)", form)
		}
		env := g6EnvMap(c.env(home))
		v, present := env[shot.ChromeEnvName]
		if !present {
			t.Errorf("%q does not pin %s — the arm launches the host's browser, so its wall clock is the host's cold start", form, shot.ChromeEnvName)
			continue
		}
		if !g6PathInside(home, v) {
			t.Errorf("%q pins %s=%s, which is NOT inside the case's own home %s — the point of the fixture is that no host browser is resolved", form, shot.ChromeEnvName, v, home)
		}
		// (2) The fixture is SEEDED by the case itself, and the argv record it
		// writes is the evidence the arm asserts on: an owned browser whose launch
		// nothing observes would let the arm pass on a verb that never exec'd it.
		if c.setup == nil {
			t.Errorf("%q injects a browser but seeds no fixture — the stub would never exist", form)
			continue
		}
		// Materialise the fixture the way the runner does, so the stub examined
		// below is the file the probe will actually execute (an env pointing at a
		// stub the setup never writes is a broken fixture, and that is asserted
		// here rather than discovered as a puzzling launch refusal).
		c.setup(t, home)
		st, err := os.Stat(g6ShotStubChromePath(home))
		if err != nil {
			t.Errorf("%q pins a browser at %s but the fixture does not write it: %v", form, v, err)
			continue
		}
		if st.Mode()&execBitsMask == 0 {
			t.Errorf("%q pins a browser that is not executable (mode %v) — internal/shot rejects it at discovery", form, st.Mode())
		}
		if !g6ContainsPath(c.wantSurvives, g6ShotStubArgvFile) {
			t.Errorf("%q does not assert the stub's argv record (%s) survives — the launch half of the form is then unproven", form, g6ShotStubArgvFile)
		}
		if c.wantContains[g6ShotStubArgvFile] == "" {
			t.Errorf("%q reads no content out of the argv record — a file that exists is not a launch; the claim needs the verb's own flag set", form)
		}

		// (3) --out stays INSIDE the sandbox (Rule 30): a render arm whose output
		// path escapes the case's home can leave a PNG in the checkout.
		for _, a := range c.argv(home) {
			if strings.HasPrefix(a, "-") {
				continue
			}
			if filepath.IsAbs(a) && !g6PathInside(home, a) {
				t.Errorf("%q is handed the absolute path %s, which is outside its own home — a probe writing there escapes the sandbox", form, a)
			}
		}
	}
}

// g6ShotGateHomeMode is the mode the gate materialises its throwaway home with
// (the same one the fixture's own sandbox uses, so the stub it writes is a file
// the gate can actually stat and execute-check).
const g6ShotGateHomeMode = 0o700

// g6ShotBrowserForms are the arms this gate covers: the two forms that render,
// i.e. the only ones whose cost included the host's browser. The third shot form
// (`--self --addr`) answers from the reachability guard before any launch, so it
// needs no browser and no budget.
var g6ShotBrowserForms = []string{
	"gopi tool shot --url URL [--out PATH] [flags]",
	"gopi tool shot --self",
}

// execBitsMask is the POSIX execute-bit mask. internal/shot's own discovery mask
// is unexported, so the criterion (an executable file) is re-stated here rather
// than imported — the value, not a second policy.
const execBitsMask = os.FileMode(0o111)

// g6ContainsPath reports whether rels names rel.
func g6ContainsPath(rels []string, rel string) bool {
	for _, r := range rels {
		if r == rel {
			return true
		}
	}
	return false
}
