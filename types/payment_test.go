package types_test

import (
	"testing"

	"github.com/nathejk/shared-go/types"
)

// The wire values are the contract, not an implementation detail: they are in the
// event log and in the method column of every existing payment row. Asserting the
// literals means a rename shows up here rather than as rows nobody can group.
func TestPaymentMethodWireValues(t *testing.T) {
	for _, tc := range []struct {
		method types.PaymentMethod
		want   string
	}{
		{types.PaymentMethodMobilePay, "mobilepay"},
		{types.PaymentMethodInternalTransfer, "internal-transfer"},
		{types.PaymentMethodNone, ""},
	} {
		if got := string(tc.method); got != tc.want {
			t.Errorf("method = %q, want %q", got, tc.want)
		}
	}
}

// Valid() draws the line where MemberStatus.Valid() draws it: the unset value is
// readable but not publishable.
func TestPaymentMethodValid(t *testing.T) {
	for _, tc := range []struct {
		method types.PaymentMethod
		want   bool
	}{
		{types.PaymentMethodMobilePay, true},
		{types.PaymentMethodInternalTransfer, true},
		{types.PaymentMethodNone, false},
		{types.PaymentMethod("MobilePay"), false},
		{types.PaymentMethod("internal_transfer"), false},
	} {
		if got := tc.method.Valid(); got != tc.want {
			t.Errorf("PaymentMethod(%q).Valid() = %v, want %v", tc.method, got, tc.want)
		}
	}
}
