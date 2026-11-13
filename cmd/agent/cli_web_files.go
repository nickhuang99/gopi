package main

// cli_web_files.go — the web carrier's half of the Files API switchover: a turn
// that carries pictures uploads each one ONCE to DeepSeek's Files API and sends
// the model a file REFERENCE instead of the base64 bytes.
//
// WHY (measured, not assumed): an inline picture grows the request body by 4/3
// of its size, travels again on every following turn, and is hard-capped at
// 32 MiB per picture. A file reference is one short string, is resolved
// server-side from a stored upload, and the endpoint accepts 64 MiB per file.
//
// WHAT DOES NOT CHANGE: the page, the 📷 endpoint, the 📎 picker and the wire
// from browser to /api/chat. The browser still ships {media_type, data_b64};
// the SUBSTITUTION happens here, at the one place where a browser image becomes
// a model message. That is deliberate — one substitution point, so the inline
// and file transports cannot drift into two different validators.
//
// NO FALLBACK (Project Rule 15). An upload that fails returns an error the
// caller shows verbatim; the turn does NOT quietly become an inline turn. A
// silent downgrade would make "we use the Files API" false exactly when the
// picture is biggest and the fallback most destructive (the inline path is what
// the 32 MiB cap and the request-body cap exist to refuse).

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/nick/gopi/internal/deepseek"
)

// Named constants — Project Rule 1.
const (
	// webFilesUploadFailureStatus answers a turn whose picture could not be
	// uploaded. 502 (not 500) because the failure is the UPSTREAM's, and not
	// 4xx because the browser's request was valid: resending it unchanged
	// cannot help, which is what a 4xx would claim.
	webFilesUploadFailureStatus = http.StatusBadGateway

	// webFilesCacheMax bounds the file_id cache. Each entry is a short id, so
	// the memory cost is trivial; the bound exists so a long-lived `gopi web`
	// process cannot grow one entry per picture forever (the Files API account
	// ceiling is 25 GiB / 10 000 files).
	//
	// THE BOUND IS NOT A LICENCE TO DROP PICTURES ON THE FLOOR. A full table
	// EVICTS its least recently used entry and reclaims that object upstream
	// (DeleteFile), because the old behaviour — silently refusing to cache a
	// new picture — left the carrier re-uploading the same bytes on every turn
	// while the account only ever grew (measured: the same new picture sent
	// three times after the table filled up produced three uploads, while an
	// OLD picture's re-send produced none).
	webFilesCacheMax = 256

	// webFilesCacheEvictReason is the reported cause of an eviction. It is one
	// constant because it appears in three channels that must agree (the
	// counter, the stderr line, and the summary a turn prints): the table was
	// at its bound and a newer picture had to be admitted.
	webFilesCacheEvictReason = "least recently used, table at its bound"

	// webFilesFilename / webFilesUploadErrPrefix name the upload and its
	// refusal. The filename is what the Files API lists the object as; the
	// prefix makes the failure greppable in a session log.
	webFilesFilename        = "web-turn.png"
	webFilesUploadErrPrefix = "image upload to the Files API failed"

	// webFilesQuotaRemedyConsole is where an operator frees account headroom.
	// Spelled once (Rule 1) because the refusal text and any future surface
	// must send the reader to the same place.
	webFilesQuotaRemedyConsole = "https://platform.deepseek.com/"
)

// webFilesQuotaEvidence are the lowercase fragments an endpoint's refusal
// carries when the cause is the ACCOUNT'S CAPACITY rather than the picture.
// They are matched against the status and the body together, so a 502 that
// merely passes through a gateway is NOT mistaken for a full account — the
// refusal says "the endpoint refused", and the capacity wording is used only
// when the body itself names a capacity word (Rule 41: an inference is never
// dressed as a measurement).
var webFilesQuotaEvidence = []string{
	"quota", "capacity", "storage", "no space", "file limit", "file_limit",
	"too many files", "exceed", "insufficient", "account limit", "账户", "容量", "配额",
}

// webFilesCache remembers which picture (by content) already has a file_id, so
// the SAME image is uploaded once and referenced on every later turn — the
// whole point of the Files API for a conversation that keeps a screenshot in
// its history.
//
// Keyed by CONTENT, not by name or by turn: a re-captured identical frame and a
// re-attached identical file are the same object upstream, and uploading them
// twice would burn two of the account's 10 000 slots for one picture.
type webFilesCache struct {
	mu  sync.Mutex
	ids map[string]string

	// used is each entry's logical recency tick: the value of clock when it was
	// last admitted or last HIT. It is what an eviction reads (the smallest
	// tick goes first) and it is persisted, so the ordering survives a restart
	// (cli_web_files_cache.go).
	used map[string]int64

	// clock is the monotonic tick every `used` value is relative to. It is
	// loaded from the table and written back with it, never reset to zero on
	// start — a reset would make every entry a restart inherited look STALER
	// than every entry the new process admits.
	clock int64

	// counters make eviction and reclamation OBSERVABLE (Rule 15). A bounded
	// table that silently drops entries is exactly the defect this field
	// exists to make impossible to reintroduce: "nothing happened" and
	// "everything was evicted" must not look alike.
	evicted      int64
	reclaimed    int64
	reclaimFails int64

	// loaded is set once the on-disk table has been read (or its absence /
	// a refusal has been reported). It is what makes the FIRST lookup — not
	// the constructor — pay for the disk read, so every construction path
	// (including a test's bare struct literal) gets cross-process reuse
	// without being edited.
	loaded bool
}

