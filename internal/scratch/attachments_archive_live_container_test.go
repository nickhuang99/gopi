// attachments_archive_live_container_test.go — the KEEP the candidate-only veto
// could not see: a LIVE run's own `attachments/`.
//
// THE DEFECT THIS PINS (measured 2026-10-16, this host, task
// 01a142630844745eb09a754876bcec9a). `attachmentsArchiveCollectable` asks about
// the CANDIDATE — `heldIn(<container>/attachments)` — and a live run does not
// stand in its archive: it stands in its CONTAINER (`cd $S`, the shape every
// evidence-writing bash call uses — Rule 37). Measured against the real sweep on
// a sandbox profile, with a live process whose cwd was the container and whose
// container carried `metadata.json`: an EMPTY, aged `<container>/attachments`
// came back `would-remove tree=session-attachments`. The sweep's own environment
// hands every run that shape, so the run cannot avoid it.
//
// SCOPE, measured in the same probe and pinned by the second test: an archive
// HOLDING BYTES is refused before the holder question is asked, so the exposure
// is exactly the empty-archive case — which is also 99.7% of the family this arm
// exists to collect (75,602 of 75,851 empty, measured 2026-10-02).
package scratch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fixtureLiveRunContainer makes one session container that carries a live run's
// own record (`*.jsonl`) and an `attachments/` area, and returns both paths.
func fixtureLiveRunContainer(t *testing.T, sessions, container string) (containerPath, archive string) {
	t.Helper()
	archive = fixtureArchiveContainer(t, sessions, container)
	containerPath = filepath.Join(sessions, container)
	if err := os.WriteFile(filepath.Join(containerPath, scratchDirFixtureRecord), []byte("{}\n"), FilePerm); err != nil {
		t.Fatalf("write the run's record: %v", err)
	}
	return containerPath, archive
}

// withHolderIndex runs fn with the tree's /proc holder pass replaced by index —
// the red-proof seam this package already uses for the evidence verdict. A
// holder is a LIVE PROCESS, which a test cannot schedule deterministically, so
// the alternative is a sleep to tune and a test that flakes; the pass itself is
// exercised by the arm's own tests, and what is under test here is the DECISION
// built on the index it returns.
func withHolderIndex(t *testing.T, index map[string]struct{}, fn func()) {
	t.Helper()
	prev := attachmentsArchiveHeldIndex
	attachmentsArchiveHeldIndex = func(string) map[string]struct{} { return index }
	t.Cleanup(func() { attachmentsArchiveHeldIndex = prev })
	fn()
}

// planFor drives the REAL plan entry point for one sessions root.
func planFor(t *testing.T, root string) map[string]bool {
	t.Helper()
	plan, err := AttachmentsArchivePlan(root, time.Now(), SessionAttachmentsStaleAge)
	if err != nil {
		t.Fatalf("plan %s: %v", root, err)
	}
	planned := map[string]bool{}
	for _, e := range plan {
		planned[e.Path] = true
	}
	return planned
}

// TestAttachmentsArchiveKeepsALiveRunsArchive is the regression guard: the
// archive of a container a live process stands in is not a candidate, while the
// same fixture with no holder still is — or "nothing was collected" would also be
// satisfied by a sweep that collects nothing.
func TestAttachmentsArchiveKeepsALiveRunsArchive(t *testing.T) {
	sessions := fixtureSessionsTree(t)
	liveContainer, liveArchive := fixtureLiveRunContainer(t, sessions, "0a000000-0000-7000-8000-0000000000a1")
	deadArchive := fixtureArchiveContainer(t, sessions, "0b000000-0000-7000-8000-0000000000b2")

	// The holder shape the sweep's own /proc pass reports for a run working in its
	// session directory: the CONTAINER is the indexed path — not the archive.
	heldContainer := heldSet(HolderScan{Paths: []string{liveContainer}}, sessions)
	if len(heldContainer) == 0 {
		t.Fatal("the fixture's holder index is empty, so the two cases below cannot be about a holder")
	}

	var planned map[string]bool
	withHolderIndex(t, heldContainer, func() { planned = planFor(t, sessions) })

	if planned[liveArchive] {
		t.Errorf("the archive %s was planned while a live process's cwd IS its container: the run's own attachments were judged by a question about the archive, not about the run", liveArchive)
	}
	if !planned[deadArchive] {
		t.Fatalf("the CONTROL — the same fixture with NO holder — was refused: the arm must still collect an ordinary empty archive, or the assertion above is satisfied by a sweep that lists nothing at all")
	}
}

// TestAttachmentsArchiveHolderQuestionRedProof is the RED direction. The sweep's
// real /proc pass indexes each holder's path AND its ancestors (`heldSet`), so a
// holder whose cwd is the container yields `{container, sessions-root}` and
// NOTHING about the archive — and on that index the PRE-FIX question
// (`heldIn(<container>/attachments)`) is blind, while the container question is
// not. Then, at sweep level: with an index that cannot see the holder at all, the
// live run's archive IS planned — so the KEEP in the test above is the container
// veto at work and not the age rule, the contents rule or a sweep that lists
// nothing.
func TestAttachmentsArchiveHolderQuestionRedProof(t *testing.T) {
	sessions := fixtureSessionsTree(t)
	liveContainer, liveArchive := fixtureLiveRunContainer(t, sessions, "0c000000-0000-7000-8000-0000000000c3")

	// Exactly what the real single pass yields for a process whose cwd is the
	// container: the container and its ancestors, and nothing below it.
	containerWise := heldSet(HolderScan{Paths: []string{liveContainer}}, sessions)
	if heldIn(liveContainer, containerWise) != true {
		t.Fatalf("the fixture's index does not even carry the container: %v", containerWise)
	}
	if heldIn(liveArchive, containerWise) {
		t.Fatalf("the pre-fix question must be BLIND here, or this proof describes a shape the sweep never produces (index=%v)", containerWise)
	}
	if !heldByAncestor(liveArchive, liveContainer, containerWise) {
		t.Fatalf("the container question must see the holder the candidate question misses (index=%v)", containerWise)
	}

	var planned map[string]bool
	withHolderIndex(t, heldSet(HolderScan{}, sessions), func() { planned = planFor(t, sessions) })
	if !planned[liveArchive] {
		t.Fatalf("with an index that cannot see the holder the archive was still refused: the sweep-level red direction cannot be produced, so the KEEP above proves nothing about the veto")
	}
}

// TestAttachmentsArchiveStillRefusesAnArchiveHoldingBytes pins the scope: the
// container veto is about a HOLDER. An archive holding evidence is refused on its
// own account, and an empty unheld archive is still collectable — the arm has not
// been turned into a fence around the whole tree.
func TestAttachmentsArchiveStillRefusesAnArchiveHoldingBytes(t *testing.T) {
	sessions := fixtureSessionsTree(t)
	withBytes := fixtureArchiveContainer(t, sessions, "0d000000-0000-7000-8000-0000000000d4", "report.md")
	empty := fixtureArchiveContainer(t, sessions, "0e000000-0000-7000-8000-0000000000e5")

	var planned map[string]bool
	withHolderIndex(t, heldSet(HolderScan{}, sessions), func() { planned = planFor(t, sessions) })

	if planned[withBytes] {
		t.Errorf("an archive HOLDING BYTES was planned: evidence is not collectable at any age")
	}
	if !planned[empty] {
		t.Errorf("an EMPTY unheld archive was refused: the arm collects empty archives and must keep doing so")
	}
}
