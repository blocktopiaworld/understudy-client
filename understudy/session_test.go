package understudy

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blocktopiaworld/understudy-client/internal/entities"
	"github.com/blocktopiaworld/understudy-client/internal/inventory"
	"github.com/blocktopiaworld/understudy-client/internal/world"
	"github.com/blocktopiaworld/understudy-client/protocol"
)

// fakeServer is the far end of an in-memory connection: it records everything
// the client sends and lets a test push packets back.
//
// This is what makes the session behaviour testable at all. Connect, the idle
// position loop and the teleport settle gate are all defined by *what goes on
// the wire and when*, and none of that is reachable while the only way to get
// a connected Client is to run a Minecraft server.
type fakeServer struct {
	conn *protocol.Conn

	mu   sync.Mutex
	sent []protocol.Packet
}

// newSession wires a Client to a fakeServer over net.Pipe and starts draining
// whatever the client sends.
func newSession(t *testing.T) (*Client, *fakeServer) {
	t.Helper()
	clientSide, serverSide := net.Pipe()
	t.Cleanup(func() {
		_ = clientSide.Close()
		_ = serverSide.Close()
	})

	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := &Client{
		opts: Options{
			Username:    "TestBot",
			ReadTimeout: 5 * time.Second,
			Logger:      discard,
		},
		v:        testVersion(t),
		log:      discard,
		uuid:     protocol.OfflineUUID("TestBot"),
		entities: entities.New(),
		effects:  newEffectSet(),
		gameMode: GameModeUnknown,
		world:    world.New(),
		inv:      inventory.New(),
		window:   inventory.NewContainer(),
		conn:     protocol.NewConn(clientSide),
		state:    protocol.StatePlay,
	}
	s := &fakeServer{conn: protocol.NewConn(serverSide)}
	go s.drain()
	return c, s
}

// settled returns a session whose teleport window has already passed, so a
// verb under test is not accidentally measuring the settle gate.
func settled(t *testing.T) (*Client, *fakeServer) {
	t.Helper()
	c, s := newSession(t)
	c.lastTeleportEcho.Store(time.Now().Add(-2 * TeleportSettle).UnixNano())
	return c, s
}

// drain reads until the connection closes, recording every packet.
func (s *fakeServer) drain() {
	for {
		p, err := s.conn.ReadPacket()
		if err != nil {
			return
		}
		// Copy the payload: the connection reuses its frame buffer.
		data := make([]byte, len(p.Data))
		copy(data, p.Data)

		s.mu.Lock()
		s.sent = append(s.sent, protocol.Packet{ID: p.ID, Data: data})
		s.mu.Unlock()
	}
}

// received returns every packet the client has sent so far.
func (s *fakeServer) received() []protocol.Packet {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]protocol.Packet, len(s.sent))
	copy(out, s.sent)
	return out
}

// countOf returns how many packets of an ID the client has sent.
func (s *fakeServer) countOf(id int32) int {
	n := 0
	for _, p := range s.received() {
		if p.ID == id {
			n++
		}
	}
	return n
}

// first returns the first packet of an ID, for asserting on its fields.
func (s *fakeServer) first(t *testing.T, id int32, what string) protocol.Packet {
	t.Helper()
	for _, p := range s.received() {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("no %s packet (id %d) was sent", what, id)
	return protocol.Packet{}
}

// reset forgets everything recorded so far.
func (s *fakeServer) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = nil
}

// waitFor polls until cond holds or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, what)
}

// recorded gives the fake server's reader a moment to catch up with packets the
// client has already written, for assertions that count what it sent.
//
// It reports nothing and fails nothing: the assertion that follows is the one
// that knows what it wanted and how to say so.
func recorded(cond func() bool) {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// teleportPacket builds a server teleport, as the play state would send one.
func teleportPacket(v *protocol.Version, id int32, x, y, z float64) []byte {
	return protocol.NewWriter(v.Packets.CBPlayPosition).
		VarInt(id).
		F64(x).F64(y).F64(z).
		F64(0).F64(0).F64(0).
		F32(0).F32(0).
		// Flags is an int, not a byte. It was written as a byte here for as
		// long as nothing read it; the moment the client did, this fixture was
		// three bytes short and every teleport in these tests stopped
		// confirming.
		I32(0).
		Bytes()
}

// run starts the read loop and returns a cancel func.
func run(t *testing.T, c *Client) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = c.Run(ctx) }()
	return cancel
}

