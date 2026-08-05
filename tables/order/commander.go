package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/tables/product"
	"github.com/nathejk/shared-go/types"
)

// Errors returned by the commander. Mapped to HTTP 4xx by the API layer.
var (
	// ErrNotOpen is returned when a mutation is attempted against an order
	// that is not in StatusOpen (i.e. paid or cancelled). Paid orders are
	// immutable by design — the caller must EnsureOpenOrder for a fresh one.
	ErrNotOpen = errors.New("order is not open")

	// ErrProductNotEligible is returned when SetDerivedLines / AddManualLine
	// references a product whose EligibleFor list does not include the
	// order's OwnerType.
	ErrProductNotEligible = errors.New("product is not eligible for this owner")

	// ErrProductInactive is returned when adding an inactive (retired)
	// product to a new order. Existing lines that already reference an
	// inactive product are preserved by the projector.
	ErrProductInactive = errors.New("product is not active")

	// ErrOutOfStock is returned when adding lines would exceed a product's
	// finite stock. Products with NULL stock are unlimited and never raise
	// this error.
	ErrOutOfStock = errors.New("product out of stock")

	// ErrLineNotFound is returned by RemoveLine when the given LineID does
	// not exist on the order.
	ErrLineNotFound = errors.New("order line not found")

	// ErrMissingMemberID is returned when a DesiredLine is missing its
	// MemberID. Every line on an order must be attributable to a specific
	// member (the participant for participation lines, the recipient for
	// merchandise) so the order_line projection can answer "who ordered
	// what" without a JSON scan through attributes. Reservation
	// placeholders that don't yet have a real member identity should pass
	// a stable synthetic ID such as "pending-1" — the validation only
	// rejects empty strings.
	ErrMissingMemberID = errors.New("order line is missing memberId")
)

// Commands is the public command surface of the order package. The HTTP
// handlers (and any other caller) take this interface as a dependency.
//
// Every mutating method returns the post-mutation Order computed in-memory
// so callers don't have to round-trip the (eventually consistent)
// projection. The returned Order's PaidAmount / DueAmount are read from
// the projection at the start of the call: still accurate because mutating
// lines doesn't change payments. Lines and TotalAmount reflect the new
// state — exactly what the projector will write once it consumes the
// emitted event.
type Commands interface {
	// EnsureOpenOrder returns the (oldest) open order for the given owner
	// if one exists, otherwise creates a new empty order. This is the
	// normal entry point for the "update existing order if any" UX rule.
	EnsureOpenOrder(ctx context.Context, ownerType types.TeamType, ownerID string) (*Order, error)

	// SetDerivedLines replaces every line on the order with origin
	// "derived" by the given lines. Lines with origin "manual" are
	// preserved. Validates eligibility and stock; returns ErrNotOpen if
	// the order is not in StatusOpen.
	SetDerivedLines(ctx context.Context, orderID string, lines []DesiredLine) (*Order, error)

	// AddManualLine appends a single line of origin "manual" to the order.
	// If the line's LineID is empty, a UUID is generated. Validates
	// eligibility and stock; returns ErrNotOpen if the order is not open.
	AddManualLine(ctx context.Context, orderID string, line DesiredLine) (*Order, error)

	// RemoveLine deletes the line with the given LineID from the order
	// regardless of origin. Returns ErrLineNotFound if no such line
	// exists, and ErrNotOpen if the order is not open.
	RemoveLine(ctx context.Context, orderID, lineID string) (*Order, error)

	// Cancel transitions an open order to StatusCancelled. Reason is a
	// free-form string surfaced in the read model for support / audit.
	Cancel(ctx context.Context, orderID, reason string) (*Order, error)
}

