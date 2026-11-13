package shot

// filmlocale.go — THE LOCALE A FILMED CHILD RUNS UNDER: pinned to a UTF-8
// locale, or the film is REFUSED.
//
// WHY THIS FILE EXISTS (the gap `01a10370` declared and did not close). A film's
// Chinese was broken twice, and the first defect hid the second:
//
//	1. THE FONT. xterm's default face is the X11 core bitmap `fixed`, whose
//	   charset is ISO8859-1 — zero CJK glyphs, so Chinese filmed as □. Fixed
//	   2026-10-04 by moving the default to xfce4-terminal (VTE + fontconfig).
//	2. THE LOCALE. A terminal DECODES ITS PTY STREAM AS TEXT, and the CODE SET it
//	   decodes with is the one its own LOCALE names. xfce4-terminal inherits the
//	   caller's environment — and when this driver runs under LANG=C / LC_ALL=C
//	   (the ambient state of this very host), VTE has no UTF-8 in its codeset, so
//	   it advances ONE CELL PER BYTE. The SAME Chinese line then films 15.41
//	   cell-widths wide instead of 11.17: each CJK glyph becomes byte-sized and
//	   shoves its neighbours along, so the film is a picture of a layout the
//	   product never drew. Measured 2026-10-04 on one private Xvfb: the same
//	   xfce4-terminal measures 11.17 units with LANG=C.UTF-8 and 15.41 units with
//	   LANG=C (this task's evidence, red→green, in
//	   devdoc/film-child-locale-pin.md).
//
// THE FIX IS A PIN, NOT A HOPE. The filmed child's environment is composed here:
// every inherited locale spelling is DROPPED (see filmChildLocaleStripPrefixes)
// and ONE UTF-8 locale is written in. The child therefore SEES exactly one locale
// rather than a stack whose winner depends on how libc resolves it, and NOTHING
// about the caller's own environment is touched — the pin is an entry in the
// CHILD's env slice and nowhere else, the same discipline displayEnvName already
// follows for DISPLAY (an environment a driver cleans up after is an environment
// it broke).
//
// THE PRECEDENT IS THIS REPO'S OWN, TWICE. internal/tools/bash.go pins
// bashDefaultLocale ("C") onto every bash child, and cmd/agent/merge_run.go pins
// mergeGitLocale onto every git child of the merge lane; both REPLACE the gettext
// spellings rather than appending, because which copy of a duplicated name a
// child takes is READER-dependent — glibc getenv(3) (which is what this file's
// DISPLAY half is read by, through Xlib) and setlocale(3)+strerror(3) take the
// FIRST — and Go's os.Getenv takes the FIRST with them — while the shells take
// the LAST (internal/tools/
// git_child_locale.go, ChildLocaleDuplicateKeyRule; measured 2026-10-16) — so an
// appended override is the entry that overrides only for SOME readers. This file
// is the third instance of that shape, with the two differences the film's own
// requirement forces:
//
//   - The pinned locale must be UTF-8. The other two pins want a NARROW locale
//     ("C") because they read diagnostics; this one wants a WIDE one, because the
//     terminal must be able to DECODE the product's text. That is a property of
//     the child, not a language preference: nothing in this package reads text
//     (Rule 1's reason for the other two pins does not apply here, and copying
//     "C" here would recreate the defect).
//   - The pin must be PROVED AVAILABLE before it is used (below).
//
// THE PIN FAILS LOUDLY (Rule 15). A locale that is not installed is NOT refused by
// the libc call that sets it: `setlocale` returns NULL and the process runs on in
// "C" — measured 2026-10-04, `env -i LC_ALL=xx_YY.bogus locale -a` exits 0 with a
// warning on stderr. An unverified pin is therefore WORSE than no pin, because it
// looks fixed and films mojibake. Availability is CHECKED against the host's own
// locale listing, and a host that cannot provide a UTF-8 locale gets a REFUSAL
// naming the command, the reason and the knob — never a silent fallback to
// whatever the caller happened to have.
//
// ZERO NEW DEPENDENCIES (Rule 35): the listing is read by running the host's own
// `locale` binary (coreutils/glibc, no package installs anything), and C.UTF-8 is
// glibc's built-in UTF-8 locale — compiled in, not shipped in /usr/share/i18n, so
// it is present on a host whose /usr/lib/locale holds nothing else.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// The knob, the pin and the listing. Every literal is named (Rules 1/1b).
const (
	// FilmChildLocaleEnv is the escape hatch's env name: the locale the filmed
	// child is pinned to, when the operator needs a locale this driver would not
	// pick by itself. It is the SAME shape as GOPI_BASH_LOCALE and
	// GOPI_MERGE_LOCALE (each of those is defined in the package that owns its
	// child, and read by ONE site) — and it is deliberately env-ONLY: the pin's
	// value is not a settings key, because a settings file that could turn this
	// back into a non-UTF-8 locale would be a file that can silently make every
	// film on the host mojibake again, which is the defect this file removes
	// (Rule 2's ranking still holds: the env is read HERE and nowhere else).
	FilmChildLocaleEnv = "GOPI_FILM_LOCALE"
	// FilmChildLocaleDefault is the locale this driver pins when nobody asked for
	// one. It is C.UTF-8 rather than en_US.UTF-8 on purpose: glibc compiles C.UTF-8
	// IN (no locale archive, no locale-gen, nothing to install), so the pin is
	// available on a bare host — the zero-dependency property the whole film
	// pipeline is built to keep (Rule 35). A different locale is available through
	// the knob, and is accepted only if it is UTF-8 AND installed.
	FilmChildLocaleDefault = "C.UTF-8"
	// filmChildLocaleListBinary is the program whose own listing is the evidence
	// that a locale exists. Checking the name against the filesystem instead would
	// be a guess about glibc's layout (compiled-in locales leave no directory), and
	// the question to answer is exactly the one this command answers: "does THIS
	// libc know this locale name?".
	filmChildLocaleListBinary = "locale"
	// filmChildLocaleListArg is its listing switch (`locale -a` = every name this
	// host can set).
	filmChildLocaleListArg = "-a"
	// filmChildLocaleListTimeout bounds the listing. A `locale` that hangs must not
	// hang a film: the refusal is a bounded, reported failure, never a wait.
	filmChildLocaleListTimeout = 5 * time.Second
)

