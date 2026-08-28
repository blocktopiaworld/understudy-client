<!-- What changed, and why. The why is the part that survives. -->

## Checks

- [ ] `make check` passes (formatting, vet, lint, tests under -race)
- [ ] If this touches a decoder: captured bytes added to the wire tests, and the payload is consumed exactly
- [ ] If this touches protocol tables: says which server version it was verified against

<!--
If this was verified against a live server, say which one and what the
server's own view reported. An assertion on the client's own belief is not
evidence — see CONTRIBUTING.md.
-->
