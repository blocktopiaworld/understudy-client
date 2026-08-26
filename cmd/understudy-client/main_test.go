package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func devNull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestParseFlagsDefaults(t *testing.T) {
	cfg, err := parseFlags(nil, devNull(t))
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.addr != "127.0.0.1:25565" {
		t.Errorf("addr = %q, want the local default", cfg.addr)
	}
	if cfg.username != "Understudy" {
		t.Errorf("username = %q, want Understudy", cfg.username)
	}
	if cfg.hold != 15*time.Second {
		t.Errorf("hold = %v, want 15s", cfg.hold)
	}
	// Empty means auto-detect: ping the server and adopt whatever it reports,
	// which is what makes one binary usable against a fleet.
	if cfg.version != "" {
		t.Errorf("version = %q, want empty (auto-detect)", cfg.version)
	}
	if cfg.noRespawn || cfg.noIdlePos || cfg.trace || cfg.debug {
		t.Errorf("a boolean flag defaulted to true: %+v", cfg)
	}
}

func TestParseFlagsOverrides(t *testing.T) {
	cfg, err := parseFlags([]string{
		"-addr", "example.com:1234",
		"-username", "Tester",
		"-hold", "30s",
		"-version", "26.1",
		"-control", "8080",
		"-no-respawn",
		"-no-idle-position",
		"-trace",
		"-debug",
	}, devNull(t))
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.addr != "example.com:1234" || cfg.username != "Tester" {
		t.Errorf("addr/username = %q/%q", cfg.addr, cfg.username)
	}
	if cfg.hold != 30*time.Second {
		t.Errorf("hold = %v, want 30s", cfg.hold)
	}
	if cfg.control != "8080" {
		t.Errorf("control = %q, want 8080", cfg.control)
	}
	if !cfg.noRespawn || !cfg.noIdlePos || !cfg.trace || !cfg.debug {
		t.Errorf("a boolean flag did not take: %+v", cfg)
	}
}

func TestParseFlagsRejectsUnknownFlags(t *testing.T) {
	if _, err := parseFlags([]string{"-nonsense"}, devNull(t)); err == nil {
		t.Error("parseFlags with an unknown flag = nil error, want an error")
	}
}

// --versions must not need a server, so it has to be handled before anything
// tries to connect.
func TestRunListsVersionsWithoutConnecting(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "versions")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer tmp.Close()

	if err := run([]string{"-versions", "-addr", "127.0.0.1:1"}, tmp); err != nil {
		t.Fatalf("run -versions: %v", err)
	}
	out, err := os.ReadFile(tmp.Name())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(out), "26.1") {
		t.Errorf("-versions printed %q, want the supported version list", out)
	}
}

// A failed connect must be reported as an error rather than exiting, so the
// deferred cleanup in run actually gets to happen. os.Exit does not unwind the
// stack, and calling it from the middle of main skipped the connection close.
func TestRunReportsAConnectFailure(t *testing.T) {
	// Port 1 is not listening, and an explicit version skips the ping.
	err := run([]string{"-addr", "127.0.0.1:1", "-version", "26.1", "-hold", "1s"}, devNull(t))
	if err == nil {
		t.Fatal("run against a dead port = nil error, want a connect failure")
	}
	if !strings.Contains(err.Error(), "connect failed") {
		t.Errorf("error = %q, want it to name the connect failure", err)
	}
}

func TestRunRejectsBadConfiguration(t *testing.T) {
	err := run([]string{"-addr", "malformed", "-version", "26.1"}, devNull(t))
	if err == nil {
		t.Fatal("run with a malformed addr = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "configuration error") {
		t.Errorf("error = %q, want it flagged as a configuration error", err)
	}
}

func TestRunRejectsAnUnknownVersion(t *testing.T) {
	err := run([]string{"-addr", "127.0.0.1:1", "-version", "not-a-version"}, devNull(t))
	if err == nil {
		t.Fatal("run with an unknown version = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "configuration error") {
		t.Errorf("error = %q, want it flagged as a configuration error", err)
	}
}
