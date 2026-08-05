package order

import (
	"context"
	"testing"
	"time"

	"github.com/jrgensen/cqrs"
	"github.com/jrgensen/cqrs/cqrstest"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/types"

	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/tables/payment"
)

// sagaFakeQueries is a Queries whose GetByID the test controls. It returns
// orders[call], with the last entry repeating, so a test can model a projection
// that lags (under-paid) then catches up (paid). Only GetByID is exercised by
// the saga; the rest satisfy the interface and are never called.
type sagaFakeQueries struct {
	orders []*Order
	err    error
	calls  int
}

func (f *sagaFakeQueries) GetByID(context.Context, string) (*Order, error) {
	if f.err != nil {
		return nil, f.err
	}
	o := f.orders[min(f.calls, len(f.orders)-1)]
	f.calls++
	return o, nil
}
func (*sagaFakeQueries) FindOpenOrder(context.Context, types.YearSlug, types.TeamType, string) (*Order, error) {
	return nil, tables.ErrRecordNotFound
}
func (*sagaFakeQueries) ListByOwner(context.Context, types.YearSlug, types.TeamType, string) ([]Order, error) {
	return nil, nil
}
func (*sagaFakeQueries) ReservedQuantity(context.Context, types.YearSlug, string) (int, error) {
	return 0, nil
}
func (*sagaFakeQueries) PaidQuantityBySKU(context.Context, types.YearSlug, types.TeamType, string) (map[string]int, error) {
	return nil, nil
}

type sagaFakePayments struct{ pmt *payment.Payment }

func (f sagaFakePayments) GetByReference(string) (*payment.Payment, error) { return f.pmt, nil }

// receivedMsg builds a payment.received message the way production does, so
// HandleMessage decodes the same body it would off the wire.
func receivedMsg(t *testing.T, reference string) cqrs.Message {
	t.Helper()
	m := cqrstest.NewMessage(cqrs.SubjectFromStr("NATHEJK:2026.payment." + reference + ".received"))
	if err := m.SetBody(&messages.NathejkPaymentReceived{Reference: reference}); err != nil {
		t.Fatalf("set body: %v", err)
	}
	return m
}

// newTestSaga wires a saga with fakes and a recording sleep seam. The order
// sequence is returned from GetByID in order, last repeating.
func newTestSaga(orders ...*Order) (*saga, *cqrstest.Publisher, *[]time.Duration) {
	pub := &cqrstest.Publisher{}
	var slept []time.Duration
	s := &saga{
		p:        pub,
		q:        &sagaFakeQueries{orders: orders},
		payments: sagaFakePayments{pmt: &payment.Payment{OrderForeignKey: "order-1"}},
		settle:   2 * time.Second,
		attempts: DefaultSagaAttempts,
		sleep:    func(d time.Duration) { slept = append(slept, d) },
	}
	return s, pub, &slept
}

func openPaidOrder() *Order {
	return &Order{OrderID: "order-1", Year: "2026", Status: StatusOpen, TotalAmount: 45000, PaidAmount: 45000}
}

func openUnpaidOrder() *Order {
	return &Order{OrderID: "order-1", Year: "2026", Status: StatusOpen, TotalAmount: 45000, PaidAmount: 20000}
}

// The whole point of task 006: the saga must expose CaughtUp() so the jetstream
// layer (which discovers it by runtime type assertion) can call it.
func TestSagaImplementsCatchupListener(t *testing.T) {
	var c cqrs.Consumer = NewSaga(&cqrstest.Publisher{}, &sagaFakeQueries{}, sagaFakePayments{}, 0)
	if _, ok := c.(interface{ CaughtUp() }); !ok {
		t.Fatal("saga must implement CaughtUp() so replay catch-up can be signalled")
	}
}