// A teleport has to be answered with both a confirm and a position echo, or
// the server resends it and refuses to accept the player's movement.
func TestTeleportIsAcknowledged(t *testing.T) {
	c, s := newSession(t)
	run(t, c)

	if err := s.conn.WritePacket(teleportPacket(c.v, 7, 10, 64, -20)); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}

	waitFor(t, 2*time.Second, "the teleport confirm", func() bool {
		return s.countOf(c.v.Packets.SBPlayTeleportConfirm) > 0
	})
	waitFor(t, 2*time.Second, "the position echo", func() bool {
		return s.countOf(c.v.Packets.SBPlayPositionLook) > 0
	})

	// The confirm must carry the teleport's own id, or the server keeps
	// waiting for one that matches.
	confirm := s.first(t, c.v.Packets.SBPlayTeleportConfirm, "teleport confirm")
	if got := confirm.Reader().VarInt(); got != 7 {
		t.Errorf("teleport confirm carried id %d, want 7", got)
	}

	if pos := c.Position(); pos.X != 10 || pos.Y != 64 || pos.Z != -20 {
		t.Errorf("Position() = %+v, want (10,64,-20)", pos)
	}
	// Claiming to be airborne is what makes a server treat a bot as flying.
	if !c.OnGround() {
		t.Error("OnGround() = false straight after a teleport, want true")
	}
	if c.Corrections() != 1 {
		t.Errorf("Corrections() = %d, want 1", c.Corrections())
	}
}

// A real client sends a movement packet roughly twenty times a second whether
// or not the player moved. A bot that only speaks when it has somewhere to be
// is silent for most of a session, which the server can see.
func TestIdlePositionLoopReportsEveryTick(t *testing.T) {
	c, s := newSession(t)
	run(t, c)

	// The loop starts on the first teleport: before that there is no position
	// to report and 0,0,0 is a lie the server would have to correct.
	if err := s.conn.WritePacket(teleportPacket(c.v, 1, 0, 64, 0)); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	waitFor(t, 2*time.Second, "the teleport echo", func() bool {
		return s.countOf(c.v.Packets.SBPlayPositionLook) > 0
	})

	// Assert the behaviour, not a wall-clock rate. Every write here is a
	// synchronous handoff over an unbuffered pipe, so counting packets in a
	// fixed window fails on a single scheduling hiccup while proving nothing
	// extra: a loop that is absent never reaches the target at all, and one
	// that is present reaches it in roughly wanted*TickRate.
	s.reset()
	const wanted = 5
	waitFor(t, 5*time.Second, "the idle loop to report unprompted", func() bool {
		return s.countOf(c.v.Packets.SBPlayPositionLook) >= wanted
	})

	// Nothing prompted those: no teleport, no verb, no movement. The only
	// thing that could have sent them is the heartbeat.
	got := s.countOf(c.v.Packets.SBPlayPositionLook)
	if got < wanted {
		t.Errorf("idle loop sent %d position packets, want at least %d", got, wanted)
	}

	// And it must not be a busy loop. Over a further window it should stay in
	// the same order of magnitude as the tick rate rather than spinning.
	s.reset()
	const window = 300 * time.Millisecond
	time.Sleep(window)
	if burst := s.countOf(c.v.Packets.SBPlayPositionLook); burst > int(window/TickRate)*5 {
		t.Errorf("idle loop sent %d packets in %v, want nearer %d — this is a busy loop",
			burst, window, int(window/TickRate))
	}
}

func TestIdlePositionLoopCanBeDisabled(t *testing.T) {
	c, s := newSession(t)
	c.opts.DisableIdlePosition = true
	run(t, c)

	if err := s.conn.WritePacket(teleportPacket(c.v, 1, 0, 64, 0)); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	waitFor(t, 2*time.Second, "the teleport echo", func() bool {
		return s.countOf(c.v.Packets.SBPlayPositionLook) > 0
	})

	s.reset()
	time.Sleep(300 * time.Millisecond)
	if got := s.countOf(c.v.Packets.SBPlayPositionLook); got != 0 {
		t.Errorf("sent %d position packets with the idle loop disabled, want 0", got)
	}
}

// The idle loop must yield to the action verbs rather than interleaving a
// stale position into the middle of a walk or a descent.
func TestIdlePositionLoopYieldsToActions(t *testing.T) {
	c, _ := newSession(t)
	c.markPositionSent()
	if got := c.sincePositionSent(); got >= TickRate {
		t.Errorf("sincePositionSent() = %v straight after a send, want under %v", got, TickRate)
	}

	c.lastPositionSent.Store(time.Now().Add(-time.Second).UnixNano())
	if got := c.sincePositionSent(); got < TickRate {
		t.Errorf("sincePositionSent() = %v a second later, want at least %v", got, TickRate)
	}
}

