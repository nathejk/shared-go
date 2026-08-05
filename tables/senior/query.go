package senior

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

type querier struct {
	db cqrs.Reader
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
func (q *querier) GetAll(ctx context.Context, f Filter) ([]*Senior, error) {
	where := []string{}
	args := []any{}
	if f.YearSlug != "" {
		where = append(where, "s.year = ?")
		args = append(args, f.YearSlug)
	}
	if len(f.TeamIDs) == 1 {
		where = append(where, "s.teamId = ?")
		args = append(args, f.TeamIDs[0])
	}
	if len(f.TeamIDs) > 1 {
		where = append(where, fmt.Sprintf("s.teamId IN (?%s)", strings.Repeat(",?", len(f.TeamIDs)-1)))
		for _, id := range f.TeamIDs {
			args = append(args, id)
		}
	}
	if f.Lok > 0 {
		where = append(where, "k.lok = ?")
		args = append(args, f.Lok)
	}
	if len(where) == 0 {
		where = []string{"1 = 1"}
	}
	query := `SELECT s.memberId, s.teamId, s.year, s.armNumber, s.name, s.address, s.postalCode, s.city, s.email, s.phone, s.birthday, s.tshirtSize, s.diet
		FROM senior s JOIN klan k ON s.teamId = k.teamId
		WHERE ` + strings.Join(where, " AND ") + ` ORDER BY teamId`

	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return []*Senior{}, nil
		default:
			return nil, err
		}
	}
	defer rows.Close()

	//totalRecords := 0
	seniors := []*Senior{}
	for rows.Next() {
		var s Senior
		if err := rows.Scan(&s.MemberID, &s.TeamID, &s.YearSlug, &s.ArmNumber, &s.Name, &s.Address, &s.PostalCode, &s.City, &s.Email, &s.Phone, &s.Birthday, &s.TshirtSize, &s.Diet); err != nil {
			return nil, err
		}
		seniors = append(seniors, &s)
	}
	// When the rows.Next() loop has finished, call rows.Err() to retrieve any error
	// that was encountered during the iteration.
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return seniors, nil
}

func (q *querier) GetByID(ctx context.Context, memberID types.MemberID) (*Senior, error) {
	if len(memberID) == 0 {
		return nil, tables.ErrRecordNotFound
	}

	query := `SELECT s.memberId, s.teamId, s.year, s.armNumber, s.name, s.address, s.postalCode, s.city, s.email, s,phone, s.birthday, s.tshirtSize, d.diet
		FROM senior s
		WHERE s.memberId = ?`
	var s Senior
	err := q.db.QueryRow(query, memberID).Scan(&s.MemberID, &s.TeamID, &s.YearSlug, &s.ArmNumber, &s.Name, &s.Address, &s.PostalCode, &s.City, &s.Email, &s.Phone, &s.Birthday, &s.TshirtSize, &s.Diet)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, tables.ErrRecordNotFound
		default:
			return nil, err
		}
	}
	return &s, nil
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
