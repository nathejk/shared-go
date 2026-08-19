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

	// SyncNeeded reports whether the order's derived lines already say what
	// the given desired set implies, so a caller rendering an order can skip
	// SetDerivedLines when there is nothing to change.
	//
	// Use this rather than comparing lines yourself: it applies the same
	// paid-unit offset SetDerivedLines does, so the read path and the write
	// path cannot disagree about the target. A caller whose comparison drifts
	// from the command republishes the order's lines on every page load.
	SyncNeeded(ctx context.Context, o *Order, desired []DesiredLine) (bool, error)

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
//   - For derived lines: "derived:{ProductSKU}:{MemberID}", with the size
//     appended for sized products so that a credit line and a charge line for
//     the same member do not collide. Derived LineIDs are stable across
//     SetDerivedLines calls so the projector naturally upserts.
//   - For manual lines: a fresh UUID.
//
// Quantity is normally positive. A negative quantity is a credit: it reclaims a
// unit already paid for, and is produced only by ApplyPaidOffset, always paired
// with a positive line for the same SKU so the pair costs nothing. Zero means
// "this line is not present".
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
	// units per variant (participation seats, t-shirts in a given size); the
	// people occupying them may change over time. So the open order should only
	// carry the desired lines *beyond* what has already been paid, plus the
	// zero-sum pairs that record a size change on a unit already paid for. This
	// makes a swapped member cost nothing, keeps a size change free while still
	// stating which shirt is owed, and charges the (N+1)-th unit. Only paid
	// orders count; cancelled and other open orders do not.
	desired, err = c.offsetAgainstPaid(ctx, o, desired)
	if err != nil {
		return nil, err
	}

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
		if d.Quantity == 0 {
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
//
// Positive quantities only, on both sides. A derived credit line reclaims a paid
// unit of another size and must not create headroom for the unit replacing it:
// the pair nets to zero, so netting them would let a size change pass a stock
// check that the replacement size alone would fail. Both sums are filtered, not
// just the new one — filtering only newQty while existingQty still netted would
// overstate reservedElsewhere for an order that already holds a credit, and block
// it on stock it is not asking for.
func (c *commander) checkStock(ctx context.Context, o *Order, full []messages.NathejkOrder_Line) error {
	// Aggregate the new desired quantity per SKU.
	newQtyBySKU := map[string]int{}
	for _, l := range full {
		if l.Quantity > 0 {
			newQtyBySKU[l.ProductSKU] += l.Quantity
		}
	}
	// Aggregate this order's previous quantity per SKU.
	existingQtyBySKU := map[string]int{}
	for _, l := range o.Lines {
		if l.Quantity > 0 {
			existingQtyBySKU[l.ProductSKU] += l.Quantity
		}
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

// offsetAgainstPaid gathers what ApplyPaidOffset needs — the owner's paid units
// per variant, and the catalogue size order for each SKU involved — and applies
// it.
//
// Split out so SyncNeeded can compute the identical target: the two are the write
// path and the read path of the same decision, and any difference between them
// shows up as an order that republishes its lines on every page load.
func (c *commander) offsetAgainstPaid(ctx context.Context, o *Order, desired []DesiredLine) ([]DesiredLine, error) {
	paid, err := c.q.PaidQuantityByVariant(ctx, c.year, o.OwnerType, o.OwnerID)
	if err != nil {
		return nil, err
	}
	sizeOrder, err := c.sizeOrder(ctx, o.Year, desired, paid)
	if err != nil {
		return nil, err
	}
	return ApplyPaidOffset(desired, paid, sizeOrder), nil
}

// sizeOrder reads the catalogue sizes of every SKU that could take part in a
// reclaim, so the pairing order is the catalogue's and not the map's.
//
// A SKU missing from the catalogue is not an error here: buildLines validates the
// desired lines against the catalogue immediately afterwards and reports it
// properly, whereas failing here would turn a retired product into an opaque
// error from the offset step.
func (c *commander) sizeOrder(ctx context.Context, year types.YearSlug, desired []DesiredLine, paid map[VariantKey]int) (map[string][]string, error) {
	skus := map[string]struct{}{}
	for _, d := range desired {
		skus[d.ProductSKU] = struct{}{}
	}
	for k := range paid {
		skus[k.SKU] = struct{}{}
	}
	out := make(map[string][]string, len(skus))
	for sku := range skus {
		p, err := c.products.GetBySKU(ctx, year, sku)
		if err != nil {
			continue
		}
		out[sku] = p.Sizes
	}
	return out, nil
}

// SyncNeeded reports whether o's derived lines already say what desired implies,
// so a caller rendering an order can skip SetDerivedLines when there is nothing
// to change.
//
// It exists so that the read path cannot drift from the write path. Both apply
// the same paid-unit offset and compare the same keys; a caller reimplementing
// the comparison would eventually disagree with SetDerivedLines, and the symptom
// — a lines.changed event on every GET — is easy to cause and hard to notice.
//
// Keys on (sku, memberId, size, quantity). Quantity is part of the key because a
// credit line and a charge line differ only in its sign, and treating -1 xxl as
// interchangeable with +1 xxl would report "in sync" for an order that says the
// opposite of what is wanted.
func (c *commander) SyncNeeded(ctx context.Context, o *Order, desired []DesiredLine) (bool, error) {
	target, err := c.offsetAgainstPaid(ctx, o, desired)
	if err != nil {
		return false, err
	}

	want := map[string]int{}
	for _, d := range target {
		if d.Quantity == 0 {
			continue
		}
		want[lineKey(d.ProductSKU, d.MemberID, lineSize(d.Attributes), d.Quantity)]++
	}
	have := map[string]int{}
	for _, l := range o.Lines {
		if messages.LineOrigin(l.Origin) != messages.LineOriginDerived {
			continue
		}
		have[lineKey(l.ProductSKU, l.MemberID, lineSize(l.Attributes), l.Quantity)]++
	}
	if len(want) != len(have) {
		return true, nil
	}
	for k, n := range want {
		if have[k] != n {
			return true, nil
		}
	}
	return false, nil
}

func lineKey(sku, memberID, size string, qty int) string {
	return fmt.Sprintf("%s|%s|%s|%d", sku, memberID, size, qty)
}

// ApplyPaidOffset removes already-paid units from a desired line set, and pairs
// what is left against paid units of a different size so that a size change on
// an already-paid unit costs nothing and still says what has to be shipped.
//
// Billing is by *count*, not member identity: a team pays for a number of
// participation seats and t-shirts, and the people occupying them may change. So
// for each variant we drop up to paid[variant] of the desired lines; whichever
// lines survive are the ones charged. That is what makes a swapped member cost
// nothing and still charges the (N+1)-th unit.
//
// Size, however, is not fungible. Counting paid units per SKU alone made a size
// change free by making it *invisible*: the desired 3xl cancelled against the
// paid xxl and the open order ended up empty, so the only place fulfillment can
// read "which shirts does this team get" said xxl forever. Units are therefore
// counted per (SKU, size), and an uncovered desired unit that can claim a paid
// unit of another size emits two lines instead of none:
//
//	tshirt.adult  qty -1  {"size":"xxl"}   the unit being reclaimed
//	tshirt.adult  qty +1  {"size":"3xl"}   the unit now owed
//
// Both are priced from the catalogue, so the pair sums to zero and nothing is
// charged — the payer's total is unchanged and the order states the swap.
//
// Paid units left over once every desired unit is covered produce nothing: a
// reduction is not a size change, and this mechanism does not refund.
//
// sizeOrder maps a SKU to its catalogue sizes (product.Product.Sizes) and decides
// which paid size gets reclaimed when several could be. It is passed in rather
// than looked up because this function must stay pure: the show-path sync check
// and SetDerivedLines have to compute the same target from the same input, or the
// order republishes its lines on every read.
func ApplyPaidOffset(desired []DesiredLine, paid map[VariantKey]int, sizeOrder map[string][]string) []DesiredLine {
	if len(paid) == 0 {
		return desired
	}

	// remaining[sku][size] is the paid units not yet accounted for.
	remaining := map[string]map[string]int{}
	for k, qty := range paid {
		if qty <= 0 {
			continue
		}
		if remaining[k.SKU] == nil {
			remaining[k.SKU] = map[string]int{}
		}
		remaining[k.SKU][k.Size] += qty
	}

	// Pass 1: cancel same-variant matches. Whole lines, as before — going
	// unit-level here would change what a partly-covered multi-unit line costs,
	// and this is a representation fix, not a pricing one.
	uncovered := make([]DesiredLine, 0, len(desired))
	for _, d := range desired {
		size := lineSize(d.Attributes)
		if remaining[d.ProductSKU][size] >= d.Quantity && d.Quantity > 0 {
			remaining[d.ProductSKU][size] -= d.Quantity
			continue
		}
		uncovered = append(uncovered, d)
	}

	// Pass 2: pair each still-uncovered line with a paid unit of another size,
	// in input order so the output does not depend on map iteration.
	out := make([]DesiredLine, 0, len(uncovered)*2)
	for _, d := range uncovered {
		out = append(out, d)
		if d.Quantity <= 0 {
			continue
		}
		size := lineSize(d.Attributes)
		if size == "" {
			// A sizeless SKU has nothing to swap: an uncovered participation
			// seat is a genuinely new seat and is charged.
			continue
		}
		if credit, ok := reclaim(remaining[d.ProductSKU], size, d, sizeOrder[d.ProductSKU]); ok {
			out = append(out, credit)
		}
	}
	return out
}

// reclaim takes d.Quantity paid units of some size other than want out of
// remaining, and returns the credit line that gives them back. It reports false
// when no single other size can cover the line, in which case the caller charges
// full price and nothing is reclaimed.
//
// One size per line, deliberately: splitting a 2-unit line across a reclaimed xl
// and a reclaimed l would produce two credits whose pairing with the charge is no
// longer readable, and real derived lines are one unit per member.
func reclaim(remaining map[string]int, want string, d DesiredLine, catalogue []string) (DesiredLine, bool) {
	for _, size := range orderedSizes(remaining, catalogue) {
		if size == want || remaining[size] < d.Quantity {
			continue
		}
		remaining[size] -= d.Quantity

		// The credit copies the desired line's attributes so it carries any
		// other variant dimension a SKU might grow, and overrides the size with
		// the one being handed back. It borrows the desired line's MemberID:
		// buildLines requires one, and the member whose change caused the
		// reclaim is the most useful answer to "why is this here".
		attrs := make(map[string]any, len(d.Attributes)+1)
		for k, v := range d.Attributes {
			attrs[k] = v
		}
		attrs[sizeAttribute] = size

		return DesiredLine{
			ProductSKU: d.ProductSKU,
			MemberID:   d.MemberID,
			Quantity:   -d.Quantity,
			Attributes: attrs,
		}, true
	}
	return DesiredLine{}, false
}

// defaultLineID generates a stable LineID when a caller didn't provide one.
//
// For derived lines we use "derived:{sku}:{memberId}:{size}" so successive
// SetDerivedLines calls upsert into the same row rather than racking up orphans.
// MemberID is guaranteed non-empty by the buildLines validator, so this is always
// a deterministic, collision-free key.
//
// The size is part of the key because a size change puts two lines for the same
// SKU and member on one order — a credit for the size handed back and a charge
// for the size now wanted — and without it they would collide on one lineId and
// the pair would collapse to whichever came last. The size is omitted for
// sizeless SKUs so that participation lineIds keep the form they have today.
func defaultLineID(origin messages.LineOrigin, d DesiredLine) string {
	if origin == messages.LineOriginDerived {
		if size := lineSize(d.Attributes); size != "" {
			return fmt.Sprintf("derived:%s:%s:%s", d.ProductSKU, d.MemberID, size)
		}
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
