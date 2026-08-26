package protocol

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// MaxPacketSize bounds a single frame. The server should never send anything
// close to this; the cap exists so a desynced length prefix fails fast instead
// of making the client allocate gigabytes.
const MaxPacketSize = 1 << 23 // 8 MiB

// CompressionDisabled is the threshold value meaning "no compression yet".
// The server switches compression on mid-login by sending set_compression, so
// this is the starting state of every connection.
const CompressionDisabled = -1

// readBufferSize is the buffered-reader size. Comfortably larger than a chunk
// packet, so a full column is usually one syscall.
const readBufferSize = 1 << 16

// Packet is one decoded frame: its ID and its payload with the ID stripped.
type Packet struct {
	ID   int32
	Data []byte
}

// Reader returns a Reader positioned at the first field of the payload.
func (p Packet) Reader() *Reader { return NewReader(p.Data) }

// Conn is a framed Minecraft connection.
//
// It deliberately knows nothing about packet semantics — only how to get
// bytes on and off the wire. Reads are single-threaded by contract (one read
// loop), but writes are mutex-guarded because keep-alive and command traffic
// are generated from different goroutines.
type Conn struct {
	conn net.Conn
	br   *bufio.Reader

	// threshold is the compression threshold in bytes, or CompressionDisabled.
	// Payloads at or above it are zlib-compressed; smaller ones travel raw
	// with a 0 data-length marker.
	//
	// Atomic rather than guarded by writeMu: the read loop needs it for every
	// inbound frame, and taking the write lock to read one integer parks the
	// reader behind whatever write is in flight.
	threshold atomic.Int64

	writeMu sync.Mutex
}

// Dial opens a TCP connection to a Minecraft server.
func Dial(addr string, timeout time.Duration) (*Conn, error) {
	c, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("protocol: dial %s: %w", addr, err)
	}
	// Latency matters more than throughput here: a dig sequence is a handful
	// of tiny packets whose spacing the server measures, and Nagle would
	// coalesce them into the wrong timing.
	if tcp, ok := c.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
	}
	return NewConn(c), nil
}

// NewConn wraps an already-established connection.
//
// Exported so the framing layer can be driven over an in-memory pipe — a fake
// server in a test, or a proxy — without going through Dial and a real socket.
func NewConn(c net.Conn) *Conn {
	conn := &Conn{conn: c, br: bufio.NewReaderSize(c, readBufferSize)}
	conn.threshold.Store(CompressionDisabled)
	return conn
}

// SetCompressionThreshold enables (>= 0) or disables (< 0) compression. It is
// safe to call while reads and writes are in flight.
func (c *Conn) SetCompressionThreshold(t int) { c.threshold.Store(int64(t)) }

// CompressionThreshold returns the current threshold, or CompressionDisabled.
func (c *Conn) CompressionThreshold() int { return int(c.threshold.Load()) }

// SetReadDeadline bounds how long the next read may block.
func (c *Conn) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }

// Close closes the underlying socket.
func (c *Conn) Close() error { return c.conn.Close() }

// RemoteAddr returns the server address.
func (c *Conn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

// ReadPacket reads one frame.
//
// Uncompressed framing is [VarInt length][VarInt id][payload]. Once
// compression is on it becomes [VarInt packetLength][VarInt dataLength][body],
// where dataLength == 0 means the body is stored raw (it was under the
// threshold) and any other value is the *decompressed* size of a zlib body.
// Treating a 0 marker as a zlib stream is the classic bug here, so the two
// cases are handled explicitly.
func (c *Conn) ReadPacket() (Packet, error) {
	length, err := ReadVarInt(c.br)
	if err != nil {
		return Packet{}, err
	}
	if length < 0 || int(length) > MaxPacketSize {
		return Packet{}, fmt.Errorf("protocol: frame length %d out of range", length)
	}

	frame := make([]byte, length)
	if _, err := io.ReadFull(c.br, frame); err != nil {
		return Packet{}, fmt.Errorf("protocol: read frame: %w", err)
	}

	if c.CompressionThreshold() >= 0 {
		if frame, err = decompressFrame(frame); err != nil {
			return Packet{}, err
		}
	}

	pr := NewReader(frame)
	id := pr.VarInt()
	if err := pr.Err(); err != nil {
		return Packet{}, err
	}
	return Packet{ID: id, Data: pr.Remaining()}, nil
}

// decompressFrame unwraps the compressed framing: [VarInt dataLength][body].
//
// A dataLength of 0 means the body was under the threshold and travels raw.
// Treating that marker as a zlib stream is the classic bug here, so the two
// cases are separated explicitly rather than by probing the body.
func decompressFrame(frame []byte) ([]byte, error) {
	fr := NewReader(frame)
	dataLength := fr.VarInt()
	if err := fr.Err(); err != nil {
		return nil, err
	}
	body := fr.Remaining()
	if dataLength == 0 {
		return body, nil
	}
	if dataLength < 0 || int(dataLength) > MaxPacketSize {
		return nil, fmt.Errorf("protocol: decompressed length %d out of range", dataLength)
	}
	zr, err := zlib.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("protocol: zlib open: %w", err)
	}
	defer func() { _ = zr.Close() }()
	// Sized exactly from dataLength, which is bounded above: ReadFull then
	// fails on a stream that claims more than it carries, rather than growing.
	out := make([]byte, dataLength)
	if _, err := io.ReadFull(zr, out); err != nil {
		return nil, fmt.Errorf("protocol: zlib read: %w", err)
	}
	return out, nil
}

// WritePacket writes an encoded payload (which already begins with the packet
// ID VarInt) as one frame. It is safe to call from multiple goroutines: the
// control API writes while the read loop runs.
func (c *Conn) WritePacket(payload []byte) error {
	frame, err := c.encodeFrame(payload)
	if err != nil {
		return err
	}
	out := AppendVarInt(make([]byte, 0, len(frame)+maxVarIntBytes), int32(len(frame)))
	out = append(out, frame...)

	// The socket write is what needs serialising — two interleaved writes
	// would splice one frame into the middle of another.
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.conn.Write(out); err != nil {
		return fmt.Errorf("protocol: write: %w", err)
	}
	return nil
}

// encodeFrame applies the compression framing appropriate to the current
// threshold, returning the frame body without its outer length prefix.
func (c *Conn) encodeFrame(payload []byte) ([]byte, error) {
	threshold := c.CompressionThreshold()
	switch {
	case threshold < 0:
		return payload, nil
	case len(payload) < threshold:
		// Under the threshold: a 0 data-length marker, then the raw payload.
		return append(AppendVarInt(make([]byte, 0, len(payload)+1), 0), payload...), nil
	}
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	if _, err := zw.Write(payload); err != nil {
		return nil, fmt.Errorf("protocol: zlib write: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("protocol: zlib close: %w", err)
	}
	return append(AppendVarInt(nil, int32(len(payload))), zbuf.Bytes()...), nil
}
