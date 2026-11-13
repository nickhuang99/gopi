package tools

// tasknotify_alarms_test.go — the "all"-filter alarm coverage gate.
//
// Bug class this file closes: loadTaskNotifyConfig's "all"/"any" arm
// HAND-LISTED the synthetic alarm events, so an alarm introduced later was
// silently dropped for the operator whose entire configuration says "tell me
// everything". The reported instance is RevertedEvent — the merge ROLLBACK
// alarm fired by RevertNotify when a post-merge rebuild fails and master is
// reset — which GOPI_TASK_NOTIFY_EVENTS=all never delivered (while
// GOPI_TASK_NOTIFY_EVENTS=merge-reverted did). The same hand-list had already
// lost "stale", "build-failed", "merge-withdrawn" and the merge-gate alarms,
// and the agent-side alarms (loop-stuck / loop-stuck-toolful /
// session-wallclock-exceeded) were mirrored in internal/agent, invisible to
// the expansion entirely.
//
// Measured on 0d3ce544 (2026-09-19), EVENTS=all enabled 12 events and
// delivered NONE of merge-reverted / build-failed / stale / loop-stuck —
// while NotifySyntheticEvent's own doc promises `"all" receives everything`.
//
// The expansion is now registry-driven (taskNotifyAlarmEvents, the SINGLE
// source of truth) and the gates below fail the moment a FIRED alarm is
// absent from that registry — the regression guard for the RevertedEvent
// defect, not for its one spelling.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// covToolsGoFiles returns the non-test .go file paths in dir (a missing dir →
// empty slice, so a mis-rooted scan cannot silently pass by finding nothing).
func covToolsGoFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}

// covToolsAlarmConsts maps every constant declared in the non-test .go files
// of dir to its value EXPRESSION (a string literal or another constant's
// name — alias chains such as VetFailedEvent = BuildFailedEvent are real in
// this package and must resolve, or the gate reports a false bare literal).
func covToolsAlarmConsts(t *testing.T, dir string) map[string]ast.Expr {
	t.Helper()
	out := map[string]ast.Expr{}
	for _, path := range covToolsGoFiles(dir) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			continue
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						out[name.Name] = vs.Values[i]
					}
				}
			}
		}
	}
	return out
}

// covToolsResolveAlarmConst resolves a constant name through alias chains to
// its string literal ("" when the name is not a declared string constant —
// e.g. a function parameter).
func covToolsResolveAlarmConst(t *testing.T, consts map[string]ast.Expr, name string, depth int) string {
	t.Helper()
	if depth > 8 {
		t.Errorf("constant alias chain for %q is deeper than 8 — a cycle?", name)
		return ""
	}
	expr, ok := consts[name]
	if !ok {
		return ""
	}
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return ""
		}
		lit, err := strconv.Unquote(v.Value)
		if err != nil {
			return ""
		}
		return lit
	case *ast.Ident:
		return covToolsResolveAlarmConst(t, consts, v.Name, depth+1)
	}
	return ""
}

// covToolsScanNotifyCalls parses every non-test .go file in dir and returns
// the event ARGUMENTS of every NotifySyntheticEvent call — both call shapes
// (`NotifySyntheticEvent(...)` in-package and `tools.NotifySyntheticEvent(...)`
// from internal/agent). A bare string literal is returned as ("literal", lit).
func covToolsScanNotifyCalls(t *testing.T, dir string) [][2]string {
	t.Helper()
	var out [][2]string
	for _, path := range covToolsGoFiles(dir) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				name = fn.Name
			case *ast.SelectorExpr:
				name = fn.Sel.Name
			}
			// NotifySyntheticEvent carries the event in arg 0; MergeVerifyNotify
			// fronts it with a parameter, so its CALL SITES (the real events)
			// are scanned too — a new gate event must not be invisible here.
			if (name != "NotifySyntheticEvent" && name != "MergeVerifyNotify") || len(call.Args) == 0 {
				return true
			}
			switch arg := call.Args[0].(type) {
			case *ast.BasicLit:
				lit, _ := strconv.Unquote(arg.Value) //stylegate:ignore unquotes a literal from the test's own source fixture
				out = append(out, [2]string{"literal", lit})
			case *ast.Ident:
				out = append(out, [2]string{"ident", arg.Name})
			}
			return true
		})
	}
	return out
}

// TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth is the anti-regression
// gate: the registry holds every FIRED alarm, every entry is a declared
// constant, and the "all"/"any" filters enable every entry. A new alarm
// constant that never enters the registry fails HERE instead of silently
// vanishing from "all" — exactly the RevertedEvent defect.
func TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth(t *testing.T) {
	registered := make(map[string]bool, len(taskNotifyAlarmEvents))
	for _, a := range taskNotifyAlarmEvents {
		if strings.TrimSpace(a) == "" {
			t.Errorf("taskNotifyAlarmEvents carries an empty entry — every alarm must be a declared constant")
			continue
		}
		if registered[a] {
			t.Errorf("taskNotifyAlarmEvents lists %q twice", a)
		}
		registered[a] = true
	}

	consts := covToolsAlarmConsts(t, ".")

	// (1) Every registry ENTRY resolves to a declared constant: the registry
	// names constants, never bare wire strings (Rule 1b).
	for _, a := range taskNotifyAlarmEvents {
		found := false
		for name := range consts {
			if covToolsResolveAlarmConst(t, consts, name, 0) == a {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("alarm %q in taskNotifyAlarmEvents is not a declared constant of "+
				"internal/tools — declare it and register the constant (Rule 1b/13)", a)
		}
	}

	// (2) Every alarm a production call site FIRES is registered: this is the
	// content test that the hand-list failed — it names no alarm, it derives
	// the required set from the code.
	calls := covToolsScanNotifyCalls(t, ".")
	if len(calls) == 0 {
		t.Fatalf("no NotifySyntheticEvent/MergeVerifyNotify call site found — the scan is mis-rooted, not the tree clean")
	}
	for _, c := range calls {
		kind, val := c[0], c[1]
		if kind == "literal" {
			t.Errorf("NotifySyntheticEvent called with the bare literal %q — a bare wire "+
				"name cannot be enumerated by the registry and is one refactor away from "+
				"falling out of \"all\" (Rule 1b)", val)
			continue
		}
		lit := covToolsResolveAlarmConst(t, consts, val, 0)
		if lit == "" {
			// A parameter/local (e.g. MergeVerifyNotify's event arg): out of the
			// syntactic gate's reach — its call sites above ARE resolved.
			continue
		}
		if !registered[lit] {
			t.Errorf("the alarm %q (fired as %s) is NOT in taskNotifyAlarmEvents — "+
				"GOPI_TASK_NOTIFY_EVENTS=all will silently drop it for the subscriber "+
				"who asked for everything", lit, val)
		}
	}
}

// TestCovToolsNotifyAllFilterDeliversEveryAlarm is the delivery half: it fires
// EVERY registered alarm through the real pipeline with GOPI_TASK_NOTIFY_EVENTS
// set to "all" and to the "any" synonym, and asserts each one is delivered —
// enabled in the filter map is not the same as delivered (the defect was a
// filter, but the guard must see a payload).
func TestCovToolsNotifyAllFilterDeliversEveryAlarm(t *testing.T) {
	if taskNotifyEventsAny == taskNotifyEventsAll {
		t.Fatalf("taskNotifyEventsAny aliases taskNotifyEventsAll (%q) — the 'any' "+
			"synonym is part of the documented contract and must stay its own constant",
			taskNotifyEventsAll)
	}
	for _, submit := range []string{taskNotifyEventsAll, taskNotifyEventsAny} {
		t.Run(submit, func(t *testing.T) {
			// OWN PROFILE: a synthetic alarm belongs to the ledger of the profile
			// that fired it, and the delivery gate (taskNotifyDelivers) withholds
			// an event whose ledger is not the profile's own. The suite-level
			// ledger pin points OUTSIDE any profile (that is its safety
			// property), so without this the alarms are suppressed — correctly —
			// and this arm would measure the gate instead of the filter.
			taskNotifyProfileSandbox(t)
			sink := filepath.Join(t.TempDir(), "all.jsonl")
			t.Setenv(taskNotifyCmdEnv, "cat >> "+sink)
			t.Setenv(taskNotifyWebhookEnv, "")
			t.Setenv(taskNotifyEventsEnv, submit)
			t.Setenv(taskNotifyPatternEnv, "")
			for _, alarm := range taskNotifyAlarmEvents {
				NotifySyntheticEvent(alarm, "cov-sess", "cov-model", "alarm probe")
			}
			body, err := os.ReadFile(sink)
			if err != nil {
				t.Fatalf("events=%s dropped every alarm: %v", submit, err)
			}
			for _, alarm := range taskNotifyAlarmEvents {
				if !strings.Contains(string(body), `"event":"`+alarm+`"`) {
					t.Errorf("events=%s did not deliver the %q alarm — the registry entry "+
						"is not reachable by the subscriber who asked for everything", submit, alarm)
				}
			}
		})
	}
}

// TestCovToolsNotifyAllFilterEnumeratesEveryAlarm pins the mechanism itself
// (not its effect): the all/any arm sets exactly the status set plus the
// registry — so a future edit that re-introduces an inline hand-list fails
// here even if it happens to list today's alarms correctly.
func TestCovToolsNotifyAllFilterEnumeratesEveryAlarm(t *testing.T) {
	t.Setenv(taskNotifyCmdEnv, "cat > /dev/null")
	t.Setenv(taskNotifyWebhookEnv, "")
	t.Setenv(taskNotifyEventsEnv, taskNotifyEventsAll)
	cfg := loadTaskNotifyConfig()
	if !cfg.enabled() {
		t.Fatalf("notify config is not enabled — the probe cannot read the filter")
	}
	want := len(taskStatusSet) + len(taskNotifyAlarmEvents)
	if len(cfg.events) != want {
		t.Errorf("EVENTS=all enabled %d events, want %d (statuses %d + alarms %d) — "+
			"an inline hand-list has replaced the registry iteration",
			len(cfg.events), want, len(taskStatusSet), len(taskNotifyAlarmEvents))
	}
	for _, a := range taskNotifyAlarmEvents {
		if !cfg.events[a] {
			t.Errorf("EVENTS=all does not enable the registered alarm %q", a)
		}
	}
	// The alarms are NOT part of the default set: an operator who did not ask
	// for everything keeps the transition-only default.
	t.Setenv(taskNotifyEventsEnv, "")
	def := loadTaskNotifyConfig()
	for _, a := range taskNotifyAlarmEvents {
		if def.events[a] {
			t.Errorf("the alarm %q rides the DEFAULT event set — alarms fire only for an "+
				"operator who subscribed (or asked for \"all\")", a)
		}
	}
}

// TestCovToolsAlarmLiteralsHaveOneDefinitionSite enforces Rule 13/14 on the
// alarm wire names: every registered alarm's literal lives in NO non-test file
// of internal/agent — where the session alarms used to be mirrored, out of
// reach of the "all" expansion — and no notify call site passes a bare literal.
func TestCovToolsAlarmLiteralsHaveOneDefinitionSite(t *testing.T) {
	alarms := make(map[string]bool, len(taskNotifyAlarmEvents))
	for _, a := range taskNotifyAlarmEvents {
		alarms[a] = true
	}
	agentDir := filepath.Join("..", "agent")
	if files := covToolsGoFiles(agentDir); len(files) == 0 {
		t.Fatalf("internal/agent scan found no production .go file — the mirror gate "+
			"cannot read blind (%s)", agentDir)
	}
	for _, path := range covToolsGoFiles(agentDir) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil || !alarms[v] {
				return true
			}
			t.Errorf("internal/agent re-declares the alarm literal %q in %s — reference "+
				"the tools constant instead: a mirror falls out of the \"all\" expansion "+
				"(Rule 14)", v, path)
			return true
		})
	}
}
