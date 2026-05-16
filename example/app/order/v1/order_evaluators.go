package orderv1

import (
	"errors"
	"fmt"

	"github.com/funinthecloud/protosource"
)

// This file demonstrates the CommandEvaluator extension point. Any command
// type can opt in by implementing:
//
//	Evaluate(aggregate protosource.Aggregate) error
//
// The pipeline calls Evaluate AFTER proto validation and state-guard checks
// but BEFORE event emission and persistence. The signature is intentionally
// narrow: Evaluate may return one of three things, and nothing else.
//
//   1. nil                     — proceed; the generator will emit events from
//                                the command's fields as usual.
//   2. protosource.ErrSkip     — silent no-op; no events persist, the caller
//                                receives the current version and a nil error.
//                                Use for idempotent commands that have already
//                                been satisfied (e.g. removing a missing item).
//   3. any other error         — abort; nothing is persisted and the error
//                                surfaces to the caller. Use for invariant
//                                violations that protovalidate cannot express.
//
// Evaluate may NOT mutate the command, substitute the event payload, or
// inject derived fields. Events are built mechanically from the command's
// proto fields after Evaluate returns. If you need to derive event data
// from the current aggregate state, do it before calling Apply (see the
// "Secrets and derived data" section of docs/consumer-guide.md).

// Evaluate on RemoveItem makes the command idempotent: removing an item that
// is not present is a silent no-op instead of an error. This is the textbook
// ErrSkip use case — the caller's intent ("ensure this item is gone") is
// already satisfied, so emitting another ItemRemoved event would just bloat
// the event log.
func (c *RemoveItem) Evaluate(aggregate protosource.Aggregate) error {
	order, ok := aggregate.(*Order)
	if !ok {
		return fmt.Errorf("RemoveItem.Evaluate: unexpected aggregate type %T", aggregate)
	}
	if _, present := order.GetItems()[c.GetItemId()]; !present {
		return protosource.ErrSkip
	}
	return nil
}

// ErrItemAlreadyPresent is returned by AddItem.Evaluate when the caller tries
// to add an item whose id is already in the order. Exported so callers can
// branch on it with errors.Is.
var ErrItemAlreadyPresent = errors.New("order: item already present")

// Evaluate on AddItem enforces an invariant that protovalidate cannot
// express: the item_id must not already exist in the order's items map.
// This demonstrates the "abort with error" branch. Note that the collection
// ADD semantics on ItemAdded would otherwise overwrite the existing entry
// silently — Evaluate is the right place to make that loud if your domain
// requires explicit Remove-then-Add for changes.
func (c *AddItem) Evaluate(aggregate protosource.Aggregate) error {
	order, ok := aggregate.(*Order)
	if !ok {
		return fmt.Errorf("AddItem.Evaluate: unexpected aggregate type %T", aggregate)
	}
	if _, present := order.GetItems()[c.GetItem().GetItemId()]; present {
		return fmt.Errorf("%w: item_id=%q", ErrItemAlreadyPresent, c.GetItem().GetItemId())
	}
	return nil
}
