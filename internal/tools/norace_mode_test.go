//go:build !race

package tools

// raceEnabled is false in a normal (non-instrumented) test binary. See the
// race-tagged twin for why the guards care.
const raceEnabled = false
