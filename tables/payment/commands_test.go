package payment

import (
	"context"
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
	return &commander{p: pub, r: NewRepository(WithProvider(p)), year: "2026"}, pub
}

// An unwired provider must report itself rather than panic on a nil interface
// in the middle of a payment attempt.
func TestCommandsWithoutAProviderFailLoudly(t *testing.T) {
	pub := &cqrstest.Publisher{}
	c := &commander{p: pub, r: NewRepository(), year: "2026"}

	if _, err := c.Request(Charge{Amount: Amount{}, Description: "d", Phone: "40733886", Email: "a@b.dk", ReturnUrl: "u", OrderID: "order-1"}); !errors.Is(err, ErrNoProvider) {
		t.Errorf("Request err = %v, want ErrNoProvider", err)
	}
	if err := c.Capture("ref-1"); !errors.Is(err, ErrNoProvider) {
		t.Errorf("Capture err = %v, want ErrNoProvider", err)
	}
	if len(pub.Messages) != 0 {
		t.Errorf("nothing should be published, got %v", pub.Subjects())
	}
}

func TestRequestAuthorisesAndPublishes(t *testing.T) {
	prov := &fakeProvider{createResp: PaymentCreated{Reference: "ref-1", RedirectURL: "https://mp/redirect"}}
	c, pub := newTestCommander(prov)

	amount := Amount{Currency: types.CurrencyDKK, Value: 45000}
	url, err := c.Request(Charge{
		Amount:      amount,
		Description: "Nathejk tilmelding",
		Phone:       types.PhoneNumber("40733886"),
		Email:       types.EmailAddress("a@b.dk"),
		ReturnUrl:   "https://tilmelding.nathejk.dk/klan/t-1",
		OrderID:     "order-1",
	})
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
	// The event keeps the projection's polymorphic field names, and the caller
	// no longer supplies the type: every payment this entity creates is for an
	// order, so the commander stamps it.
	if body.OrderForeignKey != "order-1" || body.OrderType != orderTypeOrder {
		t.Errorf("event should carry the order linkage, got %q/%q", body.OrderForeignKey, body.OrderType)
	}
	if body.ReturnUrl != "https://tilmelding.nathejk.dk/klan/t-1" {
		t.Errorf("event returnUrl = %q", body.ReturnUrl)
	}
}

