// Command understudy-client connects a single bot to a Minecraft server and
// holds the connection open. It is the smoke test for the connection layer: if
// this prints "joined the game" and the server log agrees, the handshake,
// login, configuration and play plumbing all work.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/block-topia/understudy-client/internal/control"
	"github.com/block-topia/understudy-client/protocol"
	"github.com/block-topia/understudy-client/understudy"
)

// buildVersion is stamped at release time with -ldflags "-X main.buildVersion=...".
//
// Named for the build rather than for Minecraft, because -version already means
// "the Minecraft version to speak" and one of those two had to give way. A
// binary built any other way says "dev", which is the honest answer.
var buildVersion = "dev"

func main() {
	// All the work is in run so that every defer — the signal handler, the
	// connection close, the context cancels — actually runs. os.Exit does not
	// unwind the stack, so calling it from the middle of main skipped the
	// close and left the server holding a half-open player slot.
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "understudy-client:", err)
		os.Exit(1)
	}
}

// config is the parsed command line.
type config struct {
	addr         string
	username     string
	hold         time.Duration
	version      string
	listVersions bool
	control      string
	noRespawn    bool
	noIdlePos    bool
	trace        bool
	debug        bool
}

// parseFlags parses argv into a config. Split from run so the flag set is not
// package-global state and a test can parse its own arguments.
func parseFlags(args []string, out *os.File) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("understudy-client", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&cfg.addr, "addr", "127.0.0.1:25565", "server host:port")
	fs.StringVar(&cfg.username, "username", "Understudy", "offline-mode player name")
	fs.DurationVar(&cfg.hold, "hold", 15*time.Second,
		"how long to stay connected after joining (0 = until interrupted)")
	fs.StringVar(&cfg.version, "version", "",
		"Minecraft version to speak (default: auto-detect by pinging the server)")
	fs.BoolVar(&cfg.listVersions, "versions", false, "list supported versions and exit")
	fs.StringVar(&cfg.control, "control", "",
		"serve the remote-control HTTP API on this port or host:port (e.g. 8080). "+
			"There is no authentication; bind to loopback unless the network is trusted")
	fs.BoolVar(&cfg.noRespawn, "no-respawn", false,
		"stay dead instead of respawning (for observing death handling)")
	fs.BoolVar(&cfg.noIdlePos, "no-idle-position", false,
		"stop reporting position while standing still (a real client sends ~20/s)")
	fs.BoolVar(&cfg.trace, "trace", false, "log every clientbound packet id")
	fs.BoolVar(&cfg.debug, "debug", false, "enable debug logging")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func run(args []string, stderr *os.File) error {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		return err
	}

	if cfg.listVersions {
		_, err := fmt.Fprintf(stderr, "understudy-client %s\nspeaks: %s\n",
			buildVersion, strings.Join(protocol.Names(), ", "))
		return err
	}

	level := slog.LevelInfo
	if cfg.debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))

	opts := understudy.Options{
		Addr:                cfg.addr,
		Version:             cfg.version,
		Username:            cfg.username,
		DisableAutoRespawn:  cfg.noRespawn,
		DisableIdlePosition: cfg.noIdlePos,
		Logger:              log,
	}
	if cfg.trace {
		opts.OnPacket = func(state protocol.State, p protocol.Packet) {
			log.Debug("recv", "state", state.String(),
				"id", fmt.Sprintf("0x%02x", p.ID), "bytes", len(p.Data))
		}
	}

	bot, err := understudy.New(opts)
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	// Ctrl-C and SIGTERM should disconnect cleanly rather than leave the
	// server holding a half-open player slot.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	started := time.Now()
	if err := bot.Connect(ctx); err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}
	defer func() {
		if err := bot.Close(); err != nil {
			log.Debug("close", "err", err)
		}
	}()

	runCtx := ctx
	if cfg.hold > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, cfg.hold)
		defer cancel()
	}

	if cfg.control != "" {
		// The control API writes packets while Run reads them. That is safe:
		// the connection serialises writes internally.
		go func() {
			if err := control.New(bot, log).Serve(runCtx, control.ParseAddr(cfg.control)); err != nil {
				log.Error("control api stopped", "err", err)
			}
		}()
	}

	// Run only ever returns because something ended the session, so its error
	// is always non-nil — the question is whether that something was us.
	runErr := bot.Run(runCtx)

	// A cancelled context is the success path when --hold expires: the bot
	// stayed connected for the whole window without being kicked.
	if ctx.Err() == nil && runCtx.Err() == nil {
		return fmt.Errorf("disconnected: %w", runErr)
	}
	// Expected on a clean shutdown, but the reason still has to be visible:
	// "stopped because the hold expired" and "stopped because the server hung
	// up a moment earlier" look identical without it.
	log.Info("run ended", "err", runErr, "elapsed", time.Since(started).Round(time.Millisecond))

	if !bot.Joined() {
		return errors.New("never received the play login packet")
	}
	pos := bot.Position()
	health, food := bot.Health()
	log.Info("done",
		"uuid", bot.UUID().String(),
		"entity_id", bot.EntityID(),
		"x", pos.X, "y", pos.Y, "z", pos.Z,
		"health", health, "food", food,
		"deaths", bot.Deaths(), "dead", bot.Dead())
	return nil
}
