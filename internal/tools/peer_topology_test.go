package tools

// peer_topology_test.go — the DECLARED-TOPOLOGY gate's own tests, together
// with the regression test for the bug that motivated it:
//
//	`gopi task diff --peer <undeclared alias>` used to run the read-only
//	catalog query over ssh and report WHAT SSH SAID
//	("diff: ssh hp: exit status 2") instead of naming the actual condition —
//	the alias is not part of the declared topology. Its siblings
//	(`task sync --peer`, `task sync --peers`, `task sync --push-only`) all
//	refuse before any contact.
//
// The three properties locked here:
//
//	① FIRST REFUSAL: an undeclared alias is refused BEFORE the transport is
//	   entered (the hook seam records whether the ssh leg ran at all);
//	② ONE WORDING: the refusal names the alias, the settings.json key and
//	   the env override, and the clause comes from its single definition
//	   (DeclaredPeerGate) — a literal gate fails any production file that
//	   re-types the sentence;
//	③ NO OVER-REFUSAL: a DECLARED alias still reaches the transport with the
//	   alias it was given (the gate is not a blanket refusal).

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// declaredGatePeer is the peer the sandbox topology declares.
const declaredGatePeer = "peer-declared"

// undeclaredGatePeer is an alias the sandbox topology does NOT declare (the
// defect's shape: well-formed, resolvable-looking, never declared).
const undeclaredGatePeer = "peer-undeclared"

// gateSandbox declares a topology containing `declaredGatePeer` only.
func gateSandbox(t *testing.T) {
	t.Helper()
	covSessionSandbox(t, "node-gate", declaredGatePeer)
}

// gateTransportProbe installs a recording transport seam and returns a
// pointer to the "did it run" flag plus the alias it was handed.
func gateTransportProbe(t *testing.T) (ran *bool, seen *string) {
	t.Helper()
	wasRun, alias := false, ""
	old := reconRemoteRowSetsHook
	reconRemoteRowSetsHook = func(peer string) ([]reconRowSet, error) {
		wasRun, alias = true, peer
		return []reconRowSet{}, nil
	}
	t.Cleanup(func() { reconRemoteRowSetsHook = old })
	return &wasRun, &alias
}

// TestTaskDiffPeerRefusesUndeclaredAliasBeforeTransport is THE regression test
// for the reported defect: `task diff --peer <undeclared>` must refuse with the
// declared-topology message and must not enter the transport. RED on the
// un-fixed tree: the diff proceeded to the ssh leg and reported its exit code
// ("diff: ssh peer-undeclared: exit status 255"/"2" depending on the host),
// which told the operator nothing about the real condition.
func TestTaskDiffPeerRefusesUndeclaredAliasBeforeTransport(t *testing.T) {
	newDeltaTestDBClient(t)
	gateSandbox(t)
	ran, seen := gateTransportProbe(t)

	stats, err := TaskDiffPeer(undeclaredGatePeer)
	if err == nil {
		t.Fatalf("an undeclared alias must be refused, got %+v", stats)
	}
	if stats != nil {
		t.Fatalf("refused diff must return nil stats, got %+v", stats)
	}
	if *ran {
		t.Fatalf("the transport ran for an undeclared alias (handed %q) — the whitelist gate must come first: %v", *seen, err)
	}
	msg := err.Error()
	for _, want := range []string{
		"diff: ", // the verb's own phase prefix survives
		undeclaredGatePeer,
		config.SyncPeersSettingsKey,
		config.SyncPeersEnv,
		DeclaredPeerGate,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal must carry %q (the operator needs the alias, the key, the env override and the rule), got: %v", want, err)
		}
	}
	// The defect's SIGNATURE: the message named ssh's exit code, i.e. the
	// failure was reported by the transport instead of by the contract.
	if strings.Contains(msg, "ssh") {
		t.Errorf("refusal must not report the transport's own error — the condition is the topology, not the connection: %v", err)
	}
}

