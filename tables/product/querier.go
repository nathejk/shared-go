package product

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

// Queries is the read-only API of the product catalogue. Other packages
// (notably the order commander) take this interface as a dependency rather
// than the concrete type so that tests can substitute it.
type Queries interface {
	GetBySKU(ctx context.Context, year types.YearSlug, sku string) (*Product, error)
	ListEligibleFor(ctx context.Context, year types.YearSlug, ownerType types.TeamType) ([]Product, error)
}

type querier struct {
	db cqrs.Reader
}

// GetBySKU returns the product with the given (sku, year). Inactive products
// are still returned — callers that need to enforce "buyable" should check
// Product.Active themselves; the order commander does this so that
// retiring a product doesn't break already-issued orders that still
// reference it.
func (q *querier) GetBySKU(ctx context.Context, year types.YearSlug, sku string) (*Product, error) {
	if sku == "" || year == "" {
		return nil, tables.ErrRecordNotFound
	}
	query := `SELECT sku, year, name, kind, unitPrice, currency, eligibleFor, sizes, stock, active
		FROM product
		WHERE sku = ? AND year = ?`

	var (
		p           Product
		eligibleFor string
		sizes       string
		active      int
	)
	err := q.db.QueryRowContext(ctx, query, sku, year).Scan(
		&p.SKU, &p.Year, &p.Name, &p.Kind, &p.UnitPrice, &p.Currency,
		&eligibleFor, &sizes, &p.Stock, &active,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, tables.ErrRecordNotFound
		default:
			return nil, err
		}
	}
	p.Active = active != 0
	p.EligibleFor = parseEligibleFor(eligibleFor)
	p.Sizes = parseSizes(sizes)
	return &p, nil
}

// ListEligibleFor returns every active product for the given year that the
// given owner type may purchase. The wildcard EligibleAll matches all
// owner types.
func (q *querier) ListEligibleFor(ctx context.Context, year types.YearSlug, ownerType types.TeamType) ([]Product, error) {
	if year == "" {
		return nil, nil
	}
	query := `SELECT sku, year, name, kind, unitPrice, currency, eligibleFor, sizes, stock, active
		FROM product
		WHERE year = ? AND active = 1
		  AND (eligibleFor = '*' OR FIND_IN_SET(?, eligibleFor) > 0)
		ORDER BY kind, sku`

	rows, err := q.db.QueryContext(ctx, query, year, string(ownerType))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var (
			p           Product
			eligibleFor string
			sizes       string
			active      int
		)
		if err := rows.Scan(
			&p.SKU, &p.Year, &p.Name, &p.Kind, &p.UnitPrice, &p.Currency,
			&eligibleFor, &sizes, &p.Stock, &active,
		); err != nil {
			return nil, err
		}
		p.Active = active != 0
		p.EligibleFor = parseEligibleFor(eligibleFor)
		p.Sizes = parseSizes(sizes)
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return products, nil
}

func parseEligibleFor(s string) []types.TeamType {
	s = strings.TrimSpace(s)
	if s == "" || s == EligibleAll {
		return []types.TeamType{EligibleAll}
	}
	parts := strings.Split(s, ",")
	out := make([]types.TeamType, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, types.TeamType(p))
		}
	}
	return out
}

// parseSizes splits the comma-separated `sizes` column into a slice of
// slugs. Empty input returns nil so callers can treat "no sizes" and
// "this product doesn't have sizes" the same way.
func parseSizes(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
