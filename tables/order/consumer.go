package order

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/nathejk/shared-go/messages"

	"github.com/jrgensen/cqrs"
)

// consumer projects the four order events onto the orders / order_line
// tables. Snapshot semantics on NathejkOrderLinesChanged: the existing
// lines for the order are deleted and replaced with the lines from the
// event. This keeps the projector trivially idempotent and replay-safe.
type consumer struct {
	w cqrs.Writer
}

func (c *consumer) Consumes() []cqrs.Subject {
	return []cqrs.Subject{
		cqrs.SubjectFromStr("NATHEJK:*.order.*.created"),
		cqrs.SubjectFromStr("NATHEJK:*.order.*.lines.changed"),
		cqrs.SubjectFromStr("NATHEJK:*.order.*.cancelled"),
		cqrs.SubjectFromStr("NATHEJK:*.order.*.paid"),
	}
}

func (c *consumer) HandleMessage(msg cqrs.Message) error {
	switch {
	case msg.Subject().Match("NATHEJK.*.order.*.created"):
		return c.handleCreated(msg)
	case msg.Subject().Match("NATHEJK.*.order.*.lines.changed"):
		return c.handleLinesChanged(msg)
	case msg.Subject().Match("NATHEJK.*.order.*.cancelled"):
		return c.handleCancelled(msg)
	case msg.Subject().Match("NATHEJK.*.order.*.paid"):
		return c.handlePaid(msg)
	default:
		log.Printf("order consumer: unhandled subject %q", msg.Subject().Subject())
		return nil
	}
}

func (c *consumer) handleCreated(msg cqrs.Message) error {
	var body messages.NathejkOrderCreated
	if err := msg.Body(&body); err != nil {
		return err
	}
	if body.OrderID == "" {
		return nil
	}
	currency := body.Currency
	if currency == "" {
		currency = "DKK"
	}
	// Use INSERT IGNORE rather than ON DUPLICATE KEY UPDATE: an order's
	// (year, ownerType, ownerId) tuple is fixed at creation and must never
	// be changed by a replayed Created event.
	query := fmt.Sprintf(
		"INSERT IGNORE INTO orders SET orderId=%q, year=%q, ownerType=%q, ownerId=%q, status=%q, currency=%q, totalAmount=0, createdAt=%q, changedAt=%q",
		body.OrderID, body.Year, string(body.OwnerType), body.OwnerID, string(StatusOpen), currency, msg.Time(), msg.Time(),
	)
	if err := c.w.Consume(query); err != nil {
		return fmt.Errorf("project order.created %s: %w", body.OrderID, err)
	}
	return nil
}

func (c *consumer) handleLinesChanged(msg cqrs.Message) error {
	var body messages.NathejkOrderLinesChanged
	if err := msg.Body(&body); err != nil {
		return err
	}
	if body.OrderID == "" {
		return nil
	}

	// Snapshot replace: drop existing lines for the order, re-insert.
	// Not transactional, but the rest of this codebase isn't either, and
	// the window between DELETE and INSERT is narrow enough that the
	// occasional reader sees an empty lines list rather than wrong data.
	if err := c.w.Consume(fmt.Sprintf("DELETE FROM order_line WHERE orderId=%q", body.OrderID)); err != nil {
		return fmt.Errorf("project order.lines.changed delete %s: %w", body.OrderID, err)
	}
	for _, ln := range body.Lines {
		attrs := ""
		if len(ln.Attributes) > 0 {
			b, err := json.Marshal(ln.Attributes)
			if err != nil {
				return fmt.Errorf("encode attributes for %s/%s: %w", body.OrderID, ln.LineID, err)
			}
			attrs = string(b)
		}
		// Defensive upsert: if the snapshot ever contains two lines with
		// the same LineID (e.g. due to upstream duplicate members), the
		// second occurrence wins instead of aborting the projection mid-
		// loop. The commander deduplicates before publishing, but a
		// belt-and-braces ON DUPLICATE KEY UPDATE keeps a buggy producer
		// from leaving the order_line table in a partial state and the
		// orders.totalAmount stuck at its pre-event value.
		//
		// %q on attrs is sufficient for SQL string escaping: both Go's
		// quoted-string syntax and MariaDB's default string parsing treat
		// `\"` as `"` and `\\` as `\`, so the JSON survives the round-trip
		// untouched. (An earlier pre-escape step here turned out to be
		// double-escaping and corrupting the attributes column.)
		query := fmt.Sprintf(
			"INSERT INTO order_line SET orderId=%q, lineId=%q, productSku=%q, productName=%q, memberId=%q, unitPrice=%d, quantity=%d, lineTotal=%d, origin=%q, attributes=%q, createdAt=%q, changedAt=%q "+
				"ON DUPLICATE KEY UPDATE productSku=VALUES(productSku), productName=VALUES(productName), memberId=VALUES(memberId), unitPrice=VALUES(unitPrice), quantity=VALUES(quantity), lineTotal=VALUES(lineTotal), origin=VALUES(origin), attributes=VALUES(attributes), changedAt=VALUES(changedAt)",
			body.OrderID, ln.LineID, ln.ProductSKU, ln.ProductName, ln.MemberID, ln.UnitPrice, ln.Quantity, ln.LineTotal, string(ln.Origin), attrs, msg.Time(), msg.Time(),
		)
		if err := c.w.Consume(query); err != nil {
			return fmt.Errorf("project order.lines.changed insert %s/%s: %w", body.OrderID, ln.LineID, err)
		}
	}
	if err := c.w.Consume(fmt.Sprintf(
		"UPDATE orders SET totalAmount=%d, changedAt=%q WHERE orderId=%q",
		body.TotalAmount, msg.Time(), body.OrderID,
	)); err != nil {
		return fmt.Errorf("project order.lines.changed total %s: %w", body.OrderID, err)
	}
	return nil
}

func (c *consumer) handleCancelled(msg cqrs.Message) error {
	var body messages.NathejkOrderCancelled
	if err := msg.Body(&body); err != nil {
		return err
	}
	if body.OrderID == "" {
		return nil
	}
	// Guard with a status filter so a replayed Cancelled event never
	// regresses a paid order back to cancelled.
	query := fmt.Sprintf(
		"UPDATE orders SET status=%q, cancelReason=%q, changedAt=%q WHERE orderId=%q AND status=%q",
		string(StatusCancelled), body.Reason, msg.Time(), body.OrderID, string(StatusOpen),
	)
	if err := c.w.Consume(query); err != nil {
		return fmt.Errorf("project order.cancelled %s: %w", body.OrderID, err)
	}
	return nil
}

func (c *consumer) handlePaid(msg cqrs.Message) error {
	var body messages.NathejkOrderPaid
	if err := msg.Body(&body); err != nil {
		return err
	}
	if body.OrderID == "" {
		return nil
	}
	// Same idempotency guard as Cancelled.
	query := fmt.Sprintf(
		"UPDATE orders SET status=%q, changedAt=%q WHERE orderId=%q AND status=%q",
		string(StatusPaid), msg.Time(), body.OrderID, string(StatusOpen),
	)
	if err := c.w.Consume(query); err != nil {
		return fmt.Errorf("project order.paid %s: %w", body.OrderID, err)
	}
	return nil
}