// webFilesCacheStats is a consistent snapshot of the cache's observability
// counters. It exists so a reporter (a turn's log line, a test) reads all
// fields under ONE lock instead of racing a live server.
type webFilesCacheStats struct {
	entries      int
	evicted      int64
	reclaimed    int64
	reclaimFails int64
}

// stats returns a consistent copy of the counters and the current size.
func (c *webFilesCache) stats() webFilesCacheStats {
	if c == nil {
		return webFilesCacheStats{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadOnce()
	return webFilesCacheStats{
		entries:      len(c.ids),
		evicted:      c.evicted,
		reclaimed:    c.reclaimed,
		reclaimFails: c.reclaimFails,
	}
}

// get returns a cached file_id for a content key, reading the persistent table
// on the first lookup so a RESTARTED process reuses the id the previous one
// uploaded (cli_web_files_cache.go).
//
// A HIT IS AN ACCESS: it advances the entry's recency tick and writes the table
// back. That is what "least recently used" means, and it costs one ~30 KB
// rewrite per turn that re-references an old picture — the same order of work
// the admission path already did (measured: saveToDisk's own note, and the
// table's bound is 256 short ids).
func (c *webFilesCache) get(key string) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadOnce()
	id, ok := c.ids[key]
	if !ok {
		return "", false
	}
	c.clock++
	c.used[key] = c.clock
	c.saveToDisk()
	return id, true
}

// loadOnce reads the persistent table at most once per process. The caller
// holds the lock, so two concurrent first turns cannot both load (and cannot
// interleave a half-built map).
func (c *webFilesCache) loadOnce() {
	if c.loaded {
		return
	}
	c.loaded = true
	if c.ids == nil {
		c.ids = make(map[string]string)
	}
	if c.used == nil {
		c.used = make(map[string]int64)
	}
	c.loadFromDisk()
}

// webFilesCacheEviction is one entry pushed out of the table, plus the reason.
// It is returned to the CALLER rather than acted on here: the cache knows which
// object to reclaim but not how (the reclamation rides the API client, which
// the caller holds), and a cache that called out to the network while holding
// its own lock would serialise every turn behind one DELETE.
type webFilesCacheEviction struct {
	key    string
	fileID string
	reason string
}

// put records a file_id, evicting the LEAST RECENTLY USED entry when the table
// is at its bound and returning what it evicted so the caller can reclaim the
// remote object.
//
// WHY LRU AND NOT "refuse to grow" (the measured defect this replaces): the old
// body was `if len(c.ids) >= webFilesCacheMax { return }`, with a comment
// claiming eviction would "leak the old ones". The measurement says the
// opposite — once the table filled up, EVERY new picture was re-uploaded on
// every turn that carried it (the same new picture sent three times produced
// three uploads), while an old picture's re-send produced none. Refusing to
// cache is not a smaller leak than eviction; it is the same leak, paid again on
// every turn, forever.
//
// WHY LRU AND NOT FIFO: the carrier's own history makes the difference real.
// `s.history` keeps a picture's file reference for the whole conversation, so a
// picture the operator is still talking about gets RE-SENT, i.e. LOOKED UP, and
// `get` advances exactly that signal. FIFO would evict it anyway — while a
// picture that was admitted early and never used again (the common case: an
// old screenshot) would keep its slot. The cost of being wrong is one upload
// either way, so the policy is chosen for the case it gets RIGHT, not for the
// cheapest proxy.
func (c *webFilesCache) put(key, id string) []webFilesCacheEviction {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadOnce()
	var evicted []webFilesCacheEviction
	// A key that is already present is a RE-OBSERVATION, not a new entry: it
	// refreshes recency and cannot evict anything. Without this guard a
	// repeated upload (which re-buying the same bytes is) would push out a
	// live entry to make room for one that is already in the table.
	if _, exists := c.ids[key]; !exists {
		for len(c.ids) >= webFilesCacheMax {
			oldKey, oldID, ok := c.oldestLocked()
			if !ok {
				// Unreachable while webFilesCacheMax > 0: a non-empty map always
				// has an oldest entry. Reported rather than looped on, because a
				// silent break here would reinstate the very defect this
				// function exists to remove.
				webFilesCacheWarnf("the table is at its bound of %d and no eviction candidate was found — NOT caching this picture (it will be re-uploaded)",
					webFilesCacheMax)
				break
			}
			delete(c.ids, oldKey)
			delete(c.used, oldKey)
			c.evicted++
			evicted = append(evicted, webFilesCacheEviction{key: oldKey, fileID: oldID, reason: webFilesCacheEvictReason})
			// THE EVICTION IS REPORTED WITH ITS OBJECT AND ITS CAUSE (Rule 15):
			// the line names the file id that left the table and why, so a
			// later re-upload can be traced back to this moment instead of
			// looking like a cache that never worked.
			webFilesCacheWarnf("evicted %s (file_id %s): %s; reclaiming the remote object", oldKey, oldID, webFilesCacheEvictReason)
		}
		c.clock++
		c.ids[key] = id
		c.used[key] = c.clock
	} else {
		c.clock++
		c.used[key] = c.clock
	}
	c.saveToDisk()
	return evicted
}

// oldestLocked returns the entry a full table must give up: the smallest
// recency tick, with the key as a deterministic tie-break so two processes
// holding the same table evict the same entry. The caller holds the lock.
func (c *webFilesCache) oldestLocked() (key, id string, ok bool) {
	var oldestTick int64
	first := true
	for k, tick := range c.used {
		if _, live := c.ids[k]; !live {
			continue
		}
		if first || tick < oldestTick || (tick == oldestTick && k < key) {
			key, oldestTick, first = k, tick, false
		}
	}
	if key == "" {
		return "", "", false
	}
	return key, c.ids[key], true
}

// noteReclaimed records the outcome of one remote reclamation so the counters
// tell the truth about the account, not just about the table: an eviction that
// could NOT be reclaimed is a slot that will never come back, and it is counted
// separately from one that succeeded.
func (c *webFilesCache) noteReclaimed(ok bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if ok {
		c.reclaimed++
		return
	}
	c.reclaimFails++
}

// webImageCacheKey is the identity of one picture: its media type plus the
// SHA-256 of its DECODED bytes. The digest is taken on the bytes, never on the
// base64 text, so the same picture attached twice with different padding is one
// key.
func webImageCacheKey(mediaType string, data []byte) string {
	sum := sha256.Sum256(data)
	return strings.ToLower(strings.TrimSpace(mediaType)) + ":" + hex.EncodeToString(sum[:])
}

// stageImagesAsFiles turns the turn's validated inline images into file
// references, uploading each picture that has no cached file_id yet.
//
// It is the ONLY place the substitution happens, and it is deliberately total:
// every image in, a reference out. A failure on ANY picture fails the turn with
// an error naming the picture and the endpoint's own refusal — there is no path
// that returns half file references and half inline bytes, and none that
// returns inline bytes on failure.
func (s *webServer) stageImagesAsFiles(ctx context.Context, srcs []*deepseek.ClaudeImageSource) ([]*deepseek.ClaudeImageSource, error) {
	if len(srcs) == 0 {
		return nil, nil
	}
	client := s.filesClient()
	if client == nil {
		return nil, fmt.Errorf("%s: no API client is configured for this server", webFilesUploadErrPrefix)
	}
	out := make([]*deepseek.ClaudeImageSource, 0, len(srcs))
	for i, src := range srcs {
		if src == nil {
			return nil, fmt.Errorf("%s: image %d is empty", webFilesUploadErrPrefix, i+1)
		}
		data, err := base64.StdEncoding.DecodeString(src.Data)
		if err != nil {
			return nil, fmt.Errorf("%s: image %d: decode: %w", webFilesUploadErrPrefix, i+1, err)
		}
		key := webImageCacheKey(src.MediaType, data)
		if id, ok := s.files.get(key); ok {
			out = append(out, &deepseek.ClaudeImageSource{Type: deepseek.ClaudeSourceTypeFileAPI, FileID: id})
			continue
		}
		file, err := client.UploadFile(ctx, webFilesFilename, src.MediaType, data)
		if err != nil {
			return nil, fmt.Errorf("%s: image %d: %s", webFilesUploadErrPrefix, i+1, webFilesUploadRefusal(err))
		}
		// The table is bounded, so admitting this picture may push another one
		// out. RECLAIMING THE EVICTED OBJECT IS THE OTHER HALF OF THE ADMISSION
		// and it is the reason eviction is safe at all: an entry that leaves
		// the table while its object stays upstream turns "the account grows
		// once per picture" into "the account grows once per picture AND the
		// bytes come back later", which is strictly worse than never caching.
		//
		// A FAILED DELETE NEVER FAILS THE TURN (the brief's contract, and the
		// right one: the picture in hand uploaded fine, and the image the
		// operator is sending is not the one that failed). It is REPORTED and
		// COUNTED, so a slot that will never come back is visible instead of
		// silent (Rule 15).
		for _, ev := range s.files.put(key, file.ID) {
			s.reclaimEvictedFile(ctx, client, ev)
		}
		out = append(out, &deepseek.ClaudeImageSource{Type: deepseek.ClaudeSourceTypeFileAPI, FileID: file.ID})
	}
	return out, nil
}

// reclaimEvictedFile deletes ONE object that an eviction pushed out of the table
// and records the outcome. It is the carrier's whole reclamation leg, and it is
// deliberately unable to fail anything: every branch ends in a counter and a
// reported line.
//
// The context is the TURN's: a delete that outlives the request it was decided
// in has no one left to report to, and the object it targets is already
// unreferenced by the table either way.
func (s *webServer) reclaimEvictedFile(ctx context.Context, client *deepseek.Client, ev webFilesCacheEviction) {
	if client == nil {
		s.files.noteReclaimed(false)
		webFilesCacheWarnf("could not free OLD file_id %s (%s): no API client is configured. Your picture was stored and this turn is fine — only this older object stays on the account until you delete it", ev.fileID, ev.reason)
		return
	}
	if err := client.DeleteFile(ctx, ev.fileID); err != nil {
		s.files.noteReclaimed(false)
		webFilesCacheWarnf("could not free OLD file_id %s (%s): %v. Your picture was stored and this turn is fine — only this older object stays on the account; free it at %s",
			ev.fileID, ev.reason, err, webFilesQuotaRemedyConsole)
		return
	}
	s.files.noteReclaimed(true)
	webFilesCacheWarnf("reclaimed file_id %s (%s): the endpoint deleted the object", ev.fileID, ev.reason)
}

// filesClient is the API client the upload rides. It is the SAME client the
// turns themselves use (one credential, one base URL, one HTTP pool) so a
// deployment has nothing extra to configure — and a test that replaces the
// agent's client replaces the uploader with it.
func (s *webServer) filesClient() *deepseek.Client {
	if s.agent == nil {
		return nil
	}
	return s.agent.Client
}

// webFilesUploadRefusal renders the ACTIONABLE half of a failed upload: what
// the operator can do about it, in front of the endpoint's own words.
//
// It exists because the raw endpoint answer is unactionable by construction: a
// full account answers with a status and a body that name no remedy, and the
// carrier answered the browser with that text verbatim. The remedy that matters
// is the one gopi can never perform for the operator — NOTHING in this carrier
// deletes an uploaded object, so the account only grows and only the operator
// can free it.
//
// Three branches, and none of them guesses (Rule 41: what was measured is
// stated as measured, what was inferred is labelled):
//
//   - the endpoint refused and its own body names a CAPACITY cause → say the
//     storage is full, name the console and the deletion, and name the fact
//     gopi never reclaims;
//   - the endpoint refused for any other reason → quote its refusal and point
//     at the console too, because a per-account file limit is the other
//     capacity refusal and it is the same repair;
//   - the request never left (transport error, no API answer at all) → say so;
//     the console has nothing to do with it.
func webFilesUploadRefusal(err error) string {
	var apiErr *deepseek.FilesAPIError
	if !errors.As(err, &apiErr) {
		return fmt.Sprintf("%s (the request did not reach the endpoint, so this is a network/credential problem rather than a full account)", err.Error())
	}
	if webFilesQuotaRefusal(apiErr.Body) {
		return fmt.Sprintf("%s — THE ACCOUNT'S FILE STORAGE IS FULL OR THE FILE COUNT LIMIT WAS HIT. "+
			"Nothing in this carrier deletes an uploaded file, so the account only grows; free headroom by deleting "+
			"files you no longer need at %s (DeepSeek platform console → Files / 文件), then send the picture again. "+
			"Deleting %s in your gopi profile only forgets which ids this machine knows — it does not free the account.",
			apiErr.Error(), webFilesQuotaRemedyConsole, webFilesCacheFileName)
	}
	return fmt.Sprintf("%s — the endpoint refused this picture rather than the account being full; "+
		"if the same picture keeps failing, check the account's remaining storage at %s (DeepSeek platform console → Files / 文件), "+
		"where old uploads can also be deleted", apiErr.Error(), webFilesQuotaRemedyConsole)
}

// webFilesQuotaRefusal reports whether the endpoint's own body names a CAPACITY
// cause. It is a lowercased substring probe over the evidence list, applied to
// the BODY only — never to a status code, because a 502 from a gateway says
// nothing about the account (the refusal above already reports an
// unrecognised cause honestly).
func webFilesQuotaRefusal(body string) bool {
	lower := strings.ToLower(body)
	for _, needle := range webFilesQuotaEvidence {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
