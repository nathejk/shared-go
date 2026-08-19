package order

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// adultSizes is the catalogue order for tshirt.adult, which decides which paid
// size gets reclaimed when several could be.
var adultSizes = map[string][]string{
	"tshirt.adult": {"s", "m", "l", "xl", "xxl", "3xl"},
}

func part(id string) DesiredLine {
	return DesiredLine{ProductSKU: "participation.patrulje", MemberID: id, Quantity: 1}
}

func shirt(id, size string) DesiredLine {
	return DesiredLine{ProductSKU: "tshirt.adult", MemberID: id, Quantity: 1, Attributes: map[string]any{"size": size}}
}

func paidShirts(sizes ...string) map[VariantKey]int {
	m := map[VariantKey]int{}
	for _, s := range sizes {
		m[VariantKey{SKU: "tshirt.adult", Size: s}]++
	}
	return m
}

// summary renders a line set as "sku:size:qty" strings so a test can state the
// expected outcome, in order, without asserting on lineIds or prices.
func summary(lines []DesiredLine) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, fmt.Sprintf("%s:%s:%+d", l.ProductSKU, lineSize(l.Attributes), l.Quantity))
	}
	return out
}

// TestApplyPaidOffset covers the count-based (seat) billing rule — a team pays
// for a number of units per variant, and the open order carries only what is
// beyond that — and the size-aware half: a size change on an already-paid unit
// costs nothing but is stated as a zero-sum pair, so the order says what has to
// be shipped. Scenarios follow PRD 002 §5.
func TestApplyPaidOffset(t *testing.T) {
	tests := []struct {
		name    string
		desired []DesiredLine
		paid    map[VariantKey]int
		want    []string
	}{
		{
			name:    "no paid units keeps everything",
			desired: []DesiredLine{part("a"), part("b")},
			paid:    nil,
			want:    []string{"participation.patrulje::+1", "participation.patrulje::+1"},
		},
		{
			name:    "seat reuse: 4 paid seats, 4 different members, nothing due",
			desired: []DesiredLine{part("w"), part("x"), part("y"), part("z")},
			paid:    map[VariantKey]int{{SKU: "participation.patrulje"}: 4},
			want:    []string{},
		},
		{
			name:    "N+1: one seat beyond the paid count is charged",
			desired: []DesiredLine{part("a"), part("b"), part("c"), part("d"), part("e")},
			paid:    map[VariantKey]int{{SKU: "participation.patrulje"}: 4},
			want:    []string{"participation.patrulje::+1"},
		},
		{
			name:    "t-shirts are not consumed by paid participation",
			desired: []DesiredLine{part("a"), shirt("a", "l"), part("b"), shirt("b", "m")},
			paid:    map[VariantKey]int{{SKU: "participation.patrulje"}: 2},
			want:    []string{"tshirt.adult:l:+1", "tshirt.adult:m:+1"},
		},
		{
			name:    "same size stays absent: a paid shirt in the size wanted is not re-stated",
			desired: []DesiredLine{shirt("a", "xl"), shirt("b", "s")},
			paid:    paidShirts("xl", "s"),
			want:    []string{},
		},
		{
			// The reported case: xxl paid, 3xl wanted. Free, and visible.
			name:    "size change emits a zero-sum pair",
			desired: []DesiredLine{shirt("a", "3xl")},
			paid:    paidShirts("xxl"),
			want:    []string{"tshirt.adult:3xl:+1", "tshirt.adult:xxl:-1"},
		},
		{
			name:    "team partial change: three unchanged shirts stay absent",
			desired: []DesiredLine{shirt("a", "l"), shirt("b", "l"), shirt("c", "xl"), shirt("d", "m")},
			paid:    paidShirts("l", "l", "xl", "xl"),
			want:    []string{"tshirt.adult:m:+1", "tshirt.adult:xl:-1"},
		},
		{
			// The credit must attach to the changed unit, not to the new one.
			name:    "change plus growth: one pair, and one shirt at full price",
			desired: []DesiredLine{shirt("a", "l"), shirt("b", "l"), shirt("c", "xl"), shirt("d", "m"), shirt("e", "l")},
			paid:    paidShirts("l", "l", "xl", "xl"),
			want:    []string{"tshirt.adult:m:+1", "tshirt.adult:xl:-1", "tshirt.adult:l:+1"},
		},
		{
			name:    "shrinking below the paid count refunds nothing",
			desired: []DesiredLine{shirt("a", "l"), shirt("b", "l"), shirt("c", "l")},
			paid:    paidShirts("l", "l", "l", "l"),
			want:    []string{},
		},
		{
			name:    "unpaid order is untouched",
			desired: []DesiredLine{shirt("a", "l")},
			paid:    nil,
			want:    []string{"tshirt.adult:l:+1"},
		},
		{
			// A sizeless SKU has nothing to swap, so an uncovered seat is a
			// genuinely new seat and must never produce a credit.
			name:    "sizeless SKUs never produce credits",
			desired: []DesiredLine{part("a"), part("b")},
			paid:    map[VariantKey]int{{SKU: "participation.patrulje"}: 1},
			want:    []string{"participation.patrulje::+1"},
		},
		{
			// Casing must not read as a different size, or the offset invents a
			// change nobody made.
			name:    "sizes match regardless of case and padding",
			desired: []DesiredLine{{ProductSKU: "tshirt.adult", MemberID: "a", Quantity: 1, Attributes: map[string]any{"size": " XL "}}},
			paid:    paidShirts("xl"),
			want:    []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := summary(ApplyPaidOffset(tc.desired, tc.paid, adultSizes))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// Repeated changes must never accumulate stale credits: the result is a pure
// function of (desired, paid), so only the current desired size matters.
func TestApplyPaidOffsetRepeatedChanges(t *testing.T) {
	paid := paidShirts("xxl")

	// xxl -> 3xl -> l leaves exactly one pair, crediting the still-paid xxl.
	got := summary(ApplyPaidOffset([]DesiredLine{shirt("a", "l")}, paid, adultSizes))
	want := []string{"tshirt.adult:l:+1", "tshirt.adult:xxl:-1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after two changes: got %v, want %v", got, want)
	}

	// Changing back to the paid size collapses to nothing at all.
	got = summary(ApplyPaidOffset([]DesiredLine{shirt("a", "xxl")}, paid, adultSizes))
	if len(got) != 0 {
		t.Errorf("changing back should leave no lines, got %v", got)
	}
}

// The reclaimed size is chosen in catalogue order, not by slug: crediting the xl
// before the 3xl is what a human reading the lines expects, whereas string order
// would credit "3xl" first because "3" < "x".
func TestApplyPaidOffsetReclaimsInCatalogueOrder(t *testing.T) {
	got := summary(ApplyPaidOffset([]DesiredLine{shirt("a", "m")}, paidShirts("xl", "3xl"), adultSizes))
	want := []string{"tshirt.adult:m:+1", "tshirt.adult:xl:-1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// A size retired from the catalogue still has to be reclaimable, and has to sort
// somewhere fixed, or the ordering is not total and the output not deterministic.
func TestApplyPaidOffsetReclaimsRetiredSizesLast(t *testing.T) {
	got := summary(ApplyPaidOffset([]DesiredLine{shirt("a", "m")}, paidShirts("xl", "xxs"), adultSizes))
	want := []string{"tshirt.adult:m:+1", "tshirt.adult:xl:-1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("catalogue sizes should be reclaimed first: got %v, want %v", got, want)
	}

	// With only retired sizes paid, one is still reclaimed, by slug.
	got = summary(ApplyPaidOffset([]DesiredLine{shirt("a", "m")}, paidShirts("zzz", "xxs"), adultSizes))
	want = []string{"tshirt.adult:m:+1", "tshirt.adult:xxs:-1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Determinism is a hard requirement, not a nicety: the sync check compares this
// output against the projection, so a different-but-equivalent result on each
// call would republish the order's lines on every page load.
func TestApplyPaidOffsetIsDeterministic(t *testing.T) {
	desired := []DesiredLine{shirt("a", "m"), shirt("b", "s"), shirt("c", "3xl"), part("a")}

	first := summary(ApplyPaidOffset(desired, paidShirts("xl", "xxl", "l"), adultSizes))
	for i := 0; i < 200; i++ {
		// Rebuild the paid map each time: Go randomises map iteration order, so
		// re-inserting in a shuffled order is what would expose an algorithm
		// that iterates a map instead of a sorted key list.
		sizes := []string{"xl", "xxl", "l"}
		rand.Shuffle(len(sizes), func(i, j int) { sizes[i], sizes[j] = sizes[j], sizes[i] })
		got := summary(ApplyPaidOffset(desired, paidShirts(sizes...), adultSizes))
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differed:\n got %v\nwant %v", i, got, first)
		}
	}
	// And the pairs must genuinely be zero-sum in count terms.
	for _, s := range first {
		if strings.HasSuffix(s, ":-1") {
			return
		}
	}
	t.Errorf("expected at least one credit line in %v", first)
}

// The credit borrows the changed member's id, because buildLines requires one and
// "which member's change caused this" is the useful answer.
func TestCreditLineCarriesMemberIDAndSize(t *testing.T) {
	got := ApplyPaidOffset([]DesiredLine{shirt("member-1", "3xl")}, paidShirts("xxl"), adultSizes)
	if len(got) != 2 {
		t.Fatalf("want a pair, got %v", summary(got))
	}
	credit := got[1]
	if credit.MemberID != "member-1" {
		t.Errorf("credit MemberID = %q, want the changed member's", credit.MemberID)
	}
	if credit.Quantity != -1 {
		t.Errorf("credit Quantity = %d, want -1", credit.Quantity)
	}
	if lineSize(credit.Attributes) != "xxl" {
		t.Errorf("credit size = %q, want the reclaimed xxl", lineSize(credit.Attributes))
	}
	// Mutating the credit's attributes must not reach back into the desired
	// line's map, which the caller still holds.
	credit.Attributes["size"] = "tampered"
	if lineSize(got[0].Attributes) != "3xl" {
		t.Errorf("the charge line's size was mutated to %q", lineSize(got[0].Attributes))
	}
}

// A credit and its charge share a SKU and a member, so without the size in the
// lineId they would collide and the pair would collapse to one line.
func TestDefaultLineIDSeparatesSizes(t *testing.T) {
	charge := defaultLineID("derived", shirt("m-1", "3xl"))
	credit := defaultLineID("derived", DesiredLine{ProductSKU: "tshirt.adult", MemberID: "m-1", Quantity: -1, Attributes: map[string]any{"size": "xxl"}})
	if charge == credit {
		t.Fatalf("charge and credit share lineId %q", charge)
	}
	if want := "derived:tshirt.adult:m-1:3xl"; charge != want {
		t.Errorf("charge lineId = %q, want %q", charge, want)
	}
	// Sizeless SKUs keep the form they have today, so participation lineIds are
	// unchanged by this.
	if got, want := defaultLineID("derived", part("m-1")), "derived:participation.patrulje:m-1"; got != want {
		t.Errorf("sizeless lineId = %q, want %q", got, want)
	}
}
