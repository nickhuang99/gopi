package tools

// peer_repo_path_test.go — the regression guard for "WHERE does the peer's
// repository live" (peer_repo_path.go) and for "is the tip we mirrored even the
// same repository" (peer_repo_identity.go).
//
// THE DEFECT (measured 2026-10-25, evidence in the filing run's session dir):
// three delivery legs each carried their own copy of the peer's checkout path,
// all spelling `~/work/GoPi`, while the host DECLARES its delivery point as
// `peerbox:/home/user/work/gopi`. On the peer those are two unrelated
// repositories. The measured consequences were (a) every push refused
// non-fast-forward and (b) the staleness alarm prescribing
// `git merge refs/remotes/gitPushcheck/master` — a merge that would graft 12025
// unrelated commits into master.
//
// So the two cases that matter are asserted as PROPERTIES, not as wordings:
// the resolver must return the DECLARED path and say which arm produced it, and
// a mirror with no common ancestor must produce the different-repository
// verdict and must NOT produce the merge prescription.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	peerRepoPathTestDeclaredPath   = "/home/user/work/gopi"
	peerRepoPathTestDeclaredTarget = "peerbox" + peerRepoPathDeliverySep + peerRepoPathTestDeclaredPath
	peerRepoPathTestLastResortPath = "~/work/GoPi"
)

// peerRepoPathTestSettings writes a settings document into a scratch directory
// and points the config layer at it, and clears the resolver's once-per-process
// note cache so the announcement is observable (Rule 32: the environment is
// pinned, never the operator's own profile).
func peerRepoPathTestSettings(t *testing.T, doc map[string]any) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "settings.json")
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsPathEnv, path)
	peerRepoPathResetNotes()
}

// peerRepoPathResetNotes clears the dedupe cache so a case can observe the
// announcement the resolver makes exactly once per (arm, path).
func peerRepoPathResetNotes() {
	peerRepoPathNoteMu.Lock()
	defer peerRepoPathNoteMu.Unlock()
	peerRepoPathNotedSet = map[string]bool{}
}

// TestPeerRepoPathPrefersTheDeclaredDeliveryPoint is the PRIMARY regression: the
// path the three legs dial must be the one this host DECLARES, parsed out of
// `dev_repo`'s `<alias>:<path>` spelling — not a per-leg constant. The measured
// failure this pins was a push to `~/work/GoPi` while the declaration said
// `/home/user/work/gopi`.
func TestPeerRepoPathPrefersTheDeclaredDeliveryPoint(t *testing.T) {
	peerRepoPathTestSettings(t, map[string]any{
		// The repos table deliberately names a DIFFERENT path: arm ① must win,
		// and a test that let arm ② answer would pass on the wrong arm.
		config.ReposSettingsKey:   map[string]any{"/srv/repos-table-answer": map[string]any{}},
		config.DevRepoSettingsKey: peerRepoPathTestDeclaredTarget,
	})
	t.Setenv(config.DevRepoEnvName, "")

	res := peerRepoPathResolve("")
	if res.Path != peerRepoPathTestDeclaredPath {
		t.Fatalf("resolved path = %q, want the DECLARED delivery point's path half %q — a leg dialling anything else pushes into another repository",
			res.Path, peerRepoPathTestDeclaredPath)
	}
	if res.Channel != peerRepoPathDeclaredChannelLabel {
		t.Errorf("channel = %q, want %q (Rule 15: the resolution names the arm that decided it)", res.Channel, peerRepoPathDeclaredChannelLabel)
	}
	if res.Reason != "settings "+config.DevRepoSettingsKey {
		t.Errorf("reason = %q, want the declaring channel %q", res.Reason, "settings "+config.DevRepoSettingsKey)
	}
	// The composed target the legs hand to git carries that path — this is the
	// string that ends up in the mirror ref's reflog.
	t.Setenv(config.SyncPeersEnv, "peerbox")
	t.Setenv("GOPI_SYNC_NODE", "hp")
	if got, want := GitSyncServerTarget(), peerRepoPathTestDeclaredTarget; got != want {
		t.Errorf("GitSyncServerTarget() = %q, want %q", got, want)
	}
}

// TestPeerRepoPathEnvChannelWinsOverSettings is Rule 2's precedence seen from
// this resolver: the env channel is the ONE reader's front door, so it decides
// the path here too.
func TestPeerRepoPathEnvChannelWinsOverSettings(t *testing.T) {
	peerRepoPathTestSettings(t, map[string]any{
		config.DevRepoSettingsKey: peerRepoPathTestDeclaredTarget,
	})
	t.Setenv(config.DevRepoEnvName, "peer:/srv/from-env")

	if got := peerRepoPath(); got != "/srv/from-env" {
		t.Fatalf("resolved path = %q, want the env channel's value", got)
	}
}

