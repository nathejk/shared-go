package payment

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/types"
)

// Line is one item on the payer's receipt, in the payment entity's own
// vocabulary.
//
// Deliberately not order.Line: shared-go's order package imports payment (for
// the saga's PaymentReader), so payment importing order would close an import
// cycle. The composition root maps between them, exactly as it adapts a provider
// client to Provider — see paymentLinesFromOrder in cmd/api.
//
// Amounts are in the currency's minor unit, like everything else here. Amount is
// the line's total (UnitCount × UnitPrice) rather than something to be recomputed
// downstream, because that is the number a provider's receipt has to reconcile
// against.
type Line struct {
	// Label is what the payer reads on their receipt, e.g. "T-shirt (Large)".
	Label     string
	UnitCount int
	UnitPrice int
	Amount    int
}

// OrderTypeOrder is the OrderType stamped on every payment this entity creates,
// and the marker that a payment's OrderForeignKey is an order id.
//
// The field is not redundant on the projection, only on the way in. Historically
// a payment pointed straight at a team and OrderType said which kind
// ("patrulje", "klan", "gøgler"); 769 of the 1189 rows still look like that, and
// mobilepayCallbackHandler branches on it to recover the payer's identity. Since
// the order entity landed, every new payment is for an order — so the value is a
// constant here and Charge does not ask a caller to repeat it.
//
// Exported because the order saga needs it to tell a legacy payment (no order to
// settle, ever) from an order it has not seen projected yet (worth retrying).
const OrderTypeOrder = "order"

// Charge is a request to take money.
//
// A struct rather than a positional list because the previous signature took
// seven arguments of which three were adjacent strings, so a transposition would
// have compiled and silently mis-linked the payment to its order.
//
// Lines are optional and descriptive: they become the receipt the payer sees and
// are recorded on the requested event. They must sum to Amount — see
// linesReconcile — because a receipt that disagrees with the sum charged is
// worse than no receipt.
type Charge struct {
	Amount      Amount
	Description string
	Phone       types.PhoneNumber
	Email       types.EmailAddress
	ReturnUrl   string

	// OrderID is the order being paid for. Named for what it is: on the way in
	// this is always an order id, unlike the projection's OrderForeignKey, which
	// still holds a team id for the legacy rows.
	OrderID string

	Lines []Line
}

// linesReconcile reports whether the lines account for exactly the amount being
// charged. Zero lines reconcile trivially: no receipt is sent.
//
// This is not pedantry. An open order's lines sum to its total while the charge
// is its outstanding amount, so the two diverge the moment an order is partly
// paid — and a payment provider may reject, or worse silently accept, a receipt
// that does not add up.
func (c Charge) linesReconcile() bool {
	if len(c.Lines) == 0 {
		return true
	}
	sum := 0
	for _, l := range c.Lines {
		sum += l.Amount
	}
	return int64(sum) == c.Amount.Value
}

// Commands is the payment write-side API. Methods publish payment events onto
// the stream and drive the payment provider.
//
// Satisfied by the *table returned by New — there is no separate constructor,
// so a caller cannot end up with a commander and a projector that disagree
// about which year they are working in.
type Commands interface {
	Request(Charge) (string, error)
	Capture(reference string) error
}

type commander struct {
	p cqrs.Publisher
	r repository
	// q is used only to check that a freshly minted reference is unused. Nil
	// disables the check, which is what a unit test constructing a commander
	// directly gets.
	q    Queries
	year types.YearSlug
}

// referenceAttempts is how many times Request will mint a reference before
// giving up on finding an unused one. A collision needs ~1e12 draws to be
// likely, so reaching the second attempt at all means something is wrong with
// the randomness rather than that we were unlucky — three is generous.
const referenceAttempts = 3

