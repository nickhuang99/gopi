package tools

// peer_remote_target.go — the ONE resolver that turns a DECLARED PEER ALIAS
// into a git push/fetch TARGET (Rule 14: a shared value lives in exactly one
// place, referenced — never mirrored, never bare-written).
//
// WHY this file exists (the defect it repairs, measured 2026-10-12 on sager):
// every client-side git leg composed its target as
//
//	fmt.Sprintf("%s:%s", peer, repoTail)   // peer = settings.json sync_peers[0]
//
// which reads the ssh TOPOLOGY ALIAS as if it were a git REMOTE NAME. Those
// are two different NAME SPACES with no contract between them: the alias is
// resolved by ssh (its own config, on this host), while a remote name is a
// per-repository local setting (`git remote add`). They coincide only by
// coincidence — and the coincidence breaks loudly. On sager `sync_peers` was
// ["zbook"] while `git remote -v` listed only `origin`, so every delivery leg
// died on git's own sentence
//
//	fatal: 'zbook' does not appear to be a git repository
//
// which names neither the declared peer nor the remotes that DO exist here.
// The sibling lane already carried a partial diagnosis for that sentence
// (gitPushbackAliasNotRemoteLine); this file finishes the job on the
// RESOLUTION side, so a repo whose remote name and whose declared alias
// disagree still DELIVERS — and one whose alias resolves to nothing gets a
// named diagnosis carrying this repo's actual remote list.
//
// THE RULE, in one sentence: a declared alias is asked of THIS REPOSITORY's
// remotes to learn WHICH PEER the topology means and to say so by name — but the
// string a delivery leg carries is the explicit `<alias>:<path>` form on both
// arms, because a leg hands its target to git in a DIFFERENT repository (the
// row's worktree), where a remote name this checkout configures resolves to
// nothing. The remote arm's contribution is the ALIAS CHOICE and the named
// diagnosis, never a change of the wire form; see peerRemoteTarget.SSHTarget for
// the measurement that settled it.

import (
	"context"
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/config"
)

