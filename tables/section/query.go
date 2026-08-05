package section

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
	GetBySlug(context.Context, types.YearSlug, types.Slug) (*Section, error)
	GetAll(context.Context, Filter) ([]Section, error)
	ListYearsWithSections(context.Context) ([]types.YearSlug, error)
	CountChildren(context.Context, types.YearSlug, types.Slug) (int, error)
}

type querier struct {
	db cqrs.Reader
	r  *goqu.Database
}

func (q *querier) GetBySlug(ctx context.Context, year types.YearSlug, slug types.Slug) (*Section, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var s Section
	err := q.db.QueryRowContext(ctx,
		`SELECT slug, year, parentSlug, label, sortOrder FROM section WHERE year = ? AND slug = ?`,
		string(year), string(slug),
	).Scan(&s.Slug, &s.YearSlug, &s.ParentSlug, &s.Label, &s.SortOrder)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, tables.ErrRecordNotFound
		}
		return nil, err
	}
	return &s, nil
}

func (q *querier) GetAll(ctx context.Context, f Filter) ([]Section, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	where := goqu.Ex{}
	if f.YearSlug != "" {
		where["year"] = string(f.YearSlug)
	}
	if f.Slug != "" {
		where["slug"] = string(f.Slug)
	}

	sections := []Section{}
	err := q.r.From("section").
		Select("slug", "year", "parentSlug", "label", "sortOrder").
		Where(where).
		Order(goqu.I("parentSlug").Asc(), goqu.I("sortOrder").Asc(), goqu.I("label").Asc()).
		ScanStructsContext(ctx, &sections)
	if err != nil {
		return nil, err
	}
	return sections, nil
}

// ListYearsWithSections returns each YearSlug that currently has at least one
// section. Used by the frontend to power the "copy from year" flow.
func (q *querier) ListYearsWithSections(ctx context.Context) ([]types.YearSlug, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	rows, err := q.db.QueryContext(ctx, `SELECT DISTINCT year FROM section ORDER BY year DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	years := []types.YearSlug{}
	for rows.Next() {
		var y types.YearSlug
		if err := rows.Scan(&y); err != nil {
			return nil, err
		}
		years = append(years, y)
	}
	return years, rows.Err()
}

// CountChildren returns the number of direct child sections of a given section.
func (q *querier) CountChildren(ctx context.Context, year types.YearSlug, parent types.Slug) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var n int
	err := q.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM section WHERE year = ? AND parentSlug = ?`,
		string(year), string(parent),
	).Scan(&n)
	return n, err
}
