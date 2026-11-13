package testsuite

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestBingScenarioSuite runs the scenario-level regression suite for
// bing_search.py (plugins/websearch/tests/test_bing_scenario.py) from the Go
// test tree, so a future edit to the script cannot silently break the two
// behaviours that 2026-10 cost us:
//
//  1. a GENUINE bing SERP must be reported ok at the count the caller asked
//     for (`gopi web search "golang" --method bing --count 3` said empty while
//     the page was real — "The Go Programming Language" first);
//  2. a DECOY/challenge page (query echoed, results unrelated and random per
//     request) must never be reported as ok results.
//
// The suite works on CAPTURED pages (plugins/websearch/tests/testdata/, trimmed
// to the #b_results > li.b_algo blocks) driven through the script's real
// parser + relevance gate + finalize — the hand-built-dict unit tests in
// test_search_quality.py cannot see a defect in that path. Zero network: the
// fixtures are files.
func TestBingScenarioSuite(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skipf("python3 not available: %v", err)
	}
	scripts := []string{"bing_search.py", "google_search.py", "arxiv_search.py"}
	for _, name := range scripts {
		if _, err := os.Stat(filepath.Join("..", "plugins", "websearch", "scripts", name)); err != nil {
			t.Fatalf("repo script %s missing: %v (the scenario suite must read the repo copy, not the deployed one)", name, err)
		}
	}
	suite := filepath.Join("..", "plugins", "websearch", "tests", "test_bing_scenario.py")
	if _, err := os.Stat(suite); err != nil {
		t.Fatalf("scenario suite missing: %v", err)
	}
	cmd := exec.Command(py, suite, "-v")
	// Point the suite at the REPO copy explicitly: a stale deployed script in
	// $HOME must never be the thing this gate measures.
	cmd.Env = append(os.Environ(), "BING_SCRIPT="+filepath.ToSlash(
		filepath.Join("..", "plugins", "websearch", "scripts", "bing_search.py")))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bing scenario suite failed: %v\n%s", err, out)
	}
}
