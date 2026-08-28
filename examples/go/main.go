// Command example connects a bot, mines a block and reports what the server
// made of it — the same thing the shell examples do, without the HTTP hop.
//
//	go run ./examples/go -addr localhost:25565 -x 10 -y 64 -z 10
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/blocktopiaworld/understudy-client/understudy"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:25565", "server host:port")
	name := flag.String("username", "Probe", "offline-mode player name")
	x := flag.Int("x", 0, "block x")
	y := flag.Int("y", 64, "block y")
	z := flag.Int("z", 0, "block z")
	flag.Parse()

	if err := run(*addr, *name, int32(*x), int32(*y), int32(*z)); err != nil {
		log.Fatal(err)
	}
}

func run(addr, name string, x, y, z int32) error {
	// Ctrl-C leaves the server cleanly rather than dropping the connection.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c, err := understudy.New(understudy.Options{Addr: addr, Username: name})
	if err != nil {
		return err
	}
	defer func() {
		if err := c.Close(); err != nil {
			log.Println("close:", err)
		}
	}()

	if err := c.Connect(ctx); err != nil {
		return err
	}
	fmt.Printf("joined as %s, speaking %s\n", c.Username(), c.Version().Name)

	// Terrain trails the join. Wait for the column below to arrive rather than
	// for the position, because an unsent chunk reads as air and "no data" is
	// not "no ground".
	deadline := time.Now().Add(15 * time.Second)
	for {
		if s := c.GroundBelow(); s.Known {
			fmt.Printf("standing on y=%.0f\n", s.GroundY)
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("terrain never arrived")
		}
		time.Sleep(100 * time.Millisecond)
	}

	if _, err := c.HoldItem("minecraft:netherite_pickaxe"); err != nil {
		// Not fatal: it just means this will be slow.
		fmt.Println("no pickaxe:", err)
	}

	// The client refuses what a real client could not do, rather than sending a
	// packet the server will ignore. An out-of-reach block is an error naming
	// the distance, not a silent no-op.
	if err := c.DigBlock(ctx, x, y, z, 1, 1500*time.Millisecond); err != nil {
		return fmt.Errorf("dig: %w", err)
	}
	fmt.Printf("broke the block at %d,%d,%d\n", x, y, z)
	return nil
}
