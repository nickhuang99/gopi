package tools

// git_peer_fetch.go — THE PEER FETCH: ONE implementation, ONE runner
// (epic worktree-base-align, 2026-09-26).
//
// WHY THIS FILE EXISTS. The capability "read the peer's refs into this repo"
// had three implementations, each with its own argv, its own refspec and —
// the defect this file removes — its own `exec.CommandContext(…, "git" …)`
// wrapper:
//
//	① the push-back lane's peer-master mirror   (gitPushbackFetchMaster,
//	   task_sync_git_pushback.go),
//	② the close-out's own peer resolution (task_finish_cleanup.go,
//	   which called ① — so it inherited ①'s argv by accident of history
//	   rather than by contract),
//	③ the client serve pass's fetch-prune step  (task_sync_git.go).
//
// Three wrappers are three places a `-C` anchoring, a stderr convention or a
// timeout can drift apart, and three places that answer "what does fetching
// the peer mean here?" differently (Rules 13/14: a shared value has ONE home).
//
// WHAT IS SHARED, exactly, and what is deliberately NOT:
//
//   - gitPeerRun is the ONE `exec.Command*("git", …)` in the peer-fetch path.
//     gitSyncRun and gitPushbackGitRun are now thin adapters over it; their
//     ~24 call sites and every existing test assertion stay untouched.
//   - SyncPeerFetch is the ONE exported fetch: it mirrors the peer's
//     integration branch into the reserved ref gitPushbackCheckRef and
//     returns that tip's sha. Callers that need the tip (the push-back lane's
//     ancestry checks, the branch's own check, the ground's baseline) share it.
//   - The client pass's PRUNE step keeps its own refspec and stays separate,
//     because its DESTINATION is different and that difference is a measured
//     fix, not a style choice: it must land in the peer's remote-tracking
//     namespace (refs/remotes/<alias>/*) or the reap cannot happen at all.
//
// MEASURED 2026-09-26 on peerbox (fixture, /home/user/.gopi/sessions/
// 01a115ac-23d5-7b3b-a8d2-d6b33ac894b3/evidence/gq1): a fetch that carries
// `--prune` together with ONE explicit refspec (+) produced no prune at all —
// `refs/remotes/gitPushcheck/ghost` survived the fetch that was asked to prune
// it, and so did every other ref under that namespace. The refspec fetch's
// tracking is scoped to what it was given, so --prune on THIS call has
// nothing to reap; the same JSON-visible reap needs the remote's OWN refspec
// (`git fetch --prune <remote>`, no refspec), which is what the client arm's
// step ③ passes and why the ghost refs had to be measured empirically before
// this file could claim a prune. Requesting --prune here would therefore be
// decorative — and a decorative flag is worse than none, because it reads as
// a guarantee (Rule 15: never a silent non-effect). The mirror ref the
// mirror-fetch writes is instead cleaned by the pass itself
// (gitPushbackPruneMirrors), which is a positive statement about that ref's
// lifetime rather than a hope about git's.

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// gitPeerFetchVerb / gitPeerFetchForceFlag are git's fetch verb and the
	// force flag the mirror refspec needs: a peer whose master was rewound
	// must be MIRRORED here, not refused (the `+` refspec's contract).
	gitPeerFetchVerb      = "fetch"
	gitPeerFetchForceFlag = "--force"

	// gitPeerFetchNoTagsFlag is the one policy both destinations share: the
	// node contract is refs/heads + refs/remotes, so a peer's tag set never
	// enters this repo. The VALUE has one home — task_sync_git.go's client-arm
	// constant — and this name is a second REFERENCE to it, never a
	// re-spelling (Rules 1/13).
	gitPeerFetchNoTagsFlag = gitSyncFetchNoTagsFlag

	// gitPeerFetchPruneFlag names git's --prune for the client arm's step ③
	// (same reference, not a second literal).
	gitPeerFetchPruneFlag = gitSyncFetchPruneVerb

	// gitPeerFetchTimeout is THE timeout of one peer-fetch subprocess, and the
	// ONE definition of the value the two lane caps (gitSyncTimeout,
	// gitPushbackFetchTimeout) name. An unreachable or wedged peer must fail
	// fast — it must never hold a spawn, a serve tick or a close-out chain.
	gitPeerFetchTimeout = 60 * time.Second

	// gitPeerMirrorRefspecPrefix is the mirror refspec's leading character:
	// the refspec is a FORCED update, so a peer whose integration branch was
	// rewound is mirrored here rather than refused.
	gitPeerMirrorRefspecPrefix = "+"

	// gitPeerRevParseVerb / gitPeerVerifyFlag / gitPeerQuietFlag name the
	// revision read that turns the mirror ref back into a sha ("rev-parse
	// --verify --quiet <ref>^{commit}"): --verify + --quiet is what makes a
	// MISSING ref an error status rather than a printed name, and the
	// `^{commit}` suffix is what refuses a ref that resolves to something
	// other than a commit. Referenced from the one vocabulary this package
	// already keeps for those three tokens (Rule 14).
	gitPeerRevParseVerb = finishCleanupPreMergeRevParseVerb
	gitPeerVerifyFlag   = finishCleanupPreMergeVerifyFlag
	gitPeerQuietFlag    = finishCleanupPreMergeQuietFlag
	// gitPeerCommitSuffix is the revision suffix that requires a COMMIT.
	gitPeerCommitSuffix = "^{commit}"
)

