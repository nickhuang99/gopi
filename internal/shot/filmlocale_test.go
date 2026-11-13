package shot

import (
	"os"
	"strings"
	"sync"
	"testing"
)

// filmlocale_test.go — the REGRESSION for the filmed child's locale pin.
//
// The defect these tests exist to make unrepresentable was measured, not
// imagined. A film's Chinese was fixed twice over, and the second half was the
// one that hid behind the first:
//
//	2026-10-03  the DEFAULT TERMINAL was xterm, whose core bitmap font carries no
//	            CJK glyph — every Chinese cell filmed as □ (01a10370 fixed that by
//	            moving the default to xfce4-terminal).
//	2026-10-04  the filmed CHILD inherited the CALLER's locale. The terminal
//	            decodes its pty stream in the codeset its locale names, so under
//	            LANG=C / LC_ALL=C — the ambient state of this host — VTE advances
//	            ONE CELL PER BYTE and the same line measures ≈15.41 cell-widths
//	            instead of ≈11.17. A correct terminal, a correct font, and a film
//	            of a layout the product never drew.
//
// So the pin is not a preference and these are not style assertions: each test
// below pins one half of "the child gets a UTF-8 locale it can actually use, the
// caller's environment is not touched, and a pin that cannot be made is a
// REFUSAL rather than a silent fallback".

// resetFilmChildLocaleListForTest clears the host-listing cache so a test can
// swap the availability seam.
//
// The cache is deliberate in production (`locale -a` is about the HOST, so asking
// twice in one process would be two answers to one question), which is exactly why
// a test must be able to clear it: without this, the FIRST test to resolve the
// locale would freeze the answer for every later one and the seam tests would pass
// or fail by test ORDER.
func resetFilmChildLocaleListForTest() {
	localeListOnce = sync.Once{}
	localeListVal, localeListErr = nil, nil
}

// stubLocaleList replaces the availability seam with a fixed host listing.
func stubLocaleList(t *testing.T, names ...string) {
	t.Helper()
	resetFilmChildLocaleListForTest()
	prev := filmChildLocaleSource
	filmChildLocaleSource = func() ([]string, error) { return names, nil }
	t.Cleanup(func() {
		filmChildLocaleSource = prev
		resetFilmChildLocaleListForTest()
	})
}

// TestFilmChildEnvPinsUTF8OverAnInheritedNonUTF8Locale is THE regression.
//
// It drives the real composition function with the environment the defect was
// measured under — LANG=C, LC_ALL=C, plus an LC_CTYPE=C of the shape a caller can
// very well have — and asserts that the CHILD cannot see any of them, and does see
// one UTF-8 locale under both of the names a terminal consults.
func TestFilmChildEnvPinsUTF8OverAnInheritedNonUTF8Locale(t *testing.T) {
	stubLocaleList(t, "C.utf8", "en_US.utf8")
	t.Setenv(FilmChildLocaleEnv, "")
	caller := []string{
		"PATH=/usr/bin",
		"LANG=C",
		"LC_ALL=C",
		"LC_CTYPE=C",
		"LC_MESSAGES=en_US.UTF-8",
		"HOME=/home/somebody",
	}
	env, err := FilmTerminalChildEnv(":97", caller)
	if err != nil {
		t.Fatalf("the pin was refused although the host offers C.utf8: %v", err)
	}
	for _, banned := range []string{"LANG=C", "LC_ALL=C", "LC_CTYPE=C", "LC_MESSAGES=en_US.UTF-8"} {
		if envHas(env, banned) {
			t.Errorf("the filmed child inherits %q — an inherited locale spelling survives a value appended after it, because getenv(3) returns the FIRST match", banned)
		}
	}
	for _, name := range []string{"LANG=", "LC_ALL="} {
		v, ok := envValue(env, name)
		if !ok {
			t.Fatalf("the filmed child has no %s at all: the terminal would decide its decoding codeset from nothing", name)
		}
		if !filmChildLocaleUTF8Pattern.MatchString(v) {
			t.Errorf("the filmed child's %s=%q is not a UTF-8 locale — a non-UTF-8 codeset is the measured defect (Chinese filmed ≈15.41 cell-widths wide instead of ≈11.17)", name, v)
		}
	}
	if got, _ := envValue(env, "LANG="); localeNameKey(got) != localeNameKey(FilmChildLocaleDefault) {
		t.Errorf("the pin is %q; the host's first available candidate (%s) should have won", got, FilmChildLocaleDefault)
	}
	// The rest of the caller's environment passes through: this function's job is
	// two variables, not a curated environment.
	for _, keep := range []string{"PATH=/usr/bin", "HOME=/home/somebody"} {
		if !envHas(env, keep) {
			t.Errorf("the filmed child lost %q — the pin replaces the locale and the display, not everything else", keep)
		}
	}
}

