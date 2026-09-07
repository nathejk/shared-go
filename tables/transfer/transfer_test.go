package transfer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jrgensen/cqrs/cqrstest"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/tables/order"
	"github.com/nathejk/shared-go/types"
)

type fakeRoster struct {
	teamID types.TeamID
	err    error
}

func (f fakeRoster) TeamIDOf(context.Context, types.YearSlug, types.MemberID) (types.TeamID, error) {
	return f.teamID, f.err
}

type fakeLines struct {
	lines []order.MemberLine
	err   error
	calls int
}

func (f *fakeLines) PaidLinesByMember(context.Context, types.YearSlug, types.TeamType, string, string) ([]order.MemberLine, error) {
	f.calls++
	return f.lines, f.err
}

type fakeSources struct {
	source *types.PaymentSource
	asked  []string
}

func (f *fakeSources) SourceOf(_ context.Context, reference string) *types.PaymentSource {
	return f.source
}

func (f *fakeSources) SourceOfOrder(_ context.Context, orderID string) *types.PaymentSource {
	f.asked = append(f.asked, orderID)
	return f.source
}

// seat is the participation line every member has once they are paid for.
func seat(orderID string) order.MemberLine {
	return order.MemberLine{
		OrderID: orderID,
		Line: order.Line{
			LineID:      "derived:participation.patrulje:m-1",
			ProductSKU:  "participation.patrulje",
			ProductName: "Deltager",
			MemberID:    "m-1",
			UnitPrice:   45000,
			Quantity:    1,
			LineTotal:   45000,
			Origin:      string(messages.LineOriginDerived),
		},
	}
}

// shirt is merchandise, which is what makes attributes load-bearing: the size
// lives there and the order is authoritative for shipping.
func shirt(orderID string) order.MemberLine {
	return order.MemberLine{
		OrderID: orderID,
		Line: order.Line{
			LineID:      "derived:tshirt:m-1:xl",
			ProductSKU:  "tshirt",
			ProductName: "T-shirt",
			MemberID:    "m-1",
			UnitPrice:   12000,
			Quantity:    1,
			LineTotal:   12000,
			Origin:      string(messages.LineOriginDerived),
			Attributes:  map[string]any{"size": "xl"},
		},
	}
}

func newTestCommander(lines ...order.MemberLine) (*commander, *cqrstest.Publisher, *fakeSources) {
	pub := &cqrstest.Publisher{}
	src := &fakeSources{source: &types.PaymentSource{
		Reference: "AAAAAAAAAAAA",
		OwnerType: types.TeamTypePatrulje,
		OwnerID:   "team-from",
		Method:    types.PaymentMethodMobilePay,
		PaidAt:    "2026-06-04T12:00:00Z",
	}}
	c := &commander{
		p:       pub,
		roster:  fakeRoster{teamID: "team-from"},
		lines:   &fakeLines{lines: lines},
		sources: src,
		year:    "2026",
	}
	return c, pub, src
}

func request() Transfer {
	return Transfer{MemberID: "m-1", ToTeamID: "team-to", TeamType: types.TeamTypePatrulje}
}