// gitPeerRun is the ONE git runner of the peer-fetch path: `git -C <dir>`
// with the stderr captured into the error (Rule 15 — a caller must be able to
// report git's own words, not merely "exit status 128"), and the output
// trimmed at this single point so no caller has to remember to.
//
// The BOUND is the caller's: this function does not invent a timeout, because
// the three surfaces that reach it carry their own named cap (a serve tick's
// step, a push-back pass, a close-out gate) and a runner that overrode them
// would make those caps decorative.
//
// The environment is inherited verbatim — including an inherited GIT_DIR. That
// is deliberate and load-bearing: the callers that must survive a routed git
// environment strip it themselves, through the ONE helper that owns that list
// (withoutGitDirEnv, taskGroundGit), and a second strip here would be a second
// opinion about which variables matter.
//
// THE MESSAGE LOCALE IS THE ONE EXCEPTION, AND IT IS PINNED (Rule 2/14). This
// is the ONE `git -C <dir>` runner of the peer-fetch path, so the pin belongs
// here rather than at each of its four callers. Callers above it DO interpret
// git's English sentences:
//
//   - task_sync_git_pushback.go's gitPushbackTargetAliasNotRemote matches
//     pbUnresolvableRepoPhrase `does not appear to be a git repository`;
//   - the same file reads a failed push's detail, and its own documented exit
//     table names `fatal: Not a valid object name`.
//
// Measured on this host 2026-10-30 (git 2.43.0, zh_CN.utf8 installed):
//
//	$ LC_ALL=C            git push /nonexistent/gopi.git master
//	fatal: '/nonexistent/gopi.git' does not appear to be a git repository
//	$ LC_ALL=zh_CN.UTF-8  git push /nonexistent/gopi.git master
//	致命错误：'/nonexistent/gopi.git' does not appear to be a git repository
//
// — the PREFIX is translated while the quoted sentence is not, which is exactly
// the shape a substring marker survives by luck and not by contract, so the pin
// is what makes it hold. Root cause and the sharing rule: git_child_locale.go.
// A caller that passes its own `env` keeps every entry it built; the pin is
// applied THROUGH that slice (ChildLocaleEnvFrom), never in place of it.
func gitPeerRun(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	if env != nil {
		// nil means "inherit the calling process's environment" — the shape
		// every adapter but the ground uses. A caller that must not inherit a
		// routed git environment (GIT_DIR/GIT_WORK_TREE/GIT_INDEX_FILE) passes
		// the stripped list through the ONE helper that owns that list
		// (withoutGitDirEnv, taskGroundGit) rather than re-deriving it here.
		cmd.Env = ChildLocaleEnvFrom(env, ChildLocaleEnglish)
	} else {
		cmd.Env = ChildLocaleEnv()
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return strings.TrimSpace(stderr.String()),
			fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// gitPeerMirrorRefspec renders the refspec that mirrors the peer's integration
// branch into this package's reserved durable ref (gitPushbackCheckRef). The
// leading `+` is the mirror contract: whatever the peer advertises NOW
// overwrites this repo's stale understanding of it. A DURABLE ref (never
// FETCH_HEAD) is what an ancestry question needs, because it runs
// in a scratch worktree that cannot see the caller's FETCH_HEAD.
func gitPeerMirrorRefspec() string {
	return gitPeerMirrorRefspecPrefix + integrationBranchRef() + ":" + gitPushbackCheckRef
}

// integrationBranchRef is the peer-side ref the mirror refspec reads:
// refs/heads/<the integration branch>. The name comes from the package's ONE
// integration-branch constant (IntegrationBranchName), never from a literal.
func integrationBranchRef() string {
	return "refs/heads/" + IntegrationBranchName
}

// SyncPeerFetch fetches the peer's refs into this repo and returns the sha of
// the peer's INTEGRATION-BRANCH TIP.
//
// It is the one exported implementation of that capability: the push-back
// lane's ancestry gate, the close-out's peer resolution and the worker's ground
// baseline all call it, so "what the peer's master is right now" has exactly
// one answer per fetch, produced by one argv.
//
// It writes REMOTE-TRACKING/MIRROR refs only (refs/remotes/gitPushcheck/…):
// the caller's own branches, its HEAD and its working tree are untouched, so
// this is safe to call in the middle of a run standing in a linked worktree.
//
// THE FAILURE IS A FAILURE (Rule 15). There is deliberately NO fallback to the
// local integration branch: a caller that cannot reach the peer must be told
// so and decide for itself (the ground degradation path prints its own note;
// the caller records its own verdict). An empty (sha, nil) is not a
// value this function can produce.
func SyncPeerFetch(ctx context.Context, repo, target string) (string, error) {
	repo = strings.TrimSpace(repo)
	target = strings.TrimSpace(target)
	if repo == "" {
		return "", fmt.Errorf("peer fetch: no repo directory to fetch into")
	}
	if target == "" {
		return "", fmt.Errorf("peer fetch: no peer target (see config.ResolveSyncPeers)")
	}
	if _, err := gitPeerRun(ctx, repo, nil, gitPeerFetchVerb, gitPeerFetchForceFlag,
		gitPeerFetchNoTagsFlag, target, gitPeerMirrorRefspec()); err != nil {
		return "", fmt.Errorf("peer fetch %s (into %s): %w", target, repo, err)
	}
	tip, err := gitPeerRun(ctx, repo, nil, gitPeerRevParseVerb, gitPeerVerifyFlag, gitPeerQuietFlag,
		gitPushbackCheckRef+gitPeerCommitSuffix)
	if err != nil {
		return "", fmt.Errorf("peer fetch %s: the mirror ref %s did not resolve after a successful fetch: %w",
			target, gitPushbackCheckRef, err)
	}
	tip = strings.TrimSpace(tip)
	if tip == "" {
		return "", fmt.Errorf("peer fetch %s: the mirror ref %s resolved to nothing", target, gitPushbackCheckRef)
	}
	// IDENTITY BEFORE ANY VERDICT (measured 2026-10-25). Everything downstream —
	// the push-back lane's merge-ness and ancestry gates, the alignment facts,
	// the close-out pre-merge gate and the pass's staleness alarm — reads the
	// mirror ref as "the peer's tip of MY repository". A mirror written from a
	// repo that merely SITS at the dialled path breaks that premise silently: the
	// measured case had two unrelated repositories at the same host under two
	// paths, and the alarm's next-best answer was a merge prescription that
	// grafts 12025 commits into master. So the fetch does not RETURN a tip it has
	// not identified: a tip with no common ancestor is a REFUSAL carrying the
	// finding and the resolver's provenance, never a value the caller measures
	// against (Rule 15 — the failure is named here, at the one place that knows
	// both the target it dialled and the ref it wrote).
	if same, why := peerRepoIdentityCheck(ctx, repo, target, peerRepoPathResolve(repo).Reason); !same {
		return "", errors.New(why) // no-wrap: `why` already carries the whole verdict and its provenance
	}
	return tip, nil
}
