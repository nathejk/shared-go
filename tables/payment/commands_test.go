package payment

import (
	"errors"
	"strings"
	"testing"

	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/types"

	"github.com/jrgensen/cqrs/cqrstest"
)

// fakeProvider is a Provider whose responses and failures the test controls.
// Being able to write this at all is the point of the Provider port — before
// it, these paths could only be exercised against MobilePay's HTTP API.
type fakeProvider struct {
	created  []PaymentRequest
	captured []capture

	createResp PaymentCreated
	createErr  error
	auth       Authorization
	authErr    error
	captureErr error
}

type capture struct {
	reference string
	amount    Amount
}

func (f *fakeProvider) CreatePayment(req PaymentRequest) (PaymentCreated, error) {
	f.created = append(f.created, req)
	return f.createResp, f.createErr
}

func (f *fakeProvider) GetAuthorization(reference string) (Authorization, error) {
	return f.auth, f.authErr
}

func (f *fakeProvider) CapturePayment(reference string, amount Amount) error {
	f.captured = append(f.captured, capture{reference: reference, amount: amount})
	return f.captureErr
}

func newTestCommander(p *fakeProvider) (*commander, *cqrstest.Publisher) {
	pub := &cqrstest.Publisher{}
	return &commander{p: pub, pp: p}, pub
}

func TestRequestAuthorisesAndPublishes(t *testing.T) {
	prov := &fakeProvider{createResp: PaymentCreated{Reference: "ref-1", RedirectURL: "https://mp/redirect"}}
	c, pub := newTestCommander(prov)

	amount := Amount{Currency: types.CurrencyDKK, Value: 45000}
	url, err := c.Request(amount, "Nathejk tilmelding", types.PhoneNumber("40733886"), types.EmailAddress("a@b.dk"),
		"https://tilmelding.nathejk.dk/klan/t-1", "order-1", "order")
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if url != "https://mp/redirect" {
		t.Errorf("Request should return the provider's redirect URL, got %q", url)
	}

	if len(prov.created) != 1 {
		t.Fatalf("want 1 provider call, got %d", len(prov.created))
	}
	req := prov.created[0]
	if req.Amount != amount {
		t.Errorf("amount = %+v, want %+v", req.Amount, amount)
	}
	if req.Description != "Nathejk tilmelding" {
		t.Errorf("description = %q", req.Description)
	}
	// The payer is identified to the wallet in international form.
	if req.PhoneNumber != "4540733886" {
		t.Errorf("phone = %q, want international form 4540733886", req.PhoneNumber)
	}
	if req.Reference == "" {
		t.Error("a reference must be issued")
	}
	// The idempotency key guards the request; it must not be the payment's
	// name, or a retry would look like the same payment being renamed.
	if req.IdempotencyKey == "" || req.IdempotencyKey == req.Reference {
		t.Errorf("idempotency key %q must be present and distinct from reference %q", req.IdempotencyKey, req.Reference)
	}

	if len(pub.Messages) != 1 {
		t.Fatalf("want 1 event, got %d", len(pub.Messages))
	}
	if got := pub.Subjects()[0]; !strings.Contains(got, ".payment.ref-1.requested") {
		t.Errorf("subject %q should name the provider's reference", got)
	}
	var body messages.NathejkPaymentRequested
	if err := pub.Messages[0].Body(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The event carries the provider's reference, not the locally issued one:
	// that is what later payment events and the projection join on.
	if body.Reference != "ref-1" {
		t.Errorf("event reference = %q, want ref-1", body.Reference)
	}
	if body.Amount != 45000 || body.Currency != "DKK" {
		t.Errorf("event amount = %d %s, want 45000 DKK", body.Amount, body.Currency)
	}
	if body.OrderForeignKey != "order-1" || body.OrderType != "order" {
		t.Errorf("event should carry the order linkage, got %q/%q", body.OrderForeignKey, body.OrderType)
	}
	if body.ReturnUrl != "https://tilmelding.nathejk.dk/klan/t-1" {
		t.Errorf("event returnUrl = %q", body.ReturnUrl)
	}
}

func TestRequestPublishesNothingWhenProviderFails(t *testing.T) {
	prov := &fakeProvider{createErr: errors.New("mobilepay down")}
	c, pub := newTestCommander(prov)

	if _, err := c.Request(Amount{Currency: types.CurrencyDKK, Value: 100}, "d",
		types.PhoneNumber("40733886"), types.EmailAddress("a@b.dk"), "u", "o", "order"); err == nil {
		t.Fatal("expected the provider error to surface")
	}
	// No authorisation exists, so claiming one on the stream would corrupt the
	// projection.
	if len(pub.Messages) != 0 {
		t.Errorf("no event should be published, got %v", pub.Subjects())
	}
}

func TestCaptureTakesAuthorisedRemainder(t *testing.T) {
	// Partially captured already: only the remainder may be taken. This is the
	// arithmetic that matters — authorized and captured are cumulative totals.
	prov := &fakeProvider{auth: Authorization{
		Authorized:       true,
		Currency:         types.CurrencyDKK,
		AuthorizedAmount: 45000,
		CapturedAmount:   20000,
	}}
	c, pub := newTestCommander(prov)

	if err := c.Capture("ref-1"); err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if len(prov.captured) != 1 {
		t.Fatalf("want 1 capture, got %d", len(prov.captured))
	}
	want := Amount{Currency: types.CurrencyDKK, Value: 25000}
	if prov.captured[0].amount != want {
		t.Errorf("captured %+v, want %+v", prov.captured[0].amount, want)
	}
	if prov.captured[0].reference != "ref-1" {
		t.Errorf("captured reference = %q", prov.captured[0].reference)
	}

	// reserved is published before the capture, received after it, so a crash
	// mid-capture leaves evidence that money was expected.
	subjects := pub.Subjects()
	if len(subjects) != 2 {
		t.Fatalf("want 2 events, got %d: %v", len(subjects), subjects)
	}
	if !strings.Contains(subjects[0], ".payment.ref-1.reserved") {
		t.Errorf("first event should be reserved, got %q", subjects[0])
	}
	if !strings.Contains(subjects[1], ".payment.ref-1.received") {
		t.Errorf("second event should be received, got %q", subjects[1])
	}

	var reserved messages.NathejkPaymentReserved
	if err := pub.Messages[0].Body(&reserved); err != nil {
		t.Fatalf("decode reserved: %v", err)
	}
	if reserved.Amount != 25000 || reserved.Currency != "DKK" {
		t.Errorf("reserved = %d %s, want 25000 DKK", reserved.Amount, reserved.Currency)
	}
	var received messages.NathejkPaymentReceived
	if err := pub.Messages[1].Body(&received); err != nil {
		t.Fatalf("decode received: %v", err)
	}
	if received.Amount != 25000 {
		t.Errorf("received amount = %d, want 25000", received.Amount)
	}
}

func TestCaptureIsNoopWhenNothingToTake(t *testing.T) {
	for _, tc := range []struct {
		name string
		auth Authorization
	}{
		{
			// Created or expired, but never approved by the payer.
			name: "not authorized",
			auth: Authorization{Authorized: false, AuthorizedAmount: 45000},
		},
		{
			// Already fully captured. Capture is reachable more than once —
			// the callback URL is a plain GET the payer can reload — so this
			// must not double-charge or double-publish.
			name: "fully captured",
			auth: Authorization{Authorized: true, AuthorizedAmount: 45000, CapturedAmount: 45000},
		},
		{
			name: "captured beyond authorised",
			auth: Authorization{Authorized: true, AuthorizedAmount: 45000, CapturedAmount: 50000},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prov := &fakeProvider{auth: tc.auth}
			c, pub := newTestCommander(prov)

			if err := c.Capture("ref-1"); err != nil {
				t.Fatalf("Capture should succeed quietly, got %v", err)
			}
			if len(prov.captured) != 0 {
				t.Errorf("nothing should be captured, got %+v", prov.captured)
			}
			if len(pub.Messages) != 0 {
				t.Errorf("nothing should be published, got %v", pub.Subjects())
			}
		})
	}
}

