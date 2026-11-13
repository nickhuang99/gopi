package tools

// A peer name THIS MACHINE cannot resolve must name the mismatch (Rule 15).
//
// gopi's git legs spell their target `<alias>:<path>` (finishCleanupServerTarget
// / GitSyncServerTarget), where the alias half is the DECLARED topology —
// settings.json `sync_peers[0]`, an ssh host. Nothing requires that host to
// also be a configured git REMOTE in this repo, and git's diagnostic for the
// two worlds is the same sentence: `fatal: '<alias>' does not appear to be a
// git repository`.
//
// MEASURED 2026-10-12 on sager: `sync_peers: ["zbook"]` while `git remote -v`
// lists only `origin`. The operator-facing report carried git's sentence and
// nothing else: not the declared peer, not the remotes that DO exist here, and
// not the fact that the push was refused purely because two NAME SPACES
// disagree.
//
// The sibling arm (a configured remote whose `remote.<name>.pushurl` is
// DISABLED-no-push, measured 2026-10-08 on HP) already had a refusal; this one
// did not. The two are kept in ONE entry point (gitPushbackTargetFailureDetail)
// so a reader cannot be told two different stories about one sentence — and so
// the reverse control below proves the new arm did not swallow the old one.

import (
	"context"
	"strings"
	"testing"
)

// pbGhostAlias is a peer name that is NOT a configured remote in the fixture
// client (whose only remote is `origin`). It is spelled here rather than read
// from production so the test states the name it expects the finding to carry.
const pbGhostAlias = "ghost"

// pbUnresolvedSentence is git's own wording for a target that is not a
// repository — the sentence both arms must be reached through.
const pbUnresolvedSentence = pbGhostAlias + " does not appear to be a git repository"

// TestPushbackTargetFailureDetailIsTableDriven pins the ONE entry point's
// decisions, including the two negative controls that keep it honest: a target
// whose alias IS a configured remote must NOT collect the name-mismatch line
// (the remote is not missing — it is there, whatever else went wrong), and a
// target with no alias half (a bare path, a URL) names no remote to be missing.
func TestPushbackTargetFailureDetailIsTableDriven(t *testing.T) {
	w := newPBWorld(t)
	ctx := context.Background()
	ghost := pbGhostAlias + ":" + w.server

	cases := []struct {
		name         string
		target       string
		detail       string
		blockPushURL bool
		want         []string
		notWant      []string
	}{{
		name:   "unresolved alias names the mismatch and the remotes that exist",
		target: ghost,
		detail: "the peer rejected the push — git said: fatal: " + pbUnresolvedSentence,
		want: []string{
			pbUnresolvedSentence, // git's own words survive
			pbGhostAlias,         // the alias git could not resolve
			"origin",             // THIS repo's actual remotes — the comparison
			"sync_peers",         // the declared topology the operator must look at
		},
		notWant: []string{"remote.origin.pushurl", pushbackRemoteNameNone},
	}, {
		name:    "a target that IS a configured remote is never called missing",
		target:  "origin",
		detail:  "fatal: 'origin' does not appear to be a git repository",
		want:    []string{},
		notWant: []string{"sync_peers", pushbackRemoteNameNone},
	}, {
		name:    "an alias-free target names no remote to be missing",
		target:  w.server,
		detail:  "fatal: '" + w.server + "' does not appear to be a git repository",
		want:    []string{},
		notWant: []string{"sync_peers", pushbackRemoteNameNone},
	}, {
		name:    "an alias-free target whose failure is unrelated is returned unchanged",
		target:  w.server,
		detail:  "the peer rejected the push — git said: fatal: could not read from remote repository",
		want:    []string{"could not read from remote repository"},
		notWant: []string{"sync_peers", "origin"},
	}, {
		name:   "the ssh wording is diagnosed too (the LIVE fetch shape, measured)",
		target: ghost,
		detail: "server-master fetch: exit status 128: ssh: Could not resolve hostname " + pbGhostAlias + ": Name or service not known\nfatal: Could not read from remote repository.",
		want: []string{
			// git's own words survive...
			"Could not read from remote repository",
			// ...AND the finding lands, even though git never printed the
			// "does not appear to be a git repository" phrase here. This is
			// the red-proof correction: the arm is gated on the MEASUREMENT,
			// not on git's wording, because one unresolvable peer produces
			// different sentences from fetch and push.
			"sync_peers", "origin", pbGhostAlias,
		},
		notWant: []string{pushbackRemoteNameNone},
	}, {
		name:         "the blocked-pushurl arm still wins for a configured remote (REVERSE CONTROL)",
		target:       "origin",
		detail:       "fatal: '" + pbBlockedPushURL + "' does not appear to be a git repository",
		blockPushURL: true,
		want: []string{
			"remote.origin.pushurl", // the KEY, so a reader can find the line that caused it
			pbBlockedPushURL,        // git's refused URL, quoted
			"--unset",               // the command that clears it
		},
		notWant: []string{pushbackRemoteNameNone, "sync_peers"},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The blocked-pushurl arm reads the key off the repo, so only the
			// reverse control configures it; every other case must see it
			// UNSET, or the two arms could not be told apart.
			if tc.blockPushURL {
				w.git(w.client, "config", "remote.origin.pushurl", pbBlockedPushURL)
				defer w.git(w.client, "config", "--unset", "remote.origin.pushurl")
			}
			got := gitPushbackTargetFailureDetail(ctx, w.client, tc.target, tc.detail)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("detail %q must carry %q, got %q", tc.detail, want, got)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(got, notWant) {
					t.Errorf("detail %q must NOT carry %q, got %q", tc.detail, notWant, got)
				}
			}
		})
	}
}

