package doubaosauc

// credentials_test.go — the SHAPE of the credential, which is the measurement
// this whole channel exists to make usable.
//
// A unit test cannot prove the vendor's endpoint answers one header set with 101
// and another with 403 (that is what the epic's real run is for). What it CAN
// pin is this half of the contract: given a credential of a stated shape, the
// request carries exactly the headers of that shape, in that order, and a run
// with no credential at all is REFUSED BEFORE A SOCKET IS OPENED — with a
// message that names both shapes and never a value.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/doubaostream"
)

// secretToken is a stand-in credential whose DISTINCTIVE text makes the
// redaction assertions meaningful: any message carrying it fails the test.
const secretToken = "SECRET-TOKEN-a4f19c-never-print-me"

// writeCredential writes a credential document the way the operator's own file
// is written (KEY=VALUE lines) and returns its path.
func writeCredential(t *testing.T, lines string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), doubaostream.DefaultCredentialFile)
	if err := os.WriteFile(path, []byte(lines), mode); err != nil {
		t.Fatalf("write the credential: %v", err)
	}
	return path
}

// TestResolveCredentialsReadsTheLegacyPairFromTheFile is the working shape: the
// APPID/TOKEN document the v2 channel already reads resolves to the legacy
// console pair, and the resolved value carries the PATH (the only thing a
// message may quote).
func TestResolveCredentialsReadsTheLegacyPairFromTheFile(t *testing.T) {
	path := writeCredential(t, "APPID=app-123\nTOKEN="+secretToken+"\n", 0o600)
	cred, err := ResolveCredentials(Options{CredentialFile: path})
	if err != nil {
		t.Fatalf("ResolveCredentials: %v", err)
	}
	if cred.AppKey != "app-123" || cred.AccessKey != secretToken {
		t.Fatalf("resolved the wrong pair: AppKey=%q AccessKey=%q", cred.AppKey, cred.AccessKey)
	}
	if cred.APIKey != "" {
		t.Fatalf("both shapes were filled: APIKey=%q", cred.APIKey)
	}
	if cred.Path != path {
		t.Fatalf("Path = %q, want the file it was read from", cred.Path)
	}

	headers := cred.UpgradeHeaders(DefaultResourceID, "req-1")
	want := []doubaostream.WsHeader{
		{Name: HeaderAppKey, Value: "app-123"},
		{Name: HeaderAccessKey, Value: secretToken},
		{Name: HeaderResourceID, Value: DefaultResourceID},
		{Name: HeaderRequestID, Value: "req-1"},
	}
	if len(headers) != len(want) {
		t.Fatalf("got %d headers, want %d: %+v", len(headers), len(want), headers)
	}
	for i := range want {
		if headers[i] != want[i] {
			t.Fatalf("header %d = %+v, want %+v (the ORDER is part of the value)", i, headers[i], want[i])
		}
	}
}

// TestResolveCredentialsRefusesWithNoCredentialAnywhere is the refusal the epic
// asks for: it names BOTH shapes, the file it looked for, and how to get back to
// the on-machine engine — and it happens with no socket in sight (Op is the
// credential step).
func TestResolveCredentialsRefusesWithNoCredentialAnywhere(t *testing.T) {
	// The default location is derived from the REAL home, so the test states a
	// path that does not exist instead of touching the operator's own file.
	missing := filepath.Join(t.TempDir(), "not-here.env")
	_, err := ResolveCredentials(Options{CredentialFile: missing})
	if err == nil {
		t.Fatalf("a run with no credential was accepted")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %T, want *APIError", err)
	}
	if apiErr.Op != opCredential {
		t.Fatalf("Op = %q, want %q (refused before a socket)", apiErr.Op, opCredential)
	}
	for _, want := range []string{HeaderAppKey, HeaderAccessKey, HeaderAPIKey, missing} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %q: %v", want, err)
		}
	}
}

// TestResolveCredentialsRefusesAHalfStatedPair asserts the pair is ONE value: a
// stated half with an empty partner is refused rather than completed from the
// file, because a mixed pair authenticates as neither application — the same
// class of defect as the 403, with a worse message.
func TestResolveCredentialsRefusesAHalfStatedPair(t *testing.T) {
	path := writeCredential(t, "APPID=app-123\nTOKEN="+secretToken+"\n", 0o600)
	_, err := ResolveCredentials(Options{CredentialFile: path, AppKey: "app-123"})
	if err == nil {
		t.Fatalf("a half-stated pair was accepted")
	}
	if !strings.Contains(err.Error(), HeaderAccessKey) {
		t.Fatalf("the refusal does not name the missing half: %v", err)
	}
	// The file's other half must NOT have been used to complete the pair.
	if strings.Contains(err.Error(), secretToken) {
		t.Fatalf("the refusal leaked the file's value: %v", err)
	}
}

