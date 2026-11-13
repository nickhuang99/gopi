package tools

// merge_build_gate_modcache.go — HOW the merge-time BUILD gate RESOLVES its
// modules. This is the resolution half of the 2026-09-15 false-refusal family
// (the sibling branch fix/mergebuildgate-module-download-abstain is the
// CLASSIFICATION half: it turns a download-failure build into an ABSTAIN).
//
// THE DEFECT: the gate built every revision in a scratch Go cache pair, and
// the module half of that pair was COLD BY CONSTRUCTION (an empty
// <scratch>/gomodcache per process). With no route to proxy.golang.org on this
// host, EVERY build died on
//
//	go: downloading modernc.org/sqlite v1.44.0
//	internal/tools/tasks.go:16:2: ... i/o timeout
//
// so the gate was not measuring the branch at all: it was measuring the
// environment. Whether that landed as a refusal or an abstention was a coin
// flip on the toolchain's output text.
//
// THE FIX: the build RESOLVES ITS MODULES BY CONSTRUCTION — GOMODCACHE is
// pointed at a populated, READ-ONLY, operator-owned module cache, so there is
// no fetch to attempt in the first place. This is deliberately NOT a check on
// the build's output string (a string match is a symptom, not a resolution):
// the gate cannot reach the "go: downloading" path because the modules are
// already on disk where the toolchain looks for them.
//
// WHERE IT COMES FROM (never populated by the gate — Rule 15b: a cache the
// gate cannot see is an ABSTAIN with a log line, never a silent network fetch):
//
//	1. GOPI_MERGE_BUILD_GATE_GOMODCACHE (mergeBuildGateModCacheEnvName) — the
//	   ONE override, read in ONE place (Rule 2), for an operator whose module
//	   cache lives elsewhere or for a test that seeds its own;
//	2. the toolchain's own answer, `go env GOMODCACHE` — the cache the
//	   operator's OWN builds use, which is by definition warm for this repo
//	   (the same `go build ./...` every briefing tells an agent to run fills it).
//
// THE GATE NEVER WRITES THERE: GOCACHE stays inside the gate's own per-process
// scratch (mergeBuildGateScratch), so a merge probe still cannot fill the
// user's BUILD cache, and the shared module cache is used read-only — the
// operator's GOMODCACHE carries no write bit for us. Rule 25 is untouched
// (nothing here needs a privilege) and Rule 22 is untouched (the scratch half
// stays under internal/scratch, never /tmp).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// mergeBuildGateModCacheEnvName is the operator escape hatch for the
	// SHARED module cache the gate's builds resolve from — the one reader is
	// mergeBuildGateSharedModCache (Rule 2).
	mergeBuildGateModCacheEnvName = "GOPI_MERGE_BUILD_GATE_GOMODCACHE"

	// mergeBuildGateGoBinaryName / mergeBuildGateGoEnvVerb are the toolchain
	// call that answers "which module cache does this host use" (Rule 1b: the
	// binary name and the subcommand are both spelled once, here).
	mergeBuildGateGoBinaryName = "go"
	mergeBuildGateGoEnvVerb    = "env"

	// mergeBuildGateGoProxyEnv / mergeBuildGateGoSumDBEnv are the two module
	// SOURCES the gate makes unusable when it wants a build to prove it
	// resolved from the cache alone (GOPROXY=off is the toolchain's own
	// spelling for "no network": a missing module then fails with "module
	// lookup disabled"), plus the sum database that would otherwise be
	// consulted over the network.
	mergeBuildGateGoProxyEnv = "GOPROXY"
	mergeBuildGateGoSumDBEnv = "GOSUMDB"

	// mergeBuildGateCacheDownloadDirName / mergeBuildGateCacheDownloadSubtree
	// are the module cache's own download layout (<cache>/cache/download/…):
	// the project's dependency graph already maintains it, and its presence is
	// what makes a config directory a POPULATED module cache rather than an
	// empty one the gate would have to fetch into (which is exactly the
	// failure this file exists to prevent).
	mergeBuildGateCacheDownloadDirName = "cache"
	mergeBuildGateCacheDownloadSubtree = "download"

	// mergeBuildGateModCacheNotDirFmt / mergeBuildGateModCacheUnpopulatedFmt
	// are the two ABSTAIN reasons. They are the visible half of Rule 15b: an
	// operator reading the merger log learns WHICH path was missing, not that
	// "the build failed".
	mergeBuildGateModCacheNotDirFmt      = "the shared module cache %s does not exist or is not a directory (the gate never populates one over the network)"
	mergeBuildGateModCacheUnpopulatedFmt = "the shared module cache %s has no %s/%s subtree — it is not a populated Go module cache, and the gate will not fetch into it"
	mergeBuildGateModCacheUnresolvedFmt  = "no shared module cache could be resolved from %s or `%s %s %s`"
)