const (
	// peerRemoteTailSep separates an ssh alias from the repo PATH in the
	// explicit `<alias>:<path>` fallback target. It is the same byte the
	// peer-alias validator refuses inside an alias
	// (sshPeerAliasTargetSeparator): the two spellings are one contract, and
	// reading them from one source is what keeps a composed target and a
	// validated one from drifting apart.
	peerRemoteTailSep = sshPeerAliasTargetSeparator

	// peerRemoteTargetRemoteFmt / peerRemoteTargetFallbackFmt are the two
	// resolution sentences, one per arm, each naming WHICH form was resolved
	// and WHY a reader can tell them apart: on the remote arm the target is a
	// name this repo owns (so `remote.<name>.url`/pushurl govern it); on the
	// fallback arm it is a direct ssh target this repo has no remote for —
	// and the sentence therefore carries this repo's ACTUAL remote list, which
	// is the comparison the operator needs (Rule 15: a resolution that fell
	// back to another form says so, by name, with the measurement behind it).
	//
	// Arity: remote = (remote name, declared alias, delivered target, this
	// repo's remotes); fallback = (declared alias, rendered target, the
	// settings key, this repo's remotes). The key is the topology's own
	// constant, never a literal.
	//
	// The remote sentence names BOTH the remote and the target, because on that
	// arm they are deliberately different strings: "this repo knows your peer
	// by a remote name" is what the arm establishes, and "and the legs still
	// carry the ssh form" is what a reader must be able to see rather than
	// infer (Rule 15: a resolution says which form was resolved — and, when the
	// form it HANDS ON is not the form it resolved, that too).
	peerRemoteTargetRemoteFmt   = "this repo configures the git remote %q for the declared peer %q, so the peer is a name this repo's git really knows — the delivery legs still carry the explicit ssh form %q, because a remote name resolves only in the repository that configures it and every leg hands its target to a DIFFERENT one (this repo's remotes: %s)"
	peerRemoteTargetFallbackFmt = "the declared peer %q is not a configured git remote here, so the target is the explicit ssh form %q; %s and this repo's git remotes are two different NAME SPACES — this repo's remotes are: %s"

	// peerRemoteTargetNoRepoFmt is the THIRD sentence — the one the two above
	// could not say, and the condition they were silently answering as if it
	// were the second. Arity: (the directory asked, the probe's own argv, what
	// that probe answered).
	//
	// The distinction it carries is the whole point: "this repository
	// configures no remote of that name" and "there is no repository here to
	// ask" are two conditions, and the fallback arm is a RESOLUTION for the
	// first while it would be a FABRICATION for the second. Measured
	// 2026-10-16: a process outside any git work tree asked the niladic face,
	// the remote list came back empty (git's own refusal, indistinguishable
	// from "this repo has no remotes"), and the answer composed was
	// `<alias>:~/work/GoPi` — a REAL host and a REAL path, dialled by ssh on a
	// question nobody could answer. Rule 15: the two conditions get two
	// answers, and the unanswerable one names itself.
	peerRemoteTargetNoRepoFmt = "no repository could be established at %q (`git %s` said %q), so this repo's git config cannot be asked which git target the declared peer %q means — an UNANSWERABLE repo is not the same condition as a repository with no such remote, so this resolves to NO target rather than to the explicit ssh fallback"

	// peerRemoteRepoProbeVerb / peerRemoteRepoProbeFlag are the one git question
	// that separates those two conditions (Rule 1b: the spelling is named here,
	// never typed at the call site). `rev-parse` is the package's own verb
	// constant; the flag is this probe's, and it is `--git-dir` rather than
	// `--show-toplevel` / `--is-inside-work-tree` for a measured reason: a BARE
	// repository is a repository with its own remotes and no work tree at all,
	// and the delivery path may legitimately be handed one (measured in this
	// suite: the "repo with no remotes at all" arm of
	// TestPeerRemoteTargetNamesTheArmAndThisReposRemotes builds exactly that, and
	// a work-tree probe called it "no repository"). `--git-dir` answers the
	// question actually being asked — is there a git directory to read — and
	// answers it non-empty in both shapes.
	peerRemoteRepoProbeVerb = gitRevParseVerb
	peerRemoteRepoProbeFlag = "--git-dir"

	// peerRemoteTargetSyncToUnusableFmt is the NAMED diagnosis for the one
	// condition the alias choice could previously not report at all: a peer
	// DELIVERY DESTINATION was declared (`sync_to`, resolved by
	// config.ResolveNodeRole) and it cannot be handed to git/ssh — so the alias
	// in force is the sync_peers fallback, not what the operator declared.
	//
	// Arity: (the declared value, the env var that overrides it, the validator's
	// own sentence, the destination actually in force). Rule 15: a resolution
	// that had to fall back from the DECLARED destination says so by name, with
	// the measurement behind it — "delivered to a peer you did not name, and
	// nothing said why" is exactly the silence this sentence exists to end.
	peerRemoteTargetSyncToUnusableFmt = "the node declares the delivery destination %s=%q, which cannot be handed to git/ssh (%s); the peer in force is therefore %q, taken from the sync_peers fallback — the declared destination is UNUSED, not silently honoured"

	// peerRemoteTargetSyncToNone is the same fallback's answer when no peer at
	// all is usable. Arity: (declared value, env var, the validator sentence).
	peerRemoteTargetSyncToNoneFmt = "the node declares the delivery destination %s=%q, which cannot be handed to git/ssh (%s), and no sync_peers entry is usable either — so there is NO peer to deliver to at all"
)

// peerRemoteRepoIsEstablishable answers whether dir is a git repository at all,
// and WHY not when it is not — the measurement the resolution needs before it
// can honestly read a remote list.
//
// It is deliberately not `len(remotes) == 0`: that reading cannot tell "this
// repo configures no remotes" from "there was no repo to read", and the two
// lead to opposite answers (fallback vs no target). git's own refusal is
// carried back verbatim so the sentence names the measurement rather than the
// inference — and an EMPTY answer is never read as established, because a scan
// that found nothing must not be indistinguishable from a scan that never ran
// (Rule 15).
func peerRemoteRepoIsEstablishable(ctx context.Context, dir string) (bool, string) {
	out, err := gitPushbackGitRun(ctx, dir, peerRemoteRepoProbeVerb, peerRemoteRepoProbeFlag)
	answer := strings.TrimSpace(out)
	if err != nil || answer == "" {
		return false, answer
	}
	return true, ""
}

