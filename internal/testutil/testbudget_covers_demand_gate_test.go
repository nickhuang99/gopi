package testutil

// THE COVERAGE GATE: the declared per-package budget must actually COVER the
// slowest package's recorded demand — and that demand must exceed Go's untuned
// default, which is the fact that makes a bare `go test ./cmd/agent/` a red
// unrelated to the caller's diff.
//
// WHY THIS EXISTS, and what the neighbouring gates do NOT check.
// `TestTestBudgetHasASingleSourceAndNamesATimeout` pins that a budget EXISTS and
// parses and is passed to `go test -timeout`.
// `TestBudgetConstantCarriesItsDerivation` pins that the constant EQUALS the
// value its derivation comment is written for (45m), and that the derivation
// carries its criterion, its reading (993s) and its stream.
// Neither checks the ARITHMETIC'S MEANING. Between them there is a hole a
// future edit walks straight through: set `test_budget="12m"` and rewrite the
// derivation to say "12m", and BOTH gates stay green while `./cmd/agent` — whose
// measured demand is 993s on a normally loaded host and >=1500s at the heavier
// end of ordinary load — cannot finish inside the cap. The suite would be
// governed by a number that no longer describes the thing it governs, and the
// only witness would be an anonymous `panic: test timed out`, which is exactly
// the shape this repo has already paid for once (the 25m cap expiring at
// ΣElapsed 1493.0s, `scripts/test-budget.sh`'s own post-mortem).
//
// So the two relations are asserted HERE, and they are the whole point:
//
//	declared budget  >  recorded demand   (the merge path can finish)
//	recorded demand  >  Go's 10m default  (a bare run CANNOT — this is the fact,
//	                                       not a code verdict)
//
// Both numbers are read from their ONE source (Rule 2/14): the budget from
// `scripts/test-budget.sh`'s `test_budget=` constant, the demand from the
// reading the derivation above that constant states. Nothing is re-typed.
//
// A RED HERE IS NOT "SOMETHING IS SLOW" — it is "the declaration stopped
// describing the measured distribution", and the failure text says which of the
// two relations broke and which number moved.

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

const (
	// goDefaultTestBudget mirrors testing's own per-package wall-clock default.
	// It is a MIRROR of an external value, which is why it is named here with
	// its source rather than typed inline: `go help testflag` states it —
	// "-timeout d … The default is 10 minutes (10m)." — and a run that passes
	// no `-timeout` inherits it, since testing registers the flag with
	// `flag.Duration("test.timeout", 0, …)` and applies the 10m fallback
	// internally (measured: `flag.Lookup("test.timeout").Value.String()` reads
	// `10m0s` for a bare `go test`, `45m0s` under this repo's script).
	goDefaultTestBudget = 10 * time.Minute

	// budgetCoversDemandHeadroom is the margin the declaration must keep above
	// the recorded demand. It is not a tuning knob: it exists so the gate
	// answers "does the budget COVER this package" and not "is the budget
	// exactly equal to the measurement", which is a value that fails on the
	// first busier host. One full demand again is the smallest margin that
	// survives the load span the script's own derivation records (1.5x-3x
	// between a quiet and a spread-out host) — the criterion is that a loaded
	// host cannot reach the cap, so the margin must clear a load multiple.
	budgetCoversDemandHeadroom = 1
)

// budgetCoversDemand is the predicate, as a PURE function so the red proof can
// drive it with distributions this tree does not currently have. It returns
// whether the declaration covers the demand with the required headroom, and the
// margin it actually holds.
func budgetCoversDemand(declared, demand time.Duration) (bool, time.Duration) {
	margin := declared - demand
	return declared >= demand+demand*budgetCoversDemandHeadroom, margin
}