func TestCaptureSurfacesLookupError(t *testing.T) {
	prov := &fakeProvider{authErr: errors.New("lookup failed")}
	c, pub := newTestCommander(prov)

	if err := c.Capture("ref-1"); err == nil {
		t.Fatal("expected the lookup error to surface")
	}
	if len(pub.Messages) != 0 {
		t.Errorf("nothing should be published, got %v", pub.Subjects())
	}
	if len(prov.captured) != 0 {
		t.Errorf("nothing should be captured, got %+v", prov.captured)
	}
}

func TestCaptureStopsAfterFailedCapture(t *testing.T) {
	prov := &fakeProvider{
		auth:       Authorization{Authorized: true, Currency: types.CurrencyDKK, AuthorizedAmount: 45000},
		captureErr: errors.New("capture rejected"),
	}
	c, pub := newTestCommander(prov)

	if err := c.Capture("ref-1"); err == nil {
		t.Fatal("expected the capture error to surface")
	}
	// reserved was already published (it precedes the capture), but received
	// must not be: no money changed hands.
	subjects := pub.Subjects()
	if len(subjects) != 1 {
		t.Fatalf("want only the reserved event, got %v", subjects)
	}
	if !strings.Contains(subjects[0], ".reserved") {
		t.Errorf("got %q, want the reserved event", subjects[0])
	}
}

var _ Provider = (*fakeProvider)(nil)
