package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

// Web search tool (race #6): routes to the gopi websearch plugin scripts
// (~/.gopi/plugins/websearch/scripts/ — see the plugin's skills/websearch.md).
// The scripts own all fetching, parsing and fusion and emit one slim JSON
// object each; the Go side only picks the method(s), runs the scripts under a
// last-resort timeout, and hands the JSON to the LLM. Script failures map to
// status=unavailable — the tool never crashes.
const (
	webSearchDefaultCount = 5
	webSearchMaxCount     = 20

	// webSearchMergeTimeout bounds the whole multi-method default path
	// (bing + google + arxiv run concurrently, then merge_results.py).
	webSearchMergeTimeout = 120 * time.Second

	// webSearchResultCap caps the JSON fed back to the LLM so one noisy
	// search cannot flood the context window (same spirit as the bash tool's
	// soft cap).
	webSearchResultCap = 16 * 1024

	// webFetch defaults (mirror web_fetch.py): cleaned-text budget default
	// and hard cap. The script enforces the same bounds; the Go side clamps
	// so the LLM cannot ask for an unbounded page dump.
	webFetchDefaultMaxChars = 8000
	webFetchMaxChars        = 100000

	// webFetchResultCap is the last-resort byte cap on the fetch JSON (the
	// script already tail-truncates content at webFetchMaxChars; this guards
	// against pathological escaping blowing up the context window).
	webFetchResultCap = 200 * 1024
)

// webSearchScriptTimeout is the last-resort wall-clock cap per script.
// The scripts own the fast paths (curl --max-time, API timeouts) and
// self-report status=timeout; this only guards against a hung process.
// Generous because remote fetches (google via search-bot, the deepseek API)
// are slow by design. A var (not const) so hermetic tests can shrink it.
var webSearchScriptTimeout = 200 * time.Second

// webSearchScripts maps method name → plugin script file. doubao was removed
// with its external script (doubao_ask.py deleted — no-login policy: nothing
// unscripted may be submitted); the plugin now ships bing/google/arxiv/
// deepseek/github/wiki/merge_results only.
var webSearchScripts = map[string]string{
	"bing":     "bing_search.py",
	"google":   "google_search.py",
	"arxiv":    "arxiv_search.py",
	"deepseek": "deepseek_ask.py",
	"github":   "github_search.py",
	"wiki":     "wiki_search.py",
}

// webSearchMethodArgs appends extra per-method CLI args after the standard
// query/--count pair. github_search.py is pinned to repository search — the
// tool-level default (its script also supports code/issues, but code search
// needs a token/login, so repositories is the safe unauthenticated default).
var webSearchMethodArgs = map[string][]string{
	"github": {"--type", "repositories"},
}

// webSearchAskMethods answer with one distilled text (question + --max-tokens,
// no --count); the tool passes only the query and lets the script's own
// max-tokens default (800) apply.
var webSearchAskMethods = map[string]bool{"deepseek": true}

// webSearchPayload mirrors the plugin scripts' unified JSON contract:
// {"status":"ok|empty|timeout|unavailable","method":...,"elapsed_ms":N,
//
//	"results":[...],"count":N}. Results are shape-agnostic because methods
//
// differ (bing/google: title/url/snippet; arxiv: +abstract_snippet/published;
// deepseek: text; merged: +source/sources/score).
type webSearchPayload struct {
	Status    string                   `json:"status"`
	Method    string                   `json:"method"`
	Query     string                   `json:"query,omitempty"`
	ElapsedMs int64                    `json:"elapsed_ms"`
	Count     int                      `json:"count"`
	Results   []map[string]interface{} `json:"results"`
	Message   string                   `json:"message,omitempty"`
	Inputs    []webSearchInput         `json:"inputs,omitempty"`
}

