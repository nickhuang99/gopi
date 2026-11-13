package tools

// lock_owner.go — the ONE environment name that carries "an ancestor of mine
// holds the single-instance work lock, and a descriptor came with me".
//
// WHY IT LIVES HERE (Rule 14). Three packages need the same spelling: the process
// that STAMPS it into a child's environment (cmd/agent's merge_lock.go), the
// process that READS it back and has to PROVE the claim from the kernel before
// trusting it (cmd/agent's mergerLockHeldByAncestor), and the diagnostics that
// explain it. A second spelling anywhere in that chain fails SILENTLY in the
// worst direction: the child would find no marker, never treat itself as
// admitted, and either refuse a face its own parent is waiting on or re-take a
// lock that is already held. So it is ONE constant in the leaf package all three
// import — the same arrangement the tool wire names use (internal/toolnames).
//
// IT IS EVIDENCE, NEVER AUTHORITY. Anything on the host can set an environment
// variable, so a process that finds this marker must still show that a LIVE
// ANCESTOR of its own holds the lock — read from the kernel (LockEvidences +
// AncestorPIDs), not from the environment. The marker says only WHERE the claim
// came from; the kernel says whether it is true.
//
// NAMED AFTER THE TAKE-OVER THAT FIRST STAMPED IT (epic merger-takeover-lock,
// 2026-09-17). The take-over launcher is deleted (epic merger-delete-dead-impl,
// 2026-09-19) and the marker outlived it, because the property it carries was
// never the take-over's: it is the ADMISSION half of the work lock, which every
// live merge face still takes. The name is kept deliberately — renaming it would
// change a wire value that a running older process may still carry.
const TakeoverLockOwnerEnv = "GOPI_MERGER_LOCK_OWNER"
