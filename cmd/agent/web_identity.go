// web_identity.go — the engine IDENTITY surface, shared by every carrier of
// the web engine (`gopi web`, `gopi cli`, `gopi ui`).
//
// WHY IT EXISTS. All three carriers start the SAME engine, and the engine's
// token is re-minted per run — so "which session/model is this page actually
// talking to?" is not answerable from the URL alone. That question has been
// asked the hard way: a browser holding a cookie from an EARLIER run looked
// freshly loaded and 401'd on every send (see devdoc/web-page-token-fetch-fix.md
// and the memory record on the stale-cookie defect), and a remote operator has
// no `ps` to check. The identity route answers it over the wire the operators
// already have: one gated GET, carrying the session id (the id
// finalizeChatSession closes and `gopi session list` shows), the model actually
// serving turns (which is the VISION model while an image turn is live), the
// tools posture and the bind.
//
// IT IS NOT A NEW PUBLIC SURFACE. The route is additive and rides the same
// `webGatedHandler` as every other route, so a non-loopback bind demands the
// same per-run token and an unauthenticated request is refused exactly like
// /api/chat (the gate is the ONE decision point — untouched here). Nothing in
// it is secret: the operator already holds the token, and local trust already
// admits this host without one.
//
// THE TYPE IS THE WIRE. `gopi ui --server` consumes the very struct the route
// serializes (webFetchIdentity), so the notice the terminal prints and the JSON
// a remote curl reads cannot drift into two answers (Rule 13/14 — one
// definition, referenced by both sides).

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nick/gopi/internal/sessionevent"
)

// webIdentityPath is the read-only identity endpoint. Named (Rule 1b) because
// the route table, the page's fetch target and the tests must agree on ONE
// spelling.
const webIdentityPath = "/api/identity"

// webIdentityFetchTimeout bounds the `gopi ui --server` identity probe. It is
// a LOCAL HTTP round trip, so a peer that does not answer it promptly is a
// peer that is not serving this API — the caller reports that instead of
// blocking the terminal forever.
const webIdentityFetchTimeout = 5 * time.Second

// webIdentityBodyLimit bounds how much of an identity response is read (the
// payload is a short JSON object; a peer streaming something else must not be
// read into memory).
const webIdentityBodyLimit = 64 << 10

// webStartedFormat is the wire spelling of the server's start time: RFC3339 in
// UTC, so a remote client reads one unambiguous instant rather than a local
// rendering.
const webStartedFormat = time.RFC3339

// webIdentityPayload is BOTH the /api/identity response body and the struct
// `gopi ui` renders after fetching it. Field names are the wire contract — the
// page's identifyEngine() reads `model`/`session_id`.
type webIdentityPayload struct {
	// Addr is the bind this engine was configured with (the DISPLAY address a
	// client should use, not the listener's 0.0.0.0).
	Addr string `json:"addr"`
	// SessionID is the single persistent chat session this engine owns for the
	// WHOLE run (one session id + one JSONL + one raw log, closed once).
	SessionID string `json:"session_id,omitempty"`
	// Model is the model serving the NEXT turn: the vision model while an
	// image turn holds the shared agent, otherwise the run's model.
	Model string `json:"model,omitempty"`
	// VisionModel is the model an image turn switches to (resolved once at
	// startup), so a client can tell WHICH model a pasted image will use
	// without guessing.
	VisionModel string `json:"vision_model,omitempty"`
	// Tools is the tool-execution posture of this engine (`--tools` /
	// GOPI_WEB_TOOLS). A remote operator needs it: with tools on, anyone
	// holding the token drives an agent that runs commands here.
	Tools bool `json:"tools"`
	// Messages / Turns / Tokens are the current session's counters, so a client
	// can tell an EMPTY session from a live conversation it did not start —
	// the "am I looking at the same conversation?" check the single-session
	// design makes necessary (devdoc/web-chat-single-session.md).
	Messages int `json:"messages"`
	Turns    int `json:"turns"`
	Tokens   int `json:"tokens"`
	// Started is webStartedFormat: when this engine came up. A restarted engine
	// is visibly a NEW run (a new token, a new session), which is what makes
	// the stale-cookie class of failure self-evident instead of mysterious.
	Started string `json:"started,omitempty"`
}

