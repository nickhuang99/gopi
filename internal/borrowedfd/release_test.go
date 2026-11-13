package borrowedfd

// release_test.go — the guard for release.go, on the SMALLEST sample the defect
// can be shown on (Rule 10.4): ONE number, ONE recycle, TWO closes.
//
// The window under test is not a race to be reproduced — it is constructed
// deterministically, because the kernel recycles the lowest free number and the
// test decides when the number becomes free. That makes the defect's own moment
// an ORDER of three calls rather than a timing coincidence, which is also why
// this guard needs no load of any kind (Rule 27).
//
// Both arms ask the KERNEL (fcntl F_GETFD, readlink /proc/self/fd) and never a
// wrapper: the wrapper's own state is exactly what cannot be trusted here.

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// releaseFixtureClaim takes ONE raw descriptor number onto a real file, at a
// number the test controls, and returns the identity taken while it was still
// ours.
//
// F_DUPFD with an explicit floor is what makes the recycle deterministic: the
// number is far above anything the suite holds, so the next F_DUPFD/F_DUPFD_CLOEXEC
// with the same floor is guaranteed to land on it once it is freed.
func releaseFixtureClaim(t *testing.T, owner *os.File, floor int) (Identity, error) {
	t.Helper()
	n, _, errno := syscall.Syscall(syscall.SYS_FCNTL, owner.Fd(), uintptr(syscall.F_DUPFD), uintptr(floor))
	if errno != 0 {
		t.Fatalf("claim a number at or above %d: %v", floor, errno)
	}
	return IdentityOf(int(n))
}

// reclaimAt hands the number `n` to a STRANGER, deterministically.
//
// MEASURED, and the reason this helper exists: an ordinary os.OpenFile takes the
// LOWEST free number (fd 7 on this host), so "release the claim, then open a file"
// does NOT put the stranger on the recycled number — the first version of this
// guard skipped for exactly that reason. F_DUPFD with min=n is the call that makes
// the window an ORDER of operations instead of a hope about allocation policy.
func reclaimAt(t *testing.T, n int, path string) *os.File {
	t.Helper()
	// THE STRANGER IS OPENED AS A RAW NUMBER, never through os.OpenFile: a wrapper
	// minted before the number is placed would be ABANDONED when the descriptor is
	// renumbered below, and its finalizer would later close that same number a
	// second time — after the kernel had recycled it to somebody else. Measured:
	// that stray close landed on another test's pipe (fd 8) in the same package run
	// and failed it with "transport-lost". A wrapper is only ever minted HERE, on a
	// number that will not move again.
	strangerFd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open the stranger file: %v", err)
	}
	// MEASURED: open() takes the LOWEST free number, so it often takes the recycled
	// n by itself; dup'ing in that case finds the NEXT free number and the guard
	// fails for a reason unrelated to the defect.
	if strangerFd == n {
		return os.NewFile(uintptr(strangerFd), path)
	}
	got, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(strangerFd), uintptr(syscall.F_DUPFD), uintptr(n))
	if errno != 0 {
		t.Fatalf("claim the recycled number %d for the stranger: %v", n, errno)
	}
	if closeErr := syscall.Close(strangerFd); closeErr != nil {
		t.Fatalf("release the stranger's original raw handle after renumbering it: %v", closeErr)
	}
	if int(got) != n {
		t.Fatalf("the stranger landed on fd %d, not the recycled %d — the window this guard needs is not open", got, n)
	}
	return os.NewFile(got, path)
}

