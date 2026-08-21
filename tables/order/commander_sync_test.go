package order

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jrgensen/cqrs/cqrstest"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/tables/product"
	"github.com/nathejk/shared-go/types"
)

// syncFakeQueries is a Queries backed by one in-memory order and a paid-unit
// map, enough to drive SetDerivedLines and SyncNeeded without a database.
type syncFakeQueries struct {
	order *Order
	paid  map[VariantKey]int
}

func (f *syncFakeQueries) GetByID(context.Context, string) (*Order, error) {
	if f.order == nil {
		return nil, tables.ErrRecordNotFound
	}
	return f.order, nil
}
func (f *syncFakeQueries) PaidQuantityByVariant(context.Context, types.YearSlug, types.TeamType, string) (map[VariantKey]int, error) {
	return f.paid, nil
}

// ShippableByVariant nets this fake's single order, which is what the real query
// does across all of the owner's non-cancelled ones.
func (f *syncFakeQueries) ShippableByVariant(context.Context, types.YearSlug, types.TeamType, string) (map[VariantKey]int, error) {
	net := map[VariantKey]int{}
	for k, n := range f.paid {
		net[k] += n
	}
	if f.order != nil {
		for _, l := range f.order.Lines {
			net[VariantKey{SKU: l.ProductSKU, Size: lineSize(l.Attributes)}] += l.Quantity
		}
	}
	for k, n := range net {
		if n == 0 {
			delete(net, k)
		}
	}
	return net, nil
}
func (f *syncFakeQueries) PaidQuantityBySKU(context.Context, types.YearSlug, types.TeamType, string) (map[string]int, error) {
	bySKU := map[string]int{}
	for k, n := range f.paid {
		bySKU[k.SKU] += n
	}
	return bySKU, nil
}
func (*syncFakeQueries) FindOpenOrder(context.Context, types.YearSlug, types.TeamType, string) (*Order, error) {
	return nil, tables.ErrRecordNotFound
}
func (*syncFakeQueries) ListByOwner(context.Context, types.YearSlug, types.TeamType, string) ([]Order, error) {
	return nil, nil
}
func (*syncFakeQueries) ListByYear(context.Context, types.YearSlug) ([]Order, error) { return nil, nil }
func (*syncFakeQueries) ReservedQuantity(context.Context, types.YearSlug, string) (int, error) {
	return 0, nil
}

// fakeCatalogue answers with an unlimited adult t-shirt whose sizes are the
// catalogue order the reclaim step follows.
type fakeCatalogue struct{ stock *int }

func (f fakeCatalogue) GetBySKU(_ context.Context, _ types.YearSlug, sku string) (*product.Product, error) {
	if sku != "tshirt.adult" {
		return nil, tables.ErrRecordNotFound
	}
	return &product.Product{
		SKU:       "tshirt.adult",
		Name:      "T-shirt",
		Kind:      product.KindMerchandise,
		UnitPrice: 17500,
		Sizes:     []string{"s", "m", "l", "xl", "xxl", "3xl"},
		Stock:     f.stock,
		Active:    true,
	}, nil
}
func (fakeCatalogue) ListEligibleFor(context.Context, types.YearSlug, types.TeamType) ([]product.Product, error) {
	return nil, nil
}

func newSyncCommander(o *Order, paid map[VariantKey]int) (*commander, *syncFakeQueries, *cqrstest.Publisher) {
	pub := &cqrstest.Publisher{}
	q := &syncFakeQueries{order: o, paid: paid}
	return &commander{p: pub, q: q, products: fakeCatalogue{}, year: "2026"}, q, pub
}

func openOrder() *Order {
	return &Order{OrderID: "order-1", Year: "2026", OwnerType: types.TeamTypeKlan, OwnerID: "team-1", Status: StatusOpen}
}