// webSearchInput reports one method's outcome inside a merged payload.
type webSearchInput struct {
	Method string `json:"method"`
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// WebSearch runs the web_search tool and returns a unified JSON result string.
//
// query is the search phrase or question (required). method selects one of
// bing|google|arxiv|deepseek|github|wiki; empty runs the multi-method default
// (bing + google + arxiv merged via merge_results.py — dedup + relevance
// sort + source attribution). github searches GitHub repositories (repos via
// the search-bot VPS; code/issues need a token/login so repos is the default).
// wiki searches Wikipedia (MediaWiki API, search-bot VPS fallback behind the
// GFW).
// count caps results (default 5, max 20); it does not apply to the ask
// methods, which return a single distilled answer.
//
// The function never fails: every failure path — missing scripts dir, unknown
// method, script crash, timeout, merge failure — returns a JSON payload with
// status=unavailable and a message, so the LLM can fall back to another
// method instead of the harness erroring.
func WebSearch(query, method string, count int) string {
	start := time.Now()
	unavailable := func(msg string) string {
		return renderWebSearch(webSearchPayload{
			Status:  WebSearchStatusUnavailable,
			Method:  methodName(method),
			Query:   query,
			Results: []map[string]interface{}{},
			Message: msg,
		})
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return unavailable("missing query")
	}
	count = clampWebSearchCount(count)

	dir := webSearchScriptsDir()
	if _, err := os.Stat(dir); err != nil {
		return unavailable(fmt.Sprintf("websearch scripts dir not found: %s — install the plugin scripts there, or point %s at them",
			dir, config.WebSearchScriptsDirEnvName))
	}

	if method != "" {
		script, ok := webSearchScripts[method]
		if !ok {
			return unavailable(fmt.Sprintf("unknown method %q (want bing|google|arxiv|deepseek|github|wiki)", method))
		}
		out := runWebSearchScript(context.Background(), filepath.Join(dir, script), method, query, count)
		return capWebSearchResult(out)
	}

	return capWebSearchResult(webSearchMerged(start, dir, query, count))
}

// methodName reports the effective method label for an unavailable payload
// ("" resolves to the multi-method "merged" label).
func methodName(method string) string {
	if method == "" {
		return "merged"
	}
	return method
}

// webSearchScriptsDir resolves the plugin scripts directory: env override
// first, else <gopi data root>/plugins/websearch/scripts. The resolution — and
// the env name it reads — lives in internal/config (config.WebSearchScriptsDir,
// Rule 2: ONE reader), because the SANDBOX initializer must materialise the very
// path this function resolves for GOPI_HOME=<sandbox>; two joins with retyped
// leaves is how "dead inside a sandbox" happened.
func webSearchScriptsDir() string { return config.WebSearchScriptsDir() }

// clampWebSearchCount bounds count to [1, webSearchMaxCount]; non-positive
// input falls back to the default (the model may pass 0 or omit it).
func clampWebSearchCount(count int) int {
	if count <= 0 {
		return webSearchDefaultCount
	}
	if count > webSearchMaxCount {
		return webSearchMaxCount
	}
	return count
}

// runWebSearchScript runs one plugin script (all python3 — the plugin ships
// .py entrypoints) and returns its stdout JSON verbatim. Every failure (start
// error, timeout, non-zero exit, empty output, unparseable JSON) degrades to
// a status=unavailable payload — never an error and never a crash.
func runWebSearchScript(ctx context.Context, path, method, query string, count int) string {
	args := []string{query, "--count", strconv.Itoa(count)}
	if methodExtra, ok := webSearchMethodArgs[method]; ok {
		args = append(args, methodExtra...)
	}
	if webSearchAskMethods[method] {
		args = []string{query}
	}

	out, err := runPluginScript(ctx, path, args, webSearchScriptTimeout)
	if err != nil {
		return unavailableJSON(method, fmt.Sprintf("%s: %v", method, err))
	}
	if !json.Valid([]byte(out)) {
		return unavailableJSON(method, fmt.Sprintf("%s produced unparseable output", method))
	}
	return out
}

// runPluginScript runs one plugin python3 script with the given args under a
// wall-clock timeout, returning its trimmed stdout. On failure (start error,
// timeout, non-zero exit) it returns an error — callers map it to their own
// unavailable/error payload. The script runs in its own process group so the
// timeout can kill the WHOLE group, not just the interpreter (same pattern
// as the bash tool).
func runPluginScript(ctx context.Context, path string, args []string, timeout time.Duration) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "python3", append([]string{path}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("script start failed: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-runCtx.Done():
		// Last-resort kill: the script's own timeouts should have fired first
		// and returned a status JSON; reaching here means the process hung.
		// The group id is RE-VERIFIED before delivery: it is this child's pid, and the
		// kernel may have recycled that number to an unrelated group (see killGroupGuard.go).
		_, _ = signalProcessGroup(cmd.Process.Pid, cmd.Process.Pid, syscall.SIGKILL) //stylegate:ignore signal to a process group we are already abandoning; ESRCH is the expected error
		<-done
		return "", fmt.Errorf("script timed out after %s", timeout)
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("script exited: %w (%s)", err, strings.TrimSpace(stderr.String()))
		}
	}

	return strings.TrimSpace(stdout.String()), nil
}

