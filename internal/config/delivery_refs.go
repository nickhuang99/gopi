package config

// delivery_refs.go — the NAMES of the ref namespaces this host's delivery lane
// mirrors the peer's integration tip under, in the one place every consumer can
// import (Rule 14: a value shared across packages has exactly ONE definition,
// in a small exported-constant file, and consumers REFERENCE it — a mirror
// constant is a bare literal in disguise, Rule 13).
//
// WHY THESE TWO NAMES AND NOTHING ELSE. Two namespaces carry the peer's tip on
// a client host, and they mean different things:
//
//   - `origin` — the checkout's ORDINARY remote-tracking namespace, refreshed
//     by any fetch the operator or the clone channel runs. This is the mirror a
//     staleness reading may trust on an unconfigured host.
//   - `gitPushcheck` — the push-back lane's PRIVATE namespace
//     (internal/tools/task_sync_git_pushback.go), written by a forced fetch onto
//     a ref no worktree holds, and pruned by that lane's own pass. On a host
//     with no push-back lane it can sit frozen for days, which is a fact its
//     readers must know rather than a reason to ignore it.
//
// The VALUES live here so `internal/tools` and `cmd/agent` name the same bytes;
// neither of them may spell a second copy (Rule 13's mirror gate fails a
// duplicated literal).
const (
	// DeliveryOriginRemoteName is the ordinary remote the checkout's
	// remote-tracking refs hang under (`refs/remotes/origin/…`).
	DeliveryOriginRemoteName = "origin"

	// DeliveryPushbackMirrorNamespace is the push-back lane's private mirror
	// namespace (`refs/remotes/gitPushcheck/…`).
	DeliveryPushbackMirrorNamespace = "gitPushcheck"
)