// linesOf decodes the lines.changed event for the given order id.
func linesOf(t *testing.T, pub *cqrstest.Publisher, orderID string) messages.NathejkOrderLinesChanged {
	t.Helper()
	for i, subj := range pub.Subjects() {
		if strings.Contains(subj, orderID) && strings.HasSuffix(subj, ".lines.changed") {
			var body messages.NathejkOrderLinesChanged
			if err := pub.Messages[i].Body(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			return body
		}
	}
	t.Fatalf("no lines.changed for %s in %v", orderID, pub.Subjects())
	return messages.NathejkOrderLinesChanged{}
}

// The invariant the whole operation rests on, and the cheapest thing to test: the
// pair must cancel out. If it does not, money has been created or destroyed.
func TestTransferNetsToZero(t *testing.T) {
	c, pub, _ := newTestCommander(seat("o-1"), shirt("o-1"))

	res, err := c.TransferMember(context.Background(), request())
	if err != nil {
		t.Fatalf("TransferMember: %v", err)
	}
	credit := linesOf(t, pub, res.CreditOrderID)
	charge := linesOf(t, pub, res.ChargeOrderID)

	if credit.TotalAmount+charge.TotalAmount != 0 {
		t.Errorf("credit %d + charge %d != 0", credit.TotalAmount, charge.TotalAmount)
	}
	if charge.TotalAmount != 57000 {
		t.Errorf("charge total = %d, want the seat plus the shirt", charge.TotalAmount)
	}
	if res.Amount != 57000 {
		t.Errorf("Result.Amount = %d, want the value moved", res.Amount)
	}
}

// Merchandise moves with the member, at the price snapshotted on the original line,
// and the size survives. A t-shirt is bought for a person, not for a team, and
// losing the size misdirects a physical object.
func TestTransferCarriesMerchandiseWithItsSizeAndPrice(t *testing.T) {
	c, pub, _ := newTestCommander(seat("o-1"), shirt("o-1"))

	res, err := c.TransferMember(context.Background(), request())
	if err != nil {
		t.Fatalf("TransferMember: %v", err)
	}
	charge := linesOf(t, pub, res.ChargeOrderID)
	if len(charge.Lines) != 2 {
		t.Fatalf("want both lines moved, got %d", len(charge.Lines))
	}
	var moved *messages.NathejkOrder_Line
	for i := range charge.Lines {
		if charge.Lines[i].ProductSKU == "tshirt" {
			moved = &charge.Lines[i]
		}
	}
	if moved == nil {
		t.Fatal("the t-shirt did not move")
	}
	if moved.UnitPrice != 12000 {
		t.Errorf("unitPrice = %d, want the price snapshotted on the original line", moved.UnitPrice)
	}
	if got := moved.Attributes["size"]; got != "xl" {
		t.Errorf("size = %v, want xl to survive the move", got)
	}
	if moved.MemberID != "m-1" {
		t.Errorf("memberId = %q, must follow the member", moved.MemberID)
	}
	// Manual, not derived: a derived line is replaced wholesale by
	// SetDerivedLines, so a transfer line recorded as derived would disappear the
	// next time the destination's order was saved.
	if moved.Origin != messages.LineOriginManual {
		t.Errorf("origin = %q, want manual so SetDerivedLines cannot erase it", moved.Origin)
	}

	credit := linesOf(t, pub, res.CreditOrderID)
	for _, l := range credit.Lines {
		if l.Quantity >= 0 {
			t.Errorf("credit line %s has quantity %d, want a credit", l.LineID, l.Quantity)
		}
		if l.ProductSKU == "tshirt" && l.Attributes["size"] != "xl" {
			t.Errorf("the credit must reclaim the same size, got %v", l.Attributes)
		}
	}
}

// Ordering is the reason the roster half and the money half are one command: money
// first, reassignment last. Of the two partial failures, the one this leaves is the
// one somebody notices.
func TestTransferPublishesMoneyBeforeTheReassignment(t *testing.T) {
	c, pub, _ := newTestCommander(seat("o-1"))

	if _, err := c.TransferMember(context.Background(), request()); err != nil {
		t.Fatalf("TransferMember: %v", err)
	}
	subjects := pub.Subjects()
	last := subjects[len(subjects)-1]
	if !strings.HasSuffix(last, ".reassigned") {
		t.Errorf("the reassignment must be published last, got order:\n%v", subjects)
	}
	// And it must be the only one.
	n := 0
	for _, s := range subjects {
		if strings.HasSuffix(s, ".reassigned") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("want exactly one reassignment, got %d in %v", n, subjects)
	}
}

// A member who has not been paid for is reassigned all the same, and no orders are
// created — there is nothing to transfer. Ordinary, not an error.
func TestTransferWithoutAPaidSeatOnlyReassigns(t *testing.T) {
	c, pub, src := newTestCommander()

	res, err := c.TransferMember(context.Background(), request())
	if err != nil {
		t.Fatalf("TransferMember: %v", err)
	}
	if res.LineCount != 0 || res.Amount != 0 {
		t.Errorf("want an empty transfer, got %+v", res)
	}
	if got := pub.Subjects(); len(got) != 1 || !strings.HasSuffix(got[0], ".reassigned") {
		t.Errorf("want only the reassignment, got %v", got)
	}
	if len(src.asked) != 0 {
		t.Error("no provenance lookup is needed when no money moves")
	}
}

// Every id is a function of the transfer, so running the command twice re-states
// the same transfer instead of creating a second one — which is what makes a
// double-submitted form harmless and a replay reproducible.
func TestTransferIsDeterministicAcrossRuns(t *testing.T) {
	first, pubA, _ := newTestCommander(seat("o-1"), shirt("o-1"))
	resA, err := first.TransferMember(context.Background(), request())
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, pubB, _ := newTestCommander(seat("o-1"), shirt("o-1"))
	resB, err := second.TransferMember(context.Background(), request())
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if resA.Identity != resB.Identity {
		t.Errorf("identity differs between runs:\n%+v\n%+v", resA.Identity, resB.Identity)
	}
	subjA, subjB := pubA.Subjects(), pubB.Subjects()
	if strings.Join(subjA, "\n") != strings.Join(subjB, "\n") {
		t.Errorf("subjects differ between runs:\n%v\n%v", subjA, subjB)
	}
	// Line ids too: the projectors upsert by (orderId, lineId), so a second run
	// must land on the same rows rather than adding a parallel set.
	for _, orderID := range []string{resA.CreditOrderID, resA.ChargeOrderID} {
		a, b := linesOf(t, pubA, orderID), linesOf(t, pubB, orderID)
		for i := range a.Lines {
			if a.Lines[i].LineID != b.Lines[i].LineID {
				t.Errorf("line id %q != %q", a.Lines[i].LineID, b.Lines[i].LineID)
			}
		}
	}
	// A different destination is a different transfer.
	other := request()
	other.ToTeamID = "team-other"
	third, _, _ := newTestCommander(seat("o-1"))
	resC, err := third.TransferMember(context.Background(), other)
	if err != nil {
		t.Fatalf("third: %v", err)
	}
	if resC.TransferID == resA.TransferID {
		t.Error("a different destination must be a different transfer")
	}
}

// Both halves are covered by internal-transfer payments: requested then received,
// same reference, signed to match their order. requested is the only branch that
// inserts the row, and reserved/received are the only statuses that count towards
// an order's paidAmount — a payment left at requested is invisible to every sum
// and the order would never settle.
func TestTransferCoversBothHalvesWithInternalTransferPayments(t *testing.T) {
	c, pub, _ := newTestCommander(seat("o-1"))

	res, err := c.TransferMember(context.Background(), request())
	if err != nil {
		t.Fatalf("TransferMember: %v", err)
	}

	for _, tc := range []struct {
		name      string
		reference string
		want      int
		orderID   string
	}{
		{"credit", res.CreditReference, -45000, res.CreditOrderID},
		{"charge", res.ChargeReference, 45000, res.ChargeOrderID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requestedAt, receivedAt = -1, -1
			for i, s := range pub.Subjects() {
				switch {
				case strings.Contains(s, tc.reference) && strings.HasSuffix(s, ".requested"):
					requestedAt = i
				case strings.Contains(s, tc.reference) && strings.HasSuffix(s, ".received"):
					receivedAt = i
				}
			}
			if requestedAt < 0 || receivedAt < 0 {
				t.Fatalf("want both a requested and a received for %s, got %v", tc.reference, pub.Subjects())
			}
			if requestedAt > receivedAt {
				t.Error("requested must come first: received is an UPDATE keyed on reference and inserts nothing")
			}

			var req messages.NathejkPaymentRequested
			if err := pub.Messages[requestedAt].Body(&req); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if req.Method != types.PaymentMethodInternalTransfer {
				t.Errorf("method = %q, want the internal-transfer method", req.Method)
			}
			if req.Amount != tc.want {
				t.Errorf("amount = %d, want %d", req.Amount, tc.want)
			}
			if req.OrderForeignKey != tc.orderID || req.OrderType != "order" {
				t.Errorf("linkage = %q/%q, want the order it covers", req.OrderForeignKey, req.OrderType)
			}
			// Provenance: the transfer must still name where the money came from.
			if req.Source == nil || !req.Source.Known() || req.Source.Reference != "AAAAAAAAAAAA" {
				t.Errorf("source = %+v, want the original provider payment", req.Source)
			}

			var got messages.NathejkPaymentReceived
			if err := pub.Messages[receivedAt].Body(&got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Amount != tc.want {
				t.Errorf("received amount = %d, want %d", got.Amount, tc.want)
			}
		})
	}
}

// Provenance is resolved from the paid order that funded most of what is moving,
// deterministically, because a transfer's lines can come from several paid orders
// and a payment names one source.
func TestTransferResolvesProvenanceFromTheFundingOrder(t *testing.T) {
	c, _, src := newTestCommander(shirt("o-small"), seat("o-big"))

	if _, err := c.TransferMember(context.Background(), request()); err != nil {
		t.Fatalf("TransferMember: %v", err)
	}
	if len(src.asked) != 1 || src.asked[0] != "o-big" {
		t.Errorf("provenance asked for %v, want the order contributing the most", src.asked)
	}
}

// The regression this exists to prevent: a transfer must close its own orders.
//
// Leaving it to the payment saga made a transfer depend on another consumer's
// projection being current. The saga is a one-shot reaction to payment.received
// that reads its own projections, and this command publishes in a burst — the
// credit order's payment lands milliseconds after the order was created, so the
// saga would evaluate an order its projector had not written yet, retry for a
// couple of seconds, and abandon it permanently, because nothing ever publishes
// another payment.received for that order. The credit half lost that race
// structurally, being published first: in dev, two of three transfers left the
// sending team's credit order showing open with nothing owed.
//
// So: no cross-consumer dependency at all. Both halves are closed by the command
// that knows they are covered.
func TestTransferClosesBothOfItsOwnOrders(t *testing.T) {
	c, pub, _ := newTestCommander(seat("o-1"), shirt("o-1"))

	res, err := c.TransferMember(context.Background(), request())
	if err != nil {
		t.Fatalf("TransferMember: %v", err)
	}

	for _, tc := range []struct {
		name    string
		orderID string
		want    int
	}{
		{"credit", res.CreditOrderID, -57000},
		{"charge", res.ChargeOrderID, 57000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paidAt := -1
			receivedAt := -1
			for i, subj := range pub.Subjects() {
				switch {
				case subj == "NATHEJK.2026.order."+tc.orderID+".paid":
					paidAt = i
				case strings.HasPrefix(subj, "NATHEJK.2026.payment.") && strings.HasSuffix(subj, ".received"):
					var body messages.NathejkPaymentReceived
					if err := pub.Messages[i].Body(&body); err != nil {
						t.Fatalf("decode: %v", err)
					}
					if body.Amount == tc.want {
						receivedAt = i
					}
				}
			}
			if paidAt < 0 {
				t.Fatalf("order %s was left for the saga to close: %v", tc.orderID, pub.Subjects())
			}
			// The money trail precedes the state change, so a reader of the log
			// never sees an order closed before it was covered.
			if receivedAt < 0 || receivedAt > paidAt {
				t.Errorf("the payment must be published before the order is closed, got received=%d paid=%d", receivedAt, paidAt)
			}
			var paid messages.NathejkOrderPaid
			if err := pub.Messages[paidAt].Body(&paid); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if paid.OrderID != tc.orderID {
				t.Errorf("paid names %q, want %q", paid.OrderID, tc.orderID)
			}
			// The order's own signed total, exactly as the saga would have said it.
			if paid.PaidAmount != tc.want {
				t.Errorf("paidAmount = %d, want the order's own total %d", paid.PaidAmount, tc.want)
			}
		})
	}
}

