// Package transfer moves a member, and the money paid for them, from one team to
// another before the race.
//
// # Why one command
//
// Two things have to happen, and they are only correct together. The member's team
// changes on the roster, and the seat somebody already paid for — plus whatever
// merchandise was bought for them — moves with them. Reassigning without moving
// the money leaves one team paid up for a member who left and the other short for
// a member who arrived; moving the money without reassigning leaves a paid seat
// nobody occupies.
//
// There is no transaction across JetStream, so the ordering is chosen rather than
// hoped for: **money first, reassignment last**. Both partial failures are bad,
// but they are not equally bad. Money moved and member not is invisible — funds
// have shifted for somebody who is still on the old roster, and nothing looks
// wrong. Member moved and money not is a member on a team whose seat was never
// paid for, which shows up in the numbers the moment anyone looks. Between an
// invisible inconsistency and a visible one, take the visible one.
//
// Everything is validated before anything is published, following the same shape
// as the member-status MoveMembers command: read, decide, then publish.
//
// # What this command refuses
//
// Only what it alone can know: a member who does not exist, and a move to the team
// the member is already on. Preconditions like "the destination has accepted", "it
// has not started" or "it has room" belong to the caller, which owns the team read
// models and the operator-facing messages.
//
// In particular there is **no lower bound on the origin's member count**. Emptying
// a team out one member at a time is a required capability: a team below three may
// not start, and its remaining members have to be transferable to a team that has
// not started yet.
package transfer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/tables/order"
	"github.com/nathejk/shared-go/tables/payment"
	"github.com/nathejk/shared-go/tables/spejder"
	"github.com/nathejk/shared-go/types"
)

// Errors are distinguishable on purpose: a caller maps each to its own message for
// an operator, so "already on that team" must never arrive as the same value as
// "there was nothing to transfer" — which is not an error at all.
var (
	// ErrMemberNotFound is returned when the roster has no such member in this
	// season. Wraps tables.ErrRecordNotFound so a caller that maps that to 404
	// keeps working.
	ErrMemberNotFound = fmt.Errorf("member is not on the roster: %w", tables.ErrRecordNotFound)

	// ErrSameTeam is returned when the member is already on the destination team.
	// Distinct from "nothing to transfer": the move itself is meaningless, rather
	// than the money side being empty.
	ErrSameTeam = errors.New("member is already on that team")

	// ErrIncompleteTransfer is returned when the request is missing a member, a
	// destination or a team type. A programming error at the call site rather than
	// something an operator did.
	ErrIncompleteTransfer = errors.New("transfer is missing a member, a destination team or a team type")

	// ErrNotBalanced is returned if the credit and charge halves would not net to
	// zero. It should be unreachable — the two sets are the same lines with
	// opposite signs — and exists so that a future edit which breaks the invariant
	// fails loudly instead of moving money that does not add up.
	ErrNotBalanced = errors.New("transfer does not net to zero")
)

// Transfer is a request to move one member to another team.
//
// FromTeamID is deliberately absent. The origin is read from the roster, which is
// the only thing that actually knows it, so a stale form cannot ask for a move out
// of a team the member has already left — and there is no mismatch error for a
// caller to handle.
type Transfer struct {
	MemberID types.MemberID
	ToTeamID types.TeamID

	// TeamType is the kind of team on both sides (a member moves between teams of
	// the same kind). It is what makes the origin's orders findable, since orders
	// are keyed by (year, ownerType, ownerId).
	TeamType types.TeamType

	// Actor is who performed the move, recorded on the event.
	Actor messages.NathejkMemberActor
}

// Result describes what a transfer did, so a caller can report it without reading
// back an eventually-consistent projection.
type Result struct {
	Identity

	// FromTeamID is the team the roster had the member on.
	FromTeamID types.TeamID

	// Amount is the value moved, in the currency's minor unit, always >= 0. The
	// credit order totals -Amount and the charge order +Amount.
	Amount int

	// LineCount is how many lines moved. Zero means the member had no paid seat:
	// the reassignment still happened and no orders were created, which is a
	// perfectly ordinary outcome for a member who was added but not yet paid for.
	LineCount int
}

