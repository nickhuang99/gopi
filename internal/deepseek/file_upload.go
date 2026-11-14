package deepseek

// file_upload.go — the DeepSeek (Anthropic-compatible) Files API client: upload
// a picture ONCE, then reference it from a Messages request by file_id instead
// of inlining its base64 bytes on every turn.
//
// WHY IT EXISTS. A web turn that inlines a PNG pays for it three times: the
// request body grows by 4/3 of the bytes (base64), the SAME bytes travel again
// on every following turn, and the inline path is hard-capped at 32 MiB per
// picture. The documented transport for that case is the Files API — and the
// briefing's three documented facts were each verified against the live
// endpoint before this file was written (evidence in the run's report):
//
//  1. POST {base}/anthropic/v1/files, multipart with `purpose=user_data` and
//     the picture, returns the Anthropic-shaped object this file parses:
//     id / type / size_bytes / created_at (RFC 3339) / filename / mime_type.
//     Measured: HTTP 200,
//     {"id":"file-api-1b898a57-...","type":"file","size_bytes":75,
//     "created_at":"2026-11-06T21:21:11.505657605+00:00",
//     "filename":"gopi-probe.png","mime_type":"image/png"}.
//
//  2. A file reference on /anthropic/v1/messages REQUIRES the beta header
//     `anthropic-beta: files-api-2025-04-14`. Measured: WITHOUT it the request
//     is refused `HTTP 400 Missing or invalid anthropic-beta header`; WITH it
//     the same body is HTTP 200 and the model answered about the picture's
//     colour.
//
//  3. THE WIRE SHAPE IS `image` + `source.type=file`, NOT `type:file`. The
//     briefing (and this epic's deliverable list) said `{"type":"file",
//     "file_id":…}`; that shape is REFUSED by the real endpoint. Measured, both
//     flat and nested:
//
//     HTTP 422 Failed to deserialize the JSON body into the target type:
//     messages[0].content: unknown variant `file`, expected one of `text`,
//     `image`, `document`, `thinking`, …
//
//     while `{"type":"image","source":{"type":"file","file_id":"…"}}` is
//     HTTP 200. The source type is where the Files API lives; the block type
//     stays `image`. BuildClaudeRequest emits exactly that (see
//     claudeFileSource), and this correction is stated in the report rather
//     than silently implemented differently from the briefing.
//
// NO FALLBACK. A failed upload returns an error to the caller; nothing here
// rewrites the request to inline base64. A silent downgrade would make "we
// switched to the Files API" false exactly when the picture is large enough
// that falling back is destructive (Project Rule 15). Callers that want to ship
// an inline picture simply do not call this.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/nickhuang99/gopi/internal/rawlogfmt"
)

// Named constants — Project Rule 1. Every literal this file compares, stores or
// sends lives here.
const (
	// claudeFilesPath is the Files API path appended to the Anthropic base URL
	// by claudeFilesURL. The Anthropic SDKs append "/v1" themselves; gopi is a
	// hand-rolled HTTP client, so the full path is spelled here (a missing
	// "/v1" is a 404, not a redirect).
	claudeFilesPath = "/v1/files"

	// filesAPIBetaHeader / filesAPIBetaFilesAPI are the beta gate for a file
	// reference on /messages. Verified live: without the header the endpoint
	// answers 400 "Missing or invalid anthropic-beta header".
	filesAPIBetaHeader   = "anthropic-beta"
	filesAPIBetaFilesAPI = "files-api-2025-04-14"

	// betaHeaderValueSep is the separator an Anthropic beta header uses to
	// carry several features at once. setClaudeHeaders APPENDS to the list —
	// it never overwrites a value another feature installed.
	betaHeaderValueSep = ","

	// filesAPIPurposeField / filesAPIPurposeUserData are the multipart field
	// that tells the endpoint what the upload is for. "user_data" is the only
	// purpose that makes the file referenceable from a message.
	filesAPIPurposeField    = "purpose"
	filesAPIPurposeUserData = "user_data"

	// filesAPIFileField is the multipart field carrying the picture bytes.
	filesAPIFileField = "file"

	// claudeSourceTypeBase64 / claudeSourceTypeFile are the two transports an
	// image source may use; claudeImageBlockType is the CONTENT BLOCK type,
	// which stays "image" for both (see the measured 422 above).
	claudeSourceTypeBase64 = "base64"
	claudeSourceTypeFile   = "file"
	claudeImageBlockType   = "image"

	// The exported spellings below exist so a caller (and a test) that must
	// send or assert the Files API wire values takes them from THIS ONE place
	// instead of retyping the literals (Rules 1/14).
	FilesAPIBetaHeader  = filesAPIBetaHeader
	FilesAPIBetaValue   = filesAPIBetaFilesAPI
	FilesAPIPurposeKey  = filesAPIPurposeField
	FilesAPIPurposeData = filesAPIPurposeUserData
	FilesAPIFileField   = filesAPIFileField

	// ClaudeSourceTypeFileAPI is the source type a NAME outside this package
	// needs: an image block whose source references an uploaded file. Exported
	// (Rule 14) so the carrier that stages uploads spells the value once.
	ClaudeSourceTypeFileAPI = claudeSourceTypeFile

	// filesAPIMaxFileBytes is the per-file ceiling for a file_id reference
	// (64 MiB) — larger than the inline ceiling because the bytes no longer
	// ride inside the request body. filesAPIMaxInlineBytes is the inline
	// ceiling the 32 MiB web validator mirrors; it is named here because the
	// refusal below must state BOTH bounds to be actionable.
	filesAPIMaxFileBytes   = 64 << 20
	filesAPIMaxInlineBytes = 32 << 20
)

