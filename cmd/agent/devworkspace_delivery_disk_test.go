// The disk-exhaustion kind at the BRIEFING surface: what the next agent round
// actually reads. The unit tests in internal/devworkspace pin the class and the
// report; this file pins the two things the round is told, because "fix the
// compile error" was the sentence that cost this host round after round.
package main

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/devworkspace"
)

// diskBlocker is the blocker a clone-channel run gets on a full filesystem: the
// linker's own text, the measured ground, and the marker from the shared
// vocabulary.
func diskBlocker() *devworkspace.Blocker {
	return &devworkspace.Blocker{
		Kind:   devworkspace.BlockerKindDiskExhaustion,
		Marker: "no space left on device",
		Ground: "/home/user/workspace/01a18f47-49a7-7f34-b1db-20c5a76dc3c3",
		Words: "link: writing /home/user/workspace/x/a.out: no space left on device | " +
			"make: *** [Makefile:42: build] Error 1",
	}
}

// TestDiskExhaustionRetryBriefingNamesTheFilesystemNotTheCode is the acceptance
// arm: the briefed round must be told WHICH fact it is looking at, must be
// handed the reclaim command, and must be told not to hunt for a code defect.
func TestDiskExhaustionRetryBriefingNamesTheFilesystemNotTheCode(t *testing.T) {
	blk := diskBlocker()
	briefing := deliveryRetryBriefing(blk, blk.Ground, 1)

	for _, want := range []string{
		"不是代码",                                // the fact, in the reader's language
		"gopi tool scratch-gc gocache --trim", // the copy-pasteable reclaim command
		"不要去找代码里的缺陷",                          // the wrong next move, refused
		"没有链接成功",                              // why nothing about the branch was measured
		blk.Ground,                            // where
	} {
		if !strings.Contains(briefing, want) {
			t.Fatalf("the disk kind's retry briefing must carry %q:\n%s", want, briefing)
		}
	}
	// AND THE LEDGER SUMMARY — the first 200 runes — must carry the DECISIVE
	// line (the marker), not just the preamble: that is what the row reader sees.
	summary := agent.AutoTaskSummaryLine(briefing)
	if !strings.Contains(summary, "no space left on device") {
		t.Fatalf("the 200-rune ledger summary must carry the filesystem phrase, got %q", summary)
	}
	if !strings.Contains(summary, string(devworkspace.BlockerKindDiskExhaustion)) {
		t.Fatalf("the summary must name the kind=%q, got %q", devworkspace.BlockerKindDiskExhaustion, summary)
	}
}

// TestDiskExhaustionIsStillARefusedDelivery is the anti-widening half: a full
// filesystem is not a code failure, but it is also not a delivery. The loop must
// still refuse, and it must not borrow the nothing-to-deliver classification.
func TestDiskExhaustionIsStillARefusedDelivery(t *testing.T) {
	f := newFakeLoop(t, nil, diskBlocker(), nil)
	_, err := f.state.finish(&f.log)
	if err == nil {
		t.Fatal("the disk kind is a refused delivery and must fail")
	}
	if strings.Contains(err.Error(), "nothing to deliver") {
		t.Fatalf("the disk kind must not be classified nothing-to-deliver: %v", err)
	}
	if !strings.Contains(err.Error(), "delivery did not land") {
		t.Fatalf("the disk kind must carry the non-delivery sentence: %v", err)
	}
}
