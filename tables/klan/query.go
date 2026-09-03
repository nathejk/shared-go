package klan

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

type Queries interface {
	RequestedMemberCount(context.Context, types.YearSlug) (uint32, error)
	RequestedSeniorCount(context.Context, types.YearSlug) (int, error)
	GetAll(context.Context, Filter) ([]Klan, error)
	GetByID(context.Context, types.TeamID) (*Klan, error)
}

type querier struct {
	db cqrs.Reader
}

func (q *querier) RequestedMemberCount(ctx context.Context, year types.YearSlug) (uint32, error) {
	// Sourced from the order projection (task 005) so the klan capacity gate
	// and the order commander's checkStock read the same underlying data
	// instead of the parallel klan.reservedMemberCount column. Counts
	// participation.klan seats on non-cancelled orders; deliberately identical
	// in shape to order.querier.ReservedQuantity.
	//
	// Behaviour-preserving: klan.reservedMemberCount was written only on the
	// .reserved event (waitlisted .requested teams set a different column), and
	// those same reserved teams are the ones for which participation.klan order
	// lines are created — so both sums cover the same seats. The one divergence
	// is that a cancelled order frees its seats here (status <> 'cancelled'),
	// which the stale klan column did not; that is the intended direction.
	const query = `
		SELECT COALESCE(SUM(l.quantity), 0)
		FROM order_line l
		JOIN orders o ON o.orderId = l.orderId
		WHERE o.year = ? AND l.productSku = 'participation.klan' AND o.status <> 'cancelled'`
	var count uint32
	if err := q.db.QueryRowContext(ctx, query, year).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// RequestedSeniorCount returns the number of senior rows registered for
// `year`. Used by UpdateMembers to put klans on the waiting list when the
// global senior cap (currently 115) would be exceeded by accepting more.
//
// Cross-table read into the senior projection lives here so the klan
// commander can keep its dependencies on a single Queries interface.
func (q *querier) RequestedSeniorCount(ctx context.Context, year types.YearSlug) (int, error) {
	query := `SELECT COUNT(memberId) FROM senior WHERE year=?`
	var count int
	if err := q.db.QueryRowContext(ctx, query, year).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (q *querier) GetAll(ctx context.Context, f Filter) ([]Klan, error) {
	where := []string{}
	args := []any{}
	if f.YearSlug != "" {
		where = append(where, "t.year = ?")
		args = append(args, f.YearSlug)
	}
	if len(f.TeamIDs) == 1 {
		where = append(where, "t.teamId = ?")
		args = append(args, f.TeamIDs[0])
	}
	if len(f.TeamIDs) > 1 {
		where = append(where, fmt.Sprintf("t.teamId IN (?%s)", strings.Repeat(",?", len(f.TeamIDs)-1)))
		for _, id := range f.TeamIDs {
			args = append(args, id)
		}
	}
	if len(where) == 0 {
		where = []string{"1 = 1"}
	}
	// signupStatus != '' used to ride along in a JOIN on patruljestatus, a table
	// this entity does not own and read no column from. The predicate is klan's
	// own, so it belongs in the WHERE clause.
	where = append(where, "t.signupStatus != ''")
	// Aggregates are joined as pre-grouped derived tables rather than computed as
	// correlated subqueries per row. The correlated form ran a COUNT over `senior`
	// plus a payment sum that itself contained a LEFT JOIN on `orders` once per
	// klan, against tables that carried no index able to serve either — so this was
	// O(klaner × rows). The identical shape in the patrol list crossed its 3-second
	// budget in production and answered 500; this is the same fix, and the keys the
	// plan now needs live in tables/{senior,payment,klan,order}/table.sql.
	//
	// The payment side unions the two ways a payment reaches a team — straight to
	// the teamId (older years) or via an order owned by it — into one keyed set
	// before summing, which is the same set the OR described. UNION rather than
	// UNION ALL so a team whose orderId equals its teamId still contributes its
	// payment once, as the OR did. `orders` is deliberately not filtered by
	// ownerType: the original predicate was `o.ownerId = t.teamId` alone, and rows
	// for other owner types cannot match a klan teamId anyway.
	query := `SELECT t.teamId, t.name, t.groupName, t.korps, t.signupStatus, t.lok,
			COALESCE(m.memberCount, 0) memberCount,
			COALESCE(pay.paidAmount, 0) paidAmount
		FROM klan t
		LEFT JOIN (
			SELECT s.teamId, COUNT(*) memberCount FROM senior s GROUP BY s.teamId
		) m ON m.teamId = t.teamId
		LEFT JOIN (
			SELECT k.teamId, SUM(pmt.amount) paidAmount
			FROM (
				SELECT o.ownerId teamId, o.orderId foreignKey FROM orders o
				UNION
				SELECT k2.teamId, k2.teamId FROM klan k2
			) k
			JOIN payment pmt ON pmt.orderForeignKey = k.foreignKey
				AND pmt.status IN ('reserved', 'received')
			GROUP BY k.teamId
		) pay ON pay.teamId = t.teamId
		WHERE ` + strings.Join(where, " AND ")
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		log.Print(query)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return []Klan{}, nil
		default:
			return nil, err
		}
	}
	defer rows.Close()

	//totalRecords := 0
	klans := []Klan{}
	for rows.Next() {
		var k Klan
		if err := rows.Scan(&k.ID, &k.Name, &k.Group, &k.Korps, &k.Status, &k.Lok, &k.MemberCount, &k.PaidAmount); err != nil {
			//if err := rows.Scan(&klan.TeamID); err != nil {
			return nil, err
		}
		klans = append(klans, k)
	}
	// When the rows.Next() loop has finished, call rows.Err() to retrieve any error
	// that was encountered during the iteration.
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return klans, nil
}

func (q *querier) GetByID(ctx context.Context, teamID types.TeamID) (*Klan, error) {
	if len(teamID) == 0 {
		return nil, tables.ErrRecordNotFound
	}

	query := `SELECT t.teamId, t.year, t.name, t.groupName, t.korps, t.memberCount, t.signupStatus, t.lok
		FROM klan t
		WHERE t.teamId = ?`
	var t Klan
	err := q.db.QueryRowContext(ctx, query, teamID).Scan(
		&t.ID,
		&t.Year,
		&t.Name,
		&t.Group,
		&t.Korps,
		&t.MemberCount,
		&t.Status,
		&t.Lok,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, tables.ErrRecordNotFound
		default:
			return nil, err
		}
	}
	return &t, nil
}
