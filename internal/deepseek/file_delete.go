package deepseek

// file_delete.go — the RECLAMATION half of the Files API client: delete ONE
// uploaded object so its account slot goes back.
//
// WHY IT EXISTS (measured, not assumed). UploadFile's own contract (and this
// package's header) says the account is capped at 25 GiB / 10 000 files and
// that nothing in the carrier ever deleted an object, so a picture was bought
// once and held forever. The web carrier's file_id cache is a BOUNDED table
// (webFilesCacheMax entries) written to the profile, so once it is full a new
// picture can only be admitted by evicting an old one — and evicting an entry
// while leaving the remote object behind converts "the account grows once per
// picture" into "the account grows once per picture AND the cache forgets",
// i.e. it makes the leak worse, not smaller: the same bytes are re-uploaded on
// a later turn and BOTH objects stay.
//
// So reclamation is not a nicety, it is the precondition that makes eviction
// safe. This file is the one call that performs it.
//
// WHAT IT DELIBERATELY DOES NOT DO:
//
//   - It does not retry. A failed delete costs one account slot and one logged
//     line; a retry storm against a refusing endpoint costs the turn.
//   - It does not touch the cache. The caller decides what an eviction means;
//     this function knows only about one file id.
//   - IT WRITES NO RAW-LOG RECORD, and that is a decision, not an omission.
//     The raw log's five tags each carry a direction; `P` (TagProbeRequest) is
//     what an UPLOAD writes and is therefore the count a reader uses to answer
//     "how many pictures did this run upload" (`grep -cE '^P [0-9]+$'`).
//     Borrowing `P` for a DELETE would corrupt that count, and every other tag
//     would claim a response/chunk/error that never happened. The reclamation
//     is visible through the carrier's own channels instead: the eviction line
//     on stderr (which names the evicted file id and the reason) and the
//     cache's counters — plus the DELETE itself, which the account and any mock
//     endpoint see on the wire.
//
// NO FALLBACK: a delete that fails returns an error naming the status and the
// endpoint's own words. The CALLER swallows it (a picture that could not be
// reclaimed must never fail the operator's turn); this function does not.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Named constants — Project Rule 1.
const (
	// filesAPIDeleteAccepted / filesAPIDeleteNoContent are the two statuses the
	// endpoint may answer a delete with. Anything else is a refusal.
	filesAPIDeleteAccepted  = http.StatusOK
	filesAPIDeleteNoContent = http.StatusNoContent

	// filesDeletePathSep joins the Files API collection path to one object's
	// id. Named so the URL builder below reads as one rule rather than an
	// inline byte.
	filesDeletePathSep = "/"
)

// FilesDeleteError is a non-2xx answer from the delete endpoint. It is its own
// type (not FilesAPIError) because the two reports answer different questions:
// FilesAPIError says "the endpoint refused this picture", this one says "the
// endpoint refused to free this slot" — and the carrier reports the second one
// as a warning that does not fail the turn, which is only possible if the
// caller can tell them apart.
type FilesDeleteError struct {
	Status int
	Body   string
	FileID string
}

// Error renders the refusal with the status and the endpoint's own words —
// Rule 15: a refusal must name its cause.
func (e *FilesDeleteError) Error() string {
	return fmt.Sprintf("deepseek: files api delete %q refused with HTTP %d: %s",
		e.FileID, e.Status, truncateForError(e.Body))
}

// claudeFileURL is the single-object URL for one uploaded file. The id is
// PATH-ESCAPED: it arrives from an endpoint response (or from the profile's
// table), and an id carrying a "/" or a "?" must not be able to address a
// different endpoint. claudeFilesURL already resolved the base's suffix rule,
// so this builder only appends.
func (c *Client) claudeFileURL(id string) string {
	return c.claudeFilesURL() + filesDeletePathSep + url.PathEscape(id)
}

// DeleteFile asks the endpoint to delete ONE uploaded file, freeing its account
// slot. A nil return means the endpoint acknowledged the deletion (200 or 204).
//
// Every other outcome is an error the caller reports and then continues from:
// the object stays, the cache entry is already gone, and the only cost is one
// slot the operator can free at the console. Nothing here fails a turn — that
// is the CALLER's decision, and the web carrier's decision is "never".
func (c *Client) DeleteFile(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("deepseek: files api delete refused: empty file id")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.claudeFileURL(id), nil)
	if err != nil {
		return fmt.Errorf("deepseek: files api delete %q: build request: %w", id, err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	// The delete endpoint sits behind the same beta gate the upload and the
	// file REFERENCE do (one header installs the Files API for the whole
	// round trip).
	httpReq.Header.Set(filesAPIBetaHeader, filesAPIBetaFilesAPI)
	httpReq.Header.Set(claudeVersionHeader, claudeAPIVersion)

	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(httpReq)
	if err != nil {
		return fmt.Errorf("deepseek: files api delete %q: %w", id, err)
	}
	defer func() {
		// The body is drained below; closing it cannot fail usefully.
		_ = resp.Body.Close() //stylegate:ignore closing a response body cannot fail usefully
	}()

	// The package's ONE probe cap, never a second private one (Rule 13 — a
	// cap that bypasses the shared constant is a bare literal wearing a name).
	body, err := io.ReadAll(io.LimitReader(resp.Body, non200BodyProbeMax))
	if err != nil {
		return fmt.Errorf("deepseek: files api delete %q: read response: %w", id, err)
	}
	if resp.StatusCode != filesAPIDeleteAccepted && resp.StatusCode != filesAPIDeleteNoContent {
		return &FilesDeleteError{Status: resp.StatusCode, Body: string(body), FileID: id}
	}
	return nil
}
