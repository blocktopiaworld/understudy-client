---
title: Adding a version
nav_order: 3
---

Three things move between versions, and only the first is in minecraft-data.

**Packet ids, item and block tables.** Dense indices that shift whenever Mojang
inserts an entry, so they are generated:

```sh
npm pack minecraft-data && tar xf minecraft-data-*.tgz
node internal/gen/genversion.mjs package/minecraft-data/data \
     1.21.11 protocol/versions/version_1_21_11.go
```

If minecraft-data has not shipped the version yet — it had no 26.2 when 26.2
was added — build its input from the server's own reports first:

```sh
java -DbundlerMainClass=net.minecraft.data.Main -jar server.jar --reports
node internal/gen/reports-to-mcdata.mjs generated/reports 26.2 776 \
     package/minecraft-data/data 26.1
```

**Data component and slot display ids.** In no published dataset at all, so
they come from the server:

```sh
node internal/gen/gencomponents.mjs generated/reports/registries.json \
     26.2 protocol/versions/version_26_2_components.go
```

**Payload encodings.** These are measured against a running server, not
generated, and they are the part that cannot be skipped: knowing which id is
which does not tell you how the payload is laid out. 1.21.11 writes an item
nested in a component count-first where 26.1 writes it id-first. Fill in
`Components` only after checking, and never by copying another version's —
1.21.4 and 1.21.11 disagree with each other as much as either does with 26.1.

Leaving `Components` nil is a valid answer: components then refuse to decode on
that version and report why, which costs a partial inventory view. Guessing
costs a desynchronised one that reports nothing.

```sh
gofmt -w protocol/versions
go test ./protocol/... ./understudy/...
```

The tests check that each table registers itself, that every packet the client
needs is present rather than absent, that item and entity names resolve, and
that no version is left without component tables — a table that quietly lost an
id would otherwise fail silently at runtime.
