package testsuite

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/heavytest"
)

// web_fetch contract tests — the fetch side of the search/fetch split
// (epic pa-q-p3-wf-contract). Three contracts, zero network:
//
//  1. Geo routing: DIRECT_DOMAINS hosts (CN sites) fetch from the LOCAL
//     network (geo-correct pricing/CN-only variants); everything else goes
//     via the searchbox ssh jump host; localhost/private ranges ALWAYS ssh
//     (SSRF hard gate — a "CN-looking" private host must never go direct).
//  2. Fetch status contract: ok / error (HTTP>=400, binary) / timeout
//     (curl rc=28) / unavailable (ssh failure, http 000, no marker) — each
//     with an explanatory message, never a crash.
//  3. Single-line body regression (example.com class of bug): minified
//     one-line HTML was silently dropped to empty content by the old
//     lines[2] marker split; find+slice keeps it intact.
//
// Mechanism: testsuite/webfetch_contract_driver.py loads the REAL
// plugins/websearch/scripts/web_fetch.py, monkeypatches subprocess.run to
// canned byte payloads, and runs main() — no ssh, no curl, no DNS. The Go
// side only asserts the payload JSON.

type webFetchContractPayload struct {
	Status    string `json:"status"`
	URL       string `json:"url"`
	Chars     int    `json:"chars"`
	Truncated bool   `json:"truncated"`
	Content   string `json:"content"`
	Message   string `json:"message"`
}