// TestResolveCredentialsSingleKeyShape asserts the CONTROL shape still works:
// an explicit API key produces exactly the single-key header, and no legacy
// field. It is kept implemented (and tested) because the 403 measurement is
// only meaningful when the same client can produce both halves of it.
func TestResolveCredentialsSingleKeyShape(t *testing.T) {
	cred, err := ResolveCredentials(Options{APIKey: "key-abc"})
	if err != nil {
		t.Fatalf("ResolveCredentials: %v", err)
	}
	headers := cred.UpgradeHeaders(DefaultResourceID, "req-2")
	if len(headers) != 3 || headers[0].Name != HeaderAPIKey || headers[0].Value != "key-abc" {
		t.Fatalf("the single-key shape sent %+v", headers)
	}
	for _, h := range headers {
		if h.Name == HeaderAppKey || h.Name == HeaderAccessKey {
			t.Fatalf("the single-key shape also sent a legacy header: %+v", h)
		}
	}
}

// TestCredentialLabelNeverCarriesAValue is the redaction contract: everything a
// caller may PRINT about a credential names a path or a shape, and a run's
// messages are the only place a credential could escape into a log.
func TestCredentialLabelNeverCarriesAValue(t *testing.T) {
	path := writeCredential(t, "APPID=app-123\nTOKEN="+secretToken+"\n", 0o600)
	cred, err := ResolveCredentials(Options{CredentialFile: path})
	if err != nil {
		t.Fatalf("ResolveCredentials: %v", err)
	}
	label := cred.CredentialLabel()
	if !strings.Contains(label, path) {
		t.Fatalf("the label does not name the file: %q", label)
	}
	for _, secret := range []string{secretToken, "app-123"} {
		if strings.Contains(label, secret) {
			t.Fatalf("the label leaked a value: %q", label)
		}
	}
	inline, err := ResolveCredentials(Options{AppKey: "app-123", AccessKey: secretToken})
	if err != nil {
		t.Fatalf("ResolveCredentials (inline): %v", err)
	}
	inlineLabel := inline.CredentialLabel()
	if !strings.Contains(inlineLabel, legacyShape) {
		t.Fatalf("the inline label does not name the shape: %q", inlineLabel)
	}
	for _, secret := range []string{secretToken, "app-123"} {
		if strings.Contains(inlineLabel, secret) {
			t.Fatalf("the inline label leaked a value: %q", inlineLabel)
		}
	}
}

// TestTranscribeRefusesWithoutACredential walks the PACKAGE's entry point, not
// just the resolver: the refusal must arrive before the dial, and the audio file
// is never opened (the path below does not exist).
func TestTranscribeRefusesWithoutACredential(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-here.env")
	_, err := Transcribe(context.Background(), Options{
		CredentialFile: missing,
		AudioFile:      filepath.Join(t.TempDir(), "no-such.wav"),
	})
	if err == nil {
		t.Fatalf("Transcribe ran with no credential")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Op != opCredential {
		t.Fatalf("err = %v (op %q), want a credential refusal", err, apiErrOp(err))
	}
	if strings.Contains(err.Error(), "no-such.wav") {
		t.Fatalf("the audio was opened before the credential was checked: %v", err)
	}
}

// TestCredentialFileModeNotePublishesAWideMode asserts the announcement the
// pre-flight check makes: a world-readable credential file is reported with its
// mode (and its chmod), and a 0600 file produces nothing.
func TestCredentialFileModeNotePublishesAWideMode(t *testing.T) {
	wide := writeCredential(t, "APPID=app-123\nTOKEN="+secretToken+"\n", 0o644)
	note := CredentialFileModeNote(wide)
	if note == "" {
		t.Fatalf("a 0644 credential file produced no note")
	}
	if !strings.Contains(note, "0600") || strings.Contains(note, secretToken) {
		t.Fatalf("note = %q, want the modes and no value", note)
	}
	tight := writeCredential(t, "APPID=app-123\nTOKEN="+secretToken+"\n", 0o600)
	if note := CredentialFileModeNote(tight); note != "" {
		t.Fatalf("a 0600 credential file produced a note: %q", note)
	}
}

// apiErrOp is a small helper for the message above: it reports the op of an
// *APIError, or "" for any other error, without a second errors.As at the call
// site.
func apiErrOp(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Op
	}
	return ""
}
