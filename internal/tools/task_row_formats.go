package tools

// The ONE spelling of each `gopi task` row line's verb list.
//
// They live in their own file, and they are named constants rather than inline
// string literals, because the arity is a CONTRACT the compiler checks: a
// template with more verbs than the argument list supplies renders Go's
// literal "%!(EXTRA …)" marker into the row, and one with fewer renders
// "%!d(MISSING)" — both land in the operator's view of the ledger as if they
// were row data. A constant format string is what lets `go vet`'s printf check
// refuse a mismatch at build time (measured: appending one verb to
// taskListRowFormat fails the build with "format %s reads arg #14, but call
// has 13 args").
//
// The guard that holds the other half — that a row whose summary is ITSELF
// full of fmt markers and live verbs echoes it verbatim, data never being a
// format string — is TestTaskRendersCarryNoFmtMarkers
// (task_render_pct_marker_test.go), the same guard the shot usage template has
// carried since cli_shot_surface_test.go.
const (
	taskListRowFormat   = "#%s  [%s]  %s  (model %s, source %s, created %s)%s%s%s%s%s%s%s%s\n"
	taskSearchRowFormat = "%s  [%s]  %s  (session %s, created %s)\n"
)