// DesiredLine is the input shape callers pass to SetDerivedLines /
// AddManualLine. The commander snapshots ProductName and UnitPrice from
// the catalogue at the time of the call so that later catalogue edits
// don't retroactively change prices on existing orders.
//
// MemberID is required and must be a non-empty string. It identifies
// which member the line belongs to (the participant for participation
// lines, the recipient for t-shirts, etc.). The commander returns
// ErrMissingMemberID for any DesiredLine missing this field.
//
// LineID is optional. When omitted, the commander generates one:
//
//   - For derived lines: "derived:{ProductSKU}:{MemberID}". Derived
//     LineIDs are stable across SetDerivedLines calls so the projector
//     naturally upserts.
//   - For manual lines: a fresh UUID.
//
// Attributes is an optional bag for variant data (t-shirt size, ...).
// MemberID is *not* duplicated into Attributes — it has its own field.
type DesiredLine struct {
	LineID     string
	ProductSKU string
	MemberID   string
	Quantity   int
	Attributes map[string]any
}

type commander struct {
	p        cqrs.Publisher
	q        Queries
	products product.Queries
	year     types.YearSlug
}

// NewCommands is provided as a thin wrapper for callers that already hold
// the underlying dependencies (e.g. wiring code outside this package). The
// idiomatic way to build a commander is via order.New, which returns a
// value that already implements Commands.
func NewCommands(p cqrs.Publisher, q Queries, products product.Queries, year types.YearSlug) Commands {
	return &commander{p: p, q: q, products: products, year: year}
}

// EnsureOpenOrder — see Commands.EnsureOpenOrder.
func (c *commander) EnsureOpenOrder(ctx context.Context, ownerType types.TeamType, ownerID string) (*Order, error) {
	if existing, err := c.q.FindOpenOrder(ctx, c.year, ownerType, ownerID); err == nil {
		return existing, nil
	} else if !errors.Is(err, tables.ErrRecordNotFound) {
		return nil, err
	}

	orderID := uuid.NewString()
	now := time.Now()
	body := messages.NathejkOrderCreated{
		OrderID:   orderID,
		Year:      c.year,
		OwnerType: ownerType,
		OwnerID:   ownerID,
		Currency:  "DKK",
		Timestamp: now,
	}
	subj := cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.order.%s.created", c.year, orderID))
	msg := c.p.MessageFunc()(subj)
	msg.SetBody(&body)
	if err := c.p.Publish(msg); err != nil {
		return nil, err
	}
	// Return the in-memory representation of the freshly-created order so
	// the caller doesn't need to wait for the projection.
	return &Order{
		OrderID:   orderID,
		Year:      c.year,
		OwnerType: ownerType,
		OwnerID:   ownerID,
		Status:    StatusOpen,
		Currency:  "DKK",
		CreatedAt: now.Format(time.RFC3339),
		ChangedAt: now.Format(time.RFC3339),
	}, nil
}

// SetDerivedLines — see Commands.SetDerivedLines.
func (c *commander) SetDerivedLines(ctx context.Context, orderID string, desired []DesiredLine) (*Order, error) {
	o, err := c.q.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if o.Status != StatusOpen {
		return nil, ErrNotOpen
	}

	// Bill by unit count, not by member identity. A team pays for a number of
	// units per SKU (participation seats, t-shirts); the people occupying them
	// may change over time. So the open order should only carry the desired
	// lines *beyond* what has already been paid for that SKU. This makes a
	// swapped member (delete + add within the paid count) cost nothing, and
	// lets a t-shirt size change for free, while an (N+1)-th unit is still
	// charged. Only paid orders count toward paidQty; cancelled and other open
	// orders do not.
	paidQty, err := c.q.PaidQuantityBySKU(ctx, c.year, o.OwnerType, o.OwnerID)
	if err != nil {
		return nil, err
	}
	desired = ApplyPaidOffset(desired, paidQty)

	// Start from the existing manual lines (preserved across SetDerivedLines).
	kept := make([]messages.NathejkOrder_Line, 0, len(o.Lines))
	for _, l := range o.Lines {
		if messages.LineOrigin(l.Origin) == messages.LineOriginManual {
			kept = append(kept, toMsgLine(l))
		}
	}

	// Build the new derived lines, validating each against the catalogue.
	derived, err := c.buildLines(ctx, o, desired, messages.LineOriginDerived)
	if err != nil {
		return nil, err
	}

	full := append(kept, derived...)
	if err := c.checkStock(ctx, o, full); err != nil {
		return nil, err
	}
	if err := c.publishLinesChanged(orderID, full); err != nil {
		return nil, err
	}
	return applyLines(o, full), nil
}

