package config

import (
	"strings"
	"testing"
)

// The reachability claim is a PROMISE about the whole catalog, so the check
// that it holds must also be about the whole catalog (measured 2026-09-27:
// 44 of the 80 rows `gopi --help-env` renders carry an em dash in the flag
// column, and every one of them used to answer `gopi: unknown flag
// --gopi-<env>` — a refusal whose first word is false and whose second hides
// the reason). These tests drive the REAL surfaces — FlagOverrides, the
// accepted-flag tables flagArity builds, and the rendered help — over every
// catalog row, so a row added tomorrow cannot drift the claim away from the
// parser without turning a gate red.

// envProbeValue is the value token the probe feeds a knob. "1" is accepted by
// every shape in the catalog: a scalar flag takes it as its value, and a
// boolean knob reads it through normalizedBoolValue (the same token the parser
// consumes for `--flag 1`).
const envProbeValue = "1"

// envFlagColumn parses the rendered env table into env-name → flag column, so
// the assertion reads what an operator reads (`gopi --help-env`), never a
// second copy of the catalog. It walks only the HEAD lines: the head of a row
// starts with two spaces followed by the variable name and a space, and its
// LAST field is the flag column (help prose is indented elsewhere).
func envFlagColumn(t *testing.T, table string) map[string]string {
	t.Helper()
	col := map[string]string{}
	for _, line := range strings.Split(table, "\n") {
		if !strings.HasPrefix(line, "  ") {
			continue
		}
		rest := line[2:]
		// A HEAD line is indented exactly two spaces; prose help under a row is
		// indented deeper (envTableHelpCol), and a group title sits at column 0.
		if rest == "" || strings.HasPrefix(rest, " ") {
			continue
		}
		body := rest
		fields := strings.Fields(body)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		if name != strings.ToUpper(name) || !strings.Contains(name, "_") {
			continue // a group title, not a variable
		}
		if _, dup := col[name]; dup {
			continue
		}
		col[name] = fields[len(fields)-1]
	}
	return col
}

// TestEnvNameClaimMatchesReality_WholeCatalog is the anti-drift gate: for EVERY
// row of the catalog, the env-name-as-flag claim must hold exactly as the help
// states it — a flag-bearing row dispatches, and a flagless row is refused with
// a message that names WHY (never the bare "unknown flag", which would send the
// operator hunting a typo that does not exist).
func TestEnvNameClaimMatchesReality_WholeCatalog(t *testing.T) {
	flagless := flaglessEnvNames()
	if len(EnvVars) == 0 {
		t.Fatal("catalog is empty — nothing to check")
	}
	if len(flagless) == 0 {
		t.Fatal("no flagless rows: the em-dash half of the table (and the refusal it explains) cannot be exercised")
	}

	var flagBearing, dashRefused int
	for _, e := range EnvVars {
		spelling := envNameAsFlag(e.Env)
		args := []string{flagPrefix + spelling, envProbeValue, "version"}
		_, err := FlagOverrides(args)

		if e.CLIFlag != "" {
			flagBearing++
			if err != nil {
				t.Errorf("%s has flag --%s, so the help's env-name claim covers it — but %q was refused: %v",
					e.Env, e.CLIFlag, args[0], err)
			}
			continue
		}

		// No flag of its own: the claim does NOT cover it, and the refusal must
		// say so by name rather than claim the name is unknown.
		dashRefused++
		if err == nil {
			t.Errorf("%s has no CLI flag, but %q was accepted — the help's em-dash column is then wrong",
				e.Env, args[0])
			continue
		}
		msg := err.Error()
		if strings.HasPrefix(msg, unknownFlagPrefix) {
			t.Errorf("%s: refusal reads %q — the name IS known (it is this catalog row); "+
				"the reason is that the knob has no CLI flag, and the message must say so (Rule 15)",
				e.Env, msg)
		}
		for _, want := range []string{e.Env, spelling, flaglessRefusalStem} {
			if !strings.Contains(msg, want) {
				t.Errorf("%s: refusal %q does not name %q", e.Env, msg, want)
			}
		}
	}
	if flagBearing == 0 {
		t.Fatal("no flag-bearing rows exercised")
	}
	t.Logf("catalog rows=%d: flag-bearing=%d dispatch, flagless=%d refused by name",
		len(EnvVars), flagBearing, dashRefused)
}

