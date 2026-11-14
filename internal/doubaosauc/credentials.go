package doubaosauc

// credentials.go — WHERE THE v3 CREDENTIAL COMES FROM, and the ONE shape the
// upgrade request carries it in.
//
// THE TWO SHAPES ARE NOT INTERCHANGEABLE, and that is the measurement this
// whole channel exists to make usable (measured 2026-10-13 on this account,
// same endpoint, same resource id, same 4.83 s clip):
//
//	X-Api-Key: <UUID>                                    → 403 requested resource not granted
//	X-Api-App-Key: <AppID> + X-Api-Access-Key: <Token>   → 101 Switching Protocols
//
// The 403 READS like "this product line was never enabled", and the actual
// cause was that the UUID key belongs to a different application — a shape
// mismatch, not an entitlement. Both shapes stay implemented because the
// CONTROL is what makes that comparison reproducible: a client that can only
// speak the working shape cannot show anyone why the other one fails.
//
// THE FILE IS THE OPERATOR'S EXISTING ONE. `~/.doubao/v2_voice.env` is a
// KEY=VALUE document whose APPID and TOKEN lines are exactly this channel's
// legacy pair, so the credential is read through doubaostream.LoadCredentials
// and located through doubaostream.DefaultCredentialPath — one credential
// mechanism, no second file to keep in sync (Rule 13/14). The path is a
// DEFAULT, and the knob that names another one is the key-FILE channel gopi
// already has (--doubao-key-file / $GOPI_AUDIO_KEY_FILE / the nested settings
// key), resolved by the CLI and passed in as Options.CredentialFile.

import (
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/doubaostream"
)

// Credentials is a resolved v3 credential: the values plus, when they came from
// a file, the path they were read from. THE VALUES ARE NEVER PRINTED — every
// message this package produces names a path, a header or a knob.
type Credentials struct {
	// AppKey / AccessKey are the legacy console pair (the measured working
	// shape). Both empty when the single-key shape was chosen.
	AppKey    string
	AccessKey string
	// APIKey is the single-key shape. Empty when the legacy pair was chosen.
	APIKey string
	// Path is the file the values came from, or "" when they were supplied
	// inline (or not at all).
	Path string
	// Shape names which of the two was resolved, for the banner and the
	// refusal: legacyShape or apiKeyShape.
	Shape string
}

// The two shape names. Constants because the refusal, the banner and the tests
// all quote them (Rule 1b/13).
const (
	legacyShape = "legacy console pair (" + HeaderAppKey + " + " + HeaderAccessKey + ")"
	apiKeyShape = "single API key (" + HeaderAPIKey + ")"
)

// Credential knobs named by the refusal. They are spelled from the config
// package's constants where one exists, so a message cannot point an operator
// at a key that was renamed.
const (
	// credentialHint names the file the channel reads by default, in prose that
	// quotes the loader's own default rather than a second copy of the path.
	credentialHint = "the v3 SAUC credential is a KEY=VALUE file carrying APPID and TOKEN " +
		"(the same document the v2 streaming channel reads)"
	// credentialShapeHint states the two shapes once, so the refusal and the
	// privacy banner cannot describe them differently.
	credentialShapeHint = "two shapes are supported, and only one of them works for " +
		"a given application: the legacy console pair (" + HeaderAppKey + " + " + HeaderAccessKey +
		") or a single API key (" + HeaderAPIKey + ")"
)

