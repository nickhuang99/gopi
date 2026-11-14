package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// fakeScriptsDir writes a fake websearch plugin script set into a temp dir
// and points webSearchScriptsDir at it (restored on test end). The fakes
// echo their arguments back so tests can assert query/count routing. All
// fakes are python3 scripts — the tool runs every plugin script via python3
// (the plugin ships .py entrypoints only).
func fakeScriptsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	scripts := map[string]string{
		"bing_search.py": `#!/usr/bin/env python3
import json, sys
out = {"status": "ok", "method": "bing", "elapsed_ms": 1,
       "count": int(sys.argv[3]),
       "results": [{"title": "t:" + sys.argv[1], "url": "http://bing.example/1",
                    "snippet": "s"}]}
print(json.dumps(out))`,
		"google_search.py": `#!/usr/bin/env python3
print('{"status":"empty","method":"google","elapsed_ms":1,"results":[],"count":0}')`,
		// arxiv is the "broken method": crashes immediately.
		"arxiv_search.py": `#!/usr/bin/env python3
import sys
sys.exit(1)`,
		// deepseek refuses --count (ask scripts must not receive it).
		"deepseek_ask.py": `#!/usr/bin/env python3
import json, sys
if "--count" in sys.argv:
    sys.exit(1)
out = {"status": "ok", "method": "deepseek", "elapsed_ms": 1,
       "results": [{"text": "answer for: " + sys.argv[1]}], "count": 1}
print(json.dumps(out))`,
		"merge_results.py": `#!/usr/bin/env python3
import json, sys
# consume --query/--max-results VALUE pairs so their values are not counted
# as files (the real merge_results.py uses argparse; this fake mimics the
# parsing contract)
args = sys.argv[1:]
files = []
query = None
while args:
    a = args.pop(0)
    if a in ("--query", "--max-results"):
        if args:
            if a == "--query":
                query = args.pop(0)
            else:
                args.pop(0)
    else:
        files.append(a)
out = {"status": "ok", "method": "merged", "query": query, "elapsed_ms": 1,
       "count": len(files),
       "inputs": [{"method": "fake", "status": "ok"} for _ in files],
       "results": [{"title": "merged:" + query, "url": "http://merged.example/1",
                    "snippet": "s", "source": "bing", "sources": ["bing"]}]}
print(json.dumps(out))`,
		// web_fetch echoes url + --max-chars so tests can assert routing.
		"web_fetch.py": `#!/usr/bin/env python3
import json, sys
args = sys.argv[1:]
mc = int(args[args.index("--max-chars")+1]) if "--max-chars" in args else None
out = {"status": "ok", "url": args[0], "chars": mc, "truncated": False,
       "content": "fetched:" + args[0] + ":" + str(mc), "elapsed_ms": 1}
print(json.dumps(out))`,
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(config.WebSearchScriptsDirEnvName, dir)
	return dir
}

// parseResult is the JSON shape all websearch outputs share (status/method/
// count/results + merged extras).
type parseResult struct {
	Status  string `json:"status"`
	Method  string `json:"method"`
	Count   int    `json:"count"`
	Query   string `json:"query"`
	Results []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		Text  string `json:"text"`
	} `json:"results"`
	Message string `json:"message"`
}

func mustParse(t *testing.T, out string) parseResult {
	t.Helper()
	var r parseResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("non-JSON output: %v\n%s", err, out)
	}
	return r
}

// TestWebSearchSingleMethodBing: the single-method path runs the one script
// and passes query + count through.
func TestWebSearchSingleMethodBing(t *testing.T) {
	fakeScriptsDir(t)
	r := mustParse(t, WebSearch("deepseek", "bing", 3))
	if r.Status != "ok" || r.Method != "bing" || r.Count != 3 {
		t.Fatalf("unexpected result: %+v", r)
	}
	if len(r.Results) != 1 || r.Results[0].Title != "t:deepseek" {
		t.Fatalf("query not passed through: %+v", r)
	}
}

// TestWebSearchCountClamp: count clamps to [1,20]; 0 falls back to the
// default. The fake bing echoes the --count value it received.
func TestWebSearchCountClamp(t *testing.T) {
	fakeScriptsDir(t)
	if r := mustParse(t, WebSearch("q", "bing", 100)); r.Count != 20 {
		t.Fatalf("count=100 not clamped to 20: %+v", r)
	}
	if r := mustParse(t, WebSearch("q", "bing", 0)); r.Count != 5 {
		t.Fatalf("count=0 not defaulted to 5: %+v", r)
	}
	if r := mustParse(t, WebSearch("q", "bing", -3)); r.Count != 5 {
		t.Fatalf("count=-3 not defaulted to 5: %+v", r)
	}
}

