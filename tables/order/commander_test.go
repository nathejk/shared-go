package order

import "testing"

// TestApplyPaidOffset covers the count-based (seat) billing rule: a team pays
// for a number of units per SKU, and the open order only carries the units
// beyond what's already paid — regardless of which member occupies them.
func TestApplyPaidOffset(t *testing.T) {
	part := func(id string) DesiredLine {
		return DesiredLine{ProductSKU: "participation.patrulje", MemberID: id, Quantity: 1}
	}
	shirt := func(id, size string) DesiredLine {
		return DesiredLine{ProductSKU: "tshirt.adult", MemberID: id, Quantity: 1, Attributes: map[string]any{"size": size}}
	}
	count := func(lines []DesiredLine, sku string) int {
		n := 0
		for _, l := range lines {
			if l.ProductSKU == sku {
				n++
			}
		}
		return n
	}

	tests := []struct {
		name      string
		desired   []DesiredLine
		paid      map[string]int
		wantPart  int
		wantShirt int
	}{
		{
			name:     "no paid units keeps everything",
			desired:  []DesiredLine{part("a"), part("b")},
			paid:     nil,
			wantPart: 2,
		},
		{
			name:     "seat reuse: 4 paid seats, 4 (different) members => 0 participation due",
			desired:  []DesiredLine{part("w"), part("x"), part("y"), part("z")},
			paid:     map[string]int{"participation.patrulje": 4},
			wantPart: 0,
		},
		{
			name:     "N+1: one seat beyond the paid count is charged",
			desired:  []DesiredLine{part("a"), part("b"), part("c"), part("d"), part("e")},
			paid:     map[string]int{"participation.patrulje": 4},
			wantPart: 1,
		},
		{
			name:      "t-shirts are not consumed by paid participation",
			desired:   []DesiredLine{part("a"), shirt("a", "l"), part("b"), shirt("b", "m")},
			paid:      map[string]int{"participation.patrulje": 2},
			wantPart:  0,
			wantShirt: 2,
		},
		{
			name:      "free size change: paid t-shirts drop by count regardless of size",
			desired:   []DesiredLine{shirt("a", "xl"), shirt("b", "s")},
			paid:      map[string]int{"tshirt.adult": 2},
			wantPart:  0,
			wantShirt: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ApplyPaidOffset(tc.desired, tc.paid)
			if c := count(got, "participation.patrulje"); c != tc.wantPart {
				t.Errorf("participation lines: got %d, want %d", c, tc.wantPart)
			}
			if c := count(got, "tshirt.adult"); c != tc.wantShirt {
				t.Errorf("t-shirt lines: got %d, want %d", c, tc.wantShirt)
			}
		})
	}
}