// TestFilmChildEnvReplacesAnInheritedDisplay pins the OTHER half of the same
// discipline, which this function already carried (measured 2026-10-03: the first
// end-to-end run mapped its terminal on the operator's own X-forwarded display
// because an appended DISPLAY lost to the inherited one).
func TestFilmChildEnvReplacesAnInheritedDisplay(t *testing.T) {
	stubLocaleList(t, "C.utf8")
	t.Setenv(FilmChildLocaleEnv, "")
	env, err := FilmTerminalChildEnv(":97", []string{"DISPLAY=localhost:10.0", "PATH=/usr/bin"})
	if err != nil {
		t.Fatalf("resolve the child environment: %v", err)
	}
	if envHas(env, "DISPLAY=localhost:10.0") {
		t.Error("the filmed terminal inherits the caller's DISPLAY — the film would photograph somebody else's desktop")
	}
	if v, ok := envValue(env, "DISPLAY="); !ok || v != ":97" {
		t.Errorf("DISPLAY=%q (present=%v), want :97", v, ok)
	}
}

// TestFilmChildEnvDoesNotTouchTheCallerEnvironment is the "pin the child, never the
// caller" contract, checked as a property of the PROCESS rather than of the
// function's return value: a driver that exported LANG into its own environment
// would change the locale of everything it does afterwards, and of every child
// started for any other reason.
func TestFilmChildEnvDoesNotTouchTheCallerEnvironment(t *testing.T) {
	stubLocaleList(t, "C.utf8")
	t.Setenv("LANG", "C")
	t.Setenv("LC_ALL", "C")
	t.Setenv("LC_CTYPE", "C")
	before := os.Environ()
	if _, err := FilmTerminalChildEnv(":97", before); err != nil {
		t.Fatalf("resolve the child environment: %v", err)
	}
	for _, want := range []string{"LANG=C", "LC_ALL=C", "LC_CTYPE=C"} {
		if v, _ := os.LookupEnv(strings.TrimSuffix(want, "=C")); v != "C" {
			t.Errorf("the caller's %s is %q after composing a child environment, want %q — the pin belongs to the CHILD alone", strings.TrimSuffix(want, "=C"), v, "C")
		}
	}
	if got := len(os.Environ()); got != len(before) {
		t.Errorf("the caller's environment has %d entries after composing a child environment, want %d (unchanged)", got, len(before))
	}
}

// TestFilmChildLocaleRefusesWhenTheHostHasNoUTF8Locale is the Rule 15 half: a pin
// that cannot be made is not silently skipped.
//
// The measured reason it must be a REFUSAL: glibc's setlocale returns NULL for a
// locale that is not installed and the process runs ON in "C" — so a driver that
// pinned anyway would produce exactly the mojibake it meant to remove, while
// claiming to have fixed it.
func TestFilmChildLocaleRefusesWhenTheHostHasNoUTF8Locale(t *testing.T) {
	stubLocaleList(t, "C", "POSIX", "en_US")
	t.Setenv(FilmChildLocaleEnv, "")
	_, err := ResolveFilmChildLocale()
	if err == nil {
		t.Fatal("a host offering only non-UTF-8 locales still produced a pin")
	}
	for _, want := range []string{filmChildLocaleListBinary + " " + filmChildLocaleListArg, FilmChildLocaleEnv, filmChildLocaleWideMeasure, filmChildLocaleNarrowMeasure} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q, so an operator cannot act on it: %v", want, err)
		}
	}
}

