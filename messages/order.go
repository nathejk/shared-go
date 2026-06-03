package messages

import (
	"time"

	"github.com/nathejk/shared-go/types"
)

// LineOrigin classifies how an order line came to exist on the order.
//
//   - LineOriginDerived: produced automatically from team / personnel form
//     state (e.g. one participation line per active member, one t-shirt line
//     per member with a chosen size). The order projector replaces lines with
//     this origin on every SetLines event.
//   - LineOriginManual: added explicitly by a user (e.g. picked up from a
//     shop UI). Manual lines are preserved across SetLines events that only
//     touch derived lines.
type LineOrigin string

const (
	LineOriginDerived LineOrigin = "derived"
	LineOriginManual  LineOrigin = "manual"
)

// NathejkOrderCreated is published when a new order is opened for an owner
// (team or personnel user) within a given year. The order starts in status
// "open" and remains mutable until either NathejkOrderPaid or
// NathejkOrderCancelled is published for the same OrderID.
//
// nathejk:order.created
type NathejkOrderCreated struct {
	OrderID   string         `json:"orderId"`
	Year      types.YearSlug `json:"year"`
	OwnerType types.TeamType `json:"ownerType"`
	OwnerID   string         `json:"ownerId"`
	Currency  types.Currency `json:"currency"`
	Timestamp time.Time      `json:"timestamp"`
}

// NathejkOrderLine is a line-item snapshot embedded in
// NathejkOrderLinesChanged events. Monetary values are in the minor unit
// of the order's currency (øre for DKK).
//
// Every line carries a non-empty MemberID identifying which member the
// line belongs to — the participant for participation lines, the
// recipient for t-shirts and other merchandise. The order command layer
// rejects lines without a MemberID so we can always answer "who ordered
// what" from the order_line projection alone, without joining through
// attributes.
//
// LineID must be stable across successive NathejkOrderLinesChanged events
// for a given line, so that projectors can upsert by (OrderID, LineID).
// Suggested conventions:
//
//   - Derived lines: deterministic, e.g. "derived:participation:{memberId}"
//     or "derived:tshirt:{memberId}". The projector then naturally replaces
//     stale ones when a member is removed.
//   - Manual lines: a fresh UUID per line.
type NathejkOrder_Line struct {
	LineID      string         `json:"lineId"`
	ProductSKU  string         `json:"productSku"`
	ProductName string         `json:"productName"`
	MemberID    string         `json:"memberId"`
	UnitPrice   int            `json:"unitPrice"`
	Quantity    int            `json:"quantity"`
	LineTotal   int            `json:"lineTotal"`
	Origin      LineOrigin     `json:"origin"`
	Attributes  map[string]any `json:"attributes,omitempty"`
}

// NathejkOrderLinesChanged carries the full, post-mutation set of lines for
// an order. Consumers must treat this as the new truth and replace any
// previously known lines for the same OrderID — the event is a snapshot,
// not a diff.
//
// Publishing a snapshot (rather than per-line add/remove deltas) keeps the
// projector trivially idempotent and matches the ergonomics of the
// SetLines command, which itself accepts the desired final state.
//
// TotalAmount is the sum of the embedded LineTotal values and is included
// for convenience so downstream consumers don't have to recompute it.
//
// nathejk:order.lines.changed
type NathejkOrderLinesChanged struct {
	OrderID     string              `json:"orderId"`
	Lines       []NathejkOrder_Line `json:"lines"`
	TotalAmount int                 `json:"totalAmount"`
	Timestamp   time.Time           `json:"timestamp"`
}

// NathejkOrderCancelled marks an open order as cancelled. A paid order can
// never be cancelled; the order command layer is responsible for rejecting
// such transitions before publishing this event.
//
// nathejk:order.cancelled
type NathejkOrderCancelled struct {
	OrderID   string    `json:"orderId"`
	Reason    string    `json:"reason,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// NathejkOrderPaid is published by the order projector when received
// payments referencing the order cover its total. After this event the
// order is immutable: any subsequent NathejkOrderLinesChanged or
// NathejkOrderCancelled event for the same OrderID is a programmer error
// and should be rejected by the command layer before it is published.
//
// PaidAmount is the cumulative amount in minor units that has been
// reserved/received against the order at the moment of transition.
//
// nathejk:order.paid
type NathejkOrderPaid struct {
	OrderID    string    `json:"orderId"`
	PaidAmount int       `json:"paidAmount"`
	Timestamp  time.Time `json:"timestamp"`
}
