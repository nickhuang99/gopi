package main

// ledger_agreement_prefix.go — the ONE spelling of the `[ledger-agreement] `
// line tag.
//
// RELOCATED HERE (Rule 43) when the ledger-agreement PRECONDITION was retired
// from the worker's dispatch path: the constant lived beside the automatic
// announcement (task_ledger_agreement.go), but its reference distribution was
// not confined to that mechanism — cmd/agent/cli_tasks_sync.go prints the tag
// on `gopi task diff`'s UNCHECKED line, and that on-demand verb is KEPT. A
// symbol with a caller outside the deleted mechanism is a general tool, so it
// moves to its own file in the SAME commit as the deletion (a split would leave
// the intermediate commit uncompilable).

// ledgerAgreementLinePrefix tags every line the ledger-agreement report prints,
// so a run's log can be grepped for the verdict. Its remaining caller is the
// explicit diagnostic `gopi task diff --peer <alias>` — the manual, always-fresh
// comparison; the automatic per-dispatch announcement that used to share the
// tag is gone.
const ledgerAgreementLinePrefix = "[ledger-agreement] "
