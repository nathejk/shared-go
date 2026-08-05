// Package order is the read-model and command layer for orders.
//
// An Order belongs to an owner (a team or a personnel user) within a single
// Nathejk year. Lines on the order are either:
//
//   - "derived" — generated from the owner's current state by the team /
//     personnel handlers (e.g. one participation line per active member, one
//     t-shirt line per member with a chosen size). Replaced wholesale on
//     every SetDerivedLines call.
//   - "manual" — added explicitly by a user action (e.g. a shop "Buy" button).
//     Preserved across SetDerivedLines calls; removed only via RemoveLine.
//
// State machine:
//
//	open ──SetDerivedLines / AddManualLine / RemoveLine──> open
//	 │
//	 ├──Cancel──> cancelled  (terminal)
//	 └──Pay (saga, future)──> paid  (terminal, immutable)
//
// Multiple "open" orders per (year, owner) are structurally allowed; the
// EnsureOpenOrder helper enforces the "one open order at a time" UX rule by
// reusing an existing open order when present.
package order

import (
	"log"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/tables/product"
	"github.com/nathejk/shared-go/types"

	_ "embed"
)

// Status values written to the orders.status column.
type Status string

const (
	StatusOpen      Status = "open"
	StatusPaid      Status = "paid"
	StatusCancelled Status = "cancelled"
)

// MarshalJSON renders the order status as one of the two user-facing
// values "OPEN" or "PAID". Internally the order has three states (open,
// paid, cancelled) but the client-facing distinction is binary:
//
//   - "open"      → OPEN  (mutable, may still receive payment)
//   - "paid"      → PAID  (terminal, fully paid, immutable)
//   - "cancelled" → PAID  (terminal, no longer active; collapses into
//     PAID for display so the frontend only renders two states)
//
// The wire value is uppercase to make it visually distinct from the
// lowercase internal column value, so confusing "open" the column with
// "open" the API contract is unlikely.
func (s Status) MarshalJSON() ([]byte, error) {
	if s == StatusOpen {
		return []byte(`"OPEN"`), nil
	}
	return []byte(`"PAID"`), nil
}

// Order is the in-memory representation of an order with its lines, totals
// and (joined) paid amount. PaidAmount is computed at query time from the
// payment table — see querier.go.
type Order struct {
	OrderID      string         `json:"orderId"`
	Year         types.YearSlug `json:"year"`
	OwnerType    types.TeamType `json:"ownerType"`
	OwnerID      string         `json:"ownerId"`
	Status       Status         `json:"status"`
	Currency     string         `json:"currency"`
	TotalAmount  int            `json:"totalAmount"`
	PaidAmount   int            `json:"paidAmount"`
	DueAmount    int            `json:"dueAmount"`
	Lines        []Line         `json:"lines"`
	CancelReason string         `json:"cancelReason,omitempty"`
	CreatedAt    string         `json:"createdAt"`
	ChangedAt    string         `json:"changedAt"`
}

// Line is the in-memory representation of a row in order_line.
//
// MemberID identifies which member the line belongs to and is required on
// every line: the order command layer rejects lines without one, and the
// projector indexes order_line.memberId so the read model can answer
// "every line for this member" without a JSON scan.
type Line struct {
	LineID      string         `json:"lineId"`
	ProductSKU  string         `json:"productSku"`
	ProductName string         `json:"productName"`
	MemberID    string         `json:"memberId"`
	UnitPrice   int            `json:"unitPrice"`
	Quantity    int            `json:"quantity"`
	LineTotal   int            `json:"lineTotal"`
	Origin      string         `json:"origin"`
	Attributes  map[string]any `json:"attributes,omitempty"`
}

type table struct {
	commander
	consumer
	querier
}

// New wires the order package: creates the orders / order_line tables (idempotently),
// and constructs a value that exposes the command API (Commands), the read API
// (Queries), and the projector (cqrs.Consumer).
//
// year is the active Nathejk year that the commander stamps on newly-created
// orders. The Product dependency is used at command time to validate
// eligibility, snapshot product names/prices onto lines, and check stock.
//
// Self-healing schema: after creating the tables it ensures any columns
// or indexes added since the original schema landed are present.
func New(p cqrs.Publisher, w cqrs.Writer, r cqrs.Reader, year types.YearSlug, products product.Queries) *table {
	q := querier{db: r}
	c := commander{p: p, q: &q, products: products, year: year}
	t := &table{commander: c, consumer: consumer{w: w}, querier: q}
	if err := w.Consume(tableSchema); err != nil {
		log.Fatalf("Error creating order table %q", err)
	}
	if err := cqrs.EnsureColumn(r, w, "order_line", "memberId",
		"memberId VARCHAR(64) NOT NULL DEFAULT '' AFTER productName"); err != nil {
		log.Fatalf("Error migrating order_line.memberId %q", err)
	}
	if err := cqrs.EnsureIndex(r, w, "order_line", "idx_order_line_member",
		"ALTER TABLE order_line ADD INDEX idx_order_line_member (memberId)"); err != nil {
		log.Fatalf("Error migrating order_line.idx_order_line_member %q", err)
	}
	return t
}

//go:embed table.sql
var tableSchema string