// TestWebSearchSingleDeepseekAsk: ask scripts receive the question only —
// no --count flag (the fake deepseek exits 1 if it sees one).
func TestWebSearchSingleDeepseekAsk(t *testing.T) {
	fakeScriptsDir(t)
	r := mustParse(t, WebSearch("what is rag", "deepseek", 7))
	if r.Status != "ok" || r.Method != "deepseek" {
		t.Fatalf("unexpected result: %+v", r)
	}
	if len(r.Results) != 1 || r.Results[0].Text != "answer for: what is rag" {
		t.Fatalf("question not passed through: %+v", r)
	}
}

// TestWebSearchGitHubTypeArg: the github method pins repository search and
// passes the --type repositories flag (github_search.py's default, and the
// unauthenticated-safe path — code search needs a token/login). The fake
// github script echoes argv so we can assert the flag reaches it.
func TestWebSearchGitHubTypeArg(t *testing.T) {
	fakeScriptsDir(t)
	dir := t.TempDir()
	out := `#!/usr/bin/env python3
import json, sys
args = sys.argv[1:]
out = {"status": "ok", "method": "github", "elapsed_ms": 1,
       "type": args[args.index("--type")+1] if "--type" in args else None,
       "count": int(args[args.index("--count")+1]) if "--count" in args else 0,
       "results": [{"title": "repo:" + args[0], "url": "https://github.com/x/y",
                    "snippet": "s"}]}
print(json.dumps(out))`
	if err := os.WriteFile(filepath.Join(dir, "github_search.py"), []byte(out), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.WebSearchScriptsDirEnvName, dir)
	r := mustParse(t, WebSearch("modernc sqlite", "github", 3))
	if r.Status != "ok" || r.Method != "github" {
		t.Fatalf("unexpected result: %+v", r)
	}
	if len(r.Results) != 1 || r.Results[0].Title != "repo:modernc sqlite" {
		t.Fatalf("query not passed through: %+v", r)
	}
	if tf := r.Message; tf != "" && r.Count != 3 {
		// type is echoed in a dedicated field we re-check below via raw parse
	}
	// Re-parse raw to inspect the type field (parseResult has no Type).
	raw := WebSearch("modernc sqlite", "github", 3)
	var typed struct {
		Type  string `json:"type"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal([]byte(raw), &typed); err != nil {
		t.Fatalf("github json: %v", err)
	}
	if typed.Type != "repositories" {
		t.Fatalf("expected --type repositories, got %q", typed.Type)
	}
	if typed.Count != 3 {
		t.Fatalf("expected count 3, got %d", typed.Count)
	}
}

// TestWebSearchWikiMethod: the wiki method runs wiki_search.py with the plain
// query/--count args (no method-specific flags). The fake script echoes argv
// so we can assert the routing contract (same shape as the real script).
func TestWebSearchWikiMethod(t *testing.T) {
	fakeScriptsDir(t)
	dir := t.TempDir()
	out := `#!/usr/bin/env python3
import json, sys
args = sys.argv[1:]
out = {"status": "ok", "method": "wiki", "elapsed_ms": 1,
       "count": int(args[args.index("--count")+1]) if "--count" in args else 0,
       "results": [{"title": "wiki:" + args[0], "url": "https://en.wikipedia.org/wiki/X",
                    "snippet": "s"}]}
print(json.dumps(out))`
	if err := os.WriteFile(filepath.Join(dir, "wiki_search.py"), []byte(out), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.WebSearchScriptsDirEnvName, dir)
	r := mustParse(t, WebSearch("retrieval augmented generation", "wiki", 3))
	if r.Status != "ok" || r.Method != "wiki" {
		t.Fatalf("unexpected result: %+v", r)
	}
	if len(r.Results) != 1 || r.Results[0].Title != "wiki:retrieval augmented generation" {
		t.Fatalf("query not passed through: %+v", r)
	}
	if r.Count != 3 {
		t.Fatalf("expected count 3, got %d", r.Count)
	}
}

// TestWebSearchUnavailablePassThrough: a script's own status=unavailable is
// passed through verbatim — the tool does not crash on it.
func TestWebSearchUnavailablePassThrough(t *testing.T) {
	fakeScriptsDir(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bing_search.py"),
		[]byte("#!/usr/bin/env python3\nprint('{\"status\":\"unavailable\",\"method\":\"bing\",\"elapsed_ms\":1,\"results\":[],\"count\":0}')"),
		0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.WebSearchScriptsDirEnvName, dir)
	r := mustParse(t, WebSearch("q", "bing", 5))
	if r.Status != "unavailable" || r.Method != "bing" {
		t.Fatalf("expected unavailable passthrough: %+v", r)
	}
}

// TestWebSearchUnknownMethod: an invalid method name yields a clean
// unavailable JSON, not a crash.
func TestWebSearchUnknownMethod(t *testing.T) {
	fakeScriptsDir(t)
	r := mustParse(t, WebSearch("q", "yahoo", 5))
	if r.Status != "unavailable" || !strings.Contains(r.Message, "unknown method") {
		t.Fatalf("expected unavailable for unknown method: %+v", r)
	}
}

// TestWebSearchMissingScript: a method whose script file does not exist
// yields unavailable.
func TestWebSearchMissingScript(t *testing.T) {
	// fakeScriptsDir leaves out "google_search.py" when told to remove it.
	dir := fakeScriptsDir(t)
	if err := os.Remove(filepath.Join(dir, "google_search.py")); err != nil {
		t.Fatal(err)
	}
	r := mustParse(t, WebSearch("q", "google", 5))
	if r.Status != "unavailable" {
		t.Fatalf("expected unavailable for missing script: %+v", r)
	}
}

// TestWebSearchNonJSON: a script that prints garbage is treated as a broken
// method and wrapped into unavailable.
func TestWebSearchNonJSON(t *testing.T) {
	fakeScriptsDir(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bing_search.py"),
		[]byte("#!/usr/bin/env python3\nprint('hello world')\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.WebSearchScriptsDirEnvName, dir)
	r := mustParse(t, WebSearch("q", "bing", 5))
	if r.Status != "unavailable" {
		t.Fatalf("expected unavailable for non-JSON output: %+v", r)
	}
}

// TestWebSearchTimeout: a hung script is killed by the tool timeout and the
// result is unavailable — the agent loop does not hang.
func TestWebSearchTimeout(t *testing.T) {
	fakeScriptsDir(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bing_search.py"),
		[]byte("#!/usr/bin/env python3\nimport time\ntime.sleep(30)\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.WebSearchScriptsDirEnvName, dir)
	origTimeout := webSearchScriptTimeout
	webSearchScriptTimeout = 200 * time.Millisecond
	defer func() { webSearchScriptTimeout = origTimeout }()

	start := time.Now()
	r := mustParse(t, WebSearch("q", "bing", 5))
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout not enforced: took %v", elapsed)
	}
	if r.Status != "unavailable" {
		t.Fatalf("expected unavailable after timeout: %+v", r)
	}
}

// TestWebSearchMerged: the default mode runs all methods, tolerates the
// broken one (arxiv crash), and feeds the survivors to merge_results.py
// with the query.
func TestWebSearchMerged(t *testing.T) {
	fakeScriptsDir(t)
	r := mustParse(t, WebSearch("rag", "", 5))
	if r.Status != "ok" || r.Method != "merged" {
		t.Fatalf("expected merged ok: %+v", r)
	}
	if r.Query != "rag" {
		t.Fatalf("query not passed to merge: %+v", r)
	}
	// bing (ok) + google (empty) + arxiv (crash) each wrote a file; the merge
	// consumes every payload file regardless of status.
	if r.Count != 3 {
		t.Fatalf("merge should see 3 method files, got %d: %+v", r.Count, r)
	}
	if len(r.Results) != 1 || !strings.Contains(r.Results[0].Title, "rag") {
		t.Fatalf("merged results missing: %+v", r)
	}
}

// TestWebSearchMergedAllFail: when every method fails AND merge_results.py
// cannot run, the tool falls back to an unavailable summary of all inputs —
// never a crash or a half-empty merge.
func TestWebSearchMergedAllFail(t *testing.T) {
	dir := fakeScriptsDir(t)
	// Keep only the crashing arxiv script — all other methods have no file,
	// and merge_results.py is gone too so the merge itself fails.
	for _, name := range []string{"bing_search.py", "google_search.py", "merge_results.py"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	r := mustParse(t, WebSearch("q", "", 5))
	if r.Status != "unavailable" || r.Method != "merged" {
		t.Fatalf("expected unavailable merged: %+v", r)
	}
}

// ---------------------------------------------------------------------------
// web_fetch tests (the fetch side of the search/fetch split).
// ---------------------------------------------------------------------------

// fetchResult is the JSON shape web_fetch outputs share (mirrors
// web_fetch.py's contract).
type fetchResult struct {
	Status    string `json:"status"`
	URL       string `json:"url"`
	Chars     int    `json:"chars"`
	Truncated bool   `json:"truncated"`
	Content   string `json:"content"`
	Message   string `json:"message"`
}

func mustParseFetch(t *testing.T, out string) fetchResult {
	t.Helper()
	var r fetchResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("non-JSON output: %v\n%s", err, out)
	}
	return r
}

// TestWebFetchOk: the tool runs web_fetch.py with the URL + --max-chars and
// passes its JSON through. max_chars omitted → default 8000.
func TestWebFetchOk(t *testing.T) {
	fakeScriptsDir(t)
	r := mustParseFetch(t, WebFetch("https://example.com/page", 0))
	if r.Status != "ok" || r.URL != "https://example.com/page" {
		t.Fatalf("unexpected result: %+v", r)
	}
	if r.Chars != 8000 {
		t.Fatalf("max_chars default 8000 not applied: %+v", r)
	}
	if r.Content != "fetched:https://example.com/page:8000" {
		t.Fatalf("url/max_chars not passed through: %+v", r)
	}
}

// TestWebFetchMaxCharsClamp: max_chars clamps to [1,100000]; 0 falls back to
// the default. The fake echoes the --max-chars value it received.
func TestWebFetchMaxCharsClamp(t *testing.T) {
	fakeScriptsDir(t)
	if r := mustParseFetch(t, WebFetch("https://example.com/x", 1_000_000)); r.Chars != 100000 {
		t.Fatalf("max_chars=1e6 not clamped to 100000: %+v", r)
	}
	if r := mustParseFetch(t, WebFetch("https://example.com/x", 1234)); r.Chars != 1234 {
		t.Fatalf("max_chars=1234 not passed through: %+v", r)
	}
}

// TestWebFetchInvalidURL: non-http(s) schemes and empty URLs are rejected by
// the Go-side security gate before the script ever runs — status=error, not
// a crash.
func TestWebFetchInvalidURL(t *testing.T) {
	fakeScriptsDir(t)
	for _, u := range []string{"file:///etc/passwd", "ftp://example.com/x", "javascript:alert(1)"} {
		r := mustParseFetch(t, WebFetch(u, 0))
		if r.Status != "error" || !strings.Contains(r.Message, "http") {
			t.Fatalf("url %q: expected error with http hint, got %+v", u, r)
		}
	}
	r := mustParseFetch(t, WebFetch("", 0))
	if r.Status != "error" || !strings.Contains(r.Message, "missing url") {
		t.Fatalf("empty url: expected error, got %+v", r)
	}
}

// TestWebFetchScriptUnavailable: a script's own status=unavailable is passed
// through verbatim — the tool does not crash on it.
func TestWebFetchScriptUnavailable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "web_fetch.py"),
		[]byte("#!/usr/bin/env python3\nprint('{\"status\":\"unavailable\",\"url\":\"https://example.com/x\",\"chars\":0,\"truncated\":false,\"content\":\"\",\"message\":\"ssh to searchbox failed\"}')"),
		0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.WebSearchScriptsDirEnvName, dir)
	r := mustParseFetch(t, WebFetch("https://example.com/x", 0))
	if r.Status != "unavailable" || !strings.Contains(r.Message, "searchbox") {
		t.Fatalf("expected unavailable passthrough: %+v", r)
	}
}

// TestWebFetchMissingScript: web_fetch.py absent → unavailable, never a crash.
func TestWebFetchMissingScript(t *testing.T) {
	dir := t.TempDir() // empty — no web_fetch.py
	t.Setenv(config.WebSearchScriptsDirEnvName, dir)
	r := mustParseFetch(t, WebFetch("https://example.com/x", 0))
	if r.Status != "unavailable" {
		t.Fatalf("expected unavailable for missing script: %+v", r)
	}
}