// ResolveCredentials decides which credential shape this run uses and refuses,
// BY NAME, when neither can be produced.
//
// The precedence is:
//
//	Options.APIKey            (the single-key shape, when a caller states it)
//	Options.AppKey/AccessKey  (the legacy pair, when a caller states it)
//	the credential FILE       (APPID + TOKEN → the legacy pair)
//
// A caller-supplied value outranks the file because an explicit value is the
// operator's own statement; the file answers when nobody stated one. The
// refusal is a REFUSAL and not a warning: this channel has exactly one way to
// authenticate, and a run without it would open a socket only to be told 403 —
// which is the confusing report this function exists to prevent.
func ResolveCredentials(opts Options) (Credentials, error) {
	apiKey := strings.TrimSpace(opts.APIKey)
	appKey := strings.TrimSpace(opts.AppKey)
	accessKey := strings.TrimSpace(opts.AccessKey)

	if apiKey == "" && appKey == "" && accessKey == "" {
		path := strings.TrimSpace(opts.CredentialFile)
		if path == "" {
			// The DEFAULT location, from the transport package's own name
			// constants (DefaultCredentialDir/File) — never typed here: a second
			// spelling of the operator's credential path is a second file to keep
			// in sync (Rule 13/14).
			home, err := os.UserHomeDir()
			if err != nil {
				return Credentials{}, &APIError{Op: opCredential,
					Message: "locate the home directory to find the v3 SAUC credential: " + err.Error(), Err: err}
			}
			path = doubaostream.DefaultCredentialPath(home)
		}
		cred, err := doubaostream.LoadCredentials(path)
		if err != nil {
			return Credentials{}, &APIError{
				Op: opCredential,
				Message: fmt.Sprintf("%s. %s. Loaded no credential: %v",
					credentialHint, credentialShapeHint, err),
				Err: err,
			}
		}
		return Credentials{
			AppKey: cred.AppID, AccessKey: cred.Token,
			Path: cred.Path, Shape: legacyShape,
		}, nil
	}
	// A HALF-STATED PAIR IS REFUSED rather than completed from the file: the
	// two halves are one credential, and mixing a stated half with a file's
	// other half produces a pair that authenticates as NEITHER application —
	// the same class of defect as the 403 above, with a much worse error
	// message.
	if apiKey == "" && (appKey == "") != (accessKey == "") {
		missing := HeaderAccessKey
		if accessKey == "" {
			missing = HeaderAppKey
		}
		return Credentials{}, &APIError{
			Op: opCredential,
			Message: fmt.Sprintf("the legacy credential pair is half-stated: %s is missing while its "+
				"partner is set. %s", missing, credentialShapeHint),
		}
	}
	if apiKey != "" {
		return Credentials{APIKey: apiKey, Shape: apiKeyShape}, nil
	}
	return Credentials{AppKey: appKey, AccessKey: accessKey, Shape: legacyShape}, nil
}

// UpgradeHeaders is the credential as the upgrade request carries it: the two
// header fields of whichever shape was resolved, plus the resource id and the
// caller's request id.
//
// THE ORDER IS PART OF THE VALUE: the returned slice is written to the request
// in exactly this order, so a run being debugged can state what it sent. The
// resource id is ALWAYS written (never left to a service-side default) because
// it is what the request is billed against, and the request id is what the
// vendor's support asks for beside the log id.
//
// NO VALUE IS EVER LOGGED; this function only moves them into a header list.
func (c Credentials) UpgradeHeaders(resourceID, reqID string) []doubaostream.WsHeader {
	headers := make([]doubaostream.WsHeader, 0, 4)
	if c.APIKey != "" {
		headers = append(headers, doubaostream.WsHeader{Name: HeaderAPIKey, Value: c.APIKey})
	} else {
		headers = append(headers,
			doubaostream.WsHeader{Name: HeaderAppKey, Value: c.AppKey},
			doubaostream.WsHeader{Name: HeaderAccessKey, Value: c.AccessKey})
	}
	return append(headers,
		doubaostream.WsHeader{Name: HeaderResourceID, Value: resourceID},
		doubaostream.WsHeader{Name: HeaderRequestID, Value: reqID})
}

// CredentialLabel is what a caller may PRINT about a resolved credential: the
// file it came from, or the shape it was supplied in. NEVER a value, and never
// a prefix of one — a prefix is still a credential leak when the credential is
// short (the probe's first version printed "first 2 chars + length", which is a
// redaction of a secret's shape rather than of its content).
func (c Credentials) CredentialLabel() string {
	if c.Path != "" {
		return "file " + c.Path
	}
	if c.Shape != "" {
		return "inline " + c.Shape
	}
	return "unresolved credential"
}

// CredentialFileModeNote reports a credential file whose mode is wider than
// 0600, or "" when it is not — reusing the v2 arm's check so a host cannot end
// up with two opinions about what an acceptable credential-file mode is.
func CredentialFileModeNote(path string) string {
	if path == "" {
		return ""
	}
	note := doubaostream.CheckCredentialFileMode(path)
	if note == "" {
		return ""
	}
	return "the SAUC credential " + strings.TrimPrefix(note, "the streaming credential ")
}