// TestHelpEnvTableEmDashMatchesFlaglessSet ties the two halves of ONE fact — the
// em dash the table prints and the set the parser refuses by name — to the same
// source. A row that gains or loses a flag moves BOTH or the gate is red.
func TestHelpEnvTableEmDashMatchesFlaglessSet(t *testing.T) {
	col := envFlagColumn(t, FormatEnvTable())
	if len(col) == 0 {
		t.Fatal("parsed no rows from the env table — the parser below is measuring nothing")
	}
	flagless := flaglessEnvNames()

	for _, e := range EnvVars {
		got, ok := col[e.Env]
		if !ok {
			t.Errorf("%s is in the catalog but not in the rendered table", e.Env)
			continue
		}
		if e.CLIFlag == "" {
			if got != emDashFlagLabel {
				t.Errorf("%s has no CLI flag, so its flag column must be the em dash %q, got %q",
					e.Env, emDashFlagLabel, got)
			}
			if _, refused := flagless[envNameAsFlag(e.Env)]; !refused {
				t.Errorf("%s renders as flagless but is NOT in the by-name refusal set", e.Env)
			}
			continue
		}
		if !strings.HasPrefix(got, flagPrefix) {
			t.Errorf("%s has flag --%s, so its row must show it, got flag column %q",
				e.Env, e.CLIFlag, got)
		}
		if _, stray := flagless[envNameAsFlag(e.Env)]; stray {
			t.Errorf("%s has flag --%s and must NOT be in the flagless refusal set",
				e.Env, e.CLIFlag)
		}
	}

	// Every flagless spelling must be refused BY NAME through the real parser —
	// the set and the behaviour are one fact, asserted in one place.
	for spelling, e := range flagless {
		_, err := FlagOverrides([]string{flagPrefix + spelling, envProbeValue})
		if err == nil {
			t.Errorf("--%s (%s) parsed, but it is in the flagless set", spelling, e.Env)
			continue
		}
		if !strings.Contains(err.Error(), e.Env) {
			t.Errorf("--%s refusal %q does not name %s", spelling, err, e.Env)
		}
	}
}

// TestHelpTextClaimsOnlyTheFlagBearingSubset pins the PROSE to the measured
// subset. The old sentence ("Any other knob is reachable with its full env var
// name as a flag") was a universal promise the parser kept for 36 of 80 rows;
// the promise must now name its subset and explain the em dash, and neither
// help surface may carry the universal form again.
func TestHelpTextClaimsOnlyTheFlagBearingSubset(t *testing.T) {
	usage := FormatUsage()
	if strings.Contains(usage, universalReachabilityClaim) {
		t.Errorf("`gopi -h` still carries the unqualified promise %q, which is false for the "+
			"%d of %d catalog rows with no flag of their own", universalReachabilityClaim,
			len(flaglessEnvNames()), len(EnvVars))
	}
	for _, want := range []string{reachabilityClaimSubset, emDashFlagLabel} {
		if !strings.Contains(usage, want) {
			t.Errorf("`gopi -h` does not state %q — the claim must name the subset it covers "+
				"AND explain the em dash the flag column prints", want)
		}
	}
	table := FormatEnvTable()
	for _, want := range []string{emDashFlagLabel, emDashFlagCaption} {
		if !strings.Contains(table, want) {
			t.Errorf("`gopi --help-env` preamble does not state %q — an em dash in the flag "+
				"column is otherwise an unexplained blank", want)
		}
	}
	if strings.Contains(table, "three equivalent spellings") {
		t.Error("the env-table preamble still claims three equivalent spellings for every knob")
	}
}
