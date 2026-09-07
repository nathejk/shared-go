package types

type Currency string

const (
	CurrencyDKK Currency = "DKK"
	CurrencyNOK Currency = "NOK"
	CurrencyEUR Currency = "EUR"
)

type PaymentStatus string

const (
	PaymentStatusRequested PaymentStatus = "requested"
	PaymentStatusReserved  PaymentStatus = "reserved"
	PaymentStatusReceived  PaymentStatus = "received"
	PaymentStatusRejected  PaymentStatus = "rejected"
	PaymentStatusTimedout  PaymentStatus = "timedout"
)

// PaymentMethod is how the money behind a payment moved.
//
// String-backed and lowercase because these values are already on the wire: they
// travel on NathejkPaymentRequested.Method and land verbatim in the payment
// table's method column. Changing an existing value would orphan every row and
// every replayed event carrying the old spelling, so treat the constants below as
// append-only.
//
// A payment's method is written exactly once, by the requested event — the
// reserved and received projections are UPDATEs keyed on reference and never touch
// it — so it describes how the payment was set up rather than its current state.
type PaymentMethod string

const (
	// PaymentMethodNone is the zero value: a payment whose method was never
	// recorded. Not something to publish; it is what a legacy row or an event
	// predating the field decodes to.
	PaymentMethodNone PaymentMethod = ""

	// PaymentMethodMobilePay is a MobilePay transaction: real money arriving
	// from outside through the payment provider. Until internal transfers
	// existed this was the only method, and every historical row holds it.
	PaymentMethodMobilePay PaymentMethod = "mobilepay"

	// PaymentMethodInternalTransfer is money that a provider payment already
	// brought in, being re-attributed from one order to another — for instance
	// when a participant's paid seat moves to a different team before the race.
	// Nothing is charged and nothing is refunded; the funds are the same funds,
	// pointed somewhere else.
	//
	// It is the one method with no provider behind it. An internal transfer must
	// never be handed to a payment provider: there is no transaction to create,
	// authorise or capture, and doing so would take money a second time. It is
	// also not a refund — nothing leaves the system — which is why it is a
	// method rather than a status.
	//
	// Two consequences for whoever publishes one:
	//
	//   - It must reach reserved or received to count. Every "has this been
	//     paid" computation, including the order read model's paidAmount
	//     subquery, filters status IN ('reserved','received'); a transfer left at
	//     requested is invisible to all of them.
	//   - The money it moves came from somewhere, and that somewhere should stay
	//     nameable. A transfer whose origin is not recorded is an audit trail
	//     that dead-ends at exactly the record someone will ask about.
	PaymentMethodInternalTransfer PaymentMethod = "internal-transfer"
)

// Valid reports whether m is a method this version of the code knows.
//
// PaymentMethodNone is deliberately excluded, as with MemberStatus: an unset
// method is readable (769 legacy rows and any event predating the field) but not a
// valid thing to publish.
//
// Note that an unknown method is not an error anywhere in the pipeline — the
// projector stores whatever string arrives — so this is for callers that want to
// check, not a decoding gate.
func (m PaymentMethod) Valid() bool {
	switch m {
	case PaymentMethodMobilePay, PaymentMethodInternalTransfer:
		return true
	}
	return false
}

func (m PaymentMethod) String() string { return string(m) }
