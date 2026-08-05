package crewmember

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

type Queries interface {
	GetByID(context.Context, types.UserID) (*CrewMember, error)
	GetAll(context.Context, Filter) ([]CrewMember, error)
}

type querier struct {
	db cqrs.Reader
	r  *goqu.Database
}

func (q *querier) GetByID(ctx context.Context, id types.UserID) (*CrewMember, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var m CrewMember
	err := q.db.QueryRowContext(ctx,
		`SELECT userId, year, name, phone, email, medlemNr, groupName, corps, diet, additionals, sectionSlug
		 FROM crewmember WHERE userId = ? AND deleted = 0`,
		string(id),
	).Scan(&m.UserID, &m.YearSlug, &m.Name, &m.Phone, &m.Email, &m.MedlemNr, &m.Group, &m.Corps, &m.Diet, &m.Additionals, &m.SectionSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, tables.ErrRecordNotFound
		}
		return nil, err
	}
	return &m, nil
}

func (q *querier) GetAll(ctx context.Context, f Filter) ([]CrewMember, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	where := goqu.Ex{"deleted": 0}
	if f.YearSlug != "" {
		where["year"] = string(f.YearSlug)
	}
	if f.SectionSlug != "" {
		where["sectionSlug"] = string(f.SectionSlug)
	}
	if f.Unassigned {
		where["sectionSlug"] = ""
	}

	members := []CrewMember{}
	err := q.r.From("crewmember").
		Select("userId", "year", "name", "phone", "email", "medlemNr", "groupName", "corps", "diet", "additionals", "sectionSlug").
		Where(where).
		Order(goqu.I("name").Asc()).
		ScanStructsContext(ctx, &members)
	if err != nil {
		return nil, err
	}
	return members, nil
}