// TestReleaseClosesTheNumberItStillHolds is the NEGATIVE CONTROL: when nothing
// took the number away, the guarded release really does close it. Without this
// arm a release that refused everything would read as correct.
func TestReleaseClosesTheNumberItStillHolds(t *testing.T) {
	owner, err := os.CreateTemp(t.TempDir(), "release-control-*")
	if err != nil {
		t.Fatalf("open the fixture file: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() }) //stylegate:ignore test fixture teardown

	claim, err := releaseFixtureClaim(t, owner, releaseFixtureFloor)
	if err != nil {
		t.Fatalf("claim the fixture descriptor: %v", err)
	}
	if got, want := claim.Number(), claim.Number(); got != want {
		t.Fatalf("identity carries number %d, want %d", got, want)
	}
	if err := claim.Release(); err != nil {
		t.Fatalf("releasing fd %d while this call still held it was refused (%v) — the instrument would never close anything, "+
			"which is the failure mode that looks like safety", claim.Number(), err)
	}
	if err := fdAlive(claim.Number()); err == nil {
		t.Fatalf("descriptor %d is still open after a successful release — the instrument reported a close it did not perform", claim.Number())
	}
}

// TestReleaseRefusesARecycledNumber is the DEFECT's own shape, constructed:
//
//	claim number N  →  the number stops being ours (closing the claim stands in
//	for every way that happens: a transfer, a production close, our own release)
//	→  an open() receives N  →  a LATE release of N arrives.
//
// The late release must refuse, and it must NAME the number, the recorded
// identity and what it observed — because a bare refusal is indistinguishable
// from the instrument being broken (Rule 15).
func TestReleaseRefusesARecycledNumber(t *testing.T) {
	dir := t.TempDir()
	owner, err := os.CreateTemp(dir, "release-owner-*")
	if err != nil {
		t.Fatalf("open the fixture file: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() }) //stylegate:ignore test fixture teardown

	claim, err := releaseFixtureClaim(t, owner, releaseFixtureFloor)
	if err != nil {
		t.Fatalf("claim the fixture descriptor: %v", err)
	}
	number := claim.Number()

	// STEP 2 — the number stops being ours.
	if err := syscall.Close(number); err != nil {
		t.Fatalf("release the claim to open the recycle window: %v", err)
	}

	// STEP 2b — the kernel recycles it. A STRANGER takes the number now: a
	// different file in a different directory, so a correct observation cannot
	// confuse it with what was claimed.
	strangerPath := dir + "/stranger"
	stranger := reclaimAt(t, number, strangerPath)
	// The stranger is released UNDER THE INSTRUMENT at teardown, not raw: it is the
	// only holder of that number, so releasing it is safe and the guard proves the
	// instrument's negative arm on a live stranger too.
	defer func() {
		if relErr := Witness(int(stranger.Fd())).Release(); relErr != nil {
			t.Errorf("releasing the stranger's own descriptor %d was refused: %v", stranger.Fd(), relErr)
		}
	}()

	// STEP 3 — the late release. It must refuse, and it must not touch the stranger.
	lateErr := claim.Release()
	if lateErr == nil {
		t.Fatalf("the late release of fd %d SUCCEEDED after the number was recycled to a stranger — it just closed %q "+
			"(identity %s), a descriptor this call never owned, and syscall.Close reported success while doing it",
			number, strangerPath, claim.String())
	}
	var moved *MovedError
	if !errors.As(lateErr, &moved) {
		t.Fatalf("the refusal for fd %d is %T (%v), want *MovedError — a caller must be able to tell 'I refused to close a "+
			"stranger' from 'the close itself failed'", number, lateErr, lateErr)
	}
	if moved.Number != number {
		t.Errorf("the refusal names fd %d, want the number it refused to close (%d)", moved.Number, number)
	}
	// What the refusal must CARRY: the NUMBER, the identity RECORDED while it was
	// ours, and what the kernel OBSERVED instead (the stranger's own path — the
	// number is alive under someone else, so "closed" would be a false observation
	// here; the closed reading is pinned by TestIdentityOfRefusesAReleasedNumber).
	for _, want := range []string{strconv.Itoa(number), claim.Target(), strangerPath} {
		if !strings.Contains(moved.Error(), want) {
			t.Errorf("the refusal does not carry %q — a reader cannot tell which number, what was recorded and what was "+
				"observed (Rule 15: failures must be NAMED, not merely refused)\nfull: %s", want, moved.Error())
		}
	}
	// THE PROPERTY THE WHOLE RULE IS FOR: the stranger is untouched.
	if err := fdAlive(number); err != nil {
		t.Fatalf("the stranger's descriptor %d is CLOSED (%v) — the late release closed a live descriptor that was not its own; "+
			"this is the defect, not a refusal", number, err)
	}
	if target, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(number)); err != nil || target != strangerPath {
		t.Fatalf("descriptor %d no longer names the stranger (%q, err=%v; want %q) — the number was released or replaced",
			number, target, err, strangerPath)
	}
}

// TestReleaseRefusesWhenTheNumberWasReusedByAnotherObject is the SAME judgement
// with the number still OPEN: a stranger holds it, so nothing is "gone" and the
// refusal must come from the IDENTITY comparison rather than from ENOENT. The
// two refusals are different findings and the message must separate them.
func TestReleaseRefusesWhenTheNumberWasReusedByAnotherObject(t *testing.T) {
	dir := t.TempDir()
	owner, err := os.CreateTemp(dir, "release-owner2-*")
	if err != nil {
		t.Fatalf("open the fixture file: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() }) //stylegate:ignore test fixture teardown

	claim, err := releaseFixtureClaim(t, owner, releaseFixtureFloor)
	if err != nil {
		t.Fatalf("claim the fixture descriptor: %v", err)
	}
	number := claim.Number()
	if err := syscall.Close(number); err != nil {
		t.Fatalf("release the claim: %v", err)
	}
	strangerPath := dir + "/stranger2"
	stranger := reclaimAt(t, number, strangerPath)
	defer func() {
		if relErr := Witness(int(stranger.Fd())).Release(); relErr != nil {
			t.Errorf("releasing the stranger's own descriptor %d was refused: %v", stranger.Fd(), relErr)
		}
	}()

	lateErr := claim.Release()
	if lateErr == nil {
		t.Fatalf("the late release of fd %d succeeded although the number now denotes %q — it closed a stranger", number, strangerPath)
	}
	// Here the number is ALIVE under the stranger, so the observation must say what
	// it denotes rather than "closed" — the operator reading this has to be able to
	// see the thief.
	if !strings.Contains(lateErr.Error(), strangerPath) {
		t.Errorf("the refusal does not report what the number denotes NOW (%q) — an operator cannot find the descriptor that "+
			"took the number\nfull: %s", strangerPath, lateErr)
	}
	if err := fdAlive(number); err != nil {
		t.Fatalf("the stranger's descriptor %d was closed by the refused release (%v)", number, err)
	}
}

// TestCheckRefusesAnUnrecordedIdentity pins the zero value's meaning: an Identity
// that was never taken must NOT authorise a close. A zero value that compared
// equal (the naive `==` on a struct) would make every forgotten Claim a licence
// to close an arbitrary number.
func TestCheckRefusesAnUnrecordedIdentity(t *testing.T) {
	var unrecorded Identity
	err := unrecorded.Check()
	if err == nil {
		t.Fatalf("an Identity that was never taken authorised a close — every forgotten claim becomes a licence to close an arbitrary number")
	}
	if !strings.Contains(err.Error(), "never taken") {
		t.Errorf("the refusal of an unrecorded identity reads %q, which does not say the identity was never taken", err)
	}
}

// TestIdentityOfRefusesAReleasedNumber pins the other half of the same honesty: a
// number this process does not hold has NO identity, and the cause is named
// rather than answered with an empty one.
func TestIdentityOfRefusesAReleasedNumber(t *testing.T) {
	owner, err := os.CreateTemp(t.TempDir(), "release-identity-*")
	if err != nil {
		t.Fatalf("open the fixture file: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() }) //stylegate:ignore test fixture teardown

	n, _, errno := syscall.Syscall(syscall.SYS_FCNTL, owner.Fd(), uintptr(syscall.F_DUPFD), uintptr(releaseFixtureFloor))
	if errno != 0 {
		t.Fatalf("claim a number at or above %d: %v", releaseFixtureFloor, errno)
	}
	if err := syscall.Close(int(n)); err != nil {
		t.Fatalf("release the claim: %v", err)
	}
	if _, err := IdentityOf(int(n)); err == nil {
		t.Fatalf("IdentityOf(%d) answered an identity for a number this process does not hold — an invented identity would "+
			"later compare EQUAL and authorise a close on a stranger", n)
	}
}

// releaseFixtureFloor keeps the fixture's numbers clear of every descriptor this
// process and its children plausibly hold, so the recycle window is deterministic
// rather than lucky (Rule 1: the number is named once).
const releaseFixtureFloor = 900
