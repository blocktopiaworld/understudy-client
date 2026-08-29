package understudy

import "errors"

// A Refusal is an action the world would not accept, carrying whether waiting
// could change that.
//
// Every refusal already said what went wrong in a sentence. What a caller could
// not tell was which kind of "no" it had: a swing sent before the spawn packet
// arrived clears on its own, and an item the player does not hold never will,
// and both arrived as the same shape with a different sentence. So a caller
// either retried everything until its timeout, or matched on prose — and
// matching on prose is how a real failure gets swallowed for thirty seconds and
// then reported as something else.
//
// The client knows which it has at the point it refuses. This is that knowledge,
// written down.
type Refusal struct {
	// Reason is a short stable code. Stable is the point: it is the part a
	// caller may switch on, so it changes only when the meaning does.
	Reason Reason
	// Retryable is whether the same call, unchanged, could succeed later.
	Retryable bool

	err error
}

// Reason names a kind of refusal.
type Reason string

const (
	// Transient: the world may still become what the caller asked for.

	// ReasonNotTargetable is a block the client has not been told about. It may
	// be a block that does not exist, or one whose chunk has not arrived — and
	// from here those are the same silence.
	ReasonNotTargetable Reason = "not_targetable"
	// ReasonNotTracked is an entity the client has not seen yet.
	ReasonNotTracked Reason = "not_tracked"
	// ReasonLockedOut is a trade whose uses are spent. A villager restocks; a
	// wandering trader does not, so this is retryable only in principle.
	ReasonLockedOut Reason = "locked_out"
	// ReasonTooHungry is an action vanilla gates on food.
	ReasonTooHungry Reason = "too_hungry"
	// ReasonDead is a bot awaiting respawn.
	ReasonDead Reason = "dead"

	// Permanent: the caller has to change something.

	// ReasonNoSuchItem is an item the player is not holding anywhere.
	ReasonNoSuchItem Reason = "no_such_item"
	// ReasonOutOfReach is a target too far away to act on from here. The bot
	// does not move on its own, so this does not clear by waiting.
	ReasonOutOfReach Reason = "out_of_reach"
	// ReasonOccluded is a target with something solid in front of it.
	ReasonOccluded Reason = "occluded"
	// ReasonNoWindow is a container verb with no window open.
	ReasonNoWindow Reason = "no_window"
	// ReasonWrongWindow is a verb used against a window of another kind.
	ReasonWrongWindow Reason = "wrong_window"
	// ReasonNoOffers is a merchant with nothing to sell. A villager given a
	// profession but no offers never generates any.
	ReasonNoOffers Reason = "no_offers"
	// ReasonNoSuchTrade is an index the merchant does not offer.
	ReasonNoSuchTrade Reason = "no_such_trade"
	// ReasonNoUI is a target that opened no window within the wait.
	ReasonNoUI Reason = "no_ui"
	// ReasonBlocked is a walk that stopped making progress.
	ReasonBlocked Reason = "blocked"
	// ReasonUnchanged is an action the server accepted and did not act on: a
	// block that would not break, a placement that never appeared.
	ReasonUnchanged Reason = "unchanged"
	// ReasonUnsupported is something this version's protocol has no packet for.
	ReasonUnsupported Reason = "unsupported"
)

func (r *Refusal) Error() string { return r.err.Error() }

func (r *Refusal) Unwrap() error { return r.err }

// refuse wraps an error with what the client knows about it.
func refuse(reason Reason, retryable bool, err error) error {
	if err == nil {
		return nil
	}
	return &Refusal{Reason: reason, Retryable: retryable, err: err}
}

// AsRefusal reports what the client knows about an error, if anything.
//
// A refusal it has not classified comes back false rather than guessing, and a
// caller should treat that the way it treated every refusal before this existed.
// Saying "I do not know" is the whole reason this is worth trusting where it
// does answer.
func AsRefusal(err error) (*Refusal, bool) {
	var r *Refusal
	if errors.As(err, &r) {
		return r, true
	}
	return nil, false
}