// filmChildLocaleCandidates are the fallbacks, in preference order, tried in
// order when nobody set the knob. C.UTF-8 and C.utf8 are TWO SPELLINGS OF ONE
// LOCALE (glibc accepts both for its built-in UTF-8 locale; `locale -a` on this
// host prints `C.utf8`), so accepting either is availability, not a second choice.
// en_US.UTF-8 is last because it is the one candidate an unprovisioned host may
// lack — it is not in glibc's built-in set. The list exists because a host's
// spelling is not this driver's to assume (the same three names scripts/ui/*.sh
// probe, and the same ordering).
var filmChildLocaleCandidates = []string{
	FilmChildLocaleDefault,
	"C.utf8",
	"en_US.UTF-8",
}

// filmChildLocaleUTF8Pattern is the SHAPE of an acceptable pin: a locale name
// whose codeset is UTF-8, in either spelling. It is a shape and not a whitelist
// so an operator's own locale (zh_CN.UTF-8, de_DE.UTF-8, C.UTF-8 …) is legal —
// what is refused is a pin that is not UTF-8 at all (`C`, `POSIX`, `en_US`), since
// that is precisely the value that films mojibake. Compiled once, named here,
// never re-spelled at a call site (Rules 1/13).
//
// The language part admits no `.` or `-` on purpose: locale names do not carry
// them before the codeset (territory and modifier are `_` and `@`), and allowing
// them would make the pattern ambiguous against the codeset's own dot.
var filmChildLocaleUTF8Pattern = regexp.MustCompile(`(?i)^[a-z][a-z0-9_@]*\.utf-?8$`)

// The KEY a locale name is compared by, and the reason it is not simple string
// equality: `.UTF-8` and `.utf8` are TWO SPELLINGS OF ONE LOCALE — glibc accepts
// both for its built-in UTF-8 locale, `locale -a` prints whichever form its table
// carries, and this host prints `C.utf8` while every caller in this repo writes
// `C.UTF-8`. Comparing raw strings therefore produces a FALSE REFUSAL on a host
// that has the locale (measured 2026-10-04: the FIRST version of this file refused
// the very locale it defaults to, because `C.UTF-8` does not compare equal to
// `C.utf8`). The comparison is case-insensitive and the codeset's dash is dropped.
var filmChildLocaleDashSpelling = regexp.MustCompile(`(?i)\.utf-8$`)