// TestTaskDiffPeerDeclaredAliasStillReachesTransport is the other half of the
// gate: a DECLARED alias is not refused, and the transport receives exactly
// the alias the operator typed (no over-refusal, no alias rewriting).
func TestTaskDiffPeerDeclaredAliasStillReachesTransport(t *testing.T) {
	newDeltaTestDBClient(t)
	gateSandbox(t)
	ran, seen := gateTransportProbe(t)

	stats, err := TaskDiffPeer(declaredGatePeer)
	if err != nil {
		t.Fatalf("a declared alias must pass the gate, got: %v", err)
	}
	if !*ran {
		t.Fatal("a declared alias never reached the transport — the gate refuses too much")
	}
	if *seen != declaredGatePeer {
		t.Fatalf("transport received %q, want %q", *seen, declaredGatePeer)
	}
	if stats == nil || stats.Peer != declaredGatePeer {
		t.Fatalf("diff stats must be about the declared peer: %+v", stats)
	}
}

// TestRequireDeclaredPeerNamesTheDeclarationSources pins the gate's contract
// (Rule 15: an unreadable topology is an ERROR, never a silent "undeclared").
func TestRequireDeclaredPeerNamesTheDeclarationSources(t *testing.T) {
	gateSandbox(t)

	if err := RequireDeclaredPeer(declaredGatePeer, nil); err != nil {
		t.Errorf("declared peer refused: %v", err)
	}
	err := RequireDeclaredPeer(undeclaredGatePeer, nil)
	if err == nil {
		t.Fatal("undeclared peer accepted")
	}
	for _, want := range []string{undeclaredGatePeer, config.SyncPeersSettingsKey, config.SyncPeersEnv, DeclaredPeerGate} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal missing %q: %v", want, err)
		}
	}
	// A caller-supplied topology (--peers) outranks the declared one — the
	// session verbs' long-standing precedence.
	if err := RequireDeclaredPeer(undeclaredGatePeer, []string{undeclaredGatePeer}); err != nil {
		t.Errorf("a run-scoped declaration must license the peer: %v", err)
	}
	if ok, perr := PeerDeclared(declaredGatePeer, []string{undeclaredGatePeer}); perr != nil || ok {
		t.Errorf("PeerDeclared must answer from the run-scoped list when one is given: ok=%v err=%v", ok, perr)
	}
}

// legacyWhitelistWording is the refusal SHAPE every face used to spell for
// itself (in three slightly different versions). It may live ONLY in
// peer_topology.go now: that file owns the sentence, and a face that re-types
// it has forked the contract again.
var legacyWhitelistWording = regexp.MustCompile(`not in the [^"]{0,24}whitelist`)

// TestDeclaredTopologyWordingHasOneDefinition is the Rule 13 literal gate: no
// production file in internal/tools may spell the whitelist refusal for itself.
func TestDeclaredTopologyWordingHasOneDefinition(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	const definitionFile = "peer_topology.go"
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, rerr := os.ReadFile(filepath.Join(".", name))
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		scanned++
		hit := legacyWhitelistWording.Find(body)
		if name == definitionFile {
			if hit == nil {
				t.Errorf("%s no longer carries the canonical refusal (%s) — the gate's definition site moved or the wording drifted", definitionFile, legacyWhitelistWording)
			}
			if !strings.Contains(string(body), "DeclaredPeerGate") {
				t.Errorf("%s must reference the shared clause constant", definitionFile)
			}
			continue
		}
		if hit != nil {
			t.Errorf("%s re-types the whitelist refusal (%q) — reference RequireDeclaredPeer / DeclaredPeerGate in %s instead", name, hit, definitionFile)
		}
	}
	if scanned == 0 {
		t.Fatal("the literal gate scanned no production files — the gate is inert")
	}
}