// Closing its own orders must not make the reassignment stop being last, nor make
// the credit half stop preceding the charge half.
func TestTransferKeepsItsPublishOrder(t *testing.T) {
	c, pub, _ := newTestCommander(seat("o-1"))
	res, err := c.TransferMember(context.Background(), request())
	if err != nil {
		t.Fatalf("TransferMember: %v", err)
	}
	subjects := pub.Subjects()
	want := []string{
		"NATHEJK.2026.order." + res.CreditOrderID + ".created",
		"NATHEJK.2026.order." + res.CreditOrderID + ".lines.changed",
		"NATHEJK.2026.payment." + res.CreditReference + ".requested",
		"NATHEJK.2026.payment." + res.CreditReference + ".received",
		"NATHEJK.2026.order." + res.CreditOrderID + ".paid",
		"NATHEJK.2026.order." + res.ChargeOrderID + ".created",
		"NATHEJK.2026.order." + res.ChargeOrderID + ".lines.changed",
		"NATHEJK.2026.payment." + res.ChargeReference + ".requested",
		"NATHEJK.2026.payment." + res.ChargeReference + ".received",
		"NATHEJK.2026.order." + res.ChargeOrderID + ".paid",
		"NATHEJK.2026.spejder.m-1.reassigned",
	}
	if len(subjects) != len(want) {
		t.Fatalf("published %d events, want %d:\n%v", len(subjects), len(want), subjects)
	}
	for i := range want {
		if subjects[i] != want[i] {
			t.Errorf("event %d = %q, want %q", i, subjects[i], want[i])
		}
	}
}