func TestRequestPublishesNothingWhenProviderFails(t *testing.T) {
	prov := &fakeProvider{createErr: errors.New("mobilepay down")}
	c, pub := newTestCommander(prov)

	if _, err := c.Request(Charge{
		Amount: Amount{Currency: types.CurrencyDKK, Value: 100}, Description: "d",
		Phone: "40733886", Email: "a@b.dk", ReturnUrl: "u",
		OrderID: "o",
	}); err == nil {
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

// The receipt reaches both the provider and the event, so the payer sees it in
// the wallet and it stays on the record afterwards.
func TestRequestCarriesReceiptLinesToProviderAndEvent(t *testing.T) {
	prov := &fakeProvider{createResp: PaymentCreated{Reference: "ref-1", RedirectURL: "https://mp/r"}}
	c, pub := newTestCommander(prov)

	lines := []Line{
		{Label: "Patrulje-deltagelse", UnitCount: 1, UnitPrice: 25000, Amount: 25000},
		{Label: "T-shirt (Large)", UnitCount: 1, UnitPrice: 17500, Amount: 17500},
	}
	if _, err := c.Request(Charge{
		Amount: Amount{Currency: types.CurrencyDKK, Value: 42500},
		Phone:  "40733886", Email: "a@b.dk", OrderID: "order-1",
		Lines: lines,
	}); err != nil {
		t.Fatalf("Request: %v", err)
	}

	if got := prov.created[0].Lines; len(got) != 2 || got[1].Label != "T-shirt (Large)" {
		t.Errorf("provider should receive the receipt, got %+v", got)
	}

	var body messages.NathejkPaymentRequested
	if err := pub.Messages[0].Body(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.OrderLines) != 2 {
		t.Fatalf("event should carry the lines, got %+v", body.OrderLines)
	}
	want := messages.NathejkPayment_OrderLine{Label: "T-shirt (Large)", UnitCount: 1, UnitPrice: 17500, Amount: 17500}
	if body.OrderLines[1] != want {
		t.Errorf("event line = %+v, want %+v", body.OrderLines[1], want)
	}
}

// A receipt that does not sum to the charge is dropped, not forwarded and not
// fatal: the payment is still worth taking, and a provider may reject a receipt
// that does not add up. This is reachable whenever an order is partly paid,
// since its lines sum to the total while the charge is the outstanding amount.
func TestRequestDropsReceiptThatDoesNotSumToTheCharge(t *testing.T) {
	prov := &fakeProvider{createResp: PaymentCreated{Reference: "ref-1", RedirectURL: "https://mp/r"}}
	c, pub := newTestCommander(prov)

	if _, err := c.Request(Charge{
		// Charging 25000 but describing 42500 worth of goods.
		Amount: Amount{Currency: types.CurrencyDKK, Value: 25000},
		Phone:  "40733886", Email: "a@b.dk", OrderID: "order-1",
		Lines: []Line{
			{Label: "Patrulje-deltagelse", UnitCount: 1, UnitPrice: 25000, Amount: 25000},
			{Label: "T-shirt (Large)", UnitCount: 1, UnitPrice: 17500, Amount: 17500},
		},
	}); err != nil {
		t.Fatalf("Request should still succeed, got %v", err)
	}

	if got := prov.created[0].Lines; got != nil {
		t.Errorf("no receipt should reach the provider, got %+v", got)
	}
	if prov.created[0].Amount.Value != 25000 {
		t.Errorf("the charged amount must be untouched, got %d", prov.created[0].Amount.Value)
	}
	var body messages.NathejkPaymentRequested
	if err := pub.Messages[0].Body(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.OrderLines) != 0 {
		t.Errorf("event should carry no lines, got %+v", body.OrderLines)
	}
}

func TestChargeLinesReconcile(t *testing.T) {
	for _, tc := range []struct {
		name string
		ch   Charge
		want bool
	}{
		{"no lines is fine", Charge{Amount: Amount{Value: 100}}, true},
		{"exact", Charge{Amount: Amount{Value: 100}, Lines: []Line{{Amount: 60}, {Amount: 40}}}, true},
		{"under", Charge{Amount: Amount{Value: 100}, Lines: []Line{{Amount: 60}}}, false},
		{"over", Charge{Amount: Amount{Value: 100}, Lines: []Line{{Amount: 60}, {Amount: 60}}}, false},
	} {
		if got := tc.ch.linesReconcile(); got != tc.want {
			t.Errorf("%s: linesReconcile() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// fakeQuerier answers the reference-uniqueness lookup. taken lists references it
// should report as already in use; anything else is free.
type fakeQuerier struct {
	taken   map[string]bool
	err     error
	lookups []string
}

func (f *fakeQuerier) GetByReference(_ context.Context, ref string) (*Payment, error) {
	f.lookups = append(f.lookups, ref)
	if f.err != nil {
		return nil, f.err
	}
	if f.taken[ref] {
		return &Payment{Reference: ref}, nil
	}
	return nil, ErrRecordNotFound
}
func (f *fakeQuerier) GetAll(context.Context, Filter) ([]Payment, error) { return nil, nil }
func (f *fakeQuerier) AmountPaid(context.Context, Filter) (int, error)   { return 0, nil }

var _ Queries = (*fakeQuerier)(nil)

// The reference is customer-visible, so it is short and readable rather than a
// UUID — but it still has to be the identity the provider and the event agree on.
func TestRequestIssuesAReadableReference(t *testing.T) {
	prov := &fakeProvider{}
	// Echo back whatever reference it is handed, as MobilePay does.
	prov.createResp = PaymentCreated{RedirectURL: "https://mp/r"}
	c, pub := newTestCommander(prov)
	c.q = &fakeQuerier{}

	if _, err := c.Request(Charge{Amount: Amount{Currency: types.CurrencyDKK, Value: 100}, OrderID: "order-1"}); err != nil {
		t.Fatalf("Request: %v", err)
	}

	ref := prov.created[0].Reference
	if len(ref) != referenceLength {
		t.Errorf("reference %q is not the expected length", ref)
	}
	if strings.ContainsAny(ref, "-_") {
		t.Errorf("reference %q should be a bare id with no separators", ref)
	}
	// The uniqueness check must have looked up the reference actually used.
	if got := c.q.(*fakeQuerier).lookups; len(got) != 1 || got[0] != ref {
		t.Errorf("lookups = %v, want exactly [%s]", got, ref)
	}
	if len(pub.Messages) != 1 {
		t.Fatalf("want 1 event, got %d", len(pub.Messages))
	}
}

// A duplicate would be overwritten silently by the projector's upsert, so a
// reference already in use must be discarded and another minted.
//
// A real collision cannot be provoked — that is the point of 40 bits — so the
// lookup reports the first attempt as taken instead, which drives the same path.
func TestRequestRetriesWhenTheReferenceIsTaken(t *testing.T) {
	prov := &fakeProvider{createResp: PaymentCreated{RedirectURL: "https://mp/r"}}
	c, _ := newTestCommander(prov)
	fq := &firstTakenQuerier{}
	c.q = fq

	if _, err := c.Request(Charge{Amount: Amount{Value: 100}}); err != nil {
		t.Fatalf("Request: %v", err)
	}

	if len(fq.lookups) != 2 {
		t.Fatalf("want 2 lookups (one rejected, one accepted), got %v", fq.lookups)
	}
	used := prov.created[0].Reference
	if used == fq.lookups[0] {
		t.Errorf("used the reference reported as taken: %q", used)
	}
	if used != fq.lookups[1] {
		t.Errorf("used %q but verified %q", used, fq.lookups[1])
	}
	// The second draw must be a different reference, not a retry of the same one.
	if fq.lookups[0] == fq.lookups[1] {
		t.Errorf("both attempts drew %q; the generator is not being re-run", fq.lookups[0])
	}
}

// firstTakenQuerier reports the first reference it is asked about as in use and
// every later one as free.
type firstTakenQuerier struct {
	fakeQuerier
	asked int
}

func (q *firstTakenQuerier) GetByReference(_ context.Context, ref string) (*Payment, error) {
	q.lookups = append(q.lookups, ref)
	q.asked++
	if q.asked == 1 {
		return &Payment{Reference: ref}, nil
	}
	return nil, ErrRecordNotFound
}

// Every attempt colliding must fail the payment rather than proceed with a
// reference known to be in use.
func TestRequestFailsWhenEveryReferenceIsTaken(t *testing.T) {
	prov := &fakeProvider{createResp: PaymentCreated{RedirectURL: "https://mp/r"}}
	c, pub := newTestCommander(prov)
	c.q = &alwaysTakenQuerier{}

	if _, err := c.Request(Charge{Amount: Amount{Value: 100}}); err == nil {
		t.Fatal("expected Request to fail rather than reuse a reference")
	}
	if len(prov.created) != 0 {
		t.Errorf("no payment should be created, got %+v", prov.created)
	}
	if len(pub.Messages) != 0 {
		t.Errorf("nothing should be published, got %v", pub.Subjects())
	}
}

type alwaysTakenQuerier struct{ fakeQuerier }

func (a *alwaysTakenQuerier) GetByReference(_ context.Context, ref string) (*Payment, error) {
	a.lookups = append(a.lookups, ref)
	return &Payment{Reference: ref}, nil
}

// A failing lookup must not block a payment: the reference is almost certainly
// free, and refusing money because a read failed is the worse outcome.
func TestRequestProceedsWhenTheUniquenessCheckErrors(t *testing.T) {
	prov := &fakeProvider{createResp: PaymentCreated{RedirectURL: "https://mp/r"}}
	c, _ := newTestCommander(prov)
	c.q = &fakeQuerier{err: errors.New("database down")}

	if _, err := c.Request(Charge{Amount: Amount{Value: 100}}); err != nil {
		t.Fatalf("Request should proceed, got %v", err)
	}
	if len(prov.created) != 1 {
		t.Errorf("the payment should still have been created")
	}
}