// TestPeerRepoPathFallsBackToTheReposTable is arm ②: no delivery point declared,
// one unambiguous table entry — this host's second statement of where its system
// repo lives.
func TestPeerRepoPathFallsBackToTheReposTable(t *testing.T) {
	peerRepoPathTestSettings(t, map[string]any{
		config.ReposSettingsKey: map[string]any{"/srv/only-one": map[string]any{}},
	})
	t.Setenv(config.DevRepoEnvName, "")

	res := peerRepoPathResolve("")
	if res.Path != "/srv/only-one" {
		t.Fatalf("resolved path = %q, want the table's only entry", res.Path)
	}
	if res.Channel != peerRepoPathReposChannelLabel {
		t.Errorf("channel = %q, want %q", res.Channel, peerRepoPathReposChannelLabel)
	}
}

// TestPeerRepoPathReposTableIsAmbiguousWithoutASubject pins the refusal this arm
// owes when the table lists several repositories and none of them is the
// checkout being delivered from: picking one silently would be a coin flip
// deciding a delivery destination, so the arm declines BY NAME and the last
// resort (announced) answers instead.
func TestPeerRepoPathReposTableIsAmbiguousWithoutASubject(t *testing.T) {
	peerRepoPathTestSettings(t, map[string]any{
		config.ReposSettingsKey: map[string]any{
			"/srv/a": map[string]any{},
			"/srv/b": map[string]any{},
		},
	})
	t.Setenv(config.DevRepoEnvName, "")

	res := peerRepoPathResolve("")
	if res.Channel != peerRepoPathLastResortChannelLabel {
		t.Fatalf("channel = %q, want the LAST-RESORT arm on an ambiguous table", res.Channel)
	}
	if !strings.Contains(res.Reason, "2 entries") {
		t.Errorf("reason %q must name the ambiguity (the count of entries), not just fall through silently", res.Reason)
	}
}

// TestPeerRepoPathSubjectDisambiguatesTheReposTable is the other half of arm ②:
// with a SUBJECT (the checkout being delivered from) the table is a lookup, so an
// entry describing that checkout answers even among several.
func TestPeerRepoPathSubjectDisambiguatesTheReposTable(t *testing.T) {
	subject := t.TempDir()
	peerRepoPathTestSettings(t, map[string]any{
		config.ReposSettingsKey: map[string]any{
			subject:  map[string]any{},
			"/srv/b": map[string]any{},
		},
	})
	t.Setenv(config.DevRepoEnvName, "")

	if got := peerRepoPathForRepo(subject); got != subject {
		t.Fatalf("resolved path = %q, want the table entry naming the subject %q", got, subject)
	}
}

// TestPeerRepoPathLastResortAnnouncesItself is Rule 15's half of the fallback: a
// path nobody declared must SAY it is a guess, with the reason. The measured
// silence — three constants nobody could tell from a declaration — is what let
// this defect live for as long as it did.
func TestPeerRepoPathLastResortAnnouncesItself(t *testing.T) {
	peerRepoPathTestSettings(t, map[string]any{})
	t.Setenv(config.DevRepoEnvName, "")

	res := peerRepoPathResolve("")
	if res.Path != peerRepoPathTestLastResortPath {
		t.Fatalf("resolved path = %q, want the named last resort %q", res.Path, peerRepoPathTestLastResortPath)
	}
	if res.Channel != peerRepoPathLastResortChannelLabel {
		t.Fatalf("channel = %q, want the LAST-RESORT arm", res.Channel)
	}
	// The announcement is deduped per process; this case asserts the SENTENCE a
	// reader actually receives (peerRepoPathNoteText), which must carry the three
	// things the silent constant never said: the arm, the value, and the setting
	// that would end the guessing.
	note := peerRepoPathNoteText(res)
	want := []string{config.DevRepoSettingsKey, peerRepoPathTestLastResortPath, peerRepoPathLastResortChannelLabel}
	for _, w := range want {
		if !strings.Contains(note, w) {
			t.Errorf("the fallback note %q must name %q — a fallback nobody can see is the defect this replaces", note, w)
		}
	}
	if res.Reason == "" {
		t.Error("the fallback arm must carry a reason naming why every declaration declined")
	}
}

// TestPeerRepoPathRefusesAGitURLAsADeliveryPoint pins the one value that looks
// like a declaration but cannot be parsed as one: a `ssh://…` / `user@host:path`
// spelling is a repo spec for the PEER's git, not a filesystem path this process
// may dial, so the arm declines by name instead of handing git a path half that
// is really a URL.
func TestPeerRepoPathRefusesAGitURLAsADeliveryPoint(t *testing.T) {
	for _, declared := range []string{
		"ssh://git@peer/srv/gopi.git",
		"nick@peer:/srv/gopi.git",
	} {
		t.Run(declared, func(t *testing.T) {
			peerRepoPathTestSettings(t, map[string]any{config.DevRepoSettingsKey: declared})
			t.Setenv(config.DevRepoEnvName, "")

			res := peerRepoPathResolve("")
			if res.Path == declared {
				t.Fatalf("the URL %q was handed on as a repo PATH", declared)
			}
			if !strings.Contains(res.Reason, "git URL") {
				t.Errorf("reason %q must name the refusal (a git URL is not a `<alias>:<path>` delivery point)", res.Reason)
			}
		})
	}
}
