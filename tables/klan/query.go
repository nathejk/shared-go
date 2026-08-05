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

/*
	func (q *querier) query(filters Filter, query string, args []any) ([]types.TeamID, Metadata, error) {
		// Create a context with a 3-second timeout.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		rows, err := q.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, Metadata{}, err
		}
		defer rows.Close()

		totalRecords := 0
		teamIDs := []types.TeamID{}
		for rows.Next() {
			var teamID types.TeamID
			if err := rows.Scan(&teamID); err != nil {
				return nil, Metadata{}, err
			}
			teamIDs = append(teamIDs, teamID)
		}
		// When the rows.Next() loop has finished, call rows.Err() to retrieve any error
		// that was encountered during the iteration.
		if err = rows.Err(); err != nil {
			return nil, Metadata{}, err
		}
		metadata := calculateMetadata(filters.YearSlug, totalRecords, filters.Page, filters.PageSize)

		return teamIDs, metadata, nil
	}
*/
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
	query := `SELECT t.teamId, t.name, t.groupName, t.korps, t.signupStatus, t.lok,
			(SELECT COUNT(*) FROM senior s where t.teamId = s.teamId) memberCount,
			(SELECT COALESCE(SUM(pmt.amount), 0)
				FROM payment pmt
				LEFT JOIN orders o ON o.orderId = pmt.orderForeignKey
				WHERE pmt.status IN ('reserved', 'received')
				  AND (pmt.orderForeignKey = t.teamId OR o.ownerId = t.teamId)) as paidAmount
		FROM klan t
		JOIN patruljestatus ts ON t.teamId = ts.teamId AND t.signupStatus != ''
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
		JOIN patruljestatus ts ON t.teamId = ts.teamID
		WHERE t.teamId = ?`
	var t Klan
	err := q.db.QueryRow(query, teamID).Scan(
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

/*
	func (m TeamModel) GetStartedTeamIDs(filters Filters) ([]types.TeamID, Metadata, error) {
		sql := `SELECT teamId FROM patruljestatus WHERE startedUts > 0 AND (LOWER(year) = LOWER(?) OR ? = '')`
		args := []any{filters.Year, filters.Year}
		return m.query(filters, sql, args)
	}

	func (m TeamModel) GetDiscontinuedTeamIDs(filters Filters) ([]types.TeamID, Metadata, error) {
		//sql := "SELECT teamId FROM patruljestatus WHERE startedUts > 0 AND (LOWER(year) = LOWER($1) OR $1 = '')"
		sql := `SELECT DISTINCT m.teamId FROM patruljemerged m JOIN patruljestatus s ON m.teamId = s.teamId WHERE s.startedUts > 0 AND (LOWER(year) = LOWER(?) OR ? = '')`
		args := []any{filters.Year, filters.Year}
		return m.query(filters, sql, args)
	}

	func (m TeamModel) RequestedSeniorCount() int {
		query := `SELECT COUNT(memberId) FROM senior WHERE year=%d`
		var count int
		_ = m.DB.QueryRow(query, 2026).Scan(&count)
		return count
	}

	func (m TeamModel) GetPatruljer(filters Filters) ([]*Patrulje, Metadata, error) {
		// Create a context with a 3-second timeout.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		query := `SELECT p.teamId, p.teamNumber, p.name, p.groupName, p.korps, p.liga, p.memberCount, IF(pm.parentTeamId IS NOT NULL, "JOIN", IF(startedUts > 0, "STARTED",  signupStatus))
			FROM patrulje p
			JOIN patruljestatus ps ON p.teamId = ps.teamID AND (LOWER(p.year) = LOWER(?) OR ? = '')`
		args := []any{filters.Year, filters.Year}
		rows, err := m.DB.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, Metadata{}, err
		}
		defer rows.Close()

		totalRecords := 0
		patruljer := []*Patrulje{}
		for rows.Next() {
			var p Patrulje
			if err := rows.Scan(&p.ID, &p.Number, &p.Name, &p.Group, &p.Korps, &p.Liga, &p.MemberCount, &p.Status); err != nil {
				return nil, Metadata{}, err
			}
			patruljer = append(patruljer, &p)
		}
		// When the rows.Next() loop has finished, call rows.Err() to retrieve any error
		// that was encountered during the iteration.
		if err = rows.Err(); err != nil {
			return nil, Metadata{}, err
		}
		metadata := calculateMetadata(filters.Year, totalRecords, filters.Page, filters.PageSize)

		return patruljer, metadata, nil
	}
func (m TeamModel) GetPatrulje(teamID types.TeamID) (*Patrulje, error) {
	if len(teamID) == 0 {
		return nil, ErrRecordNotFound
	}

	query := `SELECT p.teamId, p.teamNumber, p.name, p.groupName, p.korps, p.liga, p.memberCount
		FROM patrulje p
		JOIN patruljestatus ps ON p.teamId = ps.teamID
		WHERE p.teamId = ?`
	var p Patrulje
	err := m.DB.QueryRow(query, teamID).Scan(
		&p.ID,
		&p.Number,
		&p.Name,
		&p.Group,
		&p.Korps,
		&p.Liga,
		&p.MemberCount,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, ErrRecordNotFound
		default:
			return nil, err
		}
	}
	return &p, nil
}

func (m TeamModel) GetKlan(teamID types.TeamID) (*Klan, error) {
	if len(teamID) == 0 {
		return nil, ErrRecordNotFound
	}

	query := `SELECT t.teamId, t.name, t.groupName, t.korps, t.memberCount, t.signupStatus
		FROM klan t
		JOIN patruljestatus ts ON t.teamId = ts.teamID
		WHERE t.teamId = ?`
	var t Klan
	err := m.DB.QueryRow(query, teamID).Scan(
		&t.ID,
		&t.Name,
		&t.Group,
		&t.Korps,
		&t.MemberCount,
		&t.Status,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, ErrRecordNotFound
		default:
			return nil, err
		}
	}
	return &t, nil
}

func (m TeamModel) GetContact(teamID types.TeamID) (*Contact, error) {
	if len(teamID) == 0 {
		return nil, ErrRecordNotFound
	}

	query := `SELECT p.contactName, p.contactPhone, p.contactEmail, p.contactRole
		FROM patrulje p
		JOIN patruljestatus ps ON p.teamId = ps.teamID
		WHERE p.teamId = ?`
	c := Contact{TeamID: teamID}
	err := m.DB.QueryRow(query, teamID).Scan(
		&c.Name,
		&c.Phone,
		&c.Email,
		&c.Role,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, ErrRecordNotFound
		default:
			return nil, err
		}
	}
	return &c, nil
}
*/
