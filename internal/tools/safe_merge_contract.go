// safe-merge GUARD CONTRACT — the Refusal vocabulary shared by the guard's
// TWO homes (Rule 14: one definition, never a mirror).
//
// The guard has two homes by construction, and neither can import the other:
//
//	home A (bash) — scripts/safe-merge.sh, the pre-merge gate PROJECT.md Rule 9
//	                orders a human/PA to run before any merge; its shared
//	                vocabulary lives in scripts/lib/safe-merge-contract.sh.
//	home B (Go)   — mergeExecSafeMergeBlock (task_merge_executor.go), step ③
//	                of the merge-backlog executor: the SAME guard, run by
//	                `gopi task merge-execute` instead of by a human.
//
// Go cannot source a shell library, so the vocabulary exists twice — and that
// is exactly the drift this file's whole purpose is to make visible and
// checked, not to hide: `TestSafeMergeContractShellVocabularyParity`
// (safe_merge_contract_test.go) parses scripts/lib/safe-merge-contract.sh and
// fails when a kind token or a rendered sentence stops agreeing with the Go
// constant below, in either direction. A refusal kind exists in BOTH homes or
// it does not exist (Rule 1b: no bare literal carries it inline).
//
// THE PAIRING IS THE CONTRACT. The defect that motivated [e]/[f] was not a
// missing check in one home — it was a guard that certified a GHOST branch
// "safe to merge" (4 green lines, exit 0) while the executor's mirror of the
// same guard reported the resulting failure as "merge conflict". Two homes,
// two different stories about one branch. A change to one home's refusals is
// therefore a change to this file first.
package tools

// The refusal KINDS — the machine-readable token each home reports for a hit.
// The bash side emits them in the conclusion line
// ("拒绝种类: missing-ref unrelated-history"); the Go side returns the kind
// inside the reason string it hands the caller. They are spelled identically
// and the parity test is what keeps it that way.
const (
	// safeMergeRefusalArchiveTag is guard check [a]: the branch carries
	// archive/<branch> — an archived loser; the ledger already said no.
	safeMergeRefusalArchiveTag = "archive-tag"

	// safeMergeRefusalBlacklistPrefix is guard check [b]: the branch name
	// hits a blacklist prefix (loser/, archive/).
	safeMergeRefusalBlacklistPrefix = "blacklist-prefix"

	// safeMergeRefusalWorktreeOccupied is guard check [c]: the branch is
	// checked out in a linked worktree — merging it would silently not
	// apply the branch's commits (or git would refuse the checked-out ref).
	safeMergeRefusalWorktreeOccupied = "worktree-occupied"

	// safeMergeRefusalMissingRef is guard check [e]: THE GHOST BRANCH.
	// `git rev-parse --verify --quiet refs/heads/<b>^{commit}` exits 1 with
	// NO output — the ref does not resolve (deleted after an e2e probe,
	// pruned after a merge, or simply mistyped). It is the check that was
	// missing while four green lines certified such a name "全部通过 ——
	// 可安全 merge", sending a whole solver session after a branch that
	// does not exist.
	safeMergeRefusalMissingRef = "missing-ref"

	// safeMergeRefusalUnrelatedHistory is guard check [f]: the ref resolves
	// but shares NO commit with the integration branch (an orphan root —
	// a scratch `git init` pushed into the real repository). `git merge`
	// accepts it only under --allow-unrelated-histories, so EVERY merge
	// channel fails on it forever.
	safeMergeRefusalUnrelatedHistory = "unrelated-history"

	// safeMergeRefusalProbeFailed is the third answer of a two-answer
	// question: the probe could not be answered at all (not a repository,
	// a broken object store, a language barrier in the message). Rule 15:
	// "I cannot tell" is NOT "safe" — the guard refuses rather than
	// certifying. In bash it is a refusal kind; in Go the probe error is
	// preserved and wrapped, which is the same refusal carried as an error.
	safeMergeRefusalProbeFailed = "probe-failed"
)

