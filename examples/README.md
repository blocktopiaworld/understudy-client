# Examples

Runnable scripts, in the order you would meet them.

Each shell example expects a bot already running with its control API up:

```sh
understudy-client -addr localhost:25565 -username Probe -hold 0 -control 8181
```

and takes the control address as `$BOT`, defaulting to `localhost:8181`.

| | |
| --- | --- |
| [01-look-around.sh](01-look-around.sh) | connect, read the world, understand what the bot can see |
| [02-mine-and-place.sh](02-mine-and-place.sh) | hold a tool, break blocks, put one back |
| [03-craft-at-a-table.sh](03-craft-at-a-table.sh) | open a crafting table, lay out a recipe, take the result |
| [04-trade-with-a-villager.sh](04-trade-with-a-villager.sh) | read a merchant's offers and complete a trade |
| [05-assert-a-statistic.sh](05-assert-a-statistic.sh) | the point of all this — make the server's own numbers move |
| [go/main.go](go/main.go) | the same thing as a Go library, without the HTTP hop |

`jq` pretty-prints the responses when it is installed. The scripts fall back to
raw JSON when it is not, and every one of them has been run both ways.

Example 5 needs an RCON client to ask the server what it thinks happened. It
assumes `mcrcon`; set `RCON_CMD` to use your own.
