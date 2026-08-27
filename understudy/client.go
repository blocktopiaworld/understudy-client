package understudy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blocktopia/understudy-client/internal/entities"
	"github.com/blocktopia/understudy-client/internal/inventory"
	"github.com/blocktopia/understudy-client/internal/nbt"
	"github.com/blocktopia/understudy-client/internal/world"
	"github.com/blocktopia/understudy-client/protocol"
)

// Default timeouts, applied by New when Options leaves them zero.
const (
	// DefaultDialTimeout bounds the TCP connect.
	DefaultDialTimeout = 10 * time.Second
	// DefaultReadTimeout bounds any single read once connected. It must
	// comfortably exceed the server's keep-alive interval (vanilla: 15s) or a
	// healthy idle connection is torn down as if it had stalled.
	DefaultReadTimeout = 60 * time.Second
)

// Options configures a Client.
type Options struct {
	// Addr is the server's host:port.
	Addr string
	// Host and Port are what the handshake advertises. Servers may route on
	// these (virtual hosts, BungeeCord), so they are kept separate from the
	// address actually dialled. Defaults are derived from Addr.
	Host string
	Port uint16
	// Username is the offline-mode player name. The UUID is derived from it.
	Username string
	// Version is the Minecraft version to speak, e.g. "26.1". Empty means
	// auto-detect: the client pings the server first and adopts whatever
	// protocol it reports, which is what makes one binary usable against a
	// fleet of servers on different versions.
	Version string
	// DialTimeout bounds the TCP connect. Defaults to DefaultDialTimeout.
	DialTimeout time.Duration
	// ReadTimeout bounds any single read. Defaults to DefaultReadTimeout.
	ReadTimeout time.Duration

	// DisableAutoFall stops the bot falling by itself after a teleport.
	//
	// Auto-fall is on by default because teleporting is constant during tests
	// and vanilla kicks a floating player after about four seconds. A bot
	// teleported into mid-air and left there dies mid-run with no warning,
	// which is a failure that has nothing to do with what was being tested.
	//
	// Falling after a teleport is also simply what a real client does. When the
	// bot is already on solid ground the fall detects that within a tick or two
	// and costs ~100ms, so leaving it on is nearly free.
	DisableAutoFall bool

	// DisableAutoRespawn leaves a killed bot on the death screen. Auto-respawn
	// is on by default (the zero value) because without it a dead bot is a
	// *silent* dead end: the connection stays healthy and keep-alives keep
	// flowing, but the server will not position a player who has not
	// respawned, so every later action is quietly ignored and the session
	// simply stops making progress. Bots die routinely — lava, falls, mobs —
	// so this is on by default. It is switchable because anything asserting on
	// death itself needs to observe the corpse.
	DisableAutoRespawn bool

	// DisableIdlePosition stops the bot reporting its position while standing
	// still. See Client.startPositionLoop for why that is almost never what
	// you want.
	DisableIdlePosition bool

	// Logger receives connection lifecycle events. Defaults to slog.Default().
	Logger *slog.Logger

	// OnPacket, if set, observes every decoded clientbound packet.
	//
	// It is called synchronously on the read loop, so a slow callback delays
	// keep-alive replies and can get the bot kicked. Intended for tracing.
	OnPacket func(state protocol.State, p protocol.Packet)
}

