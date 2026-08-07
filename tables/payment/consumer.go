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

// Consumes subscribes to every year, not a hard-coded one. The shared copy of
// this entity named NATHEJK.2026.payment.* and would have stopped projecting
// payments on 1 January 2027 without any error to show why.
func (c *consumer) Consumes() []cqrs.Subject {
	return []cqrs.Subject{
		cqrs.SubjectFromStr("NATHEJK.*.payment.*.requested"),
		cqrs.SubjectFromStr("NATHEJK.*.payment.*.reserved"),
		cqrs.SubjectFromStr("NATHEJK.*.payment.*.received"),
	}
}

// HandleMessage projects a payment event.
//
// Every branch returns its error instead of calling log.Fatalf as the shared
// copy did: the Writer is wrapped in a dead-letter writer whose entire purpose
// is to record a failing statement and carry on, and killing the process
// defeats it — one malformed payment event would take the API down on every
// subsequent replay.
//
// The operations column accumulates one JSON entry per transition, appended
// rather than overwritten, so a payment captured in several parts can be
// reconstructed from the projection alone. status/changedAt remain the
// current-state summary.
func (c *consumer) HandleMessage(msg cqrs.Message) error {
	switch {
	case msg.Subject().Match("NATHEJK.*.payment.*.requested"):
		var body messages.NathejkPaymentRequested
		if err := msg.Body(&body); err != nil {
			return err
		}
		// A payment with no reference cannot be keyed, updated or joined to
		// an order; there is nothing useful to store.
		if body.Reference == "" {
			return nil
		}
		// year comes from the subject, not msg.Time().Year(): the subject
		// carries the domain year the payment belongs to, which is what the
		// commands publish and what hq's year filter means. Both copies used
		// the publication timestamp, which is the same value right up until a
		// season is opened in the preceding calendar year.
		return c.w.Consume(fmt.Sprintf(
			"INSERT INTO payment SET reference=%q, receiptEmail=%q, returnUrl=%q, year=%q, currency=%q, amount=%d, method=%q, createdAt=%q, changedAt=%q, status=%q, orderForeignKey=%q, orderType=%q, "+
				"operations=JSON_ARRAY(JSON_OBJECT('type',%q,'amount',%d,'time',%q)) "+
				"ON DUPLICATE KEY UPDATE receiptEmail=VALUES(receiptEmail), returnUrl=VALUES(returnUrl), year=VALUES(year), currency=VALUES(currency), amount=VALUES(amount), method=VALUES(method), status=VALUES(status), orderForeignKey=VALUES(orderForeignKey), orderType=VALUES(orderType), operations=VALUES(operations)",
			body.Reference, body.ReceiptEmail, body.ReturnUrl, msg.Subject().Parts()[1], body.Currency, body.Amount, body.Method,
			msg.Time(), msg.Time(), types.PaymentStatusRequested, body.OrderForeignKey, body.OrderType,
			types.PaymentStatusRequested, body.Amount, msg.Time(),
		))

	case msg.Subject().Match("NATHEJK.*.payment.*.reserved"):
		var body messages.NathejkPaymentReserved
		if err := msg.Body(&body); err != nil {
			return err
		}
		return c.w.Consume(transition(types.PaymentStatusReserved, body.Reference, body.Amount, msg))

	case msg.Subject().Match("NATHEJK.*.payment.*.received"):
		var body messages.NathejkPaymentReceived
		if err := msg.Body(&body); err != nil {
			return err
		}
		return c.w.Consume(transition(types.PaymentStatusReceived, body.Reference, body.Amount, msg))

	default:
		log.Printf("Unhandled message %q", msg.Subject().Subject())
	}
	return nil
}

// transition builds the UPDATE for a state change: set the current status and
// append the operation to the trail.
//
// JSON_ARRAY_APPEND on a NULL column yields NULL, which would erase the trail —
// COALESCE guards rows created before the operations column existed, whose value
// the ALTER in table.sql defaults but which an older projector may have left
// null.
func transition(status types.PaymentStatus, reference string, amount int, msg cqrs.Message) string {
	return fmt.Sprintf(
		"UPDATE payment SET status=%q, changedAt=%q, "+
			"operations=JSON_ARRAY_APPEND(COALESCE(operations,JSON_ARRAY()),'$',JSON_OBJECT('type',%q,'amount',%d,'time',%q)) "+
			"WHERE reference=%q",
		status, msg.Time(), status, amount, msg.Time(), reference,
	)
}

var _ cqrs.Consumer = (*consumer)(nil)
