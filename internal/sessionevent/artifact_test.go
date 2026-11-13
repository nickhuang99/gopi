package sessionevent

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestArtifactIdentityAbsenceIsNotABlank is the Rule-15a regression for the
// block: an artifact that carries NEITHER a toolchain stamp nor a recipe's
// declaration must round-trip as an EXPLICIT absence carrying its reason — not
// as an object whose "revision" is empty. A reader that saw a blank could not
// tell "this build carries no identity" from "this writer stopped recording
// identity", which is the whole defect the block exists to remove.
func TestArtifactIdentityAbsenceIsNotABlank(t *testing.T) {
	absent := artifactIdentityFrom("/usr/local/bin/gopi", "", "")
	if absent.Source != ArtifactSourceAbsent {
		t.Fatalf("an artifact with neither source was recorded as %q, want %q", absent.Source, ArtifactSourceAbsent)
	}
	if absent.Reason == "" {
		t.Fatal("the absent arm carries no reason — an absence must be STATED")
	}
	b, err := json.Marshal(absent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	if strings.Contains(got, `"revision"`) {
		t.Errorf("the absent shape carries a revision key (%s) — a blank reads as a value that was lost", got)
	}
	if !strings.Contains(got, `"source":"`+ArtifactSourceAbsent+`"`) {
		t.Errorf("the absent shape does not carry its source: %s", got)
	}
	var back ArtifactIdentity
	if uerr := json.Unmarshal(b, &back); uerr != nil {
		t.Fatalf("unmarshal: %v", uerr)
	}
	if back != absent {
		t.Errorf("round trip lost the block: %+v != %+v", back, absent)
	}
	if _, ok, derr := DecodeArtifactIdentity(b); derr != nil || !ok {
		t.Errorf("the absent shape is not decodable: (ok=%v, err=%v)", ok, derr)
	}
}

// TestArtifactIdentityPrecedence pins the rule the two writers disagree about:
// an ATTESTED toolchain stamp always beats a recipe's DECLARATION, and the
// declaration answers only where the stamp is silent. Without this arm a
// resolver that always answered the declaration would look correct in the one
// arm a worktree build produces.
func TestArtifactIdentityPrecedence(t *testing.T) {
	const (
		attested  = "adafe54b259ea7e10efd6dbaf2b278ad706a510b"
		declared  = "1111111111111111111111111111111111111111"
		exePath   = "/usr/local/bin/gopi"
		emptyPath = ""
	)

	stamped := artifactIdentityFrom(exePath, declared, attested)
	if stamped.Source != ArtifactSourceToolchainVCS || stamped.Revision != attested {
		t.Fatalf("a stamped build answered %+v, want the ATTESTED revision %s from %q",
			stamped, attested, ArtifactSourceToolchainVCS)
	}

	declaredOnly := artifactIdentityFrom(exePath, declared, "")
	if declaredOnly.Source != ArtifactSourceLdflagsSelfDeclared || declaredOnly.Revision != declared {
		t.Fatalf("a declaration-only build answered %+v, want the DECLARED revision %s from %q",
			declaredOnly, declared, ArtifactSourceLdflagsSelfDeclared)
	}

	// A whitespace-only declaration is not a value (Rule 15): the arm is the
	// RECORDED absence, not a source word wrapped around a blank.
	blank := artifactIdentityFrom(exePath, "   ", "")
	if blank.Source != ArtifactSourceAbsent || blank.Revision != "" {
		t.Fatalf("a blank declaration was read as %+v, want the absent arm", blank)
	}

	// The executable half travels with every arm, including the one where the
	// path itself is unknown: a run whose exe cannot be resolved records an
	// empty path rather than dropping the block (the reason is on stderr, and
	// the source/reason halves still describe the artifact).
	if blank.ExePath != exePath {
		t.Fatalf("the path half was lost: %+v", blank)
	}
	if got := artifactIdentityFrom(emptyPath, declared, attested); got.ExePath != "" {
		t.Fatalf("an unknown executable path was invented as %q", got.ExePath)
	}
}

// TestDecodeArtifactIdentitySeparatesSilenceFromNonsense is the reader half:
// an ABSENT field is ok=false (the caller decides what silence means), while a
// PRESENT field this package does not understand is an ERROR — collapsing the
// two would report a fallback the recording never asked for (Rule 15).
func TestDecodeArtifactIdentitySeparatesSilenceFromNonsense(t *testing.T) {
	if _, ok, err := DecodeArtifactIdentity(nil); ok || err != nil {
		t.Fatalf("a silent field answered (ok=%v, err=%v), want (false, nil)", ok, err)
	}
	if _, ok, err := DecodeArtifactIdentity(json.RawMessage(`{}`)); ok || err == nil {
		t.Fatalf("a field with no source answered (ok=%v, err=%v), want an error", ok, err)
	}
	if _, ok, err := DecodeArtifactIdentity(json.RawMessage(`{"source":"telepathy"}`)); ok || err == nil {
		t.Fatalf("an unknown source answered (ok=%v, err=%v), want an error", ok, err)
	}
	// A source word that carries no revision is ALSO nonsense, not an absence:
	// the two source kinds assert a revision and a record that names one
	// without carrying it is a broken recording.
	for _, src := range []string{ArtifactSourceToolchainVCS, ArtifactSourceLdflagsSelfDeclared} {
		raw := json.RawMessage(`{"source":"` + src + `"}`)
		if _, ok, err := DecodeArtifactIdentity(raw); ok || err == nil {
			t.Errorf("source %q with no revision answered (ok=%v, err=%v), want an error", src, ok, err)
		}
	}
}

// TestArtifactIdentityStringNeverPrintsABlankRevision pins the diagnostic
// renderer, so a caller composing a log line cannot produce "exe @ " — which
// reads as a truncated sentence rather than as a stated absence.
func TestArtifactIdentityStringNeverPrintsABlankRevision(t *testing.T) {
	absent := artifactIdentityFrom("/usr/local/bin/gopi", "", "")
	s := absent.String()
	if !strings.Contains(s, ArtifactSourceAbsent) {
		t.Errorf("the absent arm renders as %q, which does not state the absence", s)
	}
	if strings.HasSuffix(strings.TrimSpace(s), "@") {
		t.Errorf("the absent arm renders as %q — a dangling separator reads as a lost value", s)
	}
	if strings.Contains(s, "/usr/local/bin/gopi @  (") {
		t.Errorf("the absent arm printed an empty revision: %q", s)
	}
}
