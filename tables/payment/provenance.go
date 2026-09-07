package payment

import (
	"context"
	"database/sql"
	"log"

	"github.com/doug-martin/goqu/v9"
	"github.com/nathejk/shared-go/types"
)

// SourceResolver answers "where did this payment's money originally come from?".
//
// A separate, narrow interface rather than another method on Queries: adding to
// Queries would break every fake that implements it in another repo, and the
// callers that need this — whoever creates an internal transfer — need nothing
// else from the read side.
type SourceResolver interface {
	// SourceOf resolves the provenance a transfer should carry when it moves the
	// money of the payment named by reference.
	SourceOf(ctx context.Context, reference string) *types.PaymentSource

	// SourceOfOrder is SourceOf for the payment that covered an order, which is
	// the form a caller moving money between orders actually has to hand.
	SourceOfOrder(ctx context.Context, orderID string) *types.PaymentSource
}

// SourceOf resolves the provenance a transfer should carry when it moves the
// money of the payment named by reference.
//
// It never fails and never returns nil. Identifying the source will often be
// impossible — payment.orderForeignKey is polymorphic, most historical payments
// are not reachable from an order at all, and the root may be from a season whose
// events are long since projected elsewhere — and that is the normal case rather
// than an edge case. Refusing to record a transfer because a lookup came back
// empty would be the wrong trade every time, so an unresolvable source is
// types.UnknownPaymentSource(): explicitly unknown, which is information, and
// distinguishable from the nil that means "not a transfer".
//
// Root-preserving. If the payment being moved from is itself a transfer, its
// recorded root is inherited and the payment we moved from becomes Via, so
// provenance survives A → B → C without degrading into a chain somebody has to
// walk. Exactly one extra hop is read, because every transfer stores the root
// rather than its predecessor — which also means inconsistent data cannot send
// this into an unbounded walk.
//
// Call it once, when the transfer is created, and put the result on the event.
// Read models here are rebuilt from JetStream on every start, so provenance
// computed by a projector from whatever the read model happens to say at replay
// time would not be the provenance that was true when the money moved.
func (q *querier) SourceOf(ctx context.Context, reference string) *types.PaymentSource {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	return resolveSource(reference, func(ref string) (sourceRow, bool) {
		return q.sourceRow(ctx, ref)
	})
}

// resolveSource is SourceOf's decision-making, separated from its reads.
//
// The interesting part of provenance is which row ends up describing the money —
// the payment itself, its root one hop back, or nothing at all — and that is worth
// testing without a database. lookup returns a payment row and whether it was
// found; everything else is here.
func resolveSource(reference string, lookup func(string) (sourceRow, bool)) *types.PaymentSource {
	if reference == "" {
		return types.UnknownPaymentSource()
	}
	row, ok := lookup(reference)
	if !ok {
		return types.UnknownPaymentSource()
	}

	switch root := row.SourceReference; {
	case root == "":
		// A provider payment: it is its own root, which is the common case.
		return row.provenance()

	case root == types.PaymentSourceUnknown:
		// Moving money whose own origin was already unknown. It stays unknown —
		// inventing a root here would be worse than admitting there isn't one —
		// but the hop we do know is worth keeping.
		return &types.PaymentSource{Reference: types.PaymentSourceUnknown, Via: row.Reference}

	default:
		rootRow, ok := lookup(root)
		if !ok {
			// The root is named but not projected here (a previous season, most
			// likely). Its reference is still the answer; the snapshot is not
			// available, and a partial truth beats discarding the reference.
			return &types.PaymentSource{Reference: root, Via: row.Reference}
		}
		s := rootRow.provenance()
		s.Via = row.Reference
		return s
	}
}

// sourceRow is one payment as provenance needs it, plus its order's owner.
type sourceRow struct {
	Reference       string              `db:"reference"`
	Method          types.PaymentMethod `db:"method"`
	CreatedAt       string              `db:"createdAt"`
	SourceReference string              `db:"sourceReference"`
	OrderForeignKey string              `db:"orderForeignKey"`
	OrderType       string              `db:"orderType"`

	// Nullable: the LEFT JOIN misses for every legacy payment, which points at a
	// team rather than an order.
	OrderOwnerType sql.NullString `db:"orderOwnerType"`
	OrderOwnerID   sql.NullString `db:"orderOwnerId"`
}

