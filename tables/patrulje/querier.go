package patrulje

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

type querier struct {
	db cqrs.Reader
}

func (q *querier) GetAll(ctx context.Context, filters Filter) ([]*Patrulje, error) {
	// Create a context with a 3-second timeout.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `SELECT p.teamId, teamNumber, name, groupName, korps, liga, contactName, contactPhone, contactEmail, contactRole, signupStatus,
			(SELECT COUNT(*) FROM spejder s where p.teamId = s.teamId) memberCount,
			(SELECT COUNT(*) FROM spejder s where p.teamId = s.teamId AND s.tshirtSize != '') tshirtCount,
			(SELECT COALESCE(SUM(pmt.amount), 0)
				FROM payment pmt
				LEFT JOIN orders o ON o.orderId = pmt.orderForeignKey
				WHERE pmt.status IN ('reserved', 'received')
				  AND (pmt.orderForeignKey = p.teamId OR o.ownerId = p.teamId)) as paidAmount
		FROM patrulje p
		WHERE (LOWER(p.year) = LOWER(?) OR ? = '')`
	args := []any{filters.YearSlug, filters.YearSlug}
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	//totalRecords := 0
	patruljer := []*Patrulje{}
	for rows.Next() {
		var p Patrulje
		if err := rows.Scan(&p.TeamID, &p.TeamNumber, &p.Name, &p.Group, &p.Korps, &p.Liga, &p.ContactName, &p.ContactPhone, &p.ContactEmail, &p.ContactRole, &p.SignupStatus, &p.MemberCount, &p.TshirtCount, &p.PaidAmount); err != nil {
			return nil, err
		}
		payableAmount := p.TshirtCount*175 + p.MemberCount*250
		if p.SignupStatus != "" {
		} else if p.PaidAmount == 0 {
			p.SignupStatus = types.SignupStatusPay
		} else if p.PaidAmount >= payableAmount {
			p.SignupStatus = types.SignupStatusPaid
		} else {
			p.SignupStatus = types.SignupStatusSemipaid
		}
		patruljer = append(patruljer, &p)
	}
	// When the rows.Next() loop has finished, call rows.Err() to retrieve any error
	// that was encountered during the iteration.
	if err = rows.Err(); err != nil {
		return nil, err
	}
	//metadata := calculateMetadata(filters.Year, totalRecords, filters.Page, filters.PageSize)

	return patruljer, nil
}

// GetLastWithNumber returns the patrulje currently holding the highest
// teamNumber, or tables.ErrRecordNotFound if no patrulje has been numbered
// yet. Used by AssignNumber to compute the next number.
func (q *querier) GetLastWithNumber(ctx context.Context) (*Patrulje, error) {
	query := `SELECT teamId, teamNumber FROM patrulje WHERE teamNumber != "" ORDER BY length(teamNumber) DESC, teamNumber DESC LIMIT 1`
	var p Patrulje
	err := q.db.QueryRowContext(ctx, query).Scan(&p.TeamID, &p.TeamNumber)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, tables.ErrRecordNotFound
		default:
			return nil, err
		}
	}
	return &p, nil
}

func (q *querier) GetByID(ctx context.Context, teamID types.TeamID) (*Patrulje, error) {
	if len(teamID) == 0 {
		return nil, tables.ErrRecordNotFound
	}

	// No join on patruljestatus: that table is no longer maintained, and joining
	// it silently hid every patrulje without a status row.
	query := `SELECT p.teamId, p.teamNumber, p.name, p.groupName, p.korps, p.liga, p.memberCount,
			p.contactName, p.contactPhone, p.contactEmail, p.contactRole
		FROM patrulje p
		WHERE p.teamId = ?`
	var p Patrulje
	err := q.db.QueryRowContext(ctx, query, teamID).Scan(
		&p.TeamID,
		&p.TeamNumber,
		&p.Name,
		&p.Group,
		&p.Korps,
		&p.Liga,
		&p.MemberCount,
		&p.ContactName,
		&p.ContactPhone,
		&p.ContactEmail,
		&p.ContactRole,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, tables.ErrRecordNotFound
		default:
			return nil, err
		}
	}
	return &p, nil
}
