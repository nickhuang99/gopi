// cli_web_page_harness_route_census_test.go — the CENSUS arm the route guard
// was missing: every chat route the server registers must be one the harness
// knows about by name.
//
// Why it exists (the residual of the defect fixed in 2b192e3b). That commit made
// each bucket an EQUALITY test on a named route, which is what stopped the +1:
// a new '/api/chat…' route can no longer fall into the turn bucket by accident.
// But the harness is plain node and carries its OWN copy of the route names, and
// the existing guard (cli_web_page_harness_route_test.go) compares the FIVE
// names it already knows — one per entry in its `want` map. It is therefore
// blind to the sixth: add `const webChatSomethingPath = "/api/chat/something"`
// to the mux and nothing fails, the harness keeps its five, and the new route
// falls through to the empty-JSON fallback. That is not a counting defect
// (equality keeps it out of `state.posts`), it is the OBSERVABILITY defect the
// same commit fixed for /reset and /interrupt: "they had no handler at all and
// had been falling through to an empty JSON ack". Nothing could assert on a
// route the harness did not model.
//
// So this file reads the SERVER's declarations out of the Go sources and the
// harness's out of the harness source and reports the set difference, in both
// directions. Rule 13/14: it reuses harnessRouteConsts (the ONE parser of the
// harness's literals) rather than declaring a second copy of it, and it declares
// only the server-side pattern, which nothing else reads.
//
// Both directions matter, but they are NOT the same severity, and the messages
// say which is which:
//   - a server route the harness does not name → the harness cannot observe it
//     (a silent blind spot; the scenario that should assert on it is vacuous);
//   - a harness route the server does not name → the harness counts a route
//     nothing POSTs to (the "turn tally is not about the shipped page" failure
//     TestPageHarnessRoutesAgreeWithTheServer already words for the five).

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// serverChatPathConstRe reads a chat route declaration out of the server's own
// source: `const webChatQueuePath = "/api/chat/queue"`. The name is required to
// start with `webChat` because that is the naming convention the mux's chat
// routes follow (webChatPath, webChatQueuePath, …); the path is required to sit
// under '/api/chat' because that is the prefix whose over-broad matching caused
// the defect. A declaration outside both is not this census's subject.
var serverChatPathConstRe = regexp.MustCompile(`(?m)^const (webChat\w*) = "(/api/chat[^"]*)"$`)

// serverChatPathFloors is the anti-vacuum floor: the mux registered five chat
// routes when this census landed (turn, queue, queue/cancel, reset, interrupt).
// A scan that finds fewer has stopped reading the file it thinks it reads, and
// must not pass as "no difference".
const serverChatPathFloor = 5

// serverChatRouteSources are the Go files that declare the chat routes. Named
// rather than globbed: a census that quietly widened to every file would start
// reporting test fixtures and the page's own JS.
var serverChatRouteSources = []string{"cli_web.go", "cli_web_queue.go"}

// serverChatPaths scans the server sources and returns name → path.
func serverChatPaths(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, name := range serverChatRouteSources {
		raw, err := os.ReadFile(filepath.Join(filepath.Dir(pageHarnessPath), name))
		if err != nil {
			t.Fatalf("read %s: %v — the census reads the server's own declarations, so an unreadable source is a refusal, never an empty set", name, err)
		}
		for _, m := range serverChatPathConstRe.FindAllStringSubmatch(string(raw), -1) {
			out[m[1]] = m[2]
		}
	}
	if len(out) < serverChatPathFloor {
		t.Fatalf("found %d chat route declaration(s) in %v, floor is %d — the regex or the declarations moved, so this census would report a difference over a set it never read",
			len(out), serverChatRouteSources, serverChatPathFloor)
	}
	return out
}