// TestHarvestRefusesUndeclaredAliasBeforeTransport is THE regression test for
// the sibling of the diff defect, measured 2026-09-23 on master:
//
//	$ gopi task harvest --peer not-declared      # sync_peers == ["a-peer"]
//	gopi task harvest error: harvest: ssh not-declared: exit status 2
//	[exit=1, the recording ssh shim recorded ONE leg]
//
// Harvest is the family's most expensive face: it WRITES the peer's rows into
// this ledger in one transaction and then pushes every local branch back — so
// a topology typo ran a real pull against a host nobody declared. The refusal
// must come from the contract (RequireDeclaredPeer), name the alias, the
// settings key, the env override and the shared clause, must not mention ssh,
// and must be reached before the transport seam AND before the ledger is
// opened.
func TestHarvestRefusesUndeclaredAliasBeforeTransport(t *testing.T) {
	dbPath := harvestTestDB(t)
	gateSandbox(t)

	ran := false
	opts := HarvestOptions{
		Peer: undeclaredGatePeer,
		Fetch: func(peer string, anchor float64) ([]harvestRemoteRow, error) {
			ran = true
			return nil, nil
		},
	}
	counts, err := TaskHarvest(opts)
	if err == nil {
		t.Fatalf("an undeclared alias must be refused, got %+v", counts)
	}
	if counts != nil {
		t.Fatalf("refused harvest must return nil counts, got %+v", counts)
	}
	if ran {
		t.Fatal("the transport ran for an undeclared alias — the whitelist gate must come first")
	}
	msg := err.Error()
	for _, want := range []string{
		"harvest: ", // the verb's own phase prefix survives
		undeclaredGatePeer,
		config.SyncPeersSettingsKey,
		config.SyncPeersEnv,
		DeclaredPeerGate,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal must carry %q (alias, key, env override, clause), got: %v", want, err)
		}
	}
	// The defect's SIGNATURE: the failure was reported by the transport.
	if strings.Contains(msg, "ssh") {
		t.Errorf("refusal must not report the transport's own error — the condition is the topology: %v", err)
	}
	// The gate precedes the LOCAL WRITE too: no ledger may have been created
	// at the pinned path by a refused pull.
	if _, serr := os.Stat(dbPath); serr == nil {
		t.Errorf("a refused harvest opened the ledger at %s — the gate must precede the DB open (harvest writes)", dbPath)
	}
}

// TestHarvestDeclaredAliasStillReachesTransport is the other half: the gate
// must not become a blanket refusal of the pull path.
func TestHarvestDeclaredAliasStillReachesTransport(t *testing.T) {
	harvestTestDB(t)
	gateSandbox(t)

	wasRun, alias := false, ""
	counts, err := TaskHarvest(HarvestOptions{
		Peer: declaredGatePeer,
		Fetch: func(peer string, anchor float64) ([]harvestRemoteRow, error) {
			wasRun, alias = true, peer
			return []harvestRemoteRow{}, nil
		},
	})
	if err != nil {
		t.Fatalf("a declared alias must not be refused: %v", err)
	}
	if !wasRun {
		t.Fatal("a declared alias never reached the transport — the gate refuses too much")
	}
	if alias != declaredGatePeer {
		t.Fatalf("transport received %q, want %q", alias, declaredGatePeer)
	}
	if counts == nil {
		t.Fatal("a permitted harvest must report counts")
	}
}

// bareHarvestPeerLiteral matches a TaskHarvest call whose Peer is typed as a
// string literal instead of referenced from a constant:
//
//	TaskHarvest(HarvestOptions{Peer: SOME_LITERAL, …})   ← the shape this gate bans
//
// Scope is deliberately narrow (Rule 1b/13 in the form that actually bites
// here): `"hp"` is a generic fixture alias all over this suite — unrelated
// session/delta/reflow tests use it for their own peers — so a package-wide
// literal ban would flag a hundred honest fixtures. What must not be typed
// inline is the peer of a HARVEST call, because every harvest fixture has to
// DECLARE that alias (TaskHarvest gates on the topology before the transport):
// a literal there is one edit away from a test that exercises the GATE while
// its name promises the pull.
var bareHarvestPeerLiteral = regexp.MustCompile(`(?s)TaskHarvest\((?:[^()]|\([^()]*\)){0,160}?Peer:\s*"`)

// TestHarvestCallSitesReferenceAPeerConstant is that gate: every
// TaskHarvest(...) call site in this package's tests names its peer through a
// constant (harvestFixturePeer / gitPushbackTestPeer / …), so the declared
// topology and the pulled-from alias cannot drift apart.
func TestHarvestCallSitesReferenceAPeerConstant(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	scanned, calls := 0, 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, rerr := os.ReadFile(filepath.Join(".", name))
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		scanned++
		calls += strings.Count(string(body), "TaskHarvest(")
		if hit := bareHarvestPeerLiteral.Find(body); hit != nil {
			t.Errorf("%s types a TaskHarvest peer as a bare string literal (%q) — reference a declared-peer constant instead (harvestFixturePeer / harvestDeclarePeer)", name, hit)
		}
	}
	if scanned == 0 || calls == 0 {
		t.Fatalf("the gate scanned %d test files and found %d TaskHarvest call sites — it is inert", scanned, calls)
	}
}
