package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// Regression-guard constants (Rule 1: named, never inline).
const (
	// proxyProbeTimeout bounds one probe of the gopi binary: the startup path
	// either binds or refuses within milliseconds, so this window is only ever
	// paid by a real hang.
	proxyProbeTimeout = 30 * time.Second
	// proxyProbeExitRefused is the exit status the CLI uses for every refusal.
	proxyProbeExitRefused = 1
	// proxyProbeLoopback is the address the probe binds when the test decides
	// the port; 127.0.0.1:0 lets the kernel choose one nothing else holds.
	proxyProbeLoopback = "127.0.0.1:0"
	// proxyProbePortRe extracts the port out of a listening banner.
	proxyProbePortRe = `(` + proxyListeningPrefix + `)([0-9.:\[\]]+):([0-9]+)`
)

// proxyProbeArgs is the argv every probe uses: a complete, admitted flag set
// whose upstream is deliberately unreachable (127.0.0.1:1), because these
// cases are about the LISTENER — the bind, its ordering, and the socket it
// opens. Translation is internal/proxy's own coverage.
//
// THE FAMILY DOOR IS APPLIED HERE, IN THE PRODUCER, NOT AT THE exec CALL SITES
// (Rule 13: one definition, referenced everywhere). CliVerbProxy is the RETIRED
// flat spelling: driving it at argv[0] prints the retirement notice and exits
// proxyProbeExitRefused, so a driver that types it never reaches the code under
// test. Mapping here means the two drivers below cannot disagree — the defect
// this guards against was exactly that: proxyStartProbe mapped the door and
// proxyRunProbe did not, so the same argv produced a live proxy for one case
// and a refusal for the other. A caller now has no spelling to get wrong.
func proxyProbeArgs(listen string) []string {
	return familyAddressArgs([]string{
		CliVerbProxy,
		"--upstream", "openai",
		"--openai-base-url", "http://127.0.0.1:1/v1",
		"--openai-model", "stub-model",
		"--openai-key", "stub-key",
		"--listen", listen,
	})
}

// TestProxyBusyPortNeverClaimsListening is the regression guard for the
// ordering defect: the startup banner used to be printed BEFORE the socket was
// bound, so a doomed start announced "listening on 127.0.0.1:PORT" and then
// died with `bind: address already in use` (rc=1). The claim preceded the fact
// it stated, which is a lie an operator acts on.
//
// The test holds the port itself with an ordinary listener (Rule 27: no
// synthetic pressure — a bound socket is the whole mechanism), runs the real
// binary against it, and asserts that the ONE line the startup path may print
// on a refusal is the bind error: no "listening on", no "listening" of any
// spelling.
func TestProxyBusyPortNeverClaimsListening(t *testing.T) {
	bin := gopiTestBinPath(t)
	holder, err := net.Listen("tcp", proxyProbeLoopback)
	if err != nil {
		t.Fatalf("hold a port for the busy case: %v", err)
	}
	defer func() {
		if err := holder.Close(); err != nil {
			t.Errorf("release the held port: %v", err)
		}
	}()
	addr := holder.Addr().String()

	var stderr bytes.Buffer
	exit := proxyRunProbe(t, bin, addr, &stderr)
	if exit != proxyProbeExitRefused {
		t.Fatalf("a busy port must be refused with exit %d, got %d\nstderr:\n%s",
			proxyProbeExitRefused, exit, stderr.String())
	}
	out := stderr.String()
	if strings.Contains(out, proxyListeningPrefix) {
		t.Fatalf("the proxy claimed readiness for a port it never bound (%q) — the banner precedes the fact:\n%s",
			addr, out)
	}
	if strings.Contains(strings.ToLower(out), "listening") {
		t.Fatalf("a refused start printed a listening claim in some other spelling:\n%s", out)
	}
	if !strings.Contains(out, "bind") {
		t.Fatalf("a refused start must report the bind failure, got:\n%s", out)
	}
	if !strings.Contains(out, addr) {
		t.Fatalf("the refusal must name the address it could not bind (%s), got:\n%s", addr, out)
	}
}