// The reported case, end to end: a member with a paid xxl asks for a 3xl. The
// open order must state the swap and charge nothing.
func TestSetDerivedLinesRecordsAFreeSizeChange(t *testing.T) {
	o := openOrder()
	c, _, pub := newSyncCommander(o, paidShirts("xxl"))

	got, err := c.SetDerivedLines(context.Background(), "order-1", []DesiredLine{shirt("m-1", "3xl")})
	if err != nil {
		t.Fatalf("SetDerivedLines: %v", err)
	}
	if len(got.Lines) != 2 {
		t.Fatalf("want the zero-sum pair, got %d lines: %+v", len(got.Lines), got.Lines)
	}
	if got.TotalAmount != 0 {
		t.Errorf("TotalAmount = %d, want 0 — a size change on a paid unit is free", got.TotalAmount)
	}
	if got.DueAmount != 0 {
		t.Errorf("DueAmount = %d, want 0, so no payment link is minted", got.DueAmount)
	}

	var charged, credited *Line
	for i := range got.Lines {
		switch got.Lines[i].Quantity {
		case 1:
			charged = &got.Lines[i]
		case -1:
			credited = &got.Lines[i]
		}
	}
	if charged == nil || credited == nil {
		t.Fatalf("want one +1 and one -1 line, got %+v", got.Lines)
	}
	if lineSize(charged.Attributes) != "3xl" || charged.LineTotal != 17500 {
		t.Errorf("charge line = %+v, want +1 3xl at 17500", charged)
	}
	if lineSize(credited.Attributes) != "xxl" || credited.LineTotal != -17500 {
		t.Errorf("credit line = %+v, want -1 xxl at -17500", credited)
	}
	if len(pub.Messages) != 1 {
		t.Fatalf("want one lines.changed event, got %d", len(pub.Messages))
	}
	var body messages.NathejkOrderLinesChanged
	if err := pub.Messages[0].Body(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.TotalAmount != 0 {
		t.Errorf("published TotalAmount = %d, want 0", body.TotalAmount)
	}
}

// The PRD's headline risk: if the read path and the write path disagree about the
// target, every page load republishes the lines. SyncNeeded must report false for
// an order SetDerivedLines just wrote.
func TestSyncNeededIsFalseAfterSetDerivedLines(t *testing.T) {
	o := openOrder()
	c, q, pub := newSyncCommander(o, paidShirts("xxl"))
	desired := []DesiredLine{shirt("m-1", "3xl")}

	written, err := c.SetDerivedLines(context.Background(), "order-1", desired)
	if err != nil {
		t.Fatalf("SetDerivedLines: %v", err)
	}
	// The projection has caught up: the order now holds what was published.
	q.order = written
	pub.Reset()

	need, err := c.SyncNeeded(context.Background(), written, desired)
	if err != nil {
		t.Fatalf("SyncNeeded: %v", err)
	}
	if need {
		t.Fatal("SyncNeeded must be false for lines that were just written, or every GET republishes them")
	}

	// And the write path itself must be idempotent for the same input.
	again, err := c.SetDerivedLines(context.Background(), "order-1", desired)
	if err != nil {
		t.Fatalf("second SetDerivedLines: %v", err)
	}
	if len(again.Lines) != len(written.Lines) || again.TotalAmount != written.TotalAmount {
		t.Errorf("second call changed the order: %+v vs %+v", again.Lines, written.Lines)
	}
}

// A credit is not interchangeable with a charge: an order holding -1 xxl where
// +1 xxl is wanted must be reported out of sync, which is why quantity is part of
// the comparison key.
func TestSyncNeededDistinguishesCreditFromCharge(t *testing.T) {
	o := openOrder()
	o.Lines = []Line{{
		LineID: "derived:tshirt.adult:m-1:xxl", ProductSKU: "tshirt.adult", MemberID: "m-1",
		UnitPrice: 17500, Quantity: -1, LineTotal: -17500,
		Origin: string(messages.LineOriginDerived), Attributes: map[string]any{"size": "xxl"},
	}}
	c, _, _ := newSyncCommander(o, nil)

	need, err := c.SyncNeeded(context.Background(), o, []DesiredLine{shirt("m-1", "xxl")})
	if err != nil {
		t.Fatalf("SyncNeeded: %v", err)
	}
	if !need {
		t.Error("a -1 line must not satisfy a +1 desired line")
	}
}

// A credit must not buy stock headroom for the size replacing it: with one shirt
// in stock and one already reserved elsewhere, a size change nets to zero units
// and passes, but adding a second shirt does not.
func TestCheckStockIgnoresCreditLines(t *testing.T) {
	one := 1
	o := openOrder()
	c, _, _ := newSyncCommander(o, paidShirts("xxl"))
	c.products = fakeCatalogue{stock: &one}

	if _, err := c.SetDerivedLines(context.Background(), "order-1", []DesiredLine{shirt("m-1", "3xl")}); err != nil {
		t.Fatalf("a size change should fit in stock: %v", err)
	}
	// Two uncovered shirts against one in stock: the credit for the swap must
	// not make room for the genuinely new one.
	_, err := c.SetDerivedLines(context.Background(), "order-1", []DesiredLine{shirt("m-1", "3xl"), shirt("m-2", "l"), shirt("m-3", "m")})
	if err == nil {
		t.Error("want ErrOutOfStock: a credit must not create stock headroom")
	}
}

// The order is authoritative for size, so the net variant mix is the answer to
// "which shirts does this owner get". After a free size change that must be the
// new size only — the old one must not still be packed.
func TestShippableNetsTheSizeChange(t *testing.T) {
	o := openOrder()
	c, q, _ := newSyncCommander(o, paidShirts("xxl"))

	written, err := c.SetDerivedLines(context.Background(), "order-1", []DesiredLine{shirt("m-1", "3xl")})
	if err != nil {
		t.Fatalf("SetDerivedLines: %v", err)
	}
	q.order = written

	net, err := q.ShippableByVariant(context.Background(), "2026", types.TeamTypeKlan, "team-1")
	if err != nil {
		t.Fatalf("ShippableByVariant: %v", err)
	}
	want := map[VariantKey]int{{SKU: "tshirt.adult", Size: "3xl"}: 1}
	if len(net) != len(want) {
		t.Fatalf("net = %v, want %v", net, want)
	}
	for k, n := range want {
		if net[k] != n {
			t.Errorf("net[%+v] = %d, want %d", k, net[k], n)
		}
	}
	if _, stillThere := net[VariantKey{SKU: "tshirt.adult", Size: "xxl"}]; stillThere {
		t.Error("the reclaimed xxl must net out, or fulfillment packs both shirts")
	}
}

// An order recording only a free size change owes nothing, so no payment will
// ever arrive to close it and the saga will never fire. Settle is how it stops
// being mutable.
func TestSettleFreezesAFreeExchange(t *testing.T) {
	o := openOrder()
	c, q, pub := newSyncCommander(o, paidShirts("xxl"))

	written, err := c.SetDerivedLines(context.Background(), "order-1", []DesiredLine{shirt("m-1", "3xl")})
	if err != nil {
		t.Fatalf("SetDerivedLines: %v", err)
	}
	q.order = written
	pub.Reset()

	settled, err := c.Settle(context.Background(), "order-1")
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if settled.Status != StatusPaid {
		t.Errorf("Status = %q, want %q", settled.Status, StatusPaid)
	}
	if len(pub.Messages) != 1 {
		t.Fatalf("want one order.paid event, got %d", len(pub.Messages))
	}
	if !pub.Messages[0].Subject().Match("NATHEJK.2026.order.*.paid") {
		t.Errorf("unexpected subject %q", pub.Subjects()[0])
	}
	var body messages.NathejkOrderPaid
	if err := pub.Messages[0].Body(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.PaidAmount != 0 {
		t.Errorf("PaidAmount = %d, want 0 — nothing was paid", body.PaidAmount)
	}
}

func TestSettleRefusesWhatItMustNotFreeze(t *testing.T) {
	shirtLine := func(size string, qty int) Line {
		return Line{
			LineID: "derived:tshirt.adult:m-1:" + size, ProductSKU: "tshirt.adult", MemberID: "m-1",
			UnitPrice: 17500, Quantity: qty, LineTotal: 17500 * qty,
			Origin: string(messages.LineOriginDerived), Attributes: map[string]any{"size": size},
		}
	}

	t.Run("an order that owes money", func(t *testing.T) {
		o := openOrder()
		o.Lines = []Line{shirtLine("l", 1)}
		o.TotalAmount = 17500
		c, _, pub := newSyncCommander(o, nil)

		if _, err := c.Settle(context.Background(), "order-1"); !errors.Is(err, ErrOrderNotFree) {
			t.Errorf("err = %v, want ErrOrderNotFree: settling is not a way to get goods for free", err)
		}
		if len(pub.Messages) != 0 {
			t.Errorf("nothing should be published, got %v", pub.Subjects())
		}
	})

	t.Run("an empty order", func(t *testing.T) {
		o := openOrder()
		c, _, pub := newSyncCommander(o, nil)

		if _, err := c.Settle(context.Background(), "order-1"); !errors.Is(err, ErrEmptyOrder) {
			t.Errorf("err = %v, want ErrEmptyOrder", err)
		}
		if len(pub.Messages) != 0 {
			t.Errorf("nothing should be published, got %v", pub.Subjects())
		}
	})
}

// A repeated save must not fail, so settling an order that is no longer open is a
// no-op rather than an error.
func TestSettleIsIdempotent(t *testing.T) {
	o := openOrder()
	o.Status = StatusPaid
	c, _, pub := newSyncCommander(o, nil)

	got, err := c.Settle(context.Background(), "order-1")
	if err != nil {
		t.Fatalf("Settle on a settled order should be a no-op, got %v", err)
	}
	if got.Status != StatusPaid {
		t.Errorf("Status = %q, want it left alone", got.Status)
	}
	if len(pub.Messages) != 0 {
		t.Errorf("nothing should be republished, got %v", pub.Subjects())
	}
}

// The property §8.1b calls load-bearing: once a settled exchange counts as paid,
// the paid count must include the credit, so the returned size nets to zero and
// the new size becomes the paid one. A positive-only paid count would leave the
// old size on the books and hand the owner a free second shirt on their next
// change.
func TestSettledExchangeNetsIntoThePaidCount(t *testing.T) {
	// Paid: one xxl. Settled: +1 3xl, -1 xxl. Summed raw, as
	// PaidQuantityByVariant does.
	paid := map[VariantKey]int{
		{SKU: "tshirt.adult", Size: "xxl"}: 1, // the original paid order
	}
	for _, l := range []struct {
		size string
		qty  int
	}{{"3xl", 1}, {"xxl", -1}} { // the settled exchange
		paid[VariantKey{SKU: "tshirt.adult", Size: l.size}] += l.qty
	}
	if got := paid[VariantKey{SKU: "tshirt.adult", Size: "xxl"}]; got != 0 {
		t.Fatalf("paid xxl = %d, want 0 — the returned size must net out", got)
	}
	if got := paid[VariantKey{SKU: "tshirt.adult", Size: "3xl"}]; got != 1 {
		t.Fatalf("paid 3xl = %d, want 1", got)
	}

	// A further change to l therefore reclaims the 3xl, not the xxl, and stays
	// free.
	got := summary(ApplyPaidOffset([]DesiredLine{shirt("m-1", "l")}, paid, adultSizes))
	want := []string{"tshirt.adult:l:+1", "tshirt.adult:3xl:-1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v — the second change must reclaim the size actually held", got, want)
	}
}

// A free crew signup settles too. participation.crew is priced at 0 because crew
// are volunteers, so a crew order is complete the moment it is derived: nothing is
// outstanding and no payment will arrive. Settle keys on the order's value, which
// is why this works without a special case — and this test exists because an
// earlier comment claimed only zero-sum exchange pairs could total zero.
func TestSettleFreesignupOrder(t *testing.T) {
	o := openOrder()
	o.OwnerType = types.TeamTypeCrew
	o.Lines = []Line{{
		LineID: "derived:participation.crew:m-1", ProductSKU: "participation.crew", MemberID: "m-1",
		UnitPrice: 0, Quantity: 1, LineTotal: 0,
		Origin: string(messages.LineOriginDerived),
	}}
	o.TotalAmount = 0
	c, _, pub := newSyncCommander(o, nil)

	settled, err := c.Settle(context.Background(), "order-1")
	if err != nil {
		t.Fatalf("a free crew order should settle: %v", err)
	}
	if settled.Status != StatusPaid {
		t.Errorf("Status = %q, want %q", settled.Status, StatusPaid)
	}
	if len(pub.Messages) != 1 {
		t.Fatalf("want one order.paid event, got %d", len(pub.Messages))
	}
}
