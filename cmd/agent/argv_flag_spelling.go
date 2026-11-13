package main

// argvFlagPrefix is the argv spelling of a flag NAME: the flag sets store the
// bare name (`peer`), while an operator, a refusal message and a proxy scan all
// carry the dashes (`--peer`). Composing one from the other is why this exists
// as a constant instead of a literal typed at each site (Rules 1b/13/14).
//
// WHY IT LIVES IN A FILE OF ITS OWN. It was `syncFlagPrefix`, defined inside
// cmd/agent/cli_tasks_sync.go beside the cross-node sync CLI grammar. That
// whole grammar went with the sync verbs, but this ONE atom never belonged to
// it: its readers are the generic flag scanner (cli_memory.go's
// memoryFlagSet.optionalValue), the notify proxy scan (cli_notify.go), the
// session arity pass (cli_session_flag_arity.go) and the voice verb's refusal
// text (cli_voice.go) — none of which is a peer protocol. Deleting the file
// without carrying it out would have left four non-sync call sites undefined,
// and a `const syncFlagPrefix = "--"` re-created under a sync filename is the
// defect Rule 43 names: the removal judge is the reference DISTRIBUTION, not
// the file name.
const argvFlagPrefix = "--"
