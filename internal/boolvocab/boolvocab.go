// Package boolvocab is the ONE definition of the boolean on/off spelling
// vocabulary (PROJECT.md Rule 14: a value shared across packages has exactly ONE
// definition, in a package every consumer can import).
//
// WHY IT IS A LEAF PACKAGE. The mirroring this package removes was not a
// cross-package reachability accident: internal/heavytest is a LEAF by design
// (in-package tests of internal/scratch and internal/tools import it, so it may
// not reach a package that could close a cycle — internal/config imports
// internal/scratch), and internal/agent / internal/tools each carried their own
// copy rather than take a dependency. A package with ZERO project imports is
// importable from every one of those positions, so the single definition is
// reachable where a mirror used to be the only option. The precedent is
// internal/toolnames (a tiny exported-constant leaf).
//
// THE DEFECT THIS CLOSES (2026-08-29 / 2026-08-30, twice). config.ApplyEnv
// force-writes the canonical "off" into the environment for every disabled
// switch. A reader that tested `os.Getenv(...) != ""` treated that literal
// "off" as ENABLED — the full raw request/stream dump flooded stderr, and
// deepseek's retry logging went dark. Each of the six copies of the spelling
// list was patched independently for it. One list means the next fix lands
// once.
package boolvocab

import "strings"

// TrueValues / FalseValues are the canonical spelling lists, comma-separated so
// they read the same way in a doc comment, a settings schema or a test table.
// They are the DOMAIN of every boolean env/flag/settings channel: "truthy-only"
// means the enabling spellings are exactly these, and a canonical force-written
// "off" is never one of them.
const (
	// TrueValues are the spellings that ENABLE a switch (EnvBool's truthy set).
	TrueValues = "1,true,on,yes,enabled"
	// FalseValues are the spellings that DISABLE a switch.
	FalseValues = "0,false,off,no,disabled"
)

// TrueSet / FalseSet are the parsed token sets (Rule 1: parsed one place, so
// every consumer probes a map instead of re-spelling a switch).
var (
	TrueSet  = newSet(TrueValues)
	FalseSet = newSet(FalseValues)
)

// newSet parses a comma-separated spelling list into a token set. Tokens are
// lower-cased and trimmed so callers pass an already-normalized key.
func newSet(list string) map[string]bool {
	set := make(map[string]bool)
	for _, tok := range strings.Split(list, ",") {
		if tok = strings.TrimSpace(strings.ToLower(tok)); tok != "" {
			set[tok] = true
		}
	}
	return set
}

// Normalize canonicalises one spelling: lower-case + trim outer whitespace.
// Every former mirror applied exactly this, so a consolidated reader sees the
// same key its private copy saw (behaviour-equivalence).
func Normalize(raw string) string {
	return strings.TrimSpace(strings.ToLower(raw))
}

// IsTrue reports whether a raw value carries one of the ENABLING spellings
// (TrueValues). Everything else — including "0", "off", "false", "no" and the
// empty string — is false.
func IsTrue(raw string) bool {
	return TrueSet[Normalize(raw)]
}

// IsFalse reports whether a raw value carries one of the DISABLING spellings
// (FalseValues). A default-ON switch is `!IsFalse(raw)`: an unrecognized value
// leaves it on, exactly as every former falsey list behaved.
func IsFalse(raw string) bool {
	return FalseSet[Normalize(raw)]
}
