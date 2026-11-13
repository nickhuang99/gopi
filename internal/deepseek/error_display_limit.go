package deepseek

// maxErrorDisplayBytes caps how much of an API error body is shown in a
// returned error message — the non-200 branch of retryDo, the 1214
// degradation-ladder failure, and the ClaudeChat non-200 tail. The full body
// still reaches isAccountLevelStatus via non200BodyProbeMax.
//
// PROJECT.md Rule 1/13: the literal 500 was inlined at three sites; it is declared
// ONCE here and every site references it, so the shown length cannot drift from
// the classified length.
//
// Why a separate file rather than the top of retry.go: normative devdocs cite
// lines in retry.go by LINE NUMBER (devdoc/audit-ab3-zbook-2026-09-04.md names
// `internal/deepseek/retry.go:141` and `:186`; 39 such citations exist across
// the corpus), and an inserted declaration block shifts them off their symbols.
// A new file adds no line to any existing file, so those citations stay put —
// that is the reason, and it still holds.
//
// HISTORY, so nobody satisfies a check that does not exist. This used to read
// "the doc-anchor gate turns red": the deleted devdoc line-anchor gate asserted
// that devdoc line anchors resolved, and it was deleted with internal/docclass
// in 5a35f61. Nothing resolves those 39 citations now — they are prose that can
// rot silently. The census that replaced it over CONFIG.md's rows
// (`go run internal/standards/testdata/config-anchor-census.go . <table> --check`,
// its TestMergeConfigAnchorGate* arms in internal/tools) did not look at
// retry.go either, and it went with the documentation-citation family in
// 78bdf05a (2026-11-05): nothing executable checks an anchor on this tree.
const maxErrorDisplayBytes = 500
