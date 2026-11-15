// Regression tests for the auth-shape change (legacy console credentials).
//
// The defect this pins: the probe used to hardcode ONE header (X-Api-Key with
// a UUID key), which the v3 SAUC endpoint answers with 403 "requested resource
// not granted" — reading like a missing entitlement while the real cause is
// that the credential is of the other shape. The contract under test is small
// and exact:
//
//	legacy -> exactly {X-Api-App-Key, X-Api-Access-Key}
//	apikey -> exactly {X-Api-Key}
//
// plus "the announced/redacted view of a credential never carries it whole".
package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// testdataFixtures are READ-ONLY inputs. The suite no longer creates its
// credential files at run time in a per-test temp directory: this is a
// standalone nested module (doubao-minclient, zero-dependency), so it cannot
// import internal/testutil to pin a scratch root, and a file that is only READ
// has no reason to be written at all. Keeping the bytes in testdata also puts
// them in review, which the inline WriteFile calls did not.
const (
	credFixtureDir  = "testdata"
	credFixtureName = "v2_voice.env"
	keyFixtureName  = "apikey.txt"
)

const (
	testAppID   = "5210362471"
	testToken   = "tok-abcdefghijklmnop"
	testUUIDKey = "35a7881f-0000-4000-8000-000000000000"
)

func legacyCreds() credentials {
	return credentials{appID: testAppID, accessToken: testToken}
}

func TestAuthHeadersLegacyEmitsExactlyTheAppKeyPair(t *testing.T) {
	hs := authHeaders(AuthLegacy, testUUIDKey, legacyCreds())
	if len(hs) != 2 {
		t.Fatalf("legacy auth emitted %d headers, want 2 (the app-key pair)", len(hs))
	}
	if hs[0][0] != HeaderAppKey || hs[0][1] != testAppID {
		t.Errorf("header 0 = %q=%q, want %s=%s", hs[0][0], hs[0][1], HeaderAppKey, testAppID)
	}
	if hs[1][0] != HeaderAccessKey || hs[1][1] != testToken {
		t.Errorf("header 1 = %q=%q, want %s=<access token>", hs[1][0], hs[1][1], HeaderAccessKey)
	}
	for _, h := range hs {
		if h[0] == HeaderAPIKey {
			t.Errorf("legacy auth must NOT emit %s (that is the other scheme, and the 403's cause)", HeaderAPIKey)
		}
	}
}

func TestAuthHeadersAPIKeyEmitsExactlyOneHeader(t *testing.T) {
	hs := authHeaders(AuthAPIKey, testUUIDKey, credentials{})
	if len(hs) != 1 {
		t.Fatalf("apikey auth emitted %d headers, want 1", len(hs))
	}
	if hs[0][0] != HeaderAPIKey || hs[0][1] != testUUIDKey {
		t.Errorf("header = %q=%q, want %s=<uuid key>", hs[0][0], hs[0][1], HeaderAPIKey)
	}
	for _, h := range hs {
		if h[0] == HeaderAppKey || h[0] == HeaderAccessKey {
			t.Errorf("apikey auth must NOT emit %s (legacy-only header)", h[0])
		}
	}
}

func TestBuildHandshakeCarriesAuthOnceAndKeepsTheUpgrade(t *testing.T) {
	for _, tc := range []struct {
		mode  string
		key   string
		creds credentials
		want  []string
		not   []string
	}{
		{AuthLegacy, testUUIDKey, legacyCreds(), []string{HeaderAppKey, HeaderAccessKey}, []string{HeaderAPIKey}},
		{AuthAPIKey, testUUIDKey, credentials{}, []string{HeaderAPIKey}, []string{HeaderAppKey, HeaderAccessKey}},
	} {
		req := string(buildHandshake("/api/v3/sauc/bigmodel_nostream", "openspeech.bytedance.com",
			"dGhlIHNhbXBsZSBub25jZQ==", "req-1", ResourceSeedASR20, tc.mode, tc.key, tc.creds))

		for _, want := range tc.want {
			if n := strings.Count(req, "\r\n"+want+":"); n != 1 {
				t.Errorf("[%s] %s appears %d times, want exactly 1\n%s", tc.mode, want, n, req)
			}
		}
		for _, not := range tc.not {
			if strings.Contains(req, "\r\n"+not+":") {
				t.Errorf("[%s] request must not carry %s\n%s", tc.mode, not, req)
			}
		}
		// The resource id is what selects the billing entitlement; it is never
		// implicit (a wrong/absent value is a 403 of a different colour).
		if !strings.Contains(req, "\r\n"+HeaderResourceID+": "+ResourceSeedASR20+"\r\n") {
			t.Errorf("[%s] resource id missing from handshake\n%s", tc.mode, req)
		}
		if !strings.HasPrefix(req, "GET /api/v3/sauc/bigmodel_nostream HTTP/1.1\r\n") {
			t.Errorf("[%s] request line malformed: %q", tc.mode, strings.SplitN(req, "\r\n", 2)[0])
		}
		if !strings.HasSuffix(req, "\r\n\r\n") {
			t.Errorf("[%s] handshake is not terminated by a blank line", tc.mode)
		}
	}
}