// Commands is the write API of this package.
type Commands interface {
	// TransferMember moves the member to another team, moving their paid seat and
	// merchandise with them.
	//
	// Publishes, in this order: the credit order and its internal-transfer
	// payment, the charge order and its payment, and finally the reassignment.
	// Safe to call again with the same arguments — every id is derived from the
	// transfer (see Identity), so a repeat re-states the same transfer instead of
	// making a second one.
	TransferMember(ctx context.Context, t Transfer) (*Result, error)
}

type commander struct {
	p       cqrs.Publisher
	roster  spejder.RosterReader
	lines   order.MemberLineReader
	sources payment.SourceResolver
	year    types.YearSlug
}

// New wires the transfer command.
//
// The dependencies are the three narrow read interfaces this operation needs, each
// declared by the package that owns the data: the roster (which team is the member
// on), the order lines (what has been paid for them), and payment provenance
// (where that money originally came from). All three are satisfied by the entities
// returned by spejder.New, order.New and payment.New.
func New(p cqrs.Publisher, roster spejder.RosterReader, lines order.MemberLineReader, sources payment.SourceResolver, year types.YearSlug) Commands {
	return &commander{p: p, roster: roster, lines: lines, sources: sources, year: year}
}

// TransferMember — see Commands.TransferMember.
func (c *commander) TransferMember(ctx context.Context, t Transfer) (*Result, error) {
	if t.MemberID == "" || t.ToTeamID == "" || t.TeamType == "" {
		return nil, ErrIncompleteTransfer
	}

	from, err := c.roster.TeamIDOf(ctx, c.year, t.MemberID)
	switch {
	case errors.Is(err, tables.ErrRecordNotFound):
		return nil, ErrMemberNotFound
	case err != nil:
		return nil, err
	}
	if from == t.ToTeamID {
		return nil, ErrSameTeam
	}

	paid, err := c.lines.PaidLinesByMember(ctx, c.year, t.TeamType, string(from), string(t.MemberID))
	if err != nil {
		return nil, err
	}

	id := IdentityOf(c.year, t.MemberID, from, t.ToTeamID)
	res := &Result{Identity: id, FromTeamID: from, LineCount: len(paid)}

	// Nothing has been published yet, and nothing will be until every decision is
	// made — including the provenance lookup, which reads the payment side.
	credit, charge, total := split(id, paid)
	if err := balanced(credit, charge); err != nil {
		return nil, err
	}
	res.Amount = total

	var source *types.PaymentSource
	if len(paid) > 0 {
		source = c.sources.SourceOfOrder(ctx, fundingOrder(paid))
	}

	// Money first. See the package doc: of the two partial failures, the one that
	// leaves a member on a team whose seat is unpaid is the one somebody notices.
	if len(paid) > 0 {
		if err := c.publishHalf(id.CreditOrderID, t.TeamType, from, credit, -total, id.CreditReference, source); err != nil {
			return nil, err
		}
		if err := c.publishHalf(id.ChargeOrderID, t.TeamType, t.ToTeamID, charge, total, id.ChargeReference, source); err != nil {
			return nil, err
		}
	}

	// Reassignment last.
	if err := c.publishReassignment(t, from); err != nil {
		return nil, err
	}
	return res, nil
}

