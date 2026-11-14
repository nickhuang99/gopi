package main

// cli_memory_sync_test.go — the ROOT-CAUSE regression guard for the restored
// manual door.
//
// The defect this guards (measured 2026-11-05/06): `5677cc6e` deleted the
// cross-node sync CLI family, and nothing noticed that the ASSET TRANSPORT
// underneath was still composing `ssh <peer> gopi memory sync --serve-assets`
// — a verb no binary dispatched — while the production path had been replaced
// by a refusal. Restoring the verb in the dispatcher alone would leave the same
// class alive in a new shape: a page, a flag or a direction that the parser and
// the transport disagree about.
//
// So these tests drive the DECISIONS the CLI makes, not the tool layer (which
// has its own file, internal/tools/memory_sync_assets_test.go):
//   ① an undeclared peer is refused BEFORE any leg exists, and the refusal names
//     this node's whole whitelist (Rule 15: a refusal an operator can act on);
//   ② `--push` and `--pull` are mutually exclusive, and neither means BOTH;
//   ③ the flag table ADMITS exactly the flags the page documents — a `--pus`
//     typo must not silently run a bidirectional session, because the whole
//     point of this verb is that the operator picks a direction.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestMemorySyncDirectionResolvesTheThreeShapes pins ② and the default: the two
// one-way modes and the bidirectional one, from the same flag set the parser
// hands runMemorySync.
func TestMemorySyncDirectionResolvesTheThreeShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "neither flag is bidirectional", args: nil, want: tools.AssetSyncModeBoth},
		{name: "--push is outward only", args: []string{"--push"}, want: tools.AssetSyncModePush},
		{name: "--pull is inward only", args: []string{"--pull"}, want: tools.AssetSyncModePull},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := newMemoryFlagSet()
			for _, name := range []string{
				memorySyncPeerFlagKey, memorySyncDBFlagKey, memorySyncDryRunFlagKey,
				memorySyncPushFlagKey, memorySyncPullFlagKey, memorySyncTimeoutFlagKey,
				memorySyncServeAssetsFlagKe,
			} {
				fs.expect(name)
			}
			for _, name := range []string{
				memorySyncDryRunFlagKey, memorySyncPushFlagKey,
				memorySyncPullFlagKey, memorySyncServeAssetsFlagKe,
			} {
				fs.boolean(name)
			}
			if _, err := fs.parse(tc.args); err != nil {
				t.Fatalf("the documented spelling was REFUSED by its own parser: %v", err)
			}
			got, err := memorySyncDirection(fs)
			if err != nil {
				t.Fatalf("memorySyncDirection(%v): %v", tc.args, err)
			}
			if got != tc.want {
				t.Errorf("memorySyncDirection(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// TestMemorySyncDirectionRefusesBothFlags is the red half of ②: naming both is
// an ambiguous request, and an ambiguous request resolved to a preference is a
// run that sends data the operator did not ask to send.
func TestMemorySyncDirectionRefusesBothFlags(t *testing.T) {
	fs := newMemoryFlagSet()
	fs.expect(memorySyncPushFlagKey)
	fs.expect(memorySyncPullFlagKey)
	fs.boolean(memorySyncPushFlagKey)
	fs.boolean(memorySyncPullFlagKey)
	if _, err := fs.parse([]string{"--push", "--pull"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err := memorySyncDirection(fs)
	if err == nil {
		t.Fatal("--push together with --pull must be REFUSED; resolving it silently sends data one way the operator did not choose")
	}
	for _, want := range []string{"--push", "--pull", "mutually exclusive"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q:\n%s", want, err)
		}
	}
}

// TestMemorySyncRequireDeclaredPeerNamesTheWhitelist pins ①: the refusal carries
// the peers this node DOES declare, and a node that declares NONE says so — the
// two are different facts with different repairs (declare one, or pick one from
// the list), and rendering both as "not declared" is the silent-failure shape.
func TestMemorySyncRequireDeclaredPeerNamesTheWhitelist(t *testing.T) {
	t.Run("declared peer passes", func(t *testing.T) {
		t.Setenv(config.MemoryPeersEnv, "peerbox")
		if err := memorySyncRequireDeclaredPeer("peerbox"); err != nil {
			t.Fatalf("a declared peer was refused: %v", err)
		}
	})
	t.Run("undeclared peer is refused with the list", func(t *testing.T) {
		t.Setenv(config.MemoryPeersEnv, "peerbox,hp")
		err := memorySyncRequireDeclaredPeer("nosuchhost")
		if err == nil {
			t.Fatal("an undeclared peer must be refused before any leg exists")
		}
		for _, want := range []string{"nosuchhost", "peerbox", "hp", config.MemoryPeersSettingsKey} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal must name %q:\n%s", want, err)
			}
		}
	})
	t.Run("a node with no peers says exactly that", func(t *testing.T) {
		t.Setenv(config.MemoryPeersEnv, "")
		err := memorySyncRequireDeclaredPeer("peerbox")
		if err == nil {
			t.Fatal("with no declared peers nothing can be declared a peer")
		}
		if !strings.Contains(err.Error(), "NO memory peers") {
			t.Errorf("an empty whitelist is a DIFFERENT fact from a wrong alias and must read as one:\n%s", err)
		}
	})
}

// memorySyncRetiredEveryFlagWord is the cadence flag the old automatic face
// carried. It is a literal ON PURPOSE, the same reason
// memory_sync_address_guard_test.go's address is one: it is the SUBJECT of a
// measurement, and a constant it could be derived from no longer exists (the
// reader went in batch 5), so a test-local spelling is the only honest one.
const memorySyncRetiredEveryFlagWord = "--every"

// TestMemorySyncValidatesEveryFlagBeforeTheDryRunBranch is the regression guard
// for a defect MEASURED on this branch (2026-11-06): `--dry-run` returns early,
// so when the direction and timeout checks were placed after it, `--dry-run
// --timeout 0` exited 0 and printed a normal report while the same value
// without `--dry-run` exited 1. One flag set, two verdicts — and the lenient one
// is the rehearsal, which is exactly where an operator looks for a refusal
// before running the real thing.
//
// The order is not observable from a unit test directly (runMemorySync exits),
// so this pins the PROPERTY the fix bought: the two validators are pure and
// total — they refuse before the branch that would skip them can be blamed for
// a value they never saw.
func TestMemorySyncValidatesEveryFlagBeforeTheDryRunBranch(t *testing.T) {
	t.Run("a bad timeout is refused even with --dry-run in the argv", func(t *testing.T) {
		fs := newMemoryFlagSet()
		fs.expect(memorySyncDryRunFlagKey)
		fs.boolean(memorySyncDryRunFlagKey)
		fs.expect(memorySyncTimeoutFlagKey)
		if _, err := fs.parse([]string{"--dry-run", "--timeout", "0"}); err != nil {
			t.Fatalf("parse: %v", err)
		}
		if err := applyMemorySyncTimeout(fs); err == nil {
			t.Fatal("--timeout 0 must be refused; a rehearsal that accepts a value the real run refuses teaches the wrong thing")
		}
	})
	t.Run("both direction flags are refused even with --dry-run in the argv", func(t *testing.T) {
		fs := newMemoryFlagSet()
		for _, name := range []string{memorySyncDryRunFlagKey, memorySyncPushFlagKey, memorySyncPullFlagKey} {
			fs.expect(name)
			fs.boolean(name)
		}
		if _, err := fs.parse([]string{"--dry-run", "--push", "--pull"}); err != nil {
			t.Fatalf("parse: %v", err)
		}
		if _, err := memorySyncDirection(fs); err == nil {
			t.Fatal("--push with --pull must be refused regardless of --dry-run")
		}
	})
}

// TestMemorySyncUsageAndParserAgree is ③ at the surface level: every flag the
// page documents is admitted, and the two direction flags the page shows are the
// values the mode resolver selects (Rule 14 — the word, the mode and the page
// cannot drift into three answers).
func TestMemorySyncUsageAndParserAgree(t *testing.T) {
	for _, flag := range []string{
		memorySyncPeerFlagKey, memorySyncDBFlagKey, memorySyncDryRunFlagKey,
		memorySyncPushFlagKey, memorySyncPullFlagKey, memorySyncTimeoutFlagKey,
		memorySyncServeAssetsFlagKe,
	} {
		if !strings.Contains(memorySyncUsage, "--"+flag) {
			t.Errorf("the parser admits --%s but the page never documents it", flag)
		}
	}
	// The deprecated periodic face must NOT be advertised: it is the one part of
	// the old surface still deleted (its reader went in batch 5).
	if strings.Contains(memorySyncUsage, memorySyncRetiredEveryFlagWord) {
		t.Errorf("the page advertises %s, whose reader no longer exists — a promise the parser refuses",
			memorySyncRetiredEveryFlagWord)
	}
	// The page names the manual contract, which is the whole reason this verb was
	// restored as a one-shot rather than with its timer.
	if !strings.Contains(memorySyncUsage, "MANUAL BY DESIGN") {
		t.Error("the page must state that the door is manual: no timer, no store trigger, one session per invocation")
	}
}
