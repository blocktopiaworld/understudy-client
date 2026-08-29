// Package control exposes a bot's verbs over HTTP so it can be driven from a
// shell, a test runner, or another machine.
//
// It is internal to this module rather than part of the client package on
// purpose: the transport is a policy choice, and the client library should not
// decide that everyone wants an HTTP listener.
//
// # Security
//
// There is no authentication. Anything that can reach the listener can move
// the bot. Bind it to a loopback address unless the network it is on is one
// you already trust.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/blocktopiaworld/understudy-client/understudy"
)

// Timeouts for the control listener. The read-header timeout is what protects
// a bare listener from a stalled client; the others bound a handler that is
// waiting on the bot.
const (
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 120 * time.Second
	shutdownTimeout   = 2 * time.Second

	// defaultActionTimeout bounds a verb that does real work. Anything that
	// walks, falls or crafts takes wall-clock time, and an unreachable target
	// would otherwise hold the request open indefinitely.
	defaultActionTimeout = 60 * time.Second
	// settleDelay lets the server apply a consequence — fall damage, a food
	// bar — before the response reports it. Without it every fall looks
	// harmless because the numbers are read a tick too early.
	settleDelay = 300 * time.Millisecond
)

// Server serves the control API for one bot.
type Server struct {
	bot Bot
	log *slog.Logger
}

// New builds a control server over a bot.
func New(bot Bot, log *slog.Logger) *Server {
	return &Server{bot: bot, log: log}
}

// Handler returns the routed HTTP handler, which is also what the tests
// exercise — there is no separate code path for serving.
func (s *Server) Handler() http.Handler { return s.routes() }

// Serve runs the control API until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}
	// Shutdown runs on its own goroutine so ListenAndServe stays in charge of
	// the return value; a cancelled context surfaces as ErrServerClosed.
	done := make(chan struct{})
	// The shutdown context is deliberately not derived from ctx: this runs
	// *because* ctx was cancelled, so a child of it would already be done and
	// Shutdown would abandon in-flight requests instead of draining them.
	go func() { //nolint:gosec // G118: see above — a detached context is the point
		defer close(done)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	s.log.Info("control api listening", "addr", addr)
	err := srv.ListenAndServe()
	<-done
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("control api: %w", err)
	}
	return nil
}

// ParseAddr accepts either a bare port or a full host:port, so `--control 8080`
// and `--control 127.0.0.1:8080` both work.
func ParseAddr(v string) string {
	if _, err := strconv.Atoi(v); err == nil {
		return ":" + v
	}
	return v
}

// --- responses ---------------------------------------------------------------

// body is a JSON object being assembled for a response.
type body map[string]any

// writeJSON sends a JSON response.
//
// The encode error is logged rather than dropped: it means the response is
// already truncated on the wire, and silently discarding it turns a malformed
// reply into an unexplained client-side parse failure.
func (s *Server) writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.log.Warn("could not write control response", "err", err)
	}
}

// badRequest reports a malformed request: bad JSON, a missing field, an
// unparseable coordinate. The caller sent something wrong.
func (s *Server) badRequest(w http.ResponseWriter, err error) {
	s.log.Warn("control request rejected", "err", err)
	s.writeJSON(w, http.StatusBadRequest, body{"error": err.Error()})
}

// failed reports an action that could not be performed: the bot is dead, out
// of reach, not in play yet. The request was well-formed; the world said no.
//
// These are surfaced as structured errors rather than logged and swallowed,
// because every verb can fail for an *expected* reason and the caller needs to
// distinguish "you asked wrongly" from "it did not work".
//
// extra carries whatever the verb managed before failing.
func (s *Server) failed(w http.ResponseWriter, err error, extra body) {
	s.log.Warn("control action failed", "err", err)
	out := body{"error": err.Error()}
	// What kind of "no" this is, when the client knows. A caller waiting on an
	// action can stop the moment it hears a permanent one, instead of retrying
	// until its own timeout on something that was never going to become true.
	//
	// Absent when the client has not classified the refusal, which a caller
	// should read as "unknown" and handle the way it did before these existed.
	// Saying nothing is what makes the answer worth trusting where it is given.
	if refusal, ok := understudy.AsRefusal(err); ok {
		out["reason"] = string(refusal.Reason)
		out["retryable"] = refusal.Retryable
	}
	for k, v := range extra {
		out[k] = v
	}
	s.writeJSON(w, http.StatusConflict, out)
}

// position returns the fields every action response carries, so a caller can
// always see where the bot ended up.
func (s *Server) position() body {
	pos := s.bot.Position()
	return body{
		"x": pos.X, "y": pos.Y, "z": pos.Z,
		"yaw": pos.Yaw, "pitch": pos.Pitch,
	}
}

// okWith sends the standard success response plus extra fields.
func (s *Server) okWith(w http.ResponseWriter, extra body) {
	out := s.position()
	out["ok"] = true
	for k, v := range extra {
		out[k] = v
	}
	s.writeJSON(w, http.StatusOK, out)
}
