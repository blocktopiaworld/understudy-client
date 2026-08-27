# Security

## Reporting

Report vulnerabilities through GitHub's private advisory form on this
repository ("Security" → "Report a vulnerability"). Please do not open a public
issue for anything exploitable.

## Scope, and one thing to know up front

This is a headless Minecraft client for testing. Two properties are deliberate
and are not vulnerabilities:

**The control API has no authentication.** `-control` serves plain HTTP with no
tokens and no TLS, and anything that can reach the port can drive the bot. It
is meant to be bound to loopback for a test harness. Binding it to a public
interface exposes it, and that is the operator's decision rather than a defect.

**The client trusts the server it connects to.** It parses whatever that server
sends. Decoders are bounded — counts are capped, reads are length-checked, and
a malformed packet ends that packet rather than the process — but connecting to
a hostile server is outside the threat model.

Worth reporting: memory-unsafe behaviour, a panic reachable from ordinary server
traffic, an unbounded allocation driven by a packet field, or anything that
escapes the process.
