package ledgerwindow

import (
	"strings"
)

// The window predicate, assembled from the named tokens above. Two call shapes
// are supported on purpose:
//
//   - Where(column) returns a predicate with the two bounds as `?`
//     placeholders, in the order Since then Until, for a caller that passes the
//     values to Query/QueryRow.
//   - WhereLiteral(column, window) returns the same predicate with the bounds
//     inlined as SQL literals, for surfaces that BUILD SQL text (the CLI/script
//     report surfaces do; they have no parameter binding to hand).
//
// Both spell every token through the constants in ledgerwindow.go, so the
// 'utc' modifier cannot be dropped in one shape and kept in the other.

// SQL punctuation, named (Rule 1b): the gate derives the correct predicate's
// text from these, so a change here changes the detector with it.
const (
	// CommaSQL separates a function's arguments.
	CommaSQL = ","
	// OpenParenSQL / CloseParenSQL delimit a function call.
	OpenParenSQL  = "("
	CloseParenSQL = ")"
	// QuoteSQL is the SQL string-literal delimiter.
	QuoteSQL = "'"
	// QuoteEscapeSQL is SQL's doubled-quote escape inside a string literal.
	QuoteEscapeSQL = "'" + "'"
	// FormatEpochSecondsSQL is the conversion specifier as SQL sees it: a
	// string literal. Passing the bare %s would be a syntax error, which is
	// how this constant came to exist — the format is an argument like any
	// other, and it must be quoted like any other.
	FormatEpochSecondsSQL = QuoteSQL + FormatEpochSeconds + QuoteSQL
)

// PlaceholderSQL is the SQL parameter placeholder both bound shapes emit.
const PlaceholderSQL = "?"

// SpaceSQL is the single space the predicate's words are joined with.
const SpaceSQL = " "

// Where renders the interval predicate over a ledger time column, with the
// bounds as placeholders: `claimed_at BETWEEN strftime('%s',?,'utc') AND
// strftime('%s',?,'utc')`.
//
// The caller supplies the two wall-clock strings, Since first.
func Where(column string) string {
	return column + SpaceSQL + BetweenKeyword + SpaceSQL + boundExpr(PlaceholderSQL) +
		SpaceSQL + AndOperator + SpaceSQL + boundExpr(PlaceholderSQL)
}

// WhereLiteral renders the same predicate with the two wall-clock bounds
// inlined as SQL string literals — the shape a report surface that prints its
// own SQL needs. The bounds are quoted (QuoteLiteral), so a value carrying a
// quote cannot break out of the literal.
func WhereLiteral(column string, w Window) string {
	return column + SpaceSQL + BetweenKeyword + SpaceSQL + boundExpr(QuoteLiteral(w.Since)) +
		SpaceSQL + AndOperator + SpaceSQL + boundExpr(QuoteLiteral(w.Until))
}

// boundExpr renders one end of the interval: the epoch-seconds reading of a
// wall-clock value, converted the way the ledger was stamped.
//
// `value` is either PlaceholderSQL or a quoted literal; the 'utc' modifier is
// appended by the modifier list, so no caller can assemble this expression and
// omit it (the reason the modifier is a constant and not a typed string).
func boundExpr(value string) string {
	args := strings.Join([]string{FormatEpochSecondsSQL, value, UTCModifierSQL}, CommaSQL)
	return FuncStrftime + OpenParenSQL + args + CloseParenSQL
}

// QuoteLiteral renders s as a SQL string literal (quoting it and doubling any
// embedded quote), so a caller-supplied bound cannot terminate the literal
// early. It is the ONLY place this package writes a quote around a value.
func QuoteLiteral(s string) string {
	return QuoteSQL + strings.ReplaceAll(s, QuoteSQL, QuoteEscapeSQL) + QuoteSQL
}

// The reporting query the surfaces share: how many rows a column's epoch stamp
// falls inside the window. Its words are constants for the same Rule 1b reason
// as everything else here, and TableTasks is the name the ledger's own DDL
// creates (the table the columns above live in).
const (
	// TableTasks is the task-ledger table.
	TableTasks = "tasks"
	// SelectCountPrefix opens a counting query.
	SelectCountPrefix = "SELECT COUNT(*) FROM " + TableTasks + " WHERE "
)

// CountQuery renders the shared count query over one epoch column. No timestamp
// arithmetic appears in it: the comparison is BETWEEN two epoch values, exactly
// as the ledger stores them, and the conversion of the wall-clock bounds is the
// predicate's job (Where).
func CountQuery(column string) string {
	return SelectCountPrefix + Where(column)
}
