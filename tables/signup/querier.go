package signup

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

type Queries interface {
	GetByID(context.Context, types.TeamID) (*Signup, error)
}

type querier struct {
	db cqrs.Reader
}

func (q querier) GetByID(ctx context.Context, teamID types.TeamID) (*Signup, error) {
	if len(teamID) == 0 {
		return nil, tables.ErrRecordNotFound
	}

	query := `SELECT teamId, year, teamType, name, email, emailPending, phone, phonePending, pincode, createdAt
		FROM signup
		WHERE teamId = ?`
	var p Signup
	err := q.db.QueryRow(query, teamID).Scan(
		&p.TeamID,
		&p.Year,
		&p.TeamType,
		&p.Name,
		&p.Email,
		&p.EmailPending,
		&p.Phone,
		&p.PhonePending,
		&p.Pincode,
		&p.CreatedAt,
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
