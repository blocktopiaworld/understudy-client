package understudy

import "github.com/blocktopia/understudy-client/protocol"

// The player state the server reports, behind one lock.
//
// These are all trivial guarded reads. They are gathered here rather than
// scattered through the files that happen to write them so that the set of
// things protected by c.mu is visible in one place — a field that grows a
// second unsynchronised accessor somewhere else is exactly how a data race
// gets introduced.

// State returns the current protocol state.
func (c *Client) State() protocol.State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

func (c *Client) setState(s protocol.State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = s
}

// Position returns the last position the server told us about.
func (c *Client) Position() Position {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pos
}

// EntityID returns the player's entity ID, valid once joined.
func (c *Client) EntityID() int32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entityID
}

// Joined reports whether the play login packet has been seen.
func (c *Client) Joined() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.joined
}

// Health returns the last known health and food level.
func (c *Client) Health() (health float32, food int32) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.health, c.food
}

// Dead reports whether the bot is currently on the death screen. A dead bot
// silently ignores actions, so anything driving it should check this rather
// than trusting that a healthy connection means a usable player.
func (c *Client) Dead() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.dead
}

// Deaths returns how many times the bot has died this session.
func (c *Client) Deaths() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.deaths
}

// Corrections returns how many position corrections the server has issued.
// A rise in this value is how the client learns it has hit something.
func (c *Client) Corrections() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.corrections
}

// OnGround reports the bot's last known support state.
func (c *Client) OnGround() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.onGround
}

// HeldSlot returns the selected hotbar slot.
func (c *Client) HeldSlot() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.heldSlot
}

func (c *Client) setHeldSlotLocal(slot int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.heldSlot = slot
}

// Input returns the current movement-input bits.
func (c *Client) Input() uint8 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.input
}
