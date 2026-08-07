package order

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/tables/payment"
)

// PaymentReader is the small slice of the payment read API that the order
// saga consumes. Declared as an interface so this package takes no dependency
// on the payment entity's concrete read side — payment.Queries satisfies it,
// which the assertion below pins so the two cannot drift apart again.
type PaymentReader interface {
	GetByReference(ctx context.Context, reference string) (*payment.Payment, error)
}

var _ PaymentReader = payment.Queries(nil)

// DefaultSagaSettle is the total budget the saga spends waiting for the
// (eventually consistent) payment projection to reflect a just-received
// payment. It is spread across DefaultSagaAttempts reads rather than paid as a
// single up-front sleep, so an order whose projection is already current
// transitions immediately instead of always waiting the full interval.
const DefaultSagaSettle = 2 * time.Second

// DefaultSagaAttempts is how many times HandleMessage reads the projection
// before giving up on a not-yet-fully-paid order. Tolerates projection lag: a
// genuinely under-paid order simply exhausts the attempts and stays open.
const DefaultSagaAttempts = 5

// saga listens for payment events and transitions the corresponding order
// to status="paid" once cumulative payments cover its total. This is the
// only path by which orders reach StatusPaid; once there, the
// SetDerivedLines / AddManualLine / RemoveLine / Cancel commands all
// reject mutations with ErrNotOpen, giving the immutability guarantee
// users asked for.
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
	settle   time.Duration
	attempts int

	// live is false until CaughtUp fires, i.e. while the saga is replaying the
	// historical stream on startup. During replay the between-read waits are
	// skipped (see HandleMessage), turning an N×settle startup cost into none.
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

// NewSaga wires the payment->order paid saga. Pass the order Queries
// (typically the *table returned by order.New), a PaymentReader (typically
// the payment entity's *payment.Query), and the JetStream publisher.
// settle lets you tune or zero out the projection-catchup delay; pass 0
// to fall back to DefaultSagaSettle.
func NewSaga(p cqrs.Publisher, q Queries, payments PaymentReader, settle time.Duration) cqrs.Consumer {
	if settle <= 0 {
		settle = DefaultSagaSettle
	}
	return &saga{p: p, q: q, payments: payments, settle: settle, attempts: DefaultSagaAttempts}
}

func (s *saga) Consumes() []cqrs.Subject {
	// Subscribing to .received only is sufficient: by that point the
	// payment.received event has already been published, the prior
	// .reserved row carries the same amount, and the order's joined
	// paidAmount sums both reserved and received states. .reserved would
	// trigger the same transition slightly earlier, but at the cost of
	// firing twice per payment for no benefit.
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

func (s *saga) HandleMessage(msg cqrs.Message) error {
	var body messages.NathejkPaymentReceived
	if err := msg.Body(&body); err != nil {
		return err
	}
	if body.Reference == "" {
		return nil
	}

	// The order's joined paidAmount only counts payments in
	// {'reserved','received'}, and the payment projector runs on an
	// independent consumer, so right after a payment.received the projection
	// may not yet reflect it. Rather than a single fixed sleep-then-read
	// (which fires too early under load and too late otherwise), read up to
	// `attempts` times, waiting between tries until the order shows fully paid.
	//
	// The between-read wait is skipped entirely during replay (CaughtUp has not
	// fired): there the reads just run back-to-back, keeping startup fast. See
	// task 006. This still is not a cross-consumer barrier — a genuinely lagging
	// projector can outlast the budget — but it is strictly more robust than the
	// old single sleep.
	attempts := s.attempts
	if attempts < 1 {
		attempts = 1
	}
	var wait time.Duration
	if s.live.Load() {
		wait = s.settle / time.Duration(attempts)
	}
	for i := 0; i < attempts; i++ {
		if i > 0 && wait > 0 {
			s.nap(wait)
		}
		retry, err := s.attemptTransition(body.Reference)
		if err != nil {
			return err
		}
		if !retry {
			return nil
		}
	}
	// Budget exhausted: the order is still open and not yet fully covered.
	// Either the projection lagged beyond the budget, or the order is
	// genuinely under-paid; both leave it open, which is the safe outcome.
	return nil
}

// attemptTransition reads the payment and its order once and, if the order is
// open and fully paid, publishes NathejkOrderPaid. The bool reports whether a
// retry could still change the outcome: true only when the order exists, is
// open, but is not yet fully paid (a possible projection lag). Every terminal
// case — unknown/legacy reference, already-paid, cancelled, free, or a
// successful transition — returns false.
func (s *saga) attemptTransition(reference string) (retry bool, err error) {
	ctx := context.Background()
	pmt, err := s.payments.GetByReference(ctx, reference)
	if err != nil {
		// Reference not found / not interesting; nothing to do.
		return false, nil
	}
	if pmt == nil || pmt.OrderForeignKey == "" {
		return false, nil
	}

	// Legacy payments use the team/user ID as OrderForeignKey rather
	// than an order ID. GetByID will return ErrRecordNotFound for those —
	// silently skip; the saga is a no-op for the legacy flow by design.
	o, err := s.q.GetByID(ctx, pmt.OrderForeignKey)
	if err != nil {
		if errors.Is(err, tables.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if o.Status != StatusOpen {
		return false, nil
	}
	// A free order (TotalAmount == 0) shouldn't auto-transition on a random
	// payment hitting it — it'd never be in this code path without a positive
	// payment, but guard anyway.
	if o.TotalAmount <= 0 {
		return false, nil
	}
	// Open but not yet fully covered: could be projection lag — worth a retry.
	if o.PaidAmount < o.TotalAmount {
		return true, nil
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
		return false, err
	}
	return false, nil
}
