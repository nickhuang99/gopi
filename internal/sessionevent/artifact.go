package sessionevent

// artifact.go — the PROCESS-IDENTITY block a session records about the
// artifact that WROTE it: which executable ran the run, which revision that
// binary is the build of, and WHO says so.
//
// WHY IT IS A SESSION RECORD FIELD AND NOT A LOG LINE. Measured 2026-10-14
// (task 01a1355d, evidence
// ~/.gopi/sessions/01a1355d-d0f4-71b1-8d97-5305dd229431/artifacts/
// bug-worker-stale-binary.md): a task run's tool surfaces are executed by the
// WORKER's own binary, and that binary may come from ANOTHER checkout — the
// worker was `gopi worker` with exe /home/user/work/GoPi/bin/gopi (revision
// adafe54b) while the run's ground was a linked worktree whose source the run
// was editing. So "the branch is fixed and the package suite is green" and "the
// shape the live bash surface exhibits" are answers about two different code
// versions, and a report's `Ran:`/`Result:` lines cannot be pinned to a
// revision after the fact. The one place that can fix that is the run's OWN
// record: a session's session_start and metadata.json are written by the run
// itself, so the identity has to travel with the recording, not with a log the
// reader has to correlate.
//
// THE SHAPE FOLLOWS TerminalGeometry (geometry.go) DELIBERATELY: the
// discriminant field is REQUIRED and never omitted, the numbers/values that do
// not exist in a degraded arm are ABSENT rather than zeroed, and the reason a
// value is missing is carried in the record instead of being inferred from a
// blank (Rule 15 / 15a). Here the discriminant is SOURCE:
//
//	{"source":"toolchain-vcs","exePath":"…","revision":"adafe54b…"}
//	{"source":"ldflags-self-declared","exePath":"…","revision":"adafe54b…"}
//	{"source":"absent","exePath":"…","reason":"the Go toolchain embedded no VCS build settings …"}
//
// Three arms, because there are three facts a reader must be able to tell
// apart — and "no revision" is one of them, never a blank:
//
//   - toolchain-vcs: cmd/go inspected a repository root it recognized and
//     stamped vcs.revision. ATTESTED: the toolchain checked the tree.
//   - ldflags-self-declared: the toolchain stamped nothing (a LINKED WORKTREE
//     build gets none — cmd/go's isVCSRoot requires `.git` to be a DIRECTORY;
//     see devdoc/binary-identity-on-a-worktree-ground.md), and the revision is
//     the BUILD RECIPE's word, written by the linker through
//     version.RevisionLdflagPath. DECLARED: nothing verifies the tree really
//     was that revision.
//   - absent: no stamp and no declaration. The record NAMES the absence in the
//     reason field, so a reader can never confuse "this artifact carries no
//     identity" with "this writer stopped recording identity" (Rule 15).
//
// The vocabulary lives HERE rather than in the producer because these strings
// are the PERSISTED WIRE FORMAT of every session JSONL and metadata.json this
// writes (the same charter events.go states for the event names) — and because
// the readers (`gopi session log`, a jq probe, a future analysis pass) resolve
// the field through this package.
//
// The record is ADDITIVE and OPTIONAL: a session written by an older binary
// simply has no such key. Every existing reader tolerates it structurally — it
// is a JSON object with no "message" key, so the formatters that switch on
// event type render nothing for it (EventTypeSignalVerdict's own doc states the
// same contract), and DecodeArtifactIdentity answers ok=false for silence.

import (
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/nick/gopi/internal/buildidentity"
)

