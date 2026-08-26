package understudy

import (
	"fmt"
	"net"
	"strconv"
)

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
