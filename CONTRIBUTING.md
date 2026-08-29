# Contributing

## Before you push

```sh
make check
```

That is formatting, `go vet`, `golangci-lint` and the tests under the race
detector — the same four things CI runs, in the same order. If CI disagrees
with `make check`, the Makefile is the bug.

The module has no dependencies and is meant to keep it that way. A change that
adds one needs to say in the pull request why the standard library will not do.

## What this project is strict about

Most of the protocol here is undocumented. Nothing describes Minecraft's data
components, so every encoding in `understudy/components.go` was read off
captured packets rather than looked up. That history is why a few habits are
enforced harder than usual:

**Do not confuse "I do not know" with "there is nothing there."** An unloaded
chunk reads as air everywhere. A component the decoder has no shape for is not
an empty component. A recipe that failed to decode is not a recipe that does
not exist. Each of those has cost real debugging time here, and each is now a
distinct value rather than a shared falsy one — `Support.Known` beside
`Support.Found`, `MissingRecipes` beside `KnownRecipes`. Keep that split when
you add to it.

**Two samples, not one.** For anything with a length or a count in it, one
sample proves almost nothing: it walks one path and leaves the other untried.
A potion read from a single capture looked six bytes wide and was five. A
`blocks_attacks` with no bypass tag hid a field being read as the wrong type.
Where widths can vary, give the test two samples that differ.

**The server is the authority.** A test that asserts the client's own belief
asserts nothing. Check through RCON, or through what the server sends back.

## Tests

Unit tests cover the pure parts: parsing, geometry, inventories, block tables.
The verbs that need a live server — workstations, trades, storage — are covered
by runs against real Paper, Fabric and vanilla servers instead, so their unit
coverage is low by design rather than by neglect.

If you add a payload encoding, add its captured bytes to the wire tests. The
assertion that matters is that the payload is consumed exactly: with no length
prefix, landing on the final byte is the only evidence a reading is right.

## Style

`docs/go-style.md` is the long version. The short version: comments say why,
not what, and an error message should tell the reader what to do next.

## The documentation site

`docs/` is a Jekyll site that GitHub Pages builds. There is no local toolchain
and nothing to install: the theme is fetched by `remote_theme`, and the plugins
in `docs/_config.yml` are ones Pages already runs.

Adding a page means adding a markdown file with front matter — `title`, and
`parent` plus `nav_order` if it belongs under an existing section. Cross-links
stay as ordinary relative links to the `.md` file: `jekyll-relative-links`
rewrites them to the built URL, so the same link works on the site and when
reading the file on GitHub. Writing site URLs by hand breaks the second one.

**Turning it on is a one-time setting**, and it needs the repository to be
public — GitHub Pages is not available for private repositories on the free
plan. Settings → Pages → Source: *Deploy from a branch*, branch `main`, folder
`/docs`. No workflow, no CI minutes.