// WebSearchStatusUnavailable is the payload status that means "the tool could
// not run": a missing scripts directory, an unknown method, a script that
// crashed or timed out, a merge that produced nothing. It is exported because a
// CALLER — the CLI, which turns it into a non-zero exit status — must branch on
// the same spelling the payload carries, and a retyped literal is how the two
// sides drift (Rule 1b).
const WebSearchStatusUnavailable = "unavailable"

// webSearchStatusError is the payload status of a request the tool REFUSED
// (a missing URL, a non-http scheme). It is deliberately NOT the unavailable
// status: the tool ran and answered, so the CLI keeps exit status 0 for it —
// a caller retrying an unavailable tool must not retry a refused argument.
const webSearchStatusError = "error"

// UnavailableStatus reports whether a rendered tool payload carries
// status=unavailable, without the caller having to unmarshal it: the payload is
// one line of JSON produced by this package, and every caller that must react to
// failure (the CLI's exit status) needs the answer for BOTH web faces (search
// and fetch) through ONE function.
func UnavailableStatus(payload string) bool {
	return payloadStatus(payload) == WebSearchStatusUnavailable
}

// unavailableJSON renders a status=unavailable payload (the tool's only
// failure representation).
func unavailableJSON(method, msg string) string {
	return renderWebSearch(webSearchPayload{
		Status:  WebSearchStatusUnavailable,
		Method:  method,
		Results: []map[string]interface{}{},
		Message: msg,
	})
}

// webSearchMerged runs the multi-method default: bing + google + arxiv
// concurrently, then fuses the survivors with merge_results.py (dedup +
// relevance sort + source attribution). Methods that fail self-report
// status=unavailable and are skipped by the merge; the result is ok whenever
// any method survives. If merge_results.py itself cannot run, the best
// surviving single method is returned instead.
func webSearchMerged(start time.Time, dir, query string, count int) string {
	ctx, cancel := context.WithTimeout(context.Background(), webSearchMergeTimeout)
	defer cancel()

	methods := []string{"bing", "google", "arxiv"}
	// Index-keyed slice (NOT a map): three goroutines write concurrently, and
	// Go maps are not safe for concurrent writes even to distinct keys.
	// Same pattern as executeToolBatchParallel's results slice.
	payloads := make([]string, len(methods))
	var wg sync.WaitGroup
	for i, m := range methods {
		wg.Add(1)
		go func(i int, m string) {
			defer wg.Done()
			payloads[i] = runWebSearchScript(ctx, filepath.Join(dir, webSearchScripts[m]), m, query, count)
		}(i, m)
	}
	wg.Wait()

	merged, err := mergeWebSearchResults(ctx, dir, query, count, payloads, methods)
	if err == nil {
		return merged
	}

	// Fallback: merge unavailable — return the best surviving single method
	// (bing preferred), else an unavailable summary of all inputs.
	order := map[string]int{"bing": 0, "google": 1, "arxiv": 2}
	best := ""
	for i, m := range methods {
		if payloadStatus(payloads[i]) == "ok" && (best == "" || order[m] < order[best]) {
			best = m
		}
	}
	if best != "" {
		return payloads[order[best]]
	}

	inputs := make([]webSearchInput, 0, len(methods))
	for i, m := range methods {
		inputs = append(inputs, webSearchInput{Method: m, Status: payloadStatus(payloads[i]), Count: payloadCount(payloads[i])})
	}
	sort.Slice(inputs, func(i, j int) bool { return order[inputs[i].Method] < order[inputs[j].Method] })
	return renderWebSearch(webSearchPayload{
		Status:    WebSearchStatusUnavailable,
		Method:    "merged",
		Query:     query,
		ElapsedMs: msSince(start),
		Results:   []map[string]interface{}{},
		Message:   fmt.Sprintf("no method returned usable results (merge: %v)", err),
		Inputs:    inputs,
	})
}

