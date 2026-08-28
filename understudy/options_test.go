package understudy

import (
	"testing"
	"time"

	"github.com/block-topia/understudy-client/protocol"
)

func TestNewRequiresAddrAndUsername(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"no addr", Options{Username: "Bot"}},
		{"no username", Options{Addr: "127.0.0.1:25565"}},
		{"neither", Options{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.opts); err == nil {
				t.Errorf("New(%+v) = nil error, want a validation error", tc.opts)
			}
		})
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	c, err := New(Options{Addr: "play.example.com:25566", Username: "Bot"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.opts.DialTimeout != DefaultDialTimeout {
		t.Errorf("DialTimeout = %v, want %v", c.opts.DialTimeout, DefaultDialTimeout)
	}
	if c.opts.ReadTimeout != DefaultReadTimeout {
		t.Errorf("ReadTimeout = %v, want %v", c.opts.ReadTimeout, DefaultReadTimeout)
	}
	if c.opts.Logger == nil {
		t.Error("Logger is nil, want slog.Default()")
	}
	// Host and Port are what the handshake advertises, derived from Addr when
	// not given: servers may route on these.
	if c.opts.Host != "play.example.com" || c.opts.Port != 25566 {
		t.Errorf("Host/Port = %q/%d, want play.example.com/25566", c.opts.Host, c.opts.Port)
	}
}

// A BungeeCord or virtual-host setup dials one address and advertises another.
func TestNewKeepsExplicitHostAndPort(t *testing.T) {
	c, err := New(Options{
		Addr: "10.0.0.5:25565", Host: "play.example.com", Port: 25565, Username: "Bot",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.opts.Host != "play.example.com" {
		t.Errorf("Host = %q, want the explicit value", c.opts.Host)
	}
}

func TestNewRejectsAMalformedAddr(t *testing.T) {
	if _, err := New(Options{Addr: "not-an-address", Username: "Bot"}); err == nil {
		t.Error("New with a malformed addr = nil error, want an error")
	}
}

func TestNewResolvesAnExplicitVersion(t *testing.T) {
	names := protocol.Names()
	if len(names) == 0 {
		t.Skip("no generated versions registered")
	}
	c, err := New(Options{Addr: "127.0.0.1:25565", Username: "Bot", Version: names[0]})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Version() == nil || c.Version().Name != names[0] {
		t.Errorf("Version() = %v, want %q", c.Version(), names[0])
	}
}

func TestNewRejectsAnUnknownVersion(t *testing.T) {
	if _, err := New(Options{
		Addr: "127.0.0.1:25565", Username: "Bot", Version: "not-a-version",
	}); err == nil {
		t.Error("New with an unknown version = nil error, want an error")
	}
}

// Auto-detection leaves the version unresolved until Connect, which matters
// because callers key assertions on Version().
func TestNewLeavesVersionNilForAutoDetect(t *testing.T) {
	c, err := New(Options{Addr: "127.0.0.1:25565", Username: "Bot"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Version() != nil {
		t.Errorf("Version() = %v before Connect with auto-detect, want nil", c.Version())
	}
}

// On an offline-mode server the UUID is the player's identity, so a bot that
// derives the wrong one plays correctly while anything checking up on it looks
// at a player who does not exist.
func TestNewDerivesTheOfflineUUID(t *testing.T) {
	c, err := New(Options{Addr: "127.0.0.1:25565", Username: "Understudy"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.UUID() != protocol.OfflineUUID("Understudy") {
		t.Errorf("UUID() = %s, want the derived offline UUID", c.UUID())
	}
	if c.Username() != "Understudy" {
		t.Errorf("Username() = %q, want Understudy", c.Username())
	}
}

func TestNewDoesNotDial(t *testing.T) {
	// Nothing is listening on this port; New must still succeed.
	if _, err := New(Options{Addr: "127.0.0.1:1", Username: "Bot"}); err != nil {
		t.Errorf("New = %v, want nil — New must not perform I/O", err)
	}
}

func TestCloseOnAnUnconnectedClientIsSafe(t *testing.T) {
	c, err := New(Options{Addr: "127.0.0.1:25565", Username: "Bot"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close() before Connect = %v, want nil", err)
	}
}

func TestOptionsWithDefaultsPreservesExplicitTimeouts(t *testing.T) {
	opts, err := Options{
		Addr: "127.0.0.1:25565", Username: "Bot",
		DialTimeout: 3 * time.Second, ReadTimeout: 7 * time.Second,
	}.withDefaults()
	if err != nil {
		t.Fatalf("withDefaults: %v", err)
	}
	if opts.DialTimeout != 3*time.Second || opts.ReadTimeout != 7*time.Second {
		t.Errorf("timeouts = %v/%v, want the explicit 3s/7s", opts.DialTimeout, opts.ReadTimeout)
	}
}

func TestSplitHostPort(t *testing.T) {
	for _, tc := range []struct {
		addr     string
		wantHost string
		wantPort uint16
		wantErr  bool
	}{
		{addr: "127.0.0.1:25565", wantHost: "127.0.0.1", wantPort: 25565},
		{addr: "play.example.com:25566", wantHost: "play.example.com", wantPort: 25566},
		{addr: "[::1]:25565", wantHost: "::1", wantPort: 25565},
		{addr: "no-port", wantErr: true},
		{addr: "host:not-a-port", wantErr: true},
		{addr: "host:99999", wantErr: true},
	} {
		t.Run(tc.addr, func(t *testing.T) {
			host, port, err := splitHostPort(tc.addr)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("splitHostPort(%q) err = %v, want error: %v", tc.addr, err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if host != tc.wantHost || port != tc.wantPort {
				t.Errorf("splitHostPort(%q) = %q, %d; want %q, %d",
					tc.addr, host, port, tc.wantHost, tc.wantPort)
			}
		})
	}
}