// AddManualLine — see Commands.AddManualLine.
func (c *commander) AddManualLine(ctx context.Context, orderID string, line DesiredLine) (*Order, error) {
	o, err := c.q.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if o.Status != StatusOpen {
		return nil, ErrNotOpen
	}

	added, err := c.buildLines(ctx, o, []DesiredLine{line}, messages.LineOriginManual)
	if err != nil {
		return nil, err
	}

	full := make([]messages.NathejkOrder_Line, 0, len(o.Lines)+len(added))
	for _, l := range o.Lines {
		full = append(full, toMsgLine(l))
	}
	full = append(full, added...)

	if err := c.checkStock(ctx, o, full); err != nil {
		return nil, err
	}
	if err := c.publishLinesChanged(orderID, full); err != nil {
		return nil, err
	}
	return applyLines(o, full), nil
}

// RemoveLine — see Commands.RemoveLine.
func (c *commander) RemoveLine(ctx context.Context, orderID, lineID string) (*Order, error) {
	o, err := c.q.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if o.Status != StatusOpen {
		return nil, ErrNotOpen
	}

	full := make([]messages.NathejkOrder_Line, 0, len(o.Lines))
	found := false
	for _, l := range o.Lines {
		if l.LineID == lineID {
			found = true
			continue
		}
		full = append(full, toMsgLine(l))
	}
	if !found {
		return nil, ErrLineNotFound
	}
	if err := c.publishLinesChanged(orderID, full); err != nil {
		return nil, err
	}
	return applyLines(o, full), nil
}

// Cancel — see Commands.Cancel.
func (c *commander) Cancel(ctx context.Context, orderID, reason string) (*Order, error) {
	o, err := c.q.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if o.Status != StatusOpen {
		return nil, ErrNotOpen
	}
	body := messages.NathejkOrderCancelled{
		OrderID:   orderID,
		Reason:    reason,
		Timestamp: time.Now(),
	}
	subj := cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.order.%s.cancelled", o.Year, orderID))
	msg := c.p.MessageFunc()(subj)
	msg.SetBody(&body)
	if err := c.p.Publish(msg); err != nil {
		return nil, err
	}
	o.Status = StatusCancelled
	o.CancelReason = reason
	return o, nil
}

// buildLines turns DesiredLine values into validated NathejkOrder_Line
// snapshots, looking up each product in the catalogue. It does not perform
// stock checks — those happen in checkStock once the full new line set is
// known, so that we count one order's own lines correctly.
//
// Lines are deduplicated by LineID: if two DesiredLines resolve to the
// same LineID (typically because of upstream duplicates such as a member
// list with repeated entries), the later one wins. This keeps the
// projector's INSERT step from hitting a primary-key collision — the
// snapshot only has to be unique by LineID anyway.
func (c *commander) buildLines(ctx context.Context, o *Order, desired []DesiredLine, origin messages.LineOrigin) ([]messages.NathejkOrder_Line, error) {
	type indexed struct {
		pos  int
		line messages.NathejkOrder_Line
	}
	byLine := make(map[string]indexed, len(desired))
	next := 0
	for _, d := range desired {
		if d.Quantity <= 0 {
			continue // a quantity of zero just means "this line is not present"
		}
		if d.MemberID == "" {
			return nil, fmt.Errorf("%s: %w", d.ProductSKU, ErrMissingMemberID)
		}
		p, err := c.products.GetBySKU(ctx, o.Year, d.ProductSKU)
		if err != nil {
			return nil, fmt.Errorf("product %s: %w", d.ProductSKU, err)
		}
		if !p.Active {
			return nil, fmt.Errorf("%s: %w", d.ProductSKU, ErrProductInactive)
		}
		if !p.IsEligibleFor(o.OwnerType) {
			return nil, fmt.Errorf("%s for %s: %w", d.ProductSKU, o.OwnerType, ErrProductNotEligible)
		}

		lineID := d.LineID
		if lineID == "" {
			lineID = defaultLineID(origin, d)
		}

		line := messages.NathejkOrder_Line{
			LineID:      lineID,
			ProductSKU:  p.SKU,
			ProductName: p.Name,
			MemberID:    d.MemberID,
			UnitPrice:   p.UnitPrice,
			Quantity:    d.Quantity,
			LineTotal:   p.UnitPrice * d.Quantity,
			Origin:      origin,
			Attributes:  d.Attributes,
		}
		if existing, ok := byLine[lineID]; ok {
			// Preserve the position of the first occurrence so output
			// order is stable, but overwrite the line value with the
			// later occurrence.
			byLine[lineID] = indexed{pos: existing.pos, line: line}
			continue
		}
		byLine[lineID] = indexed{pos: next, line: line}
		next++
	}

	out := make([]messages.NathejkOrder_Line, len(byLine))
	for _, ix := range byLine {
		out[ix.pos] = ix.line
	}
	return out, nil
}