// mergeWebSearchResults writes the per-method payloads to temp files and runs
// merge_results.py over them; it returns the merged JSON (or an error when the
// script cannot run or its output is unparseable).
func mergeWebSearchResults(ctx context.Context, dir, query string, count int, payloads []string, methods []string) (string, error) {
	mergePath := filepath.Join(dir, "merge_results.py")
	if _, err := os.Stat(mergePath); err != nil {
		// The ACTIONABLE half of the message: this is the failure a sandbox with
		// an empty scripts directory produces (three real scripts, no merge), and
		// the caller's fix is the same as for a missing directory — put the
		// plugin's scripts at the resolved path, or point the knob at them.
		return "", fmt.Errorf("merge_results.py not found: %w — install the plugin scripts there, or point %s at them (%s)",
			err, config.WebSearchScriptsDirEnvName, dir)
	}

	tmp, err := scratch.MkdirTemp(scratch.RuntimeClassWebSearch, "gopi-websearch-merge-*")
	if err != nil {
		return "", fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	files := make([]string, 0, len(methods))
	for i, m := range methods {
		p := filepath.Join(tmp, m+".json")
		if err := os.WriteFile(p, []byte(payloads[i]), 0o600); err != nil {
			return "", fmt.Errorf("write %s: %w", p, err)
		}
		files = append(files, p)
	}

	args := append(files, "--query", query, "--max-results", strconv.Itoa(count))
	runCtx, cancel := context.WithTimeout(ctx, webSearchMergeTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(runCtx, "python3", append([]string{mergePath}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("merge start: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-runCtx.Done():
		// The group id is RE-VERIFIED before delivery: it is this child's pid, and the
		// kernel may have recycled that number to an unrelated group (see killGroupGuard.go).
		_, _ = signalProcessGroup(cmd.Process.Pid, cmd.Process.Pid, syscall.SIGKILL) //stylegate:ignore signal to a process group we are already abandoning; ESRCH is the expected error
		<-done
		return "", fmt.Errorf("merge timed out")
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("merge exited: %w (%s)", err, strings.TrimSpace(stderr.String()))
		}
	}
	out := strings.TrimSpace(stdout.String())
	if !json.Valid([]byte(out)) {
		return "", fmt.Errorf("merge produced unparseable output")
	}
	return out, nil
}

// payloadStatus extracts the status field of a script JSON payload ("" when
// unparseable — treated as unavailable).
func payloadStatus(payload string) string {
	var p struct {
		Status string `json:"status"`
	}
	if json.Unmarshal([]byte(payload), &p) != nil {
		return WebSearchStatusUnavailable
	}
	return p.Status
}

// payloadCount extracts the count field of a script JSON payload.
func payloadCount(payload string) int {
	var p struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal([]byte(payload), &p) //stylegate:ignore best-effort decode of an optional field; the zero value is the documented fallback
	return p.Count
}

// renderWebSearch marshals a payload to the tool's single-line JSON output.
func renderWebSearch(p webSearchPayload) string {
	if p.Results == nil {
		p.Results = []map[string]interface{}{}
	}
	b, err := json.Marshal(p)
	if err != nil {
		// Unreachable in practice (payload holds only marshalable fields);
		// keep the contract: never crash, always JSON.
		return fmt.Sprintf(`{"status":`+WebSearchStatusUnavailable+`,"method":%q,"results":[],"count":0,"message":"render failed: %v"}`, p.Method, err)
	}
	return string(b)
}

// capWebSearchResult truncates the tool result at webSearchResultCap with a
// note, so one noisy search cannot flood the context window.
func capWebSearchResult(out string) string {
	if len(out) <= webSearchResultCap {
		return out
	}
	cut := out[:webSearchResultCap]
	if i := strings.LastIndexByte(cut, '\n'); i > len(cut)-200 {
		cut = cut[:i]
	}
	return cut + fmt.Sprintf("\n[web_search output truncated at %d bytes]", webSearchResultCap)
}

// msSince reports elapsed milliseconds since start (mirrors the scripts'
// elapsed_ms field).
func msSince(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}

// ---------------------------------------------------------------------------
// web_fetch — the fetch side of the search/fetch split (Search ≠ Read).
// web_search returns snippets; web_fetch reads ONE URL's cleaned full text.
// ---------------------------------------------------------------------------

// webFetchPayload mirrors web_fetch.py's unified JSON contract:
// {"status":"ok|error|timeout|unavailable","url":"...","chars":N,
//
//	"truncated":bool,"content":"...","elapsed_ms":N,"message":"..."}
type webFetchPayload struct {
	Status    string `json:"status"`
	URL       string `json:"url"`
	Chars     int    `json:"chars"`
	Truncated bool   `json:"truncated"`
	Content   string `json:"content"`
	ElapsedMs int64  `json:"elapsed_ms"`
	Message   string `json:"message,omitempty"`
}

// WebFetch fetches a single http(s) URL and returns its cleaned full text as
// a unified JSON result string. The page is fetched on the search-bot VPS
// jump host (GFW'd local host cannot reach many overseas sites), boilerplate
// HTML is stripped, and the text is tail-truncated to maxChars (default 8000)
// so the payload stays slim. The script owns status semantics: ok (content
// returned, possibly truncated), error (bad scheme / HTTP >= 400 / binary
// body), timeout (remote curl --max-time), unavailable (jump-host/connection
// failure). The function never fails — every failure path returns a JSON
// payload with an explanatory message, so the LLM can fall back instead of
// the harness erroring.
func WebFetch(url string, maxChars int) string {
	start := time.Now()
	fail := func(status, msg string) string {
		return renderWebFetch(webFetchPayload{
			Status:    status,
			URL:       url,
			ElapsedMs: msSince(start),
			Message:   msg,
		})
	}

	url = strings.TrimSpace(url)
	if url == "" {
		return fail(webSearchStatusError, "missing url")
	}
	if !isHTTPURL(url) {
		return fail(webSearchStatusError, "only http/https URLs are supported")
	}
	maxChars = clampWebFetchMaxChars(maxChars)

	dir := webSearchScriptsDir()
	script := filepath.Join(dir, "web_fetch.py")
	if _, err := os.Stat(script); err != nil {
		return fail(WebSearchStatusUnavailable, fmt.Sprintf("web_fetch.py not found: %s — install the plugin scripts there, or point %s at them",
			script, config.WebSearchScriptsDirEnvName))
	}

	ctx, cancel := context.WithTimeout(context.Background(), webSearchScriptTimeout)
	defer cancel()
	out, err := runPluginScript(ctx, script,
		[]string{url, "--max-chars", strconv.Itoa(maxChars)}, webSearchScriptTimeout)
	if err != nil {
		return fail(WebSearchStatusUnavailable, err.Error())
	}
	if !json.Valid([]byte(out)) {
		return fail(WebSearchStatusUnavailable, "web_fetch.py produced unparseable output")
	}
	return capWebFetchResult(out)
}

// isHTTPURL reports whether s is an absolute http/https URL with a host
// (the web_fetch security gate — mirrors web_fetch.py's local validation).
func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// clampWebFetchMaxChars bounds maxChars to [1, webFetchMaxChars]; non-positive
// input falls back to the default (the model may pass 0 or omit it). The
// script applies its own floor (MIN_MAX_CHARS=200) on the low end.
func clampWebFetchMaxChars(maxChars int) int {
	if maxChars <= 0 {
		return webFetchDefaultMaxChars
	}
	if maxChars > webFetchMaxChars {
		return webFetchMaxChars
	}
	return maxChars
}

// renderWebFetch marshals a fetch payload to the tool's single-line JSON
// output (same never-crash, always-JSON contract as renderWebSearch).
func renderWebFetch(p webFetchPayload) string {
	b, err := json.Marshal(p)
	if err != nil {
		return fmt.Sprintf(`{"status":`+webSearchStatusError+`,"url":%q,"chars":0,"truncated":false,"content":"","message":"render failed: %v"}`, p.URL, err)
	}
	return string(b)
}

// capWebFetchResult truncates the tool result at webFetchResultCap with a
// note (same spirit as capWebSearchResult). The script's own max-chars cap
// bounds content, so this only fires on pathological escaping.
func capWebFetchResult(out string) string {
	if len(out) <= webFetchResultCap {
		return out
	}
	cut := out[:webFetchResultCap]
	if i := strings.LastIndexByte(cut, '\n'); i > len(cut)-200 {
		cut = cut[:i]
	}
	return cut + fmt.Sprintf("\n[web_fetch output truncated at %d bytes]", webFetchResultCap)
}
