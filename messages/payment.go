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
	Reference    string                     `json:"reference"`
	ReturnUrl    string                     `json:"returnUrl"`
	ReceiptEmail types.EmailAddress         `json:"receiptEmail"`
	Amount       int                        `json:"amount"`
	Currency     string                     `json:"currency"`
	Timestamp    time.Time                  `json:"timestamp"`
	Method       string                     `json:"method"`
	OrderLines   []NathejkPayment_OrderLine `json:"orderLines,omitempty"`
	//TeamID     types.TeamID               `json:"teamId,omitempty"`
	//MemberID   types.MemberID             `json:"memberId,omitempty"`
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