// ArtifactIdentityOf resolves the identity of the RUNNING artifact into the
// recorded shape: the executable path plus, when one exists, the revision this
// binary is the build of and WHICH party said so.
//
// declaredRevision is the build recipe's own claim (the value the linker wrote
// through version.RevisionLdflagPath), passed IN rather than read here: that
// variable lives in internal/version, and this package is the leaf the session
// wire format lives in, so it stays import-free (the same reason the event
// names live here and not in a consumer). The caller owns the wiring; this owns
// the SHAPE and the precedence.
//
// PRECEDENCE IS THE POINT, and it is the rule `gopi version` already prints: a
// TOOLCHAIN stamp is ATTESTED (cmd/go inspected a checkout it recognized), a
// recipe's declaration is not, so the stamp always wins and the declaration
// only answers where the stamp is silent — exactly a LINKED-WORKTREE build,
// where cmd/go's isVCSRoot requires `.git` to be a DIRECTORY and therefore
// never even invokes git (devdoc/binary-identity-on-a-worktree-ground.md).
//
// The toolchain's key is read from the artifact itself
// (runtime/debug.BuildInfo) through the SHARED leaf constant
// (buildidentity.BuildInfoKeyRevision), never typed here: three packages read
// this key back and a second spelling is the defect Rule 13 exists to stop
// (the package's first draft typed it inline; the leaf re-export is what made
// importing it possible — internal/version is unreachable from here because
// its test build closes a cycle through internal/testutil).
//
// An artifact with NEITHER is recorded as ArtifactSourceAbsent WITH THE REASON,
// never as an empty revision (Rule 15): "this build carries no identity" and
// "this writer stopped recording identity" must not look the same.
func ArtifactIdentityOf(exePath, declaredRevision string) ArtifactIdentity {
	return artifactIdentityFrom(exePath, declaredRevision, toolchainRevision())
}

// toolchainRevision reads the revision cmd/go stamped into THIS artifact, or ""
// when it stamped none. It is separated from the resolution below so the three
// arms of the shape can be exercised without a differently-built binary: the
// precedence IS the deliverable and it must be testable (Rule 11's spirit —
// a claim nobody can execute is not verified).
func toolchainRevision() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range bi.Settings {
		if s.Key != buildidentity.BuildInfoKeyRevision {
			continue
		}
		if rev := strings.TrimSpace(s.Value); rev != "" {
			return rev
		}
	}
	return ""
}

// artifactIdentityFrom is the PURE resolution: three inputs — the executable,
// the recipe's declaration, the toolchain's stamp — one shape, and the
// precedence rule stated above stated once. attested wins over declared; a
// declaration answers only where the stamp is silent; neither means the absence
// is RECORDED (with its reason), never blanked.
func artifactIdentityFrom(exePath, declaredRevision, attestedRevision string) ArtifactIdentity {
	id := ArtifactIdentity{ExePath: strings.TrimSpace(exePath)}
	if attested := strings.TrimSpace(attestedRevision); attested != "" {
		id.Revision = attested
		id.Source = ArtifactSourceToolchainVCS
		return id
	}
	if declared := strings.TrimSpace(declaredRevision); declared != "" {
		id.Revision = declared
		id.Source = ArtifactSourceLdflagsSelfDeclared
		return id
	}
	id.Source = ArtifactSourceAbsent
	id.Reason = ArtifactReasonNoVCSStamp
	return id
}

// FieldArtifactIdentity is the session-row / metadata key carrying the block.
// Spelled once: the writer, the decoder and the tests all reference it.
const FieldArtifactIdentity = "artifactIdentity"

// The source words — the value of the block's required "source" field.
const (
	// ArtifactSourceToolchainVCS means cmd/go recognized the build directory
	// as a repository root and wrote vcs.revision itself: ATTESTED.
	ArtifactSourceToolchainVCS = "toolchain-vcs"
	// ArtifactSourceLdflagsSelfDeclared means the toolchain wrote no stamp and
	// the revision came from the build recipe's `-X` operand: DECLARED, not
	// verified by anything.
	ArtifactSourceLdflagsSelfDeclared = "ldflags-self-declared"
	// ArtifactSourceAbsent means the artifact carries NO revision at all —
	// neither attested nor declared. It is a recorded FACT, never a blank.
	ArtifactSourceAbsent = "absent"
)

