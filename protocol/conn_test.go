package protocol

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
)

// pipePair returns two Conns talking to each other over an in-memory pipe, so
// the framing layer can be exercised without a socket.
func pipePair(t *testing.T) (client, server *Conn) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() {
		_ = a.Close()
		_ = b.Close()
	})
	return NewConn(a), NewConn(b)
}

// send writes a payload from one side and reads the packet from the other,
// without deadlocking on the unbuffered pipe.
func send(t *testing.T, from, to *Conn, payload []byte) Packet {
	t.Helper()
	errc := make(chan error, 1)
	go func() { errc <- from.WritePacket(payload) }()
	p, err := to.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	return p
}

func TestFramingUncompressed(t *testing.T) {
	c, s := pipePair(t)
	p := send(t, c, s, NewWriter(0x2a).String("hello").I32(7).Bytes())

	if p.ID != 0x2a {
		t.Errorf("packet id = %#x, want 0x2a", p.ID)
	}
	r := p.Reader()
	if got := r.String(); got != "hello" {
		t.Errorf("body string = %q, want %q", got, "hello")
	}
	if got := r.I32(); got != 7 {
		t.Errorf("body int = %d, want 7", got)
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err() = %v", err)
	}
}

// Once compression is on, a dataLength of 0 means the body travels raw and any
// other value is the *decompressed* size. Treating the 0 marker as a zlib
// stream is the classic bug in this framing, so both sides of the threshold
// are exercised.
func TestFramingBothSidesOfTheThreshold(t *testing.T) {
	c, s := pipePair(t)
	const threshold = 64
	c.SetCompressionThreshold(threshold)
	s.SetCompressionThreshold(threshold)

	for _, tc := range []struct {
		name string
		size int
	}{
		{"under the threshold, sent raw", 8},
		{"one byte under", threshold - 2},
		{"exactly at the threshold, compressed", threshold},
		{"well over, compressed", threshold * 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := bytes.Repeat([]byte{'z'}, tc.size)
			p := send(t, c, s, append(NewWriter(5).Bytes(), body...))
			if p.ID != 5 {
				t.Errorf("packet id = %d, want 5", p.ID)
			}
			if !bytes.Equal(p.Data, body) {
				t.Errorf("payload did not survive: got %d bytes, want %d", len(p.Data), len(body))
			}
		})
	}
}

// Compressed data that zlib expands rather than shrinks still has to survive
// byte for byte.
func TestCompressedRoundTripOfIncompressiblePayload(t *testing.T) {
	c, s := pipePair(t)
	c.SetCompressionThreshold(16)
	s.SetCompressionThreshold(16)

	// Deterministic pseudo-random: a linear congruential sequence, which zlib
	// cannot usefully compress.
	body := make([]byte, 4096)
	state := uint32(12345)
	for i := range body {
		state = state*1664525 + 1013904223
		body[i] = byte(state >> 24)
	}

	p := send(t, c, s, append(NewWriter(7).Bytes(), body...))
	if p.ID != 7 {
		t.Errorf("packet id = %d, want 7", p.ID)
	}
	if !bytes.Equal(p.Data, body) {
		t.Errorf("payload did not survive (%d bytes back, want %d)", len(p.Data), len(body))
	}
}

func TestFramingBackToBackPackets(t *testing.T) {
	c, s := pipePair(t)
	go func() {
		for i := range 5 {
			_ = c.WritePacket(NewWriter(int32(i)).I32(int32(i * 100)).Bytes())
		}
	}()
	for i := range 5 {
		p, err := s.ReadPacket()
		if err != nil {
			t.Fatalf("ReadPacket %d: %v", i, err)
		}
		if p.ID != int32(i) {
			t.Errorf("packet %d has id %d", i, p.ID)
		}
		if got := p.Reader().I32(); got != int32(i*100) {
			t.Errorf("packet %d body = %d, want %d", i, got, i*100)
		}
	}
}

// A desynced length prefix must fail fast rather than make the client allocate
// gigabytes.
func TestReadPacketRejectsOversizedFrame(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() {
		_, _ = a.Write(AppendVarInt(nil, MaxPacketSize+1))
	}()
	if _, err := NewConn(b).ReadPacket(); err == nil {
		t.Error("ReadPacket of an oversized frame = nil error, want an error")
	}
}

func TestCompressionThresholdDefaultsDisabled(t *testing.T) {
	c, _ := pipePair(t)
	if got := c.CompressionThreshold(); got != CompressionDisabled {
		t.Errorf("CompressionThreshold() = %d on a fresh connection, want %d",
			got, CompressionDisabled)
	}
	c.SetCompressionThreshold(256)
	if got := c.CompressionThreshold(); got != 256 {
		t.Errorf("CompressionThreshold() = %d after setting 256", got)
	}
}

// The read loop and the control API write from different goroutines. Framing
// must stay intact: an interleaved write would splice one frame into another
// and desync the stream permanently.
func TestConcurrentWritesDoNotInterleave(t *testing.T) {
	c, s := pipePair(t)

	const writers, each = 8, 25
	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each writer sends a distinctly sized body, so a spliced frame
			// shows up as a length no writer would have produced.
			payload := NewWriter(int32(w)).
				String(strings.Repeat(string(rune('a'+w)), w+1)).Bytes()
			for range each {
				if err := c.WritePacket(payload); err != nil {
					t.Errorf("WritePacket: %v", err)
					return
				}
			}
		}()
	}

	seen := make(map[int32]int)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range writers * each {
			p, err := s.ReadPacket()
			if err != nil {
				t.Errorf("ReadPacket: %v", err)
				return
			}
			r := p.Reader()
			body := r.String()
			if err := r.Err(); err != nil {
				t.Errorf("packet %d body: %v", p.ID, err)
				return
			}
			if len(body) != int(p.ID)+1 {
				t.Errorf("packet %d carried a %d-byte body, want %d — frames interleaved",
					p.ID, len(body), p.ID+1)
				return
			}
			seen[p.ID]++
		}
	}()

	wg.Wait()
	<-done
	for w := range writers {
		if seen[int32(w)] != each {
			t.Errorf("writer %d: read %d packets, want %d", w, seen[int32(w)], each)
		}
	}
}

func TestDecompressFrameRawMarker(t *testing.T) {
	payload := []byte{0x2a, 0x01, 0x02}
	frame := append(AppendVarInt(nil, 0), payload...)
	got, err := decompressFrame(frame)
	if err != nil {
		t.Fatalf("decompressFrame: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("decompressFrame = %v, want the raw payload %v", got, payload)
	}
}

func TestDecompressFrameRejectsBadInput(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame []byte
	}{
		{"empty", nil},
		{"length beyond the cap", AppendVarInt(nil, MaxPacketSize+1)},
		{"negative length", AppendVarInt(nil, -1)},
		{"not a zlib stream", append(AppendVarInt(nil, 8), 'n', 'o', 'p', 'e')},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decompressFrame(tc.frame); err == nil {
				t.Errorf("decompressFrame(%v) = nil error, want an error", tc.frame)
			}
		})
	}
}

func TestReadPacketOnClosedConnection(t *testing.T) {
	a, b := net.Pipe()
	c := NewConn(a)
	_ = b.Close()
	_, err := c.ReadPacket()
	if err == nil {
		t.Error("ReadPacket on a closed peer = nil error, want an error")
	} else if !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "closed") {
		t.Logf("ReadPacket returned %v (acceptable)", err)
	}
	_ = a.Close()
}