// localeNameKey normalizes a locale name for comparison — see
// filmChildLocaleDashSpelling. It is applied on BOTH sides of every availability
// question, never to the value a run reports (a report quotes the host's own
// spelling, which is what an operator can paste into a shell).
func localeNameKey(name string) string {
	return strings.ToLower(filmChildLocaleDashSpelling.ReplaceAllString(name, ".utf8"))
}

// matchLocale answers the availability question AND returns the host's own
// spelling of the name, so a film reports the name this host would accept rather
// than the one this driver typed.
func matchLocale(names []string, want string) (string, bool) {
	key := localeNameKey(want)
	for _, n := range names {
		if localeNameKey(n) == key {
			return n, true
		}
	}
	return "", false
}

// filmChildLocaleStripPrefixes are the environment spellings the pin REPLACES.
//
// IT IS A PREFIX LIST AND IT DROPS THE WHOLE `LC_` FAMILY, not the four gettext
// names the other two pins replace. Two reasons, both about this child rather than
// about tidiness: (1) the variable that actually governs character decoding and
// width is LC_CTYPE, and an inherited LC_CTYPE survives a LANG the driver appends —
// leaving the exact decision this file exists to make to a variable nobody looked
// at; (2) the film child's locale must be ONE value, so the honest composition is
// "drop every locale spelling, write the pin", which cannot be defeated by a
// variable that was not thought of.
var filmChildLocaleStripPrefixes = []string{"LANG=", "LANGUAGE=", "LC_"}

// filmChildLocaleSource is the seam at the AVAILABILITY boundary: the function
// that asks the host which locales exist.
//
// Production never assigns it; a test replaces it to drive the criterion itself
// with no dependency on the machine running the suite (the same seam idiom
// filmWindowCapture uses, and for the same reason: the decision — refuse when
// there is no UTF-8 locale, prefer the built-in one when there is — is the
// deliverable, and it must be provable without a screen OR a particular locale
// set). One test does ask the REAL host, so "the seam is honest" is checked rather
// than assumed.
var filmChildLocaleSource = systemLocaleList

// localeListOnce caches the host's listing: the question is about the host and
// cannot change inside one process, so asking twice would be two answers to one
// question (Rule 2 — one read site) and a needless second process per film.
var (
	localeListOnce sync.Once
	localeListVal  []string
	localeListErr  error
)

// systemLocaleList returns every locale name this host can set, in the host's own
// order, read by running `locale -a`.
//
// The child is given an environment holding ONE locale ("C"), and that is not
// cosmetic: the listing must not depend on whether the CALLER's locale happens to
// be installed. Measured 2026-10-04, `env -i LC_ALL=xx_YY.bogus locale -a` still
// prints the whole list (exit 0, warning on stderr), so the listing is stable
// either way — the clean environment makes that a property of the code rather than
// a property of the caller.
func systemLocaleList() ([]string, error) {
	path, err := exec.LookPath(filmChildLocaleListBinary)
	if err != nil {
		return nil, fmt.Errorf("the host's locale listing is unavailable: no %s executable on PATH (%s), so this driver cannot prove that a UTF-8 locale exists before pinning it — and an unproved pin is worse than none (glibc's setlocale returns NULL for a locale that is not installed and the process runs on in \"C\", which films Chinese as mojibake)",
			filmChildLocaleListBinary, os.Getenv("PATH"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), filmChildLocaleListTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, filmChildLocaleListArg)
	cmd.Env = []string{filmChildLocaleProbeEnv}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("the host's locale listing could not be read (%s %s, within %s): %w",
			filmChildLocaleListBinary, filmChildLocaleListArg, filmChildLocaleListTimeout, err)
	}
	names := make([]string, 0, len(out)/8)
	for _, line := range strings.Split(string(out), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("the host's locale listing (%s %s) is EMPTY — a host that can set no locale at all cannot be pinned, and an empty listing is reported rather than read as \"nothing matched\"",
			filmChildLocaleListBinary, filmChildLocaleListArg)
	}
	return names, nil
}