// Request authorises a payment with the provider and announces it on the
// stream, returning the URL the payer must be sent to.
//
// The event carries the provider's reference rather than the locally generated
// one: that is the identity every later payment event and the projection join
// on. The idempotency key is separate and deliberately distinct, so a retried
// request cannot look like the same payment being renamed.
//
// Receipt lines that do not sum to the charged amount are dropped rather than
// forwarded, and the payment proceeds without a receipt. Failing the payment
// over a cosmetic mismatch would be worse; sending a wrong one worse still.
func (c *commander) Request(ch Charge) (string, error) {
	lines := ch.Lines
	if !ch.linesReconcile() {
		log.Printf("payment: receipt lines do not sum to %d for order %s; requesting without a receipt",
			ch.Amount.Value, ch.OrderID)
		lines = nil
	}

	reference, err := c.newUnusedReference()
	if err != nil {
		return "", err
	}
	resp, err := c.r.provider.CreatePayment(PaymentRequest{
		IdempotencyKey: uuid.New().String(),
		Reference:      reference,
		Amount:         ch.Amount,
		Description:    ch.Description,
		PhoneNumber:    ch.Phone.InternationalNumber(),
		Lines:          lines,
	})
	if err != nil {
		return "", err
	}

	body := messages.NathejkPaymentRequested{
		Reference:       resp.Reference,
		ReceiptEmail:    ch.Email,
		ReturnUrl:       ch.ReturnUrl,
		Amount:          int(ch.Amount.Value),
		Currency:        string(ch.Amount.Currency),
		Timestamp:       time.Now(),
		Method:          "mobilepay",
		OrderLines:      messageLines(lines),
		OrderForeignKey: ch.OrderID,
		OrderType:       OrderTypeOrder,
	}
	msg := c.p.MessageFunc()(c.subject(resp.Reference, "requested"))
	msg.SetBody(body)

	if err := c.p.Publish(msg); err != nil {
		return "", err
	}
	return resp.RedirectURL, nil
}

// newUnusedReference mints a reference and checks it is not already taken.
//
// The check exists because a duplicate would not fail: the projector upserts on
// the reference, so a second payment carrying one that is already in use
// silently overwrites the first. At 40 bits that is vanishingly unlikely, but
// "vanishingly unlikely silent data loss" is worth one indexed read to rule out.
//
// It is not a hard guarantee — the projection is eventually consistent, so a
// reference minted moments ago may not be visible yet. It is a cheap backstop on
// top of the entropy, not a substitute for it.
func (c *commander) newUnusedReference() (string, error) {
	var lastErr error
	for range referenceAttempts {
		ref, err := newReference()
		if err != nil {
			return "", err
		}
		if c.q == nil {
			return ref, nil
		}
		_, err = c.q.GetByReference(context.Background(), ref)
		switch {
		case errors.Is(err, ErrRecordNotFound):
			return ref, nil
		case err != nil:
			// The lookup itself failed. Do not treat that as "taken" and spin;
			// the reference is almost certainly free, and refusing to take a
			// payment because a read failed is the worse outcome.
			log.Printf("payment: could not verify reference %q is unused: %v", ref, err)
			return ref, nil
		default:
			lastErr = fmt.Errorf("payment: reference %q is already in use", ref)
			log.Print(lastErr)
		}
	}
	return "", lastErr
}

// messageLines projects receipt lines onto the wire type. Kept separate so the
// command API does not expose the message struct, which would make every caller
// depend on the event contract.
func messageLines(lines []Line) []messages.NathejkPayment_OrderLine {
	out := make([]messages.NathejkPayment_OrderLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, messages.NathejkPayment_OrderLine{
			Label:     l.Label,
			UnitCount: l.UnitCount,
			UnitPrice: l.UnitPrice,
			Amount:    l.Amount,
		})
	}
	return out
}

// Capture claims the authorised-but-not-yet-taken funds of a payment.
//
// Reachable more than once — the provider's callback is a plain GET the payer
// can reload — so it must be safe to repeat: nothing is captured and nothing
// published once the authorisation is exhausted.
func (c *commander) Capture(reference string) error {
	auth, err := c.r.provider.GetAuthorization(reference)
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

	// reserved is published before the capture and received after it, so a
	// crash mid-capture leaves evidence that money was expected.
	msg := c.p.MessageFunc()(c.subject(reference, "reserved"))
	msg.SetBody(&messages.NathejkPaymentReserved{
		Reference: reference,
		Amount:    int(available.Value),
		Currency:  string(available.Currency),
		Timestamp: time.Now(),
	})
	if err := c.p.Publish(msg); err != nil {
		return err
	}

	if err := c.r.provider.CapturePayment(reference, available); err != nil {
		return err
	}

	msg = c.p.MessageFunc()(c.subject(reference, "received"))
	msg.SetBody(&messages.NathejkPaymentReceived{
		Reference: reference,
		Amount:    int(available.Value),
		Currency:  string(available.Currency),
		Timestamp: time.Now(),
	})
	return c.p.Publish(msg)
}

// subject builds the event subject for a payment transition. Centralised
// because the three call sites previously disagreed on the domain separator
// ("NATHEJK:2026." vs "NATHEJK.2026."); both normalise to the same subject, but
// only one of them says so.
func (c *commander) subject(reference, event string) cqrs.Subject {
	return cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.payment.%s.%s", c.year, reference, event))
}

var _ Commands = (*commander)(nil)