// The gate exists because the server silently ignores player_action while a
// teleport is settling — which is why a one-shot place used to vanish while
// mining retried through the window by accident.
func TestBlockActionsWaitForTheTeleportToSettle(t *testing.T) {
	c, s := newSession(t)
	cancel := run(t, c)
	defer cancel()

	if err := s.conn.WritePacket(teleportPacket(c.v, 1, 0, 64, 0)); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	waitFor(t, 2*time.Second, "the teleport confirm", func() bool {
		return s.countOf(c.v.Packets.SBPlayTeleportConfirm) > 0
	})

	// A dig issued immediately must answer the server before it acts: the state
	// being waited on is "awaiting position from client", so the gate sends a
	// position rather than sleeping until one happens along.
	before := s.countOf(c.v.Packets.SBPlayPositionLook)
	start := time.Now()
	if err := c.StartDig(context.Background(), 0, 63, 0, 1); err != nil {
		t.Fatalf("StartDig: %v", err)
	}
	elapsed := time.Since(start)

	// Both counts below race the fake server's reader: StartDig returns once the
	// packets are on the socket, which is before the other end has read and
	// recorded them. So give the recorder a moment to catch up before reading
	// its slice — this waits on the test's own bookkeeping, never on the client,
	// and the assertions still fire with their own messages if nothing arrives.
	// Sampling it the instant StartDig returned failed about one run in five,
	// always on the dig: it is the last thing written, so it is the one still in
	// flight.
	recorded(func() bool {
		return s.countOf(c.v.Packets.SBPlayPositionLook) > before &&
			s.countOf(c.v.Packets.SBPlayBlockDig) > 0
	})

	if after := s.countOf(c.v.Packets.SBPlayPositionLook); after <= before {
		t.Errorf("StartDig sent no position packet (%d then %d), want the gate to answer the "+
			"server rather than wait for it", before, after)
	}
	if s.countOf(c.v.Packets.SBPlayBlockDig) == 0 {
		t.Error("no block_dig packet reached the server")
	}
	// It still waits — the server needs a tick to act on that position — but a
	// tick, not the whole window. Sleeping the window out cost 346ms a time
	// across eleven repositions per field, which is what made this worth fixing.
	//
	// The bound here is deliberately loose. An earlier version asserted the
	// elapsed time was under half the settle window, which held in isolation
	// and failed under the load of the whole suite running with -race — a
	// timing assertion measuring the machine rather than the code. What
	// actually distinguishes the fix from the bug is the position packet
	// above: sleeping the window out sends nothing.
	if elapsed < TickRate/2 {
		t.Errorf("StartDig returned after %v, want it to give the server ~%v to act", elapsed, TickRate)
	}
	if elapsed > 4*TeleportSettle {
		t.Errorf("StartDig took %v, which is far past even the old %v window",
			elapsed, TeleportSettle)
	}

	// A second action costs nothing: the server has our position already.
	start = time.Now()
	if err := c.StartDig(context.Background(), 0, 63, 0, 1); err != nil {
		t.Fatalf("second StartDig: %v", err)
	}
	// Generous for the same reason: what matters is that it does not wait
	// again, not the exact microseconds under a race detector.
	if elapsed := time.Since(start); elapsed > TickRate {
		t.Errorf("the second StartDig took %v, want ~0 — the server is already answered", elapsed)
	}
}

func TestKeepAliveIsAnswered(t *testing.T) {
	c, s := newSession(t)
	run(t, c)

	const id int64 = 0x0102030405060708
	if err := s.conn.WritePacket(
		protocol.NewWriter(c.v.Packets.CBPlayKeepAlive).I64(id).Bytes()); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	waitFor(t, 2*time.Second, "the keep-alive reply", func() bool {
		return s.countOf(c.v.Packets.SBPlayKeepAlive) > 0
	})
	reply := s.first(t, c.v.Packets.SBPlayKeepAlive, "keep-alive")
	if got := reply.Reader().I64(); got != id {
		t.Errorf("keep-alive reply carried %#x, want %#x", got, id)
	}
}

// A dead bot silently ignores actions, so a death that goes unanswered is a
// session that quietly stops making progress.
func TestDeathTriggersRespawn(t *testing.T) {
	c, s := newSession(t)
	run(t, c)

	if err := s.conn.WritePacket(
		protocol.NewWriter(c.v.Packets.CBPlayDeathCombatEvent).Bytes()); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	waitFor(t, 2*time.Second, "the respawn request", func() bool {
		return s.countOf(c.v.Packets.SBPlayClientCommand) > 0
	})
	if !c.Dead() {
		t.Error("Dead() = false after a death packet, want true")
	}
	if c.Deaths() != 1 {
		t.Errorf("Deaths() = %d, want 1", c.Deaths())
	}
}

