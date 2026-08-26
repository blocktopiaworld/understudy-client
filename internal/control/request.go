package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Request plumbing.
//
// Every handler used to repeat the same six lines: decode, check, act, check,
// respond. That is twenty-odd copies of an error path, and they had already
// drifted — a malformed JSON body and an out-of-reach block both answered 409,
// so a caller could not tell a bug in its own request from a refusal by the
// world.

// decode reads a JSON body, tolerating an empty one so verbs with no
// parameters can be called with a bare POST.
func decode[T any](r *http.Request) (T, error) {
	var v T
	if r.ContentLength == 0 {
		return v, nil
	}
	dec := json.NewDecoder(r.Body)
	// Reject unknown fields: a typo'd key would otherwise be silently ignored
	// and the verb would run with defaults, which reads as "the bot ignored me".
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, fmt.Errorf("malformed JSON body: %w", err)
	}
	return v, nil
}

// handle wires up the common shape of a control verb: decode the body, run the
// action, and turn its error into the right status.
//
// act returns the extra response fields to merge into the standard payload, so
// a verb that just does something can return nil.
func handle[T any](s *Server, act func(ctx context.Context, in T) (body, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		in, err := decode[T](r)
		if err != nil {
			s.badRequest(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), defaultActionTimeout)
		defer cancel()

		extra, err := act(ctx, in)
		if err != nil {
			var invalid *invalidRequestError
			if errors.As(err, &invalid) {
				s.badRequest(w, err)
				return
			}
			// The extra fields survive the error on purpose. A verb that got
			// part-way — "dug 6 of 9" — tells the caller far more than the
			// error alone, and discarding the count would make a partial
			// success indistinguishable from having done nothing.
			s.failed(w, err, extra)
			return
		}
		s.okWith(w, extra)
	}
}

// invalidRequestError marks an error as the caller's fault rather than the
// world's, so handle can pick the status code without every verb having to
// write the response itself.
type invalidRequestError struct{ err error }

func (e *invalidRequestError) Error() string { return e.err.Error() }
func (e *invalidRequestError) Unwrap() error { return e.err }

// invalidf builds a 400-worthy error.
func invalidf(format string, args ...any) error {
	return &invalidRequestError{err: fmt.Errorf(format, args...)}
}

// --- query parameters --------------------------------------------------------

// blockCoords reads the x/y/z query parameters shared by the read-only
// endpoints.
func blockCoords(r *http.Request) (x, y, z int32, err error) {
	q := r.URL.Query()
	var out [3]int32
	for i, name := range []string{"x", "y", "z"} {
		raw := q.Get(name)
		v, convErr := strconv.ParseInt(raw, 10, 32)
		if convErr != nil {
			return 0, 0, 0, invalidf("bad %s coordinate %q", name, raw)
		}
		out[i] = int32(v)
	}
	return out[0], out[1], out[2], nil
}

// --- optional fields ---------------------------------------------------------

// orDefault returns v when it is positive, else fallback. Used for the
// "0 means unset" numeric fields the JSON API exposes.
func orDefault[T int | int32 | int64](v, fallback T) T {
	if v > 0 {
		return v
	}
	return fallback
}

// millis converts an optional millisecond field to a duration, falling back
// when it is unset.
func millis(ms int, fallback time.Duration) time.Duration {
	if ms <= 0 {
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}

// deref returns the value behind p, or fallback when p is nil.
func deref[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}
	return *p
}
