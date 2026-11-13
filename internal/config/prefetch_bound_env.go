package config

// The flags whose value is valid ONLY BEFORE the subcommand word.
//
// WHY THIS TABLE EXISTS. BootstrapEnvFromArgs peeks the token AFTER a
// flag to take its value, which is the ordinary `--name value` convention —
// and that convention has a failure mode when the value is OPTIONAL, as it is
// for a boolean switch. `gopi --memory-prefetch chat` (the flag written
// without a value, immediately in front of its verb) would peek past the verb,
// arm the knob with the literal word `chat`, and DELETE that word from argv —
// so the operator would silently get a bare run instead of the REPL he typed,
// with the knob reporting a value no human wrote.
//
// A flag in this table is therefore hoisted only while no non-flag word has
// been seen yet, which is exactly the window in which a TOP-LEVEL flag can
// appear. Once the verb is in, the token is left in argv for the subcommand's
// own (closed) vocabulary to accept or refuse — never silently consumed.
//
// This is deliberately a small table rather than a global change of behaviour:
// `--settings X`, `--home X`, `--output-language zh-CN` and the eq form of
// every flag keep the existing peek (their value is never optional), and the
// hoist for those must keep working after a verb for the pass-through cases
// (e.g. `gopi task run --settings X`).
func prefetchBoundEnv() map[string]bool {
	return map[string]bool{MemoryPrefetchFlagName: true}
}