// TestProxyBannerNamesBoundSocket is the other half of the guard: the banner is
// still printed when the bind SUCCEEDS, and it names the socket the kernel
// actually bound — the port from the banner accepts a real HTTP request, so the
// sentence is true exactly when it is printed.
func TestProxyBannerNamesBoundSocket(t *testing.T) {
	bin := gopiTestBinPath(t)
	// :0 makes the kernel choose the port, so the banner cannot be right by
	// echoing back the literal the operator typed.
	cmd, stderr := proxyStartProbe(t, bin, proxyProbeLoopback)
	defer proxyStopProbe(t, cmd)

	addr := proxyWaitForBanner(t, stderr)
	if strings.Contains(addr, ":0") {
		t.Fatalf("the banner repeated the requested %q instead of the bound port: %s",
			proxyProbeLoopback, addr)
	}
	resp, err := http.Post("http://"+addr+"/v1/messages", "application/json",
		strings.NewReader(`{"model":"stub-model","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("the banner promised %s but no HTTP request is served there: %v\n%s",
			addr, err, stderr.text())
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close the proxy response body: %v", err)
		}
	}()
	// Any HTTP status proves the proxy answered on that socket; the upstream is
	// unreachable on purpose, so an error status is the expected answer.
	if resp.StatusCode == 0 {
		t.Fatalf("no HTTP status from %s — the proxy did not serve the bound socket", addr)
	}
}

// proxyRunProbe runs the binary to completion and returns its exit status.
func proxyRunProbe(t *testing.T, bin, listen string, stderr *bytes.Buffer) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), proxyProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, proxyProbeArgs(listen)...)
	cmd.Env = cliContractEnv(t.TempDir())
	cmd.Stderr = stderr
	err := cmd.Run()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	t.Fatalf("run %s %v: %v", bin, proxyProbeArgs(listen), err)
	return -1
}

// proxyStartProbe starts the binary and leaves it serving. stderr is a pipe the
// caller reads the banner from, so the ordering claim is observable in the
// child's own output rather than reconstructed after the fact.
func proxyStartProbe(t *testing.T, bin, listen string) (*exec.Cmd, *proxyProbeStderr) {
	t.Helper()
	// No familyAddressArgs here: proxyProbeArgs already carries the door, so
	// this driver and proxyRunProbe build the SAME argv (Rule 13).
	cmd := exec.Command(bin, proxyProbeArgs(listen)...)
	cmd.Env = cliContractEnv(t.TempDir())
	stderr := &proxyProbeStderr{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}
	return cmd, stderr
}

// proxyWaitForBanner blocks until the child prints its readiness banner and
// returns the address the banner names.
func proxyWaitForBanner(t *testing.T, stderr *proxyProbeStderr) string {
	t.Helper()
	re := regexp.MustCompile(proxyProbePortRe)
	deadline := time.Now().Add(proxyProbeTimeout)
	for {
		if m := re.FindStringSubmatch(stderr.text()); m != nil {
			host := m[2]
			if host == "" {
				host = "[::]"
			}
			return net.JoinHostPort(host, m[3])
		}
		if time.Now().After(deadline) {
			t.Fatalf("the proxy never printed its listening banner:\n%s", stderr.text())
		}
		time.Sleep(proxyProbePollInterval) //stylegate:allow-sleep polling another PROCESS's stderr cannot be notified in-process
	}
}

// proxyStopProbe ends a serving child and reaps it.
func proxyStopProbe(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd.Process == nil {
		return
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Errorf("kill the proxy probe: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Errorf("reap the proxy probe: %v", err)
		}
	}
}

// proxyProbeStderr accumulates a child's stderr as it is written, so a test can
// poll for a line while the child keeps running. It is the io.Writer handed to
// exec.Cmd.Stderr (no pipe-reader goroutine of its own: the Write calls come
// from the child's own os/exec copy goroutine, and the mutex is what makes the
// counter safe to read from the test goroutine).
type proxyProbeStderr struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends one chunk of the child's stderr. It never fails: an in-memory
// buffer is the whole destination.
func (s *proxyProbeStderr) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// text returns what the child has written so far.
func (s *proxyProbeStderr) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// proxyProbePollInterval is how often a probe re-reads the accumulated output.
const proxyProbePollInterval = 20 * time.Millisecond

// TestParseProxyArgs_UnknownFlagSingleDash guards the cosmetic double-dash
// bug: the token reported by the "unknown flag" error already carries a
// leading dash (only tokens starting with '-' are classified as unknown
// flags), so prepending "--" produced a misleading "unknown flag ---foo".
// The error must read "unknown flag --foo" (exactly two dashes).
func TestParseProxyArgs_UnknownFlagSingleDash(t *testing.T) {
	_, err := parseProxyArgs([]string{"--upstream", "openai", "--foo"})
	if err == nil {
		t.Fatalf("expected an error for unknown flag --foo, got nil")
	}
	want := "unknown flag --foo"
	if err.Error() != want {
		t.Fatalf("expected error %q, got %q (triple-dash regression?)", want, err.Error())
	}
	if !strings.Contains(err.Error(), "unknown flag --foo") {
		t.Fatalf("error %q does not contain single-dash flag %q", err.Error(), want)
	}
	if strings.Contains(err.Error(), "---foo") {
		t.Fatalf("error %q contains the misleading triple-dash form; dash stripped twice", err.Error())
	}
}

// TestParseProxyArgs_ValidArgs guards the happy path so the flag fix doesn't
// break normal parsing.
func TestParseProxyArgs_ValidArgs(t *testing.T) {
	opts, err := parseProxyArgs([]string{"--upstream", "openai", "--openai-key", "sekret", "--verbose"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.upstream != "openai" {
		t.Errorf("expected upstream openai, got %q", opts.upstream)
	}
	if opts.openaiKey != "sekret" {
		t.Errorf("expected openai key sekret, got %q", opts.openaiKey)
	}
	if !opts.verbose {
		t.Errorf("expected verbose=true")
	}
}
