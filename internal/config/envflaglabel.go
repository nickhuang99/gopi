package config

// EnvVarsFlagLabels exports the catalog's flag COLUMN — one entry per row of
// EnvVars, with the flag face exactly as `gopi --help-env` renders it (`--flag`
// for a row that has one, the em dash for a row that does not).
//
// WHY IT EXISTS: a documentation gate (internal/standards) asserts that
// CONFIG.md §2c's flag column agrees with the catalog. The far side of that
// comparison must be the SAME rendering the operator reads in the help output —
// otherwise the doc, the help and the gate hold three opinions about a knob's
// flag face and only two of them can be right. envFlagLabel is that rendering
// and was unexported, which left the gate with two bad choices: mirror the
// formatting (Rule 13's defect) or re-derive it from CLIFlag (which is the
// bare, dashless spelling — `tmpdir`, not `--tmpdir` — so every row would have
// to know the prefix convention too). This wrapper is the third choice: one
// exported accessor over the one definition.
//
// It is deliberately a COPY, not a live view: a caller cannot mutate the
// catalog through it, and the rows are returned in EnvVars order.
//
// Help rides along because it is the catalog's own statement of a knob's flag
// FACE when that face lives on a subcommand: a row with no agent-level flag can
// still say "…also the --every flag, which wins" in its help text, and a doc
// gate needs that sentence to score a cell naming a subcommand flag without
// inventing an allow-list of its own.
func EnvVarsFlagLabels() []EnvFlagLabel {
	out := make([]EnvFlagLabel, 0, len(EnvVars))
	for _, e := range EnvVars {
		out = append(out, EnvFlagLabel{Env: e.Env, Key: e.Key, Flag: envFlagLabel(e), Help: e.Help})
	}
	return out
}

// EnvVarsNoFlagMarker is the flag column's value for a catalog row that has no
// flag of its own, exported so a consumer comparing a rendered column needs no
// second spelling of it (Rule 13/14: one definition, referenced never mirrored).
const EnvVarsNoFlagMarker = emDashFlagLabel

// EnvFlagLabel is one catalog row's identity and flag face, as rendered.
type EnvFlagLabel struct {
	// Env is the row's environment-variable name — the join key a caller matches
	// a documentation row against.
	Env string
	// Key is the row's settings.json key ("" when the knob has none).
	Key string
	// Flag is the rendered CLI flag face: "--name", or EnvVarsNoFlagMarker when
	// the row has no flag of its own.
	Flag string
	// Help is the row's one-line help, the catalog's own record of a flag face
	// that lives on a subcommand rather than on the agent flag parser.
	Help string
}