// peerRemoteTarget is the ONE resolution of "which git target does this
// declared peer mean in THIS repository" — the form and the sentence that
// explains it, produced together so no caller can report a form it did not
// resolve (Rule 15: a resolution's reason travels with its result).
//
// Rendered is the empty string for exactly one reason: no peer is declared (or
// the declared alias is unusable — see peerRemoteDeclaredAlias). It is the
// fail-soft signal the chain's callers already read, so it is never a guess.
type peerRemoteTarget struct {
	// Alias is the declared ssh peer alias this resolution is about ("" when
	// none could be taken from the topology).
	Alias string
	// Dir is the repository this answer was asked OF — the checkout whose own
	// git config decided the form. It travels WITH the answer because the
	// question "which repo's remotes?" is what makes two resolutions of one
	// declared alias differ (measured 2026-10-16: the process's own answer and
	// the delivered row's answer are answers about different repositories), so a
	// reader who only saw Rendered could not tell them apart (Rule 15: a
	// resolution's reason travels with its result — and so does its subject).
	Dir string
	// Remote is the repo's configured remote of that alias name ("" when the
	// repo has none — the fallback arm, not a failure).
	Remote string
	// Rendered is the REPO-LEVEL resolution: a remote NAME on the remote arm,
	// `<alias>:<path>` on the fallback arm, or "" (no target at all). It is the
	// answer to "which name does this repo know the peer by", which is a real
	// question — but it is NOT always the string a delivery leg may carry. A
	// leg that carries it asks SSHTarget below, and the two differ on the
	// remote arm by MEASUREMENT, not by taste.
	Rendered string
	// SSHTarget is the string a leg that CARRIES its target hands to git — the
	// close-out push, the ground-baseline fetch, and every other leg that gives
	// git a destination of its own instead of a name this repo configures.
	//
	// It is the explicit `<alias>:<path>` form on BOTH arms, and the remote arm
	// does not weaken that: a remote NAME is resolvable only inside the
	// repository that configures it, and a delivery target is handed to git in
	// a DIFFERENT repository — the row's worktree (finishCleanupMainRepoDir /
	// the ground's own repo), whose `git remote` list is its own. Measured on
	// this host, in a repo that configures no remote named `zbook`:
	//
	//	git fetch zbook        → fatal: 'zbook' … unknown transport
	//	git push  zbook        → fatal: 'zbook' does not appear to be a git repository
	//
	// — which is the very sentence this resolver exists to abolish, re-created
	// one repository further along. The ssh form carries its own transport
	// (`ssh` + the alias in the user's own ssh config, `~` expanded by the
	// PEER's shell), so it needs nothing from the receiving repo's config, and
	// the push lands at the peer's declared go-forward path
	// (peer_repo_path.go — the ONE resolver) rather than at whatever
	// `remote.<name>.url` happens to spell there.
	//
	// So the remote arm's contribution is the ALIAS CHOICE and the named
	// diagnosis — the declared peer is a name this repo's git really knows —
	// while the string the legs carry stays ONE form on both arms. Empty
	// exactly when Rendered is empty (no alias = nothing to dial).
	SSHTarget string
	// Reason is the named diagnosis: one sentence stating which arm the
	// resolution took, the alias it came from, the form the legs will carry,
	// and this repo's actual remotes (Rule 15: say what was resolved AND what
	// was found — "found nothing" is a measurement, never a blank).
	Reason string
}

// peerRemoteDeclaredAlias is the ONE reader of "the declared peer this leg
// should deliver to", and the declared DELIVERY DESTINATION comes FIRST.
//
// THE PRIORITY, in this order, and it is the whole of what this function knows
// (Rule 14: one reader, so the peer-pushback legs and the close-out chain cannot
// disagree about which peer is in force):
//
//	① sync_to — the operator's delivery destination, resolved by the config
//	   layer's ONE node-identity reader (config.ResolveNodeRole, which merges
//	   GOPI_SYNC_TO over settings `sync_to`; this file never reads the settings
//	   key itself — Rule 2). Used only when it passes the ssh-alias syntax gate.
//	② sync_peers — the topology's peer list (config.ResolveSyncPeers), first
//	   entry that passes the same gate. This is the pre-existing behaviour, kept
//	   verbatim as the FALLBACK arm so every node that declares no sync_to (the
//	   server, and every single-node host) resolves exactly what it always did.
//	③ "" — no usable peer, which stays the ONE meaning it always had: there is
//	   nobody to deliver to. The caller's fail-soft arm names it
//	   (noServerTargetSkipReason).
//
// WHY sync_to FIRST (measured 2026-10-16 on this branch, devdoc/hp-push-topology
// -audit.md G8): `sync_peers` was designed as the TASK-LEDGER whitelist, so a
// client that exchanges rows with a second host listed it there and its branches
// were delivered to whichever alias came first in that list — the delivery
// destination the operator declared (`sync_to`) had NO reader anywhere, and the
// close-out's push therefore went to a peer the operator never named for
// delivery. The declared destination is the more specific and the more explicit
// statement of intent; the peer list is the general topology.
//
// A declared-but-UNUSABLE sync_to is never a guess and never silence (Rule 15):
// the fallback is taken, and peerRemoteSyncToUnusableReason names it — the
// declared value, the env var that overrides it, the validator's own sentence
// and the destination actually in force — so the resolution's reader can hand
// that to an operator instead of letting `<some peer>` stand unexplained.
func peerRemoteDeclaredAlias() string {
	alias, _ := peerRemoteDeclaredAliasWithReason()
	return alias
}

