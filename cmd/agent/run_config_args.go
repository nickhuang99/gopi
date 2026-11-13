package main

import "github.com/nick/gopi/internal/config"

// catalogConfigArgs reduces a VERB FACE's argv to the words the configuration
// resolution may see.
//
// WHY THE REDUCTION EXISTS. resolveRunConfig takes the argv of the RUN path
// (`gopi "<prompt>" --settings X`), whose flag vocabulary IS the catalog. A verb
// face has flags of its own (`gopi chat --tools`), so handing its argv straight
// to config.FlagOverrides would refuse the face's own switches as unknown flags
// — a refusal about the wrong command line. The reduction answers ONE question
// per word ("is this a CATALOG flag, and does it take a value?"), and it is
// asked through the catalog's OWN exported predicates: config.CatalogFlagArity
// reads the same arity table FlagOverrides parses with, and its operand is split
// with config.CatalogFlagValueTaken / config.IsCatalogBoolValue — the three
// predicates the resume face already uses. So a knob added to the catalog is
// accepted by every face the same day, and no face keeps a list to forget
// (Rules 2/13/14). The PRECEDENCE (flag > env > settings > default) lives in
// resolveRunConfig and is NOT restated here: this function reorders nothing and
// decides nothing — it only decides which words are the resolution's business.
//
// consumed is 1:1 with args: consumed[i] is true when args[i] was appended to
// configArgs (the flag itself) or swallowed as that flag's operand. A caller
// that also has to read the REMAINING words (the resume face builds its prompt
// from them) gets the same walk instead of a second one.
func catalogConfigArgs(args []string) (configArgs []string, consumed []bool) {
	consumed = make([]bool, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		needsValue, known := config.CatalogFlagArity(a)
		if !known {
			continue
		}
		configArgs = append(configArgs, a)
		consumed[i] = true
		// `--name=value` carries its operand inside the token: nothing to
		// consume, and an empty operand is the parser's own refusal to make.
		if _, inline := config.CatalogFlagValueTaken(a); inline {
			continue
		}
		if i+1 >= len(args) {
			continue // dangling flag: the parser names the missing value
		}
		next := args[i+1]
		if needsValue {
			if isFlagWord(next) {
				// The operand is another flag: the parser's own
				// "flag --name requires a value" is the sentence that names
				// this, and it needs to SEE both words to say so.
				continue
			}
			configArgs = append(configArgs, next)
			consumed[i+1] = true
			i++
			continue
		}
		// Boolean knob: the parser consumes a following token only when it is a
		// boolean spelling, and the caller's word collector must consume the
		// SAME token — or `--debug false "run tests"` leaves `false` in the
		// prompt.
		if config.IsCatalogBoolValue(next) {
			configArgs = append(configArgs, next)
			consumed[i+1] = true
			i++
		}
	}
	return configArgs, consumed
}

// isFlagWord answers "does this token read as an OPTION rather than a value".
// A lone "-" is stdin, and every caller in this package treats it as a value.
func isFlagWord(a string) bool {
	return len(a) > 1 && a[0] == '-'
}
