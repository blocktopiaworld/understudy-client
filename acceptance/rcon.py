#!/usr/bin/env python3
"""Minimal RCON client. Replaces the compiled one the scratchpad lost."""
import socket, struct, sys

def rcon(host, port, password, *commands):
    s = socket.create_connection((host, port), timeout=10)
    rid = 0
    def send(kind, body):
        nonlocal rid
        rid += 1
        p = struct.pack("<ii", rid, kind) + body.encode() + b"\x00\x00"
        s.sendall(struct.pack("<i", len(p)) + p)
        return rid
    def recv():
        n = struct.unpack("<i", s.recv(4))[0]
        data = b""
        while len(data) < n:
            data += s.recv(n - len(data))
        return struct.unpack("<ii", data[:8])[1], data[8:-2].decode("utf-8", "replace")
    send(3, password)
    if recv()[0] == -1:
        raise SystemExit("rcon: auth failed")
    out = []
    for c in commands:
        send(2, c)
        out.append(recv()[1])
    s.close()
    return out

if __name__ == "__main__":
    host, port = (sys.argv[1].split(":") + ["25575"])[:2]
    for line in rcon(host, int(port), sys.argv[2], *sys.argv[3:]):
        print(line)
