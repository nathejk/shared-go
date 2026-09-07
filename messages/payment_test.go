package messages_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/types"
)

// Typing Method must be invisible on the wire.
//
// The field went from string to types.PaymentMethod so a typo is a compile error
// rather than a second spelling in the method column. That is only safe if the
// encoding is byte-identical: every payment.requested event ever published is
// still on the stream and gets replayed on every start, and the projector writes
// method verbatim into a table whose 1189 existing rows say "mobilepay".
func TestPaymentRequestedMethodIsEncodedAsAPlainString(t *testing.T) {
	b, err := json.Marshal(messages.NathejkPaymentRequested{
		Reference: "abc123",
		Method:    types.PaymentMethodMobilePay,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(b); !strings.Contains(got, `"method":"mobilepay"`) {
		t.Errorf("encoded as %s, want a plain lowercase \"method\":\"mobilepay\"", got)
	}
}

// A historical event must still decode, whatever its method says.
//
// The three cases are the three shapes actually on the stream: the method we
// know, no method field at all (events published before the field existed), and
// a value this build does not recognise (an older or newer producer). None may
// error — a payment that fails to decode is a payment that stops being projected,
// which is a worse outcome than an unrecognised string in a column.
func TestPaymentRequestedDecodesAnyMethod(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want types.PaymentMethod
	}{
		{"known", `{"reference":"a","method":"mobilepay"}`, types.PaymentMethodMobilePay},
		{"absent", `{"reference":"a"}`, types.PaymentMethodNone},
		{"unknown to this build", `{"reference":"a","method":"swish"}`, types.PaymentMethod("swish")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body messages.NathejkPaymentRequested
			if err := json.Unmarshal([]byte(tc.raw), &body); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if body.Method != tc.want {
				t.Errorf("Method = %q, want %q", body.Method, tc.want)
			}
			// Round-trip: re-encoding must not rewrite what arrived.
			out, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var again messages.NathejkPaymentRequested
			if err := json.Unmarshal(out, &again); err != nil {
				t.Fatalf("re-unmarshal: %v", err)
			}
			if again.Method != tc.want {
				t.Errorf("after round-trip Method = %q, want %q", again.Method, tc.want)
			}
		})
	}
}