func TestSagaSkipsWaitsDuringReplay(t *testing.T) {
	// Order never becomes fully paid, so the saga exhausts its attempts. Still,
	// during replay (not live) it must never wait between reads.
	s, pub, slept := newTestSaga(openUnpaidOrder())

	if err := s.HandleMessage(receivedMsg(t, "ref-1")); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(*slept) != 0 {
		t.Errorf("no waits expected during replay, got %v", *slept)
	}
	if len(pub.Messages) != 0 {
		t.Errorf("under-paid order should not transition, got %v", pub.Subjects())
	}
}

func TestSagaTransitionsImmediatelyWhenAlreadyPaid(t *testing.T) {
	// Projection already current: the first read shows fully paid, so the saga
	// transitions without waiting even when live.
	s, pub, slept := newTestSaga(openPaidOrder())
	s.CaughtUp()

	if err := s.HandleMessage(receivedMsg(t, "ref-1")); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(*slept) != 0 {
		t.Errorf("no wait expected when the first read already shows paid, got %v", *slept)
	}
	if len(pub.Messages) != 1 {
		t.Fatalf("want 1 order.paid event, got %d", len(pub.Messages))
	}
}

func TestSagaRetriesUntilProjectionCatchesUp(t *testing.T) {
	// Live, and the projection lags: first read under-paid, second read paid.
	// The saga must wait once (settle/attempts) then transition.
	s, pub, slept := newTestSaga(openUnpaidOrder(), openPaidOrder())
	s.CaughtUp()

	if err := s.HandleMessage(receivedMsg(t, "ref-1")); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(*slept) != 1 {
		t.Fatalf("want exactly one between-read wait, got %v", *slept)
	}
	if want := 2 * time.Second / time.Duration(DefaultSagaAttempts); (*slept)[0] != want {
		t.Errorf("wait = %v, want settle/attempts = %v", (*slept)[0], want)
	}
	if len(pub.Messages) != 1 {
		t.Fatalf("want 1 order.paid event after catch-up, got %d", len(pub.Messages))
	}
}

func TestSagaGivesUpAfterMaxAttempts(t *testing.T) {
	// Live, projection never catches up: bounded by attempts, no transition,
	// and exactly attempts-1 waits (waits are between reads).
	s, pub, slept := newTestSaga(openUnpaidOrder())
	s.CaughtUp()

	if err := s.HandleMessage(receivedMsg(t, "ref-1")); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(*slept) != DefaultSagaAttempts-1 {
		t.Errorf("want %d waits, got %d", DefaultSagaAttempts-1, len(*slept))
	}
	if len(pub.Messages) != 0 {
		t.Errorf("no transition expected, got %v", pub.Subjects())
	}
}

func TestSagaTransitionsFullyPaidOpenOrder(t *testing.T) {
	s, pub, _ := newTestSaga(openPaidOrder())
	s.CaughtUp()

	if err := s.HandleMessage(receivedMsg(t, "ref-1")); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(pub.Messages) != 1 {
		t.Fatalf("want 1 order.paid event, got %d", len(pub.Messages))
	}
	if got := pub.Subjects()[0]; got != "NATHEJK.2026.order.order-1.paid" {
		t.Errorf("subject = %q, want the order paid subject", got)
	}
	var body messages.NathejkOrderPaid
	if err := pub.Messages[0].Body(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.OrderID != "order-1" || body.PaidAmount != 45000 {
		t.Errorf("unexpected paid body %+v", body)
	}
}

func TestSagaNoTransitionWhenNotOpenOrZeroTotal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		order *Order
	}{
		{"already paid", &Order{OrderID: "order-1", Status: StatusPaid, TotalAmount: 45000, PaidAmount: 45000}},
		{"zero total", &Order{OrderID: "order-1", Status: StatusOpen, TotalAmount: 0, PaidAmount: 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, pub, _ := newTestSaga(tc.order)
			s.CaughtUp()
			if err := s.HandleMessage(receivedMsg(t, "ref-1")); err != nil {
				t.Fatalf("HandleMessage: %v", err)
			}
			if len(pub.Messages) != 0 {
				t.Errorf("no transition expected, got %v", pub.Subjects())
			}
		})
	}
}
