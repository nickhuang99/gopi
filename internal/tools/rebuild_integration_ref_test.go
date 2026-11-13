package tools

// The close loop's CURRENT-NESS measurement: which ref answers "what revision
// must this repo's artifact carry?" (task 01a1385d).
//
// THE INCIDENT THESE TESTS PIN (measured 2026-10-14, nick-HP-Laptop): the
// deployed artifact attested a revision EQUAL to the checkout's LOCAL
// refs/heads/master, so the shortcut in RebuildAfterMerge called it "already
// current" and SKIPPED — while refs/remotes/gitPushcheck/master (the ref the
// task grounds are cut from) was 97 commits ahead, and the machine ran code
// that many commits stale. The mechanism that already ticks could not repair
// that class, because by its own definition the artifact WAS current.
//
// Both arms are asserted here, against REAL git: an artifact equal to the local
// tip but BEHIND the integration ref must NOT read as current (the build runs),
// and an artifact equal to the integration ref's tip must read as current (the
// build does not). A third case keeps the peerless/single-host deployment
// exactly as shipped: with no integration ref at all the measurement falls back
// to HEAD and says so.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rebuildMirrorRef is the mirror ref the delivery lane writes and the ground
// baseline reads — spelled through the ONE constant, never typed here.
var rebuildMirrorRef = gitPushbackCheckRef

// rebuildNoIntegrationRefBranch is the branch a peerless single-host
// deployment may have checked out when it carries NEITHER of the integration
// ref names (the local integration branch or the mirror). Named here so the
// fallback-to-HEAD arm is built from a checkout that genuinely has no
// integration ref rather than from one whose ref happened to be spelled like it.
const rebuildNoIntegrationRefBranch = "trunk"

// rebuildTestRepoOnBranch makes a temp repo whose only branch is name (no
// master, no mirror) — the peerless/test shape. Returns the repo and its tip.
func rebuildTestRepoOnBranch(t *testing.T, name string) (repo, tip string) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, repo, "init", "-q", "-b", name)
	rebuildTestGit(t, repo, "config", "user.email", "t@t")
	rebuildTestGit(t, repo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, repo, "add", "f.txt")
	rebuildTestGit(t, repo, "commit", "-q", "-m", "c1")
	return repo, rebuildTestGit(t, repo, "rev-parse", "--short", "HEAD")
}

