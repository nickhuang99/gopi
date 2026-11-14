// Package version is the single source of truth for the version of a gopi
// BINARY (Rule 14: a value more than one package must agree on lives in ONE
// small exported leaf package — never mirrored, never typed inline).
//
// WHY THE VERSION IS INTERNAL AND HARD-CODED. gopi is a general agent: it
// merges ANY git repo, and it is run from directories that are not its own
// checkout at all. An identity BORROWED from whatever checkout the process
// happens to stand in is therefore not an identity — the same binary prints
// a different "version" depending on cwd, and no version at all when cwd is
// not a git repo (the tarball case, the /tmp case, the scratch-repo case).
// The version of a binary is a property of the BINARY, so it is compiled in
// here and travels with the artifact: `gopi version` needs no git, no
// filesystem and no network to state it.
//
// RELEASE TAGS ARE THE REPO OWNER'S DECISION, NOT A MERGE SIDE EFFECT. The
// `version/*` tags an earlier design minted on every merge round are gone
// from the code: merging is git work, and naming a release is a decision the
// repo's owner makes deliberately. Nothing in this package writes a tag, and
// no merge path calls into it (the coupling was deleted, not hidden behind a
// flag — see devdoc/version-identity.md).
package version

import (
	"fmt"
	"regexp"

	"github.com/nickhuang99/gopi/internal/buildidentity"
)

// DefaultVersion is the version of the gopi SOURCE TREE this binary was
// built from — the compile-time default and the single place the value is
// written.
//
// THE SCHEME (semantic versioning, with the bump happening in the commit
// that earns it):
//
//		vMAJOR.MINOR.PATCH   e.g. v0.1.0, v1.4.0, v2.0.0
//
//	  - MAJOR — an incompatible change to a surface a user or a script drives:
//	    a CLI verb or flag removed/renamed, an output format a consumer parses
//	    (the `revision:` line the post-merge rebuild verifier reads is one),
//	    a settings.json key dropped or given a new meaning.
//	  - MINOR — a new user-visible capability or verb, backwards compatible.
//	  - PATCH — a fix, a doc-only landing, an internal refactor with no
//	    user-visible surface change.
//
// The value is bumped IN THE SAME COMMIT as the change that warrants it, by
// the author of that change. There is no release-time step and no tool that
// rewrites it: the string is in the source, so it is in the binary, so it is
// what the running artifact says about itself. A pre-1.0 MAJOR is a promise,
// not a number: v0.y.z means the surfaces above may still move.
//
// A leading `v` and the three numeric fields are the FORMAT (versionShape),
// and a test pins that this default satisfies it — a malformed default is the
// one failure nobody would notice until a release.
//
// CURRENT VALUE — why v0.5.0 (MINOR bump, 2026-11-14): the OPEN-SOURCE
// READINESS bump. No verb changed shape and no output format a consumer parses
// moved; what earned the MINOR is that the tree can now be published as it
// stands — the module path is the public one, and the fleet-specific material a
// stranger cannot use is gone. A reader comparing v0.4.0 and v0.5.0 sees one
// history: v0.4.0 was the last version of the private working tree, v0.5.0 is
// the first of the public one.
//
// PREVIOUS — why v0.4.0 (MINOR bump, 2026-11-01): THREE user-visible
// surfaces changed shape in the two weeks since v0.3.0, and each one is read by
// somebody outside this repository.
//
//  1. `git_blame` — every commit it returns now carries `task_id`,
//     `task_summary`, `task_status` and `task_host`, joined from the row that
//     the commit's `task:` trailer names (4cc2b5ba). A consumer that reads the
//     tool's JSON gets four fields it did not have before, and the trailer/row
//     resolution happens inside the tool rather than at the call site.
//
//  2. The chat action-class table LEFT THE BINARY and lives in a TRACKED
//     project file, `.gopi/chat-policy.md` (663f8990). The compiled-in table
//     now names no path at all; a project declares its own classes in its own
//     checkout, and the resolver walks up to the directory holding `.git` to
//     find it. Precedence is file > `GOPI.md` fence > built-in. Nobody needs a
//     new gopi build to change what counts as a repository write.
//
//  3. `gopi session replay --full` lifts the row ceiling as well as the
//     line ceiling (67eee3cd) — the flag used to reach the WIDTH of a row and
//     not how many rows exist, so a reader asking for the whole material got
//     15,550 lines where the material holds 29,633.
//
// WHY MINOR AND NOT MAJOR: no verb and no flag was REMOVED, every pre-existing
// alias still runs, and `gopi version`'s own report shape is untouched. What
// changed is what a surface RETURNS — additive fields, a relocated table with a
// compatible fallback, and a flag that now reaches material it was always
// documented to reach. Additive and backwards compatible on the surfaces that
// matter is MINOR by this file's own scale.
//
// PREVIOUS — why v0.3.0 (MINOR bump, 2026-10-25): the session's unit
// vocabulary was settled on the operator's ruling. The session report and
// `gopi session replay` now print ONE number — `turns=` — where they used to
// print `rounds=` and `turns=` side by side, and the JSONL's `user` row
// records which FACE produced the line (human / self-driven / queued) so a
// self-typed turn stays countable AND traceable. That is a format a consumer
// reads, so the shape change is real: a script parsing `rounds=` out of the
// session report no longer finds it (the `--rounds` table and its flag are
// still there as a compatibility alias, unchanged).
//
// WHY MINOR AND NOT MAJOR: no verb or flag was removed, the `gopi version`
// report shape is untouched (internal/tools/rebuild.go's RevisionOfDisplay
// still reads the same lines), and every pre-existing alias still runs. A new
// user-visible vocabulary plus a new recording field, backwards compatible on
// the surfaces that matter, is MINOR by this file's own scale.
//
// THE HISTORY BELOW IS KEPT ON PURPOSE — a reader comparing two binaries needs
// to know what each bump meant, and the previous entry is the one that will be
// cited when v0.2.0 turns up on a peer.
//
// PREVIOUS — why v0.2.0 (MINOR bump, 2026-10-16): the framework change that
// earned it is the git-clone workspace channel — a cloned repo can now be a
// task's GROUND (a row carries the workspace as its ground, not a derived
// worktree name) and that row is driven through a dev_repo switch plus a
// pull/build/push loop.
const DefaultVersion = "v0.5.0"

