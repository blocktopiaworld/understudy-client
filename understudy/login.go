package understudy

import (
	"context"
	"errors"
	"fmt"

	"github.com/block-topia/understudy-client/protocol"
)

// The pre-play states. Each is a small loop that answers whatever the server
// asks until it hands over to the next state.

// nextStateLogin is the handshake's "intent" field for logging in, as opposed
// to 1 for a server-list ping.
const nextStateLogin int32 = 2

// handshake announces the protocol version and the intent to log in.
func (c *Client) handshake() error {
	w := protocol.NewWriter(c.v.Packets.SBHandshake).
		VarInt(c.v.Protocol).
		String(c.opts.Host).
		// The handshake carries the port as an unsigned short, not a VarInt.
		I16(int16(c.opts.Port)).
		VarInt(nextStateLogin)
	if err := c.conn.WritePacket(w.Bytes()); err != nil {
		return err
	}
	c.setState(protocol.StateLogin)
	return nil
}

// login completes the login state.
//
// Only offline mode is supported: the server is expected never to send
// encryption_begin. If it does, that means online-mode is on and the bot
// cannot proceed — worth an explicit error, because the alternative symptom
// is an unexplained hang.
func (c *Client) login(ctx context.Context) error {
	w := protocol.NewWriter(c.v.Packets.SBLoginStart).
		String(c.opts.Username).
		UUID(c.UUID())
	if err := c.conn.WritePacket(w.Bytes()); err != nil {
		return err
	}

	for {
		p, err := c.readPacket(ctx)
		if err != nil {
			return err
		}
		switch p.ID {
		case c.v.Packets.CBLoginCompress:
			r := p.Reader()
			threshold := int(r.VarInt())
			if err := r.Err(); err != nil {
				return err
			}
			c.conn.SetCompressionThreshold(threshold)
			c.log.Debug("compression enabled", "threshold", threshold)

		case c.v.Packets.CBLoginSuccess:
			done, err := c.loginSuccess(p)
			if err != nil || done {
				return err
			}

		case c.v.Packets.CBLoginDisconnect:
			r := p.Reader()
			return fmt.Errorf("understudy: disconnected during login: %s", r.String())

		case c.v.Packets.CBLoginEncryptionBegin:
			return errors.New("understudy: server requested encryption (online-mode=true); " +
				"this client only supports offline mode")
		}
	}
}

// loginSuccess adopts the server's identity for this session and acknowledges,
// which moves both sides into configuration.
func (c *Client) loginSuccess(p protocol.Packet) (bool, error) {
	r := p.Reader()
	uuid := r.UUID()
	name := r.String()
	if err := r.Err(); err != nil {
		return false, err
	}
	c.log.Info("login success", "username", name, "uuid", uuid.String())
	if uuid != c.UUID() {
		// Not fatal, but it means assertions keyed on the derived UUID would
		// look at the wrong player, so it must be visible.
		c.log.Warn("server assigned a different uuid than derived",
			"derived", c.UUID().String(), "assigned", uuid.String())
		c.mu.Lock()
		c.uuid = uuid
		c.mu.Unlock()
	}
	if err := c.conn.WritePacket(
		protocol.NewWriter(c.v.Packets.SBLoginAcknowledged).Bytes()); err != nil {
		return false, err
	}
	c.setState(protocol.StateConfiguration)
	return true, nil
}

// Client information sent during configuration. The server waits for this
// before finishing configuration, and the trailing particleStatus field was
// added in 1.21.2 — omitting it desyncs the packet rather than erroring.
const (
	clientLocale       = "en_us"
	clientViewDistance = 10
	chatModeEnabled    = 0
	allSkinParts       = 0x7f
	mainHandRight      = 1
	particleStatusAll  = 0
)

// configure completes the configuration state.
//
// The non-obvious one is ping. Nothing in the vanilla flow requires it, but a
// Fabric API configuration task sends one and blocks until it is answered —
// so a client that ignores it sits in configuration forever with no error,
// having successfully logged in. Answering it is what actually gets a bot
// into the world on a Fabric server.
func (c *Client) configure(ctx context.Context) error {
	settings := protocol.NewWriter(c.v.Packets.SBConfigSettings).
		String(clientLocale).
		I8(clientViewDistance).
		VarInt(chatModeEnabled).
		Bool(true).            // chat colours
		U8(allSkinParts).      //
		VarInt(mainHandRight). //
		Bool(false).           // text filtering
		Bool(true).            // allow server listings
		VarInt(particleStatusAll)
	if err := c.conn.WritePacket(settings.Bytes()); err != nil {
		return err
	}

	for {
		p, err := c.readPacket(ctx)
		if err != nil {
			return err
		}
		done, err := c.handleConfigPacket(p)
		if err != nil || done {
			return err
		}
	}
}

// handleConfigPacket answers one configuration packet, reporting whether
// configuration is now complete.
func (c *Client) handleConfigPacket(p protocol.Packet) (done bool, err error) {
	switch p.ID {
	case c.v.Packets.CBConfigPing:
		r := p.Reader()
		id := r.I32()
		if err := r.Err(); err != nil {
			return false, err
		}
		return false, c.conn.WritePacket(
			protocol.NewWriter(c.v.Packets.SBConfigPong).I32(id).Bytes())

	case c.v.Packets.CBConfigKeepAlive:
		r := p.Reader()
		id := r.I64()
		if err := r.Err(); err != nil {
			return false, err
		}
		return false, c.conn.WritePacket(
			protocol.NewWriter(c.v.Packets.SBConfigKeepAlive).I64(id).Bytes())

	case c.v.Packets.CBConfigSelectKnownPacks:
		// Replying with an empty set tells the server we have no cached data
		// packs, so it should send registry data in full. A bot discards that
		// data anyway; claiming to know packs we do not would suppress
		// information we might later want.
		return false, c.conn.WritePacket(
			protocol.NewWriter(c.v.Packets.SBConfigSelectKnownPacks).VarInt(0).Bytes())

	case c.v.Packets.CBConfigCodeOfConduct:
		return false, c.conn.WritePacket(
			protocol.NewWriter(c.v.Packets.SBConfigAcceptCodeOfConduct).Bytes())

	case c.v.Packets.CBConfigFinishConfiguration:
		if err := c.conn.WritePacket(
			protocol.NewWriter(c.v.Packets.SBConfigFinishConfiguration).Bytes()); err != nil {
			return false, err
		}
		c.setState(protocol.StatePlay)
		c.log.Info("entered play state")
		return true, nil

	case c.v.Packets.CBConfigDisconnect:
		r := p.Reader()
		return false, fmt.Errorf("understudy: disconnected during configuration: %s", r.String())
	}
	return false, nil
}
