package replaydrive

// port_wording_test.go — the anti-regression guard for the stub's PORT CLAIM.
//
// The standard is devdoc/replay-concurrency-and-port-semantics.md §1, measured on
// this host: the socket is loopback-only and the kernel assigns the port, so it is
// unreachable from another HOST — but it is NOT private, because any process on
// this machine that learns the port is answered (two independent clients did, each
// getting `HTTP/1.0 200 OK`). The stronger sentence is the one this test refuses,
// and it is refused because it is FALSE, not because it is impolite: a reader who
// believes it would think a recording's contents are confined to this run.
//
// A TEXT gate rather than a behavioural one, deliberately: "no other process can
// reach it" is a claim about other processes, and testing it would mean making one
// — a load-shaped, environment-dependent proof for a sentence that simply must not
// be written. The files scanned are the ones that OWN the wording: this package and
// the verb that starts the stub.

import (
	"os"
	"strings"
	"testing"
)

// bannedPortClaims are the phrases the doc bans. Each is a promise the socket does
// not make.
var bannedPortClaims = []string{
	"no other process can reach it",
	"no other process can connect",
	"nothing else can connect",
	"别的进程侦听不到",
	"别的进程连不上",
	"private channel",
}

// wordingSources are the files that own the stub's port claim, relative to this
// package's directory.
var wordingSources = []string{
	"replaydrive.go",
	"../../cmd/agent/chat_replay.go",
}

// negationMarkers are the words that turn a banned phrase into a DENIAL of it.
// MENTION IS NOT PRESCRIPTION (the same distinction Rule 25 draws for a privilege):
// the standard's own text has to be able to say "it is NOT a private channel" —
// that sentence is the correction — while a line that PROMISES privacy is the
// defect. The exemption is per LINE, so a denial on one line cannot launder a
// promise on another.
var negationMarkers = []string{"not ", "isn't", "never ", "false", "banned", "do not write", "must not", "不是", "不得"}

// bannedClaimLines returns the lines of text that carry a banned claim WITHOUT
// denying it. Separated from the file walk so the red proof below can drive it
// with synthetic lines.
func bannedClaimLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		low := strings.ToLower(line)
		if !hasNegation(low) {
			for _, claim := range bannedPortClaims {
				if strings.Contains(low, strings.ToLower(claim)) {
					out = append(out, line)
					break
				}
			}
		}
	}
	return out
}

// hasNegation reports whether the line denies a claim rather than making it.
func hasNegation(lowLine string) bool {
	for _, m := range negationMarkers {
		if strings.Contains(lowLine, m) {
			return true
		}
	}
	return false
}

// TestNoSourceClaimsTheStubPortIsPrivate keeps the banned sentence out of the code
// that describes the listener.
func TestNoSourceClaimsTheStubPortIsPrivate(t *testing.T) {
	for _, rel := range wordingSources {
		raw, err := os.ReadFile(rel) //stylegate:ignore a source file this test scans for wording is small and read whole on purpose
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for _, line := range bannedClaimLines(string(raw)) {
			t.Errorf("%s carries the banned port claim on a line that does not deny it:\n  %s\nthe socket is loopback-only and kernel-assigned (off-HOST unreachable), but a process on this machine that knows the port IS answered; see devdoc/replay-concurrency-and-port-semantics.md §1",
				rel, strings.TrimSpace(line))
		}
	}
}

// TestBannedClaimLinesSeparatesMentionFromPromise is the red proof: a line that
// PROMISES privacy is caught, the same phrase DENIED is not — including the two
// phrasings this repository actually uses to deny it.
func TestBannedClaimLinesSeparatesMentionFromPromise(t *testing.T) {
	promise := "// the port is isolated: no other process can reach it"
	if got := bannedClaimLines(promise); len(got) != 1 {
		t.Fatalf("the promise was not caught: %v", got)
	}
	for _, denial := range []string{
		"// it is NOT a private channel: any process that knows the port is answered",
		"// do not write that no other process can reach it — it was measured FALSE",
		"// 别的进程侦听不到 不是事实，不得这样写",
	} {
		if got := bannedClaimLines(denial); len(got) != 0 {
			t.Fatalf("a denial was flagged as the defect: %q -> %v", denial, got)
		}
	}
	// The exemption is per LINE: a denial on one line must not launder the next.
	if got := bannedClaimLines("// this is not a promise\n// no other process can reach it"); len(got) != 1 {
		t.Fatalf("a promise on its own line was laundered by the previous line's denial: %v", got)
	}
}