func TestDeathCanBeLeftUnanswered(t *testing.T) {
	c, s := newSession(t)
	c.opts.DisableAutoRespawn = true
	run(t, c)

	if err := s.conn.WritePacket(
		protocol.NewWriter(c.v.Packets.CBPlayDeathCombatEvent).Bytes()); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	waitFor(t, 2*time.Second, "the death to register", func() bool { return c.Dead() })

	time.Sleep(100 * time.Millisecond)
	if got := s.countOf(c.v.Packets.SBPlayClientCommand); got != 0 {
		t.Errorf("sent %d respawn requests with auto-respawn disabled, want 0", got)
	}
}

// A respawn can cross dimensions, and neither entity IDs nor chunk coordinates
// survive that.
func TestRespawnClearsEntitiesAndTerrain(t *testing.T) {
	c, s := newSession(t)
	c.entities.Spawn(&Entity{ID: 1, TypeName: "minecraft:pig"})
	c.world.Store(emptyColumn(0, 0))
	run(t, c)

	if err := s.conn.WritePacket(
		protocol.NewWriter(c.v.Packets.CBPlayRespawn).Bytes()); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	waitFor(t, 2*time.Second, "the world to be cleared", func() bool {
		return c.LoadedChunks() == 0 && len(c.entities.All()) == 0
	})
	if c.Dead() {
		t.Error("Dead() = true after respawning, want false")
	}
}

func TestHealthIsTracked(t *testing.T) {
	c, s := newSession(t)
	run(t, c)

	if err := s.conn.WritePacket(
		protocol.NewWriter(c.v.Packets.CBPlayUpdateHealth).F32(7.5).VarInt(12).Bytes()); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	waitFor(t, 2*time.Second, "the health update", func() bool {
		h, _ := c.Health()
		return h == 7.5
	})
	if h, food := c.Health(); h != 7.5 || food != 12 {
		t.Errorf("Health() = %g, %d; want 7.5, 12", h, food)
	}
}

func TestJoinRecordsTheEntityID(t *testing.T) {
	c, s := newSession(t)
	run(t, c)

	if err := s.conn.WritePacket(
		protocol.NewWriter(c.v.Packets.CBPlayLogin).I32(4242).Bytes()); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	waitFor(t, 2*time.Second, "the join", func() bool { return c.Joined() })
	if got := c.EntityID(); got != 4242 {
		t.Errorf("EntityID() = %d, want 4242", got)
	}
}

func TestRunRejectsBeingCalledBeforePlay(t *testing.T) {
	c := newTestClient(t)
	c.setState(protocol.StateLogin)
	if err := c.Run(context.Background()); err == nil {
		t.Error("Run before the play state = nil error, want an error")
	}
}

// A kick is the most important thing this client ever reports, and the reason
// arrives as NBT rather than a plain string.
func TestKickReportsAReadableReason(t *testing.T) {
	c, s := newSession(t)
	errCh := make(chan error, 1)
	go func() { errCh <- c.Run(context.Background()) }()

	payload := protocol.NewWriter(c.v.Packets.CBPlayKickDisconnect).U8(0x0a).U8(0x00).Bytes()
	payload = append(payload, []byte("multiplayer.disconnect.flying")...)
	payload = append(payload, 0x00)
	if err := s.conn.WritePacket(payload); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}

	select {
	case err := <-errCh:
		wantErrContaining(t, err, "Run after a kick", "multiplayer.disconnect.flying")
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after a kick")
	}
}

// Close must wait for the client's own goroutines, or an auto-fall keeps
// writing into a closed socket after the caller has moved on.
func TestCloseWaitsForBackgroundWork(t *testing.T) {
	c, _ := newSession(t)
	started := make(chan struct{})
	finished := make(chan struct{})
	c.background(func() {
		close(started)
		time.Sleep(80 * time.Millisecond)
		close(finished)
	})
	<-started

	if err := c.Close(); err != nil {
		t.Logf("Close: %v (expected for a pipe)", err)
	}
	select {
	case <-finished:
	default:
		t.Error("Close returned while a background goroutine was still running")
	}
}

// An unhandled packet is skipped by its length prefix and costs nothing — that
// is what lets this client decode a couple of dozen of the 141 clientbound
// play packets and ignore the rest.
func TestUnknownPacketsAreSkipped(t *testing.T) {
	c, s := newSession(t)
	run(t, c)

	// A packet ID this client never handles, with a body that would be
	// nonsense if decoded.
	if err := s.conn.WritePacket(
		protocol.NewWriter(0x7e).String(strings.Repeat("x", 100)).Bytes()); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	// The session must still answer a keep-alive afterwards.
	if err := s.conn.WritePacket(
		protocol.NewWriter(c.v.Packets.CBPlayKeepAlive).I64(1).Bytes()); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	waitFor(t, 2*time.Second, "the keep-alive after an unknown packet", func() bool {
		return s.countOf(c.v.Packets.SBPlayKeepAlive) > 0
	})
}
