package payment

import "github.com/nathejk/shared-go/types"

// This file declares what the payment entity requires from a payment service
// provider, in Nathejk's own vocabulary rather than any provider's.
//
// Unlike the validator and mail ports, this one is not satisfied structurally:
// a provider's client speaks its own types, so cmd/api adapts it to Provider.
// That is the point. Previously these command signatures named MobilePay types
// directly, which meant a second provider — or a test — could not be
// introduced without changing the domain. The types below carry only what the
// commands actually use, which turned out to be a small fraction of MobilePay's
// payment model.

// Amount is a sum of money. Value is in the currency's minor unit (øre for
// DKK), matching how both the provider APIs and the projections represent it.
type Amount struct {
	Currency types.Currency
	Value    int64
}

// PaymentRequest asks a provider to authorise a payment.
//
// Reference is Nathejk's identifier for the payment and is expected back on
// the created payment; the provider must treat it as the payment's name rather
// than assigning its own. IdempotencyKey guards against a retried request
// creating a second authorisation.
type PaymentRequest struct {
	IdempotencyKey string
	Reference      string
	Amount         Amount
	Description    string

	// PhoneNumber identifies the payer to a wallet provider, in international
	// form.
	PhoneNumber string

	// Lines is the receipt to show the payer, and is guaranteed to sum to
	// Amount when non-empty — Request drops it otherwise rather than pass a
	// receipt that does not add up. Empty means "no receipt"; a provider that
	// cannot render one may ignore it entirely.
	Lines []Line
}

// PaymentCreated is the result of authorising a payment. RedirectURL is where
// the payer must be sent to complete it.
type PaymentCreated struct {
	Reference   string
	RedirectURL string
}

// Authorization is the state of a payment at the provider, reduced to what
// Capture needs to decide whether there is anything to capture.
//
// AuthorizedAmount and CapturedAmount are cumulative totals, so the amount
// still capturable is their difference — a payment may legitimately be captured
// in several parts.
type Authorization struct {
	// Authorized reports whether the payer has approved the payment. A
	// payment that is merely created, or has expired, has not.
	Authorized       bool
	Currency         types.Currency
	AuthorizedAmount int64
	CapturedAmount   int64
}

// Provider is a payment service provider: it authorises payments, reports
// their state, and captures authorised funds.
//
// Implementations are expected to be remote calls, and every method may fail
// for transport reasons alone.
type Provider interface {
	// CreatePayment authorises a new payment.
	CreatePayment(req PaymentRequest) (PaymentCreated, error)

	// GetAuthorization reports the current state of the payment named by
	// reference.
	GetAuthorization(reference string) (Authorization, error)

	// CapturePayment claims amount from an authorised payment. Capturing more
	// than remains authorised is the caller's error to avoid; see
	// Authorization.
	CapturePayment(reference string, amount Amount) error
}
