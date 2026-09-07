package order

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/nathejk/shared-go/types"
)

// MemberLine is one order line together with the order it sits on.
//
// The order id travels with the line because a caller moving a member's paid
// seat needs to know which paid order funded it — that is where the payment, and
// therefore the provenance of the money, is found.
type MemberLine struct {
	OrderID string
	Line
}

// MemberLineReader reads the lines an owner has already paid for on behalf of one
// member.
//
// A narrow interface rather than another method on Queries, which is implemented
// by fakes in other repos and by this package's own tests.
type MemberLineReader interface {
	// PaidLinesByMember returns every line attributed to memberID on a paid order
	// owned by (ownerType, ownerID) in the given year, oldest order first.
	PaidLinesByMember(ctx context.Context, year types.YearSlug, ownerType types.TeamType, ownerID string, memberID string) ([]MemberLine, error)
}

// PaidLinesByMember — see MemberLineReader.PaidLinesByMember.
//
// Paid orders only, which is the whole point: an unpaid line is not a seat anybody
// has bought, so there is nothing to move.
//
// Every line is returned, not just the participation one, and at the unitPrice
// snapshotted on the line rather than today's catalogue price. Merchandise moves
// with its owner (a t-shirt was bought for a person, not for a team), and a
// catalogue price that has changed since must not silently re-price a seat
// somebody already paid for.
//
// Credit lines — negative quantities, produced by a paid-unit offset for a size
// change — are included as they are. They come in zero-sum pairs, so copying the
// whole set preserves both the money (the pair costs nothing) and the shipping
// answer (which size the owner is actually owed).
//
// Ordered deterministically by (orderId, lineId) so a caller deriving stable ids
// from the result gets the same ids every time it runs.
func (q *querier) PaidLinesByMember(ctx context.Context, year types.YearSlug, ownerType types.TeamType, ownerID string, memberID string) ([]MemberLine, error) {
	if memberID == "" || ownerID == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	rows, err := q.db.QueryContext(ctx,
		`SELECT l.orderId, l.lineId, l.productSku, l.productName, l.memberId,
			l.unitPrice, l.quantity, l.lineTotal, l.origin, l.attributes
			FROM order_line l
			JOIN orders o ON o.orderId = l.orderId
			WHERE o.year = ? AND o.ownerType = ? AND o.ownerId = ? AND o.status = 'paid'
			  AND l.memberId = ?
			ORDER BY l.orderId ASC, l.lineId ASC`,
		year, string(ownerType), ownerID, memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MemberLine
	for rows.Next() {
		var (
			ml       MemberLine
			attrJSON sql.NullString
		)
		if err := rows.Scan(&ml.OrderID, &ml.LineID, &ml.ProductSKU, &ml.ProductName, &ml.MemberID,
			&ml.UnitPrice, &ml.Quantity, &ml.LineTotal, &ml.Origin, &attrJSON); err != nil {
			return nil, err
		}
		if attrJSON.Valid && strings.TrimSpace(attrJSON.String) != "" {
			if err := json.Unmarshal([]byte(attrJSON.String), &ml.Attributes); err != nil {
				// Same tolerance as listLines. Note the cost here is higher than
				// elsewhere: attributes carry the t-shirt size, and the order is
				// authoritative for shipping, so a line whose attributes cannot be
				// read moves without its size. Logged rather than dropped, because
				// dropping it would lose the seat as well.
				log.Printf("order.PaidLinesByMember: bad attributes on %s/%s: %v", ml.OrderID, ml.LineID, err)
				ml.Attributes = nil
			}
		}
		out = append(out, ml)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

var _ MemberLineReader = (*querier)(nil)
