// Package chatpolicy is the RULE LAYER of the chat console's write boundary:
// a three-valued decision (allow / prompt / forbidden), a parser for the
// ```chat-policy fence of the operator's GOPI.md, and a prefix matcher that
// turns a tokenised command line into one of those three answers.
//
// TWO ORTHOGONAL LAYERS, deliberately kept in separate places:
//
//   - this package answers "the rules say what about THIS command?" — it knows
//     nothing about startup, environment variables, settings files or a
//     session. Its input is the fence's text, its output is a Policy.
//   - the POSTURE (internal/agent/chat_policy_posture.go) answers "what
//     happens when no rule matched?" — resolved ONCE at startup and frozen, so
//     no code running inside the process can widen it later.
//
// WHY THE SPLIT (measured precedent, codex-rs/execpolicy + hermes
// tools/approval.py): a single hardcoded judge conflates the two questions, so
// every policy change becomes a code change. Here a new rule is a line the
// operator writes, and a new default-answer is one startup switch.
//
// THE VOCABULARY IS THREE VALUES, never two (decision.go): Prompt is a real
// outcome — "the operator must be asked" — and it is neither Allow nor
// Forbidden. A two-valued judge can only lie about it one way or the other.
//
// THIS PACKAGE IS NOT WIRED INTO ANY CALL SITE YET. chat_write_guard.go keeps
// its behaviour byte-for-byte; this is the rule engine those call sites will
// consume in the following work items. Nothing here reads the environment, the
// filesystem or a global: every function is a pure function of its arguments
// (Rule 2 — one place decides; the callers own their channels).
//
// ZERO THIRD-PARTY DEPENDENCIES: standard library only, no TOML/YAML — the
// rule syntax is the fenced plain-text form designed in parse.go.
package chatpolicy
