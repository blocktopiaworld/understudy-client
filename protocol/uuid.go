package protocol

import (
	"crypto/md5" //nolint:gosec // G501: RFC-4122 v3 mandates MD5; see OfflineUUID
	"encoding/hex"
	"strings"
)

// UUID is a raw 128-bit Minecraft UUID in wire order.
type UUID [16]byte

// String renders the canonical 8-4-4-4-12 hyphenated form.
func (u UUID) String() string {
	h := hex.EncodeToString(u[:])
	var b strings.Builder
	b.Grow(36)
	b.WriteString(h[0:8])
	b.WriteByte('-')
	b.WriteString(h[8:12])
	b.WriteByte('-')
	b.WriteString(h[12:16])
	b.WriteByte('-')
	b.WriteString(h[16:20])
	b.WriteByte('-')
	b.WriteString(h[20:32])
	return b.String()
}

// OfflineUUID derives the UUID an offline-mode server assigns to a username.
//
// Vanilla computes `UUID.nameUUIDFromBytes(("OfflinePlayer:"+name).getBytes(UTF_8))`,
// a plain RFC-4122 v3 (MD5) UUID. The bot has to derive the identical value,
// because on an offline-mode server the UUID *is* the player's identity: every
// statistic, advancement and permission the server records is keyed by it.
//
// Get this wrong and the bot plays perfectly while anything checking up on it
// looks at a player who does not exist. Any other tool that addresses the same
// player must derive the UUID the same way.
func OfflineUUID(name string) UUID {
	// MD5 is not a security choice here and cannot be substituted: an RFC-4122
	// v3 UUID *is* an MD5 digest, and the server derives the same value the
	// same way. Any stronger hash would produce a different UUID and address a
	// player who does not exist.
	sum := md5.Sum([]byte("OfflinePlayer:" + name)) //nolint:gosec // G401: see above
	sum[6] = (sum[6] & 0x0f) | 0x30                 // version 3
	sum[8] = (sum[8] & 0x3f) | 0x80                 // RFC-4122 variant
	return UUID(sum)
}
