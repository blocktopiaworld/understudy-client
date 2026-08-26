// Package protocol implements the Minecraft wire format: framing, the
// primitive encodings, chunk decoding, and the per-version tables that say
// which packet ID means what.
//
// It knows nothing about playing the game. Everything here is about getting
// bytes on and off a socket correctly, which is why it is separable from the
// package that decides what to do with them.
//
// # Files
//
//	conn.go       framing, compression, the read/write halves of a connection
//	varint.go     VarInt and VarLong, the two encodings everything else rests on
//	reader.go     bounded field decoding; accumulates the first error
//	writer.go     field encoding
//	chunk.go      paletted chunk containers and block-state lookup
//	version.go    Version, VersionSpec, and the per-version lookups
//	registry.go   the name and protocol-number registries
//	constants.go  wire constants: packet states, dig statuses, slot indices
//	names.go      namespacing helpers for item and entity names
//	uuid.go       offline-mode UUID derivation
//
// The generated tables are not here. They are ~9,700 lines across three files,
// which in one directory buries the ten hand-written ones above; they live in
// protocol/versions instead.
//
// # Versions
//
// Almost everything that changes between Minecraft releases is a number in a
// table rather than a branch in code. Packet IDs are dense indices that shift
// whenever Mojang inserts a packet; entity and item names are indexed by wire
// ID; block-state classification is a set of ranges. Those tables are
// generated from minecraft-data by internal/gen/genversion.mjs into
// protocol/versions, where they register themselves from init.
//
// So this package defines the machinery and protocol/versions supplies the
// data. Importing protocol alone gives you an empty registry — something must
// import protocol/versions for ByName and ByProtocol to resolve anything. The
// understudy package does that for you; a program using protocol directly
// needs the blank import itself.
//
// Keeping the data out means a test, or a tool that speaks one version, can
// build exactly the Version it needs through NewVersion without linking in
// three full tables.
//
// The handful of genuine format differences that cannot be expressed as a
// table live in ChunkFormat, and each is documented with the symptom it causes
// when it is wrong. They share a shape: nothing errors at the mistake, and a
// short read surfaces several sections downstream.
//
// # Decoding
//
// Reader accumulates the first error and keeps going, so a packet decoder
// reads every field and checks once at the end rather than after each. Every
// length that arrives from the wire is bounded before it is used to size an
// allocation or as a divisor — see MaxStringLen, MaxSections and
// MaxBitsPerEntry. That is not defensive programming for its own sake: a
// desynced stream puts arbitrary bytes where a length prefix should be, and an
// unbounded one turns a single corrupt packet into an out-of-memory kill or a
// division by zero.
//
// # Concurrency
//
// A Conn may be written from any goroutine; reads are single-threaded by
// contract, since there is one read loop. A Version is immutable once
// registered and safe to share. Reader and Writer are not safe for concurrent
// use, and neither are ChunkColumn and ChunkSection — the caller holding those
// holds the lock.
package protocol