// checkStock enforces finite-stock products against the post-mutation
// line set. Two kinds of products use different rules:
//
//   - KindParticipation ("team overflow"): the entire request passes
//     iff any stock remains when this order starts adding. This matches
//     the klan rule — "if 1 seat is left, a klan of 4 still fits". An
//     existing klan editing its members is similarly never blocked by
//     a system that is already over its cap, since their previous seats
//     don't count as "elsewhere".
//
//   - KindMerchandise ("strict per-unit"): newQty must fit within
//     stock - reservedElsewhere. Used for shop items like t-shirts where
//     stocking 1 means selling 1, not many.
//
// reservedElsewhere = total reserved across all non-cancelled orders
// for the SKU, minus what this very order had on its previous lines.
// Subtracting our own lines prevents double-counting when an existing
// order is being edited (e.g. a klan changing members).
//
// Products with Stock == nil are unlimited and always skipped.
func (c *commander) checkStock(ctx context.Context, o *Order, full []messages.NathejkOrder_Line) error {
	// Aggregate the new desired quantity per SKU.
	newQtyBySKU := map[string]int{}
	for _, l := range full {
		newQtyBySKU[l.ProductSKU] += l.Quantity
	}
	// Aggregate this order's previous quantity per SKU.
	existingQtyBySKU := map[string]int{}
	for _, l := range o.Lines {
		existingQtyBySKU[l.ProductSKU] += l.Quantity
	}

	for sku, newQty := range newQtyBySKU {
		p, err := c.products.GetBySKU(ctx, o.Year, sku)
		if err != nil {
			return err
		}
		if p.Stock == nil {
			continue // unlimited
		}
		reservedAll, err := c.q.ReservedQuantity(ctx, o.Year, sku)
		if err != nil {
			return err
		}
		reservedElsewhere := reservedAll - existingQtyBySKU[sku]

		if p.Kind == product.KindParticipation {
			// Team overflow rule: as long as there's any remaining stock
			// at the moment of decision, the whole order passes.
			if reservedElsewhere >= *p.Stock {
				return fmt.Errorf("%s (no seats remaining): %w", sku, ErrOutOfStock)
			}
			continue
		}

		// Default (KindMerchandise and any future kinds): strict.
		if reservedElsewhere+newQty > *p.Stock {
			return fmt.Errorf("%s (need %d, %d remaining): %w",
				sku, newQty, *p.Stock-reservedElsewhere, ErrOutOfStock)
		}
	}
	return nil
}

// publishLinesChanged emits a NathejkOrderLinesChanged event with the full
// new line set and the recomputed total.
func (c *commander) publishLinesChanged(orderID string, lines []messages.NathejkOrder_Line) error {
	total := 0
	for _, l := range lines {
		total += l.LineTotal
	}
	body := messages.NathejkOrderLinesChanged{
		OrderID:     orderID,
		Lines:       lines,
		TotalAmount: total,
		Timestamp:   time.Now(),
	}
	subj := cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK:%s.order.%s.lines.changed", c.year, orderID))
	msg := c.p.MessageFunc()(subj)
	msg.SetBody(&body)
	return c.p.Publish(msg)
}

