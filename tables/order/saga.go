package order

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/tables/payment"
	"github.com/nathejk/shared-go/types"
)

// PaymentReader is the small slice of the payment read API that the order
// saga consumes. Declared as an interface so this package takes no dependency
// on the payment entity's concrete read side — payment.Queries satisfies it,
// which the assertion below pins so the two cannot drift apart again.
type PaymentReader interface {
	GetByReference(ctx context.Context, reference string) (*payment.Payment, error)
}

var _ PaymentReader = payment.Queries(nil)

// DefaultSagaSettle is the total budget the saga spends waiting for a
// projection to catch up with the event it is reacting to. It is spread across
// DefaultSagaAttempts reads rather than paid as a single up-front sleep, so an
// order whose projection is already current transitions immediately instead of
// always waiting the full interval.
const DefaultSagaSettle = 2 * time.Second

// DefaultSagaAttempts is how many times HandleMessage reads before giving up on
// an order it could not settle. Tolerates projection lag: a genuinely
// under-paid order simply exhausts the attempts and stays open.
const DefaultSagaAttempts = 5

// saga listens for payment events and transitions the corresponding order
// to status="paid" once cumulative payments cover its total. Once there, the
// SetDerivedLines / AddManualLine / RemoveLine / Cancel commands all
// reject mutations with ErrNotOpen, giving the immutability guarantee
// users asked for.
//
// This is the only path by which an order that *owes money* reaches StatusPaid.
// An order that owes nothing — one recording a free size change, say — will never
// see a payment, so Commands.Settle publishes the same event with a paid amount
// of zero. The two cannot be confused: Settle refuses a non-zero total, and the
// TotalAmount <= 0 guard in attemptTransition keeps the saga from settling a free
// order on the back of some unrelated payment.
//
// The saga is idempotent at multiple layers:
//
//   - It only emits NathejkOrderPaid when the order is currently
//     StatusOpen (a paid order returns early).
//   - The projector's handlePaid uses WHERE status='open' so a replayed
//     event is a no-op.
//
// It is *not* perfectly race-free: a partial-refund flow could land an
// order back below its total after status=paid. That isn't on the
// roadmap — flag for revisit if it ever is.
type saga struct {
	p        cqrs.Publisher
	q        Queries
	payments PaymentReader
	year     types.YearSlug
	settle   time.Duration
	attempts int

	// live is false until CaughtUp fires, i.e. while the saga is replaying the
	// historical stream on startup. Replay skips the waits that only help a
	// lagging payment projection, but not the ones that help an unprojected
	// order — see waitBeforeRetry.
	live atomic.Bool

	// sleep is a test seam; nil means time.Sleep.
	sleep func(time.Duration)
}

// CaughtUp marks the saga live: the stream has been replayed up to the point
// it had reached when the process started, so subsequent events are new rather
// than historical. It satisfies stream.CatchupListener, which the jetstream
// Subscribe path invokes once this consumer's backlog has drained (fixed in
// jrgensen/stream v0.1.2). The interface is discovered by a runtime type
// assertion on the concrete type, so this package need not import stream to
// participate — the local assertion below documents the contract instead.
func (s *saga) CaughtUp() { s.live.Store(true) }

var _ interface{ CaughtUp() } = (*saga)(nil)

// NewSaga wires the payment->order paid saga. Pass the order Queries (typically
// the *table returned by order.New), a PaymentReader (typically the payment
// entity, whose Queries satisfies it), and the JetStream publisher.
//
// year is the season this saga settles orders for; payments from any other
// season are ignored (see forOurSeason). Pass "" to consider every season,
// which is what callers written before this parameter existed effectively did.
//
// settle lets you tune or zero out the projection-catchup delay; pass 0 to fall
// back to DefaultSagaSettle.
func NewSaga(p cqrs.Publisher, q Queries, payments PaymentReader, year types.YearSlug, settle time.Duration) cqrs.Consumer {
	if settle <= 0 {
		settle = DefaultSagaSettle
	}
	return &saga{p: p, q: q, payments: payments, year: year, settle: settle, attempts: DefaultSagaAttempts}
}

func (s *saga) Consumes() []cqrs.Subject {
	// Subscribing to .received only is sufficient: by that point the
	// payment.received event has already been published, the prior
	// .reserved row carries the same amount, and the order's joined
	// paidAmount sums both reserved and received states. .reserved would
	// trigger the same transition slightly earlier, but at the cost of
	// firing twice per payment for no benefit.
	//
	// The year stays a wildcard even though the saga only acts on its own
	// season: the filter belongs somewhere a misconfiguration is visible, not
	// in a subject that would silently match nothing if year were empty.
	return []cqrs.Subject{
		cqrs.SubjectFromStr("NATHEJK:*.payment.*.received"),
	}
}

func (s *saga) nap(d time.Duration) {
	if s.sleep != nil {
		s.sleep(d)
		return
	}
	time.Sleep(d)
}

// forOurSeason reports whether a payment event belongs to the season this saga
// settles orders for.
//
// Older seasons are closed: their orders are done and nothing about them can
// change, so re-deriving them on every replay is wasted work. It also keeps the
// unprojected case below meaningful — payments from before the order entity
// existed name a team rather than an order, so every one of them would look
// like an order the saga cannot find.
//
// Subjects are NATHEJK.<year>.payment.<reference>.received; cqrs normalises the
// domain separator, so the year is always Parts()[1].
func (s *saga) forOurSeason(subj cqrs.Subject) bool {
	if s.year == "" {
		return true
	}
	parts := subj.Parts()
	if len(parts) < 2 {
		return false
	}
	return strings.EqualFold(parts[1], string(s.year))
}