// TestDeclaredBudgetCoversTheSlowestPackagesDemand is the acceptance gate: the
// shipped declaration must clear the recorded demand AND that demand must clear
// Go's default. Read together, the two relations are the machine-checked
// statement of "the merge path can run this package; a bare `go test` cannot".
func TestDeclaredBudgetCoversTheSlowestPackagesDemand(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}

	// 1. The declaration, from its ONE source.
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(budgetScriptPath)))
	if err != nil {
		t.Fatalf("read %s: %v — the declared per-package test budget is gone", budgetScriptPath, err)
	}
	script := string(raw)
	m := regexp.MustCompile(budgetAttributionBudgetConstRe).FindStringSubmatch(script)
	if m == nil {
		t.Fatalf("%s declares no `test_budget=\"…\"` — the per-package budget must live there (Rule 1a/2)",
			budgetScriptPath)
	}
	declared, err := time.ParseDuration(m[1])
	if err != nil {
		t.Fatalf("%s declares test_budget=%q, which Go cannot parse: %v", budgetScriptPath, m[1], err)
	}
	if declared <= 0 {
		t.Fatalf("%s declares test_budget=%q, which is not a positive cap: 0 disables the wall-clock "+
			"guarantee entirely, so a real hang would never be caught", budgetScriptPath, m[1])
	}

	// 2. The demand, from the reading the derivation states — the SAME constant
	// the attribution gate pins, so the two gates cannot disagree about it.
	demand, err := time.ParseDuration(budgetAttributionDerivationReading)
	if err != nil {
		t.Fatalf("the recorded demand reading %q is not a Go duration: %v",
			budgetAttributionDerivationReading, err)
	}
	if !regexp.MustCompile(`(?m)^#+\s*demand_quiet\s*=\s*` + regexp.QuoteMeta(budgetAttributionDerivationReading)).
		MatchString(script) {
		t.Fatalf("%s no longer states `demand_quiet = %s` above the constant — the arithmetic this gate "+
			"checks (and that a reader re-derives) must stay written down, next to the number it produced",
			budgetScriptPath, budgetAttributionDerivationReading)
	}

	// 3. THE TWO RELATIONS. Each names its own breach (Rule 15): a reader must
	// learn which number moved, not merely that "the budget is wrong".
	ok, margin := budgetCoversDemand(declared, demand)
	if !ok {
		t.Errorf("the declared budget %s does not cover the recorded demand %s with the required headroom "+
			"(margin %s, want >= %s): a merge/verify run of the slowest package would exhaust the cap and "+
			"report its red as a TIMEOUT — a wall-clock verdict wearing a test verdict's clothes. Either "+
			"raise `test_budget` in %s or re-measure the demand and record the new stream beside it",
			declared, demand, margin, demand*budgetCoversDemandHeadroom, budgetScriptPath)
	}
	if demand <= goDefaultTestBudget {
		t.Errorf("the recorded demand %s does not exceed Go's default per-package cap %s — this fact is "+
			"what makes the declaration necessary, and it has stopped being true. Either the package got "+
			"cheaper than the default (then say so, and this gate's premise is retired deliberately) or "+
			"the recorded reading is stale (re-measure and update `demand_quiet` in %s with its stream)",
			demand, goDefaultTestBudget, budgetScriptPath)
	}

	// 4. The fact itself, stated where the caller who typed `go test` will read
	// it. This is not decoration: the run that trips Go's default prints an
	// anonymous `panic: test timed out`, and this line is the sentence that
	// tells its reader the red was a CAP, not the tree.
	t.Logf("per-package budget: declared=%s covers recorded demand=%s (margin=%s); "+
		"Go's default cap is %s, i.e. %s does NOT fit %s and a bare `go test %s` run is a cap verdict, "+
		"not a code verdict — use `make test-budget PKGS='%s'`",
		declared, demand, margin, goDefaultTestBudget, demand, goDefaultTestBudget,
		budgetSlowestPackagePattern, budgetSlowestPackagePattern)
}

// budgetSlowestPackagePattern is the package this gate's demand reading belongs
// to — named once, because both the assertion text and the remedy command quote
// it, and a remedy that names the wrong package sends the reader somewhere else.
const budgetSlowestPackagePattern = "./cmd/agent"

// TestBudgetCoveragePredicateRejectsAShrunkenDeclaration is the RED PROOF: the
// predicate must go red for the declaration shapes that would look plausible in
// review, so a green is evidence about THIS tree and not a tautology. Each row
// carries its own expected verdict and margin, taken from the script's own
// post-mortem history rather than invented.
func TestBudgetCoveragePredicateRejectsAShrunkenDeclaration(t *testing.T) {
	demand := 993 * time.Second // the recorded demand, as a duration
	for _, row := range []struct {
		name     string
		declared time.Duration
		cover    bool
		margin   time.Duration
		why      string
	}{
		{
			name:     "Go's default (the value a bare `go test` inherits)",
			declared: goDefaultTestBudget,
			cover:    false,
			margin:   goDefaultTestBudget - demand,
			why:      "the shipped defect: the demand exceeds the cap, so the run dies by timeout",
		},
		{
			name:     "the retired 25m cap that expired mid-suite",
			declared: 25 * time.Minute,
			cover:    false,
			margin:   25*time.Minute - demand,
			why: "it CLEARS the quiet demand but not the headroom — which is exactly how it expired at " +
				"ΣElapsed 1493.0s of its 1500s on a loaded host, with nothing wrong in the code",
		},
		{
			name:     "the shipped 45m",
			declared: 45 * time.Minute,
			cover:    true,
			margin:   45*time.Minute - demand,
			why:      "clears the whole measured span with room for a third agent arriving mid-run",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, margin := budgetCoversDemand(row.declared, demand)
			if got != row.cover {
				t.Errorf("budgetCoversDemand(declared=%s, demand=%s) = %v, want %v — %s",
					row.declared, demand, got, row.cover, row.why)
			}
			if margin != row.margin {
				t.Errorf("budgetCoversDemand(declared=%s, demand=%s) reported margin %s, want %s",
					row.declared, demand, margin, row.margin)
			}
		})
	}
}