// TestPushbackUnresolvedRemoteNameIsDiagnosedByTheRealLane drives the
// PRODUCTION entry point (gitPushbackPlan) at a target this machine cannot
// resolve, and asserts the refusal the operator actually reads. It is the
// end-to-end half: the detail-level table above could pass while the lane's
// own first target-git call (the peer-master fetch, which runs BEFORE any
// push) still reported git's bare sentence.
//
// RED STATE (before the fix): flush.Failed[0] was
// "server-master fetch: git fetch … : exit status 128: fatal: 'ghost' does not
// appear to be a git repository" — true, and useless: it names neither the
// declared topology nor this repo's remotes.
func TestPushbackUnresolvedRemoteNameIsDiagnosedByTheRealLane(t *testing.T) {
	// The lane's test fence refuses an in-process pass whose target is outside
	// test space; this test is about the REFUSAL SHAPE, not about where the
	// target lives. Nothing can leave the machine anyway: the alias does not
	// resolve, so no transport is even attempted.
	t.Setenv(finishCleanupInProcessTestMarker, "1")
	w := newPBWorld(t)

	flush := gitPushbackPlan(context.Background(), w.client, pbGhostAlias+":"+w.server)

	if len(flush.Failed) != 1 {
		t.Fatalf("an unresolvable target must refuse the flush exactly once: failed=%v pushed=%v", flush.Failed, flush.Pushed)
	}
	line := flush.Failed[0]
	// ① git's own sentence survives — the finding is an ADDITION to it, never
	//    a replacement (a reader must still see what git said). The measured
	//    sentence for the FETCH leg is the ssh one: the fetch runs before any
	//    push, so it is what an unresolvable peer actually reports.
	//
	//    THE SENTENCE COMPARED AGAINST IS GIT'S OWN, MEASURED AT TEST TIME —
	//    never a phrase typed into this file. git prints its diagnostics in the
	//    caller's locale, so the hardcoded "Could not read from remote
	//    repository" made this test fail on a zh_CN host (MEASURED 2026-10-12:
	//    the refusal carried `fatal: 无法读取远程仓库。` — the PRODUCT was right,
	//    the assertion was the locale-dependent half). The probe below asks git
	//    the same question the lane asks (same target, same verb) under the
	//    suite's pinned child locale, so this assertion follows the locale the
	//    suite runs git under instead of demanding one language; the predicate
	//    is exact containment and is shared with the locale guard
	//    (pushbackKeepsGitSentence, task_sync_git_pushback_locale_test.go), so
	//    the two cannot drift.
	if gitSentence := pbcFetchUnresolvablePeer(t, w.client, pbGhostAlias+":"+w.server, toolsPinnedChildLocale); !pushbackKeepsGitSentence(line, gitSentence) {
		t.Errorf("the refusal must keep git's own words verbatim (git, under %s, says %q): %q",
			toolsPinnedChildLocale, gitSentence, line)
	}
	// ② the name the operator used, so it is greppable.
	if !strings.Contains(line, pbGhostAlias) {
		t.Errorf("the refusal must name the unresolvable alias %q: %q", pbGhostAlias, line)
	}
	// ③ THE COMPARISON — this repo's actual remotes. Without them the operator
	//    learns the name is wrong but not what to correct it TO.
	if !strings.Contains(line, "origin") {
		t.Errorf("the refusal must list this repo's real remotes: %q", line)
	}
	// ④ the declared topology, by its OWN key name — it is the file to look at.
	if !strings.Contains(line, "sync_peers") {
		t.Errorf("the refusal must name the declared-topology key: %q", line)
	}
	// ⑤ nothing was pushed — the set is `flush.Pushed` (the branches the flush
	// reports it moved), and the guard under test is the target's own
	// unresolvability, measured (gitPushbackTargetAliasNotRemote asks `git remote`)
	// BEFORE any push is attempted. Corroborated inside this test by the
	// peer-ref assertion below, and by a measured run: after `gopi task push`
	// against an unresolvable peer, `git --git-dir=<peer> for-each-ref` still
	// printed only `refs/heads/master` — the peer gained nothing.
	//
	// why: correct — no idempotent member can be wrongly excluded from `flush.Pushed`: on this path the peer-master FETCH leg runs first and fails (exit 128), so the plan abstains before any push is attempted, and a refusal that moved no ref has no step left owing.
	if len(flush.Pushed) != 0 {
		t.Errorf("an unresolvable target must push nothing: %v", flush.Pushed)
	}
	if !w.serverRefExists("master") {
		t.Errorf("the fixture's peer must be untouched by the refusal (master still there)")
	}
}