// withDefaults fills in the zero-valued options and validates the rest.
func (o Options) withDefaults() (Options, error) {
	if o.Addr == "" {
		return o, errors.New("understudy: Addr is required")
	}
	if o.Username == "" {
		return o, errors.New("understudy: Username is required")
	}
	if o.DialTimeout == 0 {
		o.DialTimeout = DefaultDialTimeout
	}
	if o.ReadTimeout == 0 {
		o.ReadTimeout = DefaultReadTimeout
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Host == "" || o.Port == 0 {
		host, port, err := splitHostPort(o.Addr)
		if err != nil {
			return o, err
		}
		if o.Host == "" {
			o.Host = host
		}
		if o.Port == 0 {
			o.Port = port
		}
	}
	return o, nil
}

// Position is the player's location and facing, as last known from the server.
type Position struct {
	X, Y, Z    float64
	Yaw, Pitch float32
}

// Client is a single bot connection. See the package comment for what may be
// called concurrently.
type Client struct {
	opts     Options
	log      *slog.Logger
	conn     *protocol.Conn
	entities *entities.Tracker
	world    *world.World
	inv      *inventory.Inventory
	window   *inventory.Container
	trades   []TradeOffer
	recipes  map[string]RecipeID
	// recipesMissing counts entries the server sent that could not be decoded.
	// Without it a short book is indistinguishable from a small one, and
	// "no recipe for that" reads the same whether the recipe does not exist or
	// simply never decoded.
	recipesMissing int
	// v holds every version-specific detail: packet IDs, entity and block
	// tables, and the chunk framing flags. Resolved during Connect, since
	// auto-detection needs a round trip to the server, and read-only after.
	v *protocol.Version

	// uuid is the offline-mode UUID derived from Username. It is set before
	// connecting, because callers key assertions on it, and replaced only if
	// the server disagrees — see loginSuccess.
	uuid protocol.UUID

	// mu guards the mutable player state below.
	mu       sync.RWMutex
	state    protocol.State
	entityID int32
	pos      Position
	joined   bool
	health   float32
	food     int32
	dead     bool
	deaths   int
	onGround bool
	heldSlot int
	input    uint8
	// corrections counts server-issued position corrections. See the increment
	// site: it is one of the client's ground-detection signals.
	corrections int

	// falling guards auto-fall against re-entry. A fall provokes the very
	// position corrections that trigger auto-fall, so without this the bot
	// would spawn a new fall for every packet of its own descent.
	falling atomic.Bool

	// loadedSent guards the once-per-session player_loaded packet.
	loadedSent atomic.Bool

	// positionLoop guards the once-per-session idle position goroutine.
	positionLoop atomic.Bool

	// lastPositionSent is when a movement packet last went out, as Unix nanos.
	// The idle loop uses it to stay out of the way of a walk or a fall.
	lastPositionSent atomic.Int64

	// lastTeleportEcho is when the reply to a server teleport last went out, as
	// Unix nanos. See awaitTeleportSettle.
	lastTeleportEcho atomic.Int64

	// blockSequence numbers this connection's block actions. See nextSequence.
	blockSequence atomic.Int32

	// background goroutines (auto-fall, the idle position loop) are tracked so
	// Close can wait for them instead of letting them write to a closed socket.
	wg sync.WaitGroup
}

// New builds a Client. It does not perform any I/O.
func New(opts Options) (*Client, error) {
	opts, err := opts.withDefaults()
	if err != nil {
		return nil, err
	}

	var version *protocol.Version
	if opts.Version != "" {
		if version, err = protocol.ByName(opts.Version); err != nil {
			return nil, err
		}
	}

	return &Client{
		opts:     opts,
		v:        version,
		log:      opts.Logger.With("bot", opts.Username),
		uuid:     protocol.OfflineUUID(opts.Username),
		entities: entities.New(),
		world:    world.New(),
		inv:      inventory.New(),
		window:   inventory.NewContainer(),
	}, nil
}

// Username returns the bot's player name.
func (c *Client) Username() string { return c.opts.Username }

// Version returns the protocol version this client is speaking. It is nil
// until Connect has resolved it, which matters when auto-detecting.
func (c *Client) Version() *protocol.Version { return c.v }

// UUID returns the player's UUID: derived from the username for an
// offline-mode server, or whatever the server assigned if it disagreed.
//
// On an offline-mode server this is the player's identity — every statistic
// and advancement the server records is keyed by it — so anything checking up
// on the bot afterwards must use this value.
func (c *Client) UUID() protocol.UUID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.uuid
}

// background runs fn in a tracked goroutine, so Close can wait it out.
func (c *Client) background(fn func()) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		fn()
	}()
}

// Close tears down the connection and waits for the client's own goroutines to
// finish, so a caller that returns straight after does not leave an auto-fall
// writing into a closed socket.
func (c *Client) Close() error {
	var err error
	if c.conn != nil {
		err = c.conn.Close()
	}
	c.wg.Wait()
	return err
}

// Connect dials the server and drives handshake -> login -> configuration,
// returning once the client has entered the play state.
//
// It does not read play-state packets; call Run for that. Splitting the two
// lets a caller connect, assert on having joined, and only then start
// pumping — which keeps "did the bot get in?" separable from "what happened
// after it got in?".
//
// On failure the connection is closed before returning, so a caller that gives
// up after a failed Connect leaks nothing.
func (c *Client) Connect(ctx context.Context) (err error) {
	if c.v == nil {
		// Auto-detect. A server-list ping is a complete, separate connection
		// that needs no version agreement, so it is safe to make before we know
		// which protocol to speak.
		v, err := DetectVersion(c.opts.Addr, c.opts.Host, c.opts.Port, c.opts.DialTimeout)
		if err != nil {
			return err
		}
		c.v = v
		c.log.Info("auto-detected server version", "version", v.Name, "protocol", v.Protocol)
	}

	conn, err := protocol.Dial(c.opts.Addr, c.opts.DialTimeout)
	if err != nil {
		return err
	}
	c.conn = conn
	defer func() {
		if err != nil {
			_ = c.conn.Close()
		}
	}()

	c.setState(protocol.StateHandshaking)
	c.log.Info("connected", "addr", c.opts.Addr, "uuid", c.UUID().String())

	if err = c.handshake(); err != nil {
		return err
	}
	if err = c.login(ctx); err != nil {
		return err
	}
	return c.configure(ctx)
}

