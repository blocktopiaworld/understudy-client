package understudy

import (
	"errors"
	"fmt"
	"testing"
)

// A refusal the client has classified says which kind of "no" it is, and one it
// has not says nothing rather than guessing. Guessing is the whole thing this
// exists to replace: a caller matching on prose swallows a real failure for
// thirty seconds and then reports it as something else.
func TestARefusalCarriesWhatTheClientKnows(t *testing.T) {
	t.Run("classified", func(t *testing.T) {
		err := refuse(ReasonOutOfReach, false, errors.New("too far"))
		r, ok := AsRefusal(err)
		if !ok {
			t.Fatal("AsRefusal did not recognise a refusal")
		}
		if r.Reason != ReasonOutOfReach || r.Retryable {
			t.Errorf("got %s retryable=%v, want out_of_reach retryable=false", r.Reason, r.Retryable)
		}
		if err.Error() != "too far" {
			t.Errorf("Error() = %q, want the wrapped message unchanged", err)
		}
	})

	t.Run("unclassified says nothing", func(t *testing.T) {
		if _, ok := AsRefusal(errors.New("something else")); ok {
			t.Error("AsRefusal claimed to know about an ordinary error")
		}
	})

	t.Run("survives wrapping", func(t *testing.T) {
		wrapped := fmt.Errorf("loading fuel: %w", refuse(ReasonNoSuchItem, false, errors.New("no coal")))
		r, ok := AsRefusal(wrapped)
		if !ok {
			t.Fatal("AsRefusal lost the refusal through a wrap")
		}
		if r.Reason != ReasonNoSuchItem {
			t.Errorf("Reason = %s, want no_such_item", r.Reason)
		}
	})

	t.Run("nil stays nil", func(t *testing.T) {
		if refuse(ReasonDead, true, nil) != nil {
			t.Error("refuse turned a nil error into a refusal")
		}
	})
}

// The sentinels callers compare against must keep working through the wrapper.
func TestTheSentinelsStillCompare(t *testing.T) {
	if !errors.Is(fmt.Errorf("opening: %w", ErrNoContainer), ErrNoContainer) {
		t.Error("ErrNoContainer no longer matches through a wrap")
	}
	r, ok := AsRefusal(ErrNoContainer)
	if !ok || r.Reason != ReasonNoWindow {
		t.Error("ErrNoContainer lost its reason")
	}
}