// The reason vocabulary for the arms that have no revision. Named (Rule 1b)
// because the writer composes them and the tests assert them.
const (
	// ArtifactReasonNoVCSStamp is the reason recorded when the artifact carries
	// no revision and no declaration — the toolchain's own silence, which is
	// the normal state of a linked-worktree build.
	ArtifactReasonNoVCSStamp = "the artifact carries no revision: the Go toolchain embedded no VCS " +
		"build settings for it (a linked-worktree build gets none, and so does -buildvcs=false) and no " +
		"build recipe declared one"
)

// ArtifactIdentity is the recorded identity of the process that wrote the
// session: its executable path plus the revision the artifact claims and the
// provenance of that claim.
//
// Revision and Reason are `omitempty` because in the arms where they do not
// apply they are not values at all — the rule TerminalGeometry states: the
// field whose ABSENCE carries meaning is the one that must not be written as a
// zero. Source is REQUIRED and never omitted, so the zero-value round trip is
// covered by a test (Rule 15a).
type ArtifactIdentity struct {
	// ExePath is the resolved path of the executable this process IS
	// (os.Executable at the moment the session was opened) — the fact that
	// makes "which binary ran this run" answerable after the run is gone.
	ExePath string `json:"exePath,omitempty"`
	// Revision is the revision the artifact claims. Its PROVENANCE is Source:
	// two writers produce this string and only one of them was checked by
	// anything.
	Revision string `json:"revision,omitempty"`
	// Source is the provenance of Revision and the block's required
	// discriminant — one of the ArtifactSource* values, never "".
	Source string `json:"source"`
	// Reason states why there is no revision, when Source is
	// ArtifactSourceAbsent. It is absent in the two arms that carry one.
	Reason string `json:"reason,omitempty"`
}

// String renders the block for a diagnostic line, so no caller composes that
// sentence out of a bare value: an absent revision must read as an absence,
// never as an empty revision followed by nothing.
func (a ArtifactIdentity) String() string {
	exe := a.ExePath
	if exe == "" {
		exe = "unknown executable"
	}
	if a.Revision != "" {
		return exe + " @ " + a.Revision + " (" + a.Source + ")"
	}
	if a.Reason != "" {
		return exe + " @ " + ArtifactSourceAbsent + " (" + a.Reason + ")"
	}
	return exe + " @ " + ArtifactSourceAbsent
}

// DecodeArtifactIdentity reads the block out of a session row's field.
//
// It answers an error — never a silent absent — when the field is present but
// does not carry a source this package defines: "the recording says something I
// do not understand" and "the recording says nothing" are different facts about
// a recording (DecodeTerminalGeometry's contract, Rule 15). A nil/absent field
// is reported by ok=false with no error, and the caller decides what silence
// means.
func DecodeArtifactIdentity(raw json.RawMessage) (a ArtifactIdentity, ok bool, err error) {
	if len(raw) == 0 {
		return ArtifactIdentity{}, false, nil
	}
	var decoded ArtifactIdentity
	if uerr := json.Unmarshal(raw, &decoded); uerr != nil {
		return ArtifactIdentity{}, false, fmt.Errorf("%s: %w", FieldArtifactIdentity, uerr)
	}
	switch decoded.Source {
	case ArtifactSourceToolchainVCS, ArtifactSourceLdflagsSelfDeclared:
		if decoded.Revision == "" {
			return ArtifactIdentity{}, false, fmt.Errorf(
				"%s: source %q carries no revision", FieldArtifactIdentity, decoded.Source)
		}
		return decoded, true, nil
	case ArtifactSourceAbsent:
		return decoded, true, nil
	case "":
		return ArtifactIdentity{}, false, fmt.Errorf("%s carries no %q field", FieldArtifactIdentity, "source")
	default:
		return ArtifactIdentity{}, false, fmt.Errorf(
			"%s carries an unknown source %q", FieldArtifactIdentity, decoded.Source)
	}
}