// ClaudeFile is one uploaded file as the Anthropic-compatible Files API
// describes it. The field names are NOT the OpenAI-compatible ones: size is
// `size_bytes` (not `bytes`), type is `type` (not `object`), and created_at is
// an RFC 3339 STRING (not a Unix second count). A wrong name deserializes to a
// zero value silently, which is why every field is parsed and the id is
// REQUIRED by UploadFile.
type ClaudeFile struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt string `json:"created_at"`
	Filename  string `json:"filename"`
	MIMEType  string `json:"mime_type"`
}

// claudeFilesURL returns the Files API URL for the configured base URL. It
// follows claudeMessagesURL's rule: a base that already ends in "/anthropic"
// (DeepSeek's own default) gets "/v1/files" appended, anything else gets the
// "/anthropic/v1/files" prefix. Measured against
// https://api.deepseek.com/anthropic → /anthropic/v1/files → HTTP 200.
func (c *Client) claudeFilesURL() string {
	base := strings.TrimRight(c.BaseURL, "/")
	if strings.HasSuffix(base, claudeAPIPath) {
		return base + claudeFilesPath
	}
	return base + claudeAPIPath + claudeFilesPath
}

// UploadFile sends ONE picture to the Files API and returns its descriptor
// (whose ID is the handle a later Messages request references).
//
// It fails loudly on every branch: an empty body, an oversize picture (naming
// both the file and the inline ceiling), a transport error, a non-200 status
// (carrying status + body), a body that is not the documented JSON, and a
// response with no id. The caller gets an error it can show the operator, and
// NOTHING here rewrites the request to inline base64.
func (c *Client) UploadFile(ctx context.Context, filename, mimeType string, data []byte) (*ClaudeFile, error) {
	if len(data) == 0 {
		return nil, errors.New("deepseek: files api upload refused: empty file body")
	}
	if len(data) > filesAPIMaxFileBytes {
		return nil, fmt.Errorf("deepseek: files api upload refused: %d bytes exceeds the %d-byte per-file limit (inline would cap at %d bytes)",
			len(data), filesAPIMaxFileBytes, filesAPIMaxInlineBytes)
	}
	if strings.TrimSpace(mimeType) == "" {
		return nil, errors.New("deepseek: files api upload refused: empty media type")
	}
	if strings.TrimSpace(filename) == "" {
		filename = filesAPIDefaultFilename
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	// purpose FIRST: the endpoint reads the intent before the payload and the
	// documented example sends it in this order.
	if err := mw.WriteField(filesAPIPurposeField, filesAPIPurposeUserData); err != nil {
		return nil, fmt.Errorf("deepseek: files api multipart purpose: %w", err)
	}
	// CreatePart (not CreateFormFile) because CreateFormFile hardcodes
	// application/octet-stream, and the endpoint judges the format from the
	// declared type. The header names are RFC 7578 form-data, not constants
	// this package owns.
	partHeader := textproto.MIMEHeader{}
	partHeader.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name=%q; filename=%q`, filesAPIFileField, filename))
	partHeader.Set("Content-Type", mimeType)
	part, err := mw.CreatePart(partHeader)
	if err != nil {
		return nil, fmt.Errorf("deepseek: files api multipart file part: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return nil, fmt.Errorf("deepseek: files api multipart body: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("deepseek: files api multipart close: %w", err)
	}

	reqBody := buf.Bytes()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.claudeFilesURL(), bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("deepseek: files api create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	// The upload endpoint requires the beta header too (measured: the upload
	// itself was sent with it and answered 200).
	httpReq.Header.Set(filesAPIBetaHeader, filesAPIBetaFilesAPI)
	httpReq.Header.Set(claudeVersionHeader, claudeAPIVersion)
	httpReq.Header.Set("Content-Type", mw.FormDataContentType())

	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("deepseek: files api upload %q: %w", filename, err)
	}
	defer func() {
		// The body is fully read below on every path that matters; closing it
		// cannot fail in a way the caller could act on.
		_ = resp.Body.Close() //stylegate:ignore closing a response body cannot fail usefully
	}()

	// The package's ONE probe cap, deliberately not a second one: the upload
	// answer is a ~200-byte JSON object, and a private 1 MiB cap here is exactly
	// the drift TestNon200ProbeCap_NoBareLimitReaderLiteral refuses (Rule 13 —
	// a cap that bypasses the shared constant is a bare literal wearing a name).
	body, err := io.ReadAll(io.LimitReader(resp.Body, non200BodyProbeMax))
	if err != nil {
		return nil, fmt.Errorf("deepseek: files api upload %q: read response: %w", filename, err)
	}
	if c.RawLog != nil {
		c.RawLog(rawlogfmt.TagProbeRequest, "", reqBody)
		c.RawLog(rawlogfmt.TagResponse, "", body)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &FilesAPIError{Status: resp.StatusCode, Body: string(body), Filename: filename}
	}

	var f ClaudeFile
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("deepseek: files api upload %q: response is not the documented JSON: %w (body: %s)",
			filename, err, truncateForError(string(body)))
	}
	if strings.TrimSpace(f.ID) == "" {
		return nil, fmt.Errorf("deepseek: files api upload %q: response carried no file id (body: %s)",
			filename, truncateForError(string(body)))
	}
	return &f, nil
}

// filesAPIDefaultFilename is the name the endpoint stores when the caller has
// none. The response-body cap is NOT private to this file: it is the package's
// non200BodyProbeMax, so the package keeps one cap rather than two.
const filesAPIDefaultFilename = "upload"

// FilesAPIError is a non-200 answer from the upload endpoint. It is its own
// type so a caller can distinguish "the endpoint refused the picture" (report
// the status + body verbatim) from "the request never left" (transport error),
// which are different repairs. It never wraps into a silent inline fallback.
type FilesAPIError struct {
	Status   int
	Body     string
	Filename string
}

// Error renders the refusal with the status and the endpoint's own words —
// Rule 15: a refusal must name its cause.
func (e *FilesAPIError) Error() string {
	return fmt.Sprintf("deepseek: files api upload %q refused with HTTP %d: %s",
		e.Filename, e.Status, truncateForError(e.Body))
}

// truncateForError bounds an endpoint body quoted inside an error message.
// A full 64 MiB picture echo has no business inside an HTTP error string.
func truncateForError(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= filesAPIErrorDisplayMax {
		return s
	}
	return s[:filesAPIErrorDisplayMax] + "…"
}

// filesAPIErrorDisplayMax bounds the body quoted in an error / FilesAPIError.
const filesAPIErrorDisplayMax = 512

// claudeFileSource builds the SOURCE of an image content block that references
// an uploaded file — {"type":"file","file_id":…}. The BLOCK type stays "image"
// (measured 422 for any other spelling; see this file's header).
func claudeFileSource(fileID string) ClaudeImageSource {
	return ClaudeImageSource{Type: claudeSourceTypeFile, FileID: fileID}
}

// HasFileBlocks reports whether a request references any uploaded file. It is
// the ONE predicate behind the beta header (Rule 14): every send path asks it,
// so a request cannot carry a file reference without the header that makes it
// legal, and a text-only request cannot carry a beta header it does not need.
func HasFileBlocks(req *ClaudeRequest) bool {
	if req == nil {
		return false
	}
	for i := range req.Messages {
		for j := range req.Messages[i].Content {
			src := req.Messages[i].Content[j].Source
			if src != nil && src.Type == claudeSourceTypeFile {
				return true
			}
		}
	}
	return false
}

// addBetaHeader appends one Anthropic beta feature to a request's beta header
// WITHOUT replacing what is already there. A header may carry several features
// comma-separated, and another feature installing its own value must keep it —
// overwriting would disable a capability the caller asked for.
func addBetaHeader(h http.Header, feature string) {
	feature = strings.TrimSpace(feature)
	if feature == "" {
		return
	}
	existing := h.Values(filesAPIBetaHeader)
	seen := make(map[string]bool, len(existing)+1)
	merged := make([]string, 0, len(existing)+1)
	for _, v := range existing {
		for _, piece := range strings.Split(v, betaHeaderValueSep) {
			piece = strings.TrimSpace(piece)
			if piece == "" || seen[piece] {
				continue
			}
			seen[piece] = true
			merged = append(merged, piece)
		}
	}
	if !seen[feature] {
		merged = append(merged, feature)
	}
	h.Set(filesAPIBetaHeader, strings.Join(merged, betaHeaderValueSep))
}