// peerRemoteDeclaredAliasWithReason is the SAME selection with its diagnosis
// attached: the alias in force plus the sentence explaining a declared
// destination that could not be used ("" when nothing was declared, and
// therefore nothing was overridden).
//
// It exists as ONE implementation behind two faces (Rule 14) rather than as a
// second selection: `peerRemoteDeclaredAlias` is the face the legs use, and this
// face is for the reader that must SHOW the resolution.
func peerRemoteDeclaredAliasWithReason() (string, string) {
	// ① the declared delivery destination.
	syncTo := peerRemoteSyncTo()
	if syncTo == "" {
		// ② the declared topology, unchanged.
		return peerRemoteFirstUsablePeer(), ""
	}
	alias, err := ValidateSSHPeerAlias(syncTo)
	if err == nil {
		return alias, ""
	}
	// The declared destination is UNUSABLE. Rule 15: never silence it — answer
	// with a usable fallback and the sentence naming the override, or refuse
	// with the destination actually in force and the validator's own reason.
	if fallback := peerRemoteFirstUsablePeer(); fallback != "" {
		return fallback, fmt.Sprintf(peerRemoteTargetSyncToUnusableFmt,
			config.SyncToEnv, syncTo, err.Error(), fallback)
	}
	return "", fmt.Sprintf(peerRemoteTargetSyncToNoneFmt,
		config.SyncToEnv, syncTo, err.Error())
}

// peerRemoteSyncTo is the ONE read of the declared DELIVERY DESTINATION for the
// tools layer: the config package's own node-identity resolver
// (config.ResolveNodeRole, whose sync_to half merges GOPI_SYNC_TO over
// settings.json `sync_to` — Rule 2: the env var is read there and nowhere else).
// Trimmed, and an empty answer keeps its single meaning: nothing was declared.
func peerRemoteSyncTo() string {
	_, syncTo := config.ResolveNodeRole()
	return strings.TrimSpace(syncTo)
}

// peerRemoteFirstUsablePeer is the sync_peers arm: the first declared peer whose
// alias passes the ssh-alias syntax guard (an option-shaped alias must never
// reach ssh OR git). Empty when there is none to take.
func peerRemoteFirstUsablePeer() string {
	for _, p := range peerRemoteDeclaredPeers() {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, err := ValidateSSHPeerAlias(p); err != nil {
			continue
		}
		return p
	}
	return ""
}

// peerRemoteDeclaredPeers is the ONE read of the declared topology (settings.json
// sync_peers through config.ResolveSyncPeers — Rule 2: that single reader is the
// only place the env var is read). It is split out from the alias selection so
// the two questions — "what was declared?" and "which of it may git/ssh be handed?"
// — stay separable: a caller that must REPORT the declaration asks this, a caller
// that needs a usable target asks peerRemoteDeclaredAlias.
func peerRemoteDeclaredPeers() []string {
	peers, err := config.ResolveSyncPeers(nil)
	if err != nil {
		return nil
	}
	return peers
}

