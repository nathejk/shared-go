package payment

import (
	"fmt"
	"log"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/types"
)

type consumer struct {
	w cqrs.Writer
}

func (c *consumer) Consumes() (subjs []cqrs.Subject) {
	return []cqrs.Subject{
		//cqrs.SubjectFromStr("monolith:nathejk_team"),
		//cqrs.SubjectFromStr("nathejk"),
		cqrs.SubjectFromStr("NATHEJK.2026.payment.*.requested"),
		cqrs.SubjectFromStr("NATHEJK.2026.payment.*.reserved"),
		cqrs.SubjectFromStr("NATHEJK.2026.payment.*.received"),
	}
}

func (c *consumer) HandleMessage(msg cqrs.Message) error {
	switch true {
	case msg.Subject().Match("NATHEJK.*.payment.*.requested"):
		var body messages.NathejkPaymentRequested
		if err := msg.Body(&body); err != nil {
			return err
		}
		if body.Reference == "" {
			return nil
		}
		sql := fmt.Sprintf("INSERT INTO payment SET reference=%q, receiptEmail=%q, returnUrl=%q, year=\"%d\", currency=%q, amount=%d, method=%q, createdAt=%q, changedAt=%q, status=%q, orderForeignKey=%q, orderType=%q ON DUPLICATE KEY UPDATE receiptEmail=VALUES(receiptEmail), returnUrl=VALUES(returnUrl), year=VALUES(year), currency=VALUES(currency), amount=VALUES(amount), method=VALUES(method), status=VALUES(status), orderForeignKey=VALUES(orderForeignKey), orderType=VALUES(orderType)", body.Reference, body.ReceiptEmail, body.ReturnUrl, msg.Time().Year(), body.Currency, body.Amount, body.Method, msg.Time(), msg.Time(), types.PaymentStatusRequested, body.OrderForeignKey, body.OrderType)
		if err := c.w.Consume(sql); err != nil {
			log.Fatalf("Error consuming sql %q", err)
		}

	case msg.Subject().Match("NATHEJK.*.payment.*.reserved"):
		var body messages.NathejkPaymentReserved
		if err := msg.Body(&body); err != nil {
			return err
		}
		err := c.w.Consume(fmt.Sprintf("UPDATE payment SET status=%q, changedAt=%q WHERE reference=%q", types.PaymentStatusReserved, msg.Time(), body.Reference))
		if err != nil {
			log.Fatalf("Error consuming sql %q", err)
		}

	case msg.Subject().Match("NATHEJK.*.payment.*.received"):
		var body messages.NathejkPaymentReceived
		if err := msg.Body(&body); err != nil {
			return err
		}
		err := c.w.Consume(fmt.Sprintf("UPDATE payment SET status=%q, changedAt=%q WHERE reference=%q", types.PaymentStatusReceived, msg.Time(), body.Reference))
		if err != nil {
			log.Fatalf("Error consuming sql %q", err)
		}
	default:
		log.Printf("Unhandled message %q", msg.Subject().Subject())
	}
	return nil
}
