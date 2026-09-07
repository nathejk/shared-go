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

// PaymentSourceUnknown is the Reference of a transfer whose source payment could
// not be identified: "we looked and could not tell", which is information, as
// opposed to a zero value, which is not.
//
// Safe as a sentinel because it cannot collide with a real reference: references
// are twelve characters of Crockford base32 (uppercase and digits only), so no
// lowercase seven-character string is reachable. It is spelled out rather than
// encoded as a separate boolean so the distinction survives into the column and
// into any query written against it.
const PaymentSourceUnknown = "unknown"

// PaymentSource records where the money behind a payment originally came from.
//
// It exists because an internal transfer (see PaymentMethodInternalTransfer) does
// not bring money in — it re-attributes money a provider payment already brought
// in. Without this, a transferred payment's audit trail dead-ends: you can see
// that an order was covered by a transfer and nothing more, so "who paid for
// this?" becomes unanswerable for exactly the records somebody is most likely to
// ask about.
//
// Three states, and they must stay distinguishable:
//
//   - **Not a transfer.** No source at all (a nil *PaymentSource on the event, an
//     empty column). A provider payment has no provenance because it *is* the
//     provenance.
//   - **Known.** Reference names the root provider payment.
//   - **Unknown.** Reference is PaymentSourceUnknown. Identifying the source often
//     fails and that is the normal case, not an edge case: payment.orderForeignKey
//     is polymorphic and most historical payments are not reachable from an order
//     at all. An unresolvable source must never prevent a transfer from being
//     recorded, so this state is a first-class outcome rather than an error.
//
// Reference is the **root**, not one hop back. Money can move more than once
// (A → B → C), and a transfer whose own source was a transfer inherits the
// original provider payment's reference rather than naming its predecessor —
// otherwise provenance degrades with every move and answering a simple question
// means walking an unbounded chain backwards. Via records the immediate
// predecessor as well, which is additional information; losing the root is not an
// option.
//
// It is deliberately not a foreign key with referential intent. The root may be
// from a previous season or from the 769 legacy rows, and nothing should refuse to
// record a transfer because a lookup failed. It is a reference for humans and
// reports.
//
// The remaining fields are a snapshot taken when the transfer is created, so a
// display does not have to re-resolve a chain that may cross seasons. There is no
// owner *name*: names live in the team read models, which are not this package's
// to read and which a caller can join for a current one, while the id is the part
// that stays true. For many legacy payments the owner is the only thing knowable
// at all, which is why it is recorded next to the reference rather than derived
// from it.
type PaymentSource struct {
	// Reference is the root provider payment, or PaymentSourceUnknown.
	Reference string `json:"reference"`

	// Via is the immediate predecessor when the money has moved more than once,
	// empty when this transfer takes it straight from the root.
	Via string `json:"via,omitempty"`

	OwnerType TeamType      `json:"ownerType,omitempty"`
	OwnerID   string        `json:"ownerId,omitempty"`
	Method    PaymentMethod `json:"method,omitempty"`

	// PaidAt is when the root payment was recorded, as the projection stores its
	// timestamps (a string), so provenance can be read without a second lookup.
	PaidAt string `json:"paidAt,omitempty"`
}

// UnknownPaymentSource is the provenance of a transfer whose source could not be
// identified. Non-nil on purpose: "this is a transfer, and we could not tell where
// the money came from" is a different fact from "this is not a transfer".
func UnknownPaymentSource() *PaymentSource {
	return &PaymentSource{Reference: PaymentSourceUnknown}
}

// Known reports whether the source payment was actually identified.
func (s PaymentSource) Known() bool {
	return s.Reference != "" && s.Reference != PaymentSourceUnknown
}

// RootReference is the value to record in an indexed column and to query on: the
// root payment's reference, or PaymentSourceUnknown when it could not be
// identified.
//
// Normalising here means a producer that leaves Reference empty still ends up
// stored as explicitly unknown rather than as "not a transfer", so the three
// states cannot collapse into two by omission.
func (s PaymentSource) RootReference() string {
	if s.Reference == "" {
		return PaymentSourceUnknown
	}
	return s.Reference
}