// filmChildLocaleProbeEnv is the environment the listing is taken under. It is a
// single assignment, not a table: the listing's job is to answer about the HOST,
// so the fewer inherited variables it can depend on the better (see
// systemLocaleList).
const filmChildLocaleProbeEnv = "LC_ALL=C"

// knownLocaleList is the cached listing, and the ONE caller of the source seam.
func knownLocaleList() ([]string, error) {
	localeListOnce.Do(func() {
		localeListVal, localeListErr = filmChildLocaleSource()
	})
	return localeListVal, localeListErr
}

// ResolveFilmChildLocale decides the locale the filmed child is pinned to, or
// refuses the film.
//
//	rank 1  GOPI_FILM_LOCALE (validated: it must be UTF-8 AND installed)
//	rank 2  the first candidate of filmChildLocaleCandidates the host has
//
// The value RETURNED is the host's own spelling of the name (see matchLocale), so
// what a film reports is a name the operator can paste into a shell on this host.
//
// There is deliberately NO rank 3. "Leave the caller's locale alone" is not a
// fallback — it is the defect — and "use the pin anyway, unproved" is the silent
// failure this function exists to prevent (a pin glibc discards leaves a film that
// LOOKS fixed). A host that can offer no UTF-8 locale gets a film that does not
// start, with the reason and the knob in its message.
func ResolveFilmChildLocale() (string, error) {
	names, err := knownLocaleList()
	if err != nil {
		return "", err
	}
	if want := strings.TrimSpace(os.Getenv(FilmChildLocaleEnv)); want != "" {
		if !filmChildLocaleUTF8Pattern.MatchString(want) {
			return "", fmt.Errorf("%s=%q cannot be pinned onto a filmed terminal: the pin must name a locale whose codeset is UTF-8 (a name of the form lang[_TERRITORY][.CODESET], e.g. %s or zh_CN.UTF-8), because the terminal DECODES the product's text with the codeset its locale names — a non-UTF-8 locale (\"C\", \"POSIX\") makes it advance one cell per BYTE and film Chinese %s units wide. Unset %s to get this driver's own choice",
				FilmChildLocaleEnv, want, FilmChildLocaleDefault, filmChildLocaleWideMeasure, FilmChildLocaleEnv)
		}
		host, ok := matchLocale(names, want)
		if !ok {
			return "", fmt.Errorf("%s=%q is not installed on this host, so it cannot be pinned: glibc's setlocale returns NULL for an unknown locale and the process runs on in \"C\" — the film would be photographed with exactly the wrong width while looking fixed. Hosts answer for themselves with `%s %s` (this host offers: %s). Set %s to one of those, or unset it to get %s",
				FilmChildLocaleEnv, want, filmChildLocaleListBinary, filmChildLocaleListArg, localeListSample(names), FilmChildLocaleEnv, FilmChildLocaleDefault)
		}
		return host, nil
	}
	for _, cand := range filmChildLocaleCandidates {
		if !filmChildLocaleUTF8Pattern.MatchString(cand) {
			// A candidate this driver would itself refuse is not a candidate: the
			// list is DATA, and data that drifts from the rule is caught here
			// rather than pinned onto a child.
			continue
		}
		if host, ok := matchLocale(names, cand); ok {
			return host, nil
		}
	}
	return "", fmt.Errorf("this host offers NO UTF-8 locale, so a film cannot be made here: the terminal would decode the product's text in a non-UTF-8 codeset and photograph Chinese at %s cell-widths instead of %s (the exact defect this pin removes). Install one (on glibc hosts C.UTF-8 is built in; otherwise the distro's own locale package), or name one the host has with %s=NAME — the pinned locale must be UTF-8, so there is no third option. This host's listing (`%s %s`) offers %d name(s) and none of them is UTF-8: %s",
		filmChildLocaleWideMeasure, filmChildLocaleNarrowMeasure, FilmChildLocaleEnv,
		filmChildLocaleListBinary, filmChildLocaleListArg, len(names), localeListSample(names))
}

