package agent

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// envInt reads the integer env var name. When the var is unset, empty, not a
// valid integer, or fails the valid(n) range check, it emits a
// "[gopi] warning:" line to stderr naming the var and the default, then
// returns def. Otherwise it returns the parsed value. display is how the
// default is rendered in that warning (varies: an int, a float, or a
// time.Duration like "600s"), matching each call site's historical text.
//
// This is the single home for the "Getenv → Atoi → validate → warn → fall
// back" template that previously repeated ~14× across loop.go and compact.go
// with per-site drifts in the range rule and warning format.
func envInt(name string, def int, display interface{}, valid func(int) bool) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || !valid(n) {
		fmt.Fprintf(os.Stderr, "[gopi] warning: invalid %s=%q, using default %v\n", name, v, display)
		return def
	}
	return n
}

// envDurationSeconds is envInt for a time.Duration setting expressed in
// whole SECONDS: unset/empty/0/invalid → def (0 is the documented "use the
// default" spelling, so it must not warn), a positive integer → that many
// seconds. The caller passes the EnvName constant, keeping Rule 1b/14
// (one definition, referenced — never a bare literal).
func envDurationSeconds(name string, def time.Duration) time.Duration {
	secs := envInt(name, int(def/time.Second), def, func(n int) bool { return n > 0 })
	return time.Duration(secs) * time.Second
}

// envFloat is envInt for float values, with the same validate-and-warn
// contract.
func envFloat(name string, def float64, display interface{}, valid func(float64) bool) float64 {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || !valid(f) {
		fmt.Fprintf(os.Stderr, "[gopi] warning: invalid %s=%q, using default %v\n", name, v, display)
		return def
	}
	return f
}