// Run pumps play-state packets until ctx is cancelled, the server disconnects,
// or a protocol error occurs. Exactly one goroutine may call it.
//
// Only the packets required to stay connected, to know where we are and to
// model the world get decoded; everything else is skipped by its length
// prefix. That is what keeps this client small — there are 141 clientbound
// play packets in 26.1 and this handles a couple of dozen.
func (c *Client) Run(ctx context.Context) error {
	if c.State() != protocol.StatePlay {
		return errors.New("understudy: Run called before entering play state")
	}
	for {
		p, err := c.readPacket(ctx)
		if err != nil {
			return err
		}
		// An unhandled packet is simply skipped: it was framed by its length
		// prefix, so ignoring its body costs nothing and keeps the stream in
		// step.
		if _, err := c.dispatch(ctx, p); err != nil {
			return err
		}
	}
}

// dispatch routes one play packet to the subsystem that owns it.
//
// The subsystem handlers come first and short-circuit: entity bookkeeping and
// chunk data are the high-volume traffic, and they are self-contained.
func (c *Client) dispatch(ctx context.Context, p protocol.Packet) (bool, error) {
	for _, handle := range []func(protocol.Packet) (bool, error){
		c.handleEntityPacket,
		c.handleWorldPacket,
		c.handleInventoryPacket,
		c.handleContainerPacket,
	} {
		if handled, err := handle(p); err != nil || handled {
			return handled, err
		}
	}
	return c.handleSessionPacket(ctx, p)
}

// handleSessionPacket decodes the packets that keep the session alive and
// track the player themselves: joining, being moved, health, death and kicks.
func (c *Client) handleSessionPacket(ctx context.Context, p protocol.Packet) (bool, error) {
	switch p.ID {
	case c.v.Packets.CBPlayLogin:
		r := p.Reader()
		entityID := r.I32()
		if err := r.Err(); err != nil {
			return true, err
		}
		c.mu.Lock()
		c.entityID = entityID
		c.joined = true
		c.mu.Unlock()
		c.log.Info("joined the game", "entity_id", entityID)
		return true, nil

	case c.v.Packets.CBPlayPosition:
		return true, c.handleTeleport(ctx, p)

	case c.v.Packets.CBPlayKeepAlive:
		r := p.Reader()
		id := r.I64()
		if err := r.Err(); err != nil {
			return true, err
		}
		return true, c.conn.WritePacket(
			protocol.NewWriter(c.v.Packets.SBPlayKeepAlive).I64(id).Bytes())

	case c.v.Packets.CBPlayUpdateHealth:
		r := p.Reader()
		health := r.F32()
		food := r.VarInt()
		if err := r.Err(); err != nil {
			return true, err
		}
		c.mu.Lock()
		c.health, c.food = health, food
		c.mu.Unlock()
		return true, nil

	case c.v.Packets.CBPlayDeathCombatEvent:
		return true, c.handleDeath()

	case c.v.Packets.CBPlayRespawn:
		// The payload is a SpawnInfo block that this client does not need; the
		// authoritative position arrives separately as a teleport.
		c.mu.Lock()
		c.dead = false
		c.mu.Unlock()
		// A respawn can cross dimensions, and entity IDs do not survive that.
		// Keeping stale entries would let the bot attack an ID that now means
		// something else, or nothing.
		c.entities.Reset()
		// Terrain is dimension-scoped too, and the same chunk coordinate means
		// different blocks on the other side of a portal.
		c.world.Reset()
		c.log.Info("respawned")
		return true, nil

	case c.v.Packets.CBPlayKickDisconnect:
		// The play-state kick reason is an NBT text component, not a
		// length-prefixed string. Reading it as one prints binary noise on the
		// single most important failure path, so pull the readable parts out
		// instead of pretending to decode NBT.
		return true, fmt.Errorf("understudy: kicked: %s", nbt.ReadableText(p.Data))
	}
	return false, nil
}

