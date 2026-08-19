package order

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

// Queries is the read-only API of the order projection.
type Queries interface {
	// GetByID returns the order with its lines and computed paid/due amounts.
	GetByID(ctx context.Context, orderID string) (*Order, error)

	// FindOpenOrder returns the (lowest createdAt) open order for the given
	// owner if any, else (nil, ErrRecordNotFound). Used by EnsureOpenOrder
	// to implement the "one open order per owner" UX rule.
	FindOpenOrder(ctx context.Context, year types.YearSlug, ownerType types.TeamType, ownerID string) (*Order, error)

	// ListByOwner returns every order for the given owner, newest first.
	ListByOwner(ctx context.Context, year types.YearSlug, ownerType types.TeamType, ownerID string) ([]Order, error)

	// ListByYear returns every order for the given year, newest first, with line
	// items hydrated via a single grouped query (no per-order N+1). Paid/due
	// amounts are computed. Used by the year-wide Ordrehistorik list and its
	// line-item summary.
	ListByYear(ctx context.Context, year types.YearSlug) ([]Order, error)

	// ReservedQuantity returns the total quantity of the given product
	// currently sitting on non-cancelled order lines for the given year.
	// Used by the commander to compute "remaining stock" before adding /
	// changing a derived or manual line.
	ReservedQuantity(ctx context.Context, year types.YearSlug, productSKU string) (int, error)

	// PaidQuantityBySKU returns the total quantity per productSKU that already
	// appears on a paid order for the given owner in the given year. Used by
	// SetDerivedLines to bill by unit count: a team pays for a number of
	// participation seats and t-shirts, and the people occupying those units
	// may change, so the open order should only charge for units beyond the
	// paid count — not re-charge whenever a specific member changes.
	//
	// Cancelled and open orders are deliberately excluded; the contract is
	// "paid".
	//
	// Counts units per SKU regardless of size. Use PaidQuantityByVariant when
	// the size matters — which it does for anything that has to be shipped.
	PaidQuantityBySKU(ctx context.Context, year types.YearSlug, ownerType types.TeamType, ownerID string) (map[string]int, error)

	// PaidQuantityByVariant is PaidQuantityBySKU keyed by (SKU, size) instead of
	// by SKU alone, so a paid t-shirt is counted as the size it was actually
	// bought in.
	//
	// This is what lets a post-payment size change stay free and still be
	// visible: knowing only that one adult t-shirt is paid for, the offset
	// cancels the new size against the old one and the order ends up saying
	// nothing about what to ship. Knowing it was an xxl, the offset can reclaim
	// that xxl and charge nothing for the 3xl replacing it.
	//
	// Sizes are normalised (trimmed, lowercased); lines with no size attribute
	// count under the "" size.
	PaidQuantityByVariant(ctx context.Context, year types.YearSlug, ownerType types.TeamType, ownerID string) (map[VariantKey]int, error)

	// ShippableByVariant returns the net quantity per (SKU, size) the owner is to
	// receive: what fulfillment packs.
	//
	// This is the read that makes the order authoritative for size (see the
	// package doc). Net, and across every non-cancelled order — both properties
	// are load-bearing:
	//
	//   - Net, because a size change is recorded as a pair of lines (-1 of the
	//     size handed back, +1 of the size now wanted). Summing only positives
	//     here would pack both shirts.
	//   - Open orders included, because a free size change lives on an open order
	//     with nothing due. Counting paid orders only would pack the old size.
	//
	// A consequence worth knowing: a shirt that is genuinely unpaid — an
	// (N+1)-th one sitting on an open order awaiting payment — is included too.
	// The question this answers is "which shirts does this owner's order say they
	// get", not "which shirts have been paid for"; cross-reference DueAmount if
	// the difference matters to the caller.
	//
	// Variants that net to zero are omitted rather than returned as 0.
	ShippableByVariant(ctx context.Context, year types.YearSlug, ownerType types.TeamType, ownerID string) (map[VariantKey]int, error)
}

type querier struct {
	db cqrs.Reader
}

// orderColumns is the column list reused by GetByID / FindOpenOrder /
// ListByOwner / ListByYear. paidAmount is computed via subquery against the
// payment table; status filters keep it consistent with payment.AmountPaid.
// Both sides are in the currency's minor unit, so the sums are comparable
// without conversion.
const orderColumns = `o.orderId, o.year, o.ownerType, o.ownerId, o.status, o.currency, o.totalAmount,
	COALESCE((SELECT SUM(p.amount) FROM payment p WHERE p.orderForeignKey = o.orderId AND p.status IN ('reserved', 'received')), 0) AS paidAmount,
	o.cancelReason, o.createdAt, o.changedAt`