// runWebFetchDriver runs the mock-subprocess driver and returns stdout.
// It fails the test on any driver error — in particular on the driver's
// ZERO-NETWORK tripwire (any unexpected subprocess call explodes there).
func runWebFetchDriver(t *testing.T, args ...string) string {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skipf("python3 not available: %v", err)
	}
	driver := filepath.Join("webfetch_contract_driver.py")
	if _, err := os.Stat(driver); err != nil {
		t.Fatalf("driver missing: %v", err)
	}
	// Pin the direct-domain list to the script default: _pick_route reads
	// WEB_FETCH_DIRECT_DOMAINS at import; an ambient value on the host would
	// make the route contract non-deterministic. (Empty string would clear
	// the list entirely — set the exact default instead.)
	t.Setenv("WEB_FETCH_DIRECT_DOMAINS", "bigmodel.cn,z.ai,open.bigmodel.cn,docs.bigmodel.cn")

	cmd := exec.Command(py, append([]string{driver}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("driver %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

// TestWebFetchContract_GeoRoute asserts the direct/ssh routing decision on
// the REAL _pick_route logic: geo-sensitive CN domains + their subdomains go
// direct; everything else (and every private/localhost form — SSRF gate)
// goes via the ssh jump host. A lookalike domain (evil-bigmodel.cn) must NOT
// match the suffix contract.
func TestWebFetchContract_GeoRoute(t *testing.T) {
	probes := []struct{ url, want string }{
		{"https://docs.bigmodel.cn/x", "direct"},
		{"http://bigmodel.cn/", "direct"},
		{"http://sub.docs.bigmodel.cn/x", "direct"}, // subdomain inherits
		{"https://z.ai/pricing", "direct"},
		{"https://example.com/", "ssh"},
		{"https://evil-bigmodel.cn/x", "ssh"}, // suffix must not over-match
		{"http://localhost:9/x", "ssh"},       // SSRF gate: never direct
		{"http://127.0.0.1/x", "ssh"},
		{"http://10.0.0.1/x", "ssh"},
		{"http://10.0.0.1/x", "ssh"},
		{"http://172.16.0.1/x", "ssh"}, // 172.16-31 private range
		{"http://172.32.0.1/x", "ssh"}, // public range still ssh (default)
		{"http://[::1]:8/x", "ssh"},    // bracketed IPv6 loopback
	}
	out := runWebFetchDriver(t, append([]string{"route"},
		func() []string {
			urls := make([]string, len(probes))
			for i, p := range probes {
				urls[i] = p.url
			}
			return urls
		}()...)...)

	got := map[string]string{}
	for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(strings.TrimSpace(ln), "|", 2)
		if len(parts) != 2 {
			t.Fatalf("bad probe line %q (out: %s)", ln, out)
		}
		got[parts[0]] = parts[1]
	}
	for _, p := range probes {
		if got[p.url] != p.want {
			t.Errorf("route(%s) = %q, want %q", p.url, got[p.url], p.want)
		}
	}
}

// TestWebFetchContract_FetchStatusContract runs every canned fetch case and
// asserts the unified payload contract: status semantics, message wording,
// and the ok-path content cleaning (script/style/nav dropped, entities
// unescaped, block tags newline-separated).
func TestWebFetchContract_FetchStatusContract(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionE2ENetwork)
	cases := []struct {
		name   string
		caseID string
		want   func(t *testing.T, p webFetchContractPayload)
	}{
		{
			name:   "single-line body survives (example.com regression)",
			caseID: "ssh-single-line",
			want: func(t *testing.T, p webFetchContractPayload) {
				if p.Status != "ok" || p.Content == "" {
					t.Fatalf("status=%s content=%q: single-line body must yield non-empty cleaned text", p.Status, p.Content)
				}
				for _, want := range []string{"single-line-body-regression-7f3a", "A&B"} {
					if !strings.Contains(p.Content, want) {
						t.Errorf("content missing %q (got %q)", want, p.Content)
					}
				}
				for _, banned := range []string{"<p>", "<html>", "var a=1", "menu-chrome", ".x{}"} {
					if strings.Contains(p.Content, banned) {
						t.Errorf("content still contains boilerplate %q (got %q)", banned, p.Content)
					}
				}
			},
		},
		{
			name:   "multi-line body still parses",
			caseID: "ssh-multiline",
			want: func(t *testing.T, p webFetchContractPayload) {
				if p.Status != "ok" || !strings.Contains(p.Content, "line-body-ok-994d") {
					t.Fatalf("status=%s content=%q, want ok with body marker", p.Status, p.Content)
				}
			},
		},
		{
			name:   "HTTP 500 -> error",
			caseID: "ssh-http500",
			want: func(t *testing.T, p webFetchContractPayload) {
				if p.Status != "error" || !strings.Contains(p.Message, "HTTP 500") {
					t.Fatalf("status=%s message=%q, want error/HTTP 500", p.Status, p.Message)
				}
			},
		},
		{
			name:   "curl http 000 -> unavailable",
			caseID: "ssh-000",
			want: func(t *testing.T, p webFetchContractPayload) {
				if p.Status != "unavailable" || !strings.Contains(p.Message, "http 000") {
					t.Fatalf("status=%s message=%q, want unavailable/http 000", p.Status, p.Message)
				}
			},
		},
		{
			name:   "curl rc=28 -> timeout",
			caseID: "ssh-timeout",
			want: func(t *testing.T, p webFetchContractPayload) {
				if p.Status != "timeout" || !strings.Contains(p.Message, "timed out") {
					t.Fatalf("status=%s message=%q, want timeout", p.Status, p.Message)
				}
			},
		},
		{
			name:   "payload without @@HTTP marker -> unavailable",
			caseID: "ssh-nomarker",
			want: func(t *testing.T, p webFetchContractPayload) {
				if p.Status != "unavailable" || !strings.Contains(p.Message, "no marker") {
					t.Fatalf("status=%s message=%q, want unavailable/no marker", p.Status, p.Message)
				}
			},
		},
		{
			name:   "binary body -> error",
			caseID: "ssh-binary",
			want: func(t *testing.T, p webFetchContractPayload) {
				if p.Status != "error" || !strings.Contains(p.Message, "binary") {
					t.Fatalf("status=%s message=%q, want error/binary", p.Status, p.Message)
				}
			},
		},
		{
			name:   "max-chars truncation marks the tail",
			caseID: "ssh-truncated",
			want: func(t *testing.T, p webFetchContractPayload) {
				if p.Status != "ok" || !p.Truncated {
					t.Fatalf("status=%s truncated=%v, want ok+truncated", p.Status, p.Truncated)
				}
				if !strings.HasSuffix(p.Content, "…[truncated — tail omitted]") {
					t.Errorf("content must end with the truncation marker (got tail: %q)", tail(p.Content, 40))
				}
			},
		},
		{
			name:   "direct route keeps its own content-type marker",
			caseID: "direct-ok",
			want: func(t *testing.T, p webFetchContractPayload) {
				if p.Status != "ok" || !strings.Contains(p.Content, "direct-cny-price-31c9") {
					t.Fatalf("status=%s content=%q, want ok with direct body", p.Status, p.Content)
				}
				if !strings.Contains(p.Message, "direct") {
					t.Errorf("message=%q, want the direct-route content-type marker", p.Message)
				}
			},
		},
		{
			name:   "direct failure never silently falls back to ssh",
			caseID: "direct-fail",
			want: func(t *testing.T, p webFetchContractPayload) {
				if p.Status == "ok" || p.Content != "" {
					t.Fatalf("status=%s content=%q: a failed direct fetch must report failure, not fallback content", p.Status, p.Content)
				}
				if !strings.Contains(p.Message, "(direct route)") {
					t.Errorf("message=%q, want the (direct route) attribution", p.Message)
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := runWebFetchDriver(t, "fetch", c.caseID)
			var p webFetchContractPayload
			if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &p); err != nil {
				t.Fatalf("payload not JSON: %v\n%s", err, out)
			}
			if p.URL == "" {
				t.Errorf("payload.url empty — contract requires the requested url echoed back")
			}
			c.want(t, p)
		})
	}
}

// TestWebFetchContract_SingleLineBodyRegression is the epic's named repro:
// example.com-class minified HTML (whole document on ONE line) fetched via
// the ssh jump host. The pre-fix parser split on lines[2] and silently
// dropped single-line bodies to empty content; the contract is chars>0 with
// the body text intact.
func TestWebFetchContract_SingleLineBodyRegression(t *testing.T) {
	out := runWebFetchDriver(t, "fetch", "ssh-single-line")
	var p webFetchContractPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &p); err != nil {
		t.Fatalf("payload not JSON: %v\n%s", err, out)
	}
	if p.Chars <= 0 || p.Content == "" {
		t.Fatalf("REGRESSION: single-line body dropped — chars=%d content=%q", p.Chars, p.Content)
	}
}

