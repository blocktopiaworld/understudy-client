package understudy

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/blocktopiaworld/understudy-client/protocol"
)

// statusResponse is the part of a server-list ping this client cares about.
type statusResponse struct {
	Version struct {
		Name     string `json:"name"`
		Protocol int32  `json:"protocol"`
	} `json:"version"`
}

// ServerStatus is what a server-list ping reports.
type ServerStatus struct {
	// VersionName is the server's own label, e.g. "Fabric 26.1.2". It is free
	// text and servers rewrite it freely, so it is informational only.
	VersionName string
	// Protocol is the wire protocol number. This is the authoritative value —
	// it is what the handshake must match.
	Protocol int32
}

// PingServer performs a server-list ping and reports the version.
//
// The status handshake is deliberately version-agnostic: the protocol number
// sent in it is ignored by the server for a status request, so this works
// against any version without knowing it in advance. That is what makes
// auto-detection possible at all.
func PingServer(addr, host string, port uint16, timeout time.Duration) (ServerStatus, error) {
	conn, err := protocol.Dial(addr, timeout)
	if err != nil {
		return ServerStatus{}, err
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return ServerStatus{}, err
	}

	// Handshake with next state 1 (status) rather than 2 (login).
	handshake := protocol.NewWriter(0x00).
		VarInt(0). // protocol version: ignored for status requests
		String(host).
		I16(int16(port)).
		VarInt(1)
	if err := conn.WritePacket(handshake.Bytes()); err != nil {
		return ServerStatus{}, err
	}
	// Status request: empty body.
	if err := conn.WritePacket(protocol.NewWriter(0x00).Bytes()); err != nil {
		return ServerStatus{}, err
	}

	p, err := conn.ReadPacket()
	if err != nil {
		return ServerStatus{}, fmt.Errorf("understudy: ping read: %w", err)
	}
	r := p.Reader()
	payload := r.String()
	if err := r.Err(); err != nil {
		return ServerStatus{}, fmt.Errorf("understudy: ping decode: %w", err)
	}

	var status statusResponse
	if err := json.Unmarshal([]byte(payload), &status); err != nil {
		return ServerStatus{}, fmt.Errorf("understudy: ping json: %w", err)
	}
	return ServerStatus{
		VersionName: status.Version.Name,
		Protocol:    status.Version.Protocol,
	}, nil
}

// DetectVersion pings a server and resolves the matching protocol table.
//
// It matches on the protocol number, not the version name: names are free text
// that proxies and forks rewrite ("Paper 1.21", "Velocity"), whereas the
// protocol number is what actually has to agree on the wire.
func DetectVersion(addr, host string, port uint16, timeout time.Duration) (*protocol.Version, error) {
	status, err := PingServer(addr, host, port, timeout)
	if err != nil {
		return nil, fmt.Errorf("understudy: could not detect server version: %w", err)
	}
	v, err := protocol.ByProtocol(status.Protocol)
	if err != nil {
		return nil, fmt.Errorf("understudy: server reports %q (protocol %d): %w",
			status.VersionName, status.Protocol, err)
	}
	return v, nil
}