// MilestoneDate is WHEN THIS VERSION WAS DECLARED — the calendar fact of the
// milestone, not the date of whichever commit happened to carry this file.
//
// WHY IT IS A HAND-WRITTEN CONSTANT AND NOT `vcs.time` (operator's ruling,
// 2026-10-25): `gopi version` used to print the COMMITTER DATE of the binary's
// revision under the name `built:` and then under the name `committed:`. Both
// labels reported a physical fact about one commit, and neither answered the
// question a reader of a version line actually has — "when was this milestone
// declared?". The two diverge the moment the milestone is bumped in a commit
// whose own date is older (a rebase, a cherry-pick, an amend), which is exactly
// what happened on 2026-10-25: the report said 2026-10-24 while v0.3.0 was
// being declared.
//
// The operator's rule is therefore: WHEN YOU BUMP DefaultVersion, SET THIS TO
// THAT MOMENT, in the same commit. It is one line, it is written by hand, and
// it is the whole point — the milestone date is a DECISION the author records,
// not a timestamp some later commit overwrites. Later commits do not touch it:
// a version is declared once and stays declared at the day it was declared.
//
// FORMAT: RFC3339 in UTC, e.g. "2026-10-25T06:20:00Z". A test pins the shape
// (versionMilestoneShape), because a malformed date is the one failure nobody
// notices until a release page renders garbage.
const MilestoneDate = "2026-11-01T19:10:00Z"

// Version is the version this binary reports. It is a VARIABLE, not a
// constant, for exactly one reason: a release build may stamp an artifact
// that is not the tree's default with
//
//	go build -ldflags "-X github.com/nickhuang99/gopi/internal/version.Version=v1.2.3"
//
// and only a variable can be set by the linker. The compile-time default is
// DefaultVersion, so a build with NO ldflags — the documented build (Rule 12,
// `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`) — still carries a
// well-formed, hard-coded version.
var Version = DefaultVersion

