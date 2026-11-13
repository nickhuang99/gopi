//go:build race

package tools

// raceEnabled reports that this test binary was built with the race
// detector, whose instrumentation makes even pure-Go work (the modernc/sqlite
// path in particular) several times slower. Scratch-root timing guards scale
// their budget by raceBudgetMultiplier so they measure the FILESYSTEM rather
// than the instrumentation.
const raceEnabled = true