// The RENDERED sentences. Both homes print the same words for the same fact;
// a human who reads the bash gate's output and a human who reads the ledger's
// failed-row reason must be reading the same refusal.
const (
	// safeMergeMsgMissingRef is the [e] sentence; %s is the branch name.
	safeMergeMsgMissingRef = "分支不存在 —— 禁止 merge (git rev-parse --verify --quiet refs/heads/%s 无法解析该 ref)"

	// safeMergeMsgUnrelatedHistory is the [f] sentence; %s is the
	// integration branch name (master).
	safeMergeMsgUnrelatedHistory = "与 %s 无共同历史 (孤儿根 —— git merge 只在 --allow-unrelated-histories 下才接受) —— 禁止 merge"

	// safeMergeMsgProbeFailed is the inconclusive-probe sentence; %s is
	// git's own diagnostic text.
	safeMergeMsgProbeFailed = "ref 存在性探测无法回答 (git rev-parse 报: %s) —— 不确定既不是缺失也不是安全，禁止 merge"

	// safeMergeMsgAncestryProbeFailed is the [f] probe's own unanswerable
	// sentence. It is a SEPARATE sentence from safeMergeMsgProbeFailed on
	// purpose: sharing one rendered line made a git failure in the ancestry leg
	// print "ref 存在性探测无法回答 (git rev-parse 报: exit 1)" — naming a probe
	// that never ran and a reason that never happened (measured 2026-09-23).
	safeMergeMsgAncestryProbeFailed = "历史相关性探测无法回答 (git merge-base --all 报: %s) —— 不确定不等于有共同历史，禁止 merge"
)

// The PROBE. The guard asks ONE question — "do these two refs share history?" —
// via `git merge-base --all <integration> <branch>`, and the EXIT STATUS and
// the OUTPUT together are the answer:
//
//	exit 0, non-empty output — common history → pass.
//	exit 1, EMPTY output     — no common ancestor → refuse (unrelated-history).
//	                           MEASURED 2026-09-23 on a real orphan root: git
//	                           exits 1 here, not 0 — "empty output" alone is not
//	                           the signature, the PAIR (exit, output) is.
//	any other shape          — git cannot answer (including exit 0 with empty
//	                           output) → refuse (probe-failed, Rule 15; never
//	                           collapsed into "safe").
//
// WHY NOT `merge-base --is-ancestor`, which this guard used first: "one is an
// ancestor of the other" is a STRICTER question than "they share history", and
// it answers 1 in BOTH directions for the ordinary case — two branches that
// descend from a common base and have since diverged, i.e. every real feature
// branch. An is-ancestor probe therefore refuses exactly the merges this guard
// exists to let through (measured 2026-09-23: it refused the plain
// `master`/`feat/e2e-merge` fixture in TestMergeExecConflict, which shares a
// base commit with master and simply has one more commit on top).
//
// safeMergeAncestryProbe names the command so the two homes cannot drift.
const safeMergeAncestryProbe = "merge-base --all"

// safeMergeAncestryRefusal is the exit status that — with EMPTY output — is
// git's own named answer for "no common ancestor". MEASURED 2026-09-23 on a
// real orphan root (a commit built on the empty tree via `git commit-tree`):
// `git merge-base --all <related> <orphan>` exits 1 and prints nothing. It is a
// named constant because the ORPHAN SIGNATURE is the PAIR (exit, output), not
// either half: "output is empty" alone is also what a broken invocation looks
// like, and "exit 1" alone is also what --is-ancestor returns for every
// ordinary divergent branch.
const safeMergeAncestryRefusal = 1

// safeMergeRefusalWanted marks the guard's return-channel convention: the Go
// mirror reports a hit as a wrapped error (never as an unwrapped string the
// caller might swallow — Rule 15), so the operator-facing ledger reason names
// the branch that does not resolve instead of the misleading "merge conflict"
// the merge leg would report after git rejects the ghost.
//
// Kept as a documented marker rather than a bool parameter so the mirror's
// signature stays a pure `(repo, branch) → error` — the shape both the
// executor step ③ and the tests drive.
const _ = safeMergeRefusalWantedDoc

const safeMergeRefusalWantedDoc = "refusals are returned as errors (never a bare string), so a caller cannot ignore one by discarding a value"