// TestFilmChildLocaleRefusesAPinThatIsNotUTF8: the knob is a channel, not an
// override of the requirement. Naming a non-UTF-8 locale the host HAS is still
// refused, because it is the exact value that films mojibake.
func TestFilmChildLocaleRefusesAPinThatIsNotUTF8(t *testing.T) {
	stubLocaleList(t, "C", "C.utf8", "en_US")
	t.Setenv(FilmChildLocaleEnv, "C")
	_, err := ResolveFilmChildLocale()
	if err == nil {
		t.Fatal("a pin of \"C\" was accepted: a non-UTF-8 codeset is the defect this pin exists to remove")
	}
	if !strings.Contains(err.Error(), FilmChildLocaleEnv+"=\"C\"") {
		t.Errorf("the refusal does not quote the value it refused: %v", err)
	}
}

// TestFilmChildLocaleRefusesAPinThisHostDoesNotHave: naming a locale is a request,
// and a request the host cannot satisfy is reported with the host's own listing
// rather than silently ignored.
func TestFilmChildLocaleRefusesAPinThisHostDoesNotHave(t *testing.T) {
	stubLocaleList(t, "C", "C.utf8")
	t.Setenv(FilmChildLocaleEnv, "zh_CN.UTF-8")
	_, err := ResolveFilmChildLocale()
	if err == nil {
		t.Fatal("an uninstalled locale was accepted")
	}
	if !strings.Contains(err.Error(), "zh_CN.UTF-8") || !strings.Contains(err.Error(), "C.utf8") {
		t.Errorf("the refusal must name the value AND the host's listing, got: %v", err)
	}
}

// TestFilmChildLocaleAcceptsAnInstalledUTF8Pin: the knob has to be usable, or it is
// a knob nobody can set.
func TestFilmChildLocaleAcceptsAnInstalledUTF8Pin(t *testing.T) {
	stubLocaleList(t, "C", "zh_CN.utf8", "en_US.utf8")
	t.Setenv(FilmChildLocaleEnv, "zh_CN.UTF-8")
	got, err := ResolveFilmChildLocale()
	if err != nil {
		t.Fatalf("an installed UTF-8 locale was refused: %v", err)
	}
	if localeNameKey(got) != localeNameKey("zh_CN.UTF-8") {
		t.Errorf("the pin is %q, want the locale the caller asked for (in the host's own spelling)", got)
	}
}

// TestFilmChildLocaleFallsBackInOrder: with no knob set, the driver prefers its own
// built-in default (glibc compiles C.UTF-8 in, so it is the candidate a bare host
// has), and only then a locale that must have been provisioned.
func TestFilmChildLocaleFallsBackInOrder(t *testing.T) {
	stubLocaleList(t, "C", "en_US.utf8")
	t.Setenv(FilmChildLocaleEnv, "")
	got, err := ResolveFilmChildLocale()
	if err != nil {
		t.Fatalf("a host with a provisioned UTF-8 locale was refused: %v", err)
	}
	if localeNameKey(got) != localeNameKey("en_US.UTF-8") {
		t.Errorf("fallback chose %q, want en_US.UTF-8 — the built-in default is absent from this listing", got)
	}
}

// TestFilmChildLocaleAcceptsTheHostsSpellingOfItsOwnDefault: C.UTF-8 and C.utf8 are
// two spellings of ONE locale (this host's `locale -a` prints the lower-case form),
// so availability is what decides, not the case of the letters or the dash in the
// codeset. The value a run REPORTS is the host's own spelling — the name an
// operator can paste back into a shell on this machine.
func TestFilmChildLocaleAcceptsTheHostsSpellingOfItsOwnDefault(t *testing.T) {
	stubLocaleList(t, "C.utf8")
	t.Setenv(FilmChildLocaleEnv, "")
	got, err := ResolveFilmChildLocale()
	if err != nil {
		t.Fatalf("the host's own spelling of the built-in UTF-8 locale was refused: %v", err)
	}
	if got != "C.utf8" {
		t.Errorf("the pin is %q; a report should carry the HOST's own spelling (C.utf8), which is the name this machine answers to", got)
	}
	if localeNameKey(got) != localeNameKey(FilmChildLocaleDefault) {
		t.Errorf("the pin %q is not the driver's own default locale %q", got, FilmChildLocaleDefault)
	}
}