// rebuildIntegrationRefFixture builds the incident's shape: a repo whose LOCAL
// master sits at one commit, with the integration mirror ref carried FORWARD by
// extra commits (the mirror is strictly ahead, so it is the ref the grounds
// follow). Returns the repo, the local tip and the mirror tip.
func rebuildIntegrationRefFixture(t *testing.T) (repo, localTip, mirrorTip string) {
	t.Helper()
	repo, localTip = rebuildTestRepo(t)
	// Advance the mirror ref (never the local branch): commit on a scratch
	// branch and point the mirror at it, then return the worktree to master —
	// the mirror is a ref, not a checkout.
	rebuildTestGit(t, repo, "checkout", "-q", "-b", "scratch/ahead")
	if err := os.WriteFile(filepath.Join(repo, "ahead.txt"), []byte("mirror work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, repo, "add", "ahead.txt")
	rebuildTestGit(t, repo, "commit", "-q", "-m", "mirror-only work")
	mirrorTip = rebuildTestGit(t, repo, "rev-parse", "HEAD")
	rebuildTestGit(t, repo, "update-ref", rebuildMirrorRef, mirrorTip)
	rebuildTestGit(t, repo, "checkout", "-q", "master")
	if got := rebuildTestGit(t, repo, "rev-parse", "--short", "HEAD"); got != localTip {
		t.Fatalf("fixture: local master moved (got %s want %s) — the race between local HEAD and the mirror is the whole point", got, localTip)
	}
	return repo, localTip, mirrorTip
}

// rebuildWriteTipBin writes bin/gopi as a script that reports the tip of ref
// when asked --version — the honest stand-in for the vcs stamp a real build
// produces (the same shape rebuild_test.go's fixtures use).
func rebuildWriteTipBin(t *testing.T, repo, ref string) {
	t.Helper()
	binDir := filepath.Join(repo, rebuildBinDirName)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then git -C " + repo + " rev-parse --short " + ref + "; fi\n"
	bin := filepath.Join(binDir, rebuildBinName)
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestRebuildNotCurrentWhenArtifactFollowsLocalHeadButNotIntegrationRef is arm
// ①: the artifact attests the LOCAL tip, the integration ref is ahead, and the
// close loop must therefore NOT take the already-current shortcut — it must
// rebuild, measured against the integration ref.
//
// This is the incident reproduced: before the fix, `gitTip` answered with local
// HEAD, versionMatches said yes, and the build never ran.
func TestRebuildNotCurrentWhenArtifactFollowsLocalHeadButNotIntegrationRef(t *testing.T) {
	repo, localTip, mirrorTip := rebuildIntegrationRefFixture(t)
	if localTip == mirrorTip {
		t.Fatal("fixture: local tip equals the mirror tip — there is no drift to measure")
	}
	// The stale artifact: it follows LOCAL HEAD, exactly as the deployed one did.
	rebuildWriteTipBin(t, repo, taskGitHeadToken)
	built := filepath.Join(t.TempDir(), "built.marker")
	// The build writes an artifact attesting the INTEGRATION ref's tip (what a
	// correct build from the grounds' ref produces) and leaves a marker so the
	// test can prove the build really ran.
	buildCmd := "printf '#!/bin/sh\\nif [ \"$1\" = \"--version\" ]; then git -C " + repo +
		" rev-parse --short " + rebuildMirrorRef + "; fi\\n' > " + filepath.Join(repo, rebuildBinDirName, rebuildBinName) +
		" && chmod +x " + filepath.Join(repo, rebuildBinDirName, rebuildBinName) + " && echo ran > " + built
	rebuildTestSettings(t, map[string]map[string]any{
		repo: {"build": buildCmd, "build_timeout_sec": 60},
	})

	res := RebuildAfterMerge(repo)
	if res.Elapsed == 0 {
		t.Fatalf("the already-current shortcut fired while the artifact followed the LOCAL HEAD and the integration ref was ahead (expected=%s local=%s): the rebuild must run", res.ExpectedVersion, localTip)
	}
	if !res.OK {
		t.Fatalf("rebuild after the integration ref moved must succeed: %+v (err=%q)", res, res.Err)
	}
	if res.ExpectedRef != rebuildMirrorRef {
		t.Errorf("ExpectedRef = %q, want the integration ref %q — the measurement must name the ref it used", res.ExpectedRef, rebuildMirrorRef)
	}
	if res.MeasureNote != "" {
		t.Errorf("a measurement taken against the preferred ref must carry no fallback note, got %q", res.MeasureNote)
	}
	if got := strings.TrimSpace(res.ExpectedVersion); got != mirrorTip[:len(got)] && !strings.HasPrefix(mirrorTip, got) {
		t.Errorf("ExpectedVersion = %q, want the integration ref's tip (%s)", got, mirrorTip)
	}
	if _, err := os.Stat(built); err != nil {
		t.Fatalf("the build did not run: %v", err)
	}
}

// TestRebuildCurrentWhenArtifactFollowsIntegrationRef is arm ②: an artifact
// that DOES follow the integration ref reads as current, so the shortcut still
// works and a round with nothing new to build costs one probe (the idempotency
// the close loop's unconditional callers depend on).
func TestRebuildCurrentWhenArtifactFollowsIntegrationRef(t *testing.T) {
	repo, localTip, mirrorTip := rebuildIntegrationRefFixture(t)
	if localTip == mirrorTip {
		t.Fatal("fixture: local tip equals the mirror tip — a false green would be indistinguishable here")
	}
	rebuildWriteTipBin(t, repo, rebuildMirrorRef)
	built := filepath.Join(t.TempDir(), "built.marker")
	buildCmd := "echo ran > " + built
	rebuildTestSettings(t, map[string]map[string]any{
		repo: {"build": buildCmd, "build_timeout_sec": 60},
	})

	res := RebuildAfterMerge(repo)
	if !res.OK {
		t.Fatalf("an artifact following the integration ref must read as current: %+v (err=%q)", res, res.Err)
	}
	if res.Elapsed != 0 {
		t.Fatalf("the rebuild ran for an artifact already at the integration tip %s (elapsed=%s)", mirrorTip, res.Elapsed)
	}
	if res.ExpectedRef != rebuildMirrorRef {
		t.Errorf("ExpectedRef = %q, want %q", res.ExpectedRef, rebuildMirrorRef)
	}
	if _, err := os.Stat(built); err == nil {
		t.Fatal("the build command ran although the artifact was already at the integration tip")
	}
}

// TestRebuildFallsBackToHeadWhenNoIntegrationRef pins the deployment the fix
// must NOT regress: a checkout with neither a mirror nor a local integration
// branch (a peerless single-host deployment whose branch is named something
// else) is measured against HEAD — exactly the pre-fix question — and the
// fallback is REPORTED rather than silently substituted.
func TestRebuildFallsBackToHeadWhenNoIntegrationRef(t *testing.T) {
	repo, _ := rebuildTestRepoOnBranch(t, rebuildNoIntegrationRefBranch)
	rebuildWriteTipBin(t, repo, taskGitHeadToken)
	built := filepath.Join(t.TempDir(), "built.marker")
	rebuildTestSettings(t, map[string]map[string]any{
		repo: {"build": "echo ran > " + built, "build_timeout_sec": 60},
	})

	res := RebuildAfterMerge(repo)
	if !res.OK {
		t.Fatalf("a repo with no integration ref must still read as current against HEAD: %+v (err=%q)", res, res.Err)
	}
	if res.Elapsed != 0 {
		t.Fatalf("the rebuild ran although the artifact already followed HEAD in a repo with no integration ref")
	}
	if res.ExpectedRef != taskGitHeadToken {
		t.Errorf("ExpectedRef = %q, want %q for the peerless case", res.ExpectedRef, taskGitHeadToken)
	}
	if res.MeasureNote == "" {
		t.Fatal("the fallback to HEAD was SILENT — a reader cannot tell it apart from a measurement of the integration ref (Rule 15)")
	}
	if _, err := os.Stat(built); err == nil {
		t.Fatal("the build command ran although the artifact already followed HEAD")
	}
}

// TestRebuildMeasuresTheLocalIntegrationBranchAndSaysSo is the middle arm: a
// checkout that has a local integration branch but NO mirror is measured
// against that branch (equal to HEAD here, so the shipped behaviour is
// preserved byte for byte) and carries the reason the mirror was not used.
func TestRebuildMeasuresTheLocalIntegrationBranchAndSaysSo(t *testing.T) {
	repo, tip := rebuildTestRepo(t) // master checked out, no mirror ref
	rebuildWriteTipBin(t, repo, taskGitHeadToken)
	rebuildTestSettings(t, map[string]map[string]any{
		repo: {"build": "exit 1", "build_timeout_sec": 60},
	})

	res := RebuildAfterMerge(repo)
	if !res.OK || res.Elapsed != 0 {
		t.Fatalf("expected the already-current skip measured against the local integration branch: %+v (err=%q)", res, res.Err)
	}
	if want := refsHeadsPrefix + IntegrationBranchName; res.ExpectedRef != want {
		t.Errorf("ExpectedRef = %q, want %q", res.ExpectedRef, want)
	}
	if res.ExpectedVersion == "" || !strings.HasPrefix(strings.TrimSpace(tip), res.ExpectedVersion) {
		t.Errorf("ExpectedVersion = %q, want the local integration branch's tip %s", res.ExpectedVersion, tip)
	}
	if res.MeasureNote == "" {
		t.Fatal("a repo whose mirror was absent must say so — otherwise 'measured against the ref the grounds follow' and 'measured against this checkout' read identically")
	}
}

// TestRebuildTipForRepoPrefersTheMirrorAndExplainsItsFallback pins the
// measurement itself, independent of any build: the resolver names the ref it
// read, prefers the mirror when it is strictly ahead, and never answers without
// saying which ref it used.
func TestRebuildTipForRepoPrefersTheMirrorAndExplainsItsFallback(t *testing.T) {
	repo, localTip, mirrorTip := rebuildIntegrationRefFixture(t)
	got := rebuildTipForRepo(repo)
	if got.Err != nil {
		t.Fatalf("measurement failed: %v", got.Err)
	}
	if got.Ref != rebuildMirrorRef {
		t.Errorf("Ref = %q, want the integration ref %q", got.Ref, rebuildMirrorRef)
	}
	if got.Note != "" {
		t.Errorf("no fallback happened, so no note is owed; got %q", got.Note)
	}
	if !strings.HasPrefix(mirrorTip, got.Revision) {
		t.Errorf("Revision = %q, want the mirror tip %s", got.Revision, mirrorTip)
	}
	if got.Revision == localTip {
		t.Errorf("Revision %q is the LOCAL tip — the pre-fix measurement, and the defect itself", got.Revision)
	}

	// A repo with neither ref falls back to HEAD, named.
	plain, plainTip := rebuildTestRepoOnBranch(t, rebuildNoIntegrationRefBranch)
	fallback := rebuildTipForRepo(plain)
	if fallback.Err != nil {
		t.Fatalf("fallback measurement failed: %v", fallback.Err)
	}
	if fallback.Ref != taskGitHeadToken {
		t.Errorf("fallback Ref = %q, want %q", fallback.Ref, taskGitHeadToken)
	}
	if !strings.HasPrefix(plainTip, fallback.Revision) {
		t.Errorf("fallback Revision = %q, want HEAD %s", fallback.Revision, plainTip)
	}
	if fallback.Note == "" {
		t.Error("the fallback to HEAD carried no note — a silent fallback is the defect this fix must not reintroduce")
	}
}