// The banner is what ends up in logs and transcripts, so it must be redacted.
func TestAuthAnnounceNeverCarriesTheSecretWhole(t *testing.T) {
	legacy := authAnnounce(AuthLegacy, testUUIDKey, legacyCreds(), "keyfile", "cred.env")
	if strings.Contains(legacy, testToken) {
		t.Errorf("legacy banner leaked the access token: %q", legacy)
	}
	if !strings.Contains(legacy, HeaderAppKey) || !strings.Contains(legacy, HeaderAccessKey) {
		t.Errorf("legacy banner must name the headers it sends: %q", legacy)
	}

	single := authAnnounce(AuthAPIKey, testUUIDKey, credentials{}, "keyfile", "cred.env")
	if strings.Contains(single, testUUIDKey) {
		t.Errorf("apikey banner leaked the key: %q", single)
	}
}

func TestRedactShortensAndNeverEchoesWhole(t *testing.T) {
	got := redact(testToken)
	if strings.Contains(got, testToken) {
		t.Fatalf("redact returned the whole credential: %q", got)
	}
	if !strings.Contains(got, itoa(len(testToken))) {
		t.Errorf("redact should name the true length: %q", got)
	}
	if redact("") != "(unset)" {
		t.Errorf("redact('') = %q, want (unset)", redact(""))
	}
	// A one-character credential must not panic nor widen the prefix.
	short := redact("x")
	if strings.Contains(short, "(0 chars)") {
		t.Errorf("redact of a 1-char value lost its length: %q", short)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestParseCredFileReadsAppIDAndToken(t *testing.T) {
	c, err := parseCredFile([]byte("# comment line\n\nAPPID=5210362471\nTOKEN=tok-abcdefghijklmnop\nSECRET=SecretKey\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.appID != testAppID || c.accessToken != testToken {
		t.Fatalf("parsed %+v, want appID/token from the file", c)
	}
	if !c.Complete() {
		t.Error("Complete() = false on a full pair")
	}
}

func TestParseCredFileAcceptsAccessTokenAliasAndRefusesPartial(t *testing.T) {
	c, err := parseCredFile([]byte("APPID=1\nACCESS_TOKEN=abc\n"))
	if err != nil {
		t.Fatalf("alias form should parse: %v", err)
	}
	if c.accessToken != "abc" {
		t.Errorf("ACCESS_TOKEN alias not honoured: %+v", c)
	}
	if _, err := parseCredFile([]byte("APPID=1\n")); err == nil {
		t.Error("a file with no token must be refused, not silently half-read")
	}
}

func TestResolveCredentialsLegacyPrefersFlagsOverFile(t *testing.T) {
	path := filepath.Join(credFixtureDir, credFixtureName)
	creds, key, err := resolveCredentials(AuthLegacy, testAppID, "", path, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if creds.appID != testAppID {
		t.Errorf("flag app id should win: got %q", creds.appID)
	}
	if creds.accessToken != "filetok" {
		t.Errorf("missing half should come from the file: got %q", creds.accessToken)
	}
	if key != "" {
		t.Errorf("legacy mode must not return a single key, got %q", key)
	}
}

// A legacy run must not depend on the single-key file, and vice versa: reading
// only the source the mode needs is what keeps one scheme's absence from
// failing the other.
func TestResolveCredentialsReadsOnlyTheSourceItsModeNeeds(t *testing.T) {
	keyPath := filepath.Join(credFixtureDir, keyFixtureName)
	creds, key, err := resolveCredentials(AuthAPIKey, "", "", "no/such/cred.env", keyPath)
	if err != nil || key != testUUIDKey {
		t.Errorf("apikey mode: key=%q err=%v (must ignore the credential file)", key, err)
	}
	if creds.Complete() {
		t.Errorf("apikey mode must not populate the legacy pair: %+v", creds)
	}
	if _, _, err := resolveCredentials(AuthLegacy, testAppID, testToken, "no/such/cred.env", "no/such/key.txt"); err != nil {
		t.Errorf("legacy mode with both flags must ignore the files: %v", err)
	}
	if _, _, err := resolveCredentials(AuthLegacy, "", "", "no/such/cred.env", ""); err == nil {
		t.Error("legacy mode with no credential anywhere must fail loudly")
	}
}
