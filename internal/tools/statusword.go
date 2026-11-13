package tools

// statusword.go — the ONE definition of the separator that a `status:` field
// uses to chain MEASURED markers after its status WORD (Rule 14: a value more
// than one package depends on lives in one exported constant in a leaf; Rule 1:
// never type it inline).
//
// WHY IT IS EXPORTED. `task show` renders `status: <word>` and, when a
// measurement was taken, appends a marker after it (` + holder dead (pid N
// gone)`, taskholder_gone.go's taskHolderGoneMarkerFmt and its `recycled`
// sibling). Any caller that compares the status WORD therefore reads a field
// that MAY carry markers, and has to know where the word ends — so the render
// site is the only place that may define the boundary. Keeping the constant
// test-side (where it started, cmd/agent/cli_tasks_send_cancel_test.go's
// statusWordMarkerSep) put a PRODUCTION rendering decision in a test file and
// let the two spellings drift.
//
// THE ORDERING INVARIANT, and the reason one constant is enough: markers are
// appended AFTER the word, so trimming at the FIRST occurrence can never cut a
// status value in half — no value in taskStatusValues contains the separator.
// TestStatusWordMarkerSepIsAbsentFromEveryStatusValue is the guard for that
// half; the cmd/agent splitter (statusWord) is the guard for the other.
const StatusWordMarkerSep = " + "
