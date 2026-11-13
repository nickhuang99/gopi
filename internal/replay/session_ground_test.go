package replay

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// session_ground_test.go — the regression guard for the ONE property the film
// directory must have: TWO RECORDINGS STARTED IN THE SAME SECOND NEVER SHARE A
// DIRECTORY.
//
// WHY THIS TEST EXISTS (measured 2026-10-03). FilmDir names its directory by a
// SECOND-resolution UTC stamp, so the stamp alone is not an identifier: two runs
// that start inside the same second compute the SAME name. The implementation
// answers that with a "-N" suffix (the (a) arm — disambiguate, never overwrite)
// and it is race-tolerant by construction: a stat followed by a mkdir whose
// EEXIST is another `continue`, so a competitor that wins between the two is
// stepped over rather than written into. The property was true and UNTESTED —
// that shell driver pinned only the SEQUENTIAL case (C5: two
// resolutions of one session name two different directories, seconds apart),
// which passes even against an implementation that would collide under load.
//
// SO THIS TEST DRIVES THE RACE, not the clock: N goroutines released by one
// closed channel call FilmDir with the SAME `now`. A sequential test cannot
// reach this window, which is exactly why the same-second collision could have
// shipped unnoticed.
//
// The assertion is on DIRECTORIES THAT REALLY EXIST-ON-DISK, not on returned
// strings: a name that was returned but never created would satisfy "distinct"
// and still produce a film whose frames land nowhere.

// sameSecondRuns is the concurrency the test drives. It stays far below
// stampAttempts: the failures below are reported INDEPENDENTLY, so a broken
// implementation shows WHICH property broke — a call that ran out of suffix
// attempts and a call that returned a duplicate NAME are different diagnoses
// and the reader must not have to infer one from the other's message.
const sameSecondRuns = 32

func TestFilmDirGivesEverySameSecondRunItsOwnDirectory(t *testing.T) {
	g := SessionGround{Dir: t.TempDir()}
	// ONE timestamp for every caller: the collision the test is about.
	now := time.Date(2026, 10, 3, 14, 10, 25, 0, time.UTC)

	var start sync.WaitGroup
	var done sync.WaitGroup
	gate := make(chan struct{})
	dirs := make([]string, sameSecondRuns)
	errs := make([]error, sameSecondRuns)

	for i := 0; i < sameSecondRuns; i++ {
		start.Add(1)
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Done()
			<-gate // one release: every goroutine enters FilmDir together
			dirs[i], errs[i] = g.FilmDir(now)
		}(i)
	}
	start.Wait()
	close(gate)
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("FilmDir(call %d) failed: %v", i, err)
		}
	}

	// 1. Every caller got a name of its own. This assertion runs FIRST and
	//    independently of the errors above: the property under test is
	//    DISTINCTNESS, and an implementation that collides must be reported as
	//    a collision even if it also happened to fail some other way.
	seen := make(map[string]int, sameSecondRuns)
	for i, dir := range dirs {
		if dir == "" { // the call that failed above: not a name, not a collision
			continue
		}
		if prev, dup := seen[dir]; dup {
			t.Errorf("calls %d and %d share the film directory %q — two recordings of one second would overwrite each other's replay.json and GIF", prev, i, dir)
		}
		seen[dir] = i
	}

	// 2. Every name is a directory that is REALLY THERE — the returned string is
	//    a claim; this is the state it claims.
	wantDir := filepath.Join(g.ArtifactsDir(), replayDirName)
	stamp := now.Format(stampLayout)
	for i, dir := range dirs {
		if dir == "" { // the call that failed above: nothing to check
			continue
		}
		st, err := os.Stat(dir)
		if err != nil {
			t.Errorf("call %d returned %s, which was never created: %v", i, dir, err)
			continue
		}
		if !st.IsDir() {
			t.Errorf("call %d returned %s, which is not a directory", i, dir)
		}
		if filepath.Dir(dir) != wantDir {
			t.Errorf("call %d returned %s, outside the session's own artifacts area %s", i, dir, wantDir)
		}
		// A name that does not START with the stamp is not this second's film.
		if base := filepath.Base(dir); len(base) < len(stamp) || base[:len(stamp)] != stamp {
			t.Errorf("call %d returned %s, which does not carry the stamp %s", i, dir, stamp)
		}
	}

	// 3. Exactly ONE film keeps the bare stamp (no suffix): the suffix is the
	//    disambiguator, so it must not be handed out when the name is free. This
	//    is what makes the FIRST film of a second the one a human can find by
	//    stamp alone.
	bare := 0
	for _, dir := range dirs {
		if filepath.Base(dir) == stamp {
			bare++
		}
	}
	if bare != 1 {
		t.Errorf("%d film directories carry the bare stamp %s, want exactly 1", bare, stamp)
	}

	// 4. The caller's next film in the SAME second still gets a NEW directory —
	//    "never returns a directory that exists", stated as a property rather
	//    than as the naming rule above.
	fresh, err := g.FilmDir(now)
	if err != nil {
		t.Fatalf("a further FilmDir(now) failed: %v", err)
	}
	if _, taken := seen[fresh]; taken {
		t.Errorf("a further FilmDir(now) returned an already-used directory %s", fresh)
	}
	if st, err := os.Stat(fresh); err != nil || !st.IsDir() {
		t.Errorf("the further FilmDir(now) returned %s, which was not created (err=%v)", fresh, err)
	}
}