// Revision is the revision THIS BINARY was told it was built from — named by
// the BUILD RECIPE and set by the linker (a `-X` operand, never computed at run
// time), through exactly this command:
//
//	CGO_ENABLED=0 go build -ldflags "-X <RevisionLdflagPath>=$(git rev-parse HEAD)" -o bin/gopi ./cmd/agent/
//
// WHY A SECOND SOURCE EXISTS AT ALL (measured 2026-10-13, Go 1.24.0, and read
// out of the toolchain's own source). The toolchain's `vcs.revision` stamp is
// written only when cmd/go recognizes the build directory as a repository root,
// and it recognizes one by a `.git` DIRECTORY —
// cmd/go/internal/vcs/vcs.go: `vcsGit.RootNames` carries
// `{filename: ".git", isDir: true}`, and `isVCSRoot` accepts it only when
// `fi.IsDir() == root.isDir`. A LINKED worktree — the shape every gopi task
// ground has (Rule 6a) — carries `.git` as a FILE (`gitdir: …`) instead, so the
// toolchain never even invokes git and drops the stamp SILENTLY: the same
// command under `-buildvcs=true` (the spelling that fails loudly when it cannot
// DETECT a VCS) still produces 0 `vcs.*` settings rather than an error. A
// reader checking `go version -m | grep vcs` then finds nothing, which is
// indistinguishable from "never checked" — the exact reason Rule 31's
// binary-identity evidence could not be produced where a run is allowed to
// build.
//
// This variable carries the same identity on the BUILD RECIPE's word: the
// toolchain cannot stamp a shape it does not recognize, but the recipe knows
// which commit it is building and can say so. It is NOT a toolchain stamp and
// MUST NOT be presented as one — nothing verifies that the tree really was that
// revision, which is why `gopi version` labels the value with its own
// provenance line (cmd/agent/cli_version.go) whenever this is what answered.
// Wherever a toolchain stamp DOES exist it wins: the stamp is attested, this is
// declared.
var Revision = ""

// RevisionLdflagPath is the `-X` operand that sets Revision. The linker's syntax
// is `-X importpath.name=value`, so the path and the variable name have to be
// spelled together and must match this package's own import path; naming it
// ONCE is what stops the documented recipe and the test that exercises it from
// drifting into two spellings (Rules 1b/13/14).
const RevisionLdflagPath = buildidentity.RevisionLdflagPath

// The build-metadata keys the Go toolchain writes into a binary built from a
// git checkout (runtime/debug.BuildInfo.Settings). They are the TOOLCHAIN's
// wire names rather than gopi's, but they are values more than one package
// must agree on — the version command prints them, internal/tools reads them
// back to decide whether the running artifact is the committed tip, and
// internal/sessionevent records the revision in a session — so their ONE
// definition lives in internal/buildidentity and is re-exported here
// (Rules 13/14).
//
// WHY THEY MOVED OUT OF THIS PACKAGE (measured): internal/sessionevent needs
// the revision key to write a session's artifactIdentity, and it CANNOT import
// this package — the test build closes a cycle (internal/version's testmain →
// internal/testutil → internal/sessionevent → internal/version) that Go
// refuses with `import cycle not allowed in test`. A leaf constant package
// every consumer can import is the Rule 14 answer, the same one
// RevisionLdflagPath above already uses. The names are kept as aliases so
// every existing reader in this tree keeps its spelling.
const (
	// BuildInfoKeyRevision is the revision `go build` stamped into the
	// binary ("" whenever the toolchain embedded no stamp — a build outside
	// any repository, and also a build whose repository root it did not
	// recognize, e.g. a linked worktree; cmd/agent/cli_version.go states the
	// absence and its measurements).
	BuildInfoKeyRevision = buildidentity.BuildInfoKeyRevision
	// BuildInfoKeyTime is the committer time (RFC3339, UTC) of that revision.
	BuildInfoKeyTime = buildidentity.BuildInfoKeyTime
	// BuildInfoKeyModified is set to BuildInfoValueModifiedTrue when the
	// source tree had uncommitted tracked changes at build time.
	BuildInfoKeyModified = buildidentity.BuildInfoKeyModified
	// BuildInfoValueModifiedTrue is the only value of BuildInfoKeyModified
	// that means dirty; any other value means clean.
	BuildInfoValueModifiedTrue = buildidentity.BuildInfoValueModifiedTrue
)

// versionShape is the format every version value must have: a leading `v`,
// three dot-separated numeric fields, and an optional pre-release/build tail
// ("-rc1", "-dirty.2"). It is a named constant (Rule 1b) because the
// validator and the doc that promises the format must not drift.
const versionShape = `^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`

// versionShapeRe is versionShape compiled once — Validate runs in a test and
// must not pay a compile per call.
var versionShapeRe = regexp.MustCompile(versionShape)

// Validate reports whether s is a well-formed version string
// (versionShape). Callers use it where a malformed value would travel
// further than a returned error: the build stamp, a settings override, a
// test expectation.
func Validate(s string) error {
	if versionShapeRe.MatchString(s) {
		return nil
	}
	return fmt.Errorf("version %q does not match the version format %s", s, versionShape)
}