// chatRouteCensus diffs the two sets. It is a pure function so the RED PROOF can
// plant a mismatch without mutating a production file (Rule 5's restore
// criterion is trivial when nothing production-side is touched).
func chatRouteCensus(server, harness map[string]string) (unobserved, phantom []string) {
	harnessByPath := map[string]string{}
	for n, p := range harness {
		harnessByPath[p] = n
	}
	serverByPath := map[string]string{}
	for n, p := range server {
		serverByPath[p] = n
	}
	for p, n := range serverByPath {
		if _, ok := harnessByPath[p]; !ok {
			unobserved = append(unobserved, fmt.Sprintf(`%s (%s)`, p, n))
		}
	}
	for p, n := range harnessByPath {
		if _, ok := serverByPath[p]; !ok {
			phantom = append(phantom, fmt.Sprintf(`%s (%s)`, p, n))
		}
	}
	sort.Strings(unobserved)
	sort.Strings(phantom)
	return unobserved, phantom
}

// TestPageHarnessModelsEveryChatRouteInTheMux is the real-tree arm: no server
// chat route may be invisible to the harness, and no harness route may name a
// path the server does not register.
func TestPageHarnessModelsEveryChatRouteInTheMux(t *testing.T) {
	server := serverChatPaths(t)
	harness := harnessRouteConsts(t)
	unobserved, phantom := chatRouteCensus(server, harness)
	if len(unobserved) > 0 {
		t.Errorf("%s models no route for %d chat route(s) the server registers: %s — a route the harness does not name falls through to its empty-JSON fallback, so no scenario can assert anything about it (this is how /reset and /interrupt went untested until 2b192e3b). Declare it as a route constant and give it its own bucket.",
			filepath.Base(pageHarnessPath), len(unobserved), strings.Join(unobserved, ", "))
	}
	if len(phantom) > 0 {
		t.Errorf("%s names %d chat route(s) that no source registers: %s — the harness would count a route nothing POSTs to, so its tally is not about the shipped page",
			filepath.Base(pageHarnessPath), len(phantom), strings.Join(phantom, ", "))
	}
	t.Logf("chat route census: server=%d harness=%d unobserved=%d phantom=%d", len(server), len(harness), len(unobserved), len(phantom))
}

// TestChatRouteCensusDetectsAPlantedMismatch is the RED PROOF for the arm above.
// It plants one route of each kind into in-memory maps — no production file is
// touched, so there is nothing to restore — and requires the census to name the
// planted route by PATH, not merely to report "a difference".
func TestChatRouteCensusDetectsAPlantedMismatch(t *testing.T) {
	server := map[string]string{
		"webChatPath":         "/api/chat",
		"webChatQueuePath":    "/api/chat/queue",
		"webChatBrandNewPath": "/api/chat/brand-new",
	}
	harness := map[string]string{
		"CHAT_TURN_ROUTE":    "/api/chat",
		"CHAT_QUEUE_ROUTE":   "/api/chat/queue",
		"CHAT_PHANTOM_ROUTE": "/api/chat/phantom",
	}
	unobserved, phantom := chatRouteCensus(server, harness)
	if len(unobserved) != 1 || !strings.Contains(unobserved[0], "/api/chat/brand-new") {
		t.Fatalf("the census did not name the server route the harness does not model (got %v) — it must point at the path, or the refusal is unactionable", unobserved)
	}
	if len(phantom) != 1 || !strings.Contains(phantom[0], "/api/chat/phantom") {
		t.Fatalf("the census did not name the harness route no source registers (got %v)", phantom)
	}
	t.Logf("planted mismatch detected: unobserved=%v phantom=%v", unobserved, phantom)
}

// TestChatRouteCensusAgreesOnTheRealSets is the green control: run the census
// over the SHIPPED sets and require no difference, so the red proof above cannot
// be passing because the census reports a difference unconditionally.
func TestChatRouteCensusAgreesOnTheRealSets(t *testing.T) {
	unobserved, phantom := chatRouteCensus(serverChatPaths(t), harnessRouteConsts(t))
	if len(unobserved) != 0 || len(phantom) != 0 {
		t.Fatalf("the shipped sets disagree: unobserved=%v phantom=%v", unobserved, phantom)
	}
}