// provenance describes this payment as the source of somebody else's money.
//
// The owner is recovered from whichever linkage the row actually uses: through the
// order for current payments, and straight off the polymorphic pair for the legacy
// ones, where orderForeignKey *is* the team id and orderType names its kind. That
// second branch is not a nicety — in the 2026 data only 44 of 189 paid orders are
// linked by order id, so for most historical payments the owner is the only thing
// knowable at all.
func (r sourceRow) provenance() *types.PaymentSource {
	s := &types.PaymentSource{
		Reference: r.Reference,
		Method:    r.Method,
		PaidAt:    r.CreatedAt,
	}
	switch {
	case r.OrderType == OrderTypeOrder:
		s.OwnerType = types.TeamType(r.OrderOwnerType.String)
		s.OwnerID = r.OrderOwnerID.String
	case r.OrderType != "":
		s.OwnerType = types.TeamType(r.OrderType)
		s.OwnerID = r.OrderForeignKey
	}
	return s
}

// sourceRow reads one payment. A missing row and a failed read are the same
// outcome here — no provenance — because the caller is about to move money either
// way; the difference is only whether it is worth logging.
func (q *querier) sourceRow(ctx context.Context, reference string) (sourceRow, bool) {
	var row sourceRow
	found, err := q.sourceRowDataset(reference).ScanStructContext(ctx, &row)
	if err != nil {
		log.Printf("payment: resolving provenance of %q: %v", reference, err)
		return sourceRow{}, false
	}
	return row, found
}

func (q *querier) sourceRowDataset(reference string) *goqu.SelectDataset {
	return q.db.
		From(goqu.T("payment").As("p")).
		LeftJoin(goqu.T("orders").As("o"), goqu.On(goqu.I("o.orderId").Eq(goqu.I("p.orderForeignKey")))).
		Select(
			goqu.I("p.reference"), goqu.I("p.method"), goqu.I("p.createdAt"),
			goqu.I("p.sourceReference"), goqu.I("p.orderForeignKey"), goqu.I("p.orderType"),
			goqu.I("o.ownerType").As("orderOwnerType"), goqu.I("o.ownerId").As("orderOwnerId"),
		).
		Prepared(true).
		Where(goqu.I("p.reference").Eq(reference))
}

var _ SourceResolver = (*querier)(nil)

// SourceOfOrder — see SourceResolver.SourceOfOrder.
//
// Resolves through the payment that actually secured the order: reserved or
// received, since those are the only states every paid-amount computation counts,
// and the earliest of them when an order was paid in several parts. The earliest
// rather than the largest because it is the one that made the order real, and
// because "earliest" is stable — a later capture must not change the answer for a
// transfer that already happened.
//
// An order with no such payment yields types.UnknownPaymentSource(), which is the
// honest answer for a free order, a settled one, or an order whose payment is not
// projected here.
func (q *querier) SourceOfOrder(ctx context.Context, orderID string) *types.PaymentSource {
	if orderID == "" {
		return types.UnknownPaymentSource()
	}
	ref, ok := q.coveringReference(ctx, orderID)
	if !ok {
		return types.UnknownPaymentSource()
	}
	return q.SourceOf(ctx, ref)
}

// coveringReference returns the earliest secured payment on an order.
func (q *querier) coveringReference(ctx context.Context, orderID string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var ref string
	found, err := q.coveringReferenceDataset(orderID).ScanValContext(ctx, &ref)
	if err != nil {
		log.Printf("payment: finding the payment covering order %q: %v", orderID, err)
		return "", false
	}
	return ref, found && ref != ""
}

func (q *querier) coveringReferenceDataset(orderID string) *goqu.SelectDataset {
	return q.db.
		From(goqu.T("payment").As("p")).
		Select(goqu.I("p.reference")).
		Prepared(true).
		Where(
			goqu.I("p.orderForeignKey").Eq(orderID),
			goqu.I("p.orderType").Eq(OrderTypeOrder),
			goqu.I("p.status").In(string(types.PaymentStatusReserved), string(types.PaymentStatusReceived)),
		).
		Order(goqu.I("p.createdAt").Asc(), goqu.I("p.reference").Asc()).
		Limit(1)
}
