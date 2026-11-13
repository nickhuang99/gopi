package tools

// A push the peer's OWN CONFIG refuses must say so (Rule 15).
//
// The lane's first shape was `pushbackPushRefusedReasonFmt`, whose sentence is
// true and useless: "the peer rejected the push" reads identically for a
// non-fast-forward, a permission denial, and a push that never left this
// machine because `remote.<name>.pushurl` points at something that is not a
// repository.
//
// MEASURED 2026-10-08 on HP (the client), on the real repo: `remote.zbook.pushurl`
// = `DISABLED-no-push`, and `git push zbook master:master` fails with
// `fatal: 'DISABLED-no-push' does not appear to be a git repository` — while
// every gopi lane spells its target `<alias>:<path>` (`GitSyncServerTarget()` →
// `zbook:~/work/GoPi`, `finishCleanupServerTarget()` → the same shape), which
// git hands to ssh directly and which that key does NOT govern. So the guardrail
// could never have stopped gopi's own push-back; what it could do is make a
// refusal unreadable, and that is what this test pins.
//
// The test uses the NAME spelling on purpose: only that spelling consults
// `remote.<name>.pushurl`, and it is reachable from this lane's exported entry
// (`gitPushbackPlan(ctx, repo, "<remote-name>")`) — the same call shape the
// `gitPushbackPlan` test fence and the peer-ref tests already drive.

import (
	"context"
	"strings"
	"testing"
)

// pbBlockedPushURL is the sentinel a blocked push URL is set to — deliberately
// not a path and not a URL, so git can only answer "not a repository".
const pbBlockedPushURL = "DISABLED-no-push"

// pbPushRefusedPhrase is the lane's own verdict, spelled here (and NOT read
// from the production constant) so the test states what it expects from the
// refusal rather than echoing whatever the refusal happens to contain.
const pbPushRefusedPhrase = "the peer rejected the push"

// TestPushbackBlockedPushURLIsNamedInTheRefusal is the red-proof for the
// actionable refusal: a push refused by the peer's `remote.<name>.pushurl`
// must name git's own sentence, the key, its value, and the command that
// clears it. Its red state (before the fix) is the generic line, which names
// none of the three.
func TestPushbackBlockedPushURLIsNamedInTheRefusal(t *testing.T) {
	// The push-back lane's own test fence refuses an in-process test that
	// pushes a deliverable at a target outside test space (the target here is
	// a remote NAME, whose local side is not a path at all). This test is
	// about the REFUSAL SHAPE, not about where the target lives, so it takes
	// the documented opt-out — the same one task_finish_cleanup_test.go:134
	// takes. Nothing leaves the machine either way: the push URL is a
	// sentinel git cannot resolve.
	t.Setenv(finishCleanupInProcessTestMarker, "1")
	w := newPBWorld(t)
	// A deliverable that FAST-FORWARDS the peer's master, so every earlier
	// gate passes and the PUSH is the step that decides the outcome (a branch
	// that failed an earlier gate would never reach the push, and the test
	// would pass for the wrong reason).
	w.git(w.client, "checkout", "-q", "-b", "feat/blocked")
	pbWrite(t, w.client, "blocked.txt", "work that must not be able to be delivered")
	w.git(w.client, "add", "blocked.txt")
	w.git(w.client, "commit", "-q", "-m", "blocked deliverable")
	w.git(w.client, "checkout", "-q", "master")
	// The guardrail, on the push half of the peer's name.
	w.git(w.client, "config", "remote.origin.pushurl", pbBlockedPushURL)

	flush := gitPushbackPlan(context.Background(), w.client, "origin")

	// why: correct — the push is the step under test and the only step that can
	// produce this entry (every earlier gate passes); the peer's refs/heads is asserted untouched below.
	if len(flush.Pushed) != 0 {
		t.Fatalf("a push refused by the peer's config must push nothing: pushed=%v", flush.Pushed)
	}
	if len(flush.Failed) != 1 {
		t.Fatalf("exactly one refusal is owed for one branch: failed=%v skipped=%v", flush.Failed, flush.Skipped)
	}
	line := flush.Failed[0]
	// ① keyed by the branch's own name — the refusal says WHICH branch.
	if !strings.HasPrefix(line, "feat/blocked: ") {
		t.Errorf("the refusal must be keyed by the branch name, got %q", line)
	}
	// ② the lane's own verdict survives (it is not the collision arm's line).
	if !strings.Contains(line, pbPushRefusedPhrase) {
		t.Errorf("the refusal lost the lane's verdict %q: %q", pbPushRefusedPhrase, line)
	}
	// ③ git's OWN words — the sentence that distinguishes a blocked URL from a
	//    transport failure or a non-fast-forward.
	if !strings.Contains(line, pbBlockedPushURL) {
		t.Errorf("the refusal must quote the refused push URL %q: %q", pbBlockedPushURL, line)
	}
	// ④ the KEY, so a reader can find the line that caused it without guessing.
	key := "remote.origin.pushurl"
	if !strings.Contains(line, key) {
		t.Errorf("the refusal must name the config key %q: %q", key, line)
	}
	// ⑤ the remedy — a refusal with no action is the defect this closes.
	if !strings.Contains(line, "--unset") {
		t.Errorf("the refusal must carry the command that clears the block: %q", line)
	}
	// ⑥ nothing crossed the wire: the peer's namespace is untouched.
	if w.serverRefExists("feat/blocked") {
		t.Errorf("the blocked push must not have created refs/heads/feat/blocked on the peer")
	}
}

// TestPushbackPushURLProbeIsSilentWhenUnset is the other half of the finding:
// the blocked-pushurl arm must be driven by a MEASURED key, never by the fact
// that a push failed. A probe that answered "found" for any remote would make
// every refusal claim a block that does not exist — the fabrication this arm
// would otherwise introduce (Rule 15: an unmeasured premise reported as a
// finding).
func TestPushbackPushURLProbeIsSilentWhenUnset(t *testing.T) {
	w := newPBWorld(t)

	// ① the key is unset on a configured remote → no pushurl is riding it.
	if url, ok := gitPushbackRemotePushURL(context.Background(), w.client, "origin"); ok {
		t.Errorf("origin has no pushurl configured, yet the probe answered (%q, true)", url)
	}
	// ② the target is a PATH (this lane's ordinary spelling — the ssh shape
	//    `<alias>:<path>` and a bare local path both carry a separator), which
	//    is not a remote NAME and cannot carry `remote.<name>.pushurl`.
	for _, target := range []string{w.server, "zbook:~/work/GoPi"} {
		if url, ok := gitPushbackRemotePushURL(context.Background(), w.client, target); ok {
			t.Errorf("target %q is not a remote name, yet the probe answered (%q, true)", target, url)
		}
	}
	// ③ and when the key IS set, it is read back verbatim — the same call shape
	//    git uses, so the value reported to the operator is the value git read.
	w.git(w.client, "config", "remote.origin.pushurl", pbBlockedPushURL)
	url, ok := gitPushbackRemotePushURL(context.Background(), w.client, "origin")
	if !ok || url != pbBlockedPushURL {
		t.Errorf("the probe must read the configured key back verbatim, got (%q, %v)", url, ok)
	}
}