// ApplyPaidOffset removes already-paid units from a desired line set so the
// open order only carries (and bills) the units beyond what has already been
// paid. paidQty is the paid quantity per productSKU (see
// Queries.PaidQuantityBySKU).
//
// Billing is by *count*, not member identity: a team pays for a number of
// participation seats and t-shirts, and the people occupying them may change.
// So for each SKU we drop up to paidQty[sku] of the desired lines; whichever
// lines survive are the ones charged. This is what makes a swapped member cost
// nothing (delete + add within the paid count) and a t-shirt size change free,
// while still charging the (N+1)-th unit.
//
// The function is pure so the show-path sync check and the SetDerivedLines
// command compute the same target from the same input.
func ApplyPaidOffset(desired []DesiredLine, paidQty map[string]int) []DesiredLine {
	if len(paidQty) == 0 {
		return desired
	}
	remaining := make(map[string]int, len(paidQty))
	for sku, qty := range paidQty {
		remaining[sku] = qty
	}
	filtered := make([]DesiredLine, 0, len(desired))
	for _, d := range desired {
		if remaining[d.ProductSKU] >= d.Quantity {
			remaining[d.ProductSKU] -= d.Quantity
			continue
		}
		filtered = append(filtered, d)
	}
	return filtered
}

// defaultLineID generates a stable LineID when a caller didn't provide one.
//
// For derived lines we use "derived:{sku}:{memberId}" so successive
// SetDerivedLines calls upsert into the same row rather than racking up
// orphans. MemberID is guaranteed non-empty by the buildLines validator,
// so this is always a deterministic, collision-free key.
//
// For manual lines a UUID is fine — RemoveLine takes the LineID as an
// argument so callers don't need to derive it.
func defaultLineID(origin messages.LineOrigin, d DesiredLine) string {
	if origin == messages.LineOriginDerived {
		return fmt.Sprintf("derived:%s:%s", d.ProductSKU, d.MemberID)
	}
	return uuid.NewString()
}

// toMsgLine projects a read-model Line into the wire format used by
// NathejkOrderLinesChanged. Read and wire happen to share field names but
// keeping a converter avoids accidental coupling if either drifts.
func toMsgLine(l Line) messages.NathejkOrder_Line {
	return messages.NathejkOrder_Line{
		LineID:      l.LineID,
		ProductSKU:  l.ProductSKU,
		ProductName: l.ProductName,
		MemberID:    l.MemberID,
		UnitPrice:   l.UnitPrice,
		Quantity:    l.Quantity,
		LineTotal:   l.LineTotal,
		Origin:      messages.LineOrigin(l.Origin),
		Attributes:  l.Attributes,
	}
}

// applyLines builds the in-memory post-mutation Order returned by the
// SetDerivedLines / AddManualLine / RemoveLine commands. It reuses the
// owner / status / paidAmount fields from the pre-mutation snapshot —
// payments don't change as a result of these commands, so the existing
// projection-derived PaidAmount is still correct — and overwrites the
// lines / totals from the freshly-built event payload.
func applyLines(o *Order, lines []messages.NathejkOrder_Line) *Order {
	total := 0
	out := make([]Line, 0, len(lines))
	for _, l := range lines {
		total += l.LineTotal
		out = append(out, Line{
			LineID:      l.LineID,
			ProductSKU:  l.ProductSKU,
			ProductName: l.ProductName,
			MemberID:    l.MemberID,
			UnitPrice:   l.UnitPrice,
			Quantity:    l.Quantity,
			LineTotal:   l.LineTotal,
			Origin:      string(l.Origin),
			Attributes:  l.Attributes,
		})
	}
	due := total - o.PaidAmount
	return &Order{
		OrderID:      o.OrderID,
		Year:         o.Year,
		OwnerType:    o.OwnerType,
		OwnerID:      o.OwnerID,
		Status:       o.Status,
		Currency:     o.Currency,
		TotalAmount:  total,
		PaidAmount:   o.PaidAmount,
		DueAmount:    due,
		Lines:        out,
		CancelReason: o.CancelReason,
		CreatedAt:    o.CreatedAt,
		ChangedAt:    time.Now().Format(time.RFC3339),
	}
}
