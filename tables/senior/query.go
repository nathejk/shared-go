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