// The two measured widths, as constants, so the refusal and the devdoc cannot
// quote different numbers (Rule 1b/13). They are MEASUREMENTS of the same line
// `中文测试ABC` (4 CJK glyphs + 3 ASCII): 11 = 4 double-width cells + 3 cells, and
// 15 = 4 glyphs x 3 bytes + 3 cells. The decimals are the probe's own precision
// (it self-calibrates one ASCII advance from the pixels; see
// devdoc/film-child-locale-pin.md), and 11.17/15.41 are reported as ≈ in prose
// wherever the exact value matters less than the ratio.
const (
	// filmChildLocaleNarrowMeasure is the advance width of that line in cells when
	// the terminal decodes UTF-8 and the width is measured per CHARACTER.
	filmChildLocaleNarrowMeasure = "≈11.17"
	// filmChildLocaleWideMeasure is the same line's width when the terminal has no
	// UTF-8 codeset and advances per BYTE — 3.7 cells too wide, and the reason the
	// neighbouring ASCII is pushed out of place.
	filmChildLocaleWideMeasure = "≈15.41"
)

// localeListSample renders a bounded, SSORTED sample of a listing for a refusal
// message: a host can list hundreds of names, and a refusal nobody can finish
// reading is a refusal nobody reads.
func localeListSample(names []string) string {
	const maxSample = 12
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	if len(sorted) > maxSample {
		sorted = append(sorted[:maxSample], fmt.Sprintf("… (%d more)", len(names)-maxSample))
	}
	return strings.Join(sorted, ", ")
}

// FilmTerminalChildEnv composes the filmed child's WHOLE environment: DISPLAY
// pointing at this run's private display, and the locale PINNED to a UTF-8 one.
//
// IT REPLACES RATHER THAN APPENDS, for both halves and for the same reason: a
// duplicated name leaves the reader to decide, and no single order is the rule —
// `getenv(3)` (this half's reader: Xlib resolves DISPLAY through it) takes the
// FIRST match, so appending a second DISPLAY — or a second LANG — leaves the
// CALLER's winning THERE, while a shell or Go reader would take the appended one
// and appear to work (internal/tools/git_child_locale.go,
// ChildLocaleDuplicateKeyRule; measured 2026-10-16). One entry is the shape that
// holds for every reader. Measured 2026-10-03 for DISPLAY: the first
// end-to-end run of this driver mapped its terminal on the operator's own
// X-forwarded display and reported, correctly, that no window on the private
// display published the terminal's pid. The locale half was measured 2026-10-04:
// under LANG=C the filmed Chinese line measured 15.41 cell-widths instead of
// 11.17.
//
// NOTHING OF THE CALLER'S IS MODIFIED: this function reads `environ` and returns a
// new slice. The pin lives in the child's environment and nowhere else — this
// process's own LANG/LC_* are never set, so a driver that films a CJK surface
// leaves no trace in the shell it was started from (the property the task's
// evidence re-verifies with `env` before and after a film).
//
// The refusal is RETURNED, never printed and swallowed: a film whose locale cannot
// be pinned must not start at all (Rule 15).
func FilmTerminalChildEnv(display string, environ []string) ([]string, error) {
	locale, err := ResolveFilmChildLocale()
	if err != nil {
		return nil, err
	}
	displayPrefix := displayEnvName + "="
	out := make([]string, 0, len(environ)+3)
	for _, kv := range environ {
		if strings.HasPrefix(kv, displayPrefix) || hasAnyPrefix(kv, filmChildLocaleStripPrefixes) {
			continue
		}
		out = append(out, kv)
	}
	// LC_ALL is the strongest of the locale variables, and it is written for the
	// same reason the two existing pins write it: a child that consults LC_ALL (or
	// that resets LC_ALL from LANG itself) sees ONE answer either way, and a
	// terminal emulator reads both.
	return append(out, displayPrefix+display, "LANG="+locale, "LC_ALL="+locale), nil
}

// hasAnyPrefix reports whether kv begins with any of the given prefixes — the
// loop the locale strips share, so the two spellings of "this variable is
// replaced, not inherited" cannot drift apart.
func hasAnyPrefix(kv string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(kv, p) {
			return true
		}
	}
	return false
}

// FilmChildLocaleNotice is the one-line report a film run prints so its locale
// pin is AUDITABLE rather than invisible: the pin changes the pixels of every
// Chinese frame, and a reader who cannot see which locale produced them cannot
// tell a correct film from a mojibake one.
func FilmChildLocaleNotice(locale string) string {
	return fmt.Sprintf("the filmed terminal is pinned to locale %s (its child environment replaces every inherited %s/LC_*; %s names a different one)",
		locale, "LANG", FilmChildLocaleEnv)
}