func (q *querier) GetByID(ctx context.Context, orderID string) (*Order, error) {
	if orderID == "" {
		return nil, tables.ErrRecordNotFound
	}
	row := q.db.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM orders o WHERE o.orderId = ?`, orderID)
	o, err := scanOrder(row)
	if err != nil {
		return nil, err
	}
	lines, err := q.listLines(ctx, orderID)
	if err != nil {
		return nil, err
	}
	o.Lines = lines
	return o, nil
}

func (q *querier) FindOpenOrder(ctx context.Context, year types.YearSlug, ownerType types.TeamType, ownerID string) (*Order, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT `+orderColumns+`
			FROM orders o
			WHERE o.year = ? AND o.ownerType = ? AND o.ownerId = ? AND o.status = 'open'
			ORDER BY o.createdAt ASC
			LIMIT 1`,
		year, string(ownerType), ownerID)
	o, err := scanOrder(row)
	if err != nil {
		return nil, err
	}
	lines, err := q.listLines(ctx, o.OrderID)
	if err != nil {
		return nil, err
	}
	o.Lines = lines
	return o, nil
}

// hasLines excludes orders with nothing on them from the list queries.
//
// EnsureOpenOrder creates an empty open order as soon as somebody opens a shop
// page, so without this every visitor leaves a blank row in their own order
// history. Keyed on the absence of lines rather than on totalAmount = 0, which
// would also have hidden a genuinely free order — a zero-priced or fully
// discounted product is still something the owner ordered.
const hasLines = `EXISTS (SELECT 1 FROM order_line l WHERE l.orderId = o.orderId)`

func (q *querier) ListByOwner(ctx context.Context, year types.YearSlug, ownerType types.TeamType, ownerID string) ([]Order, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT `+orderColumns+`
			FROM orders o
			WHERE o.year = ? AND o.ownerType = ? AND o.ownerId = ?
			  AND `+hasLines+`
			ORDER BY o.createdAt DESC`,
		year, string(ownerType), ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, *o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Hydrate lines in one grouped query rather than one per order.
	linesByOrder, err := q.linesForOwner(ctx, year, ownerType, ownerID)
	if err != nil {
		return nil, err
	}
	for i := range orders {
		orders[i].Lines = linesByOrder[orders[i].OrderID]
	}
	return orders, nil
}

func (q *querier) ListByYear(ctx context.Context, year types.YearSlug) ([]Order, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT `+orderColumns+`
			FROM orders o
			WHERE o.year = ?
			  AND `+hasLines+`
			ORDER BY o.createdAt DESC`,
		year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, *o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Hydrate lines for the whole year in one grouped query — the year-wide list
	// feeds a line-item summary, and a per-order fetch here would be N+1.
	linesByOrder, err := q.linesForYear(ctx, year)
	if err != nil {
		return nil, err
	}
	for i := range orders {
		orders[i].Lines = linesByOrder[orders[i].OrderID]
	}
	return orders, nil
}

// groupedLineColumns is listLines' column list prefixed with the order id, for
// the queries that hydrate many orders at once.
const groupedLineColumns = `l.orderId, l.lineId, l.productSku, l.productName, l.memberId,
	l.unitPrice, l.quantity, l.lineTotal, l.origin, l.attributes`

// linesForYear returns every order line belonging to the given year, grouped by
// orderId, in a single query. Used by ListByYear so hydrating a whole year of
// orders costs one extra round-trip instead of one per order.
func (q *querier) linesForYear(ctx context.Context, year types.YearSlug) (map[string][]Line, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT `+groupedLineColumns+`
			FROM order_line l
			JOIN orders o ON o.orderId = l.orderId
			WHERE o.year = ?
			ORDER BY l.createdAt ASC, l.lineId ASC`,
		year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLinesByOrder(rows, "order.linesForYear")
}

// linesForOwner is linesForYear narrowed to one owner, for ListByOwner.
func (q *querier) linesForOwner(ctx context.Context, year types.YearSlug, ownerType types.TeamType, ownerID string) (map[string][]Line, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT `+groupedLineColumns+`
			FROM order_line l
			JOIN orders o ON o.orderId = l.orderId
			WHERE o.year = ? AND o.ownerType = ? AND o.ownerId = ?
			ORDER BY l.createdAt ASC, l.lineId ASC`,
		year, string(ownerType), ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLinesByOrder(rows, "order.linesForOwner")
}

