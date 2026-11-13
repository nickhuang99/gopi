// HTTP client helpers for `gopi tool tokens` — kept small and dependency-free.
package main

import (
	"bytes"
	"net/http"
	"time"
)

// countTimeout bounds one count_tokens round trip. The endpoint answers in well
// under a second when reachable; this exists so an unreachable host fails the
// REPORT rather than hanging the tool.
const countTimeout = 60 * time.Second

// httpRequest builds the count_tokens POST for one body.
func httpRequest(url string, body []byte, key string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	if key != "" {
		req.Header.Set("x-api-key", key)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	return req, nil
}

// httpDo performs the request under the fixed timeout.
func httpDo(req *http.Request) (*http.Response, error) {
	c := &http.Client{Timeout: countTimeout}
	return c.Do(req)
}
