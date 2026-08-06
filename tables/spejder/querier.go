package spejder

import (
	"context"
	"log"
	"time"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/types"
)

type querier struct {
	db cqrs.Reader
}

func (q querier) GetByID(c context.Context, memberID types.MemberID) (*Spejder, error) {
	return nil, nil
}

func (q querier) GetAll(c context.Context, filters Filter) ([]*Spejder, Metadata, error) {
	// Create a context with a 3-second timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	query := `Select
  s.memberId,
  s.teamId,
  IFNULL(ss.status, 'paid') AS status,
  name,
  address,
  postalCode,
  city,
  email,
  phone,
  phoneParent,
  birthday,
  ` + "`returning`" + `,
  tshirtsize
from spejder s
left join spejderstatus ss on s.memberId = ss.id and s.year = ss.year
WHERE  (LOWER(s.year) = LOWER(?) OR ? = '') AND  (s.teamId = ? OR ? = '')`
	args := []any{filters.YearSlug, filters.YearSlug, filters.TeamID, filters.TeamID}
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		log.Print(err)
		return nil, Metadata{}, err
	}
	defer rows.Close()

	totalRecords := 0
	spejdere := []*Spejder{}
	for rows.Next() {
		var s Spejder
		if err := rows.Scan(&s.ID, &s.InitialTeamID, &s.Status, &s.Name, &s.Address, &s.PostalCode, &s.City, &s.Email, &s.Phone, &s.PhoneParent, &s.Birthday, &s.Returning, &s.TShirtSize); err != nil {
			log.Print(err)
			return nil, Metadata{}, err
		}
		s.MemberID = s.ID
		spejdere = append(spejdere, &s)
	}
	// When the rows.Next() loop has finished, call rows.Err() to retrieve any error
	// that was encountered during the iteration.
	if err = rows.Err(); err != nil {
		return nil, Metadata{}, err
	}
	metadata := calculateMetadata(filters.YearSlug, totalRecords, filters.Page, filters.PageSize)

	return spejdere, metadata, nil
}

type SpejderStatus struct {
	MemberID  types.MemberID
	TeamID    types.TeamID
	Status    types.MemberStatus
	Name      string
	TeamName  string
	UpdatedAt time.Time
}

func (q querier) GetInactive(filters Filter) ([]*SpejderStatus, Metadata, error) {
	// Create a context with a 3-second timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	query := `
	select s.memberId, s.name, s.teamId, p.name, ss.status, ss.updatedAt
from spejder s
join patrulje p on s.teamId = p.teamId
join spejderstatus ss on s.memberId = ss.id and s.year = ss.year
WHERE (LOWER(s.year) = LOWER(?) OR ? = '')`

	args := []any{filters.YearSlug, filters.YearSlug, filters.TeamID, filters.TeamID}
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		log.Print(err)
		return nil, Metadata{}, err
	}
	defer rows.Close()

	totalRecords := 0
	sss := []*SpejderStatus{}
	for rows.Next() {
		var s SpejderStatus
		if err := rows.Scan(&s.MemberID, &s.Name, &s.TeamID, &s.TeamName, &s.Status, &s.UpdatedAt); err != nil {
			return nil, Metadata{}, err
		}
		sss = append(sss, &s)
	}
	// When the rows.Next() loop has finished, call rows.Err() to retrieve any error
	// that was encountered during the iteration.
	if err = rows.Err(); err != nil {
		return nil, Metadata{}, err
	}
	metadata := calculateMetadata(filters.YearSlug, totalRecords, filters.Page, filters.PageSize)

	return sss, metadata, nil
}