// TestFilmChildLocaleIsResolvedOncePerProcess pins the cache: the question is about
// the HOST, so two films in one process must not ask it twice.
func TestFilmChildLocaleIsResolvedOncePerProcess(t *testing.T) {
	resetFilmChildLocaleListForTest()
	prev := filmChildLocaleSource
	calls := 0
	filmChildLocaleSource = func() ([]string, error) {
		calls++
		return []string{"C.utf8"}, nil
	}
	t.Cleanup(func() {
		filmChildLocaleSource = prev
		resetFilmChildLocaleListForTest()
	})
	t.Setenv(FilmChildLocaleEnv, "")
	if _, err := ResolveFilmChildLocale(); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := ResolveFilmChildLocale(); err != nil {
		t.Fatalf("resolve again: %v", err)
	}
	if calls != 1 {
		t.Errorf("the host listing was read %d time(s) for two resolutions, want 1", calls)
	}
}

// TestSystemLocaleListIsTheHostsOwnListing drives the REAL availability source (no
// seam): the seam is only trustworthy while the thing behind it answers honestly,
// so one test asks it rather than stubbing it.
//
// It asserts the listing is non-empty — an empty listing would make every
// resolution fail, and a scan that found nothing must be distinguishable from a
// scan that never ran (Rule 15) — and, when the host does offer a UTF-8 locale, the
// real resolution returns one of the host's OWN names.
func TestSystemLocaleListIsTheHostsOwnListing(t *testing.T) {
	resetFilmChildLocaleListForTest()
	t.Cleanup(resetFilmChildLocaleListForTest)
	names, err := systemLocaleList()
	if err != nil {
		t.Fatalf("the host's own locale listing could not be read: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("the host's locale listing is empty")
	}
	for _, n := range names {
		if strings.TrimSpace(n) == "" {
			t.Fatalf("the listing carries an empty name, so it is not this host's own listing: %q", names)
		}
	}
	t.Setenv(FilmChildLocaleEnv, "")
	got, rerr := ResolveFilmChildLocale()
	if rerr != nil {
		// Honest: a host with no UTF-8 locale MUST refuse, and this test says so
		// rather than demanding a property the machine may not have.
		if _, ok := matchLocale(names, FilmChildLocaleDefault); !ok {
			t.Logf("this host offers no UTF-8 locale and the driver refused, as designed: %v", rerr)
			return
		}
		t.Fatalf("the host lists a UTF-8 locale but the driver refused: %v", rerr)
	}
	if _, ok := matchLocale(names, got); !ok {
		t.Errorf("the pin %q is not one of the host's own locale names (%d listed)", got, len(names))
	}
}

// TestFilmChildLocaleNoticeNamesTheKnob: the pin changes the pixels of every
// Chinese frame, so the run reports it; a report that does not say where to change
// it leaves an operator unable to act.
func TestFilmChildLocaleNoticeNamesTheKnob(t *testing.T) {
	notice := FilmChildLocaleNotice("C.UTF-8")
	for _, want := range []string{"C.UTF-8", FilmChildLocaleEnv, "LC_"} {
		if !strings.Contains(notice, want) {
			t.Errorf("the notice does not name %q: %s", want, notice)
		}
	}
}

// envValue returns the value of the FIRST entry spelled name=value, which is the
// entry getenv(3) would return — the property every assertion here is about.
func envValue(env []string, name string) (string, bool) {
	for _, kv := range env {
		if strings.HasPrefix(kv, name) {
			return strings.TrimPrefix(kv, name), true
		}
	}
	return "", false
}

// envHas reports whether the slice carries this exact entry.
func envHas(env []string, kv string) bool {
	for _, e := range env {
		if e == kv {
			return true
		}
	}
	return false
}
