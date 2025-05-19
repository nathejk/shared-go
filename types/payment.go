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