// attemptResult is what one pass over the payment and its order found, and
// therefore whether reading again can change the answer.
type attemptResult int

const (
	// resultSettled: nothing further to do. The order transitioned, or was
	// already paid, cancelled or free, or the payment names no order this saga
	// is responsible for.
	resultSettled attemptResult = iota

	// resultUnderpaid: the order is open but the payments on it do not cover
	// its total. Either the payment projection has yet to catch up, or the
	// order genuinely is not paid for; the two are indistinguishable from here,
	// which is why the attempts are bounded.
	resultUnderpaid

	// resultUnprojected: the payment, or the order it names, is not in the read
	// model yet, though the events that create it are on the stream. Only
	// another consumer making progress resolves this.
	resultUnprojected
)

func (s *saga) HandleMessage(msg cqrs.Message) error {
	if !s.forOurSeason(msg.Subject()) {
		return nil
	}
	var body messages.NathejkPaymentReceived
	if err := msg.Body(&body); err != nil {
		return err
	}
	if body.Reference == "" {
		return nil
	}

	// The order's joined paidAmount only counts payments in
	// {'reserved','received'}, and the payment projector and the order
	// projector are consumers in their own right, so right after a
	// payment.received neither projection is guaranteed to reflect the event
	// yet. Rather than a single fixed sleep-then-read (which fires too early
	// under load and too late otherwise), read up to `attempts` times, pausing
	// between tries when a pause could plausibly help.
	attempts := s.attempts
	if attempts < 1 {
		attempts = 1
	}
	wait := s.settle / time.Duration(attempts)
	last := resultSettled
	for i := 0; i < attempts; i++ {
		if i > 0 && s.waitBeforeRetry(last) {
			s.nap(wait)
		}
		res, err := s.attemptTransition(body.Reference)
		if err != nil {
			return err
		}
		if res == resultSettled {
			return nil
		}
		last = res
	}
	// Budget exhausted.
	if last == resultUnprojected {
		// Worth saying out loud: unlike an under-paid order, this one is
		// expected to be payable and we simply never saw it. It resolves on a
		// later replay, since the events are on the stream permanently, but
		// until then the order shows as open despite being paid.
		log.Printf("order saga: payment %s: order still not projected after %d attempts; will settle on a later replay", body.Reference, attempts)
	}
	// Either projection lagged beyond the budget, or the order is genuinely
	// under-paid; both leave it open, which is the safe outcome.
	return nil
}

// waitBeforeRetry reports whether pausing before the next read could change the
// outcome.
//
// An unprojected order or payment always warrants the pause, replay or not: the
// only thing that can fix it is another consumer advancing, and back-to-back
// reads in this goroutine give it no chance to. Pausing during replay is
// self-limiting rather than a per-event cost — the other projector uses the
// pause to get ahead of this consumer and stays there, so a rebuild pays it a
// handful of times, not once per payment.
//
// An under-paid order during replay does not warrant it. There the reads run
// back-to-back, which keeps a rebuild fast: the common reason to be under-paid
// mid-replay is that the order really was under-paid at that point in history,
// and no amount of waiting changes what a replayed event says.
func (s *saga) waitBeforeRetry(last attemptResult) bool {
	if last == resultUnprojected {
		return true
	}
	return s.live.Load()
}

// attemptTransition reads the payment and its order once and, if the order is
// open and fully paid, publishes NathejkOrderPaid.
//
// Errors other than "not found" are returned rather than swallowed: a failed
// read is not evidence that an order should stay open, and the dead-letter
// writer exists to record exactly this.
func (s *saga) attemptTransition(reference string) (attemptResult, error) {
	ctx := context.Background()
	pmt, err := s.payments.GetByReference(ctx, reference)
	switch {
	case errors.Is(err, tables.ErrRecordNotFound):
		// We are reacting to this payment's own event, so the payment exists on
		// the stream; the payment projector has just not written it yet.
		return resultUnprojected, nil
	case err != nil:
		return resultSettled, err
	}
	if pmt == nil || pmt.OrderForeignKey == "" {
		return resultSettled, nil
	}
	// Payments made before the order entity landed put a team or user id in
	// OrderForeignKey and name the kind in OrderType. There is no order to
	// settle and never will be, so this is terminal rather than a projection
	// race — telling the two apart is what keeps the retries below meaningful.
	if pmt.OrderType != payment.OrderTypeOrder {
		return resultSettled, nil
	}

	o, err := s.q.GetByID(ctx, pmt.OrderForeignKey)
	switch {
	case errors.Is(err, tables.ErrRecordNotFound):
		// The payment names an order, so the order was created and its events
		// are on the stream — the order projector is simply behind this saga.
		// Retryable: this is the replay race that would otherwise leave a paid
		// order showing as open for the lifetime of the process.
		return resultUnprojected, nil
	case err != nil:
		return resultSettled, err
	}
	if o.Status != StatusOpen {
		return resultSettled, nil
	}
	// A free order (TotalAmount == 0) shouldn't auto-transition on a random
	// payment hitting it — it'd never be in this code path without a positive
	// payment, but guard anyway.
	if o.TotalAmount <= 0 {
		return resultSettled, nil
	}
	if o.PaidAmount < o.TotalAmount {
		return resultUnderpaid, nil
	}

	paid := messages.NathejkOrderPaid{
		OrderID:    o.OrderID,
		PaidAmount: o.PaidAmount,
		Timestamp:  time.Now(),
	}
	subj := cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.order.%s.paid", o.Year, o.OrderID))
	out := s.p.MessageFunc()(subj)
	out.SetBody(&paid)
	if err := s.p.Publish(out); err != nil {
		log.Printf("order saga: publish paid for %s: %v", o.OrderID, err)
		return resultSettled, err
	}
	return resultSettled, nil
}