// handleTeleport answers a server-issued teleport.
//
// The acknowledgement is not optional bookkeeping. Until the server sees both
// the confirm and the position echo it holds the player in an "awaiting
// position from client" state, resends the teleport, and — the part that bites
// — silently drops use_item, use_item_on and player_action. See
// awaitTeleportSettle.
func (c *Client) handleTeleport(ctx context.Context, p protocol.Packet) error {
	r := p.Reader()
	teleportID := r.VarInt()
	x, y, z := r.F64(), r.F64(), r.F64()
	r.F64() // dx
	r.F64() // dy
	r.F64() // dz
	yaw, pitch := r.F32(), r.F32()
	if err := r.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	c.pos = Position{X: x, Y: y, Z: z, Yaw: yaw, Pitch: pitch}
	// Assume the server put us somewhere solid. Claiming to be airborne on
	// every update is what makes a server treat a bot as flying, so the default
	// has to be on-ground rather than zero.
	c.onGround = true
	// Count server-issued corrections. This is a ground-truth signal: the
	// server rejects a move into solid terrain and snaps the player back, so a
	// correction arriving mid-descent means "you have landed".
	c.corrections++
	c.mu.Unlock()

	if err := c.conn.WritePacket(
		protocol.NewWriter(c.v.Packets.SBPlayTeleportConfirm).VarInt(teleportID).Bytes()); err != nil {
		return err
	}
	// Echoing the position back is what the vanilla client does and what
	// settles the server's "has the player accepted?" check.
	ack := protocol.NewWriter(c.v.Packets.SBPlayPositionLook).
		F64(x).F64(y).F64(z).
		F32(yaw).F32(pitch).
		U8(protocol.MovementOnGround)
	if err := c.conn.WritePacket(ack.Bytes()); err != nil {
		return err
	}
	c.markTeleportEcho()
	c.log.Info("position synced", "x", x, "y", y, "z", z)

	c.announceLoaded()
	// Only start reporting idle position once we know where we are; before the
	// first teleport there is no position to report.
	c.startPositionLoop(ctx)
	// A teleport may well have put the bot in mid-air. Settle it onto ground
	// before vanilla's four-second float timer kicks it.
	c.autoFall(ctx)
	return nil
}

// handleDeath records a death and, unless disabled, asks to respawn.
func (c *Client) handleDeath() error {
	// The death message is a text component and is deliberately left unparsed —
	// decoding NBT text is a large dependency for a string this client never
	// asserts on.
	c.mu.Lock()
	c.dead = true
	c.deaths++
	deaths := c.deaths
	c.mu.Unlock()
	c.log.Info("died", "deaths", deaths)

	if c.opts.DisableAutoRespawn {
		c.log.Warn("auto-respawn disabled; bot will stay dead and ignore further actions")
		return nil
	}
	// Until this is sent the player stays on the death screen: the server keeps
	// the connection alive but refuses to position them, so everything
	// afterwards silently does nothing.
	respawn := protocol.NewWriter(c.v.Packets.SBPlayClientCommand).
		VarInt(protocol.ClientCommandPerformRespawn)
	if err := c.conn.WritePacket(respawn.Bytes()); err != nil {
		return err
	}
	c.log.Info("respawn requested")
	return nil
}

// deadlineMargin keeps the socket deadline just behind the context deadline so
// a clean shutdown never surfaces as an i/o timeout.
const deadlineMargin = 100 * time.Millisecond

// readPacket reads one packet, honouring ctx and the read timeout.
func (c *Client) readPacket(ctx context.Context) (protocol.Packet, error) {
	if err := ctx.Err(); err != nil {
		return protocol.Packet{}, err
	}
	deadline := time.Now().Add(c.opts.ReadTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		// Deliberately let the context deadline land first. Setting the socket
		// deadline to exactly the context deadline is a race: the i/o timeout
		// can surface before ctx.Err() is set, which reports a clean shutdown
		// as a read failure. The margin makes the context always win.
		deadline = d.Add(deadlineMargin)
	}
	if err := c.conn.SetReadDeadline(deadline); err != nil {
		return protocol.Packet{}, err
	}
	p, err := c.conn.ReadPacket()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return protocol.Packet{}, ctxErr
		}
		return protocol.Packet{}, err
	}
	if c.opts.OnPacket != nil {
		c.opts.OnPacket(c.State(), p)
	}
	return p, nil
}

// announceLoaded tells the server the world has finished loading, once.
//
// A vanilla client sends this after its level renderer is ready, and the
// server gates parts of a player's interaction on having received it. Never
// sending it leaves the player permanently half-joined: the symptom is that
// the *first* block action of a session is silently dropped — no rejection,
// the block simply does not break — and everything after it works, because by
// then the server has settled the player some other way.
//
// Sent after the first position sync rather than immediately on entering play,
// which is the point a real client has something loaded to report.
func (c *Client) announceLoaded() {
	if c.v.Packets.SBPlayPlayerLoaded == protocol.Absent {
		return
	}
	if !c.loadedSent.CompareAndSwap(false, true) {
		return
	}
	if err := c.conn.WritePacket(protocol.NewWriter(c.v.Packets.SBPlayPlayerLoaded).Bytes()); err != nil {
		c.log.Warn("could not announce player loaded", "err", err)
		return
	}
	c.log.Debug("announced player loaded")
}

// splitHostPort parses "host:port" into the parts the handshake advertises.
func splitHostPort(addr string) (string, uint16, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, fmt.Errorf("understudy: parse addr %q: %w", addr, err)
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return "", 0, fmt.Errorf("understudy: parse port in %q: %w", addr, err)
	}
	return host, uint16(port), nil
}
