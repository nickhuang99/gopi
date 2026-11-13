package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// UpstreamConfig describes the OpenAI-compatible upstream to forward to.
type UpstreamConfig struct {
	BaseURL string // e.g. https://open.bigmodel.cn/api/paas/v4
	Model   string // e.g. glm-5.3-flash
	Key     string // API key (sent as Authorization: Bearer)
}

// Server is the gopi tool proxy HTTP server.
type Server struct {
	Upstream UpstreamConfig
	Verbose  bool
	Logger   *log.Logger

	// upstreamPath is the OpenAI endpoint path appended to Upstream.BaseURL.
	upstreamPath string
}

// NewServer builds a Server with the given upstream config.
func NewServer(up UpstreamConfig, verbose bool, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	base := strings.TrimRight(up.BaseURL, "/")
	if strings.HasSuffix(base, "/chat/completions") {
		base = strings.TrimSuffix(base, "/chat/completions")
	}
	return &Server{
		Upstream:     up,
		Verbose:      verbose,
		Logger:       logger,
		upstreamPath: base + "/chat/completions",
	}
}

// Handler returns the http.Handler for the proxy. It accepts the Anthropic
// Messages endpoint at POST /v1/messages (standard Anthropic) and POST
// /anthropic/messages (gopi's own DeepSeek-style client path) so both standard
// clients and gopi itself can point base_url at the proxy.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", s.handleMessages)
	mux.HandleFunc("/anthropic/messages", s.handleMessages)
	// Health & default.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	return mux
}

// handleMessages is the Anthropic POST /v1/messages handler.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	start := time.Now()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeAnthropicError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var ar AnthropicRequest
	if err := json.Unmarshal(body, &ar); err != nil {
		s.writeAnthropicError(w, http.StatusBadRequest, "invalid Anthropic request: "+err.Error())
		return
	}

	// Translate to OpenAI.
	openReq := convertRequest(&ar, s.Upstream.Model)
	openBody, err := json.Marshal(openReq)
	if err != nil {
		s.writeAnthropicError(w, http.StatusInternalServerError, "failed to build upstream request")
		return
	}

	if s.Verbose {
		s.Logger.Printf("[proxy] → %s %s (stream=%v) model=%s tokens=%d",
			r.Method, r.URL.Path, ar.Stream, openReq.Model, openReq.MaxTokens)
	}

	// Forward to the upstream.
	req, err := http.NewRequest(http.MethodPost, s.upstreamPath, bytes.NewReader(openBody))
	if err != nil {
		s.writeAnthropicError(w, http.StatusInternalServerError, "failed to create upstream request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.Upstream.Key)
	if ar.Stream {
		req.Header.Set("Accept", "text/event-stream")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.writeAnthropicError(w, http.StatusBadGateway, "upstream request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	// Upstream error -> pass through as Anthropic error with status passthrough.
	if resp.StatusCode >= 300 {
		upBody, _ := io.ReadAll(resp.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		msg := extractUpstreamError(upBody)
		s.writeAnthropicErrorWithStatus(w, resp.StatusCode, msg)
		return
	}

	if ar.Stream {
		s.streamResponse(w, r, resp.Body, ar)
	} else {
		s.nonStreamResponse(w, resp.Body, ar)
	}

	if s.Verbose {
		s.Logger.Printf("[proxy] ← %s done in %s", r.URL.Path, time.Since(start).Round(time.Millisecond))
	}
}

// streamResponse relays the upstream SSE stream, translating each OpenAI chunk
// into Anthropic events. message_start is emitted lazily by the translator on
// its first chunk (which carries the real upstream id/model).
func (s *Server) streamResponse(w http.ResponseWriter, r *http.Request, up io.Reader, ar AnthropicRequest) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	fl, ok := w.(http.Flusher)
	if !ok {
		return
	}

	acc := newStreamTranslator(ar.Model, ar.Model)

	sc := bufio.NewScanner(up)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(strings.TrimSpace(line), "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk OpenAIChatChunk
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue // tolerate malformed frames; skip.
		}
		if err := acc.Feed(w, chunk); err != nil {
			return
		}
		fl.Flush()
	}
}

// nonStreamResponse relays a non-streaming upstream response as a full
// Anthropic JSON message.
func (s *Server) nonStreamResponse(w http.ResponseWriter, resp io.Reader, ar AnthropicRequest) {
	body, err := io.ReadAll(resp)
	if err != nil {
		s.writeAnthropicError(w, http.StatusBadGateway, "failed to read upstream response")
		return
	}
	var or OpenAIChatResponse
	if err := json.Unmarshal(body, &or); err != nil {
		s.writeAnthropicError(w, http.StatusBadGateway, "invalid upstream response: "+err.Error())
		return
	}
	msg := ConvertNonStreamResponse(or.ID, or.Model, &or, "")
	msg.Model = arOrModel(or.Model, ar.Model)
	respBody, _ := json.Marshal(msg) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBody)
}

func arOrModel(up, an string) string {
	if up != "" {
		return up
	}
	return an
}

// writeAnthropicError writes the Anthropic error envelope with a default 500.
func (s *Server) writeAnthropicError(w http.ResponseWriter, status int, msg string) {
	s.writeAnthropicErrorWithStatus(w, status, msg)
}

// writeAnthropicErrorWithStatus writes the Anthropic error envelope: the spec
// strips `data:` and uses a JSON body {"type":"error","error":{...}}. The
// transit-openai proxy serves it as a plain JSON error response with the HTTP
// status preserved (both stream and non-stream forms are covered by gopi's
// client).
func (s *Server) writeAnthropicErrorWithStatus(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "error",
		"error": map[string]string{
			"type":    "api_error",
			"message": msg,
		},
	}) //stylegate:ignore best-effort JSON error body on an already-failed request; the status code carries the outcome
}

// extractUpstreamError pulls a human-readable message out of an OpenAI error body.
func extractUpstreamError(body []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	s := string(body)
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return "upstream error: " + s
}