// scanLinesByOrder scans groupedLineColumns rows into a map keyed by order id.
//
// Shared by the grouped hydration queries so their scanning — and their
// tolerance for malformed attributes — cannot drift from listLines'. caller
// names the log line, since a bad row is worth knowing which query found it.
func scanLinesByOrder(rows *sql.Rows, caller string) (map[string][]Line, error) {
	byOrder := map[string][]Line{}
	for rows.Next() {
		var (
			orderID  string
			l        Line
			attrJSON sql.NullString
		)
		if err := rows.Scan(&orderID, &l.LineID, &l.ProductSKU, &l.ProductName, &l.MemberID, &l.UnitPrice, &l.Quantity, &l.LineTotal, &l.Origin, &attrJSON); err != nil {
			return nil, err
		}
		if attrJSON.Valid && strings.TrimSpace(attrJSON.String) != "" {
			if err := json.Unmarshal([]byte(attrJSON.String), &l.Attributes); err != nil {
				// Tolerate malformed attributes JSON, same as listLines.
				log.Printf("%s: skipping bad attributes for %s/%s: %v", caller, orderID, l.LineID, err)
				l.Attributes = nil
			}
		}
		byOrder[orderID] = append(byOrder[orderID], l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return byOrder, nil
}

// ReservedQuantity — see Queries.ReservedQuantity.
//
// Positive quantities only. Derived credit lines carry a negative quantity to
// reclaim a paid unit of another size (see ApplyPaidOffset), and letting those
// net against the total would hand out stock that nothing has released: the
// reclaimed unit is a specific size returning to a per-SKU counter that cannot
// express sizes. Since a credit is always paired with a charge for the same SKU,
// positive-only and net accounting agree on the SKU total anyway — they differ
// only in the window where a credit exists without its pair, which is a state
// this package never produces.
func (q *querier) ReservedQuantity(ctx context.Context, year types.YearSlug, productSKU string) (int, error) {
	var qty sql.NullInt64
	err := q.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(CASE WHEN l.quantity > 0 THEN l.quantity ELSE 0 END), 0)
			FROM order_line l
			JOIN orders o ON o.orderId = l.orderId
			WHERE o.year = ? AND l.productSku = ? AND o.status <> 'cancelled'`,
		year, productSKU).Scan(&qty)
	if err != nil {
		return 0, err
	}
	return int(qty.Int64), nil
}

// PaidQuantityByVariant — see Queries.PaidQuantityByVariant.
//
// Aggregated in Go rather than with JSON_EXTRACT and a GROUP BY: attributes is a
// text column, not a native json one, and may hold NULL or ” as well as JSON,
// so the extraction has to tolerate all three. Doing it in Go also means the
// size is read by exactly the same code that reads it everywhere else — lineSize
// — rather than by a second, SQL-shaped definition free to drift from it.
func (q *querier) PaidQuantityByVariant(ctx context.Context, year types.YearSlug, ownerType types.TeamType, ownerID string) (map[VariantKey]int, error) {
	return q.quantityByVariant(ctx, year, ownerType, ownerID, "o.status = 'paid'", false)
}

// ShippableByVariant — see Queries.ShippableByVariant.
func (q *querier) ShippableByVariant(ctx context.Context, year types.YearSlug, ownerType types.TeamType, ownerID string) (map[VariantKey]int, error) {
	return q.quantityByVariant(ctx, year, ownerType, ownerID, "o.status <> 'cancelled'", true)
}

// quantityByVariant sums an owner's order lines per (SKU, size).
//
// statusFilter is interpolated, which is safe because both call sites pass a
// literal; dropZero omits variants that net to nothing, which only the shippable
// read wants — a paid count of zero and an absent paid count mean the same thing
// to the offset, whereas a netted-out variant is meaningfully "pack none of
// these".
func (q *querier) quantityByVariant(ctx context.Context, year types.YearSlug, ownerType types.TeamType, ownerID, statusFilter string, dropZero bool) (map[VariantKey]int, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	rows, err := q.db.QueryContext(ctx,
		`SELECT l.productSku, l.quantity, l.attributes
			FROM order_line l
			JOIN orders o ON o.orderId = l.orderId
			WHERE o.year = ? AND o.ownerType = ? AND o.ownerId = ? AND `+statusFilter,
		year, string(ownerType), ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	qtys := map[VariantKey]int{}
	for rows.Next() {
		var (
			sku      string
			qty      int
			attrJSON sql.NullString
		)
		if err := rows.Scan(&sku, &qty, &attrJSON); err != nil {
			return nil, err
		}
		var attrs map[string]any
		if attrJSON.Valid && strings.TrimSpace(attrJSON.String) != "" {
			if err := json.Unmarshal([]byte(attrJSON.String), &attrs); err != nil {
				// Same tolerance as listLines: a line with unreadable
				// attributes still counts as a unit, just a sizeless one.
				// Dropping it would re-charge a shirt somebody already bought.
				log.Printf("order.quantityByVariant: bad attributes on a %s line for %s: %v", sku, ownerID, err)
				attrs = nil
			}
		}
		qtys[VariantKey{SKU: sku, Size: lineSize(attrs)}] += qty
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if dropZero {
		for k, n := range qtys {
			if n == 0 {
				delete(qtys, k)
			}
		}
	}
	return qtys, nil
}

// PaidQuantityBySKU — see Queries.PaidQuantityBySKU.
//
// Sums order_line.quantity per productSKU across the owner's paid orders.
// This is the "paid units" (seats / t-shirts) count the open order bills
// against; individual memberIds are irrelevant because a paid unit is
// fungible — the member occupying a seat (or the size of a t-shirt) may
// change without re-charging.
func (q *querier) PaidQuantityBySKU(ctx context.Context, year types.YearSlug, ownerType types.TeamType, ownerID string) (map[string]int, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT l.productSku, COALESCE(SUM(l.quantity), 0)
			FROM order_line l
			JOIN orders o ON o.orderId = l.orderId
			WHERE o.year = ? AND o.ownerType = ? AND o.ownerId = ? AND o.status = 'paid'
			GROUP BY l.productSku`,
		year, string(ownerType), ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	qtys := map[string]int{}
	for rows.Next() {
		var sku string
		var qty int
		if err := rows.Scan(&sku, &qty); err != nil {
			return nil, err
		}
		qtys[sku] = qty
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return qtys, nil
}

// scanRow is the small intersection of *sql.Row and *sql.Rows that scanOrder
// needs, so we can share the column list between single-row and multi-row
// queries.
type scanRow interface {
	Scan(dest ...any) error
}

func scanOrder(r scanRow) (*Order, error) {
	var (
		o            Order
		ownerType    string
		status       string
		cancelReason sql.NullString
	)
	err := r.Scan(
		&o.OrderID, &o.Year, &ownerType, &o.OwnerID, &status, &o.Currency, &o.TotalAmount,
		&o.PaidAmount, &cancelReason, &o.CreatedAt, &o.ChangedAt,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, tables.ErrRecordNotFound
		default:
			return nil, err
		}
	}
	o.OwnerType = types.TeamType(ownerType)
	o.Status = Status(status)
	o.CancelReason = cancelReason.String

	// Clamp PaidAmount and DueAmount on terminal orders so the wire
	// shape stays coherent regardless of payment-table drift.
	//
	// Background: PaidAmount is computed from a JOIN against the payment
	// table filtered to status IN ('reserved','received'). For an order
	// that's already in StatusPaid (the saga has fired) this *should*
	// equal TotalAmount, but the two can drift apart for a few reasons:
	//
	//   - manual `UPDATE orders SET status='paid'` for testing without
	//     matching payment rows;
	//   - a payment row later changes status (refund, MobilePay
	//     cancellation) so the JOIN no longer counts it;
	//   - a payment row gets deleted.
	//
	// The order layer's contract is "paid means paid", so we trust
	// Status as the source of truth and report PaidAmount=TotalAmount /
	// DueAmount=0 to consumers. Investigating drift is a separate
	// concern; the read API never lies to its callers.
	switch o.Status {
	case StatusPaid:
		o.PaidAmount = o.TotalAmount
		o.DueAmount = 0
	default:
		// DueAmount can go negative here if a payment overpays the order
		// (legacy data, or a future partial-refund flow). That is tolerated
		// deliberately: there is no refund flow today, so the order model does
		// not represent credit/refund state, and the frontend clamps the
		// display with max(0, due). Modelling overpay/refund properly is
		// deferred until refunds become a real flow — see task 003 for the
		// decision and the sketch to revisit then.
		o.DueAmount = o.TotalAmount - o.PaidAmount
	}
	return &o, nil
}

func (q *querier) listLines(ctx context.Context, orderID string) ([]Line, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT lineId, productSku, productName, memberId, unitPrice, quantity, lineTotal, origin, attributes
			FROM order_line
			WHERE orderId = ?
			ORDER BY createdAt ASC, lineId ASC`,
		orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lines []Line
	for rows.Next() {
		var (
			l        Line
			attrJSON sql.NullString
		)
		if err := rows.Scan(&l.LineID, &l.ProductSKU, &l.ProductName, &l.MemberID, &l.UnitPrice, &l.Quantity, &l.LineTotal, &l.Origin, &attrJSON); err != nil {
			return nil, err
		}
		if attrJSON.Valid && strings.TrimSpace(attrJSON.String) != "" {
			if err := json.Unmarshal([]byte(attrJSON.String), &l.Attributes); err != nil {
				// Tolerate malformed attributes JSON: log and continue with
				// empty attributes for this line. The line itself is still
				// useful (productName, quantity, lineTotal, etc. are valid),
				// and a single bad row shouldn't poison the whole order.
				// Re-saving the order overwrites the row and clears the
				// problem.
				log.Printf("order.listLines: skipping bad attributes for %s/%s: %v", orderID, l.LineID, err)
				l.Attributes = nil
			}
		}
		lines = append(lines, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}