// A transfer of nothing but zero-priced lines has no meaningful payment: there is
// no money to record. It is still closed the same way — this case was the precedent
// the change above generalised, not an exception to it.
func TestTransferSettlesZeroValueHalvesWithoutAPayment(t *testing.T) {
	free := seat("o-1")
	free.UnitPrice = 0
	free.LineTotal = 0
	c, pub, _ := newTestCommander(free)

	res, err := c.TransferMember(context.Background(), request())
	if err != nil {
		t.Fatalf("TransferMember: %v", err)
	}
	if res.Amount != 0 {
		t.Fatalf("Amount = %d, want 0", res.Amount)
	}
	subjects := strings.Join(pub.Subjects(), "\n")
	if strings.Contains(subjects, ".payment.") {
		t.Errorf("a zero-value transfer must not invent a payment:\n%s", subjects)
	}
	for _, orderID := range []string{res.CreditOrderID, res.ChargeOrderID} {
		if !strings.Contains(subjects, orderID+".paid") {
			t.Errorf("order %s must not be left open:\n%s", orderID, subjects)
		}
	}
}

// Refusals, and the fact that each is its own value: a caller maps them to
// different messages, and "already on that team" must never be confused with a
// transfer that simply had nothing to move.
func TestTransferRefusesOnlyWhatItAloneCanKnow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*commander)
		request Transfer
		want    error
	}{
		{
			name:    "no member id",
			request: Transfer{ToTeamID: "team-to", TeamType: types.TeamTypePatrulje},
			want:    ErrIncompleteTransfer,
		},
		{
			name:    "no destination",
			request: Transfer{MemberID: "m-1", TeamType: types.TeamTypePatrulje},
			want:    ErrIncompleteTransfer,
		},
		{
			name:    "no team type",
			request: Transfer{MemberID: "m-1", ToTeamID: "team-to"},
			want:    ErrIncompleteTransfer,
		},
		{
			name:    "member not on the roster",
			mutate:  func(c *commander) { c.roster = fakeRoster{err: tables.ErrRecordNotFound} },
			request: request(),
			want:    ErrMemberNotFound,
		},
		{
			name:    "already on that team",
			mutate:  func(c *commander) { c.roster = fakeRoster{teamID: "team-to"} },
			request: request(),
			want:    ErrSameTeam,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, pub, _ := newTestCommander(seat("o-1"))
			if tc.mutate != nil {
				tc.mutate(c)
			}
			_, err := c.TransferMember(context.Background(), tc.request)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			// Validated before anything is published.
			if len(pub.Messages) != 0 {
				t.Errorf("a refused transfer must publish nothing, got %v", pub.Subjects())
			}
		})
	}
}