// peerRemoteTargetFor resolves the delivery target for ONE declared alias in
// ONE repository (dir), and is the single entry point every peer-pushing leg
// calls (Rule 14). repoTail is the path the explicit fallback form carries —
// the leg's own named constant, never a literal.
//
// The resolution has exactly two arms, and BOTH are resolutions rather than
// failures:
//
//	remote arm    the repo configures a remote named like the alias → the
//	              declared peer is a name this repo's git really KNOWS, and the
//	              diagnosis says so by name (Rendered carries the remote name
//	              for readers that must SHOW the resolution). The wire form is
//	              unchanged — see SSHTarget.
//	fallback arm  the repo has no such remote → the same explicit
//	              `<alias>:<path>` target this leg has always composed. It is
//	              NOT an error — a legitimate topology may name the peer only in
//	              ssh terms — so it is rendered and EXPLAINED, and the diagnosis
//	              carries this repo's actual remotes so an operator sees at a
//	              glance whether the two name spaces disagree (Rule 15).
//
// Two conditions answer SSHTarget == "" (and with it Rendered == ""): an
// unreadable topology or an unusable alias — there is no peer to deliver to,
// and the caller's existing fail-soft arm (noServerTargetSkipReason) already
// names it — and dir naming no repository at all, see
// peerRemoteRepoIsEstablishable: there the fallback arm's `<alias>:<path>`
// would be a guess about a host layout nobody measured, so it is refused BY
// NAME instead (Rule 15).
func peerRemoteTargetFor(ctx context.Context, dir, alias, repoTail string) peerRemoteTarget {
	res := peerRemoteTarget{Alias: strings.TrimSpace(alias), Dir: dir}
	if res.Alias == "" {
		return res
	}
	if ok, probe := peerRemoteRepoIsEstablishable(ctx, dir); !ok {
		res.Reason = fmt.Sprintf(peerRemoteTargetNoRepoFmt, dir,
			peerRemoteRepoProbeVerb+" "+peerRemoteRepoProbeFlag, probe, res.Alias)
		return res
	}
	// The ONE string the legs carry, composed BEFORE the arms are told apart:
	// both arms hand git the explicit ssh form (SSHTarget's doc carries the
	// measurement), so it cannot be a per-arm decision. It is composed only
	// once the repository is ESTABLISHED, so the unanswerable arm above still
	// answers "no target" instead of dialling a path nobody measured.
	res.SSHTarget = res.Alias + peerRemoteTailSep + repoTail
	for _, name := range gitPushbackGitRemoteNames(ctx, dir) {
		if name != res.Alias {
			continue
		}
		res.Remote = name
		res.Rendered = name
		res.Reason = fmt.Sprintf(peerRemoteTargetRemoteFmt, name, res.Alias, res.SSHTarget, gitPushbackRemoteNameList(ctx, dir))
		return res
	}
	res.Rendered = res.SSHTarget
	res.Reason = fmt.Sprintf(peerRemoteTargetFallbackFmt, res.Alias, res.Rendered,
		config.SyncPeersSettingsKey, gitPushbackRemoteNameList(ctx, dir))
	return res
}

// peerRemoteFallbackTarget is the explicit `<alias>:<path>` form for an alias,
// for the callers that need the ssh spelling itself and not a resolution (the
// option-injection guard reads this half of a composed target).
func peerRemoteFallbackTarget(alias, repoTail string) string {
	return strings.TrimSpace(alias) + peerRemoteTailSep + repoTail
}

// peerRemoteTargetFailureDetail decorates a FAILED delivery with the ONE
// sentence that says how this leg's target was resolved — and it is the answer
// to the question git's own failure cannot answer: "did the target go through a
// remote this repo configures, or was it handed to ssh as a bare host:path?"
// (Rule 15: a refusal names the condition it happened under, not only the
// error it returned).
//
// The two halves are deliberate and share the existing entry points:
//
//	gitPushbackTargetFailureDetail  the name-mismatch finding and the pushurl
//	                                finding — unchanged, measured, one
//	                                definition for every push path (Rule 14).
//	res.Reason                      the RESOLUTION's own sentence, appended
//	                                only on the FALLBACK arm: that is the arm
//	                                where "which form did this take" is a real
//	                                question, because the resolution already
//	                                had to decide it. On the remote arm the
//	                                target IS a name this repo owns, and the
//	                                failure belongs to the remote's own config
//	                                (its URL, its pushurl), which the halves
//	                                above already name.
//
// It resolves AGAIN rather than receiving a resolution, because the failure
// paths call it with the target they were handed (the chain's injected-target
// seam passes a fixture target that no topology produced) — and the extra `git
// remote` read is the price of keeping ONE decoration entry point.
func peerRemoteTargetFailureDetail(ctx context.Context, dir, target, detail string) string {
	out := gitPushbackTargetFailureDetail(ctx, dir, target, detail)
	alias := peerRemoteDeclaredAlias()
	if alias == "" {
		return out
	}
	res := peerRemoteTargetFor(ctx, dir, alias, peerRemoteRepoTail(target, alias))
	if res.Rendered != strings.TrimSpace(target) || res.Remote != "" {
		return out
	}
	return out + finishCleanupReasonSeparator + res.Reason
}

// peerRemoteRepoTail answers the path half of an `<alias>:<path>` target, so a
// re-resolution compares a target against the resolution that would produce it
// — and answers the resolver's own tail (peer_repo_path.go) when the target is
// not that shape at all (the leg's own declared tail is then the only honest
// answer, and it is the one the original resolution used).
func peerRemoteRepoTail(target, alias string) string {
	head, tail, ok := strings.Cut(strings.TrimSpace(target), peerRemoteTailSep)
	if !ok || strings.TrimSpace(head) != alias || strings.TrimSpace(tail) == "" {
		return peerRepoPath()
	}
	return tail
}
