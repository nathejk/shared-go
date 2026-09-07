package messages

import (
	"time"

	"github.com/nathejk/shared-go/types"
)

type NathejkPayment_OrderLine struct {
	UnitCount int    `json:"unitCount"`
	UnitPrice int    `json:"unitPrice"`
	Amount    int    `json:"amount"`
	Label     string `json:"label"`
}

type NathejkPaymentRequested struct {
	Reference       string                     `json:"reference"`
	ReturnUrl       string                     `json:"returnUrl"`
	ReceiptEmail    types.EmailAddress         `json:"receiptEmail"`
	Amount          int                        `json:"amount"`
	Currency        string                     `json:"currency"`
	Timestamp       time.Time                  `json:"timestamp"`
	Method          types.PaymentMethod        `json:"method"`
	OrderLines      []NathejkPayment_OrderLine `json:"orderLines,omitempty"`
	OrderForeignKey string                     `json:"orderForeignKey,omitempty"`
	OrderType       string                     `json:"orderType,omitempty"`

	// Source is where the money behind this payment originally came from, set
	// only for an internal transfer — a provider payment has no provenance
	// because it *is* the provenance.
	//
	// A pointer so absent, which is what every event published before this field
	// existed decodes to, means "not a transfer" and cannot be confused with a
	// transfer whose source could not be identified
	// (types.UnknownPaymentSource()). The distinction is the whole point: one is
	// "the question does not apply", the other is "we looked and could not tell".
	//
	// It belongs on the event rather than being computed by a projector because
	// the read models are rebuilt from JetStream on every start: resolved once at
	// creation time, a replay years later reproduces the same provenance, whereas
	// anything derived from current read-model state would not.
	//
	// It is carried on requested specifically because that is the only branch of
	// the payment projector that writes the descriptive columns — reserved and
	// received are UPDATEs keyed on reference.
	Source *types.PaymentSource `json:"source,omitempty"`
}

type NathejkPaymentReserved struct {
	Reference string    `json:"reference"`
	Amount    int       `json:"amount"`
	Currency  string    `json:"currency"`
	Timestamp time.Time `json:"timestamp"`
}

type NathejkPaymentReceived struct {
	Reference string    `json:"reference"`
	Amount    int       `json:"amount"`
	Currency  string    `json:"currency"`
	Timestamp time.Time `json:"timestamp"`
}

type NathejkPaymentFailed struct {
	Reference string    `json:"reference"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}