// split turns the member's paid lines into the two halves of the transfer and
// returns the value moved.
//
// Both halves carry the same products, the same snapshotted unitPrice, the same
// memberId and the same attributes — the t-shirt size lives in attributes and the
// order is authoritative for shipping, so losing it misdirects a physical object.
// Only the sign differs.
//
// Origin is manual, not derived: derived lines are replaced wholesale by
// SetDerivedLines, and a transfer line recomputed from roster state would vanish
// the next time the team's order was saved.
func split(id Identity, paid []order.MemberLine) (credit, charge []messages.NathejkOrder_Line, total int) {
	for n, src := range paid {
		lineID := id.LineID(n)
		charge = append(charge, messages.NathejkOrder_Line{
			LineID:      lineID,
			ProductSKU:  src.ProductSKU,
			ProductName: src.ProductName,
			MemberID:    src.MemberID,
			UnitPrice:   src.UnitPrice,
			Quantity:    src.Quantity,
			LineTotal:   src.UnitPrice * src.Quantity,
			Origin:      messages.LineOriginManual,
			Attributes:  src.Attributes,
		})
		negated := charge[len(charge)-1]
		negated.Quantity = -src.Quantity
		negated.LineTotal = -src.UnitPrice * src.Quantity
		credit = append(credit, negated)
		total += src.UnitPrice * src.Quantity
	}
	return credit, charge, total
}

// balanced enforces the invariant that makes the whole operation auditable: the
// credit and the charge must cancel out. A transfer that does not net to zero has
// created or destroyed money.
func balanced(credit, charge []messages.NathejkOrder_Line) error {
	sum := 0
	for _, l := range credit {
		sum += l.LineTotal
	}
	for _, l := range charge {
		sum += l.LineTotal
	}
	if sum != 0 {
		return fmt.Errorf("%d: %w", sum, ErrNotBalanced)
	}
	return nil
}

// fundingOrder picks the paid order whose money the transfer is following.
//
// The one contributing the most value, with the lowest order id breaking a tie, so
// the choice is deterministic and describes where most of the money actually came
// from. Lines can come from several paid orders — a seat bought in the first round
// and a t-shirt added later — and provenance names one payment, so one of them has
// to be chosen rather than guessed at read time.
func fundingOrder(paid []order.MemberLine) string {
	byOrder := map[string]int{}
	for _, l := range paid {
		byOrder[l.OrderID] += l.UnitPrice * l.Quantity
	}
	best, bestTotal := "", 0
	for id, total := range byOrder {
		if best == "" || total > bestTotal || (total == bestTotal && id < best) {
			best, bestTotal = id, total
		}
	}
	return best
}

// publishHalf creates one side of the transfer: a dedicated order with the lines,
// the payment covering it, and the order.paid that closes it.
//
// A dedicated order, never EnsureOpenOrder: reusing the owner's existing open order
// would contaminate a real, unpaid order with transfer lines and make its total
// meaningless.
//
// amount is the order's total: negative for the credit half, positive for the
// charge half. A credit is covered by a negative payment, so paidAmount ==
// totalAmount on both halves and the two are audited identically.
//
// # Why this closes its own orders
//
// Because it can, and because leaving it to the payment saga did not work.
//
// The saga is a one-shot reaction to payment.received that reads its service's own
// projections, and this command publishes its events in a burst — the credit
// order's payment lands milliseconds after the order was created, before the order
// projector has necessarily caught up. The saga then retries for a couple of
// seconds and gives up permanently, because nothing will ever publish another
// payment.received for that order. The credit half loses that race structurally:
// it is published first, so its payment arrives when the projector has had the
// least time. In dev, two of three transfers left the sending team's credit order
// showing "Åben" with nothing owed, recoverable only by restarting the service so
// the stream replayed.
//
// The command already knows both orders are covered — it created the orders *and*
// the payments in the same operation. Rediscovering that fact asynchronously,
// through another consumer's lag, was the design flaw. Publishing order.paid here
// removes the race rather than widening a retry budget against a window with no
// upper bound, and makes a transfer as reliable as its own reassignment.
//
// This is not "a caller may assert paid". It is the same freedom Commands.Settle
// has, for the same reason: the order owes nothing, and here that is known rather
// than believed, having just been computed. The saga remains the only path for an
// order whose money arrives from outside.
func (c *commander) publishHalf(orderID string, ownerType types.TeamType, ownerID types.TeamID, lines []messages.NathejkOrder_Line, amount int, reference string, source *types.PaymentSource) error {
	now := time.Now()
	if err := c.publish(fmt.Sprintf("NATHEJK:%s.order.%s.created", c.year, orderID), &messages.NathejkOrderCreated{
		OrderID:   orderID,
		Year:      c.year,
		OwnerType: ownerType,
		OwnerID:   string(ownerID),
		Currency:  types.CurrencyDKK,
		Timestamp: now,
	}); err != nil {
		return err
	}
	total := 0
	for _, l := range lines {
		total += l.LineTotal
	}
	if err := c.publish(fmt.Sprintf("NATHEJK:%s.order.%s.lines.changed", c.year, orderID), &messages.NathejkOrderLinesChanged{
		OrderID:     orderID,
		Lines:       lines,
		TotalAmount: total,
		Timestamp:   now,
	}); err != nil {
		return err
	}

	// A transfer of nothing but zero-priced lines gets no payment: there would be
	// no money to record. Everything else is covered before it is closed, so the
	// payment trail always precedes the state change.
	if amount != 0 {
		if err := c.publishPayment(orderID, amount, reference, source, now); err != nil {
			return err
		}
	}

	// Same event, same shape, whatever the amount — including zero, which used to
	// be the only case handled here. PaidAmount is the order's own signed total, as
	// the saga would have published it.
	//
	// Idempotent by two independent guards, which matters because this deliberately
	// creates the case where the order is already paid by the time the saga sees
	// the payment: the projector's handlePaid updates WHERE status='open', and the
	// saga returns early on any order that is not open.
	return c.publish(fmt.Sprintf("NATHEJK:%s.order.%s.paid", c.year, orderID), &messages.NathejkOrderPaid{
		OrderID:    orderID,
		PaidAmount: amount,
		Timestamp:  now,
	})
}