// TestPushbackNoRemoteAtAllReadsAsNoneNotBlank pins the empty-listing arm: a
// repo with NO configured remotes must say so in words. A blank there would be
// indistinguishable from a measurement that never ran (Rule 15), and the
// operator would be told "this repo's remotes are: " with nothing after it.
func TestPushbackNoRemoteAtAllReadsAsNoneNotBlank(t *testing.T) {
	w := newPBWorld(t)
	// A second repo with no remote at all: clone-free, so nothing configures one.
	bare := w.client + "-noremote"
	w.git(w.root, "init", "-q", "-b", "master", bare)

	if got := gitPushbackRemoteNameList(context.Background(), bare); got != pushbackRemoteNameNone {
		t.Errorf("a remote-less repo must read as the named 'none' spelling, got %q", got)
	}
	// And the finding built on it still says what to do.
	got := gitPushbackTargetFailureDetail(context.Background(), bare,
		pbGhostAlias+":~/work/GoPi", "fatal: 'ghost' does not appear to be a git repository")
	if !strings.Contains(got, pushbackRemoteNameNone) {
		t.Errorf("the finding must carry the 'none' measurement, got %q", got)
	}
	if !strings.Contains(got, pushbackRemoteNameVerb) {
		t.Errorf("the finding must name the command whose answer it reports, got %q", got)
	}
}
