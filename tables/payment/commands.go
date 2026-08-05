package payment

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/types"
)

// Commands is the payment write-side API. Methods publish payment events
// onto the stream and (where relevant) drive the payment provider.
type Commands interface {
	Request(amount Amount, desc string, phone types.PhoneNumber, email types.EmailAddress, returnUrl, orderForeignKey, orderType string) (string, error)
	Capture(reference string) error
}

type commander struct {
	p  cqrs.Publisher
	pp Provider
}

// NewCommands wires a payment commander. The publisher is used for
// emitting the NathejkPayment* events that drive the projections; the
// provider is used to create and capture authorisations.
func NewCommands(p cqrs.Publisher, pp Provider) Commands {
	return &commander{p: p, pp: pp}
}

func (c *commander) Request(amount Amount, desc string, phone types.PhoneNumber, email types.EmailAddress, returnUrl string, orderForeignKey string, orderType string) (string, error) {
	reference := uuid.New().String()
	resp, err := c.pp.CreatePayment(PaymentRequest{
		IdempotencyKey: uuid.New().String(),
		Reference:      reference,
		Amount:         amount,
		Description:    desc,
		PhoneNumber:    phone.InternationalNumber(),
	})
	if err != nil {
		return "", err
	}

	body := messages.NathejkPaymentRequested{
		Reference:       resp.Reference,
		ReceiptEmail:    email,
		ReturnUrl:       returnUrl,
		Amount:          int(amount.Value),
		Currency:        string(amount.Currency),
		Timestamp:       time.Now(),
		Method:          "mobilepay",
		OrderLines:      []messages.NathejkPayment_OrderLine{},
		OrderForeignKey: orderForeignKey,
		OrderType:       orderType,
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.payment.%s.requested", "2026", resp.Reference)))
	msg.SetBody(body)

	if err := c.p.Publish(msg); err != nil {
		return "", err
	}
	return resp.RedirectURL, nil
}

func (c *commander) Capture(reference string) error {
	auth, err := c.pp.GetAuthorization(reference)
	if err != nil {
		return err
	}

	// Capture only what is authorised and not yet taken. Both totals are
	// cumulative, so this stays correct across partial captures.
	available := Amount{
		Currency: auth.Currency,
		Value:    auth.AuthorizedAmount - auth.CapturedAmount,
	}

	if !auth.Authorized || available.Value <= 0 {
		return nil
	}

	body := &messages.NathejkPaymentReserved{
		Reference: reference,
		Amount:    int(available.Value),
		Currency:  string(available.Currency),
		Timestamp: time.Now(),
	}
	msg := c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK.%s.payment.%s.reserved", "2026", reference)))
	msg.SetBody(body)

	if err := c.p.Publish(msg); err != nil {
		return err
	}
	if err := c.pp.CapturePayment(reference, available); err != nil {
		return err
	}
	msg = c.p.MessageFunc()(cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.payment.%s.received", "2026", reference)))
	msg.SetBody(&messages.NathejkPaymentReceived{
		Reference: reference,
		Amount:    int(available.Value),
		Currency:  string(available.Currency),
		Timestamp: time.Now(),
	})

	if err := c.p.Publish(msg); err != nil {
		return err
	}

	// TODO send mail
	return nil
}
