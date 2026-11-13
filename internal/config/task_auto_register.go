package config

// The task-ledger AUTO-REGISTER kill switch — the ONE home of its NAME.
//
// WHY THIS CONSTANT LIVES IN config (Rule 14: a value shared across packages
// has exactly one definition, in a package every consumer can import). The name
// is read by THREE parties that cannot import each other:
//
//   - internal/agent READS it (`autoTaskRegisterEnabled` → its start hook),
//   - internal/tools must NAME it for the process spawners it hands an
//     environment to (the round child, the merger's resolve child), and
//     `internal/agent` imports `internal/tools`, so tools can never import
//     agent back,
//   - internal/testutil must DROP it from a suite's process environment
//     (PinSuiteAutoRegisterEnv), and testutil is imported BY both of those
//     packages' test binaries — importing either one back would be a cycle.
//
// Until 2026-10-29 the two production sides each carried their own string
// literal for this name (`agent.autoTaskRegisterEnv` and
// `tools.AutoRegisterOffEnv`), held in step only by a comment. That is Rule 13's
// defect class: two definition sites that drift the moment one is renamed, with
// nothing but prose holding them together. config is the leaf both of them
// already import, so one reference replaces two literals.
//
// SEMANTICS ARE NOT HERE. "What value means OFF" is internal/agent's
// (autoTaskRegisterEnabled / AutoTaskRegisterOffValue); this file answers only
// the SPELLING.
const TaskAutoRegisterEnv = "GOPI_AUTO_TASK_REGISTER"