// A member not on the roster still maps to the module-wide not-found sentinel, so a
// caller translating that to a 404 keeps working.
func TestMemberNotFoundWrapsTheModuleSentinel(t *testing.T) {
	if !errors.Is(ErrMemberNotFound, tables.ErrRecordNotFound) {
		t.Error("ErrMemberNotFound should wrap tables.ErrRecordNotFound")
	}
}

// No lower bound on the origin's size: emptying a team out one member at a time is
// required, because a team below three may not start and its members have to be
// transferable to one that has not started.
func TestTransferDoesNotCheckTheOriginsMemberCount(t *testing.T) {
	c, pub, _ := newTestCommander(seat("o-1"))
	// Nothing in the command's dependencies can even count the origin's members —
	// asserted by the fact that it publishes a complete transfer knowing only the
	// member, the teams and the paid lines.
	if _, err := c.TransferMember(context.Background(), request()); err != nil {
		t.Fatalf("TransferMember: %v", err)
	}
	if len(pub.Messages) == 0 {
		t.Error("the transfer should have gone through")
	}
}

// A read failure must reach the caller rather than being papered over: publishing
// half a transfer because a query failed is the outcome to avoid.
func TestTransferReturnsReadErrors(t *testing.T) {
	boom := errors.New("connection refused")
	c, pub, _ := newTestCommander()
	c.lines = &fakeLines{err: boom}

	if _, err := c.TransferMember(context.Background(), request()); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want %v", err, boom)
	}
	if len(pub.Messages) != 0 {
		t.Errorf("nothing may be published after a failed read, got %v", pub.Subjects())
	}
}