// publishPayment covers one half of the transfer with an internal-transfer
// payment.
//
// Both requested *and* received, for the same reference and in that order. The
// payment projector only inserts a row on requested — reserved and received are
// UPDATEs keyed on reference — so a lone received updates nothing and the payment
// silently does not exist. And it has to reach received (or reserved) to count at
// all: every paid-amount computation filters status IN ('reserved','received'), so
// a payment left at requested is invisible to the order's paidAmount and the order
// would never settle.
//
// The provenance travels on requested, which is the only branch that writes the
// descriptive columns.
func (c *commander) publishPayment(orderID string, amount int, reference string, source *types.PaymentSource, now time.Time) error {
	if err := c.publish(fmt.Sprintf("NATHEJK:%s.payment.%s.requested", c.year, reference), &messages.NathejkPaymentRequested{
		Reference:       reference,
		Amount:          amount,
		Currency:        string(types.CurrencyDKK),
		Timestamp:       now,
		Method:          types.PaymentMethodInternalTransfer,
		OrderForeignKey: orderID,
		OrderType:       payment.OrderTypeOrder,
		Source:          source,
	}); err != nil {
		return err
	}
	return c.publish(fmt.Sprintf("NATHEJK:%s.payment.%s.received", c.year, reference), &messages.NathejkPaymentReceived{
		Reference: reference,
		Amount:    amount,
		Currency:  string(types.CurrencyDKK),
		Timestamp: now,
	})
}

// publishReassignment records the roster change. Its own event, not
// spejder.*.team.moved — see messages.NathejkMemberReassigned for why the
// difference in meaning is carried by the subject.
func (c *commander) publishReassignment(t Transfer, from types.TeamID) error {
	return c.publish(fmt.Sprintf("NATHEJK:%s.spejder.%s.reassigned", c.year, t.MemberID), &messages.NathejkMemberReassigned{
		MemberID:   t.MemberID,
		FromTeamID: from,
		ToTeamID:   t.ToTeamID,
		Actor:      t.Actor,
	})
}

func (c *commander) publish(subject string, body any) error {
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(subject))
	if err := msg.SetBody(body); err != nil {
		return err
	}
	return c.p.Publish(msg)
}

var _ Commands = (*commander)(nil)
