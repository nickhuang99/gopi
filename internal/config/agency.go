package config

import (
	"os"
	"strconv"
	"strings"
)

// Agency-proposal pass knobs (epic agency-proposals). The pass is a LEVEL-3
// agency behaviour: gopi mines its OWN ledger for recurring patterns worth an
// improvement and proposes them as ordinary pending tasks. It is OFF unless
// explicitly enabled — the conservative default (prefer missing an
// improvement over flooding the queue).
//
//	env unset / empty → DISABLED (the default)
//	env truthy        → ENABLED  ("1"/"on"/"yes"/"true"/"enabled")
//	env anything else → DISABLED
//
// The REPORT-ONLY face (`gopi task improvements`) never consults this switch:
// reporting is free, writing is not.
const (
	// AgencyEnv turns the improvement-proposal pass on.
	AgencyEnv = "GOPI_AGENCY"
)

// AgencyProposalsEnabled reports whether the improvement-proposal pass may
// WRITE proposals. One env, one reader (Rule 2), truthy-only parsing via
// EnvBool — so a CLI/GOPI_AGENCY=off force-write can never read as enabled.
func AgencyProposalsEnabled() bool {
	return EnvBool(AgencyEnv)
}

// AgencyMinFamilySizeEnv overrides the recurrence-size gate (how many rows a
// task family needs before it is a pattern). Unset → the caller's measured
// named-constant default; malformed/non-positive → the default too, because a
// typo must never silently disable the gate.
const AgencyMinFamilySizeEnv = "GOPI_AGENCY_MIN_FAMILY"

// AgencyMinFamilySize resolves the recurrence-size gate. This is a POLICY
// knob (an operator tuning the pass on a young ledger); the shipped default is
// the distribution-measured constant.
func AgencyMinFamilySize(def int) int {
	raw := strings.TrimSpace(os.Getenv(AgencyMinFamilySizeEnv))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
