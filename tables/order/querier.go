package order

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strings"

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
	PaidQuantityBySKU(ctx context.Context, year types.YearSlug, ownerType types.TeamType, ownerID string) (map[string]int, error)
}

type querier struct {
	db cqrs.Reader
}

// orderColumns is the column list reused by GetByID / FindOpenOrder /
// ListByOwner. paidAmount is computed via subquery against the payment
// table; status filters keep it consistent with payment.AmountPaidByTeamID.
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

func (q *querier) ListByOwner(ctx context.Context, year types.YearSlug, ownerType types.TeamType, ownerID string) ([]Order, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT `+orderColumns+`
			FROM orders o
			WHERE o.year = ? AND o.ownerType = ? AND o.ownerId = ?
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
	// Hydrate lines per order. Cheap for the small N this app deals with;
	// can be denormalised into a single GROUP_CONCAT query later if needed.
	for i := range orders {
		lines, err := q.listLines(ctx, orders[i].OrderID)
		if err != nil {
			return nil, err
		}
		orders[i].Lines = lines
	}
	return orders, nil
}

func (q *querier) ReservedQuantity(ctx context.Context, year types.YearSlug, productSKU string) (int, error) {
	var qty sql.NullInt64
	err := q.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(l.quantity), 0)
			FROM order_line l
			JOIN orders o ON o.orderId = l.orderId
			WHERE o.year = ? AND l.productSku = ? AND o.status <> 'cancelled'`,
		year, productSKU).Scan(&qty)
	if err != nil {
		return 0, err
	}
	return int(qty.Int64), nil
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