// identityPayload snapshots this engine's identity. It reads the shared agent's
// model under s.mu (an image turn swaps it for the duration of the turn) and the
// transcript length under the same lock, so the answer is one consistent
// snapshot rather than a torn one.
func (s *webServer) identityPayload() webIdentityPayload {
	p := webIdentityPayload{Addr: s.addr, Tools: s.tools, VisionModel: s.visionModel}
	if !s.startedAt.IsZero() {
		p.Started = s.startedAt.UTC().Format(webStartedFormat)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p.Messages = len(s.history)
	p.Turns = s.iterCount
	p.Tokens = s.totalTokens
	if s.agent != nil {
		p.Model = s.agent.Model
		if s.agent.SessionLog != nil {
			p.SessionID = s.agent.SessionLog.SessionID()
		}
	}
	return p
}

// handleIdentity answers GET webIdentityPath. It is mounted inside
// webChatHandler, so it is covered by the access gate exactly like every other
// route — this handler adds no exception and checks no credential of its own.
func (s *webServer) handleIdentity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed (GET "+webIdentityPath+")", http.StatusMethodNotAllowed)
		return
	}
	payload, err := json.Marshal(s.identityPayload())
	if err != nil {
		// Recorded, never swallowed (Rule 15): an identity that cannot be
		// marshalled is a broken server, and the client must not read a
		// truncated body as a real answer.
		fmt.Fprintf(os.Stderr, "gopi web: %s: %v\n", webIdentityPath, err)
		http.Error(w, "identity encoding failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(payload) //stylegate:ignore a failed write to an identity JSON body is the client's disconnect, not a server fault
}

// webFetchIdentity reads the identity of an ALREADY-RUNNING engine over the
// wire (`gopi ui --server BASE`). The token is carried as a Bearer credential
// from the same variable the TUI's own client uses, so the probe authenticates
// by the route scripts use and never by a page cookie.
//
// A failure is returned (Rule 15): the caller decides whether an unidentified
// engine is fatal for what it is doing, rather than printing an identity it did
// not verify. The deadline is webIdentityFetchTimeout.
func webFetchIdentity(base, token string) (webIdentityPayload, error) {
	ctx, cancel := context.WithTimeout(context.Background(), webIdentityFetchTimeout)
	defer cancel()
	url := strings.TrimRight(base, "/") + webIdentityPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return webIdentityPayload{}, fmt.Errorf("build identity request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return webIdentityPayload{}, fmt.Errorf("identity request to %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck // the body is read below; a close error adds nothing
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, webIdentityBodyLimit)) //stylegate:ignore reads a bounded error body for a diagnostic message; a short read only shortens it//nolint:errcheck // best-effort body quote; the status is the load-bearing fact
		return webIdentityPayload{}, fmt.Errorf("identity request to %s: HTTP %d — %s", url, resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	var p webIdentityPayload
	if err := json.NewDecoder(io.LimitReader(resp.Body, webIdentityBodyLimit)).Decode(&p); err != nil {
		return webIdentityPayload{}, fmt.Errorf("decode identity from %s: %w", url, err)
	}
	return p, nil
}

// webRouteURL builds the URL for a ROUTE on an origin, carrying the per-run
// access token as a query parameter AFTER the route path —
// `http://host:port/api/identity?t=<token>`.
//
// The ORDER is the whole point: the gate reads the token from the URL's query
// string, so a token appended after the path authenticates the path. Putting the
// path after a query (`…?t=<token>/api/identity`) makes the token's VALUE
// `<token>/api/identity`, the constant-time compare fails, and the operator's
// own banner hands them a 401 — measured on a live LAN bind, which is why this
// is ONE helper instead of string concatenation at each call site.
//
// A loopback origin under local trust carries no token (that URL can only be
// opened here, where the gate serves it anyway) — the same rule webTokenURL
// applies to the banner's lines.
func webRouteURL(origin, route, token string, localTrust bool) string {
	u := strings.TrimRight(origin, "/") + route
	if token == "" {
		return u
	}
	if localTrust && webURLIsLoopback(u) {
		return u
	}
	return u + "?" + webTokenPair(token)
}

// webIdentityNoticeFormat is the ONE identity line every carrier prints. It is
// a const so the terminal carriers and their tests agree on the spelling, and
// the field order is fixed: session, model, tools, addr.
const webIdentityNoticeFormat = "session=%s model=%s tools=%v addr=%s"

// webIdentityNotice renders a payload as that line, naming the pieces a local
// operator checks when a browser misbehaves (session id ↔ model ↔ tools ↔
// bind) and substituting an explicit unknown for anything the engine could not
// report — never an empty string that reads like a normal value (Rule 15).
func webIdentityNotice(p webIdentityPayload) string {
	session := p.SessionID
	if session == "" {
		session = webIdentityUnknown
	}
	model := p.Model
	if model == "" {
		model = webIdentityUnknown
	}
	addr := p.Addr
	if addr == "" {
		addr = webIdentityUnknown
	}
	return fmt.Sprintf(webIdentityNoticeFormat, session, model, p.Tools, addr)
}

// webIdentityUnknown is the stand-in for a field the engine could not report.
// It is a value, not an absence: "unknown" is information, "" is not.
const webIdentityUnknown = "unknown"

// webPrintIdentity writes the identity block to stderr — the carriers call it
// while they still own the terminal, so its lines precede any rendering. It
// prints the notice plus the counters that make an EMPTY session
// distinguishable from a live one, and (when the caller can name one) the
// identity route's URL so a remote operator can re-check the same facts later.
//
// identityURL is passed COMPLETE (origin + path, token-carrying when the bind
// requires one): this function never assembles a URL, so it cannot print one
// that 401s.
func webPrintIdentity(prefix string, identityURL string, p webIdentityPayload) {
	fmt.Fprintf(os.Stderr, "%s: engine %s\n", prefix, webIdentityNotice(p))
	// The counter is the web engine's turn count (s.iterCount): how many turns
	// this server has run, which IS the turn count under the operator's ruling
	// (2026-10-27) — an input is an input whatever face delivered it. It prints
	// under the bare word (sessionevent.CounterUnitTurn); no qualifier is
	// invented for it, because there is no rival quantity to be told apart from.
	if identityURL == "" {
		fmt.Fprintf(os.Stderr, "%s: messages=%d %s=%d tokens=%d started=%s\n", prefix,
			p.Messages, sessionevent.CounterUnitTurn, p.Turns, p.Tokens, webStartedOrUnknown(p))
		return
	}
	fmt.Fprintf(os.Stderr, "%s: identity %s (messages=%d %s=%d tokens=%d started=%s)\n", prefix,
		identityURL, p.Messages, sessionevent.CounterUnitTurn, p.Turns, p.Tokens, webStartedOrUnknown(p))
}

// webStartedOrUnknown renders the started instant, or the explicit unknown.
func webStartedOrUnknown(p webIdentityPayload) string {
	if p.Started == "" {
		return webIdentityUnknown
	}
	return p.Started
}