// mergeBuildGateSharedModCache resolves and VALIDATES the read-only module
// cache the gate's builds resolve their modules from. ok=false means the gate
// must ABSTAIN (never build against an empty cache and report the resulting
// fetch error as a statement about the branch).
//
// The validation is deliberately about EXISTENCE, not about being writable: a
// populated cache is usable read-only, and requiring write access would force
// the gate back onto a cache it populates itself — the defect.
func mergeBuildGateSharedModCache() (string, bool) {
	dir := strings.TrimSpace(os.Getenv(mergeBuildGateModCacheEnvName))
	if dir == "" {
		dir = mergeBuildGateOperatorModCache()
	}
	if dir == "" {
		taskNotifyLog("merger: BUILD gate has no shared module cache (%s) — ABSTAINING rather than fetching over the network",
			fmt.Sprintf(mergeBuildGateModCacheUnresolvedFmt, mergeBuildGateModCacheEnvName,
				mergeBuildGateGoBinaryName, mergeBuildGateGoEnvVerb, mergeBuildGateGoModCacheEnv))
		return "", false
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		taskNotifyLog("merger: BUILD gate shared module cache unusable: %s (%v)",
			fmt.Sprintf(mergeBuildGateModCacheNotDirFmt, dir), err)
		return "", false
	}
	download := filepath.Join(dir, mergeBuildGateCacheDownloadDirName, mergeBuildGateCacheDownloadSubtree)
	if sub, serr := os.Stat(download); serr != nil || !sub.IsDir() {
		taskNotifyLog("merger: BUILD gate shared module cache unusable: %s (%v)",
			fmt.Sprintf(mergeBuildGateModCacheUnpopulatedFmt, dir,
				mergeBuildGateCacheDownloadDirName, mergeBuildGateCacheDownloadSubtree), serr)
		return "", false
	}
	return dir, true
}

// mergeBuildGateOperatorModCache asks the Go toolchain which module cache this
// host uses (`go env GOMODCACHE`) — the cache the operator's own builds read
// and fill, so it is warm for exactly the modules this repo needs.
//
// An unanswerable question returns "" (the caller ABSTAINS and says so): a
// guessed path would silently resolve to an empty cache, which is the original
// defect wearing a different hat.
func mergeBuildGateOperatorModCache() string {
	out, err := exec.Command(mergeBuildGateGoBinaryName, mergeBuildGateGoEnvVerb, mergeBuildGateGoModCacheEnv).Output()
	if err != nil {
		taskNotifyLog("merger: BUILD gate could not resolve the operator module cache (`%s %s %s`: %v)",
			mergeBuildGateGoBinaryName, mergeBuildGateGoEnvVerb, mergeBuildGateGoModCacheEnv, err)
		return ""
	}
	// A relative path is not a cache the build subprocess can be pointed at —
	// it would resolve against the gate's CWD (the merger's directory), i.e. a
	// different cache every run. `go env` answers with an absolute path; a
	// relative one means something unusual is configured, so refuse rather
	// than guess (Rule 15b).
	dir := strings.TrimSpace(string(out))
	if dir == "" || !filepath.IsAbs(dir) {
		taskNotifyLog("merger: BUILD gate refused a non-absolute %s %q (`%s %s %s`)",
			mergeBuildGateGoModCacheEnv, dir, mergeBuildGateGoBinaryName, mergeBuildGateGoEnvVerb, mergeBuildGateGoModCacheEnv)
		return ""
	}
	return dir
}