// TestWebFetchContract_RepoScriptsAreTheSourceOfTruth replaces the old
// TestWebFetchContract_DeployedScriptSync (task 01a16ac0, 2026-10-28).
//
// WHAT WAS WRONG. The old gate read /home/user/.gopi/plugins/websearch/scripts/<name>
// and failed whenever that file's bytes differed from the repo copy. Two
// independent facts make that assertion indefensible:
//
//  1. /home/user/.gopi/plugins is the OPERATOR'S INSTALL AREA, not a mirror of this
//     checkout. internal/scratch/profile_root.go lists it among the profile
//     root's children as `Class: ProfileChildState` with the bound "the plugin
//     tree the operator installs into; never a candidate" — i.e. the system's
//     OWN registry forbids treating it as disposable, derived material. A file
//     there is the operator's, and a repo checkout has no standing to require
//     it to match.
//  2. NOTHING MATERIALISES THE MIRROR. grep for the resolver's readers returns
//     no write path: config.WebSearchScriptsDir is read at RUN time
//     (internal/tools/websearch.go:179), and the only writer of that tree is a
//     human running cp. So the equality the old gate demanded was never
//     guaranteed by any mechanism; it held by luck between syncs, and the
//     measured history is exactly that — the seven files there all carry
//     mtime 2026-09-12 02:58:19 while the repo side was edited repeatedly
//     until 2026-10-18. The gate proved nothing about the repo and everything
//     about whether somebody had remembered to copy.
//
// Worse, its failure message told the reader to "sync one side (cp) and
// commit" — an instruction that pulls the operator's local，installed tree
// into a repository commit, which is the opposite of a source of truth.
//
// WHAT THE EPIC ACTUALLY LEARNED, and what survives here: the deployed copy
// once carried two fixes the repo lacked, so the lesson is "the REPO is the
// source of truth and no gate may measure anything else" — the shape
// TestBingScenario already states one file over ("a stale deployed script in
// $HOME must never be the thing this gate measures"). This gate therefore
// reads the repo copy ONLY, and asserts the properties a source of truth must
// have on its own: every versioned script is present, non-empty, carries a
// python entry point, and compiles.
//
// A drift report between $HOME and the repo would be useful, but it is not a
// SUITE gate: a check whose verdict depends on an operator's home directory
// cannot be green in CI, in a fresh clone, or in the sandbox — it would be red
// for reasons the repo cannot fix.
func TestWebFetchContract_RepoScriptsAreTheSourceOfTruth(t *testing.T) {
	scripts := []string{
		"bing_search.py", "google_search.py", "arxiv_search.py",
		"github_search.py", "wiki_search.py", "web_fetch.py",
		"deepseek_ask.py", "merge_results.py",
	}
	py, pyErr := exec.LookPath("python3")
	for _, name := range scripts {
		repo, err := filepath.Abs(filepath.Join("..", "plugins", "websearch", "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(repo)
		if err != nil {
			t.Errorf("repo script %s missing from the source of truth: %v", name, err)
			continue
		}
		if len(body) == 0 {
			t.Errorf("repo script %s is EMPTY — an empty file satisfies `git add` and breaks every caller", name)
			continue
		}
		// A python entry point, not merely a file with a .py name (Rule 15: a
		// named file that cannot run is a silent failure waiting to happen).
		if !bytes.Contains(body, []byte("__main__")) && !bytes.Contains(body, []byte("def main(")) {
			t.Errorf("repo script %s carries no entry point (no __main__ block and no main()) — it cannot be executed as a tool", name)
		}
		if pyErr != nil {
			continue // no interpreter on this host: presence + entry point are all we can judge
		}
		if out, err := exec.Command(py, "-m", "py_compile", repo).CombinedOutput(); err != nil {
			t.Errorf("repo script %s does not compile:\n%s", name, out)
		}
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