// The balance check is a backstop against a future edit, not a reachable state.
func TestBalancedRejectsAPairThatDoesNotCancel(t *testing.T) {
	credit := []messages.NathejkOrder_Line{{LineTotal: -45000}}
	charge := []messages.NathejkOrder_Line{{LineTotal: 46000}}
	if err := balanced(credit, charge); !errors.Is(err, ErrNotBalanced) {
		t.Fatalf("error = %v, want ErrNotBalanced", err)
	}
	if err := balanced(credit, []messages.NathejkOrder_Line{{LineTotal: 45000}}); err != nil {
		t.Fatalf("a balanced pair must pass, got %v", err)
	}
}

// The transfer id is embedded in every line id on both halves, which is how a
// caller finds the two orders of a transfer from either one without a new column.
func TestLineIDsIdentifyTheTransferAndPairTheHalves(t *testing.T) {
	c, pub, _ := newTestCommander(seat("o-1"), shirt("o-1"))
	res, err := c.TransferMember(context.Background(), request())
	if err != nil {
		t.Fatalf("TransferMember: %v", err)
	}
	credit := linesOf(t, pub, res.CreditOrderID)
	charge := linesOf(t, pub, res.ChargeOrderID)

	for i := range charge.Lines {
		if !strings.Contains(charge.Lines[i].LineID, res.TransferID) {
			t.Errorf("line id %q does not name the transfer", charge.Lines[i].LineID)
		}
		// Same id on both halves: line i of the credit is the other side of line i
		// of the charge, so finance can pair them without matching on amounts.
		if credit.Lines[i].LineID != charge.Lines[i].LineID {
			t.Errorf("halves disagree on line %d: %q vs %q", i, credit.Lines[i].LineID, charge.Lines[i].LineID)
		}
	}
}

// The two orders and the two payment references must be four distinct names; a
// collision would make one half overwrite the other.
func TestIdentityNamesAreDistinct(t *testing.T) {
	id := IdentityOf("2026", "m-1", "team-from", "team-to")
	names := []string{id.CreditOrderID, id.ChargeOrderID, id.CreditReference, id.ChargeReference}
	seen := map[string]bool{}
	for _, n := range names {
		if n == "" {
			t.Fatalf("empty name in %+v", id)
		}
		if seen[n] {
			t.Errorf("duplicate name %q in %+v", n, id)
		}
		seen[n] = true
	}
	// A payment reference must be impossible to confuse with a provider one, which
	// is twelve characters of Crockford base32 and nothing else.
	for _, ref := range []string{id.CreditReference, id.ChargeReference} {
		if !strings.HasPrefix(ref, "T-") {
			t.Errorf("reference %q should be marked as a transfer", ref)
		}
	}
	// The year is part of the key: the same move in two seasons is two transfers.
	if IdentityOf("2025", "m-1", "team-from", "team-to").TransferID == id.TransferID {
		t.Error("the same move in another season must be a different transfer")
	}
}
