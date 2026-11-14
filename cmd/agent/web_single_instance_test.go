package main

// web_single_instance_test.go — ONE `gopi web` server per profile, and the
// distinction the operator's ruling turned on: the SERVER is exclusive, the
// BROWSERS are not.
//
// The property is a kernel flock, so it is proved by taking it twice — once
// through the seam under test and once through the SAME `internal/runlock` a
// second server would use — rather than by asserting a string.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/runlock"
)

// TestWebServerLockIsOnePerProfile — the second server is refused, naming the
// lock, and the FIRST one's release is what admits it.
func TestWebServerLockIsOnePerProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)

	release, err := webTakeServerLock()
	if err != nil {
		t.Fatalf("the first server was refused: %v", err)
	}
	if release == nil {
		t.Fatalf("the first server got a nil release — its lock would never be freed")
	}

	// A second server, through the seam under test: it MUST be refused.
	if _, secondErr := webTakeServerLock(); secondErr == nil {
		t.Fatalf("a SECOND `gopi web` on the same profile was admitted — two servers would execute the same conversation twice")
	}

	// And the refusal is a REAL kernel lock, not a bookkeeping flag: the same
	// directory refuses `runlock.Lock`, which is what any other holder would
	// call. This is the assertion that separates "one server" from "one
	// variable".
	lockDir := webServerLockDir(config.ProfileDirIn(home))
	if _, heldErr := runlock.Lock(lockDir); heldErr == nil {
		t.Fatalf("the server lock directory %s was lockable by a second holder — the exclusion is not a lock", lockDir)
	}

	release()
	// After the release the slot is free again: a stop-then-start cycle works,
	// which is what an operator restarting their server needs.
	release2, err := webTakeServerLock()
	if err != nil {
		t.Fatalf("after releasing, a new server was still refused: %v", err)
	}
	release2()
}

// TestWebServerLockIsPerProfileNotPerAddress — two profiles are two servers.
// The operator's criterion is the PROCESS, explicitly NOT the address/port
// ("even address/port combination is considered single instance"), so the lock
// must not be keyed on anything the address carries.
func TestWebServerLockIsPerProfileNotPerAddress(t *testing.T) {
	homeA := t.TempDir()
	t.Setenv(config.GopiDirEnv, homeA)
	releaseA, err := webTakeServerLock()
	if err != nil {
		t.Fatalf("profile A was refused: %v", err)
	}
	defer releaseA()

	homeB := t.TempDir()
	t.Setenv(config.GopiDirEnv, homeB)
	releaseB, err := webTakeServerLock()
	if err != nil {
		t.Fatalf("profile B was refused because profile A runs a server: %v — the criterion is the process per profile, not the host", err)
	}
	releaseB()
}

// TestWebServerLockBusyRefusalNamesItsCause — a refusal an operator cannot act
// on is the silence this package exists to end (Rule 15). It names the lock
// path, the holder the kernel recorded, and the live door (a browser tab).
func TestWebServerLockBusyRefusalNamesItsCause(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	release, err := webTakeServerLock()
	if err != nil {
		t.Fatalf("the first server was refused: %v", err)
	}
	defer release()

	_, secondErr := webTakeServerLock()
	if secondErr == nil {
		t.Fatalf("no refusal to inspect")
	}
	// The RETURNED error is the kernel's own refusal (the held lock, named):
	// that is the fact the caller branches on, so it must carry the path.
	if !strings.Contains(secondErr.Error(), runlock.LockFileName) {
		t.Fatalf("the refusal does not name the lock it could not take:\n%s", secondErr.Error())
	}
	// The OPERATOR-facing half is the stderr block the seam prints; it is
	// asserted through the ONE format constant that renders it, because the
	// sentence an operator acts on is the sentence that must carry the live
	// door (a browser tab on the running server, never a second attempt).
	// The prog name rides the format's own %s verb (rendered by runWeb), so it
	// is checked as the VERB plus the sentence, not as a literal in the body.
	for _, want := range []string{"%s: another", "one server at a time", "BROWSER TAB"} {
		if !strings.Contains(webServerLockBusyFormat, want) {
			t.Fatalf("the busy refusal does not name %q — an operator cannot act on it:\n%s", want, webServerLockBusyFormat)
		}
	}
}
